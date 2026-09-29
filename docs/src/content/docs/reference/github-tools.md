---
title: GitHub Tools (for reading from GitHub)
description: Configure reading information from GitHub, including integrity filtering, repository access restrictions, cross-repository access, remote mode, and additional authentication.
sidebar:
  order: 710
---

The GitHub Tools (`tools.github`) let the agentic step read information such as issues and pull requests from GitHub.

In most workflows, no extra configuration is needed. The default toolset already provides access to the current repository and public repositories, subject to the network firewall.

## GitHub Toolsets

You can enable specific API groups to increase the available tools or narrow the default selection:

```yaml wrap
tools:
  github:
    toolsets: [repos, issues, pull_requests, actions]
```

**Available**: `context`, `repos`, `issues`, `pull_requests`, `users`, `actions`, `code_security`, `discussions`, `labels`, `notifications`, `orgs`, `projects`, `gists`, `search`, `dependabot`, `experiments`, `secret_protection`, `security_advisories`, `stargazers`

**Shorthand values**:

- `default` — expands to `context`, `repos`, `issues`, `pull_requests`, `users`
- `all` — expands to all available toolsets **except** `dependabot` (see note below)

**Default**: `context`, `repos`, `issues`, `pull_requests`, `users`

Common toolsets include `context` (user and team info), `repos` (repository operations, code search, commits, releases), `issues`, `pull_requests`, `actions` (workflows, runs, artifacts), `code_security`, `discussions`, and `labels`.

