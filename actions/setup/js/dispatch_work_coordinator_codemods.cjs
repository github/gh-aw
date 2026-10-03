// @ts-check
"use strict";

const CURRENT_VERSION = 3;

// Keep each successive protocol transformation as data so historical upgrades
// remain inspectable and deterministic. An absent version is the original log.
const CODEMODS = Object.freeze([
  Object.freeze({ from: 0, to: 1, rename: Object.freeze({}), set: Object.freeze({ version: 1 }) }),
  Object.freeze({ from: 1, to: 2, rename: Object.freeze({ work: "work_id", claim: "claim_id", attempt: "attempt_id" }), set: Object.freeze({ version: 2 }) }),
  Object.freeze({ from: 2, to: 3, rename: Object.freeze({}), set: Object.freeze({ version: 3 }) }),
]);

function applyCodemods(message, version) {
  let upgraded = { ...message };
  for (let next = version; next < CURRENT_VERSION; next++) {
    const codemod = CODEMODS.find(item => item.from === next);
    if (!codemod || codemod.to !== next + 1) throw new TypeError("missing dispatch coordinator transaction codemod");
    for (const [from, to] of Object.entries(codemod.rename)) {
      if (upgraded[from] !== null) upgraded[to] = upgraded[from];
      delete upgraded[from];
    }
    upgraded = { ...upgraded, ...codemod.set };
  }
  return upgraded;
}

function upgradeTransaction(message) {
  if (!message || typeof message !== "object" || Array.isArray(message)) {
    throw new TypeError("transaction must be an object");
  }
  const version = Object.hasOwn(message, "version") ? message.version : 0;
  if (!Number.isSafeInteger(version) || version < 0 || version > CURRENT_VERSION) {
    throw new TypeError("unsupported dispatch coordinator transaction version");
  }
  if (version === CURRENT_VERSION) return message;
  if (Object.hasOwn(message, "work_id")) {
    if (version === 0) return { ...message, version: CURRENT_VERSION };
    if (version === 2) return applyCodemods(message, version);
    throw new TypeError("unsupported dispatch coordinator transaction version");
  }
  if (version > 1) throw new TypeError("versioned dispatch coordinator transactions require work_id");
  const fields = Object.keys(message)
    .filter(field => field !== "version")
    .sort();
  const expected = ["attempt", "claim", "kind", "work"];
  if (fields.length !== expected.length || fields.some((field, index) => field !== expected[index])) {
    throw new TypeError("legacy transaction must contain exactly version, kind, work, claim, and attempt");
  }
  if (["Work", "WorkCancellation"].includes(message.kind) && (message.claim !== null || message.attempt !== null)) {
    throw new TypeError("legacy Work must not include a claim or attempt");
  }
  if (["Claim", "ClaimCancellation"].includes(message.kind) && message.attempt !== null) {
    throw new TypeError("legacy Claim must not include an attempt");
  }
  const upgraded = applyCodemods(message, version);
  if (upgraded.kind === "Work") upgraded.work = { legacy_work_id: upgraded.work_id };
  if (upgraded.kind === "Claim") upgraded.run_id = `legacy:${upgraded.claim_id}`;
  return upgraded;
}

function upgradeTransactions(messages) {
  const upgraded = messages.map(upgradeTransaction);
  const sequences = new Map();
  let maximum = 0;
  for (const message of upgraded) {
    if (message.kind === "Work" && message.sequence !== undefined) {
      if (!Number.isSafeInteger(message.sequence) || message.sequence < 1) throw new TypeError("work sequence must be a positive safe integer");
      maximum = Math.max(maximum, message.sequence);
      sequences.set(message.work_id, message.sequence);
    }
  }
  return upgraded.map((message, index) => {
    if (message.kind !== "Work" || message.sequence !== undefined) return message;
    if (messages[index].version >= 2) throw new TypeError("work sequence is required");
    if (!sequences.has(message.work_id)) {
      if (maximum === Number.MAX_SAFE_INTEGER) throw new RangeError("work queue sequence exhausted");
      sequences.set(message.work_id, ++maximum);
    }
    return { ...message, sequence: sequences.get(message.work_id) };
  });
}

module.exports = { CURRENT_VERSION, CODEMODS, upgradeTransaction, upgradeTransactions };
