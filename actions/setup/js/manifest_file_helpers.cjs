// @ts-check

/** @typedef {import('./types/handler-factory').HandlerConfig} HandlerConfig */
const { extractDiffGitHeaderEntries } = require("./patch_path_helpers.cjs");

function normalizeDotFolderExcludes(excludes) {
  return new Set((Array.isArray(excludes) ? excludes : []).map(exclude => String(exclude || "").replace(/\/+$/, "")).filter(Boolean));
}

function formatHeaderLineForError(headerLine) {
  const value = typeof headerLine === "string" ? headerLine : "";
  const truncated = value.length > 200 ? `${value.slice(0, 200)}…` : value;
  return JSON.stringify(truncated);
}

/**
 * Extracts the unique set of file basenames (filename without directory path) changed in a git patch.
 * Parses "diff --git a/<path> b/<path>" headers to determine which files were modified.
 * Both the a/<path> (original) and b/<path> (new) sides are captured so that renames and copies
 * are detected even when only the original filename matches a manifest file pattern.
 * The special sentinel "dev/null" (used for new-file/deleted-file diffs) is ignored.
 *
 * @param {string} patchContent - The git patch content
 * @returns {string[]} Deduplicated list of file basenames changed in the patch
 */
function extractFilenamesFromPatch(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return [];
  }
  const fileSet = new Set();
  const entries = extractDiffGitHeaderEntries(patchContent);
  for (const entry of entries) {
    if (!entry.parseable) {
      continue;
    }
    for (const filePath of [entry.oldPath, entry.newPath]) {
      // "dev/null" is the sentinel used when a file is created or deleted; skip it
      if (filePath && filePath !== "dev/null") {
        const parts = filePath.split("/");
        const basename = parts[parts.length - 1];
        if (basename) {
          fileSet.add(basename);
        }
      }
    }
  }
  return Array.from(fileSet);
}

/**
 * Extracts the unique set of full file paths changed in a git patch.
 * Parses "diff --git a/<path> b/<path>" headers and returns both sides
 * (excluding the "dev/null" sentinel).  Full paths are needed for
 * prefix-based protection (e.g. ".github/").
 *
 * Both the `a/<path>` (original) and `b/<path>` (new) sides are captured so
 * that renames are fully detected — e.g. renaming `.github/old.yml` to
 * `.github/new.yml` adds both paths to the returned set.
 *
 * @param {string} patchContent - The git patch content
 * @returns {string[]} Deduplicated list of full file paths changed in the patch
 */
function extractPathsFromPatch(patchContent) {
  if (!patchContent || !patchContent.trim()) {
    return [];
  }
  const pathSet = new Set();
  const entries = extractDiffGitHeaderEntries(patchContent);
  for (const entry of entries) {
    if (!entry.parseable) {
      // Fail-closed: unparseable diff --git headers are security-relevant because
      // git am may still apply the hunk via ---/+++ fallback. Throwing here prevents
      // silent policy bypass (see github/agentic-workflows#539).
      throw new Error(`Patch contains unparseable diff --git header (fail-closed): ${formatHeaderLineForError(entry.headerLine)}. ` + `Cannot verify file-protection policy. Rejecting patch.`);
    }
    for (const filePath of [entry.oldPath, entry.newPath]) {
      if (filePath && filePath !== "dev/null") {
        pathSet.add(filePath);
      }
    }
  }
  return Array.from(pathSet);
}

/**
 * Checks whether any files modified in the patch match the given list of manifest file names.
 * Matching is done by file basename only (no path comparison).
 *
 * @param {string} patchContent - The git patch content
 * @param {string[]} manifestFiles - List of manifest file names to check against (e.g. ["package.json", "go.mod"])
 * @returns {{ hasManifestFiles: boolean, manifestFilesFound: string[] }}
 */
function checkForManifestFiles(patchContent, manifestFiles) {
  if (!manifestFiles || manifestFiles.length === 0) {
    return { hasManifestFiles: false, manifestFilesFound: [] };
  }
  const changedFiles = extractFilenamesFromPatch(patchContent);
  const manifestFileSet = new Set(manifestFiles);
  const manifestFilesFound = changedFiles.filter(f => manifestFileSet.has(f));
  return { hasManifestFiles: manifestFilesFound.length > 0, manifestFilesFound };
}

/**
 * Checks whether any files modified in the patch have a path that starts with one of the
 * given protected path prefixes (e.g. ".github/").  This catches arbitrary files under a
 * protected directory, regardless of their filename.
 *
 * @param {string} patchContent - The git patch content
 * @param {string[]} pathPrefixes - List of path prefixes to check (e.g. [".github/"])
 * @returns {{ hasProtectedPaths: boolean, protectedPathsFound: string[] }}
 */
function checkForProtectedPaths(patchContent, pathPrefixes) {
  if (!pathPrefixes || pathPrefixes.length === 0) {
    return { hasProtectedPaths: false, protectedPathsFound: [] };
  }
  const changedPaths = extractPathsFromPatch(patchContent);
  const found = changedPaths.filter(p => pathPrefixes.some(prefix => p.startsWith(prefix)));
  return { hasProtectedPaths: found.length > 0, protectedPathsFound: found };
}

