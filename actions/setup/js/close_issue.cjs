// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

const { resolveTargetRepoConfig, resolveAndValidateRepo, validateRepo } = require("./repo_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { ERR_NOT_FOUND, ERR_VALIDATION } = require("./error_codes.cjs");
const { createCloseEntityHandler, buildCommentBody, ISSUE_CONFIG } = require("./close_entity_helpers.cjs");
const { loadTemporaryIdMapFromResolved, resolveRepoIssueTarget } = require("./temporary_id.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeIssueIntentMetadata } = require("./issue_intents.cjs");

/**
 * Parse a `duplicate_of` value into { owner, repo, issueNumber }.
 * Accepts:
 *   - plain number or numeric string (same-repo)
 *   - "#NUMBER" (same-repo)
 *   - "owner/repo#NUMBER"
 *   - "https://github.com/owner/repo/issues/NUMBER"
 *
 * @param {string|number} value - The duplicate_of field value
 * @param {string} defaultOwner - Owner to use when not specified in value
 * @param {string} defaultRepo - Repo to use when not specified in value
 * @returns {{ owner: string, repo: string, issueNumber: number }|null} Parsed reference or null if invalid
 */
function parseDuplicateOf(value, defaultOwner, defaultRepo) {
  if (value === undefined || value === null || value === "") return null;

  const str = String(value).trim();

  // Bare number or "#NUMBER" — positive integers only (no #0 or 0)
  const bareMatch = str.match(/^#?([1-9]\d*)$/);
  if (bareMatch) {
    if (bareMatch[1].length > 15) return null;
    const issueNumber = parseInt(bareMatch[1], 10);
    if (!Number.isSafeInteger(issueNumber) || issueNumber < 1) return null;
    return { owner: defaultOwner, repo: defaultRepo, issueNumber };
  }

  // "owner/repo#NUMBER" — owner and repo must not contain '/' or '#'
  const refMatch = str.match(/^([\w.-]+)\/([\w.-]+)#([1-9]\d*)$/);
  if (refMatch) {
    if (refMatch[3].length > 15) return null;
    const issueNumber = parseInt(refMatch[3], 10);
    if (!Number.isSafeInteger(issueNumber) || issueNumber < 1) return null;
    return { owner: refMatch[1], repo: refMatch[2], issueNumber };
  }

  // GitHub issue URL: https://github.com/owner/repo/issues/NUMBER — stop at path/query/fragment
  const urlMatch = str.match(/^https?:\/\/github\.com\/([\w.-]+)\/([\w.-]+)\/issues\/([1-9]\d*)(?:[?#/].*)?$/);
  if (urlMatch) {
    if (urlMatch[3].length > 15) return null;
    const issueNumber = parseInt(urlMatch[3], 10);
    if (!Number.isSafeInteger(issueNumber) || issueNumber < 1) return null;
    return { owner: urlMatch[1], repo: urlMatch[2], issueNumber };
  }

  return null;
}

/**
 * Mark an issue as a duplicate of another issue using the GitHub GraphQL markAsDuplicate mutation.
 * This creates a native "marked this as a duplicate of #X" timeline event.
 *
 * @param {any} github - Authenticated GitHub client (must support graphql)
 * @param {string} duplicateNodeId - GraphQL node ID of the issue being marked as a duplicate
 * @param {string} canonicalOwner - Owner of the canonical issue's repo
 * @param {string} canonicalRepo - Repo of the canonical issue
 * @param {number} canonicalNumber - Issue number that is the canonical original
 * @returns {Promise<void>}
 */
async function markIssueAsDuplicate(github, duplicateNodeId, canonicalOwner, canonicalRepo, canonicalNumber) {
  if (!duplicateNodeId) {
    throw new Error(`${ERR_NOT_FOUND}: node_id missing for duplicate issue`);
  }

  const { data: canonicalData } = await github.rest.issues.get({
    owner: canonicalOwner,
    repo: canonicalRepo,
    issue_number: canonicalNumber,
  });

  const canonicalNodeId = canonicalData.node_id;
  if (!canonicalNodeId) {
    throw new Error(`${ERR_NOT_FOUND}: node_id missing for canonical issue ${canonicalOwner}/${canonicalRepo}#${canonicalNumber}`);
  }

  if (duplicateNodeId === canonicalNodeId) {
    throw new Error(`${ERR_VALIDATION}: Cannot mark issue as a duplicate of itself`);
  }

  await github.graphql(
    `mutation($duplicateId: ID!, $canonicalId: ID!) {
      markAsDuplicate(input: { duplicateId: $duplicateId, canonicalId: $canonicalId }) {
        duplicate {
          ... on Issue {
            id
            number
          }
        }
      }
    }`,
    { duplicateId: duplicateNodeId, canonicalId: canonicalNodeId }
  );
}

/**
 * Get issue details using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @returns {Promise<{number: number, title: string, labels: Array<{name: string}>, html_url: string, state: string}>} Issue details
 */
async function getIssueDetails(github, owner, repo, issueNumber) {
  const { data: issue } = await github.rest.issues.get({
    owner,
    repo,
    issue_number: issueNumber,
  });

  if (!issue) {
    throw new Error(`${ERR_NOT_FOUND}: Issue #${issueNumber} not found in ${owner}/${repo}`);
  }

  return issue;
}

/**
 * Add comment to a GitHub Issue using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} message - Comment body
 * @returns {Promise<{id: number, html_url: string}>} Comment details
 */
async function addIssueComment(github, owner, repo, issueNumber, message) {
  const { data: comment } = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body: message,
  });

  return comment;
}

/**
 * Close a GitHub Issue using REST API
 * @param {any} github - GitHub REST API instance
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {number} issueNumber - Issue number
 * @param {string} stateReason - The reason for closing: "COMPLETED", "NOT_PLANNED", or "DUPLICATE"
 * @returns {Promise<{number: number, html_url: string, title: string, node_id: string}>} Issue details
 */
async function closeIssue(github, owner, repo, issueNumber, stateReason, intentMetadata, useIssueIntent) {
  const baseParams = {
    owner,
    repo,
    issue_number: issueNumber,
    state: "closed",
    state_reason: (stateReason || "COMPLETED").toLowerCase(),
  };

  const hasIntentMetadata = Boolean(intentMetadata && Object.keys(intentMetadata).length > 0);
  if (useIssueIntent && hasIntentMetadata) {
    try {
      const { data: issue } = await github.request("PATCH /repos/{owner}/{repo}/issues/{issue_number}", {
        owner,
        repo,
        issue_number: issueNumber,
        state: { value: "closed", ...intentMetadata },
        state_reason: baseParams.state_reason,
      });
      return issue;
    } catch (error) {
      core.warning(`Issue-intent close path unavailable, falling back to legacy close path: ${getErrorMessage(error)}`);
    }
  }

  const { data: issue } = await github.rest.issues.update(baseParams);

  return issue;
}

/**
 * Main handler factory for close_issue
 * Returns a message handler function that processes individual close_issue messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Determine the state-reason configuration mode:
  //   - config.state_reason (string): scalar — fixed reason, agent cannot override
  //   - config.allowed_state_reason (string[]): list — agent may select from this subset
  //   - neither set: omitted — agent may select from all three supported values
  const configStateReason = config.state_reason || null;
  /** @type {string[]|null} */
  const configStateReasons = Array.isArray(config.allowed_state_reason) && config.allowed_state_reason.length > 0 ? config.allowed_state_reason : null;

  // The fallback used when the agent does not supply state_reason at item level.
  // Scalar config → use the configured value.
  // List config   → use the first item in the list.
  // Omitted       → default to "COMPLETED" (backward compatible).
  const defaultStateReason = configStateReason || (configStateReasons ? configStateReasons[0] : "COMPLETED");

  const issueIntentEnabled = config.issue_intent !== false;
  const requiredLabels = config.required_labels || [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const githubClient = await createAuthenticatedGitHubClient(config);

  let stateReasonMode;
  if (configStateReason) {
    stateReasonMode = `scalar(${configStateReason})`;
  } else if (configStateReasons) {
    stateReasonMode = `list(${configStateReasons.join(",")})`;
  } else {
    stateReasonMode = "omitted";
  }
  core.info(`Close issue configuration: max=${config.max || 10}, state_reason=${stateReasonMode}, issue_intent=${issueIntentEnabled}`);
  if (requiredLabels.length > 0) {
    core.info(`Required labels: ${requiredLabels.join(", ")}`);
  }
  if (requiredTitlePrefix) {
    core.info(`Required title prefix: ${requiredTitlePrefix}`);
  }
  core.info(`Default target repo: ${defaultTargetRepo}`);
  if (allowedRepos.size > 0) {
    core.info(`Allowed repos: ${Array.from(allowedRepos).join(", ")}`);
  }

  return createCloseEntityHandler(
    config,
    ISSUE_CONFIG,
    {
      resolveTarget(item, _config, resolvedTemporaryIds) {
        // Resolve and validate target repository
        const repoResult = resolveAndValidateRepo(item, defaultTargetRepo, allowedRepos, "issue");
        if (!repoResult.success) {
          return { success: false, error: repoResult.error };
        }
        const { repo: entityRepo, repoParts } = repoResult;

        // Determine issue number - either from explicit field or from context
        if (item.issue_number !== undefined) {
          // Try to resolve as temporary ID first, then fall back to integer parsing
          const tempIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);
          const resolvedTarget = resolveRepoIssueTarget(item.issue_number, tempIdMap, repoParts.owner, repoParts.repo);
          if (resolvedTarget.wasTemporaryId && resolvedTarget.resolved) {
            const issueNumber = resolvedTarget.resolved.number;
            core.info(`Resolved temporary ID '${item.issue_number}' to #${issueNumber}`);
            return { success: true, entityNumber: issueNumber, owner: repoParts.owner, repo: repoParts.repo, entityRepo };
          } else if (resolvedTarget.wasTemporaryId && !resolvedTarget.resolved) {
            return {
              success: false,
              deferred: true,
              error: resolvedTarget.errorMessage || `Unresolved temporary ID: ${item.issue_number}`,
            };
          }

          // Not a temporary ID - parse as integer
          const issueNumber = parseInt(String(item.issue_number), 10);
          if (Number.isNaN(issueNumber)) {
            return { success: false, error: `Invalid issue number: ${item.issue_number}` };
          }
          return { success: true, entityNumber: issueNumber, owner: repoParts.owner, repo: repoParts.repo, entityRepo };
        }

        // Fall back to context issue number
        const contextIssue = context.payload?.issue?.number;
        if (!contextIssue) {
          return { success: false, error: "No issue number available" };
        }
        return { success: true, entityNumber: contextIssue, owner: repoParts.owner, repo: repoParts.repo, entityRepo };
      },

      getDetails: getIssueDetails,

      validateLabels(entity, entityNumber, requiredLabels) {
        if (requiredLabels.length > 0) {
          const issueLabels = entity.labels.map(/** @param {any} l */ l => (typeof l === "string" ? l : l.name || ""));
          const missingLabels = requiredLabels.filter(required => !issueLabels.includes(required));
          if (missingLabels.length > 0) {
            return {
              valid: false,
              warning: `Issue #${entityNumber} missing required labels: ${missingLabels.join(", ")}`,
              error: `Missing required labels: ${missingLabels.join(", ")}`,
            };
          }
        }
        return { valid: true };
      },

      buildCommentBody(sanitizedBody) {
        const triggeringIssueNumber = context.payload?.issue?.number;
        const triggeringPRNumber = context.payload?.pull_request?.number;
        return buildCommentBody(sanitizedBody, triggeringIssueNumber, triggeringPRNumber, config.body_footer);
      },

      addComment: addIssueComment,

      closeEntity(github, owner, repo, entityNumber, item) {
        // Determine effective state_reason, validating against permitted values when applicable.
        let stateReason;
        if (item.state_reason !== undefined && item.state_reason !== null) {
          const provided = String(item.state_reason);
          if (configStateReason) {
            // Scalar config: state_reason is fixed by config; the agent cannot override it.
            // The field was not exposed in the tool schema, so enforce the configured value
            // regardless of what the agent supplied.
            stateReason = configStateReason;
            if (provided.toLowerCase() !== stateReason.toLowerCase()) {
              core.debug(`state_reason agent value "${provided.toLowerCase()}" overridden by scalar config value "${stateReason.toLowerCase()}"`);
            }
          } else if (configStateReasons) {
            // List config: validate against the configured subset.
            const upperProvided = provided.toUpperCase();
            const upperAllowed = configStateReasons.map(r => r.toUpperCase());
            if (!upperAllowed.includes(upperProvided)) {
              throw new Error(`${ERR_VALIDATION}: state_reason "${provided}" is not permitted. Allowed values: ${configStateReasons.join(", ")}`);
            }
            stateReason = provided.toLowerCase();
          } else {
            // Omitted config: validate against all three supported values.
            const upperProvided = provided.toUpperCase();
            const supportedUpper = ["COMPLETED", "NOT_PLANNED", "DUPLICATE"];
            if (!supportedUpper.includes(upperProvided)) {
              throw new Error(`${ERR_VALIDATION}: state_reason "${provided}" is not a supported value. Supported values: completed, not_planned, duplicate`);
            }
            stateReason = provided.toLowerCase();
          }
        } else {
          stateReason = defaultStateReason;
        }

        const intentMetadata = issueIntentEnabled ? normalizeIssueIntentMetadata(item) : {};
        core.info(`Closing issue #${entityNumber} with state_reason=${stateReason}`);

        const closePromise = closeIssue(github, owner, repo, entityNumber, stateReason, intentMetadata, issueIntentEnabled);

        // When duplicate_of is provided and state_reason is DUPLICATE, create the native duplicate relationship
        const stateReasonUpper = stateReason.toUpperCase();
        if (item.duplicate_of !== undefined && item.duplicate_of !== null && stateReasonUpper !== "DUPLICATE") {
          core.warning(`duplicate_of is set but state_reason is ${stateReason} (not DUPLICATE); native duplicate marking will be skipped`);
        }
        if (item.duplicate_of !== undefined && item.duplicate_of !== null && stateReasonUpper === "DUPLICATE") {
          const parsed = parseDuplicateOf(item.duplicate_of, owner, repo);
          if (parsed) {
            // Validate canonical repo against the same allowedRepos policy
            const canonicalRepoSlug = `${parsed.owner}/${parsed.repo}`;
            const repoValidation = validateRepo(canonicalRepoSlug, defaultTargetRepo, allowedRepos);
            if (!repoValidation.valid) {
              core.warning(`Skipping native duplicate marking: canonical repo "${canonicalRepoSlug}" is not in the allowed-repos list`);
              return closePromise;
            }

            // Guard against marking an issue as a duplicate of itself
            if (parsed.owner === owner && parsed.repo === repo && parsed.issueNumber === entityNumber) {
              core.warning(`Skipping native duplicate marking: issue #${entityNumber} cannot be a duplicate of itself`);
              return closePromise;
            }

            core.info(`Marking issue #${entityNumber} as duplicate of ${parsed.owner}/${parsed.repo}#${parsed.issueNumber}`);
            return closePromise.then(async closedEntity => {
              try {
                await markIssueAsDuplicate(github, closedEntity.node_id, parsed.owner, parsed.repo, parsed.issueNumber);
                core.info(`✓ Marked issue #${entityNumber} as duplicate of ${parsed.owner}/${parsed.repo}#${parsed.issueNumber}`);
              } catch (dupError) {
                core.warning(`Failed to mark native duplicate relationship for #${entityNumber}: ${getErrorMessage(dupError)}`);
              }
              return closedEntity;
            });
          } else {
            core.warning(`duplicate_of value "${item.duplicate_of}" could not be parsed; skipping native duplicate marking`);
          }
        }

        return closePromise;
      },

      continueOnCommentError: false,

      buildSuccessResult(closedEntity, commentResult, wasAlreadyClosed) {
        return {
          success: true,
          number: closedEntity.number,
          url: closedEntity.html_url,
          title: closedEntity.title,
          alreadyClosed: wasAlreadyClosed,
        };
      },
    },
    githubClient
  );
}

module.exports = { main, parseDuplicateOf };
