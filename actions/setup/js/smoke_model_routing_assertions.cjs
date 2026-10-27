// @ts-check
"use strict";

const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
/**
 * Deterministic model-routing smoke assertions.
 *
 * Smoke workflows call this from a `post-steps` github-script step. Every check
 * reads runner- and proxy-written evidence (the unified session and the
 * api-proxy token-usage log), never the agent's own reply or the agent-writable
 * `agent/awf-routing-outcome.json` (`model_routing.outcome`).
 *
 * Request attribution follows what AWF records: a token-usage record with
 * `purpose: "routing_classification"` is the classifier; every other record is
 * agent traffic (AWF writes no `purpose` for agent requests, and `x_initiator`
 * is only the billing class). Agent traffic is attributed to a declared
 * sub-agent when the session correlates it (request IDs on `subagent.*` events,
 * or sub-agent IDs on proxy records); otherwise by model: requests on a declared
 * sub-agent's model belong to that sub-agent, and all other agent traffic
 * belongs to the main agent. Declared sub-agent models must therefore be
 * distinct from each other and disjoint from `allowedModels`.
 *
 * Check IDs:
 * - C1 configuration: declared sub-agent models are distinct and not in
 *   allowed-models
 * - E1 agent execution outcome; E2 evidence files present and parseable
 * - R1 routing selected; R2 one classifier call; R3 main-agent traffic on the
 *   selected model and a supported endpoint; R4 main-agent requests exist and
 *   use only allowed models
 * - M1 main-agent requests exist and use the expected endpoint (when configured)
 * - P1 the selection record's requested mode (`objective.mode`) equals the
 *   configured mode, its recommended profile (`mode`) is economy, balanced or
 *   robust, and its selected effort is one of the selected model's /reflect
 *   `supported_reasoning_efforts`, or null when that list is empty (when
 *   configured). The recommended profile and selected effort are also printed
 *   as info; they vary between runs and are never pass/fail on their own.
 * - H2 coverage effort (only when `expectSelectedEffort` is set): the proxy
 *   selection record's selected effort equals the expected effort, so a run
 *   that routed to a different effort is not mistaken for coverage of it.
 * - H3 applied effort (only when `expectRequestedEffortMatchesSelection` is
 *   true): every main-agent `firewall.model_routing` `stage: "request"` event
 *   written by the firewall has `requestedEffort` (the effort AWF read from the
 *   forwarded request body) equal to the selection record's `selectedEffort`.
 *   Classifier requests (`purpose: "routing_classification"`) and requests on
 *   a declared sub-agent model are excluded; fails when no main-agent request
 *   events exist.
 * - S1 sub-agent ran exactly once; S2 completed without failing; S3 status 200 on
 *   the declared model and endpoint; S4 events carry the declared agent name
 *   (when configured)
 * - T1–T4 AWF AI-credit steering (only when `steering` is configured):
 *   T1 an `ai_credit` 80% notice on a 200 response in token-usage.jsonl, the
 *   same `steering` on the session `firewall.token_usage` event with the same
 *   request ID, and no `subagent.started` events (the main agent received it);
 *   T2 each `(type, threshold)` at most once, thresholds in {80, 90, 95, 99},
 *   and each `ai_credit` notice's pre-request total at or above that share of
 *   the budget (order is not checked: AWF sends the highest pending threshold
 *   first); T3 the final `ai_credits_total` is below the budget, no
 *   `ai_credits_limit_exceeded` routing failure, and agent execution
 *   succeeded; T4 every other check passed
 *
 * - U1–U3 unified-session contract. `main` runs them only when the smoke opts
 *   in; `sessionContractMain` always runs E2 and U1–U3 alone for unrouted
 *   smokes. Session-contract fields:
 *   - `expectModelInventory: true` opts in and enables U2
 *   - `copilotInventory: true` opts in, enables U2 and also requires a
 *     configured `copilot` endpoint with models
 *   - `resolutionRoles` (any array, including `[]`) opts in and lists the
 *     roles U3 requires; `[]` requires none
 *   `false` or omitted booleans and an omitted `resolutionRoles` do not opt
 *   in, so a smoke that sets only `false` values runs no U checks in `main`.
 *   U1 no `session.collection_warning` events; U2 exactly one
 *   agent-phase `model.inventory`, and with `copilotInventory` its `copilot`
 *   endpoint is configured with a non-empty model list (when
 *   `expectModelInventory` or `copilotInventory` is true; otherwise an info
 *   line reports that U2 was not evaluated); U3 every role in `resolutionRoles` has
 *   exactly one `model.resolution` with status exact or resolved and a
 *   resolved model. U1–U3 are not evaluated when the session file is missing.
 *   - `requiredSubAgents` (array of names) opts in and enables U4: each named
 *     sub-agent has at least one `subagent.started`, every started invocation
 *     has a `subagent.completed`, no `subagent.failed` is recorded for it, and
 *     its events record a model that is not an unresolved `${{ ... }}`
 *     expression or `__GH_AW_*__` placeholder. A sub-agent that never started
 *     fails U4. Lifecycle events are correlated as in S1/S2.
 *
 * Every check that iterates over requests fails when there are none to check.
 *
 * Threat-detection checks (`detectionMain`, run from
 * `safe-outputs.threat-detection.post-steps` in the detection job) read the
 * detection run's own evidence under `/tmp/gh-aw/threat-detection`:
 * - D1 detection execution outcome is success and the structured result file
 *   parses with the verdict keys (`execution.json` is not completion evidence)
 * - D2 detection requests exist, all on the configured detection model, which is
 *   disjoint from allowed-models
 * - D3 no routing_classification request, no model-routing selection, and no
 *   model-routing block in the detection's resolved AWF config
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_ROOT_DIR = "/tmp/gh-aw";
const SESSION_PATH = "usage/aw_session.jsonl";
const TOKEN_USAGE_PATHS = ["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"];
const MODEL_ROUTING_PATHS = TOKEN_USAGE_PATHS.map(file => file.replace(/token-usage\.jsonl$/, "model-routing.jsonl"));
const REFLECT_PATHS = ["sandbox/firewall/awf-reflect.json", "agent/awf-reflect.json"];
const CLASSIFIER_PURPOSE = "routing_classification";
const MAX_OBSERVED = 12;
const STEERING_THRESHOLDS = [80, 90, 95, 99];
// AWF clamps every budget to a non-overridable hard cap.
const AI_CREDITS_HARD_CAP = 10000;
const AI_CREDITS_LIMIT_CODE = "ai_credits_limit_exceeded";
const CREDIT_EPSILON = 1e-6;
const ROUTING_PROFILES = ["economy", "balanced", "robust"];
const RESOLVED_STATUSES = ["exact", "resolved"];

/**
 * @typedef {{ name: string, model: string, endpoint: string | string[] }} SubAgentExpectation
 * @typedef {{ maxAiCredits: number }} SteeringExpectation
 * @typedef {{
 *   expectModelInventory?: boolean,
 *   copilotInventory?: boolean,
 *   resolutionRoles?: string[],
 *   requiredSubAgents?: string[],
 * }} SessionContractExpectations
 * @typedef {SessionContractExpectations & {
 *   allowedModels: string[],
 *   mainEndpoint?: string,
 *   routingMode?: string,
 *   subAgents?: SubAgentExpectation[],
 *   requireDeclaredAgentNames?: boolean,
 *   executionOutcome?: string,
 *   steering?: SteeringExpectation,
 *   expectSelectedEffort?: string,
 *   expectRequestedEffortMatchesSelection?: boolean,
 * }} RoutingExpectations
 * @typedef {{ passes: string[], failures: string[], info?: string[] }} RoutingAssertionResults
 */

/**
 * Normalize provider-qualified and dated model IDs to the served model name.
 * @param {unknown} model
 * @returns {string}
 */
