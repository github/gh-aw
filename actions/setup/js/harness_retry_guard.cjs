// @ts-check

"use strict";

const { emitInfrastructureIncomplete } = require("./safeoutputs_cli.cjs");

// Stop retrying this long before the step hard timeout so the harness can emit
// structured safe-output diagnostics instead of being terminated by Actions.
const SOFT_TIMEOUT_BUFFER_MS = 90 * 1000;

const AI_CREDITS_EXCEEDED_PATTERNS = [/\bmax[\s_-]*ai[\s_-]*credits[\s_-]*exceeded\b/i, /\bai[\s_-]*credits[\s_-]*rate[\s_-]*limit[\s_-]*error\b/i, /ai[\s_-]*credits?.*(?:rate[\s-]*limit|limit exceeded|budget exceeded|exceeded)/i];

// Canonical rejection emitted by the AWF API proxy once the configured `apiProxy.maxAiCredits`
// budget is exhausted: an HTTP 403 whose message carries the proxy-computed used/max pair, e.g.
//   "API Error: 403 Maximum AI credits exceeded (302.111025 / 300)."
// Only the proxy can answer a provider request with this status/message pair, so it is treated as
// trusted evidence of budget enforcement. This matters because the firewall audit JSONL — the other
// trusted source — is only flushed during container teardown, which happens after the harness has
// already classified the failed attempt.
const AI_CREDITS_EXCEEDED_PROXY_REJECTION_RE = /\b403\b[^\n]{0,80}?maximum ai credits exceeded\s*\(\s*(\d+(?:\.\d+)?)\s*\/\s*(\d+(?:\.\d+)?)\s*\)/i;

const AWF_API_PROXY_BLOCKING_REQUESTS_PATTERNS = [/\bawf\b.*\bapi[\s_-]*proxy\b.*\bblocking requests\b/i, /\bapi[\s_-]*proxy\b.*\bblocking requests\b/i, /\bapi[\s_-]*proxy\b.*\bblocked requests?\b/i, /\bDIFC_FILTERED\b/];
const GOAL_ALREADY_ACTIVE_PATTERNS = [/\bthis thread already has a goal\b[\s\S]*?\buse update_goal\b/i, /\bcannot create a new goal because this thread has an unfinished goal\b;\s*\bcomplete the existing goal first\b/i];

// Patterns to detect Anthropic "max_runs_exceeded" (HTTP 403).
// This occurs when the per-session LLM invocation quota is exhausted.
// Retrying is pointless because each fresh-run attempt immediately fails with
// the same 403 until the quota resets.  Matches both the JSON error type
// ("max_runs_exceeded") and the human-readable message
// ("Maximum LLM invocations exceeded").
const MAX_RUNS_EXCEEDED_PATTERNS = [/\bmax_runs_exceeded\b/i, /Maximum LLM invocations exceeded/i];

// Guardrail rejections emitted by the AWF API proxy as HTTP 403 responses. These are
// deliberate policy decisions by the proxy, never credential problems: the offending counter
// is not reset by a fresh attempt, so retrying against the same proxy is guaranteed to fail
// again. Some CLIs (notably the Copilot CLI) discard the structured body and print a generic
// "Authentication failed with provider ... (HTTP 403)" line, so the guard name must be
// recovered from the proxy's own structured logs (see ai_credits_context.cjs); these patterns
// are the text-form fallback for engines that do surface the proxy body.
const API_PROXY_GUARD_REJECTION_PATTERNS = [
  {
    guard: "max_cache_misses_exceeded",
    pattern: /(?:\bmax_cache_misses_exceeded\b|\bmaximum\s+consecutive\s+cache\s+misses\s+exceeded\b)/i,
    counterRe: /maximum\s+consecutive\s+cache\s+misses\s+exceeded\s*\(\s*(\d+)\s*\/\s*(\d+)\s*\)/i,
    counterNames: ["consecutive_cache_misses", "max_cache_misses"],
  },
  {
    guard: "effective_tokens_limit_exceeded",
    pattern: /(?:\beffective_tokens_limit_exceeded\b|\bmaximum\s+effective\s+tokens\s+exceeded\b)/i,
    counterRe: /maximum\s+effective\s+tokens\s+exceeded\s*\(\s*(\d+)\s*\/\s*(\d+)\s*\)/i,
    counterNames: ["effective_tokens", "max_effective_tokens"],
  },
  {
    guard: "permission_denied_limit_exceeded",
    pattern: /\bpermission_denied_limit_exceeded\b/i,
    counterRe: /permission\s+denied\s+limit\s+exceeded\s*\(\s*(\d+)\s*\/\s*(\d+)\s*\)/i,
    counterNames: ["permission_denied_count", "max_permission_denied"],
  },
  { guard: "model_policy_violation", pattern: /\bmodel_policy_violation\b/i, counterRe: null, counterNames: [] },
];

