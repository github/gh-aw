// @ts-check
/// <reference types="@actions/github-script" />

const { validateTargetRepo, parseAllowedRepos, getDefaultTargetRepo } = require("./repo_helpers.cjs");
const { ERR_VALIDATION, ERR_NOT_FOUND } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { checkoutHasPersistedExtraheader, gitExecSilent } = require("./git_auth_helpers.cjs");
const { maskSecret } = require("./actions_secret_masking.cjs");

/**
 * Dynamic repository checkout utilities for multi-repo scenarios
 * Enables switching between different repositories during handler execution
 */

/**
 * Get the currently checked out repository slug from git remote
 * @returns {Promise<string|null>} The repo slug (owner/repo) or null if not determinable
 */
async function getCurrentCheckoutRepo() {
  try {
    const result = await exec.getExecOutput("git", ["config", "--get", "remote.origin.url"], { silent: true });
    const url = result.stdout.trim();

    // Extract repo slug from URL
    // Handle HTTPS and SSH formats
    /** @type {any} */
    let slug = null;

    // Remove .git suffix if present
    let cleanUrl = url;
    if (cleanUrl.endsWith(".git")) {
      cleanUrl = cleanUrl.slice(0, -4);
    }

    // HTTPS: https://github.com/owner/repo
    const httpsMatch = cleanUrl.match(/https?:\/\/[^/]+\/([^/]+\/[^/]+)$/);
    if (httpsMatch) {
      slug = httpsMatch[1].toLowerCase();
    }

    // SSH: git@github.com:owner/repo
    const sshMatch = cleanUrl.match(/git@[^:]+:([^/]+\/[^/]+)$/);
    if (sshMatch) {
      slug = sshMatch[1].toLowerCase();
    }

    return slug;
  } catch {
    return null;
  }
}

/**
 * Checkout a different repository for patch application
 * This is used when processing entries with a `repo` parameter that differs from the current checkout
 *
 * @param {string} repoSlug - Repository slug (owner/repo) to checkout
 * @param {string} token - GitHub token for authentication
 * @param {Object} options - Additional options
 * @param {string} [options.baseBranch] - Base branch to checkout (defaults to 'main')
 * @param {string[]|string} [options.allowedRepos] - Allowed repository patterns for allowlist validation
 * @returns {Promise<Object>} Result with success status
 */
async function checkoutRepo(repoSlug, token, options = {}) {
  const baseBranch = options.baseBranch || "main";
  const parts = (repoSlug || "").trim().split("/");

  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return {
      success: false,
      error: `${ERR_VALIDATION}: Invalid repository slug: ${repoSlug}. Expected format: owner/repo`,
    };
  }

  // Validate target repo against configured allowlist before any git operations
  const allowedRepos = parseAllowedRepos(options.allowedRepos);
  if (allowedRepos.size > 0) {
    const defaultRepo = getDefaultTargetRepo();
    const validation = validateTargetRepo(repoSlug, defaultRepo, allowedRepos);
    if (!validation.valid) {
      return { success: false, error: `${ERR_VALIDATION}: ${validation.error}` };
    }
  }

  const [owner, repo] = parts;

  core.info(`Switching checkout to repository: ${repoSlug}`);

  try {
    // Get GitHub server URL (for GHES support)
    const serverUrl = process.env.GITHUB_SERVER_URL || "https://github.com";

    // Configure the new remote URL (no embedded credentials). Authentication is
    // provided by the http.<serverUrl>/.extraheader credential that the safe_outputs
    // checkout persisted into .git/config (persist-credentials: true with the resolved
    // push token). That extraheader applies to every <serverUrl> URL, so it already
    // authenticates the repository we are switching to.
    const remoteUrl = `${serverUrl}/${repoSlug}.git`;

    // Change remote origin to the new repo. Replacing the URL also drops any
    // embedded-credential URL configure_git_credentials.sh may have set, leaving the
    // persisted extraheader as the single source of auth.
    core.info(`Configuring remote origin to: ${repoSlug}`);
    await exec.exec("git", ["remote", "set-url", "origin", remoteUrl]);

    // Trust the persisted credential rather than injecting a second one. git treats
    // http.<url>.extraheader as multi-valued, so adding our own header on top of the
    // checkout's persisted header would put two Authorization headers on the wire,
    // which GitHub rejects with "Duplicate header: 'Authorization'" (HTTP 400). Only
    // configure an extraheader when the checkout did not already persist one (e.g. a
    // caller that runs without persist-credentials).
    const hasPersistedAuth = await checkoutHasPersistedExtraheader(serverUrl);
    if (!hasPersistedAuth) {
      // Use extraheader to pass the token without embedding it in the URL (more secure).
      maskSecret(token);
      const tokenBase64 = Buffer.from(`x-access-token:${token}`).toString("base64");
      maskSecret(tokenBase64);
      await gitExecSilent(["config", `http.${serverUrl}/.extraheader`, `Authorization: basic ${tokenBase64}`]);
    } else {
      core.info("Reusing persisted git credential for authentication (skipping extraheader injection)");
    }

    // Fetch the new repo
    core.info(`Fetching repository: ${repoSlug}`);
    await exec.exec("git", ["fetch", "origin", "--prune"]);

    // Reset to the base branch of the new repo
    core.info(`Checking out base branch: ${baseBranch}`);
    try {
      await exec.exec("git", ["checkout", "-B", baseBranch, `origin/${baseBranch}`]);
    } catch {
      throw new Error(`${ERR_NOT_FOUND}: Base branch ${baseBranch} not found in ${repoSlug}`);
    }

    // Clean up any local changes
    await exec.exec("git", ["clean", "-fd"]);
    await exec.exec("git", ["reset", "--hard", "HEAD"]);

    core.info(`Successfully switched to repository: ${repoSlug}`);

    return {
      success: true,
      repoSlug: repoSlug,
    };
  } catch (error) {
    const errorMsg = getErrorMessage(error);
    core.error(`Failed to checkout repository ${repoSlug}: ${errorMsg}`);
    return {
      success: false,
      error: `Failed to checkout repository ${repoSlug}: ${errorMsg}`,
    };
  }
}

