// @ts-check
// <reference types="@actions/github-script" />

const { executeExpiredEntityCleanup } = require("./expired_entity_main_flow.cjs");
const { generateExpiredEntityFooter } = require("./generate_footer.cjs");

/**
 * Add comment to a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @param {string} message - Comment body
 * @returns {Promise<any>} Comment details
 */
async function addPullRequestComment(github, owner, repo, prNumber, message) {
  const result = await github.rest.issues.createComment({
    owner: owner,
    repo: repo,
    issue_number: prNumber,
    body: message,
  });

  return result.data;
}

/**
 * Close a GitHub Pull Request using REST API
 * @param {any} github - GitHub REST instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} prNumber - Pull request number
 * @returns {Promise<any>} Pull request details
 */
async function closePullRequest(github, owner, repo, prNumber) {
  const result = await github.rest.pulls.update({
    owner: owner,
    repo: repo,
    pull_number: prNumber,
    state: "closed",
  });

  return result.data;
}

async function main() {
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  // Get workflow metadata for footer
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
  const runId = context.runId || 0;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload?.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${owner}/${repo}/actions/runs/${runId}`;

  await executeExpiredEntityCleanup(github, owner, repo, {
    entityType: "pull requests",
    graphqlField: "pullRequests",
    resultKey: "pullRequests",
    entityLabel: "Pull Request",
    summaryHeading: "Expired Pull Requests Cleanup",
    processEntity: async pr => {
      const closingMessage = `This pull request was automatically closed because it expired on ${pr.expirationDate.toISOString()}.` + generateExpiredEntityFooter(workflowName, runUrl, workflowId);

      await addPullRequestComment(github, owner, repo, pr.number, closingMessage);
      core.info(`  ✓ Comment added successfully`);

      await closePullRequest(github, owner, repo, pr.number);
      core.info(`  ✓ Pull request closed successfully`);

      return {
        status: "closed",
        record: {
          number: pr.number,
          url: pr.url,
          title: pr.title,
        },
      };
    },
  });
}

module.exports = { main };
