// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("dispatch");

const { canonical, closed, digest, integer, queueError } = require("./work_queue_codec.cjs");
const { actorFromContext, dispatchPrincipal } = require("./work_queue_policy.cjs");
const { gateKey } = require("./work_queue_graph.cjs");
const { observationRefreshBudget } = require("./work_queue_limits.cjs");
const { assignmentsForRequest, generateRequestOperations, validateWorkerContinuation } = require("./work_queue_replay.cjs");
const store = require("./work_queue_store.cjs");
const { normalizeAssignment, normalizeClaimScope, readClaimScopeContext } = require("./work_queue_claim_scope.cjs");
const { readStagedIntentBatch, requestIdForIntent, requestForIntent, normalizeDispatchParameters, normalizeSubmitParameters } = require("./work_queue_intents.cjs");
const { assertPolicyProposal, loadQueue, policyProposalFor, publishOperations, validateStoredAssignment, expectedWorkerRun, bindingForRun } = require("./work_queue_binding.cjs");
const { authenticatePublisher, authenticateIntentPublisher, immutableRef, fetchNativeRun, postQueueDispatch, validateNativeRun } = require("./work_queue_native.cjs");
const { dependencyKey, allowedScope, resolveDependencies, resolveExternalEdges } = require("./work_queue_dependency_resolver.cjs");
const { lifecycleEvidence, reconcileDispatch, reconcileQueue, releaseAssignment } = require("./work_queue_reconciler.cjs");
const { DEFAULT_INTENT_PATH } = require("./work_queue_mcp_server.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { controlReceiptForRequest } = require("./work_queue_control_receipts.cjs");
const { normalizeDispatchCredential, createDispatchCredentialValidator, isDispatchCredentialProof } = require("./work_queue_dispatch_credential.cjs");
const { verifyWorkerRoute } = require("./work_queue_provisioning.cjs");
const { futurePolicy } = require("./work_queue_deployment.cjs");
const { approvedWorkerProfiles, synchronizeDeployments } = require("./work_queue_deployment_control.cjs");

function assertQueueControlRole(options) {
  const runtime = resolveWorkQueueRuntime(options.context?.payload, { role: options.role, requireAssignment: options.requireAssignment });
  if (runtime.role === "observer") throw new Error(`${SAFE_OUTPUT_E001}: work_queue_observer_read_only`);
  return runtime;
}

function normalizeControlIntentScope(options, intent, runtime = assertQueueControlRole(options)) {
  const scope = options.assignment === undefined ? runtime.assignment || readClaimScopeContext()?.assignment : options.assignment;
  if (!scope) {
    if (runtime.role === "worker") throw new Error("work_queue_assignment_required");
    if (Object.hasOwn(intent, "claim_handle")) throw new Error("work_queue_claim_scope_invalid");
    return null;
  }
  const normalized = normalizeClaimScope({ ...(Object.hasOwn(intent, "claim_handle") ? { claim_handle: intent.claim_handle } : {}) }, scope);
  if (runtime.assignment && canonical(normalizeAssignment(scope)) !== canonical(runtime.assignment)) throw new Error("work_queue_assignment_mismatch");
  return normalized;
}

function approvedProfile(profile, config) {
  const names = new Set(config.work_queue_workflows || []);
  const contexts = new Set(config.aw_context_workflows || []);
  immutableRef(profile.ref);
  const name = profile.workflow.replace(/^\.github\/workflows\//, "").replace(/\.(?:lock\.yml|yml|yaml)$/, "");
  if (!names.has(name) || !contexts.has(name)) throw new Error("work_queue_worker_not_compiler_approved");
}

function approvedAssignments(state, assignments, config) {
  for (const assignment of assignments) {
    const profile = state.dispatches.get(assignment.dispatch_id)?.profile ?? futurePolicy(state).pools[assignment.pool]?.profiles[assignment.worker_profile];
    if (!profile) throw new Error("work_queue_assignment_profile_missing");
    approvedProfile(profile, config);
  }
}

function approvedSubmissionParameters(state, trustedContext, parameters, config) {
  if (state.policy.authorization !== "aw") return parameters;
  closed(parameters, ["nodes"], [], "submit intent");
  if (!Array.isArray(parameters.nodes)) throw queueError("work_queue_graph_limit", "submission requires a bounded node array");
  const parent = trustedContext.role === "worker" ? validateWorkerContinuation(state, trustedContext).work : undefined;
  const names = new Set(config.work_queue_workflows || []);
  const contexts = new Set(config.aw_context_workflows || []);
  return {
    nodes: parameters.nodes.map(node => {
      const poolName = node.pool === undefined ? "default" : node.pool;
      const pool = state.policy.pools[poolName];
      if (!pool) throw queueError("work_queue_profile_not_approved", "submission pool has no installed worker routes");
      const ownProfile = parent?.pool === poolName ? parent.worker_profile : undefined;
      let profileName = node.worker_profile;
      if (profileName === undefined) {
        profileName =
          ownProfile ??
          Object.keys(pool.profiles)
            .sort()
            .find(name => {
              const workflow = pool.profiles[name].workflow.replace(/^\.github\/workflows\//, "").replace(/\.lock\.yml$/, "");
              return names.has(workflow) && contexts.has(workflow);
            });
      }
      const profile = pool.profiles[profileName];
      if (!profile) throw queueError("work_queue_profile_not_approved", "submission requires a caller-approved worker route");
      if (profileName !== ownProfile) approvedProfile(profile, config);
      return { ...node, worker_profile: profileName };
    }),
  };
}

function staleEdges(state, pool, at) {
  const policy = state.policy.pools[pool];
  const edges = new Map();
  for (const work of state.works.values()) {
    if (work.pool !== pool || work.state !== "available" || work.retry_not_before > at || work.depends_on.some(edge => edge.kind === "work" && state.works.get(edge.work_id)?.barrier !== "verified")) continue;
    for (const edge of work.depends_on) {
      if (edge.kind === "work") continue;
      const key = gateKey(edge.resource, edge.condition);
      const observation = state.observations.get(key);
      if (!observation || observation.credential_generation !== state.credential_generation || observation.observed_at > at || at - observation.observed_at > policy.max_observation_age_ms) edges.set(key, edge);
    }
  }
  return [...edges.values()];
}

async function refreshDependencies(options, state, pool, trustedContext) {
  log.debug("dependencies.refresh.start");
  if (trustedContext.role === "worker") {
    if (validateWorkerContinuation(state, trustedContext).work.pool !== pool) throw new Error("work_queue_control_pool_not_authorized");
    return [];
  } else if (!["dispatcher", "reconciler", "administrator"].includes(trustedContext.role)) {
    throw new Error("work_queue_observation_role_required");
  }
  const budget = observationRefreshBudget(state);
  if (budget === 0) return [];
  const at = options.now ?? Date.now();
  const edges = staleEdges(state, pool, at).slice(0, budget);
  log.debug("dependencies.refresh.budget", { budget, stale_edges: edges.length });
  if (!edges.length) return [];
  const operations = [];
  const readable = [];
  const resolver = options.dependencyResolver;
  for (const edge of edges) {
    let readStatus = "external_read_credentials_missing";
    if (resolver) {
      try {
        const scope = allowedScope({ ...edge.resource, condition: edge.condition }, resolver.scopes);
        if (scope.access_generation === state.credential_generation) {
          readable.push(edge);
          continue;
        }
        readStatus = "external_access_generation_mismatch";
      } catch (error) {
        if (error?.message !== "external_not_allowlisted") throw error;
        readStatus = "external_not_allowlisted";
      }
    }
    const operation = { kind: "Observation", resource: edge.resource, condition: edge.condition, state: "unknown", observed_at: at, credential_generation: state.credential_generation, read_status: readStatus };
    operations.push({ ...operation, observation_id: `observation:${digest(operation)}` });
  }
  if (readable.length) operations.push(...(await resolveExternalEdges(readable, { ...resolver, now: at, maxResources: 128, maxReads: 256 })).operations);
  log.debug("dependencies.refresh.complete", { readable: readable.length, observations: operations.length });
  return operations;
}

async function resolveAdmissionResources(options, state, parameters, trustedContext) {
  log.debug("admission.resolve.start");
  assertRemediationBackend(state, parameters, options.remediationVerifier);
  const references = [];
  for (const node of parameters.nodes) {
    for (const edge of node.depends_on) {
      if (edge.kind === "work") continue;
      if (edge.kind !== edge.resource?.kind) throw new Error("external_resource_type_mismatch");
      references.push({ target: edge, field: "resource", resource: { ...edge.resource, condition: edge.condition } });
    }
    if (node.subject) references.push({ target: node, field: "subject", resource: { ...node.subject, condition: node.subject.kind === "issue" ? "completed" : "merged" } });
    if (node.backing_issue) references.push({ target: node, field: "backing_issue", resource: { ...node.backing_issue, condition: "completed" } });
  }
  if (!references.length) return parameters;
  log.debug("admission.resolve.references", { references: references.length });
  const resolver = options.dependencyResolver;
  if (!resolver || !resolver.scopes.every(scope => scope.access_generation === state.credential_generation)) throw new Error("external_read_credentials_missing");
  const at = options.now ?? Date.now();
  const resolved = await resolveDependencies(
    references.map(reference => reference.resource),
    { ...resolver, now: at, maxResources: 128, maxReads: 256 }
  );
  for (const observation of resolved.observations) {
    if (
      !["ok", "external_reopened", "external_closed_not_completed", "external_closed_unmerged", "external_predicate_unknown", "external_merge_evidence_missing"].includes(observation.read_status) ||
      !observation.resource.repository_id ||
      !observation.resource.resource_id
    )
      throw new Error("external_admission_unverified");
  }
  for (const reference of references) {
    const verified = resolved.bindings.get(dependencyKey(reference.resource));
    if (!verified) throw new Error("external_admission_identity_mismatch");
    const { condition, ...identity } = verified;
    reference.target[reference.field] = identity;
  }
  log.debug("admission.resolve.complete", { observations: resolved.observations.length });
  return parameters;
}

function assertRemediationBackend(state, parameters, verifier) {
  for (const node of parameters.nodes) {
    if (!node.replacement_of) continue;
    const failed = state.works.get(node.replacement_of.work_id);
    if (failed?.barrier === "failed" && failed.disposition !== "none" && typeof verifier !== "function") throw new Error("work_queue_trusted_remediation_required");
  }
}

function rejectionReceipt(error, destination) {
  const status = error?.response?.status;
  const requestId = error?.response?.headers?.["x-github-request-id"];
  if (![400, 401, 403, 404, 422, 429].includes(status) || error.status !== status || typeof requestId !== "string" || !requestId) return null;
  return `github_rejection:${digest({ status, request_id: requestId, repository: destination.repository, workflow: destination.workflow, ref: destination.ref })}`;
}

async function launchAssignment(options, supplied) {
  log.debug("launch.start");
  assertQueueControlRole(options);
  let latest = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(latest.projection, supplied, { allowReleased: true });
  log.debug("launch.assignment.checked", { claims: assignment.claims.length, released: dispatch.released, reserved: dispatch.state === "reserved", staged: isStagedMode(options) || isStagedMode(options.config) });
  if (isStagedMode(options) || isStagedMode(options.config)) return { state: "staged_preview", released: false, dispatched: false };
  if (dispatch.released) return { state: "released", released: true, dispatched: false };
  if (dispatch.state !== "reserved") return { ...(await reconcileDispatch({ ...options, assignment })), dispatched: false };
  immutableRef(profile.ref);
  if (latest.projection.policy.authorization === "aw") {
    if (!profile.logical_contract) {
      approvedProfile(profile, options.config || {});
      await verifyWorkerRoute({ githubClient: options.githubClient, owner: options.context.repo.owner, repo: options.context.repo.repo, profile });
    }
  }
  if (typeof options.validateDispatchCredential !== "function") throw new Error("work_queue_dispatch_credential_validator_required");
  const credential = await options.validateDispatchCredential({ assignment, profile });
  if (profile.principal !== undefined && credential?.principal !== profile.principal) throw new Error("work_queue_dispatch_credential_principal_mismatch");
  if (!isDispatchCredentialProof(credential, options.dispatchClient || options.githubClient, profile)) throw new Error("work_queue_dispatch_credential_proof_invalid");
  log.debug("launch.credential.checked");
  const destinationMetadata = options.destination || {};
  closed(destinationMetadata, [], ["host", "api_host"], "work_queue_native_destination");
  const trustedContext = await authenticatePublisher({ ...options, role: "dispatcher" });
  const sender = actorFromContext(trustedContext);
  const start = { kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "started", sender, ...(profile.principal === undefined ? { credential_principal: credential.principal } : {}) };
  let published;
  try {
    published = await publishOperations(options, trustedContext, ["start", assignment.dispatch_id], "dispatch", [start]);
  } catch (error) {
    log.failure("launch.start_marker.failed", error);
    return { ...(await reconcileDispatch({ ...options, assignment })), dispatched: false };
  }
  if (published.publishedNow !== true || published.reused !== false || published.persisted !== true || published.recovered !== false || published.idempotent !== false)
    return { ...(await reconcileDispatch({ ...options, assignment })), dispatched: false };
  latest = await loadQueue(options);
  log.debug("launch.start_marker.persisted");
  const started = latest.projection.dispatches.get(assignment.dispatch_id);
  if (!started || started.state !== "started" || started.run || started.released || canonical(started.sender) !== canonical(sender) || dispatchPrincipal(started) !== credential.principal)
    return { state: "launch_unresolved", released: false, dispatched: false };
  const destination = { ...destinationMetadata, repository: sender.repository, workflow: profile.workflow, ref: profile.ref };
  let returned;
  try {
    log.debug("launch.post.start");
    returned = await postQueueDispatch(options.dispatchClient || options.githubClient, destination, { work_queue_assignment: canonical(assignment) });
    log.debug("launch.post.complete");
  } catch (error) {
    log.failure("launch.post.failed", error);
    const checkedAt = options.now ?? Date.now();
    const receipt = rejectionReceipt(error, destination);
    if (receipt) {
      log.debug("launch.post.definitive_rejection");
      const evidence = lifecycleEvidence(assignment, profile, sender.repository, "nonlaunch", "github_api", checkedAt, { receipt, principal: credential.principal });
      await publishOperations(options, trustedContext, ["reject", assignment.dispatch_id, receipt], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "rejected", evidence }]);
      return { ...(await releaseAssignment(options, assignment, evidence)), dispatched: true };
    }
    try {
      await publishOperations(options, trustedContext, ["uncertain", assignment.dispatch_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "uncertain", reason: "post_response_unconfirmed" }]);
    } catch (error) {
      log.failure("launch.uncertain_marker.failed", error);
      // A durable start marker alone already fences every possible resend.
    }
    log.debug("launch.unresolved");
    return { state: "launch_unresolved", released: false, dispatched: true };
  }
  try {
    const expected = expectedWorkerRun(assignment, profile, options.context, sender.repository, credential.principal);
    const proof = validateNativeRun(await fetchNativeRun(options.dispatchClient || options.githubClient, sender.repository, returned.run_id), { ...expected, run_id: returned.run_id });
    const binding = bindingForRun(proof, expected);
    const evidence = lifecycleEvidence(assignment, profile, sender.repository, "reconciliation", "github_api", options.now ?? Date.now(), { run_id: proof.run_id, run_attempt: 1, principal: proof.principal });
    await publishOperations(options, trustedContext, ["bind", assignment.dispatch_id, proof.run_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "bound", run: binding, evidence }]);
    latest = await loadQueue(options);
    if (canonical(latest.projection.dispatches.get(assignment.dispatch_id)?.run) !== canonical(binding)) throw new Error("run_binding_conflict");
    log.debug("launch.binding.persisted");
    return { state: "bound", released: false, dispatched: true, run_id: returned.run_id, html_url: returned.html_url };
  } catch (error) {
    log.failure("launch.binding.failed", error);
    try {
      await publishOperations(options, trustedContext, ["uncertain", assignment.dispatch_id, returned.run_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "uncertain", reason: "binding_unconfirmed" }]);
    } catch (error) {
      log.failure("launch.binding_uncertain_marker.failed", error);
      // Authenticated activation can recover binding publication without POST.
    }
    return { state: "launch_unresolved", released: false, dispatched: true, run_id: returned.run_id };
  }
}

