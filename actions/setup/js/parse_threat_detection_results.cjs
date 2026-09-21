// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Parse Threat Detection Results
 *
 * This module parses the threat detection results from the detection log file
 * (written by the detection copilot via tee) and determines whether any
 * security threats were detected (prompt injection, secret leak, malicious
 * patch). It sets the appropriate output and fails the workflow if threats
 * are detected.
 *
 * The detection copilot writes its verdict to stdout which is piped through
 * `tee -a` to detection.log. This parser reads that file — NOT agent_output.json
 * (which is the main agent's structured output used as *input* to the detection
 * copilot).
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { listFilesRecursively } = require("./file_helpers.cjs");
const { DETECTION_LOG_FILENAME, DETECTION_RESULT_FILENAME } = require("./constants.cjs");
const { ERR_SYSTEM, ERR_PARSE, ERR_VALIDATION } = require("./error_codes.cjs");
const { parseAPIProxyGuardRejectionFromEventLog, formatAPIProxyGuardRejection } = require("./ai_credits_context.cjs");

const RESULT_PREFIX = "THREAT_DETECTION_RESULT:";

/**
 * Strip a single leading Markdown emphasis delimiter (bold `**`, italic `*`,
 * bold `__`, or italic `_`) from the start of a string, if present.
 *
 * Detection models sometimes wrap the verdict line in Markdown emphasis, e.g.:
 *   **THREAT_DETECTION_RESULT:{"prompt_injection":false,...}**
 * This helper allows callers to recognize the RESULT_PREFIX immediately after
 * such delimiters without treating arbitrary prose mentions of the prefix as
 * an authoritative verdict (matching remains anchored to the start of the line).
 *
 * @param {string} text - Trimmed line to inspect
 * @returns {string} The text with a single leading emphasis delimiter removed, if any
 */
function stripLeadingMarkdownEmphasis(text) {
  return text.replace(/^(\*\*|\*|__|_)/, "");
}

/**
 * Check whether a trimmed line begins with RESULT_PREFIX, optionally preceded
 * by a single Markdown emphasis delimiter, and if so return the line with
 * that delimiter stripped so downstream brace-counting extraction can operate
 * on it directly. Returns null if the line does not begin with the prefix
 * (with or without emphasis).
 *
 * @param {string} trimmedLine - A trimmed line to check
 * @returns {string|null} The line with leading emphasis stripped, or null if not a match
 */
function matchResultPrefixLine(trimmedLine) {
  if (trimmedLine.startsWith(RESULT_PREFIX)) return trimmedLine;
  const stripped = stripLeadingMarkdownEmphasis(trimmedLine);
  if (stripped !== trimmedLine && stripped.startsWith(RESULT_PREFIX)) return stripped;
  return null;
}

/**
 * Extract a complete JSON object from a string that starts with RESULT_PREFIX,
 * using character-by-character brace counting to find the matching closing brace.
 * Tracks string context so that braces inside JSON string values are not counted.
 * This avoids regex and correctly handles escape sequences (e.g. \", \\).
 *
 * The input may contain actual newline characters inside JSON string values when
 * the outer stream-json decoder has unescaped \n sequences. The extraction still
 * produces the complete JSON object; callers must normalize those newlines before
 * passing the JSON to JSON.parse.
 *
 * @param {string} text - String beginning with RESULT_PREFIX followed by a JSON object
 * @returns {string|null} RESULT_PREFIX + complete JSON object, or null if not found
 */
