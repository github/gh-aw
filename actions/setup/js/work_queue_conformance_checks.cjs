"use strict";

const assert = require("node:assert/strict");
const fixtures = require("./work_queue_conformance_fixtures.json");
const { canonical, identity, parseStrictJSON } = require("./work_queue_codec.cjs");
const { decimal } = require("./work_queue_policy.cjs");
const { appendCommit, buildCommit, checkpointCommit, newRequest, parseTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog, validateOperation } = require("./work_queue_replay.cjs");
const { assignmentForDispatch, claimIdentity, planDispatch } = require("./work_queue_scheduler.cjs");
const { buildFixtures } = require("./work_queue_conformance_fixture_generator.cjs");
const h = require("./work_queue_test_helpers.cjs");
const sharedSelections = require("../../../specs/work-queue/fixtures/selection.json");
const sharedPrefix = require("../../../specs/work-queue/fixtures/canonical-prefix.json");
const sharedReasons = require("../../../specs/work-queue/fixtures/reason-validation.json");
const sharedIdentities = require("../../../specs/work-queue/fixtures/identity-validation.json");
const sharedCanonical = require("../../../specs/work-queue/fixtures/canonical.json");

/** @param {unknown} error */
const errorCode = error => (error && typeof error === "object" && "code" in error ? error.code : undefined);
const profilesFor = names => Object.fromEntries(names.map(name => [name, { workflow: `.github/workflows/${name}.lock.yml` }]));

/** Build and decide one shared selection case exactly as described in selection.json. */
function runSelection(fixture) {
  const byKey = new Map();
  const nodes = fixture.nodes.map(node => {
    const work = h.work(node.key, { priority: node.priority, fairness_key: node.account, worker_profile: node.profile, depends_on: node.after.map(key => ({ kind: "work", work_id: byKey.get(key).work_id })) });
    byKey.set(node.key, work);
    return work;
  });
  let log = h.submit([], nodes);
  if (fixture.open.length) {
    const parameters = { pool: "default", max_claims: 256, max_dispatches: 256, concurrency: 4096, per_account_limit: 0, profiles: profilesFor([...new Set(fixture.nodes.map(node => node.profile))].sort()) };
    const request = newRequest("dispatch_next", h.dispatcher, parameters);
    const state = replayTransactions(log);
    const claims = fixture.open.map(key => ({ kind: "Claim", work_id: byKey.get(key).work_id, ...claimIdentity(request.id, byKey.get(key).work_id) }));
    const commit = buildCommit(state, h.dispatcher, request, claims, 1);
    appendCommit(state, commit);
    log = [...log, commit];
  }
  const state = replayTransactions(log);
  const before = canonical(serializeProjection(state));
  const decision = planDispatch(state, { pool: "default", ...fixture.parameters, profiles: profilesFor(fixture.parameters.profiles) }, "request:selection-fixture");
  assert.equal(canonical(serializeProjection(state)), before, "planning never mutates the projection");
  return { keys: decision.operations.map(operation => state.works.get(operation.work_id).node_key), reason: decision.reason };
}

function minimalWithReason(kind, reason) {
  if (kind === "ClaimCancellation") return { kind, work_id: "w", claim_id: "c", reason };
  if (kind === "WorkCancellation") return { kind, work_id: "w", reason };
  return { kind, dispatch_id: "d", reason };
}

/** Replay every prefix, the projection and the assignment of one shared ledger fixture. */
function checkLedger(ledger) {
  assert.equal(serializeTransactionLog(ledger.commits), ledger.canonical);
  assert.equal(canonical(parseTransactionLog(ledger.canonical)), canonical(ledger.commits));
  for (let index = 0; index < ledger.commits.length; index++) {
    const expected = ledger.prefixes[index];
    const state = replayTransactions(ledger.commits.slice(0, index + 1));
    assert.equal(state.tip, expected.tip);
    assert.equal(state.commits, expected.commits);
    assert.deepEqual(state.stats, expected.stats);
    for (const work of state.works.values()) assert.deepEqual({ state: work.state, attempts: work.attempts }, expected.works[work.node_key]);
    for (const dispatch of state.dispatches.values()) assert.equal(dispatch.state, expected.dispatches[dispatch.dispatch_id]);
    const decision = planDispatch(state, sharedPrefix.next_parameters, sharedPrefix.next_request_id);
    assert.deepEqual({ keys: decision.operations.map(operation => state.works.get(operation.work_id).node_key), reason: decision.reason }, expected.next);
  }
  const state = replayTransactions(ledger.commits);
  assert.equal(canonical(serializeProjection(state)), canonical(ledger.projection));
  assert.equal(canonical(assignmentForDispatch(state, ledger.assignment.dispatch_id)), canonical(ledger.assignment));
}

/**
 * The checkpoint restores to its own projection and carries the folded records unchanged,
 * minus exactly the named Work no later commit reads, with its Claims, Dispatches, creator
 * and the gate Observations only it referenced.
 */
function checkCheckpoint(ledger, { dropped, observations }) {
  const restored = ledger.checkpoint.projection;
  assert.equal(canonical(serializeProjection(replayTransactions([ledger.checkpoint.commit]))), canonical(restored));
  const folded = serializeProjection(replayTransactions(ledger.commits));
  const gone = new Set(Object.values(folded.works).flatMap(work => (dropped.includes(work.node_key) ? [work.work_id] : [])));
  assert.equal(gone.size, dropped.length);
  const kept = (records, workOf) => Object.fromEntries(Object.entries(records).filter(([id, record]) => !gone.has(workOf(record, id))));
  assert.equal(restored.repository, folded.repository);
  assert.equal(canonical(restored.works), canonical(kept(folded.works, work => work.work_id)));
  assert.equal(canonical(restored.claims), canonical(kept(folded.claims, claim => claim.work_id)));
  assert.equal(canonical(restored.dispatches), canonical(kept(folded.dispatches, dispatch => folded.claims[dispatch.claims[0]].work_id)));
  assert.equal(canonical(restored.work_creators), canonical(kept(folded.work_creators, (_, id) => id)));
  assert.equal(Object.keys(restored.observations).length, observations);
  for (const [key, observation] of Object.entries(restored.observations)) assert.equal(canonical(observation), canonical(folded.observations[key]));
  // Below the window bound the checkpoint keeps every request of the folded log.
  assert.deepEqual(ledger.checkpoint.commit.operations[0].state.requests, folded.requests);
}

