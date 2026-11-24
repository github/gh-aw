// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactionLog } = require("./work_queue_replay.cjs");
const { releaseAssignment } = require("./work_queue_reconciler.cjs");

const context = { repo: { owner: "owner", repo: "repo" }, runId: 300, runAttempt: 1, payload: {} };
const policy = maxAttempts => ({
  version: 4,
  pending_limit: 30,
  issues: false,
  effect_scope: "owner/repo",
  workers: ["worker"],
  pools: { default: { concurrency: 1, per_account_limit: 0, max_attempts: maxAttempts } },
});

let saved;
beforeEach(() => {
  saved = process.env.GITHUB_WORKFLOW_REF;
  process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/agentics-maintenance.yml@refs/heads/main";
});
afterEach(() => {
  if (saved === undefined) delete process.env.GITHUB_WORKFLOW_REF;
  else process.env.GITHUB_WORKFLOW_REF = saved;
});

function dispatchedLedger() {
  const node = h.work("a");
  const planned = h.dispatchNext(h.submit([], [node]));
  return { node, dispatchId: planned.decision.dispatches[0], log: h.launch(planned.log, planned.decision.dispatches[0]) };
}

describe("releaseAssignment", () => {
  it("releases the Dispatch and returns its Work to the pool", async () => {
    const { node, dispatchId, log } = dispatchedLedger();
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    expect(await releaseAssignment({ githubClient: fake.githubClient, context, policy: policy(3) }, dispatchId, "run_missing")).toEqual({ dispatch_id: dispatchId, released: true, commit_id: expect.stringMatching(/^q_/) });
    expect(replayTransactionLog(fake.contents()).works.get(node.work_id)).toMatchObject({ state: "available", attempts: 1 });
  });

  it("cancels Work whose last attempt the release consumed", async () => {
    const { node, dispatchId, log } = dispatchedLedger();
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    await releaseAssignment({ githubClient: fake.githubClient, context, policy: policy(1) }, dispatchId, "run_terminal");
    expect(replayTransactionLog(fake.contents()).works.get(node.work_id)).toMatchObject({ state: "cancelled", cancellation_reason: "attempts_exhausted", attempts: 1 });
  });

  it("commits nothing when the Dispatch is closed or no longer applies", async () => {
    const { dispatchId, log } = dispatchedLedger();
    const fake = h.fakeQueueGitHub(h.ledgerText(h.release(log, dispatchId)));
    expect((await releaseAssignment({ githubClient: fake.githubClient, context, policy: policy(3) }, dispatchId, "operator")).released).toBe(false);
    const open = h.fakeQueueGitHub(h.ledgerText(log));
    expect((await releaseAssignment({ githubClient: open.githubClient, context, policy: policy(3) }, dispatchId, "operator", () => false)).released).toBe(false);
    expect(open.contents()).toBe(h.ledgerText(log));
  });
});
