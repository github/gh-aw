---
"gh-aw": patch
---

Upgrade the MCP Gateway (`gh-aw-mcpg`) Docker image default from `v0.4.16` to `v0.4.17`, updating the pinned immutable digest and recompiling all generated workflow manifests, download steps, and CLI proxy references. `v0.4.17` ships `github/gh-aw-mcpg#12605`, which implements the `github-repository-delegation-v1` atomic bootstrap contract that dynamic agent enclaves consume, so dynamic repository enclaves now fail closed on any pinned mcpg older than `v0.4.17`.
