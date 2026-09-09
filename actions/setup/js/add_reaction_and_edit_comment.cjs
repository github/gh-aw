// @ts-check
/// <reference types="@actions/github-script" />

const { getRunStartedMessage } = require("./messages_run_status.cjs");
const { getErrorMessage, isLockedError, isRateLimitError } = require("./error_helpers.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { ERR_API, ERR_NOT_FOUND, ERR_VALIDATION, ERR_PARSE } = require("./error_codes.cjs");
const { buildWorkflowRunUrl, EVENT_TYPE_DESCRIPTIONS } = require("./workflow_metadata_helpers.cjs");
const { createDiscussionComment, isRestEndpoint, resolveTopLevelDiscussionCommentId } = require("./github_api_helpers.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { addReaction, addDiscussionReaction, getDiscussionNodeId, REACTION_MAP } = require("./add_reaction.cjs");

/** Valid GitHub reaction types */
const VALID_REACTIONS = Object.freeze(Object.keys(REACTION_MAP));

/**
 * @typedef {{ route: string, params: Record<string, unknown> }} RestEndpoint
 */

/**
 * Validate a required field extracted from an event payload, calling setFailed if missing.
 * @param {unknown} value - The extracted value
 * @param {string} fieldName - Human-readable field name for the error message
 * @param {string} errorCode - Error code prefix (ERR_NOT_FOUND or ERR_VALIDATION)
 * @returns {boolean} true if valid, false if missing (setFailed already called)
 */
function requireEventField(value, fieldName, errorCode) {
  if (value == null) {
    core.setFailed(`${errorCode}: ${fieldName} not found in event payload`);
    return false;
  }
  return true;
}

/**
 * @param {unknown} endpoint
 * @param {string} endpointName
 * @param {string} eventName
 * @returns {RestEndpoint}
 */
function expectRestEndpoint(endpoint, endpointName, eventName) {
  if (!isRestEndpoint(endpoint)) {
    throw new Error(`${ERR_VALIDATION}: Unexpected ${endpointName} endpoint shape for event: ${eventName}`);
  }

  return endpoint;
}

/**
 * @param {string} endpoint
 * @param {"discussion"|"discussion_comment"} eventName
 * @returns {number}
 */
function parseDiscussionEndpoint(endpoint, eventName) {
  const match = endpoint.match(eventName === "discussion" ? /^discussion:([1-9]\d*)$/ : /^discussion_comment:([1-9]\d*):[1-9]\d*$/);
  const discussionNumber = Number(match?.[1]);
  if (!Number.isSafeInteger(discussionNumber)) {
    throw new Error(`${ERR_VALIDATION}: Invalid discussion endpoint: ${endpoint}`);
  }
  return discussionNumber;
}

/**
 * Resolve the reaction and comment API endpoints for a given event.
 * Returns null (after calling core.setFailed) when the event or payload is invalid.
 * @param {string} eventName - The GitHub event name
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {Record<string, any>} payload - The event payload
 * @returns {Promise<{reactionEndpoint: { route: string, params: Record<string, unknown> } | string, commentUpdateEndpoint: { route: string, params: Record<string, unknown> } | string} | null>}
 */
async function resolveEventEndpoints(eventName, owner, repo, payload) {
  switch (eventName) {
    case "issues": {
      const issueNumber = payload?.issue?.number;
      if (!requireEventField(issueNumber, "Issue number", ERR_NOT_FOUND)) return null;
      return {
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner, repo, issue_number: issueNumber } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: issueNumber } },
      };
    }

    case "issue_comment": {
      const commentId = payload?.comment?.id;
      const issueNumber = payload?.issue?.number;
      if (!requireEventField(commentId, "Comment ID", ERR_VALIDATION)) return null;
      if (!requireEventField(issueNumber, "Issue number", ERR_NOT_FOUND)) return null;
      return {
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", params: { owner, repo, comment_id: commentId } },
        // Create new comment on the issue itself, not on the comment
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: issueNumber } },
      };
    }

    case "pull_request": {
      const prNumber = payload?.pull_request?.number;
      if (!requireEventField(prNumber, "Pull request number", ERR_NOT_FOUND)) return null;
      // PRs are "issues" for the reactions endpoint
      return {
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner, repo, issue_number: prNumber } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: prNumber } },
      };
    }

    case "pull_request_review_comment": {
      const reviewCommentId = payload?.comment?.id;
      const prNumber = payload?.pull_request?.number;
      if (!requireEventField(reviewCommentId, "Review comment ID", ERR_VALIDATION)) return null;
      if (!requireEventField(prNumber, "Pull request number", ERR_NOT_FOUND)) return null;
      return {
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", params: { owner, repo, comment_id: reviewCommentId } },
        // Create new comment on the PR itself (using issues endpoint since PRs are issues)
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: prNumber } },
      };
    }

    case "discussion": {
      const discussionNumber = payload?.discussion?.number;
      if (!requireEventField(discussionNumber, "Discussion number", ERR_NOT_FOUND)) return null;
      // Discussions use GraphQL API - get the node ID
      const discussionNodeId = await getDiscussionNodeId(owner, repo, discussionNumber);
      return {
        reactionEndpoint: discussionNodeId, // Store node ID for GraphQL
        commentUpdateEndpoint: `discussion:${discussionNumber}`, // Special format to indicate discussion
      };
    }

    case "discussion_comment": {
      const discussionNumber = payload?.discussion?.number;
      const commentId = payload?.comment?.id;
      if (!discussionNumber || !commentId) {
        core.setFailed(`${ERR_NOT_FOUND}: Discussion or comment information not found in event payload`);
        return null;
      }
      const commentNodeId = payload?.comment?.node_id;
      if (!requireEventField(commentNodeId, "Discussion comment node ID", ERR_NOT_FOUND)) return null;
      return {
        reactionEndpoint: commentNodeId, // Store node ID for GraphQL
        commentUpdateEndpoint: `discussion_comment:${discussionNumber}:${commentId}`, // Special format
      };
    }

    default:
      core.setFailed(`${ERR_VALIDATION}: Unsupported event type: ${eventName}`);
      return null;
  }
}