function servedModel(model) {
  return String(model || "")
    .trim()
    .replace(/^.*\//, "")
    .replace(/-[0-9]{4}(?:-[0-9]{2}-[0-9]{2}|[0-9]{4})$/, "")
    .replace(/^(claude-.+)-([0-9]+)-([0-9]+)$/, "$1-$2.$3");
}

/**
 * Drop query strings (for example `/v1/messages?beta=true`) and trailing slashes.
 * @param {unknown} endpoint
 * @returns {string}
 */
function normalizeEndpoint(endpoint) {
  return String(endpoint || "")
    .split("?")[0]
    .replace(/(.)\/+$/, "$1");
}

/** @param {any} request @returns {string} */
function requestEndpoint(request) {
  return normalizeEndpoint(request?.path ?? request?.endpoint);
}

/** @param {any} request @returns {string} */
function describeRequest(request) {
  const status = request?.status ?? "<no status>";
  return `${servedModel(request?.model) || "<no model>"} ${requestEndpoint(request) || "<no endpoint>"} ${status}`;
}

/** @param {any[]} requests @returns {string} */
function describeRequests(requests) {
  if (!requests.length) return "none";
  const shown = requests.slice(0, MAX_OBSERVED).map(describeRequest).join(", ");
  return requests.length > MAX_OBSERVED ? `${shown}, … (${requests.length} total)` : shown;
}

/** @param {any} event @returns {string} */
function describeEvent(event) {
  const data = event?.data ?? {};
  const details = [`agentName=${data.agentName ?? "<none>"}`];
  for (const key of ["model", "resolvedModel", "outcome", "errorCode"]) {
    if (data[key] !== undefined) details.push(`${key}=${data[key]}`);
  }
  return `${event?.type ?? "<no type>"}(${details.join(" ")})`;
}

/** @param {any[]} events @returns {string} */
function describeEvents(events) {
  if (!events.length) return "none";
  const shown = events.slice(0, MAX_OBSERVED).map(describeEvent).join(", ");
  return events.length > MAX_OBSERVED ? `${shown}, … (${events.length} total)` : shown;
}

/** @param {any} event @returns {boolean} */
function isClassifierRequest(event) {
  return event?.purpose === CLASSIFIER_PURPOSE;
}

/**
 * @param {any} value
 * @returns {string}
 */
function idString(value) {
  return value === undefined || value === null ? "" : String(value);
}

/** @param {any} request @returns {string} */
function requestId(request) {
  return idString(request?.request_id ?? request?.requestId);
}

/** @param {any} request @returns {string} */
function requestAgentId(request) {
  return idString(request?.agent_id ?? request?.agentId);
}

/** @param {any} event @returns {string} */
function eventRequestId(event) {
  return idString(event?.data?.requestId ?? event?.data?.request_id ?? event?.requestId ?? event?.request_id);
}

/**
 * Supported endpoints for a model, according to the routing selection and the
 * AWF `/reflect` routing metadata.
 * @param {string} model
 * @param {any} selection
 * @param {any} reflect
 * @returns {Set<string>}
 */
function supportedEndpoints(model, selection, reflect) {
  const endpoints = new Set();
  const reflectEndpoints = Array.isArray(reflect?.endpoints) ? reflect.endpoints : [];
  for (const endpoint of reflectEndpoints) {
    for (const entry of Array.isArray(endpoint?.routing_models) ? endpoint.routing_models : []) {
      if (servedModel(entry?.model_id) !== model || !Array.isArray(entry?.supported_endpoints)) continue;
      for (const value of entry.supported_endpoints) endpoints.add(normalizeEndpoint(value));
    }
  }
  // The selection's endpoint is used only when /reflect has no metadata for the model.
  if (!endpoints.size && selection?.endpoint) endpoints.add(normalizeEndpoint(selection.endpoint));
  endpoints.delete("");
  return endpoints;
}

/**
 * Supported reasoning efforts for a model from the AWF `/reflect` routing
 * metadata, or null when /reflect has no effort list for the model.
 * @param {string} model
 * @param {any} reflect
 * @returns {Set<string> | null}
 */
function supportedEfforts(model, reflect) {
  /** @type {Set<string> | null} */
  let efforts = null;
  const reflectEndpoints = Array.isArray(reflect?.endpoints) ? reflect.endpoints : [];
  for (const endpoint of reflectEndpoints) {
    for (const entry of Array.isArray(endpoint?.routing_models) ? endpoint.routing_models : []) {
      if (servedModel(entry?.model_id) !== model || !Array.isArray(entry?.supported_reasoning_efforts)) continue;
      efforts ??= new Set();
      for (const value of entry.supported_reasoning_efforts) efforts.add(String(value));
    }
  }
  return efforts;
}

/** @param {any} event @returns {string} */
function invocationKey(event) {
  return idString(event?.data?.invocationId ?? event?.agentId ?? event?.data?.toolCallId);
}

/**
 * Lifecycle events for one declared sub-agent: its `subagent.started` events
 * (by agent name) and every `subagent.*` event correlated to them by
 * invocation or agent ID (or tool call ID when no agent ID is recorded).
 * @param {any[]} subagentEvents - All `subagent.*` events in the session.
 * @param {string} name - Declared sub-agent name.
 * @returns {{ started: any[], ids: Set<string>, activity: any[] }}
 */
function subAgentLifecycle(subagentEvents, name) {
  const started = subagentEvents.filter(event => event.type === "subagent.started" && event.data?.agentName === name);
  const ids = new Set(started.map(event => idString(event.data?.invocationId ?? event.agentId)).filter(Boolean));
  const toolCallIds = new Set(
    started
      .filter(event => !idString(event.data?.invocationId ?? event.agentId))
      .map(event => idString(event.data?.toolCallId))
      .filter(Boolean)
  );
  const activity = subagentEvents.filter(event => ids.has(idString(event.data?.invocationId)) || ids.has(idString(event.agentId)) || toolCallIds.has(idString(event.data?.toolCallId)));
  return { started, ids, activity };
}

/** @param {any[]} events @returns {any[]} */
function subagentLifecycleEvents(events) {
  return events.filter(event => typeof event?.type === "string" && event.type.startsWith("subagent."));
}

/**
 * Evaluate model-routing evidence against the smoke expectations.
 * @param {{ events: any[], requests: any[], reflect?: any, expectations: RoutingExpectations }} input
 * @returns {RoutingAssertionResults}
 */
function evaluateModelRoutingEvidence({ events, requests, reflect = null, expectations }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [], info: [] };
  /** @param {string} id @param {string} message */
  const pass = (id, message) => results.passes.push(`PASS ${id} ${message}`);
  /** @param {string} id @param {string} message */
  const fail = (id, message) => results.failures.push(`FAIL ${id} ${message}`);

  const allowedList = [...new Set((expectations.allowedModels ?? []).map(servedModel).filter(Boolean))];
  const allowed = new Set(allowedList);
  const subAgents = expectations.subAgents ?? [];

  // C1: sub-agent models must be disjoint from the routing candidates and from
  // each other, otherwise requests cannot be attributed by model.
  if (subAgents.length) {
    const c1Problems = [];
    const overlapping = subAgents.filter(agent => allowed.has(servedModel(agent.model)));
    if (overlapping.length)
      c1Problems.push(
        `declared sub-agent model(s) also in allowed-models [${allowedList.join(", ")}]: ${overlapping.map(agent => `${agent.name}=${servedModel(agent.model)}`).join(", ")}; sub-agent models must be disjoint from the routing candidates`
      );
    /** @type {Map<string, string[]>} */
    const namesByModel = new Map();
    for (const agent of subAgents) {
      const model = servedModel(agent.model);
      namesByModel.set(model, [...(namesByModel.get(model) ?? []), agent.name]);
    }
    const shared = [...namesByModel].filter(([, names]) => names.length > 1);
    if (shared.length) c1Problems.push(`declared sub-agents share a model: ${shared.map(([model, names]) => `${model} (${names.join(", ")})`).join(", ")}; each sub-agent must declare a distinct model`);
    if (c1Problems.length) fail("C1", c1Problems.join("; "));
    else pass("C1", `declared sub-agent models are distinct and disjoint from allowed-models [${allowedList.join(", ")}]`);
  }

  if (expectations.executionOutcome !== undefined) {
    if (expectations.executionOutcome === "success") pass("E1", "agent execution outcome is success");
    else fail("E1", `agent execution outcome is ${expectations.executionOutcome || "<empty>"}, expected success`);
  }

  // R1: runner-written routing status and a firewall selection record.
  const workflowInfo = events.filter(event => event?.type === "workflow.info" && event.provenance?.component === "workflow" && !String(event.provenance?.path ?? "").startsWith("agent/"));
  const runnerRouting = workflowInfo.map(event => event.data?.modelRouting).find(routing => routing && typeof routing.status === "string");
  const runnerStatus = runnerRouting?.status;
  const selections = events.filter(event => event?.type === "firewall.model_routing" && event.provenance?.component === "firewall" && event.data?.stage === "selection");
  const selection = selections.filter(event => event.data?.selectedModel).at(-1)?.data;
  const selectedModel = servedModel(selection?.selectedModel);
  const harnessOutcomes = events.filter(event => event?.type === "model_routing.outcome");
  const r1Problems = [];
  if (runnerStatus !== "selected") r1Problems.push(`runner-written routing status is ${runnerStatus ?? "missing"}, expected selected`);
  if (!selection) r1Problems.push(`no firewall model_routing selection record with a selected model (observed ${selections.length} selection record(s))`);
  else if (!allowed.has(selectedModel)) r1Problems.push(`selected model ${selectedModel} is outside allowed-models [${allowedList.join(", ")}]`);
  if (r1Problems.length) {
    const ignored = harnessOutcomes.length ? `; ignored agent-written model_routing.outcome (status=${harnessOutcomes.map(event => event.data?.status ?? "<none>").join(", ")})` : "";
    fail("R1", `routing not selected: ${r1Problems.join("; ")}${ignored}`);
  } else {
    pass("R1", `routing selected ${selectedModel} (runner status=${runnerStatus}, endpoint=${normalizeEndpoint(selection?.endpoint) || "<none>"})`);
  }

  // R2: exactly one classifier request.
  const classifierRequests = requests.filter(isClassifierRequest);
  if (classifierRequests.length === 1) pass("R2", `one routing_classification request: ${describeRequest(classifierRequests[0])}`);
  else fail("R2", `expected exactly 1 routing_classification request, observed ${classifierRequests.length}: ${describeRequests(classifierRequests)}`);

  // Every non-classifier request is agent traffic; AWF does not record which
  // agent made it.
  const agentTraffic = requests.filter(request => !isClassifierRequest(request));

  const subagentEvents = subagentLifecycleEvents(events);
  const startedEvents = subagentEvents.filter(event => event.type === "subagent.started");
  const observedNames = [...new Set(startedEvents.map(event => event.data?.agentName ?? "<none>"))];

  // Attribute agent traffic to declared sub-agents: prefer session correlation
  // (request IDs on sub-agent events, or sub-agent IDs on proxy records), and
  // fall back to the declared model.
  const subAgentEvidence = subAgents.map(agent => {
    const { started, ids, activity } = subAgentLifecycle(subagentEvents, agent.name);
    const eventRequestIds = new Set(activity.map(eventRequestId).filter(Boolean));
    const correlated = agentTraffic.filter(request => eventRequestIds.has(requestId(request)) || ids.has(requestAgentId(request)));
    return { agent, model: servedModel(agent.model), started, activity, correlated };
  });
  const correlatedRequests = new Set(subAgentEvidence.flatMap(evidence => evidence.correlated));
  const attributed = subAgentEvidence.map(evidence => (evidence.correlated.length ? evidence.correlated : agentTraffic.filter(request => !correlatedRequests.has(request) && servedModel(request.model) === evidence.model)));
  const subagentRequests = new Set(attributed.flat());
  const mainRequests = agentTraffic.filter(request => !subagentRequests.has(request));
  const observedMain = mainRequests.length ? describeRequests(mainRequests) : `none (agent requests: ${describeRequests(agentTraffic)})`;

  // R3: main-agent traffic on the selected model and a supported endpoint.
  if (!selectedModel) {
    fail("R3", `no selected model to verify; observed main-agent requests: ${observedMain}`);
  } else {
    const endpoints = supportedEndpoints(selectedModel, selection, reflect);
    const onModel = mainRequests.filter(request => servedModel(request.model) === selectedModel);
    if (!endpoints.size) {
      fail("R3", `no supported-endpoint metadata for ${selectedModel} in the routing selection or /reflect; observed: ${onModel.length ? describeRequests(onModel) : observedMain}`);
    } else {
      const ok = onModel.find(request => Number(request.status) === 200 && endpoints.has(requestEndpoint(request)));
      if (ok) pass("R3", `main-agent request on selected model: ${describeRequest(ok)}`);
      else
        fail(
          "R3",
          `no 200 request for ${selectedModel} on a supported endpoint (${[...endpoints].join(", ")}); observed: ${onModel.length ? describeRequests(onModel) : `no ${selectedModel} requests; main-agent requests: ${observedMain}`}`
        );
    }
  }

  // R4: main-agent requests must use allowed-models; sub-agent models are scoped
  // to their own checks and cannot widen the main agent's model policy.
  const outOfPolicy = mainRequests.filter(request => !allowed.has(servedModel(request.model)));
  if (!mainRequests.length) fail("R4", `no main-agent requests to check against allowed-models [${allowedList.join(", ")}]; observed agent requests: ${describeRequests(agentTraffic)}`);
  else if (outOfPolicy.length) fail("R4", `main-agent request(s) on models outside allowed-models [${allowedList.join(", ")}]: ${describeRequests(outOfPolicy)}`);
  else pass("R4", `all ${mainRequests.length} main-agent request(s) used allowed models`);

  // M1: main-agent requests use the expected endpoint.
  if (expectations.mainEndpoint) {
    const expected = normalizeEndpoint(expectations.mainEndpoint);
    const wrong = mainRequests.filter(request => requestEndpoint(request) !== expected);
    if (!mainRequests.length) fail("M1", `no main-agent requests to check for ${expected}; observed agent requests: ${describeRequests(agentTraffic)}`);
    else if (wrong.length) fail("M1", `main-agent request(s) not on ${expected}; observed: ${describeRequests(wrong)}`);
    else pass("M1", `all ${mainRequests.length} main-agent request(s) used ${expected}`);
  }

  // P1: requested mode, recommended profile and selected effort. The
  // recommendation and effort are not deterministic, so only their validity is checked.
  const selectedEffort = selection?.selectedEffort;
  if (selection) results.info?.push(`INFO routing recommended profile=${selection.mode ?? "<none>"} requested mode=${selection.objective?.mode ?? "<none>"} selected effort=${selectedEffort ?? "<none>"}`);
  if (expectations.routingMode) {
    const p1Problems = [];
    if (!selection) {
      p1Problems.push("no firewall model_routing selection record with a selected model");
    } else {
      if (selection.degradedClassification === true) {
        const classification = events.filter(event => event?.type === "firewall.model_routing" && event.data?.stage === "classification").at(-1)?.data;
        const classifierRequest = requests.filter(isClassifierRequest).at(-1);
        const degradedReason = selection.degradedReason ?? "missing";
        const classifierModel = classification?.classifierModel ?? selection.classifierModel ?? classifierRequest?.model ?? "<missing>";
        p1Problems.push(`classification degraded: degraded_reason=${degradedReason}; classifier_model=${classifierModel}`);
      }
      const requested = selection.objective?.mode;
      if (requested !== expectations.routingMode) p1Problems.push(`requested mode (objective.mode) is ${requested ?? "missing"}, expected ${expectations.routingMode}`);
      if (!ROUTING_PROFILES.includes(selection.mode)) p1Problems.push(`recommended profile (mode) is ${selection.mode ?? "missing"}, expected one of ${ROUTING_PROFILES.join(", ")}`);
      const efforts = supportedEfforts(selectedModel, reflect);
      if (!efforts) p1Problems.push(`no /reflect supported_reasoning_efforts for ${selectedModel} to check selected effort ${selectedEffort ?? "null"} against`);
      else if (!efforts.size && selectedEffort !== null) p1Problems.push(`selected effort is ${selectedEffort}, expected null because /reflect lists no supported efforts for ${selectedModel}`);
      else if (efforts.size && !efforts.has(String(selectedEffort))) p1Problems.push(`selected effort ${selectedEffort ?? "null"} is not among ${selectedModel}'s /reflect supported efforts [${[...efforts].join(", ")}]`);
    }
    if (p1Problems.length) fail("P1", p1Problems.join("; "));
    else pass("P1", `requested mode ${expectations.routingMode}; recommended profile ${selection?.mode} and selected effort ${selectedEffort ?? "null"} are valid for ${selectedModel}`);
  }

  for (const [index, { agent, model, started, activity, correlated }] of subAgentEvidence.entries()) {
    const endpoints = (Array.isArray(agent.endpoint) ? agent.endpoint : [agent.endpoint]).map(normalizeEndpoint);
    const label = `sub-agent ${agent.name}`;

    // S1: ran exactly once.
    if (started.length === 1) pass("S1", `${label} started once`);
    else fail("S1", `${label}: expected exactly 1 subagent.started, observed ${started.length}; observed agent names: [${observedNames.join(", ")}]`);

    // S2: completed and did not fail.
    const completed = activity.filter(event => event.type === "subagent.completed");
    const failed = activity.filter(event => event.type === "subagent.failed");
    if (!started.length) fail("S2", `${label}: never started; observed events: ${describeEvents(subagentEvents)}`);
    else if (!completed.length || failed.length) fail("S2", `${label}: completed=${completed.length} failed=${failed.length}; observed events: ${describeEvents(activity)}`);
    else pass("S2", `${label} completed`);

    // S3: sub-agent traffic on the declared model and endpoint.
    const related = attributed[index];
    const onModel = related.filter(request => servedModel(request.model) === model);
    const ok = onModel.find(request => Number(request.status) === 200 && endpoints.includes(requestEndpoint(request)));
    const recorded = activity.flatMap(event => [event.data?.model, event.data?.selectedModel, event.data?.resolvedModel, event.data?.firstDispatchedModel]).filter(value => typeof value === "string" && value !== "");
    const mismatched = [...new Set(recorded.filter(value => servedModel(value) !== model))];
    const s3Problems = [];
    if (!ok) s3Problems.push(`no 200 request for ${model} on ${endpoints.join(" or ")}`);
    if (mismatched.length) s3Problems.push(`recorded model(s) [${mismatched.join(", ")}] differ from declared ${model}`);
    if (s3Problems.length) {
      const observed = onModel.length
        ? onModel.map(request => `${requestEndpoint(request) || "<no endpoint>"} ${request.status ?? "<no status>"}`).join(", ")
        : `no ${model} requests; ${correlated.length ? `requests correlated to ${label}: ${describeRequests(correlated)}` : `agent requests: ${describeRequests(agentTraffic)}`}`;
      fail("S3", `${label}: ${s3Problems.join("; ")}; observed: ${observed}`);
    } else {
      pass("S3", `${label} request ${describeRequest(ok)}`);
    }

    // S4: events carry the declared agent name.
    if (expectations.requireDeclaredAgentNames) {
      const renamed = activity.filter(event => event.data?.agentName !== undefined && event.data.agentName !== agent.name);
      if (!started.length) fail("S4", `${label}: no events carry the declared agent name; observed agent names: [${observedNames.join(", ")}]`);
      else if (renamed.length) fail("S4", `${label}: events carry agent name(s) other than the declared name; observed: ${describeEvents(renamed)}`);
      else pass("S4", `${label} events carry the declared agent name`);
    }
  }

  if (expectations.requireDeclaredAgentNames) {
    const declared = new Set(subAgents.map(agent => agent.name));
    const undeclared = startedEvents.filter(event => !declared.has(event.data?.agentName));
    if (undeclared.length) fail("S4", `sub-agent events carry undeclared agent name(s); declared: [${[...declared].join(", ")}]; observed: ${describeEvents(undeclared)}`);
  }

  return results;
}

