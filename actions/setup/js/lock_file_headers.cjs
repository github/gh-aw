// @ts-check
"use strict";

const { createInterface } = require("node:readline");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

/** @param {string} source */
function parseMarkdownJSON(source) {
  if (Buffer.byteLength(source, "utf8") > 4 * 1024 * 1024) throw new Error("Markdown metadata exceeds the bounded input limit");
  const value = JSON.parse(source);
  /** @type {Array<{keys: Set<string>|null, key: boolean}>} */
  const stack = [];
  let nodes = 0;
  for (const token of source.matchAll(/"(?:\\.|[^"\\])*"|[{}\[\]:,]/g)) {
    if (++nodes > 10000) throw new Error("Markdown JSON metadata exceeds structural limits");
    const text = token[0];
    const frame = stack.at(-1);
    if (text === "{" || text === "[") {
      stack.push({ keys: text === "{" ? new Set() : null, key: text === "{" });
      if (stack.length > 100) throw new Error("Markdown JSON metadata exceeds structural limits");
    } else if (text === "}" || text === "]") stack.pop();
    else if (text === "," && frame?.keys) frame.key = true;
    else if (text.startsWith('"') && frame?.keys && frame.key) {
      const key = JSON.parse(text);
      if (frame.keys.has(key)) throw new Error("Duplicate Markdown JSON metadata key");
      frame.keys.add(key);
      frame.key = false;
    }
  }
  const checkNumbers = (/** @type {any} */ item) => {
    if (typeof item === "number" && !Number.isFinite(item)) throw new Error("Markdown JSON metadata numbers must be finite");
    if (item && typeof item === "object") for (const child of Object.values(item)) checkNumbers(child);
  };
  checkNumbers(value);
  return value;
}

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
        const source = line.slice(prefix.length);
        const value = parseStrictJSON(source, name === "manifest" ? { finiteNumbersIn: "markdown_metadata" } : {});
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

module.exports = { readLockFileHeaders, parseLockFileHeaders, parseMarkdownJSON };
