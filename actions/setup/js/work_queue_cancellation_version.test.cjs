// @ts-check
import { describe, expect, it } from "vitest";
import { canonical } from "./work_queue_codec.cjs";
import { obsoleteWork } from "./work_queue_deployment.cjs";
import { compactTransactions, replayTransactions, validateOperation } from "./work_queue_replay.cjs";
import { queueFixture, REF } from "./work_queue_lifecycle.test_helpers.cjs";

const CONTRACT = "a".repeat(64);
const REASON = "worker_contract_obsolete";

function fixture(granted = false, expiresAt = undefined) {
  return queueFixture({
    count: 3,
    granted,
    configurePolicy: policy => {
      policy.authorization = "aw";
      policy.producers = {};
      const profile = policy.pools.default.profiles.default;
      delete profile.principal;
      profile.logical_contract = CONTRACT;
      policy.pools.default.profiles.other = { ...profile, workflow: ".github/workflows/other.lock.yml" };
    },
    configureWork: (work, index) => {
      if (index === 0 && expiresAt !== undefined) work.expires_at = expiresAt;
      if (index === 1) work.execution_ref = REF;
      if (index === 2) work.worker_profile = "other";
    },
  });
}

function deploy(f, contract = "b".repeat(64), available = true) {
  const worker = f.state.deployments.get("default").get("default");
  f.append(
    "deployment",
    {
      operations: [
        {
          kind: "Deployment",
          pool: "default",
          worker_profile: "default",
          expected_ref: worker.current_ref,
          expected_contract: worker.current_contract,
          profile: { ...worker.revisions[worker.current_ref].profile, ref: "b".repeat(40), logical_contract: contract },
          available,
          reason: "compiler_deployment",
        },
      ],
    },
    f.dispatcher
  );
}

describe("versioned Work cancellation compatibility", () => {
  it.each([false, true])("replays legacy compatible and pinned cancellation history after incompatible deployment=%s", changed => {
    const f = fixture();
    if (changed) deploy(f);
    const operations = [...f.state.works.keys()].map(work_id => ({ kind: "WorkCancellation", work_id, reason: REASON }));
    f.append("cancel_work", { operations });
    const original = canonical(f.transactions);
    let log = f.transactions;
    for (const sha of ["c".repeat(40), "d".repeat(40)]) {
      log = compactTransactions(log, sha, f.administrator, f.at + 1);
      const state = replayTransactions(log);
      for (const operation of operations) {
        expect(state.works.get(operation.work_id).state).toBe("cancelled");
        expect(state.cancellations.get(operation.work_id)).toEqual(operation);
        expect(state.cancellations.get(operation.work_id)).not.toHaveProperty("version");
      }
    }
    expect(canonical(f.transactions)).toBe(original);
  });

  it("checks new retirement, preserves its marker across checkpoints and rejects cross-form terminal rewrites", () => {
    const f = fixture();
    deploy(f);
    const work_id = [...f.state.works.keys()][0];
    const operation = { kind: "WorkCancellation", version: 1, work_id, reason: REASON };
    f.append("cancel_work", { operations: [operation] });
    f.append("cancel_work", { operations: [operation] });
    const checkpoint = compactTransactions(f.transactions, "c".repeat(40), f.administrator, f.at + 1);
    expect(replayTransactions(checkpoint).cancellations.get(work_id)).toEqual(operation);
    expect(() => f.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id, reason: REASON }] })).toThrow(expect.objectContaining({ code: "cancellation_conflict" }));
    const legacy = fixture();
    const legacyId = [...legacy.state.works.keys()][0];
    legacy.append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id: legacyId, reason: REASON }] });
    expect(() => legacy.append("cancel_work", { operations: [{ ...operation, work_id: legacyId }] })).toThrow(expect.objectContaining({ code: "cancellation_conflict" }));
  });

  it.each(["compatible", "pinned", "unavailable", "claimed", "other-profile"])("rejects checked retirement of %s Work atomically", scenario => {
    const f = fixture(scenario === "claimed");
    if (scenario !== "compatible") deploy(f, "b".repeat(64), scenario !== "unavailable");
    const works = [...f.state.works.values()];
    const selected = scenario === "pinned" ? works[1] : scenario === "other-profile" ? works[2] : works[0];
    const original = canonical(f.transactions);
    const operation = { kind: "WorkCancellation", version: 1, work_id: selected.work_id, reason: REASON };
    expect(() => f.append("cancel_work", { operations: [operation] })).toThrow(expect.objectContaining({ code: "deployment_conflict" }));
    expect(canonical(f.transactions)).toBe(original);
  });

  it("preserves all ownership/barrier guards and does not infer obsolescence from missing deployments", () => {
    const f = fixture();
    deploy(f);
    const state = f.state;
    const work = [...state.works.values()][0];
    for (const change of [{ state: "claimed" }, { claim_id: "owned" }, ...["pending", "failed", "verified"].map(barrier => ({ barrier }))]) expect(obsoleteWork(state, { ...work, ...change })).toBe(false);
    state.deployments.clear();
    expect(obsoleteWork(state, work)).toBe(false);
  });

  it("rejects checked retirement when the publication time reaches the Work deadline", () => {
    const f = fixture(false, 2000);
    deploy(f);
    const work_id = [...f.state.works.keys()][0];
    const original = canonical(f.transactions);
    expect(() => f.append("cancel_work", { operations: [{ kind: "WorkCancellation", version: 1, work_id, reason: REASON }] }, f.administrator, "expired-retirement", 2000)).toThrow(expect.objectContaining({ code: "cancellation_conflict" }));
    expect(canonical(f.transactions)).toBe(original);
  });

  it.each(["dispatcher", "worker", "projector"])("does not grant %s new cancellation authority", role => {
    const f = fixture();
    deploy(f);
    const work_id = [...f.state.works.keys()][0];
    const original = canonical(f.transactions);
    const actor = { ...f.dispatcher, role };
    expect(() => f.append("cancel_work", { operations: [{ kind: "WorkCancellation", version: 1, work_id, reason: REASON }] }, actor)).toThrow();
    expect(canonical(f.transactions)).toBe(original);
  });

  it.each([0, 2, null, "1", false])("rejects unsupported explicit cancellation version %s", version => {
    expect(() => validateOperation({ kind: "WorkCancellation", version, work_id: "w", reason: REASON })).toThrow(expect.objectContaining({ code: "unsupported_protocol" }));
  });
});
