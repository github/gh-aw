// @ts-check

const { isDeepStrictEqual } = require("node:util");
const { createSessionEvent, isSessionEvent, normalizeAgentSession, transformFlatSessionEntries, isMetric, isTokenCount } = require("./agent_session.cjs");

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {{text: Map<number, string>, thinking: Map<number, string>, calls: Set<number>}} MessageState */

/** @param {any} message @returns {string|undefined} */
function messageIdentity(message) {
  const id = message?.id ?? message?.responseId;
  if (id !== undefined) return JSON.stringify([message.role, id]);
  if (message?.timestamp !== undefined) return JSON.stringify([message.role, message.timestamp]);
  return undefined;
}

/** @returns {MessageState} */
function newMessageState() {
  return { text: new Map(), thinking: new Map(), calls: new Set() };
}

/** @param {any} result @returns {boolean} */
function isPiResultError(result) {
  return !!(
    result &&
    (result.isError === true ||
      result.is_error === true ||
      result.status === "error" ||
      result.status === "failed" ||
      result.error != null ||
      [result.exitCode, result.details?.exitCode, result.structuredContent?.exitCode].some(code => typeof code === "number" && code !== 0))
  );
}

/**
 * Pi repeats complete messages at message_end, turn_end and agent_end. Text deltas
 * are observations, whereas those envelopes are snapshots of the same message.
 * @param {Array<any>} records
 * @returns {SessionEvent[]}
 */
