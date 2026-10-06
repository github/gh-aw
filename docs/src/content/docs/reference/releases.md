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
set -euo pipefail
INSTALLER=$(mktemp)
trap 'rm -f "$INSTALLER"' EXIT
curl -fsSL https://raw.githubusercontent.com/github/gh-aw/4c53fac4c30c2d9f27ea6fab5e5ce0f15a21e78e/install-gh-aw.sh -o "$INSTALLER"
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | sha256sum -c -
else
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | shasum -a 256 -c -
fi

# Latest release
bash "$INSTALLER"

# Specific version
bash "$INSTALLER" v0.64.5
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
