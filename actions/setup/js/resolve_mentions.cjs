// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * @typedef {Object} MentionResolutionResult
 * @property {string[]} allowedMentions - List of allowed mention usernames
 * @property {number} totalMentions - Total number of mentions found
 * @property {number} resolvedCount - Number of mentions resolved via API
 * @property {boolean} limitExceeded - Whether the 50 mention limit was exceeded
 */

/**
 * Extract all @mentions from text
 * @param {string} text - The text to extract mentions from
 * @returns {string[]} Array of unique usernames mentioned (case-preserved)
 */
function extractMentions(text) {
  if (!text || typeof text !== "string") {
    return [];
  }

  const { getErrorMessage } = require("./error_helpers.cjs");

  const mentionRegex = /(^|[^\w`])@([A-Za-z0-9](?:[A-Za-z0-9_-]{0,37}[A-Za-z0-9])?(?:\/[A-Za-z0-9._-]+)?)/g;
  const mentions = [];
  const seen = new Set();

  let match;
  while ((match = mentionRegex.exec(text)) !== null) {
    const username = match[2];
    const lowercaseUsername = username.toLowerCase();
    if (!seen.has(lowercaseUsername)) {
      seen.add(lowercaseUsername);
      mentions.push(username);
    }
  }

  return mentions;
}

/**
 * Check if a user from the payload is a bot
 * @param {any} user - User object from GitHub payload
 * @returns {boolean} True if the user is a bot
 */
function isPayloadUserBot(user) {
  return !!(user && user.type === "Bot");
}

/**
 * Get recent collaborators (any permission level) - optimistic resolution
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {any} github - GitHub API instance
 * @param {any} core - GitHub Actions core module
 * @returns {Promise<Map<string, boolean>>} Map of username (lowercase) to whether they're allowed (any collaborator, not bot)
 */
async function getRecentCollaborators(owner, repo, github, core) {
  try {
    // Fetch only first page (30 collaborators) for optimistic resolution
    const collaborators = await github.rest.repos.listCollaborators({
      owner: owner,
      repo: repo,
      affiliation: "direct",
      per_page: 30,
    });

    const allowedMap = new Map();
    for (const collaborator of collaborators.data) {
      const lowercaseLogin = collaborator.login.toLowerCase();
      // Allow any collaborator (regardless of permission level) except bots
      const isAllowed = collaborator.type !== "Bot";
      allowedMap.set(lowercaseLogin, isAllowed);
    }

    return allowedMap;
  } catch (error) {
    core.warning(`Failed to fetch recent collaborators: ${getErrorMessage(error)}`);
    return new Map();
  }
}

/**
 * Check individual user's permission lazily
 * @param {string} username - Username to check
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {any} github - GitHub API instance
 * @param {any} core - GitHub Actions core module
 * @returns {Promise<boolean>} True if user is allowed (any collaborator, not bot)
 */
async function checkUserPermission(username, owner, repo, github, core) {
  try {
    // First check if user exists and is not a bot
    const { data: user } = await github.rest.users.getByUsername({
      username: username,
    });

    if (user.type === "Bot") {
      return false;
    }

    // Check if user is a collaborator (any permission level)
    const { data: permissionData } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner: owner,
      repo: repo,
      username: username,
    });

    // Allow any permission level (read, triage, write, maintain, admin)
    return permissionData.permission !== "none";
  } catch (error) {
    // User doesn't exist, not a collaborator, or API error - deny
    return false;
  }
}

/**
 * Resolve mentions lazily with optimistic caching
 * @param {string} text - The text containing mentions
 * @param {string[]} knownAuthors - Known authors that should be allowed (e.g., issue author, comment author)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {any} github - GitHub API instance
 * @param {any} core - GitHub Actions core module
 * @returns {Promise<MentionResolutionResult>} Resolution result with allowed mentions
 */
async function resolveMentionsLazily(text, knownAuthors, owner, repo, github, core) {
  // Extract all mentions from text
  const mentions = extractMentions(text);
  const totalMentions = mentions.length;

  core.info(`Found ${totalMentions} unique mentions in text`);

  // Limit to 50 mentions - filter out excess without API lookup
  const limitExceeded = totalMentions > 50;
  const mentionsToProcess = limitExceeded ? mentions.slice(0, 50) : mentions;

  if (limitExceeded) {
    core.warning(`Mention limit exceeded: ${totalMentions} mentions found, processing only first 50`);
  }

  // Build set of known allowed authors (case-insensitive)
  const knownAuthorsLowercase = new Set(knownAuthors.filter(a => a).map(a => a.toLowerCase()));

  // Optimistically fetch recent collaborators (first page only)
  const collaboratorCache = await getRecentCollaborators(owner, repo, github, core);
  core.info(`Cached ${collaboratorCache.size} recent collaborators for optimistic resolution`);

  const allowedMentions = [];
  let resolvedCount = 0;

  // Process each mention
  for (const mention of mentionsToProcess) {
    const lowerMention = mention.toLowerCase();

    // Check if it's a known author (already verified as non-bot in caller)
    if (knownAuthorsLowercase.has(lowerMention)) {
      allowedMentions.push(mention);
      continue;
    }

    // Check optimistic cache
    if (collaboratorCache.has(lowerMention)) {
      if (collaboratorCache.get(lowerMention)) {
        allowedMentions.push(mention);
      }
      continue;
    }

    // Not in cache - lazy lookup individual user
    resolvedCount++;
    const isAllowed = await checkUserPermission(mention, owner, repo, github, core);
    if (isAllowed) {
      allowedMentions.push(mention);
    }
  }

  core.info(`Resolved ${resolvedCount} mentions via individual API calls`);
  core.info(`Total allowed mentions: ${allowedMentions.length}`);

  return {
    allowedMentions,
    totalMentions,
    resolvedCount,
    limitExceeded,
  };
}

module.exports = {
  extractMentions,
  isPayloadUserBot,
  getRecentCollaborators,
  checkUserPermission,
  resolveMentionsLazily,
};
