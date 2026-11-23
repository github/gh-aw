// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("dispatch");
const { queueControlErrorCode } = require("./work_queue_logging.cjs");
const { canonical, closed, integer, queueError } = require("./work_queue_codec.cjs");
const { poolEnv, readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { approvalGate, gateKey } = require("./work_queue_graph.cjs");
const { observationRefreshBudget } = require("./work_queue_limits.cjs");
const { cloneState, openClaims } = require("./work_queue_replay.cjs");
const { fifoCompare, planDispatch } = require("./work_queue_scheduler.cjs");
const { loadQueue, publishRequest, releaseOperations } = require("./work_queue_binding.cjs");
const { normalizeDispatchParameters, normalizeSubmitParameters, readStagedIntentBatch, validatePreparedPlanIntentBatch } = require("./work_queue_intents.cjs");
const { launchFailureReason, launchedRunEvidence, postQueueDispatch } = require("./work_queue_native.cjs");
const { dependencyKey, resolveDependencies, resolveExternalEdges } = require("./work_queue_dependency_resolver.cjs");
const { DEFAULT_INTENT_PATH } = require("./work_queue_mcp_server.cjs");
const { blockedReceipt, controlResult } = require("./work_queue_control_receipts.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");

const MISSING_INTENTS = "Work queue dispatcher produced no control intents; inspect the preparation step and tool-denial logs.";

/**
 * @typedef {import("./work_queue_control_receipts.cjs").ControlReceipt} ControlReceipt
 * @typedef {{githubClient?: any, queueClient?: any, dispatchClient?: any, context: any, core?: any, config?: Record<string, any>, policy?: import("./work_queue_policy.cjs").PolicyEnv | null, role?: string, requireAssignment?: boolean, branch?: string, staged?: boolean, dependencyResolver?: {scopes: Array<{host: string, repository: string}>, getClient: (scope: any) => Promise<any>}}} ControlOptions
 */

/** The job's queue role; observers never write the ledger. */
function queueRuntime(options) {
  const runtime = resolveWorkQueueRuntime(options.context?.payload, { role: options.role, requireAssignment: options.requireAssignment });
  if (runtime.role === "observer") throw queueError("unauthorized_operation", "observers cannot publish queue controls");
  return runtime;
}

/** @param {ControlOptions} options */
function requirePolicy(options) {
  const policy = options.policy ?? readWorkQueuePolicyEnv();
  if (!policy) throw queueError("policy_missing", "work queue controls require the compiled policy environment");
  return policy;
}

/** @param {ControlOptions} options */
function staged(options) {
  return options.staged === true || isStagedMode(options) || isStagedMode(options.config);
}

/** @param {ControlOptions} options @param {string} role */
function queueTarget(options, role) {
  return { githubClient: options.queueClient || options.githubClient, context: options.context, role, core: options.core, ...(options.branch === undefined ? {} : { branch: options.branch }) };
}

/** The Work of a worker run's own open Claim; its children inherit its graph and pool. */
function workerParent(state, runtime, context) {
  const dispatch = state.dispatches.get(runtime.assignment?.dispatch_id);
  const claim = dispatch?.state === "bound" && dispatch.run.run_id === String(context.runId) ? openClaims(state, dispatch)[0] : undefined;
  if (!claim) throw queueError("work_queue_control_claim_not_authorized", "a worker submits child Work only while its bound Claim is open");
  return state.works.get(claim.work_id);
}

/** Worker child submissions default graph, pool, priority, fairness and profile to the parent Work. */
function inheritWorkerSubmission(state, runtime, context, parameters) {
  if (runtime.role !== "worker") return parameters;
  const parent = workerParent(state, runtime, context);
  closed(parameters, ["nodes"], [], "submit intent", "work_queue_intent_invalid");
  if (!Array.isArray(parameters.nodes)) throw queueError("work_queue_graph_limit", "submission requires a node array");
  return {
    nodes: parameters.nodes.map(node => {
      const pool = node.pool ?? parent.pool;
      return {
        ...node,
        graph_id: node.graph_id ?? parent.graph_id,
        pool,
        priority: node.priority ?? parent.priority,
        fairness_key: node.fairness_key ?? parent.fairness_key,
        ...(node.worker_profile === undefined && pool === parent.pool ? { worker_profile: parent.worker_profile } : {}),
      };
    }),
  };
}

/**
 * Fill missing numeric identities of external references through the dependency
 * resolver. Fully identified references are left for replay to validate.
 * @param {ControlOptions} options
 * @param {Array<Record<string, any>>} works
 */
async function resolveAdmissionResources(options, works) {
  const references = [];
  const incomplete = resource => resource && (resource.repository_id === undefined || resource.resource_id === undefined);
  for (const work of works) {
    work.depends_on.forEach((edge, index) => {
      if (edge.kind !== "work" && incomplete(edge.resource)) references.push({ apply: binding => (work.depends_on[index] = { ...edge, resource: binding }), resource: { ...edge.resource, condition: edge.condition } });
    });
    if (incomplete(work.subject)) references.push({ apply: binding => (work.subject = binding), resource: { ...work.subject, condition: work.subject.kind === "issue" ? "completed" : "merged" } });
    if (incomplete(work.backing_issue)) references.push({ apply: binding => (work.backing_issue = binding), resource: { ...work.backing_issue, condition: "completed" } });
  }
  if (!references.length) return works;
  if (!options.dependencyResolver) throw queueError("external_admission_unverified", "external references need the dependency resolver");
  let resolved;
  try {
    resolved = await resolveDependencies(
      references.map(reference => reference.resource),
      { ...options.dependencyResolver, maxResources: 128, maxReads: 256 }
    );
  } catch {
    throw queueError("external_admission_unverified", "external references could not be resolved");
  }
  for (const reference of references) {
    const binding = resolved.results.get(dependencyKey(reference.resource))?.binding;
    if (!binding) throw queueError("external_admission_unverified", "an external reference could not be identified");
    reference.apply(binding);
  }
  return works;
}

/**
 * Commit staged Work. Existing Work IDs are idempotent no-ops; the pool's pending
 * Work (available or claimed) may not exceed the policy env pending_limit.
 * @param {ControlOptions} options
 * @returns {Promise<ControlReceipt>}
 */
async function submitIntent(options, runtime, intent) {
  const policy = requirePolicy(options);
  const latest = await loadQueue({ ...queueTarget(options, runtime.role), policy });
  const works = await resolveAdmissionResources(options, normalizeSubmitParameters(inheritWorkerSubmission(latest.state, runtime, options.context, intent.parameters), policy));
  const published = await publishRequest({
    ...queueTarget(options, runtime.role),
    kind: "submit",
    parameters: { work_ids: works.map(work => work.work_id) },
    operationsFor: state => {
      if (runtime.role === "worker") workerParent(state, runtime, options.context);
      const fresh = works.filter(work => !state.works.has(work.work_id));
      const pending = new Map();
      for (const work of state.works.values()) if (work.state === "available" || work.state === "claimed") pending.set(work.pool, (pending.get(work.pool) ?? 0) + 1);
      for (const work of fresh) {
        pending.set(work.pool, (pending.get(work.pool) ?? 0) + 1);
        if (pending.get(work.pool) > policy.pending_limit) throw queueError("work_queue_pending_limit", `pool ${work.pool} would exceed its pending_limit of ${policy.pending_limit}`);
      }
      return fresh;
    },
  });
  log.debug("intent.submit.committed", { works: works.length, recovered: published.recovered });
  return { intent_id: intent.intent_id, request_id: published.request.id, ...(published.commit ? { commit_id: published.commit.id } : {}), status: "committed" };
}

/**
 * Observations for unresolved external edges of the pool's available Work whose
 * predecessors completed, in dispatch order, bounded by the refresh budget.
 * Read failures leave the edge unsatisfied; they never block the dispatch.
 * @param {ControlOptions} options
 */
async function refreshObservations(options, state, parameters) {
  const budget = observationRefreshBudget(parameters.max_claims);
  if (!options.dependencyResolver || budget === 0) return [];
  const candidates = [...state.works.values()]
    .filter(work => work.pool === parameters.pool && work.state === "available" && Object.hasOwn(parameters.profiles, work.worker_profile))
    .filter(work => work.depends_on.every(edge => edge.kind !== "work" || state.works.get(edge.work_id)?.state === "completed"))
    .sort((a, b) => a.priority - b.priority || fifoCompare(a, b));
  const edges = new Map();
  let approvals = 0;
  for (const work of candidates) {
    for (const edge of work.depends_on) {
      if (edge.kind === "work" || edges.size >= budget) continue;
      const key = gateKey(edge.resource, edge.condition);
      if (state.observations.get(key)?.state !== "ready" && !edges.has(key)) {
        edges.set(key, edge);
        if (edge.condition === "approved") approvals++;
      }
    }
  }
  if (!edges.size) return [];
  try {
    // An approval read also verifies the closer's current repository permission.
    return await resolveExternalEdges([...edges.values()], { ...options.dependencyResolver, maxResources: edges.size, maxReads: Math.min(512, edges.size * 2 + approvals) });
  } catch (error) {
    log.failure("dependencies.refresh.failed", error, true);
    options.core?.warning?.("Work queue dependency refresh failed; external edges stay unsatisfied.");
    return [];
  }
}

/**
 * Worker workflow paths this dispatcher's compiled safe-outputs.dispatch-workflow config
 * allows it to launch: each queue worker name with its compiled file extension.
 * @param {Record<string, any> | undefined} config
 * @returns {Set<string>}
 */
function allowedWorkerWorkflows(config) {
  const listed = new Set(Array.isArray(config?.workflows) ? config.workflows : []);
  const files = config?.workflow_files && typeof config.workflow_files === "object" ? config.workflow_files : {};
  const allowed = new Set();
  for (const name of Array.isArray(config?.work_queue_workflows) ? config.work_queue_workflows : []) {
    if (typeof name === "string" && listed.has(name) && Object.hasOwn(files, name) && typeof files[name] === "string") allowed.add(`.github/workflows/${name}${files[name]}`);
  }
  return allowed;
}

/**
 * Re-check the resolved workflow against the allowlist, POST the worker launch, and read
 * back the run GitHub created. Record `Dispatch{started}` with that run and its revision,
 * or a `launch_failed` Release (Work returns to the pool) when the launch was refused,
 * failed, or produced no verifiable run: a worker binds only a run recorded here.
 * @param {ControlOptions} options
 * @param {Set<string>} allowed
 * @returns {Promise<import("./work_queue_control_receipts.cjs").LaunchReceipt>}
 */
async function launchDispatch(options, policy, dispatch, allowed) {
  const dispatchId = dispatch.dispatch_id;
  /** @type {string | undefined} */
  let failure;
  /** @type {{run_id: string, revision: string} | undefined} */
  let launch;
  if (!allowed.has(dispatch.workflow)) {
    log.debug("launch.workflow.not_allowlisted", { allowed: allowed.size });
    failure = "not_allowlisted";
  } else {
    const client = options.dispatchClient || options.githubClient;
    const destination = { owner: options.context.repo.owner, repo: options.context.repo.repo, workflow: dispatch.workflow };
    try {
      const response = await postQueueDispatch(
        client,
        { ...destination, ref: options.config?.["target-ref"] || process.env.GITHUB_REF || options.context.ref },
        { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) }
      );
      launch = await launchedRunEvidence(client, destination, response);
    } catch (error) {
      log.failure("launch.post.failed", error, true);
      failure = launchFailureReason(error);
    }
  }
  try {
    await publishRequest({
      ...queueTarget(options, "dispatcher"),
      kind: "launch",
      parameters: { dispatch_id: dispatchId },
      operationsFor: state => {
        if (state.dispatches.get(dispatchId)?.state !== "reserved") return [];
        return launch ? [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", run_id: launch.run_id, revision: launch.revision }] : releaseOperations(state, dispatchId, "launch_failed", policy);
      },
    });
  } catch (error) {
    log.failure("launch.record.failed", error, true);
    options.core?.warning?.(`Work queue could not record the launch of ${dispatchId}; maintenance resolves the dispatch.`);
  }
  return failure ? { dispatch_id: dispatchId, state: "launch_failed", reason: failure } : { dispatch_id: dispatchId, state: "started" };
}

/**
 * Claim the pool's next eligible Work (priority, then FIFO) and launch one worker
 * per Dispatch. Pool limits come from the compiled policy env on every run.
 * @param {ControlOptions & {message: Record<string, any>, remainingDispatches: number}} options
 * @returns {Promise<ControlReceipt>}
 */
async function dispatchQueueIntent(options) {
  const runtime = queueRuntime(options);
  const { intent_id: intentId, type, ...requested } = options.message;
  if (type !== undefined && type !== "work_queue_dispatch_next") throw queueError("work_queue_intent_invalid", "unsupported queue control message");
  if (runtime.role !== "dispatcher") throw queueError("work_queue_worker_dispatch_unsupported", "only dispatchers publish dispatch_next");
  const normalized = normalizeDispatchParameters(requested);
  if (normalized.max_dispatches > options.remainingDispatches) throw queueError("work_queue_dispatch_budget_exceeded", "dispatch request exceeds the run's dispatch budget");
  const policy = requirePolicy(options);
  const pool = poolEnv(policy, normalized.pool);
  // Claims go only to profiles this dispatcher is compiled to launch; replay pins them to the request.
  const allowed = allowedWorkerWorkflows(options.config);
  const profiles = Object.entries(pool.profiles).filter(([, profile]) => allowed.has(profile.workflow));
  if (!profiles.length) throw queueError("work_queue_worker_not_allowlisted", `pool ${normalized.pool} has no worker profile in this dispatcher's dispatch-workflow allowlist`);
  log.debug("dispatch.profiles.allowlisted", { profiles: profiles.length, excluded: Object.keys(pool.profiles).length - profiles.length });
  const parameters = {
    ...normalized,
    concurrency: pool.concurrency,
    per_account_limit: pool.per_account_limit,
    profiles: Object.fromEntries(profiles.map(([name, profile]) => [name, { workflow: profile.workflow }])),
  };
  const receiptBase = intentId === undefined ? {} : { intent_id: intentId };
  if (staged(options)) return { ...receiptBase, status: "committed", dispatches: 0, launches: [] };
  const latest = await loadQueue({ ...queueTarget(options, "dispatcher"), policy });
  const observations = await refreshObservations(options, latest.state, parameters);
  const published = await publishRequest({
    ...queueTarget(options, "dispatcher"),
    kind: "dispatch_next",
    parameters,
    operationsFor: (state, request) => {
      const changed = observations.filter(observation => canonical(state.observations.get(gateKey(observation.resource, observation.condition)) ?? null) !== canonical(observation));
      const plan = overlay => {
        const observed = cloneState(state);
        for (const observation of overlay) observed.observations.set(gateKey(observation.resource, observation.condition), observation);
        return planDispatch(observed, parameters, request.id);
      };
      let decision = plan(changed);
      // A new ready approval grants one pinned successor; commit it only with that Claim.
      const granted = new Set(
        decision.operations.flatMap(operation => {
          const gate = approvalGate(state.works.get(operation.work_id));
          return gate ? [gateKey(gate.resource, gate.condition)] : [];
        })
      );
      const fresh = changed.filter(observation => observation.condition !== "approved" || observation.state !== "ready" || granted.has(gateKey(observation.resource, observation.condition)));
      if (fresh.length !== changed.length) decision = plan(fresh);
      log.debug("dispatch.plan", { claims: decision.operations.length, observations: fresh.length, deferred_approvals: changed.length - fresh.length });
      return [...fresh, ...decision.operations];
    },
  });
  const dispatches = [...published.state.dispatches.values()].filter(dispatch => dispatch.request_id === published.request.id);
  const launches = [];
  for (const dispatch of dispatches) if (dispatch.state === "reserved") launches.push(await launchDispatch(options, policy, dispatch, allowed));
  log.debug("intent.dispatch.complete", { dispatches: dispatches.length, launches: launches.length });
  return { ...receiptBase, request_id: published.request.id, ...(published.commit ? { commit_id: published.commit.id } : {}), status: "committed", dispatches: dispatches.length, launches };
}

/** Best-effort Issue projection after committed controls. */
async function projectAfterControls(options) {
  try {
    await require("./work_queue_issues.cjs").projectIssues({
      githubClient: options.queueClient || options.githubClient,
      context: options.context,
      core: options.core,
      policy: requirePolicy(options),
      ...(options.branch === undefined ? {} : { branch: options.branch }),
    });
  } catch (error) {
    log.failure("issues.project.failed", error, true);
    options.core?.warning?.("Work queue Issue projection failed; maintenance retries it.");
  }
}

/**
 * @param {ControlOptions & {maxDispatches?: number}} options
 * @param {Array<any>} intents
 * @param {Array<{intent_id?: string, reason: string}>} errors
 * @returns {Promise<{receipts: ControlReceipt[], remaining: number}>}
 */
async function processParsedWorkQueueIntents(options, intents, errors) {
  const runtime = queueRuntime(options);
  let remaining = options.maxDispatches ?? 1;
  if (runtime.role === "dispatcher" && !intents.length && !errors.length) return { receipts: [blockedReceipt("work_queue_dispatcher_intents_missing")], remaining };
  const invalidPlan = validatePreparedPlanIntentBatch(intents, errors);
  if (invalidPlan) return { receipts: [blockedReceipt(invalidPlan)], remaining };
  const receipts = errors.map(error => blockedReceipt(error.reason, error.intent_id));
  let committed = false;
  for (const intent of intents) {
    try {
      if (intent.kind === "dispatch_next") {
        const receipt = await dispatchQueueIntent({ ...options, remainingDispatches: remaining, message: { intent_id: intent.intent_id, ...intent.parameters } });
        remaining -= receipt.dispatches ?? 0;
        receipts.push(receipt);
      } else if (staged(options)) receipts.push({ intent_id: intent.intent_id, status: "committed" });
      else receipts.push(await submitIntent(options, runtime, intent));
      committed ||= !staged(options);
    } catch (error) {
      log.failure("intent.process.blocked", error, true);
      receipts.push(blockedReceipt(queueControlErrorCode(error), intent.intent_id));
    }
  }
  if (committed) await projectAfterControls(options);
  return { receipts, remaining };
}

function configuredDispatchBudget(options) {
  const explicit = options.maxDispatches ?? options.config?.max;
  if (explicit !== undefined) return integer(explicit, 0, 4096, "run dispatch budget");
  const raw = process.env.GH_AW_WORK_QUEUE_MAX_DISPATCHES;
  if (raw === undefined) return 1;
  if (!/^(?:0|[1-9][0-9]*)$/.test(raw)) throw new Error("work_queue_dispatch_budget_invalid");
  return integer(Number(raw), 0, 4096, "run dispatch budget");
}

/**
 * Process the staged queue intents of this job and publish `work_queue_requests`.
 * The step fails only when an intent is blocked or a dispatcher staged none.
 * @param {Partial<ControlOptions> & {github?: any, intentPath?: string, maxDispatches?: number}} [options]
 */
async function main(options = {}) {
  const coreApi = options.core || global.core;
  const githubClient = options.githubClient || options.github || global.github;
  const configured = { ...options, core: coreApi, githubClient, context: options.context || global.context, config: options.config || {} };
  /** @type {ControlReceipt[]} */
  let receipts;
  let remaining = 0;
  try {
    const { intents, errors } = readStagedIntentBatch(options.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH);
    ({ receipts, remaining } = await processParsedWorkQueueIntents({ ...configured, maxDispatches: configuredDispatchBudget(configured) }, intents, errors));
  } catch (error) {
    log.failure("controls.blocked", error, true);
    receipts = [blockedReceipt("staged_queue_intents_invalid")];
  }
  const result = controlResult(receipts, remaining);
  coreApi.setOutput("work_queue_requests", JSON.stringify(result));
  coreApi.setOutput("work_queue_controls_status", result.status);
  coreApi.info(`Work queue controls: ${result.status}; ${receipts.length} receipts`);
  if (coreApi.summary?.addRaw && coreApi.summary?.write) await coreApi.summary.addRaw(result.summary).write();
  if (!result.success) {
    const failure = receipts.some(receipt => receipt.reason === "work_queue_dispatcher_intents_missing")
      ? MISSING_INTENTS
      : `Work queue controls blocked: ${[...new Set(receipts.filter(receipt => receipt.status === "blocked").map(receipt => receipt.reason))].join(", ")}. Inspect the work_queue_requests output and step summary.`;
    if (typeof coreApi.setFailed === "function") coreApi.setFailed(failure);
    else throw new Error(failure);
  }
  return result;
}

module.exports = { allowedWorkerWorkflows, configuredDispatchBudget, dispatchQueueIntent, inheritWorkerSubmission, launchDispatch, main, processParsedWorkQueueIntents, refreshObservations, resolveAdmissionResources };