/**
 * H2: the proxy-selected effort is the effort this smoke exists to cover.
 * @param {{ events: any[], expectedEffort: string }} evidence
 * @returns {RoutingAssertionResults}
 */
function evaluateSelectedEffortCoverage({ events, expectedEffort }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [], info: [] };
  const selection = events.filter(event => event?.type === "firewall.model_routing" && event.provenance?.component === "firewall" && event.data?.stage === "selection" && event.data?.selectedModel).at(-1)?.data;
  const selectedEffort = selection?.selectedEffort ?? null;
  if (selectedEffort === expectedEffort) results.passes.push(`PASS H2 proxy-selected effort ${selectedEffort} is the covered effort`);
  else results.failures.push(`FAIL H2 proxy-selected effort ${selectedEffort ?? "null"}, expected ${expectedEffort}: this run does not cover the ${expectedEffort} path`);
  return results;
}

/**
 * H3: every main-agent request the proxy forwarded carried the selected effort.
 * @param {{ events: any[], subAgentModels?: string[] }} evidence
 * @returns {RoutingAssertionResults}
 */
function evaluateRequestedEffort({ events, subAgentModels = [] }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [], info: [] };
  const routing = events.filter(event => event?.type === "firewall.model_routing" && event.provenance?.component === "firewall");
  const selection = routing.filter(event => event.data?.stage === "selection" && event.data?.selectedModel).at(-1)?.data;
  const selectedEffort = selection?.selectedEffort ?? null;
  const subAgentServed = new Set(subAgentModels.map(servedModel));
  const requests = routing.map(event => event.data).filter(data => data?.stage === "request" && data.purpose !== CLASSIFIER_PURPOSE && !subAgentServed.has(servedModel(data.requestedModel)));
  if (!requests.length) {
    results.failures.push("FAIL H3 no main-agent firewall model_routing request events to check the requested effort");
    return results;
  }
  const mismatched = requests.filter(data => (data.requestedEffort ?? null) !== selectedEffort);
  if (mismatched.length) {
    const shown = mismatched
      .slice(0, MAX_OBSERVED)
      .map(data => `${data.requestId ?? "<no request id>"} requested=${data.requestedEffort ?? "null"} selected=${selectedEffort ?? "null"}`)
      .join(", ");
    const observed = mismatched.length > MAX_OBSERVED ? `${shown}, … (${mismatched.length} total)` : shown;
    results.failures.push(`FAIL H3 ${mismatched.length} of ${requests.length} main-agent request(s) did not request the selected effort: ${observed}`);
  } else {
    results.passes.push(`PASS H3 all ${requests.length} main-agent request(s) requested the selected effort ${selectedEffort ?? "null"}`);
  }
  return results;
}