async function intentContext(options, intent, { recoverAccepted = false, preview = false } = {}) {
  const runtime = assertQueueControlRole(options);
  const normalized = normalizeControlIntentScope(options, intent, runtime);
  if (!normalized) {
    return authenticateIntentPublisher({ ...options, role: "dispatcher" });
  }
  const scope = options.assignment === undefined ? runtime.assignment || readClaimScopeContext()?.assignment : options.assignment;
  const latest = await loadQueue({ ...options, policyProposal: undefined, initializationContext: undefined });
  const { assignment, dispatch, profile } = validateStoredAssignment(latest.projection, scope, { allowReleased: !preview });
  const trustedContext = await authenticateIntentPublisher({ ...options, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle: normalized.claim_handle });
  const expected = expectedWorkerRun(assignment, profile, options.context, trustedContext.repository, dispatchPrincipal(dispatch));
  const proof = validateNativeRun(trustedContext.native_run, { ...expected, run_id: trustedContext.run_id });
  if (!dispatch.run || canonical(dispatch.run) !== canonical(bindingForRun(proof, expected))) throw new Error("work_queue_binding_not_durable");
  if (preview) return trustedContext;
  if (recoverAccepted && intent.intent_id !== undefined && latest.projection.requests.has(requestIdForIntent(trustedContext, intent.intent_id))) return trustedContext;
  validateWorkerContinuation(latest.projection, trustedContext);
  assertPolicyProposal(latest.projection, options);
  return trustedContext;
}

