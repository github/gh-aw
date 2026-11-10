// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { AIC_SCAN_CACHE_FILE_PATH, AIC_SCAN_CACHE_ARTIFACT_NAME, CACHE_RETENTION_MS, readScanCache } = require("./daily_aic_cache_helpers.cjs");
const { API_READ_OPERATIONS, createAPIBudget, retryNotBefore, isQuotaError, safeResponseHeaders, getAPIReadFailure, withQuotaResetRecovery } = require("./daily_aic_api_budget.cjs");
const { getGitHubAPIDiagnostic } = require("./github_api_diagnostics.cjs");

// Snapshot discovery is only an optimization; the guardrail still scans the
// complete authoritative window after a miss.
const MAX_SNAPSHOT_PRODUCERS = 2;
const MAX_SNAPSHOT_LISTED_RUNS = 10;
const MAX_SNAPSHOT_HISTORY_PAGES = 2;

function isTrustedProducer(run, current, repository, defaultBranch) {
  if (run.workflow_id !== current.workflow_id || run.path !== current.path || run.repository?.full_name !== repository) return false;
  // pull_request executes the contributor's workflow. Never trust its summary,
  // even if it has the same workflow ID or the artifact has the expected name.
  return (
    run.event === "pull_request_target" || (["push", "schedule", "workflow_dispatch", "issues", "issue_comment"].includes(run.event) && !!defaultBranch && run.head_branch === defaultBranch && run.head_repository?.full_name === repository)
  );
}

async function* listRequiredWorkflowRuns(owner, repo, current, repository, defaultBranch, budget) {
  let candidates = 0;
  const created = `>=${new Date(Date.now() - CACHE_RETENTION_MS).toISOString()}`;
  for (let page = 1; page <= MAX_SNAPSHOT_HISTORY_PAGES && candidates < MAX_SNAPSHOT_PRODUCERS; page++) {
    const response = await budget.request(() => github.rest.actions.listWorkflowRunsForRepo({ owner, repo, status: "completed", created, per_page: 100, page }), true, API_READ_OPERATIONS.REPOSITORY_HISTORY);
    const pageRuns = response.data.workflow_runs || [];
    const trustedRuns = pageRuns.filter(run => run.id !== context.runId && isTrustedProducer(run, current, repository, defaultBranch));
    core.info(`[daily-aic-cache] Evaluated repository history page: ${JSON.stringify({ page, listedRuns: pageRuns.length, trustedCandidates: trustedRuns.length })}`);
    // Inspect each page before spending quota on older history. A verified
    // snapshot ends restoration; the guardrail still lists its complete window.
    for (const run of trustedRuns.slice(0, MAX_SNAPSHOT_PRODUCERS - candidates)) {
      candidates++;
      yield run;
    }
    if (pageRuns.length < 100) break;
  }
}

async function* listRecentWorkflowRuns(owner, repo, current, repository, defaultBranch, budget) {
  let response;
  core.info(`[daily-aic-cache] Querying workflow-specific run history: ${JSON.stringify({ workflowId: current.workflow_id })}`);
  try {
    response = await budget.request(
      () => github.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: current.workflow_id, status: "completed", created: `>=${new Date(Date.now() - CACHE_RETENTION_MS).toISOString()}`, per_page: MAX_SNAPSHOT_LISTED_RUNS }),
      true,
      API_READ_OPERATIONS.WORKFLOW_HISTORY
    );
  } catch (error) {
    if (error?.status !== 404 || isQuotaError(error) || safeResponseHeaders(error?.response?.headers)["retry-after"] != null) throw error;
    core.info("[daily-aic-cache] Workflow-specific history unavailable; searching repository history for required workflow runs.");
    yield* listRequiredWorkflowRuns(owner, repo, current, repository, defaultBranch, budget);
    return;
  }
  core.info(`[daily-aic-cache] Workflow-specific run history returned candidates: ${JSON.stringify({ workflowId: current.workflow_id, runs: response.data.workflow_runs?.length ?? 0 })}`);
  yield* response.data.workflow_runs;
}

