// @ts-check

const { readJsonlFile } = require("./jsonl_helpers.cjs");
const { createHash } = require("crypto");
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

function ledgerCoveragePath(repoMemoryDir, repository, workflowId) {
  const identity = createHash("sha256")
    .update(JSON.stringify([repository, workflowId]))
    .digest("hex");
  return path.join(ledgerRoot(repoMemoryDir), `coverage-${identity}.json`);
}

function readLedgerCoverageStart({ repoMemoryDir, repository, workflowId, now = Date.now() }) {
  let coverage;
  try {
    coverage = JSON.parse(fs.readFileSync(ledgerCoveragePath(repoMemoryDir, repository, workflowId), "utf8"));
  } catch (error) {
    if (error instanceof SyntaxError || (error && typeof error === "object" && "code" in error && error.code === "ENOENT")) return null;
    throw new Error(`Failed to read daily AIC repo-memory coverage: ${getErrorMessage(error)}`, { cause: error });
  }
  const start = Date.parse(coverage?.initialized_at);
  if (!Number.isFinite(start)) return null;
  return coverage?.version === 1 && coverage.repository === repository && coverage.workflow_id === workflowId && typeof coverage.initialized_at === "string" && start <= now ? start : null;
}

function initializeLedgerCoverage({ repoMemoryDir, repository, workflowId, now }) {
  // Validate the marker's version and identity; existence alone cannot establish coverage.
  if (readLedgerCoverageStart({ repoMemoryDir, repository, workflowId, now }) !== null) return;
  try {
    fs.writeFileSync(ledgerCoveragePath(repoMemoryDir, repository, workflowId), JSON.stringify({ version: 1, repository, workflow_id: workflowId, initialized_at: new Date(now).toISOString() }) + "\n", "utf8");
  } catch (error) {
    throw new Error(`Failed to initialize daily AIC repo-memory coverage: ${getErrorMessage(error)}`, { cause: error });
  }
  core.info("[daily-workflow-aic] Initialized repo-memory coverage; authoritative artifact accounting is required for the first 24 hours.");
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
    try {
      for (const entry of readJsonlFile(filePath)) {
        try {
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
    } catch (error) {
      if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") continue;
      throw new Error(`Failed to read daily AIC repo-memory ledger ${filePath}: ${getErrorMessage(error)}`, { cause: error });
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
  let aic;
  try {
    aic = sumAICFromUsageJSONLFiles(usageFiles);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "AIC_USAGE_UNPRICED") {
      core.warning("[daily-workflow-aic] Skipping repo-memory ledger write: usage is unpriced.");
      return;
    }
    throw error;
  }
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
  initializeLedgerCoverage({ repoMemoryDir, repository, workflowId, now });
  core.info(`[daily-workflow-aic] Appended repo-memory ledger entry: ${JSON.stringify({ runId, workflowId, aic, files: usageFiles.length })}`);
}

module.exports = {
  LEDGER_SUBDIR,
  WINDOW_MS,
  USAGE_STAGING_ROOT,
  appendCurrentRunLedgerEntry,
  appendLedgerEntry,
  ledgerCoveragePath,
  ledgerReadPaths,
  ledgerRoot,
  repoMemoryDirOrThrow,
  readLedgerEntries,
  readLedgerCoverageStart,
};
