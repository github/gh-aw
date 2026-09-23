// @ts-check

const fs = require("fs");
const path = require("path");
const { execGitSync } = require("./git_helpers.cjs");
const { validateTargetRepo, parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");
const { lookupCheckout, loadAllCheckouts } = require("./checkout_manifest.cjs");

/**
 * Debug logging helper - logs to stderr when DEBUG env var matches
 * @param {string} message - Debug message to log
 */
function debugLog(message) {
  const debug = process.env.DEBUG || "";
  if (debug === "*" || debug.includes("find_repo_checkout") || debug.includes("multi-repo")) {
    console.error(`[find_repo_checkout] ${message}`);
  }
}

/**
 * Normalize a repo slug to lowercase owner/repo format
 * @param {string} repoSlug - The repo slug (owner/repo)
 * @returns {string} Normalized lowercase slug
 */
function normalizeRepoSlug(repoSlug) {
  if (!repoSlug) return "";
  return repoSlug.toLowerCase().trim();
}

/**
 * Extract repo slug from a git remote URL
 * Handles various URL formats:
 * - https://github.com/owner/repo.git
 * - https://github.com/owner/repo
 * - git@github.com:owner/repo.git
 * - git@github.com:owner/repo
 * @param {string} remoteUrl - The git remote URL
 * @returns {string|null} The repo slug (owner/repo) or null if not parseable
 */
function extractRepoSlugFromUrl(remoteUrl) {
  if (!remoteUrl) return null;

  // Remove .git suffix if present
  let url = remoteUrl.trim();
  if (url.endsWith(".git")) {
    url = url.slice(0, -4);
  }

  // Handle HTTPS URLs: https://github.com/owner/repo
  const httpsMatch = url.match(/https?:\/\/[^/]+\/([^/]+\/[^/]+)$/);
  if (httpsMatch) {
    return normalizeRepoSlug(httpsMatch[1]);
  }

  // Handle SSH URLs: git@github.com:owner/repo
  const sshMatch = url.match(/git@[^:]+:([^/]+\/[^/]+)$/);
  if (sshMatch) {
    return normalizeRepoSlug(sshMatch[1]);
  }

  return null;
}

/**
 * Find all repositories that contain a .git directory or gitdir-link file
 * @param {string} basePath - The base path to search from
 * @param {number} [maxDepth=5] - Maximum directory depth to search
 * @returns {string[]} Array of paths to .git directories
 */
function findGitDirectories(basePath, maxDepth = 5) {
  const gitDirs = [];

  /**
   * Recursively scan directories
   * @param {string} dir - Current directory
   * @param {number} depth - Current depth
   */
  function scan(dir, depth) {
    if (depth > maxDepth) return;

    try {
      const entries = fs.readdirSync(dir, { withFileTypes: true });

      for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);

        if (entry.name === ".git" && (entry.isDirectory() || entry.isFile())) {
          // Found a git directory or gitdir-link file - add the parent (repo root)
          gitDirs.push(dir);
          continue;
        }

        if (!entry.isDirectory()) continue;

        // Skip common non-repo directories for performance
        if (entry.name === "node_modules" || entry.name === ".npm" || entry.name === ".cache") {
          continue;
        }

        // Recurse into subdirectories
        scan(fullPath, depth + 1);
      }
    } catch {
      // Ignore permission errors etc
    }
  }

  scan(basePath, 0);
  return gitDirs;
}

/**
 * Get the remote origin URL for a git repository
 *
 * The safe-outputs server runs inside a container whose uid differs from the
 * runner user that owns clones created by `steps:` entries or a manual
 * `actions/checkout`. Under git's "dubious ownership" protection,
 * `git config --get` silently ignores the repository config and exits 1, so
 * every nested clone would look like it had no remote. Inject a scoped
 * safe.directory override for this single config read so discovery works
 * without mutating process-wide git trust for every scanned workspace path.
 *
 * @param {string} repoPath - Path to the repository root
 * @returns {string|null} The remote URL or null if not found
 */
function getRemoteOriginUrl(repoPath) {
  try {
    const url = execGitSync(["config", "--get", "remote.origin.url"], {
      cwd: repoPath,
      env: {
        ...process.env,
        GIT_CONFIG_COUNT: "1",
        GIT_CONFIG_KEY_0: "safe.directory",
        GIT_CONFIG_VALUE_0: path.resolve(repoPath),
      },
      suppressLogs: true,
    });
    return url.trim();
  } catch {
    return null;
  }
}

/**
 * Resolve a workspace-relative path from the checkout manifest into an
 * absolute path under the workspace. The manifest stores an empty string
 * to represent the workspace root. Returns null when the manifest path is
 * absolute or escapes the workspace root via `..` traversal, so a malformed
 * or tampered manifest cannot redirect lookups outside of $GITHUB_WORKSPACE.
 * @param {string} workspaceRoot
 * @param {string} relPath
 * @returns {string|null}
 */
function resolveManifestPath(workspaceRoot, relPath) {
  if (!relPath || relPath === ".") {
    return workspaceRoot;
  }
  if (path.isAbsolute(relPath)) {
    return null;
  }
  const wsResolved = path.resolve(workspaceRoot);
  const resolved = path.resolve(wsResolved, relPath);
  if (resolved !== wsResolved && !resolved.startsWith(wsResolved + path.sep)) {
    return null;
  }
  return resolved;
}

