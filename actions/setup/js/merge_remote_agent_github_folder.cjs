// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Merge remote repository's .github folder into current repository
 *
 * This script handles importing .github folder content from remote repositories.
 * It uses sparse checkout to efficiently download only the .github folder
 * and merges it into the current repository, failing on conflicts.
 *
 * This script runs in a github-script context where core object
 * is available globally. Do NOT use npm packages from the actions org.
 *
 * Environment Variables:
 * - GH_AW_REPOSITORY_IMPORTS: JSON array of repository imports (e.g., '["owner/repo@ref"]')
 * - GH_AW_AGENT_FILE: Path to the agent file (e.g., ".github/agents/my-agent.md") [legacy]
 * - GH_AW_AGENT_IMPORT_SPEC: Import specification (e.g., "owner/repo/.github/agents/agent.md@v1.0.0") [legacy]
 * - GITHUB_WORKSPACE: Path to the current repository workspace
 */

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const { getErrorMessage } = require("./error_helpers.cjs");

// Get the core object - in github-script context it's global, for testing we create a minimal version
const coreObj =
  typeof core !== "undefined"
    ? core
    : {
        info: msg => console.log(msg),
        warning: msg => console.warn(msg),
        error: msg => console.error(msg),
        setFailed: msg => {
          console.error(msg);
          process.exitCode = 1;
        },
      };

/**
 * Parse the agent import specification to extract repository details
 * Format: owner/repo/path@ref or owner/repo@ref or owner/repo/path
 * @param {string} importSpec - The import specification
 * @returns {{owner: string, repo: string, ref: string} | null}
 */
