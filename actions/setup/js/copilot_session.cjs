// @ts-check

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {import("./types/agent_session").SessionUsage} SessionUsage */
/** @typedef {import("./types/agent_session").SessionResultData} SessionResultData */

const { normalizeAgentSession, createSessionEvent, accumulateSessionUsage, isTokenCount, isMetric, sessionContext } = require("./agent_session.cjs");
const { isDeepStrictEqual } = require("node:util");
const { getMessageRefusal } = require("./provider_refusal.cjs");

const COPILOT_CONVERSATION_EVENT_TYPES = new Set([
  "assistant.message",
  "assistant.message_delta",
  "assistant.reasoning",
  "assistant.reasoning_delta",
  "assistant.refusal",
  "user.message",
  "tool.execution_start",
  "tool.execution_complete",
  "session.result",
]);

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

/** @param {any} event @returns {ReturnType<typeof sessionContext>} */
function copilotSessionContext(event) {
  const context = sessionContext(event);
  const data = event?.data ?? {};
  if ([data.parentToolUseId, event?.parentToolUseId, event?.parent_tool_use_id, data.parent_tool_use_id].some(value => value !== undefined)) return context;
  const parentToolCallId = data.parentToolCallId !== undefined ? data.parentToolCallId : event?.parentToolCallId;
  return typeof parentToolCallId === "string" || parentToolCallId === null ? { ...context, parentToolUseId: parentToolCallId } : context;
}

/**
 * Remove only complete identified copies in the same observed session.
 * Conflicting/reused IDs and unidentified observations remain independent evidence.
 * @param {SessionEvent[]} fallback
 * @param {SessionEvent[][]} retained
 * @returns {SessionEvent[]}
 */
function deduplicateCopilotFallback(fallback, retained) {
  const scoped = entries => {
    let activeSessionId;
    const childSessions = new Map();
    return entries.map(event => {
      const context = copilotSessionContext(event);
      const childKey = JSON.stringify([context.agentId, context.parentToolUseId]);
      if (["session.start", "session.init"].includes(event.type) && context.sessionId !== undefined) {
        if (context.agentId !== undefined || context.parentToolUseId) childSessions.set(childKey, context.sessionId);
        else {
          if (activeSessionId !== context.sessionId) childSessions.clear();
          activeSessionId = context.sessionId;
        }
      }
      return { event, sessionId: context.sessionId !== undefined ? context.sessionId : childSessions.has(childKey) ? childSessions.get(childKey) : activeSessionId };
    });
  };
  const observations = retained.flatMap(scoped);
  return scoped(fallback)
    .filter(({ event, sessionId }) => typeof event.id !== "string" || !event.id || !observations.some(previous => previous.sessionId === sessionId && isDeepStrictEqual(previous.event, event)))
    .map(({ event }) => event);
}

/**
 * Copilot persists lifecycle events but emits assistant.usage only on the live
 * transport. Keep those observations and project their accounting separately.
 * @param {Array<any>} entries
 * @returns {SessionEvent[]}
 */
