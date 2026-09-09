---
"gh-aw": minor
---

Add experimental native Linear safe outputs: `linear-create-issue`, `linear-add-comment`, and `linear-update-issue`. The privileged `safe_outputs` job sanitizes agent-emitted operations and executes fixed GraphQL mutations against a configured Linear team/issue target, keeping the `linear-token` credential out of agent jobs, MCP schemas, and artifacts.
