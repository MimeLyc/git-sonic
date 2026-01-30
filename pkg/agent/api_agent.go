package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"git_sonic/pkg/llm"
	"git_sonic/pkg/orchestrator"
	"git_sonic/pkg/tools"
)

// APIAgent implements Agent using the local orchestrator with LLM API.
type APIAgent struct {
	// provider is the LLM API provider (Claude, OpenAI, etc.).
	provider llm.LLMProvider

	// registry contains available tools.
	registry *tools.Registry

	// loop is the orchestrator agent loop.
	loop *orchestrator.AgentLoop

	// options configures the agent behavior.
	options APIAgentOptions
}

// APIAgentOptions configures the APIAgent.
type APIAgentOptions struct {
	// MaxIterations limits agent loop iterations.
	MaxIterations int

	// MaxMessages limits conversation history size.
	MaxMessages int

	// MaxTokens limits response token count.
	MaxTokens int

	// SystemPrompt is the default system prompt.
	SystemPrompt string

	// CompactConfig configures context compaction.
	CompactConfig *CompactConfig
}

// NewAPIAgent creates a new APIAgent.
// The provider parameter accepts any LLMProvider implementation (ClaudeProvider, OpenAIProvider, etc.)
// or the legacy AgentRunner which implements LLMProvider for backward compatibility.
func NewAPIAgent(provider llm.LLMProvider, registry *tools.Registry, opts APIAgentOptions) *APIAgent {
	if registry == nil {
		registry = tools.NewRegistry()
	}
	loop := orchestrator.NewAgentLoop(provider, registry)

	// Set defaults
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 50
	}
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = 50
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4096
	}

	return &APIAgent{
		provider: provider,
		registry: registry,
		loop:     loop,
		options:  opts,
	}
}

// Execute runs the agent with the given request.
func (a *APIAgent) Execute(ctx context.Context, req AgentRequest) (AgentResult, error) {
	startTime := time.Now()
	log.Printf("[api-agent] starting execution: workdir=%s task_length=%d",
		req.WorkDir, len(req.Task))

	// Build user prompt from request
	userPrompt := buildUserPrompt(req)

	// Convert AgentRequest to OrchestratorRequest
	orchReq := orchestrator.OrchestratorRequest{
		SystemPrompt:     req.SystemPrompt,
		RepoInstructions: req.RepoInstructions,
		InitialMessages: []llm.Message{
			llm.NewTextMessage(llm.RoleUser, userPrompt),
		},
		MaxIterations: a.options.MaxIterations,
		MaxMessages:   a.options.MaxMessages,
		WorkDir:       req.WorkDir,
		ToolContext:   tools.NewToolContext(req.WorkDir),
	}

	// Apply request options
	if req.Options.MaxIterations > 0 {
		orchReq.MaxIterations = req.Options.MaxIterations
	}
	if req.Options.CompactConfig != nil {
		orchReq.CompactConfig = orchestrator.CompactConfig{
			Enabled:    req.Options.CompactConfig.Enabled,
			Threshold:  req.Options.CompactConfig.Threshold,
			KeepRecent: req.Options.CompactConfig.KeepRecent,
		}
	} else if a.options.CompactConfig != nil {
		orchReq.CompactConfig = orchestrator.CompactConfig{
			Enabled:    a.options.CompactConfig.Enabled,
			Threshold:  a.options.CompactConfig.Threshold,
			KeepRecent: a.options.CompactConfig.KeepRecent,
		}
	}

	// Set up callbacks
	if req.Callbacks.OnMessage != nil {
		orchReq.OnMessage = req.Callbacks.OnMessage
	}
	if req.Callbacks.OnToolCall != nil {
		orchReq.OnToolCall = req.Callbacks.OnToolCall
	}
	if req.Callbacks.OnToolResult != nil {
		orchReq.OnToolResult = req.Callbacks.OnToolResult
	}

	// Run the orchestrator
	orchResult, err := a.loop.Run(ctx, orchReq)
	if err != nil {
		log.Printf("[api-agent] ERROR: orchestrator failed: %v", err)
		return AgentResult{
			Success: false,
			Message: fmt.Sprintf("orchestrator error: %v", err),
		}, err
	}

	// Convert OrchestratorResult to AgentResult
	result := convertOrchestratorResult(orchResult, startTime)
	log.Printf("[api-agent] execution complete: success=%v decision=%s iterations=%d",
		result.Success, result.Decision, result.Usage.TotalIterations)

	return result, nil
}

