// @ts-check
/// <reference types="@actions/github-script" />

const { getRunStartedMessage } = require("./messages_run_status.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");

async function main() {
  // Read inputs from environment variables
  const reaction = process.env.GH_AW_REACTION || "eyes";
  const command = process.env.GH_AW_COMMAND; // Only present for command workflows
  const runId = context.runId;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

  core.info(`Reaction type: ${reaction}`);
  core.info(`Command name: ${command || "none"}`);
  core.info(`Run ID: ${runId}`);
  core.info(`Run URL: ${runUrl}`);

  // Validate reaction type
  const validReactions = ["+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"];
  if (!validReactions.includes(reaction)) {
    core.setFailed(`Invalid reaction type: ${reaction}. Valid reactions are: ${validReactions.join(", ")}`);
    return;
  }

  // Determine the API endpoint based on the event type
  let reactionEndpoint;
  let commentUpdateEndpoint;
  let shouldCreateComment = false;
  const eventName = context.eventName;
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  try {
    switch (eventName) {
      case "issues":
        const issueNumber = context.payload?.issue?.number;
        if (!issueNumber) {
          core.setFailed("Issue number not found in event payload");
          return;
        }
        reactionEndpoint = `/repos/${owner}/${repo}/issues/${issueNumber}/reactions`;
        commentUpdateEndpoint = `/repos/${owner}/${repo}/issues/${issueNumber}/comments`;
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      case "issue_comment":
        const commentId = context.payload?.comment?.id;
        const issueNumberForComment = context.payload?.issue?.number;
        if (!commentId) {
          core.setFailed("Comment ID not found in event payload");
          return;
        }
        if (!issueNumberForComment) {
          core.setFailed("Issue number not found in event payload");
          return;
        }
        reactionEndpoint = `/repos/${owner}/${repo}/issues/comments/${commentId}/reactions`;
        // Create new comment on the issue itself, not on the comment
        commentUpdateEndpoint = `/repos/${owner}/${repo}/issues/${issueNumberForComment}/comments`;
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      case "pull_request":
        const prNumber = context.payload?.pull_request?.number;
        if (!prNumber) {
          core.setFailed("Pull request number not found in event payload");
          return;
        }
        // PRs are "issues" for the reactions endpoint
        reactionEndpoint = `/repos/${owner}/${repo}/issues/${prNumber}/reactions`;
        commentUpdateEndpoint = `/repos/${owner}/${repo}/issues/${prNumber}/comments`;
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      case "pull_request_review_comment":
        const reviewCommentId = context.payload?.comment?.id;
        const prNumberForReviewComment = context.payload?.pull_request?.number;
        if (!reviewCommentId) {
          core.setFailed("Review comment ID not found in event payload");
          return;
        }
        if (!prNumberForReviewComment) {
          core.setFailed("Pull request number not found in event payload");
          return;
        }
        reactionEndpoint = `/repos/${owner}/${repo}/pulls/comments/${reviewCommentId}/reactions`;
        // Create new comment on the PR itself (using issues endpoint since PRs are issues)
        commentUpdateEndpoint = `/repos/${owner}/${repo}/issues/${prNumberForReviewComment}/comments`;
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      case "discussion":
        const discussionNumber = context.payload?.discussion?.number;
        if (!discussionNumber) {
          core.setFailed("Discussion number not found in event payload");
          return;
        }
        // Discussions use GraphQL API - get the node ID
        const discussion = await getDiscussionId(owner, repo, discussionNumber);
        reactionEndpoint = discussion.id; // Store node ID for GraphQL
        commentUpdateEndpoint = `discussion:${discussionNumber}`; // Special format to indicate discussion
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      case "discussion_comment":
        const discussionCommentNumber = context.payload?.discussion?.number;
        const discussionCommentId = context.payload?.comment?.id;
        if (!discussionCommentNumber || !discussionCommentId) {
          core.setFailed("Discussion or comment information not found in event payload");
          return;
        }
        // Get the comment node ID from the payload
        const commentNodeId = context.payload?.comment?.node_id;
        if (!commentNodeId) {
          core.setFailed("Discussion comment node ID not found in event payload");
          return;
        }
        reactionEndpoint = commentNodeId; // Store node ID for GraphQL
        commentUpdateEndpoint = `discussion_comment:${discussionCommentNumber}:${discussionCommentId}`; // Special format
        // Create comments for all workflows using reactions
        shouldCreateComment = true;
        break;

      default:
        core.setFailed(`Unsupported event type: ${eventName}`);
        return;
    }

    core.info(`Reaction API endpoint: ${reactionEndpoint}`);

    // Add reaction first
    // For discussions, reactionEndpoint is a node ID (GraphQL), otherwise it's a REST API path
    const isDiscussionEvent = eventName === "discussion" || eventName === "discussion_comment";
    if (isDiscussionEvent) {
      await addDiscussionReaction(reactionEndpoint, reaction);
    } else {
      await addReaction(reactionEndpoint, reaction);
    }

    // Then add comment if applicable
    if (shouldCreateComment && commentUpdateEndpoint) {
      core.info(`Comment endpoint: ${commentUpdateEndpoint}`);
      await addCommentWithWorkflowLink(commentUpdateEndpoint, runUrl, eventName);
    } else {
      core.info(`Skipping comment for event type: ${eventName}`);
    }
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to process reaction and comment creation: ${errorMessage}`);
    core.setFailed(`Failed to process reaction and comment creation: ${errorMessage}`);
  }
}

/**
 * Add a reaction to a GitHub issue, PR, or comment using REST API
 * @param {string} endpoint - The GitHub API endpoint to add the reaction to
 * @param {string} reaction - The reaction type to add
 */
async function addReaction(endpoint, reaction) {
  const response = await github.request("POST " + endpoint, {
    content: reaction,
    headers: {
      Accept: "application/vnd.github+json",
    },
  });

  const reactionId = response.data?.id;
  if (reactionId) {
    core.info(`Successfully added reaction: ${reaction} (id: ${reactionId})`);
    core.setOutput("reaction-id", reactionId.toString());
  } else {
    core.info(`Successfully added reaction: ${reaction}`);
    core.setOutput("reaction-id", "");
  }
}

/**
 * Add a reaction to a GitHub discussion or discussion comment using GraphQL
 * @param {string} subjectId - The node ID of the discussion or comment
 * @param {string} reaction - The reaction type to add (mapped to GitHub's ReactionContent enum)
 */
async function addDiscussionReaction(subjectId, reaction) {
  // Map reaction names to GitHub's GraphQL ReactionContent enum
  const reactionMap = {
    "+1": "THUMBS_UP",
    "-1": "THUMBS_DOWN",
    laugh: "LAUGH",
    confused: "CONFUSED",
    heart: "HEART",
    hooray: "HOORAY",
    rocket: "ROCKET",
    eyes: "EYES",
  };

  const reactionContent = reactionMap[reaction];
  if (!reactionContent) {
    throw new Error(`Invalid reaction type for GraphQL: ${reaction}`);
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
 * @returns {Promise<{id: string, url: string}>} Discussion details
 */
async function getDiscussionId(owner, repo, discussionNumber) {
  const { repository } = await github.graphql(
    `
    query($owner: String!, $repo: String!, $num: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $num) { 
          id 
          url
        }
      }
    }`,
    { owner, repo, num: discussionNumber }
  );

  if (!repository || !repository.discussion) {
    throw new Error(`Discussion #${discussionNumber} not found in ${owner}/${repo}`);
  }

  return {
    id: repository.discussion.id,
    url: repository.discussion.url,
  };
}