/** @param {unknown} value @returns {number | undefined} */
function finiteNumber(value) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

/** @param {any} steering @returns {string} */
function steeringKey(steering) {
  return `${steering?.type ?? "<no type>"}@${steering?.threshold ?? "<no threshold>"}`;
}

/** @param {any} request @returns {string} */
function describeNotice(request) {
  const total = finiteNumber(request?.ai_credits_total);
  const thisResponse = finiteNumber(request?.ai_credits_this_response);
  const before = total !== undefined && thisResponse !== undefined ? Number((total - thisResponse).toFixed(6)) : "<unknown>";
  return `${steeringKey(request?.steering)} request=${requestId(request) || "<none>"} status=${request?.status ?? "<none>"} total_before=${before} total_after=${total ?? "<unknown>"}`;
}

/** @param {any[]} requests @returns {string} */
function describeNotices(requests) {
  if (!requests.length) return "none";
  const shown = requests.slice(0, MAX_OBSERVED).map(describeNotice).join(", ");
  return requests.length > MAX_OBSERVED ? `${shown}, … (${requests.length} total)` : shown;
}

/**
 * Evaluate AWF AI-credit steering evidence (T1–T3). Reads only proxy- and
 * runner-written records: the api-proxy token-usage log, the unified session
 * and the api-proxy model-routing log.
 * @param {{ events: any[], requests: any[], routingRecords?: any[], expectations: { steering: SteeringExpectation, executionOutcome?: string } }} input
 * @returns {RoutingAssertionResults}
 */