async function restoreWithBudget(cachePath, options, budget) {
  const client = options.createArtifactClient?.() || new DefaultArtifactClient({ request: budget.request });
  const { owner, repo } = context.repo;
  const repository = `${owner}/${repo}`;
  const currentResponse = await budget.request(() => github.rest.actions.getWorkflowRun({ owner, repo, run_id: context.runId }), true, API_READ_OPERATIONS.CURRENT_RUN);
  const current = currentResponse.data;
  if (!Number.isSafeInteger(current?.workflow_id) || current.workflow_id <= 0) throw new Error("Cannot resolve workflow for daily AIC snapshot restore");
  const defaultBranch = context.payload.repository?.default_branch;
  const runs = listRecentWorkflowRuns(owner, repo, current, repository, defaultBranch, budget);
  let token;
  let candidates = 0;
  for await (const run of runs) {
    const isCurrentRun = run.id === context.runId;
    const trusted = isTrustedProducer(run, current, repository, defaultBranch);
    core.info(
      `[daily-aic-cache] Evaluated snapshot producer: ${JSON.stringify({
        producerRunId: run.id,
        isCurrentRun,
        trusted,
        workflowMatches: run.workflow_id === current.workflow_id,
        pathMatches: run.path === current.path,
        repositoryMatches: run.repository?.full_name === repository,
        defaultBranchMatches: !!defaultBranch && run.head_branch === defaultBranch,
        headRepositoryMatches: run.head_repository?.full_name === repository,
      })}`
    );
    if (isCurrentRun || !trusted) continue;
    if (candidates++ >= MAX_SNAPSHOT_PRODUCERS) break;
    if (!token) {
      const auth = await github.auth({ type: "token" });
      if (!auth || typeof auth !== "object" || !("token" in auth) || typeof auth.token !== "string" || !auth.token) {
        throw new Error("No token available to restore daily AIC observations");
      }
      token = auth.token;
    }
    const response = await budget.request(() => github.rest.actions.listWorkflowRunArtifacts({ owner, repo, run_id: run.id, per_page: 100 }), true, API_READ_OPERATIONS.ARTIFACT_LIST);
    const artifact = response.data.artifacts.find(item => item.name === AIC_SCAN_CACHE_ARTIFACT_NAME && !item.expired);
    if (!artifact) {
      core.info(`[daily-aic-cache] Skipped snapshot producer: ${JSON.stringify({ producerRunId: run.id, reason: "no_unexpired_scan_artifact" })}`);
      continue;
    }
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-scan-restore-"));
    try {
      const download = await client.downloadArtifact(artifact.id, {
        path: directory,
        findBy: { token, workflowRunId: run.id, repositoryOwner: owner, repositoryName: repo },
      });
      const file = path.join(download.downloadPath || directory, path.basename(AIC_SCAN_CACHE_FILE_PATH));
      if (!fs.existsSync(file)) {
        core.info(`[daily-aic-cache] Skipped scan artifact: ${JSON.stringify({ producerRunId: run.id, artifactId: artifact.id, reason: "missing_snapshot_file" })}`);
        continue;
      }
      const entries = readScanCache(fs.readFileSync(file, "utf8"), repository, current.workflow_id);
      if (entries.size === 0) {
        core.info(`[daily-aic-cache] Skipped scan artifact: ${JSON.stringify({ producerRunId: run.id, artifactId: artifact.id, reason: "no_valid_observations" })}`);
        continue;
      }
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

async function mainWithPaths(cachePath = AIC_SCAN_CACHE_FILE_PATH, options = {}) {
  const budget = createAPIBudget();
  try {
    await withQuotaResetRecovery(() => restoreWithBudget(cachePath, options, budget), options.recovery);
  } catch (error) {
    const diagnostic = getGitHubAPIDiagnostic(error);
    const failure = getAPIReadFailure(error);
    const retryAt = diagnostic?.retryNotBefore ?? retryNotBefore(error?.response?.headers);
    if (failure) {
      const detail = diagnostic?.message ?? `GitHub API read failed (HTTP ${failure.status ?? "unknown"}; reason=${failure.reason})`;
      const message = `Daily AIC snapshot restore stopped at ${failure.operation} after ${failure.attempts} attempt(s): ${detail}. Accounting remains unverified; agent startup is blocked.`;
      const stage = `Activation stage: Restore daily AIC scan observations. API responses observed by this restore: ${budget.snapshot().requests}; shared quota used is not this step's request count.`;
      const discovery =
        failure.operation === API_READ_OPERATIONS.CURRENT_RUN
          ? "Snapshot discovery has not started in the failed restore phase attempt; earlier attempts may have queried history or artifacts."
          : "Snapshot restoration did not complete; a cache miss has not been established.";
      const recovery =
        diagnostic?.quota || failure.reason === "retry_restricted"
          ? `${retryAt ? `Blocked until at least ${retryAt}.` : "Blocked until: unknown; no valid future retry deadline was provided."} Check existing scheduled runs after the deadline before retrying; do not queue simultaneous reruns. Inspect this step's github-token reference and concurrent consumers of that credential's quota; never print token values. A reset does not verify accounting or authorize agent startup.`
          : "";
      const summary = [message, stage, discovery, recovery].filter(Boolean).join("\n\n");
      core.info(`[daily-aic-cache] ${summary}`);
      if (core.summary) {
        try {
          await core.summary.addRaw(`### Daily AIC snapshot restore blocked\n\n${summary}\n`).write();
        } catch {
          core.warning("Failed to write daily AIC snapshot restore failure summary.");
        }
      }
    }
    if (retryAt) core.info(`[daily-aic-cache] No further API requests before ${retryAt}`);
    throw error;
  } finally {
    core.info(`[daily-aic-cache] GitHub API rate limit level: ${JSON.stringify(budget.snapshot())}`);
  }
}

async function main() {
  const { shouldSkipDailyAICGuardrail } = require("./check_daily_aic_workflow_guardrail.cjs");
  if (shouldSkipDailyAICGuardrail()) return;
  await mainWithPaths();
}

module.exports = { main, mainWithPaths, isTrustedProducer };
