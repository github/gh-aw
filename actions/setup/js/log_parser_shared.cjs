// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { unfenceMarkdown } = require("./markdown_unfencing.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");
const createLogParserFormatters = require("./log_parser_format.cjs");
const { buildStepSummaryDetailsSection } = require("./log_parser_step_summary_builder.cjs");
const { isSessionEvent, normalizeAgentSession, normalizeSessionUsage, projectSessionResult, projectSessionInitialization, sessionOutputText, sessionToolSuccess, sessionTokenTotal, isMetric } = require("./agent_session.cjs");
const { escapeSummaryText, toolInventoryName, displayArgument } = require("./agent_session_render.cjs");

/**
 * Shared utility functions for log parsers
 * Used by parse_claude_log.cjs, parse_copilot_log.cjs, and parse_codex_log.cjs
 */

/**
 * Maximum length for tool output content in characters.
 * Tool output/response sections are truncated to this length to keep step summaries readable.
 * Reduced from 500 to 256 for more compact output.
 */
const MAX_TOOL_OUTPUT_LENGTH = 256;

/**
 * Maximum step summary size in bytes (1000KB).
 * GitHub Actions step summaries have a limit of 1024KB. We use 1000KB to leave buffer space.
 * We stop rendering additional content when approaching this limit to prevent workflow failures.
 */
const MAX_STEP_SUMMARY_SIZE = 1000 * 1024;

/**
 * Maximum length for bash command display in plain text summaries.
 * Commands are truncated to this length for compact display.
 */
const MAX_BASH_COMMAND_DISPLAY_LENGTH = 40;

/**
 * Maximum length for agent response text blocks in conversation summaries.
 * Increased from 500 to allow structured output (tables, lists) to survive intact.
 */
const MAX_AGENT_TEXT_LENGTH = 2000;

/**
 * Warning message shown when step summary size limit is reached.
 * This message is added directly to markdown (not tracked) to ensure it's always visible.
 * The message is small (~70 bytes) and won't cause practical issues with the 8MB limit.
 */
const SIZE_LIMIT_WARNING = "\n\n*Step summary size limit reached. Additional content truncated.*\n\n";

/**
 * Matches AWF infrastructure lines written by the firewall/container wrapper.
 * These lines are produced by the AWF infrastructure (container lifecycle, firewall proxy)
 * rather than by the engine itself, and must be excluded when analysing agent output.
 *
 * Examples of matched lines:
 *   - [INFO] API proxy logs available at: …
 *   - [WARN] Command completed with exit code: 1
 *   - [SUCCESS] Containers stopped successfully
 *   - [ERROR] …
 *   - [entrypoint] Starting firewall…       (lowercase — container script convention)
 *   - [health-check] Proxy ready            (lowercase — container script convention)
 *   - [copilot-harness] 2026-… attempt 1: … (lowercase — Node harness wrapper convention)
 *   - [claude-harness] 2026-… …             (lowercase — Node harness wrapper convention)
 *   - [codex-harness] 2026-… …              (lowercase — Node harness wrapper convention)
 *   -  Container awf-squid  Removed         (Docker Compose lifecycle output)
 *   -  Network …  Removed
 *   - Process exiting with code: 1          (AWF wrapper exit line)
 *
 * Note: INFO/WARN/SUCCESS/ERROR are uppercase (AWF wrapper convention); entrypoint and
 * health-check are lowercase (container script convention). Mixed casing is intentional
 * and reflects the actual output produced by different AWF components.
 *
 * Used by parse_copilot_log.cjs (parsePrettyPrintFormat) and handle_agent_failure.cjs
 * (buildEngineFailureContext) to strip infrastructure noise from engine log analysis.
 */
const AWF_INFRA_LINE_RE = /^\[(INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|copilot-harness|claude-harness|codex-harness)\]|^ (?:Container|Network|Volume) |^Process exiting with code:/;

/**
 * Tracks the size of content being added to a step summary.
 * Used to prevent exceeding GitHub Actions step summary size limits.
 */
class StepSummaryTracker {
  /**
   * Creates a new step summary size tracker.
   * @param {number} [maxSize=MAX_STEP_SUMMARY_SIZE] - Maximum allowed size in bytes
   */
  constructor(maxSize = MAX_STEP_SUMMARY_SIZE) {
    /** @type {number} */
    this.currentSize = 0;
    /** @type {number} */
    this.maxSize = maxSize;
    /** @type {boolean} */
    this.limitReached = false;
  }

  /**
   * Adds content to the tracker and returns whether the limit has been reached.
   * @param {string} content - Content to add
   * @returns {boolean} True if the content was added, false if the limit was reached
   */
  add(content) {
    if (this.limitReached) {
      return false;
    }

    const contentSize = Buffer.byteLength(content, "utf8");
    if (this.currentSize + contentSize > this.maxSize) {
      this.limitReached = true;
      return false;
    }

    this.currentSize += contentSize;
    return true;
  }

  /**
   * Checks if the limit has been reached.
   * @returns {boolean} True if the limit has been reached
   */
  isLimitReached() {
    return this.limitReached;
  }

  /**
   * Gets the remaining byte capacity before the limit.
   * @returns {number} Remaining bytes available (0 when limit is reached)
   */
  remaining() {
    return Math.max(0, this.maxSize - this.currentSize);
  }

  /**
   * Gets the current accumulated size.
   * @returns {number} Current size in bytes
   */
  getSize() {
    return this.currentSize;
  }

  /**
   * Resets the tracker.
   */
  reset() {
    this.currentSize = 0;
    this.limitReached = false;
  }
}

/**
 * Formats duration in milliseconds to human-readable string
 * @param {number} ms - Duration in milliseconds
 * @returns {string} Formatted duration string (e.g., "1s", "1m 30s")
 */
function formatDuration(ms) {
  if (!ms || ms <= 0) return "";

  const seconds = Math.round(ms / 1000);
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (remainingSeconds === 0) {
    return `${minutes}m`;
  }
  return `${minutes}m ${remainingSeconds}s`;
}

