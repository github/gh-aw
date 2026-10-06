// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Utilities shared by git patch generation and validation code.
 *
 * This module intentionally has no side effects and no coupling to the
 * patch-generation orchestration in generate_git_patch.cjs. Each helper is
 * pure/stateless or performs a well-defined local filesystem operation, which
 * keeps the surface small, easy to test against a real git repo, and reusable
 * by other safe-output handlers (e.g. bundle transport, create_pull_request
 * fallback paths).
 */

const fs = require("fs");

const { getErrorMessage } = require("./error_helpers.cjs");
const { execGitSync } = require("./git_helpers.cjs");
const { scopedArtifactFilename } = require("./work_queue_claim_scope.cjs");

/**
 * Debug logging helper - logs to stderr when DEBUG env var matches
 * @param {string} message - Debug message to log
 */
function debugLog(message) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("generate_git_patch") || debug.includes("patch")) {
    console.error(`[git_patch_utils] ${message}`);
  }
}

/**
 * Sanitize a string for use as a patch filename component.
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
 * Sanitize a branch name for use as a patch filename
 * @param {string} branchName - The branch name to sanitize
 * @returns {string} The sanitized branch name safe for use in a filename
 */
function sanitizeBranchNameForPatch(branchName) {
  return sanitizeForFilename(branchName, "unknown");
}

/**
 * Sanitize a repo slug for use in a filename
 * @param {string} repoSlug - The repo slug (owner/repo)
 * @returns {string} The sanitized slug safe for use in a filename
 */
function sanitizeRepoSlugForPatch(repoSlug) {
  return sanitizeForFilename(repoSlug, "");
}

/**
 * Get the patch file path for a given branch name
 * @param {string} branchName - The branch name
 * @returns {string} The full patch file path
 */
function getPatchPathForBranch(branchName) {
  const sanitized = sanitizeBranchNameForPatch(branchName);
  return scopedArtifactFilename(`/tmp/gh-aw/aw-${sanitized}.patch`);
}

/**
 * Get the patch file path for a given branch name and repo slug
 * Used for multi-repo scenarios to prevent patch file collisions
 * @param {string} branchName - The branch name
 * @param {string} repoSlug - The repository slug (owner/repo)
 * @returns {string} The full patch file path including repo disambiguation
 */
function getPatchPathForBranchInRepo(branchName, repoSlug) {
  const sanitizedBranch = sanitizeBranchNameForPatch(branchName);
  const sanitizedRepo = sanitizeRepoSlugForPatch(repoSlug);
  return scopedArtifactFilename(`/tmp/gh-aw/aw-${sanitizedRepo}-${sanitizedBranch}.patch`);
}

/**
 * Builds the pathspec arguments to exclude specific files from a git command.
 * Produces ["--", ":(exclude)pattern1", ":(exclude)pattern2", ...] or [] when
 * the input is empty/unset. These are passed after a "--" so git treats them
 * as pathspecs, not revisions.
 *
 * @param {string[] | undefined | null} excludedFiles - Glob patterns to exclude
 * @returns {string[]} Arguments to append to a git format-patch or git diff call
 */
function buildExcludePathspecs(excludedFiles) {
  if (!Array.isArray(excludedFiles) || excludedFiles.length === 0) {
    return [];
  }
  return ["--", ...excludedFiles.map(p => `:(exclude)${p}`)];
}

/**
 * Compute net UTF-8 bytes added by a unified diff.
 *
 * "Net added bytes" per file = (bytes in added lines) − (bytes in deleted lines), clamped to
 * zero per file, then summed across all files in the diff.
 *
 * Using the net value instead of raw additions means that files which are
 * completely rewritten with similar-sized content (e.g. a JSON object whose
 * keys are regenerated) contribute only their actual growth to the patch-size
 * budget rather than their entire new content.  This avoids the false positive
 * where the tool reports "entire source code size" for a rewrite that barely
 * changes the logical payload.
 *
 * Clamping is applied per file, not globally, so that a large deletion in one
 * file cannot mask a large addition in a different file.  Each file's net
 * contribution is clamped to zero independently before being added to the
 * running total.
 *
 * Rules:
 *   - Only lines inside diff hunks (after the first "@@" line) are examined.
 *   - Lines that start with "+++" (file header) are excluded because they appear
 *     before the first "@@" and are never inside a hunk.
 *   - A new "diff " line flushes the current file's net contribution and resets
 *     per-file counters.
 *
 * @param {string} patchContent - Output of `git diff` (unified diff format)
 * @returns {number} Sum of per-file net added bytes (≥ 0)
 */
function getPatchDiffSizeBytes(patchContent) {
  let inHunk = false;
  let fileAdd = 0;
  let fileDel = 0;
  let total = 0;
  for (const line of patchContent.split("\n")) {
    if (line.startsWith("diff ")) {
      total += Math.max(0, fileAdd - fileDel);
      fileAdd = 0;
      fileDel = 0;
      inHunk = false;
    } else if (line.startsWith("@@")) {
      inHunk = true;
    }
    if (inHunk) {
      if (line.startsWith("+")) {
        fileAdd += Buffer.byteLength(line + "\n", "utf8");
      } else if (line.startsWith("-")) {
        fileDel += Buffer.byteLength(line + "\n", "utf8");
      }
    }
  }
  total += Math.max(0, fileAdd - fileDel);
  return total;
}

/**
 * Compute the net patch diff size for staged changes (git diff --cached).
 *
 * Returns the net bytes added by the staged diff: additions minus deletions,
 * clamped to zero.  This is the "diff size" used to enforce max-patch-size on
 * repo-memory pushes.
 *
 * @param {Object} options
 * @param {(args: string[], opts?: Record<string, any>) => string} options.execGitSyncFn
 * @param {string} [options.cwd]
 * @param {string[]} [options.pathspecs] - Optional pathspecs to limit the staged diff
 * @returns {number}
 */
