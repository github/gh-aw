// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");
const { redactMCPScriptLogMessage } = require("./diagnostic_sanitization.cjs");

/**
 * Parses mcp-scripts MCP server logs and creates a step summary
 * Log format: [timestamp] [server-name] message
 */

/**
 * Main function to parse and display mcp-scripts logs
 */
async function main() {
  try {
    // Get the mcp-scripts logs directory path
    const mcpScriptsLogsDir = require("./runtime_paths.cjs").runtimePath("mcp-scripts/logs");

    if (!fs.existsSync(mcpScriptsLogsDir)) {
      core.info(`No mcp-scripts logs directory found at: ${mcpScriptsLogsDir}`);
      return;
    }

    // Find all log files
    const files = fs.readdirSync(mcpScriptsLogsDir).filter(file => file.endsWith(".log"));

    if (files.length === 0) {
      core.info(`No mcp-scripts log files found in: ${mcpScriptsLogsDir}`);
      return;
    }

    core.info(`Found ${files.length} mcp-scripts log file(s)`);

    // Parse all log files and aggregate results
    const allLogEntries = [];

    for (const file of files) {
      const filePath = path.join(mcpScriptsLogsDir, file);
      core.info(`Parsing mcp-scripts log: ${file}`);

      const content = fs.readFileSync(filePath, "utf8");
      const lines = content.split("\n").filter(line => line.trim());

      for (const line of lines) {
        const entry = parseMCPScriptsLogLine(line);
        if (entry) {
          allLogEntries.push(entry);
        }
      }
    }

    if (allLogEntries.length === 0) {
      core.info("No parseable log entries found in mcp-scripts logs");
      return;
    }

    // Generate plain text summary for core.info (Copilot CLI style)
    const plainTextSummary = generatePlainTextSummary(allLogEntries);
    core.info(plainTextSummary);

    // Generate step summary
    const summary = generateMCPScriptsSummary(allLogEntries);
    await core.summary.addRaw(summary).write();
  } catch (error) {
    core.setFailed(`${ERR_PARSE}: ${getErrorMessage(error)}`);
  }
}

/**
 * Parses a single mcp-scripts log line
 * Expected format: [timestamp] [server-name] message
 * @param {string} line - Log line to parse
 * @returns {Object|null} Parsed log entry or null if invalid
 */
function parseMCPScriptsLogLine(line) {
  // Match format: [timestamp] [server-name] message
  const match = line.match(/^\[([^\]]+)\]\s+\[([^\]]+)\]\s+(.+)$/);

  if (!match) {
    // Return unparsed line as-is for display
    return {
      timestamp: null,
      serverName: null,
      message: line.trim(),
      raw: true,
    };
  }

  const [, timestamp, serverName, message] = match;

  return {
    timestamp: timestamp.trim(),
    serverName: serverName.trim(),
    message: message.trim(),
    raw: false,
  };
}

/**
 * Generates a lightweight plain text summary optimized for console output.
 * This is designed for core.info output, similar to agent logs style.
 *
 * @param {Array<Object>} logEntries - Parsed log entries
 * @returns {string} Plain text summary for console output
 */