function parseAgentImportSpec(importSpec) {
  if (!importSpec) {
    return null;
  }

  coreObj.info(`Parsing import spec: ${importSpec}`);

  // Remove section reference if present (file.md#Section)
  let cleanSpec = importSpec;
  if (importSpec.includes("#")) {
    cleanSpec = importSpec.split("#")[0];
  }

  // Split on @ to get path and ref
  const parts = cleanSpec.split("@");
  const pathPart = parts[0];
  const ref = parts.length > 1 ? parts[1] : "main";

  // Parse path: owner/repo or owner/repo/path/to/file.md
  const slashParts = pathPart.split("/");
  if (slashParts.length < 2) {
    coreObj.warning(`Invalid import spec format: ${importSpec}`);
    return null;
  }

  const owner = slashParts[0];
  const repo = slashParts[1];

  // Check if this is a local import (starts with . or doesn't have owner/repo format)
  if (owner.startsWith(".") || owner.includes("github/workflows")) {
    coreObj.info("Import is local, skipping remote .github folder merge");
    return null;
  }

  coreObj.info(`Parsed: owner=${owner}, repo=${repo}, ref=${ref}`);
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
    // Validate that item doesn't contain path traversal sequences
    validateSafePath(item, dir, "directory item");
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
 * Validate that a string only contains safe characters for git operations
 * @param {string} value - Value to validate
 * @param {string} name - Name of the parameter (for error messages)
 * @throws {Error} If value contains unsafe characters
 */
function validateGitParameter(value, name) {
  // Allow alphanumeric, hyphens, underscores, dots, and forward slashes
  // This is safe for git owner/repo/ref names
  const safePattern = /^[a-zA-Z0-9._/-]+$/;
  if (!safePattern.test(value)) {
    throw new Error(`Invalid ${name}: contains unsafe characters. Only alphanumeric, hyphens, underscores, dots, and forward slashes are allowed.`);
  }
}

/**
 * Validate path to prevent path traversal attacks
 * @param {string} userPath - Path component from user input
 * @param {string} basePath - Base path that result must be within
 * @param {string} name - Name of the parameter (for error messages)
 * @throws {Error} If path attempts to traverse outside base path
 */
function validateSafePath(userPath, basePath, name) {
  // Reject paths with null bytes
  if (userPath.includes('\0')) {
    throw new Error(`Invalid ${name}: contains null bytes`);
  }
  
  // Reject paths that attempt to traverse up (..)
  if (userPath.includes('..')) {
    throw new Error(`Invalid ${name}: path traversal detected`);
  }
  
  // Resolve the full path and ensure it's within the base path
  const resolvedPath = path.resolve(basePath, userPath);
  const resolvedBase = path.resolve(basePath);
  
  if (!resolvedPath.startsWith(resolvedBase + path.sep) && resolvedPath !== resolvedBase) {
    throw new Error(`Invalid ${name}: path escapes base directory`);
  }
  
  return resolvedPath;
}

/**
 * Sparse checkout the .github folder from a remote repository
 * @deprecated This function is no longer used. The compiler now generates actions/checkout steps
 * that checkout repositories into .github/aw/imports/ (relative to GITHUB_WORKSPACE), and mergeRepositoryGithubFolder uses those.
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} ref - Git reference (branch, tag, or SHA)
 * @param {string} tempDir - Temporary directory for checkout
 */
function sparseCheckoutGithubFolder(owner, repo, ref, tempDir) {
  coreObj.info(`Performing sparse checkout of .github folder from ${owner}/${repo}@${ref}`);

  // Validate inputs to prevent command injection
  validateGitParameter(owner, "owner");
  validateGitParameter(repo, "repo");
  validateGitParameter(ref, "ref");

  const repoUrl = `https://github.com/${owner}/${repo}.git`;

  try {
    // Initialize git repository
    execFileSync("git", ["init"], { cwd: tempDir, stdio: "pipe" });
    coreObj.info("Initialized temporary git repository");

    // Configure sparse checkout
    execFileSync("git", ["config", "core.sparseCheckout", "true"], { cwd: tempDir, stdio: "pipe" });
    coreObj.info("Enabled sparse checkout");

    // Set sparse checkout pattern to only include .github folder
    const sparseCheckoutFile = path.join(tempDir, ".git", "info", "sparse-checkout");
    fs.writeFileSync(sparseCheckoutFile, ".github/\n");
    coreObj.info("Configured sparse checkout pattern: .github/");

    // Add remote - using execFileSync prevents shell injection
    execFileSync("git", ["remote", "add", "origin", repoUrl], { cwd: tempDir, stdio: "pipe" });
    coreObj.info(`Added remote: ${repoUrl}`);

    // Fetch and checkout - using execFileSync with validated ref
    coreObj.info(`Fetching ref: ${ref}`);
    execFileSync("git", ["fetch", "--depth", "1", "origin", ref], { cwd: tempDir, stdio: "pipe" });

    coreObj.info("Checking out .github folder");
    execFileSync("git", ["checkout", "FETCH_HEAD"], { cwd: tempDir, stdio: "pipe" });

    coreObj.info("Sparse checkout completed successfully");
  } catch (error) {
    throw new Error(`Sparse checkout failed: ${getErrorMessage(error)}`);
  }
}

/**
 * Merge .github folder from source to destination, failing on conflicts
 * Only copies files from specific subfolders: agents, skills, prompts, instructions, plugins
 * @param {string} sourcePath - Source .github folder path
 * @param {string} destPath - Destination .github folder path
 * @returns {{merged: number, conflicts: string[]}}
 */
function mergeGithubFolder(sourcePath, destPath) {
  coreObj.info(`Merging .github folder from ${sourcePath} to ${destPath}`);

  const conflicts = [];
  let mergedCount = 0;

  // Only copy files from these specific subfolders
  const allowedSubfolders = ["agents", "skills", "prompts", "instructions", "plugins"];

  // Get all files from source .github folder
  const sourceFiles = getAllFiles(sourcePath);
  coreObj.info(`Found ${sourceFiles.length} files in source .github folder`);

  for (const relativePath of sourceFiles) {
    // Validate relative path to prevent path traversal
    validateSafePath(relativePath, sourcePath, "relative file path");
    
    // Check if the file is in one of the allowed subfolders
    const pathParts = relativePath.split(path.sep);
    const topLevelFolder = pathParts[0];

    if (!allowedSubfolders.includes(topLevelFolder)) {
      coreObj.info(`Skipping file outside allowed subfolders: ${relativePath}`);
      continue;
    }

    const sourceFile = path.join(sourcePath, relativePath);
    const destFile = path.join(destPath, relativePath);

    // Check if destination file exists
    if (pathExists(destFile)) {
      // Compare file contents
      const sourceContent = fs.readFileSync(sourceFile);
      const destContent = fs.readFileSync(destFile);

      if (!sourceContent.equals(destContent)) {
        conflicts.push(relativePath);
        coreObj.error(`Conflict detected: ${relativePath}`);
      } else {
        coreObj.info(`File already exists with same content: ${relativePath}`);
      }
    } else {
      // Copy file to destination
      const destDir = path.dirname(destFile);
      if (!pathExists(destDir)) {
        fs.mkdirSync(destDir, { recursive: true });
        coreObj.info(`Created directory: ${path.relative(destPath, destDir)}`);
      }

      fs.copyFileSync(sourceFile, destFile);
      mergedCount++;
      coreObj.info(`Merged file: ${relativePath}`);
    }
  }

  return { merged: mergedCount, conflicts };
}

/**
 * Merge a repository's .github folder into the workspace using pre-checked-out folder
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} ref - Git reference
 * @param {string} workspace - Workspace path
 */
async function mergeRepositoryGithubFolder(owner, repo, ref, workspace) {
  coreObj.info(`Merging .github folder from ${owner}/${repo}@${ref} into workspace`);

  // Calculate the pre-checked-out folder path
  // This matches the format generated by the compiler: .github/aw/imports/<owner>-<repo>-<sanitized-ref>
  // The path is relative to GITHUB_WORKSPACE, so we need to resolve it
  const sanitizedRef = ref.replace(/\//g, "-").replace(/:/g, "-").replace(/\\/g, "-");
  const relativePath = `.github/aw/imports/${owner}-${repo}-${sanitizedRef}`;
  const checkoutPath = path.join(workspace, relativePath);

  coreObj.info(`Looking for pre-checked-out repository at: ${checkoutPath}`);

  // Check if the pre-checked-out folder exists
  if (!pathExists(checkoutPath)) {
    throw new Error(`Pre-checked-out repository not found at ${checkoutPath}. The actions/checkout step may have failed.`);
  }

  // Check if .github folder exists in the checked-out repository
  const sourceGithubFolder = path.join(checkoutPath, ".github");
  if (!pathExists(sourceGithubFolder)) {
    coreObj.warning(`Remote repository ${owner}/${repo}@${ref} does not contain a .github folder`);
    return;
  }

  // Merge .github folder into current repository
  const destGithubFolder = path.join(workspace, ".github");

  // Ensure destination .github folder exists
  if (!pathExists(destGithubFolder)) {
    fs.mkdirSync(destGithubFolder, { recursive: true });
    coreObj.info("Created .github folder in workspace");
  }

  const { merged, conflicts } = mergeGithubFolder(sourceGithubFolder, destGithubFolder);

  // Report results
  if (conflicts.length > 0) {
    coreObj.error(`Found ${conflicts.length} file conflicts:`);
    for (const conflict of conflicts) {
      coreObj.error(`  - ${conflict}`);
    }
    throw new Error(`Cannot merge .github folder from ${owner}/${repo}@${ref}: ${conflicts.length} file(s) conflict with existing files`);
  }

  if (merged > 0) {
    coreObj.info(`Successfully merged ${merged} file(s) from ${owner}/${repo}@${ref}`);
  } else {
    coreObj.info("No new files to merge");
  }
}

/**
 * Main execution
 */
async function main() {
  try {
    coreObj.info("Starting remote .github folder merge");

    // Check for repository imports (owner/repo@ref format - merge entire .github folder)
    const repositoryImportsEnv = process.env.GH_AW_REPOSITORY_IMPORTS;
    if (repositoryImportsEnv) {
      coreObj.info(`Repository imports detected: ${repositoryImportsEnv}`);

      // Parse the JSON array of repository imports
      let repositoryImports;
      try {
        repositoryImports = JSON.parse(repositoryImportsEnv);
      } catch (error) {
        throw new Error(`Failed to parse GH_AW_REPOSITORY_IMPORTS: ${getErrorMessage(error)}`);
      }

      if (!Array.isArray(repositoryImports)) {
        throw new Error("GH_AW_REPOSITORY_IMPORTS must be a JSON array");
      }

      // Get workspace path
      const workspace = process.env.GITHUB_WORKSPACE;
      if (!workspace) {
        throw new Error("GITHUB_WORKSPACE environment variable not set");
      }

      // Process each repository import
      for (const repoImport of repositoryImports) {
        coreObj.info(`Processing repository import: ${repoImport}`);

        const parsed = parseAgentImportSpec(repoImport);
        if (!parsed) {
          coreObj.warning(`Skipping invalid repository import: ${repoImport}`);
          continue;
        }

        const { owner, repo, ref } = parsed;
        await mergeRepositoryGithubFolder(owner, repo, ref, workspace);
      }

      coreObj.info("All repository imports processed successfully");
      return;
    }

    // Legacy path: Handle agent file imports (for backward compatibility)
    // Get agent file path from environment
    const agentFile = process.env.GH_AW_AGENT_FILE;
    if (!agentFile) {
      coreObj.info("No GH_AW_AGENT_FILE or GH_AW_REPOSITORY_IMPORTS specified, skipping .github folder merge");
      return;
    }

    coreObj.info(`Agent file: ${agentFile}`);

    // Get agent import specification
    const importSpec = process.env.GH_AW_AGENT_IMPORT_SPEC;
    if (!importSpec) {
      coreObj.info("No GH_AW_AGENT_IMPORT_SPEC specified, assuming local agent");
      return;
    }

    coreObj.info(`Agent import spec: ${importSpec}`);

    // Parse import specification
    const parsed = parseAgentImportSpec(importSpec);
    if (!parsed) {
      coreObj.info("Agent is local or import spec is invalid, skipping remote merge");
      return;
    }

    const { owner, repo, ref } = parsed;
    coreObj.info(`Remote agent detected: ${owner}/${repo}@${ref}`);

    // Get workspace path
    const workspace = process.env.GITHUB_WORKSPACE;
    if (!workspace) {
      throw new Error("GITHUB_WORKSPACE environment variable not set");
    }

    await mergeRepositoryGithubFolder(owner, repo, ref, workspace);

    coreObj.info("Remote .github folder merge completed successfully");
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    coreObj.setFailed(`Failed to merge remote .github folder: ${errorMessage}`);
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
  mergeRepositoryGithubFolder,
  main,
};
