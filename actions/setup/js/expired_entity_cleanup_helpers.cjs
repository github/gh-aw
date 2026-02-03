// @ts-check
// <reference types="@actions/github-script" />

const { extractExpirationDate } = require("./ephemerals.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_MAX_UPDATES_PER_RUN = 100;
const DEFAULT_GRAPHQL_DELAY_MS = 500;

/**
 * Delay execution for a specified number of milliseconds
 * @param {number} ms - Milliseconds to delay
 * @returns {Promise<void>}
 */
function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

/**
 * Validate entity creation date
 * @param {string} createdAt - ISO 8601 creation date
 * @returns {boolean} True if valid
 */
function validateCreationDate(createdAt) {
  const creationDate = new Date(createdAt);
  return !isNaN(creationDate.getTime());
}

/**
 * Categorize entities by expiration state and log status
 * @param {Array<{number: number, title: string, url: string, body: string, createdAt: string}>} entities
 * @param {{entityLabel: string}} options
 * @returns {{expired: Array<any>, notExpired: Array<any>, now: Date}}
 */
function categorizeByExpiration(entities, { entityLabel }) {
  const now = new Date();
  core.info(`Current date/time: ${now.toISOString()}`);

  const expired = [];
  const notExpired = [];

  for (const entity of entities) {
    core.info(`Processing ${entityLabel} #${entity.number}: ${entity.title}`);

    if (!validateCreationDate(entity.createdAt)) {
      core.warning(`  ${entityLabel} #${entity.number} has invalid creation date: ${entity.createdAt}, skipping`);
      continue;
    }
    core.info(`  Creation date: ${entity.createdAt}`);

    const expirationDate = extractExpirationDate(entity.body);
    if (!expirationDate) {
      core.warning(`  ${entityLabel} #${entity.number} has invalid expiration date format, skipping`);
      continue;
    }
    core.info(`  Expiration date: ${expirationDate.toISOString()}`);

    const isExpired = now >= expirationDate;
    const timeDiff = expirationDate.getTime() - now.getTime();
    const daysUntilExpiration = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
    const hoursUntilExpiration = Math.floor(timeDiff / (1000 * 60 * 60));

    if (isExpired) {
      const daysSinceExpiration = Math.abs(daysUntilExpiration);
      const hoursSinceExpiration = Math.abs(hoursUntilExpiration);
      core.info(`  ✓ ${entityLabel} #${entity.number} is EXPIRED (expired ${daysSinceExpiration} days, ${hoursSinceExpiration % 24} hours ago)`);
      expired.push({
        ...entity,
        expirationDate,
      });
    } else {
      core.info(`  ✗ ${entityLabel} #${entity.number} is NOT expired (expires in ${daysUntilExpiration} days, ${hoursUntilExpiration % 24} hours)`);
      notExpired.push({
        ...entity,
        expirationDate,
      });
    }
  }

  core.info(`Expiration check complete: ${expired.length} expired, ${notExpired.length} not yet expired`);
  return { expired, notExpired, now };
}

/**
 * Process expired entities with per-entity handler and rate limiting
 * @param {Array<any>} expiredEntities
 * @param {{
 *   entityLabel: string,
 *   maxPerRun?: number,
 *   delayMs?: number,
 *   processEntity: (entity: any) => Promise<{status: "closed" | "skipped", record: any}>
 * }} options
 * @returns {Promise<{closed: Array<any>, skipped: Array<any>, failed: Array<any>}>}
 */
async function processExpiredEntities(expiredEntities, { entityLabel, maxPerRun = DEFAULT_MAX_UPDATES_PER_RUN, delayMs = DEFAULT_GRAPHQL_DELAY_MS, processEntity }) {
  const entitiesToProcess = expiredEntities.slice(0, maxPerRun);

  if (expiredEntities.length > maxPerRun) {
    core.warning(`Found ${expiredEntities.length} expired ${entityLabel.toLowerCase()}s, but only closing the first ${maxPerRun}`);
    core.info(`Remaining ${expiredEntities.length - maxPerRun} expired ${entityLabel.toLowerCase()}s will be closed in the next run`);
  }

  core.info(`Preparing to close ${entitiesToProcess.length} ${entityLabel.toLowerCase()}(s)`);

  const closed = [];
  const failed = [];
  const skipped = [];

  for (let i = 0; i < entitiesToProcess.length; i++) {
    const entity = entitiesToProcess[i];
    core.info(`[${i + 1}/${entitiesToProcess.length}] Processing ${entityLabel.toLowerCase()} #${entity.number}: ${entity.url}`);

    try {
      const result = await processEntity(entity);

      if (result.status === "skipped") {
        skipped.push(result.record);
      } else {
        closed.push(result.record);
      }

      core.info(`✓ Successfully processed ${entityLabel.toLowerCase()} #${entity.number}: ${entity.url}`);
    } catch (error) {
      core.error(`✗ Failed to close ${entityLabel.toLowerCase()} #${entity.number}: ${getErrorMessage(error)}`);
      core.error(`  Error details: ${JSON.stringify(error, null, 2)}`);
      failed.push({
        number: entity.number,
        url: entity.url,
        title: entity.title,
        error: getErrorMessage(error),
      });
    }

    if (i < entitiesToProcess.length - 1) {
      core.info(`  Waiting ${delayMs}ms before next operation...`);
      await delay(delayMs);
    }
  }

  return { closed, skipped, failed };
}

/**
 * Build not-yet-expired list section
 * @param {Array<any>} notExpiredEntities
 * @param {Date} now
 * @param {string} entityLabel
 * @returns {string}
 */
function buildNotExpiredSection(notExpiredEntities, now, entityLabel) {
  if (notExpiredEntities.length === 0) {
    return "";
  }

  let section = `### Not Yet Expired\n\n`;

  const list = notExpiredEntities.length > 10 ? notExpiredEntities.slice(0, 10) : notExpiredEntities;
  if (notExpiredEntities.length > 10) {
    section += `${notExpiredEntities.length} ${entityLabel.toLowerCase()}(s) not yet expired (showing first 10):\n\n`;
  }

  for (const entity of list) {
    const timeDiff = entity.expirationDate.getTime() - now.getTime();
    const days = Math.floor(timeDiff / (1000 * 60 * 60 * 24));
    const hours = Math.floor(timeDiff / (1000 * 60 * 60)) % 24;
    section += `- ${entityLabel} #${entity.number}: [${entity.title}](${entity.url}) - Expires in ${days}d ${hours}h\n`;
  }

  return section;
}

/**
 * Build standardized cleanup summary content
 * @param {{
 *   heading: string,
 *   entityLabel: string,
 *   searchStats: {totalScanned: number, pageCount: number},
 *   withExpirationCount: number,
 *   expired: Array<any>,
 *   notExpired: Array<any>,
 *   closed: Array<any>,
 *   failed: Array<any>,
 *   skipped?: Array<any>,
 *   maxPerRun: number,
 *   includeSkippedHeading?: boolean,
 *   now?: Date
 * }} params
 * @returns {string}
 */
function buildExpirationSummary(params) {
  const { heading, entityLabel, searchStats, withExpirationCount, expired, notExpired, closed, failed, skipped = [], maxPerRun, includeSkippedHeading = false, now = new Date() } = params;

  let summaryContent = `## ${heading}\n\n`;
  summaryContent += `**Scan Summary**\n`;
  summaryContent += `- Scanned: ${searchStats.totalScanned} ${entityLabel.toLowerCase()}s across ${searchStats.pageCount} page(s)\n`;
  summaryContent += `- With expiration markers: ${withExpirationCount} ${entityLabel.toLowerCase()}(s)\n`;
  summaryContent += `- Expired: ${expired.length} ${entityLabel.toLowerCase()}(s)\n`;
  summaryContent += `- Not yet expired: ${notExpired.length} ${entityLabel.toLowerCase()}(s)\n\n`;

  summaryContent += `**Closing Summary**\n`;
  summaryContent += `- Successfully closed: ${closed.length} ${entityLabel.toLowerCase()}(s)\n`;
  if (includeSkippedHeading && skipped.length > 0) {
    summaryContent += `- Skipped (already had comment): ${skipped.length} ${entityLabel.toLowerCase()}(s)\n`;
  }
  if (failed.length > 0) {
    summaryContent += `- Failed to close: ${failed.length} ${entityLabel.toLowerCase()}(s)\n`;
  }
  if (expired.length > maxPerRun) {
    summaryContent += `- Remaining for next run: ${expired.length - maxPerRun} ${entityLabel.toLowerCase()}(s)\n`;
  }
  summaryContent += `\n`;

  if (closed.length > 0) {
    summaryContent += `### Successfully Closed ${entityLabel}s\n\n`;
    for (const entity of closed) {
      summaryContent += `- ${entityLabel} #${entity.number}: [${entity.title}](${entity.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (includeSkippedHeading && skipped.length > 0) {
    summaryContent += `### Skipped (Already Had Comment)\n\n`;
    for (const entity of skipped) {
      summaryContent += `- ${entityLabel} #${entity.number}: [${entity.title}](${entity.url})\n`;
    }
    summaryContent += `\n`;
  }

  if (failed.length > 0) {
    summaryContent += `### Failed to Close\n\n`;
    for (const entity of failed) {
      summaryContent += `- ${entityLabel} #${entity.number}: [${entity.title}](${entity.url}) - Error: ${entity.error}\n`;
    }
    summaryContent += `\n`;
  }

  summaryContent += buildNotExpiredSection(notExpired, now, entityLabel);

  return summaryContent;
}

module.exports = {
  buildExpirationSummary,
  categorizeByExpiration,
  DEFAULT_GRAPHQL_DELAY_MS,
  DEFAULT_MAX_UPDATES_PER_RUN,
  delay,
  processExpiredEntities,
  validateCreationDate,
};
