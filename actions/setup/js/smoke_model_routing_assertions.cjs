// @ts-check
"use strict";

/**
 * Deterministic model-routing smoke assertions.
 *
 * Smoke workflows call this from a `post-steps` github-script step. Every check
 * reads runner- and proxy-written evidence (the unified session and the
 * api-proxy token-usage log), never the agent's own reply. The agent-writable
 * `agent/awf-routing-outcome.json` (`model_routing.outcome`) is never evidence.
 *
 * Check IDs:
 * - E1 agent execution outcome; E2 evidence files present and parseable
 * - R1 routing selected; R2 one classifier call; R3 agent traffic on the selected
 *   model and a supported endpoint; R4 no out-of-policy models
 * - M1 main-agent requests use the expected endpoint (when configured)
 * - S1 sub-agent ran exactly once; S2 completed without failing; S3 status 200 on
 *   the declared model and endpoint; S4 events carry the declared agent name
 *   (when configured)
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_ROOT_DIR = "/tmp/gh-aw";
const SESSION_PATH = "usage/aw_session.jsonl";
const TOKEN_USAGE_PATHS = ["sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl", "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"];
const REFLECT_PATHS = ["sandbox/firewall/awf-reflect.json", "agent/awf-reflect.json"];
const CLASSIFIER_PURPOSE = "routing_classification";
const MAX_OBSERVED = 12;

/**
 * @typedef {{ name: string, model: string, endpoint: string | string[] }} SubAgentExpectation
 * @typedef {{
 *   allowedModels: string[],
 *   mainEndpoint?: string,
 *   subAgents?: SubAgentExpectation[],
 *   requireDeclaredAgentNames?: boolean,
 *   executionOutcome?: string,
 * }} RoutingExpectations
 * @typedef {{ passes: string[], failures: string[] }} RoutingAssertionResults
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
 * Evaluate model-routing evidence against the smoke expectations.
 * @param {{ events: any[], requests: any[], reflect?: any, expectations: RoutingExpectations }} input
 * @returns {RoutingAssertionResults}
 */
