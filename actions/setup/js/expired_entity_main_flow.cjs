// @ts-check
// <reference types="@actions/github-script" />

const { searchEntitiesWithExpiration } = require("./expired_entity_search_helpers.cjs");
const { buildExpirationSummary, categorizeByExpiration, DEFAULT_GRAPHQL_DELAY_MS, DEFAULT_MAX_UPDATES_PER_RUN, processExpiredEntities } = require("./expired_entity_cleanup_helpers.cjs");

/**
 * Configuration for entity-specific behavior
 * @typedef {Object} EntityFlowConfig
 * @property {string} entityType - Entity type name for logging (e.g., "issues", "pull requests", "discussions")
 * @property {string} graphqlField - GraphQL field name (e.g., "issues", "pullRequests", "discussions")
 * @property {string} resultKey - Key to use in return object (e.g., "issues", "pullRequests", "discussions")
 * @property {string} entityLabel - Capitalized label for display (e.g., "Issue", "Pull Request", "Discussion")
 * @property {string} summaryHeading - Heading for summary (e.g., "Expired Issues Cleanup", "Expired Pull Requests Cleanup")
 * @property {boolean} [enableDedupe] - Enable duplicate ID tracking in search (default: false)
 * @property {boolean} [includeSkippedHeading] - Include skipped section in summary (default: false)
 * @property {(entity: any) => Promise<{status: "closed" | "skipped", record: any}>} processEntity - Function to process each expired entity
 */

/**
 * Execute the standardized expired entity cleanup flow
 *
 * This function orchestrates the complete flow:
 * 1. Search for entities with expiration markers
 * 2. Categorize by expiration status
 * 3. Handle early exits (no entities, none expired)
 * 4. Process expired entities (comment + close)
 * 5. Generate and write summary
 *
 * @param {any} github - GitHub API instance (GraphQL + REST)
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {EntityFlowConfig} config - Entity-specific configuration
 * @returns {Promise<void>}
 */
async function executeExpiredEntityCleanup(github, owner, repo, config) {
  core.info(`Searching for expired ${config.entityType} in ${owner}/${repo}`);

  // Step 1: Search for entities with expiration markers
  const { items: entitiesWithExpiration, stats: searchStats } = await searchEntitiesWithExpiration(github, owner, repo, {
    entityType: config.entityType,
    graphqlField: config.graphqlField,
    resultKey: config.resultKey,
    enableDedupe: config.enableDedupe || false,
  });

  // Early exit: No entities found
  if (entitiesWithExpiration.length === 0) {
    core.info(`No ${config.entityType} with expiration markers found`);

    const summaryContent =
      `## ${config.summaryHeading}\n\n` + `**Scanned**: ${searchStats.totalScanned} ${config.entityType} across ${searchStats.pageCount} page(s)\n\n` + `**Result**: No ${config.entityType} with expiration markers found\n`;

    await core.summary.addRaw(summaryContent).write();
    return;
  }

  core.info(`Found ${entitiesWithExpiration.length} ${config.entityType.slice(0, -1)}(s) with expiration markers`);

  // Step 2: Categorize by expiration status
  const {
    expired: expiredEntities,
    notExpired: notExpiredEntities,
    now,
  } = categorizeByExpiration(entitiesWithExpiration, {
    entityLabel: config.entityLabel,
  });

  // Early exit: None expired
  if (expiredEntities.length === 0) {
    core.info(`No expired ${config.entityType} found`);

    const summaryContent =
      `## ${config.summaryHeading}\n\n` +
      `**Scanned**: ${searchStats.totalScanned} ${config.entityType} across ${searchStats.pageCount} page(s)\n\n` +
      `**With expiration markers**: ${entitiesWithExpiration.length} ${config.entityType.slice(0, -1)}(s)\n\n` +
      `**Expired**: 0 ${config.entityType}\n\n` +
      `**Not yet expired**: ${notExpiredEntities.length} ${config.entityType.slice(0, -1)}(s)\n`;

    await core.summary.addRaw(summaryContent).write();
    return;
  }

  core.info(`Found ${expiredEntities.length} expired ${config.entityType.slice(0, -1)}(s)`);

  // Step 3: Process expired entities with entity-specific handler
  const { closed, skipped, failed } = await processExpiredEntities(expiredEntities, {
    entityLabel: config.entityLabel,
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    delayMs: DEFAULT_GRAPHQL_DELAY_MS,
    processEntity: config.processEntity,
  });

  // Step 4: Build and write summary
  const summaryContent = buildExpirationSummary({
    heading: config.summaryHeading,
    entityLabel: config.entityLabel,
    searchStats,
    withExpirationCount: entitiesWithExpiration.length,
    expired: expiredEntities,
    notExpired: notExpiredEntities,
    closed,
    skipped,
    failed,
    maxPerRun: DEFAULT_MAX_UPDATES_PER_RUN,
    includeSkippedHeading: config.includeSkippedHeading || false,
    now,
  });

  await core.summary.addRaw(summaryContent).write();
  core.info(`Successfully closed ${closed.length} expired ${config.entityType.slice(0, -1)}(s)`);
}

module.exports = {
  executeExpiredEntityCleanup,
};
