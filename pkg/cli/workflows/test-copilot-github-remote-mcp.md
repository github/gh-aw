---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
tools:
  github:
    mode: remote
    allowed: [get_repository, list_issues, issue_read]
---

# Test Copilot with GitHub Remote MCP

This is a test workflow to verify Copilot's ability to use the hosted GitHub MCP server in remote mode.

Please use the remote GitHub MCP server to:
1. Get information about this repository (github/gh-aw)
2. List the first 3 open issues
3. Get details for issue #1 if it exists

The workflow uses `mode: remote` to connect to the hosted GitHub MCP server at https://api.githubcopilot.com/mcp/ with GH_AW_GITHUB_TOKEN for authentication.
