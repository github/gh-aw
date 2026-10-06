---
title: Using GitHub Copilot with GitHub Agentic Workflows
description: Select and authenticate GitHub Copilot as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[GitHub Copilot CLI](https://github.com/features/copilot/cli) is a coding agent from GitHub and the default AI engine for GitHub Agentic Workflows. GitHub Agentic Workflows runs Copilot CLI in GitHub Actions, adding GitHub event triggers and guardrails.

## Selecting GitHub Copilot as the AI engine

GitHub Copilot is the default AI engine used by GitHub Agentic Workflows. To select it explicitly, add this to the workflow frontmatter:

```yaml
engine: copilot
```

To authenticate:
- For organization-billed usage, grant [`copilot-requests: write`](/gh-aw/reference/auth/#copilot-requests-write-permission).
- Otherwise, to authenticate with a GitHub Copilot subscription, provide a [`COPILOT_GITHUB_TOKEN`](/gh-aw/reference/auth/#copilot_github_token) secret containing a fine-grained PAT with Copilot Requests access.

## Example: scheduled repository report

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write

engine: copilot

safe-outputs:
  create-issue:
    title-prefix: "[status] "
    labels: [report]
    close-older-issues: true
---

# Daily Repository Status

Analyze the repository and create a concise daily status report covering:
- Open issues and their priority
- Recent PR activity
- Upcoming work items
```

## Capabilities and limitations

Copilot supports the broadest set of `gh-aw` engine-specific features: native custom-agent selection with `engine.agent`, custom harnesses, `max-continuations`, bare mode, and per-command bash allowlisting. Copilot CLI provides native `tools.web-search` through its built-in `web_search` tool; declare `tools: web-search:` to enable it, including in workflows that do not use GitHub repository tools. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

### Dynamic workflows

Copilot CLI [dynamic workflows](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-dynamic-workflows) are enabled by default. gh-aw enables the CLI's `EXTENSIONS` feature flag, loads project extensions in prompt mode, and pre-approves workflow runs. SDK sessions honor the same switch through their tool catalog and permission handler, without granting additional shell or MCP access. Save reusable definitions and supporting files under `.github/extensions/<name>/extension.mjs`, and explicitly request the registered workflow by name in the prompt. Their agents remain subject to the configured tool permissions and sandbox policies.

gh-aw's existing engine-config restoration snapshots the engine-declared folders, including Copilot's `.github/`, recursively from the activation checkout and restores them after agent checkouts, including when `checkout.pull-request: false` is set. Restoring the entire configuration directory prevents PR-head extension code, settings, and hooks from replacing trusted definitions. Use `ambient-folders` for supporting files elsewhere in the repository. Dynamic workflow availability also depends on the Copilot account; enabling extensions does not make workflows available with a BYOK provider.

The repository's [Copilot dynamic workflow smoke test](https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-copilot-dynamic-workflow.md) invokes the packaged `smoke-copilot-dynamic-workflow` extension by name. It verifies a relative module import, a nested hidden support file, durable steps, and one structured subagent result. Run it with `gh aw run smoke-copilot-dynamic-workflow`, or dispatch **Smoke Trigger** with `dynamic-workflow: true` to exercise a feature branch before the new workflow is registered on the default branch.

Current Copilot CLI releases require `engine.args: ["--experimental"]` to expose the dynamic workflow tools. Loading an extension through the `EXTENSIONS` flag alone does not expose those tools.

Set `engine.dynamic-workflows: false` to disable the CLI's extension feature flag and project extension loading, deny workflow runs, and restore engine configuration only when PR checkout requires it:

```yaml
engine:
  id: copilot
  dynamic-workflows: false
```

## GitHub Agentic Workflows vs. Copilot CLI in GitHub Actions

Running coding agent CLIs such as `copilot` directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
