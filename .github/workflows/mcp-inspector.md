---
emoji: "🔍"
description: Inspects MCP (Model Context Protocol) server configurations and validates their functionality
on:
  schedule: weekly on monday around 18:00
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
network:
  allowed:
    - defaults
    - containers
    - node
    - node-cdns
    - fonts
sandbox:
  agent:
    id: awf
timeout-minutes: 20
strict: false
features:
  gh-aw-detection: true
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[mcp-inspector] "
      expires: 1d
  # Note: arxiv.md excluded — docker.io/mcp/arxiv-mcp-server has Critical/High CVEs (see #51713)
  - shared/mcp/ast-grep.md
  # Note: azure.md excluded due to schema validation issue with entrypointArgs
  # Note: brave.md excluded — docker.io/mcp/brave-search has Critical/High CVEs (see #48546)
  # Note: markitdown.md excluded — docker.io/mcp/markitdown has Critical/High CVEs (see #49515)
  # Note: context7.md removed — docker.io/mcp/context7 has Critical/High CVEs (see #51715)
  - shared/mcp/datadog.md
  - shared/mcp/deepwiki.md
  # Note: fabric-rti.md excluded — its uvx auto-container docker.io/python:alpine has High CVEs in CPython 3.14.7 with no fixed release (see #51711)
  # Note: markitdown.md excluded — docker.io/mcp/markitdown has Critical/High CVEs (see #49515)
  - shared/mcp/microsoft-docs.md
  # Note: notion.md excluded — docker.io/mcp/notion has Critical/High CVEs (see #49517)
  # Note: server-memory.md removed — mcp/memory has Critical/High CVEs and license violations (see #51716)
  - shared/mcp/sentry.md
  - shared/mcp/slack.md
  - shared/mcp/tavily.md
  - shared/mcp/serena-go.md
  - shared/otlp.md
tools:
  cli-proxy: true
  agentic-workflows:
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