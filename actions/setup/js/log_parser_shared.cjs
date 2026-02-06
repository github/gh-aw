// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { unfenceMarkdown } = require("./markdown_unfencing.cjs");

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
 * Warning message shown when step summary size limit is reached.
 * This message is added directly to markdown (not tracked) to ensure it's always visible.
 * The message is small (~70 bytes) and won't cause practical issues with the 8MB limit.
 */
const SIZE_LIMIT_WARNING = "\n\n⚠️ *Step summary size limit reached. Additional content truncated.*\n\n";

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
 * Generates markdown summary from conversation log entries
 * This is the core shared logic between Claude and Copilot log parsers
 *
 * When a summaryTracker is provided, the function tracks the accumulated size
 * and stops rendering additional content when approaching the step summary limit.
 *
 * @param {Array} logEntries - Array of log entries with type, message, etc.
 * @param {Object} options - Configuration options
 * @param {Function} options.formatToolCallback - Callback function to format tool use (content, toolResult) => string
 * @param {Function} options.formatInitCallback - Callback function to format initialization (initEntry) => string or {markdown: string, mcpFailures: string[]}
 * @param {StepSummaryTracker} [options.summaryTracker] - Optional tracker for step summary size limits
 * @returns {{markdown: string, commandSummary: Array<string>, sizeLimitReached: boolean}} Generated markdown, command summary, and size limit status
 */
