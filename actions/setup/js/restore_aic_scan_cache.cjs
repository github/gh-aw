// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { AIC_SCAN_CACHE_FILE_PATH, AIC_SCAN_CACHE_ARTIFACT_NAME, CACHE_RETENTION_MS, readScanCache } = require("./daily_aic_cache_helpers.cjs");
const { createAPIBudget, retryNotBefore, safeResponseHeaders } = require("./daily_aic_api_budget.cjs");
const { logRateLimitFromResponse } = require("./github_rate_limit_logger.cjs");

const MAX_ARTIFACT_PAGES = 5;
const MAX_PRODUCER_RUN_PAGES = 5;
const MAX_SNAPSHOT_DOWNLOADS = 3;

function isTrustedProducer(run, current, repository, defaultBranch) {
  if (run.workflow_id !== current.workflow_id || run.path !== current.path || run.repository?.full_name !== repository) return false;
  // pull_request executes the contributor's workflow. Never trust its summary,
  // even if it has the same workflow ID or the artifact has the expected name.
  return run.event === "pull_request_target" || (["push", "schedule", "workflow_dispatch"].includes(run.event) && !!defaultBranch && run.head_branch === defaultBranch && run.head_repository?.full_name === repository);
}

async function mainWithPaths(cachePath = AIC_SCAN_CACHE_FILE_PATH, options = {}) {
  const budget = createAPIBudget();
  const observe = response => {
    const headers = safeResponseHeaders(response.headers);
    logRateLimitFromResponse({ headers }, "daily-aic-cache.restore");
    core.info(`[daily-aic-cache] API response: ${JSON.stringify({ request: budget.snapshot().requests + 1, status: response.status, ...headers })}`);
    budget.observe(response);
  };
  const client = options.createArtifactClient?.() || new DefaultArtifactClient({ onResponse: observe });
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  core.info(`[daily-aic-cache] Discovery limits: ${JSON.stringify({ artifactPages: MAX_ARTIFACT_PAGES, producerRunPages: MAX_PRODUCER_RUN_PAGES, snapshotDownloads: MAX_SNAPSHOT_DOWNLOADS })}`);
  const currentResponse = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  observe(currentResponse);
  const current = currentResponse.data;
  if (!current.workflow_id) throw new Error("Cannot resolve workflow for daily AIC snapshot restore");
  const defaultBranch = context.payload.repository?.default_branch;
  const auth = await github.auth({ type: "token" });
  if (!auth || typeof auth !== "object" || !("token" in auth) || typeof auth.token !== "string" || !auth.token) {
    throw new Error("No token available to restore daily AIC observations");
  }
  const token = auth.token;
  const artifacts = [];
  for (let page = 1; page <= MAX_ARTIFACT_PAGES; page++) {
    const response = await github.rest.actions.listArtifactsForRepo({ owner, repo, name: AIC_SCAN_CACHE_ARTIFACT_NAME, per_page: 100, page });
    observe(response);
    let olderArtifactsFound = false;
    for (const artifact of response.data.artifacts) {
      const createdAt = artifact.created_at ? Date.parse(artifact.created_at) : NaN;
      if (Number.isFinite(createdAt) && createdAt < Date.now() - CACHE_RETENTION_MS) {
        olderArtifactsFound = true;
        break;
      }
      if (artifact.name !== AIC_SCAN_CACHE_ARTIFACT_NAME || artifact.expired || !artifact.workflow_run?.id || artifact.workflow_run.id === context.runId) continue;
      artifacts.push(artifact);
    }
    if (olderArtifactsFound || page * 100 >= response.data.total_count) break;
    if (page === MAX_ARTIFACT_PAGES) core.info("[daily-aic-cache] Artifact discovery page limit reached.");
  }
  const candidateRunIds = new Set(artifacts.map(artifact => artifact.workflow_run.id));
  core.info(`[daily-aic-cache] Snapshot candidates: ${JSON.stringify({ artifacts: artifacts.length, producerRuns: candidateRunIds.size })}`);
  const producerRuns = new Map();
  const created = `>=${new Date(Date.now() - CACHE_RETENTION_MS).toISOString()}`;
  for (let page = 1; page <= MAX_PRODUCER_RUN_PAGES && producerRuns.size < candidateRunIds.size; page++) {
    const response = await github.rest.actions.listWorkflowRunsForRepo({ owner, repo, created, per_page: 100, page });
    observe(response);
    for (const run of response.data.workflow_runs || []) {
      if (candidateRunIds.has(run.id)) producerRuns.set(run.id, run);
    }
    if (page * 100 >= response.data.total_count) break;
  }
  core.info(`[daily-aic-cache] Matched producer runs: ${JSON.stringify({ matched: producerRuns.size, candidates: candidateRunIds.size })}`);
  let downloads = 0;
  for (const artifact of artifacts) {
    if (downloads >= MAX_SNAPSHOT_DOWNLOADS) {
      core.info("[daily-aic-cache] Snapshot download limit reached.");
      break;
    }
    const run = producerRuns.get(artifact.workflow_run.id);
    if (!run || !isTrustedProducer(run, current, repository, defaultBranch)) continue;
    downloads++;
    core.info(`[daily-aic-cache] Downloading verified snapshot: ${JSON.stringify({ producerRunId: run.id, artifactId: artifact.id, download: downloads })}`);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-scan-restore-"));
    try {
      const download = await client.downloadArtifact(artifact.id, {
        path: directory,
        findBy: { token, workflowRunId: run.id, repositoryOwner: owner, repositoryName: repo },
      });
      const file = path.join(download.downloadPath || directory, path.basename(AIC_SCAN_CACHE_FILE_PATH));
      if (!fs.existsSync(file)) continue;
      const entries = readScanCache(fs.readFileSync(file, "utf8"), repository, current.workflow_id);
      if (entries.size === 0) continue;
      fs.mkdirSync(path.dirname(cachePath), { recursive: true });
      fs.writeFileSync(cachePath, [...entries.values()].map(entry => JSON.stringify(entry)).join("\n") + "\n", "utf8");
      core.info(`[daily-aic-cache] Restored verified scan observations: ${JSON.stringify({ producerRunId: run.id, artifactId: artifact.id, entries: entries.size })}`);
      return;
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  }
  core.info("[daily-aic-cache] No trusted scan snapshot found; resolve the complete window from usage artifacts.");
}

async function main(options = {}) {
  const { shouldSkipDailyAICGuardrail } = require("./check_daily_aic_workflow_guardrail.cjs");
  if (shouldSkipDailyAICGuardrail()) return;
  try {
    await mainWithPaths();
  } catch (error) {
    const headers = safeResponseHeaders(error?.response?.headers);
    logRateLimitFromResponse({ headers }, "daily-aic-cache.restore-failed");
    core.info(`[daily-aic-cache] Restore failed: ${JSON.stringify({ status: Number(error?.status || error?.response?.status) || undefined, ...headers })}`);
    const retryAt = retryNotBefore(error?.response?.headers);
    if (retryAt) core.info(`[daily-aic-cache] No further API requests before ${retryAt}`);
    if (options.continueOnError === true) {
      core.warning("[daily-aic-cache] Snapshot restore unavailable; continuing without cached observations.");
    } else {
      core.setFailed("[daily-aic-cache] Snapshot restore unavailable; stopping activation.");
      return;
    }
  }
}

module.exports = { main, mainWithPaths, isTrustedProducer };