function generatePlainTextSummary(logEntries) {
  logEntries = logEntries.map(entry => ({ ...entry, message: redactMCPScriptLogMessage(entry.message || "") }));
  const lines = [];

  const truncate = (value, max = 120) => {
    if (!value) return "";
    const normalized = String(value).replace(/\s+/g, " ").trim();
    if (normalized.length <= max) return normalized;
    return `${normalized.slice(0, max - 3)}...`;
  };

  const parseJSON = value => {
    try {
      return JSON.parse(value);
    } catch {
      return null;
    }
  };

  const extractToolAndPayload = (message, marker) => {
    const markerIndex = message.indexOf(marker);
    if (markerIndex === -1) return null;

    const prefix = message.slice(0, markerIndex).trimEnd();
    const payload = message.slice(markerIndex + marker.length).trim();

    const toolMatch = prefix.match(/\[\s*([^\]]+)\s*\]\s*$/);
    if (!toolMatch) return null;

    return {
      tool: toolMatch[1].trim(),
      payload,
    };
  };

  /**
   * @typedef {Object} RenderedToolCall
   * @property {string} tool
   * @property {string} serverName
   * @property {string} argsDisplay
   * @property {string} resultPreview
   */

  /** @type {Array<RenderedToolCall>} */
  const renderedCalls = [];
  /** @type {Map<string, number[]>} */
  const pendingByTool = new Map();
  const diagnostics = [];

  const addPending = (tool, index) => {
    const key = tool.toLowerCase();
    const pending = pendingByTool.get(key);
    if (pending) {
      pending.push(index);
      return;
    }
    pendingByTool.set(key, [index]);
  };

  const consumePending = tool => {
    const key = tool.toLowerCase();
    const pending = pendingByTool.get(key);
    if (!pending || pending.length === 0) return -1;
    const index = pending.shift();
    if (pending.length === 0) {
      pendingByTool.delete(key);
    }
    return typeof index === "number" ? index : -1;
  };

  for (const entry of logEntries) {
    const message = (entry.message || "").trim();
    if (!message) continue;

    // Parse: "  [gh] Invoking handler with args: { ... }"
    const invokingPayload = extractToolAndPayload(message, "Invoking handler with args:");
    if (invokingPayload) {
      const tool = invokingPayload.tool;
      const argsDisplay = " · args: [REDACTED]";
      const callIndex = renderedCalls.push({
        tool,
        serverName: entry.serverName || "mcpscripts",
        argsDisplay,
        resultPreview: "",
      });
      addPending(tool, callIndex - 1);
      continue;
    }

    // Parse: "callBackendTool ... toolName=gh, args=map[args:pr view ...]"
    const backendToolPrefix = "toolName=";
    const backendArgsPrefix = "args=";
    const backendToolIndex = message.indexOf(backendToolPrefix);
    const backendArgsIndex = message.indexOf(backendArgsPrefix);
    if (backendToolIndex !== -1 && backendArgsIndex !== -1 && backendArgsIndex > backendToolIndex) {
      const toolPart = message.slice(backendToolIndex + backendToolPrefix.length, backendArgsIndex);
      const tool = toolPart.replace(",", "").trim();
      const argsDisplay = " · args: [REDACTED]";
      const callIndex = renderedCalls.push({
        tool,
        serverName: entry.serverName || "mcpscripts",
        argsDisplay,
        resultPreview: "",
      });
      addPending(tool, callIndex - 1);
      continue;
    }

    // Parse: "  [gh] Serialized result: {...}"
    const serializedResultPayload = extractToolAndPayload(message, "Serialized result:");
    if (serializedResultPayload) {
      const tool = serializedResultPayload.tool;
      const resultRaw = serializedResultPayload.payload;
      const parsedResult = parseJSON(resultRaw);
      const preview = parsedResult ? truncate(JSON.stringify(parsedResult), 110) : truncate(resultRaw, 110);
      const callIndex = consumePending(tool);
      if (callIndex >= 0) {
        renderedCalls[callIndex].resultPreview = preview;
      }
      continue;
    }

    if (/\b(error|failed)\b/i.test(message)) {
      diagnostics.push(`✗ ${truncate(message, 150)}`);
    }
  }

  if (renderedCalls.length === 0) {
    // Fallback to raw logs when no recognizable tool calls exist.
    let lineCount = 0;
    for (const entry of logEntries) {
      if (lineCount >= 200) {
        lines.push(`... (truncated, showing first 200 lines of ${logEntries.length} total entries)`);
        break;
      }
      if (entry.raw) {
        lines.push(truncate(entry.message, 150));
      } else {
        const server = entry.serverName ? `[${entry.serverName}] ` : "";
        lines.push(truncate(`${server}${entry.message}`.trim(), 150));
      }
      lineCount++;
    }
    return lines.join("\n");
  }

  for (const call of renderedCalls) {
    lines.push(`● ${call.tool} (MCP: ${call.serverName || "mcpscripts"})${call.argsDisplay}`);
    if (call.resultPreview) {
      lines.push(`  └ ${call.resultPreview}`);
    }
  }

  if (diagnostics.length > 0) {
    lines.push("");
    lines.push("Additional MCP diagnostics:");
    lines.push(...diagnostics.slice(0, 20));
    if (diagnostics.length > 20) {
      lines.push(`... (${diagnostics.length - 20} more diagnostics)`);
    }
  }

  return lines.join("\n");
}

