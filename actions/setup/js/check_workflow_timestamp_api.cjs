// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check workflow file timestamps using GitHub API to detect outdated lock files
 * This script compares the last commit time of the source .md file
 * with the compiled .lock.yml file and warns if recompilation is needed
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { computeFrontmatterHash, extractHashFromLockFile } = require("./frontmatter_hash.cjs");
const fs = require("fs");
const path = require("path");

async function main() {
  const workflowFile = process.env.GH_AW_WORKFLOW_FILE;

  if (!workflowFile) {
    core.setFailed("Configuration error: GH_AW_WORKFLOW_FILE not available.");
    return;
  }

  // Construct file paths
  const workflowBasename = workflowFile.replace(".lock.yml", "");
  const workflowMdPath = `.github/workflows/${workflowBasename}.md`;
  const lockFilePath = `.github/workflows/${workflowFile}`;

  core.info(`Checking workflow timestamps using GitHub API:`);
  core.info(`  Source: ${workflowMdPath}`);
  core.info(`  Lock file: ${lockFilePath}`);

  const { owner, repo } = context.repo;
  const ref = context.sha;

  // Helper function to get the last commit for a file
  async function getLastCommitForFile(path) {
    try {
      const response = await github.rest.repos.listCommits({
        owner,
        repo,
        path,
        per_page: 1,
        sha: ref,
      });

      if (response.data && response.data.length > 0) {
        const commit = response.data[0];
        const committerDate = commit.commit.committer?.date;
        if (!committerDate) {
          return null;
        }
        return {
          sha: commit.sha,
          date: committerDate,
          message: commit.commit.message,
        };
      }
      return null;
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`Could not fetch commit for ${path}: ${errorMessage}`);
      return null;
    }
  }

  // Fetch last commits for both files
  const workflowCommit = await getLastCommitForFile(workflowMdPath);
  const lockCommit = await getLastCommitForFile(lockFilePath);

  // Handle cases where files don't exist
  if (!workflowCommit) {
    core.info(`Source file does not exist: ${workflowMdPath}`);
  }

  if (!lockCommit) {
    core.info(`Lock file does not exist: ${lockFilePath}`);
  }

  if (!workflowCommit || !lockCommit) {
    core.info("Skipping timestamp check - one or both files not found");
    return;
  }

  // Parse dates for comparison
  const workflowDate = new Date(workflowCommit.date);
  const lockDate = new Date(lockCommit.date);

  core.info(`  Source last commit: ${workflowDate.toISOString()} (${workflowCommit.sha.substring(0, 7)})`);
  core.info(`  Lock last commit: ${lockDate.toISOString()} (${lockCommit.sha.substring(0, 7)})`);

  // Display frontmatter hashes
  await displayFrontmatterHashes(workflowMdPath, lockFilePath);

  // Check if workflow file is newer than lock file
  if (workflowDate > lockDate) {
    const warningMessage = `Lock file '${lockFilePath}' is outdated! The workflow file '${workflowMdPath}' has been modified more recently. Run 'gh aw compile' to regenerate the lock file.`;

    // Format timestamps and commits for display
    const workflowTimestamp = workflowDate.toISOString();
    const lockTimestamp = lockDate.toISOString();

    // Add summary to GitHub Step Summary
    let summary = core.summary
      .addRaw("### ⚠️ Workflow Lock File Warning\n\n")
      .addRaw("**WARNING**: Lock file is outdated and needs to be regenerated.\n\n")
      .addRaw("**Files:**\n")
      .addRaw(`- Source: \`${workflowMdPath}\`\n`)
      .addRaw(`  - Last commit: ${workflowTimestamp}\n`)
      .addRaw(`  - Commit SHA: [\`${workflowCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${workflowCommit.sha})\n`)
      .addRaw(`- Lock: \`${lockFilePath}\`\n`)
      .addRaw(`  - Last commit: ${lockTimestamp}\n`)
      .addRaw(`  - Commit SHA: [\`${lockCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${lockCommit.sha})\n\n`)
      .addRaw("**Action Required:** Run `gh aw compile` to regenerate the lock file.\n\n");

    await summary.write();

    // Fail the step to prevent workflow from running with outdated configuration
    core.setFailed(warningMessage);
  } else if (workflowCommit.sha === lockCommit.sha) {
    core.info("✅ Lock file is up to date (same commit)");
  } else {
    core.info("✅ Lock file is up to date");
  }
}

/**
 * Display frontmatter hashes from lock file and recomputed from source
 * @param {string} workflowMdPath - Path to the source .md file
 * @param {string} lockFilePath - Path to the compiled .lock.yml file
 */
async function displayFrontmatterHashes(workflowMdPath, lockFilePath) {
  try {
    core.info("Checking frontmatter hashes:");

    // Extract hash from lock file
    let lockFileHash = "";
    try {
      const lockContent = fs.readFileSync(lockFilePath, "utf8");
      lockFileHash = extractHashFromLockFile(lockContent);
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`  Could not read lock file: ${errorMessage}`);
      return;
    }

    // Recompute hash from .md file
    let recomputedHash = "";
    try {
      // Get the absolute path to the workflow file
      const workspacePath = process.env.GITHUB_WORKSPACE || process.cwd();
      const absoluteMdPath = path.join(workspacePath, workflowMdPath);

      if (fs.existsSync(absoluteMdPath)) {
        recomputedHash = await computeFrontmatterHash(absoluteMdPath);
      } else {
        core.info(`  Source file not found: ${workflowMdPath}`);
        return;
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`  Could not compute hash from source: ${errorMessage}`);
      return;
    }

    // Display both hashes
    core.info(`  Lock file hash:  ${lockFileHash || "(not found)"}`);
    core.info(`  Recomputed hash: ${recomputedHash}`);

    // Check if hashes match
    if (lockFileHash && recomputedHash) {
      if (lockFileHash === recomputedHash) {
        core.info("  ✅ Frontmatter hashes match");
      } else {
        core.warning(`  ⚠️  Frontmatter hashes DO NOT match - frontmatter may have changed`);
      }
    }
  } catch (error) {
    // Don't fail the step if hash checking fails, just log it
    const errorMessage = getErrorMessage(error);
    core.info(`Could not check frontmatter hashes: ${errorMessage}`);
  }
}

module.exports = { main };
