// @ts-check
/// <reference types="@actions/github-script" />

const { generatePlainTextSummary, generateCopilotCliStyleSummary, wrapAgentLogInSection, formatSafeOutputsPreview } = require("./log_parser_shared.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");
const { redactStepSummaryContent } = require("./redact_secrets.cjs");
const { collectAddMaskedValues, applyAddMaskRedaction } = require("./add_mask_redaction.cjs");
const { projectSessionResult, isTokenCount, observedSessionModel } = require("./agent_session.cjs");
const { redactSessionForPublication } = require("./agent_session_render.cjs");
const INFERENCE_ACCESS_ERROR_PATTERN = /Access denied by policy settings|invalid access to inference/i;
const CLAUDE_RATE_LIMIT_PATTERN = /rate_limit_error|429 Too Many Requests|"api_error_status"\s*:\s*429|request rejected \(429\)|rate limit/i;
const CLAUDE_OVERLOAD_PATTERN = /overloaded_error|"overloaded"/i;
/** Matches HTTP protocol/status lines such as "HTTP/1.1 503". */
const CLAUDE_HTTP_5XX_PROTOCOL_PATTERN = /HTTP(?:\/\d\.\d)?\s+5\d{2}\b/;
/** Matches structured status fields such as "status: 502" or "status code=500". */
const CLAUDE_HTTP_5XX_STATUS_FIELD_PATTERN = /status(?:\s+code)?\s*[:=]\s*5\d{2}\b/;
/** Matches request/fetch/http error prose that carries an explicit 5xx code. */
const CLAUDE_HTTP_5XX_REQUEST_ERROR_PATTERN = /(?:http|fetch|request)\s+(?:failed|error)[^\n]*?\b5\d{2}\b/;
const CLAUDE_HTTP_5XX_STATUS_PATTERN = new RegExp([CLAUDE_HTTP_5XX_PROTOCOL_PATTERN.source, CLAUDE_HTTP_5XX_STATUS_FIELD_PATTERN.source, CLAUDE_HTTP_5XX_REQUEST_ERROR_PATTERN.source].join("|"), "i");
const STARTUP_DIAGNOSTIC_LINE_PATTERN = /(?:ERR_|Error:|CAPIError|Authentication failed|rate[_ -]?limit|429|\b5\d{2}\b|overloaded|inference)/i;
const MAX_DIAGNOSTIC_TAIL_LINES = 8;
const AGENT_STDIO_LOG_PATH = "/tmp/gh-aw/agent-stdio.log";

/**
 * Build startup diagnostics for Claude failures with no structured entries.
 * @param {string} rawContent
 * @returns {{
 *   exitCode: string,
 *   inferenceAccessError: boolean,
 *   aiCreditsRateLimitError: boolean,
 *   transientInferenceAvailabilityError: boolean,
 *   summaryLine: string,
 *   summaryMarkdown: string
 * }}
 */
function buildClaudeStartupDiagnostics(rawContent) {
  const content = typeof rawContent === "string" ? rawContent : "";
  const lines = content
    .split(/\r?\n/)
    .map(line => line.trim())
    .filter(Boolean);
  const startupLines = lines.filter(line => line.includes("[claude-harness]") || STARTUP_DIAGNOSTIC_LINE_PATTERN.test(line));
  // Intentionally avoid falling back to arbitrary raw log lines here. For empty-structured-log
  // failures, the unmatched tail can contain unrelated stdout/stderr, prompts, or secrets; when
  // no startup/diagnostic lines are recognized, we emit no tail section rather than risk leaking it.
  const tailLines = startupLines.slice(-MAX_DIAGNOSTIC_TAIL_LINES);
  const tailText = tailLines.join("\n");
  const safeTailText = escapeHtml(tailText);

  let exitCode = "unknown";
  const donePrefix = "done: exitCode=";
  const doneIdx = content.lastIndexOf(donePrefix);
  if (doneIdx !== -1) {
    const doneTail = content.slice(doneIdx + donePrefix.length);
    const doneCode = doneTail.match(/^(\d+)/);
    if (doneCode) {
      exitCode = doneCode[1];
    }
  } else {
    const failedPrefix = "failed: exitCode=";
    const failedIdx = content.lastIndexOf(failedPrefix);
    if (failedIdx !== -1) {
      const failedTail = content.slice(failedIdx + failedPrefix.length);
      const failedCode = failedTail.match(/^(\d+)/);
      if (failedCode) {
        exitCode = failedCode[1];
      }
    }
  }
  if (exitCode === "unknown") {
    const awfExitCode = content.match(/Process exiting with code:\s*(\d+)\b/i);
    if (awfExitCode) {
      exitCode = awfExitCode[1];
    }
  }

  const inferenceAccessError = INFERENCE_ACCESS_ERROR_PATTERN.test(content);
  const aiCreditsRateLimitError = CLAUDE_RATE_LIMIT_PATTERN.test(content) || CLAUDE_OVERLOAD_PATTERN.test(content);
  const transientInferenceAvailabilityError = aiCreditsRateLimitError || CLAUDE_HTTP_5XX_STATUS_PATTERN.test(content);
  const summaryLine = `Claude startup failed before structured logging (exitCode=${exitCode}).`;
  const summaryMarkdown = safeTailText ? `<details><summary>Claude startup diagnostics</summary>\n\n<pre><code>${safeTailText}</code></pre>\n</details>` : "";

  return {
    exitCode,
    inferenceAccessError,
    aiCreditsRateLimitError,
    transientInferenceAvailabilityError,
    summaryLine,
    summaryMarkdown,
  };
}

