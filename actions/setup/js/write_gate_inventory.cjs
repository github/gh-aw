// @ts-check

const fs = require("node:fs");
const { createHash } = require("node:crypto");
const { DECISIONS, parseItemDecisions, restrictiveDecision } = require("./write_gate_decisions.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { parseStrictJSON } = require("./work_queue_codec.cjs");

const WRITE_DECISION_SCHEMA = "gh-aw/write-decisions/v1";
const DIGEST_PATTERN = /^[a-f0-9]{64}$/;
const OUTPUT_ID_PATTERN = /^(?:so_[A-Za-z0-9_-]{12}|job_[a-f0-9]{16})$/;
const MAX_INVENTORY_BYTES = 16 * 1024 * 1024;

/**
 * @typedef {{output_id: string, item_sha256: string, input_sha256: string, complete: boolean, depends_on: string[], publication?: {request_ids?: string[]}}} PendingDecisionItem
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
  const items = manifest.items.map(item => {
    const selection = !gateAllowed
      ? { decision: "drop", reason: verdict.decision === "deny" && /^[a-z0-9_.-]{1,64}$/.test(verdict.reason) ? verdict.reason : "gate_denied" }
      : item.complete !== true
        ? { decision: "drop", reason: "gate_item_incomplete" }
        : verdict.decision === "allow"
          ? { decision: "apply", reason: verdict.reason }
          : parsed.get(item.output_id) || { decision: "drop", reason: "gate_item_missing" };
    return { output_id: item.output_id, item_sha256: item.item_sha256, input_sha256: item.input_sha256, gate_decision: selection.decision, decision_reason: selection.reason };
  });
  const authorized = new Map(items.map(item => [item.output_id, item]));
  const planned = new Map(manifest.items.map(item => [item.output_id, item]));
  for (const item of items) {
    const requests = planned.get(item.output_id)?.publication?.request_ids;
    if (requests === undefined) continue;
    if (!Array.isArray(requests) || new Set(requests).size !== requests.length || requests.some(id => typeof id !== "string" || id === item.output_id || !planned.has(id) || planned.get(id)?.publication)) {
      item.gate_decision = "drop";
      item.decision_reason = "gate_request_invalid";
      warn(`Publication ${item.output_id} has an invalid source request; it was dropped`);
      continue;
    }
    for (const id of requests) {
      const request = authorized.get(id);
      const decision = restrictiveDecision(item.gate_decision, request?.gate_decision);
      if (decision !== item.gate_decision) {
        item.gate_decision = decision;
        item.decision_reason = "gate_request_restricted";
      }
    }
  }
  return {
    schema: WRITE_DECISION_SCHEMA,
    manifest_sha256: manifestSha256,
    decision: !gateAllowed || (items.length > 0 && items.every(item => item.gate_decision === "drop")) ? "deny" : verdict.decision === "allow" && items.some(item => item.gate_decision !== "apply") ? "partial" : verdict.decision,
    items,
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
    typeof inventory.decision !== "string" ||
    !["allow", "partial", "deny"].includes(inventory.decision) ||
    !("items" in inventory) ||
    !Array.isArray(inventory.items) ||
    Object.keys(inventory).some(key => !["schema", "manifest_sha256", "decision", "items"].includes(key))
  ) {
    throw new Error(`${ERR_VALIDATION}: Malformed normalized write decisions`);
  }
  const known = new Set();
  for (const item of inventory.items) {
    if (
      !item ||
      typeof item !== "object" ||
      typeof item.output_id !== "string" ||
      !OUTPUT_ID_PATTERN.test(item.output_id) ||
      known.has(item.output_id) ||
      typeof item.item_sha256 !== "string" ||
      !DIGEST_PATTERN.test(item.item_sha256) ||
      typeof item.input_sha256 !== "string" ||
      !DIGEST_PATTERN.test(item.input_sha256) ||
      !DECISIONS.includes(item.gate_decision) ||
      typeof item.decision_reason !== "string" ||
      !/^[a-z0-9_.-]{1,64}$/.test(item.decision_reason) ||
      Object.keys(item).some(key => !["output_id", "item_sha256", "input_sha256", "gate_decision", "decision_reason"].includes(key))
    ) {
      throw new Error(`${ERR_VALIDATION}: Malformed normalized write decision item`);
    }
    if (inventory.decision === "deny" && item.gate_decision !== "drop") throw new Error(`${ERR_VALIDATION}: Denied gate cannot authorize an item`);
    if (inventory.decision === "allow" && item.gate_decision !== "apply") throw new Error(`${ERR_VALIDATION}: Narrowed items require a partial normalized decision`);
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
  } catch (cause) {
    throw new Error(`${ERR_VALIDATION}: Failed to persist normalized write decisions`, { cause });
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
  const descriptor = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  let bytes;
  try {
    const stat = fs.fstatSync(descriptor);
    if (!stat.isFile() || stat.size > MAX_INVENTORY_BYTES) throw new Error(`${ERR_VALIDATION}: Invalid write decision inventory file`);
    bytes = fs.readFileSync(descriptor);
    if (bytes.length !== stat.size || bytes.length > MAX_INVENTORY_BYTES) throw new Error(`${ERR_VALIDATION}: Write decision inventory changed during read`);
  } catch (cause) {
    throw new Error(`${ERR_VALIDATION}: Failed to read normalized write decisions`, { cause });
  } finally {
    fs.closeSync(descriptor);
  }
  if (createHash("sha256").update(bytes).digest("hex") !== expectedSha256) throw new Error(`${ERR_VALIDATION}: Write decision inventory digest mismatch`);
  let inventory;
  try {
    inventory = parseStrictJSON(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes), { maxBytes: MAX_INVENTORY_BYTES });
  } catch (cause) {
    throw new Error(`${ERR_VALIDATION}: Malformed normalized write decisions`, { cause });
  }
  validateWriteDecisionInventory(inventory);
  if (inventory.manifest_sha256 !== manifestSha256) throw new Error(`${ERR_VALIDATION}: Write decision pending manifest mismatch`);
  return inventory;
}

module.exports = { WRITE_DECISION_SCHEMA, OUTPUT_ID_PATTERN, buildWriteDecisionInventory, validateWriteDecisionInventory, writeDecisionInventory, readDecisionInventory };
