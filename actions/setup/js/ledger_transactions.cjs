// @ts-check
"use strict";

const crypto = require("node:crypto");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const TEMPORARY_ID = /^#?[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const RESERVED_RECORD_KEYS = new Set(["hash", "id", "parents", "sha", "timestamp", "transaction_id", "version"]);
const MAX_RECORD_DEPTH = 32;
const MAX_RECORD_FIELDS = 128;
const MAX_RECORD_KEY_BYTES = 256;

function finalId(transactionId, index) {
  const hex = crypto.createHash("sha256").update(`${transactionId}:${index}`).digest("hex");
  return `ldg-${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-${((parseInt(hex.slice(16, 18), 16) & 0x3f) | 0x80).toString(16).padStart(2, "0")}${hex.slice(18, 20)}-${hex.slice(20, 32)}`;
}

/**
 * Normalize safe-output ledger append requests without touching canonical state.
 * @param {Array<{ledger?: string, temp_id?: string, record: object}>} requests
 * @param {{transactionId: string, ledgerNames: Set<string>, ledgers?: Record<string, {schema?: object, max_record_kb?: number, max_patch_kb?: number}>}} options
 */
function normalizeLedgerAppends(requests, { transactionId, ledgerNames, ledgers = {} }) {
  if (!Array.isArray(requests) || requests.length > 100) throw new RangeError("Invalid ledger append batch");
  const mapping = new Map();
  const normalized = [];
  const patchBytes = new Map();
  for (const [index, request] of requests.entries()) {
    if (!request || typeof request !== "object" || !request.record || typeof request.record !== "object" || Array.isArray(request.record)) throw new TypeError("Invalid ledger append request");
    const ledger = request.ledger || (ledgerNames.size === 1 ? [...ledgerNames][0] : undefined);
    if (!ledger || !ledgerNames.has(ledger)) throw new TypeError("Unknown target ledger");
    const options = ledgers[ledger] || {};
    const record = sanitizeRecord(request.record);
    if (options.schema) {
      const schemaError = validateValueAgainstSchema(record, options.schema);
      if (schemaError) throw new TypeError(`Ledger record does not match schema: ${schemaError.path || "(root)"} ${schemaError.message || "is invalid"}`);
    }
    const maxRecordBytes = (options.max_record_kb || 32) * 1024;
    const recordBytes = Buffer.byteLength(JSON.stringify(record), "utf8");
    if (recordBytes > maxRecordBytes) throw new RangeError("Ledger record exceeds max-record-kb");
    const tempId = typeof request.temp_id === "string" ? request.temp_id.replace(/^#/, "") : undefined;
    if (request.temp_id !== undefined && (typeof request.temp_id !== "string" || !TEMPORARY_ID.test(request.temp_id) || mapping.has(`${ledger}:${tempId}`))) {
      throw new TypeError("Invalid or duplicate temporary ID");
    }
    const id = finalId(transactionId, index);
    if (tempId) mapping.set(`${ledger}:${tempId}`, id);
    const normalizedRecord = { ...record, id };
    const normalizedRecordBytes = Buffer.byteLength(JSON.stringify(normalizedRecord), "utf8");
    if (normalizedRecordBytes > maxRecordBytes) throw new RangeError("Ledger record exceeds max-record-kb");
    normalized.push({ ledger, transaction_id: transactionId, index, record: normalizedRecord });
    const totalBytes = (patchBytes.get(ledger) || 0) + normalizedRecordBytes;
    if (totalBytes > (options.max_patch_kb || 10) * 1024) throw new RangeError("Ledger append batch exceeds max-patch-kb");
    patchBytes.set(ledger, totalBytes);
  }
  const rewrite = (value, ledger) => {
    if (typeof value === "string") {
      if (!value.startsWith("#")) return value;
      return mapping.get(`${ledger}:${value.slice(1)}`) || value;
    }
    if (Array.isArray(value)) return value.map(child => rewrite(child, ledger));
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, rewrite(child, ledger)]));
    return value;
  };
  for (const item of normalized) item.record = rewrite(item.record, item.ledger);
  return { version: 1, transaction_id: transactionId, appends: normalized };
}

function sanitizeRecord(record) {
  if (Object.getPrototypeOf(record) !== Object.prototype && Object.getPrototypeOf(record) !== null) throw new TypeError("Ledger record must be a plain object");
  const stack = [{ value: record, depth: 0 }];
  while (stack.length > 0) {
    const entry = stack.pop();
    if (!entry) continue;
    const { value, depth } = entry;
    if (depth > MAX_RECORD_DEPTH) throw new RangeError("Ledger record exceeds maximum nesting depth");
    if (Array.isArray(value)) {
      for (const item of value) if (item && typeof item === "object") stack.push({ value: item, depth: depth + 1 });
      continue;
    }
    if (!value || typeof value !== "object") continue;
    const keys = Object.keys(value);
    if (keys.length > MAX_RECORD_FIELDS) throw new RangeError("Ledger record exceeds maximum field count");
    for (const key of keys) {
      if (Buffer.byteLength(key, "utf8") > MAX_RECORD_KEY_BYTES) throw new RangeError("Ledger record field name is too long");
      if (depth === 0 && RESERVED_RECORD_KEYS.has(key)) throw new TypeError(`Ledger record contains reserved field "${key}"`);
      const child = value[key];
      if (child && typeof child === "object") stack.push({ value: child, depth: depth + 1 });
    }
  }
  return JSON.parse(JSON.stringify(record));
}

module.exports = { finalId, normalizeLedgerAppends };
