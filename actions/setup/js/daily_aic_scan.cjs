// @ts-check

const fs = require("fs");
const path = require("path");
const { AIC_SCAN_CACHE_FILE_PATH, readScanCache, matchesCompletedRun, scanCacheEntry } = require("./daily_aic_cache_helpers.cjs");
const { API_READ_OPERATIONS, getErrorStatus, isQuotaError, isTransientError, safeResponseHeaders, getAPIReadFailure, withQuotaResetRecovery } = require("./daily_aic_api_budget.cjs");

const WINDOW_MS = 24 * 60 * 60 * 1000;
const MAX_PAGES = 25;
const MAX_UNFILTERED_HISTORY_PAGES = 10;
const UNFILTERED_HISTORY_PAGE_SIZE = 100;

/**
 * The run listing is authoritative for window membership and attempt identity.
 * Publish only individually resolved observations, including nonzero usage.
 * Missing entries and concurrent snapshots are safe misses, never zero usage.
 */
async function scanDailyAICOnce({ github, context, budget, artifactClient, getRunAIC, listPage, token, fallbackAIC = 0, cachePath = AIC_SCAN_CACHE_FILE_PATH, now = Date.now() }) {
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  const currentResponse = await budget.request(() => github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId }), true, API_READ_OPERATIONS.CURRENT_RUN);
  const current = currentResponse.data;
  if (!Number.isSafeInteger(current?.workflow_id) || current.workflow_id <= 0) throw new Error("Cannot resolve the daily AIC workflow");
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
    const result = await listPage(
      github,
      {
        owner,
        repo,
        workflowId: current.workflow_id,
        created,
        page,
        perPage: 100,
        lookupMode,
      },
      budget
    );
    lookupMode = result.lookupMode;
    const pageRuns = result.response.data?.workflow_runs;
    if (!Array.isArray(pageRuns)) throw new Error("Incomplete daily AIC workflow history");
    core.info(
      `[daily-workflow-aic] Listed workflow run candidates: ${JSON.stringify({
        page,
        lookupMode,
        sourceRunCount: result.sourceRunCount,
        returnedRunCount: pageRuns.length,
      })}`
    );
    if (page === 1 && result.sourceRunCount === 0) {
      const inWindow = timestamp => {
        const createdAt = Date.parse(timestamp);
        if (!Number.isFinite(createdAt)) throw new Error("A workflow run has an unknown creation time");
        return createdAt >= now - WINDOW_MS;
      };
      const observedRuns = [...entries.values()].filter(entry => entry.run_id !== context.runId && inWindow(entry.created_at)).length;
      if (observedRuns > 0) throw new Error(`Empty daily AIC workflow history contradicts ${observedRuns} restored observations in the inspection window`);
      let historyComplete = false;
      for (let historyPage = 1; historyPage <= MAX_UNFILTERED_HISTORY_PAGES; historyPage++) {
        const isWorkflowLookup = lookupMode === "workflow_id";
        const unfiltered = await budget.request(
          () =>
            isWorkflowLookup
              ? github.rest.actions.listWorkflowRuns({
                  owner,
                  repo,
                  workflow_id: current.workflow_id,
                  created,
                  per_page: UNFILTERED_HISTORY_PAGE_SIZE,
                  page: historyPage,
                })
              : github.rest.actions.listWorkflowRunsForRepo({
                  owner,
                  repo,
                  created,
                  per_page: UNFILTERED_HISTORY_PAGE_SIZE,
                  page: historyPage,
                }),
          true,
          isWorkflowLookup ? API_READ_OPERATIONS.WORKFLOW_HISTORY : API_READ_OPERATIONS.REPOSITORY_HISTORY
        );
        const runs = unfiltered.data?.workflow_runs;
        if (!Array.isArray(runs)) throw new Error("Incomplete daily AIC workflow history");
        for (const run of runs) {
          if (!isWorkflowLookup && (!Number.isSafeInteger(run?.workflow_id) || run.workflow_id <= 0)) {
            throw new Error("Incomplete daily AIC repository workflow history");
          }
          if (run.workflow_id != null && run.workflow_id !== current.workflow_id) continue;
          if (run.id === context.runId) continue;
          if (!inWindow(run.created_at)) {
            historyComplete = true;
            break;
          }
          if (run.status === "completed") {
            throw new Error("Empty daily AIC workflow history contradicts a completed run in the inspection window");
          }
        }
        if (historyComplete || runs.length < UNFILTERED_HISTORY_PAGE_SIZE) {
          historyComplete = true;
          break;
        }
      }
      if (!historyComplete) throw new Error("Unfiltered daily AIC workflow history exceeds the complete pagination window");
    }
    for (const run of pageRuns) {
      if (!Number.isSafeInteger(run?.id) || run.id <= 0) throw new Error("A workflow run has an invalid identity");
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
      const hit = matchesCompletedRun(cached, run) && cached.source === "recorded";
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
      let source;
      let accountingReason;
      if (hit) {
        aic = cached.aic;
        reason = "scan_cache";
        source = "recorded";
        accountingReason = cached.accountingReason;
      } else {
        try {
          const inspection = { github, budget, accountingReason: undefined };
          aic = await getRunAIC(artifactClient, run.id, token, owner, repo, run, inspection);
          accountingReason = inspection.accountingReason;
          reason = "artifact_accounting";
          source = "recorded";
        } catch (error) {
          const status = getErrorStatus(error);
          const canUseFallback = Number.isFinite(fallbackAIC) && fallbackAIC > 0 && !isQuotaError(error) && safeResponseHeaders(error?.response?.headers)["retry-after"] == null && (!status || isTransientError(error));
          core.info(
            `[daily-workflow-aic] Run AIC resolution failed: ${JSON.stringify({
              runId: run.id,
              runAttempt: run.run_attempt,
              status: status || null,
              error: getAPIReadFailure(error) || (error instanceof Error ? error.message : String(error)),
              fallbackAvailable: canUseFallback,
            })}`
          );
          if (!canUseFallback) throw error;
          aic = fallbackAIC;
          reason = "max_ai_credits_fallback";
          source = "estimated";
          core.warning(
            `[daily-workflow-aic] Assuming max AI Credits after all accounting sources failed: ${JSON.stringify({
              runId: run.id,
              runAttempt: run.run_attempt,
              assumedAIC: aic,
              error: error instanceof Error ? error.message : String(error),
              hint: "Check the artifact discovery/download diagnostics above. This is a conservative estimate, not measured usage or provider billing.",
            })}`
          );
        }
      }
      entries.set(run.id, { ...scanCacheEntry(run, aic, repository, current.workflow_id, now, source), accountingReason });
      if (hit) {
        cacheHits++;
      }
      core.info(`[daily-workflow-aic] Computed run AIC: ${JSON.stringify({ runId: run.id, aic, reason })}`);
      countedRuns.push({ ...run, aic, source, accountingReason });
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

async function scanDailyAIC(options) {
  return withQuotaResetRecovery(() => scanDailyAICOnce(options), options.recovery);
}

module.exports = { scanDailyAIC };
