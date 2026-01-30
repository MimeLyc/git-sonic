package unit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git_sonic/pkg/llm"
)

func TestCommandRunnerReadsOutputFile(t *testing.T) {
	workDir := t.TempDir()
	scriptPath := filepath.Join(workDir, "llm.sh")
	outputPath := filepath.Join(workDir, "llm_response.json")

	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo 'not-json'\n" +
		"printf '%s' '{\"decision\":\"proceed\",\"commit_message\":\"msg\",\"pr_title\":\"t\",\"pr_body\":\"b\"}' > \"$1\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := llm.CommandRunner{Command: scriptPath, Args: []string{outputPath}, Timeout: time.Second}
	result, err := runner.Run(context.Background(), llm.Request{Prompt: "test", OutputPath: outputPath}, workDir)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Response.Decision != llm.DecisionProceed {
		t.Fatalf("unexpected decision: %s", result.Response.Decision)
	}
	if strings.TrimSpace(result.Stdout) == "" {
		t.Fatalf("expected output from file")
	}
}
