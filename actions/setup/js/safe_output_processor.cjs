// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Shared processor for safe-output scripts
 * Provides common pipeline: load agent output, handle staged mode, parse config, resolve target
 */

const { loadAgentOutput } = require("./load_agent_output.cjs");
const { generateStagedPreview } = require("./staged_preview.cjs");
const { parseAllowedItems, resolveTarget } = require("./safe_output_helpers.cjs");
const { getSafeOutputConfig, validateMaxCount } = require("./safe_output_validator.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { assertClaimAuthorized } = require("./work_queue_claim_scope.cjs");

/**
 * @typedef {Object} ProcessorConfig
 * @property {string} itemType - The type field value to match in agent output (e.g., "add_labels")
 * @property {string} configKey - The key to use when reading from config.json (e.g., "add_labels")
 * @property {string} displayName - Human-readable name for logging (e.g., "Add Labels")
 * @property {string} itemTypeName - Name used in error messages (e.g., "label addition")
 * @property {boolean} [supportsPR] - When true, allows both issue AND PR contexts; when false, only PR context (default: false)
 * @property {boolean} [supportsIssue] - When true, passes supportsPR=true to resolveTarget to enable both contexts (default: false)
 * @property {boolean} [findMultiple] - Whether to find multiple items instead of just one (default: false)
 * @property {Object} envVars - Environment variable names
 * @property {string} [envVars.allowed] - Env var for allowed items list
 * @property {string} [envVars.maxCount] - Env var for max count
 * @property {string} [envVars.target] - Env var for target configuration
 */

/**
 * @typedef {Object} ProcessorResult
 * @property {boolean} success - Whether processing should continue
 * @property {any} [item] - The found item (when findMultiple is false)
 * @property {any[]} [items] - The found items (when findMultiple is true)
 * @property {Object} [config] - Parsed configuration
 * @property {string[]|undefined} [config.allowed] - Allowed items list
 * @property {number} [config.maxCount] - Maximum count
 * @property {string} [config.target] - Target configuration
 * @property {Object} [targetResult] - Result from resolveTarget (when findMultiple is false)
 * @property {number} [targetResult.number] - Target issue/PR number
 * @property {string} [targetResult.contextType] - Type of context (issue or pull request)
 * @property {string} [reason] - Reason why processing should not continue
 */

/**
 * Process the initial steps common to safe-output scripts:
 * 1. Load agent output
 * 2. Find matching item(s)
 * 3. Handle staged mode
 * 4. Parse configuration (from passed config or fallback to env vars)
 * 5. Resolve target (for single-item processors)
 *
 * @param {ProcessorConfig} config - Processor configuration
 * @param {Object} stagedPreviewOptions - Options for staged preview
 * @param {string} stagedPreviewOptions.title - Title for staged preview
 * @param {string} stagedPreviewOptions.description - Description for staged preview
 * @param {(item: any, index: number) => string} stagedPreviewOptions.renderItem - Function to render item in preview
 * @param {Object} [handlerConfig] - Handler-specific configuration passed from handler manager
 * @returns {Promise<ProcessorResult>} Processing result
 */
async function processSafeOutput(config, stagedPreviewOptions, handlerConfig = null) {
  const { itemType, configKey, displayName, itemTypeName, supportsPR = false, supportsIssue = false, findMultiple = false, envVars } = config;

  // Step 1: Load agent output
  const result = loadAgentOutput();
  if (!result.success) {
    return { success: false, reason: "Agent output not available" };
  }

  // Step 2: Find matching item(s)
  let items;
  if (findMultiple) {
    items = result.items.filter(item => item.type === itemType && !item._claimScopeError);
    if (items.length === 0) {
      core.info(`No ${itemType} items found in agent output`);
      return { success: false, reason: `No ${itemType} items found` };
    }
    core.info(`Found ${items.length} ${itemType} item(s)`);
  } else {
    const item = result.items.find(item => item.type === itemType && !item._claimScopeError);
    if (!item) {
      core.warning(`No ${itemType.replace(/_/g, "-")} item found in agent output`);
      return { success: false, reason: `No ${itemType} item found` };
    }
    items = [item];
    // Log item details based on common fields
    const itemDetails = getItemDetails(item);
    if (itemDetails) {
      core.info(`Found ${itemType.replace(/_/g, "-")} item with ${itemDetails}`);
    }
  }

  for (const item of items) await assertClaimAuthorized(item);

  // Step 3: Handle 🎭 Staged Mode Preview — output preview via generateStagedPreview, skip real writes
  if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true") {
    await generateStagedPreview({
      title: stagedPreviewOptions.title,
      description: stagedPreviewOptions.description,
      items: items,
      renderItem: stagedPreviewOptions.renderItem,
    });
    return { success: false, reason: "Staged mode - preview generated" };
  }

  // Step 4: Parse configuration
  // If handlerConfig is provided (from handler manager), use it; otherwise fall back to file config + env vars
  let allowed, maxCount, target;

  if (handlerConfig) {
    // Use config passed from handler manager
    core.debug(`Using handler config: ${JSON.stringify(handlerConfig)}`);

    // Parse allowed items from handlerConfig
    allowed = handlerConfig.allowed || handlerConfig.allowed_labels || handlerConfig.allowed_repos;
    if (Array.isArray(allowed)) {
      core.info(`Allowed ${itemTypeName}s: ${JSON.stringify(allowed)}`);
    } else if (typeof allowed === "string") {
      allowed = parseAllowedItems(allowed);
      core.info(`Allowed ${itemTypeName}s: ${JSON.stringify(allowed)}`);
    } else {
      core.info(`No ${itemTypeName} restrictions - any ${itemTypeName}s are allowed`);
    }

    // Get max count from handlerConfig
    maxCount = handlerConfig.max || 3;
    core.info(`Max count: ${maxCount}`);

    // Get target from handlerConfig
    target = handlerConfig.target || "triggering";
    core.info(`${displayName} target configuration: ${target}`);
  } else {
    // Fall back to reading from config file + env vars (backward compatibility)
    const safeOutputConfig = getSafeOutputConfig(configKey);

    // Parse allowed items (from env or config)
    const allowedEnvValue = envVars.allowed ? process.env[envVars.allowed] : undefined;
    allowed = parseAllowedItems(allowedEnvValue) || safeOutputConfig.allowed;
    if (allowed) {
      core.info(`Allowed ${itemTypeName}s: ${JSON.stringify(allowed)}`);
    } else {
      core.info(`No ${itemTypeName} restrictions - any ${itemTypeName}s are allowed`);
    }

    // Parse max count (env takes priority, then config)
    const maxCountEnvValue = envVars.maxCount ? process.env[envVars.maxCount] : undefined;
    const maxCountResult = validateMaxCount(maxCountEnvValue, safeOutputConfig.max);
    if (!maxCountResult.valid) {
      core.setFailed(maxCountResult.error);
      return { success: false, reason: "Invalid max count configuration" };
    }
    maxCount = maxCountResult.value;
    core.info(`Max count: ${maxCount}`);

    // Get target configuration
    target = envVars.target ? process.env[envVars.target] || "triggering" : "triggering";
    core.info(`${displayName} target configuration: ${target}`);
  }

  // For multiple items, return early without target resolution
  if (findMultiple) {
    return {
      success: true,
      items: items,
      config: {
        allowed,
        maxCount,
        target,
      },
    };
  }

  // Step 5: Resolve target (for single-item processors)
  const item = items[0];
  const targetResult = resolveTarget({
    targetConfig: target,
    item: item,
    context,
    itemType: itemTypeName,
    // supportsPR in resolveTarget: true=both issue and PR contexts, false=PR-only
    // If supportsIssue is true, we pass supportsPR=true to enable both contexts
    supportsPR: supportsPR || supportsIssue,
    supportsIssue: false,
  });

  if (!targetResult.success) {
    if (targetResult.shouldFail) {
      core.setFailed(targetResult.error);
    } else {
      core.info(targetResult.error);
    }
    return { success: false, reason: targetResult.error };
  }

  return {
    success: true,
    item: item,
    config: {
      allowed,
      maxCount,
      target,
    },
    targetResult: {
      number: targetResult.number,
      contextType: targetResult.contextType,
    },
  };
}