/**
 * Track and manage the currently active checkout
 * Returns a manager object that handles repo switching
 *
 * @param {string} token - GitHub token for authentication
 * @param {Object} options - Options
 * @param {string} [options.defaultBaseBranch] - Default base branch
 * @returns {Object} Checkout manager with switchTo method
 */
function createCheckoutManager(token, options = {}) {
  /** @type {any} */
  let currentRepo = null;
  const defaultBaseBranch = options.defaultBaseBranch || "main";

  return {
    /**
     * Get the currently checked out repo
     * @returns {Promise<string|null>}
     */
    async getCurrent() {
      if (!currentRepo) {
        currentRepo = await getCurrentCheckoutRepo();
      }
      return currentRepo;
    },

    /**
     * Switch to a different repository if needed
     * @param {string} targetRepo - Target repo slug
     * @param {Object} [opts] - Options
     * @param {string} [opts.baseBranch] - Base branch to use
     * @returns {Promise<Object>} Result with success status
     */
    async switchTo(targetRepo, opts = {}) {
      const baseBranch = opts.baseBranch || defaultBaseBranch;
      const targetLower = targetRepo.toLowerCase();

      // Get current checkout if not known
      if (!currentRepo) {
        currentRepo = await getCurrentCheckoutRepo();
      }

      // Check if we're already on the right repo
      if (currentRepo === targetLower) {
        core.info(`Already on repository: ${targetRepo}`);
        return { success: true, switched: false };
      }

      // Need to switch
      core.info(`Switching from ${currentRepo || "unknown"} to ${targetRepo}`);
      const result = await checkoutRepo(targetRepo, token, { baseBranch });

      if (result.success) {
        currentRepo = targetLower;
        return { success: true, switched: true };
      }

      return result;
    },
  };
}

/**
 * Cache of repositories materialized by materializeRepo in this process, keyed by
 * lowercase repo slug, so multiple messages targeting the same repository reuse it.
 * @type {Map<string, string>}
 */
const materializedRepos = new Map();

/**
 * Initialize (or reuse) a standalone git repository for `repoSlug` under RUNNER_TEMP,
 * without actions/checkout. Used when safe-outputs.dynamic-checkout is enabled.
 *
 * The repository gets an `origin` remote, a local git identity, and a local
 * http.<server>/.extraheader credential built from `token`, scoped to this temp
 * repository only (never the workspace). When `baseBranch` is provided, it is
 * shallow-fetched and checked out so HEAD points at a commit.
 *
 * @param {string} repoSlug - Repository slug (owner/repo)
 * @param {string} token - Token used to authenticate fetch/push for this repository
 * @param {Object} [options]
 * @param {string} [options.baseBranch] - Branch to shallow-fetch and check out
 * @param {boolean} [options.partialClone] - Configure origin as a blob:none promisor remote on first init
 * @param {string} [options.rootDir] - Override the root directory (defaults to $RUNNER_TEMP/gh-aw/dynamic-checkout)
 * @returns {Promise<{success: true, path: string, reused: boolean} | {success: false, error: string}>}
 */
