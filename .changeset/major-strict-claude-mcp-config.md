---
"gh-aw": major
---

Claude workflows now use strict MCP configuration and no longer load servers from ambient configuration, such as a repository-local `.mcp.json`.

**⚠️ Breaking Change**: MCP servers configured only through ambient Claude configuration will no longer be available to workflows.

**Migration guide:**
- Declare custom MCP servers in the workflow's `mcp-servers:` frontmatter so gh-aw can include them in its explicit MCP configuration.
- Keep using gh-aw's built-in tool configuration for integrations such as GitHub.
