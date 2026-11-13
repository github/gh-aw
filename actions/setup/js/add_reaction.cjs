// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage, isLockedError, isRateLimitError, isConnectTimeoutError } = require("./error_helpers.cjs");
const { ERR_API, ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { withRetry } = require("./error_recovery.cjs");

const MAX_CONNECTION_RETRIES = 2;
const CONNECTION_RETRY_DELAY_MS = 1000;

/** @type {Record<string, string>} Maps REST reaction names to GraphQL ReactionContent enum values */
const REACTION_MAP = {
  "+1": "THUMBS_UP",
  "-1": "THUMBS_DOWN",
  laugh: "LAUGH",
  confused: "CONFUSED",
  heart: "HEART",
  hooray: "HOORAY",
  rocket: "ROCKET",
  eyes: "EYES",
};

/**
 * Add or remove a reaction to the triggering item (issue, PR, comment, or discussion).
 * This provides immediate feedback to the user when a workflow is triggered.
 * GH_AW_REMOVE_REACTION_ID opts into cleanup; an explicitly empty ID is a no-op.
 * This script only manages reactions - it does NOT create comments.
 * Use add_reaction_and_edit_comment.cjs in the activation job to create the comment with workflow link.
 */
async function main() {
  // Read inputs from environment variables
  const reaction = process.env.GH_AW_REACTION || "eyes";
  const removeReactionId = process.env.GH_AW_REMOVE_REACTION_ID;
  if (removeReactionId !== undefined && !removeReactionId.trim()) {
    core.info("Skipping reaction cleanup: no reaction ID provided");
    return;
  }
  const removing = Boolean(removeReactionId);

  core.info(`${removing ? "Removing" : "Adding"} reaction: ${reaction}`);

  // Validate reaction type
  if (!Object.hasOwn(REACTION_MAP, reaction)) {
    core.setFailed(`${ERR_VALIDATION}: Invalid reaction type: ${reaction}. Valid reactions are: ${Object.keys(REACTION_MAP).join(", ")}`);
    return;
  }

  // Determine the API endpoint based on the event type
  const invocationContext = resolveInvocationContext(context);
  const eventName = invocationContext.eventName;
  const { owner, repo } = invocationContext.eventRepo;
  const payload = invocationContext.eventPayload;

  /** @type {{ route: string, params: Record<string, unknown> } | null} */
  const reactionEndpoint = resolveRestEndpoint(eventName, owner, repo, payload);

  if (reactionEndpoint === null) {
    // GraphQL paths are handled separately; REST validation failures already called setFailed.
    if (!isRestReactionEvent(eventName)) {
      await handleGraphQLOrUnknownEvent(eventName, owner, repo, payload, reaction, removing);
    }
    return;
  }

  core.info(`${removing ? "Removing" : "Adding"} reaction to: ${reactionEndpoint.route}`);
  try {
    await addReaction(reactionEndpoint.route, reactionEndpoint.params, reaction, removeReactionId);
  } catch (error) {
    handleReactionError(error, removing);
  }
}

/**
 * Extract a required field from the event payload, calling setFailed if missing.
 * @param {unknown} value - The extracted value (already resolved by caller)
 * @param {string} fieldName - Human-readable field name for error messages
 * @param {string} errorCode - Error code prefix (ERR_NOT_FOUND or ERR_VALIDATION)
 * @returns {boolean} true if valid, false if missing (setFailed already called)
 */
function requirePayloadField(value, fieldName, errorCode) {
  if (value == null) {
    core.setFailed(`${errorCode}: ${fieldName} not found in event payload`);
    return false;
  }
  return true;
}

/**
 * Resolve the REST API route and params for non-discussion events.
 * Returns null for discussion/discussion_comment/pull_request_review/unsupported events (handled separately).
 * @param {string} eventName
 * @param {string} owner
 * @param {string} repo
 * @param {Record<string, any>} payload
 * @returns {{ route: string, params: Record<string, unknown> } | null}
 */
function resolveRestEndpoint(eventName, owner, repo, payload) {
  switch (eventName) {
    case "issues": {
      const issueNumber = payload?.issue?.number;
      if (!requirePayloadField(issueNumber, "Issue number", ERR_NOT_FOUND)) return null;
      return { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner, repo, issue_number: issueNumber } };
    }

    case "issue_comment": {
      const commentId = payload?.comment?.id;
      if (!requirePayloadField(commentId, "Comment ID", ERR_VALIDATION)) return null;
      return { route: "POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", params: { owner, repo, comment_id: commentId } };
    }

    case "pull_request": {
      const prNumber = payload?.pull_request?.number;
      if (!requirePayloadField(prNumber, "Pull request number", ERR_NOT_FOUND)) return null;
      // PRs are "issues" for the reactions endpoint
      return { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner, repo, issue_number: prNumber } };
    }

    case "pull_request_review_comment": {
      const reviewCommentId = payload?.comment?.id;
      if (!requirePayloadField(reviewCommentId, "Review comment ID", ERR_VALIDATION)) return null;
      return { route: "POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", params: { owner, repo, comment_id: reviewCommentId } };
    }

    case "pull_request_review":
      // Reactions are not supported on review objects; skip silently.
      return null;

    default:
      return null;
  }
}

