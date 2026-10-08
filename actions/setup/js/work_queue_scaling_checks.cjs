"use strict";

const assert = require("node:assert/strict");
const { newWork, validateGraphAdmission } = require("./work_queue_graph.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { planDispatch, planNext } = require("./work_queue_scheduler.cjs");
const { replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const { administrator, genesis, submission, grant, bind, finish, operationCommit, evidence } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("queue retained-history scaling", () => {
    it("validates a maximum-depth forward DAG and reports its cycle without recursive traversal", () => {
      const state = replayTransactions([genesis()]);
      const nodes = Array.from({ length: 4096 }, (_, index) => ({
        ...newWork({}, "deep", String(index), "default", state.policy, 1),
        /** @type {Array<{kind: string, work_id: string}>} */
        depends_on: [],
      }));
      for (let index = 0; index < nodes.length - 1; index++) nodes[index].depends_on.push({ kind: "work", work_id: nodes[index + 1].work_id });
      assert.equal(validateGraphAdmission(state, nodes, administrator), nodes);
      nodes[nodes.length - 1].depends_on.push({ kind: "work", work_id: nodes[0].work_id });
      assert.throws(() => validateGraphAdmission(state, nodes, administrator), { code: "dependency_cycle" });
    });

    it("does not traverse historical payloads or commits when packing active Work", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.max_claims = 16;
      const log = [genesis(policy)];
      log.push(submission(log, ["archived", ...Array.from({ length: 16 }, (_, index) => `active-${index}`)]));
      log.push(operationCommit(log, "cancel-archive", "cancel_work", [{ kind: "WorkCancellation", work_id: log[1].operations[0].work_id, reason: "stress_cancelled" }], administrator));
      const state = replayTransactions(log);
      const archived = state.works.get(log[1].operations[0].work_id);
      Object.defineProperty(archived, "payload", {
        get() {
          throw new Error("historical payload scanned");
        },
      });
      Object.defineProperty(state.transactions[0], "trace", {
        enumerable: true,
        get() {
          throw new Error("historical commit scanned");
        },
      });
      const result = planDispatch(state, { pool: "default", max_claims: 16, max_dispatches: 1, max_bytes: 49152 }, { requestId: "scale", commitId: "scale", at: 100 });
      assert.equal(result.operations.length, 16);
      assert.equal(result.assignments.length, 1);
      assert.equal(state.claims.size, 0);
      assert.equal(state.dispatches.size, 0);
    });

    it("rebuilds derived indexes after callers change a detached public projection", () => {
      const log = [genesis()];
      log.push(submission(log, ["first", "second"]));
      const state = replayTransactions(log);
      assert.equal(planNext(state, "default", 100).work_id, log[1].operations[0].work_id);
      state.works.get(log[1].operations[0].work_id).state = "cancelled";
      assert.equal(planNext(state, "default", 100).work_id, log[1].operations[1].work_id);
    });

    it("retains verified historical Results needed by a new scheduling copy", () => {
      const log = [genesis()];
      log.push(submission(log, ["parent"]));
      const decision = grant(log);
      log.push(decision.commit);
      const assignment = decision.assignments[0];
      const bound = bind(log, assignment.dispatch_id);
      log.splice(0, log.length, ...bound);
      log.push(finish(log, assignment.dispatch_id, "h1", "completed"));
      let state = replayTransactions(log);
      const dispatch = state.dispatches.get(assignment.dispatch_id);
      const claim = state.claims.get(assignment.claims[0].claim_id);
      const result = {
        kind: "Result",
        work_id: claim.work_id,
        claim_id: claim.claim_id,
        completion_id: state.works.get(claim.work_id).completion_id,
        descriptor: { value: "verified" },
        evidence: evidence(state, dispatch, "delivery", 50, { source: "verified_receipts", run_id: dispatch.run.run_id, run_attempt: 1, receipt: "stress_verified" }),
      };
      log.push(operationCommit(log, "result", "result", [result], undefined, 50));
      log.push(
        operationCommit(
          log,
          "release",
          "release",
          [{ kind: "Release", dispatch_id: dispatch.dispatch_id, evidence: evidence(state, dispatch, "terminal_run", 60, { run_id: dispatch.run.run_id, run_attempt: 1, status: "completed", conclusion: "success" }) }],
          undefined,
          60
        )
      );
      log.push(submission(log, ["child"], { id: "child", at: 70, transform: node => ({ ...node, depends_on: [{ kind: "work", work_id: claim.work_id }] }) }));
      state = replayTransactions(log);
      const before = serializeProjection(state);
      const planned = planDispatch(state, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }, { requestId: "child-grant", commitId: "child-grant", at: 80 });
      assert.deepEqual(planned.assignments[0].claims[0].result_refs, [{ work_id: claim.work_id, result_commit_id: "result", descriptor: { value: "verified" } }]);
      planned.assignments[0].claims[0].result_refs[0].descriptor.value = "mutated";
      planned.assignments[0].claims[0].work.task = "mutated";
      assert.deepEqual(serializeProjection(state), before);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
