// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const cp = require("child_process");
const crypto = require("crypto");
const { getErrorMessage } = require("./error_helpers.cjs");
const { readExperimentAssignments } = require("./experiment_helpers.cjs");
const { calculateWorkingSetFromEntries } = require("./working_set_metrics.cjs");
const { executeOperationalValueEvaluator } = require("./operational_value_grader.cjs");

// --- Constants ---
const TMP_GH_AW = "/tmp/gh-aw";
const GRADERS_DIR = path.join(TMP_GH_AW, "agent", "graders");
const MANIFEST_PATH = path.join(GRADERS_DIR, "grader_manifest.json");
const PAYLOAD_PATH = path.join(GRADERS_DIR, "grader_payload.json");
const RESULTS_PATH = path.join(GRADERS_DIR, "grader_results.json");
const OPERATIONAL_VALUE_EVALUATOR_PATH = path.join(GRADERS_DIR, "operational_value_evaluator.sh");

// Trace source file paths
const TOKEN_USAGE_PATHS = [
  path.join(TMP_GH_AW, "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"),
  path.join(TMP_GH_AW, "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl"),
  path.join(TMP_GH_AW, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"),
];
const AGENT_USAGE_PATH = path.join(TMP_GH_AW, "agent_usage.json");
const MCP_GATEWAY_LOG_PATHS = [path.join(TMP_GH_AW, "mcp-logs/gateway.jsonl"), path.join(TMP_GH_AW, "mcp-logs/mcp-gateway.jsonl"), path.join(TMP_GH_AW, "mcp-logs/rpc-messages.jsonl")];
const AGENT_OUTPUT_PATH = path.join(TMP_GH_AW, "agent_output.json");
const AGENT_LOG_PATH = path.join(TMP_GH_AW, "agent.log");
const AGENT_LOG_JSONL_PATH = path.join(TMP_GH_AW, "agent_log.jsonl");
const EVALS_RESULTS_PATH = path.join(TMP_GH_AW, "evals.jsonl");

// Safety limits
const MAX_FILE_SIZE = 50 * 1024 * 1024; // 50 MB
const MAX_LINE_LENGTH = 1024 * 1024; // 1 MB per line
const SCRIPT_TIMEOUT_MS = 5000; // 5 seconds per custom grader
const SCRIPT_WORKER_OVERHEAD_MS = 1000; // Allow worker startup/serialization overhead.
const SCRIPT_WORKER_PATH = path.join(__dirname, "trace_graders_worker.cjs");

const GRADER_VERSION = 1;
const IMPLEMENTATION_ID = "gh-aw/graders";

// --- Trace preprocessing ---

/**
 * Safely read a file if it exists and is within size limits.
 * @param {string} filePath
 * @returns {string|null}
 */
function safeReadFile(filePath) {
  try {
    if (!fs.existsSync(filePath)) return null;
    const stat = fs.statSync(filePath);
    if (stat.size > MAX_FILE_SIZE) {
      core.warning(`Graders: skipping oversized file ${filePath} (${stat.size} bytes)`);
      return null;
    }
    return fs.readFileSync(filePath, "utf-8");
  } catch {
    // Intentionally ignore unreadable/missing files in fallback path probes.
    return null;
  }
}

/**
 * Safely parse JSONL, skipping malformed or oversized lines.
 * @param {string} content
 * @returns {any[]}
 */
function safeParseJsonl(content) {
  const results = [];
  for (const line of content.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    if (trimmed.length > MAX_LINE_LENGTH) continue;
    try {
      results.push(JSON.parse(trimmed));
    } catch {
      continue;
    }
  }
  return results;
}

/**
 * Safely parse JSON, returning null on failure.
 * @param {string} content
 * @returns {object|null}
 */
function safeParseJson(content) {
  if (content.length > MAX_FILE_SIZE) return null;
  try {
    return JSON.parse(content);
  } catch {
    return null;
  }
}

/**
 * @param {any} v
 * @returns {v is Record<string, any>}
 */
function isRecord(v) {
  return v !== null && typeof v === "object" && !Array.isArray(v);
}

/**
 * Read the first available file from a list of candidate paths.
 * @param {string[]} paths
 * @returns {string|null}
 */
function readFirstAvailable(paths) {
  for (const p of paths) {
    const content = safeReadFile(p);
    if (content !== null) return content;
  }
  return null;
}

/**
 * Deep-freeze an object recursively. Returns the same object.
 * @template T
 * @param {T} obj
 * @returns {T}
 */
function deepFreeze(obj) {
  if (obj === null || typeof obj !== "object") return obj;
  Object.freeze(obj);
  for (const key of Object.getOwnPropertyNames(obj)) {
    const v = /** @type {any} */ obj[key];
    if (v !== null && typeof v === "object" && !Object.isFrozen(v)) {
      deepFreeze(v);
    }
  }
  return obj;
}

/**
 * Deep clone via JSON round-trip (safe for plain data objects).
 * @param {any} obj
 * @returns {any}
 */
function deepClone(obj) {
  if (obj === null || obj === undefined) return obj;
  try {
    return structuredClone(obj);
  } catch {
    if (Array.isArray(obj)) {
      return obj.map(item => deepClone(item));
    }
    if (obj !== null && typeof obj === "object") {
      const out = {};
      for (const [k, v] of Object.entries(obj)) {
        out[k] = deepClone(v);
      }
      return out;
    }
    return obj;
  }
}

/**
 * Build a compact summary of eval outputs for cross-linking with grader results.
 * @returns {{total:number, yes:number, no:number, unknown:number, byQuestion:Record<string, string>}|null}
 */
function readEvalSummary() {
  const content = safeReadFile(EVALS_RESULTS_PATH);
  if (!content) {
    return null;
  }
  const records = safeParseJsonl(content);
  if (records.length === 0) {
    return null;
  }
  const summary = {
    total: 0,
    yes: 0,
    no: 0,
    unknown: 0,
    byQuestion: {},
  };
  for (const record of records) {
    if (!record || typeof record !== "object") {
      continue;
    }
    const id = typeof record.id === "string" ? record.id : "";
    const answerRaw = typeof record.answer === "string" ? record.answer.toUpperCase() : "UNKNOWN";
    const answer = answerRaw === "YES" || answerRaw === "NO" ? answerRaw : "UNKNOWN";
    summary.total += 1;
    if (answer === "YES") summary.yes += 1;
    else if (answer === "NO") summary.no += 1;
    else summary.unknown += 1;
    if (id) {
      summary.byQuestion[id] = answer;
    }
  }
  return summary.total > 0 ? summary : null;
}

/**
 * @typedef {object} PreprocessedTrace
 * @property {any[]} tokenUsageEntries - Parsed token-usage JSONL records
 * @property {object|null} agentUsage - Parsed agent_usage.json
 * @property {any[]} mcpGatewayEntries - Parsed MCP gateway log records
 * @property {object|null} agentOutput - Parsed agent_output.json
 * @property {any[]} toolCalls - Extracted tool call records from MCP gateway
 * @property {any[]} gatewayRequests - Request/response pairs from gateway
 * @property {any[]} retryEvents - Detected retry events
 * @property {any[]} errorEvents - Detected error events
 * @property {any[]} steps - Extracted execution steps (LLM requests)
 * @property {number} totalInputTokens - Sum of input tokens
 * @property {number} totalOutputTokens - Sum of output tokens
 * @property {number} totalDurationMs - Sum of duration_ms from token usage
 * @property {number} totalRequests - Count of token usage entries (LLM requests)
 * @property {any[]} files - Files mentioned in agent output
 * @property {any[]} artifacts - Artifacts/outputs from agent
 */

/**
 * Single preprocessing pass over all trace files.
 * @returns {PreprocessedTrace}
 */
function preprocessTrace() {
  // Token usage
  const tokenContent = readFirstAvailable(TOKEN_USAGE_PATHS);
  const tokenUsageEntries = tokenContent ? safeParseJsonl(tokenContent) : [];

  // Agent usage
  const agentUsageContent = safeReadFile(AGENT_USAGE_PATH);
  const agentUsage = agentUsageContent ? safeParseJson(agentUsageContent) : null;

  // MCP gateway logs
  const gatewayContent = readFirstAvailable(MCP_GATEWAY_LOG_PATHS);
  const mcpGatewayEntries = gatewayContent ? safeParseJsonl(gatewayContent) : [];

  // Agent output
  const agentOutputContent = safeReadFile(AGENT_OUTPUT_PATH);
  const agentOutput = agentOutputContent ? safeParseJson(agentOutputContent) : null;

  // Extract tool calls from MCP gateway entries.
  // Also support rpc fallback records that expose tool_name/payload.tool_name.
  const toolCalls = mcpGatewayEntries
    .filter(e => {
      const payload = isRecord(e.payload) ? e.payload : null;
      return e.type === "tool_call" || e.method === "tools/call" || e.event === "tool_call" || typeof e.tool_name === "string" || (payload !== null && typeof payload.tool_name === "string");
    })
    .map(e => {
      const payload = isRecord(e.payload) ? e.payload : null;
      const toolName = typeof e.tool_name === "string" ? e.tool_name : payload !== null && typeof payload.tool_name === "string" ? payload.tool_name : undefined;
      const args = payload !== null ? (payload.arguments ?? payload.params) : undefined;
      return {
        ...e,
        name: e.name || e.tool || toolName,
        tool: e.tool || toolName,
        arguments: e.arguments ?? args,
      };
    });

  // Gateway request/response pairs
  const gatewayRequests = mcpGatewayEntries.filter(e => e.type === "request" || e.type === "response" || e.method);

  // Retry events
  const retryEvents = mcpGatewayEntries.filter(e => e.retry === true || e.event === "retry" || (typeof e.message === "string" && /retry|retrying/i.test(String(e.message))));

  // Error events
  const errorEvents = mcpGatewayEntries.filter(e => e.level === "error" || e.type === "error" || e.event === "error");

  // Aggregate token counts
  let totalInputTokens = 0;
  let totalOutputTokens = 0;
  let totalDurationMs = 0;
  for (const entry of tokenUsageEntries) {
    totalInputTokens += Number(entry.input_tokens) || 0;
    totalOutputTokens += Number(entry.output_tokens) || 0;
    totalDurationMs += Number(entry.duration_ms) || 0;
  }

  // Steps: each token usage entry represents an LLM request/step
  const steps = tokenUsageEntries.map((entry, i) => ({
    index: i,
    inputTokens: Number(entry.input_tokens) || 0,
    outputTokens: Number(entry.output_tokens) || 0,
    durationMs: Number(entry.duration_ms) || 0,
    model: entry.model || null,
  }));

  // Extract files and artifacts from agent output
  const ao = agentOutput !== null && typeof agentOutput === "object" ? agentOutput : null;
  const files = ao && "files" in ao && Array.isArray(ao.files) ? ao.files : [];
  const artifacts = ao && "outputs" in ao && Array.isArray(ao.outputs) ? ao.outputs : ao && "items" in ao && Array.isArray(ao.items) ? ao.items : [];

  return {
    tokenUsageEntries,
    agentUsage,
    mcpGatewayEntries,
    agentOutput,
    toolCalls,
    gatewayRequests,
    retryEvents,
    errorEvents,
    steps,
    totalInputTokens,
    totalOutputTokens,
    totalDurationMs,
    totalRequests: tokenUsageEntries.length,
    files,
    artifacts,
  };
}

// --- Built-in graders ---

/** @type {Record<string, {unit: string, direction: string, threshold?: number, min?: number, max?: number}>} */
const BUILTIN_META = {
  "tool-success-rate": { unit: "ratio", direction: "higher_is_better", threshold: 0.8, min: 0, max: 1 },
  "tool-failure-count": { unit: "count", direction: "lower_is_better", threshold: 5 },
  retries: { unit: "count", direction: "lower_is_better", threshold: 10 },
  loops: { unit: "count", direction: "lower_is_better", threshold: 3 },
  "trajectory-efficiency": { unit: "ratio", direction: "higher_is_better", min: 0, max: 1 },
  "execution-step-count": { unit: "count", direction: "lower_is_better" },
  "execution-duration": { unit: "ms", direction: "lower_is_better" },
  "working-set-rebuild-factor": { unit: "factor", direction: "lower_is_better", min: 1 },
  "context-growth": { unit: "factor", direction: "lower_is_better" },
  "artifact-production": { unit: "count", direction: "higher_is_better" },
};

/**
 * @param {any} toolCall
 * @returns {boolean}
 */
function isToolFailure(toolCall) {
  return toolCall.success === false || toolCall.status === "error" || toolCall.status === "failure" || toolCall.error !== undefined;
}

/**
 * @param {PreprocessedTrace} trace
 * @returns {number} Success rate of tool calls (0-1), or 1 if no tool calls
 */
function gradeToolSuccessRate(trace) {
  if (trace.toolCalls.length === 0) return 1;
  const successes = trace.toolCalls.length - trace.toolCalls.filter(isToolFailure).length;
  return successes / trace.toolCalls.length;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeToolFailureCount(trace) {
  return trace.toolCalls.filter(isToolFailure).length;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeRetries(trace) {
  return trace.retryEvents.length;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeLoops(trace) {
  let loops = 0;
  let prevKey = "";
  for (const t of trace.toolCalls) {
    const key = `${String(t.name || t.tool)}:${JSON.stringify(t.arguments || t.params || "")}`;
    if (key === prevKey) loops++;
    prevKey = key;
  }
  return loops;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeTrajectoryEfficiency(trace) {
  if (trace.toolCalls.length === 0) return 1;
  const uniqueTools = new Set(trace.toolCalls.map(t => String(t.name || t.tool || "")));
  return Math.min(1, uniqueTools.size / trace.toolCalls.length);
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeExecutionStepCount(trace) {
  return trace.totalRequests;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeExecutionDuration(trace) {
  return trace.totalDurationMs;
}

/** @param {PreprocessedTrace} trace @returns {number|null} */
function gradeWorkingSetRebuildFactor(trace) {
  const { workingSet } = calculateWorkingSetFromEntries(trace.tokenUsageEntries);
  return workingSet.rebuild_factor ?? null;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeContextGrowth(trace) {
  if (trace.tokenUsageEntries.length < 2) return 1;
  const first = trace.tokenUsageEntries[0];
  const firstTokens = (Number(first.input_tokens) || 0) + (Number(first.output_tokens) || 0);
  if (firstTokens === 0) return 1;
  const totalTokens = trace.totalInputTokens + trace.totalOutputTokens;
  return totalTokens / firstTokens;
}

/** @param {PreprocessedTrace} trace @returns {number} */
function gradeArtifactProduction(trace) {
  return trace.artifacts.length;
}

/** @type {Record<string, (trace: PreprocessedTrace) => number|null>} */
const BUILTIN_GRADERS = {
  "tool-success-rate": gradeToolSuccessRate,
  "tool-failure-count": gradeToolFailureCount,
  retries: gradeRetries,
  loops: gradeLoops,
  "trajectory-efficiency": gradeTrajectoryEfficiency,
  "execution-step-count": gradeExecutionStepCount,
  "execution-duration": gradeExecutionDuration,
  "working-set-rebuild-factor": gradeWorkingSetRebuildFactor,
  "context-growth": gradeContextGrowth,
  "artifact-production": gradeArtifactProduction,
};

// --- Execution ---

/**
 * Evaluate a grader's pass/fail against its threshold.
 * @param {number} value
 * @param {string} direction
 * @param {number|undefined} threshold
 * @returns {boolean|null} null if no threshold set
 */
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
 * @property {number|null} value
 * @property {string} unit
 * @property {boolean|null} passed
 * @property {string} status - "pass" | "fail" | "error" | "unavailable"
 * @property {string} [severity]
 * @property {string} [details]
 * @property {string} [message]
 * @property {string} [error]
 * @property {string} source - "builtin" | "inline" | "value"
 * @property {object} [observation]
 * @property {object} [diagnostics]
 * @property {number|null} [baselineValue]
 * @property {number|null} [deltaFromBaseline]
 * @property {{id: string, version: number, digest?: string}} implementation
 */

/**
 * Escape values for step summary rendering.
 * @param {any} value
 * @returns {string}
 */
function sanitizeSummaryText(value) {
  return String(value ?? "")
    .replace(/\r?\n/g, " ")
    .replace(/\\/g, "\\\\")
    .replace(/\|/g, "\\|")
    .replace(/[<>&]/g, ch => (ch === "<" ? "&lt;" : ch === ">" ? "&gt;" : "&amp;"))
    .trim();
}

/**
 * @param {GraderResult} result
 * @returns {result is GraderResult & {value: number}}
 */
function hasComputedValue(result) {
  return typeof result.value === "number";
}

/**
 * Build the Graders section body for the GitHub Actions step summary.
 * @param {GraderResult[]} results
 * @returns {string}
 */
function buildGradersSummaryBody(results) {
  const computedResults = results.filter(hasComputedValue);
  if (computedResults.length === 0) {
    return "\n\nNo grader values available.\n\n";
  }

  const statusLabels = { pass: "Pass", fail: "Fail", error: "Error", unavailable: "Unavailable" };
  const rows = computedResults.map(result => {
    const value = String(Number(result.value.toFixed(4)));
    return `| ${statusLabels[result.status] || "Unknown"} | ${sanitizeSummaryText(result.name)} | ${sanitizeSummaryText(result.source)} | ${value} | ${sanitizeSummaryText(result.unit || "—")} |`;
  });

  return ["", "", "| Status | Grader | Source | Value | Unit |", "| --- | --- | --- | --- | --- |", ...rows, "", ""].join("\n");
}

/**
 * Normalize a grader result from either built-in number or custom object return.
 * @param {string} id
 * @param {any} rawResult - number or {value, unit?, passed?, severity?, details?, message?}
 * @param {{name: string, unit: string, direction: string, threshold?: number, source: string, digest?: string}} meta
 * @returns {GraderResult}
 */
function normalizeResult(id, rawResult, meta) {
  /** @type {GraderResult} */
  const base = {
    id,
    name: meta.name || id,
    value: null,
    unit: meta.unit || "",
    passed: null,
    status: "error",
    source: meta.source,
    implementation: { id: IMPLEMENTATION_ID, version: GRADER_VERSION, ...(meta.digest ? { digest: meta.digest } : {}) },
  };

  if (rawResult === null || rawResult === undefined) {
    base.status = "unavailable";
    base.message = "grader returned null/undefined";
    return base;
  }

  let value;
  if (typeof rawResult === "object" && rawResult !== null && !Array.isArray(rawResult)) {
    // Object result from custom script
    value = rawResult.value;
    if (typeof rawResult.severity === "string" && ["error", "warning", "info", "note"].includes(rawResult.severity)) base.severity = rawResult.severity;
    if (rawResult.details) base.details = String(rawResult.details);
    if (rawResult.message) base.message = String(rawResult.message);
    if (typeof rawResult.passed === "boolean") base.passed = rawResult.passed;
    if (isRecord(rawResult.observation)) base.observation = deepClone(rawResult.observation);
    if (isRecord(rawResult.diagnostics)) base.diagnostics = deepClone(rawResult.diagnostics);
    if (typeof rawResult.baselineValue === "number" || rawResult.baselineValue === null) base.baselineValue = rawResult.baselineValue;
    if (typeof rawResult.deltaFromBaseline === "number" || rawResult.deltaFromBaseline === null) base.deltaFromBaseline = rawResult.deltaFromBaseline;
  } else {
    value = rawResult;
  }

  if (value === null || value === undefined) {
    base.status = "unavailable";
    base.message ||= "grader returned no value";
    return base;
  }

  if (typeof value !== "number" || !isFinite(value)) {
    base.status = "error";
    base.error = `grader ${id} returned non-finite value: ${value}`;
    return base;
  }

  base.value = value;

  // Evaluate threshold if not already set by custom script
  if (base.passed === null) {
    base.passed = evaluateThreshold(value, meta.direction, meta.threshold);
  }

  // Determine status
  if (base.passed === true) base.status = "pass";
  else if (base.passed === false) base.status = "fail";
  else base.status = "pass"; // no threshold = informational pass

  return base;
}

/**
 * Run a single built-in grader safely.
 * @param {string} id
 * @param {PreprocessedTrace} trace
 * @param {{name: string, unit: string, direction: string, threshold?: number, source: string}} meta
 * @returns {GraderResult}
 */
function runBuiltinGrader(id, trace, meta) {
  const fn = BUILTIN_GRADERS[id];
  if (!fn) {
    return { ...normalizeResult(id, null, meta), status: "error", error: `grader ${id}: no implementation found` };
  }
  try {
    const value = fn(trace);
    return normalizeResult(id, value, meta);
  } catch (err) {
    const result = normalizeResult(id, null, meta);
    result.status = "error";
    result.error = `grader ${id} runtime error: ${getErrorMessage(err)}`;
    return result;
  }
}

/**
 * Run a custom inline script in an isolated worker subprocess.
 * Script receives {trace, run, workflow, config, helpers} and should return {value, ...} or a number.
 * @param {string} id
 * @param {string} script
 * @param {PreprocessedTrace} trace
 * @param {{name: string, unit: string, direction: string, threshold?: number, source: string, digest?: string, config?: object, graderCount?: number}} meta
 * @returns {GraderResult}
 */
function executeCustomGraderInSubprocess(id, script, trace, meta) {
  const payload = {
    id,
    script,
    trace,
    config: meta.config || {},
    graderCount: Number(meta.graderCount) || 0,
    timeoutMs: SCRIPT_TIMEOUT_MS,
  };
  const safeEnv = {};
  for (const key of ["PATH", "HOME", "TMPDIR", "TEMP", "TMP", "SystemRoot", "ComSpec"]) {
    if (process.env[key]) {
      safeEnv[key] = process.env[key];
    }
  }
  const timeoutMs = SCRIPT_TIMEOUT_MS + SCRIPT_WORKER_OVERHEAD_MS;
  const proc = cp.spawnSync(process.execPath, [SCRIPT_WORKER_PATH], {
    input: JSON.stringify(payload),
    encoding: "utf-8",
    timeout: timeoutMs,
    maxBuffer: 1024 * 1024,
    env: safeEnv,
  });

  const procError = proc.error;
  if (procError) {
    if (typeof procError.message === "string" && /ETIMEDOUT|timed out/i.test(procError.message)) {
      throw new Error(`script worker timed out after ${timeoutMs}ms`);
    }
    throw procError;
  }

  if (proc.status !== 0) {
    const stderr = (proc.stderr || "").trim();
    throw new Error(stderr || `script worker exited with status ${String(proc.status)}`);
  }

  let parsed;
  try {
    parsed = JSON.parse(proc.stdout || "{}");
  } catch (err) {
    throw new Error(`invalid script worker output: ${getErrorMessage(err)}`, { cause: err });
  }

  if (!parsed || parsed.ok !== true) {
    throw new Error(parsed && typeof parsed.error === "string" ? parsed.error : "script worker returned an error");
  }
  return parsed.value;
}

function runCustomGrader(id, script, trace, meta) {
  try {
    const rawResult = executeCustomGraderInSubprocess(id, script, trace, meta);
    return normalizeResult(id, rawResult, meta);
  } catch (err) {
    const result = normalizeResult(id, null, meta);
    result.status = "error";
    result.error = `grader ${id} runtime error: ${getErrorMessage(err)}`;
    return result;
  }
}

function runOperationalValueGrader(id, evaluatorContent, meta, options) {
  try {
    const rawResult = executeOperationalValueEvaluator(evaluatorContent, meta, options);
    return normalizeResult(id, rawResult, meta);
  } catch (err) {
    const result = normalizeResult(id, null, meta);
    result.status = "error";
    result.error = `grader ${id} runtime error: ${getErrorMessage(err)}`;
    return result;
  }
}

function archiveOperationalValueEvaluator(evaluatorContent, expectedDigest, outputPath = OPERATIONAL_VALUE_EVALUATOR_PATH) {
  const actualDigest = crypto.createHash("sha256").update(evaluatorContent, "utf8").digest("hex");
  if (!expectedDigest || actualDigest !== expectedDigest) {
    throw new Error(`operational-value evaluator digest mismatch: expected ${expectedDigest || "none"}, got ${actualDigest}`);
  }
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, evaluatorContent, { encoding: "utf8", mode: 0o600 });
}

/**
 * Legacy adapter for existing tests. Runs a grader by id.
 * @param {string} id
 * @param {boolean} builtin
 * @param {string|undefined} script
 * @param {PreprocessedTrace} trace
 * @param {object} [config]
 * @returns {{ value: number|null, error: string|null }}
 */
function runGrader(id, builtin, script, trace, config) {
  const meta = { name: id, unit: "", direction: "", source: builtin ? "builtin" : "inline", config };
  /** @type {GraderResult} */
  let result;
  if (builtin && BUILTIN_GRADERS[id]) {
    result = runBuiltinGrader(id, trace, meta);
  } else if (script) {
    result = runCustomGrader(id, script, trace, meta);
  } else {
    return { value: null, error: `grader ${id}: no implementation found` };
  }
  return { value: result.value, error: result.error || null };
}

/**
 * Main entry point. Called from the github-script step with base64 manifest and exec spec.
 * @param {string} manifestB64 - Base64-encoded JSON manifest
 * @param {string} [execSpecB64] - Base64-encoded JSON array of {id, script|run}
 */
async function main(manifestB64, execSpecB64) {
  /** @type {{version: number, graders: any[]}} */
  let manifest;
  try {
    const manifestJson = Buffer.from(manifestB64, "base64").toString("utf-8");
    manifest = JSON.parse(manifestJson);
  } catch (err) {
    core.setFailed(`Graders: failed to parse manifest: ${getErrorMessage(err)}`);
    return;
  }

  // Decode trusted executable payloads for custom graders.
  /** @type {Record<string, {script?: string, run?: string}>} */
  const executionMap = {};
  if (execSpecB64) {
    try {
      const specJson = Buffer.from(execSpecB64, "base64").toString("utf-8");
      const specs = JSON.parse(specJson);
      for (const s of specs) {
        if (s.id && (s.script || s.run)) executionMap[s.id] = { script: s.script, run: s.run };
      }
    } catch (err) {
      core.warning(`Graders: failed to parse exec spec: ${getErrorMessage(err)}`);
    }
  }

  // Write manifest file
  try {
    fs.mkdirSync(GRADERS_DIR, { recursive: true });
    fs.writeFileSync(MANIFEST_PATH, JSON.stringify(manifest, null, 2));
  } catch (err) {
    core.warning(`Graders: failed to write manifest: ${getErrorMessage(err)}`);
  }

  // Filter to enabled graders
  const graders = manifest.graders || [];
  const enabledGraders = graders.filter(g => g.enabled);
  if (enabledGraders.length === 0) {
    core.info("Graders: no enabled graders, skipping");
    return;
  }

  let operationalValueEvaluatorArchiveError;
  const operationalValueManifest = enabledGraders.find(grader => grader.source === "operational-value");
  if (operationalValueManifest) {
    try {
      const evaluatorContent = executionMap[operationalValueManifest.id]?.run;
      if (!evaluatorContent) throw new Error("operational-value evaluator is missing from the execution specification");
      archiveOperationalValueEvaluator(evaluatorContent, operationalValueManifest.digest);
    } catch (err) {
      operationalValueEvaluatorArchiveError = getErrorMessage(err);
      core.warning(`Graders: unable to archive operational-value evaluator: ${operationalValueEvaluatorArchiveError}`);
    }
  }

  // Single preprocessing pass
  core.info(`Graders: preprocessing trace files for ${enabledGraders.length} grader(s)...`);
  const trace = preprocessTrace();
  try {
    fs.writeFileSync(PAYLOAD_PATH, JSON.stringify(trace));
  } catch (err) {
    core.warning(`Graders: failed to write payload: ${getErrorMessage(err)}`);
  }
  const runCreatedAt = process.env.GH_AW_RUN_CREATED_AT;
  const operationalValueRunMetadata = runCreatedAt ? { createdAt: runCreatedAt } : undefined;

  // Run all graders
  /** @type {GraderResult[]} */
  const results = [];
  for (const grader of enabledGraders) {
    const meta = {
      name: grader.name || grader.id,
      unit: grader.unit || "",
      direction: grader.direction || "",
      threshold: grader.threshold,
      source: grader.source || "builtin",
      digest: grader.digest,
      config: grader.config,
      graderCount: enabledGraders.length,
    };
    /** @type {GraderResult} */
    let result;
    if (grader.source === "builtin" && BUILTIN_GRADERS[grader.id]) {
      result = runBuiltinGrader(grader.id, trace, meta);
    } else if (grader.source === "operational-value" && operationalValueEvaluatorArchiveError) {
      result = normalizeResult(grader.id, null, meta);
      result.status = "error";
      result.error = `grader ${grader.id} runtime error: ${operationalValueEvaluatorArchiveError}`;
    } else if (grader.source === "operational-value" && executionMap[grader.id]?.run) {
      result = runOperationalValueGrader(grader.id, executionMap[grader.id].run, meta, { runMetadata: operationalValueRunMetadata });
    } else if (executionMap[grader.id]?.script) {
      result = runCustomGrader(grader.id, executionMap[grader.id].script, trace, meta);
    } else {
      result = normalizeResult(grader.id, null, meta);
      result.status = "unavailable";
      result.error = `grader ${grader.id}: no implementation available`;
    }
    results.push(result);
    if (result.error) {
      core.warning(`Grader ${grader.id}: ${result.error}`);
    }
  }

  // Build normalized output — NO timestamp for deterministic byte-equivalence
  const passed = results.filter(r => r.status === "pass").length;
  const failed = results.filter(r => r.status === "fail").length;
  const errorCount = results.filter(r => r.status === "error").length;

  const output = {
    version: GRADER_VERSION,
    run: {
      id: String(process.env.GITHUB_RUN_ID || ""),
      attempt: Number(process.env.GITHUB_RUN_ATTEMPT) || 1,
      graderCount: results.length,
      passed,
      failed,
      errors: errorCount,
    },
    context: {
      experiments: readExperimentAssignments() || undefined,
      evals: readEvalSummary() || undefined,
    },
    results,
  };

  // Write results
  try {
    fs.writeFileSync(RESULTS_PATH, JSON.stringify(output, null, 2));
    core.info(`Graders: wrote results to ${RESULTS_PATH}`);
  } catch (err) {
    core.warning(`Graders: failed to write results: ${getErrorMessage(err)}`);
  }

  // Step summary
  core.summary.addDetails("Graders", buildGradersSummaryBody(results));
  const errResults = results.filter(r => r.error);
  if (errResults.length > 0) {
    const errLines = errResults.map(r => `- **${sanitizeSummaryText(r.id)}**: runtime error (see step logs)`).join("\n");
    core.summary.addDetails("Grader Errors", errLines);
  }
  await core.summary.write({ overwrite: false });

  core.info(`Graders: ${passed} passed, ${failed} failed, ${errorCount} errors`);
}

module.exports = {
  main,
  preprocessTrace,
  safeReadFile,
  safeParseJsonl,
  safeParseJson,
  readFirstAvailable,
  readEvalSummary,
  deepFreeze,
  deepClone,
  runGrader,
  runBuiltinGrader,
  runCustomGrader,
  runOperationalValueGrader,
  normalizeResult,
  buildGradersSummaryBody,
  evaluateThreshold,
  BUILTIN_GRADERS,
  BUILTIN_META,
  GRADER_VERSION,
  IMPLEMENTATION_ID,
  GRADERS_DIR,
  MANIFEST_PATH,
  PAYLOAD_PATH,
  RESULTS_PATH,
  OPERATIONAL_VALUE_EVALUATOR_PATH,
  archiveOperationalValueEvaluator,
  MAX_FILE_SIZE,
  MAX_LINE_LENGTH,
  SCRIPT_TIMEOUT_MS,
  gradeToolSuccessRate,
  gradeToolFailureCount,
  gradeRetries,
  gradeLoops,
  gradeTrajectoryEfficiency,
  gradeExecutionStepCount,
  gradeExecutionDuration,
  gradeWorkingSetRebuildFactor,
  gradeContextGrowth,
  gradeArtifactProduction,
};
