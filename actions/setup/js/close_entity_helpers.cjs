// @ts-check
/// <reference types="@actions/github-script" />

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { generateFooterWithMessages, getBodyFooterMessage, getDetectionCautionAlert } = require("./messages_footer.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { getRepositoryUrl } = require("./get_repository_url.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { validateTargetRepo, resolveTargetRepoConfig } = require("./repo_helpers.cjs");
const { ERR_API } = require("./error_codes.cjs");

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
 * Build comment body with tracker ID and footer
 * @param {string} body - The original comment body
 * @param {number|undefined} triggeringIssueNumber - Issue number that triggered this workflow
 * @param {number|undefined} triggeringPRNumber - PR number that triggered this workflow
 * @param {string} [bodyFooterTemplate] - Deterministic body footer template
 * @returns {string} The complete comment body with tracker ID and footer
 */
function buildCommentBody(body, triggeringIssueNumber, triggeringPRNumber, bodyFooterTemplate) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
  const runUrl = buildWorkflowRunUrl(context, context.repo);

  // Inject CAUTION at top of body if threat detection warning was raised.
  // Caller is responsible for sanitizing body before passing it here.
  const detectionCaution = getDetectionCautionAlert(workflowName, runUrl);
  const bodyWithCaution = detectionCaution ? detectionCaution + "\n\n" + body.trim() : body.trim();
  let finalBody =
    bodyWithCaution + getTrackerID("markdown") + "\n\n" + generateFooterWithMessages(workflowName, runUrl, workflowSource, workflowSourceURL, triggeringIssueNumber, triggeringPRNumber, undefined, undefined, { skipDetectionCaution: true });
  const bodyFooter = getBodyFooterMessage(bodyFooterTemplate, { workflowName, runUrl });
  if (bodyFooter) {
    finalBody += "\n\n" + bodyFooter.trimEnd();
  }
  return finalBody;
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
      if (Number.isNaN(parsed) || parsed <= 0) {
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
    if (Number.isNaN(parsed) || parsed <= 0) {
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
 * @typedef {Object} CloseEntityHandlerCallbacks
 * @property {(item: Object, config: Object, resolvedTemporaryIds?: any) => ({success: true, entityNumber: number, owner: string, repo: string, entityRepo?: string} | {success: false, error: string, deferred?: boolean})} resolveTarget
 *   Resolves the entity number and target repository from the message and handler config.
 *   The factory passes `item`, `config`, and `resolvedTemporaryIds`; implementations may ignore
 *   `config` or `resolvedTemporaryIds` if not needed.
 * @property {(github: any, owner: string, repo: string, entityNumber: number) => Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} getDetails
 *   Fetches entity details from the GitHub API.
 * @property {(entity: Object, entityNumber: number, requiredLabels: string[]) => {valid: true} | {valid: false, warning?: string, error: string}} validateLabels
 *   Validates entity labels against the required-labels filter.
 * @property {(sanitizedBody: string, item: Object) => string} buildCommentBody
 *   Builds the final comment body from the already-sanitized body text.
 *   The factory passes both `sanitizedBody` and `item`; implementations may ignore `item`
 *   if they retrieve context values (e.g. triggering PR number) from the global `context` directly.
 * @property {(github: any, owner: string, repo: string, entityNumber: number, body: string) => Promise<{id: number, html_url: string}>} addComment
 *   Posts a comment to the entity.
 * @property {(github: any, owner: string, repo: string, entityNumber: number, item: Object, config: Object) => Promise<{number: number, html_url: string, title: string}>} closeEntity
 *   Closes the entity via the GitHub API.
 *   The factory passes `item` and `config` for implementations that need per-item overrides
 *   (e.g. `state_reason`); implementations that don't need them may ignore those parameters.
 * @property {(closedEntity: Object, commentResult: Object|null, wasAlreadyClosed: boolean, commentPosted: boolean) => Object} buildSuccessResult
 *   Builds the success result object returned to the caller.
 * @property {boolean} [continueOnCommentError]
 *   When true, a failed comment post is logged but does not abort the close operation.
 *   When false/omitted, a comment failure propagates and causes the handler to return an error.
 */

/**
 * Create a message-level close-entity handler function.
 *
 * Centralises the common close-flow pipeline:
 *   1. Max-count gating
 *   2. Comment body resolution (item.body → config.comment fallback)
 *   3. Content sanitization
 *   4. Target repository / entity number resolution (via callbacks.resolveTarget)
 *   5. Entity details fetch + already-closed detection
 *   6. Label filter validation (via callbacks.validateLabels)
 *   7. Title-prefix filter validation
 *   8. Staged-mode preview short-circuit
 *   9. Comment posting (with optional continueOnCommentError)
 *  10. Entity close (skipped when already closed)
 *  11. Success result construction (via callbacks.buildSuccessResult)
 *
 * Entity-specific behaviour (API calls, label semantics, comment body
 * construction, result shape, cross-repo support) is supplied through the
 * callbacks argument so that each handler only retains the code that is
 * genuinely unique to it.
 *
 * @param {Object} config - Handler configuration object from main()
 * @param {EntityConfig} entityConfig - Entity display/type configuration
 * @param {CloseEntityHandlerCallbacks} callbacks - Entity-specific callbacks
 * @param {any} githubClient - Authenticated GitHub client
 * @returns {import('./types/handler-factory').MessageHandlerFunction} Message handler function
 */
function createCloseEntityHandler(config, entityConfig, callbacks, githubClient) {
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const maxCount = config.max || 10;
  const comment = config.comment || "";
  const isStaged = isStagedMode(config);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const allowBody = config.allow_body !== false; // default true; false only when explicitly set to false

  let processedCount = 0;

  return async function handleCloseEntity(message, resolvedTemporaryIds) {
    // 1. Max-count gating
    if (processedCount >= maxCount) {
      core.warning(`Skipping ${entityConfig.itemType}: max count of ${maxCount} reached`);
      return { success: false, error: `Max count of ${maxCount} reached` };
    }
    processedCount++;

    const item = message;

    // Log message structure for debugging (avoid logging body content)
    const logFields = { has_body: !!item.body, body_length: item.body ? item.body.length : 0 };
    if (item[entityConfig.numberField] !== undefined) {
      logFields[entityConfig.numberField] = item[entityConfig.numberField];
    }
    if (item.repo !== undefined) {
      logFields.has_repo = true;
    }
    core.info(`Processing ${entityConfig.itemType} message: ${JSON.stringify(logFields)}`);

    // 2. Comment body resolution
    /** @type {string|undefined} */
    let commentToPost;
    /** @type {string} */
    let commentSource = "unknown";

    if (!allowBody) {
      // allow-body: false — drop any body the agent provided and skip the comment
      if (typeof item.body === "string" && item.body.trim() !== "") {
        core.warning(`${entityConfig.itemType}: allow-body is false — dropping non-empty body (length=${item.body.length}) and closing without a comment`);
      } else {
        core.info(`${entityConfig.itemType}: allow-body is false — closing without a comment`);
      }
      commentToPost = undefined;
    } else if (typeof item.body === "string" && item.body.trim() !== "") {
      commentToPost = item.body;
      commentSource = "item.body";
    } else if (typeof comment === "string" && comment.trim() !== "") {
      commentToPost = comment;
      commentSource = "config.comment";
    } else {
      core.warning("No comment body provided in message and no default comment configured");
      return { success: false, error: "No comment body provided" };
    }

    if (commentToPost !== undefined) {
      core.info(`Comment body determined: length=${commentToPost.length}, source=${commentSource}`);

      // 3. Content sanitization
      commentToPost = sanitizeContent(commentToPost);
    }

    // 4. Target repository / entity number resolution
    const targetResult = callbacks.resolveTarget(item, config, resolvedTemporaryIds);
    if (!targetResult.success) {
      core.warning(`Skipping ${entityConfig.itemType}: ${targetResult.error}`);
      return { success: false, deferred: targetResult.deferred || false, error: targetResult.error };
    }
    const { entityNumber, owner, repo: repoName, entityRepo } = targetResult;
    if (entityRepo) {
      core.info(`Target repository: ${entityRepo}`);
    }

    // 4b. Cross-repository allowlist validation (SEC-005)
    const resolvedRepo = `${owner}/${repoName}`;
    const repoValidation = validateTargetRepo(resolvedRepo, defaultTargetRepo, allowedRepos);
    if (!repoValidation.valid) {
      core.warning(`Skipping ${entityConfig.itemType}: cross-repo check failed for "${resolvedRepo}": ${repoValidation.error}`);
      return { success: false, error: repoValidation.error };
    }

    try {
      // 5. Entity details fetch
      core.info(`Fetching ${entityConfig.displayName} details for #${entityNumber} in ${owner}/${repoName}`);
      const entity = await callbacks.getDetails(githubClient, owner, repoName, entityNumber);
      core.info(`${entityConfig.displayNameCapitalized} #${entityNumber} fetched: state=${entity.state}, title="${entity.title}", labels=[${entity.labels.map(l => l.name || l).join(", ")}]`);

      const wasAlreadyClosed = entity.state === "closed";
      if (wasAlreadyClosed) {
        core.info(`${entityConfig.displayNameCapitalized} #${entityNumber} is already closed, but will still add comment`);
      }

      // 6. Label filter validation
      const labelResult = callbacks.validateLabels(entity, entityNumber, requiredLabels);
      if (!labelResult.valid) {
        core.warning(labelResult.warning || `Skipping ${entityConfig.displayName} #${entityNumber}: ${labelResult.error}`);
        return { success: false, error: labelResult.error };
      }
      if (requiredLabels.length > 0) {
        core.info(`${entityConfig.displayNameCapitalized} #${entityNumber} has required labels: ${requiredLabels.join(", ")}`);
      }

      // 7. Title-prefix filter validation
      if (requiredTitlePrefix && !checkTitlePrefixFilter(entity.title, requiredTitlePrefix)) {
        core.warning(`${entityConfig.displayNameCapitalized} #${entityNumber} title doesn't start with "${requiredTitlePrefix}"`);
        return { success: false, error: `Title doesn't start with "${requiredTitlePrefix}"` };
      }
      if (requiredTitlePrefix) {
        core.info(`${entityConfig.displayNameCapitalized} #${entityNumber} has required title prefix: "${requiredTitlePrefix}"`);
      }

      // 8. Staged-mode preview short-circuit
      if (isStaged) {
        const repoStr = entityRepo || `${owner}/${repoName}`;
        logStagedPreviewInfo(`Would close ${entityConfig.displayName} #${entityNumber} in ${repoStr}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            number: entityNumber,
            repo: repoStr,
            alreadyClosed: wasAlreadyClosed,
            hasComment: !!commentToPost,
          },
        };
      }

      // 9. Comment posting (skipped when allow-body: false or no body available)
      /** @type {{id: number, html_url: string}|null} */
      let commentResult = null;
      let commentPosted = false;
      if (commentToPost !== undefined) {
        const commentBody = callbacks.buildCommentBody(commentToPost, item);
        core.info(`Adding comment to ${entityConfig.displayName} #${entityNumber}: length=${commentBody.length}`);

        try {
          commentResult = await callbacks.addComment(githubClient, owner, repoName, entityNumber, commentBody);
          commentPosted = true;
          core.info(`✓ Comment posted to ${entityConfig.displayName} #${entityNumber}: ${commentResult.html_url}`);
          core.info(`Comment details: id=${commentResult.id}, body_length=${commentBody.length}`);
        } catch (commentError) {
          const errorMsg = getErrorMessage(commentError);
          if (callbacks.continueOnCommentError) {
            core.error(`Failed to add comment to ${entityConfig.displayName} #${entityNumber}: ${errorMsg}`);
            core.error(
              `Error details: ${JSON.stringify({
                entityNumber,
                hasBody: !!item.body,
                bodyLength: item.body ? item.body.length : 0,
                errorMessage: errorMsg,
              })}`
            );
            // commentPosted stays false; close operation continues
          } else {
            throw new Error(`${ERR_API}: Failed to add comment to ${entityConfig.displayName} #${entityNumber}: ${errorMsg}`, { cause: commentError });
          }
        }
      } else {
        core.info(`Skipping comment for ${entityConfig.displayName} #${entityNumber}: no comment body`);
      }

      // 10. Entity close (skipped when already closed)
      let closedEntity;
      if (wasAlreadyClosed) {
        core.info(`${entityConfig.displayNameCapitalized} #${entityNumber} was already closed, comment ${commentPosted ? "added successfully" : "posting attempted"}`);
        closedEntity = entity;
      } else {
        closedEntity = await callbacks.closeEntity(githubClient, owner, repoName, entityNumber, item, config);
        core.info(`✓ ${entityConfig.displayNameCapitalized} #${entityNumber} closed successfully: ${closedEntity.html_url}`);
      }

      core.info(`${entityConfig.itemType} completed successfully for ${entityConfig.displayName} #${entityNumber}`);

      // 11. Success result construction
      return callbacks.buildSuccessResult(closedEntity, commentResult, wasAlreadyClosed, commentPosted);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to close ${entityConfig.displayName} #${entityNumber}: ${errorMessage}`);
      core.error(
        `Error details: ${JSON.stringify({
          entityNumber,
          hasBody: !!item.body,
          bodyLength: item.body ? item.body.length : 0,
          errorMessage,
        })}`
      );
      return { success: false, error: errorMessage };
    }
  };
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
  const isStaged = isStagedMode(handlerConfig);

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

      // Build comment body (sanitize first, then append tracker/footer)
      const sanitizedItemBody = sanitizeContent(item.body);
      const commentBody = buildCommentBody(sanitizedItemBody, triggeringIssueNumber, triggeringPRNumber);

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
  createCloseEntityHandler,
  ISSUE_CONFIG,
  PULL_REQUEST_CONFIG,
};