/**
 * Formats a bash command by normalizing whitespace and escaping
 * @param {string} command - The raw bash command string
 * @returns {string} Formatted and escaped command string
 */
function formatBashCommand(command) {
  if (!command) return "";

  // Convert multi-line commands to single line by replacing newlines with spaces
  // and collapsing multiple spaces
  let formatted = command
    .replace(/\n/g, " ") // Replace newlines with spaces
    .replace(/\r/g, " ") // Replace carriage returns with spaces
    .replace(/\t/g, " ") // Replace tabs with spaces
    .replace(/\s+/g, " ") // Collapse multiple spaces into one
    .trim(); // Remove leading/trailing whitespace

  // Escape backticks to prevent markdown issues
  formatted = formatted.replace(/`/g, "\\`");

  // Truncate if too long (keep reasonable length for summary)
  const maxLength = 300;
  if (formatted.length > maxLength) {
    formatted = formatted.substring(0, maxLength) + "...";
  }

  return formatted;
}

/**
 * Truncates a string to a maximum length with ellipsis
 * @param {string} str - The string to truncate
 * @param {number} maxLength - Maximum allowed length
 * @returns {string} Truncated string with ellipsis if needed
 */
function truncateString(str, maxLength) {
  if (!str) return "";
  if (str.length <= maxLength) return str;
  return str.substring(0, maxLength) + "...";
}

/**
 * Calculates approximate token count from text using 4 chars per token estimate
 * @param {string} text - The text to estimate tokens for
 * @returns {number} Approximate token count
 */
function estimateTokens(text) {
  if (!text) return 0;
  return Math.ceil(text.length / 4);
}

/**
 * Formats MCP tool name from internal format to display format
 * @param {string} toolName - The raw tool name (e.g., mcp__github__search_issues)
 * @returns {string} Formatted tool name (e.g., github::search_issues)
 */
function formatMcpName(toolName) {
  // Convert mcp__github__search_issues to github::search_issues
  if (toolName.startsWith("mcp__")) {
    const parts = toolName.split("__");
    if (parts.length >= 3) {
      const provider = parts[1]; // github, etc.
      const method = parts.slice(2).join("_"); // search_issues, etc.
      return `${provider}::${method}`;
    }
  }
  return toolName;
}

/**
 * Checks if a tool name looks like a custom agent (kebab-case with multiple words)
 * Custom agents have names like: add-safe-output-type, cli-consistency-checker, etc.
 * @param {string} toolName - The tool name to check
 * @returns {boolean} True if the tool name appears to be a custom agent
 */
function isLikelyCustomAgent(toolName) {
  // Custom agents are kebab-case with at least one hyphen and multiple word segments
  // They should not start with common prefixes like 'mcp__', 'safe', etc.
  if (!toolName || typeof toolName !== "string") {
    return false;
  }

  // Must contain at least one hyphen
  if (!toolName.includes("-")) {
    return false;
  }

  // Should not contain double underscores (MCP tools)
  if (toolName.includes("__")) {
    return false;
  }

  // Should not start with safe (safeoutputs, safeinputs handled separately)
  if (toolName.toLowerCase().startsWith("safe")) {
    return false;
  }

  // Should be all lowercase with hyphens (kebab-case)
  // Allow letters, numbers, and hyphens only
  if (!/^[a-z0-9]+(-[a-z0-9]+)+$/.test(toolName)) {
    return false;
  }

  return true;
}

/**
 * Generates information section markdown from the last log entry
 * @param {any} lastEntry - The last log entry with metadata (num_turns, duration_ms, etc.)
 * @param {Object} options - Configuration options
 * @param {Function} [options.additionalInfoCallback] - Optional callback for additional info (lastEntry) => string
 * @returns {string} Information section markdown
 */
function generateInformationSection(lastEntry, options = {}) {
  const { additionalInfoCallback } = options;

  let markdown = "";

  if (!lastEntry) {
    return buildStepSummaryDetailsSection("Information", "", { emptyBodyMessage: "No information available." });
  }

  if (lastEntry.num_turns !== undefined) {
    markdown += `**Turns:** ${lastEntry.num_turns}\n\n`;
  }

  if (lastEntry.duration_ms !== undefined) {
    const durationSec = Math.round(lastEntry.duration_ms / 1000);
    const minutes = Math.floor(durationSec / 60);
    const seconds = durationSec % 60;
    markdown += `**Duration:** ${minutes}m ${seconds}s\n\n`;
  }

  if (lastEntry.total_cost_usd !== undefined) {
    markdown += `**Total Cost:** $${lastEntry.total_cost_usd.toFixed(4)}\n\n`;
  }

  // Call additional info callback if provided (for engine-specific info)
  if (additionalInfoCallback) {
    const additionalInfo = additionalInfoCallback(lastEntry);
    if (additionalInfo) {
      markdown += additionalInfo;
    }
  }

  if (lastEntry.usage) {
    const usage = normalizeSessionUsage(lastEntry.usage) ?? lastEntry.usage;
    const totalTokens = sessionTokenTotal(usage);
    if (totalTokens !== undefined || usage.cache_creation_input_tokens !== undefined || usage.cache_read_input_tokens !== undefined) {
      markdown += `**Token Usage:**\n`;
      const calculation = usage.total_tokens !== undefined ? "source total" : usage.input_tokens_include_cache === false ? "input + output + separate cache" : "input + output";
      if (totalTokens !== undefined) {
        const complete = usage.total_tokens !== undefined || (usage.input_tokens !== undefined && usage.output_tokens !== undefined);
        markdown += complete ? `- Total: ${totalTokens.toLocaleString()} (${calculation})\n` : `- Observed Tokens: ${totalTokens.toLocaleString()} (partial accounting)\n`;
      }
      if (usage.input_tokens !== undefined) markdown += `- Input: ${usage.input_tokens.toLocaleString()}\n`;
      if (usage.cache_creation_input_tokens !== undefined) markdown += `- Cache Creation: ${usage.cache_creation_input_tokens.toLocaleString()}\n`;
      if (usage.cache_read_input_tokens !== undefined) markdown += `- Cache Read: ${usage.cache_read_input_tokens.toLocaleString()}\n`;
      if (usage.output_tokens !== undefined) markdown += `- Output: ${usage.output_tokens.toLocaleString()}\n`;
      markdown += "\n";
    }
  }

  if (lastEntry.errors && Array.isArray(lastEntry.errors) && lastEntry.errors.length > 0) {
    markdown += `**Errors:**\n`;
    for (const error of lastEntry.errors) {
      markdown += `- ${escapeSummaryText(sessionOutputText(error))}\n`;
    }
    markdown += "\n";
  }

  if (Array.isArray(lastEntry.permission_denials)) {
    markdown += `**Permission Denials:** ${lastEntry.permission_denials.length}\n\n`;
    for (const denial of lastEntry.permission_denials) markdown += `- ${escapeSummaryText(sessionOutputText(denial))}\n`;
  }

  return buildStepSummaryDetailsSection("Information", markdown, { emptyBodyMessage: "No information available." });
}

/**
 * Formats MCP parameters into a human-readable string
 * @param {any} input - JSON arguments, including arrays and scalar values
 * @returns {string} Formatted parameters string
 */
function formatMcpParameters(input) {
  if (input === undefined) return "";
  if (input === null || typeof input !== "object" || Array.isArray(input)) return truncateString(displayArgument(input), 100);
  const keys = Object.keys(input);
  if (keys.length === 0) return "";

  const paramStrs = [];
  for (const key of keys.slice(0, 4)) {
    // Show up to 4 parameters
    const rawValue = input[key];
    let value;

    if (Array.isArray(rawValue)) {
      // Format arrays as [item1, item2, ...]
      if (rawValue.length === 0) {
        value = "[]";
      } else if (rawValue.length <= 3) {
        // Show all items for small arrays
        const items = rawValue.map(item => (typeof item === "object" && item !== null ? JSON.stringify(item) : String(item)));
        value = `[${items.join(", ")}]`;
      } else {
        // Show first 2 items and count for larger arrays
        const items = rawValue.slice(0, 2).map(item => (typeof item === "object" && item !== null ? JSON.stringify(item) : String(item)));
        value = `[${items.join(", ")}, ...${rawValue.length - 2} more]`;
      }
    } else if (typeof rawValue === "object" && rawValue !== null) {
      // Format objects as JSON
      value = JSON.stringify(rawValue);
    } else {
      // Primitive values (string, number, boolean, null, undefined)
      value = rawValue === undefined ? "" : String(rawValue);
    }

    paramStrs.push(`${key}: ${truncateString(value, 40)}`);
  }

  if (keys.length > 4) {
    paramStrs.push("...");
  }

  return paramStrs.join(", ");
}

/**
 * Formats a tool name with its input parameters for display in summaries.
 * If the input has parameters, appends them in parentheses; otherwise returns the name unchanged.
 * @param {string} name - The display name of the tool (e.g., "github-list_issues")
 * @param {Object} input - The tool input parameters object
 * @returns {string} Tool name with optional parameters (e.g., "github-list_issues(owner: github, repo: gh-aw)")
 */
function formatToolDisplayName(name, input) {
  const params = formatMcpParameters(input);
  return params ? `${name}(${params})` : name;
}

/**
 * Formats initialization information from system init entry
 * @param {any} initEntry - The system init entry containing tools, mcp_servers, etc.
 * @param {Object} options - Configuration options
 * @param {Function} [options.mcpFailureCallback] - Optional callback for tracking MCP failures (server) => void
 * @param {Function} [options.modelInfoCallback] - Optional callback for rendering model info (initEntry) => string
 * @param {boolean} [options.includeSlashCommands] - Whether to include slash commands section (default: false)
 * @returns {{markdown: string, mcpFailures?: string[]}} Result with formatted markdown string and optional MCP failure list
 */
function formatInitializationSummary(initEntry, options = {}) {
  const { mcpFailureCallback, modelInfoCallback, includeSlashCommands = false } = options;
  let markdown = "";
  const mcpFailures = [];

  if (initEntry.source_engine !== undefined) markdown += `**Engine:** ${escapeSummaryText(sessionOutputText(initEntry.source_engine))}\n\n`;
  // Display model and session info
  if (initEntry.model !== undefined) {
    markdown += `**Model:** ${escapeSummaryText(sessionOutputText(initEntry.model) || '""')}\n\n`;
  }

  // Call model info callback for engine-specific model information (e.g., Copilot premium info)
  if (modelInfoCallback) {
    const modelInfo = modelInfoCallback(initEntry);
    if (modelInfo) {
      markdown += modelInfo;
    }
  } else if (initEntry.model_info !== undefined) {
    markdown += formatToolCallAsDetails({ summary: "Model Information", sections: [{ label: "Metadata", content: JSON.stringify(initEntry.model_info, null, 2), language: "json" }] });
  }

  if (initEntry.session_id !== undefined) {
    markdown += `**Session ID:** ${escapeSummaryText(sessionOutputText(initEntry.session_id) || '""')}\n\n`;
  }

  if (typeof initEntry.cwd === "string") {
    // Show a cleaner path by removing common prefixes
    const cleanCwd = initEntry.cwd.replace(/^\/home\/runner\/work\/[^\/]+\/[^\/]+/, ".");
    markdown += `**Working Directory:** ${escapeSummaryText(cleanCwd || '""')}\n\n`;
  }

  // Display MCP servers status
  if (initEntry.mcp_servers && Array.isArray(initEntry.mcp_servers)) {
    markdown += "**MCP Servers:**\n";
    for (const server of initEntry.mcp_servers) {
      if (!server || typeof server !== "object") {
        markdown += `- ❓ ${escapeSummaryText(sessionOutputText(server))} (unknown)\n`;
        continue;
      }
      const statusIcon = server.status === "connected" ? "✅" : server.status === "failed" ? "❌" : "❓";
      markdown += `- ${statusIcon} ${escapeSummaryText(typeof server.name === "string" ? server.name : "unknown")} (${escapeSummaryText(typeof server.status === "string" ? server.status : "unknown")})\n`;

      // Track failed MCP servers - call callback if provided (for Claude's detailed error tracking)
      if (server.status === "failed") {
        mcpFailures.push(server.name);

        // Call callback to allow engine-specific failure handling
        if (mcpFailureCallback) {
          const failureDetails = mcpFailureCallback(server);
          if (failureDetails) {
            markdown += failureDetails;
          }
        }
      }
    }
    markdown += "\n";
  }

  // Display tools by category
  if (initEntry.tools && Array.isArray(initEntry.tools)) {
    markdown += "**Available Tools:**\n";

    // Categorize tools with improved groupings
    /** @type {{ [key: string]: string[] }} */
    const categories = {
      Core: [],
      "File Operations": [],
      Builtin: [],
      "Safe Outputs": [],
      "MCP Scripts": [],
      "Git/GitHub": [],
      Playwright: [],
      Serena: [],
      MCP: [],
      "Custom Agents": [],
      Other: [],
    };

    // Builtin tools that come with gh-aw / Copilot
    const builtinTools = ["bash", "write_bash", "read_bash", "stop_bash", "list_bash", "grep", "glob", "view", "create", "edit", "store_memory", "code_review", "codeql_checker", "report_progress", "report_intent", "gh-advisory-database"];

    // Internal tools that are specific to Copilot CLI
    const internalTools = ["fetch_copilot_cli_documentation"];

    for (const toolEntry of initEntry.tools) {
      const tool = toolInventoryName(toolEntry);
      if (typeof tool !== "string") continue;
      const toolLower = tool.toLowerCase();

      if (["Task", "Bash", "BashOutput", "KillBash", "ExitPlanMode"].includes(tool)) {
        categories["Core"].push(tool);
      } else if (["Read", "Edit", "MultiEdit", "Write", "LS", "Grep", "Glob", "NotebookEdit"].includes(tool)) {
        categories["File Operations"].push(tool);
      } else if (builtinTools.includes(toolLower) || internalTools.includes(toolLower)) {
        categories["Builtin"].push(tool);
      } else if (tool.startsWith("safeoutputs-") || tool.startsWith("safe_outputs-")) {
        // Extract the tool name without the prefix for cleaner display
        const toolName = tool.replace(/^safeoutputs-|^safe_outputs-/, "");
        categories["Safe Outputs"].push(toolName);
      } else if (tool.startsWith("safeinputs-") || tool.startsWith("mcp_scripts-") || tool.startsWith("mcpscripts-")) {
        // Extract the tool name without the prefix for cleaner display
        const toolName = tool.replace(/^safeinputs-|^mcp_scripts-|^mcpscripts-/, "");
        categories["MCP Scripts"].push(toolName);
      } else if (tool.startsWith("mcp__github__")) {
        categories["Git/GitHub"].push(formatMcpName(tool));
      } else if (tool.startsWith("mcp__playwright__")) {
        categories["Playwright"].push(formatMcpName(tool));
      } else if (tool.startsWith("mcp__serena__")) {
        categories["Serena"].push(formatMcpName(tool));
      } else if (tool.startsWith("mcp__") || ["ListMcpResourcesTool", "ReadMcpResourceTool"].includes(tool)) {
        categories["MCP"].push(tool.startsWith("mcp__") ? formatMcpName(tool) : tool);
      } else if (isLikelyCustomAgent(tool)) {
        // Custom agents typically have hyphenated names (kebab-case)
        categories["Custom Agents"].push(tool);
      } else {
        categories["Other"].push(tool);
      }
    }

    // Display categories with tools
    for (const [category, tools] of Object.entries(categories)) {
      if (tools.length > 0) {
        markdown += `- **${category}:** ${tools.length} tools\n`;
        // Show all tools for complete visibility
        markdown += `  - ${tools.map(escapeSummaryText).join(", ")}\n`;
      }
    }
    markdown += "\n";
  }

  // Display slash commands if available (Claude-specific)
  if (includeSlashCommands && initEntry.slash_commands && Array.isArray(initEntry.slash_commands)) {
    const commandCount = initEntry.slash_commands.length;
    markdown += `**Slash Commands:** ${commandCount} available\n`;
    if (commandCount <= 10) {
      markdown += `- ${initEntry.slash_commands.map(toolInventoryName).map(escapeSummaryText).join(", ")}\n`;
    } else {
      markdown += `- ${initEntry.slash_commands.slice(0, 5).map(toolInventoryName).map(escapeSummaryText).join(", ")}, and ${commandCount - 5} more\n`;
    }
    markdown += "\n";
  }

  // Return format compatible with both engines
  // Claude expects { markdown, mcpFailures }, Copilot expects just markdown
  if (mcpFailures.length > 0) {
    return { markdown, mcpFailures };
  }
  return { markdown };
}

/**
 * Parses log content as JSON array or JSONL format
 * Handles multiple formats: JSON array, JSONL, and mixed format with debug logs
 * @param {string} logContent - The raw log content as a string
 * @returns {Array|null} Array of parsed log entries, or null if parsing fails
 */
function parseLogEntries(logContent) {
  if (typeof logContent !== "string" || !logContent.trim()) return null;
  let logEntries;

  // First, try to parse as JSON array (old format)
  try {
    logEntries = JSON.parse(logContent);
    if (!Array.isArray(logEntries) || logEntries.length === 0) {
      throw new Error(`${ERR_PARSE}: Not a JSON array or empty array`);
    }
    return logEntries;
  } catch (jsonArrayError) {
    // If that fails, try to parse as JSONL format (mixed format with debug logs)
    logEntries = [];
    const lines = logContent.split("\n");

    for (const line of lines) {
      const trimmedLine = line.trim();
      if (trimmedLine === "") {
        continue; // Skip empty lines
      }

      // Handle lines that start with [ (JSON array format)
      if (trimmedLine.startsWith("[{")) {
        try {
          const arrayEntries = JSON.parse(trimmedLine);
          if (Array.isArray(arrayEntries)) {
            logEntries.push(...arrayEntries);
            continue;
          }
        } catch (arrayParseError) {
          // Skip invalid array lines
          continue;
        }
      }

      // Skip debug log lines that don't start with {
      // (these are typically timestamped debug messages)
      if (!trimmedLine.startsWith("{")) {
        continue;
      }

      // Try to parse each line as JSON
      try {
        const jsonEntry = JSON.parse(trimmedLine);
        logEntries.push(jsonEntry);
      } catch (jsonLineError) {
        // Skip invalid JSON lines (could be partial debug output)
        continue;
      }
    }
  }

  // Return null if we couldn't parse anything
  if (!Array.isArray(logEntries) || logEntries.length === 0) {
    return null;
  }

  return logEntries;
}

/**
 * Detects whether entries are in Copilot event log format.
 * @param {Array<any>} logEntries
 * @returns {boolean}
 */
function isCopilotEventLogEntries(logEntries) {
  if (!Array.isArray(logEntries) || logEntries.length === 0) {
    return false;
  }

  return logEntries.some(isSessionEvent);
}

/**
 * Converts legacy trace entries to Copilot event log format.
 * @param {Array<any>} logEntries
 * @param {{sourceEngine?: string}} [options]
 * @returns {Array<any>}
 */
function convertLegacyLogEntriesToCopilotEvents(logEntries, options = {}) {
  return normalizeAgentSession(logEntries, options);
}

/**
 * Converts Copilot event log entries to legacy trace entries used by renderers.
 * @param {Array<any>} logEntries
 * @returns {Array<any>}
 */
function convertCopilotEventsToLegacyLogEntries(logEntries) {
  if (!Array.isArray(logEntries) || logEntries.length === 0) {
    return [];
  }
  if (!isCopilotEventLogEntries(logEntries)) {
    return logEntries;
  }
  logEntries = normalizeAgentSession(logEntries);

  /** @type {Array<any>} */
  const normalizedEntries = [];
  const pendingByToolCallId = new Map();
  const pendingIdsByToolName = new Map();
  let toolCounter = 0;
  const usedToolIds = new Set(logEntries.filter(e => typeof e.data?.toolCallId === "string").map(e => e.data.toolCallId));
  const displayToolId = () => {
    let id;
    do {
      id = `sdk_tool_${++toolCounter}`;
    } while (usedToolIds.has(id));
    usedToolIds.add(id);
    return id;
  };

  const addPendingId = (toolName, toolId) => {
    const existing = pendingIdsByToolName.get(toolName);
    if (existing) {
      existing.push(toolId);
      return;
    }
    pendingIdsByToolName.set(toolName, [toolId]);
  };

  const shiftPendingId = toolName => {
    const existing = pendingIdsByToolName.get(toolName);
    if (!existing || existing.length === 0) return null;
    const toolId = existing.shift();
    if (existing.length === 0) {
      pendingIdsByToolName.delete(toolName);
    }
    return toolId ?? null;
  };

  const removePendingId = (toolName, toolId) => {
    const existing = pendingIdsByToolName.get(toolName);
    if (!existing || existing.length === 0) return;
    const idx = existing.indexOf(toolId);
    if (idx === -1) return;
    existing.splice(idx, 1);
    if (existing.length === 0) {
      pendingIdsByToolName.delete(toolName);
    }
  };

  const normalizeToolName = (rawToolName, mcpServerName) => {
    let toolName = typeof rawToolName === "string" && rawToolName.trim() ? rawToolName.trim() : "unknown";
    // The Copilot CLI emits the builtin shell tool as lowercase "bash", but the
    // shared formatters (formatToolUse, commandSummary bullet list) special-case
    // the capitalized "Bash" name used by Claude. Normalize so Copilot's bash
    // calls get the same command formatting instead of falling through to the
    // generic tool renderer.
    if (toolName.startsWith("mcp__")) {
      return toolName;
    }
    const serverName = typeof mcpServerName === "string" ? mcpServerName.trim() : "";
    if (!serverName) {
      return toolName.toLowerCase() === "bash" ? "Bash" : toolName;
    }
    return `mcp__${serverName}__${toolName}`;
  };

  const readString = (...values) => {
    for (const value of values) {
      if (typeof value === "string") return value;
      if (value !== undefined) return sessionOutputText(value);
    }
    return "";
  };

  // Builds the tool_use `input` object for a Copilot SDK tool event.
  // Copilot SDK bash events carry the executed command as a top-level `data.command`
  // field rather than nesting it inside `data.input`/`data.parameters`, so fall back
  // to it (and merge it in when structured input lacks a command) to avoid dropping
  // the command from the rendered summary.
  // Pass { includeCommand: false } when normalizing orphaned completion events that
  // may still carry structured input but cannot reliably recover the original command.
  const buildToolInput = (data, options = {}) => {
    const { includeCommand = true } = options;
    const base = Object.hasOwn(data, "input") ? data.input : data.parameters;
    if (base && typeof base === "object" && !Array.isArray(base)) {
      if (includeCommand && base.command === undefined && typeof data.command === "string") {
        return { ...base, command: data.command };
      }
      return base;
    }
    if (base !== undefined) return base;
    if (includeCommand && typeof data.command === "string") {
      return { command: data.command };
    }
    return base === undefined ? {} : base;
  };

  for (const entry of logEntries) {
    if (!entry || typeof entry !== "object") continue;
    const data = entry.data && typeof entry.data === "object" ? entry.data : {};

    switch (entry.type) {
      case "session.start":
      case "session.init":
        break;

      case "user.message":
        normalizedEntries.push({ type: "user", message: { content: [{ type: "text", text: data.content }] } });
        break;

      case "assistant.refusal":
        normalizedEntries.push({
          type: "assistant",
          message: { content: [{ type: "text", text: `[Policy refusal: ${data.reason}${data.partial === true ? ", partial" : ""}]\n${readString(data.content, data.explanation)}` }] },
        });
        break;

      case "assistant.message": {
        const text = readString(data.content, data.message);
        if (!text.trim()) break;
        normalizedEntries.push({
          type: "assistant",
          message: {
            content: [{ type: "text", text }],
          },
        });
        break;
      }

      case "assistant.reasoning":
      case "reasoning": {
        const text = readString(data.content);
        if (!text.trim()) break;
        normalizedEntries.push({
          type: "assistant",
          message: {
            content: [{ type: "thinking", thinking: text }],
          },
        });
        break;
      }

      case "tool.execution_start": {
        const toolName = normalizeToolName(data.toolName, data.mcpServerName);
        const toolCallId = typeof data.toolCallId === "string" ? data.toolCallId : null;
        const resolvedToolId = toolCallId ?? displayToolId();
        if (toolCallId !== null) {
          pendingByToolCallId.set(toolCallId, { id: resolvedToolId, name: toolName });
        }
        addPendingId(toolName, resolvedToolId);
        normalizedEntries.push({
          type: "assistant",
          message: {
            content: [{ type: "tool_use", id: resolvedToolId, name: toolName, input: buildToolInput(data), command: data.command, has_input: data.input !== undefined || data.parameters !== undefined, standard_trace: true }],
          },
        });
        break;
      }

      case "tool.execution_complete": {
        const toolName = normalizeToolName(data.toolName, data.mcpServerName);
        const toolCallId = typeof data.toolCallId === "string" ? data.toolCallId : null;
        /** @type {any} */
        let resolvedToolId = null;

        if (toolCallId !== null && pendingByToolCallId.has(toolCallId)) {
          const pending = pendingByToolCallId.get(toolCallId);
          resolvedToolId = pending.id;
          pendingByToolCallId.delete(toolCallId);
          if (resolvedToolId !== null) {
            removePendingId(pending.name, resolvedToolId);
          }
        }
        if (resolvedToolId === null && toolCallId === null && (pendingIdsByToolName.get(toolName)?.length ?? 0) === 1) {
          resolvedToolId = shiftPendingId(toolName);
        }
        if (resolvedToolId === null) {
          resolvedToolId = toolCallId ?? displayToolId();
          normalizedEntries.push({
            type: "assistant",
            message: {
              // Orphaned completion events have no corresponding start event, so keep
              // structured input but do not synthesize a command from completion data.
              content: [
                { type: "tool_use", id: resolvedToolId, name: toolName, input: buildToolInput(data, { includeCommand: false }), has_input: data.input !== undefined || data.parameters !== undefined, orphaned: true, standard_trace: true },
              ],
            },
          });
        }

        const success = sessionToolSuccess(data);
        const output = Object.hasOwn(data, "output") && data.output !== undefined ? sessionOutputText(data.output) : Object.hasOwn(data, "result") ? sessionOutputText(data.result) : data.error != null ? sessionOutputText(data.error) : "";

        normalizedEntries.push({
          type: "user",
          message: {
            content: [
              {
                type: "tool_result",
                tool_use_id: resolvedToolId,
                content: output,
                has_output: data.output !== undefined || data.result !== undefined,
                error: data.error,
                is_error: success === undefined ? undefined : !success,
                duration_ms: isMetric(data.durationMs) ? data.durationMs : undefined,
              },
            ],
          },
        });
        break;
      }

      case "session.result": {
        break;
      }

      default:
        break;
    }
  }

  const result = projectSessionResult(logEntries);
  const init = projectSessionInitialization(logEntries);
  if (init) normalizedEntries.unshift(init);
  if (result) normalizedEntries.push(result);

  return normalizedEntries;
}

const { generateConversationMarkdown, formatToolUse, generatePlainTextSummary, generateCopilotCliStyleSummary } = createLogParserFormatters({
  formatBashCommand,
  formatMcpName,
  formatToolDisplayName,
  formatResultPreview,
  formatToolCallAsDetails,
  formatMcpParameters,
  truncateString,
  estimateTokens,
  formatDuration,
  unfenceMarkdown,
  isCopilotEventLogEntries,
  convertCopilotEventsToLegacyLogEntries,
  generateInformationSection,
  createSummaryTracker: () => new StepSummaryTracker(MAX_STEP_SUMMARY_SIZE - Buffer.byteLength(SIZE_LIMIT_WARNING, "utf8")),
  MAX_STEP_SUMMARY_SIZE,
  MAX_AGENT_TEXT_LENGTH,
  SIZE_LIMIT_WARNING,
});

/**
 * Generic helper to format a tool call as an HTML details section.
 * This is a reusable helper for all code engines (Claude, Copilot, Codex).
 *
 * Tool output/response content is automatically truncated to MAX_TOOL_OUTPUT_LENGTH (256 chars)
 * to keep step summaries readable and prevent size limit issues.
 *
 * @param {Object} options - Configuration options
 * @param {string} options.summary - The summary text to show in the collapsed state (e.g., "✅ github::list_issues")
 * @param {string} [options.statusIcon] - Status icon (✅, ❌, or ❓). If not provided, should be included in summary.
 * @param {Array<{label: string, content: string, language?: string}>} [options.sections] - Array of content sections to show in expanded state
 * @param {string} [options.metadata] - Optional metadata to append to summary (e.g., "~100t", "5s")
 * @param {number} [options.maxContentLength=MAX_TOOL_OUTPUT_LENGTH] - Maximum length for section content before truncation
 * @returns {string} Formatted HTML details string or plain summary if no sections provided
 *
 * @example
 * // Basic usage with sections
 * formatToolCallAsDetails({
 *   summary: "✅ github::list_issues",
 *   metadata: "~100t",
 *   sections: [
 *     { label: "Parameters", content: '{"state":"open"}', language: "json" },
 *     { label: "Response", content: '{"items":[]}', language: "json" }
 *   ]
 * });
 *
 * @example
 * // Bash command usage
 * formatToolCallAsDetails({
 *   summary: "✅ <code>ls -la</code>",
 *   sections: [
 *     { label: "Command", content: "ls -la", language: "bash" },
 *     { label: "Output", content: "file1.txt\nfile2.txt" }
 *   ]
 * });
 */
function formatToolCallAsDetails(options) {
  const { summary, statusIcon, sections, metadata, maxContentLength = MAX_TOOL_OUTPUT_LENGTH } = options;

  // Build the full summary line
  let fullSummary = summary;
  if (statusIcon && !summary.startsWith(statusIcon)) {
    fullSummary = `${statusIcon} ${summary}`;
  }
  if (metadata) {
    fullSummary += ` ${metadata}`;
  }

  // If no sections or all sections are empty, just return the summary
  const hasContent = sections && sections.some(s => s.content && s.content.trim());
  if (!hasContent) {
    return `${fullSummary}\n\n`;
  }

  // Build the details content
  let detailsContent = "";
  for (const section of sections) {
    if (!section.content || !section.content.trim()) {
      continue;
    }

    detailsContent += `**${section.label}:**\n\n`;

    // Truncate content if it exceeds maxContentLength
    let content = section.content;
    if (content.length > maxContentLength) {
      content = content.substring(0, maxContentLength) + "... (truncated)";
    }

    const runs = content.match(/`+/g) ?? [];
    const fence = "`".repeat(runs.reduce((longest, run) => Math.max(longest, run.length + 1), 6));
    detailsContent += `${fence}${section.language || ""}\n`;
    detailsContent += content;
    detailsContent += `\n${fence}\n\n`;
  }

  // Remove trailing newlines from details content
  detailsContent = detailsContent.trimEnd();

  return `<details>\n<summary>${fullSummary}</summary>\n\n${detailsContent}\n</details>\n\n`;
}

/**
 * Formats a tool result content into a preview string showing the first 2 non-empty lines.
 * Uses tree-branch characters (├, └) for visual hierarchy in copilot-cli style.
 *
 * Examples:
 *   1 line:  "   └ result text"
 *   2 lines: "   ├ line 1\n   └ line 2"
 *   3+ lines: "   ├ line 1\n   └ line 2 (+ 1 more)"
 *
 * @param {string} resultText - The result text to preview
 * @param {number} [maxLineLength=80] - Maximum characters per preview line
 * @returns {string} Formatted preview string, or empty string if no content
 */
function formatResultPreview(resultText, maxLineLength = 80) {
  if (!resultText) return "";

  // Scan line-by-line to avoid building a full array for large outputs.
  // Normalize CRLF by stripping trailing \r from each line.
  let firstLine = "";
  let secondLine = "";
  let nonEmptyLineCount = 0;
  let start = 0;

  while (start <= resultText.length) {
    const newlineIndex = resultText.indexOf("\n", start);
    const end = newlineIndex === -1 ? resultText.length : newlineIndex;
    // Strip trailing \r to handle Windows CRLF line endings
    const rawLine = resultText.substring(start, end).replace(/\r$/, "");

    if (rawLine.trim()) {
      nonEmptyLineCount += 1;
      if (nonEmptyLineCount === 1) {
        const truncated = rawLine.substring(0, maxLineLength);
        firstLine = rawLine.length > maxLineLength ? truncated + "..." : truncated;
      } else if (nonEmptyLineCount === 2) {
        const truncated = rawLine.substring(0, maxLineLength);
        secondLine = rawLine.length > maxLineLength ? truncated + "..." : truncated;
      }
    }

    if (newlineIndex === -1) {
      break;
    }
    start = newlineIndex + 1;
  }

  if (nonEmptyLineCount === 0) return "";
  if (nonEmptyLineCount === 1) {
    return `   └ ${firstLine}`;
  }
  if (nonEmptyLineCount === 2) {
    return `   ├ ${firstLine}\n   └ ${secondLine}`;
  }

  return `   ├ ${firstLine}\n   └ ${secondLine} (+ ${nonEmptyLineCount - 2} more)`;
}

/**
 * Wraps agent log markdown in a details/summary section
 * @param {string} markdown - The agent log markdown content
 * @param {Object} options - Configuration options
 * @param {string} [options.parserName="Agent"] - Name of the parser (e.g., "Copilot", "Claude")
 * @param {boolean} [options.open=true] - Whether the section should be open by default
 * @returns {string} Wrapped markdown in details/summary tags
 */
function wrapAgentLogInSection(markdown, options = {}) {
  const { parserName = "Agent", open = true } = options;

  if (!markdown || markdown.trim().length === 0) {
    return "";
  }

  const openAttr = open ? " open" : "";
  const title = "Agentic Conversation";

  return `<details${openAttr}>\n<summary>${title}</summary>\n\n${markdown}\n</details>`;
}

/**
 * Formats safe outputs preview for display in logs
 * @param {string} safeOutputsContent - The raw JSONL content from safe outputs file
 * @param {Object} options - Configuration options
 * @param {boolean} [options.isPlainText=false] - Whether to format for plain text (core.info) or markdown (step summary)
 * @param {number} [options.maxEntries=5] - Maximum number of entries to show in preview
 * @returns {string} Formatted safe outputs preview
 */
function formatSafeOutputsPreview(safeOutputsContent, options = {}) {
  const { isPlainText = false, maxEntries = 5 } = options;

  if (!safeOutputsContent || safeOutputsContent.trim().length === 0) {
    return "";
  }

  const lines = safeOutputsContent.trim().split("\n");
  const entries = [];

  // Parse JSONL entries
  for (const line of lines) {
    if (!line.trim()) continue;
    try {
      const entry = JSON.parse(line);
      entries.push(entry);
    } catch (e) {
      // Skip invalid JSON lines
      continue;
    }
  }

  if (entries.length === 0) {
    return "";
  }

  // Build preview
  const preview = [];
  const entriesToShow = entries.slice(0, maxEntries);
  const hasMore = entries.length > maxEntries;

  if (isPlainText) {
    // Plain text format for core.info
    preview.push("");
    preview.push("Safe Outputs Preview:");
    preview.push(`  Total: ${entries.length} ${entries.length === 1 ? "entry" : "entries"}`);

    for (let i = 0; i < entriesToShow.length; i++) {
      const entry = entriesToShow[i];
      preview.push("");
      preview.push(`  [${i + 1}] ${entry.type || "unknown"}`);
      if (entry.title) {
        const titleStr = typeof entry.title === "string" ? entry.title : String(entry.title);
        preview.push(`      Title: ${truncateString(titleStr, 60)}`);
      }
      if (entry.body) {
        const bodyStr = typeof entry.body === "string" ? entry.body : String(entry.body);
        const bodyPreview = truncateString(bodyStr.replace(/\n/g, " "), 80);
        preview.push(`      Body: ${bodyPreview}`);
      }
      // noop, missing_tool, and missing_data carry their primary content in
      // "message"/"reason" rather than title/body — surface those too.
      if (entry.message) {
        const messageStr = typeof entry.message === "string" ? entry.message : String(entry.message);
        preview.push(`      Message: ${truncateString(messageStr.replace(/\n/g, " "), 80)}`);
      }
      if (entry.reason) {
        const reasonStr = typeof entry.reason === "string" ? entry.reason : String(entry.reason);
        preview.push(`      Reason: ${truncateString(reasonStr.replace(/\n/g, " "), 80)}`);
      }
    }

    if (hasMore) {
      preview.push("");
      preview.push(`  ... and ${entries.length - maxEntries} more ${entries.length - maxEntries === 1 ? "entry" : "entries"}`);
    }
  } else {
    // Markdown format for step summary
    preview.push("");
    preview.push("<details>");
    preview.push("<summary>Safe Outputs</summary>\n");
    preview.push(`**Total Entries:** ${entries.length}`);
    preview.push("");

    for (let i = 0; i < entriesToShow.length; i++) {
      const entry = entriesToShow[i];
      preview.push(`**${i + 1}. ${entry.type || "Unknown Type"}**`);
      preview.push("");

      if (entry.title) {
        const titleStr = typeof entry.title === "string" ? entry.title : String(entry.title);
        preview.push(`**Title:** ${titleStr}`);
        preview.push("");
      }

      if (entry.body) {
        const bodyStr = typeof entry.body === "string" ? entry.body : String(entry.body);
        const bodyPreview = truncateString(bodyStr, 200);
        preview.push("<details>");
        preview.push("<summary>Preview</summary>");
        preview.push("");
        preview.push("``````");
        preview.push(bodyPreview);
        preview.push("``````");
        preview.push("</details>");
        preview.push("");
      }

      // noop, missing_tool, and missing_data carry their primary content in
      // "message"/"reason" rather than title/body — surface those too.
      if (entry.message) {
        const messageStr = typeof entry.message === "string" ? entry.message : String(entry.message);
        preview.push(`**Message:** ${truncateString(messageStr, 200)}`);
        preview.push("");
      }

      if (entry.reason) {
        const reasonStr = typeof entry.reason === "string" ? entry.reason : String(entry.reason);
        preview.push(`**Reason:** ${truncateString(reasonStr, 200)}`);
        preview.push("");
      }

      if (entry.data !== undefined) {
        const dataString = truncateString(JSON.stringify(entry.data, null, 2), 400);
        preview.push("**Data:**");
        preview.push("```json");
        preview.push(dataString);
        preview.push("```");
        preview.push("");
      }
    }

    if (hasMore) {
      preview.push(`*... and ${entries.length - maxEntries} more ${entries.length - maxEntries === 1 ? "entry" : "entries"}*`);
      preview.push("");
    }

    preview.push("</details>");
  }

  return preview.join("\n");
}

/**
 * Wraps a log parser function with consistent error handling.
 * This eliminates duplication of try/catch blocks and error message formatting across parsers.
 *
 * @param {Function} parseFunction - The parser function to wrap
 * @param {string} parserName - Name of the parser (e.g., "Claude", "Copilot", "Codex")
 * @param {string} logContent - The raw log content to parse
 * @returns {{markdown: string, mcpFailures?: string[], maxTurnsHit?: boolean, logEntries?: Array}} Result object with markdown and optional metadata
 */
function wrapLogParser(parseFunction, parserName, logContent) {
  try {
    return parseFunction(logContent);
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    return {
      markdown: `## Agent Log Summary\n\nError parsing ${parserName} log (tried both JSON array and JSONL formats): ${errorMessage}\n`,
      mcpFailures: [],
      maxTurnsHit: false,
      logEntries: [],
    };
  }
}

/**
 * Factory helper to create a standardized engine log parser entry point.
 * Encapsulates the common scaffolding pattern used across all engine parsers.
 *
 * @param {Object} options - Parser configuration options
 * @param {string} options.parserName - Name of the engine (e.g., "Claude", "Copilot", "Codex")
 * @param {(content: string) => string|{markdown: string, mcpFailures?: string[], maxTurnsHit?: boolean, logEntries?: Array<any>}} options.parseFunction - Engine-specific parser function
 * @param {boolean} [options.supportsDirectories=false] - Whether the parser supports reading from directories
 * @returns {() => Promise<void>} Main function that runs the log parser
 */
function createEngineLogParser(options) {
  const { runLogParser } = require("./log_parser_bootstrap.cjs");
  const { parserName, parseFunction, supportsDirectories = false } = options;

  return async function main() {
    await runLogParser({
      parseLog: logContent => wrapLogParser(parseFunction, parserName, logContent),
      parserName,
      supportsDirectories,
    });
  };
}

// Export functions and constants
module.exports = {
  // Constants
  MAX_TOOL_OUTPUT_LENGTH,
  MAX_STEP_SUMMARY_SIZE,
  AWF_INFRA_LINE_RE,
  // Classes
  StepSummaryTracker,
  // Functions
  formatDuration,
  formatBashCommand,
  truncateString,
  estimateTokens,
  formatMcpName,
  isLikelyCustomAgent,
  buildStepSummaryDetailsSection,
  generateConversationMarkdown,
  generateInformationSection,
  formatMcpParameters,
  formatToolDisplayName,
  formatInitializationSummary,
  formatToolUse,
  isCopilotEventLogEntries,
  convertLegacyLogEntriesToCopilotEvents,
  convertCopilotEventsToLegacyLogEntries,
  parseLogEntries,
  formatToolCallAsDetails,
  formatResultPreview,
  generatePlainTextSummary,
  generateCopilotCliStyleSummary,
  wrapAgentLogInSection,
  formatSafeOutputsPreview,
  wrapLogParser,
  createEngineLogParser,
};
