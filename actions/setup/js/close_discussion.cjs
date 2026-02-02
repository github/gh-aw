// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Get discussion details using GraphQL with pagination for labels
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @returns {Promise<{id: string, title: string, category: {name: string}, labels: {nodes: Array<{name: string}>}, url: string}>} Discussion details
 */
async function getDiscussionDetails(github, owner, repo, discussionNumber) {
  // Fetch all labels with pagination
  const allLabels = [];
  let hasNextPage = true;
  let cursor = null;
  let discussion = null;

  while (hasNextPage) {
    const query = await github.graphql(
      `
      query($owner: String!, $repo: String!, $num: Int!, $cursor: String) {
        repository(owner: $owner, name: $repo) {
          discussion(number: $num) {
            id
            title
            category {
              name
            }
            url
            labels(first: 100, after: $cursor) {
              nodes {
                name
              }
              pageInfo {
                hasNextPage
                endCursor
              }
            }
          }
        }
      }`,
      { owner, repo, num: discussionNumber, cursor }
    );

    if (!query?.repository?.discussion) {
      throw new Error(`Discussion #${discussionNumber} not found in ${owner}/${repo}`);
    }

    // Store the discussion metadata from the first query
    if (!discussion) {
      discussion = {
        id: query.repository.discussion.id,
        title: query.repository.discussion.title,
        category: query.repository.discussion.category,
        url: query.repository.discussion.url,
      };
    }

    const labels = query.repository.discussion.labels?.nodes || [];
    allLabels.push(...labels);

    hasNextPage = query.repository.discussion.labels?.pageInfo?.hasNextPage || false;
    cursor = query.repository.discussion.labels?.pageInfo?.endCursor || null;
  }

  if (!discussion) {
    throw new Error(`Failed to fetch discussion #${discussionNumber}`);
  }

  return {
    id: discussion.id,
    title: discussion.title,
    category: discussion.category,
    url: discussion.url,
    labels: {
      nodes: allLabels,
    },
  };
}

/**
 * Add comment to a GitHub Discussion using GraphQL
 * @param {any} github - GitHub GraphQL instance
 * @param {string} discussionId - Discussion node ID
 * @param {string} message - Comment body
 * @returns {Promise<{id: string, url: string}>} Comment details
 */
async function addDiscussionComment(github, discussionId, message) {
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
    { dId: discussionId, body: message }
  );

  return result.addDiscussionComment.comment;
}

/**
 * Close a GitHub Discussion using GraphQL
 * @param {any} github - GitHub GraphQL instance
 * @param {string} discussionId - Discussion node ID
 * @param {string|undefined} reason - Optional close reason (RESOLVED, DUPLICATE, OUTDATED, or ANSWERED)
 * @returns {Promise<{id: string, url: string}>} Discussion details
 */
async function closeDiscussion(github, discussionId, reason) {
  const mutation = reason
    ? `
      mutation($dId: ID!, $reason: DiscussionCloseReason!) {
        closeDiscussion(input: { discussionId: $dId, reason: $reason }) {
          discussion { 
            id
            url
          }
        }
      }`
    : `
      mutation($dId: ID!) {
        closeDiscussion(input: { discussionId: $dId }) {
          discussion { 
            id
            url
          }
        }
      }`;

  const variables = reason ? { dId: discussionId, reason } : { dId: discussionId };
  const result = await github.graphql(mutation, variables);

  return result.closeDiscussion.discussion;
}

/**
 * Main handler factory for close_discussion
 * Returns a message handler function that processes individual close_discussion messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const maxCount = config.max || 10;

  core.info(`Close discussion configuration: max=${maxCount}`);
  if (requiredLabels.length > 0) {
    core.info(`Required labels: ${requiredLabels.join(", ")}`);
  }
  if (requiredTitlePrefix) {
    core.info(`Required title prefix: ${requiredTitlePrefix}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single close_discussion message
   * @param {Object} message - The close_discussion message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCloseDiscussion(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping close_discussion: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    // Determine discussion number
    const discussionNumber = message.discussion_number !== undefined ? parseInt(String(message.discussion_number), 10) : context.payload?.discussion?.number;

    if (!discussionNumber || isNaN(discussionNumber)) {
      const errorMsg = message.discussion_number !== undefined ? `Invalid discussion number: ${message.discussion_number}` : "No discussion_number provided and not in discussion context";
      core.warning(errorMsg);
      return {
        success: false,
        error: errorMsg,
      };
    }

    try {
      // Fetch discussion details
      const discussion = await getDiscussionDetails(github, context.repo.owner, context.repo.repo, discussionNumber);

      // Validate required labels if configured
      if (requiredLabels.length > 0) {
        const discussionLabels = new Set(discussion.labels.nodes.map(l => l.name));
        const missingLabels = requiredLabels.filter(required => !discussionLabels.has(required));
        if (missingLabels.length > 0) {
          const error = `Missing required labels: ${missingLabels.join(", ")}`;
          core.warning(`Discussion #${discussionNumber} ${error}`);
          return { success: false, error };
        }
      }

      // Validate required title prefix if configured
      if (requiredTitlePrefix && !discussion.title.startsWith(requiredTitlePrefix)) {
        core.warning(`Discussion #${discussionNumber} title doesn't start with "${requiredTitlePrefix}"`);
        return {
          success: false,
          error: `Title doesn't start with "${requiredTitlePrefix}"`,
        };
      }

      // Add comment if body is provided
      let commentUrl;
      if (message.body) {
        const comment = await addDiscussionComment(github, discussion.id, message.body);
        core.info(`Added comment to discussion #${discussionNumber}: ${comment.url}`);
        commentUrl = comment.url;
      }

      // Close the discussion
      const reason = message.reason ?? undefined;
      core.info(`Closing discussion #${discussionNumber} with reason: ${reason || "none"}`);
      await closeDiscussion(github, discussion.id, reason);
      core.info(`Closed discussion #${discussionNumber}: ${discussion.url}`);

      return {
        success: true,
        number: discussionNumber,
        url: discussion.url,
        commentUrl,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to close discussion #${discussionNumber}: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
