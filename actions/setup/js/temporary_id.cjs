// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");

const crypto = require("crypto");

/**
 * Regex pattern for matching temporary ID references in text
 * Format: #aw_XXX to #aw_XXXXXXXX (aw_ prefix + 3 to 8 alphanumeric characters)
 */
const TEMPORARY_ID_PATTERN = /#(aw_[A-Za-z0-9]{3,8})/gi;

/**
 * @typedef {Object} RepoIssuePair
 * @property {string} repo - Repository slug in "owner/repo" format
 * @property {number} number - Issue or discussion number
 */

/**
 * Generate a temporary ID with aw_ prefix for temporary issue IDs
 * @returns {string} A temporary ID in format aw_XXXXXXXX (8 alphanumeric characters)
 */
function generateTemporaryId() {
  // Generate 8 random alphanumeric characters (A-Za-z0-9)
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let result = "aw_";
  for (let i = 0; i < 8; i++) {
    const randomIndex = Math.floor(Math.random() * chars.length);
    result += chars[randomIndex];
  }
  return result;
}

/**
 * Check if a value is a valid temporary ID (aw_ prefix + 3 to 8 alphanumeric characters)
 * @param {any} value - The value to check
 * @returns {boolean} True if the value is a valid temporary ID
 */
function isTemporaryId(value) {
  if (typeof value === "string") {
    return /^aw_[A-Za-z0-9]{3,8}$/i.test(value);
  }
  return false;
}

/**
 * Normalize a temporary ID to lowercase for consistent map lookups
 * @param {string} tempId - The temporary ID to normalize
 * @returns {string} Lowercase temporary ID
 */
function normalizeTemporaryId(tempId) {
  return String(tempId).toLowerCase();
}

/**
 * Replace temporary ID references in text with actual issue numbers
 * Format: #aw_XXXX (or #aw_XXXXXXXX) -> #123 (same repo) or owner/repo#123 (cross-repo)
 * @param {string} text - The text to process
 * @param {Map<string, RepoIssuePair>} tempIdMap - Map of temporary_id to {repo, number}
 * @param {string} [currentRepo] - Current repository slug for same-repo references
 * @returns {string} Text with temporary IDs replaced with issue numbers
 */
function replaceTemporaryIdReferences(text, tempIdMap, currentRepo) {
  return text.replace(TEMPORARY_ID_PATTERN, (match, tempId) => {
    const resolved = tempIdMap.get(normalizeTemporaryId(tempId));
    if (resolved !== undefined) {
      // If we have a currentRepo and the issue is in the same repo, use short format
      if (currentRepo && resolved.repo === currentRepo) {
        return `#${resolved.number}`;
      }
      // Otherwise use full repo#number format for cross-repo references
      return `${resolved.repo}#${resolved.number}`;
    }
    // Return original if not found (it may be created later)
    return match;
  });
}

/**
 * Replace temporary ID references in text with actual issue numbers (legacy format)
 * This is a compatibility function that works with Map<string, number>
 * Format: #aw_XXXX (or #aw_XXXXXXXX) -> #123
 * @param {string} text - The text to process
 * @param {Map<string, number>} tempIdMap - Map of temporary_id to issue number
 * @returns {string} Text with temporary IDs replaced with issue numbers
 */
function replaceTemporaryIdReferencesLegacy(text, tempIdMap) {
  return text.replace(TEMPORARY_ID_PATTERN, (match, tempId) => {
    const issueNumber = tempIdMap.get(normalizeTemporaryId(tempId));
    if (issueNumber !== undefined) {
      return `#${issueNumber}`;
    }
    // Return original if not found (it may be created later)
    return match;
  });
}

/**
 * Validate and process a temporary_id from a message
 * Auto-generates a temporary ID if not provided, or validates and normalizes if provided
 *
 * @param {Object} message - The message object that may contain a temporary_id field
 * @param {string} entityType - Type of entity (e.g., "issue", "discussion", "project") for error messages
 * @returns {{temporaryId: string, error: null} | {temporaryId: null, error: string}} Result with temporaryId or error
 */
