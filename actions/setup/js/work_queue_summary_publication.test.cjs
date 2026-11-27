import { describe, expect, it, vi } from "vitest";

const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { replayTransactionLog } = require("./work_queue_replay.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { dispatchNext, dispatchParameters, dispatcher, fakeQueueGitHub, ledgerText, producer, submit, work } = require("./work_queue_test_helpers.cjs");

function summaryCore() {
  const summary = { addRaw: vi.fn(), write: vi.fn().mockResolvedValue(undefined) };
  summary.addRaw.mockReturnValue(summary);
  return { info: vi.fn(), warning: vi.fn(), summary };
}

function publishOptions(fake, actor, request, operationsFor, extra = {}) {
  return { githubClient: fake.githubClient, owner: "owner", repo: "repo", actor, request, operationsFor, ...extra };
}

/** A dispatch_next publication that re-plans against every CAS re-read. */
function dispatchOptions(fake, core, actor = dispatcher, extra = {}) {
  const parameters = dispatchParameters({ max_claims: 1, max_dispatches: 1 });
  return publishOptions(fake, actor, { kind: "dispatch_next", parameters }, (state, request) => planDispatch(state, parameters, request.id).operations, { core, ...extra });
}

const ledger = fake => replayTransactionLog(fake.contents());

describe("queue publication step summaries", () => {
  it("renders Work admission and later updates only after persistence", async () => {
    const fake = fakeQueueGitHub(ledgerText(submit([], [work("seed")])));
    const core = summaryCore();
    core.summary.addRaw.mockImplementation(markdown => {
      expect(markdown).toContain(ledger(fake).tip.slice(0, 24));
      return core.summary;
    });
    const node = work("admitted-node");
    const admitted = await publishWorkQueueRequest(publishOptions(fake, producer, { kind: "submit", parameters: { work_ids: [node.work_id] } }, () => [node], { core }));
    expect(admitted).toMatchObject({ committed: true, recovered: false });
    expect(core.summary.addRaw.mock.calls[0][0]).toMatch(/^<details>\n<summary>Work queue admission<\/summary>\n\n/);
    expect(core.summary.addRaw.mock.calls[0][0]).toContain("<code>admitted-node</code>");
    const granted = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(granted.committed).toBe(true);
    expect(core.summary.addRaw.mock.calls[1][0]).toContain("<code>dispatch_next</code>");
    expect(core.summary.write).toHaveBeenCalledTimes(2);
    expect(core.warning).not.toHaveBeenCalled();
  });

  it("summarizes a durable Claim once, not idempotent recovery or zero-operation requests", async () => {
    const fake = fakeQueueGitHub(ledgerText(submit([], [work("claimed")])));
    const core = summaryCore();
    const result = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(result).toMatchObject({ committed: true, recovered: false });
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(core.summary.addRaw.mock.calls[0][0]).toContain("1 open Claims; 1 active Dispatches (1 not yet bound to a run)");
    const replay = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(replay).toMatchObject({ committed: true, recovered: true });
    const empty = await publishWorkQueueRequest(dispatchOptions(fake, core, { ...dispatcher, run_id: "101" }));
    expect(empty).toMatchObject({ committed: false, commit: null });
    expect(core.summary.addRaw).toHaveBeenCalledTimes(1);
    expect(ledger(fake).claims.size).toBe(1);
  });

  it("never renders a candidate that loses CAS and becomes ineligible", async () => {
    const log = submit([], [work("losing-candidate")]);
    const fake = fakeQueueGitHub(ledgerText(log));
    const core = summaryCore();
    fake.state.beforeUpdate = () => fake.install(ledgerText(dispatchNext(log, { max_claims: 1, max_dispatches: 1 }, { ...dispatcher, run_id: "999" }).log));
    const result = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(result.committed).toBe(false);
    expect(result.state.claims.size).toBe(1);
    expect(core.summary.addRaw).not.toHaveBeenCalled();
  });

  it("recovers an ambiguous committed write without selecting again or rendering a second view", async () => {
    const fake = fakeQueueGitHub(ledgerText(submit([], [work("ambiguous-grant")])));
    fake.state.ambiguousOnce = true;
    const core = summaryCore();
    const result = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(result).toMatchObject({ committed: true, recovered: true });
    await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(core.summary.addRaw).not.toHaveBeenCalled();
    expect(fake.state.updates).toBe(1);
    expect(ledger(fake).claims.size).toBe(1);
  });

  it("does not reinterpret a summary write failure as an ambiguous CAS", async () => {
    const fake = fakeQueueGitHub(ledgerText(submit([], [work("summary-io-failure")])));
    const core = summaryCore();
    core.summary.write.mockRejectedValue(new Error("summary filesystem unavailable"));
    const result = await publishWorkQueueRequest(dispatchOptions(fake, core));
    expect(result).toMatchObject({ committed: true, recovered: false });
    expect(fake.state.updates).toBe(1);
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("Publication and Claim accounting are unchanged"));
    expect(ledger(fake).claims.size).toBe(1);
  });

  it("keeps rejected queue updates out of summaries", async () => {
    const core = summaryCore();
    const fake = fakeQueueGitHub(ledgerText(submit([], [work("seed")])));
    await expect(publishWorkQueueRequest(dispatchOptions(fake, core, dispatcher, { branch: "../queue" }))).rejects.toThrow(/branch_invalid/);
    expect(core.summary.addRaw).not.toHaveBeenCalled();
    expect(fake.state.updates).toBe(0);
  });
});
