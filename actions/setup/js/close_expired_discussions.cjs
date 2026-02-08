// @ts-check
// <reference types="@actions/github-script" />

const { executeExpiredEntityCleanup } = require("./expired_entity_main_flow.cjs");
const { generateExpiredEntityFooter } = require("./generate_footer.cjs");

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

  // Get workflow metadata for footer
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
  const runId = context.runId || 0;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  const runUrl = context.payload?.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${owner}/${repo}/actions/runs/${runId}`;

  await executeExpiredEntityCleanup(github, owner, repo, {
    entityType: "discussions",
    graphqlField: "discussions",
    resultKey: "discussions",
    entityLabel: "Discussion",
    summaryHeading: "Expired Discussions Cleanup",
    enableDedupe: true, // Discussions may have duplicates across pages
    includeSkippedHeading: true,
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

      const closingMessage = `This discussion was automatically closed because it expired on ${discussion.expirationDate.toISOString()}.` + generateExpiredEntityFooter(workflowName, runUrl, workflowId) + "\n\n<!-- gh-aw-closed -->";

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
}

module.exports = { main };
