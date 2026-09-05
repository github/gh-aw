// @ts-check
/// <reference types="@actions/github-script" />

/** @type {typeof import("fs")} */
const fs = require("fs");
/** @type {typeof import("crypto")} */
const crypto = require("crypto");
const { updateActivationComment } = require("./update_activation_comment.cjs");
const { pushSignedCommits } = require("./push_signed_commits.cjs");
const { getTrackerID } = require("./get_tracker_id.cjs");
const { removeDuplicateTitleFromDescription } = require("./remove_duplicate_title.cjs");
const { sanitizeTitle, applyTitlePrefix } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { replaceTemporaryIdReferences, replaceTemporaryIdReferencesInPatch, getOrGenerateTemporaryId } = require("./temporary_id.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { addExpirationToFooter } = require("./ephemerals.cjs");
const { generateWorkflowIdMarker, generateWorkflowCallIdMarker, generateCloseKeyMarker, normalizeCloseOlderKey } = require("./generate_footer.cjs");
const { parseBoolTemplatable, parseIntTemplatable } = require("./templatable.cjs");
const { assembleMarkdownBodyParts } = require("./markdown_body_helpers.cjs");
const { getBodyHeader, getDisclosureHeader } = require("./messages_header.cjs");
const { generateHistoryUrl } = require("./generate_history_link.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { pushExtraEmptyCommit } = require("./extra_empty_commit.cjs");
const { createCheckoutManager } = require("./dynamic_checkout.cjs");
const { closeOlderPullRequests } = require("./close_older_pull_requests.cjs");
const { findRepoCheckout } = require("./find_repo_checkout.cjs");
const { getBaseBranch } = require("./get_base_branch.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { checkFileProtection, checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { renderTemplateFromFile, renderFilesList, buildProtectedFileList, getPromptPath } = require("./messages_core.cjs");
const { withGitHubHostToken } = require("./git_auth_helpers.cjs");
const { COPILOT_REVIEWER_BOT, FAQ_CREATE_PR_PERMISSIONS_URL } = require("./constants.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { extractPatchBaseCommit } = require("./commit_sha_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { createOrUpdatePullRequest } = require("./create_or_update_pull_request.cjs");
const { findAgent, getIssueDetails, assignAgentToIssue } = require("./assign_agent_helpers.cjs");
const { ensureFullHistoryForBundle, extractBundlePrerequisiteCommits, getBundlePrerequisites, isShallowOrSparseCheckout, linearizeRangeAsCommit } = require("./git_helpers.cjs");
const { parseDiffGitHeader: parseDiffGitHeaderPaths, extractDiffGitHeaderEntries } = require("./patch_path_helpers.cjs");
const { resolveTransportPaths } = require("./resolve_transport_paths.cjs");
const { resolveAllowedMentionsFromPayload } = require("./resolve_mentions_from_payload.cjs");
const {
  MANAGED_FALLBACK_ISSUE_LABEL,
  LABEL_MAX_RETRIES,
  LABEL_INITIAL_DELAY_MS,
  LABEL_MAX_DELAY_MS,
  summarizeListForLog,
  createBundleTempRef,
  isLabelTransientError,
  parseAllowedBaseBranches,
  isBaseBranchAllowed,
  parseStringListConfig,
  mergeFallbackIssueLabels,
  sanitizeFallbackAssignees,
  neutralizeClosingKeywordsForIssueBody,
  generatePatchPreview,
  buildManifestProtectionCreatePrUrl,
  renderManifestProtectionFallbackBody,
  buildPushErrorSection,
  buildManualBranchRecoveryCommands,
  shellQuote,
} = require("./create_pull_request_helpers.cjs");
const { isStackedEnabled, parseStackMetadata, hasCircularStackDependency, buildStackMetadataLines, stackedDisabledError, circularStackError, verifyStackBaseBranchExists, createStackTracker } = require("./stacked_pull_requests.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/**
 * Creates an authenticated GitHub client for copilot assignment on fallback issues.
 * Prefers the agent-specific token (GH_AW_ASSIGN_TO_AGENT_TOKEN) because the Copilot
 * assignment API requires a PAT rather than a GitHub App token.
 *
 * Token priority:
 *   1. config["github-token"] — explicit per-handler override
 *   2. GH_AW_ASSIGN_TO_AGENT_TOKEN — injected by the compiler when copilot is in assignees
 *   3. global github — step-level token (fallback when no agent token is available)
 *
 * @param {Object} config - Handler configuration
 * @returns {Promise<Object>} Authenticated GitHub client
 */
async function createCopilotAssignmentClient(config) {
  const token = config["github-token"] || process.env.GH_AW_ASSIGN_TO_AGENT_TOKEN;
  if (!token) {
    core.debug("No dedicated agent token configured — using step-level github client for copilot assignment");
    return github;
  }
  core.info("Using dedicated github client for copilot assignment");
  return global.getOctokit(token);
}

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "create_pull_request";

// NOTE: MANAGED_FALLBACK_ISSUE_LABEL, createBundleTempRef, and summarizeListForLog
// are imported from create_pull_request_helpers.cjs above.

/**
 * Attempt automatic recovery for git am add/add conflicts by preferring the patch version.
 *
 * @param {{ exec: Function, getExecOutput: Function }} execApi - Exec API with git command helpers
 * @returns {Promise<{ recovered: boolean, attempted: boolean, errorMessage?: string }>}
 */
async function tryRecoverGitAmAddAddConflict(execApi) {
  try {
    const unresolvedFilesResult = await execApi.getExecOutput("git", ["diff", "--name-only", "--diff-filter=U", "-z"]);
    const unresolvedFiles = unresolvedFilesResult.stdout.split("\0").filter(line => line.length > 0);
    core.debug(`Add/add recovery probe unresolved files (${unresolvedFiles.length}): ${summarizeListForLog(unresolvedFiles)}`);

    if (unresolvedFiles.length === 0) {
      return { recovered: false, attempted: false };
    }

    const statusPorcelainResult = await execApi.getExecOutput("git", ["status", "--porcelain", "-z"]);
    const addAddFiles = new Set(
      statusPorcelainResult.stdout
        .split("\0")
        .filter(line => line.length > 0)
        .filter(line => line.startsWith("AA "))
        .map(line => line.substring(3))
    );
    core.debug(`Add/add recovery probe AA files (${addAddFiles.size}): ${summarizeListForLog(Array.from(addAddFiles))}`);
    const allConflictsAreAddAdd = unresolvedFiles.every(file => addAddFiles.has(file));
    if (!allConflictsAreAddAdd) {
      core.debug("Add/add recovery skipped because unresolved conflicts include non-AA entries");
      return { recovered: false, attempted: false };
    }

    core.warning(`Detected add/add conflict(s) for ${unresolvedFiles.join(", ")}; preferring patch version and continuing`);
    for (const file of unresolvedFiles) {
      try {
        const { stdout: unresolvedIndexOutput } = await execApi.getExecOutput("git", ["ls-files", "-u", "--", file]);
        let oursBlobSha = "";
        let theirsBlobSha = "";
        for (const line of unresolvedIndexOutput.split("\n")) {
          if (!line.trim()) {
            continue;
          }
          const fields = line.trim().split(/\s+/);
          if (fields.length < 3) {
            continue;
          }
          if (fields[2] === "2") {
            oursBlobSha = fields[1];
          } else if (fields[2] === "3") {
            theirsBlobSha = fields[1];
          }
        }

        const getBlobSize = async blobSha => {
          if (!blobSha) {
            return "unknown";
          }
          try {
            const { stdout } = await execApi.getExecOutput("git", ["cat-file", "-s", blobSha]);
            return stdout.trim() || "unknown";
          } catch {
            return "unknown";
          }
        };

        const oursSize = await getBlobSize(oursBlobSha);
        const theirsSize = await getBlobSize(theirsBlobSha);
        core.warning(`Resolving add/add conflict for ${file}: ours ${oursBlobSha || "unknown"} (${oursSize} bytes), theirs ${theirsBlobSha || "unknown"} (${theirsSize} bytes); preferring patch version (--theirs)`);
      } catch (metadataError) {
        core.warning(`Resolving add/add conflict for ${file}; failed to read conflict blob metadata: ${getErrorMessage(metadataError)}. Preferring patch version (--theirs)`);
      }

      core.debug(`Checking out patch version for add/add conflict file: ${file}`);
      await execApi.exec("git", ["checkout", "--theirs", "--", file]);
      await execApi.exec("git", ["add", "--", file]);
    }
    await execApi.exec("git", ["am", "--continue"]);
    core.info("Patch applied successfully after resolving add/add conflict(s)");
    return { recovered: true, attempted: true };
  } catch (recoveryError) {
    core.debug(`Add/add recovery threw: ${getErrorMessage(recoveryError)}`);
    return { recovered: false, attempted: true, errorMessage: getErrorMessage(recoveryError) };
  }
}

/**
 * Resolves auto-merge enablement and merge method from the handler config.
 *
 * Supported values:
 *   - false / "false" / empty => disabled
 *   - true / "true"  => enabled with SQUASH as the default merge strategy
 *   - "squash" | "merge" | "rebase" => enabled with explicit strategy
 *   - any other value => disabled with a warning (fail-closed)
 *
 * @param {any} value
 * @returns {{ enabled: boolean, mergeMethod?: "SQUASH" | "MERGE" | "REBASE" }}
 */
function parseAutoMergeConfig(value) {
  const normalized = String(value ?? "")
    .trim()
    .toLowerCase();
  if (!normalized || normalized === "false") {
    return { enabled: false };
  }

  switch (normalized) {
    case "squash":
    case "true":
      return { enabled: true, mergeMethod: "SQUASH" };
    case "merge":
      return { enabled: true, mergeMethod: "MERGE" };
    case "rebase":
      return { enabled: true, mergeMethod: "REBASE" };
    default:
      core.warning(`Unrecognized auto-merge value "${value}". Expected true, false, "squash", "merge", or "rebase". Auto-merge will be disabled.`);
      return { enabled: false };
  }
}

/**
 * Apply a git bundle to a local branch without fetching directly into the branch ref.
 * Fetching directly into refs/heads/<branch> fails when that branch is currently checked out.
 *
 * @param {string} bundleFilePath - Path to the bundle file
 * @param {string} branchName - Target branch name
 * @param {string} originalAgentBranch - Original source branch name from the agent, if different
 * @param {{ exec: Function, getExecOutput: Function }} execApi - GitHub Actions exec API
 * @param {string} [baseBranch] - Base branch name (used for iterative shallow-clone deepening)
 * @returns {Promise<void>}
 */
async function applyBundleToBranch(bundleFilePath, branchName, originalAgentBranch, execApi, baseBranch) {
  let bundleBranchRef = `refs/heads/${originalAgentBranch || branchName}`;
  const bundleTargetRef = `refs/heads/${branchName}`;
  const bundleTempRef = createBundleTempRef(branchName);

  try {
    await ensureFullHistoryForBundle(execApi, {}, { baseRef: baseBranch, bundleFilePath });
    core.info(`Applying bundle ${bundleFilePath} to ${bundleTargetRef} using temp ref ${bundleTempRef} from ${bundleBranchRef}`);

    // Fetch from bundle into a temporary ref, then update the target branch.
    // bundleBranchRef is the source ref inside the bundle (typically refs/heads/<agent-branch>).
    // Use getExecOutput with ignoreReturnCode so we can read the actual stderr from git —
    // exec() only throws "The process '...' failed with exit code 1" which loses the
    // "lacks these prerequisite commits" text needed for the recovery path below.
    core.info(`Attempting bundle fetch from ${bundleBranchRef} into ${bundleTempRef}`);
    const initialBundleFetch = await execApi.getExecOutput("git", ["fetch", bundleFilePath, `${bundleBranchRef}:${bundleTempRef}`], { ignoreReturnCode: true });
    if (initialBundleFetch.exitCode !== 0) {
      const initialFetchErrorOutput = initialBundleFetch.stderr || `exit code ${initialBundleFetch.exitCode}`;

      // Recovery path for bundle prerequisite failures: fetch missing prerequisite
      // commit objects, then retry with the original bundle ref.
      // This handles the race where main advanced between agent-time and safe_outputs-time:
      // the bundle's base commit may not be reachable from a fetch-depth:1 shallow clone
      // (e.g. when the commit is on a ref not in the fetch refspec).
      const prerequisiteCommits = extractBundlePrerequisiteCommits(initialFetchErrorOutput);
      if (prerequisiteCommits.length > 0) {
        core.warning(`Bundle fetch with ${bundleBranchRef} failed due to ${prerequisiteCommits.length} missing prerequisite commit(s); fetching prerequisites from origin and retrying`);
        core.info(`Prerequisite commits: ${summarizeListForLog(prerequisiteCommits)}`);
        core.info(`Fetching ${prerequisiteCommits.length} prerequisite commit(s) from origin`);
        // Use --filter=blob:none only when the local repo is already shallow or sparse —
        // in a full clone we already have all blobs and must not convert the repo to a
        // partial clone (which would trigger lazy blob fetches on later operations).
        const useBlobFilter = await isShallowOrSparseCheckout(execApi);
        const prerequisiteFetchArgs = useBlobFilter ? ["fetch", "--filter=blob:none", "origin", ...prerequisiteCommits] : ["fetch", "origin", ...prerequisiteCommits];
        if (useBlobFilter) {
          core.info("Using --filter=blob:none for prerequisite fetch (shallow or sparse checkout detected)");
        }
        await execApi.exec("git", prerequisiteFetchArgs);
        core.info("Fetched prerequisite commits from origin successfully");
        try {
          core.info(`Retrying bundle fetch from ${bundleBranchRef} into ${bundleTempRef} after prerequisite recovery`);
          await execApi.exec("git", ["fetch", bundleFilePath, `${bundleBranchRef}:${bundleTempRef}`]);
          core.info("Bundle fetch retry succeeded after prerequisite recovery");
        } catch (retryError) {
          throw new Error(`Bundle fetch failed after fetching ${prerequisiteCommits.length} prerequisite commit(s): ${getErrorMessage(retryError)}`, { cause: retryError });
        }
      } else {
        // Fallback: resolve the source ref directly from the bundle contents.
        // Some agents may emit a JSONL branch name that differs from the ref embedded in the bundle.
        core.warning(`Bundle fetch with ${bundleBranchRef} failed: ${initialFetchErrorOutput}; resolving branch ref from bundle heads`);
        core.info(`Inspecting bundle heads from ${bundleFilePath}`);
        const { stdout: bundleHeadsOutput } = await execApi.getExecOutput("git", ["bundle", "list-heads", bundleFilePath]);
        const branchRefs = bundleHeadsOutput
          .split("\n")
          .map(line => line.trim().split(/\s+/)[1] || "")
          .filter(ref => /^refs\/heads\/[A-Za-z0-9._][A-Za-z0-9._/-]*$/.test(ref));
        core.info(`Bundle list-heads returned ${branchRefs.length} candidate branch ref(s): ${summarizeListForLog(branchRefs)}`);

        if (branchRefs.length === 1) {
          bundleBranchRef = branchRefs[0];
          core.info(`Resolved bundle source ref from list-heads: ${bundleBranchRef}`);
          core.info(`Fetching resolved bundle ref ${bundleBranchRef} into ${bundleTempRef}`);
          await execApi.exec("git", ["fetch", bundleFilePath, `${bundleBranchRef}:${bundleTempRef}`]);
        } else if (branchRefs.length === 0) {
          // Bundle contains only HEAD (no refs/heads/* entry). This happens when the
          // agent created the bundle with `git bundle create <file> HEAD` rather than
          // including a named branch ref.  Fetch using the HEAD refspec directly so
          // the bundle objects become accessible, then point the temp ref at them.
          const headLine = bundleHeadsOutput
            .split("\n")
            .map(line => line.trim())
            .find(line => /^[0-9a-f]{40}\s+HEAD$/.test(line));
          if (headLine) {
            core.info(`Bundle has no refs/heads entries; fetching HEAD directly into ${bundleTempRef}`);
            // Use getExecOutput with ignoreReturnCode so we can read actual stderr
            // and perform prerequisite recovery before failing — same pattern as the
            // initial bundle fetch above.  When the agent ran on a non-default branch
            // the bundle prerequisite is that branch tip, which isn't reachable from
            // the local main-only checkout, so a bare exec() would throw and lose the
            // "lacks these prerequisite commits" text needed for recovery.
            const headBundleFetch = await execApi.getExecOutput("git", ["fetch", bundleFilePath, `HEAD:${bundleTempRef}`], { ignoreReturnCode: true });
            if (headBundleFetch.exitCode !== 0) {
              const headFetchErrorOutput = headBundleFetch.stderr || `exit code ${headBundleFetch.exitCode}`;
              const headPrereqCommits = extractBundlePrerequisiteCommits(headFetchErrorOutput);
              if (headPrereqCommits.length > 0) {
                core.warning(`HEAD bundle fetch failed due to ${headPrereqCommits.length} missing prerequisite commit(s); fetching prerequisites from origin and retrying`);
                core.info(`Prerequisite commits: ${summarizeListForLog(headPrereqCommits)}`);
                const useBlobFilter = await isShallowOrSparseCheckout(execApi);
                const headPrereqFetchArgs = useBlobFilter ? ["fetch", "--filter=blob:none", "origin", ...headPrereqCommits] : ["fetch", "origin", ...headPrereqCommits];
                if (useBlobFilter) {
                  core.info("Using --filter=blob:none for prerequisite fetch (shallow or sparse checkout detected)");
                }
                await execApi.exec("git", headPrereqFetchArgs);
                core.info("Fetched HEAD bundle prerequisite commits from origin successfully");
                try {
                  core.info(`Retrying HEAD bundle fetch into ${bundleTempRef} after prerequisite recovery`);
                  await execApi.exec("git", ["fetch", bundleFilePath, `HEAD:${bundleTempRef}`]);
                  core.info("HEAD bundle fetch retry succeeded after prerequisite recovery");
                } catch (retryError) {
                  const retryErrorMessage = getErrorMessage(retryError);
                  throw new Error(`HEAD bundle fetch failed after fetching ${headPrereqCommits.length} prerequisite commit(s): ${retryErrorMessage}`, {
                    cause: retryError,
                  });
                }
              } else {
                throw new Error(`Failed to apply HEAD-only bundle: ${headFetchErrorOutput}`);
              }
            }
          } else {
            throw new Error(`Failed to resolve bundle branch ref from list-heads: bundle contains no refs/heads entries and no HEAD ref`);
          }
        } else {
          throw new Error(`Failed to resolve bundle branch ref from list-heads: expected exactly 1 refs/heads entry, found ${branchRefs.length}`);
        }
      }
    }
    core.info(`Fetched bundle to ${bundleTempRef}`);
    await execApi.exec("git", ["update-ref", bundleTargetRef, bundleTempRef]);
    core.info(`Created local branch ${branchName} from bundle`);
    await execApi.exec("git", ["checkout", branchName]);
    // Ensure the working tree matches the new HEAD in case checkout left any index/working tree drift.
    await execApi.exec("git", ["reset", "--hard"]);
    core.info(`Checked out branch ${branchName} from bundle`);
  } finally {
    try {
      await execApi.exec("git", ["update-ref", "-d", bundleTempRef]);
    } catch (cleanupError) {
      // Non-fatal cleanup
      core.warning(`Non-fatal cleanup: failed to delete temporary bundle ref ${bundleTempRef}: ${getErrorMessage(cleanupError)}`);
    }
  }
}

/**
 * Rewrites the current branch to a single non-merge commit relative to the bundle's
 * actual base commit (the prerequisite SHA recorded in the bundle). Falls back to
 * origin/<baseBranch> when the bundle prerequisite cannot be determined.
 *
 * Using the bundle's prerequisite SHA instead of the current origin/<baseBranch> tip
 * ensures the linearized commit only contains the agent's actual changes and does not
 * revert or absorb commits that were added to the base branch after the agent's checkout.
 *
 * @param {string} baseBranch
 * @param {{ exec: Function, getExecOutput: Function }} execApi
 * @param {string} [bundleFilePath] - Optional path to the bundle file; used to extract the
 *   precise base commit the agent worked from.
 * @param {{ excludedFiles?: string[] }} [options]
 * @returns {Promise<void>}
 */
async function rewriteBundleBranchAsSingleCommit(baseBranch, execApi, bundleFilePath, options = {}) {
  const fallbackBaseRef = `origin/${baseBranch}`;
  let baseRef = fallbackBaseRef;

  if (bundleFilePath) {
    try {
      const prereqs = await getBundlePrerequisites(execApi, bundleFilePath);
      if (prereqs.length === 1) {
        // Guard: verify the prerequisite SHA is accessible in the local repository
        // before using it as a linearization base. In a shallow clone the commit
        // may not have been fetched, causing `git reset --soft <sha>` to fail.
        // We check reachability here — before synthesizing any commit — so we can
        // fall back cleanly rather than letting linearizeRangeAsCommit abort mid-run.
        const prereqSha = prereqs[0];
        try {
          const { exitCode } = await execApi.getExecOutput("git", ["cat-file", "-e", `${prereqSha}^{commit}`], { ignoreReturnCode: true, silent: true });
          if (exitCode === 0) {
            baseRef = prereqSha;
            core.info(`Using bundle prerequisite commit ${baseRef} as linearization base (avoids including base-branch drift)`);
          } else {
            core.info(`Bundle prerequisite ${prereqSha} not accessible locally; falling back to ${fallbackBaseRef} as linearization base`);
          }
        } catch {
          core.info(`Could not verify bundle prerequisite accessibility; falling back to ${fallbackBaseRef} as linearization base`);
        }
      } else if (prereqs.length > 1) {
        core.info(`Bundle has ${prereqs.length} prerequisite commits; falling back to ${fallbackBaseRef} as linearization base`);
      } else {
        core.info(`Bundle declares no prerequisites; falling back to ${fallbackBaseRef} as linearization base`);
      }
    } catch (prereqError) {
      core.warning(`Could not extract bundle prerequisites: ${getErrorMessage(prereqError)}; falling back to ${fallbackBaseRef}`);
    }
  }

  let commitHeadline = "Apply bundled create_pull_request changes";
  try {
    const { stdout: headlineOut } = await execApi.getExecOutput("git", ["log", "-1", "--format=%s", "HEAD"]);
    if (headlineOut.trim()) {
      commitHeadline = headlineOut.trim();
    }
  } catch {
    // Non-fatal: use default commit headline.
  }

  core.warning(`Rewriting bundled commits to a single linear commit for signed push compatibility (base: ${baseRef})`);
  const newHead = await linearizeRangeAsCommit(baseRef, commitHeadline, execApi, {
    excludedFiles: options.excludedFiles,
    rebaseOnto: fallbackBaseRef,
  });
  core.info(`Bundle rewrite completed (new HEAD: ${newHead})`);
}

// NOTE: isLabelTransientError, LABEL_MAX_RETRIES, LABEL_INITIAL_DELAY_MS, LABEL_MAX_DELAY_MS,
// parseAllowedBaseBranches, isBaseBranchAllowed, parseStringListConfig, mergeFallbackIssueLabels,
// sanitizeFallbackAssignees, neutralizeClosingKeywordsForIssueBody, generatePatchPreview,
// buildManifestProtectionCreatePrUrl, and renderManifestProtectionFallbackBody
// are imported from create_pull_request_helpers.cjs above.

/**
 * Creates a fallback GitHub issue, retrying on rate-limit and other transient errors
 * (with exponential back-off) and retrying without assignees if the API rejects them.
 * This ensures fallback issue creation remains reliable even if an assignee username
 * is invalid, the repository does not have that collaborator, or the installation token
 * quota is temporarily exhausted.
 * @param {any} githubClient - Authenticated GitHub client
 * @param {{owner: string, repo: string}} repoParts - Repository owner and name
 * @param {string} title - Issue title
 * @param {string} body - Issue body
 * @param {string[]} labels - Issue labels
 * @param {string[] | null} assignees - Sanitized assignees (null = omit field)
 * @returns {Promise<{data: any, issueRepoParts: {owner: string, repo: string}}>}
 */
async function createFallbackIssue(githubClient, repoParts, title, body, labels, assignees) {
  const payload = {
    owner: repoParts.owner,
    repo: repoParts.repo,
    title,
    body,
    labels,
    ...(assignees && assignees.length > 0 && { assignees }),
  };

  const parseRepo = slug => {
    const s = (slug || "").trim();
    if (!s.includes("/")) return null;
    const [owner, repo] = s.split("/");
    return owner && repo ? { owner, repo } : null;
  };

  // innerCreate is called recursively so that both the 422-assignee and 410-redirect
  // paths go through the full recovery loop. This ensures, for example, that a 422
  // in the alternate repo (after a 410 redirect) is still handled by the assignee guard.
  // triedOwnerRepos is declared here (inside createFallbackIssue) so each call to the
  // function gets its own fresh Set. It is intentionally shared across withRetry
  // attempts within the same call so that repos that already returned 410 are not
  // retried again after a transient error (e.g. rate limit) on a subsequent candidate.
  const triedOwnerRepos = new Set();
  const innerCreate = async () => {
    try {
      const response = await githubClient.rest.issues.create(payload);
      return { ...response, issueRepoParts: { owner: payload.owner, repo: payload.repo } };
    } catch (error) {
      const status = typeof error === "object" && error !== null && "status" in error ? error.status : undefined;
      const message = getErrorMessage(error).toLowerCase();
      const isAssigneeError = status === 422 && (message.includes("assignee") || message.includes("assignees") || message.includes("unprocessable"));
      if (isAssigneeError && payload.assignees && payload.assignees.length > 0) {
        const removedAssignees = payload.assignees.join(", ");
        core.warning(`Fallback issue creation failed due to assignee error, retrying without assignees: ${getErrorMessage(error)}`);
        // Mutate payload in-place so that any subsequent withRetry attempts also
        // omit assignees and do not re-trigger the same 422 path.
        delete payload.assignees;
        payload.body = `${payload.body}\n\n> [!NOTE]\n> Assignees (${removedAssignees}) could not be set on this issue due to an API error.`;
        return await innerCreate();
      }

      // Handle issues-disabled (410 Gone) by redirecting to an alternative repo.
      // Priority: GH_AW_FAILURE_ISSUE_REPO → GITHUB_REPOSITORY. Pick the first candidate
      // that has not already been attempted (tracked via triedOwnerRepos) to prevent
      // infinite back-and-forth when multiple candidates also have issues disabled.
      // Mutate payload in-place so any subsequent attempts use the new target.
      if (status === 410) {
        const originalTarget = `${payload.owner}/${payload.repo}`;
        triedOwnerRepos.add(originalTarget.toLowerCase());
        const failureRepo = parseRepo(process.env.GH_AW_FAILURE_ISSUE_REPO || "");
        const workflowRepo = parseRepo(process.env.GITHUB_REPOSITORY || "");
        const alt = [failureRepo, workflowRepo].find(r => r !== null && !triedOwnerRepos.has(`${r.owner}/${r.repo}`.toLowerCase()));

        if (alt) {
          core.warning(`Issues are disabled in ${originalTarget}; retrying fallback issue creation in ${alt.owner}/${alt.repo}`);
          payload.owner = alt.owner;
          payload.repo = alt.repo;
          return await innerCreate();
        }

        core.warning(`Issues are disabled in ${originalTarget} and no alternate repo is available to create the fallback issue`);
      }

      throw error;
    }
  };

  return withRetry(innerCreate, RATE_LIMIT_RETRY_CONFIG, `create fallback issue in ${repoParts.owner}/${repoParts.repo}`);
}

/**
 * Maximum limits for pull request parameters to prevent resource exhaustion.
 * These limits align with GitHub's API constraints and security best practices.
 */
/** @type {number} Default maximum number of unique files allowed per pull request.
 * Can be overridden via the `max-patch-files` safe-outputs config option. */
const MAX_FILES = 100;

/**
 * Parses a value as a positive integer, returning null for invalid/non-positive input.
 * @param {unknown} value
 * @returns {number | null}
 */
function parsePositiveInteger(value) {
  if (typeof value !== "string" && typeof value !== "number") {
    return null;
  }
  const parsed = Number.parseInt(String(value), 10);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

/**
 * Parses one `diff --git` header line and returns the preferred file path key.
 *
 * @param {string} headerLine
 * @returns {string|null}
 */
function parseDiffGitHeader(headerLine) {
  const parsed = parseDiffGitHeaderPaths(headerLine);
  return parsed.newPath || parsed.oldPath || null;
}

/**
 * Counts the number of unique file paths touched by a git patch.
 *
 * `git format-patch` emits one `diff --git` header per (commit, file), so the
 * same file modified across multiple commits will appear multiple times. The
 * file-count safety limit counts unique files (i.e. how many distinct files
 * this push touches), not raw header occurrences.
 *
 * Headers whose paths cannot be parsed contribute one *synthetic* entry each
 * to the unique-file set, so a malformed or quoted-with-escapes header line
 * can never silently bypass the limit (we conservatively over-count rather
 * than under-count when in doubt).
 *
 * @param {string} patchContent - Patch content to inspect (may be empty)
 * @returns {number} Number of unique file paths referenced in the patch
 */
function countUniquePatchFiles(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return 0;
  }
  const files = new Set();
  const entries = extractDiffGitHeaderEntries(patchContent);
  let unparseableIdx = 0;
  for (const entry of entries) {
    const path = entry.newPath || entry.oldPath;
    if (path) {
      files.add(path);
    } else {
      files.add(`__unparseable_header_${entry.headerIndex}_${unparseableIdx++}`);
    }
  }
  return files.size;
}

/**
 * Enforces maximum limits on pull request parameters to prevent resource exhaustion attacks.
 * Per Safe Outputs specification requirement SEC-003, limits must be enforced before API calls.
 *
 * The file-count check measures the number of *unique* files in the patch (not
 * the number of `diff --git` headers, which can be inflated when the patch
 * contains multiple commits touching the same file).
 *
 * @param {string} patchContent - Patch content to validate
 * @param {number} [maxFiles=MAX_FILES] - Maximum number of unique files allowed
 * @throws {Error} When any limit is exceeded, with error code E003 and details
 */
function enforcePullRequestLimits(patchContent, maxFiles = MAX_FILES) {
  if (!patchContent || !patchContent.trim()) {
    return;
  }

  const limit = Number.isFinite(maxFiles) && maxFiles > 0 ? maxFiles : MAX_FILES;
  const fileCount = countUniquePatchFiles(patchContent);

  // Check file count - max limit exceeded check
  if (fileCount > limit) {
    throw new Error(
      `E003: Cannot create pull request with more than ${limit} files (received ${fileCount}). ` +
        `To increase the limit, set \`max-patch-files: ${fileCount}\` (or higher) under ` +
        `\`safe-outputs.create-pull-request\` in your workflow frontmatter.`
    );
  }
}

// NOTE: generatePatchPreview is imported from create_pull_request_helpers.cjs above.

/**
 * Check whether the remote branch already exists and, if so, either reuse it
 * (when preserve-branch-name and recreate-ref are enabled, by force-deleting
 * the remote ref so the subsequent push recreates it from the local HEAD) or rename
 * the local branch by appending a random hex suffix.
 *
 * The "force-delete then recreate" semantic is gated behind `recreate-ref`
 * because the existing remote branch may have diverged from the local HEAD
 * (e.g. a long-lived branch whose previous PR was merged and is now behind
 * the base branch). Deleting the ref first lets `pushSignedCommits` recreate
 * the branch at the local commit's parent OID and replay only the local
 * commits via the GraphQL `createCommitOnBranch` mutation, which is what
 * users intend by enabling `recreate-ref` on a reusable branch.
 *
 * When `preserve-branch-name: true` but `recreate-ref: false` (default),
 * an existing remote branch results in an error so the caller falls back to
 * the configured fallback (e.g. opening an issue) rather than silently
 * destroying the remote ref.
 *
 * @param {string} branchName - Current local branch name.
 * @param {boolean} preserveBranchName - Whether preserve-branch-name is enabled.
 * @param {object} [options] - Additional options.
 * @param {boolean} [options.recreateRef] - Whether recreate-ref is enabled.
 *   Only meaningful when preserveBranchName is true.
 * @param {any} [options.githubClient] - Authenticated Octokit client used to delete the
 *   existing remote ref when recreate-ref is enabled.
 * @param {string} [options.owner] - Repository owner for the deleteRef call.
 * @param {string} [options.repo] - Repository name for the deleteRef call.
 * @param {string} [options.remoteTarget] - Remote name or URL used for remote branch existence checks.
 * @param {string} [options.remoteToken] - Optional token used for authenticated remote branch checks.
 * @param {string} [options.cwd] - Optional working directory for git operations; scopes git config overrides to the correct checkout.
 * @returns {Promise<string>} The (possibly renamed) branch name to use going forward.
 */
async function handleRemoteBranchCollision(branchName, preserveBranchName, options = {}) {
  const cwd = options.cwd;
  let remoteBranchExists = false;
  try {
    const remoteTarget = options.remoteTarget || "origin";
    const checkRemoteBranch = async () => exec.getExecOutput("git", ["ls-remote", "--heads", remoteTarget, branchName], cwd ? { cwd } : {});
    let checkResult;
    if (options.remoteToken) {
      checkResult = await withGitHubHostToken(options.remoteToken, checkRemoteBranch, cwd);
    } else {
      checkResult = await checkRemoteBranch();
    }
    const { stdout } = checkResult;
    if (stdout.trim()) {
      remoteBranchExists = true;
    }
  } catch (checkError) {
    core.info(`Remote branch check failed (non-fatal): ${getErrorMessage(checkError)}`);
  }

  if (!remoteBranchExists) {
    return branchName;
  }

  if (preserveBranchName) {
    const { recreateRef, githubClient, owner, repo } = options;
    if (!recreateRef) {
      // preserve-branch-name asked us to keep the exact branch name, but
      // recreate-ref is not enabled, so we cannot silently destroy the
      // existing remote ref. Surface an error so the caller falls back to the
      // configured fallback (e.g. opening an issue).
      throw new Error(
        `Remote branch "${branchName}" already exists and preserve-branch-name is enabled. ` + `Set recreate-ref: true to force-delete and recreate the remote ref, or disable ` + `preserve-branch-name to allow renaming the branch.`
      );
    }
    // Reuse the existing branch by deleting the remote ref so the subsequent
    // push recreates it from the local HEAD (force-push semantics). This is the
    // intended behavior when recreate-ref is enabled for long-lived
    // reusable branches whose previous PR was merged.
    if (!githubClient || !owner || !repo) {
      throw new Error(
        `Remote branch "${branchName}" already exists and recreate-ref is enabled, ` +
          `but no GitHub client was provided to delete the existing remote ref. This is an ` +
          `internal error: the caller must pass githubClient, owner, and repo to reuse the branch.`
      );
    }
    core.warning(`Remote branch ${branchName} already exists - reusing it (recreate-ref enabled, force-deleting remote ref)`);
    let deleteBlocked = false;
    try {
      await githubClient.rest.git.deleteRef({ owner, repo, ref: `heads/${branchName}` });
      core.info(`Deleted remote branch ${branchName} to reuse it`);
    } catch (deleteError) {
      /** @type {any} */
      const err = deleteError;
      const status = err && typeof err === "object" ? err.status : undefined;
      const message = err && typeof err === "object" ? String(err.message || "") : "";
      // 422 "Reference does not exist" can happen if the branch was deleted concurrently;
      // treat that as success and continue.
      if (status === 422 && /Reference does not exist/i.test(message)) {
        core.info(`Remote branch ${branchName} was already deleted concurrently; continuing`);
      } else if (status === 422 && (/Cannot delete this branch/i.test(message) || /Repository rule violations/i.test(message))) {
        // A branch protection rule (e.g. a ruleset that blocks deletion) prevented
        // the delete. Fall back gracefully by appending a random suffix to the branch
        // name rather than failing hard, so a PR can still be created.
        core.warning(`Remote branch "${branchName}" cannot be deleted due to branch protection rules (recreate-ref blocked). ` + `Falling back to rename with random suffix.`);
        deleteBlocked = true;
      } else {
        throw new Error(`Failed to delete existing remote branch "${branchName}" for reuse with recreate-ref: ${message || getErrorMessage(err)}`, { cause: err });
      }
    }
    if (!deleteBlocked) {
      return branchName;
    }
  }

  core.warning(`Remote branch ${branchName} already exists - appending random suffix`);
  const extraHex = crypto.randomBytes(4).toString("hex");
  const oldBranch = branchName;
  const renamedBranch = `${branchName}-${extraHex}`;
  // Rename local branch
  await exec.exec("git", ["branch", "-m", oldBranch, renamedBranch], cwd ? { cwd } : {});
  core.info(`Renamed branch to ${renamedBranch}`);
  return renamedBranch;
}

/**
 * Main handler factory for create_pull_request
 * Returns a message handler function that processes individual create_pull_request messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const rawBranchPrefix = config.branch_prefix || "";
  const normalizedBranchPrefix = normalizeBranchName(rawBranchPrefix);
  if (rawBranchPrefix && normalizedBranchPrefix !== rawBranchPrefix) {
    const branchPrefixWarning = [
      `Branch prefix "${rawBranchPrefix}" contains characters that are invalid in a git ref.`,
      `Using normalized prefix: "${normalizedBranchPrefix}".`,
      "Update branch-prefix in the workflow configuration to avoid this warning.",
    ].join(" ");
    core.warning(branchPrefixWarning);
  }
  const branchPrefix = normalizedBranchPrefix;
  const titlePrefix = config.title_prefix || "";
  const envLabels = parseStringListConfig(config.labels);
  const configFallbackLabels = parseStringListConfig(config.fallback_labels);
  const configReviewers = parseStringListConfig(config.reviewers);
  const configTeamReviewers = parseStringListConfig(config.team_reviewers);
  const rawAssignees = parseStringListConfig(config.assignees);
  const hasCopilotInAssignees = rawAssignees.some(a => a.toLowerCase() === "copilot");
  const configAssignees = sanitizeFallbackAssignees(rawAssignees);
  const draftDefault = parseBoolTemplatable(config.draft, true);
  const ifNoChanges = config.if_no_changes || "warn";
  const allowEmpty = parseBoolTemplatable(config.allow_empty, false);
  const { enabled: autoMerge, mergeMethod: autoMergeMethod } = parseAutoMergeConfig(config.auto_merge);
  const preserveBranchName = config.preserve_branch_name === true;
  const recreateRef = config.recreate_ref === true;
  const signedCommits = config.signed_commits !== false;
  const expiresHours = config.expires ? parseInt(String(config.expires), 10) : 0;
  const maxCount = config.max || 1; // PRs are typically limited to 1
  const maxSizeKb = parsePositiveInteger(config.max_patch_size) ?? 4096;
  const maxFiles = parsePositiveInteger(config.max_patch_files) ?? MAX_FILES;
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const allowedBaseBranches = parseAllowedBaseBranches(config.allowed_base_branches);
  // Stacked pull requests (a pull request whose base branch is another pull request branch) are
  // enabled by default. They can be disabled with `stacked: false` for GitHub Enterprise Server
  // and other instances that do not support the feature.
  const stackedPullRequestsEnabled = isStackedEnabled(config);
  // Tracks the pull requests created so far in this run so later messages can stack on top of them.
  const stackTracker = createStackTracker();
  const githubClient = await createAuthenticatedGitHubClient(config);
  const maxMentions = parseIntTemplatable(config.mentions?.max, 50);
  let allowedMentionAliases = [];
  if (Array.isArray(config.allowedMentionAliases)) {
    allowedMentionAliases = config.allowedMentionAliases;
  } else if (config.mentions != null) {
    allowedMentionAliases = await resolveAllowedMentionsFromPayload(context, githubClient, core, config.mentions);
  }

  // Check if copilot assignment is enabled for fallback issues
  const assignCopilot = process.env.GH_AW_ASSIGN_COPILOT === "true";

  // Lazily-initialised client for copilot assignment (only allocated when needed).
  // Uses GH_AW_ASSIGN_TO_AGENT_TOKEN (agent token preference chain) when available,
  // otherwise falls back to the step-level github object.
  /** @type {Object|null} */
  let copilotClient = null;

  /**
   * Assigns copilot to a fallback issue using agent helpers, if copilot was requested
   * in the assignees config and the GH_AW_ASSIGN_COPILOT env var is set.
   * A no-op when either condition is false. The copilotClient is initialised lazily
   * on the first call and reused for subsequent issues.
   * @param {string} owner - Repository owner
   * @param {string} repo - Repository name
   * @param {number} issueNumber - Fallback issue number
   */
  async function assignCopilotToFallbackIssueIfEnabled(owner, repo, issueNumber) {
    if (!hasCopilotInAssignees || !assignCopilot) return;
    if (!copilotClient) {
      copilotClient = await createCopilotAssignmentClient(config);
    }
    core.info(`Assigning copilot coding agent to fallback issue #${issueNumber} in ${owner}/${repo}...`);
    try {
      const agentId = await findAgent(owner, repo, "copilot", issueNumber, copilotClient);
      if (!agentId) {
        core.warning(`copilot coding agent is not available for ${owner}/${repo}`);
        return;
      }
      const issueDetails = await getIssueDetails(owner, repo, issueNumber, copilotClient);
      if (!issueDetails) {
        core.warning(`Failed to get issue details for copilot assignment of fallback issue #${issueNumber}`);
        return;
      }
      if (issueDetails.currentAssignees.some(a => a.id === agentId)) {
        core.info(`copilot is already assigned to fallback issue #${issueNumber}`);
        return;
      }
      const assigned = await assignAgentToIssue(
        issueDetails.issueId,
        agentId,
        issueDetails.currentAssignees,
        "copilot",
        null, // allowedAgents — not restricted for fallback issues
        null, // model — not applicable
        null, // customAgent — not applicable
        null, // customInstructions — not applicable
        null, // baseBranch — not applicable
        copilotClient,
        issueDetails.taskContext
      );
      if (assigned) {
        core.info(`Successfully assigned copilot coding agent to fallback issue #${issueNumber}`);
      } else {
        core.warning(`Failed to assign copilot to fallback issue #${issueNumber}`);
      }
    } catch (error) {
      core.warning(`Failed to assign copilot to fallback issue #${issueNumber}: ${getErrorMessage(error)}`);
    }
  }

  // Base branch from config (if set) - validated at factory level if explicit
  // Dynamic base branch resolution happens per-message after resolving the actual target repo
  const configBaseBranch = config.base_branch || null;
  const configuredHeadRepo = typeof config["head-repo"] === "string" ? config["head-repo"].trim() : "";
  const headGitHubToken = typeof config["head-github-token"] === "string" ? config["head-github-token"].trim() : "";

  // SECURITY: If base branch is explicitly configured, validate it at factory level
  if (configBaseBranch) {
    const normalizedConfigBase = normalizeBranchName(configBaseBranch);
    if (!normalizedConfigBase) {
      throw new Error(`Invalid baseBranch: sanitization resulted in empty string (original: "${configBaseBranch}")`);
    }
    if (configBaseBranch !== normalizedConfigBase) {
      throw new Error(`Invalid baseBranch: contains invalid characters (original: "${configBaseBranch}", normalized: "${normalizedConfigBase}")`);
    }
  }

  const includeFooter = parseBoolTemplatable(config.footer, true);
  const fallbackAsIssue = config.fallback_as_issue !== false; // Default to true (fallback enabled)
  const autoCloseIssue = parseBoolTemplatable(config.auto_close_issue, true); // Default to true (auto-close enabled)
  const closeOlderPullRequestsEnabled = parseBoolTemplatable(config.close_older_pull_requests, false);
  const rawCloseOlderKey = config.close_older_key ? String(config.close_older_key) : "";
  const closeOlderKey = rawCloseOlderKey ? normalizeCloseOlderKey(rawCloseOlderKey) : "";

  // Environment validation - fail early if required variables are missing
  const workflowId = process.env.GH_AW_WORKFLOW_ID;
  if (!workflowId) {
    throw new Error("GH_AW_WORKFLOW_ID environment variable is required");
  }

  const callerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID || "";

  // Extract triggering issue number from context (for auto-linking PRs to issues)
  const triggeringIssueNumber = typeof context !== "undefined" && context.payload?.issue?.number && !context.payload?.issue?.pull_request ? context.payload.issue.number : undefined;

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  core.info(`Base branch: ${configBaseBranch || "(dynamic - resolved per target repo)"}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (configuredHeadRepo) {
    core.info(`Configured head repo: ${configuredHeadRepo}`);
  }
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }
  if (allowedBaseBranches.size > 0) {
    core.info(`Allowed base branches: ${Array.from(allowedBaseBranches).join(", ")}`);
  }
  if (!stackedPullRequestsEnabled) {
    core.info("Stacked pull requests are disabled (stacked: false)");
  }
  if (envLabels.length > 0) {
    core.info(`Default labels: ${envLabels.join(", ")}`);
  }
  if (configFallbackLabels.length > 0) {
    core.info(`Configured fallback issue labels: ${configFallbackLabels.join(", ")}`);
  }
  if (configReviewers.length > 0) {
    core.info(`Configured reviewers: ${configReviewers.join(", ")}`);
  }
  if (configTeamReviewers.length > 0) {
    core.info(`Configured team reviewers: ${configTeamReviewers.join(", ")}`);
  }
  if (configAssignees && configAssignees.length > 0) {
    core.info(`Configured assignees (for pull request and fallback issues): ${configAssignees.join(", ")}`);
  }
  if (titlePrefix) {
    core.info(`Title prefix: ${titlePrefix}`);
  }
  core.info(`Draft default: ${draftDefault}`);
  core.info(`If no changes: ${ifNoChanges}`);
  core.info(`Allow empty: ${allowEmpty}`);
  core.info(`Auto-merge: ${autoMerge}`);
  core.info(`Signed commits: ${signedCommits}`);
  if (expiresHours > 0) {
    core.info(`Pull requests expire after: ${expiresHours} hours`);
  }
  if (closeOlderPullRequestsEnabled) {
    core.info(`Close older pull requests enabled: older PRs with same workflow-id marker will be closed`);
    if (rawCloseOlderKey) {
      core.info(`  Using explicit close-older-key: "${closeOlderKey}"`);
    }
  }
  core.info(`Max count: ${maxCount}`);
  core.info(`Max patch size: ${maxSizeKb} KB`);
  core.info(`Max patch files: ${maxFiles}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Multi-repo support: checkout mapping from compile-time checkout: configs.
  // When target-repo: "*" is configured and repos are checked out into subdirectories,
  // the checkout_mapping tells us where each repo lives on disk.
  const checkoutMapping = config.checkout_mapping || null;

  // Create checkout manager for multi-repo support (fallback when no checkout_mapping)
  // Token is available via GITHUB_TOKEN environment variable (set by the workflow job)
  const checkoutToken = process.env.GITHUB_TOKEN;
  const checkoutManager = checkoutToken ? createCheckoutManager(checkoutToken, { defaultBaseBranch: configBaseBranch }) : null;

  // Log multi-repo support status
  if (checkoutMapping) {
    core.info(`Multi-repo support enabled via checkout mapping: ${Object.keys(checkoutMapping).length} repo(s) mapped`);
  } else if (allowedRepos.size > 0 && checkoutManager) {
    core.info(`Multi-repo support enabled: can switch between repos in allowed-repos list`);
  } else if (allowedRepos.size > 0 && !checkoutManager) {
    core.warning(`Multi-repo support disabled: GITHUB_TOKEN not available for dynamic checkout`);
  }

  /**
   * Message handler function that processes a single create_pull_request message
   * @param {Object} message - The create_pull_request message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status and PR details
   */
  return async function handleCreatePullRequest(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping create_pull_request: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const pullRequestItem = message;

    const tempIdResult = getOrGenerateTemporaryId(pullRequestItem, "pull request");
    if (tempIdResult.error) {
      core.warning(`Skipping create_pull_request: ${tempIdResult.error}`);
      return { success: false, error: tempIdResult.error };
    }
    const temporaryId = tempIdResult.temporaryId;

    core.info(`Processing create_pull_request: title=${pullRequestItem.title || "No title"}, bodyLength=${pullRequestItem.body?.length || 0}`);

    // Determine the patch and bundle file paths. The MCP server sets these on
    // the entry it writes, but the validation step strips them as a defense
    // against agent-forged values. Recover them by re-deriving from `branch`.
    const transportPaths = resolveTransportPaths(pullRequestItem, defaultTargetRepo);
    const patchFilePath = transportPaths.patchPath;
    core.info(`Patch file path: ${patchFilePath || "(not set)"}`);

    // Determine the bundle file path from the message (set when patch-format: bundle is configured)
    const bundleFilePath = transportPaths.bundlePath;
    if (bundleFilePath) {
      core.info(`Bundle file path: ${bundleFilePath}`);
    }
    // Resolve and validate target repository
    const repoResult = resolveAndValidateRepo(pullRequestItem, defaultTargetRepo, allowedRepos, "pull request");
    if (!repoResult.success) {
      core.warning(`Skipping pull request: ${repoResult.error}`);
      return {
        success: false,
        error: repoResult.error,
      };
    }
    const { repo: itemRepo, repoParts } = repoResult;
    core.info(`Target repository: ${itemRepo}`);
    let pushRepo = itemRepo;
    let pushRepoParts = repoParts;
    let pushGithubClient = githubClient;
    if (configuredHeadRepo) {
      const headRepoResult = resolveAndValidateRepo({ repo: configuredHeadRepo }, itemRepo, allowedRepos, "pull request head repository");
      if (!headRepoResult.success) {
        return { success: false, error: headRepoResult.error };
      }
      pushRepo = headRepoResult.repo;
      pushRepoParts = headRepoResult.repoParts;
      if (headGitHubToken && typeof global.getOctokit === "function") {
        pushGithubClient = global.getOctokit(headGitHubToken);
      }
      core.info(`Resolved head repository: ${pushRepo}`);
    }
    const pushRemoteUrl = pushRepo.toLowerCase() === itemRepo.toLowerCase() ? "" : `${(process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "")}/${pushRepo}.git`;
    const getPullRequestHeadRef = branch => (pushRepo.toLowerCase() === itemRepo.toLowerCase() ? branch : `${pushRepoParts.owner}:${branch}`);

    // Resolve base branch for this target repository
    // Use config value if set, otherwise resolve dynamically for the specific target repo
    // Dynamic resolution is needed for issue_comment events on PRs where the base branch
    // is not available in GitHub Actions expressions and requires an API call
    // NOTE: Must be resolved before checkout so cross-repo checkout uses the correct branch
    let baseBranch = configBaseBranch || (await getBaseBranch(repoParts));
    const defaultBaseBranch = baseBranch;

    // Stacked pull request metadata declared by the agent (position/root/dependencies).
    const stackMetadata = parseStackMetadata(pullRequestItem);
    // Pull request created earlier in this run that this pull request stacks on top of.
    /** @type {import("./stacked_pull_requests.cjs").StackEntry | null} */
    let stackBaseEntry = null;

    // Optional agent-provided base branch override.
    // The default base branch is always implicitly allowed even without allowed_base_branches.
    // Overriding to a different branch requires allowed_base_branches to be configured, unless the
    // base branch belongs to a pull request created earlier in this same run (a stacked pull request).
    if (typeof pullRequestItem.base === "string" && pullRequestItem.base.trim() !== "") {
      const requestedBaseBranchRaw = pullRequestItem.base.trim();
      const requestedBaseBranchForLog = JSON.stringify(requestedBaseBranchRaw);
      core.info(`Base branch override requested: ${requestedBaseBranchForLog}`);
      if (requestedBaseBranchRaw === baseBranch && allowedBaseBranches.size === 0) {
        // The agent explicitly specified the current base branch with no allowlist configured —
        // this is a no-op, not a true override, so no allowlist check is needed.
        core.info(`Base branch ${requestedBaseBranchForLog} matches the default base branch, no override needed`);
      } else {
        // A base branch that differs from the default base branch creates a stacked pull request.
        const isStackedRequest = requestedBaseBranchRaw !== defaultBaseBranch;
        if (isStackedRequest && !stackedPullRequestsEnabled) {
          core.warning(`Rejecting base branch override ${requestedBaseBranchForLog}: stacked pull requests are disabled`);
          return { success: false, error: stackedDisabledError(requestedBaseBranchRaw, defaultBaseBranch) };
        }

        // Branch created by an earlier pull request in this run: the branch name may have been
        // salted/prefixed, so resolve it to the branch that was actually pushed. No allowlist check
        // is required because the branch was created by this same run.
        const previousStackEntry = stackTracker.get(requestedBaseBranchRaw, itemRepo);
        if (isStackedRequest && previousStackEntry) {
          stackBaseEntry = previousStackEntry;
          baseBranch = previousStackEntry.branch;
          core.info(`Stacking on pull request #${previousStackEntry.number} (base branch ${baseBranch})`);
        } else if (allowedBaseBranches.size === 0) {
          core.warning(`Rejecting base branch override ${requestedBaseBranchForLog}: allowed-base-branches is not configured`);
          return {
            success: false,
            error: "Base branch override is not allowed. Configure safe-outputs.create-pull-request.allowed-base-branches to allow per-run base overrides.",
          };
        } else {
          const requestedBaseBranch = normalizeBranchName(requestedBaseBranchRaw);
          if (!requestedBaseBranch) {
            core.warning(`Rejecting base branch override ${requestedBaseBranchForLog}: sanitization resulted in empty branch name`);
            return {
              success: false,
              error: `Invalid base branch override: sanitization resulted in empty string (original: "${requestedBaseBranchRaw}")`,
            };
          }
          if (requestedBaseBranchRaw !== requestedBaseBranch) {
            core.warning(`Rejecting base branch override ${requestedBaseBranchForLog}: sanitized value '${requestedBaseBranch}' does not match original`);
            return {
              success: false,
              error: `Invalid base branch override: contains invalid characters (original: "${requestedBaseBranchRaw}", normalized: "${requestedBaseBranch}")`,
            };
          }
          const requestedBaseBranchSafeForLog = JSON.stringify(requestedBaseBranch);
          if (!isBaseBranchAllowed(requestedBaseBranch, allowedBaseBranches)) {
            core.warning(`Rejecting base branch override ${requestedBaseBranchSafeForLog}: does not match allowed patterns (${Array.from(allowedBaseBranches).join(", ")})`);
            return {
              success: false,
              error: `Base branch override '${requestedBaseBranch}' is not allowed. Allowed patterns: ${Array.from(allowedBaseBranches).join(", ")}`,
            };
          }

          core.info(`Base branch override accepted: ${requestedBaseBranchSafeForLog}`);
          baseBranch = requestedBaseBranch;
          core.info(`Using agent-provided base branch override: ${baseBranch}`);
        }
      }
    }

    // A pull request is stacked when its effective base branch is not the default base branch.
    const isStackedPullRequest = baseBranch !== defaultBaseBranch;
    if (isStackedPullRequest) {
      core.info(`Stacked pull request detected: base branch ${baseBranch} differs from default base branch ${defaultBaseBranch}`);
    }

    // Multi-repo support: Switch to the correct working directory for the target repo.
    // Priority:
    // 1. checkout_mapping: repos checked out into subdirectories (wildcard target-repo)
    // 2. findRepoCheckout: scan workspace for repo checkouts (subdirectory discovery)
    // 3. createCheckoutManager: dynamic git remote switching (legacy allowed-repos)
    /** @type {any} */
    let repoCwd = undefined;
    const workflowRepo = process.env.GITHUB_REPOSITORY || "";
    const isTargetingDifferentRepo = itemRepo && itemRepo.toLowerCase() !== workflowRepo.toLowerCase();

    if (isTargetingDifferentRepo && checkoutMapping) {
      // Use checkout mapping to find the subdirectory for this repo
      const targetLower = itemRepo.toLowerCase();
      const mappedPath = checkoutMapping[targetLower];
      if (mappedPath) {
        const absolutePath = require("path").resolve(process.env.GITHUB_WORKSPACE || process.cwd(), mappedPath);
        repoCwd = absolutePath;
        core.info(`Using checkout mapping: ${itemRepo} -> ${mappedPath}`);
      } else {
        // Repo not in mapping; try scanning workspace
        const checkoutResult = findRepoCheckout(itemRepo, process.env.GITHUB_WORKSPACE, { allowedRepos: [...allowedRepos] });
        if (checkoutResult.success) {
          repoCwd = checkoutResult.path;
          core.info(`Found checkout for ${itemRepo} via workspace scan at: ${repoCwd}`);
        } else {
          core.warning(`Repository ${itemRepo} not found in checkout mapping or workspace`);
          return {
            success: false,
            error: `Repository '${itemRepo}' not found in workspace. Configure it in checkout: with a path to enable multi-repo PR creation.`,
          };
        }
      }
    } else if (isTargetingDifferentRepo && !checkoutMapping) {
      // Legacy path: use findRepoCheckout first, fall back to dynamic checkout manager
      const checkoutResult = findRepoCheckout(itemRepo, process.env.GITHUB_WORKSPACE, { allowedRepos: [...allowedRepos] });
      if (checkoutResult.success) {
        repoCwd = checkoutResult.path;
        core.info(`Found checkout for ${itemRepo} at: ${repoCwd}`);
      } else if (checkoutManager) {
        // Fall back to dynamic remote switching (changes git remote in workspace root)
        const switchResult = await checkoutManager.switchTo(itemRepo, { baseBranch });
        if (!switchResult.success) {
          core.warning(`Failed to switch to repository ${itemRepo}: ${switchResult.error}`);
          return {
            success: false,
            error: `Failed to checkout repository ${itemRepo}: ${switchResult.error}`,
          };
        }
        if (switchResult.switched) {
          core.info(`Switched checkout to repository: ${itemRepo} (dynamic remote switch)`);
        }
      } else {
        // No checkout found and no checkout manager available.
        // Proceed without repoCwd — if a patch/bundle is required it will fail at apply time;
        // if allow_empty is set the PR can still be created via API alone.
        core.warning(`Repository '${itemRepo}' not found in workspace and no checkout manager available; proceeding from workspace root`);
      }
    }

    // If we have a repoCwd (subdirectory checkout), change process cwd for git operations
    const originalCwd = process.cwd();
    if (repoCwd) {
      try {
        process.chdir(repoCwd);
      } catch (chdirError) {
        return { success: false, error: `Failed to change working directory to '${repoCwd}': ${getErrorMessage(chdirError)}` };
      }
      core.info(`Changed working directory to: ${repoCwd}`);
    }

    try {
      // SECURITY: Sanitize dynamically resolved base branch to prevent shell injection
      const originalBaseBranch = baseBranch;
      baseBranch = normalizeBranchName(baseBranch);
      if (!baseBranch) {
        return {
          success: false,
          error: `Invalid base branch: sanitization resulted in empty string (original: "${originalBaseBranch}")`,
        };
      }
      if (originalBaseBranch !== baseBranch) {
        return {
          success: false,
          error: `Invalid base branch: contains invalid characters (original: "${originalBaseBranch}", normalized: "${baseBranch}")`,
        };
      }
      core.info(`Base branch for ${itemRepo}: ${baseBranch}`);

      // Stacked pull requests target a branch that must already exist. Verify it before doing any
      // work so the failure is reported with actionable guidance instead of an opaque API error.
      // Branches created earlier in this run are known to exist and are not re-checked.
      if (isStackedPullRequest && !stackBaseEntry) {
        const baseBranchCheck = await verifyStackBaseBranchExists(githubClient, repoParts, baseBranch, itemRepo, defaultBaseBranch);
        if (!baseBranchCheck.success) {
          return baseBranchCheck;
        }
      }

      // Check if patch file exists and has valid content.
      // Always require patch content for policy enforcement, even when bundle transport
      // is used for apply-time commit transport.
      const hasBundleFile = !!(bundleFilePath && fs.existsSync(bundleFilePath));
      const applyTransport = hasBundleFile ? "bundle" : "patch";
      core.info(`Apply transport mode: ${applyTransport} (bundle file present: ${hasBundleFile})`);
      if (bundleFilePath && !hasBundleFile) {
        core.warning(`Bundle file path was provided but file is not present on disk: ${bundleFilePath}; falling back to patch transport`);
      }
      const hasPatchFile = !!(patchFilePath && fs.existsSync(patchFilePath));
      if (!hasPatchFile) {
        // If allow-empty is enabled, we can proceed without a patch file
        if (allowEmpty) {
          core.info("No patch file found, but allow-empty is enabled - will create empty PR");
        } else {
          const message = "No patch file found - cannot create pull request without changes";

          // If in staged mode, still show preview
          if (isStaged) {
            let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
            summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
            summaryContent += `**Status:** ⚠️ No patch file found\n\n`;
            summaryContent += `**Message:** ${message}\n\n`;

            // Write to step summary
            await core.summary.addRaw(summaryContent).write();
            core.info("📝 Pull request creation preview written to step summary (no patch file)");
            return { success: true, staged: true };
          }

          switch (ifNoChanges) {
            case "error":
              return { success: false, error: message };

            case "ignore":
              // Silent success - no console output
              return { success: false, skipped: true };

            case "warn":
            default:
              core.warning(message);
              return { success: false, error: message, skipped: true };
          }
        }
      }

      let patchContent = "";
      let isEmpty = true;
      if (hasPatchFile) {
        try {
          patchContent = fs.readFileSync(patchFilePath, "utf8");
        } catch (err) {
          throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
        }
        isEmpty = !patchContent || !patchContent.trim();
      }

      // Enforce max limits on patch before processing.
      // Count files once here so the catch block can reuse the value without re-parsing.
      const patchFileCount = countUniquePatchFiles(patchContent);
      try {
        enforcePullRequestLimits(patchContent, maxFiles);
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.warning(`Pull request limit exceeded: ${errorMessage}`);

        // In staged mode, show a preview instead of performing API side effects
        if (isStaged) {
          let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
          summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
          summaryContent += `**Status:** ⚠️ Patch file limit exceeded\n\n`;
          summaryContent += `**Message:** ${errorMessage}\n\n`;

          await core.summary.addRaw(summaryContent).write();
          core.info("📝 Pull request creation preview written to step summary (file limit exceeded)");
          return { success: true, staged: true };
        }

        if (!fallbackAsIssue) {
          return { success: false, error: errorMessage };
        }

        // Surface the limit error in a fallback issue so it appears in the agent failure
        // issue/comment thread and the workflow operator knows exactly how to fix it.
        const rawFallbackTitle = pullRequestItem.title?.trim() || "Agent Output";
        const fallbackTitle = applyTitlePrefix(sanitizeTitle(rawFallbackTitle, titlePrefix), titlePrefix);
        const fallbackLabels = mergeFallbackIssueLabels(configFallbackLabels.length > 0 ? configFallbackLabels : envLabels);
        const fallbackTemplatePath = getPromptPath("e003_file_limit_fallback.md");
        const fallbackBody = renderTemplateFromFile(fallbackTemplatePath, {
          error_message: errorMessage,
          suggested_limit: patchFileCount,
        });

        try {
          const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, fallbackTitle, fallbackBody, fallbackLabels, configAssignees);
          core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);
          await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);
          await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");
          return {
            success: true,
            fallback_used: true,
            issue_number: issue.number,
            issue_url: issue.html_url,
          };
        } catch (issueError) {
          const combinedError = `Pull request limit exceeded and failed to create fallback issue. Limit error: ${errorMessage}. Issue error: ${getErrorMessage(issueError)}`;
          core.error(combinedError);
          return { success: false, error: combinedError };
        }
      }

      // Check for actual error conditions (but allow empty patches as valid noop)
      if (patchContent.includes("Failed to generate patch")) {
        // If allow-empty is enabled, ignore patch errors and proceed
        if (allowEmpty) {
          core.info("Patch file contains error, but allow-empty is enabled - will create empty PR");
          patchContent = "";
          isEmpty = true;
        } else {
          const message = "Patch file contains error message - cannot create pull request without changes";

          // If in staged mode, still show preview
          if (isStaged) {
            let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
            summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
            summaryContent += `**Status:** ⚠️ Patch file contains error\n\n`;
            summaryContent += `**Message:** ${message}\n\n`;

            // Write to step summary
            await core.summary.addRaw(summaryContent).write();
            core.info("📝 Pull request creation preview written to step summary (patch error)");
            return { success: true, staged: true };
          }

          switch (ifNoChanges) {
            case "error":
              return { success: false, error: message };

            case "ignore":
              // Silent success - no console output
              return { success: false, skipped: true };

            case "warn":
            default:
              core.warning(message);
              return { success: false, error: message, skipped: true };
          }
        }
      }

      // Validate patch size (unless empty)
      if (!isEmpty) {
        // maxSizeKb is already extracted from config at the top
        const patchSizeBytes = Buffer.byteLength(patchContent, "utf8");
        const patchSizeKb = Math.ceil(patchSizeBytes / 1024);

        core.info(`Patch size: ${patchSizeKb} KB (maximum allowed: ${maxSizeKb} KB)`);

        if (patchSizeKb > maxSizeKb) {
          const message = `Patch size (${patchSizeKb} KB) exceeds maximum allowed size (${maxSizeKb} KB)`;

          // If in staged mode, still show preview with error
          if (isStaged) {
            let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
            summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";
            summaryContent += `**Status:** ❌ Patch size exceeded\n\n`;
            summaryContent += `**Message:** ${message}\n\n`;

            // Write to step summary
            await core.summary.addRaw(summaryContent).write();
            core.info("📝 Pull request creation preview written to step summary (patch size error)");
            return { success: true, staged: true };
          }

          return { success: false, error: message };
        }

        core.info("Patch size validation passed");
      }

      // Check file protection: allowlist (strict) or protected-files policy.
      /** @type {string[] | null} Protected files that trigger fallback-to-issue handling */
      let manifestProtectionFallback = null;
      /** @type {string[] | null} Protected files that trigger request-review handling */
      let manifestProtectionRequestReview = null;
      /** @type {unknown} */
      let manifestProtectionPushFailedError = null;
      if (!isEmpty) {
        const protection = checkFileProtection(patchContent, {
          ...config,
          protected_files_policy: config.protected_files_policy ?? "request_review",
        });
        if (protection.action === "deny") {
          const filesStr = protection.files.join(", ");
          const message =
            protection.source === "allowlist"
              ? `Cannot create pull request: patch modifies files outside the allowed-files list (${filesStr}). Add the files to the allowed-files configuration field or remove them from the patch.`
              : `Cannot create pull request: patch modifies protected files (${filesStr}). Add them to the allowed-files configuration field or set protected-files: fallback-to-issue to create a review issue instead.`;
          core.error(message);
          return { success: false, error: message };
        }
        if (protection.action === "fallback") {
          manifestProtectionFallback = protection.files;
          core.warning(`Protected file protection triggered (fallback-to-issue): ${protection.files.join(", ")}. Will create review issue instead of pull request.`);
        }
        if (protection.action === "request_review") {
          manifestProtectionRequestReview = protection.files;
          core.warning(`Protected file protection triggered (request_review): ${protection.files.join(", ")}. Will create pull request with caution and request-changes review.`);
        }
      }

      if (isEmpty && !isStaged && !allowEmpty) {
        const message = "Patch file is empty - no changes to apply (noop operation)";

        switch (ifNoChanges) {
          case "error":
            return { success: false, error: "No changes to push - failing as configured by if-no-changes: error" };

          case "ignore":
            // Silent success - no console output
            return { success: false, skipped: true };

          case "warn":
          default:
            core.warning(message);
            return { success: false, error: message, skipped: true };
        }
      }

      if (!isEmpty) {
        core.info("Patch content validation passed");
      } else if (allowEmpty) {
        core.info("Patch file is empty - processing empty PR creation (allow-empty is enabled)");
      } else {
        core.info("Patch file is empty - processing noop operation");
      }

      // If in staged mode, emit step summary instead of creating PR
      if (isStaged) {
        let summaryContent = "## 🎭 Staged Mode: Create Pull Request Preview\n\n";
        summaryContent += "The following pull request would be created if staged mode was disabled:\n\n";

        summaryContent += `**Title:** ${pullRequestItem.title || "No title provided"}\n\n`;
        summaryContent += `**Branch:** ${pullRequestItem.branch || "auto-generated"}\n\n`;
        summaryContent += `**Base:** ${baseBranch}\n\n`;

        if (pullRequestItem.body) {
          summaryContent += `**Body:**\n${pullRequestItem.body}\n\n`;
        }

        if (patchFilePath && fs.existsSync(patchFilePath)) {
          let patchStats;
          try {
            patchStats = fs.readFileSync(patchFilePath, "utf8");
          } catch (err) {
            throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
          }
          if (patchStats.trim()) {
            summaryContent += `**Changes:** Patch file exists with ${patchStats.split("\n").length} lines\n\n`;
            summaryContent += `<details><summary>Show patch preview</summary>\n\n\`\`\`diff\n${patchStats.slice(0, 2000)}${patchStats.length > 2000 ? "\n... (truncated)" : ""}\n\`\`\`\n\n</details>\n\n`;
          } else {
            summaryContent += `**Changes:** No changes (empty patch)\n\n`;
          }
        }

        // Write to step summary
        await core.summary.addRaw(summaryContent).write();
        core.info("📝 Pull request creation preview written to step summary");
        return { success: true, staged: true };
      }

      // Extract title, body, and branch from the JSON item
      let title = pullRequestItem.title.trim();
      let processedBody = pullRequestItem.body;

      // Replace temporary ID references in the body with resolved issue/PR numbers
      // This allows PRs to reference issues created earlier in the same workflow
      // by using temporary IDs like #aw_123abc456def
      if (resolvedTemporaryIds && Object.keys(resolvedTemporaryIds).length > 0) {
        // Convert object to Map for compatibility with replaceTemporaryIdReferences
        const tempIdMap = new Map(Object.entries(resolvedTemporaryIds));
        processedBody = replaceTemporaryIdReferences(processedBody, tempIdMap, itemRepo);
        core.info(`Resolved ${tempIdMap.size} temporary ID references in PR body`);
      }

      // Remove duplicate title from description if it starts with a header matching the title
      processedBody = removeDuplicateTitleFromDescription(title, processedBody);

      // Sanitize body content to neutralize @mentions, URLs, and other security risks
      processedBody = sanitizeContent(processedBody, { allowedAliases: allowedMentionAliases, maxMentions });

      // Auto-add "Fixes #N" closing keyword if triggered from an issue and not already present.
      // This ensures the triggering issue is auto-closed when the PR is merged.
      // Agents are instructed to include this but don't reliably do so.
      // This behavior can be disabled by setting auto-close-issue: false in the workflow config.
      if (triggeringIssueNumber && autoCloseIssue) {
        const hasClosingKeyword = /(?:fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved)\s+#\d+/i.test(processedBody);
        if (!hasClosingKeyword) {
          processedBody = processedBody.trimEnd() + `\n\n- Fixes #${triggeringIssueNumber}`;
          core.info(`Auto-added "Fixes #${triggeringIssueNumber}" closing keyword to PR body as bullet point`);
        }
      } else if (triggeringIssueNumber && !autoCloseIssue) {
        core.info(`Skipping auto-close keyword for #${triggeringIssueNumber} (auto-close-issue: false)`);
      }

      let bodyLines = processedBody.split("\n");
      let branchName = pullRequestItem.branch ? pullRequestItem.branch.trim() : null;
      // Preserve the original agent branch name for bundle transport (the bundle was created
      // using this branch name as the refs/heads ref inside the bundle file).
      const originalAgentBranch = branchName;
      const randomHex = crypto.randomBytes(8).toString("hex");

      // SECURITY: Sanitize branch name to prevent shell injection (CWE-78)
      // Branch names from user input must be normalized before use in git commands.
      // When preserve-branch-name is disabled (default), a random salt suffix is
      // appended to avoid collisions.
      if (branchName) {
        const originalBranchName = branchName;
        branchName = normalizeBranchName(branchName, preserveBranchName ? null : randomHex);

        // Validate it's not empty after normalization
        if (!branchName) {
          throw new Error(`Invalid branch name: sanitization resulted in empty string (original: "${originalBranchName}")`);
        }

        if (preserveBranchName) {
          core.info(`Using branch name from JSONL without salt suffix (preserve-branch-name enabled): ${branchName}`);
        } else {
          core.info(`Using branch name from JSONL with added salt: ${branchName}`);
        }
        if (originalBranchName !== branchName) {
          core.info(`Branch name sanitized: "${originalBranchName}" -> "${branchName}"`);
        }
      }

      // If no title was found, use a default
      if (!title) {
        title = "Agent Output";
      }

      // Sanitize title for Unicode security and remove any duplicate prefixes
      title = sanitizeTitle(title, titlePrefix);

      // Apply title prefix (only if it doesn't already exist)
      title = applyTitlePrefix(title, titlePrefix);

      // Add AI disclaimer with workflow name and run url
      const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
      const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
      const runUrl = buildWorkflowRunUrl(context, context.repo);
      const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE ?? "";
      const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL ?? "";
      const triggeringPRNumber = context.payload.pull_request?.number;
      const triggeringDiscussionNumber = context.payload.discussion?.number;

      // Prepend threat detection caution alert at the very top of the PR body so it is
      // immediately visible to reviewers. The caution is omitted from the footer to
      // avoid duplication (skipDetectionCaution is passed to generateFooterWithMessages).

      // Inject body header before user content (unshifted first, so caution will appear before it)
      const bodyHeader = getBodyHeader({ workflowName, runUrl });
      if (bodyHeader) {
        bodyLines.unshift(...bodyHeader.split("\n"), "");
      }

      // Inject disclosure header (this runs after body-header, but appears before it because unshift prepends)
      const disclosureHeader = getDisclosureHeader({ workflowName, runUrl });
      if (disclosureHeader) {
        bodyLines.unshift(...disclosureHeader.split("\n"), "");
      }

      // Keep the protected-files notice directly under detection caution:
      // this block runs first, then detectionCaution below unshifts to index 0.
      if (manifestProtectionRequestReview && manifestProtectionRequestReview.length > 0) {
        const protectedFilesNoticeTemplatePath = getPromptPath("manifest_protection_request_review.md");
        const protectedFilesNotice = renderTemplateFromFile(protectedFilesNoticeTemplatePath, {
          files: renderFilesList(manifestProtectionRequestReview.join(", ")),
        });
        bodyLines.unshift(protectedFilesNotice, "", "");
      }
      // Inject CAUTION at top of body (unshifted after header so it appears first in the final output)
      const detectionCaution = assembleMarkdownBodyParts({
        includeFooter: false,
        workflowName,
        runUrl,
      }).detectionCaution;
      if (detectionCaution) {
        // unshift(caution, "", "") places the caution alert at index 0 and two blank
        // separator lines so the main body content follows after a full empty line.
        bodyLines.unshift(detectionCaution, "", "");
      }

      // Add fingerprint comment if present
      const trackerIDComment = getTrackerID("markdown");
      if (trackerIDComment) {
        bodyLines.push(trackerIDComment);
      }

      // Stacked pull request metadata: record the stack relationship in the body so reviewers can
      // see how the pull requests relate. Metadata declared by the agent is preserved even when
      // stacked pull requests are disabled (the pull request then simply targets the default base).
      const stackDependencyBranches = [...stackMetadata.dependencies];
      if (stackBaseEntry && !stackDependencyBranches.includes(stackBaseEntry.branch)) {
        stackDependencyBranches.unshift(stackBaseEntry.branch);
      }
      const dependsOnPullRequests = stackTracker.resolveDependencies(stackDependencyBranches, itemRepo);
      const stackMetadataLines = buildStackMetadataLines({
        base: isStackedPullRequest ? baseBranch : null,
        position: stackMetadata.position,
        root: stackMetadata.root,
        dependencies: stackDependencyBranches,
        dependsOnPullRequests,
      });
      if (stackMetadataLines.length > 0) {
        bodyLines.push("", ...stackMetadataLines);
        core.info(`Added stacked pull request metadata to body: ${stackMetadataLines.join(" ")}`);
      }

      // Snapshot the body content (without footer) for use in protected-files fallback ordering.
      // The protected-files section must appear before the footer (including guard notices such as
      // the integrity-filtering note) so that the footer always comes last in the issue body.
      const mainBodyContent = bodyLines.join("\n").trim();
      const issueSafeMainBodyContent = neutralizeClosingKeywordsForIssueBody(mainBodyContent);

      // Generate footer using messages template system (respects custom messages.footer config)
      // When footer is disabled, only add XML markers (no visible footer content)
      const footerParts = [];
      if (includeFooter) {
        const historyUrl =
          generateHistoryUrl({
            owner: repoParts.owner,
            repo: repoParts.repo,
            itemType: "pull_request",
            workflowId,
            serverUrl: context.serverUrl,
          }) ?? undefined;
        // The footer builder skips detection caution so the caution already prepended at
        // the top of the body is not duplicated in the footer.
        let footer = assembleMarkdownBodyParts({
          includeFooter: true,
          workflowName,
          runUrl,
          workflowSource,
          workflowSourceURL,
          triggeringIssueNumber,
          triggeringPRNumber,
          triggeringDiscussionNumber,
          historyUrl,
        }).footer;
        footer = addExpirationToFooter(footer, expiresHours, "Pull Request");
        if (expiresHours > 0) {
          footer += "\n\n<!-- gh-aw-expires-type: pull-request -->";
        }
        bodyLines.push(``, footer);
        footerParts.push(footer);
      }

      // Add standalone workflow-id marker for searchability (consistent with comments)
      // Always add XML markers even when footer is disabled
      if (workflowId) {
        const workflowIdMarker = generateWorkflowIdMarker(workflowId);
        // Add to bodyLines for the normal PR body path.
        // Add to footerParts so the fallback issue body places it after the protected-files section.
        bodyLines.push(``, workflowIdMarker);
        footerParts.push(workflowIdMarker);
      }

      // Embed gh-aw-workflow-call-id marker so callers sharing the same reusable workflow
      // do not close each other's PRs when close-older-pull-requests is enabled.
      if (callerWorkflowId) {
        bodyLines.push(generateWorkflowCallIdMarker(callerWorkflowId));
      }

      // Embed gh-aw-close-key marker when an explicit deduplication key is set.
      if (closeOlderKey) {
        bodyLines.push(generateCloseKeyMarker(closeOlderKey));
      }

      bodyLines.push("");

      // Prepare the body content
      const body = bodyLines.join("\n").trim();
      const issueSafeBody = neutralizeClosingKeywordsForIssueBody(body);
      // Footer section (footer + workflow-id marker) used when ordering protected-files notices
      const footerContent = footerParts.join("\n\n");

      // Build labels array - merge config labels with message labels
      let labels = [...envLabels];
      if (pullRequestItem.labels && Array.isArray(pullRequestItem.labels)) {
        labels = [...labels, ...pullRequestItem.labels];
      }
      labels = labels
        .filter(label => !!label)
        .map(label => String(label).trim())
        .filter(label => label);
      // Add agentic-threat-detected label when threat detection produced a warning
      if (detectionCaution && !labels.includes("agentic-threat-detected")) {
        labels.push("agentic-threat-detected");
      }
      // Use explicitly configured fallback labels when present; otherwise preserve
      // existing behavior by reusing pull request labels for fallback issues.
      const effectiveFallbackLabels = configFallbackLabels.length > 0 ? configFallbackLabels : labels;

      // Configuration enforces draft as a policy, not a fallback (consistent with autoMerge/allowEmpty)
      const draft = draftDefault;
      if (pullRequestItem.draft !== undefined && pullRequestItem.draft !== draftDefault) {
        core.warning(
          `Agent requested draft: ${pullRequestItem.draft}, but configuration enforces draft: ${draftDefault}. ` +
            `Configuration takes precedence for security. To change this, update safe-outputs.create-pull-request.draft in the workflow file.`
        );
      }

      core.info(`Creating pull request with title: ${title}`);
      core.info(`Labels: ${JSON.stringify(labels)}`);
      core.info(`Draft: ${draft}`);
      core.info(`Body length: ${body.length}`);

      // When no branch name was provided by the agent, generate a unique one.
      if (!branchName) {
        core.info("No branch name provided in JSONL, generating unique branch name");
        branchName = `${workflowId}-${randomHex}`;
      }

      // Apply the configured branch prefix (e.g. "signed/") if it hasn't already been applied.
      if (branchPrefix && !branchName.startsWith(branchPrefix)) {
        branchName = `${branchPrefix}${branchName}`;
        core.info(`Applied branch prefix: ${branchName}`);
      }

      core.info(`Generated branch name: ${branchName}`);
      core.info(`Base branch: ${baseBranch}`);

      // Reject stacks that would depend on themselves: the requested base (or one of its ancestors
      // in this run) is the branch of the pull request being created.
      const stackBranchAliases = [branchName, originalAgentBranch];
      if (hasCircularStackDependency(stackBranchAliases, baseBranch, stackTracker.parents) || stackMetadata.dependencies.some(dependency => stackBranchAliases.includes(dependency))) {
        const error = circularStackError(branchName, baseBranch);
        core.warning(error);
        return { success: false, error };
      }

      // Create a new branch using git CLI, ensuring it's based on the correct base branch

      // First, fetch the base branch specifically (since we use shallow checkout)
      core.info(`Fetching base branch: ${baseBranch}`);

      // Fetch without creating/updating local branch to avoid conflicts with current branch
      // This works even when we're already on the base branch
      await exec.exec("git", ["fetch", "origin", baseBranch]);

      // Apply the patch/bundle using git CLI (skip if empty)
      // Track number of new commits pushed so we can restrict the extra empty commit
      // to branches with exactly one new commit (security: prevents use of CI trigger
      // token on multi-commit branches where workflow files may have been modified).
      let newCommitCount = 0;
      if (hasBundleFile) {
        // Bundle transport: fetch commits directly from the bundle file.
        // This preserves merge commit topology and per-commit metadata (messages, authorship)
        // unlike git format-patch which flattens history and drops merge resolution content.
        core.info(`Applying changes from bundle: ${bundleFilePath}`);
        try {
          await applyBundleToBranch(bundleFilePath, branchName, originalAgentBranch, exec, baseBranch);
        } catch (bundleError) {
          core.error(`Failed to apply bundle: ${getErrorMessage(bundleError)}`);
          return { success: false, error: "Failed to apply bundle" };
        }

        // Push the commits from the bundle to the remote branch
        // Note: when manifestProtectionFallback is set we still push the branch so the
        // fallback issue can include a compare URL.  Genuine push failures are handled in
        // the catch block below.
        {
          const forkCwd = process.cwd();
          const runBundlePush = async () => {
            branchName = await handleRemoteBranchCollision(branchName, preserveBranchName, {
              recreateRef,
              githubClient: pushGithubClient,
              owner: pushRepoParts.owner,
              repo: pushRepoParts.repo,
              remoteTarget: pushRemoteUrl || "origin",
              remoteToken: headGitHubToken,
              cwd: forkCwd,
            });

            await pushSignedCommits({
              githubClient: pushGithubClient,
              owner: pushRepoParts.owner,
              repo: pushRepoParts.repo,
              branch: branchName,
              baseRef: `origin/${baseBranch}`,
              cwd: forkCwd,
              pushRemoteUrl,
              pushToken: headGitHubToken,
              signedCommits,
              resolvedTemporaryIds,
              currentRepo: itemRepo,
              validationConfig: config,
            });
          };
          try {
            await runBundlePush();
            core.info("Changes pushed to branch (from bundle)");

            // Count new commits on PR branch relative to base
            try {
              const { stdout: countStr } = await exec.getExecOutput("git", ["rev-list", "--count", `origin/${baseBranch}..HEAD`]);
              newCommitCount = parseInt(countStr.trim(), 10);
              core.info(`${newCommitCount} new commit(s) on branch relative to origin/${baseBranch}`);
            } catch {
              core.info("Could not count new commits - extra empty commit will be skipped");
            }
          } catch (initialPushError) {
            /** @type {unknown} */
            let pushError = initialPushError;
            let pushRecovered = false;
            const pushErrorMessage = getErrorMessage(pushError);
            const isSignedMergeReplayRefusal = signedCommits && /pushSignedCommits: refusing unsigned push/.test(pushErrorMessage) && /merge commit/i.test(pushErrorMessage);

            if (isSignedMergeReplayRefusal) {
              core.warning("Signed push rejected merge commit topology from bundle; rewriting branch and retrying signed push");
              try {
                await rewriteBundleBranchAsSingleCommit(baseBranch, exec, bundleFilePath, {
                  excludedFiles: Array.isArray(config.excluded_files) ? config.excluded_files : [],
                });
                const runRetryPush = async () =>
                  pushSignedCommits({
                    githubClient: pushGithubClient,
                    owner: pushRepoParts.owner,
                    repo: pushRepoParts.repo,
                    branch: branchName,
                    baseRef: `origin/${baseBranch}`,
                    cwd: forkCwd,
                    pushRemoteUrl,
                    pushToken: headGitHubToken,
                    signedCommits,
                    resolvedTemporaryIds,
                    currentRepo: itemRepo,
                    validationConfig: config,
                  });
                await runRetryPush();
                core.info("Changes pushed to branch after bundle rewrite retry");

                try {
                  const { stdout: countStr } = await exec.getExecOutput("git", ["rev-list", "--count", `origin/${baseBranch}..HEAD`]);
                  newCommitCount = parseInt(countStr.trim(), 10);
                  core.info(`${newCommitCount} new commit(s) on branch relative to origin/${baseBranch}`);
                } catch {
                  core.info("Could not count new commits - extra empty commit will be skipped");
                }
                pushRecovered = true;
              } catch (retryPushError) {
                pushError = retryPushError;
              }
            }

            if (!pushRecovered) {
              core.error(`Git push failed: ${getErrorMessage(pushError)}`);

              if (manifestProtectionFallback) {
                // Push failed specifically for a protected-file modification. Don't create
                // a generic push-failed issue — fall through to the manifestProtectionFallback
                // block below, which will create the proper protected-file review issue with
                // patch artifact download instructions (since the branch was not pushed).
                core.warning("Git push failed for protected-file modification - deferring to protected-file review issue");
                manifestProtectionPushFailedError = pushError;
              } else if (!fallbackAsIssue) {
                const error = `Failed to push changes: ${getErrorMessage(pushError)}`;
                return { success: false, error, error_type: "push_failed" };
              } else {
                core.warning("Git push operation failed - creating fallback issue instead of pull request");

                const runUrl = buildWorkflowRunUrl(context, context.repo);
                const runId = context.runId;

                const artifactFileName = bundleFilePath ? bundleFilePath.replace("/tmp/gh-aw/", "") : "aw-unknown.bundle";
                const recoveryInstructions = buildManualBranchRecoveryCommands({
                  hasBundleFile: true,
                  runId,
                  artifactFileName,
                  branchName,
                  baseBranch,
                  tempRef: createBundleTempRef(branchName),
                });
                const pushFailureMessage = sanitizeContent(neutralizeClosingKeywordsForIssueBody(getErrorMessage(pushError)), { allowedAliases: allowedMentionAliases, maxMentions })
                  .replace(/\s+/g, " ")
                  .trim();
                const pushErrorSection = buildPushErrorSection(getErrorMessage(pushError), pushFailureMessage);
                const fallbackBody = `${issueSafeBody}

---

> [!NOTE]
> This was originally intended as a pull request, but the git push operation failed.
>
${pushErrorSection}
>
> **Workflow Run:** [View run details and download bundle artifact](${runUrl})
>
> The bundle file is available in the \`agent\` artifact in the workflow run linked above.

<details>
<summary>Create the pull request manually</summary>

\`\`\`sh
${recoveryInstructions}

# Push the branch to the target remote
git push ${shellQuote(pushRemoteUrl || "origin")} ${shellQuote(branchName)}

# Create the pull request
gh pr create --title ${shellQuote(title)} --base ${shellQuote(baseBranch)} --head ${shellQuote(getPullRequestHeadRef(branchName))} --repo ${shellQuote(`${repoParts.owner}/${repoParts.repo}`)}
\`\`\`

</details>`;

                try {
                  const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, title, fallbackBody, mergeFallbackIssueLabels(effectiveFallbackLabels), configAssignees);

                  core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);
                  await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);
                  await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

                  return {
                    success: true,
                    fallback_used: true,
                    issue_number: issue.number,
                    issue_url: issue.html_url,
                  };
                } catch (issueError) {
                  const error = `Failed to push changes and failed to create fallback issue. Push error: ${getErrorMessage(pushError)}. Issue error: ${getErrorMessage(issueError)}`;
                  return { success: false, error };
                }
              }
            }
          }
        }
      } else {
        // Checkout the base branch (using origin/${baseBranch} if local doesn't exist)
        try {
          await exec.exec("git", ["checkout", baseBranch]);
        } catch (checkoutError) {
          // If local branch doesn't exist, create it from origin
          core.info(`Local branch ${baseBranch} doesn't exist, creating from origin/${baseBranch}`);
          await exec.exec("git", ["checkout", "-b", baseBranch, `origin/${baseBranch}`]);
        }

        // Handle branch creation/checkout
        let branchBaseRef = baseBranch;
        const recordedBaseCommit = extractPatchBaseCommit(patchContent);
        if (recordedBaseCommit) {
          core.info(`Patch route base_commit resolved: ${recordedBaseCommit}`);
          core.info(`Using base_commit embedded in the patch for patch apply: ${recordedBaseCommit}`);
          try {
            try {
              await exec.exec("git", ["fetch", "origin", recordedBaseCommit, "--depth=1"]);
            } catch (fetchError) {
              core.info(`Note: could not fetch base commit ${recordedBaseCommit} explicitly (${getErrorMessage(fetchError)}); will verify local availability next`);
            }
            await exec.exec("git", ["cat-file", "-e", recordedBaseCommit]);
            const ancestryCheck = await exec.getExecOutput("git", ["merge-base", "--is-ancestor", recordedBaseCommit, `origin/${baseBranch}`], { ignoreReturnCode: true });
            if (ancestryCheck.exitCode !== 0) {
              throw new Error(`recorded base_commit ${recordedBaseCommit} is not an ancestor of origin/${baseBranch}; falling back to ${baseBranch}`);
            }
            branchBaseRef = recordedBaseCommit;
          } catch (baseCommitError) {
            core.warning(`Recorded base_commit ${recordedBaseCommit} is not available in this checkout (${getErrorMessage(baseCommitError)}); falling back to ${baseBranch}`);
          }
        }
        core.info(`Branch should not exist locally, creating new branch from base: ${branchName} (${branchBaseRef})`);
        await exec.exec("git", ["checkout", "-b", branchName, branchBaseRef]);
        core.info(`Created new branch from base: ${branchName} (${branchBaseRef})`);

        // Apply the patch using git CLI (skip if empty)
        if (!isEmpty && patchFilePath) {
          /** @type {any} */
          let postApplyBaseRef = null;
          const capturePostApplyBaseRef = async () => {
            const headResult = await exec.getExecOutput("git", ["rev-parse", "HEAD"]);
            const resolvedRef = headResult.stdout.trim();
            if (resolvedRef) {
              postApplyBaseRef = resolvedRef;
            }
          };

          // Resolve temporary ID references in patch content before applying
          // This handles references like #aw_XXX in committed source code
          if (resolvedTemporaryIds && Object.keys(resolvedTemporaryIds).length > 0) {
            const tempIdMap = new Map(Object.entries(resolvedTemporaryIds));
            const originalPatchContent = patchContent;
            patchContent = replaceTemporaryIdReferencesInPatch(patchContent, tempIdMap, itemRepo);
            if (patchContent !== originalPatchContent) {
              core.info("Resolved temporary ID references in patch content");
              try {
                fs.writeFileSync(patchFilePath, patchContent, "utf8");
              } catch (err) {
                throw new Error(`Failed to write file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
              }
            }
          }

          core.info("Applying patch...");
          const patchLines = patchContent.split("\n");
          const previewLineCount = Math.min(500, patchLines.length);
          core.info(`Patch preview (first ${previewLineCount} of ${patchLines.length} lines):`);
          for (let i = 0; i < previewLineCount; i++) {
            core.info(patchLines[i]);
          }

          // Patches are created with git format-patch, so use git am to apply them
          // Use --3way to handle cross-repo patches where the patch base may differ from target repo
          // This allows git to resolve create-vs-modify mismatches when a file exists in target but not source
          let patchApplied = false;
          try {
            await capturePostApplyBaseRef();
            await exec.exec("git", ["am", "--3way", patchFilePath]);
            core.info("Patch applied successfully");
            patchApplied = true;
          } catch (patchError) {
            core.error(`Failed to apply patch with --3way: ${getErrorMessage(patchError)}`);

            const recoveredFromAddAddConflict = await tryRecoverGitAmAddAddConflict(exec);
            if (recoveredFromAddAddConflict.recovered) {
              patchApplied = true;
            } else {
              if (recoveredFromAddAddConflict.errorMessage) {
                core.warning(`Automatic add/add conflict recovery attempt failed: ${recoveredFromAddAddConflict.errorMessage}`);
              }
              // Investigate why the patch failed by logging git status and the failed patch
              try {
                core.info("Investigating patch failure...");

                // Log git status to see the current state
                const statusResult = await exec.getExecOutput("git", ["status"]);
                core.info("Git status output:");
                core.info(statusResult.stdout);

                // Log the failed patch diff
                const patchResult = await exec.getExecOutput("git", ["am", "--show-current-patch=diff"]);
                core.info("Failed patch content:");
                core.info(patchResult.stdout);
              } catch (investigateError) {
                core.warning(`Failed to investigate patch failure: ${getErrorMessage(investigateError)}`);
              }

              // Abort the failed git am before attempting any fallback
              try {
                await exec.exec("git am --abort");
                core.info("Aborted failed git am");
              } catch (abortError) {
                core.warning(`Failed to abort git am: ${getErrorMessage(abortError)}`);
              }

              // Fallback (Option 1): create the PR branch at the original base commit so the PR
              // can still be created. GitHub will show the merge conflicts, allowing manual resolution.
              // This handles the case where the target branch received intervening commits after
              // the patch was generated, making --3way unable to resolve the conflicts automatically.
              core.info("Attempting fallback: create PR branch at original base commit...");
              try {
                // Use the base commit recorded at patch generation time.
                // The From <sha> header in format-patch output contains the agent's new commit SHA
                // which does not exist in this checkout, so we cannot derive the base from it.
                const originalBaseCommit = extractPatchBaseCommit(patchContent);
                if (!originalBaseCommit) {
                  core.warning("No base_commit embedded in patch - fallback not possible");
                } else {
                  core.info(`Original base commit from patch generation: ${originalBaseCommit}`);

                  // In shallow clones (fetch-depth: 1) the base commit may not be locally available.
                  // Attempt to fetch it explicitly before checking whether it exists.
                  try {
                    await exec.exec("git", ["fetch", "origin", originalBaseCommit, "--depth=1"]);
                  } catch (fetchError) {
                    // Non-fatal: the commit may already be available, or the server may not support
                    // fetching individual SHAs (e.g. some GHE configurations). Log for troubleshooting.
                    core.info(`Note: could not fetch base commit ${originalBaseCommit} explicitly (${getErrorMessage(fetchError)}); will verify local availability next`);
                  }

                  // Verify the base commit is available in this repo (may not exist cross-repo)
                  await exec.exec("git", ["cat-file", "-e", originalBaseCommit]);
                  core.info("Original base commit exists locally - proceeding with fallback");

                  // Re-create the PR branch at the original base commit
                  await exec.exec("git", ["checkout", baseBranch]);
                  try {
                    await exec.exec("git", ["branch", "-D", branchName]);
                  } catch {
                    // Branch may not exist yet, ignore
                  }
                  await exec.exec("git", ["checkout", "-b", branchName, originalBaseCommit]);
                  core.info(`Created branch ${branchName} at original base commit ${originalBaseCommit}`);

                  // Try --3way first to maximize repair opportunities even on fallback branches.
                  // If that still fails with add/add conflicts, recover and continue git am.
                  try {
                    await capturePostApplyBaseRef();
                    await exec.exec("git", ["am", "--3way", patchFilePath]);
                  } catch (fallbackPatchError) {
                    core.warning(`Fallback git am --3way failed: ${getErrorMessage(fallbackPatchError)}`);
                    const recoveredFallback = await tryRecoverGitAmAddAddConflict(exec);
                    if (!recoveredFallback.recovered) {
                      if (recoveredFallback.errorMessage) {
                        core.warning(`Automatic add/add conflict recovery attempt failed during fallback: ${recoveredFallback.errorMessage}`);
                      }
                      try {
                        await exec.exec("git am --abort");
                      } catch (abortFallbackError) {
                        core.warning(`Failed to abort fallback git am: ${getErrorMessage(abortFallbackError)}`);
                      }
                      throw fallbackPatchError;
                    }
                  }

                  core.info("Patch applied successfully at original base commit");
                  core.warning(`PR branch ${branchName} is based on an earlier commit than the current ${baseBranch} HEAD. The pull request will show merge conflicts that require manual resolution.`);
                  patchApplied = true;
                }
              } catch (fallbackError) {
                core.warning(`Fallback to original base commit failed: ${getErrorMessage(fallbackError)}`);
              }
            }

            if (!patchApplied) {
              return { success: false, error: "Failed to apply patch" };
            }
          }

          // POST-APPLY FILE PROTECTION: Verify actual files written match policy.
          // This is the primary defense against parser-differential attacks where the JS
          // patch parser and git am disagree on which files a patch contains.
          // (see github/agentic-workflows#539)
          {
            const diffBaseRef = postApplyBaseRef || `origin/${baseBranch}`;
            const diffResult = await exec.getExecOutput("git", ["diff", "--name-only", "--no-renames", `${diffBaseRef}..HEAD`]);
            const actualFiles = diffResult.stdout
              .split("\n")
              .map(f => f.trim())
              .filter(Boolean);
            if (actualFiles.length > 0) {
              core.info(`Post-apply verification: ${actualFiles.length} file(s) actually modified`);
              const postApplyProtection = checkFileProtectionPostApply(actualFiles, {
                ...config,
                protected_files_policy: config.protected_files_policy ?? "request_review",
              });
              if (postApplyProtection.action === "deny") {
                const filesStr = postApplyProtection.files.join(", ");
                const msg = `SECURITY: Post-apply file-protection check failed. ` + `The patch applied files that were not detected by the pre-apply parser: ${filesStr}. ` + `This may indicate a patch-parser bypass attempt. Aborting.`;
                core.error(msg);
                return { success: false, error: msg };
              }
              if (postApplyProtection.action === "fallback") {
                manifestProtectionFallback = postApplyProtection.files;
                core.warning(`Post-apply: Protected file protection triggered (fallback-to-issue): ${postApplyProtection.files.join(", ")}`);
              }
              if (postApplyProtection.action === "request_review") {
                manifestProtectionRequestReview = postApplyProtection.files;
                core.warning(`Post-apply: Protected file protection triggered (request_review): ${postApplyProtection.files.join(", ")}`);
              }
            }
          }

          // Push the applied commits to the branch (with fallback to issue creation on failure)
          // Note: when manifestProtectionFallback is set we still push the branch so the
          // fallback issue can include a compare URL.  Genuine push failures are handled in
          // the catch block below.
          {
            const forkCwd = process.cwd();
            const runPatchPush = async () => {
              branchName = await handleRemoteBranchCollision(branchName, preserveBranchName, {
                recreateRef,
                githubClient: pushGithubClient,
                owner: pushRepoParts.owner,
                repo: pushRepoParts.repo,
                remoteTarget: pushRemoteUrl || "origin",
                remoteToken: headGitHubToken,
                cwd: forkCwd,
              });

              await pushSignedCommits({
                githubClient: pushGithubClient,
                owner: pushRepoParts.owner,
                repo: pushRepoParts.repo,
                branch: branchName,
                baseRef: `origin/${baseBranch}`,
                cwd: forkCwd,
                pushRemoteUrl,
                pushToken: headGitHubToken,
                signedCommits,
                resolvedTemporaryIds,
                currentRepo: itemRepo,
                validationConfig: config,
              });
            };
            try {
              await runPatchPush();
              core.info("Changes pushed to branch");

              // Count new commits on PR branch relative to base, used to restrict
              // the extra empty CI-trigger commit to exactly 1 new commit.
              try {
                const { stdout: countStr } = await exec.getExecOutput("git", ["rev-list", "--count", `origin/${baseBranch}..HEAD`]);
                newCommitCount = parseInt(countStr.trim(), 10);
                core.info(`${newCommitCount} new commit(s) on branch relative to origin/${baseBranch}`);
              } catch {
                // Non-fatal - newCommitCount stays 0, extra empty commit will be skipped
                core.info("Could not count new commits - extra empty commit will be skipped");
              }
            } catch (pushError) {
              // Push failed - create fallback issue instead of PR (if fallback is enabled)
              core.error(`Git push failed: ${getErrorMessage(pushError)}`);

              if (manifestProtectionFallback) {
                // Push failed specifically for a protected-file modification. Don't create
                // a generic push-failed issue — fall through to the manifestProtectionFallback
                // block below, which will create the proper protected-file review issue with
                // patch artifact download instructions (since the branch was not pushed).
                core.warning("Git push failed for protected-file modification - deferring to protected-file review issue");
                manifestProtectionPushFailedError = pushError;
              } else if (!fallbackAsIssue) {
                // Fallback is disabled - return error without creating issue
                core.error("fallback-as-issue is disabled - not creating fallback issue");
                const error = `Failed to push changes: ${getErrorMessage(pushError)}`;
                return {
                  success: false,
                  error,
                  error_type: "push_failed",
                };
              } else {
                core.warning("Git push operation failed - creating fallback issue instead of pull request");

                const runUrl = buildWorkflowRunUrl(context, context.repo);
                const runId = context.runId;

                // Read patch content for preview
                let patchPreview = "";
                if (patchFilePath && fs.existsSync(patchFilePath)) {
                  let patchContent;
                  try {
                    patchContent = fs.readFileSync(patchFilePath, "utf8");
                  } catch (err) {
                    throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
                  }
                  patchPreview = generatePatchPreview(patchContent);
                }

                const patchFileName = patchFilePath ? patchFilePath.replace("/tmp/gh-aw/", "") : "aw-unknown.patch";
                const recoveryInstructions = buildManualBranchRecoveryCommands({
                  hasBundleFile: false,
                  runId,
                  artifactFileName: patchFileName,
                  branchName,
                  baseBranch,
                });
                const pushFailureMessage = sanitizeContent(neutralizeClosingKeywordsForIssueBody(getErrorMessage(pushError)), { allowedAliases: allowedMentionAliases, maxMentions })
                  .replace(/\s+/g, " ")
                  .trim();
                const pushErrorSection = buildPushErrorSection(getErrorMessage(pushError), pushFailureMessage);
                const fallbackBody = `${issueSafeBody}

---

> [!NOTE]
> This was originally intended as a pull request, but the git push operation failed.
>
${pushErrorSection}
>
> **Workflow Run:** [View run details and download patch artifact](${runUrl})
>
> The patch file is available in the \`agent\` artifact in the workflow run linked above.

<details>
<summary>Create the pull request manually</summary>

\`\`\`sh
${recoveryInstructions}

# Push the branch to the target remote
git push ${shellQuote(pushRemoteUrl || "origin")} ${shellQuote(branchName)}

# Create the pull request
gh pr create --title ${shellQuote(title)} --base ${shellQuote(baseBranch)} --head ${shellQuote(getPullRequestHeadRef(branchName))} --repo ${shellQuote(`${repoParts.owner}/${repoParts.repo}`)}
\`\`\`

</details>
${patchPreview}`;

                try {
                  const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, title, fallbackBody, mergeFallbackIssueLabels(effectiveFallbackLabels), configAssignees);

                  core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);
                  await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);

                  // Update the activation comment with issue link (if a comment was created)
                  //
                  // NOTE: we pass 'github' (global octokit) instead of githubClient (repo-scoped octokit) because the issue is created
                  // in the same repo as the activation, so the global client has the correct context for updating the comment.
                  await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

                  // Write summary to GitHub Actions summary
                  await core.summary
                    .addRaw(
                      `

## Push Failure Fallback
- **Push Error:** ${getErrorMessage(pushError)}
- **Fallback Issue:** [#${issue.number}](${issue.html_url})
- **Patch Artifact:** Available in workflow run artifacts
- **Note:** Push failed, created issue as fallback
`
                    )
                    .write();

                  return {
                    success: true,
                    fallback_used: true,
                    push_failed: true,
                    issue_number: issue.number,
                    issue_url: issue.html_url,
                    branch_name: branchName,
                    repo: itemRepo,
                    head_repo: pushRepo,
                  };
                } catch (issueError) {
                  const error = `Failed to push and failed to create fallback issue. Push error: ${getErrorMessage(pushError)}. Issue error: ${getErrorMessage(issueError)}`;
                  core.error(error);
                  return {
                    success: false,
                    error,
                  };
                }
              } // end else (generic push-failed fallback)
            }
          }
        } else {
          core.info("Skipping patch application (empty patch)");

          // For empty patches with allow-empty, we still need to push the branch
          if (allowEmpty) {
            core.info("allow-empty is enabled - will create branch and push with empty commit");
            // Push the branch with an empty commit to allow PR creation
            try {
              // Create an empty commit to ensure there's a commit difference
              await exec.exec(`git commit --allow-empty -m "Initialize"`);
              core.info("Created empty commit");

              const forkCwd = process.cwd();
              const runEmptyPush = async () => {
                branchName = await handleRemoteBranchCollision(branchName, preserveBranchName, {
                  recreateRef,
                  githubClient: pushGithubClient,
                  owner: pushRepoParts.owner,
                  repo: pushRepoParts.repo,
                  remoteTarget: pushRemoteUrl || "origin",
                  remoteToken: headGitHubToken,
                  cwd: forkCwd,
                });

                await pushSignedCommits({
                  githubClient: pushGithubClient,
                  owner: pushRepoParts.owner,
                  repo: pushRepoParts.repo,
                  branch: branchName,
                  baseRef: `origin/${baseBranch}`,
                  cwd: forkCwd,
                  pushRemoteUrl,
                  pushToken: headGitHubToken,
                  signedCommits,
                  resolvedTemporaryIds,
                  currentRepo: itemRepo,
                  validationConfig: config,
                });
              };
              await runEmptyPush();
              core.info("Empty branch pushed successfully");

              // Count new commits (will be 1 from the Initialize commit)
              try {
                const { stdout: countStr } = await exec.getExecOutput("git", ["rev-list", "--count", `origin/${baseBranch}..HEAD`]);
                newCommitCount = parseInt(countStr.trim(), 10);
                core.info(`${newCommitCount} new commit(s) on branch relative to origin/${baseBranch}`);
              } catch {
                // Non-fatal - newCommitCount stays 0, extra empty commit will be skipped
                core.info("Could not count new commits - extra empty commit will be skipped");
              }
            } catch (pushError) {
              const error = `Failed to push empty branch: ${getErrorMessage(pushError)}`;
              core.error(error);
              return {
                success: false,
                error,
                error_type: "push_failed",
              };
            }
          } else {
            // For empty patches without allow-empty, handle if-no-changes configuration
            const message = "No changes to apply - noop operation completed successfully";

            switch (ifNoChanges) {
              case "error":
                return { success: false, error: "No changes to apply - failing as configured by if-no-changes: error" };

              case "ignore":
                // Silent success - no console output
                return { success: false, skipped: true };

              case "warn":
              default:
                core.warning(message);
                return { success: false, error: message, skipped: true };
            }
          }
        } // end if (!isEmpty) / else patch application block
      } // end else (!hasBundleFile - patch path)

      // Protected file protection – fallback-to-issue path:
      // The patch has been applied (and pushed, unless manifestProtectionPushFailedError is set).
      // Instead of creating a pull request, we create a review issue so a human can carefully
      // inspect the protected file changes before merging.
      // - Normal case (push succeeded): provides a GitHub compare URL to click and create the PR.
      // - Push-failed case: push was rejected (e.g. missing `workflows` permission); provides
      //   patch artifact download instructions instead of the compare URL.
      if (manifestProtectionFallback) {
        const allFound = manifestProtectionFallback;
        const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
        // Use head branch (branchName) for links when push succeeded; fall back to baseBranch
        // for the push-failed case where the head branch is not yet on the remote.
        const branchForLinks = manifestProtectionPushFailedError ? baseBranch : branchName;
        const fileList = buildProtectedFileList(allFound, githubServer, repoParts.owner, repoParts.repo, branchForLinks);

        let fallbackBody;
        if (manifestProtectionPushFailedError) {
          // Push failed — branch not on remote, so compare URL is unavailable.
          // Use the push-failed template with artifact download instructions, matching
          // whichever transport (bundle or format-patch) was actually used to encode the changes.
          const runId = context.runId;
          const artifactFileName = hasBundleFile ? bundleFilePath.replace("/tmp/gh-aw/", "") : patchFilePath ? patchFilePath.replace("/tmp/gh-aw/", "") : "aw-unknown.patch";
          const applyInstructions = buildManualBranchRecoveryCommands({
            hasBundleFile,
            runId,
            artifactFileName,
            branchName,
            baseBranch,
            tempRef: createBundleTempRef(branchName),
          });
          const pushFailedTemplatePath = getPromptPath("manifest_protection_push_failed_fallback.md");
          fallbackBody = renderTemplateFromFile(pushFailedTemplatePath, {
            main_body: issueSafeMainBodyContent,
            footer: footerContent,
            files: fileList,
            apply_instructions: applyInstructions,
            branch_name: branchName,
            base_branch: baseBranch,
            title,
            repo: `${repoParts.owner}/${repoParts.repo}`,
          });
        } else {
          // Normal case — push succeeded, provide compare URL.
          const createPrUrl = buildManifestProtectionCreatePrUrl(githubServer, repoParts, baseBranch, branchName, title, undefined, getPullRequestHeadRef(branchName));
          fallbackBody = renderManifestProtectionFallbackBody(issueSafeMainBodyContent, footerContent, fileList, createPrUrl);
        }

        try {
          const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, title, fallbackBody, mergeFallbackIssueLabels(effectiveFallbackLabels), configAssignees);

          core.info(`Created protected-file-protection review issue #${issue.number}: ${issue.html_url}`);

          if (!manifestProtectionPushFailedError) {
            try {
              const createPrUrl = buildManifestProtectionCreatePrUrl(githubServer, repoParts, baseBranch, branchName, title, issue.number, getPullRequestHeadRef(branchName));
              const fallbackBodyWithCloseKeyword = renderManifestProtectionFallbackBody(issueSafeMainBodyContent, footerContent, fileList, createPrUrl);

              await withRetry(
                () =>
                  githubClient.rest.issues.update({
                    owner: issueRepoParts.owner,
                    repo: issueRepoParts.repo,
                    issue_number: issue.number,
                    body: fallbackBodyWithCloseKeyword,
                  }),
                RATE_LIMIT_RETRY_CONFIG,
                `update protected-file-protection fallback issue #${issue.number} with auto-close link`
              );
            } catch (updateIssueBodyError) {
              core.warning(`Failed to update protected-file-protection fallback issue #${issue.number} with auto-close link: ${getErrorMessage(updateIssueBodyError)}`);
            }
          }

          await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);

          await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

          return {
            success: true,
            fallback_used: true,
            issue_number: issue.number,
            issue_url: issue.html_url,
            branch_name: branchName,
            repo: itemRepo,
            head_repo: pushRepo,
          };
        } catch (issueError) {
          const error = `Protected file protection: failed to create review issue. Error: ${getErrorMessage(issueError)}`;
          core.error(error);
          return { success: false, error };
        }
      }

      // Try to create the pull request, with fallback to issue creation
      try {
        const { data: pullRequest } = await createOrUpdatePullRequest({
          githubClient,
          repoParts,
          title,
          body,
          branchName: getPullRequestHeadRef(branchName),
          baseBranch,
          draft,
        });

        core.info(`Created pull request #${pullRequest.number}: ${pullRequest.html_url}`);

        // Record this pull request so later messages in the same run can stack on top of it.
        // Both the agent-provided branch name and the effective (prefixed/salted) name are keys.
        stackTracker.record({ branch: branchName, number: pullRequest.number, url: pullRequest.html_url, repo: itemRepo }, { agentBranch: originalAgentBranch, baseBranch });

        // Add labels if specified
        if (labels.length > 0) {
          try {
            await withRetry(
              () =>
                githubClient.rest.issues.addLabels({
                  owner: repoParts.owner,
                  repo: repoParts.repo,
                  issue_number: pullRequest.number,
                  labels: labels,
                }),
              {
                maxRetries: LABEL_MAX_RETRIES,
                initialDelayMs: LABEL_INITIAL_DELAY_MS,
                maxDelayMs: LABEL_MAX_DELAY_MS,
                backoffMultiplier: 2,
                shouldRetry: isLabelTransientError,
              },
              `add labels to PR #${pullRequest.number}`
            );
            core.info(`Added labels to pull request: ${JSON.stringify(labels)}`);
          } catch (labelError) {
            // Label addition is non-critical - warn but don't fail the PR creation.
            // GitHub's API may transiently fail to resolve the PR node ID immediately
            // after creation, which causes label operations to fail with an unprocessable error.
            // If this warning appears, repository checks that require labels on the opened event
            // may fail transiently; consider triggering required-label checks on the labeled event instead.
            core.warning(`Failed to add labels to PR #${pullRequest.number}: ${getErrorMessage(labelError)}`);
          }
        }

        // Add configured reviewers if specified
        if (configReviewers.length > 0 || configTeamReviewers.length > 0) {
          const hasCopilot = configReviewers.includes("copilot");
          const otherReviewers = configReviewers.filter(r => r !== "copilot");

          if (otherReviewers.length > 0 || configTeamReviewers.length > 0) {
            core.info(`Requesting reviewers for pull request #${pullRequest.number}: reviewers=${JSON.stringify(otherReviewers)}, team_reviewers=${JSON.stringify(configTeamReviewers)}`);
            try {
              /** @type {{ owner: string, repo: string, pull_number: number, reviewers: string[], team_reviewers?: string[] }} */
              const reviewerRequest = {
                owner: repoParts.owner,
                repo: repoParts.repo,
                pull_number: pullRequest.number,
                reviewers: otherReviewers,
              };
              if (configTeamReviewers.length > 0) {
                reviewerRequest.team_reviewers = configTeamReviewers;
              }
              await githubClient.rest.pulls.requestReviewers(reviewerRequest);
              core.info(`Requested reviewers for pull request #${pullRequest.number}: reviewers=${JSON.stringify(otherReviewers)}, team_reviewers=${JSON.stringify(configTeamReviewers)}`);
            } catch (reviewerError) {
              core.warning(`Failed to request reviewers for PR #${pullRequest.number}: ${getErrorMessage(reviewerError)}`);
            }
          }

          if (hasCopilot) {
            core.info(`Requesting copilot as reviewer for pull request #${pullRequest.number}`);
            try {
              await githubClient.rest.pulls.requestReviewers({
                owner: repoParts.owner,
                repo: repoParts.repo,
                pull_number: pullRequest.number,
                reviewers: [COPILOT_REVIEWER_BOT],
              });
              core.info(`Requested copilot as reviewer for pull request #${pullRequest.number}`);
            } catch (copilotError) {
              core.warning(`Failed to request copilot as reviewer for PR #${pullRequest.number}: ${getErrorMessage(copilotError)}`);
            }
          }
        }

        if (configAssignees && configAssignees.length > 0) {
          core.info(`Assigning assignees to pull request #${pullRequest.number}: ${JSON.stringify(configAssignees)}`);
          try {
            await githubClient.rest.issues.addAssignees({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: pullRequest.number,
              assignees: configAssignees,
            });
            core.info(`Assigned assignees to pull request #${pullRequest.number}: ${JSON.stringify(configAssignees)}`);
          } catch (assigneeError) {
            core.warning(`Failed to assign assignees to PR #${pullRequest.number}: ${getErrorMessage(assigneeError)}`);
          }
        }

        const requestChangesSections = [];
        if (manifestProtectionRequestReview && manifestProtectionRequestReview.length > 0) {
          const protectedFilesReviewTemplatePath = getPromptPath("manifest_protection_request_changes_review.md");
          requestChangesSections.push(
            renderTemplateFromFile(protectedFilesReviewTemplatePath, {
              files: renderFilesList(manifestProtectionRequestReview),
            })
          );
        }
        if (detectionCaution) {
          const detectionReason = process.env.GH_AW_DETECTION_REASON || "unknown";
          const detectionWarningReviewTemplatePath = getPromptPath("threat_warning_request_changes_review.md");
          requestChangesSections.push(
            renderTemplateFromFile(detectionWarningReviewTemplatePath, {
              detectionReason,
              runUrl,
            })
          );
        }
        if (requestChangesSections.length > 0) {
          const requestChangesBody = requestChangesSections.join("\n\n---\n\n");
          /** @type {{ owner: string, repo: string, pull_number: number, event: "REQUEST_CHANGES" | "COMMENT", body: string, commit_id?: string }} */
          const requestChangesParams = {
            owner: repoParts.owner,
            repo: repoParts.repo,
            pull_number: pullRequest.number,
            event: "REQUEST_CHANGES",
            body: requestChangesBody,
          };
          if (pullRequest.head && pullRequest.head.sha) {
            requestChangesParams.commit_id = pullRequest.head.sha;
          }
          core.info(`Creating REQUEST_CHANGES review for PR #${pullRequest.number} due to protected files`);
          try {
            await withRetry(() => githubClient.rest.pulls.createReview(requestChangesParams), RATE_LIMIT_RETRY_CONFIG, `create REQUEST_CHANGES review for PR #${pullRequest.number}`);
            core.info(`Created REQUEST_CHANGES review for PR #${pullRequest.number}`);
          } catch (requestChangesError) {
            const requestChangesErrorMessage = getErrorMessage(requestChangesError);
            const ownPrMessages = ["Can not request changes on your own pull request"];
            if (ownPrMessages.some(msg => requestChangesErrorMessage.includes(msg))) {
              core.warning(`Cannot submit REQUEST_CHANGES on own PR #${pullRequest.number}. Retrying with COMMENT.`);
              try {
                const commentReviewParams = { ...requestChangesParams, event: "COMMENT" };
                await withRetry(() => githubClient.rest.pulls.createReview(commentReviewParams), RATE_LIMIT_RETRY_CONFIG, `create COMMENT review fallback for PR #${pullRequest.number}`);
                core.info(`Created COMMENT review fallback for PR #${pullRequest.number}`);
              } catch (commentReviewError) {
                core.warning(`Failed to create COMMENT review fallback for PR #${pullRequest.number}: ${getErrorMessage(commentReviewError)}`);
              }
            } else {
              core.warning(`Failed to create REQUEST_CHANGES review for PR #${pullRequest.number}: ${requestChangesErrorMessage}`);
            }
          }
        }

        // Enable auto-merge if configured
        if (autoMerge) {
          try {
            await githubClient.graphql(
              `mutation($prId: ID!, $mergeMethod: PullRequestMergeMethod) {
              enablePullRequestAutoMerge(input: {pullRequestId: $prId, mergeMethod: $mergeMethod}) {
                pullRequest {
                  id
                }
              }
            }`,
              {
                prId: pullRequest.node_id,
                mergeMethod: autoMergeMethod,
              }
            );
            core.info(`Enabled auto-merge for pull request #${pullRequest.number}`);
          } catch (autoMergeError) {
            core.warning(`Failed to enable auto-merge for PR #${pullRequest.number}: ${getErrorMessage(autoMergeError)}`);
          }
        }

        // Update the activation comment with PR link (if a comment was created)
        //
        // NOTE: we pass 'github' (global octokit) instead of githubClient (repo-scoped octokit) because the issue is created
        // in the same repo as the activation, so the global client has the correct context for updating the comment.
        await updateActivationComment(github, context, core, pullRequest.html_url, pullRequest.number);

        // Close older pull requests if enabled (best-effort: errors are logged but do not fail the workflow)
        if (closeOlderPullRequestsEnabled) {
          if (workflowId || closeOlderKey) {
            const searchKey = closeOlderKey ? `gh-aw-close-key: ${closeOlderKey}` : `workflow-id: ${workflowId}`;
            core.info(`Attempting to close older pull requests for ${repoParts.owner}/${repoParts.repo}#${pullRequest.number} using ${searchKey}`);
            try {
              const closedPRs = await closeOlderPullRequests(github, repoParts.owner, repoParts.repo, workflowId, { number: pullRequest.number, html_url: pullRequest.html_url }, workflowName, runUrl, callerWorkflowId, closeOlderKey);
              if (closedPRs.length > 0) {
                core.info(`Closed ${closedPRs.length} older pull request(s)`);
              }
            } catch (error) {
              // Log error but don't fail the workflow
              core.warning(`Failed to close older pull requests: ${getErrorMessage(error)}`);
            }
          } else {
            core.warning("Close older pull requests enabled but neither GH_AW_WORKFLOW_ID nor close-older-key is set - skipping");
          }
        }

        // Write summary to GitHub Actions summary
        await core.summary
          .addRaw(
            `

## Pull Request
- **Pull Request**: [#${pullRequest.number}](${pullRequest.html_url})
- **Branch**: \`${branchName}\`
- **Base Branch**: \`${baseBranch}\`
`
          )
          .write();

        // Push an extra empty commit if a token is configured and exactly 1 new commit was pushed.
        // This works around the GITHUB_TOKEN limitation where pushes don't trigger CI events.
        // Restricting to exactly 1 new commit prevents the CI trigger token being used on
        // multi-commit branches where workflow files may have been iteratively modified.
        const ciTriggerResult = await pushExtraEmptyCommit({
          branchName,
          repoOwner: pushRepoParts.owner,
          repoName: pushRepoParts.repo,
          newCommitCount,
        });
        if (ciTriggerResult.success && !ciTriggerResult.skipped) {
          core.info("Extra empty commit pushed - CI checks should start shortly");
        }

        // Return success with PR details
        return {
          success: true,
          number: pullRequest.number,
          url: pullRequest.html_url,
          managedBody: body,
          branch_name: branchName,
          temporaryId: temporaryId,
          repo: itemRepo,
          head_repo: pushRepo,
        };
      } catch (prError) {
        const errorMessage = getErrorMessage(prError);
        core.warning(`Failed to create pull request: ${errorMessage}`);

        // Check if the error is the specific "GitHub actions is not permitted to create or approve pull requests" error
        if (errorMessage.includes("GitHub Actions is not permitted to create or approve pull requests")) {
          core.error(`Permission error: GitHub Actions is not permitted to create or approve pull requests. See FAQ: ${FAQ_CREATE_PR_PERMISSIONS_URL}`);

          // Branch has already been pushed - create a fallback issue with a link to create the PR via GitHub UI
          const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
          // Encode branch name path segments individually to preserve '/' while encoding other special characters
          const encodedBase = baseBranch.split("/").map(encodeURIComponent).join("/");
          const encodedHead = getPullRequestHeadRef(branchName).split("/").map(encodeURIComponent).join("/");
          const createPrUrl = `${githubServer}/${repoParts.owner}/${repoParts.repo}/compare/${encodedBase}...${encodedHead}?expand=1&title=${encodeURIComponent(title)}`;

          // Read patch content for preview
          let patchPreview = "";
          if (patchFilePath && fs.existsSync(patchFilePath)) {
            let patchContent;
            try {
              patchContent = fs.readFileSync(patchFilePath, "utf8");
            } catch (err) {
              throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
            }
            patchPreview = generatePatchPreview(patchContent);
          }

          const fallbackTemplatePath = getPromptPath("pr_permission_denied_fallback.md");
          const fallbackBody = renderTemplateFromFile(fallbackTemplatePath, {
            main_body: issueSafeMainBodyContent,
            branch_name: branchName,
            create_pr_url: createPrUrl,
            faq_url: FAQ_CREATE_PR_PERMISSIONS_URL,
            patch_preview: patchPreview,
            footer: footerContent,
          });

          try {
            const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, title, fallbackBody, mergeFallbackIssueLabels(effectiveFallbackLabels), configAssignees);

            core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);
            await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);

            await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

            return {
              success: true,
              fallback_used: true,
              issue_number: issue.number,
              issue_url: issue.html_url,
              branch_name: branchName,
              repo: itemRepo,
              head_repo: pushRepo,
            };
          } catch (issueError) {
            const error = `Failed to create pull request (permission denied) and failed to create fallback issue. PR error: ${errorMessage}. Issue error: ${getErrorMessage(issueError)}`;
            core.error(error);
            return {
              success: false,
              error,
              error_type: "permission_denied",
            };
          }
        }

        if (!fallbackAsIssue) {
          // Fallback is disabled - return error without creating issue
          core.error("fallback-as-issue is disabled - not creating fallback issue");
          return {
            success: false,
            error: errorMessage,
            error_type: "pr_creation_failed",
          };
        }

        core.info("Falling back to creating an issue instead");

        // Create issue as fallback with enhanced body content
        const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
        const branchUrl = `${githubServer}/${pushRepoParts.owner}/${pushRepoParts.repo}/tree/${branchName}`;

        // Read patch content for preview
        let patchPreview = "";
        if (patchFilePath && fs.existsSync(patchFilePath)) {
          let patchContent;
          try {
            patchContent = fs.readFileSync(patchFilePath, "utf8");
          } catch (err) {
            throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
          }
          patchPreview = generatePatchPreview(patchContent);
        }

        const fallbackBody = `${issueSafeBody}

---

> [!NOTE]
> This was originally intended as a pull request, but PR creation failed. The changes have been pushed to the branch [\`${branchName}\`](${branchUrl}).
>
> **Original error:** ${errorMessage}

To create the pull request manually:

\`\`\`sh
gh pr create --title "${title}" --base ${baseBranch} --head ${getPullRequestHeadRef(branchName)} --repo ${repoParts.owner}/${repoParts.repo}
\`\`\`
${patchPreview}`;

        try {
          const { data: issue, issueRepoParts } = await createFallbackIssue(githubClient, repoParts, title, fallbackBody, mergeFallbackIssueLabels(effectiveFallbackLabels), configAssignees);

          core.info(`Created fallback issue #${issue.number}: ${issue.html_url}`);
          await assignCopilotToFallbackIssueIfEnabled(issueRepoParts.owner, issueRepoParts.repo, issue.number);

          // Update the activation comment with issue link (if a comment was created)
          // NOTE: we pass 'github' (global octokit) instead of githubClient (repo-scoped octokit) because the issue is created
          // in the same repo as the activation, so the global client has the correct context for updating the comment.
          await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");

          // Return success with fallback flag
          return {
            success: true,
            fallback_used: true,
            issue_number: issue.number,
            issue_url: issue.html_url,
            branch_name: branchName,
            repo: itemRepo,
            head_repo: pushRepo,
          };
        } catch (issueError) {
          const error = `Failed to create both pull request and fallback issue. PR error: ${errorMessage}. Issue error: ${getErrorMessage(issueError)}`;
          core.error(error);
          return {
            success: false,
            error,
          };
        }
      }
    } finally {
      // Restore original working directory after multi-repo subdirectory operations
      if (repoCwd) {
        process.chdir(originalCwd);
      }
    }
  }; // End of handleCreatePullRequest
} // End of main

module.exports = {
  main,
  enforcePullRequestLimits,
  countUniquePatchFiles,
  parseDiffGitHeader,
  applyBundleToBranch,
  rewriteBundleBranchAsSingleCommit,
  parseAutoMergeConfig,
};
