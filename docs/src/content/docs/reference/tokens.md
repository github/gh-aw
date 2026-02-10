---
title: GitHub Tokens
description: Comprehensive reference for all GitHub tokens used in gh-aw, including authentication, token precedence, and security best practices
sidebar:
  order: 650
disable-agentic-editing: true
---

GitHub Agentic Workflows authenticate using multiple tokens depending on the operation. This reference explains which token to use, when it's required, and how precedence works across different operations.

## User vs. Org Ownership

Ownership affects token requirements for repositories and Projects (v2). If the owner is your personal username, it is user-owned. If the owner is an organization, it is org-owned and managed with shared roles and access controls.

To confirm ownership, check the owner name and avatar at the top of the page or in the URL (`github.com/owner-name/...`). Clicking the owner takes you to a personal profile or an organization page, which confirms it instantly.

<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 400px), 1fr)); gap: 1.5rem; margin: 2rem 0;">
  <div class="gh-aw-video-wrapper">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="/gh-aw/images/user-owned_dark.png">
      <img alt="User-owned repository example" src="/gh-aw/images/user-owned_light.png">
    </picture>
    <div class="gh-aw-video-caption" role="note">
      User-owned repository: avatar shows a personal profile icon, URL includes username
    </div>
  </div>

  <div class="gh-aw-video-wrapper">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="/gh-aw/images/org-owned_dark.png">
      <img alt="Organization-owned repository example" src="/gh-aw/images/org-owned_light.png">
    </picture>
    <div class="gh-aw-video-caption" role="note">
      Organization-owned repository: avatar shows organization icon, URL includes org name
    </div>
  </div>
</div>

## Quick start: tokens you actually configure

GitHub Actions always provides `GITHUB_TOKEN` for you automatically.
For GitHub Agentic Workflows, you only need to create a few **optional** secrets in your own repo:

| When you need this…                                  | Secret to create                       | Notes |
|------------------------------------------------------|----------------------------------------|-------|
| Copilot workflows (CLI, engine, agent sessions, etc.)   | `COPILOT_GITHUB_TOKEN`                 | Needs Copilot Requests permission. For org-owned repos, needs org permissions: Members (read-only), GitHub Copilot Business (read-only). |
| Cross-repo Project Ops / remote GitHub tools         | `GH_AW_GITHUB_TOKEN`                   | PAT or app token with cross-repo access. |
| Assigning agents/bots to issues or pull requests     | `GH_AW_AGENT_TOKEN`                    | Used by `assign-to-agent` and Copilot assignee/reviewer flows. |
| Any GitHub Projects v2 operations                    | `GH_AW_PROJECT_GITHUB_TOKEN`           | **Required** for `project new` CLI command, `create-project`, and `update-project`. Default `GITHUB_TOKEN` cannot access Projects v2 API. |
| Isolating Model Context Protocol (MCP) server permissions (advanced optional) | `GH_AW_GITHUB_MCP_SERVER_TOKEN`        | Only if you want MCP to use a different token than other jobs. |