function extractResultFromText(text) {
  const jsonStartPos = text.indexOf("{", RESULT_PREFIX.length);
  if (jsonStartPos === -1) return null;

  let depth = 0;
  let inString = false;
  let escaped = false;
  let jsonEndPos = -1;
  for (let i = jsonStartPos; i < text.length; i++) {
    const ch = text[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (ch === "\\" && inString) {
      escaped = true;
      continue;
    }
    if (ch === '"') {
      inString = !inString;
      continue;
    }
    if (inString) {
      continue;
    }
    if (ch === "{") {
      depth++;
    } else if (ch === "}") {
      depth--;
      if (depth === 0) {
        jsonEndPos = i;
        break;
      }
    }
  }
  if (jsonEndPos === -1) return null;

  return RESULT_PREFIX + text.substring(jsonStartPos, jsonEndPos + 1);
}

/** Required fields that must be present for a valid structured threat detection object. */
const STRUCTURED_OUTPUT_REQUIRED_FIELDS = ["prompt_injection", "secret_leak", "malicious_patch"];

/**
 * Try to extract a threat detection verdict from Codex structured output.
 * When Codex runs with -c response_schema enabled for detection jobs, it outputs the
 * JSON verdict object directly — without the THREAT_DETECTION_RESULT: prefix that
 * non-structured (text-mode) engines use.  This function detects such output by
 * checking for the expected threat detection fields and wraps it with the prefix so
 * the existing parsing pipeline can handle it uniformly.
 *
 * @param {string} text - Text that may be a raw JSON threat detection object
 * @returns {string|null} RESULT_PREFIX + JSON string if the text is a valid
 *   threat detection object without the prefix, null otherwise
 */
function extractStructuredOutput(text) {
  const trimmed = text.trim();
  if (!trimmed.startsWith("{")) return null;
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    if (!STRUCTURED_OUTPUT_REQUIRED_FIELDS.every(field => field in parsed)) return null;
    return RESULT_PREFIX + trimmed;
  } catch {
    return null;
  }
}

/**
 * Try to extract a THREAT_DETECTION_RESULT value from a stream-json line.
 * Stream-json output from Claude wraps the result in JSON envelopes like:
 *   {"type":"result","result":"THREAT_DETECTION_RESULT:{\"prompt_injection\":...}"}
 *
 * The same result also appears in {"type":"assistant"} messages, but we only
 * extract from "type":"result" which is the authoritative final summary.
 *
 * When Codex runs with structured outputs (response_schema), the Responses API
 * emits the verdict as a plain JSON object in response events (no prefix).
 * extractStructuredOutput handles that case as a fallback.
 *
 * @param {string} line - A single line from the detection log
 * @returns {string|null} The raw THREAT_DETECTION_RESULT:... string if found, null otherwise
 */
