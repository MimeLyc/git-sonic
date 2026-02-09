package unit_test

import (
	"testing"
	"time"

	"git_sonic/internal/config"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN": "token",
		"LLM_COMMAND":  "llm",
	}
	cfg, err := config.LoadFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("expected default listen addr")
	}
	if cfg.LLMTimeout != 30*time.Minute {
		t.Fatalf("expected default timeout")
	}
}

func TestLoadFromEnvAPIMode(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":     "token",
		"LLM_API_BASE_URL": "https://api.example.com",
		"LLM_API_KEY":      "api-key",
		"LLM_API_MODEL":    "model",
	}
	cfg, err := config.LoadFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLMAPIBaseURL != "https://api.example.com" {
		t.Fatalf("unexpected base url: %s", cfg.LLMAPIBaseURL)
	}
	if cfg.LLMAPIModel != "model" {
		t.Fatalf("unexpected model: %s", cfg.LLMAPIModel)
	}
}

func TestLoadFromEnvLists(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":   "token",
		"LLM_COMMAND":    "llm",
		"TRIGGER_LABELS": "a, b ,c",
	}
	cfg, err := config.LoadFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TriggerLabels) != 3 || cfg.TriggerLabels[1] != "b" {
		t.Fatalf("unexpected trigger labels: %#v", cfg.TriggerLabels)
	}
}
