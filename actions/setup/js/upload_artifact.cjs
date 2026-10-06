// @ts-check
/// <reference types="@actions/github-script" />

const { isStagedMode } = require("./safe_output_helpers.cjs");

/**
 * upload_artifact handler
 *
 * Validates artifact upload requests emitted by the model via the upload_artifact safe output
 * tool, then uploads the approved files directly via the internal artifact client.
 *
 * Files can be pre-staged in /tmp/gh-aw/safeoutputs/upload-artifacts/ or referenced by their
 * original path.  When a requested path is not found in the staging directory the handler
 * automatically copies the file (or directory) from its original location — supporting
 * absolute paths, workspace-relative paths, and cwd-relative paths.
 *
 * This handler follows the per-message handler pattern used by the safe_outputs handler loop.
 * main(config) returns a per-message handler function that:
 * 1. Validates the request against the workflow's policy configuration.
 * 2. Resolves the requested files (path or filter-based) from the staging directory.
 * 3. Uploads the approved files directly via DefaultArtifactClient.uploadArtifact().
 * 4. Sets step outputs (slot_N_tmp_id, upload_artifact_count) for downstream consumers.
 * 5. Generates a temporary artifact ID for each upload and writes a resolver file.
 *
 * Configuration keys (passed via config parameter from handler manager):
 *   max-uploads       - Max number of upload_artifact calls allowed (default: 1)
 *   retention-days    - Fixed retention period in days (default: 30); agent cannot override
 *   skip-archive      - Fixed skip-archive flag (default: false); agent cannot override
 *   max-size-bytes    - Maximum total bytes per upload (default: 100 MB)
 *   allowed-paths     - Array of allowed path glob patterns
 *   default-if-no-files - "error" or "ignore" (default: "error")
 *   filters-include   - Array of default include glob patterns
 *   filters-exclude   - Array of default exclude glob patterns
 *   staged            - true for staged/dry-run mode (skips actual upload)
 */

