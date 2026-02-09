package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git_sonic/internal/config"
	"git_sonic/internal/controller/webhook"
	"git_sonic/pkg/github"
	"git_sonic/pkg/gitutil"
	"git_sonic/pkg/logging"
	"github.com/MimeLyc/agent-core-go/pkg/instructions"
	"github.com/MimeLyc/agent-core-go/pkg/llm"
)

// GitHubClient defines GitHub API methods needed by the engine.
type GitHubClient interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (github.Issue, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]github.Comment, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	SetIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error
	CreatePR(ctx context.Context, owner, repo string, req github.PRRequest) (github.PR, error)
	UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error
	AddAssignees(ctx context.Context, owner, repo string, number int, assignees []string) error
	GetRepo(ctx context.Context, owner, repo string) (github.Repo, error)
	GetPR(ctx context.Context, owner, repo string, number int) (github.PR, error)
}

// GitClient defines git operations needed by the engine.
type GitClient interface {
	Clone(ctx context.Context, repoURL, dir string) error
	CheckoutBranch(ctx context.Context, dir, branch, base string) error
	CommitAll(ctx context.Context, dir, message string) error
	Push(ctx context.Context, dir, branch string) error
	SetRemoteAuth(ctx context.Context, dir, token string) error
	ApplyPatch(ctx context.Context, dir, patch string) error
	HasChanges(ctx context.Context, dir string) (bool, error)
}

// LLMRunner executes LLM requests.
type LLMRunner interface {
	Run(ctx context.Context, req llm.Request, workDir string) (llm.RunResult, error)
}

// Workspace subdirectory names
const (
	// RepoSubdir is the subdirectory within the workspace where the repository is cloned
	RepoSubdir = "repo"
	// OutputsSubdir is the subdirectory within the workspace for intermediate outputs
	OutputsSubdir = "outputs"
)

// Engine executes webhook workflows.
type Engine struct {
	cfg    config.Config
	gh     GitHubClient
	git    GitClient
	llm    LLMRunner
	now    func() time.Time
	logger *logging.Logger
}

// NewEngine creates a new workflow engine.
func NewEngine(cfg config.Config, gh GitHubClient, git GitClient, llmRunner LLMRunner) *Engine {
	return &Engine{
		cfg:    cfg,
		gh:     gh,
		git:    git,
		llm:    llmRunner,
		now:    time.Now,
		logger: logging.Default(),
	}
}

// HandleIssueLabel handles issue label events.
func (e *Engine) HandleIssueLabel(ctx context.Context, event webhook.Event) error {
	log := e.logger.With("delivery_id", event.DeliveryID, "event_type", event.Type)

	if event.Type != webhook.EventIssues {
		log.Debug("skipping event: not an issues event")
		return nil
	}
	if event.Action != "labeled" {
		log.Debug("skipping event: action is not labeled", "action", event.Action)
		return nil
	}
	if event.Issue == nil {
		log.Warn("skipping event: missing issue payload")
		return errors.New("missing issue payload")
	}
	if event.Issue.State != "open" {
		log.Debug("skipping event: issue is not open", "issue", event.Issue.Number, "state", event.Issue.State)
		return nil
	}
	if !contains(event.Label, e.cfg.TriggerLabels) {
		log.Debug("skipping event: label not in trigger list", "label", event.Label, "triggers", e.cfg.TriggerLabels)
		return nil
	}

	wfLog := e.logger.StartWorkflow("issue-label",
		"issue", event.Issue.Number,
		"repo", event.Repository.FullName,
		"label", event.Label,
		"sender", event.Sender,
	)
	err := e.handleIssue(ctx, event, true, wfLog)
	wfLog.EndWorkflow(err)
	return err
}

// HandleIssueComment handles issue comment events.
func (e *Engine) HandleIssueComment(ctx context.Context, event webhook.Event) error {
	log := e.logger.With("delivery_id", event.DeliveryID, "event_type", event.Type)

	if event.Type != webhook.EventIssueComment {
		log.Debug("skipping event: not an issue_comment event")
		return nil
	}
	if event.Action != "created" {
		log.Debug("skipping event: action is not created", "action", event.Action)
		return nil
	}
	if event.Issue == nil {
		log.Warn("skipping event: missing issue payload")
		return errors.New("missing issue payload")
	}
	if event.Issue.State != "open" {
		log.Debug("skipping event: issue is not open", "issue", event.Issue.Number, "state", event.Issue.State)
		return nil
	}

	wfLog := e.logger.StartWorkflow("issue-comment",
		"issue", event.Issue.Number,
		"repo", event.Repository.FullName,
		"sender", event.Sender,
	)
	err := e.handleIssue(ctx, event, false, wfLog)
	wfLog.EndWorkflow(err)
	return err
}