function getStagedPatchDiffSizeBytes({ execGitSyncFn, cwd, pathspecs }) {
  if (Array.isArray(pathspecs) && pathspecs.length === 0) {
    return 0;
  }
  const args = ["diff", "--cached", "--no-renames"];
  if (Array.isArray(pathspecs)) {
    args.push("--", ...pathspecs);
  }
  const patchContent = execGitSyncFn(args, { stdio: "pipe", cwd });
  return getPatchDiffSizeBytes(patchContent);
}

/**
 * Compute the net diff size in bytes between two refs in the given git repo.
 *
 * This is the value that should be compared against `max_patch_size` in
 * push_to_pull_request_branch: it reflects how much the PR branch will
 * actually change as a result of the push, independent of how the patch or
 * bundle transport encodes the commit history.
 *
 * Implementation note: we use `git diff --binary --output=<tmpfile>` rather
 * than buffering the diff through execGitSync's stdout. That keeps memory
 * usage O(1) regardless of the diff size (we just stat the file) and avoids
 * hitting the execGitSync maxBuffer on large binary diffs. The temp file is
 * removed in `finally` on success, failure, and stat failure alike.
 *
 * @param {Object} args - Arguments
 * @param {string} args.baseRef - Base ref (commit SHA, branch, or ref)
 * @param {string} args.headRef - Head ref (commit SHA, branch, or ref)
 * @param {string} args.cwd - Working directory containing the git repo
 * @param {string} args.tmpPath - Absolute path to the temp diff file (will be
 *   written and removed by this function)
 * @param {string[]} [args.excludedFiles] - Glob patterns to exclude
 * @returns {number | null} The net diff size in bytes, or null on failure
 */
function computeIncrementalDiffSize({ baseRef, headRef, cwd, tmpPath, excludedFiles }) {
  if (!baseRef || !headRef || !cwd || !tmpPath) {
    return null;
  }
  const excludeArgs = buildExcludePathspecs(excludedFiles);
  /** @type {any} */
  let diffSize = null;
  try {
    execGitSync(["diff", "--binary", `--output=${tmpPath}`, `${baseRef}..${headRef}`, ...excludeArgs], { cwd });
    if (fs.existsSync(tmpPath)) {
      diffSize = fs.statSync(tmpPath).size;
      debugLog(`Computed incremental net diffSize=${diffSize} bytes (baseRef=${baseRef}..${headRef})`);
    }
  } catch (diffErr) {
    debugLog(`Failed to compute incremental net diffSize - ${getErrorMessage(diffErr)}`);
  } finally {
    // Best-effort cleanup of the temp diff file; we only needed its size.
    try {
      if (fs.existsSync(tmpPath)) {
        fs.unlinkSync(tmpPath);
      }
    } catch {
      // Cleanup failure is non-fatal.
    }
  }
  return diffSize;
}

/**
 * Returns true when `ancestor` is an ancestor of (or identical to) `descendant`.
 * Any git failure (unknown revision, missing object, corrupt object store) is
 * treated as "not an ancestor" - callers only need a boolean signal for base
 * selection. Failures are suppressed here (suppressLogs: true); callers that
 * need richer diagnostics on an unexpected failure should call
 * describeGitFailure on their own caught error instead.
 * @param {string} ancestor
 * @param {string} descendant
 * @param {string|undefined} cwd
 * @returns {boolean}
 */
function isAncestorCommit(ancestor, descendant, cwd) {
  try {
    execGitSync(["merge-base", "--is-ancestor", "--", ancestor, descendant], { cwd, suppressLogs: true });
    return true;
  } catch {
    return false;
  }
}

/**
 * Returns true when the repository is a partial clone (objects are fetched lazily
 * from a promisor remote). In gh-aw checkouts credentials are not persisted, so any
 * lazy fetch is unauthenticated and fails - which surfaces as confusing git errors.
 * @param {string|undefined} cwd
 * @returns {boolean}
 */
function isPartialClone(cwd) {
  try {
    return execGitSync(["config", "--get", "remote.origin.promisor"], { cwd, suppressLogs: true }).trim() === "true";
  } catch {
    return false;
  }
}

/**
 * Appends a partial-clone diagnostic to an error message when the failure looks like
 * a failed lazy object hydration from an unauthenticated promisor remote.
 * @param {string} message
 * @param {string|undefined} cwd
 * @returns {string}
 */
function describeGitFailure(message, cwd) {
  if (!/promisor|Authentication failed|Invalid username or token|fetch-pack|object not found/i.test(message)) {
    return message;
  }
  if (!isPartialClone(cwd)) {
    return message;
  }
  return (
    `${message} ` +
    "This repository is a partial clone (remote.origin.promisor=true), so git tried to lazily fetch missing objects from the remote. " +
    "That fetch is unauthenticated because the checkout used persist-credentials: false. " +
    "Fetch the required objects during checkout (for example checkout.fetch-depth: 0 without a blob filter) to avoid lazy fetches."
  );
}

module.exports = {
  sanitizeForFilename,
  sanitizeBranchNameForPatch,
  sanitizeRepoSlugForPatch,
  getPatchPathForBranch,
  getPatchPathForBranchInRepo,
  buildExcludePathspecs,
  computeIncrementalDiffSize,
  getPatchDiffSizeBytes,
  getStagedPatchDiffSizeBytes,
  isAncestorCommit,
  isPartialClone,
  describeGitFailure,
};