const fs = require("fs");
const os = require("os");
const path = require("path");
const { DefaultArtifactClient } = require("./artifact_client.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { ERR_VALIDATION, ERR_SYSTEM } = require("./error_codes.cjs");
const { isTemporaryId, normalizeTemporaryId } = require("./temporary_id.cjs");
const { lstatGuard } = require("./symlink_guard.cjs");
const { assertClaimAuthorized, currentClaimHandle, claimArtifactPath, scopedArtifactFilename, recordClaimEffect, claimIdentity, assertClaimIdentity, receiptMatchesClaim } = require("./work_queue_claim_scope.cjs");
const privateDeliveryReceipts = new WeakMap();

/**
 * Staging directory where the model places files to be uploaded.
 * Uses RUNNER_TEMP to match the path used by the compiled workflow when
 * downloading the staging artifact in the safe_outputs job.
 * Note: Computed once at module load time. RUNNER_TEMP must be set before
 * this module is required/evaluated.
 */
function stagingDirectory() {
  const base = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-artifacts");
  const handle = currentClaimHandle();
  return (handle ? claimArtifactPath(base, handle) : base) + path.sep;
}

/** Path where the resolver mapping (tmpId → artifact name) is written. */
const RESOLVER_FILE = process.env.GH_AW_ARTIFACT_RESOLVER_FILE || "/tmp/gh-aw/artifact-resolver.json";

/**
 * Generate a temporary artifact ID using the same aw_ prefix format as other safe outputs.
 * Format: aw_<8 alphanumeric characters (A-Za-z0-9)>
 * @returns {string}
 */
function generateTemporaryArtifactId() {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let id = "aw_";
  for (let i = 0; i < 8; i++) {
    id += chars[Math.floor(Math.random() * chars.length)];
  }
  return id;
}

/**
 * Resolve the temporary artifact ID from an upload_artifact message.
 * If the message declares a valid `temporary_id`, that value is used (normalised to lowercase).
 * Otherwise a random ID is generated.
 *
 * Honouring `message.temporary_id` is essential for topological ordering: when an agent
 * declares a `temporary_id` on the `upload_artifact` message and then embeds `#aw_ID` in a
 * subsequent message body, the dependency graph built by the topological sort can order
 * the upload before the referencing message, and the handler manager can replace the
 * reference with the resolved artifact URL.
 *
 * @param {Record<string, any>} message - The upload_artifact message from the model
 * @returns {string} The temporary artifact ID to use for this upload
 */
function resolveTemporaryArtifactId(message) {
  const declared = message.temporary_id;
  if (declared && typeof declared === "string") {
    const trimmed = declared.trim();
    if (isTemporaryId(trimmed)) {
      return normalizeTemporaryId(trimmed);
    }
    if (typeof core !== "undefined") {
      core.warning(`upload_artifact: invalid temporary_id format '${declared}'. ` + `Temporary IDs must be 'aw_' followed by 3–12 alphanumeric or underscore characters. ` + `A random ID will be generated instead.`);
    }
  }
  return generateTemporaryArtifactId();
}

/**
 * Check whether a relative path matches any of the provided glob patterns.
 * @param {string} relPath - Path relative to the staging root
 * @param {string[]} patterns
 * @returns {boolean}
 */
function matchesAnyPattern(relPath, patterns) {
  if (patterns.length === 0) return false;
  return patterns.some(pattern => {
    const regex = globPatternToRegex(pattern);
    return regex.test(relPath);
  });
}

/**
 * Validate that a path does not escape the staging root using traversal sequences.
 * @param {string} filePath - Absolute path
 * @param {string} root - Absolute root directory (must end with /)
 * @returns {boolean}
 */
function isWithinRoot(filePath, root) {
  const resolved = path.resolve(filePath);
  const normalRoot = path.resolve(root);
  return resolved.startsWith(normalRoot + path.sep) || resolved === normalRoot;
}

/**
 * Validate that a canonical absolute path does not refer to sensitive system or credential
 * locations. Mirrors the check in safe_outputs_handlers.cjs for defense-in-depth.
 *
 * @param {string} canonicalPath - Resolved absolute path
 * @returns {string|null} Error message or null if safe
 */
function validateSourcePath(canonicalPath) {
  const parts = canonicalPath.split(path.sep);
  if (parts.some(p => p === ".git")) {
    return `path contains sensitive repository metadata (.git): ${canonicalPath}`;
  }

  const systemDenied = ["/etc", "/proc", "/sys", "/dev", "/run", "/boot", "/lib", "/lib64", "/usr/lib", "/usr/local/lib"];
  for (const denied of systemDenied) {
    const normalDenied = path.resolve(denied);
    if (canonicalPath === normalDenied || canonicalPath.startsWith(normalDenied + path.sep)) {
      return `path refers to a system directory: ${canonicalPath}`;
    }
  }

  const homeDir = os.homedir();
  if (homeDir) {
    const sensitiveNames = [".ssh", ".aws", ".gnupg", ".docker", ".kube", ".azure", ".gcp", ".config", ".netrc", ".npmrc", ".gitconfig", ".gitcredentials", ".git-credentials"];
    for (const name of sensitiveNames) {
      const sensitive = path.join(path.resolve(homeDir), name);
      if (canonicalPath === sensitive || canonicalPath.startsWith(sensitive + path.sep)) {
        return `path refers to a sensitive HOME location: ${canonicalPath}`;
      }
    }
  }

  return null;
}

/**
 * Resolve a root path to its canonical form, falling back to path.resolve when the
 * directory does not yet exist (e.g. GITHUB_WORKSPACE before checkout).
 * @param {string} root
 * @returns {string}
 */
function canonicalizeRoot(root) {
  try {
    return fs.realpathSync(root);
  } catch {
    return path.resolve(root);
  }
}

/**
 * Validate that a canonical absolute path is within one of the allowed source roots.
 * Allowed roots: staging directory, GITHUB_WORKSPACE.
 * RUNNER_TEMP is intentionally excluded — only the specific staging subdirectory is allowed.
 *
 * @param {string} canonicalPath - Resolved absolute path
 * @returns {string|null} Error message or null if within an allowed root
 */
function validateAllowedRoot(canonicalPath) {
  const allowedRoots = [canonicalizeRoot(stagingDirectory())];
  if (process.env.GITHUB_WORKSPACE) {
    allowedRoots.push(canonicalizeRoot(process.env.GITHUB_WORKSPACE));
  }
  const withinAllowedRoot = allowedRoots.some(root => canonicalPath === root || canonicalPath.startsWith(root + path.sep));
  if (!withinAllowedRoot) {
    return `path is outside allowed source roots (GITHUB_WORKSPACE, staging directory): ${canonicalPath}`;
  }
  return null;
}

/**
 * Recursively list all regular files under a directory.
 * @param {string} dir - Absolute directory path
 * @param {string} baseDir - Root used to compute relative paths
 * @returns {string[]} Relative paths from baseDir
 */
function listFilesRecursive(dir, baseDir) {
  /** @type {string[]} */
  const files = [];
  if (!fs.existsSync(dir)) return files;

  const entries = fs.readdirSync(dir, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listFilesRecursive(fullPath, baseDir));
    } else if (entry.isFile()) {
      // Reject symlinks – entry.isFile() returns false for symlinks unless dereferenced.
      // We check explicitly to avoid following symlinks.
      if (lstatGuard(fullPath) !== null) {
        files.push(path.relative(baseDir, fullPath));
      } else {
        core.warning(`Skipping symlink: ${fullPath}`);
      }
    }
  }
  return files;
}

