// @ts-check
"use strict";

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { normalizeLedgerAppends } = require("./ledger_transactions.cjs");

function transactionPath() {
  return path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw", "ledger-transactions.json");
}

async function main(config = {}) {
  const ledgers = Array.isArray(config.ledgers) ? config.ledgers : [];
  const ledgerConfigs = Object.fromEntries(ledgers.map(ledger => [ledger.name, ledger]));
  const ledgerNames = new Set(Object.keys(ledgerConfigs));
  if (!ledgerNames.size) throw new TypeError("ledger_append has no configured ledgers");

  const requests = [];
  const handleLedgerAppend = async message => {
    requests.push(message);
    return { success: true, queued: true };
  };

  handleLedgerAppend.finalize = () => {
    const transactionId = `${process.env.GITHUB_RUN_ID || "local"}:${process.env.GITHUB_RUN_ATTEMPT || "1"}`;
    const transaction = normalizeLedgerAppends(requests, { transactionId, ledgerNames, ledgers: ledgerConfigs });
    const artifact = { version: 1, transaction_id: transaction.transaction_id, ledgers: {} };
    for (const append of transaction.appends) {
      artifact.ledgers[append.ledger] ||= { appends: [] };
      artifact.ledgers[append.ledger].appends.push(append);
    }
    const file = transactionPath();
    fs.mkdirSync(path.dirname(file), { recursive: true });
    const descriptor = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_TRUNC | fs.constants.O_NOFOLLOW, 0o600);
    try {
      fs.writeFileSync(descriptor, `${JSON.stringify(artifact)}\n`);
    } finally {
      fs.closeSync(descriptor);
    }
  };

  return handleLedgerAppend;
}

module.exports = { main, transactionPath };
