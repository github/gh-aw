// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Helper module for resolving allowed mentions from GitHub event payloads
 */

const { resolveMentionsLazily, isPayloadUserBot } = require("./resolve_mentions.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Push a non-bot user's login to the array if present.
 * @param {string[]} users - Target array
 * @param {{ login?: string, type?: string } | null | undefined} user - User object from payload
 */
function pushNonBotUser(users, user) {
  if (user?.login && !isPayloadUserBot(user)) {
    users.push(user.login);
  }
}

/**
 * Push non-bot assignee logins to the array.
 * @param {string[]} users - Target array
 * @param {Array<{ login?: string, type?: string }> | null | undefined} assignees - Assignees from payload
 */
function pushNonBotAssignees(users, assignees) {
  if (Array.isArray(assignees)) {
    for (const assignee of assignees) {
      pushNonBotUser(users, assignee);
    }
  }
}

/**
 * Extract known authors from a GitHub event payload based on event type.
 * @param {any} context - GitHub Actions context
 * @returns {string[]} Array of known author logins from the payload
 */
function extractKnownAuthorsFromPayload(context) {
  if (!context || typeof context !== "object") {
    return [];
  }

  const users = /** @type {string[]} */ [];
  const { eventName, payload = {} } = context;

  switch (eventName) {
    case "issues":
      pushNonBotUser(users, payload.issue?.user);
      pushNonBotAssignees(users, payload.issue?.assignees);
      break;

    case "pull_request":
    case "pull_request_target":
      pushNonBotUser(users, payload.pull_request?.user);
      pushNonBotAssignees(users, payload.pull_request?.assignees);
      break;

    case "issue_comment":
      pushNonBotUser(users, payload.comment?.user);
      pushNonBotUser(users, payload.issue?.user);
      pushNonBotAssignees(users, payload.issue?.assignees);
      break;

    case "pull_request_review_comment":
      pushNonBotUser(users, payload.comment?.user);
      pushNonBotUser(users, payload.pull_request?.user);
      pushNonBotAssignees(users, payload.pull_request?.assignees);
      break;

    case "pull_request_review":
      pushNonBotUser(users, payload.review?.user);
      pushNonBotUser(users, payload.pull_request?.user);
      pushNonBotAssignees(users, payload.pull_request?.assignees);
      break;

    case "discussion":
      pushNonBotUser(users, payload.discussion?.user);
      break;

    case "discussion_comment":
      pushNonBotUser(users, payload.comment?.user);
      pushNonBotUser(users, payload.discussion?.user);
      break;

    case "release":
      pushNonBotUser(users, payload.release?.author);
      break;

    case "workflow_dispatch":
      if (typeof context.actor === "string" && context.actor.length > 0) {
        users.push(context.actor);
      }
      break;

    default:
      break;
  }

  return users;
}

/**
 * Fetch members of a GitHub team and return their logins.
 * Accepts "team-slug" (resolved against the current org) or "org/team-slug" format.
 * Failures are non-fatal: a warning is logged and an empty array is returned.
 * @param {string} teamEntry - Team identifier, e.g. "my-team" or "myorg/my-team"
 * @param {string} defaultOrg - The org to use when no org is specified in teamEntry
 * @param {any} github - GitHub API client
 * @param {any} core - GitHub Actions core
 * @returns {Promise<string[]>} Array of member logins (non-bot), empty on any failure
 */
async function fetchTeamMembers(teamEntry, defaultOrg, github, core) {
  let org = defaultOrg;
  let teamSlug = teamEntry;

  // Support "org/team-slug" format
  const slashIdx = teamEntry.indexOf("/");
  if (slashIdx !== -1) {
    org = teamEntry.slice(0, slashIdx);
    teamSlug = teamEntry.slice(slashIdx + 1);
  }

  if (!org || !teamSlug) {
    core.warning(`[MENTIONS] Skipping invalid team entry: "${teamEntry}"`);
    return [];
  }

  try {
    const logins = /** @type {string[]} */ [];
    let page = 1;
    const maxPages = 10; // cap at 1000 members to avoid excessive API calls

    while (page <= maxPages) {
      const response = await github.rest.teams.listMembersInOrg({
        org,
        team_slug: teamSlug,
        per_page: 100,
        page,
      });
      const pageLogins = response.data.filter(member => member.type !== "Bot" && typeof member.login === "string").map(member => member.login);
      logins.push(...pageLogins);
      if (response.data.length < 100) {
        break; // no more pages
      }
      page++;
    }

    core.info(`[MENTIONS] Fetched ${logins.length} member(s) from team ${org}/${teamSlug}`);
    return logins;
  } catch (error) {
    const status = typeof error === "object" && error !== null && "status" in error ? error.status : undefined;
    const isRateLimit = status === 429 || (status === 403 && /rate.?limit/i.test(getErrorMessage(error)));
    const isPermission = !isRateLimit && (status === 403 || status === 404);

    if (isRateLimit) {
      core.warning(`[MENTIONS] Rate limit reached while fetching team ${org}/${teamSlug} members - skipping team (retry later or reduce team count)`);
    } else if (isPermission) {
      core.warning(`[MENTIONS] Cannot access team ${org}/${teamSlug} (HTTP ${status}) - ensure the token has 'read:org' scope and the team exists`);
    } else {
      core.warning(`[MENTIONS] Failed to fetch members for team ${org}/${teamSlug}: ${getErrorMessage(error)}`);
    }
    return [];
  }
}

/**
 * Resolve allowed mentions from the current GitHub event context
 * @param {any} context - GitHub Actions context
 * @param {any} github - GitHub API client
 * @param {any} core - GitHub Actions core
 * @param {any} [mentionsConfig] - Mentions configuration from safe-outputs
 * @param {string[]} [extraKnownAuthors] - Additional known authors to allow (e.g. pre-fetched target issue authors)
 * @returns {Promise<string[]>} Array of allowed mention usernames
 */
async function resolveAllowedMentionsFromPayload(context, github, core, mentionsConfig, extraKnownAuthors) {
  // Return empty array if context is not available (e.g., in tests)
  if (!context || !github || !core) {
    return [];
  }

  // If mentions is explicitly set to false, return empty array (all mentions escaped)
  if (mentionsConfig && mentionsConfig.enabled === false) {
    core.info("[MENTIONS] Mentions explicitly disabled - all mentions will be escaped");
    return [];
  }

  // Get configuration options (with defaults)
  const allowCollaboratorMentions = (mentionsConfig?.allowedCollaborators ?? mentionsConfig?.allowTeamMembers) !== false; // default: true
  const allowContext = mentionsConfig?.allowContext !== false; // default: true
  const allowedList = mentionsConfig?.allowed || [];
  const allowedTeams = mentionsConfig?.allowedTeams || [];

  try {
    const { owner, repo } = context.repo;
    const knownAuthors = allowContext ? extractKnownAuthorsFromPayload(context) : [];

    // Add allowed list (always included regardless of configuration)
    if (Array.isArray(allowedList)) {
      knownAuthors.push(...allowedList.filter(alias => typeof alias === "string" && alias.length > 0));
    }

    // Add members from allowed-teams (always included regardless of collaborator mention setting)
    if (Array.isArray(allowedTeams) && allowedTeams.length > 0) {
      core.info(`[MENTIONS] Fetching members for ${allowedTeams.length} configured team(s)`);
      for (const teamEntry of allowedTeams) {
        if (typeof teamEntry === "string" && teamEntry.length > 0) {
          const teamMembers = await fetchTeamMembers(teamEntry, owner, github, core);
          knownAuthors.push(...teamMembers);
        }
      }
    }

    // Add extra known authors (e.g. pre-fetched target issue authors for explicit item_number)
    if (extraKnownAuthors && extraKnownAuthors.length > 0) {
      core.info(`[MENTIONS] Adding ${extraKnownAuthors.length} extra known author(s): ${extraKnownAuthors.join(", ")}`);
      knownAuthors.push(...extraKnownAuthors.filter(alias => typeof alias === "string" && alias.length > 0));
    }

    // Deduplicate while preserving order and original case.
    const deduplicatedKnownAuthors = [];
    const seenKnownAuthors = new Set();
    for (const author of knownAuthors) {
      const key = author.toLowerCase();
      if (seenKnownAuthors.has(key)) {
        continue;
      }
      seenKnownAuthors.add(key);
      deduplicatedKnownAuthors.push(author);
    }

    // If collaborator mentions are disabled, only use known authors (context + allowed list)
    if (!allowCollaboratorMentions) {
      core.info(`[MENTIONS] Collaborator mentions disabled - only allowing context (${deduplicatedKnownAuthors.length} users)`);
      return deduplicatedKnownAuthors;
    }

    // Build allowed mentions list from known authors and collaborators
    // We pass the known authors as fake mentions in text so they get processed
    const fakeText = deduplicatedKnownAuthors.map(author => `@${author}`).join(" ");
    const mentionResult = await resolveMentionsLazily(fakeText, deduplicatedKnownAuthors, owner, repo, github, core);
    const allowedMentions = mentionResult.allowedMentions;

    if (allowedMentions.length > 0) {
      core.info(`[OUTPUT COLLECTOR] Allowed mentions: ${allowedMentions.join(", ")}`);
    } else {
      core.info("[OUTPUT COLLECTOR] No allowed mentions - all mentions will be escaped");
    }

    return allowedMentions;
  } catch (error) {
    core.warning(`Failed to resolve mentions for output collector: ${getErrorMessage(error)}`);
    return [];
  }
}

module.exports = {
  resolveAllowedMentionsFromPayload,
  extractKnownAuthorsFromPayload,
  fetchTeamMembers,
  pushNonBotUser,
  pushNonBotAssignees,
};
