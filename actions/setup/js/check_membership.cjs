// @ts-check
/// <reference types="@actions/github-script" />

const { parseRequiredPermissions, parseAllowedBots, checkRepositoryPermission, checkBotStatus, isAllowedBot, isConfusedDeputyAttack } = require("./check_permissions_utils.cjs");
const { writeDenialSummary } = require("./pre_activation_summary.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { withRetry, isTransientError } = require("./error_recovery.cjs");

/**
 * Attempt to authorize the actor via the bots allowlist.
 *
 * Returns `{ handled: true }` when a final authorization decision was reached (either
 * `authorized_bot` or `bot_not_active`) and outputs/summary have already been set.
 * Returns `{ handled: false }` when the actor is not in the allowlist, when the
 * allowlist is empty, or when the bot-status check failed entirely — in all of these
 * cases the caller should fall through to the standard repository roles check.
 *
 * @param {string} actorToValidate
 * @param {string[]} allowedBots
 * @param {string} owner
 * @param {string} repo
 * @returns {Promise<{ handled: boolean }>}
 */
async function checkBotAllowlistAuthorization(actorToValidate, allowedBots, owner, repo) {
  if (allowedBots.length === 0 || !isAllowedBot(actorToValidate, allowedBots)) {
    return { handled: false };
  }

  core.info(`Actor '${actorToValidate}' matched the allowed bots list: ${allowedBots.join(", ")}`);

  // Verify the bot is active/installed on the repository
  const botStatus = await checkBotStatus(actorToValidate, owner, repo);

  if (botStatus.isBot && botStatus.isActive) {
    core.info(`✅ Bot '${actorToValidate}' is active on the repository and authorized`);
    core.setOutput("is_team_member", "true");
    core.setOutput("result", "authorized_bot");
    core.setOutput("user_permission", "bot");
    return { handled: true };
  } else if (botStatus.isBot && !botStatus.isActive) {
    const errorMessage = `Access denied: Bot '${actorToValidate}' is not active/installed on this repository`;
    core.warning(`Bot '${actorToValidate}' is in the allowed list but not active/installed on ${owner}/${repo}`);
    core.setOutput("is_team_member", "false");
    core.setOutput("result", "bot_not_active");
    core.setOutput("user_permission", "bot");
    core.setOutput("error_message", errorMessage);
    await writeDenialSummary(errorMessage, "The bot is in the allowed list but is not installed or active on this repository. Install the GitHub App and try again.");
    return { handled: true };
  } else {
    core.info(`Actor '${actorToValidate}' is in allowed bots list but bot status check failed`);
    return { handled: false };
  }
}

function readWorkflowDispatchAwContext(payload) {
  try {
    const rawAwContext = payload?.inputs?.aw_context;
    if (typeof rawAwContext !== "string" || rawAwContext.trim() === "") {
      return null;
    }
    const parsed = JSON.parse(rawAwContext);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

async function main() {
  const { eventName } = context;
  const actor = context.actor;
  const { owner, repo } = context.repo;
  const requiredPermissions = parseRequiredPermissions();
  const allowedBots = parseAllowedBots();
  let actorToValidate = actor;

  // workflow_dispatch is never treated as a trusted event.
  // For centralized slash-command dispatches, validate the original triggering actor.
  if (eventName === "workflow_dispatch") {
    const awContext = readWorkflowDispatchAwContext(context.payload);
    const commandName = typeof awContext?.command_name === "string" ? awContext.command_name.trim() : "";
    const triggerLabel = typeof awContext?.trigger_label === "string" ? awContext.trigger_label.trim() : "";
    const propagatedActor = typeof awContext?.actor === "string" ? awContext.actor.trim() : "";

    if ((commandName || triggerLabel) && actor === "github-actions[bot]") {
      if (!propagatedActor) {
        const errorMessage = "Access denied: workflow_dispatch aw_context.actor is required for centralized command dispatches.";
        core.warning(errorMessage);
        core.setOutput("is_team_member", "false");
        core.setOutput("result", "config_error");
        core.setOutput("error_message", errorMessage);
        await writeDenialSummary(errorMessage, "Ensure centralized command dispatches include aw_context.actor.");
        return;
      }

      actorToValidate = propagatedActor;
      core.info(`Validating centralized workflow_dispatch against originating actor '${actorToValidate}'`);

      const itemType = typeof awContext?.item_type === "string" ? awContext.item_type.trim() : "";
      const rawItemNumber = typeof awContext?.item_number === "string" ? awContext.item_number.trim() : "";
      if (itemType === "pull_request") {
        if (!/^\d+$/.test(rawItemNumber)) {
          const errorMessage = "Access denied: centralized slash-command dispatch is missing a valid pull request number.";
          core.warning(errorMessage);
          core.setOutput("is_team_member", "false");
          core.setOutput("result", "fork_pull_request");
          core.setOutput("error_message", errorMessage);
          await writeDenialSummary(errorMessage, "Dispatch metadata is incomplete. Re-run from the original PR event.");
          return;
        }
        const pullNumber = Number.parseInt(rawItemNumber, 10);
        if (!Number.isInteger(pullNumber) || pullNumber <= 0) {
          const errorMessage = "Access denied: centralized slash-command dispatch is missing a valid pull request number.";
          core.warning(errorMessage);
          core.setOutput("is_team_member", "false");
          core.setOutput("result", "fork_pull_request");
          core.setOutput("error_message", errorMessage);
          await writeDenialSummary(errorMessage, "Dispatch metadata is incomplete. Re-run from the original PR event.");
          return;
        }

        try {
          const response = await withRetry(
            () =>
              github.rest.pulls.get({
                owner,
                repo,
                pull_number: pullNumber,
              }),
            {
              maxRetries: 3,
              initialDelayMs: 2500,
              maxDelayMs: 30000,
              backoffMultiplier: 2,
              jitterMs: 0,
              shouldRetry: error => {
                const status = error?.response?.status ?? error?.status;
                return (typeof status === "number" && status >= 500) || isTransientError(error);
              },
            },
            "pull request provenance check"
          );
          const pullRequest = response?.data;
          const headRepo = pullRequest?.head?.repo?.full_name;
          const baseRepo = pullRequest?.base?.repo?.full_name;
          if (!headRepo || !baseRepo) {
            const errorMessage = "Access denied: centralized slash-command dispatch pull request repository metadata is unavailable.";
            core.warning(errorMessage);
            core.setOutput("is_team_member", "false");
            core.setOutput("result", "fork_pull_request");
            core.setOutput("error_message", errorMessage);
            await writeDenialSummary(errorMessage, "Check the pre_activation log and ensure pull request repository metadata is present.");
            return;
          }
          if (headRepo !== baseRepo) {
            const errorMessage = "Access denied: centralized slash-command dispatch from fork-based pull requests is not allowed.";
            core.warning(errorMessage);
            core.setOutput("is_team_member", "false");
            core.setOutput("result", "fork_pull_request");
            core.setOutput("error_message", errorMessage);
            await writeDenialSummary(errorMessage, "Run slash-command workflows from branches in the base repository.");
            return;
          }
        } catch (error) {
          const errorMessage = `Repository permission check failed: Unable to verify pull request provenance (${getErrorMessage(error)}).`;
          core.warning(errorMessage);
          core.setOutput("is_team_member", "false");
          core.setOutput("result", "api_error");
          core.setOutput("error_message", errorMessage);
          await writeDenialSummary(errorMessage, "Check the pre_activation log and ensure the workflow token can read pull request metadata.");
          return;
        }
      }
    }
    core.info(`Event ${eventName} requires validation`);
  }

  // skip check for other safe events
  // workflow_run is intentionally excluded due to HIGH security risks:
  // - Privilege escalation (inherits permissions from triggering workflow)
  // - Branch protection bypass (can execute on protected branches)
  // - Secret exposure (secrets available from untrusted code)
  // merge_group is safe because:
  // - Only triggered by GitHub's merge queue system (not user-initiated)
  // - Requires branch protection rules to be enabled
  // - Validates combined state of multiple PRs before merging
  const safeEvents = ["schedule", "merge_group"];
  if (safeEvents.includes(eventName)) {
    core.info(`✅ Event ${eventName} does not require validation`);
    core.setOutput("is_team_member", "true");
    core.setOutput("result", "safe_event");
    return;
  }

  if (requiredPermissions.length === 0) {
    core.warning("❌ Configuration error: Required permissions not specified. Contact repository administrator.");
    core.setOutput("is_team_member", "false");
    core.setOutput("result", "config_error");
    core.setOutput("error_message", "Configuration error: Required permissions not specified");
    await writeDenialSummary("Configuration error: Required permissions not specified.", "Contact the repository administrator to fix the workflow frontmatter configuration.");
    return;
  }

  // Guard against Dependabot Confused Deputy attacks.
  // An attacker can trigger @dependabot recreate (for pull_request events) or
  // @dependabot show (for issue_comment events) to make dependabot appear as the
  // actor, bypassing permission checks that rely solely on github.actor.
  // Reference: https://labs.boostsecurity.io/articles/weaponizing-dependabot-pwn-request-at-its-finest/
  if (isConfusedDeputyAttack(actorToValidate, eventName, context.payload)) {
    const errorMessage = `Access denied: Potential confused deputy attack detected. Actor '${actorToValidate}' does not match the event author. The workflow may have been triggered indirectly via a bot command.`;
    core.warning(errorMessage);
    core.setOutput("is_team_member", "false");
    core.setOutput("result", "confused_deputy");
    core.setOutput("error_message", errorMessage);
    await writeDenialSummary(errorMessage, "This can occur when a bot command (e.g. @dependabot recreate) causes a bot to appear as the actor on a PR or comment that was originally authored by a different user.");
    return;
  }

  // If the actor is in the bots allowlist, skip the roles check entirely and go straight
  // to bot-status verification. A bot listed in on.bots: is an explicit grant; the roles
  // mismatch (bots typically have "none" repo permission) is expected and not actionable.
  // Checking bots first also avoids a spurious "permission does not meet requirements"
  // warning that would otherwise be emitted by the roles check before authorization succeeds.
  const botResult = await checkBotAllowlistAuthorization(actorToValidate, allowedBots, owner, repo);
  if (botResult.handled) {
    return;
  }

  // Check if the actor has the required repository permissions
  const result = await checkRepositoryPermission(actorToValidate, owner, repo, requiredPermissions);

  if (result.authorized) {
    core.setOutput("is_team_member", "true");
    core.setOutput("result", "authorized");
    core.setOutput("user_permission", result.permission);
  } else {
    // Not authorized by role or bot
    if (result.error) {
      const errorMessage = `Repository permission check failed: ${result.error}`;
      core.setOutput("is_team_member", "false");
      core.setOutput("result", "api_error");
      core.setOutput("error_message", errorMessage);
      await writeDenialSummary(errorMessage, "The permission check failed with a GitHub API error. Check the `pre_activation` job log for details.");
    } else {
      const errorMessage =
        `Access denied: User '${actorToValidate}' is not authorized. Required permissions: ${requiredPermissions.join(", ")}. ` +
        `To allow this user to run the workflow, add their role to the frontmatter. Example: roles: [${requiredPermissions.join(", ")}, ${result.permission}]`;
      core.setOutput("is_team_member", "false");
      core.setOutput("result", "insufficient_permissions");
      core.setOutput("user_permission", result.permission);
      core.setOutput("error_message", errorMessage);
      await writeDenialSummary(errorMessage, `To allow a bot or GitHub App actor, add it to \`on.bots:\` in the workflow frontmatter. ` + `To change the required roles for human actors, update \`on.roles:\` in the workflow frontmatter.`);
    }
  }
}

module.exports = { main, checkBotAllowlistAuthorization };