function evaluateSteeringEvidence({ events, requests, routingRecords = [], expectations }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [] };
  /** @param {string} id @param {string} message */
  const pass = (id, message) => results.passes.push(`PASS ${id} ${message}`);
  /** @param {string} id @param {string} message */
  const fail = (id, message) => results.failures.push(`FAIL ${id} ${message}`);

  const configured = expectations.steering?.maxAiCredits;
  const budgetValid = typeof configured === "number" && Number.isInteger(configured) && configured >= 1;
  const budget = budgetValid ? Math.min(configured, AI_CREDITS_HARD_CAP) : NaN;
  const budgetProblem = `steering.maxAiCredits must be an integer of 1 or more, got ${configured ?? "<none>"}`;

  // The classifier counts toward the budget but never carries a notice.
  const agentTraffic = requests.filter(request => !isClassifierRequest(request));
  const notices = agentTraffic.filter(request => request?.steering && typeof request.steering === "object");
  const sessionUsage = events.filter(event => event?.type === "firewall.token_usage" && event.provenance?.component === "firewall" && event.provenance?.phase === "agent");
  const sessionNotices = sessionUsage.filter(event => event.data?.steering && typeof event.data.steering === "object");
  const describeSessionNotices = () => (sessionNotices.length ? sessionNotices.map(event => `${steeringKey(event.data.steering)} request=${eventRequestId(event) || "<none>"}`).join(", ") : "none");

  // T1: the 80% notice reached the main agent and both logs agree on it.
  const t1Problems = [];
  const credit80 = notices.filter(request => request.steering.type === "ai_credit" && Number(request.steering.threshold) === 80);
  if (!credit80.length) t1Problems.push(`no ai_credit@80 notice in token-usage.jsonl`);
  else if (!credit80.some(request => Number(request.status) === 200)) t1Problems.push(`ai_credit@80 notice only on non-200 response(s), so it never reached the model`);
  const noticeKey = (id, steering) => JSON.stringify([id, steeringKey(steering)]);
  const tokenNoticeKeys = new Set(notices.map(request => noticeKey(requestId(request), request.steering)));
  for (const request of notices) {
    const id = requestId(request);
    const matching = sessionUsage.filter(event => eventRequestId(event) === id);
    if (!id) t1Problems.push(`notice ${steeringKey(request.steering)} has no request_id`);
    else if (!matching.length) t1Problems.push(`no session firewall.token_usage event for notice ${steeringKey(request.steering)} request=${id}`);
    else if (!matching.some(event => steeringKey(event.data?.steering) === steeringKey(request.steering))) {
      t1Problems.push(`session event for request=${id} carries steering ${matching.map(event => steeringKey(event.data?.steering)).join(", ")}, expected ${steeringKey(request.steering)}`);
    }
  }
  const unmatchedSession = sessionNotices.filter(event => !tokenNoticeKeys.has(noticeKey(eventRequestId(event), event.data.steering)));
  if (unmatchedSession.length) t1Problems.push(`session notice(s) with no matching token-usage.jsonl record: ${unmatchedSession.map(event => `${steeringKey(event.data.steering)} request=${eventRequestId(event) || "<none>"}`).join(", ")}`);
  const started = events.filter(event => event?.type === "subagent.started");
  if (started.length) t1Problems.push(`${started.length} subagent.started event(s), so a notice may have gone to a sub-agent: ${describeEvents(started)}`);
  if (t1Problems.length) fail("T1", `${t1Problems.join("; ")}; observed token-usage notices: ${describeNotices(notices)}; observed session notices: ${describeSessionNotices()}`);
  else pass("T1", `main agent received ${describeNotice(credit80.find(request => Number(request.status) === 200))}; session event matches; no sub-agents`);

  // T2: each (type, threshold) once, on a known threshold, and never early.
  // Order is not checked: AWF sends the highest pending threshold first.
  const t2Problems = [];
  if (!notices.length) t2Problems.push("no steering notices to check");
  /** @type {Map<string, number>} */
  const counts = new Map();
  for (const request of notices) counts.set(steeringKey(request.steering), (counts.get(steeringKey(request.steering)) ?? 0) + 1);
  const repeated = [...counts].filter(([, count]) => count > 1);
  if (repeated.length) t2Problems.push(`repeated notice(s): ${repeated.map(([key, count]) => `${key} x${count}`).join(", ")}`);
  const unknown = notices.filter(request => !STEERING_THRESHOLDS.includes(request.steering.threshold));
  if (unknown.length) t2Problems.push(`threshold(s) outside [${STEERING_THRESHOLDS.join(", ")}]: ${[...new Set(unknown.map(request => steeringKey(request.steering)))].join(", ")}`);
  const creditNotices = notices.filter(request => request.steering.type === "ai_credit");
  if (creditNotices.length && !budgetValid) t2Problems.push(budgetProblem);
  else {
    for (const request of creditNotices) {
      const total = finiteNumber(request.ai_credits_total);
      const thisResponse = finiteNumber(request.ai_credits_this_response);
      const threshold = Number(request.steering.threshold);
      if (total === undefined || thisResponse === undefined) {
        t2Problems.push(`notice ${steeringKey(request.steering)} request=${requestId(request) || "<none>"} lacks numeric ai_credits_total/ai_credits_this_response`);
        continue;
      }
      const required = (threshold / 100) * budget;
      if (total - thisResponse + CREDIT_EPSILON < required) t2Problems.push(`early notice ${describeNotice(request)}: total before the request is below ${threshold}% of ${budget} (${Number(required.toFixed(6))})`);
    }
  }
  if (t2Problems.length) fail("T2", `${t2Problems.join("; ")}; observed notices: ${describeNotices(notices)}`);
  else pass("T2", `${notices.length} notice(s), each once and at or above its threshold of ${budget}: ${describeNotices(notices)}`);

  // T3: the run ended under budget, without a budget rejection, and succeeded.
  const t3Problems = [];
  const totals = requests.map(request => finiteNumber(request?.ai_credits_total)).filter(value => value !== undefined);
  const finalTotal = totals.length ? totals.reduce((a, b) => Math.max(a, b), -Infinity) : undefined;
  if (!budgetValid) t3Problems.push(budgetProblem);
  if (finalTotal === undefined) t3Problems.push("no ai_credits_total in token-usage.jsonl");
  else if (budgetValid && finalTotal >= budget) t3Problems.push(`final ai_credits_total ${finalTotal} is at or above the budget ${budget}`);
  /** @param {any} record */
  const isBudgetFailure = record => record?.stage === "failure" && record?.code === AI_CREDITS_LIMIT_CODE;
  const routingFailures = routingRecords.filter(isBudgetFailure);
  const sessionFailures = events.filter(event => event?.type === "firewall.model_routing" && event.provenance?.component === "firewall" && isBudgetFailure(event.data));
  if (routingFailures.length) t3Problems.push(`${routingFailures.length} ${AI_CREDITS_LIMIT_CODE} failure record(s) in model-routing.jsonl`);
  if (sessionFailures.length) t3Problems.push(`${sessionFailures.length} ${AI_CREDITS_LIMIT_CODE} firewall.model_routing failure event(s) in the session`);
  if (expectations.executionOutcome !== "success") t3Problems.push(`agent execution outcome is ${expectations.executionOutcome || "<empty>"}, expected success`);
  if (t3Problems.length) fail("T3", t3Problems.join("; "));
  else pass("T3", `final ai_credits_total ${finalTotal} is below the budget ${budget}; no budget rejection; agent execution succeeded`);

  return results;
}

/**
 * Whether the smoke opted in to the unified-session contract: a `true`
 * inventory flag or an explicit `resolutionRoles` array. `false` does not opt in.
 * @param {SessionContractExpectations} expectations
 * @returns {boolean}
 */
function hasSessionContract(expectations) {
  return expectations.expectModelInventory === true || expectations.copilotInventory === true || Array.isArray(expectations.resolutionRoles) || Array.isArray(expectations.requiredSubAgents);
}

/** @param {any} inventory @returns {string} */
function describeInventoryEndpoints(inventory) {
  const endpoints = Array.isArray(inventory?.data?.endpoints) ? inventory.data.endpoints : [];
  if (!endpoints.length) return "none";
  return endpoints.map(endpoint => `${endpoint?.provider ?? "<no provider>"} configured=${endpoint?.configured} models=${Array.isArray(endpoint?.models) ? endpoint.models.length : "<none>"}`).join(", ");
}

/** @param {any[]} resolutions @returns {string} */
function describeResolutions(resolutions) {
  if (!resolutions.length) return "none";
  return resolutions.map(event => `${event.data?.role ?? "<no role>"}: ${event.data?.configured_model ?? "<none>"} -> ${event.data?.resolved_model || "<none>"} (${event.data?.status ?? "<no status>"})`).join(", ");
}

