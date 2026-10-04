// @ts-check

const { detectErrors, buildOutputLines, isCAPIServerError } = require("./agent_error_patterns.cjs");
const { detectNonRetryableHarnessGuard, isAuthenticationFailedError } = require("./harness_error_patterns.cjs");
const { crashSignalNameForExitCode } = require("./harness_crash_signals.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/** @typedef {import("./types/agent_session").AgentExecutionData} AgentExecutionData */
/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */

/** @param {unknown} value @returns {number} */
function validateAgentExitCode(value) {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0 || value > 255) throw new Error(`${ERR_VALIDATION}: Invalid agent.execution exitCode`);
  return value;
}

/** @param {any} data */
function validateAgentExecution(data) {
  if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error(`${ERR_VALIDATION}: Invalid agent.execution data`);
  for (const key of ["categories", "errorTypes", "errorCodes"]) {
    if (!Array.isArray(data[key]) || data[key].some(value => !(typeof value === "string" && value.length > 0) && !(key === "errorCodes" && Number.isSafeInteger(value)))) {
      throw new Error(`${ERR_VALIDATION}: Invalid agent.execution ${key}`);
    }
    if (new Set(data[key]).size !== data[key].length) throw new Error(`${ERR_VALIDATION}: Duplicate agent.execution ${key}`);
  }
  if (Object.hasOwn(data, "exitCode")) validateAgentExitCode(data.exitCode);
}

/** @param {string} value @returns {number} */
function parseAgentExitCode(value) {
  const trimmed = value.trim();
  if (!/^\d{1,3}$/.test(trimmed) || Number(trimmed) > 255) throw new Error(`${ERR_VALIDATION}: Invalid agent execution exit code`);
  return Number(trimmed);
}

/** @param {SessionEvent} event @returns {event is import("./types/agent_session").AgentExecutionEvent} */
function isAgentExecutionEvent(event) {
  if (event.type !== "agent.execution") return false;
  validateAgentExecution(event.data);
  return true;
}

/**
 * Observe only engine errors, never tool failures or quoted conversation text.
 * @param {any} record
 * @returns {any[]}
 */
function recordErrors(record) {
  if (!record || typeof record !== "object" || Array.isArray(record)) return [];
  const data = record.data ?? record;
  if (record.type === "error" && ["warning", "info"].includes(data.severity)) return [];
  if (["session.error", "claude.assistant_error", "claude.api_retry", "error", "turn.failed"].includes(record.type)) return [data];
  if (record.type === "session.result") return [...(Array.isArray(data.errors) ? data.errors : []), ...(data.is_error === true || data.status === "failed" ? [data] : [])];
  if (record.type === "assistant" && record.is_api_error_message === true) return [record];
  if (record.type === "system" && record.subtype === "api_retry") return [record];
  if (record.type === "stream_event" && record.event?.type === "error") return [record.event];
  if (record.type === "result" && record.is_error === true) return [record];
  if (["turn_end", "message_end"].includes(record.type) && record.message?.errorMessage) return [record.message];
  if (record.type === "result" && data.error) return [data.error];
  return [];
}

