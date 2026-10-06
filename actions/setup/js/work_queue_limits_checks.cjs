"use strict";

const assert = require("node:assert/strict");
const { canonicalBytes } = require("./work_queue_codec.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { checkLedgerBudget, recoveryHeadroom } = require("./work_queue_limits.cjs");
const { newRequest, replayTransactions } = require("./work_queue_replay.cjs");
const { administrator, bind, commit, evidence, finish, genesis, grant, reconciler, submission } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("queue operating envelope and conservative closure reserve", () => {
    it("rejects payload, assignment, graph, pending-node and operation bounds before admission", () => {
      for (const [field, value, transform] of [
        ["payload_bytes", 8, node => node],
        ["assignment_bytes", 1, node => node],
        ["graph_nodes", 1, node => node],
        ["pending_nodes", 1, node => node],
        ["operations", 1, node => node],
      ]) {
        const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
        policy.limits[field] = value;
        assert.throws(() => {
          const log = [genesis(policy)];
          const nodes = submission(log, ["a", "b"], { transform });
          replayTransactions([...log, nodes]);
        }, /resource_limit|assignment_limit|policy_invalid/);
      }
    });
    it("budgets bounded retries, delivery and native closure separately and preserves recovery-only writes", () => {
      const log = [genesis()];
      log.push(submission(log, ["a"]));
      const state = replayTransactions(log);
      assert.equal(recoveryHeadroom(state), 3 * (4 * 1024 + 8192) + 2 * 4096 + 2 * 1024 + 8192);
      const granted = grant(log);
      const charged = replayTransactions([...log, granted.commit]);
      assert.equal(recoveryHeadroom(charged), 2 * (4 * 1024 + 8192) + 2 * 4096 + 2 * 1024 + 8192 + 8192 + 9 * (2 * 1024 + 12288) + 2 * 1024 + 8192);
      const nearLimit = structuredClone(charged);
      nearLimit.ledgerBytes = nearLimit.policy.limits.ledger_bytes;
      assert.throws(() => checkLedgerBudget(nearLimit, 1, true), /ledger_limit/);
      assert.doesNotThrow(() => checkLedgerBudget(nearLimit, 1024, false));
      assert.throws(() => checkLedgerBudget(nearLimit, 1024, false, true), /optional observations/);
      nearLimit.ledgerBytes = nearLimit.policy.limits.ledger_bytes + nearLimit.policy.limits.recovery_bytes - recoveryHeadroom(nearLimit);
      assert.throws(() => checkLedgerBudget(nearLimit, 1, false), /bounded closure\/recovery headroom/);
      nearLimit.ledgerBytes += nearLimit.policy.limits.recovery_bytes;
      assert.throws(() => checkLedgerBudget(nearLimit, 1, false), /ledger_limit/);
    });
    it("rejects admission that spends an undersized recovery reserve", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.recovery_bytes = 1024;
      const log = [genesis(policy)];
      log.push(submission(log, ["a"]));
      assert.throws(() => replayTransactions(log), /ledger_limit/);
    });
    it("funds last-attempt Completion, maximum delivery and mixed closure after Controls fill free recovery capacity", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.ledger_bytes = 8 * 1024;
      policy.limits.recovery_bytes = 220 * 1024;
      policy.pools.default.retry.max_attempts = 1;
      policy.pools.default.profiles.default.max_claims = 3;
      let log = [genesis(policy)];
      log.push(submission(log, ["a", "b", "c"]));
      const granted = grant(log, { max_claims: 3, max_dispatches: 3 });
      assert.equal(granted.assignments.length, 1);
      assert.equal(granted.assignments[0].claims.length, 3);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      let state = replayTransactions(log);
      function fillFreeCapacity(stage) {
        for (let index = 0; index < 512; index++) {
          const operations = [{ kind: "Control", control: "grants_paused", value: true, reason: "x" }];
          const candidate = commit(state.tip, `fill-${stage}-${index}`, "control", administrator, { operations }, operations, 4000, state.policy_epoch);
          let next;
          try {
            next = replayTransactions([...log, candidate]);
          } catch (error) {
            assert.equal(error.code, "ledger_limit");
            assert.ok(state.ledgerBytes > state.policy.limits.ledger_bytes);
            assert.ok(state.policy.limits.ledger_bytes + state.policy.limits.recovery_bytes - state.ledgerBytes - recoveryHeadroom(state) < canonicalBytes(candidate) + 1);
            assert.equal(state.requests.has(candidate.request.id), false);
            assert.equal(replayTransactions(log).tip, state.tip);
            return;
          }
          log.push(candidate);
          state = next;
        }
        assert.fail("Controls did not exhaust unallocated recovery capacity");
      }
      const maximumActor = { ...reconciler, workflow: "\\".repeat(256), run_id: "9".repeat(256), run_attempt: 4096, dispatch_id: "\\".repeat(256), claim_handle: "\\".repeat(256) };
      function maximumEvidence(delivery) {
        const proof = evidence(state, state.dispatches.get(dispatchId), delivery ? "delivery" : "terminal_run", 4000, {
          source: delivery ? "verified_receipts" : "github_api",
          run_id: "200",
          run_attempt: 1,
          status: "completed",
          receipt: "\\".repeat(256),
          conclusion: "x",
        });
        const remaining = state.policy.limits.evidence_bytes - canonicalBytes(proof);
        assert.ok(remaining >= 0 && remaining <= 255);
        proof.conclusion += "x".repeat(remaining);
        assert.equal(canonicalBytes(proof), state.policy.limits.evidence_bytes);
        return proof;
      }
      fillFreeCapacity("completion");
      const beforeCompletion = recoveryHeadroom(state);
      const completion = finish(log, dispatchId, "h1", "completed", { id: "last-attempt", at: 4000 });
      log.push(completion);
      state = replayTransactions(log);
      assert.equal(beforeCompletion - recoveryHeadroom(state), 8192);
      const member = granted.assignments[0].claims[0];
      assert.equal(state.works.get(member.work_id).barrier, "pending");
      fillFreeCapacity("result");
      const descriptor = { x: "x".repeat(state.policy.limits.result_bytes - 8) };
      assert.equal(canonicalBytes(descriptor), state.policy.limits.result_bytes);
      const results = [{ kind: "Result", work_id: member.work_id, claim_id: member.claim_id, completion_id: completion.id, descriptor, evidence: maximumEvidence(true) }];
      const result = commit(state.tip, "maximum-result", "result", maximumActor, { operations: results }, results, 4000, state.policy_epoch);
      result.request = newRequest("\\".repeat(256), "result", maximumActor, { operations: results });
      log.push(result);
      state = replayTransactions(log);
      assert.equal(state.works.get(member.work_id).barrier, "verified");
      fillFreeCapacity("release");
      const operations = granted.assignments[0].claims.slice(1).flatMap(claim => [
        { kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "x".repeat(128), retry_not_before: 34000 },
        { kind: "WorkCancellation", work_id: claim.work_id, reason: "x".repeat(128) },
      ]);
      operations.push({ kind: "Release", dispatch_id: dispatchId, evidence: maximumEvidence(false) });
      const release = commit(state.tip, "maximum-release", "release", maximumActor, { operations }, operations, 4000, state.policy_epoch);
      release.request = newRequest('"'.repeat(256), "release", maximumActor, { operations });
      const closed = replayTransactions([...log, release]);
      assert.equal(closed.dispatches.get(dispatchId).released, true);
      assert.equal([...closed.works.values()].filter(work => work.state === "completed").length, 1);
      assert.equal([...closed.works.values()].filter(work => work.state === "cancelled").length, 2);
      assert.equal(recoveryHeadroom(closed), 0);
      for (const handle of ["h2", "h3"]) {
        log.push(finish(log, dispatchId, handle, "cancelled", { id: `exhausted-${handle}`, at: 4000 }));
        state = replayTransactions(log);
      }
      const lifecycleLimit = state.policy.pools.default.reconciliation.max_attempts + 4;
      while (state.lifecycleWrites.get(dispatchId) < lifecycleLimit) {
        const writes = state.lifecycleWrites.get(dispatchId);
        const dispatch = state.dispatches.get(dispatchId);
        const observations = [
          {
            kind: "Dispatch",
            dispatch_id: dispatchId,
            state: "bound",
            run: dispatch.run,
            evidence: evidence(state, dispatch, "reconciliation", 4000, { run_id: "200", run_attempt: 1 }),
          },
        ];
        log.push(commit(state.tip, `rebind-${writes}`, "dispatch", reconciler, { operations: observations }, observations, 4000, state.policy_epoch));
        state = replayTransactions(log);
      }
      assert.equal(recoveryHeadroom(state), 2 * state.policy.limits.evidence_bytes + 8192);
      fillFreeCapacity("native-only");
      const finalOperations = [{ kind: "Release", dispatch_id: dispatchId, evidence: maximumEvidence(false) }];
      const finalRelease = commit(state.tip, "final-native-release", "release", maximumActor, { operations: finalOperations }, finalOperations, 4000, state.policy_epoch);
      finalRelease.request = newRequest('"'.repeat(256), "release", maximumActor, { operations: finalOperations });
      assert.ok(canonicalBytes(finalRelease) + 1 > 4096);
      assert.ok(canonicalBytes(finalRelease) + 1 <= recoveryHeadroom(state));
      assert.equal(recoveryHeadroom(replayTransactions([...log, finalRelease])), 0);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
