// @ts-check
"use strict";

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, isTokenCount, isMetric } = require("./agent_session.cjs");

const main = createEngineLogParser({ parserName: "Agy", parseFunction: parseAgyLog, supportsDirectories: false });

/** @param {any} event @returns {boolean} */
function isAgyEvent(event) {
  return !!event && typeof event === "object" && !Array.isArray(event) && ["init", "step_update", "result"].includes(event.event);
}

/**
 * Native result usage is cumulative within a conversation, not per step or turn.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean}}
 */
function parseAgyLog(content) {
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const messages = new Map();
  const starts = new Set();
  const completions = new Set();
  const results = new Map();
  let conversation = "agy";
  let initialized = false;
  let maxTurnsHit = false;
  for (const [index, line] of content.split(/\r?\n/).entries()) {
    if (!line.trim().startsWith("{")) continue;
    let raw;
    try {
      raw = JSON.parse(line);
    } catch {
      logEntries.push({ type: "session.collection_warning", data: { code: "malformed_jsonl", line: index + 1 } });
      continue;
    }
    if (isSessionEvent(raw)) {
      logEntries.push(structuredClone(raw));
      if (raw.type === "session.error" && /max.?turns|turn limit|maximum.*turns/i.test(String(raw.data?.error ?? ""))) maxTurnsHit = true;
      continue;
    }
    if (!raw?.event && ["ERROR", "CANCELED", "INTERRUPTED", "INVALID"].includes(raw?.status)) raw = { event: "result", result: raw };
    if (!isAgyEvent(raw)) continue;
    const body = raw[raw.event];
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      logEntries.push({ type: "session.collection_warning", data: { code: "malformed_agy_event", line: index + 1 } });
      continue;
    }
    if (typeof (raw.conversation_id || body.conversation_id) === "string") conversation = raw.conversation_id || body.conversation_id;
    const source = { ...raw, id: `${conversation}:${raw.event}:${body.step_index ?? body.num_turns ?? index}` };
    const emit = (type, data) => logEntries.push(createSessionEvent(source, type, data));
    if (raw.event === "init") {
      emit("session.init", { sourceEngine: "agy", ...(typeof body.model === "string" ? { model: body.model } : {}) });
      initialized = true;
    } else if (raw.event === "step_update") {
      if (!initialized) {
        emit("session.init", { sourceEngine: "agy" });
        initialized = true;
      }
      const id = `${conversation}:${body.step_index}`;
      if (body.step_type === "agent_response" && typeof body.text_delta === "string") {
        const previous = messages.get(id);
        if (previous) previous.data.content += body.text_delta;
        else {
          const message = createSessionEvent(source, "assistant.message", { content: body.text_delta });
          messages.set(id, message);
          logEntries.push(message);
        }
      } else if (body.step_type === "tool") {
        const tool = body.tool_info;
        const name = tool?.name || body.tool_name;
        if (typeof name !== "string" || !Number.isSafeInteger(body.step_index) || body.step_index < 0) {
          emit("session.collection_warning", { code: "malformed_agy_tool" });
          continue;
        }
        if (!starts.has(id)) {
          emit("tool.execution_start", { toolCallId: id, toolName: name, input: tool?.parameters ?? {} });
          starts.add(id);
        }
        if (body.state === "DONE" && !completions.has(id)) {
          if (!tool || (!Object.hasOwn(tool, "output") && !tool.error)) {
            emit("session.collection_warning", { code: "malformed_agy_tool_result", toolCallId: id });
            continue;
          }
          emit("tool.execution_complete", { toolCallId: id, toolName: name, success: !tool?.error, ...(tool?.output !== undefined ? { output: tool.output } : {}), ...(tool?.error ? { error: tool.error } : {}) });
          completions.add(id);
        }
      }
    } else {
      const previous = results.get(conversation)?.data;
      const usage = { ...previous?.usage };
      for (const [native, canonical] of [
        ["input_tokens", "input_tokens"],
        ["output_tokens", "output_tokens"],
        ["thinking_tokens", "reasoning_output_tokens"],
        ["cache_read_tokens", "cache_read_input_tokens"],
        ["total_tokens", "total_tokens"],
      ]) {
        if (isTokenCount(body.usage?.[native])) usage[canonical] = body.usage[native];
        else if (body.usage?.[native] != null) emit("session.collection_warning", { code: "invalid_usage", field: native });
      }
      if (Object.keys(usage).length) usage.input_tokens_include_cache = true;
      const completed = body.status === "SUCCESS";
      const errors = typeof body.error === "string" ? [body.error] : [];
      if (!completed) emit("session.error", { error: body.error || `Agy status: ${body.status || "missing"}` });
      if (typeof body.error === "string" && /max.?turns|turn limit|maximum.*turns/i.test(body.error)) maxTurnsHit = true;
      if (typeof body.response === "string" && body.response && ![...messages.keys()].some(key => key.startsWith(`${conversation}:`))) {
        emit("assistant.message", { content: body.response });
      }
      const data = {
        status: completed ? "completed" : ["CANCELED", "INTERRUPTED"].includes(body.status) ? "interrupted" : "error",
        nativeStatus: body.status,
        ...(isTokenCount(body.num_turns) ? { numTurns: body.num_turns } : previous?.numTurns !== undefined ? { numTurns: previous.numTurns } : {}),
        ...(isMetric(body.duration_seconds) ? { durationMs: body.duration_seconds * 1000 } : previous?.durationMs !== undefined ? { durationMs: previous.durationMs } : {}),
        ...(Object.keys(usage).length ? { usage } : {}),
        ...(errors.length ? { errors } : {}),
      };
      results.set(conversation, createSessionEvent(source, "session.result", data));
    }
  }
  logEntries.push(...results.values());
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Agy", "Log format not recognized as Agy stream JSON. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit,
  };
}

module.exports = { main, parseAgyLog, isAgyEvent };