function extractFromStreamJson(line) {
  const trimmed = line.trim();

  // Support log lines prefixed with runner/codex tracing metadata, e.g.:
  //   2026-... TRACE ...: {"type":"response.output_text.done",...}
  // Assumes the first JSON object on the line is the event payload.
  const jsonStart = trimmed.indexOf("{");
  if (jsonStart === -1) return null;
  const jsonText = trimmed.slice(jsonStart);

  /**
   * @param {string} text
   * @returns {string|null}
   */
  function extractPrefixedResult(text) {
    const prefixIdx = text.indexOf(RESULT_PREFIX);
    if (prefixIdx === -1) return null;
    return extractResultFromText(text.slice(prefixIdx));
  }

  /**
   * @param {unknown} content
   * @returns {string|null}
   */
  function extractFromAssistantContent(content) {
    if (typeof content === "string") {
      return extractPrefixedResult(content) || extractStructuredOutput(content);
    }
    if (!Array.isArray(content)) return null;
    for (const part of content) {
      const text = part && typeof part === "object" && typeof part.text === "string" ? part.text : null;
      if (!text) continue;
      const extracted = extractPrefixedResult(text) || extractStructuredOutput(text);
      if (extracted) return extracted;
    }
    return null;
  }

  try {
    const obj = JSON.parse(jsonText);
    // Only extract from the authoritative "result" summary, not "assistant" messages.
    // In stream-json mode, the same content appears in both; using only "result"
    // avoids double-counting.
    if (obj.type === "result" && typeof obj.result === "string") {
      // The result field contains the model's full response text, which may
      // include analysis before the THREAT_DETECTION_RESULT line.
      // Split by newlines to find the line that starts with the prefix.
      //
      // IMPORTANT: The outer JSON.parse unescapes \n sequences into actual newline
      // characters. If the model placed a literal newline inside a reasons string
      // value, the JSON object for the verdict gets split across multiple lines here.
      // To handle this robustly, we find the prefix line by index, rejoin all
      // subsequent lines, then use brace-counting to locate the complete JSON object.
      const resultLines = obj.result.split("\n");
      let prefixLineIdx = -1;
      /** @type {string | null} */
      let strippedFirstLine = null;
      for (let i = 0; i < resultLines.length; i++) {
        const matched = matchResultPrefixLine(resultLines[i].trim());
        if (matched !== null) {
          prefixLineIdx = i;
          strippedFirstLine = matched;
          break;
        }
      }
      if (prefixLineIdx === -1) return null;

      // Rejoin all lines from the prefix line onward so that any JSON string
      // values split by actual newlines are reassembled. Use the emphasis-
      // stripped version of the first line so brace-counting starts cleanly.
      const joined = [strippedFirstLine, ...resultLines.slice(prefixLineIdx + 1)].join("\n").trim();

      // Extract the complete JSON object using brace-counting.
      return extractResultFromText(joined);
    }

    // Codex responses API emits final output text in response events.
    // Try the prefixed format first; fall back to structured output (no prefix)
    // when Codex runs with -c response_schema in detection mode.
    if (obj.type === "response.output_text.done" && typeof obj.text === "string") {
      return extractPrefixedResult(obj.text) || extractStructuredOutput(obj.text);
    }
    if (obj.type === "response.content_part.done" && obj.part && typeof obj.part.text === "string") {
      return extractPrefixedResult(obj.part.text) || extractStructuredOutput(obj.part.text);
    }
    if (obj.type === "item.completed" && obj.item && typeof obj.item.text === "string") {
      return extractPrefixedResult(obj.item.text) || extractStructuredOutput(obj.item.text);
    }

    // Pi emits final assistant verdicts in turn_end/message_end envelopes.
    if ((obj.type === "turn_end" || obj.type === "message_end") && obj.message && obj.message.role === "assistant") {
      return extractFromAssistantContent(obj.message.content);
    }

    // Some streams emit final assistant state via message_update.
    if (obj.type === "message_update" && obj.message && obj.message.role === "assistant") {
      return extractFromAssistantContent(obj.message.content);
    }

    // Agent-level summaries may include all messages; parse assistant messages only.
    if (obj.type === "agent_end" && Array.isArray(obj.messages)) {
      for (const message of obj.messages) {
        if (!message || message.role !== "assistant") continue;
        const extracted = extractFromAssistantContent(message.content);
        if (extracted) return extracted;
      }
    }
  } catch {
    // Not valid JSON — ignored, this is not a stream-json line.
  }
  return null;
}

/**
 * Parse the detection log and extract the THREAT_DETECTION_RESULT.
 *
 * Supports two output formats:
 * 1. **Stream-json** (--output-format stream-json): The result is embedded inside
 *    a JSON envelope: {"type":"result","result":"THREAT_DETECTION_RESULT:{...}"}
 * 2. **Raw/text** (--output-format text or --print): The result appears as a
 *    bare line: THREAT_DETECTION_RESULT:{...}
 *
 * Strategy: extract from stream-json "type":"result" lines first (authoritative).
 * If none found, fall back to raw line matching. This avoids double-counting
 * since stream-json mode produces both "assistant" and "result" envelopes
 * containing the same string.
 *
 * @param {string} content - The raw detection log content
 * @returns {{ verdict?: { prompt_injection: boolean, secret_leak: boolean, malicious_patch: boolean, reasons: string[] }, error?: string }}
 */
