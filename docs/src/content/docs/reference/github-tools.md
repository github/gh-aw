---
title: GitHub Tools (for reading from GitHub)
description: Configure reading information from GitHub, including integrity filtering, repository access restrictions, cross-repository access, remote mode, and additional authentication.
sidebar:
  order: 710
---

The GitHub Tools (`tools.github`) let the agentic step read information such as issues and pull requests from GitHub. Most workflows need no extra configuration: the default toolset reads the current repository and public repositories, subject to the network firewall.

## GitHub Toolsets

You can enable specific API groups to increase the available tools or narrow the default selection:

```yaml wrap
tools:
  github:
    toolsets: [repos, issues, pull_requests, actions]
```

**Available**: `context`, `repos`, `issues`, `pull_requests`, `users`, `actions`, `code_security`, `discussions`, `labels`, `notifications`, `orgs`, `projects`, `gists`, `dependabot`, `secret_protection`, `security_advisories`, `stargazers`

**Shorthand values**:

- `default` — expands to `context`, `repos`, `issues`, `pull_requests`, `users`
- `all` — expands to all available toolsets **except** `dependabot` (see note below)

**Default**: `context`, `repos`, `issues`, `pull_requests`, `users`

`repos` covers repository operations, code search, commits, and releases; `actions` covers workflows, runs, and artifacts.

:::note
`toolsets: [all]` does **not** include `dependabot`. Opt into it explicitly and see [Using the `dependabot` toolset](#using-the-dependabot-toolset) for authentication. Some other toolsets also require [additional authentication](#additional-authentication-for-github-tools).
:::

### Code Search Rate Limits

The MCP `search_code` tool has a much smaller budget than other GitHub reads: [GitHub's search rate limits](https://docs.github.com/en/rest/search/search#rate-limit) allow 10 authenticated code-search requests per minute, versus 30 for other search endpoints. An available `core` quota does not mean code search is available. Agent start and end snapshots log the `core`, `code_search`, and `search` limits, and MCP HTTP 429 failures appear in the agent job summary with any retry hint.

To inspect code in another repository, prefer [`checkout:` with `sparse-checkout`](/gh-aw/reference/checkout/) and then search locally with `grep` or `rg` (supported since v0.89.17):

```aw wrap
permissions:
  contents: read
checkout:
  - repository: owner/target-repo
    ref: main
    path: target-repo
    sparse-checkout: |
      src
      docs
    github-token: ${{ secrets.TARGET_REPO_READ_TOKEN }}
tools:
  bash: ["grep", "rg"]
```

The checkout token must have read access to the target repository, since `GITHUB_TOKEN` is scoped to the workflow's own repository. This credential is separate from `tools.github` authentication. For occasional API code searches, set a small [`max-calls` limit](#restricting-tools-toolsgithuballowed); it does not reserve quota or prevent throttling.

## Restricting Tools (`tools.github.allowed`)

`tools.github.allowed` limits which GitHub MCP tools the agent can call. Entries are either a tool name (unlimited calls) or an object with a `max-calls` cap:

```yaml wrap
tools:
  github:
    allowed:
      - name: issue_read
        max-calls: 1
      - list_labels
      - pull_request_read
```

`max-calls` must be a positive integer, enforced by the MCP gateway at runtime. The shorthand `"issue_read:1"` is **not** a call limit; it is treated as a literal, unknown tool name.

`toolsets` selects which API groups load, while `allowed` narrows which individual tools can be called and how often.

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

By default, the GitHub Tools read the current repository and public repositories, subject to the network firewall. For private repositories, configure additional authentication, and optionally narrow access with `tools.github.allowed-repos`. See [Cross-Repository Operations](/gh-aw/reference/cross-repository/).

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

**`gh-proxy` mode** — uses the pre-authenticated `gh` CLI directly, with no MCP server startup overhead. It is required for [integrity reactions](/gh-aw/reference/integrity/). The legacy `features: {cli-proxy: true}` flag is equivalent and still accepted.

```yaml wrap
tools:
  github:
    mode: gh-proxy
```

### CLI proxy startup diagnostics

The CLI proxy waits up to 30 seconds for readiness. Diagnostics are captured outside artifact upload roots (1 MiB input limit). On failure, the last 20 lines are printed after redacting credentials, including a custom `GH_TOKEN`; unrelated workflow secrets are not forwarded. If capture exceeds the limit or redaction fails, the diagnostics are withheld and a fixed error is reported. Timeout diagnostics keep the readiness failure reason, and a cleanup failure fails the step.

## Additional Authentication for GitHub Tools

Some workflows need a GitHub PAT or GitHub App to read more than the default scope allows: org or user information, other private repositories, projects, or [remote mode](#github-tools-access-modes). This authentication is for **reading** only; writes are configured through [Safe Outputs](/gh-aw/reference/safe-outputs/).

### Using a Personal Access Token (PAT)

Create a fine-grained PAT, store it as a repository secret, and reference it in your workflow:

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

A GitHub App is the more secure option. See [Using a GitHub App for Authentication](/gh-aw/reference/auth/#using-a-github-app-for-authentication) for complete setup instructions.

### Using a magic secret

Alternatively, set the magic secret `GH_AW_GITHUB_MCP_SERVER_TOKEN` to a suitable PAT. GitHub Agentic Workflows recognizes this name automatically, so it need not be referenced in frontmatter.

```bash wrap
gh aw secrets set GH_AW_GITHUB_MCP_SERVER_TOKEN --value "<your-pat-token>"
```

### Using the `dependabot` toolset

The `dependabot` toolset needs `vulnerability-alerts: read` and `security-events: read` in `permissions:`, which `GITHUB_TOKEN` supports natively:

```yaml
permissions:
  vulnerability-alerts: read
  security-events: read
```

A PAT or GitHub App also works. For an App, grant it `vulnerability-alerts: read` as well.

## Cross-Visibility Opt-Out (`private-to-public-flows`)

In public repositories, the MCP Gateway enforces _cross-visibility protections_ that stop private repository data from reaching public sinks:

- **`forcePublicRepos`** — restricts the GitHub MCP server to public repositories at runtime.
- **`sink-visibility` enforcement** — blocks output to a `public` sink when the agent holds secrecy tags from private-repo reads.

To intentionally allow private-to-public flows, declare `private-to-public-flows` under `tools.github`.

### Blanket opt-out (`allow`)

```yaml
tools:
  github:
    private-to-public-flows: allow
```

This disables both `forcePublicRepos` and `sink-visibility` enforcement for **all** MCP servers (the gateway emits `"forcePublicRepos": false`).

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

This exempts only the listed server IDs from `sink-visibility` enforcement. `forcePublicRepos` stays in effect. The form is rejected in strict mode. The compiler emits the IDs as `"sinkVisibilityExemptServers"`; the built-in GitHub server ID is `github`, and custom IDs match their `mcp-servers` key.

### Security implications

Opting out of cross-visibility protections means the agent may read from private repositories and write that data to public sinks (e.g., a public GitHub issue, a Slack channel). Only use this when your workflow explicitly requires it and you understand the data-flow implications.

See [MCP Gateway Specification Section 10.9](/gh-aw/reference/mcp-gateway/#109-cross-visibility-opt-out-private-to-public-flows) for full protocol details.

## Learn More

- [Tools Reference](/gh-aw/reference/tools/) - All tool configurations
- [Authentication Reference](/gh-aw/reference/auth/) - Token setup and permissions
- [Integrity Filtering](/gh-aw/reference/integrity/) - Public repository content filtering
- [MCPs Guide](/gh-aw/guides/mcps/) - Model Context Protocol setup
