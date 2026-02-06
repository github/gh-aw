// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { getRepositoryUrl } = require("./get_repository_url.cjs");
const { replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { getMissingInfoSections } = require("./missing_messages_helper.cjs");
const { getMessages } = require("./messages_core.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_comment";

// Copy helper functions from original file
async function minimizeComment(github, nodeId, reason = "outdated") {
  const query = /* GraphQL */ `
    mutation ($nodeId: ID!, $classifier: ReportedContentClassifiers!) {
      minimizeComment(input: { subjectId: $nodeId, classifier: $classifier }) {
        minimizedComment {
          isMinimized
        }
      }
    }
  `;

  const result = await github.graphql(query, { nodeId, classifier: reason });

  return {
    id: nodeId,
    isMinimized: result.minimizeComment.minimizedComment.isMinimized,
  };
}

/**
 * Find comments on an issue/PR with a specific tracker-id
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue/PR number
 * @param {string} workflowId - Workflow ID to search for
 * @returns {Promise<Array<{id: number, node_id: string, body: string}>>}
 */
async function findCommentsWithTrackerId(github, owner, repo, issueNumber, workflowId) {
  const comments = [];
  let page = 1;
  const perPage = 100;

  // Paginate through all comments
  while (true) {
    const { data } = await github.rest.issues.listComments({
      owner,
      repo,
      issue_number: issueNumber,
      per_page: perPage,
      page,
    });

    if (data.length === 0) {
      break;
    }

    // Filter comments that contain the workflow-id and are NOT reaction comments
    const filteredComments = data
      .filter(comment => comment.body?.includes(`<!-- gh-aw-workflow-id: ${workflowId} -->`) && !comment.body.includes(`<!-- gh-aw-comment-type: reaction -->`))
      .map(({ id, node_id, body }) => ({ id, node_id, body }));

    comments.push(...filteredComments);

    if (data.length < perPage) {
      break;
    }

    page++;
  }

  return comments;
}

/**
 * Find comments on a discussion with a specific workflow ID
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @param {string} workflowId - Workflow ID to search for
 * @returns {Promise<Array<{id: string, body: string}>>}
 */
async function findDiscussionCommentsWithTrackerId(github, owner, repo, discussionNumber, workflowId) {
  const query = /* GraphQL */ `
    query ($owner: String!, $repo: String!, $num: Int!, $cursor: String) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $num) {
          comments(first: 100, after: $cursor) {
            nodes {
              id
              body
            }
            pageInfo {
              hasNextPage
              endCursor
            }
          }
        }
      }
    }
  `;

  const comments = [];
  let cursor = null;

  while (true) {
    const result = await github.graphql(query, { owner, repo, num: discussionNumber, cursor });

    if (!result.repository?.discussion?.comments?.nodes) {
      break;
    }

    const filteredComments = result.repository.discussion.comments.nodes
      .filter(comment => comment.body?.includes(`<!-- gh-aw-workflow-id: ${workflowId} -->`) && !comment.body.includes(`<!-- gh-aw-comment-type: reaction -->`))
      .map(({ id, body }) => ({ id, body }));

    comments.push(...filteredComments);

    if (!result.repository.discussion.comments.pageInfo.hasNextPage) {
      break;
    }

    cursor = result.repository.discussion.comments.pageInfo.endCursor;
  }

  return comments;
}

/**
 * Hide all previous comments from the same workflow
 * @param {any} github - GitHub API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} itemNumber - Issue/PR/Discussion number
 * @param {string} workflowId - Workflow ID to match
 * @param {boolean} isDiscussion - Whether this is a discussion
 * @param {string} reason - Reason for hiding (default: outdated)
 * @param {string[] | null} allowedReasons - List of allowed reasons (default: null for all)
 * @returns {Promise<number>} Number of comments hidden
 */
async function hideOlderComments(github, owner, repo, itemNumber, workflowId, isDiscussion, reason = "outdated", allowedReasons = null) {
  if (!workflowId) {
    core.info("No workflow ID available, skipping hide-older-comments");
    return 0;
  }

  // Normalize reason to uppercase for GitHub API
  const normalizedReason = reason.toUpperCase();

  // Validate reason against allowed reasons if specified (case-insensitive)
  if (allowedReasons && allowedReasons.length > 0) {
    const normalizedAllowedReasons = allowedReasons.map(r => r.toUpperCase());
    if (!normalizedAllowedReasons.includes(normalizedReason)) {
      core.warning(`Reason "${reason}" is not in allowed-reasons list [${allowedReasons.join(", ")}]. Skipping hide-older-comments.`);
      return 0;
    }
  }

  core.info(`Searching for previous comments with workflow ID: ${workflowId}`);

  let comments;
  if (isDiscussion) {
    comments = await findDiscussionCommentsWithTrackerId(github, owner, repo, itemNumber, workflowId);
  } else {
    comments = await findCommentsWithTrackerId(github, owner, repo, itemNumber, workflowId);
  }

  if (comments.length === 0) {
    core.info("No previous comments found with matching workflow ID");
    return 0;
  }

  core.info(`Found ${comments.length} previous comment(s) to hide with reason: ${normalizedReason}`);

  let hiddenCount = 0;
  for (const comment of comments) {
    // TypeScript can't narrow the union type here, but we know it's safe due to isDiscussion check
    // @ts-expect-error - comment has node_id when not a discussion
    const nodeId = isDiscussion ? String(comment.id) : comment.node_id;
    core.info(`Hiding comment: ${nodeId}`);

    const result = await minimizeComment(github, nodeId, normalizedReason);
    hiddenCount++;
    core.info(`✓ Hidden comment: ${nodeId}`);
  }

  core.info(`Successfully hidden ${hiddenCount} comment(s)`);
  return hiddenCount;
}

/**
 * Comment on a GitHub Discussion using GraphQL
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @param {string} message - Comment body
 * @param {string|null|undefined} replyToId - Optional comment node ID to reply to (for threaded comments)
 * @returns {Promise<{id: string, html_url: string, discussion_url: string}>} Comment details
 */
async function commentOnDiscussion(github, owner, repo, discussionNumber, message, replyToId) {
  // 1. Retrieve discussion node ID
  const { repository } = await github.graphql(
    `
    query($owner: String!, $repo: String!, $num: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $num) { 
          id 
          url
        }
      }
    }`,
    { owner, repo, num: discussionNumber }
  );

  if (!repository || !repository.discussion) {
    throw new Error(`Discussion #${discussionNumber} not found in ${owner}/${repo}`);
  }

  const discussionId = repository.discussion.id;
  const discussionUrl = repository.discussion.url;

  // 2. Add comment (with optional replyToId for threading)
  const mutation = replyToId
    ? `mutation($dId: ID!, $body: String!, $replyToId: ID!) {
        addDiscussionComment(input: { discussionId: $dId, body: $body, replyToId: $replyToId }) {
          comment { 
            id 
            body 
            createdAt 
            url
          }
        }
      }`
    : `mutation($dId: ID!, $body: String!) {
        addDiscussionComment(input: { discussionId: $dId, body: $body }) {
          comment { 
            id 
            body 
            createdAt 
            url
          }
        }
      }`;

  const variables = replyToId ? { dId: discussionId, body: message, replyToId } : { dId: discussionId, body: message };

  const result = await github.graphql(mutation, variables);

  const comment = result.addDiscussionComment.comment;

  return {
    id: comment.id,
    html_url: comment.url,
    discussion_url: discussionUrl,
  };
}

/**
 * Main handler factory for add_comment
 * Returns a message handler function that processes individual add_comment messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const hideOlderCommentsEnabled = config.hide_older_comments === true;
  const commentTarget = config.target || "triggering";
  const maxCount = config.max || 20;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  // Check if append-only-comments is enabled in messages config
  const messagesConfig = getMessages();
  const appendOnlyComments = messagesConfig?.appendOnlyComments === true;

  core.info(`Add comment configuration: max=${maxCount}, target=${commentTarget}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (hideOlderCommentsEnabled) {
    core.info("Hide-older-comments is enabled");
  }
  if (appendOnlyComments) {
    core.info("Append-only-comments is enabled - will not hide older comments");
  }

  // Track state
  let processedCount = 0;
  const temporaryIdMap = new Map();
  const createdComments = [];

  // Get workflow ID for hiding older comments
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";

  /**
   * Message handler function
   * @param {Object} message - The add_comment message
   * @param {Object} resolvedTemporaryIds - Resolved temporary IDs
   * @returns {Promise<Object>} Result
   */
  return async function handleAddComment(message, resolvedTemporaryIds) {
    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping add_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Merge resolved temp IDs
    if (resolvedTemporaryIds) {
      for (const [tempId, resolved] of Object.entries(resolvedTemporaryIds)) {
        if (!temporaryIdMap.has(tempId)) {
          temporaryIdMap.set(tempId, resolved);
        }
      }
    }

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "comment");
    if (!repoResult.success) {
      core.warning(`Skipping comment: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);

    // Determine target number and type
    let itemNumber;
    let isDiscussion = false;

    // Check if item_number was explicitly provided in the message
    if (item.item_number !== undefined && item.item_number !== null) {
      // Use the explicitly provided item_number
      itemNumber = typeof item.item_number === "number" ? item.item_number : parseInt(String(item.item_number), 10);

      if (isNaN(itemNumber) || itemNumber <= 0) {
        core.warning(`Invalid item_number specified: ${item.item_number}`);
        return {
          success: false,
          error: `Invalid item_number specified: ${item.item_number}`,
        };
      }

      core.info(`Using explicitly provided item_number: #${itemNumber}`);
    } else {
      // Check if this is a discussion context
      const isDiscussionContext = context.eventName === "discussion" || context.eventName === "discussion_comment";

      if (isDiscussionContext) {
        // For discussions, always use the discussion context
        isDiscussion = true;
        itemNumber = context.payload?.discussion?.number;

        if (!itemNumber) {
          core.warning("Discussion context detected but no discussion number found");
          return {
            success: false,
            error: "No discussion number available",
          };
        }

        core.info(`Using discussion context: #${itemNumber}`);
      } else {
        // For issues/PRs, use the resolveTarget helper which respects target configuration
        const targetResult = resolveTarget({
          targetConfig: commentTarget,
          item: item,
          context: context,
          itemType: "add_comment",
          supportsPR: true, // add_comment supports both issues and PRs
          supportsIssue: false,
        });

        if (!targetResult.success) {
          core.warning(targetResult.error);
          return {
            success: false,
            error: targetResult.error,
          };
        }

        itemNumber = targetResult.number;
        core.info(`Resolved target ${targetResult.contextType} #${itemNumber} (target config: ${commentTarget})`);
      }
    }

    // Replace temporary ID references in body
    let processedBody = replaceTemporaryIdReferences(item.body || "", temporaryIdMap, itemRepo);

    // Add tracker ID and footer
    const trackerIDComment = getTrackerID("markdown");
    if (trackerIDComment) {
      processedBody += "\n\n" + trackerIDComment;
    }

    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const runId = context.runId;
    const runUrl = `https://github.com/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;
    processedBody += `\n\n> AI generated by [${workflowName}](${runUrl})`;

    // Add missing tools and data sections if available
    const missingInfoSections = getMissingInfoSections();
    if (missingInfoSections) {
      processedBody += missingInfoSections;
    }

    core.info(`Adding comment to ${isDiscussion ? "discussion" : "issue/PR"} #${itemNumber} in ${itemRepo}`);

    try {
      // Hide older comments if enabled AND append-only-comments is not enabled
      // When append-only-comments is true, we want to keep all comments visible
      if (hideOlderCommentsEnabled && !appendOnlyComments && workflowId) {
        await hideOlderComments(github, repoParts.owner, repoParts.repo, itemNumber, workflowId, isDiscussion);
      } else if (hideOlderCommentsEnabled && appendOnlyComments) {
        core.info("Skipping hide-older-comments because append-only-comments is enabled");
      }

      /** @type {{ id: string | number, html_url: string }} */
      let comment;
      if (isDiscussion) {
        // Use GraphQL for discussions
        const discussionQuery = `
          query($owner: String!, $repo: String!, $number: Int!) {
            repository(owner: $owner, name: $repo) {
              discussion(number: $number) {
                id
              }
            }
          }
        `;
        const queryResult = await github.graphql(discussionQuery, {
          owner: repoParts.owner,
          repo: repoParts.repo,
          number: itemNumber,
        });

        const discussionId = queryResult?.repository?.discussion?.id;
        if (!discussionId) {
          throw new Error(`Discussion #${itemNumber} not found in ${itemRepo}`);
        }

        comment = await commentOnDiscussion(github, repoParts.owner, repoParts.repo, itemNumber, processedBody, null);
      } else {
        // Use REST API for issues/PRs
        const { data } = await github.rest.issues.createComment({
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: itemNumber,
          body: processedBody,
        });
        comment = data;
      }

      core.info(`Created comment: ${comment.html_url}`);

      // Add tracking metadata
      const commentResult = {
        id: comment.id,
        html_url: comment.html_url,
        _tracking: {
          commentId: comment.id,
          itemNumber: itemNumber,
          repo: itemRepo,
          isDiscussion: isDiscussion,
        },
      };

      createdComments.push(commentResult);

      return {
        success: true,
        commentId: comment.id,
        url: comment.html_url,
        itemNumber: itemNumber,
        repo: itemRepo,
        isDiscussion: isDiscussion,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);

      // Check if this is a 404 error (discussion/issue was deleted or wrong type)
      // @ts-expect-error - Error handling with optional chaining
      const is404 = error?.status === 404 || errorMessage.includes("404") || errorMessage.toLowerCase().includes("not found");

      // If 404 and item_number was explicitly provided and we tried as issue/PR,
      // retry as a discussion (the user may have provided a discussion number)
      if (is404 && !isDiscussion && item.item_number !== undefined && item.item_number !== null) {
        core.info(`Item #${itemNumber} not found as issue/PR, retrying as discussion...`);

        try {
          // Try to find and comment on the discussion
          const discussionQuery = `
            query($owner: String!, $repo: String!, $number: Int!) {
              repository(owner: $owner, name: $repo) {
                discussion(number: $number) {
                  id
                }
              }
            }
          `;
          const queryResult = await github.graphql(discussionQuery, {
            owner: repoParts.owner,
            repo: repoParts.repo,
            number: itemNumber,
          });

          const discussionId = queryResult?.repository?.discussion?.id;
          if (!discussionId) {
            throw new Error(`Discussion #${itemNumber} not found in ${itemRepo}`);
          }

          core.info(`Found discussion #${itemNumber}, adding comment...`);
          const comment = await commentOnDiscussion(github, repoParts.owner, repoParts.repo, itemNumber, processedBody, null);

          core.info(`Created comment on discussion: ${comment.html_url}`);

          // Add tracking metadata
          const commentResult = {
            id: comment.id,
            html_url: comment.html_url,
            _tracking: {
              commentId: comment.id,
              itemNumber: itemNumber,
              repo: itemRepo,
              isDiscussion: true,
            },
          };

          createdComments.push(commentResult);

          return {
            success: true,
            commentId: comment.id,
            url: comment.html_url,
            itemNumber: itemNumber,
            repo: itemRepo,
            isDiscussion: true,
          };
        } catch (discussionError) {
          const discussionErrorMessage = getErrorMessage(discussionError);
          // @ts-expect-error - Error handling with optional chaining
          const isDiscussion404 = discussionError?.status === 404 || discussionErrorMessage.toLowerCase().includes("not found");

          if (isDiscussion404) {
            // Neither issue/PR nor discussion found - truly doesn't exist
            core.warning(`Target #${itemNumber} was not found as issue, PR, or discussion (may have been deleted): ${discussionErrorMessage}`);
            return {
              success: true,
              warning: `Target not found: ${discussionErrorMessage}`,
              skipped: true,
            };
          }

          // Other error when trying as discussion
          core.error(`Failed to add comment to discussion: ${discussionErrorMessage}`);
          return {
            success: false,
            error: discussionErrorMessage,
          };
        }
      }

      if (is404) {
        // Treat 404s as warnings - the target was deleted between execution and safe output processing
        core.warning(`Target was not found (may have been deleted): ${errorMessage}`);
        return {
          success: true,
          warning: `Target not found: ${errorMessage}`,
          skipped: true,
        };
      }

      // For non-404 errors, fail as before
      core.error(`Failed to add comment: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
