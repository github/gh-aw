// @ts-check

const { isSessionEvent, createSessionEvent, normalizeAgentSession, normalizeSessionUsage, accumulateSessionUsage, reconcileSessionUsage, isMetric, projectSessionResult } = require("./agent_session.cjs");
const { getMessageRefusal } = require("./provider_refusal.cjs");

/**
 * Claude input tokens exclude cache reads and cache writes.
 * @param {any} usage
 * @returns {Record<string, any>|undefined}
 */
function claudeUsage(usage) {
  const normalized = normalizeSessionUsage(usage);
  return normalized ? { ...normalized, input_tokens_include_cache: false } : undefined;
}

/** @param {any} record @returns {Record<string, any>} */
function sourceFields(record) {
  const { type, data, ...fields } = record;
  return { ...fields, ...data };
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

  const channelKey = source => JSON.stringify([source.session_id, source.parent_tool_use_id]);
  const messageKey = (source, id) => JSON.stringify([source.session_id, source.parent_tool_use_id, id]);
  const toolKey = (source, id) => JSON.stringify([source.session_id, source.parent_tool_use_id, id]);
  const emit = (source, type, data) => {
    const event = createSessionEvent(source, type, data);
    events.push(event);
    return event;
  };
  const native = (source, type) => emit(source, type, sourceFields(source));
  const reportUsage = (source, id, usage) => {
    const normalized = claudeUsage(usage);
    if (id === undefined || !normalized) return;
    const key = messageKey(source, id);
    responseUsage.set(key, { usage: { ...responseUsage.get(key)?.usage, ...normalized }, source });
  };
  const reportError = (source, error) => emit(source, "session.result", { sourceEngine: "claude", errors: [error] });

  const streamFields = (state, index) => ({ delta: true, ...(state.id !== undefined ? { messageId: state.id } : {}), ...(index !== undefined ? { contentIndex: index } : {}) });
  const emitBlock = (source, block, metadata = {}) => {
    if (!block || typeof block !== "object") return;
    const data = { ...sourceFields(source), ...block, ...metadata };
    if (block.type === "text" && typeof block.text === "string") {
      return emit(source, source.type === "user" ? "user.message" : "assistant.message", { ...data, content: block.text });
    }
    if (block.type === "thinking" && typeof block.thinking === "string") {
      return emit(source, "assistant.reasoning", { ...data, content: block.thinking });
    }
    if (block.type === "tool_use") {
      const event = emit(source, "tool.execution_start", { ...data, toolCallId: block.id, toolName: block.name, input: block.input });
      if (block.id !== undefined) tools.set(toolKey(source, block.id), event.data);
      return event;
    }
    if (block.type === "tool_result") {
      const start = block.tool_use_id !== undefined ? tools.get(toolKey(source, block.tool_use_id)) : undefined;
      const success = block.is_error === true || block.error != null ? false : block.is_error === false ? true : typeof block.success === "boolean" ? block.success : undefined;
      return emit(source, "tool.execution_complete", {
        ...data,
        toolCallId: block.tool_use_id,
        toolName: block.name ?? start?.toolName,
        success,
        output: block.content,
        durationMs: isMetric(block.duration_ms) ? block.duration_ms : undefined,
      });
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
    if (["message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"].includes(source.type)) {
      source = { ...source, type: "stream_event", event: source };
    }
    if (source.type === "message" && source.role === "assistant" && source.stop_reason === "refusal") {
      events.push(...normalizeAgentSession([source], { sourceEngine: "claude" }));
      continue;
    }
    if (source.type === "system" && source.subtype === "init") {
      const mapped = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
      if (mapped) emit(source, "session.init", { ...sourceFields(source), ...mapped.data });
    } else if (source.type === "stream_event" && source.event && typeof source.event === "object") {
      native(source, "claude.stream_event");
      const raw = source.event;
      const channel = channelKey(source);
      if (raw.type === "message_start") {
        const state = { id: raw.message?.id, blocks: new Map(), textEvents: [], refusalEvent: undefined };
        streams.set(channel, state);
        if (state.id !== undefined) streamedMessages.set(messageKey(source, state.id), state);
        reportUsage(source, state.id, raw.message?.usage);
      } else if (raw.type === "error") {
        reportError(source, raw.error ?? raw);
      } else {
        let state = streams.get(channel);
        if (!state) {
          state = { id: undefined, blocks: new Map(), textEvents: [], refusalEvent: undefined };
          streams.set(channel, state);
        }
        if (raw.type === "message_delta") {
          reportUsage(source, state.id, raw.usage);
          const refusal = getMessageRefusal(raw.delta);
          if (refusal) {
            const texts = [...state.blocks.values()].filter(block => block.type === "text").map(block => block.text);
            state.refusalEvent = emit(source, "assistant.refusal", { ...sourceFields(source), ...refusal, ...(texts.length ? { content: texts.join("") } : {}) });
            for (const textEvent of state.textEvents) {
              const index = events.indexOf(textEvent);
              if (index !== -1) events.splice(index, 1);
            }
            state.textEvents = [];
          }
        } else if (raw.type === "content_block_start" && raw.content_block) {
          const block = raw.content_block;
          const event = emitBlock(source, block, streamFields(state, raw.index));
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
            } else {
              const event = emitBlock(source, { type: "text", text: delta.text }, streamFields(state, raw.index));
              if (event?.type === "assistant.message") state.textEvents.push(event);
            }
          } else if (delta.type === "thinking_delta" && typeof delta.thinking === "string") {
            block.text += delta.thinking;
            emitBlock(source, { type: "thinking", thinking: delta.thinking }, streamFields(state, raw.index));
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
        }
      }
    } else if (source.type === "assistant" || source.type === "user") {
      const message = source.message;
      const content = typeof message === "string" ? message : message?.content;
      if (source.type === "assistant" && source.error != null) {
        native(source, "claude.assistant_error");
        reportError(source, { error: source.error, message: structuredClone(message) });
        continue;
      }
      reportUsage(source, message?.id, message?.usage);
      const blocks = typeof content === "string" ? [{ type: "text", text: content }] : Array.isArray(content) ? content : [];
      const streamed = source.type === "assistant" && message?.id !== undefined ? streamedMessages.get(messageKey(source, message.id)) : undefined;
      if (streamed) native(source, "claude.assistant_snapshot");
      const refusal = source.type === "assistant" ? getMessageRefusal(message) : undefined;
      if (refusal) {
        if (streamed?.refusalEvent) Object.assign(streamed.refusalEvent.data, refusal);
        else emit(source, "assistant.refusal", { ...sourceFields(source), ...refusal });
      }
      for (const [index, block] of blocks.entries()) {
        if (refusal && block?.type === "text") continue;
        const observed = streamed?.blocks.get(index);
        if (!observed) {
          emitBlock(source, block);
        } else if (block.type === "text" || block.type === "thinking") {
          const text = block.type === "thinking" ? block.thinking : block.text;
          if (typeof text === "string" && text.startsWith(observed.text) && text !== observed.text) {
            emitBlock(source, { ...block, [block.type === "thinking" ? "thinking" : "text"]: text.slice(observed.text.length) }, streamFields(streamed, index));
            observed.text = text;
          }
        } else if (block.type === "tool_use" && observed.type === "tool_use" && observed.event?.data.toolCallId === block.id) {
          if (Object.hasOwn(block, "input")) observed.event.data.input = structuredClone(block.input);
        } else {
          emitBlock(source, block);
        }
      }
    } else if (source.type === "reasoning" && typeof source.data?.content === "string") {
      emit(source, "assistant.reasoning", source.data);
    } else if (source.type === "result") {
      const mapped = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
      if (!mapped || mapped.type !== "session.result") continue;
      /** @type {import("./types/agent_session").SessionResultData} */
      const data = { ...sourceFields(source), ...mapped.data, sourceEngine: "claude", usage: claudeUsage(source.usage) };
      const failed = source.is_error === true || (typeof source.subtype === "string" && source.subtype.startsWith("error"));
      if (failed && (!Array.isArray(data.errors) || data.errors.length === 0)) {
        const diagnostic = {};
        for (const key of ["subtype", "is_error", "terminal_reason", "api_error_status", "error", "result"]) {
          if (Object.hasOwn(source, key)) diagnostic[key] = source[key];
        }
        data.errors = [diagnostic];
      }
      terminalResults.push(emit(source, "session.result", data));
    } else if (source.type === "system" && typeof source.subtype === "string") {
      native(source, "claude.system");
      if (source.subtype === "api_retry" && source.error != null) {
        reportError(source, { error: source.error, error_status: source.error_status, attempt: source.attempt });
      } else if (source.error != null) {
        reportError(source, source.error);
      } else {
        const content = typeof source.message === "string" ? source.message : (source.message?.content ?? source.content);
        if (typeof content === "string") emitBlock(source, { type: "text", text: content });
        else if (Array.isArray(content)) {
          for (const block of content) emitBlock(source, block);
        }
      }
    } else if (["tool_progress", "tool_use_summary", "rate_limit_event", "auth_status"].includes(source.type)) {
      native(source, `claude.${source.type}`);
    }
  }

  if (responseUsage.size > 0) {
    const sessions = new Map();
    for (const observation of responseUsage.values()) {
      if (observation.source.parent_tool_use_id) continue;
      const key = observation.source.session_id ?? "";
      const session = sessions.get(key) ?? { usage: {}, source: observation.source };
      accumulateSessionUsage(session.usage, observation.usage);
      sessions.set(key, session);
    }
    for (const [id, session] of sessions) {
      const observed = { ...session.usage, input_tokens_include_cache: false };
      const terminal = [...terminalResults].reverse().find(event => !event.parent_tool_use_id && (event.session_id ?? "") === id);
      if (terminal) {
        const snapshots = terminalResults.filter(event => !event.parent_tool_use_id && (event.session_id ?? "") === id);
        terminal.data.usage = reconcileSessionUsage(observed, projectSessionResult(snapshots)?.usage);
      } else {
        emit(session.source, "session.result", { sourceEngine: "claude", usage: observed, partial: true });
      }
    }
  }
  return events;
}

module.exports = { normalizeClaudeSession };
