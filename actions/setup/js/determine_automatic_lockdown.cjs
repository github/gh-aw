// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Determines automatic lockdown mode for GitHub MCP server based on repository visibility
 * and custom token availability.
 *
 * Lockdown mode is automatically enabled for public repositories when ANY custom GitHub token
 * is configured (GH_AW_GITHUB_TOKEN, GH_AW_GITHUB_MCP_SERVER_TOKEN, or custom github-token).
 * This prevents unauthorized access to private repositories that the token may have access to.
 *
 * For public repositories WITHOUT custom tokens, lockdown mode is disabled (false) as
 * the default GITHUB_TOKEN is already scoped to the current repository.
 *
 * For private repositories, lockdown mode is not necessary (false) as there is no risk
 * of exposing private repository access.
 *
 * @param {any} github - GitHub API client
 * @param {any} context - GitHub context
 * @param {any} core - GitHub Actions core library
 * @returns {Promise<void>}
 */
async function determineAutomaticLockdown(github, context, core) {
  try {
    core.info("Determining automatic lockdown mode for GitHub MCP server");

    const { owner, repo } = context.repo;
    core.info(`Checking repository: ${owner}/${repo}`);

    // Fetch repository information
    const { data: repository } = await github.rest.repos.get({
      owner,
      repo,
    });

    const isPrivate = repository.private;
    const visibility = repository.visibility || (isPrivate ? "private" : "public");

    core.info(`Repository visibility: ${visibility}`);
    core.info(`Repository is private: ${isPrivate}`);

    // Check if any custom GitHub token is configured
    const hasGhAwToken = !!process.env.GH_AW_GITHUB_TOKEN;
    const hasGhAwMcpToken = !!process.env.GH_AW_GITHUB_MCP_SERVER_TOKEN;
    const hasCustomToken = !!process.env.CUSTOM_GITHUB_TOKEN;
    const hasAnyCustomToken = hasGhAwToken || hasGhAwMcpToken || hasCustomToken;

    core.info(`GH_AW_GITHUB_TOKEN configured: ${hasGhAwToken}`);
    core.info(`GH_AW_GITHUB_MCP_SERVER_TOKEN configured: ${hasGhAwMcpToken}`);
    core.info(`Custom github-token configured: ${hasCustomToken}`);
    core.info(`Any custom token configured: ${hasAnyCustomToken}`);

    // Set lockdown based on visibility AND custom token availability
    // Public repos with any custom token should have lockdown enabled to prevent token from accessing private repos
    // Public repos without custom tokens use default GITHUB_TOKEN (already scoped), so lockdown is not needed
    const shouldLockdown = !isPrivate && hasAnyCustomToken;

    core.info(`Automatic lockdown mode determined: ${shouldLockdown}`);
    core.setOutput("lockdown", shouldLockdown.toString());
    core.setOutput("visibility", visibility);

    if (shouldLockdown) {
      core.info("Automatic lockdown mode enabled for public repository with custom GitHub token");
      core.warning("GitHub MCP lockdown mode enabled for public repository. " + "This prevents the GitHub token from accessing private repositories.");
    } else if (!isPrivate && !hasAnyCustomToken) {
      core.info("Automatic lockdown mode disabled for public repository (no custom tokens configured)");
      core.info("To enable lockdown mode for enhanced security, configure GH_AW_GITHUB_TOKEN as a repository secret and set 'lockdown: true' in your workflow.");
    } else {
      core.info("Automatic lockdown mode disabled for private/internal repository");
    }
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    core.error(`Failed to determine automatic lockdown mode: ${errorMessage}`);
    // Default to lockdown mode for safety
    core.setOutput("lockdown", "true");
    core.setOutput("visibility", "unknown");
    core.warning("Failed to determine repository visibility. Defaulting to lockdown mode for security.");
  }
}

module.exports = determineAutomaticLockdown;
