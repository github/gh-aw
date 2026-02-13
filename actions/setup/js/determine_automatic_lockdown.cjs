// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Determines automatic lockdown mode for GitHub MCP server based on repository visibility
 * and GH_AW_GITHUB_TOKEN availability.
 *
 * Lockdown mode is automatically enabled for public repositories ONLY when GH_AW_GITHUB_TOKEN
 * is configured as a repository secret. This prevents unauthorized access to private repositories
 * that the token may have access to.
 *
 * For public repositories WITHOUT GH_AW_GITHUB_TOKEN, lockdown mode is disabled (false) as
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

    // Check if GH_AW_GITHUB_TOKEN is set
    const hasGhAwToken = !!process.env.GH_AW_GITHUB_TOKEN;
    core.info(`GH_AW_GITHUB_TOKEN configured: ${hasGhAwToken}`);

    // Set lockdown based on visibility AND token availability
    // Public repos with GH_AW_GITHUB_TOKEN should have lockdown enabled to prevent token from accessing private repos
    // Public repos without GH_AW_GITHUB_TOKEN use default GITHUB_TOKEN (already scoped), so lockdown is not needed
    const shouldLockdown = !isPrivate && hasGhAwToken;

    core.info(`Automatic lockdown mode determined: ${shouldLockdown}`);
    core.setOutput("lockdown", shouldLockdown.toString());
    core.setOutput("visibility", visibility);

    if (shouldLockdown) {
      core.info("Automatic lockdown mode enabled for public repository with GH_AW_GITHUB_TOKEN");
      core.warning("GitHub MCP lockdown mode enabled for public repository. " + "This prevents the GitHub token from accessing private repositories.");
    } else if (!isPrivate && !hasGhAwToken) {
      core.info("Automatic lockdown mode disabled for public repository (GH_AW_GITHUB_TOKEN not configured)");
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
