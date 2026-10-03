// @ts-check

/**
 * @typedef {import("./types/agent_session").SessionEvent} SessionEvent
 */

const USAGE_ALIASES = {
  input_tokens: "inputTokens",
  output_tokens: "outputTokens",
  cache_creation_input_tokens: "cacheCreationInputTokens",
  cache_read_input_tokens: "cacheReadInputTokens",
};

/** @param {any} value @returns {boolean} */
function isSessionEvent(value) {
  return !!value && typeof value.type === "string" && value.type.includes(".") && !!value.data && typeof value.data === "object" && !Array.isArray(value.data);
}

/** @param {any} value @returns {boolean} */
function isTokenCount(value) {
  return Number.isSafeInteger(value) && value >= 0;
}

/** @param {any} value @returns {boolean} */
function isMetric(value) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

/** @param {any} usage @returns {Record<string, any>|undefined} */
function normalizeSessionUsage(usage) {
  if (!usage || typeof usage !== "object" || Array.isArray(usage)) return undefined;
  const result = { ...usage };
  for (const [key, alias] of Object.entries(USAGE_ALIASES)) {
    const value = Object.hasOwn(usage, key) ? usage[key] : usage[alias];
    if (isTokenCount(value)) result[key] = value;
    else delete result[key];
  }
  if (Array.isArray(usage.overflowed_tokens)) {
    result.overflowed_tokens = [...new Set(usage.overflowed_tokens.filter(key => typeof key === "string" && Object.hasOwn(USAGE_ALIASES, key)))];
    for (const key of result.overflowed_tokens) {
      delete result[key];
      delete result[USAGE_ALIASES[key]];
    }
  } else delete result.overflowed_tokens;
  return result;
}

/**
 * Overflow is explicit unavailable evidence, unlike a snapshot omitting a field.
 * @param {any} previous
 * @param {any} snapshot
 * @returns {Record<string, any>|undefined}
 */
function reconcileSessionUsage(previous, snapshot) {
  const normalized = normalizeSessionUsage(snapshot);
  if (!normalized) return previous;
  const result = { ...previous, ...normalized };
  const overflowed = new Set(Array.isArray(previous?.overflowed_tokens) ? previous.overflowed_tokens : []);
  for (const key of Object.keys(USAGE_ALIASES)) {
    if (isTokenCount(normalized[key])) overflowed.delete(key);
  }
  for (const key of normalized.overflowed_tokens ?? []) {
    overflowed.add(key);
    delete result[key];
    delete result[USAGE_ALIASES[key]];
  }
  if (overflowed.size) result.overflowed_tokens = [...overflowed];
  else delete result.overflowed_tokens;
  return result;
}

/** @param {Record<string, any>} target @param {any} usage */
function accumulateSessionUsage(target, usage) {
  const normalized = normalizeSessionUsage(usage);
  if (!normalized) return;
  const overflowed = new Set([...(Array.isArray(target.overflowed_tokens) ? target.overflowed_tokens : []), ...(normalized.overflowed_tokens ?? [])]);
  for (const key of Object.keys(USAGE_ALIASES)) {
    if (overflowed.has(key) || !isTokenCount(normalized[key])) continue;
    const sum = (target[key] ?? 0) + normalized[key];
    if (isTokenCount(sum)) target[key] = sum;
    else overflowed.add(key);
  }
  if (overflowed.size) {
    target.overflowed_tokens = [...overflowed];
    for (const key of overflowed) {
      delete target[key];
      delete target[USAGE_ALIASES[key]];
    }
  }
}

/**
 * Retain the source envelope and payload additions when expanding an observation.
 * @template {SessionEvent["type"]} T
 * @param {any} source
 * @param {T} type
 * @param {import("./types/agent_session").SessionEventData<T>} data
 * @returns {import("./types/agent_session").SessionEventFor<T>}
 */
function createSessionEvent(source, type, data) {
  return structuredClone({ ...source, type, data: { ...source?.data, ...data } });
}

/**
 * Normalize each record independently; native extension events remain opaque.
 * @param {Array<any>} entries
 * @param {{sourceEngine?: string}} [options]
 * @returns {SessionEvent[]}
 */
