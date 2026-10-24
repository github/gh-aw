// @ts-check
/// <reference types="@actions/github-script" />

const { getCloseOlderDiscussionMessage } = require("./messages_close_discussion.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { MAX_CLOSE_COUNT: SHARED_MAX_CLOSE_COUNT } = require("./close_older_entities.cjs");
const { searchOlderEntitiesByMarker } = require("./close_older_search_helpers.cjs");
const { createCloseOlderSearchAdapter, closeOlderWithDescriptor } = require("./close_older_handler_factory.cjs");
const { currentClaimHandle } = require("./work_queue_claim_scope.cjs");
const { graphEffectIdentity } = require("./work_queue_effect_client.cjs");

/**
 * Maximum number of older discussions to close
 */
const MAX_CLOSE_COUNT = SHARED_MAX_CLOSE_COUNT;

/**
 * Delay between GraphQL API calls in milliseconds to avoid rate limiting
 */
const GRAPHQL_DELAY_MS = 500;

/**
 * Search for open discussions with a matching workflow-id marker
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {string|undefined} categoryId - Optional category ID to filter by
 * @param {number} excludeNumber - Discussion number to exclude (the newly created one)
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering.
 *   When set, filters by the `gh-aw-workflow-call-id` marker so callers sharing the same
 *   reusable workflow do not close each other's discussions. Falls back to `gh-aw-workflow-id`
 *   when not provided (backward compat for discussions created before this fix).
 * @param {string} [closeOlderKey] - Optional explicit deduplication key. When set, the
 *   `gh-aw-close-key` marker is used as the primary search term and exact filter instead
 *   of the workflow-id / workflow-call-id markers.
 * @returns {Promise<Array<{id: string, number: number, title: string, url: string}>>} Matching discussions
 */
async function searchOlderDiscussions(github, owner, repo, workflowId, categoryId, excludeNumber, callerWorkflowId, closeOlderKey) {
  return searchOlderEntitiesByMarker({
    owner,
    repo,
    workflowId,
    excludeNumber,
    entityType: "discussion",
    callerWorkflowId,
    closeOlderKey,
    executeSearch: searchQuery =>
      github.graphql(
        `
        query($searchTerms: String!, $first: Int!) {
          search(query: $searchTerms, type: DISCUSSION, first: $first) {
            nodes {
              ... on Discussion {
                id
                number
                title
                url
                body
                category {
                  id
                }
                closed
              }
            }
          }
        }`,
        { searchTerms: searchQuery, first: 50 }
      ),
    getItems: result => result?.search?.nodes,
    mapItem: d => ({
      id: d.id,
      number: d.number,
      title: d.title,
      url: d.url,
    }),
    additionalFilter: (d, extra) => {
      if (d.closed) {
        extra.closedCount = (extra.closedCount || 0) + 1;
        return false;
      }
      if (categoryId && (!d.category || d.category.id !== categoryId)) {
        return false;
      }
      return true;
    },
    extraLabels: [["closedCount", "Excluded closed discussions"]],
  });
}

/**
 * Add comment to a GitHub Discussion using GraphQL
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner (unused for GraphQL, but kept for consistency)
 * @param {string} repo - Repository name (unused for GraphQL, but kept for consistency)
 * @param {string} discussionId - Discussion node ID
 * @param {string} message - Comment body
 * @param {Array<object>} [deliveryReceipts] - Trusted native cleanup receipts for Claim readback
 * @returns {Promise<{id: string, url: string}>} Comment details
 */
async function addDiscussionComment(github, owner, repo, discussionId, message, deliveryReceipts = []) {
  const query = `
    mutation($dId: ID!, $body: String!) {
      addDiscussionComment(input: { discussionId: $dId, body: $body }) {
        comment { 
          id 
          url
        }
      }
    }`;
  const variables = { dId: discussionId, body: sanitizeContent(message) };
  const result = await github.graphql(query, variables);
  if (currentClaimHandle()) deliveryReceipts.push({ type: "comment", discussionId, id: result.addDiscussionComment.comment.id, body: variables.body, effectId: graphEffectIdentity(query, variables, result) });

  return result.addDiscussionComment.comment;
}

/**
 * Close a GitHub Discussion as OUTDATED using GraphQL
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner (unused for GraphQL, but kept for consistency)
 * @param {string} repo - Repository name (unused for GraphQL, but kept for consistency)
 * @param {string} discussionId - Discussion node ID
 * @param {Array<object>} [deliveryReceipts] - Trusted native cleanup receipts for Claim readback
 * @returns {Promise<{id: string, url: string}>} Discussion details
 */
async function closeDiscussionAsOutdated(github, owner, repo, discussionId, deliveryReceipts = []) {
  const query = `
    mutation($dId: ID!) {
      closeDiscussion(input: { discussionId: $dId, reason: OUTDATED }) {
        discussion { 
          id
          url
        }
      }
    }`;
  const variables = { dId: discussionId };
  const result = await github.graphql(query, variables);
  if (currentClaimHandle()) deliveryReceipts.push({ type: "close", discussionId, id: result.closeDiscussion.discussion.id, effectId: graphEffectIdentity(query, variables, result) });

  return result.closeDiscussion.discussion;
}

/**
 * Close older discussions that match the workflow-id marker
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {string|undefined} categoryId - Optional category ID to filter by
 * @param {{number: number, url: string}} newDiscussion - The newly created discussion
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering
 * @param {string} [closeOlderKey] - Optional explicit deduplication key for close-older matching
 * @param {Array<object>} [deliveryReceipts] - Trusted native cleanup receipts for Claim readback
 * @returns {Promise<Array<{number: number, url: string}>>} List of closed discussions
 */
async function closeOlderDiscussions(github, owner, repo, workflowId, categoryId, newDiscussion, workflowName, runUrl, callerWorkflowId, closeOlderKey, deliveryReceipts = []) {
  return closeOlderWithDescriptor({
    github,
    owner,
    repo,
    workflowId,
    newEntity: newDiscussion,
    workflowName,
    runUrl,
    entityType: "discussion",
    entityTypePlural: "discussions",
    searchOlderEntities: createCloseOlderSearchAdapter(searchOlderDiscussions, [categoryId], [callerWorkflowId, closeOlderKey]),
    getCloseMessage: params =>
      getCloseOlderDiscussionMessage({
        newDiscussionUrl: params.newEntityUrl,
        newDiscussionNumber: params.newEntityNumber,
        workflowName: params.workflowName,
        runUrl: params.runUrl,
      }),
    addComment: (client, owner, repo, id, message) => addDiscussionComment(client, owner, repo, id, message, deliveryReceipts),
    closeEntity: (client, owner, repo, id) => closeDiscussionAsOutdated(client, owner, repo, id, deliveryReceipts),
    delayMs: GRAPHQL_DELAY_MS,
    getEntityId: entity => entity.id,
    getEntityUrl: entity => entity.url,
    mapClosedEntity: item => ({
      number: item.number,
      url: item.url || "",
    }),
  });
}

module.exports = {
  closeOlderDiscussions,
  searchOlderDiscussions,
  addDiscussionComment,
  closeDiscussionAsOutdated,
  MAX_CLOSE_COUNT,
  GRAPHQL_DELAY_MS,
};
