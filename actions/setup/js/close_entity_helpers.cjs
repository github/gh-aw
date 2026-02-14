// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { generateFooterWithMessages } = require("./messages_footer.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { getRepositoryUrl } = require("./get_repository_url.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @typedef {'issue' | 'pull_request'} EntityType
 */

/**
 * @typedef {Object} EntityConfig
 * @property {EntityType} entityType - The type of entity (issue or pull_request)
 * @property {string} itemType - The agent output item type (e.g., "close_issue")
 * @property {string} itemTypeDisplay - Human-readable item type for log messages (e.g., "close-issue")
 * @property {string} numberField - The field name for the entity number in agent output (e.g., "issue_number")
 * @property {string} envVarPrefix - Environment variable prefix (e.g., "GH_AW_CLOSE_ISSUE")
 * @property {string[]} contextEvents - GitHub event names for this entity context
 * @property {string} contextPayloadField - The field name in context.payload (e.g., "issue")
 * @property {string} urlPath - URL path segment (e.g., "issues" or "pull")
 * @property {string} displayName - Human-readable display name (e.g., "issue" or "pull request")
 * @property {string} displayNamePlural - Human-readable display name plural (e.g., "issues" or "pull requests")
 * @property {string} displayNameCapitalized - Capitalized display name (e.g., "Issue" or "Pull Request")
 * @property {string} displayNameCapitalizedPlural - Capitalized display name plural (e.g., "Issues" or "Pull Requests")
 */

/**
 * @typedef {Object} EntityCallbacks
 * @property {(github: any, owner: string, repo: string, entityNumber: number) => Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} getDetails
 * @property {(github: any, owner: string, repo: string, entityNumber: number, message: string) => Promise<{id: number, html_url: string}>} addComment
 * @property {(github: any, owner: string, repo: string, entityNumber: number) => Promise<{number: number, html_url: string, title: string}>} closeEntity
 */

/**
 * Build the run URL for the current workflow
 * @returns {string} The workflow run URL
 */
function buildRunUrl() {
  const runId = context.runId;
  const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
  return context.payload.repository ? `${context.payload.repository.html_url}/actions/runs/${runId}` : `${githubServer}/${context.repo.owner}/${context.repo.repo}/actions/runs/${runId}`;
}

/**
 * Build comment body with tracker ID and footer
 * @param {string} body - The original comment body
 * @param {number|undefined} triggeringIssueNumber - Issue number that triggered this workflow
 * @param {number|undefined} triggeringPRNumber - PR number that triggered this workflow
 * @returns {string} The complete comment body with tracker ID and footer
 */
function buildCommentBody(body, triggeringIssueNumber, triggeringPRNumber) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runUrl = buildRunUrl();

  return body.trim() + getTrackerID("markdown") + generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, undefined);
}

/**
 * Check if labels match the required labels filter
 * @param {Array<{name: string}>} entityLabels - Labels on the entity
 * @param {string[]} requiredLabels - Required labels (any match)
 * @returns {boolean} True if entity has at least one required label or no filter is set
 */
function checkLabelFilter(entityLabels, requiredLabels) {
  if (requiredLabels.length === 0) return true;

  const labelNames = entityLabels.map(l => l.name);
  return requiredLabels.some(required => labelNames.includes(required));
}

/**
 * Check if title matches the required prefix filter
 * @param {string} title - Entity title
 * @param {string} requiredTitlePrefix - Required title prefix
 * @returns {boolean} True if title starts with required prefix or no filter is set
 */
function checkTitlePrefixFilter(title, requiredTitlePrefix) {
  if (!requiredTitlePrefix) return true;
  return title.startsWith(requiredTitlePrefix);
}

/**
 * Generate staged preview content for a close entity operation
 * @param {EntityConfig} config - Entity configuration
 * @param {any[]} items - Items to preview
 * @param {string[]} requiredLabels - Required labels filter
 * @param {string} requiredTitlePrefix - Required title prefix filter
 * @returns {Promise<void>}
 */
async function generateCloseEntityStagedPreview(config, items, requiredLabels, requiredTitlePrefix) {
  let summaryContent = `## 🎭 Staged Mode: Close ${config.displayNameCapitalizedPlural} Preview\n\n`;
  summaryContent += `The following ${config.displayNamePlural} would be closed if staged mode was disabled:\n\n`;

  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    summaryContent += `### ${config.displayNameCapitalized} ${i + 1}\n`;

    const entityNumber = item[config.numberField];
    if (entityNumber) {
      const repoUrl = getRepositoryUrl();
      const entityUrl = `${repoUrl}/${config.urlPath}/${entityNumber}`;
      summaryContent += `**Target ${config.displayNameCapitalized}:** [#${entityNumber}](${entityUrl})\n\n`;
    } else {
      summaryContent += `**Target:** Current ${config.displayName}\n\n`;
    }

    summaryContent += `**Comment:**\n${item.body || "No content provided"}\n\n`;

    if (requiredLabels.length > 0) {
      summaryContent += `**Required Labels:** ${requiredLabels.join(", ")}\n\n`;
    }
    if (requiredTitlePrefix) {
      summaryContent += `**Required Title Prefix:** ${requiredTitlePrefix}\n\n`;
    }

    summaryContent += "---\n\n";
  }

  // Write to step summary
  await core.summary.addRaw(summaryContent).write();
  core.info(`📝 ${config.displayNameCapitalized} close preview written to step summary`);
}

