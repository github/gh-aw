// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { execGitSync } = require("./git_helpers.cjs");

/**
 * Push repo-memory changes to git branch
 * Environment variables:
 *   ARTIFACT_DIR: Path to the downloaded artifact directory containing memory files
 *   MEMORY_ID: Memory identifier (used for subdirectory path)
 *   TARGET_REPO: Target repository (owner/name)
 *   BRANCH_NAME: Branch name to push to
 *   MAX_FILE_SIZE: Maximum file size in bytes
 *   MAX_FILE_COUNT: Maximum number of files per commit
 *   FILE_GLOB_FILTER: Optional space-separated list of file patterns (e.g., "*.md metrics/** data/**")
 *                     Supports * (matches any chars except /) and ** (matches any chars including /)
 *
 *                     IMPORTANT: Patterns are matched against the RELATIVE FILE PATH from the artifact directory,
 *                     NOT against the branch path. Do NOT include the branch name in the patterns.
 *
 *                     Example:
 *                       BRANCH_NAME: memory/code-metrics
 *                       Artifact file: /tmp/gh-aw/repo-memory/default/history.jsonl
 *                       Relative path tested: "history.jsonl"
 *                       CORRECT pattern: "*.jsonl"
 *                       INCORRECT pattern: "memory/code-metrics/*.jsonl"  (includes branch name)
 *
 *                     The branch name is used for git operations (checkout, push) but not for pattern matching.
 *   GH_TOKEN: GitHub token for authentication
 *   GITHUB_RUN_ID: Workflow run ID for commit messages
 */

