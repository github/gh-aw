---
title: APM Dependencies
description: Install and manage APM (Agent Package Manager) packages in your agentic workflows, including skills, prompts, instructions, agents, hooks, and plugins.
sidebar:
  order: 330
---

[APM (Agent Package Manager)](https://microsoft.github.io/apm/) manages AI agent primitives such as skills, prompts, instructions, agents, hooks, and plugins (including the Claude `plugin.json` specification). Packages can depend on other packages and APM resolves the full dependency tree.

APM is configured by importing the `shared/apm.md` workflow, which creates a dedicated `apm` job that packs packages and uploads the bundle as a GitHub Actions artifact. The agent job then downloads and unpacks the bundle for deterministic startup.

## Where `shared/apm.md` comes from

`shared/apm.md` is a **local workflow file** that gh-aw resolves at `.github/workflows/shared/apm.md` in your repository — it is not a remote import (the `uses:` syntax inside `imports:` is gh-aw's local-import shape, not GitHub Actions' `uses: owner/repo@ref`).

The canonical source is maintained in [microsoft/apm](https://github.com/microsoft/apm/blob/main/.github/workflows/shared/apm.md). Add it to your repository with:

```bash
gh aw add microsoft/apm/.github/workflows/shared/apm.md --dir shared
```

Running `gh aw update` will keep your vendored copy in sync with the canonical source.

The canonical version pins `microsoft/apm-action@v1.5.0` and supports multi-org GitHub App authentication (`apps:[]`) and multi-bundle restore.

## Usage

Import `shared/apm.md` and supply the list of packages via the `packages` parameter:

```aw wrap
imports:
  - uses: shared/apm.md
    with:
      packages:
        - microsoft/apm-sample-package
        - github/awesome-copilot/skills/review-and-refactor
        - anthropics/skills/skills/frontend-design
```

## Reproducibility and governance

APM lock files (`apm.lock`) pin every package to an exact commit SHA, so the same versions are installed on every run. Lock file diffs appear in pull requests and are reviewable before merge, giving teams and enterprises a clear audit trail and the ability to govern which agent context is in use. See the [APM governance guide](https://microsoft.github.io/apm/enterprise/governance/) for details on policy enforcement and access controls.

## Package reference formats

Each entry in `packages` is an APM package reference. Supported formats:

| Format | Description |
|--------|-------------|
| `owner/repo` | Full APM package |
| `owner/repo/path/to/primitive` | Individual primitive (skill, instruction, plugin, etc.) from a repository |
| `owner/repo#ref` | Package pinned to a tag, branch, or commit SHA |

### Examples

```aw wrap
imports:
  - uses: shared/apm.md
    with:
      packages:
        # Full APM package
        - microsoft/apm-sample-package
        # Individual primitive from any repository
        - github/awesome-copilot/skills/review-and-refactor
        # Plugin (Claude plugin.json format)
        - github/awesome-copilot/plugins/context-engineering
        # Version-pinned to a tag
        - microsoft/apm-sample-package#v2.0
        # Version-pinned to a branch
        - microsoft/apm-sample-package#main
```

## How it works

The `shared/apm.md` import adds a dedicated `apm` job to the compiled workflow. This job runs `microsoft/apm-action` to install packages and create a bundle archive, which is uploaded as a GitHub Actions artifact. The agent job downloads and restores the bundle as pre-steps, making all skills and tools available at runtime.

Packages are fetched using the cascading token fallback: `GH_AW_PLUGINS_TOKEN` → `GH_AW_GITHUB_TOKEN` → `GITHUB_TOKEN`.

To reproduce or debug the pack/unpack flow locally, run `apm pack` and `apm unpack` directly. See the [pack and distribute guide](https://microsoft.github.io/apm/guides/pack-distribute/) for instructions.

## Pull request runs

For pull request triggers, the agent job replaces agent config folders (`.agents`, `.github`, and the engine folder such as `.claude`) with a base-branch snapshot so a fork PR cannot inject skill or instruction files.

## Reference

| Resource | URL |
|----------|-----|
| APM documentation | https://microsoft.github.io/apm/ |
| APM governance guide | https://microsoft.github.io/apm/enterprise/governance/ |
| Pack and distribute guide | https://microsoft.github.io/apm/guides/pack-distribute/ |
| gh-aw integration (APM docs) | https://microsoft.github.io/apm/integrations/gh-aw/ |
| apm-action (GitHub) | https://github.com/microsoft/apm-action |
| microsoft/apm (GitHub) | https://github.com/microsoft/apm |
| shared/apm.md (canonical) | https://github.com/microsoft/apm/blob/main/.github/workflows/shared/apm.md |
