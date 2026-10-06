// @ts-check
"use strict";

const CURRENT_VERSION = 3;
const CODEMODS = Object.freeze([]);

function upgradeTransaction() {
  throw new Error("unsupported_protocol: work queue records are never upgraded; initialize an explicit version-3 queue after quiescing old writers");
}

module.exports = { CURRENT_VERSION, CODEMODS, upgradeTransaction };
