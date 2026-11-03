---
description: Maps every compiler-generated job to its purpose, dependencies, and GitHub token or GitHub App configuration so agents grant credentials only to the job that needs them.
---

# Compiler-generated jobs

Generated job IDs are reserved. Built-in job configuration may add
`setup-steps`, `pre-steps`, `needs`, `if`, and — for `agent` and `detection` only —
`timeout-minutes`; it cannot replace compiler-managed permissions or authentication.

## Job timeouts

The generated `agent` and `detection` jobs carry their own `timeout-minutes`,
resolved independently from the top-level `timeout-minutes` that bounds the
`agentic_execution` step. Generated job timeout overrides must be positive
integer literals.

| Timeout | Frontmatter override | Built-in default |
|---|---|---|
| `agent` job (all steps) | `jobs.agent.timeout-minutes` (positive integer) | 60 minutes |
| `detection` job and its execution step | `jobs.detection.timeout-minutes` (positive integer) | 10 minutes |
| `agentic_execution` step | top-level `timeout-minutes` | 20 minutes |

The step default is never used as the agent job budget. When top-level
`timeout-minutes` is an explicit literal larger than the built-in agent job
default, the agent job default is raised to that value so an explicit step
budget is not truncated by the implicit job budget.

```yaml
timeout-minutes: 25   # agentic_execution step

jobs:
  agent:
    timeout-minutes: 90
  detection:
    timeout-minutes: 30
```

Other generated jobs are not bounded by these values; `safe-outputs.timeout-minutes`
covers the safe-outputs job.

## Credential configuration

Configure credentials on the consuming feature:

| Jobs | Token | GitHub App |
|---|---|---|
| `pre_activation`, `activation` | `on.github-token` | `on.github-app` |
| `agent` | `tools.github.github-token: ${{ secrets.MY_TOKEN }}` | `tools.github.github-app` |
| `safe_outputs`, `upload_assets`, `upload_code_scanning_sarif`, `call-*` | `safe-outputs.github-token` or handler `github-token` | `safe-outputs.github-app` or handler `github-app` |
| Maintenance | Generated workflow `GITHUB_TOKEN` permissions or maintenance configuration | — |

Use secret expressions for custom tokens and private keys. Grant least privilege.
Keep `agent` and `detection` read-only; use safe outputs for writes.
`permissions: { copilot-requests: write }` permits Copilot inference through
`${{ github.token }}` without a PAT or `COPILOT_GITHUB_TOKEN`.

## Workflow job graph

| Job | Created when | Needs | Notes |
|---|---|---|---|
| `pre_activation` | Trigger validation, role/skip checks, or `on.github-app` | — | Trigger-time reactions, comments, and queries. |
| `activation` | Most workflows | `pre_activation` if present | Activation-only credential use. |
| `agent` | Every workflow | `activation` if present | GitHub tool access; read-only. |
| `detection` | Safe-output threat detection | `agent`, `activation` | Read-only; no credential configuration. |
| `safe_outputs` | Built-in output, script, action, or job | `agent`, `activation`, `detection` if present, `unlock` if `lock-for-agent`, plus any explicit `safe-outputs.needs` jobs | Approved writes. |
| `upload_assets` | Release-asset output | `safe_outputs`, `activation` | Inherits safe-output authentication; release permissions only. |
| `upload_code_scanning_sarif` | Code-scanning output | `safe_outputs` | Inherits safe-output authentication; `security-events: write`. |
| `unlock` | `lock-for-agent` | `activation`, `agent`, `detection` if present | Workflow-token lock cleanup; do not grant agent writes. |
| `evals` | `evals` | `agent`, `activation`, `detection` if present | Decisions API evaluation and redacted state persistence; framework-managed `contents: write`; runs alongside safe outputs. Dry-run omits persistence and uses `contents: read`. |
| `conclusion` | Compiler-generated conclusion | All generated and custom jobs | No custom credential; aggregates results and usage. |
| `push_repo_memory` | Repository-backed memory | After threat detection | Framework-managed repository credential. |
| `update_cache_memory` | Cache memory | After threat detection | Framework-managed repository credential. |
| `push_experiments_state` | Repository-backed experiments | After threat detection | Framework-managed repository credential. |
| `call-<sanitized-worker-name>` | `safe-outputs.call-workflow` worker | — | Configure the called workflow input through the worker's `github-token` or `github-app`. |

## Maintenance workflow jobs

`agentics-maintenance.yml` uses its `GITHUB_TOKEN` with explicit job permissions.
Do not configure these jobs in agentic-workflow frontmatter.

| Jobs | Purpose |
|---|---|
| `close-expired-discussions`, `close-expired-issues`, `close-expired-pull-requests` | Close expired items. |
| `cleanup-cache-memory` | Remove stale cache-memory entries. |
| `run_operation`, `update_pull_request_branches`, `apply_safe_outputs` | Run selected maintenance operations. |
| `create_labels`, `label_disable_agentic_workflow`, `label_apply_safe_outputs` | Manage labels. |
| `activity_report`, `forecast_report` | Produce reports. |
| `close_agentic_workflows_issues`, `validate_workflows`, `compile-workflows`, `secret-validation` | Maintain workflow configuration. |

## Compatibility aliases

`pre-activation` and `safe-outputs` are reserved compatibility aliases for
`pre_activation` and `safe_outputs`. The compiler emits the underscore forms;
use those forms in `needs` and built-in job configuration.
