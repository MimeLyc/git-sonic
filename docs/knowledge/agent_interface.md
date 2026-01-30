# Unified Agent Interface

## Overview

The `pkg/agent` package provides a unified interface for different agent implementations, abstracting the differences between:
- **APIAgent** - Our own agent implementation using LLM APIs with tool support
- **CLIAgent** - External CLI tools (Claude Code, aider, etc.)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Agent Interface                         │
│  Execute(ctx, AgentRequest) → (AgentResult, error)          │
└─────────────────────────────────────────────────────────────┘
          │                              │
          ▼                              ▼
┌─────────────────────┐      ┌─────────────────────────────────┐
│     APIAgent        │      │        CLIAgent                 │
│  (wraps AgentLoop)  │      │  (Claude Code, aider, etc.)    │
└─────────────────────┘      └─────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────┐
│                   LLMProvider Interface                      │
│         Call(ctx, AgentRequest) → (AgentResponse, error)    │
└─────────────────────────────────────────────────────────────┘
          │                              │
          ▼                              ▼
┌─────────────────────┐      ┌─────────────────────────────────┐
│  ClaudeProvider     │      │     OpenAIProvider             │
│  (Claude API)       │      │  (OpenAI/OpenRouter/DeepSeek)  │
└─────────────────────┘      └─────────────────────────────────┘
```

## Key Files

### Core Interface
- `pkg/agent/agent.go:8-19` - Agent interface with Execute, Capabilities, Close
- `pkg/agent/agent.go:21-41` - AgentCapabilities struct

### LLM Provider Interface
- `pkg/llm/provider.go:9-17` - LLMProvider interface
- `pkg/llm/provider.go:19-30` - LLMProviderType constants (claude, openai)
- `pkg/llm/provider.go:32-50` - LLMProviderConfig struct
- `pkg/llm/provider.go:52-64` - NewLLMProvider factory

### Provider Implementations
- `pkg/llm/claude_provider.go:26-37` - ClaudeProvider struct
- `pkg/llm/claude_provider.go:39-62` - NewClaudeProvider constructor
- `pkg/llm/claude_provider.go:69-147` - ClaudeProvider.Call implementation
- `pkg/llm/openai_provider.go:26-37` - OpenAIProvider struct
- `pkg/llm/openai_provider.go:39-62` - NewOpenAIProvider constructor
- `pkg/llm/openai_provider.go:69-145` - OpenAIProvider.Call implementation

### Types
- `pkg/agent/types.go:11-32` - AgentRequest with Task, SystemPrompt, Context, Options
- `pkg/agent/types.go:131-168` - AgentResult with Decision, Summary, FileChanges, ToolCalls
- `pkg/agent/types.go:69-88` - AgentOptions with MaxIterations, Timeout, CompactConfig

### Implementations
- `pkg/agent/api_agent.go:16-29` - APIAgent wrapping LLMProvider and orchestrator
- `pkg/agent/cli_agent.go:64-87` - CLIAgent for external CLI tools
- `pkg/agent/cli_agent.go:159-198` - ClaudeCodeClient for Claude CLI

### Factory
- `pkg/agent/factory.go:16-29` - AgentType constants (api, cli, claude-code, auto)
- `pkg/agent/factory.go:46-81` - APIConfig with ProviderType field
- `pkg/agent/factory.go:83-95` - NewAgent factory function

### Backward Compatibility
- `pkg/llm/agent.go:11-37` - AgentRunner wrapper (deprecated, delegates to ClaudeProvider)
- `pkg/agent/runner_adapter.go:12-27` - RunnerAdapter implementing llm.Runner

## Configuration

Environment variables:
- `AGENT_TYPE` - "api", "cli", "claude-code", or "auto" (default: "api")
- `LLM_PROVIDER_TYPE` - "claude" or "openai" (default: "claude", when AGENT_TYPE=api)
- `CLI_COMMAND` - Path to CLI binary (e.g., "claude", "aider")
- `CLI_ARGS` - Additional arguments for CLI command

### Deprecated (backward compatible)
- `CLAUDE_CODE_PATH` - Use CLI_COMMAND instead
- `CLAUDE_CODE_ARGS` - Use CLI_ARGS instead

## Usage

### Creating an APIAgent with Claude

```go
cfg := agent.AgentConfig{
    Type: agent.AgentTypeAPI,
    API: &agent.APIConfig{
        ProviderType: llm.ProviderClaude, // or "claude"
        BaseURL:      "https://api.anthropic.com",
        APIKey:       "sk-...",
        Model:        "claude-sonnet-4-20250514",
    },
    Registry: toolRegistry,
}

ag, err := agent.NewAgent(cfg)
```

### Creating an APIAgent with OpenAI

```go
cfg := agent.AgentConfig{
    Type: agent.AgentTypeAPI,
    API: &agent.APIConfig{
        ProviderType: llm.ProviderOpenAI, // or "openai"
        BaseURL:      "https://api.openai.com",
        APIKey:       "sk-...",
        Model:        "gpt-4",
    },
    Registry: toolRegistry,
}

ag, err := agent.NewAgent(cfg)
```

### Creating a CLIAgent

```go
cfg := agent.AgentConfig{
    Type: agent.AgentTypeCLI,
    ClaudeCode: &agent.ClaudeCodeConfig{
        Command: "claude",
        Args:    []string{"--no-stream"},
        Timeout: 30 * time.Minute,
    },
}

ag, err := agent.NewAgent(cfg)
```

### Executing a Task

```go
result, err := ag.Execute(ctx, agent.AgentRequest{
    Task:    "Fix the bug in main.go",
    WorkDir: "/path/to/repo",
})
```

### Using LLMProvider Directly

```go
provider, err := llm.NewLLMProvider(llm.LLMProviderConfig{
    Type:    llm.ProviderClaude,
    BaseURL: "https://api.anthropic.com",
    APIKey:  "sk-...",
    Model:   "claude-sonnet-4-20250514",
})

resp, err := provider.Call(ctx, llm.AgentRequest{
    System:   "You are a helpful assistant.",
    Messages: []llm.Message{llm.NewTextMessage(llm.RoleUser, "Hello")},
})
```

### Backward Compatibility

Use RunnerAdapter to maintain compatibility with existing llm.Runner interface:

```go
runner := agent.NewRunnerAdapter(ag, systemPrompt)
// runner implements llm.Runner
```

## Decision Values

- `DecisionProceed` - Changes are ready to commit
- `DecisionNeedsInfo` - More information is needed
- `DecisionStop` - Task cannot be automated

## File Operations

FileChange represents modifications:
- `FileOpCreate` - New file created
- `FileOpModify` - Existing file modified
- `FileOpDelete` - File deleted

## Type Aliases (Backward Compatibility)

The following type aliases are provided for backward compatibility:
- `ExternalAgent` = `CLIAgent`
- `ExternalAgentClient` = `CLIAgentClient`
- `ExternalRequest` = `CLIRequest`
- `ExternalResponse` = `CLIResponse`
- `ExternalAgentConfig` = `CLIAgentConfig`
- `NewExternalAgent` = `NewCLIAgent`