/**
 * Find the checkout directory for a given repo slug
 * Searches the workspace for git repos and matches by remote URL
 *
 * @param {string} repoSlug - The repository slug to find (owner/repo format)
 * @param {string} [workspaceRoot] - The workspace root to search from
 * @param {Object} [options] - Additional options
 * @param {string[]|string} [options.allowedRepos] - Allowed repository patterns for validation
 * @returns {Object} Result with success status and path or error
 */
function findRepoCheckout(repoSlug, workspaceRoot, options = {}) {
  const ws = workspaceRoot || process.env.GITHUB_WORKSPACE || process.cwd();
  const targetSlug = normalizeRepoSlug(repoSlug);

  debugLog(`Searching for repo: ${targetSlug} in workspace: ${ws}`);

  if (!targetSlug) {
    return {
      success: false,
      error: "Invalid repo slug provided",
    };
  }

  // Validate target repo against configured allowlist before searching
  const allowedRepos = parseAllowedRepos(options.allowedRepos);
  if (allowedRepos.size > 0) {
    const defaultRepo = getDefaultTargetRepo();
    const validation = validateTargetRepo(targetSlug, defaultRepo, allowedRepos);
    if (!validation.valid) {
      return { success: false, error: validation.error };
    }
  }

  // First, consult the checkout manifest written by the compiler-emitted
  // "Build checkout manifest for safe-outputs handlers" step. This is the
  // authoritative source for cross-repo checkout paths and does not depend
  // on `git config --get remote.origin.url`, which a later "Configure Git
  // credentials" step may have overwritten to point at the workflow repo.
  // The manifest is written before `actions/checkout` runs, so fall back to
  // the git scan when the resolved path is unsafe or does not exist on disk
  // (failed checkout, workspace wiped, manifest stale).
  const manifestEntry = lookupCheckout(targetSlug);
  if (manifestEntry) {
    const resolved = resolveManifestPath(ws, manifestEntry.path);
    if (resolved && fs.existsSync(resolved)) {
      debugLog(`Found manifest entry for ${targetSlug}: ${resolved}`);
      return {
        success: true,
        path: resolved,
        repoSlug: targetSlug,
      };
    }
    debugLog(`Manifest entry for ${targetSlug} unusable (path: ${manifestEntry.path}), falling back to git scan`);
  }

  // Find all git directories in the workspace
  const gitDirs = findGitDirectories(ws);
  debugLog(`Found ${gitDirs.length} git directories: ${gitDirs.join(", ")}`);

  // Check each git directory for a matching remote
  for (const repoPath of gitDirs) {
    const remoteUrl = getRemoteOriginUrl(repoPath);
    if (!remoteUrl) {
      debugLog(`No remote URL found for: ${repoPath}`);
      continue;
    }

    const foundSlug = extractRepoSlugFromUrl(remoteUrl);
    debugLog(`Repo at ${repoPath} has slug: ${foundSlug}`);

    if (foundSlug === targetSlug) {
      debugLog(`Found match: ${repoPath}`);
      return {
        success: true,
        path: repoPath,
        repoSlug: targetSlug,
      };
    }
  }

  // Special case: check workspace root as a potential match
  // This handles the scenario where only the root is a repo
  const rootRemoteUrl = getRemoteOriginUrl(ws);
  if (rootRemoteUrl) {
    const rootSlug = extractRepoSlugFromUrl(rootRemoteUrl);
    debugLog(`Workspace root has slug: ${rootSlug}`);
    if (rootSlug === targetSlug) {
      return {
        success: true,
        path: ws,
        repoSlug: targetSlug,
      };
    }
  }

  return {
    success: false,
    error: `Repository '${repoSlug}' not found in workspace. Make sure it's checked out under $GITHUB_WORKSPACE, either via a 'checkout:' frontmatter entry or by cloning it into the workspace in a 'steps:' entry.`,
    searchedPaths: gitDirs,
  };
}

/**
 * Build a map of all checked-out repos in the workspace
 * @param {string} [workspaceRoot] - The workspace root to search from
 * @returns {Map<string, string>} Map of repo slug -> checkout path
 */
function buildRepoCheckoutMap(workspaceRoot) {
  const ws = workspaceRoot || process.env.GITHUB_WORKSPACE || process.cwd();
  const map = new Map();

  // Seed from the checkout manifest first so cross-repo entries survive even
  // when a later "Configure Git credentials" step has rewritten remote.origin.url.
  // Only seed entries whose resolved path is safe (inside the workspace) and
  // actually exists on disk, mirroring the guarantee that the git-scan branch
  // provides for every entry it produces.
  const manifest = loadAllCheckouts();
  for (const [slug, entry] of manifest) {
    const resolved = resolveManifestPath(ws, entry.path);
    if (resolved && fs.existsSync(resolved)) {
      map.set(slug, resolved);
    }
  }

  const gitDirs = findGitDirectories(ws);

  for (const repoPath of gitDirs) {
    const remoteUrl = getRemoteOriginUrl(repoPath);
    if (!remoteUrl) continue;

    const slug = extractRepoSlugFromUrl(remoteUrl);
    if (slug && !map.has(slug)) {
      map.set(slug, repoPath);
    }
  }

  // Also check workspace root
  const rootRemoteUrl = getRemoteOriginUrl(ws);
  if (rootRemoteUrl) {
    const rootSlug = extractRepoSlugFromUrl(rootRemoteUrl);
    if (rootSlug && !map.has(rootSlug)) {
      map.set(rootSlug, ws);
    }
  }

  debugLog(`Built repo checkout map with ${map.size} entries`);
  return map;
}

module.exports = {
  findRepoCheckout,
  buildRepoCheckoutMap,
  extractRepoSlugFromUrl,
  normalizeRepoSlug,
  findGitDirectories,
};
