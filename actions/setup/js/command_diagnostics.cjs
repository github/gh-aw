// @ts-check
"use strict";

const path = require("node:path");
const { redactBuiltInPatterns } = require("./redact_secrets.cjs");
const { collectAddMaskedValues, applyAddMaskRedaction } = require("./add_mask_redaction.cjs");
const { parseGoDiagnostics } = require("./go_diagnostics.cjs");
const { parseTypeScriptDiagnostics } = require("./typescript_diagnostics.cjs");
const { parsePythonDiagnostics } = require("./python_diagnostics.cjs");

const MAX_INPUT_BYTES = 256 * 1024;
const MAX_REPORT_BYTES = 16 * 1024;
const MAX_DIAGNOSTICS = 50;
const LANGUAGES = Object.freeze(["go", "typescript", "python"]);
/** @typedef {import("./types/command_diagnostics").DiagnosticReport} Report */
/** @typedef {import("./types/command_diagnostics").CommandDiagnosticInput} Input */
/** @typedef {import("./types/command_diagnostics").DiagnosticOptions} Options */

/** @param {unknown} value @returns {import("./types/command_diagnostics").DiagnosticLanguage[]} */
function parseDiagnosticLanguages(value) {
  if (value === undefined) return [];
  const languages = typeof value === "string" ? [value] : value;
  if (!Array.isArray(languages) || languages.length === 0 || languages.some(language => !LANGUAGES.includes(language)) || new Set(languages).size !== languages.length) {
    throw new Error("diagnostics must be a language or a unique nonempty list of go, typescript, python");
  }
  return languages;
}

/** @param {string | undefined} value */
function parseDiagnosticLanguagesJSON(value) {
  try {
    return parseDiagnosticLanguages(JSON.parse(value || "null") ?? undefined);
  } catch (error) {
    throw new Error("Invalid command diagnostics configuration; use go, typescript, or python", { cause: error });
  }
}

/** @param {string} text @param {number} bytes @returns {string} */
function utf8Prefix(text, bytes) {
  const buffer = Buffer.from(text);
  if (buffer.length <= bytes) return text;
  let end = bytes;
  while (end > 0 && (buffer[end] & 0xc0) === 0x80) end--;
  return buffer.subarray(0, end).toString("utf8");
}

/** @param {unknown} value @returns {value is Report} */
function isDiagnosticReport(value) {
  if (!value || typeof value !== "object" || !("version" in value) || value.version !== 1 || !("diagnostics" in value) || !Array.isArray(value.diagnostics) || value.diagnostics.length > MAX_DIAGNOSTICS) return false;
  if (!("truncated" in value) || typeof value.truncated !== "boolean" || !("issues" in value) || !Array.isArray(value.issues) || value.issues.some(issue => typeof issue !== "string")) return false;
  const location = item =>
    item === undefined ||
    (item &&
      typeof item.path === "string" &&
      item.path.length > 0 &&
      Buffer.byteLength(item.path) <= 1024 &&
      !/[\x00-\x1f\x7f-\x9f\\:]|^\/|(?:^|\/)\.\.(?:\/|$)/.test(item.path) &&
      Number.isSafeInteger(item.line) &&
      item.line > 0 &&
      (item.column === undefined || (Number.isSafeInteger(item.column) && item.column > 0)));
  return (
    Buffer.byteLength(JSON.stringify(value)) <= MAX_REPORT_BYTES &&
    value.diagnostics.every(
      item =>
        item &&
        LANGUAGES.includes(item.language) &&
        ["error", "warning", "info"].includes(item.severity) &&
        typeof item.message === "string" &&
        Buffer.byteLength(item.message) <= 2000 &&
        location(item.location) &&
        [
          [item.code, 128],
          [item.test, 256],
          [item.package, 256],
        ].every(([field, limit]) => field === undefined || (typeof field === "string" && Buffer.byteLength(field) <= limit)) &&
        (item.related === undefined ||
          (Array.isArray(item.related) && item.related.length <= 10 && item.related.every(related => related && typeof related.message === "string" && Buffer.byteLength(related.message) <= 1000 && location(related.location))))
    )
  );
}