/** @param {{graph_id: string}[]} [acceptedNodes] */
function inheritedSubmissionParameters(parent, parameters, acceptedNodes = undefined) {
  closed(parameters, ["nodes"], [], "submit intent");
  if (!Array.isArray(parameters.nodes)) throw new Error("work_queue_graph_limit");
  return {
    nodes: parameters.nodes.map((node, index) => ({
      ...node,
      graph_id: node.graph_id === undefined ? (acceptedNodes?.[index]?.graph_id ?? parent.graph_id) : node.graph_id,
      pool: node.pool === undefined ? parent.pool : node.pool,
      priority: node.priority === undefined ? parent.priority : node.priority,
      fairness_key: node.fairness_key === undefined ? parent.fairness_key : node.fairness_key,
    })),
  };
}

function inheritWorkerSubmission(state, trustedContext, parameters) {
  if (actorFromContext(trustedContext).role !== "worker") return parameters;
  return inheritedSubmissionParameters(validateWorkerContinuation(state, trustedContext).work, parameters);
}

function acceptedSubmissionParameters(state, trustedContext, parameters, prior) {
  if (prior.request.kind !== "submit" || canonical(prior.actor) !== canonical(actorFromContext(trustedContext))) throw new Error("work_queue_request_reused");
  const policy =
    state.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "Policy" && operation.epoch === prior.policy_epoch)?.policy ??
    (prior.compacted_parameters_digest && prior.policy_epoch === state.policy_epoch ? state.policy : undefined);
  if (!policy) throw new Error("work_queue_policy_missing");
  let inherited = parameters;
  if (trustedContext.role === "worker") {
    // Reconstruct an accepted request's immutable metadata without granting fresh continuation authority.
    const dispatch = state.dispatches.get(trustedContext.dispatch_id);
    const member = dispatch?.claims.find(claim => claim.handle === trustedContext.claim_handle);
    const parent = member && state.works.get(member.work_id);
    if (!parent) throw new Error("work_queue_control_claim_not_authorized");
    inherited = inheritedSubmissionParameters(parent, parameters, prior.request.parameters.nodes);
  }
  if (policy.authorization === "aw") {
    // Recovery retains the adapter's original route choice even if the caller's
    // current allowlist or the global default has changed.
    inherited = {
      nodes: inherited.nodes.map((node, index) => (node.worker_profile === undefined ? { ...node, worker_profile: prior.request.parameters.nodes[index]?.worker_profile } : node)),
    };
  }
  const normalized = normalizeSubmitParameters(inherited, policy, trustedContext.created_at, state);
  for (let index = 0; index < normalized.nodes.length; index++) {
    const accepted = prior.request.parameters.nodes[index];
    for (const field of ["logical_contract", "execution_ref"]) {
      if (parameters.nodes[index]?.[field] !== undefined && parameters.nodes[index][field] !== accepted[field]) throw new Error("work_queue_request_reused");
      if (Object.hasOwn(accepted, field)) normalized.nodes[index][field] = accepted[field];
      else delete normalized.nodes[index][field];
    }
  }
  const restoreIdentity = (resource, accepted) => {
    if (!resource || !accepted) return resource;
    return {
      ...resource,
      ...(Object.hasOwn(resource, "repository_id") ? {} : { repository_id: accepted.repository_id }),
      ...(Object.hasOwn(resource, "resource_id") ? {} : { resource_id: accepted.resource_id }),
    };
  };
  for (const node of normalized.nodes) {
    const accepted = prior.request.parameters.nodes.find(work => work.work_id === node.work_id);
    node.depends_on = node.depends_on.map((edge, index) => {
      const original = accepted?.depends_on[index];
      return edge.kind !== "work" && original?.kind === edge.kind && original.condition === edge.condition ? { ...edge, resource: restoreIdentity(edge.resource, original.resource) } : edge;
    });
    if (node.subject) node.subject = restoreIdentity(node.subject, accepted?.subject);
    if (node.backing_issue) node.backing_issue = restoreIdentity(node.backing_issue, accepted?.backing_issue);
  }
  return normalized;
}

