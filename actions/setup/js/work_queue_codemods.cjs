// @ts-check
"use strict";

const CURRENT_VERSION = 2;

// Keep each successive protocol transformation as data so historical upgrades
// remain inspectable and deterministic. An absent version is the original log.
const CODEMODS = Object.freeze([Object.freeze({ from: 0, to: 1, set: Object.freeze({ version: 1 }) }), Object.freeze({ from: 1, to: 2, set: Object.freeze({ version: 2 }) })]);

const HISTORICAL_FIELDS = Object.freeze(["kind", "work", "claim", "attempt"]);

function upgradeTransaction(message) {
  if (!message || typeof message !== "object" || Array.isArray(message)) {
    throw new TypeError("transaction must be an object");
  }
  const version = Object.hasOwn(message, "version") ? message.version : 0;
  if (!Number.isSafeInteger(version) || version < 0 || version > CURRENT_VERSION) {
    throw new TypeError("unsupported work queue transaction version");
  }
  // Historical versions are closed shapes. Do not legitimize v2 metadata by
  // relabeling an invalid historical record, or invent an enqueue timestamp.
  if (version < CURRENT_VERSION) {
    const expectedFields = Object.hasOwn(message, "version") ? ["version", ...HISTORICAL_FIELDS] : HISTORICAL_FIELDS;
    const fields = Object.keys(message);
    if (fields.length !== expectedFields.length || fields.some(field => !expectedFields.includes(field))) {
      throw new TypeError("historical transaction must contain exactly version (if present), kind, work, claim, and attempt");
    }
  }
  let upgraded = message;
  for (let next = version; next < CURRENT_VERSION; next++) {
    const codemod = CODEMODS.find(item => item.from === next);
    if (!codemod || codemod.to !== next + 1) {
      throw new TypeError("missing work queue transaction codemod");
    }
    upgraded = { ...upgraded, ...codemod.set };
  }
  return upgraded;
}

module.exports = { CURRENT_VERSION, CODEMODS, upgradeTransaction };
