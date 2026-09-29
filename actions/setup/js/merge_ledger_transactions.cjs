// @ts-check
/// <reference types="@actions/github-script" />

/**
 * merge_ledger_transactions.cjs
 *
 * Merges redacted `ledger_mutation` audit entries written by the ledger MCP server
 * during the agent run into the safe-output file, so they are ingested, validated,
 * bounded and reported by the log-only ledger handler.
 *
 * The ledger transaction log is untrusted input: it lives in an agent-reachable
 * temporary directory and its entries are self-reported. This step therefore keeps
 * only well-formed append entries carrying redacted metadata, drops everything else,
 * and never fails the workflow. Authoritative ledger state remains the shard content
 * validated by the persistence job.
 *
 * Environment variables:
 *   GH_AW_SAFE_OUTPUTS - Path to the safe-output JSONL file consumed by ingestion
 *   GH_AW_LEDGER_TRANSACTION_LOG - Optional override for the ledger transaction log path
 */

const fs = require("fs");
const path = require("path");
const { LEDGER_TRANSACTION_LOG_PATH } = require("./constants.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @type {number} Maximum transaction log size inspected, in bytes */
const MAX_LOG_BYTES = 4 * 1024 * 1024;

/** @type {number} Maximum audit entries merged into the safe-output file.
 * Mirrors `LedgerMutationDefaultMax` in pkg/workflow/repo_memory_ledger_safe_output.go,
 * which bounds the same type during safe-output validation. */
const MAX_ENTRIES = 1000;

/** @type {RegExp} Accepted record and payload hash syntax */
const HASH = /^sha256:[0-9a-f]{64}$/;

/** @type {RegExp} Accepted record identifier syntax */
const RECORD_ID = /^ldg-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/**
 * Validate one parsed audit entry and return its redacted, normalized form.
 * @param {any} candidate
 * @returns {Object | null}
 */
function normalizeEntry(candidate) {
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) return null;
  if (candidate.type !== "ledger_mutation" || candidate.operation !== "append") return null;
  const fields = candidate.record;
  if (!fields || typeof fields !== "object" || Array.isArray(fields)) return null;
  if (typeof fields.id !== "string" || !RECORD_ID.test(fields.id)) return null;
  if (typeof fields.sha !== "string" || !HASH.test(fields.sha)) return null;
  if (typeof fields.payload_sha !== "string" || !HASH.test(fields.payload_sha)) return null;
  if (typeof fields.type !== "string" || !fields.type || fields.type.length > 128) return null;
  if (typeof fields.timestamp !== "string" || fields.timestamp.length > 64) return null;
  if (!Array.isArray(fields.parents) || fields.parents.some(parent => typeof parent !== "string" || !HASH.test(parent))) return null;
  if (typeof candidate.timestamp !== "string" || candidate.timestamp.length > 64) return null;
  return {
    type: "ledger_mutation",
    operation: "append",
    timestamp: candidate.timestamp,
    record: {
      id: fields.id,
      type: fields.type,
      timestamp: fields.timestamp,
      parents: fields.parents,
      sha: fields.sha,
      payload_sha: fields.payload_sha,
    },
  };
}

/**
 * Read and validate the ledger transaction log.
 * @param {string} logPath
 * @returns {Object[]}
 */
function readAuditEntries(logPath) {
  let stat;
  try {
    stat = fs.lstatSync(logPath);
  } catch {
    return [];
  }
  if (!stat.isFile() || stat.isSymbolicLink()) {
    core.warning("Ledger transaction log is not a regular file; skipping ledger audit merge");
    return [];
  }
  if (stat.size > MAX_LOG_BYTES) {
    core.warning(`Ledger transaction log exceeds ${MAX_LOG_BYTES} bytes; skipping ledger audit merge`);
    return [];
  }
  let content;
  try {
    content = fs.readFileSync(logPath, "utf8");
  } catch (error) {
    throw new Error(`Failed to read ledger transaction log: ${getErrorMessage(error)}`, { cause: error });
  }
  const entries = [];
  const seen = new Set();
  let dropped = 0;
  for (const line of content.split("\n")) {
    if (!line.trim()) continue;
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      dropped++;
      continue;
    }
    const normalized = normalizeEntry(parsed);
    if (!normalized) {
      dropped++;
      continue;
    }
    const key = normalized.record.sha;
    if (seen.has(key)) continue;
    seen.add(key);
    if (entries.length >= MAX_ENTRIES) {
      dropped++;
      continue;
    }
    entries.push(normalized);
  }
  if (dropped > 0) {
    core.warning(`Dropped ${dropped} malformed or excess ledger audit entr${dropped === 1 ? "y" : "ies"}`);
  }
  return entries;
}

async function main() {
  const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
  if (!safeOutputsPath) {
    core.info("No safe-output file configured; skipping ledger audit merge");
    return;
  }
  const logPath = process.env.GH_AW_LEDGER_TRANSACTION_LOG || LEDGER_TRANSACTION_LOG_PATH;
  try {
    const entries = readAuditEntries(logPath);
    if (entries.length === 0) {
      core.info("No ledger audit entries to merge");
      return;
    }
    fs.mkdirSync(path.dirname(safeOutputsPath), { recursive: true });
    fs.appendFileSync(safeOutputsPath, entries.map(entry => `${JSON.stringify(entry)}\n`).join(""));
    core.info(`Merged ${entries.length} ledger audit entr${entries.length === 1 ? "y" : "ies"} into the safe-output file`);
  } catch (error) {
    // Auditing must never break the run: the ledger records themselves are already durable.
    core.warning(`Failed to merge ledger audit entries: ${getErrorMessage(error)}`);
  }
}

module.exports = { main, normalizeEntry, readAuditEntries };
