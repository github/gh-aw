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

Linear is configured through an imported connector, not a built-in toolset. Import `.github/workflows/connectors/linear.md`, grant exact resources, and select `tools.linear: [get-issue]` or the reviewed hosted MCP registration. See [Connectors](/gh-aw/reference/connectors/#linear).

### Jira connector

Import `.github/workflows/connectors/jira.md` and select `tools.jira: [personal]` or `[service-account]` with explicit grants. The former `auth`/`allowed` built-in configuration is no longer supported. See the [Jira connector reference](/gh-aw/reference/connectors/#jira).

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

Engine behavior differs:

- **Claude**: Web search is off by default and enabled only when `web-search:` is declared.
- **Codex**: Enabled when either `web-search:` or `web-fetch:` is declared; the two cannot be disabled independently. With neither, Codex runs with `-c web_search="disabled"`. Use `network.hosted-web` to restrict hosted retrieval.
- **Copilot**: Runs in offline BYOK mode, so native web tools are disabled. Compilation rejects `web-search:` and, in CLI mode, `web-fetch:`. With `engine.copilot-sdk: true`, `web-fetch:` works through a proxy-aware custom tool that follows `network.allowed`; this does not enable native search.
- **Gemini** and **Pi**: No built-in web search. `web-search:` has no effect and `gh aw compile` prints a warning.

Copilot, Gemini, and Pi need a third-party MCP server for search (Copilot CLI mode also needs one for fetch). See [Using Web Search](/gh-aw/reference/web-search/).

### Playwright Tool (`playwright:`)

Configure Playwright for browser automation and testing:

```yaml wrap
tools:
  playwright:
    version: "1.56.1"  # Optional: specify version
```

See the **[Playwright Reference](/gh-aw/reference/playwright/)** for full configuration, network access, and browser support.

### Cache Memory (`cache-memory:`)

Persistent memory storage across workflow runs for trends and historical data.

```yaml wrap
tools:
  cache-memory:
```

See the **[Cache Memory Reference](/gh-aw/reference/cache-memory/)** for full configuration and examples.

### Drive Memory (`drive-memory:`) — Private Preview

An experimental, feature-gated GitHub Drives integration. Do not configure it unless GitHub has enrolled the repository in the private preview. See the [Drive Memory Reference](/gh-aw/experimental/drive-memory/).

### Repo Memory (`repo-memory:`)

Repository memory that persists across executions.

```yaml wrap
tools:
  repo-memory:
```

See the **[Repo Memory Reference](/gh-aw/reference/repo-memory/)** for full configuration and examples.

### QMD Documentation Search — Experimental

Import a shared workflow to configure [QMD Documentation Search](/gh-aw/experimental/qmd/), including its MCP server, indexing job, and cache-memory. See the [example shared workflow](/gh-aw/experimental/qmd/#example-shared-workflow).

### Introspection on Agentic Workflows (`agentic-workflows:`)

Provides workflow introspection, log analysis, and debugging tools. Requires `actions: read` permission:

```yaml wrap
permissions:
  actions: read
tools:
  agentic-workflows:
```

See [GH-AW as an MCP Server](/gh-aw/reference/gh-aw-as-mcp-server/) for available operations.

The built-in container runs as the runner's UID and GID unless a container user is configured, so downloaded logs stay runner-owned and the post-agent secret-redaction step can read them. Redaction failures still remove unsanitized sources and fail the step.

The `compile` tool is read-only (`--no-emit`) by default. Remote imports and nested includes must already be cached; container validation does not pull missing Docker images. Workflow-writing tools (`add`, `update`, `fix`), emitted or dry-run compilation, and external scanners require opt-in at server startup:

```aw wrap
tools:
  agentic-workflows:
    allow-write-tools: true
```

This sets `--allow-write-tools` while preserving actor validation. It permits local workflow modifications and compiler scanner execution, not additional GitHub permissions or workflow dispatch. Enable it only for trusted clients and workflow sources.

### MCP CLI Mounting (`cli-proxy:`)

Set `tools.cli-proxy: true` to mount each user-facing MCP server as a standalone CLI tool on `PATH`, so the agent can invoke it from shell instead of through the MCP protocol:

```yaml wrap
tools:
  cli-proxy: true
```

With CLI mounting enabled, workflow-accessible servers such as `safeoutputs` and `mcpscripts` are wrapped as executables:

```bash
safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.\nQuoted text: \"hello\".\nLiteral path: C:\\temp\\file.txt"}'
mcpscripts mcpscripts-gh --args "issue list --limit 5"
```

For `add_comment`, use `item_number`, not `issue_number` (schema validation strips the latter). Pass safe-output arguments as one single-quoted JSON object, encoding newlines as `\n`, double quotes as `\"`, backslashes as `\\`, and apostrophes as `\u0027`. Stdin also works: `safeoutputs <tool_name> .` reads a complete JSON object from stdin.

CLI mounting only changes the agent-facing interface: mounted servers leave the MCP tool list, which can reduce token use from large tool schemas. Defaults to `false`.

CLI mounting and `gh-proxy` mode both require shell access. If `tools.bash` is disabled (`bash: false` or `bash: []`), `cli-proxy: true` and `tools.github.mode: gh-proxy` are rejected at compile time, and strict mode requires `cli-proxy: false` to be set explicitly:

```yaml wrap
tools:
  bash: false
  cli-proxy: false
  github:
    mode: local
```

With `cli-proxy: false` and an MCP-backed GitHub mode (`local` or `remote`), servers such as `safeoutputs` stay on the MCP protocol and CLI instructions are omitted from the prompt. Run `gh aw fix` to add the explicit setting to existing workflows.

## Tool Timeout Configuration

### Tool Operation Timeout (`tools.timeout`)

Sets the per-operation timeout in seconds when supported by the engine. Claude and Codex default to 60 seconds. For Codex, this configures native MCP tool calls, not shell-command duration; use `timeout-minutes` for the overall execution limit.

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

Both fields accept integers or GitHub Actions expressions, so `workflow_call` workflows can parameterize them:

```yaml wrap
tools:
  timeout: ${{ inputs.tool-timeout }}
  startup-timeout: ${{ inputs.startup-timeout }}
```

> [!NOTE]
> Expressions resolve before execution, and Codex converts them to numeric TOML settings without falling back to defaults. An explicit per-server timeout in `engine.config` takes precedence over `tools.timeout` or `tools.startup-timeout`.

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

**Options**: `command` + `args` (process-based), `container` (Docker image), `url` + `headers` (HTTP endpoint), `ca-cert-file` (per-server HTTPS CA trust), `registry` (MCP registry URI), `env` (environment variables), `allowed` (tool restrictions), `required` (startup criticality). See [MCPs Guide](/gh-aw/guides/mcps/) for setup.

### Custom CA Certificates

`ca-cert-file` adds PEM CA certificates to system trust for one custom HTTPS MCP server; other servers and model-provider connections keep their trust. Certificate and hostname verification stay enabled. Requires MCP Gateway v0.4.35 or later.

```yaml wrap
mcp-servers:
  internal:
    type: http
    url: https://mcp.internal.example/mcp
    ca-cert-file: .github/certs/internal-ca.pem
```

The path is relative to `GITHUB_WORKSPACE` or an absolute Linux host path. For a custom checkout path such as `checkout: { path: target }`, include that directory (`target/.github/certs/internal-ca.pem`). The `${{ github.workspace }}/` and `${{ runner.temp }}/` prefixes are also supported. Paths may contain letters, digits, spaces, `_`, `.`, `/`, and `-`; traversal and other expressions are rejected. The file must exist and be readable before the **Start MCP Gateway** step. The gateway mounts only that file, read-only, even when `sandbox.mcp.mounts` overrides the defaults.

To provision PEM content from a secret, write it to a file in a reviewed setup step and reference that path:

```yaml wrap
steps:
  - name: Provision MCP CA
    env:
      MCP_CA_PEM: ${{ secrets.MCP_CA_PEM }}
    run: |
      umask 077
      printf '%s\n' "$MCP_CA_PEM" > "$RUNNER_TEMP/internal-ca.pem"
```

Do not put a private key in this file. Missing or unreadable files fail startup; malformed PEM or files without valid CA certificates are rejected without printing certificate contents. `ca-cert-file` is not supported for stdio servers or plain HTTP URLs, and `gh aw mcp inspect` reports it as unsupported. It does not replace the model-provider `sandbox.agent.ca-cert` setting.

### Required Field

By default every MCP server is startup-critical: if it can't be reached, the workflow fails. Set `required: false` to make a server best-effort; an unreachable server then logs a warning and is skipped:

```yaml wrap
mcp-servers:
  datadog:
    type: http
    url: "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp"
    required: false
```

Use this for optional integrations so transient outages don't block other servers. At least one server must still connect for startup to proceed.

### Registry Field

The `registry` field records a server's source URI in an MCP registry. It is informational only: gh-aw does not enforce registry usage or change execution. It works with stdio and HTTP servers:

```yaml wrap
mcp-servers:
  filesystem:
    registry: "https://api.mcp.github.com/v0/servers/modelcontextprotocol/filesystem"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
```

## Learn More

See [GitHub Tools](/gh-aw/reference/github-tools/), [Playwright](/gh-aw/reference/playwright/), [Cache Memory](/gh-aw/reference/cache-memory/), [Repo Memory](/gh-aw/reference/repo-memory/), [MCP Scripts](/gh-aw/reference/mcp-scripts/), and [MCPs](/gh-aw/guides/mcps/).
