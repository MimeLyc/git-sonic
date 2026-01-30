# git-sonic

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Autonomous GitHub issue resolver powered by LLM agents. git-sonic listens to GitHub webhooks, automatically analyzes issues, generates fixes, and creates pull requests.

## Features

- **Webhook-driven automation** — Responds to GitHub issue labels, issue comments, and PR review comments
- **Multi-agent support** — Works with Claude API, OpenAI API, or CLI agents (Claude Code, aider)
- **Built-in tools** — File operations, bash execution, git commands, GitHub API
- **MCP integration** — Extend capabilities via Model Context Protocol servers
- **Kubernetes ready** — Includes deployment manifests and Docker support

## Quick Start

### Prerequisites

- Go 1.21+
- Git
- GitHub personal access token with `repo` scope
- LLM API key (Anthropic or OpenAI) or Claude Code CLI

### Installation

```bash
git clone https://github.com/pingcap/git-sonic.git
cd git-sonic
make build
```

### Run with Claude API

```bash
export GITHUB_TOKEN=ghp_xxx
export LLM_API_BASE_URL=https://api.anthropic.com
export LLM_API_KEY=sk-ant-xxx
export LLM_API_MODEL=claude-sonnet-4-20250514
export AGENT_TYPE=api

./bin/git-sonic
```

### Run with Claude Code CLI

```bash
export GITHUB_TOKEN=ghp_xxx
export AGENT_TYPE=cli
export CLI_COMMAND=claude

./bin/git-sonic
```

## How It Works

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   GitHub    │────►│  git-sonic  │────►│  LLM Agent  │────►│  Create PR  │
│  (webhook)  │     │  (server)   │     │  (analyze)  │     │  (commit)   │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

1. Add `ai-ready` label to an issue
2. git-sonic receives webhook, clones repo, creates branch
3. LLM agent analyzes issue and generates code changes
4. Changes are committed and pushed as a pull request
5. Issue is updated with PR link and `ai-done` label

### Label State Machine

| Current | Event | Next |
|---------|-------|------|
| — | Add `ai-ready` | `ai-in-progress` |
| `ai-in-progress` | Success | `ai-done` |
| `ai-in-progress` | Needs info | `ai-needs-info` |
| `ai-needs-info` | User comments | `ai-in-progress` |

## Configuration

### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | HTTP server address |
| `WEBHOOK_PATH` | `/webhook` | Webhook endpoint path |
| `GITHUB_TOKEN` | required | GitHub token with repo access |
| `REPO_CLONE_BASE` | `./workdir` | Working directory for clones |
| `MAX_WORKERS` | `2` | Concurrent job workers |

### Labels

| Variable | Default | Description |
|----------|---------|-------------|
| `TRIGGER_LABELS` | `ai-ready` | Labels that trigger automation |
| `IN_PROGRESS_LABEL` | `ai-in-progress` | Processing in progress |
| `NEEDS_INFO_LABEL` | `ai-needs-info` | More information needed |
| `DONE_LABEL` | `ai-done` | Processing complete |

### Agent Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_TYPE` | `api` | Agent type: `api`, `cli`, `claude-code`, `auto` |
| `LLM_PROVIDER_TYPE` | `claude` | Provider: `claude` or `openai` |
| `LLM_TIMEOUT` | `30m` | Agent execution timeout |

#### API Mode

| Variable | Default | Description |
|----------|---------|-------------|
| `LLM_API_BASE_URL` | — | API endpoint (e.g., `https://api.anthropic.com`) |
| `LLM_API_KEY` | — | API key |
| `LLM_API_MODEL` | — | Model name |
| `LLM_API_MAX_ATTEMPTS` | `5` | Retry attempts |
| `AGENT_MAX_ITERATIONS` | `50` | Max tool call iterations |
| `AGENT_MAX_TOKENS` | `4096` | Max tokens per response |

#### CLI Mode

| Variable | Default | Description |
|----------|---------|-------------|
| `CLI_COMMAND` | `claude` | CLI binary path |
| `CLI_ARGS` | — | Additional CLI arguments |

### Advanced

