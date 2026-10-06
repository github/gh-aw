// @ts-check
/// <reference types="@actions/github-script" />

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */

const { createEngineLogParser, generateConversationMarkdown, generateInformationSection, buildStepSummaryDetailsSection, formatInitializationSummary, formatToolUse, AWF_INFRA_LINE_RE } = require("./log_parser_shared.cjs");
const { projectSessionResult, createSessionEvent, accumulateSessionUsage, isTokenCount, normalizeAgentSession, normalizeSessionUsage } = require("./agent_session.cjs");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { getMessageRefusal, normalizeOpenAIChatUsage } = require("./provider_refusal.cjs");

const main = createEngineLogParser({
  parserName: "Copilot",
  parseFunction: parseCopilotLog,
  supportsDirectories: true,
});

const AWF_TOKEN_WARNING_RE = /\[AWF TOKEN WARNING\][^\n\r]+/g;

/** @param {Array<any>} logEntries @returns {string[]} */
function extractAwfTokenWarnings(logEntries) {
  /** @type {string[]} */
  const warnings = [];
  const seen = new Set();
  const visit = value => {
    if (typeof value === "string") {
      for (const match of value.match(AWF_TOKEN_WARNING_RE) ?? []) {
        const normalized = match.trim();
        if (!seen.has(normalized)) {
          seen.add(normalized);
          warnings.push(normalized);
        }
      }
    } else if (Array.isArray(value)) {
      for (const item of value) visit(item);
    } else if (value && typeof value === "object") {
      for (const key of ["text", "message", "content", "system", "data"]) visit(value[key]);
    }
  };
  for (const entry of logEntries) {
    if (entry.type !== "user.message") visit(entry);
  }
  return warnings;
}

/**
 * @param {string} logContent
 * @returns {{markdown: string, logEntries: SessionEvent[], mcpFailures?: string[], maxTurnsHit?: boolean}}
 */
function parseCopilotLog(logContent) {
  let logEntries;
  try {
    logEntries = JSON.parse(logContent);
    if (!Array.isArray(logEntries)) throw new Error("Not a JSON array");
  } catch {
    logEntries = parseDebugLogFormat(logContent);
  }
  let canonicalLogEntries = normalizeCopilotSession(logEntries);
  if (!canonicalLogEntries.length) canonicalLogEntries = normalizeCopilotSession(parsePrettyPrintFormat(logContent));
  if (!canonicalLogEntries.length) {
    return { markdown: buildStepSummaryDetailsSection("Agent Log Summary", "Log format not recognized as Copilot JSON array or JSONL."), logEntries: [] };
  }

  const conversationResult = generateConversationMarkdown(canonicalLogEntries, {
    includeInformation: false,
    formatToolCallback: (toolUse, toolResult) => formatToolUse(toolUse, toolResult, { includeDetailedParameters: false }),
    formatInitCallback: initEntry =>
      formatInitializationSummary(initEntry, {
        includeSlashCommands: false,
        modelInfoCallback: entry => {
          if (!entry.model_info) return "";
          const modelInfo = entry.model_info;
          let markdown = "";
          if (modelInfo.name) {
            markdown += `**Model Name:** ${modelInfo.name}`;
            if (modelInfo.vendor) markdown += ` (${modelInfo.vendor})`;
            markdown += "\n\n";
          }
          if (modelInfo.billing) {
            const billing = modelInfo.billing;
            if (billing.is_premium === true) {
              markdown += "**Premium Model:** Yes";
              if (billing.multiplier && billing.multiplier !== 1) markdown += ` (${billing.multiplier}x cost multiplier)`;
              markdown += "\n";
              if (Array.isArray(billing.restricted_to) && billing.restricted_to.length) markdown += `**Required Plans:** ${billing.restricted_to.join(", ")}\n`;
              markdown += "\n";
            } else if (billing.is_premium === false) {
              markdown += "**Premium Model:** No\n\n";
            }
          }
          return markdown;
        },
      }),
  });
  let markdown = conversationResult.markdown;
  const warnings = extractAwfTokenWarnings(canonicalLogEntries);
  if (warnings.length) markdown += buildStepSummaryDetailsSection("Firewall Steering", warnings.map(warning => `- ${warning}\n`).join(""));
  markdown += generateInformationSection(projectSessionResult(canonicalLogEntries), { additionalInfoCallback: () => "" });
  return { markdown, logEntries: canonicalLogEntries };
}