/**
 * Get a description of item details for logging
 * @param {any} item - The safe output item
 * @returns {string|null} Description string or null
 */
function getItemDetails(item) {
  if (item.labels && Array.isArray(item.labels)) {
    return `${item.labels.length} labels`;
  }
  if (item.reviewers && Array.isArray(item.reviewers)) {
    return `${item.reviewers.length} reviewers`;
  }
  return null;
}

/**
 * Sanitize and deduplicate an array of string items
 * @param {any[]} items - Raw items array
 * @returns {string[]} Sanitized and deduplicated array
 */
function sanitizeItems(items) {
  return items
    .filter(item => item != null && item !== false && item !== 0)
    .map(item => String(item).trim())
    .filter(item => item)
    .filter((item, index, arr) => arr.indexOf(item) === index);
}

/**
 * Filter items by allowed list
 * @param {string[]} items - Items to filter
 * @param {string[]|undefined} allowed - Allowed items list (undefined means all allowed)
 * @returns {string[]} Filtered items
 */
function filterByAllowed(items, allowed) {
  if (!allowed || allowed.length === 0) {
    return items;
  }
  return items.filter(item => allowed.includes(item));
}

/**
 * Filter out items matching blocked patterns
 * @param {string[]} items - Items to filter
 * @param {string[]|undefined} blockedPatterns - Blocked patterns list (undefined means no blocking)
 * @returns {string[]} Filtered items with blocked items removed
 */