/**
 * Copy a single file to the staging directory, preserving the relative path structure.
 * Rejects symlinks and creates intermediate directories as needed.
 *
 * @param {string} sourcePath - Absolute path to the source file
 * @param {string} destRelPath - Relative path within the staging directory
 * @returns {{ error: string|null }}
 */
function copySingleFileToStaging(sourcePath, destRelPath) {
  const destPath = path.join(stagingDirectory(), destRelPath);
  // Never overwrite a file that is already staged — the pre-staged version takes precedence.
  if (fs.existsSync(destPath)) {
    core.info(`Skipping auto-copy for ${destRelPath}: already exists in staging directory`);
    return { error: null };
  }
  const stat = lstatGuard(sourcePath);
  if (stat === null) {
    return { error: `symlinks are not allowed: ${sourcePath}` };
  }
  if (!stat.isFile()) {
    return { error: `not a regular file: ${sourcePath}` };
  }
  try {
    fs.mkdirSync(path.dirname(destPath), { recursive: true });
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to create directory ${path.dirname(destPath)}: ${getErrorMessage(err)}`, { cause: err });
  }
  try {
    fs.copyFileSync(sourcePath, destPath);
    fs.chmodSync(destPath, 0o600);
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to copy file ${sourcePath} to ${destPath}: ${getErrorMessage(err)}`, { cause: err });
  }
  return { error: null };
}

/**
 * Recursively copy a directory into the staging directory.
 * Skips symlinks and logs warnings for them.
 *
 * @param {string} sourceDir - Absolute path to the source directory
 * @param {string} destRelDir - Relative directory path within the staging directory
 * @returns {{ copiedCount: number, error: string|null }}
 */
function copyDirectoryToStaging(sourceDir, destRelDir) {
  let copiedCount = 0;
  const entries = fs.readdirSync(sourceDir, { withFileTypes: true });
  for (const entry of entries) {
    const srcFull = path.join(sourceDir, entry.name);
    const destRel = path.join(destRelDir, entry.name);
    const stat = lstatGuard(srcFull);
    if (stat === null) {
      core.warning(`Skipping symlink during auto-copy: ${srcFull}`);
      continue;
    }
    if (stat.isDirectory()) {
      // Reject sensitive directory names at every level (e.g. foo/.git/config).
      let canonicalDir;
      try {
        canonicalDir = fs.realpathSync(srcFull);
      } catch (err) {
        return { copiedCount, error: `failed to resolve canonical path for ${srcFull}: ${getErrorMessage(err)}` };
      }
      const sensitiveErr = validateSourcePath(canonicalDir);
      if (sensitiveErr) {
        return { copiedCount, error: sensitiveErr };
      }
      const sub = copyDirectoryToStaging(srcFull, destRel);
      if (sub.error) return sub;
      copiedCount += sub.copiedCount;
    } else if (stat.isFile()) {
      // Revalidate each file's canonical path before copying.
      let canonicalFile;
      try {
        canonicalFile = fs.realpathSync(srcFull);
      } catch (err) {
        return { copiedCount, error: `failed to resolve canonical path for ${srcFull}: ${getErrorMessage(err)}` };
      }
      const sensitiveErr = validateSourcePath(canonicalFile);
      if (sensitiveErr) {
        return { copiedCount, error: sensitiveErr };
      }
      const result = copySingleFileToStaging(srcFull, destRel);
      if (result.error) return { copiedCount, error: result.error };
      copiedCount++;
    }
  }
  return { copiedCount, error: null };
}

