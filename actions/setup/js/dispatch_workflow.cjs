// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "dispatch_workflow";

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Main handler factory for dispatch_workflow
 * Returns a message handler function that processes individual dispatch_workflow messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedWorkflows = config.workflows || [];
  const maxCount = config.max || 1;
  const workflowFiles = config.workflow_files || {}; // Map of workflow name to file extension

  core.info(`Dispatch workflow configuration: max=${maxCount}`);
  if (allowedWorkflows.length > 0) {
    core.info(`Allowed workflows: ${allowedWorkflows.join(", ")}`);
  }
  if (Object.keys(workflowFiles).length > 0) {
    core.info(`Workflow files: ${JSON.stringify(workflowFiles)}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;
  let lastDispatchTime = 0;

  // Get the current repository context and ref
  const repo = context.repo;

  // Helper function to get the default branch
  const getDefaultBranchRef = async () => {
    // Try to get from context payload first
    if (context.payload.repository?.default_branch) {
      return `refs/heads/${context.payload.repository.default_branch}`;
    }

    // Fall back to querying the repository
    try {
      const { data: repoData } = await github.rest.repos.get({
        owner: repo.owner,
        repo: repo.repo,
      });
      return `refs/heads/${repoData.default_branch}`;
    } catch (error) {
      core.warning(`Failed to fetch default branch: ${getErrorMessage(error)}`);
      return "refs/heads/main";
    }
  };

  // When running in a PR context, GITHUB_REF points to the merge ref (refs/pull/{PR_NUMBER}/merge)
  // which is not a valid branch ref for dispatching workflows. Instead, we need to use
  // GITHUB_HEAD_REF which contains the actual PR branch name.
  let ref;
  if (process.env.GITHUB_HEAD_REF) {
    // We're in a pull_request event, use the PR branch ref
    ref = `refs/heads/${process.env.GITHUB_HEAD_REF}`;
    core.info(`Using PR branch ref: ${ref}`);
  } else if (process.env.GITHUB_REF || context.ref) {
    // Use GITHUB_REF for non-PR contexts (push, workflow_dispatch, etc.)
    ref = process.env.GITHUB_REF || context.ref;
  } else {
    // Last resort: fetch the repository's default branch
    ref = await getDefaultBranchRef();
    core.info(`Using default branch ref: ${ref}`);
  }

  /**
   * Message handler function that processes a single dispatch_workflow message
   * @param {Object} message - The dispatch_workflow message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleDispatchWorkflow(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping dispatch_workflow: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;
    const workflowName = item.workflow_name;

    if (!workflowName || workflowName.trim() === "") {
      core.warning("Workflow name is empty, skipping");
      return {
        success: false,
        error: "Workflow name is empty",
      };
    }

    // Validate workflow is in allowed list
    if (allowedWorkflows.length > 0 && !allowedWorkflows.includes(workflowName)) {
      const error = `Workflow "${workflowName}" is not in the allowed workflows list: ${allowedWorkflows.join(", ")}`;
      core.warning(error);
      return {
        success: false,
        error: error,
      };
    }

    try {
      // Add 5 second delay between dispatches (except for the first one)
      if (lastDispatchTime > 0) {
        const timeSinceLastDispatch = Date.now() - lastDispatchTime;
        const delayNeeded = 5000 - timeSinceLastDispatch;
        if (delayNeeded > 0) {
          core.info(`Waiting ${Math.ceil(delayNeeded / 1000)} seconds before next dispatch...`);
          await new Promise(resolve => setTimeout(resolve, delayNeeded));
        }
      }

      core.info(`Dispatching workflow: ${workflowName}`);

      // Prepare inputs - convert all values to strings as required by workflow_dispatch
      /** @type {Record<string, string>} */
      const inputs = {};
      if (item.inputs && typeof item.inputs === "object") {
        for (const [key, value] of Object.entries(item.inputs)) {
          // Convert value to string
          if (value === null || value === undefined) {
            inputs[key] = "";
          } else if (typeof value === "object") {
            inputs[key] = JSON.stringify(value);
          } else {
            inputs[key] = String(value);
          }
        }
      }

      // Get the workflow file extension from compile-time resolution
      const extension = workflowFiles[workflowName];
      if (!extension) {
        return {
          success: false,
          error: `Workflow "${workflowName}" file extension not found in configuration. This workflow may not have been validated at compile time.`,
        };
      }

      const workflowFile = `${workflowName}${extension}`;
      core.info(`Dispatching workflow: ${workflowFile}`);

      // Dispatch the workflow using the resolved file
      await github.rest.actions.createWorkflowDispatch({
        owner: repo.owner,
        repo: repo.repo,
        workflow_id: workflowFile,
        ref: ref,
        inputs: inputs,
      });

      core.info(`✓ Successfully dispatched workflow: ${workflowFile}`);

      // Record the time of this dispatch for rate limiting
      lastDispatchTime = Date.now();

      return {
        success: true,
        workflow_name: workflowName,
        inputs: inputs,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to dispatch workflow "${workflowName}": ${errorMessage}`);

      return {
        success: false,
        error: `Failed to dispatch workflow "${workflowName}": ${errorMessage}`,
      };
    }
  };
}

module.exports = { main };
