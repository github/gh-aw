// @ts-check

const {
  isSessionEvent,
  createSessionEvent,
  normalizeAgentSession,
  normalizeSessionUsage,
  accumulateSessionUsage,
  reconcileSessionUsage,
  isMetric,
  isTokenCount,
  projectSessionResult,
  sessionContext,
  sessionScopeKey,
} = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");
const { DYNAMIC_WORKFLOW_EVENT_TYPES } = require("./dynamic_workflow_session.cjs");

/**
 * Claude input tokens exclude cache reads and cache writes.
 * @param {any} usage
 * @returns {Record<string, any>|undefined}
 */
function claudeUsage(usage) {
  const normalized = normalizeSessionUsage(usage);
  if (!normalized) return undefined;
  const thinking = usage.output_tokens_details?.thinking_tokens;
  if (!Object.hasOwn(usage, "reasoning_output_tokens") && !Object.hasOwn(usage, "reasoningOutputTokens") && isTokenCount(thinking)) normalized.reasoning_output_tokens = thinking;
  return { ...normalized, input_tokens_include_cache: false };
}

/** @param {any} record @returns {Record<string, any>} */
function sourceFields(record) {
  const { type, data, ...fields } = record;
  return { ...fields, ...data };
}

/** @param {any} block @param {any} [start] */
function toolFields(block, start) {
  const mcp = typeof block.name === "string" ? /^mcp__(.+?)__(.+)$/.exec(block.name) : undefined;
  return {
    toolName: mcp?.[2] ?? block.name ?? start?.toolName,
    mcpServerName: block.mcpServerName ?? mcp?.[1] ?? start?.mcpServerName,
  };
}

/**
 * Preserve SDK transport observations alongside the core events they expose.
 * Streaming snapshots are not additional answers, and retries are not tool failures.
 * @param {Array<any>} records
 * @returns {import("./types/agent_session").SessionEvent[]}
 */
