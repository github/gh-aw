// @ts-check

const { buildStepSummaryDetailsSection } = require("./log_parser_step_summary_builder.cjs");
const { sessionTokenTotal, sessionOutputText, observedSessionModel, projectSessionResult } = require("./agent_session.cjs");
const { escapeSummaryText, renderInitializationLines, toolOutcome, boundSummaryLines } = require("./agent_session_render.cjs");
const { isUnifiedSessionTrace, publicationAgentSessions, renderUnifiedSession } = require("./unified_session_render.cjs");

/**
 * Minimal dependency contract injected from log_parser_shared.cjs.
 * Keeping this explicit helps prevent silent drift between modules.
 *
 * @typedef {Object} LogParserFormatterDeps
 * @property {(command: string) => string} formatBashCommand
 * @property {(toolName: string) => string} formatMcpName
 * @property {(name: string, input: Object) => string} formatToolDisplayName
 * @property {(resultText: string, maxLineLength?: number) => string} formatResultPreview
 * @property {(options: {summary: string, statusIcon?: string, sections?: Array<{label: string, content: string, language?: string}>, metadata?: string, maxContentLength?: number}) => string} formatToolCallAsDetails
 * @property {(input: Record<string, any>) => string} formatMcpParameters
 * @property {(str: string, maxLength: number) => string} truncateString
 * @property {(text: string) => number} estimateTokens
 * @property {(ms: number) => string} formatDuration
 * @property {(text: string) => string} unfenceMarkdown
 * @property {(entries: Array<any>) => boolean} isCopilotEventLogEntries
 * @property {(entries: Array<any>) => Array<any>} convertCopilotEventsToLegacyLogEntries
 * @property {(entry: any) => string} generateInformationSection
 * @property {() => any} createSummaryTracker
 * @property {number} MAX_STEP_SUMMARY_SIZE
 * @property {number} MAX_AGENT_TEXT_LENGTH
 * @property {string} SIZE_LIMIT_WARNING
 */

/**
 * Public formatter API returned by createLogParserFormatters().
 *
 * @typedef {Object} LogParserFormatters
 * @property {(logEntries: Array<any>, options: {formatToolCallback: Function, formatInitCallback: Function, summaryTracker?: any, includeInformation?: boolean}) => {markdown: string, commandSummary: Array<string>, sizeLimitReached: boolean}} generateConversationMarkdown
 * @property {(toolUse: any, toolResult: any, options?: {includeDetailedParameters?: boolean}) => string} formatToolUse
 * @property {(logEntries: Array<any>, options?: {model?: string, parserName?: string}) => string} generatePlainTextSummary
 * @property {(logEntries: Array<any>, options?: {model?: string, parserName?: string, maxBytes?: number}) => string} generateCopilotCliStyleSummary
 */

/**
 * Creates formatter functions for log parsing summaries and rendering.
 * Dependencies are injected to avoid module cycles with log_parser_shared.cjs.
 *
 * @param {LogParserFormatterDeps} deps - Dependency injection container
 * @returns {LogParserFormatters} Formatter functions
 */