/**
 * Evaluate the unified-session contract (U1–U3). Every expectation is explicit
 * per smoke; nothing is inferred from sub-agent declarations.
 * @param {{ events: any[], expectations: SessionContractExpectations }} input
 * @returns {RoutingAssertionResults}
 */
function evaluateSessionContract({ events, expectations }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [], info: [] };
  /** @param {string} id @param {string} message */
  const pass = (id, message) => results.passes.push(`PASS ${id} ${message}`);
  /** @param {string} id @param {string} message */
  const fail = (id, message) => results.failures.push(`FAIL ${id} ${message}`);

  // U1: the collector reported no data it had to drop.
  const warnings = events.filter(event => event?.type === "session.collection_warning");
  if (warnings.length) {
    const groups = new Map();
    for (const event of warnings) {
      const code = event.data?.code ?? "<no code>";
      const source = event.data?.path ?? "<no path>";
      const key = JSON.stringify([code, source]);
      const group = groups.get(key) ?? { code, source, count: 0, line: undefined };
      group.count++;
      group.line ??= event.data?.line;
      groups.set(key, group);
    }
    const observed = [...groups.values()].map(group => `${group.code} in ${group.source} (${group.count} occurrence(s)${group.line === undefined ? "" : `, example line ${group.line}`})`);
    fail("U1", `${warnings.length} session.collection_warning event(s): ${observed.join(", ")}`);
  } else pass("U1", "no session.collection_warning events");

  // U2: exactly one agent-phase model inventory (the collector can also add
  // detection-phase inventories and one per candidate group).
  if (expectations.expectModelInventory || expectations.copilotInventory) {
    const inventories = events.filter(event => event?.type === "model.inventory");
    const agentInventories = inventories.filter(event => event.provenance?.phase === "agent");
    const observed = inventories.map(event => `${event.provenance?.phase ?? "<no phase>"} ${event.provenance?.path ?? "<no path>"}`).join(", ") || "none";
    if (agentInventories.length !== 1) {
      fail("U2", `expected exactly 1 agent-phase model.inventory, observed ${agentInventories.length}; model.inventory events: ${observed}`);
    } else {
      const inventory = agentInventories[0];
      const source = inventory.provenance?.path ?? "<no path>";
      const copilot = (Array.isArray(inventory.data?.endpoints) ? inventory.data.endpoints : []).find(endpoint => endpoint?.provider === "copilot");
      const copilotModels = Array.isArray(copilot?.models) ? copilot.models.length : 0;
      if (expectations.copilotInventory && (copilot?.configured !== true || !copilotModels)) {
        fail("U2", `agent-phase model.inventory from ${source} has no configured copilot endpoint with models; observed endpoints: ${describeInventoryEndpoints(inventory)}`);
      } else {
        pass("U2", `one agent-phase model.inventory from ${source}${expectations.copilotInventory ? `; copilot endpoint configured with ${copilotModels} model(s)` : ""}`);
      }
    }
  } else (results.info ??= []).push("U2 not evaluated: neither expectModelInventory nor copilotInventory is true");

  // U3: each required role recorded exactly one successful model resolution.
  const roles = expectations.resolutionRoles ?? [];
  const resolutions = events.filter(event => event?.type === "model.resolution");
  const u3Problems = [];
  for (const role of roles) {
    const forRole = resolutions.filter(event => event.data?.role === role);
    if (!forRole.length) {
      u3Problems.push(`no model.resolution for role ${role}`);
      continue;
    }
    if (forRole.length > 1) {
      u3Problems.push(`expected exactly 1 model.resolution for role ${role}, observed ${forRole.length}`);
      continue;
    }
    const data = forRole[0].data ?? {};
    if (!RESOLVED_STATUSES.includes(data.status)) u3Problems.push(`role ${role} status is ${data.status ?? "<none>"}${data.cause ? ` (cause ${data.cause})` : ""}, expected exact or resolved`);
    else if (data.resolved_model === null || data.resolved_model === undefined || data.resolved_model === "") u3Problems.push(`role ${role} has no resolved_model`);
  }
  if (u3Problems.length) fail("U3", `${u3Problems.join("; ")}; observed model.resolution: ${describeResolutions(resolutions)}`);
  else if (roles.length) pass("U3", `model.resolution recorded for ${describeResolutions(resolutions.filter(event => roles.includes(event.data?.role)))}`);
  else pass("U3", `no model.resolution roles required; observed model.resolution: ${describeResolutions(resolutions)}`);

  // U4: each required sub-agent was invoked, every invocation completed, none
  // failed, and its recorded model is not an unresolved expression or placeholder.
  const subagentEvents = subagentLifecycleEvents(events);
  for (const name of expectations.requiredSubAgents ?? []) {
    const label = `sub-agent ${name}`;
    const { started, activity } = subAgentLifecycle(subagentEvents, name);
    const failed = subagentEvents.filter(event => event.type === "subagent.failed" && (activity.includes(event) || event.data?.agentName === name));
    const completedKeys = new Set(activity.filter(event => event.type === "subagent.completed" && event.data?.cancelled !== true).map(invocationKey));
    const incomplete = started.filter(event => !completedKeys.has(invocationKey(event)));
    const models = [
      ...new Set([...activity, ...failed].flatMap(event => [event.data?.model, event.data?.selectedModel, event.data?.resolvedModel, event.data?.firstDispatchedModel]).filter(value => typeof value === "string" && value !== "")),
    ];
    const unresolved = models.filter(model => /\$\{\{|__GH_AW_/.test(model));
    const u4Problems = [];
    if (!started.length) u4Problems.push("never started (no subagent.started)");
    if (failed.length) u4Problems.push(`${failed.length} subagent.failed event(s)${failed.map(event => (event.data?.error ? `: ${String(event.data.error).slice(0, 200)}` : "")).join("")}`);
    if (started.length && incomplete.length) u4Problems.push(`${incomplete.length} of ${started.length} invocation(s) have no subagent.completed`);
    if (started.length && !models.length) u4Problems.push("no model recorded on its lifecycle events");
    if (unresolved.length) u4Problems.push(`unresolved model expression(s) [${unresolved.join(", ")}]`);
    if (u4Problems.length) fail("U4", `${label}: ${u4Problems.join("; ")}; observed events: ${describeEvents(started.length ? [...new Set([...activity, ...failed])] : subagentEvents)}`);
    else pass("U4", `${label} started ${started.length} time(s) and completed on model(s) [${models.join(", ")}]`);
  }

  return results;
}

/**
 * @param {string} file
 * @param {string[]} problems
 * @returns {any[]}
 */
function readJsonl(file, problems) {
  const records = [];
  let content;
  try {
    content = fs.readFileSync(file, "utf8");
  } catch (error) {
    problems.push(`failed to read ${file}: ${getErrorMessage(error)}`);
    return records;
  }
  for (const event of iterateJsonlEvents(content)) {
    if (event.type === "json") records.push(event.value);
    else if (event.reason !== "blank") problems.push(`${file}:${event.lineNumber} is not valid JSON: ${getErrorMessage(event.error)}`);
  }
  return records;
}

/**
 * Collect evidence from the gh-aw temp directory. Post-steps run before the
 * usage-artifact step writes aw_session.jsonl, so the unified session is built first.
 * With `sessionOnly`, only the unified session is read.
 * @param {{ rootDir?: string, engine?: string, buildSession?: boolean, withRoutingRecords?: boolean, sessionOnly?: boolean }} [options]
 * @returns {{ events: any[], requests: any[], routingRecords: any[], reflect: any, problems: string[], sessionFound: boolean }}
 */
function collectModelRoutingEvidence({ rootDir = DEFAULT_ROOT_DIR, engine, buildSession = true, withRoutingRecords = false, sessionOnly = false } = {}) {
  /** @type {string[]} */
  const problems = [];
  if (buildSession) {
    try {
      const { writeUnifiedSession } = require("./unified_session.cjs");
      writeUnifiedSession({ rootDir, ...(engine ? { engine } : {}) });
    } catch (error) {
      problems.push(`failed to build the unified session: ${getErrorMessage(error)}`);
    }
  }

  let events = [];
  const sessionFile = path.join(rootDir, SESSION_PATH);
  const sessionFound = fs.existsSync(sessionFile);
  if (sessionFound) events = readJsonl(sessionFile, problems);
  else problems.push(`missing ${SESSION_PATH}`);
  if (sessionOnly) return { events, requests: [], routingRecords: [], reflect: null, problems, sessionFound };

  let requests = [];
  const tokenFile = TOKEN_USAGE_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file));
  if (tokenFile) {
    requests = readJsonl(tokenFile, problems).filter(record => record && typeof record === "object" && !Array.isArray(record) && (record.event === undefined || record.event === "token_usage"));
  } else {
    problems.push(`missing api-proxy token-usage.jsonl (looked in ${TOKEN_USAGE_PATHS.join(", ")})`);
  }

  // model-routing.jsonl is read only for the steering checks (T3), which look
  // for budget rejections in it; a missing file is not an evidence problem.
  let routingRecords = [];
  const routingFile = withRoutingRecords ? MODEL_ROUTING_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file)) : undefined;
  if (routingFile) routingRecords = readJsonl(routingFile, problems).filter(record => record && typeof record === "object" && !Array.isArray(record));

  let reflect = null;
  const reflectFile = REFLECT_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file));
  if (reflectFile) {
    try {
      reflect = JSON.parse(fs.readFileSync(reflectFile, "utf8"));
    } catch (error) {
      problems.push(`${reflectFile} is not valid JSON: ${getErrorMessage(error)}`);
    }
  }
  return { events, requests, routingRecords, reflect, problems, sessionFound };
}