/**
 * @param {string} text
 * @returns {string}
 */
function escapeHtml(text) {
  const htmlEntities = { '"': "&quot;", "'": "&#39;", "&": "&amp;", "<": "&lt;", ">": "&gt;" };
  return text.replace(/["'&<>]/g, char => htmlEntities[char]);
}

/**
 * Bootstrap helper for log parser entry points.
 * Handles common logic for environment variable lookup, file existence checks,
 * content reading (file or directory), and summary emission.
 *
 * @param {Object} options - Configuration options
 * @param {(content: string) => string|{markdown: string, mcpFailures?: string[], maxTurnsHit?: boolean, logEntries?: Array<any>}} options.parseLog - Parser function that takes log content and returns markdown or result object
 * @param {string} options.parserName - Name of the parser (e.g., "Codex", "Claude", "Copilot")
 * @param {boolean} [options.supportsDirectories=false] - Whether the parser supports reading from directories
 * @returns {Promise<void>}
 */
async function runLogParser(options) {
  const fs = require("fs");
  const path = require("path");
  const { parseLog, parserName, supportsDirectories = false } = options;

  /**
   * Recursively searches a directory tree for the first events.jsonl file.
   * This file is written by the Copilot CLI and contains structured session events.
   * @param {string} dirPath - Directory to search
   * @returns {string|null} Absolute path to events.jsonl, or null if not found
   */
  function findEventsJsonlRecursive(dirPath) {
    try {
      const entries = fs.readdirSync(dirPath, { withFileTypes: true });
      for (const entry of entries) {
        const fullPath = path.join(dirPath, entry.name);
        if (entry.isDirectory()) {
          const found = findEventsJsonlRecursive(fullPath);
          if (found) return found;
        } else if (entry.name === "events.jsonl") {
          return fullPath;
        }
      }
    } catch (e) {
      // Ignore read errors (e.g. permission denied on subdirectories)
    }
    return null;
  }

  /**
   * Count valid JSONL entries from a safe outputs file.
   * @param {string} content - Raw safe outputs JSONL content
   * @returns {number} Number of valid entries
   */
  function countSafeOutputEntries(content) {
    if (!content || content.trim().length === 0) {
      return 0;
    }

    let count = 0;
    const lines = content.trim().split(/\r?\n/);
    for (const line of lines) {
      const trimmedLine = line.trim();
      if (!trimmedLine) {
        continue;
      }
      try {
        JSON.parse(trimmedLine);
        count++;
      } catch (e) {
        // Ignore invalid JSONL lines
      }
    }
    return count;
  }

  /**
   * Returns true if the log entries show the agent ran at least one turn.
   *
   * "At least one turn" is used (rather than "all work finished") because the
   * log only records the turn count, not whether every intended task succeeded.
   * The check is sufficient to distinguish a post-completion MCP relaunch
   * failure (the agent was already executing) from a startup failure where the
   * MCP never launched and the agent ran zero turns.
   *
   * Handles both log formats:
   *   - Legacy format (Codex, Copilot, etc.): { type: "result", num_turns: N }
   *   - Copilot event format (Claude): { type: "session.result", data: { numTurns: N } }
   *
   * @param {Array|null|undefined} entries
   * @returns {boolean}
   */
  function agentRanToCompletion(entries) {
    if (!entries || !Array.isArray(entries) || entries.length === 0) {
      return false;
    }
    return entries.some(e => {
      if (!e || typeof e !== "object") return false;
      // Legacy format
      if (e.type === "result" && typeof e.num_turns === "number" && e.num_turns > 0) return true;
      // Copilot event format (Claude)
      if (e.type === "session.result" && e.data && typeof e.data.numTurns === "number" && e.data.numTurns > 0) return true;
      return false;
    });
  }

  try {
    const logPath = process.env.GH_AW_AGENT_OUTPUT;
    if (!logPath) {
      core.info("No agent log file specified");
      return;
    }

    if (!fs.existsSync(logPath)) {
      core.info(`Log path not found: ${logPath}`);
      return;
    }

    let content = "";

    // Check if logPath is a directory or a file
    const stat = fs.statSync(logPath);
    if (stat.isDirectory()) {
      if (!supportsDirectories) {
        core.info(`Log path is a directory but ${parserName} parser does not support directories: ${logPath}`);
        return;
      }

      // Prefer events.jsonl (structured Copilot session format) over debug .log files
      const eventsJsonlPath = findEventsJsonlRecursive(logPath);
      if (eventsJsonlPath) {
        core.info(`Using Copilot session events from: ${eventsJsonlPath}`);
        content = fs.readFileSync(eventsJsonlPath, "utf8");
      } else {
        // Read all log files from the directory and concatenate them
        const files = fs.readdirSync(logPath);
        const logFiles = files.filter(file => file.endsWith(".log") || file.endsWith(".txt"));

        if (logFiles.length === 0) {
          core.info(`No log files found in directory: ${logPath}`);
          return;
        }

        // Sort log files by name to ensure consistent ordering
        logFiles.sort();

        // Concatenate all log files
        for (const file of logFiles) {
          const filePath = path.join(logPath, file);
          const fileContent = fs.readFileSync(filePath, "utf8");

          // Add a newline before this file if the previous content doesn't end with one
          if (content.length > 0 && !content.endsWith("\n")) {
            content += "\n";
          }

          content += fileContent;
        }
      }
    } else {
      // Read the single log file
      content = fs.readFileSync(logPath, "utf8");
    }

    const result = parseLog(content);
    const publicationMasks = new Set(collectAddMaskedValues(content));
    const redactPublication = text => applyAddMaskRedaction(redactStepSummaryContent(text), [...publicationMasks]);

    // Handle result that may be a simple string or an object with metadata
    let markdown = "";
    let mcpFailures = [];
    let maxTurnsHit = false;
    /** @type {any} */
    let logEntries = null;

    if (typeof result === "string") {
      markdown = result;
    } else if (result && typeof result === "object") {
      markdown = result.markdown || "";
      mcpFailures = result.mcpFailures || [];
      maxTurnsHit = result.maxTurnsHit || false;
      logEntries = result.logEntries || null;
    }

    // Enrich agent-stdio.log with a normalized result entry when the engine does not
    // write one directly (e.g. Copilot, Pi).  The OTEL conclusion span
    // (send_otlp_span.cjs → readAgentRuntimeMetrics) reads agent-stdio.log for the
    // gh-aw.turns attribute and token usage; without this entry those fields are zero
    // for every engine except Claude Code, leaving 80 % of fleet runs un-triageable.
    //
    // Safety rules:
    //  1. Only append when agent-stdio.log does NOT already contain a result entry
    //     (avoids double-counting on Claude Code runs where the entry is written by
    //     the --debug-file flag).
    //  2. The appended line must be a standalone JSON object on its own line so that
    //     the existing line-oriented parser in readAgentRuntimeMetrics can find it.
    //  3. All errors are non-fatal – telemetry enrichment must never break workflows.
    if (logEntries && Array.isArray(logEntries)) {
      const resultEntry = projectSessionResult(logEntries);
      if (resultEntry && (isTokenCount(resultEntry.num_turns) || isTokenCount(resultEntry.usage?.input_tokens) || isTokenCount(resultEntry.usage?.output_tokens))) {
        const normalizedResultEntry = {
          type: "result",
          num_turns: resultEntry.num_turns,
          usage:
            isTokenCount(resultEntry.usage?.input_tokens) || isTokenCount(resultEntry.usage?.output_tokens)
              ? {
                  input_tokens: resultEntry.usage?.input_tokens,
                  output_tokens: resultEntry.usage?.output_tokens,
                }
              : undefined,
        };
        const stdioLogPath = AGENT_STDIO_LOG_PATH;
        try {
          let alreadyHasResult = false;
          let newline = "";
          const isUsableResult = entry => entry?.type === "result" && (isTokenCount(entry.num_turns) || isTokenCount(entry.usage?.input_tokens) || isTokenCount(entry.usage?.output_tokens));
          if (fs.existsSync(stdioLogPath)) {
            const stdioContent = fs.readFileSync(stdioLogPath, "utf8");
            if (stdioContent && !stdioContent.endsWith("\n")) newline = "\n";
            alreadyHasResult = stdioContent.split("\n").some(line => {
              const objectStart = line.indexOf("{");
              const arrayStart = line.indexOf("[");
              let start = -1;
              if (objectStart >= 0 && arrayStart >= 0) {
                start = Math.min(objectStart, arrayStart);
              } else if (objectStart >= 0) {
                start = objectStart;
              } else {
                start = arrayStart;
              }
              if (start < 0) return false;
              try {
                const parsed = JSON.parse(line.slice(start));
                if (Array.isArray(parsed)) {
                  return parsed.some(isUsableResult);
                }
                return isUsableResult(parsed);
              } catch {
                return false;
              }
            });
          }
          if (!alreadyHasResult) {
            fs.mkdirSync(path.dirname(stdioLogPath), { recursive: true });
            fs.appendFileSync(stdioLogPath, newline + JSON.stringify(normalizedResultEntry) + "\n");
            core.info(`[log-parser] Wrote ${parserName} result entry to agent-stdio.log: num_turns=${normalizedResultEntry.num_turns ?? "n/a"}`);
          }
        } catch (err) {
          core.warning(`[log-parser] Failed to enrich agent-stdio.log with result entry: ${getErrorMessage(err)}`);
        }
      }
    }

    // Redact add-mask values from agent-stdio.log before it is uploaded as an
    // artifact so plaintext secrets do not persist outside live job logs.
    const stdioLogPath = AGENT_STDIO_LOG_PATH;
    try {
      if (fs.existsSync(stdioLogPath)) {
        const stdioContent = fs.readFileSync(stdioLogPath, "utf8");
        const maskedValues = collectAddMaskedValues(stdioContent);
        for (const value of maskedValues) publicationMasks.add(value);
        if (maskedValues.length > 0) {
          const redactedContent = applyAddMaskRedaction(stdioContent, maskedValues);
          if (redactedContent !== stdioContent) {
            fs.writeFileSync(stdioLogPath, redactedContent, "utf8");
            core.info(`[log-parser] Sanitized agent-stdio.log before artifact upload using ${maskedValues.length} collected add-mask value(s)`);
          }
        }
      }
    } catch (err) {
      core.warning(`[log-parser] Failed to redact add-mask values in agent-stdio.log: ${getErrorMessage(err)}`);
    }

    // Read safe outputs file if available
    let safeOutputsContent = "";
    let safeOutputEntriesCount = 0;
    const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
    if (safeOutputsPath && fs.existsSync(safeOutputsPath)) {
      try {
        safeOutputsContent = fs.readFileSync(safeOutputsPath, "utf8");
        safeOutputEntriesCount = countSafeOutputEntries(safeOutputsContent);
      } catch (error) {
        core.warning(`Failed to read safe outputs file: ${getErrorMessage(error)}`);
      }
    }

    if (markdown) {
      // Generate lightweight plain text summary for core.info and Copilot CLI style for step summary
      if (logEntries && Array.isArray(logEntries) && logEntries.length > 0) {
        const publicationEntries = redactSessionForPublication(logEntries, redactPublication);
        const model = observedSessionModel(logEntries);

        const plainTextSummary = generatePlainTextSummary(publicationEntries, {
          model: model === undefined ? undefined : redactPublication(model),
          parserName,
        });
        core.info(redactPublication(plainTextSummary));

        // Add safe outputs preview to core.info
        if (safeOutputsContent) {
          const safeOutputsPlainText = formatSafeOutputsPreview(safeOutputsContent, { isPlainText: true });
          if (safeOutputsPlainText) {
            core.info(safeOutputsPlainText);
          }
        }

        // Generate Copilot CLI style markdown for step summary
        const copilotCliStyleMarkdown = generateCopilotCliStyleSummary(publicationEntries, {
          model: model === undefined ? undefined : redactPublication(model),
          parserName,
        });

        // Wrap the agent log in a details/summary section (open by default)
        const wrappedAgentLog = wrapAgentLogInSection(copilotCliStyleMarkdown, {
          parserName,
          open: true,
        });

        // Add safe outputs preview to step summary
        let fullMarkdown = wrappedAgentLog;
        if (safeOutputsContent) {
          const safeOutputsMarkdown = formatSafeOutputsPreview(safeOutputsContent, { isPlainText: false });
          if (safeOutputsMarkdown) {
            fullMarkdown += "\n" + safeOutputsMarkdown;
          }
        }

        await core.summary.addRaw(redactPublication(fullMarkdown)).write();
      } else {
        // Fallback path: markdown exists but no structured log entries were parsed.
        // Suppress the "parsed successfully" message for Claude since it always produces
        // logEntries when healthy — absence of entries means the parse fell back and is
        // about to emit a guardrail warning/failure below.
        if (parserName !== "Claude") {
          core.info(`${parserName} log parsed successfully`);
        }

        // Add safe outputs preview to core.info (fallback path)
        if (safeOutputsContent) {
          const safeOutputsPlainText = formatSafeOutputsPreview(safeOutputsContent, { isPlainText: true });
          if (safeOutputsPlainText) {
            core.info(safeOutputsPlainText);
          }
        }

        // Wrap the original markdown in a details/summary section (open by default)
        const wrappedAgentLog = wrapAgentLogInSection(markdown, {
          parserName,
          open: true,
        });

        // Write wrapped markdown to step summary if available
        let fullMarkdown = wrappedAgentLog;
        if (safeOutputsContent) {
          const safeOutputsMarkdown = formatSafeOutputsPreview(safeOutputsContent, { isPlainText: false });
          if (safeOutputsMarkdown) {
            fullMarkdown += "\n" + safeOutputsMarkdown;
          }
        }
        await core.summary.addRaw(redactPublication(fullMarkdown)).write();
      }
    } else {
      core.error(`Failed to parse ${parserName} log`);
    }

    // Claude-specific guardrail: if no structured log entries were parsed, treat as execution failure.
    // This catches silent startup failures where Claude exits before producing JSON tool activity.
    // Exception: when safeOutputEntriesCount > 0 the agent demonstrably completed and emitted
    // safe outputs — treat as a non-fatal post-completion infrastructure failure (e.g. sandbox
    // teardown race leaving agent-stdio.log unreadable) and downgrade to a warning.
    if (parserName === "Claude" && (!logEntries || logEntries.length === 0)) {
      if (safeOutputEntriesCount > 0) {
        core.warning(
          `Claude produced no structured log entries, but agent completed with ${safeOutputEntriesCount} safe output ${safeOutputEntriesCount === 1 ? "entry" : "entries"} — treating as non-fatal post-completion infrastructure failure`
        );
      } else {
        const diagnostics = buildClaudeStartupDiagnostics(content);
        if (diagnostics.summaryMarkdown) {
          await core.summary.addRaw(redactStepSummaryContent(diagnostics.summaryMarkdown)).write();
        }

        if (diagnostics.inferenceAccessError) {
          core.setOutput("inference_access_error", "true");
        }
        if (diagnostics.aiCreditsRateLimitError) {
          core.setOutput("ai_credits_rate_limit_error", "true");
        }

        const errorCode = diagnostics.inferenceAccessError || diagnostics.transientInferenceAvailabilityError ? ERR_API : ERR_CONFIG;
        const failureKind = diagnostics.inferenceAccessError
          ? "inference access denied by policy"
          : diagnostics.transientInferenceAvailabilityError
            ? "transient inference availability signal detected"
            : "startup/configuration failure detected";

        core.setFailed(`${errorCode}: Claude execution failed: no structured log entries were produced. ${diagnostics.summaryLine} ${failureKind}.`);
        return;
      }
    }

    // Handle MCP server failures if present
    if (mcpFailures && mcpFailures.length > 0) {
      const failedServers = mcpFailures.join(", ");
      if (safeOutputEntriesCount > 0) {
        core.warning(`MCP server(s) failed to launch (${failedServers}), but agent completed with ${safeOutputEntriesCount} safe output ${safeOutputEntriesCount === 1 ? "entry" : "entries"}`);
      } else if (agentRanToCompletion(logEntries)) {
        // The agent ran turns to completion even though an MCP server failed to launch.
        // This is a post-completion relaunch/health-probe failure — the MCP server was
        // healthy during execution (the agent used it throughout the run) and the failure
        // occurred after the work was done.  Treat as non-fatal so genuine task success
        // is not masked by a transient infrastructure event.
        core.warning(`MCP server(s) failed to launch (${failedServers}), but agent completed turns — treating as non-fatal post-completion relaunch`);
      } else {
        core.setFailed(`${ERR_API}: MCP server(s) failed to launch: ${failedServers}`);
        return;
      }
    }

    // Handle max-turns limit if hit
    if (maxTurnsHit) {
      core.setFailed(`${ERR_VALIDATION}: Agent execution stopped: max-turns limit reached. The agent did not complete its task successfully.`);
    }
  } catch (error) {
    core.setFailed(`${ERR_API}: ${getErrorMessage(error)}`);
  }
}

// Export for testing and usage
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    runLogParser,
  };
}
