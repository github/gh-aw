// @ts-check

/**
 * Validates that lockdown mode requirements are met at runtime.
 *
 * When lockdown mode is explicitly enabled in the workflow configuration,
 * at least one custom GitHub token must be configured (GH_AW_GITHUB_TOKEN,
 * GH_AW_GITHUB_MCP_SERVER_TOKEN, or custom github-token). Without any custom token,
 * the workflow will fail with a clear error message.
 *
 * This validation runs at the start of the workflow to fail fast if requirements
 * are not met, providing clear guidance to the user.
 *
 * @param {any} core - GitHub Actions core library
 * @returns {void}
 */
function validateLockdownRequirements(core) {
  // Check if lockdown mode is explicitly enabled (set to "true" in frontmatter)
  const lockdownEnabled = process.env.GITHUB_MCP_LOCKDOWN_EXPLICIT === "true";

  if (!lockdownEnabled) {
    // Lockdown not explicitly enabled, no validation needed
    core.info("Lockdown mode not explicitly enabled, skipping validation");
    return;
  }

  core.info("Lockdown mode is explicitly enabled, validating requirements...");

  // Check if any custom GitHub token is configured
  // This matches the token selection logic used by the MCP gateway:
  // GH_AW_GITHUB_MCP_SERVER_TOKEN || GH_AW_GITHUB_TOKEN || custom github-token
  const hasGhAwToken = !!process.env.GH_AW_GITHUB_TOKEN;
  const hasGhAwMcpToken = !!process.env.GH_AW_GITHUB_MCP_SERVER_TOKEN;
  const hasCustomToken = !!process.env.CUSTOM_GITHUB_TOKEN;
  const hasAnyCustomToken = hasGhAwToken || hasGhAwMcpToken || hasCustomToken;

  core.info(`GH_AW_GITHUB_TOKEN configured: ${hasGhAwToken}`);
  core.info(`GH_AW_GITHUB_MCP_SERVER_TOKEN configured: ${hasGhAwMcpToken}`);
  core.info(`Custom github-token configured: ${hasCustomToken}`);

  if (!hasAnyCustomToken) {
    const errorMessage =
      "Lockdown mode is enabled (lockdown: true) but no custom GitHub token is configured.\\n" +
      "\\n" +
      "Please configure one of the following as a repository secret:\\n" +
      "  - GH_AW_GITHUB_TOKEN (recommended)\\n" +
      "  - GH_AW_GITHUB_MCP_SERVER_TOKEN (alternative)\\n" +
      "  - Custom github-token in your workflow frontmatter\\n" +
      "\\n" +
      "See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/auth.mdx\\n" +
      "\\n" +
      "To set a token:\\n" +
      '  gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_FINE_GRAINED_PAT"';

    core.setFailed(errorMessage);
    throw new Error(errorMessage);
  }

  core.info("✓ Lockdown mode requirements validated: Custom GitHub token is configured");
}

module.exports = validateLockdownRequirements;