function normalizeClaudeSession(records) {
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const events = [];
  const tools = new Map();
  const streams = new Map();
  const streamedMessages = new Map();
  const responseUsage = new Map();
  const terminalResults = [];
  const denialReports = [];
  const agentTasks = new Map();
  const agentsByTool = new Map();

  const channelKey = sessionScopeKey;
  const messageKey = (source, id) => JSON.stringify([channelKey(source), id]);
  const toolKey = messageKey;
  const taskKey = (source, id) => JSON.stringify([sessionContext(source).sessionId, id]);
  const registerAgent = (source, id, toolId, fields) => {
    if (typeof id !== "string") return;
    const key = taskKey(source, id);
    const stored = agentTasks.get(key);
    const context = sessionContext(source);
    const observed = Object.fromEntries(Object.entries({ ...fields, ...context }).filter(([, value]) => value !== undefined));
    const task = { ...stored, ...observed, task_id: id, ...(typeof toolId === "string" ? { tool_use_id: toolId } : {}) };
    if (context.agentId === undefined && context.parentToolUseId !== undefined && context.parentToolUseId !== sessionContext({ data: stored }).parentToolUseId) delete task.agentId;
    agentTasks.set(key, task);
    if (typeof toolId !== "string") return;
    const tool = taskKey(source, toolId);
    const ids = agentsByTool.get(tool) ?? new Set();
    ids.add(id);
    agentsByTool.set(tool, ids);
  };
  for (const record of records) {
    if (record?.type === "system" && record.task_type === "local_agent") registerAgent(record, record.task_id, record.tool_use_id, sourceFields(record));
    else if (record?.type === "subagent.started" && record.data?.task_type === "local_agent") registerAgent(record, record.agentId, record.data.toolCallId, record.data);
    else if (typeof record?.tool_use_result?.agentId === "string") {
      const block = record.message?.content?.find?.(block => block?.type === "tool_result");
      registerAgent(record, record.tool_use_result.agentId, block?.tool_use_id, { model: record.tool_use_result.resolvedModel });
    }
  }
  const parentAgent = source => {
    const ids = agentsByTool.get(taskKey(source, sessionContext(source).parentToolUseId));
    return ids?.size === 1 ? [...ids][0] : undefined;
  };
  const emit = (source, type, data) => {
    const event = createSessionEvent(source, type, { ...data, ...sessionContext(source) });
    events.push(event);
    return event;
  };
  const native = (source, type) => emit(source, type, sourceFields(source));
  const emitSubagent = (source, type, task, fields) => {
    const stored = sessionContext({ data: task });
    const explicit = sessionContext(source);
    const context = { ...stored, ...explicit };
    // A different explicit parent invalidates the stored caller, including a root override.
    if (explicit.agentId === undefined && explicit.parentToolUseId !== undefined && explicit.parentToolUseId !== stored.parentToolUseId) delete context.agentId;
    const caller = context.agentId ?? parentAgent({ data: context });
    if (caller !== undefined) context.agentId = caller;
    const event = emit(source, type, { ...sourceFields(source), toolCallId: task.tool_use_id, ...fields, ...context });
    event.agentId = task.task_id;
    return event;
  };
  const reportUsage = (source, id, usage) => {
    const normalized = claudeUsage(usage);
    if (id === undefined || !normalized) return;
    const key = messageKey(source, id);
    responseUsage.set(key, { usage: { ...responseUsage.get(key)?.usage, ...normalized }, source });
  };
  const reportError = (source, error) => emit(source, "session.result", { ...sourceFields(source), sourceEngine: "claude", sourceType: source.type, errors: [error] });
  const retainSnapshot = (event, source) => {
    if (!event) return;
    (event.data.snapshots ??= []).push(structuredClone(source));
    if (event.data.model === undefined && source.message?.model !== undefined) event.data.model = source.message.model;
  };
  const emitRefusal = (source, state, refusal, partial) => {
    const { delta, ...identity } = streamFields(state);
    const data = { ...sourceFields(source), ...identity, ...refusal, ...(partial ? { partial: true } : {}) };
    if (state.refusalEvent) {
      retainSnapshot(state.refusalEvent, source);
      Object.assign(state.refusalEvent.data, refusal);
      if (!partial) delete state.refusalEvent.data.partial;
    } else {
      state.refusalEvent = emit(source, "assistant.refusal", data);
      if (!partial) retainSnapshot(state.refusalEvent, source);
    }
    if (state.textEvents.length) {
      (state.refusalEvent.data.observedTextEvents ??= []).push(...structuredClone(state.textEvents));
      for (const textEvent of state.textEvents) {
        const index = events.indexOf(textEvent);
        if (index !== -1) events.splice(index, 1);
      }
      state.textEvents = [];
    }
    return state.refusalEvent;
  };

  const streamFields = (state, index) => ({ delta: true, ...(state.id !== undefined ? { messageId: state.id } : {}), ...(state.model !== undefined ? { model: state.model } : {}), ...(index !== undefined ? { contentIndex: index } : {}) });
  const emitBlock = (source, block, metadata = {}) => {
    if (!block || typeof block !== "object") return;
    const data = { ...sourceFields(source), ...block, messageId: source.message?.id, model: source.message?.model, ...metadata };
    if (block.type === "text" && typeof block.text === "string") {
      return emit(source, source.type === "user" ? "user.message" : "assistant.message", { ...data, content: block.text });
    }
    if (block.type === "thinking" && typeof block.thinking === "string") {
      return emit(source, "assistant.reasoning", { ...data, content: block.thinking });
    }
    if (block.type === "redacted_thinking") {
      return emit(source, "assistant.reasoning", { ...data, content: block });
    }
    if (["image", "document"].includes(block.type)) {
      return emit(source, source.type === "user" ? "user.message" : "assistant.message", { ...data, content: block });
    }
    if (source.type === "assistant" && block.type === "refusal" && typeof block.refusal === "string") {
      return emit(source, "assistant.refusal", { ...data, reason: "refusal", content: block.refusal });
    }
    if (block.type === "tool_use") {
      const event = emit(source, "tool.execution_start", { ...data, ...toolFields(block), toolCallId: block.id, input: block.input });
      if (block.id !== undefined) tools.set(toolKey(source, block.id), event.data);
      return event;
    }
    if (block.type === "tool_result") {
      const start = block.tool_use_id !== undefined ? tools.get(toolKey(source, block.tool_use_id)) : undefined;
      const result = source.tool_use_result;
      const failed = block.is_error === true || block.isError === true || block.success === false || block.error != null || result?.is_error === true || result?.isError === true || result?.error != null || result?.interrupted === true;
      const success = failed ? false : block.is_error === false || block.isError === false ? true : typeof block.success === "boolean" ? block.success : undefined;
      const workflow = source.tool_use_result?.taskType === "local_workflow" ? source.tool_use_result : undefined;
      const agent = typeof source.tool_use_result?.agentId === "string" ? source.tool_use_result : undefined;
      const event = emit(source, "tool.execution_complete", {
        ...data,
        toolCallId: block.tool_use_id,
        ...toolFields(block, start),
        success,
        output: block.content,
        ...(failed && !Object.hasOwn(block, "is_error") ? { isError: true } : {}),
        ...(block.error === undefined && result?.error !== undefined ? { error: result.error } : {}),
        durationMs: isMetric(block.duration_ms) ? block.duration_ms : undefined,
        ...(workflow ? { taskId: workflow.taskId, taskType: workflow.taskType, workflowName: workflow.workflowName, workflowRunId: workflow.runId, status: workflow.status } : {}),
        ...(agent ? { taskId: agent.agentId, taskType: "local_agent", status: agent.status } : {}),
      });
      if (agent && typeof agent.resolvedModel === "string") emitSubagent(source, "subagent.configured", agentTasks.get(taskKey(source, agent.agentId)), { model: agent.resolvedModel });
      return event;
    }
    return emit(source, "claude.content_block", data);
  };

  for (const record of records) {
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    let source = structuredClone(record);
    if (isSessionEvent(source)) {
      const mapped = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
      events.push(mapped);
      if (mapped.type === "session.result") terminalResults.push(mapped);
      if (mapped.type === "tool.execution_start" && mapped.data.toolCallId !== undefined) tools.set(toolKey(mapped, mapped.data.toolCallId), mapped.data);
      continue;
    }
    const agentId = parentAgent(source);
    if (source.agentId === undefined && agentId !== undefined) source.agentId = agentId;
    if (["message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"].includes(source.type)) {
      source = { ...source, type: "stream_event", event: source };
    }
    if (source.type === "message" && (source.role === "assistant" || source.role === "user")) {
      source = { ...source, type: source.role, message: source };
    }
    if (source.type === "system" && source.subtype === "init") {
      const mapped = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
      if (mapped) emit(source, "session.init", { ...sourceFields(source), ...mapped.data });
    } else if (source.type === "stream_event" && source.event && typeof source.event === "object") {
      const transport = native(source, "claude.stream_event");
      let mappedTransport = false;
      const raw = source.event;
      const channel = channelKey(source);
      if (raw.type === "message_start") {
        const state = { id: raw.message?.id, model: raw.message?.model, blocks: new Map(), textEvents: [], refusalEvent: undefined, transportEvent: transport };
        streams.set(channel, state);
        if (state.id !== undefined) streamedMessages.set(messageKey(source, state.id), state);
        reportUsage(source, state.id, raw.message?.usage);
      } else if (raw.type === "error") {
        reportError(source, raw.error ?? raw);
        mappedTransport = true;
      } else {
        let state = streams.get(channel);
        if (!state) {
          state = { id: undefined, model: undefined, blocks: new Map(), textEvents: [], refusalEvent: undefined };
          streams.set(channel, state);
        }
        if (raw.type === "message_delta") {
          reportUsage(source, state.id, raw.usage);
          const refusal = getMessageRefusal(raw.delta);
          if (refusal) {
            const texts = [...state.blocks.values()].filter(block => block.type === "text").map(block => block.text);
            emitRefusal(source, state, { ...refusal, ...(texts.length ? { content: texts.join("") } : {}) }, true);
            mappedTransport = true;
          }
        } else if (raw.type === "content_block_start" && raw.content_block) {
          const block = raw.content_block;
          const event = emitBlock(source, block, streamFields(state, raw.index));
          mappedTransport = event !== undefined && event.type !== "claude.content_block";
          if (event?.type === "assistant.message") state.textEvents.push(event);
          state.blocks.set(raw.index, {
            type: block.type,
            text: block.type === "thinking" ? (block.thinking ?? "") : (block.text ?? ""),
            argumentText: "",
            event,
          });
        } else if (raw.type === "content_block_delta") {
          const delta = raw.delta;
          if (!delta) continue;
          let block = state.blocks.get(raw.index);
          if (!block && (delta.type === "text_delta" || delta.type === "thinking_delta")) {
            block = { type: delta.type === "text_delta" ? "text" : "thinking", text: "", argumentText: "" };
            state.blocks.set(raw.index, block);
          }
          if (!block) continue;
          if (delta.type === "text_delta" && typeof delta.text === "string") {
            block.text += delta.text;
            if (state.refusalEvent) {
              const texts = [...state.blocks.values()].filter(value => value.type === "text").map(value => value.text);
              if (texts.length) state.refusalEvent.data.content = texts.join("");
              retainSnapshot(state.refusalEvent, source);
            } else {
              const event = emitBlock(source, { type: "text", text: delta.text }, streamFields(state, raw.index));
              if (event?.type === "assistant.message") state.textEvents.push(event);
              block.event ??= event;
            }
            mappedTransport = true;
          } else if (delta.type === "thinking_delta" && typeof delta.thinking === "string") {
            block.text += delta.thinking;
            const event = emitBlock(source, { type: "thinking", thinking: delta.thinking }, streamFields(state, raw.index));
            block.event ??= event;
            mappedTransport = true;
          } else if (delta.type === "input_json_delta" && typeof delta.partial_json === "string") {
            block.argumentText += delta.partial_json;
            if (block.event) block.event.data.argumentText = block.argumentText;
          } else if (delta.type === "signature_delta" && typeof delta.signature === "string" && block.event) {
            block.event.data.signature = (block.event.data.signature ?? "") + delta.signature;
          }
        } else if (raw.type === "content_block_stop") {
          const block = state.blocks.get(raw.index);
          if (block?.type === "tool_use" && block.argumentText && block.event) {
            try {
              block.event.data.input = JSON.parse(block.argumentText);
            } catch {
              // Malformed/truncated arguments retain exact argumentText; recover at the next record.
              continue;
            }
          }
        } else if (raw.type === "message_stop" && state.refusalEvent) {
          retainSnapshot(state.refusalEvent, source);
          delete state.refusalEvent.data.partial;
          mappedTransport = true;
        }
      }
      // The mapped event already retains this transport envelope and its metadata.
      if (mappedTransport) {
        events.splice(events.indexOf(transport), 1);
      }
    } else if (source.type === "assistant" || source.type === "user") {
      const message = source.message;
      const content = typeof message === "string" ? message : message?.content;
      if (source.type === "assistant" && source.error != null) {
        reportError(source, { error: source.error, message: structuredClone(message) });
        continue;
      }
      if (source.type === "assistant") reportUsage(source, message?.id, message?.usage);
      const blocks = typeof content === "string" ? [{ type: "text", text: content }] : Array.isArray(content) ? content : [];
      const streamed = source.type === "assistant" && message?.id !== undefined ? streamedMessages.get(messageKey(source, message.id)) : undefined;
      const refusal = source.type === "assistant" ? getMessageRefusal(message) : undefined;
      if (refusal) {
        if (streamed) emitRefusal(source, streamed, refusal, false);
        else {
          const fields = sourceFields(source);
          delete fields.content;
          emit(source, "assistant.refusal", { ...fields, ...refusal });
        }
      }
      if (!refusal && content !== undefined && typeof content !== "string" && (!Array.isArray(content) || (content.length === 0 && !streamed))) {
        emit(source, source.type === "user" ? "user.message" : "assistant.message", { ...sourceFields(source), content, messageId: message?.id, model: message?.model });
      }
      if (streamed && !refusal && blocks.length === 0) retainSnapshot(streamed.transportEvent, source);
      for (const [snapshotIndex, block] of blocks.entries()) {
        if (refusal && ["text", "refusal"].includes(block?.type)) continue;
        // SDK assistant records can contain one finalized block rather than the full response.
        const candidates = blocks.length === 1 && streamed ? [...streamed.blocks.entries()].filter(([, observed]) => observed.type === block?.type && (block.type !== "tool_use" || observed.event?.data.toolCallId === block.id)) : [];
        const index = candidates.length === 1 ? candidates[0][0] : snapshotIndex;
        const observed = streamed?.blocks.get(index);
        retainSnapshot(observed?.event, source);
        if (!observed || observed.type !== block?.type) {
          const event = emitBlock(source, block, { contentIndex: index });
          if (streamed && event) {
            retainSnapshot(event, source);
            streamed.blocks.set(index, { type: block.type, text: block.type === "thinking" ? (block.thinking ?? "") : (block.text ?? ""), argumentText: "", event });
            if (event.type === "assistant.message") streamed.textEvents.push(event);
          }
        } else if (block.type === "text" || block.type === "thinking") {
          if (Object.hasOwn(block, "signature") && observed.event) observed.event.data.signature = block.signature;
          const text = block.type === "thinking" ? block.thinking : block.text;
          if (typeof text === "string" && text.startsWith(observed.text) && text !== observed.text) {
            const event = emitBlock(source, { ...block, [block.type === "thinking" ? "thinking" : "text"]: text.slice(observed.text.length) }, streamFields(streamed, index));
            if (event?.type === "assistant.message") streamed.textEvents.push(event);
            observed.text = text;
          } else if (typeof text === "string" && text !== observed.text) {
            const event = emitBlock(source, block, { contentIndex: index, delta: false });
            if (event?.type === "assistant.message") streamed.textEvents.push(event);
            observed.text = text;
          }
        } else if (block.type === "tool_use" && observed.type === "tool_use" && observed.event?.data.toolCallId === block.id) {
          if (Object.hasOwn(block, "input")) observed.event.data.input = structuredClone(block.input);
        } else if (["redacted_thinking", "image", "document"].includes(block.type) && observed.event) {
          Object.assign(observed.event.data, structuredClone(block), { content: structuredClone(block) });
        } else {
          emitBlock(source, block, { contentIndex: index });
        }
      }
    } else if (source.type === "reasoning" && typeof source.data?.content === "string") {
      emit(source, "assistant.reasoning", source.data);
    } else if (source.type === "result") {
      const mapped = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
      if (!mapped || mapped.type !== "session.result") continue;
      /** @type {import("./types/agent_session").SessionResultData} */
      const data = { ...sourceFields(source), ...mapped.data, sourceEngine: "claude", sourceType: source.type, usage: claudeUsage(source.usage) };
      const failed = source.is_error === true || (typeof source.subtype === "string" && source.subtype.startsWith("error"));
      if (failed) data.status = "error";
      else if (!Object.hasOwn(data, "status") && typeof source.subtype === "string") data.status = source.subtype;
      if (failed && (!Array.isArray(data.errors) || data.errors.length === 0)) {
        const diagnostic = {};
        for (const key of ["subtype", "is_error", "terminal_reason", "api_error_status", "error", "result"]) {
          if (Object.hasOwn(source, key)) diagnostic[key] = source[key];
        }
        data.errors = [diagnostic];
      }
      if (Array.isArray(data.permissionDenials)) {
        data.permissionDenials = data.permissionDenials.filter(denial => {
          if (!denial || typeof denial !== "object" || Array.isArray(denial) || typeof denial.tool_use_id !== "string") return true;
          const context = sessionContext(source);
          const matches = denialReports.filter(report => {
            const observed = sessionContext(report.event);
            return (
              report.denial.tool_use_id === denial.tool_use_id &&
              observed.sessionId === context.sessionId &&
              (context.parentToolUseId === undefined || observed.parentToolUseId === undefined || context.parentToolUseId === observed.parentToolUseId) &&
              (context.agentId === undefined || observed.agentId === undefined || context.agentId === observed.agentId)
            );
          });
          if (!matches.length || new Set(matches.map(report => JSON.stringify([sessionContext(report.event).parentToolUseId ?? null, sessionContext(report.event).agentId]))).size !== 1) return true;
          for (const report of matches) Object.assign(report.denial, { ...denial, ...report.denial });
          return false;
        });
      }
      terminalResults.push(emit(source, "session.result", data));
    } else if (source.type === "system" && typeof source.subtype === "string") {
      const task = agentTasks.get(taskKey(source, source.task_id));
      if (source.subtype === "permission_denied") {
        const denial = sourceFields(source);
        const event = emit(source, "session.result", { ...denial, sourceEngine: "claude", sourceType: source.type, permissionDenials: [denial] });
        denialReports.push({ event, denial: event.data.permissionDenials[0] });
      } else if (source.subtype === "api_retry" && source.error != null) {
        reportError(source, { error: source.error, error_status: source.error_status, attempt: source.attempt });
      } else if (source.error != null && !task) {
        reportError(source, source.error);
      } else if (task && source.subtype === "task_started") {
        emitSubagent(source, "subagent.started", task, {
          agentName: source.subagent_type,
          agentType: source.subagent_type,
          parentId: parentAgent(source),
          executionMode: typeof source.is_backgrounded === "boolean" ? (source.is_backgrounded ? "background" : "sync") : undefined,
          spawnDepth: isTokenCount(source.spawn_depth) ? source.spawn_depth : undefined,
        });
      } else if (task && source.subtype === "task_notification" && ["completed", "failed", "stopped"].includes(source.status)) {
        emitSubagent(source, source.status === "failed" ? "subagent.failed" : "subagent.completed", task, {
          durationMs: isMetric(source.usage?.duration_ms) ? source.usage.duration_ms : undefined,
          totalTokens: isTokenCount(source.usage?.total_tokens) ? source.usage.total_tokens : undefined,
          totalToolCalls: isTokenCount(source.usage?.tool_uses) ? source.usage.tool_uses : undefined,
          ...(source.status === "stopped" ? { cancelled: true } : {}),
        });
      } else native(source, !task && Object.hasOwn(DYNAMIC_WORKFLOW_EVENT_TYPES, source.subtype) ? DYNAMIC_WORKFLOW_EVENT_TYPES[source.subtype] : "claude.system");
      if (source.subtype !== "permission_denied" && source.error == null) {
        const content = typeof source.message === "string" ? source.message : (source.message?.content ?? source.content);
        if (typeof content === "string") emitBlock(source, { type: "text", text: content });
        else if (Array.isArray(content)) {
          for (const block of content) emitBlock(source, block);
        }
      }
    } else if (["tool_progress", "tool_use_summary", "rate_limit_event", "auth_status"].includes(source.type)) {
      if (source.type === "auth_status" && source.error != null) reportError(source, source.error);
      else native(source, `claude.${source.type}`);
    } else if (source.type === "error" && source.error != null) {
      reportError(source, source.error);
    }
  }

  if (responseUsage.size > 0) {
    const sessions = new Map();
    for (const observation of responseUsage.values()) {
      const key = channelKey(observation.source);
      const session = sessions.get(key) ?? { usage: {}, source: observation.source };
      accumulateSessionUsage(session.usage, observation.usage);
      sessions.set(key, session);
    }
    for (const [key, session] of sessions) {
      const observed = { ...session.usage, input_tokens_include_cache: false };
      const terminal = [...terminalResults].reverse().find(event => channelKey(event) === key);
      if (terminal) {
        const snapshots = terminalResults.filter(event => channelKey(event) === key);
        terminal.data.usage = reconcileSessionUsage(observed, projectSessionResult(snapshots, { includeNested: true })?.usage);
      } else {
        emit(session.source, "session.result", { sourceEngine: "claude", usage: observed, partial: true });
      }
    }
  }
  return events;
}

module.exports = { normalizeClaudeSession };
