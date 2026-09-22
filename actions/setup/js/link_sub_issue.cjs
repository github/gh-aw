// @ts-check
/// <reference types="@actions/github-script" />

const { loadTemporaryIdMapFromResolved, resolveRepoIssueTarget } = require("./temporary_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { resolveTarget } = require("./safe_output_helpers.cjs");

const HANDLER_TYPE = "link_sub_issue";
const { linkSubIssue } = require("./sub_issue_helpers.cjs");

/**
 * Main handler factory for link_sub_issue
 * Returns a message handler function that processes individual link_sub_issue messages
 * @param {Object} config - Handler configuration from GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG
 * @returns {Promise<Function>} Message handler function (message, resolvedTemporaryIds) => result
 */
async function main(config = {}) {
  // Extract configuration from config object
  const parentRequiredLabels = config.parent_required_labels || [];
  const parentTitlePrefix = config.parent_title_prefix || "";
  const subRequiredLabels = config.sub_required_labels || [];
  const subTitlePrefix = config.sub_title_prefix || "";
  const maxCount = config.max || 5;
  const targetConfig = config.target || "triggering";
  const githubClient = await createAuthenticatedGitHubClient(config);

  // Check if we're in staged mode
  const isStaged = isStagedMode(config);

  if (parentRequiredLabels.length > 0) {
    core.info(`Parent required labels: ${JSON.stringify(parentRequiredLabels)}`);
  }
  if (parentTitlePrefix) {
    core.info(`Parent title prefix: ${parentTitlePrefix}`);
  }
  if (subRequiredLabels.length > 0) {
    core.info(`Sub-issue required labels: ${JSON.stringify(subRequiredLabels)}`);
  }
  if (subTitlePrefix) {
    core.info(`Sub-issue title prefix: ${subTitlePrefix}`);
  }
  core.info(`Max count: ${maxCount}`);

  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  let defaultOwner = context.repo.owner;
  let defaultRepo = context.repo.repo;
  if (defaultTargetRepo) {
    const parts = defaultTargetRepo.split("/");
    if (parts.length === 2 && parts[0] && parts[1]) {
      defaultOwner = parts[0];
      defaultRepo = parts[1];
      core.info(`Target repository: ${defaultTargetRepo}`);
    }
  }
  if (allowedRepos.size > 0) core.info(`Allowed repositories: ${Array.from(allowedRepos).join(", ")}`);

  // Track how many items we've processed for max limit
  let processedCount = 0;

  /**
   * Message handler function that processes a single link_sub_issue message
   * @param {Object} message - The link_sub_issue message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleLinkSubIssue(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping link_sub_issue: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const item = message;

    // Resolve and validate target repository for this item
    const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "sub-issue link");
    if (!repoResult.success) {
      core.warning(`Skipping link_sub_issue: ${repoResult.error}`);
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: repoResult.error,
      };
    }
    const itemOwner = repoResult.repoParts.owner;
    const itemRepo = repoResult.repoParts.repo;

    // Convert resolvedTemporaryIds to a normalized Map for resolveIssueNumber
    const temporaryIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);

    // Resolve the sub-issue from model output. The configured target controls the parent.
    const subResolved = resolveRepoIssueTarget(item.sub_issue_number, temporaryIdMap, itemOwner, itemRepo);
    let parentResolved;
    let targetItem = item;
    if (targetConfig === "*") {
      parentResolved = resolveRepoIssueTarget(item.parent_issue_number, temporaryIdMap, itemOwner, itemRepo);
      if (parentResolved.resolved) {
        targetItem = { ...item, issue_number: parentResolved.resolved.number };
      }
    }

    // Check if either parent or sub issue is an unresolved temporary ID
    // If so, defer the operation to allow for resolution later
    const hasUnresolvedParent = parentResolved?.wasTemporaryId && !parentResolved.resolved;
    const hasUnresolvedSub = subResolved.wasTemporaryId && !subResolved.resolved;

    if (hasUnresolvedParent || hasUnresolvedSub) {
      const unresolvedIds = [];
      if (hasUnresolvedParent) {
        unresolvedIds.push(`parent: ${item.parent_issue_number}`);
      }
      if (hasUnresolvedSub) {
        unresolvedIds.push(`sub: ${item.sub_issue_number}`);
      }
      core.info(`Deferring link_sub_issue: unresolved temporary IDs (${unresolvedIds.join(", ")})`);

      // Return a deferred status to indicate this should be retried later
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        deferred: true,
        error: `Unresolved temporary IDs: ${unresolvedIds.join(", ")}`,
      };
    }

    // Check for other resolution errors (non-temporary ID issues)
    if (parentResolved?.errorMessage) {
      core.warning(`Failed to resolve parent issue: ${parentResolved.errorMessage}`);
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: parentResolved.errorMessage,
      };
    }

    if (subResolved.errorMessage) {
      core.warning(`Failed to resolve sub-issue: ${subResolved.errorMessage}`);
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: subResolved.errorMessage,
      };
    }

    const subIssueNumber = subResolved.resolved?.number;
    const parentTarget = resolveTarget({
      targetConfig,
      item: targetItem,
      context,
      itemType: HANDLER_TYPE,
      supportsIssue: true,
    });
    if (!parentTarget.success) {
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: parentTarget.error,
      };
    }
    const parentIssueNumber = parentTarget.number;

    if (!parentIssueNumber || !subIssueNumber) {
      core.error("Internal error: Issue numbers are undefined after successful resolution");
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: "Issue numbers undefined",
      };
    }

    if (parentResolved?.wasTemporaryId && parentResolved.resolved) {
      core.info(`Resolved parent temporary ID '${item.parent_issue_number}' to ${parentResolved.resolved.owner}/${parentResolved.resolved.repo}#${parentIssueNumber}`);
    }
    if (subResolved.wasTemporaryId && subResolved.resolved) {
      core.info(`Resolved sub-issue temporary ID '${item.sub_issue_number}' to ${subResolved.resolved.owner}/${subResolved.resolved.repo}#${subIssueNumber}`);
    }

    const owner = parentResolved?.resolved?.owner || itemOwner;
    const repo = parentResolved?.resolved?.repo || itemRepo;

    // Sub-issue linking is only supported within the same repository.
    if (subResolved.resolved) {
      const parentRepoSlug = `${owner}/${repo}`;
      const subRepoSlug = `${subResolved.resolved.owner}/${subResolved.resolved.repo}`;
      if (parentRepoSlug !== subRepoSlug) {
        const error = `Parent and sub-issue must be in the same repository for link_sub_issue (got ${parentRepoSlug} and ${subRepoSlug})`;
        core.warning(error);
        return {
          parent_issue_number: item.parent_issue_number,
          sub_issue_number: item.sub_issue_number,
          success: false,
          error,
        };
      }

      // Validate the effective parent/sub-issue pair (after applying `target`), not the
      // raw model-provided values which may have been overridden by a fixed/triggering target.
      if (parentRepoSlug === subRepoSlug && parentIssueNumber === subIssueNumber) {
        const error = `Parent and sub-issue must be different (both resolve to #${parentIssueNumber})`;
        core.warning(error);
        return {
          parent_issue_number: item.parent_issue_number,
          sub_issue_number: item.sub_issue_number,
          success: false,
          error,
        };
      }
    }

    // Fetch parent issue to validate filters
    let parentIssue;
    try {
      const parentResponse = await githubClient.rest.issues.get({
        owner,
        repo,
        issue_number: parentIssueNumber,
      });
      parentIssue = parentResponse.data;
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.warning(`Failed to fetch parent issue #${parentIssueNumber}: ${errorMessage}`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: false,
        error: `Failed to fetch parent issue: ${errorMessage}`,
      };
    }

    // Validate parent issue filters
    if (parentRequiredLabels.length > 0) {
      const parentLabels = parentIssue.labels.map(l => (typeof l === "string" ? l : l.name || ""));
      const missingLabels = parentRequiredLabels.filter(required => !parentLabels.includes(required));
      if (missingLabels.length > 0) {
        core.warning(`Parent issue #${parentIssueNumber} is missing required labels: ${missingLabels.join(", ")}. Skipping.`);
        return {
          parent_issue_number: parentIssueNumber,
          sub_issue_number: subIssueNumber,
          success: false,
          error: `Parent issue missing required labels: ${missingLabels.join(", ")}`,
        };
      }
    }

    if (parentTitlePrefix && !parentIssue.title.startsWith(parentTitlePrefix)) {
      core.warning(`Parent issue #${parentIssueNumber} title does not start with "${parentTitlePrefix}". Skipping.`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: false,
        error: `Parent issue title does not start with "${parentTitlePrefix}"`,
      };
    }

    // Fetch sub-issue to validate filters
    let subIssue;
    try {
      const subResponse = await githubClient.rest.issues.get({
        owner,
        repo,
        issue_number: subIssueNumber,
      });
      subIssue = subResponse.data;
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to fetch sub-issue #${subIssueNumber}: ${errorMessage}`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: false,
        error: `Failed to fetch sub-issue: ${errorMessage}`,
      };
    }

    // Check if the sub-issue already has a parent using GraphQL
    try {
      const parentCheckQuery = `
        query($owner: String!, $repo: String!, $number: Int!) {
          repository(owner: $owner, name: $repo) {
            issue(number: $number) {
              parent {
                number
                title
              }
            }
          }
        }
      `;
      const parentCheckResult = await githubClient.graphql(parentCheckQuery, {
        owner,
        repo,
        number: subIssueNumber,
      });

      const existingParent = parentCheckResult?.repository?.issue?.parent;
      if (existingParent) {
        core.warning(`Sub-issue #${subIssueNumber} is already a sub-issue of #${existingParent.number} ("${existingParent.title}"). Skipping.`);
        return {
          parent_issue_number: parentIssueNumber,
          sub_issue_number: subIssueNumber,
          success: true,
          skipped: true,
        };
      }
    } catch (error) {
      // If the GraphQL query fails (e.g., parent field not available), log warning but continue
      const errorMessage = getErrorMessage(error);
      core.warning(`Could not check if sub-issue #${subIssueNumber} has a parent: ${errorMessage}. Proceeding with link attempt.`);
    }

    // Validate sub-issue filters
    if (subRequiredLabels.length > 0) {
      const subLabels = subIssue.labels.map(l => (typeof l === "string" ? l : l.name || ""));
      const missingLabels = subRequiredLabels.filter(required => !subLabels.includes(required));
      if (missingLabels.length > 0) {
        core.warning(`Sub-issue #${subIssueNumber} is missing required labels: ${missingLabels.join(", ")}. Skipping.`);
        return {
          parent_issue_number: parentIssueNumber,
          sub_issue_number: subIssueNumber,
          success: false,
          error: `Sub-issue missing required labels: ${missingLabels.join(", ")}`,
        };
      }
    }

    if (subTitlePrefix && !subIssue.title.startsWith(subTitlePrefix)) {
      core.warning(`Sub-issue #${subIssueNumber} title does not start with "${subTitlePrefix}". Skipping.`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: false,
        error: `Sub-issue title does not start with "${subTitlePrefix}"`,
      };
    }

    // Link the sub-issue using GraphQL mutation
    try {
      const parentNodeId = parentIssue.node_id;
      const subIssueNodeId = subIssue.node_id;

      // If in staged mode, preview without executing
      if (isStaged) {
        logStagedPreviewInfo(`Would link issue #${subIssueNumber} as sub-issue of #${parentIssueNumber}`);
        return {
          success: true,
          staged: true,
          previewInfo: {
            parent_issue_number: parentIssueNumber,
            sub_issue_number: subIssueNumber,
          },
        };
      }

      await linkSubIssue(
        {
          owner,
          repo,
          parentIssueNumber,
          subIssueNumber,
          parentNodeId,
          subIssueNodeId,
        },
        githubClient
      );

      core.info(`Successfully linked issue #${subIssueNumber} as sub-issue of #${parentIssueNumber}`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: true,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      // If the mutation fails because the sub-issue is already linked (to any parent),
      // treat it as a successful no-op — the pre-flight parent check may have failed to
      // detect the existing relationship (e.g. due to schema availability), but the
      // desired state (sub-issue belongs to a group) is effectively achieved.
      if (/already.*sub.?issue/i.test(errorMessage)) {
        core.warning(`Sub-issue #${subIssueNumber} is already linked as a sub-issue (confirmed via mutation error). Skipping.`);
        return {
          parent_issue_number: parentIssueNumber,
          sub_issue_number: subIssueNumber,
          success: true,
          skipped: true,
        };
      }
      core.warning(`Failed to link issue #${subIssueNumber} as sub-issue of #${parentIssueNumber}: ${errorMessage}`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: false,
        error: errorMessage,
      };
    }
  };
}

module.exports = { main };
