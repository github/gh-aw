// @ts-check

/**
 * daily_aic_cache_helpers.cjs
 *
 * Shared helpers for reading and pruning the per-workflow AIC usage cache JSONL file.
 * Both the daily-AIC guardrail check (check_daily_aic_workflow_guardrail.cjs) and the
 * cache write step (write_daily_aic_usage_cache.cjs) use the same retention policy;
 * this module is the single source of truth for that logic.
 */

/** Path where the per-workflow usage cache lives on the runner. */
const AIC_USAGE_CACHE_FILE_PATH = "/tmp/gh-aw/agentic-workflow-usage-cache.jsonl";

/** Cache entries older than this threshold (in ms) are pruned when reading or writing. */
const CACHE_RETENTION_MS = 48 * 60 * 60 * 1000;
const AIC_SCAN_CACHE_FILE_PATH = "/tmp/gh-aw/agentic-workflow-usage-scan-v2.jsonl";
const AIC_SCAN_CACHE_ARTIFACT_NAME = "aic-usage-scan-v2";

/**
 * A snapshot is a set of observations, not proof that the current window is complete.
 * Every observation must still match the authoritative completed-run listing.
 */
function readScanCache(content, repository, workflowId, now = Date.now()) {
  const entries = new Map();
  for (const line of content.split("\n").filter(line => line.trim())) {
    let entry;
    try {
      entry = JSON.parse(line);
    } catch {
      continue;
    }
    if (
      entry?.version !== 2 ||
      entry.coverage_version !== 1 ||
      entry.repository !== repository ||
      entry.workflow_id !== workflowId ||
      !Number.isSafeInteger(entry.run_id) ||
      entry.run_id <= 0 ||
      !Number.isSafeInteger(entry.run_attempt) ||
      entry.run_attempt <= 0 ||
      !Number.isFinite(entry.aic) ||
      entry.aic < 0 ||
      !Number.isFinite(Date.parse(entry.created_at)) ||
      !Number.isFinite(Date.parse(entry.updated_at)) ||
      !Number.isFinite(Date.parse(entry.observed_at)) ||
      Date.parse(entry.observed_at) < now - CACHE_RETENTION_MS ||
      Date.parse(entry.observed_at) > now
    ) {
      continue;
    }
    const prior = entries.get(entry.run_id);
    if (prior && prior.run_attempt === entry.run_attempt && prior.updated_at === entry.updated_at && prior.aic !== entry.aic) {
      throw new Error("Conflicting daily AIC observations for a completed run attempt");
    }
    if (!prior || Date.parse(entry.observed_at) >= Date.parse(prior.observed_at)) {
      entries.set(entry.run_id, entry);
    }
  }
  return entries;
}

function matchesCompletedRun(entry, run) {
  return entry?.run_attempt === run.run_attempt && entry.created_at === run.created_at && entry.updated_at === run.updated_at;
}

function scanCacheEntry(run, aic, repository, workflowId, now = Date.now()) {
  if (!Number.isFinite(aic) || aic < 0) {
    throw new Error("Daily AIC observation is not a finite non-negative value");
  }
  return {
    version: 2,
    coverage_version: 1,
    repository,
    workflow_id: workflowId,
    run_id: run.id,
    run_attempt: run.run_attempt,
    created_at: run.created_at,
    updated_at: run.updated_at,
    aic,
    observed_at: new Date(now).toISOString(),
  };
}

/**
 * Splits raw JSONL file content into lines, pruning entries whose `timestamp` field
 * is older than `cutoffMs`.
 *
 * - Empty lines are discarded.
 * - Lines that do not look like JSON objects (do not start with "{") are discarded.
 * - Object-like lines that cannot be parsed as JSON are preserved (defensive: avoids data loss).
 * - Lines that have no `timestamp` field are preserved (backward compatibility with
 *   entries written by older versions of the write script).
 *
 * @param {string} content  Raw JSONL file content.
 * @param {number} cutoffMs Entries with a `timestamp` that parses to a value strictly
 *                          less than `cutoffMs` are pruned.
 * @returns {{ keptLines: string[], prunedCount: number, totalCount: number }}
 */
function pruneStaleJSONLCacheLines(content, cutoffMs) {
  /** @type {string[]} */
  const keptLines = [];
  let prunedCount = 0;
  let totalCount = 0;
  for (const rawLine of content.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    totalCount++;
    if (!line.startsWith("{")) {
      prunedCount++;
      continue;
    }
    try {
      const entry = JSON.parse(line);
      if (typeof entry?.timestamp === "string") {
        const ts = Date.parse(entry.timestamp);
        if (Number.isFinite(ts) && ts < cutoffMs) {
          prunedCount++;
          continue;
        }
      }
      keptLines.push(line);
    } catch {
      // Preserve object-like lines that cannot be parsed (defensive: avoids data loss).
      keptLines.push(line);
    }
  }
  return { keptLines, prunedCount, totalCount };
}

module.exports = {
  AIC_USAGE_CACHE_FILE_PATH,
  CACHE_RETENTION_MS,
  pruneStaleJSONLCacheLines,
  AIC_SCAN_CACHE_FILE_PATH,
  AIC_SCAN_CACHE_ARTIFACT_NAME,
  readScanCache,
  matchesCompletedRun,
  scanCacheEntry,
};
