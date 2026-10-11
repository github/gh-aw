// @ts-check
/// <reference types="@actions/github-script" />
require("./shim.cjs");

/**
 * emit_outcome_spans.cjs
 *
 * Reads per-item outcome evaluations from /tmp/gh-aw/outcome-evaluations.jsonl
 * and the fleet summary from /tmp/gh-aw/outcome-summary.json, then emits OTLP
 * spans for each evaluated outcome item plus one fleet summary span.
 *
 * Designed to run as a pre-agent step in the outcome-collector workflow so that
 * outcome data is available in OTEL dashboards (Grafana, Datadog, Honeycomb)
 * for per-workflow, per-type, and per-result drill-down.
 *
 * Span naming:
 *   - Per-item:  gh-aw.outcome.evaluation
 *   - Summary:   gh-aw.outcome.summary
 *
 * Errors are non-fatal: export failures must never break the workflow.
 */

const fs = require("fs");
const { nowMs } = require("./performance_now.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const {
  generateTraceId,
  generateSpanId,
  buildAttr,
  buildOTLPBatchPayload,
  buildOTLPSpan,
  buildGitHubActionsResourceAttributes,
  parseOTLPEndpoints,
  sendOTLPToAllEndpoints,
  appendToOTLPJSONL,
  readJSONIfExists,
  buildCustomOTLPAttributes,
} = require("./send_otlp_span.cjs");

const AW_INFO_PATH = "/tmp/gh-aw/aw_info.json";
const EVALUATIONS_PATH = "/tmp/gh-aw/outcome-evaluations.jsonl";
const SUMMARY_PATH = "/tmp/gh-aw/outcome-summary.json";
const OTLP_STATUS_UNSET = 0;
const OTLP_STATUS_OK = 1;
const OTLP_STATUS_ERROR = 2;

/**
 * Read a JSONL file, returning an array of parsed objects.
 * Silently skips invalid lines.
 * @param {string} filePath
 * @returns {any[]}
 */
function readJSONL(filePath) {
  try {
    const lines = fs.readFileSync(filePath, "utf8").split("\n");
    /** @type {any[]} */
    const items = [];
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        items.push(JSON.parse(trimmed));
      } catch {
        // Invalid NDJSON line — ignored.
      }
    }
    return items;
  } catch {
    return [];
  }
}