function getOrGenerateTemporaryId(message, entityType = "item") {
  // Auto-generate if not provided
  if (message.temporary_id === undefined || message.temporary_id === null) {
    return {
      temporaryId: generateTemporaryId(),
      error: null,
    };
  }

  // Validate type
  if (typeof message.temporary_id !== "string") {
    return {
      temporaryId: null,
      error: `temporary_id must be a string (got ${typeof message.temporary_id})`,
    };
  }

  // Normalize and validate format
  const rawTemporaryId = message.temporary_id.trim();
  const normalized = rawTemporaryId.startsWith("#") ? rawTemporaryId.substring(1).trim() : rawTemporaryId;

  if (!isTemporaryId(normalized)) {
    return {
      temporaryId: null,
      error: `Invalid temporary_id format: '${message.temporary_id}'. Temporary IDs must be in format 'aw_' followed by 3 to 8 alphanumeric characters (A-Za-z0-9). Example: 'aw_abc' or 'aw_Test123'`,
    };
  }

  return {
    temporaryId: normalized.toLowerCase(),
    error: null,
  };
}

/**
 * Load the temporary ID map from environment variable
 * Supports both old format (temporary_id -> number) and new format (temporary_id -> {repo, number})
 * @returns {Map<string, RepoIssuePair>} Map of temporary_id to {repo, number}
 */
function loadTemporaryIdMap() {
  const mapJson = process.env.GH_AW_TEMPORARY_ID_MAP;
  if (!mapJson || mapJson === "{}") {
    return new Map();
  }
  try {
    const mapObject = JSON.parse(mapJson);
    /** @type {Map<string, RepoIssuePair>} */
    const result = new Map();

    for (const [key, value] of Object.entries(mapObject)) {
      const normalizedKey = normalizeTemporaryId(key);
      if (typeof value === "number") {
        // Legacy format: number only, use context repo
        const contextRepo = `${context.repo.owner}/${context.repo.repo}`;
        result.set(normalizedKey, { repo: contextRepo, number: value });
      } else if (typeof value === "object" && value !== null && "repo" in value && "number" in value) {
        // New format: {repo, number}
        result.set(normalizedKey, { repo: String(value.repo), number: Number(value.number) });
      }
    }
    return result;
  } catch (error) {
    if (typeof core !== "undefined") {
      core.warning(`Failed to parse temporary ID map: ${getErrorMessage(error)}`);
    }
    return new Map();
  }
}

/**
 * Build a normalized temporary ID map from an object or Map.
 * Supports values in both formats:
 * - number (legacy)
 * - { repo, number }
 *
 * @param {any} resolvedTemporaryIds - Object or Map of temporary IDs to resolved values
 * @returns {Map<string, RepoIssuePair>} Map of normalized temporary_id to {repo, number}
 */
function loadTemporaryIdMapFromResolved(resolvedTemporaryIds) {
  /** @type {Map<string, RepoIssuePair>} */
  const result = new Map();

  if (!resolvedTemporaryIds) {
    return result;
  }

  const contextRepo = typeof context !== "undefined" ? `${context.repo.owner}/${context.repo.repo}` : "";

  const entries = resolvedTemporaryIds instanceof Map ? Array.from(resolvedTemporaryIds.entries()) : Object.entries(resolvedTemporaryIds);
  for (const [key, value] of entries) {
    const normalizedKey = normalizeTemporaryId(key);
    if (typeof value === "number") {
      result.set(normalizedKey, { repo: contextRepo, number: value });
      continue;
    }
    if (typeof value === "object" && value !== null) {
      if ("repo" in value && "number" in value) {
        result.set(normalizedKey, { repo: String(value.repo), number: Number(value.number) });
        continue;
      }
      if ("number" in value) {
        result.set(normalizedKey, { repo: contextRepo, number: Number(value.number) });
        continue;
      }
    }
  }

  return result;
}

/**
 * Resolve an issue number that may be a temporary ID or an actual issue number
 * Returns structured result with the resolved number, repo, and metadata
 * @param {any} value - The value to resolve (can be temporary ID, number, or string)
 * @param {Map<string, any>} temporaryIdMap - Map of temporary ID to resolved value (supports legacy formats)
 * @returns {{resolved: RepoIssuePair|null, wasTemporaryId: boolean, errorMessage: string|null}}
 */
