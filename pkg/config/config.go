package config

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration.
type Config struct {
	ListenAddr        string
	WebhookPath       string
	IPAllowlist       string
	GitHubToken       string
	RepoCloneBase     string
	MaxWorkers        int
	TriggerLabels     []string
	NeedsInfoLabel    string
	InProgressLabel   string
	DoneLabel         string
	PRSlashCommands   []string
	LLMCommand        string
	LLMArgs           []string
	LLMAPIBaseURL     string
	LLMAPIKey         string
	LLMAPIModel       string
	LLMAPIPath        string
	LLMAPIKeyHeader   string
	LLMAPIKeyPrefix   string
	LLMAPIMaxAttempts int
	LLMTimeout        time.Duration
	LogLevel          string

	// Agent mode configuration
	AgentMode          bool
	AgentMaxIterations int
	AgentMaxMessages   int
	AgentMaxTokens     int
	ToolsEnabled       bool
	MCPServers         []MCPServerConfig

	// Compact (context summarization) configuration
	CompactEnabled    bool
	CompactThreshold  int // Trigger compact when messages exceed this
	CompactKeepRecent int // Keep this many recent messages after compact

	// Unified Agent configuration
	AgentType       string   // "api", "cli", "claude-code", "auto"
	LLMProviderType string   // "claude", "openai" (when AgentType=api)
	CLICommand      string   // CLI command path (e.g., "claude", "aider")
	CLIArgs         []string // Additional arguments for CLI command

	// Deprecated: Use CLICommand instead
	ClaudeCodePath string
	// Deprecated: Use CLIArgs instead
	ClaudeCodeArgs []string
}

// MCPServerConfig configures an MCP server connection.
type MCPServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

const (
	defaultListenAddr          = ":8080"
	defaultWebhookPath         = "/webhook"
	defaultRepoBase            = "./workdir"
	defaultMaxWorkers          = 2
	defaultTriggerLabels       = "ai-ready"
	defaultNeedsInfoLabel      = "ai-needs-info"
	defaultInProgressLabel     = "ai-in-progress"
	defaultDoneLabel           = "ai-done"
	defaultPRSlashCommands     = "/ai-optimize"
	defaultLLMAPIPath          = "/v1/chat/completions"
	defaultLLMAPIKeyHeader     = "Authorization"
	defaultLLMAPIKeyPrefix     = "Bearer"
	defaultLLMAPIMaxAttempts   = 5
	defaultLLMTimeout          = 30 * time.Minute
	defaultLogLevel            = "info"
	defaultAgentMaxIterations  = 50
	defaultAgentMaxMessages    = 40
	defaultAgentMaxTokens      = 4096
	defaultCompactThreshold    = 30
	defaultCompactKeepRecent   = 10
	defaultAgentType           = "api"
)