async function main() {
  const evaluations = readJSONL(EVALUATIONS_PATH);
  const summary = readJSONIfExists(SUMMARY_PATH);
  const awInfo = readJSONIfExists(AW_INFO_PATH) || {};

  if (evaluations.length === 0 && (!summary || summary.total_outcomes === 0)) {
    core.info("[outcome-otel] No outcome evaluations to export");
    return;
  }

  const endpoints = parseOTLPEndpoints();
  const hasEndpoints = endpoints.length > 0;

  if (!hasEndpoints) {
    core.info("[outcome-otel] No OTLP endpoints configured, writing JSONL mirror only");
  }

  // Read aw_info.json first: GH_AW_INFO_* env vars are only present during setup,
  // while aw_info.json is the authoritative runtime source for later
  // github-script steps. Prefer cli_version for CLI-named version dimensions,
  // and only fall back to engine version fields when CLI version is unavailable.
  const staged = awInfo.staged === true || process.env.GH_AW_INFO_STAGED === "true";
  const scopeVersion = (typeof awInfo.cli_version === "string" ? awInfo.cli_version : "") || process.env.GH_AW_INFO_CLI_VERSION || awInfo.agent_version || awInfo.version || process.env.GH_AW_INFO_VERSION || "unknown";
  const traceId = (process.env.GITHUB_AW_OTEL_TRACE_ID || "").trim().toLowerCase() || generateTraceId();
  const parentSpanId = (process.env.GITHUB_AW_OTEL_PARENT_SPAN_ID || "").trim().toLowerCase() || "";
  const summarySpanId = generateSpanId();
  const serviceName = process.env.OTEL_SERVICE_NAME || "gh-aw";
  const repository = process.env.GITHUB_REPOSITORY || "";
  const runId = process.env.GITHUB_RUN_ID || "";
  const runAttempt = process.env.GITHUB_RUN_ATTEMPT || "1";
  const eventName = process.env.GITHUB_EVENT_NAME || "";
  const ref = process.env.GITHUB_REF || "";
  const refName = process.env.GITHUB_REF_NAME || "";
  const sha = process.env.GITHUB_SHA || "";
  const job = process.env.GITHUB_JOB || "";
  const workflowRef = process.env.GITHUB_WORKFLOW_REF || "";
  const actorId = process.env.GITHUB_ACTOR_ID || "";
  const runnerOs = process.env.RUNNER_OS || "";
  const runnerArch = process.env.RUNNER_ARCH || "";
  const runnerName = process.env.RUNNER_NAME || "";
  const runnerEnvironment = process.env.RUNNER_ENVIRONMENT || "";

  const resourceAttributes = buildGitHubActionsResourceAttributes({
    repository,
    runId,
    eventName,
    ref,
    refName,
    sha,
    job,
    workflowRef,
    actorId,
    runnerOs,
    runnerArch,
    runnerName,
    runnerEnvironment,
    staged,
    runAttempt,
  });

  const evalEndMs = nowMs();

  // -------------------------------------------------------------------------
  // Per-item outcome spans
  // -------------------------------------------------------------------------
  const itemSpans = [];

  for (const eval_ of evaluations) {
    const type = typeof eval_.type === "string" ? eval_.type : "";
    const result = typeof eval_.result === "string" ? eval_.result : "unknown";
    // Fall back to the legacy result field so older JSONL artifacts still render
    // useful spans while newer artifacts carry explicit normalized fields.
    const outcomeStatus = typeof eval_.outcome_status === "string" ? eval_.outcome_status : result;
    const evidenceStrength = typeof eval_.evidence_strength === "string" ? eval_.evidence_strength : "weak";
    const signal = typeof eval_.signal === "string" ? eval_.signal : "";
    const detail = typeof eval_.detail === "string" ? eval_.detail : "";
    const workflow = typeof eval_.workflow === "string" ? eval_.workflow : "";
    const sourceRunId = typeof eval_.run_id === "number" ? eval_.run_id : 0;
    const url = typeof eval_.url === "string" ? eval_.url : "";
    const repo = typeof eval_.repo === "string" ? eval_.repo : "";
    const timestamp = typeof eval_.timestamp === "string" ? eval_.timestamp : "";
    const event = typeof eval_.event === "string" ? eval_.event : "";
    const resolutionSec = typeof eval_.resolution_sec === "number" ? eval_.resolution_sec : null;
    const pendingAgeSec = typeof eval_.pending_age_sec === "number" ? eval_.pending_age_sec : null;
    const reviewComments = typeof eval_.review_comments === "number" ? eval_.review_comments : null;
    const changedFiles = typeof eval_.changed_files === "number" ? eval_.changed_files : null;
    const additions = typeof eval_.additions === "number" ? eval_.additions : null;
    const deletions = typeof eval_.deletions === "number" ? eval_.deletions : null;
    const reactionsTotal = typeof eval_.reactions_total === "number" ? eval_.reactions_total : null;
    const reactionsPositive = typeof eval_.reactions_positive === "number" ? eval_.reactions_positive : null;
    const reactionsNegative = typeof eval_.reactions_negative === "number" ? eval_.reactions_negative : null;
    const comments = typeof eval_.comments === "number" ? eval_.comments : null;
    const zeroTouch = eval_.zero_touch === true;

    const attributes = [
      buildAttr("gh-aw.exporter.name", "outcome-collector"),
      buildAttr("gh-aw.outcome.type", type),
      buildAttr("gh-aw.outcome.result", result),
      buildAttr("gh-aw.outcome.outcome_status", outcomeStatus),
      buildAttr("gh-aw.outcome.evidence_strength", evidenceStrength),
      buildAttr("gh-aw.outcome.workflow", workflow),
      buildAttr("gh-aw.outcome.run_id", sourceRunId),
      buildAttr("gh-aw.outcome.repo", repo),
    ];

    if (url) attributes.push(buildAttr("gh-aw.outcome.url", url));
    if (detail) attributes.push(buildAttr("gh-aw.outcome.detail", detail));
    if (signal) attributes.push(buildAttr("gh-aw.outcome.signal", signal));
    if (timestamp) attributes.push(buildAttr("gh-aw.outcome.created_at", timestamp));
    if (event) attributes.push(buildAttr("gh-aw.outcome.event", event));
    if (resolutionSec !== null) attributes.push(buildAttr("gh-aw.outcome.resolution_sec", resolutionSec));
    if (pendingAgeSec !== null) attributes.push(buildAttr("gh-aw.outcome.pending_age_sec", pendingAgeSec));
    if (reviewComments !== null) attributes.push(buildAttr("gh-aw.outcome.review_comments", reviewComments));
    if (changedFiles !== null) attributes.push(buildAttr("gh-aw.outcome.changed_files", changedFiles));
    if (additions !== null) attributes.push(buildAttr("gh-aw.outcome.additions", additions));
    if (deletions !== null) attributes.push(buildAttr("gh-aw.outcome.deletions", deletions));
    if (reactionsTotal !== null) attributes.push(buildAttr("gh-aw.outcome.reactions_total", reactionsTotal));
    if (reactionsPositive !== null) attributes.push(buildAttr("gh-aw.outcome.reactions_positive", reactionsPositive));
    if (reactionsNegative !== null) attributes.push(buildAttr("gh-aw.outcome.reactions_negative", reactionsNegative));
    if (comments !== null) attributes.push(buildAttr("gh-aw.outcome.comments", comments));
    for (const field of ["human_comments", "human_reviews", "human_edits"]) {
      if (typeof eval_[field] === "number") attributes.push(buildAttr(`gh-aw.outcome.${field}`, eval_[field]));
    }
    if (zeroTouch) attributes.push(buildAttr("gh-aw.outcome.zero_touch", true));

    // Map normalized outcome_status to OTLP status: accepted=OK, rejected=ERROR, all others=UNSET
    const statusCode = outcomeStatus === "rejected" ? OTLP_STATUS_ERROR : outcomeStatus === "accepted" ? OTLP_STATUS_OK : OTLP_STATUS_UNSET;

    itemSpans.push(
      buildOTLPSpan({
        traceId,
        spanId: generateSpanId(),
        parentSpanId: summarySpanId,
        spanName: "gh-aw.outcome.evaluation",
        startMs: evalEndMs - 1, // point-in-time span
        endMs: evalEndMs,
        attributes,
        statusCode,
      })
    );
  }

  // -------------------------------------------------------------------------
  // Fleet summary span
  // -------------------------------------------------------------------------
  function getSummaryNumber(field, fallback) {
    return summary && typeof summary[field] === "number" ? summary[field] : fallback;
  }

  const summaryAttributes = [
    buildAttr("gh-aw.exporter.name", "outcome-collector"),
    buildAttr("gh-aw.outcome.runs_checked", getSummaryNumber("runs_checked", 0)),
    buildAttr("gh-aw.outcome.total", getSummaryNumber("total_outcomes", evaluations.length)),
    buildAttr("gh-aw.outcome.accepted", getSummaryNumber("accepted", 0)),
    buildAttr("gh-aw.outcome.rejected", getSummaryNumber("rejected", 0)),
    buildAttr("gh-aw.outcome.ignored", getSummaryNumber("ignored", 0)),
    buildAttr("gh-aw.outcome.pending", getSummaryNumber("pending", 0)),
    buildAttr("gh-aw.outcome.unknown", getSummaryNumber("unknown", 0)),
    buildAttr("gh-aw.outcome.errors", getSummaryNumber("errors", 0)),
    buildAttr("gh-aw.outcome.lifecycle", getSummaryNumber("lifecycle", 0)),
    buildAttr("gh-aw.outcome.noop", getSummaryNumber("noop", 0)),
    buildAttr("gh-aw.outcome.accepted_strong", getSummaryNumber("accepted_strong", 0)),
    buildAttr("gh-aw.outcome.accepted_medium", getSummaryNumber("accepted_medium", 0)),
    buildAttr("gh-aw.outcome.accepted_weak", getSummaryNumber("accepted_weak", 0)),
    buildAttr("gh-aw.outcome.fallback_exists_only_count", getSummaryNumber("fallback_exists_only_count", 0)),
    buildAttr("gh-aw.outcome.acceptance_rate", getSummaryNumber("acceptance_rate", 0)),
    buildAttr("gh-aw.outcome.waste_rate", getSummaryNumber("waste_rate", 0)),
    buildAttr("gh-aw.outcome.noop_rate", getSummaryNumber("noop_rate", 0)),
    buildAttr("gh-aw.outcome.zero_touch_count", getSummaryNumber("zero_touch", 0)),
    buildAttr("gh-aw.outcome.zero_touch_rate", getSummaryNumber("zero_touch_rate", 0)),
    buildAttr("gh-aw.outcome.item_count", evaluations.length),
  ];

  if (summary && summary.date) {
    summaryAttributes.push(buildAttr("gh-aw.outcome.date", summary.date));
  }

  // Median time-to-resolution: prefer summary value, fall back to local computation
  const summaryMedian = summary && typeof summary.median_resolution_sec === "number" ? summary.median_resolution_sec : null;
  if (summaryMedian !== null) {
    summaryAttributes.push(buildAttr("gh-aw.outcome.median_resolution_sec", summaryMedian));
  } else {
    const resolutionTimes = evaluations
      .filter(e => typeof e.resolution_sec === "number" && e.resolution_sec > 0)
      .map(e => e.resolution_sec)
      .sort((a, b) => a - b);
    if (resolutionTimes.length > 0) {
      const mid = Math.floor(resolutionTimes.length / 2);
      const median = resolutionTimes.length % 2 !== 0 ? resolutionTimes[mid] : Math.round((resolutionTimes[mid - 1] + resolutionTimes[mid]) / 2);
      summaryAttributes.push(buildAttr("gh-aw.outcome.median_resolution_sec", median));
    }
  }

  // Trigger type distribution
  const events = [...new Set(evaluations.map(e => e.event).filter(Boolean))].sort();
  if (events.length > 0) {
    summaryAttributes.push(buildAttr("gh-aw.outcome.events", events.join(",")));
  }

  // Distinct workflows evaluated
  const workflows = [...new Set(evaluations.map(e => e.workflow).filter(Boolean))].sort();
  if (workflows.length > 0) {
    summaryAttributes.push(buildAttr("gh-aw.outcome.workflows", workflows.join(",")));
  }

  // Distinct outcome types seen
  const types = [...new Set(evaluations.map(e => e.type).filter(Boolean))].sort();
  if (types.length > 0) {
    summaryAttributes.push(buildAttr("gh-aw.outcome.types", types.join(",")));
  }

  // Append user-defined custom attributes from observability.otlp.attributes.
  summaryAttributes.push(...buildCustomOTLPAttributes());

  const summarySpan = buildOTLPSpan({
    traceId,
    spanId: summarySpanId,
    parentSpanId: parentSpanId || undefined,
    spanName: "gh-aw.outcome.summary",
    startMs: evalEndMs - 100, // span covers the evaluation window
    endMs: evalEndMs,
    attributes: summaryAttributes,
    statusCode: 1, // OK
  });

  // -------------------------------------------------------------------------
  // Batch and send
  // -------------------------------------------------------------------------
  const allSpans = [summarySpan, ...itemSpans];

  const payload = buildOTLPBatchPayload({
    serviceName,
    scopeVersion,
    resourceAttributes,
    spans: allSpans,
  });

  // Always write to local JSONL mirror
  appendToOTLPJSONL(payload);

  core.info(`[outcome-otel] Emitting ${evaluations.length} outcome span(s) + 1 summary span`);

  if (hasEndpoints) {
    await sendOTLPToAllEndpoints(endpoints, payload, { skipJSONL: true });
    core.info(`[outcome-otel] Exported to ${endpoints.length} endpoint(s)`);
  }
}

if (require.main === module) {
  main().catch(err => {
    // Non-fatal: OTEL export failures must never break the workflow
    console.warn(`[outcome-otel] Export failed (non-fatal): ${getErrorMessage(err)}`);
  });
}

module.exports = {
  main,
};
