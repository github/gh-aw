// @ts-check
/// <reference types="@actions/github-script" />

const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { fetchAndLogRateLimit } = require("./github_rate_limit_logger.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");
const { generateFooterWithExpiration, createExpirationLine } = require("./ephemerals.cjs");
const { generateXMLMarker } = require("./messages.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { redactBuiltInPatterns } = require("./redact_secrets.cjs");

const GITHUB_API_VERSION = "2022-11-28";
const FAILED_JOBS_ISSUE_EXPIRES_HOURS = 24 * 7; // 1 week
const STATE_PUSH_JOB_IDS = new Set(["push_ledger_changes", "push_repo_memory", "push_evals_state", "push_experiment_state"]);

/**
 * Read only error lines from an already-masked state-push job log.
 * @param {number} jobId
 * @returns {Promise<string>}
 */
async function getStatePushError(jobId) {
  try {
    const response = await github.rest.actions.downloadJobLogsForWorkflowRun({ ...context.repo, job_id: jobId });
    const log = typeof response.data === "string" ? response.data : Buffer.from(response.data).toString("utf8");
    const errors = log.split(/\r?\n/).filter(line => /##\[error\]|fatal:|error:|remote: error:|!\s+\[rejected\]/i.test(line));
    const detail = redactBuiltInPatterns(errors.slice(-20).join("\n"))
      .content.replace(/(https?:\/\/)[^/\s@]+@/gi, "$1[REDACTED]@")
      .replace(/(authorization:\s*(?:bearer|basic)\s+)\S+/gi, "$1[REDACTED]");
    return sanitizeContent(detail, 4000).replace(/```/g, "``\u200b`");
  } catch {
    core.warning(`Could not retrieve error details for state-push job ${jobId}; see the job log`);
    return "";
  }
}

/**
 * Stable builtin job IDs whose failures are already reported
 * by the handle_agent_failure step (the agent job) or are framework-internal
 * (conclusion = current job, pre_activation/activation = reported via agent failure issue flags,
 * safe-outputs/safe_outputs and detection = handled by dedicated conclusion reporting).
 */
const BUILTIN_REPORTED_JOB_IDS = new Set(["agent", "conclusion", "activation", "pre_activation", "pre-activation", "safe_outputs", "safe-outputs", "detection"]);

/**
 * Check whether an error is a GitHub permission error for issues write.
 * @param {unknown} error
 * @returns {boolean}
 */
function isIssueWritePermissionError(error) {
  if (!(error instanceof Error)) return false;
  const msg = error.message.toLowerCase();
  return msg.includes("resource not accessible") || msg.includes("must have push access") || msg.includes("403");
}

/**
 * Check whether an error is a GitHub permission error for actions read.
 * @param {unknown} error
 * @returns {boolean}
 */
function isActionsReadPermissionError(error) {
  if (!(error instanceof Error)) return false;
  const msg = error.message.toLowerCase();
  return msg.includes("resource not accessible") || msg.includes("403");
}

/**
 * Format the list of failed jobs as a markdown bulleted list.
 * @param {Array<{name: string, html_url: string | null, error_detail?: string}>} jobs
 * @returns {string}
 */
function formatFailedJobsList(jobs) {
  return jobs
    .map(job => {
      const safeName = sanitizeContent(job.name);
      const detail = job.error_detail
        ? `\n\n  \`\`\`text\n${job.error_detail
            .split("\n")
            .map(line => `  ${line}`)
            .join("\n")}\n  \`\`\`\n`
        : "";
      if (job.html_url && job.html_url.startsWith("https://")) {
        const safeUrl = sanitizeContent(job.html_url);
        return `- [\`${safeName}\`](${safeUrl})${detail}`;
      }
      return `- \`${safeName}\`${detail}`;
    })
    .join("\n");
}

/**
 * Filter failures by stable IDs from needs; use the jobs API only for display names and links.
 * @returns {Promise<Array<{name: string, html_url: string | null}>>}
 */
async function getFailedNonBuiltinJobs() {
  let jobResults;
  let jobDisplayNames;
  try {
    jobResults = JSON.parse(process.env.GH_AW_JOB_RESULTS || "");
    jobDisplayNames = JSON.parse(process.env.GH_AW_JOB_DISPLAY_NAMES || "");
  } catch (error) {
    throw new Error(`${SAFE_OUTPUT_E001}: Failed to parse failed-job reporting metadata: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!jobResults || typeof jobResults !== "object" || Array.isArray(jobResults) || !jobDisplayNames || typeof jobDisplayNames !== "object" || Array.isArray(jobDisplayNames)) {
    throw new Error("GH_AW_JOB_RESULTS and GH_AW_JOB_DISPLAY_NAMES must be JSON objects");
  }
  const failedJobIds = Object.keys(jobResults).filter(jobId => {
    if (jobResults[jobId]?.result !== "failure") return false;
    if (BUILTIN_REPORTED_JOB_IDS.has(jobId)) {
      core.info(`Skipping builtin job ID: ${jobId} (handled by dedicated failure reporting)`);
      return false;
    }
    return true;
  });
  if (failedJobIds.length === 0) return [];

  const { owner, repo } = context.repo;
  const runId = context.runId;

  core.info(`Querying jobs for workflow run ${runId}`);

  /** @type {Array<{name: string, html_url: string | null, id?: number, error_detail?: string}>} */
  const runJobs = [];

  let page = 1;
  const perPage = 100;

  while (true) {
    const response = await github.rest.actions.listJobsForWorkflowRun({
      owner,
      repo,
      run_id: runId,
      per_page: perPage,
      page,
      filter: "latest",
    });

    const jobs = response.data.jobs;
    core.info(`Page ${page}: retrieved ${jobs.length} jobs`);

    for (const job of jobs) {
      if (job.conclusion !== "failure") {
        continue;
      }
      runJobs.push({ name: job.name, html_url: job.html_url, ...(job.id ? { id: job.id } : {}) });
    }

    if (jobs.length < perPage) {
      break;
    }
    page++;
  }

  const matchesName = (name, display) => name === display || name.startsWith(`${display} (`) || name.startsWith(`${display} / `);
  const knownIds = new Set([...Object.keys(jobResults), ...Object.keys(jobDisplayNames)]);
  const failedJobs = [];
  for (const jobId of failedJobIds) {
    const displayName = jobDisplayNames[jobId] || jobId;
    const matchingJobs = runJobs.filter(job => matchesName(job.name, displayName));
    const ambiguousName = [...knownIds].some(otherId => otherId !== jobId && matchingJobs.some(job => matchesName(job.name, jobDisplayNames[otherId] || otherId)));
    if (matchingJobs.length > 0 && !ambiguousName) {
      for (const job of matchingJobs) {
        const detail = STATE_PUSH_JOB_IDS.has(jobId) && job.id ? await getStatePushError(job.id) : "";
        failedJobs.push({ name: job.name, html_url: job.html_url, ...(detail ? { error_detail: detail } : {}) });
      }
      continue;
    }
    core.warning(`Could not uniquely match failed job ID ${jobId} to its API display name; linking to the workflow run`);
    failedJobs.push({ name: jobId, html_url: process.env.GH_AW_RUN_URL || null });
  }
  return failedJobs;
}

/**
 * Report failed non-builtin jobs by creating a GitHub issue.
 * Checks API rate limits before and after querying the jobs list.
 * Respects GH_AW_REPORT_FAILED_JOBS env var (default: true).
 */
async function main() {
  try {
    // Check if reporting is enabled
    const reportFailedJobs = parseBoolTemplatable(process.env.GH_AW_REPORT_FAILED_JOBS, true);
    if (!reportFailedJobs) {
      core.info("Failed jobs reporting is disabled (report-failed-jobs: false), skipping");
      return;
    }

    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const { owner, repo } = context.repo;

    // Check rate limit before querying jobs
    core.info("Checking GitHub API rate limit before querying jobs");
    await fetchAndLogRateLimit(github, "report_failed_jobs_before");

    /** @type {Array<{name: string, html_url: string | null}>} */
    let failedJobs;
    try {
      failedJobs = await getFailedNonBuiltinJobs();
    } catch (error) {
      if (isActionsReadPermissionError(error)) {
        core.info(`Skipping failed jobs reporting: token lacks actions:read permission (${getErrorMessage(error)})`);
        return;
      }
      core.warning(`Failed to query jobs for run: ${getErrorMessage(error)}`);
      return;
    } finally {
      // Check rate limit after querying jobs
      core.info("Checking GitHub API rate limit after querying jobs");
      await fetchAndLogRateLimit(github, "report_failed_jobs_after");
    }

    if (failedJobs.length === 0) {
      core.info("No failed non-builtin jobs found, skipping issue creation");
      return;
    }

    core.info(`Found ${failedJobs.length} failed non-builtin job(s): ${failedJobs.map(j => j.name).join(", ")}`);

    // Render the issue body from template
    const failedJobsList = formatFailedJobsList(failedJobs);
    const templatePath = getPromptPath("failed_jobs_issue.md");
    const issueBodyContent = renderTemplateFromFile(templatePath, {
      workflow_name: workflowName,
      workflow_source_url: workflowSourceURL,
      run_url: runUrl,
      failed_jobs_list: failedJobsList,
    });

    const xmlMarker = generateXMLMarker(workflowName, runUrl);
    const issueBody = generateFooterWithExpiration({
      footerText: `${issueBodyContent}\n\n${xmlMarker}`,
      expiresHours: FAILED_JOBS_ISSUE_EXPIRES_HOURS,
    });

    const issueTitle = `[aw] Failed jobs: ${workflowName}`;

    core.info(`Creating failed jobs issue: "${issueTitle}"`);

    try {
      const newIssue = await github.rest.issues.create({
        owner,
        repo,
        title: issueTitle,
        body: issueBody,
        labels: ["agentic-workflows"],
        headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
      });

      core.info(`Created failed jobs issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    } catch (error) {
      if (isIssueWritePermissionError(error)) {
        core.info(`Skipping failed jobs issue creation: token lacks issues:write permission (${getErrorMessage(error)})`);
      } else {
        core.warning(`Failed to create failed jobs issue: ${getErrorMessage(error)}`);
      }
    }
  } catch (error) {
    core.warning(`report_failed_jobs: unexpected error: ${getErrorMessage(error)}`);
  }
}

module.exports = { main, getFailedNonBuiltinJobs, formatFailedJobsList, BUILTIN_REPORTED_JOB_IDS };
