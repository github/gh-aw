// @ts-check
// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { extractExpirationDate } = require("./ephemerals.cjs");
const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");

/**
 * Maximum number of discussions to update per run
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
 * Validate discussion creation date
 * @param {string} createdAt - ISO 8601 creation date
 * @returns {boolean} True if valid
 */
function validateCreationDate(createdAt) {
  const creationDate = new Date(createdAt);
  return !isNaN(creationDate.getTime());
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
 * Check if a discussion already has an expiration comment and fetch its closed state
 * @param {any} github - GitHub GraphQL instance
 * @param {string} discussionId - Discussion node ID
 * @returns {Promise<{hasComment: boolean, isClosed: boolean}>} Object with comment existence and closed state
 */
async function hasExpirationComment(github, discussionId) {
  const result = await github.graphql(
    `
    query($dId: ID!) {
      node(id: $dId) {
        ... on Discussion {
          closed
          comments(first: 100) {
            nodes {
              body
            }
          }
        }
      }
    }`,
    { dId: discussionId }
  );

  if (!result || !result.node) {
    return { hasComment: false, isClosed: false };
  }

  const isClosed = result.node.closed || false;
  const comments = result.node.comments?.nodes || [];
  const expirationCommentPattern = /<!--\s*gh-aw-closed\s*-->/;
  const hasComment = comments.some(comment => comment.body && expirationCommentPattern.test(comment.body));

  return { hasComment, isClosed };
}

async function main() {
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  core.info(`Searching for expired discussions in ${owner}/${repo}`);

  // Search for discussions with expiration markers (enable dedupe for discussions)
  const { items: discussionsWithExpiration, stats: searchStats } = await searchEntitiesWithExpiration(github, owner, repo, {
    entityType: "discussions",
    graphqlField: "discussions",
    resultKey: "discussions",
    enableDedupe: true, // Discussions may have duplicates across pages
  });

  if (discussionsWithExpiration.length === 0) {
    core.info("No discussions with expiration markers found");

    // Write summary even when no discussions found
    let summaryContent = `## Expired Discussions Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} discussions across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**Result**: No discussions with expiration markers found\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${discussionsWithExpiration.length} discussion(s) with expiration markers`);

  // Check which discussions are expired
  const now = new Date();
  core.info(`Current date/time: ${now.toISOString()}`);
  const expiredDiscussions = [];
  const notExpiredDiscussions = [];

  for (const discussion of discussionsWithExpiration) {
    core.info(`Processing discussion #${discussion.number}: ${discussion.title}`);

    // Validate creation date
    if (!validateCreationDate(discussion.createdAt)) {
      core.warning(`  Discussion #${discussion.number} has invalid creation date: ${discussion.createdAt}, skipping`);
      continue;
    }
    core.info(`  Creation date: ${discussion.createdAt}`);

    // Extract and validate expiration date
    const expirationDate = extractExpirationDate(discussion.body);
    if (!expirationDate) {
      core.warning(`  Discussion #${discussion.number} has invalid expiration date format, skipping`);
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
      core.info(`  ✓ Discussion #${discussion.number} is EXPIRED (expired ${daysSinceExpiration} days, ${hoursSinceExpiration % 24} hours ago)`);
      expiredDiscussions.push({
        ...discussion,
        expirationDate: expirationDate,
      });
    } else {
      core.info(`  ✗ Discussion #${discussion.number} is NOT expired (expires in ${daysUntilExpiration} days, ${hoursUntilExpiration % 24} hours)`);
      notExpiredDiscussions.push({
        ...discussion,
        expirationDate: expirationDate,
      });
    }
  }

  core.info(`Expiration check complete: ${expiredDiscussions.length} expired, ${notExpiredDiscussions.length} not yet expired`);

  if (expiredDiscussions.length === 0) {
    core.info("No expired discussions found");

    // Write summary when no expired discussions
    let summaryContent = `## Expired Discussions Cleanup\n\n`;
    summaryContent += `**Scanned**: ${searchStats.totalScanned} discussions across ${searchStats.pageCount} page(s)\n\n`;
    summaryContent += `**With expiration markers**: ${discussionsWithExpiration.length} discussion(s)\n\n`;
    summaryContent += `**Expired**: 0 discussions\n\n`;
    summaryContent += `**Not yet expired**: ${notExpiredDiscussions.length} discussion(s)\n`;
    await core.summary.addRaw(summaryContent).write();

    return;
  }

  core.info(`Found ${expiredDiscussions.length} expired discussion(s)`);

  // Limit to MAX_UPDATES_PER_RUN
  const discussionsToClose = expiredDiscussions.slice(0, MAX_UPDATES_PER_RUN);

  if (expiredDiscussions.length > MAX_UPDATES_PER_RUN) {
    core.warning(`Found ${expiredDiscussions.length} expired discussions, but only closing the first ${MAX_UPDATES_PER_RUN}`);
    core.info(`Remaining ${expiredDiscussions.length - MAX_UPDATES_PER_RUN} expired discussions will be closed in the next run`);
  }

  core.info(`Preparing to close ${discussionsToClose.length} discussion(s)`);

  let closedCount = 0;
  const closedDiscussions = [];
  const failedDiscussions = [];

  let skippedCount = 0;
  const skippedDiscussions = [];

  for (let i = 0; i < discussionsToClose.length; i++) {
    const discussion = discussionsToClose[i];

    core.info(`[${i + 1}/${discussionsToClose.length}] Processing discussion #${discussion.number}: ${discussion.url}`);

    try {
      // Check if an expiration comment already exists and if discussion is closed
      core.info(`  Checking for existing expiration comment and closed state on discussion #${discussion.number}`);
      const { hasComment, isClosed } = await hasExpirationComment(github, discussion.id);

      if (isClosed) {
        core.warning(`  Discussion #${discussion.number} is already closed, skipping`);
        skippedDiscussions.push({
          number: discussion.number,
          url: discussion.url,
          title: discussion.title,
        });
        skippedCount++;
        continue;
      }

      if (hasComment) {
        core.warning(`  Discussion #${discussion.number} already has an expiration comment, skipping to avoid duplicate`);
        skippedDiscussions.push({
          number: discussion.number,
          url: discussion.url,
          title: discussion.title,
        });
        skippedCount++;

        // Still try to close it if it's somehow still open
        core.info(`  Attempting to close discussion #${discussion.number} without adding another comment`);
        await closeDiscussionAsOutdated(github, discussion.id);
        core.info(`  ✓ Discussion closed successfully`);

        closedDiscussions.push({
          number: discussion.number,
          url: discussion.url,
          title: discussion.title,
        });
        closedCount++;
      } else {
        const closingMessage = `This discussion was automatically closed because it expired on ${discussion.expirationDate.toISOString()}.\n\n<!-- gh-aw-closed -->`;

        // Add comment first
        core.info(`  Adding closing comment to discussion #${discussion.number}`);
        await addDiscussionComment(github, discussion.id, closingMessage);
        core.info(`  ✓ Comment added successfully`);

        // Then close the discussion as outdated
        core.info(`  Closing discussion #${discussion.number} as outdated`);
        await closeDiscussionAsOutdated(github, discussion.id);
        core.info(`  ✓ Discussion closed successfully`);

        closedDiscussions.push({
          number: discussion.number,
          url: discussion.url,
          title: discussion.title,
        });

        closedCount++;
      }

      core.info(`✓ Successfully processed discussion #${discussion.number}: ${discussion.url}`);
    } catch (error) {
      core.error(`✗ Failed to close discussion #${discussion.number}: ${getErrorMessage(error)}`);
      core.error(`  Error details: ${JSON.stringify(error, null, 2)}`);
      failedDiscussions.push({
        number: discussion.number,
        url: discussion.url,
        title: discussion.title,
        error: getErrorMessage(error),
      });
      // Continue with other discussions even if one fails
    }

    // Add delay between GraphQL operations to avoid rate limiting (except for the last item)
    if (i < discussionsToClose.length - 1) {
      core.info(`  Waiting ${GRAPHQL_DELAY_MS}ms before next operation...`);
      await delay(GRAPHQL_DELAY_MS);
    }
  }

  // Write comprehensive summary
  let summaryContent = `## Expired Discussions Cleanup\n\n`;
  summaryContent += `**Scan Summary**\n`;
  summaryContent += `- Scanned: ${searchStats.totalScanned} discussions across ${searchStats.pageCount} page(s)\n`;
  summaryContent += `- With expiration markers: ${discussionsWithExpiration.length} discussion(s)\n`;
  summaryContent += `- Expired: ${expiredDiscussions.length} discussion(s)\n`;
  summaryContent += `- Not yet expired: ${notExpiredDiscussions.length} discussion(s)\n\n`;

  summaryContent += `**Closing Summary**\n`;
  summaryContent += `- Successfully closed: ${closedCount} discussion(s)\n`;
  if (skippedCount > 0) {
    summaryContent += `- Skipped (already had comment): ${skippedCount} discussion(s)\n`;
  }
  if (failedDiscussions.length > 0) {
    summaryContent += `- Failed to close: ${failedDiscussions.length} discussion(s)\n`;
  }
  if (expiredDiscussions.length > MAX_UPDATES_PER_RUN) {
    summaryContent += `- Remaining for next run: ${expiredDiscussions.length - MAX_UPDATES_PER_RUN} discussion(s)\n`;
  }
  summaryContent += `\n`;

  if (closedCount > 0) {
    summaryContent += `### Successfully Closed Discussions\n\n`;
    for (const closed of closedDiscussions) {
      summaryContent += `- Discussion #${closed.number}: [${closed.title}](${closed.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (skippedCount > 0) {
    summaryContent += `### Skipped (Already Had Comment)\n\n`;
    for (const skipped of skippedDiscussions) {
      summaryContent += `- Discussion #${skipped.number}: [${skipped.title}](${skipped.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (failedDiscussions.length > 0) {
    summaryContent += `### Failed to Close\n\n`;
    for (const failed of failedDiscussions) {
      summaryContent += `- Discussion #${failed.number}: [${failed.title}](${failed.url}) - Error: ${failed.error}\n`;
    }
    summaryContent += `\n`;
  }

  if (notExpiredDiscussions.length > 0 && notExpiredDiscussions.length <= 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    for (const notExpired of notExpiredDiscussions) {
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Discussion #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  } else if (notExpiredDiscussions.length > 10) {
    summaryContent += `### Not Yet Expired\n\n`;
    summaryContent += `${notExpiredDiscussions.length} discussion(s) not yet expired (showing first 10):\n\n`;
    for (let i = 0; i < 10; i++) {
      const notExpired = notExpiredDiscussions[i];
      const timeDiff = notExpired.expirationDate.getTime() - now.getTime();
      const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
      const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
      summaryContent += `- Discussion #${notExpired.number}: [${notExpired.title}](${notExpired.url}) - Expires in ${days}d ${hours}h\n`;
    }
  }

  await core.summary.addRaw(summaryContent).write();

  core.info(`Successfully closed ${closedCount} expired discussion(s)`);
}

module.exports = { main };
