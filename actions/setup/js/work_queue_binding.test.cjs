import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { exhaustedCancellation, maxAttempts, publishRequest, releaseOperations } = require("./work_queue_binding.cjs");

const policy = attempts => ({
  version: 4,
  pending_limit: 10,
  issues: false,
  pools: { default: { concurrency: 1, per_account_limit: 0, max_attempts: attempts, default_profile: "worker", profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } } } },
});

function claimed() {
  const planned = h.dispatchNext(h.submit([], [h.work("a")]), { concurrency: 1 });
  return { log: planned.log, dispatch: planned.decision.dispatches[0] };
}

describe("attempts rule", () => {
  it("cancels Work whose cancelled Claim uses its last attempt", () => {
    const { log, dispatch } = claimed();
    const state = replayTransactions(log);
    const claim = state.claims.get(state.dispatches.get(dispatch).claims[0]);
    expect(maxAttempts(null, "default")).toBe(3);
    expect(maxAttempts(policy(5), "default")).toBe(5);
    expect(exhaustedCancellation(state, claim, policy(2))).toEqual([]);
    expect(exhaustedCancellation(state, claim, policy(1))).toEqual([{ kind: "WorkCancellation", work_id: claim.work_id, reason: "attempts_exhausted", claim_id: claim.claim_id }]);
    expect(releaseOperations(state, dispatch, "run_missing", policy(1))).toEqual([{ kind: "Release", dispatch_id: dispatch, reason: "run_missing" }, ...exhaustedCancellation(state, claim, policy(1))]);
    expect(releaseOperations(state, "d_missing", "run_missing", policy(1))).toEqual([]);
    expect(releaseOperations(replayTransactions(h.release(log, dispatch)), dispatch, "operator", policy(1))).toEqual([]);
  });
});

describe("job publication", () => {
  it("publishes as the job's own actor", async () => {
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a")])));
    const result = await publishRequest({
      githubClient: fake.githubClient,
      context: { repo: { owner: "owner", repo: "repo" }, runId: 12, runAttempt: 1 },
      role: "producer",
      kind: "submit",
      parameters: { work_ids: [h.work("b").work_id] },
      operationsFor: () => [h.work("b")],
    });
    expect(result.commit.actor).toMatchObject({ role: "producer", repository: "owner/repo", run_id: "12", run_attempt: 1 });
    expect(result.state.works.has(h.work("b").work_id)).toBe(true);
  });
});
