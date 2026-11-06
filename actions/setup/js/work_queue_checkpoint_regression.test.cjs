import { describe, it } from "vitest";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { deflateRawSync, inflateRawSync } from "node:zlib";

const require = createRequire(import.meta.url);
const { canonical } = require("./work_queue_codec.cjs");
const { administrator, genesis, operationCommit, submission } = require("./work_queue_test_helpers.cjs");
const { compactTransactions, prepareCheckpoint, reconstructCheckpoint, replayCheckpointSource, replayTransactions, serializeProjection, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { legacyCheckpoint, rewriteCheckpoint } = require("./work_queue_checkpoint_regression_helpers.cjs");
const { acceptedSubmissionParameters } = require("./work_queue_dispatch.cjs");

function expiringLog() {
  const log = [genesis()];
  log.push(submission(log, ["old", "fresh"], { transform: (work, index) => (index === 0 ? { ...work, expires_at: 50 } : work) }));
  return log;
}

describe("checkpoint and expiration internal-state regressions", () => {
  it("rejects noncanonical compressed receipts even when native JSON parsing produces the original snapshot", () => {
    const log = [genesis()];
    log.push(submission(log, ["accepted"]));
    const root = compactTransactions(log, "a".repeat(40), administrator, 100)[0];
    const text = inflateRawSync(Buffer.from(root.operations[0].state.requests_compressed, "base64")).toString("utf8");
    for (const changed of [
      text.replace(/"at":([0-9]+)/, '"at":$1,"at":$1'),
      text.replace(/"at":([0-9]+)/, (_, at) => `"at":${at},"\\u0061t":${at}`),
      text.replace(/"at":([0-9]+)/, '"at":$1.0'),
      text.replace(/"at":([0-9]+)/, '"at":$1e0'),
      text.replace('"actor":', '"\\u0061ctor":'),
      ` ${text}`,
    ]) {
      assert.notEqual(changed, text);
      assert.equal(canonical(JSON.parse(changed)), text);
      const damaged = structuredClone(root);
      damaged.operations[0].state.requests_compressed = deflateRawSync(Buffer.from(changed, "utf8")).toString("base64");
      assert.throws(() => replayTransactions([damaged]), /checkpoint_invalid/);
    }
  });

  it("recovers checkpoints written before full Policy receipt history and retains current-epoch submit recovery", () => {
    const log = [genesis()];
    log.push(submission(log, ["accepted"]));
    const current = compactTransactions(log, "a".repeat(40), administrator, 100);
    const legacy = rewriteCheckpoint(current, snapshot => {
      for (const receipt of snapshot.requests) receipt.events = receipt.events.map(event => (["Policy", "PoolInstall"].includes(event.kind) ? { kind: event.kind } : event));
    });
    assert.notEqual(legacy[0].operations[0].state_sha256, current[0].operations[0].state_sha256);
    const recovered = reconstructCheckpoint(log, "a".repeat(40), administrator, 100, legacy[0].operations[0].state_sha256);
    assert.equal(canonical(recovered), canonical(legacy));
    assert.equal(canonical(serializeProjection(replayCheckpointSource(serializeTransactionLog(log), "a".repeat(40), legacy[0]))), canonical(serializeProjection(replayTransactions(log))));
    const state = replayTransactions(recovered);
    const prior = state.requests.get(log[1].request.id);
    const original = { nodes: log[1].request.parameters.nodes.map(({ kind, batch_trust_domain, enqueued, ...node }) => node) };
    const context = { ...prior.actor, authenticated: true, roles: [prior.actor.role], created_at: log[1].at };
    assert.deepEqual(acceptedSubmissionParameters(state, context, original, prior), prior.request.parameters);
  });

  it("verifies fresh checkpoint source replay without trusting altered history or state digests", () => {
    const log = [genesis()];
    log.push(submission(log, ["accepted"]));
    const prior = "a".repeat(40);
    const contents = serializeTransactionLog(log);
    const root = compactTransactions(log, prior, administrator, 100)[0];
    const replayed = replayCheckpointSource(contents, prior, root);
    assert.equal(canonical(serializeProjection(replayed)), canonical(serializeProjection(replayTransactions(log))));
    for (const field of ["prior_git_sha", "prior_tip", "history_sha256", "state_sha256"]) {
      const changed = structuredClone(root);
      changed.operations[0][field] = field === "prior_tip" ? "different-tip" : "b".repeat(field === "prior_git_sha" ? 40 : 64);
      assert.throws(() => replayCheckpointSource(contents, prior, changed), /checkpoint_invalid/);
    }
    for (const actor of [
      { ...administrator, role: "producer" },
      { ...administrator, repository: "other/repo" },
    ]) {
      assert.throws(() => replayCheckpointSource(contents, prior, { ...root, actor }), /checkpoint_invalid/);
    }
    const malformed = contents.replace('"version":3', '"version":3,"version":3');
    assert.throws(() => replayCheckpointSource(malformed, prior, root), /duplicate_key/);
  });

  it("refreshes every public view after an isolated as-of expiration pass", () => {
    const log = expiringLog();
    const original = canonical(log);
    const historical = replayTransactions(log);
    const before = canonical(serializeProjection(historical));
    const viewed = replayTransactions(log, 50);
    const [old, fresh] = [...viewed.works.values()];
    assert.equal(old.state, "cancelled");
    assert.deepEqual(viewed.work, { [old.work_id]: "cancelled", [fresh.work_id]: "available" });
    assert.deepEqual(viewed.available, [fresh.work_id]);
    assert.deepEqual(viewed.winner, historical.winner);
    assert.deepEqual(viewed.claim, historical.claim);
    assert.deepEqual(viewed.stats, { ...historical.stats, available: 1, cancelled: 1 });
    assert.equal(canonical(serializeProjection(historical)), before);
    assert.equal(canonical(log), original);
    assert.deepEqual(replayTransactions(log).available, [old.work_id, fresh.work_id]);
  });

  it("retains accepted explicit expiration cancellations in the checkpoint hash", () => {
    const log = expiringLog();
    const work = log[1].operations[0];
    const operation = { kind: "WorkCancellation", work_id: work.work_id, reason: "work_expired" };
    log.push(operationCommit(log, "cancel-expired", "cancel_work", [operation], administrator, 60));
    const state = replayTransactions(log);
    assert.deepEqual(state.cancellations.get(work.work_id), operation);
    const checkpoint = compactTransactions(log, "a".repeat(40), administrator, 60);
    assert.deepEqual(checkpoint[0].operations[0].state.cancellations[work.work_id], operation);
    assert.deepEqual(replayTransactions(checkpoint).cancellations.get(work.work_id), operation);
  });

  it("defaults both prepareCheckpoint input forms to the committed watermark", () => {
    const log = [genesis()];
    log.push(operationCommit(log, "tick", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "clock" }], administrator, 60));
    log.push(submission(log, ["backdated"], { at: 2 }));
    for (const source of [log, replayTransactions(log)]) {
      const checkpoint = prepareCheckpoint(source, "a".repeat(40));
      assert.equal(checkpoint[0].at, 60);
      assert.equal(checkpoint[0].operations[0].state.last_at, 60);
    }
    const legacy = legacyCheckpoint(log, "a".repeat(40), administrator, 2);
    for (const source of [legacy, replayTransactions(legacy)]) {
      const checkpoint = prepareCheckpoint(source, "b".repeat(40));
      assert.equal(checkpoint[0].at, 60);
      assert.equal(checkpoint[0].operations[0].state.last_at, 60);
    }
    const future = structuredClone(log);
    future[1].at = Number.MAX_SAFE_INTEGER;
    const checkpoint = compactTransactions(future, "b".repeat(40), administrator);
    assert.equal(checkpoint[0].at, Number.MAX_SAFE_INTEGER);
    assert.throws(() => compactTransactions(log, "a".repeat(40), administrator, null), /checkpoint_invalid/);
    assert.throws(() => prepareCheckpoint(log, "a".repeat(40), administrator, 2), /checkpoint_invalid/);
  });
});