function dispatchesByOrigin(state, origin, exceptRequestId) {
  let count = 0;
  for (const dispatch of state.dispatches.values()) {
    if (dispatch.request_id === exceptRequestId) continue;
    const actor = state.requests.get(dispatch.request_id)?.actor;
    if (actor && ["principal", "repository", "workflow", "run_id", "run_attempt"].every(field => actor[field] === origin[field])) count++;
  }
  return count;
}

async function dispatchQueueIntent(options) {
  log.debug("intent.dispatch.start");
  const runtime = assertQueueControlRole(options);
  const message = options.message;
  closed(message, ["type", "intent_id", "pool", "max_claims", "max_dispatches"], ["claim_handle"], "work_queue_dispatch_next");
  if (message.type !== "work_queue_dispatch_next") throw new Error("work_queue_dispatch_intent_invalid");
  const normalized = normalizeControlIntentScope(options, message, runtime);
  const configured = { ...options, githubClient: options.githubClient || options.queueClient };
  let latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
  if (!latest.projection.policy) throw new Error("policy_missing");
  const requested = { pool: message.pool, max_claims: message.max_claims, max_dispatches: message.max_dispatches };
  if (isStagedMode(options) || isStagedMode(options.config)) {
    const trustedContext = await intentContext(configured, message, { preview: true });
    if (trustedContext.role === "worker" && latest.projection.dispatches.get(trustedContext.dispatch_id)?.pool !== requested.pool) throw new Error("work_queue_control_pool_not_authorized");
    assertPolicyProposal(latest.projection, configured);
    normalizeDispatchParameters(requested, latest.projection.policy, options.remainingDispatches);
    return { success: true, staged: true, status: "staged_preview", dispatches: 0, ...(normalized ? { claim_handle: normalized.claim_handle } : {}) };
  }
  const trustedContext = await intentContext(configured, message, { recoverAccepted: true });
  latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
  const stableId = requestIdForIntent(trustedContext, message.intent_id);
  const prior = latest.projection.requests.get(stableId);
  if (!prior) {
    await synchronizeDeployments(configured, trustedContext);
    latest = await loadQueue({ ...configured, policyProposal: undefined, initializationContext: undefined });
  }
  log.debug("intent.dispatch.recovery", { prior_request: !!prior });
  const runBudget = options.runDispatchBudget ?? options.config?.max ?? options.maxDispatches ?? options.remainingDispatches;
  const remaining = Math.min(options.remainingDispatches, Math.max(0, runBudget - dispatchesByOrigin(latest.projection, trustedContext, stableId)));
  const parameters = prior
    ? prior.request.parameters
    : {
        ...normalizeDispatchParameters(requested, latest.projection.policy, remaining),
        ...(latest.projection.deployments?.size ? { worker_profiles: approvedWorkerProfiles(latest.projection, requested.pool, options.config ?? {}) } : {}),
      };
  if (prior && ["pool", "max_claims", "max_dispatches"].some(field => parameters[field] !== requested[field])) throw new Error("work_queue_request_reused");
  if (trustedContext.role === "worker" && latest.projection.dispatches.get(trustedContext.dispatch_id)?.pool !== parameters.pool) throw new Error("work_queue_control_pool_not_authorized");
  const request = requestForIntent(trustedContext, message.intent_id, "dispatch_next", parameters);
  if (prior && (prior.request.kind !== request.kind || prior.request.fingerprint !== request.fingerprint || canonical(prior.actor) !== canonical(actorFromContext(trustedContext)))) throw new Error("work_queue_request_reused");
  if (prior && trustedContext.role === "worker") {
    try {
      validateWorkerContinuation(latest.projection, trustedContext);
    } catch (error) {
      if (!["claim_ineffective", "claim_effects_unauthorized"].includes(error?.code)) throw error;
      const groups = assignmentsForRequest(latest.projection, request.id);
      return { success: true, status: "durable", request_id: request.id, recovered: true, dispatches: groups.length, launches: [], acknowledgement_only: true, control: controlReceiptForRequest(latest.projection, request.id) };
    }
  }
  if (!prior) {
    assertPolicyProposal(latest.projection, configured);
    if (trustedContext.role !== "worker") await reconcileQueue({ ...configured, pool: parameters.pool });
  }
  const validateCandidateScope = (state, stable, actor) => {
    assertPolicyProposal(state, configured);
    if (actor.role === "worker" && validateWorkerContinuation(state, trustedContext).work.pool !== stable.parameters.pool) throw new Error("work_queue_control_pool_not_authorized");
    if (dispatchesByOrigin(state, trustedContext, stableId) + parameters.max_dispatches > runBudget) throw new Error("work_queue_dispatch_budget_exceeded");
  };
  const published = await (configured.publishWorkQueueRequest || store.publishWorkQueueRequest)({
    githubClient: configured.queueClient || configured.githubClient,
    owner: configured.context.repo.owner,
    repo: configured.context.repo.repo,
    ...(configured.branch === undefined ? {} : { branch: configured.branch }),
    context: trustedContext,
    actor: actorFromContext(trustedContext),
    request,
    refreshObservations: (state, stable, actor) => {
      validateCandidateScope(state, stable, actor);
      return refreshDependencies(configured, state, stable.parameters.pool, trustedContext);
    },
    generateOperations: (state, stable, actor, at, id, observations) => {
      if (!Array.isArray(observations)) throw new Error("work_queue_observation_refresh_required");
      validateCandidateScope(state, stable, actor);
      const decision = generateRequestOperations(state, stable, actor, at, id, observations);
      // Refuse the whole deterministic prefix before Claims, rather than skip
      // unauthorized routes and change queue fairness or strand reservations.
      approvedAssignments(state, "assignments" in decision ? decision.assignments : [], options.config || {});
      return decision;
    },
    core: configured.core,
  });
  const groups = assignmentsForRequest(published.state, request.id);
  log.debug("intent.dispatch.published", { assignments: groups.length, recovered: published.recovered === true });
  const launches = [];
  for (const assignment of groups) launches.push({ dispatch_id: assignment.dispatch_id, ...(await launchAssignment(configured, assignment)) });
  log.debug("intent.dispatch.complete", { assignments: groups.length, launches: launches.length });
  return { success: true, status: groups.length ? "durable" : "no_grant", request_id: request.id, reason: published.reason, dispatches: groups.length, launches, control: controlReceiptForRequest(published.state, request.id) };
}