function normalizeAgentSession(entries, { sourceEngine } = {}) {
  if (!Array.isArray(entries)) return [];
  /** @type {SessionEvent[]} */
  const events = [];
  const toolUses = new Map();
  const anonymousStarts = [];
  for (const entry of entries) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) continue;
    const emit = (type, data, block = {}) => events.push(createSessionEvent(entry, type, { ...block, ...data }));
    if (isSessionEvent(entry)) {
      events.push(structuredClone(entry));
      if (entry.type === "tool.execution_start" && entry.data.toolCallId !== undefined) toolUses.set(entry.data.toolCallId, entry.data);
      continue;
    }
    if (entry.type === "system" && entry.subtype === "init") {
      emit("session.init", {
        sourceEngine,
        model: entry.model,
        sessionId: entry.session_id,
        cwd: entry.cwd,
        tools: entry.tools,
        mcpServers: entry.mcp_servers,
        slashCommands: entry.slash_commands,
        modelInfo: entry.model_info,
      });
    } else if (entry.type === "assistant" || entry.type === "user" || (entry.type === "system" && entry.subtype)) {
      const content = typeof entry.message === "string" ? entry.message : entry.message?.content;
      const blocks = typeof content === "string" ? [{ type: "text", text: content }] : Array.isArray(content) ? content : [];
      for (const block of blocks) {
        if (!block || typeof block !== "object") continue;
        if (block.type === "text" && typeof block.text === "string") {
          emit(entry.type === "user" ? "user.message" : "assistant.message", { content: block.text }, block);
        } else if (block.type === "thinking" && typeof block.thinking === "string") {
          emit("assistant.reasoning", { content: block.thinking }, block);
        } else if (block.type === "tool_use") {
          const data = { ...block, toolCallId: block.id, toolName: block.name, input: block.input };
          if (block.id !== undefined) toolUses.set(block.id, data);
          else anonymousStarts.push(data);
          emit("tool.execution_start", data);
        } else if (block.type === "tool_result") {
          const start = block.tool_use_id !== undefined ? toolUses.get(block.tool_use_id) : anonymousStarts.length === 1 ? anonymousStarts.pop() : undefined;
          emit(
            "tool.execution_complete",
            {
              toolCallId: block.tool_use_id,
              toolName: block.name ?? start?.toolName,
              success: block.error != null || block.is_error === true ? false : block.is_error === false ? true : typeof block.success === "boolean" ? block.success : Object.hasOwn(block, "is_error") ? undefined : true,
              output: block.content,
              durationMs: isMetric(block.duration_ms) ? block.duration_ms : undefined,
            },
            block
          );
        }
      }
    } else if (entry.type === "reasoning" && typeof entry.data?.content === "string") {
      emit("assistant.reasoning", entry.data);
    } else if (entry.type === "result") {
      const usage = normalizeSessionUsage(entry.usage);
      if (usage && sourceEngine === "claude" && usage.input_tokens_include_cache === undefined && (usage.cache_creation_input_tokens !== undefined || usage.cache_read_input_tokens !== undefined)) usage.input_tokens_include_cache = false;
      emit("session.result", {
        numTurns: isTokenCount(entry.num_turns) ? entry.num_turns : undefined,
        durationMs: isMetric(entry.duration_ms) ? entry.duration_ms : undefined,
        totalCostUsd: isMetric(entry.total_cost_usd) ? entry.total_cost_usd : undefined,
        usage,
        errors: entry.errors,
        permissionDenials: entry.permission_denials,
      });
    }
  }
  return events;
}

/**
 * Select snapshots field by field, never sum snapshots or erase prior known values.
 * @param {Array<any>} events
 * @returns {Record<string, any>|undefined}
 */
