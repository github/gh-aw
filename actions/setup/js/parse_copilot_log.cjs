// @ts-check
/// <reference types="@actions/github-script" />

const {
  createEngineLogParser,
  generateConversationMarkdown,
  generateInformationSection,
  buildStepSummaryDetailsSection,
  formatInitializationSummary,
  formatToolUse,
  parseLogEntries,
  AWF_INFRA_LINE_RE,
  isCopilotEventLogEntries,
  convertLegacyLogEntriesToCopilotEvents,
  convertCopilotEventsToLegacyLogEntries,
} = require("./log_parser_shared.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");

const main = createEngineLogParser({
  parserName: "Copilot",
  parseFunction: parseCopilotLog,
  supportsDirectories: true,
});

const AWF_TOKEN_WARNING_RE = /\[AWF TOKEN WARNING\][^\n\r]+/g;

/**
 * Extracts AWF token steering warnings from parsed Copilot log entries.
 * Handles several structured log shapes defensively because steering notices
 * may appear as system entries, text blocks, or plain message strings.
 * @param {Array<any>} logEntries
 * @returns {string[]}
 */
function extractAwfTokenWarnings(logEntries) {
  /** @type {string[]} */
  const warnings = [];
  const seen = new Set();

  const addMatches = value => {
    if (typeof value !== "string") return;
    const matches = value.match(AWF_TOKEN_WARNING_RE);
    if (!matches) return;
    for (const match of matches) {
      const normalized = match.trim();
      if (!normalized || seen.has(normalized)) continue;
      seen.add(normalized);
      warnings.push(normalized);
    }
  };

  const visit = value => {
    if (!value) return;
    if (typeof value === "string") {
      addMatches(value);
      return;
    }
    if (Array.isArray(value)) {
      for (const item of value) visit(item);
      return;
    }
    if (typeof value !== "object") return;

    if (typeof value.text === "string") addMatches(value.text);
    if (typeof value.message === "string") addMatches(value.message);
    if (typeof value.content === "string") addMatches(value.content);
    if (typeof value.system === "string") addMatches(value.system);
    if (typeof value.data?.content === "string") addMatches(value.data.content);

    if (Array.isArray(value.content)) visit(value.content);
    if (Array.isArray(value.message?.content)) visit(value.message.content);
    if (Array.isArray(value.system)) visit(value.system);
    if (value.data && typeof value.data === "object") visit(value.data);
  };

  for (const entry of logEntries) visit(entry);
  return warnings;
}

/**
 * Detects whether parsed entries are Copilot SDK events.jsonl entries that need
 * conversion into the normalized trace structure used by summary renderers.
 * @param {Array<any>} logEntries
 * @returns {boolean}
 */
function isCopilotSdkEventsFormat(logEntries) {
  return isCopilotEventLogEntries(logEntries);
}

/**
 * Parses Copilot CLI log content and converts it to markdown format
 * @param {string} logContent - The raw log content as a string
 * @returns {{markdown: string, logEntries: Array, mcpFailures?: string[], maxTurnsHit?: boolean}} Formatted result with markdown and metadata
 */