function normalizeCopilotSession(entries) {
  const source = normalizeAgentSession(entries, { sourceEngine: "copilot" });
  for (const [index, event] of source.entries()) {
    const context = copilotSessionContext(event);
    if (context.parentToolUseId !== undefined && sessionContext(event).parentToolUseId === undefined) event.data.parentToolUseId = context.parentToolUseId;
    if (event.type !== "assistant.message") continue;
    const refusal = getMessageRefusal(event.data, typeof event.data.finishReason === "string" ? event.data.finishReason : undefined);
    if (refusal) source[index] = createSessionEvent(event, "assistant.refusal", refusal);
  }
  /** @type {SessionEvent[]} */
  const events = [];
  const accounting = new Map();
  const turns = new Map();
  const tools = new Map();
  const summaryScopes = [];
  const scopeSessions = new Map();
  const sourceSessions = new Map();
  const childSessions = new Map();
  const originScopes = new Map();
  for (const [index, event] of source.entries()) {
    const provenance = event.provenance && typeof event.provenance === "object" ? event.provenance : {};
    const sourceKey = JSON.stringify(["component" in provenance ? provenance.component : undefined, "phase" in provenance ? provenance.phase : undefined, "path" in provenance ? provenance.path : undefined]);
    const context = sessionContext(event);
    const childKey = JSON.stringify([sourceKey, context.agentId, context.parentToolUseId]);
    if (["session.start", "session.init"].includes(event.type) && context.sessionId !== undefined) {
      if (context.agentId !== undefined || context.parentToolUseId) childSessions.set(childKey, context.sessionId);
      else sourceSessions.set(sourceKey, context.sessionId);
    }
    const originKey = JSON.stringify([sourceKey, context.agentId, context.parentToolUseId, event.id, event.timestamp]);
    const sessionId = context.sessionId !== undefined ? context.sessionId : childSessions.has(childKey) ? childSessions.get(childKey) : sourceSessions.get(sourceKey);
    const observedScope = JSON.stringify([sourceKey, sessionId, context.agentId, context.parentToolUseId]);
    const scope = event.copilotProjection && event.id !== undefined ? (originScopes.get(originKey) ?? observedScope) : observedScope;
    summaryScopes[index] = scope;
    if (!scopeSessions.has(scope)) scopeSessions.set(scope, sessionId);
    if (!event.copilotProjection && event.id !== undefined) originScopes.set(originKey, scope);
  }
  const projections = new Map();
  const projectionKey = (label, event, scope) => JSON.stringify([scope, label, event.type, event.id, event.timestamp]);
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
  for (const [index, event] of source.entries()) {
    if (!event.copilotProjection) continue;
    const key = projectionKey(event.copilotProjection, event, summaryScopes[index]);
    const bucket = projections.get(key) ?? [];
    bucket.push({ event, evidence: projectionEvidence(event), consumed: false });
    projections.set(key, bucket);
  }
  const deltaText = new Map();
  const deltaObservations = new Map();
  const deltaIdentity = (event, index, channel) => JSON.stringify([summaryScopes[index], channel, channel === "assistant.reasoning" ? event.data.reasoningId : event.data.messageId]);
  for (const [index, event] of source.entries()) {
    if (["assistant.message_delta", "assistant.reasoning_delta"].includes(event.type) && typeof event.data.deltaContent === "string") {
      const channel = event.type === "assistant.reasoning_delta" ? "assistant.reasoning" : "assistant.message";
      const id = channel === "assistant.reasoning" ? event.data.reasoningId : event.data.messageId;
      if (id === undefined) continue;
      const identity = deltaIdentity(event, index, channel);
      if (event.id !== undefined) {
        const key = JSON.stringify([identity, event.id, event.timestamp]);
        const observations = deltaObservations.get(key) ?? [];
        const evidence = projectionEvidence(event);
        if (observations.some(previous => isDeepStrictEqual(previous, evidence))) continue;
        observations.push(evidence);
        deltaObservations.set(key, observations);
      }
      deltaText.set(identity, (deltaText.get(identity) ?? "") + event.data.deltaContent);
    }
  }
  const snapshots = new Set();
  for (const [index, event] of source.entries()) {
    if (!["assistant.message", "assistant.reasoning"].includes(event.type) || event.copilotProjection || typeof event.data.content !== "string") continue;
    const identity = deltaIdentity(event, index, event.type);
    if (deltaText.has(identity) && event.data.content.startsWith(deltaText.get(identity))) snapshots.add(identity);
  }
  const summaryMessages = new Map();
  const emittedMessages = new Map();
  const summaryDeltas = new Map();
  const retainedSummaries = new Set();
  for (const [index, event] of source.entries()) {
    const scope = summaryScopes[index];
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
  const turnResults = new Set();
  const refusalCalls = new Set(
    source.map((event, index) => (event.type === "assistant.refusal" && event.data.apiCallId !== undefined ? JSON.stringify([summaryScopes[index], event.data.apiCallId]) : undefined)).filter(value => value !== undefined)
  );

  for (let index = 0; index < source.length; index++) {
    const event = source[index];
    /** @type {Record<string, any>} */
    const data = event.data;
    const summaryScope = summaryScopes[index];
    if (typeof event.copilotProjection === "string" && ["assistant.message_delta", "assistant.reasoning_delta"].includes(event.copilotProjection) && snapshots.has(deltaIdentity(event, index, event.type))) continue;
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
      const sessionId = scopeSessions.get(summaryScope);
      const projected = {
        ...createSessionEvent(event, type, {
          ...(String(type) === "session.result" ? { sourceEngine: "copilot" } : {}),
          ...(sessionId !== undefined && !Object.hasOwn(event.data, "sessionId") ? { sessionId } : {}),
          ...fields,
        }),
        copilotProjection: label,
      };
      const key = projectionKey(label, projected, summaryScope);
      const bucket = projections.get(key) ?? [];
      const identified = event.id !== undefined;
      const comparable = projectionEvidence(projected);
      const previous = bucket.find(item => {
        if (!identified && item.consumed) return false;
        if (isDeepStrictEqual(item.evidence, comparable)) return true;
        // Older persisted managed projections can lack newly mapped fields.
        // Upgrade only absent additions, never replace conflicting evidence.
        const compatible = structuredClone(comparable);
        for (const key of ["sourceEngine", "sessionId", "delta"]) {
          if (!Object.hasOwn(item.evidence.data, key)) delete compatible.data[key];
        }
        if (compatible.data.usage && item.evidence.data.usage && !Object.hasOwn(item.evidence.data.usage, "reasoning_output_tokens")) delete compatible.data.usage.reasoning_output_tokens;
        if (!isDeepStrictEqual(item.evidence, compatible)) return false;
        Object.assign(item.event.data, projected.data);
        item.evidence = projectionEvidence(item.event);
        return true;
      });
      if (previous) {
        previous.consumed = true;
        return;
      }
      events.push(projected);
      if (identified) {
        bucket.push({ event: projected, evidence: comparable, consumed: true });
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
      if (data.toolCallId !== undefined) tools.set(JSON.stringify([summaryScope, data.toolCallId]), data);
    } else if (event.type === "tool.execution_complete") {
      const start = data.toolCallId !== undefined ? tools.get(JSON.stringify([summaryScope, data.toolCallId])) : undefined;
      if (!Object.hasOwn(data, "toolName") && start?.toolName !== undefined) data.toolName = start.toolName;
      if (!Object.hasOwn(data, "exitCode") && Number.isSafeInteger(data.shellExecution?.exitCode)) data.exitCode = data.shellExecution.exitCode;
    } else if ((event.type === "assistant.message" || event.type === "assistant.refusal") && typeof data.reasoningText === "string") {
      project("assistant.reasoning", { content: data.reasoningText });
    } else if (["assistant.message_delta", "assistant.reasoning_delta"].includes(event.type) && typeof data.deltaContent === "string") {
      const channel = event.type === "assistant.reasoning_delta" ? "assistant.reasoning" : "assistant.message";
      if (!snapshots.has(deltaIdentity(event, index, channel))) project(channel, { content: data.deltaContent, delta: true });
    } else if (event.type === "session.task_complete" && typeof data.summary === "string") {
      const messages = summaryMessages.get(summaryScope) ?? new Set();
      if (!messages.has(data.summary)) {
        project("assistant.message", { content: data.summary });
        messages.add(data.summary);
        summaryMessages.set(summaryScope, messages);
        retainedSummaries.add(JSON.stringify([summaryScope, data.summary]));
      }
    } else if (event.type === "assistant.turn_end") {
      const scopeTurns = turns.get(summaryScope) ?? { identities: new Set(), count: 0, event };
      const identity = data.turnId ?? event.id;
      if (identity === undefined || !scopeTurns.identities.has(identity)) {
        scopeTurns.count++;
        if (identity !== undefined) scopeTurns.identities.add(identity);
      }
      scopeTurns.event = event;
      turns.set(summaryScope, scopeTurns);
    } else if (event.type === "assistant.usage") {
      const reports = accounting.get(summaryScope) ?? new Map();
      const identity = data.apiCallId ?? event.id;
      const key = identity ?? Symbol();
      reports.set(key, { ...reports.get(key), ...copilotUsage(data) });
      accounting.set(summaryScope, reports);
      /** @type {SessionUsage} */
      const usage = {};
      for (const report of reports.values()) accumulateSessionUsage(usage, report);
      if (Object.keys(usage).length) project("session.result", { usage: { ...usage } });
      const refusalKey = data.apiCallId !== undefined ? JSON.stringify([summaryScope, data.apiCallId]) : undefined;
      if (data.finishReason === "content_filter" && (refusalKey === undefined || !refusalCalls.has(refusalKey))) {
        project("assistant.refusal", { reason: "content_filter" });
        if (refusalKey !== undefined) refusalCalls.add(refusalKey);
      }
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
    } else if (event.type === "session.error" || event.type === "model.call_failure") {
      project("session.result", { errors: [data.error !== undefined ? structuredClone(data.error) : structuredClone(data)] });
    }
    if (event.type === "session.result" && isTokenCount(data.numTurns)) turnResults.add(summaryScope);
  }

  for (const [scope, observed] of turns) {
    if (!turnResults.has(scope)) {
      const sessionId = scopeSessions.get(scope);
      events.push({
        ...createSessionEvent(observed.event, "session.result", { sourceEngine: "copilot", ...(sessionId !== undefined ? { sessionId } : {}), numTurns: observed.count }),
        copilotProjection: "finalized-turns",
      });
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
    ["reasoningTokens", "reasoning_output_tokens"],
    ["cacheReadTokens", "cache_read_input_tokens"],
    ["cacheWriteTokens", "cache_creation_input_tokens"],
  ]) {
    if (isTokenCount(data?.[nativeKey])) usage[key] = data[nativeKey];
  }
  return usage;
}

module.exports = { hasCopilotConversation, hasMalformedJsonl, copilotSessionContext, deduplicateCopilotFallback, normalizeCopilotSession };
