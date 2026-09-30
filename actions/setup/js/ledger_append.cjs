// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { normalizeLedgerAppends, normalizeLedgerCompactions } = require("./ledger_transactions.cjs");

const requests = { appends: [], compactions: [] };
let ledgerConfigs = {};

function transactionPath() {
  return path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw", "ledger-transactions.json");
}

function configure(config = {}) {
  const ledgers = Array.isArray(config.ledgers) ? config.ledgers : [];
  ledgerConfigs = Object.fromEntries(ledgers.map(ledger => [ledger.name, ledger]));
  const ledgerNames = new Set(Object.keys(ledgerConfigs));
  if (!ledgerNames.size) throw new TypeError("Ledger safe output has no configured ledgers");
}

function finalize() {
  try {
    const transactionId = `${process.env.GITHUB_RUN_ID || "local"}:${process.env.GITHUB_RUN_ATTEMPT || "1"}`;
    const ledgerNames = new Set(Object.keys(ledgerConfigs));
    const transaction = normalizeLedgerAppends(requests.appends, { transactionId, ledgerNames, ledgers: ledgerConfigs });
    const patchBytes = new Map();
    for (const append of transaction.appends) patchBytes.set(append.ledger, (patchBytes.get(append.ledger) || 0) + Buffer.byteLength(JSON.stringify(append.record)));
    const compactions = normalizeLedgerCompactions(requests.compactions, { transactionId, ledgerNames, ledgers: ledgerConfigs, startIndex: transaction.appends.length, startPatchBytes: patchBytes });
    const artifact = { version: 1, transaction_id: transaction.transaction_id, ledgers: {} };
    for (const append of transaction.appends) {
      artifact.ledgers[append.ledger] ||= { appends: [], compactions: [] };
      artifact.ledgers[append.ledger].appends.push(append);
    }
    for (const compact of compactions) {
      artifact.ledgers[compact.ledger] ||= { appends: [], compactions: [] };
      artifact.ledgers[compact.ledger].compactions.push(compact);
    }
    const file = transactionPath();
    const directory = path.dirname(file);
    try {
      fs.mkdirSync(directory, { recursive: true });
    } catch (error) {
      throw new Error("Failed to create ledger artifact directory", { cause: error });
    }
    if (!fs.lstatSync(directory).isDirectory()) throw new TypeError("Ledger artifact directory must be a real directory");
    const fd = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
    try {
      try {
        fs.writeFileSync(fd, `${JSON.stringify(artifact)}\n`);
      } catch (error) {
        throw new Error("Failed to write ledger transaction artifact", { cause: error });
      }
    } finally {
      fs.closeSync(fd);
    }
  } finally {
    requests.appends.length = 0;
    requests.compactions.length = 0;
  }
}

async function main(config = {}) {
  configure(config);
  const handleLedgerAppend = async message => {
    requests.appends.push(message);
    return { success: true, queued: true };
  };
  handleLedgerAppend.finalize = finalize;

  return handleLedgerAppend;
}

async function compact(config = {}) {
  configure(config);
  const handleLedgerCompact = async message => {
    requests.compactions.push(message);
    return { success: true, queued: true };
  };
  handleLedgerCompact.finalize = finalize;
  return handleLedgerCompact;
}

module.exports = { main, compact, transactionPath };
