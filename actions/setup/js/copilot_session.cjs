// @ts-check

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {import("./types/agent_session").SessionUsage} SessionUsage */
/** @typedef {import("./types/agent_session").SessionResultData} SessionResultData */

const { normalizeAgentSession, createSessionEvent, accumulateSessionUsage, isTokenCount, isMetric, sessionEventContexts } = require("./agent_session.cjs");
const { isDeepStrictEqual } = require("node:util");

const COPILOT_CONVERSATION_EVENT_TYPES = new Set(["assistant.message", "assistant.message_delta", "assistant.reasoning", "assistant.reasoning_delta", "assistant.refusal", "user.message", "tool.execution_start", "tool.execution_complete"]);

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
  const usageByScope = new Map();
  const responses = new Set();
  const turns = new Set();
  const tools = new Map();
  const projections = new Map();
  const projectionKey = (label, event) => JSON.stringify([label, event.type, event.id, event.timestamp]);
  const projectionData = data => Object.fromEntries(Object.entries(data).filter(([, value]) => value !== undefined));
  /** @param {unknown} value @returns {unknown} */
  const projectionProvenance = value => {
    if (!value || typeof value !== "object" || Array.isArray(value)) return value;
    return Object.fromEntries(
      Object.entries(value)
        .filter(([key]) => !["index", "timestampMs", "persistedPath"].includes(key))
        .map(([key, nested]) => [key, key === "native" ? projectionProvenance(nested) : nested])
    );
  };
  const projectionEvidence = event =>
    projectionData({
      ...event,
      data: projectionData(event.data),
      ...(Object.hasOwn(event, "provenance") ? { provenance: projectionProvenance(event.provenance) } : {}),
    });
  const summaryScopes = [];
  const contexts = sessionEventContexts(source);
  for (const [index, event] of source.entries()) {
    const context = contexts[index];
    if (
      context.nested &&
      [
        "session.start",
        "session.init",
        "session.result",
        "session.error",
        "session.shutdown",
        "session.task_complete",
        "user.message",
        "system.message",
        "assistant.message",
        "assistant.reasoning",
        "assistant.refusal",
        "assistant.message_delta",
        "assistant.reasoning_delta",
        "assistant.turn_start",
        "assistant.turn_end",
        "assistant.usage",
        "tool.execution_start",
        "tool.execution_complete",
      ].includes(event.type)
    ) {
      if (!Object.hasOwn(event, "agentId") && typeof context.agentId === "string") event.agentId = context.agentId;
      for (const key of ["sessionId", "agentId", "parentAgentId", "parentToolCallId"]) {
        if (!Object.hasOwn(event.data, key) && context[key] !== undefined) event.data[key] = context[key];
      }
    }
  }
  const originScopes = new Map();
  const summaryMessages = new Map();
  const emittedMessages = new Map();
  const summaryDeltas = new Map();
  const retainedSummaries = new Set();
  for (const [index, event] of source.entries()) {
    const context = contexts[index];
    const originKey = JSON.stringify([context.sourceKey, context.agentId, context.agentId === undefined ? context.parentToolCallId : undefined, event.id, event.timestamp]);
    const observedScope = context.scope;
    const scope = event.copilotProjection && event.id !== undefined ? (originScopes.get(originKey) ?? observedScope) : observedScope;
    summaryScopes[index] = scope;
    if (!event.copilotProjection && event.id !== undefined) originScopes.set(originKey, scope);
    if (event.type === "assistant.message_delta" && typeof event.data.deltaContent === "string") {
      const identity = JSON.stringify([scope, event.data.messageId ?? index]);
      const previous = summaryDeltas.get(identity);
      summaryDeltas.set(identity, { scope, content: (previous?.content ?? "") + event.data.deltaContent });
    }
    if (event.type !== "assistant.message" || typeof event.data.content !== "string") continue;
    if (!summaryMessages.has(scope)) summaryMessages.set(scope, new Set());
    summaryMessages.get(scope).add(event.data.content);
    if (event.copilotProjection !== "session.task_complete") {
      if (!emittedMessages.has(scope)) emittedMessages.set(scope, new Set());
      emittedMessages.get(scope).add(event.data.content);
    }
  }
  for (const { scope, content } of summaryDeltas.values()) {
    if (!summaryMessages.has(scope)) summaryMessages.set(scope, new Set());
    summaryMessages.get(scope).add(content);
    if (!emittedMessages.has(scope)) emittedMessages.set(scope, new Set());
    emittedMessages.get(scope).add(content);
  }
  const correlationKey = (scope, id) => JSON.stringify([scope, id]);
  const streamKey = (scope, event) => JSON.stringify([scope, event.type.startsWith("assistant.reasoning") ? "reasoning" : "message", event.data.reasoningId ?? event.data.messageId]);
  const deltaText = new Map();
  const nativeStarts = new Set();
  const recordedTurnScopes = new Set();
  const recordedUsage = new Set();
  const usageSourceKey = (scope, event) => JSON.stringify([scope, event.id, event.data.apiCallId, event.timestamp]);
  for (const [index, event] of source.entries()) {
    const scope = summaryScopes[index];
    if (["assistant.message_delta", "assistant.reasoning_delta"].includes(event.type) && (event.data.messageId !== undefined || event.data.reasoningId !== undefined) && typeof event.data.deltaContent === "string") {
      const key = streamKey(scope, event);
      deltaText.set(key, (deltaText.get(key) ?? "") + event.data.deltaContent);
    }
    if (event.type === "tool.execution_start" && !event.copilotProjection && event.data.toolCallId !== undefined) nativeStarts.add(correlationKey(scope, event.data.toolCallId));
    if (event.type === "assistant.turn_end") recordedTurnScopes.add(scope);
    if (event.type === "assistant.usage" && !event.copilotProjection) recordedUsage.add(usageSourceKey(scope, event));
  }
  const supersededAccounting = new Set(
    source.filter(
      (event, index) =>
        (event.copilotProjection === "finalized-turns" && recordedTurnScopes.has(summaryScopes[index])) ||
        (event.copilotProjection === "assistant.usage" && event.type === "session.result" && recordedUsage.has(usageSourceKey(summaryScopes[index], event)))
    )
  );
  for (const event of source) {
    if (!event.copilotProjection || supersededAccounting.has(event)) continue;
    const key = projectionKey(event.copilotProjection, event);
    const bucket = projections.get(key) ?? [];
    bucket.push({ evidence: projectionEvidence(event), consumed: false });
    projections.set(key, bucket);
  }
  const snapshots = new Set();
  for (const [index, event] of source.entries()) {
    const key = streamKey(summaryScopes[index], event);
    if (["assistant.message", "assistant.reasoning"].includes(event.type) && !event.copilotProjection && typeof event.data.content === "string" && deltaText.has(key) && event.data.content.startsWith(deltaText.get(key))) snapshots.add(key);
  }
  const finalizedTurns = new Map();
  const turnResults = new Set();

  for (let index = 0; index < source.length; index++) {
    const event = source[index];
    /** @type {Record<string, any>} */
    const data = event.data;
    const summaryScope = summaryScopes[index];
    const context = contexts[index];
    if (supersededAccounting.has(event)) continue;
    if ((event.copilotProjection === "assistant.message_delta" || event.copilotProjection === "assistant.reasoning_delta") && snapshots.has(streamKey(summaryScope, event))) continue;
    if (event.type === "tool.execution_start" && event.copilotProjection === "assistant.toolRequests" && data.toolCallId !== undefined && nativeStarts.has(correlationKey(summaryScope, data.toolCallId))) continue;
    if (event.type === "assistant.message" && event.copilotProjection === "session.task_complete") {
      const identity = JSON.stringify([summaryScope, data.content]);
      if (emittedMessages.get(summaryScope)?.has(data.content) || retainedSummaries.has(identity)) continue;
      retainedSummaries.add(identity);
    }
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
      if (data.toolCallId !== undefined) tools.set(correlationKey(summaryScope, data.toolCallId), data);
    } else if (event.type === "tool.execution_complete") {
      const start = data.toolCallId !== undefined ? tools.get(correlationKey(summaryScope, data.toolCallId)) : undefined;
      for (const key of ["toolName", "mcpServerName"]) {
        if (!Object.hasOwn(data, key) && start?.[key] !== undefined) data[key] = structuredClone(start[key]);
      }
      if (!Object.hasOwn(data, "exitCode") && Number.isSafeInteger(data.shellExecution?.exitCode)) data.exitCode = data.shellExecution.exitCode;
    } else if (event.type === "assistant.message" || event.type === "assistant.refusal") {
      if (typeof data.reasoningText === "string") project("assistant.reasoning", { content: data.reasoningText });
      for (const request of Array.isArray(data.toolRequests) ? data.toolRequests : []) {
        if (!request || typeof request !== "object" || Array.isArray(request) || typeof request.name !== "string") continue;
        if (request.toolCallId !== undefined && nativeStarts.has(correlationKey(summaryScope, request.toolCallId))) continue;
        const fields = {
          ...request,
          toolName: request.name,
          ...(!Object.hasOwn(request, "input") && !Object.hasOwn(request, "parameters") && Object.hasOwn(request, "arguments") ? { input: structuredClone(request.arguments) } : {}),
        };
        project("tool.execution_start", fields, "assistant.toolRequests");
        if (request.toolCallId !== undefined) tools.set(correlationKey(summaryScope, request.toolCallId), fields);
      }
    } else if (["assistant.message_delta", "assistant.reasoning_delta"].includes(event.type) && typeof data.deltaContent === "string" && !snapshots.has(streamKey(summaryScope, event))) {
      project(event.type === "assistant.reasoning_delta" ? "assistant.reasoning" : "assistant.message", { content: data.deltaContent, delta: true });
    } else if (event.type === "session.task_complete" && typeof data.summary === "string" && data.summary.trim()) {
      const messages = summaryMessages.get(summaryScope) ?? new Set();
      if (!messages.has(data.summary)) {
        project("assistant.message", { content: data.summary });
        messages.add(data.summary);
        summaryMessages.set(summaryScope, messages);
        retainedSummaries.add(JSON.stringify([summaryScope, data.summary]));
      }
    } else if (event.type === "assistant.turn_end") {
      const identity = data.turnId !== undefined || event.id !== undefined ? correlationKey(summaryScope, data.turnId ?? event.id) : undefined;
      if (identity === undefined || !turns.has(identity)) {
        const previous = finalizedTurns.get(summaryScope);
        finalizedTurns.set(summaryScope, { count: (previous?.count ?? 0) + 1, event, context });
        if (identity !== undefined) turns.add(identity);
      }
    } else if (event.type === "assistant.usage") {
      /** @type {SessionUsage} */
      const usage = usageByScope.get(summaryScope) ?? {};
      const identity = data.apiCallId !== undefined || event.id !== undefined ? correlationKey(summaryScope, data.apiCallId ?? event.id) : undefined;
      if (identity === undefined || !responses.has(identity)) {
        if (identity !== undefined) responses.add(identity);
        accumulateSessionUsage(usage, copilotUsage(data));
        usageByScope.set(summaryScope, usage);
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
      if (data.agentMetrics !== undefined) result.agentMetrics = structuredClone(data.agentMetrics);
      if (Object.keys(result).length) project("session.result", result);
    } else if (event.type === "session.error") {
      project("session.result", { errors: [data.error !== undefined ? structuredClone(data.error) : structuredClone(data)] });
    }
    if (event.type === "session.result" && isTokenCount(data.numTurns)) turnResults.add(summaryScope);
  }

  for (const [scope, { count, event, context }] of finalizedTurns) {
    if (!turnResults.has(scope)) {
      const identity = Object.fromEntries(
        Object.entries({ sessionId: context.sessionId, agentId: context.agentId, parentAgentId: context.parentAgentId, parentToolCallId: context.parentToolCallId }).filter(([, value]) => value !== undefined)
      );
      events.push({ ...event, type: "session.result", data: { ...identity, numTurns: count }, copilotProjection: "finalized-turns" });
    }
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
    ["reasoningTokens", "reasoning_output_tokens"],
  ]) {
    if (isTokenCount(data?.[nativeKey])) usage[key] = data[nativeKey];
  }
  return usage;
}

module.exports = { hasCopilotConversation, hasMalformedJsonl, normalizeCopilotSession };