/**
 * Decode recognized CLI display framing without rewriting the payload text.
 * @param {string} logContent
 * @returns {SessionEvent[]}
 */
function parsePrettyPrintFormat(logContent) {
  if (!/^[✗●✓] /m.test(logContent)) return [];
  const lines = logContent.split("\n");
  /** @type {SessionEvent[]} */
  const entries = [];
  /** @type {string[]} */
  let text = [];
  /** @type {Record<string, any>} */
  const footer = {};
  /** @type {Record<string, any>} */
  const breakdown = {};
  /** @type {Record<string, any>|undefined} */
  let tokens;
  let inModelBreakdown = false;

  const flushText = () => {
    if (text.length) entries.push({ type: "assistant.message", data: { content: text.join("") } });
    text = [];
  };
  const count = value => {
    const number = Number.parseFloat(value);
    return Math.round(number * (/m$/i.test(value) ? 1000000 : /k$/i.test(value) ? 1000 : 1));
  };

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (AWF_INFRA_LINE_RE.test(line)) continue;
    const trimmed = line.trim();
    const marker = line.match(/^([✗●✓]) (.*)$/);
    if (marker) {
      flushText();
      inModelBreakdown = false;
      let header = marker[2];
      const mayWrap = /^\S+\s+\S/.test(header);
      const rawContinuation = [];
      const output = [];
      while (index + 1 < lines.length) {
        const next = lines[index + 1];
        const continuation = next.match(/^\s*[└│] ?(.*)$/);
        if (continuation || /^ {4}/.test(next)) {
          rawContinuation.push(next);
          output.push(continuation ? continuation[1] : next.slice(4));
          index++;
        } else if (mayWrap && /^ {2}\S/.test(next) && !output.length) {
          rawContinuation.push(next);
          header += "\n" + next.slice(2);
          index++;
        } else {
          break;
        }
      }
      const mcp = header.match(/^(\S+) (\S+) · /);
      const namedTool =
        /^(?:Bash|BashOutput|KillBash|Read|Write|Edit|MultiEdit|LS|Grep|Glob|TodoWrite|Task|WebFetch|WebSearch|AskUserQuestion|NotebookEdit|Skill|EnterPlanMode|ExitPlanMode|ListMcpResourcesTool|ReadMcpResourceTool)(?:\s|$)/.test(header);
      // Preserve named tool layouts, without restricting which executables can run.
      const shell = !mcp && !/^mcp__\S+/.test(header) && !namedTool;
      const toolName = mcp ? mcp[2] : shell ? "bash" : header.match(/^\S+/)?.[0];
      const source = { prettyPrint: { marker: marker[1], header: marker[2], continuation: rawContinuation } };
      entries.push(
        createSessionEvent(source, "tool.execution_start", {
          toolName,
          ...(mcp ? { mcpServerName: mcp[1] } : {}),
          ...(shell && !mcp ? { command: header } : {}),
        })
      );
      entries.push(
        createSessionEvent(source, "tool.execution_complete", {
          toolName,
          ...(mcp ? { mcpServerName: mcp[1] } : {}),
          success: marker[1] !== "✗",
          ...(output.length ? { output: output.join("\n") } : {}),
        })
      );
      continue;
    }

    if (/^Breakdown by AI model:/.test(trimmed)) {
      flushText();
      inModelBreakdown = true;
      continue;
    }
    if (inModelBreakdown) {
      const model = line.match(/^ +(\S+)\s+([\d.]+[km]?)\s+in,\s+([\d.]+[km]?)\s+out(?:,\s+([\d.]+[km]?)\s+cached)?/i);
      if (model) {
        entries.push({ type: "session.init", data: { sourceEngine: "copilot", model: model[1] } });
        accumulateSessionUsage(breakdown, {
          input_tokens: count(model[2]),
          output_tokens: count(model[3]),
          ...(model[4] !== undefined ? { cache_read_input_tokens: count(model[4]) } : {}),
        });
        continue;
      }
      inModelBreakdown = false;
    }
    if (/^(?:Total usage est:|API time spent:|Total session time:|Total code changes:|Changes\s+[+-]?\d|Duration\s+\d|Tokens\s+[↑↓]|Resume\s{2,}copilot\s+--resume=|Turns:)/.test(trimmed)) {
      flushText();
      const tokenMatch = trimmed.match(/^Tokens\s+↑\s*([\d.]+[km]?)\s*[•·]\s*↓\s*([\d.]+[km]?)(?:\s*[•·]\s*([\d.]+[km]?)\s*\(cached\))?/i);
      const inlineCached = trimmed.match(/^Tokens\s+↑\s*([\d.]+[km]?)\s*\(\s*([\d.]+[km]?)\s+cached(?:\s*,\s*([\d.]+[km]?)\s+written)?\s*\)\s*[•·]\s*↓\s*([\d.]+[km]?)/i);
      if (tokenMatch) {
        tokens = { input_tokens: count(tokenMatch[1]), output_tokens: count(tokenMatch[2]) };
        if (tokenMatch[3] !== undefined) tokens.cache_read_input_tokens = count(tokenMatch[3]);
      } else if (inlineCached) {
        tokens = { input_tokens: count(inlineCached[1]), output_tokens: count(inlineCached[4]), cache_read_input_tokens: count(inlineCached[2]) };
        if (inlineCached[3] !== undefined) tokens.cache_creation_input_tokens = count(inlineCached[3]);
      }
      const turns = trimmed.match(/^Turns:\s*(\d+)$/i);
      if (turns && isTokenCount(Number(turns[1]))) footer.numTurns = Number(turns[1]);
      const duration = trimmed.match(/^(?:Duration\s+|Total session time:\s*)([\d.]+(?:h|m|s)(?:\s+[\d.]+(?:h|m|s))*)$/);
      if (duration) {
        const milliseconds = [...duration[1].matchAll(/([\d.]+)(h|m|s)/g)].reduce((total, match) => total + Number(match[1]) * ({ h: 3600000, m: 60000, s: 1000 }[match[2]] ?? 0), 0);
        if (Number.isFinite(milliseconds) && milliseconds >= 0) footer.durationMs = milliseconds;
      }
      const resume = trimmed.match(/^Resume\s{2,}copilot\s+--resume=(\S+)$/);
      if (resume) entries.push({ type: "session.init", data: { sourceEngine: "copilot", sessionId: resume[1] } });
      continue;
    }
    text.push(line + (index < lines.length - 1 ? "\n" : ""));
  }
  flushText();
  const reportedUsage = normalizeSessionUsage(tokens ?? breakdown);
  if (reportedUsage && Object.keys(reportedUsage).length) footer.usage = reportedUsage;
  if (Object.keys(footer).length) entries.push({ type: "session.result", data: footer });
  return entries;
}

