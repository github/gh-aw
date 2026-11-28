---
name: ESLint Factory Dispatcher
description: Admits daily ESLint factory tasks and dispatches queued work to the corresponding worker
on:
  schedule: daily
  workflow_dispatch:
imports:
  - shared/reporting.md
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
    max: 24
  noop:
steps:
  - name: Prepare the UTC ESLint factory cohort
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { buildESLintFactoryPlan, parseESLintRuleIds } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/eslint_factory_portfolio.cjs"));
        const { stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const date = new Date(run.created_at).toISOString().slice(0, 10);
        const cutoff = new Date(Date.parse(`${date}T00:00:00.000Z`) - 1).toISOString();
        const { data: commits } = await github.rest.repos.listCommits({ ...context.repo, path: "eslint-factory/src/index.ts", sha: repository.default_branch, until: cutoff, per_page: 1 });
        if (!Array.isArray(commits) || !commits[0]?.sha) throw new Error("ESLint registry revision before the UTC cohort date was not found");
        const registryRevision = commits[0].sha;
        const { data: registry } = await github.rest.repos.getContent({ ...context.repo, path: "eslint-factory/src/index.ts", ref: registryRevision });
        if (registry.type !== "file" || registry.encoding !== "base64" || !registry.content) throw new Error("ESLint rule registry must be readable as a base64 file");
        const rules = parseESLintRuleIds(Buffer.from(registry.content, "base64").toString("utf8"));
        const plan = buildESLintFactoryPlan({
          date,
          repository: repository.full_name,
          repositoryId: String(repository.id),
          registryRevision,
          rules,
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/eslint-factory-plan.json", JSON.stringify(plan));
        stagePlanIntents(path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl"), plan);
        core.info(`ESLint factory cohort ${plan.date}: ${plan.nodes.length} tasks for rules ${plan.rules.join(", ")}`);
timeout-minutes: 15
strict: true
---

# ESLint Factory Dispatcher

Read `/tmp/gh-aw/agent/eslint-factory-plan.json`. The trusted preparation step
pins the rule registry to the latest commit before the cohort's UTC date and
defines one miner task plus one refiner task and one monster task for each of
the day's three rotating rules (`plan.rules`). Use the exact stored UTC date,
registry revision, nodes, profiles, contracts, and budgets.
Do not invent additional tasks or broaden resource scopes.

Read the activation snapshot first with `work_queue_read`, using
`{"pool":"eslint-factory","limit":32}`. Also inspect `{"pool":"default","limit":32}`
for legacy ESLint Work. A null `snapshot_sha` means the queue branch
does not exist yet.
Treat it as an empty backlog; the first trusted producer submission
creates the branch with its Work. Surface read/tool errors explicitly; never
report a failed read as an empty backlog.

When `<mcp-clis>` advertises the wrappers, invoke their subcommands with one JSON
argument through the shell tool:

```bash
work-queue work_queue_read '{"pool":"eslint-factory","limit":32}'
work-queue work_queue_read '{"pool":"default","limit":32}'
```

Use the advertised MCP tools directly when wrappers are not available.

The trusted preparation step has already staged the prepared cohort, whether
the queue is initialized or absent, with `{"nodes": <the plan.nodes array>}`
before its dispatch requests. Do not call `work_queue_submit` or
`work_queue_dispatch_next` again; read and explain the snapshot and staged plan
only. The pinned registry revision and date-keyed graph/node identities make
same-day preparations idempotent, including manual runs and reruns. Do not
change stored task payloads on retries or replace admitted Work. AW authorizes
trusted participants; the compiled queue policy controls scheduling for pool
`eslint-factory`, priority `3`, and accounting key `""`. A failed admission is
not permission to bypass scheduling bounds.
Submission is a staged intent, not proof of durable admission.

The following request is already staged from `plan.dispatch`:

```json
{"pool":"eslint-factory","max_claims":21,"max_dispatches":21}
```

Keep the request within the compiled pool limits and this workflow's dispatch
budget. Even an empty snapshot or a prediction of no eligible Work can be stale;
trusted processing refreshes the ledger. A response with `status: "staged"` and
an `intent_id` is not proof of Claims, native launches, or completed Work. Report
only that the request was staged, not that workers ran or that the live queue is
empty.

The prepared plan also stages a `default` pool request capped at three claims to
drain legacy ESLint Work. Keep both requests within compiled pool limits and this
workflow's dispatch budget.

Do not select Work IDs, workers, revisions, or targets from a queue snapshot.
The scheduler selects eligible Work in priority, then FIFO order and launches
the compatible worker profile from the compiled policy. The legacy request is
limited to the three ESLint worker workflows, which accept old payloads without
`rule.id` using their earlier broad scope. This workflow's
`safe-outputs.dispatch-workflow.workflows` list is the compiler-approved
worker-name allowlist; it does not replace the compiled profiles or Claim/run
ownership. Do not call ordinary `dispatch_workflow` or typed per-worker dispatch
tools.

Safe-output processing uses only the compiler-generated queue policy; never
choose or modify it. If admission fails, report the failure; do not bypass it
with `noop` or ordinary worker dispatch.

Scheduling overrides are shared in `.github/workflows/aw.json` under
`work_queue`. Trusted `safe-outputs.dispatch-workflow` processing uses the
built-in Actions token with `actions: write`; no custom launch secret is required.
Trusted queue admission and safe outputs use the same built-in token, not
`GH_AW_GITHUB_TOKEN` or another custom secret.
AW dispatches declared worker routes and binds each worker run to its dispatch.
The first accepted submission automatically creates the queue; no
administrator seed is required. Scheduling changes in `aw.json` take effect on
the next run after recompilation.