async function main() {
  const artifactDir = process.env.ARTIFACT_DIR;
  const memoryId = process.env.MEMORY_ID;
  const targetRepo = process.env.TARGET_REPO;
  const branchName = process.env.BRANCH_NAME;
  const maxFileSize = parseInt(process.env.MAX_FILE_SIZE || "10240", 10);
  const maxFileCount = parseInt(process.env.MAX_FILE_COUNT || "100", 10);
  const fileGlobFilter = process.env.FILE_GLOB_FILTER || "";
  const ghToken = process.env.GH_TOKEN;
  const githubRunId = process.env.GITHUB_RUN_ID || "unknown";

  // Log environment variable configuration for debugging
  core.info("Environment configuration:");
  core.info(`  MEMORY_ID: ${memoryId}`);
  core.info(`  MAX_FILE_SIZE: ${maxFileSize}`);
  core.info(`  MAX_FILE_COUNT: ${maxFileCount}`);
  core.info(`  FILE_GLOB_FILTER: ${fileGlobFilter ? `"${fileGlobFilter}"` : "(empty - all files accepted)"}`);
  core.info(`  FILE_GLOB_FILTER length: ${fileGlobFilter.length}`);

  /** @param {unknown} value */
  function isPlainObject(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
  }

  /** @param {string} absPath */
  function tryParseJSONFile(absPath) {
    const raw = fs.readFileSync(absPath, "utf8");
    if (!raw.trim()) {
      throw new Error(`Empty JSON file: ${absPath}`);
    }
    try {
      return JSON.parse(raw);
    } catch (e) {
      throw new Error(`Invalid JSON in ${absPath}: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  // Validate required environment variables
  if (!artifactDir || !memoryId || !targetRepo || !branchName || !ghToken) {
    core.setFailed("Missing required environment variables: ARTIFACT_DIR, MEMORY_ID, TARGET_REPO, BRANCH_NAME, GH_TOKEN");
    return;
  }

  // Source directory with memory files (artifact location)
  // The artifactDir IS the memory directory (no nested structure needed)
  const sourceMemoryPath = artifactDir;

  // Check if artifact memory directory exists
  if (!fs.existsSync(sourceMemoryPath)) {
    core.info(`Memory directory not found in artifact: ${sourceMemoryPath}`);
    return;
  }

  // We're already in the checked out repository (from checkout step)
  const workspaceDir = process.env.GITHUB_WORKSPACE || process.cwd();
  core.info(`Working in repository: ${workspaceDir}`);

  // Disable sparse checkout to work with full branch content
  // This is necessary because checkout was configured with sparse-checkout
  core.info(`Disabling sparse checkout...`);
  try {
    execGitSync(["sparse-checkout", "disable"], { stdio: "pipe" });
  } catch (error) {
    // Ignore if sparse checkout wasn't enabled
    core.info("Sparse checkout was not enabled or already disabled");
  }

  // Checkout or create the memory branch
  core.info(`Checking out branch: ${branchName}...`);
  try {
    const repoUrl = `https://x-access-token:${ghToken}@github.com/${targetRepo}.git`;

    // Try to fetch the branch
    try {
      execGitSync(["fetch", repoUrl, `${branchName}:${branchName}`], { stdio: "pipe" });
      execGitSync(["checkout", branchName], { stdio: "inherit" });
      core.info(`Checked out existing branch: ${branchName}`);
    } catch (fetchError) {
      // Branch doesn't exist, create orphan branch
      core.info(`Branch ${branchName} does not exist, creating orphan branch...`);
      execGitSync(["checkout", "--orphan", branchName], { stdio: "inherit" });
      // Use --ignore-unmatch to avoid failure when directory is empty
      execGitSync(["rm", "-r", "-f", "--ignore-unmatch", "."], { stdio: "pipe" });
      core.info(`Created orphan branch: ${branchName}`);
    }
  } catch (error) {
    core.setFailed(`Failed to checkout branch: ${getErrorMessage(error)}`);
    return;
  }

  // Create destination directory in repo
  // Files are copied to the root of the checked-out branch (workspaceDir)
  // The branch name (e.g., "memory/campaigns") identifies the branch,
  // but files go at the branch root, not in a nested subdirectory
  const destMemoryPath = workspaceDir;
  core.info(`Destination directory: ${destMemoryPath}`);

  // Recursively scan and collect files from artifact directory
  let filesToCopy = [];

  // Log the file glob filter configuration
  if (fileGlobFilter) {
    core.info(`File glob filter enabled: ${fileGlobFilter}`);
    const patternCount = fileGlobFilter.trim().split(/\s+/).filter(Boolean).length;
    core.info(`Number of patterns: ${patternCount}`);
  } else {
    core.info("No file glob filter - all files will be accepted");
  }

  /**
   * Recursively scan directory and collect files
   * @param {string} dirPath - Directory to scan
   * @param {string} relativePath - Relative path from sourceMemoryPath (for nested files)
   */
  function scanDirectory(dirPath, relativePath = "") {
    const entries = fs.readdirSync(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);
      const relativeFilePath = relativePath ? path.join(relativePath, entry.name) : entry.name;

      if (entry.isDirectory()) {
        // Recursively scan subdirectory
        scanDirectory(fullPath, relativeFilePath);
      } else if (entry.isFile()) {
        const stats = fs.statSync(fullPath);

        // Validate file name patterns if filter is set
        if (fileGlobFilter) {
          const patterns = fileGlobFilter.trim().split(/\s+/).filter(Boolean).map(globPatternToRegex);

          // Test patterns against the relative file path within the memory directory
          // Patterns are specified relative to the memory artifact directory, not the branch path
          const normalizedRelPath = relativeFilePath.replace(/\\/g, "/");

          // Enhanced logging: Show what we're testing (use info for first file to aid debugging)
          core.debug(`Testing file: ${normalizedRelPath}`);
          core.debug(`File glob filter: ${fileGlobFilter}`);
          core.debug(`Number of patterns: ${patterns.length}`);

          const matchResults = patterns.map((pattern, idx) => {
            const matches = pattern.test(normalizedRelPath);
            const patternStr = fileGlobFilter.trim().split(/\s+/).filter(Boolean)[idx];
            core.debug(`  Pattern ${idx + 1}: "${patternStr}" -> ${pattern.source} -> ${matches ? "✓ MATCH" : "✗ NO MATCH"}`);
            return matches;
          });

          if (!matchResults.some(m => m)) {
            // Enhanced warning with more context about the filtering issue
            core.warning(`Skipping file that does not match allowed patterns: ${normalizedRelPath}`);
            core.info(`  File path being tested (relative to artifact): ${normalizedRelPath}`);
            core.info(`  Configured patterns: ${fileGlobFilter}`);
            const patternStrs = fileGlobFilter.trim().split(/\s+/).filter(Boolean);
            patterns.forEach((pattern, idx) => {
              core.info(`    Pattern: "${patternStrs[idx]}" -> Regex: ${pattern.source} -> ${matchResults[idx] ? "✅ MATCH" : "❌ NO MATCH"}`);
            });
            core.info(`  Note: Patterns are matched against the full relative file path from the artifact directory.`);
            core.info(`  If patterns include directory prefixes (like 'branch-name/'), ensure files are organized that way in the artifact.`);
            // Skip this file instead of failing - it may be from a previous run with different patterns
            return;
          }
        }

        // Validate file size
        if (stats.size > maxFileSize) {
          core.error(`File exceeds size limit: ${relativeFilePath} (${stats.size} bytes > ${maxFileSize} bytes)`);
          core.setFailed("File size validation failed");
          throw new Error("File size validation failed");
        }

        filesToCopy.push({
          relativePath: relativeFilePath,
          source: fullPath,
          size: stats.size,
        });
      }
    }
  }

  try {
    scanDirectory(sourceMemoryPath);
    core.info(`Scan complete: Found ${filesToCopy.length} file(s) to copy`);
    if (filesToCopy.length > 0 && filesToCopy.length <= 10) {
      core.info("Files found:");
      filesToCopy.forEach(f => core.info(`  - ${f.relativePath} (${f.size} bytes)`));
    } else if (filesToCopy.length > 10) {
      core.info(`First 10 files:`);
      filesToCopy.slice(0, 10).forEach(f => core.info(`  - ${f.relativePath} (${f.size} bytes)`));
      core.info(`  ... and ${filesToCopy.length - 10} more`);
    }
  } catch (error) {
    core.setFailed(`Failed to scan artifact directory: ${getErrorMessage(error)}`);
    return;
  }

  // Validate file count
  if (filesToCopy.length > maxFileCount) {
    core.setFailed(`Too many files (${filesToCopy.length} > ${maxFileCount})`);
    return;
  }

  if (filesToCopy.length === 0) {
    core.info("No files to copy from artifact");
    return;
  }

  core.info(`Copying ${filesToCopy.length} validated file(s)...`);

  // Copy files to destination (preserving directory structure)
  for (const file of filesToCopy) {
    const destFilePath = path.join(destMemoryPath, file.relativePath);
    const destDir = path.dirname(destFilePath);

    try {
      // Path traversal protection
      const resolvedRoot = path.resolve(destMemoryPath) + path.sep;
      const resolvedDest = path.resolve(destFilePath);
      if (!resolvedDest.startsWith(resolvedRoot)) {
        core.setFailed(`Refusing to write outside repo-memory directory: ${file.relativePath}`);
        return;
      }

      // Ensure destination directory exists
      fs.mkdirSync(destDir, { recursive: true });

      // Copy file
      fs.copyFileSync(file.source, destFilePath);
      core.info(`Copied: ${file.relativePath} (${file.size} bytes)`);
    } catch (error) {
      core.setFailed(`Failed to copy file ${file.relativePath}: ${getErrorMessage(error)}`);
      return;
    }
  }

  // Check if we have any changes to commit
  let hasChanges = false;
  try {
    const status = execGitSync(["status", "--porcelain"]);
    hasChanges = status.trim().length > 0;
  } catch (error) {
    core.setFailed(`Failed to check git status: ${getErrorMessage(error)}`);
    return;
  }

  if (!hasChanges) {
    core.info("No changes detected after copying files");
    return;
  }

  core.info("Changes detected, committing and pushing...");

  // Stage all changes
  try {
    execGitSync(["add", "."], { stdio: "inherit" });
  } catch (error) {
    core.setFailed(`Failed to stage changes: ${getErrorMessage(error)}`);
    return;
  }

  // Commit changes
  try {
    execGitSync(["commit", "-m", `Update repo memory from workflow run ${githubRunId}`], { stdio: "inherit" });
  } catch (error) {
    core.setFailed(`Failed to commit changes: ${getErrorMessage(error)}`);
    return;
  }

  // Pull with merge strategy (ours wins on conflicts)
  core.info(`Pulling latest changes from ${branchName}...`);
  try {
    const repoUrl = `https://x-access-token:${ghToken}@github.com/${targetRepo}.git`;
    execGitSync(["pull", "--no-rebase", "-X", "ours", repoUrl, branchName], { stdio: "inherit" });
  } catch (error) {
    // Pull might fail if branch doesn't exist yet or on conflicts - this is acceptable
    core.warning(`Pull failed (this may be expected): ${getErrorMessage(error)}`);
  }

  // Push changes
  core.info(`Pushing changes to ${branchName}...`);
  try {
    const repoUrl = `https://x-access-token:${ghToken}@github.com/${targetRepo}.git`;
    execGitSync(["push", repoUrl, `HEAD:${branchName}`], { stdio: "inherit" });
    core.info(`Successfully pushed changes to ${branchName} branch`);
  } catch (error) {
    core.setFailed(`Failed to push changes: ${getErrorMessage(error)}`);
    return;
  }
}

module.exports = { main };
