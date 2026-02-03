// @ts-check
// <reference types="@actions/github-script" />

const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");
const { buildExpirationSummary, categorizeByExpiration, DEFAULT_GRAPHQL_DELAY_MS, DEFAULT_MAX_UPDATES_PER_RUN, processExpiredEntities } = require("./expired_entity_cleanup_helpers.cjs");

/**
 * Add comment to a GitHub Issue using REST API
 * @param {any} github - GitHub REST instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} message - Comment body
 * @returns {Promise<any>} Comment details
 */
async function addIssueComment(github, owner, repo, issueNumber, message) {
  const result = await github.rest.issues.createComment({
    owner: owner,
    repo: repo,
    issue_number: issueNumber,
    body: message,
  });

  return result.data;
}

/**
 * Close a GitHub Issue using REST API
 * @param {any} github - GitHub REST instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<any>} Issue details
 */
async function closeIssue(github, owner, repo, issueNumber) {
  const result = await github.rest.issues.update({
    owner: owner,
    repo: repo,
    issue_number: issueNumber,
    state: "closed",
    state_reason: "not_planned",
  });

  return result.data;
}

async function main() {
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  core.info(`Searching for expired issues in ${owner}/${repo}`);

  // Search for issues with expiration markers
  const { items: issuesWithExpiration, stats: searchStats } = await searchEntitiesWithExpiration(github, owner, repo, {
    entityType: "issues",
    graphqlField: "issues",
    resultKey: "issues",
  });

  if (issuesWithExpiration.length === 0) {
    core.info("No issues with expiration markers found");

    // Write summary even when no issues found
    let summaryContent = `## Expired Issues Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} issues across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**Result**: No issues with expiration markers found\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${issuesWithExpiration.length} issue(s) with expiration markers`);

  const {
    expired: expiredIssues,
    notExpired: notExpiredIssues,
    now,
  } = categorizeByExpiration(issuesWithExpiration, {
    entityLabel: "Issue",
  });

  if (expiredIssues.length === 0) {
    core.info("No expired issues found");

    // Write summary when no expired issues
    let summaryContent = `## Expired Issues Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} issues across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**With expiration markers**: ${issuesWithExpiration.length} issue(s)\n\n`;
    summaryContent += `**Expired**: 0 issues\n\n`;
    summaryContent += `**Not yet expired**: ${notExpiredIssues.length} issue(s)\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${expiredIssues.length} expired issue(s)`);

  const { closed, failed } = await processExpiredEntities(expiredIssues, {
    entityLabel: "Issue",
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    delayMs: DEFAULT_GRAPHQL_DELAY_MS,
    processEntity: async issue => {
      const closingMessage = `This issue was automatically closed because it expired on ${issue.expirationDate.toISOString()}.`;

      await addIssueComment(github, owner, repo, issue.number, closingMessage);
      core.info(`  ✓ Comment added successfully`);

      await closeIssue(github, owner, repo, issue.number);
      core.info(`  ✓ Issue closed successfully`);

      return {
        status: "closed",
        record: {
          number: issue.number,
          url: issue.url,
          title: issue.title,
        },
      };
    },
  });

  const summaryContent = buildExpirationSummary({
    heading: "Expired Issues Cleanup",
    entityLabel: "Issue",
    searchStats,
    withExpirationCount: issuesWithExpiration.length,
    expired: expiredIssues,
    notExpired: notExpiredIssues,
    closed,
    failed,
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    now,
  });

  await core.summary.addRaw(summaryContent).write();
  core.info(`Successfully closed ${closed.length} expired issue(s)`);
}

module.exports = { main };
