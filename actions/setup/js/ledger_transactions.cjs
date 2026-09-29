// @ts-check
"use strict";

const crypto = require("node:crypto");

const TEMPORARY_ID = /^#?[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;

function finalId(transactionId, index) {
  const hex = crypto.createHash("sha256").update(`${transactionId}:${index}`).digest("hex");
  return `ldg-${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-${((parseInt(hex.slice(16, 18), 16) & 0x3f) | 0x80).toString(16).padStart(2, "0")}${hex.slice(18, 20)}-${hex.slice(20, 32)}`;
}

/**
 * Normalize safe-output ledger append requests without touching canonical state.
 * @param {Array<{ledger?: string, temp_id?: string, record: object}>} requests
 * @param {{transactionId: string, ledgerNames: Set<string>}} options
 */
function normalizeLedgerAppends(requests, { transactionId, ledgerNames }) {
  if (!Array.isArray(requests) || requests.length > 100) throw new RangeError("Invalid ledger append batch");
  const mapping = new Map();
  const normalized = [];
  for (const [index, request] of requests.entries()) {
    if (!request || typeof request !== "object" || !request.record || typeof request.record !== "object") throw new TypeError("Invalid ledger append request");
    const ledger = request.ledger || (ledgerNames.size === 1 ? [...ledgerNames][0] : undefined);
    if (!ledger || !ledgerNames.has(ledger)) throw new TypeError("Unknown target ledger");
    if (request.temp_id !== undefined && (typeof request.temp_id !== "string" || !TEMPORARY_ID.test(request.temp_id) || mapping.has(`${ledger}:${request.temp_id}`))) {
      throw new TypeError("Invalid or duplicate temporary ID");
    }
    const id = finalId(transactionId, index);
    if (request.temp_id) mapping.set(`${ledger}:${request.temp_id}`, id);
    normalized.push({ ledger, transaction_id: transactionId, record: { ...request.record, id } });
  }
  const rewrite = value => {
    if (typeof value === "string") {
      for (const [key, id] of mapping) if (value === key.slice(key.indexOf(":") + 1)) return id;
      return value;
    }
    if (Array.isArray(value)) return value.map(rewrite);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, rewrite(child)]));
    return value;
  };
  for (const item of normalized) item.record = rewrite(item.record);
  return { version: 1, transaction_id: transactionId, appends: normalized };
}

module.exports = { normalizeLedgerAppends };
