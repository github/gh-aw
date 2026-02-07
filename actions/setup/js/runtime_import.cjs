// @ts-check
/// <reference types="@actions/github-script" />

// runtime_import.cjs
// Processes {{#runtime-import filepath}} and {{#runtime-import? filepath}} macros
// at runtime to import markdown file contents dynamically.
// Also processes inline @path and @url references.

const { getErrorMessage } = require("./error_helpers.cjs");

const fs = require("fs");
const path = require("path");
const https = require("https");
const http = require("http");

/**
 * Checks if a file starts with front matter (---\n)
 * @param {string} content - The file content to check
 * @returns {boolean} - True if content starts with front matter
 */
function hasFrontMatter(content) {
  return content.trimStart().startsWith("---\n") || content.trimStart().startsWith("---\r\n");
}

/**
 * Removes XML comments from content
 * @param {string} content - The content to process
 * @returns {string} - Content with XML comments removed
 */
function removeXMLComments(content) {
  // Remove XML/HTML comments: <!-- ... -->
  // Apply repeatedly to handle nested/overlapping patterns that could reintroduce comment markers
  let previous;
  do {
    previous = content;
    content = content.replace(/<!--[\s\S]*?-->/g, "");
  } while (content !== previous);
  return content;
}

/**
 * Safe list of allowed GitHub Actions expressions
 * These are expressions that cannot be tampered with by users
 * and are safe to evaluate at runtime.
 *
 * This list matches pkg/constants/constants.go:AllowedExpressions
 */
const ALLOWED_EXPRESSIONS = [
  "github.event.after",
  "github.event.before",
  "github.event.check_run.id",
  "github.event.check_suite.id",
  "github.event.comment.id",
  "github.event.deployment.id",
  "github.event.deployment_status.id",
  "github.event.head_commit.id",
  "github.event.installation.id",
  "github.event.issue.number",
  "github.event.discussion.number",
  "github.event.pull_request.number",
  "github.event.milestone.number",
  "github.event.check_run.number",
  "github.event.check_suite.number",
  "github.event.workflow_job.run_id",
  "github.event.workflow_run.number",
  "github.event.label.id",
  "github.event.milestone.id",
  "github.event.organization.id",
  "github.event.page.id",
  "github.event.project.id",
  "github.event.project_card.id",
  "github.event.project_column.id",
  "github.event.release.assets[0].id",
  "github.event.release.id",
  "github.event.release.tag_name",
  "github.event.repository.id",
  "github.event.repository.default_branch",
  "github.event.review.id",
  "github.event.review_comment.id",
  "github.event.sender.id",
  "github.event.workflow_run.id",
  "github.event.workflow_run.conclusion",
  "github.event.workflow_run.html_url",
  "github.event.workflow_run.head_sha",
  "github.event.workflow_run.run_number",
  "github.event.workflow_run.event",
  "github.event.workflow_run.status",
  "github.event.issue.state",
  "github.event.issue.title",
  "github.event.pull_request.state",
  "github.event.pull_request.title",
  "github.event.discussion.title",
  "github.event.discussion.category.name",
  "github.event.release.name",
  "github.event.workflow_job.id",
  "github.event.deployment.environment",
  "github.event.pull_request.head.sha",
  "github.event.pull_request.base.sha",
  "github.actor",
  "github.job",
  "github.owner",
  "github.repository",
  "github.repository_owner",
  "github.run_id",
  "github.run_number",
  "github.server_url",
  "github.workflow",
  "github.workspace",
];

/**
 * Checks if an expression is in the safe list
 * @param {string} expr - The expression to check (without ${{ }})
 * @returns {boolean} - True if expression is safe
 */
