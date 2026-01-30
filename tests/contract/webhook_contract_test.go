package contract_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"git_sonic/pkg/webhook"
)

func TestParseEventIssuesLabeled(t *testing.T) {
	payload := `{
  "action": "labeled",
  "label": {"name": "ai-ready"},
  "issue": {
    "number": 12,
    "state": "open",
    "title": "Bug report",
    "body": "Details",
    "labels": [{"name": "ai-ready"}]
  },
  "repository": {
    "full_name": "org/repo",
    "clone_url": "https://github.com/org/repo.git",
    "default_branch": "main"
  },
  "sender": {"login": "labeler"}
}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "abc")

	event, err := webhook.ParseEvent(req)
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}
	if event.Type != webhook.EventIssues {
		t.Fatalf("expected issues event, got %s", event.Type)
	}
	if event.Action != "labeled" {
		t.Fatalf("expected action labeled, got %s", event.Action)
	}
	if event.Issue == nil || event.Issue.Number != 12 {
		t.Fatalf("unexpected issue data")
	}
	if event.Label != "ai-ready" {
		t.Fatalf("expected label ai-ready, got %s", event.Label)
	}
	if event.Repository.FullName != "org/repo" {
		t.Fatalf("unexpected repo name")
	}
}
