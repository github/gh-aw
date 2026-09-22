// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { getPRNumber } = require("./update_context_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveTargetRepoConfig, validateTargetRepo } = require("./repo_helpers.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

/**
 * Type constant for handler identification
 */
const HANDLER_TYPE = "resolve_pull_request_review_thread";

/**
 * Look up a review thread's parent PR number and repository via the GraphQL API.
 * Used to validate the thread before resolving.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} threadId - Review thread node ID (e.g., 'PRRT_kwDOABCD...')
 * @returns {Promise<
 *   | {status: "missing"}
 *   | {status: "thread", threadId: string, prNumber: number, repoNameWithOwner: string|null, isResolved: boolean}
 *   | {status: "invalid_type", nodeType: string}
 *   | {status: "comment_without_thread"}
 * >} Thread lookup result
 */
async function getThreadPullRequestInfo(github, threadId) {
  const query = /* GraphQL */ `
    query ($threadId: ID!) {
      node(id: $threadId) {
        __typename
        ... on PullRequestReviewThread {
          id
          isResolved
          pullRequest {
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
              name
              nameWithOwner
              owner {
                login
              }
            }
          }
        }
      }
    }
  `;

  const result = await github.graphql(query, { threadId });

  const threadNode = result?.node;
  if (!threadNode) {
    return { status: "missing" };
  }
  if (threadNode.__typename === "PullRequestReviewComment") {
    return findThreadInfoForReviewComment(github, threadId, threadNode);
  }
  if (threadNode.__typename !== "PullRequestReviewThread") {
    return {
      status: "invalid_type",
      nodeType: threadNode.__typename || "unknown",
    };
  }
  const pullRequest = threadNode.pullRequest;

  return {
    status: "thread",
    threadId: threadNode.id,
    prNumber: pullRequest.number,
    repoNameWithOwner: pullRequest.repository?.nameWithOwner ?? null,
    isResolved: threadNode?.isResolved === true,
  };
}

/**
 * Resolve a PullRequestReviewComment node ID to its containing review thread.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} commentId - Pull request review comment node ID (e.g., 'PRRC_kwDOABCD...')
 * @param {any} commentNode - PullRequestReviewComment node returned by the initial lookup
 * @returns {Promise<
 *   | {status: "thread", threadId: string, prNumber: number, repoNameWithOwner: string|null, isResolved: boolean}
 *   | {status: "comment_without_thread"}
 * >}
 */
async function findThreadInfoForReviewComment(github, commentId, commentNode) {
  const pullRequest = commentNode.pullRequest;
  const repository = pullRequest?.repository;
  const prNumber = pullRequest?.number;
  const repoOwner = repository?.owner?.login;
  const repoName = repository?.name;
  const repoNameWithOwner = repository?.nameWithOwner ?? (repoOwner && repoName ? `${repoOwner}/${repoName}` : null);

  if (!prNumber || !repoOwner || !repoName) {
    return { status: "comment_without_thread" };
  }

  const threadListQuery = /* GraphQL */ `
    query ($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
      repository(owner: $owner, name: $repo) {
        pullRequest(number: $number) {
          reviewThreads(first: 100, after: $cursor) {
            pageInfo {
              hasNextPage
              endCursor
            }
            nodes {
              id
              isResolved
              comments(first: 100) {
                pageInfo {
                  hasNextPage
                  endCursor
                }
                nodes {
                  id
                }
              }
            }
          }
        }
      }
    }
  `;

  const threadCommentsQuery = /* GraphQL */ `
    query ($threadId: ID!, $cursor: String) {
      node(id: $threadId) {
        ... on PullRequestReviewThread {
          comments(first: 100, after: $cursor) {
            pageInfo {
              hasNextPage
              endCursor
            }
            nodes {
              id
            }
          }
        }
      }
    }
  `;

  let threadCursor = null;
  do {
    const result = await github.graphql(threadListQuery, {
      owner: repoOwner,
      repo: repoName,
      number: prNumber,
      cursor: threadCursor,
    });
    const reviewThreads = result?.repository?.pullRequest?.reviewThreads;

    for (const thread of reviewThreads?.nodes || []) {
      // Check first page of comments for this thread
      if ((thread.comments?.nodes || []).some(comment => comment?.id === commentId)) {
        return {
          status: "thread",
          threadId: thread.id,
          prNumber,
          repoNameWithOwner,
          isResolved: thread.isResolved === true,
        };
      }

      // If there are more comment pages, paginate within this thread
      let commentCursor = thread.comments?.pageInfo?.hasNextPage ? thread.comments.pageInfo.endCursor : null;
      while (commentCursor) {
        const commentResult = await github.graphql(threadCommentsQuery, {
          threadId: thread.id,
          cursor: commentCursor,
        });
        const commentsPage = commentResult?.node?.comments;
        if ((commentsPage?.nodes || []).some(comment => comment?.id === commentId)) {
          return {
            status: "thread",
            threadId: thread.id,
            prNumber,
            repoNameWithOwner,
            isResolved: thread.isResolved === true,
          };
        }
        commentCursor = commentsPage?.pageInfo?.hasNextPage ? commentsPage.pageInfo.endCursor : null;
      }
    }

    threadCursor = reviewThreads?.pageInfo?.hasNextPage ? reviewThreads.pageInfo.endCursor : null;
  } while (threadCursor);

  return { status: "comment_without_thread" };
}

