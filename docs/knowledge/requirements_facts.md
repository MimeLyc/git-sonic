# Requirements Facts

- Primary requirements are defined in `docs/specs/1_webhook_auto_pr/requirements.md`.
- Webhook events in scope: issue label add, issue comment, PR comment.
- Trigger labels are configurable; default progress labels are `ai/in-progress` and `ai/done`.
- PR body must include: issue description, requirements analysis, solution overview, change summary, tests/risk, manual tests.
- PR comment slash command (example `/ai-optimize`) triggers optimization and must be noted in PR body.
- LLM providers must support base URL + API key + model (including OpenRouter), synchronous API calls, retries (5 attempts, exponential backoff with jitter starting at 10s), usage/cost recording, and response rewrapping into the internal JSON contract.
- Each run executes in an isolated, disposable job environment scheduled by the webhook controller.
