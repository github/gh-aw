// @ts-check
/// <reference types="@actions/github-script" />

/**
 * write_daily_aic_usage_cache.cjs
 *
 * Called from the conclusion job to record this run's AI Credits consumption in the
 * per-workflow usage cache. The cache is later restored in the activation job so the
 * daily-AIC guardrail can look up prior run costs without re-downloading artifacts.
 *
 * Requires setupGlobals() to have been called first (sets global.core).
 */

const fs = require("fs");
const path = require("path");

const { findJSONLFiles, sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { AIC_USAGE_CACHE_FILE_PATH, CACHE_RETENTION_MS, pruneStaleJSONLCacheLines } = require("./daily_aic_cache_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Directory prepared by the "Collect usage artifact files" step in the conclusion job.
 * Contains agent_usage.jsonl and agent/token_usage.jsonl which mirror the contents of
 * the "usage" artifact that getRunAIC() downloads during the daily-AIC guardrail check.
 */
const USAGE_DIR = "/tmp/gh-aw/usage";

/**
 * @param {string} message
 * @param {Record<string, unknown>} [details]
 */
function logCache(message, details) {
  const suffix = details && Object.keys(details).length > 0 ? ": " + JSON.stringify(details) : "";
  core.info(`[daily-aic-cache] ${message}${suffix}`);
}

/**
 * Appends a `{run_id, aic, timestamp}` JSONL entry to the cache file, preserving any existing
 * entries that were restored from the previous cache snapshot and are within the 48-hour
 * retention window.  Entries older than {@link CACHE_RETENTION_MS} are pruned to keep the
 * cache file bounded.
 *
 * @param {string} [cacheFilePath] Override the cache file path (defaults to {@link AIC_USAGE_CACHE_FILE_PATH}; useful in tests).
 * @param {string} [usageDir] Override the usage directory (defaults to {@link USAGE_DIR}; useful in tests).
 * @returns {Promise<void>}
 */
async function mainWithPaths(cacheFilePath, usageDir) {
  const cachePath = cacheFilePath || AIC_USAGE_CACHE_FILE_PATH;
  const usageDirPath = usageDir || USAGE_DIR;
  try {
    const runId = Number(process.env.GITHUB_RUN_ID || 0);
    if (!runId) {
      core.warning("[daily-aic-cache] GITHUB_RUN_ID not set; skipping cache write.");
      return;
    }

    // Compute AIC from the usage JSONL files prepared by buildUsageArtifactUploadSteps.
    const usageFiles = findJSONLFiles(usageDirPath);
    logCache("Scanning usage JSONL files", { dir: usageDirPath, count: usageFiles.length, files: usageFiles });
    const aic = sumAICFromUsageJSONLFiles(usageFiles);
    logCache("Computed AIC for current run", { runId, aic });

    // Skip writing a non-finite or negative AIC: those values indicate an unexpected computation
    // error.  A zero AIC is written intentionally — it records runs where the agent was blocked
    // (e.g. daily-AIC guardrail exceeded) so that subsequent activations do not waste an
    // artifact-download round-trip trying to re-fetch usage data that does not exist.
    if (!Number.isFinite(aic) || aic < 0) {
      core.warning(`[daily-aic-cache] Computed AIC is ${aic} (negative or non-finite); skipping cache write.`);
      return;
    }

    // Read existing cache content (restored from the previous run's cache snapshot, if any).
    // Entries with a `timestamp` older than CACHE_RETENTION_MS are pruned to keep the file
    // bounded.  Entries without a `timestamp` (written by an older version of this script)
    // are preserved for backward compatibility.
    /** @type {string[]} */
    let keptLines = [];
    try {
      if (fs.existsSync(cachePath)) {
        const raw = fs.readFileSync(cachePath, "utf8").trimEnd();
        const cutoff = Date.now() - CACHE_RETENTION_MS;
        const prunedResult = pruneStaleJSONLCacheLines(raw, cutoff);
        keptLines = prunedResult.keptLines;
        const { prunedCount: pruned, totalCount: total } = prunedResult;
        logCache("Loaded existing cache entries", { path: cachePath, total, kept: keptLines.length, pruned });
      } else {
        logCache("No existing cache file found; starting fresh", { path: cachePath });
      }
    } catch (readErr) {
      core.warning(`[daily-aic-cache] Could not read existing cache file: ${getErrorMessage(readErr)}`);
    }

    // Build the updated JSONL content.
    const newEntry = JSON.stringify({ run_id: runId, aic, timestamp: new Date().toISOString() });
    const updatedContent = keptLines.length > 0 ? `${keptLines.join("\n")}\n${newEntry}\n` : `${newEntry}\n`;

    // Ensure the directory exists and write the updated file.
    const dir = path.dirname(cachePath);
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(cachePath, updatedContent, "utf8");
    logCache("Wrote cache entry", { runId, aic, path: cachePath });
  } catch (error) {
    // Non-fatal: a cache write failure should never block the conclusion job.
    core.warning(`[daily-aic-cache] Failed to write usage cache: ${getErrorMessage(error)}`);
  }
}

/**
 * Entry point called from the GitHub Actions step.
 *
 * @returns {Promise<void>}
 */
async function main() {
  return mainWithPaths();
}

module.exports = { main, mainWithPaths };
