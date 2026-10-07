"use strict";

const assert = require("node:assert/strict");
const { canonicalBytes, identity } = require("./work_queue_codec.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { dependencyStatus, gateKey, validateResource, observationSatisfies } = require("./work_queue_graph.cjs");
const { newRequest, replayTransactions } = require("./work_queue_replay.cjs");
const { bind, evidence, finish, genesis, grant, operationCommit, reconciler, submission } = require("./work_queue_test_helpers.cjs");

function registerTests({ describe, it }) {
  describe("typed bounded Work/Issue/PR dependency graph", () => {
    it("preserves opaque resource identities and refuses wrong hosts, resource kinds and numeric coercion", () => {
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "999999999999999999999", resource_id: "999999999999999999998", number: "42" };
      assert.equal(validateResource(resource), resource);
      assert.doesNotThrow(() => validateResource({ ...resource, repository_id: "9".repeat(256), resource_id: "8".repeat(256), number: "7".repeat(256) }));
      for (const invalid of [
        { ...resource, number: 42 },
        { ...resource, number: "042" },
        { ...resource, repository_id: "0" },
        { ...resource, kind: "work" },
        { ...resource, host: "agent-controlled.invalid" },
        { ...resource, repository_id: "9".repeat(257) },
      ])
        assert.throws(() => validateResource(invalid));
    });
    it("deduplicates typed gates by stable IDs rather than renamed display coordinates", () => {
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "3" };
      const renamed = { ...resource, repository: "owner/renamed", number: "30" };
      assert.equal(gateKey(resource, "completed"), gateKey(renamed, "completed"));
      for (const distinct of [
        { ...renamed, repository_id: "4" },
        { ...renamed, resource_id: "5" },
        { ...renamed, kind: "pull_request" },
      ])
        assert.notEqual(gateKey(resource, "completed"), gateKey(distinct, "completed"));
      assert.notEqual(gateKey(resource, "completed"), gateKey(resource, "closed"));
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.allowed_repositories.push("owner/renamed");
      policy.limits.graph_nodes = 3;
      const log = [genesis(policy)];
      const edges = [resource, renamed].map(resource => ({ kind: "issue", resource, condition: "completed" }));
      log.push(submission(log, ["a", "b"], { transform: (node, index) => ({ ...node, depends_on: [edges[index]] }) }));
      log.push(
        operationCommit(
          log,
          "renamed-observation",
          "observe",
          [
            {
              kind: "Observation",
              observation_id: "renamed-proof",
              resource: renamed,
              condition: "completed",
              state: "ready",
              observed_at: 20,
              credential_generation: "initial",
              read_status: "ok",
              resource_state: "closed",
              state_reason: "completed",
            },
          ],
          reconciler,
          30
        )
      );
      const state = replayTransactions(log);
      assert.equal(state.stats.nodes, 3);
      for (const work of state.works.values()) assert.equal(dependencyStatus(state, work, 30).ready, true);
      const duplicated = submission(log, ["c"], { id: "duplicate-gate", transform: node => ({ ...node, depends_on: edges }) });
      assert.throws(() => replayTransactions([...log, duplicated]), /duplicate predecessor/);
    });
    it("distinguishes Issue completed/closed and PR merged with actual merge provenance", () => {
      const observation = { state: "ready", read_status: "ok", resource: { kind: "issue" }, condition: "completed", resource_state: "closed", state_reason: "not_planned" };
      assert.equal(observationSatisfies(observation), false);
      assert.equal(observationSatisfies({ ...observation, condition: "closed" }), true);
      assert.equal(observationSatisfies({ ...observation, state_reason: "completed" }), true);
      assert.equal(observationSatisfies({ ...observation, read_status: "inaccessible", state_reason: "completed" }), false);
      const pr = { state: "ready", read_status: "ok", resource: { kind: "pull_request" }, condition: "merged", merged: true };
      assert.equal(observationSatisfies(pr), false);
      assert.equal(observationSatisfies({ ...pr, merge_commit: "opaque commit proof" }), true);
      assert.equal(observationSatisfies({ ...pr, merged: false, merge_commit: "not a merge" }), false);
    });
    it("counts different gate predicates separately and never grants foreign resource mutation rights", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.graph_nodes = 2;
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "3" };
      const log = [genesis(policy)];
      log.push(
        submission(log, ["a"], {
          transform: node => ({
            ...node,
            depends_on: [
              { kind: "issue", resource, condition: "completed" },
              { kind: "issue", resource, condition: "closed" },
            ],
          }),
        })
      );
      assert.throws(() => replayTransactions(log), /graph counts/);
      const ordinary = [genesis()];
      ordinary.push(submission(ordinary, ["foreign"], { transform: node => ({ ...node, subject: { ...resource, repository: "foreign/private" } }) }));
      assert.throws(() => replayTransactions(ordinary), /allowlisted/);
    });
    it("rejects Work whose bounded future Result inputs cannot fit an immutable assignment", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.assignment_bytes = 5000;
      const log = [genesis(policy)];
      const nodes = submission(log, ["parent", "child"]);
      nodes.operations[1].depends_on = [{ kind: "work", work_id: nodes.operations[0].work_id }];
      nodes.request = newRequest(nodes.request.id, "submit", nodes.actor, { nodes: nodes.operations });
      assert.throws(
        () => replayTransactions([...log, nodes]),
        error => error instanceof Error && "code" in error && error.code === "assignment_limit" && /bounded declared Result inputs/.test(error.message)
      );
    });
    it("admits ten bounded future Result references and atomically rejects eleven or more with the shared assignment limit code", () => {
      for (const count of [7, 9, 10, 11, 12]) {
        const log = [genesis()];
        const nodes = submission(log, ["child", ...Array.from({ length: count }, (_, index) => `parent-${index}`)]);
        nodes.operations[0].depends_on = nodes.operations.slice(1).map(parent => ({ kind: "work", work_id: parent.work_id }));
        nodes.request = newRequest(nodes.request.id, "submit", nodes.actor, { nodes: nodes.operations });
        if (count <= 10) assert.equal(replayTransactions([...log, nodes]).works.size, count + 1);
        else
          assert.throws(
            () => replayTransactions([...log, nodes]),
            error => error instanceof Error && "code" in error && error.code === "assignment_limit"
          );
      }
    });
    it("admits at the exact conservative escaped future assignment byte ceiling and rejects one byte below it", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const initial = [genesis(policy)];
      const nodes = submission(initial, ["child", "parent"]);
      const [child, parent] = nodes.operations;
      child.depends_on = [{ kind: "work", work_id: parent.work_id }];
      const worstIdentity = "\\".repeat(256);
      assert.equal(canonicalBytes(worstIdentity), 514);
      assert.equal(canonicalBytes('"'.repeat(256)), 514);
      assert.equal(identity(worstIdentity), worstIdentity);
      assert.equal(identity('"'.repeat(256)), '"'.repeat(256));
      assert.throws(() => identity("\u0001".repeat(256)), /ledger_invalid/);
      const descriptor = { data: "x".repeat(policy.limits.result_bytes - canonicalBytes({ data: "" })) };
      assert.equal(canonicalBytes(descriptor), policy.limits.result_bytes);
      const assignment = {
        version: 3,
        dispatch_id: "d".repeat(70),
        request_id: worstIdentity,
        commit_id: worstIdentity,
        policy_epoch: "initial",
        pool: child.pool,
        worker_profile: child.worker_profile,
        claims: [{ handle: "h16", claim_id: "c".repeat(70), work_id: child.work_id, work: child.payload, result_refs: [{ work_id: parent.work_id, result_commit_id: worstIdentity, descriptor }] }],
      };
      const bytes = canonicalBytes(assignment);
      for (const limit of [bytes, bytes - 1]) {
        const bounded = structuredClone(policy);
        bounded.limits.assignment_bytes = limit;
        const root = genesis(bounded);
        const request = newRequest(nodes.request.id, "submit", nodes.actor, { nodes: nodes.operations });
        const log = [root, { ...nodes, request }];
        if (limit === bytes) assert.equal(replayTransactions(log).works.size, 2);
        else
          assert.throws(
            () => replayTransactions(log),
            error => error instanceof Error && "code" in error && error.code === "assignment_limit"
          );
      }
    });
    it("charges actual immutable verified Results instead of reserving the maximum for every predecessor", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.assignment_bytes = 8192;
      policy.pools.default.logical_limit = 9;
      policy.pools.default.profiles.default.max_claims = 9;
      let log = [genesis(policy)];
      log.push(
        submission(
          log,
          Array.from({ length: 9 }, (_, index) => `parent-${index}`)
        )
      );
      const parents = log[1].operations;
      const granted = grant(log, { max_claims: parents.length, max_dispatches: 1 });
      assert.equal(granted.assignments.length, 1);
      assert.equal(granted.assignments[0].claims.length, parents.length);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      for (const member of granted.assignments[0].claims) log.push(finish(log, dispatchId, member.handle, "completed"));
      for (const member of granted.assignments[0].claims) {
        const state = replayTransactions(log);
        const claim = state.claims.get(member.claim_id);
        const dispatch = state.dispatches.get(dispatchId);
        log.push(
          operationCommit(
            log,
            `verified-${member.handle}`,
            "result",
            [
              {
                kind: "Result",
                work_id: member.work_id,
                claim_id: member.claim_id,
                completion_id: claim.terminal_commit_id,
                descriptor: {},
                evidence: evidence(state, dispatch, "delivery", 100, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: `receipt-${member.handle}` }),
              },
            ],
            reconciler,
            100
          )
        );
      }
      const admitted = submission(log, ["known-results-child"], { id: "submit-known-results-child", transform: node => ({ ...node, depends_on: parents.map(parent => ({ kind: "work", work_id: parent.work_id })) }) });
      const state = replayTransactions([...log, admitted]);
      assert.equal(state.works.size, parents.length + 1);
      assert.equal(dependencyStatus(state, admitted.operations[0], 100).ready, true);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
