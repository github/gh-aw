// @ts-check
"use strict";

const { parseLogEntries } = require("./log_parser_shared.cjs");

const CODEX_LEGACY_OUTCOME = /^(?:([\w-]+)\.([\w-]+)\(.*\)|(.+?))\s+(success|succeeded|failure|failed)\s+in\s+(\d+(?:\.\d+)?)(ms|s):$/;
const CODEX_LEGACY_METADATA = /^(?:OpenAI Codex|--------|workdir:|model:|provider:|approval:|sandbox:|reasoning effort:|reasoning summaries:|DEBUG codex|INFO codex|\d{4}-\d{2}-\d{2}T[\d:.]+Z\s+(?:DEBUG|INFO|WARN|ERROR))/;
const CODEX_LEGACY_TOOL = /^tool\s+([\w-]+)\.([\w-]+)\((.*)\)$/;
const CODEX_LEGACY_OLD_TOOL = /^ToolCall:\s+([\w-]+)__([\w-]+)\s+(.*)$/;
const CODEX_LEGACY_EXEC = /^exec\s+(?:bash\s+-lc\s+'([^']*)'|(.+?)(?: in \/.*)?)$/;

/** @param {string} line @returns {string} */
function codexLegacyPayload(line) {
  return line.replace(/^\[[^\]]+\]\s+/, "").replace(/^\d{4}-\d{2}-\d{2}T\S+\s+(?:DEBUG|INFO|WARN|ERROR)\s+\S+:\s*/, "");
}

/** @param {string} line @returns {number|undefined} */
function extractCodexLegacyTokens(line) {
  const payload = codexLegacyPayload(line);
  const totalMatch = payload.startsWith("total_tokens:") || payload.includes("TokenCount") ? payload.match(/\btotal_tokens:\s*([\d,]+)/) : null;
  const match = payload.match(/^tokens\s+used:\s*([\d,]+)\s*$/i) ?? totalMatch;
  if (!match) return undefined;
  if (!/^\d+(?:,\d{3})*$/.test(match[1])) return undefined;
  const count = Number(match[1].replace(/,/g, ""));
  return Number.isSafeInteger(count) && count >= 0 ? count : undefined;
}

/** @param {string} line @returns {boolean} */
function codexLegacyBoundary(line) {
  const payload = codexLegacyPayload(line);
  return (
    /^(?:thinking|codex|user|tokens used)$/.test(payload) ||
    CODEX_LEGACY_METADATA.test(line) ||
    CODEX_LEGACY_TOOL.test(payload) ||
    CODEX_LEGACY_OLD_TOOL.test(payload) ||
    CODEX_LEGACY_EXEC.test(payload) ||
    CODEX_LEGACY_OUTCOME.test(payload) ||
    /^(?:ERROR:|Reconnecting\.\.\.)/.test(payload) ||
    extractCodexLegacyTokens(line) !== undefined
  );
}

/** @param {string[]} lines @returns {string[]} */
function codexLegacyStartupLines(lines) {
  const end = lines.findIndex(line => codexLegacyBoundary(line) && !CODEX_LEGACY_METADATA.test(line));
  return end === -1 ? lines : lines.slice(0, end);
}

/**
 * Legacy message channels can contain JSON answers or quoted event records.
 * Their text belongs to the channel until the next native text boundary.
 * @param {string[]} lines
 * @param {number} currentIndex
 * @returns {number|null}
 */
function codexLegacyMessageEnd(lines, currentIndex) {
  if (!/^(?:thinking|codex|user)$/.test(codexLegacyPayload(lines[currentIndex]))) return null;
  let end = currentIndex;
  while (end + 1 < lines.length && !codexLegacyBoundary(lines[end + 1])) end++;
  return end;
}

/**
 * A JSON value following a legacy completion belongs to that tool, regardless
 * of lifecycle-looking fields inside its payload.
 * @param {string[]} lines
 * @param {number} currentIndex
 * @returns {number|null}
 */
function codexLegacyResultEnd(lines, currentIndex) {
  if (!CODEX_LEGACY_OUTCOME.test(codexLegacyPayload(lines[currentIndex]))) return null;
  let depth = 0;
  let inString = false;
  let escaped = false;
  let started = false;
  const content = [];
  for (let index = currentIndex + 1; index < lines.length; index++) {
    const line = lines[index];
    content.push(line);
    for (const character of line) {
      if (!started) {
        if (/\s/.test(character)) continue;
        if (character !== "{" && character !== "[") return null;
        started = true;
      }
      if (inString) {
        if (escaped) escaped = false;
        else if (character === "\\") escaped = true;
        else if (character === '"') inString = false;
      } else if (character === '"') inString = true;
      else if (character === "{" || character === "[") depth++;
      else if (character === "}" || character === "]") depth--;
    }
    if (started && depth === 0 && !inString) {
      try {
        JSON.parse(content.join("\n"));
        return index;
      } catch {
        return null;
      }
    }
  }
  return null;
}

/** @param {string} content @returns {Array<any>} */
function collectCodexJSONRecords(content) {
  if (typeof content !== "string" || content.length === 0) return [];
  let document;
  try {
    document = JSON.parse(content);
  } catch {
    document = null;
  }
  if (Array.isArray(document)) return parseLogEntries(content) ?? [];
  const records = [];
  const lines = content.split("\n");
  for (let index = 0; index < lines.length; index++) {
    const end = codexLegacyMessageEnd(lines, index) ?? codexLegacyResultEnd(lines, index);
    if (end !== null) {
      index = end;
      continue;
    }
    records.push(...(parseLogEntries(lines[index]) ?? []));
  }
  return records;
}

module.exports = {
  CODEX_LEGACY_OUTCOME,
  CODEX_LEGACY_METADATA,
  CODEX_LEGACY_TOOL,
  CODEX_LEGACY_OLD_TOOL,
  CODEX_LEGACY_EXEC,
  codexLegacyPayload,
  codexLegacyBoundary,
  codexLegacyStartupLines,
  codexLegacyMessageEnd,
  codexLegacyResultEnd,
  extractCodexLegacyTokens,
  collectCodexJSONRecords,
};
