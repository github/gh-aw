---
name: Smoke Work Queue Dispatcher
description: Keep the smoke pool at three synthetic tasks every six hours and request three native smoke worker Claims
intent: Detect failures in native work queue admission, dispatch, and worker completion without changing source content, allowing framework queue tracking mutations.
on:
  schedule: every 6 hours
if: github.run_attempt == 1
imports:
  - shared/work-queue-staged-plan.md
permissions:
  contents: read
  copilot-requests: write
engine: copilot
network:
  allowed: []
tools:
  work-queue: true
  cli-proxy: true
  bash:
    - cat /tmp/gh-aw/agent/smoke-work-queue-plan.json
safe-outputs:
  # No agent-authored effects; keep verification runnable even when the agent is skipped.
  threat-detection: false
  dispatch-workflow:
    workflows: [smoke-work-queue-worker]
    target-ref: ${{ github.event.repository.default_branch }}
    max: 3
jobs:
  verify_smoke_dispatch:
    needs: [agent, safe_outputs]
    if: always()
    runs-on: ubuntu-slim
    timeout-minutes: 3
    permissions: {}
    steps:
      - name: Require accepted smoke dispatch
        env:
          SMOKE_AGENT_RESULT: ${{ needs.agent.result }}
          SMOKE_SAFE_OUTPUTS_RESULT: ${{ needs.safe_outputs.result }}
          SMOKE_DISPATCH_RECEIPT: ${{ needs.safe_outputs.outputs.work_queue_requests }}
        run: |
          set -euo pipefail
          node <<'JS'
          const assert = require("node:assert/strict");
          assert.equal(process.env.SMOKE_AGENT_RESULT, "success", "Smoke agent job must run and succeed");
          assert.equal(process.env.SMOKE_SAFE_OUTPUTS_RESULT, "success", "Trusted smoke dispatch processing must succeed");
          const result = JSON.parse(process.env.SMOKE_DISPATCH_RECEIPT);
          assert.equal(result.version, 4);
          assert.equal(result.success, true, "Trusted queue controls must succeed");
          const dispatches = result.receipts.filter(receipt => Object.hasOwn(receipt, "dispatches"));
          assert.equal(dispatches.length, 1, "Smoke requires exactly one trusted dispatch receipt");
          const receipt = dispatches[0];
          assert.equal(receipt.status, "committed", "Smoke requires a committed dispatch request");
          assert.equal(receipt.dispatches, 3, "Smoke requires exactly three worker dispatches");
          assert.equal(receipt.launches.length, 3);
          for (const launch of receipt.launches) {
            assert.equal(launch.state, "started", "Smoke requires every worker dispatch to be accepted");
            assert.match(launch.dispatch_id, /^d_[0-9a-f]{64}_1$/);
          }
          assert.equal(new Set(receipt.launches.map(launch => launch.dispatch_id)).size, 3, "Smoke requires three distinct worker dispatches");
          JS
  report_smoke_failure:
    needs: [agent, safe_outputs, verify_smoke_dispatch]
    # failure() is required: without a status function the implicit success() would skip this job.
    # Reruns (run_attempt > 1) skip activation by design and fail verify structurally; do not alert on them.
    if: failure() && needs.verify_smoke_dispatch.result == 'failure' && github.run_attempt == 1
    runs-on: ubuntu-slim
    timeout-minutes: 3
    permissions:
      issues: write
    steps:
      - name: Open smoke failure issue assigned to elefthei
        uses: actions/github-script@v9.0.0
        env:
          SMOKE_AGENT_RESULT: ${{ needs.agent.result }}
          SMOKE_SAFE_OUTPUTS_RESULT: ${{ needs.safe_outputs.result }}
          SMOKE_VERIFY_RESULT: ${{ needs.verify_smoke_dispatch.result }}
        with:
          script: |
            const path = require("node:path");
            const { buildIssueReport, createReportIssue } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/setup/js/issue_report.cjs"));
            const text = value => ({ type: "text", value: String(value), agentTainted: false });
            const { owner, repo } = context.repo;
            const runUrl = `${context.serverUrl}/${owner}/${repo}/actions/runs/${context.runId}/attempts/${context.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT}`;
            const title = `[WorkQueue Health] Smoke Work Queue Dispatcher failed - run ${context.runId}`;
            const report = buildIssueReport(title, [
              { type: "heading", level: 2, children: [text("Smoke Work Queue Dispatcher failure")], agentTainted: false },
              {
                type: "paragraph",
                children: [
                  text("Workflow run: "),
                  { type: "link", url: runUrl, children: [text(runUrl)], agentTainted: false },
                ],
                agentTainted: false,
              },
              {
                type: "table",
                headers: [[text("Job")], [text("Result")]],
                rows: [
                  [[text("agent")], [text(process.env.SMOKE_AGENT_RESULT)]],
                  [[text("safe_outputs")], [text(process.env.SMOKE_SAFE_OUTPUTS_RESULT)]],
                  [[text("verify_smoke_dispatch")], [text(process.env.SMOKE_VERIFY_RESULT)]],
                ],
                agentTainted: false,
              },
              {
                type: "paragraph",
                children: [
                  text("The "),
                  { type: "code", value: "verify_smoke_dispatch", agentTainted: false },
                  text(" check requires successful agent and safe_outputs jobs plus exactly three accepted "),
                  { type: "code", value: "smoke-work-queue-worker", agentTainted: false },
                  text(" dispatches. Check the "),
                  { type: "code", value: "verify_smoke_dispatch", agentTainted: false },
                  text(" log for the failed assertion, then inspect the queue ledger and worker runs."),
                ],
                agentTainted: false,
              },
            ], { agentTainted: false });
            const issue = await createReportIssue(github, {
              owner,
              repo,
              title,
              labels: ["automation", "testing", "backlog:run-alerts"],
              assignees: ["elefthei"],
            }, report);
            core.info(`Opened issue #${issue.data.number}`);