function registerTests({ describe, it }) {
  describe("shared v4 queue conformance fixtures", () => {
    it("regenerates the shared lifecycle fixture byte for byte", () => assert.equal(canonical(buildFixtures()), canonical(sharedPrefix)));
    it("replays every prefix of the shared lifecycle ledger", () => checkLedger(sharedPrefix));
    it("restores the shared checkpoint to the pruned projection", () => checkCheckpoint(sharedPrefix, { dropped: ["c"], observations: 0 }));
    for (const rejection of sharedPrefix.rejections)
      it(`shared rejection: ${rejection.name}`, () =>
        assert.throws(
          () => replayTransactions([...sharedPrefix.commits, ...rejection.extra, rejection.commit]),
          error => errorCode(error) === rejection.code
        ));
    const continuation = sharedPrefix.continuation;
    it("replays every prefix of the shared continuation ledger", () => checkLedger(continuation));
    it("restores the shared continuation checkpoint to the pruned projection", () => checkCheckpoint(continuation, { dropped: ["p"], observations: 1 }));
    for (const rejection of continuation.rejections)
      it(`shared continuation rejection: ${rejection.name}`, () =>
        assert.throws(
          () => replayTransactions([...continuation.commits.slice(0, rejection.base), ...rejection.extra, rejection.commit]),
          error => errorCode(error) === rejection.code
        ));
    it("replays migrate genesis and rejects a late migrate", () => {
      const migration = sharedPrefix.migration;
      assert.equal(serializeTransactionLog(migration.commits), migration.canonical);
      assert.equal(canonical(serializeProjection(replayTransactions(migration.commits))), canonical(migration.projection));
      assert.throws(
        () => replayTransactions([...migration.commits, migration.late_migrate.commit]),
        error => errorCode(error) === migration.late_migrate.code
      );
    });
    for (const fixture of sharedSelections.cases) it(`shared selection: ${fixture.name}`, () => assert.deepEqual(runSelection(fixture), { keys: fixture.expected, reason: fixture.reason }));
    for (const fixture of sharedReasons.cases)
      it(`shared reason: ${fixture.name}`, () => {
        const operation = minimalWithReason(fixture.kind, fixture.reason);
        if (fixture.valid) assert.equal(validateOperation(operation).reason, fixture.reason);
        else
          assert.throws(
            () => validateOperation(operation),
            error => errorCode(error) === "reason_invalid"
          );
      });
    for (const fixture of sharedCanonical.valid) it(`shared canonical: ${fixture.name}`, () => assert.equal(canonical(parseStrictJSON(fixture.input)), fixture.expected));
    for (const [index, input] of sharedCanonical.invalid.entries()) it(`shared canonical: reject malformed representation ${index + 1}`, () => assert.throws(() => parseStrictJSON(input)));
    for (const fixture of sharedIdentities.identity)
      it(`shared identity: ${fixture.name}`, () => {
        const value = fixture.text.repeat(fixture.repeat);
        assert.equal(Buffer.byteLength(value, "utf8"), fixture.expected_utf8_bytes);
        if (fixture.valid) assert.equal(identity(value), value);
        else assert.throws(() => identity(value), /ledger_invalid/);
      });
    for (const fixture of sharedIdentities.decimal)
      it(`shared decimal: ${fixture.name}`, () => {
        const value = "9".repeat(fixture.digits);
        if (fixture.valid) assert.equal(decimal(value, "native ID"), value);
        else assert.throws(() => decimal(value, "native ID"), /ledger_invalid/);
      });
    for (const fixture of fixtures.codec)
      it(fixture.name, () => {
        if (fixture.invalid) assert.throws(() => parseStrictJSON(fixture.input));
        else assert.equal(canonical(parseStrictJSON(fixture.input)), fixture.canonical);
      });
  });
}

/**
 * Differential harness for the Go engine: stdin `{transactions, parameters?, request_id?, checkpoint?}`,
 * stdout canonical `{state, canonical, decision?, checkpoint?}` or `{error}` when replay rejects;
 * `checkpoint` names the actor that builds the replayed projection's Checkpoint commit.
 */
function replayCommand(input) {
  try {
    const state = replayTransactions(input.transactions);
    const output = { state: serializeProjection(state), canonical: serializeTransactionLog(input.transactions) };
    if (input.parameters) output.decision = planDispatch(state, input.parameters, input.request_id);
    if (input.checkpoint) output.checkpoint = checkpointCommit(state, input.checkpoint, 1);
    return output;
  } catch (error) {
    const code = errorCode(error);
    if (typeof code !== "string") throw error;
    return { error: code };
  }
}

if (require.main === module) {
  if (process.argv.includes("--replay")) {
    const input = parseStrictJSON(require("node:fs").readFileSync(0, "utf8"), { maxBytes: 80 * 1024 * 1024 });
    process.stdout.write(canonical(replayCommand(input)) + "\n");
  } else registerTests(require("node:test"));
}
module.exports = { registerTests, replayCommand, runSelection };
