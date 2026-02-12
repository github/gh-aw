// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { processItems } = require("./safe_output_processor.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "assign_to_user";

/**
 * Main handler factory for assign_to_user
 * Returns a message handler function that processes individual assign_to_user messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedAssignees = config.allowed || [];
  const maxCount = config.max || 10;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);

  core.info(`Assign to user configuration: max=${maxCount}`);
  if (allowedAssignees.length > 0) {
    core.info(`Allowed assignees: ${allowedAssignees.join(", ")}`);
  }
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single assign_to_user message
   * @param {Object} message - The assign_to_user message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleAssignToUser(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping assign_to_user: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

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

    // Determine issue number
    let issueNumber;
    if (assignItem.issue_number !== undefined) {
      issueNumber = parseInt(String(assignItem.issue_number), 10);
      if (isNaN(issueNumber)) {
        core.warning(`Invalid issue_number: ${assignItem.issue_number}`);
        return {
          success: false,
          error: `Invalid issue_number: ${assignItem.issue_number}`,
        };
      }
    } else {
      // Use context issue if available
      const contextIssue = context.payload?.issue?.number;
      if (!contextIssue) {
        core.warning("No issue_number provided and not in issue context");
        return {
          success: false,
          error: "No issue number available",
        };
      }
      issueNumber = contextIssue;
    }

    // Support both singular "assignee" and plural "assignees" for flexibility
    let requestedAssignees = [];
    if (assignItem.assignees && Array.isArray(assignItem.assignees)) {
      requestedAssignees = assignItem.assignees;
    } else if (assignItem.assignee) {
      requestedAssignees = [assignItem.assignee];
    }

    core.info(`Requested assignees: ${JSON.stringify(requestedAssignees)}`);

    // Use shared helper to filter, sanitize, dedupe, and limit
    const uniqueAssignees = processItems(requestedAssignees, allowedAssignees, maxCount);

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

    try {
      // Add assignees to the issue
      await github.rest.issues.addAssignees({
        owner: repoParts.owner,
        repo: repoParts.repo,
        issue_number: issueNumber,
        assignees: uniqueAssignees,
      });

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
}

module.exports = { main };
