// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Repository-related helper functions for safe-output scripts
 * Provides common repository parsing, validation, and resolution logic
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
 * Get the default target repository
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
 * Validate that a repo is allowed for operations
 * If repo is a bare name (no slash), it is automatically qualified with the
 * default repo's organization (e.g., "gh-aw" becomes "github/gh-aw" if
 * the default repo is "github/something").
 * @param {string} repo - Repository slug to validate (can be "owner/repo" or just "repo")
 * @param {string} defaultRepo - Default target repository
 * @param {Set<string>} allowedRepos - Set of explicitly allowed repos
 * @returns {{valid: boolean, error: string|null, qualifiedRepo: string}}
 */
function validateRepo(repo, defaultRepo, allowedRepos) {
  // If repo is a bare name (no slash), qualify it with the default repo's org
  let qualifiedRepo = repo;
  if (!repo.includes("/")) {
    const defaultRepoParts = parseRepoSlug(defaultRepo);
    if (defaultRepoParts) {
      qualifiedRepo = `${defaultRepoParts.owner}/${repo}`;
    }
  }

  // Default repo is always allowed
  if (qualifiedRepo === defaultRepo) {
    return { valid: true, error: null, qualifiedRepo };
  }
  // Check if it's in the allowed repos list
  if (allowedRepos.has(qualifiedRepo)) {
    return { valid: true, error: null, qualifiedRepo };
  }
  return {
    valid: false,
    error: `Repository '${repo}' is not in the allowed-repos list. Allowed: ${defaultRepo}${allowedRepos.size > 0 ? ", " + Array.from(allowedRepos).join(", ") : ""}`,
    qualifiedRepo,
  };
}

/**
 * Parse owner and repo from a repository slug
 * @param {string} repoSlug - Repository slug in "owner/repo" format
 * @returns {{owner: string, repo: string}|null}
 */
function parseRepoSlug(repoSlug) {
  const parts = repoSlug.split("/");
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return null;
  }
  return { owner: parts[0], repo: parts[1] };
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

/**
 * Resolve and validate target repository from a message item
 * Combines repo resolution, validation, and parsing into a single function
 * @param {Object} item - Message item that may contain a repo field
 * @param {string} defaultTargetRepo - Default target repository slug
 * @param {Set<string>} allowedRepos - Set of allowed repository slugs
 * @param {string} operationType - Type of operation (e.g., "comment", "pull request", "issue") for error messages
 * @returns {{success: true, repo: string, repoParts: {owner: string, repo: string}}|{success: false, error: string}}
 */
function resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, operationType) {
  // Determine target repository for this operation
  const itemRepo = item.repo ? String(item.repo).trim() : defaultTargetRepo;

  // Validate the repository is allowed
  const repoValidation = validateRepo(itemRepo, defaultTargetRepo, allowedRepos);
  if (!repoValidation.valid) {
    // When valid is false, error is guaranteed to be non-null
    const errorMessage = repoValidation.error;
    if (!errorMessage) {
      throw new Error("Internal error: repoValidation.error should not be null when valid is false");
    }
    return {
      success: false,
      error: errorMessage,
    };
  }

  // Use the qualified repo from validation (handles bare names)
  const qualifiedItemRepo = repoValidation.qualifiedRepo;

  // Parse the repository slug
  const repoParts = parseRepoSlug(qualifiedItemRepo);
  if (!repoParts) {
    return {
      success: false,
      error: `Invalid repository format '${itemRepo}'. Expected 'owner/repo'.`,
    };
  }

  return {
    success: true,
    repo: qualifiedItemRepo,
    repoParts: repoParts,
  };
}

module.exports = {
  parseAllowedRepos,
  getDefaultTargetRepo,
  validateRepo,
  parseRepoSlug,
  resolveTargetRepoConfig,
  resolveAndValidateRepo,
};
