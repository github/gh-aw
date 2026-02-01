// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check workflow file timestamps using GitHub API to detect outdated lock files
 * This script compares the last commit time of the source .md file
 * with the compiled .lock.yml file and warns if recompilation is needed
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { extractHashFromLockFile } = require("./frontmatter_hash_pure.cjs");
const { getFileContent } = require("./github_api_helpers.cjs");

/**
 * Compute frontmatter hash using the Go binary (gh aw hash-frontmatter)
 * This ensures consistency between compilation and validation
 * @param {string} workflowPath - Path to the workflow file
 * @returns {Promise<string>} The SHA-256 hash as a lowercase hexadecimal string
 */
async function computeFrontmatterHashViaGo(workflowPath) {
  try {
    let hashOutput = "";
    let errorOutput = "";

    const exitCode = await exec.exec("gh", ["aw", "hash-frontmatter", workflowPath], {
      silent: true,
      ignoreReturnCode: true,
      listeners: {
        stdout: data => {
          hashOutput += data.toString();
        },
        stderr: data => {
          errorOutput += data.toString();
        },
      },
    });

    if (exitCode !== 0) {
      throw new Error(`gh aw hash-frontmatter failed with exit code ${exitCode}: ${errorOutput}`);
    }

    // Extract the hash from output (remove any ANSI codes and whitespace)
    const hash = hashOutput.replace(/\x1b\[[0-9;]*m/g, "").trim();

    // Validate hash format (should be 64 hex characters)
    if (!/^[a-f0-9]{64}$/.test(hash)) {
      throw new Error(`Invalid hash format received: ${hash}`);
    }

    return hash;
  } catch (error) {
    throw new Error(`Failed to compute hash via Go binary: ${getErrorMessage(error)}`);
  }
}

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

  // Helper function to compute and compare frontmatter hashes
  async function logFrontmatterHashComparison() {
    try {
      // Fetch lock file content to extract stored hash
      const lockFileContent = await getFileContent(github, owner, repo, lockFilePath, ref);
      if (!lockFileContent) {
        core.info("Unable to fetch lock file content for hash comparison");
        return;
      }

      const storedHash = extractHashFromLockFile(lockFileContent);
      if (!storedHash) {
        core.info("No frontmatter hash found in lock file");
        return;
      }

      // Compute hash using Go binary for consistency with compilation
      // Note: This requires the workflow file to exist locally in the checkout
      const recomputedHash = await computeFrontmatterHashViaGo(workflowMdPath);

      // Log hash comparison
      core.info(`Frontmatter hash comparison:`);
      core.info(`  Lock file hash:    ${storedHash}`);
      core.info(`  Recomputed hash:   ${recomputedHash}`);

      if (storedHash === recomputedHash) {
        core.info(`  Status: ✅ Hashes match`);
      } else {
        core.info(`  Status: ⚠️  Hashes differ`);
      }
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`Could not compute frontmatter hash: ${errorMessage}`);
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

  // Log frontmatter hash comparison
  await logFrontmatterHashComparison();
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

module.exports = { main };