/**
 * Parse configuration from environment variables
 * @param {string} envVarPrefix - Environment variable prefix
 * @returns {{requiredLabels: string[], requiredTitlePrefix: string, target: string}}
 */
function parseEntityConfig(envVarPrefix) {
  const labelsEnvVar = `${envVarPrefix}_REQUIRED_LABELS`;
  const titlePrefixEnvVar = `${envVarPrefix}_REQUIRED_TITLE_PREFIX`;
  const targetEnvVar = `${envVarPrefix}_TARGET`;

  const requiredLabels = process.env[labelsEnvVar] ? process.env[labelsEnvVar].split(",").map(l => l.trim()) : [];
  const requiredTitlePrefix = process.env[titlePrefixEnvVar] || "";
  const target = process.env[targetEnvVar] || "triggering";

  return { requiredLabels, requiredTitlePrefix, target };
}

/**
 * Resolve the entity number based on target configuration and context
 * @param {EntityConfig} config - Entity configuration
 * @param {string} target - Target configuration ("triggering", "*", or explicit number)
 * @param {any} item - The agent output item
 * @param {boolean} isEntityContext - Whether we're in the correct entity context
 * @returns {{success: true, number: number} | {success: false, message: string}}
 */
function resolveEntityNumber(config, target, item, isEntityContext) {
  if (target === "*") {
    const targetNumber = item[config.numberField];
    if (targetNumber) {
      const parsed = parseInt(targetNumber, 10);
      if (isNaN(parsed) || parsed <= 0) {
        return {
          success: false,
          message: `Invalid ${config.displayName} number specified: ${targetNumber}`,
        };
      }
      return { success: true, number: parsed };
    }
    return {
      success: false,
      message: `Target is "*" but no ${config.numberField} specified in ${config.itemTypeDisplay} item`,
    };
  }

  if (target !== "triggering") {
    const parsed = parseInt(target, 10);
    if (isNaN(parsed) || parsed <= 0) {
      return {
        success: false,
        message: `Invalid ${config.displayName} number in target configuration: ${target}`,
      };
    }
    return { success: true, number: parsed };
  }

  // Default behavior: use triggering entity
  if (isEntityContext) {
    const number = context.payload[config.contextPayloadField]?.number;
    if (!number) {
      return {
        success: false,
        message: `${config.displayNameCapitalized} context detected but no ${config.displayName} found in payload`,
      };
    }
    return { success: true, number };
  }

  return {
    success: false,
    message: `Not in ${config.displayName} context and no explicit target specified`,
  };
}

/**
 * Escape special markdown characters in a title
 * @param {string} title - The title to escape
 * @returns {string} Escaped title
 */
function escapeMarkdownTitle(title) {
  return title.replace(/[[\]()]/g, "\\$&");
}

/**
 * Process close entity items from agent output
 * @param {EntityConfig} config - Entity configuration
 * @param {EntityCallbacks} callbacks - Entity-specific API callbacks
 * @param {Object} handlerConfig - Handler-specific configuration object
 * @returns {Promise<Array<{entity: {number: number, html_url: string, title: string}, comment: {id: number, html_url: string}}>|undefined>}
 */