/**
 * Append U1–U3 to `results`, or note that they were not evaluated when the
 * session file is missing (E2 already reports that).
 * @param {RoutingAssertionResults} results
 * @param {{ events: any[], sessionFound: boolean }} evidence
 * @param {SessionContractExpectations} expectations
 */
function appendSessionContract(results, evidence, expectations) {
  if (!evidence.sessionFound) {
    (results.info ??= []).push(`U1–U3 not evaluated: missing ${SESSION_PATH}`);
    return;
  }
  const contract = evaluateSessionContract({ events: evidence.events, expectations });
  results.passes.push(...contract.passes);
  results.failures.push(...contract.failures);
  (results.info ??= []).push(...(contract.info ?? []));
}

/**
 * Collect evidence and evaluate all checks.
 * @param {RoutingExpectations & { rootDir?: string, engine?: string, buildSession?: boolean }} options
 * @returns {RoutingAssertionResults}
 */
function checkModelRoutingEvidence(options) {
  const { rootDir, engine, buildSession, ...expectations } = options;
  const evidence = collectModelRoutingEvidence({ rootDir, engine, buildSession, withRoutingRecords: Boolean(expectations.steering) });
  const results = evaluateModelRoutingEvidence({ events: evidence.events, requests: evidence.requests, reflect: evidence.reflect, expectations });
  if (evidence.problems.length) results.failures.unshift(...evidence.problems.map(problem => `FAIL E2 evidence: ${problem}`));
  else results.passes.unshift("PASS E2 evidence: unified session and api-proxy token usage present");
  if (expectations.expectSelectedEffort) {
    const coverage = evaluateSelectedEffortCoverage({ events: evidence.events, expectedEffort: expectations.expectSelectedEffort });
    results.passes.push(...coverage.passes);
    results.failures.push(...coverage.failures);
  }
  if (expectations.expectRequestedEffortMatchesSelection) {
    const applied = evaluateRequestedEffort({ events: evidence.events, subAgentModels: (expectations.subAgents ?? []).map(agent => agent.model) });
    results.passes.push(...applied.passes);
    results.failures.push(...applied.failures);
  }
  if (hasSessionContract(expectations)) appendSessionContract(results, evidence, expectations);
  if (expectations.steering) {
    // T4: the existing checks still pass alongside the steering checks.
    const failedIds = [...new Set(results.failures.map(line => line.split(" ")[1]))];
    const steering = evaluateSteeringEvidence({
      events: evidence.events,
      requests: evidence.requests,
      routingRecords: evidence.routingRecords,
      expectations: { steering: expectations.steering, executionOutcome: expectations.executionOutcome },
    });
    results.passes.push(...steering.passes);
    results.failures.push(...steering.failures);
    if (failedIds.length) results.failures.push(`FAIL T4 existing check(s) failed: ${failedIds.join(", ")}`);
    else results.passes.push(`PASS T4 all ${results.passes.length - steering.passes.length} existing check(s) passed`);
  }
  return results;
}

/**
 * @param {any} actionsCore
 * @param {RoutingAssertionResults} results
 * @param {string} label
 */
function reportResults(actionsCore, results, label) {
  for (const line of results.info ?? []) actionsCore.info(line);
  for (const line of results.passes) actionsCore.info(line);
  for (const line of results.failures) actionsCore.error(line);
  if (results.failures.length) {
    actionsCore.setFailed(`${label} smoke assertions failed (${results.failures.length}):\n${results.failures.join("\n")}`);
  } else {
    actionsCore.info(`All ${results.passes.length} ${label.toLowerCase()} smoke assertions passed`);
  }
}

/**
 * Post-step entry point: logs every check and fails the step on any failure.
 * @param {RoutingExpectations & { rootDir?: string, engine?: string, buildSession?: boolean, core?: any }} options
 * @returns {Promise<RoutingAssertionResults>}
 */
async function main(options) {
  const { core: actionsCore = global.core, ...rest } = options;
  const results = checkModelRoutingEvidence(rest);
  reportResults(actionsCore, results, "Model-routing");
  return results;
}

/**
 * Collect the unified session and evaluate E2 (session presence) and U1–U3,
 * without the routing checks.
 * @param {SessionContractExpectations & { rootDir?: string, engine?: string, buildSession?: boolean }} options
 * @returns {RoutingAssertionResults}
 */
function checkSessionContract(options) {
  const { rootDir, engine, buildSession, ...expectations } = options;
  const evidence = collectModelRoutingEvidence({ rootDir, engine, buildSession, sessionOnly: true });
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [], info: [] };
  if (evidence.problems.length) results.failures.push(...evidence.problems.map(problem => `FAIL E2 evidence: ${problem}`));
  else results.passes.push("PASS E2 evidence: unified session present");
  appendSessionContract(results, evidence, expectations);
  return results;
}

/**
 * Post-step entry point for unrouted smokes: logs E2 and U1–U3 and fails the
 * step on any failure.
 * @param {SessionContractExpectations & { rootDir?: string, engine?: string, buildSession?: boolean, core?: any }} options
 * @returns {Promise<RoutingAssertionResults>}
 */
async function sessionContractMain(options) {
  const { core: actionsCore = global.core, ...rest } = options;
  const results = checkSessionContract(rest);
  reportResults(actionsCore, results, "Unified-session");
  return results;
}

const DETECTION_ROOT_DIR = "/tmp/gh-aw/threat-detection";
const DETECTION_RESULT_PATH = "detection_result.json";
const DETECTION_VERDICT_FLAGS = ["prompt_injection", "secret_leak", "malicious_patch"];
const DETECTION_VERDICT_LISTS = ["reasons", "warnings"];
const MODEL_ROUTING_LOG = "model-routing.jsonl";
const RESOLVED_CONFIG_PATH = "sandbox/firewall/audit/awf-resolved-config.json";

/**
 * @typedef {{ detectionModel: string, allowedModels: string[], executionOutcome?: string }} DetectionExpectations
 * @typedef {{ D1?: string[], D2?: string[], D3?: string[] }} DetectionEvidenceProblems
 */

/** @param {any} value @returns {boolean} */
function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Evaluate the threat-detection run's own evidence: detection must complete on
 * its configured model and must not be routed.
 * @param {{ result?: any, requests: any[], routingEvents?: any[], resolvedConfig?: any, problems?: DetectionEvidenceProblems, expectations: DetectionExpectations }} input
 * @returns {RoutingAssertionResults}
 */
