---
title: Feature Flags
description: "Enable experimental or optional compiler and runtime behaviors in GitHub Agentic Workflows using the features: frontmatter field"
sidebar:
  order: 670
---

The `features:` frontmatter field enables experimental or optional compiler and runtime behaviors as key-value pairs.

```yaml wrap
features:
  my-experimental-feature: true
  action-mode: "script"
```

## Action Mode (`features.action-mode`)

Controls how the workflow compiler generates custom action references in compiled workflows. Can be set to `"dev"`, `"release"`, `"action"`, or `"script"`.

```yaml wrap
features:
  action-mode: "script"
```

**Available modes:**

- **`dev`** (default): References custom actions using local paths (e.g., `uses: ./actions/setup`). Best for development and testing workflows in the gh-aw repository.

- **`release`**: References custom actions using SHA-pinned remote paths within `github/gh-aw` (e.g., `uses: github/gh-aw/actions/setup@sha`). Used for production workflows with version pinning.

- **`action`**: References custom actions from the `github/gh-aw-actions` external repository at the same release version (e.g., `uses: github/gh-aw-actions/setup@sha`). Uses SHA pinning when available, with a version-tag fallback. Use this when deploying workflows from the `github/gh-aw-actions` distribution repository.

- **`script`**: Generates direct shell script calls instead of using GitHub Actions `uses:` syntax. The compiler:
  1. Checks out the `github/gh-aw` repository's `actions` folder to `/tmp/gh-aw/actions-source`
  2. Runs the setup script directly: `bash /tmp/gh-aw/actions-source/actions/setup/setup.sh`
  3. Uses shallow clone (`depth: 1`) for efficiency

**When to use script mode:**

- Testing custom action scripts during development
- Debugging action installation issues
- Environments where local action references are not available
- Advanced debugging scenarios requiring direct script execution

**Example:**

```yaml wrap
---
name: Debug Workflow
on: workflow_dispatch
features:
  action-mode: "script"
permissions:
  contents: read
---

Debug workflow using script mode for custom actions.
```

**Note:** The `action-mode` can also be overridden via the CLI flag `--action-mode` or the environment variable `GH_AW_ACTION_MODE`. The precedence is: CLI flag > feature flag > environment variable > auto-detection.

## Copilot BYOK Mode (Default for `engine: copilot`)

Copilot offline Bring Your Own Key (BYOK) behavior is now the default for `engine: copilot`, bundling four behaviors:

1. Injecting a dummy `COPILOT_API_KEY` to trigger the AWF BYOK runtime path.
2. Implicitly enabling `cli-proxy`.
3. Forcing the Copilot CLI to install at `latest` (ignoring any pinned `engine.version`).
4. Setting `COPILOT_MODEL` to `${{ vars.GH_AW_MODEL_AGENT_COPILOT || 'default' }}` — Copilot BYOK providers require a non-empty model, so the compiler provides the `default` sentinel as the fallback when `GH_AW_MODEL_AGENT_COPILOT` is not set.

No feature flag is required.

To use a different model, set the `GH_AW_MODEL_AGENT_COPILOT` repository variable. The compiled workflow uses `${{ vars.GH_AW_MODEL_AGENT_COPILOT || 'default' }}` for `COPILOT_MODEL`.

> [!IMPORTANT]
> `features.byok-copilot` is deprecated and no longer needed. Existing workflows may still include it, but it has no effect.
>
> For Copilot BYOK setup and policy details, see [Using your LLM provider API keys with Copilot](https://docs.github.com/en/copilot/how-tos/administer-copilot/manage-for-enterprise/use-your-own-api-keys).

> [!NOTE]
> Copilot BYOK defaults apply only to `engine: copilot` workflows. Other engines are unchanged.

## AWF Failure Diagnostics (`features.awf-diagnostic-logs`)

Enables AWF Docker operational diagnostics collection on failure by adding `--diagnostic-logs` to AWF runtime arguments.

When enabled, AWF includes failure diagnostics under the `diagnostics/` subdirectory in the `firewall-audit-logs` artifact (for example, container logs, exit codes, mount metadata, and sanitized compose configuration).

```yaml wrap
features:
  awf-diagnostic-logs: true
```

## External Threat Detection (`features.gh-aw-detection`)

Threat detection uses the stable external `threat-detect` binary by default.
Enabling `gh-aw-detection` does not emit an experimental-feature warning. Set
`features.gh-aw-detection: false` to use the legacy inline engine path.

```yaml wrap
features:
  gh-aw-detection: false
```

## Reaction-based Trust Signals (`features.integrity-reactions`)

Enables maintainers to promote or demote content past the integrity filter using GitHub reactions (👍, ❤️, 👎, 😕), without adding labels or modifying issue state. Available from gh-aw v0.68.2.

```yaml wrap
features:
  integrity-reactions: true
```

When set, the compiler automatically enables the CLI proxy (required to identify reaction authors) and injects default endorsement and disapproval reaction configuration. Only the `features.integrity-reactions` flag is required — the reaction fields under `tools.github` (`endorsement-reactions`, `disapproval-reactions`, `endorser-min-integrity`, `disapproval-integrity`) are optional overrides.

See [Promoting and demoting items via reactions](/gh-aw/reference/integrity/#promoting-and-demoting-items-via-reactions) in the Integrity Filtering Reference for complete configuration details.

## DIFC Proxy (`tools.github.integrity-proxy`)

Controls DIFC (Data Integrity and Flow Control) proxy injection. When `tools.github.min-integrity` is configured, the compiler inserts proxy steps around the agent that enforce integrity-level isolation at the network boundary. The proxy is **enabled by default** — set `integrity-proxy: false` to opt out.

```yaml wrap
tools:
  github:
    min-integrity: approved
    # integrity-proxy: false  # uncomment to disable proxy injection
```

Without `min-integrity`, `integrity-proxy` has no effect. When both are configured, the proxy enforces network-boundary integrity filtering in addition to the MCP gateway-level filtering. Set `integrity-proxy: false` when you only need gateway-level filtering.

## Learn More

- [Frontmatter Reference](/gh-aw/reference/frontmatter/) — Complete frontmatter field reference
- [AI Engines](/gh-aw/reference/engines/) — Engine configuration including Copilot BYOK
- [Integrity Filtering](/gh-aw/reference/integrity/) — Integrity levels, reactions, and DIFC proxy
- [Network Permissions](/gh-aw/reference/network/) — Network access configuration
