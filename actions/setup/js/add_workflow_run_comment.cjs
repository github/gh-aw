// @ts-check
/// <reference types="@actions/github-script" />

const { getRunStartedMessage } = require("./messages_run_status.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");
const { getMessages } = require("./messages_core.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { buildWorkflowRunUrl, EVENT_TYPE_DESCRIPTIONS } = require("./workflow_metadata_helpers.cjs");
const { isRestEndpoint, resolveTopLevelDiscussionCommentId } = require("./github_api_helpers.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

/**
 * @typedef {{ owner: string, repo: string }} RepoRef
 * @typedef {{ id: string, url: string, repo: RepoRef }} CommentMetadata
 * @typedef {{ id: string, url: string, repo: RepoRef | null }} ReusableStatusComment
 */

/**
 * Helper function to get discussion node ID via GraphQL
 * @param {number} discussionNumber - The discussion number
 * @param {{ owner: string, repo: string }} [eventRepo] - Repository where the discussion event occurred (defaults to context.repo at runtime)
 * @returns {Promise<string>} The discussion node ID
 */
async function getDiscussionNodeId(discussionNumber, eventRepo = context.repo) {
  const { repository } = await github.graphql(
    `
    query($owner: String!, $repo: String!, $num: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $num) { 
          id 
        }
      }
    }`,
    { owner: eventRepo.owner, repo: eventRepo.repo, num: discussionNumber }
  );
  return repository.discussion.id;
}

/**
 * Helper function to set comment outputs and return comment metadata
 * @param {string|number} commentId - The comment ID
 * @param {string} commentUrl - The comment URL
 * @param {RepoRef} [eventRepo] - Repository where the comment was created (defaults to context.repo at runtime)
 * @param {{ logReuse?: boolean }} [options]
 * @returns {CommentMetadata}
 */
function setCommentOutputs(commentId, commentUrl, eventRepo = context.repo, options = {}) {
  if (options.logReuse) {
    core.info(`Reusing existing status comment outputs`);
  } else {
    core.info(`Successfully created comment with workflow link`);
  }

  core.info(`Comment ID: ${commentId}`);
  core.info(`Comment URL: ${commentUrl}`);
  core.info(`Comment Repo: ${eventRepo.owner}/${eventRepo.repo}`);
  core.setOutput("comment-id", commentId.toString());
  core.setOutput("comment-url", commentUrl);
  core.setOutput("comment-repo", `${eventRepo.owner}/${eventRepo.repo}`);
  return {
    id: commentId.toString(),
    url: commentUrl,
    repo: eventRepo,
  };
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
 * @param {unknown} value
 * @returns {Record<string, any>|null}
 */
function parseObject(value) {
  if (!value) return null;
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) return null;
    try {
      const parsed = JSON.parse(trimmed);
      return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : null;
    } catch {
      return null;
    }
  }
  if (typeof value === "object" && !Array.isArray(value)) {
    return /** @type {Record<string, any>} */ value;
  }
  return null;
}

/**
 * @param {unknown} value
 * @returns {RepoRef | null}
 */
function parseRepoSlug(value) {
  if (typeof value !== "string") {
    return null;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  const parts = trimmed.split("/");
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return null;
  }
  return { owner: parts[0], repo: parts[1] };
}

/**
 * Read aw_context from workflow_dispatch or repository_dispatch payloads.
 * Accepts both snake_case and camelCase input names for compatibility.
 * @param {any} payload
 * @returns {Record<string, any>|null}
 */
function extractAwContextFromPayload(payload) {
  return parseObject(payload?.inputs?.aw_context) || parseObject(payload?.inputs?.awContext) || parseObject(payload?.client_payload?.aw_context) || parseObject(payload?.client_payload?.awContext);
}

/**
 * @param {any} rawContext
 * @returns {ReusableStatusComment | null}
 */
function readReusableStatusComment(rawContext) {
  const awContext = extractAwContextFromPayload(rawContext?.payload);
  if (!awContext) {
    return null;
  }

  const rawId = awContext.status_comment_id ?? awContext.statusCommentId;
  const id = rawId == null ? "" : String(rawId).trim();
  if (!id) {
    return null;
  }

  const rawUrl = awContext.status_comment_url ?? awContext.statusCommentUrl;
  const url = typeof rawUrl === "string" ? rawUrl.trim() : "";
  const repo = parseRepoSlug(awContext.status_comment_repo ?? awContext.statusCommentRepo);
  return { id, url, repo };
}

/**
 * @param {any} rawContext
 * @param {string} message
 */
function reportCommentError(rawContext, message) {
  if (rawContext?.nonFatalStatusCommentErrors) {
    core.warning(message);
    return;
  }
  core.setFailed(message);
}

/**
 * @param {Record<string, any>|null} awContext
 * @param {string} key
 * @returns {string}
 */
function readAwContextString(awContext, key) {
  if (!awContext || typeof awContext[key] !== "string") {
    return "";
  }
  return awContext[key].trim();
}

/**
 * @param {ReusableStatusComment} reusableComment
 * @param {{
 *   source: "native" | "workflow_dispatch" | "repository_dispatch";
 *   eventName: string;
 *   eventPayload: any;
 *   workflowRepo: { owner: string, repo: string };
 *   eventRepo: { owner: string, repo: string };
 * }} invocationContext
 * @param {any} rawContext
 * @returns {Promise<string>}
 */
async function updateReusableStatusComment(reusableComment, invocationContext, rawContext) {
  const awContext = extractAwContextFromPayload(rawContext?.payload);
  const dispatchedRunUrl = readAwContextString(awContext, "dispatched_run_url");
  const dispatchedWorkflowName = readAwContextString(awContext, "dispatched_workflow_name");
  const runUrl = dispatchedRunUrl || buildWorkflowRunUrl(rawContext, invocationContext.workflowRepo);
  const commentBody = buildCommentBody(invocationContext.eventName, runUrl, dispatchedWorkflowName || undefined, rawContext?.workflowEmoji);

  // Discussion comments use GraphQL node IDs and a dedicated update mutation.
  if (reusableComment.id.startsWith("DC_")) {
    const result = await github.graphql(
      `
      mutation($commentId: ID!, $body: String!) {
        updateDiscussionComment(input: { commentId: $commentId, body: $body }) {
          comment { id url }
        }
      }`,
      { commentId: reusableComment.id, body: commentBody }
    );
    const updatedUrl = result?.updateDiscussionComment?.comment?.url;
    return typeof updatedUrl === "string" && updatedUrl.trim() ? updatedUrl : reusableComment.url;
  }

  const commentRepo = reusableComment.repo || invocationContext.eventRepo;
  const numericCommentId = Number(reusableComment.id);
  if (!Number.isInteger(numericCommentId) || numericCommentId <= 0) {
    throw new Error(`${ERR_VALIDATION}: Reusable status comment ID must be a positive integer (received "${reusableComment.id}")`);
  }

  const response = await github.request("PATCH /repos/{owner}/{repo}/issues/comments/{comment_id}", {
    owner: commentRepo.owner,
    repo: commentRepo.repo,
    comment_id: numericCommentId,
    body: commentBody,
    headers: { Accept: "application/vnd.github+json" },
  });
  const updatedUrl = response?.data?.html_url;
  return typeof updatedUrl === "string" && updatedUrl.trim() ? updatedUrl : reusableComment.url;
}

/**
 * Add a comment with a workflow run link to the triggering item.
 * This script ONLY creates comments - it does NOT add reactions.
 * Use add_reaction.cjs in the pre-activation job to add reactions first for immediate feedback.
 * @param {Omit<typeof context, "issue" | "repo"> & {repo?: typeof context.repo, workflowEmoji?: string, nonFatalStatusCommentErrors?: boolean}} [rawContext]
 */
async function createOrReuseStatusComment(rawContext = context) {
  const messagesConfig = getMessages();
  if (!parseBoolTemplatable(messagesConfig?.activationComments, true)) {
    core.info("activation-comments is disabled: skipping activation comment creation");
    return null;
  }

  const invocationContext = resolveInvocationContext(rawContext);
  const reusableComment = readReusableStatusComment(rawContext);
  if (reusableComment) {
    core.info(`Reusing existing status comment ID: ${reusableComment.id}`);
    if (!reusableComment.repo) {
      core.warning("Reusable status comment repo missing; falling back to the invocation event repo.");
    }
    let reusableCommentUrl = reusableComment.url;
    try {
      reusableCommentUrl = await updateReusableStatusComment(reusableComment, invocationContext, rawContext);
      core.info("Updated reusable status comment with current workflow run metadata");
    } catch (error) {
      core.warning(`Failed to update reusable status comment body: ${getErrorMessage(error)}`);
      if (!reusableCommentUrl) {
        core.warning("No fallback reusable status comment URL available; comment-url output will be empty.");
      }
    }
    const outputs = setCommentOutputs(reusableComment.id, reusableCommentUrl, reusableComment.repo || invocationContext.eventRepo, { logReuse: true });
    return {
      ...outputs,
      reused: true,
    };
  }

  const runUrl = buildWorkflowRunUrl(rawContext, invocationContext.workflowRepo);

  core.info(`Run ID: ${rawContext.runId}`);
  core.info(`Run URL: ${runUrl}`);
  core.info(`Event source: ${invocationContext.source}`);

  // Determine the API endpoint based on the event type
  let commentEndpoint;
  const eventName = invocationContext.eventName;
  const owner = invocationContext.eventRepo.owner;
  const repo = invocationContext.eventRepo.repo;
  const payload = invocationContext.eventPayload;

  switch (eventName) {
    case "issues":
    case "issue_comment": {
      const number = payload?.issue?.number;
      if (!number) {
        reportCommentError(rawContext, `${ERR_NOT_FOUND}: Issue number not found in event payload`);
        return null;
      }
      commentEndpoint = { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: number } };
      break;
    }

    case "pull_request":
    case "pull_request_comment":
    case "pull_request_review":
    case "pull_request_review_comment": {
      const number = payload?.pull_request?.number;
      if (!number) {
        reportCommentError(rawContext, `${ERR_NOT_FOUND}: Pull request number not found in event payload`);
        return null;
      }
      commentEndpoint = { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner, repo, issue_number: number } };
      break;
    }

    case "discussion": {
      const discussionNumber = payload?.discussion?.number;
      if (!discussionNumber) {
        reportCommentError(rawContext, `${ERR_NOT_FOUND}: Discussion number not found in event payload`);
        return null;
      }
      commentEndpoint = `discussion:${discussionNumber}`;
      break;
    }

    case "discussion_comment": {
      const discussionCommentNumber = payload?.discussion?.number;
      const discussionCommentId = payload?.comment?.id;
      if (!discussionCommentNumber || !discussionCommentId) {
        reportCommentError(rawContext, `${ERR_NOT_FOUND}: Discussion or comment information not found in event payload`);
        return null;
      }
      commentEndpoint = `discussion_comment:${discussionCommentNumber}:${discussionCommentId}`;
      break;
    }

    default:
      reportCommentError(rawContext, `${ERR_VALIDATION}: Unsupported event type: ${eventName}`);
      return null;
  }

  core.info(`Creating comment on: ${typeof commentEndpoint === "object" ? commentEndpoint.route : commentEndpoint}`);
  return addCommentWithWorkflowLink(commentEndpoint, runUrl, eventName, { ...invocationContext, workflowEmoji: rawContext?.workflowEmoji });
}

