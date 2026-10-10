// @ts-check

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection, AWF_INFRA_LINE_RE } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, normalizeAgentSession, normalizeSessionUsage, isTokenCount, isMetric } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

const main = createEngineLogParser({ parserName: "Cursor", parseFunction: parseCursorLog });
const isObject = value => value !== null && typeof value === "object" && !Array.isArray(value);
const FAILURE_RE = /^\[cursor-harness\].*(?:failed with exit code|checksum did not match|executable was not found|is required|Unsupported Cursor)|^Error: \S/;
const VERSION_RE = /^\d{4}\.\d{2}\.\d{2}-[a-f0-9]+\r?\n?$/;

/** @param {any} record @returns {boolean} */
function isCursorRecord(record) {
  if (!isObject(record)) return false;
  return (
    isSessionEvent(record) ||
    (record.type === "system" && record.subtype === "init") ||
    (["assistant", "user"].includes(record.type) && (typeof record.message?.content === "string" || Array.isArray(record.message?.content))) ||
    (record.type === "tool_call" && ["started", "completed"].includes(record.subtype) && isObject(record.tool_call)) ||
    (record.type === "result" && ["result", "usage", "num_turns", "errors", "duration_ms", "total_cost_usd", "permission_denials", "is_error"].some(key => Object.hasOwn(record, key))) ||
    (record.type === "error" && ["error", "message"].some(key => Object.hasOwn(record, key))) ||
    (["thinking", "reasoning"].includes(record.type) && typeof (record.text ?? record.thinking) === "string")
  );
}

/** @param {string} content @returns {Array<{raw?: any, text?: string, sourceText?: string}>} */
function cursorObservations(content) {
  try {
    const array = JSON.parse(content);
    if (Array.isArray(array)) return array.map(raw => ({ raw }));
    if (isObject(array)) return [{ raw: array }];
    return [];
  } catch {
    // Recover supported records independently from mixed stdout or truncated JSONL.
  }
  return (content.match(/[^\n]*\n|[^\n]+$/g) ?? []).map(text => {
    try {
      const raw = JSON.parse(text);
      return { raw, sourceText: text };
    } catch {
      return { text };
    }
  });
}

/** @param {any} raw @param {any} tool @returns {Record<string, any>} */
function cursorToolOutcome(raw, tool) {
  const result = tool.result;
  const data = {};
  if (Object.hasOwn(tool, "result")) data.output = result;
  if (Object.hasOwn(raw, "duration_ms") && isMetric(raw.duration_ms)) data.durationMs = raw.duration_ms;
  const exitCode = raw.exit_code ?? result?.exitCode ?? result?.success?.exitCode;
  if (typeof exitCode === "number" && Number.isInteger(exitCode)) data.exitCode = exitCode;
  if (Object.hasOwn(raw, "is_error")) data.is_error = raw.is_error;
  if (Object.hasOwn(raw, "status")) data.status = raw.status;
  if (Object.hasOwn(result ?? {}, "error")) data.error = result.error;
  if (Object.hasOwn(raw, "error")) data.error = raw.error;
  const failed =
    raw.is_error === true ||
    raw.isError === true ||
    raw.success === false ||
    ["error", "failed", "failure"].includes(raw.status) ||
    (data.exitCode !== undefined && data.exitCode !== 0) ||
    (data.error != null && data.error !== false) ||
    result?.is_error === true ||
    result?.isError === true ||
    result?.success === false;
  if (failed) data.success = false;
  else if (typeof raw.success === "boolean") data.success = raw.success;
  else if (typeof raw.is_error === "boolean") data.success = !raw.is_error;
  else if (Object.hasOwn(result ?? {}, "success") && result.success !== null) data.success = true;
  return data;
}

/** @param {any} raw @returns {Record<string, any>} */
function cursorResultData(raw) {
  const data = { sourceEngine: "cursor", sourceType: raw.type };
  if (raw.type === "error" || raw.is_error === true || ["error", "failed", "failure"].includes(raw.subtype)) {
    data.status = "error";
    data.errors = raw.errors ?? [Object.hasOwn(raw, "error") ? raw.error : Object.hasOwn(raw, "message") ? raw.message : raw];
  } else if (raw.subtype === "success" || raw.is_error === false) data.status = "completed";
  if (Object.hasOwn(raw, "errors")) data.errors = raw.errors;
  if (Object.hasOwn(raw, "permission_denials")) data.permissionDenials = raw.permission_denials;
  if (isMetric(raw.duration_ms)) data.durationMs = raw.duration_ms;
  if (isMetric(raw.total_cost_usd)) data.totalCostUsd = raw.total_cost_usd;
  if (isTokenCount(raw.num_turns)) data.numTurns = raw.num_turns;
  const usage = normalizeSessionUsage(raw.usage);
  if (usage) data.usage = usage;
  return data;
}

