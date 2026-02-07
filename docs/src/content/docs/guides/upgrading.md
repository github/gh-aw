---
title: Upgrading Agentic Workflows
description: Step-by-step guide to upgrade your repository to the latest version of agentic workflows, including updating extensions, applying codemods, compiling workflows, and validating changes.
sidebar:
  order: 100
---

This guide walks you through upgrading your agentic workflows to the latest version, ensuring you have access to the newest features, improvements, and security fixes.

> [!TIP]
> Agentic Upgrade
>
> You can start an agent session in your repository on GitHub.com and use the command `/agent agentic-workflows Upgrade` to automatically upgrade your workflows.

## Overview

The upgrade process updates three key areas:

1. **Agent and prompt files** - GitHub Copilot instructions, dispatcher agent, and workflow creation prompts
2. **Workflow syntax** - Automatically migrates deprecated fields and applies the latest configuration patterns
3. **Workflow compilation** - Automatically compiles all workflows to generate up-to-date `.lock.yml` files

> [!TIP]
> Quick Upgrade
>
> For most users, upgrading is a single command:
> ```bash wrap
> gh aw upgrade
> ```
> This updates agent files, applies codemods, and compiles all workflows.

## Prerequisites

Before upgrading, ensure you have GitHub CLI (`gh`) v2.0.0+, the latest gh-aw extension, and a clean working directory in your Git repository. Verify with `gh --version`, `gh extension list | grep gh-aw`, and `git status`.

## Step 1: Upgrade the Extension

Upgrade the `gh-aw` extension to get the latest features and codemods:

```bash wrap
gh extension upgrade gh-aw
```

> [!TIP]
> Working in GitHub Codespaces?
>
> If the extension upgrade fails due to restricted permissions that prevent global npm installs, use the standalone installer instead:
>
> ```bash wrap
> curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
> ```
>