// HandlePRComment handles PR comment optimization events.
func (e *Engine) HandlePRComment(ctx context.Context, event webhook.Event) error {
	log := e.logger.With("delivery_id", event.DeliveryID, "event_type", event.Type)

	if event.Type != webhook.EventPRComment {
		log.Debug("skipping event: not a pull_request_review_comment event")
		return nil
	}
	if event.Action != "created" {
		log.Debug("skipping event: action is not created", "action", event.Action)
		return nil
	}
	if event.PullRequest == nil {
		log.Warn("skipping event: missing pull request payload")
		return errors.New("missing pull request payload")
	}
	slash := findSlashCommand(event.CommentBody, e.cfg.PRSlashCommands)
	if slash == "" {
		log.Debug("skipping event: no slash command found", "pr", event.PullRequest.Number)
		return nil
	}

	wfLog := e.logger.StartWorkflow("pr-optimize",
		"pr", event.PullRequest.Number,
		"repo", event.Repository.FullName,
		"slash_command", slash,
		"sender", event.Sender,
	)

	err := e.handlePROptimize(ctx, event, slash, wfLog)
	wfLog.EndWorkflow(err)
	return err
}

func (e *Engine) handlePROptimize(ctx context.Context, event webhook.Event, slash string, log *logging.Logger) error {
	// Step 1: Parse repository info
	done := log.Step("parse-repo-info")
	owner, repo, err := splitFullName(event.Repository.FullName)
	if err != nil {
		done(err)
		return log.WrapError("parse-repo-info", "splitFullName", err)
	}
	done(nil)

	// Step 2: Get PR details
	done = log.Step("get-pr-details", "pr", event.PullRequest.Number)
	pr, err := e.gh.GetPR(ctx, owner, repo, event.PullRequest.Number)
	if err != nil {
		done(err)
		return log.WrapError("get-pr-details", "GetPR", err)
	}
	if pr.State != "open" {
		log.Info("PR is not open, skipping", "state", pr.State)
		done(nil)
		return nil
	}
	done(nil)

	// Step 3: Prepare workspace
	done = log.Step("prepare-workspace")
	workDir, err := e.prepareWorkspace(ctx, event.Repository, fmt.Sprintf("pr-%d", pr.Number), log)
	if err != nil {
		done(err)
		return err // Already wrapped
	}
	repDir := repoDir(workDir)
	log.Info("workspace prepared", "workdir", workDir, "repodir", repDir)
	done(nil)

	// Step 4: Set remote auth
	done = log.Step("set-remote-auth")
	if err := e.git.SetRemoteAuth(ctx, repDir, e.cfg.GitHubToken); err != nil {
		done(err)
		return log.WrapError("set-remote-auth", "SetRemoteAuth", err)
	}
	done(nil)

	// Step 5: Checkout branch
	done = log.Step("checkout-branch", "branch", pr.HeadRef)
	baseRef := "origin/" + pr.HeadRef
	if err := e.git.CheckoutBranch(ctx, repDir, pr.HeadRef, baseRef); err != nil {
		done(err)
		return log.WrapError("checkout-branch", "CheckoutBranch", err)
	}
	done(nil)

	// Step 6: Prepare LLM prompt
	done = log.Step("prepare-llm-prompt")
	contextReq := llm.Request{
		Mode:         "pr_optimize",
		RepoPath:     repDir,
		RepoFullName: event.Repository.FullName,
		PRNumber:     pr.Number,
		PRTitle:      pr.Title,
		PRBody:       pr.Body,
		PRHeadRef:    pr.HeadRef,
		PRBaseRef:    pr.BaseRef,
		CommentBody:  event.CommentBody,
		SlashCommand: slash,
		Requirements: "Optimize the existing PR based on the slash command.",
	}
	request, err := e.preparePrompt(workDir, contextReq)
	if err != nil {
		done(err)
		return log.WrapError("prepare-llm-prompt", "preparePrompt", err)
	}
	done(nil)

	// Step 7: Run LLM
	done = log.Step("run-llm")
	result, err := e.llm.Run(ctx, request, repDir)
	e.writeArtifacts(workDir, request, result, err)
	if err != nil {
		done(err)
		_ = e.gh.CreateIssueComment(ctx, owner, repo, pr.Number, "Automation failed: "+err.Error())
		return log.WrapError("run-llm", "Run", err)
	}
	log.Info("LLM completed", "decision", result.Response.Decision)
	done(nil)

	// Step 8: Check decision
	if result.Response.Decision != llm.DecisionProceed {
		log.StepInfo("check-decision", "LLM decided not to proceed", "decision", result.Response.Decision)
		comment := result.Response.Summary
		if result.Response.NeedsInfoComment != "" {
			comment = result.Response.NeedsInfoComment
		}
		if comment == "" {
			comment = "Automation stopped without changes."
		}
		return e.gh.CreateIssueComment(ctx, owner, repo, pr.Number, comment)
	}

	// Step 9: Apply changes
	done = log.Step("apply-changes", "files_count", len(result.Response.Files), "has_patch", result.Response.Patch != "")
	if err := e.applyChanges(ctx, workDir, result.Response, log); err != nil {
		done(err)
		return err // Already wrapped
	}
	done(nil)

	// Step 10: Commit changes
	done = log.Step("commit-changes")
	commitMsg := fallback(result.Response.CommitMessage, fmt.Sprintf("Optimize PR #%d", pr.Number))
	if err := e.git.CommitAll(ctx, repDir, commitMsg); err != nil {
		done(err)
		return log.WrapError("commit-changes", "CommitAll", err)
	}
	done(nil)

	// Step 11: Push changes
	done = log.Step("push-changes", "branch", pr.HeadRef)
	if err := e.git.Push(ctx, repDir, pr.HeadRef); err != nil {
		done(err)
		return log.WrapError("push-changes", "Push", err)
	}
	done(nil)

	// Step 12: Update PR body
	done = log.Step("update-pr-body")
	newBody := result.Response.PRBody
	if newBody == "" {
		newBody = appendSlashContext(pr.Body, slash)
	}
	if err := e.gh.UpdatePRBody(ctx, owner, repo, pr.Number, newBody); err != nil {
		done(err)
		return log.WrapError("update-pr-body", "UpdatePRBody", err)
	}
	done(nil)

	// Step 13: Post completion comment
	done = log.Step("post-completion-comment")
	if err := e.gh.CreateIssueComment(ctx, owner, repo, pr.Number, "Automation applied: "+slash); err != nil {
		done(err)
		return log.WrapError("post-completion-comment", "CreateIssueComment", err)
	}
	done(nil)

	return nil
}

