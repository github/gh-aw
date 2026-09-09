// @ts-check

/**
 * Detect agent engine errors in the agent stdio log and AWF firewall audit log.
 *
 * Scans the agent stdio log for known error patterns and the AWF firewall audit
 * JSONL log for structured error events, then sets GitHub Actions output variables
 * for each detected error class:
 *
 *   - inference_access_error: The COPILOT_GITHUB_TOKEN does not have valid
 *     access to inference (e.g., "Access denied by policy settings").
 *   - mcp_policy_error: MCP servers were blocked by enterprise/organization
 *     policy (e.g., "MCP servers were blocked by policy: 'github', 'safeoutputs'").
 *   - agentic_engine_timeout: A timeout signature was detected in engine logs.
 *     This includes process termination by signal (SIGTERM/SIGKILL/SIGINT),
 *     typically due to step timeout-minutes, and SDK idle-timeout messages
 *     ("Timeout after <n>ms waiting for session.idle"). It is also set when the
 *     engine execution step failed after running for its full `timeout-minutes`
 *     budget, which is how GitHub Actions step-level timeouts surface for engines
 *     that leave no timeout signature in the agent log.
 *   - model_not_supported_error: The configured model is invalid or unsupported
 *     for the selected engine/account (for example unknown model name, model not
 *     found, or model unavailable for the plan).
 *   - http_400_response_error: The engine surfaced a generic HTTP 400 Bad Request
 *     response (for example "Response status code does not indicate success: 400 (Bad Request)").
 *   - capi_quota_exceeded_error: The Copilot CAPI quota has been exhausted
 *     or rate-limited (e.g., "CAPIError: 429 429 quota exceeded",
 *     "CAPIError: Too Many Requests", or the Copilot CLI's own
 *     retry-exhaustion message "Failed to get response from the AI model;
 *     retried N times ... Last error: 429/5xx" which carries no "CAPIError:"
 *     prefix). All matched forms are treated as non-retryable because the
 *     Copilot CLI/SDK has already retried internally before surfacing the
 *     error.
 *   - invocation_cap_exceeded: The per-run pooled LLM invocation cap is
 *     fully exhausted (e.g., "CAPIError: 429 Maximum LLM invocations exceeded (N/N)"
 *     or `"type":"max_runs_exceeded"`). This is more specific than generic
 *     CAPI quota exhaustion and takes precedence in step outputs.
 *   - missing_model_pricing_error / missing_model_pricing_model_name: The AWF API
 *     proxy rejected a request because the model has no AI credits pricing entry.
 *     Detected from the agent stdio log (text pattern) and the AWF firewall audit
 *     JSONL log (`unknown_model_ai_credits` event type). Both sources are checked
 *     and their results merged.
 *   - shell_expansion_guard_rejected: The sandbox's shell command-injection guard
 *     rejected a shell command for containing (or appearing to contain) bash
 *     expansion patterns (command substitution, indirect expansion, parameter
 *     transformation, etc.), e.g. "...could enable arbitrary code execution.
 *     Please rewrite the command without these expansion patterns." This can
 *     misfire on benign multi-line `printf`/`safeoutputs` CLI invocations; agents
 *     should switch to the `jq -Rs` file-piping pattern instead of retrying the
 *     same command verbatim.
 * This replaces the individual bash scripts (detect_inference_access_error.sh,
 * detect_mcp_policy_error.sh) with a single JavaScript step.
 *
 * In addition to the output variables above, when the engine sets
 * GH_AW_ENGINE_INTERNAL_LOGS_DIR (via CodingAgentEngine.GetInternalLogsDir) and the execution
 * step failed, this script tails the most recently modified `*.log` file under that directory
 * into the step log (see renderInternalEngineLogOnFailure()). Some engines (e.g. Codex CLI)
 * write their own tracing/diagnostic output to files rather than to stdout/stderr, so a bare
 * non-zero exit code with no console output can still have a diagnosable error recorded there.
 *
 * Exit codes:
 *   0 — Always succeeds (uses continue-on-error in the workflow step)
 */

"use strict";

require("./shim.cjs");