/**
 * Attempt to locate the requested path outside the staging directory and copy it in.
 *
 * Search order for absolute paths:
 *   1. Use the absolute path directly.
 *
 * Search order for relative paths:
 *   1. GITHUB_WORKSPACE environment variable (GitHub Actions workspace).
 *   2. Current working directory (process.cwd()).
 *
 * @param {string} reqPath - The path from the request (absolute or relative)
 * @returns {{ copied: boolean, relPath: string, error: string|null }}
 */
function autoCopyToStaging(reqPath) {
  if (path.isAbsolute(reqPath)) {
    if (!fs.existsSync(reqPath)) {
      return { copied: false, relPath: "", error: `absolute path does not exist: ${reqPath}` };
    }
    const stat = lstatGuard(reqPath);
    if (stat === null) {
      return { copied: false, relPath: "", error: `symlinks are not allowed: ${reqPath}` };
    }

    // Canonicalize and validate before copying.
    let canonical;
    try {
      canonical = fs.realpathSync(reqPath);
    } catch (err) {
      return { copied: false, relPath: "", error: `failed to resolve canonical path for ${reqPath}: ${getErrorMessage(err)}` };
    }
    const sensitiveErr = validateSourcePath(canonical);
    if (sensitiveErr) {
      return { copied: false, relPath: "", error: sensitiveErr };
    }
    const rootErr = validateAllowedRoot(canonical);
    if (rootErr) {
      return { copied: false, relPath: "", error: rootErr };
    }

    // Derive a relative destination path from the basename (or relative to filesystem root for nested paths).
    const relPath = path.basename(reqPath);
    if (stat.isDirectory()) {
      const result = copyDirectoryToStaging(reqPath, relPath);
      if (result.error) return { copied: false, relPath: "", error: result.error };
      core.info(`Auto-copied directory ${reqPath} to staging (${result.copiedCount} file(s))`);
    } else {
      const result = copySingleFileToStaging(reqPath, relPath);
      if (result.error) return { copied: false, relPath: "", error: result.error };
      core.info(`Auto-copied file ${reqPath} to staging as ${relPath}`);
    }
    return { copied: true, relPath, error: null };
  }

  // Relative path: search in GITHUB_WORKSPACE, then cwd.
  const searchRoots = [];
  if (process.env.GITHUB_WORKSPACE) {
    searchRoots.push(process.env.GITHUB_WORKSPACE);
  }
  const cwd = process.cwd();
  if (!searchRoots.includes(cwd)) {
    searchRoots.push(cwd);
  }

  for (const root of searchRoots) {
    const candidate = path.resolve(root, reqPath);
    if (!fs.existsSync(candidate)) continue;
    const stat = lstatGuard(candidate);
    if (stat === null) {
      return { copied: false, relPath: "", error: `symlinks are not allowed: ${candidate}` };
    }

    // Canonicalize and validate before copying.
    let canonical;
    try {
      canonical = fs.realpathSync(candidate);
    } catch (err) {
      return { copied: false, relPath: "", error: `failed to resolve canonical path for ${candidate}: ${getErrorMessage(err)}` };
    }
    const sensitiveErr = validateSourcePath(canonical);
    if (sensitiveErr) {
      return { copied: false, relPath: "", error: sensitiveErr };
    }
    const rootErr = validateAllowedRoot(canonical);
    if (rootErr) {
      return { copied: false, relPath: "", error: rootErr };
    }

    if (stat.isDirectory()) {
      const result = copyDirectoryToStaging(candidate, reqPath);
      if (result.error) return { copied: false, relPath: "", error: result.error };
      core.info(`Auto-copied directory ${candidate} to staging as ${reqPath} (${result.copiedCount} file(s))`);
    } else {
      const result = copySingleFileToStaging(candidate, reqPath);
      if (result.error) return { copied: false, relPath: "", error: result.error };
      core.info(`Auto-copied file ${candidate} to staging as ${reqPath}`);
    }
    return { copied: true, relPath: reqPath, error: null };
  }

  return { copied: false, relPath: "", error: null };
}

/**
 * Resolve the list of files to upload for a single request.
 * Applies: staging root → auto-copy → allowed-paths → request include/exclude → dedup + sort.
 *
 * If a path-based request refers to a file that is not in the staging directory but exists
 * elsewhere (absolute path, workspace, or cwd), the file is automatically copied into the
 * staging directory before resolution continues.
 *
 * @param {Record<string, any>} request - Parsed upload_artifact record
 * @param {string[]} allowedPaths - Policy allowed-paths patterns
 * @param {string[]} defaultInclude - Policy default include patterns
 * @param {string[]} defaultExclude - Policy default exclude patterns
 * @returns {{ files: string[], error: string|null }}
 */
