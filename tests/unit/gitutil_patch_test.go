package unit_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git_sonic/pkg/gitutil"
)

func TestApplyPatch(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	patch := `--- a/hello.txt
+++ b/hello.txt
@@ -1 +1 @@
-old
+new
`
	client := gitutil.Client{}
	if err := client.ApplyPatch(context.Background(), dir, patch); err != nil {
		t.Fatalf("apply patch: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected content: %s", string(data))
	}
}

func TestCommitAllExcludesAutomationArtifacts(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	// Configure git user for commit
	if err := exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("git config name: %v", err)
	}

	// Create initial commit
	initFile := filepath.Join(dir, "init.txt")
	if err := os.WriteFile(initFile, []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write init file: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "add", "init.txt").Run(); err != nil {
		t.Fatalf("git add init: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", "init").Run(); err != nil {
		t.Fatalf("git commit init: %v", err)
	}

	// Create files: one real change, one automation artifact
	realFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(realFile, []byte("# Real change\n"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	artifactFile := filepath.Join(dir, "llm_output.json")
	if err := os.WriteFile(artifactFile, []byte(`{"decision":"proceed"}`), 0o644); err != nil {
		t.Fatalf("write artifact file: %v", err)
	}
	contextFile := filepath.Join(dir, "context.json")
	if err := os.WriteFile(contextFile, []byte(`{"issue":123}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	client := gitutil.Client{}

	// Check HasChanges - should return true (README.md changed)
	hasChanges, err := client.HasChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !hasChanges {
		t.Fatal("expected HasChanges to return true")
	}

	// Commit
	if err := client.CommitAll(context.Background(), dir, "test commit"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	// Check what was committed
	output, err := exec.Command("git", "-C", dir, "show", "--name-only", "--format=").Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	committedFiles := strings.TrimSpace(string(output))

	// README.md should be committed
	if !strings.Contains(committedFiles, "README.md") {
		t.Errorf("expected README.md to be committed, got: %s", committedFiles)
	}

	// Automation artifacts should NOT be committed
	if strings.Contains(committedFiles, "llm_output.json") {
		t.Errorf("llm_output.json should not be committed, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "context.json") {
		t.Errorf("context.json should not be committed, got: %s", committedFiles)
	}
}

func TestHasChangesExcludesAutomationArtifacts(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Create initial commit
	initFile := filepath.Join(dir, "init.txt")
	if err := os.WriteFile(initFile, []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write init file: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("git config name: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "add", "init.txt").Run(); err != nil {
		t.Fatalf("git add init: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", "init").Run(); err != nil {
		t.Fatalf("git commit init: %v", err)
	}

	client := gitutil.Client{}

	// Only create automation artifacts - should return false
	artifactFile := filepath.Join(dir, "llm_output.json")
	if err := os.WriteFile(artifactFile, []byte(`{"decision":"proceed"}`), 0o644); err != nil {
		t.Fatalf("write artifact file: %v", err)
	}

	hasChanges, err := client.HasChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if hasChanges {
		t.Fatal("expected HasChanges to return false when only automation artifacts changed")
	}

	// Now add a real file - should return true
	realFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(realFile, []byte("# Real change\n"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	hasChanges, err = client.HasChanges(context.Background(), dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !hasChanges {
		t.Fatal("expected HasChanges to return true when real file changed")
	}
}