// Capabilities returns the agent's capabilities.
func (a *APIAgent) Capabilities() AgentCapabilities {
	toolList := a.registry.List()
	toolInfos := make([]ToolInfo, len(toolList))
	for i, t := range toolList {
		toolInfos[i] = ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
		}
	}

	return AgentCapabilities{
		SupportsTools:      true,
		AvailableTools:     toolInfos,
		SupportsStreaming:  false,
		SupportsCompaction: true,
		MaxContextTokens:   200000, // Claude's context window
		Provider:           "api",
	}
}

// Close releases resources.
func (a *APIAgent) Close() error {
	return nil
}

// buildUserPrompt creates the user prompt from an AgentRequest.
// Always uses structured context when available (issue/PR data), since the agent
// works in the repo directory and cannot access files in the outputs directory.
func buildUserPrompt(req AgentRequest) string {
	hasContext := req.Context.IssueNumber > 0 || req.Context.PRNumber > 0

	// Only use req.Task directly if there's no structured context
	if !hasContext && req.Task != "" {
		return req.Task
	}

	// Build prompt from structured context
	var parts []string

	if req.Context.RepoFullName != "" {
		parts = append(parts, fmt.Sprintf("Repository: %s", req.Context.RepoFullName))
	}

	parts = append(parts, "Working directory: current directory is the repository root. All file paths should be relative to this directory.")

	if req.Context.IssueNumber > 0 {
		parts = append(parts, fmt.Sprintf("\n## Issue #%d", req.Context.IssueNumber))
		if req.Context.IssueTitle != "" {
			parts = append(parts, fmt.Sprintf("Title: %s", req.Context.IssueTitle))
		}
		if req.Context.IssueBody != "" {
			parts = append(parts, fmt.Sprintf("Body:\n%s", req.Context.IssueBody))
		}
		if len(req.Context.IssueLabels) > 0 {
			parts = append(parts, fmt.Sprintf("Labels: %s", strings.Join(req.Context.IssueLabels, ", ")))
		}
		if len(req.Context.IssueComments) > 0 {
			parts = append(parts, "\n### Comments:")
			for _, c := range req.Context.IssueComments {
				parts = append(parts, fmt.Sprintf("@%s: %s", c.User, c.Body))
			}
		}
	}

	if req.Context.PRNumber > 0 {
		parts = append(parts, fmt.Sprintf("\n## PR #%d", req.Context.PRNumber))
		if req.Context.PRTitle != "" {
			parts = append(parts, fmt.Sprintf("Title: %s", req.Context.PRTitle))
		}
		if req.Context.PRBody != "" {
			parts = append(parts, fmt.Sprintf("Body:\n%s", req.Context.PRBody))
		}
		if req.Context.PRHeadRef != "" {
			parts = append(parts, fmt.Sprintf("Head: %s", req.Context.PRHeadRef))
		}
		if req.Context.PRBaseRef != "" {
			parts = append(parts, fmt.Sprintf("Base: %s", req.Context.PRBaseRef))
		}
	}

	if req.Context.CommentBody != "" {
		parts = append(parts, fmt.Sprintf("\n## Comment\n%s", req.Context.CommentBody))
	}

	if req.Context.SlashCommand != "" {
		parts = append(parts, fmt.Sprintf("\nSlash Command: %s", req.Context.SlashCommand))
	}

	if req.Context.Requirements != "" {
		parts = append(parts, fmt.Sprintf("\n## Requirements\n%s", req.Context.Requirements))
	}

	parts = append(parts, "\n## Instructions")
	parts = append(parts, "Analyze the context and make the necessary code changes.")
	parts = append(parts, "Use the available tools to read files, make changes, and run commands.")
	parts = append(parts, "IMPORTANT: Your current working directory is the repository root. Use relative paths (e.g., 'src/main.go', not '/path/to/repo/src/main.go'). Do NOT create new top-level directories like 'workdir/' - work within the existing repository structure.")
	parts = append(parts, "When complete, output a JSON object with the following fields:")
	parts = append(parts, "- decision: 'proceed' (changes ready), 'needs_info' (need more info), or 'stop' (cannot automate)")
	parts = append(parts, "- needs_info_comment: explanation if decision is needs_info")
	parts = append(parts, "- commit_message: commit message for changes")
	parts = append(parts, "- pr_title: title for the PR")
	parts = append(parts, "- pr_body: body for the PR")
	parts = append(parts, "- files: map of relative file paths to their complete new content")
	parts = append(parts, "- summary: summary of what was done")

	return strings.Join(parts, "\n")
}