function filterByBlocked(items, blockedPatterns) {
  if (!blockedPatterns || blockedPatterns.length === 0) {
    return items;
  }

  // Import the pattern matching function
  const { isUsernameBlocked } = require("./safe_output_helpers.cjs");

  return items.filter(item => {
    const blocked = isUsernameBlocked(item, blockedPatterns);
    if (blocked) {
      core.info(`Filtering out blocked item: ${item}`);
    }
    return !blocked;
  });
}

/**
 * Limit items to max count
 * @param {string[]} items - Items to limit
 * @param {number} maxCount - Maximum number of items
 * @returns {string[]} Limited items
 */
function limitToMaxCount(items, maxCount) {
  if (items.length > maxCount) {
    core.info(`Too many items (${items.length}), limiting to ${maxCount}`);
    return items.slice(0, maxCount);
  }
  return items;
}

/**
 * Process items through the standard pipeline: filter by allowed, filter blocked, sanitize, dedupe, limit
 * @param {any[]} rawItems - Raw items array from agent output
 * @param {string[]|undefined} allowed - Allowed items list
 * @param {number} maxCount - Maximum number of items
 * @param {string[]|undefined} blocked - Blocked patterns list (optional)
 * @returns {string[]} Processed items
 */
function processItems(rawItems, allowed, maxCount, blocked = undefined) {
  // Filter by allowed list first
  const filtered = filterByAllowed(rawItems, allowed);

  // Filter out blocked items
  const notBlocked = filterByBlocked(filtered, blocked);

  // Sanitize and deduplicate
  const sanitized = sanitizeItems(notBlocked);

  // Limit to max count
  return limitToMaxCount(sanitized, maxCount);
}

module.exports = {
  processSafeOutput,
  sanitizeItems,
  filterByAllowed,
  filterByBlocked,
  limitToMaxCount,
  processItems,
};
