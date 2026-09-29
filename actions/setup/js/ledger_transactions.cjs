// @ts-check
"use strict";

const crypto = require("node:crypto");
const { validateValueAgainstSchema } = require("./mcp_scripts_validation.cjs");

const TEMPORARY_ID = /^#?[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const RESERVED_RECORD_KEYS = new Set(["hash", "id", "parents", "payload_sha", "sha", "timestamp", "transaction_id", "version"]);
const DEFAULT_MAX_RECORD_BYTES = 32 * 1024;
const DEFAULT_MAX_PATCH_BYTES = 10 * 1024;

function finalId(transactionId, index) {
  const hex = crypto.createHash("sha256").update(`${transactionId}:${index}`).digest("hex");
  return `ldg-${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-${((parseInt(hex.slice(16, 18), 16) & 0x3f) | 0x80).toString(16).padStart(2, "0")}${hex.slice(18, 20)}-${hex.slice(20, 32)}`;
}

/**
 * Normalize safe-output ledger append requests without touching canonical state.
 * @param {Array<{ledger?: string, temp_id?: string, record: object}>} requests
 * @param {{transactionId: string, ledgerNames?: Set<string>, ledgerConfigs?: Map<string, {schema?: object, maxRecordKB?: number, maxPatchKB?: number}>}} options
 */
function normalizeLedgerAppends(requests, { transactionId, ledgerNames, ledgerConfigs = new Map() }) {
  if (typeof transactionId !== "string" || !transactionId) throw new TypeError("Invalid ledger transaction ID");
  if (!(ledgerConfigs instanceof Map)) throw new TypeError("Invalid ledger configuration");
  const configuredNames = ledgerNames || new Set(ledgerConfigs.keys());
  if (!(configuredNames instanceof Set) || configuredNames.size === 0) throw new TypeError("No ledgers are configured");
  if (!Array.isArray(requests) || requests.length > 100) throw new RangeError("Invalid ledger append batch");
  const mapping = new Map();
  const normalized = [];
  for (const [index, request] of requests.entries()) {
    if (
      !request ||
      typeof request !== "object" ||
      !request.record ||
      typeof request.record !== "object" ||
      Array.isArray(request.record) ||
      (Object.getPrototypeOf(request.record) !== Object.prototype && Object.getPrototypeOf(request.record) !== null)
    ) {
      throw new TypeError("Invalid ledger append request");
    }
    const ledger = request.ledger || (configuredNames.size === 1 ? [...configuredNames][0] : undefined);
    if (!ledger || !configuredNames.has(ledger)) throw new TypeError("Unknown target ledger");
    const config = ledgerConfigs.get(ledger) || {};
    for (const key of Object.keys(request.record)) {
      if (RESERVED_RECORD_KEYS.has(key)) throw new TypeError(`Ledger record contains reserved field "${key}"`);
    }
    let record;
    let recordJSON;
    try {
      recordJSON = JSON.stringify(request.record);
      if (recordJSON === undefined) throw new TypeError("Invalid JSON record");
      record = JSON.parse(recordJSON);
    } catch (error) {
      throw new TypeError("Ledger record must be valid JSON", { cause: error });
    }
    if (config.schema && validateValueAgainstSchema(record, config.schema) !== null) throw new TypeError("Ledger record does not match its configured schema");
    if (request.temp_id !== undefined && (typeof request.temp_id !== "string" || !TEMPORARY_ID.test(request.temp_id) || mapping.has(`${ledger}:${request.temp_id}`))) {
      throw new TypeError("Invalid or duplicate temporary ID");
    }
    const id = finalId(transactionId, index);
    if (request.temp_id) mapping.set(`${ledger}:${request.temp_id}`, id);
    normalized.push({ ledger, transaction_id: transactionId, record: { ...record, id } });
  }
  const rewrite = (value, ledger) => {
    if (typeof value === "string") {
      return mapping.get(`${ledger}:${value}`) || value;
    }
    if (Array.isArray(value)) return value.map(child => rewrite(child, ledger));
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, rewrite(child, ledger)]));
    return value;
  };
  for (const item of normalized) item.record = rewrite(item.record, item.ledger);
  const patchSizes = new Map();
  for (const item of normalized) {
    const config = ledgerConfigs.get(item.ledger) || {};
    const maxRecordBytes = (config.maxRecordKB || DEFAULT_MAX_RECORD_BYTES / 1024) * 1024;
    const maxPatchBytes = (config.maxPatchKB || DEFAULT_MAX_PATCH_BYTES / 1024) * 1024;
    if (!Number.isSafeInteger(maxRecordBytes) || maxRecordBytes < 1 || !Number.isSafeInteger(maxPatchBytes) || maxPatchBytes < 1) {
      throw new TypeError("Invalid ledger size limits");
    }
    const bytes = Buffer.byteLength(`${JSON.stringify({ transaction_id: item.transaction_id, record: item.record })}\n`, "utf8");
    if (bytes > maxRecordBytes) throw new RangeError("Ledger record exceeds configured record-size limit");
    const nextPatchSize = (patchSizes.get(item.ledger) || 0) + bytes;
    if (nextPatchSize > maxPatchBytes) throw new RangeError("Ledger append exceeds configured patch-size limit");
    patchSizes.set(item.ledger, nextPatchSize);
  }
  return { version: 1, transaction_id: transactionId, appends: normalized };
}

module.exports = { normalizeLedgerAppends };
