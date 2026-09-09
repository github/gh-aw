// @ts-check

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { ERR_PARSE, ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const MAX_FRONTMATTER_HASH_INPUT_BYTES = 1 << 20; // 1 MiB
const HTTP_STATUS_UNAUTHORIZED = 401;
const HTTP_STATUS_FORBIDDEN = 403;
const HTTP_STATUS_NOT_FOUND = 404;

/**
 * Maximum depth for recursive symlink resolution when fetching remote files via the GitHub API.
 * Mirrors the Go constant pkg/constants/constants.go MaxSymlinkDepth.
 */
const MAX_SYMLINK_DEPTH = 5;

/**
 * Default file reader using Node.js fs module
 * @param {string} filePath - Path to the file
 * @returns {Promise<string>} File content
 */
async function defaultFileReader(filePath) {
  try {
    return fs.readFileSync(filePath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${filePath}: ${getErrorMessage(err)}`, { cause: err });
  }
}

/**
 * Parses a boolean flag from frontmatter text using simple text scanning.
 * Matches lines of the form "key: true" or "key: false" at the top level.
 * Returns false when the key is absent or the value is not "true".
 * @param {string} frontmatterText - Raw frontmatter text (between --- delimiters)
 * @param {string} key - The frontmatter key to look up
 * @returns {boolean}
 */
function parseBoolFromFrontmatter(frontmatterText, key) {
  const escapedKey = key.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(`^${escapedKey}:\\s*(true|false)\\s*$`, "m");
  const match = frontmatterText.match(pattern);
  return match !== null && match[1] === "true";
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
 * @param {boolean} [options.verbose] - When true, emits detailed debug logging via core.info()
 *                                      for every step of the computation. Useful when diagnosing
 *                                      unexpected hash mismatches.
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeFrontmatterHash(workflowPath, options = {}) {
  const fileReader = options.fileReader || defaultFileReader;
  const verbose = options.verbose === true;
  // log is a thin helper that only emits when verbose mode is active.
  // It uses core.info when available (GitHub Actions environment) and falls back to
  // console.log so the function remains testable outside Actions.
  const log = msg => {
    if (!verbose) return;
    const logFn = typeof core !== "undefined" ? core.info : console.log;
    logFn(`[hash-debug] ${msg}`);
  };

  log(`Starting hash computation for: ${workflowPath}`);

  const content = await fileReader(workflowPath);

  log(`File content length: ${content.length} bytes`);

  // Extract frontmatter text and markdown body
  const { frontmatterText, markdown } = extractFrontmatterAndBody(content);

  log(`Frontmatter text (${frontmatterText.length} chars):\n---\n${frontmatterText}\n---`);
  log(`Markdown body length: ${markdown.length} chars`);

  // Get base directory for resolving imports
  const baseDir = path.dirname(workflowPath);

  // Check for inlined-imports flag in frontmatter (text-based, no YAML parsing)
  const inlinedImports = parseBoolFromFrontmatter(frontmatterText, "inlined-imports");

  log(`inlined-imports: ${inlinedImports}`);

  // Process imports using text-based parsing
  const { importedFiles, importedFrontmatterTexts } = await processImportsTextBased(frontmatterText, baseDir, undefined, fileReader);

  log(`Imported files (${importedFiles.length}): ${JSON.stringify(importedFiles)}`);

  // Build canonical representation from text
  // The key insight is to treat frontmatter as mostly text
  // and only parse enough to extract field structure for canonical ordering
  const canonical = {};

  // Add the main frontmatter text as-is (trimmed and normalized)
  const normalizedFrontmatter = normalizeFrontmatterText(frontmatterText);
  const normalizedImportedFrontmatterTexts = importedFrontmatterTexts.map(t => normalizeFrontmatterText(t));
  validateNormalizedFrontmatterHashInputSize(normalizedFrontmatter, normalizedImportedFrontmatterTexts);
  canonical["frontmatter-text"] = normalizedFrontmatter;

  log(`Normalized frontmatter-text:\n${normalizedFrontmatter}`);

  // Add sorted imported files list
  if (importedFiles.length > 0) {
    canonical.imports = importedFiles.sort();
    log(`canonical.imports: ${JSON.stringify(canonical.imports)}`);
  }

  // Add sorted imported frontmatter texts (concatenated with delimiter)
  if (importedFrontmatterTexts.length > 0) {
    const sortedTexts = normalizedImportedFrontmatterTexts.sort();
    canonical["imported-frontmatters"] = sortedTexts.join("\n---\n");
    log(`canonical.imported-frontmatters:\n${canonical["imported-frontmatters"]}`);
  }

  // When inlined-imports is enabled, the entire markdown body is compiled into the lock
  // file, so any change to the body must invalidate the hash. Include the full body text.
  // Otherwise, only extract the relevant template expressions (env./vars. references).
  if (inlinedImports) {
    canonical["body-text"] = normalizeFrontmatterText(markdown);
    log(`canonical.body-text (inlined-imports): ${canonical["body-text"].substring(0, 200)}...`);
  } else {
    // Extract template expressions with env. or vars.
    const expressions = mergeSortedUniqueStrings(extractRelevantTemplateExpressions(markdown), await collectRuntimeImportTemplateExpressions(frontmatterText, markdown, baseDir, fileReader));
    if (expressions.length > 0) {
      canonical["template-expressions"] = expressions;
      log(`canonical.template-expressions: ${JSON.stringify(expressions)}`);
    } else {
      log("No template expressions (env./vars.) found in markdown body");
    }
  }

  // Serialize to canonical JSON
  const canonicalJSON = marshalCanonicalJSON(canonical);

  log(`Canonical JSON (${canonicalJSON.length} chars):\n${canonicalJSON}`);

  // Compute SHA-256 hash
  const hash = crypto.createHash("sha256").update(canonicalJSON, "utf8").digest("hex");

  log(`Computed SHA-256 hash: ${hash}`);

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
    throw new Error(`${ERR_PARSE}: Frontmatter not properly closed`);
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
    // Join import path with base directory (preserves relative paths for GitHub API compatibility)
    const fullPath = path.join(baseDir, importPath);

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
        // Extract path from object-form imports (uses: <path> / path: <path>).
        // These are valid import forms that the compiler accepts; extract the path
        // value so that changes to imported files are reflected in the hash.
        if (item.startsWith("uses:")) {
          item = item.substring("uses:".length).trim();
        } else if (item.startsWith("path:")) {
          item = item.substring("path:".length).trim();
        }
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
 * Validates combined normalized frontmatter input size for hash computation.
 * Enforces the 1 MiB ceiling used by FH-TV-NEG-001 across main and imported frontmatter text.
 * @param {string} normalizedFrontmatterText
 * @param {string[]} normalizedImportedFrontmatterTexts
 * @throws {Error} When total normalized input exceeds MAX_FRONTMATTER_HASH_INPUT_BYTES.
 */
function validateNormalizedFrontmatterHashInputSize(normalizedFrontmatterText, normalizedImportedFrontmatterTexts) {
  let totalBytes = Buffer.byteLength(normalizedFrontmatterText, "utf8");
  for (const text of normalizedImportedFrontmatterTexts) {
    totalBytes += Buffer.byteLength(text, "utf8");
  }

  if (totalBytes > MAX_FRONTMATTER_HASH_INPUT_BYTES) {
    throw new Error(`${ERR_VALIDATION}: frontmatter hash input exceeds ${MAX_FRONTMATTER_HASH_INPUT_BYTES} bytes after normalization`);
  }
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
 * Extract all GitHub Actions template expressions.
 * @param {string} markdown
 * @returns {string[]}
 */
function extractAllTemplateExpressions(markdown) {
  const expressions = [];
  const regex = /\$\{\{([^}]+)\}\}/g;
  let match;

  while ((match = regex.exec(markdown)) !== null) {
    if (!match[1].trim()) continue;
    expressions.push(match[0]);
  }

  return [...new Set(expressions)].sort();
}

/**
 * @typedef {Object} RuntimeImportReference
 * @property {string} path
 * @property {number|null} startLine
 * @property {number|null} endLine
 */

/**
 * Collect template expressions from content that may be added to the prompt by
 * runtime imports.
 * @param {string} frontmatterText
 * @param {string} markdown
 * @param {string} baseDir
 * @param {Function} fileReader
 * @returns {Promise<string[]>}
 */
async function collectRuntimeImportTemplateExpressions(frontmatterText, markdown, baseDir, fileReader) {
  const seen = new Set();
  let expressions = await extractRuntimeImportTemplateExpressionsFromMarkdown(markdown, baseDir, seen, fileReader);

  const importedBodies = await collectImportedBodies(frontmatterText, baseDir, new Set(), fileReader);
  for (const body of importedBodies) {
    expressions = mergeSortedUniqueStrings(expressions, extractAllTemplateExpressions(body));
    expressions = mergeSortedUniqueStrings(expressions, await extractRuntimeImportTemplateExpressionsFromMarkdown(body, baseDir, seen, fileReader));
  }

  return expressions;
}

/**
 * @param {string} markdown
 * @param {string} baseDir
 * @param {Set<string>} seen
 * @param {Function} fileReader
 * @returns {Promise<string[]>}
 */
async function extractRuntimeImportTemplateExpressionsFromMarkdown(markdown, baseDir, seen, fileReader) {
  const refs = extractRuntimeImportReferences(markdown);
  let expressions = [];

  for (const ref of refs) {
    const body = await readRuntimeImportBodyForHash(ref, baseDir, seen, fileReader);
    if (body === null) continue;
    expressions = mergeSortedUniqueStrings(expressions, extractAllTemplateExpressions(body));
    expressions = mergeSortedUniqueStrings(expressions, await extractRuntimeImportTemplateExpressionsFromMarkdown(body, baseDir, seen, fileReader));
  }

  return expressions;
}

/**
 * @param {string} markdown
 * @returns {RuntimeImportReference[]}
 */
function extractRuntimeImportReferences(markdown) {
  if (!markdown) return [];

  const refs = [];
  const seen = new Set();
  const regex = /\{\{#(?:runtime-import|import)\??(?:[ \t]+|[ \t]*:[ \t]*)([^{}]+?)\}\}/g;
  let match;
  while ((match = regex.exec(markdown)) !== null) {
    const target = match[1].trim();
    if (!target) continue;
    const rangeMatch = target.match(/^(.+?):(\d+)-(\d+)$/);
    const ref = rangeMatch ? { path: rangeMatch[1].trim(), startLine: Number.parseInt(rangeMatch[2], 10), endLine: Number.parseInt(rangeMatch[3], 10) } : { path: target, startLine: null, endLine: null };
    if (/^https?:\/\//i.test(ref.path)) continue;
    const key = `${ref.path.replace(/\\/g, "/")}:${ref.startLine ?? 0}-${ref.endLine ?? 0}`;
    if (seen.has(key)) continue;
    refs.push(ref);
    seen.add(key);
  }
  return refs;
}

/**
 * @param {RuntimeImportReference} ref
 * @param {string} baseDir
 * @param {Set<string>} seen
 * @param {Function} fileReader
 * @returns {Promise<string|null>}
 */
async function readRuntimeImportBodyForHash(ref, baseDir, seen, fileReader) {
  for (const candidate of runtimeImportHashCandidatePaths(ref.path, baseDir)) {
    const key = `${candidate}:${ref.startLine ?? 0}-${ref.endLine ?? 0}`;
    if (seen.has(key)) return null;
    if (fs.existsSync(candidate) && !runtimeImportHashRealPathAllowed(candidate, baseDir)) continue;
    try {
      let content = await fileReader(candidate);
      seen.add(key);
      if (ref.startLine !== null || ref.endLine !== null) {
        content = applyRuntimeImportLineRangeForHash(content, ref.startLine, ref.endLine);
      }
      const { markdown } = extractFrontmatterAndBody(content);
      return markdown;
    } catch (err) {
      continue;
    }
  }
  return null;
}

function runtimeImportHashRealPathAllowed(candidate, baseDir) {
  const workspaceRoot = runtimeImportHashWorkspaceRoot(baseDir);
  for (const base of [path.join(workspaceRoot, ".github"), path.join(workspaceRoot, ".agents")]) {
    if (realPathWithinBaseForHash(candidate, base)) return true;
  }
  return false;
}

function realPathWithinBaseForHash(pathToCheck, baseDir) {
  try {
    const realBase = fs.realpathSync(baseDir);
    const realPath = fs.realpathSync(pathToCheck);
    const relativePath = path.relative(realBase, realPath);
    return relativePath !== ".." && !relativePath.startsWith(`..${path.sep}`) && !path.isAbsolute(relativePath);
  } catch {
    return false;
  }
}

/**
 * @param {string} content
 * @param {number|null} startLine
 * @param {number|null} endLine
 * @returns {string}
 */
function applyRuntimeImportLineRangeForHash(content, startLine, endLine) {
  const lines = content.split("\n");
  const start = startLine && startLine > 0 ? startLine : 1;
  const end = endLine && endLine > 0 && endLine <= lines.length ? endLine : lines.length;
  if (start > end || start > lines.length) return "";
  return lines.slice(start - 1, end).join("\n");
}

/**
 * @param {string} importPath
 * @param {string} baseDir
 * @returns {string[]}
 */
function runtimeImportHashCandidatePaths(importPath, baseDir) {
  let normalized = String(importPath || "")
    .trim()
    .replace(/\\/g, "/");
  if (normalized.startsWith("/")) {
    normalized = normalized.replace(/^\/+/, "");
    if (!normalized.startsWith(".github/") && !normalized.startsWith(".agents/")) {
      return [];
    }
  }
  if (normalized.startsWith("./")) {
    normalized = normalized.substring(2);
  }
  if (!normalized || normalized.startsWith("../") || normalized.includes("/../")) {
    return [];
  }

  const workspaceRoot = runtimeImportHashWorkspaceRoot(baseDir);
  if (normalized.startsWith(".agents/") || normalized.startsWith(".github/")) {
    return [path.join(workspaceRoot, normalized)];
  }

  return [path.join(workspaceRoot, ".github", normalized), path.join(workspaceRoot, ".github", "workflows", normalized), path.join(baseDir, normalized)].map(p => path.normalize(p)).filter((p, index, all) => all.indexOf(p) === index);
}

/**
 * @param {string} baseDir
 * @returns {string}
 */
function runtimeImportHashWorkspaceRoot(baseDir) {
  const normalized = baseDir.replace(/\\/g, "/");
  const markerIndex = normalized.indexOf("/.github/");
  if (markerIndex >= 0) {
    return normalized.substring(0, markerIndex);
  }
  if (normalized.endsWith("/.github")) {
    return path.dirname(baseDir);
  }
  return baseDir;
}

/**
 * @param  {...string[]} groups
 * @returns {string[]}
 */
function mergeSortedUniqueStrings(...groups) {
  return [...new Set(groups.flat().filter(Boolean))].sort();
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
 * Process imports from frontmatter to collect body texts of all transitively imported files.
 * Used to include imported file bodies in the body hash.
 * @param {string} frontmatterText - The frontmatter text (used to find imports)
 * @param {string} baseDir - Base directory for resolving imports
 * @param {Set<string>} visited - Set of visited files for cycle detection
 * @param {Function} fileReader - File reader function (async (filePath) => content)
 * @returns {Promise<string[]>} Array of imported body texts
 */
async function collectImportedBodies(frontmatterText, baseDir, visited = new Set(), fileReader = defaultFileReader) {
  const importedBodyTexts = [];

  const imports = extractImportsFromText(frontmatterText);
  if (imports.length === 0) {
    return importedBodyTexts;
  }

  const sortedImports = [...imports].sort();

  for (const importPath of sortedImports) {
    const fullPath = path.join(baseDir, importPath);

    if (visited.has(fullPath)) continue;
    visited.add(fullPath);

    try {
      const importContent = await fileReader(fullPath);
      const { frontmatterText: importFrontmatterText, markdown: importBody } = extractFrontmatterAndBody(importContent);

      importedBodyTexts.push(importBody);

      const importBaseDir = path.dirname(fullPath);
      const nestedBodies = await collectImportedBodies(importFrontmatterText, importBaseDir, visited, fileReader);
      importedBodyTexts.push(...nestedBodies);
    } catch (err) {
      continue;
    }
  }

  return importedBodyTexts;
}

/**
 * Computes a SHA-256 hash of the markdown body (after frontmatter) including the bodies
 * of all transitively imported files. This hash covers changes to the prompt body that are
 * not captured by the frontmatter hash.
 *
 * @param {string} workflowPath - Path to the workflow file
 * @param {Object} [options] - Optional configuration
 * @param {Function} [options.fileReader] - Custom file reader function (async (filePath) => content)
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string (64 characters)
 */
async function computeBodyHash(workflowPath, options = {}) {
  const fileReader = options.fileReader || defaultFileReader;

  const content = await fileReader(workflowPath);
  const { frontmatterText, markdown } = extractFrontmatterAndBody(content);

  const baseDir = path.dirname(workflowPath);

  const importedBodies = await collectImportedBodies(frontmatterText, baseDir, undefined, fileReader);

  const allParts = [normalizeFrontmatterText(markdown)];

  if (importedBodies.length > 0) {
    const sortedBodies = importedBodies.map(b => normalizeFrontmatterText(b)).sort();
    allParts.push(...sortedBodies);
  }

  const combined = allParts.join("\n---\n");
  const hash = crypto.createHash("sha256").update(combined, "utf8").digest("hex");
  return hash;
}

/**
 * Extract body hash from lock file content.
 * Only the new JSON metadata format (# gh-aw-metadata: {...}) contains the body hash.
 * @param {string} lockFileContent - Content of the .lock.yml file
 * @returns {string} The extracted body hash or empty string if not found
 */
function extractBodyHashFromLockFile(lockFileContent) {
  const lines = lockFileContent.split("\n");
  for (const line of lines) {
    const metadataMatch = line.match(/^#\s*gh-aw-metadata:\s*(\{.+\})/);
    if (metadataMatch) {
      try {
        const metadata = JSON.parse(metadataMatch[1]);
        if (metadata.body_hash) {
          return metadata.body_hash;
        }
      } catch (err) {
        // Invalid JSON metadata — ignored.
      }
    }
  }
  return "";
}

/**
 * Extract hash from lock file content
 * Supports both formats:
 * - New JSON format: # gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"..."}
 * - Old format: # frontmatter-hash: ...
 * @param {string} lockFileContent - Content of the .lock.yml file
 * @returns {string} The extracted hash or empty string if not found
 */
function extractHashFromLockFile(lockFileContent) {
  const lines = lockFileContent.split("\n");
  for (const line of lines) {
    // Try new JSON metadata format first
    const metadataMatch = line.match(/^#\s*gh-aw-metadata:\s*(\{.+\})/);
    if (metadataMatch) {
      try {
        const metadata = JSON.parse(metadataMatch[1]);
        if (metadata.frontmatter_hash) {
          return metadata.frontmatter_hash;
        }
      } catch (err) {
        // Invalid JSON — ignored, continue to check old format.
      }
    }

    // Fall back to old format for backward compatibility
    if (line.startsWith("# frontmatter-hash: ")) {
      return line.substring(20).trim();
    }
  }
  return "";
}

/**
 * Checks whether a single path component in a remote GitHub repository is a symlink.
 * Returns the symlink target string if it is one, or null for regular files/directories.
 * Errors (including 404) are treated as "not a symlink" to allow the caller to continue
 * walking the remaining path components.
 * Mirrors the Go logic in pkg/parser/remote_download_file.go checkRemoteSymlink.
 *
 * @param {Object} github - GitHub API client (@actions/github)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} dirPath - Repository-relative path of the component to inspect
 * @param {string} ref - Git reference (branch, tag, or commit SHA)
 * @param {Map<string, Promise<string|null>>} [symlinkLookupCache] - Optional cache for symlink lookups
 * @returns {Promise<string|null>} Symlink target, or null if not a symlink
 */
async function checkRemoteSymlink(github, owner, repo, dirPath, ref, symlinkLookupCache) {
  const cachedLookup = symlinkLookupCache && symlinkLookupCache.get(dirPath);
  if (cachedLookup) {
    return await cachedLookup;
  }

  const lookupPromise = (async () => {
    try {
      const response = await github.rest.repos.getContent({
        owner,
        repo,
        path: dirPath,
        ref,
      });
      // Array response → directory listing, not a symlink
      if (Array.isArray(response.data)) {
        return null;
      }
      // An empty string target is treated as absent (falsy); only non-empty targets are valid.
      if (response.data.type === "symlink" && response.data.target) {
        return response.data.target;
      }
      return null;
    } catch (err) {
      const errAny = /** @type {any} */ err;
      const status = errAny?.status || (errAny?.response && errAny.response.status);
      if (status === HTTP_STATUS_NOT_FOUND || status === HTTP_STATUS_FORBIDDEN || status === HTTP_STATUS_UNAUTHORIZED) {
        return null;
      }
      throw err;
    }
  })();

  if (symlinkLookupCache) {
    symlinkLookupCache.set(dirPath, lookupPromise);
  }

  try {
    return await lookupPromise;
  } catch (err) {
    if (symlinkLookupCache && symlinkLookupCache.get(dirPath) === lookupPromise) {
      symlinkLookupCache.delete(dirPath);
    }
    throw err;
  }
}

/**
 * Resolves symlinks in a remote GitHub repository path by walking each directory component.
 * The GitHub Contents API does not follow symlinks in path components. For example, if
 * .github/agents is a symlink to ../.ai/agents, requesting .github/agents/e2etest.md
 * returns a 404. This function detects the symlink and returns the equivalent resolved path.
 * Mirrors the Go logic in pkg/parser/remote_download_file.go resolveRemoteSymlinks.
 *
 * @param {Object} github - GitHub API client (@actions/github)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} filePath - Repository-relative path to resolve (e.g. ".github/agents/e2etest.md")
 * @param {string} ref - Git reference (branch, tag, or commit SHA)
 * @param {Map<string, Promise<string|null>>} [symlinkLookupCache] - Optional cache for path-component symlink lookups
 * @returns {Promise<string|null>} Resolved repository-relative path, or null if no symlinks found
 */
async function resolveRemoteSymlinks(github, owner, repo, filePath, ref, symlinkLookupCache) {
  const parts = filePath.split("/");
  if (parts.length <= 1) {
    return null; // No directory components to resolve
  }

  // Resolve at most one symlink per call so each retry validates the next
  // filesystem view independently. If the resolved path also contains a
  // symlink, fetchFile will re-invoke resolveRemoteSymlinks via another 404
  // retry (bounded by MAX_SYMLINK_DEPTH).
  //
  // Skip well-known non-symlink prefix components to avoid spurious API calls:
  //   - ".github" alone is never a symlink
  //   - ".github/workflows" is a reserved GitHub Actions directory and never a symlink
  // Starting at a higher index reduces unnecessary probes when a path 404s for unrelated
  // reasons (e.g. malformed nested import resolution that produces double directory names).
  let startIndex = 1;
  if (filePath.startsWith(".github/workflows/")) {
    startIndex = 3; // Skip ".github" and ".github/workflows" components
  } else if (filePath.startsWith(".github/")) {
    startIndex = 2; // Skip ".github" component; still check e.g. ".github/agents"
  }
  for (let i = startIndex; i < parts.length; i++) {
    const dirPath = parts.slice(0, i).join("/");
    const target = await checkRemoteSymlink(github, owner, repo, dirPath, ref, symlinkLookupCache);
    if (target === null) {
      continue;
    }

    // Resolve the symlink: join the parent directory with the symlink target, then normalize.
    // Uses path.posix to ensure forward-slash handling for repository-relative paths.
    const parentDir = i > 1 ? parts.slice(0, i - 1).join("/") : "";
    let resolvedBase;
    if (parentDir) {
      resolvedBase = path.posix.normalize(path.posix.join(parentDir, target));
    } else {
      resolvedBase = path.posix.normalize(target);
    }

    // Safety: resolved base must not escape repository root.
    // path.posix.normalize has already been applied, so a path starting with ".." genuinely
    // escapes the root — there are no remaining "../" segments that normalize would have collapsed.
    if (!resolvedBase || resolvedBase === "." || path.posix.isAbsolute(resolvedBase) || resolvedBase.startsWith("..")) {
      return null;
    }

    const remaining = parts.slice(i).join("/");
    const resolvedPath = path.posix.normalize(path.posix.join(resolvedBase, remaining));

    // Same safety check on the final resolved path after appending the remaining components.
    if (!resolvedPath || resolvedPath === "." || path.posix.isAbsolute(resolvedPath) || resolvedPath.startsWith("..")) {
      return null;
    }

    return resolvedPath;
  }

  return null; // No symlinks found
}

/**
 * Creates a file reader that uses GitHub's getFileContent API.
 * When a file is not found (HTTP 404), the reader automatically attempts to resolve symlinks
 * in the path components and retries with the resolved path. This handles the case where an
 * import path traverses a symlinked directory (e.g. .github/agents → ../.ai/agents) because
 * the GitHub Contents API does not follow symlinks in path components.
 * @param {Object} github - GitHub API client (@actions/github)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} ref - Git reference (branch, tag, or commit SHA)
 * @returns {Function} File reader function compatible with computeFrontmatterHash
 */
function createGitHubFileReader(github, owner, repo, ref) {
  /** @type {Map<string, Promise<string|null>>} */
  const symlinkLookupCache = new Map();
  /** @type {Map<string, Promise<string|null>>} */
  const resolvedPathCache = new Map();

  async function resolveRemoteSymlinksCached(filePath) {
    const cachedResolution = resolvedPathCache.get(filePath);
    if (cachedResolution) {
      return await cachedResolution;
    }

    const resolutionPromise = resolveRemoteSymlinks(github, owner, repo, filePath, ref, symlinkLookupCache);
    resolvedPathCache.set(filePath, resolutionPromise);
    try {
      return await resolutionPromise;
    } catch (err) {
      if (resolvedPathCache.get(filePath) === resolutionPromise) {
        resolvedPathCache.delete(filePath);
      }
      throw err;
    }
  }

  async function fetchFile(filePath, symlinkDepth) {
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
      // When the file is not found, attempt symlink resolution on path components.
      // This handles the case where an import path traverses a symlinked directory
      // (e.g. .github/agents → ../.ai/agents), which the GitHub Contents API cannot
      // follow automatically. Mirrors the Go logic in remote_download_file.go.
      const errorObject = typeof error === "object" && error !== null ? error : null;
      const status = errorObject ? errorObject.status || (errorObject.response && errorObject.response.status) : undefined;
      if (status === HTTP_STATUS_NOT_FOUND) {
        if (symlinkDepth >= MAX_SYMLINK_DEPTH) {
          throw new Error(`${ERR_SYSTEM}: Failed to read file ${filePath} from GitHub: symlink chain exceeded maximum depth of ${MAX_SYMLINK_DEPTH}`);
        }
        const resolvedPath = await resolveRemoteSymlinksCached(filePath);
        if (resolvedPath !== null && resolvedPath !== filePath) {
          return fetchFile(resolvedPath, symlinkDepth + 1);
        }
      }

      const errorMessage = getErrorMessage(error);
      throw new Error(`${ERR_SYSTEM}: Failed to read file ${filePath} from GitHub: ${errorMessage}`);
    }
  }

  return filePath => fetchFile(filePath, 0);
}

module.exports = {
  computeFrontmatterHash,
  computeBodyHash,
  extractFrontmatterAndBody,
  extractImportsFromText,
  extractRelevantTemplateExpressions,
  extractAllTemplateExpressions,
  collectRuntimeImportTemplateExpressions,
  extractRuntimeImportReferences,
  marshalCanonicalJSON,
  marshalSorted,
  extractHashFromLockFile,
  extractBodyHashFromLockFile,
  normalizeFrontmatterText,
  parseBoolFromFrontmatter,
  processImportsTextBased,
  collectImportedBodies,
  defaultFileReader,
  createGitHubFileReader,
  checkRemoteSymlink,
  resolveRemoteSymlinks,
};
