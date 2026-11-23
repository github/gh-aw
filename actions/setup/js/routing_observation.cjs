// @ts-check
"use strict";

/**
 * The verdict on a routed run: whether its result counts as evidence about the
 * pair AWF selected, and if not, why. The unified session already holds AWF's
 * selection, its record of every request and each request's priced usage; the
 * verdict is the one fact it lacks, because it needs the conclusion job's final
 * job results and validated safe outputs.
 */

const { attributeRequest, routedRequests } = require("./routing_analytics.cjs");

/** @typedef {import("./types/unified_session").RoutingObservationData} RoutingObservationData */
/** @typedef {import("./types/unified_session").RoutingSkipReason} RoutingSkipReason */
/** @typedef {{terminal: string[], errors: number, agentIncomplete: boolean, infrastructure: boolean, failureCause?: string}} AgentOutputSummary */

/**
 * @typedef {object} RunEvidence
 * @property {"match" | "mismatch" | "unknown"} pair whether the agent's requests ran the selected pair
 * @property {string | undefined} agentResult
 * @property {{outcome?: string, failureClasses: string[]} | undefined} harness the final harness attempt
 * @property {boolean} startupFailed
 * @property {{categories: string[], errorCodes: Array<string | number>}} execution
 * @property {number[]} proxyStatuses HTTP statuses AWF recorded for the agent's failed requests
 * @property {boolean} contentFiltered a content filter blocked a response in the main conversation
 * @property {AgentOutputSummary | undefined} output
 * @property {"clean" | "threat" | "inconclusive"} detection
 */

const CLASSIFIER_PURPOSE = "routing_classification";
const NON_TERMINAL_OUTPUTS = new Set(["report_missing_tool", "report_missing_data", "report_incomplete", "request_continuation"]);
const THREAT_FLAGS = ["prompt_injection", "promptInjection", "secret_leak", "secretLeak", "malicious_patch", "maliciousPatch"];
const INCONCLUSIVE_DETECTION_REASONS = new Set(["agent_failure", "parse_error", "github_api_quota_exhausted"]);

// Platform causes. Each set names evidence about the platform, never about the
// model's answer; a run that succeeded is not re-examined for them. The
// collector's driver_crash cause also covers unclassified nonzero exits, so a
// crash counts only through its own evidence: a crash signal or an interrupted attempt.
const INFRASTRUCTURE_FAILURE_CAUSES = new Set(["agent_startup_failure", "secret_redaction_failure", "engine_outage"]);
// Generic labels a guard's budget rejection can arrive wrapped in; beside a confirmed limit they are the model's.
const GUARD_WRAPPERS = new Set(["authentication_failed", "api_proxy_guard_rejected"]);
const INFRASTRUCTURE_CATEGORIES = new Set([
  "agentic_engine_timeout",
  "authentication_failed",
  "awf_api_proxy_blocking_requests",
  "capi_server_error",
  "goal_already_active",
  "inference_access_error",
  "mcp_policy_error",
  "missing_model_pricing_error",
  "model_not_supported_error",
  "sandbox_runtime_crash",
]);
const INFRASTRUCTURE_CLASSES = new Set([
  "api_proxy_guard_rejected",
  "authentication_failed",
  "capi_server_error",
  "connection_refused",
  "invalid_model",
  "mcp_gateway_shutdown",
  "mcp_policy_blocked",
  "missing_api_key",
  "model_endpoint_mismatch",
  "model_not_supported",
  "model_routing_failed",
  "no_auth_info",
  "overloaded_error",
  "sandbox_runtime_crash",
  "sdk_session_idle_timeout",
  "server_error",
  "startup_error",
  "unsupported_model_tools",
  "watchdog",
]);
const RATE_LIMIT_CATEGORIES = new Set(["ai_credits_rate_limit_error", "capi_quota_exceeded_error"]);
const RATE_LIMIT_CLASSES = new Set(["capi_quota_exceeded", "rate_limit_error", "rate_limit_wait_exceeded", "token_per_minute_rate_limit"]);
const CANCELLED_CLASSES = new Set(["cancelled", "cancelled_or_timed_out"]);
// Limits the selected pair reached on its own: steps, tokens, credits, tool denials or a content filter.
const BEHAVIOR_CATEGORIES = new Set(["effective_tokens_limit_exceeded", "invocation_cap_exceeded", "max_ai_credits_exceeded", "permission_denied_limit_exceeded"]);
const BEHAVIOR_CLASSES = new Set(["ai_credits_exhausted", "content_filter", "invocation_cap_exceeded", "max_turns"]);