func (e *Engine) handleIssue(ctx context.Context, event webhook.Event, requireLabeler bool, log *logging.Logger) error {
	// Step 1: Parse repository info
	done := log.Step("parse-repo-info")
	owner, repo, err := splitFullName(event.Repository.FullName)
	if err != nil {
		done(err)
		return log.WrapError("parse-repo-info", "splitFullName", err)
	}
	done(nil)

	// Step 2: Get issue details
	done = log.Step("get-issue-details", "issue", event.Issue.Number)
	issue, err := e.gh.GetIssue(ctx, owner, repo, event.Issue.Number)
	if err != nil {
		done(err)
		return log.WrapError("get-issue-details", "GetIssue", err)
	}
	if issue.State != "open" {
		log.Info("issue is not open, skipping", "state", issue.State)
		done(nil)
		return nil
	}
	done(nil)

	// Step 3: Get issue comments
	done = log.Step("get-issue-comments")
	comments, err := e.gh.ListIssueComments(ctx, owner, repo, issue.Number)
	if err != nil {
		done(err)
		return log.WrapError("get-issue-comments", "ListIssueComments", err)
	}
	log.Info("fetched comments", "count", len(comments))
	done(nil)

	// Step 4: Prepare workspace
	done = log.Step("prepare-workspace")
	workDir, err := e.prepareWorkspace(ctx, event.Repository, fmt.Sprintf("issue-%d", issue.Number), log)
	if err != nil {
		done(err)
		return err // Already wrapped
	}
	repDir := repoDir(workDir)
	log.Info("workspace prepared", "workdir", workDir, "repodir", repDir)
	done(nil)

	// Step 5: Set remote auth
	done = log.Step("set-remote-auth")
	if err := e.git.SetRemoteAuth(ctx, repDir, e.cfg.GitHubToken); err != nil {
		done(err)
		return log.WrapError("set-remote-auth", "SetRemoteAuth", err)
	}
	done(nil)

	// Step 6: Get default branch
	done = log.Step("get-default-branch")
	defaultBranch := event.Repository.DefaultBranch
	if defaultBranch == "" {
		repoInfo, err := e.gh.GetRepo(ctx, owner, repo)
		if err != nil {
			done(err)
			return log.WrapError("get-default-branch", "GetRepo", err)
		}
		defaultBranch = repoInfo.DefaultBranch
	}
	log.Info("using default branch", "branch", defaultBranch)
	done(nil)

	// Step 7: Checkout new branch (include full timestamp to avoid conflicts on retry)
	branch := fmt.Sprintf("llm/issue-%d-%s", issue.Number, e.now().Format("20060102-150405"))
	done = log.Step("checkout-branch", "branch", branch, "base", defaultBranch)
	if err := e.git.CheckoutBranch(ctx, repDir, branch, "origin/"+defaultBranch); err != nil {
		done(err)
		return log.WrapError("checkout-branch", "CheckoutBranch", err)
	}
	done(nil)

	// Step 8: Update issue labels to in-progress (remove all other status labels including triggers)
	done = log.Step("update-labels-in-progress")
	labelsToRemove := append([]string{e.cfg.DoneLabel, e.cfg.NeedsInfoLabel}, e.cfg.TriggerLabels...)
	labels := updateProgressLabels(issue.Labels, e.cfg.InProgressLabel, labelsToRemove...)
	if err := e.gh.SetIssueLabels(ctx, owner, repo, issue.Number, labels); err != nil {
		done(err)
		return log.WrapError("update-labels-in-progress", "SetIssueLabels", err)
	}
	done(nil)

	// Step 9: Prepare LLM prompt
	done = log.Step("prepare-llm-prompt")
	contextReq := llm.Request{
		Mode:          issueMode(event),
		RepoPath:      repDir,
		RepoFullName:  event.Repository.FullName,
		IssueNumber:   issue.Number,
		IssueTitle:    issue.Title,
		IssueBody:     issue.Body,
		IssueLabels:   issue.Labels,
		IssueComments: toLLMComments(comments),
		CommentBody:   event.CommentBody,
		Requirements:  "Address the issue by implementing a fix and preparing a PR.",
	}
	request, err := e.preparePrompt(workDir, contextReq)
	if err != nil {
		done(err)
		return log.WrapError("prepare-llm-prompt", "preparePrompt", err)
	}
	done(nil)

	// Step 10: Run LLM
	done = log.Step("run-llm")
	result, err := e.llm.Run(ctx, request, repDir)
	e.writeArtifacts(workDir, request, result, err)
	if err != nil {
		done(err)
		_ = e.gh.CreateIssueComment(ctx, owner, repo, issue.Number, "Automation failed: "+err.Error())
		return log.WrapError("run-llm", "Run", err)
	}
	log.Info("LLM completed", "decision", result.Response.Decision, "files_count", len(result.Response.Files))
	done(nil)

	// Step 11: Check decision
	if result.Response.Decision != llm.DecisionProceed {
		log.StepInfo("check-decision", "LLM decided not to proceed", "decision", result.Response.Decision)
		comment := result.Response.NeedsInfoComment
		if comment == "" {
			comment = result.Response.Summary
		}
		if comment == "" {
			comment = "More information is required before automation can proceed."
		}
		mentions := mentionParticipants(issue.Author, comments)
		if mentions != "" {
			comment = comment + "\n\n" + mentions
		}
		if e.cfg.NeedsInfoLabel != "" {
			labelsToRemove := append([]string{e.cfg.InProgressLabel, e.cfg.DoneLabel}, e.cfg.TriggerLabels...)
			labels := updateProgressLabels(issue.Labels, e.cfg.NeedsInfoLabel, labelsToRemove...)
			_ = e.gh.SetIssueLabels(ctx, owner, repo, issue.Number, labels)
		}
		return e.gh.CreateIssueComment(ctx, owner, repo, issue.Number, comment)
	}

	// Step 12: Apply changes (write files or apply patch)
	done = log.Step("apply-changes", "files_count", len(result.Response.Files), "has_patch", result.Response.Patch != "")
	if err := e.applyChanges(ctx, workDir, result.Response, log); err != nil {
		done(err)
		return err // Already wrapped
	}
	done(nil)

	// Step 13: Check for changes
	done = log.Step("check-for-changes")
	hasChanges, err := e.git.HasChanges(ctx, repDir)
	if err != nil {
		done(err)
		return log.WrapError("check-for-changes", "HasChanges", err)
	}
	if !hasChanges {
		log.Warn("no file changes detected")
		done(nil)
		return e.gh.CreateIssueComment(ctx, owner, repo, issue.Number,
			"Automation completed but no file changes were made. The LLM indicated it would make changes but none were detected.")
	}
	log.Info("changes detected")
	done(nil)

	// Step 14: Commit changes
	done = log.Step("commit-changes")
	commitMessage := fallback(result.Response.CommitMessage, fmt.Sprintf("Resolve issue #%d", issue.Number))
	if err := e.git.CommitAll(ctx, repDir, commitMessage); err != nil {
		done(err)
		return log.WrapError("commit-changes", "CommitAll", err)
	}
	done(nil)

	// Step 15: Push changes
	done = log.Step("push-changes", "branch", branch)
	if err := e.git.Push(ctx, repDir, branch); err != nil {
		done(err)
		return log.WrapError("push-changes", "Push", err)
	}
	done(nil)

	// Step 16: Create PR
	done = log.Step("create-pr")
	prTitle := fallback(result.Response.PRTitle, fmt.Sprintf("Resolve issue #%d", issue.Number))
	prBody := fallback(result.Response.PRBody, fmt.Sprintf("Resolves #%d", issue.Number))
	pr, err := e.gh.CreatePR(ctx, owner, repo, github.PRRequest{Title: prTitle, Body: prBody, Head: branch, Base: defaultBranch})
	if err != nil {
		done(err)
		return log.WrapError("create-pr", "CreatePR", err)
	}
	log.Info("PR created", "pr_number", pr.Number, "pr_url", pr.URL)
	done(nil)

	// Step 17: Add assignees (optional)
	if requireLabeler && event.Sender != "" {
		done = log.Step("add-assignees", "assignee", event.Sender)
		if err := e.gh.AddAssignees(ctx, owner, repo, pr.Number, []string{event.Sender}); err != nil {
			log.Warn("failed to add assignees", "error", err)
		}
		done(nil)
	}

	// Step 18: Update labels to done (remove all other status labels including triggers)
	done = log.Step("update-labels-done")
	labelsToRemove = append([]string{e.cfg.InProgressLabel, e.cfg.NeedsInfoLabel}, e.cfg.TriggerLabels...)
	labels = updateProgressLabels(issue.Labels, e.cfg.DoneLabel, labelsToRemove...)
	if err := e.gh.SetIssueLabels(ctx, owner, repo, issue.Number, labels); err != nil {
		done(err)
		return log.WrapError("update-labels-done", "SetIssueLabels", err)
	}
	done(nil)

	// Step 19: Post completion comment
	done = log.Step("post-completion-comment")
	comment := fmt.Sprintf("Automation completed. PR: %s", pr.URL)
	if err := e.gh.CreateIssueComment(ctx, owner, repo, issue.Number, comment); err != nil {
		done(err)
		return log.WrapError("post-completion-comment", "CreateIssueComment", err)
	}
	done(nil)

	return nil
}

