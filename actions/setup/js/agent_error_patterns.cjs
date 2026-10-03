// @ts-check

const { MAX_RUNS_EXCEEDED_PATTERNS, isMaxRunsExceededError } = require("./harness_error_patterns.cjs");
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
// Also matches a standalone "400 Bad Request" line emitted by the Copilot CLI, and
// "400 400 400 no model endpoints available given user constraints" which is emitted
// by the Copilot SDK when no model endpoints are available for the user's configured constraints.
// Also matches "400 400 400 stream_options: Extra inputs are not permitted" which is emitted when
// the Copilot SDK sends an OpenAI-only field to an Anthropic-type provider.
// The non-first alternatives are anchored to a leading "400" to avoid false positives from unrelated
// diagnostic or informational messages that might contain the phrase.
const HTTP_400_RESPONSE_ERROR_PATTERN =
  /(?:Response status code does not indicate success:\s*400(?:\s*\(Bad Request\))?|(?:^|\r?\n)[ \t]*400 Bad Request[ \t]*(?=\r?\n|$)|400[^\n]*no model endpoints available given user constraints|400[^\n]*stream_options:\s*Extra inputs are not permitted)/i;

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
//     (Copilot CLI's own retry-exhaustion message, no "CAPIError:" prefix)
// Quota errors remain non-retryable after the CLI's internal retries.
const CAPI_QUOTA_EXCEEDED_PATTERN = /CAPIError:\s*(?:429\s+)?(?:429\s+quota exceeded|Too Many Requests)|Failed to get response from the AI model;\s*retried\s+\d+\s+times[^\n]{0,300}?Last error:\s*429\b/i;

// Any retry-exhausted 5xx is treated as transient and may recover during harness backoff.
const CAPI_SERVER_ERROR_PATTERN = /Failed to get response from the AI model;\s*retried\s+\d+\s+times[^\n]{0,300}?Last error:\s*5\d{2}\b/i;

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

function isCAPIServerError(output) {
  return CAPI_SERVER_ERROR_PATTERN.test(output);
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
  const modelDiagnostics = logContent
    .split(/\r?\n/)
    .filter(line => !/\bWARN\s+codex_models_manager::model_info: Unknown model\b.*\bfallback model metadata\./.test(line) && !/^"?Model metadata unavailable; using fallback metadata\."?$/.test(line))
    .join("\n");
  return {
    inferenceAccessError: INFERENCE_ACCESS_ERROR_PATTERN.test(logContent),
    mcpPolicyError: MCP_POLICY_BLOCKED_PATTERN.test(logContent),
    agenticEngineTimeout: isAgenticEngineTimeout(logContent),
    modelNotSupportedError: MODEL_NOT_SUPPORTED_PATTERN.test(modelDiagnostics) || isUnsupportedModelToolsError(logContent),
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

module.exports = {
  sanitizeModelName,
  detectErrors,
  extractMissingModelPricingModelName,
  isCAPIQuotaExceededError,
  isCAPIServerError,
  isInvocationCapExceededError,
  isMaxCacheMissesExceededError,
  isAgenticEngineTimeout,
  isUnsupportedModelToolsError,
  isStepTimeout,
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
};
