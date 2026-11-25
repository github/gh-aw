---
name: Smoke Work Queue Dispatcher
description: Top the smoke pool up to three synthetic tasks every six hours and request three native smoke worker Claims
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
            const { owner, repo } = context.repo;
            const runUrl = `${context.serverUrl}/${owner}/${repo}/actions/runs/${context.runId}/attempts/${context.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT}`;
            const issue = await github.rest.issues.create({
              owner,
              repo,
              title: `[WorkQueue Health] Smoke Work Queue Dispatcher failed - run ${context.runId}`,
              body: [
                "## Smoke Work Queue Dispatcher failure",
                "",
                `**Workflow run:** ${runUrl}`,
                "",
                "| Job | Result |",
                "| --- | --- |",
                `| agent | ${process.env.SMOKE_AGENT_RESULT} |`,
                `| safe_outputs | ${process.env.SMOKE_SAFE_OUTPUTS_RESULT} |`,
                `| verify_smoke_dispatch | ${process.env.SMOKE_VERIFY_RESULT} |`,
                "",
                "`verify_smoke_dispatch` requires a successful agent and safe_outputs job plus exactly three accepted `smoke-work-queue-worker` dispatches. Check the `verify_smoke_dispatch` log for the failed assertion, then the queue ledger and worker runs.",
              ].join("\n"),
              labels: ["automation", "testing", "backlog:run-alerts"],
              assignees: ["elefthei"],
            });
            core.info(`Opened issue #${issue.data.number}`);
steps:
  - name: Prepare synthetic work and bounded dispatch
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
        const { loadWorkQueueSnapshot, readWorkQueueState } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_mcp_server.cjs"));
        const node = node_key => ({
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
          },
          depends_on: [],
        });
        // Continuation successors of the previous cycle are already available; top the pool up to three so each cycle dispatches exactly three.
        const available = readWorkQueueState(loadWorkQueueSnapshot(), { pool: "smoke-work-queue", limit: 1 }).counts.available ?? 0;
        const roots = Math.max(1, 3 - available);
        const plan = {
          version: 1,
          nodes: Array.from({ length: roots }, (_, index) => node(`smoke-${index + 1}`)),
          dispatch: { pool: "smoke-work-queue", max_claims: 3, max_dispatches: 3 },
        };
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/smoke-work-queue-plan.json", JSON.stringify(plan));
        stagePlanIntents(path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl"), plan);
        core.info(`Staged ${roots} synthetic Work (${available} already available) and one bounded smoke dispatch request for three Claims`);
timeout-minutes: 10
strict: true
---

# Work Queue Dispatcher Smoke

Read `/tmp/gh-aw/agent/smoke-work-queue-plan.json`, then call `work_queue_read`
with `{"pool":"smoke-work-queue","limit":8}`. An absent queue is empty. A tool
error is a failure, not an empty backlog.

The trusted preparation step stages one to three synthetic Work (`node_key`
`smoke-1`…, identical payloads), enough to bring the pool's available smoke
Work to three, followed by
`{"pool":"smoke-work-queue","max_claims":3,"max_dispatches":3}`. Completed
smoke Claims each admit one queue-routed continuation successor, so the pool
usually already holds successors from the previous cycle. Explain the
staged plan; do not submit again, choose a winner, dispatch directly, or emit
`noop`. Work identity is stable for this activation, and whole-run reruns
are disabled. The scheduler takes the oldest available smoke Work first, so it
may select an older smoke task when recovering a backlog; never release
capacity or modify other pools.

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
