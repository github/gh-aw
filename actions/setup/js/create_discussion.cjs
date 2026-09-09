// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_discussion";

const { getTrackerID } = require("./get_tracker_id.cjs");
const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");
const { generateTemporaryId, isTemporaryId, normalizeTemporaryId, getOrGenerateTemporaryId, replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { removeDuplicateTitleFromDescription } = require("./remove_duplicate_title.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { createExpirationLine, generateFooterWithExpiration, addExpirationToFooter } = require("./ephemerals.cjs");
const { assembleMarkdownBodyParts } = require("./markdown_body_helpers.cjs");
const { getBodyHeader, getDisclosureHeader } = require("./messages_header.cjs");
const { generateWorkflowIdMarker, generateWorkflowCallIdMarker, generateCloseKeyMarker, normalizeCloseOlderKey } = require("./generate_footer.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { sanitizeLabelContent } = require("./sanitize_label_content.cjs");
const { tryEnforceArrayLimit } = require("./limit_enforcement_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { closeOlderDiscussions: closeOlderDiscussionsFunc } = require("./close_older_discussions.cjs");
const { parseBoolTemplatable, parseIntTemplatable } = require("./templatable.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { generateHistoryLink, generateHistoryUrl } = require("./generate_history_link.cjs");
const { MAX_LABELS } = require("./constants.cjs");
const { fetchAllRepoLabels } = require("./github_api_helpers.cjs");
const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");

/**
 * Fetch repository ID and discussion categories for a repository
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @returns {Promise<{repositoryId: string, discussionCategories: Array<{id: string, name: string, slug: string, description: string}>}|null>}
 */
async function fetchRepoDiscussionInfo(githubClient, owner, repo) {
  const repositoryQuery = `
    query($owner: String!, $repo: String!) {
      repository(owner: $owner, name: $repo) {
        id
        discussionCategories(first: 20) {
          nodes {
            id
            name
            slug
            description
          }
        }
      }
    }
  `;
  const queryResult = await githubClient.graphql(repositoryQuery, {
    owner: owner,
    repo: repo,
  });
  if (!queryResult || !queryResult.repository) {
    return null;
  }
  return {
    repositoryId: queryResult.repository.id,
    discussionCategories: queryResult.repository.discussionCategories.nodes || [],
  };
}

/**
 * Resolve category ID for a repository
 * @param {string} categoryConfig - Category ID, name, or slug from config
 * @param {string} itemCategory - Category from agent output item (optional)
 * @param {Array<{id: string, name: string, slug: string}>} categories - Available categories
 * @returns {{id: string, matchType: string, name: string, requestedCategory?: string}|undefined} Resolved category info
 */
function resolveCategoryId(categoryConfig, itemCategory, categories) {
  // Use item category if provided, otherwise use config
  const categoryToMatch = itemCategory || categoryConfig;

  if (categoryToMatch) {
    // Try to match against category IDs first (exact match, case-sensitive)
    const categoryById = categories.find(cat => cat.id === categoryToMatch);
    if (categoryById) {
      return { id: categoryById.id, matchType: "id", name: categoryById.name };
    }

    // Normalize the category to match for case-insensitive comparison
    const normalizedCategoryToMatch = categoryToMatch.toLowerCase();

    // Try to match against category names (case-insensitive)
    const categoryByName = categories.find(cat => cat.name.toLowerCase() === normalizedCategoryToMatch);
    if (categoryByName) {
      return { id: categoryByName.id, matchType: "name", name: categoryByName.name };
    }
    // Try to match against category slugs (routes, case-insensitive)
    const categoryBySlug = categories.find(cat => cat.slug.toLowerCase() === normalizedCategoryToMatch);
    if (categoryBySlug) {
      return { id: categoryBySlug.id, matchType: "slug", name: categoryBySlug.name };
    }
  }

  // Fall back to "Announcements" category if available, otherwise first category
  if (categories.length > 0) {
    // Try to find an "Announcements" category (case-insensitive)
    const announcementCategory = categories.find(cat => cat.name.toLowerCase() === "announcements" || cat.slug.toLowerCase() === "announcements");

    if (announcementCategory) {
      return {
        id: announcementCategory.id,
        matchType: "fallback-announcement",
        name: announcementCategory.name,
        requestedCategory: categoryToMatch,
      };
    }

    // Otherwise use first category
    return {
      id: categories[0].id,
      matchType: "fallback",
      name: categories[0].name,
      requestedCategory: categoryToMatch,
    };
  }

  return undefined;
}

/**
 * Fetches label node IDs for the given label names
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string[]} labelNames - Array of label names to fetch IDs for
 * @returns {Promise<Array<{name: string, id: string}>>} Array of label objects with name and ID
 */
async function fetchLabelIds(githubClient, owner, repo, labelNames) {
  if (!labelNames || labelNames.length === 0) {
    return [];
  }

  try {
    const allLabels = await fetchAllRepoLabels(githubClient, owner, repo);
    const labelMap = new Map(allLabels.map(label => [label.name.toLowerCase(), label]));

    // Match requested labels (case-insensitive)
    const matchedLabels = [];
    const unmatchedLabels = [];

    for (const requestedLabel of labelNames) {
      const normalizedName = requestedLabel.toLowerCase();
      const matchedLabel = labelMap.get(normalizedName);
      if (matchedLabel) {
        matchedLabels.push({ name: matchedLabel.name, id: matchedLabel.id });
      } else {
        unmatchedLabels.push(requestedLabel);
      }
    }

    if (unmatchedLabels.length > 0) {
      core.warning(`Could not find label IDs for: ${unmatchedLabels.join(", ")}`);
      const MAX_DISPLAY = 20;
      const displayedLabels = allLabels.slice(0, MAX_DISPLAY).map(l => l.name);
      const truncationNote = allLabels.length > MAX_DISPLAY ? ` … (${allLabels.length} total)` : "";
      core.info(`These labels may not exist in the repository. Available labels: ${displayedLabels.join(", ")}${truncationNote}`);
    }

    return matchedLabels;
  } catch (error) {
    core.warning(`Failed to fetch label IDs: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Applies labels to a discussion using GraphQL
 * @param {string} discussionId - Discussion node ID
 * @param {string[]} labelIds - Array of label node IDs to add
 * @returns {Promise<boolean>} True if labels were applied successfully
 */
async function applyLabelsToDiscussion(githubClient, discussionId, labelIds) {
  if (!labelIds || labelIds.length === 0) {
    return true; // Nothing to do
  }

  try {
    const addLabelsMutation = `
      mutation($labelableId: ID!, $labelIds: [ID!]!) {
        addLabelsToLabelable(input: {
          labelableId: $labelableId,
          labelIds: $labelIds
        }) {
          labelable {
            ... on Discussion {
              id
              labels(first: 10) {
                nodes {
                  name
                }
              }
            }
          }
        }
      }
    `;

    const mutationResult = await githubClient.graphql(addLabelsMutation, {
      labelableId: discussionId,
      labelIds: labelIds,
    });

    const appliedLabels = mutationResult?.addLabelsToLabelable?.labelable?.labels?.nodes || [];
    core.info(`Successfully applied ${appliedLabels.length} labels to discussion`);
    return true;
  } catch (error) {
    core.warning(`Failed to apply labels to discussion: ${getErrorMessage(error)}`);
    return false;
  }
}

/**
 * Checks if an error is a permissions-related error
 * @param {string} errorMessage - The error message to check
 * @returns {boolean} True if the error is permissions-related
 */
function isPermissionsError(errorMessage) {
  const msg = errorMessage.toLowerCase();
  return (
    msg.includes("resource not accessible") ||
    msg.includes("insufficient permissions") ||
    msg.includes("bad credentials") ||
    msg.includes("not authenticated") ||
    msg.includes("requires authentication") ||
    msg.includes("discussions not enabled") ||
    msg.includes("failed to fetch repository information")
  );
}

/**
 * Handles fallback to create-issue when discussion creation fails
 * @param {Function} createIssueHandler - The create_issue handler function
 * @param {Object} item - The original discussion message item
 * @param {string} qualifiedItemRepo - The qualified repository name (owner/repo)
 * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
 * @param {string} contextMessage - Context-specific error message prefix
 * @returns {Promise<Object>} Result with success/error status
 */
async function handleFallbackToIssue(createIssueHandler, item, qualifiedItemRepo, resolvedTemporaryIds, contextMessage) {
  try {
    // Prepare issue message with a note about the fallback
    const fallbackNote = `\n\n---\n\n> [!WARNING]\n> This was intended to be a discussion, but discussions could not be created due to permissions issues. This issue was created as a fallback.\n> To enable discussions, ensure the repository has an Announcements category (announcement-capable) configured.\n`;
    const issueMessage = {
      ...item,
      body: (item.body || "") + fallbackNote,
      repo: qualifiedItemRepo,
    };

    // Call the create_issue handler
    const issueResult = await createIssueHandler(issueMessage, resolvedTemporaryIds);

    if (issueResult.success) {
      core.info(`✓ Successfully created issue ${issueResult.repo}#${issueResult.number} as fallback`);
      return {
        success: true,
        repo: issueResult.repo,
        number: issueResult.number,
        url: issueResult.url,
        fallback: "issue", // Indicate this was a fallback
      };
    } else {
      core.error(`Fallback to create-issue also failed: ${issueResult.error}`);
      return {
        success: false,
        error: `${contextMessage} and fallback to issue also failed: ${issueResult.error}`,
      };
    }
  } catch (fallbackError) {
    const fallbackErrorMessage = getErrorMessage(fallbackError);
    core.error(`Fallback to create-issue failed: ${fallbackErrorMessage}`);
    return {
      success: false,
      error: `${contextMessage} and fallback to issue threw an error: ${fallbackErrorMessage}`,
    };
  }
}

/**
 * Main handler factory for create_discussion
 * Returns a message handler function that processes individual create_discussion messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const titlePrefix = config.title_prefix || "";
  const configCategory = config.category || "";
  const maxCount = config.max || 10;
  const expiresHours = config.expires ? parseInt(String(config.expires), 10) : 0;
  const minBodyLength = config.min_body_length ? parseInt(String(config.min_body_length), 10) : 0;
  const fallbackToIssue = config.fallback_to_issue !== false; // Default to true
  const closeOlderDiscussionsEnabled = parseBoolTemplatable(config.close_older_discussions, false);
  const rawCloseOlderKey = config.close_older_key ? String(config.close_older_key) : "";
  const closeOlderKey = rawCloseOlderKey ? normalizeCloseOlderKey(rawCloseOlderKey) : "";
  if (rawCloseOlderKey && !closeOlderKey) {
    throw new Error(`${ERR_VALIDATION}: close-older-key "${rawCloseOlderKey}" is invalid: it must contain at least one alphanumeric character after normalization`);
  }
  if (Number.isNaN(minBodyLength) || minBodyLength < 0) {
    throw new Error(`${ERR_VALIDATION}: min_body_length must be a non-negative integer (got: ${config.min_body_length})`);
  }
  const includeFooter = parseBoolTemplatable(config.footer, true);

  // Create an authenticated GitHub client. Uses config["github-token"] when set
  // (for cross-repository operations), otherwise falls back to the step-level github.
  const githubClient = await createAuthenticatedGitHubClient(config);
  const maxMentions = parseIntTemplatable(config.mentions?.max, 50);
  let allowedMentionAliases = [];
  if (Array.isArray(config.allowedMentionAliases)) {
    allowedMentionAliases = config.allowedMentionAliases;
  } else if (config.mentions != null) {
    allowedMentionAliases = await resolveAllowedMentionsFromPayload(context, githubClient, core, config.mentions);
  }

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  // Parse labels from config
  const labelsConfig = config.labels || [];
  const labels = Array.isArray(labelsConfig)
    ? labelsConfig
    : String(labelsConfig)
        .split(",")
        .map(l => l.trim())
        .filter(l => l.length > 0);

  core.info(`Create discussion configuration: max=${maxCount}`);
  if (minBodyLength > 0) {
    core.info(`Minimum discussion body length guard enabled: ${minBodyLength}`);
  }
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (fallbackToIssue) {
    core.info("Fallback to issue enabled: will create an issue if discussion creation fails due to permissions");
  }
  if (closeOlderDiscussionsEnabled) {
    core.info("Close older discussions enabled: will close older discussions/issues with same workflow-id marker");
    if (closeOlderKey) {
      core.info(`  Using explicit close-older-key: "${closeOlderKey}"`);
    }
  }

  // Track state
  let processedCount = 0;
  const repoInfoCache = new Map();
  const temporaryIdMap = new Map();

  // Initialize create_issue handler for fallback if enabled
  /** @type {any} */
  let createIssueHandler = null;
  if (fallbackToIssue) {
    const { main: createIssueMain } = require("./create_issue.cjs");
    createIssueHandler = await createIssueMain({
      ...config, // Pass through most config
      title_prefix: titlePrefix,
      max: maxCount,
      expires: expiresHours,
      // Map close_older_discussions to close_older_issues for fallback issues
      close_older_issues: closeOlderDiscussionsEnabled,
      close_older_key: closeOlderKey,
    });
  }

  /**
   * Message handler function that processes a single create_discussion message
   * @param {Object} message - The create_discussion message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleCreateDiscussion(message, resolvedTemporaryIds) {
    // Check max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_discussion: max count of ${maxCount} reached`);
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
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "discussion");
    if (!repoResult.success) {
      core.warning(`Skipping discussion: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: qualifiedItemRepo, repoParts } = repoResult;

    // Get repository info (cached)
    let repoInfo = repoInfoCache.get(qualifiedItemRepo);
    if (!repoInfo) {
      try {
        const fetchedInfo = await fetchRepoDiscussionInfo(githubClient, repoParts.owner, repoParts.repo);
        if (!fetchedInfo) {
          const error = `Failed to fetch repository information for '${qualifiedItemRepo}'`;
          core.warning(error);
          return {
            success: false,
            error,
          };
        }
        repoInfo = fetchedInfo;
        repoInfoCache.set(qualifiedItemRepo, repoInfo);
        core.info(`Fetched discussion categories for ${qualifiedItemRepo}`);
      } catch (error) {
        const errorMessage = getErrorMessage(error);

        // Check if this is a permissions error and fallback is enabled
        if (fallbackToIssue && createIssueHandler && isPermissionsError(errorMessage)) {
          core.warning(`Failed to fetch discussion info due to permissions: ${errorMessage}`);
          core.info(`Falling back to create-issue for ${qualifiedItemRepo}`);

          return await handleFallbackToIssue(createIssueHandler, item, qualifiedItemRepo, resolvedTemporaryIds, "Failed to fetch discussion info");
        }

        // No fallback or not a permissions error - return original error
        // Provide enhanced error message with troubleshooting hints
        const enhancedError =
          `Failed to fetch repository information for '${qualifiedItemRepo}': ${errorMessage}. ` +
          `This may indicate that discussions are not enabled for this repository. ` +
          `Please verify that discussions are enabled in the repository settings at https://github.com/${qualifiedItemRepo}/settings.`;
        core.error(enhancedError);
        return {
          success: false,
          error: enhancedError,
        };
      }
    }

    // Resolve category
    const resolvedCategory = resolveCategoryId(configCategory, item.category, repoInfo.discussionCategories);
    if (!resolvedCategory) {
      const error = `No discussion categories available in ${qualifiedItemRepo}`;
      core.error(error);
      return {
        success: false,
        error,
      };
    }

    const categoryId = resolvedCategory.id;
    core.info(`Using category: ${resolvedCategory.name} (${resolvedCategory.matchType})`);

    // Get or generate the temporary ID for this discussion
    const tempIdResult = getOrGenerateTemporaryId(message, "discussion");
    if (tempIdResult.error) {
      core.warning(`Skipping discussion: ${tempIdResult.error}`);
      return {
        success: false,
        error: tempIdResult.error,
      };
    }
    // At this point, temporaryId is guaranteed to be a string (not null)
    const temporaryId = /** @type {string} */ tempIdResult.temporaryId;
    core.info(`Processing create_discussion: title=${message.title}, bodyLength=${message.body?.length ?? 0}, temporaryId=${temporaryId}, repo=${qualifiedItemRepo}`);

    // Build labels array (merge config labels with item-specific labels)
    const discussionLabels = [...labels, ...(Array.isArray(item.labels) ? item.labels : [])]
      .filter(Boolean)
      .map(label => String(label).trim())
      .filter(Boolean)
      .map(label => sanitizeLabelContent(label))
      .filter(Boolean)
      .map(label => (label.length > 64 ? label.substring(0, 64) : label))
      .filter((label, index, arr) => arr.indexOf(label) === index);

    // Enforce max limits on labels before API calls
    const limitResult = tryEnforceArrayLimit(discussionLabels, MAX_LABELS, "labels");
    if (!limitResult.success) {
      core.warning(`Discussion limit exceeded: ${limitResult.error}`);
      return { success: false, error: limitResult.error };
    }

    // Build title
    let title = item.title ? item.title.trim() : "";
    let processedBody = replaceTemporaryIdReferences(item.body || "", temporaryIdMap, qualifiedItemRepo);
    processedBody = removeDuplicateTitleFromDescription(title, processedBody);
    const preSanitizeBodyLength = processedBody.trim().length;

    // Sanitize body content to neutralize @mentions, URLs, and other security risks
    processedBody = sanitizeContent(processedBody, { allowedAliases: allowedMentionAliases, maxMentions });
    if (minBodyLength > 0 && preSanitizeBodyLength < minBodyLength) {
      const error = `Discussion body length ${preSanitizeBodyLength} is below configured minimum ${minBodyLength}`;
      core.error(error);
      return {
        success: false,
        error,
      };
    }

    if (!title) {
      title = item.body || "Discussion";
    }

    // Sanitize title for Unicode security and remove any duplicate prefixes
    title = sanitizeTitle(title, titlePrefix);

    // Apply title prefix (only if it doesn't already exist)
    title = applyTitlePrefix(title, titlePrefix);

    // Build body
    let bodyLines = processedBody.split("\n");

    // Add tracker ID
    const trackerIDComment = getTrackerID("markdown");
    if (trackerIDComment) {
      bodyLines.push(trackerIDComment);
    }

    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
    // GH_AW_CALLER_WORKFLOW_ID is set at compile time to `github.repository/<workflow-id>`.
    // When multiple workflows call the same reusable workflow via workflow_call they all
    // share the same GH_AW_WORKFLOW_ID. We embed a separate gh-aw-workflow-call-id marker
    // with the caller's identity so close-older-discussions can distinguish callers precisely.
    const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";
    const runUrl = buildWorkflowRunUrl(context, context.repo);

    // Inject body header before user content (unshifted first, so caution will appear before it)
    const bodyHeader = getBodyHeader({ workflowName, runUrl });
    if (bodyHeader) {
      bodyLines.unshift(...bodyHeader.split("\n"), "");
    }

    // Inject disclosure header (this runs after body-header, but appears before it because unshift prepends)
    const disclosureHeader = getDisclosureHeader({ workflowName, runUrl });
    if (disclosureHeader) {
      bodyLines.unshift(...disclosureHeader.split("\n"), "");
    }

    const triggeringIssueNumber = context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;
    const triggeringPRNumber = context.payload?.pull_request?.number || (context.payload?.issue?.pull_request ? context.payload.issue.number : undefined);
    const triggeringDiscussionNumber = context.payload?.discussion?.number;
    const historyUrl = includeFooter
      ? (generateHistoryUrl({
          owner: repoParts.owner,
          repo: repoParts.repo,
          itemType: "discussion",
          workflowCallId: callerWorkflowId,
          workflowId,
          serverUrl: context.serverUrl,
        }) ?? undefined)
      : undefined;
    const markdownParts = assembleMarkdownBodyParts({
      includeFooter,
      workflowName,
      runUrl,
      workflowSource: process.env.GH_AW_WORKFLOW_SOURCE ?? "",
      workflowSourceURL: process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "",
      triggeringIssueNumber,
      triggeringPRNumber,
      triggeringDiscussionNumber,
      historyUrl,
    });

    // Inject CAUTION at top of body if threat detection warning was raised
    // (unshifted after header so it appears first in the final output)
    const detectionCaution = markdownParts.detectionCaution;
    if (detectionCaution) {
      bodyLines.unshift(...detectionCaution.split("\n"), "");
    }

    // Generate footer with expiration using helper
    if (includeFooter) {
      const footer = addExpirationToFooter(markdownParts.footer, expiresHours, "Discussion");
      bodyLines.push(``, footer);
    }

    // Add standalone workflow-id marker for searchability (consistent with comments)
    // Always add XML markers even when footer is disabled
    if (workflowId) {
      bodyLines.push(``, generateWorkflowIdMarker(workflowId));
    }
    // Add workflow-call-id marker when available to allow close-older-discussions to
    // distinguish callers that share the same reusable workflow (and GH_AW_WORKFLOW_ID)
    if (callerWorkflowId) {
      bodyLines.push(generateWorkflowCallIdMarker(callerWorkflowId));
    }
    // Add explicit close-key marker when a custom deduplication key is provided
    if (closeOlderKey) {
      bodyLines.push(generateCloseKeyMarker(closeOlderKey));
    }

    bodyLines.push("");
    const body = bodyLines.join("\n").trim();

    core.info(`Creating discussion in ${qualifiedItemRepo} with title: ${title}`);

    // If in staged mode, preview the discussion without creating it
    if (isStaged) {
      logStagedPreviewInfo(`Would create discussion in ${qualifiedItemRepo}`);
      return {
        success: true,
        staged: true,
        previewInfo: {
          repo: qualifiedItemRepo,
          title,
          bodyLength: body.length,
          temporaryId,
        },
      };
    }

    // Track whether the discussion was successfully created to guard against
    // double-posting: if the discussion is created but a subsequent operation
    // (e.g., close-older-discussions search) unexpectedly escapes its own
    // try-catch and reaches the outer catch, we must NOT fall back to creating
    // an issue — the discussion already exists.
    /** @type {any} */
    let createdDiscussion = null;

    try {
      const createDiscussionMutation = `
        mutation($repositoryId: ID!, $categoryId: ID!, $title: String!, $body: String!) {
          createDiscussion(input: {
            repositoryId: $repositoryId,
            categoryId: $categoryId,
            title: $title,
            body: $body
          }) {
            discussion {
              id
              number
              title
              url
            }
          }
        }
      `;

      const mutationResult = await githubClient.graphql(createDiscussionMutation, {
        repositoryId: repoInfo.repositoryId,
        categoryId: categoryId,
        title: title,
        body: body,
      });

      const discussion = mutationResult.createDiscussion.discussion;
      if (!discussion) {
        const error = "No discussion data returned";
        core.error(error);
        return {
          success: false,
          error,
        };
      }

      // Mark the discussion as created so the outer catch won't trigger a
      // fallback issue even if a post-creation operation fails unexpectedly.
      createdDiscussion = discussion;
      core.info(`Created discussion ${qualifiedItemRepo}#${discussion.number}: ${discussion.url}`);

      // Close older discussions if enabled
      if (closeOlderDiscussionsEnabled) {
        if (workflowId || closeOlderKey) {
          const searchKey = closeOlderKey ? `close-older-key: ${closeOlderKey}` : `workflow-id: ${workflowId}`;
          core.info(`Attempting to close older discussions for ${qualifiedItemRepo}#${discussion.number} using ${searchKey}`);
          try {
            const closedDiscussions = await closeOlderDiscussionsFunc(
              github,
              repoParts.owner,
              repoParts.repo,
              workflowId,
              categoryId || undefined,
              { number: discussion.number, url: discussion.url },
              workflowName,
              runUrl,
              callerWorkflowId,
              closeOlderKey
            );
            if (closedDiscussions.length > 0) {
              core.info(`Closed ${closedDiscussions.length} older discussion(s)`);
            }
          } catch (error) {
            // Log error but don't fail the workflow
            core.warning(`Failed to close older discussions: ${getErrorMessage(error)}`);
          }
        } else {
          core.warning("Close older discussions enabled but GH_AW_WORKFLOW_ID environment variable not set - skipping");
        }
      }

      // Apply labels if configured
      if (discussionLabels.length > 0) {
        core.info(`Applying ${discussionLabels.length} labels to discussion: ${discussionLabels.join(", ")}`);
        const labelIdsData = await fetchLabelIds(githubClient, repoParts.owner, repoParts.repo, discussionLabels);
        if (labelIdsData.length > 0) {
          const labelIds = labelIdsData.map(l => l.id);
          const labelsApplied = await applyLabelsToDiscussion(githubClient, discussion.id, labelIds);
          if (labelsApplied) {
            core.info(`✓ Applied labels: ${labelIdsData.map(l => l.name).join(", ")}`);
          }
        } else if (discussionLabels.length > 0) {
          core.warning(`⚠ No matching labels found in repository for: ${discussionLabels.join(", ")}`);
        }
      }

      return {
        success: true,
        repo: qualifiedItemRepo,
        number: discussion.number,
        url: discussion.url,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);

      // Guard against double-posting: detect cases where the discussion was
      // already persisted even though an error was thrown.
      //
      // Two scenarios can cause this:
      //
      // 1. A post-creation operation (close-older-discussions, label application)
      //    unexpectedly escaped its inner try-catch and reached this outer catch
      //    after `createdDiscussion` was set.
      //
      // 2. @octokit/graphql threw a GraphqlResponseError that contains BOTH the
      //    created discussion (partial success) AND errors — for example, when
      //    GitHub returns `{"data": {"createDiscussion": {...}}, "errors": [...]}`.
      //    In that case `createdDiscussion` is still null but the discussion was
      //    already persisted in GitHub's database.
      //
      // In either case we must NOT fall back to creating an issue, as that would
      // result in both a discussion and a fallback issue existing at the same time.
      // prettier-ignore
      const errorAny = /** @type {any} */ (error);
      /** @type {{id: string, number: number, title: string, url: string} | null | undefined} */
      const partialDiscussion = errorAny?.data?.createDiscussion?.discussion;
      const resolvedDiscussion = createdDiscussion || partialDiscussion;
      if (resolvedDiscussion) {
        core.warning(`Discussion ${qualifiedItemRepo}#${resolvedDiscussion.number} was created but a post-creation operation failed: ${errorMessage}`);
        return {
          success: true,
          repo: qualifiedItemRepo,
          number: resolvedDiscussion.number,
          url: resolvedDiscussion.url,
        };
      }

      // Check if this is a permissions error and fallback is enabled
      if (fallbackToIssue && createIssueHandler && isPermissionsError(errorMessage)) {
        core.warning(`Discussion creation failed due to permissions: ${errorMessage}`);
        core.info(`Falling back to create-issue for ${qualifiedItemRepo}`);

        return await handleFallbackToIssue(createIssueHandler, item, qualifiedItemRepo, resolvedTemporaryIds, "Discussion creation failed");
      }

      // No fallback or not a permissions error - return original error
      // Provide enhanced error message with troubleshooting hints
      const enhancedError =
        `Failed to create discussion in '${qualifiedItemRepo}': ${errorMessage}. ` +
        `Common causes: (1) Discussions not enabled in repository settings, ` +
        `(2) Invalid category ID, or (3) Insufficient permissions. ` +
        `Verify discussions are enabled at https://github.com/${qualifiedItemRepo}/settings and check the category configuration.`;
      core.error(enhancedError);
      return {
        success: false,
        error: enhancedError,
      };
    }
  };
}

module.exports = { main };