function transformPiV3Entries(records) {
  records = records.filter(record => record && typeof record === "object" && !Array.isArray(record));
  /** @type {SessionEvent[]} */
  const events = [];
  const executionStarts = new Set(records.filter(r => r.type === "tool_execution_start" && r.toolCallId !== undefined).map(r => r.toolCallId));
  const executionEnds = new Set(records.filter(r => r.type === "tool_execution_end" && r.toolCallId !== undefined).map(r => r.toolCallId));
  const finalizedCalls = new Set();
  for (const raw of records) {
    const messages = raw.type === "agent_end" ? raw.messages : [raw.message];
    for (const message of messages ?? []) {
      for (const part of Array.isArray(message?.content) ? message.content : []) {
        if (part?.type === "toolCall" && part.id !== undefined) finalizedCalls.add(part.id);
      }
    }
    if (raw.assistantMessageEvent?.type === "toolcall_end" && raw.assistantMessageEvent.toolCall?.id !== undefined) finalizedCalls.add(raw.assistantMessageEvent.toolCall.id);
  }
  const calls = new Set();
  const completions = new Set();
  /** @type {Map<string, MessageState>} */
  const messageStates = new Map();
  /** @type {Array<{message: any, state: MessageState}>} */
  const snapshots = [];
  let assistantState = newMessageState();
  let activeState = assistantState;
  let partialAssistant = false;
  /**
   * @template {SessionEvent["type"]} T
   * @param {any} raw
   * @param {T} type
   * @param {import("./types/agent_session").SessionEventData<T>} data
   */
  const emit = (raw, type, data) => events.push(createSessionEvent(raw, type, data));

  const emitText = (raw, state, kind, index, text, delta, role = "assistant", metadata = {}) => {
    if (typeof text !== "string") return;
    const previous = state[kind].get(index);
    const content = delta ? text : previous === undefined ? text : text.startsWith(previous) ? text.slice(previous.length) : undefined;
    if (content === undefined) {
      emit(raw, "pi.message_snapshot", { ...metadata, content: text, contentIndex: index, channel: kind });
      return;
    }
    if (delta || previous === undefined || content !== "") {
      emit(raw, role === "user" ? "user.message" : kind === "thinking" ? "assistant.reasoning" : "assistant.message", { ...metadata, content });
    }
    state[kind].set(index, delta ? (previous ?? "") + text : text);
  };

  const emitCall = (raw, part, state = assistantState, index = raw.assistantMessageEvent?.contentIndex ?? 0) => {
    if (part.id !== undefined && (executionStarts.has(part.id) || calls.has(part.id))) return;
    if (part.id === undefined && state.calls.has(index)) return;
    if (part.id !== undefined) calls.add(part.id);
    state.calls.add(index);
    emit(raw, "tool.execution_start", { ...part, toolCallId: part.id, toolName: part.name, input: part.arguments });
  };

  const emitCompletion = (raw, message) => {
    const id = message.toolCallId;
    if (id !== undefined && (executionEnds.has(id) || completions.has(id))) return;
    if (id !== undefined) completions.add(id);
    emit(raw, "tool.execution_complete", {
      ...message,
      toolCallId: id,
      toolName: message.toolName,
      output: message.content,
      success: message.isError === true || isPiResultError(message) ? false : message.isError === false ? true : undefined,
    });
  };

  const emitMessage = (raw, message, fallback) => {
    if (!message || !["assistant", "user", "toolResult"].includes(message.role)) return;
    if (message.role === "toolResult") {
      const before = events.length;
      emitCompletion(raw, message);
      if (events.length === before && raw.type !== "agent_end") emit(raw, "pi.message_snapshot", { role: message.role, toolCallId: message.toolCallId });
      return;
    }
    const before = events.length;
    const identity = messageIdentity(message) ?? (raw.id !== undefined && raw.type !== "agent_end" ? JSON.stringify(["record", raw.id]) : undefined);
    const state = (identity !== undefined && messageStates.get(identity)) || fallback;
    if (identity !== undefined) messageStates.set(identity, state);
    if (message.role === "assistant") assistantState = state;
    const content = typeof message.content === "string" ? [{ type: "text", text: message.content }] : message.content;
    for (const [index, part] of (Array.isArray(content) ? content : []).entries()) {
      if (part?.type === "text" || part?.type === "thinking") {
        emitText(raw, state, part.type, index, part.type === "text" ? part.text : part.thinking, false, message.role, part);
      } else if (part?.type === "toolCall") emitCall(raw, part, state, index);
    }
    if (!snapshots.some(snapshot => snapshot.state === state && isDeepStrictEqual(snapshot.message, message))) snapshots.push({ message: structuredClone(message), state });
    if (message.role === "assistant" && events.length === before && raw.type !== "agent_end") emit(raw, "pi.message_snapshot", { role: message.role, model: message.model, usage: message.usage });
  };

  for (const raw of records) {
    if (isSessionEvent(raw)) events.push(structuredClone(raw));
    else if (raw.type === "session") emit(raw, "session.init", { sourceEngine: "pi", sessionId: raw.id, cwd: raw.cwd, model: raw.model });
    else if (raw.type === "turn_start") {
      assistantState = activeState = newMessageState();
      partialAssistant = false;
    } else if (raw.type === "message_start") {
      activeState = newMessageState();
      if (raw.message?.role === "assistant") {
        assistantState = activeState;
        partialAssistant = true;
      }
      emitMessage(raw, raw.message, activeState);
    } else if (raw.type === "message_update") {
      const update = raw.assistantMessageEvent;
      const kind = update?.type?.startsWith("thinking_") ? "thinking" : "text";
      if (update?.type === "text_delta" || update?.type === "thinking_delta") {
        partialAssistant = true;
        emitText(raw, assistantState, kind, update.contentIndex ?? 0, update.delta, true);
      } else if (update?.type === "text_end" || update?.type === "thinking_end") {
        emitText(raw, assistantState, kind, update.contentIndex ?? 0, update.content, false);
      } else if (update) {
        emit(raw, "pi.message_update", { ...update, usage: raw.usage });
        if (update.type === "toolcall_end" && update.toolCall) emitCall(raw, update.toolCall);
        if (update.type === "toolcall_start" && !finalizedCalls.has(update.id)) emitCall(raw, { id: update.id, name: update.toolName });
      }
    } else if (raw.type === "message_end") emitMessage(raw, raw.message, raw.message?.role === "assistant" ? assistantState : activeState);
    else if (raw.type === "turn_end") {
      emitMessage(raw, raw.message, assistantState);
      for (const result of raw.toolResults ?? []) emitCompletion(raw, result);
      assistantState = newMessageState();
      partialAssistant = false;
    } else if (raw.type === "tool_execution_start") {
      if (raw.toolCallId !== undefined && calls.has(raw.toolCallId)) continue;
      if (raw.toolCallId !== undefined) calls.add(raw.toolCallId);
      emit(raw, "tool.execution_start", { toolCallId: raw.toolCallId, toolName: raw.toolName, input: raw.args });
    } else if (raw.type === "tool_execution_end") {
      if (raw.toolCallId !== undefined && completions.has(raw.toolCallId)) continue;
      if (raw.toolCallId !== undefined) completions.add(raw.toolCallId);
      emit(raw, "tool.execution_complete", {
        toolCallId: raw.toolCallId,
        toolName: raw.toolName,
        result: raw.result,
        output: raw.result,
        error: raw.error ?? raw.result?.error,
        success: raw.isError === true || raw.error != null || isPiResultError(raw.result) ? false : raw.isError === false ? true : undefined,
        durationMs: isMetric(raw.durationMs) ? raw.durationMs : undefined,
      });
    } else if (raw.type === "tool_execution_update") emit(raw, "pi.tool_execution_update", { toolCallId: raw.toolCallId, toolName: raw.toolName, input: raw.args, partialResult: raw.partialResult });
    else if (raw.type === "agent_end") {
      const consumed = new Set();
      const messages = raw.messages ?? [];
      const lastAssistant = messages.findLastIndex(message => message?.role === "assistant");
      for (const [position, message] of messages.entries()) {
        const index = snapshots.findIndex((snapshot, i) => !consumed.has(i) && isDeepStrictEqual(snapshot.message, message));
        if (index >= 0) consumed.add(index);
        emitMessage(raw, message, index >= 0 ? snapshots[index].state : partialAssistant && position === lastAssistant ? assistantState : newMessageState());
      }
    } else if (raw.type === "error") emit(raw, "pi.error", { error: raw.error ?? raw.message });
    else events.push(...normalizeAgentSession(transformFlatSessionEntries([raw]), { sourceEngine: "pi" }));
  }
  return events;
}

