package gitutil

import (
	"bufio"
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExcludedFiles are automation artifacts that should not be committed.
// Note: With the new workspace structure (repo/ and outputs/ subdirectories),
// these files are normally written to outputs/ which is outside the git repository.
// This list is kept as a safety net in case files are accidentally written to repo/.
var ExcludedFiles = map[string]bool{
	"context.json":         true,
	"repo_instructions.md": true,
	"prompt.md":            true,
	"llm_response.json":    true,
	"llm_output.json":      true,
	"run.log":              true,
}

// Client runs git commands.
type Client struct {
	GitBinary string
}

func (c Client) gitBinary() string {
	if c.GitBinary == "" {
		return "git"
	}
	return c.GitBinary
}

// Clone clones a repository into dir.
func (c Client) Clone(ctx context.Context, repoURL, dir string) error {
	return c.run(ctx, "clone", repoURL, dir)
}

// CheckoutBranch checks out a branch from base.
func (c Client) CheckoutBranch(ctx context.Context, dir, branch, base string) error {
	if base == "" {
		return c.runDir(ctx, dir, "checkout", "-B", branch)
	}
	return c.runDir(ctx, dir, "checkout", "-B", branch, base)
}

// HasChanges returns true if there are uncommitted changes in the working directory.
// Excludes automation artifacts defined in ExcludedFiles.
func (c Client) HasChanges(ctx context.Context, dir string) (bool, error) {
	// Get list of changed files (unstaged)
	output, err := c.runDirOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}

	// Check if any non-excluded files have changes
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}
		// Format: XY filename (XY are status codes, then space, then filename)
		filename := strings.TrimSpace(line[3:])
		// Handle renamed files (old -> new)
		if idx := strings.Index(filename, " -> "); idx != -1 {
			filename = filename[idx+4:]
		}
		baseName := filepath.Base(filename)
		if !ExcludedFiles[baseName] {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// CommitAll stages non-excluded changes and commits with message.
// Returns nil without error if there are no changes to commit.
// Excludes automation artifacts defined in ExcludedFiles.
func (c Client) CommitAll(ctx context.Context, dir, message string) error {
	// Get list of all changed files
	output, err := c.runDirOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		return err
	}

	// Collect files to stage (excluding automation artifacts)
	var filesToStage []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}
		filename := strings.TrimSpace(line[3:])
		// Handle renamed files (old -> new)
		if idx := strings.Index(filename, " -> "); idx != -1 {
			filename = filename[idx+4:]
		}
		baseName := filepath.Base(filename)
		if !ExcludedFiles[baseName] {
			filesToStage = append(filesToStage, filename)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if len(filesToStage) == 0 {
		// No non-excluded changes to commit
		return nil
	}

	// Stage only the non-excluded files
	args := append([]string{"add", "--"}, filesToStage...)
	if err := c.runDir(ctx, dir, args...); err != nil {
		return err
	}

	return c.runDir(ctx, dir, "commit", "-m", message)
}

// Push pushes a branch to origin.
func (c Client) Push(ctx context.Context, dir, branch string) error {
	return c.runDir(ctx, dir, "push", "origin", branch)
}

// SetRemoteAuth updates origin URL with a token.
func (c Client) SetRemoteAuth(ctx context.Context, dir, token string) error {
	if token == "" {
		return nil
	}
	output, err := c.runDirOutput(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	current := strings.TrimSpace(output)
	updated, err := injectToken(current, token)
	if err != nil {
		return err
	}
	return c.runDir(ctx, dir, "remote", "set-url", "origin", updated)
}

// ApplyPatch applies a unified diff patch to the repository.
func (c Client) ApplyPatch(ctx context.Context, dir, patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, c.gitBinary(), "apply", "--whitespace=nowarn")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(patch)
	return cmd.Run()
}

func (c Client) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, c.gitBinary(), args...)
	return cmd.Run()
}

func (c Client) runDir(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, c.gitBinary(), args...)
	cmd.Dir = dir
	return cmd.Run()
}

func (c Client) runDirOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.gitBinary(), args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func injectToken(rawURL, token string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.User = url.UserPassword("x-access-token", token)
	return parsed.String(), nil
}

// InjectToken adds token authentication to a repository URL.
func InjectToken(rawURL, token string) (string, error) {
	return injectToken(rawURL, token)
}

// RedactToken removes token information from URLs for logs.
func RedactToken(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.User != nil {
		parsed.User = url.User("x-access-token")
	}
	return parsed.String()
}
