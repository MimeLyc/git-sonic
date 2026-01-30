package e2e_test

import (
	"context"
	"testing"
	"time"

	"git_sonic/pkg/config"
	"git_sonic/pkg/github"
	"git_sonic/pkg/llm"
	"git_sonic/pkg/webhook"
	"git_sonic/pkg/workflow"
)

type fakeGitHub struct {
	createdPR   bool
	assignedTo  string
	commented   bool
	labelUpdate bool
}

type fakeGit struct{}

type fakeLLM struct{}

func (f *fakeGitHub) GetIssue(ctx context.Context, owner, repo string, number int) (github.Issue, error) {
	return github.Issue{Number: number, State: "open", Title: "t", Body: "b", Labels: []string{"ai-ready"}, Author: "author"}, nil
}

func (f *fakeGitHub) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]github.Comment, error) {
	return []github.Comment{{User: "commenter", Body: "details"}}, nil
}

func (f *fakeGitHub) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	f.commented = true
	return nil
}

func (f *fakeGitHub) SetIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	f.labelUpdate = true
	return nil
}

func (f *fakeGitHub) CreatePR(ctx context.Context, owner, repo string, req github.PRRequest) (github.PR, error) {
	f.createdPR = true
	return github.PR{Number: 10, URL: "https://example.com/pr/10"}, nil
}

func (f *fakeGitHub) UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error {
	return nil
}

func (f *fakeGitHub) AddAssignees(ctx context.Context, owner, repo string, number int, assignees []string) error {
	if len(assignees) > 0 {
		f.assignedTo = assignees[0]
	}
	return nil
}

func (f *fakeGitHub) GetRepo(ctx context.Context, owner, repo string) (github.Repo, error) {
	return github.Repo{DefaultBranch: "main", CloneURL: "https://github.com/org/repo.git"}, nil
}

func (f *fakeGitHub) GetPR(ctx context.Context, owner, repo string, number int) (github.PR, error) {
	return github.PR{}, nil
}

func (f *fakeGit) Clone(ctx context.Context, repoURL, dir string) error               { return nil }
func (f *fakeGit) CheckoutBranch(ctx context.Context, dir, branch, base string) error { return nil }
func (f *fakeGit) CommitAll(ctx context.Context, dir, message string) error           { return nil }
func (f *fakeGit) Push(ctx context.Context, dir, branch string) error                 { return nil }
func (f *fakeGit) SetRemoteAuth(ctx context.Context, dir, token string) error         { return nil }
func (f *fakeGit) ApplyPatch(ctx context.Context, dir, patch string) error            { return nil }
func (f *fakeGit) HasChanges(ctx context.Context, dir string) (bool, error)           { return true, nil }

func (f *fakeLLM) Run(ctx context.Context, req llm.Request, workDir string) (llm.RunResult, error) {
	return llm.RunResult{Response: llm.Response{Decision: llm.DecisionProceed, CommitMessage: "msg", PRTitle: "title", PRBody: "body"}}, nil
}

func TestIssueLabelFlowCreatesPR(t *testing.T) {
	cfg := config.Config{
		TriggerLabels:   []string{"ai-ready"},
		InProgressLabel: "ai-in-progress",
		DoneLabel:       "ai-done",
		NeedsInfoLabel:  "ai-needs-info",
		RepoCloneBase:   t.TempDir(),
		LLMTimeout:      10 * time.Second,
	}

	gh := &fakeGitHub{}
	engine := workflow.NewEngine(cfg, gh, &fakeGit{}, &fakeLLM{})
	event := webhook.Event{
		Type:   webhook.EventIssues,
		Action: "labeled",
		Label:  "ai-ready",
		Sender: "labeler",
		Repository: webhook.Repository{
			FullName:      "org/repo",
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
		},
		Issue: &webhook.Issue{
			Number: 12,
			State:  "open",
			Title:  "t",
			Body:   "b",
			Labels: []string{"ai-ready"},
		},
	}

	if err := engine.HandleIssueLabel(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gh.createdPR {
		t.Fatalf("expected PR to be created")
	}
	if gh.assignedTo != "labeler" {
		t.Fatalf("expected assignee labeler, got %s", gh.assignedTo)
	}
	if !gh.commented {
		t.Fatalf("expected issue comment to be posted")
	}
}
