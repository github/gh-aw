// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { compactTransactions, deriveWorkId, parseTransactionLog, replayTransactions, serializeTransactions, validateTransaction } from "./dispatch_work_coordinator.cjs";

const work = { title: "Fix issue", payload: { priority: 2 } };
const workId = deriveWorkId(work);
const workTx = { type: "Work", work_id: workId, work };
const claim = (claimId, runId = "100") => ({ type: "Claim", claim_id: claimId, work_id: workId, run_id: runId, workflow_id: "worker.yml" });

test("empty and canonical work replay", () => {
  assert.deepEqual(replayTransactions([]).counts, {
    available: 0,
    claimed: 0,
    completed: 0,
    cancelled: 0,
    outstanding: 0,
    active_claims: 0,
    terminal: 0,
  });
  const projection = replayTransactions([workTx]);
  assert.equal(projection.works[0].state, "available");
  assert.equal(projection.counts.outstanding, 1);
  assert.equal(deriveWorkId({ payload: { priority: 2 }, title: "Fix issue" }), workId);
  assert.notEqual(deriveWorkId({ ...work, title: "Changed" }), workId);
});

test("claim arbitration is deterministic and independent of record order", () => {
  const transactions = [workTx, claim("claim-b"), claim("claim-a", "101")];
  const first = replayTransactions(transactions);
  const second = replayTransactions(transactions.slice().reverse());
  assert.deepEqual(first, second);
  assert.equal(first.works[0].effective_claim_id, "claim-a");
  assert.deepEqual(
    first.works[0].claims.map(entry => [entry.claim_id, entry.state]),
    [
      ["claim-a", "effective"],
      ["claim-b", "superseded"],
    ]
  );
});

test("claim cancellation advances arbitration to the next stable claim", () => {
  const state = replayTransactions([workTx, claim("claim-a"), claim("claim-b"), { type: "ClaimCancellation", claim_id: "claim-a" }]);
  assert.equal(state.works[0].effective_claim_id, "claim-b");
  assert.equal(state.works[0].claims[0].state, "cancelled");
  assert.equal(state.counts.claimed, 1);
});

test("completion is terminal and only an effective claim may complete", () => {
  const completion = { type: "Completion", claim_id: "claim-a", outcome: { result: "done" } };
  const completed = replayTransactions([workTx, claim("claim-a"), completion]);
  assert.equal(completed.works[0].state, "completed");
  assert.deepEqual(completed.works[0].completion, { result: "done" });
  assert.equal(completed.counts.terminal, 1);
  assert.throws(() => replayTransactions([workTx, claim("claim-a"), claim("claim-b"), { type: "Completion", claim_id: "claim-b" }]), /non-effective/);
  assert.throws(() => replayTransactions([workTx, claim("claim-a"), { type: "Completion", claim_id: "claim-a" }, { type: "WorkCancellation", work_id: workId }]), /both completed and cancelled/);
});

test("work cancellation is terminal", () => {
  const state = replayTransactions([workTx, claim("claim-a"), { type: "WorkCancellation", work_id: workId }]);
  assert.equal(state.works[0].state, "cancelled");
  assert.equal(state.counts.terminal, 1);
  assert.equal(state.counts.active_claims, 0);
});

test("transactions are validated, references are checked, and exact duplicates are idempotent", () => {
  assert.equal(replayTransactions([workTx, workTx]).works.length, 1);
  assert.throws(() => replayTransactions([claim("orphan")]), /unknown Work/);
  assert.throws(() => replayTransactions([{ type: "ClaimCancellation", claim_id: "missing" }]), /unknown Claim/);
  assert.throws(() => validateTransaction({ type: "Work", work_id: workId, work, extra: true }), /fields/);
  assert.throws(() => validateTransaction({ type: "Completion", claim_id: "claim-a", outcome: { nested: { claim_id: "claim-z" } } }), /authority fields/);
});

test("JSONL parsing and serialization use a stable canonical transaction set", () => {
  const input = [claim("claim-b"), workTx, claim("claim-a")];
  const serialized = serializeTransactions(input);
  assert.equal(serialized, serializeTransactions(input.slice().reverse()));
  assert.deepEqual(parseTransactionLog(serialized), compactTransactions(input));
  assert.deepEqual(replayTransactions(parseTransactionLog(serialized)), replayTransactions(input));
  assert.throws(() => parseTransactionLog('{"type":"unknown"}\n'), /fields/);
  assert.throws(() => parseTransactionLog('{"type":"Work"\n'), /malformed JSON/);
  assert.throws(() => parseTransactionLog("{}\n\n"), /empty record/);
});

test("compaction removes only redundant identical transactions and preserves replay", () => {
  const source = [workTx, claim("claim-a"), claim("claim-a"), { type: "ClaimCancellation", claim_id: "claim-a" }];
  const compacted = compactTransactions(source);
  assert.equal(compacted.length, 3);
  assert.deepEqual(replayTransactions(compacted), replayTransactions(source));
});