/** @param {unknown} value @returns {value is Record<string, any>} */
const isRecord = value => value !== null && typeof value === "object" && !Array.isArray(value);

/**
 * Whether AWF routed the run, whether its agent made requests, whether they
 * ran the selected pair, and the HTTP statuses AWF recorded for the ones that
 * failed. Requests are attributed exactly as routing analytics attributes them;
 * the router's own classifier call is not the agent's.
 * @param {{tokenUsage: unknown[], routing: unknown[], upstreamErrors?: unknown[], subAgentModels?: unknown}} records agent-phase AWF records
 * @returns {{routed: boolean, started: boolean, pair: RunEvidence["pair"], statuses: number[]}}
 */
function routedRun({ tokenUsage, routing, upstreamErrors = [], subAgentModels }) {
  const routes = routedRequests(routing);
  const classifier = new Set(tokenUsage.flatMap(record => (isRecord(record) && record.purpose === CLASSIFIER_PURPOSE ? [record.request_id] : [])));
  for (const [id, route] of routes) if (route?.purpose === CLASSIFIER_PURPOSE) classifier.add(id);
  const counts = { main: 0, subagent: 0, deviated: 0, unattributed: 0 };
  const seen = new Set();
  for (const record of tokenUsage) {
    if (!isRecord(record) || record.purpose === CLASSIFIER_PURPOSE) continue;
    const id = record.request_id;
    if (typeof id !== "string" || !id) {
      counts.unattributed++;
      continue;
    }
    if (seen.has(id)) continue;
    seen.add(id);
    counts[attributeRequest(routes.get(id), record.provider, subAgentModels)]++;
  }
  // Requests AWF routed but recorded no usage for, such as ones rejected upstream.
  for (const [id, route] of routes) {
    if (seen.has(id) || classifier.has(id)) continue;
    seen.add(id);
    counts[attributeRequest(route, route?.provider, subAgentModels)]++;
  }
  return {
    routed: routing.some(record => isRecord(record) && record.stage === "selection"),
    started: counts.main + counts.subagent + counts.deviated + counts.unattributed > 0,
    pair: counts.deviated > 0 ? "mismatch" : counts.main > 0 && counts.unattributed === 0 ? "match" : "unknown",
    statuses: upstreamErrors.flatMap(record => (isRecord(record) && !classifier.has(record.request_id) && Number.isInteger(record.status) ? [record.status] : [])),
  };
}

/**
 * Summarize the validated safe outputs the agent job wrote (agent_output.json).
 * @param {unknown} value
 * @returns {AgentOutputSummary | undefined}
 */
function summarizeAgentOutput(value) {
  if (!isRecord(value) || !Array.isArray(value.items) || !Array.isArray(value.errors)) return undefined;
  const types = value.items.map(item => (isRecord(item) && typeof item.type === "string" ? item.type : ""));
  // The collector adds one report_incomplete of its own when no terminal output was found.
  const collectorCause = typeof value.collectorFailureCause === "string" ? value.collectorFailureCause : typeof value.collectorEmptyOutputCause === "string" ? "unknown" : undefined;
  const reports = value.items.filter(item => isRecord(item) && item.type === "report_incomplete");
  // Engine runtimes file their own report when the provider fails.
  const platformReports = reports.filter(item => item.reason === "infrastructure_error").length;
  return {
    terminal: types.filter(type => type && !NON_TERMINAL_OUTPUTS.has(type)),
    errors: value.errors.length,
    agentIncomplete: reports.length - platformReports > (collectorCause === undefined ? 0 : 1),
    infrastructure: platformReports > 0 || typeof value.collectorInfrastructureCause === "string",
    ...(collectorCause === undefined ? {} : { failureCause: collectorCause }),
  };
}