/**
 * Resolve a pull request review thread using the GraphQL API.
 * @param {any} github - GitHub GraphQL instance
 * @param {string} threadId - Review thread node ID (e.g., 'PRRT_kwDOABCD...')
 * @returns {Promise<{threadId: string, isResolved: boolean}>} Resolved thread details
 */
async function resolveReviewThreadAPI(github, threadId) {
  const query = /* GraphQL */ `
    mutation ($threadId: ID!) {
      resolveReviewThread(input: { threadId: $threadId }) {
        thread {
          id
          isResolved
        }
      }
    }
  `;

  const result = await github.graphql(query, { threadId });

  return {
    threadId: result.resolveReviewThread.thread.id,
    isResolved: result.resolveReviewThread.thread.isResolved,
  };
}

/**
 * Check whether a GraphQL error indicates integration-token actor restrictions.
 * @param {unknown} error
 * @returns {boolean}
 */
function isIntegrationAccessError(error) {
  const integrationErrorFragment = "resource not accessible by integration";
  /** @type {string[]} */
  const messages = [getErrorMessage(error)];

  if (error && typeof error === "object" && "errors" in error && Array.isArray(error.errors)) {
    for (const graphQLError of error.errors) {
      if (typeof graphQLError?.message === "string") {
        messages.push(graphQLError.message);
      }
    }
  }

  return messages.some(message => message.toLowerCase().includes(integrationErrorFragment));
}

/**
 * Check whether an error indicates the referenced GraphQL node no longer exists.
 * Review thread node IDs can become stale between the agent turn and the safe-outputs
 * replay (thread already resolved, deleted, or superseded), which surfaces as a
 * "Could not resolve to a node" or "Not Found" error. These are treated as skippable.
 * @param {unknown} error
 * @returns {boolean}
 */
function isMissingNodeError(error) {
  /** @type {string[]} */
  const messages = [getErrorMessage(error)];
  /** @type {Array<{type?: unknown, message?: unknown, path?: unknown}>} */
  const graphQLErrors = [];

  if (error && typeof error === "object" && "errors" in error && Array.isArray(error.errors)) {
    for (const graphQLError of error.errors) {
      graphQLErrors.push(graphQLError);
      if (typeof graphQLError?.message === "string") {
        messages.push(graphQLError.message);
      }
    }
  }

  const hasNodeScopedNotFoundType = graphQLErrors.some(graphQLError => {
    if (typeof graphQLError?.type !== "string" || graphQLError.type.toUpperCase() !== "NOT_FOUND") {
      return false;
    }
    const hasNodeScopedMessage = typeof graphQLError?.message === "string" && graphQLError.message.toLowerCase().includes("could not resolve to a node");
    const hasNodeScopedPath =
      Array.isArray(graphQLError?.path) &&
      graphQLError.path.some(pathPart => {
        if (typeof pathPart !== "string") return false;
        const normalized = pathPart.toLowerCase();
        // GraphQL paths for stale-thread mutation failures are rooted at "resolveReviewThread".
        return normalized === "node" || normalized === "resolvereviewthread";
      });
    return hasNodeScopedMessage || hasNodeScopedPath;
  });
  if (hasNodeScopedNotFoundType) {
    return true;
  }

  return messages.some(message => {
    const normalized = message.trim().toLowerCase();
    // Match the stale-node GraphQL error, or Octokit's bare "Not Found" 404 message.
    // Deliberately avoid a loose "not found" substring match so unrelated errors
    // (e.g. "Repository not found") still surface as real failures.
    return normalized.includes("could not resolve to a node") || normalized === "not found";
  });
}