function generateConversationMarkdown(logEntries, options) {
  const { formatToolCallback, formatInitCallback, summaryTracker } = options;

  const toolUsePairs = new Map(); // Map tool_use_id to tool_result

  // First pass: collect tool results by tool_use_id
  for (const entry of logEntries) {
    if (entry.type === "user" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_result" && content.tool_use_id) {
          toolUsePairs.set(content.tool_use_id, content);
        }
      }
    }
  }

  let markdown = "";
  let sizeLimitReached = false;

  /**
   * Helper to add content with size tracking
   * @param {string} content - Content to add
   * @returns {boolean} True if content was added, false if limit reached
   */
  function addContent(content) {
    if (summaryTracker && !summaryTracker.add(content)) {
      sizeLimitReached = true;
      return false;
    }
    markdown += content;
    return true;
  }

  // Check for initialization data first
  const initEntry = logEntries.find(entry => entry.type === "system" && entry.subtype === "init");

  if (initEntry && formatInitCallback) {
    if (!addContent("## 🚀 Initialization\n\n")) {
      return { markdown, commandSummary: [], sizeLimitReached };
    }
    const initResult = formatInitCallback(initEntry);
    // Handle both string and object returns (for backward compatibility)
    if (typeof initResult === "string") {
      if (!addContent(initResult)) {
        return { markdown, commandSummary: [], sizeLimitReached };
      }
    } else if (initResult && initResult.markdown) {
      if (!addContent(initResult.markdown)) {
        return { markdown, commandSummary: [], sizeLimitReached };
      }
    }
    if (!addContent("\n")) {
      return { markdown, commandSummary: [], sizeLimitReached };
    }
  }

  if (!addContent("\n## 🤖 Reasoning\n\n")) {
    return { markdown, commandSummary: [], sizeLimitReached };
  }

  // Second pass: process assistant messages in sequence
  for (const entry of logEntries) {
    if (sizeLimitReached) break;

    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (sizeLimitReached) break;

        if (content.type === "text" && content.text) {
          // Add reasoning text directly
          let text = content.text.trim();
          // Apply unfencing to remove accidental outer markdown fences
          text = unfenceMarkdown(text);
          if (text && text.length > 0) {
            if (!addContent(text + "\n\n")) {
              break;
            }
          }
        } else if (content.type === "tool_use") {
          // Process tool use with its result
          const toolResult = toolUsePairs.get(content.id);
          const toolMarkdown = formatToolCallback(content, toolResult);
          if (toolMarkdown) {
            if (!addContent(toolMarkdown)) {
              break;
            }
          }
        }
      }
    }
  }

  // Add size limit notice if limit was reached
  if (sizeLimitReached) {
    markdown += SIZE_LIMIT_WARNING;
    return { markdown, commandSummary: [], sizeLimitReached };
  }

  if (!addContent("## 🤖 Commands and Tools\n\n")) {
    markdown += SIZE_LIMIT_WARNING;
    return { markdown, commandSummary: [], sizeLimitReached: true };
  }

  const commandSummary = []; // For the succinct summary

  // Collect all tool uses for summary
  for (const entry of logEntries) {
    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_use") {
          const toolName = content.name;
          const input = content.input || {};

          // Skip internal tools - only show external commands and API calls
          if (["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"].includes(toolName)) {
            continue; // Skip internal file operations and searches
          }

          // Find the corresponding tool result to get status
          const toolResult = toolUsePairs.get(content.id);
          let statusIcon = "❓";
          if (toolResult) {
            statusIcon = toolResult.is_error === true ? "❌" : "✅";
          }

          // Add to command summary (only external tools)
          if (toolName === "Bash") {
            const formattedCommand = formatBashCommand(input.command || "");
            commandSummary.push(`* ${statusIcon} \`${formattedCommand}\``);
          } else if (toolName.startsWith("mcp__")) {
            const mcpName = formatMcpName(toolName);
            commandSummary.push(`* ${statusIcon} \`${mcpName}(...)\``);
          } else {
            // Handle other external tools (if any)
            commandSummary.push(`* ${statusIcon} ${toolName}`);
          }
        }
      }
    }
  }

  // Add command summary
  if (commandSummary.length > 0) {
    for (const cmd of commandSummary) {
      if (!addContent(`${cmd}\n`)) {
        markdown += SIZE_LIMIT_WARNING;
        return { markdown, commandSummary, sizeLimitReached: true };
      }
    }
  } else {
    if (!addContent("No commands or tools used.\n")) {
      markdown += SIZE_LIMIT_WARNING;
      return { markdown, commandSummary, sizeLimitReached: true };
    }
  }

  return { markdown, commandSummary, sizeLimitReached };
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

  let markdown = "\n## 📊 Information\n\n";

  if (!lastEntry) {
    return markdown;
  }

  if (lastEntry.num_turns) {
    markdown += `**Turns:** ${lastEntry.num_turns}\n\n`;
  }

  if (lastEntry.duration_ms) {
    const durationSec = Math.round(lastEntry.duration_ms / 1000);
    const minutes = Math.floor(durationSec / 60);
    const seconds = durationSec % 60;
    markdown += `**Duration:** ${minutes}m ${seconds}s\n\n`;
  }

  if (lastEntry.total_cost_usd) {
    markdown += `**Total Cost:** $${lastEntry.total_cost_usd.toFixed(4)}\n\n`;
  }

  // Call additional info callback if provided (for engine-specific info like premium requests)
  if (additionalInfoCallback) {
    const additionalInfo = additionalInfoCallback(lastEntry);
    if (additionalInfo) {
      markdown += additionalInfo;
    }
  }

  if (lastEntry.usage) {
    const usage = lastEntry.usage;
    if (usage.input_tokens || usage.output_tokens) {
      // Calculate total tokens (matching Go parser logic)
      const inputTokens = usage.input_tokens || 0;
      const outputTokens = usage.output_tokens || 0;
      const cacheCreationTokens = usage.cache_creation_input_tokens || 0;
      const cacheReadTokens = usage.cache_read_input_tokens || 0;
      const totalTokens = inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens;

      markdown += `**Token Usage:**\n`;
      if (totalTokens > 0) markdown += `- Total: ${totalTokens.toLocaleString()}\n`;
      if (usage.input_tokens) markdown += `- Input: ${usage.input_tokens.toLocaleString()}\n`;
      if (usage.cache_creation_input_tokens) markdown += `- Cache Creation: ${usage.cache_creation_input_tokens.toLocaleString()}\n`;
      if (usage.cache_read_input_tokens) markdown += `- Cache Read: ${usage.cache_read_input_tokens.toLocaleString()}\n`;
      if (usage.output_tokens) markdown += `- Output: ${usage.output_tokens.toLocaleString()}\n`;
      markdown += "\n";
    }
  }

  if (lastEntry.errors && Array.isArray(lastEntry.errors) && lastEntry.errors.length > 0) {
    markdown += `**Errors:**\n`;
    for (const error of lastEntry.errors) {
      markdown += `- ${error}\n`;
    }
    markdown += "\n";
  }

  if (lastEntry.permission_denials && lastEntry.permission_denials.length > 0) {
    markdown += `**Permission Denials:** ${lastEntry.permission_denials.length}\n\n`;
  }

  return markdown;
}

