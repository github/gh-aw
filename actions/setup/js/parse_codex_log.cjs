// @ts-check
/// <reference types="@actions/github-script" />

const { createEngineLogParser, truncateString, estimateTokens, formatToolCallAsDetails } = require("./log_parser_shared.cjs");

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
      serverCount = parseInt(countMatch[1]);
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
 * Convert parsed Codex data to logEntries format for plain text rendering
 * @param {Array<{type: string, content?: string, toolName?: string, params?: string, response?: string, statusIcon?: string}>} parsedData - Parsed Codex log data
 * @returns {Array} logEntries array in the format expected by generatePlainTextSummary
 */
function convertToLogEntries(parsedData) {
  const logEntries = [];
  
  for (const item of parsedData) {
    if (item.type === "thinking") {
      // Add thinking as assistant text content
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
 * Parse codex log content and format as markdown
 * @param {string} logContent - The raw log content to parse
 * @returns {{markdown: string, logEntries: Array, mcpFailures: Array<string>, maxTurnsHit: boolean}} Parsed log data
 */
function parseCodexLog(logContent) {
  if (!logContent) {
    return {
      markdown: "## 🤖 Commands and Tools\n\nNo log content provided.\n\n## 🤖 Reasoning\n\nUnable to parse reasoning from log.\n\n",
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
    markdown += "## 🚀 Initialization\n\n";
    markdown += mcpInfo.markdown;
  }

  markdown += "## 🤖 Reasoning\n\n";

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

    // Tool call line "tool github.list_pull_requests(...)"
    const toolMatch = line.match(/^tool\s+(\w+)\.(\w+)\(/);
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

      // Look ahead to find the result status
      let statusIcon = "❓"; // Unknown by default
      for (let j = i + 1; j < Math.min(i + LOOKAHEAD_WINDOW, lines.length); j++) {
        const nextLine = lines[j];
        if (nextLine.includes(`${server}.${toolName}(`) && nextLine.includes("success in")) {
          statusIcon = "✅";
          break;
        } else if (nextLine.includes(`${server}.${toolName}(`) && (nextLine.includes("failed in") || nextLine.includes("error"))) {
          statusIcon = "❌";
          break;
        }
      }

      markdown += `${statusIcon} ${server}::${toolName}(...)\n\n`;
      continue;
    }

    // Process thinking content (filter out timestamp lines and very short lines)
    if (inThinkingSection && line.trim().length > 20 && !line.match(/^\d{4}-\d{2}-\d{2}T/)) {
      const trimmed = line.trim();
      thinkingContent.push(trimmed);
      // Add thinking content directly to markdown
      markdown += `${trimmed}\n\n`;
    }
  }
  
  // Save any remaining thinking content
  if (thinkingContent.length > 0) {
    parsedData.push({
      type: "thinking",
      content: thinkingContent.join("\n"),
    });
  }

  markdown += "## 🤖 Commands and Tools\n\n";

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
  markdown += "\n## 📊 Information\n\n";

  // Extract metadata from Codex logs
  let totalTokens = 0;

  // TokenCount(TokenCountEvent { ... total_tokens: 13281 ...
  const tokenCountMatches = logContent.matchAll(/total_tokens:\s*(\d+)/g);
  for (const match of tokenCountMatches) {
    const tokens = parseInt(match[1]);
    totalTokens = Math.max(totalTokens, tokens); // Use the highest value (final total)
  }

  // Also check for "tokens used\n<number>" at the end (number may have commas)
  const finalTokensMatch = logContent.match(/tokens used\n([\d,]+)/);
  if (finalTokensMatch) {
    // Remove commas before parsing
    totalTokens = parseInt(finalTokensMatch[1].replace(/,/g, ""));
  }

  if (totalTokens > 0) {
    markdown += `**Total Tokens Used:** ${totalTokens.toLocaleString()}\n\n`;
  }

  // Count tool calls
  const toolCalls = (logContent.match(/ToolCall:\s+\w+__\w+/g) || []).length;

  if (toolCalls > 0) {
    markdown += `**Tool Calls:** ${toolCalls}\n\n`;
  }

  // Convert parsed data to logEntries format
  const logEntries = convertToLogEntries(parsedData);

  // Check for MCP failures
  const mcpFailures = mcpInfo.servers
    .filter(server => server.status === "failed")
    .map(server => server.name);

  return {
    markdown,
    logEntries,
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
    formatCodexToolCall,
    formatCodexBashCall,
    extractMCPInitialization,
  };
}