function parseCopilotLog(logContent) {
  let logEntries;

  // First, try to parse as JSON array (structured format)
  try {
    logEntries = JSON.parse(logContent);
    if (!Array.isArray(logEntries)) {
      throw new Error(`${ERR_PARSE}: Not a JSON array`);
    }
  } catch (jsonArrayError) {
    // If that fails, try to parse as debug logs format
    const debugLogEntries = parseDebugLogFormat(logContent);
    if (debugLogEntries && debugLogEntries.length > 0) {
      logEntries = debugLogEntries;
    } else {
      // Try JSONL format using shared function
      logEntries = parseLogEntries(logContent);

      // If still nothing, try the pretty-print stdout format (✗/● markers)
      if (!logEntries || logEntries.length === 0) {
        const prettyPrintEntries = parsePrettyPrintFormat(logContent);
        if (prettyPrintEntries && prettyPrintEntries.length > 0) {
          logEntries = prettyPrintEntries;
        }
      }
    }
  }

  if (!logEntries || logEntries.length === 0) {
    return { markdown: buildStepSummaryDetailsSection("Agent Log Summary", "Log format not recognized as Copilot JSON array or JSONL."), logEntries: [] };
  }

  const isEventFormat = isCopilotSdkEventsFormat(logEntries);
  let canonicalLogEntries = isEventFormat ? logEntries : convertLegacyLogEntriesToCopilotEvents(logEntries, { sourceEngine: "copilot" });
  const legacyRenderEntries = isEventFormat ? convertCopilotEventsToLegacyLogEntries(canonicalLogEntries) : logEntries;
  if (isEventFormat && !canonicalLogEntries.some(entry => entry?.type === "session.result")) {
    const legacyResult = legacyRenderEntries.find(entry => entry?.type === "result");
    canonicalLogEntries.push({
      type: "session.result",
      data: {
        numTurns: legacyResult?.num_turns,
        durationMs: legacyResult?.duration_ms,
        totalCostUsd: legacyResult?.total_cost_usd,
        usage: legacyResult?.usage,
        errors: legacyResult?.errors,
        permissionDenials: legacyResult?.permission_denials,
      },
    });
  }

  // Generate conversation markdown using shared function
  const conversationResult = generateConversationMarkdown(canonicalLogEntries, {
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult, { includeDetailedParameters: false }),
    formatInitCallback: initEntry =>
      formatInitializationSummary(initEntry, {
        includeSlashCommands: false,
        modelInfoCallback: entry => {
          // Display premium model information if available (Copilot-specific)
          if (!entry.model_info) return "";

          const modelInfo = entry.model_info;
          let markdown = "";

          // Display model name and vendor
          if (modelInfo.name) {
            markdown += `**Model Name:** ${modelInfo.name}`;
            if (modelInfo.vendor) {
              markdown += ` (${modelInfo.vendor})`;
            }
            markdown += "\n\n";
          }

          // Display billing/premium information
          if (modelInfo.billing) {
            const billing = modelInfo.billing;
            if (billing.is_premium === true) {
              markdown += `**Premium Model:** Yes`;
              if (billing.multiplier && billing.multiplier !== 1) {
                markdown += ` (${billing.multiplier}x cost multiplier)`;
              }
              markdown += "\n";

              if (billing.restricted_to && Array.isArray(billing.restricted_to) && billing.restricted_to.length > 0) {
                markdown += `**Required Plans:** ${billing.restricted_to.join(", ")}\n`;
              }
              markdown += "\n";
            } else if (billing.is_premium === false) {
              markdown += `**Premium Model:** No\n\n`;
            }
          }

          return markdown;
        },
      }),
  });

  let markdown = conversationResult.markdown;
  const awfTokenWarnings = extractAwfTokenWarnings(canonicalLogEntries);

  if (awfTokenWarnings.length > 0) {
    let steeringBody = "";
    for (const warning of awfTokenWarnings) {
      steeringBody += `- ${warning}\n`;
    }
    markdown += buildStepSummaryDetailsSection("Firewall Steering", steeringBody);
  }

  // Add Information section
  const lastEntry = legacyRenderEntries[legacyRenderEntries.length - 1];

  markdown += generateInformationSection(lastEntry, {
    additionalInfoCallback: () => "",
  });

  return { markdown, logEntries: canonicalLogEntries };
}

/**
 * Parses the "pretty-print" stdout format emitted by the Copilot CLI when
 * debug logs are written to a --log-dir directory (not captured on stdout).
 * This format uses ✗ for failed tool calls and ● or ✓ for successful ones.
 * @param {string} logContent - Raw log content as a string
 * @returns {Array} Array of log entries in structured format, or empty array if not detected
 */
