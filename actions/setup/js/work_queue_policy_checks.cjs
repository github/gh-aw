"use strict";

const assert = require("node:assert/strict");
const { actorFromContext, decimal, defaultPolicy, validatePolicy, validateRequestRole, validateTrustedContext } = require("./work_queue_policy.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { administrator, context, dispatcher, genesis, operationCommit } = require("./work_queue_test_helpers.cjs");
const sharedContract = require("../../../specs/work-queue/fixtures/contract.json");

function registerTests({ describe, it }) {
  describe("mandatory immutable queue policy and trusted caller normalization", () => {
    it("rejects missing/disabled policy modes and malformed weights, defaults, producer or route entitlements", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      assert.equal(validatePolicy(policy), policy);
      for (const mode of [undefined, false, "disabled", "advisory"]) assert.throws(() => validatePolicy({ ...policy, mode }));
      for (const weight of [0, -1, 1.5, 1001, Infinity]) assert.throws(() => validatePolicy({ ...policy, accounting_weights: { "": 1, a: weight } }));
      assert.throws(() => validatePolicy({ ...policy, accounting_weights: { "": 2 } }));
      assert.throws(() => validatePolicy({ ...policy, accounting_weights: { "": 1, ["é".repeat(65)]: 1 } }));
      assert.throws(() => validatePolicy({ ...policy, producers: { "1001": { pools: ["unknown"], priorities: [3], fairness_keys: [""] } } }));
      const badRoute = structuredClone(policy);
      badRoute.pools.default.default_profile = "not-approved";
      assert.throws(() => validatePolicy(badRoute));
      const disabledLimits = structuredClone(policy);
      disabledLimits.limits.predecessors = 0;
      assert.throws(() => validatePolicy(disabledLimits));
      for (const ref of ["refs/heads/main", "a".repeat(39), "A".repeat(40)]) {
        const mutable = structuredClone(policy);
        mutable.pools.default.profiles.default.ref = ref;
        assert.throws(() => validatePolicy(mutable), /immutable revisions/);
      }
      for (const workflow of ["worker.lock.yml", ".github/workflows/../worker.lock.yml", ".github/workflows/worker.yml"]) {
        const bad = structuredClone(policy);
        bad.pools.default.profiles.default.workflow = workflow;
        assert.throws(() => validatePolicy(bad), /approved workflow paths/);
      }
      const closure = structuredClone(policy);
      closure.limits.operations = 2;
      assert.throws(() => validatePolicy(closure), /worst-case assignment closure/);
      const unboundedBackoff = structuredClone(policy);
      unboundedBackoff.pools.default.retry.backoff_ms = 3600001;
      assert.throws(() => validatePolicy(unboundedBackoff));
    });
    it("matches all independent shared request-role combinations, including unknown request kinds", () => {
      const kinds = [...new Set(Object.values(sharedContract.request_roles).flat()), "unknown"];
      let combinations = 0;
      for (const [role, approvedKinds] of Object.entries(sharedContract.request_roles)) {
        const actor = { ...dispatcher, role, ...(role === "worker" ? { dispatch_id: "bound-dispatch", claim_handle: "h1" } : {}) };
        for (const kind of kinds) {
          if (approvedKinds.includes(kind)) assert.doesNotThrow(() => validateRequestRole(actor, kind), `${role}/${kind}`);
          else
            assert.throws(
              () => validateRequestRole(actor, kind),
              error => error.code === "actor_unauthorized",
              `${role}/${kind}`
            );
          combinations++;
        }
      }
      assert.equal(combinations, 65);
    });
    it("never derives authenticated role, native origin, or attempt from the agent's actor fields", () => {
      const trusted = context(dispatcher);
      assert.deepEqual(actorFromContext(trusted), dispatcher);
      assert.equal(validateTrustedContext(trusted, dispatcher).principal, "1001");
      assert.throws(() => actorFromContext({ ...dispatcher }), /authenticated caller/);
      assert.throws(() => actorFromContext({ ...trusted, roles: ["producer"] }), /approved role/);
      assert.throws(() => validateTrustedContext(trusted, { ...dispatcher, role: "administrator" }), /differs from authenticated caller/);
      assert.equal(actorFromContext({ ...trusted, run_attempt: 2 }).run_attempt, 2);
      assert.equal(actorFromContext({ ...trusted, role: "producer", roles: ["producer"], run_attempt: 2 }).run_attempt, 2);
      assert.equal(actorFromContext({ ...trusted, run_id: "9007199254740993", run_attempt: 4096 }).run_id, "9007199254740993");
      for (const run_attempt of [0, 4097, 1.5, "2"]) assert.throws(() => actorFromContext({ ...trusted, run_attempt }), /originating run attempt/);
      const worker = { ...trusted, role: "worker", roles: ["worker"], dispatch_id: "bound-dispatch", claim_handle: "h1" };
      assert.throws(() => actorFromContext({ ...worker, run_attempt: 2 }), /attempt 1/);
      assert.equal(decimal("9".repeat(256), "native ID"), "9".repeat(256));
      assert.throws(() => decimal("9".repeat(257), "native ID"));
    });
    it("requires lossless canonical GitHub IDs for authenticated principals, profiles and producer keys", () => {
      for (const principal of ["bot", "0", "01", "1\n", "", 1001, "9".repeat(257)]) {
        assert.throws(() => actorFromContext(context({ ...dispatcher, principal })));
        const profilePolicy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
        profilePolicy.pools.default.profiles.default.principal = principal;
        assert.throws(() => validatePolicy(profilePolicy));
        if (typeof principal === "string") {
          const producerPolicy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
          producerPolicy.producers = { [principal]: producerPolicy.producers["1001"] };
          assert.throws(() => validatePolicy(producerPolicy));
        }
      }
      for (const principal of ["9007199254740993", "9".repeat(129), "9".repeat(256)]) {
        assert.equal(actorFromContext(context({ ...dispatcher, principal })).principal, principal);
        const policy = defaultPolicy({ repository: "owner/repo", principal });
        assert.equal(validatePolicy(policy).pools.default.profiles.default.principal, principal);
        assert.deepEqual(Object.keys(policy.producers), [principal]);
      }
    });
    it("does not resurrect a stale credential-generation observation on a repeated cutover name", () => {
      const log = [genesis()];
      log.push(operationCommit(log, "rotate", "control", [{ kind: "Control", control: "credential_generation", value: "second", reason: "equivalent_scope" }], administrator));
      const revert = operationCommit(log, "revert", "control", [{ kind: "Control", control: "credential_generation", value: "initial", reason: "unsafe_revival" }], administrator);
      assert.throws(() => replayTransactions([...log, revert]), /credential_generation_reused/);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