function createLogParserFormatters(deps) {
  const {
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
    createSummaryTracker,
    MAX_STEP_SUMMARY_SIZE,
    MAX_AGENT_TEXT_LENGTH,
    SIZE_LIMIT_WARNING,
  } = deps;

  const INTERNAL_TOOLS = ["Read", "Write", "Edit", "MultiEdit", "LS", "Grep", "Glob", "TodoWrite"];

  /**
   * Selects an outer markdown code fence that is longer than any backtick run
   * present in the rendered content, so nested code fences in agent output
   * cannot prematurely close the wrapper fence.
   * @param {string[]} contentLines
   * @returns {string}
   */
  function buildSafeOuterCodeFence(contentLines) {
    let maxBacktickRun = 0;
    for (const line of contentLines) {
      const text = String(line ?? "");
      const runRe = /`+/g;
      let match;
      while ((match = runRe.exec(text)) !== null) {
        if (match[0].length > maxBacktickRun) {
          maxBacktickRun = match[0].length;
        }
      }
    }
    return "`".repeat(Math.max(3, maxBacktickRun + 1));
  }

  function normalizeEntriesForRendering(logEntries) {
    if (!Array.isArray(logEntries)) return [];
    if (isCopilotEventLogEntries(logEntries)) {
      return convertCopilotEventsToLegacyLogEntries(logEntries);
    }
    return logEntries.filter(entry => entry && typeof entry === "object");
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
   * @param {any} [options.summaryTracker] - Optional tracker for step summary size limits
   * @param {boolean} [options.includeInformation] - Include accounting for canonical traces by default
   * @returns {{markdown: string, commandSummary: Array<string>, sizeLimitReached: boolean}} Generated markdown, command summary, and size limit status
   */
  function generateConversationMarkdown(logEntries, options) {
    if (isUnifiedSessionTrace(logEntries)) {
      const markdown = unifiedSummary(logEntries, true);
      return {
        markdown,
        commandSummary: publicationAgentSessions(logEntries).flatMap(group => generateConversationMarkdown(group.events, options).commandSummary),
        sizeLimitReached: markdown.includes("summary truncated:") || markdown.includes("byte limit reached"),
      };
    }
    const standard = isCopilotEventLogEntries(logEntries);
    const { formatToolCallback, formatInitCallback } = options;
    const summaryTracker = options.summaryTracker ?? (standard ? createSummaryTracker() : undefined);
    const renderEntries = normalizeEntriesForRendering(logEntries);
    const toolUsePairs = collectToolUsePairs(renderEntries);

    let markdown = "";
    let sizeLimitReached = false;

    function addContent(content) {
      if (summaryTracker && !summaryTracker.add(content)) {
        sizeLimitReached = true;
        return false;
      }
      markdown += content;
      return true;
    }

    /**
     * Adds a details section, truncating the body when it would exceed the
     * remaining step-summary budget. Emits partial content with a truncation
     * note rather than dropping the entire section.
     * @param {string} title
     * @param {string} body
     * @returns {boolean} True if any content was emitted
     */
    function addDetailsSectionFitting(title, body) {
      const fullSection = buildStepSummaryDetailsSection(title, body);
      if (!summaryTracker || Buffer.byteLength(fullSection, "utf8") <= summaryTracker.remaining()) {
        return addContent(fullSection);
      }
      sizeLimitReached = true;

      // Full section doesn't fit — try truncating the body to use what remains.
      if (!summaryTracker) {
        return false;
      }

      const truncationNote = "\n\n*(content truncated — step summary size limit reached)*\n";
      const truncNoteSize = Buffer.byteLength(truncationNote, "utf8");
      const shell = `<details>\n<summary>${title}</summary>\n\n\n</details>\n\n`;
      const shellSize = Buffer.byteLength(shell, "utf8");
      const availableForBody = summaryTracker.remaining() - shellSize - truncNoteSize;

      if (availableForBody <= 0) {
        return false;
      }

      // Truncate body at a clean UTF-8 character boundary.
      // UTF-8 continuation bytes have the form 10xxxxxx (0x80–0xBF).
      // Walking back past them ensures the cutoff lands on a start byte
      // (0x00–0x7F for ASCII, 0xC0–0xFF for multi-byte leaders), so the
      // resulting slice is always a well-formed UTF-8 string.
      const UTF8_CONTINUATION_MASK = 0xc0;
      const UTF8_CONTINUATION_PREFIX = 0x80;
      const bodyBuf = Buffer.from(body, "utf8");
      let cutoff = Math.min(availableForBody, bodyBuf.length);
      while (cutoff > 0 && (bodyBuf[cutoff] & UTF8_CONTINUATION_MASK) === UTF8_CONTINUATION_PREFIX) {
        cutoff--;
      }
      const truncatedBody = bodyBuf.slice(0, cutoff).toString("utf8") + truncationNote;
      return addContent(buildStepSummaryDetailsSection(title, truncatedBody));
    }

    const initEntry = renderEntries.find(entry => entry.type === "system" && entry.subtype === "init");
    if (initEntry && formatInitCallback) {
      const initResult = formatInitCallback(initEntry);
      const initBody = typeof initResult === "string" ? initResult : initResult && initResult.markdown ? initResult.markdown : "";
      if (!addContent(buildStepSummaryDetailsSection("Initialization", initBody))) {
        markdown += SIZE_LIMIT_WARNING;
        return { markdown, commandSummary: [], sizeLimitReached };
      }
    }
    const observedModel = observedSessionModel(logEntries);
    if (observedModel && observedModel !== initEntry?.model) {
      if (!addDetailsSectionFitting("Model", `**Observed Model:** ${escapeSummaryText(observedModel)}\n`)) {
        markdown += SIZE_LIMIT_WARNING;
        return { markdown, commandSummary: [], sizeLimitReached };
      }
    }

    let reasoningBody = "";
    let commandDetailsBody = "";

    for (const entry of renderEntries) {
      if (entry.type !== "assistant" || !entry.message?.content) {
        continue;
      }
      if (summaryTracker && summaryTracker.isLimitReached()) {
        break;
      }

      for (const content of entry.message.content) {
        if (content.type === "text" && content.text) {
          let text = content.text.trim();
          text = unfenceMarkdown(text);
          if (standard) text = escapeSummaryText(text);
          if (text) {
            reasoningBody += text + "\n\n";
          }
        } else if (content.type === "thinking" && content.thinking) {
          let text = content.thinking.trim();
          text = unfenceMarkdown(text);
          if (standard) text = escapeSummaryText(text);
          if (text) {
            reasoningBody += `<sub><em>${text.replace(/\n/g, "<br>")}</em></sub>\n\n`;
          }
        } else if (content.type === "tool_use") {
          const toolResult = toolUsePairs.get(content.id);
          const toolMarkdown = formatToolCallback(content, toolResult);
          if (toolMarkdown) {
            commandDetailsBody += toolMarkdown;
          }
        }
      }
    }

    if (!addDetailsSectionFitting("Reasoning", reasoningBody)) {
      markdown += SIZE_LIMIT_WARNING;
      return { markdown, commandSummary: [], sizeLimitReached: true };
    }

    const commandSummary = [];
    for (const entry of renderEntries) {
      if (entry.type !== "assistant" || !entry.message?.content) {
        continue;
      }
      if (summaryTracker && summaryTracker.isLimitReached()) {
        break;
      }

      for (const content of entry.message.content) {
        if (content.type !== "tool_use") {
          continue;
        }

        const toolName = typeof content.name === "string" ? content.name : "unknown";
        const input = content.input === undefined ? {} : content.input;
        const fields = input && typeof input === "object" && !Array.isArray(input) ? input : {};
        if (!standard && INTERNAL_TOOLS.includes(toolName)) {
          continue;
        }

        const toolResult = toolUsePairs.get(content.id);
        let statusIcon = "❓";
        if (toolResult) {
          statusIcon = toolResult.is_error === true ? "❌" : toolResult.is_error === false ? "✅" : "❓";
        }

        if (toolName === "Bash") {
          const command = typeof fields.command === "string" ? fields.command : typeof content.command === "string" ? content.command : undefined;
          if (command === undefined) commandSummary.push(`* ${statusIcon} ${escapeSummaryText(formatToolDisplayName(toolName, input))}`);
          else {
            const formattedCommand = formatBashCommand(command);
            const fence = formattedCommand.includes("`") ? buildSafeOuterCodeFence([formattedCommand]) : "`";
            commandSummary.push(`* ${statusIcon} ${fence}${formattedCommand}${fence}`);
          }
        } else if (toolName.startsWith("mcp__")) {
          const mcpName = formatMcpName(toolName);
          const fence = mcpName.includes("`") ? buildSafeOuterCodeFence([mcpName]) : "`";
          commandSummary.push(`* ${statusIcon} ${fence}${mcpName}(...)${fence}`);
        } else {
          commandSummary.push(`* ${statusIcon} ${escapeSummaryText(toolName)}`);
        }
      }
    }

    let commandsBody = "";
    if (commandSummary.length > 0) {
      commandsBody += commandSummary.join("\n") + "\n\n";
    } else {
      commandsBody += "No commands or tools used.\n";
    }
    if (commandDetailsBody.trim()) {
      commandsBody += commandDetailsBody.trim() + "\n";
    }

    if (!addDetailsSectionFitting("Commands and Tools", commandsBody)) {
      markdown += SIZE_LIMIT_WARNING;
      return { markdown, commandSummary, sizeLimitReached: true };
    }
    if (options.includeInformation ?? standard) {
      const info = generateInformationSection(projectSessionResult(logEntries));
      if (!addContent(info)) {
        markdown += SIZE_LIMIT_WARNING;
        return { markdown, commandSummary, sizeLimitReached: true };
      }
    }

    return { markdown, commandSummary, sizeLimitReached };
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
    const toolName = typeof toolUse.name === "string" ? toolUse.name : "unknown";
    const input = toolUse.input === undefined ? {} : toolUse.input;
    const fields = input && typeof input === "object" && !Array.isArray(input) ? input : {};
    const standard = toolUse.standard_trace === true;

    if (toolName === "TodoWrite" && !standard) {
      return "";
    }

    function getStatusIcon() {
      return { failed: "❌", succeeded: "✅", pending: "❓", unknown: "❓" }[toolOutcome(toolResult)];
    }

    const statusIcon = getStatusIcon();
    let summary = "";
    let details = "";

    const outputPresent = toolResult && (toolResult.has_output ?? toolResult.content !== undefined);
    if (outputPresent) details = sessionOutputText(toolResult.content);
    const errorText = toolResult?.error != null ? sessionOutputText(toolResult.error) : "";

    const inputText = JSON.stringify(input);
    const outputText = details;
    const totalTokens = estimateTokens(inputText) + estimateTokens(outputText);

    let metadata = "";
    if (toolResult && toolResult.duration_ms !== undefined) {
      metadata += `<code>${toolResult.duration_ms === 0 ? "0s" : formatDuration(toolResult.duration_ms)}</code> `;
    }
    if (totalTokens > 0) {
      metadata += `<code>~${totalTokens}t</code>`;
    }
    metadata = metadata.trim();

    switch (toolName) {
      case "Bash": {
        const command = typeof fields.command === "string" ? fields.command : typeof toolUse.command === "string" ? toolUse.command : undefined;
        const description = typeof fields.description === "string" ? fields.description : "";
        if (command === undefined) {
          summary = escapeSummaryText(formatToolDisplayName(toolName, input));
          break;
        }
        const formattedCommand = formatBashCommand(command);

        if (description) {
          summary = `${escapeSummaryText(description)}: <code>${escapeSummaryText(formattedCommand)}</code>`;
        } else {
          summary = `<code>${escapeSummaryText(formattedCommand)}</code>`;
        }
        break;
      }

      case "Read": {
        const filePath = typeof fields.file_path === "string" ? fields.file_path : typeof fields.path === "string" ? fields.path : "";
        const relativePath = filePath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, "");
        summary = filePath ? `Read <code>${escapeSummaryText(relativePath)}</code>` : escapeSummaryText(formatToolDisplayName(toolName, input));
        break;
      }

      case "Write":
      case "Edit":
      case "MultiEdit": {
        const writeFilePath = typeof fields.file_path === "string" ? fields.file_path : typeof fields.path === "string" ? fields.path : "";
        const writeRelativePath = writeFilePath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, "");
        summary = writeFilePath ? `Write <code>${escapeSummaryText(writeRelativePath)}</code>` : escapeSummaryText(formatToolDisplayName(toolName, input));
        break;
      }

      case "Grep":
      case "Glob": {
        const query = typeof fields.query === "string" ? fields.query : typeof fields.pattern === "string" ? fields.pattern : "";
        summary = query ? `Search for <code>${escapeSummaryText(truncateString(query, 80))}</code>` : escapeSummaryText(formatToolDisplayName(toolName, input));
        break;
      }

      case "LS": {
        const lsPath = typeof fields.path === "string" ? fields.path : "";
        const lsRelativePath = lsPath.replace(/^\/[^\/]*\/[^\/]*\/[^\/]*\/[^\/]*\//, "");
        summary = lsPath ? `LS: ${escapeSummaryText(lsRelativePath || lsPath)}` : escapeSummaryText(formatToolDisplayName(toolName, input));
        break;
      }

      default:
        if (toolName.startsWith("mcp__")) {
          const mcpName = formatMcpName(toolName);
          const params = formatMcpParameters(input);
          summary = escapeSummaryText(`${mcpName}(${params})`);
        } else {
          const keys = Object.keys(fields);
          if (keys.length > 0) {
            const mainParam = keys.find(k => ["query", "command", "path", "file_path", "content"].includes(k)) || keys[0];
            const value = sessionOutputText(fields[mainParam]);

            if (value) {
              summary = escapeSummaryText(`${toolName}: ${truncateString(value, 100)}`);
            } else {
              summary = escapeSummaryText(toolName);
            }
          } else {
            summary = escapeSummaryText(standard && toolUse.has_input && input && typeof input === "object" && !Array.isArray(input) ? `${toolName}({})` : formatToolDisplayName(toolName, input));
          }
        }
    }

    /** @type {Array<{label: string, content: string, language?: string}>} */
    const sections = [];

    if (includeDetailedParameters) {
      if (Object.keys(fields).length > 0 || input === null || typeof input !== "object" || Array.isArray(input) || toolUse.has_input) {
        sections.push({
          label: "Parameters",
          content: JSON.stringify(input, null, 2),
          language: "json",
        });
      }
    }

    if (outputPresent) {
      sections.push({
        label: includeDetailedParameters ? "Response" : "Output",
        content: details || "[empty output]",
      });
    }
    if (errorText) sections.push({ label: "Error", content: errorText });
    if (standard) {
      if (toolUse.orphaned) summary += " [start unavailable]";
      if (!toolResult) summary += " [pending]";
      else if (toolOutcome(toolResult) === "unknown") summary += " [outcome unknown]";
    }

    return formatToolCallAsDetails({
      summary,
      statusIcon,
      sections,
      metadata: metadata || undefined,
    });
  }

  function collectToolUsePairs(logEntries) {
    const toolUsePairs = new Map();
    for (const entry of logEntries) {
      if (entry.type === "user" && entry.message?.content) {
        for (const content of entry.message.content) {
          if (content.type === "tool_result" && content.tool_use_id !== undefined) {
            toolUsePairs.set(content.tool_use_id, content);
          }
        }
      }
    }
    return toolUsePairs;
  }

  function appendConversationLine(lines, line, state) {
    if (state.conversationLineCount >= state.maxConversationLines) {
      state.conversationTruncated = true;
      return false;
    }
    lines.push(line);
    state.conversationLineCount++;
    return true;
  }

  function appendAgentText(lines, text, state) {
    let displayText = text;
    if (displayText.length > MAX_AGENT_TEXT_LENGTH) {
      displayText = displayText.substring(0, MAX_AGENT_TEXT_LENGTH) + `... [truncated: showing first ${MAX_AGENT_TEXT_LENGTH} of ${text.length} chars]`;
    }

    const textLines = displayText.split("\n");
    for (let i = 0; i < textLines.length; i++) {
      if (i === 0) {
        state.traceEventCount += 1;
      }
      const prefix = i === 0 ? `[${state.traceEventCount}] ◆ ` : "  ";
      if (!appendConversationLine(lines, `${prefix}${textLines[i]}`, state)) {
        return;
      }
    }
    appendConversationLine(lines, "", state);
  }

  function appendReasoningText(lines, text, state) {
    let displayText = text;
    if (displayText.length > MAX_AGENT_TEXT_LENGTH) {
      displayText = displayText.substring(0, MAX_AGENT_TEXT_LENGTH) + `... [truncated: showing first ${MAX_AGENT_TEXT_LENGTH} of ${text.length} chars]`;
    }

    const textLines = displayText.split("\n");
    for (let i = 0; i < textLines.length; i++) {
      if (i === 0) {
        state.traceEventCount += 1;
      }
      const prefix = i === 0 ? `[${state.traceEventCount}] ◐ ` : "  ";
      if (!appendConversationLine(lines, `${prefix}${textLines[i]}`, state)) {
        return;
      }
    }
    appendConversationLine(lines, "", state);
  }

  function appendToolExecutionLine(lines, content, toolUsePairs, state) {
    const toolName = typeof content.name === "string" ? content.name : "unknown";
    const input = content.input === undefined ? {} : content.input;
    const fields = input && typeof input === "object" && !Array.isArray(input) ? input : {};

    if (!state.standard && INTERNAL_TOOLS.includes(toolName)) {
      return;
    }

    const toolResult = toolUsePairs.get(content.id);
    const outcome = toolOutcome(toolResult);
    const statusIcon = outcome === "failed" ? "✗" : outcome === "succeeded" ? "✓" : "?";

    let displayName;

    if (toolName === "Bash") {
      const command = typeof fields.command === "string" ? fields.command : typeof content.command === "string" ? content.command : undefined;
      displayName = command === undefined ? formatToolDisplayName(toolName, input) : `$ ${formatBashCommand(command)}`;
    } else if (toolName.startsWith("mcp__")) {
      const formattedName = formatMcpName(toolName).replace("::", "-");
      displayName = formatToolDisplayName(formattedName, input);
    } else {
      displayName = formatToolDisplayName(toolName, input);
    }
    if (state.standard) {
      if (content.has_input && input && typeof input === "object" && !Array.isArray(input) && Object.keys(input).length === 0) displayName += " [input: {}]";
      if (content.orphaned) displayName += " [start unavailable]";
      if (outcome === "pending") displayName += " [pending]";
      if (outcome === "unknown") displayName += " [outcome unknown]";
      if (toolResult?.duration_ms !== undefined) displayName += ` [${toolResult.duration_ms === 0 ? "0s" : formatDuration(toolResult.duration_ms)}]`;
    }

    state.traceEventCount += 1;
    if (!appendConversationLine(lines, `[${state.traceEventCount}] ${statusIcon} ${displayName}`, state)) {
      return;
    }

    if (state.standard && toolName === "Bash" && content.has_input && (Object.keys(fields).length > 1 || input === null || typeof input !== "object" || Array.isArray(input))) {
      if (!appendConversationLine(lines, `   Arguments: ${formatMcpParameters(input)}`, state)) return;
    }
    if (toolResult && (toolResult.has_output ?? toolResult.content !== undefined)) {
      const resultText = sessionOutputText(toolResult.content) || "[empty output]";
      for (const previewLine of formatResultPreview(resultText).split("\n")) {
        if (!appendConversationLine(lines, previewLine, state)) {
          return;
        }
      }
    }
    if (toolResult?.error != null) {
      for (const errorLine of formatResultPreview(sessionOutputText(toolResult.error)).split("\n")) {
        if (!appendConversationLine(lines, `   Error: ${errorLine.trimStart()}`, state)) return;
      }
    }

    appendConversationLine(lines, "", state);
  }

  function appendStatistics(lines, logEntries, toolUsePairs, standard) {
    const lastEntry = logEntries.findLast(entry => entry.type === "result");
    lines.push("Statistics:");
    if (lastEntry?.num_turns !== undefined) {
      lines.push(`  Turns: ${lastEntry.num_turns}`);
    }
    if (lastEntry?.duration_ms !== undefined) {
      const duration = lastEntry.duration_ms === 0 ? "0s" : formatDuration(lastEntry.duration_ms);
      if (duration) {
        lines.push(`  Duration: ${duration}`);
      }
    }

    let toolCounts = { total: 0, success: 0, error: 0, pending: 0, unknown: 0 };
    for (const entry of logEntries) {
      if (entry.type === "assistant" && entry.message?.content) {
        for (const content of entry.message.content) {
          if (content.type === "tool_use") {
            const toolName = content.name;
            if (!standard && INTERNAL_TOOLS.includes(toolName)) {
              continue;
            }
            toolCounts.total++;
            const toolResult = toolUsePairs.get(content.id);
            const outcome = toolOutcome(toolResult);
            if (outcome === "failed") {
              toolCounts.error++;
            } else if (outcome === "succeeded") {
              toolCounts.success++;
            } else toolCounts[outcome]++;
          }
        }
      }
    }

    if (toolCounts.total > 0) {
      lines.push(`  Tools: ${toolCounts.success}/${toolCounts.total} succeeded`);
      if (standard) {
        if (toolCounts.error) lines.push(`  Failed Tools: ${toolCounts.error}`);
        if (toolCounts.pending) lines.push(`  Pending Tools: ${toolCounts.pending}`);
        if (toolCounts.unknown) lines.push(`  Unknown Outcomes: ${toolCounts.unknown}`);
      }
    }
    if (lastEntry?.usage) {
      const usage = lastEntry.usage;
      const totalTokens = sessionTokenTotal(usage);
      if (totalTokens !== undefined) {
        const inputTokens = usage.input_tokens === undefined ? "unknown" : usage.input_tokens.toLocaleString();
        const outputTokens = usage.output_tokens === undefined ? "unknown" : usage.output_tokens.toLocaleString();
        const complete = usage.total_tokens !== undefined || (usage.input_tokens !== undefined && usage.output_tokens !== undefined);
        lines.push(`  Tokens: ${totalTokens.toLocaleString()} ${complete ? "total" : "observed"} (${inputTokens} in / ${outputTokens} out)`);
      }
      if (usage.cache_read_input_tokens !== undefined) lines.push(`  Cache Read Tokens: ${usage.cache_read_input_tokens.toLocaleString()}`);
      if (usage.cache_creation_input_tokens !== undefined) lines.push(`  Cache Creation Tokens: ${usage.cache_creation_input_tokens.toLocaleString()}`);
    }
    if (lastEntry?.total_cost_usd !== undefined) {
      lines.push(`  Cost: $${lastEntry.total_cost_usd.toFixed(4)}`);
    }
    if (lastEntry?.errors && Array.isArray(lastEntry.errors) && lastEntry.errors.length > 0) {
      lines.push("  Errors:");
      for (const error of lastEntry.errors) {
        lines.push(`    ${sessionOutputText(error)}`);
      }
    }
    if (Array.isArray(lastEntry?.permission_denials)) {
      lines.push(`  Permission Denials: ${lastEntry.permission_denials.length}`);
      for (const denial of lastEntry.permission_denials) lines.push(`    ${sessionOutputText(denial)}`);
    }
  }

  function generateSummaryLines(logEntries) {
    const renderEntries = normalizeEntriesForRendering(logEntries);
    const lines = [];
    const toolUsePairs = collectToolUsePairs(renderEntries);

    const state = {
      conversationLineCount: 0,
      maxConversationLines: 5000,
      conversationTruncated: false,
      traceEventCount: 0,
      standard: isCopilotEventLogEntries(logEntries),
    };

    for (const entry of renderEntries) {
      if (state.conversationLineCount >= state.maxConversationLines) {
        state.conversationTruncated = true;
        break;
      }

      if (entry.type === "assistant" && entry.message?.content) {
        for (const content of entry.message.content) {
          if (state.conversationLineCount >= state.maxConversationLines) {
            state.conversationTruncated = true;
            break;
          }

          if (content.type === "text" && content.text) {
            let text = content.text.trim();
            text = unfenceMarkdown(text);
            if (text && text.length > 0) {
              appendAgentText(lines, text, state);
            }
          } else if (content.type === "thinking" && content.thinking) {
            let text = content.thinking.trim();
            text = unfenceMarkdown(text);
            if (text && text.length > 0) {
              appendReasoningText(lines, text, state);
            }
          } else if (content.type === "tool_use") {
            appendToolExecutionLine(lines, content, toolUsePairs, state);
          }
        }
      }
    }

    if (state.conversationTruncated) {
      lines.push("... (conversation truncated)");
      lines.push("");
    }

    appendStatistics(lines, renderEntries, toolUsePairs, state.standard);

    return lines;
  }

  /**
   * Generates plain-text Copilot CLI style summary for logs.
   * @param {Array} logEntries - Array of log entries with type, message, etc.
   * @param {Object} options - Configuration options
   * @param {string} [options.model] - Model name to include in the header
   * @param {string} [options.parserName] - Name of the parser (e.g., "Copilot", "Claude")
   * @returns {string} Plain text summary for console output
   */
  function generatePlainTextSummary(logEntries, options = {}) {
    if (isUnifiedSessionTrace(logEntries)) return unifiedSummary(logEntries, false);
    const { parserName = "Agent" } = options;
    const model = options.model ?? observedSessionModel(logEntries);
    const lines = [];

    lines.push(`=== ${parserName} Execution Summary ===`);
    if (model) {
      lines.push(`Model: ${model}`);
    }
    if (isCopilotEventLogEntries(logEntries)) lines.push(...renderInitializationLines(normalizeEntriesForRendering(logEntries).find(entry => entry.type === "system" && entry.subtype === "init")));
    lines.push("");

    lines.push("Conversation:");
    lines.push("");

    lines.push(...generateSummaryLines(logEntries));

    return boundSummaryLines(lines, MAX_STEP_SUMMARY_SIZE, 4 * MAX_AGENT_TEXT_LENGTH + 128).join("\n");
  }

  /**
   * Generates a markdown-formatted Copilot CLI style summary for step summaries.
   * @param {Array} logEntries - Array of log entries with type, message, etc.
   * @param {Object} options - Configuration options
   * @param {string} [options.model] - Model name to include in the header
   * @param {string} [options.parserName] - Name of the parser (e.g., "Copilot", "Claude")
   * @param {number} [options.maxBytes] - Remaining publication budget for a unified trace
   * @returns {string} Markdown-formatted summary for step summary rendering
   */
  function generateCopilotCliStyleSummary(logEntries, options = {}) {
    if (isUnifiedSessionTrace(logEntries)) return unifiedSummary(logEntries, true, options.maxBytes);
    const lines = [];
    const standard = isCopilotEventLogEntries(logEntries);
    const model = options.model ?? observedSessionModel(logEntries);
    const initialization = isCopilotEventLogEntries(logEntries) ? renderInitializationLines(normalizeEntriesForRendering(logEntries).find(entry => entry.type === "system" && entry.subtype === "init")) : [];
    const fullBody = [...(model ? [`Model: ${model}`, ""] : []), ...initialization, "Conversation:", "", ...generateSummaryLines(logEntries)];
    const maxLineBytes = 4 * MAX_AGENT_TEXT_LENGTH + 128;
    let preamble = "";
    if (standard) {
      const entries = normalizeEntriesForRendering(logEntries);
      const statistics = [];
      appendStatistics(statistics, entries, collectToolUsePairs(entries), true);
      const visibleStats = boundSummaryLines(statistics, 16 * 1024, maxLineBytes);
      const statsFence = buildSafeOuterCodeFence(visibleStats);
      preamble = `### Agent session\n\n${statsFence}\n${visibleStats.join("\n")}\n${statsFence}\n\n<details><summary>Trace details</summary>\n\n`;
    }
    const tail = standard ? "\n\n</details>" : "";
    const budget = MAX_STEP_SUMMARY_SIZE - Buffer.byteLength(preamble + tail, "utf8") - 2 * (maxLineBytes + 1) - 3;
    const bodyLines = boundSummaryLines(fullBody, budget, maxLineBytes);
    const fence = buildSafeOuterCodeFence(bodyLines);

    lines.push(fence);
    lines.push(...bodyLines);
    lines.push(fence);

    return preamble + lines.join("\n") + tail;
  }

  /** @param {Array<any>} events @param {boolean} markdown @param {number} [maxBytes] @returns {string} */
  function unifiedSummary(events, markdown, maxBytes = MAX_STEP_SUMMARY_SIZE) {
    return renderUnifiedSession(events, {
      markdown,
      maxBytes: Math.min(maxBytes, MAX_STEP_SUMMARY_SIZE),
      maxLineBytes: 4 * MAX_AGENT_TEXT_LENGTH + 128,
      agentStatistics: entries => {
        const projected = normalizeEntriesForRendering(entries);
        const model = observedSessionModel(entries);
        const lines = [...(model ? [`Model: ${model}`] : []), ...renderInitializationLines(projected.find(entry => entry.type === "system" && entry.subtype === "init"))];
        appendStatistics(lines, projected, collectToolUsePairs(projected), true);
        return lines;
      },
    });
  }

  return {
    generateConversationMarkdown,
    formatToolUse,
    generatePlainTextSummary,
    generateCopilotCliStyleSummary,
  };
}

module.exports = createLogParserFormatters;
