---
"gh-aw": minor
---

Route `engine: claude` workflows with `model: copilot/claude-...` through GitHub Copilot's native Messages API. Use `permissions: { copilot-requests: write }` for Actions-token inference without an Anthropic API key or PAT. Correct credential isolation for explicit GitHub-provider configurations and add a canary for CAPI inference and tool round-trips.
