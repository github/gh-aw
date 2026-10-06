// @ts-check

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {import("./types/agent_session").SessionUsage} SessionUsage */
/** @typedef {import("./types/agent_session").SessionResultData} SessionResultData */

const { normalizeAgentSession, createSessionEvent, accumulateSessionUsage, isTokenCount, isMetric } = require("./agent_session.cjs");
const { isDeepStrictEqual } = require("node:util");

const COPILOT_CONVERSATION_EVENT_TYPES = new Set(["assistant.message", "assistant.message_delta", "user.message", "tool.execution_start", "tool.execution_complete"]);

/**
 * @param {Array<any>} events
 * @returns {boolean}
 */
function hasCopilotConversation(events) {
  return events.some(event => COPILOT_CONVERSATION_EVENT_TYPES.has(event?.type));
}

/**
 * @param {string} content
 * @returns {boolean}
 */
function hasMalformedJsonl(content) {
  return content.split(/\r?\n/).some(line => {
    if (!line.trim()) return false;
    try {
      JSON.parse(line);
      return false;
    } catch {
      return true;
    }
  });
}

/**
 * Copilot persists lifecycle events but emits assistant.usage only on the live
 * transport. Keep those observations and project their accounting separately.
 * @param {Array<any>} entries
 * @returns {SessionEvent[]}
 */
