// @ts-check

const fs = require("fs");
const path = require("path");
const { findJSONLFiles, isDailyAICUsageJSONLFile, sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const LEDGER_SUBDIR = "daily-aic-ledger";
const WINDOW_MS = 24 * 60 * 60 * 1000;
const USAGE_ROOT = "/tmp/gh-aw";
const USAGE_STAGING_ROOT = path.join(USAGE_ROOT, "usage");

function repoMemoryDirOrThrow(repoMemoryDir = process.env.GH_AW_DAILY_AIC_REPO_MEMORY_DIR) {
  const resolved = typeof repoMemoryDir === "string" ? repoMemoryDir.trim() : "";
  if (!resolved) {
    throw new Error("GH_AW_DAILY_AIC_REPO_MEMORY_DIR is required for the daily AIC repo-memory backend.");
  }
  return resolved;
}

function ledgerRoot(repoMemoryDir) {
  return path.join(repoMemoryDirOrThrow(repoMemoryDir), LEDGER_SUBDIR);
}

function utcDay(ms) {
  return new Date(ms).toISOString().slice(0, 10);
}

function ledgerPathForDay(root, day) {
  return path.join(root, `${day}.jsonl`);
}

function ledgerReadPaths(root, now = Date.now()) {
  return [ledgerPathForDay(root, utcDay(now)), ledgerPathForDay(root, utcDay(now - WINDOW_MS))].filter((value, index, array) => array.indexOf(value) === index);
}

function validEntry(entry, repository, workflowId, now = Date.now()) {
  if (entry?.version !== 1) return false;
  if (entry.repository !== repository || entry.workflow_id !== workflowId) return false;
  const timestamp = Date.parse(entry.timestamp);
  return Number.isFinite(timestamp) && timestamp >= now - WINDOW_MS && timestamp <= now && Number.isSafeInteger(entry.run_id) && entry.run_id > 0 && Number.isFinite(entry.aic) && entry.aic >= 0;
}

function readLedgerEntries({ repoMemoryDir, repository, workflowId, now = Date.now() }) {
  const root = ledgerRoot(repoMemoryDir);
  const entriesByRun = new Map();
  for (const filePath of ledgerReadPaths(root, now)) {
    let content = "";
    try {
      content = fs.readFileSync(filePath, "utf8");
    } catch {
      continue;
    }
    for (const line of content.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed);
        if (!validEntry(entry, repository, workflowId, now)) continue;
        const prior = entriesByRun.get(entry.run_id);
        const entryTimestamp = Date.parse(entry.timestamp);
        const priorTimestamp = prior ? Date.parse(prior.timestamp) : NaN;
        if (Number.isFinite(entryTimestamp) && (!prior || !Number.isFinite(priorTimestamp) || entryTimestamp >= priorTimestamp)) {
          entriesByRun.set(entry.run_id, entry);
        }
      } catch {
        // Ignore malformed ledger lines.
      }
    }
  }
  return [...entriesByRun.values()].sort((a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp));
}

function appendLedgerEntry(entry, repoMemoryDir, now = Date.now()) {
  const root = ledgerRoot(repoMemoryDir);
  try {
    fs.mkdirSync(root, { recursive: true });
  } catch (error) {
    throw new Error(`Failed to create daily AIC repo-memory ledger directory ${root}: ${getErrorMessage(error)}`, { cause: error });
  }
  const ledgerPath = ledgerPathForDay(root, utcDay(now));
  try {
    fs.appendFileSync(ledgerPath, `${JSON.stringify(entry)}\n`, "utf8");
  } catch (error) {
    throw new Error(`Failed to append daily AIC repo-memory ledger entry to ${ledgerPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * @param {{ repoMemoryDir?: string, usageRoot?: string, now?: number }} [options]
 */
function appendCurrentRunLedgerEntry({ repoMemoryDir, usageRoot = USAGE_STAGING_ROOT, now = Date.now() } = {}) {
  const runId = Number(process.env.GITHUB_RUN_ID || 0);
  const repository = process.env.GITHUB_REPOSITORY || "";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || process.env.GITHUB_WORKFLOW || "";
  if (!Number.isFinite(runId) || runId <= 0 || !repository || !workflowId) {
    core.warning("[daily-workflow-aic] Skipping repo-memory ledger write: missing run, repository, or workflow identity.");
    return;
  }
  const usageFiles = findJSONLFiles(usageRoot).filter(isDailyAICUsageJSONLFile);
  const aic = sumAICFromUsageJSONLFiles(usageFiles);
  if (!Number.isFinite(aic) || aic < 0) {
    core.warning(`[daily-workflow-aic] Skipping repo-memory ledger write: computed AIC is ${aic}.`);
    return;
  }
  appendLedgerEntry(
    {
      version: 1,
      repository,
      workflow_id: workflowId,
      run_id: runId,
      run_url: process.env.GH_AW_RUN_URL || "",
      actor: process.env.GITHUB_TRIGGERING_ACTOR || process.env.GITHUB_ACTOR || "",
      aic,
      timestamp: new Date(now).toISOString(),
    },
    repoMemoryDir,
    now
  );
  core.info(`[daily-workflow-aic] Appended repo-memory ledger entry: ${JSON.stringify({ runId, workflowId, aic, files: usageFiles.length })}`);
}

module.exports = {
  LEDGER_SUBDIR,
  WINDOW_MS,
  USAGE_STAGING_ROOT,
  appendCurrentRunLedgerEntry,
  appendLedgerEntry,
  ledgerReadPaths,
  ledgerRoot,
  repoMemoryDirOrThrow,
  readLedgerEntries,
};
