// @ts-check

/**
 * Validates that lockdown mode requirements are met at runtime.
 * 
 * When lockdown mode is explicitly enabled in the workflow configuration,
 * GH_AW_GITHUB_TOKEN MUST be configured as a repository secret. Without it,
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
  
  // Check if GH_AW_GITHUB_TOKEN is configured
  const hasGhAwToken = !!process.env.GH_AW_GITHUB_TOKEN;

  if (!hasGhAwToken) {
    const errorMessage =
      "Lockdown mode is enabled (lockdown: true) but GH_AW_GITHUB_TOKEN is not configured.\\n" +
      "\\n" +
      "Please configure GH_AW_GITHUB_TOKEN as a repository secret with appropriate permissions.\\n" +
      "See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/auth.mdx#gh_aw_github_token\\n" +
      "\\n" +
      "To set the token:\\n" +
      '  gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_FINE_GRAINED_PAT"';

    core.setFailed(errorMessage);
    throw new Error(errorMessage);
  }
  
  core.info("✓ Lockdown mode requirements validated: GH_AW_GITHUB_TOKEN is configured");
}

module.exports = validateLockdownRequirements;
