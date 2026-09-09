// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser, truncateString, estimateTokens, formatToolCallAsDetails, buildStepSummaryDetailsSection, convertLegacyLogEntriesToCopilotEvents } = require("./log_parser_shared.cjs");

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
 * Convert parsed Codex data to logEntries format for plain text rendering
 * @param {Array<{type: string, content?: string, toolName?: string, params?: string, response?: string, statusIcon?: string}>} parsedData - Parsed Codex log data
 * @returns {Array} logEntries array in the format expected by generatePlainTextSummary
 */
function convertToLogEntries(parsedData) {
  const logEntries = [];

  for (const item of parsedData) {
    if (item.type === "thinking") {
      // Add thinking as assistant reasoning content (distinct from regular text)
      logEntries.push({
        type: "assistant",
        message: {
          content: [
            {
              type: "thinking",
              thinking: item.content,
            },
          ],
        },
      });
    } else if (item.type === "text") {
      // Add a plain agent message as assistant text content
      logEntries.push({
        type: "assistant",
        message: {
          content: [
            {
              type: "text",
              text: item.content,
            },
          ],
        },
      });
    } else if (item.type === "tool") {
      // Add tool use as assistant content
      const toolUseId = `tool_${logEntries.length}`;

      // Parse params - it might be a plain JSON string or need parsing
      /** @type {any} */
      let inputObj = {};
      if (item.params) {
        try {
          inputObj = JSON.parse(item.params);
        } catch (e) {
          // If parsing fails, wrap it as a string
          inputObj = { params: item.params };
        }
      }

      logEntries.push({
        type: "assistant",
        message: {
          content: [
            {
              type: "tool_use",
              id: toolUseId,
              name: item.toolName, // Already in server__method format
              input: inputObj,
            },
          ],
        },
      });

      // Add tool result as user content
      logEntries.push({
        type: "user",
        message: {
          content: [
            {
              type: "tool_result",
              tool_use_id: toolUseId,
              content: item.response || "",
              is_error: item.statusIcon === "❌",
            },
          ],
        },
      });
    } else if (item.type === "bash") {
      // Add bash command as tool use
      const toolUseId = `bash_${logEntries.length}`;
      logEntries.push({
        type: "assistant",
        message: {
          content: [
            {
              type: "tool_use",
              id: toolUseId,
              name: "Bash",
              input: { command: item.content },
            },
          ],
        },
      });

      // Add bash result as user content
      logEntries.push({
        type: "user",
        message: {
          content: [
            {
              type: "tool_result",
              tool_use_id: toolUseId,
              content: item.response || "",
              is_error: item.statusIcon === "❌",
            },
          ],
        },
      });
    }
  }

  return logEntries;
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
 * @returns {boolean} True if at least one Codex JSONL event line is present
 */
function isCodexJsonlFormat(lines) {
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("{")) continue;
    let event;
    try {
      event = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (event && typeof event === "object" && typeof event.type === "string" && /^(thread|turn|item)\./.test(event.type)) {
      return true;
    }
  }
  return false;
}

