// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check workflow file timestamps using GitHub API to detect outdated lock files
 * This script compares the last commit time of the source .md file
 * with the compiled .lock.yml file and warns if recompilation is needed
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { extractHashFromLockFile, computeFrontmatterHash, createGitHubFileReader } = require("./frontmatter_hash_pure.cjs");
const { getFileContent } = require("./github_api_helpers.cjs");

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
  // Returns: { match: boolean, storedHash: string, recomputedHash: string } or null on error
  async function compareFrontmatterHashes() {
    try {
      // Fetch lock file content to extract stored hash
      const lockFileContent = await getFileContent(github, owner, repo, lockFilePath, ref);
      if (!lockFileContent) {
        core.info("Unable to fetch lock file content for hash comparison");
        return null;
      }

      const storedHash = extractHashFromLockFile(lockFileContent);
      if (!storedHash) {
        core.info("No frontmatter hash found in lock file");
        return null;
      }

      // Compute hash using pure JavaScript implementation
      // Create a GitHub file reader for fetching workflow files via API
      const fileReader = createGitHubFileReader(github, owner, repo, ref);
      const recomputedHash = await computeFrontmatterHash(workflowMdPath, { fileReader });

      const match = storedHash === recomputedHash;

      // Log hash comparison
      core.info(`Frontmatter hash comparison:`);
      core.info(`  Lock file hash:    ${storedHash}`);
      core.info(`  Recomputed hash:   ${recomputedHash}`);
      core.info(`  Status: ${match ? "✅ Hashes match" : "⚠️  Hashes differ"}`);

      return { match, storedHash, recomputedHash };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.info(`Could not compute frontmatter hash: ${errorMessage}`);
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

  const workflowTime = workflowDate.getTime();
  const lockTime = lockDate.getTime();

  // Check if workflow file is newer than lock file
  if (workflowTime > lockTime) {
    // Workflow file is newer - check frontmatter hash to determine if recompilation needed
    core.info("Workflow file is newer - checking frontmatter hash");
    const hashComparison = await compareFrontmatterHashes();

    if (!hashComparison) {
      // Could not compute hash - be conservative and fail
      core.warning("Could not compare frontmatter hashes - assuming lock file is outdated");
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
    } else if (hashComparison.match) {
      // Hashes match - lock file is up to date despite timestamp difference
      core.info("✅ Lock file is up to date (frontmatter hashes match despite timestamp difference)");
    } else {
      // Hashes differ - lock file needs recompilation
      const warningMessage = `Lock file '${lockFilePath}' is outdated! The workflow file '${workflowMdPath}' frontmatter has changed. Run 'gh aw compile' to regenerate the lock file.`;

      // Format timestamps and commits for display
      const workflowTimestamp = workflowDate.toISOString();
      const lockTimestamp = lockDate.toISOString();

      // Add summary to GitHub Step Summary
      let summary = core.summary
        .addRaw("### ⚠️ Workflow Lock File Warning\n\n")
        .addRaw("**WARNING**: Lock file is outdated (frontmatter hash mismatch).\n\n")
        .addRaw("**Files:**\n")
        .addRaw(`- Source: \`${workflowMdPath}\`\n`)
        .addRaw(`  - Last commit: ${workflowTimestamp}\n`)
        .addRaw(`  - Commit SHA: [\`${workflowCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${workflowCommit.sha})\n`)
        .addRaw(`  - Frontmatter hash: \`${hashComparison.recomputedHash.substring(0, 12)}...\`\n`)
        .addRaw(`- Lock: \`${lockFilePath}\`\n`)
        .addRaw(`  - Last commit: ${lockTimestamp}\n`)
        .addRaw(`  - Commit SHA: [\`${lockCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${lockCommit.sha})\n`)
        .addRaw(`  - Stored hash: \`${hashComparison.storedHash.substring(0, 12)}...\`\n\n`)
        .addRaw("**Action Required:** Run `gh aw compile` to regenerate the lock file.\n\n");

      await summary.write();

      // Fail the step to prevent workflow from running with outdated configuration
      core.setFailed(warningMessage);
    }
  } else if (workflowCommit.sha === lockCommit.sha) {
    // Same commit - definitely up to date
    core.info("✅ Lock file is up to date (same commit)");
  } else if (workflowTime === lockTime) {
    // Timestamps are equal (coarse timestamp) but different commits
    // Use frontmatter hash comparison to determine if recompilation is needed
    core.info("Timestamps are equal - using frontmatter hash comparison");
    const hashComparison = await compareFrontmatterHashes();

    if (!hashComparison) {
      // Could not compute hash - be conservative and assume it's ok
      core.info("⚠️  Could not compare frontmatter hashes - assuming lock file is up to date");
      core.info("✅ Lock file is up to date (timestamp check passed, hash comparison unavailable)");
    } else if (hashComparison.match) {
      // Hashes match - lock file is up to date
      core.info("✅ Lock file is up to date (hashes match)");
    } else {
      // Hashes differ - lock file needs recompilation
      const warningMessage = `Lock file '${lockFilePath}' is outdated! The workflow file '${workflowMdPath}' frontmatter has changed. Run 'gh aw compile' to regenerate the lock file.`;

      // Format timestamps and commits for display
      const workflowTimestamp = workflowDate.toISOString();
      const lockTimestamp = lockDate.toISOString();

      // Add summary to GitHub Step Summary
      let summary = core.summary
        .addRaw("### ⚠️ Workflow Lock File Warning\n\n")
        .addRaw("**WARNING**: Lock file is outdated (frontmatter hash mismatch).\n\n")
        .addRaw("**Files:**\n")
        .addRaw(`- Source: \`${workflowMdPath}\`\n`)
        .addRaw(`  - Last commit: ${workflowTimestamp}\n`)
        .addRaw(`  - Commit SHA: [\`${workflowCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${workflowCommit.sha})\n`)
        .addRaw(`  - Frontmatter hash: \`${hashComparison.recomputedHash.substring(0, 12)}...\`\n`)
        .addRaw(`- Lock: \`${lockFilePath}\`\n`)
        .addRaw(`  - Last commit: ${lockTimestamp}\n`)
        .addRaw(`  - Commit SHA: [\`${lockCommit.sha.substring(0, 7)}\`](https://github.com/${owner}/${repo}/commit/${lockCommit.sha})\n`)
        .addRaw(`  - Stored hash: \`${hashComparison.storedHash.substring(0, 12)}...\`\n\n`)
        .addRaw("**Action Required:** Run `gh aw compile` to regenerate the lock file.\n\n");

      await summary.write();

      // Fail the step to prevent workflow from running with outdated configuration
      core.setFailed(warningMessage);
    }
  } else {
    // Lock file is newer than workflow file
    // This means the lock was recompiled after the .md file, so it's up to date
    // We verify the hash for informational purposes but don't fail
    core.info("Lock file is newer - verifying frontmatter hash for consistency");
    const hashComparison = await compareFrontmatterHashes();

    if (!hashComparison) {
      // Could not compute hash
      core.info("⚠️  Could not compare frontmatter hashes");
      core.info("✅ Lock file is up to date (lock is newer than source)");
    } else if (hashComparison.match) {
      // Hashes match - perfect consistency
      core.info("✅ Lock file is up to date (lock is newer and hashes match)");
    } else {
      // Hashes differ but lock is newer, so it's still considered up to date
      // The .md file may have been edited after the lock was compiled
      core.info("⚠️  Frontmatter hash mismatch detected, but lock file is newer than source");
      core.info("✅ Lock file is up to date (lock was recompiled after source changes)");
    }
  }
}

module.exports = { main };
