---
name: ESLint Factory Dispatcher
description: Admits daily ESLint factory tasks and dispatches queued work to the corresponding worker
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine: copilot
tools:
  work-queue: true
  cli-proxy: true
  bash:
    - cat /tmp/gh-aw/agent/eslint-factory-plan.json
safe-outputs:
  dispatch-workflow:
    workflows: [eslint-miner, eslint-refiner, eslint-monster]
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
  noop:
steps:
  - name: Prepare the UTC ESLint factory cohort
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { buildESLintFactoryPlan } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/eslint_factory_portfolio.cjs"));
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const plan = buildESLintFactoryPlan({
          date: new Date(run.created_at).toISOString().slice(0, 10),
          repository: repository.full_name,
          repositoryId: String(repository.id),
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/eslint-factory-plan.json", JSON.stringify(plan));
        core.info(`ESLint factory cohort ${plan.date}: ${plan.nodes.length} tasks`);
timeout-minutes: 15
strict: true
---

# ESLint Factory Dispatcher

Read `/tmp/gh-aw/agent/eslint-factory-plan.json`. The trusted preparation step
defines one independent task for each approved worker: miner, refiner, and
monster. Use the exact stored UTC date, nodes, profiles, contracts, and budgets.
Do not invent additional tasks or broaden resource scopes.

Read the activation snapshot first with `work_queue_read`, using
`{"pool":"default","limit":32}`. Check `queue_state`, not just `total`:
`"uninitialized"` means the queue Policy is absent, not that an initialized queue
has no work. Report this deployment failure with `missing_data`, then stop without
dispatching or calling `noop`. Surface read/tool errors explicitly; never turn an
error into an empty-backlog report.

When `<mcp-clis>` advertises the wrappers, invoke their subcommands with one JSON
argument through the shell tool:

```bash
work-queue work_queue_read '{"pool":"default","limit":32}'
safeoutputs missing_data '{"data_type":"work-queue policy","reason":"Queue is uninitialized; an authenticated administrator must install Policy and producer entitlements before dispatch."}'
```

Run the second command only for an uninitialized queue. Do not call `safeoutputs`
as a structured tool with `command` and `description`; it is a CLI wrapper.
Use the advertised MCP tools directly when wrappers are not available.

For an initialized queue, submit the prepared cohort:
Call `work_queue_submit` once with
`{"nodes": <the plan.nodes array>}` before requesting any grants. Date-keyed
graph/node identities make repeated submissions on the same UTC day idempotent,
including manual runs and reruns. Do not change stored task payloads on retries
or replace admitted Work. Trusted processing enforces the authenticated
dispatcher's installed producer entitlement for pool `default`, priority `3`,
and accounting key `""`; a failed admission is not permission to bypass Policy.
With CLI wrappers, use the `work-queue work_queue_submit` subcommand with one
JSON argument containing the exact stored nodes. Submission is also a staged
intent, not proof of durable admission.

Then request the trusted scheduler's fair prefix once, using `plan.dispatch`:

```bash
work-queue work_queue_dispatch_next '{"pool":"default","max_claims":3,"max_dispatches":3}'
```

Keep the request within the installed pool policy and this workflow's dispatch
budget. Even an empty snapshot or a prediction of no eligible Work can be stale;
trusted processing refreshes the ledger. A response with `status: "staged"` and
an `intent_id` is not proof of Claims, native launches, or completed Work. Report
only that the request was staged, not that workers ran or that the live queue is
empty.

Do not select Work IDs, workers, revisions, or targets from a queue snapshot.
The scheduler selects eligible Work and launches only the compatible worker
profile at its installed immutable workflow revision. This workflow's
`safe-outputs.dispatch-workflow.workflows` list is the compiler-approved
worker-name allowlist; it does not replace the installed policy's profile,
revision, or principal binding. Do not call ordinary `dispatch_workflow` or typed
per-worker dispatch tools.

The authenticated operator provisions the queue Policy and grants this
dispatcher's authenticated principal producer entitlement before this workflow
runs. Do not bootstrap Policy. If producer entitlement is missing, report the
admission failure explicitly; do not call `noop` or ordinary worker dispatch.