function resolveFiles(request, allowedPaths, defaultInclude, defaultExclude) {
  const hasPath = "path" in request;
  const hasFilters = "filters" in request;
  if (hasPath === hasFilters) {
    return { files: [], error: "exactly one of 'path' or 'filters' must be present" };
  }

  /** @type {string[]} */
  let candidateRelPaths;

  if ("path" in request) {
    let reqPath = String(request.path);

    // For absolute paths, attempt auto-copy to staging.
    if (path.isAbsolute(reqPath)) {
      const copyResult = autoCopyToStaging(reqPath);
      if (copyResult.error) {
        return { files: [], error: copyResult.error };
      }
      if (!copyResult.copied) {
        return { files: [], error: `path must be relative (staging-dir-relative), got absolute path: ${reqPath}` };
      }
      // Switch to the relative path inside the staging directory.
      reqPath = copyResult.relPath;
    }

    // Reject traversal
    const resolved = path.resolve(stagingDirectory(), reqPath);
    if (!isWithinRoot(resolved, stagingDirectory())) {
      return { files: [], error: `path must not traverse outside staging directory: ${reqPath}` };
    }

    // If the path does not exist in staging, try auto-copy from workspace/cwd.
    if (!fs.existsSync(resolved)) {
      const copyResult = autoCopyToStaging(reqPath);
      if (copyResult.error) {
        return { files: [], error: copyResult.error };
      }
      if (!copyResult.copied) {
        const available = listFilesRecursive(stagingDirectory(), stagingDirectory());
        const hint =
          available.length > 0
            ? ` Available files: [${available.slice(0, 20).join(", ")}]${available.length > 20 ? ` … and ${available.length - 20} more` : ""}`
            : " The staging directory is empty — did you forget to copy files to " + stagingDirectory() + "?";
        return { files: [], error: `path does not exist in staging directory: ${reqPath}.${hint}` };
      }
      reqPath = copyResult.relPath;
    }

    const stat = lstatGuard(path.resolve(stagingDirectory(), reqPath));
    if (stat === null) {
      return { files: [], error: `symlinks are not allowed: ${reqPath}` };
    }
    if (stat.isDirectory()) {
      candidateRelPaths = listFilesRecursive(path.resolve(stagingDirectory(), reqPath), stagingDirectory());
    } else {
      candidateRelPaths = [reqPath];
    }
  } else {
    // Filter-based selection: start from all files in the staging directory.
    const allFiles = listFilesRecursive(stagingDirectory(), stagingDirectory());
    const requestFilters = request.filters || {};
    const include = /** @type {string[]} */ requestFilters.include || defaultInclude;
    const exclude = /** @type {string[]} */ requestFilters.exclude || defaultExclude;

    candidateRelPaths = allFiles.filter(f => (include.length === 0 || matchesAnyPattern(f, include)) && (exclude.length === 0 || !matchesAnyPattern(f, exclude)));
  }

  // Apply allowed-paths policy filter.
  if (allowedPaths.length > 0) {
    candidateRelPaths = candidateRelPaths.filter(f => matchesAnyPattern(f, allowedPaths));
  }

  // Deduplicate and sort deterministically.
  const unique = Array.from(new Set(candidateRelPaths)).sort();
  return { files: unique, error: null };
}

/**
 * Validate skip_archive constraints:
 * - skip_archive may only be used for a single file.
 * - directories are rejected (already expanded to file list).
 *
 * @param {boolean} skipArchive
 * @param {string[]} files
 * @returns {string|null} Error message or null
 */
function validateSkipArchive(skipArchive, files) {
  if (!skipArchive) return null;
  if (files.length !== 1) {
    return `skip-archive requires exactly one selected file, but ${files.length} files matched`;
  }
  return null;
}

/**
 * Compute total size of the given file list (relative paths from STAGING_DIR).
 * @param {string[]} files
 * @returns {number} Total size in bytes
 */
function computeTotalSize(files) {
  let total = 0;
  for (const f of files) {
    const abs = path.join(stagingDirectory(), f);
    try {
      total += fs.statSync(abs).size;
    } catch {
      // Ignore missing files (already validated upstream).
    }
  }
  return total;
}

