// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "assign_milestone";

/**
 * Main handler factory for assign_milestone
 * Returns a message handler function that processes individual assign_milestone messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedMilestones = config.allowed || [];
  const maxCount = config.max || 10;

  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  core.info(`Assign milestone configuration: max=${maxCount}`);
  if (allowedMilestones.length > 0) {
    core.info(`Allowed milestones: ${allowedMilestones.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Cache milestones to avoid fetching multiple times
  let allMilestones = null;

  /**
   * Message handler function that processes a single assign_milestone message
   * @param {Object} message - The assign_milestone message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleAssignMilestone(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping assign_milestone: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    const issueNumber = Number(item.issue_number);
    const milestoneNumber = Number(item.milestone_number);

    if (isNaN(issueNumber) || issueNumber <= 0) {
      core.error(`Invalid issue_number: ${item.issue_number}`);
      return {
        success: false,
        error: `Invalid issue_number: ${item.issue_number}`,
      };
    }

    if (isNaN(milestoneNumber) || milestoneNumber <= 0) {
      core.error(`Invalid milestone_number: ${item.milestone_number}`);
      return {
        success: false,
        error: `Invalid milestone_number: ${item.milestone_number}`,
      };
    }

    // Fetch milestones if needed and not already cached
    if (allowedMilestones.length > 0 && allMilestones === null) {
      try {
        const milestonesResponse = await github.rest.issues.listMilestones({
          owner: context.repo.owner,
          repo: context.repo.repo,
          state: "all",
          per_page: 100,
        });
        allMilestones = milestonesResponse.data;
        core.info(`Fetched ${allMilestones.length} milestones from repository`);
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to fetch milestones: ${errorMessage}`);
        return {
          success: false,
          error: `Failed to fetch milestones for validation: ${errorMessage}`,
        };
      }
    }

    // Validate against allowed list if configured
    if (allowedMilestones.length > 0 && allMilestones) {
      const milestone = allMilestones.find(m => m.number === milestoneNumber);

      if (!milestone) {
        core.warning(`Milestone #${milestoneNumber} not found in repository`);
        return {
          success: false,
          error: `Milestone #${milestoneNumber} not found in repository`,
        };
      }

      const isAllowed = allowedMilestones.includes(milestone.title) || allowedMilestones.includes(String(milestoneNumber));

      if (!isAllowed) {
        core.warning(`Milestone "${milestone.title}" (#${milestoneNumber}) is not in the allowed list`);
        return {
          success: false,
          error: `Milestone "${milestone.title}" (#${milestoneNumber}) is not in the allowed list`,
        };
      }
    }

    // Assign the milestone to the issue
    try {
      // If in staged mode, preview without executing
      if (isStaged) {
        core.info(`Staged mode: Would assign milestone #${milestoneNumber} to issue #${issueNumber}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            issue_number: issueNumber,
            milestone_number: milestoneNumber,
          },
        };
      }

      await github.rest.issues.update({
        owner: context.repo.owner,
        repo: context.repo.repo,
        issue_number: issueNumber,
        milestone: milestoneNumber,
      });

      core.info(`Successfully assigned milestone #${milestoneNumber} to issue #${issueNumber}`);
      return {
        success: true,
        issue_number: issueNumber,
        milestone_number: milestoneNumber,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to assign milestone #${milestoneNumber} to issue #${issueNumber}: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
