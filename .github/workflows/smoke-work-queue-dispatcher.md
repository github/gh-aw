---
name: Smoke Work Queue Dispatcher
description: Submit three synthetic tasks every six hours and request three native smoke worker Claims
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
          JS
steps:
  - name: Prepare synthetic work and bounded dispatch
    uses: actions/github-script@v9.0.0
    with:
      script: |
        const fs = require("node:fs");
        const path = require("node:path");
        const { stagePlanIntents } = require(path.join(process.env.RUNNER_TEMP, "gh-aw/actions/work_queue_intents.cjs"));
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
        const plan = {
          version: 1,
          nodes: ["smoke-1", "smoke-2", "smoke-3"].map(node),
          dispatch: { pool: "smoke-work-queue", max_claims: 3, max_dispatches: 3 },
        };
        fs.mkdirSync("/tmp/gh-aw/agent", { recursive: true });
        fs.writeFileSync("/tmp/gh-aw/agent/smoke-work-queue-plan.json", JSON.stringify(plan));
        stagePlanIntents(path.join(process.env.RUNNER_TEMP, "gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl"), plan);
        core.info("Staged three synthetic Work and one bounded smoke dispatch request for three Claims");
timeout-minutes: 10
strict: true
---

# Work Queue Dispatcher Smoke

Read `/tmp/gh-aw/agent/smoke-work-queue-plan.json`, then call `work_queue_read`
with `{"pool":"smoke-work-queue","limit":8}`. An absent queue is empty. A tool
error is a failure, not an empty backlog.

The trusted preparation step stages exactly three synthetic Work (`node_key`
`smoke-1`, `smoke-2`, `smoke-3`, identical payloads) followed by
`{"pool":"smoke-work-queue","max_claims":3,"max_dispatches":3}`. Explain the
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
downstream check also rejects skipped or failed jobs. The dispatcher does not
wait for worker binding or completion.

`effect_contract: {kind: "none"}` forbids Claim resource effects, not framework
queue bookkeeping. Queue ledger commits, tracking Issues, labels, and Claim
comments produced by the repository's enabled Issue projection are permitted
smoke effects. Neither agent may create or modify source content or request
other Issue mutations. Never reset or reseed an existing queue.
