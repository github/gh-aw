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
  // item.updated carries a full snapshot, not a text delta. Retain the transport
  // observations, but expose only the latest available text for each native item.
  let thread = 0;
  const scopes = records.map(record => {
    if (record?.type === "thread.started" && !isSessionEvent(record)) thread++;
    return thread;
  });
  const itemKey = (record, index) => (record.item?.id === undefined ? undefined : JSON.stringify([scopes[index], record.item.type, record.item.id]));
  records.forEach((record, index) => {
    if (["item.started", "item.updated", "item.completed"].includes(record?.type) && ["agent_message", "reasoning", "user_message"].includes(record.item?.type) && itemText(record.item) !== undefined) {
      const key = itemKey(record, index);
      if (key !== undefined) messageSnapshots.set(key, index);
    }
  });
  /** @type {Record<string, any>} */
  const usage = {};
  let turns = 0;
  for (const [index, record] of records.entries()) {
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    if (isSessionEvent(record)) {
      events.push(structuredClone(record));
      if (record.type === "session.result") {
        if (record.data.usage) Object.assign(usage, normalizeSessionUsage(record.data.usage));
        if (isTokenCount(record.data.numTurns)) turns = record.data.numTurns;
      }
      continue;
    }
    const emit = (type, data) => events.push(createSessionEvent(record, type, data));
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
      });
    } else if (record.type === "turn.started") {
      emit("turn.started", {});
    } else if (record.type === "turn.completed") {
      const id = record.turn_id ?? record.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index], id]);
      if (key !== undefined && completedTurns.has(key)) continue;
      if (key !== undefined) completedTurns.add(key);
      turns++;
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
      emit("session.result", { sourceType: "turn.completed", status: "completed", numTurns: turns, usage: Object.keys(usage).length ? { ...usage } : undefined });
    } else if (record.type === "turn.failed" || record.type === "error") {
      const id = record.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index], record.type, id]);
      if (key !== undefined && completedErrors.has(key)) continue;
      if (key !== undefined) completedErrors.add(key);
      const error = Object.hasOwn(record, "error") ? record.error : record.message;
      emit("session.result", { sourceType: record.type, status: record.type === "turn.failed" ? "failed" : undefined, errors: error === undefined ? undefined : [error] });
    } else if (["item.started", "item.updated", "item.completed"].includes(record.type) && record.item && typeof record.item === "object") {
      const item = record.item;
      const completed = record.type === "item.completed";
      const id = item.tool_call_id ?? item.id;
      const key = id === undefined ? undefined : JSON.stringify([scopes[index], item.type, id]);
      if (item.type === "mcp_tool_call" || item.type === "command_execution") {
        const command = item.type === "command_execution";
        const previous = key === undefined ? undefined : starts.get(key);
        const start = {
          toolCallId: id,
          toolName: command ? "bash" : Object.hasOwn(item, "tool") ? item.tool : previous?.toolName,
          mcpServerName: command ? undefined : Object.hasOwn(item, "server") ? item.server : previous?.mcpServerName,
          input: command ? (Object.hasOwn(item, "command") ? { command: item.command } : undefined) : item.arguments,
          command: command ? item.command : undefined,
        };
        const invocation = record.type === "item.started" || (command ? Object.hasOwn(item, "command") : Object.hasOwn(item, "tool") && Object.hasOwn(item, "arguments"));
        if (!previous && invocation) {
          emit("tool.execution_start", { ...item, ...start });
          if (key !== undefined) starts.set(key, start);
        } else if (!completed) {
          emit("codex.item_snapshot", { item });
        }
        if (completed && item.status !== "in_progress" && item.status !== "pending") {
          if (key !== undefined && completedItems.has(key)) {
            emit("codex.item_snapshot", { item });
            continue;
          }
          if (key !== undefined) completedItems.add(key);
          const failed = sessionToolSuccess(item) === false || sessionToolSuccess(item.result ?? {}) === false || (typeof item.exit_code === "number" && item.exit_code !== 0);
          const success = failed ? false : ["completed", "succeeded", "success"].includes(item.status) || item.exit_code === 0 ? true : undefined;
          emit("tool.execution_complete", {
            ...item,
            ...start,
            success,
            output: command ? item.aggregated_output : item.result,
            result: item.result,
            error: item.error,
            exitCode: item.exit_code,
            durationMs: isMetric(item.duration_ms) ? item.duration_ms : undefined,
          });
        } else if (completed) {
          emit("codex.item_snapshot", { item });
        }
      } else if (item.type === "agent_message" || item.type === "reasoning" || item.type === "user_message") {
        const text = itemText(item);
        const snapshotKey = itemKey(record, index);
        if (text === undefined || (snapshotKey !== undefined && messageSnapshots.get(snapshotKey) !== index)) emit("codex.item_snapshot", { item });
        else emit(item.type === "reasoning" ? "assistant.reasoning" : item.type === "user_message" ? "user.message" : "assistant.message", { ...item, content: text });
      } else if (item.type === "error" && completed) {
        if (key !== undefined && completedErrors.has(key)) continue;
        if (key !== undefined) completedErrors.add(key);
        emit("session.result", { errors: [Object.hasOwn(item, "message") ? item.message : item] });
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
