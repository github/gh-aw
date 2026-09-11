// @ts-check
/// <reference types="@actions/github-script" />
// @safe-outputs-exempt SEC-004 — body sanitized transitively via sanitizeContent in update_handler_factory.cjs / update_pr_description_helpers.cjs

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "update_pull_request";

const { buildUpdatedBody } = require("./update_pr_description_helpers.cjs");
const { resolveTarget, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { createUpdateHandlerFactory, createStandardResolveNumber, createStandardFormatResult } = require("./update_handler_factory.cjs");
const { buildCommonEntityUpdateData } = require("./update_entity_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { fetchPullRequestState, mergePullRequestState } = require("./safe_output_execution_metadata.cjs");
const { withRetry, isTransientError } = require("./error_recovery.cjs");

/**
 * @param {unknown} error
 * @returns {number|undefined}
 */
function getErrorStatus(error) {
  // Resolve the effective HTTP status by checking the error and its .originalError chain.
  // withRetry wraps the original error in an enhanced error that lacks .status, so we need
  // to walk the chain to find the underlying status from the GitHub API response.
  /** @type {number | undefined} */
  let status;
  /** @type {any} */
  let current = error;
  while (current !== null && typeof current === "object") {
    if ("status" in current && typeof current.status === "number") {
      status = current.status;
      break;
    }
    current = current.originalError ?? null;
  }
  return status;
}

/**
 * @param {unknown} error
 * @returns {boolean}
 */
function isStackedPRUnsupportedUpdateBranchError(error) {
  const status = getErrorStatus(error);
  const message = getErrorMessage(error).toLowerCase();
  return (status === 403 || status === 422) && message.includes("updating a stacked pr's branch via this endpoint is not supported");
}

/**
 * @param {unknown} error
 * @returns {boolean}
 */
function isNonFatalUpdateBranchError(error) {
  const status = getErrorStatus(error);
  const message = getErrorMessage(error).toLowerCase();
  const hasWorkflowsPermissionPhrase = /without\s+`?workflows`?\s+permission/i.test(message);
  const hasWorkflowMutationRefusal = message.includes("refusing to allow a github app to create or update workflow");
  // Require both permission wording and update-branch context to avoid treating unrelated
  // "workflows permission" errors as non-fatal for pull request branch updates.
  const hasWorkflowsPermissionError = hasWorkflowsPermissionPhrase && (hasWorkflowMutationRefusal || message.includes("update pull request"));
  // GitHub update-branch API also returns 403 with this message when a PR contains workflow
  // file changes and the check times out, rather than the usual "refusing to allow" phrase.
  const hasWorkflowsScopeRequired = message.includes("`workflows` scope may be required") || message.includes("unable to determine if workflow can be created or updated");

  if (status !== undefined) {
    if (status === 403 && (hasWorkflowsPermissionError || hasWorkflowsScopeRequired)) {
      core.info(`Treating update-branch error as non-fatal: workflows permission/scope restriction on 403 (status=${status})`);
      return true;
    }
    if (status !== 422 && !message.includes("head ref does not exist")) {
      core.info(`Treating update-branch error as fatal: status is not 422 and head-ref is not missing (status=${status})`);
      return false;
    }
  }

  // GitHub update-branch API can return these 422 messages for benign conditions:
  // - already up to date ("There are no new commits on the base branch")
  // - cannot auto-update due to conflict ("merge conflict between base and head")
  // - stale merged targets where the head branch was deleted ("head ref does not exist")
  // These should not fail safe output processing.
  // A deleted head ref is a stale target regardless of which numeric status the API/proxy reports.
  // Keep requiring some numeric status so transport/proxy failures with no HTTP response still fail.
  // Restrict the other phrases to status === 422 to avoid silently swallowing proxy/network
  // errors. hasWorkflowsPermissionError / hasWorkflowsScopeRequired are only checked for errors
  // with no numeric status (status === undefined); the explicit 403 case is already handled by
  // the if-block above.
  const isNonFatal =
    (status !== undefined && message.includes("head ref does not exist")) ||
    (status === 422 && (message.includes("there are no new commits on the base branch") || message.includes("merge conflict between base and head"))) ||
    ((hasWorkflowsPermissionError || hasWorkflowsScopeRequired) && status === undefined);
  const finalReason = isNonFatal ? "matched known benign update-branch validation/scope condition" : "did not match any known benign update-branch condition";
  core.info(`Treating update-branch error as ${isNonFatal ? "non-fatal" : "fatal"}: ${finalReason} (status=${status ?? "unknown"})`);
  return isNonFatal;
}

/**
 * @param {any} github
 * @param {any} context
 * @param {number} prNumber
 * @returns {Promise<boolean>}
 */
async function tryStackedPRUpdateBranch(github, context, prNumber) {
  /** @type {number|undefined} */
  let stackNumber;

  try {
    const { data: stacks } = await github.request("GET /repos/{owner}/{repo}/stacks", {
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_request: prNumber,
      per_page: 1,
    });
    if (Array.isArray(stacks) && stacks.length > 0 && typeof stacks[0]?.number === "number") {
      stackNumber = stacks[0].number;
    }
  } catch (error) {
    core.info(`Unable to resolve stack number from stack list for #${prNumber}: ${getErrorMessage(error)}; skipping stack sync`);
    return false;
  }

  if (stackNumber === undefined) {
    core.info(`Unable to sync stacked PR #${prNumber}: no stack number could be resolved`);
    return false;
  }

  try {
    await github.request("POST /repos/{owner}/{repo}/stacks/{stack_number}/sync", {
      owner: context.repo.owner,
      repo: context.repo.repo,
      stack_number: stackNumber,
    });
    core.info(`Synced stacked PR #${prNumber} via stack #${stackNumber}`);
    return true;
  } catch (error) {
    core.info(`Unable to sync stacked PR #${prNumber} via stack #${stackNumber}: ${getErrorMessage(error)}`);
    return false;
  }
}

/**
 * Execute the pull request update API call
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {number} prNumber - PR number to update
 * @param {any} updateData - Data to update
 * @returns {Promise<any>} Updated pull request
 */
async function executePRUpdate(github, context, prNumber, updateData) {
  // Handle body operation (append/prepend/replace/replace-island)
  const operation = updateData._operation || "replace";
  const rawBody = updateData._rawBody;
  const includeFooter = updateData._includeFooter !== false; // Default to true
  const bodyFooter = updateData._bodyFooter;

  // Remove internal fields (including update_branch which is handled separately below)
  const { _operation, _rawBody, _includeFooter, _bodyFooter, _workflowRepo, _update_branch_stacks, update_branch, ...apiData } = updateData;
  const updateBranch = update_branch === true;
  const updateBranchStacksEnabled = _update_branch_stacks !== false;

  if (updateBranch) {
    core.info(`update_branch stacked PR sync fallback is ${updateBranchStacksEnabled ? "enabled" : "disabled"} for pull request #${prNumber}`);
    core.info(`Updating pull request #${prNumber} branch with base branch changes`);
    try {
      await withRetry(
        () =>
          github.rest.pulls.updateBranch({
            owner: context.repo.owner,
            repo: context.repo.repo,
            pull_number: prNumber,
          }),
        {
          maxRetries: 1,
          initialDelayMs: 0,
          jitterMs: 0,
          shouldRetry: isTransientError,
        },
        `update pull request #${prNumber} branch from base`
      );
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      if (isStackedPRUnsupportedUpdateBranchError(error)) {
        if (updateBranchStacksEnabled) {
          core.info(`Attempting stacked PR stack-sync fallback for pull request #${prNumber} after update_branch 422 unsupported response`);
          const didSyncStack = await tryStackedPRUpdateBranch(github, context, prNumber);
          if (didSyncStack) {
            core.info(`Updated stacked PR #${prNumber} branch via stack sync`);
          } else {
            core.warning(`Failed to update pull request #${prNumber} branch from base (non-fatal): ${errorMessage}`);
          }
        } else {
          core.info(`Skipping stacked PR stack-sync fallback for pull request #${prNumber}: update_branch_stacks=false`);
          core.warning(`Failed to update pull request #${prNumber} branch from base (non-fatal): ${errorMessage}`);
        }
      } else if (isNonFatalUpdateBranchError(error)) {
        core.warning(`Failed to update pull request #${prNumber} branch from base (non-fatal): ${errorMessage}`);
      } else {
        core.warning(`Failed to update pull request #${prNumber} branch from base: ${errorMessage}`);
        throw error;
      }
    }
  }

  // If we have a body, process it with the appropriate operation
  if (rawBody !== undefined) {
    // Fetch current PR body for all operations (needed for append/prepend/replace-island/replace)
    const { data: currentPR } = await github.rest.pulls.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: prNumber,
    });
    const currentBody = currentPR.body || "";

    apiData.body = buildUpdatedBody({
      context,
      currentBody,
      newContent: rawBody,
      operation,
      includeFooter,
      bodyFooter,
      workflowRepo: _workflowRepo,
      itemType: "pull_request",
    });

    core.info(`Will update body (length: ${apiData.body.length})`);
  }

  if (Object.keys(apiData).length === 0) {
    // update_branch-only operations need the authoritative post-update PR state so the
    // manifest can persist after_state fields such as head_sha/base/draft for later
    // retained-update evaluation. A synthetic {number, html_url} result is not enough.
    const { data: pullRequest } = await github.rest.pulls.get({
      owner: context.repo.owner,
      repo: context.repo.repo,
      pull_number: prNumber,
    });
    return pullRequest;
  }

  const { data: pr } = await github.rest.pulls.update({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: prNumber,
    ...apiData,
  });

  return pr;
}