/**
 * Checks all files in a patch against an allowlist of glob patterns.
 * When `allowed-files` is configured, it acts as a strict allowlist: every file
 * touched by the patch must match at least one pattern; files that do not match
 * are returned as disallowed.
 *
 * Glob matching supports `*` (matches any characters except `/`) and `**` (matches
 * any characters including `/`).  Each changed file is tested as its full path
 * (e.g. `.github/workflows/ci.yml`) against the provided patterns.
 *
 * @param {string} patchContent - The git patch content
 * @param {string[]} allowedFilePatterns - Glob patterns for files permitted by the allowlist
 * @returns {{ hasDisallowedFiles: boolean, disallowedFiles: string[] }}
 */
function checkAllowedFiles(patchContent, allowedFilePatterns) {
  if (!allowedFilePatterns || allowedFilePatterns.length === 0) {
    return { hasDisallowedFiles: false, disallowedFiles: [] };
  }
  const allPaths = extractPathsFromPatch(patchContent);
  if (allPaths.length === 0) {
    return { hasDisallowedFiles: false, disallowedFiles: [] };
  }
  const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
  const compiledPatterns = allowedFilePatterns.map(p => globPatternToRegex(p));
  const disallowedFiles = allPaths.filter(p => !compiledPatterns.some(re => re.test(p)));
  return { hasDisallowedFiles: disallowedFiles.length > 0, disallowedFiles };
}

/**
 * Identifies which files in a patch match the given list of excluded-file glob patterns.
 * Matching is done against the full file path (e.g. `.github/workflows/ci.yml`).
 *
 * Glob matching supports `*` (matches any characters except `/`) and `**` (matches
 * any characters including `/`).
 *
 * @param {string} patchContent - The git patch content
 * @param {string[]} excludedFilePatterns - Glob patterns for files to exclude
 * @returns {{ excludedFiles: string[] }}
 */
function checkExcludedFiles(patchContent, excludedFilePatterns) {
  if (!excludedFilePatterns || excludedFilePatterns.length === 0) {
    return { excludedFiles: [] };
  }
  const allPaths = extractPathsFromPatch(patchContent);
  if (allPaths.length === 0) {
    return { excludedFiles: [] };
  }
  const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
  const compiledPatterns = excludedFilePatterns.map(p => globPatternToRegex(p));
  const excludedFiles = allPaths.filter(p => compiledPatterns.some(re => re.test(p)));
  return { excludedFiles };
}

/**
 * Checks whether any files modified in the patch reside inside a top-level
 * directory whose name starts with ".".  For example, `.cursor/settings.json`
 * or `.vscode/extensions.json` would both match.
 *
 * Root-level dot-files (e.g. `.env`) are NOT matched — only paths that have at
 * least two components (i.e. the file is *inside* a dot-directory) are flagged.
 *
 * Specific dot-folder prefixes can be opted out via the `excludes` parameter
 * (e.g. `[".agents/"]` to allow an agent to write its own instruction files).
 *
 * @param {string} patchContent - The git patch content
 * @param {string[]} [excludes] - Optional list of dot-folder prefixes to skip (e.g. [".agents/"])
 * @returns {{ hasTopLevelDotFolders: boolean, topLevelDotFoldersFound: string[] }}
 */
function checkForTopLevelDotFolders(patchContent, excludes) {
  const changedPaths = extractPathsFromPatch(patchContent);
  const excludeSet = normalizeDotFolderExcludes(excludes);
  const found = changedPaths.filter(p => {
    const slashIdx = p.indexOf("/");
    if (slashIdx === -1) return false; // root-level file, not inside a folder
    const firstComponent = p.substring(0, slashIdx);
    if (!firstComponent.startsWith(".") || firstComponent.length < 2) return false;
    return !excludeSet.has(firstComponent);
  });
  return { hasTopLevelDotFolders: found.length > 0, topLevelDotFoldersFound: found };
}

/**
 * Evaluates a patch against the configured file-protection policy and returns a
 * single structured result, eliminating nested branching in callers.
 *
 * The checks are applied in order and all must pass:
 * 1. If `allowed_files` is set → every file in the patch must match at least one pattern (deny if not).
 * 2. `protected-files` policy applies independently: "allowed" = skip,
 *    "fallback-to-issue" = create review issue, "request_review" = create PR with
 *    request-changes review, default ("blocked") = deny.
 *
 * To allow an agent to write protected files, set both `allowed-files` (strict scope) and
 * `protected-files: allowed` (explicit permission) — neither overrides the other implicitly.
 *
 * Note: `excluded-files` are excluded at patch generation time via `git format-patch`
 * `:(exclude)` pathspecs (see `generateGitPatch` options), so they will never appear in
 * the patch passed to this function.
 *
 * @param {string} patchContent - The git patch content
 * @param {HandlerConfig} config
 * @returns {{ action: 'allow' } | { action: 'deny', source: 'allowlist'|'protected', files: string[] } | { action: 'fallback', files: string[] } | { action: 'request_review', files: string[] }}
 */