function parseDetectionLog(content) {
  const lines = content.split("\n");

  // Phase 1: Try stream-json extraction (from "type":"result" lines only)
  const streamMatches = [];
  for (const line of lines) {
    const extracted = extractFromStreamJson(line);
    if (extracted) {
      streamMatches.push(extracted);
    }
  }

  // Phase 2: If no stream-json result field matches, try assistant stream chunk matching.
  // Gemini stream-json output may emit assistant text in multiple "type":"message"
  // entries (with role=assistant) where the verdict is split across chunks:
  //   "THREAT_DETECTION_"
  //   "RESULT:{...}"
  // Reassemble assistant chunks and parse the first complete verdict object.
  const assistantMatches = [];
  if (streamMatches.length === 0) {
    const assistantChunks = [];
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed.startsWith("{")) continue;
      try {
        const obj = JSON.parse(trimmed);
        if (obj.type === "message" && obj.role === "assistant" && typeof obj.content === "string") {
          assistantChunks.push(obj.content);
        }
      } catch {
        // Not valid JSON — ignore
      }
    }

    if (assistantChunks.length > 0) {
      const combinedAssistantText = assistantChunks.join("");
      const prefixIdx = combinedAssistantText.indexOf(RESULT_PREFIX);
      if (prefixIdx !== -1) {
        const extracted = extractResultFromText(combinedAssistantText.slice(prefixIdx));
        if (extracted !== null) {
          assistantMatches.push(extracted);
        }
      }
    }
  }

  // Phase 3: If no stream-json or assistant chunk results, try raw line matching.
  // Apply the same join-and-brace-count approach to handle cases where the
  // reasons values contain actual newlines that split the JSON across lines.
  const rawMatches = [];
  if (streamMatches.length === 0 && assistantMatches.length === 0) {
    let i = 0;
    while (i < lines.length) {
      const matchedFirstLine = matchResultPrefixLine(lines[i].trim());
      if (matchedFirstLine !== null) {
        const joined = [matchedFirstLine, ...lines.slice(i + 1)].join("\n").trim();
        const extracted = extractResultFromText(joined);
        if (extracted !== null) {
          // Successfully extracted a complete JSON object; advance past consumed lines.
          rawMatches.push(extracted);
          // Count how many lines were consumed by this match so the loop
          // skips past them and does not re-match continuation lines.
          const jsonPart = extracted.substring(RESULT_PREFIX.length);
          const extraLines = jsonPart.split("\n").length - 1;
          i += extraLines + 1;
        } else {
          // No complete {…} object found (e.g. null, [], string, truncated JSON);
          // fall back to the trimmed line so the parsing step reports a useful error.
          rawMatches.push(lines[i].trim());
          i++;
        }
      } else {
        i++;
      }
    }
  }

  const matches = streamMatches.length > 0 ? streamMatches : assistantMatches.length > 0 ? assistantMatches : rawMatches;

  if (matches.length === 0) {
    return { error: "No THREAT_DETECTION_RESULT found in detection log. The detection model may have failed to follow the output format." };
  }

  // Deduplicate identical results. The detection command writes to the same file
  // via both --debug-file and tee, so the same line often appears 2-3 times.
  // Only error if the entries actually disagree (different verdicts).
  const uniqueMatches = [...new Set(matches)];

  if (uniqueMatches.length > 1) {
    return {
      error: `Multiple conflicting THREAT_DETECTION_RESULT entries found (${uniqueMatches.length} unique out of ${matches.length} total) in detection log. Expected one consistent verdict. Entries: ${uniqueMatches.map((m, i) => `\n  [${i + 1}] ${m}`).join("")}`,
    };
  }

  const jsonPart = uniqueMatches[0].substring(RESULT_PREFIX.length);
  try {
    // Normalize literal newline characters to JSON escape sequences before parsing.
    // When the outer stream-json decoder unescapes \n sequences, actual newline
    // characters may end up inside JSON string values (e.g. in reasons entries).
    // Replacing them with the two-character sequence \n restores valid JSON so
    // that JSON.parse can handle them correctly.
    const normalizedJson = jsonPart.split("\n").join("\\n");
    const parsed = JSON.parse(normalizedJson);

    // The result must be a plain object, not null, an array, or a primitive.
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { error: `THREAT_DETECTION_RESULT JSON must be an object, got ${parsed === null ? "null" : Array.isArray(parsed) ? "array" : typeof parsed}. Raw value: ${matches[0]}` };
    }

    // Validate that threat flags are actual booleans.
    // Boolean("false") === true, so accepting non-boolean types would cause
    // false positives (string "false" treated as a detection).
    for (const field of ["prompt_injection", "secret_leak", "malicious_patch"]) {
      if (typeof parsed[field] !== "boolean") {
        return { error: `Invalid type for "${field}": expected boolean, got ${typeof parsed[field]} (${JSON.stringify(parsed[field])}). Raw value: ${matches[0]}` };
      }
    }

    const verdict = {
      prompt_injection: parsed.prompt_injection,
      secret_leak: parsed.secret_leak,
      malicious_patch: parsed.malicious_patch,
      reasons: Array.isArray(parsed.reasons) ? parsed.reasons : [],
    };
    return { verdict };
  } catch (/** @type {any} */ parseError) {
    return { error: `Failed to parse JSON from THREAT_DETECTION_RESULT: ${getErrorMessage(parseError)}\nRaw value: ${matches[0]}` };
  }
}

