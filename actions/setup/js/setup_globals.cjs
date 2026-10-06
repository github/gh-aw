// @ts-check
/// <reference types="@actions/github-script" />

/**
 * setup_globals.cjs
 * Helper function to store GitHub Actions builtin objects in the global scope
 * This allows required modules to access these objects without needing to pass them as parameters
 */

const { createRateLimitAwareGithub } = require("./github_rate_limit_logger.cjs");
const { parseRuntimeFeatures, hasRuntimeFeature, getRuntimeFeatureValue } = require("./runtime_features.cjs");
const { GITHUB_API_VERSION } = require("./constants.cjs");

/**
 * @param {Record<string, string | number | undefined>} headers
 * @param {Record<string, string | number | undefined>} [defaults]
 * @returns {void}
 */
function applyGitHubApiVersion(headers, defaults = {}) {
  const headerNames = new Set(Object.keys(headers).map(name => name.toLowerCase()));
  for (const [name, value] of Object.entries(defaults)) {
    const normalizedName = name.toLowerCase();
    if (!headerNames.has(normalizedName)) {
      headers[normalizedName] = value;
      headerNames.add(normalizedName);
    }
  }
  if (!headerNames.has("x-github-api-version")) {
    headers["x-github-api-version"] = GITHUB_API_VERSION;
  }
}

/**
 * Identify only recognizable credential types; never persist or display tokens.
 * @param {string} token
 * @returns {string}
 */
function credentialSource(token) {
  if (typeof token !== "string") return "unknown";
  if (process.env.GITHUB_TOKEN && token === process.env.GITHUB_TOKEN) return "github_actions";
  if (token.startsWith("github_pat_") || token.startsWith("ghp_")) return "pat";
  if (token.startsWith("ghs_") || token.startsWith("ghu_")) return "app";
  return "unknown";
}

/**
 * Stores GitHub Actions builtin objects (core, github, context, exec, io, getOctokit) in the global scope
 * This must be called before requiring any script that depends on these globals
 *
 * The github object is wrapped with a rate-limit-aware proxy so that every
 * github.rest.*.*() call automatically logs rate-limit headers to
 * /tmp/gh-aw/github_rate_limits.jsonl for post-run observability.
 *
 * @param {typeof core} coreModule - The @actions/core module
 * @param {any} githubModule - The @actions/github module
 * @param {typeof context} contextModule - The GitHub context object
 * @param {typeof exec} execModule - The @actions/exec module
 * @param {typeof io} ioModule - The @actions/io module
 * @param {typeof getOctokit} getOctokitFn - The getOctokit function (builtin in actions/github-script@v9)
 */
function setupGlobals(coreModule, githubModule, contextModule, execModule, ioModule, getOctokitFn) {
  global.core = coreModule;
  const runtimeFeatures = Object.freeze(parseRuntimeFeatures(process.env.GH_AW_RUNTIME_FEATURES));
  global.runtimeFeatures = runtimeFeatures;
  global.hasRuntimeFeature = key => hasRuntimeFeature(runtimeFeatures, key);
  global.getRuntimeFeatureValue = key => getRuntimeFeatureValue(runtimeFeatures, key);
  // Pin a supported REST API version, preserving explicit overrides even
  // after Octokit normalizes header names to lowercase.
  githubModule.hook.before("request", options => {
    applyGitHubApiVersion(options.headers);
  });
  // @ts-expect-error - Assigning to global properties that are declared as const
  // Wrap the github object so every github.rest.*.*() call automatically logs
  // x-ratelimit-* headers to github_rate_limits.jsonl for observability.
  global.github = createRateLimitAwareGithub(githubModule);
  global.context = contextModule;
  // @ts-expect-error - Assigning to global properties that are declared as const
  global.exec = execModule;
  // @ts-expect-error - Assigning to global properties that are declared as const
  global.io = ioModule;
  // Wrap getOctokit so every client created via global.getOctokit(token) also
  // carries the default X-GitHub-Api-Version for
  // per-handler authenticated clients (cross-repo PAT operations, etc.).
  // Also validates that the token is not an OAuth token (gho_...) which is
  // unsuitable for automation.
  global.getOctokit = (token, options = {}) => {
    if (typeof token === "string" && token.startsWith("gho_")) {
      throw new Error(
        "OAuth token (gho_...) detected. OAuth tokens are not suitable for automation: " +
          "they are typically over-provisioned, may expire when the user logs out, and cannot be " +
          "scoped to specific repositories. Replace the token with a fine-grained Personal Access Token " +
          "at: https://github.com/settings/personal-access-tokens/new"
      );
    }
    const client = getOctokitFn(token, options);
    // Octokit constructor options do not apply top-level headers to requests.
    client.hook.before("request", requestOptions => {
      applyGitHubApiVersion(requestOptions.headers, options.headers);
    });
    return createRateLimitAwareGithub(client, credentialSource(token));
  };
}

module.exports = { setupGlobals };