/**
 * Derive a sanitised artifact name from a path or a slot index.
 * @param {Record<string, any>} request
 * @param {number} slotIndex
 * @returns {string}
 */
function deriveArtifactName(request, slotIndex) {
  if (typeof request.name === "string" && request.name.trim()) {
    return request.name.trim().replace(/[^a-zA-Z0-9._\-]/g, "-");
  }
  if ("path" in request && typeof request.path === "string") {
    const base = path.basename(String(request.path)).replace(/[^a-zA-Z0-9._\-]/g, "-");
    if (base) return base;
  }
  return `artifact-slot-${slotIndex}`;
}

/**
 * Create or return the internal DefaultArtifactClient.
 * global.__createArtifactClient can be set in tests to inject a mock client factory.
 * @returns {Promise<{ uploadArtifact: (name: string, files: string[], rootDir: string, opts: object) => Promise<{id?: number, size?: number, digest?: string}> }>}
 */
async function getArtifactClient() {
  if (typeof global.__createArtifactClient === "function") {
    return global.__createArtifactClient();
  }
  return new DefaultArtifactClient();
}

/**
 * Returns a per-message handler function that processes a single upload_artifact request.
 *
 * @param {Object} config - Handler configuration from the safe outputs config
 * @returns {Promise<Function>} Per-message handler function
 */