const fs = require("fs");
const path = require("path");
const { MAX_RUNS_EXCEEDED_PATTERNS, isMaxRunsExceededError } = require("./harness_retry_guard.cjs");
const { parseUnknownModelAICreditsAndModelFromAuditLog, parseMaxCacheMissesExceededFromEventLog } = require("./ai_credits_context.cjs");
const { renderLogFromFile } = require("./render_detection_log.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const LOG_FILE = "/tmp/gh-aw/agent-stdio.log";

// File written by the engine execution step with the epoch milliseconds at which the
// engine CLI was started. Used to measure how long the engine ran before it was killed.
const AGENT_CLI_START_MS_FILE = "/tmp/gh-aw/agent_cli_start_ms.txt";

// Tolerance applied when comparing the engine run duration against the step timeout budget.
// The start timestamp is written a moment after the step begins, so a step killed by
// GitHub Actions ("The action '...' has timed out after N minutes.") reports slightly less
// than the full budget here.
const STEP_TIMEOUT_TOLERANCE_MS = 30_000;

// Pattern: Copilot CLI inference access denied
const INFERENCE_ACCESS_ERROR_PATTERN = /Access denied by policy settings|invalid access to inference/;

// Pattern: MCP servers blocked by enterprise/organization policy
const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;

// Pattern: Agentic engine timeout.
// Covers both timeout signatures observed in engine logs:
//   1) Process killed by signal after step timeout-minutes:
//      [copilot-harness] ... process closed exitCode=1 signal=SIGTERM ...
//   2) Copilot SDK idle-timeout while waiting for session.idle:
//      [sdk-driver] error: Timeout after 870000ms waiting for session.idle
// The second form can occur even when the driver collected output, and should
// still be classified as a timeout for conclusion/reporting purposes.
// NOTE: use isAgenticEngineTimeout() for detection logic that excludes post-result
// watchdog SIGTERMs (watchdogFired=true). This pattern is exported for direct tests only.
const AGENTIC_ENGINE_TIMEOUT_PATTERN = /(?:signal=SIG(?:TERM|KILL|INT)|Timeout after \d+ms waiting for session\.idle)/;

// Pattern: copilot-harness "process closed" line with SIGTERM and watchdogFired=true.
// This indicates the post-result idle watchdog fired a SIGTERM — the agent completed
// its work but the process did not exit cleanly in time. This is NOT a step timeout.
const WATCHDOG_SIGTERM_PATTERN = /process closed[^\n]*signal=SIG(?:TERM|KILL|INT)[^\n]*watchdogFired=true/;

// Pattern: copilot-harness "process closed" line with SIGTERM and watchdogFired NOT true
// (watchdogFired=false or watchdogFired field absent). This indicates a genuine external kill,
// typically from the step timeout-minutes limit.
const STEP_TIMEOUT_SIGTERM_PATTERN = /process closed[^\n]*signal=SIG(?:TERM|KILL|INT)(?![^\n]*watchdogFired=true)/;
const PROCESS_CLOSED_SIGTERM_PATTERN = /process closed[^\n]*signal=SIG(?:TERM|KILL|INT)/;

/**
 * Determines if the log content shows a genuine agentic engine timeout.
 *
 * Returns false when the only SIGTERM source is the post-result idle watchdog
 * (watchdogFired=true on the "process closed" line). The watchdog fires when the
 * process is idle after completing its work, which is NOT a step timeout.
 *
 * @param {string} logContent - Contents of the agent stdio log
 * @returns {boolean}
 */
function isAgenticEngineTimeout(logContent) {
  // Always detect SDK idle-timeout (distinct from the step timeout).
  if (/Timeout after \d+ms waiting for session\.idle/.test(logContent)) return true;

  // No signal-based termination at all.
  if (!AGENTIC_ENGINE_TIMEOUT_PATTERN.test(logContent)) return false;

  // If there is a "process closed" line with SIGTERM and watchdogFired=true, the post-result
  // watchdog fired. Check whether there is also a "process closed" SIGTERM line that did NOT
  // have watchdogFired=true (which would mean a genuine external kill happened too).
  if (WATCHDOG_SIGTERM_PATTERN.test(logContent)) {
    return STEP_TIMEOUT_SIGTERM_PATTERN.test(logContent);
  }

  // Only classify as timeout when the signal is on a "process closed" line.
  return PROCESS_CLOSED_SIGTERM_PATTERN.test(logContent);
}

// Pattern: Configured model is invalid or unavailable.
// Covers common engine/provider variants:
//   - "The requested model is not supported"
//   - "invalid model name '...'"
//   - "unknown model <id>"
//   - "model ... not found"
//   - "model ... does not exist"
//   - "Model not found" (standalone, e.g. AIC api-proxy 404: "404 Not Found: Model not found")
//   - "No model available. Check policy enablement under GitHub Settings > Copilot" (Copilot SDK
//     driver, typically raised when a subagent/`task` dispatch requests a model that is disabled
//     by the org/repo Copilot policy). Anchored to the "policy enablement" phrase so that the
//     generic "No model available" wording alone does not produce false positives.
const MODEL_NOT_SUPPORTED_PATTERN =
  /(?:The requested model is not supported|invalid model(?:\s+name)?\s+['"`]?[a-z0-9._:/@-]+['"`]?(?=(?:\s*$|\s*[\n\r.,;:!?)]))|unknown model\s+['"`]?[a-z0-9._:/@-]+['"`]?(?=(?:\s*$|\s*[\n\r.,;:!?)]))|model(?:\s+name)?\s+['"`]?[a-z0-9._:/@-]+['"`]?\s+(?:is\s+)?(?:not found|does not exist|not supported|not available|unavailable)|404\b[^\n]*\bModel\s+not\s+found|No model available\b[^\n]*policy enablement)/i;

/**
 * Determines if Codex emitted a `turn.failed` event for a model that does not
 * support its custom tool schema.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isUnsupportedModelToolsError(output) {
  return output.split(/\r?\n/).some(line => {
    try {
      const event = JSON.parse(line);
      if (event?.type !== "turn.failed" || !event.error) return false;

      const candidates = [event.error];
      for (let visited = 0; visited < 8 && candidates.length > 0; visited++) {
        const current = candidates.shift();
        if (!current || typeof current !== "object") continue;
        if (current.message === "Invalid value: 'custom'" && current.param === "tools") return true;

        if (current.error && typeof current.error === "object") candidates.push(current.error);
        for (const value of [current.message, current.metadata?.raw]) {
          if (typeof value !== "string") continue;
          try {
            candidates.push(JSON.parse(value));
          } catch {
            // Ignore non-JSON strings.
          }
        }
      }
      return false;
    } catch {
      return false;
    }
  });
}

// Pattern: Generic HTTP 400 Bad Request responses emitted by engine / SDK wrappers.
// NOTE: keep in sync with HTTP_400_RESPONSE_ERROR_PATTERN in copilot_harness.cjs.
// Also matches "400 400 400 no model endpoints available given user constraints" which is emitted
// by the Copilot SDK when no model endpoints are available for the user's configured constraints.
// Also matches "400 400 400 stream_options: Extra inputs are not permitted" which is emitted when
// the Copilot SDK sends an OpenAI-only field to an Anthropic-type provider.
// The non-first alternatives are anchored to a leading "400" to avoid false positives from unrelated
// diagnostic or informational messages that might contain the phrase.
const HTTP_400_RESPONSE_ERROR_PATTERN =
  /(?:Response status code does not indicate success:\s*400(?:\s*\(Bad Request\))?|400[^\n]*no model endpoints available given user constraints|400[^\n]*stream_options:\s*Extra inputs are not permitted)/i;

// Pattern: AWF API proxy rejects a request because the model has no AI credits pricing configured
// and no default fallback pricing is set. Emitted as:
//   "400 400 Model "claude-opus-5" has no AI credits pricing and no default pricing is configured."
//   "400 Model "claude-opus-5" has no AI credits pricing"
// Captures the model name in group 1 for use in remediation guidance.
const MISSING_MODEL_PRICING_PATTERN = /Model\s+"([^"]+)"\s+has no AI credits pricing/i;

// Pattern: Copilot/CAPI quota exhaustion and rate-limit responses.
// Matches all observed forms:
//   "CAPIError: 429 429 quota exceeded"  (original observed form)
//   "CAPIError: 429 Too Many Requests"   (HTTP 429 form)
//   "CAPIError: Too Many Requests"       (no status code in message)
//   "Failed to get response from the AI model; retried 5 times ... Last error: 429 Too Many Requests"
//     (Copilot CLI's own retry-exhaustion message, no "CAPIError:" prefix — seen with both
//     429 and 5xx terminal statuses, e.g. "Last error: 503 Service Unavailable")
// All forms are treated as non-retryable; the Copilot CLI/SDK has already retried
// internally before surfacing this error (evidenced by "retried N times" context).
const CAPI_QUOTA_EXCEEDED_PATTERN = /CAPIError:\s*(?:429\s+)?(?:429\s+quota exceeded|Too Many Requests)|Failed to get response from the AI model;\s*retried\s+\d+\s+times[^\n]{0,300}?Last error:\s*(?:429|5\d{2})\b/i;

/**
 * Build a case-insensitive merged RegExp from literal/regex patterns.
 * @param {(RegExp|string)[]} patterns
 * @returns {RegExp}
 */
function buildCombinedPattern(patterns) {
  const patternSources = patterns.map(pattern => (pattern instanceof RegExp ? pattern.source : String(pattern))).filter(Boolean);
  return new RegExp(patternSources.join("|"), "i");
}

// Pattern: per-run LLM invocation cap exhausted.
// Matches both the Anthropic JSON error type ("max_runs_exceeded") and the
// human-readable message form ("Maximum LLM invocations exceeded") seen in
// both CAPI (Copilot CLI: "CAPIError: 429 Maximum LLM invocations exceeded (N/N)")
// and direct Anthropic API responses ("max_runs_exceeded").
// The pooled per-run invocation budget is saturated — retries cannot make progress.
const INVOCATION_CAP_EXCEEDED_PATTERN = buildCombinedPattern(MAX_RUNS_EXCEEDED_PATTERNS);

// Pattern: AWF API proxy consecutive cache miss limit exceeded.
// The AWF API proxy (engine-agnostic) enforces a configurable limit on back-to-back
// requests that miss the prompt cache (apiProxy.maxCacheMisses, default 5). When the
// limit is reached it rejects further requests with HTTP 403 and error type
// "max_cache_misses_exceeded". Two observable forms:
//   1) JSON error type in provider API response:  "max_cache_misses_exceeded"
//   2) Human-readable message from SDK wrapper:   "Maximum consecutive cache misses exceeded"
// Structured events from the AWF API proxy event log are checked separately via
// parseMaxCacheMissesExceededFromEventLog().
const MAX_CACHE_MISSES_EXCEEDED_PATTERN = /(?:\bmax_cache_misses_exceeded\b|\bmaximum\s+consecutive\s+cache\s+misses\s+exceeded\b)/i;

// Pattern: the sandbox's shell command-injection guard rejected a shell command believed to
// contain dangerous bash expansion patterns (command substitution, indirect expansion, parameter
// transformation, backtick substitution, etc.). Observed message form:
//   "...indirect expansion, or nested command substitution) that could enable arbitrary code
//   execution. Please rewrite the command without these expansion patterns."
// This guard can misfire on benign multi-line printf/safeoutputs CLI invocations. Retrying the
// identical command is pointless — it will be rejected again — so this is surfaced as a distinct,
// actionable diagnostic instead of a generic shell failure.
const SHELL_EXPANSION_GUARD_REJECTED_PATTERN = /could enable arbitrary code execution\b[\s\S]{0,200}?\brewrite the command without these expansion patterns\b/i;

/**
 * Determines if the collected output contains the observed Copilot/CAPI quota exhaustion error.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isCAPIQuotaExceededError(output) {
  return CAPI_QUOTA_EXCEEDED_PATTERN.test(output);
}

/**
 * Determines if the collected output indicates the per-run LLM invocation cap is exhausted.
 * This covers both the CAPI form ("CAPIError: 429 Maximum LLM invocations exceeded (N/N)")
 * and the Anthropic JSON form ("max_runs_exceeded"). The pooled budget cannot be recovered
 * within the current run — retrying is pointless.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isInvocationCapExceededError(output) {
  return isMaxRunsExceededError(output);
}

/**
 * Determines if the collected output indicates the AWF API proxy cache miss limit is exceeded.
 * Checks the agent stdio log for the text-form signal. The structured AWF API proxy event log
 * is checked separately in detectErrors() via parseMaxCacheMissesExceededFromEventLog().
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isMaxCacheMissesExceededError(output) {
  return MAX_CACHE_MISSES_EXCEEDED_PATTERN.test(output);
}

/**
 * Determines if the collected output shows the sandbox's shell command-injection guard
 * rejected a command for containing (or appearing to contain) dangerous bash expansion
 * patterns. Retrying the same command verbatim will not succeed; the agent should switch
 * to the `jq -Rs` file-piping pattern for multi-line safeoutputs CLI bodies instead.
 * @param {string} output - Collected stdout+stderr from the process
 * @returns {boolean}
 */
function isShellExpansionGuardRejectedError(output) {
  return SHELL_EXPANSION_GUARD_REJECTED_PATTERN.test(output);
}

/**
 * Determines whether the engine execution step was killed by the GitHub Actions
 * step-level `timeout-minutes` limit.
 *
 * A step timeout terminates the engine externally, so it can leave no timeout
 * signature in the agent stdio log (only GitHub's own annotation "The action '...'
 * has timed out after N minutes." in the runner log, which is not available here).
 * Without this check such runs are misreported as "engine terminated unexpectedly".
 *
 * Detection requires both:
 *   1. The engine execution step did not succeed (outcome is not "success"), and
 *   2. The engine ran for at least its configured timeout budget (minus a small
 *      tolerance for the delay between step start and the start timestamp write).
 *
 * @param {{ outcome?: string, timeoutMinutes?: string, startMs?: number, nowMs?: number }} options
 * @returns {boolean}
 */
function isStepTimeout({ outcome, timeoutMinutes, startMs, nowMs }) {
  if (!outcome || outcome === "success" || outcome === "skipped") return false;

  const minutes = parseInt(String(timeoutMinutes || "").trim(), 10);
  if (!Number.isFinite(minutes) || minutes <= 0) return false;

  const start = Number(startMs);
  const now = Number(nowMs);
  if (!Number.isFinite(start) || start <= 0 || !Number.isFinite(now)) return false;

  const elapsedMs = now - start;
  if (elapsedMs <= 0) return false;

  return elapsedMs >= minutes * 60_000 - STEP_TIMEOUT_TOLERANCE_MS;
}

/**
 * Read the engine CLI start timestamp (epoch milliseconds) written by the execution step.
 * @returns {number} Epoch milliseconds, or NaN when unavailable/unparsable
 */
function readAgentCLIStartMs() {
  try {
    if (!fs.existsSync(AGENT_CLI_START_MS_FILE)) return NaN;
    return parseInt(fs.readFileSync(AGENT_CLI_START_MS_FILE, "utf8").trim(), 10);
  } catch {
    return NaN;
  }
}

/**
 * Detect a step-level timeout from the current environment and on-disk start timestamp.
 * @returns {boolean}
 */
function detectStepTimeoutFromEnvironment() {
  return isStepTimeout({
    outcome: process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME,
    timeoutMinutes: process.env.GH_AW_ENGINE_STEP_TIMEOUT_MINUTES,
    startMs: readAgentCLIStartMs(),
    nowMs: Date.now(),
  });
}

/**
 * Normalize model names to a single safe line for GitHub Actions outputs and issue titles.
 * @param {string} value
 * @returns {string}
 */
function sanitizeModelName(value) {
  return value.replace(/\r?\n|\r/g, " ").trim();
}

/**
 * Extract model name from a "no AI credits pricing" error message.
 * @param {string} logContent - Contents of the agent stdio log
 * @returns {string} Model name, or empty string if not found
 */
function extractMissingModelPricingModelName(logContent) {
  const match = logContent.match(MISSING_MODEL_PRICING_PATTERN);
  return match ? sanitizeModelName(match[1]) : "";
}

/**
 * Detect known error patterns in a log string and return detection results.
 * @param {string} logContent - Contents of the agent stdio log
 * @returns {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean, capiQuotaExceededError: boolean, invocationCapExceeded: boolean, maxCacheMissesExceeded: boolean, missingModelPricingError: boolean, missingModelPricingModelName: string, shellExpansionGuardRejected: boolean }}
 */
function detectErrors(logContent) {
  const missingModelPricingModelName = extractMissingModelPricingModelName(logContent);
  return {
    inferenceAccessError: INFERENCE_ACCESS_ERROR_PATTERN.test(logContent),
    mcpPolicyError: MCP_POLICY_BLOCKED_PATTERN.test(logContent),
    agenticEngineTimeout: isAgenticEngineTimeout(logContent),
    modelNotSupportedError: MODEL_NOT_SUPPORTED_PATTERN.test(logContent) || isUnsupportedModelToolsError(logContent),
    http400ResponseError: HTTP_400_RESPONSE_ERROR_PATTERN.test(logContent),
    capiQuotaExceededError: isCAPIQuotaExceededError(logContent),
    invocationCapExceeded: isInvocationCapExceededError(logContent),
    maxCacheMissesExceeded: isMaxCacheMissesExceededError(logContent),
    missingModelPricingError: missingModelPricingModelName !== "",
    missingModelPricingModelName,
    shellExpansionGuardRejected: isShellExpansionGuardRejectedError(logContent),
  };
}

/**
 * Build GitHub Actions output lines from detection results.
 * @param {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean, capiQuotaExceededError: boolean, invocationCapExceeded: boolean, maxCacheMissesExceeded: boolean, missingModelPricingError: boolean, missingModelPricingModelName: string, shellExpansionGuardRejected: boolean }} results
 * @returns {string[]}
 */
function buildOutputLines(results) {
  const effectiveCAPIQuotaExceeded = results.capiQuotaExceededError && !results.invocationCapExceeded;
  return [
    `inference_access_error=${results.inferenceAccessError}`,
    `mcp_policy_error=${results.mcpPolicyError}`,
    `agentic_engine_timeout=${results.agenticEngineTimeout}`,
    `model_not_supported_error=${results.modelNotSupportedError}`,
    `http_400_response_error=${results.http400ResponseError}`,
    `capi_quota_exceeded_error=${effectiveCAPIQuotaExceeded}`,
    `invocation_cap_exceeded=${results.invocationCapExceeded}`,
    `max_cache_misses_exceeded=${results.maxCacheMissesExceeded}`,
    `missing_model_pricing_error=${results.missingModelPricingError}`,
    `missing_model_pricing_model_name=${results.missingModelPricingModelName}`,
    `shell_expansion_guard_rejected=${results.shellExpansionGuardRejected}`,
  ];
}

/**
 * Write GitHub Actions outputs to $GITHUB_OUTPUT.
 * @param {{ inferenceAccessError: boolean, mcpPolicyError: boolean, agenticEngineTimeout: boolean, modelNotSupportedError: boolean, http400ResponseError: boolean, capiQuotaExceededError: boolean, invocationCapExceeded: boolean, maxCacheMissesExceeded: boolean, missingModelPricingError: boolean, missingModelPricingModelName: string, shellExpansionGuardRejected: boolean }} results
 */
function writeOutputs(results) {
  const outputFile = process.env.GITHUB_OUTPUT;
  if (!outputFile) {
    process.stderr.write("[detect-agent-errors] GITHUB_OUTPUT not set — skipping output\n");
    return;
  }

  const lines = buildOutputLines(results);
  try {
    fs.appendFileSync(outputFile, lines.join("\n") + "\n");
  } catch (err) {
    process.stderr.write(`[detect-agent-errors] Failed to write to GITHUB_OUTPUT: ${getErrorMessage(err)}\n`);
  }
}

/**
 * Finds the most recently modified `*.log` file under `dir`, recursing into subdirectories.
 * @param {string} dir
 * @returns {string | undefined}
 */
function findMostRecentLogFile(dir) {
  /** @type {{ path: string, mtimeMs: number }[]} */
  const logFiles = [];

  /** @param {string} current */
  function walk(current) {
    let entries;
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const entryPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        walk(entryPath);
      } else if (entry.isFile() && entry.name.endsWith(".log")) {
        try {
          logFiles.push({ path: entryPath, mtimeMs: fs.statSync(entryPath).mtimeMs });
        } catch {
          // Ignore files that disappear or fail to stat between readdir and stat.
        }
      }
    }
  }

  walk(dir);
  if (logFiles.length === 0) {
    return undefined;
  }

  logFiles.sort((a, b) => b.mtimeMs - a.mtimeMs);
  return logFiles[0].path;
}