function evaluateModelRoutingEvidence({ events, requests, reflect = null, expectations }) {
  /** @type {RoutingAssertionResults} */
  const results = { passes: [], failures: [] };
  /** @param {string} id @param {string} message */
  const pass = (id, message) => results.passes.push(`PASS ${id} ${message}`);
  /** @param {string} id @param {string} message */
  const fail = (id, message) => results.failures.push(`FAIL ${id} ${message}`);

  const allowedList = [...new Set((expectations.allowedModels ?? []).map(servedModel).filter(Boolean))];
  const allowed = new Set(allowedList);
  const subAgents = expectations.subAgents ?? [];
  const subAgentModels = new Set(subAgents.map(agent => servedModel(agent.model)));

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

  // R3: agent traffic on the selected model and a supported endpoint.
  const agentRequests = requests.filter(request => !isClassifierRequest(request));
  if (!selectedModel) {
    fail("R3", `no selected model to verify; observed agent requests: ${describeRequests(agentRequests)}`);
  } else {
    const endpoints = supportedEndpoints(selectedModel, selection, reflect);
    const onModel = agentRequests.filter(request => servedModel(request.model) === selectedModel);
    if (!endpoints.size) {
      fail("R3", `no supported-endpoint metadata for ${selectedModel} in the routing selection or /reflect; observed: ${describeRequests(onModel.length ? onModel : agentRequests)}`);
    } else {
      const ok = onModel.find(request => Number(request.status) === 200 && endpoints.has(requestEndpoint(request)));
      if (ok) pass("R3", `agent request on selected model: ${describeRequest(ok)}`);
      else
        fail(
          "R3",
          `no 200 request for ${selectedModel} on a supported endpoint (${[...endpoints].join(", ")}); observed: ${onModel.length ? describeRequests(onModel) : `no ${selectedModel} requests; other requests: ${describeRequests(agentRequests)}`}`
        );
    }
  }

  // R4: no out-of-policy models.
  const permitted = new Set([...allowed, ...subAgentModels]);
  const outOfPolicy = agentRequests.filter(request => !permitted.has(servedModel(request.model)));
  if (outOfPolicy.length)
    fail("R4", `request(s) on models outside allowed-models [${allowedList.join(", ")}]${subAgentModels.size ? ` and declared sub-agent models [${[...subAgentModels].join(", ")}]` : ""}: ${describeRequests(outOfPolicy)}`);
  else pass("R4", `all ${agentRequests.length} agent request(s) used permitted models`);

  // M1: main-agent requests use the expected endpoint.
  if (expectations.mainEndpoint) {
    const expected = normalizeEndpoint(expectations.mainEndpoint);
    const mainRequests = agentRequests.filter(request => {
      const model = servedModel(request.model);
      return allowed.has(model) && !subAgentModels.has(model);
    });
    const wrong = mainRequests.filter(request => requestEndpoint(request) !== expected);
    if (!mainRequests.length) fail("M1", `no main-agent requests on allowed models to check for ${expected}; observed: ${describeRequests(agentRequests)}`);
    else if (wrong.length) fail("M1", `main-agent request(s) not on ${expected}; observed: ${describeRequests(wrong)}`);
    else pass("M1", `all ${mainRequests.length} main-agent request(s) used ${expected}`);
  }

  const subagentEvents = events.filter(event => typeof event?.type === "string" && event.type.startsWith("subagent."));
  const startedEvents = subagentEvents.filter(event => event.type === "subagent.started");
  const observedNames = [...new Set(startedEvents.map(event => event.data?.agentName ?? "<none>"))];
  for (const agent of subAgents) {
    const model = servedModel(agent.model);
    const endpoints = (Array.isArray(agent.endpoint) ? agent.endpoint : [agent.endpoint]).map(normalizeEndpoint);
    const label = `sub-agent ${agent.name}`;

    // S1: ran exactly once.
    const started = startedEvents.filter(event => event.data?.agentName === agent.name);
    if (started.length === 1) pass("S1", `${label} started once`);
    else fail("S1", `${label}: expected exactly 1 subagent.started, observed ${started.length}; observed agent names: [${observedNames.join(", ")}]`);

    const ids = new Set(started.map(event => event.data?.invocationId ?? event.agentId).filter(Boolean));
    const activity = subagentEvents.filter(event => ids.has(event.data?.invocationId) || ids.has(event.agentId));

    // S2: completed and did not fail.
    const completed = activity.filter(event => event.type === "subagent.completed");
    const failed = activity.filter(event => event.type === "subagent.failed");
    if (!started.length) fail("S2", `${label}: never started; observed events: ${describeEvents(subagentEvents)}`);
    else if (!completed.length || failed.length) fail("S2", `${label}: completed=${completed.length} failed=${failed.length}; observed events: ${describeEvents(activity)}`);
    else pass("S2", `${label} completed`);

    // S3: status 200 on the declared model and endpoint; recorded models match.
    const onModel = requests.filter(request => !isClassifierRequest(request) && servedModel(request.model) === model);
    const ok = onModel.find(request => Number(request.status) === 200 && endpoints.includes(requestEndpoint(request)));
    const recorded = activity.flatMap(event => [event.data?.model, event.data?.selectedModel, event.data?.resolvedModel, event.data?.firstDispatchedModel]).filter(value => typeof value === "string" && value !== "");
    const mismatched = [...new Set(recorded.filter(value => servedModel(value) !== model))];
    const s3Problems = [];
    if (!ok) s3Problems.push(`no 200 request for ${model} on ${endpoints.join(" or ")}`);
    if (mismatched.length) s3Problems.push(`recorded model(s) [${mismatched.join(", ")}] differ from declared ${model}`);
    if (s3Problems.length) {
      const observed = onModel.length ? onModel.map(request => `${requestEndpoint(request) || "<no endpoint>"} ${request.status ?? "<no status>"}`).join(", ") : `no ${model} requests; other requests: ${describeRequests(agentRequests)}`;
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
 * @param {string} file
 * @param {string[]} problems
 * @returns {any[]}
 */
function readJsonl(file, problems) {
  const records = [];
  const lines = fs.readFileSync(file, "utf8").split("\n");
  for (const [index, line] of lines.entries()) {
    if (!line.trim()) continue;
    try {
      records.push(JSON.parse(line));
    } catch (error) {
      problems.push(`${file}:${index + 1} is not valid JSON: ${getErrorMessage(error)}`);
    }
  }
  return records;
}

/**
 * Collect evidence from the gh-aw temp directory. Post-steps run before the
 * usage-artifact step writes aw_session.jsonl, so the unified session is built first.
 * @param {{ rootDir?: string, engine?: string, buildSession?: boolean }} [options]
 * @returns {{ events: any[], requests: any[], reflect: any, problems: string[] }}
 */
function collectModelRoutingEvidence({ rootDir = DEFAULT_ROOT_DIR, engine, buildSession = true } = {}) {
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
  if (fs.existsSync(sessionFile)) events = readJsonl(sessionFile, problems);
  else problems.push(`missing ${SESSION_PATH}`);

  let requests = [];
  const tokenFile = TOKEN_USAGE_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file));
  if (tokenFile) {
    requests = readJsonl(tokenFile, problems).filter(record => record && typeof record === "object" && !Array.isArray(record) && (record.event === undefined || record.event === "token_usage"));
  } else {
    problems.push(`missing api-proxy token-usage.jsonl (looked in ${TOKEN_USAGE_PATHS.join(", ")})`);
  }

  let reflect = null;
  const reflectFile = REFLECT_PATHS.map(file => path.join(rootDir, file)).find(file => fs.existsSync(file));
  if (reflectFile) {
    try {
      reflect = JSON.parse(fs.readFileSync(reflectFile, "utf8"));
    } catch (error) {
      problems.push(`${reflectFile} is not valid JSON: ${getErrorMessage(error)}`);
    }
  }
  return { events, requests, reflect, problems };
}

/**
 * Collect evidence and evaluate all checks.
 * @param {RoutingExpectations & { rootDir?: string, engine?: string, buildSession?: boolean }} options
 * @returns {RoutingAssertionResults}
 */
function checkModelRoutingEvidence(options) {
  const { rootDir, engine, buildSession, ...expectations } = options;
  const evidence = collectModelRoutingEvidence({ rootDir, engine, buildSession });
  const results = evaluateModelRoutingEvidence({ events: evidence.events, requests: evidence.requests, reflect: evidence.reflect, expectations });
  if (evidence.problems.length) results.failures.unshift(...evidence.problems.map(problem => `FAIL E2 evidence: ${problem}`));
  else results.passes.unshift("PASS E2 evidence: unified session and api-proxy token usage present");
  return results;
}

/**
 * Post-step entry point: logs every check and fails the step on any failure.
 * @param {RoutingExpectations & { rootDir?: string, engine?: string, buildSession?: boolean, core?: any }} options
 * @returns {Promise<RoutingAssertionResults>}
 */
async function main(options) {
  const { core: actionsCore = global.core, ...rest } = options;
  const results = checkModelRoutingEvidence(rest);
  for (const line of results.passes) actionsCore.info(line);
  for (const line of results.failures) actionsCore.error(line);
  if (results.failures.length) {
    actionsCore.setFailed(`Model-routing smoke assertions failed (${results.failures.length}):\n${results.failures.join("\n")}`);
  } else {
    actionsCore.info(`All ${results.passes.length} model-routing smoke assertions passed`);
  }
  return results;
}

module.exports = {
  CLASSIFIER_PURPOSE,
  TOKEN_USAGE_PATHS,
  servedModel,
  normalizeEndpoint,
  supportedEndpoints,
  evaluateModelRoutingEvidence,
  collectModelRoutingEvidence,
  checkModelRoutingEvidence,
  main,
};