async function materializeRepo(repoSlug, token, options = {}) {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");

  const parts = (repoSlug || "").trim().split("/");
  if (parts.length !== 2 || !/^[A-Za-z0-9_.-]+$/.test(parts[0]) || !/^[A-Za-z0-9_.-]+$/.test(parts[1]) || parts.some(p => p === "." || p === "..")) {
    return { success: false, error: `${ERR_VALIDATION}: Invalid repository slug: ${repoSlug}. Expected format: owner/repo` };
  }
  if (!token) {
    return { success: false, error: `${ERR_VALIDATION}: dynamic-checkout requires a GitHub token (GITHUB_TOKEN) to fetch ${repoSlug}` };
  }

  const key = repoSlug.trim().toLowerCase();
  const rootDir = options.rootDir || path.join(process.env.RUNNER_TEMP || os.tmpdir(), "gh-aw", "dynamic-checkout");
  const repoDir = path.join(rootDir, ...key.split("/"));
  const serverUrl = (process.env.GITHUB_SERVER_URL || "https://github.com").replace(/\/+$/, "");
  const gitOpts = { cwd: repoDir };

  try {
    const reused = materializedRepos.get(key) === repoDir && fs.existsSync(path.join(repoDir, ".git"));
    if (reused) {
      core.info(`Reusing dynamic checkout of ${repoSlug} at ${repoDir}`);
      // Discard state left behind by a previous message (failed am, dirty tree).
      await exec.exec("git", ["am", "--abort"], { ...gitOpts, ignoreReturnCode: true, silent: true });
      await exec.exec("git", ["reset", "--hard"], { ...gitOpts, ignoreReturnCode: true, silent: true });
      await exec.exec("git", ["clean", "-fdx"], { ...gitOpts, ignoreReturnCode: true, silent: true });
    } else {
      core.info(`Initializing dynamic checkout of ${repoSlug} at ${repoDir}`);
      fs.rmSync(repoDir, { recursive: true, force: true });
      fs.mkdirSync(repoDir, { recursive: true });
      await exec.exec("git", ["init", "--quiet"], gitOpts);
      await exec.exec("git", ["remote", "add", "origin", `${serverUrl}/${repoSlug.trim()}.git`], gitOpts);
      await exec.exec("git", ["config", "--local", "user.email", "github-actions[bot]@users.noreply.github.com"], gitOpts);
      await exec.exec("git", ["config", "--local", "user.name", "github-actions[bot]"], gitOpts);
      await exec.exec("git", ["config", "--local", "am.keepcr", "true"], gitOpts);
      if (options.partialClone) {
        // Blobless partial clone: full commit/tree history (so ancestry checks work) while
        // blobs are fetched lazily using the local credential configured below.
        await exec.exec("git", ["config", "--local", "remote.origin.promisor", "true"], gitOpts);
        await exec.exec("git", ["config", "--local", "remote.origin.partialclonefilter", "blob:none"], gitOpts);
      }

      maskSecret(token);
      const tokenBase64 = Buffer.from(`x-access-token:${token}`).toString("base64");
      maskSecret(tokenBase64);
      await gitExecSilent(["config", "--local", `http.${serverUrl}/.extraheader`, `Authorization: basic ${tokenBase64}`], repoDir);
      materializedRepos.set(key, repoDir);
    }

    if (options.baseBranch) {
      const baseBranch = options.baseBranch;
      core.info(`Fetching base branch ${baseBranch} for ${repoSlug}`);
      try {
        await exec.exec("git", ["fetch", "--depth=1", "origin", `+refs/heads/${baseBranch}:refs/remotes/origin/${baseBranch}`], gitOpts);
      } catch (fetchError) {
        throw new Error(`${ERR_NOT_FOUND}: Base branch ${baseBranch} could not be fetched from ${repoSlug}: ${getErrorMessage(fetchError)}`);
      }
      await exec.exec("git", ["checkout", "--quiet", "-B", baseBranch, `refs/remotes/origin/${baseBranch}`], gitOpts);
    }

    return { success: true, path: repoDir, reused };
  } catch (error) {
    materializedRepos.delete(key);
    const errorMsg = getErrorMessage(error);
    core.error(`Failed to materialize repository ${repoSlug}: ${errorMsg}`);
    return { success: false, error: `Failed to materialize repository ${repoSlug}: ${errorMsg}` };
  }
}

module.exports = {
  getCurrentCheckoutRepo,
  checkoutRepo,
  createCheckoutManager,
  materializeRepo,
};
