// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_discussion";

const { getTrackerID } = require("./get_tracker_id.cjs");
const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");
const { replaceTemporaryIdReferences } = require("./temporary_id.cjs");
const { parseAllowedRepos, getDefaultTargetRepo, validateRepo, parseRepoSlug } = require("./repo_helpers.cjs");
const { removeDuplicateTitleFromDescription } = require("./remove_duplicate_title.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { createExpirationLine, generateFooterWithExpiration } = require("./ephemerals.cjs");
const { generateWorkflowIdMarker } = require("./generate_footer.cjs");

/**
 * Fetch repository ID and discussion categories for a repository
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @returns {Promise<{repositoryId: string, discussionCategories: Array<{id: string, name: string, slug: string, description: string}>}|null>}
 */
async function fetchRepoDiscussionInfo(owner, repo) {
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
  const queryResult = await github.graphql(repositoryQuery, {
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

  // Fall back to first category if available
  if (categories.length > 0) {
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
 * Checks if an error is a permissions-related error
 * @param {string} errorMessage - The error message to check
 * @returns {boolean} True if the error is permissions-related
 */
function isPermissionsError(errorMessage) {
  return (
    errorMessage.includes("Resource not accessible") ||
    errorMessage.includes("Insufficient permissions") ||
    errorMessage.includes("Bad credentials") ||
    errorMessage.includes("Not Authenticated") ||
    errorMessage.includes("requires authentication") ||
    errorMessage.includes("Discussions not enabled") ||
    errorMessage.includes("Failed to fetch repository information")
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
    const fallbackNote = `\n\n---\n\n> **Note:** This was intended to be a discussion, but discussions could not be created due to permissions issues. This issue was created as a fallback.\n`;
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
  const allowedRepos = parseAllowedRepos(config.allowed_repos);
  const defaultTargetRepo = getDefaultTargetRepo(config);
  const titlePrefix = config.title_prefix || "";
  const configCategory = config.category || "";
  const maxCount = config.max || 10;
  const expiresHours = config.expires ? parseInt(String(config.expires), 10) : 0;
  const fallbackToIssue = config.fallback_to_issue !== false; // Default to true
  const closeOlderDiscussions = config.close_older_discussions === true || config.close_older_discussions === "true";

  // Parse labels from config
  const labelsConfig = config.labels || [];
  const labels = Array.isArray(labelsConfig)
    ? labelsConfig
    : String(labelsConfig)
        .split(",")
        .map(l => l.trim())
        .filter(l => l.length > 0);

  core.info(`Create discussion configuration: max=${maxCount}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (fallbackToIssue) {
    core.info("Fallback to issue enabled: will create an issue if discussion creation fails due to permissions");
  }
  if (closeOlderDiscussions) {
    core.info("Close older discussions enabled: will close older discussions/issues with same workflow-id marker");
  }

  // Track state
  let processedCount = 0;
  const repoInfoCache = new Map();
  const temporaryIdMap = new Map();

  // Initialize create_issue handler for fallback if enabled
  let createIssueHandler = null;
  if (fallbackToIssue) {
    const { main: createIssueMain } = require("./create_issue.cjs");
    createIssueHandler = await createIssueMain({
      ...config, // Pass through most config
      title_prefix: titlePrefix,
      max: maxCount,
      expires: expiresHours,
      // Map close_older_discussions to close_older_issues for fallback issues
      close_older_issues: closeOlderDiscussions,
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

    // Determine target repository
    const itemRepo = item.repo ? String(item.repo).trim() : defaultTargetRepo;

    // Validate repository
    const repoValidation = validateRepo(itemRepo, defaultTargetRepo, allowedRepos);
    if (!repoValidation.valid) {
      // When valid is false, error is guaranteed to be non-null
      const errorMessage = repoValidation.error;
      if (!errorMessage) {
        throw new Error("Internal error: repoValidation.error should not be null when valid is false");
      }
      core.warning(`Skipping discussion: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }

    // Use the qualified repo from validation (handles bare names like "gh-aw" -> "github/gh-aw")
    const qualifiedItemRepo = repoValidation.qualifiedRepo;

    // Parse repository slug
    const repoParts = parseRepoSlug(qualifiedItemRepo);
    if (!repoParts) {
      const error = `Invalid repository format '${itemRepo}'. Expected 'owner/repo'.`;
      core.warning(`Skipping discussion: ${error}`);
      return {
        success: false,
        error,
      };
    }

    // Get repository info (cached)
    let repoInfo = repoInfoCache.get(qualifiedItemRepo);
    if (!repoInfo) {
      try {
        const fetchedInfo = await fetchRepoDiscussionInfo(repoParts.owner, repoParts.repo);
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

    // Build title
    let title = item.title ? item.title.trim() : "";
    let processedBody = replaceTemporaryIdReferences(item.body || "", temporaryIdMap, qualifiedItemRepo);
    processedBody = removeDuplicateTitleFromDescription(title, processedBody);

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
    const runId = context.runId;
    const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
    const runUrl = context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;

    // Generate footer with expiration using helper
    const footer = generateFooterWithExpiration({
      footerText: `> AI generated by [${workflowName}](${runUrl})`,
      expiresHours,
      entityType: "Discussion",
    });

    bodyLines.push(``, ``, footer);

    // Add standalone workflow-id marker for searchability (consistent with comments)
    if (workflowId) {
      bodyLines.push(``, generateWorkflowIdMarker(workflowId));
    }

    bodyLines.push("");
    const body = bodyLines.join("\n").trim();

    core.info(`Creating discussion in ${qualifiedItemRepo} with title: ${title}`);

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

      const mutationResult = await github.graphql(createDiscussionMutation, {
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

      core.info(`Created discussion ${qualifiedItemRepo}#${discussion.number}: ${discussion.url}`);

      return {
        success: true,
        repo: qualifiedItemRepo,
        number: discussion.number,
        url: discussion.url,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);

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
