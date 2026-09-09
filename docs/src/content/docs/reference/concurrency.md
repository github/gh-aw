---
title: Concurrency Control
description: Complete guide to concurrency control in GitHub Agentic Workflows, including agent job concurrency configuration and engine isolation.
sidebar:
  order: 1400
---

GitHub Agentic Workflows uses dual-level concurrency control to prevent resource exhaustion and ensure predictable execution:
- **Per-workflow**: Limits based on workflow name and trigger context (issue, PR, branch)
- **Per-engine**: Limits AI execution across all workflows via `engine.concurrency`

## Per-Workflow Concurrency

Workflow-level concurrency groups include the workflow name plus context-specific identifiers:

| Trigger Type | Concurrency Group | Cancel In Progress |
|--------------|-------------------|-------------------|
| Issues | `gh-aw-${{ github.workflow }}-${{ issue.number }}` | No |
| Pull Requests | `gh-aw-${{ github.workflow }}-${{ pr.number \|\| ref }}` | Yes (new commits cancel outdated runs) |
| Push | `gh-aw-${{ github.workflow }}-${{ github.ref }}` | No |
| Schedule/Other | `gh-aw-${{ github.workflow }}` | No |
| Label-triggered (label trigger shorthand or label_command) | `gh-aw-${{ github.workflow }}-${{ entity.number }}-${{ github.event.label.name }}` | Yes for PRs, No otherwise |

This ensures workflows on different issues, PRs, or branches run concurrently without interference.

For triggers where `cancel-in-progress` is not enabled, the generated top-level group also emits `queue: max` by default, so back-to-back triggers (for example rapid pushes) are queued and run sequentially instead of displacing pending runs. Set `features.group-concurrency-queue: false` to opt out (see [Queue Behavior](#queue-behavior-queue)).

## Per-Engine Concurrency

The default per-engine pattern `gh-aw-{engine-id}` ensures only one agent job runs per engine across all workflows, preventing AI resource exhaustion. The group includes only the engine ID and `gh-aw-` prefix — workflow name, issue/PR numbers, and branches are excluded.

```yaml wrap
jobs:
  agent:
    concurrency:
      group: "gh-aw-{engine-id}"
```

## Custom Concurrency

Override either level independently:

```yaml wrap
---
on: push
concurrency:  # Workflow-level
  group: custom-group-${{ github.ref }}
  cancel-in-progress: true
engine:
  id: copilot
  concurrency:  # Engine-level
    group: "gh-aw-copilot-${{ github.workflow }}"
tools:
  github:
    allowed: [list_issues]
---
```

## Safe Outputs Job Concurrency

The `safe_outputs` job runs independently from the agent job and can process outputs concurrently across workflow runs. Use `safe-outputs.concurrency-group` to serialize access when needed:

```yaml wrap
safe-outputs:
  concurrency-group: "safe-outputs-${{ github.repository }}"
  create-issue:
```

When set, the `safe_outputs` job uses `cancel-in-progress: false` — meaning queued runs wait for the in-progress run to finish rather than being cancelled. This is useful for workflows that create issues or pull requests where duplicate operations would be undesirable.

See [Safe Outputs](/gh-aw/reference/safe-outputs/#safe-outputs-job-concurrency-concurrency-group) for details.

## Queue Behavior (`queue`)

GitHub Actions concurrency groups accept an optional `queue` field that controls how multiple pending runs in the same group are handled. The gh-aw compiler preserves this field in both top-level and per-engine concurrency blocks:

| Value | Behavior |
|---|---|
| `single` (Actions default) | Only the latest pending run is kept; earlier pending runs are discarded. |
| `max` | All pending runs queue and run in arrival order. |

```yaml wrap
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  queue: max
```

Compiler-generated concurrency groups (the top-level workflow group, and the agent, output, and conclusion jobs) emit `queue: max` by default so back-to-back triggers run sequentially rather than being dropped. The top-level group only emits `queue: max` when `cancel-in-progress` is not enabled, since GitHub Actions rejects the combination of `queue: max` and `cancel-in-progress: true`. Set `features.group-concurrency-queue: false` to omit `queue` from generated groups and revert to the Actions default:

```yaml wrap
features:
  group-concurrency-queue: false
```

## Conclusion Job Concurrency

The `conclusion` job — which handles reporting and post-agent cleanup — automatically receives a workflow-specific concurrency group derived from the workflow filename:

```yaml wrap
conclusion:
  concurrency:
    group: "gh-aw-conclusion-my-workflow"
    cancel-in-progress: false
    queue: max
```

This prevents conclusion jobs from colliding when multiple agents run the same workflow concurrently. The group uses `cancel-in-progress: false` so queued conclusion runs complete in order rather than being discarded, and `queue: max` preserves arrival order for queued runs (see [Queue Behavior](#queue-behavior-queue)).

This concurrency group is set automatically during compilation and requires no manual configuration.

When `concurrency.job-discriminator` is set, the discriminator is also appended to the conclusion job's concurrency group, making each run's group distinct:

```yaml wrap
concurrency:
  job-discriminator: ${{ github.event.issue.number || github.run_id }}
```

This generates a group like `gh-aw-conclusion-my-workflow-${{ github.event.issue.number || github.run_id }}`, preventing concurrent runs for different issues or inputs from competing for the same conclusion slot.

## Fan-Out Concurrency (`job-discriminator`)

When multiple workflow instances are dispatched concurrently with different inputs (fan-out pattern), compiler-generated job-level concurrency groups are static across all runs — causing all but the latest dispatched run to be cancelled as they compete for the same slot.

Use `concurrency.job-discriminator` to append a unique expression to compiler-generated job-level concurrency groups (`agent`, `output`, and `conclusion` jobs), making each dispatched run's group distinct:

```yaml wrap
concurrency:
  job-discriminator: ${{ inputs.finding_id }}
```

This generates a unique job-level concurrency group per dispatched run, preventing fan-out cancellations while preserving the per-workflow concurrency group at the workflow level.

Common expressions:

| Scenario | Expression |
|---|---|
| Fan-out by a specific input | `${{ inputs.finding_id }}` |
| Universal uniqueness (e.g. scheduled runs) | `${{ github.run_id }}` |
| Dispatched or scheduled fallback | `${{ inputs.organization \|\| github.run_id }}` |

:::note
`job-discriminator` is a gh-aw extension and is stripped from the compiled lock file. It does not appear in the generated GitHub Actions YAML.
:::

Shared workflows may define `concurrency.job-discriminator`. When multiple shared workflows define it, the first imported value wins; a value in the main workflow always takes precedence.

:::note
`job-discriminator` has no effect on workflows triggered by `workflow_dispatch`-only, `push`, or `pull_request` events, or when the engine provides an explicit job-level concurrency configuration.
:::

## Learn More

- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete frontmatter reference
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Safe output processing and job configuration
