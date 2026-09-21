// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { ERR_VALIDATION, ERR_API } = require("./error_codes.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

/**
 * Type constant for handler identification
 */
const HANDLER_TYPE = "hide_comment";

/**
 * Hide a comment using the GraphQL API.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} nodeId - Comment node ID (e.g., 'IC_kwDOABCD123456')
 * @param {string} reason - Reason for hiding (default: spam)
 * @returns {Promise<{id: string, isMinimized: boolean}>} Hidden comment details
 */
async function hideCommentAPI(github, nodeId, reason = "spam") {
  const query = /* GraphQL */ `
    mutation ($nodeId: ID!, $classifier: ReportedContentClassifiers!) {
      minimizeComment(input: { subjectId: $nodeId, classifier: $classifier }) {
        minimizedComment {
          isMinimized
        }
      }
    }
  `;

  const result = await github.graphql(query, { nodeId, classifier: reason });

  return {
    id: nodeId,
    isMinimized: result.minimizeComment.minimizedComment.isMinimized,
  };
}

/**
 * Resolve a safe-output comment_id into a GraphQL node ID.
 * Supports both GraphQL node IDs and numeric REST comment IDs.
 * @param {any} github - GitHub client
 * @param {{owner?: string, repo?: string}|null|undefined} repoContext - Repository context
 * @param {string|number} commentId - GraphQL node ID or numeric REST comment ID
 * @returns {Promise<{nodeId: string, itemNumber: number, repo: string, kind: "issue"|"discussion"}>} Resolved comment target
 */