/**
 * Formats MCP parameters into a human-readable string
 * @param {Record<string, any>} input - The input object containing parameters
 * @returns {string} Formatted parameters string
 */
function formatMcpParameters(input) {
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
      value = String(rawValue || "");
    }

    paramStrs.push(`${key}: ${truncateString(value, 40)}`);
  }

  if (keys.length > 4) {
    paramStrs.push("...");
  }

  return paramStrs.join(", ");
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

  // Display model and session info
  if (initEntry.model) {
    markdown += `**Model:** ${initEntry.model}\n\n`;
  }

  // Call model info callback for engine-specific model information (e.g., Copilot premium info)
  if (modelInfoCallback) {
    const modelInfo = modelInfoCallback(initEntry);
    if (modelInfo) {
      markdown += modelInfo;
    }
  }

  if (initEntry.session_id) {
    markdown += `**Session ID:** ${initEntry.session_id}\n\n`;
  }

  if (initEntry.cwd) {
    // Show a cleaner path by removing common prefixes
    const cleanCwd = initEntry.cwd.replace(/^\/home\/runner\/work\/[^\/]+\/[^\/]+/, ".");
    markdown += `**Working Directory:** ${cleanCwd}\n\n`;
  }

  // Display MCP servers status
  if (initEntry.mcp_servers && Array.isArray(initEntry.mcp_servers)) {
    markdown += "**MCP Servers:**\n";
    for (const server of initEntry.mcp_servers) {
      const statusIcon = server.status === "connected" ? "✅" : server.status === "failed" ? "❌" : "❓";
      markdown += `- ${statusIcon} ${server.name} (${server.status})\n`;

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
      "Safe Inputs": [],
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

    for (const tool of initEntry.tools) {
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
      } else if (tool.startsWith("safeinputs-") || tool.startsWith("safe_inputs-")) {
        // Extract the tool name without the prefix for cleaner display
        const toolName = tool.replace(/^safeinputs-|^safe_inputs-/, "");
        categories["Safe Inputs"].push(toolName);
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
        markdown += `  - ${tools.join(", ")}\n`;
      }
    }
    markdown += "\n";
  }

  // Display slash commands if available (Claude-specific)
  if (includeSlashCommands && initEntry.slash_commands && Array.isArray(initEntry.slash_commands)) {
    const commandCount = initEntry.slash_commands.length;
    markdown += `**Slash Commands:** ${commandCount} available\n`;
    if (commandCount <= 10) {
      markdown += `- ${initEntry.slash_commands.join(", ")}\n`;
    } else {
      markdown += `- ${initEntry.slash_commands.slice(0, 5).join(", ")}, and ${commandCount - 5} more\n`;
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
 * Formats a tool use entry with its result into markdown
 * @param {any} toolUse - The tool use object containing name, input, etc.
 * @param {any} toolResult - The corresponding tool result object
 * @param {Object} options - Configuration options
 * @param {boolean} [options.includeDetailedParameters] - Whether to include detailed parameter section (default: false)
 * @returns {string} Formatted markdown string
 */
function formatToolUse(toolUse, toolResult, options = {}) {
  const { includeDetailedParameters = false } = options;
  const toolName = toolUse.name;
  const input = toolUse.input || {};

  // Skip TodoWrite except the very last one (we'll handle this separately)
  if (toolName === "TodoWrite") {
    return ""; // Skip for now, would need global context to find the last one
  }

  // Helper function to determine status icon
  function getStatusIcon() {
    if (toolResult) {
      return toolResult.is_error === true ? "❌" : "✅";
    }
    return "❓"; // Unknown by default
  }

  const statusIcon = getStatusIcon();
  let summary = "";
  let details = "";

  // Get tool output from result
  if (toolResult && toolResult.content) {
    if (typeof toolResult.content === "string") {
      details = toolResult.content;
    } else if (Array.isArray(toolResult.content)) {
      details = toolResult.content.map(c => (typeof c === "string" ? c : c.text || "")).join("\n");
    }
  }

  // Calculate token estimate from input + output
  const inputText = JSON.stringify(input);
  const outputText = details;
  const totalTokens = estimateTokens(inputText) + estimateTokens(outputText);

  // Format metadata (duration and tokens)
  let metadata = "";
  if (toolResult && toolResult.duration_ms) {
    metadata += `<code>${formatDuration(toolResult.duration_ms)}</code> `;
  }
  if (totalTokens > 0) {
    metadata += `<code>~${totalTokens}t</code>`;
  }
  metadata = metadata.trim();

  // Build the summary based on tool type
  switch (toolName) {
    case "Bash":
      const command = input.command || "";
      const description = input.description || "";

      // Format the command to be single line
      const formattedCommand = formatBashCommand(command);

      if (description) {
        summary = `${description}: <code>${formattedCommand}</code>`;
      } else {
        summary = `<code>${formattedCommand}</code>`;
      }
      break;

    case "Read":
      const filePath = input.file_path || input.path || "";
      const relativePath = filePath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, ""); // Remove /home/runner/work/repo/repo/ prefix
      summary = `Read <code>${relativePath}</code>`;
      break;

    case "Write":
    case "Edit":
    case "MultiEdit":
      const writeFilePath = input.file_path || input.path || "";
      const writeRelativePath = writeFilePath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, "");
      summary = `Write <code>${writeRelativePath}</code>`;
      break;

    case "Grep":
    case "Glob":
      const query = input.query || input.pattern || "";
      summary = `Search for <code>${truncateString(query, 80)}</code>`;
      break;

    case "LS":
      const lsPath = input.path || "";
      const lsRelativePath = lsPath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, "");
      summary = `LS: ${lsRelativePath || lsPath}`;
      break;

    default:
      // Handle MCP calls and other tools
      if (toolName.startsWith("mcp__")) {
        const mcpName = formatMcpName(toolName);
        const params = formatMcpParameters(input);
        summary = `${mcpName}(${params})`;
      } else {
        // Generic tool formatting - show the tool name and main parameters
        const keys = Object.keys(input);
        if (keys.length > 0) {
          // Try to find the most important parameter
          const mainParam = keys.find(k => ["query", "command", "path", "file_path", "content"].includes(k)) || keys[0];
          const value = String(input[mainParam] || "");

          if (value) {
            summary = `${toolName}: ${truncateString(value, 100)}`;
          } else {
            summary = toolName;
          }
        } else {
          summary = toolName;
        }
      }
  }

  // Build sections for formatToolCallAsDetails
  /** @type {Array<{label: string, content: string, language?: string}>} */
  const sections = [];

  // For Copilot: include detailed parameters section
  if (includeDetailedParameters) {
    const inputKeys = Object.keys(input);
    if (inputKeys.length > 0) {
      sections.push({
        label: "Parameters",
        content: JSON.stringify(input, null, 2),
        language: "json",
      });
    }
  }

  // Add response section if we have details
  // Note: formatToolCallAsDetails will truncate content to MAX_TOOL_OUTPUT_LENGTH
  if (details && details.trim()) {
    sections.push({
      label: includeDetailedParameters ? "Response" : "Output",
      content: details,
    });
  }

  // Use the shared formatToolCallAsDetails helper
  return formatToolCallAsDetails({
    summary,
    statusIcon,
    sections,
    metadata: metadata || undefined,
  });
}

