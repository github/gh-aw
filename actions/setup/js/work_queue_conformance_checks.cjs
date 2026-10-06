"use strict";

const assert = require("node:assert/strict");
const fixtures = require("./work_queue_conformance_fixtures.json");
const { canonical, identity, parseStrictJSON, validateReason } = require("./work_queue_codec.cjs");
const { decimal, defaultPolicy } = require("./work_queue_policy.cjs");
const { genesis, grant, submission } = require("./work_queue_test_helpers.cjs");
const { parseTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog, validateClaimAuthority } = require("./work_queue_replay.cjs");
const { assignmentOnly, diagnostics, planDispatch, planNext } = require("./work_queue_scheduler.cjs");
const sharedSelections = require("../../../specs/work-queue/fixtures/selection.json");
const sharedPrefixes = require("../../../specs/work-queue/fixtures/canonical-prefix.json");
const sharedReasons = require("../../../specs/work-queue/fixtures/reason-validation.json");
const sharedIdentities = require("../../../specs/work-queue/fixtures/identity-validation.json");
const sharedCanonical = require("../../../specs/work-queue/fixtures/canonical.json");

function runFixture(fixture) {
  const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
  policy.mode = fixture.mode;
  policy.accounting_weights = fixture.accounting_weights;
  policy.producers["1001"].fairness_keys = Object.keys(fixture.accounting_weights);
  policy.pools.default.logical_limit = 256;
  policy.pools.default.native_limit = 256;
  const log = [genesis(policy)];
  log.push(
    submission(
      log,
      fixture.works.map(work => work.name),
      { transform: (node, index) => ({ ...node, priority: fixture.works[index].priority, fairness_key: fixture.works[index].fairness_key, enqueued: fixture.works[index].enqueued }) }
    )
  );
  const decision = grant(log, { max_claims: fixture.expected_names.length, max_dispatches: 256 });
  const state = replayTransactions([...log, decision.commit]);
  const names = decision.operations.map(operation => state.works.get(operation.work_id).node_key);
  return { names, decision, state: serializeProjection(state) };
}

