---
# Ruflo MCP Server
# Shared stdio MCP wrapper for Ruflo multi-agent orchestration.
#
# Docs:
# - https://github.com/ruvnet/ruflo
# - https://github.com/ruvnet/ruflo/blob/main/docs/USERGUIDE.md
#
# Usage:
#   imports:
#     - shared/mcp/ruflo.md

mcp-servers:
  ruflo:
    command: "npx"
    args: ["-y", "ruflo@latest", "mcp", "start", "--transport", "stdio"]
    allowed:
      - memory_search
      - memory_store
      - swarm_init
      - swarm_status
      - agent_spawn
      - agent_list
      - task_orchestrate
      - task_status
---

<!--
Ruflo MCP shared configuration for GitHub Agentic Workflows.

Why stdio instead of Docker:
- Ruflo's published setup guidance centers on `npx ruflo@latest mcp start`
- No official GHCR/Docker image for the MCP server was identified from upstream docs

Runtime requirements:
- Node.js 24+ (the generated workflow currently runs on Node.js 24)
- Network access to npm (`node` ecosystem) for first-run package resolution

Selected allowlist:
- memory_search / memory_store for bounded memory usage
- swarm_init / swarm_status for swarm lifecycle
- agent_spawn / agent_list for role coordination
- task_orchestrate / task_status for work routing and monitoring

Usage:
  imports:
    - shared/mcp/ruflo.md
-->
