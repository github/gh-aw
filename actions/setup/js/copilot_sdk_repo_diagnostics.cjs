// @ts-check

"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { neutralizeWorkflowCommands } = require("./sanitized_logging.cjs");
const { collectAddMaskedValues, applyAddMaskRedaction } = require("./add_mask_redaction.cjs");
const { BUILT_IN_PATTERNS } = require("./redact_secrets.cjs");

const MAX_REPOSITORY_FAILURE_BYTES = 8192;
const MAX_REPOSITORY_DIAGNOSTIC_PATHS = 20;
const MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES = 256;
const MAX_REPOSITORY_EVENT_SECTIONS = 32;
const REPOSITORY_DIAGNOSTIC_FALLBACK = "Repository operation failed; diagnostic details withheld because safe formatting failed.";
const TRUNCATED = " [truncated]";

/** @typedef {{message: string, stdout?: string, stderr?: string, tracked?: string[], untracked?: string[]}} RepositoryDiagnostic */
/** @typedef {{label: string, text: string, path?: boolean}} DiagnosticSection */

/** @param {string} diagnostic @returns {import("@github/copilot-sdk").ToolResultObject} */
function failureEnvelope(diagnostic) {
  /** @type {import("@github/copilot-sdk").ToolResultObject} */
  const result = { resultType: "failure", textResultForLlm: diagnostic, error: diagnostic };
  return result;
}

