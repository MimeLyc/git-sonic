package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"git_sonic/pkg/github"
)

func TestCreateIssueComment(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	client := github.NewClient(server.URL, "token")
	err := client.CreateIssueComment(context.Background(), "org", "repo", 9, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/repos/org/repo/issues/9/comments" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
}
