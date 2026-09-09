// @ts-check
/// <reference types="@actions/github-script" />

// SEC-005: This module generates git bundles via git CLI commands and does not make
// GitHub API calls using a user-supplied target repository. The "target repo" references
// in documentation describe cross-repo checkout scenarios only; no validateTargetRepo
// allowlist check is required in this handler.

const fs = require("fs");
const os = require("os");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { generateGitPatch } = require("./generate_git_patch.cjs");
const { ensureOriginRemoteTrackingRef, execGitSync } = require("./git_helpers.cjs");
const { isAncestorCommit, describeGitFailure } = require("./git_patch_utils.cjs");
const { normalizeCommitSHA } = require("./commit_sha_helpers.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Debug logging helper - logs to stderr when DEBUG env var matches
 * @param {string} message - Debug message to log
 */
function debugLog(message) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("generate_git_bundle") || debug.includes("bundle")) {
    console.error(`[generate_git_bundle] ${message}`);
  }
}

/**
 * Sanitize a string for use as a bundle filename component.
 * Replaces path separators and special characters with dashes.
 * @param {string} value - The value to sanitize
 * @param {string} fallback - Fallback value when input is empty or nullish
 * @returns {string} The sanitized string safe for use in a filename
 */
function sanitizeForFilename(value, fallback) {
  if (!value) return fallback;
  return value
    .replace(/[/\\:*?"<>|]/g, "-")
    .replace(/-{2,}/g, "-")
    .replace(/^-|-$/g, "")
    .toLowerCase();
}

/**
 * Sanitize a branch name for use as a bundle filename
 * @param {string} branchName - The branch name to sanitize
 * @returns {string} The sanitized branch name safe for use in a filename
 */
function sanitizeBranchNameForBundle(branchName) {
  return sanitizeForFilename(branchName, "unknown");
}

/**
 * Get the bundle file path for a given branch name
 * @param {string} branchName - The branch name
 * @returns {string} The full bundle file path
 */
function getBundlePathForBranch(branchName) {
  const sanitized = sanitizeBranchNameForBundle(branchName);
  return `/tmp/gh-aw/aw-${sanitized}.bundle`;
}

/**
 * Sanitize a repo slug for use in a filename
 * @param {string} repoSlug - The repo slug (owner/repo)
 * @returns {string} The sanitized slug safe for use in a filename
 */
function sanitizeRepoSlugForBundle(repoSlug) {
  return sanitizeForFilename(repoSlug, "");
}

/**
 * Get the bundle file path for a given branch name and repo slug
 * Used for multi-repo scenarios to prevent bundle file collisions
 * @param {string} branchName - The branch name
 * @param {string} repoSlug - The repository slug (owner/repo)
 * @returns {string} The full bundle file path including repo disambiguation
 */
function getBundlePathForBranchInRepo(branchName, repoSlug) {
  const sanitizedBranch = sanitizeBranchNameForBundle(branchName);
  const sanitizedRepo = sanitizeRepoSlugForBundle(repoSlug);
  return `/tmp/gh-aw/aw-${sanitizedRepo}-${sanitizedBranch}.bundle`;
}

/**
 * Generates a git bundle file for the current changes.
 * Bundle transport preserves merge commit topology and per-commit metadata,
 * unlike format-patch which loses merge resolution content.
 *
 * @param {string} branchName - The branch name to generate bundle for
 * @param {string} baseBranch - The base branch to diff against (e.g., "main", "master")
 * @param {Object} [options] - Optional parameters
 * @param {string} [options.mode="full"] - Bundle generation mode:
 *   - "full": Include all commits since merge-base with default branch (for create_pull_request)
 *   - "incremental": Only include commits since origin/branchName (for push_to_pull_request_branch)
 *     In incremental mode, origin/branchName is fetched explicitly and merge-base fallback is disabled.
 * @param {string} [options.cwd] - Working directory for git commands. Defaults to GITHUB_WORKSPACE or process.cwd().
 *   Use this for multi-repo scenarios where repos are checked out to subdirectories.
 * @param {string} [options.repoSlug] - Repository slug (owner/repo) to include in bundle filename for disambiguation.
 *   Required for multi-repo scenarios to prevent bundle file collisions.
 * @param {string} [options.token] - GitHub token for git authentication. Falls back to GITHUB_TOKEN env var.
 *   Use this for cross-repo scenarios where a custom PAT with access to the target repo is needed.
 * @param {string[]} [options.excludedFiles] - Glob patterns for files to exclude from the pushed commit set.
 *   When set, the bundle is synthesized from the same filtered patch used for validation so the pushed files
 *   match the validated patch file set.
 * @param {string} [options.incrementalBaseRef] - Explicit local ref to use as the pre-agent
 *   PR head in incremental mode (for example refs/remotes/origin/pr-head for fork PR checkouts).
 * @param {string} [options.incrementalBaseSha] - Explicit commit SHA to use as the pre-agent
 *   PR head in incremental mode. Takes precedence over incrementalBaseRef when present.
 * @returns {Promise<Object>} Object with bundle info or error
 */
async function generateGitBundle(branchName, baseBranch, options = {}) {
  const mode = options.mode || "full";
  // Support custom cwd for multi-repo scenarios
  const cwd = options.cwd || process.env.GITHUB_WORKSPACE || process.cwd();

  const bundlePath = options.repoSlug ? getBundlePathForBranchInRepo(branchName, options.repoSlug) : getBundlePathForBranch(branchName);

  // Validate baseBranch early to avoid confusing git errors (e.g., origin/undefined)
  if (typeof baseBranch !== "string" || baseBranch.trim() === "") {
    const errorMessage = "baseBranch is required and must be a non-empty string (received: " + String(baseBranch) + ")";
    debugLog(`Invalid baseBranch: ${errorMessage}`);
    return {
      success: false,
      error: errorMessage,
      bundlePath,
    };
  }

  const defaultBranch = baseBranch;
  const githubSha = process.env.GITHUB_SHA;

  debugLog(`Starting bundle generation: mode=${mode}, branch=${branchName}, defaultBranch=${defaultBranch}`);
  debugLog(`Environment: cwd=${cwd}, GITHUB_SHA=${githubSha || "(not set)"}`);

  // Ensure /tmp/gh-aw directory exists
  const bundleDir = path.dirname(bundlePath);
  if (!fs.existsSync(bundleDir)) {
    try {
      fs.mkdirSync(bundleDir, { recursive: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to create directory ${bundleDir}: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  let bundleGenerated = false;
  /** @type {any} */
  let errorMessage = null;
  /** @type {any} */
  let baseCommitSha = null;

  try {
    // Strategy 1: If we have a branch name, check if that branch exists and create bundle
    if (branchName) {
      debugLog(`Strategy 1: Checking if branch '${branchName}' exists locally`);
      try {
        execGitSync(["show-ref", "--verify", "--quiet", `refs/heads/${branchName}`], { cwd });
        debugLog(`Strategy 1: Branch '${branchName}' exists locally`);

        // Determine base ref for bundle generation
        let baseRef;

        if (mode === "incremental") {
          // INCREMENTAL MODE (for push_to_pull_request_branch):
          // Only include commits that are new since the pre-agent PR head. Prefer an
          // explicit baseline captured by checkout_pr_branch.cjs (refs/pull/N/head
          // fetched to origin/pr-head), because fork PR heads are not branches in the
          // base repository's origin namespace and origin/<branchName> may refer to a
          // same-named base-repo branch.
          const explicitIncrementalBaseSha = normalizeCommitSHA(options.incrementalBaseSha);
          const explicitIncrementalBaseRef = typeof options.incrementalBaseRef === "string" ? options.incrementalBaseRef.trim() : "";
          if (explicitIncrementalBaseSha || explicitIncrementalBaseRef) {
            const candidateBaseRef = explicitIncrementalBaseSha || explicitIncrementalBaseRef;
            debugLog(`Strategy 1 (incremental): Using explicit PR-head baseline ${candidateBaseRef}`);
            try {
              baseRef = execGitSync(["rev-parse", "--verify", `${candidateBaseRef}^{commit}`], { cwd }).trim();
            } catch (baselineError) {
              errorMessage =
                `Cannot generate incremental bundle: explicit PR-head baseline ${candidateBaseRef} is not present in checkout '${cwd}'. ` +
                `Ensure the PR checkout step completed and fetched refs/pull/<number>/head before calling push_to_pull_request_branch.`;
              debugLog(`Strategy 1 (incremental): explicit PR-head baseline failed: ${getErrorMessage(baselineError)}`);
              return {
                success: false,
                error: errorMessage,
                bundlePath,
              };
            }
          } else {
            // Backward-compatible fallback for same-repository PRs and existing callers:
            // try origin/branchName locally, then a single network fetch attempt.
            debugLog(`Strategy 1 (incremental): Resolving origin/${branchName}`);
            const incrementalRefResult = ensureOriginRemoteTrackingRef(branchName, { cwd, token: options.token, suppressLogs: true });
            if (incrementalRefResult.exists) {
              baseRef = `origin/${branchName}`;
              if (incrementalRefResult.fetched) {
                debugLog(`Strategy 1 (incremental): Fetched origin/${branchName} from remote, baseRef=${baseRef}`);
              } else {
                debugLog(`Strategy 1 (incremental): Using existing remote tracking ref, baseRef=${baseRef}`);
              }
            } else {
              debugLog(`Strategy 1 (incremental): origin/${branchName} not present locally and remote fetch failed (${incrementalRefResult.fetchError ? getErrorMessage(incrementalRefResult.fetchError) : "no error"}), failing`);
              errorMessage =
                `Cannot generate incremental bundle: no pre-agent PR-head baseline was available for branch '${branchName}' in checkout '${cwd}'. ` +
                `Tried refs/remotes/origin/${branchName}, but it is not present and could not be fetched from origin. ` +
                `For fork PRs, ensure the workflow PR checkout step ran first so refs/pull/<number>/head is recorded as the bundle baseline; checkout.fetch cannot fetch fork branches from the base repository.`;
              return {
                success: false,
                error: errorMessage,
                bundlePath,
              };
            }
          }
        } else {
          // FULL MODE (for create_pull_request):
          // Include all commits since merge-base with default branch.
          debugLog(`Strategy 1 (full): Checking if origin/${branchName} exists`);
          try {
            execGitSync(["show-ref", "--verify", "--quiet", `refs/remotes/origin/${branchName}`], { cwd });
            baseRef = `origin/${branchName}`;
            debugLog(`Strategy 1 (full): Using existing origin/${branchName} as baseRef`);
          } catch {
            debugLog(`Strategy 1 (full): origin/${branchName} not found, trying merge-base with ${defaultBranch}`);
            const defaultBranchRefResult = ensureOriginRemoteTrackingRef(defaultBranch, { cwd, token: options.token, suppressLogs: true });
            const hasLocalDefaultBranch = defaultBranchRefResult.exists;
            if (hasLocalDefaultBranch) {
              if (defaultBranchRefResult.fetched) {
                debugLog(`Strategy 1 (full): fetched origin/${defaultBranch} from remote`);
              } else {
                debugLog(`Strategy 1 (full): origin/${defaultBranch} exists locally`);
              }
            } else {
              debugLog(
                `Strategy 1 (full): origin/${defaultBranch} not present locally and remote fetch failed (likely private repo without credentials in MCP server). Add ${JSON.stringify(defaultBranch)} to checkout.fetch to enable this strategy. Falling through to Strategy 2.`
              );
            }

            // When the workflow runs from a ref that is not contained in the default
            // branch (e.g. a workflow_dispatch on a feature branch), the merge-base with
            // the default branch is far behind the checked-out commit: the bundle would
            // then carry the dispatched branch's own commits instead of only the agent's,
            // and on a partial clone it needs base-side objects that were never fetched.
            // GITHUB_SHA is the commit the agent started from and is fully local.
            const dispatchedSha = normalizeCommitSHA(githubSha);
            const branchTipSha = execGitSync(["rev-parse", branchName], { cwd }).trim();
            if (hasLocalDefaultBranch && dispatchedSha && dispatchedSha !== branchTipSha && isAncestorCommit(dispatchedSha, branchName, cwd) && !isAncestorCommit(dispatchedSha, `origin/${defaultBranch}`, cwd)) {
              baseRef = dispatchedSha;
              debugLog(`Strategy 1 (full): GITHUB_SHA ${dispatchedSha} is not contained in origin/${defaultBranch} (non-default-branch run); using it as the bundle base instead of the merge-base`);
            } else if (hasLocalDefaultBranch) {
              baseRef = execGitSync(["merge-base", "--", `origin/${defaultBranch}`, branchName], { cwd }).trim();
              debugLog(`Strategy 1 (full): Computed merge-base: ${baseRef}`);
            } else {
              debugLog(`Strategy 1 (full): No remote refs available, falling through to Strategy 2`);
              throw new Error(`${ERR_SYSTEM}: No remote refs available for merge-base calculation`);
            }
          }
        }

        // Resolve baseRef to a SHA
        baseCommitSha = execGitSync(["rev-parse", baseRef], { cwd }).trim();
        debugLog(`Strategy 1: Resolved baseRef ${baseRef} to SHA ${baseCommitSha}`);

        // Count commits to be included
        const commitCount = parseInt(execGitSync(["rev-list", "--count", `${baseRef}..${branchName}`], { cwd }).trim(), 10);
        debugLog(`Strategy 1: Found ${commitCount} commits between ${baseRef} and ${branchName}`);

        if (commitCount > 0) {
          // Generate bundle from the determined base to the branch
          // git bundle create <file> <range> creates a bundle with the commit range.
          // In incremental mode, also exclude origin/<defaultBranch> when present so
          // a "merge base branch into PR branch" workflow does not re-embed upstream
          // commits that the remote already has.
          if (Array.isArray(options.excludedFiles) && options.excludedFiles.length > 0) {
            const patchResult = await generateGitPatch(branchName, baseBranch, options);
            if (!patchResult.success) {
              return {
                success: false,
                error: patchResult.error || "Failed to generate filtered patch for bundle synthesis",
                bundlePath,
              };
            }

            const tempWorktree = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-filtered-bundle-"));
            // Repository hooks (post-checkout, pre-applypatch, ...) must not run for these
            // internal synthesis operations: a repository configured for Git LFS (or any other
            // hook requiring tooling absent from the safe-outputs environment) would otherwise
            // fail `git worktree add` / `git am` and make a valid branch look unusable.
            const tempHooksDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-filtered-bundle-hooks-"));
            const noHooksArgs = ["-c", `core.hooksPath=${tempHooksDir}`];
            try {
              execGitSync([...noHooksArgs, "worktree", "add", "--detach", tempWorktree, baseCommitSha], { cwd });
              execGitSync([...noHooksArgs, "am", "--3way", patchResult.patchPath], { cwd: tempWorktree });
              execGitSync([...noHooksArgs, "bundle", "create", bundlePath, `${baseCommitSha}..HEAD`], { cwd: tempWorktree });
            } finally {
              try {
                execGitSync([...noHooksArgs, "worktree", "remove", "--force", tempWorktree], { cwd });
              } catch (removeError) {
                debugLog(`Failed to remove temporary filtered-bundle worktree ${tempWorktree}: ${getErrorMessage(removeError)}`);
              }
              fs.rmSync(tempWorktree, { recursive: true, force: true });
              fs.rmSync(tempHooksDir, { recursive: true, force: true });
            }
          } else {
            const bundleCreateArgs = ["bundle", "create", bundlePath, `${baseRef}..${branchName}`];
            if (mode === "incremental") {
              const defaultBranchRefResult = ensureOriginRemoteTrackingRef(defaultBranch, {
                cwd,
                token: options.token,
                suppressLogs: true,
              });
              if (defaultBranchRefResult.exists) {
                bundleCreateArgs.push(`^origin/${defaultBranch}`);
                debugLog(`Strategy 1 (incremental): excluding origin/${defaultBranch} from bundle prerequisites`);
              } else {
                const warningMessage = `Strategy 1 (incremental): origin/${defaultBranch} not present locally and remote fetch failed (likely private repo without credentials in MCP server); bundle will include base-branch history. Add ${JSON.stringify(defaultBranch)} to checkout.fetch to enable this optimisation.`;
                debugLog(warningMessage);
                core.warning(warningMessage);
              }
            }
            execGitSync(bundleCreateArgs, { cwd });
          }

          if (fs.existsSync(bundlePath)) {
            const stat = fs.statSync(bundlePath);
            if (stat.size > 0) {
              bundleGenerated = true;
              debugLog(`Strategy 1: SUCCESS - Generated bundle of ${stat.size} bytes`);
            }
          }
        } else if (mode === "incremental") {
          // In incremental mode, zero commits means nothing new to push
          return {
            success: false,
            error: "No new commits to push - your changes may already be on the remote branch",
            bundlePath,
            bundleSize: 0,
          };
        }
      } catch (branchError) {
        // Strategy 1 failed. Determine branch existence from local refs only so a
        // network/auth failure (e.g. a lazy blob fetch on a partial clone) is never
        // reported as a missing local branch.
        let branchExistsLocally = false;
        try {
          execGitSync(["rev-parse", "--verify", "--quiet", `refs/heads/${branchName}`], { cwd, suppressLogs: true });
          branchExistsLocally = true;
        } catch {
          // Branch really is absent from the local refs — probe failure is ignored.
        }
        const branchErrorMessage = describeGitFailure(getErrorMessage(branchError), cwd);
        if (branchExistsLocally) {
          debugLog(`Strategy 1: Failed to generate bundle for branch '${branchName}' (branch exists locally) - ${branchErrorMessage}`);
        } else {
          debugLog(`Strategy 1: Branch '${branchName}' does not exist locally - ${branchErrorMessage}`);
        }
        if (mode === "incremental") {
          if (branchExistsLocally) {
            return {
              success: false,
              error: `Cannot generate incremental bundle for branch ${branchName} in checkout '${cwd}': ${branchErrorMessage}`,
              bundlePath,
            };
          }
          return {
            success: false,
            error:
              `Branch ${branchName} does not exist locally in checkout '${cwd}'. ` +
              "Cannot generate incremental bundle. Possible causes: " +
              `(1) you have not checked out '${branchName}' yet (for example: git checkout -b ${branchName} --track origin/${branchName}); ` +
              "(2) the tool is running in the wrong repository checkout. Ensure GITHUB_WORKSPACE points to the repository that contains this pull request branch.",
            bundlePath,
          };
        }
      }
    }

    // Strategy 2: Check if commits were made to current HEAD since checkout
    if (!bundleGenerated) {
      debugLog(`Strategy 2: Checking commits since GITHUB_SHA`);
      const currentHead = execGitSync(["rev-parse", "HEAD"], { cwd }).trim();
      debugLog(`Strategy 2: currentHead=${currentHead}, GITHUB_SHA=${githubSha || "(not set)"}`);

      if (!githubSha) {
        debugLog(`Strategy 2: GITHUB_SHA not set, cannot use this strategy`);
        errorMessage = "GITHUB_SHA environment variable is not set";
      } else if (currentHead === githubSha) {
        debugLog(`Strategy 2: HEAD equals GITHUB_SHA - no new commits`);
      } else {
        let shaExistsInRepo = false;
        try {
          execGitSync(["cat-file", "-e", githubSha], { cwd });
          shaExistsInRepo = true;
          debugLog(`Strategy 2: GITHUB_SHA exists in this repo`);
        } catch {
          debugLog(`Strategy 2: GITHUB_SHA not found in repo (cross-repo checkout?)`);
        }

        if (shaExistsInRepo) {
          try {
            execGitSync(["merge-base", "--is-ancestor", githubSha, "HEAD"], { cwd });
            debugLog(`Strategy 2: GITHUB_SHA is an ancestor of HEAD`);

            baseCommitSha = githubSha;

            const commitCount = parseInt(execGitSync(["rev-list", "--count", `${githubSha}..HEAD`], { cwd }).trim(), 10);
            debugLog(`Strategy 2: Found ${commitCount} commits between GITHUB_SHA and HEAD`);

            if (commitCount > 0) {
              // If branchName is provided and doesn't exist locally (Strategy 1 already failed),
              // create a local branch pointing to HEAD so the bundle contains
              // refs/heads/<branchName> — required by create_pull_request.cjs when applying the bundle.
              let rangeEnd = "HEAD";
              if (branchName) {
                // Check whether the current branch already IS branchName.
                // `git branch -f` refuses to update the currently checked-out branch
                // ("cannot force update the branch used by worktree"), which would cause
                // rangeEnd to fall back to "HEAD" and produce a bundle with only a HEAD
                // ref instead of refs/heads/<branchName>.  This is the common case when
                // a worker agent checks out the new branch, commits on it, and then calls
                // create_pull_request — HEAD is already pointing to branchName.
                let currentBranch = "";
                try {
                  currentBranch = execGitSync(["rev-parse", "--abbrev-ref", "HEAD"], { cwd }).trim();
                } catch {
                  // Unable to determine current branch; fall through to git branch -f attempt.
                }

                if (currentBranch === branchName) {
                  rangeEnd = branchName;
                  debugLog(`Strategy 2: HEAD is already on '${branchName}', using as range end directly`);
                } else {
                  try {
                    // Use -f (force) to overwrite any stale local branch from previous runs,
                    // since Strategy 1 verified the named branch does not exist as a proper local ref.
                    // Use -- so a branch name beginning with "-" is not parsed as another option.
                    execGitSync(["branch", "-f", "--", branchName, "HEAD"], { cwd });
                    rangeEnd = branchName;
                    debugLog(`Strategy 2: Created local branch '${branchName}' pointing to HEAD for bundle ref`);
                  } catch (branchErr) {
                    debugLog(`Strategy 2: Could not create branch '${branchName}': ${getErrorMessage(branchErr)}, using HEAD`);
                  }
                }
              }
              execGitSync(["bundle", "create", bundlePath, `${githubSha}..${rangeEnd}`], { cwd });

              if (fs.existsSync(bundlePath)) {
                const stat = fs.statSync(bundlePath);
                if (stat.size > 0) {
                  bundleGenerated = true;
                  debugLog(`Strategy 2: SUCCESS - Generated bundle of ${stat.size} bytes`);
                }
              }
            }
          } catch (ancestorErr) {
            debugLog(`Strategy 2: GITHUB_SHA is not an ancestor of HEAD - ${getErrorMessage(ancestorErr)}`);
          }
        }
      }
    }

    // Strategy 3: Cross-repo fallback - find commits not reachable from any remote ref
    if (!bundleGenerated && branchName) {
      debugLog(`Strategy 3: Cross-repo fallback - finding commits not reachable from remote refs`);
      try {
        const remoteRefsOutput = execGitSync(["for-each-ref", "--format=%(refname)", "refs/remotes/"], { cwd }).trim();

        if (remoteRefsOutput) {
          const remoteRefs = remoteRefsOutput.split("\n").filter(r => r);
          debugLog(`Strategy 3: Found ${remoteRefs.length} remote refs`);

          if (remoteRefs.length > 0) {
            const remoteExcludeArgs = remoteRefs.flatMap(ref => ["--not", ref]);
            const revListArgs = ["rev-list", "--count", branchName, ...remoteExcludeArgs];

            const commitCount = parseInt(execGitSync(revListArgs, { cwd }).trim(), 10);
            debugLog(`Strategy 3: Found ${commitCount} commits not reachable from any remote ref`);

            if (commitCount > 0) {
              let baseCommit;
              for (const ref of remoteRefs) {
                try {
                  baseCommit = execGitSync(["merge-base", ref, branchName], { cwd }).trim();
                  if (baseCommit) {
                    debugLog(`Strategy 3: Found merge-base ${baseCommit} with ref ${ref}`);
                    break;
                  }
                } catch {
                  // Merge-base failed for this ref — ignored, try next ref.
                }
              }

              if (baseCommit) {
                baseCommitSha = baseCommit;
                execGitSync(["bundle", "create", bundlePath, `${baseCommit}..${branchName}`], { cwd });

                if (fs.existsSync(bundlePath)) {
                  const stat = fs.statSync(bundlePath);
                  if (stat.size > 0) {
                    bundleGenerated = true;
                    debugLog(`Strategy 3: SUCCESS - Generated bundle of ${stat.size} bytes`);
                  }
                }
              } else {
                debugLog(`Strategy 3: Could not find merge-base with any remote ref`);
              }
            }
          }
        } else {
          debugLog(`Strategy 3: No remote refs found`);
        }
      } catch (strategy3Err) {
        debugLog(`Strategy 3: Failed - ${getErrorMessage(strategy3Err)}`);
      }
    }
  } catch (error) {
    errorMessage = `Failed to generate bundle: ${getErrorMessage(error)}`;
  }

  // Check if bundle was generated and has content
  if (bundleGenerated && fs.existsSync(bundlePath)) {
    let stat;
    try {
      stat = fs.statSync(bundlePath);
    } catch (err) {
      return {
        success: false,
        error: `Failed to inspect generated bundle ${bundlePath}: ${getErrorMessage(err)}`,
        bundlePath,
      };
    }
    const bundleSize = stat.size;

    if (bundleSize === 0) {
      debugLog(`Final: Bundle file exists but is empty`);
      return {
        success: false,
        error: "No changes to commit - bundle is empty",
        bundlePath,
        bundleSize: 0,
      };
    }

    debugLog(`Final: SUCCESS - bundleSize=${bundleSize} bytes, baseCommit=${baseCommitSha || "(unknown)"}`);
    return {
      success: true,
      bundlePath,
      bundleSize,
      baseCommit: baseCommitSha,
    };
  }

  // No bundle generated
  debugLog(`Final: FAILED - ${errorMessage || "No changes to commit - no commits found"}`);
  return {
    success: false,
    error: errorMessage || "No changes to commit - no commits found",
    bundlePath,
  };
}

module.exports = {
  generateGitBundle,
  getBundlePathForBranch,
  getBundlePathForBranchInRepo,
  sanitizeBranchNameForBundle,
  sanitizeRepoSlugForBundle,
};
