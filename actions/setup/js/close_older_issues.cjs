// @ts-check
/// <reference types="@actions/github-script" />

const { sanitizeContent } = require("./sanitize_content.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { MAX_CLOSE_COUNT: SHARED_MAX_CLOSE_COUNT } = require("./close_older_entities.cjs");
const { searchOlderEntitiesByMarker } = require("./close_older_search_helpers.cjs");
const { createCloseOlderSearchAdapter, closeOlderWithDescriptor } = require("./close_older_handler_factory.cjs");

/**
 * Maximum number of older issues to close
 */
const MAX_CLOSE_COUNT = SHARED_MAX_CLOSE_COUNT;

/**
 * Delay between API calls in milliseconds to avoid rate limiting
 */
const API_DELAY_MS = 500;

/**
 * Search for open issues with a matching workflow-id marker
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {number} excludeNumber - Issue number to exclude (the newly created one)
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering.
 *   When set, filters by the `gh-aw-workflow-call-id` marker so callers sharing the same
 *   reusable workflow do not close each other's issues. Falls back to `gh-aw-workflow-id`
 *   when not provided (backward compat for issues created before this fix).
 * @param {string} [closeOlderKey] - Optional explicit deduplication key. When set, the
 *   `gh-aw-close-key` marker is used as the primary search term and exact filter instead
 *   of the workflow-id / workflow-call-id markers.
 * @param {Set<number>} [additionalExcludeNumbers] - Optional set of additional issue numbers
 *   to exclude from the results (e.g. all issues created in the current run).
 * @returns {Promise<Array<{number: number, title: string, html_url: string, labels: Array<{name: string}>, created_at: string}>>} Matching issues
 */
async function searchOlderIssues(github, owner, repo, workflowId, excludeNumber, callerWorkflowId, closeOlderKey, additionalExcludeNumbers) {
  return searchOlderEntitiesByMarker({
    owner,
    repo,
    workflowId,
    excludeNumber,
    entityType: "issue",
    callerWorkflowId,
    closeOlderKey,
    additionalExcludeNumbers,
    entityQualifier: "is:issue",
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
      if (item.pull_request) {
        extra.pullRequestCount = (extra.pullRequestCount || 0) + 1;
        return false;
      }
      return true;
    },
    extraLabels: [["pullRequestCount", "Excluded pull requests"]],
  });
}

/**
 * Add comment to a GitHub Issue using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addIssueComment(github, owner, repo, issueNumber, message) {
  core.info(`Adding comment to issue #${issueNumber} in ${owner}/${repo}`);
  core.info(`  Comment length: ${message.length} characters`);

  const result = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
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
 * Close a GitHub Issue as "not planned" using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<{number: number, html_url: string}>} Issue details
 */
async function closeIssueAsNotPlanned(github, owner, repo, issueNumber) {
  core.info(`Closing issue #${issueNumber} in ${owner}/${repo} as "not planned"`);

  const result = await github.rest.issues.update({
    owner,
    repo,
    issue_number: issueNumber,
    state: "closed",
    state_reason: "not_planned",
  });

  core.info(`  ✓ Issue #${result.data.number} closed successfully`);
  core.info(`  Issue URL: ${result.data.html_url}`);

  return {
    number: result.data.number,
    html_url: result.data.html_url,
  };
}

/**
 * Generate closing message for older issues
 * @param {object} params - Parameters for the message
 * @param {string} params.newIssueUrl - URL of the new issue
 * @param {number} params.newIssueNumber - Number of the new issue
 * @param {string} params.workflowName - Name of the workflow
 * @param {string} params.runUrl - URL of the workflow run
 * @returns {string} Closing message
 */
function getCloseOlderIssueMessage({ newIssueUrl, newIssueNumber, workflowName, runUrl }) {
  return renderSizedReport({
    type: "report",
    title: "Outdated issue",
    children: [
      {
        type: "paragraph",
        children: [
          { type: "text", value: "This issue is being closed as outdated. A newer issue has been created: ", agentTainted: false },
          { type: "issue_reference", number: String(newIssueNumber), agentTainted: false },
        ],
      },
      { type: "paragraph", children: [{ type: "link", url: newIssueUrl, children: [{ type: "text", value: "View newer issue", agentTainted: false }] }] },
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
  }).body.trimEnd();
}

/**
 * Close older issues that match the workflow-id marker
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {{number: number, html_url: string}} newIssue - The newly created issue
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {string} [callerWorkflowId] - Optional calling workflow identity for precise filtering
 * @param {string} [closeOlderKey] - Optional explicit deduplication key for close-older matching
 * @param {Set<number>} [currentRunIssueNumbers] - Optional set of issue numbers created in the
 *   current run. When provided, these issues are excluded from the close-older search so that
 *   issues created earlier in the same run are never incorrectly closed.
 * @returns {Promise<Array<{number: number, html_url: string}>>} List of closed issues
 */
async function closeOlderIssues(github, owner, repo, workflowId, newIssue, workflowName, runUrl, callerWorkflowId, closeOlderKey, currentRunIssueNumbers) {
  return closeOlderWithDescriptor({
    github,
    owner,
    repo,
    workflowId,
    newEntity: newIssue,
    workflowName,
    runUrl,
    entityType: "issue",
    entityTypePlural: "issues",
    searchOlderEntities: createCloseOlderSearchAdapter(searchOlderIssues, [], [callerWorkflowId, closeOlderKey, currentRunIssueNumbers]),
    getCloseMessage: params =>
      getCloseOlderIssueMessage({
        newIssueUrl: params.newEntityUrl,
        newIssueNumber: params.newEntityNumber,
        workflowName: params.workflowName,
        runUrl: params.runUrl,
      }),
    addComment: addIssueComment,
    closeEntity: closeIssueAsNotPlanned,
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
  closeOlderIssues,
  searchOlderIssues,
  addIssueComment,
  closeIssueAsNotPlanned,
  getCloseOlderIssueMessage,
  MAX_CLOSE_COUNT,
  API_DELAY_MS,
};
