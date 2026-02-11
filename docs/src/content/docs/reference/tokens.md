---
title: GitHub Tokens
description: Comprehensive reference for all GitHub tokens used in gh-aw, including authentication, token precedence, and security best practices
sidebar:
  order: 650
disable-agentic-editing: true
---

## Which token(s) do I need?

GitHub Actions automatically provides a default `GITHUB_TOKEN` that works for most workflows. However, depending on what your workflow needs to do, you may need additional tokens:

- **Cross-repo access or remote GitHub tools** – Add [`GH_AW_GITHUB_TOKEN`](#gh_aw_github_token-enhanced-pat-for-cross-repo-and-remote-tools)
- **Copilot engine and agent operations** – Add [`COPILOT_GITHUB_TOKEN`](#copilot_github_token-copilot-authentication)
- **GitHub Projects v2 operations** – Add [`GH_AW_PROJECT_GITHUB_TOKEN`](#gh_aw_project_github_token-github-projects-v2)
- **Assign Copilot agents to issues/PRs** – Add [`GH_AW_AGENT_TOKEN`](#gh_aw_agent_token-agent-assignment)
- **MCP server with isolated permissions** (optional) – Add [`GH_AW_GITHUB_MCP_SERVER_TOKEN`](#gh_aw_github_mcp_server_token-github-mcp-server)

## How do I add tokens to my repository?

You can set up your tokens manually in the GitHub UI or use the CLI for a streamlined experience. Note that the CLI also provides commands to check existing secrets and validate token permissions.

### Adding tokens using the CLI

<div style="padding-left: 1.5rem;">

```bash
gh aw secrets set COPILOT_GITHUB_TOKEN --value "YOUR_COPILOT_PAT"
```

You can also check existing secrets with:

```bash
gh aw secrets bootstrap
```

You can validate token permissions and configuration with:

```bash
gh aw init --tokens --engine <engine>
```

</div>

### Adding tokens using the GitHub UI

<div style="padding-left: 1.5rem;">

1. Go to your repository on GitHub
2. Click on "Settings" → "Secrets and variables" → "Actions"
3. Click "New repository secret" and add the token name and value

<div class="gh-aw-video-container" style="max-width: 800px; margin: 1.5rem 0;">
  <div class="gh-aw-video-wrapper">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="/gh-aw/images/actions-secrets_dark.png">
      <img alt="Repository secrets page showing configured tokens" src="/gh-aw/images/actions-secrets_light.png">
    </picture>
  </div>
  <div class="gh-aw-video-caption" role="note">
    Repository secrets in GitHub Actions settings showing three configured tokens
  </div>
</div>

</div>

## Who owns the repository?

Ownership affects token requirements for repositories and Projects (v2). If the owner is your personal username, it is user-owned. If the owner is an organization, it is org-owned and managed with shared roles and access controls.

To confirm ownership, check the owner name and avatar at the top of the page or in the URL (`github.com/owner-name/...`). Clicking the owner takes you to a personal profile or an organization page, which confirms it instantly. Here are examples of both (left: user-owned, right: org-owned):

<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr)); gap: 1rem; margin: 1.5rem 0; max-width: 800px;">
  <div class="gh-aw-video-container">
    <div class="gh-aw-video-wrapper">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="/gh-aw/images/user-owned_dark.png">
        <img alt="User-owned repository example" src="/gh-aw/images/user-owned_light.png">
      </picture>
    </div>
    <div class="gh-aw-video-caption" role="note">
      User-owned repository: avatar shows a personal profile icon, URL includes username
    </div>
  </div>

  <div class="gh-aw-video-container">
    <div class="gh-aw-video-wrapper">
      <picture>
        <source media="(prefers-color-scheme: dark)" srcset="/gh-aw/images/org-owned_dark.png">
        <img alt="Organization-owned repository example" src="/gh-aw/images/org-owned_light.png">
      </picture>
    </div>
    <div class="gh-aw-video-caption" role="note">
      Organization-owned repository: avatar shows organization icon, URL includes org name
    </div>
  </div>
</div>

## GitHub App tokens for GitHub MCP server

This section previously described how to configure GitHub App tokens for the GitHub MCP server. The guidance has been consolidated into the token reference sections below. Use the token descriptions and configuration details in **Token Reference** to choose and configure the appropriate token(s) for your workflows.

> Note: This stub section exists to preserve existing deep links that reference `#github-app-tokens-for-github-mcp-server`.
## Token Reference

### `GITHUB_TOKEN` {#github-token}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #60a5fa; color: white; font-size: 0.875rem; font-weight: 500;">Automatically provided</span>

<div style="padding-left: 1.5rem;">

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

</div>

---

### `GH_AW_GITHUB_TOKEN` {#enhanced-pat-for-cross-repo-and-remote-tools}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #c084fc; color: white; font-size: 0.875rem; font-weight: 500;">Personal Access Token</span>

<div style="padding-left: 1.5rem;">

> [!IMPORTANT]
> **Required** if you need cross-repository access or remote GitHub tools mode

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

</div>


---

### `GH_AW_GITHUB_MCP_SERVER_TOKEN` {#github-mcp-server}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #c084fc; color: white; font-size: 0.875rem; font-weight: 500;">Personal Access Token</span> (optional override)

<div style="padding-left: 1.5rem;">

> [!TIP]
> **Optional** – Use only if you need to isolate GitHub MCP server permissions

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

</div>


---

### `GH_AW_PROJECT_GITHUB_TOKEN` {#github-projects-v2}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #c084fc; color: white; font-size: 0.875rem; font-weight: 500;">Personal Access Token</span>

<div style="padding-left: 1.5rem;">

> [!IMPORTANT]
> **Required** for any GitHub Projects v2 operations (creating, updating, or reading project boards)

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

<div class="gh-aw-video-wrapper">
  <video 
    controls
    muted 
    playsinline 
    poster="/gh-aw/videos/create-pat-user-project.png"
    style="width: 100%; aspect-ratio: 16/9;"
  >
    <source src="/gh-aw/videos/create-pat-user-project.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
  <div class="gh-aw-video-caption" role="note">
    Creating a classic PAT for user-owned private projects
  </div>
</div>

You **must** use a **classic PAT** with the `project` scope. Fine-grained PATs do **not** work with user-owned Projects.

1. Create a [classic PAT](https://github.com/settings/tokens/new) with scopes:
   - `project` (required for user Projects)
   - `repo` (required if accessing private repositories)

**For Organization-owned Projects (v2)**:

<div class="gh-aw-video-wrapper">
  <video 
    controls
    muted 
    playsinline 
    poster="/gh-aw/videos/create-pat-org-project.png"
    style="width: 100%; aspect-ratio: 16/9;"
  >
    <source src="/gh-aw/videos/create-pat-org-project.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
  <div class="gh-aw-video-caption" role="note">
    Creating a fine-grained PAT for organization-owned projects
  </div>
</div>

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

</div>


---

### `COPILOT_GITHUB_TOKEN` {#copilot-authentication}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #c084fc; color: white; font-size: 0.875rem; font-weight: 500;">Personal Access Token</span>

<div style="padding-left: 1.5rem;">

> [!IMPORTANT]
> **Required** for all Copilot operations (engine, agent sessions, and bot assignments)

The recommended token for all Copilot-related operations including the Copilot engine, agent session creation, and bot assignments.

**Required for**:

- `engine: copilot` workflows
- `create-agent-session:` safe outputs
- Assigning `copilot` as issue assignee
- Adding `copilot` as PR reviewer



**Setup**:

The required token type depends on whether you own the repository or an organization owns it:

**For User-owned Repositories**:

<div class="gh-aw-video-wrapper">
  <video 
    controls
    muted 
    playsinline 
    poster="/gh-aw/videos/create-pat-user-copilot.png"
    style="width: 100%; aspect-ratio: 16/9;"
  >
    <source src="/gh-aw/videos/create-pat-user-copilot.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
  <div class="gh-aw-video-caption" role="note">
    Creating a fine-grained PAT for user-owned repositories with Copilot permissions
  </div>
</div>

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new) with:
   - **Resource owner**: Your user account
   - **Repository access**: "Public repositories" or select specific repos
     - **Note**: You should leave "Public repositories" enabled; otherwise, you will not have access to the Copilot Requests permission option.
   - **Permissions**: 
     - Copilot Requests: Read-only (required)

**For Organization-owned Repositories**:

<div class="gh-aw-video-wrapper">
  <video 
    controls 
    muted 
    playsinline 
    poster="/gh-aw/videos/create-pat-org-copilot.png"
    style="width: 100%; aspect-ratio: 16/9;"
  >
    <source src="/gh-aw/videos/create-pat-org-copilot.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
  <div class="gh-aw-video-caption" role="note">
    Creating a fine-grained PAT for organization-owned repositories with Copilot permissions
  </div>
</div>

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

</div>


---

### `GH_AW_AGENT_TOKEN` {#agent-assignment}
<span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #c084fc; color: white; font-size: 0.875rem; font-weight: 500;">Personal Access Token</span>

<div style="padding-left: 1.5rem;">

> [!IMPORTANT]
> **Required** if you need to programmatically assign Copilot agents to issues or PRs

Primary token used by `assign-to-agent:` safe outputs to programmatically assign GitHub Copilot agents to issues or pull requests. This is distinct from the standard GitHub UI workflow for [assigning issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) and is used specifically for automated agent assignment through workflow safe outputs.

**Required for**:

- `assign-to-agent:` safe outputs
- Programmatic agent assignment operations

**Token precedence and fallback**:

- If `GH_AW_AGENT_TOKEN` is set, it is always used for agent assignment.
- If `GH_AW_AGENT_TOKEN` is _not_ set, `gh-aw` falls back to `GH_AW_GITHUB_TOKEN`, and then to `GITHUB_TOKEN`, matching the runtime resolution `GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN`.

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

<div class="gh-aw-video-wrapper">
  <video 
    controls
    muted 
    playsinline 
    poster="/gh-aw/videos/create-pat-org-agent.png"
    style="width: 100%; aspect-ratio: 16/9;"
  >
    <source src="/gh-aw/videos/create-pat-org-agent.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
  <div class="gh-aw-video-caption" role="note">
    Creating a fine-grained PAT for organization-owned repositories with permissions for agent assignment
  </div>
</div>

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
> **Technical Implementation**: Both methods use the GraphQL `replaceActorsForAssignable` mutation to assign the `copilot-swe-agent` bot to issues or PRs. Both follow the token precedence and fallback behavior documented above.
> 
> See [GitHub's official documentation on assigning issues to Copilot](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) for more details on the Copilot coding agent.

> [!NOTE]
> Resource owner requirements
> The token's resource owner must match the repository ownership:
> - **User-owned repositories**: Use a token where the resource owner is your user account
> - **Organization-owned repositories**: Use a token where the resource owner is the organization
>
> This ensures the token has the appropriate permissions to assign agents to issues and pull requests in the repository.

</div>


---

### `GITHUB_MCP_SERVER_TOKEN` (Automatically Configured) <span style="display: inline-block; padding: 0.25rem 0.75rem; border-radius: 9999px; background-color: #60a5fa; color: white; font-size: 0.875rem; font-weight: 500;">Automatically set</span>

<div style="padding-left: 1.5rem;">

> [!TIP]
> **Do not configure manually** – Automatically managed by gh-aw compiler

This environment variable is automatically set by gh-aw based on your GitHub tools configuration. Configure tokens using `GH_AW_GITHUB_TOKEN`, `GH_AW_GITHUB_MCP_SERVER_TOKEN`, or workflow-level `github-token` instead.

</div>


---

## Related Documentation

- [Engines](/gh-aw/reference/engines/) - Engine-specific authentication
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Safe output token configuration
- [Tools](/gh-aw/reference/tools/) - Tool authentication and modes
- [Permissions](/gh-aw/reference/permissions/) - Permission model overview
