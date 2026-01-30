// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Merge remote agent repository's .github folder into current repository
 *
 * This script handles importing .github folder content from repositories that contain
 * agent files. It uses sparse checkout to efficiently download only the .github folder
 * and merges it into the current repository, failing on conflicts.
 *
 * Environment Variables:
 * - GH_AW_AGENT_FILE: Path to the agent file (e.g., ".github/agents/my-agent.md")
 * - GH_AW_AGENT_IMPORT_SPEC: Import specification (e.g., "owner/repo/.github/agents/agent.md@v1.0.0")
 * - GITHUB_WORKSPACE: Path to the current repository workspace
 */

const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const core = require("@actions/core");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Parse the agent import specification to extract repository details
 * Format: owner/repo/path@ref or owner/repo/path
 * @param {string} importSpec - The import specification
 * @returns {{owner: string, repo: string, ref: string} | null}
 */
function parseAgentImportSpec(importSpec) {
  if (!importSpec) {
    return null;
  }

  core.info(`Parsing agent import spec: ${importSpec}`);

  // Remove section reference if present (file.md#Section)
  let cleanSpec = importSpec;
  if (importSpec.includes("#")) {
    cleanSpec = importSpec.split("#")[0];
  }

  // Split on @ to get path and ref
  const parts = cleanSpec.split("@");
  const pathPart = parts[0];
  const ref = parts.length > 1 ? parts[1] : "main";

  // Parse path: owner/repo/path/to/file.md
  const slashParts = pathPart.split("/");
  if (slashParts.length < 3) {
    core.warning(`Invalid agent import spec format: ${importSpec}`);
    return null;
  }

  const owner = slashParts[0];
  const repo = slashParts[1];

  // Check if this is a local import (starts with . or doesn't have owner/repo format)
  if (owner.startsWith(".") || owner.includes("github/workflows")) {
    core.info("Agent import is local, skipping remote .github folder merge");
    return null;
  }

  core.info(`Parsed: owner=${owner}, repo=${repo}, ref=${ref}`);
  return { owner, repo, ref };
}

/**
 * Check if a path exists
 * @param {string} filePath - Path to check
 * @returns {boolean}
 */