/**
 * Parse the Codex experimental JSONL event stream into the shared logEntries model.
 * Maps `item.completed` items (agent_message, reasoning, mcp_tool_call,
 * command_execution) and `turn.completed` usage onto the same `parsedData`
 * structure used by the legacy parser, so the common renderer is reused.
 * @param {string} logContent - The raw log content
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseCodexJsonl(logContent) {
  const DEFAULT_STATUS_ICON = "🔧";

  const lines = logContent.split("\n");
  const parsedData = [];
  /** @type {any} */
  let usage = null;
  let turnCount = 0;

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("{")) continue;
    let event;
    try {
      event = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (!event || typeof event !== "object") continue;

    if (event.type === "turn.completed") {
      turnCount++;
      if (event.usage && typeof event.usage === "object") {
        usage = event.usage;
      }
      continue;
    }

    // Only render completed items; `item.started` entries are interim duplicates.
    if (event.type !== "item.completed" || !event.item || typeof event.item !== "object") {
      continue;
    }

    const item = event.item;
    switch (item.type) {
      case "agent_message": {
        if (typeof item.text === "string" && item.text.trim()) {
          parsedData.push({ type: "text", content: item.text });
        }
        break;
      }
      case "reasoning": {
        const reasoning = typeof item.text === "string" ? item.text : typeof item.summary === "string" ? item.summary : "";
        if (reasoning.trim()) {
          parsedData.push({ type: "thinking", content: reasoning });
        }
        break;
      }
      case "mcp_tool_call": {
        const server = item.server || "mcp";
        const toolName = item.tool || "tool";
        const params = item.arguments != null ? JSON.stringify(item.arguments) : "";
        let response = "";
        if (item.error != null) {
          response = typeof item.error === "string" ? item.error : JSON.stringify(item.error);
        } else if (item.result != null) {
          response = typeof item.result === "string" ? item.result : JSON.stringify(item.result);
        }
        const isError = item.status === "failed" || item.error != null;
        parsedData.push({
          type: "tool",
          toolName: `${server}__${toolName}`,
          params,
          response,
          statusIcon: isError ? "❌" : "✅",
        });
        break;
      }
      case "command_execution": {
        const command = typeof item.command === "string" ? item.command : "";
        const response = typeof item.aggregated_output === "string" ? item.aggregated_output : "";
        const isError = item.status === "failed" || (typeof item.exit_code === "number" && item.exit_code !== 0);
        parsedData.push({
          type: "bash",
          content: command,
          response,
          statusIcon: isError ? "❌" : "✅",
        });
        break;
      }
      case "error": {
        const rawMessage = typeof item.message === "string" ? item.message : JSON.stringify(item);
        const message = rawMessage.trim();
        if (message) {
          parsedData.push({ type: "error", content: message });
        }
        break;
      }
      default:
        break;
    }
  }

  const errorMessages = parsedData.filter(item => item.type === "error").map(item => item.content);

  // Build markdown so the parser returns a truthy result and core.info has a
  // readable fallback. The step summary itself is rendered from logEntries.
  let markdown = "";
  if (errorMessages.length > 0) {
    markdown += "<details>\n<summary>Errors</summary>\n\n";
    for (const message of errorMessages) {
      markdown += `> ${message}\n\n`;
    }
    markdown += "</details>\n\n";
  }
  markdown += "<details>\n<summary>Reasoning</summary>\n\n";
  for (const item of parsedData) {
    if (item.type === "text") {
      markdown += `${item.content}\n\n`;
    } else if (item.type === "thinking") {
      markdown += `<sub><em>${item.content}</em></sub>\n\n`;
    }
  }
  markdown += "</details>\n\n<details>\n<summary>Commands and Tools</summary>\n\n";
  for (const item of parsedData) {
    if (item.type === "tool") {
      const toolNameValue = item.toolName || "unknown-server__unknown-tool";
      const [server, toolName] = toolNameValue.split("__", 2);
      markdown += formatCodexToolCall(server, toolName, item.params || "", item.response || "", item.statusIcon || DEFAULT_STATUS_ICON);
    } else if (item.type === "bash") {
      markdown += formatCodexBashCall(item.content || "", item.response || "", item.statusIcon || DEFAULT_STATUS_ICON);
    }
  }
  markdown += "</details>\n\n<details>\n<summary>Information</summary>\n\n";
  if (usage) {
    const inputTokens = typeof usage.input_tokens === "number" ? usage.input_tokens : 0;
    const outputTokens = typeof usage.output_tokens === "number" ? usage.output_tokens : 0;
    const totalTokens = inputTokens + outputTokens;
    if (totalTokens > 0) {
      markdown += `**Total Tokens Used:** ${totalTokens.toLocaleString()}\n\n`;
    }
  }
  markdown += "</details>\n\n";

  const logEntries = convertToLogEntries(parsedData);

  // Prepend a system init entry so the session preview renders (matches the
  // legacy path and the Claude/Copilot/Gemini parsers).
  const model = extractCodexModel(logContent);
  logEntries.unshift({
    type: "system",
    subtype: "init",
    model: model || undefined,
  });

  // Surface token usage, turn count, and error messages via a result entry so
  // Statistics, the Information section's Errors list, and the OTEL telemetry
  // enrichment (agent-stdio.log result line) are populated.
  if (usage || errorMessages.length > 0) {
    logEntries.push({
      type: "result",
      num_turns: turnCount > 0 ? turnCount : undefined,
      usage: usage
        ? {
            input_tokens: typeof usage.input_tokens === "number" ? usage.input_tokens : undefined,
            output_tokens: typeof usage.output_tokens === "number" ? usage.output_tokens : undefined,
            cache_read_input_tokens: typeof usage.cached_input_tokens === "number" ? usage.cached_input_tokens : undefined,
          }
        : undefined,
      errors: errorMessages.length > 0 ? errorMessages : undefined,
    });
  }

  const canonicalLogEntries = convertLegacyLogEntriesToCopilotEvents(logEntries, { sourceEngine: "codex" });

  return {
    markdown,
    logEntries: canonicalLogEntries,
    mcpFailures: [],
    maxTurnsHit: false,
  };
}

