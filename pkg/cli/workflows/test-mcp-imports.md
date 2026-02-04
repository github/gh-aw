---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot

imports:
  - shared/mcp/test-server.md

tools:
  github:
    allowed: ["get_repository", "list_commits"]
---

# Test MCP Imports

This workflow imports shared MCP server configuration to test that `mcp inspect` properly processes imports.

The workflow should have access to both the github MCP server (defined here) and any MCP servers imported from shared files.