function resolveIssueNumber(value, temporaryIdMap) {
  if (value === undefined || value === null) {
    return { resolved: null, wasTemporaryId: false, errorMessage: "Issue number is missing" };
  }

  // Strip # prefix if present to allow flexible temporary ID format
  const valueStr = String(value).trim();
  const valueWithoutHash = valueStr.startsWith("#") ? valueStr.substring(1) : valueStr;

  // Check if it's a temporary ID
  if (isTemporaryId(valueWithoutHash)) {
    const resolvedPair = temporaryIdMap.get(normalizeTemporaryId(valueWithoutHash));
    if (resolvedPair !== undefined) {
      // Support legacy format where the map value is the issue number.
      const contextRepo = typeof context !== "undefined" ? `${context.repo.owner}/${context.repo.repo}` : "";
      if (typeof resolvedPair === "number") {
        return { resolved: { repo: contextRepo, number: resolvedPair }, wasTemporaryId: true, errorMessage: null };
      }
      if (typeof resolvedPair === "object" && resolvedPair !== null) {
        if ("repo" in resolvedPair && "number" in resolvedPair) {
          return {
            resolved: { repo: String(resolvedPair.repo), number: Number(resolvedPair.number) },
            wasTemporaryId: true,
            errorMessage: null,
          };
        }
        if ("number" in resolvedPair) {
          return { resolved: { repo: contextRepo, number: Number(resolvedPair.number) }, wasTemporaryId: true, errorMessage: null };
        }
      }
    }
    return {
      resolved: null,
      wasTemporaryId: true,
      errorMessage: `Temporary ID '${valueStr}' not found in map. Ensure the issue was created before linking.`,
    };
  }

  // Check if it looks like a malformed temporary ID
  if (valueWithoutHash.startsWith("aw_")) {
    return {
      resolved: null,
      wasTemporaryId: false,
      errorMessage: `Invalid temporary ID format: '${valueStr}'. Temporary IDs must be in format 'aw_' followed by 3 to 8 alphanumeric characters (A-Za-z0-9). Example: 'aw_abc' or 'aw_abc12345'`,
    };
  }

  // It's a real issue number - use context repo as default
  const issueNumber = typeof value === "number" ? value : parseInt(valueWithoutHash, 10);
  if (isNaN(issueNumber) || issueNumber <= 0) {
    return { resolved: null, wasTemporaryId: false, errorMessage: `Invalid issue number: ${value}. Expected either a valid temporary ID (format: aw_ followed by 3-8 alphanumeric characters) or a numeric issue number.` };
  }

  const contextRepo = typeof context !== "undefined" ? `${context.repo.owner}/${context.repo.repo}` : "";
  return { resolved: { repo: contextRepo, number: issueNumber }, wasTemporaryId: false, errorMessage: null };
}

/**
 * Resolve an issue number that may be a temporary ID and return a concrete owner/repo/number triple.
 *
 * @param {any} value - The value to resolve
 * @param {Map<string, RepoIssuePair>} temporaryIdMap - Normalized map of temporary IDs to {repo, number}
 * @param {string} defaultOwner - Fallback owner when repo slug isn't available
 * @param {string} defaultRepo - Fallback repo when repo slug isn't available
 * @returns {{resolved: {owner: string, repo: string, number: number}|null, wasTemporaryId: boolean, errorMessage: string|null}}
 */
