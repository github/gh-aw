// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { sanitizeLabelContent } = require("./sanitize_label_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { matchesSimpleGlob } = require("./glob_pattern_helpers.cjs");

/**
 * Load and parse the safe outputs configuration from config.json
 * @returns {any} The parsed configuration object
 */
function loadSafeOutputsConfig() {
  const configPath = `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/config.json`;
  try {
    if (!fs.existsSync(configPath)) {
      core.warning(`Config file not found at ${configPath}, using defaults`);
      return {};
    }
    const configContent = fs.readFileSync(configPath, "utf8");
    return JSON.parse(configContent);
  } catch (error) {
    core.warning(`Failed to load config: ${getErrorMessage(error)}`);
    return {};
  }
}

/**
 * Get configuration for a specific safe output type
 * @param {string} outputType - The type of safe output (e.g., "add_labels", "update_issue")
 * @returns {{max?: number, target?: string, allowed?: string[]}} The configuration for this output type
 */
function getSafeOutputConfig(outputType) {
  const config = loadSafeOutputsConfig();
  return config[outputType] || {};
}

/**
 * Validate and sanitize a title string
 * @param {any} title - The title to validate
 * @param {string} fieldName - The name of the field for error messages (default: "title")
 * @returns {{valid: boolean, value?: string, error?: string}} Validation result
 */
function validateTitle(title, fieldName = "title") {
  if (title === undefined || title === null) {
    return { valid: false, error: `${fieldName} is required` };
  }

  if (typeof title !== "string") {
    return { valid: false, error: `${fieldName} must be a string` };
  }

  const trimmed = title.trim();
  if (trimmed.length === 0) {
    return { valid: false, error: `${fieldName} cannot be empty` };
  }

  return { valid: true, value: trimmed };
}

/**
 * Validate and sanitize a body/content string
 * @param {any} body - The body to validate
 * @param {string} fieldName - The name of the field for error messages (default: "body")
 * @param {boolean} required - Whether the body is required (default: false)
 * @returns {{valid: boolean, value?: string, error?: string}} Validation result
 */
function validateBody(body, fieldName = "body", required = false) {
  if (body === undefined || body === null) {
    if (required) {
      return { valid: false, error: `${fieldName} is required` };
    }
    return { valid: true, value: "" };
  }

  if (typeof body !== "string") {
    return { valid: false, error: `${fieldName} must be a string` };
  }

  return { valid: true, value: body };
}

/**
 * Validate and sanitize an array of labels
 * @param {any} labels - The labels to validate
 * @param {string[]|undefined} allowedLabels - Optional list of allowed label patterns (supports glob patterns like "team-*", "area/*")
 * @param {number} maxCount - Maximum number of labels allowed
 * @param {string[]|undefined} blockedPatterns - Optional list of blocked label patterns (supports glob patterns like "~*", "*[bot]")
 * @returns {{valid: boolean, value?: string[], error?: string}} Validation result
 */
function validateLabels(labels, allowedLabels = undefined, maxCount = 3, blockedPatterns = undefined) {
  if (!labels || !Array.isArray(labels)) {
    return { valid: false, error: "labels must be an array" };
  }

  // Check for removal attempts (labels starting with '-')
  for (const label of labels) {
    if (label && typeof label === "string" && label.startsWith("-")) {
      return { valid: false, error: `Label removal is not permitted. Found line starting with '-': ${label}` };
    }
  }

  // Filter out blocked labels first (security boundary)
  let validLabels = labels;
  if (blockedPatterns && blockedPatterns.length > 0) {
    const blockedLabels = [];
    validLabels = labels.filter(label => {
      const labelStr = String(label).trim();
      const isBlocked = blockedPatterns.some(pattern => matchesSimpleGlob(labelStr, pattern));
      if (isBlocked) {
        blockedLabels.push(labelStr);
      }
      return !isBlocked;
    });
    if (blockedLabels.length > 0) {
      core.info(`Filtered out ${blockedLabels.length} blocked labels: ${blockedLabels.join(", ")}`);
    }
  }

  // Filter labels based on allowed list if provided
  if (allowedLabels && allowedLabels.length > 0) {
    validLabels = validLabels.filter(label => {
      const labelStr = String(label).trim();
      return allowedLabels.some(pattern => matchesSimpleGlob(labelStr, pattern));
    });
  }

  // Sanitize and deduplicate labels
  const uniqueLabels = validLabels
    .filter(label => label != null && label !== false && label !== 0)
    .map(label => String(label).trim())
    .filter(label => label)
    .map(label => sanitizeLabelContent(label))
    .filter(label => label)
    .map(label => (label.length > 64 ? label.substring(0, 64) : label))
    .filter((label, index, arr) => arr.indexOf(label) === index);

  // Apply max count limit
  if (uniqueLabels.length > maxCount) {
    core.warning(`Too many labels (${uniqueLabels.length}), limiting to ${maxCount}. Dropped labels: ${uniqueLabels.slice(maxCount).join(", ")}`);
    return { valid: true, value: uniqueLabels.slice(0, maxCount) };
  }

  if (uniqueLabels.length === 0) {
    return { valid: false, error: "No valid labels found after sanitization" };
  }

  return { valid: true, value: uniqueLabels };
}

/**
 * Validate max count from environment variable with config fallback
 * @param {string|undefined} envValue - Environment variable value
 * @param {number|undefined} configDefault - Default from config.json
 * @param {number} [fallbackDefault] - Fallback default for testing (optional, defaults to 1)
 * @returns {{valid: true, value: number} | {valid: false, error: string}} Validation result
 */
function validateMaxCount(envValue, configDefault, fallbackDefault = 1) {
  // Priority: env var > config.json > fallback default
  // In production, config.json should always have the default
  // Fallback is provided for backward compatibility and testing
  const defaultValue = configDefault !== undefined ? configDefault : fallbackDefault;

  if (!envValue) {
    return { valid: true, value: defaultValue };
  }

  const parsed = parseInt(envValue, 10);
  if (Number.isNaN(parsed) || parsed < 1) {
    return {
      valid: false,
      error: `Invalid max value: ${envValue}. Must be a positive integer`,
    };
  }

  return { valid: true, value: parsed };
}

module.exports = {
  loadSafeOutputsConfig,
  getSafeOutputConfig,
  validateTitle,
  validateBody,
  validateLabels,
  validateMaxCount,
};
