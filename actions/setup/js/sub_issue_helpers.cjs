// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { graphEffectIdentity } = require("./work_queue_effect_client.cjs");

// Maximum number of sub-issues per parent issue
const MAX_SUB_ISSUES = 64;

const ADD_SUB_ISSUE_MUTATION = `
      mutation AddSubIssue($parentId: ID!, $subIssueId: ID!) {
        addSubIssue(input: { issueId: $parentId, subIssueId: $subIssueId }) {
          issue {
            id
            number
          }
          subIssue {
            id
            number
          }
        }
      }
    `;

/**
 * Resolve the GitHub client to use for GraphQL requests
 * @param {any} [githubClient] - Optional authenticated client
 * @returns {any}
 */
function getGitHubClient(githubClient) {
  return githubClient || github;
}

/**
 * Gets the sub-issue count for a parent issue using GraphQL
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<number|null>} - Sub-issue count or null if query failed
 */
async function getSubIssueCount(owner, repo, issueNumber, githubClient = undefined) {
  try {
    const subIssueQuery = `
      query($owner: String!, $repo: String!, $issueNumber: Int!) {
        repository(owner: $owner, name: $repo) {
          issue(number: $issueNumber) {
            subIssues(first: ${MAX_SUB_ISSUES + 1}) {
              totalCount
            }
          }
        }
      }
    `;

    const result = await getGitHubClient(githubClient).graphql(subIssueQuery, {
      owner,
      repo,
      issueNumber,
    });

    return result?.repository?.issue?.subIssues?.totalCount || 0;
  } catch (error) {
    core.warning(`Could not check sub-issue count for #${issueNumber}: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Resolve the GraphQL node ID for an issue
 * @param {Object} params - Lookup parameters
 * @param {string} params.owner - Repository owner
 * @param {string} params.repo - Repository name
 * @param {number} params.issueNumber - Issue number to resolve
 * @param {any} [githubClient] - Optional authenticated client
 * @returns {Promise<string>} GraphQL node ID
 */
async function getIssueNodeId({ owner, repo, issueNumber }, githubClient = undefined) {
  const result = await getGitHubClient(githubClient).graphql(
    `
      query($owner: String!, $repo: String!, $issueNumber: Int!) {
        repository(owner: $owner, name: $repo) {
          issue(number: $issueNumber) {
            id
          }
        }
      }
    `,
    {
      owner,
      repo,
      issueNumber,
    }
  );

  return result.repository.issue.id;
}

/**
 * Link two issues with the addSubIssue mutation
 * @param {Object} params - Link parameters
 * @param {string} params.parentNodeId - GraphQL node ID of the parent issue
 * @param {string} params.subIssueNodeId - GraphQL node ID of the child issue
 * @param {any} [githubClient] - Optional authenticated client
 * @returns {Promise<{effectId: string}>}
 */
async function addSubIssue({ parentNodeId, subIssueNodeId }, githubClient = undefined) {
  const variables = { parentId: parentNodeId, subIssueId: subIssueNodeId };
  const response = await getGitHubClient(githubClient).graphql(ADD_SUB_ISSUE_MUTATION, variables);
  return { effectId: graphEffectIdentity(ADD_SUB_ISSUE_MUTATION, variables, response) };
}

/**
 * Resolve missing issue node IDs and link the child issue as a sub-issue
 * @param {Object} params - Link parameters
 * @param {string} params.owner - Repository owner
 * @param {string} params.repo - Repository name
 * @param {number} params.parentIssueNumber - Parent issue number
 * @param {number} params.subIssueNumber - Child issue number
 * @param {string} [params.parentNodeId] - Optional parent GraphQL node ID
 * @param {string} [params.subIssueNodeId] - Optional child GraphQL node ID
 * @param {any} [githubClient] - Optional authenticated client
 * @returns {Promise<{parentNodeId: string, subIssueNodeId: string, effectId: string}>}
 */
async function linkSubIssue({ owner, repo, parentIssueNumber, subIssueNumber, parentNodeId, subIssueNodeId }, githubClient = undefined) {
  const resolvedParentNodeId = parentNodeId || (await getIssueNodeId({ owner, repo, issueNumber: parentIssueNumber }, githubClient));
  const resolvedSubIssueNodeId = subIssueNodeId || (await getIssueNodeId({ owner, repo, issueNumber: subIssueNumber }, githubClient));

  const { effectId } = await addSubIssue(
    {
      parentNodeId: resolvedParentNodeId,
      subIssueNodeId: resolvedSubIssueNodeId,
    },
    githubClient
  );

  return {
    parentNodeId: resolvedParentNodeId,
    subIssueNodeId: resolvedSubIssueNodeId,
    effectId,
  };
}

module.exports = {
  MAX_SUB_ISSUES,
  getSubIssueCount,
  getIssueNodeId,
  addSubIssue,
  linkSubIssue,
};
