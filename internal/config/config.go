package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

// Config holds runtime configuration.
type Config struct {
	ListenAddr      string
	WebhookPath     string
	IPAllowlist     string
	GitHubToken     string
	RepoCloneBase   string
	MaxWorkers      int
	TriggerLabels   []string
	NeedsInfoLabel  string
	InProgressLabel string
	DoneLabel       string
	PRSlashCommands []string
	LogLevel        string

	// AI/LLM runtime configuration remains in reusable pkg/llm.
	llm.RuntimeConfig
}

const (
	defaultListenAddr      = ":8080"
	defaultWebhookPath     = "/webhook"
	defaultRepoBase        = "./workdir"
	defaultMaxWorkers      = 2
	defaultTriggerLabels   = "ai-ready"
	defaultNeedsInfoLabel  = "ai-needs-info"
	defaultInProgressLabel = "ai-in-progress"
	defaultDoneLabel       = "ai-done"
	defaultPRSlashCommands = "/ai-optimize"
	defaultLogLevel        = "info"
)

// Load loads configuration from environment variables.
func Load() (Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv loads configuration from a getenv-like function.
func LoadFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		ListenAddr:      getOrDefault(getenv, "LISTEN_ADDR", defaultListenAddr),
		WebhookPath:     getOrDefault(getenv, "WEBHOOK_PATH", defaultWebhookPath),
		IPAllowlist:     getenv("IP_ALLOWLIST"),
		GitHubToken:     getenv("GITHUB_TOKEN"),
		RepoCloneBase:   getOrDefault(getenv, "REPO_CLONE_BASE", defaultRepoBase),
		MaxWorkers:      getIntOrDefault(getenv, "MAX_WORKERS", defaultMaxWorkers),
		TriggerLabels:   parseList(getOrDefault(getenv, "TRIGGER_LABELS", defaultTriggerLabels)),
		NeedsInfoLabel:  getOrDefault(getenv, "NEEDS_INFO_LABEL", defaultNeedsInfoLabel),
		InProgressLabel: getOrDefault(getenv, "IN_PROGRESS_LABEL", defaultInProgressLabel),
		DoneLabel:       getOrDefault(getenv, "DONE_LABEL", defaultDoneLabel),
		PRSlashCommands: parseList(getOrDefault(getenv, "PR_SLASH_COMMANDS", defaultPRSlashCommands)),
		LogLevel:        getOrDefault(getenv, "LOG_LEVEL", defaultLogLevel),
		RuntimeConfig:   llm.LoadRuntimeConfig(getenv),
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
	return cfg.RuntimeConfig.UsesAPI()
}

func getOrDefault(getenv func(string) string, key, def string) string {
	val := getenv(key)
	if val == "" {
		return def
	}
	return val
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