function registerTests({ describe, it }) {
  describe("independent shared queue conformance fixtures", () => {
    for (const fixture of sharedCanonical.valid) it(`shared Go/JS canonical: ${fixture.name}`, () => assert.equal(canonical(parseStrictJSON(fixture.input)), fixture.expected));
    for (const [index, input] of sharedCanonical.invalid.entries()) it(`shared Go/JS canonical: reject malformed representation ${index + 1}`, () => assert.throws(() => parseStrictJSON(input)));
    for (const fixture of sharedReasons.cases)
      it(`shared Go/JS reason: ${fixture.name}`, () => {
        if (fixture.valid) assert.equal(validateReason(fixture.reason), fixture.reason);
        else assert.throws(() => validateReason(fixture.reason), /reason_invalid/);
      });
    for (const fixture of sharedIdentities.identity)
      it(`shared Go/JS identity: ${fixture.name}`, () => {
        const value = fixture.text.repeat(fixture.repeat);
        assert.equal(Buffer.byteLength(value, "utf8"), fixture.expected_utf8_bytes);
        if (fixture.valid) assert.equal(identity(value), value);
        else assert.throws(() => identity(value), /ledger_invalid/);
      });
    for (const fixture of sharedIdentities.decimal)
      it(`shared Go/JS decimal: ${fixture.name}`, () => {
        const value = "9".repeat(fixture.digits);
        if (fixture.valid) assert.equal(decimal(value, "native ID"), value);
        else assert.throws(() => decimal(value, "native ID"), /ledger_invalid/);
      });
    for (const fixture of fixtures.selection) it(fixture.name, () => assert.deepEqual(runFixture(fixture).names, fixture.expected_names));
    for (const fixture of fixtures.codec)
      it(fixture.name, () => {
        if (fixture.invalid) assert.throws(() => parseStrictJSON(fixture.input));
        else assert.equal(canonical(parseStrictJSON(fixture.input)), fixture.canonical);
      });
    for (const fixture of sharedSelections)
      it(`shared Go/JS: ${fixture.name}`, () => {
        const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
        policy.mode = fixture.mode;
        policy.class_weights = fixture.class_weights;
        policy.accounting_weights = fixture.accounting_weights;
        policy.producers["1001"].fairness_keys = Object.keys(fixture.accounting_weights);
        const profile = { ...policy.pools.default.profiles.default, max_claims: fixture.profile_max, share_keys: fixture.share_keys };
        policy.pools.default.profiles = { default: profile, x: profile, y: profile };
        const log = [genesis(policy)];
        log.push(
          submission(
            log,
            fixture.nodes.map(node => node.key),
            {
              transform: (node, index) => ({ ...node, priority: fixture.nodes[index].priority, fairness_key: fixture.nodes[index].account, worker_profile: fixture.nodes[index].profile, enqueued: fixture.nodes[index].enqueued }),
            }
          )
        );
        const state = replayTransactions(log);
        const before = canonical(serializeProjection(state));
        const decision = planDispatch(state, { pool: "default", max_claims: fixture.max_claims, max_dispatches: fixture.max_dispatches, max_bytes: 48 << 10 }, { requestId: "fixture-grant", commitId: "fixture-commit", at: 3000 });
        assert.deepEqual(
          decision.operations.map(operation => state.works.get(operation.work_id).node_key),
          fixture.expected
        );
        assert.equal(decision.reason, fixture.reason);
        assert.equal(canonical(serializeProjection(state)), before);
      });
    it("shared Go/JS: independently generated canonical wire, all twelve operations and every causal prefix", () => {
      assert.equal(serializeTransactionLog(sharedPrefixes.commits), sharedPrefixes.canonical);
      assert.equal(canonical(parseTransactionLog(sharedPrefixes.canonical)), canonical(sharedPrefixes.commits));
      assert.equal(new Set(sharedPrefixes.commits.flatMap(commit => commit.operations.map(operation => operation.kind))).size, 12);
      for (let index = 0; index < sharedPrefixes.prefixes.length; index++) {
        const expected = sharedPrefixes.prefixes[index];
        const state = replayTransactions(sharedPrefixes.commits.slice(0, index + 1));
        assert.equal(state.tip, expected.tip);
        assert.equal(state.policy_epoch, expected.policy_epoch);
        assert.equal(state.stats.claims, expected.claims);
        assert.equal(state.stats.dispatches, expected.native_reservations);
        for (const work of state.works.values()) {
          assert.equal(work.state, expected.works[work.node_key].state);
          assert.equal(work.barrier, expected.works[work.node_key].barrier);
        }
        const next = planNext(state, "default", sharedPrefixes.commits[index].at);
        assert.equal(next.work_id ? state.works.get(next.work_id).node_key : "", expected.next_node);
        assert.equal(next.reason, expected.reason);
        const clock = state.clocks.get("default");
        const debt = clock ? diagnostics(clock) : { classes: { v: "0" }, keys: {} };
        assert.equal(debt.classes.v, expected.class_v);
        assert.equal(debt.keys[3]?.v || "0", expected.key_v);
      }
      const granted = replayTransactions(sharedPrefixes.commits.slice(0, 4));
      assert.deepEqual(assignmentOnly(granted.dispatches.get(sharedPrefixes.assignment.dispatch_id)), sharedPrefixes.assignment);
      const permuted = [...sharedPrefixes.commits].reverse().concat(sharedPrefixes.commits[3]);
      assert.equal(serializeTransactionLog(permuted), sharedPrefixes.canonical);
      assert.deepEqual(serializeProjection(replayTransactions(permuted)), serializeProjection(replayTransactions(sharedPrefixes.commits)));
    });
    it("rejects new Claim authority from a drained and retired Policy epoch", () => {
      const state = replayTransactions(sharedPrefixes.commits);
      const dispatch = state.dispatches.get(sharedPrefixes.assignment.dispatch_id);
      assert.notEqual(dispatch.policy_epoch, state.policy_epoch);
      const member = dispatch.claims[0];
      const context = { ...dispatch.run, authenticated: true, roles: ["worker"], role: "worker", dispatch_id: dispatch.dispatch_id, claim_handle: member.handle };
      assert.throws(() => validateClaimAuthority(state, member.claim_id, context, { requireCompletion: true }), /retired Policy epoch/);
    });
    it("retains exact native projection provenance and stable-ID observation budgets", () => {
      const view = serializeProjection(replayTransactions(sharedPrefixes.commits));
      assert.equal(view.repository, "owner/repo");
      const dispatch = view.dispatches[sharedPrefixes.assignment.dispatch_id];
      assert.equal(dispatch.lifecycle_writes, 2);
      assert.deepEqual(dispatch.profile, sharedPrefixes.commits[0].operations[0].policy.pools.default.profiles.default);
      assert.deepEqual(
        sharedPrefixes.assignment.claims.map(member => view.claims[member.claim_id].terminal_commit_id),
        ["q6", "q8", "q9"]
      );
      const observed = serializeProjection(replayTransactions(sharedPrefixes.commits.slice(0, 3)));
      assert.deepEqual(observed.observation_writes, {
        '{"condition":"completed","host":"github.com","kind":"issue","repository_id":"1","resource_id":"9007199254740993"}': 1,
      });
    });
  });
}

if (require.main === module) {
  if (process.argv.includes("--json")) process.stdout.write(canonical(fixtures.selection.map(runFixture)) + "\n");
  else if (process.argv.includes("--replay")) {
    const fs = require("node:fs");
    const { planDispatch, planNext, selectionOnly } = require("./work_queue_scheduler.cjs");
    const { serializeTransactionLog } = require("./work_queue_replay.cjs");
    const input = parseStrictJSON(fs.readFileSync(0, "utf8"));
    const state = replayTransactions(input.transactions);
    const output = { state: serializeProjection(state), canonical: serializeTransactionLog(input.transactions) };
    if (input.pool) output.next = selectionOnly(planNext(state, input.pool, input.at));
    if (input.parameters) output.decision = planDispatch(state, input.parameters, { requestId: input.request_id, commitId: input.commit_id, at: input.at });
    process.stdout.write(canonical(output) + "\n");
  } else registerTests(require("node:test"));
}
module.exports = { registerTests, runFixture };
