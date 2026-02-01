// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");

// Version information - should match Go constants
// Note: gh-aw version is excluded for non-release builds to prevent
// hash changes during development. Only include it for release builds.
const VERSIONS = {
  // "gh-aw": "dev", // Excluded for non-release builds
  awf: "v0.11.2",
  agents: "v0.0.86",
  gateway: "v0.0.86",
};

/**
 * Default file reader using Node.js fs module
 * @param {string} filePath - Path to the file
 * @returns {Promise<string>} File content
 */
async function defaultFileReader(filePath) {
  return fs.readFileSync(filePath, "utf8");
}

/**
 * Computes a deterministic SHA-256 hash of workflow frontmatter
 * Pure JavaScript implementation without Go binary dependency
 * Uses text-based parsing only - no YAML library dependencies
 *
 * @param {string} workflowPath - Path to the workflow file
 * @param {Object} [options] - Optional configuration
 * @param {Function} [options.fileReader] - Custom file reader function (async (filePath) => content)
 *                                          If not provided, uses fs.readFileSync
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeFrontmatterHash(workflowPath, options = {}) {
  const fileReader = options.fileReader || defaultFileReader;

  const content = await fileReader(workflowPath);

  // Extract frontmatter text and markdown body
  const { frontmatterText, markdown } = extractFrontmatterAndBody(content);

  // Get base directory for resolving imports
  const baseDir = path.dirname(workflowPath);

  // Extract template expressions with env. or vars.
  const expressions = extractRelevantTemplateExpressions(markdown);

  // Process imports using text-based parsing
  const { importedFiles, importedFrontmatterTexts } = await processImportsTextBased(frontmatterText, baseDir, undefined, fileReader);

  // Build canonical representation from text
  // The key insight is to treat frontmatter as mostly text
  // and only parse enough to extract field structure for canonical ordering
  const canonical = {};

  // Add the main frontmatter text as-is (trimmed and normalized)
  canonical["frontmatter-text"] = normalizeFrontmatterText(frontmatterText);

  // Add sorted imported files list
  if (importedFiles.length > 0) {
    canonical.imports = importedFiles.sort();
  }

  // Add sorted imported frontmatter texts (concatenated with delimiter)
  if (importedFrontmatterTexts.length > 0) {
    const sortedTexts = importedFrontmatterTexts.map(t => normalizeFrontmatterText(t)).sort();
    canonical["imported-frontmatters"] = sortedTexts.join("\n---\n");
  }

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
 * Extracts frontmatter text and markdown body from workflow content
 * Text-based extraction - no YAML parsing
 * @param {string} content - The markdown content
 * @returns {{frontmatterText: string, markdown: string}} The frontmatter text and body
 */
