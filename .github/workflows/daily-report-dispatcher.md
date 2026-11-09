---
name: Daily Discussion Report Dispatcher
description: Admits a bounded discussion-report cohort with daily rotation and weekly report slots, then requests native queue grants
on:
  schedule: daily around 12:00
if: github.run_attempt == 1
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
    - cat /tmp/gh-aw/agent/daily-report-plan.json
safe-outputs:
  dispatch-workflow:
    workflows:
      - daily-compiler-quality
      - daily-evals-report
      - daily-firewall-report
      - daily-issues-report
      - daily-observability-report
      - daily-regulatory
      - daily-repo-chronicle
      - daily-secrets-analysis
      - daily-team-evolution-insights
      - daily-token-consumption-report
      - deep-report
      - artifacts-summary
      - repo-tree-map
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
  noop:
steps:
  - name: Prepare the UTC daily report cohort
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { buildDailyReportPlan } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/daily_report_portfolio.cjs"));
        const { stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const plan = buildDailyReportPlan({
          date: new Date(Date.parse(run.created_at) - 86400000).toISOString().slice(0, 10),
          repository: repository.full_name,
          repositoryId: String(repository.id),
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/daily-report-plan.json", JSON.stringify(plan));
        stagePlanIntents(path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl"), plan);
        core.info(`Daily report cohort ${plan.date}: ${plan.selected.join(", ")}`);
timeout-minutes: 15
strict: true
---

# Daily Discussion Report Dispatcher

Read `/tmp/gh-aw/agent/daily-report-plan.json`. The trusted preparation step picks
three distinct members of the thirteen-workflow portfolio using a deterministic
UTC rotation. DeepReport reserves one slot every day to synthesize previously
published reports. Artifact and tree reports reserve one slot on Sunday and Monday
activations respectively; the remaining slots rotate across the ten daily
profiles without skipping a member. DeepReport is independent Work, not a
same-cohort fan-in: it analyzes prior published evidence and never waits inside
its run for other workers. Use the exact stored date, node definitions and budgets; never invent
additional candidates or admission identities.

1. Call `work_queue_read` with `{"pool":"daily-reports","limit":32}`. An absent
   queue is an empty backlog; the first trusted submission atomically publishes
   Policy and Work. Reads never seed the queue. Surface queue errors explicitly.
2. The staged submission contains `{"nodes": <the plan.nodes array>}`.
   Date-keyed graph/node identities make an identical submission idempotent.
   Do not edit payloads on a retry or replace immutable admitted Work.
3. The staged dispatch request contains the exact `plan.dispatch`:
   `{"pool":"daily-reports","max_claims":3,"max_dispatches":3}`.
   Do not select winning Work IDs, workflows or revisions from the snapshot.
   The trusted scheduler selects the eligible fair prefix against fresh state
   and records its Claims and reservations in `work-queue.jsonl`.

Use only the advertised work-queue MCP tools (or their `work-queue` CLI wrapper).
Do not call ordinary `dispatch_workflow` or target-specific dispatch tools.
The allowlist approves routes; the installed Policy pins actual immutable
revisions, singleton assignments and resource scope. AW handles authorization
and binds the actual native run to each Claim. Scheduling overrides, including
the `daily-reports` pool and equal-weight report keys, live in the shared
`.github/workflows/aw.json` `work_queue` section.

This workflow runs daily around 12:00 UTC, away from the observed 10:00 UTC
shared API quota exhaustion. This timing reduces contention but does not
guarantee quota availability; daily credit accounting must still fail closed
when it cannot verify usage.

This workflow has one daily schedule and no manual-dispatch trigger. Whole-run
reruns cannot admit another cohort. At most three native launches are permitted
in its original daily activation; capacity, pauses or uncertain old launches may
permit fewer, and older eligible cohorts may run before newly admitted reports.
Do not force-release reservations to make a target count. Three launches are not
a guarantee of three successfully published discussions. Report only the staged
request; never label it as a completed report or infer
that a stale snapshot guarantees no eligible grant.

## Status report format

Use `###` or lower headings for any status report. Start with a visible summary
of the staged request, then key counts and dispatch status, collapse lengthy
cohort details in `<details><summary><b>...</b></summary>...</details>`, and end
with next actions or limitations. Keep staging failures and the distinction
between staged requests and completed reports visible; never imply that a report
was published. Format applicable run references as
`[§12345](https://github.com/${{ github.repository }}/actions/runs/12345)`.
