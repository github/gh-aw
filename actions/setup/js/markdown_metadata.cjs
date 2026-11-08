// @ts-check
"use strict";

const { execFileSync } = require("node:child_process");
const path = require("node:path");

/**
 * The separate metadata helper owns frontmatter syntax and scalar edits. This bridge only
 * exchanges bounded JSON; lock-file consumers use readLockFileHeaders instead.
 * @param {string} operation
 * @param {string} content
 * @param {{value?: string, replacements?: Record<string, string>, allowDuplicates?: boolean}} [options]
 * @returns {{value: any, content: string, scalars?: Record<string, {value: string, start: number, end: number}>, error?: string}}
 */
function readMarkdownMetadata(operation, content, options = {}) {
  const binary = path.join(__dirname, process.platform === "win32" ? "markdown-metadata.exe" : "markdown-metadata");
  const output = execFileSync(binary, [], {
    input: JSON.stringify({ operation, content, ...options }),
    encoding: "utf8",
    maxBuffer: 4 * 1024 * 1024,
    timeout: 10000,
  });
  const result = JSON.parse(output);
  if (result.error) throw new Error(result.error);
  return result;
}

/** @param {string} content @param {{uniqueKeys?: boolean, version?: string}} [options] */
function readDocumentMetadata(content, options = {}) {
  return readMarkdownMetadata("document", content, { allowDuplicates: options.uniqueKeys === false });
}

module.exports = { readMarkdownMetadata, readDocumentMetadata };
