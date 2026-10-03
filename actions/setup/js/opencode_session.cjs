// @ts-check

const { createSessionEvent, isSessionEvent, isMetric, isTokenCount, accumulateSessionUsage, sessionToolSuccess } = require("./agent_session.cjs");

/** @param {any} value @returns {boolean} */
function isObject(value) {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

/** @param {any} record @returns {boolean} */
function isOpenCodeRecord(record) {
  if (!isObject(record)) return false;
  if (isSessionEvent(record)) return true;
  const partTypes = { text: "text", reasoning: "reasoning", tool_use: "tool", step_start: "step-start", step_finish: "step-finish" };
  if (Object.hasOwn(partTypes, record.type)) return isObject(record.part) && record.part.type === partTypes[record.type];
  if (record.type === "error") return Object.hasOwn(record, "sessionID") && Object.hasOwn(record, "error");
  if (!isObject(record.properties)) return false;
  if (record.type === "session.created") return isObject(record.properties.info) && Object.hasOwn(record.properties.info, "id");
  if (record.type === "message.updated") return ["user", "assistant"].includes(record.properties.info?.role) && Object.hasOwn(record.properties.info, "id");
  if (record.type === "message.part.updated") return isObject(record.properties.part) && typeof record.properties.part.type === "string";
  if (record.type === "message.part.delta") return typeof record.properties.delta === "string" && typeof record.properties.field === "string" && Object.hasOwn(record.properties, "partID");
  return record.type === "session.error" && Object.hasOwn(record.properties, "error");
}

/** @param {any} tokens @returns {Record<string, any> | undefined} */
function openCodeUsage(tokens) {
  if (!isObject(tokens)) return undefined;
  /** @type {Record<string, any>} */
  const usage = {};
  for (const [key, value] of Object.entries({
    input_tokens: tokens.input,
    output_tokens: tokens.output,
    reasoning_output_tokens: tokens.reasoning,
    total_tokens: tokens.total,
    cache_read_input_tokens: tokens.cache?.read,
    cache_creation_input_tokens: tokens.cache?.write,
  })) {
    if (isTokenCount(value)) usage[key] = value;
  }
  // OpenCode v1.2.14 getUsage subtracts cache reads/writes from input.
  if (Object.hasOwn(usage, "input_tokens")) usage.input_tokens_include_cache = false;
  return Object.keys(usage).length ? usage : undefined;
}

/** @param {any} time @returns {number | undefined} */
function observedDuration(time) {
  return isMetric(time?.start) && isMetric(time?.end) && time.end >= time.start ? time.end - time.start : undefined;
}

/** @param {Map<any, any>} session @returns {Record<string, any>} */
function aggregateAccounting(session) {
  /** @type {Record<string, any>} */
  const usage = {};
  const extraOverflow = new Set();
  let cost;
  let costOverflow = false;
  for (const message of session.values()) {
    for (const field of ["input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "total_tokens", "reasoning_output_tokens", "cost"]) {
      const snapshot = message.snapshots.get(field);
      const steps = [...message.steps.values()];
      // Native message.tokens is the LAST step, whereas message.cost is cumulative.
      // Prefer observed step token reports; use message tokens only as a fallback.
      const values = field === "cost" ? steps.filter(step => !snapshot || step.index > snapshot.index).map(step => step.cost) : steps.map(step => step.usage?.[field]).filter(isTokenCount);
      if (snapshot && (field === "cost" || !values.length)) values.unshift(snapshot.value);
      for (const value of values) {
        if (field === "cost") {
          if (!isMetric(value) || costOverflow) continue;
          const total = (cost ?? 0) + value;
          if (isMetric(total)) cost = total;
          else {
            cost = undefined;
            costOverflow = true;
          }
        } else if (["total_tokens", "reasoning_output_tokens"].includes(field)) {
          if (!isTokenCount(value) || extraOverflow.has(field)) continue;
          const total = (usage[field] ?? 0) + value;
          if (isTokenCount(total)) usage[field] = total;
          else {
            delete usage[field];
            extraOverflow.add(field);
          }
        } else accumulateSessionUsage(usage, { [field]: value });
      }
    }
  }
  if (Object.hasOwn(usage, "input_tokens")) usage.input_tokens_include_cache = false;
  if (extraOverflow.size) usage.opencodeOverflowedTokens = [...extraOverflow];
  return { usage: Object.keys(usage).length ? usage : undefined, totalCostUsd: cost, ...(costOverflow ? { costOverflow: true } : {}) };
}

/**
 * CLI run --format json parts and native SDK events have distinct envelopes.
 * Preserve those envelopes while mapping only their observed protocol payloads.
 * @param {any[]} records
 * @returns {import("./types/agent_session").AgentSession}
 */
function normalizeOpenCodeSession(records) {
  /** @type {import("./types/agent_session").AgentSession} */
  const events = [];
  if (!Array.isArray(records)) return events;
  const roles = new Map();
  const parts = new Map();
  const texts = new Map();
  const starts = new Set();
  const completions = new Map();
  const accounting = new Map();
  const accountingSources = new Map();
  const key = (session, message, part) => JSON.stringify([session, message, part]);
  for (const record of records) {
    if (!isOpenCodeRecord(record) || isSessionEvent(record)) continue;
    const info = record.properties?.info;
    if (record.type === "message.updated") roles.set(key(info.sessionID, info.id, undefined), info.role);
    const part = record.part ?? record.properties?.part;
    if (part) parts.set(key(part.sessionID ?? record.sessionID, part.messageID, part.id), part);
  }

  for (const [index, record] of records.entries()) {
    if (!isOpenCodeRecord(record)) continue;
    if (isSessionEvent(record)) {
      events.push(structuredClone(record));
      continue;
    }
    const emit = (type, data) => events.push(createSessionEvent(record, type, data));
    const properties = record.properties;
    const part = record.part ?? properties?.part;
    const sessionID = record.sessionID ?? part?.sessionID ?? properties?.info?.sessionID ?? properties?.sessionID;
    const messageID = part?.messageID ?? properties?.info?.id;
    const partKey = part?.id === undefined ? undefined : key(sessionID, messageID, part.id);
    const extension = (type, data) => emit(type, data);

    const account = (report, snapshot) => {
      let session = accounting.get(sessionID);
      if (!session) accounting.set(sessionID, (session = new Map()));
      const messageKey = messageID ?? (snapshot ? index : undefined);
      let message = session.get(messageKey);
      if (!message) session.set(messageKey, (message = { steps: new Map(), snapshots: new Map() }));
      const contribution = { index, usage: openCodeUsage(report.tokens), cost: isMetric(report.cost) ? report.cost : undefined };
      if (!contribution.usage && contribution.cost === undefined) return;
      accountingSources.set(sessionID, { record, index });
      if (snapshot) {
        for (const [field, value] of Object.entries(contribution.usage ?? {})) {
          if (isTokenCount(value)) message.snapshots.set(field, { value, index });
        }
        if (contribution.cost !== undefined) message.snapshots.set("cost", { value: contribution.cost, index });
      } else {
        const stepID = part?.id ?? index;
        contribution.index = message.steps.get(stepID)?.index ?? index;
        message.steps.set(stepID, contribution);
      }
    };

    if (record.type === "session.created") {
      const info = properties.info;
      emit("session.init", { ...info, sourceEngine: "opencode", sessionId: info.id, cwd: info.directory });
    } else if (record.type === "error" || record.type === "session.error") {
      emit("session.result", { sessionId: sessionID, errors: [record.type === "error" ? record.error : properties.error] });
    } else if (record.type === "message.updated") {
      const info = properties.info;
      extension("opencode.message_snapshot", { ...properties });
      if (info.role === "assistant") {
        account(info, true);
        if (Object.hasOwn(info, "error")) emit("session.result", { sessionId: sessionID, errors: [info.error] });
      }
    } else if (record.type === "message.part.delta") {
      const delta = properties;
      const deltaKey = key(delta.sessionID, delta.messageID, delta.partID);
      const known = parts.get(deltaKey);
      const role = roles.get(key(delta.sessionID, delta.messageID, undefined));
      if (delta.field !== "text" || !["text", "reasoning"].includes(known?.type) || !["user", "assistant"].includes(role)) {
        extension("opencode.part_delta", { ...delta });
        continue;
      }
      emit(known.type === "reasoning" ? "assistant.reasoning" : role === "user" ? "user.message" : "assistant.message", { ...delta, content: delta.delta, delta: true });
      texts.set(deltaKey, (texts.get(deltaKey) ?? "") + delta.delta);
    } else if (part?.type === "text" || part?.type === "reasoning") {
      const role = record.part ? "assistant" : roles.get(key(sessionID, messageID, undefined));
      if (typeof part.text !== "string" || !["user", "assistant"].includes(role)) {
        extension("opencode.part_snapshot", { part });
        continue;
      }
      const previous = partKey === undefined ? undefined : texts.get(partKey);
      if (previous !== undefined && (part.text === previous || !part.text.startsWith(previous))) {
        extension("opencode.part_snapshot", { part });
      } else {
        emit(part.type === "reasoning" ? "assistant.reasoning" : role === "user" ? "user.message" : "assistant.message", { ...part, content: previous === undefined ? part.text : part.text.slice(previous.length) });
        if (partKey !== undefined) texts.set(partKey, part.text);
      }
    } else if (part?.type === "tool" && isObject(part.state)) {
      const state = part.state;
      const toolKey = part.callID === undefined ? partKey : key(sessionID, messageID, part.callID);
      const data = { ...part, ...state, toolCallId: part.callID, toolName: part.tool, input: state.input };
      const terminal = ["completed", "error"].includes(state.status);
      if (["pending", "running", "completed", "error"].includes(state.status) && Object.hasOwn(state, "input") && (toolKey === undefined || !starts.has(toolKey))) {
        emit("tool.execution_start", data);
        if (toolKey !== undefined) starts.add(toolKey);
      } else if (!terminal) {
        extension("opencode.part_snapshot", { part });
      }
      const completion = JSON.stringify(part);
      if (terminal && (toolKey === undefined || completions.get(toolKey) !== completion)) {
        emit("tool.execution_complete", {
          ...data,
          output: state.output,
          error: Object.hasOwn(state, "error") ? state.error : part.error,
          success:
            sessionToolSuccess(part) === false ||
            sessionToolSuccess(state) === false ||
            sessionToolSuccess(state.output ?? {}) === false ||
            sessionToolSuccess(state.metadata ?? {}) === false ||
            (typeof state.metadata?.exit === "number" && state.metadata.exit !== 0)
              ? false
              : state.status === "completed",
          exitCode: Object.hasOwn(state, "exitCode") ? state.exitCode : state.metadata?.exit,
          durationMs: observedDuration(state.time),
        });
        if (toolKey !== undefined) completions.set(toolKey, completion);
      } else if (terminal) extension("opencode.part_snapshot", { part });
    } else if (part?.type === "step-finish") {
      extension("opencode.step_finish", { part });
      account(part, false);
    } else if (part?.type === "step-start") {
      extension("opencode.step_start", { part });
    } else if (part) {
      extension("opencode.part_snapshot", { part });
    }
  }
  // Emit one derived total after the native observations, not overlapping
  // intermediate totals that could leave stale metrics after an overflow.
  for (const [sessionID, source] of [...accountingSources.entries()].sort((left, right) => left[1].index - right[1].index)) {
    events.push(createSessionEvent(source.record, "session.result", { sessionId: sessionID, ...aggregateAccounting(accounting.get(sessionID)) }));
  }
  return events;
}

module.exports = { normalizeOpenCodeSession, isOpenCodeRecord, openCodeUsage };