/** @param {string[]} inputs @param {string[]} supplied */
function diagnosticMasks(inputs, supplied) {
  const content = inputs.join("\n");
  const values = [...supplied, ...collectAddMaskedValues(content.replace(/\r\n?/g, "\n"))];
  for (const { pattern } of BUILT_IN_PATTERNS) {
    for (const value of content.match(pattern) ?? []) values.push(value);
  }
  for (const match of content.matchAll(/\b(?:proxy-)?authorization\b["']?[ \t]*[:=][ \t]*["']?([^\r\n"']+)/gi)) values.push(match[1].trim());
  for (const match of content.matchAll(/\b[a-z][a-z0-9+.-]*:\/\/([^\s/?#"'<>]+)@/gi)) values.push(match[1]);
  for (const match of content.matchAll(/[?&](?:access_token|token|api[_-]?key|key|auth|authorization|password|sig|signature)=([^&#\s"'<>]+)/gi)) values.push(match[1]);
  return [...new Set(values.flatMap(value => [value, value.replace(/^(?:Bearer|Basic|token)[ \t]+/i, "")]).filter(Boolean))].sort((left, right) => right.length - left.length);
}

/** @param {string} text */
function renderLiteral(text) {
  const neutralized = neutralizeWorkflowCommands(text).replaceAll("::", ": :");
  return JSON.stringify(neutralized)
    .slice(1, -1)
    .replace(/[<>&`{}\[\]\x7f-\x9f\u061c\u200b-\u200f\u2028-\u202e\u2060-\u206f\ufeff]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

/** @param {string} rendered @param {number} bytes */
function excerpt(rendered, bytes) {
  if (Buffer.byteLength(rendered, "utf8") <= bytes) return rendered;
  let prefix = "";
  let size = Buffer.byteLength(TRUNCATED, "utf8");
  // Keep both UTF-8 code points and literal escape sequences intact.
  for (const [token] of rendered.matchAll(/\\(?:u[0-9a-f]{4}|["\\bfnrt])|[\s\S]/gu)) {
    size += Buffer.byteLength(token, "utf8");
    if (size > bytes) break;
    prefix += token;
  }
  return prefix + TRUNCATED;
}

/** @param {RepositoryDiagnostic} diagnostic @returns {DiagnosticSection[]} */
function diagnosticSections({ message, stdout, stderr, tracked = [], untracked = [] }) {
  /** @type {DiagnosticSection[]} */
  const sections = [{ label: "", text: message || "Repository operation failed (no diagnostic message)." }];
  if (stdout) sections.push({ label: "stdout: ", text: stdout });
  if (stderr) sections.push({ label: "stderr: ", text: stderr });
  if (tracked.length || untracked.length) sections.push({ label: "", text: `Tracked changes: ${tracked.length}; untracked additions: ${untracked.length}.` });
  // Interleave the separately validated, sorted lists so neither category is hidden.
  for (let index = 0; index < Math.max(tracked.length, untracked.length); index++) {
    if (index < tracked.length) sections.push({ label: "tracked change: ", text: tracked[index], path: true });
    if (index < untracked.length) sections.push({ label: "untracked addition: ", text: untracked[index], path: true });
  }
  return sections;
}

/** @param {DiagnosticSection[]} sections @param {string[]} maskedValues @param {string} [separator] */
function formatSections(sections, maskedValues, separator = "\n") {
  // Collect declarations from ALL bounded inputs, including undisplayed paths
  // and late/cross-stream masks, before taking any display excerpt.
  const masks = diagnosticMasks(
    sections.map(section => section.text),
    maskedValues
  );
  let paths = 0;
  const rendered = sections.map(section => ({ ...section, text: renderLiteral(applyAddMaskRedaction(section.text, masks)) || "(redacted)" })).filter(section => !section.path || ++paths <= MAX_REPOSITORY_DIAGNOSTIC_PATHS);
  if (paths > MAX_REPOSITORY_DIAGNOSTIC_PATHS) rendered.push({ label: "", text: `[truncated: ${paths - MAX_REPOSITORY_DIAGNOSTIC_PATHS} additional mutation paths omitted]` });
  let low = Buffer.byteLength(TRUNCATED, "utf8");
  let high = MAX_REPOSITORY_FAILURE_BYTES;
  let result = failureEnvelope(REPOSITORY_DIAGNOSTIC_FALLBACK);
  // A shared excerpt allowance reserves space for each nonempty stream/path.
  // Measure the FINAL envelope: JSON escaping and the duplicated message count.
  while (low <= high) {
    const allowance = Math.floor((low + high) / 2);
    const text = rendered.map(section => section.label + excerpt(section.text, section.path ? Math.min(allowance, MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES) : allowance)).join(separator);
    const candidate = failureEnvelope(text);
    if (Buffer.byteLength(JSON.stringify(candidate), "utf8") <= MAX_REPOSITORY_FAILURE_BYTES) {
      result = candidate;
      low = allowance + 1;
    } else {
      high = allowance - 1;
    }
  }
  return result;
}

/** @param {RepositoryDiagnostic} diagnostic @param {string[]} maskedValues */
function formatFailure(diagnostic, maskedValues) {
  return formatSections(diagnosticSections(diagnostic), maskedValues);
}

class RepositoryDiagnosticError extends Error {
  /** @type {RepositoryDiagnostic} */
  #diagnostic;
  /** @type {string[]} */
  #maskedValues;

  /** @param {RepositoryDiagnostic} diagnostic @param {string[]} maskedValues */
  constructor(diagnostic, maskedValues) {
    super(diagnostic.message);
    this.#diagnostic = diagnostic;
    this.#maskedValues = [...maskedValues];
    // Wrappers that use getErrorMessage still receive safe, useful detail.
    this.message = createRepositoryFailure(this).textResultForLlm;
  }

  /** @param {string[]} maskedValues */
  format(maskedValues) {
    return formatFailure(this.#diagnostic, [...this.#maskedValues, ...maskedValues]);
  }
}

/**
 * Formatting never classifies an outcome or expands a stack, cause or aggregate.
 * Callers supply only credentials they already hold; this helper discovers none.
 * @param {unknown} error
 * @param {string[]} [maskedValues]
 * @returns {import("@github/copilot-sdk").ToolResultObject}
 */
function createRepositoryFailure(error, maskedValues = []) {
  try {
    return error instanceof RepositoryDiagnosticError ? error.format(maskedValues) : formatFailure({ message: getErrorMessage(error) }, maskedValues);
  } catch {
    return failureEnvelope(REPOSITORY_DIAGNOSTIC_FALLBACK);
  }
}

/**
 * Keep native diagnostic sections independently budgeted when logging them again.
 * Recognized labels affect display only, never failure classification or path authorization.
 * @param {unknown} error
 * @param {string[]} [maskedValues]
 * @returns {import("@github/copilot-sdk").ToolResultObject}
 */
function createRepositoryEventFailure(error, maskedValues = []) {
  try {
    const message = getErrorMessage(error);
    if (!message) return formatFailure({ message }, maskedValues);
    // Redact the whole captured message before splitting or removing labels.
    const redacted = applyAddMaskRedaction(message, diagnosticMasks([message], maskedValues));
    const lines = redacted.split("\n");
    if (lines.length > MAX_REPOSITORY_EVENT_SECTIONS) return failureEnvelope(REPOSITORY_DIAGNOSTIC_FALLBACK);
    const sections = lines.map(text => {
      const label = /^(stdout: |stderr: |tracked change: |untracked addition: )/.exec(text)?.[0] ?? "";
      return { label, text: text.slice(label.length), path: label === "tracked change: " || label === "untracked addition: " };
    });
    return formatSections(sections, [], "\\n");
  } catch {
    return failureEnvelope(REPOSITORY_DIAGNOSTIC_FALLBACK);
  }
}

/** @param {string} command @param {import("./copilot_sdk_repo_process.cjs").ProcessResult} result @param {string[]} [maskedValues] */
function repositoryCommandError(command, { exitCode, stdout, stderr }, maskedValues = []) {
  return new RepositoryDiagnosticError({ message: `${command} failed with exit code ${exitCode}`, stdout, stderr }, maskedValues);
}

/**
 * Paths must already have passed parseRepositoryPaths; rendering does not authorize them.
 * @param {string[]} tracked @param {string[]} untracked @param {string[]} [maskedValues]
 */
function repositoryMutationError(tracked, untracked, maskedValues = []) {
  return new RepositoryDiagnosticError({ message: "Go validation changed or added files in the projected checkout", tracked, untracked }, maskedValues);
}

module.exports = {
  MAX_REPOSITORY_FAILURE_BYTES,
  MAX_REPOSITORY_DIAGNOSTIC_PATHS,
  MAX_REPOSITORY_DIAGNOSTIC_PATH_BYTES,
  REPOSITORY_DIAGNOSTIC_FALLBACK,
  createRepositoryFailure,
  createRepositoryEventFailure,
  repositoryCommandError,
  repositoryMutationError,
};
