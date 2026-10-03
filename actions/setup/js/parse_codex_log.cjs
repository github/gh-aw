// @ts-check
/// <reference types="@actions/github-script" />

const {
  createEngineLogParser,
  truncateString,
  estimateTokens,
  formatToolCallAsDetails,
  buildStepSummaryDetailsSection,
  parseLogEntries,
  generateConversationMarkdown,
  generateInformationSection,
  formatInitializationSummary,
  formatToolUse,
} = require("./log_parser_shared.cjs");
const { normalizeCodexSession, isCodexRecord } = require("./codex_session.cjs");
const { projectSessionResult, createSessionEvent, sessionToolSuccess } = require("./agent_session.cjs");

const main = createEngineLogParser({
  parserName: "Codex",
  parseFunction: parseCodexLog,
  supportsDirectories: false,
});

/**
 * Extract MCP server initialization information from Codex logs
 * @param {string[]} lines - Array of log lines
 * @returns {{hasInfo: boolean, markdown: string, servers: Array<{name: string, status: string, error?: string}>}} MCP initialization info
 */
function extractMCPInitialization(lines) {
  const mcpServers = new Map(); // Map server name to status/error info
  let serverCount = 0;
  let connectedCount = 0;
  let availableTools = [];

  for (const line of lines) {
    // Match: Initializing MCP servers from config
    if (line.includes("Initializing MCP servers") || (line.includes("mcp") && line.includes("init"))) {
      // Continue to next patterns
    }

    // Match: Found N MCP servers in configuration
    const countMatch = line.match(/Found (\d+) MCP servers? in configuration/i);
    if (countMatch) {
      serverCount = parseInt(countMatch[1], 10);
    }

    // Match: Connecting to MCP server: <name>
    const connectingMatch = line.match(/Connecting to MCP server[:\s]+['"]?(\w+)['"]?/i);
    if (connectingMatch) {
      const serverName = connectingMatch[1];
      if (!mcpServers.has(serverName)) {
        mcpServers.set(serverName, { name: serverName, status: "connecting" });
      }
    }

    // Match: MCP server '<name>' connected successfully
    const connectedMatch = line.match(/MCP server ['"](\w+)['"] connected successfully/i);
    if (connectedMatch) {
      const serverName = connectedMatch[1];
      mcpServers.set(serverName, { name: serverName, status: "connected" });
      connectedCount++;
    }

    // Match: Failed to connect to MCP server '<name>': <error>
    const failedMatch = line.match(/Failed to connect to MCP server ['"](\w+)['"][:]\s*(.+)/i);
    if (failedMatch) {
      const serverName = failedMatch[1];
      const error = failedMatch[2].trim();
      mcpServers.set(serverName, { name: serverName, status: "failed", error });
    }

    // Match: MCP server '<name>' initialization failed
    const initFailedMatch = line.match(/MCP server ['"](\w+)['"] initialization failed/i);
    if (initFailedMatch) {
      const serverName = initFailedMatch[1];
      const existing = mcpServers.get(serverName);
      if (existing && existing.status !== "failed") {
        mcpServers.set(serverName, { name: serverName, status: "failed", error: "Initialization failed" });
      }
    }

    // Match: Available tools: tool1, tool2, tool3
    const toolsMatch = line.match(/Available tools:\s*(.+)/i);
    if (toolsMatch) {
      const toolsStr = toolsMatch[1];
      availableTools = toolsStr
        .split(",")
        .map(t => t.trim())
        .filter(t => t.length > 0);
    }
  }

  // Build markdown output
  let markdown = "";
  const hasInfo = mcpServers.size > 0 || availableTools.length > 0;

  if (mcpServers.size > 0) {
    markdown += "**MCP Servers:**\n";

    // Count by status
    const servers = Array.from(mcpServers.values());
    const connected = servers.filter(s => s.status === "connected");
    const failed = servers.filter(s => s.status === "failed");

    markdown += `- Total: ${servers.length}${serverCount > 0 && servers.length !== serverCount ? ` (configured: ${serverCount})` : ""}\n`;
    markdown += `- Connected: ${connected.length}\n`;
    if (failed.length > 0) {
      markdown += `- Failed: ${failed.length}\n`;
    }
    markdown += "\n";

    // List each server with status
    for (const server of servers) {
      const statusIcon = server.status === "connected" ? "✅" : server.status === "failed" ? "❌" : "⏳";
      markdown += `- ${statusIcon} **${server.name}** (${server.status})`;
      if (server.error) {
        markdown += `\n  - Error: ${server.error}`;
      }
      markdown += "\n";
    }
    markdown += "\n";
  }

  if (availableTools.length > 0) {
    markdown += "**Available MCP Tools:**\n";
    markdown += `- Total: ${availableTools.length} tools\n`;
    markdown += `- Tools: ${availableTools.slice(0, 10).join(", ")}${availableTools.length > 10 ? ", ..." : ""}\n\n`;
  }

  return {
    hasInfo,
    markdown,
    servers: Array.from(mcpServers.values()),
  };
}

/**
 * Extract error messages from Codex logs (e.g., model access blocked, cyber_policy_violation)
 * @param {string[]} lines - Array of log lines
 * @returns {{hasErrors: boolean, messages: string[], reconnectCount: number, maxReconnects: number}} Error info
 */
function extractCodexErrorMessages(lines) {
  const messages = new Set();
  let reconnectCount = 0;
  let maxReconnects = 0;

  for (const line of lines) {
    // Match: ERROR: <message> (final error after all retries exhausted)
    const errorMatch = line.match(/^ERROR:\s*(.+)$/);
    if (errorMatch) {
      messages.add(errorMatch[1].trim());
    }

    // Match: Reconnecting... N/M (error message) - reconnect attempts with error details
    const reconnectMatch = line.match(/^Reconnecting\.\.\.\s+(\d+)\/(\d+)\s*\((.+)\)$/);
    if (reconnectMatch) {
      const attempt = parseInt(reconnectMatch[1], 10);
      const total = parseInt(reconnectMatch[2], 10);
      if (attempt > reconnectCount) reconnectCount = attempt;
      if (total > maxReconnects) maxReconnects = total;
      messages.add(reconnectMatch[3].trim());
    }
  }

  return {
    hasErrors: messages.size > 0,
    messages: Array.from(messages),
    reconnectCount,
    maxReconnects,
  };
}

/**
 * Extract the model name from Codex log header lines.
 * Codex logs include a line like "model: o4-mini" near the top.
 * @param {string} logContent - The raw log content
 * @returns {string|null} The model name, or null if not found
 */
function extractCodexModel(logContent) {
  const match = logContent.match(/^model:\s*(.+)$/m);
  if (match) {
    return match[1].trim();
  }
  // Fallback for the experimental JSONL format, which has no "model:" header line.
  // The codex harness logs the resolved spawn command, e.g.
  //   [codex-harness] ... spawning: codex exec --model gpt-5.4 -c ...
  const spawnMatch = logContent.match(/spawning:\s*codex\s+exec\s+--model\s+(\S+)/);
  if (spawnMatch) {
    return spawnMatch[1].trim();
  }
  return null;
}

/**
 * Detects whether the log is in the Codex experimental JSONL event format.
 * Newer Codex CLI versions (e.g. 0.141+) emit a stream of JSON objects such as
 * `{"type":"thread.started",...}`, `{"type":"turn.completed",...}` and
 * `{"type":"item.completed","item":{"type":"agent_message",...}}` instead of the
 * legacy pretty-printed "thinking"/"tool server.method(...)" lines.
 * @param {string[]} lines - The log split into lines
 * @param {Array<any>} [entries] - Already parsed records, when available
 * @returns {boolean} True if at least one Codex JSONL event line is present
 */
function isCodexJsonlFormat(lines, entries = parseLogEntries(lines.join("\n")) ?? []) {
  return entries.some(entry => isCodexRecord(entry) || normalizeCodexSession([entry]).length > 0);
}

/**
 * Parse the Codex experimental JSONL event stream into the shared logEntries model.
 * Retains native item lifecycles, message snapshots and per-turn accounting.
 * @param {string} logContent - The raw log content
 * @param {Array<any>} [entries] - Already parsed records, when available
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseCodexJsonl(logContent, entries = parseLogEntries(logContent) ?? []) {
  const canonicalLogEntries = normalizeCodexSession(collectCodexMixedRecords(logContent, entries), extractCodexModel(logContent));
  const conversation = generateConversationMarkdown(canonicalLogEntries, {
    includeInformation: false,
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult),
    formatInitCallback: init => formatInitializationSummary(init),
  });
  const markdown = conversation.markdown + generateInformationSection(projectSessionResult(canonicalLogEntries));

  return {
    markdown,
    logEntries: canonicalLogEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

/**
 * Keep recognized text observations beside JSON records without splitting the
 * native normalizer's lifecycle/accounting state into independent sessions.
 * @param {string} logContent
 * @param {Array<any>} entries
 * @returns {Array<any>}
 */
function collectCodexMixedRecords(logContent, entries) {
  try {
    if (Array.isArray(JSON.parse(logContent))) return entries;
  } catch {
    // Mixed streams are not a single JSON document.
  }
  const records = [];
  let text = [];
  let hasFramedResult = false;
  const accounting = { inlineTokens: 0, hasTokenSnapshot: false };
  const flushText = () => {
    const content = text.join("\n");
    records.push(...parseCodexLegacySession(text, /^model:/m.test(content) ? extractCodexModel(content) : null, accounting));
    text = [];
  };
  const lines = logContent.split("\n");
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    const resultEnd = codexLegacyResultEnd(lines, index);
    if (resultEnd !== null) {
      hasFramedResult = true;
      text.push(...lines.slice(index, resultEnd + 1));
      index = resultEnd;
      continue;
    }
    const parsed = parseLogEntries(line);
    if (parsed && isCodexJsonlFormat([], parsed)) {
      flushText();
      records.push(...parsed);
    } else text.push(line);
  }
  flushText();
  return records.length > 0 || hasFramedResult ? records : entries;
}

const CODEX_LEGACY_OUTCOME = /^(?:([\w-]+)\.([\w-]+)\(.*\)|(.+?))\s+(success|succeeded|failure|failed)\s+in\s+(\d+(?:\.\d+)?)(ms|s):$/;

/**
 * A JSON value following a legacy completion belongs to that tool, regardless
 * of lifecycle-looking fields inside its payload.
 * @param {string[]} lines
 * @param {number} currentIndex
 * @returns {number|null}
 */
function codexLegacyResultEnd(lines, currentIndex) {
  if (!CODEX_LEGACY_OUTCOME.test(codexLegacyPayload(lines[currentIndex]))) return null;
  let depth = 0;
  let inString = false;
  let escaped = false;
  let started = false;
  const content = [];
  for (let index = currentIndex + 1; index < lines.length; index++) {
    const line = lines[index];
    content.push(line);
    for (const character of line) {
      if (!started) {
        if (/\s/.test(character)) continue;
        if (character !== "{" && character !== "[") return null;
        started = true;
      }
      if (inString) {
        if (escaped) escaped = false;
        else if (character === "\\") escaped = true;
        else if (character === '"') inString = false;
      } else if (character === '"') inString = true;
      else if (character === "{" || character === "[") depth++;
      else if (character === "}" || character === "]") depth--;
    }
    if (started && depth === 0 && !inString) {
      try {
        JSON.parse(content.join("\n"));
        return index;
      } catch {
        return null;
      }
    }
  }
  return null;
}

/** @param {string} line @returns {number|undefined} */
function extractCodexLegacyTokens(line) {
  const payload = codexLegacyPayload(line);
  const totalMatch = payload.startsWith("total_tokens:") || payload.includes("TokenCount") ? payload.match(/\btotal_tokens:\s*([\d,]+)/) : null;
  const match = payload.match(/^tokens\s+used:\s*([\d,]+)\s*$/i) ?? totalMatch;
  if (!match) return undefined;
  if (!/^\d+(?:,\d{3})*$/.test(match[1])) return undefined;
  const count = Number(match[1].replace(/,/g, ""));
  return Number.isSafeInteger(count) && count >= 0 ? count : undefined;
}

/** @param {string} line @returns {string} */
function codexLegacyPayload(line) {
  return line.replace(/^\[[^\]]+\]\s+/, "").replace(/^\d{4}-\d{2}-\d{2}T\S+\s+(?:DEBUG|INFO|WARN|ERROR)\s+\S+:\s*/, "");
}

/**
 * Legacy presentation uses lookahead; canonical observations must instead remain
 * at their source positions, including orphan outcomes and dangling invocations.
 * @param {string[]} lines
 * @param {string|null} model
 * @param {{inlineTokens: number, hasTokenSnapshot: boolean}} [accounting]
 * @returns {import("./types/agent_session").AgentSession}
 */
function parseCodexLegacySession(lines, model, accounting = { inlineTokens: 0, hasTokenSnapshot: false }) {
  /** @type {import("./types/agent_session").AgentSession} */
  const events = [];
  const metadata = /^(?:OpenAI Codex|--------|workdir:|model:|provider:|approval:|sandbox:|reasoning effort:|reasoning summaries:|DEBUG codex|INFO codex|\d{4}-\d{2}-\d{2}T[\d:.]+Z\s+(?:DEBUG|INFO|WARN|ERROR))/;
  const tool = /^tool\s+([\w-]+)\.([\w-]+)\((.*)\)$/;
  const oldTool = /^ToolCall:\s+([\w-]+)__([\w-]+)\s+(.*)$/;
  const exec = /^exec\s+(?:bash\s+-lc\s+'([^']*)'|(.+?)(?: in \/.*)?)$/;
  const outcome = CODEX_LEGACY_OUTCOME;
  const boundary = line => {
    const payload = codexLegacyPayload(line);
    return (
      /^(?:thinking|codex|user|tokens used)$/.test(payload) ||
      metadata.test(line) ||
      tool.test(payload) ||
      oldTool.test(payload) ||
      exec.test(payload) ||
      outcome.test(payload) ||
      /^(?:ERROR:|Reconnecting\.\.\.)/.test(payload) ||
      extractCodexLegacyTokens(line) !== undefined
    );
  };
  const emit = (source, type, data) => events.push(createSessionEvent(source, type, data));
  const cwd = lines.find(line => line.startsWith("workdir: "))?.slice("workdir: ".length);
  if (model !== null || cwd !== undefined || lines.some(line => line.startsWith("OpenAI Codex"))) {
    emit({}, "session.init", { sourceEngine: "codex", model: model ?? undefined, cwd });
  }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const payload = codexLegacyPayload(line);
    const timestamp = line.match(/^\[([^\]]+)\]/)?.[1] ?? line.match(/^(\d{4}-\d{2}-\d{2}T\S+)/)?.[1];
    const source = timestamp === undefined ? {} : { timestamp };
    const tokens = extractCodexLegacyTokens(line);
    if (tokens !== undefined) {
      if (/^tokens\s+used:/i.test(payload)) {
        accounting.inlineTokens += tokens;
        if (!accounting.hasTokenSnapshot && Number.isSafeInteger(accounting.inlineTokens)) emit(source, "session.result", { usage: { total_tokens: accounting.inlineTokens } });
      } else {
        accounting.hasTokenSnapshot = true;
        emit(source, "session.result", { usage: { total_tokens: tokens } });
      }
      continue;
    }
    if (
      metadata.test(line) &&
      !outcome.test(payload) &&
      !tool.test(payload) &&
      !oldTool.test(payload) &&
      !exec.test(payload) &&
      !["thinking", "codex", "user"].includes(payload) &&
      !payload.startsWith("ERROR:") &&
      !payload.startsWith("Reconnecting...")
    )
      continue;
    if (["thinking", "codex", "user"].includes(payload)) {
      const text = [];
      while (i + 1 < lines.length && !boundary(lines[i + 1])) text.push(lines[++i]);
      emit(source, payload === "thinking" ? "assistant.reasoning" : payload === "user" ? "user.message" : "assistant.message", { content: text.join("\n") });
      continue;
    }
    const invocation = payload.match(tool) ?? payload.match(oldTool);
    const command = payload.match(exec);
    if (invocation) {
      let input = invocation[3];
      try {
        input = JSON.parse(input);
      } catch {
        // Intentional recovery: preserve the original argument text when JSON is malformed or partial.
      }
      emit(source, "tool.execution_start", { toolName: invocation[2], mcpServerName: invocation[1], input });
    } else if (command) {
      const text = command[1] ?? command[2];
      emit(source, "tool.execution_start", { toolName: "bash", input: { command: text }, command: text });
    } else {
      const completion = payload.match(outcome);
      if (completion) {
        const output = [];
        const resultEnd = codexLegacyResultEnd(lines, i);
        if (resultEnd !== null) {
          output.push(...lines.slice(i + 1, resultEnd + 1));
          i = resultEnd;
        } else while (i + 1 < lines.length && !boundary(lines[i + 1])) output.push(lines[++i]);
        const outputText = output.join("\n");
        let resultFailed = false;
        try {
          resultFailed = sessionToolSuccess(JSON.parse(outputText)) === false;
        } catch {
          // Plain command output is not a structured MCP result.
        }
        emit(source, "tool.execution_complete", {
          toolName: completion[3] !== undefined ? "bash" : completion[2],
          mcpServerName: completion[1],
          success: !["failed", "failure"].includes(completion[4]) && !resultFailed,
          durationMs: Number(completion[5]) * (completion[6] === "s" ? 1000 : 1),
          output: outputText,
        });
      } else if (payload.startsWith("ERROR:")) {
        emit(source, "session.result", { errors: [payload.replace(/^ERROR: ?/, "")] });
      } else {
        const retry = payload.match(/^Reconnecting\.\.\.\s+(\d+)\/(\d+)\s*\((.*)\)$/);
        if (retry) emit({ ...source, reconnectAttempt: Number(retry[1]), maxReconnects: Number(retry[2]) }, "session.result", { errors: [retry[3]] });
        else if (payload === "tokens used") {
          const count = extractCodexLegacyTokens(`tokens used: ${lines[i + 1] ?? ""}`);
          if (count !== undefined) {
            i++;
            accounting.hasTokenSnapshot = true;
            emit(source, "session.result", { usage: { total_tokens: count } });
          }
        }
      }
    }
  }
  return events;
}

/**
 * Parse codex log content and format as markdown
 * @param {string} logContent - The raw log content to parse
 * @param {Array<any>} [parsed] - Already parsed records, when available
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseCodexLog(logContent, parsed = parseLogEntries(logContent) ?? []) {
  // Newer Codex CLI versions emit a structured JSONL event stream rather than the
  // legacy pretty-printed format. Route those to the dedicated JSONL parser.
  if (logContent && isCodexJsonlFormat([], parsed)) {
    return parseCodexJsonl(logContent, parsed);
  }
  if (!logContent) {
    return {
      markdown: buildStepSummaryDetailsSection("Commands and Tools", "No log content provided.") + buildStepSummaryDetailsSection("Reasoning", "Unable to parse reasoning from log."),
      logEntries: [],
      mcpFailures: [],
      maxTurnsHit: false,
    };
  }

  const lines = logContent.split("\n");

  // Look-ahead window size for finding tool results
  // New format has verbose debug logs, so requires larger window
  const LOOKAHEAD_WINDOW = 50;

  let markdown = "";

  // Extract MCP initialization information
  const mcpInfo = extractMCPInitialization(lines);
  if (mcpInfo.hasInfo) {
    markdown += buildStepSummaryDetailsSection("Initialization", mcpInfo.markdown);
  }

  // Extract error messages (e.g., model access blocked, cyber_policy_violation)
  const errorInfo = extractCodexErrorMessages(lines);
  if (errorInfo.hasErrors) {
    markdown += "<details>\n<summary>Errors</summary>\n\n";
    for (const message of errorInfo.messages) {
      markdown += `> ${message}\n\n`;
    }
    if (errorInfo.reconnectCount > 0) {
      markdown += `> Reconnect attempts: ${errorInfo.reconnectCount}/${errorInfo.maxReconnects}\n\n`;
    }
    markdown += "</details>\n\n";
  }

  markdown += "<details>\n<summary>Reasoning</summary>\n\n";

  // Second pass: process full conversation flow with interleaved reasoning and tools
  let inThinkingSection = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Skip metadata lines (including Rust debug lines)
    if (
      line.includes("OpenAI Codex") ||
      line.startsWith("--------") ||
      line.includes("workdir:") ||
      line.includes("model:") ||
      line.includes("provider:") ||
      line.includes("approval:") ||
      line.includes("sandbox:") ||
      line.includes("reasoning effort:") ||
      line.includes("reasoning summaries:") ||
      line.includes("tokens used:") ||
      line.includes("DEBUG codex") ||
      line.includes("INFO codex") ||
      line.match(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z\s+(DEBUG|INFO|WARN|ERROR)/)
    ) {
      continue;
    }

    // Thinking section starts with standalone "thinking" line
    if (line.trim() === "thinking") {
      inThinkingSection = true;
      continue;
    }

    // Tool call line "tool github.list_pull_requests(...)" (new Codex format without timestamp prefix)
    const toolMatch = line.match(/^tool\s+(\w+)\.(\w+)\((.*)\)$/);
    if (toolMatch) {
      inThinkingSection = false;

      const server = toolMatch[1];
      const toolName = toolMatch[2];
      const params = toolMatch[3];

      // Look ahead to find the result status and response
      let statusIcon = "❓"; // Unknown by default
      let response = "";
      for (let j = i + 1; j < Math.min(i + LOOKAHEAD_WINDOW, lines.length); j++) {
        const nextLine = lines[j];
        if (nextLine.includes(`${server}.${toolName}(`) && (nextLine.includes("success in") || nextLine.includes("failed in"))) {
          const isError = nextLine.includes("failed in");
          statusIcon = isError ? "❌" : "✅";

          // Extract response - it's the JSON object following this line
          let jsonLines = [];
          let braceCount = 0;
          let inJson = false;

          for (let k = j + 1; k < Math.min(j + LOOKAHEAD_WINDOW, lines.length); k++) {
            const respLine = lines[k];

            // Stop if we hit the next tool call or tokens used
            if (respLine.match(/^tool\s+\w+\.\w+\(/) || respLine.includes("ToolCall:") || respLine.includes("tokens used")) {
              break;
            }

            // Count braces to track JSON boundaries
            for (const char of respLine) {
              if (char === "{") {
                braceCount++;
                inJson = true;
              } else if (char === "}") {
                braceCount--;
              }
            }

            if (inJson) {
              jsonLines.push(respLine);
            }

            if (inJson && braceCount === 0) {
              break;
            }
          }

          response = jsonLines.join("\n");
          break;
        }
      }

      markdown += `${statusIcon} ${server}::${toolName}(...)\n\n`;
      continue;
    }

    // Process thinking content (filter out timestamp lines and very short lines)
    if (inThinkingSection && line.trim().length > 20 && !line.match(/^\d{4}-\d{2}-\d{2}T/)) {
      const trimmed = line.trim();
      // Add thinking content directly to markdown with open circle icon and italic styling
      markdown += `<sub><em>${trimmed}</em></sub>\n\n`;
    }
  }

  markdown += "</details>\n\n<details>\n<summary>Commands and Tools</summary>\n\n";

  // First pass: collect tool calls with details
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Match: tool server.method(params) or ToolCall: server__method params
    const toolMatch = line.match(/^\[.*?\]\s+tool\s+(\w+)\.(\w+)\((.+)\)/) || line.match(/ToolCall:\s+(\w+)__(\w+)\s+(\{.+\})/);

    // Also match: exec bash -lc 'command' in /path
    const bashMatch = line.match(/^\[.*?\]\s+exec\s+bash\s+-lc\s+'([^']+)'/);

    if (toolMatch) {
      const server = toolMatch[1];
      const toolName = toolMatch[2];
      const params = toolMatch[3];

      // Look ahead to find the result
      let statusIcon = "❓";
      let response = "";
      let isError = false;

      for (let j = i + 1; j < Math.min(i + LOOKAHEAD_WINDOW, lines.length); j++) {
        const nextLine = lines[j];

        // Check for result line: server.method(...) success/failed in Xms:
        if (nextLine.includes(`${server}.${toolName}(`) && (nextLine.includes("success in") || nextLine.includes("failed in"))) {
          isError = nextLine.includes("failed in");
          statusIcon = isError ? "❌" : "✅";

          // Extract response - it's the JSON object following this line
          let jsonLines = [];
          let braceCount = 0;
          let inJson = false;

          for (let k = j + 1; k < Math.min(j + 30, lines.length); k++) {
            const respLine = lines[k];

            // Stop if we hit the next tool call or tokens used
            if (respLine.includes("tool ") || respLine.includes("ToolCall:") || respLine.includes("tokens used")) {
              break;
            }

            // Count braces to track JSON boundaries
            for (const char of respLine) {
              if (char === "{") {
                braceCount++;
                inJson = true;
              } else if (char === "}") {
                braceCount--;
              }
            }

            if (inJson) {
              jsonLines.push(respLine);
            }

            if (inJson && braceCount === 0) {
              break;
            }
          }

          response = jsonLines.join("\n");
          break;
        }
      }

      // Format the tool call with HTML details
      markdown += formatCodexToolCall(server, toolName, params, response, statusIcon);
    } else if (bashMatch) {
      const command = bashMatch[1];

      // Look ahead to find the result
      let statusIcon = "❓";
      let response = "";
      let isError = false;

      for (let j = i + 1; j < Math.min(i + LOOKAHEAD_WINDOW, lines.length); j++) {
        const nextLine = lines[j];

        // Check for bash result line: bash -lc 'command' succeeded/failed in Xms:
        if (nextLine.includes("bash -lc") && (nextLine.includes("succeeded in") || nextLine.includes("failed in"))) {
          isError = nextLine.includes("failed in");
          statusIcon = isError ? "❌" : "✅";

          // Extract response - it's the plain text following this line
          let responseLines = [];

          for (let k = j + 1; k < Math.min(j + 20, lines.length); k++) {
            const respLine = lines[k];

            // Stop if we hit the next tool call, exec, or tokens used
            if (respLine.includes("tool ") || respLine.includes("exec ") || respLine.includes("ToolCall:") || respLine.includes("tokens used") || respLine.includes("thinking")) {
              break;
            }

            responseLines.push(respLine);
          }

          response = responseLines.join("\n");
          break;
        }
      }

      // Format the bash command with HTML details
      markdown += formatCodexBashCall(command, response, statusIcon);
    }
  }

  // Add Information section
  markdown += "</details>\n\n<details>\n<summary>Information</summary>\n\n";

  // Extract metadata from Codex logs
  const model = extractCodexModel(logContent);
  const canonicalLogEntries = parseCodexLegacySession(lines, model);
  const totalTokens = projectSessionResult(canonicalLogEntries)?.usage?.total_tokens;

  if (totalTokens !== undefined) {
    markdown += `**Total Tokens Used:** ${totalTokens.toLocaleString()}\n\n`;
  }

  // Count tool calls
  const toolCalls = (logContent.match(/ToolCall:\s+\w+__\w+/g) || []).length;

  if (toolCalls > 0) {
    markdown += `**Tool Calls:** ${toolCalls}\n\n`;
  }
  markdown += "</details>\n\n";

  // Check for MCP failures
  const mcpFailures = mcpInfo.servers.filter(server => server.status === "failed").map(server => server.name);

  return {
    markdown,
    logEntries: canonicalLogEntries,
    mcpFailures,
    maxTurnsHit: false, // Codex doesn't have max-turns concept in logs
  };
}

/**
 * Format a Codex tool call with HTML details
 * Uses the shared formatToolCallAsDetails helper for consistent rendering across all engines.
 * @param {string} server - The server name (e.g., "github", "time")
 * @param {string} toolName - The tool name (e.g., "list_pull_requests")
 * @param {string} params - The parameters as JSON string
 * @param {string} response - The response as JSON string
 * @param {string} statusIcon - The status icon (✅, ❌, or ❓)
 * @returns {string} Formatted HTML details string
 */
function formatCodexToolCall(server, toolName, params, response, statusIcon) {
  // Calculate token estimate from params + response
  const totalTokens = estimateTokens(params) + estimateTokens(response);

  // Format metadata
  let metadata = "";
  if (totalTokens > 0) {
    metadata = `<code>~${totalTokens}t</code>`;
  }

  const summary = `<code>${server}::${toolName}</code>`;

  // Build sections array
  const sections = [];

  if (params && params.trim()) {
    sections.push({
      label: "Parameters",
      content: params,
      language: "json",
    });
  }

  if (response && response.trim()) {
    sections.push({
      label: "Response",
      content: response,
      language: "json",
    });
  }

  return formatToolCallAsDetails({
    summary,
    statusIcon,
    metadata,
    sections,
  });
}

/**
 * Format a Codex bash call with HTML details
 * Uses the shared formatToolCallAsDetails helper for consistent rendering across all engines.
 * @param {string} command - The bash command
 * @param {string} response - The response as plain text
 * @param {string} statusIcon - The status icon (✅, ❌, or ❓)
 * @returns {string} Formatted HTML details string
 */
function formatCodexBashCall(command, response, statusIcon) {
  // Calculate token estimate from command + response
  const totalTokens = estimateTokens(command) + estimateTokens(response);

  // Format metadata
  let metadata = "";
  if (totalTokens > 0) {
    metadata = `<code>~${totalTokens}t</code>`;
  }

  const summary = `<code>bash: ${truncateString(command, 60)}</code>`;

  // Build sections array
  const sections = [];

  sections.push({
    label: "Command",
    content: command,
    language: "bash",
  });

  if (response && response.trim()) {
    sections.push({
      label: "Output",
      content: response,
    });
  }

  return formatToolCallAsDetails({
    summary,
    statusIcon,
    metadata,
    sections,
  });
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parseCodexLog,
    parseCodexJsonl,
    isCodexJsonlFormat,
    formatCodexToolCall,
    formatCodexBashCall,
    extractMCPInitialization,
    extractCodexErrorMessages,
    extractCodexModel,
  };
}