/**
 * Get the node ID for a discussion comment
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @param {number} commentId - Comment ID (database ID, not node ID)
 * @returns {Promise<{id: string, url: string}>} Comment details
 */
async function getDiscussionCommentId(owner, repo, discussionNumber, commentId) {
  // First, get the discussion ID
  const discussion = await getDiscussionId(owner, repo, discussionNumber);
  if (!discussion) throw new Error(`Discussion #${discussionNumber} not found in ${owner}/${repo}`);

  // Then fetch the comment by traversing discussion comments
  // Note: GitHub's GraphQL API doesn't provide a direct way to query comment by database ID
  // We need to use the comment's node ID from the event payload if available
  // For now, we'll use a simplified approach - the commentId from context.payload.comment.node_id

  // If the event payload provides node_id, we can use it directly
  // Otherwise, this would need to fetch all comments and find the matching one
  const nodeId = context.payload?.comment?.node_id;
  if (nodeId) {
    return {
      id: nodeId,
      url: context.payload.comment?.html_url || discussion?.url,
    };
  }

  throw new Error(`Discussion comment node ID not found in event payload for comment ${commentId}`);
}

/**
 * Add a comment with a workflow run link
 * @param {string} endpoint - The GitHub API endpoint to create the comment (or special format for discussions)
 * @param {string} runUrl - The URL of the workflow run
 * @param {string} eventName - The event type (to determine the comment text)
 */
