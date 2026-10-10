// @ts-check
"use strict";

const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isSessionEvent, isTokenCount, isMetric } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

const main = createEngineLogParser({ parserName: "Agy", parseFunction: parseAgyLog, supportsDirectories: false });

/** @param {any} event @returns {boolean} */
function isAgyEvent(event) {
  return !!event && typeof event === "object" && !Array.isArray(event) && ["init", "step_update", "result", "user"].includes(event.event);
}

/** @param {any} value @returns {boolean} */
const isObject = value => !!value && typeof value === "object" && !Array.isArray(value);

/**
 * Agy v1.3.1 reports uncached input separately from cache reads.
 * @param {any} native
 * @param {(type: any, data: any) => any} emit
 * @param {any} [previous]
 */
function agyUsage(native, emit, previous) {
  if (!isObject(native)) return previous;
  const usage = { ...previous, ...native };
  for (const [field, canonical] of [
    ["input_tokens", "input_tokens"],
    ["output_tokens", "output_tokens"],
    ["thinking_tokens", "reasoning_output_tokens"],
    ["cache_read_tokens", "cache_read_input_tokens"],
    ["total_tokens", "total_tokens"],
  ]) {
    delete usage[field];
    if (isTokenCount(native[field])) usage[canonical] = native[field];
    else {
      if (previous?.[canonical] !== undefined) usage[canonical] = previous[canonical];
      if (native[field] != null) emit("session.collection_warning", { code: "invalid_usage", field });
    }
  }
  if (!Object.hasOwn(native, "input_tokens_include_cache") && ["input_tokens", "output_tokens", "reasoning_output_tokens", "cache_read_input_tokens", "total_tokens"].some(field => isTokenCount(usage[field])))
    usage.input_tokens_include_cache = false;
  return usage;
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
  const assistantText = new Map();
  const refusalText = new Map();
  const starts = new Map();
  const completions = new Map();
  const reports = new Map();
  const results = new Map();
  const nativeRecords = new WeakMap();
  let lastMessageKey;
  let conversation;
  let anonymousSession = 0;
  let maxTurnsHit = false;
  for (const [index, line] of content.split(/\r?\n/).entries()) {
    if (!line.trim().startsWith("{")) continue;
    const contiguousMessageKey = lastMessageKey;
    lastMessageKey = undefined;
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
    if (!isObject(raw)) continue;
    if (!raw.event && typeof raw.status === "string") raw = { ...raw, event: "result", result: raw };
    if (typeof raw.event !== "string") continue;
    const nativeBody = raw.event === "user" ? raw.message : raw[raw.event];
    const body = isObject(nativeBody) ? nativeBody : !isAgyEvent(raw) ? raw : nativeBody;
    if (!isObject(body)) {
      logEntries.push({ type: "session.collection_warning", data: { code: "malformed_agy_event", line: index + 1 } });
      continue;
    }
    const nativeConversation = Object.hasOwn(raw, "conversation_id") ? raw.conversation_id : body.conversation_id;
    if (typeof nativeConversation === "string" || nativeConversation === null) conversation = nativeConversation;
    else if (raw.event === "init") conversation = undefined;
    if (raw.event === "init" && typeof conversation !== "string") anonymousSession++;
    const scope = JSON.stringify(typeof conversation === "string" ? ["native", conversation] : ["anonymous", anonymousSession]);
    const source = { ...raw };
    for (const field of ["id", "parentId", "timestamp"]) if (!Object.hasOwn(source, field) && Object.hasOwn(body, field)) source[field] = body[field];
    const context = conversation !== undefined ? { sessionId: conversation } : {};
    const emit = (type, data) => {
      const event = createSessionEvent(source, type, { ...body, ...context, ...data });
      nativeRecords.set(event, structuredClone(raw));
      logEntries.push(event);
      return event;
    };
    const replace = (map, key, event) => {
      const previous = map.get(key);
      if (previous) {
        event.data.nativeSnapshots = [...(previous.data.nativeSnapshots ?? []), nativeRecords.get(previous)];
        logEntries.splice(logEntries.indexOf(previous), 1);
      }
      map.set(key, event);
    };
    if (raw.event === "init") {
      emit("session.init", {
        sourceEngine: "agy",
        ...(Object.hasOwn(body, "mcp_servers") ? { mcpServers: body.mcp_servers } : {}),
        ...(Object.hasOwn(body, "model_info") ? { modelInfo: body.model_info } : {}),
        ...(Object.hasOwn(body, "slash_commands") ? { slashCommands: body.slash_commands } : {}),
      });
    } else if (raw.event === "user") {
      emit("user.message", {});
    } else if (raw.event === "step_update") {
      const key = isTokenCount(body.step_index) ? JSON.stringify([scope, body.step_index]) : undefined;
      const usage = isObject(body.usage) ? agyUsage(body.usage, emit, key !== undefined ? reports.get(key)?.data.usage : undefined) : undefined;
      if (usage) {
        const report = emit("usage.report", {
          usage,
          ...(isTokenCount(body.step_index) ? { stepIndex: body.step_index } : {}),
          ...(isMetric(body.duration_seconds) && isMetric(body.duration_seconds * 1000) ? { durationMs: body.duration_seconds * 1000 } : {}),
        });
        if (key !== undefined) replace(reports, key, report);
      }
      if (body.step_type !== "tool" && Object.hasOwn(body, "error")) {
        emit("session.error", {});
      } else if (body.step_type === "user_input") {
        emit("user.message", Object.hasOwn(body, "text_delta") ? { content: body.text_delta } : {});
      } else if (["agent_response", "reasoning", "thinking", "agent_reasoning"].includes(body.step_type)) {
        const reasoning = body.step_type !== "agent_response";
        const refusal = reasoning ? undefined : getMessageRefusal({ ...body, ...(Object.hasOwn(body, "text_delta") ? { content: body.text_delta } : {}) });
        const type = refusal ? "assistant.refusal" : reasoning ? "assistant.reasoning" : "assistant.message";
        const messageKey = key !== undefined ? `${key}:${type}` : undefined;
        const previous = messageKey !== undefined && contiguousMessageKey === messageKey ? messages.get(messageKey) : undefined;
        const content = Object.hasOwn(body, "text_delta") ? body.text_delta : body.content;
        const hasMetadata = ["id", "parentId", "timestamp"].some(field => Object.hasOwn(source, field));
        if (!hasMetadata && previous && (content === undefined || (typeof previous.data.content === "string" && typeof content === "string"))) {
          if (typeof content === "string") previous.data.content += content;
          previous.data.nativeUpdates ??= [nativeRecords.get(previous)];
          previous.data.nativeUpdates.push(structuredClone(raw));
        } else {
          const message = emit(type, { ...(content !== undefined ? { content } : {}), ...refusal, ...(hasMetadata && typeof body.text_delta === "string" ? { delta: true } : {}) });
          if (messageKey !== undefined) messages.set(messageKey, message);
        }
        lastMessageKey = messageKey;
        if (!reasoning && !refusal && typeof content === "string") {
          const text = assistantText.get(scope);
          assistantText.set(scope, { key, content: text?.key === key && key !== undefined ? text.content + content : content });
        } else if (refusal && typeof refusal.content === "string") {
          const text = refusalText.get(scope);
          refusalText.set(scope, { key, content: text?.key === key && key !== undefined ? text.content + refusal.content : refusal.content });
        }
      } else if (body.step_type === "tool") {
        const tool = isObject(body.tool_info) ? body.tool_info : {};
        const callId = Object.hasOwn(tool, "toolCallId") ? tool.toolCallId : Object.hasOwn(tool, "tool_call_id") ? tool.tool_call_id : Object.hasOwn(body, "toolCallId") ? body.toolCallId : body.tool_call_id;
        const toolKey = typeof callId === "string" ? JSON.stringify([scope, "tool", callId]) : key;
        const start = toolKey !== undefined ? starts.get(toolKey)?.data : undefined;
        const name = Object.hasOwn(tool, "name") ? tool.name : (body.tool_name ?? start?.toolName);
        const parameters = Object.hasOwn(tool, "input") ? tool.input : tool.parameters;
        const data = {
          ...tool,
          ...(typeof callId === "string" ? { toolCallId: callId } : {}),
          ...(isTokenCount(body.step_index) ? { stepIndex: body.step_index } : {}),
          ...(typeof name === "string" ? { toolName: name } : {}),
          ...(Object.hasOwn(tool, "input") || Object.hasOwn(tool, "parameters") ? { input: parameters } : {}),
          ...(typeof tool.command === "string" ? { command: tool.command } : typeof parameters?.CommandLine === "string" ? { command: parameters.CommandLine } : {}),
          ...(typeof tool.mcpServerName === "string"
            ? { mcpServerName: tool.mcpServerName }
            : name === "call_mcp_tool" && typeof parameters?.ServerName === "string"
              ? { mcpServerName: parameters.ServerName }
              : typeof start?.mcpServerName === "string"
                ? { mcpServerName: start.mcpServerName }
                : {}),
        };
        if (body.state === "ACTIVE") {
          if (toolKey !== undefined && starts.has(toolKey)) {
            const event = emit("tool.execution_update", { ...start, ...data });
            starts.set(toolKey, event);
          } else {
            const event = emit("tool.execution_start", data);
            if (toolKey !== undefined) starts.set(toolKey, event);
          }
        } else if (body.state === "DONE") {
          const failed = (tool.error !== undefined && tool.error !== null && tool.error !== false && tool.error !== "") || tool.isError === true || tool.is_error === true;
          const event = emit("tool.execution_complete", {
            ...data,
            status: body.state,
            ...(failed ? { success: false } : typeof tool.success === "boolean" ? { success: tool.success } : Object.hasOwn(tool, "output") ? { success: true } : {}),
            ...(isMetric(body.duration_seconds) && isMetric(body.duration_seconds * 1000) ? { durationMs: body.duration_seconds * 1000 } : {}),
          });
          if (toolKey !== undefined) replace(completions, toolKey, event);
        } else {
          emit("agy.step_update", {});
        }
      } else if (body.step_type === "checkpoint") {
        if (!usage) emit("session.checkpoint", {});
      } else {
        emit("agy.step_update", {});
      }
    } else if (raw.event === "result") {
      const previous = results.get(scope)?.data;
      const usage = agyUsage(body.usage, emit, previous?.usage);
      const errors = Object.hasOwn(body, "error") ? [body.error] : body.errors;
      if (Object.hasOwn(body, "error") || ["ERROR", "INVALID", "CANCELED", "INTERRUPTED"].includes(body.status)) emit("session.error", {});
      if (typeof body.error === "string" && /max.?turns|turn limit|maximum.*turns/i.test(body.error)) maxTurnsHit = true;
      const refusal = getMessageRefusal({ ...body, ...(Object.hasOwn(body, "response") ? { content: body.response } : {}) });
      if (refusal && (refusal.content === undefined || refusal.content !== refusalText.get(scope)?.content)) {
        const observed = refusalText.get(scope)?.content;
        emit("assistant.refusal", { ...refusal, ...(typeof refusal.content === "string" && typeof observed === "string" && refusal.content.startsWith(observed) ? { content: refusal.content.slice(observed.length) } : {}) });
        refusalText.set(scope, { content: refusal.content });
      } else if (Object.hasOwn(body, "response") && body.response !== assistantText.get(scope)?.content) {
        const observed = assistantText.get(scope)?.content;
        if (!refusal) emit("assistant.message", { content: typeof body.response === "string" && typeof observed === "string" && body.response.startsWith(observed) ? body.response.slice(observed.length) : body.response });
        assistantText.set(scope, { content: body.response });
      }
      const data = {
        sourceEngine: "agy",
        ...(typeof body.status === "string"
          ? {
              status: body.status === "SUCCESS" ? "completed" : ["CANCELED", "INTERRUPTED"].includes(body.status) ? "interrupted" : ["ERROR", "INVALID"].includes(body.status) ? "error" : body.status,
              nativeStatus: body.status,
              sourceType: body.status,
            }
          : {}),
        ...(isTokenCount(body.num_turns) ? { numTurns: body.num_turns } : previous?.numTurns !== undefined ? { numTurns: previous.numTurns } : {}),
        ...(isMetric(body.duration_seconds) && isMetric(body.duration_seconds * 1000) ? { durationMs: body.duration_seconds * 1000 } : previous?.durationMs !== undefined ? { durationMs: previous.durationMs } : {}),
        ...(usage !== undefined ? { usage } : {}),
        ...(Array.isArray(errors) ? { errors } : {}),
        ...(Object.hasOwn(body, "permission_denials") ? { permissionDenials: body.permission_denials } : {}),
        ...(isMetric(body.total_cost_usd) ? { totalCostUsd: body.total_cost_usd } : previous?.totalCostUsd !== undefined ? { totalCostUsd: previous.totalCostUsd } : {}),
      };
      const terminal = emit("session.result", data);
      delete terminal.data.num_turns;
      delete terminal.data.total_cost_usd;
      if (usage === undefined) delete terminal.data.usage;
      replace(results, scope, terminal);
    } else {
      emit(`agy.${raw.event}`, {});
    }
  }
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Agy", "Log format not recognized as Agy stream JSON. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit,
  };
}

module.exports = { main, parseAgyLog, isAgyEvent };