async function main(config = {}) {
  const factoryClaim = currentClaimHandle();
  const factoryIdentity = factoryClaim ? claimIdentity(factoryClaim) : null;
  const maxUploads = typeof config["max-uploads"] === "number" ? config["max-uploads"] : 1;
  // retention-days and skip-archive are fixed workflow configuration; the agent cannot override them.
  const configuredRetention = config["retention-days"] ?? 30;
  const retentionDays = typeof configuredRetention === "number" || typeof configuredRetention === "string" ? Number(configuredRetention) : NaN;
  if (!Number.isInteger(retentionDays) || retentionDays < 1 || retentionDays > 400) {
    throw new Error(`${ERR_VALIDATION}: upload_artifact retention-days must resolve to an integer between 1 and 400, got ${JSON.stringify(configuredRetention)}`);
  }
  const skipArchive = config["skip-archive"] === true;
  const maxSizeBytes = typeof config["max-size-bytes"] === "number" ? config["max-size-bytes"] : 104857600;
  const defaultIfNoFiles = typeof config["default-if-no-files"] === "string" ? config["default-if-no-files"] : "error";
  const allowedPaths = Array.isArray(config["allowed-paths"]) ? config["allowed-paths"] : [];
  const filtersInclude = Array.isArray(config["filters-include"]) ? config["filters-include"] : [];
  const filtersExclude = Array.isArray(config["filters-exclude"]) ? config["filters-exclude"] : [];
  const isStaged = isStagedMode(config);

  core.info(`upload_artifact handler: max_uploads=${maxUploads}, retention_days=${retentionDays}, skip_archive=${skipArchive}`);
  core.info(`Allowed paths: ${allowedPaths.length > 0 ? allowedPaths.join(", ") : "(none – all staging files allowed)"}`);

  // Slot index tracks which slot each successful request maps to.
  let slotIndex = 0;

  /** @type {Record<string, string>} resolver: tmpId → artifact name */
  const resolver = {};

  /**
   * Per-message handler: processes one upload_artifact request.
   *
   * Called by the safe_outputs handler manager for each `upload_artifact` message emitted
   * by the model. State (slotIndex, resolver) is shared across calls via closure so that
   * successive requests are assigned to sequential slot directories.
   *
   * @param {Object} message - The upload_artifact message from the model
   * @param {Object} resolvedTemporaryIds - Map of already-resolved temporary IDs (unused here)
   * @param {Map<string, any>} temporaryIdMap - Shared temp-ID map; the handler does not modify it
   * @returns {Promise<{success: boolean, error?: string, skipped?: boolean, tmpId?: string, temporaryId?: string, artifactName?: string, artifactId?: number, artifactUrl?: string, slotIndex?: number}>}
   */
  return async function handleUploadArtifact(message, resolvedTemporaryIds, temporaryIdMap) {
    if (factoryClaim && currentClaimHandle() !== factoryClaim) throw new Error("upload_artifact factory cannot escape its original Claim");
    if (factoryIdentity) assertClaimIdentity(factoryIdentity);
    message = await assertClaimAuthorized(message);
    if (slotIndex >= maxUploads) {
      return {
        success: false,
        error: `${ERR_VALIDATION}: upload_artifact: exceeded max-uploads policy (${maxUploads}). Reduce the number of upload_artifact calls or raise max-uploads in workflow configuration.`,
      };
    }

    const i = slotIndex;

    // Resolve files.
    const { files, error: resolveError } = resolveFiles(message, allowedPaths, filtersInclude, filtersExclude);
    if (resolveError) {
      return { success: false, error: `${ERR_VALIDATION}: upload_artifact: ${resolveError}` };
    }

    if (files.length === 0) {
      if (defaultIfNoFiles === "ignore") {
        core.warning(`upload_artifact: no files matched, skipping (if-no-files=ignore)`);
        return { success: false, skipped: true, error: "No files matched the selection criteria" };
      }
      return {
        success: false,
        error: `${ERR_VALIDATION}: upload_artifact: no files matched the selection criteria. Check allowed-paths, filters, or use defaults.if-no-files: ignore to skip empty uploads.`,
      };
    }

    // Validate skip-archive file-count constraint.
    const skipArchiveError = validateSkipArchive(skipArchive, files);
    if (skipArchiveError) {
      return { success: false, error: `${ERR_VALIDATION}: upload_artifact: ${skipArchiveError}` };
    }

    // Validate total size.
    const totalSize = computeTotalSize(files);
    if (totalSize > maxSizeBytes) {
      return {
        success: false,
        error: `${ERR_VALIDATION}: upload_artifact: total file size ${totalSize} bytes exceeds max-size-bytes limit of ${maxSizeBytes} bytes.`,
      };
    }

    // Derive artifact name and generate temporary ID.
    const baseName = deriveArtifactName(message, i);
    const artifactName = factoryClaim ? `claim-${path.basename(claimArtifactPath("", factoryClaim))}-${baseName}` : baseName;
    const tmpId = resolveTemporaryArtifactId(message);
    if (Object.prototype.hasOwnProperty.call(resolver, tmpId)) {
      core.warning(`upload_artifact: duplicate temporary_id "${tmpId}" detected for artifact "${artifactName}". Using the first occurrence. Ensure each artifact has a unique temporary_id.`);
    } else {
      resolver[tmpId] = artifactName;
    }

    core.info(`Slot ${i}: artifact="${artifactName}", files=${files.length}, size=${totalSize}B, retention=${retentionDays}d, skip_archive=${skipArchive}, tmp_id=${tmpId}`);

    /** @type {number|undefined} */
    let artifactId;
    /** @type {string} */
    let artifactUrl = "";
    let trustedDigest;
    let trustedSize;

    if (!isStaged) {
      // Upload files directly via the internal artifact client.
      const absoluteFiles = files.map(f => path.join(stagingDirectory(), f));
      const client = await getArtifactClient();
      try {
        const uploadOpts = { retentionDays };
        if (skipArchive) {
          uploadOpts.skipArchive = true;
        }
        const repository = process.env.GITHUB_REPOSITORY || "";
        await assertClaimAuthorized({ ...message, repo: repository }, { effect: true, resource: { repository, target_run_id: process.env.GITHUB_RUN_ID } });
        const effect = recordClaimEffect({ kind: "artifact", repository, outcome: "unknown" });
        const uploadResult = await client.uploadArtifact(artifactName, absoluteFiles, stagingDirectory(), uploadOpts);
        if (effect) {
          effect.outcome = "succeeded";
          effect.id = String(uploadResult.id || "");
        }
        artifactId = uploadResult.id;
        trustedDigest = uploadResult.digest;
        trustedSize = uploadResult.size;
        core.info(`Uploaded artifact "${artifactName}" (id=${artifactId ?? "n/a"}, size=${uploadResult.size ?? totalSize}B)`);

        // Construct the artifact URL from the artifact ID and GitHub context.
        if (artifactId) {
          const serverUrl = process.env.GITHUB_SERVER_URL || "https://github.com";
          const repository = process.env.GITHUB_REPOSITORY || "";
          const runId = process.env.GITHUB_RUN_ID || "";
          if (repository && runId) {
            artifactUrl = new URL(`/${repository}/actions/runs/${runId}/artifacts/${artifactId}`, serverUrl).toString();
            core.info(`Artifact URL: ${artifactUrl}`);
          }
        }
      } catch (err) {
        return {
          success: false,
          error: `${ERR_VALIDATION}: upload_artifact: failed to upload artifact "${artifactName}": ${getErrorMessage(err)}`,
        };
      }
    } else {
      // Emit a staged mode preview summary.
      let summaryContent = "## 🎭 Staged Mode: Upload Artifact Preview\n\n";
      summaryContent += "The following artifact upload would be performed if staged mode was disabled:\n\n";
      summaryContent += `**Artifact Name:** ${artifactName}\n\n`;
      summaryContent += `**Files (${files.length}):**\n\n`;
      for (const file of files) {
        summaryContent += `- \`${file}\`\n`;
      }
      summaryContent += `\n**Total Size:** ${totalSize} bytes\n\n`;
      summaryContent += `**Retention:** ${retentionDays} days\n\n`;
      summaryContent += `> ℹ️ Upload skipped (staged mode active)\n\n`;

      await core.summary.addRaw(summaryContent).write();
      core.info("📝 Upload artifact preview written to step summary");
    }

    // Set step outputs so downstream jobs can reference the tmp ID.
    const outputPrefix = factoryClaim ? `claim_${path.basename(claimArtifactPath("", factoryClaim))}_` : "";
    core.setOutput(`${outputPrefix}slot_${i}_tmp_id`, tmpId);
    core.setOutput(`${outputPrefix}slot_${i}_file_count`, String(files.length));
    core.setOutput(`${outputPrefix}slot_${i}_size_bytes`, String(totalSize));
    if (artifactId !== undefined) {
      core.setOutput(`${outputPrefix}slot_${i}_artifact_id`, String(artifactId));
    }
    if (artifactUrl) {
      core.setOutput(`${outputPrefix}slot_${i}_artifact_url`, artifactUrl);
    }

    slotIndex++;

    // Update the count output.
    core.setOutput(`${outputPrefix}upload_artifact_count`, String(slotIndex));

    // Write/update resolver mapping so downstream steps can resolve tmp IDs to artifact names.
    try {
      const resolverFile = scopedArtifactFilename(RESOLVER_FILE);
      fs.mkdirSync(path.dirname(resolverFile), { recursive: true });
      fs.writeFileSync(resolverFile, JSON.stringify(resolver, null, 2));
      core.info(`Wrote artifact resolver mapping to ${resolverFile}`);
    } catch (err) {
      core.warning(`Failed to write artifact resolver file: ${getErrorMessage(err)}`);
    }

    const result = {
      success: true,
      tmpId,
      temporaryId: tmpId,
      artifactName,
      artifactId,
      artifactUrl,
      slotIndex: i,
    };
    if (factoryClaim && !isStaged && artifactId && typeof trustedDigest === "string" && /^(?:sha256:)?[a-f0-9]{64}$/.test(trustedDigest)) {
      privateDeliveryReceipts.set(result, {
        ...factoryIdentity,
        repository: process.env.GITHUB_REPOSITORY,
        run_id: process.env.GITHUB_RUN_ID,
        name: artifactName,
        id: artifactId,
        digest: trustedDigest.replace(/^sha256:/, ""),
        size: trustedSize,
      });
    }
    return result;
  };
}

async function verifyArtifactDelivery({ claim, result, github }) {
  const receipt = result && privateDeliveryReceipts.get(result);
  if (!receiptMatchesClaim(receipt, claim) || !receipt.run_id) return { verified: false };
  const [owner, repo] = receipt.repository.split("/");
  const { data } = await github.rest.actions.getArtifact({ owner, repo, artifact_id: receipt.id });
  if (
    data.id !== receipt.id ||
    data.name !== receipt.name ||
    data.expired !== false ||
    String(data.workflow_run?.id) !== receipt.run_id ||
    data.digest !== `sha256:${receipt.digest}` ||
    (receipt.size !== undefined && data.size_in_bytes !== receipt.size)
  )
    return { verified: false };
  return {
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "artifact", repository: receipt.repository, id: String(receipt.id), target_run_id: receipt.run_id, name: receipt.name, digest: data.digest, url: result.artifactUrl },
    evidence: { source: "github_artifact_api", digest: data.digest, run_id: receipt.run_id, size: data.size_in_bytes },
  };
}

module.exports = { main, verifyArtifactDelivery };
