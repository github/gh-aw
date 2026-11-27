// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");

const STANDARD_ROLES = new Set(["admin", "maintain", "write", "triage", "read"]);

// Base roles a custom organization repository role can be derived from. `admin` is
// deliberately excluded: custom repository roles can never confer admin access.
const CUSTOM_ROLE_BASE_ROLES = new Set(["maintain", "write", "triage", "read"]);

/**
 * Normalize GitHub permission/role aliases to the canonical values used by on.roles.
 * @param {string} role
 * @returns {string}
 */
function normalizeRoleName(role) {
  return role === "maintainer" ? "maintain" : role;
}

/**
 * Shared utility for repository permission validation
 * Used by both check_permissions.cjs and check_membership.cjs
 */

/**
 * Parse required permissions from environment variable
 * @returns {string[]} Array of required permission levels
 */
function parseRequiredPermissions() {
  return process.env.GH_AW_REQUIRED_ROLES?.split(",").filter(p => p.trim()) ?? [];
}

/**
 * Parse allowed bot identifiers from environment variable
 * @returns {string[]} Array of allowed bot identifiers
 */
function parseAllowedBots() {
  return process.env.GH_AW_ALLOWED_BOTS?.split(",").filter(b => b.trim()) ?? [];
}

/**
 * Canonicalize a bot/App identifier by stripping the [bot] suffix.
 * Both "my-app" and "my-app[bot]" normalize to "my-app".
 * @param {string} name - Bot identifier (with or without [bot] suffix)
 * @returns {string} The base slug without [bot] suffix
 */
function canonicalizeBotIdentifier(name) {
  return name.endsWith("[bot]") ? name.slice(0, -5) : name;
}

/**
 * Check if an actor matches any entry in the allowed bots list,
 * treating <slug> and <slug>[bot] as equivalent App identities.
 * @param {string} actor - The runtime actor name
 * @param {string[]} allowedBots - Array of allowed bot identifiers
 * @returns {boolean}
 */
function isAllowedBot(actor, allowedBots) {
  const canonicalActor = canonicalizeBotIdentifier(actor);
  return allowedBots.some(bot => canonicalizeBotIdentifier(bot) === canonicalActor);
}

/**
 * Read the `allow_bot_authored_trigger_comment` flag from an inbound aw_context
 * that was passed as a workflow input (`inputs.aw_context`) or as a
 * `repository_dispatch` client payload (`client_payload.aw_context`).
 *
 * Returns `true` only when the flag is explicitly set to the boolean `true` in a
 * valid JSON aw_context object.  Any parse error or missing field returns `false`.
 *
 * @param {any|undefined} payload - The GitHub event payload (context.payload)
 * @returns {boolean}
 */
function readAllowBotAuthoredTriggerComment(payload) {
  try {
    const raw = payload?.inputs?.aw_context ?? payload?.client_payload?.aw_context;
    if (raw == null) return false;
    const parsed = typeof raw === "string" ? JSON.parse(raw.trim()) : raw;
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return false;
    return parsed.allow_bot_authored_trigger_comment === true;
  } catch (err) {
    // Malformed aw_context is treated as absent — default to safe behaviour (flag is false).
    // Log at debug level so workflow authors can diagnose issues with aw_context format.
    core.debug?.(`readAllowBotAuthoredTriggerComment: failed to parse aw_context: ${getErrorMessage(err)}`);
    return false;
  }
}

/**
 * Detect a potential Dependabot Confused Deputy attack.
 *
 * Attack vectors defended against:
 *   - @dependabot recreate on a PR not authored by dependabot causes
 *     github.actor = 'dependabot[bot]' while pull_request.user.login remains
 *     the original (human) PR author.
 *   - @dependabot show on an issue causes github.actor = 'dependabot[bot]'
 *     while comment.user.login differs from the actor.
 *
 * Reference: https://labs.boostsecurity.io/articles/weaponizing-dependabot-pwn-request-at-its-finest/
 *
 * @param {string} actor - The current github.actor
 * @param {string} eventName - The GitHub event name (e.g. "pull_request", "issue_comment")
 * @param {any|undefined} payload - The GitHub event payload (context.payload)
 * @returns {boolean} true if the event looks like a confused deputy attack
 */