/** @param {string} value @param {Input} input */
function repositoryPath(value, { root, cwd }) {
  if (Buffer.byteLength(value) > 1024 || /[\x00-\x1f\x7f-\x9f<>]|:\/\//.test(value) || value.split(/[\\/]/).includes("..")) return undefined;
  const windows = /^[A-Za-z]:[\\/]/.test(value) || /^[A-Za-z]:[\\/]/.test(root ?? "");
  const paths = windows ? path.win32 : path.posix;
  if (!root || !paths.isAbsolute(root)) return undefined;
  const base = cwd ? paths.resolve(root, cwd) : root;
  const absolute = paths.resolve(base, value);
  const relative = paths.relative(root, absolute).replaceAll("\\", "/");
  if (!relative || relative === ".." || relative.startsWith("../") || paths.isAbsolute(relative) || relative.includes(":")) return undefined;
  return relative;
}

/** @param {Input} input @param {unknown} selected @param {Options} [options] @returns {Report} */
function parseDiagnostics(input, selected, options = {}) {
  const languages = parseDiagnosticLanguages(selected);
  /** @type {Report} */
  const report = { version: 1, diagnostics: [], truncated: false, issues: [] };
  if (languages.length === 0) return report;
  const inputs = [input.stdout, input.stderr, input.output].filter(value => typeof value === "string");
  const total = inputs.reduce((size, value) => size + Buffer.byteLength(value), 0);
  if (total > MAX_INPUT_BYTES) {
    // Do not excerpt before redaction: a late add-mask declaration may protect an early value.
    report.truncated = true;
    report.issues.push("Diagnostic input exceeds 256 KiB; parsing withheld.");
    return report;
  }
  const combined = inputs.join("\n").replace(/\r\n?/g, "\n");
  const masks = [...(options.maskedValues ?? []), ...collectAddMaskedValues(combined)];
  for (const match of combined.matchAll(/\b(?:proxy-)?authorization\b["']?[ \t]*[:=][ \t]*["']?([^\r\n"']+)/gi)) {
    masks.push(match[1].trim(), match[1].trim().replace(/^(?:Bearer|Basic|token)[ \t]+/i, ""));
  }
  const redact = text => redactBuiltInPatterns(applyAddMaskRedaction(text, [...(options.secrets ?? []), ...masks])).content;
  const seen = new Set();
  const parsers = { go: parseGoDiagnostics, typescript: parseTypeScriptDiagnostics, python: parsePythonDiagnostics };
  const clean = text => redact(text).replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))/g, "");
  for (const language of languages) {
    // Preserve stream boundaries; neither capture nor parsing claims cross-stream order.
    for (const content of inputs) {
      for (const parsed of parsers[language](clean(content.replace(/\r\n?/g, "\n")))) {
        const location = original => {
          const filename = repositoryPath(original.path, input);
          if (!filename || !Number.isSafeInteger(original.line) || original.line < 1) return undefined;
          return { path: filename, line: original.line, ...(Number.isSafeInteger(original.column) && original.column > 0 ? { column: original.column } : {}) };
        };
        const message = redact(parsed.location && !location(parsed.location) ? `${parsed.location.path}:${parsed.location.line}: ${parsed.message}` : parsed.message);
        const related = parsed.related?.slice(0, 10).map(item => {
          const message = redact(item.location && !location(item.location) ? `${item.location.path}:${item.location.line}: ${item.message}` : item.message);
          if (Buffer.byteLength(message) > 1000) report.truncated = true;
          return { message: utf8Prefix(message, 1000), ...(item.location ? { location: location(item.location) } : {}) };
        });
        const diagnostic = {
          ...parsed,
          message: utf8Prefix(message, 2000),
          ...(parsed.code ? { code: utf8Prefix(redact(parsed.code), 128) } : {}),
          ...(parsed.test ? { test: utf8Prefix(redact(parsed.test), 256) } : {}),
          ...(parsed.package ? { package: utf8Prefix(redact(parsed.package), 256) } : {}),
          location: parsed.location ? location(parsed.location) : undefined,
          related,
        };
        if (diagnostic.related?.length === 0) delete diagnostic.related;
        const key = JSON.stringify(diagnostic);
        if (seen.has(key)) continue;
        seen.add(key);
        if (report.diagnostics.length >= MAX_DIAGNOSTICS || Buffer.byteLength(JSON.stringify(report)) + Buffer.byteLength(key) + 64 > MAX_REPORT_BYTES) {
          report.truncated = true;
          continue;
        }
        report.diagnostics.push(diagnostic);
        if (
          Buffer.byteLength(message) > 2000 ||
          (parsed.related?.length ?? 0) > 10 ||
          (parsed.code && Buffer.byteLength(parsed.code) > 128) ||
          (parsed.test && Buffer.byteLength(parsed.test) > 256) ||
          (parsed.package && Buffer.byteLength(parsed.package) > 256)
        )
          report.truncated = true;
      }
    }
  }
  return report;
}

module.exports = { parseDiagnostics, parseDiagnosticLanguages, parseDiagnosticLanguagesJSON, isDiagnosticReport, utf8Prefix, MAX_INPUT_BYTES, MAX_REPORT_BYTES, MAX_DIAGNOSTICS };
