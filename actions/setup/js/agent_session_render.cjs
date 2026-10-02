// @ts-check

const { sessionOutputText } = require("./agent_session.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");

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
    return JSON.parse(
      JSON.stringify(events, function (key, value) {
        if (typeof value !== "string") return value;
        if (identities.has(this) && ["id", "toolCallId", "tool_use_id"].includes(key)) return value;
        if (!cache.has(value)) cache.set(value, redact(value));
        return cache.get(value);
      })
    );
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to prepare agent session for publication: ${getErrorMessage(error)}`, { cause: error });
  }
}

module.exports = { escapeSummaryText, toolInventoryName, displayArgument, renderInitializationLines, toolOutcome, boundSummaryLines, redactSessionForPublication };