:::note
`toolsets: [all]` does **not** include the `dependabot` toolset. The `dependabot` toolset must be opted into explicitly. See [Using the `dependabot` toolset](#using-the-dependabot-toolset) for authentication requirements.
:::

Some toolsets require [additional authentication](#additional-authentication-for-github-tools).

## Restricting Tools (`tools.github.allowed`)

Use `tools.github.allowed` to restrict which GitHub MCP tools the agent can call. Each entry is either a string tool name or an object with a per-tool call limit:

```yaml wrap
tools:
  github:
    allowed:
      - name: issue_read
        max-calls: 1
      - list_labels
      - pull_request_read
```

- **String entries** (`list_labels`) — allow unlimited calls to that tool within the run.
- **Object entries** (`{ name: <tool>, max-calls: <n> }`) — cap how many times the tool can be invoked. `max-calls` must be a positive integer; the MCP gateway enforces the cap at runtime.

The shorthand form `"issue_read:1"` is **not** interpreted as a call limit — it is treated as a literal (and therefore unknown) tool name.

This complements toolset selection: `toolsets` decides which API groups are loaded, while `allowed` further narrows which individual tools the agent may invoke and how many times.

## GitHub Integrity Filtering (`tools.github.min-integrity`)

Sets the minimum integrity level required for content the agent can access. For public repositories, `min-integrity: approved` is applied automatically. See [Integrity Filtering](/gh-aw/reference/integrity/) for levels, examples, user blocking, and approval labels.

## Guard Policy Frontmatter Examples

The GitHub tool supports guard-policy fields directly under `tools.github`:

```yaml wrap
tools:
  github:
    allowed-repos:
      - "github/gh-aw"
      - "github/*"
    min-integrity: approved
    blocked-users:
      - "malicious-user"
    trusted-users:
      - "trusted-contributor"
    approval-labels:
      - "safe-for-agent"
      - "human-reviewed"
```

:::note
`tools.github.repos` is a deprecated alias of `tools.github.allowed-repos`. Use `allowed-repos` for new workflows.
:::

## GitHub Cross-Repository Reading

By default, the GitHub Tools can read from the current repository and all public repositories, subject to the network firewall. To read from other private repositories, configure additional authentication. You can further restrict repository access during execution with `tools.github.allowed-repos`. See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for details and examples.

## GitHub Tools Access Modes

The `tools.github.mode` field controls how the agent accesses GitHub. Three values are supported:

| Mode | Transport | Notes |
|------|-----------|-------|
| `local` (default) | Docker-based GitHub MCP Server inside the Actions VM | No extra authentication required |
| `remote` | Hosted GitHub MCP Server managed by GitHub | Requires [additional authentication](#additional-authentication-for-github-tools) |
| `gh-proxy` | Pre-authenticated `gh` CLI directly (no MCP server) | Preferred for performance; required for [integrity reactions](/gh-aw/reference/integrity/) |

**`remote` mode** — uses a hosted MCP server managed by GitHub. Requires a GitHub token with appropriate permissions:

```yaml wrap
tools:
  github:
    mode: remote
    github-token: ${{ secrets.CUSTOM_PAT }}  # Required for remote mode
```

**`gh-proxy` mode** — uses the pre-authenticated `gh` CLI directly instead of an MCP server. This offers lower latency because there is no MCP server startup overhead, and it is required for workflows that use [integrity reactions](/gh-aw/reference/integrity/). The legacy `features: {cli-proxy: true}` feature flag is equivalent and is still accepted for backward compatibility.

```yaml wrap
tools:
  github:
    mode: gh-proxy
```

## Additional Authentication for GitHub Tools

In some circumstances you must use a GitHub PAT or GitHub app to give the GitHub tools used by your workflow additional capabilities.

This authentication relates to **reading** information from GitHub. Additional authentication to write to GitHub is handled separately through various [Safe Outputs](/gh-aw/reference/safe-outputs/).

This is required when your workflow needs read access to org or user information, other private repositories, projects, or GitHub tools [remote mode](#github-tools-access-modes).

### Using a Personal Access Token (PAT)

If additional authentication is required, one way is to create a fine-grained PAT with appropriate permissions, add it as a repository secret, and reference it in your workflow:

1. Create a [fine-grained PAT](https://github.com/settings/personal-access-tokens/new?description=GitHub+Agentic+Workflows+-+GitHub+tools+access&contents=read&issues=read&pull_requests=read) (this link pre-fills the description and common read permissions) with repository access to the repos you need and read permissions that match your toolsets:

   - **Repository permissions**: `Contents` for `repos`; `Issues` for `issues`; `Pull requests` for `pull_requests`; `Projects` for `projects`; `Security events` for `dependabot`, `code_security`, `secret_protection`, and `security_advisories`.
   - **Organization permissions**: `Members` and `Teams` when you need org-level info in `context`.
   - **Remote mode**: no additional permission beyond the required repo or org reads.

2. Add it to your repository secrets, either by CLI or GitHub UI:

   ```bash wrap
   gh aw secrets set MY_PAT_FOR_GITHUB_TOOLS --value "<your-pat-token>"
   ```

3. Configure in your workflow frontmatter:

   ```yaml wrap
   tools:
     github:
       github-token: ${{ secrets.MY_PAT_FOR_GITHUB_TOOLS }}
   ```

### Using a GitHub App

Alternatively, you can use a GitHub App for enhanced security. See [Using a GitHub App for Authentication](/gh-aw/reference/auth/#using-a-github-app-for-authentication) for complete setup instructions.

### Using a magic secret

Alternatively, you can set the magic secret `GH_AW_GITHUB_MCP_SERVER_TOKEN` to a suitable PAT (see the above guide for creating one). This secret name is known to GitHub Agentic Workflows and does not need to be explicitly referenced in your workflow.

```bash wrap
gh aw secrets set GH_AW_GITHUB_MCP_SERVER_TOKEN --value "<your-pat-token>"
```

### Using the `dependabot` toolset

The `dependabot` toolset requires the `vulnerability-alerts: read` and `security-events: read` permissions. These are now supported natively by `GITHUB_TOKEN`. Add them to your workflow's `permissions:` field:

```yaml
permissions:
  vulnerability-alerts: read
  security-events: read
```

Alternatively, you can authenticate with a PAT or GitHub App. If using a GitHub App, add `vulnerability-alerts: read` to your workflow's `permissions:` field and ensure the GitHub App is configured with this permission.

## Cross-Visibility Opt-Out (`private-to-public-flows`)

By default, the MCP Gateway enforces _cross-visibility protections_ that prevent private repository data from flowing into public-repository sinks. These protections are active whenever the workflow runs in a public repository:

- **`forcePublicRepos`** — restricts the GitHub MCP server to public repositories only at runtime, preventing private-data secrecy tags from accumulating in the agent's context.
- **`sink-visibility` enforcement** — blocks any output to a sink whose visibility is `public` when the agent carries non-empty secrecy tags from private-repo reads.

When you intentionally need private-to-public data flows, declare `private-to-public-flows` under `tools.github`.

### Blanket opt-out (`allow`)

```yaml
tools:
  github:
    private-to-public-flows: allow
```

This disables both `forcePublicRepos` and the default `sink-visibility` enforcement for **all** MCP servers. The MCP Gateway emits `"forcePublicRepos": false` in its startup config.

:::caution
`private-to-public-flows` is **not compatible with strict mode** in either form. In non-strict mode, the compiler warns because GitHub Actions logs are public and agents may expose private data through public destinations.
:::

### Selective exemption (list of server IDs)

```yaml
tools:
  github:
    private-to-public-flows:
      - github
      - my-custom-server
mcp-servers:
  my-custom-server:
    type: http
    url: "http://localhost:9000/mcp"
```

This exempts only the listed MCP server IDs from the default `sink-visibility` enforcement. `forcePublicRepos` is **not** disabled; private repo access still requires the allow-only policy to permit it. This form is rejected in strict mode.

The compiler emits `"sinkVisibilityExemptServers": ["github", "my-custom-server"]` in the gateway config. The built-in GitHub MCP server ID is `github`. Custom server IDs match the key used in `mcp-servers`.

### Security implications

Opting out of cross-visibility protections means the agent may read from private repositories and write that data to public sinks (e.g., a public GitHub issue, a Slack channel). Only use this when your workflow explicitly requires it and you understand the data-flow implications.

See [MCP Gateway Specification Section 10.9](/gh-aw/reference/mcp-gateway/#109-cross-visibility-opt-out-private-to-public-flows) for full protocol details.

## Learn More

- [Tools Reference](/gh-aw/reference/tools/) - All tool configurations
- [Authentication Reference](/gh-aw/reference/auth/) - Token setup and permissions
- [Integrity Filtering](/gh-aw/reference/integrity/) - Public repository content filtering
- [MCPs Guide](/gh-aw/guides/mcps/) - Model Context Protocol setup
