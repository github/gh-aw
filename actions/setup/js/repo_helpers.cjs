// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Repository-related helper functions for safe-output scripts
 * Provides common repository parsing, validation, and resolution logic
 */

const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

// ============================================================================
// Type Definitions
// ============================================================================

/**
 * Parsed repository owner and name
 * @typedef {Object} RepoParts
 * @property {string} owner - Repository owner (organization or user)
 * @property {string} repo - Repository name
 */

/**
 * Result of repository validation
 * @typedef {Object} RepoValidationResult
 * @property {boolean} valid - Whether the repository is allowed
 * @property {string|null} error - Error message if validation failed, null otherwise
 * @property {string} qualifiedRepo - Fully qualified repository slug (owner/repo)
 */

/**
 * Successful result from resolveAndValidateRepo
 * @typedef {Object} RepoResolutionSuccess
 * @property {true} success - Always true for success
 * @property {string} repo - Fully qualified repository slug (owner/repo)
 * @property {RepoParts} repoParts - Parsed owner and repo components
 */

/**
 * Failed result from resolveAndValidateRepo
 * @typedef {Object} RepoResolutionError
 * @property {false} success - Always false for error
 * @property {string} error - Error message describing why resolution failed
 */

/**
 * Union type for resolveAndValidateRepo result
 * @typedef {RepoResolutionSuccess | RepoResolutionError} RepoResolutionResult
 */

/**
 * Result of resolveTargetRepoConfig
 * @typedef {Object} TargetRepoConfig
 * @property {string} defaultTargetRepo - Default target repository slug
 * @property {Set<string>} allowedRepos - Set of allowed repository patterns
 */

/**
 * Handler configuration object with repository settings
 * @typedef {Object} HandlerRepoConfig
 * @property {string} [target-repo] - Configured target repository
 * @property {string[]|string} [allowed_repos] - Allowed repositories (array or comma-separated)
 */

/**
 * Message item that may contain a repo field
 * @typedef {Object} MessageItemWithRepo
 * @property {string} [repo] - Optional repository slug override
 */

// ============================================================================
// Functions
// ============================================================================

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
 * @param {HandlerRepoConfig | import('./types/handler-factory').HandlerConfig} [config] - Optional config object with target-repo field
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
  // Fall back to context repo (only available in github-script or shim-provided context)
  if (typeof context !== "undefined" && context.repo?.owner && context.repo?.repo) {
    return `${context.repo.owner}/${context.repo.repo}`;
  }
  // Fall back to GITHUB_REPOSITORY env var (available in standalone daemon mode)
  const githubRepo = process.env.GITHUB_REPOSITORY;
  if (githubRepo) {
    return githubRepo;
  }
  return "";
}

/**
 * Check if a qualified repo matches any allowed repo pattern.
 * Supports exact matches and wildcard patterns using glob syntax:
 *   - "*" matches any repository
 *   - "github/*" matches any repository in the "github" org
 *   - "STAR/gh-aw" (where STAR is *) matches "gh-aw" in any org
 * @param {string} qualifiedRepo - Fully qualified repo slug "owner/repo"
 * @param {Set<string>} allowedRepos - Set of allowed repo patterns
 * @returns {boolean}
 */
function isRepoAllowed(qualifiedRepo, allowedRepos) {
  // Fast path: exact match
  if (allowedRepos.has(qualifiedRepo)) {
    return true;
  }
  // Check for wildcard patterns
  for (const pattern of allowedRepos) {
    if (pattern === "*") {
      return true;
    }
    if (pattern.includes("*") && globPatternToRegex(pattern, { pathMode: true, caseSensitive: true }).test(qualifiedRepo)) {
      return true;
    }
  }
  return false;
}

/**
 * Validate that a repo is allowed for operations
 * If repo is a bare name (no slash), it is automatically qualified with the
 * default repo's organization (e.g., "gh-aw" becomes "github/gh-aw" if
 * the default repo is "github/something").
 * Allowed repos support wildcard patterns (e.g., "github/*", "*").
 * @param {string} repo - Repository slug to validate (can be "owner/repo" or just "repo")
 * @param {string} defaultRepo - Default target repository
 * @param {Set<string>} allowedRepos - Set of explicitly allowed repo patterns
 * @returns {RepoValidationResult}
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

  // Wildcard default repo allows any target repo, but still require a valid owner/repo slug
  if (defaultRepo === "*") {
    const parsed = parseRepoSlug(qualifiedRepo);
    if (!parsed) {
      return {
        valid: false,
        error: `Repository '${repo}' is not a valid 'owner/repo' slug.`,
        qualifiedRepo,
      };
    }
    // Normalize to a fully-qualified slug to honor the contract of RepoValidationResult
    qualifiedRepo = `${parsed.owner}/${parsed.repo}`;
    return { valid: true, error: null, qualifiedRepo };
  }

  // Default repo is always allowed
  if (qualifiedRepo === defaultRepo) {
    return { valid: true, error: null, qualifiedRepo };
  }
  // Check if it's in the allowed repos list (supports wildcards)
  if (isRepoAllowed(qualifiedRepo, allowedRepos)) {
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
 * @returns {RepoParts|null}
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
 * @param {HandlerRepoConfig | import('./types/handler-factory').HandlerConfig} config - Handler configuration object
 * @returns {TargetRepoConfig}
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
 * @param {MessageItemWithRepo} item - Message item that may contain a repo field
 * @param {string} defaultTargetRepo - Default target repository slug
 * @param {Set<string>} allowedRepos - Set of allowed repository slugs
 * @param {string} operationType - Type of operation (e.g., "comment", "pull request", "issue") for error messages
 * @returns {RepoResolutionResult}
 */
function resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, operationType) {
  // Normalize the default target repo (may be empty if not configured)
  const trimmedDefaultTargetRepo = defaultTargetRepo ? String(defaultTargetRepo).trim() : "";

  // Determine target repository for this operation, allowing item.repo to override
  const rawItemRepo = item && item.repo != null ? String(item.repo).trim() : "";
  const itemRepo = rawItemRepo || trimmedDefaultTargetRepo;

  // If we still don't have a repo after considering overrides, treat as configuration/environment issue
  if (!itemRepo) {
    return {
      success: false,
      error: `Unable to determine target repository for ${operationType}. Set GH_AW_TARGET_REPO_SLUG, ensure GITHUB_REPOSITORY is available, or configure target-repo in safe-outputs settings.`,
    };
  }

  // Validate the repository is allowed
  const repoValidation = validateRepo(itemRepo, trimmedDefaultTargetRepo, allowedRepos);
  if (!repoValidation.valid) {
    // When valid is false, error is guaranteed to be non-null
    const errorMessage = repoValidation.error;
    if (!errorMessage) {
      throw new Error(`${ERR_VALIDATION}: Internal error: repoValidation.error should not be null when valid is false`);
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

/**
 * Validate a target repository for cross-repository operations.
 * Shared utility for handlers that accept user-supplied target repositories;
 * must be called before any API interaction with the cross-repo target.
 * This named function makes cross-repo validation intent explicit and
 * allows conformance checks to verify SEC-005 compliance by file.
 * @param {string} repo - Repository slug to validate (can be "owner/repo" or just "repo")
 * @param {string} defaultRepo - Default (always-allowed) target repository
 * @param {Set<string>} allowedRepos - Set of explicitly allowed repo patterns
 * @returns {RepoValidationResult}
 */
function validateTargetRepo(repo, defaultRepo, allowedRepos) {
  return validateRepo(repo, defaultRepo, allowedRepos);
}

/**
 * Resolve the execution owner/repo pair for maintenance scripts.
 *
 * In the SideRepoOps pattern a hosting repo manages workflows that operate on a
 * separate target repository.  When `GH_AW_TARGET_REPO_SLUG` is set to a valid
 * "owner/repo" slug the maintenance scripts should operate against that repo
 * instead of the workflow execution context (`context.repo`).
 *
 * Throws when `GH_AW_TARGET_REPO_SLUG` is present but not in exact "owner/repo" format
 * to prevent silently operating against the wrong repository.
 *
 * @returns {{ owner: string, repo: string }}
 */
function resolveExecutionOwnerRepo() {
  const targetRepoSlug = process.env.GH_AW_TARGET_REPO_SLUG;
  if (targetRepoSlug) {
    const parsed = parseRepoSlug(targetRepoSlug);
    if (!parsed) {
      throw new Error(`${ERR_VALIDATION}: Invalid GH_AW_TARGET_REPO_SLUG: "${targetRepoSlug}". Expected exact "owner/repo" format (one slash, non-empty on both sides).`);
    }
    return parsed;
  }
  return { owner: context.repo.owner, repo: context.repo.repo };
}

/**
 * Resolve the configured failure-issue repository (`GH_AW_FAILURE_ISSUE_REPO`).
 *
 * SEC-005: when the value was written as a literal "owner/repo" in the workflow
 * frontmatter it is trusted compile-time configuration. When it was written as a GitHub
 * Actions expression (for example `${{ inputs.failure-issue-repo }}` in a reusable
 * workflow) the compiler sets `GH_AW_FAILURE_ISSUE_REPO_FROM_EXPRESSION` to "true"; the
 * resolved value is then caller-controlled at runtime and is validated with
 * `validateTargetRepo` against an explicit allowlist limited to the current repository
 * owner before any API use.
 *
 * @param {string} defaultRepoSlug - Current repository slug ("owner/repo") used as the default
 * @param {(message: string) => void} [warn] - Optional warning sink for rejected values
 * @returns {RepoParts|null} Parsed owner/repo when a usable override is configured, otherwise null
 */
function resolveFailureIssueRepo(defaultRepoSlug, warn) {
  const configured = (process.env.GH_AW_FAILURE_ISSUE_REPO || "").trim();
  if (!configured) {
    return null;
  }
  const parsed = parseRepoSlug(configured);
  if (!parsed) {
    if (warn) {
      warn(`Ignoring invalid failure-issue-repo value "${configured}"; expected "owner/repo" format`);
    }
    return null;
  }
  if (process.env.GH_AW_FAILURE_ISSUE_REPO_FROM_EXPRESSION !== "true") {
    return parsed;
  }
  const defaultOwner = parseRepoSlug(defaultRepoSlug)?.owner;
  const allowedRepos = parseAllowedRepos(defaultOwner ? `${defaultOwner}/*` : "");
  const validation = validateTargetRepo(`${parsed.owner}/${parsed.repo}`, defaultRepoSlug, allowedRepos);
  if (!validation.valid) {
    if (warn) {
      warn(`Ignoring runtime failure-issue-repo value "${parsed.owner}/${parsed.repo}": expression-provided repositories must belong to the "${defaultOwner}" owner`);
    }
    return null;
  }
  return parsed;
}

module.exports = {
  parseAllowedRepos,
  getDefaultTargetRepo,
  isRepoAllowed,
  validateRepo,
  validateTargetRepo,
  parseRepoSlug,
  resolveTargetRepoConfig,
  resolveAndValidateRepo,
  resolveExecutionOwnerRepo,
  resolveFailureIssueRepo,
};