function isSafeExpression(expr) {
  const trimmed = expr.trim();

  // Check exact match in allowed list
  if (ALLOWED_EXPRESSIONS.includes(trimmed)) {
    return true;
  }

  // Check if it matches dynamic patterns:
  // - needs.* and steps.* (job dependencies and step outputs)
  // - github.event.inputs.* (workflow_dispatch inputs)
  // - github.aw.inputs.* (shared workflow inputs)
  // - inputs.* (workflow_call inputs)
  // - env.* (environment variables)
  const dynamicPatterns = [/^(needs|steps)\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$/, /^github\.event\.inputs\.[a-zA-Z0-9_-]+$/, /^github\.aw\.inputs\.[a-zA-Z0-9_-]+$/, /^inputs\.[a-zA-Z0-9_-]+$/, /^env\.[a-zA-Z0-9_-]+$/];

  for (const pattern of dynamicPatterns) {
    if (pattern.test(trimmed)) {
      return true;
    }
  }

  // Check for OR expressions with literals (e.g., "inputs.repository || 'default'")
  // Pattern: safe_expression || 'literal' or safe_expression || "literal" or safe_expression || `literal`
  // Also supports numbers and booleans as literals
  const orMatch = trimmed.match(/^(.+?)\s*\|\|\s*(.+)$/);
  if (orMatch) {
    const leftExpr = orMatch[1].trim();
    const rightExpr = orMatch[2].trim();

    // Check if left side is safe
    const leftIsSafe = isSafeExpression(leftExpr);
    if (!leftIsSafe) {
      return false;
    }

    // Check if right side is a literal string (single, double, or backtick quotes)
    const isStringLiteral = /^(['"`]).*\1$/.test(rightExpr);
    // Check if right side is a number literal
    const isNumberLiteral = /^-?\d+(\.\d+)?$/.test(rightExpr);
    // Check if right side is a boolean literal
    const isBooleanLiteral = rightExpr === "true" || rightExpr === "false";

    if (isStringLiteral || isNumberLiteral || isBooleanLiteral) {
      return true;
    }

    // If right side is also a safe expression (e.g., secrets.FOO || secrets.BAR)
    if (isSafeExpression(rightExpr)) {
      return true;
    }
  }

  return false;
}

/**
 * Evaluates a safe GitHub Actions expression at runtime
 * @param {string} expr - The expression to evaluate (without ${{ }})
 * @returns {string} - The evaluated value or original expression if cannot evaluate
 */
function evaluateExpression(expr) {
  const trimmed = expr.trim();

  // Check for OR expressions with literals (e.g., "inputs.repository || 'default'")
  const orMatch = trimmed.match(/^(.+?)\s*\|\|\s*(.+)$/);
  if (orMatch) {
    const leftExpr = orMatch[1].trim();
    const rightExpr = orMatch[2].trim();

    // Try to evaluate the left expression
    const leftValue = evaluateExpression(leftExpr);

    // Check if left value is truthy (not empty, not undefined, not null)
    // If it's wrapped in ${{ }}, it means it couldn't be evaluated
    if (!leftValue.startsWith("${{")) {
      return leftValue;
    }

    // Left value is falsy or couldn't be evaluated, use the right side
    // If right side is a literal, extract and return it
    const stringLiteralMatch = rightExpr.match(/^(['"`])(.+)\1$/);
    if (stringLiteralMatch) {
      return stringLiteralMatch[2]; // Return the literal value without quotes
    }

    // If right side is a number or boolean literal, return it
    if (/^-?\d+(\.\d+)?$/.test(rightExpr) || rightExpr === "true" || rightExpr === "false") {
      return rightExpr;
    }

    // Otherwise try to evaluate the right expression
    return evaluateExpression(rightExpr);
  }

  // Check if this is a needs.* or steps.* expression that should be looked up from environment variables
  // The compiler extracts these expressions and makes them available as GH_AW_* environment variables
  // For example: needs.search_issues.outputs.issue_list → GH_AW_NEEDS_SEARCH_ISSUES_OUTPUTS_ISSUE_LIST
  if (trimmed.startsWith("needs.") || trimmed.startsWith("steps.")) {
    // Convert expression to environment variable name
    // e.g., "needs.search_issues.outputs.issue_list" → "GH_AW_NEEDS_SEARCH_ISSUES_OUTPUTS_ISSUE_LIST"
    const envVarName = "GH_AW_" + trimmed.toUpperCase().replace(/\./g, "_");
    const envValue = process.env[envVarName];
    if (envValue !== undefined && envValue !== null && envValue !== "") {
      return envValue;
    }
    // If not found in environment, continue to try other evaluation methods below
  }

  // Access GitHub context through environment variables
  // The context object is available globally when running in github-script
  if (typeof context !== "undefined") {
    try {
      // Build the evaluation context with safe properties
      const evalContext = {
        github: {
          actor: context.actor,
          job: context.job,
          owner: context.repo.owner,
          repository: `${context.repo.owner}/${context.repo.repo}`,
          repository_owner: context.repo.owner,
          run_id: context.runId,
          run_number: context.runNumber,
          server_url: process.env.GITHUB_SERVER_URL || "https://github.com",
          workflow: context.workflow,
          workspace: process.env.GITHUB_WORKSPACE || "",
          event: context.payload || {},
        },
        env: process.env,
        inputs: context.payload?.inputs || {},
      };

      // Parse property access (e.g., "github.actor" -> ["github", "actor"])
      const parts = trimmed.split(".");
      let value = evalContext;

      for (const part of parts) {
        // Handle array access like release.assets[0].id
        const arrayMatch = part.match(/^([a-zA-Z0-9_-]+)\[(\d+)\]$/);
        if (arrayMatch) {
          const key = arrayMatch[1];
          const index = parseInt(arrayMatch[2], 10);
          value = value?.[key]?.[index];
        } else {
          value = value?.[part];
        }

        if (value === undefined || value === null) {
          break;
        }
      }

      // If we successfully resolved the value, return it as a string
      if (value !== undefined && value !== null) {
        return String(value);
      }
    } catch (error) {
      // If evaluation fails, log but don't throw
      const errorMessage = error instanceof Error ? error.message : String(error);
      core.warning(`Failed to evaluate expression "${trimmed}": ${errorMessage}`);
    }
  }

  // If we can't evaluate, return the original expression wrapped in ${{ }}
  // This allows GitHub Actions to evaluate it later
  return `\${{ ${trimmed} }}`;
}

/**
 * Validates and renders GitHub Actions expressions in content
 * @param {string} content - The content with potential expressions
 * @param {string} source - The source identifier (file path or URL) for error messages
 * @returns {string} - Content with safe expressions rendered
 * @throws {Error} - If unsafe expressions are found
 */
function processExpressions(content, source) {
  // Pattern to match GitHub Actions expressions: ${{ ... }}
  const expressionRegex = /\$\{\{([\s\S]*?)\}\}/g;

  const matches = [...content.matchAll(expressionRegex)];
  if (matches.length === 0) {
    return content;
  }

  core.info(`Found ${matches.length} expression(s) in ${source}`);

  const unsafeExpressions = [];
  const replacements = new Map();

  // First pass: validate all expressions
  for (const match of matches) {
    const fullMatch = match[0];
    const expr = match[1];

    // Skip multiline expressions (security: prevent injection)
    if (expr.includes("\n")) {
      unsafeExpressions.push(expr.trim());
      continue;
    }

    const trimmed = expr.trim();

    // Check if expression is safe
    if (!isSafeExpression(trimmed)) {
      unsafeExpressions.push(trimmed);
      continue;
    }

    // Expression is safe - evaluate it
    const evaluated = evaluateExpression(trimmed);
    replacements.set(fullMatch, evaluated);
  }

  // If any unsafe expressions found, throw error
  if (unsafeExpressions.length > 0) {
    const errorMsg =
      `${source} contains unauthorized GitHub Actions expressions:\n` +
      unsafeExpressions.map(e => `  - ${e}`).join("\n") +
      "\n\n" +
      "Only expressions from the safe list can be used in runtime imports.\n" +
      "Safe expressions include:\n" +
      "  - github.actor, github.repository, github.run_id, etc.\n" +
      "  - github.event.issue.number, github.event.pull_request.number, etc.\n" +
      "  - needs.*, steps.*, env.*, inputs.*\n\n" +
      "See documentation for the complete list of allowed expressions.";
    throw new Error(errorMsg);
  }

  // Second pass: replace safe expressions with evaluated values
  let result = content;
  for (const [original, evaluated] of replacements.entries()) {
    result = result.replace(original, evaluated);
  }

  core.info(`Successfully processed ${replacements.size} safe expression(s) in ${source}`);
  return result;
}

/**
 * Checks if content contains GitHub Actions macros (${{ ... }})
 * @param {string} content - The content to check
 * @returns {boolean} - True if GitHub Actions macros are found
 */
function hasGitHubActionsMacros(content) {
  return /\$\{\{[\s\S]*?\}\}/.test(content);
}

/**
 * Fetches content from a URL with caching
 * @param {string} url - The URL to fetch
 * @param {string} cacheDir - Directory to store cached URL content
 * @returns {Promise<string>} - The fetched content
 * @throws {Error} - If URL fetch fails
 */
async function fetchUrlContent(url, cacheDir) {
  // Create cache directory if it doesn't exist
  if (!fs.existsSync(cacheDir)) {
    fs.mkdirSync(cacheDir, { recursive: true });
  }

  // Generate cache filename from URL (hash it for safety)
  const crypto = require("crypto");
  const urlHash = crypto.createHash("sha256").update(url).digest("hex");
  const cacheFile = path.join(cacheDir, `url-${urlHash}.cache`);

  // Check if cached version exists and is recent (less than 1 hour old)
  if (fs.existsSync(cacheFile)) {
    const stats = fs.statSync(cacheFile);
    const ageInMs = Date.now() - stats.mtimeMs;
    const oneHourInMs = 60 * 60 * 1000;

    if (ageInMs < oneHourInMs) {
      core.info(`Using cached content for URL: ${url}`);
      return fs.readFileSync(cacheFile, "utf8");
    }
  }

  // Fetch URL content
  core.info(`Fetching content from URL: ${url}`);

  return new Promise((resolve, reject) => {
    const protocol = url.startsWith("https") ? https : http;

    protocol
      .get(url, res => {
        if (res.statusCode !== 200) {
          reject(new Error(`Failed to fetch URL ${url}: HTTP ${res.statusCode}`));
          return;
        }

        let data = "";
        res.on("data", chunk => {
          data += chunk;
        });

        res.on("end", () => {
          // Cache the content
          fs.writeFileSync(cacheFile, data, "utf8");
          resolve(data);
        });
      })
      .on("error", err => {
        reject(new Error(`Failed to fetch URL ${url}: ${err.message}`));
      });
  });
}

/**
 * Processes a URL import and returns content with sanitization
 * @param {string} url - The URL to fetch
 * @param {boolean} optional - Whether the import is optional
 * @param {number} [startLine] - Optional start line (1-indexed, inclusive)
 * @param {number} [endLine] - Optional end line (1-indexed, inclusive)
 * @returns {Promise<string>} - The processed URL content
 * @throws {Error} - If URL fetch fails or content is invalid
 */
async function processUrlImport(url, optional, startLine, endLine) {
  const cacheDir = "/tmp/gh-aw/url-cache";

  // Fetch URL content (with caching)
  let content;
  try {
    content = await fetchUrlContent(url, cacheDir);
  } catch (error) {
    if (optional) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Optional runtime import URL failed: ${url}: ${errorMessage}`);
      return "";
    }
    throw error;
  }

  // If line range is specified, extract those lines first (before other processing)
  if (startLine !== undefined || endLine !== undefined) {
    const lines = content.split("\n");
    const totalLines = lines.length;

    // Validate line numbers (1-indexed)
    const start = startLine !== undefined ? startLine : 1;
    const end = endLine !== undefined ? endLine : totalLines;

    if (start < 1 || start > totalLines) {
      throw new Error(`Invalid start line ${start} for URL ${url} (total lines: ${totalLines})`);
    }
    if (end < 1 || end > totalLines) {
      throw new Error(`Invalid end line ${end} for URL ${url} (total lines: ${totalLines})`);
    }
    if (start > end) {
      throw new Error(`Start line ${start} cannot be greater than end line ${end} for URL ${url}`);
    }

    // Extract lines (convert to 0-indexed)
    content = lines.slice(start - 1, end).join("\n");
  }

  // Check for front matter and warn
  if (hasFrontMatter(content)) {
    core.warning(`URL ${url} contains front matter which will be ignored in runtime import`);
    // Remove front matter (everything between first --- and second ---)
    const lines = content.split("\n");
    let inFrontMatter = false;
    let frontMatterCount = 0;
    const processedLines = [];

    for (const line of lines) {
      if (line.trim() === "---" || line.trim() === "---\r") {
        frontMatterCount++;
        if (frontMatterCount === 1) {
          inFrontMatter = true;
          continue;
        } else if (frontMatterCount === 2) {
          inFrontMatter = false;
          continue;
        }
      }
      if (!inFrontMatter && frontMatterCount >= 2) {
        processedLines.push(line);
      }
    }
    content = processedLines.join("\n");
  }

  // Remove XML comments
  content = removeXMLComments(content);

  // Process GitHub Actions expressions (validate and render safe ones)
  if (hasGitHubActionsMacros(content)) {
    content = processExpressions(content, `URL ${url}`);
  }

  return content;
}

/**
 * Wraps bare GitHub expressions in template conditionals with ${{ }}
 * Transforms {{#if expression}} to {{#if ${{ expression }} }} if expression looks like a GitHub Actions expression
 * @param {string} content - The markdown content
 * @returns {string} - Content with GitHub expressions wrapped
 */
function wrapExpressionsInTemplateConditionals(content) {
  // Pattern to match {{#if expression}} where expression is not already wrapped in ${{ }}
  const pattern = /\{\{#if\s+((?:\$\{\{[^\}]*\}\}|[^\}])*?)\s*\}\}/g;

  return content.replace(pattern, (match, expr) => {
    const trimmed = expr.trim();

    // If already wrapped in ${{ }}, return as-is
    if (trimmed.startsWith("${{") && trimmed.endsWith("}}")) {
      return match;
    }

    // If it's an environment variable reference (starts with ${), return as-is
    if (trimmed.startsWith("${")) {
      return match;
    }

    // If it's a placeholder reference (starts with __), return as-is
    if (trimmed.startsWith("__")) {
      return match;
    }

    // Only wrap expressions that look like GitHub Actions expressions
    // GitHub Actions expressions typically contain dots (e.g., github.actor, github.event.issue.number)
    // or specific keywords (true, false, null)
    const looksLikeGitHubExpr =
      trimmed.includes(".") ||
      trimmed === "true" ||
      trimmed === "false" ||
      trimmed === "null" ||
      trimmed.startsWith("github.") ||
      trimmed.startsWith("needs.") ||
      trimmed.startsWith("steps.") ||
      trimmed.startsWith("env.") ||
      trimmed.startsWith("inputs.");

    if (!looksLikeGitHubExpr) {
      // Not a GitHub Actions expression, leave as-is
      return match;
    }

    // Wrap the expression
    return `{{#if \${{ ${trimmed} }} }}`;
  });
}

/**
 * Extracts GitHub expressions from wrapped template conditionals and replaces them with placeholders
 * Transforms {{#if ${{ expression }} }} to {{#if __GH_AW_PLACEHOLDER__ }}
 * @param {string} content - The markdown content with wrapped expressions
 * @returns {string} - Content with expressions replaced by placeholders
 */
function extractAndReplacePlaceholders(content) {
  // Pattern to match {{#if ${{ expression }} }} where expression needs to be extracted
  const pattern = /\{\{#if\s+\$\{\{\s*(.*?)\s*\}\}\s*\}\}/g;

  return content.replace(pattern, (match, expr) => {
    const trimmed = expr.trim();

    // Generate placeholder name from expression
    // Convert dots and special chars to underscores and uppercase
    const placeholder = generatePlaceholderName(trimmed);

    // Return the conditional with placeholder
    return `{{#if __${placeholder}__ }}`;
  });
}

/**
 * Generates a placeholder name from a GitHub expression
 * @param {string} expr - The GitHub expression (e.g., "github.event.issue.number")
 * @returns {string} - The placeholder name (e.g., "GH_AW_GITHUB_EVENT_ISSUE_NUMBER")
 */
function generatePlaceholderName(expr) {
  // Check if it's a simple property access chain (e.g., github.event.issue.number)
  const simplePattern = /^[a-zA-Z][a-zA-Z0-9_.]*$/;

  if (simplePattern.test(expr)) {
    // Convert dots to underscores and uppercase
    // e.g., "github.event.issue.number" -> "GH_AW_GITHUB_EVENT_ISSUE_NUMBER"
    return "GH_AW_" + expr.replace(/\./g, "_").toUpperCase();
  }

  // For boolean literals, use special placeholders
  if (expr === "true") {
    return "GH_AW_TRUE";
  }
  if (expr === "false") {
    return "GH_AW_FALSE";
  }
  if (expr === "null") {
    return "GH_AW_NULL";
  }

  // For complex expressions or unknown variables, create a generic placeholder
  // Replace non-alphanumeric characters with underscores
  const sanitized = expr.replace(/[^a-zA-Z0-9_]/g, "_").toUpperCase();
  return "GH_AW_" + sanitized;
}

/**
 * Reads and processes a file or URL for runtime import
 * @param {string} filepathOrUrl - The path to the file (relative to GITHUB_WORKSPACE) or URL to import
 * @param {boolean} optional - Whether the import is optional (true for {{#runtime-import? filepath}})
 * @param {string} workspaceDir - The GITHUB_WORKSPACE directory path
 * @param {number} [startLine] - Optional start line (1-indexed, inclusive)
 * @param {number} [endLine] - Optional end line (1-indexed, inclusive)
 * @returns {Promise<string>} - The processed file or URL content, or empty string if optional and file not found
 * @throws {Error} - If file/URL is not found and import is not optional, or if GitHub Actions macros are detected
 */
async function processRuntimeImport(filepathOrUrl, optional, workspaceDir, startLine, endLine) {
  // Check if this is a URL
  if (/^https?:\/\//i.test(filepathOrUrl)) {
    return await processUrlImport(filepathOrUrl, optional, startLine, endLine);
  }

  // Otherwise, process as a file
  let filepath = filepathOrUrl;

  // Trim .github/ prefix if provided (support both .github/file and file)
  // This allows users to use either format
  if (filepath.startsWith(".github/")) {
    filepath = filepath.substring(8); // Remove ".github/"
  } else if (filepath.startsWith(".github\\")) {
    filepath = filepath.substring(8); // Remove ".github\" (Windows)
  }

  // Remove leading ./ or ../ if present
  if (filepath.startsWith("./")) {
    filepath = filepath.substring(2);
  } else if (filepath.startsWith(".\\")) {
    filepath = filepath.substring(2);
  }
  // Note: We don't allow ../ paths as they would escape .github folder

  // Construct the path within .github folder
  const githubFolder = path.join(workspaceDir, ".github");
  const absolutePath = path.resolve(githubFolder, filepath);
  const normalizedPath = path.normalize(absolutePath);
  const normalizedGithubFolder = path.normalize(githubFolder);

  // Security check: ensure the resolved path is within the .github folder
  // Use path.relative to check if the path escapes the .github folder
  const relativePath = path.relative(normalizedGithubFolder, normalizedPath);
  if (relativePath.startsWith("..") || path.isAbsolute(relativePath)) {
    throw new Error(`Security: Path ${filepathOrUrl} must be within .github folder (resolves to: ${relativePath})`);
  }

  // Check if file exists
  if (!fs.existsSync(normalizedPath)) {
    if (optional) {
      core.warning(`Optional runtime import file not found: ${filepath}`);
      return "";
    }
    throw new Error(`Runtime import file not found: ${filepath}`);
  }

  // Read the file
  let content = fs.readFileSync(normalizedPath, "utf8");

  // If line range is specified, extract those lines first (before other processing)
  if (startLine !== undefined || endLine !== undefined) {
    const lines = content.split("\n");
    const totalLines = lines.length;

    // Validate line numbers (1-indexed)
    const start = startLine !== undefined ? startLine : 1;
    const end = endLine !== undefined ? endLine : totalLines;

    if (start < 1 || start > totalLines) {
      throw new Error(`Invalid start line ${start} for file ${filepath} (total lines: ${totalLines})`);
    }
    if (end < 1 || end > totalLines) {
      throw new Error(`Invalid end line ${end} for file ${filepath} (total lines: ${totalLines})`);
    }
    if (start > end) {
      throw new Error(`Start line ${start} cannot be greater than end line ${end} for file ${filepath}`);
    }

    // Extract lines (convert to 0-indexed)
    content = lines.slice(start - 1, end).join("\n");
  }

  // Check for front matter and warn
  if (hasFrontMatter(content)) {
    core.warning(`File ${filepath} contains front matter which will be ignored in runtime import`);
    // Remove front matter (everything between first --- and second ---)
    const lines = content.split("\n");
    let inFrontMatter = false;
    let frontMatterCount = 0;
    const processedLines = [];

    for (const line of lines) {
      if (line.trim() === "---" || line.trim() === "---\r") {
        frontMatterCount++;
        if (frontMatterCount === 1) {
          inFrontMatter = true;
          continue;
        } else if (frontMatterCount === 2) {
          inFrontMatter = false;
          continue;
        }
      }
      if (!inFrontMatter && frontMatterCount >= 2) {
        processedLines.push(line);
      }
    }
    content = processedLines.join("\n");
  }

  // Remove XML comments
  content = removeXMLComments(content);

  // Wrap expressions in template conditionals
  // This handles {{#if expression}} where expression is not already wrapped in ${{ }}
  content = wrapExpressionsInTemplateConditionals(content);

  // Extract and replace GitHub expressions in template conditionals with placeholders
  // This transforms {{#if ${{ expression }} }} to {{#if __GH_AW_PLACEHOLDER__ }}
  content = extractAndReplacePlaceholders(content);

  // Process GitHub Actions expressions (validate and render safe ones)
  if (hasGitHubActionsMacros(content)) {
    content = processExpressions(content, `File ${filepath}`);
  }

  return content;
}

/**
 * Processes all runtime-import macros in the content recursively
 * @param {string} content - The markdown content containing runtime-import macros
 * @param {string} workspaceDir - The GITHUB_WORKSPACE directory path
 * @param {Set<string>} [importedFiles] - Set of already imported files (for recursion tracking)
 * @param {Map<string, string>} [importCache] - Cache of imported file contents (for deduplication)
 * @param {Array<string>} [importStack] - Stack of currently importing files (for circular dependency detection)
 * @returns {Promise<string>} - Content with runtime-import macros replaced by file/URL contents
 */
async function processRuntimeImports(content, workspaceDir, importedFiles = new Set(), importCache = new Map(), importStack = []) {
  // Pattern to match {{#runtime-import filepath}} or {{#runtime-import? filepath}}
  // Captures: optional flag (?), whitespace, filepath/URL (which may include :startline-endline)
  const pattern = /\{\{#runtime-import(\?)?[ \t]+([^\}]+?)\}\}/g;

  let processedContent = content;
  const matches = [];
  let match;

  // Reset regex state and collect all matches
  pattern.lastIndex = 0;

  while ((match = pattern.exec(content)) !== null) {
    const optional = match[1] === "?";
    const filepathWithRange = match[2].trim();
    const fullMatch = match[0];

    // Parse filepath/URL and optional line range (filepath:startline-endline)
    const rangeMatch = filepathWithRange.match(/^(.+?):(\d+)-(\d+)$/);
    let filepathOrUrl, startLine, endLine;

    if (rangeMatch) {
      filepathOrUrl = rangeMatch[1];
      startLine = parseInt(rangeMatch[2], 10);
      endLine = parseInt(rangeMatch[3], 10);
    } else {
      filepathOrUrl = filepathWithRange;
      startLine = undefined;
      endLine = undefined;
    }

    matches.push({
      fullMatch,
      filepathOrUrl,
      optional,
      startLine,
      endLine,
      filepathWithRange,
    });
  }

  // Process all imports sequentially (to handle async URLs)
  for (const matchData of matches) {
    const { fullMatch, filepathOrUrl, optional, startLine, endLine, filepathWithRange } = matchData;

    // Check if this file is already in the import cache
    if (importCache.has(filepathWithRange)) {
      // Reuse cached content
      const cachedContent = importCache.get(filepathWithRange);
      if (cachedContent !== undefined) {
        processedContent = processedContent.replace(fullMatch, cachedContent);
        core.info(`Reusing cached content for ${filepathWithRange}`);
        continue;
      }
    }

    // Check for circular dependencies
    if (importStack.includes(filepathWithRange)) {
      const cycle = [...importStack, filepathWithRange].join(" -> ");
      throw new Error(`Circular dependency detected: ${cycle}`);
    }

    // Add to import stack for circular dependency detection
    importStack.push(filepathWithRange);

    try {
      // Import the file content
      let importedContent = await processRuntimeImport(filepathOrUrl, optional, workspaceDir, startLine, endLine);

      // Recursively process any runtime-import macros in the imported content
      if (importedContent && /\{\{#runtime-import/.test(importedContent)) {
        core.info(`Recursively processing runtime-imports in ${filepathWithRange}`);
        importedContent = await processRuntimeImports(importedContent, workspaceDir, importedFiles, importCache, [...importStack]);
      }

      // Cache the fully processed content
      importCache.set(filepathWithRange, importedContent);
      importedFiles.add(filepathWithRange);

      // Replace the macro with the imported content
      processedContent = processedContent.replace(fullMatch, importedContent);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      throw new Error(`Failed to process runtime import for ${filepathWithRange}: ${errorMessage}`);
    } finally {
      // Remove from import stack
      importStack.pop();
    }
  }

  return processedContent;
}

module.exports = {
  processRuntimeImports,
  processRuntimeImport,
  hasFrontMatter,
  removeXMLComments,
  hasGitHubActionsMacros,
  isSafeExpression,
  evaluateExpression,
  processExpressions,
  wrapExpressionsInTemplateConditionals,
  extractAndReplacePlaceholders,
  generatePlaceholderName,
};