/**
 * Try to parse the threat detection verdict from a Codex structured output file.
 *
 * When Codex runs with `--output-last-message <file>`, it writes the final model
 * response directly to that file as plain text. Combined with `--output-schema`,
 * the content is the JSON verdict object (no THREAT_DETECTION_RESULT: prefix, no
 * log noise).  This function reads and validates the file, returning the verdict
 * or an error object exactly like `parseDetectionLog`.
 *
 * @param {string} resultFilePath - Absolute path to the structured result file
 * @returns {{ verdict?: { prompt_injection: boolean, secret_leak: boolean, malicious_patch: boolean, reasons: string[] }, error?: string } | null}
 *   Returns null if the file does not exist (caller should fall back to log parsing).
 *   Returns an error object if the file exists but is malformed (conservative: report parse_error).
 */
function parseStructuredResultFile(resultFilePath) {
  if (!fs.existsSync(resultFilePath)) {
    return null;
  }

  let content;
  try {
    content = fs.readFileSync(resultFilePath, "utf8");
  } catch (/** @type {any} */ readError) {
    return { error: `Failed to read structured result file: ${getErrorMessage(readError)}` };
  }

  const trimmed = content.trim();
  if (!trimmed) {
    return { error: "Structured result file is empty" };
  }

  try {
    const parsed = JSON.parse(trimmed);

    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { error: `Structured result must be a JSON object, got ${parsed === null ? "null" : Array.isArray(parsed) ? "array" : typeof parsed}` };
    }

    for (const field of ["prompt_injection", "secret_leak", "malicious_patch"]) {
      if (typeof parsed[field] !== "boolean") {
        return { error: `Invalid type for "${field}" in structured result: expected boolean, got ${typeof parsed[field]} (${JSON.stringify(parsed[field])})` };
      }
    }

    return {
      verdict: {
        prompt_injection: parsed.prompt_injection,
        secret_leak: parsed.secret_leak,
        malicious_patch: parsed.malicious_patch,
        reasons: Array.isArray(parsed.reasons) ? parsed.reasons : [],
      },
    };
  } catch (/** @type {any} */ parseError) {
    return { error: `Failed to parse JSON from structured result file: ${getErrorMessage(parseError)}` };
  }
}

/**
 * Surface an AWF API proxy guardrail rejection (e.g. `max_cache_misses_exceeded`) in the
 * detection job log. The proxy answers such requests with HTTP 403 and engines commonly
 * report that as a generic authentication failure, so without this the detection job shows
 * no trace of the guard that actually stopped the run.
 *
 * @param {string} [eventLogPathOverride] - Explicit proxy log path (tests); defaults to the known proxy log locations
 * @returns {boolean} true when a guard rejection was found and reported
 */
