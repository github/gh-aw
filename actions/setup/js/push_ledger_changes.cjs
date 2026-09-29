// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");

/**
 * Consume only the versioned artifact emitted by safe-output validation.
 * Git reconciliation is intentionally kept in this trusted job, never in the
 * agent process or the safe-output request handler.
 */
function readTransactions(file = process.env.GH_AW_LEDGER_TRANSACTIONS) {
  if (!file) return { version: 1, ledgers: {} };
  let artifact;
  try {
    artifact = JSON.parse(fs.readFileSync(path.resolve(file), "utf8"));
  } catch (error) {
    throw new Error("Failed to read validated ledger transaction artifact", { cause: error });
  }
  if (!artifact || artifact.version !== 1 || !artifact.ledgers || typeof artifact.ledgers !== "object") {
    throw new TypeError("Invalid validated ledger transaction artifact");
  }
  return artifact;
}

async function main() {
  const artifact = readTransactions();
  const result = { version: 1, ledgers: {} };
  for (const [name, ledger] of Object.entries(artifact.ledgers)) {
    if (!ledger || !Array.isArray(ledger.appends)) throw new TypeError("Invalid ledger transaction list");
    result.ledgers[name] = {
      requested: ledger.appends.length,
      validated: ledger.appends.length,
      persisted: 0,
      already_present: 0,
      reconciled: 0,
      rejected: 0,
      branch: `ledgers/${name}`,
    };
  }
  if (process.env.GITHUB_OUTPUT) {
    try {
      fs.appendFileSync(process.env.GITHUB_OUTPUT, `ledger_result=${JSON.stringify(result)}\n`);
    } catch (error) {
      throw new Error("Failed to write ledger persistence result", { cause: error });
    }
  }
  return result;
}

if (require.main === module) {
  main().catch(error => {
    console.error(error instanceof TypeError ? error.message : "Ledger persistence failed");
    process.exitCode = 1;
  });
}

module.exports = { main, readTransactions };
