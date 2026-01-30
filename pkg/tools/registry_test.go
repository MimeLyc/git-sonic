package tools

import (
	"context"
	"testing"
)

// mockTool is a test tool implementation.
type mockTool struct {
	name string
}

func (t mockTool) Name() string                             { return t.name }
func (t mockTool) Description() string                      { return "test tool" }
func (t mockTool) InputSchema() map[string]any              { return map[string]any{} }
func (t mockTool) Execute(ctx context.Context, toolCtx *ToolContext, input map[string]any) (ToolResult, error) {
	return NewToolResult("ok"), nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	tool := mockTool{name: "test_tool"}
	if err := r.Register(tool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := r.Get("test_tool")
	if got == nil {
		t.Fatal("expected tool, got nil")
	}
	if got.Name() != "test_tool" {
		t.Fatalf("expected test_tool, got %s", got.Name())
	}
}

func TestRegistryDuplicateRegistration(t *testing.T) {
	r := NewRegistry()

	tool := mockTool{name: "test_tool"}
	if err := r.Register(tool); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := r.Register(tool); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestRegistryGetNonExistent(t *testing.T) {
	r := NewRegistry()

	got := r.Get("nonexistent")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()

	r.MustRegister(mockTool{name: "tool1"})
	r.MustRegister(mockTool{name: "tool2"})

	tools := r.List()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestRegistryHas(t *testing.T) {
	r := NewRegistry()

	r.MustRegister(mockTool{name: "exists"})

	if !r.Has("exists") {
		t.Fatal("expected tool to exist")
	}
	if r.Has("nonexistent") {
		t.Fatal("expected tool to not exist")
	}
}

func TestRegistryClear(t *testing.T) {
	r := NewRegistry()

	r.MustRegister(mockTool{name: "tool1"})
	r.MustRegister(mockTool{name: "tool2"})

	r.Clear()

	if r.Count() != 0 {
		t.Fatalf("expected 0 tools, got %d", r.Count())
	}
}