/**
 * Parse codex log content and format as markdown
 * @param {string} logContent - The raw log content to parse
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseCodexLog(logContent) {
  // Newer Codex CLI versions emit a structured JSONL event stream rather than the
  // legacy pretty-printed format. Route those to the dedicated JSONL parser.
  if (logContent && isCodexJsonlFormat(logContent.split("\n"))) {
    return parseCodexJsonl(logContent);
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
  const parsedData = []; // Array to collect structured data for logEntries conversion

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
  let thinkingContent = []; // Collect thinking content in chunks

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
      // Save previous thinking content if any
      if (thinkingContent.length > 0) {
        parsedData.push({
          type: "thinking",
          content: thinkingContent.join("\n"),
        });
        thinkingContent = [];
      }
      inThinkingSection = true;
      continue;
    }

    // Tool call line "tool github.list_pull_requests(...)" (new Codex format without timestamp prefix)
    const toolMatch = line.match(/^tool\s+(\w+)\.(\w+)\((.*)\)$/);
    if (toolMatch) {
      // Save previous thinking content if any
      if (inThinkingSection && thinkingContent.length > 0) {
        parsedData.push({
          type: "thinking",
          content: thinkingContent.join("\n"),
        });
        thinkingContent = [];
      }
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

      // Add to parsedData so this tool call appears in logEntries for the common renderer
      parsedData.push({
        type: "tool",
        toolName: `${server}__${toolName}`,
        params,
        response,
        statusIcon,
      });

      markdown += `${statusIcon} ${server}::${toolName}(...)\n\n`;
      continue;
    }

    // Process thinking content (filter out timestamp lines and very short lines)
    if (inThinkingSection && line.trim().length > 20 && !line.match(/^\d{4}-\d{2}-\d{2}T/)) {
      const trimmed = line.trim();
      thinkingContent.push(trimmed);
      // Add thinking content directly to markdown with open circle icon and italic styling
      markdown += `<sub><em>${trimmed}</em></sub>\n\n`;
    }
  }

  // Save any remaining thinking content
  if (thinkingContent.length > 0) {
    parsedData.push({
      type: "thinking",
      content: thinkingContent.join("\n"),
    });
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

      // Collect data for logEntries conversion
      parsedData.push({
        type: "tool",
        toolName: `${server}__${toolName}`,
        params,
        response,
        statusIcon,
      });

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

          response = responseLines.join("\n").trim();
          break;
        }
      }

      // Collect data for logEntries conversion
      parsedData.push({
        type: "bash",
        content: command,
        response,
        statusIcon,
      });

      // Format the bash command with HTML details
      markdown += formatCodexBashCall(command, response, statusIcon);
    }
  }

  // Add Information section
  markdown += "</details>\n\n<details>\n<summary>Information</summary>\n\n";

  // Extract metadata from Codex logs
  let totalTokens = 0;

  // TokenCount(TokenCountEvent { ... total_tokens: 13281 ...
  const tokenCountMatches = logContent.matchAll(/total_tokens:\s*(\d+)/g);
  for (const match of tokenCountMatches) {
    const tokens = parseInt(match[1], 10);
    totalTokens = Math.max(totalTokens, tokens); // Use the highest value (final total)
  }

  // Also check for "tokens used\n<number>" at the end (number may have commas)
  const finalTokensMatch = logContent.match(/tokens used\n([\d,]+)/);
  if (finalTokensMatch) {
    // Remove commas before parsing
    totalTokens = parseInt(finalTokensMatch[1].replace(/,/g, ""), 10);
  }

  if (totalTokens > 0) {
    markdown += `**Total Tokens Used:** ${totalTokens.toLocaleString()}\n\n`;
  }

  // Count tool calls
  const toolCalls = (logContent.match(/ToolCall:\s+\w+__\w+/g) || []).length;

  if (toolCalls > 0) {
    markdown += `**Tool Calls:** ${toolCalls}\n\n`;
  }
  markdown += "</details>\n\n";

  // Convert parsed data to logEntries format
  const logEntries = convertToLogEntries(parsedData);

  // Always prepend a system init entry so the session preview is shown even for
  // failed or sparse runs (matches behaviour of Claude, Copilot, and Gemini parsers).
  const model = extractCodexModel(logContent);
  logEntries.unshift({
    type: "system",
    subtype: "init",
    model: model || undefined,
  });

  // When there are no tool calls or thinking entries, surface error messages in the
  // preview so users can see why the session failed.
  const hasConversationEntries = logEntries.some(e => e.type !== "system");
  if (!hasConversationEntries && errorInfo.hasErrors) {
    for (const message of errorInfo.messages) {
      logEntries.push({
        type: "assistant",
        message: {
          content: [{ type: "text", text: message }],
        },
      });
    }
    if (errorInfo.reconnectCount > 0) {
      logEntries.push({
        type: "assistant",
        message: {
          content: [{ type: "text", text: `Reconnect attempts: ${errorInfo.reconnectCount}/${errorInfo.maxReconnects}` }],
        },
      });
    }
  }

  // Check for MCP failures
  const mcpFailures = mcpInfo.servers.filter(server => server.status === "failed").map(server => server.name);

  const canonicalLogEntries = convertLegacyLogEntriesToCopilotEvents(logEntries, { sourceEngine: "codex" });

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
