---
tools:
  cache-memory:
    key: agentdb-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}
steps:
  - name: Ensure AgentDB cache path
    run: mkdir -p "/tmp/gh-aw/cache-memory/agentdb-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}"
mcp-servers:
  agentdb:
    command: "npx"
    args: ["agentdb@3.0.0-alpha.20", "mcp", "start"]
    env:
      AGENTDB_PATH: "/tmp/gh-aw/cache-memory/agentdb-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}/discussions.rvf"
    allowed: ["*"]
---

<!--
## AgentDB MCP Server

Shared MCP configuration for AgentDB vector memory/search.

- Docs: https://github.com/ruvnet/agentdb/blob/main/docs/README-full.md
- Launch command from docs: `npx agentdb mcp start`
- Default store path in this workflow runtime: `/tmp/gh-aw/cache-memory/agentdb-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}/discussions.rvf`

Usage:

```yaml
imports:
  - shared/mcp/agentdb.md
```
-->
