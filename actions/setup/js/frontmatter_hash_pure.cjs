// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const yaml = require("js-yaml");

// Version information - should match Go constants
const VERSIONS = {
  "gh-aw": "dev",
  awf: "v0.11.2",
  agents: "v0.0.84",
  gateway: "v0.0.84",
};

/**
 * Computes a deterministic SHA-256 hash of workflow frontmatter
 * Pure JavaScript implementation without Go binary dependency
 *
 * @param {string} workflowPath - Path to the workflow file
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeFrontmatterHash(workflowPath) {
  const content = fs.readFileSync(workflowPath, "utf8");
  
  // Extract frontmatter and markdown body
  const { frontmatter, markdown } = extractFrontmatterAndBody(content);
  
  // Get base directory for resolving imports
  const baseDir = path.dirname(workflowPath);
  
  // Extract template expressions with env. or vars.
  const expressions = extractRelevantTemplateExpressions(markdown);
  
  // Process imports
  const { importedFiles, importedFrontmatters } = await processImports(frontmatter, baseDir);
  
  // Build canonical representation by concatenating frontmatters in sorted import order
  const canonical = buildCanonicalFrontmatterWithImports(frontmatter, importedFiles, importedFrontmatters);
  
  // Add template expressions if present
  if (expressions.length > 0) {
    canonical["template-expressions"] = expressions;
  }
  
  // Add version information
  canonical.versions = VERSIONS;
  
  // Serialize to canonical JSON
  const canonicalJSON = marshalCanonicalJSON(canonical);
  
  // Compute SHA-256 hash
  const hash = crypto.createHash("sha256").update(canonicalJSON, "utf8").digest("hex");
  
  return hash;
}

/**
 * Extracts frontmatter and markdown body from workflow content
 * @param {string} content - The markdown content
 * @returns {{frontmatter: Record<string, any>, markdown: string}} The parsed frontmatter and body
 */
