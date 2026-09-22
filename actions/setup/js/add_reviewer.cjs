// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 * @typedef {import('./types/handler-factory').HandlerConfig} HandlerConfig
 * @typedef {import('./types/handler-factory').ResolvedTemporaryIds} ResolvedTemporaryIds
 * @typedef {import('./types/handler-factory').HandlerResult} HandlerResult
 */

/**
 * @typedef {{ reviewers?: Array<string|null|undefined|false>, team_reviewers?: Array<string|null|undefined|false>, pull_request_number?: number|string, repo?: string }} AddReviewerMessage
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_reviewer";

const { processItems } = require("./safe_output_processor.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode, checkRequiredFilter, resolveTarget } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { attachExecutionState, extractReviewStateFromData, fetchPullRequestReviewState } = require("./safe_output_execution_metadata.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { COPILOT_REVIEWER_BOT, COPILOT_REVIEWER_BOT_ID } = require("./constants.cjs");
const { ERR_API } = require("./error_codes.cjs");

/**
 * Requests Copilot as a reviewer on a pull request via GraphQL (bot reviewers cannot use the REST API).
 * Resolves the PR's node ID, then issues the requestReviews mutation with the Copilot bot ID.
 * @param {{owner: string, repo: string}} repoParts - Repository owner/name
 * @param {number} prNumber - Pull request number
 * @param {{ githubClient: any, resolveCopilotBotNodeId: () => Promise<string> }} dependencies - Helper dependencies
 * @returns {Promise<any>} The updated pull request data (REST shape), fetched after the mutation succeeds
 */
async function addCopilotReviewer(repoParts, prNumber, { githubClient, resolveCopilotBotNodeId }) {
  const pullRequestQuery = `
    query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        pullRequest(number: $number) {
          id
        }
      }
    }
  `;
  const pullRequestResponse = await githubClient.graphql(pullRequestQuery, {
    owner: repoParts.owner,
    repo: repoParts.repo,
    number: prNumber,
  });
  const pullRequestId = pullRequestResponse?.repository?.pullRequest?.id;
  if (!pullRequestId) {
    throw new Error(`${ERR_API}: Could not resolve pull request node ID for ${repoParts.owner}/${repoParts.repo}#${prNumber}`);
  }

  const requestReviewsMutation = `
    mutation($pullRequestId: ID!, $botIds: [ID!]!) {
      requestReviews(input: { pullRequestId: $pullRequestId, botIds: $botIds, union: true }) {
        pullRequest {
          id
        }
      }
    }
  `;
  await githubClient.graphql(requestReviewsMutation, {
    pullRequestId,
    botIds: [await resolveCopilotBotNodeId()],
  });

  const response = await githubClient.rest.pulls.get({
    owner: repoParts.owner,
    repo: repoParts.repo,
    pull_number: prNumber,
  });
  return response?.data;
}

