// @ts-check
"use strict";

const { parseLogEntries } = require("./log_parser_shared.cjs");

const CODEX_LEGACY_OUTCOME = /^(?:([\w-]+)\.([\w-]+)\(.*\)|(.+?))\s+(success|succeeded|failure|failed)\s+in\s+(\d+(?:\.\d+)?)(ms|s):$/;

/** @param {string} line @returns {string} */
function codexLegacyPayload(line) {
  return line.replace(/^\[[^\]]+\]\s+/, "").replace(/^\d{4}-\d{2}-\d{2}T\S+\s+(?:DEBUG|INFO|WARN|ERROR)\s+\S+:\s*/, "");
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
    const end = codexLegacyResultEnd(lines, index);
    if (end !== null) {
      index = end;
      continue;
    }
    records.push(...(parseLogEntries(lines[index]) ?? []));
  }
  return records;
}

module.exports = { CODEX_LEGACY_OUTCOME, codexLegacyPayload, codexLegacyResultEnd, collectCodexJSONRecords };
