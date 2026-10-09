"use strict";

const assert = require("node:assert/strict");
const { canonical, fingerprint } = require("./work_queue_codec.cjs");
const {
  appendCommit,
  causalChain,
  explainWork,
  generateRequestOperations,
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
const { actorFromContext, defaultPolicy, validateTrustedContext } = require("./work_queue_policy.cjs");
const { newChildWork, newWork, dependencyStatus, gateKey, validateGraphAdmission } = require("./work_queue_graph.cjs");
const { diagnostics, planDispatch, planNext, reservationCounts } = require("./work_queue_scheduler.cjs");
const { administrator, bind, commit, context, dispatcher, evidence, finish, genesis, grant, operationCommit, producer, reconciler, submission, workerActor } = require("./work_queue_test_helpers.cjs");

function fixture(names = ["a", "b", "c"], policy = defaultPolicy({ repository: "owner/repo", principal: "1001" })) {
  const log = [genesis(policy)];
  log.push(submission(log, names));
  return log;
}

function registerTests({ describe, it }) {
  describe("closed causal fair queue replay", () => {
    it("bounds producer WorkCancellation by installed pool, priority and accounting entitlements", () => {
      const base = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      const policy = {
        ...base,
        accounting_weights: { "": 1, tenant: 2, other: 3 },
        pools: { ...base.pools, other: structuredClone(base.pools.default) },
        producers: {
          "1001": { pools: ["default"], priorities: [3], fairness_keys: ["tenant"] },
          "1002": { pools: ["default", "other"], priorities: [3, 4], fairness_keys: ["tenant", "other"] },
        },
      };
      const log = [genesis(policy)];
      const allowed = { ...newWork({ task: "allowed" }, "graph", "allowed", "default", policy, 1), fairness_key: "tenant" };
      log.push(commit("genesis", "allowed-submission", "submit", producer, { nodes: [allowed] }, [allowed], 1));
      const outside = [
        { ...newWork({ task: "outside-pool" }, "other-graph", "outside-pool", "other", policy, 2), fairness_key: "tenant" },
        { ...newWork({ task: "outside-priority" }, "graph", "outside-priority", "default", policy, 2), priority: 4, fairness_key: "tenant" },
        { ...newWork({ task: "outside-key" }, "graph", "outside-key", "default", policy, 2), fairness_key: "other" },
      ];
      log.push(commit("allowed-submission", "foreign-submission", "submit", { ...producer, principal: "1002" }, { nodes: outside }, outside, 2));
      const operation = { kind: "WorkCancellation", work_id: allowed.work_id, reason: "producer_cancelled" };
      const accepted = operationCommit(log, "allowed-cancellation", "cancel_work", [operation], producer, 3);
      const state = replayTransactions([...log, accepted]);
      assert.equal(state.works.get(allowed.work_id).state, "cancelled");
      assert.equal(state.claims.size, 0);
      const beforeSelection = canonical(serializeProjection(state));
      assert.equal(planNext(state, "default", 4).work_id, outside[2].work_id);
      const packing = planDispatch(state, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }, { requestId: "cancelled-producer-selection", commitId: "cancelled-producer-claim", at: 4 });
      assert.deepEqual(
        packing.operations.map(operation => operation.work_id),
        [outside[2].work_id]
      );
      assert.equal(canonical(serializeProjection(state)), beforeSelection);
      for (const node of outside) {
        const rejected = operationCommit(log, `reject-${node.node_key}`, "cancel_work", [{ ...operation, work_id: node.work_id }], producer, 3);
        assert.throws(
          () => replayTransactions([...log, rejected]),
          error => error instanceof Error && "code" in error && error.code === "admission_unauthorized",
          node.node_key
        );
      }
      const unregistered = operationCommit(log, "unregistered-cancellation", "cancel_work", [operation], { ...producer, principal: "1003" }, 3);
      assert.throws(
        () => replayTransactions([...log, unregistered]),
        error => error instanceof Error && "code" in error && error.code === "admission_unauthorized"
      );
      assert.ok([...replayTransactions(log).works.values()].every(work => work.state === "available"));
    });
    it("rechecks producer entitlements before identical terminal cancellation no-ops", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.accounting_weights.other = 1;
      policy.producers["1002"] = { pools: ["default"], priorities: [3], fairness_keys: ["other"] };
      const entitled = { ...producer, principal: "1002" };
      const work = { ...newWork({ task: "foreign-account" }, "graph", "foreign-account", "default", policy, 1), fairness_key: "other" };
      const log = [genesis(policy)];
      log.push(commit("genesis", "foreign-admission", "submit", entitled, { nodes: [work] }, [work], 1));
      const operation = { kind: "WorkCancellation", work_id: work.work_id, reason: "producer_cancelled" };
      const terminal = operationCommit(log, "entitled-cancellation", "cancel_work", [operation], entitled, 2);
      log.push(terminal);
      const before = canonical(serializeProjection(replayTransactions(log)));
      const repeated = operationCommit(log, "entitled-terminal-repeat", "cancel_work", [operation], entitled, 3);
      assert.equal(replayTransactions([...log, repeated]).works.get(work.work_id).cancellation_commit_id, terminal.id);
      for (const principal of ["1001", "1003"]) {
        const rejected = operationCommit(log, `foreign-terminal-repeat-${principal}`, "cancel_work", [operation], { ...producer, principal }, 3);
        assert.throws(
          () => replayTransactions([...log, rejected]),
          error => error instanceof Error && "code" in error && error.code === "admission_unauthorized",
          principal
        );
      }
      assert.equal(canonical(serializeProjection(replayTransactions(log))), before);
    });
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
        log.push(commit(log[log.length - 1].id, `long-history-${index}`, "control", administrator, { operations }, operations, index));
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
    it("replays atomic producer and dispatcher genesis with exactly one Policy and the submitted Work", () => {
      const root = genesis();
      const nodes = submission([root], ["a", "b"]).operations;
      for (const actor of [producer, dispatcher]) {
        const bootstrap = commit(null, "bootstrap", "submit", actor, { nodes }, [...root.operations, ...nodes], 1);
        const state = replayTransactions([bootstrap]);
        assert.deepEqual(state.policy, root.operations[0].policy);
        assert.equal(state.policy_epoch, root.policy_epoch);
        assert.deepEqual(
          [...state.works.keys()],
          nodes.map(node => node.work_id)
        );
        assert.equal(state.transactions.length, 1);
        const later = submission([bootstrap], ["c"], { id: "later" });
        assert.equal(replayTransactions([bootstrap, later]).works.size, 3);
      }
    });
    it("rejects malformed, unauthorized and non-genesis producer Policy installation", () => {
      const root = genesis();
      const policy = root.operations[0];
      const nodes = submission([root], ["a"]).operations;
      const bootstrap = commit(null, "bootstrap", "submit", producer, { nodes }, [policy, ...nodes], 1);
      for (const operations of [nodes, [policy], [...nodes, policy], [policy, policy, ...nodes]]) {
        assert.throws(() => replayTransactions([{ ...bootstrap, operations }]), /policy_missing|request_invalid/);
      }
      const mismatched = [{ ...nodes[0], priority: 2 }];
      assert.throws(() => replayTransactions([{ ...bootstrap, request: newRequest(bootstrap.request.id, "submit", producer, { nodes: mismatched }) }]), /request_invalid/);
      const wrongEpoch = { ...bootstrap, policy_epoch: "other" };
      assert.throws(() => replayTransactions([wrongEpoch]), /policy_not_quiescent/);
      const unregistered = { ...producer, principal: "1002" };
      assert.throws(() => replayTransactions([commit(null, "bootstrap", "submit", unregistered, { nodes }, bootstrap.operations, 1)]), /admission_unauthorized/);
      assert.throws(() => replayTransactions([commit(null, "bootstrap", "submit", administrator, { nodes }, bootstrap.operations, 1)]), /policy_missing/);
      assert.throws(() => replayTransactions([root, { ...bootstrap, previous: root.id }]), /policy_not_quiescent/);
      assert.throws(() => replayTransactions([commit(null, "producer-policy", "policy", producer, { operations: [policy] }, [policy], 1)]), /actor_unauthorized|policy_missing/);
    });
    it("appends once to canonical deduplicated history and rejects stale tips and reused commit IDs", () => {
      const log = fixture(["a"]);
      const operations = [{ kind: "Control", control: "grants_paused", value: true, reason: "stress_control" }];
      const candidate = commit(log[1].id, "append-control", "control", administrator, { operations }, operations);
      const appended = appendCommit([...log].reverse().concat(log), candidate);
      assert.deepEqual(appended.transactions, [...log, candidate]);
      assert.deepEqual(serializeProjection(appended.state), serializeProjection(replayTransactions([...log, candidate])));
      assert.throws(() => appendCommit(log, { ...candidate, previous: log[0].id }), { code: "ledger_invalid" });
      assert.throws(() => appendCommit(log, { ...candidate, id: log[0].id }), { code: "ledger_invalid" });
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
    it("requires positive decimal principals on standalone native evidence and run bindings", () => {
      const proof = { kind: "reconciliation", source: "github_api", repository: "owner/repo", workflow: ".github/workflows/worker.lock.yml", ref: "a".repeat(40), principal: "1001", checked_at: 1 };
      const run = { run_id: "200", run_attempt: 1, repository: proof.repository, workflow: proof.workflow, ref: proof.ref, principal: proof.principal, event: "workflow_dispatch" };
      for (const principal of ["1", "9007199254740993", "9".repeat(256)]) {
        assert.doesNotThrow(() => validateEvidence({ ...proof, principal }));
        assert.doesNotThrow(() => validateRunBinding({ ...run, principal }));
      }
      for (const principal of ["login", "0", "01", "-1", "+1", "1.5", "1e3", "\u0661"]) {
        assert.throws(
          () => validateEvidence({ ...proof, principal }),
          error => error instanceof Error && "code" in error && error.code === "evidence_invalid",
          principal
        );
        assert.throws(
          () => validateRunBinding({ ...run, principal }),
          error => error instanceof Error && "code" in error && error.code === "run_binding_conflict",
          principal
        );
      }
      for (const principal of ["", "1\n", "9".repeat(257), 1001, null]) {
        assert.throws(() => validateEvidence({ ...proof, principal }));
        assert.throws(() => validateRunBinding({ ...run, principal }));
      }
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
      const { workflow: omittedWorkflow, ...missingWorkflow } = actor;
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
    it("never reuses a released historical native run and distinguishes exact large run IDs", () => {
      const firstRunId = "9007199254740993";
      const secondRunId = "9007199254740992";
      assert.equal(Number(firstRunId), Number(secondRunId));
      let log = fixture(["first", "second"]);
      const first = grant(log, { id: "first-reservation" });
      log.push(first.commit);
      const firstDispatch = first.assignments[0].dispatch_id;
      log = bind(log, firstDispatch, { id: "first-native", runId: firstRunId });
      log.push(finish(log, firstDispatch, "h1", "completed"));
      let state = replayTransactions(log);
      const claim = state.claims.get(first.operations[0].claim_id);
      log.push(
        operationCommit(
          log,
          "first-native-result",
          "result",
          [
            {
              kind: "Result",
              work_id: claim.work_id,
              claim_id: claim.claim_id,
              completion_id: claim.terminal_commit_id,
              descriptor: {},
              evidence: evidence(state, state.dispatches.get(firstDispatch), "delivery", 50, { source: "verified_receipts", run_id: firstRunId, run_attempt: 1, receipt: "first-native-receipt" }),
            },
          ],
          reconciler,
          50
        )
      );
      state = replayTransactions(log);
      log.push(
        operationCommit(
          log,
          "first-native-release",
          "release",
          [{ kind: "Release", dispatch_id: firstDispatch, evidence: evidence(state, state.dispatches.get(firstDispatch), "terminal_run", 60, { run_id: firstRunId, run_attempt: 1, status: "completed", conclusion: "success" }) }],
          reconciler,
          60
        )
      );
      const second = grant(log, { id: "second-reservation", at: 70 });
      log.push(second.commit);
      const secondDispatch = second.assignments[0].dispatch_id;
      log.push(operationCommit(log, "second-native-start", "dispatch", [{ kind: "Dispatch", dispatch_id: secondDispatch, state: "started", sender: dispatcher }], dispatcher, 79));
      state = replayTransactions(log);
      assert.equal(state.dispatches.get(firstDispatch).released, true);
      assert.equal(state.dispatches.get(firstDispatch).run.run_id, firstRunId);
      const profile = state.dispatches.get(secondDispatch).profile;
      const binding = (run_id, id) =>
        operationCommit(
          log,
          id,
          "dispatch",
          [
            {
              kind: "Dispatch",
              dispatch_id: secondDispatch,
              state: "bound",
              run: { run_id, run_attempt: 1, repository: "owner/repo", workflow: profile.workflow, ref: profile.ref, principal: profile.principal, event: "workflow_dispatch" },
              evidence: evidence(state, state.dispatches.get(secondDispatch), "reconciliation", 80, { run_id, run_attempt: 1 }),
            },
          ],
          reconciler,
          80
        );
      const unchangedLog = serializeTransactionLog(log);
      const unchangedProjection = serializeProjection(state);
      const reused = binding(firstRunId, "reused-native-binding");
      for (const records of [[...log, reused], [...log, reused].reverse()])
        assert.throws(
          () => replayTransactions(records),
          error => error instanceof Error && "code" in error && error.code === "run_binding_conflict" && error.message.includes("native run is already bound to another assignment")
        );
      assert.equal(serializeTransactionLog(log), unchangedLog);
      assert.deepEqual(serializeProjection(replayTransactions(log)), unchangedProjection);
      const distinct = binding(secondRunId, "distinct-native-binding");
      const accepted = replayTransactions([...log, distinct]);
      assert.equal(accepted.dispatches.get(firstDispatch).run.run_id, firstRunId);
      assert.equal(accepted.dispatches.get(secondDispatch).run.run_id, secondRunId);
      assert.equal(reservationCounts(accepted, "default", "").native, 1);
      assert.deepEqual(accepted.clocks, state.clocks);
      assert.deepEqual(serializeProjection(replayTransactions([...log, distinct].reverse())), serializeProjection(accepted));
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
          error => error instanceof Error && "code" in error && error.code === "cancellation_conflict"
        );
      const workOperation = { kind: "WorkCancellation", work_id: claimOperation.work_id, reason: "operator_cancelled" };
      const terminal = operationCommit(log, "terminal-work-cancellation", "cancel_work", [workOperation], reconciler, 50);
      log.push(terminal);
      const duplicateWork = operationCommit(log, "same-work-cancellation", "cancel_work", [workOperation], reconciler, 60);
      assert.equal(replayTransactions([...log, duplicateWork]).works.get(workOperation.work_id).cancellation_commit_id, terminal.id);
      assert.throws(
        () => replayTransactions([...log, operationCommit(log, "changed-work-cancellation", "cancel_work", [{ ...workOperation, reason: "different_reason" }], reconciler, 60)]),
        error => error instanceof Error && "code" in error && error.code === "cancellation_conflict"
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
      for (const intent of [submit, request, observe]) assert.throws(() => validateRequestContext(bound, intent, actor), { code: "claim_effects_unauthorized" });
      assert.throws(() => validateWorkerContinuation(bound, trusted), { code: "claim_effects_unauthorized" });
      log.push(finish(log, dispatchId, "h1", "completed"));
      const completed = replayTransactions(log);
      for (const intent of [submit, request, observe]) assert.doesNotThrow(() => validateRequestContext(completed, intent, actor));
      assert.equal(Object.hasOwn(policy.producers, actor.principal), false);
      assert.doesNotThrow(() => validateGraphAdmission(completed, [child], actor));
      const childCommit = commit(completed.tip, "worker-child", "submit", actor, { nodes: [child] }, [child], 40);
      assert.equal(replayTransactions([...log, childCommit]).works.get(child.work_id).node_key, "child");
      assert.throws(() => validateGraphAdmission(completed, [child], { ...actor, role: "producer" }), /submission entitlement/);
      assert.throws(() => validateGraphAdmission(bound, [child], actor), { code: "claim_effects_unauthorized" });
      for (const changed of [{ principal: "1004" }, { run_id: "201" }, { workflow: ".github/workflows/foreign.lock.yml" }, { claim_handle: "h2" }])
        assert.throws(() => validateGraphAdmission(completed, [child], { ...actor, ...changed }), /run_binding_conflict|claim_scope_invalid/);
      for (const changed of [{ pool: "other" }, { priority: 1 }, { fairness_key: "other" }]) assert.throws(() => validateGraphAdmission(completed, [{ ...child, ...changed }], actor), { code: "child_entitlement" });
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
    it("retains producer 11's frozen tenant/priority entitlement without impersonation by bound worker 22", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "11" });
      policy.accounting_weights.tenant = 7;
      policy.producers["11"] = { pools: ["default"], priorities: [1], fairness_keys: ["tenant"] };
      policy.pools.default.profiles.default.principal = "22";
      policy.pools.other = structuredClone(policy.pools.default);
      const origin = { role: "producer", principal: "11", repository: "owner/repo" };
      const parent = { ...newWork({ task: "parent" }, "lineage", "parent", "default", policy, 1), priority: 1, fairness_key: "tenant" };
      let log = [genesis(policy)];
      log.push(commit("genesis", "producer-parent", "submit", origin, { nodes: [parent] }, [parent], 1));
      const granted = grant(log);
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      const bound = replayTransactions(log);
      const actor = workerActor(bound, dispatchId);
      const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
      const child = {
        ...newWork({ task: "child" }, "lineage", "child", parent.pool, policy, 40),
        priority: parent.priority,
        fairness_key: parent.fairness_key,
        depends_on: [{ kind: "work", work_id: parent.work_id }],
      };
      const request = newRequest("worker-22-child", "submit", actor, { nodes: [child] });
      assert.equal(actor.principal, "22");
      assert.equal(policy.producers["22"], undefined);
      assert.throws(() => newChildWork(bound, actor, child.payload, child.node_key, child.enqueued), { code: "claim_effects_unauthorized" });
      assert.throws(() => validateRequestContext(bound, request, actor), { code: "claim_effects_unauthorized" });
      log.push(finish(log, dispatchId, "h1", "completed"));
      const completed = replayTransactions(log);
      const claim = completed.claims.get(completed.works.get(parent.work_id).claim_id);
      const dispatch = completed.dispatches.get(dispatchId);
      log.push(
        operationCommit(
          log,
          "verified-parent",
          "result",
          [
            {
              kind: "Result",
              work_id: parent.work_id,
              claim_id: claim.claim_id,
              completion_id: claim.terminal_commit_id,
              descriptor: {},
              evidence: evidence(completed, dispatch, "delivery", 45, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: "trusted-parent" }),
            },
          ],
          reconciler,
          45
        )
      );
      for (const prefix of [log.slice(0, -1), log]) {
        const state = replayTransactions(prefix);
        if (state.works.get(parent.work_id).barrier === "verified") {
          for (const validate of [
            () => validateClaimAuthority(state, claim.claim_id, trusted, { requireCompletion: true }),
            () => newChildWork(state, actor, child.payload, child.node_key, child.enqueued),
            () => validateWorkerContinuation(state, trusted),
            () => validateRequestContext(state, request, actor),
            () => validateGraphAdmission(state, [child], actor),
          ])
            assert.throws(validate, { code: "claim_effects_unauthorized" });
          continue;
        }
        assert.deepEqual({ ...newChildWork(state, actor, child.payload, child.node_key, child.enqueued), depends_on: child.depends_on }, child);
        assert.doesNotThrow(() => validateWorkerContinuation(state, trusted));
        assert.doesNotThrow(() => validateRequestContext(state, request, actor));
        assert.doesNotThrow(() => validateGraphAdmission(state, [child], actor));
        const admitted = commit(state.tip, "worker-child-22", "submit", actor, { nodes: [child] }, [child], 50);
        assert.deepEqual(generateRequestOperations(state, request, actor, 50, admitted.id).operations, [child]);
        const replayed = replayTransactions([...prefix, admitted]);
        assert.deepEqual(replayed.requests.get(admitted.request.id).actor, actor);
        assert.deepEqual(replayed.transactions[replayed.works.get(parent.work_id).position.commit].actor, origin);
        assert.deepEqual(serializeProjection(replayTransactions([...prefix, admitted].reverse())), serializeProjection(replayed));
        assert.equal(replayed.works.get(child.work_id).fairness_key, "tenant");
        assert.equal(replayed.works.get(child.work_id).priority, 1);
        for (const changed of [{ principal: "11" }, { run_id: "201" }, { run_attempt: 2 }, { claim_handle: "h2" }]) {
          const spoofed = { ...actor, ...changed };
          assert.throws(() => newChildWork(state, spoofed, child.payload, child.node_key, child.enqueued));
          assert.throws(() => validateRequestContext(state, newRequest("spoofed-child", "submit", spoofed, { nodes: [child] }), spoofed));
        }
        for (const changed of [{ role: "producer" }, { role: "administrator" }, { principal: "11", role: "producer" }]) assert.throws(() => validateTrustedContext(trusted, { ...actor, ...changed }), /request origin differs/);
        assert.deepEqual(actorFromContext({ ...trusted, logical_origin: origin }), actor);
        assert.throws(() => actorFromContext({ ...trusted, role: "producer" }), /approved role/);
        assert.throws(() => newChildWork(state, { ...actor, logical_origin: origin }, child.payload, child.node_key, child.enqueued));
        for (const changed of [{ pool: "other" }, { priority: 5 }, { fairness_key: "" }, { worker_profile: "agent-admin" }, { logical_origin: origin }])
          assert.throws(() => validateRequestContext(state, newRequest("escalated-child", "submit", actor, { nodes: [{ ...child, ...changed }] }), actor));
        const resource = { kind: "issue", host: "github.com", repository: "foreign/repo", repository_id: "9", resource_id: "10", number: "1" };
        assert.throws(() => validateRequestContext(state, newRequest("foreign-resource-child", "submit", actor, { nodes: [{ ...child, subject: resource }] }), actor), /not allowlisted/);
        assert.throws(() => validateRequestContext(state, newRequest("broader-dispatch", "dispatch_next", actor, { pool: "other", max_claims: 1, max_dispatches: 1, max_bytes: 49152 }), actor), /parent's pool/);
        assert.throws(() => validateRequestContext(state, newRequest("worker-policy", "policy", actor, { operations: [{ kind: "Policy", epoch: "spoofed", policy }] }), actor), /cannot publish policy/);
      }
    });
    it("closes fresh scoped queue controls after Result without closing a delivery-pending sibling", () => {
      const policy = defaultPolicy({ repository: "owner/repo", principal: "1001" });
      policy.pools.default.profiles.default.max_claims = 2;
      let log = fixture(["parent", "sibling", "next"], policy);
      const granted = grant(log, { max_claims: 2 });
      log.push(granted.commit);
      const dispatchId = granted.assignments[0].dispatch_id;
      log = bind(log, dispatchId);
      log.push(finish(log, dispatchId, "h1", "completed"));
      log.push(finish(log, dispatchId, "h2", "completed"));
      const pending = replayTransactions(log);
      const first = pending.claims.get(granted.operations[0].claim_id);
      const result = {
        kind: "Result",
        work_id: first.work_id,
        claim_id: first.claim_id,
        completion_id: first.terminal_commit_id,
        descriptor: { summary: "all scoped effects verified" },
        evidence: evidence(pending, pending.dispatches.get(dispatchId), "delivery", 65, { source: "verified_receipts", run_id: "200", run_attempt: 1, receipt: "complete-control-inventory" }),
      };
      const verified = operationCommit(log, "closed-control-result", "result", [result], reconciler, 65);
      const closedLog = [...log, verified];
      const closed = replayTransactions(closedLog);
      const before = canonical(serializeProjection(closed));
      for (const handle of ["h1", "h2"]) {
        const actor = workerActor(pending, dispatchId, handle);
        const child = newChildWork(pending, actor, { task: `child-${handle}` }, `child-${handle}`, 60);
        const observation = {
          kind: "Observation",
          observation_id: `control-observation-${handle}`,
          resource: { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "2", number: "2" },
          condition: "completed",
          state: "ready",
          observed_at: 70,
          credential_generation: "initial",
          read_status: "ok",
          resource_state: "closed",
          state_reason: "completed",
        };
        for (const [kind, parameters] of Object.entries({
          submit: { nodes: [child] },
          dispatch_next: { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 },
          observe: { operations: [observation] },
        })) {
          const id = `worker-control-${handle}-${kind}`;
          const request = newRequest(`request-${id}`, kind, actor, parameters);
          const decision = generateRequestOperations(pending, request, actor, 70, id);
          assert.equal(decision.operations.length, 1);
          const candidate = commit(closed.tip, id, kind, actor, parameters, decision.operations, 70);
          const trusted = context(actor, { ref: "0".repeat(40), event: "workflow_dispatch" });
          if (handle === "h1") {
            for (const validate of [
              () => validateRequestContext(closed, request, actor),
              () => generateRequestOperations(closed, request, actor, 70, id),
              () => replayTransactions([...closedLog, candidate]),
              () => validateWorkerContinuation(closed, trusted),
              () => validateClaimAuthority(closed, first.claim_id, trusted, { requireCompletion: true }),
            ])
              assert.throws(validate, { code: "claim_effects_unauthorized" });
            if (kind === "dispatch_next") assert.throws(() => planDispatchWithObservations(closed, request, actor, 70, id, []), { code: "claim_effects_unauthorized" });
          } else {
            assert.doesNotThrow(() => validateRequestContext(closed, request, actor));
            assert.equal(generateRequestOperations(closed, request, actor, 70, id).operations.length, 1);
            assert.equal(replayTransactions([...closedLog, candidate]).requests.has(request.id), true);
            if (kind === "dispatch_next") assert.equal(planDispatchWithObservations(closed, request, actor, 70, id, []).operations.length, 1);
            assert.doesNotThrow(() => validateWorkerContinuation(closed, trusted));
            assert.doesNotThrow(() => validateClaimAuthority(closed, pending.dispatches.get(dispatchId).claims[1].claim_id, trusted, { requireCompletion: true }));
          }
          assert.equal(canonical(serializeProjection(closed)), before);
        }
      }
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
      assert.throws(() => validateWorkerContinuation(state, source), { code: "claim_effects_unauthorized" });
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
      const exhausted = log[log.length - 1].operations;
      assert.equal(exhausted[exhausted.length - 1].kind, "WorkCancellation");
      const prefix = log.slice(0, -1);
      const wrongReason = structuredClone(log[log.length - 1]);
      wrongReason.operations[wrongReason.operations.length - 1].reason = "operator_cancelled";
      assert.throws(() => replayTransactions([...prefix, wrongReason]), /request_invalid|work_unauthorized/);
      const siblingClosure = structuredClone(log[log.length - 1]);
      siblingClosure.operations[siblingClosure.operations.length - 1].work_id = log[1].operations[1].work_id;
      assert.throws(() => replayTransactions([...prefix, siblingClosure]), /request_invalid|work_unauthorized/);
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
      const exact = structuredClone(operations);
      assert.ok(exact[1].evidence);
      assert.ok(exact[3].evidence);
      exact[0].retry_not_before = 34000;
      exact[1].evidence.checked_at = 4000;
      exact[2].retry_not_before = 39000;
      exact[3].evidence.checked_at = 9000;
      const exactState = replayTransactions([...log, operationCommit(log, "exact-proof-backoff", "release", exact, reconciler, 50000)]);
      assert.equal(exactState.works.get(granted.operations[0].work_id).retry_not_before, 34000);
      assert.equal(exactState.works.get(granted.operations[1].work_id).retry_not_before, 39000);
      assert.deepEqual(diagnostics(exactState.clocks.get("default")), diagnostics(state.clocks.get("default")));
      const earlySecond = structuredClone(exact);
      earlySecond[2].retry_not_before = 38000;
      assert.throws(() => replayTransactions([...log, operationCommit(log, "early-second-proof", "release", earlySecond, reconciler, 50000)]), /retry_invalid/);
      const absentSecond = structuredClone(exact.slice(0, 3));
      assert.throws(() => replayTransactions([...log, operationCommit(log, "absent-second-proof", "release", absentSecond, reconciler, 50000)]), /retry_invalid/);
      absentSecond[2].retry_not_before = 80000;
      const unanchoredSecond = replayTransactions([...log, operationCommit(log, "decision-time-second", "release", absentSecond, reconciler, 50000)]);
      assert.equal(unanchoredSecond.works.get(granted.operations[1].work_id).retry_not_before, 80000);
      assert.equal(reservationCounts(unanchoredSecond, "default", "").native, 1);
      const foreignSecond = structuredClone(exact);
      assert.ok(foreignSecond[3].evidence);
      foreignSecond[3].evidence.run_id = "201";
      assert.throws(() => replayTransactions([...log, operationCommit(log, "foreign-second-proof", "release", foreignSecond, reconciler, 50000)]), /evidence_invalid/);
    });
  });
}

if (require.main === module) registerTests(require("node:test"));
module.exports = { registerTests };