function resolveRepoIssueTarget(value, temporaryIdMap, defaultOwner, defaultRepo) {
  const result = resolveIssueNumber(value, temporaryIdMap);
  if (!result.resolved) {
    return { resolved: null, wasTemporaryId: result.wasTemporaryId, errorMessage: result.errorMessage };
  }

  // For non-temporary numeric issue numbers, prefer the caller-provided default repo.
  // For temporary IDs, the resolved repo (if present) should win.
  const defaultRepoSlug = defaultOwner && defaultRepo ? `${defaultOwner}/${defaultRepo}` : "";
  const repoSlug = result.wasTemporaryId ? result.resolved.repo || defaultRepoSlug : defaultRepoSlug || result.resolved.repo;
  const parts = String(repoSlug).split("/");
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return {
      resolved: null,
      wasTemporaryId: result.wasTemporaryId,
      errorMessage: `Invalid repository slug '${repoSlug}' while resolving issue target (expected 'owner/repo')`,
    };
  }

  return {
    resolved: { owner: parts[0], repo: parts[1], number: result.resolved.number },
    wasTemporaryId: result.wasTemporaryId,
    errorMessage: null,
  };
}

/**
 * Check if text contains unresolved temporary ID references
 * An unresolved temporary ID is one that appears in the text but is not in the tempIdMap
 * @param {string} text - The text to check for unresolved temporary IDs
 * @param {Map<string, RepoIssuePair>|Object} tempIdMap - Map or object of temporary_id to {repo, number}
 * @returns {boolean} True if text contains any unresolved temporary IDs
 */
function hasUnresolvedTemporaryIds(text, tempIdMap) {
  if (!text || typeof text !== "string") {
    return false;
  }

  // Convert tempIdMap to Map if it's a plain object
  const map = tempIdMap instanceof Map ? tempIdMap : new Map(Object.entries(tempIdMap || {}));

  // Find all temporary ID references in the text
  const matches = text.matchAll(TEMPORARY_ID_PATTERN);

  for (const match of matches) {
    const tempId = match[1]; // The captured group (aw_XXXXXXXXXXXX)
    const normalizedId = normalizeTemporaryId(tempId);

    // If this temp ID is not in the map, it's unresolved
    if (!map.has(normalizedId)) {
      return true;
    }
  }

  return false;
}

/**
 * Serialize the temporary ID map to JSON for output
 * @param {Map<string, RepoIssuePair>} tempIdMap - Map of temporary_id to {repo, number}
 * @returns {string} JSON string of the map
 */
function serializeTemporaryIdMap(tempIdMap) {
  const obj = Object.fromEntries(tempIdMap);
  return JSON.stringify(obj);
}

/**
 * Load the temporary project map from environment variable
 * @returns {Map<string, string>} Map of temporary_project_id to project URL
 */
function loadTemporaryProjectMap() {
  const mapJson = process.env.GH_AW_TEMPORARY_PROJECT_MAP;
  if (!mapJson || mapJson === "{}") {
    return new Map();
  }
  try {
    const mapObject = JSON.parse(mapJson);
    /** @type {Map<string, string>} */
    const result = new Map();

    for (const [key, value] of Object.entries(mapObject)) {
      const normalizedKey = normalizeTemporaryId(key);
      if (typeof value === "string") {
        result.set(normalizedKey, value);
      }
    }
    return result;
  } catch (error) {
    if (typeof core !== "undefined") {
      core.warning(`Failed to parse temporary project map: ${getErrorMessage(error)}`);
    }
    return new Map();
  }
}

/**
 * Replace temporary project ID references in text with actual project URLs
 * Format: #aw_XXXX (or #aw_XXXXXXXX) -> https://github.com/orgs/myorg/projects/123
 * @param {string} text - The text to process
 * @param {Map<string, string>} tempProjectMap - Map of temporary_project_id to project URL
 * @returns {string} Text with temporary project IDs replaced with project URLs
 */
function replaceTemporaryProjectReferences(text, tempProjectMap) {
  return text.replace(TEMPORARY_ID_PATTERN, (match, tempId) => {
    const resolved = tempProjectMap.get(normalizeTemporaryId(tempId));
    if (resolved !== undefined) {
      return resolved;
    }
    // Return original if not found (it may be an issue ID)
    return match;
  });
}

/**
 * Extract all temporary ID references from a message
 * Checks fields that commonly contain temporary IDs:
 * - body (for create_issue, create_discussion, add_comment)
 * - parent_issue_number, sub_issue_number (for link_sub_issue)
 * - issue_number (for add_comment, update_issue, etc.)
 * - discussion_number (for create_discussion, update_discussion)
 *
 * @param {any} message - The safe output message
 * @returns {Set<string>} Set of normalized temporary IDs referenced by this message
 */
