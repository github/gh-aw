// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

/**
 * Computes a deterministic SHA-256 hash of workflow frontmatter
 * including contributions from all imported workflows.
 *
 * This implementation follows the Frontmatter Hash Specification (v1.0)
 * and must produce identical hashes to the Go implementation.
 *
 * @param {string} workflowPath - Path to the workflow file
 * @returns {string} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeFrontmatterHash(workflowPath) {
  // Read the workflow file
  const content = fs.readFileSync(workflowPath, "utf8");

  // Extract frontmatter
  const frontmatter = extractFrontmatter(content);

  // Process imports recursively (BFS)
  const baseDir = path.dirname(workflowPath);
  const importsResult = await processImports(frontmatter, baseDir);

  // Build canonical frontmatter
  const canonical = buildCanonicalFrontmatter(frontmatter, importsResult);

  // Serialize to canonical JSON
  const canonicalJSON = marshalCanonicalJSON(canonical);

  // Compute SHA-256 hash
  const hash = crypto.createHash("sha256").update(canonicalJSON, "utf8").digest("hex");

  return hash;
}

/**
 * Extracts frontmatter from markdown content
 * @param {string} content - The markdown content
 * @returns {Object} The parsed frontmatter object
 */
function extractFrontmatter(content) {
  const lines = content.split("\n");

  // Check for frontmatter delimiter
  if (lines.length === 0 || lines[0].trim() !== "---") {
    return {};
  }

  // Find end of frontmatter
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

  // Extract and parse YAML
  const frontmatterLines = lines.slice(1, endIndex);
  const frontmatterYAML = frontmatterLines.join("\n");

  // Simple YAML parsing (for basic structures)
  // For production, use a proper YAML parser like 'js-yaml'
  try {
    const yaml = require("js-yaml");
    return yaml.load(frontmatterYAML) || {};
  } catch (err) {
    throw new Error(`Failed to parse frontmatter: ${err.message}`);
  }
}

/**
 * Processes imports recursively in BFS order
 * @param {Object} frontmatter - The frontmatter object
 * @param {string} baseDir - Base directory for resolving imports
 * @returns {Object} Import processing results
 */
async function processImports(frontmatter, baseDir) {
  const result = {
    mergedTools: "",
    mergedMCPServers: "",
    mergedEngines: [],
    mergedSafeOutputs: [],
    mergedSafeInputs: [],
    mergedSteps: "",
    mergedRuntimes: "",
    mergedServices: "",
    mergedNetwork: "",
    mergedPermissions: "",
    mergedSecretMasking: "",
    mergedBots: [],
    mergedPostSteps: "",
    mergedLabels: [],
    mergedCaches: [],
    importedFiles: [],
    agentFile: "",
    importInputs: {},
  };

  // Check if imports field exists
  if (!frontmatter.imports || !Array.isArray(frontmatter.imports)) {
    return result;
  }

  // BFS queue and visited set
  const queue = [];
  const visited = new Set();

  // Seed the queue with initial imports
  for (const importItem of frontmatter.imports) {
    const importPath = typeof importItem === "string" ? importItem : importItem.path;
    if (!importPath) continue;

    // Handle section references (file.md#Section)
    const [filePath, sectionName] = importPath.includes("#")
      ? importPath.split("#", 2)
      : [importPath, null];

    // Resolve import path
    const fullPath = path.resolve(baseDir, filePath);

    if (!visited.has(fullPath)) {
      visited.add(fullPath);
      queue.push({
        importPath,
        fullPath,
        sectionName,
        baseDir,
        inputs: typeof importItem === "object" ? importItem.inputs || {} : {},
      });
      result.importedFiles.push(importPath);
    }
  }

  // Process queue (BFS)
  while (queue.length > 0) {
    const item = queue.shift();

    // Check if this is an agent file
    const isAgentFile = item.fullPath.includes("/.github/agents/") && 
                        item.fullPath.toLowerCase().endsWith(".md");
    if (isAgentFile) {
      // Extract relative path from .github/ onwards
      const idx = item.fullPath.indexOf("/.github/");
      if (idx >= 0) {
        result.agentFile = item.fullPath.substring(idx + 1);
      }
      continue; // Agent files don't contribute frontmatter
    }

    // Read and process imported file
    try {
      const content = fs.readFileSync(item.fullPath, "utf8");
      const importedFrontmatter = extractFrontmatter(content);

      // Merge inputs
      Object.assign(result.importInputs, item.inputs);

      // Process nested imports
      if (importedFrontmatter.imports && Array.isArray(importedFrontmatter.imports)) {
        for (const nestedImportItem of importedFrontmatter.imports) {
          const nestedImportPath = typeof nestedImportItem === "string" 
            ? nestedImportItem 
            : nestedImportItem.path;
          if (!nestedImportPath) continue;

          const [nestedFilePath] = nestedImportPath.includes("#")
            ? nestedImportPath.split("#", 2)
            : [nestedImportPath, null];

          const nestedFullPath = path.resolve(path.dirname(item.fullPath), nestedFilePath);

          if (!visited.has(nestedFullPath)) {
            visited.add(nestedFullPath);
            queue.push({
              importPath: nestedImportPath,
              fullPath: nestedFullPath,
              sectionName: null,
              baseDir: path.dirname(item.fullPath),
              inputs: typeof nestedImportItem === "object" ? nestedImportItem.inputs || {} : {},
            });
            result.importedFiles.push(nestedImportPath);
          }
        }
      }

      // Merge frontmatter fields
      mergeFrontmatterFields(result, importedFrontmatter);
    } catch (err) {
      // Continue on error (matching Go behavior)
      console.error(`Failed to process import ${item.fullPath}: ${err.message}`);
    }
  }

  return result;
}