/** @param {string} line @returns {boolean} */
function isAgentDiagnosticLine(line) {
  if (/^\[(?:copilot|claude|codex)-harness\]/.test(line)) return !/\b(?:outputTail=|spawning:|resolved --prompt-file:)/.test(line);
  if (/^\[(?:copilot-sdk-driver|sdk-driver)\]\s+(?:\[sdk-driver\]\s+)?(?:error|unhandled error):/i.test(line)) return true;
  if (/^\[gh-aw\/pi-provider\]\s+\S+\s+provider_error\b/.test(line)) return true;
  if (/^(?:\[ERROR\]\s*|\d{4}-\d{2}-\d{2}T\S+\s+ERROR\s+)/.test(line)) return true;
  const message = line.replace(/^(?:Error|API Error|error):\s*/i, "");
  return /^(?:Access denied by policy settings\b|invalid access to inference\b|(?:!\s*\d+\s+)?MCP servers were blocked by policy:|Timeout after \d+ms waiting for session\.idle\b|The requested model is not supported\b|(?:invalid|unknown) model\b|model(?:\s+name)?\s+['"`]?[a-z0-9._:/@-]+['"`]?\s+(?:is\s+)?(?:not found|does not exist|not supported|not available|unavailable)\b|No model available\b|Response status code does not indicate success:|[45]\d{2}\s|CAPIError:|Failed to get response from the AI model;|Maximum (?:LLM invocations|consecutive cache misses|effective tokens|AI credits) exceeded\b|Model\s+"[^"]+"\s+has no AI credits pricing\b|Authentication failed\b|not logged in\b|(?:max_runs_exceeded|max_ai_credits_exceeded|ai_credits_rate_limit_error|max_cache_misses_exceeded|effective_tokens_limit_exceeded|permission_denied_limit_exceeded|model_policy_violation)(?:=true)?$|AWF API proxy blocking requests\b|This thread already has a goal\b|cannot create a new goal because this thread has an unfinished goal\b|could enable arbitrary code execution\b|Command rejected:)/i.test(
    message
  );
}

/**
 * Plain transcript blocks are not diagnostics, even if their text starts with
 * an error signature. A source-prefixed runtime line restores attribution.
 * @param {string} content
 * @returns {{diagnostics: string[], errors: any[]}}
 */
function collectAgentErrorEvidence(content) {
  const diagnostics = [];
  const errors = [];
  try {
    const document = JSON.parse(content);
    for (const value of Array.isArray(document) ? document : [document]) errors.push(...recordErrors(value));
    return { diagnostics, errors };
  } catch {
    // Mixed stdio is parsed as individual records and attributed diagnostic lines.
  }
  let transcriptBlock = false;
  let fencedBlock = false;
  for (const raw of content.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    if (/^```/.test(line)) {
      fencedBlock = !fencedBlock;
      continue;
    }
    if (fencedBlock) continue;
    if (/^(?:assistant|user|thinking|tool(?: output)?|exec)(?::|\s*$)/i.test(line)) {
      transcriptBlock = true;
      continue;
    }
    let record;
    try {
      record = JSON.parse(line);
    } catch {
      const runtimeLine = /^\[(?:(?:copilot|claude|codex)-harness|copilot-sdk-driver|sdk-driver|gh-aw\/pi-provider|ERROR)\]/.test(line) || /^\d{4}-\d{2}-\d{2}T\S+\s+ERROR\s+/.test(line);
      if (runtimeLine) transcriptBlock = false;
      if (!transcriptBlock && isAgentDiagnosticLine(line)) {
        diagnostics.push(line);
        const providerError = line.match(/^\[gh-aw\/pi-provider\].*\bprovider_error\b.*\berror=("(?:[^"\\\u0000-\u001f]|\\["\\/bfnrt]|\\u[0-9a-fA-F]{4})*")$/);
        if (providerError) errors.push({ errorMessage: JSON.parse(providerError[1]) });
      }
      continue;
    }
    if (!transcriptBlock) {
      for (const value of Array.isArray(record) ? record : [record]) errors.push(...recordErrors(value));
    }
  }
  return { diagnostics, errors };
}

/** @param {string} content @returns {string} */
function agentErrorDiagnosticText(content) {
  const { diagnostics, errors } = collectAgentErrorEvidence(content);
  return [...diagnostics, ...collectNativeErrorEvidence(errors).diagnostics].join("\n");
}

/**
 * @param {any[]} errors
 */
function collectNativeErrorEvidence(errors) {
  const diagnostics = [];
  const codes = new Set();
  const types = new Set();
  for (const error of errors) {
    diagnostics.push(JSON.stringify(error));
    const queue = [error];
    for (let visited = 0; visited < 16 && queue.length; visited++) {
      const value = queue.shift();
      if (typeof value === "string") {
        diagnostics.push(value);
        try {
          queue.push(JSON.parse(value));
        } catch {
          // Free-form messages are not error type identifiers.
        }
        continue;
      }
      if (!value || typeof value !== "object" || Array.isArray(value)) continue;
      for (const key of ["code", "error_code", "errorCode", "status", "error_status", "api_error_status"]) {
        const code = value[key];
        if (key === "status" && typeof code === "string" && !/^\d{3}$/.test(code)) continue;
        if ((typeof code === "string" && code.length > 0) || Number.isSafeInteger(code)) codes.add(code);
      }
      for (const key of ["error_type", "errorType", "terminal_reason"]) {
        if (typeof value[key] === "string" && value[key]) types.add(value[key]);
      }
      if (value.type === "result" && value.is_error === true && typeof value.subtype === "string" && value.subtype !== "success") types.add(value.subtype);
      if (typeof value.type === "string" && !["assistant", "system", "result", "message", "error", "turn.failed", "session.error"].includes(value.type)) types.add(value.type);
      if (typeof value.error === "string" && /^[a-z][a-z0-9_]*$/i.test(value.error)) types.add(value.error);
      for (const key of ["error", "message", "errorMessage"]) {
        if (value[key] !== undefined) queue.push(value[key]);
      }
      if (value.metadata?.raw !== undefined) queue.push(value.metadata.raw);
    }
  }
  return { diagnostics, codes, types };
}

/**
 * One observation summarizes all attempts; it is not a claim of final failure.
 * Persisted detector categories are authoritative, including timeout-only evidence.
 * @param {{content?: string, events?: SessionEvent[], categories?: string[], exitCode?: number, observations?: AgentExecutionData[]}} [options]
 * @returns {import("./types/agent_session").AgentExecutionEvent | undefined}
 */
function collectAgentExecution({ content = "", events = [], categories = [], exitCode, observations = [] } = {}) {
  const categorySet = new Set(categories);
  const { diagnostics, errors: rawErrors } = collectAgentErrorEvidence(content);
  const errors = [...events.flatMap(recordErrors), ...rawErrors];
  const { diagnostics: nativeDiagnostics, codes, types } = collectNativeErrorEvidence(errors);
  let observedExit = exitCode;
  for (const data of observations) {
    validateAgentExecution(data);
    for (const category of data.categories) categorySet.add(category);
    for (const code of data.errorCodes) codes.add(code);
    for (const type of data.errorTypes) types.add(type);
    if (observedExit === undefined) observedExit = data.exitCode;
  }
  for (const line of diagnostics) {
    const harness = line.match(/^\[(?:copilot|claude|codex)-harness\].*?\bfailure_reason=([a-z][a-z0-9_]*)/);
    if (harness) categorySet.add(harness[1]);
    const terminal = line.match(/^\[(?:copilot|claude|codex)-harness\].*?\bdone: exitCode=(\d+)\b/);
    if (terminal && exitCode === undefined && !observations.some(data => data.exitCode !== undefined)) observedExit = Number(terminal[1]);
  }
  diagnostics.push(...nativeDiagnostics);
  const text = diagnostics.join("\n");
  for (const diagnostic of diagnostics) {
    for (const match of diagnostic.matchAll(/(?:Response status code does not indicate success:\s*|CAPIError:\s*|Last error:\s*|\bAPI Error:\s*)([45]\d{2})\b/g)) codes.add(Number(match[1]));
    const status = diagnostic.match(/^[ \t]*([45]\d{2}) (?:Bad Request|Too Many Requests|Forbidden|Unauthorized|Internal Server Error|Service Unavailable)\b/);
    if (status) codes.add(Number(status[1]));
  }
  for (const line of buildOutputLines(detectErrors(text))) {
    if (line.endsWith("=true")) categorySet.add(line.slice(0, -5));
  }
  if (isCAPIServerError(text)) categorySet.add("capi_server_error");
  if (isAuthenticationFailedError(text)) categorySet.add("authentication_failed");
  const guards = detectNonRetryableHarnessGuard(text);
  if (guards.aiCreditsExceeded) categorySet.add("max_ai_credits_exceeded");
  if (guards.awfAPIProxyBlockingRequests) categorySet.add("awf_api_proxy_blocking_requests");
  if (guards.goalAlreadyActive) categorySet.add("goal_already_active");
  if (guards.apiProxyGuardRejection) categorySet.add(guards.apiProxyGuardRejection.guard);
  const crashSignal = observedExit === undefined ? null : crashSignalNameForExitCode(observedExit);
  if (crashSignal) {
    categorySet.add("sandbox_runtime_crash");
    types.add(crashSignal);
  }
  if (categorySet.has("invocation_cap_exceeded")) categorySet.delete("capi_quota_exceeded_error");
  if (!categorySet.size && !codes.size && !types.size && observedExit === undefined && !errors.length) return undefined;
  const data = {
    categories: [...categorySet].sort(),
    errorCodes: [...codes].sort((a, b) => {
      const left = String(a);
      const right = String(b);
      if (left !== right) return left < right ? -1 : 1;
      return typeof a === typeof b ? 0 : typeof a === "number" ? -1 : 1;
    }),
    errorTypes: [...types].sort(),
    ...(observedExit !== undefined ? { exitCode: observedExit } : {}),
  };
  validateAgentExecution(data);
  return { type: "agent.execution", data };
}

module.exports = { collectAgentExecution, agentErrorDiagnosticText, validateAgentExecution, validateAgentExitCode, parseAgentExitCode, isAgentExecutionEvent };
