---
description: Cache, tool, import, and permission reference for GitHub Agentic Workflows frontmatter.
---

# Tools, Imports, and Permissions

### Cache Configuration

The `cache:` field supports the same syntax as the GitHub Actions `actions/cache` action:

**Single Cache:**

```yaml
cache:
  key: node-modules-${{ hashFiles('package-lock.json') }}
  path: node_modules
  restore-keys: |
    node-modules-
```

**Multiple Caches:**

```yaml
cache:
  - key: node-modules-${{ hashFiles('package-lock.json') }}
    path: node_modules
    restore-keys: |
      node-modules-
  - key: build-cache-${{ github.sha }}
    path:
      - dist
      - .cache
    restore-keys:
      - build-cache-
    fail-on-cache-miss: false
```

**Supported Cache Parameters:**

- `key:` - Cache key (required)
- `path:` - Files/directories to cache (required, string or array)
- `restore-keys:` - Fallback keys (string or array)
- `upload-chunk-size:` - Chunk size for large files (integer)
- `fail-on-cache-miss:` - Fail if cache not found (boolean)
- `lookup-only:` - Only check cache existence (boolean)

Cache steps are auto-added to the workflow job; cache config is removed from the final `.lock.yml`.

> **Memory configuration**: For `cache-memory:`, `repo-memory:`, and `comment-memory:`, see [memory.md](memory.md).


## Tool Configuration

The `tools:` field configures which tools the coding agent may use.

### GitHub Tools (`tools.github`)

- `allowed:` - Array of allowed GitHub API functions. Each entry is either a string tool name (e.g., `issue_read`) or an object `{ name: <tool>, max-calls: <n> }` to cap how many times that tool may be called per run. Colon shorthand (`"issue_read:1"`) is **not** a call-limit form.

  ```yaml
  tools:
    github:
      allowed:
        - { name: issue_read, max-calls: 1 }
        - list_labels
        - pull_request_read
  ```
- `mode:` - GitHub access mode. **Prefer `"gh-proxy"`** — it is faster (no MCP server startup) and lets the agent use `gh` shell commands directly for all GitHub reads (issues, PRs, discussions, commits, etc.):
  - `"gh-proxy"` (**preferred**) — pre-authenticated `gh` CLI available in bash; no GitHub MCP server is registered. Use `gh` commands for all GitHub reads.
  - `"local"` (default) — Docker-based GitHub MCP Server; use GitHub MCP tools for reads, `gh` is not authenticated.
  - **do NOT use `"remote"`** — it does not work with the GitHub Actions token; use `"gh-proxy"` instead.
- `version:` - MCP server version (local mode only)
- `args:` - Additional command-line arguments (local mode only)
- `read-only:` - The GitHub MCP server always operates in read-only mode; this field is accepted but has no effect
- `github-token:` - Custom GitHub token
- `lockdown:` - Enable lockdown mode to limit content surfaced from public repositories to items authored by users with push access (boolean, default: false)
- `github-app:` - GitHub App configuration for token minting; when set, mints an installation access token at workflow start that overrides `github-token`
  - `client-id:` - GitHub App client ID (required, e.g., `${{ vars.APP_ID }}`). Use `app-id:` for legacy compatibility.
  - `private-key:` - GitHub App private key (required, e.g., `${{ secrets.APP_PRIVATE_KEY }}`)
  - `owner:` - Optional installation owner (defaults to current repository owner)
  - `repositories:` - Optional list of repositories to grant access to (array)
  - `permissions:` - Optional extra permissions to include in the minted token for org-level API access (object)
    - Example: `permissions: { members: read, organization-administration: read }` — required when calling org-level APIs (e.g., `orgs`, `users` toolsets) since the default GITHUB_TOKEN does not have org-scoped permissions
