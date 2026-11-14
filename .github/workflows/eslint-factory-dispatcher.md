---
name: ESLint Factory Dispatcher
description: Admits daily ESLint factory tasks and dispatches queued work to the corresponding worker
on:
  schedule: daily
  workflow_dispatch:
imports:
  - shared/work-queue-staged-plan.md
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
tools:
  work-queue: true
  cli-proxy: true
  bash:
    - cat /tmp/gh-aw/agent/eslint-factory-plan.json
safe-outputs:
  github-token: ${{ secrets.GITHUB_TOKEN }}
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
        const { stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const plan = buildESLintFactoryPlan({
          date: new Date(run.created_at).toISOString().slice(0, 10),
          repository: repository.full_name,
          repositoryId: String(repository.id),
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/eslint-factory-plan.json", JSON.stringify(plan));
        stagePlanIntents(path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl"), plan);
        core.info(`ESLint factory cohort ${plan.date}: ${plan.nodes.length} tasks`);
timeout-minutes: 15
strict: true
---

# ESLint Factory Dispatcher

## Markdown Report Formatting

For every human-readable Markdown report, comment, or discussion:
- Use `###` or deeper headings; never use `#` or `##`.
- Follow this order: overview, key metrics/issues, detailed findings, next actions. Keep the overview and key metrics/issues visible.
- Put long findings, lists, and evidence in `<details><summary>**View detailed findings**</summary> ... </details>` blocks.
- Finish with concrete next actions, using checkboxes for actionable follow-up.
- Format workflow-run references as `[§12345](https://github.com/owner/repo/actions/runs/12345)` when applicable.

Keep required machine-readable output formats, including JSON and safe-output payloads, unchanged.

Read `/tmp/gh-aw/agent/eslint-factory-plan.json`. The trusted preparation step
defines one independent task for each approved worker: miner, refiner, and
monster. Use the exact stored UTC date, nodes, profiles, contracts, and budgets.
Do not invent additional tasks or broaden resource scopes.

Read the activation snapshot first with `work_queue_read`, using
`{"pool":"default","limit":32}`. Check `queue_state`, not just `total`:
`"uninitialized"` with a null snapshot SHA means the queue branch does not
exist yet. Treat it as an empty backlog; the first trusted producer submission
will atomically bootstrap the branch with its compiler-approved Policy and Work.
Surface read/tool errors explicitly; never report an existing policyless ledger
as an empty backlog.

When `<mcp-clis>` advertises the wrappers, invoke their subcommands with one JSON
argument through the shell tool:

```bash
work-queue work_queue_read '{"pool":"default","limit":32}'
```

Use the advertised MCP tools directly when wrappers are not available.

The trusted preparation step has already staged the prepared cohort, whether
the queue is initialized or absent, with `{"nodes": <the plan.nodes array>}`
before its dispatch request. Do not call `work_queue_submit` or
`work_queue_dispatch_next` again; read and explain the snapshot and staged plan
only. Date-keyed
graph/node identities make repeated submissions on the same UTC day idempotent,
including manual runs and reruns. Do not change stored task payloads on retries
or replace admitted Work. AW authorizes trusted participants; the installed
Policy controls scheduling for pool `default`, priority `3`, and accounting key
`""`. A failed admission is not permission to bypass scheduling bounds.
Submission is a staged intent, not proof of durable admission.

The following request is already staged from `plan.dispatch`:

```json
{"pool":"default","max_claims":3,"max_dispatches":3}
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
revision, or Claim/run ownership. Do not call ordinary `dispatch_workflow` or typed
per-worker dispatch tools.

Safe-output processing uses only the compiler-approved Policy proposal; never
choose or modify Policy. If bootstrap or admission fails, report the failure; do
not bypass it with `noop` or ordinary worker dispatch.

Scheduling overrides are shared in `.github/workflows/aw.json` under
`work_queue`. Trusted `safe-outputs.dispatch-workflow` processing uses the
built-in Actions token with `actions: write`; no custom launch secret is required.
Trusted queue admission and safe outputs use the same built-in token, not
`GH_AW_GITHUB_TOKEN` or another custom secret.
AW pins declared worker routes
to the immutable workflow revision; it binds each actual native run to its
Claim. The first accepted submission automatically creates the queue with
compiled Policy and Work; no administrator seed is required.
Changing economic scheduling after bootstrap requires a quiescent queue and an
administrator Policy update. Compatible worker revisions evolve through trusted
deployment reconciliation; editing this proposal alone cannot replace installed
Policy.