/**
 * Parses log content as JSON array or JSONL format
 * Handles multiple formats: JSON array, JSONL, and mixed format with debug logs
 * @param {string} logContent - The raw log content as a string
 * @returns {Array|null} Array of parsed log entries, or null if parsing fails
 */
function parseLogEntries(logContent) {
  let logEntries;

  // First, try to parse as JSON array (old format)
  try {
    logEntries = JSON.parse(logContent);
    if (!Array.isArray(logEntries) || logEntries.length === 0) {
      throw new Error("Not a JSON array or empty array");
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

    // Use 6 backticks to avoid conflicts with content that may contain 3 or 5 backticks
    if (section.language) {
      detailsContent += `\`\`\`\`\`\`${section.language}\n`;
    } else {
      detailsContent += "``````\n";
    }
    detailsContent += content;
    detailsContent += "\n``````\n\n";
  }

  // Remove trailing newlines from details content
  detailsContent = detailsContent.trimEnd();

  return `<details>\n<summary>${fullSummary}</summary>\n\n${detailsContent}\n</details>\n\n`;
}

/**
 * Generates a lightweight plain text summary optimized for raw text rendering.
 * This is designed for console output (core.info) instead of markdown step summaries.
 *
 * The output includes:
 * - A compact header with model info
 * - Agent conversation with response text and tool executions
 * - Basic execution statistics
 *
 * @param {Array} logEntries - Array of log entries with type, message, etc.
 * @param {Object} options - Configuration options
 * @param {string} [options.model] - Model name to include in the header
 * @param {string} [options.parserName] - Name of the parser (e.g., "Copilot", "Claude")
 * @returns {string} Plain text summary for console output
 */
function generatePlainTextSummary(logEntries, options = {}) {
  const { model, parserName = "Agent" } = options;
  const lines = [];

  // Header
  lines.push(`=== ${parserName} Execution Summary ===`);
  if (model) {
    lines.push(`Model: ${model}`);
  }
  lines.push("");

  // Collect tool usage pairs for status lookup
  const toolUsePairs = new Map();
  for (const entry of logEntries) {
    if (entry.type === "user" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_result" && content.tool_use_id) {
          toolUsePairs.set(content.tool_use_id, content);
        }
      }
    }
  }

  // Generate conversation flow with agent responses and tool executions
  lines.push("Conversation:");
  lines.push("");

  let conversationLineCount = 0;
  const MAX_CONVERSATION_LINES = 5000; // Limit conversation output
  let conversationTruncated = false;

  for (const entry of logEntries) {
    if (conversationLineCount >= MAX_CONVERSATION_LINES) {
      conversationTruncated = true;
      break;
    }

    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (conversationLineCount >= MAX_CONVERSATION_LINES) {
          conversationTruncated = true;
          break;
        }

        if (content.type === "text" && content.text) {
          // Display agent response text
          let text = content.text.trim();
          // Apply unfencing to remove accidental outer markdown fences
          text = unfenceMarkdown(text);
          if (text && text.length > 0) {
            // Truncate long responses to keep output manageable
            const maxTextLength = 500;
            let displayText = text;
            if (displayText.length > maxTextLength) {
              displayText = displayText.substring(0, maxTextLength) + "...";
            }

            // Split into lines and add Agent prefix
            const textLines = displayText.split("\n");
            for (const line of textLines) {
              if (conversationLineCount >= MAX_CONVERSATION_LINES) {
                conversationTruncated = true;
                break;
              }
              lines.push(`Agent: ${line}`);
              conversationLineCount++;
            }
            lines.push(""); // Add blank line after agent response
            conversationLineCount++;
          }
        } else if (content.type === "tool_use") {
          // Display tool execution
          const toolName = content.name;
          const input = content.input || {};

          // Skip internal tools (file operations)
          if (["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"].includes(toolName)) {
            continue;
          }

          const toolResult = toolUsePairs.get(content.id);
          const isError = toolResult?.is_error === true;
          const statusIcon = isError ? "✗" : "✓";

          // Format tool execution in Copilot CLI style
          let displayName;
          let resultPreview = "";

          if (toolName === "Bash") {
            const cmd = formatBashCommand(input.command || "");
            displayName = `$ ${cmd}`;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : String(toolResult.content);
              const resultLines = resultText.split("\n").filter(l => l.trim());
              if (resultLines.length > 0) {
                const previewLine = resultLines[0].substring(0, 80);
                if (resultLines.length > 1) {
                  resultPreview = `   └ ${resultLines.length} lines...`;
                } else if (previewLine) {
                  resultPreview = `   └ ${previewLine}`;
                }
              }
            }
          } else if (toolName.startsWith("mcp__")) {
            // Format MCP tool names like github-list_pull_requests
            const formattedName = formatMcpName(toolName).replace("::", "-");
            displayName = formattedName;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : JSON.stringify(toolResult.content);
              const truncated = resultText.length > 80 ? resultText.substring(0, 80) + "..." : resultText;
              resultPreview = `   └ ${truncated}`;
            }
          } else {
            displayName = toolName;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : String(toolResult.content);
              const truncated = resultText.length > 80 ? resultText.substring(0, 80) + "..." : resultText;
              resultPreview = `   └ ${truncated}`;
            }
          }

          lines.push(`${statusIcon} ${displayName}`);
          conversationLineCount++;

          if (resultPreview) {
            lines.push(resultPreview);
            conversationLineCount++;
          }

          lines.push(""); // Add blank line after tool execution
          conversationLineCount++;
        }
      }
    }
  }

  if (conversationTruncated) {
    lines.push("... (conversation truncated)");
    lines.push("");
  }

  // Statistics
  const lastEntry = logEntries[logEntries.length - 1];
  lines.push("Statistics:");
  if (lastEntry?.num_turns) {
    lines.push(`  Turns: ${lastEntry.num_turns}`);
  }
  if (lastEntry?.duration_ms) {
    const duration = formatDuration(lastEntry.duration_ms);
    if (duration) {
      lines.push(`  Duration: ${duration}`);
    }
  }

  // Count tools for statistics
  let toolCounts = { total: 0, success: 0, error: 0 };
  for (const entry of logEntries) {
    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_use") {
          const toolName = content.name;
          // Skip internal tools
          if (["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"].includes(toolName)) {
            continue;
          }
          toolCounts.total++;
          const toolResult = toolUsePairs.get(content.id);
          const isError = toolResult?.is_error === true;
          if (isError) {
            toolCounts.error++;
          } else {
            toolCounts.success++;
          }
        }
      }
    }
  }

  if (toolCounts.total > 0) {
    lines.push(`  Tools: ${toolCounts.success}/${toolCounts.total} succeeded`);
  }
  if (lastEntry?.usage) {
    const usage = lastEntry.usage;
    if (usage.input_tokens || usage.output_tokens) {
      // Calculate total tokens (matching Go parser logic)
      const inputTokens = usage.input_tokens || 0;
      const outputTokens = usage.output_tokens || 0;
      const cacheCreationTokens = usage.cache_creation_input_tokens || 0;
      const cacheReadTokens = usage.cache_read_input_tokens || 0;
      const totalTokens = inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens;

      lines.push(`  Tokens: ${totalTokens.toLocaleString()} total (${usage.input_tokens.toLocaleString()} in / ${usage.output_tokens.toLocaleString()} out)`);
    }
  }
  if (lastEntry?.total_cost_usd) {
    lines.push(`  Cost: $${lastEntry.total_cost_usd.toFixed(4)}`);
  }

  return lines.join("\n");
}

