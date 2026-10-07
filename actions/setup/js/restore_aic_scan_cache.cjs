// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { AIC_SCAN_CACHE_FILE_PATH, AIC_SCAN_CACHE_ARTIFACT_NAME, readScanCache } = require("./daily_aic_cache_helpers.cjs");
const { createAPIBudget, retryNotBefore } = require("./daily_aic_api_budget.cjs");

const MAX_RESTORE_RUNS = 10;
const MAX_RESTORE_PAGES = 25;
const SCAN_WINDOW_MS = 24 * 60 * 60 * 1000;

function isTrustedProducer(run, current, repository, defaultBranch) {
  if (run.workflow_id !== current.workflow_id || run.path !== current.path || run.repository?.full_name !== repository) return false;
  // pull_request executes the contributor's workflow. Never trust its summary,
  // even if it has the same workflow ID or the artifact has the expected name.
  return run.event === "pull_request_target" || (["push", "schedule", "workflow_dispatch"].includes(run.event) && !!defaultBranch && run.head_branch === defaultBranch && run.head_repository?.full_name === repository);
}

async function listRequiredWorkflowRuns(owner, repo, current, repository, defaultBranch, budget) {
  const runs = [];
  const created = `>=${new Date(Date.now() - SCAN_WINDOW_MS).toISOString()}`;
  for (let page = 1; page <= MAX_RESTORE_PAGES && runs.length < MAX_RESTORE_RUNS; page++) {
    const response = await github.rest.actions.listWorkflowRunsForRepo({ owner, repo, status: "completed", created, per_page: 100, page });
    budget.observe(response);
    const pageRuns = response.data.workflow_runs || [];
    runs.push(...pageRuns.filter(run => run.id !== context.runId && isTrustedProducer(run, current, repository, defaultBranch)).slice(0, MAX_RESTORE_RUNS - runs.length));
    if (pageRuns.length < 100) break;
  }
  return runs;
}

async function listRecentWorkflowRuns(owner, repo, current, repository, defaultBranch, budget) {
  let response;
  try {
    response = await github.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: current.workflow_id, per_page: 10 });
  } catch (error) {
    if (error?.status !== 404) throw error;
    core.info("[daily-aic-cache] Workflow-specific history unavailable; searching repository history for required workflow runs.");
    return listRequiredWorkflowRuns(owner, repo, current, repository, defaultBranch, budget);
  }
  budget.observe(response);
  return response.data.workflow_runs;
}

async function mainWithPaths(cachePath = AIC_SCAN_CACHE_FILE_PATH, options = {}) {
  const budget = createAPIBudget();
  const client = options.createArtifactClient?.() || new DefaultArtifactClient({ onResponse: budget.observe });
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  const currentResponse = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId });
  budget.observe(currentResponse);
  const current = currentResponse.data;
  if (!current.workflow_id) throw new Error("Cannot resolve workflow for daily AIC snapshot restore");
  const defaultBranch = context.payload.repository?.default_branch;
  const runs = await listRecentWorkflowRuns(owner, repo, current, repository, defaultBranch, budget);
  const auth = await github.auth({ type: "token" });
  if (!auth || typeof auth !== "object" || !("token" in auth) || typeof auth.token !== "string" || !auth.token) {
    throw new Error("No token available to restore daily AIC observations");
  }
  const token = auth.token;
  for (const run of runs) {
    if (run.id === context.runId || !isTrustedProducer(run, current, repository, defaultBranch)) continue;
    const response = await github.rest.actions.listWorkflowRunArtifacts({ owner, repo, run_id: run.id, per_page: 100 });
    budget.observe(response);
    const artifact = response.data.artifacts.find(item => item.name === AIC_SCAN_CACHE_ARTIFACT_NAME && !item.expired);
    if (!artifact) continue;
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

async function main() {
  const { shouldSkipDailyAICGuardrail } = require("./check_daily_aic_workflow_guardrail.cjs");
  if (shouldSkipDailyAICGuardrail()) return;
  try {
    await mainWithPaths();
  } catch (error) {
    const retryAt = retryNotBefore(error?.response?.headers);
    if (retryAt) core.info(`[daily-aic-cache] No further API requests before ${retryAt}`);
    throw error;
  }
}

module.exports = { main, mainWithPaths, isTrustedProducer };
