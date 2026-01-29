// @ts-check
/// <reference types="@actions/github-script" />

const { loadTemporaryIdMapFromResolved, resolveRepoIssueTarget } = require("./temporary_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

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

    // Convert resolvedTemporaryIds to a normalized Map for resolveIssueNumber
    const temporaryIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);

    // Resolve issue numbers, supporting temporary IDs from create_issue job
    const parentResolved = resolveRepoIssueTarget(item.parent_issue_number, temporaryIdMap, context.repo.owner, context.repo.repo);
    const subResolved = resolveRepoIssueTarget(item.sub_issue_number, temporaryIdMap, context.repo.owner, context.repo.repo);

    // Check if either parent or sub issue is an unresolved temporary ID
    // If so, defer the operation to allow for resolution later
    const hasUnresolvedParent = parentResolved.wasTemporaryId && !parentResolved.resolved;
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
    if (parentResolved.errorMessage) {
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

    const parentIssueNumber = parentResolved.resolved?.number;
    const subIssueNumber = subResolved.resolved?.number;

    if (!parentIssueNumber || !subIssueNumber) {
      core.error("Internal error: Issue numbers are undefined after successful resolution");
      return {
        parent_issue_number: item.parent_issue_number,
        sub_issue_number: item.sub_issue_number,
        success: false,
        error: "Issue numbers undefined",
      };
    }

    if (parentResolved.wasTemporaryId && parentResolved.resolved) {
      core.info(`Resolved parent temporary ID '${item.parent_issue_number}' to ${parentResolved.resolved.owner}/${parentResolved.resolved.repo}#${parentIssueNumber}`);
    }
    if (subResolved.wasTemporaryId && subResolved.resolved) {
      core.info(`Resolved sub-issue temporary ID '${item.sub_issue_number}' to ${subResolved.resolved.owner}/${subResolved.resolved.repo}#${subIssueNumber}`);
    }

    // Sub-issue linking is only supported within the same repository.
    if (parentResolved.resolved && subResolved.resolved) {
      const parentRepoSlug = `${parentResolved.resolved.owner}/${parentResolved.resolved.repo}`;
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
    }

    const owner = parentResolved.resolved?.owner || context.repo.owner;
    const repo = parentResolved.resolved?.repo || context.repo.repo;

    // Fetch parent issue to validate filters
    let parentIssue;
    try {
      const parentResponse = await github.rest.issues.get({
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
      const subResponse = await github.rest.issues.get({
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
      const parentCheckResult = await github.graphql(parentCheckQuery, {
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
          success: false,
          error: `Sub-issue is already a sub-issue of #${existingParent.number}`,
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
      // Get the parent issue's node ID for GraphQL
      const parentNodeId = parentIssue.node_id;
      const subNodeId = subIssue.node_id;

      // Use GraphQL mutation to add sub-issue
      await github.graphql(
        `
        mutation AddSubIssue($parentId: ID!, $subIssueId: ID!) {
          addSubIssue(input: { issueId: $parentId, subIssueId: $subIssueId }) {
            issue {
              id
              number
            }
            subIssue {
              id
              number
            }
          }
        }
      `,
        {
          parentId: parentNodeId,
          subIssueId: subNodeId,
        }
      );

      core.info(`Successfully linked issue #${subIssueNumber} as sub-issue of #${parentIssueNumber}`);
      return {
        parent_issue_number: parentIssueNumber,
        sub_issue_number: subIssueNumber,
        success: true,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
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
