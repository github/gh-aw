// @ts-check

const { createSessionEvent, normalizeAgentSession, isSessionEvent, accumulateSessionUsage, normalizeSessionUsage, reconcileSessionUsage, isTokenCount, isMetric, sessionToolSuccess, sessionContext } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

const TOOL_ITEMS = new Set(["mcp_tool_call", "command_execution", "file_change", "web_search", "collab_tool_call"]);
const TOKEN_ALIASES = {
  inputTokens: "input_tokens",
  outputTokens: "output_tokens",
  totalTokens: "total_tokens",
  reasoningOutputTokens: "reasoning_output_tokens",
  cacheReadInputTokens: "cache_read_input_tokens",
  cacheCreationInputTokens: "cache_creation_input_tokens",
  cached_input_tokens: "cache_read_input_tokens",
  cache_write_input_tokens: "cache_creation_input_tokens",
};
const TOKEN_FIELDS = new Set([...Object.keys(TOKEN_ALIASES), ...Object.values(TOKEN_ALIASES), "overflowed_tokens"]);

/** @param {any} record @returns {boolean} */
function isCodexRecord(record) {
  if (!record || typeof record !== "object" || Array.isArray(record)) return false;
  return (
    isSessionEvent(record) ||
    (record.type === "thread.started" && Object.hasOwn(record, "thread_id")) ||
    ["turn.started", "turn.completed", "turn.failed"].includes(record.type) ||
    (record.type === "error" && (Object.hasOwn(record, "message") || Object.hasOwn(record, "error"))) ||
    (["item.started", "item.updated", "item.completed"].includes(record.type) && !!record.item && typeof record.item === "object" && !Array.isArray(record.item) && typeof record.item.type === "string" && record.item.type.length > 0)
  );
}

/** @param {any} item @returns {import("./types/agent_session").JsonValue|undefined} */
function itemContent(item) {
  return typeof item?.text === "string" ? item.text : typeof item?.summary === "string" ? item.summary : item?.content;
}

/** @param {any} report @returns {Record<string, any>|undefined} */
function codexUsage(report) {
  if (!report || typeof report !== "object" || Array.isArray(report)) return undefined;
  const contribution = { ...report };
  for (const [alias, field] of Object.entries({ cached_input_tokens: "cache_read_input_tokens", cache_write_input_tokens: "cache_creation_input_tokens" })) {
    if (!Object.hasOwn(report, field) && Object.hasOwn(report, alias)) contribution[field] = report[alias];
  }
  return normalizeSessionUsage(contribution);
}

