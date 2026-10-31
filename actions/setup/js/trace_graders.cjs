// @ts-check

const { createSessionLookups, stableStringify, isRecord } = require("./grader_session.cjs");
const { TRAJECTORY_GRADERS } = require("./trajectory_graders.cjs");
const { collectFailureObservations, FAILURE_CATEGORY_IDS } = require("./failure_graders.cjs");
const BUILTIN_META = require("./builtin_graders.json");

/** @typedef {import("./types/unified_session").UnifiedSession} UnifiedSession */
/** @typedef {ReturnType<typeof createSessionLookups>} SessionLookups */
/** @typedef {(session: UnifiedSession, config?: Record<string, any>, lookups?: SessionLookups) => any} Grader */

const GRADER_VERSION = 2;
const IMPLEMENTATION_ID = "gh-aw/graders";
const DEFAULT_GRADER_IDS = [
  "tool-success-rate",
  "tool-failure-count",
  "retries",
  "loops",
  "trajectory-efficiency",
  "execution-step-count",
  "execution-duration",
  "working-set-rebuild-factor",
  "context-growth",
  "artifact-production",
  ...FAILURE_CATEGORY_IDS,
];

/** @type {WeakMap<SessionLookups, ReturnType<typeof collectFailureObservations>>} */
const failureCache = new WeakMap();
/** @param {UnifiedSession} session @param {SessionLookups} lookups */
function failureResults(session, lookups) {
  let results = failureCache.get(lookups);
  if (!results) {
    results = collectFailureObservations(session, lookups);
    failureCache.set(lookups, results);
  }
  return results;
}

/** @param {(lookups: SessionLookups) => any} grade @returns {Grader} */
function fromStats(grade) {
  return (session, _config, lookups = createSessionLookups(session)) => grade(lookups);
}

/** Every implementation consumes the same in-memory log; none perform I/O. @type {Record<string, Grader>} */
const BUILTIN_GRADERS = {
  "tool-success-rate": fromStats(({ stats }) => (stats.toolCallCount ? (stats.toolCallCount - stats.failedToolCalls) / stats.toolCallCount : 1)),
  "tool-failure-count": fromStats(({ stats }) => stats.failedToolCalls),
  retries: fromStats(({ stats }) => stats.retryCount),
  loops: fromStats(({ toolCalls }) => {
    let loops = 0;
    let previous;
    for (const call of toolCalls) {
      const key = stableStringify([call.sessionScope, call.name, call.arguments]);
      if (key === previous) loops++;
      previous = key;
    }
    return loops;
  }),
  "trajectory-efficiency": fromStats(({ toolCalls }) => (toolCalls.length ? new Set(toolCalls.map(call => call.name)).size / toolCalls.length : 1)),
  "execution-step-count": fromStats(({ stats }) => stats.totalRequests || null),
  "execution-duration": fromStats(({ stats }) => stats.totalDurationMs),
  "working-set-rebuild-factor": fromStats(({ stats }) => (stats.workingSet.measurement_state === "measured" ? (stats.workingSet.rebuild_factor ?? null) : null)),
  "context-growth": fromStats(({ trace }) => {
    if (!trace.tokenUsageEntries.length) return null;
    if (trace.totalInputTokens === null || trace.totalOutputTokens === null) return null;
    const first = trace.tokenUsageEntries[0];
    const firstTokens = (first.input_tokens ?? 0) + (first.output_tokens ?? 0);
    return firstTokens ? (trace.totalInputTokens + trace.totalOutputTokens) / firstTokens : null;
  }),
  "artifact-production": fromStats(({ stats }) => stats.artifactCount),
  ...Object.fromEntries(FAILURE_CATEGORY_IDS.map(id => [id, (session, _config, lookups = createSessionLookups(session)) => failureResults(session, lookups)[id]])),
  ...Object.fromEntries(Object.entries(TRAJECTORY_GRADERS).map(([id, grade]) => [id, (session, config, lookups = createSessionLookups(session)) => grade(lookups.trace, config)])),
};

/** @param {number} value @param {string} direction @param {number | undefined} threshold @returns {boolean | null} */
function evaluateThreshold(value, direction, threshold) {
  if (threshold === undefined || threshold === null) return null;
  if (direction === "higher_is_better") return value >= threshold;
  if (direction === "lower_is_better") return value <= threshold;
  return null;
}

/**
 * @typedef {object} GraderResult
 * @property {string} id
 * @property {string} name
 * @property {string} [description]
 * @property {number|null} value
 * @property {string} unit
 * @property {boolean|null} passed
 * @property {string} status
 * @property {string} source
 * @property {string} [error]
 * @property {string} [message]
 * @property {string} [details]
 * @property {string} [severity]
 * @property {Record<string, any>} [diagnostics]
 * @property {{id: string, version: number}} implementation
 */

/** @param {string} id @param {any} raw @param {any} meta @returns {GraderResult} */
function normalizeResult(id, raw, meta) {
  const object = isRecord(raw) ? raw : {};
  const value = isRecord(raw) ? raw.value : raw;
  const passed = typeof object.passed === "boolean" ? object.passed : typeof value === "number" ? evaluateThreshold(value, meta.direction, meta.threshold) : null;
  const finite = typeof value === "number" && Number.isFinite(value);
  const unavailable = value === null || value === undefined;
  return {
    id,
    name: meta.name || id,
    ...(meta.description ? { description: meta.description } : {}),
    value: finite ? value : null,
    unit: meta.unit || "",
    passed: finite ? passed : null,
    status: unavailable ? "unavailable" : !finite ? "error" : passed === false ? "fail" : "pass",
    source: "builtin",
    implementation: { id: IMPLEMENTATION_ID, version: GRADER_VERSION },
    ...(!finite && !unavailable ? { error: `grader ${id} returned a non-finite numeric value` } : {}),
    ...(typeof object.message === "string" ? { message: object.message } : {}),
    ...(typeof object.details === "string" ? { details: object.details } : {}),
    ...(typeof object.severity === "string" ? { severity: object.severity } : {}),
    ...(isRecord(object.diagnostics) ? { diagnostics: structuredClone(object.diagnostics) } : {}),
  };
}

