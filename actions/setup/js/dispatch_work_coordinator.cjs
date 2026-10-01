// @ts-check
"use strict";

const crypto = require("node:crypto");

const MAX_LOG_BYTES = 10 * 1024 * 1024;
const MAX_TRANSACTION_BYTES = 64 * 1024;
const MAX_TRANSACTIONS = 100000;
const MAX_VALUE_DEPTH = 32;
const AUTHORITY_FIELDS = new Set(["work_id", "claim_id", "claimant", "generation"]);
const TRANSACTION_FIELDS = {
  Work: new Set(["type", "work_id", "work"]),
  Claim: new Set(["type", "claim_id", "work_id", "run_id", "workflow_id"]),
  ClaimCancellation: new Set(["type", "claim_id"]),
  Completion: new Set(["type", "claim_id", "outcome"]),
  WorkCancellation: new Set(["type", "work_id"]),
};

function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map(key => `${JSON.stringify(key)}:${canonicalJSON(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function hash(value) {
  return crypto.createHash("sha256").update(value).digest("hex");
}

function compareCanonical(left, right) {
  const a = canonicalJSON(left);
  const b = canonicalJSON(right);
  return a < b ? -1 : a > b ? 1 : 0;
}

function validateJSONValue(value, depth = 0) {
  if (depth > MAX_VALUE_DEPTH) throw new RangeError("Coordinator data exceeds maximum nesting depth");
  if (value === null || typeof value === "string" || typeof value === "boolean") return;
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("Coordinator data contains a non-finite number");
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) validateJSONValue(item, depth + 1);
    return;
  }
  if (!value || typeof value !== "object" || (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null)) {
    throw new TypeError("Coordinator data must contain only JSON values");
  }
  for (const [key, child] of Object.entries(value)) {
    if (key === "__proto__" || key === "constructor" || key === "prototype") throw new TypeError("Coordinator data contains a reserved property");
    validateJSONValue(child, depth + 1);
  }
}

function validateId(value, label) {
  if (typeof value !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(value)) {
    throw new TypeError(`Invalid ${label}`);
  }
}

function deriveWorkId(work) {
  validateJSONValue(work);
  if (!work || typeof work !== "object" || Array.isArray(work)) throw new TypeError("Work must be a JSON object");
  const canonical = canonicalJSON(work);
  if (Buffer.byteLength(canonical, "utf8") > MAX_TRANSACTION_BYTES - 256) throw new RangeError("Work exceeds the transaction-size limit");
  return `work-${hash(canonical)}`;
}

function validateTransaction(transaction) {
  if (!transaction || typeof transaction !== "object" || Array.isArray(transaction)) throw new TypeError("Invalid coordinator transaction");
  const fields = TRANSACTION_FIELDS[transaction.type];
  if (!fields || Object.keys(transaction).some(key => !fields.has(key))) throw new TypeError("Invalid coordinator transaction fields");
  for (const field of fields) {
    if (field !== "outcome" && !Object.hasOwn(transaction, field)) throw new TypeError("Incomplete coordinator transaction");
  }

  switch (transaction.type) {
    case "Work":
      validateId(transaction.work_id, "Work ID");
      if (!transaction.work || typeof transaction.work !== "object" || Array.isArray(transaction.work)) throw new TypeError("Work payload must be a JSON object");
      validateJSONValue(transaction.work);
      if (deriveWorkId(transaction.work) !== transaction.work_id) throw new TypeError("Work ID does not match its canonical payload");
      break;
    case "Claim":
      validateId(transaction.claim_id, "Claim ID");
      validateId(transaction.work_id, "Work ID");
      validateId(transaction.run_id, "workflow run ID");
      if (typeof transaction.workflow_id !== "string" || transaction.workflow_id.length > 512) throw new TypeError("Invalid workflow ID");
      break;
    case "ClaimCancellation":
      validateId(transaction.claim_id, "Claim ID");
      break;
    case "Completion":
      validateId(transaction.claim_id, "Claim ID");
      if (Object.hasOwn(transaction, "outcome")) {
        validateJSONValue(transaction.outcome);
        if (!transaction.outcome || typeof transaction.outcome !== "object" || Array.isArray(transaction.outcome)) {
          throw new TypeError("Completion outcome must be a JSON object");
        }
        validateOutcomeAuthorityFields(transaction.outcome);
      }
      break;
    case "WorkCancellation":
      validateId(transaction.work_id, "Work ID");
      break;
    default:
      throw new TypeError("Unknown coordinator transaction type");
  }

  function validateOutcomeAuthorityFields(value) {
    if (Array.isArray(value)) {
      for (const item of value) validateOutcomeAuthorityFields(item);
      return;
    }
    if (!value || typeof value !== "object") return;
    for (const [key, child] of Object.entries(value)) {
      if (AUTHORITY_FIELDS.has(key)) throw new TypeError("Completion outcome cannot contain Claim authority fields");
      validateOutcomeAuthorityFields(child);
    }
  }

  if (Buffer.byteLength(canonicalJSON(transaction), "utf8") > MAX_TRANSACTION_BYTES) throw new RangeError("Coordinator transaction exceeds the size limit");
  return structuredClone(transaction);
}

function parseTransactionLog(contents) {
  if (typeof contents !== "string" || Buffer.byteLength(contents, "utf8") > MAX_LOG_BYTES) {
    throw new RangeError("Coordinator transaction log exceeds the size limit");
  }
  if (contents === "") return [];
  const lines = contents.split("\n");
  if (lines.at(-1) === "") lines.pop();
  if (lines.length > MAX_TRANSACTIONS) throw new RangeError("Coordinator transaction log contains too many records");
  if (lines.some(line => line.trim() === "")) throw new TypeError("Coordinator transaction log contains an empty record");
  return lines.map(line => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      throw new TypeError("Coordinator transaction log contains malformed JSON");
    }
    return validateTransaction(parsed);
  });
}

function stableTransactions(transactions) {
  const byContent = new Map();
  for (const transaction of transactions) {
    const valid = validateTransaction(transaction);
    byContent.set(canonicalJSON(valid), valid);
  }
  return [...byContent.values()].sort(compareCanonical);
}

function replayTransactions(input) {
  if (!Array.isArray(input)) throw new TypeError("Coordinator transactions must be an array");
  const transactions = stableTransactions(input);
  const works = new Map();
  const claims = new Map();
  const cancellations = new Set();
  const workCancellations = new Set();
  const completions = new Map();

  for (const transaction of transactions) {
    switch (transaction.type) {
      case "Work": {
        const existing = works.get(transaction.work_id);
        if (existing && canonicalJSON(existing.work) !== canonicalJSON(transaction.work)) throw new TypeError("Conflicting Work transactions");
        works.set(transaction.work_id, transaction);
        break;
      }
      case "Claim": {
        const existing = claims.get(transaction.claim_id);
        if (existing && canonicalJSON(existing) !== canonicalJSON(transaction)) throw new TypeError("Conflicting Claim transactions");
        claims.set(transaction.claim_id, transaction);
        break;
      }
      case "ClaimCancellation":
        cancellations.add(transaction.claim_id);
        break;
      case "WorkCancellation":
        workCancellations.add(transaction.work_id);
        break;
      case "Completion": {
        const existing = completions.get(transaction.claim_id);
        if (existing && canonicalJSON(existing) !== canonicalJSON(transaction)) throw new TypeError("Conflicting Completion transactions");
        completions.set(transaction.claim_id, transaction);
        break;
      }
    }
  }

  for (const claim of claims.values()) {
    if (!works.has(claim.work_id)) throw new TypeError("Claim references unknown Work");
  }
  for (const workId of workCancellations) {
    if (!works.has(workId)) throw new TypeError("WorkCancellation references unknown Work");
  }
  for (const claimId of cancellations) {
    if (!claims.has(claimId)) throw new TypeError("ClaimCancellation references unknown Claim");
  }
  for (const claimId of completions.keys()) {
    if (!claims.has(claimId)) throw new TypeError("Completion references unknown Claim");
  }

  const claimsByWork = new Map();
  for (const claim of claims.values()) {
    if (!claimsByWork.has(claim.work_id)) claimsByWork.set(claim.work_id, []);
    claimsByWork.get(claim.work_id).push(claim);
  }

  const projection = [];
  let completedCount = 0;
  let cancelledCount = 0;
  let activeClaimCount = 0;
  let outstandingCount = 0;
  for (const [workId, transaction] of [...works.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))) {
    const workClaims = (claimsByWork.get(workId) || []).slice().sort((a, b) => (a.claim_id < b.claim_id ? -1 : a.claim_id > b.claim_id ? 1 : 0));
    const liveClaims = workClaims.filter(claim => !cancellations.has(claim.claim_id));
    const effectiveClaim = liveClaims[0];
    const completion = effectiveClaim ? completions.get(effectiveClaim.claim_id) : undefined;
    const cancelled = workCancellations.has(workId);

    for (const claim of workClaims) {
      if (completions.has(claim.claim_id) && claim !== effectiveClaim) {
        throw new TypeError("Completion references a non-effective Claim");
      }
    }
    if (cancelled && completion) throw new TypeError("Work cannot be both completed and cancelled");

    const state = cancelled ? "cancelled" : completion ? "completed" : effectiveClaim ? "claimed" : "available";
    if (state === "cancelled") cancelledCount++;
    if (state === "completed") completedCount++;
    if (state === "claimed") activeClaimCount++;
    if (state === "available" || state === "claimed") outstandingCount++;

    projection.push({
      work_id: workId,
      work: structuredClone(transaction.work),
      state,
      effective_claim_id: cancelled ? null : effectiveClaim?.claim_id || null,
      claims: workClaims.map(claim => ({
        claim_id: claim.claim_id,
        run_id: claim.run_id,
        workflow_id: claim.workflow_id,
        state: cancellations.has(claim.claim_id) ? "cancelled" : !cancelled && claim === effectiveClaim ? "effective" : "superseded",
      })),
      completion: completion ? structuredClone(completion.outcome ?? null) : null,
    });
  }

  return {
    works: projection,
    counts: {
      available: projection.filter(work => work.state === "available").length,
      claimed: projection.filter(work => work.state === "claimed").length,
      completed: completedCount,
      cancelled: cancelledCount,
      outstanding: outstandingCount,
      active_claims: activeClaimCount,
      terminal: completedCount + cancelledCount,
    },
  };
}

function serializeTransactions(transactions) {
  const content = stableTransactions(transactions).map(canonicalJSON).join("\n") + (transactions.length ? "\n" : "");
  if (Buffer.byteLength(content, "utf8") > MAX_LOG_BYTES) throw new RangeError("Coordinator transaction log exceeds the size limit");
  return content;
}

function compactTransactions(transactions) {
  const compacted = stableTransactions(transactions);
  if (canonicalJSON(replayTransactions(transactions)) !== canonicalJSON(replayTransactions(compacted))) {
    throw new Error("Compacted coordinator log changed the projection");
  }
  return compacted;
}

module.exports = {
  AUTHORITY_FIELDS,
  compactTransactions,
  deriveWorkId,
  parseTransactionLog,
  replayTransactions,
  serializeTransactions,
  validateTransaction,
};
