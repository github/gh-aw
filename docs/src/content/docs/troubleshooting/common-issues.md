---
title: Common Issues
description: Frequently encountered issues when working with GitHub Agentic Workflows and their solutions.
sidebar:
  order: 200
---

This reference documents frequently encountered issues when working with GitHub Agentic Workflows, organized by workflow stage and component.

## Installation Issues

### Extension Installation Fails

If `gh extension install github/gh-aw` fails with authentication or permission errors, use the standalone installer:

```bash wrap
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
```

The installer works in restricted networks (Codespaces, MITM proxies) and installs to `~/.local/share/gh/extensions/gh-aw/gh-aw`.

### Install Specific Version

Install specific versions by passing the version tag as an argument. Find available versions at the [releases page](https://github.com/github/gh-aw/releases).

```bash wrap
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash -s -- v0.40.0
```

### Extension Not Found After Installation

Verify installation with `gh extension list`. If not listed, reinstall with `gh extension install github/gh-aw` or use the standalone installer.

### Codespace Authentication Issues

GitHub Codespaces may have limited permissions for installing extensions. Try the standalone installer (recommended) or grant additional permissions to your Codespace token.

## Organization Policy Issues

### Custom Actions Not Allowed in Enterprise Organizations

**Error Message:**

```text
The action github/gh-aw/actions/setup@a933c835b5e2d12ae4dead665a0fdba420a2d421 is not allowed in {ORG} because all actions must be from a repository owned by your enterprise, created by GitHub, or verified in the GitHub Marketplace.
```

**Cause:** Enterprise policies restrict which GitHub Actions can be used. Workflows use `github/gh-aw/actions/setup` which may not be allowed.

**Solution:** Enterprise administrators must allow `github/gh-aw` in the organization's action policies:

#### Option 1: Allow Specific Repositories (Recommended)

Add `github/gh-aw` to your organization's allowed actions list:

1. Navigate to your organization's settings: `https://github.com/organizations/YOUR_ORG/settings/actions`
2. Under **Policies**, select **Allow select actions and reusable workflows**
3. In the **Allow specified actions and reusable workflows** section, add:
   ```text
   github/gh-aw@*
   ```
4. Save the changes

See GitHub's docs on [managing Actions permissions](https://docs.github.com/en/organizations/managing-organization-settings/disabling-or-limiting-github-actions-for-your-organization#allowing-select-actions-and-reusable-workflows-to-run).

#### Option 2: Configure Organization-Wide Policy File

Add `github/gh-aw@*` to your centralized `policies/actions.yml` and commit to your organization's `.github` repository. See GitHub's docs on [community health files](https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions/creating-a-default-community-health-file).

```yaml
allowed_actions:
  - "actions/*"
  - "github/gh-aw@*"
```

#### Verification

Wait a few minutes for policy propagation, then re-run your workflow. If issues persist, verify at `https://github.com/organizations/YOUR_ORG/settings/actions`.

> [!TIP]
> The gh-aw actions are open source at [github.com/github/gh-aw/tree/main/actions](https://github.com/github/gh-aw/tree/main/actions) and pinned to specific SHAs for security.

## Repository Configuration Issues

### Actions Restrictions Reported During Init

The CLI validates three permission layers when running `gh aw init` or `gh aw add-wizard`:

**Actions disabled:** Enable in Repository Settings → Actions → General. [Docs](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository).

**Local-only restriction:** Switch to "Allow all actions" or "Allow select actions" with GitHub-created actions enabled. Workflows need `actions/checkout`, `actions/setup-node`, etc. [Docs](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#managing-github-actions-permissions-for-your-repository).

**Selective allowlist:** Enable "Allow actions created by GitHub" checkbox in Settings → Actions → General. [Docs](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#allowing-select-actions-and-reusable-workflows-to-run).

> [!NOTE]
> Organization-level policies override repository settings. Request changes from org admins if settings are grayed out.

## Workflow Compilation Issues

### Workflow Won't Compile

If `gh aw compile` fails, check YAML frontmatter syntax (proper indentation with spaces, colons with spaces after them), verify required fields like `on:` are present, and ensure field types match the schema. Use `gh aw compile --verbose` for detailed error messages.

### Lock File Not Generated

If `.lock.yml` isn't created, fix compilation errors first (`gh aw compile 2>&1 | grep -i error`) and verify write permissions on `.github/workflows/`.

### Orphaned Lock Files

Remove old `.lock.yml` files after deleting `.md` files with `gh aw compile --purge`.

## Import and Include Issues

### Import File Not Found

Import paths are relative to repository root. Verify the file exists with `git status`. Examples: `.github/workflows/shared/tools.md` or `shared/security-notice.md`.

### Multiple Agent Files Error

Import only one `.github/agents/` file per workflow. Use other imports for shared content.

### Circular Import Dependencies

If compilation hangs, check for circular import references and remove them.

## Tool Configuration Issues

### GitHub Tools Not Available

Configure GitHub tools using `toolsets:` (recommended) or verify tool names from the [tools reference](/gh-aw/reference/tools/):

```yaml wrap
tools:
  github:
    toolsets: [repos, issues]  # Recommended: use toolsets
```

### Toolset Missing Expected Tools

Check [toolset contents](/gh-aw/reference/tools/#toolset-contents) to find your tool, combine toolsets as needed (e.g., `toolsets: [default, actions]`), or run `gh aw mcp inspect <workflow>` to see available tools.

### MCP Server Connection Failures

Verify package installation, configuration syntax, and required environment variables:

```yaml
mcp-servers:
  my-server:
    command: "npx"
    args: ["@myorg/mcp-server"]
    env:
      API_KEY: "${{ secrets.MCP_API_KEY }}"
```

### Playwright Network Access Denied

Add blocked domains to the `allowed_domains` list:

```yaml wrap
tools:
  playwright:
    allowed_domains: ["github.com", "*.github.io"]
```

## Permission Issues

### Write Operations Fail

Use safe outputs, or ask for new safe output types if needed.

### Safe Outputs Not Creating Issues

Disable staged mode to create issues (not just preview):

```yaml wrap
safe-outputs:
  staged: false
  create-issue:
    title-prefix: "[bot] "
    labels: [automation]
```

### Token Permission Errors

Add permissions to `GITHUB_TOKEN` or use a custom token:

```yaml wrap
# Increase GITHUB_TOKEN permissions
permissions:
  contents: write
  issues: write

# Or use custom token
safe-outputs:
  github-token: ${{ secrets.CUSTOM_PAT }}
  create-issue:
```

### Project Field Type Errors

**Problem**: GitHub Projects reserves field types like `REPOSITORY` that cannot be updated via API.

**Solution**: Use alternative field names (`repo`, `source_repository`, `linked_repo`) instead of `repository`:

```yaml wrap
# ❌ Wrong
safe-outputs:
  update-project:
    fields:
      repository: "myorg/myrepo"

# ✅ Correct
safe-outputs:
  update-project:
    fields:
      repo: "myorg/myrepo"
```

Delete conflicting fields in GitHub Projects UI and recreate with different names.

## Engine-Specific Issues

### Copilot CLI Not Found

Verify compilation succeeded. The compiled workflow should include CLI installation steps.

### Model Not Available

Use the default model (`engine: copilot`) or specify an available one (`engine: {id: copilot, model: gpt-4}`).

## Context Expression Issues

### Unauthorized Expression

Use only [allowed expressions](/gh-aw/reference/templating/) like `github.event.issue.number`, `github.repository`, or `needs.activation.outputs.text`. Expressions like `secrets.GITHUB_TOKEN` or `env.MY_VAR` are not allowed.

### Sanitized Context Empty

`needs.activation.outputs.text` is only populated for issue, PR, or comment events (e.g., `on: issues:`) but not for other triggers like `push:`.

## Build and Test Issues

### Documentation Build Fails

Install dependencies, check for malformed frontmatter or MDX syntax, and fix broken links:

```bash wrap
cd docs
rm -rf node_modules package-lock.json
npm install
npm run build
```

### Tests Failing After Changes

Format code and check for issues before running tests:

```bash wrap
make fmt
make lint
make test-unit
```

## Network and Connectivity Issues

### Firewall Denials for Package Registries

Add ecosystem identifiers to allow package installations:

```yaml wrap
network:
  allowed:
    - defaults    # Basic infrastructure
    - python      # PyPI, pip
    - node        # npm, yarn, pnpm
    - containers  # Docker Hub, GHCR
    - go          # Go modules
```

See [Network Configuration Guide](/gh-aw/guides/network-configuration/) for details.

### URLs Appearing as "(redacted)"

URLs are filtered when domains aren't in the allowed list. Add domains to your `network:` configuration:

```yaml wrap
network:
  allowed:
    - defaults
    - "api.example.com"
```

See [Network Permissions](/gh-aw/reference/network/) for details.

### Cannot Download Remote Imports

Verify network access with `curl -I https://raw.githubusercontent.com/github/gh-aw/main/README.md` and GitHub authentication with `gh auth status`.

### MCP Server Connection Timeout

Use local MCP servers if HTTP connections timeout (`command: "node"`, `args: ["./server.js"]`).

## Cache Issues

### Cache Not Restoring

Verify cache key patterns match. Caches expire after 7 days.

```yaml wrap
cache:
  key: deps-${{ hashFiles('package-lock.json') }}
  restore-keys: deps-
```

### Cache Memory Not Persisting

Configure cache for the memory MCP server:

```yaml wrap
tools:
  cache-memory:
    key: memory-${{ github.workflow }}-${{ github.run_id }}
```

## Workflow Failures and Debugging

### Why Did My Workflow Fail?

Common causes: missing tokens (`COPILOT_GITHUB_TOKEN`), permission mismatches (`permissions:`), network restrictions (`network.allowed`), disabled tools (`tools:`), or AI API rate limits. Use `gh aw audit <run-id>` to investigate.

### How Do I Debug a Failing Workflow?

Check workflow logs (`gh aw logs`), audit the run (`gh aw audit <run-id>`), inspect `.lock.yml`, use Copilot Chat (`/agent agentic-workflows debug`), or watch compilation (`gh aw compile --watch`).

### Debugging Strategies

Enable verbose compilation (`--verbose`), set `ACTIONS_STEP_DEBUG = true`, inspect lock files, check MCP config (`gh aw mcp inspect`), and review logs.

## Operational Runbooks

See [Workflow Health Monitoring Runbook](https://github.com/github/gh-aw/blob/main/.github/aw/runbooks/workflow-health.md) for diagnosing missing-tool errors, authentication failures, and configuration issues.

## Getting Help

Review [reference docs](/gh-aw/reference/workflow-structure/), search [existing issues](https://github.com/github/gh-aw/issues), enable verbose flags, or create an issue. See [Error Reference](/gh-aw/troubleshooting/errors/) and [Frontmatter Reference](/gh-aw/reference/frontmatter/).
