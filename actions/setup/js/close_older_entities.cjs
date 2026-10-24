// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { getWorkflowIdMarkerContent } = require("./generate_footer.cjs");
const { MAX_CLOSE_COUNT } = require("./constants.cjs");

/**
 * Delay execution for a specified number of milliseconds
 * @param {number} ms - Milliseconds to delay
 * @returns {Promise<void>}
 */
function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Configuration for entity-specific behavior
 * @typedef {Object} EntityCloseConfig
 * @property {string} entityType - Entity type name for logging (e.g., "issue", "discussion")
 * @property {string} entityTypePlural - Plural form (e.g., "issues", "discussions")
 * @property {(github: any, owner: string, repo: string, workflowId: string, ...args: any[]) => Promise<Array<any>>} searchOlderEntities - Function to search for older entities
 * @property {(params: any) => string} getCloseMessage - Function to generate closing message
 * @property {(github: any, owner: string, repo: string, entityId: any, message: string) => Promise<{id: any, url?: string, html_url?: string}>} addComment - Function to add comment to entity
 * @property {(github: any, owner: string, repo: string, entityId: any) => Promise<{number?: number, id?: string, url?: string, html_url?: string}>} closeEntity - Function to close entity
 * @property {number} delayMs - Delay between API operations in milliseconds
 * @property {(entity: any) => any} getEntityId - Function to extract entity ID (for API calls)
 * @property {(entity: any) => string} getEntityUrl - Function to extract entity URL
 */

/**
 * Close older entities that match the workflow-id marker
 *
 * This function orchestrates the complete flow:
 * 1. Search for older entities with matching workflow-id marker
 * 2. Log results and handle early exits (no entities found)
 * 3. Apply MAX_CLOSE_COUNT limit
 * 4. Process each entity: comment + close
 * 5. Handle errors gracefully (continue with remaining entities)
 * 6. Report summary statistics
 *
 * @param {any} github - GitHub API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} workflowId - Workflow ID to match in the marker
 * @param {any} newEntity - The newly created entity (contains number and url/html_url)
 * @param {string} workflowName - Name of the workflow
 * @param {string} runUrl - URL of the workflow run
 * @param {EntityCloseConfig} config - Entity-specific configuration
 * @param {...any} extraArgs - Additional arguments to pass to searchOlderEntities
 * @returns {Promise<Array<{number: number, url?: string, html_url?: string}>>} List of closed entities
 */
async function closeOlderEntities(github, owner, repo, workflowId, newEntity, workflowName, runUrl, config, ...extraArgs) {
  core.info("=".repeat(70));
  core.info(`Starting closeOlder${config.entityType.charAt(0).toUpperCase() + config.entityType.slice(1)}s operation`);
  core.info("=".repeat(70));

  core.info(`Search criteria: workflow-id marker: "${getWorkflowIdMarkerContent(workflowId)}"`);
  core.info(`New ${config.entityType} reference: #${newEntity.number} (${newEntity.url || newEntity.html_url})`);
  core.info(`Workflow: ${workflowName}`);
  core.info(`Run URL: ${runUrl}`);
  core.info("");

  // Step 1: Search for older entities
  const olderEntities = await config.searchOlderEntities(github, owner, repo, workflowId, ...extraArgs, newEntity.number);

  if (olderEntities.length === 0) {
    core.info(`✓ No older ${config.entityTypePlural} found to close - operation complete`);
    core.info("=".repeat(70));
    return [];
  }

  core.info("");
  core.info(`Found ${olderEntities.length} older ${config.entityType}(s) matching the criteria`);
  for (const entity of olderEntities) {
    core.info(`  - ${config.entityType.charAt(0).toUpperCase() + config.entityType.slice(1)} #${entity.number}: ${entity.title}`);
    if (entity.labels) {
      core.info(`    Labels: ${entity.labels.map(l => l.name).join(", ") || "(none)"}`);
    }
    core.info(`    URL: ${config.getEntityUrl(entity)}`);
  }

  // Step 2: Limit to MAX_CLOSE_COUNT entities
  const entitiesToClose = olderEntities.slice(0, MAX_CLOSE_COUNT);

  if (olderEntities.length > MAX_CLOSE_COUNT) {
    core.warning("");
    core.warning(`⚠️  Found ${olderEntities.length} older ${config.entityTypePlural}, but only closing the first ${MAX_CLOSE_COUNT}`);
    core.warning(`    The remaining ${olderEntities.length - MAX_CLOSE_COUNT} ${config.entityType}(s) will be processed in subsequent runs`);
  }

  core.info("");
  core.info(`Preparing to close ${entitiesToClose.length} ${config.entityType}(s)...`);
  core.info("");

  const closedEntities = [];

  // Step 3: Process each entity
  for (let i = 0; i < entitiesToClose.length; i++) {
    const entity = entitiesToClose[i];
    core.info("-".repeat(70));
    core.info(`Processing ${config.entityType} ${i + 1}/${entitiesToClose.length}: #${entity.number}`);
    core.info(`  Title: ${entity.title}`);
    core.info(`  URL: ${config.getEntityUrl(entity)}`);

    try {
      // Generate closing message
      const closingMessage = config.getCloseMessage({
        newEntityUrl: newEntity.url || newEntity.html_url,
        newEntityNumber: newEntity.number,
        workflowName,
        runUrl,
      });

      core.info(`  Message length: ${closingMessage.length} characters`);
      core.info("");

      // Add comment first
      await config.addComment(github, owner, repo, config.getEntityId(entity), closingMessage);

      // Then close the entity
      await config.closeEntity(github, owner, repo, config.getEntityId(entity));

      closedEntities.push({
        number: entity.number,
        ...(entity.url && { url: entity.url }),
        ...(entity.html_url && { html_url: entity.html_url }),
      });

      core.info("");
      core.info(`✓ Successfully closed ${config.entityType} #${entity.number}`);
    } catch (error) {
      core.info("");
      core.error(`✗ Failed to close ${config.entityType} #${entity.number}`);
      core.error(`  Error: ${getErrorMessage(error)}`);
      if (error instanceof Error && error.stack) {
        core.error(`  Stack trace: ${error.stack}`);
      }
      // Continue with other entities even if one fails
    }

    // Step 4: Add delay between API operations to avoid rate limiting (except for the last item)
    if (i < entitiesToClose.length - 1) {
      core.info("");
      core.info(`Waiting ${config.delayMs}ms before processing next ${config.entityType} to avoid rate limiting...`);
      await delay(config.delayMs);
    }
  }

  core.info("");
  core.info("=".repeat(70));
  core.info(`Closed ${closedEntities.length} of ${entitiesToClose.length} ${config.entityType}(s) successfully`);
  if (closedEntities.length < entitiesToClose.length) {
    core.warning(`Failed to close ${entitiesToClose.length - closedEntities.length} ${config.entityType}(s) - check logs above for details`);
  }
  core.info("=".repeat(70));

  return closedEntities;
}

module.exports = {
  closeOlderEntities,
  MAX_CLOSE_COUNT,
  delay,
};