Check your version with `gh aw version` and compare against the [latest release](https://github.com/github/gh-aw/releases). If you encounter issues, try a clean reinstall with `gh extension remove gh-aw` followed by `gh extension install github/gh-aw`.

## Step 2: Backup Your Workflows

Create a backup branch (`git checkout -b backup-before-upgrade`) or ensure your changes are committed and pushed. Since workflows are tracked in Git, you can always revert changes with `git checkout HEAD~1 -- .github/workflows/my-workflow.md`.

## Step 3: Run the Upgrade Command

Run the upgrade command from your repository root:

```bash wrap
gh aw upgrade
```

This command performs three main operations:

### 3.1 Updates Agent and Prompt Files

The upgrade updates these files to the latest templates (similar to running `gh aw init`):

- `.github/aw/github-agentic-workflows.md` - GitHub Copilot custom instructions
- `.github/agents/agentic-workflows.agent.md` - Dispatcher agent for routing tasks
- `.github/aw/create-agentic-workflow.md` - Prompt for creating new workflows
- `.github/aw/update-agentic-workflow.md` - Prompt for updating existing workflows
- `.github/aw/create-shared-agentic-workflow.md` - Prompt for shared workflows
- `.github/aw/debug-agentic-workflow.md` - Prompt for debugging workflows
- `.github/aw/upgrade-agentic-workflows.md` - Prompt for upgrade guidance

### 3.2 Applies Codemods to All Workflows

The upgrade automatically applies codemods to fix deprecated fields in all workflow files (`.github/workflows/*.md`):

| Codemod | What It Fixes | Example |
|---------|---------------|---------|
| **timeout-minutes-migration** | Replaces `timeout_minutes` with `timeout-minutes` | `timeout_minutes: 30` → `timeout-minutes: 30` |
| **network-firewall-migration** | Removes deprecated `network.firewall` field | Deletes `firewall: mandatory` |
| **sandbox-agent-false-removal** | Removes `sandbox.agent: false` (firewall now mandatory) | Deletes `agent: false` |
| **safe-inputs-mode-removal** | Removes deprecated `safe-inputs.mode` field | Deletes `mode: auto` |
| **schedule-at-to-around-migration** | Converts `daily at TIME` to `daily around TIME` | `daily at 10:00` → `daily around 10:00` |
| **delete-schema-file** | Deletes deprecated schema file | Removes `.github/aw/schemas/agentic-workflow.json` |
| **delete-old-agents** | Deletes old `.agent.md` files moved to `.github/aw/` | Removes outdated agent files |

### 3.3 Compiles All Workflows

The upgrade automatically compiles all workflows to generate or update `.lock.yml` files, ensuring they're ready to run in GitHub Actions.

**Example output:**

```text
Updating agent and prompt files...
✓ Updated agent and prompt files
Applying codemods to all workflows...
Processing workflow: daily-team-status
  ✓ Applied schedule-at-to-around-migration
  ✓ Applied timeout-minutes-migration
Processing workflow: issue-triage
  ✓ Applied safe-inputs-mode-removal
All workflows processed.
Compiling all workflows...
✓ Compiled 3 workflow(s)

✓ Upgrade complete
```

### Command Options

```bash wrap
# Standard upgrade (updates agent files + applies codemods + compiles workflows)
gh aw upgrade

# Verbose output (shows detailed progress)
gh aw upgrade -v

# Update agent files only (skip codemods and compilation)
gh aw upgrade --no-fix

# Upgrade workflows in custom directory
gh aw upgrade --dir custom/workflows
```

> [!WARNING]
> Custom Workflow Directory
>
> If you're using a custom workflow directory (not `.github/workflows`), always specify it with `--dir`:
> ```bash wrap
> gh aw upgrade --dir path/to/workflows
> ```

## Step 4: Review the Changes

After upgrading, carefully review all changes before committing:

Review changes with `git diff .github/workflows/` to verify that deprecated fields are updated, formatting is preserved, and workflow logic remains intact.

### Common Changes

Typical migrations include `timeout_minutes` → `timeout-minutes`, `daily at` → `daily around`, and removal of deprecated `network.firewall` and `safe-inputs.mode` fields. Use `git diff --word-diff` for detailed comparison.

## Step 5: Verify Compilation

The upgrade automatically compiles workflows. To validate specific workflows, run `gh aw compile my-workflow --validate`. Common issues include invalid YAML syntax, deprecated fields (fix with `gh aw fix --write`), or incorrect schedule format. See the [schedule syntax reference](/gh-aw/reference/schedule-syntax/) for details.

## Step 6: Review Lock Files

Verify that each `.md` workflow has a corresponding `.lock.yml` file with `git status | grep .lock.yml`. Never edit `.lock.yml` files directly-they're auto-generated. Always edit the `.md` source and recompile.

## Step 7: Test Your Workflows

Test workflows locally with `gh aw status` and `gh aw compile my-workflow --validate`. Trigger manual runs with `gh aw run my-workflow` and monitor with `gh aw logs my-workflow`. If using MCP servers, verify configuration with `gh aw mcp list`. Consider testing in a draft PR before merging to production.

## Step 8: Commit and Push

Stage and commit your changes:

```bash wrap
git add .github/workflows/ .github/aw/ .github/agents/
git commit -m "Upgrade agentic workflows to latest version"
git push origin main
```

For better traceability, consider separate commits for agent files, workflow migrations, and lock files. Always commit both `.md` and `.lock.yml` files together - never add `.lock.yml` to `.gitignore`.

## Troubleshooting

**Extension upgrade fails:** Try a clean reinstall with `gh extension remove gh-aw && gh extension install github/gh-aw`.

**Codemods not applied:** Manually apply with `gh aw fix --write -v`.

**Compilation errors:** Review errors with `gh aw compile my-workflow --validate` and fix YAML syntax in source files.

**Workflows not running:** Verify `.lock.yml` files are committed, check status with `gh aw status`, and confirm secrets are valid with `gh aw secrets bootstrap`.

**Breaking changes:** Revert with `git checkout backup-before-upgrade` and review [release notes](https://github.com/github/gh-aw/releases).

## Advanced Topics

**Upgrading across versions:** Review the [changelog](https://github.com/github/gh-aw/blob/main/CHANGELOG.md) for cumulative changes.

**Custom directories:** Use `gh aw upgrade --dir custom/workflows`.

**Selective codemods:** Apply specific workflows with `gh aw fix my-workflow --write` or skip codemods with `gh aw upgrade --no-fix`.

**CI/CD automation:** Automate upgrades with a scheduled workflow that creates PRs. Always review automated upgrade PRs before merging.

## Best Practices

Upgrade regularly to stay current with features and security fixes. Always review changes, test workflows, and read release notes. Keep backups using Git branches for easy rollback.

## What's Next?

Learn about new features in the [changelog](https://github.com/github/gh-aw/blob/main/CHANGELOG.md), explore the [frontmatter reference](/gh-aw/reference/frontmatter-full/), review [best practices](/gh-aw/guides/deterministic-agentic-patterns/), or browse the [agentics collection](https://github.com/githubnext/agentics).

Need help? Check the [troubleshooting guide](/gh-aw/troubleshooting/common-issues/) or [open an issue](https://github.com/github/gh-aw/issues/new).
