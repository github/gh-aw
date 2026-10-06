// @ts-check
"use strict";

const queue = require("./work_queue_replay.cjs");
const store = require("./work_queue_store.cjs");
const { canonical, digest, parseStrictJSON } = require("./work_queue_codec.cjs");
const { normalizeAssignment } = require("./work_queue_claim_scope.cjs");
const { actorFromContext, validatePolicy, validateTrustedContext } = require("./work_queue_policy.cjs");
const { requestForIntent } = require("./work_queue_intents.cjs");
const { authenticatePublisher, validateNativeRun } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

function policyProposalFor(options) {
  const raw = Object.hasOwn(options, "policyProposal") ? options.policyProposal : process.env.GH_AW_WORK_QUEUE_POLICY;
  if (raw === undefined) return;
  if (typeof raw === "string" && Buffer.byteLength(raw, "utf8") > 4 * 1024 * 1024) throw new Error("work_queue_policy_proposal_limit");
  const proposal = typeof raw === "string" ? parseStrictJSON(raw) : raw;
  validatePolicy(proposal);
  return proposal;
}

function assertPolicyProposal(state, options) {
  const proposal = policyProposalFor(options);
  if (proposal === undefined) return;
  if (!state.policy) throw new Error("work_queue_policy_missing");
  if (canonical(proposal) !== canonical(state.policy)) throw new Error("work_queue_policy_proposal_mismatch");
}

async function loadQueue(options) {
  const context = options.context;
  const readLog = options.readWorkQueueLog || store.readWorkQueueLog;
  const parameters = {
    githubClient: options.queueClient || options.githubClient,
    owner: context.repo.owner,
    repo: context.repo.repo,
    ...(options.branch === undefined ? {} : { branch: options.branch }),
    core: options.core,
  };
  const readBoundLog = async () => {
    const log = await readLog(parameters);
    const repository = `${context.repo.owner}/${context.repo.repo}`.toLowerCase();
    if (!Array.isArray(log.transactions) || log.transactions.some(commit => typeof commit?.actor?.repository !== "string" || commit.actor.repository.toLowerCase() !== repository)) throw new Error("work_queue_ledger_repository_mismatch");
    return log;
  };
  let log = await readBoundLog();
  let projection = log.state || queue.replayTransactions(log.transactions);
  if (!projection.policy && options.initializationContext !== undefined) {
    if (log.sha !== null || !Array.isArray(log.transactions) || log.transactions.length) throw new Error("work_queue_genesis_not_absent");
    if (isStagedMode(options) || isStagedMode(options.config)) throw new Error("work_queue_policy_missing");
    const approved = options.initializationContext;
    const administrator = actorFromContext(approved);
    validateTrustedContext(approved, administrator);
    if (administrator.role !== "administrator") throw new Error("work_queue_initialization_not_authorized");
    const actual = await authenticatePublisher({ ...options, role: "administrator" });
    if (["principal", "repository", "workflow", "run_id", "run_attempt", "ref", "event"].some(field => approved[field] !== undefined && approved[field] !== actual[field])) throw new Error("work_queue_initialization_native_mismatch");
    const proposal = policyProposalFor(options);
    if (proposal === undefined) throw new Error("work_queue_policy_proposal_required");
    await (options.initializeWorkQueue || store.initializeWorkQueue)({ ...parameters, context: actual, policyProposal: proposal });
    log = await readBoundLog();
    projection = log.state || queue.replayTransactions(log.transactions);
    if (!projection.policy) throw new Error("work_queue_policy_missing");
  }
  assertPolicyProposal(projection, options);
  return { ...log, projection };
}

async function publishOperations(options, trustedContext, purpose, kind, operations, validatePrefix) {
  const request = requestForIntent(trustedContext, `intent:${digest(purpose)}`, kind, { operations });
  return (options.publishWorkQueueRequest || store.publishWorkQueueRequest)({
    githubClient: options.queueClient || options.githubClient,
    owner: options.context.repo.owner,
    repo: options.context.repo.repo,
    ...(options.branch === undefined ? {} : { branch: options.branch }),
    context: trustedContext,
    actor: actorFromContext(trustedContext),
    request,
    ...(validatePrefix === undefined
      ? {}
      : {
          generateOperations: (state, stable, actor, at, id, observations) => {
            validatePrefix(state);
            return queue.generateRequestOperations(state, stable, actor, at, id, observations);
          },
        }),
    core: options.core,
  });
}

function validateStoredAssignment(state, supplied, { allowReleased = false } = {}) {
  const assignment = normalizeAssignment(supplied);
  const dispatch = state.dispatches.get(assignment.dispatch_id);
  const stored = dispatch && queue.assignmentForDispatch(state, assignment.dispatch_id);
  if (!stored || canonical(stored) !== canonical(assignment)) throw new Error("work_queue_assignment_mismatch");
  const retired = state.policy_epoch !== assignment.policy_epoch;
  if (retired && !(allowReleased && dispatch.released)) throw new Error("work_queue_assignment_policy_mismatch");
  if (!dispatch || (dispatch.released && !allowReleased)) throw new Error("work_queue_dispatch_not_active");
  const profile = dispatch.profile;
  if (!profile) throw new Error("work_queue_assignment_profile_missing");
  return { assignment: stored, dispatch, profile };
}

function expectedWorkerRun(assignment, profile, context) {
  return {
    repository: `${context.repo.owner}/${context.repo.repo}`,
    ...(context.payload?.repository?.id === undefined ? {} : { repository_id: context.payload.repository.id }),
    workflow: profile.workflow,
    ref: profile.ref,
    principal_id: profile.principal,
    dispatch_id: assignment.dispatch_id,
  };
}

function bindingForRun(proof, expected) {
  return { run_id: proof.run_id, run_attempt: 1, repository: expected.repository, workflow: expected.workflow, ref: expected.ref, principal: expected.principal_id, event: "workflow_dispatch" };
}

async function bindWorkerAssignment(options) {
  const initial = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(initial.projection, options.assignment);
  if (!["started", "uncertain", "unresolved", "bound"].includes(dispatch.state)) throw new Error("work_queue_launch_marker_required");
  const trustedContext = await authenticatePublisher({ ...options, role: "worker", dispatch_id: assignment.dispatch_id });
  const expected = expectedWorkerRun(assignment, profile, options.context);
  const proof = validateNativeRun(trustedContext.native_run, { ...expected, run_id: trustedContext.run_id });
  const binding = bindingForRun(proof, expected);
  if (dispatch.run && canonical(dispatch.run) !== canonical(binding)) throw new Error("run_binding_conflict");
  if (!dispatch.run) {
    const evidence = {
      kind: "reconciliation",
      source: "trusted_activation",
      repository: binding.repository,
      workflow: binding.workflow,
      ref: binding.ref,
      principal: binding.principal,
      checked_at: options.now ?? Date.now(),
      run_id: binding.run_id,
      run_attempt: 1,
    };
    await publishOperations(options, trustedContext, ["bind", assignment.dispatch_id, proof.run_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "bound", run: binding, evidence }]);
  }
  const latest = await loadQueue(options);
  const validated = validateStoredAssignment(latest.projection, assignment);
  if (!validated.dispatch.run || canonical(validated.dispatch.run) !== canonical(binding)) throw new Error("work_queue_binding_not_durable");
  return { assignment, binding, trustedContext, projection: latest.projection };
}

module.exports = { policyProposalFor, assertPolicyProposal, loadQueue, publishOperations, validateStoredAssignment, expectedWorkerRun, bindingForRun, bindWorkerAssignment };
