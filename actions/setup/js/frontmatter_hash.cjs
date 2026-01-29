// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

/**
 * Computes a deterministic SHA-256 hash of workflow frontmatter
 * This is a simplified implementation that delegates to the Go binary
 * for maximum compatibility and correctness.
 *
 * @param {string} workflowPath - Path to the workflow file
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeFrontmatterHash(workflowPath) {
  // For now, this is a placeholder that computes a simple hash
  // The full implementation will call the Go binary
  const content = fs.readFileSync(workflowPath, "utf8");
  const frontmatter = extractFrontmatter(content);
  const canonical = buildCanonicalFrontmatter(frontmatter, {
    importedFiles: [],
    mergedEngines: [],
    mergedLabels: [],
    mergedBots: [],
  });
  const canonicalJSON = marshalCanonicalJSON(canonical);
  const hash = crypto.createHash("sha256").update(canonicalJSON, "utf8").digest("hex");
  return hash;
}

/**
 * Extracts frontmatter from markdown content
 * NOTE: This is a simplified placeholder. For production use, this should
 * parse YAML properly or call the Go implementation.
 * 
 * @param {string} content - The markdown content
 * @returns {Record<string, any>} The parsed frontmatter object
 */
function extractFrontmatter(content) {
  const lines = content.split("\n");
  if (lines.length === 0 || lines[0].trim() !== "---") {
    return {};
  }

  let endIndex = -1;
  for (let i = 1; i < lines.length; i++) {
    if (lines[i].trim() === "---") {
      endIndex = i;
      break;
    }
  }

  if (endIndex === -1) {
    throw new Error("Frontmatter not properly closed");
  }

  // TODO: Implement proper YAML parsing or call Go binary
  // For now, return empty object - the hash will still be deterministic
  // but won't include frontmatter content
  console.warn("extractFrontmatter: YAML parsing not fully implemented, returning empty object");
  return {};
}

/**
 * Builds canonical frontmatter representation
 * @param {Record<string, any>} frontmatter - The main frontmatter
 * @param {any} importsResult - The imports processing results
 * @returns {Record<string, any>} Canonical frontmatter object
 */
function buildCanonicalFrontmatter(frontmatter, importsResult) {
  const canonical = {};

  const addField = (/** @type {string} */ key) => {
    if (frontmatter[key] !== undefined) {
      canonical[key] = frontmatter[key];
    }
  };

  addField("engine");
  addField("on");
  addField("permissions");
  addField("tracker-id");
  addField("tools");
  addField("description");

  if (importsResult.importedFiles && importsResult.importedFiles.length > 0) {
    canonical.imports = importsResult.importedFiles;
  }

  return canonical;
}

/**
 * Marshals data to canonical JSON with sorted keys
 * @param {Record<string, any>} data - The data to marshal
 * @returns {string} Canonical JSON string
 */
function marshalCanonicalJSON(data) {
  return marshalSorted(data);
}

/**
 * Recursively marshals data with sorted keys
 * @param {any} data - The data to marshal
 * @returns {string} JSON string with sorted keys
 */
function marshalSorted(data) {
  if (data === null || data === undefined) {
    return "null";
  }

  const type = typeof data;

  if (type === "string" || type === "number" || type === "boolean") {
    return JSON.stringify(data);
  }

  if (Array.isArray(data)) {
    if (data.length === 0) return "[]";
    const elements = data.map(elem => marshalSorted(elem));
    return "[" + elements.join(",") + "]";
  }

  if (type === "object") {
    const keys = Object.keys(data).sort();
    if (keys.length === 0) return "{}";
    const pairs = keys.map(key => {
      const keyJSON = JSON.stringify(key);
      const valueJSON = marshalSorted(data[key]);
      return keyJSON + ":" + valueJSON;
    });
    return "{" + pairs.join(",") + "}";
  }

  return JSON.stringify(data);
}

module.exports = {
  computeFrontmatterHash,
  extractFrontmatter,
  buildCanonicalFrontmatter,
  marshalCanonicalJSON,
  marshalSorted,
};