function pathExists(filePath) {
  try {
    fs.accessSync(filePath, fs.constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * Recursively get all files in a directory
 * @param {string} dir - Directory to scan
 * @param {string} baseDir - Base directory for relative paths
 * @returns {string[]} Array of relative file paths
 */
function getAllFiles(dir, baseDir = dir) {
  const files = [];
  const items = fs.readdirSync(dir);

  for (const item of items) {
    const fullPath = path.join(dir, item);
    const stat = fs.statSync(fullPath);

    if (stat.isDirectory()) {
      files.push(...getAllFiles(fullPath, baseDir));
    } else {
      files.push(path.relative(baseDir, fullPath));
    }
  }

  return files;
}

/**
 * Sparse checkout the .github folder from a remote repository
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} ref - Git reference (branch, tag, or SHA)
 * @param {string} tempDir - Temporary directory for checkout
 */
function sparseCheckoutGithubFolder(owner, repo, ref, tempDir) {
  core.info(`Performing sparse checkout of .github folder from ${owner}/${repo}@${ref}`);

  const repoUrl = `https://github.com/${owner}/${repo}.git`;

  try {
    // Initialize git repository
    execSync("git init", { cwd: tempDir, stdio: "pipe" });
    core.info("Initialized temporary git repository");

    // Configure sparse checkout
    execSync("git config core.sparseCheckout true", { cwd: tempDir, stdio: "pipe" });
    core.info("Enabled sparse checkout");

    // Set sparse checkout pattern to only include .github folder
    const sparseCheckoutFile = path.join(tempDir, ".git", "info", "sparse-checkout");
    fs.writeFileSync(sparseCheckoutFile, ".github/\n");
    core.info("Configured sparse checkout pattern: .github/");

    // Add remote
    execSync(`git remote add origin ${repoUrl}`, { cwd: tempDir, stdio: "pipe" });
    core.info(`Added remote: ${repoUrl}`);

    // Fetch and checkout
    core.info(`Fetching ref: ${ref}`);
    execSync(`git fetch --depth 1 origin ${ref}`, { cwd: tempDir, stdio: "pipe" });

    core.info("Checking out .github folder");
    execSync(`git checkout FETCH_HEAD`, { cwd: tempDir, stdio: "pipe" });

    core.info("Sparse checkout completed successfully");
  } catch (error) {
    throw new Error(`Sparse checkout failed: ${getErrorMessage(error)}`);
  }
}

/**
 * Merge .github folder from source to destination, failing on conflicts
 * @param {string} sourcePath - Source .github folder path
 * @param {string} destPath - Destination .github folder path
 * @returns {{merged: number, conflicts: string[]}}
 */
function mergeGithubFolder(sourcePath, destPath) {
  core.info(`Merging .github folder from ${sourcePath} to ${destPath}`);

  const conflicts = [];
  let mergedCount = 0;

  // Get all files from source .github folder
  const sourceFiles = getAllFiles(sourcePath);
  core.info(`Found ${sourceFiles.length} files in source .github folder`);

  for (const relativePath of sourceFiles) {
    const sourceFile = path.join(sourcePath, relativePath);
    const destFile = path.join(destPath, relativePath);

    // Check if destination file exists
    if (pathExists(destFile)) {
      // Compare file contents
      const sourceContent = fs.readFileSync(sourceFile);
      const destContent = fs.readFileSync(destFile);

      if (!sourceContent.equals(destContent)) {
        conflicts.push(relativePath);
        core.error(`Conflict detected: ${relativePath}`);
      } else {
        core.info(`File already exists with same content: ${relativePath}`);
      }
    } else {
      // Copy file to destination
      const destDir = path.dirname(destFile);
      if (!pathExists(destDir)) {
        fs.mkdirSync(destDir, { recursive: true });
        core.info(`Created directory: ${path.relative(destPath, destDir)}`);
      }

      fs.copyFileSync(sourceFile, destFile);
      mergedCount++;
      core.info(`Merged file: ${relativePath}`);
    }
  }

  return { merged: mergedCount, conflicts };
}

/**
 * Main execution
 */
async function main() {
  try {
    core.info("Starting remote agent .github folder merge");

    // Get agent file path from environment
    const agentFile = process.env.GH_AW_AGENT_FILE;
    if (!agentFile) {
      core.info("No GH_AW_AGENT_FILE specified, skipping .github folder merge");
      return;
    }

    core.info(`Agent file: ${agentFile}`);

    // Get agent import specification
    const importSpec = process.env.GH_AW_AGENT_IMPORT_SPEC;
    if (!importSpec) {
      core.info("No GH_AW_AGENT_IMPORT_SPEC specified, assuming local agent");
      return;
    }

    core.info(`Agent import spec: ${importSpec}`);

    // Parse import specification
    const parsed = parseAgentImportSpec(importSpec);
    if (!parsed) {
      core.info("Agent is local or import spec is invalid, skipping remote merge");
      return;
    }

    const { owner, repo, ref } = parsed;
    core.info(`Remote agent detected: ${owner}/${repo}@${ref}`);

    // Get workspace path
    const workspace = process.env.GITHUB_WORKSPACE;
    if (!workspace) {
      throw new Error("GITHUB_WORKSPACE environment variable not set");
    }

    core.info(`Workspace: ${workspace}`);

    // Create temporary directory for sparse checkout
    const tempDir = path.join("/tmp", `gh-aw-agent-merge-${Date.now()}`);
    fs.mkdirSync(tempDir, { recursive: true });
    core.info(`Created temporary directory: ${tempDir}`);

    try {
      // Sparse checkout .github folder from remote repository
      sparseCheckoutGithubFolder(owner, repo, ref, tempDir);

      // Check if .github folder exists in remote repository
      const sourceGithubFolder = path.join(tempDir, ".github");
      if (!pathExists(sourceGithubFolder)) {
        core.warning(`Remote repository ${owner}/${repo}@${ref} does not contain a .github folder`);
        return;
      }

      // Merge .github folder into current repository
      const destGithubFolder = path.join(workspace, ".github");

      // Ensure destination .github folder exists
      if (!pathExists(destGithubFolder)) {
        fs.mkdirSync(destGithubFolder, { recursive: true });
        core.info("Created .github folder in workspace");
      }

      const { merged, conflicts } = mergeGithubFolder(sourceGithubFolder, destGithubFolder);

      // Report results
      if (conflicts.length > 0) {
        core.error(`Found ${conflicts.length} file conflicts:`);
        for (const conflict of conflicts) {
          core.error(`  - ${conflict}`);
        }
        throw new Error(`Cannot merge .github folder from ${owner}/${repo}@${ref}: ${conflicts.length} file(s) conflict with existing files`);
      }

      if (merged > 0) {
        core.info(`Successfully merged ${merged} file(s) from ${owner}/${repo}@${ref}`);
      } else {
        core.info("No new files to merge");
      }
    } finally {
      // Clean up temporary directory
      if (pathExists(tempDir)) {
        fs.rmSync(tempDir, { recursive: true, force: true });
        core.info("Cleaned up temporary directory");
      }
    }

    core.info("Remote agent .github folder merge completed successfully");
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.setFailed(`Failed to merge remote agent .github folder: ${errorMessage}`);
  }
}

// Run if executed directly (not imported)
if (require.main === module) {
  main();
}

module.exports = {
  parseAgentImportSpec,
  pathExists,
  getAllFiles,
  sparseCheckoutGithubFolder,
  mergeGithubFolder,
  main,
};
