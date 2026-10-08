import { describe, expect, it, vi } from "vitest";

const { fakeGitHub, options } = require("./work_queue_store_checks.cjs");
const { initializeWorkQueue, publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { newRequest, replayTransactions } = require("./work_queue_replay.cjs");
const { administrator, context, dispatcher, genesis, operationCommit, producer, submission } = require("./work_queue_test_helpers.cjs");

function summaryCore() {
  const summary = { addRaw: vi.fn(), write: vi.fn().mockResolvedValue(undefined) };
  summary.addRaw.mockReturnValue(summary);
  return { info: vi.fn(), warning: vi.fn(), summary };
}

function dispatchRequest(id) {
  return newRequest(id, "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
}

describe("queue publication step summaries", () => {
  it("renders Policy installation, Work admission and later control updates only after persistence", async () => {
    const fake = fakeGitHub();
    const core = summaryCore();
    core.summary.addRaw.mockImplementation(markdown => {
      expect(fake.log().length).toBeGreaterThan(0);
      expect(markdown).toContain(replayTransactions(fake.log()).tip.slice(0, 24));
      return core.summary;
    });
    await initializeWorkQueue({ githubClient: fake.githubClient, owner: "owner", repo: "repo", context: context(administrator), core });
    const nodes = submission(fake.log(), ["admitted-node"]).operations;
    const admitted = await publishWorkQueueRequest(options(fake, newRequest("admit-summary", "submit", producer, { nodes }), producer, { core }));
    expect(admitted.publishedNow).toBe(true);
    expect(core.summary.addRaw.mock.calls[1][0]).toContain("### Work queue admission");
    expect(core.summary.addRaw.mock.calls[1][0]).toContain("<code>admitted-node</code>");
    const control = newRequest("control-summary", "control", administrator, { operations: [{ kind: "Control", control: "grants_paused", value: true, reason: "maintenance" }] });
    const updated = await publishWorkQueueRequest(options(fake, control, administrator, { core }));
    expect(updated.publishedNow).toBe(true);
    expect(core.summary.addRaw.mock.calls[2][0]).toContain("<code>control</code>");
    expect(core.summary.write).toHaveBeenCalledTimes(3);
    expect(core.warning).not.toHaveBeenCalled();
  });

  it("summarizes a durable Claim grant once, not idempotent recovery or zero-operation requests", async () => {
    const initial = [genesis()];
    initial.push(submission(initial, ["claimed"]));
    const fake = fakeGitHub(initial);
    const core = summaryCore();
    const request = dispatchRequest("grant-summary");
    const result = await publishWorkQueueRequest(options(fake, request, dispatcher, { core }));
    expect(result.publishedNow).toBe(true);
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(core.summary.addRaw.mock.calls[0][0]).toContain("1 open Claims");
    expect(core.summary.addRaw.mock.calls[0][0]).toContain("1 native reservations (1 unbound)");
    const replay = await publishWorkQueueRequest(options(fake, request, dispatcher, { core }));
    expect(replay.reused).toBe(true);
    const empty = await publishWorkQueueRequest(options(fake, dispatchRequest("no-more-work"), dispatcher, { core }));
    expect(empty.operations).toEqual([]);
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(replayTransactions(fake.log()).claims.size).toBe(1);
  });

  it("never renders a candidate that loses CAS and becomes ineligible", async () => {
    const initial = [genesis()];
    initial.push(submission(initial, ["losing-candidate"]));
    const fake = fakeGitHub(initial);
    const core = summaryCore();
    fake.state.beforeUpdate = async ({ log, install }) => {
      install([...log, operationCommit(log, "pause-before-grant", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "paused" }], administrator, 100)]);
    };
    const result = await publishWorkQueueRequest(options(fake, dispatchRequest("lost-cas-summary"), dispatcher, { core }));
    expect(result.publishedNow).toBe(false);
    expect(result.state.claims.size).toBe(0);
    expect(core.summary.addRaw).not.toHaveBeenCalled();
  });

  it("renders a recovered ambiguous committed write without selecting or charging again", async () => {
    const initial = [genesis()];
    initial.push(submission(initial, ["ambiguous-grant"]));
    const fake = fakeGitHub(initial);
    fake.state.ambiguousOnce = true;
    const core = summaryCore();
    const request = dispatchRequest("ambiguous-summary");
    const result = await publishWorkQueueRequest(options(fake, request, dispatcher, { core }));
    expect(result.recovered).toBe(true);
    expect(result.publishedNow).toBe(false);
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(core.summary.addRaw.mock.calls[0][0]).toContain("1 open Claims");
    await publishWorkQueueRequest(options(fake, request, dispatcher, { core }));
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(fake.state.updates).toBe(1);
    expect(replayTransactions(fake.log()).claims.size).toBe(1);
  });

  it("does not reinterpret a summary write failure as an ambiguous CAS or revoke the winning sender", async () => {
    const initial = [genesis()];
    initial.push(submission(initial, ["summary-io-failure"]));
    const fake = fakeGitHub(initial);
    const core = summaryCore();
    core.summary.write.mockRejectedValue(new Error("summary filesystem unavailable"));
    const result = await publishWorkQueueRequest(options(fake, dispatchRequest("summary-write-failed"), dispatcher, { core }));
    expect(result).toMatchObject({ publishedNow: true, persisted: true, reused: false, recovered: false });
    expect(fake.state.updates).toBe(1);
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("Publication and Claim accounting are unchanged"));
    expect(replayTransactions(fake.log()).claims.size).toBe(1);
  });

  it("keeps rejected queue updates out of summaries", async () => {
    const core = summaryCore();
    const fake = fakeGitHub([genesis()]);
    await expect(publishWorkQueueRequest(options(fake, dispatchRequest("invalid-branch-summary"), dispatcher, { core, branch: "../queue" }))).rejects.toThrow(/branch_invalid/);
    expect(core.summary.addRaw).not.toHaveBeenCalled();
    expect(fake.state.updates).toBe(0);
  });
});
