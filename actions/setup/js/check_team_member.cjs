// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Check if user has admin or maintainer permissions
 * @returns {Promise<void>}
 */
async function main() {
  const actor = context.actor;
  const { owner, repo } = context.repo;

  // Check if the actor has repository access (admin, maintain permissions)
  try {
    core.info(`Checking if user '${actor}' is admin or maintainer of ${owner}/${repo}`);

    const repoPermission = await github.rest.repos.getCollaboratorPermissionLevel({
      owner,
      repo,
      username: actor,
    });

    const permission = repoPermission.data.permission;
    core.info(`Repository permission level: ${permission}`);

    if (permission === "admin" || permission === "maintain") {
      core.info(`User has ${permission} access to repository`);
      core.setOutput("is_team_member", "true");
      return;
    }
  } catch (repoError) {
    const errorMessage = repoError instanceof Error ? repoError.message : String(repoError);
    core.warning(`Repository permission check failed: ${errorMessage}`);
  }

  // Fail the workflow when team membership check fails (cancellation handled by activation job's if condition)
  core.warning(`Access denied: Only authorized team members can trigger this workflow. User '${actor}' is not authorized.`);
  core.setFailed(`Access denied: User '${actor}' is not authorized for this workflow`);
  core.setOutput("is_team_member", "false");
}

module.exports = { main };