| Variable | Default | Description |
|----------|---------|-------------|
| `IP_ALLOWLIST` | — | Allowed IPs/CIDRs (comma-separated) |
| `TOOLS_ENABLED` | `true` | Enable built-in tools |
| `MCP_SERVERS` | — | MCP server configs (JSON) |
| `COMPACT_ENABLED` | `false` | Enable context compaction |
| `COMPACT_THRESHOLD` | `30` | Message count before compaction |

## Development

### Run Tests

```bash
make test
```

### Local Testing with Mock LLM

```bash
chmod +x scripts/mock_llm.sh
export GITHUB_TOKEN=your_token
export LLM_COMMAND=./scripts/mock_llm.sh
make run
```

### Send Test Webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H 'X-GitHub-Event: issues' \
  -H 'X-GitHub-Delivery: test-1' \
  -d '{
    "action": "labeled",
    "label": {"name": "ai-ready"},
    "issue": {
      "number": 1,
      "state": "open",
      "title": "Test issue",
      "body": "Fix the bug",
      "labels": [{"name": "ai-ready"}]
    },
    "repository": {
      "full_name": "org/repo",
      "clone_url": "https://github.com/org/repo.git",
      "default_branch": "main"
    },
    "sender": {"login": "user"}
  }'
```

### Forward Real Webhooks

Use GitHub CLI to forward webhooks to your local server:

```bash
gh webhook forward --repo OWNER/REPO --events issues,issue_comment --url http://localhost:8080/webhook
```

## Deployment

### Docker

```bash
# Build
make docker-build

# Run
make docker-run
```

Or manually:

```bash
docker build -t git-sonic .
docker run --rm --env-file .env -p 8080:8080 git-sonic
```

### Kubernetes

Manifests are in `deploy/k8s/`:

```bash
# Update secrets
kubectl create secret generic git-sonic-secrets \
  --from-literal=github-token=ghp_xxx \
  --from-literal=llm-api-key=sk-xxx

# Deploy
kubectl apply -f deploy/k8s/

# Verify
kubectl get pods -l app=git-sonic
kubectl logs -l app=git-sonic -f
```

### GitHub Webhook Setup

1. Go to repo **Settings → Webhooks → Add webhook**
2. Set Payload URL to your server endpoint
3. Content type: `application/json`
4. Select events: `Issues`, `Issue comments`, `Pull request review comments`

## Architecture

```
git-sonic/
├── cmd/git-sonic/       # Entry point
├── pkg/
│   ├── agent/           # Unified agent interface (API + CLI)
│   ├── config/          # Configuration loading
│   ├── github/          # GitHub API client
│   ├── gitutil/         # Git operations
│   ├── llm/             # LLM providers (Claude, OpenAI)
│   ├── mcp/             # MCP server integration
│   ├── orchestrator/    # Agent loop, tool execution
│   ├── queue/           # Job queue
│   ├── server/          # HTTP webhook handler
│   ├── tools/           # Tool interface and builtins
│   ├── webhook/         # Webhook parsing
│   └── workflow/        # Issue/PR workflows
├── deploy/k8s/          # Kubernetes manifests
└── docs/                # Documentation
```

### Built-in Tools

| Tool | Description |
|------|-------------|
| `read_file` | Read file contents |
| `write_file` | Write to file |
| `list_files` | List directory |
| `bash` | Execute commands |
| `git_*` | Git operations |
| `github_*` | GitHub API |

### Repository Instructions

git-sonic reads `CLAUDE.md` or `AGENT.md` from the repository root to customize agent behavior per-repo.

## Troubleshooting

### Check Logs

```bash
# View latest workdir
ls -lt workdir/ | head -5

# Check LLM output
cat workdir/issue-XXX-*/outputs/llm_output.json | jq .

# View git status
cd workdir/issue-XXX-*/repo && git status
```

### Common Issues

| Issue | Solution |
|-------|----------|
| Push fails (non-fast-forward) | Branch already exists; uses timestamp to avoid conflicts |
| No changes detected | LLM didn't generate file changes; check `llm_output.json` |
| Webhook not received | Verify IP allowlist includes webhook source |

## License

Apache 2.0