/** @param {string} id @param {UnifiedSession} session @param {any} meta @param {SessionLookups} [lookups] @returns {GraderResult} */
function runBuiltinGrader(id, session, meta, lookups = createSessionLookups(session)) {
  if (!Object.hasOwn(BUILTIN_GRADERS, id)) throw new Error(`unsupported grader ${id}: only built-in graders are allowed`);
  try {
    return normalizeResult(id, BUILTIN_GRADERS[id](session, meta.config ?? {}, lookups), meta);
  } catch (error) {
    return { ...normalizeResult(id, null, meta), status: "error", error: `grader ${id} runtime error: ${error instanceof Error ? error.message : String(error)}` };
  }
}

/** @param {any[]} graders @returns {any[]} */
function withDefaultGraders(graders) {
  const declared = new Set();
  const result = graders.map(grader => {
    if (!isRecord(grader) || !Object.hasOwn(BUILTIN_GRADERS, grader.id) || (grader.source !== undefined && grader.source !== "builtin") || Object.hasOwn(grader, "script") || Object.hasOwn(grader, "run")) {
      throw new Error("custom graders are not supported; only built-in graders are allowed");
    }
    if (declared.has(grader.id)) throw new Error(`duplicate grader ${grader.id}`);
    declared.add(grader.id);
    return { ...BUILTIN_META[grader.id], enabled: true, ...grader, source: "builtin" };
  });
  for (const id of DEFAULT_GRADER_IDS) {
    if (!declared.has(id)) result.push({ id, ...BUILTIN_META[id], enabled: true, source: "builtin" });
  }
  return result;
}

/**
 * Pure grading entry point. The caller owns log collection and result persistence.
 * @param {UnifiedSession} session
 * @param {any[]} [graders]
 */
function computeGraders(session, graders = []) {
  const manifest = { version: GRADER_VERSION, graders: withDefaultGraders(graders) };
  const lookups = createSessionLookups(session);
  const results = manifest.graders.filter(grader => grader.enabled).map(grader => runBuiltinGrader(grader.id, session, grader, lookups));
  return { manifest, output: buildGraderOutput(results, lookups) };
}

/** @param {GraderResult[]} results @param {SessionLookups} lookups */
function buildGraderOutput(results, lookups) {
  const info = lookups.eventsOfType("workflow.info")[0]?.data;
  const evals = lookups.eventsOfType("eval.result").map(event => event.data);
  const byQuestion = Object.fromEntries(evals.filter(record => typeof record.id === "string").map(record => [record.id, record.answer]));
  return {
    version: GRADER_VERSION,
    run: {
      id: String(info?.runId ?? ""),
      ...(info?.runAttempt !== undefined ? { attempt: info.runAttempt } : {}),
      graderCount: results.length,
      passed: results.filter(result => result.status === "pass").length,
      failed: results.filter(result => result.status === "fail").length,
      errors: results.filter(result => result.status === "error").length,
    },
    context: {
      experiments: lookups.eventsOfType("experiment.assignment")[0]?.data.assignments,
      evals: evals.length
        ? {
            total: evals.length,
            yes: evals.filter(record => record.answer === "YES").length,
            no: evals.filter(record => record.answer === "NO").length,
            unknown: evals.filter(record => record.answer !== "YES" && record.answer !== "NO").length,
            byQuestion,
          }
        : undefined,
    },
    results,
  };
}

/** @param {any} value @returns {string} */
function sanitizeSummaryText(value) {
  return String(value ?? "")
    .replace(/\r?\n/g, " ")
    .replace(/\\/g, "\\\\")
    .replace(/\|/g, "\\|")
    .replace(/[<>&]/g, ch => (ch === "<" ? "&lt;" : ch === ">" ? "&gt;" : "&amp;"))
    .trim();
}

/** @param {GraderResult[]} results @returns {string} */
function buildGradersSummaryBody(results) {
  const computed = results.filter(hasComputedValue);
  if (!computed.length) return "\n\nNo grader values available.\n\n";
  return [
    "",
    "",
    "| Status | Grader | Source | Value | Unit |",
    "| --- | --- | --- | --- | --- |",
    ...computed.map(result => `| ${result.status === "fail" ? "Fail" : "Pass"} | ${sanitizeSummaryText(result.name)} | builtin | ${Number(result.value.toFixed(4))} | ${sanitizeSummaryText(result.unit)} |`),
    "",
    "",
  ].join("\n");
}

/** @param {GraderResult} result @returns {result is GraderResult & {value: number}} */
function hasComputedValue(result) {
  return typeof result.value === "number";
}

module.exports = {
  computeGraders,
  buildGraderOutput,
  createSessionLookups,
  runBuiltinGrader,
  withDefaultGraders,
  normalizeResult,
  evaluateThreshold,
  buildGradersSummaryBody,
  BUILTIN_GRADERS,
  BUILTIN_META,
  DEFAULT_GRADER_IDS,
  GRADER_VERSION,
  IMPLEMENTATION_ID,
};