/**
 * Parse adjacent JSONL records and legacy/Rust structured debug blocks together
 * so one damaged block never hides an independently parseable observation.
 * @param {string} logContent
 * @returns {Array<any>}
 */
function parseDebugLogFormat(logContent) {
  /** @type {Array<any>} */
  const entries = [];
  /** @type {Record<string, any>} */
  const usage = {};
  const responses = new Set();
  let requestId;
  /** @type {{kind: string, lines: string[], timestamp?: string, requestId?: string}|undefined} */
  let block;

  const processBlock = (value, framing) => {
    if (!value || typeof value !== "object") return;
    const source = { ...value, ...(framing.timestamp && value.timestamp === undefined ? { timestamp: framing.timestamp } : {}), ...(framing.requestId && value.requestId === undefined ? { requestId: framing.requestId } : {}) };
    if (framing.kind === "Tools") {
      if (Array.isArray(value)) entries.push(createSessionEvent({ tools: value, ...(framing.timestamp ? { timestamp: framing.timestamp } : {}) }, "session.init", { sourceEngine: "copilot", tools: value }));
      return;
    }
    if (framing.kind === "Got model info") {
      entries.push(createSessionEvent(source, "session.init", { sourceEngine: "copilot", modelInfo: value }));
      return;
    }
    if (value.object === "response") {
      entries.push(...normalizeAgentSession([source], { sourceEngine: "copilot" }));
      return;
    }
    if (typeof value.type === "string" || Array.isArray(value)) {
      entries.push(...(Array.isArray(value) ? value : [value]));
      return;
    }
    if (!Array.isArray(value.choices) && value.error === undefined) return;
    if (typeof value.model === "string") entries.push(createSessionEvent(source, "session.init", { sourceEngine: "copilot", model: value.model }));
    for (const choice of Array.isArray(value.choices) ? value.choices : []) {
      const message = choice?.message;
      if (!message && choice?.finish_reason === "content_filter") {
        entries.push(createSessionEvent(source, "assistant.refusal", { reason: "content_filter" }));
        continue;
      }
      if (!message || (message.role !== undefined && message.role !== "assistant")) continue;
      if (typeof message.reasoning_text === "string") entries.push(createSessionEvent(source, "assistant.reasoning", { content: message.reasoning_text }));
      const refusal = getMessageRefusal(message, choice.finish_reason);
      if (refusal) entries.push(createSessionEvent(source, "assistant.refusal", refusal));
      else if (typeof message.content === "string") entries.push(createSessionEvent(source, "assistant.message", { content: message.content }));
      for (const tool of Array.isArray(message.tool_calls) ? message.tool_calls : []) {
        if (!tool?.function) continue;
        let input = tool.function.arguments;
        if (typeof input === "string") {
          try {
            input = JSON.parse(input);
          } catch {
            // Invalid argument JSON is intentionally ignored; retain the original argument text.
          }
        }
        entries.push(createSessionEvent(source, "tool.execution_start", { ...tool, toolCallId: tool.id, toolName: tool.function.name, input }));
      }
    }
    const identity = value.id ?? framing.requestId;
    const duplicate = identity !== undefined && responses.has(identity);
    if (identity !== undefined) responses.add(identity);
    if (!duplicate && value.usage) {
      accumulateSessionUsage(usage, normalizeOpenAIChatUsage(value.usage) ?? {});
      if (Object.keys(usage).length) entries.push(createSessionEvent(source, "session.result", { usage: { ...usage } }));
    }
    if (!duplicate && value.error !== undefined) entries.push(createSessionEvent(source, "session.result", { errors: [value.error] }));
  };

  for (const line of logContent.split("\n")) {
    const debug = line.match(/^(?:(\d{4}-\d{2}-\d{2}T\S+)\s+)?\[DEBUG\]\s+(?:\[rust:[^\]]+\]\s+)?(.*)$/);
    const clean = debug ? debug[2] : line;
    const response = clean.match(/^response \(Request-ID ([^)]+)\):/);
    if (response) {
      requestId = response[1];
      block = undefined;
      continue;
    }
    const marker = debug && clean.match(/^(data|Tools|Got model info):\s*(.*)$/);
    if (marker) {
      block = { kind: marker[1], lines: [], timestamp: debug[1], requestId: marker[1] === "data" ? requestId : undefined };
      if (marker[1] === "data") requestId = undefined;
    }
    let parsed;
    try {
      parsed = JSON.parse(marker ? marker[2] : clean);
    } catch {
      // Ignore this malformed framing line; independently valid adjacent records remain recoverable.
    }
    if (!marker && parsed && block?.kind !== "Tools" && normalizeAgentSession(Array.isArray(parsed) ? parsed : [parsed]).length) {
      entries.push(...(Array.isArray(parsed) ? parsed : [parsed]));
      block = undefined;
      continue;
    }
    if (!block) continue;
    const payload = marker ? marker[2] : clean;
    if (!marker && /^\d{4}-\d{2}-\d{2}T\S+\s+\[/.test(line) && !/^\s*[\[{"}\]]/.test(payload)) {
      block = undefined;
      continue;
    }
    if (payload || block.lines.length) block.lines.push(payload);
    let value;
    try {
      value = JSON.parse(block.lines.join("\n"));
    } catch {
      // Recover at the next record or block marker, without fabricating a completion for truncated JSON.
      continue;
    }
    processBlock(value, block);
    block = undefined;
  }
  return entries;
}

module.exports = { main, parseCopilotLog, parsePrettyPrintFormat, parseDebugLogFormat };
