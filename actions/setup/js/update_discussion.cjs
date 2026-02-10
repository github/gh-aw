// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { isDiscussionContext, getDiscussionNumber } = require("./update_context_helpers.cjs");
const { createUpdateHandlerFactory } = require("./update_handler_factory.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");

/**
 * Execute the discussion update API call using GraphQL
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {number} discussionNumber - Discussion number to update
 * @param {any} updateData - Data to update
 * @returns {Promise<any>} Updated discussion
 */
async function executeDiscussionUpdate(github, context, discussionNumber, updateData) {
  // First, fetch the discussion node ID
  const getDiscussionQuery = `
    query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $number) {
          id
          title
          body
          url
        }
      }
    }
  `;

  const queryResult = await github.graphql(getDiscussionQuery, {
    owner: context.repo.owner,
    repo: context.repo.repo,
    number: discussionNumber,
  });

  const discussion = queryResult?.repository?.discussion;
  if (!discussion) {
    throw new Error(`Discussion #${discussionNumber} not found`);
  }

  // Build mutation for updating discussion
  let mutation = `
    mutation($discussionId: ID!, $title: String, $body: String) {
      updateDiscussion(input: { discussionId: $discussionId, title: $title, body: $body }) {
        discussion {
          id
          title
          body
          url
        }
      }
    }
  `;

  const variables = {
    discussionId: discussion.id,
    title: updateData.title || discussion.title,
    body: updateData.body || discussion.body,
  };

  const mutationResult = await github.graphql(mutation, variables);
  return mutationResult.updateDiscussion.discussion;
}

/**
 * Resolve discussion number from message and configuration
 * Discussions have special handling - they don't use the standard resolveTarget helper
 * @param {Object} item - The message item
 * @param {string} updateTarget - Target configuration
 * @param {Object} context - GitHub Actions context
 * @returns {{success: true, number: number} | {success: false, error: string}} Resolution result
 */
function resolveDiscussionNumber(item, updateTarget, context) {
  // Discussions are special - they have their own context type separate from issues/PRs
  // We need to handle them differently
  if (item.discussion_number !== undefined) {
    const discussionNumber = parseInt(String(item.discussion_number), 10);
    if (isNaN(discussionNumber)) {
      return {
        success: false,
        error: `Invalid discussion number: ${item.discussion_number}`,
      };
    }
    return { success: true, number: discussionNumber };
  } else if (updateTarget !== "triggering") {
    // Explicit number target
    const discussionNumber = parseInt(updateTarget, 10);
    if (isNaN(discussionNumber) || discussionNumber <= 0) {
      return {
        success: false,
        error: `Invalid discussion number in target: ${updateTarget}`,
      };
    }
    return { success: true, number: discussionNumber };
  } else {
    // Use triggering context (default)
    if (isDiscussionContext(context.eventName, context.payload)) {
      const discussionNumber = getDiscussionNumber(context.payload);
      if (!discussionNumber) {
        return {
          success: false,
          error: "No discussion number available",
        };
      }
      return { success: true, number: discussionNumber };
    } else {
      return {
        success: false,
        error: "Not in discussion context",
      };
    }
  }
}

/**
 * Build update data from message
 * @param {Object} item - The message item
 * @param {Object} config - Configuration object
 * @returns {{success: true, data: Object} | {success: false, error: string}} Update data result
 */
function buildDiscussionUpdateData(item, config) {
  const updateData = {};

  if (item.title !== undefined) {
    // Sanitize title for Unicode security (no prefix handling needed for updates)
    updateData.title = sanitizeTitle(item.title);
  }
  if (item.body !== undefined) {
    updateData.body = item.body;
  }

  return { success: true, data: updateData };
}

/**
 * Format success result for discussion update
 * @param {number} discussionNumber - Discussion number
 * @param {Object} updatedDiscussion - Updated discussion object
 * @returns {Object} Formatted success result
 */
function formatDiscussionSuccessResult(discussionNumber, updatedDiscussion) {
  return {
    success: true,
    number: discussionNumber,
    url: updatedDiscussion.url,
    title: updatedDiscussion.title,
  };
}

/**
 * Main handler factory for update_discussion
 * Returns a message handler function that processes individual update_discussion messages
 * @type {HandlerFactoryFunction}
 */
const main = createUpdateHandlerFactory({
  itemType: "update_discussion",
  itemTypeName: "discussion",
  supportsPR: false,
  resolveItemNumber: resolveDiscussionNumber,
  buildUpdateData: buildDiscussionUpdateData,
  executeUpdate: executeDiscussionUpdate,
  formatSuccessResult: formatDiscussionSuccessResult,
});

module.exports = { main };