function parsePrettyPrintFormat(logContent) {
  // Only attempt this format if the characteristic markers are present
  if (!/^[✗●✓]/m.test(logContent)) {
    return [];
  }

  const INFRA_LINE_RE = AWF_INFRA_LINE_RE;
  const FAILED_TOOL_RE = /^✗\s+(\S+)/;
  const SUCCESS_TOOL_RE = /^(?:●|✓)\s+(\S+)/;
  const CONTINUATION_RE = /^\s+[└│]/;
  const DEEP_INDENT_RE = /^ {4,}/;
  const MODEL_BREAKDOWN_RE = /^Breakdown by AI model:/;
  const MODEL_LINE_RE = /^ +(\S+)\s+([\d.]+k?)\s+in,\s+([\d.]+k?)\s+out(?:,\s+([\d.]+k?)\s+cached)?/;
  // Recognise both legacy ("Total usage est:" / "API time spent:" / …) and the
  // newer Copilot CLI footer ("Changes  +N -N", "Duration  Ns", "Tokens  ↑N ↓N (cached)",
  // "Resume  copilot --resume=…"). The newer footer omits a colon and uses arrow glyphs, so we
  // extend the regex rather than relying on the legacy "Total …:" prefix alone. The "Resume"
  // hint is matched against the exact CLI command syntax to avoid false-positives on prose
  // that happens to start with "Resume  ".
  const USAGE_LINES_RE = /^(?:Total usage est:|API time spent:|Total session time:|Total code changes:|Changes\s+[+-]?\d|Duration\s+\d|Tokens\s+[↑↓]|Resume\s{2,}copilot\s+--resume=)/;

  const parseTokenCount = s => {
    const normalized = s.toLowerCase();
    const n = parseFloat(normalized);
    if (normalized.endsWith("m")) return Math.round(n * 1000000);
    return normalized.endsWith("k") ? Math.round(n * 1000) : n;
  };

  const lines = logContent.split("\n").filter(line => !INFRA_LINE_RE.test(line));

  const toolEntries = [];
  const agentTextLines = [];
  let inputTokens = 0;
  let outputTokens = 0;
  let cacheReadTokens = 0;
  let cacheWriteTokens = 0;
  let modelName = "unknown";
  let inModelBreakdown = false;
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      inModelBreakdown = false;
      i++;
      continue;
    }

    // Failed tool call: ✗ tool_name
    const failedMatch = trimmed.match(FAILED_TOOL_RE);
    if (failedMatch) {
      inModelBreakdown = false;
      const toolName = failedMatch[1];
      const outputLines = [];
      i++;
      while (i < lines.length && (CONTINUATION_RE.test(lines[i]) || DEEP_INDENT_RE.test(lines[i]))) {
        outputLines.push(lines[i].replace(/^\s*[└│]\s?/, "").replace(/^ {4}/, ""));
        i++;
      }
      toolEntries.push({ name: toolName, success: false, output: outputLines.join("\n").trim() });
      continue;
    }

    // Successful tool call: ● tool_name
    const successMatch = trimmed.match(SUCCESS_TOOL_RE);
    if (successMatch) {
      inModelBreakdown = false;
      const toolName = successMatch[1];
      const outputLines = [];
      i++;
      while (i < lines.length && (CONTINUATION_RE.test(lines[i]) || DEEP_INDENT_RE.test(lines[i]))) {
        outputLines.push(lines[i].replace(/^\s*[└│]\s?/, "").replace(/^ {4}/, ""));
        i++;
      }
      toolEntries.push({ name: toolName, success: true, output: outputLines.join("\n").trim() });
      continue;
    }

    // Skip usage stat lines
    if (USAGE_LINES_RE.test(trimmed)) {
      // Newer Copilot CLI footer: "Tokens    ↑ 163.9k • ↓ 567 • 149.2k (cached)"
      // The arrow + (cached) form has no "Breakdown by AI model" section, so this
      // is the only place token totals appear. Capture them when present so they
      // surface in the Information section.
      const tokenMatch = trimmed.match(/^Tokens\s+↑\s*([\d.]+[km]?)\s*[•·]\s*↓\s*([\d.]+[km]?)(?:\s*[•·]\s*([\d.]+[km]?)\s*\(cached\))?/i);
      if (tokenMatch) {
        if (inputTokens === 0) inputTokens = parseTokenCount(tokenMatch[1]);
        if (outputTokens === 0) outputTokens = parseTokenCount(tokenMatch[2]);
        if (tokenMatch[3] && cacheReadTokens === 0) cacheReadTokens = parseTokenCount(tokenMatch[3]);
      } else {
        // Newer footer variant where the cached count is shown inline after the
        // up-arrow rather than trailing the line:
        //   "Tokens    ↑ 422.2k (375.0k cached) • ↓ 2.4k"
        // (emitted by Copilot CLI 1.0.55). The trailing-cached regex above does
        // not match this ordering, so handle it explicitly to avoid dropping the
        // token totals from the Information section.
        const inlineCachedMatch = trimmed.match(/^Tokens\s+↑\s*([\d.]+[km]?)\s*\(\s*([\d.]+[km]?)\s+cached(?:\s*,\s*([\d.]+[km]?)\s+written)?\s*\)\s*[•·]\s*↓\s*([\d.]+[km]?)(?:\s*\([^)]*\))?/i);
        if (inlineCachedMatch) {
          if (inputTokens === 0) inputTokens = parseTokenCount(inlineCachedMatch[1]);
          if (cacheReadTokens === 0) cacheReadTokens = parseTokenCount(inlineCachedMatch[2]);
          if (inlineCachedMatch[3] && cacheWriteTokens === 0) cacheWriteTokens = parseTokenCount(inlineCachedMatch[3]);
          if (outputTokens === 0) outputTokens = parseTokenCount(inlineCachedMatch[4]);
        }
      }
      i++;
      continue;
    }

    // Model breakdown header
    if (MODEL_BREAKDOWN_RE.test(trimmed)) {
      inModelBreakdown = true;
      i++;
      continue;
    }

    // Model breakdown line: "  model_name  Xk in, Xk out, Xk cached"
    if (inModelBreakdown) {
      const modelMatch = line.match(MODEL_LINE_RE);
      if (modelMatch) {
        if (modelName === "unknown") modelName = modelMatch[1];
        inputTokens += parseTokenCount(modelMatch[2]);
        outputTokens += parseTokenCount(modelMatch[3]);
        if (modelMatch[4]) cacheReadTokens += parseTokenCount(modelMatch[4]);
        i++;
        continue;
      }
      inModelBreakdown = false;
    }

    // Agent text line
    agentTextLines.push(trimmed);
    i++;
  }

  if (toolEntries.length === 0 && agentTextLines.length === 0) {
    return [];
  }

  const entries = [];

  // System init entry
  const initEntry = {
    type: "system",
    subtype: "init",
    model: modelName,
    tools: [],
    session_id: null,
  };
  entries.push(initEntry);

  // Tool call entries (assistant + user result pairs)
  for (let j = 0; j < toolEntries.length; j++) {
    const tc = toolEntries[j];
    const toolId = `pretty_tool_${j}`;
    entries.push({
      type: "assistant",
      message: {
        content: [{ type: "tool_use", id: toolId, name: tc.name, input: {} }],
      },
    });
    entries.push({
      type: "user",
      message: {
        content: [
          {
            type: "tool_result",
            tool_use_id: toolId,
            content: tc.output || (tc.success ? "success" : "error"),
            is_error: !tc.success,
          },
        ],
      },
    });
  }

  // Agent text (if any)
  const agentText = agentTextLines.join("\n").trim();
  if (agentText) {
    entries.push({
      type: "assistant",
      message: { content: [{ type: "text", text: agentText }] },
    });
  }

  // Derive the number of turns from the CLI's "Turns:" statistic if available.
  // Fallback to the number of tool entries to preserve existing behavior when absent.
  let numTurns = toolEntries.length;
  const turnsMatch = logContent.match(/Turns:\s*(\d+)/i);
  if (turnsMatch && turnsMatch[1]) {
    const parsedTurns = parseInt(turnsMatch[1], 10);
    if (!Number.isNaN(parsedTurns) && parsedTurns > 0) {
      numTurns = parsedTurns;
    }
  }

  // Result entry with token usage
  const usage = {};
  if (inputTokens > 0) usage.input_tokens = inputTokens;
  if (outputTokens > 0) usage.output_tokens = outputTokens;
  if (cacheReadTokens > 0) usage.cache_read_input_tokens = cacheReadTokens;
  if (cacheWriteTokens > 0) usage.cache_creation_input_tokens = cacheWriteTokens;
  entries.push({
    type: "result",
    num_turns: numTurns,
    usage,
  });

  return entries;
}

