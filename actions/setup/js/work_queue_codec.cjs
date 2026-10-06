// @ts-check
"use strict";

const { createHash } = require("node:crypto");

const MAX_DEPTH = 64;
const MAX_PARSE_BYTES = 80 * 1024 * 1024;
const MAX_SNAPSHOT_PARSE_BYTES = 161 * 1024 * 1024;

function queueError(code, message) {
  return Object.assign(new Error(`${code}: ${message}`), { code });
}

function utf8Compare(a, b) {
  return Buffer.compare(Buffer.from(a, "utf8"), Buffer.from(b, "utf8"));
}

function validString(value) {
  if (typeof value !== "string" || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value)) {
    throw queueError("invalid_unicode", "strings must contain valid Unicode scalar values");
  }
  return value;
}

function canonical(value, depth = 0) {
  if (depth > MAX_DEPTH) throw queueError("resource_limit", "JSON nesting exceeds 64");
  if (value === null) return "null";
  if (typeof value === "string") return JSON.stringify(validString(value));
  if (typeof value === "boolean") return String(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw queueError("codec_invalid", "expected a finite JSON number");
    if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw queueError("noncanonical_number", "use safe integer numbers or explicit strings for other exact quantities");
    return String(value);
  }
  if (Array.isArray(value)) {
    if (value.length > 16384) throw queueError("resource_limit", "JSON array exceeds 16384 members");
    const items = [];
    for (let index = 0; index < value.length; index++) {
      if (!Object.hasOwn(value, index)) throw queueError("codec_invalid", "sparse JSON arrays are unsupported");
      items.push(canonical(value[index], depth + 1));
    }
    return `[${items.join(",")}]`;
  }
  if (typeof value !== "object" || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null)) {
    throw queueError("codec_invalid", "expected JSON data");
  }
  return `{${Object.keys(value)
    .sort(utf8Compare)
    .map(key => `${JSON.stringify(validString(key))}:${canonical(value[key], depth + 1)}`)
    .join(",")}}`;
}

function canonicalBytes(value) {
  return Buffer.byteLength(canonical(value), "utf8");
}

function digest(value) {
  return createHash("sha256").update(canonical(value), "utf8").digest("hex");
}

function fingerprint(actor, kind, parameters) {
  return digest({ actor, kind, parameters });
}

// JSON.parse alone cannot detect duplicate (including escaped-equivalent) keys.
function parseStrictJSON(text, { maxBytes = MAX_PARSE_BYTES } = {}) {
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 1 || maxBytes > MAX_SNAPSHOT_PARSE_BYTES) throw queueError("resource_limit", "JSON parser byte limit must be a positive safe integer at most 161 MiB");
  if (typeof text !== "string" || Buffer.byteLength(text, "utf8") > maxBytes) throw queueError("resource_limit", "JSON input exceeds parser limit");
  let i = 0;
  const skip = () => {
    while (i < text.length && /[\x20\t\r\n]/.test(text[i])) i++;
  };
  const fail = () => {
    throw queueError("codec_invalid", `invalid JSON at offset ${i}`);
  };
  function string() {
    const start = i++;
    while (i < text.length) {
      const char = text[i++];
      if (char === '"') {
        let decoded;
        try {
          decoded = JSON.parse(text.slice(start, i));
        } catch (error) {
          if (!(error instanceof SyntaxError)) throw error;
          return fail();
        }
        return validString(decoded);
      }
      if (char === "\\") i++;
      else if (char.charCodeAt(0) < 32) fail();
    }
    return fail();
  }
  function read(depth) {
    if (depth > MAX_DEPTH) throw queueError("resource_limit", "JSON nesting exceeds 64");
    skip();
    const char = text[i];
    if (char === '"') return string();
    if (char === "{") {
      i++;
      const object = Object.create(null);
      skip();
      if (text[i] === "}") {
        i++;
        return object;
      }
      for (;;) {
        skip();
        if (text[i] !== '"') fail();
        const key = string();
        if (Object.hasOwn(object, key)) throw queueError("duplicate_key", "duplicate JSON object key");
        skip();
        if (text[i++] !== ":") fail();
        object[key] = read(depth + 1);
        skip();
        const end = text[i++];
        if (end === "}") return object;
        if (end !== ",") fail();
      }
    }
    if (char === "[") {
      i++;
      const array = [];
      skip();
      if (text[i] === "]") {
        i++;
        return array;
      }
      for (;;) {
        if (array.length >= 16384) throw queueError("resource_limit", "JSON array exceeds 16384 members");
        array.push(read(depth + 1));
        skip();
        const end = text[i++];
        if (end === "]") return array;
        if (end !== ",") fail();
      }
    }
    const literal = /^(?:null|true|false|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)/.exec(text.slice(i));
    if (!literal) return fail();
    i += literal[0].length;
    const value = JSON.parse(literal[0]);
    if (typeof value === "number" && (!Number.isSafeInteger(value) || String(value) !== literal[0])) throw queueError("noncanonical_number", "use safe integer numbers or canonical decimal strings");
    return value;
  }
  const result = read(0);
  skip();
  if (i !== text.length) fail();
  return result;
}

function closed(value, required, optional = [], name = "record") {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw queueError("ledger_invalid", `${name} must be an object`);
  if (required.some(key => !Object.hasOwn(value, key)) || Object.keys(value).some(key => !required.includes(key) && !optional.includes(key))) {
    throw queueError("ledger_invalid", `${name} has missing or unknown fields`);
  }
}

function identity(value, name = "identity", allowEmpty = false, maxBytes = 256) {
  validString(value);
  if ((!allowEmpty && !value) || Buffer.byteLength(value, "utf8") > maxBytes || /[\x00-\x1f\x7f]/.test(value)) throw queueError("ledger_invalid", `invalid ${name}`);
  return value;
}

function integer(value, min, max, name) {
  if (!Number.isSafeInteger(value) || value < min || value > max) throw queueError("ledger_invalid", `invalid ${name}`);
  return value;
}

function validateReason(reason) {
  if (typeof reason !== "string" || reason.length > 128 || !/^[A-Za-z0-9]/.test(reason) || /[^A-Za-z0-9_.:-]/.test(reason)) throw queueError("reason_invalid", "reason must be a bounded sanitized code");
  return reason;
}

module.exports = { MAX_PARSE_BYTES, MAX_SNAPSHOT_PARSE_BYTES, canonical, canonicalBytes, closed, digest, fingerprint, identity, integer, parseStrictJSON, queueError, utf8Compare, validateReason, validString };
