package contract_test

import (
	"testing"

	"git_sonic/pkg/llm"
)

func TestParseResponseRequiresDecision(t *testing.T) {
	_, err := llm.ParseResponse([]byte(`{"summary":"missing decision"}`))
	if err == nil {
		t.Fatalf("expected error for missing decision")
	}
}

func TestParseResponsePatch(t *testing.T) {
	resp, err := llm.ParseResponse([]byte(`{"decision":"proceed","patch":"diff"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Patch != "diff" {
		t.Fatalf("unexpected patch: %s", resp.Patch)
	}
}

func TestParseResponseProceed(t *testing.T) {
	resp, err := llm.ParseResponse([]byte(`{"decision":"proceed","commit_message":"msg","pr_title":"t","pr_body":"b"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != llm.DecisionProceed {
		t.Fatalf("unexpected decision: %s", resp.Decision)
	}
}

func TestParseResponseWithNoise(t *testing.T) {
	payload := "BEGIN\n{\"decision\":\"proceed\",\"summary\":\"ok\"}\nEND"
	resp, err := llm.ParseResponse([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != llm.DecisionProceed {
		t.Fatalf("unexpected decision: %s", resp.Decision)
	}
}

func TestParseResponseSkipsOtherJSON(t *testing.T) {
	payload := "noise {\"summary\":\"missing\"} more {\"decision\":\"stop\",\"summary\":\"done\"}"
	resp, err := llm.ParseResponse([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Decision != llm.DecisionStop {
		t.Fatalf("unexpected decision: %s", resp.Decision)
	}
}
