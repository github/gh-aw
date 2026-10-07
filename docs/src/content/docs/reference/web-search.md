---
title: Web Search
description: How to add web search capabilities to GitHub Agentic Workflows using Tavily MCP server.
sidebar:
  order: 15
---

Use the Tavily Model Context Protocol (MCP) server to add web search to workflows. Alternatives such as Exa, SerpAPI, and Brave Search also exist, but this page covers Tavily.

Copilot's native `web_search` tool is unavailable in offline BYOK mode, including SDK mode. `gh aw compile` rejects `tools.web-search` for Copilot; configure an MCP server as shown below instead.

## Tavily Search

[Tavily](https://tavily.com/) provides structured JSON search results, news search, and an MCP server at [@tavily/mcp](https://github.com/tavily-ai/tavily-mcp).

```aw wrap
---
on: issues

engine: copilot

mcp-servers:
  tavily:
    command: npx
    args: ["-y", "@tavily/mcp"]
    env:
      TAVILY_API_KEY: "${{ secrets.TAVILY_API_KEY }}"
    allowed: ["search", "search_news"]
---

# Search and Respond

Search the web for information about: ${{ github.event.issue.title }}

Use the tavily search tool to find recent information.
```

### Setup

Sign up at [tavily.com](https://tavily.com/) to get an API key, then add it as a repository secret:

```bash
gh aw secrets set TAVILY_API_KEY --value "<your-api-key>"
```

Review the [Tavily Terms of Service](https://tavily.com/terms), then test the workflow with `gh aw mcp inspect <workflow>`.

## Tool Discovery

Inspect the workflow to see which Tavily tools are available:

```bash wrap
# Inspect the MCP server in your workflow
gh aw mcp inspect my-workflow --server tavily

# List tools with details
gh aw mcp list-tools tavily my-workflow --verbose
```

## Network Permissions

Agentic workflows require explicit network permissions for MCP servers:

```yaml wrap
network:
  allowed:
    - defaults
    - "*.tavily.com"
```

## Learn More

- [MCP Integration](/gh-aw/guides/mcps/) - Complete MCP server guide
- [Tools](/gh-aw/reference/tools/) - Tool configuration reference
- [CLI Commands](/gh-aw/setup/cli/) - CLI commands including `mcp inspect`
- [Model Context Protocol Specification](https://github.com/modelcontextprotocol/specification)
- [Tavily MCP Server](https://github.com/tavily-ai/tavily-mcp)
- [Tavily Documentation](https://tavily.com/)