// applyChanges writes files from the response or applies a patch.
// Files are written to the repo subdirectory within the workspace.
func (e *Engine) applyChanges(ctx context.Context, workDir string, resp llm.Response, log *logging.Logger) error {
	repDir := repoDir(workDir)

	// Write files from the files map (preferred method)
	if len(resp.Files) > 0 {
		for filePath, content := range resp.Files {
			fullPath := filepath.Join(repDir, filePath)
			dir := filepath.Dir(fullPath)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return log.WrapError("apply-changes", fmt.Sprintf("MkdirAll(%s)", dir), err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
				return log.WrapError("apply-changes", fmt.Sprintf("WriteFile(%s)", filePath), err)
			}
			log.Debug("wrote file", "path", filePath, "size", len(content))
		}
		return nil
	}

	// Fallback: apply patch if provided
	if resp.Patch != "" {
		log.Info("applying patch (fallback method)", "patch_size", len(resp.Patch))
		if err := e.git.ApplyPatch(ctx, repDir, resp.Patch); err != nil {
			return log.WrapError("apply-changes", "ApplyPatch", err)
		}
	}

	return nil
}

// prepareWorkspace creates the workspace directory structure and clones the repository.
// Returns the workspace root directory. The repository is cloned into workDir/repo/
// and intermediate outputs are stored in workDir/outputs/.
func (e *Engine) prepareWorkspace(ctx context.Context, repo webhook.Repository, prefix string, log *logging.Logger) (string, error) {
	base := e.cfg.RepoCloneBase
	if base == "" {
		base = "./workdir"
	}
	workDir := filepath.Join(base, fmt.Sprintf("%s-%s", prefix, e.now().Format("20060102-150405")))

	log.Debug("creating workdir", "path", workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", log.WrapError("prepare-workspace", "MkdirAll", err)
	}

	// Create outputs subdirectory
	outputsDir := filepath.Join(workDir, OutputsSubdir)
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		return "", log.WrapError("prepare-workspace", "MkdirAll(outputs)", err)
	}

	// Create repo subdirectory
	repoDir := filepath.Join(workDir, RepoSubdir)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", log.WrapError("prepare-workspace", "MkdirAll(repo)", err)
	}

	cloneURL := repo.CloneURL
	if cloneURL == "" {
		return "", log.WrapError("prepare-workspace", "validate", errors.New("missing clone URL"))
	}

	if e.cfg.GitHubToken != "" {
		updated, err := gitutil.InjectToken(cloneURL, e.cfg.GitHubToken)
		if err == nil {
			cloneURL = updated
		}
	}

	// Clone into the repo subdirectory
	log.Debug("cloning repository", "url", gitutil.RedactToken(cloneURL), "target", repoDir)
	if err := e.git.Clone(ctx, cloneURL, repoDir); err != nil {
		return "", log.WrapError("prepare-workspace", "Clone", err)
	}

	return workDir, nil
}

