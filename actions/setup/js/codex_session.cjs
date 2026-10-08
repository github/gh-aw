// @ts-check

const { createSessionEvent, normalizeAgentSession, isSessionEvent, accumulateSessionUsage, normalizeSessionUsage, isTokenCount, isMetric, sessionToolSuccess } = require("./agent_session.cjs");

/** @param {any} record @returns {boolean} */
function isCodexRecord(record) {
  if (!record || typeof record !== "object" || Array.isArray(record)) return false;
  return (
    isSessionEvent(record) ||
    (record.type === "thread.started" && Object.hasOwn(record, "thread_id")) ||
    ["turn.started", "turn.completed", "turn.failed"].includes(record.type) ||
    (record.type === "error" && (Object.hasOwn(record, "message") || Object.hasOwn(record, "error"))) ||
    (["item.started", "item.updated", "item.completed"].includes(record.type) && !!record.item && typeof record.item === "object")
  );
}

/** @param {any} item @returns {string|undefined} */
function itemText(item) {
  return typeof item?.text === "string" ? item.text : typeof item?.summary === "string" ? item.summary : undefined;
}

/** @param {any} item @param {any} previous @returns {Record<string, any>|undefined} */
function toolInvocation(item, previous) {
  const own = (field, fallback) => (Object.hasOwn(item, field) ? item[field] : fallback);
  switch (item.type) {
    case "command_execution":
      return { toolName: "bash", command: own("command", previous?.command), input: Object.hasOwn(item, "command") ? { command: item.command } : previous?.input };
    case "mcp_tool_call":
      return { toolName: own("tool", previous?.toolName), mcpServerName: own("server", previous?.mcpServerName), input: own("arguments", previous?.input) };
    case "collab_tool_call":
      return {
        toolName: own("tool", previous?.toolName),
        input:
          previous?.input !== undefined || ["sender_thread_id", "receiver_thread_ids", "prompt"].some(field => Object.hasOwn(item, field))
            ? {
                ...previous?.input,
                ...(Object.hasOwn(item, "sender_thread_id") ? { senderThreadId: item.sender_thread_id } : {}),
                ...(Object.hasOwn(item, "receiver_thread_ids") ? { receiverThreadIds: item.receiver_thread_ids } : {}),
                ...(Object.hasOwn(item, "prompt") ? { prompt: item.prompt } : {}),
              }
            : undefined,
      };
    case "file_change":
      return { toolName: "apply_patch", input: Object.hasOwn(item, "changes") ? { changes: item.changes } : previous?.input };
    case "web_search":
      return {
        toolName: "web_search",
        input:
          Object.hasOwn(item, "query") || Object.hasOwn(item, "action") ? { ...previous?.input, ...Object.fromEntries(["query", "action"].filter(field => Object.hasOwn(item, field)).map(field => [field, item[field]])) } : previous?.input,
      };
    case "todo_list":
      return { toolName: "update_plan", input: Object.hasOwn(item, "items") ? { items: item.items } : previous?.input };
    default:
      return undefined;
  }
}

/** @param {any} item @returns {any} */
function toolOutput(item) {
  switch (item.type) {
    case "command_execution":
      return item.aggregated_output;
    case "collab_tool_call":
      return Object.hasOwn(item, "agents_states") ? { agentsStates: item.agents_states } : undefined;
    case "file_change":
      return Object.hasOwn(item, "changes") ? { changes: item.changes } : undefined;
    case "web_search":
      return item.results;
    case "todo_list":
      return Object.hasOwn(item, "items") ? { items: item.items } : undefined;
    default:
      return item.result;
  }
}

/**
 * @param {Array<any>} records
 * @param {string|null} [model]
 * @returns {import("./types/agent_session").AgentSession}
 */
