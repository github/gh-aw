// @ts-check
/// <reference types="@actions/github-script" />

/** @type {typeof import("fs")} */
const fs = require("fs");
const path = require("path");
const { generateStagedPreview } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { pushSignedCommits } = require("./push_signed_commits.cjs");
const { updateActivationCommentWithCommit, updateActivationComment } = require("./update_activation_comment.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { pushExtraEmptyCommit } = require("./extra_empty_commit.cjs");
const { detectForkPR, checkBranchPushable } = require("./pr_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { checkFileProtection, checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");
const { POLICY_FILE_PROTECTION_DENIED_REASON_CODE } = require("./error_codes.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { renderTemplateFromFile, buildProtectedFileList, getPromptPath } = require("./messages_core.cjs");
const { withGitHubHostToken } = require("./git_auth_helpers.cjs");
const { ensureFullHistoryForBundle, extractBundlePrerequisiteCommits, isShallowOrSparseCheckout, linearizeRangeAsCommit, ensureSafeDirectoryTrust } = require("./git_helpers.cjs");
const { extractPatchBaseCommit } = require("./commit_sha_helpers.cjs");
const { findRepoCheckout } = require("./find_repo_checkout.cjs");
const { getThreatWarningPresentation } = require("./threat_detection_warning.cjs");
const { attachExecutionState } = require("./safe_output_execution_metadata.cjs");
const { resolveTransportPaths } = require("./resolve_transport_paths.cjs");
const { buildManualBranchApplyCommands } = require("./create_pull_request_helpers.cjs");

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "push_to_pull_request_branch";
const MISSING_BRANCH_ERROR_TEMPLATE = branchName => `Branch ${branchName} no longer exists on origin (it may have been deleted), can't push to it.`;
const MISSING_REMOTE_REF_PATTERNS = [
  "couldn't find remote ref",
  "could not find remote ref",
  "remote ref does not exist",
  "did not match any file(s) known to git",
  "unknown revision or path not in the working tree",
  "fatal: couldn't find remote ref",
  "exit code 128",
];
/**
 * @param {unknown} value
 * @returns {boolean}
 */
function looksLikeMissingRemoteBranchError(value) {
  const text = String(value ?? "").toLowerCase();
  return MISSING_REMOTE_REF_PATTERNS.some(pattern => text.includes(pattern));
}

/**
 * @param {unknown} rawAwContext
 * @returns {{ item_type: string, item_number: number | null } | null}
 */
function parseAwContext(rawAwContext) {
  /**
   * @param {unknown} parsed
   * @returns {{ item_type: string, item_number: number | null } | null}
   */
  function validateAndNormalizeParsedContext(parsed) {
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return null;
    }
    const parsedObj = /** @type {Record<string, unknown>} */ parsed;
    const itemTypeValue = parsedObj["item_type"];
    const itemNumberValue = parsedObj["item_number"];
    const itemType = typeof itemTypeValue === "string" ? itemTypeValue : "";
    const itemNumber = parsePositiveInteger(itemNumberValue);
    return { item_type: itemType, item_number: itemNumber };
  }

  if (rawAwContext == null) {
    return null;
  }
  if (typeof rawAwContext === "string") {
    const trimmed = rawAwContext.trim();
    if (!trimmed) {
      return null;
    }
    try {
      const parsed = JSON.parse(trimmed);
      return validateAndNormalizeParsedContext(parsed);
    } catch {
      return null;
    }
  }
  return validateAndNormalizeParsedContext(rawAwContext);
}

/**
 * Parses a value into a positive integer.
 *
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
 * Uses git as the source of truth for the files modified by a fetched bundle ref.
 *
 * @param {{ getExecOutput: (command: string, args?: string[], options?: any) => Promise<{ stdout: string }> }} exec
 * @param {Record<string, unknown>} gitOptions
 * @param {string} rangeBaseRef
 * @param {string} bundleRef
 * @returns {Promise<string[]>}
 */
async function getBundlePreApplyFiles(exec, gitOptions, rangeBaseRef, bundleRef) {
  const bundleDiffResult = await exec.getExecOutput("git", ["diff", "--name-only", "--no-renames", "-z", `${rangeBaseRef}..${bundleRef}`], gitOptions);
  return bundleDiffResult.stdout.split("\0").filter(Boolean);
}

async function fetchBundlePrerequisites(exec, core, gitAuthEnv, baseGitOpts, prerequisiteCommits, logPrefix = "") {
  core.warning(`${logPrefix}bundle fetch failed due to ${prerequisiteCommits.length} missing prerequisite commit(s); fetching prerequisites from origin and retrying`);
  core.info(`${logPrefix}fetching ${prerequisiteCommits.length} prerequisite commit(s) from origin`);
  // Use --filter=blob:none only when the local repo is already shallow or sparse —
  // in a full clone we already have all blobs and must not convert the repo to a
  // partial clone (which would trigger lazy blob fetches on later operations).
  const prereqGitOpts = { env: { ...process.env, ...gitAuthEnv }, ...baseGitOpts };
  const useBlobFilter = await isShallowOrSparseCheckout(exec, prereqGitOpts);
  const prerequisiteFetchArgs = useBlobFilter ? ["fetch", "--filter=blob:none", "origin", ...prerequisiteCommits] : ["fetch", "origin", ...prerequisiteCommits];
  if (useBlobFilter) {
    core.info(`${logPrefix}using --filter=blob:none for prerequisite fetch (shallow or sparse checkout detected)`);
  }
  await exec.exec("git", prerequisiteFetchArgs, prereqGitOpts);
  core.info(`${logPrefix}fetched prerequisite commits from origin successfully`);
}

/**
 * Measure the expanded blob size of files changed by the applied agent commits.
 * Deleted files contribute zero bytes because no new content is being introduced.
 *
 * @param {Record<string, unknown>} gitOptions
 * @param {string[]} files
 * @returns {number}
 */
function getChangedBlobSizeBytes(gitOptions, files) {
  let total = 0;
  const cwd = typeof gitOptions.cwd === "string" && gitOptions.cwd ? gitOptions.cwd : process.cwd();
  const resolvedCwd = path.resolve(cwd);
  for (const file of files) {
    const filePath = path.resolve(resolvedCwd, file);
    if (filePath !== resolvedCwd && !filePath.startsWith(`${resolvedCwd}${path.sep}`)) {
      continue;
    }
    try {
      const stats = fs.statSync(filePath);
      if (stats.isFile()) {
        total += stats.size;
      }
    } catch {
      // Stat failure is ignored: deleted files have no expanded content to add to the limit.
    }
  }
  return total;
}

/**
 * Checks if a git push stderr output indicates that the 'workflows' scope is required.
 * GitHub rejects branch pushes that contain .github/workflows/** changes when the token
 * lacks the 'workflows' scope, producing one of two known error message variants.
 *
 * @param {string} stderr - The captured stderr from a failed git push
 * @returns {boolean} true when the rejection is due to missing 'workflows' scope
 */
function isWorkflowsScopeRejection(stderr) {
  if (!stderr) return false;
  const lower = stderr.toLowerCase();
  return lower.includes("`workflows` scope") || lower.includes("workflow can be created or updated due to timeout");
}

/**
 * Returns the list of unique workflow file paths (.github/workflows/**) present in the
 * local branch history beyond the PR's base branch.  This is used as a pre-flight check
 * before pushing a new branch ref: GitHub rejects such pushes when the token lacks the
 * 'workflows' scope, even if the current changeset itself does not touch workflow files
 * (the rejection is based on ALL commits reachable from the pushed ref).
 *
 * Uses `origin/${baseBranch}` as the exclusion baseline so that commits already on the
 * PR's target branch (which GitHub has already accepted) are excluded.  Falls back to
 * `origin/HEAD` when `baseBranch` is not available.  In shallow PR checkouts where the
 * named remote ref is not fetched, falls back to `GITHUB_BASE_SHA` (always present as a
 * commit object in GitHub Actions `pull_request` events).  Returns an empty array only
 * when all baselines are exhausted — in that case the push is still attempted and any
 * real 'workflows' scope rejection will be caught and surfaced as the typed error
 * downstream.
 *
 * Note: `origin/${baseBranch}` and `origin/HEAD` are intentionally different baselines
 * for their respective layers.  `origin/${baseBranch}` limits detection to commits the
 * agent actually introduced (correct for the PR delta).  Using `origin/HEAD` here would
 * traverse commits on the target branch itself for PRs targeting non-default branches,
 * producing false-positive `workflows_scope_required` errors.
 *
 * @param {{ getExecOutput: Function }} exec - @actions/exec module (or compatible mock)
 * @param {Record<string, any>} gitOptions - Base git exec options (cwd, env, etc.)
 * @param {string | undefined} baseBranch - PR base branch name (e.g. "main"); falls back to origin/HEAD when not provided
 * @param {typeof core} coreLogger - Actions core logger used for debug output
 * @returns {Promise<string[]>} Unique workflow file paths found in the branch history
 */
async function detectWorkflowFileChanges(exec, gitOptions, baseBranch, coreLogger) {
  const primary = baseBranch && baseBranch.trim() ? `origin/${baseBranch}` : "origin/HEAD";
  const baselines = [primary];
  const githubBaseSha = process.env.GITHUB_BASE_SHA;
  if (githubBaseSha && githubBaseSha.trim()) {
    baselines.push(githubBaseSha.trim());
  }

  for (const baseline of baselines) {
    try {
      const result = await exec.getExecOutput("git", ["log", "--name-only", "--pretty=format:", "HEAD", "--not", baseline, "--", ".github/workflows/"], { ...gitOptions, ignoreReturnCode: true });
      if (result.exitCode !== 0) {
        // Non-zero exit means the baseline ref was not resolvable (e.g. shallow clone
        // without the named remote ref fetched); try the next fallback.
        coreLogger.debug(`detectWorkflowFileChanges: git log exited ${result.exitCode} (baseline '${baseline}' may be unavailable); trying next fallback`);
        continue;
      }
      const files = [
        ...new Set(
          result.stdout
            .split("\n")
            .map(f => f.trim())
            .filter(Boolean)
        ),
      ];
      if (baseline !== primary) {
        coreLogger.debug(`detectWorkflowFileChanges: used fallback baseline '${baseline}'; found ${files.length} workflow file(s)`);
      }
      return files;
    } catch (err) {
      coreLogger.debug(`detectWorkflowFileChanges: git log threw (baseline '${baseline}'): ${getErrorMessage(err)}; trying next fallback`);
    }
  }

  coreLogger.debug(`detectWorkflowFileChanges: all baselines exhausted; skipping pre-flight`);
  return [];
}

/**
 * Performs a pre-flight workflow-scope check before pushing a new branch ref.
 * Returns a non-fatal skip result when the branch history contains workflow file changes
 * but the agent's own changeset has none (scope requirement originates from pre-existing
 * commits).  Returns a hard typed error only when the agent itself staged workflow files.
 * Returns null when the push may proceed.
 *
 * Extracts the duplicated guard that appears in both the review-branch and
 * fallback-branch push paths so future changes only need to be made in one place.
 *
 * @param {{ getExecOutput: Function }} exec - @actions/exec module (or compatible mock)
 * @param {Record<string, any>} gitOptions - Base git exec options (cwd, env, etc.)
 * @param {boolean} allowWorkflows - Whether the push token has the 'workflows' scope
 * @param {string | undefined} baseBranch - PR base branch name passed through to detectWorkflowFileChanges
 * @param {string} context - Short label for the push path (e.g. "Review branch", "Fallback branch")
 * @param {typeof core} coreLogger - Actions core logger
 * @param {string[] | undefined} agentChangedFiles - Files from the agent's post-apply diff; used to distinguish agent vs pre-existing workflow changes.
 *   Pass `undefined` when the distinction cannot be made — the function will fall back to the original hard-error behavior.
 * @returns {Promise<{ success: false, error_type: string, error: string } | { success: false, skipped: true, error: string } | null>}
 */
async function runWorkflowScopePreflightCheck(exec, gitOptions, allowWorkflows, baseBranch, context, coreLogger, agentChangedFiles) {
  if (allowWorkflows) return null;
  const workflowFiles = await detectWorkflowFileChanges(exec, gitOptions, baseBranch, coreLogger);
  if (workflowFiles.length > 0) {
    coreLogger.info(`Pre-flight check: branch history contains workflow file changes (${workflowFiles.join(", ")}). Failing before push attempt.`);
    if (agentChangedFiles !== undefined) {
      const agentWorkflowFiles = agentChangedFiles.filter(f => f.startsWith(".github/workflows/"));
      if (agentWorkflowFiles.length === 0) {
        return buildWorkflowsScopeSkip(context, coreLogger);
      }
    }
    return buildWorkflowsScopeError(`${context} pre-flight`, coreLogger);
  }
  return null;
}

/**
 * Builds the typed result and logs actionable guidance when a branch push fails
 * because the token lacks the 'workflows' scope.
 *
 * @param {string} context - Short label identifying the push path (e.g. "Review branch", "Fallback branch")
 * @param {typeof core} coreLogger - Actions core logger
 * @returns {{ success: false, error_type: "workflows_scope_required", error: string }}
 */
function buildWorkflowsScopeError(context, coreLogger) {
  coreLogger.error(`${context} push rejected: the branch includes changes to workflow files (.github/workflows/**) that require the 'workflows' scope on the push token.`);
  coreLogger.error("To allow this workflow to push workflow file changes, configure 'push-to-pull-request-branch.allow-workflows: true' together with a GitHub App in 'safe-outputs.github-app'.");
  return {
    success: false,
    error_type: "workflows_scope_required",
    error: `${context} push rejected: the branch includes changes to workflow files (.github/workflows/**) requiring the 'workflows' scope. The token used for the safe-outputs checkout does not have this scope. Fix: configure 'push-to-pull-request-branch.allow-workflows: true' with a GitHub App in 'safe-outputs.github-app', or exclude workflow files from the changeset.`,
  };
}

/**
 * Builds a non-fatal skip result and emits a warning when the branch history requires
 * the 'workflows' scope but the scope requirement originates from pre-existing commits
 * rather than the agent's own changeset.
 *
 * @param {string} context - Short label identifying the push path (e.g. "Review branch", "Fallback branch")
 * @param {typeof core} coreLogger - Actions core logger
 * @returns {{ success: false, skipped: true, error: string }}
 */
function buildWorkflowsScopeSkip(context, coreLogger) {
  const message =
    `${context}: branch history contains workflow file changes (.github/workflows/**) requiring the 'workflows' scope, ` +
    `but the agent's own changeset does not include workflow files — the scope requirement originates from pre-existing commits. ` +
    `Skipping push to avoid a scope rejection. ` +
    `To allow pushing workflow file changes, configure 'push-to-pull-request-branch.allow-workflows: true' with a GitHub App in 'safe-outputs.github-app'.`;
  coreLogger.warning(message);
  return { success: false, skipped: true, error: message };
}

/**
 * Main handler factory for push_to_pull_request_branch
 * Returns a message handler function that processes individual push_to_pull_request_branch messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration from config parameter
  const target = config.target || "triggering";
  const titlePrefix = config.title_prefix || "";
  const rawRequiredLabels = config.required_labels ?? config.labels;
  const envLabels = rawRequiredLabels ? (Array.isArray(rawRequiredLabels) ? rawRequiredLabels : rawRequiredLabels.split(",")).map(label => String(label).trim()).filter(label => label) : [];
  const ifNoChanges = config.if_no_changes || "warn";
  const ignoreMissingBranchFailure = config.ignore_missing_branch_failure === true;
  const fallbackAsPullRequest = config.fallback_as_pull_request !== false;
  const checkBranchProtection = config.check_branch_protection !== false;
  const signedCommits = config.signed_commits !== false;
  const commitTitleSuffix = config.commit_title_suffix || "";
  const maxSizeKb = parsePositiveInteger(config.max_patch_size) ?? 4096;
  const maxCount = config.max || 0; // 0 means no limit
  const allowWorkflows = config.allow_workflows === true;

  // Cross-repo support: resolve target repository from config
  // This allows pushing to PRs in a different repository than the workflow
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Ensure the workspace is trusted in the bridge process (Process Safe Outputs step).
  // The bridge runs outside the Docker container as a potentially different user/HOME,
  // so the in-container `git config --global safe.directory` may not be visible here.
  // Using GIT_CONFIG_* env vars avoids relying on ~/.gitconfig and covers cases where
  // the conditional "Configure Git credentials" step was skipped or HOME differs.
  ensureSafeDirectoryTrust(process.env.GITHUB_WORKSPACE || process.cwd());

  // Git network operations authenticate using the credentials actions/checkout
  // persisted into .git/config for the safe_outputs job (persist-credentials: true,
  // using the resolved push token). We intentionally do NOT inject an additional
  // http.extraheader via GIT_CONFIG_* here: doing so duplicates the Authorization
  // header already present in .git/config and causes the server to reject the request
  // with "Duplicate header: Authorization" (HTTP 400) on git fetch/push. GitHub API
  // ("gh") operations authenticate separately via the authenticated Octokit client above.
  const gitAuthEnv = {};

  // Base branch from config (if set) - used only for logging at factory level
  // Dynamic base branch resolution happens per-message after resolving the actual target repo
  const configBaseBranch = config.base_branch || null;
  const configuredHeadRepo = typeof config["head-repo"] === "string" ? config["head-repo"].trim() : "";
  const headGitHubToken = typeof config["head-github-token"] === "string" ? config["head-github-token"].trim() : "";

  // Check if we're in staged mode (either globally or per-handler config)
  const isStaged = isStagedMode(config);

  core.info(`Target: ${target}`);
  if (configBaseBranch) {
    core.info(`Base branch (from config): ${configBaseBranch}`);
  }
  if (titlePrefix) {
    core.info(`Title prefix: ${titlePrefix}`);
  }
  if (envLabels.length > 0) {
    core.info(`Required labels: ${envLabels.join(", ")}`);
  }
  core.info(`If no changes: ${ifNoChanges}`);
  core.info(`Ignore missing branch failure: ${ignoreMissingBranchFailure}`);
  core.info(`Fallback as pull request: ${fallbackAsPullRequest}`);
  core.info(`Check branch protection: ${checkBranchProtection}`);
  core.info(`Push signed commits: ${signedCommits}`);
  if (commitTitleSuffix) {
    core.info(`Commit title suffix: ${commitTitleSuffix}`);
  }
  core.info(`Max patch size: ${maxSizeKb} KB`);
  core.info(`Max count: ${maxCount || "unlimited"}`);
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (configuredHeadRepo) {
    core.info(`Configured head repo: ${configuredHeadRepo}`);
  }
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${[...allowedRepos].join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function - processes individual push_to_pull_request_branch messages
   * @param {any} message - The push_to_pull_request_branch message to process
   * @param {import('./types/handler-factory').ResolvedTemporaryIds} resolvedTemporaryIds - Map of temporary IDs to resolved IDs
   * @returns {Promise<import('./types/handler-factory').HandlerResult>}
   */
  return async function handlePushToPullRequestBranch(message, resolvedTemporaryIds) {
    // Check max count
    if (maxCount > 0 && processedCount >= maxCount) {
      core.info(`Skipping message - max count (${maxCount}) reached`);
      return { success: false, error: `Max count (${maxCount}) reached`, skipped: true };
    }

    processedCount++;

    // Determine the patch and bundle file paths. The MCP server sets these on
    // the entry it writes, but the validation step strips them as a defense
    // against agent-forged values. Recover them by re-deriving from `branch`.
    const transportPaths = resolveTransportPaths(message, defaultTargetRepo);
    const patchFilePath = transportPaths.patchPath;
    core.info(`Patch file path: ${patchFilePath || "(not set)"}`);

    // Determine the bundle file path from the message (set when patch-format: bundle is configured)
    const bundleFilePath = transportPaths.bundlePath;
    if (bundleFilePath) {
      core.info(`Bundle file path: ${bundleFilePath}`);
    }

    // Check if bundle or patch file exists
    const hasBundleFile = !!(bundleFilePath && fs.existsSync(bundleFilePath));
    const hasPatchFile = !!(patchFilePath && fs.existsSync(patchFilePath));
    const applyTransport = hasBundleFile ? "bundle" : "patch";
    core.info(`Apply transport mode: ${applyTransport} (patch file present: ${hasPatchFile}, bundle file present: ${hasBundleFile})`);
    if (bundleFilePath && !hasBundleFile) {
      core.warning(`Bundle file path was provided but file is not present on disk: ${bundleFilePath}; falling back to patch transport`);
    }

    // Always require a patch file. The patch remains the preview/debug artifact and
    // the first-pass validation input; bundle transport adds an authoritative
    // pre-apply git diff check later after the bundle ref has been fetched.
    if (!hasPatchFile) {
      const msg = "No patch file found - cannot push without changes";

      switch (ifNoChanges) {
        case "error":
          return { success: false, error: msg };
        case "ignore":
          return { success: false, error: msg, skipped: true };
        case "warn":
        default:
          core.info(msg);
          return { success: false, error: msg, skipped: true };
      }
    }

    let patchContent;
    try {
      patchContent = fs.readFileSync(patchFilePath, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
    }

    // Check for actual error conditions
    if (patchContent.includes("Failed to generate patch")) {
      const msg = "Patch file contains error message - cannot push without changes";
      core.error("Patch file generation failed");
      core.error(`Patch file location: ${patchFilePath}`);
      core.error(`Patch file size: ${Buffer.byteLength(patchContent, "utf8")} bytes`);
      const previewLength = Math.min(500, patchContent.length);
      core.error(`Patch file preview (first ${previewLength} characters):`);
      core.error(patchContent.substring(0, previewLength));
      return { success: false, error: msg };
    }
    const isEmpty = !patchContent || !patchContent.trim();
    // Validate patch/bundle size against `max_patch_size`.
    //
    // Size-check source of truth, in order of preference:
    // Use the uncompressed patch representation for both transport modes.
    // Bundle size is compressed and can undercount highly compressible changes.
    if (!isEmpty) {
      const patchSizeBytes = Buffer.byteLength(patchContent, "utf8");
      const patchSizeKb = Math.ceil(patchSizeBytes / 1024);

      let bundleSizeBytes = 0;
      if (hasBundleFile) {
        try {
          bundleSizeBytes = fs.statSync(bundleFilePath).size;
        } catch (statErr) {
          core.warning(`Failed to stat bundle file for size check: ${getErrorMessage(statErr)}`);
        }
      }
      const bundleSizeKb = Math.ceil(bundleSizeBytes / 1024);

      const sizeForCheckBytes = patchSizeBytes;
      const sizeLabel = "Patch size";
      const sizeForCheckKb = Math.ceil(sizeForCheckBytes / 1024);

      if (hasBundleFile) {
        core.info(`Bundle file size: ${bundleSizeKb} KB`);
      } else {
        core.info(`Patch file size: ${patchSizeKb} KB`);
      }
      core.info(`${sizeLabel}: ${sizeForCheckKb} KB (maximum allowed: ${maxSizeKb} KB)`);

      if (sizeForCheckKb > maxSizeKb) {
        let msg;
        msg = `Patch size (${sizeForCheckKb} KB) exceeds maximum allowed size (${maxSizeKb} KB)`;
        return { success: false, error: msg };
      }

      core.info("Patch size validation passed");
    }

    // Check file protection: allowlist (strict) or protected-files policy.
    // Fallback-to-issue detection is deferred until after PR metadata is resolved below.
    /** @type {string[] | null} Protected files found in the patch (manifest basenames + path-prefix matches) */
    let protectedFilesForFallback = null;
    if (!isEmpty) {
      const protection = checkFileProtection(patchContent, config);
      if (protection.action === "deny") {
        const filesStr = protection.files.join(", ");
        const msg =
          protection.source === "allowlist"
            ? `Cannot push to pull request branch: patch modifies files outside the allowed-files list (${filesStr}). Add the files to the allowed-files configuration field or remove them from the patch.`
            : `Cannot push to pull request branch: patch modifies protected files (${filesStr}). Add them to the allowed-files configuration field or set protected-files: fallback-to-issue to create a review issue instead.`;
        core.warning(msg);
        return { success: false, skipped: true, reasonCode: POLICY_FILE_PROTECTION_DENIED_REASON_CODE, error: msg };
      }
      if (protection.action === "fallback") {
        protectedFilesForFallback = protection.files;
        core.warning(`Protected file protection triggered (fallback-to-issue): ${protection.files.join(", ")}. Will create review issue instead of pushing.`);
      }
    }

    if (isEmpty) {
      const msg = "Patch file is empty - no changes to apply (noop operation)";

      switch (ifNoChanges) {
        case "error":
          return { success: false, error: "No changes to push - failing as configured by if-no-changes: error" };
        case "ignore":
          return { success: false, error: msg, skipped: true };
        case "warn":
        default:
          core.info(msg);
          return { success: false, error: msg, skipped: true };
      }
    }

    core.info("Patch content validation passed");
    core.info(`Target configuration: ${target}`);

    // If in staged mode, emit 🎭 Staged Mode Preview via generateStagedPreview
    if (isStaged) {
      await generateStagedPreview({
        title: "Push to PR Branch",
        description: "The following changes would be pushed if staged mode was disabled:",
        items: [{ target, commit_message: message.commit_message }],
        renderItem: item => {
          let content = `**Target:** ${item.target}\n\n`;

          if (item.commit_message) {
            content += `**Commit Message:** ${item.commit_message}\n\n`;
          }

          if (patchFilePath && fs.existsSync(patchFilePath)) {
            let patchStats;
            try {
              patchStats = fs.readFileSync(patchFilePath, "utf8");
            } catch (err) {
              throw new Error(`Failed to read file ${patchFilePath}: ${getErrorMessage(err)}`, { cause: err });
            }
            if (patchStats.trim()) {
              content += `**Changes:** Patch file exists with ${patchStats.split("\n").length} lines\n\n`;
              content += `<details><summary>Show patch preview</summary>\n\n\`\`\`diff\n${patchStats.slice(0, 2000)}${patchStats.length > 2000 ? "\n... (truncated)" : ""}\n\`\`\`\n\n</details>\n\n`;
            } else {
              content += `**Changes:** No changes (empty patch)\n\n`;
            }
          }
          return content;
        },
      });
      return { success: true, staged: true };
    }

    // Validate target configuration
    if (target !== "*" && target !== "triggering") {
      const pullNumber = parseInt(target, 10);
      if (Number.isNaN(pullNumber)) {
        return { success: false, error: 'Invalid target configuration: must be "triggering", "*", or a valid pull request number' };
      }
    }

    // Compute the target branch name based on target configuration
    let pullNumber;
    if (target === "triggering") {
      pullNumber = typeof context !== "undefined" ? context.payload?.pull_request?.number || context.payload?.issue?.number : undefined;
      if (!pullNumber) {
        const awContext = typeof context !== "undefined" ? parseAwContext(context.payload?.inputs?.aw_context) : null;
        const awItemType = awContext?.item_type.trim() ?? "";
        const awItemNumber = awContext?.item_number ?? null;
        if (awItemType === "pull_request" && awItemNumber !== null) {
          pullNumber = awItemNumber;
          core.info(`Resolved triggering pull request number '${pullNumber}' from aw_context.`);
        }
      }

      if (!pullNumber) {
        return { success: false, error: 'push-to-pull-request-branch with target "triggering" requires pull request context' };
      }
    } else if (target === "*") {
      if (message.pull_request_number) {
        pullNumber = parseInt(message.pull_request_number, 10);
      }
    } else {
      pullNumber = parseInt(target, 10);
    }

    let branchName;
    let prTitle = "";
    let prLabels = [];
    /** @type {any} */
    let branchStateBefore = null;

    if (!pullNumber) {
      return { success: false, error: "Pull request number is required but not found" };
    }

    // Resolve and validate target repository
    // For cross-repo scenarios, the PR may be in a different repository than the workflow
    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "push to PR branch");
    if (!repoResult.success) {
      return { success: false, error: repoResult.error };
    }
    const itemRepo = repoResult.repo;
    const repoParts = repoResult.repoParts;

    core.info(`Target repository: ${itemRepo}`);

    // Resolve the checkout directory for the target repo.
    // When the target repo differs from the workflow repo, it may be checked out
    // into a subdirectory of GITHUB_WORKSPACE (e.g. via actions/checkout path:).
    // All git operations must run from that directory, not from GITHUB_WORKSPACE.
    /** @type {any} */
    let repoCwd = undefined;
    const workflowRepo = process.env.GITHUB_REPOSITORY || "";
    if (itemRepo.toLowerCase() !== workflowRepo.toLowerCase()) {
      core.info(`Cross-repo push: looking for checkout of ${itemRepo}`);
      // First try the checkout mapping (faster than scanning the workspace)
      const checkoutMappingConfig = config.checkout_mapping || null;
      if (checkoutMappingConfig) {
        const targetLower = itemRepo.toLowerCase();
        const mappedPath = checkoutMappingConfig[targetLower];
        if (mappedPath) {
          const path = require("path");
          repoCwd = path.resolve(process.env.GITHUB_WORKSPACE || process.cwd(), mappedPath);
          core.info(`Using checkout mapping: ${itemRepo} -> ${mappedPath}`);
        }
      }
      // Fall back to workspace scan if not found in mapping
      if (!repoCwd) {
        const checkoutResult = findRepoCheckout(itemRepo, process.env.GITHUB_WORKSPACE, { allowedRepos: [...allowedRepos] });
        if (!checkoutResult.success) {
          return {
            success: false,
            error: `Repository '${itemRepo}' not found in workspace. Check out the target repo with actions/checkout and set its 'path' input so the checkout can be located. If checking out multiple repositories, ensure each actions/checkout step uses the appropriate 'path' input.`,
          };
        }
        repoCwd = checkoutResult.path;
      }
      core.info(`Found checkout for ${itemRepo} at: ${repoCwd}`);
    }

    // Base options for all git exec calls - includes cwd when running in a subdirectory checkout
    const baseGitOpts = repoCwd ? { cwd: repoCwd } : {};

    // For cross-repo checkouts, also trust the specific subdirectory. The factory-level call
    // covers GITHUB_WORKSPACE; this per-message call covers subdirectory checkout paths.
    if (repoCwd) {
      ensureSafeDirectoryTrust(repoCwd);
    }
    let pullRequest;
    try {
      const response = await githubClient.rest.pulls.get({
        owner: repoParts.owner,
        repo: repoParts.repo,
        pull_number: pullNumber,
      });
      pullRequest = response.data;
      branchName = pullRequest.head.ref;
      prTitle = pullRequest.title || "";
      prLabels = pullRequest.labels.map(label => label.name);
      if (typeof pullRequest.head?.sha === "string" && pullRequest.head.sha) {
        branchStateBefore = { head_sha: pullRequest.head.sha };
      }
    } catch (error) {
      core.info(`Warning: Could not fetch PR ${pullNumber} from ${itemRepo}: ${getErrorMessage(error)}`);
      return { success: false, error: `Failed to determine branch name for PR ${pullNumber} in ${itemRepo}` };
    }

    let pushRepo = itemRepo;
    let pushRepoParts = repoParts;
    let pushGithubClient = githubClient;
    const actualHeadRepo = typeof pullRequest.head?.repo?.full_name === "string" ? pullRequest.head.repo.full_name : "";

    // SECURITY: When head.repo is null (likely a deleted fork) we cannot verify which
    // repository the PR head came from. Always reject to prevent writes to an unverifiable PR.
    if (pullRequest.head?.repo == null) {
      const nullHeadErr = "Cannot push to PR: head repository is null (likely a deleted fork)";
      core.error(nullHeadErr);
      return { success: false, error: nullHeadErr };
    }

    // SECURITY: Validate the PR head repository against the configured or default expected value
    // before any fork-status branching. This prevents writes to a same-repo PR when head-repo
    // names an automation fork, and prevents writes to a fork PR when head-repo is not set.
    const expectedHeadRepo = configuredHeadRepo || itemRepo;
    if (actualHeadRepo && actualHeadRepo.toLowerCase() !== expectedHeadRepo.toLowerCase()) {
      const { isFork: actualIsFork } = detectForkPR(pullRequest);
      if (actualIsFork && !configuredHeadRepo) {
        core.error(`Cannot push to fork PR branch: head is '${actualHeadRepo}', not '${itemRepo}'`);
        core.error("Fork PRs remain blocked unless safe-outputs.push-to-pull-request-branch.head-repo is configured.");
        return {
          success: false,
          error: `Cannot push to fork PR: head repository '${actualHeadRepo}' does not match target '${itemRepo}'. Configure safe-outputs.push-to-pull-request-branch.head-repo and matching credentials to allow an automation-owned fork.`,
        };
      }
      return {
        success: false,
        error: `Cannot push to PR: head repository '${actualHeadRepo}' does not match expected '${expectedHeadRepo}'. Writes to repositories other than the configured head-repo remain blocked.`,
      };
    }

    // SECURITY: Check if this is a fork PR - only explicitly configured automation-owned
    // forks are eligible for updates.
    const { isFork, reason: forkReason } = detectForkPR(pullRequest);
    if (isFork) {
      if (!configuredHeadRepo) {
        core.error(`Cannot push to fork PR branch: ${forkReason}`);
        core.error("Fork PRs remain blocked unless safe-outputs.push-to-pull-request-branch.head-repo is configured.");
        return {
          success: false,
          error: `Cannot push to fork PR: ${forkReason}. Configure safe-outputs.push-to-pull-request-branch.head-repo and matching credentials to allow an automation-owned fork.`,
        };
      }
      const headRepoResult = resolveAndValidateRepo({ repo: configuredHeadRepo }, itemRepo, allowedRepos, "pull request head repository");
      if (!headRepoResult.success) {
        return { success: false, error: headRepoResult.error };
      }
      pushRepo = headRepoResult.repo;
      pushRepoParts = headRepoResult.repoParts;
      if (headGitHubToken && typeof global.getOctokit === "function") {
        pushGithubClient = global.getOctokit(headGitHubToken);
      }
      core.info(`Fork PR update allowed via configured head repo: ${pushRepo}`);
    } else {
      core.info(`Fork PR check: not a fork (${forkReason})`);
    }
    const pushRemoteUrl = pushRepo.toLowerCase() === itemRepo.toLowerCase() ? "" : `${(process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "")}/${pushRepo}.git`;
    const branchRemoteName = pushRemoteUrl || "origin";

    // SECURITY: Sanitize branch name to prevent shell injection (CWE-78)
    // Branch names from GitHub API must be normalized before use in git commands
    if (branchName) {
      const originalBranchName = branchName;
      branchName = normalizeBranchName(branchName);

      // Validate it's not empty after normalization
      if (!branchName) {
        return { success: false, error: `Invalid branch name: sanitization resulted in empty string (original: "${originalBranchName}")` };
      }

      if (originalBranchName !== branchName) {
        core.info(`Branch name sanitized: "${originalBranchName}" -> "${branchName}"`);
      }
    }
    const branchRemoteRef = pushRemoteUrl ? `refs/remotes/gh-aw-head/${branchName}` : `refs/remotes/origin/${branchName}`;

    core.info(`Target branch: ${branchName}`);
    core.info(`PR title: ${prTitle}`);
    core.info(`PR labels: ${prLabels.join(", ")}`);

    // SECURITY: Block pushing to the repository's default branch or any branch with
    // protection rules. PR head branches must never be default or protected branches.
    // This prevents agents from pushing directly to branches that should only receive
    // changes through reviewed pull requests.
    {
      const blockReason = await checkBranchPushable(pushGithubClient, pushRepoParts.owner, pushRepoParts.repo, branchName, checkBranchProtection);
      if (blockReason) {
        core.error(blockReason);
        return { success: false, error: blockReason };
      }
    }

    // Validate title prefix if specified
    if (titlePrefix && !prTitle.startsWith(titlePrefix)) {
      return { success: false, error: `Pull request title "${prTitle}" does not start with required prefix "${titlePrefix}"` };
    }

    // Validate labels if specified
    if (envLabels.length > 0) {
      const missingLabels = envLabels.filter(label => !prLabels.includes(label));
      if (missingLabels.length > 0) {
        return { success: false, error: `Pull request is missing required labels: ${missingLabels.join(", ")}. Current labels: ${prLabels.join(", ")}` };
      }
    }

    if (titlePrefix) {
      core.info(`✓ Title prefix validation passed: "${titlePrefix}"`);
    }
    if (envLabels.length > 0) {
      core.info(`✓ Labels validation passed: ${envLabels.join(", ")}`);
    }

    const createProtectedFilesFallbackIssue = async files => {
      const runUrl = buildWorkflowRunUrl(context, context.repo);
      const runId = context.runId;
      const artifactFileName = hasBundleFile ? bundleFilePath.replace("/tmp/gh-aw/", "") : patchFilePath ? patchFilePath.replace("/tmp/gh-aw/", "") : "aw-unknown.patch";
      const applyInstructions = buildManualBranchApplyCommands({
        hasBundleFile,
        runId,
        artifactFileName,
        branchName,
        branchRemote: branchRemoteName,
      });
      const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
      const prUrl = `${githubServer}/${repoParts.owner}/${repoParts.repo}/pull/${pullNumber}`;
      const issueTitle = `[gh-aw] Protected Files: ${prTitle || `PR #${pullNumber}`}`;
      const fileList = buildProtectedFileList(files, githubServer, repoParts.owner, repoParts.repo, branchName);
      const templatePath = getPromptPath("manifest_protection_push_to_pr_fallback.md");
      const issueBody = renderTemplateFromFile(templatePath, {
        files: fileList,
        pull_number: pullNumber,
        pr_url: prUrl,
        run_url: runUrl,
        run_id: runId,
        branch_name: branchName,
        apply_instructions: applyInstructions,
      });

      try {
        const { data: issue } = await withRetry(
          () =>
            githubClient.rest.issues.create({
              owner: repoParts.owner,
              repo: repoParts.repo,
              title: issueTitle,
              body: issueBody,
              labels: ["agentic-workflows"],
            }),
          RATE_LIMIT_RETRY_CONFIG,
          `create manifest-protection review issue in ${repoParts.owner}/${repoParts.repo}`
        );
        core.info(`Created manifest-protection review issue #${issue.number}: ${issue.html_url}`);
        await updateActivationComment(github, context, core, issue.html_url, issue.number, "issue");
        return attachExecutionState(
          {
            success: true,
            fallback_used: true,
            issue_number: issue.number,
            issue_url: issue.html_url,
          },
          branchStateBefore,
          branchStateBefore
        );
      } catch (issueError) {
        const error = `Manifest file protection: failed to create review issue. Error: ${getErrorMessage(issueError)}`;
        core.error(error);
        return { success: false, error };
      }
    };

    // Deferred protected file protection – fallback-to-issue path.
    // Create a review issue now that we have repoParts, pullNumber, and prTitle available.
    if (protectedFilesForFallback && protectedFilesForFallback.length > 0) {
      return await createProtectedFilesFallbackIssue(protectedFilesForFallback);
    }

    const hasChanges = !isEmpty;

    // Switch to or create the target branch
    core.info(`Switching to branch: ${branchName}`);

    // Detect missing/deleted branches early and return a clear error.
    // This avoids an opaque git fetch exit code when the PR branch was deleted.
    {
      const lsRemoteResult = await withGitHubHostToken(
        headGitHubToken,
        async () =>
          exec.getExecOutput("git", ["ls-remote", "--exit-code", "--heads", branchRemoteName, branchName], {
            env: { ...process.env, ...gitAuthEnv },
            ...baseGitOpts,
            ignoreReturnCode: true,
          }),
        baseGitOpts.cwd
      );

      if (lsRemoteResult.exitCode === 2) {
        const missingBranchError = MISSING_BRANCH_ERROR_TEMPLATE(branchName);
        if (ignoreMissingBranchFailure) {
          core.warning(`${missingBranchError} Skipping as configured by ignore-missing-branch-failure.`);
          return {
            success: false,
            error: missingBranchError,
            skipped: true,
          };
        }
        return {
          success: false,
          error: missingBranchError,
        };
      }

      if (lsRemoteResult.exitCode !== 0) {
        const stderr = (lsRemoteResult.stderr || "").trim();
        return {
          success: false,
          error: `Failed to verify branch ${branchName} exists on ${pushRepo}: ${stderr || `git ls-remote exited with code ${lsRemoteResult.exitCode}`}`,
        };
      }
    }

    // Fetch the specific target branch from origin
    // Authenticate using the credentials actions/checkout persisted into .git/config
    // for the safe_outputs job; no GIT_CONFIG_* extraheader is injected (see gitAuthEnv above).
    try {
      core.info(`Fetching branch: ${branchName}`);
      await withGitHubHostToken(
        headGitHubToken,
        async () =>
          exec.exec("git", ["fetch", branchRemoteName, `${branchName}:${branchRemoteRef}`], {
            env: { ...process.env, ...gitAuthEnv },
            ...baseGitOpts,
          }),
        baseGitOpts.cwd
      );
    } catch (fetchError) {
      const fetchErrorMessage = getErrorMessage(fetchError);
      if (ignoreMissingBranchFailure && looksLikeMissingRemoteBranchError(fetchErrorMessage)) {
        const missingBranchError = MISSING_BRANCH_ERROR_TEMPLATE(branchName);
        core.warning(`${missingBranchError} Skipping as configured by ignore-missing-branch-failure.`);
        return { success: false, error: missingBranchError, skipped: true };
      }
      return { success: false, error: `Failed to fetch branch ${branchName}: ${getErrorMessage(fetchError)}` };
    }

    // Check if branch exists on origin
    try {
      await exec.exec("git", ["rev-parse", "--verify", branchRemoteRef], baseGitOpts);
    } catch (verifyError) {
      const missingBranchError = MISSING_BRANCH_ERROR_TEMPLATE(branchName);
      if (ignoreMissingBranchFailure) {
        core.warning(`${missingBranchError} Skipping as configured by ignore-missing-branch-failure.`);
        return { success: false, error: missingBranchError, skipped: true };
      }
      return { success: false, error: `Branch ${branchName} does not exist on origin, can't push to it: ${getErrorMessage(verifyError)}` };
    }

    // Checkout the branch from origin
    try {
      await exec.exec("git", ["checkout", "-B", branchName, branchRemoteRef], baseGitOpts);
      core.info(`Checked out existing branch from ${pushRepo}: ${branchName}`);
    } catch (checkoutError) {
      return { success: false, error: `Failed to checkout branch ${branchName}: ${getErrorMessage(checkoutError)}` };
    }

    // Apply the patch/bundle using git CLI (skip if empty)
    // Track number of new commits added so we can restrict the extra empty commit
    // to branches with exactly one new commit (security: prevents use of CI trigger
    // token on multi-commit branches where workflow files may have been modified).
    let newCommitCount = 0;
    let remoteHeadBeforePatch = "";
    let pushedCommitSha = "";
    let rangeBaseRef = branchRemoteRef;
    if (hasChanges) {
      // Capture HEAD before applying changes to compute new-commit count later
      try {
        const { stdout } = await exec.getExecOutput("git", ["rev-parse", "HEAD"], baseGitOpts);
        remoteHeadBeforePatch = stdout.trim();
        if (remoteHeadBeforePatch) {
          rangeBaseRef = remoteHeadBeforePatch;
          core.info(`Remote branch HEAD before apply: ${remoteHeadBeforePatch}`);
        }
      } catch {
        // Non-fatal - extra empty commit will be skipped
      }

      // Pin patch application to the recorded base commit captured at patch-generation time.
      // This avoids applying a patch generated from an older branch tip onto a newer remote tip.
      // If the commit is unavailable (e.g. cross-repo/missing object), continue with current HEAD.
      if (!hasBundleFile) {
        const recordedBaseCommit = extractPatchBaseCommit(patchContent);
        if (recordedBaseCommit) {
          core.info(`Patch route base_commit resolved: ${recordedBaseCommit}`);
          try {
            try {
              await exec.exec("git", ["fetch", "origin", recordedBaseCommit, "--depth=1"], {
                env: { ...process.env, ...gitAuthEnv },
                ...baseGitOpts,
              });
            } catch (fetchError) {
              core.info(`Note: could not fetch base_commit ${recordedBaseCommit} explicitly (${getErrorMessage(fetchError)}); will verify local availability next`);
            }
            await exec.exec("git", ["cat-file", "-e", recordedBaseCommit], baseGitOpts);
            const ancestryCheck = await exec.getExecOutput("git", ["merge-base", "--is-ancestor", recordedBaseCommit, branchRemoteRef], { ...baseGitOpts, ignoreReturnCode: true });
            if (ancestryCheck.exitCode !== 0) {
              throw new Error(`recorded base_commit ${recordedBaseCommit} is not an ancestor of ${branchRemoteRef}; cannot safely re-anchor patch apply`);
            }
            if (remoteHeadBeforePatch && remoteHeadBeforePatch !== recordedBaseCommit) {
              core.warning(`Remote PR branch advanced since patch generation (remote HEAD ${remoteHeadBeforePatch}, patch base ${recordedBaseCommit}); applying patch from recorded base commit`);
            }
            await exec.exec("git", ["reset", "--hard", recordedBaseCommit], baseGitOpts);
            rangeBaseRef = recordedBaseCommit;
            core.info(`Reset branch to recorded base_commit before patch apply: ${recordedBaseCommit}`);
          } catch (baseCommitError) {
            core.warning(`Unable to use recorded base_commit ${recordedBaseCommit}; applying patch on current branch HEAD: ${getErrorMessage(baseCommitError)}`);
          }
        }
      }

      if (hasBundleFile) {
        // Bundle transport: fetch commits directly from the bundle file.
        // This preserves merge commit topology and per-commit metadata.
        core.info(`Applying changes from bundle: ${bundleFilePath}`);
        const bundleRef = `refs/bundles/push-${branchName.replace(/[^a-zA-Z0-9-]/g, "-")}`;
        try {
          await ensureFullHistoryForBundle(
            exec,
            {
              env: { ...process.env, ...gitAuthEnv },
              ...baseGitOpts,
            },
            { baseRef: branchName, bundleFilePath }
          );

          // Fetch from bundle into a temporary ref.
          // Use getExecOutput with ignoreReturnCode so we can read the actual stderr from git —
          // exec() only throws "The process '...' failed with exit code 1" which loses the
          // "lacks these prerequisite commits" text needed for the recovery path below.
          const bundleFetchRef = `refs/heads/${branchName}:${bundleRef}`;
          const initialBundleFetch = await exec.getExecOutput("git", ["fetch", bundleFilePath, bundleFetchRef], { ...baseGitOpts, ignoreReturnCode: true });
          if (initialBundleFetch.exitCode !== 0) {
            const initialFetchErrorOutput = initialBundleFetch.stderr || `exit code ${initialBundleFetch.exitCode}`;

            // Recovery path for bundle prerequisite failures: fetch missing prerequisite
            // commit objects, then retry with the original bundle ref.
            // This handles the race where main advanced between agent-time and safe_outputs-time:
            // the bundle's base commit may not be reachable from a fetch-depth:1 shallow clone
            // (e.g. when the commit is on a ref not in the fetch refspec).
            const prerequisiteCommits = extractBundlePrerequisiteCommits(initialFetchErrorOutput);
            if (prerequisiteCommits.length > 0) {
              await fetchBundlePrerequisites(exec, core, gitAuthEnv, baseGitOpts, prerequisiteCommits);
              await exec.exec("git", ["fetch", bundleFilePath, bundleFetchRef], baseGitOpts);
              core.info("Bundle fetch retry succeeded after prerequisite recovery");
            } else {
              core.warning(`Bundle fetch from refs/heads/${branchName} failed: ${initialFetchErrorOutput}; resolving source ref from bundle heads`);
              const { stdout: bundleHeadsOutput } = await exec.getExecOutput("git", ["bundle", "list-heads", bundleFilePath], baseGitOpts);
              const bundleHeads = bundleHeadsOutput
                .split("\n")
                .map(line => line.trim().split(/\s+/))
                // Bundles produced here advertise SHA-1 object IDs; reject malformed entries.
                .filter(parts => parts.length === 2 && /^[0-9a-f]{40}$/.test(parts[0]) && parts[1]);
              const branchRefChecks = await Promise.all(
                bundleHeads
                  .filter(([, ref]) => ref.startsWith("refs/heads/"))
                  .map(async ([, ref]) => ({
                    ref,
                    isValid: (await exec.getExecOutput("git", ["check-ref-format", ref], { ...baseGitOpts, ignoreReturnCode: true })).exitCode === 0,
                  }))
              );
              const branchRefs = branchRefChecks.filter(({ isValid }) => isValid).map(({ ref }) => ref);

              let bundleSourceRef;
              if (branchRefs.length === 1) {
                bundleSourceRef = branchRefs[0];
              } else if (branchRefs.length === 0) {
                const headRefs = bundleHeads.filter(([, ref]) => ref === "HEAD");
                if (headRefs.length !== 1) {
                  throw new Error(`Failed to resolve bundle source ref from list-heads: expected exactly 1 HEAD entry, found ${headRefs.length}`);
                }
                bundleSourceRef = "HEAD";
              } else {
                throw new Error(`Failed to resolve bundle source ref from list-heads: expected exactly 1 refs/heads entry, found ${branchRefs.length}`);
              }

              core.info(`Fetching resolved bundle source ${bundleSourceRef} into ${bundleRef}`);
              const resolvedBundleFetchRef = `${bundleSourceRef}:${bundleRef}`;
              const resolvedBundleFetch = await exec.getExecOutput("git", ["fetch", bundleFilePath, resolvedBundleFetchRef], { ...baseGitOpts, ignoreReturnCode: true });
              if (resolvedBundleFetch.exitCode !== 0) {
                const resolvedFetchErrorOutput = resolvedBundleFetch.stderr || `exit code ${resolvedBundleFetch.exitCode}`;
                const resolvedPrerequisiteCommits = extractBundlePrerequisiteCommits(resolvedFetchErrorOutput);
                if (resolvedPrerequisiteCommits.length === 0) {
                  throw new Error(`Failed to fetch resolved bundle source ${bundleSourceRef}: ${resolvedFetchErrorOutput}`);
                }

                await fetchBundlePrerequisites(exec, core, gitAuthEnv, baseGitOpts, resolvedPrerequisiteCommits, "[resolved] ");
                await exec.exec("git", ["fetch", bundleFilePath, resolvedBundleFetchRef], baseGitOpts);
                core.info("Resolved bundle fetch retry succeeded after prerequisite recovery");
              }
            }
          }
          core.info(`Fetched bundle to ${bundleRef}`);

          // SECURITY: Use git's own diff against the fetched bundle ref as the
          // authoritative pre-apply file set for bundle transport. This keeps
          // bundle pre-check and post-apply verification aligned even when the
          // patch artifact under-detects files (for example, merge-resolution
          // content preserved only by the bundle transport).
          {
            const bundleFiles = await getBundlePreApplyFiles(exec, baseGitOpts, rangeBaseRef, bundleRef);
            if (bundleFiles.length > 0) {
              core.info(`Pre-apply bundle verification: ${bundleFiles.length} file(s) detected from bundle transport`);
              const bundleProtection = checkFileProtectionPostApply(bundleFiles, config);
              if (bundleProtection.action === "deny") {
                const filesStr = bundleProtection.files.join(", ");
                const msg =
                  bundleProtection.source === "post-apply"
                    ? `Cannot push to pull request branch: bundle modifies files outside the allowed-files list (${filesStr}). Add the files to the allowed-files configuration field or remove them from the bundle.`
                    : `Cannot push to pull request branch: bundle modifies protected files (${filesStr}). Add them to the allowed-files configuration field or set protected-files: fallback-to-issue to create a review issue instead.`;
                core.warning(msg);
                return { success: false, skipped: true, reasonCode: POLICY_FILE_PROTECTION_DENIED_REASON_CODE, error: msg };
              }
              if (bundleProtection.action === "fallback") {
                core.warning(`Protected file protection triggered (fallback-to-issue): ${bundleProtection.files.join(", ")}. Will create review issue instead of pushing.`);
                return await createProtectedFilesFallbackIssue(bundleProtection.files);
              }
            }
          }

          // Point the checked-out branch at the bundle tip directly. In shallow
          // checkouts, merge --ff-only can fail to discover the ancestry even
          // when the bundle tip is based on the current branch tip and the
          // prerequisite exists locally.
          core.info(`Updating local branch ref refs/heads/${branchName} to ${bundleRef} (expected previous tip: ${remoteHeadBeforePatch || "unknown"})`);
          const updateRefArgs = ["update-ref", `refs/heads/${branchName}`, bundleRef];
          if (remoteHeadBeforePatch) {
            updateRefArgs.push(remoteHeadBeforePatch);
          }
          await exec.exec("git", updateRefArgs, baseGitOpts);
          await exec.exec("git", ["reset", "--hard"], baseGitOpts);
          core.info(`Updated branch to bundle tip from ${bundleRef}`);

          // Clean up the temporary ref
          try {
            await exec.exec("git", ["update-ref", "-d", bundleRef], baseGitOpts);
          } catch {
            // Non-fatal cleanup
          }
        } catch (bundleError) {
          core.error(`Failed to apply bundle: ${getErrorMessage(bundleError)}`);
          // Clean up temp ref if it exists
          try {
            await exec.exec("git", ["update-ref", "-d", bundleRef], baseGitOpts);
          } catch {
            // Ignore
          }
          return { success: false, error: `Failed to apply bundle: ${getErrorMessage(bundleError)}` };
        }
      } else {
        // Patch transport (non-default): git am --3way
        core.info("Applying patch...");
        try {
          if (commitTitleSuffix) {
            core.info(`Appending commit title suffix: "${commitTitleSuffix}"`);

            // Read the patch file
            let patchContent = fs.readFileSync(patchFilePath, "utf8");

            // Modify Subject lines in the patch to append the suffix
            patchContent = patchContent.replace(/^Subject: (?:\[PATCH\] )?(.*)$/gm, (match, title) => `Subject: [PATCH] ${title}${commitTitleSuffix}`);

            // Write the modified patch back
            fs.writeFileSync(patchFilePath, patchContent, "utf8");
            core.info(`Patch modified with commit title suffix: "${commitTitleSuffix}"`);
          }

          // Log first 100 lines of patch for debugging
          const finalPatchContent = fs.readFileSync(patchFilePath, "utf8");
          const patchLines = finalPatchContent.split("\n");
          const previewLineCount = Math.min(100, patchLines.length);
          core.info(`Patch preview (first ${previewLineCount} of ${patchLines.length} lines):`);
          for (let i = 0; i < previewLineCount; i++) {
            core.info(patchLines[i]);
          }

          // Use --3way to handle cross-repo patches where the patch base may differ from target repo
          // This allows git to resolve create-vs-modify mismatches when a file exists in target but not source
          await exec.exec("git", ["am", "--3way", patchFilePath], baseGitOpts);
          core.info("Patch applied successfully");
        } catch (error) {
          core.warning(`Initial patch apply failed, attempting add/add recovery: ${getErrorMessage(error)}`);
          let recoveredFromAddAddConflict = false;

          // Automatic recovery for add/add conflicts:
          // when a patch created from the base branch tries to "add" a file that
          // already exists on the PR branch, prefer the patch version and continue.
          try {
            const unresolvedFilesResult = await exec.getExecOutput("git", ["diff", "--name-only", "--diff-filter=U"], baseGitOpts);
            const unresolvedFiles = unresolvedFilesResult.stdout
              .split("\n")
              .map(line => line.trim())
              .filter(Boolean);

            if (unresolvedFiles.length > 0) {
              const statusPorcelainResult = await exec.getExecOutput("git", ["status", "--porcelain"], baseGitOpts);
              const addAddFiles = new Set(
                statusPorcelainResult.stdout
                  .split("\n")
                  .map(line => line.trim())
                  .filter(line => line.startsWith("AA "))
                  .map(line => line.substring(3).trim())
              );
              const allConflictsAreAddAdd = unresolvedFiles.every(file => addAddFiles.has(file));

              if (allConflictsAreAddAdd) {
                core.warning(`Detected add/add conflict(s) for ${unresolvedFiles.join(", ")}; preferring patch version and continuing`);
                for (const file of unresolvedFiles) {
                  await exec.exec("git", ["checkout", "--theirs", "--", file], baseGitOpts);
                  await exec.exec("git", ["add", "--", file], baseGitOpts);
                }
                await exec.exec("git", ["am", "--continue"], baseGitOpts);
                core.info("Patch applied successfully after resolving add/add conflict(s)");
                recoveredFromAddAddConflict = true;
              }
            }
          } catch (recoveryError) {
            core.warning(`Automatic add/add conflict recovery failed: ${getErrorMessage(recoveryError)}`);
          }

          if (recoveredFromAddAddConflict) {
            // Continue with normal push flow
          } else {
            core.error(`Failed to apply patch: ${getErrorMessage(error)}`);
            // Investigate patch failure
            try {
              core.info("Investigating patch failure...");

              const statusResult = await exec.getExecOutput("git", ["status"], baseGitOpts);
              core.info("Git status output:");
              core.info(statusResult.stdout);

              const logResult = await exec.getExecOutput("git", ["log", "--oneline", "-5"], baseGitOpts);
              core.info("Recent commits (last 5):");
              core.info(logResult.stdout);

              const diffResult = await exec.getExecOutput("git", ["diff", "HEAD"], baseGitOpts);
              core.info("Uncommitted changes:");
              core.info(diffResult.stdout && diffResult.stdout.trim() ? diffResult.stdout : "(no uncommitted changes)");

              const patchDiffResult = await exec.getExecOutput("git", ["am", "--show-current-patch=diff"], baseGitOpts);
              core.info("Failed patch diff:");
              core.info(patchDiffResult.stdout);

              const patchFullResult = await exec.getExecOutput("git", ["am", "--show-current-patch"], baseGitOpts);
              core.info("Failed patch (full):");
              core.info(patchFullResult.stdout);
            } catch (investigateError) {
              core.warning(`Failed to investigate patch failure: ${getErrorMessage(investigateError)}`);
            }

            return { success: false, error: "Failed to apply patch" };
          }
        }
      } // end else (patch path)
      core.info(`Apply transport completed; signed-push base ref: ${rangeBaseRef}`);

      // POST-APPLY FILE PROTECTION: Verify actual files written match policy.
      // This is the primary defense against parser-differential attacks where the JS
      // patch parser and git am disagree on which files a patch contains.
      // (see github/agentic-workflows#539)
      let agentChangedFiles = [];
      {
        const diffResult = await exec.getExecOutput("git", ["diff", "--name-only", "--no-renames", "-z", `${rangeBaseRef}..HEAD`], baseGitOpts);
        const actualFiles = diffResult.stdout.split("\0").filter(Boolean);
        agentChangedFiles = actualFiles;
        if (actualFiles.length > 0) {
          core.info(`Post-apply verification: ${actualFiles.length} file(s) actually modified`);
          const postApplyProtection = checkFileProtectionPostApply(actualFiles, config);
          if (postApplyProtection.action === "deny") {
            const filesStr = postApplyProtection.files.join(", ");
            const msg = `SECURITY: Post-apply file-protection check failed. ` + `The patch applied files that were not detected by the pre-apply parser: ${filesStr}. ` + `This may indicate a patch-parser bypass attempt. Aborting push.`;
            core.error(msg);
            // Reset to pre-apply state
            await exec.exec("git", ["reset", "--hard", rangeBaseRef], baseGitOpts);
            return { success: false, error: msg };
          }
          if (postApplyProtection.action === "fallback") {
            core.warning(`Post-apply: Protected file protection triggered (fallback-to-issue): ${postApplyProtection.files.join(", ")}`);
            await exec.exec("git", ["reset", "--hard", rangeBaseRef], baseGitOpts);
            return await createProtectedFilesFallbackIssue(postApplyProtection.files);
          }

          const changedBlobSizeBytes = getChangedBlobSizeBytes(baseGitOpts, actualFiles);
          const changedBlobSizeKb = Math.ceil(changedBlobSizeBytes / 1024);
          core.info(`Changed content size: ${changedBlobSizeKb} KB (maximum allowed: ${maxSizeKb} KB)`);
          if (changedBlobSizeKb > maxSizeKb) {
            const msg = `Changed content size (${changedBlobSizeKb} KB) exceeds maximum allowed size (${maxSizeKb} KB)`;
            await exec.exec("git", ["reset", "--hard", rangeBaseRef], baseGitOpts);
            return { success: false, error: msg };
          }
        }
      }

      // When threat detection produced a warning, create a review PR instead of pushing
      // directly to the existing PR branch. This allows manual review of the changes
      // before they are merged into the target PR.
      const detectionConclusionEnv = process.env.GH_AW_DETECTION_CONCLUSION;
      if (detectionConclusionEnv === "warning") {
        core.info("⚠️ Threat detection warning: creating review PR instead of direct push");

        // Create a review branch name based on the original branch, using
        // normalizeBranchName to enforce valid git ref characters + max length.
        const reviewBranchName = normalizeBranchName(`${branchName}-review`, String(Date.now()));
        try {
          // Pre-flight: check full branch history for workflow file changes.
          // GitHub rejects pushes of new branch refs whose commit history contains
          // .github/workflows/** changes when the token lacks the 'workflows' scope —
          // even if the current changeset itself does not touch workflow files.
          // Failing here avoids leaving the local branch in a renamed state after
          // a rejected push, and surfaces the error before any side effects.
          // When the workflow files are from pre-existing commits (not the agent's
          // own changeset), a non-fatal skip is returned instead of a hard error.
          const preflightError = await runWorkflowScopePreflightCheck(exec, baseGitOpts, allowWorkflows, pullRequest?.base?.ref, "Review branch", core, agentChangedFiles);
          if (preflightError) return preflightError;

          // Rename current local branch to review branch
          await exec.exec("git", ["checkout", "-b", reviewBranchName], baseGitOpts);
          core.info(`Created review branch: ${reviewBranchName}`);

          // Push the review branch — use getExecOutput to capture stderr so we
          // can detect GitHub's "workflows scope required" rejection and surface
          // a typed, actionable error instead of a bare git exit-1.
          // For fork-backed PRs, push to the head repo remote instead of origin.
          const reviewPushRemote = pushRemoteUrl || "origin";
          const reviewPushOutput = await withGitHubHostToken(
            pushRemoteUrl ? headGitHubToken : "",
            async () =>
              exec.getExecOutput("git", ["push", reviewPushRemote, reviewBranchName], {
                env: { ...process.env, ...gitAuthEnv },
                ...baseGitOpts,
                ignoreReturnCode: true,
              }),
            baseGitOpts.cwd
          );
          if (reviewPushOutput.exitCode !== 0) {
            const reviewPushStderr = (reviewPushOutput.stderr || "").trim();
            // GitHub rejects pushes to branches containing .github/workflows/** changes
            // when the token lacks the 'workflows' scope.  Distinguish between the
            // agent's own workflow files (hard error) and pre-existing commits (skip).
            if (isWorkflowsScopeRejection(reviewPushStderr)) {
              const agentWorkflowFiles = agentChangedFiles.filter(f => f.startsWith(".github/workflows/"));
              if (agentWorkflowFiles.length === 0) {
                return buildWorkflowsScopeSkip("Review branch", core);
              }
              return buildWorkflowsScopeError("Review branch", core);
            }
            throw new Error(`git push ${reviewPushRemote} ${reviewBranchName} failed (exit code ${reviewPushOutput.exitCode}): ${reviewPushStderr}`);
          }
          core.info(`Pushed review branch: ${reviewBranchName}`);

          // Create PR from review branch to original branch.
          // For fork-backed PRs, use an owner-qualified head reference.
          const reviewHeadRef = pushRemoteUrl ? `${pushRepoParts.owner}:${reviewBranchName}` : reviewBranchName;
          const detectionReasonEnv = process.env.GH_AW_DETECTION_REASON || "unknown";
          const warning = getThreatWarningPresentation(detectionReasonEnv);
          const prBody = [
            `> [!${warning.admonition}]`,
            `> ${warning.title}`,
            `> ${warning.summary}`,
            `> ${warning.marker}`,
            ">",
            `> **Reason:** ${detectionReasonEnv}`,
            ">",
            `> Review the [workflow run logs](${buildWorkflowRunUrl(context, context.repo)}) for details.`,
            "",
            `This PR contains changes that were originally intended for PR #${pullNumber} (\`${branchName}\`).`,
            "Please review the changes carefully before merging.",
          ].join("\n");

          const { data: reviewPR } = await githubClient.rest.pulls.create({
            owner: repoParts.owner,
            repo: repoParts.repo,
            title: `[review] ${prTitle || `Changes for #${pullNumber}`}`,
            body: prBody,
            head: reviewHeadRef,
            base: branchName,
          });

          core.info(`Created review PR #${reviewPR.number}: ${reviewPR.html_url}`);

          // Try to add needs-review label to the review PR
          try {
            await githubClient.rest.issues.addLabels({
              owner: repoParts.owner,
              repo: repoParts.repo,
              issue_number: reviewPR.number,
              labels: ["needs-review"],
            });
            core.info('Added "needs-review" label to review PR');
          } catch (labelError) {
            core.warning(`Failed to add "needs-review" label to review PR: ${getErrorMessage(labelError)}`);
          }

          // Update activation comment with review PR link
          await updateActivationComment(github, context, core, reviewPR.html_url, reviewPR.number, "pull_request");

          return {
            success: true,
            review_pr: true,
            branch_name: reviewBranchName,
            pr_number: reviewPR.number,
            pr_url: reviewPR.html_url,
          };
        } catch (reviewError) {
          core.error(`Failed to create review PR: ${getErrorMessage(reviewError)}`);
          return { success: false, error: `Failed to create review PR: ${getErrorMessage(reviewError)}` };
        }
      }

      // When signed commits are required, the GitHub GraphQL createCommitOnBranch mutation
      // cannot represent merge commits.  If the range to be pushed contains merge commits
      // (e.g. the agent ran `git merge origin/main` instead of `git rebase origin/main`),
      // squash the entire range into a single regular commit that carries the same tree.
      // This preserves the file-level outcome while producing a linear history that can
      // be signed.  A warning is emitted so workflow authors and agents know that rebase
      // should be preferred over merge in future runs.
      if (signedCommits && hasChanges) {
        const squashBase = rangeBaseRef;
        try {
          const { stdout: mergeCountOut } = await exec.getExecOutput("git", ["rev-list", "--merges", "--count", `${squashBase}..HEAD`], baseGitOpts);
          const mergeCount = parseInt(mergeCountOut.trim(), 10);
          if (Number.isFinite(mergeCount) && mergeCount > 0) {
            core.warning(
              `push_to_pull_request_branch: detected ${mergeCount} merge commit(s) in range ${squashBase}..HEAD. ` +
                `Merge commits cannot be pushed as signed commits. Squashing the range into a single regular commit to preserve content. ` +
                `To avoid this, use 'git rebase' instead of 'git merge' when updating the PR branch.`
            );
            // Prefer the message from the first non-merge commit in the range — it carries
            // the most meaningful description of the actual work.  Fall back to the last
            // commit's message (which may be the merge commit itself) if no regular commit
            // exists, and finally to a generic label.
            const { stdout: firstNonMergeOut } = await exec.getExecOutput("git", ["log", "--no-merges", "--max-count=1", "--format=%B", "--reverse", `${squashBase}..HEAD`], baseGitOpts);
            let squashMessage = firstNonMergeOut.trim();
            if (!squashMessage) {
              const { stdout: lastMsgOut } = await exec.getExecOutput("git", ["log", "-1", "--format=%B"], baseGitOpts);
              squashMessage = lastMsgOut.trim() || "Merge changes";
            }
            await linearizeRangeAsCommit(squashBase, squashMessage, exec, { gitOpts: baseGitOpts, commitFlags: ["--allow-empty", "--no-verify"] });
            core.info(`push_to_pull_request_branch: merge commits linearized into single regular commit for signed-commit push`);
          }
        } catch (squashErr) {
          core.warning(`push_to_pull_request_branch: failed to linearize merge commits: ${getErrorMessage(squashErr)}; push may fail`);
        }
      }

      // Push the applied commits to the branch using signed GraphQL commits (outside patch try/catch so push failures are not misattributed)
      try {
        const pushedSha = await pushSignedCommits({
          githubClient: pushGithubClient,
          owner: pushRepoParts.owner,
          repo: pushRepoParts.repo,
          branch: branchName,
          baseRef: rangeBaseRef,
          cwd: repoCwd || process.cwd(),
          gitAuthEnv,
          pushRemoteUrl,
          pushToken: headGitHubToken,
          signedCommits,
          resolvedTemporaryIds,
          currentRepo: itemRepo,
          validationConfig: config,
        });
        if (pushedSha) {
          pushedCommitSha = pushedSha;
          core.info(`pushSignedCommits returned pushed SHA: ${pushedSha}`);
        }
        core.info(`Changes committed and pushed to branch: ${branchName}`);
      } catch (pushError) {
        const pushErrorMessage = getErrorMessage(pushError);
        core.error(`Failed to push changes: ${pushErrorMessage}`);
        const nonFastForwardPatterns = ["non-fast-forward", "rejected", "fetch first", "Updates were rejected"];
        const isNonFastForward = nonFastForwardPatterns.some(pattern => pushErrorMessage.includes(pattern));
        let userMessage = isNonFastForward
          ? "Failed to push changes: remote PR branch changed while the workflow was running (non-fast-forward). Re-run the workflow on the latest PR branch state."
          : `Failed to push changes: ${pushErrorMessage}`;

        // Diagnose common race where branch was deleted after preflight checks.
        try {
          const lsRemoteAfterPushResult = await exec.getExecOutput("git", ["ls-remote", "--exit-code", "--heads", "origin", branchName], {
            env: { ...process.env, ...gitAuthEnv },
            ...baseGitOpts,
            ignoreReturnCode: true,
          });

          if (lsRemoteAfterPushResult.exitCode === 2) {
            userMessage = "Failed to push changes: remote PR branch appears to have been deleted while the workflow was running.";
          } else if (lsRemoteAfterPushResult.exitCode !== 0) {
            const remoteCheckError = (lsRemoteAfterPushResult.stderr || "").trim();
            core.warning(`Push failed and branch existence re-check also failed for ${branchName}: ${remoteCheckError || `git ls-remote exited with code ${lsRemoteAfterPushResult.exitCode}`}`);
          }
        } catch (diagnosisError) {
          core.warning(`Push failed and branch existence re-check errored for ${branchName}: ${getErrorMessage(diagnosisError)}`);
        }

        // Fallback path for diverged branches: create a new pull request so changes
        // can still be reviewed and merged into the original PR branch.
        if (isNonFastForward && fallbackAsPullRequest) {
          const fallbackBranchName = normalizeBranchName(`${branchName}-fallback`, String(Date.now()));
          core.warning(`Non-fast-forward push detected; creating fallback pull request from '${fallbackBranchName}' to '${branchName}'`);
          try {
            // Pre-flight: check full branch history for workflow file changes.
            // Like the review branch path, creating a new fallback branch ref triggers
            // GitHub's scope check on the full commit history, not just the new commits.
            // When the workflow files are from pre-existing commits (not the agent's
            // own changeset), a non-fatal skip is returned instead of a hard error.
            const preflightError = await runWorkflowScopePreflightCheck(exec, baseGitOpts, allowWorkflows, pullRequest?.base?.ref, "Fallback branch", core, agentChangedFiles);
            if (preflightError) return preflightError;

            await exec.exec("git", ["checkout", "-b", fallbackBranchName], baseGitOpts);
            // Use getExecOutput to capture stderr for 'workflows' scope diagnostics.
            // For fork-backed PRs, push to the head repo remote instead of origin.
            const fallbackPushRemote = pushRemoteUrl || "origin";
            const fallbackPushOutput = await withGitHubHostToken(
              pushRemoteUrl ? headGitHubToken : "",
              async () =>
                exec.getExecOutput("git", ["push", fallbackPushRemote, fallbackBranchName], {
                  env: { ...process.env, ...gitAuthEnv },
                  ...baseGitOpts,
                  ignoreReturnCode: true,
                }),
              baseGitOpts.cwd
            );
            if (fallbackPushOutput.exitCode !== 0) {
              const fallbackPushStderr = (fallbackPushOutput.stderr || "").trim();
              if (isWorkflowsScopeRejection(fallbackPushStderr)) {
                const agentWorkflowFiles = agentChangedFiles.filter(f => f.startsWith(".github/workflows/"));
                if (agentWorkflowFiles.length === 0) {
                  return buildWorkflowsScopeSkip("Fallback branch", core);
                }
                return buildWorkflowsScopeError("Fallback branch", core);
              }
              throw new Error(`git push ${fallbackPushRemote} ${fallbackBranchName} failed (exit code ${fallbackPushOutput.exitCode}): ${fallbackPushStderr}`);
            }

            // For fork-backed PRs, use an owner-qualified head reference.
            const fallbackHeadRef = pushRemoteUrl ? `${pushRepoParts.owner}:${fallbackBranchName}` : fallbackBranchName;

            const fallbackBody = [
              "> [!NOTE]",
              "> Direct push to the original pull request branch failed because the branch diverged (non-fast-forward).",
              `> Original PR branch: \`${branchName}\``,
              "",
              `This fallback PR contains the prepared changes for PR #${pullNumber}.`,
              "Merge this fallback PR into the original PR branch to apply them.",
              "",
              `Workflow run: ${buildWorkflowRunUrl(context, context.repo)}`,
            ].join("\n");

            const { data: fallbackPR } = await githubClient.rest.pulls.create({
              owner: repoParts.owner,
              repo: repoParts.repo,
              title: `[fallback] ${prTitle || `Changes for #${pullNumber}`}`,
              body: fallbackBody,
              head: fallbackHeadRef,
              base: branchName,
            });

            core.info(`Created fallback pull request #${fallbackPR.number}: ${fallbackPR.html_url}`);
            await updateActivationComment(github, context, core, fallbackPR.html_url, fallbackPR.number, "pull_request");

            return {
              success: true,
              fallback_used: true,
              fallback_type: "pull_request",
              pull_request_number: fallbackPR.number,
              pull_request_url: fallbackPR.html_url,
              branch_name: fallbackBranchName,
              repo: itemRepo,
              head_repo: pushRepo,
              number: fallbackPR.number,
              url: fallbackPR.html_url,
            };
          } catch (fallbackError) {
            const fallbackErrorMessage = getErrorMessage(fallbackError);
            core.error(`Failed to create fallback pull request: ${fallbackErrorMessage}`);
            userMessage = `${userMessage} Fallback pull request creation also failed: ${fallbackErrorMessage}`;
          }
        }

        return { success: false, error_type: "push_failed", error: userMessage };
      }

      // Count new commits pushed for the CI trigger decision
      if (remoteHeadBeforePatch) {
        try {
          const { stdout: countStr } = await exec.getExecOutput("git", ["rev-list", "--count", `${remoteHeadBeforePatch}..HEAD`], baseGitOpts);
          newCommitCount = parseInt(countStr.trim(), 10);
          core.info(`${newCommitCount} new commit(s) pushed to branch`);
        } catch {
          // Non-fatal - newCommitCount stays 0, extra empty commit will be skipped
          core.info("Could not count new commits - extra empty commit will be skipped");
        }
      }
    } else {
      core.info("Skipping patch application (empty patch)");

      const msg = "No changes to apply - noop operation completed successfully";

      switch (ifNoChanges) {
        case "error":
          return { success: false, error: "No changes to apply - failing as configured by if-no-changes: error" };
        case "ignore":
          // Silent success
          break;
        case "warn":
        default:
          core.info(msg);
          break;
      }
    }

    // The signed-push helper returns the commit SHA that landed on the branch.
    // Fall back to local HEAD only if the helper did not return one.
    let commitSha = pushedCommitSha;
    if (!commitSha) {
      const commitShaRes = await exec.getExecOutput("git", ["rev-parse", "HEAD"], baseGitOpts);
      if (commitShaRes.exitCode !== 0) {
        return { success: false, error: "Failed to get commit SHA" };
      }
      commitSha = commitShaRes.stdout.trim();
    }

    // Get repository base URL and construct URLs
    // For cross-repo scenarios, use repoParts (the target repo) not context.repo (the workflow repo)
    const githubServer = process.env.GITHUB_SERVER_URL || "https://github.com";
    const repoUrl = `${githubServer}/${repoParts.owner}/${repoParts.repo}`;
    const pushRepoUrl = `${githubServer}/${pushRepoParts.owner}/${pushRepoParts.repo}`;
    const pushUrl = `${pushRepoUrl}/tree/${branchName}`;
    const commitUrl = `${pushRepoUrl}/commit/${commitSha}`;

    // Update the activation comment with commit link (if a comment was created and changes were pushed)
    // Pass pullNumber so a new comment is created on the PR when no activation comment exists (e.g., schedule triggers)
    //
    // NOTE: we pass 'github' (global octokit) for updating the activation comment (same repo as workflow).
    // For the fallback path (no activation comment), we pass githubClient and targetRepo so the comment
    // is created in the correct target repository with the right authentication.
    //
    // Skip the activation comment for empty commits (0 file changes, e.g. CI trigger commits).
    // These are noise — they don't represent meaningful work and would clutter PRs on every scheduled run.
    if (hasChanges) {
      let isEmptyCommit = false;
      if (rangeBaseRef) {
        try {
          const { stdout: diffStat } = await exec.getExecOutput("git", ["diff", "--stat", rangeBaseRef, "HEAD"], baseGitOpts);
          isEmptyCommit = !diffStat.trim();
          if (isEmptyCommit) {
            core.info("Skipping activation comment: pushed commit has no file changes (empty commit)");
          }
        } catch {
          // Non-fatal — proceed with the comment if we can't determine
        }
      }
      if (!isEmptyCommit) {
        await updateActivationCommentWithCommit(github, context, core, commitSha, commitUrl, {
          targetIssueNumber: pullNumber,
          targetRepo: `${repoParts.owner}/${repoParts.repo}`,
          targetGithubClient: githubClient,
        });
      }
    }

    // Write summary to GitHub Actions summary
    const summaryTitle = hasChanges ? "Push to Branch" : "Push to Branch (No Changes)";
    const summaryContent = hasChanges
      ? `
## ${summaryTitle}
- **Branch**: \`${branchName}\`
- **Commit**: [${commitSha.substring(0, 7)}](${commitUrl})
- **URL**: [${pushUrl}](${pushUrl})
`
      : `
## ${summaryTitle}
- **Branch**: \`${branchName}\`
- **Status**: No changes to apply (noop operation)
- **URL**: [${pushUrl}](${pushUrl})
`;

    await core.summary.addRaw(summaryContent).write();

    // Push an extra empty commit if a token is configured and exactly 1 new commit was pushed.
    // This works around the GITHUB_TOKEN limitation where pushes don't trigger CI events.
    // Restricting to exactly 1 new commit prevents the CI trigger token being used on
    // multi-commit branches where workflow files may have been iteratively modified.
    if (hasChanges) {
      const ciTriggerResult = await pushExtraEmptyCommit({
        branchName,
        repoOwner: pushRepoParts.owner,
        repoName: pushRepoParts.repo,
        newCommitCount,
      });
      if (ciTriggerResult.success && !ciTriggerResult.skipped) {
        core.info("Extra empty commit pushed - CI checks should start shortly");
      }
    }

    return attachExecutionState(
      {
        success: true,
        number: pullNumber,
        repo: itemRepo,
        head_repo: pushRepo,
        url: `${repoUrl}/pull/${pullNumber}`,
        branch_name: branchName,
        commit_sha: commitSha,
        commit_url: commitUrl,
      },
      branchStateBefore || (remoteHeadBeforePatch ? { head_sha: remoteHeadBeforePatch } : null),
      commitSha ? { head_sha: commitSha } : null
    );
  };
}

module.exports = { main, HANDLER_TYPE, getBundlePreApplyFiles };