/**
 * Detect an AWF API proxy guardrail rejection in harness output.
 *
 * Only the text form is inspected here; the authoritative source is the proxy's structured
 * log, which callers should consult as well (the Copilot CLI replaces the proxy body with a
 * generic authentication message). Returns the guard name and any counters carried in the
 * human-readable message.
 *
 * @param {unknown} output
 * @returns {{ guard: string, counters: Record<string, string|number> } | null}
 */
function parseAPIProxyGuardRejection(output) {
  const safeOutput = typeof output === "string" ? output : "";
  if (!safeOutput) return null;
  for (const { guard, pattern, counterRe, counterNames } of API_PROXY_GUARD_REJECTION_PATTERNS) {
    if (!pattern.test(safeOutput)) continue;
    /** @type {Record<string, string|number>} */
    const counters = {};
    const match = counterRe ? counterRe.exec(safeOutput) : null;
    if (match) {
      counterNames.forEach((name, index) => {
        const value = Number.parseInt(match[index + 1], 10);
        if (Number.isFinite(value)) counters[name] = value;
      });
    }
    return { guard, counters };
  }
  return null;
}

// Common authentication failure patterns shared across all harnesses.
// Matches:
//   - "Authentication failed (Request ID: ...)" — Anthropic/OpenAI direct auth error
//   - `"error":"authentication_failed"` — Claude Code stream-JSON error field (e.g. 401 via AWF proxy)
//   - "not logged in" — Claude Code message when no credentials are available
const AUTHENTICATION_FAILED_PATTERNS = [/Authentication failed(?:\s*\(Request ID:[^)]+\))?/i, /"error"\s*:\s*"authentication_failed"/i, /not logged in/i];

/**
 * @param {unknown} output
 * @returns {boolean}
 */
function isMaxRunsExceededError(output) {
  const safeOutput = typeof output === "string" ? output : "";
  return MAX_RUNS_EXCEEDED_PATTERNS.some(pattern => pattern.test(safeOutput));
}

/**
 * Determines if the collected output contains an authentication failed error.
 * @param {unknown} output
 * @returns {boolean}
 */
function isAuthenticationFailedError(output) {
  const safeOutput = typeof output === "string" ? output : "";
  return AUTHENTICATION_FAILED_PATTERNS.some(pattern => pattern.test(safeOutput));
}

/**
 * Extracts the AWF API proxy AI-credits budget rejection (HTTP 403) from harness output.
 *
 * `output` is the combined stdout+stderr of the child process (see process_runner.cjs),
 * which also carries verbatim assistant/model text. Matching the rejection text anywhere
 * in that blob would let an unrelated assistant response that merely quotes or discusses
 * this phrase (followed by any non-zero exit) masquerade as a trusted budget-abort signal.
 * To require an engine-authenticated source, this only considers lines that parse as a
 * standalone JSON object AND carry a structured API-error marker that only the harness'
 * own transport layer sets — mirroring how `isInvalidRequestError` validates codex
 * `turn.failed` events. Claude Code stamps the JSON event wrapping a proxy rejection with
 * `is_api_error_message: true` and a string `error` field; plain conversational turns never
 * set these. Free-form text (JSON-less lines, or JSON without either marker) is ignored.
 * Returns null when no line carries the authenticated signature, or when the reported
 * usage does not actually reach the reported budget.
 * @param {unknown} output
 * @returns {{ aiCredits: number, maxAICredits: number } | null}
 */
