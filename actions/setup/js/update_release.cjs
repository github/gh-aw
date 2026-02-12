// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { updateBody } = require("./update_pr_description_helpers.cjs");

/**
 * Create a handler for update-release messages
 * This is the factory function called by the handler manager
 *
 * @param {Object} config - Handler configuration
 * @param {number} [config.max] - Maximum number of releases to update
 * @param {boolean} [config.footer] - Controls whether AI-generated footer is added (default: true)
 * @returns {Promise<Function>} Handler function that processes a single message
 */
async function main(config = {}) {
  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "GitHub Agentic Workflow";
  const includeFooter = config.footer !== false; // Default to true (include footer)

  /**
   * Process a single update-release message
   * @param {Object} message - The update-release message
   * @param {Object} resolvedTemporaryIds - Map of resolved temporary IDs
   * @returns {Promise<Object>} Result with release info
   */
  return async function handleUpdateRelease(message, resolvedTemporaryIds = {}) {
    // In staged mode, skip actual processing (preview is handled elsewhere)
    if (isStaged) {
      core.info(`Staged mode: Would update release with tag ${message.tag || "(inferred)"}`);
      return { skipped: true, reason: "staged_mode" };
    }

    core.info(`Processing update-release message`);

    try {
      // Infer tag from event context if not provided
      let releaseTag = message.tag;
      if (!releaseTag) {
        // Try to get tag from release event context
        if (context.eventName === "release" && context.payload.release && context.payload.release.tag_name) {
          releaseTag = context.payload.release.tag_name;
          core.info(`Inferred release tag from event context: ${releaseTag}`);
        } else if (context.eventName === "workflow_dispatch" && context.payload.inputs) {
          // Try to extract from release_url input
          const releaseUrl = context.payload.inputs.release_url;
          if (releaseUrl) {
            const urlMatch = releaseUrl.match(/github\.com\/[^\/]+\/[^\/]+\/releases\/tag\/([^\/\?#]+)/);
            if (urlMatch && urlMatch[1]) {
              releaseTag = decodeURIComponent(urlMatch[1]);
              core.info(`Inferred release tag from release_url input: ${releaseTag}`);
            }
          }
          // Try to fetch from release_id input
          if (!releaseTag && context.payload.inputs.release_id) {
            const releaseId = context.payload.inputs.release_id;
            core.info(`Fetching release with ID: ${releaseId}`);
            const { data: release } = await github.rest.repos.getRelease({
              owner: context.repo.owner,
              repo: context.repo.repo,
              release_id: parseInt(releaseId, 10),
            });
            releaseTag = release.tag_name;
            core.info(`Inferred release tag from release_id input: ${releaseTag}`);
          }
        }

        if (!releaseTag) {
          throw new Error("Release tag is required but not provided and cannot be inferred from event context");
        }
      }

      // Get the release by tag
      core.info(`Fetching release with tag: ${releaseTag}`);
      const { data: release } = await github.rest.repos.getReleaseByTag({
        owner: context.repo.owner,
        repo: context.repo.repo,
        tag: releaseTag,
      });

      core.info(`Found release: ${release.name || release.tag_name} (ID: ${release.id})`);

      // Get workflow run URL for AI attribution
      const runUrl = `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;
      const workflowId = process.env.GH_AW_WORKFLOW_ID || "";

      // Use shared helper to update body based on operation
      const newBody = updateBody({
        currentBody: release.body || "",
        newContent: message.body,
        operation: message.operation || "append",
        workflowName,
        runUrl,
        workflowId,
        includeFooter, // Pass footer flag to helper
      });

      // Update the release
      const { data: updatedRelease } = await github.rest.repos.updateRelease({
        owner: context.repo.owner,
        repo: context.repo.repo,
        release_id: release.id,
        body: newBody,
      });

      core.info(`Successfully updated release: ${updatedRelease.html_url}`);

      // Return result with release info
      return {
        tag: releaseTag,
        url: updatedRelease.html_url,
        id: updatedRelease.id,
        releaseId: updatedRelease.id,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      const tagInfo = message.tag || "inferred from context";

      // Check for specific error cases
      if (errorMessage.includes("Not Found")) {
        throw new Error(`Release with tag '${tagInfo}' not found. Please ensure the tag exists.`);
      }

      throw new Error(`Failed to update release with tag ${tagInfo}: ${errorMessage}`);
    }
  };
}

module.exports = { main };
