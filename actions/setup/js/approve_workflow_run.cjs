// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { matchesSimpleGlob } = require("./glob_pattern_helpers.cjs");
const { isRepoAllowed, parseAllowedRepos } = require("./repo_helpers.cjs");
const { SAFE_OUTPUT_E007, POLICY_FILE_PROTECTION_DENIED_REASON_CODE } = require("./error_codes.cjs");
const path = require("node:path");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { getRunStartedMessage } = require("./messages_run_status.cjs");
const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "approve_workflow_run";

/**
 * Workflow run states that the workflow run approval endpoint can act on.
 *
 * A pull request run that is held for maintainer approval (a fork pull request or an
 * agent-initiated pull request) is reported by the REST API with status "action_required".
 * Some runs are additionally reported as "waiting" while GitHub is still gating them, so
 * both states are accepted here.
 * Any other status means there is nothing left to approve.
 *
 * @type {Set<string>}
 */
const APPROVABLE_RUN_STATUSES = new Set(["action_required", "waiting"]);

/**
 * Detect permission-class API failures that should fail fast without retrying.
 *
 * @param {unknown} error
 * @returns {boolean}
 */
function isPermissionDeniedError(error) {
  const status = typeof error === "object" && error !== null && "status" in error ? error.status : undefined;
  const normalized = getErrorMessage(error).toLowerCase();
  const isKnownPermissionMessage = normalized.includes("resource not accessible by personal access token") || normalized.includes("resource not accessible by integration");
  return isKnownPermissionMessage || (status === 403 && normalized.includes("resource not accessible"));
}

/**
 * Determine whether a workflow run is still awaiting approval.
 *
 * The pending-approval state is surfaced either on the run's `status` or, for runs
 * that GitHub has already marked as completed pending an approval decision, on the
 * run's `conclusion`.
 *
 * @param {{status?: string | null, conclusion?: string | null}} run
 * @returns {boolean}
 */
function isAwaitingApproval(run) {
  return APPROVABLE_RUN_STATUSES.has(run?.status || "") || run?.conclusion === "action_required";
}

/**
 * @param {unknown} value
 * @returns {number | undefined}
 */
function parsePositiveInt(value) {
  if (typeof value !== "number" && typeof value !== "string") return undefined;
  const normalized = typeof value === "string" ? value.trim() : value;
  if (normalized === "") return undefined;
  const runId = Number(normalized);
  if (!Number.isSafeInteger(runId) || runId <= 0) return undefined;
  return runId;
}

/**
 * @param {unknown} value
 * @returns {Set<number>}
 */
function parseAllowedPullRequests(value) {
  let normalized = value;
  if (typeof normalized === "string") {
    const trimmed = normalized.trim();
    if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
      try {
        normalized = JSON.parse(trimmed);
      } catch {
        normalized = value;
      }
    }
  }
  const parsed = (Array.isArray(normalized) ? normalized : [normalized]).map(parsePositiveInt).filter(candidate => candidate !== undefined);
  return new Set(parsed);
}

/**
 * @param {unknown} value
 * @returns {string | undefined}
 */
function normalizeWorkflowFilename(value) {
  if (typeof value !== "string" || value === "") return undefined;
  return path.posix.basename(value).replace(/\.yaml$/i, ".yml");
}

/**
 * @param {unknown} workflowPath
 * @param {unknown} allowedWorkflows
 * @returns {boolean}
 */
function isAllowedWorkflow(workflowPath, allowedWorkflows) {
  const filename = normalizeWorkflowFilename(workflowPath);
  if (!filename || !Array.isArray(allowedWorkflows) || allowedWorkflows.length === 0) return false;

  return allowedWorkflows.some(pattern => {
    if (typeof pattern !== "string" || path.posix.basename(pattern) !== pattern) return false;
    const normalizedPattern = normalizeWorkflowFilename(pattern);
    return normalizedPattern !== undefined && matchesSimpleGlob(filename, normalizedPattern);
  });
}

/**
 * @returns {number | undefined}
 */
function getCurrentPullRequestNumber() {
  const payload = context.payload || {};
  return parsePositiveInt(payload.pull_request?.number || (payload.issue?.pull_request ? payload.issue.number : undefined));
}

/**
 * @param {any} githubClient
 * @param {number} pullRequestNumber
 * @returns {Promise<string[]>}
 */
async function getModifiedPullRequestFiles(githubClient, pullRequestNumber) {
  const files = await githubClient.paginate(githubClient.rest.pulls.listFiles, {
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: pullRequestNumber,
    per_page: 100,
  });
  if (!Array.isArray(files) || files.some(file => typeof file?.filename !== "string")) {
    throw new Error(`${SAFE_OUTPUT_E007}: Unable to verify modified files for pull request #${pullRequestNumber}`);
  }
  return files.map(file => file.filename);
}

/**
 * Resolve the repository that owns the head branch of a pull request.
 *
 * The head repository identifies where the pull request branch lives: the current
 * repository for agent-initiated pull requests (for example `copilot/*` branches),
 * or a fork for contributor pull requests.
 *
 * @param {any} githubClient
 * @param {number} pullRequestNumber
 * @returns {Promise<string>} Head repository slug in "owner/repo" format
 */