function extractFrontmatterAndBody(content) {
  const lines = content.split("\n");
  
  if (lines.length === 0 || lines[0].trim() !== "---") {
    return { frontmatter: {}, markdown: content };
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
  
  const frontmatterLines = lines.slice(1, endIndex);
  const markdown = lines.slice(endIndex + 1).join("\n");
  
  // Parse YAML frontmatter using js-yaml
  const yamlContent = frontmatterLines.join("\n");
  let frontmatter = {};
  
  try {
    frontmatter = yaml.load(yamlContent) || {};
  } catch (err) {
    const error = /** @type {Error} */ (err);
    throw new Error(`Failed to parse YAML frontmatter: ${error.message}`);
  }
  
  return { frontmatter, markdown };
}

/**
 * Process imports from frontmatter (simplified concatenation approach)
 * Concatenates frontmatter from imports in sorted order
 * @param {Record<string, any>} frontmatter - The frontmatter object
 * @param {string} baseDir - Base directory for resolving imports
 * @param {Set<string>} visited - Set of visited files to prevent cycles
 * @returns {Promise<{importedFiles: string[], importedFrontmatters: Array<Record<string, any>>}>}
 */
async function processImports(frontmatter, baseDir, visited = new Set()) {
  const importedFiles = [];
  const importedFrontmatters = [];
  
  // Check if imports field exists
  if (!frontmatter.imports || !Array.isArray(frontmatter.imports)) {
    return { importedFiles, importedFrontmatters };
  }
  
  // Sort imports for deterministic processing
  const sortedImports = [...frontmatter.imports].sort();
  
  for (const importPath of sortedImports) {
    // Skip if string is not provided (handle object imports by skipping)
    if (typeof importPath !== "string") continue;
    
    // Resolve import path relative to base directory
    const fullPath = path.resolve(baseDir, importPath);
    
    // Skip if already visited (cycle detection)
    if (visited.has(fullPath)) continue;
    visited.add(fullPath);
    
    // Read imported file
    try {
      if (!fs.existsSync(fullPath)) {
        // Skip missing imports silently
        continue;
      }
      
      const importContent = fs.readFileSync(fullPath, "utf8");
      const { frontmatter: importFrontmatter } = extractFrontmatterAndBody(importContent);
      
      // Add to imported files list
      importedFiles.push(importPath);
      importedFrontmatters.push(importFrontmatter);
      
      // Recursively process imports in the imported file
      const importBaseDir = path.dirname(fullPath);
      const nestedResult = await processImports(importFrontmatter, importBaseDir, visited);
      
      // Add nested imports
      importedFiles.push(...nestedResult.importedFiles);
      importedFrontmatters.push(...nestedResult.importedFrontmatters);
    } catch (err) {
      // Skip files that can't be read
      continue;
    }
  }
  
  return { importedFiles, importedFrontmatters };
}

/**
 * Simple YAML parser for frontmatter
 * Handles basic YAML structures commonly used in agentic workflows
 * @param {string} yamlContent - YAML content to parse
 * @returns {Record<string, any>} Parsed object
 */
function parseSimpleYAML(yamlContent) {
  const result = {};
  const lines = yamlContent.split("\n");
  let currentKey = null;
  let currentValue = null;
  let indent = 0;
  let inArray = false;
  let arrayItems = [];
  
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();
    
    // Skip empty lines and comments
    if (!trimmed || trimmed.startsWith("#")) continue;
    
    // Calculate indentation
    const lineIndent = line.search(/\S/);
    
    // Handle key-value pairs
    const colonIndex = trimmed.indexOf(":");
    if (colonIndex > 0) {
      // Save previous array if exists
      if (inArray && currentKey) {
        result[currentKey] = arrayItems;
        arrayItems = [];
        inArray = false;
      }
      
      const key = trimmed.substring(0, colonIndex).trim();
      let value = trimmed.substring(colonIndex + 1).trim();
      
      // Handle different value types
      if (!value) {
        // Multi-line or nested object - peek next line
        if (i + 1 < lines.length) {
          const nextLine = lines[i + 1];
          const nextTrimmed = nextLine.trim();
          if (nextTrimmed.startsWith("-")) {
            // Array coming
            currentKey = key;
            inArray = true;
            arrayItems = [];
            continue;
          }
        }
        result[key] = null;
      } else if (value === "true" || value === "false") {
        result[key] = value === "true";
      } else if (/^-?\d+$/.test(value)) {
        result[key] = parseInt(value, 10);
      } else if (/^-?\d*\.\d+$/.test(value)) {
        result[key] = parseFloat(value);
      } else {
        // String value - remove quotes if present
        value = value.replace(/^["']|["']$/g, "");
        result[key] = value;
      }
    } else if (trimmed.startsWith("-") && inArray) {
      // Array item
      let item = trimmed.substring(1).trim();
      item = item.replace(/^["']|["']$/g, "");
      arrayItems.push(item);
    }
  }
  
  // Save final array if exists
  if (inArray && currentKey) {
    result[currentKey] = arrayItems;
  }
  
  return result;
}

/**
 * Extract template expressions containing env. or vars.
 * @param {string} markdown - The markdown body
 * @returns {string[]} Array of relevant expressions (sorted)
 */
function extractRelevantTemplateExpressions(markdown) {
  const expressions = [];
  const regex = /\$\{\{([^}]+)\}\}/g;
  let match;
  
  while ((match = regex.exec(markdown)) !== null) {
    const expr = match[0]; // Full expression including ${{ }}
    const content = match[1].trim();
    
    // Check if it contains env. or vars.
    if (content.includes("env.") || content.includes("vars.")) {
      expressions.push(expr);
    }
  }
  
  // Remove duplicates and sort
  return [...new Set(expressions)].sort();
}

/**
 * Builds canonical frontmatter representation with import data
 * Concatenates frontmatter from imports in sorted order for deterministic hashing
 * @param {Record<string, any>} frontmatter - The main frontmatter
 * @param {string[]} importedFiles - List of imported file paths (sorted)
 * @param {Array<Record<string, any>>} importedFrontmatters - Frontmatter from imported files
 * @returns {Record<string, any>} Canonical frontmatter object
 */
function buildCanonicalFrontmatterWithImports(frontmatter, importedFiles, importedFrontmatters) {
  const canonical = {};
  
  // Helper to add field if exists
  const addField = (key) => {
    if (frontmatter[key] !== undefined) {
      canonical[key] = frontmatter[key];
    }
  };
  
  // Core configuration fields (order matches Go implementation)
  addField("engine");
  addField("on");
  addField("permissions");
  addField("tracker-id");
  
  // Tool and integration fields
  addField("tools");
  addField("mcp-servers");
  addField("network");
  addField("safe-outputs");
  addField("safe-inputs");
  
  // Runtime configuration fields
  addField("runtimes");
  addField("services");
  addField("cache");
  
  // Workflow structure fields
  addField("steps");
  addField("post-steps");
  addField("jobs");
  
  // Trigger and scheduling
  addField("manual-approval");
  addField("stop-time");
  
  // Metadata fields
  addField("description");
  addField("labels");
  addField("bots");
  addField("timeout-minutes");
  addField("secret-masking");
  
  // Input parameter definitions
  addField("inputs");
  
  // Add sorted imported files list
  if (importedFiles.length > 0) {
    canonical.imports = [...importedFiles].sort();
  }
  
  return canonical;
}

/**
 * Builds canonical frontmatter representation (legacy function)
 * @param {Record<string, any>} frontmatter - The main frontmatter
 * @param {string} baseDir - Base directory for resolving imports
 * @param {Record<string, any>} cache - Import cache
 * @returns {Promise<Record<string, any>>} Canonical frontmatter object
 */
async function buildCanonicalFrontmatter(frontmatter, baseDir, cache) {
  const { importedFiles, importedFrontmatters } = await processImports(frontmatter, baseDir);
  return buildCanonicalFrontmatterWithImports(frontmatter, importedFiles, importedFrontmatters);
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

/**
 * Extracts hash from lock file header
 * @param {string} lockFileContent - Content of the lock file
 * @returns {string} The hash or empty string if not found
 */
function extractHashFromLockFile(lockFileContent) {
  const hashLine = lockFileContent.split("\n").find(line => line.startsWith("# frontmatter-hash: "));
  return hashLine ? hashLine.substring(20).trim() : "";
}

module.exports = {
  computeFrontmatterHash,
  extractFrontmatterAndBody,
  parseSimpleYAML,
  extractRelevantTemplateExpressions,
  buildCanonicalFrontmatter,
  buildCanonicalFrontmatterWithImports,
  processImports,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
};