function isConfusedDeputyAttack(actor, eventName, payload) {
  if (!payload) return false;

  // For pull_request and pull_request_target events, only check on the `synchronize`
  // action AND only when the actor is a bot (login ends with "[bot]").
  // The confused deputy attack (@dependabot recreate) triggers a synchronize event
  // with actor=dependabot[bot] but pull_request.user = original human author.
  // Other pull_request actions (labeled, unlabeled, assigned, review_requested, etc.)
  // legitimately have actor != pr_author — the actor is whoever performed the action,
  // not the PR author — so checking those would cause false positives.
  // Restricting to bot actors is necessary because a human team member pushing commits
  // to a PR they did not open is legitimate collaboration, not a confused deputy attack.
  // The permission check further down validates the human's own repository permissions.
  if ((eventName === "pull_request" || eventName === "pull_request_target") && payload.action === "synchronize" && actor.endsWith("[bot]")) {
    const prAuthor = payload.pull_request?.user?.login;
    if (prAuthor !== undefined && prAuthor !== actor) {
      return true;
    }
  }

  // For pull_request_review events, the reviewer must match the actor.
  // The PR author (pull_request.user.login) is irrelevant here — the actor
  // is the person submitting the review, not the PR author.
  if (eventName === "pull_request_review") {
    const reviewAuthor = payload.review?.user?.login;
    if (reviewAuthor !== undefined && reviewAuthor !== actor) {
      return true;
    }
  }

  // For pull_request_review_comment events, the comment author must match the actor.
  // The PR author (pull_request.user.login) is irrelevant here — the actor
  // is the person writing the review comment, not the PR author.
  if (eventName === "pull_request_review_comment") {
    const commentAuthor = payload.comment?.user?.login;
    if (commentAuthor !== undefined && commentAuthor !== actor) {
      return true;
    }
  }

  // For issue_comment events, @dependabot show can trigger a comment from dependabot
  // with actor=dependabot[bot]. Verify the comment itself was authored by the actor.
  //
  // Exception: when the comment was authored by a GitHub App bot (login ends with "[bot]")
  // and the action is "edited", this is the legitimate "bot-posted-menu / user-checks-box"
  // pattern — a workflow posts a checkbox-menu comment and a human maintainer edits it to
  // tick a box. No permission elevation occurs: the human actor is who they appear to be and
  // their permissions are still checked normally against the required roles. The Dependabot
  // confused-deputy attack always goes through "created" (not "edited"), so this exception
  // does not weaken protection against that vector.
  //
  // An explicit frontmatter opt-in (on: allow-bot-authored-trigger-comment: true) compiles to
  // GH_AW_ALLOW_BOT_AUTHORED_TRIGGER_COMMENT=true and broadens the exception to cover bot
  // accounts that don't follow the standard "[bot]" naming convention.
  if (eventName === "issue_comment") {
    const commentAuthor = payload.comment?.user?.login;
    if (commentAuthor !== undefined && commentAuthor !== actor) {
      const allowFromFrontmatter = process.env.GH_AW_ALLOW_BOT_AUTHORED_TRIGGER_COMMENT === "true";
      const isBotAuthoredEdit = payload.action === "edited" && (allowFromFrontmatter || commentAuthor.endsWith("[bot]"));
      if (isBotAuthoredEdit) {
        return false;
      }
      return true;
    }
  }

  return false;
}

/**
 * Check if the actor is a bot and if it's active on the repository.
 * Accepts both <slug> and <slug>[bot] actor forms, since GitHub Apps
 * may appear either way depending on the event context.
 * @param {string} actor - GitHub username to check
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @returns {Promise<{isBot: boolean, isActive: boolean, error?: string}>}
 */