async function main() {
  const reaction = process.env.GH_AW_REACTION || "eyes";
  const commandsJSON = process.env.GH_AW_COMMANDS;
  let command = null; // Only present for command workflows
  if (commandsJSON) {
    try {
      command = JSON.parse(commandsJSON)[0] ?? null;
    } catch (err) {
      throw new Error(`${ERR_PARSE}: ` + "Failed to parse GH_AW_COMMANDS: " + getErrorMessage(err), { cause: err });
    }
  }
  const invocationContext = resolveInvocationContext(context);
  const runUrl = buildWorkflowRunUrl(context, invocationContext.workflowRepo);

  core.info(`Reaction type: ${reaction}`);
  core.info(`Command name: ${command || "none"}`);
  core.info(`Run ID: ${context.runId}`);
  core.info(`Run URL: ${runUrl}`);

  if (!VALID_REACTIONS.includes(reaction)) {
    core.setFailed(`${ERR_VALIDATION}: Invalid reaction type: ${reaction}. Valid reactions are: ${VALID_REACTIONS.join(", ")}`);
    return;
  }

  const eventName = invocationContext.eventName;
  const { owner, repo } = invocationContext.eventRepo;
  const payload = invocationContext.eventPayload;

  try {
    const endpoints = await resolveEventEndpoints(eventName, owner, repo, payload);
    if (!endpoints) return;

    const { reactionEndpoint, commentUpdateEndpoint } = endpoints;

    core.info(`Reaction API endpoint: ${typeof reactionEndpoint === "object" ? reactionEndpoint.route : reactionEndpoint}`);

    // For discussions, reactionEndpoint is a node ID (GraphQL), otherwise it's a typed REST route
    if (eventName === "discussion" || eventName === "discussion_comment") {
      if (typeof reactionEndpoint !== "string") {
        throw new Error(`${ERR_VALIDATION}: Unexpected reaction endpoint shape for event: ${eventName}`);
      }
      await addDiscussionReaction(reactionEndpoint, reaction);
    } else {
      const { route, params } = expectRestEndpoint(reactionEndpoint, "reaction", eventName);
      await addReaction(route, params, reaction);
    }

    core.info(`Comment endpoint: ${typeof commentUpdateEndpoint === "object" ? commentUpdateEndpoint.route : commentUpdateEndpoint}`);
    await addCommentWithWorkflowLink(commentUpdateEndpoint, runUrl, eventName, invocationContext);
  } catch (error) {
    if (isLockedError(error)) {
      core.info(`Cannot add reaction: resource is locked (this is expected and not an error)`);
      return;
    }
    const errorMessage = getErrorMessage(error);
    if (isRateLimitError(error)) {
      core.warning(`Cannot add reaction due to GitHub API rate limiting: ${errorMessage}`);
      return;
    }
    core.setFailed(`${ERR_API}: Failed to process reaction and comment creation: ${errorMessage}`);
  }
}

/**
 * Helper function to set comment outputs
 * @param {string} commentId - The comment ID
 * @param {string} commentUrl - The comment URL
 * @param {{ owner: string, repo: string }} [eventRepo=context.repo] - Repository where the comment was created
 */
