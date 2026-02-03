// @ts-check
// <reference types="@actions/github-script" />

const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");
const { buildExpirationSummary, categorizeByExpiration, DEFAULT_GRAPHQL_DELAY_MS, DEFAULT_MAX_UPDATES_PER_RUN, processExpiredEntities } = require("./expired_entity_cleanup_helpers.cjs");

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

  const {
    expired: expiredDiscussions,
    notExpired: notExpiredDiscussions,
    now,
  } = categorizeByExpiration(discussionsWithExpiration, {
    entityLabel: "Discussion",
  });

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

  const { closed, skipped, failed } = await processExpiredEntities(expiredDiscussions, {
    entityLabel: "Discussion",
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    delayMs: DEFAULT_GRAPHQL_DELAY_MS,
    processEntity: async discussion => {
      core.info(`  Checking for existing expiration comment and closed state on discussion #${discussion.number}`);
      const { hasComment, isClosed } = await hasExpirationComment(github, discussion.id);

      if (isClosed) {
        core.warning(`  Discussion #${discussion.number} is already closed, skipping`);
        return {
          status: "skipped",
          record: {
            number: discussion.number,
            url: discussion.url,
            title: discussion.title,
          },
        };
      }

      if (hasComment) {
        core.warning(`  Discussion #${discussion.number} already has an expiration comment, skipping to avoid duplicate`);

        core.info(`  Attempting to close discussion #${discussion.number} without adding another comment`);
        await closeDiscussionAsOutdated(github, discussion.id);
        core.info(`  ✓ Discussion closed successfully`);

        return {
          status: "skipped",
          record: {
            number: discussion.number,
            url: discussion.url,
            title: discussion.title,
          },
        };
      }

      const closingMessage = `This discussion was automatically closed because it expired on ${discussion.expirationDate.toISOString()}.\n\n<!-- gh-aw-closed -->`;

      core.info(`  Adding closing comment to discussion #${discussion.number}`);
      await addDiscussionComment(github, discussion.id, closingMessage);
      core.info(`  ✓ Comment added successfully`);

      core.info(`  Closing discussion #${discussion.number} as outdated`);
      await closeDiscussionAsOutdated(github, discussion.id);
      core.info(`  ✓ Discussion closed successfully`);

      return {
        status: "closed",
        record: {
          number: discussion.number,
          url: discussion.url,
          title: discussion.title,
        },
      };
    },
  });

  const summaryContent = buildExpirationSummary({
    heading: "Expired Discussions Cleanup",
    entityLabel: "Discussion",
    searchStats,
    withExpirationCount: discussionsWithExpiration.length,
    expired: expiredDiscussions,
    notExpired: notExpiredDiscussions,
    closed,
    skipped,
    failed,
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    includeSkippedHeading: true,
    now,
  });

  await core.summary.addRaw(summaryContent).write();
  core.info(`Successfully closed ${closed.length} expired discussion(s)`);
}

module.exports = { main };
