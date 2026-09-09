// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { assembleMarkdownBodyParts } = require("./markdown_body_helpers.cjs");
const { generateWorkflowCallIdMarker, matchesWorkflowId } = require("./generate_footer.cjs");
const { getRepositoryUrl } = require("./get_repository_url.cjs");
const { replaceTemporaryIdReferences, resolveSafeOutputIssueTarget } = require("./temporary_id.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { parseBoolTemplatable, parseIntTemplatable } = require("./templatable.cjs");
const { resolveTarget, isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { getMissingInfoSections } = require("./missing_messages_helper.cjs");
const { getMessages } = require("./messages_core.cjs");
const { getBodyHeader, getDisclosureHeader } = require("./messages_header.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { MAX_COMMENT_LENGTH, MAX_MENTIONS, MAX_LINKS, enforceCommentLimits } = require("./comment_limit_helpers.cjs");
const { createDiscussionComment, resolveTopLevelDiscussionCommentId } = require("./github_api_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { isPayloadUserBot } = require("./resolve_mentions.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "add_comment";
// Keep the full list of accepted explicit wildcard target fields (including aliases
// pre-handled by resolveSafeOutputIssueTarget) to preserve a defensive boundary check.
const WILDCARD_TARGET_FIELDS = ["item_number", "issue_number", "pull_request_number", "pr_number", "pr", "pull_number"];

/**
 * Parse trusted comment IDs supplied by workflow configuration.
 * @param {unknown} value
 * @returns {Set<string>}
 */
function parseAllowedCommentIds(value) {
  const values = [];
  const visit = item => {
    if (Array.isArray(item)) {
      for (const child of item) visit(child);
      return;
    }
    if (typeof item === "number" && Number.isInteger(item) && item > 0) {
      values.push(String(item));
      return;
    }
    if (typeof item !== "string") {
      return;
    }
    const trimmed = item.trim();
    if (!trimmed) {
      return;
    }
    if (trimmed.startsWith("[") || trimmed.startsWith('"')) {
      try {
        visit(JSON.parse(trimmed));
        return;
      } catch {
        // Fall through to delimiter parsing.
      }
    }
    for (const part of trimmed.split(/[,\s]+/)) {
      if (/^[1-9]\d*$/.test(part)) {
        values.push(part);
      }
    }
  };
  visit(value);
  return new Set(values);
}

/**
 * @param {unknown} value
 * @returns {{success: true, commentId: number} | {success: false, error: string}}
 */
function parsePositiveCommentId(value) {
  const commentId = Number(value);
  if (!Number.isInteger(commentId) || commentId <= 0) {
    return { success: false, error: "comment_id must be a positive integer" };
  }
  return { success: true, commentId };
}

/**
 * Validate an agent-supplied comment_id against the trusted workflow allowlist.
 * @param {unknown} value
 * @param {any} config
 * @param {string} commentTarget
 * @returns {{success: true, commentId: number} | {success: false, error: string}}
 */
function resolveAllowedCommentId(value, config, commentTarget) {
  const parsed = parsePositiveCommentId(value);
  if (!parsed.success) {
    return parsed;
  }
  if (commentTarget !== "*") {
    return {
      success: false,
      error: "comment_id is only allowed when safe-outputs.add-comment.target is '*' and the ID is listed in safe-outputs.add-comment.allows-comment-ids",
    };
  }
  const allowedCommentIds = parseAllowedCommentIds(config.allows_comment_ids ?? config["allows-comment-ids"]);
  if (allowedCommentIds.size === 0) {
    return {
      success: false,
      error: "comment_id requires safe-outputs.add-comment.allows-comment-ids to list trusted comment IDs",
    };
  }
  if (!allowedCommentIds.has(String(parsed.commentId))) {
    return {
      success: false,
      error: "comment_id is not listed in safe-outputs.add-comment.allows-comment-ids",
    };
  }
  return parsed;
}

/**
 * Deduplicate an array of strings using case-insensitive comparison, preserving original casing and order.
 * @param {string[]} aliases
 * @returns {string[]}
 */
function deduplicateCaseInsensitive(aliases) {
  const seen = new Set();
  return aliases.filter(alias => {
    const key = alias.toLowerCase();
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/**
 * @param {unknown} value
 * @returns {value is { enabled?: boolean | string, match?: unknown[] }}
 */
function isHideOlderCommentsObject(value) {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

/**
 * @param {unknown[]} ids
 * @returns {string[]}
 */
function normalizeWorkflowIdList(ids) {
  return [
    ...new Set(
      ids
        .filter(id => typeof id === "string")
        .map(id => id.trim())
        .filter(Boolean)
    ),
  ];
}

/**
 * Normalize a list of mention aliases: trim, strip leading "@" characters, and drop empty entries.
 * @param {unknown} aliases
 * @returns {string[]}
 */
function normalizeMentionAliases(aliases) {
  if (!Array.isArray(aliases)) return [];
  return aliases.map(alias => (typeof alias === "string" ? alias.trim().replace(/^@+/, "") : "")).filter(alias => alias.length > 0);
}

/**
 * Resolve effective event name/payload for native and forwarded contexts.
 * Supports:
 * - workflow_dispatch with event_name/event_payload inputs (via resolveInvocationContext)
 * - workflow_call/workflow_dispatch with aw_context input fallback
 *
 * Precedence:
 * 1) Start with the raw GitHub Actions context
 * 2) Apply resolveInvocationContext normalization/overrides
 * 3) Apply aw_context fallback only for relayed pull_request_review_comment metadata
 *    (this intentionally overrides event name/payload identifiers when present)
 * @param {any} rawContext
 * @returns {{ eventName: string, payload: any, workflowRepo?: { owner: string, repo: string } }}
 */
function resolveEffectiveEventContext(rawContext) {
  let eventName = rawContext?.eventName || "";
  let payload = rawContext?.payload || {};
  let workflowRepo;

  try {
    const invocation = resolveInvocationContext(rawContext);
    if (invocation?.eventName) {
      eventName = invocation.eventName;
    }
    if (invocation?.eventPayload && typeof invocation.eventPayload === "object") {
      payload = invocation.eventPayload;
    }
    if (invocation?.workflowRepo?.owner && invocation?.workflowRepo?.repo) {
      workflowRepo = invocation.workflowRepo;
    }
  } catch {
    // Best-effort only; fall back to the raw context.
  }

  if (!workflowRepo) {
    workflowRepo = rawContext?.repo;
  }

  // For workflow_call (and workflow_dispatch relay cases), aw_context can carry
  // the original event type/item/comment identifiers. This runs after
  // resolveInvocationContext on purpose so aw_context can act as the final fallback.
  const awContextRaw = rawContext?.payload?.inputs?.aw_context;
  if (typeof awContextRaw === "string" && awContextRaw.trim() !== "") {
    try {
      const awContext = JSON.parse(awContextRaw);
      const awEventType = typeof awContext?.event_type === "string" ? awContext.event_type : "";
      const awItemNumber = Number(awContext?.item_number);
      const awCommentId = Number(awContext?.comment_id);

      if (awEventType === "pull_request_review_comment" && Number.isInteger(awItemNumber) && awItemNumber > 0) {
        eventName = awEventType;
        payload = {
          ...payload,
          pull_request: {
            ...(payload?.pull_request || {}),
            number: awItemNumber,
          },
          ...(Number.isInteger(awCommentId) && awCommentId > 0
            ? {
                comment: {
                  ...(payload?.comment || {}),
                  id: awCommentId,
                },
              }
            : {}),
        };
      }
    } catch {
      // Ignore malformed aw_context and continue with existing context.
    }
  }

  return { eventName, payload, workflowRepo };
}

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
 * Find comments on an issue/PR with any matching workflow ID marker
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue/PR number
 * @param {string[]} workflowIds - Workflow IDs to search for
 * @returns {Promise<Array<{id: number, node_id: string, body: string}>>}
 */
async function findCommentsWithTrackerId(github, owner, repo, issueNumber, workflowIds) {
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

    const filteredComments = data.filter(comment => workflowIds.some(id => matchesWorkflowId(comment.body, id))).map(({ id, node_id, body }) => ({ id, node_id, body }));

    comments.push(...filteredComments);

    if (data.length < perPage) {
      break;
    }

    page++;
  }

  return comments;
}

/**
 * Find comments on a discussion with any matching workflow ID marker
 * @param {any} github - GitHub GraphQL instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} discussionNumber - Discussion number
 * @param {string[]} workflowIds - Workflow IDs to search for
 * @returns {Promise<Array<{id: string, body: string}>>}
 */
async function findDiscussionCommentsWithTrackerId(github, owner, repo, discussionNumber, workflowIds) {
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
  /** @type {any} */
  let cursor = null;

  while (true) {
    const result = await github.graphql(query, { owner, repo, num: discussionNumber, cursor });

    if (!result.repository?.discussion?.comments?.nodes) {
      break;
    }

    const filteredComments = result.repository.discussion.comments.nodes.filter(comment => workflowIds.some(id => matchesWorkflowId(comment.body, id))).map(({ id, body }) => ({ id, body }));

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
 * @param {string[]} workflowIds - Workflow IDs to match
 * @param {boolean} isDiscussion - Whether this is a discussion
 * @param {string} reason - Reason for hiding (default: outdated)
 * @param {string[] | null} allowedReasons - List of allowed reasons (default: null for all)
 * @returns {Promise<number>} Number of comments hidden
 */
async function hideOlderComments(github, owner, repo, itemNumber, workflowIds, isDiscussion, reason = "outdated", allowedReasons = null) {
  if (!Array.isArray(workflowIds) || workflowIds.length === 0) {
    core.info("No workflow IDs provided, skipping hide-older-comments");
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

  core.info(`Searching for previous comments with workflow IDs: ${workflowIds.join(", ")}`);

  let comments;
  if (isDiscussion) {
    comments = await findDiscussionCommentsWithTrackerId(github, owner, repo, itemNumber, workflowIds);
  } else {
    comments = await findCommentsWithTrackerId(github, owner, repo, itemNumber, workflowIds);
  }

  if (comments.length === 0) {
    core.info("No previous comments found with matching workflow ID");
    return 0;
  }

  core.info(`Found ${comments.length} previous comment(s) to hide with reason: ${normalizedReason}`);

  let hiddenCount = 0;
  for (const comment of comments) {
    const nodeId = isDiscussion ? String(comment.id) : /** @type {any} */ comment.node_id;
    core.info(`Hiding comment: ${nodeId}`);

    await minimizeComment(github, nodeId, normalizedReason);
    hiddenCount++;
    core.info(`✓ Hidden comment: ${nodeId}`);
  }

  core.info(`Successfully hidden ${hiddenCount} comment(s)`);
  return hiddenCount;
}

/**
 * Check whether an error from a GitHub GraphQL or REST call indicates that the
 * integration token lacks the permissions required to write to a discussion.
 * @param {unknown} error
 * @returns {boolean}
 */
function isDiscussionIntegrationAccessError(error) {
  // Lowercase for case-insensitive comparison via .toLowerCase()
  const fragment = "resource not accessible by integration";
  /** @type {string[]} */
  const messages = [getErrorMessage(error)];

  if (error && typeof error === "object" && "errors" in error && Array.isArray(/** @type {any} */ error.errors)) {
    for (const graphQLError of /** @type {any} */ error.errors) {
      if (typeof graphQLError?.message === "string") {
        messages.push(graphQLError.message);
      }
    }
  }

  return messages.some(message => message.toLowerCase().includes(fragment));
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
    throw new Error(`${ERR_NOT_FOUND}: Discussion #${discussionNumber} not found in ${owner}/${repo}`);
  }

  const discussionId = repository.discussion.id;
  const discussionUrl = repository.discussion.url;

  // 2. Add comment (with optional replyToId for threading)
  const comment = await createDiscussionComment(github, discussionId, message, replyToId);

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
  const hideOlderCommentsConfig = isHideOlderCommentsObject(config.hide_older_comments) ? config.hide_older_comments : null;
  const hideOlderCommentsEnabled = parseBoolTemplatable(hideOlderCommentsConfig ? (hideOlderCommentsConfig.enabled ?? true) : config.hide_older_comments, false);
  const hideOlderCommentsMatch = Array.isArray(hideOlderCommentsConfig?.match)
    ? normalizeWorkflowIdList(hideOlderCommentsConfig.match)
    : Array.isArray(config.hide_older_comments_match)
      ? normalizeWorkflowIdList(config.hide_older_comments_match)
      : [];
  const commentTarget = config.target || "triggering";
  const maxCount = config.max || 20;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const includeFooter = parseBoolTemplatable(config.footer, true);
  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const mentionsDisabled = config.mentions === false || config.mentions?.enabled === false;
  const preResolvedMentionAliases = !mentionsDisabled ? normalizeMentionAliases(config.allowedMentionAliases) : [];
  const configuredMentionAliases = !mentionsDisabled ? normalizeMentionAliases(config.mentions?.allowed) : [];
  const maxMentions = mentionsDisabled ? undefined : parseIntTemplatable(config.mentions?.max, 50);

  // Create an authenticated GitHub client. Uses config["github-token"] when set
  // (for cross-repository operations), otherwise falls back to the step-level github.
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  // Check if append-only-comments is enabled in messages config
  const messagesConfig = getMessages();
  const appendOnlyComments = messagesConfig?.appendOnlyComments === true;

  core.info(`Add comment configuration: max=${maxCount}, target=${commentTarget}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);
  }
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);
  if (hideOlderCommentsEnabled) {
    core.info("Hide-older-comments is enabled");
    if (hideOlderCommentsMatch.length > 0) {
      core.info(`Hide-older-comments additional workflow matches: ${hideOlderCommentsMatch.join(", ")}`);
    }
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
  const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";

  /**
   * Message handler function
   * @param {Object} message - The add_comment message
   * @param {Object} resolvedTemporaryIds - Resolved temporary IDs
   * @returns {Promise<Object>} Result
   */
  return async function handleAddComment(message, resolvedTemporaryIds) {
    const effectiveEventContext = resolveEffectiveEventContext(context);
    const effectiveContext = {
      ...context,
      eventName: effectiveEventContext.eventName,
      payload: effectiveEventContext.payload,
    };

    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping add_comment: max count of ${maxCount} reached`);
      return {
        success: false,
        skipped: true,
        reasonCode: "MAX_COUNT_REACHED",
        reason: "Max count reached",
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    // Merge resolved temp IDs
    Object.entries(resolvedTemporaryIds ?? {}).forEach(([tempId, resolved]) => {
      if (!temporaryIdMap.has(tempId)) temporaryIdMap.set(tempId, resolved);
    });

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "comment");
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
    /** @type {any} */
    let commentIdToReuse = null;
    const explicitCommentIdRaw = message.comment_id ?? message.commentId ?? message["comment-id"];
    const hasExplicitCommentId = explicitCommentIdRaw !== undefined && explicitCommentIdRaw !== null && String(explicitCommentIdRaw).trim() !== "";
    if (hasExplicitCommentId) {
      const allowedCommentId = resolveAllowedCommentId(explicitCommentIdRaw, config, commentTarget);
      if (!allowedCommentId.success) {
        return {
          success: false,
          error: allowedCommentId.error,
        };
      }
      commentIdToReuse = allowedCommentId.commentId;
    }

    // Check if item_number or issue_number was explicitly provided in the message.
    // item_number takes precedence over issue_number when both are present.
    // pr-number is accepted as an alias for item_number for robustness.
    /** @type {{ success: boolean, number?: number | null, deferred?: boolean, error?: string }} */
    let itemTargetResult = { success: true, number: null };
    if (hasExplicitCommentId) {
      try {
        const { data: existingComment } = await githubClient.rest.issues.getComment({
          owner: repoParts.owner,
          repo: repoParts.repo,
          comment_id: commentIdToReuse,
        });
        const targetURL = existingComment?.issue_url || "";
        const match = String(targetURL).match(/\/issues\/(\d+)(?:[#/?]|$)/);
        if (match) {
          itemNumber = Number(match[1]);
        }
        if (!Number.isInteger(itemNumber) || itemNumber <= 0) {
          return {
            success: false,
            error: `Could not derive issue or pull request number for allowed comment_id ${commentIdToReuse}`,
          };
        }
        core.info(`Using allowed existing comment ID: ${commentIdToReuse}`);
      } catch (err) {
        return {
          success: false,
          error: `Failed to fetch allowed comment_id ${commentIdToReuse}: ${getErrorMessage(err)}`,
        };
      }
    } else {
      itemTargetResult = resolveSafeOutputIssueTarget({ message, tempIdMap: temporaryIdMap, repoParts, handlerType: HANDLER_TYPE, aliases: ["item_number", "issue_number", "pr-number"] });
      if (!itemTargetResult.success) return itemTargetResult;
    }

    if (itemTargetResult.number != null) {
      itemNumber = itemTargetResult.number;
      core.info(`Using explicitly provided target number (item_number/issue_number/pr-number): #${itemNumber}`);
    } else if (!hasExplicitCommentId) {
      // Check if this is a discussion context
      const isDiscussionContext = effectiveContext.eventName === "discussion" || effectiveContext.eventName === "discussion_comment";

      if (isDiscussionContext) {
        // For discussions, always use the discussion context
        isDiscussion = true;
        itemNumber = effectiveContext.payload?.discussion?.number;

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
          item: message,
          context: effectiveContext,
          itemType: "add_comment",
          supportsPR: true, // add_comment supports both issues and PRs
          supportsIssue: false,
        });

        if (!targetResult.success) {
          if (targetResult.shouldFail) {
            const hasExplicitWildcardTargetField = WILDCARD_TARGET_FIELDS.some(field => message[field] != null);
            const missingWildcardTarget = commentTarget === "*" && !hasExplicitWildcardTargetField;
            if (missingWildcardTarget) {
              core.info(targetResult.error);
              return {
                success: false,
                skipped: true,
                reasonCode: "NO_CONTEXT",
                reason: "No target context available",
                error: targetResult.error,
              };
            }
            core.warning(targetResult.error);
            return {
              success: false,
              error: targetResult.error,
            };
          } else {
            // No triggering context (e.g. schedule run) — silently skip rather than fail
            core.info(targetResult.error);
            return {
              success: false,
              skipped: true,
              reasonCode: "NO_CONTEXT",
              reason: "No target context available",
              error: targetResult.error,
            };
          }
        }

        itemNumber = targetResult.number;
        core.info(`Resolved target ${targetResult.contextType} #${itemNumber} (target config: ${commentTarget})`);
      }
    }

    // Apply required-labels and required-title-prefix filters (issues/PRs only, not discussions)
    if (!isDiscussion && (requiredLabels.length > 0 || requiredTitlePrefix)) {
      try {
        const { data: filterItem } = await githubClient.rest.issues.get({
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: itemNumber,
        });
        if (requiredLabels.length > 0) {
          const itemLabels = (filterItem.labels || []).map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || ""));
          const missingLabels = requiredLabels.filter(r => !itemLabels.includes(r));
          if (missingLabels.length > 0) {
            core.info(`Skipping add_comment for #${itemNumber}: does not match required-labels filter (${requiredLabels.join(", ")})`);
            return {
              success: false,
              skipped: true,
              reasonCode: "REQUIRED_LABELS_MISMATCH",
              reason: "Required labels missing",
              error: "Item does not match required-labels filter",
              target: { repo: itemRepo, number: itemNumber },
              safeDetails: { requiredLabels, missingLabels },
            };
          }
        }
        if (requiredTitlePrefix && !filterItem.title?.startsWith(requiredTitlePrefix)) {
          core.info(`Skipping add_comment for #${itemNumber}: title does not start with required prefix "${requiredTitlePrefix}"`);
          return {
            success: false,
            skipped: true,
            reasonCode: "REQUIRED_TITLE_PREFIX_MISMATCH",
            reason: "Required title prefix missing",
            error: "Item title does not start with required prefix",
            target: { repo: itemRepo, number: itemNumber },
            safeDetails: { requiredTitlePrefix },
          };
        }
      } catch (err) {
        core.warning(`Could not fetch item #${itemNumber} to check filters: ${getErrorMessage(err)}`);
        return { success: false, error: `Failed to check required-labels/required-title-prefix filter: ${getErrorMessage(err)}` };
      }
    }

    // Collect parent issue/PR/discussion authors to allow in @mentions.
    // The body was already sanitized in collect_ndjson_output with allowed mentions from the
    // event payload (which includes the issue author). Re-sanitizing here without the same
    // allowed aliases would neutralize those preserved mentions. We re-add the parent entity
    // author so the second sanitization pass does not accidentally strip them.
    const parentAuthors = [];
    if (!mentionsDisabled) {
      if (!isDiscussion) {
        if (itemTargetResult.number != null || hasExplicitCommentId) {
          // Explicit item_number/issue_number: fetch the issue/PR to get its author
          try {
            const { data: issueData } = await githubClient.rest.issues.get({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: itemNumber,
            });
            if (issueData.user?.login && !isPayloadUserBot(issueData.user)) {
              parentAuthors.push(issueData.user.login);
            }
          } catch (err) {
            core.info(`Could not fetch parent issue/PR author for mention allowlist: ${getErrorMessage(err)}`);
          }
        } else {
          // Triggering context: use the issue/PR author from the event payload
          if (context.payload?.issue?.user?.login && !isPayloadUserBot(context.payload.issue.user)) {
            parentAuthors.push(context.payload.issue.user.login);
          }
          if (context.payload?.pull_request?.user?.login && !isPayloadUserBot(context.payload.pull_request.user)) {
            parentAuthors.push(context.payload.pull_request.user.login);
          }
        }
      } else {
        // Discussion: use the discussion author from the event payload
        if (context.payload?.discussion?.user?.login && !isPayloadUserBot(context.payload.discussion.user)) {
          parentAuthors.push(context.payload.discussion.user.login);
        }
      }
    }
    const allowedMentionAliases = deduplicateCaseInsensitive([...parentAuthors, ...preResolvedMentionAliases, ...configuredMentionAliases]);

    if (allowedMentionAliases.length > 0) {
      core.info(`[MENTIONS] Allowing aliases in comment: ${allowedMentionAliases.join(", ")}`);
    }

    // Replace temporary ID references in body
    let processedBody = replaceTemporaryIdReferences(message.body || "", temporaryIdMap, itemRepo);

    // Sanitize content to prevent injection attacks, allowing parent issue/PR/discussion authors
    // so they can be @mentioned in the generated comment.
    processedBody = sanitizeContent(processedBody, { allowedAliases: allowedMentionAliases, maxMentions });

    // Enforce max limits before processing (validates user-provided content)
    try {
      enforceCommentLimits(processedBody);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Comment validation failed: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }

    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const runUrl = buildWorkflowRunUrl(context, effectiveEventContext.workflowRepo ?? context.repo);
    const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";

    // Compute caution first so prefix assembly preserves the original execution order.
    const detectionCaution = assembleMarkdownBodyParts({
      includeFooter: false,
      workflowName,
      runUrl,
    }).detectionCaution;

    // Inject body header and disclosure header if configured (placed after caution, before user content)
    const bodyHeader = getBodyHeader({ workflowName, runUrl });
    const disclosureHeader = getDisclosureHeader({ workflowName, runUrl });

    // Build prefix: caution (if any) → disclosure header (if any) → body header (if any) → user content
    const prefixParts = [detectionCaution, disclosureHeader, bodyHeader].filter(Boolean);
    if (prefixParts.length > 0) processedBody = prefixParts.join("\n\n") + "\n\n" + processedBody;

    // Add tracker ID and footer
    const trackerIDComment = getTrackerID("markdown");
    if (trackerIDComment) {
      processedBody += "\n\n" + trackerIDComment;
    }

    // Get triggering context for footer
    const triggeringIssueNumber = context.payload.issue?.number;
    const triggeringPRNumber = context.payload.pull_request?.number;
    const triggeringDiscussionNumber = context.payload.discussion?.number;

    // Generate history URL with type= based on execution context
    const historyUrl =
      generateHistoryUrl({
        owner: repoParts.owner,
        repo: repoParts.repo,
        itemType: isDiscussion ? "discussion_comment" : "comment",
        workflowCallId: callerWorkflowId,
        workflowId,
        serverUrl: context.serverUrl,
      }) || undefined;

    const markdownParts = assembleMarkdownBodyParts({
      includeFooter,
      workflowName,
      runUrl,
      workflowSource,
      workflowSourceURL,
      triggeringIssueNumber,
      triggeringPRNumber,
      triggeringDiscussionNumber,
      historyUrl,
      markerWhenFooterDisabled: "xml",
    });

    if (includeFooter) {
      // When footer is enabled, add full footer with attribution and XML markers.
      processedBody += "\n\n" + markdownParts.footer;
    } else {
      // When footer is disabled, only add XML marker for searchability (no visible attribution text)
      processedBody += "\n\n" + markdownParts.noFooterMarker;
    }

    // Add workflow-call-id marker when available to allow close-older-comments to
    // distinguish callers that share the same reusable workflow (and GH_AW_WORKFLOW_ID)
    if (callerWorkflowId) {
      processedBody += "\n" + generateWorkflowCallIdMarker(callerWorkflowId);
    }

    // Enforce max limits again after adding footer and metadata
    // This ensures the final body (including generated content) doesn't exceed limits
    try {
      enforceCommentLimits(processedBody);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Final comment body validation failed: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }

    core.info(`Adding comment to ${isDiscussion ? "discussion" : "issue/PR"} #${itemNumber} in ${itemRepo}`);

    // If in staged mode, preview the comment without creating it
    if (isStaged) {
      logStagedPreviewInfo(`Would add comment to ${isDiscussion ? "discussion" : "issue/PR"} #${itemNumber} in ${itemRepo}`);
      return {
        success: true,
        staged: true,
        previewInfo: {
          itemNumber,
          repo: itemRepo,
          isDiscussion,
          bodyLength: processedBody.length,
        },
      };
    }

    // Records a created comment in createdComments and returns the success result.
    const recordComment = (/** @type {{ id: string | number, html_url: string }} */ comment, /** @type {boolean} */ isDiscussionFlag) => {
      createdComments.push({ id: comment.id, html_url: comment.html_url, _tracking: { commentId: comment.id, itemNumber, repo: itemRepo, isDiscussion: isDiscussionFlag } });
      return { success: true, commentId: comment.id, url: comment.html_url, body: processedBody, itemNumber, repo: itemRepo, isDiscussion: isDiscussionFlag };
    };

    // Normalize reply_to_id once so both the main discussion path and the
    // 404 discussion fallback path use the same validated value.
    const normalizedExplicitReplyToId = message.reply_to_id === undefined || message.reply_to_id === null ? null : String(message.reply_to_id).trim();
    if (message.reply_to_id !== undefined && message.reply_to_id !== null && !normalizedExplicitReplyToId) {
      core.warning("Ignoring empty discussion reply_to_id after normalization");
    }

    const rawTarget = message.target;
    const allowedTargets = ["status", "issue", "discussion"];
    if (rawTarget !== undefined && !allowedTargets.includes(rawTarget)) {
      core.warning(`Ignoring unrecognized message-level target value "${rawTarget}": only "status", "issue", or "discussion" are supported. Proceeding without comment reuse.`);
    }
    const isStatusCommentTarget = rawTarget === "status";
    const statusCommentIdRaw = process.env.GH_AW_COMMENT_ID || "";
    if (commentIdToReuse === null && isStatusCommentTarget) {
      const parsedStatusCommentId = Number(statusCommentIdRaw);
      if (Number.isInteger(parsedStatusCommentId) && parsedStatusCommentId > 0) {
        commentIdToReuse = parsedStatusCommentId;
      } else {
        core.info("target=status was requested but no reusable status comment id was available; creating a new comment");
      }
    }

    try {
      // Hide older comments if enabled AND append-only-comments is not enabled
      // When append-only-comments is true, we want to keep all comments visible
      if (hideOlderCommentsEnabled) {
        if (commentIdToReuse !== null) {
          core.info("Skipping hide-older-comments because an existing comment is being updated");
        } else if (appendOnlyComments) {
          core.info("Skipping hide-older-comments because append-only-comments is enabled");
        } else {
          const hideWorkflowIds = normalizeWorkflowIdList([workflowId, ...hideOlderCommentsMatch]);
          await hideOlderComments(githubClient, repoParts.owner, repoParts.repo, itemNumber, hideWorkflowIds, isDiscussion);
        }
      }

      /** @type {{ id: string | number, html_url: string }} */
      let comment;
      if (isDiscussion) {
        if (commentIdToReuse !== null) {
          return {
            success: false,
            error: "comment_id and target=status are only supported for issue and pull request comments",
          };
        }
        // When triggered by a discussion_comment event (without explicit item_number),
        // reply as a threaded comment to the triggering comment instead of posting top-level.
        // GitHub Discussions only supports two nesting levels, so if the triggering comment is
        // itself a reply, we resolve the top-level parent's node ID to use as replyToId.
        const hasExplicitItemNumber = itemTargetResult.number != null;
        let replyToId;
        if (context.eventName === "discussion_comment" && !hasExplicitItemNumber) {
          // When triggered by a discussion_comment event, thread the reply under the triggering comment.
          replyToId = await resolveTopLevelDiscussionCommentId(githubClient, context.payload?.comment?.node_id);
        } else if (normalizedExplicitReplyToId) {
          // Allow the agent to explicitly specify a reply_to_id (e.g. for workflow_dispatch-triggered
          // workflows that know the target comment node ID). Apply resolveTopLevelDiscussionCommentId
          // to handle cases where the caller passes a reply node ID instead of a top-level one.
          replyToId = await resolveTopLevelDiscussionCommentId(githubClient, normalizedExplicitReplyToId);
        } else {
          replyToId = null;
        }
        if (replyToId) {
          core.info(`Replying as threaded comment to discussion comment node ID: ${replyToId}`);
        }
        comment = await commentOnDiscussion(githubClient, repoParts.owner, repoParts.repo, itemNumber, processedBody, replyToId);
      } else {
        const shouldReplyToTriggeringPRReviewComment = !hasExplicitCommentId && effectiveContext.eventName === "pull_request_review_comment" && itemTargetResult.number == null;
        const triggeringReviewCommentId = Number(effectiveContext.payload?.comment?.id);

        if (shouldReplyToTriggeringPRReviewComment && Number.isInteger(triggeringReviewCommentId) && triggeringReviewCommentId > 0) {
          core.info(`Replying inline to triggering PR review comment ID: ${triggeringReviewCommentId}`);
          const { data } = await githubClient.rest.pulls.createReplyForReviewComment({
            owner: repoParts.owner,
            repo: repoParts.repo,
            pull_number: itemNumber,
            comment_id: triggeringReviewCommentId,
            body: processedBody,
          });
          comment = data;
        } else if (commentIdToReuse !== null) {
          core.info(`Updating existing comment ID: ${commentIdToReuse}`);
          const { data } = await githubClient.rest.issues.updateComment({
            owner: repoParts.owner,
            repo: repoParts.repo,
            comment_id: commentIdToReuse,
            body: processedBody,
          });
          comment = data;
        } else {
          if (shouldReplyToTriggeringPRReviewComment) {
            core.warning("Triggering PR review comment ID is missing or invalid; falling back to top-level PR comment");
          }
          // Use REST API for issues/PRs
          const { data } = await githubClient.rest.issues.createComment({
            owner: repoParts.owner,
            repo: repoParts.repo,
            issue_number: itemNumber,
            body: processedBody,
          });
          comment = data;
        }
      }

      core.info(`Created comment: ${comment.html_url}`);
      return recordComment(comment, isDiscussion);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      const normalizedErrorMessage = errorMessage.toLowerCase();
      // Known GitHub lock-related message fragments observed from REST/GraphQL comment APIs.
      const lockPhrases = ["issue is locked", "conversation is locked", "resource is locked", "resource locked"];
      const hasKnownLockPhrase = lockPhrases.some(phrase => normalizedErrorMessage.includes(phrase));

      // Check if this is a 404 error (discussion/issue was deleted or wrong type)
      const is404 = error?.status === 404 || errorMessage.includes("404") || normalizedErrorMessage.includes("not found");
      const isHttp423Locked = error?.status === 423;
      const isHttp403WithLockedMessage = error?.status === 403 && normalizedErrorMessage.includes("locked");
      const isLockedByKnownMessageWithoutStatus = error?.status == null && hasKnownLockPhrase;
      const isLocked = isHttp423Locked || isHttp403WithLockedMessage || isLockedByKnownMessageWithoutStatus;

      // If 404 and item_number was explicitly provided and we tried as issue/PR,
      // retry as a discussion (the user may have provided a discussion number)
      if (is404 && !isDiscussion && itemTargetResult.number != null) {
        core.info(`Item #${itemNumber} not found as issue/PR, retrying as discussion...`);

        try {
          core.info(`Trying #${itemNumber} as discussion...`);
          // When retrying as discussion, honour the normalized reply_to_id from the message.
          // Apply resolveTopLevelDiscussionCommentId to handle nested reply node IDs.
          const fallbackReplyToId = normalizedExplicitReplyToId ? await resolveTopLevelDiscussionCommentId(githubClient, normalizedExplicitReplyToId) : null;
          if (fallbackReplyToId) {
            core.info(`Replying as threaded comment to discussion comment node ID: ${fallbackReplyToId}`);
          }
          const comment = await commentOnDiscussion(githubClient, repoParts.owner, repoParts.repo, itemNumber, processedBody, fallbackReplyToId);

          core.info(`Created comment on discussion: ${comment.html_url}`);
          return recordComment(comment, true);
        } catch (discussionError) {
          const discussionErrorMessage = getErrorMessage(discussionError);
          const isDiscussion404 = discussionError?.status === 404 || discussionErrorMessage.toLowerCase().includes("not found");
          const isIntegrationAccessError = isDiscussionIntegrationAccessError(discussionError);

          if (isDiscussion404) {
            // Neither issue/PR nor discussion found - truly doesn't exist
            core.warning(`Target #${itemNumber} was not found as issue, PR, or discussion (may have been deleted): ${discussionErrorMessage}`);
            return {
              success: true,
              warning: `Target not found: ${discussionErrorMessage}`,
              skipped: true,
              reasonCode: "TARGET_NOT_FOUND",
              reason: "Target not found",
              target: { repo: itemRepo, number: itemNumber },
            };
          }

          if (isIntegrationAccessError) {
            // The integration token lacks discussions:write scope — surface as a configuration
            // warning (skip) rather than failing the entire safe-outputs job.
            const warningMessage =
              `Skipping add_comment for discussion #${itemNumber}: configuration mismatch ` +
              `(GitHub integration token cannot add comments to discussions: Resource not accessible by integration). ` +
              `Use safe-outputs.add-comment.github-token with a token that has discussions:write scope.`;
            core.warning(warningMessage);
            return {
              success: false,
              skipped: true,
              reasonCode: "DISCUSSIONS_TOKEN_SCOPE_MISMATCH",
              reason: "GitHub token cannot add comments to discussions",
              target: { repo: itemRepo, number: itemNumber },
              error: warningMessage,
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
          reasonCode: "TARGET_NOT_FOUND",
          reason: "Target not found",
          target: { repo: itemRepo, number: itemNumber },
        };
      }

      if (isLocked) {
        // Treat locked targets as warnings - locked PRs/issues are a valid repository state
        core.warning(`Target is locked, skipping comment: ${errorMessage}`);
        return {
          success: true,
          warning: `Target is locked: ${errorMessage}`,
          skipped: true,
          reasonCode: "TARGET_LOCKED",
          reason: "Target is locked",
          target: { repo: itemRepo, number: itemNumber },
        };
      }

      // For all other errors, propagate the failure
      core.error(`Failed to add comment: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = {
  main,
  // Export constants and functions for testing
  MAX_COMMENT_LENGTH,
  MAX_MENTIONS,
  MAX_LINKS,
  enforceCommentLimits,
  isDiscussionIntegrationAccessError,
};