function selectSessionResult(events) {
  const normalized = normalizeAgentSession(events);
  const claudeSessions = new Map();
  for (const event of normalized) {
    if (event.type !== "session.result" || event.data.sourceEngine !== "claude" || event.parent_tool_use_id || typeof event.session_id !== "string") continue;
    claudeSessions.set(event.session_id, [...(claudeSessions.get(event.session_id) ?? []), { ...event, session_id: undefined }]);
  }
  if (claudeSessions.size > 1) {
    const aggregate = { usage: {} };
    for (const observations of claudeSessions.values()) {
      const snapshot = selectSessionResult(observations);
      if (!snapshot) continue;
      accumulateSessionUsage(aggregate.usage, snapshot.usage);
      for (const key of ["numTurns", "durationMs", "totalCostUsd"]) {
        if (isMetric(snapshot[key])) aggregate[key] = (aggregate[key] ?? 0) + snapshot[key];
      }
      for (const key of ["errors", "permissionDenials"]) {
        if (Array.isArray(snapshot[key])) aggregate[key] = [...(aggregate[key] ?? []), ...snapshot[key]];
      }
    }
    for (const event of normalized) {
      if (event.type !== "session.result" || event.data.sourceEngine !== "claude" || event.parent_tool_use_id || typeof event.session_id === "string") continue;
      // Unassigned diagnostics are valid evidence, but their usage may overlap a named session.
      for (const key of ["errors", "permissionDenials"]) {
        if (Array.isArray(event.data[key])) aggregate[key] = [...(aggregate[key] ?? []), ...structuredClone(event.data[key])];
      }
    }
    aggregate.usage.input_tokens_include_cache = false;
    return aggregate;
  }
  /** @type {Record<string, any>|undefined} */
  let result;
  for (const event of normalized) {
    if (event.type !== "session.result") continue;
    if (event.data.sourceEngine === "claude" && event.parent_tool_use_id) continue;
    result ??= {};
    const data = event.data;
    for (const [key, value] of Object.entries(data)) {
      if (value === undefined) continue;
      if (key === "usage") result.usage = reconcileSessionUsage(result.usage, value);
      else if ((key === "errors" || key === "permissionDenials") && Array.isArray(value)) result[key] = [...(result[key] ?? []), ...structuredClone(value)];
      else if (key === "numTurns") {
        if (isTokenCount(value)) result[key] = value;
      } else if (key === "durationMs" || key === "totalCostUsd") {
        if (isMetric(value)) result[key] = value;
      } else result = { ...result, [key]: structuredClone(value) };
    }
  }
  return result;
}

/** @param {Array<any>} events @returns {any|undefined} */
function projectSessionResult(events) {
  const data = selectSessionResult(events);
  if (!data) return undefined;
  return {
    type: "result",
    num_turns: data.numTurns,
    duration_ms: data.durationMs,
    total_cost_usd: data.totalCostUsd,
    usage: data.usage,
    errors: data.errors,
    permission_denials: data.permissionDenials,
  };
}

/** @param {Array<any>} events @returns {any|undefined} */
function projectSessionInitialization(events) {
  let data;
  for (const event of normalizeAgentSession(events)) {
    if (event.type !== "session.init" && event.type !== "session.start") continue;
    data ??= {};
    for (const [key, value] of Object.entries(event.data)) {
      if (value !== undefined) data = { ...data, [key]: structuredClone(value) };
    }
  }
  if (!data) return undefined;
  return {
    type: "system",
    subtype: "init",
    source_engine: data.sourceEngine,
    model: data.model,
    session_id: data.sessionId,
    cwd: data.cwd,
    tools: data.tools,
    mcp_servers: data.mcpServers,
    slash_commands: data.slashCommands,
    model_info: data.modelInfo,
  };
}

/** @param {any} value @returns {string} */
function sessionOutputText(value) {
  if (typeof value === "string") return value;
  if (value === undefined) return "";
  if (value && typeof value === "object") {
    if (typeof value.text === "string") return value.text;
    if (Object.hasOwn(value, "content")) {
      return Array.isArray(value.content) ? value.content.map(sessionOutputText).join("\n") : sessionOutputText(value.content);
    }
    if (value.type === "json" && Object.hasOwn(value, "json")) return sessionOutputText(value.json);
  }
  return JSON.stringify(value, null, 2);
}

/** @param {any} data @returns {boolean|undefined} */
function sessionToolSuccess(data) {
  if (
    data.success === false ||
    data.is_error === true ||
    data.isError === true ||
    data.result?.isError === true ||
    data.result?.is_error === true ||
    data.status === "failed" ||
    data.status === "error" ||
    data.error != null ||
    (typeof data.exitCode === "number" && data.exitCode !== 0)
  )
    return false;
  return typeof data.success === "boolean" ? data.success : undefined;
}

/**
 * Shared flat Gemini/Pi transport, retaining a legacy intermediate for existing callers.
 * @param {Array<any>} records
 * @returns {Array<any>}
 */
