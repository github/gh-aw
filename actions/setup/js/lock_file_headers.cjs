// @ts-check
"use strict";

const { createInterface } = require("node:readline");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

/** @param {string[]} lines @returns {{metadata: Record<string, unknown>|undefined, manifest: Record<string, unknown>|undefined}} */
function parseLockFileHeaders(lines) {
  const headers = /** @type {{metadata: Record<string, unknown>|undefined, manifest: Record<string, unknown>|undefined}} */ { metadata: undefined, manifest: undefined };
  const seen = new Set();
  for (const line of lines.slice(0, 3)) {
    for (const name of /** @type {const} */ ["metadata", "manifest"]) {
      const prefix = `# gh-aw-${name}: `;
      if (!line.startsWith(prefix)) continue;
      if (seen.has(name)) {
        headers[name] = undefined;
        continue;
      }
      seen.add(name);
      try {
        const value = parseStrictJSON(line.slice(prefix.length));
        if (value !== null && typeof value === "object" && !Array.isArray(value)) headers[name] = value;
      } catch {
        headers[name] = undefined;
      }
    }
  }
  return headers;
}

/** @param {import("node:stream").Readable} reader */
async function readLockFileHeaders(reader) {
  const input = createInterface({ input: reader, crlfDelay: Infinity });
  const lines = [];
  try {
    for await (const line of input) {
      lines.push(line);
      if (lines.length >= 3) break;
    }
  } finally {
    input.close();
    reader.destroy();
  }
  return parseLockFileHeaders(lines);
}

module.exports = { readLockFileHeaders, parseLockFileHeaders };