/**
 * @param {string} eventName
 * @returns {boolean}
 */
function isRestReactionEvent(eventName) {
  return ["issues", "issue_comment", "pull_request", "pull_request_review", "pull_request_review_comment"].includes(eventName);
}

/**
 * Handle GraphQL-based reactions (discussion, discussion_comment) and unsupported event types.
 * @param {string} eventName
 * @param {string} owner
 * @param {string} repo
 * @param {Record<string, any>} payload
 * @param {string} reaction
 * @param {boolean} [removing]
 */
async function handleGraphQLOrUnknownEvent(eventName, owner, repo, payload, reaction, removing = false) {
  let subjectId;
  try {
    switch (eventName) {
      case "discussion": {
        const discussionNumber = payload?.discussion?.number;
        if (!requirePayloadField(discussionNumber, "Discussion number", ERR_NOT_FOUND)) return;
        subjectId = await getDiscussionNodeId(owner, repo, discussionNumber);
        break;
      }

      case "discussion_comment": {
        const commentNodeId = payload?.comment?.node_id;
        if (!requirePayloadField(commentNodeId, "Discussion comment node ID", ERR_NOT_FOUND)) return;
        subjectId = commentNodeId;
        break;
      }

      default:
        core.setFailed(`${ERR_VALIDATION}: Unsupported event type: ${eventName}`);
        return;
    }

    if (subjectId == null) {
      core.setFailed(`${ERR_VALIDATION}: subjectId could not be resolved for event: ${eventName}`);
      return;
    }

    await addDiscussionReaction(subjectId, reaction, removing);
  } catch (error) {
    handleReactionError(error, removing);
  }
}

/**
 * Handle errors from reaction API calls consistently
 * @param {unknown} error - The error to handle
 * @param {boolean} [removing]
 */
function handleReactionError(error, removing = false) {
  const operation = removing ? "remove" : "add";
  const status = error && typeof error === "object" && "status" in error ? error.status : undefined;
  if (removing && status === 404) {
    core.info("Cannot remove reaction: reaction or resource is already removed (this is expected and not an error)");
    return;
  }
  if (isLockedError(error)) {
    // Silently ignore locked resource errors - just log for debugging
    core.info(`Cannot ${operation} reaction: resource is locked (this is expected and not an error)`);
    return;
  }
  const errorMessage = getErrorMessage(error);
  if (isRateLimitError(error) || (removing && status === 429)) {
    core.warning(`Cannot ${operation} reaction due to GitHub API rate limiting: ${errorMessage}`);
    return;
  }
  core.setFailed(`${ERR_API}: Failed to ${operation} reaction: ${errorMessage}`);
}

