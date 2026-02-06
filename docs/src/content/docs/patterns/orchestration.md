---
title: Orchestration
description: Coordinate multiple agentic workflows using workflow dispatch (orchestrator/worker pattern).
---

Use this pattern when one workflow (the **orchestrator**) needs to fan out work to one or more **worker** workflows.

## The orchestrator/worker pattern

- **Orchestrator**: decides what to do next, splits work into units, dispatches workers.
- **Worker(s)**: do the concrete work (triage, code changes, analysis) with scoped permissions/tools.
- **Optional monitoring**: both orchestrator and workers can update a GitHub Project board for visibility.

## Dispatch workers with `dispatch-workflow`

Allow dispatching specific workflows:

```yaml
safe-outputs:
  dispatch-workflow:
    workflows: [repo-triage-worker, dependency-audit-worker]
    max: 10
```

During compilation, gh-aw validates the target workflows exist and support `workflow_dispatch`, then generates MCP tools the agent can call.

Reference: [/reference/safe-outputs/#workflow-dispatch-dispatch-workflow](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow)

## Passing correlation IDs

If your workers need shared context, pass an explicit input such as `tracker_id` (string) and include it in worker outputs (e.g., writing it into a Project custom field).

## Best practices

- Keep worker workflows **single-purpose** and easy to retry.
- Make orchestration **idempotent** (safe to re-run).
- Set conservative `max` limits on dispatch to prevent accidental fan-out.
- Prefer Projects + status updates for **monitoring** over ad-hoc labels.

See also: [/patterns/monitoring/](/gh-aw/patterns/monitoring/)
