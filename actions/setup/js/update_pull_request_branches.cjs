// @ts-check
const { renderSizedReport } = require("./report_size.cjs");
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { withRetry, isTransientError, sleep } = require("./error_recovery.cjs");
const { fetchAndLogRateLimit } = require("./github_rate_limit_logger.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

const LIST_PULL_REQUESTS_PER_PAGE = 100;
const UPDATE_DELAY_MS = 1000;

/**
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<number[]>}
 */
async function listOpenPullRequests(owner, repo) {
  const pulls = await github.paginate(github.rest.pulls.list, {
    owner,
    repo,
    state: "open",
    per_page: LIST_PULL_REQUESTS_PER_PAGE,
  });

  return pulls.map(pr => pr.number).filter(number => Number.isInteger(number));
}

/**
 * @param {string} owner
 * @param {string} repo
 * @param {number[]} pullNumbers
 * @returns {Promise<number[]>}
 */
async function filterMergeablePullRequests(owner, repo, pullNumbers) {
  const mergeable = [];
  const baseRepository = `${owner}/${repo}`.toLowerCase();

  for (const pullNumber of pullNumbers) {
    const { data: pull } = await withRetry(
      () =>
        github.rest.pulls.get({
          owner,
          repo,
          pull_number: pullNumber,
        }),
      {
        maxRetries: 2,
        initialDelayMs: 500,
        maxDelayMs: 2000,
        jitterMs: 0,
        shouldRetry: isTransientError,
      },
      `fetch pull request #${pullNumber}`
    );

    const headRepositoryRaw = pull?.head?.repo?.full_name;
    const headRepository = headRepositoryRaw?.toLowerCase() ?? "";
    const isSameRepository = headRepository === baseRepository;
    const isMergeable = pull?.state === "open" && pull?.mergeable === true && pull?.draft !== true && isSameRepository;
    if (isMergeable) {
      mergeable.push(pullNumber);
    } else {
      let skipReason = "not_mergeable";
      if (!isSameRepository) {
        skipReason = headRepository ? "head_repository_mismatch" : "head_repository_missing";
      }
      core.info(`Skipping PR #${pullNumber}: reason=${skipReason}, mergeable=${String(pull?.mergeable)}, state=${pull?.state || "unknown"}, draft=${String(Boolean(pull?.draft))}, head_repo=${headRepository || "unknown"}`);
    }
  }

  return mergeable;
}

/**
 * @param {unknown} error
 * @returns {boolean}
 */
function isNonFatalUpdateBranchError(error) {
  if (typeof error === "object" && error !== null && "status" in error && error.status === 422) {
    return true;
  }

  const message = getErrorMessage(error).toLowerCase();
  return message.includes("update branch failed") || message.includes("head branch is not behind");
}

/**
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @returns {Promise<void>}
 */
async function updatePullRequestBranch(owner, repo, pullNumber) {
  await withRetry(
    () =>
      github.rest.pulls.updateBranch({
        owner,
        repo,
        pull_number: pullNumber,
      }),
    {
      maxRetries: 2,
      initialDelayMs: 1000,
      maxDelayMs: 10000,
      shouldRetry: isTransientError,
    },
    `update branch for pull request #${pullNumber}`
  );
}

/**
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @param {string} runUrl
 * @returns {Promise<void>}
 */
async function addMaintenanceUpdateComment(owner, repo, pullNumber, runUrl) {
  const body = renderSizedReport({
    type: "report",
    title: "Branch updated",
    children: [
      { type: "paragraph", children: [{ type: "text", value: "🛠️ Agentic Maintenance updated this pull request branch.", agentTainted: false }] },
      { type: "paragraph", children: [{ type: "link", url: runUrl, children: [{ type: "text", value: "View workflow run", agentTainted: false }] }] },
    ],
  }).body.trimEnd();
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: pullNumber,
    body,
  });
}

/**
 * Update all mergeable PR branches.
 * @returns {Promise<void>}
 */
async function main() {
  const owner = context.repo.owner;
  const repo = context.repo.repo;
  const runUrl = buildWorkflowRunUrl(context, context.repo);

  core.info(`Updating pull request branches in ${owner}/${repo}`);
  core.info(`Run URL: ${runUrl}`);
  await fetchAndLogRateLimit(github, "update_pull_request_branches_start");

  const openPullRequests = await listOpenPullRequests(owner, repo);
  core.info(`Found ${openPullRequests.length} open pull request(s)`);
  if (openPullRequests.length === 0) return;

  const mergeablePullRequests = await filterMergeablePullRequests(owner, repo, openPullRequests);
  core.info(`Found ${mergeablePullRequests.length} mergeable pull request(s)`);
  if (mergeablePullRequests.length === 0) return;

  let updatedCount = 0;
  let skippedCount = 0;
  let failedCount = 0;

  for (const [i, pullNumber] of mergeablePullRequests.entries()) {
    if (i > 0) {
      await sleep(UPDATE_DELAY_MS);
    }
    try {
      core.info(`Updating branch for PR #${pullNumber}`);
      await updatePullRequestBranch(owner, repo, pullNumber);
      await addMaintenanceUpdateComment(owner, repo, pullNumber, runUrl);
      updatedCount++;
    } catch (error) {
      if (isNonFatalUpdateBranchError(error)) {
        skippedCount++;
        core.warning(`Skipping PR #${pullNumber}: ${getErrorMessage(error)}`);
      } else {
        failedCount++;
        core.error(`Failed to update branch for PR #${pullNumber}: ${getErrorMessage(error)}`);
      }
    }
  }

  await fetchAndLogRateLimit(github, "update_pull_request_branches_end");
  core.notice(`update_pull_request_branches completed: updated=${updatedCount}, skipped=${skippedCount}, failed=${failedCount}`);
}

module.exports = {
  main,
  filterMergeablePullRequests,
  isNonFatalUpdateBranchError,
};
