// @ts-check

const { stripVTControlCharacters } = require("node:util");
const { createEngineLogParser, generateCopilotCliStyleSummary, buildStepSummaryDetailsSection } = require("./log_parser_shared.cjs");
const { createSessionEvent, isTokenCount } = require("./agent_session.cjs");
const { collectAddMaskedValues, redactMaskedValues, isAddMaskCommandLine } = require("./add_mask_redaction.cjs");

const main = createEngineLogParser({ parserName: "Pydantic AI", parseFunction: parsePydanticLog, supportsDirectories: false });

// Adapt the imported definition's infrastructure filter and text buffering, but
// require the observed pai banner before treating unstructured stdout as speech.
const INFRASTRUCTURE = /^\[(?:INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|pydantic-ai)\]|^ (?:Container|Network|Volume) |^Process exiting with code:|^(?:DEBUG|INFO|WARNING|ERROR):|^Traceback \(most recent call last\):/;
const BANNER = /^(?:clai|pai) - Pydantic AI CLI v([0-9][0-9A-Za-z.+-]*) using (?:custom )?agent \S+ with(?:\s+(\S+))?\s*$/;
const MODEL = /^(?:openai(?:-chat)?|anthropic|copilot|github):\S+$/;
const TOOL_ANNOTATION = /^▌ Called tool ([A-Za-z_][A-Za-z0-9_.:-]*)\.$/;
const ROLE = /^(?:[▌│]\s*)?(user|human|prompt|system|developer|assistant)\s*:\s*(.*)$/i;

/**
 * Parse the observed pai Rich stdout, not an arbitrary plaintext conversation.
 * A "Called tool" annotation establishes a name only: no call ID, arguments,
 * result, success, or turn count is inferred. Structured capture remains partial.
 * @param {string} content
 * @returns {{markdown: string, logEntries: import("./types/agent_session").SessionEvent[], mcpFailures: string[], maxTurnsHit: boolean, partial: boolean}}
 */
function parsePydanticLog(content) {
  /** @type {import("./types/agent_session").SessionEvent[]} */
  const logEntries = [];
  const lines = redactMaskedValues(stripVTControlCharacters(content), collectAddMaskedValues(content)).split(/\r\n|\r|\n/);
  let recognized = false;
  let active = false;
  let userBlock = false;
  let initialized = false;
  let partial = false;
  let cliVersion;
  let model;
  let fence = "";
  let pendingText = [];

  const emit = (type, data) => logEntries.push(createSessionEvent(undefined, type, data));
  function initialize() {
    if (initialized) return;
    emit("session.init", { sourceEngine: "pydantic-ai", cliVersion, ...(model ? { model } : {}) });
    initialized = true;
  }
  function flushText() {
    let start = 0;
    let end = pendingText.length;
    while (start < end && !pendingText[start].trim()) start++;
    while (end > start && !pendingText[end - 1].trim()) end--;
    const text = pendingText.slice(start, end).join("\n");
    pendingText = [];
    if (!text) return;
    initialize();
    emit("assistant.message", { content: text });
  }

  for (const sourceLine of lines) {
    // Rich's observed terminal-width padding is framing, unlike Markdown's
    // one/two trailing spaces and indentation within readable paragraphs.
    const line = sourceLine.replace(/[ \t]{3,}$/, "");
    const trimmed = line.trim();
    if (isAddMaskCommandLine(line)) continue;

    if (!fence) {
      const role = trimmed.match(ROLE);
      if (role) {
        flushText();
        userBlock = role[1].toLowerCase() !== "assistant";
        active = recognized && !userBlock;
        if (active && role[2]) pendingText.push(role[2]);
        continue;
      }
      if (/^(?:>>>|❯|➤|>)\s/.test(trimmed)) {
        flushText();
        userBlock = true;
        active = false;
        continue;
      }
      if (userBlock) continue;
      const banner = trimmed.match(BANNER);
      if (banner) {
        flushText();
        recognized = true;
        active = true;
        cliVersion = banner[1];
        model = banner[2] && MODEL.test(banner[2]) ? banner[2] : undefined;
        continue;
      }
    }
    if (!recognized || userBlock) continue;

    if (!fence && INFRASTRUCTURE.test(line)) {
      flushText();
      if (/^\[INFO\] Stopping containers/.test(line) || /^Process exiting with code:/.test(line)) active = false;
      if (/^Traceback \(most recent call last\):/.test(line)) {
        active = false;
        userBlock = true;
      }
      continue;
    }
    if (!fence && /^[{[]/.test(trimmed)) {
      flushText();
      let raw;
      try {
        raw = JSON.parse(trimmed);
      } catch {
        // An unrecognized multiline record may be an echoed prompt. Do not
        // consume its continuation as assistant text without a new boundary.
        active = false;
        userBlock = true;
        continue;
      }
      if (raw?.type === "result" && initialized) {
        const usage = {};
        for (const key of ["input_tokens", "output_tokens", "total_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"]) {
          if (isTokenCount(raw.usage?.[key])) usage[key] = raw.usage[key];
        }
        emit("session.result", {
          sourceType: "stdout_result",
          ...(isTokenCount(raw.num_turns) ? { numTurns: raw.num_turns } : {}),
          ...(Object.keys(usage).length ? { usage } : {}),
          ...(partial ? { partial: true } : {}),
        });
      } else {
        active = false;
        userBlock = true;
      }
      continue;
    }
    if (!active) continue;
    if (!fence && MODEL.test(trimmed) && !initialized && pendingText.length === 0) {
      model = trimmed;
      continue;
    }

    if (!fence) {
      // The real final answer shares its last stdout line with AWF shutdown.
      const inlineInfrastructure = line.search(/\s{2,}\[(?:INFO|WARN|SUCCESS|ERROR|entrypoint|health-check|pydantic-ai)\]/);
      if (inlineInfrastructure !== -1) {
        if (line.slice(0, inlineInfrastructure).trim()) pendingText.push(line.slice(0, inlineInfrastructure).trimEnd());
        flushText();
        active = false;
        continue;
      }
      const tool = trimmed.match(TOOL_ANNOTATION);
      if (tool) {
        flushText();
        initialize();
        partial = true;
        emit("tool.execution_start", { toolName: tool[1], partial: true, sourceType: "stdout_annotation" });
        emit("session.collection_warning", {
          code: "partial_tool_capture",
          toolName: tool[1],
          message: "Pydantic AI stdout records the tool name only; arguments, results and outcomes were not captured.",
        });
        continue;
      }
      if (/^▌ Called tool\b/.test(trimmed)) continue;
    }
    const boundary = trimmed.match(/^(`{3,}|~{3,})/);
    if (boundary) {
      if (!fence) fence = boundary[1];
      else if (boundary[1][0] === fence[0] && boundary[1].length >= fence.length) fence = "";
    }
    pendingText.push(line);
  }
  flushText();
  return {
    markdown: logEntries.length ? generateCopilotCliStyleSummary(logEntries) : buildStepSummaryDetailsSection("Pydantic AI", "No recognizable Pydantic AI stdout conversation. Raw content is omitted."),
    logEntries,
    mcpFailures: [],
    maxTurnsHit: false,
    partial,
  };
}

module.exports = { main, parsePydanticLog };
