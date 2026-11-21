import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { LEDGER_BYTES } = require("./work_queue_limits.cjs");
const { parseTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");
const { CAS_ATTEMPTS, ledgerProtocol, publishWorkQueueCheckpoint, publishWorkQueueRequest, readWorkQueueLog, writeQueueContent } = require("./work_queue_store.cjs");

const target = fake => ({ githubClient: fake.githubClient, owner: "owner", repo: "repo" });
const submitA = { kind: "submit", parameters: { work_ids: [h.work("a").work_id] } };

describe("ledger reads", () => {
  it("treats an absent branch or empty log as an empty v4 queue", async () => {
    for (const fake of [h.fakeQueueGitHub(), h.fakeQueueGitHub("")]) {
      const current = await readWorkQueueLog(target(fake));
      expect(current).toMatchObject({ contents: "", transactions: [] });
      expect(current.state.works.size).toBe(0);
    }
  });

  it("refuses a v3 head unless the converter asks for it", async () => {
    const v3 = '{"actor":{},"id":"q_1","version":3}\n';
    expect(ledgerProtocol(v3)).toBe(3);
    expect(ledgerProtocol(h.ledgerText(h.submit([], [h.work("a")])))).toBe(4);
    const fake = h.fakeQueueGitHub(v3);
    await expect(readWorkQueueLog(target(fake))).rejects.toMatchObject({ code: "unsupported_protocol" });
    expect(await readWorkQueueLog({ ...target(fake), allowLegacy: true })).toMatchObject({ protocol: 3, contents: v3, state: null });
  });

  it("replays the stored log", async () => {
    const log = h.submit([], [h.work("a")]);
    const current = await readWorkQueueLog(target(h.fakeQueueGitHub(h.ledgerText(log))));
    expect(current.state.tip).toBe(log[0].id);
    expect(current.transactions).toEqual(log);
  });
});

describe("compare-and-swap publication", () => {
  it("creates the branch with a genesis commit and appends exact bytes afterwards", async () => {
    const fake = h.fakeQueueGitHub();
    const created = await publishWorkQueueRequest({ ...target(fake), actor: h.producer, request: submitA, operationsFor: () => [h.work("a")] });
    expect(created).toMatchObject({ committed: true, recovered: false });
    expect(created.commit.previous).toBeNull();
    const before = fake.contents();
    const appended = await publishWorkQueueRequest({ ...target(fake), actor: h.producer, request: { kind: "submit", parameters: { work_ids: [h.work("b").work_id] } }, operationsFor: () => [h.work("b")] });
    expect(fake.contents().startsWith(before)).toBe(true);
    expect(appended.commit.previous).toBe(created.commit.id);
    expect(parseTransactionLog(fake.contents())).toHaveLength(2);
  });

  it("re-plans against the re-read head after losing a race, without sleeping", async () => {
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a"), h.work("b")])));
    fake.state.beforeUpdate = () => {
      const log = parseTransactionLog(fake.contents());
      const competing = h.dispatchNext(log, { concurrency: 1 }, { ...h.dispatcher, run_id: "999" });
      fake.install(h.ledgerText(competing.log));
    };
    const plans = [];
    const parameters = h.dispatchParameters({ concurrency: 1 });
    const result = await publishWorkQueueRequest({
      ...target(fake),
      actor: h.dispatcher,
      request: { kind: "dispatch_next", parameters },
      operationsFor: (state, request) => {
        const decision = planDispatch(state, parameters, request.id);
        plans.push(decision.reason);
        return decision.operations;
      },
    });
    expect(plans).toEqual(["capacity_blocked", "capacity_blocked"]);
    expect(result).toMatchObject({ committed: false, commit: null });
    expect(replayTransactions(parseTransactionLog(fake.contents())).stats.claimed).toBe(1);
  });

  it("reports a lost acknowledgement as the committed request", async () => {
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a")])));
    fake.state.ambiguousOnce = true;
    let plans = 0;
    const result = await publishWorkQueueRequest({
      ...target(fake),
      actor: h.dispatcher,
      request: { kind: "dispatch_next", parameters: h.dispatchParameters() },
      operationsFor: (state, request) => {
        plans++;
        return planDispatch(state, h.dispatchParameters(), request.id).operations;
      },
    });
    expect(result).toMatchObject({ committed: true, recovered: true });
    expect(plans).toBe(1);
    expect(parseTransactionLog(fake.contents())).toHaveLength(2);
  });

  it("stops after a bounded number of lost races", async () => {
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a")])));
    let competitor = 0;
    const raceAgain = () => {
      const log = parseTransactionLog(fake.contents());
      fake.install(h.ledgerText(h.submit(log, [h.work(`r${competitor}`)], { ...h.producer, run_id: String(500 + competitor++) })));
      fake.state.beforeUpdate = raceAgain;
    };
    fake.state.beforeUpdate = raceAgain;
    await expect(publishWorkQueueRequest({ ...target(fake), actor: h.producer, request: { kind: "submit", parameters: { work_ids: [h.work("z").work_id] } }, operationsFor: () => [h.work("z")] })).rejects.toMatchObject({
      code: "cas_exhausted",
    });
    expect(competitor).toBe(CAS_ATTEMPTS);
  });

  it("validates the candidate before writing and refuses foreign actors", async () => {
    const fake = h.fakeQueueGitHub(h.ledgerText(h.submit([], [h.work("a")])));
    await expect(
      publishWorkQueueRequest({ ...target(fake), actor: h.workerActor("7"), request: { kind: "bind", parameters: { dispatch_id: "d_x" } }, operationsFor: () => [{ kind: "Bound", dispatch_id: "d_x", run_id: "7", run_attempt: 1 }] })
    ).rejects.toMatchObject({
      code: "dispatch_missing",
    });
    await expect(publishWorkQueueRequest({ ...target(fake), actor: { ...h.producer, repository: "other/repo" }, request: submitA, operationsFor: () => [h.work("a")] })).rejects.toMatchObject({ code: "actor_unauthorized" });
    expect(fake.state.writes).toBe(0);
  });

  it("refuses to grow the ledger past its byte bound", async () => {
    const fake = h.fakeQueueGitHub();
    await expect(writeQueueContent({ ...target(fake), branch: "work-queue", parent: null, treeSha: null, content: "x".repeat(LEDGER_BYTES + 1), message: "too big" })).rejects.toMatchObject({ code: "ledger_limit" });
  });
});

describe("checkpoints", () => {
  it("replaces the log with one Checkpoint line whose Git parent is the previous head", async () => {
    const planned = h.dispatchNext(h.submit([], [h.work("a")]));
    const dispatch = planned.decision.dispatches[0];
    const log = h.finish(h.bind(planned.log, dispatch, h.workerActor("7")), dispatch, h.workerActor("7"));
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const head = fake.refs.get("work-queue");
    expect(await publishWorkQueueCheckpoint({ ...target(fake), actor: h.reconciler, minimumLines: log.length })).toMatchObject({ checkpointed: false });
    const result = await publishWorkQueueCheckpoint({ ...target(fake), actor: h.reconciler, minimumLines: 1 });
    expect(result.checkpointed).toBe(true);
    expect(fake.commits.get(fake.refs.get("work-queue")).parents).toEqual([head]);
    const lines = fake.contents().trimEnd().split("\n");
    expect(lines).toHaveLength(1);
    const restored = (await readWorkQueueLog(target(fake))).state;
    expect(restored.works.get(h.work("a").work_id).state).toBe("completed");
    expect(await publishWorkQueueCheckpoint({ ...target(fake), actor: h.reconciler, minimumLines: 0 })).toMatchObject({ checkpointed: false });
  });
});