function evaluateDetectionEvidence({ result, requests, routingEvents = [], resolvedConfig, problems = {}, expectations }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [] };
  /** @param {string} id @param {string[]} found @param {string} message */
  const report = (id, found, message) => {
    if (found.length) results.failures.push(`FAIL ${id} ${found.join("; ")}`);
    else results.passes.push(`PASS ${id} ${message}`);
  };

  const allowedList = [...new Set((expectations.allowedModels ?? []).map(servedModel).filter(Boolean))];
  const allowed = new Set(allowedList);
  const detectionModel = servedModel(expectations.detectionModel);

  // D1: the detection step succeeded and the structured result parses with the
  // verdict keys. execution.json stays "started" after success, so it is not used.
  const d1 = [...(problems.D1 ?? [])];
  if (expectations.executionOutcome !== "success") d1.push(`detection execution outcome is ${expectations.executionOutcome || "<empty>"}, expected success`);
  if (!isPlainObject(result)) {
    if (!problems.D1?.length) d1.push(`no parsed ${DETECTION_RESULT_PATH}`);
  } else {
    const badKeys = [...DETECTION_VERDICT_FLAGS.filter(key => typeof result[key] !== "boolean"), ...DETECTION_VERDICT_LISTS.filter(key => !Array.isArray(result[key]))];
    if (badKeys.length) d1.push(`${DETECTION_RESULT_PATH} is missing or has malformed verdict key(s) [${badKeys.join(", ")}]; observed keys: [${Object.keys(result).join(", ")}]`);
  }
  report("D1", d1, `detection completed and ${DETECTION_RESULT_PATH} has verdict keys [${[...DETECTION_VERDICT_FLAGS, ...DETECTION_VERDICT_LISTS].join(", ")}]`);

  // D2: every detection request used the configured detection model, which must
  // be disjoint from the routing candidates.
  const d2 = [...(problems.D2 ?? [])];
  if (!detectionModel) d2.push("no configured detection model to check");
  else if (allowed.has(detectionModel)) d2.push(`configured detection model ${detectionModel} is in allowed-models [${allowedList.join(", ")}]; it must be disjoint from the routing candidates`);
  if (!requests.length) {
    if (!problems.D2?.length) d2.push("no detection requests in the api-proxy token usage");
  } else {
    const offModel = requests.filter(request => servedModel(request?.model) !== detectionModel);
    const routed = requests.filter(request => allowed.has(servedModel(request?.model)));
    if (offModel.length) d2.push(`detection request(s) not on configured model ${detectionModel || "<none>"}: ${describeRequests(offModel)}`);
    if (routed.length) d2.push(`detection request(s) on allowed-models [${allowedList.join(", ")}]: ${describeRequests(routed)}`);
  }
  report("D2", d2, `all ${requests.length} detection request(s) used ${detectionModel}, outside allowed-models [${allowedList.join(", ")}]`);

  // D3: detection was not routed.
  const d3 = [...(problems.D3 ?? [])];
  const classifierRequests = requests.filter(isClassifierRequest);
  if (classifierRequests.length) d3.push(`detection made ${classifierRequests.length} ${CLASSIFIER_PURPOSE} request(s): ${describeRequests(classifierRequests)}`);
  const selections = routingEvents.filter(event => event?.stage === "selection");
  if (selections.length)
    d3.push(
      `detection ${MODEL_ROUTING_LOG} has ${selections.length} selection event(s): ${selections.map(event => `${servedModel(event.wire_model ?? event.selected_model) || "<no model>"} ${normalizeEndpoint(event.endpoint) || "<no endpoint>"}`).join(", ")}`
    );
  if (!isPlainObject(resolvedConfig)) {
    if (!problems.D3?.length) d3.push("no parsed detection AWF resolved config");
  } else {
    const routingKeys = [];
    if (resolvedConfig.modelRouting !== undefined && resolvedConfig.modelRouting !== null) routingKeys.push("modelRouting");
    if (resolvedConfig.experimentalModelRouting === true) routingKeys.push("experimentalModelRouting");
    if (routingKeys.length) d3.push(`detection AWF resolved config has model-routing setting(s) [${routingKeys.join(", ")}]`);
  }
  report("D3", d3, `detection made no ${CLASSIFIER_PURPOSE} request, has no model-routing selection, and its AWF config has no model routing`);

  return results;
}

/**
 * @param {string} file
 * @param {string[]} problems
 * @returns {any}
 */
function readJsonFile(file, problems) {
  if (!fs.existsSync(file)) {
    problems.push(`missing ${file}`);
    return undefined;
  }
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch (error) {
    problems.push(`${file} is not valid JSON: ${getErrorMessage(error)}`);
    return undefined;
  }
}

/**
 * Collect the detection run's own evidence. The detection job copies its AWF
 * proxy and audit logs under the threat-detection directory before post-steps run.
 * @param {{ rootDir?: string }} [options]
 * @returns {{ result: any, requests: any[], routingEvents: any[], resolvedConfig: any, problems: Required<DetectionEvidenceProblems> }}
 */
function collectDetectionEvidence({ rootDir = DETECTION_ROOT_DIR } = {}) {
  /** @type {Required<DetectionEvidenceProblems>} */
  const problems = { D1: [], D2: [], D3: [] };

  const result = readJsonFile(path.join(rootDir, DETECTION_RESULT_PATH), problems.D1);

  /** @type {string[]} */
  const tokenProblems = [];
  let requests = [];
  const tokenFile = TOKEN_USAGE_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file));
  if (tokenFile) {
    requests = readJsonl(tokenFile, tokenProblems).filter(record => isPlainObject(record) && (record.event === undefined || record.event === "token_usage"));
  } else {
    tokenProblems.push(`missing api-proxy token-usage.jsonl (looked in ${TOKEN_USAGE_PATHS.join(", ")})`);
  }
  // D3's classifier check reads the same requests, so token-usage problems fail both.
  problems.D2.push(...tokenProblems);
  problems.D3.push(...tokenProblems);

  const routingEvents = [];
  for (const file of [...new Set(TOKEN_USAGE_PATHS.map(tokenPath => path.join(rootDir, path.dirname(tokenPath), MODEL_ROUTING_LOG)))]) {
    if (fs.existsSync(file)) routingEvents.push(...readJsonl(file, problems.D3));
  }

  const resolvedConfig = readJsonFile(path.join(rootDir, RESOLVED_CONFIG_PATH), problems.D3);
  return { result, requests, routingEvents, resolvedConfig, problems };
}

/**
 * Collect detection evidence and evaluate D1–D3.
 * @param {DetectionExpectations & { rootDir?: string }} options
 * @returns {RoutingAssertionResults}
 */
function checkDetectionEvidence(options) {
  const { rootDir, ...expectations } = options;
  const evidence = collectDetectionEvidence({ rootDir });
  return evaluateDetectionEvidence({ ...evidence, expectations });
}

/**
 * Detection-job post-step entry point: logs every check and fails the step on any failure.
 * @param {DetectionExpectations & { rootDir?: string, core?: any }} options
 * @returns {Promise<RoutingAssertionResults>}
 */
async function detectionMain(options) {
  const { core: actionsCore = global.core, ...rest } = options;
  const results = checkDetectionEvidence(rest);
  for (const line of results.passes) actionsCore.info(line);
  for (const line of results.failures) actionsCore.error(line);
  if (results.failures.length) {
    actionsCore.setFailed(`Threat-detection smoke assertions failed (${results.failures.length}):\n${results.failures.join("\n")}`);
  } else {
    actionsCore.info(`All ${results.passes.length} threat-detection smoke assertions passed`);
  }
  return results;
}

module.exports = {
  CLASSIFIER_PURPOSE,
  TOKEN_USAGE_PATHS,
  MODEL_ROUTING_PATHS,
  STEERING_THRESHOLDS,
  servedModel,
  normalizeEndpoint,
  supportedEndpoints,
  supportedEfforts,
  evaluateModelRoutingEvidence,
  evaluateSteeringEvidence,
  evaluateSelectedEffortCoverage,
  evaluateRequestedEffort,
  collectModelRoutingEvidence,
  checkModelRoutingEvidence,
  main,
  evaluateSessionContract,
  checkSessionContract,
  sessionContractMain,
  evaluateDetectionEvidence,
  collectDetectionEvidence,
  checkDetectionEvidence,
  detectionMain,
};
