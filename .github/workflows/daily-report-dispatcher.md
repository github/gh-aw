---
name: Daily Discussion Report Dispatcher
description: Admits a rotating three-of-ten discussion-report cohort and requests its bounded native queue grants
on:
  schedule: daily around 10:00
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
        const { data: run } = await github.rest.actions.getWorkflowRun({ ...context.repo, run_id: context.runId });
        const { data: repository } = await github.rest.repos.get(context.repo);
        const plan = buildDailyReportPlan({
          date: new Date(Date.parse(run.created_at) - 86400000).toISOString().slice(0, 10),
          repository: repository.full_name,
          repositoryId: String(repository.id),
        });
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/daily-report-plan.json", JSON.stringify(plan));
        core.info(`Daily report cohort ${plan.date}: ${plan.selected.join(", ")}`);
timeout-minutes: 15
strict: true
---

# Daily Discussion Report Dispatcher

Read `/tmp/gh-aw/agent/daily-report-plan.json`. The trusted preparation step picks
three distinct members of the ten-workflow portfolio using a deterministic UTC
rotation. Use the exact stored date, node definitions and budgets; never invent
additional candidates or admission identities.

1. Call `work_queue_read` with `{"pool":"daily-reports","limit":32}`. An absent
   Policy is a deployment failure, not permission to bootstrap or dispatch
   ordinary workflows. Surface queue errors explicitly.
2. Call `work_queue_submit` once with `{"nodes": <the plan.nodes array>}`.
   Date-keyed graph/node identities make an identical submission idempotent.
   Do not edit payloads on a retry or replace immutable admitted Work.
3. Call `work_queue_dispatch_next` once with the exact `plan.dispatch`:
   `{"pool":"daily-reports","max_claims":3,"max_dispatches":3}`.
   Do not select winning Work IDs, workflows or revisions from the snapshot.
   The trusted scheduler selects the eligible fair prefix against fresh state
   and records its Claims and reservations in `work-queue.jsonl`.

Use only the advertised work-queue MCP tools (or their `work-queue` CLI wrapper).
Do not call ordinary `dispatch_workflow` or target-specific dispatch tools.
The allowlist approves routes; the installed Policy pins actual immutable
revisions, principals, singleton assignments and resource scope.

This workflow has one daily schedule and no manual-dispatch trigger. Whole-run
reruns cannot admit another cohort. At most three native launches are permitted
in its original daily activation; capacity, pauses or uncertain old launches may
permit fewer, and older eligible cohorts may run before newly admitted reports.
Do not force-release reservations to make a target count. Three launches are not
a guarantee of three successfully published discussions. Use `noop` if there is
no eligible grant; never label a staged intent as a completed report.