function setCommentOutputs(commentId, commentUrl, eventRepo = context.repo) {
  core.info(`Successfully created comment with workflow link`);
  core.info(`Comment ID: ${commentId}`);
  core.info(`Comment URL: ${commentUrl}`);
  core.info(`Comment Repo: ${eventRepo.owner}/${eventRepo.repo}`);
  core.setOutput("comment-id", commentId);
  core.setOutput("comment-url", commentUrl);
  core.setOutput("comment-repo", `${eventRepo.owner}/${eventRepo.repo}`);
}

/**
 * Add a comment with a workflow run link
 * @param {{ route: string, params: Record<string, unknown> } | string} endpoint - Typed route info for REST events, or special format string for discussions
 * @param {string} runUrl - The URL of the workflow run
 * @param {string} eventName - The event type (to determine the comment text)
 * @param {{
 *   source?: "native" | "workflow_dispatch" | "repository_dispatch",
 *   eventName?: string,
 *   eventPayload?: any,
 *   workflowRepo?: { owner: string, repo: string },
 *   eventRepo?: { owner: string, repo: string }
 * } | null} [invocationContext=null] - Resolved invocation event context. When omitted, falls back to global context payload/repo.
 */
async function addCommentWithWorkflowLink(endpoint, runUrl, eventName, invocationContext = null) {
  const eventPayload = invocationContext?.eventPayload || context.payload;
  const eventRepo = invocationContext?.eventRepo || context.repo;
  try {
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "Workflow";
    const eventTypeDescription = EVENT_TYPE_DESCRIPTIONS[eventName] ?? "event";

    // Use getRunStartedMessage for the workflow link text (supports custom messages)
    const workflowLinkText = getRunStartedMessage({
      workflowName,
      runUrl,
      eventType: eventTypeDescription,
      emoji: process.env.GH_AW_WORKFLOW_EMOJI,
    });

    const lockForAgent = process.env.GH_AW_LOCK_FOR_AGENT === "true";
    const workflowId = process.env.GITHUB_WORKFLOW || "";
    const trackerId = process.env.GH_AW_TRACKER_ID || "";

    // Build comment body from parts, sanitizing first to preserve workflow markers
    const commentParts = [
      sanitizeContent(workflowLinkText),
      ...(lockForAgent && (eventName === "issues" || eventName === "issue_comment") ? ["🔒 This issue has been locked while the workflow is running to prevent concurrent modifications."] : []),
      ...(workflowId ? [generateWorkflowIdMarker(workflowId)] : []),
      ...(trackerId ? [`<!-- gh-aw-tracker-id: ${trackerId} -->`] : []),
      "<!-- gh-aw-comment-type: reaction -->",
    ];
    const commentBody = commentParts.join("\n\n");

    if (eventName === "discussion" || eventName === "discussion_comment") {
      if (typeof endpoint !== "string") {
        throw new Error(`${ERR_VALIDATION}: Unexpected comment endpoint shape for event: ${eventName}`);
      }
      const discussionNumber = parseDiscussionEndpoint(endpoint, eventName);
      const discussionId = await getDiscussionNodeId(eventRepo.owner, eventRepo.repo, discussionNumber);
      // For discussion_comment events, thread the reply under the triggering comment.
      // GitHub Discussions only supports two nesting levels, so resolve the top-level parent node ID.
      const replyToId = eventName === "discussion_comment" ? await resolveTopLevelDiscussionCommentId(github, eventPayload?.comment?.node_id) : null;
      const comment = await createDiscussionComment(github, discussionId, commentBody, replyToId);
      setCommentOutputs(comment.id, comment.url, eventRepo);
      return;
    }

    // Create a new comment for non-discussion events using typed route
    const { route, params } = expectRestEndpoint(endpoint, "comment", eventName);
    const createResponse = await github.request(route, {
      ...params,
      body: commentBody,
      headers: {
        Accept: "application/vnd.github+json",
      },
    });

    setCommentOutputs(createResponse.data.id.toString(), createResponse.data.html_url, eventRepo);
  } catch (error) {
    // Don't fail the entire job if comment creation fails - just log it
    const errorMessage = getErrorMessage(error);
    core.warning(`Failed to create comment with workflow link (This is not critical - the reaction was still added successfully): ${errorMessage}`);
  }
}

module.exports = { main, addCommentWithWorkflowLink, resolveEventEndpoints, VALID_REACTIONS, addReaction, addDiscussionReaction, expectRestEndpoint, parseDiscussionEndpoint, requireEventField };