function checkFileProtection(patchContent, config) {
  // Step 1: allowlist check (if configured)
  const allowedFilePatterns = Array.isArray(config.allowed_files) ? config.allowed_files : [];
  if (allowedFilePatterns.length > 0) {
    const { disallowedFiles } = checkAllowedFiles(patchContent, allowedFilePatterns);
    if (disallowedFiles.length > 0) {
      return { action: "deny", source: "allowlist", files: disallowedFiles };
    }
  }

  // Step 2: protected-files check (independent of allowlist)
  if (config.protected_files_policy === "allowed") {
    return { action: "allow" };
  }

  const manifestFiles = Array.isArray(config.protected_files) ? config.protected_files : [];
  const prefixes = Array.isArray(config.protected_path_prefixes) ? config.protected_path_prefixes : [];
  const { manifestFilesFound } = checkForManifestFiles(patchContent, manifestFiles);
  const { protectedPathsFound } = checkForProtectedPaths(patchContent, prefixes);
  const { topLevelDotFoldersFound } = config.protect_top_level_dot_folders ? checkForTopLevelDotFolders(patchContent, config.protected_dot_folder_excludes) : { topLevelDotFoldersFound: [] };
  const allFound = [...new Set([...manifestFilesFound, ...protectedPathsFound, ...topLevelDotFoldersFound])];

  if (allFound.length === 0) {
    return { action: "allow" };
  }

  if (config.protected_files_policy === "fallback-to-issue") {
    return { action: "fallback", files: allFound };
  }
  if (config.protected_files_policy === "request-review" || config.protected_files_policy === "request_review") {
    return { action: "request_review", files: allFound };
  }
  return { action: "deny", source: "protected", files: allFound };
}

/**
 * Post-apply file-protection verification.
 *
 * After `git am` applies a patch, this function runs `git diff --name-only`
 * against the pre-apply commit to get the *actual* files written to the working
 * tree. It then re-checks file protection against that ground-truth file list.
 *
 * This eliminates parser-differential attacks where the JS patch parser and
 * `git am` disagree on which files a patch contains (see github/agentic-workflows#539).
 *
 * @param {string[]} actualFiles - List of files actually modified (from `git diff --name-only`)
 * @param {HandlerConfig} config - The handler configuration with file protection rules
 * @returns {{ action: 'allow' } | { action: 'deny', source: 'allowlist'|'protected'|'post-apply', files: string[] } | { action: 'fallback', files: string[] } | { action: 'request_review', files: string[] }}
 */
function checkFileProtectionPostApply(actualFiles, config) {
  if (!actualFiles || actualFiles.length === 0) {
    return { action: "allow" };
  }

  // Step 1: allowlist check
  const allowedFilePatterns = Array.isArray(config.allowed_files) ? config.allowed_files : [];
  if (allowedFilePatterns.length > 0) {
    const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
    const compiledPatterns = allowedFilePatterns.map(p => globPatternToRegex(p));
    const disallowed = actualFiles.filter(file => {
      return !compiledPatterns.some(re => re.test(file));
    });
    if (disallowed.length > 0) {
      return { action: "deny", source: "post-apply", files: disallowed };
    }
  }

  // Step 2: protected-files check
  if (config.protected_files_policy === "allowed") {
    return { action: "allow" };
  }

  const manifestFiles = Array.isArray(config.protected_files) ? config.protected_files : [];
  const prefixes = Array.isArray(config.protected_path_prefixes) ? config.protected_path_prefixes : [];
  const dotFolderExcludes = normalizeDotFolderExcludes(config.protected_dot_folder_excludes);

  const allProtected = [];

  // Check manifest files (basename match)
  for (const file of actualFiles) {
    const basename = file.split("/").pop() || "";
    if (manifestFiles.includes(basename) || manifestFiles.includes(file)) {
      allProtected.push(file);
    }
  }

  // Check path prefixes
  for (const file of actualFiles) {
    if (prefixes.some(prefix => file.startsWith(prefix))) {
      if (!allProtected.includes(file)) {
        allProtected.push(file);
      }
    }
  }

  // Check top-level dot folders
  if (config.protect_top_level_dot_folders) {
    for (const file of actualFiles) {
      if (file.startsWith(".") && file.includes("/")) {
        const topFolder = file.split("/")[0];
        if (!dotFolderExcludes.has(topFolder) && !allProtected.includes(file)) {
          allProtected.push(file);
        }
      }
    }
  }

  if (allProtected.length === 0) {
    return { action: "allow" };
  }

  if (config.protected_files_policy === "fallback-to-issue") {
    return { action: "fallback", files: allProtected };
  }
  if (config.protected_files_policy === "request-review" || config.protected_files_policy === "request_review") {
    return { action: "request_review", files: allProtected };
  }
  return { action: "deny", source: "protected", files: allProtected };
}

module.exports = {
  extractFilenamesFromPatch,
  extractPathsFromPatch,
  checkForManifestFiles,
  checkForProtectedPaths,
  checkForTopLevelDotFolders,
  checkAllowedFiles,
  checkExcludedFiles,
  checkFileProtection,
  checkFileProtectionPostApply,
};
