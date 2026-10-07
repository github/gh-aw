"use strict";

const assert = require("node:assert/strict");
const { canonicalBytes } = require("./work_queue_codec.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { checkLedgerBudget, observationRefreshBudget, recoveryHeadroom } = require("./work_queue_limits.cjs");
const { appendCommit, generateRequestOperations, newRequest, replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const { administrator, bind, commit, dispatcher, evidence, finish, genesis, grant, reconciler, submission } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("queue operating envelope and conservative closure reserve", () => {
    it("reserves a Claim slot before optional reads without changing replay state", () => {
      const state = replayTransactions([genesis()]);
      const before = serializeProjection(state);
      assert.equal(observationRefreshBudget(state), 128);
      assert.equal(observationRefreshBudget(state, 255), 255);
      assert.equal(observationRefreshBudget(state, 0), 0);
      assert.deepEqual(serializeProjection(state), before);
      for (const maximum of [-1, 0.5, 256, NaN, Infinity]) assert.throws(() => observationRefreshBudget(state, maximum), /observation refresh budget/);
      assert.throws(() => observationRefreshBudget({ policy: null }), /policy_missing/);
      const paused = structuredClone(state);
      paused.grants_paused = true;
      assert.equal(observationRefreshBudget(paused), 0);
      const full = structuredClone(state);
      for (const extra of [0, 1, full.policy.limits.recovery_bytes]) {
        full.ledgerBytes = full.policy.limits.ledger_bytes + extra;
        assert.equal(observationRefreshBudget(full), 0);
      }
    });
    it("keeps unknown gates from spending the final operation slot needed by independent ready Work", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.operations = 3;
      policy.pools.default.profiles.default.max_claims = 1;
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "1", number: "1" };
      const log = [genesis(policy)];
      log.push(
        submission(log, ["gated-a", "gated-b", "gated-c"], {
          transform: (node, index) => ({ ...node, depends_on: [{ kind: "issue", condition: "completed", resource: { ...resource, resource_id: String(index + 1), number: String(index + 1) } }] }),
        })
      );
      log.push(submission(log, ["independent"], { id: "independent" }));
      const state = replayTransactions(log);
      const before = serializeProjection(state);
      const request = newRequest("bounded-unknown-frontier", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
      const budget = observationRefreshBudget(state);
      assert.equal(budget, 2);
      const observations = log[1].operations.slice(0, budget).map((node, index) => ({
        kind: "Observation",
        observation_id: `bounded-${index}`,
        resource: node.depends_on[0].resource,
        condition: "completed",
        state: "unknown",
        observed_at: 100,
        credential_generation: state.credential_generation,
        read_status: "external_read_credentials_missing",
      }));
      const decision = generateRequestOperations(state, request, dispatcher, 100, "bounded-grant", observations);
      assert.deepEqual(
        decision.operations.map(operation => operation.kind),
        ["Observation", "Observation", "Claim"]
      );
      assert.ok("assignments" in decision);
      assert.equal(decision.assignments.length, 1);
      assert.equal(decision.assignments[0].claims[0].work_id, log[2].operations[0].work_id);
      const candidate = { version: 3, id: "bounded-grant", previous: state.tip, request, actor: dispatcher, policy_epoch: state.policy_epoch, at: 100, operations: decision.operations };
      const accepted = appendCommit(log, candidate);
      assert.equal(accepted.state.observations.size, 2);
      assert.equal(accepted.state.requests.has(request.id), true);
      assert.deepEqual(serializeProjection(state), before);
      const waiting = replayTransactions(log.slice(0, 2));
      const noGrant = generateRequestOperations(waiting, request, dispatcher, 100, "waiting-grant", observations);
      assert.equal(noGrant.operations.length, 0);
      assert.equal(waiting.observations.size, 0);
      assert.equal(waiting.requests.has(request.id), false);
    });
    it("rejects payload, assignment, graph, pending-node and operation bounds before admission", () => {
      for (const { field, value } of [
        { field: "payload_bytes", value: 8 },
        { field: "assignment_bytes", value: 1 },
        { field: "graph_nodes", value: 1 },
        { field: "pending_nodes", value: 1 },
        { field: "operations", value: 1 },
      ]) {
        const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
        policy.limits[field] = value;
        assert.throws(() => {
          const log = [genesis(policy)];
          const nodes = submission(log, ["a", "b"]);
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
      assert.throws(
        () => replayTransactions(log),
        error => error instanceof Error && "code" in error && error.code === "ledger_limit" && error.message === "ledger_limit: new admission would consume bounded closure/recovery headroom"
      );
    });
    it("funds last-attempt Completion, maximum delivery and mixed closure after Controls fill free recovery capacity", test => {
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
      function fillFreeCapacity(stage, transactions = log, projection = state) {
        for (let index = 0; index < 512; index++) {
          const operations = [{ kind: "Control", control: "grants_paused", value: true, reason: "x" }];
          const candidate = commit(projection.tip, `fill-${stage}-${index}`, "control", administrator, { operations }, operations, 4000, projection.policy_epoch);
          let next;
          try {
            next = replayTransactions([...transactions, candidate]);
          } catch (error) {
            assert.equal(error.code, "ledger_limit");
            assert.ok(projection.ledgerBytes > projection.policy.limits.ledger_bytes);
            assert.ok(projection.policy.limits.ledger_bytes + projection.policy.limits.recovery_bytes - projection.ledgerBytes - recoveryHeadroom(projection) < canonicalBytes(candidate) + 1);
            assert.equal(projection.requests.has(candidate.request.id), false);
            assert.equal(replayTransactions(transactions).tip, projection.tip);
            return projection;
          }
          transactions.push(candidate);
          projection = next;
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
      state = fillFreeCapacity("completion");
      const beforeCompletion = recoveryHeadroom(state);
      const completion = finish(log, dispatchId, "h1", "completed", { id: "last-attempt", at: 4000 });
      log.push(completion);
      state = replayTransactions(log);
      assert.equal(beforeCompletion - recoveryHeadroom(state), 8192);
      const member = granted.assignments[0].claims[0];
      assert.equal(state.works.get(member.work_id).barrier, "pending");
      const descriptor = { x: "x".repeat(state.policy.limits.result_bytes - 8) };
      assert.equal(canonicalBytes(descriptor), state.policy.limits.result_bytes);
      const releasePendingOperations = granted.assignments[0].claims.slice(1).flatMap(claim => [
        { kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "x".repeat(128), retry_not_before: 34000 },
        { kind: "WorkCancellation", work_id: claim.work_id, reason: "x".repeat(128) },
      ]);
      releasePendingOperations.push({ kind: "Release", dispatch_id: dispatchId, evidence: maximumEvidence(false) });
      const releasePending = commit(state.tip, "release-before-delivery", "release", maximumActor, { operations: releasePendingOperations }, releasePendingOperations, 4000, state.policy_epoch);
      const releasedLog = [...log, releasePending];
      let releasedPending = replayTransactions(releasedLog);
      assert.equal(releasedPending.dispatches.get(dispatchId).released, true);
      assert.equal(releasedPending.works.get(member.work_id).barrier, "pending");
      assert.equal(recoveryHeadroom(releasedPending), 2 * policy.limits.result_bytes + 2 * policy.limits.evidence_bytes + 8192);
      releasedPending = fillFreeCapacity("released-pending", releasedLog, releasedPending);
      const pendingResults = [{ kind: "Result", work_id: member.work_id, claim_id: member.claim_id, completion_id: completion.id, descriptor, evidence: maximumEvidence(true) }];
      const pendingResult = commit(releasedPending.tip, "released-maximum-result", "result", maximumActor, { operations: pendingResults }, pendingResults, 4000, releasedPending.policy_epoch);
      pendingResult.request = newRequest("\\".repeat(256), "result", maximumActor, { operations: pendingResults });
      const pendingResultBytes = canonicalBytes(pendingResult) + 1;
      assert.equal(pendingResultBytes, 13448);
      assert.ok(pendingResultBytes <= recoveryHeadroom(releasedPending));
      const deliveredAfterRelease = replayTransactions([...releasedLog, pendingResult]);
      assert.equal(deliveredAfterRelease.ledgerBytes - releasedPending.ledgerBytes, pendingResultBytes);
      assert.equal(deliveredAfterRelease.works.get(member.work_id).barrier, "verified");
      assert.equal(recoveryHeadroom(deliveredAfterRelease), 0);
      test.diagnostic?.(`Released pending Completion: maximum Result ${pendingResultBytes} canonical bytes; discharged reserve ${recoveryHeadroom(releasedPending)} bytes`);
      state = fillFreeCapacity("result");
      const results = [{ kind: "Result", work_id: member.work_id, claim_id: member.claim_id, completion_id: completion.id, descriptor, evidence: maximumEvidence(true) }];
      const result = commit(state.tip, "maximum-result", "result", maximumActor, { operations: results }, results, 4000, state.policy_epoch);
      result.request = newRequest("\\".repeat(256), "result", maximumActor, { operations: results });
      log.push(result);
      state = replayTransactions(log);
      assert.equal(state.works.get(member.work_id).barrier, "verified");
      state = fillFreeCapacity("release");
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
      state = fillFreeCapacity("native-only");
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