/**
 * Generates a markdown-formatted Copilot CLI style summary for step summaries.
 * Similar to generatePlainTextSummary but outputs markdown with code blocks for proper rendering.
 *
 * The output includes:
 * - A "Conversation:" section showing agent responses and tool executions
 * - A "Statistics:" section with execution metrics
 *
 * @param {Array} logEntries - Array of log entries with type, message, etc.
 * @param {Object} options - Configuration options
 * @param {string} [options.model] - Model name to include in the header
 * @param {string} [options.parserName] - Name of the parser (e.g., "Copilot", "Claude")
 * @returns {string} Markdown-formatted summary for step summary rendering
 */
function generateCopilotCliStyleSummary(logEntries, options = {}) {
  const { model, parserName = "Agent" } = options;
  const lines = [];

  // Collect tool usage pairs for status lookup
  const toolUsePairs = new Map();
  for (const entry of logEntries) {
    if (entry.type === "user" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_result" && content.tool_use_id) {
          toolUsePairs.set(content.tool_use_id, content);
        }
      }
    }
  }

  // Generate conversation flow with agent responses and tool executions
  lines.push("```");
  lines.push("Conversation:");
  lines.push("");

  let conversationLineCount = 0;
  const MAX_CONVERSATION_LINES = 5000; // Limit conversation output
  let conversationTruncated = false;

  for (const entry of logEntries) {
    if (conversationLineCount >= MAX_CONVERSATION_LINES) {
      conversationTruncated = true;
      break;
    }

    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (conversationLineCount >= MAX_CONVERSATION_LINES) {
          conversationTruncated = true;
          break;
        }

        if (content.type === "text" && content.text) {
          // Display agent response text
          let text = content.text.trim();
          // Apply unfencing to remove accidental outer markdown fences
          text = unfenceMarkdown(text);
          if (text && text.length > 0) {
            // Truncate long responses to keep output manageable
            const maxTextLength = 500;
            let displayText = text;
            if (displayText.length > maxTextLength) {
              displayText = displayText.substring(0, maxTextLength) + "...";
            }

            // Split into lines and add Agent prefix
            const textLines = displayText.split("\n");
            for (const line of textLines) {
              if (conversationLineCount >= MAX_CONVERSATION_LINES) {
                conversationTruncated = true;
                break;
              }
              lines.push(`Agent: ${line}`);
              conversationLineCount++;
            }
            lines.push(""); // Add blank line after agent response
            conversationLineCount++;
          }
        } else if (content.type === "tool_use") {
          // Display tool execution
          const toolName = content.name;
          const input = content.input || {};

          // Skip internal tools (file operations)
          if (["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"].includes(toolName)) {
            continue;
          }

          const toolResult = toolUsePairs.get(content.id);
          const isError = toolResult?.is_error === true;
          const statusIcon = isError ? "✗" : "✓";

          // Format tool execution in Copilot CLI style
          let displayName;
          let resultPreview = "";

          if (toolName === "Bash") {
            const cmd = formatBashCommand(input.command || "");
            displayName = `$ ${cmd}`;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : String(toolResult.content);
              const resultLines = resultText.split("\n").filter(l => l.trim());
              if (resultLines.length > 0) {
                const previewLine = resultLines[0].substring(0, 80);
                if (resultLines.length > 1) {
                  resultPreview = `   └ ${resultLines.length} lines...`;
                } else if (previewLine) {
                  resultPreview = `   └ ${previewLine}`;
                }
              }
            }
          } else if (toolName.startsWith("mcp__")) {
            // Format MCP tool names like github-list_pull_requests
            const formattedName = formatMcpName(toolName).replace("::", "-");
            displayName = formattedName;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : JSON.stringify(toolResult.content);
              const truncated = resultText.length > 80 ? resultText.substring(0, 80) + "..." : resultText;
              resultPreview = `   └ ${truncated}`;
            }
          } else {
            displayName = toolName;

            // Show result preview if available
            if (toolResult && toolResult.content) {
              const resultText = typeof toolResult.content === "string" ? toolResult.content : String(toolResult.content);
              const truncated = resultText.length > 80 ? resultText.substring(0, 80) + "..." : resultText;
              resultPreview = `   └ ${truncated}`;
            }
          }

          lines.push(`${statusIcon} ${displayName}`);
          conversationLineCount++;

          if (resultPreview) {
            lines.push(resultPreview);
            conversationLineCount++;
          }

          lines.push(""); // Add blank line after tool execution
          conversationLineCount++;
        }
      }
    }
  }

  if (conversationTruncated) {
    lines.push("... (conversation truncated)");
    lines.push("");
  }

  // Statistics
  const lastEntry = logEntries[logEntries.length - 1];
  lines.push("Statistics:");
  if (lastEntry?.num_turns) {
    lines.push(`  Turns: ${lastEntry.num_turns}`);
  }
  if (lastEntry?.duration_ms) {
    const duration = formatDuration(lastEntry.duration_ms);
    if (duration) {
      lines.push(`  Duration: ${duration}`);
    }
  }

  // Count tools for statistics
  let toolCounts = { total: 0, success: 0, error: 0 };
  for (const entry of logEntries) {
    if (entry.type === "assistant" && entry.message?.content) {
      for (const content of entry.message.content) {
        if (content.type === "tool_use") {
          const toolName = content.name;
          // Skip internal tools
          if (["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"].includes(toolName)) {
            continue;
          }
          toolCounts.total++;
          const toolResult = toolUsePairs.get(content.id);
          const isError = toolResult?.is_error === true;
          if (isError) {
            toolCounts.error++;
          } else {
            toolCounts.success++;
          }
        }
      }
    }
  }

  if (toolCounts.total > 0) {
    lines.push(`  Tools: ${toolCounts.success}/${toolCounts.total} succeeded`);
  }
  if (lastEntry?.usage) {
    const usage = lastEntry.usage;
    if (usage.input_tokens || usage.output_tokens) {
      // Calculate total tokens (matching Go parser logic)
      const inputTokens = usage.input_tokens || 0;
      const outputTokens = usage.output_tokens || 0;
      const cacheCreationTokens = usage.cache_creation_input_tokens || 0;
      const cacheReadTokens = usage.cache_read_input_tokens || 0;
      const totalTokens = inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens;

      lines.push(`  Tokens: ${totalTokens.toLocaleString()} total (${usage.input_tokens.toLocaleString()} in / ${usage.output_tokens.toLocaleString()} out)`);
    }
  }
  if (lastEntry?.total_cost_usd) {
    lines.push(`  Cost: $${lastEntry.total_cost_usd.toFixed(4)}`);
  }

  lines.push("```");

  return lines.join("\n");
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
  const emoji = "🤖";
  const title = `${emoji} ${parserName} CLI Session`;

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
        preview.push("```");
        preview.push(bodyPreview);
        preview.push("```");
        preview.push("</details>");
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
 * @param {function(string): string|{markdown: string, mcpFailures?: string[], maxTurnsHit?: boolean, logEntries?: Array}} options.parseFunction - Engine-specific parser function
 * @param {boolean} [options.supportsDirectories=false] - Whether the parser supports reading from directories
 * @returns {function(): Promise<void>} Main function that runs the log parser
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
  // Classes
  StepSummaryTracker,
  // Functions
  formatDuration,
  formatBashCommand,
  truncateString,
  estimateTokens,
  formatMcpName,
  isLikelyCustomAgent,
  generateConversationMarkdown,
  generateInformationSection,
  formatMcpParameters,
  formatInitializationSummary,
  formatToolUse,
  parseLogEntries,
  formatToolCallAsDetails,
  generatePlainTextSummary,
  generateCopilotCliStyleSummary,
  wrapAgentLogInSection,
  formatSafeOutputsPreview,
  wrapLogParser,
  createEngineLogParser,
};
