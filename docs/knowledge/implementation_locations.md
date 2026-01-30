# Implementation Locations

## Entry Point
- `cmd/git-sonic/main.go`: loads config, sets up allowlist, queue, workflow engine, and HTTP server.

## Core Libraries
- `pkg/config/config.go`: config parsing, defaults, and validation.
- `pkg/allowlist/allowlist.go`: IP allowlist parsing and matching.
- `pkg/webhook/webhook.go`: webhook event parsing into typed structs.
- `pkg/github/client.go`: GitHub REST API client for issues, PRs, comments, labels.
- `pkg/gitutil/git.go`: git clone/branch/commit/push helpers and token injection.
- `pkg/llm/llm.go`: LLM request/response types, JSON extraction (`extractResponseJSON`), and command runner output normalization.
- `pkg/llm/api.go`: API-based LLM runner and chat response parsing with JSON extraction.
- `pkg/workflow/engine.go`: issue label/comment and PR comment workflows, PR creation, label updates.
- `pkg/queue/queue.go`: worker queue implementation.
- `pkg/server/server.go`: webhook HTTP handler and IP allowlist enforcement.

## LLM Output Handling
- `pkg/llm/llm.go`: `extractResponseJSON` scans output for a JSON object with `decision`, strips non-JSON text, and feeds `ParseResponse`.
- `pkg/llm/llm.go`: `CommandRunner.Run` reads stdout or `OutputPath` and stores sanitized JSON in `RunResult.Stdout`.
- `pkg/llm/api.go`: `parseAPIResponse` extracts JSON from chat content and stores sanitized output in `RunResult.Stdout`.
- `pkg/workflow/engine.go`: `writeArtifacts` writes `llm_output.json` from `RunResult.Stdout`.

## Tests
- `tests/contract/*.go`: contract tests for webhook parsing and LLM response parsing.
- `tests/integration/github_client_test.go`: GitHub client integration test with httptest.
- `tests/e2e/issue_flow_test.go`: workflow e2e test with fake dependencies.
- `tests/unit/*.go`: unit tests for config and allowlist.

## Operational Assets
- `README.md`: local testing and Kubernetes deployment steps.
- `Makefile`: build, test, format, and run targets.
- `deploy/k8s/configmap.yaml`: default runtime configuration.
- `deploy/k8s/secret.yaml`: GitHub token and LLM command secrets.
- `deploy/k8s/deployment.yaml`: deployment spec with workdir volume.
- `deploy/k8s/service.yaml`: ClusterIP service for the webhook.
