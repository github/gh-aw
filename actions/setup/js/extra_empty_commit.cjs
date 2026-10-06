// @ts-check
/// <reference types="@actions/github-script" />

const { validateTargetRepo, parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { overridePersistedExtraheader, restorePersistedExtraheader } = require("./git_auth_helpers.cjs");

/**
 * @fileoverview Extra Empty Commit Helper
 *
 * Pushes an empty commit to a branch using a different token to trigger CI events.
 * This works around the GitHub Actions limitation where events created with
 * GITHUB_TOKEN do not trigger other workflow runs.
 *
 * The token comes from `github-token-for-extra-empty-commit` in safe-outputs config
 * and is passed in as the GH_AW_CI_TRIGGER_TOKEN environment variable.
 * By the time this script runs, GH_AW_CI_TRIGGER_TOKEN must contain an actual
 * GitHub authentication token (for example, a GitHub App token or a PAT).
 * Any selection or defaulting behavior (such as resolving `app`, `default`,
 * or a specific secret reference) is handled in the workflow compiler/config
 * layer before this script is invoked.
 */

/**
 * Check whether a target repository is a cross-repo target (different from the
 * workflow's own repository). Comparison is case-insensitive.
 *
 * @param {string} repoOwner - Repository owner
 * @param {string} repoName - Repository name
 * @returns {boolean} true when the target repo differs from GITHUB_REPOSITORY
 */
function isCrossRepoTarget(repoOwner, repoName) {
  const githubRepository = process.env.GITHUB_REPOSITORY || "";
  if (!githubRepository) {
    return false;
  }
  const targetRepo = `${repoOwner}/${repoName}`;
  return targetRepo.toLowerCase() !== githubRepository.toLowerCase();
}

/**
 * Push an empty commit to a branch using a dedicated token.
 * This commit is pushed with different authentication so that push/PR events
 * are triggered for CI checks to run.
 *
 * @param {Object} options - Options for the extra empty commit
 * @param {string} options.branchName - The branch to push the empty commit to
 * @param {string} options.repoOwner - Repository owner
 * @param {string} options.repoName - Repository name
 * @param {string} [options.commitMessage] - Custom commit message (default: "ci: trigger checks")
 * @param {number} [options.newCommitCount] - Number of new commits being pushed. Only pushes the
 *   empty commit when exactly 1 new commit was pushed, preventing accidental workflow-file
 *   modifications on multi-commit branches and reducing loop risk.
 * @param {string[]|string} [options.allowedRepos] - Allowed repository patterns for allowlist validation
 * @returns {Promise<{success: boolean, skipped?: boolean, error?: string}>}
 */
async function pushExtraEmptyCommit({ branchName, repoOwner, repoName, commitMessage, newCommitCount, allowedRepos: allowedReposInput }) {
  const token = process.env.GH_AW_CI_TRIGGER_TOKEN;

  if (!token || !token.trim()) {
    core.info("No extra empty commit token configured - skipping");
    return { success: true, skipped: true };
  }

  // Validate target repository against allowlist before any git operations
  const allowedRepos = parseAllowedRepos(allowedReposInput);
  if (allowedRepos.size > 0) {
    const targetRepo = `${repoOwner}/${repoName}`;
    const defaultRepo = getDefaultTargetRepo();
    const validation = validateTargetRepo(targetRepo, defaultRepo, allowedRepos);
    if (!validation.valid) {
      core.warning(`ERR_VALIDATION: ${validation.error}`);
      return { success: false, error: validation.error ?? "" };
    }
  }

  // Cross-repo guard: never push an extra empty commit to a different repository.
  // A token is needed to create the PR and that will trigger events anyway.
  if (isCrossRepoTarget(repoOwner, repoName)) {
    core.info(`Skipping extra empty commit: cross-repo target ${repoOwner}/${repoName} differs from workflow repo ${process.env.GITHUB_REPOSITORY}`);
    return { success: true, skipped: true };
  }

  if (newCommitCount !== undefined && newCommitCount !== 1) {
    core.info(`Skipping extra empty commit: ${newCommitCount} new commit(s) pushed (only triggers for exactly 1 commit)`);
    return { success: true, skipped: true };
  }

  core.info("Extra empty commit token detected - pushing empty commit to trigger CI events");

  try {
    // Cycle prevention: count empty commits in the last 60 commits on this branch.
    // If 30 or more are empty, skip pushing to avoid infinite trigger loops.
    const MAX_EMPTY_COMMITS = 30;
    const COMMITS_TO_CHECK = 60;
    let emptyCommitCount = 0;
    let mergeCommitCount = 0;
    let analyzedCommitCount = 0;
    core.info(`Cycle check: analyzing up to ${COMMITS_TO_CHECK} recent commits on ${branchName}`);

    try {
      let logOutput = "";
      // List last N commits: for each, output "COMMIT:<hash> <parents>" then changed file names.
      // Empty commits will have no files listed after the hash line.
      await exec.exec("git", ["log", `--max-count=${COMMITS_TO_CHECK}`, "--format=COMMIT:%H %P", "--name-only", "HEAD"], {
        listeners: {
          stdout: data => {
            logOutput += data.toString();
          },
        },
        silent: true,
      });
      // Split by COMMIT: markers; each chunk starts with the hash, followed by filenames
      const chunks = logOutput.split("COMMIT:").filter(c => c.trim());
      for (const chunk of chunks) {
        analyzedCommitCount++;
        const lines = chunk.split("\n").filter(l => l.trim());
        // First line is hash + parent SHAs, remaining lines are changed files.
        // Ignore merge commits (2+ parents) so they aren't mistaken for CI-trigger empty commits.
        // git log format is "COMMIT:<hash> <parent1> <parent2>..."
        const hashAndParents = (lines[0] || "").trim().split(" ").filter(Boolean);
        const parentCount = Math.max(0, hashAndParents.length - 1);
        if (parentCount >= 2) {
          mergeCommitCount++;
          continue;
        }

        // Non-merge commit with no changed files -> empty commit
        if (lines.length <= 1) {
          emptyCommitCount++;
        }
      }
    } catch {
      // If we can't check, default to allowing the push
      emptyCommitCount = 0;
      core.warning(`Cycle check unavailable: failed to inspect git history for ${branchName}. Continuing with empty commit count set to 0.`);
    }

    if (emptyCommitCount >= MAX_EMPTY_COMMITS) {
      core.warning(`Cycle prevention: found ${emptyCommitCount} empty commits in the last ${COMMITS_TO_CHECK} commits on ${branchName}. ` + `Skipping extra empty commit to avoid potential infinite loop.`);
      return { success: true, skipped: true };
    }

    core.info(`Cycle check details: analyzed ${analyzedCommitCount} commit(s), ignored ${mergeCommitCount} merge commit(s), counted ${emptyCommitCount} empty non-merge commit(s)`);
    core.info(`Cycle check passed: ${emptyCommitCount} empty commit(s) in last ${COMMITS_TO_CHECK} (limit: ${MAX_EMPTY_COMMITS})`);

    // Configure git remote with no embedded credentials; authenticate via
    // a single replaced extraheader value to avoid duplicate Authorization headers.
    const githubServerUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
    const remoteUrl = `${githubServerUrl}/${repoOwner}/${repoName}.git`;

    // Declare previousExtraheaders before the try block so the finally clause can
    // always restore when the override was applied, even if an error is thrown.
    // overrideApplied is only set to true after overridePersistedExtraheader
    // completes without throwing, so the finally clause skips restoration when
    // the override was never applied (avoiding a spurious --unset-all that would
    // destroy existing checkout credentials).
    let previousExtraheaders = [];
    let overrideApplied = false;
    try {
      core.info(`Overriding git extraheader for CI trigger push to ${repoOwner}/${repoName} on branch ${branchName}`);
      previousExtraheaders = await overridePersistedExtraheader(githubServerUrl, token);
      overrideApplied = true;

      // Fetch and sync with the remote branch using the URL directly.
      // This is required when the PR branch was created server-side via the GitHub
      // API (e.g. via the createCommitOnBranch GraphQL mutation used by
      // pushSignedCommits), because the remote branch tip then has a different SHA
      // than the local branch tip. Without this sync, git would reject the
      // subsequent push as non-fast-forward.
      // Using the URL directly here avoids adding the named ci-trigger remote
      // before we know whether a git push will actually be needed.
      try {
        core.info(`Fetching and syncing with remote branch ${branchName}`);
        await exec.exec("git", ["fetch", remoteUrl, branchName]);
        await exec.exec("git", ["reset", "--hard", "FETCH_HEAD"]);
        core.info(`Synced local branch with remote ${branchName}`);
      } catch (error) {
        // Non-fatal: if fetch/reset fails (e.g. branch not yet on remote), continue
        // with the local HEAD and attempt the push anyway.
        const syncErrorMessage = getErrorMessage(error);
        core.warning(`Could not sync local branch with remote ${branchName} - will attempt push with local HEAD. Underlying error: ${syncErrorMessage}`);
      }

      // Create and push an empty commit.
      // Try the GitHub API (createCommitOnBranch GraphQL mutation) first, which
      // produces verified/signed commits required for branches with "Require signed
      // commits" branch protection.  Fall back to git commit + push when the API
      // path is unavailable or the OID cannot be resolved.
      const message = commitMessage || "ci: trigger checks";

      // Resolve the current HEAD OID (after the sync above) for the GraphQL
      // expectedHeadOid parameter.  If this fails, skip the API path entirely.
      let expectedHeadOid = "";
      try {
        const headResult = await exec.getExecOutput("git", ["rev-parse", "HEAD"], { silent: true, ignoreReturnCode: true });
        if (headResult.exitCode === 0) {
          expectedHeadOid = headResult.stdout.trim();
          core.info(`HEAD OID for GraphQL mutation: ${expectedHeadOid}`);
        }
      } catch (headErr) {
        core.info(`Could not resolve HEAD OID for GraphQL path: ${getErrorMessage(headErr)}`);
      }

      let committedViaApi = false;
      if (expectedHeadOid && typeof global.getOctokit === "function") {
        try {
          core.info(`Attempting to create verified empty commit via GitHub API (createCommitOnBranch)`);
          const ciGithubClient = global.getOctokit(token.trim());
          const apiResult = await ciGithubClient.graphql(
            `mutation($input: CreateCommitOnBranchInput!) {
              createCommitOnBranch(input: $input) { commit { oid } }
            }`,
            {
              input: {
                branch: { repositoryNameWithOwner: `${repoOwner}/${repoName}`, branchName },
                message: { headline: message },
                fileChanges: {},
                expectedHeadOid,
              },
            }
          );
          const newOid = apiResult?.createCommitOnBranch?.commit?.oid;
          if (!newOid) {
            throw new Error("createCommitOnBranch did not return a commit OID");
          }
          core.info(`Verified empty commit created via GitHub API (oid=${newOid})`);
          // Update the local branch HEAD to the API-created commit so that any
          // downstream git state reads see the new OID rather than stale data.
          try {
            await exec.exec("git", ["fetch", remoteUrl, branchName]);
            await exec.exec("git", ["reset", "--hard", "FETCH_HEAD"]);
            core.info(`Updated local branch to API-created commit ${newOid}`);
          } catch (updateErr) {
            core.info(`Could not update local branch to API-created commit: ${getErrorMessage(updateErr)}`);
          }
          committedViaApi = true;
        } catch (apiError) {
          core.info(`GitHub API commit creation unavailable, falling back to git commit + push: ${getErrorMessage(apiError)}`);
        }
      }

      if (!committedViaApi) {
        // Add a named remote only when a git push is actually needed.
        core.info(`Setting up temporary ci-trigger remote: ${remoteUrl}`);
        try {
          await exec.exec("git", ["remote", "remove", "ci-trigger"]);
          core.info("Removed pre-existing ci-trigger remote");
        } catch {
          // Remote doesn't exist yet — removal failure is ignored, that's fine.
        }
        await exec.exec("git", ["remote", "add", "ci-trigger", remoteUrl]);
        await exec.exec("git", ["commit", "--allow-empty", "-m", message]);
        await require("./work_queue_git_effects.cjs").assertGitPushAuthorized({ remote: "ci-trigger", branch: branchName });
        await exec.exec("git", ["push", "ci-trigger", branchName]);
      }

      core.info(`Extra empty commit pushed to ${branchName} successfully`);

      return { success: true };
    } finally {
      // Clean up the temporary remote and restore previous checkout auth state.
      try {
        await exec.exec("git", ["remote", "remove", "ci-trigger"]);
        core.info("Removed ci-trigger remote");
      } catch {
        // Non-fatal cleanup error
      }

      try {
        core.info("Restoring previous git auth configuration");
        if (overrideApplied) {
          await restorePersistedExtraheader(githubServerUrl, previousExtraheaders);
        }
      } catch (restoreError) {
        core.warning(`Failed to restore git auth configuration after CI trigger push: ${getErrorMessage(restoreError)}`);
      }
    }
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.warning(`Failed to push extra empty commit: ${errorMessage}`);

    // Extra empty commit failure is not fatal - the main push already succeeded
    return { success: false, error: errorMessage };
  }
}

module.exports = { pushExtraEmptyCommit, isCrossRepoTarget };