/** @param {any} item @param {any} previous */
function toolObservation(item, previous) {
  const command = item.type === "command_execution";
  const mcp = item.type === "mcp_tool_call";
  const web = item.type === "web_search";
  const collab = item.type === "collab_tool_call";
  const fields = web ? ["query", "action"] : collab ? ["receiver_thread_ids", "prompt"] : [];
  const parameters = Object.fromEntries(fields.filter(field => Object.hasOwn(item, field)).map(field => [field, item[field]]));
  const hasInvocation = command
    ? Object.hasOwn(item, "command")
    : mcp
      ? Object.hasOwn(item, "tool") && Object.hasOwn(item, "arguments")
      : web
        ? Object.keys(parameters).length > 0
        : collab && Object.hasOwn(item, "tool") && Object.keys(parameters).length > 0;
  const input = command
    ? Object.hasOwn(item, "command")
      ? { command: item.command }
      : previous?.input
    : mcp
      ? Object.hasOwn(item, "arguments")
        ? item.arguments
        : previous?.input
      : Object.keys(parameters).length
        ? parameters
        : previous?.input;
  return {
    hasInvocation,
    start: {
      toolCallId: item.tool_call_id ?? item.id,
      toolName: command ? "bash" : web ? "web_search" : item.type === "file_change" ? "apply_patch" : Object.hasOwn(item, "tool") ? item.tool : previous?.toolName,
      mcpServerName: mcp ? (Object.hasOwn(item, "server") ? item.server : previous?.mcpServerName) : undefined,
      input,
      command: command ? (Object.hasOwn(item, "command") ? item.command : previous?.command) : undefined,
    },
    output: command ? item.aggregated_output : mcp ? item.result : web ? item.results : collab ? item.agents_states : Object.hasOwn(item, "changes") ? { changes: item.changes } : undefined,
  };
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
  const completedErrors = new Map();
  const messageSnapshots = new Map();
  // item.updated carries a full snapshot, not a text delta. Retain the transport
  // observations, but expose only the latest available text for each native item.
  let thread = 0;
  let sessionId;
  const scopes = records.map(record => {
    if (record?.type === "thread.started" && !isSessionEvent(record) && Object.hasOwn(record, "thread_id")) {
      thread++;
      sessionId = record.thread_id;
    } else if (record?.type === "session.init" && isSessionEvent(record)) {
      thread++;
      sessionId = record.data.sessionId;
    }
    return { thread, sessionId };
  });
  const itemKey = (record, index) => (record.item?.id === undefined ? undefined : JSON.stringify([scopes[index].thread, record.item.type, record.item.id]));
  records.forEach((record, index) => {
    if (
      ["item.started", "item.updated", "item.completed"].includes(record?.type) &&
      ["agent_message", "reasoning", "user_message"].includes(record.item?.type) &&
      (itemContent(record.item) !== undefined || (record.item.type === "agent_message" && getMessageRefusal(record.item)))
    ) {
      const key = itemKey(record, index);
      if (key !== undefined) messageSnapshots.set(key, index);
    }
  });
  /** @type {Record<string, any>} */
  let usage = {};
  let turns = 0;
  const observeResult = event => {
    if (event.type !== "session.result") return;
    if (event.data.usage) usage = reconcileSessionUsage(usage, codexUsage(event.data.usage)) ?? usage;
    if (isTokenCount(event.data.numTurns)) turns = event.data.numTurns;
  };
  for (const [index, record] of records.entries()) {
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    if (isSessionEvent(record)) {
      events.push(structuredClone(record));
      observeResult(record);
      continue;
    }
    const emit = (type, data) => {
      const context = type === "tool.execution_start" || type === "tool.execution_complete" ? { sessionId: scopes[index].sessionId, ...sessionContext(record) } : {};
      events.push(createSessionEvent(record, type, { ...context, ...data }));
    };
    if (record.type === "thread.started" && Object.hasOwn(record, "thread_id")) {
      emit("session.init", {
        sourceEngine: "codex",
        sessionId: record.thread_id,
        model: Object.hasOwn(record, "model") ? record.model : (model ?? undefined),
        cwd: record.cwd,
        tools: record.tools,
        mcpServers: record.mcp_servers,
        slashCommands: record.slash_commands,
        modelInfo: record.model_info,
        reasoningEffort: record.reasoning_effort,
      });
    } else if (record.type === "turn.started") {
      emit("turn.started", {});
    } else if (record.type === "turn.completed") {
      const id = record.turn_id ?? record.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index].thread, id]);
      if (key !== undefined && completedTurns.has(key)) continue;
      if (key !== undefined) completedTurns.add(key);
      turns++;
      const report = codexUsage(record.usage);
      if (report) {
        accumulateSessionUsage(usage, report);
        for (const [field, value] of Object.entries(report)) {
          if (!TOKEN_FIELDS.has(field)) usage[field] = structuredClone(value);
        }
        for (const [alias, field] of Object.entries(TOKEN_ALIASES)) {
          if (Object.hasOwn(report, alias) || Object.hasOwn(usage, alias)) {
            if (isTokenCount(usage[field])) usage[alias] = usage[field];
            else delete usage[alias];
          }
        }
      }
      emit("session.result", { sourceType: "turn.completed", status: "completed", numTurns: turns, usage: Object.keys(usage).length ? { ...usage } : undefined });
    } else if (record.type === "turn.failed" || record.type === "error") {
      const id = record.type === "turn.failed" ? (record.turn_id ?? record.id) : record.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index].thread, "diagnostic", record.type, id]);
      const error = Object.hasOwn(record, "error") ? record.error : record.message;
      const signature = JSON.stringify(error);
      if (key !== undefined && completedErrors.has(key) && completedErrors.get(key) === signature) continue;
      if (key !== undefined) completedErrors.set(key, signature);
      emit("session.result", { sourceType: record.type, status: record.type === "turn.failed" ? "failed" : undefined, errors: error === undefined ? undefined : [error] });
    } else if (["item.started", "item.updated", "item.completed"].includes(record.type) && isCodexRecord(record)) {
      const item = record.item;
      const completed = record.type === "item.completed";
      const id = item.tool_call_id ?? item.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index].thread, "item", item.type, id]);
      if (TOOL_ITEMS.has(item.type)) {
        const previous = key === undefined ? undefined : starts.get(key);
        const { start, output, hasInvocation } = toolObservation(item, previous);
        const invocation = record.type === "item.started" || hasInvocation;
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
            continue;
          }
          if (key !== undefined) completedItems.add(key);
          const failed = sessionToolSuccess(item) === false || sessionToolSuccess(item.result ?? {}) === false || item.status === "declined" || (typeof item.exit_code === "number" && item.exit_code !== 0);
          const success = failed ? false : ["completed", "succeeded", "success"].includes(item.status) || item.exit_code === 0 || (item.type === "web_search" && item.status === undefined) ? true : undefined;
          emit("tool.execution_complete", {
            ...item,
            ...start,
            success,
            output,
            result: item.result,
            error: item.error,
            exitCode: item.exit_code,
            durationMs: isMetric(item.duration_ms) ? item.duration_ms : undefined,
          });
        } else if (completed) {
          emit("codex.item_snapshot", { item });
        }
      } else if (item.type === "agent_message" || item.type === "reasoning" || item.type === "user_message") {
        const content = itemContent(item);
        const refusal = item.type === "agent_message" ? getMessageRefusal(item) : undefined;
        const snapshotKey = itemKey(record, index);
        if ((content === undefined && !refusal) || (snapshotKey !== undefined && messageSnapshots.get(snapshotKey) !== index)) emit("codex.item_snapshot", { item });
        else
          emit(refusal ? "assistant.refusal" : item.type === "reasoning" ? "assistant.reasoning" : item.type === "user_message" ? "user.message" : "assistant.message", {
            ...item,
            messageId: item.id,
            content,
            ...(refusal ?? {}),
            ...(completed ? {} : { partial: true }),
          });
      } else if (item.type === "error" && (completed || Object.hasOwn(item, "message"))) {
        const error = Object.hasOwn(item, "message") ? item.message : item;
        const signature = JSON.stringify(error);
        if (key !== undefined && completedErrors.has(key) && completedErrors.get(key) === signature) continue;
        if (key !== undefined) completedErrors.set(key, signature);
        emit("session.result", { errors: [error] });
      } else {
        emit("codex.item_snapshot", { item });
      }
    } else {
      events.push(structuredClone(record));
      normalizeAgentSession([record], { sourceEngine: "codex" }).forEach(observeResult);
    }
  }
  return normalizeAgentSession(events, { sourceEngine: "codex" });
}

module.exports = { normalizeCodexSession, isCodexRecord };
