# Agent Orchestrator Architecture

## Overview

The agent orchestrator provides an agentic LLM execution mode that supports tool calling through the Claude Messages API. It maintains backward compatibility with the existing `llm.Runner` interface.

## Package Structure

```
pkg/
├── llm/
│   ├── types.go      # Message types (ContentBlock, Message, AgentRequest/Response)
│   └── agent.go      # AgentRunner for Claude Messages API
│
├── orchestrator/
│   ├── orchestrator.go  # Orchestrator interface
│   ├── loop.go          # AgentLoop implementation
│   ├── state.go         # Conversation state management
│   └── runner.go        # OrchestratorRunner (llm.Runner adapter)
│
├── tools/
│   ├── interface.go     # Tool interface, ToolResult
│   ├── registry.go      # Tool registration
│   ├── context.go       # ToolContext with permissions
│   └── builtin/         # Built-in tools (file, bash, git, github)
│
└── mcp/
    ├── protocol.go      # JSON-RPC and MCP types
    ├── transport.go     # StdioTransport
    ├── client.go        # MCPClient
    └── server.go        # MCPServer, MCPTool wrapper
```

## Key Types

### llm.Message
Represents a conversation message with multiple content blocks:
```go
type Message struct {
    Role    Role           `json:"role"`    // "user" or "assistant"
    Content []ContentBlock `json:"content"`
}
```

### llm.ContentBlock
Can be text, tool_use, or tool_result:
```go
type ContentBlock struct {
    Type      ContentType            `json:"type"`
    Text      string                 `json:"text,omitempty"`
    ID        string                 `json:"id,omitempty"`        // tool_use
    Name      string                 `json:"name,omitempty"`      // tool_use
    Input     map[string]interface{} `json:"input,omitempty"`     // tool_use
    ToolUseID string                 `json:"tool_use_id,omitempty"` // tool_result
    Content   string                 `json:"content,omitempty"`   // tool_result
    IsError   bool                   `json:"is_error,omitempty"`  // tool_result
}
```

### tools.Tool Interface
```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() map[string]any
    Execute(ctx context.Context, toolCtx *ToolContext, input map[string]any) (ToolResult, error)
}
```

### tools.ToolContext
Provides execution context with permissions:
```go
type ToolContext struct {
    WorkDir     string
    Permissions Permissions
    GitHubToken string
    RepoOwner   string
    RepoName    string
    Env         map[string]string
    BashTimeout int
}
```

## Agent Loop Flow

```
Initialize Context (system prompt + repo instructions + tools)
         │
         ▼
    Call LLM API ◄────────────────────┐
         │                            │
         ▼                            │
  stop_reason == "end_turn"? ──Yes──► Return final response
         │ No (tool_use)              │
         ▼                            │
  Extract tool_use blocks             │
         │                            │
         ▼                            │
  Execute tools (local + MCP)         │
         │                            │
         ▼                            │
  Build tool_result messages          │
         │                            │
         ▼                            │
  iterations < max? ──Yes─────────────┘
         │ No
         ▼
  Force completion / return error
```

## Backward Compatibility

`OrchestratorRunner` implements `llm.Runner`:
```go
func (r *OrchestratorRunner) Run(ctx context.Context, req llm.Request, workDir string) (llm.RunResult, error)
```

This allows the existing `workflow.Engine` to use the orchestrator without changes.

## MCP Integration

MCP servers are started as subprocesses with stdio transport:
1. `NewMCPServer()` starts the subprocess
2. `Initialize()` performs the MCP handshake
3. `ListTools()` retrieves available tools
4. Tools are wrapped as `MCPTool` implementing `tools.Tool`
5. Registered in the same `tools.Registry` as built-in tools

## Configuration

Enable agent mode:
```bash
export AGENT_MODE=true
export AGENT_MAX_ITERATIONS=50
export AGENT_MAX_TOKENS=4096
export TOOLS_ENABLED=true
```

Add MCP servers (JSON format):
```bash
export MCP_SERVERS='[{"name":"fs","command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/"]}]'
```

## Built-in Tools

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_file` | Write content to file |
| `list_files` | List directory contents |
| `bash` | Execute bash commands |
| `git_status` | Show git status |
| `git_diff` | Show git diff |
| `git_log` | Show commit history |
| `git_add` | Stage files |
| `git_commit` | Create commit |
| `git_branch` | Manage branches |
| `github_get_issue` | Get issue details |
| `github_create_comment` | Create issue comment |
| `github_list_issues` | List repository issues |

## Security

- File operations are constrained to WorkDir via `ValidatePath()`
- Bash commands are validated against dangerous patterns
- Bash execution has configurable timeout (default: 60s, max: 300s)
- Permissions can be restricted per ToolContext

## Message History Truncation

The orchestrator truncates message history when it exceeds `AGENT_MAX_MESSAGES` (default: 40). The truncation logic in `loop.go:truncateMessages()` preserves tool_use/tool_result pairs to avoid Claude API errors.

### Algorithm
1. Always keep the first message (initial prompt)
2. Calculate `keepFrom` index based on maxMessages
3. Collect all tool_use IDs from messages to keep
4. For each tool_result in kept messages:
   - If its tool_use_id is not in the kept set, move `keepFrom` back to include the corresponding tool_use
5. Ensure no orphaned tool_results at the start of kept messages
6. Return first message + messages from keepFrom to end

### Example
```
Messages: [0:user, 1:assistant+tool_use(id1), 2:user+tool_result(id1), 3:assistant+tool_use(id2), 4:user+tool_result(id2), 5:assistant]
maxMessages = 4

Initial keepFrom = 6 - 4 + 1 = 3
Messages to keep initially: [0, 3, 4, 5]

Check: tool_result(id2) at index 4 references tool_use(id2) at index 3 ✓ (id2 in kept)

Result: [0, 3, 4, 5] = 4 messages
```

## Context Compaction (Summarization)

When enabled, compaction summarizes long conversations instead of simple truncation. This preserves important context while reducing token usage.

### Configuration
```bash
COMPACT_ENABLED=true        # Enable compaction
COMPACT_THRESHOLD=30        # Trigger when messages > 30
COMPACT_KEEP_RECENT=10      # Keep last 10 messages
```

### How It Works
1. When `len(messages) > COMPACT_THRESHOLD`, compaction is triggered
2. Messages are split: first message + middle (to summarize) + recent messages
3. LLM generates a structured summary of the middle section
4. Result: [first message, summary message, recent messages]

### Summary Format
The summary includes:
- Original task/goal
- Key decisions made
- Files modified
- Current state
- Pending work
- Important context

### Files
- `pkg/orchestrator/compact.go` - Compactor implementation
- `pkg/orchestrator/compact_test.go` - Unit tests

### Flow
```
Messages exceed threshold?
         │ Yes
         ▼
Split: [first] + [middle] + [recent]
         │
         ▼
Generate summary of [middle] via LLM
         │
         ▼
Result: [first] + [summary] + [recent]
```

## Repository Instructions

The orchestrator reads `CLAUDE.md` or `AGENT.md` from the repository root:

```go
// readRepoInstructions in loop.go
func readRepoInstructions(workDir string) string {
    files := []string{"CLAUDE.md", "AGENT.md"}
    // Returns first file found, or empty string
}
```

Content is injected into the system prompt as:
```
## Repository Instructions

<content of CLAUDE.md or AGENT.md>
```