// repoDir returns the repository directory within the workspace.
func repoDir(workDir string) string {
	return filepath.Join(workDir, RepoSubdir)
}

// outputsDir returns the outputs directory within the workspace.
func outputsDir(workDir string) string {
	return filepath.Join(workDir, OutputsSubdir)
}

func (e *Engine) writeArtifacts(workDir string, req llm.Request, result llm.RunResult, runErr error) {
	outDir := outputsDir(workDir)
	promptPath := filepath.Join(outDir, "prompt.md")
	llmOutPath := filepath.Join(outDir, "llm_output.json")
	logPath := filepath.Join(outDir, "run.log")
	if strings.TrimSpace(req.Prompt) != "" {
		_ = os.WriteFile(promptPath, []byte(req.Prompt), 0o644)
	}
	if result.Stdout != "" {
		_ = os.WriteFile(llmOutPath, []byte(result.Stdout), 0o644)
	}
	if runErr != nil || result.Stderr != "" {
		msg := result.Stderr
		if runErr != nil {
			// Include full error with stack trace if available
			msg = msg + "\n" + fmt.Sprintf("%+v", runErr)
		}
		_ = os.WriteFile(logPath, []byte(strings.TrimSpace(msg)), 0o644)
	}
}

func (e *Engine) preparePrompt(workDir string, contextReq llm.Request) (llm.Request, error) {
	outDir := outputsDir(workDir)
	repDir := repoDir(workDir)

	contextName := "context.json"
	contextPath := filepath.Join(outDir, contextName)
	contextData, err := json.MarshalIndent(contextReq, "", "  ")
	if err != nil {
		return llm.Request{}, fmt.Errorf("marshal context: %w", err)
	}
	if err := os.WriteFile(contextPath, contextData, 0o644); err != nil {
		return llm.Request{}, fmt.Errorf("write context file: %w", err)
	}
	// Read repo instructions from repo directory
	instructions := buildRepoInstructions(repDir)
	instructionsName := "repo_instructions.md"
	instructionsPath := filepath.Join(outDir, instructionsName)
	if err := os.WriteFile(instructionsPath, []byte(instructions), 0o644); err != nil {
		return llm.Request{}, fmt.Errorf("write instructions file: %w", err)
	}
	outputName := "llm_response.json"
	outputPath := filepath.Join(outDir, outputName)
	prompt := buildPrompt(contextName, instructionsName, outputName)
	promptPath := filepath.Join(outDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return llm.Request{}, fmt.Errorf("write prompt file: %w", err)
	}
	contextReq.Prompt = prompt
	contextReq.OutputPath = outputPath
	return contextReq, nil
}

