---
title: Tools
description: Configure GitHub API tools, browser automation, and AI capabilities available to your agentic workflows, including GitHub tools, Playwright, and custom MCP servers.
sidebar:
  order: 700
---

[Tools](/gh-aw/reference/glossary/#tools) are defined in the frontmatter to specify which GitHub API calls, browser automation, and AI capabilities are available to your workflow:

```yaml wrap
tools:
  edit:
  bash: true
```

Some tools are available by default. All tools declared in imported components are merged into the final workflow.

## Edit Tool (`edit:`)

Allows file editing in the GitHub Actions workspace.

```yaml wrap
tools:
  edit:
```

## Bash Tool (`bash:`)

Enables shell command execution in the workspace. Defaults to safe commands (`echo`, `ls`, `pwd`, `cat`, `head`, `tail`, `grep`, `wc`, `sort`, `uniq`, `date`).

```yaml wrap
tools:
  bash:                              # Default safe commands
  bash: []                           # Disable all commands
  bash: ["echo", "ls", "git status"] # Specific commands only
  bash: [":*"]                       # All commands (use with caution)
```

Use wildcards like `git:*` for command families or `:*` for unrestricted access.

## Web Tools

Enable web content fetching and search capabilities:

```yaml wrap
tools:
  web-fetch:   # Fetch web content
  web-search:  # Search the web (engine-dependent)
```

**Note:** Some engines require third-party Model Context Protocol (MCP) servers for web search. See [Using Web Search](/gh-aw/guides/web-search/).

## GitHub Tools (`github:`)

Configure GitHub API operations.

```yaml wrap
tools:
  github:                                      # Default read-only access
  github:
    toolsets: [repos, issues, pull_requests]   # Recommended: toolset groups
    mode: remote                               # "local" (Docker) or "remote" (hosted)
    read-only: true                            # Read-only operations
    github-token: "${{ secrets.CUSTOM_PAT }}"  # Custom token
```

### GitHub Toolsets

> [!TIP]
> Use Toolsets Instead of Allowed
> [Toolsets](/gh-aw/reference/glossary/#toolsets) (capability collections) provide a stable API across MCP server versions and automatically include new related tools. See [Migration from Allowed to Toolsets](/gh-aw/guides/mcps/#migration-from-allowed-to-toolsets) for guidance.

Enable specific API groups to improve tool selection and reduce context size:

```yaml wrap
tools:
  github:
    toolsets: [repos, issues, pull_requests, actions]
```

**Available**: `context`, `repos`, `issues`, `pull_requests`, `users`, `actions`, `code_security`, `discussions`, `labels`, `notifications`, `orgs`, `projects`, `gists`, `search`, `dependabot`, `experiments`, `secret_protection`, `security_advisories`, `stargazers`

**Default**: `context`, `repos`, `issues`, `pull_requests`, `users`

> [!NOTE]
> GitHub Actions Compatibility
> `toolsets: [default]` expands to `[context, repos, issues, pull_requests]` (excluding `users`) since `GITHUB_TOKEN` lacks user permissions. Use a PAT for the full default set.

**Common combinations**: `[default]` (read-only), `[default, discussions]` (issue/PR), `[default, actions]` (CI/CD), `[default, code_security]` (security), `[all]` (full access)

#### Toolset Contents

Key toolsets: **context** (user/team info), **repos** (repository operations, code search, commits, releases), **issues** (issue management, comments, reactions), **pull_requests** (PR operations), **actions** (workflows, runs, artifacts), **code_security** (scanning alerts), **discussions**, **labels**.

> [!TIP]
> The `allowed:` field is not recommended for GitHub tools since tool names may change between versions. Use `toolsets:` for stability. For custom MCP servers, `allowed:` remains the standard approach.

### Modes and Restrictions

**Remote Mode**: Use hosted MCP server for faster startup (no Docker). Requires `GH_AW_GITHUB_TOKEN`:

```yaml wrap
tools:
  github:
    mode: remote  # Default: "local" (Docker)
```

Setup: `gh aw secrets set GH_AW_GITHUB_TOKEN --value "<your-pat>"`

**Read-Only**: Default behavior; restricts to read operations unless write operations configured.

**Lockdown**: Automatically determined based on repository visibility when using a custom token (`GH_AW_GITHUB_MCP_SERVER_TOKEN`). Filters public repository content to items from users with push access. Private repositories are unaffected.

- **Automatic (default)**: When `GH_AW_GITHUB_MCP_SERVER_TOKEN` is defined, lockdown is automatically enabled for public repositories and disabled for private/internal repositories
- **Manual override**: Explicitly set `lockdown: true` or `lockdown: false` to override automatic determination

```yaml wrap
tools:
  github:
    # Option 1: Automatic (recommended) - determined at runtime
    # Lockdown automatically enabled for public repos when GH_AW_GITHUB_MCP_SERVER_TOKEN is set

    # Option 2: Explicit override
    lockdown: true   # Force enable
    # or
    lockdown: false  # Explicitly disable (use with caution in public repos)
```

See [GitHub Tokens](/gh-aw/reference/tokens/#github-app-tokens-for-github-mcp-server) for security implications.

### GitHub App Authentication

Use GitHub App tokens for enhanced security with short-lived, automatically-revoked credentials:

```yaml wrap
tools:
  github:
    mode: remote
    toolsets: [repos, issues, pull_requests]
    app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      owner: "my-org"                    # Optional: defaults to current repo owner
      repositories: ["repo1", "repo2"]   # Optional: defaults to current repo only
```

**Shared workflow pattern** (recommended):

```yaml wrap
imports:
  - shared/github-mcp-app.md  # Centralized GitHub App configuration

permissions:
  contents: read
  issues: write

tools:
  github:
    toolsets: [repos, issues]
```

**Benefits**:
- On-demand token minting at workflow start
- Automatic token revocation at workflow end (even on failure)
- Permissions automatically mapped from agent job `permissions` field
- Works with both local (Docker) and remote (hosted) modes
- Isolated from safe-outputs token configuration

See [GitHub App Tokens for GitHub MCP Server](/gh-aw/reference/tokens/#github-app-tokens-for-github-mcp-server) for complete setup and configuration details.

**Token precedence**: GitHub App → `github-token` → `GH_AW_GITHUB_MCP_SERVER_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`

## Playwright Tool (`playwright:`)

Enables containerized browser automation with domain-based access control:

```yaml wrap
tools:
  playwright:
    allowed_domains: ["defaults", "github", "*.custom.com"]
    version: "1.56.1"  # Optional: defaults to 1.56.1, use "latest" for newest
```

**Domain Access**: Uses `network:` ecosystem bundles (`defaults`, `github`, `node`, `python`, etc.). Defaults to `["localhost", "127.0.0.1"]`. Domains auto-include subdomains.

## Built-in MCP Tools

### Agentic Workflows (`agentic-workflows:`)

Provides workflow introspection, log analysis, and debugging tools. Requires `actions: read` permission:

```yaml wrap
permissions:
  actions: read
tools:
  agentic-workflows:
```

See [MCP Server](/gh-aw/setup/mcp-server/#using-as-agentic-workflows-tool) for available operations.

### Cache Memory (`cache-memory:`)

Persistent memory storage across workflow runs for trends and historical data.

```yaml wrap
tools:
  cache-memory:
```

### Repo Memory (`repo-memory:`)

Repository-specific memory storage for maintaining context across executions.

```yaml wrap
tools:
  repo-memory:
```

## Custom MCP Servers (`mcp-servers:`)

Integrate custom Model Context Protocol servers for third-party services:

```yaml wrap
mcp-servers:
  slack:
    command: "npx"
    args: ["-y", "@slack/mcp-server"]
    env:
      SLACK_BOT_TOKEN: "${{ secrets.SLACK_BOT_TOKEN }}"
    allowed: ["send_message", "get_channel_history"]
```

**Options**: `command` + `args` (process-based), `container` (Docker image), `url` + `headers` (HTTP endpoint), `registry` (MCP registry URI), `env` (environment variables), `allowed` (tool restrictions). See [MCPs Guide](/gh-aw/guides/mcps/) for setup.

### Registry Field

The `registry` field specifies the URI to an MCP server's installation location in an MCP registry. This is useful for documenting the source of an MCP server and can be used by tooling to discover and install MCP servers:

```yaml wrap
mcp-servers:
  markitdown:
    registry: "https://api.mcp.github.com/v0/servers/microsoft/markitdown"
    command: "npx"
    args: ["-y", "@microsoft/markitdown"]
```

**When to use**:
- **Document server source**: Include `registry` to indicate where the MCP server is published
- **Registry-aware tooling**: Some tools may use the registry URI for discovery and version management
- **Both stdio and HTTP servers**: Works with both `command`-based stdio servers and `url`-based HTTP servers

**Examples**:

```yaml wrap
# Stdio server with registry
mcp-servers:
  filesystem:
    registry: "https://api.mcp.github.com/v0/servers/modelcontextprotocol/filesystem"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem"]

# HTTP server with registry
mcp-servers:
  custom-api:
    registry: "https://registry.example.com/servers/custom-api"
    url: "https://api.example.com/mcp"
    headers:
      Authorization: "Bearer ${{ secrets.API_TOKEN }}"
```

The `registry` field is informational and does not affect server execution. It complements other configuration fields like `command`, `args`, `container`, or `url`.

## Related Documentation

- [Safe Inputs](/gh-aw/reference/safe-inputs/) - Define custom inline tools with JavaScript or shell scripts
- [Frontmatter](/gh-aw/reference/frontmatter/) - All frontmatter configuration options
- [Network Permissions](/gh-aw/reference/network/) - Network access control for AI engines
- [MCPs](/gh-aw/guides/mcps/) - Complete Model Context Protocol setup and usage
- [CLI Commands](/gh-aw/setup/cli/) - CLI commands for workflow management
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Directory layout and organization
- [Imports](/gh-aw/reference/imports/) - Modularizing workflows with includes