/**
 * Scans log content for tool execution errors and builds a map of failed tools
 * @param {string} logContent - Raw debug log content
 * @returns {Map<string, boolean>} Map of tool IDs/names to error status
 */
function scanForToolErrors(logContent) {
  const toolErrors = new Map();
  const lines = logContent.split("\n");

  // Track recent tool calls to associate errors with them
  const recentToolCalls = [];
  const MAX_RECENT_TOOLS = 10;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Look for tool_calls in data blocks (not in JSON arguments)
    // Only match if it's in a choices/message context
    if (line.includes('"tool_calls":') && !line.includes('\\"tool_calls\\"')) {
      // Next few lines should contain tool call details
      for (let j = i + 1; j < Math.min(i + 30, lines.length); j++) {
        const nextLine = lines[j];

        // Extract tool call ID
        const idMatch = nextLine.match(/"id":\s*"([^"]+)"/);
        // Extract function name (not arguments with escaped quotes)
        const nameMatch = nextLine.match(/"name":\s*"([^"]+)"/) && !nextLine.includes('\\"name\\"');

        if (idMatch) {
          const toolId = idMatch[1];
          // Keep looking for the name
          for (let k = j; k < Math.min(j + 10, lines.length); k++) {
            const nameLine = lines[k];
            const funcNameMatch = nameLine.match(/"name":\s*"([^"]+)"/);
            if (funcNameMatch && !nameLine.includes('\\"name\\"')) {
              const toolName = funcNameMatch[1];
              recentToolCalls.unshift({ id: toolId, name: toolName });
              if (recentToolCalls.length > MAX_RECENT_TOOLS) {
                recentToolCalls.pop();
              }
              break;
            }
          }
        }
      }
    }

    // Look for error messages
    const errorMatch = line.match(/\[ERROR\].*(?:Tool execution failed|Permission denied|Resource not accessible|Error executing tool)/i);
    if (errorMatch) {
      // Try to extract tool name from error line
      const toolNameMatch = line.match(/Tool execution failed:\s*([^\s]+)/i);
      const toolIdMatch = line.match(/tool_call_id:\s*([^\s]+)/i);

      if (toolNameMatch) {
        const toolName = toolNameMatch[1];
        toolErrors.set(toolName, true);
        // Also mark by ID if we can find it in recent calls
        const matchingTool = recentToolCalls.find(t => t.name === toolName);
        if (matchingTool) {
          toolErrors.set(matchingTool.id, true);
        }
      } else if (toolIdMatch) {
        toolErrors.set(toolIdMatch[1], true);
      } else if (recentToolCalls.length > 0) {
        // Mark the most recent tool call as failed
        const lastTool = recentToolCalls[0];
        toolErrors.set(lastTool.id, true);
        toolErrors.set(lastTool.name, true);
      }
    }
  }

  return toolErrors;
}