async function getPullRequestHeadRepo(githubClient, pullRequestNumber) {
  const { data: pullRequest } = await githubClient.rest.pulls.get({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: pullRequestNumber,
  });
  if (!pullRequest?.head?.repo) {
    throw new Error(`${SAFE_OUTPUT_E007}: Cannot approve pull request #${pullRequestNumber}: its head repository is unavailable`);
  }
  const fullName = pullRequest.head.repo.full_name;
  if (typeof fullName !== "string" || fullName === "") {
    throw new Error(`${SAFE_OUTPUT_E007}: Unable to verify the head repository for pull request #${pullRequestNumber}`);
  }
  return fullName;
}

/**
 * Determine whether a pull request head repository may have its workflow runs approved.
 *
 * The current repository is always allowed, so pull requests opened from branches in this
 * repository (including agent-initiated `copilot/*` pull requests) are approvable by default.
 * Fork pull requests are approvable only when their repository matches an `allowed-repos` entry.
 *
 * @param {string} headRepo - Head repository slug in "owner/repo" format
 * @param {Set<string>} allowedRepos - Allowed repository patterns from configuration
 * @returns {boolean}
 */
function isHeadRepoAllowed(headRepo, allowedRepos) {
  const currentRepo = `${context.repo.owner}/${context.repo.repo}`;
  return headRepo === currentRepo || isRepoAllowed(headRepo, allowedRepos);
}

/**
 * Build the comment body announcing that an approved workflow run has started.
 * @param {string} runHtmlUrl - HTML URL of the approved workflow run
 * @param {number|undefined} pullRequestNumber - Pull request number the comment is posted on
 * @returns {string} The complete comment body with attribution footer
 */
function buildApprovalCommentBody(runHtmlUrl, pullRequestNumber) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runUrl = buildWorkflowRunUrl(context, context.repo);

  const message = sanitizeContent(getRunStartedMessage({ workflowName, runUrl: runHtmlUrl, eventType: "pull request" }));
  const footer = generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, undefined, pullRequestNumber, undefined, undefined);
  return `${message}\n\n${footer}`;
}

/**
 * Post a comment on the pull request announcing that the approved workflow run has started.
 * Failures are logged as warnings and never fail the overall approval.
 * @param {any} githubClient
 * @param {number} pullRequestNumber
 * @param {string} runHtmlUrl
 */
async function postApprovalComment(githubClient, pullRequestNumber, runHtmlUrl) {
  try {
    await githubClient.rest.issues.createComment({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: pullRequestNumber,
      body: buildApprovalCommentBody(runHtmlUrl, pullRequestNumber),
    });
  } catch (error) {
    core.warning(`Failed to post approval comment on pull request #${pullRequestNumber}: ${getErrorMessage(error)}`);
  }
}

