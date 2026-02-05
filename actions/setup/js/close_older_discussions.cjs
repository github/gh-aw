// @ts-check
/// <reference types="@actions/github-script" />

const { getCloseOlderDiscussionMessage } = require("./messages_close_discussion.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { getWorkflowIdMarkerContent } = require("./generate_footer.cjs");

/**
 * Maximum number of older discussions to close
 */
const MAX_CLOSE_COUNT = 10;

/**
 * Delay between GraphQL API calls in milliseconds to avoid rate limiting
 */
const GRAPHQL_DELAY_MS = 500;

/**
 * Delay execution for a specified number of milliseconds
 * @param {number} ms - Milliseconds to delay
 * @returns {Promise<void>}
 */
function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Search for open discussions with a matching workflow-id marker
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {string|undefined} categoryId - Optional category ID to filter by
 * @param {number} excludeNumber - Discussion number to exclude (the newly created one)
 * @returns {Promise<Array<{id: string, number: number, title: string, url: string}>>} Matching discussions
 */
async function searchOlderDiscussions(github, owner, repo, workflowId, categoryId, excludeNumber) {
  core.info(`Starting search for older discussions in ${owner}/${repo}`);
  core.info(`  Workflow ID: ${workflowId || "(none)"}`);
  core.info(`  Exclude discussion number: ${excludeNumber}`);

  if (!workflowId) {
    core.info("No workflow ID provided - cannot search for older discussions");
    return [];
  }

  // Build GraphQL search query
  // Search for open discussions with the workflow-id marker in the body
  const workflowIdMarker = getWorkflowIdMarkerContent(workflowId);
  // Escape quotes in workflow ID to prevent query injection
  const escapedMarker = workflowIdMarker.replace(/"/g, '\\"');
  let searchQuery = `repo:${owner}/${repo} is:open "${escapedMarker}" in:body`;

  core.info(`  Added workflow ID marker filter to query: "${escapedMarker}" in:body`);
  core.info(`Executing GitHub search with query: ${searchQuery}`);

  const result = await github.graphql(
    `
    query($searchTerms: String!, $first: Int!) {
      search(query: $searchTerms, type: DISCUSSION, first: $first) {
        nodes {
          ... on Discussion {
            id
            number
            title
            url
            category {
              id
            }
            closed
          }
        }
      }
    }`,
    { searchTerms: searchQuery, first: 50 }
  );

  core.info(`Search API returned ${result?.search?.nodes?.length || 0} total results`);

  if (!result || !result.search || !result.search.nodes) {
    core.info("No results returned from search API");
    return [];
  }

  // Filter results:
  // 1. Must not be the excluded discussion (newly created one)
  // 2. Must not be already closed
  // 3. If categoryId is specified, must match
  core.info("Filtering search results...");
  let filteredCount = 0;
  let excludedCount = 0;
  let closedCount = 0;

  const filtered = result.search.nodes
    .filter(
      /** @param {any} d */ d => {
        if (!d) {
          return false;
        }

        // Exclude the newly created discussion
        if (d.number === excludeNumber) {
          excludedCount++;
          core.info(`  Excluding discussion #${d.number} (the newly created discussion)`);
          return false;
        }

        // Exclude already closed discussions
        if (d.closed) {
          closedCount++;
          return false;
        }

        // Check category if specified
        if (categoryId && (!d.category || d.category.id !== categoryId)) {
          return false;
        }

        filteredCount++;
        core.info(`  ✓ Discussion #${d.number} matches criteria: ${d.title}`);
        return true;
      }
    )
    .map(
      /** @param {any} d */ d => ({
        id: d.id,
        number: d.number,
        title: d.title,
        url: d.url,
      })
    );

  core.info(`Filtering complete:`);
  core.info(`  - Matched discussions: ${filteredCount}`);
  core.info(`  - Excluded new discussion: ${excludedCount}`);
  core.info(`  - Excluded closed discussions: ${closedCount}`);

  return filtered;
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
 * Close a GitHub Discussion as OUTDATED using GraphQL
 * @param {any} github - GitHub GraphQL instance
 * @param {string} discussionId - Discussion node ID
 * @returns {Promise<{id: string, url: string}>} Discussion details
 */
async function closeDiscussionAsOutdated(github, discussionId) {
  const result = await github.graphql(
    `
    mutation($dId: ID!) {
      closeDiscussion(input: { discussionId: $dId, reason: OUTDATED }) {
        discussion { 
          id
          url
        }
      }
    }`,
    { dId: discussionId }
  );

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
 * @returns {Promise<Array<{number: number, url: string}>>} List of closed discussions
 */
async function closeOlderDiscussions(github, owner, repo, workflowId, categoryId, newDiscussion, workflowName, runUrl) {
  core.info("=".repeat(70));
  core.info("Starting closeOlderDiscussions operation");
  core.info("=".repeat(70));

  core.info(`Search criteria: workflow ID marker: "${getWorkflowIdMarkerContent(workflowId)}"`);
  core.info(`New discussion reference: #${newDiscussion.number} (${newDiscussion.url})`);
  core.info(`Workflow: ${workflowName}`);
  core.info(`Run URL: ${runUrl}`);
  core.info("");

  const olderDiscussions = await searchOlderDiscussions(github, owner, repo, workflowId, categoryId, newDiscussion.number);

  if (olderDiscussions.length === 0) {
    core.info("✓ No older discussions found to close - operation complete");
    core.info("=".repeat(70));
    return [];
  }

  core.info("");
  core.info(`Found ${olderDiscussions.length} older discussion(s) matching the criteria`);
  for (const discussion of olderDiscussions) {
    core.info(`  - Discussion #${discussion.number}: ${discussion.title}`);
    core.info(`    URL: ${discussion.url}`);
  }

  // Limit to MAX_CLOSE_COUNT discussions
  const discussionsToClose = olderDiscussions.slice(0, MAX_CLOSE_COUNT);

  if (olderDiscussions.length > MAX_CLOSE_COUNT) {
    core.warning("");
    core.warning(`⚠️  Found ${olderDiscussions.length} older discussions, but only closing the first ${MAX_CLOSE_COUNT}`);
    core.warning(`    The remaining ${olderDiscussions.length - MAX_CLOSE_COUNT} discussion(s) will be processed in subsequent runs`);
  }

  core.info("");
  core.info(`Preparing to close ${discussionsToClose.length} discussion(s)...`);
  core.info("");

  const closedDiscussions = [];

  for (let i = 0; i < discussionsToClose.length; i++) {
    const discussion = discussionsToClose[i];
    core.info("-".repeat(70));
    core.info(`Processing discussion ${i + 1}/${discussionsToClose.length}: #${discussion.number}`);
    core.info(`  Title: ${discussion.title}`);
    core.info(`  URL: ${discussion.url}`);

    try {
      // Generate closing message using the messages module
      const closingMessage = getCloseOlderDiscussionMessage({
        newDiscussionUrl: newDiscussion.url,
        newDiscussionNumber: newDiscussion.number,
        workflowName,
        runUrl,
      });

      core.info(`  Message length: ${closingMessage.length} characters`);
      core.info("");

      // Add comment first
      await addDiscussionComment(github, discussion.id, closingMessage);

      // Then close the discussion as outdated
      await closeDiscussionAsOutdated(github, discussion.id);

      closedDiscussions.push({
        number: discussion.number,
        url: discussion.url,
      });

      core.info("");
      core.info(`✓ Successfully closed discussion #${discussion.number}`);
    } catch (error) {
      core.info("");
      core.error(`✗ Failed to close discussion #${discussion.number}`);
      core.error(`  Error: ${getErrorMessage(error)}`);
      if (error instanceof Error && error.stack) {
        core.error(`  Stack trace: ${error.stack}`);
      }
      // Continue with other discussions even if one fails
    }

    // Add delay between GraphQL operations to avoid rate limiting (except for the last item)
    if (i < discussionsToClose.length - 1) {
      core.info("");
      core.info(`Waiting ${GRAPHQL_DELAY_MS}ms before processing next discussion to avoid rate limiting...`);
      await delay(GRAPHQL_DELAY_MS);
    }
  }

  core.info("");
  core.info("=".repeat(70));
  core.info(`Closed ${closedDiscussions.length} of ${discussionsToClose.length} discussion(s) successfully`);
  if (closedDiscussions.length < discussionsToClose.length) {
    core.warning(`Failed to close ${discussionsToClose.length - closedDiscussions.length} discussion(s) - check logs above for details`);
  }
  core.info("=".repeat(70));

  return closedDiscussions;
}

module.exports = {
  closeOlderDiscussions,
  searchOlderDiscussions,
  addDiscussionComment,
  closeDiscussionAsOutdated,
  MAX_CLOSE_COUNT,
  GRAPHQL_DELAY_MS,
};