/**
 * Parses Copilot CLI debug log format and reconstructs the conversation flow
 * @param {string} logContent - Raw debug log content
 * @returns {Array} Array of log entries in structured format
 */
function parseDebugLogFormat(logContent) {
  const entries = [];
  const lines = logContent.split("\n");

  // First pass: scan for tool errors
  const toolErrors = scanForToolErrors(logContent);

  // Extract model information from the start
  let model = "unknown";
  /** @type {any} */
  let sessionId = null;
  /** @type {any} */
  let modelInfo = null;
  let tools = [];
  const modelMatch = logContent.match(/Starting Copilot CLI: ([\d.]+)/);
  if (modelMatch) {
    sessionId = `copilot-${modelMatch[1]}-${Date.now()}`;
  }

  // Extract premium model info from "Got model info:" JSON block
  // Look for a multi-line JSON block that starts with "Got model info: {" and ends with "}"
  const gotModelInfoIndex = logContent.indexOf("[DEBUG] Got model info: {");
  if (gotModelInfoIndex !== -1) {
    // Find the start of the JSON (the opening brace)
    const jsonStart = logContent.indexOf("{", gotModelInfoIndex);
    if (jsonStart !== -1) {
      // Track braces to find the end of the JSON
      let braceCount = 0;
      let inString = false;
      let escapeNext = false;
      let jsonEnd = -1;

      for (let i = jsonStart; i < logContent.length; i++) {
        const char = logContent[i];

        if (escapeNext) {
          escapeNext = false;
          continue;
        }

        if (char === "\\") {
          escapeNext = true;
          continue;
        }

        if (char === '"' && !escapeNext) {
          inString = !inString;
          continue;
        }

        if (inString) continue;

        if (char === "{") {
          braceCount++;
        } else if (char === "}") {
          braceCount--;
          if (braceCount === 0) {
            jsonEnd = i + 1;
            break;
          }
        }
      }

      if (jsonEnd !== -1) {
        const modelInfoJson = logContent.substring(jsonStart, jsonEnd);
        try {
          modelInfo = JSON.parse(modelInfoJson);
        } catch (e) {
          // Failed to parse model info — ignored, continue without it.
        }
      }
    }
  }

  // Extract tools from "[DEBUG] Tools:" section
  // The format is: [DEBUG] Tools: \n[DEBUG] [\n  { "type": "function", "function": { "name": "..." } }\n]
  const toolsIndex = logContent.indexOf("[DEBUG] Tools:");
  if (toolsIndex !== -1) {
    // Find the start of the JSON array - look for a line that starts with [DEBUG] [
    // Skip past the "Tools:" line first
    const afterToolsLine = logContent.indexOf("\n", toolsIndex);
    let toolsStart = logContent.indexOf("[DEBUG] [", afterToolsLine);
    if (toolsStart !== -1) {
      // Find the actual '[' character after '[DEBUG] '
      toolsStart = logContent.indexOf("[", toolsStart + 7); // Skip '[DEBUG] ' which is 8 chars
    }
    if (toolsStart !== -1) {
      // Track brackets to find the end of the JSON array
      let bracketCount = 0;
      let inString = false;
      let escapeNext = false;
      let toolsEnd = -1;

      for (let i = toolsStart; i < logContent.length; i++) {
        const char = logContent[i];

        if (escapeNext) {
          escapeNext = false;
          continue;
        }

        if (char === "\\") {
          escapeNext = true;
          continue;
        }

        if (char === '"' && !escapeNext) {
          inString = !inString;
          continue;
        }

        if (inString) continue;

        if (char === "[") {
          bracketCount++;
        } else if (char === "]") {
          bracketCount--;
          if (bracketCount === 0) {
            toolsEnd = i + 1;
            break;
          }
        }
      }

      if (toolsEnd !== -1) {
        // Remove [DEBUG] prefixes from each line in the JSON
        let toolsJson = logContent.substring(toolsStart, toolsEnd);
        toolsJson = toolsJson.replace(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z \[DEBUG\] /gm, "");

        try {
          const toolsArray = JSON.parse(toolsJson);
          // Extract tool names from the OpenAI function format
          // Format: [{ "type": "function", "function": { "name": "bash", ... } }, ...]
          if (Array.isArray(toolsArray)) {
            tools = toolsArray
              .map(tool => {
                if (tool.type === "function" && tool.function && tool.function.name) {
                  // Convert github-* names to mcp__github__* format for consistency
                  let name = tool.function.name;
                  if (name.startsWith("github-")) {
                    name = "mcp__github__" + name.substring(7);
                  } else if (name.startsWith("safe_outputs-")) {
                    name = name; // Keep safe_outputs names as-is
                  }
                  return name;
                }
                return null;
              })
              .filter(name => name !== null);
          }
        } catch (e) {
          // Failed to parse tools — ignored, continue without them.
        }
      }
    }
  }

  // Find all JSON response blocks in the debug logs
  let inDataBlock = false;
  let currentJsonLines = [];
  let turnCount = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Detect start of a JSON data block
    if (line.includes("[DEBUG] data:")) {
      inDataBlock = true;
      currentJsonLines = [];
      continue;
    }

    // While in a data block, accumulate lines
    if (inDataBlock) {
      // Check if this line starts with timestamp
      const hasTimestamp = line.match(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z /);

      if (hasTimestamp) {
        // Strip the timestamp and [DEBUG] prefix to see what remains
        const cleanLine = line.replace(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z \[DEBUG\] /, "");

        // If after stripping, the line starts with JSON characters, it's part of JSON
        // Otherwise, it's a new log entry and we should end the block
        const isJsonContent = /^[{\[}\]"]/.test(cleanLine) || cleanLine.trim().startsWith('"');

        if (!isJsonContent) {
          // This is a new log line (not JSON content) - end of JSON block, process what we have
          if (currentJsonLines.length > 0) {
            try {
              const jsonStr = currentJsonLines.join("\n");
              const jsonData = JSON.parse(jsonStr);

              // Extract model info
              if (jsonData.model) {
                model = jsonData.model;
              }

              // Process the choices in the response
              if (jsonData.choices && Array.isArray(jsonData.choices)) {
                for (const choice of jsonData.choices) {
                  if (choice.message) {
                    const message = choice.message;

                    // Create an assistant entry
                    const content = [];
                    const toolResults = []; // Collect tool calls to create synthetic results (debug logs don't include actual results)

                    // Add reasoning_text first (agent's thinking before response/tools)
                    if (message.reasoning_text && message.reasoning_text.trim()) {
                      content.push({
                        type: "thinking",
                        thinking: message.reasoning_text,
                      });
                    }

                    if (message.content && message.content.trim()) {
                      content.push({
                        type: "text",
                        text: message.content,
                      });
                    }

                    if (message.tool_calls && Array.isArray(message.tool_calls)) {
                      for (const toolCall of message.tool_calls) {
                        if (toolCall.function) {
                          let toolName = toolCall.function.name;
                          const originalToolName = toolName; // Keep original for error matching
                          const toolId = toolCall.id || `tool_${Date.now()}_${Math.random()}`;
                          /** @type {any} */
                          let args = {};

                          // Parse tool name (handle github- prefix and bash)
                          if (toolName.startsWith("github-")) {
                            toolName = "mcp__github__" + toolName.substring(7);
                          } else if (toolName === "bash") {
                            toolName = "Bash";
                          }

                          // Parse arguments
                          try {
                            args = JSON.parse(toolCall.function.arguments);
                          } catch (e) {
                            args = {};
                          }

                          content.push({
                            type: "tool_use",
                            id: toolId,
                            name: toolName,
                            input: args,
                          });

                          // Check if this tool had an error (by ID or by name)
                          const hasError = toolErrors.has(toolId) || toolErrors.has(originalToolName);

                          // Create a corresponding tool result
                          toolResults.push({
                            type: "tool_result",
                            tool_use_id: toolId,
                            content: hasError ? "Permission denied or tool execution failed" : "", // Set error message if failed
                            is_error: hasError, // Mark as error if we detected failure
                          });
                        }
                      }
                    }

                    if (content.length > 0) {
                      entries.push({
                        type: "assistant",
                        message: { content },
                      });
                      turnCount++;

                      // Add tool results as a user message if we have any
                      if (toolResults.length > 0) {
                        entries.push({
                          type: "user",
                          message: { content: toolResults },
                        });
                      }
                    }
                  }
                }

                // Accumulate usage/result entry from each response
                if (jsonData.usage) {
                  // Initialize accumulator if needed
                  // @ts-ignore - Dynamic property for accumulating usage data
                  if (!entries._accumulatedUsage) {
                    // @ts-ignore
                    entries._accumulatedUsage = {
                      input_tokens: 0,
                      output_tokens: 0,
                    };
                  }

                  // Accumulate token counts from this response
                  // OpenAI uses prompt_tokens/completion_tokens, normalize to input_tokens/output_tokens
                  if (jsonData.usage.prompt_tokens) {
                    // @ts-ignore
                    entries._accumulatedUsage.input_tokens += jsonData.usage.prompt_tokens;
                  }
                  if (jsonData.usage.completion_tokens) {
                    // @ts-ignore
                    entries._accumulatedUsage.output_tokens += jsonData.usage.completion_tokens;
                  }

                  // Store result entry with accumulated usage
                  // @ts-ignore - Dynamic property for storing last result
                  entries._lastResult = {
                    type: "result",
                    num_turns: turnCount,
                    // @ts-ignore
                    usage: entries._accumulatedUsage,
                  };
                }
              }
            } catch (e) {
              // Invalid JSON block — ignored.
            }
          }

          inDataBlock = false;
          currentJsonLines = [];
          continue; // Don't add this line to JSON
        } else if (hasTimestamp && isJsonContent) {
          // This line has a timestamp but is JSON content - strip prefix and add
          currentJsonLines.push(cleanLine);
        }
      } else {
        // This line is part of the JSON - add it (remove [DEBUG] prefix if present)
        const cleanLine = line.replace(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z \[DEBUG\] /, "");
        currentJsonLines.push(cleanLine);
      }
    }
  }

  // Process any remaining JSON block at the end of file
  if (inDataBlock && currentJsonLines.length > 0) {
    try {
      const jsonStr = currentJsonLines.join("\n");
      const jsonData = JSON.parse(jsonStr);

      if (jsonData.model) {
        model = jsonData.model;
      }

      if (jsonData.choices && Array.isArray(jsonData.choices)) {
        for (const choice of jsonData.choices) {
          if (choice.message) {
            const message = choice.message;
            const content = [];
            const toolResults = []; // Collect tool calls to create synthetic results (debug logs don't include actual results)

            // Add reasoning_text first (agent's thinking before response/tools)
            if (message.reasoning_text && message.reasoning_text.trim()) {
              content.push({
                type: "thinking",
                thinking: message.reasoning_text,
              });
            }

            if (message.content && message.content.trim()) {
              content.push({
                type: "text",
                text: message.content,
              });
            }

            if (message.tool_calls && Array.isArray(message.tool_calls)) {
              for (const toolCall of message.tool_calls) {
                if (toolCall.function) {
                  let toolName = toolCall.function.name;
                  const originalToolName = toolName;
                  const toolId = toolCall.id || `tool_${Date.now()}_${Math.random()}`;
                  /** @type {any} */
                  let args = {};

                  if (toolName.startsWith("github-")) {
                    toolName = "mcp__github__" + toolName.substring(7);
                  } else if (toolName === "bash") {
                    toolName = "Bash";
                  }

                  try {
                    args = JSON.parse(toolCall.function.arguments);
                  } catch (e) {
                    args = {};
                  }

                  content.push({
                    type: "tool_use",
                    id: toolId,
                    name: toolName,
                    input: args,
                  });

                  // Check if this tool had an error (by ID or by name)
                  const hasError = toolErrors.has(toolId) || toolErrors.has(originalToolName);

                  // Create a corresponding tool result
                  toolResults.push({
                    type: "tool_result",
                    tool_use_id: toolId,
                    content: hasError ? "Permission denied or tool execution failed" : "",
                    is_error: hasError,
                  });
                }
              }
            }

            if (content.length > 0) {
              entries.push({
                type: "assistant",
                message: { content },
              });
              turnCount++;

              // Add tool results as a user message if we have any
              if (toolResults.length > 0) {
                entries.push({
                  type: "user",
                  message: { content: toolResults },
                });
              }
            }
          }
        }

        if (jsonData.usage) {
          // Initialize accumulator if needed
          // @ts-ignore - Dynamic property for accumulating usage data
          if (!entries._accumulatedUsage) {
            // @ts-ignore
            entries._accumulatedUsage = {
              input_tokens: 0,
              output_tokens: 0,
            };
          }

          // Accumulate token counts from this response
          // OpenAI uses prompt_tokens/completion_tokens, normalize to input_tokens/output_tokens
          if (jsonData.usage.prompt_tokens) {
            // @ts-ignore
            entries._accumulatedUsage.input_tokens += jsonData.usage.prompt_tokens;
          }
          if (jsonData.usage.completion_tokens) {
            // @ts-ignore
            entries._accumulatedUsage.output_tokens += jsonData.usage.completion_tokens;
          }

          // Store result entry with accumulated usage
          // @ts-ignore - Dynamic property for storing last result
          entries._lastResult = {
            type: "result",
            num_turns: turnCount,
            // @ts-ignore
            usage: entries._accumulatedUsage,
          };
        }
      }
    } catch (e) {
      // Invalid JSON — ignored.
    }
  }

  // Add system init entry at the beginning if we have entries
  if (entries.length > 0) {
    const initEntry = {
      type: "system",
      subtype: "init",
      session_id: sessionId,
      model: model,
      tools: tools, // Tools extracted from [DEBUG] Tools: section
    };

    // Add model info if available
    if (modelInfo) {
      initEntry.model_info = modelInfo;
    }

    entries.unshift(initEntry);

    // Add the final result entry if we have it
    // @ts-ignore - Dynamic property for last result
    if (entries._lastResult) {
      // @ts-ignore
      entries.push(entries._lastResult);
      // @ts-ignore
      delete entries._lastResult;
    }
  }

  return entries;
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    parseCopilotLog,
    parsePrettyPrintFormat,
  };
}
