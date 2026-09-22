// @ts-check
// @safe-outputs-exempt SEC-004
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode, checkRequiredFilter, resolveTarget } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { loadTemporaryIdMapFromResolved, resolveRepoIssueTarget } = require("./temporary_id.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "assign_milestone";

/**
 * Formats milestones as a human-readable list of titles (e.g., '"v1.0", "v2.0"').
 * @param {Array<{title: string}>|null} milestones
 * @returns {string}
 */
function formatAvailableMilestones(milestones) {
  if (!milestones || milestones.length === 0) return "none";
  return milestones.map(m => `"${m.title}"`).join(", ");
}

/**
 * Formats milestones as a human-readable list with numbers (e.g., '"v1.0" (#5), "v2.0" (#6)').
 * @param {Array<{title: string, number: number}>|null} milestones
 * @returns {string}
 */
function formatAvailableMilestonesWithNumbers(milestones) {
  if (!milestones || milestones.length === 0) return "none";
  return milestones.map(m => `"${m.title}" (#${m.number})`).join(", ");
}

/**
 * Main handler factory for assign_milestone
 * Returns a message handler function that processes individual assign_milestone messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedMilestones = config.allowed || [];
  const maxCount = config.max || 10;
  const targetConfig = config.target || "triggering";
  const autoCreate = config.auto_create === true;
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  if (requiredLabels.length > 0) core.info(`Required labels (all): ${requiredLabels.join(", ")}`);
  if (requiredTitlePrefix) core.info(`Required title prefix: ${requiredTitlePrefix}`);

  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  if (defaultTargetRepo) core.info(`Target repository: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) core.info(`Allowed repositories: ${Array.from(allowedRepos).join(", ")}`);

  core.info(`Assign milestone configuration: max=${maxCount}, auto_create=${autoCreate}`);
  if (allowedMilestones.length > 0) {
    core.info(`Allowed milestones: ${allowedMilestones.join(", ")}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;

  // Cached results from paginated title searches, scoped by "owner/repo" to prevent
  // cross-repo cache pollution in multi-repo configurations.
  /** @type {Map<string, Object>} key: "owner/repo::title" */
  const milestoneByTitle = new Map();
  /** @type {Set<string>} entries: "owner/repo" — repos whose milestones have been fully fetched */
  const exhaustedRepos = new Set();
  /** @type {Map<string, Array<Object>>} key: "owner/repo" → milestones fetched for that repo */
  const fetchedMilestonesByRepo = new Map();

  /**
   * Find a milestone by title using lazy paginated search with early exit.
   * Results are cached per-repo so repeated lookups don't re-paginate, and
   * cache entries from one repo never affect lookups for another repo.
   * @param {string} title
   * @param {string} owner
   * @param {string} repo
   * @returns {Promise<Object|null>}
   */
  async function findMilestoneByTitle(title, owner, repo) {
    const repoKey = `${owner}/${repo}`;
    const cacheKey = `${repoKey}::${title}`;
    if (milestoneByTitle.has(cacheKey)) {
      return milestoneByTitle.get(cacheKey);
    }
    if (exhaustedRepos.has(repoKey)) {
      return null;
    }
    const repoMilestones = fetchedMilestonesByRepo.get(repoKey) || [];
    let found = false;
    await githubClient.paginate(githubClient.rest.issues.listMilestones, { owner, repo, state: "all", per_page: 100 }, (response, done) => {
      for (const m of response.data) {
        const key = `${repoKey}::${m.title}`;
        if (!milestoneByTitle.has(key)) {
          milestoneByTitle.set(key, m);
          repoMilestones.push(m);
        }
        if (m.title === title) {
          found = true;
          done();
          return;
        }
      }
    });
    fetchedMilestonesByRepo.set(repoKey, repoMilestones);
    if (!found) {
      exhaustedRepos.add(repoKey);
    }
    core.info(`Searched ${repoMilestones.length} milestones for ${repoKey} (exhausted=${exhaustedRepos.has(repoKey)})`);
    return milestoneByTitle.get(cacheKey) || null;
  }

  /**
   * Message handler function that processes a single assign_milestone message
   * @param {Object} message - The assign_milestone message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleAssignMilestone(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping assign_milestone: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Convert resolvedTemporaryIds to a normalized Map for resolveRepoIssueTarget
    const temporaryIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);

    // Resolve target repo from item or default target-repo config
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "milestone assignment");
    if (!repoResult.success) {
      core.warning(`assign_milestone: ${repoResult.error}`);
      return { success: false, error: repoResult.error };
    }
    const milestoneOwner = repoResult.repoParts.owner;
    const milestoneRepo = repoResult.repoParts.repo;

    let targetItem = item;
    let resolvedIssueTarget;
    if (targetConfig === "*" && item.issue_number != null) {
      resolvedIssueTarget = resolveRepoIssueTarget(item.issue_number, temporaryIdMap, milestoneOwner, milestoneRepo);
      if (resolvedIssueTarget.wasTemporaryId && !resolvedIssueTarget.resolved) {
        core.info(`Deferring assign_milestone: unresolved temporary ID (${item.issue_number})`);
        return {
          success: false,
          deferred: true,
          error: resolvedIssueTarget.errorMessage || `Unresolved temporary ID: ${item.issue_number}`,
        };
      }
      if (resolvedIssueTarget.errorMessage || !resolvedIssueTarget.resolved) {
        core.error(`Invalid issue_number: ${item.issue_number}`);
        return {
          success: false,
          error: `Invalid issue_number: ${item.issue_number}`,
        };
      }
      targetItem = { ...item, issue_number: resolvedIssueTarget.resolved.number };
    }

    const numberResult = resolveTarget({
      targetConfig,
      item: targetItem,
      context,
      itemType: HANDLER_TYPE,
      supportsIssue: true,
    });
    if (!numberResult.success) {
      core.warning(numberResult.error);
      return { success: false, error: numberResult.error };
    }
    const issueNumber = numberResult.number;

    const repoParts = { owner: milestoneOwner, repo: milestoneRepo };
    const filterResult = await checkRequiredFilter(githubClient, repoParts, issueNumber, requiredLabels, requiredTitlePrefix, "assign_milestone");
    if (filterResult) return filterResult;

    if (resolvedIssueTarget?.wasTemporaryId) {
      core.info(`Resolved temporary ID '${item.issue_number}' to issue #${issueNumber}`);
    }

    let milestoneNumber = Number(item.milestone_number);
    const milestoneTitle = item.milestone_title || null;
    const hasMilestoneNumber = !Number.isNaN(milestoneNumber) && milestoneNumber > 0;

    // Validate that at least one of milestone_number or milestone_title is provided
    if (!hasMilestoneNumber && !milestoneTitle) {
      const msg = "Either milestone_number or milestone_title must be provided";
      core.error(msg);
      return {
        success: false,
        error: msg,
      };
    }

    // Resolve milestone by title if milestone_number is not valid
    if (!hasMilestoneNumber && milestoneTitle !== null) {
      try {
        const match = await findMilestoneByTitle(milestoneTitle, milestoneOwner, milestoneRepo);
        if (match) {
          milestoneNumber = match.number;
          core.info(`Resolved milestone title "${milestoneTitle}" to #${milestoneNumber}`);
        } else if (autoCreate) {
          // Create the milestone automatically
          const created = await githubClient.rest.issues.createMilestone({
            owner: milestoneOwner,
            repo: milestoneRepo,
            title: milestoneTitle,
          });
          milestoneNumber = created.data.number;
          const repoKey = `${milestoneOwner}/${milestoneRepo}`;
          milestoneByTitle.set(`${repoKey}::${created.data.title}`, created.data);
          const repoMilestones = fetchedMilestonesByRepo.get(repoKey) || [];
          repoMilestones.push(created.data);
          fetchedMilestonesByRepo.set(repoKey, repoMilestones);
          core.info(`Auto-created milestone "${milestoneTitle}" as #${milestoneNumber}`);
        } else {
          const available = formatAvailableMilestones(fetchedMilestonesByRepo.get(`${milestoneOwner}/${milestoneRepo}`) || []);
          core.warning(`Milestone "${milestoneTitle}" not found in repository. Available: ${available}. Set auto_create: true to create it automatically.`);
          return {
            success: false,
            error: `Milestone "${milestoneTitle}" not found in repository. Set auto_create: true to create it automatically.`,
          };
        }
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to resolve milestone "${milestoneTitle}": ${errorMessage}`);
        return {
          success: false,
          error: `Failed to resolve milestone "${milestoneTitle}": ${errorMessage}`,
        };
      }
    }

    // Validate against allowed list if configured
    if (allowedMilestones.length > 0) {
      try {
        const { data: milestone } = await githubClient.rest.issues.getMilestone({
          owner: milestoneOwner,
          repo: milestoneRepo,
          milestone_number: milestoneNumber,
        });

        const isAllowed = allowedMilestones.includes(milestone.title) || allowedMilestones.includes(String(milestoneNumber));

        if (!isAllowed) {
          core.warning(`Milestone "${milestone.title}" (#${milestoneNumber}) is not in the allowed list`);
          return {
            success: false,
            error: `Milestone "${milestone.title}" (#${milestoneNumber}) is not in the allowed list`,
          };
        }
      } catch (error) {
        const errorMessage = getErrorMessage(error);
        core.error(`Failed to validate milestone #${milestoneNumber}: ${errorMessage}`);
        return {
          success: false,
          error: `Milestone #${milestoneNumber} not found or failed to validate: ${errorMessage}`,
        };
      }
    }

    // Assign the milestone to the issue
    try {
      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would assign milestone #${milestoneNumber} to issue #${issueNumber}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            issue_number: issueNumber,
            milestone_number: milestoneNumber,
          },
        };
      }

      await githubClient.rest.issues.update({
        owner: milestoneOwner,
        repo: milestoneRepo,
        issue_number: issueNumber,
        milestone: milestoneNumber,
      });

      core.info(`Successfully assigned milestone #${milestoneNumber} to issue #${issueNumber}`);
      return {
        success: true,
        issue_number: issueNumber,
        milestone_number: milestoneNumber,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to assign milestone #${milestoneNumber} to issue #${issueNumber}: ${errorMessage}`);
      return {
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
