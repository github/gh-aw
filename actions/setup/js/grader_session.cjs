// @ts-check

const { calculateWorkingSetFromEntries } = require("./working_set_metrics.cjs");
const { ERR_GRADER_SESSION_INVALID } = require("./error_codes.cjs");

/** @typedef {import("./types/unified_session").UnifiedSession} UnifiedSession */

/** @param {unknown} value @returns {value is Record<string, any>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {unknown} value @returns {number | null} */
function count(value) {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : null;
}

/** @param {unknown} value @returns {number | null} */
function duration(value) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

/** @param {Array<number | null>} values @returns {number | null} */
function sumMeasurements(values) {
  if (!values.length || values.some(value => value === null)) return null;
  const sum = values.reduce((total, value) => (total ?? 0) + (value ?? 0), 0);
  return sum !== null && Number.isFinite(sum) && sum <= Number.MAX_SAFE_INTEGER ? sum : null;
}

/** @param {any} value @returns {string} */
function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  if (isRecord(value))
    return `{${Object.keys(value)
      .sort()
      .map(key => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(",")}}`;
  return JSON.stringify(value) ?? "";
}

/** @param {any} call @returns {boolean} */
function isToolFailure(call) {
  return call.completed === false || call.success === false || call.isError === true || call.status === "error" || call.status === "failure" || (call.error !== undefined && call.error !== null);
}

const startupScope = (id, startupId) => startupId ?? (typeof id === "string" ? /^(startup:[1-9][0-9]*):harness:/.exec(id)?.[1] : undefined);

/**
 * Prefer per-attempt evidence over matching compatibility-summary entries.
 * The history remains data-only so isolated graders can inspect it without I/O.
 * @param {UnifiedSession} session
 */
function attemptHistory(session) {
  const invocation = (event, layer, data) => JSON.stringify([event.provenance.phase, event.provenance.path ?? "", layer, startupScope(data.attemptId, data.startupAttemptId) ?? ""]);
  const canonical = new Set(session.filter(event => event.type === "harness.attempt" && event.provenance.component === "harness").map(event => JSON.stringify([invocation(event, event.data.layer, event.data), event.data.attempt])));
  const attempts = new Map();
  const add = (event, data, layer) => {
    if (!isRecord(data) || count(data.attempt) === null || data.attempt < 1 || !["harness", "startup"].includes(layer)) return;
    const key = JSON.stringify([invocation(event, layer, data), data.attemptId ?? data.attempt, duration(data.startedAt)]);
    attempts.set(key, { ...data, layer, provenance: event.provenance });
  };
  for (const event of session) {
    if (event.provenance.component !== "harness") continue;
    if (event.type === "harness.attempt") add(event, event.data, event.data.layer);
    if (event.type !== "harness.retry") continue;
    for (const [field, layer] of [
      ["attemptResults", "harness"],
      ["startupAttemptResults", "startup"],
    ]) {
      for (const data of Array.isArray(event.data[field]) ? event.data[field] : []) {
        if (isRecord(data) && !canonical.has(JSON.stringify([invocation(event, layer, data), data.attempt]))) add(event, data, layer);
      }
    }
  }
  return [...attempts.values()];
}

/**
 * Materialize indexes and accounting once. Only main-agent observations contribute
 * to execution metrics; detection and evals have their own usage in the same log.
 * @param {UnifiedSession} session
 */