async function resolveCommentNodeId(github, repoContext, commentId) {
  if (typeof commentId === "string") {
    const trimmed = commentId.trim();
    if (!trimmed) {
      throw new Error(`${ERR_VALIDATION}: comment_id is required`);
    }

    if (!/^\d+$/.test(trimmed)) {
      const query = /* GraphQL */ `
        query ($nodeId: ID!) {
          node(id: $nodeId) {
            __typename
            ... on IssueComment {
              issue {
                number
                repository {
                  nameWithOwner
                }
              }
            }
            ... on PullRequestReviewComment {
              pullRequest {
                number
                repository {
                  nameWithOwner
                }
              }
            }
            ... on DiscussionComment {
              discussion {
                number
                repository {
                  nameWithOwner
                }
              }
            }
          }
        }
      `;
      const result = await github.graphql(query, { nodeId: trimmed });
      const parent = result?.node?.issue || result?.node?.pullRequest || result?.node?.discussion;
      const itemNumber = parent?.number;
      const repo = parent?.repository?.nameWithOwner;
      if (!Number.isInteger(itemNumber) || itemNumber <= 0 || !repo) {
        throw new Error(`${ERR_VALIDATION}: comment_id must reference a comment on an issue, pull request, or discussion`);
      }
      const kind = result?.node?.discussion ? "discussion" : "issue";
      return { nodeId: trimmed, itemNumber, repo, kind };
    }

    commentId = Number.parseInt(trimmed, 10);
  }

  if (!Number.isInteger(commentId) || commentId <= 0) {
    throw new Error(`${ERR_VALIDATION}: comment_id must be a GraphQL node ID string or a positive numeric REST comment ID`);
  }

  if (!repoContext || !repoContext.owner || !repoContext.repo) {
    throw new Error(`${ERR_VALIDATION}: Unable to resolve numeric comment_id: repository context (owner/repo) is not available`);
  }

  const comment = await github.rest.issues.getComment({
    owner: repoContext.owner,
    repo: repoContext.repo,
    comment_id: commentId,
  });

  const nodeId = comment?.data?.node_id;
  const issueURL = comment?.data?.issue_url || "";
  const match = String(issueURL).match(/\/repos\/([^/]+)\/([^/]+)\/issues\/(\d+)(?:[#/?]|$)/);
  if (!nodeId || typeof nodeId !== "string") {
    throw new Error(`${ERR_API}: Failed to resolve GraphQL node ID for comment_id ${commentId}: comment not found or node_id unavailable`);
  }
  if (!match) {
    throw new Error(`${ERR_API}: Failed to resolve parent item for comment_id ${commentId}`);
  }

  return { nodeId, itemNumber: Number(match[3]), repo: `${match[1]}/${match[2]}`, kind: "issue" };
}

/**
 * Main handler factory for hide_comment
 * Returns a message handler function that processes individual hide_comment messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedReasons = config.allowed_reasons || [];
  const maxCount = config.max || 5;
  const targetConfig = config.target || "triggering";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  core.info(`Hide comment configuration: max=${maxCount}`);
  if (allowedReasons.length > 0) {
    core.info(`Allowed reasons: ${allowedReasons.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single hide_comment message
   * @param {Object} message - The hide_comment message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleHideComment(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping hide_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    try {
      const commentId = message.comment_id;
      if (commentId === undefined || commentId === null || (typeof commentId === "string" && !commentId.trim())) {
        core.warning("comment_id is required");
        return {
          success: false,
          error: "comment_id is required",
        };
      }
      const isNumericCommentId = typeof commentId === "number" || (typeof commentId === "string" && /^\d+$/.test(commentId.trim()));
      if (isNumericCommentId && (!context?.repo?.owner || !context?.repo?.repo)) {
        return {
          success: false,
          error: "Unable to resolve numeric comment_id: repository context (owner/repo) is not available",
        };
      }

      // Normalize reason to uppercase for GitHub API
      const normalizedReason = (message.reason || "SPAM").toUpperCase();

      // Validate reason against allowed reasons if specified (case-insensitive)
      if (allowedReasons.length > 0) {
        const normalizedAllowedReasons = allowedReasons.map(r => r.toUpperCase());
        if (!normalizedAllowedReasons.includes(normalizedReason)) {
          core.warning(`Reason "${message.reason}" is not in allowed-reasons list [${allowedReasons.join(", ")}]. Skipping comment ${commentId}.`);
          return {
            success: false,
            error: `Reason "${message.reason}" is not in allowed-reasons list`,
          };
        }
      }

      const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "comment");
      if (!repoResult.success) {
        return { success: false, error: repoResult.error };
      }
      const selectedRepo = repoResult.repo;
      const resolvedComment = await resolveCommentNodeId(githubClient, repoResult.repoParts, commentId);
      if (resolvedComment.repo.toLowerCase() !== selectedRepo.toLowerCase()) {
        return {
          success: false,
          error: `Comment belongs to repository ${resolvedComment.repo}, but target repository is ${selectedRepo}`,
        };
      }

      let expectedNumber;
      let expectedKind;
      if (targetConfig === "triggering") {
        const invocationContext = resolveInvocationContext(context);
        if (invocationContext.eventPayload?.discussion?.number) {
          expectedNumber = invocationContext.eventPayload.discussion.number;
          expectedKind = "discussion";
        } else {
          expectedNumber = invocationContext.eventPayload?.issue?.number ?? invocationContext.eventPayload?.pull_request?.number;
          expectedKind = "issue";
        }
        if (!expectedNumber) {
          return {
            success: false,
            error: 'Target is "triggering" but not running in issue, pull request, or discussion context',
          };
        }
      } else if (targetConfig !== "*") {
        expectedNumber = Number(targetConfig);
        if (!Number.isInteger(expectedNumber) || expectedNumber <= 0) {
          return {
            success: false,
            error: `Invalid target configuration: ${targetConfig}`,
          };
        }
      }
      if (expectedNumber && resolvedComment.itemNumber !== expectedNumber) {
        return {
          success: false,
          error: `Comment belongs to item #${resolvedComment.itemNumber}, but target is #${expectedNumber}`,
        };
      }
      if (expectedKind && resolvedComment.kind !== expectedKind) {
        return {
          success: false,
          error: `Comment belongs to a ${resolvedComment.kind === "discussion" ? "discussion" : "issue/pull request"}, but the triggering item is a ${expectedKind === "discussion" ? "discussion" : "issue/pull request"} (#${expectedNumber})`,
        };
      }

      core.info(`Hiding comment: ${commentId} (reason: ${normalizedReason})`);

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would hide comment ${commentId}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            commentId,
            itemNumber: resolvedComment.itemNumber,
            repo: resolvedComment.repo,
            reason: normalizedReason,
          },
        };
      }

      const hideResult = await hideCommentAPI(githubClient, resolvedComment.nodeId, normalizedReason);

      if (hideResult.isMinimized) {
        core.info(`Successfully hidden comment: ${resolvedComment.nodeId}`);
        return {
          success: true,
          comment_id: resolvedComment.nodeId,
          is_hidden: true,
        };
      } else {
        core.error(`Failed to hide comment: ${commentId}`);
        return {
          success: false,
          error: `Failed to hide comment: ${commentId}`,
        };
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to hide comment: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
