// @ts-check
/// <reference types="@actions/github-script" />

require("./shim.cjs");
const fs = require("fs");
const path = require("path");
const { randomUUID } = require("crypto");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/**
 * @typedef {{opaque: string, rendered: string, metadata: boolean}} LiteralFile
 * @typedef {{version: number, prefix: string, values: string[], files: Record<string, LiteralFile>}} LiteralState
 */

/** @param {LiteralState} [state] */
function createLiteralValues(state = { version: 1, prefix: `\x00VALUE_${randomUUID()}_`, values: [], files: {} }) {
  const pattern = new RegExp(`${state.prefix}(\\d+)\x00`, "g");
  /** @param {string} text */
  const restore = text =>
    text.replace(pattern, (_, index) => {
      const value = state.values[Number(index)];
      if (value === undefined) {
        core.debug("[promptLiteralValues] Rejected state: unknown_value");
        throw new Error(`${ERR_VALIDATION}: Unknown literal prompt value`);
      }
      return value;
    });
  /** @param {string} value */
  const protect = value => {
    // Flatten our own earlier protection, but never interpret a prior run's markers.
    state.values.push(restore(value));
    return `${state.prefix}${state.values.length - 1}\x00`;
  };
  /** @param {string} text */
  const strip = text => text.replace(pattern, "");
  return { state, protect, restore, strip };
}

/**
 * Restore only compiler-owned JSON scalar values. Evaluated context cannot
 * manufacture engine metadata keys or document delimiters.
 * @param {string} content
 * @param {(text: string) => string} restore
 */
function restoreInlineMetadata(content, restore) {
  const match = /^---[ \t]*\r?\n([\s\S]*?)\r?\n---[ \t]*\r?(?=\n|$)/.exec(content);
  if (!match) {
    const rendered = restore(content);
    // A restored body must not manufacture frontmatter for the engine's reader.
    if (!/^---[ \t]*\r?(?=\n|$)/.test(content) && /^---[ \t]*\r?(?=\n|$)/.test(rendered)) {
      return `---\n{}\n---\n${rendered}`;
    }
    return rendered;
  }
  const { editMarkdownMetadata, transformValues } = require("./markdown_metadata.cjs");
  const rendered = editMarkdownMetadata(content, metadata => transformValues(metadata, restore));
  const renderedMatch = /^---[ \t]*\r?\n([\s\S]*?)\r?\n---[ \t]*\r?(?=\n|$)/.exec(rendered);
  if (!renderedMatch || restore(renderedMatch[1]) !== renderedMatch[1]) throw new Error(`${ERR_VALIDATION}: Evaluated inline metadata must be a scalar value`);
  return rendered.slice(0, renderedMatch[0].length) + restore(rendered.slice(renderedMatch[0].length));
}

/** @param {string} file */
function literalStatePath(file) {
  return path.join(path.dirname(file), "prompt-literal-values.json");
}

/** @param {string} file @param {LiteralState} state */
function writeLiteralState(file, state) {
  const pattern = new RegExp(`${state.prefix}(\\d+)\x00`, "g");
  const used = new Set();
  for (const entry of Object.values(state.files)) {
    for (const match of entry.opaque.replaceAll("\\u0000", "\x00").matchAll(pattern)) used.add(Number(match[1]));
  }
  // Do not persist values discarded by author conditionals or metadata filtering.
  state.values = state.values.map((value, index) => (used.has(index) ? value : ""));
  fs.writeFileSync(literalStatePath(file), JSON.stringify(state), "utf8");
  core.debug(`[promptLiteralValues] Persisted ${Object.keys(state.files).length} file(s), ${used.size} referenced value(s), ${state.values.length - used.size} discarded value slot(s)`);
}

/** @param {string} file @returns {LiteralState|null} */
function readLiteralState(file) {
  const statePath = literalStatePath(file);
  if (!fs.existsSync(statePath)) {
    core.debug("[promptLiteralValues] No persisted state");
    return null;
  }
  if (fs.lstatSync(statePath).isSymbolicLink() || !fs.lstatSync(statePath).isFile()) {
    core.debug("[promptLiteralValues] Rejected state: non_regular_file");
    throw new Error(`${ERR_VALIDATION}: Literal prompt state must be a regular file`);
  }
  const serialized = fs.readFileSync(statePath, "utf8");
  let state;
  try {
    state = JSON.parse(serialized);
  } catch (error) {
    core.debug("[promptLiteralValues] Rejected state: invalid_json");
    // JSON parser messages can quote private state; preserve the cause, not its text.
    throw new Error(`${ERR_VALIDATION}: Literal prompt state must be valid JSON`, { cause: error });
  }
  if (
    !state ||
    typeof state !== "object" ||
    state.version !== 1 ||
    typeof state.prefix !== "string" ||
    !/^\x00VALUE_[0-9a-f-]+_$/.test(state.prefix) ||
    !Array.isArray(state.values) ||
    !state.values.every((/** @type {unknown} */ value) => typeof value === "string") ||
    !state.files ||
    typeof state.files !== "object"
  ) {
    core.debug("[promptLiteralValues] Rejected state: invalid_schema");
    throw new Error(`${ERR_VALIDATION}: Invalid literal prompt state`);
  }
  core.debug(`[promptLiteralValues] Loaded ${Object.keys(state.files).length} file(s), ${state.values.length} value slot(s)`);
  return state;
}

/** @param {string} prompt @param {string} target */
function literalFileKey(prompt, target) {
  // Inline artifacts are staged in the fixed root while activation prompts live
  // under runner.temp. Both are downloaded together under /tmp/gh-aw later.
  if (path.resolve(target).startsWith("/tmp/gh-aw/") && !path.resolve(prompt).startsWith("/tmp/gh-aw/")) {
    return path.relative("/tmp/gh-aw/aw-prompts", path.resolve(target));
  }
  return path.relative(path.dirname(path.resolve(prompt)), path.resolve(target));
}

/**
 * Read the author-owned view for validation/substitution, checking it has not
 * diverged from the rendered file. Evaluated context is not an unresolved macro.
 * @param {string} prompt @param {string} target @param {LiteralState|null} state
 */
function readAuthorContent(prompt, target, state) {
  const content = fs.readFileSync(target, "utf8");
  const entry = state?.files[literalFileKey(prompt, target)];
  if (!entry) return { content, metadata: false };
  if (typeof entry.opaque !== "string" || typeof entry.rendered !== "string" || typeof entry.metadata !== "boolean" || entry.rendered !== content) {
    core.debug("[promptLiteralValues] Rejected state: content_mismatch");
    throw new Error(`${ERR_VALIDATION}: Literal prompt state does not match ${target}`);
  }
  return { content: entry.opaque, metadata: entry.metadata };
}

module.exports = { createLiteralValues, restoreInlineMetadata, literalStatePath, literalFileKey, readLiteralState, writeLiteralState, readAuthorContent };

// The shell validator keeps its existing diagnostics, but checks only authored text.
if (require.main === module) {
  const file = process.argv[2];
  const state = readLiteralState(file);
  const { content } = readAuthorContent(file, file, state);
  process.stdout.write(state ? createLiteralValues(state).strip(content) : content);
}
