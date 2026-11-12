---
"gh-aw": patch
---

Exclude safe-output message IDs and dependency metadata from connector arguments so Linear, Jira, and other connector effects pass strict input validation.

Store connector plans as human-reviewable JSON instead of base64 in workflow environment variables, and load them from JavaScript rather than embedding them in executable source. Preserve digest verification, expression safety, and compatibility with legacy snapshots.

Escape all dollar signs in JSON snapshots so rendered MCP configuration preserves GraphQL variables and frozen-plan digests.
