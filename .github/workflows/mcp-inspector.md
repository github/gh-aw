---
description: Inspects MCP (Model Context Protocol) server configurations and validates their functionality
on:
  schedule: weekly on monday around 18:00
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
engine: copilot
network:
  allowed:
    - defaults
    - containers
    - node
    - "cdn.jsdelivr.net"        # npm package CDN
    - "fonts.googleapis.com"    # Google Fonts API
    - "fonts.gstatic.com"       # Google Fonts static content
sandbox:
  agent: awf  # Firewall enabled (migrated from network.firewall)
safe-outputs:
  create-discussion:
    category: "audits"
    max: 1
    close-older-discussions: true
timeout-minutes: 20
strict: true
imports:
  - shared/mood.md
  - shared/mcp/arxiv.md
  - shared/mcp/ast-grep.md
  # Note: azure.md excluded due to schema validation issue with entrypointArgs
  - shared/mcp/brave.md
  - shared/mcp/context7.md
  - shared/mcp/datadog.md
  - shared/mcp/deepwiki.md
  - shared/mcp/fabric-rti.md
  - shared/mcp/markitdown.md
  - shared/mcp/microsoft-docs.md
  - shared/mcp/notion.md
  - shared/mcp/sentry.md
  - shared/mcp/server-memory.md
  - shared/mcp/slack.md
  - shared/mcp/tavily.md
  - shared/reporting.md
tools:
  agentic-workflows:
  serena: ["go"]
  edit:
  bash: true
  cache-memory: true
---

# MCP Inspector Agent

Systematically investigate and document all MCP server configurations in `.github/workflows/shared/mcp/*.md`.

## Mission

For each MCP configuration file:
1. Read the file in `.github/workflows/shared/mcp/`
2. Extract: server name, type (http/container/local), tools, secrets required
3. Document configuration status and any issues

Generate:

```markdown
# 🔍 MCP Inspector Report - [DATE]

## Summary
- **Servers Inspected**: [NUMBER]  
- **By Type**: HTTP: [N], Container: [N], Local: [N]

## Inventory Table

| Server | Type | Tools | Secrets | Status |
|--------|------|-------|---------|--------|
| [name] | [type] | [count] | [Y/N] | [✅/⚠️/❌] |

## Details

### [Server Name]
- **File**: `shared/mcp/[file].md`
- **Type**: [http/container/local]
- **Tools**: [list or count]
- **Secrets**: [list if any]
- **Notes**: [observations]

[Repeat for all servers]

## Recommendations
1. [Issue or improvement]
```

Save to `/tmp/gh-aw/cache-memory/mcp-inspections/[DATE].json` and create discussion in "audits" category.