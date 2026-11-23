// @ts-check

"use strict";

const { iterateJsonl, iterateJsonlEvents, iterateLines } = require("./jsonl_helpers.cjs");
const { BUILT_IN_PATTERNS } = require("./redact_secrets.cjs");
const { collectAddMaskedValues, redactMaskedValues } = require("./add_mask_redaction.cjs");

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

const MODEL_ENDPOINT_MISMATCH_PATTERN =
  /\bCannot translate Copilot request feature\b|\bUnsupported Responses custom tool\b|\bmodel_endpoint_incompatible\b|\bnot accessible via the [^\r\n]{1,160}? endpoint\b|\bRouting model\s+["'`][a-z0-9._:/@-]{1,200}["'`]\s+to\s+\/[a-z0-9._/-]{1,160}\s+is incompatible\b/i;

/**
 * Extract a model/endpoint incompatibility, retaining only the matching message line.
 * @param {unknown} output
 * @returns {{ model?: string, endpoint?: string, detail: string } | null}
 */
function parseModelEndpointMismatch(output) {
  if (typeof output !== "string") return null;
  const maskedValues = collectAddMaskedValues(output);
  for (const line of output.split(/\r\n|\r|\n/)) {
    if (!MODEL_ENDPOINT_MISMATCH_PATTERN.test(line)) continue;
    // Redact before bounding so truncation cannot leave a partial credential exposed.
    let redactedLine = redactMaskedValues(line, maskedValues);
    for (const { pattern } of BUILT_IN_PATTERNS) redactedLine = redactedLine.replace(pattern, "***REDACTED***");
    redactedLine = redactedLine.replace(/\bBearer\s+[a-z0-9._~+/-]+=*/gi, "******");
    const match = MODEL_ENDPOINT_MISMATCH_PATTERN.exec(redactedLine);
    if (!match) continue;
    // Keep the actual signature even when a long log prefix precedes it.
    const start = Math.max(0, match.index - 200);
    const detail = redactedLine.slice(start, start + 1000).trim();
    const message = redactedLine.replace(/\\"/g, '"');
    const model = /\bmodel(?:[_ -]name)?["'`]?\s*(?:[:=]\s*|\s+)["'`]?([a-z0-9][a-z0-9._:/@-]{0,199})/i.exec(message)?.[1];
    const endpoint =
      /\bnot accessible via the\s+["'`]?([a-z0-9/][a-z0-9._:/ -]{0,159}?)["'`]?\s+endpoint\b/i.exec(message)?.[1] ||
      /\bendpoint["'`]?\s*[:=]\s*["'`]?([a-z0-9/][a-z0-9._:/-]{0,159})/i.exec(message)?.[1] ||
      /\bRouting model\s+["'`][a-z0-9._:/@-]{1,200}["'`]\s+to\s+(\/[a-z0-9._/-]{1,160})\s+is incompatible\b/i.exec(message)?.[1] ||
      (/\bUnsupported Responses custom tool\b/i.test(message) ? "responses" : undefined);
    return { ...(model && !/^(?:is|not|endpoint|request|policy)$/i.test(model) ? { model } : {}), ...(endpoint ? { endpoint } : {}), detail };
  }
  return null;
}

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
 * Select diagnostic fields without inspecting metadata, tool input, or assistant prose.
 * The caller must first attribute the record as a native error.
 * @param {any} record
 * @returns {any}
 */
function selectNativeErrorDiagnostic(record) {
  /** @param {any} value @param {number} depth @returns {any} */
  const select = (value, depth) => {
    if (depth > 8) return undefined;
    if (typeof value === "string") {
      try {
        return select(JSON.parse(value), depth + 1);
      } catch {
        return value;
      }
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const diagnostic = {};
    for (const key of ["type", "code", "error_code", "errorCode", "error_type", "errorType", "terminal_reason", "statusCode", "status_code", "status", "error_status", "api_error_status", "subtype"]) {
      if (typeof value[key] === "string" || typeof value[key] === "number") diagnostic[key] = value[key];
    }
    for (const key of ["error", "message", "errorMessage"]) {
      if (value[key] !== undefined) diagnostic[key] = select(value[key], depth + 1);
    }
    if (value.is_api_error_message === true) diagnostic.is_api_error_message = true;
    // Claude marks API-error assistant content as transport text, not conversation.
    // Its canonical error envelope retains the error identifier and assistant message.
    if ((value.type === "assistant" && value.is_api_error_message === true) || (typeof value.error === "string" && value.message?.role === "assistant")) {
      const content = value.message?.content;
      if (Array.isArray(content))
        diagnostic.message = content
          .filter(block => block?.type === "text" && typeof block.text === "string")
          .map(block => {
            const text = select(block.text, depth + 1);
            return typeof text === "string" ? text : (JSON.stringify(text) ?? "");
          })
          .join("\n");
    }
    if (value.is_error === true) {
      diagnostic.is_error = true;
      diagnostic.result = select(value.result, depth + 1);
      if (Array.isArray(value.errors)) diagnostic.errors = value.errors.map(error => select(error, depth + 1));
    }
    return diagnostic;
  };
  return select(record, 0);
}

/**
 * Endpoint incompatibility wins only within the originating diagnostic.
 * @param {string} guard
 * @param {string} diagnostic
 * @returns {boolean}
 */
function shouldSuppressGuardCategory(guard, diagnostic) {
  return guard === "model_policy_violation" && parseModelEndpointMismatch(diagnostic) !== null;
}

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
  const diagnostics = [];
  for (const record of iterateJsonl(safeOutput)) {
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    const isApiErrorEnvelope = record.type === "assistant" && record.is_api_error_message === true;
    const isNativeTransportError = record.type === "error" || record.type === "turn.failed" || record.type === "session.error";
    const isGuardRecord = API_PROXY_GUARD_REJECTION_PATTERNS.some(({ guard }) => record.type === guard || ((isNativeTransportError || isApiErrorEnvelope) && record.code === guard));
    const isFailedResult = record.type === "result" && record.is_error === true;
    const isTransportError = isNativeTransportError || isApiErrorEnvelope || isFailedResult;
    if (isGuardRecord || isTransportError) {
      const diagnostic = record.type === "session.error" && record.data && typeof record.data === "object" ? record.data : record;
      diagnostics.push(JSON.stringify(selectNativeErrorDiagnostic(diagnostic)) ?? "");
    }
    const candidates = record.type === "unknown_model_ai_credits" ? [record] : isNativeTransportError || isApiErrorEnvelope ? [record.error, record.message] : [];
    // Only unwrap transport errors, never assistant messages or tool results.
    for (let visited = 0; visited < 8 && candidates.length > 0; visited++) {
      let candidate = candidates.shift();
      if (typeof candidate === "string") {
        try {
          candidate = JSON.parse(candidate);
        } catch {
          continue;
        }
      }
      if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) continue;
      if (candidate.type === "unknown_model_ai_credits" || candidate.code === "unknown_model_ai_credits") {
        return { guard: "unknown_model_ai_credits", counters: {} };
      }
      candidates.push(candidate.error, candidate.message);
    }
  }
  // Tool results can quote entire lock files containing guard names. Only inspect
  // transport diagnostics, not assistant content or command output.
  for (const { guard, pattern, counterRe, counterNames } of API_PROXY_GUARD_REJECTION_PATTERNS) {
    for (const diagnosticText of diagnostics) {
      if (!pattern.test(diagnosticText) || shouldSuppressGuardCategory(guard, diagnosticText)) continue;
      /** @type {Record<string, string|number>} */
      const counters = {};
      const match = counterRe ? counterRe.exec(diagnosticText) : null;
      if (match) {
        counterNames.forEach((name, index) => {
          const value = Number.parseInt(match[index + 1], 10);
          if (Number.isFinite(value)) counters[name] = value;
        });
      }
      return { guard, counters };
    }
  }
  return null;
}

// Common authentication failure patterns shared across all harnesses.
// Matches:
//   - "Authentication failed (Request ID: ...)" — Anthropic/OpenAI direct auth error
//   - `"error":"authentication_failed"` — Claude Code stream-JSON error field (e.g. 401 via AWF proxy)
//   - "not logged in" — Claude Code message when no credentials are available
const AUTHENTICATION_FAILED_PATTERNS = [/Authentication failed(?:\s*\(Request ID:[^)]+\))?/i, /"error"\s*:\s*"authentication_failed"/i, /not logged in/i];

const HTML_ERROR_BODY_PATTERN = /\bJust a moment\.{3}|\bcf-chl\b|\bchallenge-platform\b|<!doctype[ \t]+html\b|<html[\s>]/i;
const API_ERROR_CONTEXT_PATTERN =
  /\b(?:API error|failed to authenticate|authentication failed|authentication_failed|HTTP(?:\/\d(?:\.\d)?)?[ \t]+[45]\d\d|unexpected status[ \t]+[45]\d\d|[45]\d\d[ \t]+(?:Forbidden|Unauthorized|Bad Gateway|Service Unavailable))\b/i;

const HTTP_RESPONSE_CONTEXT_PATTERN = /\b(?:API error|HTTP(?:\/\d(?:\.\d)?)?[ \t]+[45]\d\d|unexpected status[ \t]+[45]\d\d|[45]\d\d[ \t]+(?:Forbidden|Unauthorized|Bad Gateway|Service Unavailable))\b/i;
const HTTP_HEADER_PATTERN = /^\s*[a-z][a-z0-9-]*:[ \t]*[^\r\n]*\s*$/i;
const HTML_BODY_START_PATTERN = /^\s*(?:<!doctype\b|<html\b|Just a moment\.{3}|cf-chl\b|challenge-platform\b|\/cdn-cgi\/challenge-platform\b)/i;

/**
 * Select only native error payloads. Ordinary items and successful results cannot
 * lend error attribution to quoted challenge text.
 * @param {any} event
 * @returns {any}
 */
function nativeResponseDiagnostic(event) {
  if (!event || typeof event !== "object" || Array.isArray(event)) return undefined;
  if (event.type === "assistant" && (event.is_api_error_message === true || event.error === "authentication_failed")) return selectNativeErrorDiagnostic(event);
  if (event.type === "result" && event.is_error === true) return selectNativeErrorDiagnostic(event);
  if (["error", "session.error", "turn.failed"].includes(event.type)) return selectNativeErrorDiagnostic(event.type === "session.error" ? (event.data ?? event) : event);
  if (/^item\./.test(event.type ?? "") && event.item?.type === "error") return selectNativeErrorDiagnostic({ ...event.item, message: event.item.message ?? event.item.text });
  if (event.event === "result" && event.result?.status === "ERROR") return selectNativeErrorDiagnostic(event.result);
  if (!event.type && event.error != null) {
    const hasNativeStructure = [event, event.error].some(
      diagnostic =>
        diagnostic &&
        typeof diagnostic === "object" &&
        !Array.isArray(diagnostic) &&
        (typeof diagnostic.message === "string" ||
          diagnostic.message?.role === "assistant" ||
          typeof diagnostic.errorMessage === "string" ||
          diagnostic.is_api_error_message === true ||
          (diagnostic.is_error === true && (typeof diagnostic.result === "string" || (diagnostic.result && typeof diagnostic.result === "object") || Array.isArray(diagnostic.errors))))
    );
    if (hasNativeStructure) return selectNativeErrorDiagnostic(event);
  }
  return undefined;
}

/**
 * Keep sibling diagnostics separate, while retaining an error identifier alongside
 * its own message. Embedded JSON has already been decoded by the diagnostic selector.
 * @param {any} value
 * @param {string} [prefix]
 * @returns {Generator<string>}
 */
function* responseDiagnosticTexts(value, prefix = "") {
  if (typeof value === "string") {
    yield prefix + value;
    return;
  }
  if (Array.isArray(value)) {
    for (const child of value) yield* responseDiagnosticTexts(child, prefix);
    return;
  }
  if (!value || typeof value !== "object") return;
  const errorIsCode = typeof value.error === "string" && /^[a-z][a-z0-9_]*$/i.test(value.error);
  const codes = {};
  for (const [key, child] of Object.entries(value)) {
    if (!["message", "errorMessage", "result", "errors"].includes(key) && (key !== "error" || errorIsCode) && (typeof child === "string" || typeof child === "number" || typeof child === "boolean")) codes[key] = child;
  }
  const context = prefix + (Object.keys(codes).length ? JSON.stringify(codes) + " " : "");
  let hasMessage = false;
  for (const key of ["message", "errorMessage", "result", "errors", "error"]) {
    if (value[key] === undefined || (key === "error" && errorIsCode)) continue;
    hasMessage = true;
    yield* responseDiagnosticTexts(value[key], context);
  }
  if (!hasMessage && context) yield context;
}

/**
 * Return the end of one HTML response, never crossing a separate error or JSON
 * record. Header association is bounded; the body can exceed the retry-signal tail.
 * @param {string[]} lines
 * @param {number} index
 * @param {boolean} native
 * @returns {number | null}
 */
function htmlResponseEnd(lines, index, native) {
  const line = lines[index];
  let body = index;
  if (!(HTML_ERROR_BODY_PATTERN.test(line) && (native || API_ERROR_CONTEXT_PATTERN.test(line)))) {
    if (!HTTP_RESPONSE_CONTEXT_PATTERN.test(line) && !(native && API_ERROR_CONTEXT_PATTERN.test(line))) return null;
    let headerChars = 0;
    for (body = index + 1; body < lines.length && body <= index + 16; body++) {
      if (API_ERROR_CONTEXT_PATTERN.test(lines[body]) || lines[body].trimStart().startsWith("{")) return null;
      if (HTML_BODY_START_PATTERN.test(lines[body])) break;
      headerChars += lines[body].length;
      if (headerChars > 8192) return null;
      if (lines[body].trim() && !HTTP_HEADER_PATTERN.test(lines[body])) return null;
    }
    if (body >= lines.length || body > index + 16) return null;
  }
  // Only a completed HTML document owns arbitrary following body text.
  if (!/<!doctype[ \t]+html\b|<html[\s>]/i.test(lines[body]) || /<\/html\s*>/i.test(lines[body])) return body;
  let end = body;
  let bodyChars = lines[body].length;
  while (end + 1 < lines.length && bodyChars < 256 * 1024) {
    const next = lines[end + 1];
    if (API_ERROR_CONTEXT_PATTERN.test(next) || AUTHENTICATION_FAILED_PATTERNS.some(pattern => pattern.test(next)) || next.trimStart().startsWith("{")) break;
    bodyChars += next.length;
    end++;
    if (/<\/html\s*>/i.test(next)) return end;
  }
  return body;
}

/**
 * Decode JSONL once per input, then inspect independent diagnostics and bounded
 * plain HTTP response blocks without restarting the JSONL parser per line.
 * @param {string} output
 * @returns {Generator<{ authentication: boolean, html: boolean, endOffset: number }>}
 */
function* responseErrors(output) {
  const records = [...iterateJsonlEvents(output)];
  const lines = records.map(record => record.rawLine);
  let offset = 0;
  for (let index = 0; index < records.length; index++) {
    const record = records[index];
    offset += record.rawLine.length;
    const diagnostic = record.type === "json" ? nativeResponseDiagnostic(record.value) : undefined;
    if (diagnostic !== undefined) {
      for (const text of responseDiagnosticTexts(diagnostic)) {
        const diagnosticLines = [...iterateLines(text)];
        for (let lineIndex = 0; lineIndex < diagnosticLines.length; lineIndex++) {
          const end = htmlResponseEnd(diagnosticLines, lineIndex, true);
          yield { authentication: AUTHENTICATION_FAILED_PATTERNS.some(pattern => pattern.test(diagnosticLines[lineIndex])), html: end !== null, endOffset: offset };
          if (end !== null) lineIndex = end;
        }
      }
      continue;
    }
    const end = record.type === "invalid" && !record.rawLine.trimStart().startsWith("{") ? htmlResponseEnd(lines, index, false) : null;
    yield {
      authentication: AUTHENTICATION_FAILED_PATTERNS.some(pattern => pattern.test(record.rawLine)),
      html: end !== null,
      endOffset: end === null ? offset : offset + lines.slice(index + 1, end + 1).reduce((sum, line) => sum + line.length, 0),
    };
    if (end !== null) {
      while (index < end) offset += records[++index].rawLine.length;
    }
  }
}

/** @param {string} output @param {{tailChars?: number}} [options] */
function isHTMLResponseError(output, options = {}) {
  const tailStart = options.tailChars === undefined ? 0 : Math.max(0, output.length - options.tailChars);
  for (const error of responseErrors(output)) if (error.html && error.endOffset > tailStart) return true;
  return false;
}

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
  if (typeof output !== "string") return false;
  for (const error of responseErrors(output)) if (error.authentication && !error.html) return true;
  return false;
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
  for (const parsed of iterateJsonl(safeOutput, { lineFilter: line => line.startsWith("{") })) {
    const rejection = parseAICreditsExceededProxyRejectionRecord(parsed);
    if (rejection) return rejection;
  }
  return null;
}

/**
 * Inspect an already decoded native error without reparsing its JSONL record.
 * @param {unknown} parsed
 * @returns {{aiCredits: number, maxAICredits: number}|null}
 */
function parseAICreditsExceededProxyRejectionRecord(parsed) {
  if (!parsed || typeof parsed !== "object") return null;
  // prettier-ignore
  const record = /** @type {Record<string, unknown>} */ (parsed);
  const nativeError = record.error;
  const nativeMessage = nativeError && typeof nativeError === "object" && "message" in nativeError ? nativeError.message : undefined;
  const isCodexApiError = (record.type === "error" && typeof record.message === "string") || (record.type === "turn.failed" && typeof nativeMessage === "string");
  const isClaudeFailedResult = record.type === "result" && record.is_error === true;
  const isClaudeApiError = record.type === "assistant" && record.is_api_error_message === true;
  const isEngineFlaggedApiError = isClaudeApiError || isCodexApiError || isClaudeFailedResult;
  if (!isEngineFlaggedApiError) return null;
  const diagnostic = isCodexApiError
    ? record.type === "error"
      ? record.message
      : nativeMessage
    : isClaudeFailedResult
      ? JSON.stringify({ result: record.result, errors: record.errors })
      : JSON.stringify({ error: record.error, message: record.message });
  const match = AI_CREDITS_EXCEEDED_PROXY_REJECTION_RE.exec(typeof diagnostic === "string" ? diagnostic : "");
  if (!match) return null;
  const aiCredits = Number.parseFloat(match[1]);
  const maxAICredits = Number.parseFloat(match[2]);
  if (!Number.isFinite(aiCredits) || !Number.isFinite(maxAICredits) || maxAICredits <= 0 || aiCredits < maxAICredits) return null;
  return { aiCredits, maxAICredits };
}

/**
 * Build a predicate for a regular expression that classifies harness output.
 *
 * @param {RegExp} pattern
 * @returns {(output: string) => boolean}
 */
function createOutputPatternMatcher(pattern) {
  return output => pattern.test(output);
}

/**
 * Detect retry guard conditions that should stop harness retries immediately.
 * @param {unknown} output
 * @returns {{ aiCreditsExceeded: boolean, awfAPIProxyBlockingRequests: boolean, goalAlreadyActive: boolean, maxRunsExceeded: boolean, modelEndpointMismatch: { model?: string, endpoint?: string, detail: string } | null, apiProxyGuardRejection: { guard: string, counters: Record<string, string|number> } | null }}
 */
function detectNonRetryableHarnessGuard(output) {
  const safeOutput = typeof output === "string" ? output : "";
  return {
    aiCreditsExceeded: AI_CREDITS_EXCEEDED_PATTERNS.some(pattern => pattern.test(safeOutput)),
    awfAPIProxyBlockingRequests: AWF_API_PROXY_BLOCKING_REQUESTS_PATTERNS.some(pattern => pattern.test(safeOutput)),
    goalAlreadyActive: GOAL_ALREADY_ACTIVE_PATTERNS.some(pattern => pattern.test(safeOutput)),
    maxRunsExceeded: isMaxRunsExceededError(safeOutput),
    modelEndpointMismatch: parseModelEndpointMismatch(safeOutput),
    apiProxyGuardRejection: parseAPIProxyGuardRejection(safeOutput),
  };
}

module.exports = {
  createOutputPatternMatcher,
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
  isHTMLResponseError,
  MODEL_ENDPOINT_MISMATCH_PATTERN,
  parseModelEndpointMismatch,
  parseAPIProxyGuardRejection,
  selectNativeErrorDiagnostic,
  shouldSuppressGuardCategory,
  parseAICreditsExceededProxyRejection,
  parseAICreditsExceededProxyRejectionRecord,
};