/**
 * Threat detection's verdict: a threat found, a result it could not reach, or
 * clean. A run without detection results is clean.
 * @param {unknown[]} results detection.result data
 * @returns {RunEvidence["detection"]}
 */
function detectionVerdict(results) {
  const records = results.filter(isRecord);
  if (records.some(result => result.reason === "threat_detected" || THREAT_FLAGS.some(key => result[key] === true))) return "threat";
  const inconclusive = records.some(result => INCONCLUSIVE_DETECTION_REASONS.has(result.reason) || ["failure", "cancelled"].includes(result.job_result ?? result.jobResult));
  return inconclusive ? "inconclusive" : "clean";
}

/** @param {RunEvidence} e */
const statuses = e => [...e.execution.errorCodes.map(code => (typeof code === "number" ? code : /^\d{3}$/.test(code) ? Number(code) : NaN)).filter(Number.isInteger), ...e.proxyStatuses];

/** @param {RunEvidence} e */
function limitReached(e) {
  return e.execution.categories.some(value => BEHAVIOR_CATEGORIES.has(value)) || (e.harness?.failureClasses ?? []).some(value => BEHAVIOR_CLASSES.has(value));
}

/** @param {RunEvidence} e */
function cancelled(e) {
  return e.agentResult === "cancelled" || e.harness?.outcome === "cancelled" || (e.harness?.failureClasses ?? []).some(value => CANCELLED_CLASSES.has(value));
}

/** @param {RunEvidence} e */
function infrastructure(e) {
  const limit = limitReached(e);
  /** @param {string} value */
  const platform = value => !(limit && GUARD_WRAPPERS.has(value));
  return (
    e.output?.infrastructure === true ||
    (e.output?.failureCause !== undefined && INFRASTRUCTURE_FAILURE_CAUSES.has(e.output.failureCause)) ||
    e.startupFailed ||
    // The harness marks an attempt interrupted when its runner itself failed.
    e.harness?.outcome === "interrupted" ||
    e.execution.categories.some(value => INFRASTRUCTURE_CATEGORIES.has(value) && platform(value)) ||
    statuses(e).some(status => status >= 500 && status <= 599) ||
    (e.harness?.failureClasses ?? []).some(value => INFRASTRUCTURE_CLASSES.has(value) && platform(value))
  );
}

/** @param {RunEvidence} e */
function rateLimited(e) {
  return e.execution.categories.some(value => RATE_LIMIT_CATEGORIES.has(value)) || statuses(e).includes(429) || (e.harness?.failureClasses ?? []).some(value => RATE_LIMIT_CLASSES.has(value));
}

/** @param {RunEvidence} e */
function behavioralFailure(e) {
  return e.output?.agentIncomplete === true || e.output?.failureCause === "prompt_exhaustion" || e.contentFiltered || e.execution.errorCodes.includes("content_filter") || limitReached(e);
}

/**
 * Apply the precedence rules: cancellation, then platform causes for a run that
 * did not succeed, then whether the selected pair ran, and only then the result.
 * @param {RunEvidence} e
 * @returns {RoutingObservationData}
 */
function routingOutcome(e) {
  /** @param {RoutingSkipReason} reason @returns {RoutingObservationData} */
  const skip = reason => ({ outcome: "skip", reason });
  const { output } = e;
  const outputValid = output !== undefined && output.errors === 0 && !output.agentIncomplete && output.failureCause === undefined && output.terminal.length > 0;
  const success = e.agentResult === "success" && outputValid && e.detection === "clean";
  if (cancelled(e)) return skip("cancelled");
  if (!success && infrastructure(e)) return skip("infrastructure");
  if (!success && rateLimited(e)) return skip("rate_limited");
  if (e.pair !== "match") return skip(e.pair === "mismatch" ? "pair_mismatch" : "unknown_evidence");
  // Output that threat detection flagged is not evidence about the model.
  if (e.detection === "threat") return skip("unknown_evidence");
  if (success) return { outcome: "success" };
  if (behavioralFailure(e) || (output !== undefined && (output.errors > 0 || output.failureCause === "missing_terminal_safe_output"))) return { outcome: "failure" };
  return skip("unknown_evidence");
}

module.exports = {
  routedRun,
  summarizeAgentOutput,
  detectionVerdict,
  routingOutcome,
};