async function addCommentWithWorkflowLink(endpoint, runUrl, eventName) {
  try {
    // Get workflow name from environment variable
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";

    // Determine the event type description
    let eventTypeDescription;
    switch (eventName) {
      case "issues":
        eventTypeDescription = "issue";
        break;
      case "pull_request":
        eventTypeDescription = "pull request";
        break;
      case "issue_comment":
        eventTypeDescription = "issue comment";
        break;
      case "pull_request_review_comment":
        eventTypeDescription = "pull request review comment";
        break;
      case "discussion":
        eventTypeDescription = "discussion";
        break;
      case "discussion_comment":
        eventTypeDescription = "discussion comment";
        break;
      default:
        eventTypeDescription = "event";
    }

    // Use getRunStartedMessage for the workflow link text (supports custom messages)
    const workflowLinkText = getRunStartedMessage({
      workflowName: workflowName,
      runUrl: runUrl,
      eventType: eventTypeDescription,
    });

    // Add workflow-id and tracker-id markers for hide-older-comments feature
    const workflowId = process.env.GITHUB_WORKFLOW || "";
    const trackerId = process.env.GH_AW_TRACKER_ID || "";

    let commentBody = workflowLinkText;

    // Add lock notice if lock-for-agent is enabled for issues or issue_comment
    const lockForAgent = process.env.GH_AW_LOCK_FOR_AGENT === "true";
    if (lockForAgent && (eventName === "issues" || eventName === "issue_comment")) {
      commentBody += "\n\n🔒 This issue has been locked while the workflow is running to prevent concurrent modifications.";
    }

    // Add workflow-id marker if available
    if (workflowId) {
      commentBody += `\n\n${generateWorkflowIdMarker(workflowId)}`;
    }

    // Add tracker-id marker if available (for backwards compatibility)
    if (trackerId) {
      commentBody += `\n\n<!-- gh-aw-tracker-id: ${trackerId} -->`;
    }

    // Add comment type marker to identify this as a reaction comment
    // This prevents it from being hidden by hide-older-comments
    commentBody += `\n\n<!-- gh-aw-comment-type: reaction -->`;

    // Handle discussion events specially
    if (eventName === "discussion") {
      // Parse discussion number from special format: "discussion:NUMBER"
      const discussionNumber = parseInt(endpoint.split(":")[1], 10);

      // Create a new comment on the discussion using GraphQL
      const { repository } = await github.graphql(
        `
        query($owner: String!, $repo: String!, $num: Int!) {
          repository(owner: $owner, name: $repo) {
            discussion(number: $num) { 
              id 
            }
          }
        }`,
        { owner: context.repo.owner, repo: context.repo.repo, num: discussionNumber }
      );

      const discussionId = repository.discussion.id;

      const result = await github.graphql(
        `
        mutation($dId: ID!, $body: String!) {
          addDiscussionComment(input: { discussionId: $dId, body: $body }) {
            comment { 
              id 
              url
            }
          }
        }`,
        { dId: discussionId, body: commentBody }
      );

      const comment = result.addDiscussionComment.comment;
      core.info(`Successfully created discussion comment with workflow link`);
      core.info(`Comment ID: ${comment.id}`);
      core.info(`Comment URL: ${comment.url}`);
      core.info(`Comment Repo: ${context.repo.owner}/${context.repo.repo}`);
      core.setOutput("comment-id", comment.id);
      core.setOutput("comment-url", comment.url);
      core.setOutput("comment-repo", `${context.repo.owner}/${context.repo.repo}`);
      return;
    } else if (eventName === "discussion_comment") {
      // Parse discussion number from special format: "discussion_comment:NUMBER:COMMENT_ID"
      const discussionNumber = parseInt(endpoint.split(":")[1], 10);

      // Create a new comment on the discussion using GraphQL
      const { repository } = await github.graphql(
        `
        query($owner: String!, $repo: String!, $num: Int!) {
          repository(owner: $owner, name: $repo) {
            discussion(number: $num) { 
              id 
            }
          }
        }`,
        { owner: context.repo.owner, repo: context.repo.repo, num: discussionNumber }
      );

      const discussionId = repository.discussion.id;

      // Get the comment node ID to use as the parent for threading
      const commentNodeId = context.payload?.comment?.node_id;

      const result = await github.graphql(
        `
        mutation($dId: ID!, $body: String!, $replyToId: ID!) {
          addDiscussionComment(input: { discussionId: $dId, body: $body, replyToId: $replyToId }) {
            comment { 
              id 
              url
            }
          }
        }`,
        { dId: discussionId, body: commentBody, replyToId: commentNodeId }
      );

      const comment = result.addDiscussionComment.comment;
      core.info(`Successfully created discussion comment with workflow link`);
      core.info(`Comment ID: ${comment.id}`);
      core.info(`Comment URL: ${comment.url}`);
      core.info(`Comment Repo: ${context.repo.owner}/${context.repo.repo}`);
      core.setOutput("comment-id", comment.id);
      core.setOutput("comment-url", comment.url);
      core.setOutput("comment-repo", `${context.repo.owner}/${context.repo.repo}`);
      return;
    }

    // Create a new comment for non-discussion events
    const createResponse = await github.request("POST " + endpoint, {
      body: commentBody,
      headers: {
        Accept: "application/vnd.github+json",
      },
    });

    core.info(`Successfully created comment with workflow link`);
    core.info(`Comment ID: ${createResponse.data.id}`);
    core.info(`Comment URL: ${createResponse.data.html_url}`);
    core.info(`Comment Repo: ${context.repo.owner}/${context.repo.repo}`);
    core.setOutput("comment-id", createResponse.data.id.toString());
    core.setOutput("comment-url", createResponse.data.html_url);
    core.setOutput("comment-repo", `${context.repo.owner}/${context.repo.repo}`);
  } catch (error) {
    // Don't fail the entire job if comment creation fails - just log it
    const errorMessage = getErrorMessage(error);
    core.warning("Failed to create comment with workflow link (This is not critical - the reaction was still added successfully): " + errorMessage);
  }
}

module.exports = { main };