async function processWorkQueueIntents(options) {
  const runtime = assertQueueControlRole(options);
  const { intents, errors } = readStagedIntentBatch(options.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH);
  return processParsedWorkQueueIntents(options, runtime, intents, errors);
}

async function processParsedWorkQueueIntents(options, runtime, intents, errors) {
  log.debug("intents.process.start", { intents: intents.length, parse_errors: errors.length });
  let remaining = options.maxDispatches ?? 1;
  /** @type {Array<{status: string, intent_id?: string, reason?: string, line?: number, claim_handle?: string, request_id?: string, commit_id?: string, success?: boolean, staged?: boolean, dispatches?: number, recovered?: boolean, launches?: object[], acknowledgement_only?: boolean, control?: object | null}>} */
  const receipts = errors.map(error => ({ ...error, status: "blocked" }));
  for (const intent of intents) {
    log.debug("intent.process.start", { dispatch: intent.kind === "dispatch_next", submit: intent.kind === "submit" });
    try {
      if (intent.kind === "finish") throw new Error("work_queue_control_intent_invalid");
      const normalized = normalizeControlIntentScope(options, intent, runtime);
      if (intent.kind === "dispatch_next") {
        const result = await dispatchQueueIntent({
          ...options,
          remainingDispatches: remaining,
          message: { type: "work_queue_dispatch_next", intent_id: intent.intent_id, ...intent.parameters, ...(Object.hasOwn(intent, "claim_handle") ? { claim_handle: intent.claim_handle } : {}) },
        });
        remaining -= result.dispatches;
        receipts.push({ intent_id: intent.intent_id, ...result });
      } else {
        if (isStagedMode(options) || isStagedMode(options.config)) {
          await intentContext(options, intent, { preview: true });
          receipts.push({ intent_id: intent.intent_id, status: "staged_preview", ...(normalized ? { claim_handle: normalized.claim_handle } : {}) });
          continue;
        }
        const trustedContext = await intentContext(options, intent, { recoverAccepted: true });
        let latest = await loadQueue({ ...options, policyProposal: undefined, initializationContext: undefined });
        const prior = latest.projection.requests.get(requestIdForIntent(trustedContext, intent.intent_id));
        if (!prior) {
          await synchronizeDeployments(options, trustedContext);
          latest = await loadQueue({ ...options, policyProposal: undefined, initializationContext: undefined });
        }
        const bootstrapPolicy = !latest.projection.policy && latest.sha === null && ["producer", "dispatcher"].includes(trustedContext.role) ? policyProposalFor(options) : undefined;
        const admissionState = bootstrapPolicy ? { ...latest.projection, policy: bootstrapPolicy } : latest.projection;
        if (!admissionState.policy) throw new Error("policy_missing");
        if (!Number.isSafeInteger(trustedContext.created_at) || trustedContext.created_at < 0) throw new Error("publisher_origin_time_missing");
        if (!prior && !bootstrapPolicy) assertPolicyProposal(latest.projection, options);
        const parameters = prior
          ? acceptedSubmissionParameters(latest.projection, trustedContext, intent.parameters, prior)
          : await resolveAdmissionResources(
              options,
              admissionState,
              normalizeSubmitParameters(
                approvedSubmissionParameters(admissionState, trustedContext, inheritWorkerSubmission(admissionState, trustedContext, intent.parameters), options.config || {}),
                futurePolicy(admissionState),
                trustedContext.created_at,
                admissionState
              ),
              trustedContext
            );
        const request = requestForIntent(trustedContext, intent.intent_id, "submit", parameters);
        if (prior && (prior.request.fingerprint !== request.fingerprint || canonical(prior.actor) !== canonical(actorFromContext(trustedContext)))) throw new Error("work_queue_request_reused");
        const result = await (options.publishWorkQueueRequest || store.publishWorkQueueRequest)({
          githubClient: options.queueClient || options.githubClient,
          owner: options.context.repo.owner,
          repo: options.context.repo.repo,
          ...(options.branch === undefined ? {} : { branch: options.branch }),
          context: trustedContext,
          actor: actorFromContext(trustedContext),
          request,
          ...(bootstrapPolicy === undefined ? {} : { policyProposal: bootstrapPolicy }),
          remediationVerifier: options.remediationVerifier,
          generateOperations: (state, stable, actor, at, id) => {
            assertPolicyProposal(state, options);
            if (actor.role === "worker") validateWorkerContinuation(state, trustedContext);
            approvedSubmissionParameters(state, trustedContext, stable.parameters, options.config || {});
            return generateRequestOperations(state, stable, actor, at, id);
          },
          core: options.core,
        });
        receipts.push({ intent_id: intent.intent_id, request_id: request.id, status: "durable", commit_id: result.commit?.id, control: controlReceiptForRequest(result.state, request.id) });
        log.debug("intent.submit.persisted", { recovered: result.recovered === true });
      }
    } catch (error) {
      log.failure("intent.process.blocked", error);
      receipts.push({ intent_id: intent.intent_id, status: "blocked", reason: "queue_control_authority_or_publication_failed" });
    }
  }
  log.debug("intents.process.complete", { receipts: receipts.length, remaining_dispatches: remaining });
  return { version: 3, receipts, remaining_dispatches: remaining };
}

