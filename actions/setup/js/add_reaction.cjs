// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage, isLockedError, isRateLimitError } = require("./error_helpers.cjs");
const { ERR_API, ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

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
 * Add a reaction to the triggering item (issue, PR, comment, or discussion).
 * This provides immediate feedback to the user when a workflow is triggered.
 * This script only adds reactions - it does NOT create comments.
 * Use add_reaction_and_edit_comment.cjs in the activation job to create the comment with workflow link.
 */
async function main() {
  // Read inputs from environment variables
  const reaction = process.env.GH_AW_REACTION || "eyes";

  core.info(`Adding reaction: ${reaction}`);

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
      await handleGraphQLOrUnknownEvent(eventName, owner, repo, payload, reaction);
    }
    return;
  }

  core.info(`Adding reaction to: ${reactionEndpoint.route}`);
  try {
    await addReaction(reactionEndpoint.route, reactionEndpoint.params, reaction);
  } catch (error) {
    handleReactionError(error);
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
 */
async function handleGraphQLOrUnknownEvent(eventName, owner, repo, payload, reaction) {
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

    await addDiscussionReaction(subjectId, reaction);
  } catch (error) {
    handleReactionError(error);
  }
}

/**
 * Handle errors from reaction API calls consistently
 * @param {unknown} error - The error to handle
 */
function handleReactionError(error) {
  if (isLockedError(error)) {
    // Silently ignore locked resource errors - just log for debugging
    core.info(`Cannot add reaction: resource is locked (this is expected and not an error)`);
    return;
  }
  const errorMessage = getErrorMessage(error);
  if (isRateLimitError(error)) {
    core.warning(`Cannot add reaction due to GitHub API rate limiting: ${errorMessage}`);
    return;
  }
  core.setFailed(`${ERR_API}: Failed to add reaction: ${errorMessage}`);
}

/**
 * Add a reaction to a GitHub issue, PR, or comment using REST API
 * @param {string} route - The typed GitHub API route string (e.g. "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions")
 * @param {Record<string, unknown>} params - The route parameters (owner, repo, issue_number, etc.)
 * @param {string} reaction - The reaction type to add
 */
async function addReaction(route, params, reaction) {
  const response = await github.request(route, {
    ...params,
    content: reaction,
    headers: {
      Accept: "application/vnd.github+json",
    },
  });

  const reactionId = response.data?.id;
  core.info(`Successfully added reaction: ${reaction}${reactionId ? ` (id: ${reactionId})` : ""}`);
  core.setOutput("reaction-id", reactionId?.toString() ?? "");
}

/**
 * Add a reaction to a GitHub discussion or discussion comment using GraphQL
 * @param {string} subjectId - The node ID of the discussion or comment
 * @param {string} reaction - The reaction type to add (mapped to GitHub's ReactionContent enum)
 */
async function addDiscussionReaction(subjectId, reaction) {
  const reactionContent = REACTION_MAP[reaction];
  if (!reactionContent) {
    throw new Error(`${ERR_VALIDATION}: Invalid reaction type for GraphQL: ${reaction}`);
  }

  const result = await github.graphql(
    `
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