async function main() {
  try {
    await createOrReuseStatusComment(context);
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    // Don't fail the job - just warn since this is not critical
    core.warning(`Failed to create comment with workflow link: ${errorMessage}`);
  }
}

/**
 * Build the comment body text for a workflow run link.
 * Sanitizes the content and appends all required markers.
 * @param {string} eventName - The event type
 * @param {string} runUrl - The URL of the workflow run
 * @param {string} [workflowNameOverride] - Optional dispatched workflow name override
 * @param {string} [workflowEmojiOverride] - Optional workflow emoji override
 * @returns {string} The assembled comment body
 */
function buildCommentBody(eventName, runUrl, workflowNameOverride, workflowEmojiOverride) {
  // Whitespace-only overrides are treated as absent and fall back to env defaults.
  const normalizedWorkflowNameOverride = workflowNameOverride?.trim();
  const workflowName = normalizedWorkflowNameOverride || process.env.GH_AW_WORKFLOW_NAME || process.env.GITHUB_WORKFLOW || "Workflow";
  const workflowEmoji = workflowEmojiOverride || process.env.GH_AW_WORKFLOW_EMOJI;
  const eventTypeDescription = EVENT_TYPE_DESCRIPTIONS[eventName] ?? "event";

  // Sanitize before adding markers (defense in depth for custom message templates)
  let body = sanitizeContent(getRunStartedMessage({ workflowName, runUrl, eventType: eventTypeDescription, emoji: workflowEmoji }));

  // Add lock notice if lock-for-agent is enabled for issues or issue_comment
  if (process.env.GH_AW_LOCK_FOR_AGENT === "true" && (eventName === "issues" || eventName === "issue_comment")) {
    body += "\n\n🔒 This issue has been locked while the workflow is running to prevent concurrent modifications.";
  }

  // Add workflow-id marker for hide-older-comments feature
  const workflowId = process.env.GITHUB_WORKFLOW || "";
  if (workflowId) {
    body += `\n\n${generateWorkflowIdMarker(workflowId)}`;
  }

  // Add tracker-id marker for backwards compatibility
  const trackerId = process.env.GH_AW_TRACKER_ID || "";
  if (trackerId) {
    body += `\n\n<!-- gh-aw-tracker-id: ${trackerId} -->`;
  }

  // Identify this as a reaction comment (prevents it from being hidden by hide-older-comments)
  body += `\n\n<!-- gh-aw-comment-type: reaction -->`;

  return body;
}