async function checkBotStatus(actor, owner, repo) {
  try {
    // GitHub Apps can appear as either <slug> or <slug>[bot].
    // Treat both forms as a bot identity; always query the API with the [bot] form.
    const actorSlug = canonicalizeBotIdentifier(actor);
    const actorForApi = actor.endsWith("[bot]") ? actor : `${actorSlug}[bot]`;

    core.info(`Checking if bot '${actor}' is active on ${owner}/${repo}`);

    // Try to get the bot's permission level to verify it's installed/active on the repo.
    // GitHub Apps/bots that are installed on a repository show up in the collaborators.
    // Use the [bot]-suffixed form since that is how GitHub App identities are listed.
    try {
      const botPermission = await github.rest.repos.getCollaboratorPermissionLevel({
        owner,
        repo,
        username: actorForApi,
      });

      core.info(`Bot '${actor}' is active with permission level: ${botPermission.data.permission}`);
      return { isBot: true, isActive: true };
    } catch (botError) {
      // If we get a 404, the [bot]-suffixed form may not be listed as a collaborator.
      // Fall back to checking the non-[bot] (slug) form, as some GitHub Apps appear
      // under their plain slug name rather than the [bot]-suffixed form.
      const botErrorStatus = typeof botError === "object" && botError !== null && "status" in botError ? botError.status : undefined;
      if (botErrorStatus === 404) {
        try {
          const slugPermission = await github.rest.repos.getCollaboratorPermissionLevel({
            owner,
            repo,
            username: actorSlug,
          });
          core.info(`Bot '${actor}' is active (via slug form) with permission level: ${slugPermission.data.permission}`);
          return { isBot: true, isActive: true };
        } catch (slugError) {
          const slugErrorStatus = typeof slugError === "object" && slugError !== null && "status" in slugError ? slugError.status : undefined;
          if (slugErrorStatus === 404) {
            core.warning(`Bot '${actor}' is not active/installed on ${owner}/${repo}`);
            return { isBot: true, isActive: false };
          }
          const errorMessage = getErrorMessage(slugError);
          core.warning(`Failed to check bot status: ${errorMessage}`);
          return { isBot: true, isActive: false, error: errorMessage };
        }
      }
      // For other errors, we'll treat as inactive to be safe
      const errorMessage = getErrorMessage(botError);
      core.warning(`Failed to check bot status: ${errorMessage}`);
      return { isBot: true, isActive: false, error: errorMessage };
    }
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.warning(`Error checking bot status: ${errorMessage}`);
    return { isBot: false, isActive: false, error: errorMessage };
  }
}

/**
 * Resolve the effective repository role from a `repos.getCollaboratorPermissionLevel`
 * response body. Pure: no API calls, no logging, no ambient clients.
 *
 * Standard GitHub repository permission levels. Custom org repository roles (e.g.
 * "Security Champions") have a role_name that is not one of these — for those, fall back
 * to the standard `permission` level reported by the same endpoint so the actor is not
 * blocked simply because their custom role name is not literally listed in on.roles.
 * A custom role can never grant admin: GitHub derives custom repository roles from the
 * read/triage/write/maintain base roles only, so `admin` is refused here even if the API
 * unexpectedly reports it for a custom role.
 * @param {{permission?: unknown, role_name?: unknown} | null | undefined} data
 * @returns {{effectiveRole: string, normalizedPermission: string, normalizedRoleName: string, isCustomRole: boolean, resolvedBaseRole: string}}
 */
function resolveRepositoryRole(data) {
  const permission = typeof data?.permission === "string" ? data.permission : "";
  const rawRoleName = data?.role_name;
  const normalizedRoleName = normalizeRoleName(typeof rawRoleName === "string" ? rawRoleName : "");
  const normalizedPermission = normalizeRoleName(permission);
  const effectiveRole = normalizedRoleName || normalizedPermission;
  const isCustomRole = normalizedRoleName !== "" && !STANDARD_ROLES.has(normalizedRoleName);
  const resolvedBaseRole = isCustomRole && CUSTOM_ROLE_BASE_ROLES.has(normalizedPermission) ? normalizedPermission : "";
  return { effectiveRole, normalizedPermission, normalizedRoleName, isCustomRole, resolvedBaseRole };
}

