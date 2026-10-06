"use strict";

const assert = require("node:assert/strict");
const { canonical, fingerprint } = require("./work_queue_codec.cjs");
const {
  appendCommit,
  causalChain,
  explainWork,
  newRequest,
  parseTransactionLog,
  planDispatchWithObservations,
  replayTransactions,
  serializeProjection,
  serializeTransactionLog,
  validateClaimAuthority,
  validateEvidence,
  validateRunBinding,
  validateRequestContext,
  validateWorkerContinuation,
} = require("./work_queue_replay.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { newWork, dependencyStatus, gateKey, validateGraphAdmission } = require("./work_queue_graph.cjs");
const { diagnostics, planDispatch, planNext, reservationCounts } = require("./work_queue_scheduler.cjs");
const { administrator, bind, commit, context, dispatcher, evidence, finish, genesis, grant, operationCommit, producer, reconciler, submission, workerActor } = require("./work_queue_test_helpers.cjs");

function fixture(names = ["a", "b", "c"], policy = defaultPolicy({ repository: "owner/repo", principal: "1001" })) {
  const log = [genesis(policy)];
  log.push(submission(log, names));
  return log;
}

function registerTests({ describe, it }) {
  describe("closed causal fair queue replay", () => {
    it("selects FIFO by causal position rather than age, IDs, or physical line ordering", () => {
      const log = [genesis()];
      log.push(submission(log, ["z", "a", "b"], { transform: (node, index) => ({ ...node, enqueued: 100 - index }) }));
      const decision = grant(log, { max_claims: 3, max_dispatches: 3 });
      const expected = log[1].operations.map(node => node.work_id);
      assert.deepEqual(
        decision.operations.map(claim => claim.work_id),
        expected
      );
      const history = [...log, decision.commit];
      for (const permuted of [history, [...history].reverse(), [history[1], history[2], history[0]], [...history, ...history]]) {
        assert.equal(serializeTransactionLog(permuted), serializeTransactionLog(history));
        assert.deepEqual(serializeProjection(replayTransactions(permuted)), serializeProjection(replayTransactions(history)));
      }
      assert.equal(canonical(parseTransactionLog(serializeTransactionLog(history))), canonical(history));
    });
    it("serializes long shallow histories without treating commit positions as JSON nesting depth", () => {
      const log = [genesis()];
      for (let index = 1; index <= 80; index++) {
        const operations = [{ kind: "Control", control: "grants_paused", value: index % 2 === 1, reason: "bounded_control" }];
        log.push(commit(log.at(-1).id, `long-history-${index}`, "control", administrator, { operations }, operations, index));
      }
      const expected = log.map(entry => canonical(entry)).join("\n") + "\n";
      assert.equal(serializeTransactionLog(log), expected);
      assert.equal(serializeTransactionLog([...log].reverse()), expected);
      assert.equal(parseTransactionLog(expected).length, 81);
      assert.equal(canonical(serializeProjection(replayTransactions(parseTransactionLog(expected)))), canonical(serializeProjection(replayTransactions(log))));
    });
    it("rejects old facts, missing Policy, multiple genesis, forks, missing predecessors, cycles and conflicting duplicate IDs", () => {
      const log = fixture(["a"]);
      assert.throws(() => replayTransactions([{ version: 2, kind: "Work", work: "x", claim: null, attempt: null }]), /QueueCommit|unsupported_protocol/);
      assert.throws(() => replayTransactions([{ ...log[1], previous: null }]), /policy_missing/);
      assert.throws(() => replayTransactions([...log, { ...log[0], id: "other" }]), /exactly one genesis/);
      assert.throws(() => replayTransactions([...log, { ...log[1], id: "fork" }]), /fork/);
      assert.throws(() => replayTransactions([{ ...log[1], previous: "absent" }, log[0]]), /missing causal predecessor/);
      assert.throws(() => replayTransactions([log[0], { ...log[1], id: "cycle", previous: "cycle" }]), /cyclic/);
      assert.throws(() => replayTransactions([...log, { ...log[1], at: 2 }]), /duplicate commit/);
      assert.throws(() => parseTransactionLog(serializeTransactionLog(log) + "\n"), /empty|truncated/);
    });
    it("binds a committed stable request to the same actor, kind and semantic parameters", () => {
      const log = fixture(["a"]);
      const existing = log[1];
      const duplicate = { ...existing, id: "second", previous: existing.id };
      assert.throws(() => replayTransactions([...log, duplicate]), /request_reused/);
      assert.equal(appendCommit(log, duplicate).idempotent, true);
      const changed = { ...duplicate, request: { ...duplicate.request, parameters: { nodes: [{ ...existing.operations[0], priority: 2 }] } } };
      changed.operations = changed.request.parameters.nodes;
      changed.request.fingerprint = fingerprint(changed.actor, changed.request.kind, changed.request.parameters);
      assert.throws(() => appendCommit(log, changed), /request_reused/);
      assert.throws(() => replayTransactions([{ ...existing, request: { ...existing.request, fingerprint: "0".repeat(64) } }]), /fingerprint/);
      const foreignActor = { ...existing.actor, repository: "foreign/private" };
      const foreign = { ...existing, actor: foreignActor, request: newRequest(existing.request.id, existing.request.kind, foreignActor, existing.request.parameters) };
      assert.throws(() => replayTransactions([log[0], foreign]), /foreign queue repository/);
    });
    it("validates immutable revisions on standalone native evidence and run bindings", () => {
      const proof = { kind: "reconciliation", source: "github_api", repository: "owner/repo", workflow: ".github/workflows/worker.lock.yml", ref: "a".repeat(40), principal: "1001", checked_at: 1 };
      const run = { run_id: "200", run_attempt: 1, repository: proof.repository, workflow: proof.workflow, ref: proof.ref, principal: proof.principal, event: "workflow_dispatch" };
      for (const ref of ["a".repeat(40), "b".repeat(64)]) {
        assert.doesNotThrow(() => validateEvidence({ ...proof, ref }));
        assert.doesNotThrow(() => validateRunBinding({ ...run, ref }));
      }
      for (const ref of ["refs/heads/main", "a".repeat(39), "A".repeat(40)]) {
        assert.throws(() => validateEvidence({ ...proof, ref }), /immutable native revision/);
        assert.throws(() => validateRunBinding({ ...run, ref }), /immutable revision/);
      }
      assert.throws(() => validateEvidence({ ...proof, run_id: "200", run_attempt: 2 }), /native attempt 1/);
      assert.throws(() => validateRunBinding({ ...run, run_attempt: 2 }), /original workflow_dispatch attempts/);
    });
    it("allows positive originating dispatcher reruns to start once without granting rerun worker authority", () => {
      const log = fixture(["a"]);
      const granted = grant(log);
      log.push(granted.commit);
      const actor = { ...dispatcher, principal: "1002", run_attempt: 2 };
      const dispatchId = granted.assignments[0].dispatch_id;
      const operations = [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", sender: actor }];
      const start = operationCommit(log, "rerun-origin-start", "dispatch", operations, actor);
      const started = replayTransactions([...log, start]);
      assert.equal(started.dispatches.get(dispatchId).sender.run_attempt, 2);
      const again = operationCommit([...log, start], "rerun-origin-again", "dispatch", operations, actor);
      assert.throws(() => replayTransactions([...log, start, again]), /start marker/);
      const missingWorkflow = { ...actor };
      delete missingWorkflow.workflow;
      const invalid = operationCommit(log, "origin-missing-workflow", "dispatch", [{ ...operations[0], sender: missingWorkflow }], missingWorkflow);
      assert.throws(() => replayTransactions([...log, invalid]), /start marker/);
      const profile = started.dispatches.get(dispatchId).profile;
      assert.notEqual(actor.principal, profile.principal);
      const run = { run_id: "300", run_attempt: 1, repository: "owner/repo", workflow: profile.workflow, ref: profile.ref, principal: profile.principal, event: "workflow_dispatch" };
      const binding = operationCommit(
        [...log, start],
        "origin-independent-binding",
        "dispatch",
        [
          {
            kind: "Dispatch",
            dispatch_id: dispatchId,
            state: "bound",
            run,
            evidence: evidence(started, started.dispatches.get(dispatchId), "reconciliation", 21, { run_id: run.run_id, run_attempt: 1 }),
          },
        ],
        reconciler,
        21
      );
      const bound = replayTransactions([...log, start, binding]);
      const worker = context(workerActor(bound, dispatchId), { ref: profile.ref, event: "workflow_dispatch" });
      assert.equal(validateClaimAuthority(bound, granted.operations[0].claim_id, worker).claim.claim_id, granted.operations[0].claim_id);
      assert.throws(() => validateClaimAuthority(bound, granted.operations[0].claim_id, { ...worker, principal: actor.principal }), /run_binding_conflict/);
    });
    it("rejects invalid persisted reason codes even when their request fingerprints are valid", () => {
      const log = fixture(["a"]);
      const valid = operationCommit(log, "reason-control", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "valid_code" }], administrator);
      for (const reason of ["retained-control-" + "x".repeat(220), "raw failure prose", "code\n", "é"]) {
        const invalid = structuredClone(valid);
        invalid.operations[0].reason = reason;
        invalid.request.parameters.operations = invalid.operations;
        invalid.request.fingerprint = fingerprint(invalid.actor, invalid.request.kind, invalid.request.parameters);
        assert.throws(() => replayTransactions([...log, invalid]), /reason_invalid/);
      }
    });
    it("preserves terminal cancellation metadata and rejects conflicting reasons or retry boundaries with the shared code", () => {
      let log = fixture(["a"]);
      const granted = grant(log);
      log.push(granted.commit);
      log = bind(log, granted.assignments[0].dispatch_id);
      const cancelled = finish(log, granted.assignments[0].dispatch_id, "h1", "cancelled");
      log.push(cancelled);
      const claimOperation = cancelled.operations[0];
      const duplicateClaim = operationCommit(log, "same-claim-cancellation", "cancel_claim", [claimOperation], reconciler, 50);
      assert.equal(replayTransactions([...log, duplicateClaim]).claims.get(claimOperation.claim_id).terminal_commit_id, cancelled.id);
      for (const operation of [
        { ...claimOperation, reason: "different_reason" },
        { ...claimOperation, retry_not_before: claimOperation.retry_not_before + 1 },
      ])
        assert.throws(
          () => replayTransactions([...log, operationCommit(log, "changed-claim-cancellation", "cancel_claim", [operation], reconciler, 50)]),
          error => error.code === "cancellation_conflict"
        );
      const workOperation = { kind: "WorkCancellation", work_id: claimOperation.work_id, reason: "operator_cancelled" };
      const terminal = operationCommit(log, "terminal-work-cancellation", "cancel_work", [workOperation], reconciler, 50);
      log.push(terminal);
      const duplicateWork = operationCommit(log, "same-work-cancellation", "cancel_work", [workOperation], reconciler, 60);
      assert.equal(replayTransactions([...log, duplicateWork]).works.get(workOperation.work_id).cancellation_commit_id, terminal.id);
      assert.throws(
        () => replayTransactions([...log, operationCommit(log, "changed-work-cancellation", "cancel_work", [{ ...workOperation, reason: "different_reason" }], reconciler, 60)]),
        error => error.code === "cancellation_conflict"
      );
    });
    it("revalidates both fair choices and deterministic maximal packing", () => {
      const log = fixture();
      const decision = grant(log, { max_claims: 2, max_dispatches: 2 });
      const swapped = { ...decision.commit, operations: [...decision.operations].reverse() };
      assert.throws(() => replayTransactions([...log, swapped]), /selection_invalid/);
      const regrouped = { ...decision.commit, operations: decision.operations.map((claim, index) => (index ? { ...claim, dispatch_id: decision.operations[0].dispatch_id, handle: "h2" } : claim)) };
      assert.throws(() => replayTransactions([...log, regrouped]), /selection_invalid/);
      assert.throws(() => replayTransactions([...log, { ...decision.commit, operations: decision.operations.slice(0, 1) }]), /packing_invalid/);
      const direct = { ...decision.commit, request: newRequest("direct", "cancel_claim", reconciler, { operations: decision.operations }), actor: reconciler };
      assert.throws(() => replayTransactions([...log, direct]), /unauthorized_operation/);
    });
    it("stops X/Y/X at the first incompatible fair winner without skipping or charging it", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const profile = policy.pools.default.profiles.default;
      profile.max_claims = 3;
      policy.pools.default.profiles.other = { ...profile, workflow: ".github/workflows/other.lock.yml" };
      const log = [genesis(policy)];
      log.push(submission(log, ["x1", "y", "x2"], { transform: (node, index) => ({ ...node, worker_profile: index === 1 ? "other" : "default" }) }));
      const state = replayTransactions(log);
      const before = canonical(serializeProjection(state));
      const decision = grant(log, { max_claims: 3, max_dispatches: 1 });
      assert.equal(decision.reason, "dispatch_budget_blocked");
      assert.deepEqual(
        decision.operations.map(claim => claim.work_id),
        [log[1].operations[0].work_id]
      );
      assert.equal(decision.next.work_id, log[1].operations[1].work_id);
      assert.equal(canonical(serializeProjection(state)), before);
      const after = replayTransactions([...log, decision.commit]);
      assert.equal(after.claims.size, 1);
      assert.equal(diagnostics(after.clocks.get("default")).keys["3"].pass[""], "2");
    });
    it("keeps zero-grant previews, byte-blocked proposals and Controls debt-pure", () => {
      const log = fixture(["a", "b"]);
      const state = replayTransactions(log);
      const before = canonical(serializeProjection(state));
      assert.equal(planDispatch(state, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 1 }, { requestId: "preview", commitId: "preview", at: 10 }).reason, "assignment_bytes_blocked");
      assert.equal(canonical(serializeProjection(state)), before);
      const first = grant(log);
      log.push(first.commit);
      const charged = replayTransactions(log);
      const clocks = diagnostics(charged.clocks.get("default"));
      const pause = operationCommit(log, "pause", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "maintenance" }], administrator);
      log.push(pause);
      assert.deepEqual(diagnostics(replayTransactions(log).clocks.get("default")), clocks);
      assert.equal(planNext(replayTransactions(log), "default", 30).reason, "grants_paused");
      const resume = operationCommit(log, "resume", "control", [{ kind: "Control", control: "grants_paused", value: false, reason: "restored" }], administrator);
      log.push(resume);
      assert.deepEqual(diagnostics(replayTransactions(log).clocks.get("default")), clocks);
    });
    it("does not reset policy debt while work/reservations/results remain active", () => {
      const log = fixture(["a"]);
      const op = { kind: "Policy", epoch: "second", policy: defaultPolicy({ repository: "owner/repo", principal: "1001" }) };
      const policyChange = commit(log[1].id, "second", "policy", administrator, { operations: [op] }, [op], 5, "second");
      assert.throws(() => replayTransactions([...log, policyChange]), /policy_not_quiescent/);
      const cancelled = operationCommit(log, "cancel", "cancel_work", [{ kind: "WorkCancellation", work_id: log[1].operations[0].work_id, reason: "drain" }], administrator);
      const drained = [...log, cancelled];
      const accepted = { ...policyChange, previous: cancelled.id };
      assert.equal(replayTransactions([...drained, accepted]).policy_epoch, "second");
    });
    it("enforces exact weighted class/key service with FIFO within each accounting bucket", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.accounting_weights = { "": 1, a: 2, b: 1 };
      policy.producers["1001"].fairness_keys = ["", "a", "b"];
      policy.pools.default.logical_limit = 256;
      policy.pools.default.native_limit = 256;
      policy.pools.default.profiles.default.max_claims = 16;
      policy.pools.default.profiles.default.share_keys = true;
      const log = [genesis(policy)];
      const names = Array.from({ length: 30 }, (_, index) => `a${index}`).concat(Array.from({ length: 30 }, (_, index) => `b${index}`));
      log.push(submission(log, names, { transform: node => ({ ...node, fairness_key: node.node_key[0] }) }));
      const decision = grant(log, { max_claims: 9, max_dispatches: 1 });
      const tasks = decision.operations.map(claim => log[1].operations.find(node => node.work_id === claim.work_id).node_key);
      assert.deepEqual(tasks, ["a0", "a1", "b0", "a2", "a3", "b1", "a4", "a5", "b2"]);
      const state = replayTransactions([...log, decision.commit]);
      assert.equal(state.claims.size, 9);
      assert.equal(state.dispatches.size, 1);
      assert.equal(diagnostics(state.clocks.get("default")).keys["3"].v, "6");
    });
    it("admits forward Work edges atomically and rejects missing/self/cyclic/inconsistent graphs", () => {
      const log = [genesis()];
      const policy = log[0].operations[0].policy;
      const parent = newWork({ task: "parent" }, "graph", "parent", "default", policy, 1);
      const child = { ...newWork({ task: "child" }, "graph", "child", "default", policy, 1), depends_on: [{ kind: "work", work_id: parent.work_id }] };
      const add = nodes => commit("genesis", "graph", "submit", producer, { nodes }, nodes, 1);
      const state = replayTransactions([...log, add([child, parent])]);
      assert.equal(planNext(state, "default", 1).work_id, parent.work_id);
      assert.equal(state.works.get(child.work_id).position.operation, 0);
      assert.throws(() => replayTransactions([...log, add([child])]), /predecessor/);
      assert.throws(() => replayTransactions([...log, add([{ ...parent, depends_on: [{ kind: "work", work_id: parent.work_id }] }])]), /self|cycle/);
      const cycle = { ...parent, depends_on: [{ kind: "work", work_id: child.work_id }] };
      assert.throws(() => replayTransactions([...log, add([child, cycle])]), /child -> parent -> child|parent -> child -> parent/);
      assert.throws(() => replayTransactions([...log, add([parent, parent])]), /duplicate node/);
      assert.throws(() => replayTransactions([...log, add([{ ...parent, work_id: "agent-asserted" }])]), /work_identity_invalid/);
    });
    it("counts and deduplicates first-class gates, validates predicates/freshness and generation, never charges gates", () => {
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "999999999999999999999", resource_id: "42", number: "7" };
      const log = [genesis()];
      log.push(submission(log, ["a", "b"], { transform: node => ({ ...node, depends_on: [{ kind: "issue", resource, condition: "completed" }] }) }));
      assert.equal(replayTransactions(log).stats.nodes, 3);
      assert.equal(planNext(replayTransactions(log), "default", 1).reason, "no_eligible_work");
      const observation = {
        kind: "Observation",
        observation_id: "observed",
        resource,
        condition: "completed",
        state: "ready",
        observed_at: 2,
        credential_generation: "initial",
        read_status: "ok",
        resource_state: "closed",
        state_reason: "completed",
      };
      const invalid = { ...observation, state_reason: "not_planned" };
      assert.throws(() => operationCommit(log, "bad", "observe", [invalid]), /observation_invalid/);
      log.push(operationCommit(log, "observe", "observe", [observation], reconciler, 2));
      let state = replayTransactions(log);
      assert.equal(state.observations.get(gateKey(resource, "completed")).observation_id, "observed");
      assert.deepEqual(planNext(state, "default", 3).observations, ["observed"]);
      assert.equal(planNext(state, "default", 60003).reason, "no_eligible_work");
      assert.equal(state.claims.size, 0);
      log.push(operationCommit(log, "credential", "control", [{ kind: "Control", control: "credential_generation", value: "rotated", reason: "equivalent-scope-replacement" }], administrator, 4));
      state = replayTransactions(log);
      assert.equal(planNext(state, "default", 4).reason, "no_eligible_work");
      assert.equal(dependencyStatus(state, log[1].operations[0], 4).reason, "observation_stale");
    });
    it("packs fresh typed observations and a maximal Claim prefix atomically within the total operation bound", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.limits.operations = 3;
      const resource = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "3" };
      const log = [genesis(policy)];
      log.push(submission(log, ["a", "b", "c"], { transform: node => ({ ...node, depends_on: [{ kind: "issue", resource, condition: "completed" }] }) }));
      const state = replayTransactions(log);
      const observation = {
        kind: "Observation",
        observation_id: "fresh",
        resource,
        condition: "completed",
        state: "ready",
        observed_at: 10,
        credential_generation: "initial",
        read_status: "ok",
        resource_state: "closed",
        state_reason: "completed",
      };
      const parameters = { pool: "default", max_claims: 3, max_dispatches: 3, max_bytes: 49152 };
      const request = newRequest("request-atomic", "dispatch_next", dispatcher, parameters);
      const decision = planDispatchWithObservations(state, request, dispatcher, 10, "atomic", [observation]);
      assert.equal(decision.operations.length, 3);
      assert.deepEqual(
        decision.operations.map(operation => operation.kind),
        ["Observation", "Claim", "Claim"]
      );
      const envelope = commit(state.tip, "atomic", "dispatch_next", dispatcher, parameters, decision.operations, 10);
      const accepted = replayTransactions([...log, envelope]);
      assert.equal(accepted.claims.size, 2);
      const adminRequest = newRequest("request-admin-atomic", "dispatch_next", administrator, parameters);
      const adminDecision = planDispatchWithObservations(state, adminRequest, administrator, 10, "admin-atomic", [observation]);
      const adminEnvelope = commit(state.tip, "admin-atomic", "dispatch_next", administrator, parameters, adminDecision.operations, 10);
      assert.equal(replayTransactions([...log, adminEnvelope]).claims.size, 2);
      const adminObserve = operationCommit(log, "admin-observe", "observe", [observation], administrator, 10);
      assert.equal(replayTransactions([...log, adminObserve]).observations.size, 1);
      const producerObserve = operationCommit(log, "producer-observe", "observe", [observation], producer, 10);
      assert.throws(() => replayTransactions([...log, producerObserve]), /approved|unauthorized/);
      assert.equal(state.observations.size, 0);
      assert.equal(state.claims.size, 0);
      const blockedRequest = newRequest("request-blocked", "dispatch_next", dispatcher, { ...parameters, max_bytes: 1 });
      const blocked = planDispatchWithObservations(state, blockedRequest, dispatcher, 10, "blocked", [observation]);
      assert.equal(blocked.reason, "assignment_bytes_blocked");
      assert.deepEqual(blocked.operations, []);
      assert.equal(state.observations.size, 0);
    });
    it("finalizes batched Claims independently, retains native capacity, and requires exact run/Claim binding", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.max_claims = 3;
      let log = fixture(["a", "b", "c"], policy);
      const granted = grant(log, { max_claims: 3 });
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      const unbound = replayTransactions(log);
      const claimId = granted.operations[0].claim_id;
      assert.throws(
        () =>
          validateClaimAuthority(
            unbound,
            claimId,
            context({ role: "worker", principal: "1001", repository: "owner/repo", workflow: ".github/workflows/worker.lock.yml", run_id: "200", run_attempt: 1, dispatch_id: dispatchId, claim_handle: "h1" })
          ),
        /claim_ineffective/
      );
      log = bind(log, dispatchId);
      log.push(finish(log, dispatchId, "h1", "completed"));
      log.push(finish(log, dispatchId, "h2", "cancelled"));
      let state = replayTransactions(log);
      assert.equal(state.repository, "owner/repo");
      assert.equal(state.works.get(granted.operations[0].work_id).state, "completed");
      assert.equal(state.works.get(granted.operations[1].work_id).state, "available");
      assert.equal(state.works.get(granted.operations[2].work_id).state, "claimed");
      assert.deepEqual(reservationCounts(state, "default", ""), { logical: 1, native: 1, account: 1 });
      assert.equal(state.claims.get(claimId).terminal_commit_id, "finish-h1");
      assert.equal(state.claims.get(granted.operations[1].claim_id).terminal_commit_id, "finish-h2");
      assert.equal(state.claims.get(granted.operations[1].claim_id).cancellation_reason, "worker_cancelled");
      assert.equal(state.claims.get(granted.operations[1].claim_id).retry_not_before, 30040);
      assert.equal(state.dispatches.get(dispatchId).lifecycle_writes, 2);
      assert.deepEqual(state.dispatches.get(dispatchId).profile, policy.pools.default.profiles.default);
      const actor = workerActor(state, dispatchId, "h1");
      const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
      assert.equal(validateClaimAuthority(state, claimId, trusted, { requireCompletion: true }).claim.handle, "h1");
      assert.throws(() => validateClaimAuthority(state, claimId, { ...trusted, role: "producer", roles: ["producer"] }, { requireCompletion: true }), /actor_unauthorized/);
      assert.throws(() => validateClaimAuthority(state, claimId, { ...trusted, run_attempt: 2 }, { requireCompletion: true }), /attempt 1/);
      assert.throws(() => validateClaimAuthority(state, claimId, { ...trusted, claim_handle: "h2" }, { requireCompletion: true }), /run_binding_conflict/);
      assert.throws(() => validateClaimAuthority(state, claimId, { ...trusted, ref: "wrong" }, { requireCompletion: true }), /run_binding_conflict/);
      const forged = { ...trusted, authenticated: false };
      assert.throws(() => validateClaimAuthority(state, claimId, forged, { requireCompletion: true }), /authenticated caller/);
      log.push(finish(log, dispatchId, "h3", "completed"));
      state = replayTransactions(log);
      assert.equal(reservationCounts(state, "default", "").logical, 0);
      assert.equal(reservationCounts(state, "default", "").native, 1);
      const terminal = evidence(state, state.dispatches.get(dispatchId), "terminal_run", 50, { run_id: "200", run_attempt: 1, status: "completed", conclusion: "success" });
      const released = replayTransactions([...log, operationCommit(log, "native-release", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: terminal }], reconciler, 50)]);
      assert.equal(released.works.get(granted.operations[0].work_id).barrier, "pending");
      assert.equal(reservationCounts(released, "default", "").native, 0);
      assert.throws(() => validateClaimAuthority(released, claimId, trusted, { requireCompletion: true }), /claim_ineffective/);
    });
    it("shares scheduling eligibility with explain without conflating native packing capacity", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.native_limit = 1;
      const log = fixture(["a", "b"], policy);
      const granted = grant(log, { max_claims: 1 });
      log.push(granted.commit);
      const state = replayTransactions(log);
      const waiting = log[1].operations[1].work_id;
      assert.equal(explainWork(state, waiting, 3).scheduling.ready, true);
      assert.equal(planNext(state, "default", 3).work_id, waiting);
      assert.equal(planDispatch(state, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }, { requestId: "blocked", commitId: "blocked", at: 3 }).reason, "native_capacity_blocked");
      log.push(operationCommit(log, "pause-grants", "control", [{ kind: "Control", control: "grants_paused", value: true, reason: "operator_pause" }], administrator, 4));
      const paused = replayTransactions(log);
      assert.equal(explainWork(paused, waiting, 4).scheduling.reason, "grants_paused");
      assert.equal(planNext(paused, "default", 4).reason, "grants_paused");
    });
    it("scopes worker queue continuation to its completed original parent and approved pool resources", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.principal = "1003";
      policy.pools.other = structuredClone(policy.pools.default);
      policy.pools.other.allowed_repositories = ["other/repo"];
      policy.producers["1001"].pools.push("other");
      let log = fixture(["parent", "next"], policy);
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      const bound = replayTransactions(log);
      const actor = workerActor(bound, dispatchId, "h1");
      const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
      const child = newWork({ task: "child" }, "children", "child", "default", policy, 40);
      const submit = newRequest("worker-child", "submit", actor, { nodes: [child] });
      const parameters = { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 };
      const request = newRequest("request-worker-dispatch", "dispatch_next", actor, parameters);
      const observation = {
        kind: "Observation",
        observation_id: "worker-observation",
        resource: { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "2" },
        condition: "completed",
        state: "ready",
        observed_at: 40,
        credential_generation: "initial",
        read_status: "ok",
        resource_state: "closed",
        state_reason: "completed",
      };
      const observe = newRequest("worker-observe", "observe", actor, { operations: [observation] });
      for (const intent of [submit, request, observe]) assert.throws(() => validateRequestContext(bound, intent, actor), /must be completed/);
      assert.throws(() => validateWorkerContinuation(bound, trusted), /must be completed/);
      log.push(finish(log, dispatchId, "h1", "completed"));
      const completed = replayTransactions(log);
      for (const intent of [submit, request, observe]) assert.doesNotThrow(() => validateRequestContext(completed, intent, actor));
      assert.equal(Object.hasOwn(policy.producers, actor.principal), false);
      assert.doesNotThrow(() => validateGraphAdmission(completed, [child], actor));
      const childCommit = commit(completed.tip, "worker-child", "submit", actor, { nodes: [child] }, [child], 40);
      assert.equal(replayTransactions([...log, childCommit]).works.get(child.work_id).node_key, "child");
      assert.throws(() => validateGraphAdmission(completed, [child], { ...actor, role: "producer" }), /submission entitlement/);
      assert.throws(() => validateGraphAdmission(bound, [child], actor), /must be completed/);
      for (const changed of [{ principal: "1004" }, { run_id: "201" }, { workflow: ".github/workflows/foreign.lock.yml" }, { claim_handle: "h2" }])
        assert.throws(() => validateGraphAdmission(completed, [child], { ...actor, ...changed }), /run_binding_conflict|claim_scope_invalid/);
      for (const changed of [{ pool: "other" }, { priority: 1 }, { fairness_key: "other" }]) assert.throws(() => validateGraphAdmission(completed, [{ ...child, ...changed }], actor), /work_unauthorized/);
      const failed = structuredClone(completed);
      failed.works.get(completed.claims.get([...completed.claims.keys()][0]).work_id).barrier = "failed";
      assert.throws(() => validateGraphAdmission(failed, [child], actor), /claim_effects_unauthorized/);
      const released = structuredClone(completed);
      released.dispatches.get(dispatchId).released = true;
      assert.throws(() => validateGraphAdmission(released, [child], actor), /reservation has been released/);
      assert.equal(validateWorkerContinuation(completed, trusted).work.node_key, "parent");
      assert.throws(() => validateWorkerContinuation(completed, { ...trusted, ref: "1".repeat(40) }), /run_binding_conflict/);
      assert.throws(() => validateWorkerContinuation(completed, { ...trusted, event: "pull_request" }), /run_binding_conflict/);
      assert.throws(() => validateRequestContext(completed, newRequest("other-pool", "dispatch_next", actor, { ...parameters, pool: "other" }), actor), /parent's pool/);
      const boosted = newRequest("boosted-child", "submit", actor, { nodes: [{ ...child, priority: 1 }] });
      assert.throws(() => validateRequestContext(completed, boosted, actor), /preserve trusted parent/);
      const foreign = { ...observation, resource: { ...observation.resource, repository: "other/repo" } };
      assert.throws(() => validateRequestContext(completed, newRequest("other-gate", "observe", actor, { operations: [foreign] }), actor), /parent's pool/);
      const decision = planDispatchWithObservations(completed, request, actor, 40, "worker-dispatch", [observation]);
      const dispatched = commit(completed.tip, "worker-dispatch", "dispatch_next", actor, parameters, decision.operations, 40);
      assert.equal(replayTransactions([...log, dispatched]).claims.size, 2);
      assert.throws(() => planDispatchWithObservations(completed, request, actor, 40, "foreign-preface", [foreign]), /parent's pool/);
      const observationCommit = operationCommit(log, "worker-observe", "observe", [observation], actor, 40);
      assert.equal(replayTransactions([...log, observationCommit]).observations.size, 1);
    });
    it("requires verified Result rather than Completion, keeps terminal barriers exclusive, and admits safe immutable replacements", () => {
      let log = fixture(["parent", "child"]);
      const child = log[1].operations[1];
      child.depends_on = [{ kind: "work", work_id: log[1].operations[0].work_id }];
      log[1].request = newRequest(log[1].request.id, "submit", producer, { nodes: log[1].operations });
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      const completion = finish(log, dispatchId, "h1", "completed");
      log.push(completion);
      let state = replayTransactions(log);
      assert.equal(planNext(state, "default", 40).reason, "no_eligible_work");
      const result = {
        kind: "Result",
        work_id: granted.operations[0].work_id,
        claim_id: granted.operations[0].claim_id,
        completion_id: completion.id,
        descriptor: { summary: "verified" },
        evidence: evidence(state, state.dispatches.get(dispatchId), "delivery", 50, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: "claim-scoped receipt" }),
      };
      const verified = operationCommit(log, "result", "result", [result], reconciler, 50);
      state = replayTransactions([...log, verified]);
      assert.equal(planNext(state, "default", 50).work_id, child.work_id);
      const source = context(workerActor(state, dispatchId, "h1"), { ref: "0".repeat(40), event: "workflow_dispatch" });
      assert.throws(() => validateClaimAuthority(state, result.claim_id, source, { requireCompletion: true }), /claim_effects_unauthorized/);
      assert.equal(validateWorkerContinuation(state, source).claim.claim_id, result.claim_id);
      const terminal = evidence(state, state.dispatches.get(dispatchId), "terminal_run", 51, { run_id: "200", run_attempt: 1, status: "completed", conclusion: "success" });
      const release = operationCommit([...log, verified], "verified-release", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: terminal }], reconciler, 51);
      assert.throws(() => validateWorkerContinuation(replayTransactions([...log, verified, release]), source), /claim_ineffective/);
      const failure = {
        kind: "DeliveryFailure",
        work_id: result.work_id,
        claim_id: result.claim_id,
        completion_id: result.completion_id,
        reason: "verification_exhausted",
        disposition: "unknown",
        evidence: evidence(state, state.dispatches.get(dispatchId), "terminal_run", 50, { run_id: "200", run_attempt: 1, status: "completed", conclusion: "failure", attempts: 5, effects: "unknown" }),
      };
      const failCommit = operationCommit(log, "failure", "delivery_failure", [failure], reconciler, 50);
      const failed = replayTransactions([...log, failCommit]);
      assert.throws(() => validateWorkerContinuation(failed, source), /claim_effects_unauthorized/);
      assert.throws(() => replayTransactions([...log, verified, { ...failCommit, previous: verified.id }]), /delivery_conflict/);
      assert.throws(() => replayTransactions([...log, failCommit, { ...verified, previous: failCommit.id }]), /delivery_conflict/);
      assert.equal(failed.works.get(result.work_id).state, "completed");
      assert.equal(dependencyStatus(failed, child, 50).reason, "dependency_delivery_failed");
      const replacement = {
        ...newWork({ plan: "inspect uncertain effects; do not repeat writes" }, "graph", "replacement", "default", failed.policy, 60),
        replacement_of: { work_id: result.work_id, disposition: "inspection", evidence: "validated remediation" },
      };
      const replace = commit(failed.tip, "replace", "submit", producer, { nodes: [replacement] }, [replacement], 60);
      const replaced = replayTransactions([...log, failCommit, replace]);
      assert.equal(replaced.works.get(child.work_id).depends_on[0].work_id, result.work_id);
      assert.equal(replaced.works.get(replacement.work_id).position.commit, log.length + 1);
      const cancelled = replayTransactions([
        ...log,
        failCommit,
        replace,
        operationCommit([...log, failCommit, replace], "cancel-replacement", "cancel_work", [{ kind: "WorkCancellation", work_id: replacement.work_id, reason: "operator_cancelled" }], administrator, 61),
      ]);
      assert.equal(cancelled.works.get(replacement.work_id).cancellation_reason, "operator_cancelled");
      assert.equal(cancelled.works.get(replacement.work_id).cancellation_commit_id, "cancel-replacement");
    });
    it("preserves FIFO and debt through bounded retry and never refunds a cancellation", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.retry = { max_attempts: 2, backoff_ms: 10 };
      let log = fixture(["a", "b"], policy);
      const first = grant(log);
      log.push(first.commit);
      const dispatchId = first.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      const position = replayTransactions(log).works.get(first.operations[0].work_id).position;
      log.push(finish(log, dispatchId, "h1", "cancelled", { at: 40 }));
      let state = replayTransactions(log);
      assert.deepEqual(state.works.get(first.operations[0].work_id).position, position);
      assert.equal(planNext(state, "default", 49).work_id, log[1].operations[1].work_id);
      assert.equal(planNext(state, "default", 50).work_id, first.operations[0].work_id);
      const second = grant(log, { id: "retry", at: 50 });
      log.push(second.commit);
      assert.equal(replayTransactions(log).works.get(first.operations[0].work_id).attempts, 2);
      assert.equal(diagnostics(replayTransactions(log).clocks.get("default")).keys["3"].pass[""], "3");
      log = bind(log, second.assignments[0].dispatch_id, { id: "retry-binding", at: 60, runId: "201" });
      log.push(finish(log, second.assignments[0].dispatch_id, "h1", "cancelled", { id: "exhausted", at: 70 }));
      state = replayTransactions(log);
      assert.equal(state.works.get(first.operations[0].work_id).state, "cancelled");
      assert.equal(log.at(-1).operations.at(-1).kind, "WorkCancellation");
    });
    it("never releases uncertain launches or live runs on deadlines/cancellation alone", () => {
      let log = fixture(["a"]);
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      const state = replayTransactions(log);
      const timeout = evidence(state, state.dispatches.get(dispatchId), "reconciliation", 20, { attempts: 5 });
      const release = operationCommit(log, "release", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: timeout }], reconciler, 20);
      assert.throws(() => replayTransactions([...log, release]), /release_invalid/);
      log = bind(log, dispatchId);
      const bound = replayTransactions(log);
      const positive = evidence(bound, bound.dispatches.get(dispatchId), "terminal_run", 50, { run_id: "200", run_attempt: 1, status: "completed", conclusion: "cancelled" });
      const withoutClosing = operationCommit(log, "release-positive", "release", [{ kind: "Release", dispatch_id: dispatchId, evidence: positive }], reconciler, 50);
      assert.throws(() => replayTransactions([...log, withoutClosing]), /still-open/);
      const close = operationCommit(
        log,
        "settle-native",
        "release",
        [
          { kind: "ClaimCancellation", work_id: granted.operations[0].work_id, claim_id: granted.operations[0].claim_id, reason: "terminal_run", retry_not_before: 30050 },
          { kind: "Release", dispatch_id: dispatchId, evidence: positive },
        ],
        reconciler,
        50
      );
      const released = replayTransactions([...log, close]);
      assert.equal(reservationCounts(released, "default", "").native, 0);
      assert.equal(released.claims.size, 1);
      assert.equal(diagnostics(released.clocks.get("default")).keys["3"].pass[""], "2");
    });
    it("anchors delayed cancellation backoff only to the matching positive Release proof", () => {
      let log = fixture(["a", "b"]);
      const granted = grant(log, { max_claims: 2, max_dispatches: 2 });
      log.push(granted.commit);
      const [first, second] = granted.assignments;
      log = bind(log, first.dispatch_id, { id: "first-binding", runId: "201" });
      log = bind(log, second.dispatch_id, { id: "second-binding", runId: "202" });
      const state = replayTransactions(log);
      const operations = granted.operations.flatMap((claim, index) => {
        const at = index ? 1000 : 50;
        return [
          { kind: "ClaimCancellation", work_id: claim.work_id, claim_id: claim.claim_id, reason: "positive_terminal_proof", retry_not_before: at + 30000 },
          {
            kind: "Release",
            dispatch_id: claim.dispatch_id,
            evidence: evidence(state, state.dispatches.get(claim.dispatch_id), "terminal_run", at, { run_id: index ? "202" : "201", run_attempt: 1, status: "completed", conclusion: "cancelled" }),
          },
        ];
      });
      const delayed = operationCommit(log, "delayed-release", "release", operations, reconciler, 60000);
      const released = replayTransactions([...log, delayed]);
      assert.equal(reservationCounts(released, "default", "").native, 0);
      assert.deepEqual(diagnostics(released.clocks.get("default")), diagnostics(state.clocks.get("default")));
      const mixedProof = structuredClone(operations);
      mixedProof[2].retry_not_before = mixedProof[0].retry_not_before;
      assert.throws(() => replayTransactions([...log, operationCommit(log, "mixed-proof", "release", mixedProof, reconciler, 60000)]), /retry_invalid/);
      assert.throws(() => replayTransactions([...log, operationCommit(log, "unanchored", "cancel_claim", [operations[0]], reconciler, 60000)]), /retry_invalid/);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
