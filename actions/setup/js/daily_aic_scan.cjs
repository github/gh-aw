// @ts-check

const fs = require("fs");
const path = require("path");
const { AIC_SCAN_CACHE_FILE_PATH, readScanCache, matchesCompletedRun, scanCacheEntry } = require("./daily_aic_cache_helpers.cjs");

const WINDOW_MS = 24 * 60 * 60 * 1000;
const MAX_PAGES = 25;

function getErrorStatus(error) {
  if (!error || typeof error !== "object") return 0;
  const record = /** @type {Record<string, unknown>} */ error;
  const directStatus = Number(record.status || 0);
  if (directStatus) return directStatus;
  if (!record.response || typeof record.response !== "object") return 0;
  return Number(/** @type {Record<string, unknown>} */ record.response.status || 0);
}

/**
 * The run listing is authoritative for window membership and attempt identity.
 * Publish only individually resolved observations, including nonzero usage.
 * Missing entries and concurrent snapshots are safe misses, never zero usage.
 */
async function scanDailyAIC({ github, context, budget, artifactClient, getRunAIC, listPage, token, fallbackAIC = 0, cachePath = AIC_SCAN_CACHE_FILE_PATH, now = Date.now() }) {
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  const currentResponse = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  budget.observe(currentResponse);
  const current = currentResponse.data;
  if (!current.workflow_id) throw new Error("Cannot resolve the daily AIC workflow");
  core.info(
    `[daily-workflow-aic] Started authoritative AIC scan: ${JSON.stringify({
      repository,
      currentRunId: context.runId,
      workflowId: current.workflow_id,
      fallbackAIC: Number.isFinite(fallbackAIC) && fallbackAIC > 0 ? fallbackAIC : null,
    })}`
  );
  const entries = fs.existsSync(cachePath) ? readScanCache(fs.readFileSync(cachePath, "utf8"), repository, current.workflow_id, now) : new Map();
  core.info(`[daily-workflow-aic] Loaded scan observations: ${JSON.stringify({ workflowId: current.workflow_id, cachedEntries: entries.size })}`);
  const candidates = new Map();
  let complete = false;
  let lookupMode = "workflow_id";
  const created = `>=${new Date(now - WINDOW_MS).toISOString()}`;
  for (let page = 1; page <= MAX_PAGES; page++) {
    const result = await listPage(github, {
      owner,
      repo,
      workflowId: current.workflow_id,
      created,
      page,
      perPage: 100,
      lookupMode,
    });
    budget.observe(result.response);
    lookupMode = result.lookupMode;
    const pageRuns = result.response.data.workflow_runs || [];
    core.info(
      `[daily-workflow-aic] Listed workflow run candidates: ${JSON.stringify({
        page,
        lookupMode,
        sourceRunCount: result.sourceRunCount,
        returnedRunCount: pageRuns.length,
      })}`
    );
    for (const run of pageRuns) {
      if (run.id === context.runId) continue;
      const created = Date.parse(run.created_at);
      if (!Number.isFinite(created)) throw new Error("A workflow run has an unknown creation time");
      if (created < now - WINDOW_MS) {
        complete = true;
        break;
      }
      if (run.status !== "completed" || !Number.isSafeInteger(run.run_attempt) || run.run_attempt < 1 || !Number.isFinite(Date.parse(run.updated_at))) {
        throw new Error("A workflow run has incomplete attempt metadata");
      }
      candidates.set(run.id, run);
    }
    if (result.sourceRunCount < 100) complete = true;
    if (result.oldestUnfilteredCreatedAt != null) {
      const oldest = Date.parse(result.oldestUnfilteredCreatedAt);
      if (!Number.isFinite(oldest)) throw new Error("Workflow history has an unknown creation time");
      if (oldest < now - WINDOW_MS) complete = true;
    }
    if (complete) break;
  }
  if (!complete) throw new Error("Daily AIC workflow history exceeds the complete pagination window");

  const countedRuns = [];
  let cacheHits = 0;
  try {
    for (const run of candidates.values()) {
      const cached = entries.get(run.id);
      const hit = matchesCompletedRun(cached, run);
      core.info(
        `[daily-workflow-aic] Resolving run AIC: ${JSON.stringify({
          runId: run.id,
          runAttempt: run.run_attempt,
          conclusion: run.conclusion || "unknown",
          cacheHit: hit,
        })}`
      );
      let aic;
      let reason;
      if (hit) {
        aic = cached.aic;
        reason = "scan_cache";
      } else {
        try {
          aic = await getRunAIC(artifactClient, run.id, token, owner, repo, run, { github, budget });
          reason = "artifact_accounting";
        } catch (error) {
          const status = getErrorStatus(error);
          const canUseFallback = Number.isFinite(fallbackAIC) && fallbackAIC > 0 && !status;
          core.info(
            `[daily-workflow-aic] Run AIC resolution failed: ${JSON.stringify({
              runId: run.id,
              runAttempt: run.run_attempt,
              status: status || null,
              error: error instanceof Error ? error.message : String(error),
              fallbackAvailable: canUseFallback,
            })}`
          );
          if (!canUseFallback) throw error;
          aic = fallbackAIC;
          reason = "max_ai_credits_fallback";
          core.warning(
            `[daily-workflow-aic] Assuming max AI Credits after all accounting sources failed: ${JSON.stringify({
              runId: run.id,
              runAttempt: run.run_attempt,
              assumedAIC: aic,
            })}`
          );
        }
      }
      entries.set(run.id, scanCacheEntry(run, aic, repository, current.workflow_id, now));
      if (hit) {
        cacheHits++;
      }
      core.info(`[daily-workflow-aic] Computed run AIC: ${JSON.stringify({ runId: run.id, aic, reason })}`);
      countedRuns.push({ ...run, aic });
    }
  } finally {
    // This runs even after an API error. Partial snapshots accelerate recovery but
    // cannot authorize a later run without a new complete authoritative listing.
    fs.mkdirSync(path.dirname(cachePath), { recursive: true });
    fs.writeFileSync(cachePath, [...entries.values()].map(entry => JSON.stringify(entry)).join("\n") + "\n", "utf8");
    core.info(`[daily-workflow-aic] Persisted scan observations: ${JSON.stringify({ cachePath, entries: entries.size })}`);
  }
  core.info(`[daily-workflow-aic] Completed authoritative AIC scan: ${JSON.stringify({ candidateRuns: candidates.size, countedRuns: countedRuns.length, cacheHits })}`);
  return { countedRuns, candidateRunsCount: candidates.size, cacheHits, current };
}

module.exports = { scanDailyAIC };
