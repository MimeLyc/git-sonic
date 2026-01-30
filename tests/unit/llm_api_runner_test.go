package unit_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git_sonic/pkg/llm"
)

func TestAPIRunnerBuildsRequestAndParsesResponse(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unexpected payload: %v", err)
		}
		if payload["model"] != "test-model" {
			t.Fatalf("expected model test-model, got %v", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"decision\":\"proceed\",\"commit_message\":\"msg\",\"pr_title\":\"t\",\"pr_body\":\"b\"}"}}],"usage":{"total_tokens":15}}`))
	}))
	defer server.Close()

	runner := llm.APIRunner{
		BaseURL:     server.URL,
		APIKey:      "token",
		Model:       "test-model",
		Path:        "/v1/chat/completions",
		Timeout:     time.Second,
		HTTPClient:  server.Client(),
		MaxAttempts: 1,
		Sleep:       func(time.Duration) {},
		Backoff:     func(int) time.Duration { return 0 },
	}

	result, err := runner.Run(context.Background(), llm.Request{Mode: "issue"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Response.Decision != llm.DecisionProceed {
		t.Fatalf("expected proceed, got %s", result.Response.Decision)
	}
	if !strings.Contains(result.Stdout, "\"usage\"") {
		t.Fatalf("expected raw API response to include usage")
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("expected Authorization header, got %s", gotAuth)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("expected path /v1/chat/completions, got %s", gotPath)
	}
}

func TestAPIRunnerRetriesTransientFailures(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"decision\":\"proceed\",\"commit_message\":\"msg\",\"pr_title\":\"t\",\"pr_body\":\"b\"}"}}]}`))
	}))
	defer server.Close()

	runner := llm.APIRunner{
		BaseURL:     server.URL,
		APIKey:      "token",
		Model:       "test-model",
		Path:        "/v1/chat/completions",
		Timeout:     time.Second,
		HTTPClient:  server.Client(),
		MaxAttempts: 5,
		Sleep:       func(time.Duration) {},
		Backoff:     func(int) time.Duration { return 0 },
	}

	_, err := runner.Run(context.Background(), llm.Request{Mode: "issue"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