/**
 * Merges frontmatter fields from an imported workflow
 * @param {Object} result - The accumulated import results
 * @param {Object} frontmatter - The imported frontmatter
 */
function mergeFrontmatterFields(result, frontmatter) {
  // This is a simplified version - full implementation would merge tools, engines, etc.
  // For the hash computation, we primarily care about tracking what was imported
  
  if (frontmatter.tools) {
    const toolsJSON = JSON.stringify(frontmatter.tools);
    result.mergedTools = result.mergedTools ? 
      mergeJSON(result.mergedTools, toolsJSON) : toolsJSON;
  }

  if (frontmatter.engine) {
    if (!result.mergedEngines.includes(frontmatter.engine)) {
      result.mergedEngines.push(frontmatter.engine);
    }
  }

  if (frontmatter.labels && Array.isArray(frontmatter.labels)) {
    for (const label of frontmatter.labels) {
      if (!result.mergedLabels.includes(label)) {
        result.mergedLabels.push(label);
      }
    }
  }

  if (frontmatter.bots && Array.isArray(frontmatter.bots)) {
    for (const bot of frontmatter.bots) {
      if (!result.mergedBots.includes(bot)) {
        result.mergedBots.push(bot);
      }
    }
  }
}

/**
 * Merges two JSON strings
 * @param {string} json1 - First JSON string
 * @param {string} json2 - Second JSON string
 * @returns {string} Merged JSON string
 */
function mergeJSON(json1, json2) {
  const obj1 = JSON.parse(json1);
  const obj2 = JSON.parse(json2);
  return JSON.stringify(Object.assign({}, obj1, obj2));
}

/**
 * Builds canonical frontmatter representation
 * @param {Object} frontmatter - The main frontmatter
 * @param {Object} importsResult - The imports processing results
 * @returns {Object} Canonical frontmatter object
 */
function buildCanonicalFrontmatter(frontmatter, importsResult) {
  const canonical = {};

  // Helper to add field if exists
  const addField = (key) => {
    if (frontmatter[key] !== undefined) {
      canonical[key] = frontmatter[key];
    }
  };

  // Helper to add non-empty string
  const addString = (key, value) => {
    if (value && value !== "") {
      canonical[key] = value;
    }
  };

  // Helper to add non-empty array
  const addArray = (key, value) => {
    if (value && value.length > 0) {
      canonical[key] = value;
    }
  };

  // Core configuration fields
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

  // Metadata fields
  addField("description");
  addField("labels");
  addField("bots");
  addField("timeout-minutes");
  addField("secret-masking");

  // Input parameter definitions
  addField("inputs");

  // Add merged content from imports
  addString("merged-tools", importsResult.mergedTools);
  addString("merged-mcp-servers", importsResult.mergedMCPServers);
  addArray("merged-engines", importsResult.mergedEngines);
  addArray("merged-safe-outputs", importsResult.mergedSafeOutputs);
  addArray("merged-safe-inputs", importsResult.mergedSafeInputs);
  addString("merged-steps", importsResult.mergedSteps);
  addString("merged-runtimes", importsResult.mergedRuntimes);
  addString("merged-services", importsResult.mergedServices);
  addString("merged-network", importsResult.mergedNetwork);
  addString("merged-permissions", importsResult.mergedPermissions);
  addString("merged-secret-masking", importsResult.mergedSecretMasking);
  addArray("merged-bots", importsResult.mergedBots);
  addString("merged-post-steps", importsResult.mergedPostSteps);
  addArray("merged-labels", importsResult.mergedLabels);
  addArray("merged-caches", importsResult.mergedCaches);

  // Add list of imported files
  if (importsResult.importedFiles.length > 0) {
    canonical.imports = importsResult.importedFiles;
  }

  // Add agent file if present
  if (importsResult.agentFile) {
    canonical["agent-file"] = importsResult.agentFile;
  }

  // Add import inputs if present
  if (Object.keys(importsResult.importInputs).length > 0) {
    canonical["import-inputs"] = importsResult.importInputs;
  }

  return canonical;
}

/**
 * Marshals data to canonical JSON with sorted keys
 * @param {*} data - The data to marshal
 * @returns {string} Canonical JSON string
 */
function marshalCanonicalJSON(data) {
  return marshalSorted(data);
}

/**
 * Recursively marshals data with sorted keys
 * @param {*} data - The data to marshal
 * @returns {string} JSON string with sorted keys
 */
function marshalSorted(data) {
  if (data === null) {
    return "null";
  }

  if (data === undefined) {
    return "null";
  }

  const type = typeof data;

  if (type === "string") {
    return JSON.stringify(data);
  }

  if (type === "number" || type === "boolean") {
    return JSON.stringify(data);
  }

  if (Array.isArray(data)) {
    if (data.length === 0) {
      return "[]";
    }
    const elements = data.map(elem => marshalSorted(elem));
    return "[" + elements.join(",") + "]";
  }

  if (type === "object") {
    const keys = Object.keys(data).sort();
    if (keys.length === 0) {
      return "{}";
    }

    const pairs = keys.map(key => {
      const keyJSON = JSON.stringify(key);
      const valueJSON = marshalSorted(data[key]);
      return keyJSON + ":" + valueJSON;
    });

    return "{" + pairs.join(",") + "}";
  }

  // Fallback to standard JSON
  return JSON.stringify(data);
}

module.exports = {
  computeFrontmatterHash,
  extractFrontmatter,
  buildCanonicalFrontmatter,
  marshalCanonicalJSON,
  marshalSorted,
};