function createSessionLookups(session) {
  if (!Array.isArray(session) || !session.every(event => isRecord(event) && typeof event.type === "string" && isRecord(event.data) && isRecord(event.provenance))) {
    throw new Error(`${ERR_GRADER_SESSION_INVALID}: graders require an in-memory unified session event array`);
  }
  /** @type {Map<string, UnifiedSession>} */
  const eventsByType = new Map();
  for (const event of session) {
    const bucket = eventsByType.get(event.type) ?? [];
    bucket.push(event);
    eventsByType.set(event.type, bucket);
  }
  /** @param {string} type @returns {UnifiedSession} */
  const eventsOfType = type => eventsByType.get(type) ?? [];
  const agentEvents = session.filter(event => event.provenance?.phase === "agent");
  const attempts = attemptHistory(session);
  const attemptScopes = new Map();
  const previousScopes = new Map();
  const attemptKey = (id, startupId) => {
    const scope = startupScope(id, startupId);
    return scope ? JSON.stringify([scope, id]) : id;
  };
  for (const attempt of [...attempts].sort((a, b) => a.attempt - b.attempt)) {
    if (!attempt.attemptId || attempt.provenance.phase !== "agent") continue;
    const invocation = JSON.stringify([attempt.provenance.phase, attempt.provenance.path, attempt.layer, startupScope(attempt.attemptId, attempt.startupAttemptId) ?? ""]);
    const id = attemptKey(attempt.attemptId, attempt.startupAttemptId);
    const root = attempt.strategy === "resume" ? (previousScopes.get(invocation) ?? id) : id;
    attemptScopes.set(id, root);
    previousScopes.set(invocation, root);
  }
  const resolvedAttempt = (id, startupId, sessionId) => {
    const key = attemptKey(id, startupId);
    // Resume describes requested arguments, not proof that the engine resumed.
    return typeof sessionId === "string" && sessionId.length > 0 ? (attemptScopes.get(key) ?? key) : key;
  };
  const attemptScope = event => {
    const id = event.provenance.attemptId;
    return id ? [resolvedAttempt(id, event.provenance.startupAttemptId, event.data.sessionId)] : [];
  };
  /** @type {any[]} */
  const toolCalls = [];
  /** @type {Map<string, any>} */
  const toolCallsById = new Map();
  /** @type {Map<string, {calls: any[], next: number}>} */
  const pendingByName = new Map();
  const sessionScope = event => [event.data.sessionId ?? "", event.data.agentId ?? "", event.provenance.path ?? "", ...attemptScope(event)];
  const correlationScope = event => [event.data.sessionId ?? "", event.data.agentId ?? "", ...attemptScope(event)];
  const scope = event => JSON.stringify([...correlationScope(event), event.data.toolCallId ?? ""]);
  const nameKey = event => JSON.stringify([...correlationScope(event), event.data.toolName ?? ""]);
  const eventIndices = new Map(session.map((event, index) => [event, index]));
  for (const event of agentEvents) {
    if (event.type !== "tool.execution_start" && event.type !== "tool.execution_complete") continue;
    const data = event.data;
    const key = scope(event);
    const queueKey = nameKey(event);
    if (event.type === "tool.execution_start") {
      const call = {
        ...data,
        name: data.toolName ?? "",
        arguments: data.input ?? (data.command ? { command: data.command } : undefined),
        completed: false,
        attemptId: event.provenance.attemptId,
        startupAttemptId: event.provenance.startupAttemptId,
        sessionScope: sessionScope(event),
        eventIndex: eventIndices.get(event),
      };
      toolCalls.push(call);
      if (data.toolCallId) toolCallsById.set(key, call);
      const pending = pendingByName.get(queueKey) ?? { calls: [], next: 0 };
      pending.calls.push(call);
      pendingByName.set(queueKey, pending);
    } else {
      const pending = pendingByName.get(queueKey);
      let call = data.toolCallId ? toolCallsById.get(key) : undefined;
      if (!call && pending) {
        while (pending.next < pending.calls.length && (pending.calls[pending.next].completed || (data.toolCallId && pending.calls[pending.next].toolCallId && pending.calls[pending.next].toolCallId !== data.toolCallId))) {
          pending.next++;
        }
        call = pending.calls[pending.next++];
      }
      if (!call) {
        call = { name: data.toolName ?? "", sessionScope: sessionScope(event), eventIndex: eventIndices.get(event), attemptId: event.provenance.attemptId, startupAttemptId: event.provenance.startupAttemptId };
        toolCalls.push(call);
      }
      Object.assign(call, data, { completed: true, completionEventIndex: eventIndices.get(event) });
      if (data.toolCallId) toolCallsById.set(key, call);
    }
  }

  const nativeCallIds = new Set(
    toolCalls
      .filter(call => call.toolCallId !== undefined && call.toolCallId !== null)
      .map(call => JSON.stringify([String(call.toolCallId), ...(call.attemptId ? [resolvedAttempt(call.attemptId, call.startupAttemptId, call.sessionId)] : [])]))
  );
  const gatewayCalls = new Map();
  for (const event of agentEvents) {
    if (!["mcp.tool_call", "mcp.rpc.request", "mcp.rpc.response"].includes(event.type)) continue;
    const data = event.data;
    if (data.method && data.method !== "tools/call") continue;
    const id = data.toolCallId ?? data.rpcId ?? data.requestId;
    if (!data.toolName && id === undefined) continue;
    const key = JSON.stringify([data.serverName, id ?? eventIndices.get(event), ...attemptScope(event), ...(event.provenance.attemptId ? [data.sessionId ?? "", data.agentId ?? ""] : [])]);
    if (!data.toolName && !gatewayCalls.has(key)) continue;
    const call = gatewayCalls.get(key) ?? {
      name: data.toolName ?? "",
      completed: false,
      eventIndex: eventIndices.get(event),
      attemptId: event.provenance.attemptId,
      startupAttemptId: event.provenance.startupAttemptId,
      sessionId: data.sessionId,
      agentId: data.agentId,
      sessionScope: correlationScope(event),
    };
    if (data.toolName) call.name = data.toolName;
    if (event.type !== "mcp.rpc.request") Object.assign(call, data, { completed: true, completionEventIndex: eventIndices.get(event) });
    gatewayCalls.set(key, call);
  }
  toolCalls.push(
    ...[...gatewayCalls.values()].filter(call => {
      const id = call.toolCallId ?? call.rpcId ?? call.requestId;
      return id === undefined || id === null || !nativeCallIds.has(JSON.stringify([String(id), ...(call.attemptId ? [resolvedAttempt(call.attemptId, call.startupAttemptId, call.sessionId)] : [])]));
    })
  );

  const proxyUsage = agentEvents.filter(event => event.type === "firewall.token_usage");
  const usageEvents = proxyUsage.length ? proxyUsage : agentEvents.filter(event => event.type === "usage.report");
  const tokenUsageEntries = usageEvents.map(event => {
    const usage = isRecord(event.data.usage) ? event.data.usage : {};
    return {
      input_tokens: count(usage.inputTokens),
      output_tokens: count(usage.outputTokens),
      duration_ms: duration(event.data.durationMs),
      model: event.data.model,
    };
  });
  const totalInputTokens = sumMeasurements(tokenUsageEntries.map(entry => entry.input_tokens));
  const totalOutputTokens = sumMeasurements(tokenUsageEntries.map(entry => entry.output_tokens));
  const executionDuration = agentEvents.find(event => event.type === "execution.result" && duration(event.data.durationMs) !== null)?.data.durationMs;
  const totalDurationMs = sumMeasurements(tokenUsageEntries.map(entry => entry.duration_ms)) ?? duration(executionDuration);
  const retryEvents = agentEvents.filter(event => event.type.startsWith("mcp.") && (event.data.event === "retry" || event.data.retryHint != null || /\bretry(?:ing)?\b/i.test(String(event.data.message ?? ""))));
  const harnessRetries = attempts.filter(attempt => attempt.provenance.phase === "agent" && attempt.attempt > 1);
  // Native attempt 1 is already a retry; its sequence may reset per API operation.
  const engineRetries = session
    .filter(event => event.type === "engine.retry" && event.provenance.component === "agent" && (count(event.data.attempt) ?? 0) > 0)
    .map(event => ({ ...event.data, provenance: event.provenance, eventIndex: eventIndices.get(event) }));
  const safeOutputResults = eventsOfType("safe_output.result");
  const safeJobOutcomeTypes = new Set(
    safeOutputResults.filter(event => event.provenance.phase === "safe_outputs" && event.provenance.path === "usage/aw_session.jsonl" && event.data.status !== undefined && typeof event.data.type === "string").map(event => event.data.type)
  );
  const artifacts = safeOutputResults
    .filter(event => event.data.status === undefined || event.data.status === "success")
    .filter(event => event.data.status !== undefined || !safeJobOutcomeTypes.has(event.data.type))
    .map(event => event.data);
  const failedToolCalls = toolCalls.filter(isToolFailure).length;
  const workingSet = calculateWorkingSetFromEntries(tokenUsageEntries).workingSet;
  const trajectory = eventsOfType("trajectory.ir")
    .filter(event => event.provenance.phase === "agent")
    .at(-1)?.data;
  const trajectoryIR = {
    events: agentEvents.filter(event => event.provenance.component === "agent").map(event => ({ index: eventIndices.get(event), kind: event.type })),
    ...trajectory,
    toolCalls: trajectory?.toolCalls ?? toolCalls,
  };
  const trace = {
    toolCalls,
    tokenUsageEntries,
    totalInputTokens,
    totalOutputTokens,
    totalRequests: usageEvents.length,
    totalDurationMs,
    retryEvents,
    attempts,
    engineRetries,
    artifacts,
    trajectoryIR,
  };
  return {
    eventsOfType,
    eventIndices,
    scopeOf: event => JSON.stringify(event.data.sessionId ? correlationScope(event) : sessionScope(event)),
    toolCalls,
    toolCallsById,
    trace,
    stats: {
      toolCallCount: toolCalls.length,
      failedToolCalls,
      totalInputTokens,
      totalOutputTokens,
      totalRequests: usageEvents.length,
      totalDurationMs,
      retryCount: retryEvents.length + harnessRetries.length,
      harnessRetryCount: harnessRetries.length,
      engineRetryCount: engineRetries.filter(retry => retry.provenance.phase === "agent").length,
      artifactCount: artifacts.length,
      workingSet,
    },
  };
}

/**
 * Serialize the data-only lookup contract that can cross into the isolated worker.
 * @param {ReturnType<typeof createSessionLookups>} lookups
 */
function serializeLookupsForWorker(lookups) {
  return {
    stats: lookups.stats,
    toolCalls: lookups.toolCalls,
    trace: lookups.trace,
    toolCallsById: [...lookups.toolCallsById],
  };
}

module.exports = { createSessionLookups, serializeLookupsForWorker, stableStringify, isToolFailure, isRecord };
