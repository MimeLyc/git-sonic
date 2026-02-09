package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRepoInstructionsAggregatesRootToLeaf(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	leaf := filepath.Join(repo, "services", "api")
	mustMkdirAll(t, leaf)

	mustWriteText(t, filepath.Join(repo, "AGENT.md"), "root rules")
	mustWriteText(t, filepath.Join(repo, "services", "AGENT.md"), "services rules")
	mustWriteText(t, filepath.Join(leaf, "AGENT.md"), "api rules")

	got := buildRepoInstructions(leaf)
	for _, want := range []string{"root rules", "services rules", "api rules"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in repo instructions, got: %q", want, got)
		}
	}
}

func TestBuildRepoInstructionsFallsBackToREADME(t *testing.T) {
	repo := t.TempDir()
	mustMkdirAll(t, filepath.Join(repo, ".git"))
	mustWriteText(t, filepath.Join(repo, "README.md"), "readme fallback")

	got := buildRepoInstructions(repo)
	if !strings.Contains(got, "readme fallback") {
		t.Fatalf("expected README fallback content, got: %q", got)
	}
}

func mustWriteText(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
