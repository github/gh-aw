// @ts-check
/// <reference types="@actions/github-script" />

// SEC-005: This module generates git patches via git CLI commands and does not make
// GitHub API calls using a user-supplied target repository. The "target repo" references
// in documentation describe cross-repo checkout scenarios only; no validateTargetRepo
// allowlist check is required in this handler.

const fs = require("fs");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");
const { ensureOriginRemoteTrackingRef, execGitSync } = require("./git_helpers.cjs");
const { ERR_SYSTEM } = require("./error_codes.cjs");
const {
  sanitizeForFilename,
  sanitizeBranchNameForPatch,
  sanitizeRepoSlugForPatch,
  getPatchPathForBranch,
  getPatchPathForBranchInRepo,
  buildExcludePathspecs,
  computeIncrementalDiffSize,
  isAncestorCommit,
  describeGitFailure,
} = require("./git_patch_utils.cjs");
const { normalizeCommitSHA } = require("./commit_sha_helpers.cjs");
const { getExcludedWorkflowFiles, WORKFLOW_COMPILATION_NOTICE } = require("./workflow_file_exclusions.cjs");

// sanitizeForFilename is re-exported below for backward compatibility with
// existing callers that imported it from this module.
void sanitizeForFilename;

/**
 * Debug logging helper - logs to stderr when DEBUG env var matches
 * @param {string} message - Debug message to log
 */
function debugLog(message) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("generate_git_patch") || debug.includes("patch")) {
    console.error(`[generate_git_patch] ${message}`);
  }
}

function embedBaseCommit(patchContent, baseCommitSha) {
  const normalizedBaseCommitSha = normalizeCommitSHA(baseCommitSha);
  if (!normalizedBaseCommitSha || typeof patchContent !== "string") {
    return patchContent;
  }
  const firstNewline = patchContent.indexOf("\n");
  if (firstNewline < 0) {
    return patchContent;
  }
  return `${patchContent.slice(0, firstNewline + 1)}X-GH-AW-Base-Commit: ${normalizedBaseCommitSha}\n${patchContent.slice(firstNewline + 1)}`;
}

/**
 * Generates a git patch file for the current changes
 * @param {string} branchName - The branch name to generate patch for
 * @param {string} baseBranch - The base branch to diff against (e.g., "main", "master")
 * @param {Object} [options] - Optional parameters
 * @param {string} [options.mode="full"] - Patch generation mode:
 *   - "full": Include all commits since merge-base with default branch (for create_pull_request)
 *   - "incremental": Only include commits since origin/branchName (for push_to_pull_request_branch)
 *     In incremental mode, origin/branchName is fetched explicitly and merge-base fallback is disabled.
 * @param {string} [options.cwd] - Working directory for git commands. Defaults to GITHUB_WORKSPACE or process.cwd().
 *   Use this for multi-repo scenarios where repos are checked out to subdirectories.
 * @param {string} [options.workspacePath] - Path relative to GITHUB_WORKSPACE used as git working directory.
 *   When set, this takes precedence over options.cwd and must resolve to a directory under GITHUB_WORKSPACE.
 * @param {string} [options.repoSlug] - Repository slug (owner/repo) to include in patch filename for disambiguation.
 *   Required for multi-repo scenarios to prevent patch file collisions.
 * @param {string} [options.token] - GitHub token for git authentication. Falls back to GITHUB_TOKEN env var.
 *   Use this for cross-repo scenarios where a custom PAT with access to the target repo is needed.
 * @param {string[]} [options.excludedFiles] - Glob patterns for files to exclude from the patch.
 *   Each pattern is passed to `git format-patch` as a `:(exclude)<pattern>` magic pathspec so
 *   matching files are never included in the generated patch.
 * @param {string} [options.pinnedSha] - SECURITY: When set, use this SHA as the branch tip instead
 *   of resolving refs/heads/<branchName>. Prevents TOCTOU races where the agent flips the branch
 *   ref between patch and bundle generation.
 * @param {string} [options.incrementalBaseRef] - Explicit local ref to use as the pre-agent
 *   PR head in incremental mode (for example refs/remotes/origin/pr-head for fork PR checkouts).
 * @param {string} [options.incrementalBaseSha] - Explicit commit SHA to use as the pre-agent
 *   PR head in incremental mode. Takes precedence over incrementalBaseRef when present.
 * @returns {Promise<Object>} Object with patch info or error
 */
