// @ts-check

const fs = require("node:fs");
const { createHash } = require("node:crypto");
const { DECISIONS, parseItemDecisions } = require("./write_gate_decisions.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

const WRITE_DECISION_SCHEMA = "gh-aw/write-decisions/v1";
const DIGEST_PATTERN = /^[a-f0-9]{64}$/;
const OUTPUT_ID_PATTERN = /^so_[A-Za-z0-9_-]{12}$/;
const MAX_INVENTORY_BYTES = 16 * 1024 * 1024;

/**
 * @typedef {{output_id: string, item_sha256: string, input_sha256: string, complete: boolean, depends_on: string[]}} PendingDecisionItem
 * @typedef {{output_id: string, item_sha256: string, input_sha256: string, gate_decision: string, decision_reason: string}} DecisionInventoryItem
 * @typedef {{schema: string, manifest_sha256: string, decision: string, items: DecisionInventoryItem[]}} DecisionInventory
 */

/**
 * Capture gate authorization independently of handler results. Existing runtime
 * policies may only narrow these decisions later; the inventory never claims an
 * actual effect. The caller must verify the pending manifest before invoking it.
 *
 * @param {{items: PendingDecisionItem[]}} manifest
 * @param {string} manifestSha256
 * @param {{decision: string, reason: string, items?: unknown}} verdict
 * @param {(message: string) => void} warn
 * @returns {DecisionInventory}
 */
function buildWriteDecisionInventory(manifest, manifestSha256, verdict, warn) {
  if (!DIGEST_PATTERN.test(manifestSha256) || !Array.isArray(manifest.items)) throw new Error(`${ERR_VALIDATION}: Invalid pending manifest for write decisions`);
  const known = new Set();
  for (const item of manifest.items) {
    if (!OUTPUT_ID_PATTERN.test(item.output_id) || known.has(item.output_id) || !DIGEST_PATTERN.test(item.item_sha256) || !DIGEST_PATTERN.test(item.input_sha256)) {
      throw new Error(`${ERR_VALIDATION}: Invalid or duplicate pending item identity for write decisions`);
    }
    known.add(item.output_id);
  }
  const gateAllowed = ["allow", "partial"].includes(verdict.decision) && /^[a-z0-9_.-]{1,64}$/.test(verdict.reason);
  const parsed = gateAllowed && verdict.decision === "partial" ? parseItemDecisions(verdict.items, known, warn) : new Map();
  return {
    schema: WRITE_DECISION_SCHEMA,
    manifest_sha256: manifestSha256,
    decision: gateAllowed ? verdict.decision : "deny",
    items: manifest.items.map(item => {
      const selection = !gateAllowed
        ? { decision: "drop", reason: "gate_denied" }
        : item.complete !== true
          ? { decision: "drop", reason: "gate_item_incomplete" }
          : verdict.decision === "allow"
            ? { decision: "apply", reason: verdict.reason }
            : parsed.get(item.output_id) || { decision: "drop", reason: "gate_item_missing" };
      return { output_id: item.output_id, item_sha256: item.item_sha256, input_sha256: item.input_sha256, gate_decision: selection.decision, decision_reason: selection.reason };
    }),
  };
}

/** @param {unknown} inventory @returns {asserts inventory is DecisionInventory} */
function validateWriteDecisionInventory(inventory) {
  if (
    !inventory ||
    typeof inventory !== "object" ||
    !("schema" in inventory) ||
    inventory.schema !== WRITE_DECISION_SCHEMA ||
    !("manifest_sha256" in inventory) ||
    typeof inventory.manifest_sha256 !== "string" ||
    !DIGEST_PATTERN.test(inventory.manifest_sha256) ||
    !("decision" in inventory) ||
    !["allow", "partial", "deny"].includes(String(inventory.decision)) ||
    !("items" in inventory) ||
    !Array.isArray(inventory.items)
  ) {
    throw new Error(`${ERR_VALIDATION}: Malformed normalized write decisions`);
  }
  const known = new Set();
  for (const item of inventory.items) {
    if (
      !item ||
      typeof item !== "object" ||
      !OUTPUT_ID_PATTERN.test(item.output_id) ||
      known.has(item.output_id) ||
      !DIGEST_PATTERN.test(item.item_sha256) ||
      !DIGEST_PATTERN.test(item.input_sha256) ||
      !DECISIONS.includes(item.gate_decision) ||
      typeof item.decision_reason !== "string" ||
      !/^[a-z0-9_.-]{1,64}$/.test(item.decision_reason)
    ) {
      throw new Error(`${ERR_VALIDATION}: Malformed normalized write decision item`);
    }
    if (inventory.decision === "deny" && item.gate_decision !== "drop") throw new Error(`${ERR_VALIDATION}: Denied gate cannot authorize an item`);
    known.add(item.output_id);
  }
}

/**
 * Exclusive creation prevents silently replacing policy after processing starts.
 * @param {string} filename
 * @param {DecisionInventory} inventory
 * @returns {string} SHA-256 of the exact persisted bytes
 */
function writeDecisionInventory(filename, inventory) {
  validateWriteDecisionInventory(inventory);
  const bytes = Buffer.from(JSON.stringify(inventory), "utf8");
  if (bytes.length > MAX_INVENTORY_BYTES) throw new Error(`${ERR_VALIDATION}: Write decision inventory exceeds 16 MiB`);
  const descriptor = fs.openSync(filename, "wx", 0o600);
  try {
    fs.writeFileSync(descriptor, bytes);
  } finally {
    fs.closeSync(descriptor);
  }
  return createHash("sha256").update(bytes).digest("hex");
}

/**
 * Missing files, malformed policy and digest mismatches throw before any writes.
 * Expected digests must come from compiler-owned completed-job outputs.
 * @param {string} filename
 * @param {string} expectedSha256
 * @param {string} manifestSha256
 * @returns {DecisionInventory}
 */
function readDecisionInventory(filename, expectedSha256, manifestSha256) {
  if (!DIGEST_PATTERN.test(expectedSha256) || !DIGEST_PATTERN.test(manifestSha256)) throw new Error(`${ERR_VALIDATION}: Missing write decision integrity pin`);
  const descriptor = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  let bytes;
  try {
    const stat = fs.fstatSync(descriptor);
    if (!stat.isFile() || stat.size > MAX_INVENTORY_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid write decision inventory file`);
    bytes = fs.readFileSync(descriptor);
    if (bytes.length !== stat.size || bytes.length > MAX_INVENTORY_BYTES) throw new Error(`${ERR_VALIDATION}: Write decision inventory changed during read`);
  } finally {
    fs.closeSync(descriptor);
  }
  if (createHash("sha256").update(bytes).digest("hex") !== expectedSha256) throw new Error(`${ERR_VALIDATION}: Write decision inventory digest mismatch`);
  const inventory = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  validateWriteDecisionInventory(inventory);
  if (inventory.manifest_sha256 !== manifestSha256) throw new Error(`${ERR_VALIDATION}: Write decision pending manifest mismatch`);
  return inventory;
}

module.exports = { WRITE_DECISION_SCHEMA, buildWriteDecisionInventory, validateWriteDecisionInventory, writeDecisionInventory, readDecisionInventory };