/**
 * Renders the most recently modified log file under the engine-provided internal logs
 * directory (GH_AW_ENGINE_INTERNAL_LOGS_DIR) to the step log, when the engine execution
 * step failed.
 *
 * Some engines (e.g. Codex CLI) write their own tracing/diagnostic output to files rather
 * than to stdout/stderr, so a bare non-zero exit code with no console output can still have
 * a diagnosable error recorded there. Without this, such failures are invisible black-box
 * "exit code 1" crashes even though the engine logged the real cause internally.
 *
 * This reuses the same renderLogFromFile helper used for the threat-detection log, so the
 * rendered content gets the same secret redaction and `::stop-commands::` wrapping
 * (preventing `::`-shaped log lines from being interpreted as workflow commands) without
 * duplicating that logic in shell.
 *
 * This is a no-op when GH_AW_ENGINE_INTERNAL_LOGS_DIR is not set, the execution outcome was
 * not "failure", or no log files are found under the directory.
 * @returns {Promise<void>}
 */
async function renderInternalEngineLogOnFailure() {
  const internalLogsDir = process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR;
  if (!internalLogsDir) {
    return;
  }

  const outcome = process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME;
  if (outcome !== "failure") {
    return;
  }

  const logFile = findMostRecentLogFile(internalLogsDir);
  if (!logFile) {
    process.stderr.write(`[detect-agent-errors] No engine internal log files found under ${internalLogsDir}\n`);
    return;
  }

  await renderLogFromFile(logFile, `Engine internal logs (${internalLogsDir})`, { tailLines: 200 });
}

