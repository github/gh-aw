// @ts-check

const { sessionOutputText } = require("./agent_session.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_PARSE, ERR_VALIDATION } = require("./error_codes.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");

// Redacted identities can coincide; correlate using private source envelopes.
const publicationSources = new WeakMap();

/** @param {any} source @returns {string} */
function messageSourceScope(source) {
  return JSON.stringify([
    source.provenance?.component,
    source.provenance?.phase,
    source.provenance?.path,
    source.session_id ?? source.sessionId ?? source.data?.sessionId ?? source.data?.session_id,
    source.agentId ?? source.data?.agentId,
    source.parent_tool_use_id ?? source.data?.parentToolUseId ?? source.data?.parent_tool_use_id,
    source.channel ?? source.data?.channel,
  ]);
}

/**
 * Combine display copies only; event IDs and timestamps do not delimit a stream.
 * Native message boundaries, channels and tool events still delimit entries.
 * @param {Array<any>} events
 * @returns {Array<any>}
 */
function collapseStreamedMessages(events) {
  const output = [];
  const projections = new Set();
  const secrets = collectArtifactSecretValues();
  const claudeMessages = new Map();
  let sequence = 0;
  let geminiSnapshot;
  /** @type {{key: string, entry: any, legacy: boolean, field: string}|undefined} */
  let previous;
  for (const entry of events) {
    const source = publicationSources.get(entry) ?? entry;
    const scope = messageSourceScope(source);
    const native = source.event ?? source.data?.event;
    if (source.type === "claude.stream_event" && native && ["message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"].includes(native.type)) {
      if (native.type === "message_start") {
        claudeMessages.set(scope, { id: native.message?.id, sequence: ++sequence });
        previous = undefined;
      }
      output.push(entry);
      continue;
    }
    if (source.type === "assistant.message_delta" || source.type === "claude.assistant_snapshot" || (source.type === "gemini.message_snapshot" && source.data?.role !== "user" && source.data?.type !== "user")) {
      if (source.type === "gemini.message_snapshot") geminiSnapshot = { scope, id: source.data?.messageId ?? source.data?.message_id ?? source.messageId ?? source.message_id, content: entry.data?.content };
      output.push(entry);
      continue;
    }
    const legacy = source.type === "assistant" && source.message?.content?.length === 1;
    const block = legacy ? entry.message.content[0] : undefined;
    const type = legacy ? (block?.type === "text" ? "assistant.message" : block?.type === "thinking" ? "assistant.reasoning" : undefined) : source.type;
    const field = block?.type === "thinking" ? "thinking" : "text";
    const text = legacy ? block?.[field] : entry.data?.content;
    const messageId = source.data?.messageId ?? source.data?.message_id ?? source.messageId ?? source.message_id ?? source.message?.id;
    const claude = claudeMessages.get(scope);
    const snapshotBlocks = source.message?.content;
    const matchingBlocks = Array.isArray(snapshotBlocks) ? snapshotBlocks.map((value, index) => ({ value, index })).filter(({ value }) => value?.type === (type === "assistant.reasoning" ? "thinking" : "text")) : [];
    const claudeContinuation = claude?.id !== undefined && messageId === claude.id && matchingBlocks.length === 1;
    const position = native?.index ?? source.data?.contentIndex ?? (claudeContinuation ? matchingBlocks[0].index : undefined);
    const delta = source.delta === true || source.data?.delta === true || source.copilotProjection === "assistant.message_delta";
    const partial = source.partial === true || source.data?.partial === true;
    const key = JSON.stringify([scope, type, messageId ?? claude?.id, claude?.sequence, position]);
    const previousText = previous?.legacy ? previous.entry.message.content[0][previous.field] : previous?.entry.data.content;
    const geminiContinuation = previous?.key === key && geminiSnapshot?.scope === scope && geminiSnapshot.id === messageId && redactManifestValue(previousText + text, secrets) === redactManifestValue(geminiSnapshot.content, secrets);
    const continuation = (native && ["content_block_start", "content_block_delta"].includes(native.type)) || claudeContinuation || geminiContinuation;
    const streamed = delta || continuation || (partial && messageId !== undefined);
    geminiSnapshot = undefined;
    if (!["assistant.message", "assistant.reasoning"].includes(type) || typeof text !== "string" || (!streamed && !(messageId !== undefined && previous?.key === key))) {
      previous = undefined;
      output.push(entry);
      continue;
    }
    const snapshot = !delta && !continuation;
    const content = text;
    if (previous?.key === key) {
      const target = previous.legacy ? previous.entry.message.content[0] : previous.entry.data;
      const targetField = previous.legacy ? previous.field : "content";
      target[targetField] = snapshot ? content : target[targetField] + content;
    } else {
      const copy = legacy ? { ...entry, message: { ...entry.message, content: [{ ...block, [field]: content }] } } : { ...entry, data: { ...entry.data, content } };
      publicationSources.set(copy, source);
      projections.add(copy);
      output.push(copy);
      previous = { key, entry: copy, legacy, field };
    }
    if (snapshot && !partial) previous = undefined;
  }
  for (const entry of projections) {
    const block = entry.type === "assistant" ? entry.message.content[0] : entry.data;
    const field = entry.type === "assistant" ? (block.type === "thinking" ? "thinking" : "text") : "content";
    const redacted = redactManifestValue(block[field], secrets);
    if (typeof redacted !== "string") throw new Error(`${ERR_VALIDATION}: Expected streamed message text`);
    block[field] = redacted;
  }
  return output;
}

/** @param {string} text @returns {string} */
function escapeSummaryText(text) {
  const entities = { "&": "&amp;", "<": "&lt;", ">": "&gt;" };
  return text.replace(/[&<>]/g, char => entities[char]);
}

/** @param {any} value @returns {string} */
function toolInventoryName(value) {
  if (typeof value === "string") return value;
  if (typeof value?.name === "string") return value.name;
  if (typeof value?.function?.name === "string") return value.function.name;
  return sessionOutputText(value);
}

/** @param {any} value @returns {string} */
function displayArgument(value) {
  return typeof value === "string" ? JSON.stringify(value) : sessionOutputText(value);
}

/** @param {any} init @returns {string[]} */
function renderInitializationLines(init) {
  if (!init) return [];
  const lines = ["Initialization:"];
  if (init.source_engine !== undefined) lines.push(`  Engine: ${sessionOutputText(init.source_engine)}`);
  if (init.session_id !== undefined) lines.push(`  Session ID: ${init.session_id === "" ? '""' : sessionOutputText(init.session_id)}`);
  if (init.cwd !== undefined) lines.push(`  Working Directory: ${init.cwd === "" ? '""' : sessionOutputText(init.cwd)}`);
  if (Array.isArray(init.tools)) lines.push(`  Available Tools (${init.tools.length}): ${init.tools.map(toolInventoryName).join(", ")}`);
  if (Array.isArray(init.mcp_servers)) {
    lines.push(`  MCP Servers: ${init.mcp_servers.length}`);
    for (const server of init.mcp_servers) {
      const name = typeof server?.name === "string" ? server.name : sessionOutputText(server);
      lines.push(`    ${name} (${typeof server?.status === "string" ? server.status : "unknown"})`);
      if (server?.error !== undefined) lines.push(`      Error: ${sessionOutputText(server.error)}`);
    }
  }
  if (Array.isArray(init.slash_commands)) lines.push(`  Slash Commands (${init.slash_commands.length}): ${init.slash_commands.map(toolInventoryName).join(", ")}`);
  if (init.model_info !== undefined) lines.push(`  Model Information: ${sessionOutputText(init.model_info)}`);
  if (lines.length === 1) return [];
  return [...lines, ""];
}

/** @param {any} result @returns {"pending"|"failed"|"succeeded"|"unknown"} */
function toolOutcome(result) {
  if (!result) return "pending";
  if (result.is_error === true || result.error != null) return "failed";
  return result.is_error === false ? "succeeded" : "unknown";
}

/** @param {string} text @param {number} bytes @returns {string} */
function utf8Prefix(text, bytes) {
  const buffer = Buffer.from(text, "utf8");
  if (buffer.length <= bytes) return text;
  let end = Math.max(0, bytes);
  while (end > 0 && (buffer[end] & 0xc0) === 0x80) end--;
  return buffer.subarray(0, end).toString("utf8");
}

/** @param {string[]} lines @param {number} maxBytes @param {number} maxLineBytes @returns {string[]} */
function boundSummaryLines(lines, maxBytes, maxLineBytes) {
  const warning = "... (summary truncated: byte limit reached)";
  const lineWarning = "... [truncated: long summary line]";
  const reserved = Buffer.byteLength(warning, "utf8") + 1;
  const output = [];
  let size = 0;
  for (const raw of lines) {
    const line = Buffer.byteLength(raw, "utf8") > maxLineBytes ? utf8Prefix(raw, maxLineBytes - Buffer.byteLength(lineWarning, "utf8")) + lineWarning : raw;
    const bytes = Buffer.byteLength(line, "utf8") + 1;
    if (size + bytes > maxBytes - reserved) {
      const remaining = maxBytes - reserved - size - 1;
      if (remaining > 0) output.push(utf8Prefix(line, remaining));
      output.push(warning);
      return output;
    }
    output.push(line);
    size += bytes;
  }
  return output;
}

/**
 * Redact publication copies before preview truncation, retaining private pairing keys.
 * @param {Array<any>} events
 * @param {(text: string) => string} redact
 * @returns {Array<any>}
 */
function redactSessionForPublication(events, redact) {
  const identities = new WeakSet();
  for (const event of events) {
    if (event?.type === "tool.execution_start" || event?.type === "tool.execution_complete") {
      if (event.data && typeof event.data === "object") identities.add(event.data);
    }
    if (Array.isArray(event?.message?.content)) {
      for (const block of event.message.content) {
        if (block?.type === "tool_use" || block?.type === "tool_result") identities.add(block);
      }
    }
  }
  const cache = new Map();
  try {
    const copy = JSON.parse(
      JSON.stringify(events, function (key, value) {
        if (typeof value !== "string") return value;
        if (identities.has(this) && ["id", "toolCallId", "tool_use_id"].includes(key)) return value;
        if (!cache.has(value)) cache.set(value, redact(value));
        return cache.get(value);
      })
    );
    for (const [index, event] of copy.entries()) {
      if (event && typeof event === "object") publicationSources.set(event, publicationSources.get(events[index]) ?? events[index]);
    }
    return copy;
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to prepare agent session for publication: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = { collapseStreamedMessages, escapeSummaryText, toolInventoryName, displayArgument, renderInitializationLines, toolOutcome, boundSummaryLines, redactSessionForPublication };