/**
 * Resolve PR number from message and configuration
 * Uses the standard resolve helper for consistency with update_issue
 */
const resolvePRNumber = createStandardResolveNumber({
  itemType: "update_pull_request",
  itemNumberField: "pull_request_number",
  supportsPR: false, // update_pull_request only supports PRs, not issues
  supportsIssue: false,
});

/**
 * Build update data from message
 * @param {Object} item - The message item
 * @param {Object} config - Configuration object
 * @returns {{success: true, data: Object} | {success: true, skipped: true, reason: string} | {success: false, error: string}} Update data result
 */
function buildPRUpdateData(item, config) {
  const canUpdateTitle = config.allow_title !== false; // Default true
  const { updateData, hasCommonUpdates } = buildCommonEntityUpdateData(item, config, {
    allowTitle: canUpdateTitle,
    defaultOperation: "replace",
    configDefaultOperation: config.default_operation,
    includeBodyInApiData: true,
  });
  let hasUpdates = hasCommonUpdates;

  // Other fields (always allowed)
  if (item.state !== undefined) {
    updateData.state = item.state;
    hasUpdates = true;
  }
  if (item.base !== undefined) {
    updateData.base = item.base;
    hasUpdates = true;
  }
  if (item.draft !== undefined) {
    updateData.draft = item.draft;
    hasUpdates = true;
  }

  const updateBranch = item.update_branch !== undefined ? item.update_branch === true : config.update_branch === true;
  const updateBranchStacksEnabled = config.update_branch_stacks !== false;
  updateData._update_branch_stacks = updateBranchStacksEnabled;
  if (updateBranch) {
    updateData.update_branch = true;
    hasUpdates = true;
  }

  if (!hasUpdates) {
    return {
      success: true,
      skipped: true,
      reason: "No update fields provided or all fields are disabled",
    };
  }

  return { success: true, data: updateData };
}

