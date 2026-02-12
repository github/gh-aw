// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Allowed repositories helper functions for safe-output cross-repository operations
 * Provides configuration parsing, validation, and resolution logic for allowed-repos
 */

/**
 * Parse the allowed repos from config value (array or comma-separated string)
 * @param {string[]|string|undefined} allowedReposValue - Allowed repos from config (array or comma-separated string)
 * @returns {Set<string>} Set of allowed repository slugs
 */
function parseAllowedRepos(allowedReposValue) {
  const set = new Set();
  if (Array.isArray(allowedReposValue)) {
    allowedReposValue
      .map(repo => repo.trim())
      .filter(repo => repo)
      .forEach(repo => set.add(repo));
  } else if (typeof allowedReposValue === "string") {
    allowedReposValue
      .split(",")
      .map(repo => repo.trim())
      .filter(repo => repo)
      .forEach(repo => set.add(repo));
  }
  return set;
}

/**
 * Get the default target repository from configuration or environment
 * @param {Object} [config] - Optional config object with target-repo field
 * @returns {string} Repository slug in "owner/repo" format
 */
function getDefaultTargetRepo(config) {
  // First check if there's a target-repo in config
  if (config && config["target-repo"]) {
    return config["target-repo"];
  }
  // Fall back to env var for backward compatibility
  const targetRepoSlug = process.env.GH_AW_TARGET_REPO_SLUG;
  if (targetRepoSlug) {
    return targetRepoSlug;
  }
  // Fall back to context repo
  return `${context.repo.owner}/${context.repo.repo}`;
}

/**
 * Resolve target repository configuration from handler config
 * Combines parsing of allowed-repos and resolution of default target repo
 * @param {Object} config - Handler configuration object
 * @returns {{defaultTargetRepo: string, allowedRepos: Set<string>}}
 */
function resolveTargetRepoConfig(config) {
  const defaultTargetRepo = getDefaultTargetRepo(config);
  const allowedRepos = parseAllowedRepos(config.allowed_repos);
  return {
    defaultTargetRepo,
    allowedRepos,
  };
}

module.exports = {
  parseAllowedRepos,
  getDefaultTargetRepo,
  resolveTargetRepoConfig,
};