/**
 * Main handler factory for approve_workflow_run.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const maxCount = config.max || 1;
  const isStaged = isStagedMode(config);
  const githubToken = config["github-token"];
  const allowedRepos = parseAllowedRepos(config.allowed_repos);
  let processedCount = 0;
  // main() is constructed once per safe-outputs pass; cache token-level approval
  // denials on this handler instance so subsequent messages can fail fast.
  let approvePermissionDenied = false;

  core.info(`Approve workflow run configuration: max=${maxCount}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed pull request repositories (in addition to the current repository): ${Array.from(allowedRepos).join(", ")}`);
  }

  if (!isStaged && !githubToken) {
    const error = "approve_workflow_run requires an external github-token or GitHub App token";
    core.error(error);
    return async () => ({ success: false, error });
  }

  const githubClient = isStaged ? null : await createAuthenticatedGitHubClient(config);

  return async function handleApproveWorkflowRun(message) {
    if (approvePermissionDenied) {
      const reason = "Skipping approve_workflow_run because this token cannot approve workflow runs (permission denied on a prior attempt)";
      core.warning(reason);
      return {
        success: false,
        skipped: true,
        reasonCode: "APPROVE_WORKFLOW_RUN_PERMISSION_DENIED",
        reason,
        error: reason,
      };
    }

    const runId = parsePositiveInt(message.run_id);
    if (!runId) {
      const error = "run_id must be a positive integer";
      core.warning(error);
      return { success: false, error };
    }

    if (context.eventName === "pull_request_target") {
      const error = "approve_workflow_run cannot run on pull_request_target events; use pull_request instead";
      core.warning(error);
      return { success: false, error };
    }

    if (isStaged) {
      logStagedPreviewInfo(`Would approve workflow run ${runId}`);
      return { success: true, staged: true, run_id: runId };
    }

    if (processedCount >= maxCount) {
      core.warning(`Skipping ${HANDLER_TYPE}: max count of ${maxCount} reached`);
      return {
        success: false,
        skipped: true,
        reasonCode: "MAX_COUNT_REACHED",
        reason: "Max count reached",
        error: `Max count of ${maxCount} reached`,
      };
    }

    try {
      const { data: run } = await githubClient.rest.actions.getWorkflowRun({
        owner: context.repo.owner,
        repo: context.repo.repo,
        run_id: runId,
      });

      if (run.event !== "pull_request" || !Array.isArray(run.pull_requests) || run.pull_requests.length === 0) {
        const error = `Workflow run ${runId} is not associated with a pull request`;
        core.warning(error);
        return { success: false, error };
      }

      if (!isAwaitingApproval(run)) {
        // Benign race: by the time the safe_outputs job runs, the workflow run may have
        // already been approved (by a human or an earlier run) and moved past the
        // pending-approval state. There is nothing left to do, so report this as a skipped
        // no-op instead of a failure that would fail the whole safe outputs step.
        const reason = `Workflow run ${runId} is not awaiting approval (status: ${run.status || "none"})`;
        core.warning(reason);
        return { success: false, skipped: true, reasonCode: "NOT_AWAITING_APPROVAL", reason, error: reason };
      }

      const workflowId = parsePositiveInt(run.workflow_id);
      if (!workflowId) {
        const error = `Workflow run ${runId} has invalid workflow data`;
        core.warning(error);
        return { success: false, error };
      }
      const { data: workflow } = await githubClient.rest.actions.getWorkflow({
        owner: context.repo.owner,
        repo: context.repo.repo,
        workflow_id: workflowId,
      });
      if (!isAllowedWorkflow(workflow?.path, config.allowed_workflows)) {
        const error = `Workflow run ${runId} does not match an allowed workflow`;
        core.warning(error);
        return { success: false, error };
      }

      const allowedPullRequests = parseAllowedPullRequests(config.allowed_pull_requests);
      const currentPullRequest = getCurrentPullRequestNumber();
      const isAuthorized = run.pull_requests.every(pullRequest => {
        const pullRequestNumber = parsePositiveInt(pullRequest.number);
        return pullRequestNumber !== undefined && ((currentPullRequest !== undefined && pullRequestNumber === currentPullRequest) || allowedPullRequests.has(pullRequestNumber));
      });
      if (!isAuthorized) {
        const error = `Workflow run ${runId} is not associated exclusively with the triggering pull request or explicitly allowed pull requests`;
        core.warning(error);
        return { success: false, error };
      }

      for (const pullRequest of run.pull_requests) {
        const pullRequestNumber = parsePositiveInt(pullRequest.number);
        if (pullRequestNumber === undefined) {
          const error = `Workflow run ${runId} has invalid pull request data`;
          core.warning(error);
          return { success: false, error };
        }
        const headRepo = await getPullRequestHeadRepo(githubClient, pullRequestNumber);
        if (!isHeadRepoAllowed(headRepo, allowedRepos)) {
          const hint = allowedRepos.size === 0 ? "configure allowed-repos to approve pull requests from other repositories" : `allowed repositories: ${Array.from(allowedRepos).join(", ")}`;
          const error = `Workflow run ${runId} cannot be approved because pull request #${pullRequestNumber} comes from repository ${headRepo}, which is not in the allowed-repos list (${hint})`;
          core.warning(error);
          return { success: false, error };
        }
        const files = await getModifiedPullRequestFiles(githubClient, pullRequestNumber);
        const protection = checkFileProtectionPostApply(files, {
          ...config,
          protected_files_policy: "blocked",
        });
        if (protection.action !== "allow") {
          const error = `Workflow run ${runId} cannot be approved because pull request #${pullRequestNumber} modifies protected files (${protection.files.join(", ")})`;
          core.warning(error);
          return { success: false, skipped: true, reasonCode: POLICY_FILE_PROTECTION_DENIED_REASON_CODE, error };
        }
      }

      try {
        await githubClient.rest.actions.approveWorkflowRun({
          owner: context.repo.owner,
          repo: context.repo.repo,
          run_id: runId,
        });
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        if (isPermissionDeniedError(error)) {
          approvePermissionDenied = true;
          const reason = `Skipping approval for workflow run ${runId} because the configured token cannot approve workflow runs (${errorMessage})`;
          core.warning(reason);
          return {
            success: false,
            skipped: true,
            reasonCode: "APPROVE_WORKFLOW_RUN_PERMISSION_DENIED",
            reason,
            error: reason,
          };
        }
        throw error;
      }
      processedCount++;

      core.info(`Approved workflow run ${runId}: ${run.html_url}`);

      if (config.comment !== false) {
        for (const pullRequest of run.pull_requests) {
          const pullRequestNumber = parsePositiveInt(pullRequest.number);
          if (pullRequestNumber !== undefined) {
            await postApprovalComment(githubClient, pullRequestNumber, run.html_url);
          }
        }
      }

      return { success: true, run_id: runId, url: run.html_url };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to approve workflow run ${runId}: ${errorMessage}`);
      return { success: false, error: errorMessage };
    }
  };
}

module.exports = {
  main,
  parseAllowedPullRequests,
  parsePositiveInt,
  normalizeWorkflowFilename,
  isAllowedWorkflow,
  isAwaitingApproval,
  isPermissionDeniedError,
  getModifiedPullRequestFiles,
  getPullRequestHeadRepo,
  isHeadRepoAllowed,
  buildApprovalCommentBody,
  postApprovalComment,
};
