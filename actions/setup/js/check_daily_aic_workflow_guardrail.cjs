// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");

const { calculateDailyAICStats, findJSONLFiles, formatAICCredits, sumAICFromUsageJSONLFiles } = require("./daily_aic_workflow_helpers.cjs");
const { AIC_USAGE_CACHE_FILE_PATH, CACHE_RETENTION_MS, pruneStaleJSONLCacheLines } = require("./daily_aic_cache_helpers.cjs");
const { parsePositiveCompactNumber } = require("./numeric_limits.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { createRateLimitAwareGithub } = require("./github_rate_limit_logger.cjs");
const { scanDailyAIC } = require("./daily_aic_scan.cjs");
const { createAPIBudget, retryNotBefore, safeResponseHeaders } = require("./daily_aic_api_budget.cjs");
const { loadBillableJobs, allBillableJobsSkipped, sumCoveredComponents } = require("./daily_aic_component_coverage.cjs");

const PRIMARY_GUARDRAIL_ARTIFACT_NAMES = ["usage"];
const MAX_WORKFLOW_RUN_PAGES = 10;
const RATE_LIMIT_RESERVE = 100;
const REQUEST_OVERHEAD_BUDGET = MAX_WORKFLOW_RUN_PAGES + 4;
const ESTIMATED_API_OPERATIONS_PER_RUN = 2;
const INTEGER_FORMATTER = new Intl.NumberFormat("en-US");

/**
 * @returns {Promise<any>}
 */
async function getArtifactClient(onResponse) {
  return new DefaultArtifactClient({ onResponse });
}

/**
 * @param {string} message
 * @param {Record<string, unknown>} [details]
 * @returns {string}
 */
function formatDailyGuardrailLogMessage(message, details) {
  if (!details || Object.keys(details).length === 0) {
    return `[daily-workflow-aic] ${message}`;
  }
  let serializedDetails = "";
  try {
    serializedDetails = JSON.stringify(details);
  } catch {
    serializedDetails = JSON.stringify({ error: "failed to serialize log details" });
  }
  return `[daily-workflow-aic] ${message}: ${serializedDetails}`;
}

/**
 * Emit a consistently prefixed daily workflow AI Credits diagnostic log line.
 *
 * @param {string} message
 * @param {Record<string, unknown>} [details]
 * @returns {void}
 */
function logDailyGuardrail(message, details) {
  core.info(formatDailyGuardrailLogMessage(message, details));
}

/**
 * Event types that indicate a user-initiated slash command trigger.
 * When aw_context.event_type is one of these, the workflow was triggered by a user
 * typing a slash command in a comment, and the daily guardrail should not be skipped.
 */
const SLASH_COMMAND_EVENT_TYPES = ["issue_comment", "pull_request_review_comment", "discussion_comment"];
const SLASH_COMMAND_TRIGGERING_EVENTS = ["issues", "issue_comment", "pull_request", "pull_request_review_comment", "discussion", "discussion_comment"];
const LABEL_COMMAND_TRIGGERING_EVENTS = ["issues", "pull_request", "discussion"];

/**
 * @param {string | undefined} value
 * @returns {boolean}
 */
function envFlagEnabled(value) {
  if (typeof value !== "string") {
    return false;
  }
  const normalized = value.trim().toLowerCase();
  return normalized === "true" || normalized === "1" || normalized === "yes";
}

/**
 * @returns {boolean}
 */
function shouldSkipDailyAICGuardrail() {
  const eventName = process.env.GITHUB_EVENT_NAME || "";
  const isWorkflowCall = eventName === "workflow_call";
  const isRepositoryDispatch = eventName === "repository_dispatch";
  const hasSlashCommand = envFlagEnabled(process.env.GH_AW_HAS_SLASH_COMMAND);
  const hasLabelCommand = envFlagEnabled(process.env.GH_AW_HAS_LABEL_COMMAND);
  const rawContext = (process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT || "").trim();
  const hasDispatchContext = rawContext !== "";
  if (isWorkflowCall || isRepositoryDispatch) {
    return true;
  }
  if (eventName === "workflow_dispatch") {
    // Manual user-triggered runs intentionally bypass the daily guardrail.
    if (!hasDispatchContext) {
      return true;
    }
    // Dispatch-routed slash/label commands intentionally bypass the daily guardrail.
    try {
      const awContext = JSON.parse(rawContext);
      const isLabelCommand = typeof awContext.trigger_label === "string" && awContext.trigger_label.trim() !== "";
      const isSlashCommand = SLASH_COMMAND_EVENT_TYPES.includes(awContext.event_type);
      if (isLabelCommand || isSlashCommand) {
        return true;
      }
    } catch {
      // Malformed aw_context is ignored: skip the guardrail as a safe fallback for manual dispatch.
    }
    // Existing behavior: dispatch-routed runs with aw_context bypass the guardrail.
    return true;
  }
  if (hasSlashCommand && SLASH_COMMAND_TRIGGERING_EVENTS.includes(eventName)) {
    return true;
  }
  if (hasLabelCommand && LABEL_COMMAND_TRIGGERING_EVENTS.includes(eventName)) {
    return true;
  }
  return false;
}

/**
 * Loads the per-workflow usage cache from the JSONL file restored by the activation job's
 * cache-restore step.  Each line is a JSON object `{ run_id: number, aic: number, timestamp?: string }`.
 *
 * Entries with a `timestamp` older than {@link CACHE_RETENTION_MS} (48 h) are skipped so that
 * stale data cannot inflate the daily-AIC total.  Entries without a `timestamp` (written by an
 * older version of the write script) are kept for backward compatibility.
 *
 * Returns a `Map<runId, aic>` so that callers can check whether a prior run's AIC is already
 * known without downloading the run's artifact from the GitHub API.
 *
 * @param {string} [filePath]
 * @returns {Map<number, number>}
 */
function loadAICUsageCache(filePath) {
  const cachePath = filePath || AIC_USAGE_CACHE_FILE_PATH;
  /** @type {Map<number, number>} */
  const cache = new Map();
  try {
    if (!fs.existsSync(cachePath)) {
      logDailyGuardrail("No usage cache file found; all runs will be resolved via API", { path: cachePath });
      return cache;
    }
    const content = fs.readFileSync(cachePath, "utf8");
    const cutoff = Date.now() - CACHE_RETENTION_MS;
    const { keptLines, prunedCount: skippedStale } = pruneStaleJSONLCacheLines(content, cutoff);
    let loaded = 0;
    for (const line of keptLines) {
      try {
        const entry = JSON.parse(line);
        const runId = Number(entry?.run_id);
        const rawAic = entry?.aic;
        const aic = typeof rawAic === "number" ? rawAic : NaN;
        if (Number.isFinite(runId) && runId > 0 && Number.isFinite(aic) && aic >= 0) {
          cache.set(runId, aic);
          loaded++;
        }
      } catch {
        // Ignore malformed lines.
      }
    }
    logDailyGuardrail("Loaded usage cache", { path: cachePath, entriesLoaded: loaded, skippedStale });
  } catch (err) {
    logDailyGuardrail("Failed to load usage cache; proceeding without it", {
      path: cachePath,
      error: typeof err === "object" && err !== null && "message" in err ? String(err.message) : String(err),
    });
  }
  return cache;
}

/**
 * Appends confirmed-zero-AIC run entries to the usage cache file so that
 * subsequent activations can skip re-querying them via the GitHub API.
 * Each entry is written as a JSONL line: `{ run_id, aic: 0, timestamp }`.
 *
 * @param {number[]} runIds  IDs of runs confirmed to have no AIC usage artifact.
 * @param {string} [filePath]  Destination cache file (defaults to {@link AIC_USAGE_CACHE_FILE_PATH}).
 * @returns {void}
 */
function appendZeroAICEntriesToCache(runIds, filePath) {
  if (!Array.isArray(runIds) || runIds.length === 0) {
    return;
  }
  const cachePath = filePath || AIC_USAGE_CACHE_FILE_PATH;
  try {
    const dir = path.dirname(cachePath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    const timestamp = new Date().toISOString();
    const validIds = runIds.filter(id => typeof id === "number" && Number.isFinite(id) && id > 0);
    const lines = validIds.map(id => JSON.stringify({ run_id: id, aic: 0, timestamp })).join("\n");
    if (!lines) {
      return;
    }
    fs.appendFileSync(cachePath, lines + "\n", "utf8");
    logDailyGuardrail("Appended zero-AIC run entries to usage cache", {
      path: cachePath,
      count: validIds.length,
      runIds: validIds,
    });
  } catch (err) {
    logDailyGuardrail("Failed to append zero-AIC entries to usage cache", {
      path: cachePath,
      error: typeof err === "object" && err !== null && "message" in err ? String(err.message) : String(err),
    });
  }
}

/**
 * @param {string} artifactName
 * @returns {boolean}
 */
function matchesGuardrailArtifactName(artifactName) {
  if (!artifactName) {
    return false;
  }
  return PRIMARY_GUARDRAIL_ARTIFACT_NAMES.some(name => artifactName === name || artifactName.endsWith(`-${name}`));
}

/**
 * @param {{ listArtifacts: Function, downloadArtifact: Function }} artifactClient
 * @param {number} runId
 * @param {string} token
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<number>}
 */
async function getRunAIC(artifactClient, runId, token, owner, repo, run, inspection) {
  const components = run ? await loadBillableJobs(inspection, owner, repo, run) : null;
  if (components && allBillableJobsSkipped(components)) return 0;
  const { artifacts } = await artifactClient.listArtifacts({
    latest: true,
    findBy: {
      token,
      workflowRunId: runId,
      repositoryOwner: owner,
      repositoryName: repo,
    },
  });
  const artifactSummaries = artifacts.map(item => ({ id: item?.id ?? null, name: item?.name || "" }));
  logDailyGuardrail("Listed workflow artifacts", {
    runId,
    artifactCount: artifacts.length,
    artifacts: artifactSummaries,
  });

  const artifact = artifacts.find(item => item?.name && matchesGuardrailArtifactName(item.name));
  if (!artifact) {
    if (run) {
      throw new Error(`No usage artifact proves AIC for completed run ${runId}`);
    }
    logDailyGuardrail("No matching guardrail artifact found", {
      runId,
      availableArtifacts: artifactSummaries,
    });
    return 0;
  }
  if (!artifact.id) {
    if (run) throw new Error(`Usage artifact has no identity for completed run ${runId}`);
    logDailyGuardrail("Skipping guardrail artifact without an id", {
      runId,
      artifactName: artifact.name,
    });
    return 0;
  }
  if (run && (artifact.expired || !artifact.createdAt || !Number.isFinite(artifact.createdAt.getTime()))) {
    throw new Error(`Usage artifact does not cover the completed attempt for run ${runId}`);
  }

  logDailyGuardrail("Selected guardrail artifact", {
    runId,
    artifactId: artifact.id,
    artifactName: artifact.name,
  });
  let downloadRoot;
  try {
    downloadRoot = fs.mkdtempSync(path.join(os.tmpdir(), `gh-aw-daily-guardrail-${runId}-`));
  } catch (error) {
    throw new Error(`Failed to create temporary artifact directory for run ${runId}: ${getErrorMessage(error)}`, { cause: error });
  }
  try {
    const download = await artifactClient.downloadArtifact(artifact.id, {
      path: downloadRoot,
      findBy: {
        token,
        workflowRunId: runId,
        repositoryOwner: owner,
        repositoryName: repo,
      },
    });

    const usageJSONLFiles = findJSONLFiles(download.downloadPath || downloadRoot);
    if (run && !components && usageJSONLFiles.length === 0) {
      throw new Error(`Usage artifact contains no accounting records for run ${runId}`);
    }
    logDailyGuardrail("Downloaded guardrail artifact", {
      runId,
      artifactId: artifact.id,
      artifactName: artifact.name,
      downloadPath: download.downloadPath || downloadRoot,
      usageJSONLFiles,
    });
    const aic = components ? sumCoveredComponents(download.downloadPath || downloadRoot, components, artifact.createdAt.getTime(), artifacts, artifact.name, run.run_attempt, run.id) : sumAICFromUsageJSONLFiles(usageJSONLFiles);
    logDailyGuardrail("Computed run AIC from artifact", {
      runId,
      artifactId: artifact.id,
      aic,
    });
    return aic;
  } finally {
    fs.rmSync(downloadRoot, { recursive: true, force: true });
  }
}

/**
 * @param {number | undefined} value
 * @returns {string}
 */
function formatInteger(value) {
  const safeValue = typeof value === "number" && Number.isFinite(value) ? Math.round(value) : 0;
  return INTEGER_FORMATTER.format(safeValue);
}

/**
 * @param {string} raw
 * @returns {string}
 */
function escapeMarkdownCell(raw) {
  return String(raw || "")
    .replace(/\|/g, "\\|")
    .replace(/\n/g, " ");
}

/**
 * @param {number} remaining
 * @returns {number}
 */
function computeMaxInspectableRuns(remaining) {
  if (!Number.isFinite(remaining) || remaining <= 0) {
    return 0;
  }
  // Reserve headroom for the workflow-run listing overhead plus a conservative
  // estimate of two API operations per inspected run (artifact lookup and
  // artifact download). Adjust ESTIMATED_API_OPERATIONS_PER_RUN if observed
  // usage changes.
  return Math.max(0, Math.floor((remaining - RATE_LIMIT_RESERVE - REQUEST_OVERHEAD_BUDGET) / ESTIMATED_API_OPERATIONS_PER_RUN));
}

/**
 * @param {unknown} error
 * @param {number} status
 * @returns {boolean}
 */
function hasHttpStatus(error, status) {
  if (!error || typeof error !== "object") {
    return false;
  }
  const directStatus = "status" in error ? Number(error.status) : NaN;
  if (directStatus === status) {
    return true;
  }
  const responseStatus = "response" in error && error.response && typeof error.response === "object" && "status" in error.response ? Number(error.response.status) : NaN;
  return responseStatus === status;
}

/**
 * Missing resources and invalid permissions need configuration repair.
 * A quota rejection is distinct: its safe headers may specify a retry deadline.
 *
 * @param {unknown} error
 * @returns {boolean}
 */
function isStructuralGuardrailError(error) {
  const response = error && typeof error === "object" && "response" in error ? error.response : null;
  const headers = safeResponseHeaders(response && typeof response === "object" && "headers" in response ? response.headers : null);
  return hasHttpStatus(error, 404) || hasHttpStatus(error, 401) || (hasHttpStatus(error, 403) && headers["x-ratelimit-remaining"] !== "0" && !headers["retry-after"]);
}

/**
 * @typedef {'workflow_id' | 'repo_workflow_name_fallback'} WorkflowRunLookupMode
 */

/**
 * @param {any} githubClient
 * @param {{ owner: string, repo: string, workflowId: number, workflowName: string, page: number, perPage: number, lookupMode: WorkflowRunLookupMode }} params
 * @returns {Promise<{ response: any, lookupMode: WorkflowRunLookupMode, sourceRunCount: number, oldestUnfilteredCreatedAt?: string | null }>}
 */
async function listCompletedWorkflowRunsPage(githubClient, params) {
  const { owner, repo, workflowId, workflowName, page, perPage, lookupMode } = params;
  if (lookupMode === "repo_workflow_name_fallback") {
    const response = await githubClient.rest.actions.listWorkflowRunsForRepo({
      owner,
      repo,
      status: "completed",
      per_page: perPage,
      page,
    });
    const allRuns = response.data.workflow_runs || [];
    const filteredRuns = allRuns.filter(run => (run?.name || "") === workflowName);
    logDailyGuardrail("Filtered repository workflow runs by workflow name fallback", {
      workflowName,
      page,
      totalRunsInPage: allRuns.length,
      matchedRunsInPage: filteredRuns.length,
    });
    // Return the oldest unfiltered run's timestamp so the outer pagination loop
    // can stop early when all remaining runs predate the 24h window, even when
    // none of the runs on this page match the workflow name.
    const lastUnfilteredRun = allRuns[allRuns.length - 1];
    return {
      response: {
        ...response,
        data: {
          ...response.data,
          workflow_runs: filteredRuns,
        },
      },
      lookupMode,
      sourceRunCount: allRuns.length,
      oldestUnfilteredCreatedAt: lastUnfilteredRun?.created_at ?? null,
    };
  }

  try {
    const response = await githubClient.rest.actions.listWorkflowRuns({
      owner,
      repo,
      workflow_id: workflowId,
      status: "completed",
      per_page: perPage,
      page,
    });
    return {
      response,
      lookupMode,
      sourceRunCount: response?.data?.workflow_runs?.length || 0,
    };
  } catch (error) {
    if (!hasHttpStatus(error, 404)) {
      throw error;
    }
    if (!workflowName) {
      // A 404 with no explicit workflow name means we have no meaningful name
      // to filter by in the fallback lookup — rethrow so the outer catch can
      // classify this as a structural error rather than silently failing open.
      throw error;
    }
    logDailyGuardrail("Workflow-specific run history query returned 404; falling back to repository run listing by workflow name", {
      workflowId,
      workflowName,
      page,
    });
    return listCompletedWorkflowRunsPage(githubClient, {
      owner,
      repo,
      workflowId,
      workflowName,
      page,
      perPage,
      lookupMode: "repo_workflow_name_fallback",
    });
  }
}

/**
 * @param {string} workflowName
 * @param {string} actorLogin
 * @param {number} threshold
 * @param {Array<{id:number, html_url:string, created_at:string, conclusion:string, aic:number}>} countedRuns
 * @param {{remaining:number,limit:number,used:number,reset:string}} rateLimit
 * @param {{candidateRunsCount:number,inspectedRunsCount:number,truncatedByRateLimit:boolean}} meta
 * @returns {string}
 */
function renderDailyAICSummary(workflowName, actorLogin, threshold, countedRuns, rateLimit, meta) {
  const stats = calculateDailyAICStats(countedRuns);
  const remainingBudget = Math.max(0, threshold - stats.total);
  const usagePercent = threshold > 0 ? ((stats.total / threshold) * 100).toFixed(2) : "0.00";
  const runRows =
    countedRuns.length > 0
      ? countedRuns
          .slice()
          .sort((a, b) => Date.parse(b.created_at || "") - Date.parse(a.created_at || ""))
          .map(run => `| [#${run.id}](${run.html_url || ""}) | ${escapeMarkdownCell(run.created_at || "")} | ${escapeMarkdownCell(run.conclusion || "unknown")} | ${formatAICCredits(run.aic)} |`)
          .join("\n")
      : "| _none_ | — | — | 0 |";

  const noRunData = stats.count === 0;
  const totalAICFormatted = formatAICCredits(stats.total) || "0";
  const avgAICFormatted = noRunData ? "—" : formatAICCredits(stats.average) || "0";
  const stddevAICFormatted = noRunData ? "—" : formatAICCredits(stats.stddev) || "0";
  const minMaxAICFormatted = noRunData ? "— / —" : `${formatAICCredits(stats.min)} / ${formatAICCredits(stats.max)}`;

  const noteLines = [];
  if (meta.truncatedByRateLimit) {
    noteLines.push(`- Stopped early to preserve GitHub API rate limit headroom (${rateLimit.remaining} remaining, reserve ${RATE_LIMIT_RESERVE}).`);
  }
  if (meta.candidateRunsCount > meta.inspectedRunsCount) {
    noteLines.push(`- Considered ${meta.candidateRunsCount} prior runs in the 24h window and inspected ${meta.inspectedRunsCount}.`);
  }
  return [
    `**Workflow:** ${workflowName || "workflow"}`,
    `**Actor:** ${actorLogin || "unknown"}`,
    "",
    "| Statistic | Value |",
    "| --- | ---: |",
    `| 24h total AIC | ${totalAICFormatted} |`,
    `| Threshold | ${formatAICCredits(threshold)} |`,
    `| Threshold used | ${usagePercent}% |`,
    `| Remaining headroom | ${formatAICCredits(remainingBudget) || "0"} |`,
    `| Runs counted | ${formatInteger(stats.count)} |`,
    `| Avg AIC / run | ${avgAICFormatted} |`,
    `| Std dev AIC | ${stddevAICFormatted} |`,
    `| Min / Max AIC | ${minMaxAICFormatted} |`,
    `| API remaining | ${formatInteger(rateLimit.remaining)} / ${formatInteger(rateLimit.limit)} |`,
    `| API used | ${formatInteger(rateLimit.used)} |`,
    `| API reset | ${rateLimit.reset || "unknown"} |`,
    "",
    "Previous runs counted in the last 24 hours:",
    "",
    "| Run | Created | Conclusion | AIC |",
    "| --- | --- | --- | ---: |",
    runRows,
    ...(noteLines.length > 0 ? ["", ...noteLines] : []),
  ].join("\n");
}

/**
 * @param {string} workflowName
 * @param {string} actorLogin
 * @param {number} threshold
 * @param {Array<{id:number, html_url:string, created_at:string, conclusion:string, aic:number}>} countedRuns
 * @param {{remaining:number,limit:number,used:number,reset:string}} rateLimit
 * @param {{candidateRunsCount:number,inspectedRunsCount:number,truncatedByRateLimit:boolean}} meta
 * @returns {Promise<void>}
 */
async function appendDailyAICSummary(workflowName, actorLogin, threshold, countedRuns, rateLimit, meta) {
  const markdown = renderDailyAICSummary(workflowName, actorLogin, threshold, countedRuns, rateLimit, meta);
  core.summary.addDetails("Daily AI Credits Usage (24h)", "\n\n" + markdown);
  await core.summary.write();
}

/**
 * @returns {Promise<void>}
 *
 * Requires github-script globals (`core`, `github`, `context`) provided by setupGlobals().
 *
 * Incomplete accounting fails activation. Only a complete window may produce an
 * under_budget result; an exceeded budget keeps the existing graceful skip.
 */
async function main(options = {}) {
  core.setOutput("daily_ai_credits_exceeded", "false");
  core.setOutput("daily_ai_credits_total_effective_tokens", "");
  core.setOutput("daily_ai_credits_threshold", "");
  core.setOutput("daily_ai_credits_guardrail_status", "not_run");
  const threshold = parsePositiveCompactNumber(process.env.GH_AW_MAX_DAILY_AI_CREDITS);
  if (threshold <= 0) {
    core.setOutput("daily_ai_credits_guardrail_status", "disabled");
    return;
  }
  if (shouldSkipDailyAICGuardrail()) {
    core.setOutput("daily_ai_credits_guardrail_status", "skipped");
    core.info("Skipping daily workflow AI Credits guardrail for manual or command-driven runs.");
    return;
  }

  const token = process.env.GH_AW_GITHUB_TOKEN || process.env.GITHUB_TOKEN || process.env.GH_TOKEN || "";
  if (!token) {
    core.setOutput("daily_ai_credits_guardrail_status", "structural_error");
    core.setFailed("Daily workflow AI Credits are unknown: no artifact lookup token.");
    return;
  }

  // API failures stop this scan; do not spend more quota on the next history run.
  try {
    const githubClient = createRateLimitAwareGithub(github);
    const budget = createAPIBudget();
    const artifactClient = await module.exports.getArtifactClient(budget.observe);
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || process.env.GH_AW_WORKFLOW_ID || "workflow";
    const { countedRuns, candidateRunsCount, cacheHits, current } = await scanDailyAIC({
      github: githubClient,
      context,
      budget,
      artifactClient,
      getRunAIC: module.exports.getRunAIC,
      listPage: listCompletedWorkflowRunsPage,
      token,
      workflowName: process.env.GH_AW_WORKFLOW_NAME || "",
      cachePath: options.cachePath,
    });
    const totalAIC = countedRuns.reduce((sum, run) => sum + run.aic, 0);
    const actorLogin = process.env.GITHUB_TRIGGERING_ACTOR || current.triggering_actor?.login || current.actor?.login || process.env.GITHUB_ACTOR || "";
    const rateLimit = budget.snapshot();

    core.setOutput("daily_ai_credits_total_effective_tokens", String(totalAIC));
    core.setOutput("daily_ai_credits_threshold", String(threshold));

    /** @type {{candidateRunsCount:number,inspectedRunsCount:number,truncatedByRateLimit:boolean}} */
    const summaryMeta = {
      candidateRunsCount,
      inspectedRunsCount: countedRuns.length,
      truncatedByRateLimit: false,
    };
    logDailyGuardrail("Completed AIC inspection window", {
      // Keep these explicit to preserve existing log shape (exclude truncatedByRateLimit).
      candidateRunsCount: summaryMeta.candidateRunsCount,
      inspectedRunsCount: summaryMeta.inspectedRunsCount,
      countedRunIds: countedRuns.map(run => run.id),
      currentAIC: totalAIC,
      threshold,
      exceeded: totalAIC >= threshold,
    });

    logDailyGuardrail("Daily AIC business API requests", { requests: rateLimit.requests, cacheHits });

    if (totalAIC < threshold) {
      core.setOutput("daily_ai_credits_guardrail_status", "under_budget");
      await appendDailyAICSummary(workflowName, actorLogin, threshold, countedRuns, rateLimit, summaryMeta);
      core.info(`Daily workflow AIC guardrail not exceeded (${totalAIC}/${threshold}).`);
      return;
    }

    core.setOutput("daily_ai_credits_exceeded", "true");
    core.setOutput("daily_ai_credits_guardrail_status", "exceeded");
    try {
      await appendDailyAICSummary(workflowName, actorLogin, threshold, countedRuns, rateLimit, summaryMeta);
    } catch (summaryError) {
      core.warning(`Failed to write daily AIC summary: ${getErrorMessage(summaryError)}`);
    }
    // Log as info so the activation job succeeds. The daily_ai_credits_exceeded output
    // is already set to "true"; the agent job's condition (daily_ai_credits_exceeded != 'true')
    // will skip the agent, and the conclusion job will handle reporting via the
    // daily_ai_credits_exceeded flag. Failing the activation job here causes the overall
    // workflow to fail even though hitting the daily limit is an expected, graceful outcome.
    core.info(`Daily workflow AIC guardrail exceeded for ${workflowName}: ${totalAIC}/${threshold}.`);
  } catch (error) {
    core.setOutput("daily_ai_credits_guardrail_status", isStructuralGuardrailError(error) ? "structural_error" : "transient_error");
    const retryAt = retryNotBefore(error?.response?.headers);
    if (retryAt) core.info(`Daily AIC inspection must not retry before ${retryAt}`);
    core.setFailed(`Daily workflow AI Credits are unknown: ${getErrorMessage(error)}`);
  }
}

module.exports = {
  main,
  getArtifactClient,
  getRunAIC,
  loadAICUsageCache,
  appendZeroAICEntriesToCache,
  shouldSkipDailyAICGuardrail,
  matchesGuardrailArtifactName,
  findJSONLFiles,
  sumAICFromUsageJSONLFiles,
  calculateDailyAICStats,
  computeMaxInspectableRuns,
  renderDailyAICSummary,
  formatDailyGuardrailLogMessage,
  hasHttpStatus,
  isStructuralGuardrailError,
  listCompletedWorkflowRunsPage,
};