function transformFlatSessionEntries(records) {
  /** @type {Array<any>} */
  const entries = [];
  for (const raw of records) {
    if (!raw || typeof raw !== "object") continue;
    if (isSessionEvent(raw) || (["assistant", "user", "system"].includes(raw.type) && raw.message)) {
      entries.push(structuredClone(raw));
    } else if (raw.type === "init") {
      entries.push({ ...structuredClone(raw), type: "system", subtype: "init" });
    } else if (raw.type === "message" || raw.type === "assistant" || raw.type === "user" || raw.type === "reasoning") {
      const text = raw.content;
      if (typeof text !== "string") continue;
      const role = raw.type === "message" ? raw.role : raw.type;
      if (!["assistant", "user", "reasoning"].includes(role)) continue;
      const kind = role === "reasoning" ? "thinking" : "text";
      const last = entries.at(-1);
      const canMerge = raw.delta === true && last?.delta === true && last.type === role && last.id === raw.id && last.timestamp === raw.timestamp && last.message?.content?.length === 1 && last.message.content[0].type === kind;
      if (canMerge) last.message.content[0][kind === "text" ? "text" : "thinking"] += text;
      else entries.push({ ...structuredClone(raw), type: role === "user" ? "user" : "assistant", message: { content: [{ type: kind, [kind === "text" ? "text" : "thinking"]: text }] } });
    } else if (raw.type === "tool_use") {
      entries.push({ ...structuredClone(raw), type: "assistant", message: { content: [{ ...structuredClone(raw), type: "tool_use", id: raw.tool_id, name: raw.tool_name, input: raw.parameters }] } });
    } else if (raw.type === "tool_result") {
      const success = raw.status === "error" || raw.status === "failed" || raw.error != null ? false : raw.status === "success" ? true : undefined;
      entries.push({
        ...structuredClone(raw),
        type: "user",
        message: { content: [{ ...structuredClone(raw), type: "tool_result", tool_use_id: raw.tool_id, content: raw.output, is_error: success === undefined ? undefined : !success, duration_ms: raw.duration_ms }] },
      });
    } else if (raw.type === "result") {
      const stats = raw.stats ?? {};
      const usage = { ...raw.usage };
      for (const key of Object.keys(USAGE_ALIASES)) {
        if (isTokenCount(stats[key])) usage[key] = stats[key];
      }
      if (isTokenCount(stats.total_tokens)) usage.total_tokens = stats.total_tokens;
      if (isTokenCount(stats.cached)) usage.cache_read_input_tokens = stats.cached;
      entries.push({
        ...structuredClone(raw),
        usage: Object.keys(usage).length ? usage : undefined,
        num_turns: stats.turns ?? raw.num_turns,
        duration_ms: stats.duration_ms ?? raw.duration_ms,
        total_cost_usd: stats.total_cost_usd ?? raw.total_cost_usd,
        errors: raw.errors ?? (raw.error != null ? [raw.error] : undefined),
      });
    } else if (raw.type === "error" && (raw.error !== undefined || typeof raw.message === "string")) {
      entries.push({ ...structuredClone(raw), type: "result", errors: [raw.error ?? raw.message] });
    }
  }
  return entries;
}

/** @param {any} usage @returns {number|undefined} */
function sessionTokenTotal(usage) {
  if (!usage) return undefined;
  if (isTokenCount(usage.total_tokens)) return usage.total_tokens;
  if (!isTokenCount(usage.input_tokens) && !isTokenCount(usage.output_tokens)) return undefined;
  const cache = usage.input_tokens_include_cache === false ? (usage.cache_creation_input_tokens ?? 0) + (usage.cache_read_input_tokens ?? 0) : 0;
  const total = (usage.input_tokens ?? 0) + (usage.output_tokens ?? 0) + cache;
  return isTokenCount(total) ? total : undefined;
}

/** @param {Array<any>} events @returns {string|undefined} */
function observedSessionModel(events) {
  let model;
  for (const event of events) {
    const value = event?.type === "system" && event.subtype === "init" ? event.model : ["session.init", "session.start", "pi.message_snapshot"].includes(event?.type) ? event.data?.model : undefined;
    if (typeof value === "string" && value.length) model = value;
  }
  return model;
}

module.exports = {
  isSessionEvent,
  isTokenCount,
  isMetric,
  normalizeSessionUsage,
  accumulateSessionUsage,
  reconcileSessionUsage,
  createSessionEvent,
  normalizeAgentSession,
  selectSessionResult,
  projectSessionResult,
  projectSessionInitialization,
  sessionOutputText,
  sessionToolSuccess,
  transformFlatSessionEntries,
  sessionTokenTotal,
  observedSessionModel,
};