/**
 * Post a GraphQL comment to a discussion, optionally as a threaded reply.
 * @param {number} discussionNumber - The discussion number
 * @param {string} commentBody - The comment body
 * @param {string|null} replyToNodeId - Parent comment node ID for threading (null for top-level)
 * @param {{ owner: string, repo: string }} [eventRepo] - Repository where the discussion exists (defaults to context.repo at runtime)
 */
async function postDiscussionComment(discussionNumber, commentBody, replyToNodeId = null, eventRepo = context.repo) {
  const discussionId = await getDiscussionNodeId(discussionNumber, eventRepo);
  const mutation = replyToNodeId
    ? `mutation($dId: ID!, $body: String!, $replyToId: ID!) {
        addDiscussionComment(input: { discussionId: $dId, body: $body, replyToId: $replyToId }) {
          comment { id url }
        }
      }`
    : `mutation($dId: ID!, $body: String!) {
        addDiscussionComment(input: { discussionId: $dId, body: $body }) {
          comment { id url }
        }
      }`;
  const result = await github.graphql(mutation, { dId: discussionId, body: commentBody, ...(replyToNodeId ? { replyToId: replyToNodeId } : {}) });
  const comment = result.addDiscussionComment.comment;
  return setCommentOutputs(comment.id, comment.url, eventRepo);
}