async function processCloseEntityItems(config, callbacks, handlerConfig = {}) {
  // Check if we're in staged mode
  const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true";

  const result = loadAgentOutput();
  if (!result.success) {
    return;
  }

  // Find all items of this type
  const items = result.items.filter(/** @param {any} item */ item => item.type === config.itemType);
  if (items.length === 0) {
    core.info(`No ${config.itemTypeDisplay} items found in agent output`);
    return;
  }

  core.info(`Found ${items.length} ${config.itemTypeDisplay} item(s)`);

  // Get configuration from handlerConfig object (not environment variables)
  const requiredLabels = handlerConfig.required_labels || [];
  const requiredTitlePrefix = handlerConfig.required_title_prefix || "";
  const target = handlerConfig.target || "triggering";

  core.info(`Configuration: requiredLabels=${requiredLabels.join(",")}, requiredTitlePrefix=${requiredTitlePrefix}, target=${target}`);

  // Check if we're in the correct entity context
  const isEntityContext = config.contextEvents.some(event => context.eventName === event);

  // If in staged mode, emit step summary instead of closing entities
  if (isStaged) {
    await generateCloseEntityStagedPreview(config, items, requiredLabels, requiredTitlePrefix);
    return;
  }

  // Validate context based on target configuration
  if (target === "triggering" && !isEntityContext) {
    core.info(`Target is "triggering" but not running in ${config.displayName} context, skipping ${config.displayName} close`);
    return;
  }

  // Extract triggering context for footer generation
  const triggeringIssueNumber = context.payload?.issue?.number;
  const triggeringPRNumber = context.payload?.pull_request?.number;

  const closedEntities = [];

  // Process each item
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    core.info(`Processing ${config.itemTypeDisplay} item ${i + 1}/${items.length}: bodyLength=${item.body.length}`);

    // Resolve entity number
    const resolved = resolveEntityNumber(config, target, item, isEntityContext);
    if (!resolved.success) {
      core.info(resolved.message);
      continue;
    }
    const entityNumber = resolved.number;

    try {
      // Fetch entity details to check filters
      const entity = await callbacks.getDetails(github, context.repo.owner, context.repo.repo, entityNumber);

      // Apply label filter
      if (!checkLabelFilter(entity.labels, requiredLabels)) {
        core.info(`${config.displayNameCapitalized} #${entityNumber} does not have required labels: ${requiredLabels.join(", ")}`);
        continue;
      }

      // Apply title prefix filter
      if (!checkTitlePrefixFilter(entity.title, requiredTitlePrefix)) {
        core.info(`${config.displayNameCapitalized} #${entityNumber} does not have required title prefix: ${requiredTitlePrefix}`);
        continue;
      }

      // Check if already closed - but still add comment
      const wasAlreadyClosed = entity.state === "closed";
      if (wasAlreadyClosed) {
        core.info(`${config.displayNameCapitalized} #${entityNumber} is already closed, but will still add comment`);
      }

      // Build comment body
      const commentBody = buildCommentBody(item.body, triggeringIssueNumber, triggeringPRNumber);

      // Add comment before closing (or to already-closed entity)
      const comment = await callbacks.addComment(github, context.repo.owner, context.repo.repo, entityNumber, commentBody);
      core.info(`✓ Added comment to ${config.displayName} #${entityNumber}: ${comment.html_url}`);

      // Close the entity if not already closed
      let closedEntity;
      if (wasAlreadyClosed) {
        core.info(`${config.displayNameCapitalized} #${entityNumber} was already closed, comment added`);
        closedEntity = entity;
      } else {
        closedEntity = await callbacks.closeEntity(github, context.repo.owner, context.repo.repo, entityNumber);
        core.info(`✓ Closed ${config.displayName} #${entityNumber}: ${closedEntity.html_url}`);
      }

      closedEntities.push({
        entity: closedEntity,
        comment,
      });

      // Set outputs for the last closed entity (for backward compatibility)
      if (i === items.length - 1) {
        const numberOutputName = config.entityType === "issue" ? "issue_number" : "pull_request_number";
        const urlOutputName = config.entityType === "issue" ? "issue_url" : "pull_request_url";
        core.setOutput(numberOutputName, closedEntity.number);
        core.setOutput(urlOutputName, closedEntity.html_url);
        core.setOutput("comment_url", comment.html_url);
      }
    } catch (error) {
      core.error(`✗ Failed to close ${config.displayName} #${entityNumber}: ${getErrorMessage(error)}`);
      throw error;
    }
  }

  // Write summary for all closed entities
  if (closedEntities.length > 0) {
    let summaryContent = `\n\n## Closed ${config.displayNameCapitalizedPlural}\n`;
    for (const { entity, comment } of closedEntities) {
      const escapedTitle = escapeMarkdownTitle(entity.title);
      summaryContent += `- ${config.displayNameCapitalized} #${entity.number}: [${escapedTitle}](${entity.html_url}) ([comment](${comment.html_url}))\n`;
    }
    await core.summary.addRaw(summaryContent).write();
  }

  core.info(`Successfully closed ${closedEntities.length} ${config.displayName}(s)`);
  return closedEntities;
}

/**
 * Configuration for closing issues
 * @type {EntityConfig}
 */
const ISSUE_CONFIG = {
  entityType: "issue",
  itemType: "close_issue",
  itemTypeDisplay: "close-issue",
  numberField: "issue_number",
  envVarPrefix: "GH_AW_CLOSE_ISSUE",
  contextEvents: ["issues", "issue_comment"],
  contextPayloadField: "issue",
  urlPath: "issues",
  displayName: "issue",
  displayNamePlural: "issues",
  displayNameCapitalized: "Issue",
  displayNameCapitalizedPlural: "Issues",
};

/**
 * Configuration for closing pull requests
 * @type {EntityConfig}
 */
const PULL_REQUEST_CONFIG = {
  entityType: "pull_request",
  itemType: "close_pull_request",
  itemTypeDisplay: "close-pull-request",
  numberField: "pull_request_number",
  envVarPrefix: "GH_AW_CLOSE_PR",
  contextEvents: ["pull_request", "pull_request_review_comment"],
  contextPayloadField: "pull_request",
  urlPath: "pull",
  displayName: "pull request",
  displayNamePlural: "pull requests",
  displayNameCapitalized: "Pull Request",
  displayNameCapitalizedPlural: "Pull Requests",
};

module.exports = {
  processCloseEntityItems,
  generateCloseEntityStagedPreview,
  checkLabelFilter,
  checkTitlePrefixFilter,
  parseEntityConfig,
  resolveEntityNumber,
  buildCommentBody,
  escapeMarkdownTitle,
  ISSUE_CONFIG,
  PULL_REQUEST_CONFIG,
};