function extractTemporaryIdReferences(message) {
  const tempIds = new Set();

  if (!message || typeof message !== "object") {
    return tempIds;
  }

  // Check text fields for #aw_XXXXXXXXXXXX references
  const textFields = ["body", "title", "description"];
  for (const field of textFields) {
    if (typeof message[field] === "string") {
      let match;
      while ((match = TEMPORARY_ID_PATTERN.exec(message[field])) !== null) {
        tempIds.add(normalizeTemporaryId(match[1]));
      }
    }
  }

  // Check direct ID reference fields
  const idFields = ["parent_issue_number", "sub_issue_number", "issue_number", "discussion_number", "pull_request_number", "content_number"];

  for (const field of idFields) {
    const value = message[field];
    if (value !== undefined && value !== null) {
      // Strip # prefix if present
      const valueStr = String(value).trim();
      const valueWithoutHash = valueStr.startsWith("#") ? valueStr.substring(1) : valueStr;

      if (isTemporaryId(valueWithoutHash)) {
        tempIds.add(normalizeTemporaryId(valueWithoutHash));
      }
    }
  }

  // Check URL fields that may contain temporary IDs instead of issue numbers
  // Format: https://github.com/owner/repo/issues/#aw_XXXXXXXXXXXX or just #aw_XXXXXXXXXXXX
  const urlFields = ["item_url"];

  for (const field of urlFields) {
    const value = message[field];
    if (value !== undefined && value !== null && typeof value === "string") {
      // Extract potential temporary ID from URL or plain ID
      // Match: https://github.com/owner/repo/issues/#aw_XXX or #aw_XXXXXXXX
      const urlMatch = value.match(/issues\/(#?aw_[A-Za-z0-9]{3,8})\s*$/i);
      if (urlMatch) {
        const valueWithoutHash = urlMatch[1].startsWith("#") ? urlMatch[1].substring(1) : urlMatch[1];
        if (isTemporaryId(valueWithoutHash)) {
          tempIds.add(normalizeTemporaryId(valueWithoutHash));
        }
      } else {
        // Also check if the entire value is a temporary ID (with or without #)
        const valueStr = String(value).trim();
        const valueWithoutHash = valueStr.startsWith("#") ? valueStr.substring(1) : valueStr;
        if (isTemporaryId(valueWithoutHash)) {
          tempIds.add(normalizeTemporaryId(valueWithoutHash));
        }
      }
    }
  }

  // Check items array for bulk operations (e.g., add_comment with multiple targets)
  if (Array.isArray(message.items)) {
    for (const item of message.items) {
      if (item && typeof item === "object") {
        const itemTempIds = extractTemporaryIdReferences(item);
        for (const tempId of itemTempIds) {
          tempIds.add(tempId);
        }
      }
    }
  }

  return tempIds;
}

/**
 * Get the temporary ID that a message will create (if any)
 * Only messages with a temporary_id field will create a new entity
 *
 * @param {any} message - The safe output message
 * @returns {string|null} Normalized temporary ID that will be created, or null
 */
function getCreatedTemporaryId(message) {
  if (!message || typeof message !== "object") {
    return null;
  }

  const tempId = message.temporary_id;
  if (tempId && isTemporaryId(String(tempId))) {
    return normalizeTemporaryId(String(tempId));
  }

  return null;
}

module.exports = {
  TEMPORARY_ID_PATTERN,
  generateTemporaryId,
  isTemporaryId,
  normalizeTemporaryId,
  getOrGenerateTemporaryId,
  replaceTemporaryIdReferences,
  replaceTemporaryIdReferencesLegacy,
  loadTemporaryIdMap,
  loadTemporaryIdMapFromResolved,
  resolveIssueNumber,
  resolveRepoIssueTarget,
  hasUnresolvedTemporaryIds,
  serializeTemporaryIdMap,
  loadTemporaryProjectMap,
  replaceTemporaryProjectReferences,
  extractTemporaryIdReferences,
  getCreatedTemporaryId,
};
