// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { processItems } = require("./safe_output_processor.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { resolveTarget, extractAssignees, checkRequiredFilter } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { createCountGatedHandler } = require("./handler_scaffold.cjs");
const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "assign_to_user";

/**
 * Main handler factory for assign_to_user
 * Uses shared count-gated scaffold for max-limit enforcement.
 * @type {HandlerFactoryFunction}
 */
const main = createCountGatedHandler({
  handlerType: HANDLER_TYPE,
  setup: async (config, maxCount, isStaged) => {
    // Extract configuration
    const allowedAssignees = config.allowed ?? [];
    const blockedAssignees = config.blocked ?? [];
    const targetConfig = config.target || "triggering";
    const unassignFirst = parseBoolTemplatable(config.unassign_first, false);
    const issueIntentEnabled = config.issue_intent !== false;
    const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
    const githubClient = await createAuthenticatedGitHubClient(config);
    const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
    const requiredTitlePrefix = config.required_title_prefix || "";
    if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
    if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

    core.info(`Assign to user configuration: max=${maxCount}, unassign_first=${unassignFirst}, issue_intent=${issueIntentEnabled}`);
    if (allowedAssignees.length > 0) {
      core.info(`Allowed assignees: ${allowedAssignees.join(", ")}`);
    }
    if (blockedAssignees.length > 0) {
      core.info(`Blocked assignees: ${blockedAssignees.join(", ")}`);
    }
    core.info(`Default target repo: ${defaultTargetRepo}`);
    if (allowedRepos.size > 0) {
      core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
    }

    /**
     * Message handler function that processes a single assign_to_user message
     * @param {Object} message - The assign_to_user message to process
     * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
     * @returns {Promise<Object>} Result with success/error status
     */
    return async function handleAssignToUser(message, resolvedTemporaryIds) {
      // Resolve and validate target repository
      const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "assignee");
      if (!repoResult.success) {
        core.warning(`Skipping assign_to_user: ${repoResult.error}`);
        return {
          success: false,
          error: repoResult.error,
        };
      }
      const { repo: itemRepo, repoParts } = repoResult;
      core.info(`Target repository: ${itemRepo}`);

      const assignItem = message;
      const intentMetadata = issueIntentEnabled ? normalizeIssueIntentMetadata(assignItem) : {};

      const targetResult = resolveTarget({
        targetConfig,
        item: assignItem,
        context,
        itemType: HANDLER_TYPE,
        supportsIssue: true,
      });
      if (!targetResult.success) {
        core.warning(`Skipping assign_to_user: ${targetResult.error}`);
        return {
          success: false,
          error: targetResult.error,
        };
      }
      const issueNumber = targetResult.number;

      const filterResult = await checkRequiredFilter(githubClient, repoParts, issueNumber, requiredLabels, requiredTitlePrefix, HANDLER_TYPE);
      if (filterResult) return filterResult;

      // Extract assignees using shared helper
      const requestedAssignees = extractAssignees(assignItem);

      core.info(`Requested assignees: ${JSON.stringify(requestedAssignees)}`);

      // Use shared helper to filter, sanitize, dedupe, and limit
      const uniqueAssignees = processItems(requestedAssignees, allowedAssignees, maxCount, blockedAssignees);

      if (uniqueAssignees.length === 0) {
        core.info("No assignees to add");
        return {
          success: true,
          issueNumber: issueNumber,
          assigneesAdded: [],
          message: "No valid assignees found",
        };
      }

      core.info(`Assigning ${uniqueAssignees.length} users to issue #${issueNumber} in ${itemRepo}: ${JSON.stringify(uniqueAssignees)}`);

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would assign users to issue #${issueNumber} in ${itemRepo}`);
        if (unassignFirst) {
          logStagedPreviewInfo(`Would unassign all current assignees first`);
        }
        return {
          success: true,
          staged: true,
          previewInfo: {
            issueNumber,
            repo: itemRepo,
            assignees: uniqueAssignees,
            unassignFirst,
          },
        };
      }

      try {
        // If unassign_first is enabled, get current assignees and remove them first
        if (unassignFirst) {
          core.info(`Fetching current assignees for issue #${issueNumber} to unassign them first`);
          const issue = await githubClient.rest.issues.get({
            owner: repoParts.owner,
            repo: repoParts.repo,
            issue_number: issueNumber,
          });

          const currentAssignees = issue.data.assignees?.map(a => a.login) || [];
          if (currentAssignees.length > 0) {
            core.info(`Unassigning ${currentAssignees.length} current assignee(s): ${JSON.stringify(currentAssignees)}`);
            await githubClient.rest.issues.removeAssignees({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: issueNumber,
              assignees: currentAssignees,
            });
            core.info(`Successfully unassigned current assignees`);
          } else {
            core.info(`No current assignees to unassign`);
          }
        }

        // Add assignees to the issue
        const addAssigneesParams = {
          owner: repoParts.owner,
          repo: repoParts.repo,
          issue_number: issueNumber,
          assignees: uniqueAssignees,
        };
        if (issueIntentEnabled && Object.keys(intentMetadata).length > 0) {
          Object.assign(addAssigneesParams, intentMetadata);
        }
        await githubClient.rest.issues.addAssignees(addAssigneesParams);

        core.info(`Successfully assigned ${uniqueAssignees.length} user(s) to issue #${issueNumber} in ${itemRepo}`);

        return {
          success: true,
          issueNumber: issueNumber,
          assigneesAdded: uniqueAssignees,
        };
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to assign users: ${errorMessage}`);
        return {
          success: false,
          error: errorMessage,
        };
      }
    };
  },
});

module.exports = { main };
