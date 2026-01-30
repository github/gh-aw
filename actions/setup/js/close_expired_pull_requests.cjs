// @ts-check
// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { extractExpirationDate } = require("./ephemerals.cjs");
const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");

/**
 * Maximum number of pull requests to update per run
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
 * Validate pull request creation date
 * @param {string} createdAt - ISO 8601 creation date
 * @returns {boolean} True if valid
 */
function validateCreationDate(createdAt) {
  const creationDate = new Date(createdAt);
  return !isNaN(creationDate.getTime());
}

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

  // Check which pull requests are expired
  const now = new Date();
  core.info(`Current date/time: ${now.toISOString()}`);
  const expiredPullRequests = [];
  const notExpiredPullRequests = [];

  for (const pr of pullRequestsWithExpiration) {
    core.info(`Processing pull request #${pr.number}: ${pr.title}`);

    // Validate creation date
    if (!validateCreationDate(pr.createdAt)) {
      core.warning(`  Pull request #${pr.number} has invalid creation date: ${pr.createdAt}, skipping`);
      continue;
    }
    core.info(`  Creation date: ${pr.createdAt}`);

    // Extract and validate expiration date
    const expirationDate = extractExpirationDate(pr.body);
    if (!expirationDate) {
      core.warning(`  Pull request #${pr.number} has invalid expiration date format, skipping`);
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
      core.info(`  ✓ Pull request #${pr.number} is EXPIRED (expired ${daysSinceExpiration} days, ${hoursSinceExpiration % 24} hours ago)`);
      expiredPullRequests.push({
        ...pr,
        expirationDate: expirationDate,
      });
    } else {
      core.info(`  ✗ Pull request #${pr.number} is NOT expired (expires in ${daysUntilExpiration} days, ${hoursUntilExpiration % 24} hours)`);
      notExpiredPullRequests.push({
        ...pr,
        expirationDate: expirationDate,
      });
    }
  }

  core.info(`Expiration check complete: ${expiredPullRequests.length} expired, ${notExpiredPullRequests.length} not yet expired`);

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

  // Limit to MAX_UPDATES_PER_RUN
  const pullRequestsToClose = expiredPullRequests.slice(0, MAX_UPDATES_PER_RUN);

  if (expiredPullRequests.length > MAX_UPDATES_PER_RUN) {
    core.warning(`Found ${expiredPullRequests.length} expired pull requests, but only closing the first ${MAX_UPDATES_PER_RUN}`);
    core.info(`Remaining ${expiredPullRequests.length - MAX_UPDATES_PER_RUN} expired pull requests will be closed in the next run`);
  }

  core.info(`Preparing to close ${pullRequestsToClose.length} pull request(s)`);

  let closedCount = 0;
  const closedPullRequests = [];
  const failedPullRequests = [];

  for (let i = 0; i < pullRequestsToClose.length; i++) {
    const pr = pullRequestsToClose[i];

    core.info(`[${i + 1}/${pullRequestsToClose.length}] Processing pull request #${pr.number}: ${pr.url}`);

    try {
      const closingMessage = `This pull request was automatically closed because it expired on ${pr.expirationDate.toISOString()}.`;

      // Add comment first
      core.info(`  Adding closing comment to pull request #${pr.number}`);
      await addPullRequestComment(github, owner, repo, pr.number, closingMessage);
      core.info(`  ✓ Comment added successfully`);

      // Then close the pull request
      core.info(`  Closing pull request #${pr.number}`);
      await closePullRequest(github, owner, repo, pr.number);
      core.info(`  ✓ Pull request closed successfully`);

      closedPullRequests.push({
        number: pr.number,
        url: pr.url,
        title: pr.title,
      });

      closedCount++;
      core.info(`✓ Successfully processed pull request #${pr.number}: ${pr.url}`);
    } catch (error) {
      core.error(`✗ Failed to close pull request #${pr.number}: ${getErrorMessage(error)}`);
      core.error(`  Error details: ${JSON.stringify(error, null, 2)}`);
      failedPullRequests.push({
        number: pr.number,
        url: pr.url,
        title: pr.title,
        error: getErrorMessage(error),
      });
      // Continue with other pull requests even if one fails
    }

    // Add delay between GraphQL operations to avoid rate limiting (except for the last item)
    if (i < pullRequestsToClose.length - 1) {
      core.info(`  Waiting ${GRAPHQL_DELAY_MS}ms before next operation...`);
      await delay(GRAPHQL_DELAY_MS);
    }
  }

  // Write comprehensive summary
  let summaryContent = `## Expired Pull Requests Cleanup\n\n`;
  summaryContent += `**Scan Summary**\n`;
  summaryContent += `- Scanned: ${searchStats.totalScanned} pull requests across ${searchStats.pageCount} page(s)\n`;
  summaryContent += `- With expiration markers: ${pullRequestsWithExpiration.length} pull request(s)\n`;
  summaryContent += `- Expired: ${expiredPullRequests.length} pull request(s)\n`;
  summaryContent += `- Not yet expired: ${notExpiredPullRequests.length} pull request(s)\n\n`;

  summaryContent += `**Closing Summary**\n`;
  summaryContent += `- Successfully closed: ${closedCount} pull request(s)\n`;
  if (failedPullRequests.length > 0) {
    summaryContent += `- Failed to close: ${failedPullRequests.length} pull request(s)\n`;
  }
  if (expiredPullRequests.length > MAX_UPDATES_PER_RUN) {
    summaryContent += `- Remaining for next run: ${expiredPullRequests.length - MAX_UPDATES_PER_RUN} pull request(s)\n`;
  }
  summaryContent += `\n`;

  if (closedCount > 0) {
    summaryContent += `### Successfully Closed Pull Requests\n\n`;
    for (const closed of closedPullRequests) {
      summaryContent += `- Pull Request #${closed.number}: [${closed.title}](${closed.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (failedPullRequests.length > 0) {
    summaryContent += `### Failed to Close\n\n`;
    for (const failed of failedPullRequests) {
      summaryContent += `- Pull Request #${failed.number}: [${failed.title}](${failed.url}) - Error: ${failed.error}\n`;
    }
    summaryContent += `\n`;
  }

  if (notExpiredPullRequests.length > 0 && notExpiredPullRequests.length <= 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    for (const notExpired of notExpiredPullRequests) {
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Pull Request #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  } else if (notExpiredPullRequests.length > 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    summaryContent += `${notExpiredPullRequests.length} pull request(s) not yet expired (showing first 10):\n\n`;
    for (let i = 0; i < 10; i++) {
      const notExpired = notExpiredPullRequests[i];
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Pull Request #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  }

  await core.summary.addRaw(summaryContent).write();

  core.info(`Successfully closed ${closedCount} expired pull request(s)`);
}

module.exports = { main };
