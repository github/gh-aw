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
    if (!message || typeof message !== "object" || Array.isArray(message)) throw new TypeError("Invalid ledger append message");
    const { type, ...request } = message;
    if (type !== undefined && type !== "ledger_append") throw new TypeError("Invalid ledger append message type");
    requests.push(request);
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
  };

  return handleLedgerAppend;
}

module.exports = { main, transactionPath };
