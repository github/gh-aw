"use strict";

const assert = require("node:assert/strict");
const { clock, diagnostics, pick, planNext, tickScale } = require("./work_queue_scheduler.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { assignmentForDispatch, replayTransactions } = require("./work_queue_replay.cjs");
const { genesis, grant, submission } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("exact virtual-pass scheduler", () => {
    it("uses arbitrary-precision LCM and exact pass increments", () => {
      const weights = new Map([
        ["a", 997],
        ["b", 991],
        ["c", 983],
        ["d", 977],
        ["e", 971],
        ["f", 967],
      ]);
      const q = tickScale(weights.values());
      assert(q > BigInt(Number.MAX_SAFE_INTEGER));
      for (const weight of weights.values()) assert.equal(q % BigInt(weight), 0n);
      const first = pick(clock(), [...weights.keys()], weights, q);
      assert.equal(first.key, "a");
      assert.equal(first.clock.virtual, q / 997n);
      assert.equal(first.clock.passes.get("a"), (2n * q) / 997n);
    });
    it("never moves an unselected continuous key's goalpost", () => {
      const weights = new Map([
        ["a", 2],
        ["b", 1],
      ]);
      let parent = clock();
      const sequence = [];
      for (let i = 0; i < 9; i++) {
        const result = pick(parent, ["a", "b"], weights, 2n);
        sequence.push(result.key);
        parent = result.clock;
      }
      assert.deepEqual(sequence, ["a", "a", "b", "a", "a", "b", "a", "a", "b"]);
    });
    it("plans on copies and clamps reactivated keys without idle credit or refunds", () => {
      const weights = new Map([
        ["a", 1],
        ["b", 1],
      ]);
      let parent = clock();
      const before = diagnostics({ classes: parent, keys: new Map() });
      pick(parent, ["a", "b"], weights, 1n);
      assert.deepEqual(diagnostics({ classes: parent, keys: new Map() }), before);
      for (let i = 0; i < 10; i++) parent = pick(parent, ["a"], weights, 1n).clock;
      const result = pick(parent, ["a", "b"], weights, 1n);
      assert.equal(result.key, "a");
      assert.equal(result.clock.passes.get("b"), 11n);
      assert.equal(parent.active.has("b"), false);
    });
    it("orders accounting keys by UTF-8 bytes rather than locale or UTF-16", () => {
      const weights = new Map([
        ["𐀀", 1],
        ["", 1],
      ]);
      assert.equal(pick(clock(), ["𐀀", ""], weights, 1n).key, "");
      for (const weight of [0, -1, 1.1, 1001, Infinity]) assert.throws(() => tickScale([weight]), /policy_invalid/);
    });
    it("normalizes only the selected class child, leaving unvisited child passes and active sets unchanged", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.accounting_weights.stale = 1;
      policy.producers["1001"].fairness_keys.push("stale");
      const commits = [genesis(policy)];
      commits.push(submission(commits, ["class-1", "class-2"], { transform: (node, index) => ({ ...node, priority: index + 1 }) }));
      const state = replayTransactions(commits);
      const child = { virtual: 100n, passes: new Map([["stale", 100n]]), active: new Set(["stale"]) };
      const schedule = { classes: clock(), keys: new Map([[2, child]]) };
      state.clocks.set("default", schedule);
      const before = diagnostics(schedule);
      const decision = planNext(state, "default", 2);
      assert.equal(state.works.get(decision.work_id).priority, 1);
      assert.deepEqual(decision.nextClock.keys.get(2), child);
      assert.equal(decision.nextClock.keys.get(2).passes.has(""), false);
      assert.equal(decision.nextClock.keys.get(2).active.has("stale"), true);
      assert.equal(decision.nextClock.keys.has(1), true);
      assert.deepEqual(diagnostics(schedule), before);
    });
    it("returns a deeply frozen detached Dispatch assignment without lifecycle metadata", () => {
      const commits = [genesis()];
      commits.push(submission(commits, ["a"], { transform: node => ({ ...node, payload: { task: "a", nested: [{ flag: true }] } }) }));
      commits.push(grant(commits, { max_claims: 1, max_dispatches: 1 }).commit);
      const state = replayTransactions(commits);
      const dispatch = [...state.dispatches.values()][0];
      const assignment = assignmentForDispatch(state, dispatch.dispatch_id);
      assert.deepEqual(Object.keys(assignment).sort(), ["claims", "commit_id", "dispatch_id", "policy_epoch", "pool", "request_id", "version", "worker_profile"]);
      for (const value of [assignment, assignment.claims, assignment.claims[0], assignment.claims[0].work, assignment.claims[0].work.nested, assignment.claims[0].work.nested[0], assignment.claims[0].result_refs])
        assert.equal(Object.isFrozen(value), true);
      assert.notEqual(assignment.claims[0].work, dispatch.claims[0].work);
      assert.equal(Object.isFrozen(dispatch.claims[0].work), false);
      assert.throws(() => (assignment.claims[0].work.untrusted = true), TypeError);
      assert.throws(() => (assignment.claims[0].work.nested[0].flag = false), TypeError);
      assert.throws(() => assignmentForDispatch(state, "missing"), /dispatch_missing/);
    });
    it("opens a new group after byte overflow, then packs into the earliest group that still fits", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.max_claims = 3;
      const commits = [genesis(policy)];
      commits.push(submission(commits, ["a", "b", "c"], { transform: (node, index) => ({ ...node, payload: { task: node.node_key, text: index < 2 ? "x".repeat(10000) : "small" } }) }));
      const decision = grant(commits, { max_claims: 3, max_dispatches: 2, max_bytes: 18000 });
      const state = replayTransactions([...commits, decision.commit]);
      assert.deepEqual(
        decision.operations.map(operation => state.works.get(operation.work_id).node_key),
        ["a", "b", "c"]
      );
      assert.deepEqual(
        decision.assignments.map(assignment => assignment.claims.map(claim => state.works.get(claim.work_id).node_key)),
        [["a", "c"], ["b"]]
      );
      assert.equal(state.dispatches.size, 2);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
