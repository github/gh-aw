// @ts-check
"use strict";

const CURRENT_VERSION = 1;

// Keep each successive protocol transformation as data so historical upgrades
// remain inspectable and deterministic. An absent version is the original log.
const CODEMODS = Object.freeze([Object.freeze({ from: 0, to: 1, set: Object.freeze({ version: 1 }) })]);

function upgradeTransaction(message) {
  if (!message || typeof message !== "object" || Array.isArray(message)) {
    throw new TypeError("transaction must be an object");
  }
  const version = Object.hasOwn(message, "version") ? message.version : 0;
  if (!Number.isSafeInteger(version) || version < 0 || version > CURRENT_VERSION) {
    throw new TypeError("unsupported dispatch coordinator transaction version");
  }
  let upgraded = message;
  for (let next = version; next < CURRENT_VERSION; next++) {
    const codemod = CODEMODS.find(item => item.from === next);
    if (!codemod || codemod.to !== next + 1) {
      throw new TypeError("missing dispatch coordinator transaction codemod");
    }
    upgraded = { ...upgraded, ...codemod.set };
  }
  return upgraded;
}

module.exports = { CURRENT_VERSION, CODEMODS, upgradeTransaction };