/** @param {any} usage @returns {Record<string, any>} */
function piUsage(usage) {
  return {
    ...usage,
    input_tokens: usage.input ?? usage.input_tokens,
    output_tokens: usage.output ?? usage.output_tokens,
    cache_read_input_tokens: usage.cacheRead ?? usage.cache_read_input_tokens,
    cache_creation_input_tokens: usage.cacheWrite ?? usage.cache_creation_input_tokens,
    total_tokens: usage.totalTokens ?? usage.total_tokens,
  };
}

/**
 * Count finalized assistant messages once, even in interrupted streams that lack
 * turn_end. agent_end.messages is a transcript snapshot, never extra usage.
 * @param {Array<any>} records
 * @returns {any}
 */
function computePiV3Stats(records) {
  records = records.filter(record => record && typeof record === "object" && !Array.isArray(record));
  /** @type {Record<string, any>} */
  const usage = {};
  let turns = 0;
  let reportedTurns;
  let totalCostUsd;
  let durationMs;
  let exposedErrors = false;
  /** @type {Array<any>} */
  const errors = [];
  const seen = new Map();
  /** @type {any} */
  let lastReport;
  let lastWasMessageEnd = false;
  const reports = [];
  const addMessage = (raw, message) => {
    if (!message || message.role !== "assistant") return;
    const identity = messageIdentity(message) ?? (raw.id !== undefined && raw.type !== "agent_end" ? JSON.stringify(["record", raw.id]) : undefined);
    const sourceIdentity = raw.id !== undefined && raw.type !== "agent_end" ? JSON.stringify(["record", raw.id]) : undefined;
    const signature = { ...message, usage: undefined, errorMessage: undefined };
    let report = (sourceIdentity !== undefined && seen.get(sourceIdentity)) || (identity !== undefined && seen.get(identity));
    if (!report && raw.type === "turn_end" && lastWasMessageEnd && isDeepStrictEqual(lastReport?.signature, signature)) report = lastReport;
    if (!report) {
      report = { signature: structuredClone(signature), message: structuredClone(message), usage: {}, cost: undefined, error: undefined };
      reports.push(report);
      turns++;
    }
    if (identity !== undefined) seen.set(identity, report);
    if (sourceIdentity !== undefined) seen.set(sourceIdentity, report);
    lastReport = report;
    lastWasMessageEnd = raw.type === "message_end";
    report.message = structuredClone(message);
    if (message.usage && typeof message.usage === "object") {
      const normalized = piUsage(message.usage);
      for (const key of ["input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "total_tokens"]) {
        if (!isTokenCount(normalized[key])) continue;
        usage[key] = (usage[key] ?? 0) + normalized[key] - (report.usage[key] ?? 0);
        report.usage[key] = normalized[key];
      }
      // CI totalTokens and the driver usage agree that Pi input excludes cache.
      if (isTokenCount(message.usage.input) && [message.usage.cacheRead, message.usage.cacheWrite].some(isTokenCount)) usage.input_tokens_include_cache = false;
      if (isMetric(message.usage.cost?.total)) {
        totalCostUsd = (totalCostUsd ?? 0) + message.usage.cost.total - (report.cost ?? 0);
        report.cost = message.usage.cost.total;
      }
    }
    if (message.errorMessage !== undefined && !isDeepStrictEqual(report.error, message.errorMessage)) {
      errors.push(structuredClone(message.errorMessage));
      report.error = structuredClone(message.errorMessage);
    }
  };
  for (const raw of records) {
    if (raw.type === "turn_start") {
      lastReport = undefined;
      lastWasMessageEnd = false;
    } else if (raw.type === "message_end" || raw.type === "turn_end") {
      addMessage(raw, raw.message);
    } else if (raw.type === "agent_end") {
      const remaining = [...reports];
      for (const message of raw.messages ?? []) {
        const index = remaining.findIndex(report => isDeepStrictEqual(report.message, message));
        if (index >= 0) remaining.splice(index, 1);
        else addMessage(raw, message);
      }
      const snapshot = raw.usage ?? raw.stats?.usage ?? raw.stats;
      if (snapshot && typeof snapshot === "object") {
        const normalized = piUsage(snapshot);
        for (const key of ["input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "total_tokens"]) {
          if (isTokenCount(normalized[key])) usage[key] = normalized[key];
        }
        if (isTokenCount(snapshot.input) && [snapshot.cacheRead, snapshot.cacheWrite].some(isTokenCount)) usage.input_tokens_include_cache = false;
      }
      const cost = raw.total_cost_usd ?? raw.totalCostUsd ?? raw.stats?.total_cost_usd ?? snapshot?.cost?.total;
      if (isMetric(cost)) totalCostUsd = cost;
      const duration = raw.durationMs ?? raw.duration_ms ?? raw.stats?.duration_ms;
      if (isMetric(duration)) durationMs = duration;
      const count = raw.numTurns ?? raw.num_turns ?? raw.stats?.turns;
      if (isTokenCount(count)) reportedTurns = count;
      const diagnostics = raw.errors ?? raw.stats?.errors;
      if (Array.isArray(diagnostics)) {
        exposedErrors = true;
        errors.push(...structuredClone(diagnostics));
      }
    } else if (raw.type === "result" || raw.type === "session.result") {
      const result = normalizeAgentSession(transformFlatSessionEntries([raw]), { sourceEngine: "pi" }).find(event => event.type === "session.result")?.data;
      if (!result) continue;
      if (result.usage) {
        for (const [key, value] of Object.entries(result.usage)) {
          if (isTokenCount(value) || key === "input_tokens_include_cache") usage[key] = value;
        }
      }
      if (isMetric(result.totalCostUsd)) totalCostUsd = result.totalCostUsd;
      if (isMetric(result.durationMs)) durationMs = result.durationMs;
      if (isTokenCount(result.numTurns)) reportedTurns = result.numTurns;
    } else if (raw.type === "error") {
      const error = raw.error ?? raw.message;
      if (error !== undefined) errors.push(structuredClone(error));
    }
  }
  if (turns === 0 && reportedTurns === undefined && Object.keys(usage).length === 0 && errors.length === 0 && !exposedErrors && totalCostUsd === undefined && durationMs === undefined) return null;
  return {
    turns: reportedTurns ?? (turns || undefined),
    input_tokens: usage.input_tokens,
    output_tokens: usage.output_tokens,
    usage: Object.keys(usage).length ? usage : undefined,
    total_cost_usd: totalCostUsd,
    duration_ms: durationMs,
    errors,
    exposedErrors,
  };
}

module.exports = { transformPiV3Entries, computePiV3Stats };
