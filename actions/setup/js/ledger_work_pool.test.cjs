// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { createReducer, claimId, workId } from "./ledger_work_pool.cjs";
import { replayBuiltin } from "./ledger_builtin.cjs";
import { normalizeLedgerAppends } from "./ledger_transactions.cjs";
import { validateReplayOutput } from "./ledger_replay.cjs";

const config = { type: "work-pool", identity: ["task"] };
const work = { task: "a", title: "Do A" };

function step(reducer, intent, claimant = "run:1") {
  const result = reducer.transition(intent, claimant);
  result.facts.forEach(fact => reducer.apply(fact));
  return result;
}

test("submission is idempotent and conflicting identity is rejected", () => {
  const reducer = createReducer(config);
  assert.equal(step(reducer, { operation: "submit", work }).outcome, "submitted");
  assert.deepEqual(step(reducer, { operation: "submit", work }).facts, []);
  assert.equal(step(reducer, { operation: "submit", work: { task: "a", title: "Other" } }).outcome, "invalid");
  assert.equal(reducer.output().tables.work.rows.length, 1);
});

test("acquire retries reuse one Claim and ownership is enforced", () => {
  const reducer = createReducer(config);
  assert.equal(step(reducer, { operation: "acquire", work }).outcome, "acquired");
  assert.deepEqual(step(reducer, { operation: "acquire", work }).facts, []);
  assert.equal(step(reducer, { operation: "acquire", work: { task: "b" } }).outcome, "invalid");
  assert.equal(step(reducer, { operation: "finish", result: { ok: true } }, "other").outcome, "nothing_acquired");
  assert.equal(step(reducer, { operation: "finish", result: { ok: true } }).outcome, "finished");
  assert.equal(step(reducer, { operation: "finish" }).outcome, "already_finished");
  assert.equal(step(reducer, { operation: "acquire", work }, "other").outcome, "already_done");
  assert.equal(reducer.output().tables.claims.rows.length, 1);
});

test("release, successor generation, cancellation and no resurrection", () => {
  const reducer = createReducer(config);
  step(reducer, { operation: "acquire", work });
  assert.equal(step(reducer, { operation: "abandon", reason: "retry" }).outcome, "abandoned");
  assert.equal(step(reducer, { operation: "abandon" }).outcome, "already_abandoned");
  assert.equal(step(reducer, { operation: "acquire", work }, "run:2").outcome, "acquired");
  assert.deepEqual(
    reducer
      .output()
      .tables.claims.rows.map(claim => claim.generation)
      .sort(),
    [0, 1]
  );
  assert.equal(step(reducer, { operation: "cancel", work }).outcome, "cancelled");
  assert.equal(step(reducer, { operation: "acquire", work }, "run:3").outcome, "cancelled");
  assert.equal(reducer.output().tables.work.rows[0].effective_claim_id, null);
});

test("competing claims converge, stale owners cannot finish or abandon", () => {
  const id = workId(work, config);
  const first = { operation: "claim", work_id: id, claimant: "a", previous_claim_id: null, claim_id: claimId(id, "a", null) };
  const second = { operation: "claim", work_id: id, claimant: "b", previous_claim_id: null, claim_id: claimId(id, "b", null) };
  const reducer = createReducer(config);
  reducer.apply({ operation: "work", work_id: id, work });
  reducer.apply(first);
  reducer.apply(second);
  const loser = reducer.snapshot().claims.get(first.claim_id).effective ? "b" : "a";
  assert.equal(reducer.transition({ operation: "finish" }, loser).outcome, "ownership_lost");
  assert.equal(reducer.transition({ operation: "abandon" }, loser).outcome, "ownership_lost");
  assert.equal(reducer.output().tables.work.rows[0].state, "claimed");
});

test("selection is ordered by Work ID and independent of physical order", () => {
  const reducer = createReducer(config);
  for (const item of [{ task: "z" }, { task: "a" }, { task: "b" }]) step(reducer, { operation: "submit", work: item });
  const expected = [{ task: "z" }, { task: "a" }, { task: "b" }].map(item => workId(item, config)).sort()[0];
  const choice = reducer.transition({ operation: "acquire-next" }, "run");
  assert.equal(choice.outcome, "acquired");
  assert.equal(choice.facts[0].work_id, expected);
  const records = [
    { operation: "work", work_id: workId(work, config), work },
    { operation: "claim", work_id: workId(work, config), claimant: "a", previous_claim_id: null, claim_id: claimId(workId(work, config), "a", null) },
    { operation: "claim", work_id: workId(work, config), claimant: "b", previous_claim_id: null, claim_id: claimId(workId(work, config), "b", null) },
  ];
  const projection = replayBuiltin(
    config,
    records.map(payload => ({ payload }))
  );
  validateReplayOutput(projection);
  for (let iteration = 0; iteration < 100; iteration++) {
    const shuffled = [...records].sort(() => Math.random() - 0.5);
    assert.deepEqual(
      replayBuiltin(
        config,
        shuffled.map(payload => ({ payload }))
      ),
      projection
    );
    assert.deepEqual(
      replayBuiltin(
        config,
        [...shuffled, ...shuffled].map(payload => ({ payload }))
      ),
      projection
    );
  }
});

test("caller-provided structural fields are rejected by trusted normalization", () => {
  const options = { transactionId: "tx", ledgerNames: new Set(["pool"]), ledgers: { pool: config } };
  for (const field of ["work_id", "claim_id", "generation", "previous_claim_id", "claimant"]) {
    assert.throws(() => normalizeLedgerAppends([{ operation: "acquire", work, [field]: "invented" }], options));
  }
  assert.throws(() => normalizeLedgerAppends([{ operation: "acquire", work: { ...work, claim_id: "invented" } }], options));
});
