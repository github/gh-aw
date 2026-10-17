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

The default installation uses Copilot CLI [1.0.98](https://github.com/github/copilot-cli/releases/tag/v1.0.98). CLI 1.0.97 and 1.0.98 include fixes for explicit GitHub MCP tool selections, noninteractive hook permission decisions, resumed tool calls, and command hook completion. Set `engine.version` to override the CLI version; otherwise the installer selects a compatible cached version before falling back to the default.

Copilot SDK mode (`engine.copilot-sdk: true`) is stable and does not require an experimental opt-in. SDK session artifacts retain `model.call_finished` observations alongside durable events. Live `tool.shell_output` chunks are excluded from SDK artifacts and driver stderr until redaction can protect credentials split across chunks. Unified sessions preserve retained native payloads, including file-edit details and human-response provenance; a logical model dispatch ending is not evidence that the workflow completed.

The built-in SDK driver explicitly registers native MCP servers from a separate converter-owned configuration under `RUNNER_TEMP/gh-aw/mcp-config/copilot-sdk.json`, not the user-managed Copilot CLI catalog. Servers mounted through `tools.cli-proxy` remain CLI-only. Tool permissions and gateway enforcement still apply to registered servers.

The converter atomically replaces the catalog and publishes a SHA-256 digest through the step environment, bound to the run ID, attempt, and job. Missing, malformed, changed, or linked catalogs and stale integrity values fail before inference. Sandboxed agents receive read-only runtime assets; integrity verification does not protect against a compromised runner that can replace both the catalog and its trusted environment.

Copilot supports the broadest set of `gh-aw` engine-specific features: native custom-agent selection with `engine.agent`, custom harnesses, `max-continuations`, bare mode, and per-command bash allowlisting. Offline BYOK mode disables its native web tools. Compilation rejects `tools.web-search` and CLI-mode `tools.web-fetch`; configure an MCP server instead. SDK mode (`engine.copilot-sdk: true`) supports `tools.web-fetch` through a custom proxy-aware implementation. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

Copilot resolves [model aliases](/gh-aw/reference/engines/#model-aliases) before the CLI starts: `model: opus` and `model: copilot/opus` both run on the newest live Opus, and an unresolvable alias stops the run with `model_unavailable`. Exact ids such as `claude-opus-5.5` reach the CLI unchanged; an unavailable exact id fails at the first request and produces the unavailable-model failure issue.

### Dynamic workflows

Copilot CLI [dynamic workflows](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-dynamic-workflows) are experimental and disabled by default. Enable them explicitly with `engine.dynamic-workflows: true`. The compiler emits `Using experimental feature: copilot.dynamic-workflows` only when enabled; batch compilation includes their usage in the experimental-feature summary.

```yaml
engine:
  id: copilot
  dynamic-workflows: true
  args: ["--experimental"]
```

gh-aw enables the CLI's `EXTENSIONS` feature flag, loads project extensions in prompt mode, and pre-approves workflow runs. SDK sessions honor the same switch through their tool catalog and permission handler, without granting additional shell or MCP access. Save reusable definitions and supporting files under `.github/extensions/<name>/extension.mjs`, and explicitly request the registered workflow by name in the prompt. Their agents remain subject to the configured tool permissions and sandbox policies.

gh-aw's existing engine-config restoration snapshots the engine-declared folders, including Copilot's `.github/`, recursively from the activation checkout and restores them after agent checkouts, including when `checkout.pull-request: false` is set. Restoring the entire configuration directory prevents PR-head extension code, settings, and hooks from replacing trusted definitions. Use `ambient-folders` for supporting files elsewhere in the repository. Dynamic workflow availability also depends on the Copilot account; enabling extensions does not make workflows available with a BYOK provider.

The repository's [Copilot dynamic workflow smoke test](https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-copilot-dynamic-workflow.md) invokes the packaged `smoke-copilot-dynamic-workflow` extension by name. It verifies a relative module import, a nested hidden support file, durable steps, and one structured subagent result. Run it with `gh aw run smoke-copilot-dynamic-workflow`, or dispatch **Smoke Trigger** with `dynamic-workflow: true` to exercise a feature branch before the new workflow is registered on the default branch.

Current Copilot CLI releases require `engine.args: ["--experimental"]` to expose the dynamic workflow tools. Loading an extension through the `EXTENSIONS` flag alone does not expose those tools.

Omitting `engine.dynamic-workflows` or setting it to `false` disables the CLI's extension feature flag and project extension loading, denies workflow runs, and restores engine configuration only when PR checkout requires it:

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