function normalizeCopilotSession(entries) {
  const source = normalizeAgentSession(entries, { sourceEngine: "copilot" });
  /** @type {SessionEvent[]} */
  const events = [];
  /** @type {SessionUsage} */
  const usage = {};
  const responses = new Set();
  const turns = new Set();
  const tools = new Map();
  const projections = new Map();
  const projectionKey = (label, event) => JSON.stringify([label, event.type, event.id, event.timestamp]);
  const projectionData = data => Object.fromEntries(Object.entries(data).filter(([, value]) => value !== undefined));
  const projectionEvidence = event => projectionData({ ...event, data: projectionData(event.data) });
  for (const event of source) {
    if (!event.copilotProjection) continue;
    const key = projectionKey(event.copilotProjection, event);
    const bucket = projections.get(key) ?? [];
    bucket.push({ evidence: projectionEvidence(event), consumed: false });
    projections.set(key, bucket);
  }
  const deltaText = new Map();
  for (const event of source) {
    if (event.type === "assistant.message_delta" && event.data.messageId !== undefined && typeof event.data.deltaContent === "string") {
      deltaText.set(event.data.messageId, (deltaText.get(event.data.messageId) ?? "") + event.data.deltaContent);
    }
  }
  const snapshots = new Set(
    source
      .filter(event => event.type === "assistant.message" && !event.copilotProjection && typeof event.data.content === "string" && deltaText.has(event.data.messageId) && event.data.content.startsWith(deltaText.get(event.data.messageId)))
      .map(event => event.data.messageId)
      .filter(id => id !== undefined)
  );
  let sessionId;
  let finalizedTurns = 0;
  let hasTurnResult = false;

  for (let index = 0; index < source.length; index++) {
    const event = source[index];
    /** @type {Record<string, any>} */
    const data = event.data;
    events.push(event);
    /**
     * @template {SessionEvent["type"]} T
     * @param {T} type
     * @param {import("./types/agent_session").SessionEventData<T>} fields
     * @param {SessionEvent["type"]} [label]
     */
    const project = (type, fields, label = event.type) => {
      const projected = { ...createSessionEvent(event, type, fields), copilotProjection: label };
      const key = projectionKey(label, projected);
      const bucket = projections.get(key) ?? [];
      const identified = event.id !== undefined;
      const comparable = projectionEvidence(projected);
      const previous = bucket.find(item => (identified || !item.consumed) && isDeepStrictEqual(item.evidence, comparable));
      if (previous) {
        previous.consumed = true;
        return;
      }
      events.push(projected);
      if (identified) {
        bucket.push({ evidence: comparable, consumed: true });
        projections.set(key, bucket);
      }
    };

    if (event.type === "session.start") {
      sessionId = data.sessionId;
      if (!Object.hasOwn(data, "model") && Object.hasOwn(data, "selectedModel")) data.model = data.selectedModel;
      if (!Object.hasOwn(data, "cwd") && data.context && Object.hasOwn(data.context, "cwd")) data.cwd = data.context.cwd;
      project("session.init", {
        sourceEngine: "copilot",
        model: data.model,
        cwd: data.cwd,
      });
    } else if (event.type === "tool.execution_start") {
      if (!Object.hasOwn(data, "input") && !Object.hasOwn(data, "parameters") && Object.hasOwn(data, "arguments")) {
        event.data.input = structuredClone(data.arguments);
      }
      if (data.toolCallId !== undefined) tools.set(data.toolCallId, data);
    } else if (event.type === "tool.execution_complete") {
      const start = tools.get(data.toolCallId);
      if (!Object.hasOwn(data, "toolName") && start?.toolName !== undefined) data.toolName = start.toolName;
      if (!Object.hasOwn(data, "exitCode") && Number.isSafeInteger(data.shellExecution?.exitCode)) data.exitCode = data.shellExecution.exitCode;
    } else if ((event.type === "assistant.message" || event.type === "assistant.refusal") && typeof data.reasoningText === "string") {
      project("assistant.reasoning", { content: data.reasoningText });
    } else if (event.type === "assistant.message_delta" && typeof data.deltaContent === "string" && !snapshots.has(data.messageId)) {
      project("assistant.message", { content: data.deltaContent });
    } else if (event.type === "assistant.turn_end") {
      const identity = data.turnId !== undefined ? JSON.stringify([sessionId, event.agentId, data.turnId]) : event.id;
      if (identity === undefined || !turns.has(identity)) {
        finalizedTurns++;
        if (identity !== undefined) turns.add(identity);
      }
    } else if (event.type === "assistant.usage") {
      const identity = data.apiCallId ?? event.id;
      if (identity === undefined || !responses.has(identity)) {
        if (identity !== undefined) responses.add(identity);
        accumulateSessionUsage(usage, copilotUsage(data));
      }
      if (Object.keys(usage).length) project("session.result", { usage: { ...usage } });
    } else if (event.type === "session.shutdown") {
      /** @type {SessionUsage} */
      const snapshot = {};
      for (const metric of Object.values(data.modelMetrics ?? {})) {
        accumulateSessionUsage(snapshot, copilotUsage(metric?.usage));
      }
      // tokenDetails exposes actual cache writes even when the older model
      // usage.cacheWriteTokens field reports zero.
      const details = data.tokenDetails;
      for (const [nativeKey, key] of [
        ["cache_read", "cache_read_input_tokens"],
        ["cache_write", "cache_creation_input_tokens"],
      ]) {
        if (isTokenCount(details?.[nativeKey]?.tokenCount)) snapshot[key] = details[nativeKey].tokenCount;
      }
      if (!Object.hasOwn(snapshot, "input_tokens") && isTokenCount(details?.input?.tokenCount)) {
        snapshot.input_tokens = details.input.tokenCount;
        snapshot.input_tokens_include_cache = false;
      }
      if (!Object.hasOwn(snapshot, "output_tokens") && isTokenCount(details?.output?.tokenCount)) snapshot.output_tokens = details.output.tokenCount;
      /** @type {SessionResultData} */
      const result = {};
      if (Object.keys(snapshot).length) result.usage = snapshot;
      const end = typeof event.timestamp === "string" ? Date.parse(event.timestamp) : event.timestamp;
      if (typeof end === "number" && isMetric(data.sessionStartTime) && isMetric(end) && end >= data.sessionStartTime) result.durationMs = end - data.sessionStartTime;
      if (data.errorReason !== undefined) result.errors = [data.errorReason];
      if (Object.keys(result).length) project("session.result", result);
    } else if (event.type === "session.error") {
      project("session.result", { errors: [data.error !== undefined ? structuredClone(data.error) : structuredClone(data)] });
    }
    if (event.type === "session.result" && isTokenCount(data.numTurns)) hasTurnResult = true;
  }

  if (finalizedTurns && !hasTurnResult) {
    events.push({ type: "session.result", data: { numTurns: finalizedTurns }, copilotProjection: "finalized-turns" });
  }
  return events;
}

/** @param {any} data @returns {SessionUsage} */
function copilotUsage(data) {
  /** @type {SessionUsage} */
  const usage = {};
  for (const [nativeKey, key] of [
    ["inputTokens", "input_tokens"],
    ["outputTokens", "output_tokens"],
    ["cacheReadTokens", "cache_read_input_tokens"],
    ["cacheWriteTokens", "cache_creation_input_tokens"],
  ]) {
    if (isTokenCount(data?.[nativeKey])) usage[key] = data[nativeKey];
  }
  return usage;
}

module.exports = { hasCopilotConversation, hasMalformedJsonl, normalizeCopilotSession };
