// @ts-check
/// <reference types="@actions/github-script" />

/**
 * handler_auth.cjs
 *
 * Shared authentication helper for safe-output handlers.
 * Provides a consistent way to create authenticated GitHub clients,
 * supporting both the step-level token and per-handler tokens for
 * cross-repository operations.
 *
 * Token precedence:
 *   1. config["github-token"] — per-handler PAT (for cross-repo operations)
 *   2. global github          — step-level token set in the github-script with.github-token
 *
 * The step-level token itself follows (as set by the Go compiler):
 *   global safe-outputs.github-token > magic secrets
 */

/**
 * Creates an authenticated GitHub client from the handler configuration.
 *
 * If the handler config contains a "github-token" field, this function creates
 * a new Octokit instance authenticated with that token. This enables cross-repository
 * operations where a PAT with access to the target repo is required.
 *
 * If no per-handler token is configured, the global github object is returned
 * unchanged (preserving the step-level token from with.github-token).
 *
 * Usage in handlers:
 *   const githubClient = await createAuthenticatedGitHubClient(config);
 *   // Use githubClient for all GitHub API calls instead of the global github
 *   const { data } = await githubClient.rest.issues.create({ ... });
 *
 * @param {Object} config - Handler config object, optionally containing "github-token"
 * @returns {Promise<Object>} Authenticated GitHub client — an Octokit instance created via getOctokit()
 *   when a per-handler token is configured, or the global github object (step-level token) otherwise.
 *   Both return values expose the same API surface (rest, graphql, etc.).
 */
async function createAuthenticatedGitHubClient(config) {
  // Note: bracket notation is required because "github-token" contains a hyphen
  // (not a valid JavaScript identifier). This is consistent with other hyphenated
  // config keys like "target-repo" and "allowed-repos".
  const token = config["github-token"];
  if (!token) {
    return github;
  }
  core.info("Using per-handler github-token for cross-repository authentication");
  return global.getOctokit(token, /** @type {any} */ { credentialSource: "pat" });
}

module.exports = { createAuthenticatedGitHubClient };