// Load loads configuration from environment variables.
func Load() (Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv loads configuration from a getenv-like function.
func LoadFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		ListenAddr:         getOrDefault(getenv, "LISTEN_ADDR", defaultListenAddr),
		WebhookPath:        getOrDefault(getenv, "WEBHOOK_PATH", defaultWebhookPath),
		IPAllowlist:        getenv("IP_ALLOWLIST"),
		GitHubToken:        getenv("GITHUB_TOKEN"),
		RepoCloneBase:      getOrDefault(getenv, "REPO_CLONE_BASE", defaultRepoBase),
		MaxWorkers:         getIntOrDefault(getenv, "MAX_WORKERS", defaultMaxWorkers),
		TriggerLabels:      parseList(getOrDefault(getenv, "TRIGGER_LABELS", defaultTriggerLabels)),
		NeedsInfoLabel:     getOrDefault(getenv, "NEEDS_INFO_LABEL", defaultNeedsInfoLabel),
		InProgressLabel:    getOrDefault(getenv, "IN_PROGRESS_LABEL", defaultInProgressLabel),
		DoneLabel:          getOrDefault(getenv, "DONE_LABEL", defaultDoneLabel),
		PRSlashCommands:    parseList(getOrDefault(getenv, "PR_SLASH_COMMANDS", defaultPRSlashCommands)),
		LLMCommand:         getenv("LLM_COMMAND"),
		LLMArgs:            parseList(getenv("LLM_ARGS")),
		LLMAPIBaseURL:      getenv("LLM_API_BASE_URL"),
		LLMAPIKey:          getenv("LLM_API_KEY"),
		LLMAPIModel:        getenv("LLM_API_MODEL"),
		LLMAPIPath:         getOrDefault(getenv, "LLM_API_PATH", defaultLLMAPIPath),
		LLMAPIKeyHeader:    getOrDefault(getenv, "LLM_API_KEY_HEADER", defaultLLMAPIKeyHeader),
		LLMAPIKeyPrefix:    getOrDefault(getenv, "LLM_API_KEY_PREFIX", defaultLLMAPIKeyPrefix),
		LLMAPIMaxAttempts:  getIntOrDefault(getenv, "LLM_API_MAX_ATTEMPTS", defaultLLMAPIMaxAttempts),
		LLMTimeout:         getDurationOrDefault(getenv, "LLM_TIMEOUT", defaultLLMTimeout),
		LogLevel:           getOrDefault(getenv, "LOG_LEVEL", defaultLogLevel),
		AgentMode:          getBoolOrDefault(getenv, "AGENT_MODE", false),
		AgentMaxIterations: getIntOrDefault(getenv, "AGENT_MAX_ITERATIONS", defaultAgentMaxIterations),
		AgentMaxMessages:   getIntOrDefault(getenv, "AGENT_MAX_MESSAGES", defaultAgentMaxMessages),
		AgentMaxTokens:     getIntOrDefault(getenv, "AGENT_MAX_TOKENS", defaultAgentMaxTokens),
		ToolsEnabled:       getBoolOrDefault(getenv, "TOOLS_ENABLED", true),
		MCPServers:         parseMCPServers(getenv("MCP_SERVERS")),
		CompactEnabled:     getBoolOrDefault(getenv, "COMPACT_ENABLED", true),
		CompactThreshold:   getIntOrDefault(getenv, "COMPACT_THRESHOLD", defaultCompactThreshold),
		CompactKeepRecent:  getIntOrDefault(getenv, "COMPACT_KEEP_RECENT", defaultCompactKeepRecent),
		AgentType:       getOrDefault(getenv, "AGENT_TYPE", defaultAgentType),
		LLMProviderType: getOrDefault(getenv, "LLM_PROVIDER_TYPE", "claude"),
		CLICommand:      getOrDefaultMulti(getenv, "CLI_COMMAND", "CLAUDE_CODE_PATH", ""),
		CLIArgs:         parseListMulti(getenv, "CLI_ARGS", "CLAUDE_CODE_ARGS"),
		// Backward compatibility
		ClaudeCodePath: getenv("CLAUDE_CODE_PATH"),
		ClaudeCodeArgs: parseList(getenv("CLAUDE_CODE_ARGS")),
	}

	if cfg.GitHubToken == "" {
		return Config{}, errors.New("GITHUB_TOKEN is required")
	}

	// Validate based on agent type
	switch cfg.AgentType {
	case "api":
		if cfg.AgentMode {
			// Agent mode requires Claude API
			if cfg.LLMAPIBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMAPIModel == "" {
				return Config{}, errors.New("LLM_API_BASE_URL, LLM_API_KEY, and LLM_API_MODEL are required for agent mode")
			}
		} else if usesAPI(cfg) {
			if cfg.LLMAPIBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMAPIModel == "" {
				return Config{}, errors.New("LLM_API_BASE_URL, LLM_API_KEY, and LLM_API_MODEL are required for API mode")
			}
		} else if cfg.LLMCommand == "" {
			return Config{}, errors.New("LLM_COMMAND is required when API mode is not configured")
		}
	case "cli", "claude-code":
		// CLI mode doesn't require API credentials
		// Will use an external CLI tool (claude, aider, etc.)
	case "auto":
		// Auto mode will try to detect the best available agent
		// No strict validation needed
	default:
		// For backward compatibility, treat unknown types as legacy mode
		if cfg.AgentMode {
			if cfg.LLMAPIBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMAPIModel == "" {
				return Config{}, errors.New("LLM_API_BASE_URL, LLM_API_KEY, and LLM_API_MODEL are required for agent mode")
			}
		} else if usesAPI(cfg) {
			if cfg.LLMAPIBaseURL == "" || cfg.LLMAPIKey == "" || cfg.LLMAPIModel == "" {
				return Config{}, errors.New("LLM_API_BASE_URL, LLM_API_KEY, and LLM_API_MODEL are required for API mode")
			}
		} else if cfg.LLMCommand == "" {
			return Config{}, errors.New("LLM_COMMAND is required when API mode is not configured")
		}
	}
	if cfg.WebhookPath == "" {
		cfg.WebhookPath = defaultWebhookPath
	}
	return cfg, nil
}

func usesAPI(cfg Config) bool {
	return cfg.LLMAPIBaseURL != "" || cfg.LLMAPIKey != "" || cfg.LLMAPIModel != ""
}

func getOrDefault(getenv func(string) string, key, def string) string {
	val := getenv(key)
	if val == "" {
		return def
	}
	return val
}

// getOrDefaultMulti tries multiple keys in order, returning the first non-empty value.
func getOrDefaultMulti(getenv func(string) string, keys ...string) string {
	for _, key := range keys {
		if val := getenv(key); val != "" {
			return val
		}
	}
	return ""
}

// parseListMulti tries multiple keys in order, returning the first non-empty list.
func parseListMulti(getenv func(string) string, keys ...string) []string {
	for _, key := range keys {
		if val := getenv(key); val != "" {
			return parseList(val)
		}
	}
	return nil
}

func getIntOrDefault(getenv func(string) string, key string, def int) int {
	val := getenv(key)
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return parsed
}

func getDurationOrDefault(getenv func(string) string, key string, def time.Duration) time.Duration {
	val := getenv(key)
	if val == "" {
		return def
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return def
	}
	return parsed
}

func parseList(value string) []string {
	if value == "" {
		return nil
	}
	var parts []string
	if strings.Contains(value, ",") {
		parts = strings.Split(value, ",")
	} else {
		parts = strings.Fields(value)
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func getBoolOrDefault(getenv func(string) string, key string, def bool) bool {
	val := strings.ToLower(getenv(key))
	if val == "" {
		return def
	}
	return val == "true" || val == "1" || val == "yes"
}

// parseMCPServers parses MCP server configurations from a JSON string.
// Format: [{"name":"server1","command":"cmd","args":["arg1"],"env":{"KEY":"val"}}]
func parseMCPServers(value string) []MCPServerConfig {
	if value == "" {
		return nil
	}
	// Simple format: name:command or JSON array
	if strings.HasPrefix(value, "[") {
		// JSON format
		var servers []MCPServerConfig
		if err := json.Unmarshal([]byte(value), &servers); err != nil {
			return nil
		}
		return servers
	}
	// Simple format: name:command,name2:command2
	var servers []MCPServerConfig
	for _, part := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(parts) == 2 {
			servers = append(servers, MCPServerConfig{
				Name:    strings.TrimSpace(parts[0]),
				Command: strings.TrimSpace(parts[1]),
			})
		}
	}
	return servers
}