/**
 * Main handler factory for resolve_pull_request_review_thread
 * Returns a message handler function that processes individual resolve messages.
 *
 * By default, resolution is scoped to the triggering PR only. When target-repo or
 * allowed-repos are specified, cross-repository thread resolution is supported.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const maxCount = config.max || 10;
  const resolveTarget = config.target || "triggering";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  // Whether the user explicitly configured cross-repo targeting.
  // defaultTargetRepo always has a value (falls back to context.repo), so we check
  // the raw config keys to distinguish user-configured from default.
  const hasExplicitTargetConfig = !!(config["target-repo"] || config.allowed_repos?.length > 0);

  const githubClient = await createAuthenticatedGitHubClient(config);

  // Determine the triggering PR number from context, resolving forwarded invocations
  // (workflow_dispatch inputs/aw_context, repository_dispatch.client_payload) the same
  // way resolveTarget does.
  const invocationContext = resolveInvocationContext(context);
  const triggeringPRNumber = getPRNumber(invocationContext.eventPayload);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  core.info(`Resolve PR review thread configuration: max=${maxCount}, target=${resolveTarget}, triggeringPR=${triggeringPRNumber || "none"}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single resolve_pull_request_review_thread message
   * @param {Object} message - The resolve message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleResolvePRReviewThread(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping resolve_pull_request_review_thread: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    try {
      // Validate required fields
      const threadId = item.thread_id;
      if (!threadId || typeof threadId !== "string" || threadId.trim().length === 0) {
        core.warning('Missing or invalid required field "thread_id" in resolve message');
        return {
          success: false,
          error: 'Missing or invalid required field "thread_id" - must be a non-empty string (GraphQL node ID)',
        };
      }

      // Look up the thread's PR number and repository
      /** @type {Awaited<ReturnType<typeof getThreadPullRequestInfo>>} */
      let threadInfo;
      try {
        threadInfo = await getThreadPullRequestInfo(githubClient, threadId);
      } catch (error) {
        if (isMissingNodeError(error)) {
          core.info(`Review thread ${threadId} could not be resolved (${getErrorMessage(error)}) — already resolved or stale; skipping`);
          return {
            success: true,
            thread_id: threadId,
            is_resolved: true,
            skipped: true,
          };
        }
        throw error;
      }
      if (threadInfo.status === "missing") {
        core.info(`Review thread ${threadId} not found — already resolved or stale; skipping`);
        return {
          success: true,
          thread_id: threadId,
          is_resolved: true,
          skipped: true,
        };
      }

      if (threadInfo.status === "comment_without_thread") {
        return {
          success: false,
          error: `Could not find a PullRequestReviewThread containing review comment ${threadId}`,
        };
      }

      if (threadInfo.status !== "thread") {
        return {
          success: false,
          error: `thread_id must reference a PullRequestReviewThread node ID (PRRT_...); received ${threadInfo.nodeType} for ${threadId}`,
        };
      }

      const { threadId: resolvedThreadId, prNumber: threadPRNumber, repoNameWithOwner: threadRepo, isResolved } = threadInfo;
      if (resolvedThreadId !== threadId) {
        core.info(`Resolved review comment ${threadId} to review thread ${resolvedThreadId}`);
      }

      if (!threadRepo) {
        core.warning(`Could not determine repository for thread ${resolvedThreadId}`);
        return {
          success: false,
          error: `Could not determine the repository for thread ${resolvedThreadId}`,
        };
      }
      const repoValidation = validateTargetRepo(threadRepo, defaultTargetRepo, allowedRepos);
      if (!repoValidation.valid) {
        core.warning(`Thread ${resolvedThreadId} belongs to repo ${threadRepo}, which is not allowed`);
        return {
          success: false,
          skipped: !hasExplicitTargetConfig,
          thread_id: resolvedThreadId,
          error: repoValidation.error,
        };
      }

      if (resolveTarget === "triggering") {
        if (!triggeringPRNumber) {
          core.warning("Cannot resolve review thread: not running in a pull request context");
          return {
            success: false,
            error: "Cannot resolve review threads outside of a pull request context",
          };
        }
        if (threadPRNumber !== triggeringPRNumber) {
          core.warning(`Thread ${resolvedThreadId} belongs to PR #${threadPRNumber}, not triggering PR #${triggeringPRNumber}`);
          return {
            success: false,
            error: `Thread belongs to PR #${threadPRNumber}, but only threads on the triggering PR #${triggeringPRNumber} can be resolved`,
          };
        }
      } else if (resolveTarget !== "*") {
        const targetPRNumber = parseInt(resolveTarget, 10);
        if (Number.isNaN(targetPRNumber) || targetPRNumber <= 0) {
          core.warning(`Invalid target PR number: '${resolveTarget}'`);
          return {
            success: false,
            error: `Invalid target: '${resolveTarget}' - must be 'triggering', '*', or a positive integer`,
          };
        }
        if (threadPRNumber !== targetPRNumber) {
          core.warning(`Thread ${resolvedThreadId} belongs to PR #${threadPRNumber}, not target PR #${targetPRNumber}`);
          return {
            success: false,
            error: `Thread belongs to PR #${threadPRNumber}, but target is PR #${targetPRNumber}`,
          };
        }
      }

      core.info(`Resolving review thread: ${resolvedThreadId} (PR #${threadPRNumber}${threadRepo ? " in " + threadRepo : ""})`);

      // Apply required-labels/required-title-prefix filter
      const [threadOwner, threadRepoName] = (threadRepo || `${context.repo.owner}/${context.repo.repo}`).split("/");
      const repoParts = { owner: threadOwner, repo: threadRepoName };
      const filterResult = await checkRequiredFilter(githubClient, repoParts, threadPRNumber, requiredLabels, requiredTitlePrefix, "resolve_pull_request_review_thread");
      if (filterResult) return filterResult;

      if (isResolved) {
        core.info(`Review thread ${resolvedThreadId} is already resolved; skipping`);
        return {
          success: true,
          thread_id: resolvedThreadId,
          is_resolved: true,
          skipped: true,
        };
      }

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would resolve review thread ${resolvedThreadId}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            thread_id: resolvedThreadId,
            pr_number: threadPRNumber,
          },
        };
      }

      let resolveResult;
      try {
        resolveResult = await resolveReviewThreadAPI(githubClient, resolvedThreadId);
      } catch (error) {
        if (isMissingNodeError(error)) {
          core.info(`Review thread ${resolvedThreadId} could not be resolved (${getErrorMessage(error)}) — already resolved or stale; skipping`);
          return {
            success: true,
            thread_id: resolvedThreadId,
            is_resolved: true,
            skipped: true,
          };
        }
        if (isIntegrationAccessError(error)) {
          const warningMessage =
            `Skipping resolve_pull_request_review_thread for ${resolvedThreadId}: configuration mismatch ` +
            `(GitHub integration token cannot resolve this review thread: Resource not accessible by integration). ` +
            `Use safe-outputs.resolve-pull-request-review-thread.github-token with a token that can resolve review threads.`;
          core.warning(warningMessage);
          return {
            success: false,
            skipped: true,
            error: warningMessage,
          };
        }
        throw error;
      }

      if (resolveResult.isResolved) {
        core.info(`Successfully resolved review thread: ${resolvedThreadId}`);
        return {
          success: true,
          thread_id: resolvedThreadId,
          is_resolved: true,
        };
      } else {
        core.error(`Failed to resolve review thread: ${resolvedThreadId}`);
        return {
          success: false,
          error: `Failed to resolve review thread: ${resolvedThreadId}`,
        };
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to resolve review thread: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main, HANDLER_TYPE };
