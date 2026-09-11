---
title: Using MCPs
description: How to use Model Context Protocol (MCP) servers with GitHub Agentic Workflows to connect AI agents to GitHub, databases, and external services.
sidebar:
  order: 2
---

[Model Context Protocol](/gh-aw/reference/glossary/#mcp-model-context-protocol) (MCP) is a standard for AI tool integration, allowing agents to securely connect to external tools, databases, and services. GitHub Agentic Workflows includes built-in GitHub MCP integration and supports custom MCP servers for external services.

## Quick Start

Get your first MCP integration running in a few minutes.

### 1. Add GitHub Tools

Create a workflow file at `.github/workflows/my-workflow.md`:

```aw wrap
---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  issues: read

tools:
  github:
    toolsets: [default]
---

# Issue Analysis Agent

Analyze the issue and provide a summary of similar existing issues.
```

The `toolsets: [default]` configuration gives your agentic workflow access to repository, issue, and pull request tools.

### 2. Compile and Test

```bash
gh aw compile my-workflow
gh aw mcp inspect my-workflow
```

## GitHub MCP Server

The GitHub MCP server is built into agentic workflows and provides read-only access to GitHub's API. Write actions still go through [safe outputs](/gh-aw/reference/safe-outputs/).

### Available Toolsets

| Toolset | Purpose | Example tools |
|---------|---------|---------------|
| `context` | User and team information | `get_teams`, `get_team_members` |
| `repos` | Repository operations | `get_repository`, `get_file_contents`, `list_commits` |
| `issues` | Issue management | `list_issues`, `create_issue`, `update_issue` |
| `pull_requests` | PR operations | `list_pull_requests`, `create_pull_request` |
| `actions` | Workflow runs and artifacts | `list_workflows`, `list_workflow_runs` |
| `discussions` | GitHub Discussions | `list_discussions`, `create_discussion` |
| `code_security` | Security alerts | `list_code_scanning_alerts` |
| `users` | User profiles | `get_me` ⚠️, `get_user`, `list_users` |

The `default` toolset includes `context`, `repos`, `issues`, and `pull_requests`. It expands to the toolsets supported by GitHub Actions tokens, so `users` is excluded.

When calling `list_code_scanning_alerts` from workflow prompts, always bound the request with `state: open` and `severity: critical,high`.

### Operating Modes

Use `mode: remote` to connect to the hosted server with no Docker requirement. Use `mode: local` to run in Docker when you need local version pinning or support for restricted environments. See [Remote vs Local Mode](/gh-aw/reference/github-tools/#github-tools-access-modes).

## Manually Configuring a Custom MCP Server

Add MCP servers to your workflow's frontmatter using the `mcp-servers:` section:

```aw wrap
---
on: issues

permissions:
  contents: read

mcp-servers:
  microsoftdocs:
    url: "https://learn.microsoft.com/api/mcp"
    allowed: ["*"]
  
  notion:
    container: "mcp/notion"
    env:
      NOTION_TOKEN: "${{ secrets.NOTION_TOKEN }}"
    allowed:
      - "search_pages"
      - "get_page"
      - "get_database"
      - "query_database"
---

# Your workflow content here
```

Custom MCP servers should be **read-only**. Write operations must go through [safe outputs](/gh-aw/reference/safe-outputs/) or [Custom Safe Outputs](/gh-aw/reference/custom-safe-outputs/). Ensure your MCP server implements authentication and authorization to prevent unauthorized write access.

## Custom MCP Server Types

Choose the transport that matches how the server runs:

- `command` for local executables over stdio
- `container` for packaged local servers
- `url` for remote HTTP endpoints
- `registry` when you want to attach registry metadata to a server definition

### Stdio MCP Servers

Use stdin/stdout communication for Python modules, Node.js scripts, and local executables:

```yaml wrap
mcp-servers:
  serena:
    command: "uvx"
    args: ["--from", "git+https://github.com/oraios/serena", "serena"]
    allowed: ["*"]
```

### Docker Container MCP Servers

Run containerized MCP servers with environment variables, volume mounts, and network restrictions:

```yaml wrap
mcp-servers:
  custom-tool:
    container: "mcp/custom-tool:v1.0"
    args: ["-v", "/host/data:/app/data"]  # Volume mounts before image
    entrypointArgs: ["serve", "--port", "8080"]  # App args after image
    env:
      API_KEY: "${{ secrets.API_KEY }}"
    allowed: ["tool1", "tool2"]

network:
  allowed:
    - defaults
    - api.example.com
```

The `container` field generates `docker run --rm -i <args> <image> <entrypointArgs>`.

### HTTP MCP Servers

Remote MCP servers accessible via HTTP. Configure authentication using the `headers` field for static API keys, or the `auth` field for dynamic token acquisition:

```yaml wrap
mcp-servers:
  deepwiki:
    url: "https://mcp.deepwiki.com/sse"
    allowed:
      - read_wiki_structure
      - read_wiki_contents
      - ask_question

  authenticated-api:
    url: "https://api.example.com/mcp"
    headers:
      Authorization: "Bearer ${{ secrets.API_TOKEN }}"
    allowed: ["*"]
```

#### GitHub Actions OIDC Authentication

For MCP servers that accept GitHub Actions OIDC tokens, use the `auth` field instead of a static `headers` value. The gateway acquires a short-lived JWT from the GitHub Actions OIDC endpoint and injects it as an `Authorization: Bearer` header on every outgoing request.

```yaml wrap
permissions:
  id-token: write   # required for OIDC token acquisition

mcp-servers:
  my-secure-server:
    url: "https://my-server.example.com/mcp"
    auth:
      type: github-oidc
      audience: "https://my-server.example.com"  # optional; defaults to the server URL
    allowed: ["*"]
```

The `auth.type: github-oidc` field is only valid on HTTP servers. The MCP server is responsible for validating the token; the gateway acts as a token forwarder. See [MCP Gateway — Upstream Authentication](/gh-aw/reference/mcp-gateway/#76-upstream-authentication-oidc) for full specification details.

### Registry-based MCP Servers

Reference MCP servers from the GitHub MCP registry (the `registry` field provides metadata for tooling and is not enforced by gh-aw):

```yaml wrap
mcp-servers:
  markitdown:
    registry: https://api.mcp.github.com/v0/servers/microsoft/markitdown
    container: "ghcr.io/microsoft/markitdown"
    allowed: ["*"]
```

## MCP Tool Filtering

Use `allowed:` to expose only the tools a workflow needs, or `["*"]` to allow all:

```yaml wrap
mcp-servers:
  notion:
    container: "mcp/notion"
    allowed: ["search_pages", "get_page"]  # or ["*"] to allow all
```

The `allowed:` filter is enforced at the **MCP gateway level** — the gateway only exposes the listed tools to the agent. This enforcement applies regardless of which AI engine or permission mode is in use.

## Shared MCP Configurations

Pre-configured MCP server specifications are available in [`.github/workflows/shared/mcp/`](https://github.com/github/gh-aw/tree/main/.github/workflows/shared/mcp) and can be copied or imported directly:

| MCP Server | Import Path | Key Capabilities |
|------------|-------------|------------------|
| **Jupyter** | `shared/mcp/jupyter.md` | Execute code, manage notebooks, visualize data |
| **AgentDB** | `shared/mcp/agentdb.md` | Semantic and hybrid retrieval over agent-collected corpora (e.g. discussions, issues), backed by a runtime store at `AGENTDB_PATH` |
| **Azure Auth (OIDC bridge)** | `shared/azure-auth.md` | Re-authenticate Azure CLI inside the agent sandbox using GitHub OIDC |
| **Azure DevOps MCP** *(experimental)* | `shared/mcp/azure-devops.md` | Azure DevOps MCP endpoint with org-scoped URL, auth header, and required domains |
| **Azure MCP** | `shared/mcp/azure.md` | Azure MCP server in read-only mode with an explicit tool allowlist |
| **Others** | `shared/mcp/*.md` | AST-Grep, Azure, Brave Search, Context7, DataDog, DeepWiki, Fabric RTI, MarkItDown, Microsoft Docs, Notion, Sentry, Serena, Server Memory, Slack, Tavily |

### Azure shared imports (OIDC, Azure DevOps, Azure MCP)

Use these shared imports together when your workflow needs Azure CLI auth plus
Azure DevOps and Azure MCP tools:

```aw wrap
---
permissions:
  contents: read
  id-token: write

imports:
  - uses: shared/azure-auth.md
    with:
      azure-client-id: ${{ vars.AZURE_CLIENT_ID }}
      azure-tenant-id: ${{ vars.AZURE_TENANT_ID }}
  - uses: shared/mcp/azure-devops.md
    with:
      organization: YOUR_ORG

mcp-servers:
  azure:
    command: npx
    args:
      - -y
      - "@azure/mcp@latest"
      - server
      - start
      - --read-only
    allowed:
      - subscription_list
      - subscription_get
      - group_list
      - group_get
      - resource_list
      - resource_get
---
```

`shared/azure-auth.md` sets `AZURE_CONFIG_DIR=/tmp/gh-aw/agent/.azure` and runs `az login` in a pre-agent step so `DefaultAzureCredential` can resolve `AzureCliCredential` inside the sandbox.

Azure DevOps MCP support (`shared/mcp/azure-devops.md`) is still experimental, and its interfaces and required configuration may change. Set `ADO_MCP_AUTH_TOKEN` to the full `Authorization` header value, such as a bearer token string. In diagnostics and inspect output, the header is masked as `Authorization: ******`; this is expected.

This shared Azure DevOps configuration also requires `*.dev.azure.com`, `*.visualstudio.com`, and `*.microsoftonline.com` in the network allowlist.

Keep the command-based Azure MCP variant read-only with an explicit `allowed` list; do not switch to `allowed: ["*"]`.

## Adding MCP Servers from the Registry

Use `gh aw mcp add` to browse and add servers from the GitHub MCP registry (default: `https://api.mcp.github.com/v0`):

```bash wrap
gh aw mcp add                                                                    # List available servers
gh aw mcp add my-workflow makenotion/notion-mcp-server                           # Add server
gh aw mcp add my-workflow makenotion/notion-mcp-server --transport stdio         # Specify transport
gh aw mcp add my-workflow makenotion/notion-mcp-server --tool-id my-notion       # Custom tool ID
gh aw mcp add my-workflow server-name --registry https://custom.registry.com/v1  # Custom registry
```

## Practical Examples

### Example 1: Basic Issue Triage

```aw wrap
---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  issues: read

tools:
  github:
    toolsets: [default]

safe-outputs:
  add-comment:
---

# Issue Triage Agent

Analyze issue #${{ github.event.issue.number }} and add a comment with category, related issues, and suggested labels.
```

### Example 2: Security Audit with Discussions

```aw wrap
---
on: weekly on sunday

permissions:
  contents: read
  security-events: read
  discussions: write

tools:
  github:
    toolsets: [default, code_security, discussions]

safe-outputs:
  create-discussion:
    category: "Security"
    title-prefix: "[security-scan] "
---

# Security Audit Agent

Review code scanning alerts and create weekly security discussions with findings.
```

## Debugging and Troubleshooting

Inspect MCP configurations with `gh aw mcp inspect my-workflow` or `gh aw mcp list-tools <server> my-workflow`. Add `--server <name> --verbose` when you need per-server details.

For deeper diagnostics, import `shared/mcp-debug.md` to access diagnostic tools and the `report_diagnostics_to_pull_request` custom safe-output.

Most failures fall into two buckets:

- connection problems caused by syntax, environment variable, or network issues
- missing tools caused by the configured toolsets or `allowed` list

In both cases, `gh aw mcp inspect` is the fastest way to confirm what the workflow exposes.

## Learn More

- [MCP Scripts](/gh-aw/reference/mcp-scripts/) for inline tools without external MCP servers
- [Tools](/gh-aw/reference/tools/) for the full tools reference
- [CLI Commands](/gh-aw/setup/cli/) for commands such as `mcp inspect`
- [Imports](/gh-aw/reference/imports/) for modular workflow composition
- [Frontmatter](/gh-aw/reference/frontmatter/) for configuration details
- [Workflow Structure](/gh-aw/reference/workflow-structure/) for directory layout
- [Model Context Protocol Specification](https://github.com/modelcontextprotocol/specification)
- [GitHub MCP Server](https://github.com/github/github-mcp-server)