- `min-integrity:` - Minimum integrity level for MCP Gateway guard policy; controls which content the agent may act on based on author trust (`none`, `unapproved`, `approved`, `merged`)
- `blocked-users:` - Usernames whose content is unconditionally blocked (array or GitHub Actions expression); these users receive integrity below `none` and are always denied
- `approval-labels:` - Label names that elevate a content item's integrity to `approved` when present (array or GitHub Actions expression); does not override `blocked-users`
- `trusted-users:` - Usernames elevated to `approved` integrity regardless of `author_association` (array or GitHub Actions expression); takes precedence over `min-integrity` but not over `blocked-users`; requires `min-integrity` to be set
- `private-to-public-flows:` - Opt out of MCP Gateway cross-visibility protections (which block private-repo data from reaching public sinks). `allow` disables `forcePublicRepos` and sink-visibility enforcement for all servers (**not compatible with strict mode**); an array of MCP server IDs (e.g. `[github, my-server]`) exempts only those servers from sink-visibility enforcement (strict-mode compatible, keeps `forcePublicRepos`). Security-sensitive — only use when private→public flows are intended.
- `toolsets:` - Enable specific GitHub toolset groups (single name string or array; a string is shorthand for a one-element array)
  - **Default toolsets** (when unspecified): `context`, `repos`, `issues`, `pull_requests` (excludes `users` as GitHub Actions tokens don't support user operations)
  - **Group aliases**: `default` (recommended action-friendly set), `action-friendly` (action-safe toolsets, excludes `users`), `all` (everything)
  - **Individual toolsets**: `context`, `repos`, `issues`, `pull_requests`, `actions`, `code_quality`, `code_security`, `copilot`, `copilot_issue_intents`, `copilot_spaces`, `dependabot`, `discussions`, `gists`, `git`, `github_support_docs_search`, `labels`, `notifications`, `orgs`, `projects`, `secret_protection`, `security_advisories`, `stargazers`, `users`
    Search tools are distributed across `repos`, `orgs`, `users`, and `issues`; there is no standalone `search` toolset.
  - Examples: `toolsets: [default]`, `toolsets: [default, discussions]`, `toolsets: [repos, issues]`
  - **Recommended**: Prefer `toolsets:` over `allowed:` for better organization and reduced configuration verbosity

### Other Built-in Tools

- `agentic-workflows:` - GitHub Agentic Workflows MCP server for workflow introspection. Provides `status`, `compile`, `logs`, `audit`, and `checks` tools so agents can analyze run traces and improve workflows. Enable with `agentic-workflows: true`.
- `edit:` - File editing tools (required to write to files in the repository)
- `web-fetch:` - Web content fetching tools
- `web-search:` - Web search tools
- `bash:` - Shell command tools
  - **Bash allowlist decision rule:**
    - **PR-triggered workflows** processing **untrusted input** (issue/PR body, comment text, user-provided filenames): use a **narrow allowlist** (e.g. `[find, cat, grep, wc, jq]`). This limits blast radius if shell injection is embedded in untrusted content.
    - **`schedule` or `workflow_dispatch` workflows** with **no untrusted input** (only trusted API data or internal state): `["*"]` is acceptable.
    - **Rule of thumb**: If the workflow reads issue/PR bodies, comment text, or other user-provided strings, use a narrow list. Otherwise `["*"]` is acceptable.

  ```yaml
  # PR-triggered workflow reading untrusted user text
  on:
    pull_request:
  tools:
    bash: [find, cat, grep, wc, jq]

  # Internal scheduled workflow reading only trusted/internal data
  on:
    schedule:
      - cron: "0 * * * *"
  tools:
    bash: ["*"]
  ```
- `playwright:` - Browser automation for visual regression, accessibility, and end-to-end testing. The built-in integration uses `playwright-cli <command>` in bash, and `localhost` reaches local servers directly. `mode: mcp` is removed; use a custom `mcp-servers` entry if MCP is required. Pin the CLI with `version:` and restrict network to `local` + `playwright`.

  ```yaml
  tools:
    playwright:
      version: "0.1.11"  # optional: @playwright/cli npm package version
  ```
- `timeout:` - Per-operation timeout in seconds for all tool and MCP calls (integer or expression, default: 60 s for all engines).
- `startup-timeout:` - Timeout in seconds for MCP server initialization (integer or expression, default: 120).
- `cli-proxy:` - Mount each user-facing MCP server as a standalone CLI tool on `PATH` (boolean, default: `false`). When enabled, the agent can call MCP servers via shell (e.g. `github issue_read --method get ...`).

### Custom MCP Tools

Stdio MCP servers must be Docker-based (use `container:` + `entrypoint:`). For Node/Python servers already installed on the runner, use HTTP transport instead:

```yaml
# Stdio (Docker-based)
mcp-servers:
  my-custom-tool:
    container: "ghcr.io/my-org/my-tool:latest"
    entrypoint: "my-tool"
    allowed:
      - custom_function_1
      - custom_function_2

# HTTP (for Node/Python servers running on the runner)
mcp-servers:
  my-node-tool:
    type: http
    url: "http://localhost:8765/mcp"
```

HTTP MCP servers are also supported with optional upstream authentication:

```yaml
mcp-servers:
  my-server:
    type: http
    url: "https://myserver.example.com/mcp"
    headers:
      Authorization: "Bearer ${{ secrets.API_KEY }}"    # Optional: custom headers
  my-oidc-server:
    type: http
    url: "https://myserver.example.com/mcp"
    auth:
      type: github-oidc                                  # GitHub Actions OIDC token authentication
      audience: "https://myserver.example.com"          # Optional: custom OIDC audience
```

`auth.type: github-oidc` uses GitHub Actions OIDC tokens for secure server-to-server authentication without static credentials. The `audience` field defaults to the server URL when omitted.

- `required:` - Whether a stdio or HTTP MCP server must pass its startup connectivity check (boolean, default: `true`). Set `false` for an optional server so a failed startup check only logs a warning and the workflow continues without it, instead of failing the run.

## Agent Plugins (`plugins:`)

:::caution[Experimental]
Compiling a workflow that uses `plugins:` emits a warning; the interface may change.
:::

Installs [Agent Plugins](https://agent-plugins.org) through the selected engine (top-level field, distinct from Pi's `engine.extensions`):

```yaml
plugins:
  - octo-org/agent-plugin@v1
  - octo-org/agent-plugins/plugins/example@main
```

- Entries use `owner/repository[/path]@ref`; `ref` is required (branch, tag, or 40-char commit SHA).
- The compiler resolves every branch/tag to a commit SHA at compile time; unresolvable refs fail compilation, so generated workflows never install from a moving ref.
- Supported by `copilot`, `claude`, and `codex` (each installs plugins its own way — see [syntax-engine.md](syntax-engine.md)); `gemini` and `pi` reject `plugins:` at compile time. Imported engine definitions opt in via `engine.behaviors.plugins` (see [configure-agentic-engine.md](configure-agentic-engine.md)).
- Plugin repositories must be public — the checkout step uses the workflow's default `github.token` and does not support per-entry `github-token`/`github-app`, unlike `skills:`.
- Merge behavior across imports: see the imports merge list above.

### Engine Network Permissions

Control network access via the top-level `network:` field (defaults to `network: defaults` — basic infrastructure only). For workflows that build, test, or install packages, always add the language ecosystem alongside `defaults`:

```yaml
network:
  allowed:
    - defaults         # Basic infrastructure (CAs, Ubuntu verification, JSON schema)
    - node             # Node.js / npm ecosystem
    - "api.custom.com" # Custom domain
  blocked:
    - "*.ads.com"      # Block domain patterns
```

> **Full reference**: valid ecosystem identifiers, invalid shorthands, wildcard/protocol rules, and per-language inference live in [network.md](network.md). Do not restate the ecosystem table here.

## Imports Field

Import shared components using the `imports:` field in frontmatter:

```yaml
---
on: issues
engine: copilot
imports:
  - copilot-setup-steps.yml    # Import setup steps from copilot-setup-steps.yml
  - shared/security-notice.md
  - shared/tool-setup.md
  - shared/mcp/tavily.md
---
```

**Object form with inputs** — Use `path:`/`uses:` + `with:`/`inputs:` to pass values to shared workflows that define an `import-schema:`:

```yaml
imports:
  - path: shared/tool-setup.md
    with:
      environment: staging
      max-issues: 3
  - uses: shared/security-notice.md  # 'uses' is an alias for 'path'
```

`path`/`uses` and `with`/`inputs` are the only valid keys on an import entry. To supply environment variables or a checkout ref, set top-level `env:`/`checkout:` frontmatter inside the imported file itself — those are merged into the importing workflow (see the merge list below), not configured per import entry.

Conditional `imports:` entries are not supported. For experiment-specific prompt variants, keep the import unconditional and gate a `{{#runtime-import? ...}}` block (optional form) in the workflow body instead. The optional form is not promoted to unconditional lock-file macros, so the content is only injected when the condition is true at runtime.

Inside the imported workflow, access values via `${{ github.aw.import-inputs.<name> }}`.

### Import File Structure

Import files are in `.github/workflows/shared/` and can contain:

- Tool configurations
- Safe-outputs configurations
- Text content
- Mixed frontmatter + content

The following frontmatter fields in imported files are merged into the importing workflow:

- `tools:` - Merged with the importing workflow's tools
- `safe-outputs:` - Merged with safe-output configuration
- `env:` - Environment variables merged; conflicts between two imports defining the same key are compilation errors (remove the duplicate or move it to the main workflow to override)
- `checkout:` - Checkout configurations appended (main workflow's checkouts take precedence)
- `github-app:` - Top-level GitHub App credentials (first-wins across imports)
- `on.github-app:` - Activation GitHub App credentials (first-wins across imports)
- `steps:` - Steps appended in import order
- `pre-agent-steps:` - Steps appended in import order
- `post-steps:` - Steps appended in import order
- `jobs.<job-id>.setup-steps`, `jobs.<job-id>.pre-steps`, and `jobs.activation.steps` - Merged per job with imported steps first, then main workflow steps. Execution order is `setup-steps` before `pre-steps`; `jobs.activation.steps` run later in the activation job before the activation artifact is staged.
- `runtimes:`, `network:`, `permissions:`, `services:`, `cache:`, `features:`, `mcp-servers:`
- `plugins:` - Union by plugin path; identical refs dedupe, compatible semantic versions select the highest, incompatible majors/non-semver conflicts fail compilation

Example import file:

```markdown
---
tools:
  github:
    allowed: [get_repository, list_commits]
safe-outputs:
  create-issue:
    labels: [automation]
env:
  MY_VAR: "shared-value"
checkout:
  fetch-depth: 0
---

Additional instructions for the coding agent.
```

### Special Import: copilot-setup-steps.yml

The `copilot-setup-steps.yml` file receives special handling when imported. Instead of importing the entire job structure, **only the steps** from the `copilot-setup-steps` job are extracted and inserted **at the start** of your workflow's agent job.

**Key behaviors:**

- Only the steps array is imported (job metadata like `runs-on`, `permissions` is ignored)
- Imported steps are placed **at the start** of the agent job (before all other steps)
- Other imported steps are placed after copilot-setup-steps but before main frontmatter steps
- Main frontmatter steps come last
- Final order: **copilot-setup-steps → other imported steps → main frontmatter steps**
- Supports both `.yml` and `.yaml` extensions
- Enables clean reuse of common setup configurations across workflows

**Example:**

```yaml
---
on: issue_comment
engine: copilot
imports:
  - copilot-setup-steps.yml
  - shared/common-tools.md
steps:
  - name: Custom environment setup
    run: echo "Main frontmatter step runs last"
---
```

In the compiled workflow, the order is: copilot-setup-steps → imported steps from shared/common-tools.md → main frontmatter steps.

## Permission Patterns

**IMPORTANT**: Agentic workflows should not include write permissions (`contents: write`, `issues: write`, `pull-requests: write`) on the main agent job. Safe-outputs provide these via separate secured jobs. In `strict: true` mode, granting any of these three write scopes to the main job is a compilation error; outside strict mode it compiles but is against the recommended security posture (see [workflow-constraints.md](workflow-constraints.md)).

### Read-Only Pattern

```yaml
permissions:
  contents: read
  metadata: read
```

### Output Processing Pattern (Recommended)

```yaml
permissions:
  contents: read      # Main job minimal permissions
  actions: read

safe-outputs:
  create-issue:       # Automatic issue creation
  add-comment:  # Automatic comment creation
  create-pull-request: # Automatic PR creation
```

**Key Benefits of Safe-Outputs:**

- Main job runs with minimal permissions
- Write operations handled by dedicated jobs
- Safe-outputs jobs auto-receive required permissions
- Clear audit trail between AI processing and GitHub API