steps:
  - name: Prepare synthetic work and bounded dispatch
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { INTENT_VERSION, stageIntent, stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
        const { loadWorkQueueSnapshot, readWorkQueueState } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_mcp_server.cjs"));
        const node = (node_key, extra = {}) => ({
          graph_id: `smoke-work-queue:${context.runId}`,
          node_key,
          pool: "smoke-work-queue",
          worker_profile: "smoke-work-queue-worker",
          payload: {
            task: "Complete the synthetic work queue smoke task.",
            smoke: "work-queue-v1",
            resource_scope: {
              version: 1,
              resources: [{
                host: "github.com",
                repository: `${context.repo.owner}/${context.repo.repo}`,
                repository_id: String(context.payload.repository.id),
              }],
            },
            effect_contract: { kind: "none" },
            ...extra,
          },
          depends_on: [],
        });
        // Each dispatched Claim leaves at most one available Work behind (a successor or a retry), so the
        // pool holds 0..3 available Work here. Top it up to three; when it is full, only dispatch.
        const available = readWorkQueueState(loadWorkQueueSnapshot(), { pool: "smoke-work-queue", limit: 1 }).counts.available ?? 0;
        const roots = Math.max(0, 3 - available);
        const dispatch = { pool: "smoke-work-queue", max_claims: 3, max_dispatches: 3 };
        // smoke-1 fails its first attempt on purpose; its successors inherit the payload and do the same.
        const plan = { version: 1, nodes: Array.from({ length: roots }, (_, index) => node(`smoke-${index + 1}`, index === 0 ? { smoke_mode: "fail-once" } : {})), dispatch };
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/smoke-work-queue-plan.json", JSON.stringify(plan));
        const intents = path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl");
        if (roots > 0) stagePlanIntents(intents, plan);
        else stageIntent(intents, { version: INTENT_VERSION, intent_id: "smoke:dispatch_next", kind: "dispatch_next", parameters: dispatch });
        core.info(`Staged ${roots} synthetic Work (${available} already available) and one bounded smoke dispatch request for three Claims`);
timeout-minutes: 10
strict: true
---

# Work Queue Dispatcher Smoke

Read `/tmp/gh-aw/agent/smoke-work-queue-plan.json`, then call `work_queue_read`
with `{"pool":"smoke-work-queue","limit":8}`. An absent queue is empty. A tool
error is a failure, not an empty backlog.

The trusted preparation step stages zero to three synthetic Work (`node_key`
`smoke-1`…, identical payloads except that `smoke-1` carries
`smoke_mode: "fail-once"`), enough to bring the pool's available smoke Work to
three, followed by `{"pool":"smoke-work-queue","max_claims":3,"max_dispatches":3}`;
when the pool already holds three, it stages the dispatch request alone, so the
plan file's `nodes` may be empty. Completed smoke Claims admit queue-routed
continuation successors (two hops per root) and a failed fail-once attempt
returns its Work for one retry, so the pool usually already holds Work from the
previous cycle. Explain the staged plan; do not submit again, choose a winner,
dispatch directly, or emit `noop`. Work identity is stable for this
activation, and whole-run reruns are disabled. The scheduler takes the oldest
available smoke Work first, so it may select an older smoke task when
recovering a backlog; never release capacity or modify other pools.

The assignment-only smoke worker checks the synthetic payload and finishes its
original Claim without resource effects. A staged request is not a launch.
Check the queue ledger and the worker run when diagnosing failures; do not
wait for workers in this run.

Pool limits come from `.github/workflows/aw.json` and take effect on the next
run after recompilation. The dedicated pool has concurrency three. Fewer than
three dispatches is a smoke failure; the companion `smoke-work-queue-monitor`
workflow reports stuck or failed Work. Trusted dispatch verification rejects
missing receipts and launches that were not accepted; an always-running
downstream check also rejects skipped or failed jobs. A failed check opens a
`[WorkQueue Health]` issue assigned to `elefthei`. The dispatcher does not
wait for worker binding or completion.

`effect_contract: {kind: "none"}` forbids Claim resource effects, not framework
queue bookkeeping. Queue ledger commits, tracking Issues, labels, and Claim
comments produced by the repository's enabled Issue projection are permitted
smoke effects. Neither agent may create or modify source content or request
other Issue mutations. Never reset or reseed an existing queue.
