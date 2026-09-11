// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { isDiscussionContext, getDiscussionNumber } = require("./update_context_helpers.cjs");
const { createUpdateHandlerFactory, createStandardFormatResult } = require("./update_handler_factory.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { validateLabels } = require("./safe_output_validator.cjs");
const { tryEnforceArrayLimit } = require("./limit_enforcement_helpers.cjs");
const { MAX_LABELS } = require("./constants.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { logGraphQLError, fetchAllRepoLabels } = require("./github_api_helpers.cjs");
const { resolveNumberFromTemporaryId } = require("./temporary_id.cjs");
const { getBodyFooterMessage } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/** @type {import('./github_api_helpers.cjs').GraphQLErrorHints} */
const DISCUSSION_GRAPHQL_HINTS = {
  insufficientScopesHint:
    "This looks like a token permission problem. The GitHub token requires 'discussions: write' permission. Add 'permissions: discussions: write' to your workflow, or set 'safe-outputs.update-discussion.github-token' to a PAT with the appropriate scopes.",
  notFoundHint: "GitHub returned NOT_FOUND for the discussion. Check that the discussion number is correct and that the token has read access to the repository.",
};

/**
 * Fetches label node IDs for the given label names from the repository
 * @param {any} githubClient - GitHub API client
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string[]} labelNames - Array of label names to fetch IDs for
 * @returns {Promise<string[]>} Array of label node IDs
 */
async function fetchLabelNodeIds(githubClient, owner, repo, labelNames) {
  if (!labelNames || labelNames.length === 0) {
    return [];
  }

  const allLabels = await fetchAllRepoLabels(githubClient, owner, repo);
  const labelMap = new Map(allLabels.map(/** @param {any} l */ l => [l.name.toLowerCase(), l.id]));

  const labelIds = [];
  const unmatched = [];
  for (const name of labelNames) {
    const id = labelMap.get(name.toLowerCase());
    if (id) {
      labelIds.push(id);
    } else {
      unmatched.push(name);
    }
  }

  if (unmatched.length > 0) {
    core.warning(`Could not find label IDs for: ${unmatched.join(", ")}. Ensure these labels exist in the repository.`);
  }

  return labelIds;
}

/**
 * Replaces all labels on a discussion using GraphQL
 * @param {any} githubClient - GitHub API client
 * @param {string} discussionId - Discussion node ID
 * @param {string[]} labelIds - Array of label node IDs to set
 * @returns {Promise<void>}
 */
async function replaceDiscussionLabels(githubClient, discussionId, labelIds) {
  // Fetch existing labels so we can remove them first
  const removeQuery = `
    query($id: ID!) {
      node(id: $id) {
        ... on Discussion {
          labels(first: 100) {
            nodes { id }
          }
        }
      }
    }
  `;
  const existing = await githubClient.graphql(removeQuery, { id: discussionId });
  const existingIds = existing?.node?.labels?.nodes?.map(/** @param {any} l */ l => l.id) || [];

  if (existingIds.length > 0) {
    const removeMutation = `
      mutation($labelableId: ID!, $labelIds: [ID!]!) {
        removeLabelsFromLabelable(input: { labelableId: $labelableId, labelIds: $labelIds }) {
          clientMutationId
        }
      }
    `;
    await githubClient.graphql(removeMutation, { labelableId: discussionId, labelIds: existingIds });
  }

  if (labelIds.length > 0) {
    const addMutation = `
      mutation($labelableId: ID!, $labelIds: [ID!]!) {
        addLabelsToLabelable(input: { labelableId: $labelableId, labelIds: $labelIds }) {
          clientMutationId
        }
      }
    `;
    await githubClient.graphql(addMutation, { labelableId: discussionId, labelIds });
  }
}

/**
 * Execute the discussion update API call using GraphQL
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub Actions context
 * @param {number} discussionNumber - Discussion number to update
 * @param {any} updateData - Data to update
 * @returns {Promise<any>} Updated discussion
 */
async function executeDiscussionUpdate(github, context, discussionNumber, updateData) {
  // First, fetch the discussion node ID
  const getDiscussionQuery = `
    query($owner: String!, $repo: String!, $number: Int!) {
      repository(owner: $owner, name: $repo) {
        discussion(number: $number) {
          id
          title
          body
          url
        }
      }
    }
  `;

  let queryResult;
  try {
    queryResult = await github.graphql(getDiscussionQuery, {
      owner: context.repo.owner,
      repo: context.repo.repo,
      number: discussionNumber,
    });
  } catch (err) {
    // prettier-ignore
    const fetchError = /** @type {any} */ (err);
    logGraphQLError(fetchError, `fetch discussion #${discussionNumber} from ${context.repo.owner}/${context.repo.repo}`, DISCUSSION_GRAPHQL_HINTS);
    throw fetchError;
  }

  const discussion = queryResult?.repository?.discussion;
  if (!discussion) {
    throw new Error(`${ERR_NOT_FOUND}: Discussion #${discussionNumber} not found`);
  }

  const hasTitleUpdate = updateData.title !== undefined;
  const hasBodyUpdate = updateData.body !== undefined;
  if (hasBodyUpdate) {
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
    const runUrl = buildWorkflowRunUrl(context, updateData._workflowRepo || context.repo);
    const bodyFooter = getBodyFooterMessage(updateData._bodyFooter, { workflowName, runUrl });
    if (bodyFooter) {
      updateData.body = updateData.body.trimEnd() + "\n\n" + bodyFooter.trimEnd();
    }
  }
  const hasLabelsUpdate = updateData.labels !== undefined;

  let updatedDiscussion = discussion;

  // Only call the updateDiscussion mutation when title or body actually needs updating.
  // Skipping this when only labels are being changed avoids accidentally modifying
  // the discussion body with stale or unexpected content.
  if (hasTitleUpdate || hasBodyUpdate) {
    const mutation = `
      mutation($discussionId: ID!, $title: String, $body: String) {
        updateDiscussion(input: { discussionId: $discussionId, title: $title, body: $body }) {
          discussion {
            id
            title
            body
            url
          }
        }
      }
    `;

    const variables = {
      discussionId: discussion.id,
      title: hasTitleUpdate ? updateData.title : discussion.title,
      body: hasBodyUpdate ? updateData.body : discussion.body,
    };

    try {
      const mutationResult = await github.graphql(mutation, variables);
      updatedDiscussion = mutationResult.updateDiscussion.discussion;
    } catch (err) {
      // prettier-ignore
      const mutationError = /** @type {any} */ (err);
      logGraphQLError(mutationError, `updateDiscussion mutation for discussion #${discussionNumber} in ${context.repo.owner}/${context.repo.repo}`, DISCUSSION_GRAPHQL_HINTS);
      throw mutationError;
    }
  }

  // Handle label replacement if labels were provided
  if (hasLabelsUpdate) {
    try {
      const labelIds = await fetchLabelNodeIds(github, context.repo.owner, context.repo.repo, updateData.labels);
      await replaceDiscussionLabels(github, discussion.id, labelIds);
      core.info(`Successfully replaced labels on discussion #${discussionNumber}`);
    } catch (error) {
      const context = hasTitleUpdate || hasBodyUpdate ? "title/body updated successfully but " : "";
      core.warning(`Discussion #${discussionNumber} ${context}label update failed: ${getErrorMessage(error)}`);
    }
  }

  return updatedDiscussion;
}

/**
 * Resolve discussion number from message and configuration
 * Discussions have special handling - they don't use the standard resolveTarget helper
 * @param {Object} item - The message item
 * @param {string} updateTarget - Target configuration
 * @param {Object} context - GitHub Actions context
 * @param {Object} [resolvedTemporaryIds] - Resolved temporary IDs map
 * @returns {{success: true, number: number} | {success: false, error: string}} Resolution result
 */
function resolveDiscussionNumber(item, updateTarget, context, resolvedTemporaryIds) {
  // Discussions are special - they have their own context type separate from issues/PRs
  // We need to handle them differently
  if (item.discussion_number !== undefined) {
    const resolution = resolveNumberFromTemporaryId(item.discussion_number, resolvedTemporaryIds);
    if (resolution.errorMessage) {
      return { success: false, error: resolution.errorMessage };
    }
    if (resolution.resolved === null) {
      return { success: false, error: resolution.errorMessage ?? "Failed to resolve discussion number" };
    }
    const discussionNumber = resolution.resolved;
    if (resolution.wasTemporaryId) {
      core.info(`Resolved temporary ID '${item.discussion_number}' to discussion #${discussionNumber}`);
    }
    return { success: true, number: discussionNumber };
  } else if (updateTarget !== "triggering") {
    // Explicit number target
    const discussionNumber = parseInt(updateTarget, 10);
    if (Number.isNaN(discussionNumber) || discussionNumber <= 0) {
      return {
        success: false,
        error: `Invalid discussion number in target: ${updateTarget}`,
      };
    }
    return { success: true, number: discussionNumber };
  } else {
    // Use triggering context (default)
    if (isDiscussionContext(context.eventName, context.payload)) {
      const discussionNumber = getDiscussionNumber(context.payload);
      if (!discussionNumber) {
        return {
          success: false,
          error: "No discussion number available",
        };
      }
      return { success: true, number: discussionNumber };
    } else {
      return {
        success: false,
        error: "Not in discussion context",
      };
    }
  }
}

/**
 * Build update data from message
 * @param {Object} item - The message item
 * @param {Object} config - Configuration object
 * @returns {{success: true, data: Object} | {success: false, error: string}} Update data result
 */
function buildDiscussionUpdateData(item, config) {
  const updateData = {};

  if (item.title !== undefined) {
    if (config.allow_title !== true) {
      return { success: false, error: "Title updates are not allowed by the safe-outputs configuration" };
    }
    // Sanitize title for Unicode security (no prefix handling needed for updates)
    updateData.title = sanitizeTitle(item.title);
  }
  if (item.body !== undefined) {
    if (config.allow_body !== true) {
      return { success: false, error: "Body updates are not allowed by the safe-outputs configuration" };
    }
    updateData.body = item.body;
  }

  // Handle labels update when allowed
  if (config.allow_labels === true && item.labels !== undefined) {
    if (!Array.isArray(item.labels)) {
      return { success: false, error: "Invalid labels value: must be an array" };
    }

    const limitResult = tryEnforceArrayLimit(item.labels, MAX_LABELS, "labels");
    if (!limitResult.success) {
      core.warning(`Discussion update label limit exceeded: ${limitResult.error}`);
      return { success: false, error: limitResult.error };
    }

    const allowedLabels = config.allowed_labels || [];
    const labelsResult = validateLabels(item.labels, allowedLabels.length > 0 ? allowedLabels : undefined, MAX_LABELS);
    if (!labelsResult.valid) {
      return { success: false, error: labelsResult.error ?? "Invalid labels" };
    }

    updateData.labels = labelsResult.value ?? [];
  }

  // Pass footer config to executeUpdate (default to true)
  updateData._includeFooter = parseBoolTemplatable(config.footer, true);
  updateData._bodyFooter = config.body_footer;

  return { success: true, data: updateData };
}

/**
 * Format success result for discussion update
 * Uses the standard format helper for consistency across update handlers
 */
const formatDiscussionSuccessResult = createStandardFormatResult({
  numberField: "number",
  urlField: "url",
  urlSource: "url",
});

/**
 * Main handler factory for update_discussion
 * Returns a message handler function that processes individual update_discussion messages
 * @type {HandlerFactoryFunction}
 */
const main = createUpdateHandlerFactory({
  itemType: "update_discussion",
  itemTypeName: "discussion",
  supportsPR: false,
  resolveItemNumber: resolveDiscussionNumber,
  buildUpdateData: buildDiscussionUpdateData,
  executeUpdate: executeDiscussionUpdate,
  formatSuccessResult: formatDiscussionSuccessResult,
});

module.exports = { main };
