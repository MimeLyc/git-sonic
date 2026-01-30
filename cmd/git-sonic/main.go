package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"git_sonic/pkg/agent"
	"git_sonic/pkg/allowlist"
	"git_sonic/pkg/config"
	"git_sonic/pkg/github"
	"git_sonic/pkg/gitutil"
	"git_sonic/pkg/llm"
	"git_sonic/pkg/mcp"
	"git_sonic/pkg/orchestrator"
	"git_sonic/pkg/queue"
	"git_sonic/pkg/server"
	"git_sonic/pkg/tools"
	"git_sonic/pkg/tools/builtin"
	"git_sonic/pkg/webhook"
	"git_sonic/pkg/workflow"
)

func main() {
	var printConfig bool
	var outputFormat string
	flag.BoolVar(&printConfig, "print-config", false, "print resolved configuration and exit")
	flag.StringVar(&outputFormat, "format", "text", "output format: text or json")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	if printConfig {
		if outputFormat == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(cfg); err != nil {
				log.Fatalf("print config: %v", err)
			}
			return
		}
		fmt.Fprintf(os.Stdout, "listen_addr=%s\\nwebhook_path=%s\\n", cfg.ListenAddr, cfg.WebhookPath)
		return
	}
	ipAllowlist, err := allowlist.Parse(cfg.IPAllowlist)
	if err != nil {
		log.Fatalf("allowlist error: %v", err)
	}

	ghClient := github.NewClient("", cfg.GitHubToken)
	gitClient := gitutil.Client{}
	var llmRunner llm.Runner

	// Route based on agent type
	switch cfg.AgentType {
	case "api", "cli", "claude-code", "auto":
		llmRunner = createUnifiedAgentRunner(cfg)
		log.Printf("unified agent mode: type=%s provider=%s", cfg.AgentType, cfg.LLMProviderType)
	default:
		// Legacy path for backward compatibility
		if cfg.AgentMode {
			llmRunner = createAgentRunner(cfg)
			log.Printf("agent mode enabled: max_iterations=%d tools_enabled=%v mcp_servers=%d",
				cfg.AgentMaxIterations, cfg.ToolsEnabled, len(cfg.MCPServers))
		} else if cfg.LLMAPIBaseURL != "" && cfg.LLMAPIKey != "" && cfg.LLMAPIModel != "" {
			llmRunner = llm.APIRunner{
				BaseURL:      cfg.LLMAPIBaseURL,
				APIKey:       cfg.LLMAPIKey,
				Model:        cfg.LLMAPIModel,
				Path:         cfg.LLMAPIPath,
				APIKeyHeader: cfg.LLMAPIKeyHeader,
				APIKeyPrefix: cfg.LLMAPIKeyPrefix,
				Timeout:      cfg.LLMTimeout,
				MaxAttempts:  cfg.LLMAPIMaxAttempts,
			}
		} else {
			llmRunner = llm.CommandRunner{Command: cfg.LLMCommand, Args: cfg.LLMArgs, Timeout: cfg.LLMTimeout}
		}
	}
	engine := workflow.NewEngine(cfg, ghClient, gitClient, llmRunner)

	handler := func(ctx context.Context, job queue.Job) error {
		event := job.Event
		summary := eventSummary(event)
		log.Printf("job start: %s", summary)
		var err error
		switch event.Type {
		case webhook.EventIssues:
			err = engine.HandleIssueLabel(ctx, event)
		case webhook.EventIssueComment:
			err = engine.HandleIssueComment(ctx, event)
		case webhook.EventPRComment:
			err = engine.HandlePRComment(ctx, event)
		default:
			err = nil
		}
		if err != nil {
			// Use %+v to include stack trace for WorkflowError
			log.Printf("job error: %s err=%+v", summary, err)
		} else {
			log.Printf("job done: %s", summary)
		}
		return err
	}

	q := queue.New(cfg.MaxWorkers, handler)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx, cfg.MaxWorkers)

	srv := server.New(cfg, ipAllowlist, q)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	cancel()
	q.Stop()
}