/**
 * Main handler factory for add_reviewer
 * Returns a message handler function that processes individual add_reviewer messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const allowedReviewers = config.allowed ?? [];
  const allowedTeamReviewers = config.allowed_team_reviewers ?? [];
  const maxCount = config.max ?? 10;
  const targetConfig = config.target || "triggering";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);
  const isStaged = isStagedMode(config);

  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  core.info(`Add reviewer configuration: max=${maxCount}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (allowedReviewers.length > 0) {
    core.info(`Allowed reviewers: ${allowedReviewers.join(", ")}`);
  }
  if (allowedTeamReviewers.length > 0) {
    core.info(`Allowed team reviewers: ${allowedTeamReviewers.join(", ")}`);
  }

  /** @type {string|null} Copilot reviewer bot node ID, resolved once and cached per handler instance */
  let copilotBotNodeIdCache = null;

  /**
   * Resolves the Copilot reviewer bot's GraphQL node ID for the current GitHub instance.
   * Uses the REST users API so the result is correct on GitHub.com and GHES alike.
   * Caches the resolved ID for the lifetime of this handler to avoid redundant requests.
   * Falls back to the built-in GitHub.com constant when the API call fails.
   * @returns {Promise<string>} GraphQL node ID for the Copilot reviewer bot
   */
  async function resolveCopilotBotNodeId() {
    if (copilotBotNodeIdCache !== null) {
      return copilotBotNodeIdCache;
    }
    try {
      const response = await githubClient.rest.users.getByUsername({ username: COPILOT_REVIEWER_BOT });
      const nodeId = response?.data?.node_id;
      if (nodeId) {
        copilotBotNodeIdCache = nodeId;
        return nodeId;
      }
    } catch (err) {
      core.warning(`Could not resolve Copilot reviewer bot node ID at runtime (${getErrorMessage(err)}); using built-in fallback`);
    }
    copilotBotNodeIdCache = COPILOT_REVIEWER_BOT_ID;
    return copilotBotNodeIdCache;
  }

  let processedCount = 0;

  /**
   * @param {AddReviewerMessage} message - The add_reviewer message to process
   * @param {ResolvedTemporaryIds} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<HandlerResult>} Result with success/error status
   */
  return async function handleAddReviewer(message, resolvedTemporaryIds) {
    if (processedCount >= maxCount) {
      core.warning(`Skipping add_reviewer: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const targetResult = resolveTarget({
      targetConfig,
      item: message,
      context,
      itemType: HANDLER_TYPE,
    });
    if (!targetResult.success) {
      core.warning(targetResult.error);
      return {
        success: false,
        error: targetResult.error,
      };
    }
    const prNumber = targetResult.number;

    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "pull request reviewer");
    if (!repoResult.success) {
      core.warning(`Skipping add_reviewer: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);

    const filterResult = await checkRequiredFilter(githubClient, repoParts, prNumber, requiredLabels, requiredTitlePrefix, HANDLER_TYPE);
    if (filterResult) return filterResult;

    const requestedReviewers = message.reviewers ?? [];
    const requestedTeamReviewers = message.team_reviewers ?? [];
    core.info(`Requested reviewers: ${JSON.stringify(requestedReviewers)}`);
    core.info(`Requested team reviewers: ${JSON.stringify(requestedTeamReviewers)}`);

    // Use shared helper to filter, sanitize, dedupe, and limit across both reviewer types
    const uniqueReviewers = processItems(requestedReviewers, allowedReviewers, maxCount);
    const remainingReviewerSlots = Math.max(0, maxCount - uniqueReviewers.length);
    const uniqueTeamReviewers = processItems(requestedTeamReviewers, allowedTeamReviewers, remainingReviewerSlots);

    if (uniqueReviewers.length === 0 && uniqueTeamReviewers.length === 0) {
      core.info("No reviewers to add");
      return {
        success: true,
        skipped: true,
        prNumber,
        reviewersAdded: [],
        teamReviewersAdded: [],
        message: "No valid reviewers found",
      };
    }

    core.info(`Adding reviewers to PR #${prNumber}: reviewers=${JSON.stringify(uniqueReviewers)}, team_reviewers=${JSON.stringify(uniqueTeamReviewers)}`);

    // If in staged mode, preview without executing
    if (isStaged) {
      logStagedPreviewInfo(`Would add reviewers to PR #${prNumber}`);
      return {
        success: true,
        staged: true,
        previewInfo: {
          number: prNumber,
          reviewers: uniqueReviewers,
          team_reviewers: uniqueTeamReviewers,
        },
      };
    }

    try {
      const beforeState = await fetchPullRequestReviewState(githubClient, repoParts, prNumber);
      /** @type {any} */
      let latestPullRequest = null;

      // Special handling for "copilot" reviewer - separate it from other reviewers
      const hasCopilot = uniqueReviewers.includes("copilot");
      const otherReviewers = uniqueReviewers.filter(r => r !== "copilot");
      const manifestReviewers = hasCopilot ? [...otherReviewers, COPILOT_REVIEWER_BOT] : otherReviewers;
      // Add non-copilot reviewers first
      if (otherReviewers.length > 0 || uniqueTeamReviewers.length > 0) {
        /** @type {{ owner: string, repo: string, pull_number: number, reviewers: string[], team_reviewers?: string[] }} */
        const reviewerRequest = {
          owner: repoParts.owner,
          repo: repoParts.repo,
          pull_number: prNumber,
          reviewers: otherReviewers,
        };
        if (uniqueTeamReviewers.length > 0) {
          reviewerRequest.team_reviewers = uniqueTeamReviewers;
        }
        const response = await githubClient.rest.pulls.requestReviewers(reviewerRequest);
        latestPullRequest = response?.data || latestPullRequest;
        core.info(`Successfully added reviewers to PR #${prNumber}: reviewers=${JSON.stringify(otherReviewers)}, team_reviewers=${JSON.stringify(uniqueTeamReviewers)}`);
      }

      // Add copilot reviewer separately if requested
      if (hasCopilot) {
        try {
          const copilotPullRequestData = await addCopilotReviewer(repoParts, prNumber, {
            githubClient,
            resolveCopilotBotNodeId,
          });
          latestPullRequest = copilotPullRequestData || latestPullRequest;
          core.info(`Successfully added copilot as reviewer to PR #${prNumber}`);
        } catch (copilotError) {
          core.warning(`Failed to add copilot as reviewer: ${getErrorMessage(copilotError)}`);
          // Don't fail the whole step if copilot reviewer fails
        }
      }

      const afterState = latestPullRequest
        ? {
            ...extractReviewStateFromData(latestPullRequest, []),
            reviews: beforeState.reviews,
          }
        : await fetchPullRequestReviewState(githubClient, repoParts, prNumber);

      return attachExecutionState(
        {
          success: true,
          prNumber,
          number: prNumber,
          repo: itemRepo,
          pull_request_number: prNumber,
          pull_request_url: `https://github.com/${repoParts.owner}/${repoParts.repo}/pull/${prNumber}`,
          reviewersAdded: uniqueReviewers,
          teamReviewersAdded: uniqueTeamReviewers,
          metadata: {
            requested_reviewers: manifestReviewers,
            requested_team_reviewers: uniqueTeamReviewers,
          },
        },
        beforeState,
        afterState
      );
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to add reviewers: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