/**
 * Cursor stream-json has complete assistant segments by default. Partial output
 * adds deltas and duplicate flushes. Terminal text/usage are session snapshots,
 * not additional messages or additive accounting. Older text output is only a
 * final answer; its prose cannot establish tools, initialization, or turn counts.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseCursorLog(content) {
  const observations = cursorObservations(content);
  const native = observations.some(({ raw }) => !isSessionEvent(raw) && isCursorRecord(raw) && !(raw.type === "result" && raw.subtype === undefined));
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const assistantObserved = new Set();
  const segments = new Map();
  const versionIndex = observations.findIndex(({ text }) => text !== undefined && VERSION_RE.test(text));
  let plaintext = "";
  const flushText = () => {
    if (plaintext !== "") logEntries.push({ type: "assistant.message", data: { content: plaintext } });
    plaintext = "";
  };
  for (const [index, { raw, text, sourceText }] of observations.entries()) {
    if (text !== undefined) {
      const line = text.replace(/\r?\n$/, "");
      if (FAILURE_RE.test(line)) {
        flushText();
        logEntries.push({ type: "session.result", data: { sourceEngine: "cursor", status: "error", errors: [line] } });
      } else if (AWF_INFRA_LINE_RE.test(line) || /^\[cursor-harness\]/.test(line) || index <= versionIndex) {
        flushText();
      } else if (!native) {
        plaintext += text;
      }
      continue;
    }
    if (!isCursorRecord(raw)) {
      if (!native && versionIndex >= 0 && index > versionIndex && sourceText !== undefined) plaintext += sourceText;
      continue;
    }
    flushText();
    const source = { ...raw, ...(raw.timestamp === undefined && isMetric(raw.timestamp_ms) ? { timestamp: raw.timestamp_ms } : {}) };
    const scope = JSON.stringify([raw.session_id ?? raw.data?.sessionId]);
    const emit = (type, data) => {
      const event = createSessionEvent(source, type, data);
      logEntries.push(event);
      return event;
    };
    if (isSessionEvent(raw)) {
      logEntries.push(structuredClone(raw));
      if (["assistant.message", "assistant.refusal"].includes(raw.type)) assistantObserved.add(scope);
      segments.delete(scope);
    } else if (raw.type === "system" || raw.type === "user") {
      logEntries.push(...normalizeAgentSession([source], { sourceEngine: "cursor" }));
      segments.delete(scope);
    } else if (raw.type === "assistant") {
      const blocks = typeof raw.message.content === "string" ? [{ type: "text", text: raw.message.content }] : raw.message.content;
      const messageText = blocks
        .filter(block => block?.type === "text" && typeof block.text === "string")
        .map(block => block.text)
        .join("");
      const delta = Object.hasOwn(raw, "timestamp_ms") && !Object.hasOwn(raw, "model_call_id");
      const refusal = getMessageRefusal(raw.message);
      const previous = segments.get(scope);
      if (!refusal && !delta && previous?.partial && messageText === previous.text) {
        const target = previous.events.at(-1);
        if (target) (target.data.nativeSnapshots ??= []).push(structuredClone(raw));
        continue;
      }
      if (!refusal && !delta && previous?.partial && messageText.startsWith(previous.text)) {
        const event = emit("assistant.message", { content: messageText.slice(previous.text.length), nativeSnapshots: [structuredClone(raw)] });
        assistantObserved.add(scope);
        segments.set(scope, { text: messageText, partial: true, events: [event] });
        continue;
      }
      const events = normalizeAgentSession([source], { sourceEngine: "cursor" });
      for (const event of events) {
        if (delta && ["assistant.message", "assistant.refusal"].includes(event.type)) event.data.partial = true;
        logEntries.push(event);
      }
      if (events.some(event => ["assistant.message", "assistant.refusal"].includes(event.type))) assistantObserved.add(scope);
      segments.set(scope, { text: delta && previous?.partial ? previous.text + messageText : messageText, partial: delta && !refusal, events: delta && previous?.partial ? [...previous.events, ...events] : events });
    } else if (raw.type === "thinking" || raw.type === "reasoning") {
      emit("assistant.reasoning", { content: raw.text ?? raw.thinking });
      segments.delete(scope);
    } else if (raw.type === "tool_call") {
      segments.delete(scope);
      for (const [name, tool] of Object.entries(raw.tool_call)) {
        if (!isObject(tool)) continue;
        const data = { ...tool, ...(Object.hasOwn(raw, "call_id") ? { toolCallId: raw.call_id } : {}), toolName: name === "function" ? tool.name : (tool.name ?? name) };
        if (typeof tool.args?.command === "string") data.command = tool.args.command;
        if (typeof tool.args?.serverName === "string") data.mcpServerName = tool.args.serverName;
        if (raw.subtype === "started") {
          if (Object.hasOwn(tool, "args")) data.input = tool.args;
          else if (Object.hasOwn(tool, "arguments")) {
            data.input = tool.arguments;
            if (typeof data.input === "string") {
              try {
                data.input = JSON.parse(data.input);
              } catch {
                // Malformed provider argument text remains exact.
              }
            }
          }
          emit("tool.execution_start", data);
        } else emit("tool.execution_complete", { ...data, ...cursorToolOutcome(raw, tool) });
      }
    } else {
      segments.delete(scope);
      const data = cursorResultData(raw);
      if (typeof raw.result === "string" && !assistantObserved.has(scope) && data.status !== "error") {
        emit("assistant.message", { content: raw.result });
        assistantObserved.add(scope);
      }
      emit("session.result", data);
    }
  }
  flushText();
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Cursor", "No recognizable Cursor session observations. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

module.exports = { main, isCursorRecord, parseCursorLog };