function normalizeCodexSession(records, model = null) {
  /** @type {import("./types/agent_session").AgentSession} */
  const events = [];
  if (!Array.isArray(records)) return events;
  const starts = new Map();
  const completedTurns = new Set();
  const completedItems = new Set();
  const completedErrors = new Set();
  const messageSnapshots = new Map();
  const parents = new Map();
  const agents = new Map();
  const childUsage = new Map();
  for (const record of records) {
    if (isSessionEvent(record)) continue;
    if (typeof record?.thread_id === "string" && typeof record.parent_thread_id === "string") parents.set(record.thread_id, record.parent_thread_id);
    const item = record?.item;
    if (item?.type === "collab_tool_call" && item.tool === "spawn_agent" && typeof item.sender_thread_id === "string" && Array.isArray(item.receiver_thread_ids)) {
      for (const id of item.receiver_thread_ids) if (typeof id === "string") parents.set(id, item.sender_thread_id);
    }
  }
  // item.updated carries a full snapshot, not a text delta. Retain the transport
  // observations, but expose only the latest available text for each native item.
  let thread = 0;
  let activeId;
  const threads = new Map();
  const scopes = records.map(record => {
    if (record?.type === "thread.started" && !isSessionEvent(record)) {
      thread++;
      activeId = record.thread_id;
      threads.set(activeId, thread);
    }
    const sessionId = record?.thread_id ?? record?.item?.sender_thread_id ?? record?.session_id ?? activeId;
    return { sessionId, key: JSON.stringify([sessionId, threads.get(sessionId) ?? 0, record?.agentId, record?.parent_tool_use_id]) };
  });
  const itemKey = (record, index) => (record.item?.id === undefined ? undefined : JSON.stringify([scopes[index].key, record.item.type, record.item.id]));
  records.forEach((record, index) => {
    if (["item.started", "item.updated", "item.completed"].includes(record?.type) && ["agent_message", "reasoning", "user_message"].includes(record.item?.type) && itemText(record.item) !== undefined) {
      const key = itemKey(record, index);
      if (key !== undefined) messageSnapshots.set(key, index);
    }
  });
  /** @type {Record<string, any>} */
  const rootUsage = { usage: {}, turns: 0 };
  for (const [index, record] of records.entries()) {
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    if (isSessionEvent(record)) {
      events.push(structuredClone(record));
      if (record.type === "session.result" && record.data.parentSessionId === undefined) {
        if (record.data.usage) Object.assign(rootUsage.usage, normalizeSessionUsage(record.data.usage));
        if (isTokenCount(record.data.numTurns)) rootUsage.turns = record.data.numTurns;
      }
      continue;
    }
    const { sessionId, key: scope } = scopes[index];
    const parentSessionId = parents.get(sessionId);
    const identity = { sessionId, parentSessionId };
    const agentId = record.agentId ?? (parentSessionId !== undefined ? sessionId : undefined);
    const emit = (type, data) => events.push(createSessionEvent({ ...record, ...(agentId !== undefined ? { agentId } : {}) }, type, { ...identity, ...data }));
    if (parentSessionId !== undefined && !childUsage.has(scope)) childUsage.set(scope, { usage: {}, turns: 0 });
    const accounting = parentSessionId === undefined ? rootUsage : childUsage.get(scope);
    const usage = accounting.usage;
    if (record.type === "thread.started" && Object.hasOwn(record, "thread_id")) {
      emit("session.init", {
        sourceEngine: "codex",
        sessionId: record.thread_id,
        model: Object.hasOwn(record, "model") ? record.model : parentSessionId === undefined ? (model ?? undefined) : undefined,
        cwd: record.cwd,
        tools: record.tools,
        mcpServers: record.mcp_servers,
        slashCommands: record.slash_commands,
        modelInfo: record.model_info,
      });
    } else if (record.type === "turn.started") {
      emit("turn.started", {});
    } else if (record.type === "turn.completed") {
      const id = record.turn_id ?? record.id;
      const key = id === undefined ? undefined : JSON.stringify([scope, id]);
      if (key !== undefined && completedTurns.has(key)) continue;
      if (key !== undefined) completedTurns.add(key);
      accounting.turns++;
      const report = record.usage;
      if (report && typeof report === "object" && !Array.isArray(report)) {
        const contribution = { ...report };
        if (!Object.hasOwn(report, "cache_read_input_tokens") && Object.hasOwn(report, "cached_input_tokens")) contribution.cache_read_input_tokens = report.cached_input_tokens;
        if (!Object.hasOwn(report, "cache_creation_input_tokens") && Object.hasOwn(report, "cache_write_input_tokens")) contribution.cache_creation_input_tokens = report.cache_write_input_tokens;
        accumulateSessionUsage(usage, contribution);
        for (const [field, value] of Object.entries(report)) {
          if (["cached_input_tokens", "cache_write_input_tokens", "reasoning_output_tokens", "total_tokens"].includes(field)) {
            if (isTokenCount(value)) usage[field] = (usage[field] ?? 0) + value;
          } else if (!["input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "inputTokens", "outputTokens", "cacheReadInputTokens", "cacheCreationInputTokens"].includes(field))
            usage[field] = structuredClone(value);
        }
        for (const [alias, field] of Object.entries({ inputTokens: "input_tokens", outputTokens: "output_tokens", cacheReadInputTokens: "cache_read_input_tokens", cacheCreationInputTokens: "cache_creation_input_tokens" })) {
          if ((Object.hasOwn(report, alias) || Object.hasOwn(usage, alias)) && isTokenCount(usage[field])) usage[alias] = usage[field];
        }
      }
      emit("session.result", { sourceEngine: "codex", sourceType: "turn.completed", status: "completed", numTurns: accounting.turns, usage: Object.keys(usage).length ? { ...usage } : undefined });
    } else if (record.type === "turn.failed" || record.type === "error") {
      const id = record.id;
      const key = id === undefined ? undefined : JSON.stringify([scope, record.type, id]);
      if (key !== undefined && completedErrors.has(key)) continue;
      if (key !== undefined) completedErrors.add(key);
      const error = Object.hasOwn(record, "error") ? record.error : record.message;
      emit("session.result", { sourceEngine: "codex", sourceType: record.type, status: record.type === "turn.failed" ? "failed" : undefined, errors: error === undefined ? undefined : [error] });
    } else if (["item.started", "item.updated", "item.completed"].includes(record.type) && record.item && typeof record.item === "object") {
      const item = record.item;
      const completed = record.type === "item.completed";
      const id = item.tool_call_id ?? item.id;
      const key = id === undefined ? undefined : JSON.stringify([scope, item.type, id]);
      const previous = key === undefined ? undefined : starts.get(key);
      const invocationFields = toolInvocation(item, previous);
      if (invocationFields) {
        /** @type {Record<string, any>} */
        const start = { toolCallId: id, ...invocationFields };
        const invocation = record.type === "item.started" || (start.input !== undefined && start.toolName !== undefined);
        if (!previous && invocation) {
          emit("tool.execution_start", { ...item, ...start });
          if (key !== undefined) starts.set(key, start);
        } else if (!completed) {
          emit("codex.item_snapshot", { item });
        }
        if (previous && key !== undefined) starts.set(key, start);
        if (completed && item.status !== "in_progress" && item.status !== "pending") {
          if (key !== undefined && completedItems.has(key)) {
            emit("codex.item_snapshot", { item });
          } else {
            if (key !== undefined) completedItems.add(key);
            const failed = sessionToolSuccess(item) === false || sessionToolSuccess(item.result ?? {}) === false || item.status === "declined" || (typeof item.exit_code === "number" && item.exit_code !== 0);
            const success = failed ? false : ["completed", "succeeded", "success"].includes(item.status) || item.exit_code === 0 ? true : undefined;
            emit("tool.execution_complete", {
              ...item,
              ...start,
              success,
              output: toolOutput(item),
              result: item.result,
              error: item.error,
              exitCode: item.exit_code,
              durationMs: isMetric(item.duration_ms) ? item.duration_ms : undefined,
            });
          }
        } else if (completed) {
          emit("codex.item_snapshot", { item });
        }
        if (item.type === "collab_tool_call" && item.tool === "spawn_agent" && item.status === "completed" && Array.isArray(item.receiver_thread_ids)) {
          for (const childId of item.receiver_thread_ids) {
            if (typeof childId !== "string" || parents.get(childId) !== item.sender_thread_id) continue;
            const agentKey = childId;
            const previousState = agents.get(agentKey);
            if (previousState?.started) continue;
            events.push(createSessionEvent({ ...record, agentId: childId }, "subagent.started", { sessionId: childId, parentSessionId: item.sender_thread_id, toolCallId: id, parentId: item.sender_thread_id }));
            agents.set(agentKey, { ...previousState, started: true, toolCallId: id });
          }
        }
        if (item.type === "collab_tool_call" && item.agents_states && typeof item.agents_states === "object" && !Array.isArray(item.agents_states)) {
          for (const [childId, state] of Object.entries(item.agents_states)) {
            if (!state || typeof state !== "object" || Array.isArray(state)) continue;
            const agentKey = childId;
            const previousState = agents.get(agentKey);
            const childIdentity = { sessionId: childId, parentSessionId: parents.get(childId) };
            const childSource = { ...record, agentId: childId };
            const childEmit = (type, data) => {
              const event = createSessionEvent(childSource, type, { ...childIdentity, ...data });
              events.push(event);
              return event;
            };
            if (state.status !== previousState?.status) {
              if (state.status === "completed") childEmit("subagent.completed", { toolCallId: previousState?.toolCallId ?? id });
              else if (["errored", "not_found"].includes(state.status)) childEmit("subagent.failed", { toolCallId: previousState?.toolCallId ?? id, error: state.message });
            }
            let messageEvent = state.status === "completed" ? previousState?.messageEvent : undefined;
            if (state.status === "completed" && typeof state.message === "string" && (previousState?.status !== "completed" || previousState.message !== state.message)) {
              if (previousState?.status === "completed" && previousState.messageEvent) {
                previousState.messageEvent.type = "codex.agent_snapshot";
                previousState.messageEvent.data = { ...childIdentity, state: { status: previousState.status, message: previousState.message } };
              }
              messageEvent = childEmit("assistant.message", { content: state.message, parentToolCallId: id });
            }
            agents.set(agentKey, { ...previousState, ...state, messageEvent });
          }
        }
      } else if (item.type === "agent_message" || item.type === "reasoning" || item.type === "user_message") {
        const text = itemText(item);
        const snapshotKey = itemKey(record, index);
        if (text === undefined || (snapshotKey !== undefined && messageSnapshots.get(snapshotKey) !== index)) emit("codex.item_snapshot", { item });
        else
          emit(item.type === "reasoning" ? "assistant.reasoning" : item.type === "user_message" ? "user.message" : "assistant.message", {
            ...item,
            messageId: item.id,
            content: text,
            ...(record.type === "item.completed" ? {} : { partial: true }),
          });
      } else if (item.type === "error" && completed) {
        if (key !== undefined && completedErrors.has(key)) continue;
        if (key !== undefined) completedErrors.add(key);
        emit("session.result", { sourceEngine: "codex", errors: [Object.hasOwn(item, "message") ? item.message : item] });
      } else {
        emit("codex.item_snapshot", { item });
      }
    } else {
      events.push(structuredClone(record));
    }
  }
  return normalizeAgentSession(events, { sourceEngine: "codex" });
}

module.exports = { normalizeCodexSession, isCodexRecord };