func buildRepoInstructions(workDir string) string {
	result := instructions.Load(workDir, instructions.LoadOptions{
		CandidateFiles: []string{"AGENT.md", "AGENTS.md", "CLAUDE.md"},
		MaxBytes:       instructions.DefaultMaxBytes,
	})
	if strings.TrimSpace(result.Content) != "" {
		return result.Content
	}

	// Fallback for repos that rely on README-only guidance.
	readmePath := filepath.Join(workDir, "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		return "No repository instructions found."
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "No repository instructions found."
	}
	return fmt.Sprintf("## README.md\n%s", content)
}

func buildPrompt(contextName, instructionsName, outputName string) string {
	return strings.Join([]string{
		"You are an autonomous engineering agent running in a repo workspace.",
		"Repository root: current working directory.",
		"Read the issue/PR context from: " + contextName + ".",
		"Read repository instructions from: " + instructionsName + ".",
		"Follow all repository instructions when making changes.",
		"Repository instructions are layered from root to leaf; more specific sections should override broader ones.",
		"If the given infomation is far away from enough, respond with decision=needs_info and explain why.",
		"",
		"Required JSON fields: decision, needs_info_comment, commit_message, pr_title, pr_body, files, summary.",
		"The decision field MUST be one of: proceed (changes ready to submit as PR), needs_info (need more information from user), stop (issue should not be automated).",
		"",
		"IMPORTANT: Use the 'files' field to specify file changes.",
		"The 'files' field is a JSON object mapping relative file paths to their COMPLETE new content.",
		"Example: {\"files\": {\"README.md\": \"# Title\\n\\nNew content here...\"}}",
		"Do NOT use the 'patch' field - always use 'files' instead.",
		"",
		"Output JSON only. Do not include markdown or extra text.",
		"You may either write the JSON to stdout or write it to: " + outputName + ".",
	}, "\n")
}