/**
 * Format success result for PR update
 * Uses the standard format helper for consistency across update handlers
 */
const formatPRSuccessResult = createStandardFormatResult({
  numberField: "pull_request_number",
  urlField: "pull_request_url",
  urlSource: "html_url",
});

/**
 * Main handler factory for update_pull_request
 * Returns a message handler function that processes individual update_pull_request messages
 * @type {HandlerFactoryFunction}
 */
const main = createUpdateHandlerFactory({
  itemType: "update_pull_request",
  itemTypeName: "pull request",
  supportsPR: false,
  resolveItemNumber: resolvePRNumber,
  buildUpdateData: buildPRUpdateData,
  executeUpdate: executePRUpdate,
  formatSuccessResult: formatPRSuccessResult,
  captureExecutionMetadata: {
    captureBefore: async (githubClient, effectiveContext, prNumber) => fetchPullRequestState(githubClient, effectiveContext.repo, prNumber),
    captureAfter: async (updatedPullRequest, beforeState) => mergePullRequestState(beforeState, updatedPullRequest),
  },
  additionalConfig: {
    allow_title: true,
    allow_body: true,
    update_branch: false,
    update_branch_stacks: true,
  },
  itemFilter: async (githubClient, repoParts, prNumber, config) => {
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    return checkRequiredFilter(githubClient, repoParts, prNumber, requiredLabels, requiredTitlePrefix, "update_pull_request");
  },
});

module.exports = { main, buildPRUpdateData };
