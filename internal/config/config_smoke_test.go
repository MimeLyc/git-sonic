package config

import "testing"

func TestLoadFromEnvDefaultsInternalPath(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN": "token",
		"LLM_COMMAND":  "llm",
	}
	cfg, err := LoadFromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.WebhookPath == "" {
		t.Fatalf("expected webhook path default")
	}
}