/**
 * Add a comment with a workflow run link
 * @param {{ route: string, params: Record<string, unknown> } | string} endpoint - Typed route info for REST events, or special format string for discussions ("discussion:NUMBER" or "discussion_comment:NUMBER:COMMENT_ID")
 * @param {string} runUrl - The URL of the workflow run
 * @param {string} eventName - The event type (to determine the comment text)
 * @param {{
 *   source: "native" | "workflow_dispatch" | "repository_dispatch";
 *   eventName: string;
 *   eventPayload: any;
 *   workflowRepo: { owner: string, repo: string };
 *   eventRepo: { owner: string, repo: string };
 *   workflowEmoji?: string;
 * }|null} [invocationContext=null] - Invocation context overrides for event payload, repo, and emoji
 */
async function addCommentWithWorkflowLink(endpoint, runUrl, eventName, invocationContext = null) {
  const eventPayload = invocationContext?.eventPayload || context.payload;
  const eventRepo = invocationContext?.eventRepo || context.repo;
  const commentBody = buildCommentBody(eventName, runUrl, undefined, invocationContext?.workflowEmoji);

  if (eventName === "discussion") {
    if (typeof endpoint !== "string") {
      throw new Error(`${ERR_VALIDATION}: Unexpected comment endpoint shape for event: ${eventName}`);
    }
    const discussionNumber = parseDiscussionEndpoint(endpoint, eventName);
    return postDiscussionComment(discussionNumber, commentBody, null, eventRepo);
  }

  if (eventName === "discussion_comment") {
    if (typeof endpoint !== "string") {
      throw new Error(`${ERR_VALIDATION}: Unexpected comment endpoint shape for event: ${eventName}`);
    }
    const discussionNumber = parseDiscussionEndpoint(endpoint, eventName);

    // GitHub Discussions only supports two nesting levels, so resolve the top-level parent's node ID
    const commentNodeId = await resolveTopLevelDiscussionCommentId(github, eventPayload?.comment?.node_id);
    return postDiscussionComment(discussionNumber, commentBody, commentNodeId, eventRepo);
  }

  // Create a new comment for non-discussion events using typed route
  if (!isRestEndpoint(endpoint)) {
    throw new Error(`${ERR_VALIDATION}: Unexpected comment endpoint shape for event: ${eventName}`);
  }
  const { route, params } = endpoint;
  const createResponse = await github.request(route, {
    ...params,
    body: commentBody,
    headers: { Accept: "application/vnd.github+json" },
  });

  return setCommentOutputs(createResponse.data.id, createResponse.data.html_url, eventRepo);
}

module.exports = { main, addCommentWithWorkflowLink, buildCommentBody, postDiscussionComment, createOrReuseStatusComment, parseDiscussionEndpoint };