function extractFrontmatterAndBody(content) {
  const lines = content.split("\n");

  if (lines.length === 0 || lines[0].trim() !== "---") {
    return { frontmatterText: "", markdown: content };
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

  const frontmatterText = lines.slice(1, endIndex).join("\n");
  const markdown = lines.slice(endIndex + 1).join("\n");

  return { frontmatterText, markdown };
}

/**
 * Process imports from frontmatter using text-based parsing
 * Only parses enough to extract the imports list
 * @param {string} frontmatterText - The frontmatter text
 * @param {string} baseDir - Base directory for resolving imports
 * @param {Set<string>} visited - Set of visited files for cycle detection
 * @param {Function} fileReader - File reader function (async (filePath) => content)
 * @returns {Promise<{importedFiles: string[], importedFrontmatterTexts: string[]}>}
 */
async function processImportsTextBased(frontmatterText, baseDir, visited = new Set(), fileReader = defaultFileReader) {
  const importedFiles = [];
  const importedFrontmatterTexts = [];

  // Extract imports field using simple text parsing
  const imports = extractImportsFromText(frontmatterText);

  if (imports.length === 0) {
    return { importedFiles, importedFrontmatterTexts };
  }

  // Sort imports for deterministic processing
  const sortedImports = [...imports].sort();

  for (const importPath of sortedImports) {
    // Resolve import path relative to base directory
    const fullPath = path.resolve(baseDir, importPath);

    // Skip if already visited (cycle detection)
    if (visited.has(fullPath)) continue;
    visited.add(fullPath);

    // Read imported file
    try {
      const importContent = await fileReader(fullPath);
      const { frontmatterText: importFrontmatterText } = extractFrontmatterAndBody(importContent);

      // Add to imported files list
      importedFiles.push(importPath);
      importedFrontmatterTexts.push(importFrontmatterText);

      // Recursively process imports in the imported file
      const importBaseDir = path.dirname(fullPath);
      const nestedResult = await processImportsTextBased(importFrontmatterText, importBaseDir, visited, fileReader);

      // Add nested imports
      importedFiles.push(...nestedResult.importedFiles);
      importedFrontmatterTexts.push(...nestedResult.importedFrontmatterTexts);
    } catch (err) {
      // Skip files that can't be read
      continue;
    }
  }

  return { importedFiles, importedFrontmatterTexts };
}

/**
 * Extract imports field from frontmatter text using simple text parsing
 * Only extracts array items under "imports:" key
 * @param {string} frontmatterText - The frontmatter text
 * @returns {string[]} Array of import paths
 */
function extractImportsFromText(frontmatterText) {
  const imports = [];
  const lines = frontmatterText.split("\n");

  let inImports = false;
  let baseIndent = 0;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    // Skip empty lines and comments
    if (!trimmed || trimmed.startsWith("#")) continue;

    // Check if this is the imports: key
    if (trimmed.startsWith("imports:")) {
      inImports = true;
      baseIndent = line.search(/\S/);
      continue;
    }

    if (inImports) {
      const lineIndent = line.search(/\S/);

      // If indentation decreased or same level, we're out of the imports array
      if (lineIndent <= baseIndent && trimmed && !trimmed.startsWith("#")) {
        break;
      }

      // Extract array item
      if (trimmed.startsWith("-")) {
        let item = trimmed.substring(1).trim();
        // Remove quotes if present
        item = item.replace(/^["']|["']$/g, "");
        if (item) {
          imports.push(item);
        }
      }
    }
  }

  return imports;
}

/**
 * Normalize frontmatter text for consistent hashing
 * Removes leading/trailing whitespace and normalizes line endings
 * @param {string} text - The frontmatter text
 * @returns {string} Normalized text
 */
function normalizeFrontmatterText(text) {
  return text.trim().replace(/\r\n/g, "\n");
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
 * Marshals data to canonical JSON with sorted keys
 * @param {any} data - The data to marshal
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
 * Extract hash from lock file content
 * @param {string} lockFileContent - Content of the .lock.yml file
 * @returns {string} The extracted hash or empty string if not found
 */
function extractHashFromLockFile(lockFileContent) {
  const lines = lockFileContent.split("\n");
  for (const line of lines) {
    if (line.startsWith("# frontmatter-hash: ")) {
      return line.substring(20).trim();
    }
  }
  return "";
}

/**
 * Creates a file reader that uses GitHub's getFileContent API
 * @param {Object} github - GitHub API client (@actions/github)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} ref - Git reference (branch, tag, or commit SHA)
 * @returns {Function} File reader function compatible with computeFrontmatterHash
 */
function createGitHubFileReader(github, owner, repo, ref) {
  return async function (filePath) {
    try {
      const response = await github.rest.repos.getContent({
        owner,
        repo,
        path: filePath,
        ref,
      });

      // Decode base64 content
      if (response.data.encoding === "base64") {
        return Buffer.from(response.data.content, "base64").toString("utf8");
      }

      return response.data.content;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      throw new Error(`Failed to read file ${filePath} from GitHub: ${errorMessage}`);
    }
  };
}

module.exports = {
  computeFrontmatterHash,
  extractFrontmatterAndBody,
  extractImportsFromText,
  extractRelevantTemplateExpressions,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
  normalizeFrontmatterText,
  processImportsTextBased,
  defaultFileReader,
  createGitHubFileReader,
};