function parseAICreditsExceededProxyRejection(output) {
  // Contract: callers must pass the joined stdout+stderr string (`result.output`, as built by
  // process_runner.cjs), matching every other guard in this file. Non-string input (e.g. an
  // array/object of structured log lines) is treated as "no output" rather than coerced, since
  // `String(someObject)` would produce a meaningless "[object Object]"-style value.
  const safeOutput = typeof output === "string" ? output : "";
  for (const line of safeOutput.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed[0] !== "{") continue;
    /** @type {unknown} */
    let parsed;
    try {
      parsed = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (!parsed || typeof parsed !== "object") continue;
    // prettier-ignore
    const record = /** @type {Record<string, unknown>} */ (parsed);
    const isEngineFlaggedApiError = record.is_api_error_message === true || typeof record.error === "string";
    if (!isEngineFlaggedApiError) continue;
    const match = AI_CREDITS_EXCEEDED_PROXY_REJECTION_RE.exec(JSON.stringify(record));
    if (!match) continue;
    const aiCredits = Number.parseFloat(match[1]);
    const maxAICredits = Number.parseFloat(match[2]);
    if (!Number.isFinite(aiCredits) || !Number.isFinite(maxAICredits) || maxAICredits <= 0) continue;
    if (aiCredits < maxAICredits) continue;
    return { aiCredits, maxAICredits };
  }
  return null;
}

/**
 * Detect retry guard conditions that should stop harness retries immediately.
 * @param {unknown} output
 * @returns {{ aiCreditsExceeded: boolean, awfAPIProxyBlockingRequests: boolean, goalAlreadyActive: boolean, maxRunsExceeded: boolean, apiProxyGuardRejection: { guard: string, counters: Record<string, string|number> } | null }}
 */
function detectNonRetryableHarnessGuard(output) {
  const safeOutput = typeof output === "string" ? output : "";
  return {
    aiCreditsExceeded: AI_CREDITS_EXCEEDED_PATTERNS.some(pattern => pattern.test(safeOutput)),
    awfAPIProxyBlockingRequests: AWF_API_PROXY_BLOCKING_REQUESTS_PATTERNS.some(pattern => pattern.test(safeOutput)),
    goalAlreadyActive: GOAL_ALREADY_ACTIVE_PATTERNS.some(pattern => pattern.test(safeOutput)),
    maxRunsExceeded: isMaxRunsExceededError(safeOutput),
    apiProxyGuardRejection: parseAPIProxyGuardRejection(safeOutput),
  };
}

/**
 * Compute a soft timeout deadline for the harness based on GH_AW_TIMEOUT_MINUTES.
 * Returns null when timeout is unset/invalid.
 * @param {number} driverStartTime
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {{ timeoutMinutes: number, softDeadlineMs: number } | null}
 */
function buildSoftTimeoutGuard(driverStartTime, env = process.env) {
  const timeoutMinutes = Number(env.GH_AW_TIMEOUT_MINUTES);
  if (!Number.isFinite(timeoutMinutes) || timeoutMinutes <= 0) {
    return null;
  }
  const hardTimeoutMs = Math.floor(timeoutMinutes * 60 * 1000);
  const softDeadlineMs = driverStartTime + Math.max(hardTimeoutMs - SOFT_TIMEOUT_BUFFER_MS, 1000);
  return { timeoutMinutes, softDeadlineMs };
}

/**
 * Emit infrastructure incomplete signal and log when the soft timeout guard fires.
 * @param {{ timeoutMinutes: number, softDeadlineMs: number }} guard
 * @param {string} context - Short label for where the check fired (e.g. "before attempt 2")
 * @param {string} harnessName - Human-readable name of the harness (e.g. "Copilot harness")
 * @param {(message: string) => void} logFn - Harness-specific log function
 */
function emitSoftTimeoutSignal(guard, context, harnessName, logFn) {
  emitInfrastructureIncomplete(`${harnessName} reached soft retry budget before the ${guard.timeoutMinutes}-minute step timeout. ` + "Stopping retries early to preserve structured failure output.");
  logFn(`soft-timeout guard reached ${context}: timeoutMinutes=${guard.timeoutMinutes} bufferMs=${SOFT_TIMEOUT_BUFFER_MS}`);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    detectNonRetryableHarnessGuard,
    AI_CREDITS_EXCEEDED_PATTERNS,
    AI_CREDITS_EXCEEDED_PROXY_REJECTION_RE,
    AWF_API_PROXY_BLOCKING_REQUESTS_PATTERNS,
    GOAL_ALREADY_ACTIVE_PATTERNS,
    MAX_RUNS_EXCEEDED_PATTERNS,
    API_PROXY_GUARD_REJECTION_PATTERNS,
    AUTHENTICATION_FAILED_PATTERNS,
    isMaxRunsExceededError,
    isAuthenticationFailedError,
    parseAPIProxyGuardRejection,
    parseAICreditsExceededProxyRejection,
    SOFT_TIMEOUT_BUFFER_MS,
    buildSoftTimeoutGuard,
    emitSoftTimeoutSignal,
  };
}