func issueMode(event webhook.Event) string {
	if event.Type == webhook.EventIssueComment {
		return "issue_comment"
	}
	return "issue"
}

func toLLMComments(comments []github.Comment) []llm.Comment {
	out := make([]llm.Comment, 0, len(comments))
	for _, comment := range comments {
		out = append(out, llm.Comment{User: comment.User, Body: comment.Body})
	}
	return out
}

func contains(label string, labels []string) bool {
	for _, item := range labels {
		if item == label {
			return true
		}
	}
	return false
}

func updateProgressLabels(current []string, add string, remove ...string) []string {
	removeSet := map[string]struct{}{}
	for _, label := range remove {
		if label != "" {
			removeSet[label] = struct{}{}
		}
	}
	labels := map[string]struct{}{}
	for _, label := range current {
		if _, shouldRemove := removeSet[label]; shouldRemove {
			continue
		}
		labels[label] = struct{}{}
	}
	if add != "" {
		labels[add] = struct{}{}
	}
	out := make([]string, 0, len(labels))
	for label := range labels {
		out = append(out, label)
	}
	return out
}

func mentionParticipants(author string, comments []github.Comment) string {
	mentions := map[string]struct{}{}
	if author != "" {
		mentions[author] = struct{}{}
	}
	for _, comment := range comments {
		if comment.User != "" {
			mentions[comment.User] = struct{}{}
		}
	}
	if len(mentions) == 0 {
		return ""
	}
	out := make([]string, 0, len(mentions))
	for user := range mentions {
		out = append(out, "@"+user)
	}
	return strings.Join(out, " ")
}

func splitFullName(full string) (string, string, error) {
	parts := strings.Split(full, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full name: %s", full)
	}
	return parts[0], parts[1], nil
}

func findSlashCommand(body string, commands []string) string {
	for _, cmd := range commands {
		if cmd != "" && strings.Contains(body, cmd) {
			return cmd
		}
	}
	return ""
}

func appendSlashContext(body, slash string) string {
	if strings.Contains(body, slash) {
		return body
	}
	return strings.TrimSpace(body) + "\n\nAutomated optimization triggered by: " + slash
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

// Ensure GitClient interface compatibility with default client.
var _ GitClient = gitutil.Client{}