function reportAPIProxyGuardRejection(eventLogPathOverride) {
  /** @type {{ guard: string, counters: Record<string, string|number> } | null} */
  let rejection = null;
  try {
    rejection = parseAPIProxyGuardRejectionFromEventLog(eventLogPathOverride);
  } catch {
    return false;
  }
  if (!rejection) return false;
  core.warning(`AWF API proxy guardrail rejected detection requests: ${formatAPIProxyGuardRejection(rejection)}. This is a proxy policy outcome (HTTP 403), not an authentication failure.`);
  return true;
}

/**
 * Main entry point for parsing threat detection results and concluding the detection job.
 *
 * This function consolidates three responsibilities previously split across two steps:
 *  1. Parse the detection log and extract the threat verdict
 *  2. Log all results with extensive diagnostic detail
 *  3. Set job outputs (success, conclusion) and fail the job if threats were detected
 *
 * The RUN_DETECTION environment variable controls whether detection was needed at all:
 *  - "true"  → parse the log and evaluate the verdict
 *  - anything else → mark as skipped (no agent output to analyze)
 *
 * @returns {Promise<void>}
 */
async function main() {
  const threatDetectionDir = "/tmp/gh-aw/threat-detection";
  const logPath = path.join(threatDetectionDir, DETECTION_LOG_FILENAME);
  const runDetection = process.env.RUN_DETECTION;
  const continueOnError = process.env.GH_AW_DETECTION_CONTINUE_ON_ERROR !== "false";
  const detectionExecutionOutcome = process.env.DETECTION_AGENTIC_EXECUTION_OUTCOME || "";
  const isWarnMode = continueOnError;

  /**
   * Helper to set detection failure/warning outputs based on continue-on-error mode.
   * In warn mode, engine failures with tooling reasons fail closed; all other
   * failures set conclusion=warning, success=false, and do not fail the job.
   * In error mode: sets conclusion=failure, success=false, fails the job.
   * @param {string} reason - Categorized reason (e.g. "threat_detected", "agent_failure", "parse_error")
   * @param {string} message - Human-readable error message
   */
  function setDetectionFailure(reason, message) {
    if (reason === "agent_failure" || reason === "parse_error") {
      reportAPIProxyGuardRejection();
    }
    core.setOutput("reason", reason);
    core.exportVariable("GH_AW_DETECTION_REASON", reason);
    const mustFail = detectionExecutionOutcome === "failure" && (reason === "agent_failure" || reason === "parse_error");
    if (isWarnMode && !mustFail) {
      core.warning(`⚠️ ${message}`);
      core.setOutput("conclusion", "warning");
      core.exportVariable("GH_AW_DETECTION_CONCLUSION", "warning");
      core.setOutput("success", "false");
    } else {
      core.setOutput("conclusion", "failure");
      core.exportVariable("GH_AW_DETECTION_CONCLUSION", "failure");
      core.setOutput("success", "false");
      core.setFailed(message);
    }
  }

  /**
   * Log and evaluate a parsed verdict, then set step outputs/failure state.
   * @param {{ prompt_injection: boolean, secret_leak: boolean, malicious_patch: boolean, reasons: string[] }} verdict
   * @param {string} sourceLabel
   */
  function evaluateAndReportVerdict(verdict, sourceLabel) {
    core.info(`📋 Threat detection verdict${sourceLabel ? ` (${sourceLabel})` : ""}:`);
    core.info(`   prompt_injection : ${verdict.prompt_injection}`);
    core.info(`   secret_leak      : ${verdict.secret_leak}`);
    core.info(`   malicious_patch  : ${verdict.malicious_patch}`);
    const hasReasons = verdict.reasons.length > 0;
    if (hasReasons) {
      core.info(`   reasons (${verdict.reasons.length}):`);
      verdict.reasons.forEach((reason, i) => core.info(`     [${i + 1}] ${reason}`));
    } else {
      core.info("   reasons          : (none)");
    }

    const threatsDetected = verdict.prompt_injection || verdict.secret_leak || verdict.malicious_patch;
    if (threatsDetected) {
      const threats = [];
      if (verdict.prompt_injection) threats.push("prompt injection");
      if (verdict.secret_leak) threats.push("secret leak");
      if (verdict.malicious_patch) threats.push("malicious patch");
      const reasonsText = hasReasons ? "\nReasons: " + verdict.reasons.join("; ") : "";
      core.error("🚨 Security threats detected: " + threats.join(", "));
      if (hasReasons) {
        core.error("   Reasons: " + verdict.reasons.join("; "));
      }
      setDetectionFailure("threat_detected", `${ERR_VALIDATION}: ❌ Security threats detected: ${threats.join(", ")}${reasonsText}`);
    } else {
      core.info("✅ No security threats detected. Safe outputs may proceed.");
      core.setOutput("conclusion", "success");
      core.exportVariable("GH_AW_DETECTION_CONCLUSION", "success");
      core.setOutput("success", "true");
      core.setOutput("reason", "");
      core.exportVariable("GH_AW_DETECTION_REASON", "");
    }

    core.info("════════════════════════════════════════════════════════");
    core.info("🛡️  Threat detection conclusion complete.");
    core.info("════════════════════════════════════════════════════════");
  }

  // Top-level try/catch ensures outputs are always set and the step never throws
  // unexpectedly. Any unanticipated runtime error (e.g. I/O error outside the guarded
  // paths) is caught here and surfaced as a parse_error warning (in warn mode) or
  // failure (in strict mode). This is a defence-in-depth measure complementing the
  // continue-on-error: true that is set on the parse step in warn mode, and the
  // try/catch wrapper in the generated github-script that handles module load failures.
  try {
    await runMain();
  } catch (/** @type {any} */ unexpectedError) {
    const errorMsg = getErrorMessage(unexpectedError);
    core.error(`❌ Unexpected error in threat detection parse: ${errorMsg}`);
    setDetectionFailure("parse_error", `${ERR_SYSTEM}: ❌ Unexpected error in threat detection parse: ${errorMsg}`);
  }

  /**
   * Inner implementation of main — wrapped by the outer try/catch for safety.
   * @returns {Promise<void>}
   */
  async function runMain() {
    core.info("════════════════════════════════════════════════════════");
    core.info("🛡️  Threat Detection: Parse Results & Conclude");
    core.info("════════════════════════════════════════════════════════");
    core.info(`📋 RUN_DETECTION env: ${JSON.stringify(runDetection)}`);
    core.info(`📋 continue-on-error: ${continueOnError}`);
    core.info(`📋 detection execution outcome: ${JSON.stringify(detectionExecutionOutcome)}`);
    core.info(`📁 Threat detection directory: ${threatDetectionDir}`);
    const resultFilePath = path.join(threatDetectionDir, DETECTION_RESULT_FILENAME);
    core.info(`📄 Detection log path: ${logPath}`);
    core.info(`📄 Structured result path: ${resultFilePath}`);

    // ── Step 1: Check whether detection was needed ──────────────────────────
    if (runDetection !== "true") {
      core.info("⏭️  Detection guard output indicates detection was not needed.");
      core.info("   Reason: no agent output types or patch files were produced.");
      core.info("   Setting conclusion=skipped, success=true.");
      core.setOutput("conclusion", "skipped");
      core.exportVariable("GH_AW_DETECTION_CONCLUSION", "skipped");
      core.setOutput("success", "true");
      core.setOutput("reason", "");
      core.exportVariable("GH_AW_DETECTION_REASON", "");
      core.info("✅ Detection skipped — no threats to evaluate.");
      return;
    }

    core.info("🔍 Detection is required. Proceeding to parse detection result...");

    // ── Step 2: Try the Codex structured result file first ───────────────────
    // When Codex runs with --output-schema and --output-last-message the final
    // model response is written directly to detection_result.json as clean JSON,
    // eliminating log-scraping parse failures caused by SSE/trace noise.
    core.info(`🔎 Checking for structured result file: ${resultFilePath}`);
    const structuredResult = parseStructuredResultFile(resultFilePath);

    if (structuredResult !== null) {
      if (structuredResult.error) {
        core.warning(`⚠️  Structured result file exists but could not be parsed: ${structuredResult.error}`);
        core.info("   Falling back to detection log parsing...");
      } else if (!structuredResult.verdict) {
        core.warning("⚠️  Structured result parsed but verdict was missing. Falling back to detection log parsing...");
      } else {
        core.info("✔️  Structured result file found and parsed successfully.");
        evaluateAndReportVerdict(structuredResult.verdict, "from structured result file");
        return;
      }
    } else {
      core.info("ℹ️  No structured result file found. Falling back to detection log parsing.");
    }

    // ── Step 3: Verify the detection log file exists ─────────────────────────
    if (!fs.existsSync(logPath)) {
      core.error("❌ Detection log file not found at: " + logPath);
      core.info("📁 Listing all files in artifact directory for diagnosis: " + threatDetectionDir);
      try {
        const files = listFilesRecursively(threatDetectionDir, threatDetectionDir);
        if (files.length === 0) {
          core.warning("   ⚠️  No files found in " + threatDetectionDir);
        } else {
          core.info("   Found " + files.length + " file(s):");
          files.forEach(file => core.info("     - " + file));
        }
      } catch {
        core.warning("   ⚠️  Could not list files in " + threatDetectionDir);
      }
      setDetectionFailure("agent_failure", `${ERR_SYSTEM}: ❌ Detection log file not found at: ${logPath}`);
      return;
    }

    core.info("✔️  Detection log file exists: " + logPath);

    // ── Step 4: Read the detection log ───────────────────────────────────────
    let logContent;
    try {
      logContent = fs.readFileSync(logPath, "utf8");
    } catch (/** @type {any} */ readError) {
      core.error("❌ Failed to read detection log: " + getErrorMessage(readError));
      setDetectionFailure("agent_failure", `${ERR_SYSTEM}: ❌ Failed to read detection log: ${getErrorMessage(readError)}`);
      return;
    }

    const logLines = logContent.split("\n");
    core.info(`📊 Detection log stats: ${logLines.length} lines, ${logContent.length} bytes`);

    // Log lines containing THREAT_DETECTION_RESULT for focused diagnosis
    const resultLineMatches = logLines.map((line, i) => ({ line, idx: i + 1 })).filter(({ line }) => line.includes(RESULT_PREFIX));
    if (resultLineMatches.length > 0) {
      core.info(`📄 Lines containing THREAT_DETECTION_RESULT (${resultLineMatches.length} of ${logLines.length}):`);
      resultLineMatches.forEach(({ line, idx }) => core.info(`   [${idx}] ${line}`));
    } else {
      core.info(`📄 No lines containing THREAT_DETECTION_RESULT found in ${logLines.length} lines`);
    }

    // ── Step 5: Parse the detection result from log ───────────────────────────
    core.info("🔎 Parsing THREAT_DETECTION_RESULT from detection log...");
    const { verdict, error } = parseDetectionLog(logContent);

    if (error || !verdict) {
      const errorMsg = error || "No verdict returned from detection log parser";
      core.error("❌ Failed to parse detection result: " + errorMsg);
      core.info("💡 This usually means the AI engine did not produce the expected output format.");
      core.info('   Expected format: THREAT_DETECTION_RESULT:{"prompt_injection":bool,"secret_leak":bool,"malicious_patch":bool,"reasons":[...]}');
      setDetectionFailure("parse_error", `${ERR_PARSE}: ❌ ${errorMsg}`);
      return;
    }

    // ── Step 6: Log and evaluate verdict ─────────────────────────────────────
    evaluateAndReportVerdict(verdict, "");
  } // end runMain
}

module.exports = { main, reportAPIProxyGuardRejection, parseDetectionLog, extractFromStreamJson, extractResultFromText, extractStructuredOutput, parseStructuredResultFile };
