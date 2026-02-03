// @ts-check
// <reference types="@actions/github-script" />

const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");
const { buildExpirationSummary, categorizeByExpiration, DEFAULT_GRAPHQL_DELAY_MS, DEFAULT_MAX_UPDATES_PER_RUN, processExpiredEntities } = require("./expired_entity_cleanup_helpers.cjs");

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

  core.info(`Searching for expired pull requests in ${owner}/${repo}`);

  // Search for pull requests with expiration markers
  const { items: pullRequestsWithExpiration, stats: searchStats } = await searchEntitiesWithExpiration(github, owner, repo, {
    entityType: "pull requests",
    graphqlField: "pullRequests",
    resultKey: "pullRequests",
  });

  if (pullRequestsWithExpiration.length === 0) {
    core.info("No pull requests with expiration markers found");

    // Write summary even when no pull requests found
    let summaryContent = `## Expired Pull Requests Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} pull requests across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**Result**: No pull requests with expiration markers found\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${pullRequestsWithExpiration.length} pull request(s) with expiration markers`);

  const {
    expired: expiredPullRequests,
    notExpired: notExpiredPullRequests,
    now,
  } = categorizeByExpiration(pullRequestsWithExpiration, {
    entityLabel: "Pull Request",
  });

  if (expiredPullRequests.length === 0) {
    core.info("No expired pull requests found");

    // Write summary when no expired pull requests
    let summaryContent = `## Expired Pull Requests Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} pull requests across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**With expiration markers**: ${pullRequestsWithExpiration.length} pull request(s)\n\n`;
    summaryContent += `**Expired**: 0 pull requests\n\n`;
    summaryContent += `**Not yet expired**: ${notExpiredPullRequests.length} pull request(s)\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${expiredPullRequests.length} expired pull request(s)`);

  const { closed, failed } = await processExpiredEntities(expiredPullRequests, {
    entityLabel: "Pull Request",
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    delayMs: DEFAULT_GRAPHQL_DELAY_MS,
    processEntity: async pr => {
      const closingMessage = `This pull request was automatically closed because it expired on ${pr.expirationDate.toISOString()}.`;

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

  const summaryContent = buildExpirationSummary({
    heading: "Expired Pull Requests Cleanup",
    entityLabel: "Pull Request",
    searchStats,
    withExpirationCount: pullRequestsWithExpiration.length,
    expired: expiredPullRequests,
    notExpired: notExpiredPullRequests,
    closed,
    failed,
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    now,
  });

  await core.summary.addRaw(summaryContent).write();
  core.info(`Successfully closed ${closed.length} expired pull request(s)`);
}

module.exports = { main };
