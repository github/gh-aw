---
title: Tools
description: Configure GitHub API tools, browser automation, and AI capabilities available to your agentic workflows, including GitHub tools and custom MCP servers.
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

## Built-in Tools

### Edit Tool (`edit:`)

Allows file editing in the GitHub Actions workspace.

```yaml wrap
tools:
  edit:
```

### GitHub Tools (`github:`)

Configure GitHub API operations including toolsets, remote/local modes, and authentication.

```yaml wrap
tools:
  github:
    toolsets: [repos, issues]
```

See **[GitHub Tools Reference](/gh-aw/reference/github-tools/)** for complete configuration options.

### Linear Tools (`linear:`)

Connect to [Linear's official hosted MCP server](https://linear.app/docs/mcp) using the well-known `LINEAR_API_KEY` GitHub Actions secret:

```yaml wrap
tools:
  linear: {}
```

Set `token` to use a different secret containing a Linear API key or OAuth access token. The integration uses Streamable HTTP through the MCP gateway and always uses Linear's server-enforced read-only endpoint. Use `allowed` to restrict tool names and `required: false` to make Linear connectivity best-effort:

```yaml wrap
tools:
  linear:
    token: ${{ secrets.CUSTOM_LINEAR_TOKEN }}
    allowed: ["*"]
    required: true
```

Use `toolsets` to enable related groups of tools without maintaining individual tool names:

```yaml wrap
tools:
  linear:
    toolsets: [issues, projects]
```

Supported toolsets are `all`, `attachments`, `comments`, `customers`, `cycles`, `diffs`, `documentation`, `documents`, `initiatives`, `issues`, `milestones`, `projects`, `status_updates`, `teams`, and `users`. The compiler expands toolsets into the gateway's allowed-tool list. If `allowed` is also set, each name or wildcard must match a tool in the selected toolsets.

The Linear credential is passed to the gateway as an environment variable and sent as an `Authorization: Bearer` header. It is not embedded in MCP configuration. Linear works with `tools.cli-proxy: true` like other remote MCP servers.
### Jira Tools (`jira:`)

Connect to Atlassian's official remote Rovo MCP endpoint from non-interactive GitHub Actions workloads. Browser OAuth, device login, and user-consent flows are not supported.

Use an Atlassian service account API key:

```yaml wrap
tools:
  jira:
    auth:
      type: service-account
      token: ${{ secrets.ATLASSIAN_SERVICE_ACCOUNT_API_KEY }}
    allowed:
      - getJiraIssue
      - searchJiraIssuesUsingJql
```

Or use an Atlassian account email and API token:

```yaml wrap
tools:
  jira:
    auth:
      type: api-token
      email: ${{ secrets.ATLASSIAN_EMAIL }}
      token: ${{ secrets.ATLASSIAN_API_TOKEN }}
    allowed:
      - getJiraIssue
      - searchJiraIssuesUsingJql
```

The `allowed` list is required and accepts only these read-only Jira tools:

- `getIssueLinkTypes`
- `getJiraIssue`
- `getJiraIssueRemoteIssueLinks`
- `getJiraIssueTypeMetaWithFields`
- `getJiraProjectIssueTypesMetadata`
- `getTransitionsForJiraIssue`
- `getVisibleJiraProjects`
- `lookupJiraAccountId`
- `searchJiraIssuesUsingJql`

`allowed: ["*"]` is also accepted as shorthand for enabling all nine tools above; it is expanded to that fixed list at compile time and never grants access to the full, unrestricted MCP tool set. Omitting `allowed` or naming a write-capable tool is rejected.

The endpoint defaults to `https://mcp.atlassian.com/v1/mcp`. Set `url` only when your organization uses another HTTPS Atlassian MCP endpoint. Credentials must be direct GitHub Actions secret expressions; service account keys use the HTTP bearer scheme while API tokens use HTTP Basic authentication generated at runtime.

### Bash Tool (`bash:`)

Enables shell command execution in the workspace. Defaults to safe commands (`echo`, `printf`, `ls`, `pwd`, `cat`, `head`, `tail`, `grep`, `wc`, `sort`, `uniq`, `date`, `yq`).

```yaml wrap
tools:
  bash:                              # Default safe commands
  bash: []                           # Disable all commands
  bash: ["echo", "ls", "git status"] # Specific commands only
  bash: [":*"]                       # All commands (use with caution)
```

Use wildcards like `git:*` for command families or `:*` for unrestricted access.

### Web Tools

Enable web content fetching and search capabilities:

```yaml wrap
tools:
  web-fetch:   # Fetch web content
  web-search:  # Search the web (engine-dependent)
```

**Note:** Some engines require third-party Model Context Protocol (MCP) servers for web search. See [Using Web Search](/gh-aw/reference/web-search/).

For the **Codex** engine, `web-search:` is disabled by default. Web search is only enabled when `web-search:` is explicitly declared in the `tools:` block. Without this declaration, Codex runs with `-c web_search="disabled"` and cannot access the web.

### Playwright Tool (`playwright:`)

Configure Playwright for browser automation and testing:

```yaml wrap
tools:
  playwright:
    version: "1.56.1"  # Optional: specify version
```

See **[Playwright Reference](/gh-aw/reference/playwright/)** for complete configuration options, network access, browser support, and example workflows.

### Cache Memory (`cache-memory:`)

Persistent memory storage across workflow runs for trends and historical data.

```yaml wrap
tools:
  cache-memory:
```

See **[Cache Memory Reference](/gh-aw/reference/cache-memory/)** for complete configuration options and usage examples.

### Drive Memory (`drive-memory:`) — Private Preview

Drive memory is an experimental, feature-gated GitHub Drives integration. Do not
configure it unless GitHub has explicitly enrolled the repository in the private
preview.

The [Drive Memory Reference](/gh-aw/experimental/drive-memory/) records the preview
behavior for enrolled repositories; it is not a recommendation for general use.

### Repo Memory (`repo-memory:`)

Repository-specific memory storage for maintaining context across executions.

```yaml wrap
tools:
  repo-memory:
```

See **[Repo Memory Reference](/gh-aw/reference/repo-memory/)** for complete configuration options and usage examples.

### QMD Documentation Search (`qmd:`) — Experimental

Build a local vector search index over documentation files and expose it as an MCP search tool. The index is built in a dedicated indexing job (no `contents: read` needed in the agent job):

```yaml wrap
tools:
  qmd:
    checkouts:
      - pattern: "docs/**/*.md"
```

See **[QMD Documentation Search](/gh-aw/experimental/qmd/)** for complete configuration options, checkout support, GitHub search integration, and cache key usage.

### Introspection on Agentic Workflows (`agentic-workflows:`)

Provides workflow introspection, log analysis, and debugging tools. Requires `actions: read` permission:

```yaml wrap
permissions:
  actions: read
tools:
  agentic-workflows:
```

See [GH-AW as an MCP Server](/gh-aw/reference/gh-aw-as-mcp-server/) for available operations.

### MCP CLI Mounting (`cli-proxy:`)

Set `tools.cli-proxy: true` to mount each user-facing MCP server as a standalone CLI tool on `PATH`. When enabled, the agent can invoke MCP servers as shell commands rather than through the MCP protocol:

```yaml wrap
tools:
  cli-proxy: true
```

With CLI mounting enabled, MCP servers accessible to the workflow (such as `safeoutputs` and `mcpscripts`) are wrapped as executable commands. For example:

```bash
safeoutputs add_comment --item_number 42 --body "Analysis complete"
mcpscripts mcpscripts-gh --args "issue list --limit 5"
```

The safe-output `add_comment` tool uses `--item_number` (not `--issue_number`) to target the issue or pull request — passing `--issue_number` is silently stripped by schema validation.

The MCP gateway configuration is unchanged — servers still start as normal. Only the agent's view changes: servers registered for CLI mounting are removed from the MCP tool list and accessed via shell instead.

This reduces token consumption from large MCP tool schemas and can simplify workflow prompts when shell-style invocation is preferred.

Defaults to `false`.

CLI mounting requires shell access: the wrappers are ordinary executables invoked from bash. GitHub `gh-proxy` mode is also shell-backed because GitHub reads are performed with the `gh` CLI. When `tools.bash` is disabled (`bash: false` or `bash: []`), `cli-proxy: true` and `tools.github.mode: gh-proxy` are rejected at compile time, and strict mode requires `cli-proxy: false` to be stated explicitly:

```yaml wrap
tools:
  bash: false
  cli-proxy: false
  github:
    mode: local
```

With `cli-proxy: false` and an MCP-backed GitHub mode (`local` or `remote`), MCP servers (including `safeoutputs`) remain available through the MCP protocol, and the CLI-only instructions are omitted from the generated prompt. Run `gh aw fix` to add the explicit setting to existing workflows.

## Tool Timeout Configuration

### Tool Operation Timeout (`tools.timeout`)

Sets the per-operation timeout in seconds for tool and MCP server calls. Applies to all tools and MCP servers when supported by the engine. Defaults vary by engine (Claude: 60 s, Codex: 120 s).

```yaml wrap
tools:
  timeout: 120   # seconds
```

### MCP Server Startup Timeout (`tools.startup-timeout`)

Sets the timeout in seconds for MCP server initialization. Default is 120 seconds.

```yaml wrap
tools:
  startup-timeout: 60   # seconds
```

Both fields accept either an integer or a GitHub Actions expression string, enabling `workflow_call` reusable workflows to parameterize these values:

```yaml wrap
tools:
  timeout: ${{ inputs.tool-timeout }}
  startup-timeout: ${{ inputs.startup-timeout }}
```

> [!NOTE]
> Expression values are passed through environment variables in the compiled workflow. TOML-based engine configs (Codex MCP gateway) fall back to engine defaults when an expression is used, since TOML has no expression syntax.

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

**Options**: `command` + `args` (process-based), `container` (Docker image), `url` + `headers` (HTTP endpoint), `registry` (MCP registry URI), `env` (environment variables), `allowed` (tool restrictions), `required` (startup criticality). See [MCPs Guide](/gh-aw/guides/mcps/) for setup.

### Required Field

MCP servers must pass a startup connectivity check before the agent starts. By default every server is startup-critical: if it cannot be reached, the workflow fails. Set `required: false` to mark a server as best-effort, so an unreachable server logs a warning and the workflow continues without it:

```yaml wrap
mcp-servers:
  datadog:
    type: http
    url: "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp"
    required: false
```

Use this for optional integrations whose transient outages (for example an HTTP 503 from a hosted endpoint) should not take down the other configured servers. At least one server must still connect successfully for startup to proceed.

### Registry Field

The `registry` field specifies the source URI of an MCP server in a registry. It is informational — useful for documenting server origin and enabling registry-aware tooling — and does not affect execution. gh-aw does not enforce registry usage. Works with both stdio and HTTP servers:

```yaml wrap
mcp-servers:
  filesystem:
    registry: "https://api.mcp.github.com/v0/servers/modelcontextprotocol/filesystem"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
```

## Learn More

- [GitHub Tools](/gh-aw/reference/github-tools/) - GitHub API operations, toolsets, and modes
- [Playwright](/gh-aw/reference/playwright/) - Browser automation and testing configuration
- [Cache Memory](/gh-aw/reference/cache-memory/) - Persistent memory across workflow runs
- [Repo Memory](/gh-aw/reference/repo-memory/) - Repository-specific memory storage
- [MCP Scripts](/gh-aw/reference/mcp-scripts/) - Define custom inline tools with JavaScript or shell scripts
- [MCPs](/gh-aw/guides/mcps/) - Complete Model Context Protocol setup and usage
