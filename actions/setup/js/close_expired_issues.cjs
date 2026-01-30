// @ts-check
// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { extractExpirationDate } = require("./ephemerals.cjs");
const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");

/**
 * Maximum number of issues to update per run
 */
const MAX_UPDATES_PER_RUN = 100;

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
 * Validate issue creation date
 * @param {string} createdAt - ISO 8601 creation date
 * @returns {boolean} True if valid
 */
function validateCreationDate(createdAt) {
  const creationDate = new Date(createdAt);
  return !isNaN(creationDate.getTime());
}

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

  // Check which issues are expired
  const now = new Date();
  core.info(`Current date/time: ${now.toISOString()}`);
  const expiredIssues = [];
  const notExpiredIssues = [];

  for (const issue of issuesWithExpiration) {
    core.info(`Processing issue #${issue.number}: ${issue.title}`);

    // Validate creation date
    if (!validateCreationDate(issue.createdAt)) {
      core.warning(`  Issue #${issue.number} has invalid creation date: ${issue.createdAt}, skipping`);
      continue;
    }
    core.info(`  Creation date: ${issue.createdAt}`);

    // Extract and validate expiration date
    const expirationDate = extractExpirationDate(issue.body);
    if (!expirationDate) {
      core.warning(`  Issue #${issue.number} has invalid expiration date format, skipping`);
      continue;
    }
    core.info(`  Expiration date: ${expirationDate.toISOString()}`);

    // Check if expired
    const isExpired = now >= expirationDate;
    const timeDiff = expirationDate.getTime() - now.getTime();
    const daysUntilExpiration = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
    const hoursUntilExpiration = Math.floor(timeDiff / (1000 * 60 * 60));

    if (isExpired) {
      const daysSinceExpiration = Math.abs(daysUntilExpiration);
      const hoursSinceExpiration = Math.abs(hoursUntilExpiration);
      core.info(`  ✓ Issue #${issue.number} is EXPIRED (expired ${daysSinceExpiration} days, ${hoursSinceExpiration % 24} hours ago)`);
      expiredIssues.push({
        ...issue,
        expirationDate: expirationDate,
      });
    } else {
      core.info(`  ✗ Issue #${issue.number} is NOT expired (expires in ${daysUntilExpiration} days, ${hoursUntilExpiration % 24} hours)`);
      notExpiredIssues.push({
        ...issue,
        expirationDate: expirationDate,
      });
    }
  }

  core.info(`Expiration check complete: ${expiredIssues.length} expired, ${notExpiredIssues.length} not yet expired`);

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

  // Limit to MAX_UPDATES_PER_RUN
  const issuesToClose = expiredIssues.slice(0, MAX_UPDATES_PER_RUN);

  if (expiredIssues.length > MAX_UPDATES_PER_RUN) {
    core.warning(`Found ${expiredIssues.length} expired issues, but only closing the first ${MAX_UPDATES_PER_RUN}`);
    core.info(`Remaining ${expiredIssues.length - MAX_UPDATES_PER_RUN} expired issues will be closed in the next run`);
  }

  core.info(`Preparing to close ${issuesToClose.length} issue(s)`);

  let closedCount = 0;
  const closedIssues = [];
  const failedIssues = [];

  for (let i = 0; i < issuesToClose.length; i++) {
    const issue = issuesToClose[i];

    core.info(`[${i + 1}/${issuesToClose.length}] Processing issue #${issue.number}: ${issue.url}`);

    try {
      const closingMessage = `This issue was automatically closed because it expired on ${issue.expirationDate.toISOString()}.`;

      // Add comment first
      core.info(`  Adding closing comment to issue #${issue.number}`);
      await addIssueComment(github, owner, repo, issue.number, closingMessage);
      core.info(`  ✓ Comment added successfully`);

      // Then close the issue as not planned
      core.info(`  Closing issue #${issue.number} as not planned`);
      await closeIssue(github, owner, repo, issue.number);
      core.info(`  ✓ Issue closed successfully`);

      closedIssues.push({
        number: issue.number,
        url: issue.url,
        title: issue.title,
      });

      closedCount++;
      core.info(`✓ Successfully processed issue #${issue.number}: ${issue.url}`);
    } catch (error) {
      core.error(`✗ Failed to close issue #${issue.number}: ${getErrorMessage(error)}`);
      core.error(`  Error details: ${JSON.stringify(error, null, 2)}`);
      failedIssues.push({
        number: issue.number,
        url: issue.url,
        title: issue.title,
        error: getErrorMessage(error),
      });
      // Continue with other issues even if one fails
    }

    // Add delay between GraphQL operations to avoid rate limiting (except for the last item)
    if (i < issuesToClose.length - 1) {
      core.info(`  Waiting ${GRAPHQL_DELAY_MS}ms before next operation...`);
      await delay(GRAPHQL_DELAY_MS);
    }
  }

  // Write comprehensive summary
  let summaryContent = `## Expired Issues Cleanup\n\n`;
  summaryContent += `**Scan Summary**\n`;
  summaryContent += `- Scanned: ${searchStats.totalScanned} issues across ${searchStats.pageCount} page(s)\n`;
  summaryContent += `- With expiration markers: ${issuesWithExpiration.length} issue(s)\n`;
  summaryContent += `- Expired: ${expiredIssues.length} issue(s)\n`;
  summaryContent += `- Not yet expired: ${notExpiredIssues.length} issue(s)\n\n`;

  summaryContent += `**Closing Summary**\n`;
  summaryContent += `- Successfully closed: ${closedCount} issue(s)\n`;
  if (failedIssues.length > 0) {
    summaryContent += `- Failed to close: ${failedIssues.length} issue(s)\n`;
  }
  if (expiredIssues.length > MAX_UPDATES_PER_RUN) {
    summaryContent += `- Remaining for next run: ${expiredIssues.length - MAX_UPDATES_PER_RUN} issue(s)\n`;
  }
  summaryContent += `\n`;

  if (closedCount > 0) {
    summaryContent += `### Successfully Closed Issues\n\n`;
    for (const closed of closedIssues) {
      summaryContent += `- Issue #${closed.number}: [${closed.title}](${closed.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (failedIssues.length > 0) {
    summaryContent += `### Failed to Close\n\n`;
    for (const failed of failedIssues) {
      summaryContent += `- Issue #${failed.number}: [${failed.title}](${failed.url}) - Error: ${failed.error}\n`;
    }
    summaryContent += `\n`;
  }

  if (notExpiredIssues.length > 0 && notExpiredIssues.length <= 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    for (const notExpired of notExpiredIssues) {
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Issue #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  } else if (notExpiredIssues.length > 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    summaryContent += `${notExpiredIssues.length} issue(s) not yet expired (showing first 10):\n\n`;
    for (let i = 0; i < 10; i++) {
      const notExpired = notExpiredIssues[i];
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Issue #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  }

  await core.summary.addRaw(summaryContent).write();

  core.info(`Successfully closed ${closedCount} expired issue(s)`);
}

module.exports = { main };