// convertOrchestratorResult converts an OrchestratorResult to an AgentResult.
func convertOrchestratorResult(orchResult orchestrator.OrchestratorResult, startTime time.Time) AgentResult {
	result := AgentResult{
		Success: true,
		Usage: ExecutionUsage{
			TotalIterations:   orchResult.TotalIterations,
			TotalInputTokens:  orchResult.TotalInputTokens,
			TotalOutputTokens: orchResult.TotalOutputTokens,
			TotalDuration:     time.Since(startTime),
		},
		RawOutput: orchResult.Messages,
	}

	// Convert tool calls
	for _, tc := range orchResult.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCallRecord{
			Name:    tc.Name,
			Input:   tc.Input,
			Output:  tc.Result.Content,
			IsError: tc.Result.IsError,
		})
	}

	// Extract file changes from tool calls
	for _, tc := range orchResult.ToolCalls {
		if tc.Name == "write_file" {
			path, _ := tc.Input["path"].(string)
			content, _ := tc.Input["content"].(string)
			if path != "" {
				result.FileChanges = append(result.FileChanges, FileChange{
					Path:      path,
					Content:   content,
					Operation: FileOpModify,
				})
			}
		}
	}

	// Parse the final response
	finalText := orchResult.GetFinalText()
	result.Message = finalText

	// Try to parse as structured response
	parseStructuredResponse(&result, finalText)

	return result
}

// parseStructuredResponse attempts to extract structured fields from the final text.
func parseStructuredResponse(result *AgentResult, text string) {
	// Try to find and parse JSON in the response
	if !strings.Contains(text, `"decision"`) {
		result.Decision = DecisionProceed
		result.Summary = text
		return
	}

	var resp struct {
		Decision         string            `json:"decision"`
		NeedsInfoComment string            `json:"needs_info_comment"`
		CommitMessage    string            `json:"commit_message"`
		PRTitle          string            `json:"pr_title"`
		PRBody           string            `json:"pr_body"`
		Summary          string            `json:"summary"`
		Files            map[string]string `json:"files"`
	}

	// Find JSON object in text
	start := strings.Index(text, "{")
	if start == -1 {
		result.Decision = DecisionProceed
		result.Summary = text
		return
	}

	// Try to parse from the first { to the end
	jsonText := text[start:]
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		// Try to find matching brace
		depth := 0
		end := -1
		for i, c := range jsonText {
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		if end > 0 {
			json.Unmarshal([]byte(jsonText[:end]), &resp)
		}
	}

	// Map decision
	switch resp.Decision {
	case "proceed":
		result.Decision = DecisionProceed
	case "needs_info":
		result.Decision = DecisionNeedsInfo
	case "stop":
		result.Decision = DecisionStop
	default:
		result.Decision = DecisionProceed
	}

	result.NeedsInfoComment = resp.NeedsInfoComment
	result.CommitMessage = resp.CommitMessage
	result.PRTitle = resp.PRTitle
	result.PRBody = resp.PRBody
	result.Summary = resp.Summary

	// Add files from response
	for path, content := range resp.Files {
		// Check if already in FileChanges
		found := false
		for _, fc := range result.FileChanges {
			if fc.Path == path {
				found = true
				break
			}
		}
		if !found {
			result.FileChanges = append(result.FileChanges, FileChange{
				Path:      path,
				Content:   content,
				Operation: FileOpModify,
			})
		}
	}
}