func eventSummary(event webhook.Event) string {
	issue := ""
	if event.Issue != nil {
		issue = fmt.Sprintf("issue=%d", event.Issue.Number)
	}
	pr := ""
	if event.PullRequest != nil {
		pr = fmt.Sprintf("pr=%d", event.PullRequest.Number)
	}
	parts := []string{
		"delivery=" + event.DeliveryID,
		"event=" + string(event.Type),
		"action=" + event.Action,
		"repo=" + event.Repository.FullName,
	}
	if issue != "" {
		parts = append(parts, issue)
	}
	if pr != "" {
		parts = append(parts, pr)
	}
	if event.Label != "" {
		parts = append(parts, "label="+event.Label)
	}
	if event.Sender != "" {
		parts = append(parts, "sender="+event.Sender)
	}
	return strings.Join(parts, " ")
}

// createAgentRunner creates an orchestrator-based LLM runner for agent mode.
func createAgentRunner(cfg config.Config) llm.Runner {
	log.Printf("[agent-init] creating agent runner: base_url=%s model=%s max_tokens=%d max_iterations=%d",
		cfg.LLMAPIBaseURL, cfg.LLMAPIModel, cfg.AgentMaxTokens, cfg.AgentMaxIterations)

	// Create tool registry
	registry := tools.NewRegistry()

	// Register built-in tools if enabled
	if cfg.ToolsEnabled {
		builtin.RegisterAll(registry)
		log.Printf("[agent-init] registered %d built-in tools: %v", registry.Count(), registry.Names())
	} else {
		log.Printf("[agent-init] built-in tools disabled")
	}

	// Initialize MCP servers and register their tools
	var mcpServers []*mcp.MCPServer
	for _, serverCfg := range cfg.MCPServers {
		server, err := mcp.NewMCPServer(
			serverCfg.Name,
			serverCfg.Command,
			serverCfg.Args,
			serverCfg.Env,
			cfg.RepoCloneBase,
		)
		if err != nil {
			log.Printf("warning: failed to create MCP server %s: %v", serverCfg.Name, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := server.Initialize(ctx); err != nil {
			cancel()
			log.Printf("warning: failed to initialize MCP server %s: %v", serverCfg.Name, err)
			server.Close()
			continue
		}
		cancel()

		if err := server.RegisterTools(registry); err != nil {
			log.Printf("warning: failed to register tools from MCP server %s: %v", serverCfg.Name, err)
		} else {
			log.Printf("registered %d tools from MCP server %s", len(server.Tools()), serverCfg.Name)
		}

		mcpServers = append(mcpServers, server)
	}

	// Create agent runner
	agentRunner := llm.AgentRunner{
		BaseURL:     cfg.LLMAPIBaseURL,
		APIKey:      cfg.LLMAPIKey,
		Model:       cfg.LLMAPIModel,
		MaxTokens:   cfg.AgentMaxTokens,
		Timeout:     cfg.LLMTimeout,
		MaxAttempts: cfg.LLMAPIMaxAttempts,
	}

	// Create orchestrator
	loop := orchestrator.NewAgentLoop(agentRunner, registry)

	// Create runner adapter
	runner := orchestrator.NewOrchestratorRunner(loop, registry)
	runner.MaxIterations = cfg.AgentMaxIterations
	runner.MaxMessages = cfg.AgentMaxMessages
	runner.CompactConfig = orchestrator.CompactConfig{
		Enabled:    cfg.CompactEnabled,
		Threshold:  cfg.CompactThreshold,
		KeepRecent: cfg.CompactKeepRecent,
	}
	if cfg.CompactEnabled {
		log.Printf("[agent-init] compaction enabled: threshold=%d keep_recent=%d",
			cfg.CompactThreshold, cfg.CompactKeepRecent)
	}
	runner.SystemPrompt = defaultAgentSystemPrompt

	return runner
}

// createUnifiedAgentRunner creates an LLM runner using the unified agent interface.
func createUnifiedAgentRunner(cfg config.Config) llm.Runner {
	log.Printf("[agent-init] creating unified agent runner: type=%s", cfg.AgentType)

	// Create tool registry for API agent
	registry := tools.NewRegistry()
	if cfg.ToolsEnabled {
		builtin.RegisterAll(registry)
		log.Printf("[agent-init] registered %d built-in tools: %v", registry.Count(), registry.Names())
	}

	// Initialize MCP servers and register their tools
	for _, serverCfg := range cfg.MCPServers {
		server, err := mcp.NewMCPServer(
			serverCfg.Name,
			serverCfg.Command,
			serverCfg.Args,
			serverCfg.Env,
			cfg.RepoCloneBase,
		)
		if err != nil {
			log.Printf("warning: failed to create MCP server %s: %v", serverCfg.Name, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := server.Initialize(ctx); err != nil {
			cancel()
			log.Printf("warning: failed to initialize MCP server %s: %v", serverCfg.Name, err)
			server.Close()
			continue
		}
		cancel()

		if err := server.RegisterTools(registry); err != nil {
			log.Printf("warning: failed to register tools from MCP server %s: %v", serverCfg.Name, err)
		} else {
			log.Printf("registered %d tools from MCP server %s", len(server.Tools()), serverCfg.Name)
		}
	}

	// Build agent configuration
	agentCfg := agent.AgentConfig{
		Type:     agent.AgentType(cfg.AgentType),
		Registry: registry,
	}

	// Configure API settings (used by API agent and auto-detection)
	if cfg.LLMAPIBaseURL != "" && cfg.LLMAPIKey != "" {
		agentCfg.API = &agent.APIConfig{
			ProviderType:  llm.LLMProviderType(cfg.LLMProviderType),
			BaseURL:       cfg.LLMAPIBaseURL,
			APIKey:        cfg.LLMAPIKey,
			Model:         cfg.LLMAPIModel,
			MaxTokens:     cfg.AgentMaxTokens,
			Timeout:       cfg.LLMTimeout,
			MaxAttempts:   cfg.LLMAPIMaxAttempts,
			MaxIterations: cfg.AgentMaxIterations,
			MaxMessages:   cfg.AgentMaxMessages,
			SystemPrompt:  defaultAgentSystemPrompt,
			CompactConfig: &agent.CompactConfig{
				Enabled:    cfg.CompactEnabled,
				Threshold:  cfg.CompactThreshold,
				KeepRecent: cfg.CompactKeepRecent,
			},
		}
	}

	// Configure CLI agent settings (Claude Code, aider, etc.)
	cliCommand := cfg.CLICommand
	if cliCommand == "" {
		cliCommand = cfg.ClaudeCodePath // backward compatibility
	}
	cliArgs := cfg.CLIArgs
	if len(cliArgs) == 0 {
		cliArgs = cfg.ClaudeCodeArgs // backward compatibility
	}
	if cliCommand != "" || cfg.AgentType == "cli" || cfg.AgentType == "claude-code" || cfg.AgentType == "auto" {
		agentCfg.ClaudeCode = &agent.ClaudeCodeConfig{
			Command: cliCommand,
			Args:    cliArgs,
			Timeout: cfg.LLMTimeout,
		}
	}

	// Create the agent
	ag, err := agent.NewAgent(agentCfg)
	if err != nil {
		log.Fatalf("[agent-init] failed to create agent: %v", err)
	}

	// Wrap in RunnerAdapter for backward compatibility
	runner := agent.NewRunnerAdapter(ag, defaultAgentSystemPrompt)
	log.Printf("[agent-init] unified agent created: provider=%s", ag.Capabilities().Provider)

	return runner
}

const defaultAgentSystemPrompt = `You are an autonomous engineering agent running in a repository workspace.
Your current working directory is the repository root. All bash commands execute here.

IMPORTANT workspace rules:
- All file paths must be RELATIVE to the repository root (e.g., "src/main.go", "pkg/util/helper.go").
- Do NOT use absolute paths or search the entire filesystem (e.g., never use "find /").
- Do NOT create directories like "workdir/", "output/", or "tmp/" in the repository - only modify the existing project structure.
- Use "find . -name ..." or "grep -r ..." to search within the repository.

You have access to tools for reading/writing files, running bash commands, and interacting with git and GitHub.
Analyze the task context and use the available tools to make the necessary code changes.
When complete, output a JSON object with the following fields:
- decision: 'proceed' (changes ready), 'needs_info' (need more info), or 'stop' (cannot automate)
- needs_info_comment: explanation if decision is needs_info
- commit_message: commit message for changes
- pr_title: title for the PR
- pr_body: body for the PR
- files: map of relative file paths to their complete new content
- summary: summary of what was done`
