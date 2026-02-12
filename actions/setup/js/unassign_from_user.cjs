// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { processItems } = require("./safe_output_processor.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { resolveIssueNumber, extractAssignees } = require("./safe_output_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "unassign_from_user";

/**
 * Main handler factory for unassign_from_user
 * Returns a message handler function that processes individual unassign_from_user messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedAssignees = config.allowed || [];
  const maxCount = config.max || 10;

  // Resolve target repository configuration
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  core.info(`Unassign from user configuration: max=${maxCount}`);
  if (allowedAssignees.length > 0) {
    core.info(`Allowed assignees to unassign: ${allowedAssignees.join(", ")}`);
  }
  core.info(`Default target repository: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Additional allowed repositories: ${Array.from(allowedRepos).join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single unassign_from_user message
   * @param {Object} message - The unassign_from_user message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleUnassignFromUser(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping unassign_from_user: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const unassignItem = message;

    // Determine issue number using shared helper
    const issueResult = resolveIssueNumber(unassignItem);
    if (!issueResult.success) {
      core.warning(`Skipping unassign_from_user: ${issueResult.error}`);
      return {
        success: false,
        error: issueResult.error,
      };
    }
    const issueNumber = issueResult.issueNumber;

    // Extract assignees using shared helper
    const requestedAssignees = extractAssignees(unassignItem);

    core.info(`Requested assignees to unassign: ${JSON.stringify(requestedAssignees)}`);

    // Use shared helper to filter, sanitize, dedupe, and limit
    const uniqueAssignees = processItems(requestedAssignees, allowedAssignees, maxCount);

    if (uniqueAssignees.length === 0) {
      core.info("No assignees to remove");
      return {
        success: true,
        issueNumber: issueNumber,
        assigneesRemoved: [],
        message: "No valid assignees found",
      };
    }

    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(unassignItem, defaultTargetRepo, allowedRepos, "issue");

    if (!repoResult.success) {
      core.warning(`Repository validation failed: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }

    const repoParts = repoResult.repoParts;
    const targetRepo = repoResult.repo;

    core.info(`Unassigning ${uniqueAssignees.length} users from issue #${issueNumber} in ${targetRepo}: ${JSON.stringify(uniqueAssignees)}`);

    try {
      // Remove assignees from the issue
      await github.rest.issues.removeAssignees({
        owner: repoParts.owner,
        repo: repoParts.repo,
        issue_number: issueNumber,
        assignees: uniqueAssignees,
      });

      core.info(`Successfully unassigned ${uniqueAssignees.length} user(s) from issue #${issueNumber} in ${targetRepo}`);

      return {
        success: true,
        issueNumber: issueNumber,
        repo: targetRepo,
        assigneesRemoved: uniqueAssignees,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to unassign users: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
