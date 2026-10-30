// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeContent } = require("./sanitize_content.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { MAX_CLOSE_COUNT: SHARED_MAX_CLOSE_COUNT } = require("./close_older_entities.cjs");
const { searchOlderEntitiesByMarker } = require("./close_older_search_helpers.cjs");
const { createCloseOlderSearchAdapter, closeOlderWithDescriptor } = require("./close_older_handler_factory.cjs");

/**
 * Maximum number of older pull requests to close
 */
const MAX_CLOSE_COUNT = SHARED_MAX_CLOSE_COUNT;

/**
 * Delay between API calls in milliseconds to avoid rate limiting
 */
const API_DELAY_MS = 500;

/**
 * Search for open pull requests with a matching workflow-id marker
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {number} excludeNumber - PR number to exclude (the newly created one)
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering.
 *   When set, filters by the `gh-aw-workflow-call-id` marker so callers sharing the same
 *   reusable workflow do not close each other's PRs. Falls back to `gh-aw-workflow-id`
 *   when not provided (backward compat for PRs created before this fix).
 * @param {string} [closeOlderKey] - Optional explicit deduplication key. When set, the
 *   `gh-aw-close-key` marker is used as the primary search term and exact filter instead
 *   of the workflow-id / workflow-call-id markers.
 * @returns {Promise<Array<{number: number, title: string, html_url: string, labels: Array<{name: string}>, created_at: string}>>} Matching pull requests
 */
async function searchOlderPullRequests(github, owner, repo, workflowId, excludeNumber, callerWorkflowId, closeOlderKey) {
  return searchOlderEntitiesByMarker({
    owner,
    repo,
    workflowId,
    excludeNumber,
    entityType: "pull request",
    callerWorkflowId,
    closeOlderKey,
    entityQualifier: "is:pr",
    executeSearch: searchQuery =>
      github.rest.search.issuesAndPullRequests({
        q: searchQuery,
        per_page: 50,
      }),
    getItems: result => result?.data?.items,
    mapItem: item => ({
      number: item.number,
      title: item.title,
      html_url: item.html_url,
      labels: item.labels || [],
      created_at: item.created_at,
    }),
    additionalFilter: (item, extra) => {
      if (!item.pull_request) {
        extra.issueCount = (extra.issueCount || 0) + 1;
        return false;
      }
      return true;
    },
    extraLabels: [["issueCount", "Excluded issues"]],
  });
}

/**
 * Add comment to a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addPullRequestComment(github, owner, repo, prNumber, message) {
  core.info(`Adding comment to pull request #${prNumber} in ${owner}/${repo}`);
  core.info(`  Comment length: ${message.length} characters`);

  const result = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: prNumber,
    body: sanitizeContent(message),
  });

  core.info(`  ✓ Comment created successfully with ID: ${result.data.id}`);
  core.info(`  Comment URL: ${result.data.html_url}`);

  return {
    id: result.data.id,
    html_url: result.data.html_url,
  };
}

/**
 * Close a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<{number: number, html_url: string}>} Pull request details
 */
async function closePullRequest(github, owner, repo, prNumber) {
  core.info(`Closing pull request #${prNumber} in ${owner}/${repo}`);

  const result = await github.rest.pulls.update({
    owner,
    repo,
    pull_number: prNumber,
    state: "closed",
  });

  core.info(`  ✓ Pull request #${result.data.number} closed successfully`);
  core.info(`  Pull request URL: ${result.data.html_url}`);

  return {
    number: result.data.number,
    html_url: result.data.html_url,
  };
}

/**
 * Generate closing message for older pull requests
 * @param {object} params - Parameters for the message
 * @param {string} params.newPullRequestUrl - URL of the new pull request
 * @param {number} params.newPullRequestNumber - Number of the new pull request
 * @param {string} params.workflowName - Name of the workflow
 * @param {string} params.runUrl - URL of the workflow run
 * @returns {string} Closing message
 */
function getCloseOlderPullRequestMessage({ newPullRequestUrl, newPullRequestNumber, workflowName, runUrl }) {
  return renderSizedReport({
    type: "report",
    title: "Superseded pull request",
    children: [
      {
        type: "paragraph",
        children: [
          { type: "text", value: "This pull request is being closed as superseded. A newer pull request has been created: ", agentTainted: false },
          { type: "issue_reference", number: String(newPullRequestNumber), agentTainted: false },
        ],
      },
      { type: "paragraph", children: [{ type: "link", url: newPullRequestUrl, children: [{ type: "text", value: "View newer pull request", agentTainted: false }] }] },
      { type: "separator" },
      {
        type: "paragraph",
        children: [
          {
            type: "emphasis",
            children: [
              { type: "text", value: "This action was performed automatically by the ", agentTainted: false },
              { type: "link", url: runUrl, children: [{ type: "code", value: workflowName, agentTainted: false }] },
              { type: "text", value: " workflow.", agentTainted: false },
            ],
          },
        ],
      },
    ],
  }).body;
}

/**
 * Close older pull requests that match the workflow-id marker
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {{number: number, html_url: string}} newPullRequest - The newly created pull request
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering
 * @param {string} [closeOlderKey] - Optional explicit deduplication key for close-older matching
 * @returns {Promise<Array<{number: number, html_url: string}>>} List of closed pull requests
 */
async function closeOlderPullRequests(github, owner, repo, workflowId, newPullRequest, workflowName, runUrl, callerWorkflowId, closeOlderKey) {
  return closeOlderWithDescriptor({
    github,
    owner,
    repo,
    workflowId,
    newEntity: newPullRequest,
    workflowName,
    runUrl,
    entityType: "pull request",
    entityTypePlural: "pull requests",
    searchOlderEntities: createCloseOlderSearchAdapter(searchOlderPullRequests, [], [callerWorkflowId, closeOlderKey]),
    getCloseMessage: params =>
      getCloseOlderPullRequestMessage({
        newPullRequestUrl: params.newEntityUrl,
        newPullRequestNumber: params.newEntityNumber,
        workflowName: params.workflowName,
        runUrl: params.runUrl,
      }),
    addComment: addPullRequestComment,
    closeEntity: closePullRequest,
    delayMs: API_DELAY_MS,
    getEntityId: entity => entity.number,
    getEntityUrl: entity => entity.html_url,
    mapClosedEntity: item => ({
      number: item.number,
      html_url: item.html_url || "",
    }),
  });
}

module.exports = {
  closeOlderPullRequests,
  searchOlderPullRequests,
  addPullRequestComment,
  closePullRequest,
  getCloseOlderPullRequestMessage,
  MAX_CLOSE_COUNT,
  API_DELAY_MS,
};