> [!TIP]
> GitHub App Authentication (Recommended for Production)
> Instead of PATs, you can use GitHub Apps for enhanced security with short-lived tokens:
> - **Safe outputs**: Configure `app-id` and `private-key` in `safe-outputs.app`
> - **GitHub MCP server**: Configure `app-id` and `private-key` in `tools.github.app`, or import `shared/github-mcp-app.md`
> - See [GitHub App Tokens](#github-app-tokens) and [GitHub App Tokens for GitHub MCP Server](#github-app-tokens-for-github-mcp-server) for details

Create these as **repository secrets in *your* repo**. The easiest way is to use the GitHub Agentic Workflows CLI:

```bash
# Current repository
gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_COPILOT_PAT"
gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_PAT"
gh aw secrets set GH_AW_AGENT_TOKEN --value "YOUR_AGENT_PAT"
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_PAT"
```

After these are set, gh-aw will automatically pick the right token for each operation; you should not need per-workflow PATs in most cases.

### CLI helpers for tokens and secrets

- `gh aw secrets bootstrap` – checks which recommended token secrets (like `GH_AW_GITHUB_TOKEN`, `COPILOT_GITHUB_TOKEN`) exist in a repository and prints suggested scopes plus copy‑pasteable `gh aw secrets set` commands.
- `gh aw init --tokens --engine <engine>` – runs token checks as part of repository initialization for a specific engine (`copilot`, `claude`, `codex`).
- `gh aw secrets set <NAME>` – creates or updates a repository secret. Values can come from `--value`, `--value-from-env`, or stdin (for example, `echo "PAT" | gh aw secrets set NAME`).

### Security and scopes (least privilege)

- Use `permissions:` at the workflow or job level so `GITHUB_TOKEN` only has what that workflow needs (for example, read contents and write PRs, but nothing else):

```yaml
permissions:
  contents: read
  pull-requests: write
```

- When creating each PAT/App token above, grant access **only** to the repos and scopes required for its scenario (cross-repo Project Ops, Copilot, agents, or MCP) and nothing more.
- Only expose powerful secrets to the jobs that need them by scoping them to `env:` at the job or step level, not globally:

```yaml
jobs:
  project-ops:
    env:
      GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}
```

- For very sensitive tokens, prefer GitHub Environments or organization-level secrets with required reviewers so only trusted workflows can use them.

### Distinguish user-owned vs organization-owned repos and Projects

Token requirements often depend on who owns the repository or Project. See [User vs. Org Ownership](#user-vs-org-ownership) for how to tell whether a repo or Project is user-owned or organization-owned.
## Token Overview

| Token | Type | Purpose | User Configurable |
|-------|------|---------|-------------------|
| `GITHUB_TOKEN` | Auto-provided | Default Actions token for current repository | No (auto-provided) |
| `COPILOT_GITHUB_TOKEN` | PAT | Copilot authentication (recommended) | **Yes** (required for Copilot) |
| `GH_AW_GITHUB_TOKEN` | PAT | Enhanced token for cross-repo and remote GitHub tools | **Yes** (required for cross-repo) |
| `GH_AW_AGENT_TOKEN` | PAT | Agent assignment operations | **Yes** (required for agent ops) |
| `GH_AW_PROJECT_GITHUB_TOKEN` | PAT | Required token for GitHub Projects v2 operations | **Yes** (required for Projects v2) |
| `GH_AW_GITHUB_MCP_SERVER_TOKEN` | PAT | Custom token specifically for GitHub MCP server | **Yes** (optional override) |
| `GITHUB_MCP_SERVER_TOKEN` | Auto-set | Automatically configured by compiler | No (auto-configured) |

## `GITHUB_TOKEN` (Default)

**Type**: Automatically provided by GitHub Actions

GitHub Actions automatically provides this token with scoped access to the current repository. It's used as a fallback when no custom token is configured.

**Capabilities**:

- Read and write access to current repository
- Default permissions based on workflow `permissions:` configuration
- No cost or setup required

**Limitations**:

- Cannot access other repositories
- Cannot trigger workflows via GitHub API
- Cannot assign bots (Copilot) to issues or PRs
- Cannot authenticate with Copilot engine
- Not supported for remote GitHub MCP server mode

**When to use**: Simple workflows that only need to interact with the current repository (comments, labels, issues in the same repo).

## `GH_AW_GITHUB_TOKEN` (Enhanced PAT)

**Type**: Personal Access Token (user must configure)

A fine-grained or classic Personal Access Token providing enhanced capabilities beyond `GITHUB_TOKEN`. This is the primary token for workflows that need cross-repository access or remote GitHub tools.

**Required for**:

- Cross-repository operations (accessing other repos)
- Remote GitHub tools mode (faster startup without Docker)
- Codex engine operations with GitHub MCP
- Any operation that needs to access multiple repositories

**Setup**:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - Repository access: Select specific repos or "All repositories"
   - Permissions:
     - Contents: Read (minimum) or Read+Write (for PRs)
     - Issues: Read+Write (for issue operations)
     - Pull requests: Read+Write (for PR operations)

2. Add to repository secrets:

```bash wrap
gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_PAT"
```

**Token precedence**: per-output → global safe-outputs → workflow-level → default fallback (`GH_AW_GITHUB_MCP_SERVER_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`)

## `GH_AW_GITHUB_MCP_SERVER_TOKEN` (GitHub MCP Server)

**Type**: Personal Access Token (optional override)

A specialized token for the GitHub MCP server that takes precedence over the standard token fallback chain. Use this when you want to provide different permissions specifically for GitHub MCP server operations versus other workflow operations.

**When to use**:

- You need different permission levels for MCP server vs. other operations
- You want to isolate MCP server authentication from general workflow authentication
- You're using remote GitHub MCP mode and need a token with specific scopes

**Setup**:

```bash wrap
gh aw secrets set GH_AW_GITHUB_MCP_SERVER_TOKEN --value "YOUR_PAT"
```

**Token precedence**: tool-level → workflow-level → `GH_AW_GITHUB_MCP_SERVER_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`

The compiler automatically sets `GITHUB_MCP_SERVER_TOKEN` and passes it as `GITHUB_PERSONAL_ACCESS_TOKEN` (local/Docker) or `Authorization: Bearer` header (remote).

> [!NOTE]
> In most cases, you don't need to set this token separately. Use `GH_AW_GITHUB_TOKEN` instead, which works for both general operations and GitHub MCP server.

## `GH_AW_PROJECT_GITHUB_TOKEN` (GitHub Projects v2)

**Type**: Personal Access Token (required for Projects v2 operations)

A specialized token for GitHub Projects v2 operations used by:
- The [`project new`](/gh-aw/setup/cli/#project-new) CLI command for creating projects
- The [`update-project`](/gh-aw/reference/safe-outputs/#project-board-updates-update-project) safe output for updating projects

**Required** because the default `GITHUB_TOKEN` cannot access the GitHub Projects v2 GraphQL API.

**When to use**:

- **Always required** for any Projects v2 operations (creating, updating, or reading project boards)
- The default `GITHUB_TOKEN` cannot create or manage ProjectV2 objects via GraphQL
- You want to isolate Projects permissions from other workflow operations

**Setup**:

The required token type depends on whether you're working with **user-owned** or **organization-owned** Projects:

**For User-owned Projects (v2)**:

You **must** use a **classic PAT** with the `project` scope. Fine-grained PATs do **not** work with user-owned Projects.

1. Create a [classic PAT](https://github.com/settings/tokens/new) with scopes:
   - `project` (required for user Projects)
   - `repo` (required if accessing private repositories)

**For Organization-owned Projects (v2)**:

You can use either a classic PAT or a fine-grained PAT:

1. **Option A**: Create a **classic PAT** with `project` and `read:org` scopes:
   - `project` (required)
   - `read:org` (required for org Projects)
   - `repo` (required if accessing private repositories)

2. **Option B (recommended)**: Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Repository access**: Select specific repos that will use the workflow
   - **Repository permissions**:
     - Contents: Read
     - Issues: Read (if needed for issue-triggered workflows)
     - Pull requests: Read (if needed for PR-triggered workflows)
   - **Organization permissions** (must be explicitly granted):
     - Projects: Read & Write (required for updating org Projects)
   - **Important**: You must explicitly grant organization access during token creation

3. **Option C**: Use a GitHub App with Projects: Read+Write permission

After creating your token, add it to repository secrets:

```bash wrap
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_PAT"
```

**Token precedence**: per-output → workflow-level → `GH_AW_PROJECT_GITHUB_TOKEN` → `GITHUB_TOKEN`

**Example configuration**:

```yaml wrap
---
# Option 1: Use GH_AW_PROJECT_GITHUB_TOKEN secret (recommended for org Projects)
# Just create the secret - no workflow config needed
---

# Option 2: Explicitly configure at safe-output level
safe-outputs:
  update-project:
    github-token: ${{ secrets.CUSTOM_PROJECT_TOKEN }}

# Option 3: Organization projects with GitHub tools integration
tools:
  github:
    toolsets: [default, projects]
    github-token: ${{ secrets.ORG_PROJECT_WRITE }}
safe-outputs:
  update-project:
    github-token: ${{ secrets.ORG_PROJECT_WRITE }}
```

**For organization-owned projects**, the complete configuration should include both the GitHub tools and safe outputs using the same token with appropriate permissions.

> [!NOTE]
> Default behavior
> By default, `update-project` is **update-only**: it will not create projects. If a project doesn't exist, the job fails with instructions to create it manually.
>
> **Important**: The default `GITHUB_TOKEN` **cannot** be used for Projects v2 operations. You **must** configure `GH_AW_PROJECT_GITHUB_TOKEN` or provide a custom token via `safe-outputs.update-project.github-token`. 
>
> **GitHub Projects v2 PAT Requirements**:
> - **User-owned Projects**: Require a **classic PAT** with the `project` scope (plus `repo` if accessing private repos). Fine-grained PATs do **not** work with user-owned Projects.
> - **Organization-owned Projects**: Can use either a classic PAT with `project` + `read:org` scopes, **or** a fine-grained PAT with:
>   - Repository access to specific repositories
>   - Repository permissions: Contents: Read, Issues: Read, Pull requests: Read (as needed)
>   - Organization permissions: Projects: Read & Write
>   - Explicit organization access granted during token creation
> - **GitHub App**: Works for both user and org Projects with Projects: Read+Write permission.
>
> To opt-in to creating projects, the agent must include `create_if_missing: true` in its output, and the token must have sufficient permissions to create projects in the organization.

> [!TIP]
> When to use vs GH_AW_GITHUB_TOKEN
> - Use `GH_AW_PROJECT_GITHUB_TOKEN` when you need **Projects-specific permissions** separate from other operations
> - Use `GH_AW_GITHUB_TOKEN` as the top-level token if it already has Projects permissions and you don't need isolation
> - The precedence chain allows the top-level token to be used if `GH_AW_PROJECT_GITHUB_TOKEN` isn't set

## `COPILOT_GITHUB_TOKEN` (Copilot Authentication)

**Type**: Personal Access Token (user must configure)

The recommended token for all Copilot-related operations including the Copilot engine, agent session creation, and bot assignments.

**Required for**:

- `engine: copilot` workflows
- `create-agent-session:` safe outputs
- Assigning `copilot` as issue assignee
- Adding `copilot` as PR reviewer

**Setup**:

The required token type depends on whether you own the repository or an organization owns it:

**For User-owned Repositories**:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Resource owner**: Your user account
   - **Repository access**: "Public repositories" or select specific repos
     - **Note**: You should leave "Public repositories" enabled; otherwise, you will not have access to the Copilot Requests permission option.
   - **Permissions**: 
     - Copilot Requests: Read-only (required)

**For Organization-owned Repositories**:

When an organization owns the repository, you need a fine-grained PAT with organization-level permissions:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Resource owner**: The organization that owns the repository
   - **Repository access**: Select the specific repositories that will use the workflow
   - **Repository permissions**:
     - Contents: Read (if needed for repository access)
     - Issues: Read (if needed for issue-triggered workflows)
     - Pull requests: Read (if needed for PR-triggered workflows)
   - **Organization permissions** (must be explicitly granted):
     - Members: Read-only (required)
     - GitHub Copilot Business: Read-only (required)
   - **Important**: You must explicitly grant organization access during token creation

2. Add to repository secrets:

```bash wrap
gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_COPILOT_PAT"
```

**Token precedence**: per-output → global safe-outputs → workflow-level → `COPILOT_GITHUB_TOKEN` → `GH_AW_GITHUB_TOKEN` (legacy, deprecated)

> [!NOTE]
> Organization token requirements
> For organization-owned repositories, the token must have both:
> - **Members: Read-only** - Required to access organization member information
> - **GitHub Copilot Business: Read-only** - Required to authenticate with Copilot services
>
> These organization permissions must be explicitly granted during token creation and may require approval from your organization administrator.

> [!CAUTION]
> `GITHUB_TOKEN` is **not** included in the fallback chain (lacks "Copilot Requests" permission). `COPILOT_CLI_TOKEN` and `GH_AW_COPILOT_TOKEN` are **no longer supported** as of v0.26+.

## `GH_AW_AGENT_TOKEN` (Agent Assignment)

**Type**: Personal Access Token (user must configure)

Specialized token for `assign-to-agent:` safe outputs that programmatically assign GitHub Copilot agents to issues or pull requests. This is distinct from the standard GitHub UI workflow for [assigning issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) - this token is used for automated agent assignment through workflow safe outputs.

**Required for**:

- `assign-to-agent:` safe outputs
- Programmatic agent assignment operations

**Setup**:

The required token type and permissions depend on whether you own the repository or an organization owns it:

**For User-owned Repositories**:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Resource owner**: Your user account
   - **Repository access**: "Public repositories" or select specific repos
   - **Repository permissions**:
     - Actions: Write
     - Contents: Write
     - Issues: Write
     - Pull requests: Write

**For Organization-owned Repositories**:

When an organization owns the repository, you need a fine-grained PAT with the resource owner set to the organization:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Resource owner**: The organization that owns the repository
   - **Repository access**: Select the specific repositories that will use the workflow
   - **Repository permissions**:
     - Actions: Write
     - Contents: Write
     - Issues: Write
     - Pull requests: Write
   - **Important**: You must set the resource owner to the organization during token creation

2. Add to repository secrets:

```bash wrap
gh aw secrets set GH_AW_AGENT_TOKEN --value "YOUR_AGENT_PAT"
```

**Token precedence**: per-output → global safe-outputs → workflow-level → `GH_AW_AGENT_TOKEN` (no further fallback - must be explicitly configured)

> [!NOTE]
> Two ways to assign Copilot agents
> 
> There are two different methods for assigning GitHub Copilot agents to issues or pull requests. **Both methods use the same token (`GH_AW_AGENT_TOKEN`) and GraphQL API** to perform the assignment:
> 
> 1. **Via `assign-to-agent` safe output**: Use when you need to programmatically assign agents to **existing** issues or PRs through workflow automation. This is a standalone operation that requires the token documented on this page.
> 
>    ```yaml
>    safe-outputs:
>      assign-to-agent:
>        name: "copilot"
>        allowed: [copilot]
>    ```
> 
> 2. **Via `assignees` field in `create-issue`**: Use when creating new issues through workflows and want to assign the agent immediately. When `copilot` is in the assignees list, it's automatically filtered out and assigned via GraphQL in a separate step after issue creation (using the same token and API as method 1).
> 
>    ```yaml
>    safe-outputs:
>      create-issue:
>        assignees: copilot  # or assignees: [copilot, user1]
>    ```
> 
> Both methods result in the same outcome as [manually assigning issues to Copilot through the GitHub UI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot). Method 2 is simpler when creating issues, while method 1 provides fine-grained control for existing issues.
> 
> **Technical Implementation**: Both methods use the GraphQL `replaceActorsForAssignable` mutation to assign the `copilot-swe-agent` bot to issues or PRs. The token precedence for both is: per-output → global safe-outputs → workflow-level → `GH_AW_AGENT_TOKEN` (with fallback to `GH_AW_GITHUB_TOKEN` or `GITHUB_TOKEN` if not set).
> 
> See [GitHub's official documentation on assigning issues to Copilot](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) for more details on the Copilot coding agent.

> [!NOTE]
> Resource owner requirements
> The token's resource owner must match the repository ownership:
> - **User-owned repositories**: Use a token where the resource owner is your user account
> - **Organization-owned repositories**: Use a token where the resource owner is the organization
>
> This ensures the token has the appropriate permissions to assign agents to issues and pull requests in the repository.

## `GITHUB_MCP_SERVER_TOKEN` (Auto-configured)

**Type**: Automatically set by the compiler (do not configure manually)

This environment variable is automatically set by gh-aw based on your GitHub tools configuration. Configure tokens using `GH_AW_GITHUB_TOKEN`, `GH_AW_GITHUB_MCP_SERVER_TOKEN`, or workflow-level `github-token` instead.

## Token Configuration Patterns

### Per-Output vs Global vs Workflow-Level

You can configure tokens at three levels with different precedence:

```yaml wrap
# Workflow-level (applies to all operations by default)
github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}

safe-outputs:
  # Global safe-outputs level (overrides workflow-level for all outputs)
  github-token: ${{ secrets.GLOBAL_PAT }}
  
  create-issue:
    # Per-output level (highest priority)
    github-token: ${{ secrets.ISSUE_PAT }}
    target-repo: "org/other-repo"
  
  create-pull-request:
    # Automatically uses Copilot token chain when copilot is reviewer
    reviewers: copilot
```

### Cross-Repository Operations

Cross-repository operations always require `GH_AW_GITHUB_TOKEN` or a custom PAT with access to the target repositories:

```yaml wrap
github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}

safe-outputs:
  create-issue:
    target-repo: "org/tracking-repo"  # Requires PAT with access to org/tracking-repo
  
  add-comment:
    target-repo: "org/another-repo"  # Requires PAT with access to org/another-repo
```

### Remote GitHub Tools Mode

Remote mode requires a PAT because the default `GITHUB_TOKEN` is not supported:

```yaml wrap
github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}  # Required for remote mode

tools:
  github:
    mode: remote  # Faster startup, no Docker required
    toolsets: [default]
```

### Copilot Operations

Copilot operations require a PAT with "Copilot Requests" permission:

```yaml wrap
engine: copilot

# Option 1: Configure COPILOT_GITHUB_TOKEN secret (recommended)
# No workflow configuration needed - automatically used

# Option 2: Explicitly configure in workflow
github-token: ${{ secrets.COPILOT_GITHUB_TOKEN }}
```

## GitHub App Tokens

GitHub App installation tokens provide enhanced security with short-lived, automatically-revoked credentials. This is the recommended approach for production workflows.

**Benefits**:

- **On-demand minting**: Tokens created at job start, minimizing exposure window
- **Short-lived**: Tokens automatically revoked at job end (even on failure)
- **Automatic permissions**: Compiler calculates required permissions based on safe outputs
- **Audit trail**: All actions logged under the GitHub App identity
- **No PAT rotation**: Eliminates need for manual token rotation

**Setup**:

```yaml wrap
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: "my-org"                    # Optional: defaults to current repo owner
    repositories: ["repo1", "repo2"]   # Optional: defaults to current repo only
  
  create-issue:
    # No github-token needed - uses App token automatically
  
  create-pull-request:
    # Permissions computed based on safe output types
```

**Permission mapping**:

- `create-issue:` → Issues: Write
- `create-pull-request:` → Contents: Write, Pull requests: Write
- `add-comment:` → Issues: Write
- `add-labels:` → Issues: Write
- `update-issue:` → Issues: Write
- `create-agent-session:` → Actions: Write, Contents: Write

**Configuration inheritance**:
App configuration can be imported from shared workflows. Local configuration takes precedence:

```yaml wrap
imports:
  - shared/common-app.md  # Defines app: config

safe-outputs:
  app:
    repositories: ["repo3"]  # Overrides imported config
  create-issue:
```

### GitHub App Tokens for GitHub MCP Server

GitHub App tokens can also be used to authenticate the GitHub MCP server, providing the same security benefits as safe-outputs while isolating MCP server permissions.

**Benefits**:

- **Isolated permissions**: GitHub MCP server uses separate token from safe-outputs
- **On-demand minting**: Token created at workflow start, minimized exposure
- **Automatic revocation**: Token invalidated at workflow end (even on failure)
- **Permission mapping**: Automatically computed from agent job `permissions` field
- **Dual mode support**: Works with both local (Docker) and remote (hosted) modes

**Setup**:

Configure the GitHub App directly in the tools configuration:

```yaml wrap
permissions:
  contents: read
  issues: write
  pull-requests: read

tools:
  github:
    mode: remote  # or "local" for Docker-based
    toolsets: [repos, issues, pull_requests]
    app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      owner: "my-org"                    # Optional: defaults to current repo owner
      repositories: ["repo1", "repo2"]   # Optional: defaults to current repo only
```

**Shared workflow pattern** (recommended):

Use the provided shared workflow for centralized configuration:

```yaml wrap
imports:
  - shared/github-mcp-app.md  # Provides APP_ID and APP_PRIVATE_KEY configuration

permissions:
  contents: read
  issues: write

tools:
  github:
    toolsets: [repos, issues, pull_requests]
```

The shared workflow (`shared/github-mcp-app.md`) expects:
- **Repository Variable**: `APP_ID` - Your GitHub App ID
- **Repository Secret**: `APP_PRIVATE_KEY` - Your GitHub App private key

**How it works**:

1. At workflow start, a token minting step (`github-mcp-app-token`) is automatically inserted before MCP server setup
2. The token is minted with permissions matching the agent job's `permissions` field
3. Token is passed to the GitHub MCP server as `GITHUB_MCP_SERVER_TOKEN`
4. At workflow end, the token is automatically invalidated (even on failure)

**Token precedence**:

When a GitHub App is configured for the GitHub MCP server:

1. GitHub App token (highest priority)
2. `tools.github.github-token` (custom token)
3. `GH_AW_GITHUB_MCP_SERVER_TOKEN` (dedicated MCP token)
4. `GH_AW_GITHUB_TOKEN` (general enhanced token)
5. `GITHUB_TOKEN` (default Actions token - not supported in remote mode)

**Setup repository variables**:

```bash wrap
# Set GitHub App ID as repository variable
gh variable set APP_ID --body "123456"

# Set GitHub App private key as repository secret
gh aw secrets set APP_PRIVATE_KEY --value "$(cat path/to/private-key.pem)"
```

> [!TIP]
> Dual App Configuration
> You can configure GitHub Apps for both safe-outputs and GitHub MCP server independently:
> 
> ```yaml
> tools:
>   github:
>     app:
>       app-id: ${{ vars.MCP_APP_ID }}
>       private-key: ${{ secrets.MCP_APP_PRIVATE_KEY }}
> 
> safe-outputs:
>   app:
>     app-id: ${{ vars.SAFE_OUTPUTS_APP_ID }}
>     private-key: ${{ secrets.SAFE_OUTPUTS_APP_PRIVATE_KEY }}
> ```
> 
> This allows different permission levels for MCP server operations versus safe output operations.

> [!NOTE]
> Permission Requirements
> The GitHub App must have sufficient permissions for the operations you need:
> - **Read operations**: Contents: Read, Issues: Read, Pull requests: Read
> - **Write operations**: Match the permissions in your workflow's `permissions` field
> - **Organization access**: If accessing org-owned repositories, the app must be installed at the organization level

## Token Selection Guide

Use this guide to choose the right token for your workflow:

| Scenario | Recommended Token | Alternative |
|----------|------------------|-------------|
| Single repository, basic operations | `GITHUB_TOKEN` (default) | None needed |
| Cross-repository operations | `GH_AW_GITHUB_TOKEN` | GitHub App |
| Copilot engine workflows | `COPILOT_GITHUB_TOKEN` | None |
| Remote GitHub MCP mode | `GH_AW_GITHUB_TOKEN` | GitHub App via `tools.github.app` |
| GitHub MCP server (isolated permissions) | GitHub App via `tools.github.app` | `GH_AW_GITHUB_MCP_SERVER_TOKEN` |
| Agent assignments | `GH_AW_AGENT_TOKEN` | `GH_AW_GITHUB_TOKEN` with elevated permissions |
| GitHub Projects v2 operations | `GH_AW_PROJECT_GITHUB_TOKEN` | `GH_AW_GITHUB_TOKEN` with Projects permissions |
| Production workflows | GitHub App (safe-outputs and MCP) | `GH_AW_GITHUB_TOKEN` with fine-grained PAT |

## Security Best Practices

### Principle of Least Privilege

Always use the minimal `permissions:` in your workflow and let safe outputs handle API access with tokens:

```yaml wrap
permissions:
  contents: read  # Minimal workflow permissions

safe-outputs:
  github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}
  create-issue:  # Token handles API authentication, not workflow permissions
```

### Token Scoping

Scope different operations with different tokens when they need different permission levels:

```yaml wrap
safe-outputs:
  create-issue:
    github-token: ${{ secrets.READ_WRITE_PAT }}
    target-repo: "org/public-issues"
  
  create-pull-request:
    github-token: ${{ secrets.LIMITED_PAT }}
    target-repo: "org/code-repo"
```

### Prefer GitHub Apps

Use GitHub Apps for production workflows whenever possible:

- Better security (short-lived tokens)
- Better auditability (app identity in logs)
- No credential rotation needed
- Automatic permission management

### PAT Best Practices

When using Personal Access Tokens:

1. **Use fine-grained PATs** over classic PATs
2. **Set short expiration periods** (90 days or less)
3. **Implement rotation schedules** before expiration
4. **Limit repository access** to only what's needed
5. **Use separate tokens** for different permission levels
6. **Monitor token usage** in organization audit logs

### Avoid Common Pitfalls

**Don't**: Hardcode tokens in workflows

```yaml wrap
github-token: "ghp_xxxxxxxxxxxx"  # ❌ Never do this
```

**Do**: Use secrets

```yaml wrap
github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}  # ✅ Correct
```

**Don't**: Use overly permissive tokens

```yaml wrap
# ❌ Classic PAT with full repo access for simple issue creation
github-token: ${{ secrets.ADMIN_TOKEN }}
```

**Do**: Use appropriately scoped tokens

```yaml wrap
# ✅ Fine-grained PAT with Issues: Write only
github-token: ${{ secrets.ISSUE_TOKEN }}
```

## Common Workflow Examples

**Basic (single repository)**: Uses default `GITHUB_TOKEN` - no configuration needed

**Cross-repository**: Set `github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}` at workflow level

**Copilot operations**: Automatically uses `COPILOT_GITHUB_TOKEN` when configured

**Different permissions per output**: Override at per-output level with different PATs

**Production (most secure)**: Configure GitHub App with `app-id` and `private-key` in safe-outputs and/or tools.github

**GitHub MCP with App**: Import `shared/github-mcp-app.md` and configure `APP_ID` + `APP_PRIVATE_KEY` repository variables

## Troubleshooting

### "Resource not accessible by integration"

Token lacks required permissions. Configure the appropriate token:
- Cross-repository: `gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_PAT"`
- Copilot operations: `gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_COPILOT_PAT"`
- Agent assignments: `gh aw secrets set GH_AW_AGENT_TOKEN --value "YOUR_AGENT_PAT"`

Required PAT scopes: Issues (Read+Write), Pull requests (Read+Write), Contents (Read+Write), Copilot Requests (for Copilot)

### Token not being used

Check token precedence configuration. Tokens are resolved in order: per-output → global safe-outputs → workflow-level → default fallback. Set at the appropriate level based on your needs.

### Remote GitHub Tools requires authentication

Remote mode does not support `GITHUB_TOKEN`. Either set `GH_AW_GITHUB_TOKEN` or switch to `mode: local` in tools configuration.

### Token expiration

PATs expire. Check expiration in [GitHub settings](https://github.com/settings/tokens), regenerate, and update secret. Consider implementing rotation schedules.

### Organization policies restrict PATs

Work with organization admin to request exemption, use organization-wide GitHub App, or request pre-approved fine-grained PAT.

## Quick Reference

### Token Setup Commands

```bash wrap
# Copilot operations
gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_COPILOT_PAT"

# Enhanced GitHub token (most common, current repository)
gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_PAT"

# Agent assignments
gh aw secrets set GH_AW_AGENT_TOKEN --value "YOUR_AGENT_PAT"

# GitHub Projects v2 operations (optional)
gh aw secrets set GH_AW_PROJECT_GITHUB_TOKEN --value "YOUR_PROJECT_PAT"

# Custom MCP server token (optional)
gh aw secrets set GH_AW_GITHUB_MCP_SERVER_TOKEN --value "YOUR_PAT"

# List configured secrets (GitHub CLI)
gh secret list -a actions
```

### Token Precedence Summary

All operations follow the same general pattern: **per-output → global safe-outputs → workflow-level → specific secret → fallback**

Specific fallback chains are documented in each token's section above. Note: Copilot operations use `COPILOT_GITHUB_TOKEN` (not `GITHUB_TOKEN`), and agent assignments use `GH_AW_AGENT_TOKEN` with no further fallback.

### Required PAT Permissions

| Operation Type | Required Permissions |
|---------------|---------------------|
| Cross-repository read | Contents: Read |
| Cross-repository issues | Issues: Read+Write, Contents: Read |
| Cross-repository PRs | Pull requests: Read+Write, Contents: Read+Write |
| Copilot operations (user-owned repos) | Copilot Requests: Read-only |
| Copilot operations (org-owned repos) | Copilot Requests: Read-only + Organization permissions: Members (read-only), GitHub Copilot Business (read-only) |
| Agent assignments | Actions: Write, Contents: Write, Issues: Write, Pull requests: Write |
| GitHub Projects v2 | Projects: Read+Write (org-level for org Projects) |
| Remote GitHub MCP | Contents: Read (minimum), adjust based on toolsets |

### Migration from Legacy Tokens

`COPILOT_CLI_TOKEN` and `GH_AW_COPILOT_TOKEN` are **no longer supported** (removed in v0.26+). Migrate to `COPILOT_GITHUB_TOKEN`:

```bash wrap
gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_PAT"
```

## Related Documentation

- [Engines](/gh-aw/reference/engines/) - Engine-specific authentication
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Safe output token configuration
- [Tools](/gh-aw/reference/tools/) - Tool authentication and modes
- [Permissions](/gh-aw/reference/permissions/) - Permission model overview
