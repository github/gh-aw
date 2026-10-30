---
title: Releases and Versioning
description: How to pin the gh-aw CLI to a specific release, how compiled lock.yml files are versioned, and when to use upgrade vs update.
sidebar:
  order: 520
---

GitHub Agentic Workflows uses a two-layer versioning model: the **CLI extension** (`gh aw`) that you run locally or in CI, and the **compiled `.lock.yml` files** that run in GitHub Actions. Each layer has independent version tracking.

## Release Cadence

gh-aw follows a **weekly or bi-weekly minor release cadence**, similar to [VS Code's release practices](https://code.visualstudio.com/updates). Each release cycle:

1. A new **minor** version is published as a prerelease and floated for a few days.
2. On **Monday**, the last known-good prerelease is promoted to stable and `latest` is updated.
3. Immediately after promotion, the next minor prerelease is kicked off.

Patch releases are reserved for urgent fixes between cycles. Major releases are used only for significant breaking changes.

> [!NOTE]
> Version numbers increment on a time cadence, not on the scope of changes — a "minor" release may contain anything from small improvements to large new features.

## Release Channels

Two options are available when installing gh-aw:

| Channel | Alias | Behavior |
|---------|-------|----------|
| **Latest** (default) | `latest` | Always resolves to the most recent GitHub release |
| **Pinned** | `vMAJOR.MINOR.PATCH` | A fixed release tag — use when you need exact reproducibility |

### Installing a channel

```bash
# Latest (default — no version flag needed)
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash

# Specific version
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash -s v0.64.5
```

Via the GitHub CLI extension manager:

```bash
gh extension install github/gh-aw          # latest (default)
gh extension install github/gh-aw@v0.64.5  # pinned version
```

### Checking and updating your version

```bash
gh aw version                   # Show currently installed version

gh extension upgrade gh-aw      # Upgrade to the latest release
gh aw upgrade --pre-releases    # Also consider newer pre-releases
gh extension install github/gh-aw --force --pin v0.75.3-beta.1  # Install an exact pre-release tag
```

### Pinning in GitHub Actions

Use the [`setup-cli` action](https://github.com/github/gh-aw/blob/main/actions/setup-cli/README.md) to install a specific version in CI:

```yaml
- name: Install gh-aw
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.64.5
```

## Version Enforcement in Compiled Workflows

Every compiled `.lock.yml` embeds the gh-aw version used to produce it:

```yaml
GH_AW_INFO_AWF_VERSION: "v0.64.5"
```

At runtime, the activation job fetches `.github/aw/compat.json` and compares the embedded version against three policies:

| Policy | Effect |
|--------|--------|
| `blockedVersions` | Workflow fails — the compiled version has been revoked |
| `minimumVersion` | Workflow fails — the compiled version is too old |
| `minRecommendedVersion` | Workflow warns — an upgrade is recommended but not enforced |

This means that if you compiled a workflow months ago with an older version of `gh aw`, you may see a warning (or failure) asking you to recompile with a newer version. Run `gh aw upgrade` to bring all workflows up to date.

Setup action dependency upgrades can also introduce runtime requirements that older generated workflows do not meet. If activation reports that a workflow is too old for safe-output publishing, update `gh-aw`, run `gh aw compile`, and commit the regenerated workflow files. Updating only the pinned setup action is not sufficient; older workflows must pass the publishing job's check-run context for execution provenance. This requirement is enforced by the newer setup runtime and does not retroactively block workflows that remain pinned to an older runtime.

## Common Compatibility Fixes After Upgrading

Older workflow sources can fail compilation after a CLI upgrade. For the two configuration patterns below, preview and apply the existing automatic fixes, then regenerate lock files:

```bash
gh aw fix
gh aw fix --write
gh aw compile
```

Review and commit the changed workflow `.md` and generated `.lock.yml` files. Fixing compilation does not restore removed tool capabilities; review the workflow's tool requirements before running it.

### Copilot Web Tools in Offline BYOK Mode

Copilot runs with `COPILOT_OFFLINE=true` in offline BYOK mode. Compilation rejects `tools.web-search` in both CLI and SDK modes, and `tools.web-fetch` in CLI mode.

`gh aw fix --write` removes these unsupported declarations. If the workflow still needs web access, configure an [MCP server](/gh-aw/reference/tools/#custom-mcp-servers-mcp-servers) instead, or switch to an engine that supports the required tools, such as `engine: codex`. See [web search configuration](/gh-aw/reference/web-search/).

For fetching only, enable Copilot SDK mode to retain `tools.web-fetch`:

```aw
engine:
  id: copilot
  copilot-sdk: true
tools:
  web-fetch:
```

SDK mode provides a custom proxy-aware fetch tool that follows `network.allowed`; it does **not** enable native web search.

### Explicit Bash Configuration with `min-integrity: none`

Strict mode requires an explicit `tools.bash` setting when `tools.github.min-integrity` is `none`, so shell access is intentional when processing untrusted content.

`gh aw fix --write` inserts `tools.bash: false` when the setting is absent. Keep this value if shell access is unnecessary. If the workflow needs shell commands, declare a limited allowlist instead:

```aw
tools:
  bash: ["cat", "ls", "find", "grep", "head", "tail", "wc"]
  github:
    min-integrity: none
```

Choose only the commands the workflow needs. Setting `min-integrity: none` disables the automatic integrity protection for public repositories; use it only for workflows designed to handle untrusted input. See [integrity filtering](/gh-aw/reference/integrity/) and [Bash configuration](/gh-aw/reference/tools/#bash-tool-bash).

## How Lock Files Are Pinned

When `gh aw compile` generates a `.lock.yml`, it pins every GitHub Actions reference to a commit SHA:

```yaml
# Generated lock file (do not edit manually)
uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2
```

SHA pins are immutable — unlike tags, they cannot be silently redirected to a different commit. This protects workflows from supply-chain attacks.

The resolved SHA mappings are cached in `.github/aw/actions-lock.json`. Commit this file to version control so that all contributors and automated tools (including GitHub Copilot Coding Agent) produce identical lock files without needing broad API access.

To refresh action pins:

```bash
gh aw update-actions   # Update actions-lock.json to latest SHAs
gh aw compile          # Recompile workflows using the refreshed pins
```

> [!IMPORTANT]
> Never edit `.lock.yml` files manually and do not merge Dependabot PRs that target `github/gh-aw-actions` references in lock files. These pins are managed entirely by the compiler. See [Compilation Process](/gh-aw/reference/compilation-process/#the-gh-aw-actions-repository) for details.

## `upgrade` vs `update`

These two commands address different concerns:

### `gh aw upgrade` — update the tooling

`upgrade` brings the repository's agentic workflow infrastructure up to date with the current version of `gh aw`. It:

1. Self-updates the `gh aw` extension to the latest version
2. Regenerates the dispatcher agent file (like `gh aw init`)
3. Applies codemods to fix deprecated syntax across all workflow markdown files
4. Updates GitHub Actions versions in `actions-lock.json`
5. Recompiles all workflows to produce fresh `.lock.yml` files

Run `upgrade` after installing a new version of `gh aw`, or periodically to keep your repository current.

```bash
gh aw upgrade                          # Upgrade everything
gh aw upgrade --no-actions             # Skip updating action pins
gh aw upgrade --audit                  # Check dependency health without upgrading
gh aw upgrade --create-pull-request    # Open a PR with the changes
```

### `gh aw update` — update workflow content from source

`update` fetches the latest version of workflow markdown files from their upstream source repositories. It only applies to workflows that declare a [`source` field](/gh-aw/reference/imports/) in their frontmatter.

```yaml
# Example workflow with a source field
source: github/gh-aw/.github/workflows/shared/ci-doctor.md@v1
```

By default, `update` merges upstream changes with your local modifications (3-way merge). Use `--no-merge` to overwrite local changes entirely.

```bash
gh aw update                           # Update all workflows that have a source field
gh aw update ci-doctor                 # Update a specific workflow
gh aw update ci-doctor --no-merge      # Override local changes with upstream
gh aw update ci-doctor --major         # Allow major version updates
gh aw update --create-pull-request     # Open a PR with the changes
```

### Summary

| Command | What it updates | When to use |
|---------|----------------|-------------|
| `gh aw upgrade` | Tooling: agent files, codemods, action pins, compiled lock files | After installing a new `gh aw` version; periodic maintenance |
| `gh aw update` | Workflow content: markdown files sourced from other repositories | When upstream workflows have released new versions |

> [!TIP]
> Both commands support `--create-pull-request` to open a review PR instead of committing directly.