function configuredDispatchBudget(options) {
  const explicit = options.maxDispatches ?? options.config?.max;
  if (explicit !== undefined) return integer(explicit, 0, 4096, "run dispatch budget");
  const raw = process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES;
  if (raw === undefined) return 1;
  if (!/^(?:0|[1-9][0-9]*)$/.test(raw)) throw new Error("work_queue_dispatch_budget_invalid");
  return integer(Number(raw), 0, 4096, "run dispatch budget");
}

async function main(options = {}) {
  log.debug("controls.start");
  const coreApi = options.core || global.core;
  const githubClient = options.githubClient || options.github || global.github;
  const context = options.context || global.context;
  const config = options.config || {};
  let result;
  try {
    const runtime = assertQueueControlRole({ ...options, context });
    const { intents, errors } = readStagedIntentBatch(options.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH);
    const preview = isStagedMode(options) || isStagedMode(config);
    const launching = intents.some(intent => intent.kind === "dispatch_next") && !preview;
    if (launching && !options.validateDispatchCredential) normalizeDispatchCredential(config.work_queue_dispatch_credential);
    const dispatchClient = options.dispatchClient || (launching ? await require("./handler_auth.cjs").createAuthenticatedGitHubClient(config) : githubClient);
    const validateDispatchCredential = options.validateDispatchCredential || (!launching ? undefined : createDispatchCredentialValidator(dispatchClient, config.work_queue_dispatch_credential, config["github-token"]));
    result = await processParsedWorkQueueIntents(
      { ...options, core: coreApi, githubClient, context, config, dispatchClient, validateDispatchCredential, maxDispatches: configuredDispatchBudget({ ...options, config }) },
      runtime,
      intents,
      errors
    );
  } catch (error) {
    log.failure("controls.blocked", error);
    result = { version: 3, receipts: [{ status: "blocked", reason: "staged_queue_intents_invalid" }], remaining_dispatches: 0 };
  }
  const durable = result.receipts.filter(receipt => receipt.status === "durable").length;
  const blocked = result.receipts.filter(receipt => receipt.status === "blocked").length;
  const unresolved = result.receipts.reduce((count, receipt) => count + (receipt.launches || []).filter(launch => !["bound", "released", "staged_preview"].includes(launch.state)).length, 0);
  const recoveryRequired = blocked > 0 || unresolved > 0;
  log.debug("controls.complete", { durable, blocked, unresolved, recovery_required: recoveryRequired });
  const status = recoveryRequired ? "recovery_required" : "ok";
  const summary = ["## Work queue controls", "", `Status: **${recoveryRequired ? "Recovery required" : "Complete"}**`, "", `- Durable requests: ${durable}`, `- Blocked intents: ${blocked}`, `- Unresolved launches: ${unresolved}`].join(
    "\n"
  );
  result = { ...result, status, success: !recoveryRequired, summary };
  coreApi.setOutput("work_queue_requests", JSON.stringify(result));
  coreApi.setOutput("work_queue_controls_status", status);
  coreApi.info(`Work queue controls: ${durable} durable requests; ${blocked} blocked intents; ${unresolved} unresolved launches`);
  if (coreApi.summary?.addRaw && coreApi.summary?.write) {
    coreApi.summary.addRaw(summary);
    await coreApi.summary.write();
  }
  if (recoveryRequired) {
    const failure = "Work queue controls require recovery; inspect the work_queue_requests output and step summary.";
    if (typeof coreApi.setFailed === "function") coreApi.setFailed(failure);
    else throw new Error(failure);
  }
  return result;
}

module.exports = {
  main,
  approvedProfile,
  approvedAssignments,
  approvedSubmissionParameters,
  staleEdges,
  refreshDependencies,
  resolveAdmissionResources,
  assertRemediationBackend,
  rejectionReceipt,
  launchAssignment,
  intentContext,
  inheritWorkerSubmission,
  acceptedSubmissionParameters,
  dispatchesByOrigin,
  dispatchQueueIntent,
  processWorkQueueIntents,
  configuredDispatchBudget,
};