async function generateGitPatch(branchName, baseBranch, options = {}) {
  const mode = options.mode || "full";
  const workspaceRoot = process.env.GITHUB_WORKSPACE || process.cwd();
  let cwd = options.cwd || workspaceRoot;
  // Include repo slug in patch path for multi-repo disambiguation

  // Build :(exclude) pathspec arguments from the excludedFiles option.
  // These are appended after "--" so git treats them as pathspecs, not revisions.
  // Using git's native pathspec magic keeps the exclusions out of the patch entirely
  // without any post-processing of the generated patch file.
  const excludeArgsArr = buildExcludePathspecs(options.excludedFiles);

  /**
   * Returns the arguments to append to a format-patch call when excludedFiles is set.
   * @returns {string[]}
   */
  function excludeArgs() {
    return excludeArgsArr;
  }
  const patchPath = options.repoSlug ? getPatchPathForBranchInRepo(branchName, options.repoSlug) : getPatchPathForBranch(branchName);

  if (options.workspacePath !== undefined && options.workspacePath !== null && String(options.workspacePath).trim() !== "") {
    const root = path.resolve(workspaceRoot);
    const candidate = path.resolve(root, String(options.workspacePath));
    const relative = path.relative(root, candidate);
    if (relative.startsWith("..") || path.isAbsolute(relative)) {
      const errorMessage = `Invalid workspacePath '${String(options.workspacePath)}': path must stay under GITHUB_WORKSPACE`;
      return {
        success: false,
        error: errorMessage,
        patchPath,
      };
    }
    if (!fs.existsSync(candidate)) {
      return {
        success: false,
        error: `Invalid workspacePath '${String(options.workspacePath)}': directory does not exist`,
        patchPath,
      };
    }
    let candidateStat;
    try {
      candidateStat = fs.statSync(candidate);
    } catch (err) {
      return {
        success: false,
        error: `Failed to inspect workspacePath '${String(options.workspacePath)}': ${getErrorMessage(err)}`,
        patchPath,
      };
    }
    if (!candidateStat.isDirectory()) {
      return {
        success: false,
        error: `Invalid workspacePath '${String(options.workspacePath)}': path is not a directory`,
        patchPath,
      };
    }
    cwd = candidate;
  }

  // Validate baseBranch early to avoid confusing git errors (e.g., origin/undefined)
  if (typeof baseBranch !== "string" || baseBranch.trim() === "") {
    const errorMessage = "baseBranch is required and must be a non-empty string (received: " + String(baseBranch) + ")";
    debugLog(`Invalid baseBranch: ${errorMessage}`);
    return {
      patchPath,
      patchGenerated: false,
      errorMessage,
    };
  }

  const defaultBranch = baseBranch;
  const githubSha = process.env.GITHUB_SHA;

  debugLog(`Starting patch generation: mode=${mode}, branch=${branchName}, defaultBranch=${defaultBranch}`);
  debugLog(`Environment: cwd=${cwd}, GITHUB_SHA=${githubSha || "(not set)"}`);

  // Ensure /tmp/gh-aw directory exists
  const patchDir = path.dirname(patchPath);
  if (!fs.existsSync(patchDir)) {
    try {
      fs.mkdirSync(patchDir, { recursive: true });
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to create directory ${patchDir}: ${getErrorMessage(err)}`, { cause: err });
    }
  }

  let patchGenerated = false;
  /** @type {any} */
  let errorMessage = null;
  // Track the resolved base commit SHA so consumers (e.g. create_pull_request fallback)
  // can use it directly. The From <sha> header in format-patch output contains the
  // *new* commit SHA which won't exist in the target checkout.
  /** @type {any} */
  let baseCommitSha = null;
  /** @type {any} */
  let resolvedTipRef = null;

  try {
    // Strategy 1: If we have a branch name, check if that branch exists and get its diff
    if (branchName) {
      resolvedTipRef = options.pinnedSha || branchName;
      // SECURITY: When pinnedSha is provided, use it directly as the tip commit instead
      // of dereferencing refs/heads/<branchName>. This prevents TOCTOU races where the
      // agent can flip the branch ref between patch and bundle generation.
      const tipRef = resolvedTipRef;
      try {
        if (options.pinnedSha) {
          debugLog(`Strategy 1: Using pinned SHA ${options.pinnedSha} (branch: ${branchName})`);
        } else {
          debugLog(`Strategy 1: Checking if branch '${branchName}' exists locally`);
          // Check if the branch exists locally. This is a local-ref lookup only:
          // it never touches the network, so a failure here always means "no such
          // local branch" and never an auth/network problem.
          execGitSync(["rev-parse", "--verify", "--quiet", `refs/heads/${branchName}`], { cwd });
          debugLog(`Strategy 1: Branch '${branchName}' exists locally`);
        }

        // Determine base ref for patch generation
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
                `Cannot generate incremental patch: explicit PR-head baseline ${candidateBaseRef} is not present in checkout '${cwd}'. ` +
                `Ensure the PR checkout step completed and fetched refs/pull/<number>/head before calling push_to_pull_request_branch.`;
              debugLog(`Strategy 1 (incremental): explicit PR-head baseline failed: ${getErrorMessage(baselineError)}`);
              return {
                success: false,
                error: errorMessage,
                patchPath: patchPath,
              };
            }
          } else {
            // Backward-compatible fallback for same-repository PRs and existing callers:
            // try origin/branchName locally, then a single network fetch attempt.
            // The fetch will succeed for public same-repo branches (no credentials
            // needed) and fail fast when unavailable.
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
                `Cannot generate incremental patch: no pre-agent PR-head baseline was available for branch '${branchName}' in checkout '${cwd}'. ` +
                `Tried refs/remotes/origin/${branchName}, but it is not present and could not be fetched from origin. ` +
                `For fork PRs, ensure the workflow PR checkout step ran first so refs/pull/<number>/head is recorded as the patch baseline; checkout.fetch cannot fetch fork branches from the base repository.`;
              return {
                success: false,
                error: errorMessage,
                patchPath: patchPath,
              };
            }
          }
        } else {
          // FULL MODE (for create_pull_request):
          // Include all commits since merge-base with default branch.
          // This is appropriate for creating new PRs where we want all changes.
          //
          // IMPORTANT: We deliberately do NOT short-circuit to `origin/${branchName}` even
          // when that remote-tracking ref exists locally. That ref is fetched at workflow
          // startup and represents the *remote* branch state at that moment, not the
          // branch state before the agent made changes. If the local branch was
          // fast-forwarded to the default branch during the agent run (a common pattern),
          // using the stale `origin/${branchName}` would cause the patch to include all
          // commits from the default branch since the old branch tip — commits the agent
          // never made. Always compute the merge-base with the default branch so the patch
          // contains exactly the agent's changes.
          debugLog(`Strategy 1 (full): Computing merge-base with ${defaultBranch} (ignoring any stale origin/${branchName})`);
          // Check if origin/<defaultBranch> already exists locally (e.g., from checkout with fetch-depth: 0).
          // When missing, try a single network fetch — succeeds for public repos and
          // fails fast for private repos without credentials.
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
              `Strategy 1 (full): origin/${defaultBranch} not present locally and remote fetch failed (likely private repo without credentials in MCP server). Add ${JSON.stringify(defaultBranch)} to checkout.fetch to enable this strategy.`
            );
          }

          // If origin/<defaultBranch> is unavailable (e.g. credentials were cleaned),
          // fall back to the local base branch ref when it exists.
          /** @type {any} */
          let defaultBranchRef = null;
          if (hasLocalDefaultBranch) {
            defaultBranchRef = `origin/${defaultBranch}`;
          } else {
            try {
              execGitSync(["show-ref", "--verify", "--quiet", `refs/heads/${defaultBranch}`], { cwd });
              defaultBranchRef = defaultBranch;
              debugLog(`Strategy 1 (full): Using local branch ${defaultBranch} as fallback base ref`);
            } catch {
              // No local branch fallback either — ignored.
            }
          }

          // When the workflow runs from a ref that is not contained in the default
          // branch (the general case for a workflow_dispatch on a feature branch),
          // the merge-base with the default branch is far behind the checked-out
          // commit. Basing the patch there would include the dispatched branch's own
          // commits instead of only the agent's, and on a partial clone it requires
          // base-side blobs that were never fetched (the lazy fetch is unauthenticated
          // and fails). GITHUB_SHA is the commit the agent started from and every
          // object it needs is already present in the checkout.
          const dispatchedSha = normalizeCommitSHA(githubSha);
          const tipSha = execGitSync(["rev-parse", tipRef], { cwd }).trim();
          if (defaultBranchRef && dispatchedSha && dispatchedSha !== tipSha && isAncestorCommit(dispatchedSha, tipRef, cwd) && !isAncestorCommit(dispatchedSha, defaultBranchRef, cwd)) {
            baseRef = dispatchedSha;
            debugLog(`Strategy 1 (full): GITHUB_SHA ${dispatchedSha} is not contained in ${defaultBranchRef} (non-default-branch run); using it as the patch base instead of the merge-base`);
          } else if (defaultBranchRef) {
            try {
              baseRef = execGitSync(["merge-base", "--", defaultBranchRef, tipRef], { cwd }).trim();
            } catch (mergeBaseError) {
              // A shallow clone (or a `--depth` fetch that grafted history onto an
              // otherwise complete clone) can make the merge-base unreachable.
              // Surface that explicitly instead of the misleading "branch does not
              // exist locally" message.
              if (fs.existsSync(path.join(cwd || process.cwd(), ".git", "shallow"))) {
                /** @type {any} */
                const shallowCloneError = new Error(
                  `${ERR_SYSTEM}: Could not compute merge-base between ${defaultBranchRef} and ${tipRef} because the repository is a shallow clone (.git/shallow exists). ` +
                    "Deepen the clone (checkout.fetch-depth: 0) so the common ancestor is reachable."
                );
                shallowCloneError.isShallowCloneDiagnostic = true;
                throw shallowCloneError;
              }
              throw mergeBaseError;
            }
            debugLog(`Strategy 1 (full): Computed merge-base: ${baseRef}`);
          } else {
            // No remote refs available - fall through to Strategy 2
            debugLog(`Strategy 1 (full): No remote refs available, falling through to Strategy 2`);
            throw new Error(`${ERR_SYSTEM}: No remote refs available for merge-base calculation`);
          }
        }

        // Resolve baseRef to a SHA so we can record it for consumers
        baseCommitSha = execGitSync(["rev-parse", baseRef], { cwd }).trim();
        debugLog(`Strategy 1: Resolved baseRef ${baseRef} to SHA ${baseCommitSha}`);

        // Count commits to be included
        const commitCount = parseInt(execGitSync(["rev-list", "--count", `${baseRef}..${tipRef}`], { cwd }).trim(), 10);
        debugLog(`Strategy 1: Found ${commitCount} commits between ${baseRef} and ${tipRef}`);

        if (commitCount > 0) {
          // Generate patch from the determined base to the branch
          const patchContent = execGitSync(["format-patch", `${baseRef}..${tipRef}`, "--stdout", ...excludeArgs()], { cwd });

          if (patchContent && patchContent.trim()) {
            fs.writeFileSync(patchPath, embedBaseCommit(patchContent, baseCommitSha), "utf8");
            patchGenerated = true;
            debugLog(`Strategy 1: SUCCESS - Generated patch with ${patchContent.split("\n").length} lines`);
          }
        } else if (mode === "incremental") {
          // In incremental mode, zero commits means nothing new to push
          return {
            success: false,
            error: "No new commits to push - your changes may already be on the remote branch",
            patchPath: patchPath,
            patchSize: 0,
            patchLines: 0,
          };
        }

        // In incremental mode, the patch must be measured relative to the existing
        // PR branch head (origin/<branch>), never relative to the default branch.
        // If Strategy 1 did not produce a patch (e.g. format-patch yielded empty
        // output for an unusual commit shape — excluded-files filtering away every
        // change, or binary-only commits with unusual encoding), do NOT fall
        // through to Strategy 2 or Strategy 3 — those use GITHUB_SHA..HEAD or
        // merge-base with a remote ref and would produce a checkout-base diff
        // (which can be many MB on a long-running branch). Returning an explicit
        // error preserves the "incremental" contract that the patch reflects only
        // the new commits.
        if (!patchGenerated && mode === "incremental") {
          debugLog(`Strategy 1 (incremental): format-patch produced no output for ${baseRef}..${tipRef} despite ${commitCount} incremental commit(s), refusing to fall through to checkout-base strategies`);
          return {
            success: false,
            error: `Cannot generate incremental patch: git format-patch produced no output for ${baseRef}..${tipRef} despite ${commitCount} incremental commit(s).`,
            patchPath: patchPath,
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
          debugLog(`Strategy 1: Failed to generate patch for branch '${branchName}' (branch exists locally) - ${branchErrorMessage}`);
        } else {
          debugLog(`Strategy 1: Branch '${branchName}' does not exist locally - ${branchErrorMessage}`);
        }
        // Shallow-clone diagnostics (thrown explicitly from the merge-base block
        // above, marked with isShallowCloneDiagnostic) must reach callers immediately —
        // falling through to Strategy 2 or 3 would produce a misleading "No changes
        // to commit" result instead. Other ERR_SYSTEM-prefixed errors (e.g. an
        // expected "branch not found" failure from rev-parse) must still
        // fall through to the later strategies.
        if (branchError && branchError.isShallowCloneDiagnostic) {
          return {
            success: false,
            error: getErrorMessage(branchError),
            patchPath: patchPath,
          };
        }
        if (options.pinnedSha) {
          // SECURITY: When pinnedSha is set, fail closed — do not fall through to
          // other strategies that would resolve a different commit.
          return {
            success: false,
            error: `Pinned SHA ${options.pinnedSha} failed to generate patch: ${branchErrorMessage}`,
            patchPath: patchPath,
          };
        }
        if (mode === "incremental") {
          if (branchExistsLocally) {
            return {
              success: false,
              error: `Cannot generate incremental patch for branch ${branchName} in checkout '${cwd}': ${branchErrorMessage}`,
              patchPath: patchPath,
            };
          }
          return {
            success: false,
            error:
              `Branch ${branchName} does not exist locally in checkout '${cwd}'. ` +
              "Cannot generate incremental patch. Possible causes: " +
              `(1) you have not checked out '${branchName}' yet (for example: git checkout -b ${branchName} --track origin/${branchName}); ` +
              "(2) the tool is running in the wrong repository checkout. Ensure GITHUB_WORKSPACE points to the repository that contains this pull request branch.",
            patchPath: patchPath,
          };
        }
      }
    }

    // Strategy 2: Check if commits were made to current HEAD since checkout
    if (!patchGenerated) {
      debugLog(`Strategy 2: Checking commits since GITHUB_SHA`);
      const currentHead = execGitSync(["rev-parse", "HEAD"], { cwd }).trim();
      debugLog(`Strategy 2: currentHead=${currentHead}, GITHUB_SHA=${githubSha || "(not set)"}`);

      if (!githubSha) {
        debugLog(`Strategy 2: GITHUB_SHA not set, cannot use this strategy`);
        errorMessage = "GITHUB_SHA environment variable is not set";
      } else if (currentHead === githubSha) {
        // No commits have been made since checkout
        debugLog(`Strategy 2: HEAD equals GITHUB_SHA - no new commits`);
      } else {
        // First verify GITHUB_SHA exists in this repo's git history
        // In cross-repo checkout scenarios, GITHUB_SHA is from the workflow repo,
        // not the checked-out repository
        let shaExistsInRepo = false;
        try {
          execGitSync(["cat-file", "-e", githubSha], { cwd });
          shaExistsInRepo = true;
          debugLog(`Strategy 2: GITHUB_SHA exists in this repo`);
        } catch {
          // GITHUB_SHA doesn't exist in this repo - likely a cross-repo checkout
          // This is expected when workflow repo != checked out repo
          debugLog(`Strategy 2: GITHUB_SHA not found in repo (cross-repo checkout?)`);
        }

        if (shaExistsInRepo) {
          // Check if GITHUB_SHA is an ancestor of current HEAD
          try {
            execGitSync(["merge-base", "--is-ancestor", githubSha, "HEAD"], { cwd });
            debugLog(`Strategy 2: GITHUB_SHA is an ancestor of HEAD`);

            // Record GITHUB_SHA as the base commit
            baseCommitSha = githubSha;
            resolvedTipRef = currentHead;

            // Count commits between GITHUB_SHA and HEAD
            const commitCount = parseInt(execGitSync(["rev-list", "--count", `${githubSha}..HEAD`], { cwd }).trim(), 10);
            debugLog(`Strategy 2: Found ${commitCount} commits between GITHUB_SHA and HEAD`);

            if (commitCount > 0) {
              // Generate patch from GITHUB_SHA to HEAD
              const patchContent = execGitSync(["format-patch", `${githubSha}..HEAD`, "--stdout", ...excludeArgs()], { cwd });

              if (patchContent && patchContent.trim()) {
                fs.writeFileSync(patchPath, embedBaseCommit(patchContent, baseCommitSha), "utf8");
                patchGenerated = true;
                debugLog(`Strategy 2: SUCCESS - Generated patch with ${patchContent.split("\n").length} lines`);
              }
            }
          } catch (ancestorErr) {
            // GITHUB_SHA is not an ancestor of HEAD - repository state has diverged
            debugLog(`Strategy 2: GITHUB_SHA is not an ancestor of HEAD - ${getErrorMessage(ancestorErr)}`);
          }
        }
      }
    }

    // Strategy 3: Cross-repo fallback - find commits not reachable from any remote ref
    // This handles cases where:
    // - Cross-repo checkout with persist-credentials: false (can't fetch)
    // - GITHUB_SHA is from a different repo
    // - No origin/<defaultBranch> available locally
    if (!patchGenerated && branchName) {
      debugLog(`Strategy 3: Cross-repo fallback - finding commits not reachable from remote refs`);
      try {
        // Get all remote refs
        const remoteRefsOutput = execGitSync(["for-each-ref", "--format=%(refname)", "refs/remotes/"], { cwd }).trim();

        if (remoteRefsOutput) {
          // Build exclusion list from all remote refs
          const remoteRefs = remoteRefsOutput.split("\n").filter(r => r);
          debugLog(`Strategy 3: Found ${remoteRefs.length} remote refs: ${remoteRefs.slice(0, 5).join(", ")}${remoteRefs.length > 5 ? "..." : ""}`);

          if (remoteRefs.length > 0) {
            // Find commits on current branch not reachable from any remote ref
            // This gets commits the agent added that haven't been pushed anywhere
            const remoteExcludeArgs = remoteRefs.flatMap(ref => ["--not", ref]);
            const revListArgs = ["rev-list", "--count", branchName, ...remoteExcludeArgs];

            const commitCount = parseInt(execGitSync(revListArgs, { cwd }).trim(), 10);
            debugLog(`Strategy 3: Found ${commitCount} commits not reachable from any remote ref`);

            if (commitCount > 0) {
              // Choose the closest merge-base across all remote refs.
              // for-each-ref output is lexicographic, so "first ref" is arbitrary and can
              // point to stale branches that produce oversized patches.
              /** @type {any} */
              let bestBaseCommit = null;
              /** @type {any} */
              let bestBaseRef = null;
              let bestCommitCount = Number.POSITIVE_INFINITY;
              for (const ref of remoteRefs) {
                try {
                  const candidateBase = execGitSync(["merge-base", ref, "--", branchName], { cwd }).trim();
                  if (!candidateBase) {
                    continue;
                  }

                  const candidateCommitCount = parseInt(execGitSync(["rev-list", "--count", `${candidateBase}..${branchName}`], { cwd }).trim(), 10);
                  if (Number.isNaN(candidateCommitCount)) {
                    debugLog(`Strategy 3: Ignoring merge-base ${candidateBase} from ref ${ref} due to invalid commit count`);
                    continue;
                  }
                  if (candidateCommitCount <= 0) {
                    debugLog(`Strategy 3: Skipping ref ${ref} — merge-base not behind branch (count=${candidateCommitCount})`);
                    continue;
                  }

                  if (candidateCommitCount < bestCommitCount) {
                    bestBaseCommit = candidateBase;
                    bestBaseRef = ref;
                    bestCommitCount = candidateCommitCount;
                    if (bestCommitCount === 1) {
                      break;
                    }
                  }
                } catch {
                  // Candidate ref could not be measured — ignored, try next ref.
                }
              }

              if (bestBaseCommit) {
                baseCommitSha = bestBaseCommit;
                resolvedTipRef = branchName;
                debugLog(`Strategy 3: Selected merge-base ${bestBaseCommit} with ref ${bestBaseRef} (commitCount=${bestCommitCount})`);
                const patchContent = execGitSync(["format-patch", `${bestBaseCommit}..${branchName}`, "--stdout", ...excludeArgs()], { cwd });

                if (patchContent && patchContent.trim()) {
                  fs.writeFileSync(patchPath, embedBaseCommit(patchContent, baseCommitSha), "utf8");
                  patchGenerated = true;
                  debugLog(`Strategy 3: SUCCESS - Generated patch with ${patchContent.split("\n").length} lines`);
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
        // Strategy 3 failed - no remote refs available at all
        debugLog(`Strategy 3: Failed - ${getErrorMessage(strategy3Err)}`);
      }
    }
  } catch (error) {
    errorMessage = `Failed to generate patch: ${getErrorMessage(error)}`;
  }

  let excludedWorkflowFiles = [];
  if (options.excludedFiles?.length && baseCommitSha && resolvedTipRef) {
    try {
      const changedFiles = execGitSync(["diff", "--name-only", "--no-renames", "-z", baseCommitSha, resolvedTipRef, "--", ".github/workflows/"], { cwd }).split("\0").filter(Boolean);
      excludedWorkflowFiles = getExcludedWorkflowFiles(changedFiles, options.excludedFiles);
    } catch (error) {
      return { success: false, error: `Failed to detect excluded workflow files: ${getErrorMessage(error)}`, patchPath };
    }
  }

  // Check if patch was generated and has content
  if (patchGenerated && fs.existsSync(patchPath)) {
    let patchContent;
    try {
      patchContent = fs.readFileSync(patchPath, "utf8");
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to read file ${patchPath}: ${getErrorMessage(err)}`, { cause: err });
    }
    const patchSize = Buffer.byteLength(patchContent, "utf8");
    const patchLines = patchContent.split("\n").length;

    if (!patchContent.trim()) {
      // Empty patch
      debugLog(`Final: Patch file exists but is empty`);
      return {
        success: false,
        error: "No changes to commit - patch is empty",
        patchPath: patchPath,
        patchSize: 0,
        patchLines: 0,
      };
    }

    // In incremental mode, also compute the net diff size between baseRef and the
    // branch tip. The format-patch file size (patchSize) is the sum of every
    // commit's individual diff plus per-commit metadata headers, which can be
    // significantly larger than the actual net change. Consumers (e.g.
    // push_to_pull_request_branch) should validate `max_patch_size` against the
    // incremental net diff so the limit reflects how much the branch will
    // actually change, not the cumulative size of the commit history.
    //
    // The measurement itself (stream to temp file via `git diff --output`, stat,
    // cleanup) is extracted into git_patch_utils.computeIncrementalDiffSize so
    // it is O(1) memory and independently unit-testable against a real repo.
    //
    // When the agent has merged the default branch into the PR branch (to resolve
    // conflicts or sync a stale branch), the naive diff base of `origin/<branch>`
    // (the PR's old head) inflates diffSize to include all of the default branch's
    // new commits — even though those commits are already on origin/<defaultBranch>
    // and represent no new content in the PR. Fix: when the merge-base between
    // origin/<defaultBranch> and the local branch is NOT an ancestor of the PR's
    // current head (baseCommitSha), the agent merged default-branch commits ahead
    // of the PR head. Use the merge-base as the effective diff base to exclude those
    // merged upstream commits from the size measurement.
    let diffBaseForSize = baseCommitSha;
    if (mode === "incremental" && baseCommitSha && resolvedTipRef && defaultBranch) {
      try {
        /** @type {any} */
        let baseBranchRemoteRef = null;
        try {
          execGitSync(["show-ref", "--verify", "--quiet", `refs/remotes/origin/${defaultBranch}`], { cwd });
          baseBranchRemoteRef = `refs/remotes/origin/${defaultBranch}`;
        } catch {
          // origin/<defaultBranch> not available locally — ignored, skip the adjustment.
        }
        if (baseBranchRemoteRef) {
          // Only adjust the diff base when baseCommitSha is an ancestor of the local
          // branch tip.  If it is NOT an ancestor the branch was rewritten (rebase /
          // force-push); in that case the merge-base adjustment could undercount by
          // ignoring commits that changed relative to the remote, so keep the original
          // baseCommitSha as the diff base.
          let baseIsAncestorOfBranch = false;
          try {
            execGitSync(["merge-base", "--is-ancestor", "--", baseCommitSha, resolvedTipRef], { cwd });
            baseIsAncestorOfBranch = true;
          } catch {
            // baseCommitSha is not an ancestor of tipRef (rebase / force-push)
            debugLog(`Strategy 1 (incremental): baseCommitSha ${baseCommitSha} is not an ancestor of ${resolvedTipRef} (rebase/force-push?); skipping merge-base adjustment`);
          }

          if (baseIsAncestorOfBranch) {
            const mb = execGitSync(["merge-base", "--", baseBranchRemoteRef, resolvedTipRef], { cwd }).trim();
            // Check if mb is already an ancestor of baseCommitSha.
            // If it is, baseCommitSha is "later" and the agent did NOT merge the default
            // branch ahead of the PR head — keep baseCommitSha as the diff base.
            // If mb is NOT an ancestor of baseCommitSha, the agent merged default-branch
            // commits that are beyond the PR head. Use mb to exclude those commits from
            // the incremental diff size measurement.
            let mbIsAncestorOfBase = false;
            try {
              execGitSync(["merge-base", "--is-ancestor", "--", mb, baseCommitSha], { cwd });
              mbIsAncestorOfBase = true;
            } catch {
              // mb is not an ancestor of baseCommitSha — probe failure is ignored.
            }
            if (!mbIsAncestorOfBase) {
              debugLog(`Strategy 1 (incremental): agent merged ${defaultBranch} ahead of PR head; using merge-base ${mb} as diff base instead of PR head ${baseCommitSha}`);
              diffBaseForSize = mb;
            }
          }
        }
      } catch (adjustErr) {
        debugLog(`Strategy 1 (incremental): diff-base adjustment failed (${getErrorMessage(adjustErr)}); using original base`);
      }
    }

    /** @type {any} */
    let diffSize = null;
    if (mode === "incremental" && diffBaseForSize && resolvedTipRef) {
      diffSize = computeIncrementalDiffSize({
        baseRef: diffBaseForSize,
        headRef: resolvedTipRef,
        cwd,
        tmpPath: `${patchPath}.diff.tmp`,
        excludedFiles: options.excludedFiles,
      });
      debugLog(`Final: diffSize=${diffSize ?? "(n/a)"} bytes (baseRef=${diffBaseForSize}..${resolvedTipRef})`);
    }

    debugLog(`Final: SUCCESS - patchSize=${patchSize} bytes, patchLines=${patchLines}, diffSize=${diffSize ?? "(n/a)"} bytes, baseCommit=${baseCommitSha || "(unknown)"}`);
    return {
      success: true,
      patchPath: patchPath,
      patchSize: patchSize,
      patchLines: patchLines,
      diffSize: diffSize,
      baseCommit: baseCommitSha,
      excludedWorkflowFiles,
    };
  }

  // No patch generated
  debugLog(`Final: FAILED - ${errorMessage || "No changes to commit - no commits found"}`);
  return {
    success: false,
    error: excludedWorkflowFiles.length > 0 ? `${errorMessage || "No changes remain after workflow YAML exclusions."}\n\n${WORKFLOW_COMPILATION_NOTICE}` : errorMessage || "No changes to commit - no commits found",
    patchPath: patchPath,
    excludedWorkflowFiles,
  };
}

module.exports = {
  generateGitPatch,
  getPatchPathForBranch,
  getPatchPathForBranchInRepo,
  sanitizeBranchNameForPatch,
  sanitizeRepoSlugForPatch,
  embedBaseCommit,
};
