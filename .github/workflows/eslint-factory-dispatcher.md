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
work-queue-policy:
  mode: weighted-priority
  class-weights: [8, 4, 2, 1, 1]
  accounting-weights: {"": 1}
  producers:
    "4175913":
      pools: [default]
      priorities: [3]
      fairness-keys: [""]
  pools:
    default:
      default-profile: eslint-miner
      profiles:
        eslint-miner:
          workflow: .github/workflows/eslint-miner.lock.yml
          ref: 6976a375a288e2ac4ca5102fd54bb9adafcf2f5e
          principal: "4175913"
          trust-domain: eslint-miner
          credential-scope: repository
          effect-scope: ${{ github.repository }}
          max-claims-per-dispatch: 1
          share-keys: false
        eslint-refiner:
          workflow: .github/workflows/eslint-refiner.lock.yml
          ref: 6976a375a288e2ac4ca5102fd54bb9adafcf2f5e
          principal: "4175913"
          trust-domain: eslint-refiner
          credential-scope: repository
          effect-scope: ${{ github.repository }}
          max-claims-per-dispatch: 1
          share-keys: false
        eslint-monster:
          workflow: .github/workflows/eslint-monster.lock.yml
          ref: 6976a375a288e2ac4ca5102fd54bb9adafcf2f5e
          principal: "4175913"
          trust-domain: eslint-monster
          credential-scope: repository
          effect-scope: ${{ github.repository }}
          max-claims-per-dispatch: 1
          share-keys: false
      logical-limit: 3
      native-limit: 3
      allowed-repositories: ["${{ github.repository }}"]
      max-observation-age-ms: 60000
      retry: {max-attempts: 1, backoff-ms: 1000}
      reconciliation: {max-attempts: 5, deadline-ms: 300000}
  limits:
    ledger-bytes: 67108864
    recovery-bytes: 16777216
    payload-bytes: 8192
    graph-nodes: 3
    predecessors: 64
    pending-nodes: 30
    operations: 32
    assignment-bytes: 49152
    result-bytes: 4096
    evidence-bytes: 1024
    observation-writes: 4096
safe-outputs:
  dispatch-workflow:
    workflows: [eslint-miner, eslint-refiner, eslint-monster]
    github-token: ${{ secrets.GH_AW_GITHUB_TOKEN }}
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
`"uninitialized"` with a null snapshot SHA means the queue branch does not
exist yet. Treat it as an empty backlog; the first trusted producer submission
will atomically bootstrap the branch with its compiler-approved Policy and Work.
Do not dispatch until that submission has been admitted. Surface read/tool
errors explicitly; never turn an error or an existing policyless ledger into an
empty-backlog report.

When `<mcp-clis>` advertises the wrappers, invoke their subcommands with one JSON
argument through the shell tool:

```bash
work-queue work_queue_read '{"pool":"default","limit":32}'
```

Use the advertised MCP tools directly when wrappers are not available.

Submit the prepared cohort whether the queue is initialized or absent:
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

Safe-output processing uses only the compiler-approved Policy proposal; never
choose or modify Policy. The proposal must grant this dispatcher's principal
producer entitlement. If bootstrap or admission fails, report the failure; do
not bypass it with `noop` or ordinary worker dispatch.

The compiled proposal grants producer entitlement only to `pelikhan`
(`4175913`), the authenticated actor of this dispatcher's scheduled runs.
The selected `GH_AW_GITHUB_TOKEN` launch credential must authenticate as that
same approved worker principal; runtime authentication verifies it before any
launch. There is no fallback to the unrelated `GITHUB_TOKEN` bot identity.
Worker routes are pinned to the published revision above, not the current
default branch. Protect the queue branch before enabling first submission.
Changing identities or worker revisions after bootstrap requires a quiescent
queue and an administrator Policy update; editing this proposal alone cannot
replace installed Policy.