/**
 * Match a resolved role against required roles. For standard roles, use role_name
 * (precise: maintain/triage are not collapsed to write/read). For custom org roles,
 * fall back to the standard `permission` level GitHub already computes for the actor
 * (readable with the repository-scoped GITHUB_TOKEN); fail closed if it is not one of
 * the non-admin base roles.
 * @param {ReturnType<typeof resolveRepositoryRole>} resolved
 * @param {string[]} requiredPermissions
 * @returns {{ permission: string, roleMatchType: "effective-role" | "base-role" } | null}
 */
function matchRepositoryRole(resolved, requiredPermissions) {
  for (const requiredPerm of requiredPermissions) {
    const normalizedRequired = normalizeRoleName(requiredPerm);
    if (normalizedRequired !== "" && normalizedRequired === resolved.effectiveRole) return { permission: normalizedRequired, roleMatchType: "effective-role" };
    if (resolved.resolvedBaseRole !== "" && normalizedRequired === resolved.resolvedBaseRole) return { permission: normalizedRequired, roleMatchType: "base-role" };
  }
  return null;
}

/**
 * Check if user has required repository permissions
 * @param {string} actor - GitHub username to check
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string[]} requiredPermissions - Array of required permission levels
 * @returns {Promise<{authorized: boolean, permission?: string, error?: string}>}
 */
async function checkRepositoryPermission(actor, owner, repo, requiredPermissions) {
  try {
    core.info(`Checking if user '${actor}' has required permissions for ${owner}/${repo}`);
    core.info(`Required permissions: ${requiredPermissions.join(", ")}`);

    const repoPermission = await github.rest.repos.getCollaboratorPermissionLevel({
      owner,
      repo,
      username: actor,
    });

    const resolved = resolveRepositoryRole(repoPermission.data);
    const { effectiveRole, normalizedPermission, normalizedRoleName, isCustomRole, resolvedBaseRole } = resolved;
    const logDetails = normalizedRoleName && normalizedRoleName !== normalizedPermission ? `${normalizedPermission} (role: ${normalizedRoleName})` : normalizedPermission;
    core.info(`Repository permission level: ${logDetails}`);

    const debugRoleName = normalizedRoleName || "<empty>";
    const debugBaseRole = resolvedBaseRole || "<empty>";
    core.debug?.(`Repository permission API fields for '${actor}': permission='${normalizedPermission}', role='${debugRoleName}'`);
    core.debug?.(`Repository permission computed roles for '${actor}': effective='${effectiveRole}', custom_role=${isCustomRole}, base_role='${debugBaseRole}'`);
    if (isCustomRole && normalizedPermission === "admin") {
      core.warning(`Ignoring 'admin' permission reported for custom repository role '${normalizedRoleName}': custom roles cannot grant admin access`);
    }
    if (isCustomRole && resolvedBaseRole === "") {
      core.info(`Repository permission fallback unavailable for custom role '${normalizedRoleName}' because GitHub did not report a standard permission level`);
    }

    const permissionMatch = matchRepositoryRole(resolved, requiredPermissions);

    if (permissionMatch) {
      if (permissionMatch.roleMatchType === "base-role") {
        core.info(`Custom repository role '${normalizedRoleName}' satisfied required role '${permissionMatch.permission}' via base role`);
      }
      core.info(`✅ User has ${effectiveRole} access to repository`);
      return { authorized: true, permission: effectiveRole };
    }

    core.debug?.(`Repository permission did not match required roles: ${requiredPermissions.join(", ")}`);
    core.warning(`User permission '${effectiveRole}' does not meet requirements: ${requiredPermissions.join(", ")}`);
    return { authorized: false, permission: effectiveRole };
  } catch (repoError) {
    const errorMessage = getErrorMessage(repoError);
    core.warning(`Repository permission check failed: ${errorMessage}`);
    return { authorized: false, error: errorMessage };
  }
}

module.exports = {
  parseRequiredPermissions,
  parseAllowedBots,
  canonicalizeBotIdentifier,
  isAllowedBot,
  readAllowBotAuthoredTriggerComment,
  isConfusedDeputyAttack,
  checkRepositoryPermission,
  checkBotStatus,
  matchRepositoryRole,
  resolveRepositoryRole,
};
