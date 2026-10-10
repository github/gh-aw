---
name: Engine Conformance Dispatcher
description: Prioritize supported engines, recent changes, and rotating conformance coverage through a bounded work queue
intent: Detect engine configuration regressions with host-verified conformance checks across the supported and sample engine fleet.
on:
  schedule: daily around 09:00
  workflow_dispatch:
if: github.run_attempt == 1
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine: copilot
tools:
  work-queue: true
  cli-proxy: true
  bash:
    - cat /tmp/gh-aw/agent/engine-conformance-plan.json
safe-outputs:
  dispatch-workflow:
    workflows:
      - engine-conformance-agy
      - engine-conformance-aider
      - engine-conformance-claude
      - engine-conformance-codex
      - engine-conformance-copilot
      - engine-conformance-crush
      - engine-conformance-cursor
      - engine-conformance-deepseek-harness
      - engine-conformance-gemini
      - engine-conformance-goose
      - engine-conformance-kiro
      - engine-conformance-opencode
      - engine-conformance-pi
      - engine-conformance-pydantic-ai
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
  noop:
    report-as-issue: false
steps:
  - name: Prepare engine conformance cohort
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { buildEngineConformancePlan, ENGINES } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/engine_conformance_portfolio.cjs"));
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const date = new Date(run.created_at).toISOString().slice(0, 10);
        const until = new Date(Date.parse(date + "T00:00:00Z") - 7 * 86400000).toISOString();
        const { data: older } = await github.rest.repos.listCommits({ ...context.repo, sha: repository.default_branch, until, per_page: 1 });
        let changedPaths = [];
        if (older.length) {
          const { data: comparison } = await github.rest.repos.compareCommits({
            ...context.repo, base: older[0].sha, head: repository.default_branch,
          });
          changedPaths = comparison.total_commits > 250 || (comparison.files || []).length >= 300
            ? ENGINES.map(engine => `.github/workflows/engine-conformance-${engine}.md`)
            : (comparison.files || []).map(file => file.filename);
        }
        const plan = buildEngineConformancePlan({
          date, repository: repository.full_name, repositoryId: String(repository.id), changedPaths,
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/engine-conformance-plan.json", JSON.stringify(plan));
        core.info(`Engine conformance cohort ${date}: ${plan.selected.join(", ")}`);
timeout-minutes: 15
strict: true
---

# Engine Conformance Dispatcher

Read `/tmp/gh-aw/agent/engine-conformance-plan.json`. The trusted preparation
step selects three distinct engines: a supported engine first, a recently
changed engine (or a second supported engine), and a rotating coverage engine.
Use the exact date, nodes and budget from the plan. The 7-day change window
is a scheduling signal, not a new conformance requirement.

Read the queue with `work_queue_read` using
`{"pool":"engine-conformance","limit":32}`. An uninitialized queue is an empty
backlog; the first trusted submission atomically installs Policy and Work.
An existing policyless ledger is a failure. Surface tool failures instead of
claiming an empty queue, and never dispatch ordinary workflows.

Call `work_queue_submit` once with `{"nodes": <plan.nodes>}`. Date-keyed Work
identities make a same-day repeat idempotent. Then call
`work_queue_dispatch_next` once with the exact `plan.dispatch`:
`{"pool":"engine-conformance","max_claims":3,"max_dispatches":3}`. Use only
the advertised work-queue MCP tools or their CLI wrappers. Do not choose the
winning Work, worker, revision, or target: the installed Policy does that.
Staged intents are not evidence of native launches or successful checks.

Global scheduling for the `engine-conformance` pool and its fairness keys lives
in `.github/workflows/aw.json` under `work_queue`. AW handles authorization,
approved worker routes and actual native-run binding; no producer enrollment,
worker-principal configuration or administrator seeding is required.
The three-launch cap may produce fewer launches if capacity is unavailable.
Use `noop` only when there is no eligible grant, not when admission fails.