async function main() {
  let logContent = "";

  if (fs.existsSync(LOG_FILE)) {
    try {
      logContent = fs.readFileSync(LOG_FILE, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${LOG_FILE}: ${getErrorMessage(err)}`, { cause: err });
    }
  } else {
    process.stderr.write(`[detect-agent-errors] Log file not found: ${LOG_FILE}\n`);
  }

  const stdioResults = detectErrors(logContent);

  // Also check the AWF firewall structured JSONL logs for the `unknown_model_ai_credits`
  // event — the API proxy event log is preferred and the audit log is used as a fallback.
  // These logs carry both the error type and the model name, providing a more reliable
  // detection source than text-scanning the stdio log.
  const { detected: auditMissingPricing, modelName: auditModelName } = parseUnknownModelAICreditsAndModelFromAuditLog();
  if (auditMissingPricing && !stdioResults.missingModelPricingError) {
    process.stderr.write(`[detect-agent-errors] Detected missing model pricing from firewall structured log: model "${auditModelName}" has no AI credits pricing configured\n`);
  }

  // Also check the AWF API proxy event logs for the `max_cache_misses_exceeded` structured
  // event. This covers all engines since the proxy guardrail fires independently of the
  // underlying AI engine.
  const eventLogCacheMissesExceeded = parseMaxCacheMissesExceededFromEventLog();
  if (eventLogCacheMissesExceeded && !stdioResults.maxCacheMissesExceeded) {
    process.stderr.write("[detect-agent-errors] Detected max cache misses exceeded from AWF API proxy event log\n");
  }

  // A GitHub Actions step-level timeout kills the engine externally and can leave no
  // timeout signature in the agent log, so it is detected from the step outcome and the
  // engine run duration instead.
  const stepTimeout = detectStepTimeoutFromEnvironment();
  if (stepTimeout && !stdioResults.agenticEngineTimeout) {
    process.stderr.write(`[detect-agent-errors] Detected step timeout: the engine execution step reached its ${process.env.GH_AW_ENGINE_STEP_TIMEOUT_MINUTES}-minute timeout-minutes budget and was terminated\n`);
  }

  const results = {
    ...stdioResults,
    agenticEngineTimeout: stdioResults.agenticEngineTimeout || stepTimeout,
    maxCacheMissesExceeded: stdioResults.maxCacheMissesExceeded || eventLogCacheMissesExceeded,
    missingModelPricingError: stdioResults.missingModelPricingError || auditMissingPricing,
    missingModelPricingModelName: stdioResults.missingModelPricingModelName || sanitizeModelName(auditModelName),
  };

  if (results.inferenceAccessError) {
    process.stderr.write("[detect-agent-errors] Detected inference access error in agent log\n");
  }
  if (results.mcpPolicyError) {
    process.stderr.write("[detect-agent-errors] Detected MCP policy error in agent log\n");
  }
  if (stdioResults.agenticEngineTimeout) {
    process.stderr.write("[detect-agent-errors] Detected agentic engine timeout signature in agent log\n");
  }
  if (results.modelNotSupportedError) {
    process.stderr.write("[detect-agent-errors] Detected model configuration error: configured model is invalid or unavailable for this engine/account\n");
  }
  if (results.http400ResponseError) {
    process.stderr.write("[detect-agent-errors] Detected HTTP 400 response error in agent log\n");
  }
  if (results.capiQuotaExceededError) {
    process.stderr.write("[detect-agent-errors] Detected CAPI quota exhaustion: Copilot quota has been exceeded\n");
  }
  if (results.invocationCapExceeded) {
    process.stderr.write("[detect-agent-errors] Detected invocation cap exhaustion: the pooled per-run LLM invocation budget is fully saturated\n");
  }
  if (results.maxCacheMissesExceeded) {
    process.stderr.write("[detect-agent-errors] Detected max cache misses exceeded: the AWF API proxy consecutive cache miss limit was reached\n");
  }
  if (results.missingModelPricingError && !auditMissingPricing) {
    process.stderr.write(`[detect-agent-errors] Detected missing model pricing: model "${results.missingModelPricingModelName}" has no AI credits pricing configured\n`);
  }
  if (results.shellExpansionGuardRejected) {
    process.stderr.write(
      "[detect-agent-errors] Detected sandbox shell expansion guard rejection: a shell command was rejected for dangerous bash expansion patterns; use the jq -Rs file-piping pattern for multi-line safeoutputs CLI bodies instead of retrying\n"
    );
  }

  writeOutputs(results);

  await renderInternalEngineLogOnFailure();
}

if (require.main === module) {
  main().catch(err => {
    process.stderr.write(`[detect-agent-errors] Unhandled error: ${err instanceof Error && err.stack ? err.stack : getErrorMessage(err)}\n`);
  });
}

module.exports = {
  main,
  detectErrors,
  extractMissingModelPricingModelName,
  isCAPIQuotaExceededError,
  isInvocationCapExceededError,
  isMaxCacheMissesExceededError,
  isAgenticEngineTimeout,
  isUnsupportedModelToolsError,
  isStepTimeout,
  detectStepTimeoutFromEnvironment,
  INFERENCE_ACCESS_ERROR_PATTERN,
  MCP_POLICY_BLOCKED_PATTERN,
  AGENTIC_ENGINE_TIMEOUT_PATTERN,
  WATCHDOG_SIGTERM_PATTERN,
  STEP_TIMEOUT_SIGTERM_PATTERN,
  PROCESS_CLOSED_SIGTERM_PATTERN,
  MODEL_NOT_SUPPORTED_PATTERN,
  HTTP_400_RESPONSE_ERROR_PATTERN,
  CAPI_QUOTA_EXCEEDED_PATTERN,
  INVOCATION_CAP_EXCEEDED_PATTERN,
  MAX_CACHE_MISSES_EXCEEDED_PATTERN,
  MISSING_MODEL_PRICING_PATTERN,
  SHELL_EXPANSION_GUARD_REJECTED_PATTERN,
  isShellExpansionGuardRejectedError,
  buildOutputLines,
  findMostRecentLogFile,
  renderInternalEngineLogOnFailure,
};