/**
 * Add or remove a reaction to a GitHub issue, PR, or comment using REST API
 * @param {string} route - The typed GitHub API route string (e.g. "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions")
 * @param {Record<string, unknown>} params - The route parameters (owner, repo, issue_number, etc.)
 * @param {string} reaction - The reaction type to add
 * @param {string} [removeReactionId] - Exact reaction ID to remove instead of adding
 */
async function addReaction(route, params, reaction, removeReactionId = "") {
  const removing = Boolean(removeReactionId);
  const requestRoute = removing ? `${route.replace(/^POST /, "DELETE ")}/{reaction_id}` : route;
  let response;
  try {
    response = await withRetry(
      () =>
        github.request(requestRoute, {
          ...params,
          ...(removing ? { reaction_id: removeReactionId } : { content: reaction }),
          headers: {
            Accept: "application/vnd.github+json",
          },
        }),
      {
        maxRetries: MAX_CONNECTION_RETRIES,
        initialDelayMs: CONNECTION_RETRY_DELAY_MS,
        maxDelayMs: CONNECTION_RETRY_DELAY_MS,
        backoffMultiplier: 1,
        jitterMs: 0,
        shouldRetry: isConnectTimeoutError,
      },
      removing ? "remove reaction" : "add reaction"
    );
  } catch (error) {
    // Preserve the API error's status for callers handling locked resources.
    if (error && typeof error === "object" && "originalError" in error) {
      throw error.originalError;
    }
    throw error;
  }

  if (removing) {
    core.info(`Successfully removed reaction: ${reaction} (id: ${removeReactionId})`);
    return;
  }

  const reactionId = response.data?.id;
  core.info(`Successfully added reaction: ${reaction}${reactionId ? ` (id: ${reactionId})` : ""}`);
  core.setOutput("reaction-id", reactionId?.toString() ?? "");
}

/**
 * Add or remove the authenticated user's reaction to a discussion or comment using GraphQL
 * @param {string} subjectId - The node ID of the discussion or comment
 * @param {string} reaction - The reaction type to add (mapped to GitHub's ReactionContent enum)
 * @param {boolean} [removing]
 */
async function addDiscussionReaction(subjectId, reaction, removing = false) {
  const reactionContent = REACTION_MAP[reaction];
  if (!reactionContent) {
    throw new Error(`${ERR_VALIDATION}: Invalid reaction type for GraphQL: ${reaction}`);
  }

  const result = await github.graphql(
    removing
      ? `
    mutation($subjectId: ID!, $content: ReactionContent!) {
      removeReaction(input: { subjectId: $subjectId, content: $content }) {
        clientMutationId
      }
    }`
      : `
    mutation($subjectId: ID!, $content: ReactionContent!) {
      addReaction(input: { subjectId: $subjectId, content: $content }) {
        reaction {
          id
          content
        }
      }
    }`,
    { subjectId, content: reactionContent }
  );

  if (removing) {
    core.info(`Successfully removed reaction: ${reaction}`);
    return;
  }

  const reactionId = result.addReaction.reaction.id;
  core.info(`Successfully added reaction: ${reaction} (id: ${reactionId})`);
  core.setOutput("reaction-id", reactionId);
}

/**
 * Get the node ID for a discussion
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @returns {Promise<string>} Discussion node ID
 */
async function getDiscussionNodeId(owner, repo, discussionNumber) {
  const { repository } = await github.graphql(
    `
    query($owner: String!, $repo: String!, $num: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $num) { 
          id 
        }
      }
    }`,
    { owner, repo, num: discussionNumber }
  );

  if (!repository || !repository.discussion) {
    throw new Error(`${ERR_NOT_FOUND}: Discussion #${discussionNumber} not found in ${owner}/${repo}`);
  }

  return repository.discussion.id;
}

module.exports = { main, addReaction, addDiscussionReaction, getDiscussionNodeId, handleReactionError, resolveRestEndpoint, handleGraphQLOrUnknownEvent, REACTION_MAP };