/**
 * Generates a markdown summary of mcp-scripts logs
 * @param {Array<Object>} logEntries - Parsed log entries
 * @returns {string} Markdown summary
 */
function generateMCPScriptsSummary(logEntries) {
  logEntries = logEntries.map(entry => ({ ...entry, message: redactMCPScriptLogMessage(entry.message || "") }));
  const summary = [];

  // Count events by type
  const eventCounts = {
    startup: 0,
    toolRegistration: 0,
    toolExecution: 0,
    errors: 0,
    other: 0,
  };

  const errors = [];
  const toolCalls = [];

  for (const entry of logEntries) {
    const msg = entry.message.toLowerCase();

    // Categorize log entries
    if (msg.includes("starting mcp-scripts") || msg.includes("server started")) {
      eventCounts.startup++;
    } else if (msg.includes("registering tool") || msg.includes("tool registration")) {
      eventCounts.toolRegistration++;
    } else if (msg.includes("calling handler") || msg.includes("handler returned")) {
      eventCounts.toolExecution++;
      if (msg.includes("calling handler")) {
        // Extract tool name from message like "Calling handler for tool: my-tool"
        const toolMatch = entry.message.match(/tool:\s*(\S+)/i);
        if (toolMatch) {
          toolCalls.push({
            tool: toolMatch[1],
            timestamp: entry.timestamp,
          });
        }
      }
    } else if (msg.includes("error") || msg.includes("failed")) {
      eventCounts.errors++;
      errors.push(entry);
    } else {
      eventCounts.other++;
    }
  }

  // Wrap entire section in a details tag
  summary.push("<details>");
  summary.push("<summary>MCP Scripts Server Logs</summary>\n");

  // Statistics
  summary.push("**Statistics**\n");
  summary.push("| Metric | Count |");
  summary.push("|--------|-------|");
  summary.push(`| Total Log Entries | ${logEntries.length} |`);
  summary.push(`| Startup Events | ${eventCounts.startup} |`);
  summary.push(`| Tool Registrations | ${eventCounts.toolRegistration} |`);
  summary.push(`| Tool Executions | ${eventCounts.toolExecution} |`);
  summary.push(`| Errors | ${eventCounts.errors} |`);
  summary.push(`| Other Events | ${eventCounts.other} |`);
  summary.push("");

  // Tool execution details (if any)
  if (toolCalls.length > 0) {
    summary.push("**Tool Executions**\n");
    summary.push("<details>");
    summary.push("<summary>View tool execution details</summary>\n");
    summary.push("| Time | Tool Name |");
    summary.push("|------|-----------|");
    for (const call of toolCalls) {
      const time = call.timestamp ? new Date(call.timestamp).toLocaleTimeString() : "N/A";
      summary.push(`| ${time} | \`${call.tool}\` |`);
    }
    summary.push("\n</details>\n");
  }

  // Errors (if any)
  if (errors.length > 0) {
    summary.push("**Errors**\n");
    summary.push("<details>");
    summary.push("<summary>View error details</summary>\n");
    summary.push("```");
    for (const error of errors) {
      const time = error.timestamp ? `[${error.timestamp}]` : "";
      const server = error.serverName ? `[${error.serverName}]` : "";
      summary.push(`${time} ${server} ${error.message}`);
    }
    summary.push("```");
    summary.push("\n</details>\n");
  }

  // Full log details (collapsed by default)
  summary.push("**Full Logs**\n");
  summary.push("<details>");
  summary.push("<summary>View full mcp-scripts logs</summary>\n");
  summary.push("```");
  for (const entry of logEntries) {
    if (entry.raw) {
      // Display unparsed lines as-is
      summary.push(entry.message);
    } else {
      const time = entry.timestamp ? `[${entry.timestamp}]` : "";
      const server = entry.serverName ? `[${entry.serverName}]` : "";
      summary.push(`${time} ${server} ${entry.message}`);
    }
  }
  summary.push("```");
  summary.push("</details>");

  // Close the outer details tag
  summary.push("\n</details>");

  return summary.join("\n");
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parseMCPScriptsLogLine,
    generateMCPScriptsSummary,
    generatePlainTextSummary,
  };
}

// Run main if called directly
if (require.main === module) {
  main().catch(err => {
    console.error(err instanceof Error && err.stack ? err.stack : getErrorMessage(err));
    process.exitCode = 1;
  });
}
