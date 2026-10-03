// @ts-check
/// <reference types="@actions/github-script" />

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/** @type {string} Safe output type handled by this module */
const HANDLER_TYPE = "dispatch_workflow";

const { getErrorMessage } = require("./error_helpers.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { resolveTargetRepoConfig, parseRepoSlug, validateTargetRepo } = require("./repo_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { buildAwContext } = require("./aw_context.cjs");
const { readPublishedAssignment } = require("./publish_dispatch_work_claims.cjs");
const { loadTemporaryIdMapFromResolved, resolveIssueNumber, replaceTemporaryIdReferences } = require("./temporary_id.cjs");

function buildDispatchAwContext(claimID, staged, readAssignment = readPublishedAssignment) {
  return {
    ...buildAwContext(),
    ...(claimID !== undefined && !staged ? { work_queue: readAssignment(claimID) } : {}),
  };
}

/**
 * Main handler factory for dispatch_workflow
 * Returns a message handler function that processes individual dispatch_workflow messages
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  // Extract configuration
  const allowedWorkflows = config.workflows || [];
  const maxCount = config.max || 1;
  const workflowFiles = config.workflow_files || {}; // Map of workflow name to file extension
  const awContextWorkflows = new Set(config.aw_context_workflows || []); // Workflows that accept aw_context input
  const githubClient = await createAuthenticatedGitHubClient(config);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const allowedRefPatterns = parseAllowedRefPatterns(config.allowed_refs);
  const allowedRefRegexes = allowedRefPatterns.map(pattern => globPatternToRegex(pattern, { pathMode: true, caseSensitive: true }));

  // Resolve the dispatch destination repository from target-repo config, falling back to context.repo
  const contextRepoSlug = `${context.repo.owner}/${context.repo.repo}`;
  const normalizedTargetRepo = (defaultTargetRepo ?? "").toString().trim();

  let resolvedRepoSlug = contextRepoSlug;
  let repo = context.repo;

  if (normalizedTargetRepo) {
    const parsedRepo = parseRepoSlug(normalizedTargetRepo);
    if (!parsedRepo) {
      core.warning(`Invalid 'target-repo' configuration value '${normalizedTargetRepo}'; falling back to workflow context repository ${contextRepoSlug}.`);
    } else {
      resolvedRepoSlug = normalizedTargetRepo;
      repo = parsedRepo;
    }
  }

  const isCrossRepoDispatch = resolvedRepoSlug !== contextRepoSlug;

  // SEC-005: Enforce cross-repository allowlist per Safe Outputs Specification §3.2.6 (SP6).
  // Default-deny: cross-repo dispatch is only permitted when an explicit allowlist is configured
  // and the resolved target repo is present in that list. Uses validateTargetRepo from
  // repo_helpers.cjs for consistent slug validation and glob-pattern matching (e.g. "org/*").
  if (isCrossRepoDispatch) {
    if (allowedRepos.size === 0) {
      throw new Error(
        `E004: Cross-repository dispatch to '${resolvedRepoSlug}' is not permitted. No allowlist is configured. Define 'allowed-repos' in the workflow's 'safe-outputs.dispatch-workflow' section to enable cross-repository dispatch.`
      );
    }
    const repoValidation = validateTargetRepo(resolvedRepoSlug, contextRepoSlug, allowedRepos);
    if (!repoValidation.valid) {
      throw new Error(`E004: ${repoValidation.error}`);
    }
    core.info(`Cross-repo allowlist check passed for ${resolvedRepoSlug}`);
  }

  core.info(`Dispatch workflow configuration: max=${maxCount}`);
  if (allowedWorkflows.length > 0) {
    core.info(`Allowed workflows: ${allowedWorkflows.join(", ")}`);
  }
  if (Object.keys(workflowFiles).length > 0) {
    core.info(`Workflow files: ${JSON.stringify(workflowFiles)}`);
  }
  if (isCrossRepoDispatch) {
    core.info(`Dispatching to target repo: ${resolvedRepoSlug}`);
  }

  // Track how many items we've processed for max limit
  let processedCount = 0;
  let lastDispatchTime = 0;
  const isStaged = isStagedMode(config);

  // Helper function to get the default branch of the dispatch target repository
  const getDefaultBranchRef = async () => {
    // Only use the context payload's default_branch when dispatching to the caller's own repo
    if (!isCrossRepoDispatch && context.payload.repository?.default_branch) {
      return `refs/heads/${context.payload.repository.default_branch}`;
    }

    // Fall back to querying the target repository
    try {
      const { data: repoData } = await githubClient.rest.repos.get({
        owner: repo.owner,
        repo: repo.repo,
      });
      return `refs/heads/${repoData.default_branch}`;
    } catch (error) {
      core.warning(`Failed to fetch default branch: ${getErrorMessage(error)}`);
      return "refs/heads/main";
    }
  };

  // For pull_request events, GITHUB_REF points to a merge ref and GITHUB_HEAD_REF contains
  // the PR branch. For issue_comment events on PRs, resolve the head branch through the API.
  // For cross-repo dispatch (workflow_call relay), the caller's GITHUB_REF has no meaning on
  // the target repository, so we use the compiler-injected target-ref instead.
  const resolveDefaultRef = async () => {
    if (config["target-ref"]) {
      // Compiler-injected target ref for cross-repo dispatch (workflow_call relay pattern).
      // Takes precedence over all environment variables to avoid using the caller's ref.
      const ref = config["target-ref"];
      core.info(`Using configured target-ref: ${ref}`);
      return ref;
    }
    if (process.env.GITHUB_HEAD_REF) {
      // We're in a pull_request event, use the PR branch ref
      const ref = `refs/heads/${process.env.GITHUB_HEAD_REF}`;
      core.info(`Using PR branch ref: ${ref}`);
      return ref;
    }
    if (!isCrossRepoDispatch && context.eventName === "issue_comment" && context.payload.issue?.pull_request) {
      const { data: pullRequest } = await githubClient.rest.pulls.get({
        owner: context.repo.owner,
        repo: context.repo.repo,
        pull_number: context.payload.issue.number,
      });
      if (!pullRequest?.head?.ref) {
        throw new Error(`Unable to resolve head ref for pull request #${context.payload.issue.number}`);
      }
      const ref = `refs/heads/${pullRequest.head.ref}`;
      core.info(`Using PR branch ref: ${ref}`);
      return ref;
    }
    if (process.env.GITHUB_REF || context.ref) {
      // Use GITHUB_REF for non-PR contexts (push, workflow_dispatch, etc.)
      return process.env.GITHUB_REF || context.ref;
    }

    // Last resort: fetch the repository's default branch
    const ref = await getDefaultBranchRef();
    core.info(`Using default branch ref: ${ref}`);
    return ref;
  };
  /** @type {Promise<string> | undefined} */
  let defaultRefPromise;
  // Cache the resolved ref so multiple dispatches share a single API lookup, but drop the
  // cache on failure so a transient error doesn't poison every later dispatch in the run.
  const getDefaultRef = () => {
    defaultRefPromise ??= resolveDefaultRef().catch(error => {
      defaultRefPromise = undefined;
      throw error;
    });
    return defaultRefPromise;
  };

  /**
   * Message handler function that processes a single dispatch_workflow message
   * @param {Object} message - The dispatch_workflow message to process
   * @param {Object} resolvedTemporaryIds - Map of temporary IDs to {repo, number}
   * @returns {Promise<Object>} Result with success/error status
   */
  return async function handleDispatchWorkflow(message, resolvedTemporaryIds) {
    // Check if we've hit the max limit
    if (processedCount >= maxCount) {
      core.warning(`Skipping dispatch_workflow: max count of ${maxCount} reached`);
      return {
        success: false,
        error: `Max count of ${maxCount} reached`,
      };
    }

    processedCount++;

    const workflowName = typeof message.workflow_name === "string" ? message.workflow_name.trim() : "";

    if (!workflowName || workflowName.trim() === "") {
      core.warning("Workflow name is empty, skipping");
      return {
        success: false,
        error: "Workflow name is empty",
      };
    }

    // Validate workflow is in allowed list
    if (allowedWorkflows.length > 0 && !allowedWorkflows.includes(workflowName)) {
      const error = `Workflow "${workflowName}" is not in the allowed workflows list: ${allowedWorkflows.join(", ")}`;
      core.warning(error);
      return {
        success: false,
        error: error,
      };
    }

    try {
      // Add 5 second delay between dispatches (except for the first one)
      if (lastDispatchTime > 0) {
        const timeSinceLastDispatch = Date.now() - lastDispatchTime;
        const delayNeeded = 5000 - timeSinceLastDispatch;
        if (delayNeeded > 0) {
          core.info(`Waiting ${Math.ceil(delayNeeded / 1000)} seconds before next dispatch...`);
          await new Promise(resolve => setTimeout(resolve, delayNeeded));
        }
      }

      core.info(`Dispatching workflow: ${workflowName}`);

      if (message.ref !== undefined && message.ref !== null && typeof message.ref !== "string") {
        core.warning(`message.ref must be a string; ignoring non-string value (type: ${typeof message.ref})`);
      }
      const outputRef = typeof message.ref === "string" ? message.ref.trim() : "";
      let ref;
      if (outputRef) {
        ref = normalizeRef(outputRef);
        if (allowedRefRegexes.length === 0) {
          const error = "message.ref is not allowed unless 'allowed-refs' is configured in safe-outputs.dispatch-workflow";
          core.warning(error);
          return {
            success: false,
            error,
          };
        }
      } else {
        ref = await getDefaultRef();
      }
      // Enforce allowed-refs for every agent-supplied ref, and for context-derived refs unless
      // the ref came from the compiler-injected target-ref (which is trusted by construction and
      // may legitimately point outside the allowlist for cross-repo dispatch).
      if ((outputRef || !config["target-ref"]) && allowedRefRegexes.length > 0 && !allowedRefRegexes.some(pattern => pattern.test(ref))) {
        const error = `Ref '${ref}' is not in allowed-refs: ${allowedRefPatterns.join(", ")}`;
        core.warning(error);
        return {
          success: false,
          error,
        };
      }

      // Prepare inputs - convert all values to strings as required by workflow_dispatch
      // and resolve any #temporary_id references before dispatching
      /** @type {Record<string, string>} */
      const inputs = {};
      if (message.inputs && typeof message.inputs === "object") {
        // Build a Map from resolvedTemporaryIds for use with replaceTemporaryIdReferences
        const temporaryIdMap = loadTemporaryIdMapFromResolved(resolvedTemporaryIds);

        for (const [key, value] of Object.entries(message.inputs)) {
          if (key === "work_queue_claim_id") continue;
          // Convert value to string
          let strValue;
          if (value === null || value === undefined) {
            strValue = "";
          } else if (typeof value === "object") {
            strValue = JSON.stringify(value);
          } else {
            strValue = String(value);
          }

          // Resolve temporary ID references in string input values.
          // If the entire value is a pure temporary ID (e.g. "#aw_slosync"), resolve it
          // using the full temporaryIdMap (which retains repo context) so we can emit
          // just the number for same-repo targets and "owner/repo#number" for cross-repo,
          // consistent with how replaceTemporaryIdReferences handles embedded references.
          // For text values with embedded references (e.g. "See #aw_xxx"), replace each
          // reference with its cross-repository link (owner/repo#number).
          if (strValue !== "") {
            const pureIdResult = resolveIssueNumber(strValue, temporaryIdMap);
            if (pureIdResult.wasTemporaryId) {
              if (pureIdResult.errorMessage || !pureIdResult.resolved) {
                core.warning(`Unresolved temporary ID in input "${key}": ${pureIdResult.errorMessage}`);
              } else if (pureIdResult.resolved.repo === resolvedRepoSlug) {
                strValue = String(pureIdResult.resolved.number);
              } else {
                strValue = `${pureIdResult.resolved.repo}#${pureIdResult.resolved.number}`;
              }
            } else if (temporaryIdMap.size > 0) {
              strValue = replaceTemporaryIdReferences(strValue, temporaryIdMap, resolvedRepoSlug);
            }
          }

          inputs[key] = strValue;
        }
      }

      // Inject aw_context if the target workflow declares it as an input.
      // Only workflows listed in aw_context_workflows (populated at compile time) support this.
      const queueClaimID = message.inputs?.work_queue_claim_id;
      if (queueClaimID !== undefined && (typeof queueClaimID !== "string" || !queueClaimID || isCrossRepoDispatch || !awContextWorkflows.has(workflowName))) {
        throw new Error("work_queue_claim_id requires a same-repository worker workflow with aw_context support");
      }
      if (queueClaimID !== undefined && !isStaged && process.env.GH_AW_WORK_QUEUE_CLAIMS_VERIFIED !== "true") {
        throw new Error("work_queue_claim_id requires verified publication by tools.work-queue");
      }
      if (awContextWorkflows.has(workflowName)) {
        const awContext = buildDispatchAwContext(queueClaimID, isStaged);
        inputs["aw_context"] = JSON.stringify(awContext);
      }

      // Get the workflow file extension from compile-time resolution
      let extension = workflowFiles[workflowName];
      if (!extension && isCrossRepoDispatch) {
        extension = ".lock.yml";
        core.info(`Cross-repo workflow "${workflowName}" has no extension mapping; defaulting to ${extension}`);
      }
      if (!extension) {
        return {
          success: false,
          error: `Workflow "${workflowName}" file extension not found in configuration. This workflow may not have been validated at compile time.`,
        };
      }

      const workflowFile = `${workflowName}${extension}`;
      core.info(`Dispatching workflow: ${workflowFile}`);

      // If in staged mode, preview the dispatch without executing it
      if (isStaged) {
        logStagedPreviewInfo(`Would dispatch workflow: ${workflowFile} in ${resolvedRepoSlug} with ref: ${ref}`);
        return {
          success: true,
          staged: true,
          workflow_name: workflowName,
          inputs: inputs,
        };
      }

      // Dispatch the workflow using the resolved file.
      // Request return_run_details for newer GitHub API support; fall back without it
      // for older GitHub Enterprise Server deployments that don't support the parameter.
      /** @type {{ data: { workflow_run_id?: number } }} */
      let response;
      try {
        response = await githubClient.rest.actions.createWorkflowDispatch({
          owner: repo.owner,
          repo: repo.repo,
          workflow_id: workflowFile,
          ref: ref,
          inputs: inputs,
          return_run_details: true,
        });
      } catch (dispatchError) {
        /** @type {any} */
        const err = dispatchError;
        const status = err && typeof err === "object" ? err.status : undefined;
        const dispatchErrMessage = typeof err?.response?.data?.message === "string" ? err.response.data.message : String(dispatchError);

        const isValidationStatus = status === 400 || status === 422;
        const mentionsReturnRunDetails = typeof dispatchErrMessage === "string" && dispatchErrMessage.toLowerCase().includes("return_run_details");

        if (isValidationStatus && mentionsReturnRunDetails) {
          core.info("Workflow dispatch failed due to unsupported 'return_run_details' parameter; retrying without it for GitHub Enterprise compatibility.");
          response = await githubClient.rest.actions.createWorkflowDispatch({
            owner: repo.owner,
            repo: repo.repo,
            workflow_id: workflowFile,
            ref: ref,
            inputs: inputs,
          });
        } else {
          throw err;
        }
      }

      const runId = response && response.data ? response.data.workflow_run_id : undefined;
      if (runId) {
        core.info(`✓ Successfully dispatched workflow: ${workflowFile} (run ID: ${runId})`);
      } else {
        core.info(`✓ Successfully dispatched workflow: ${workflowFile}`);
      }

      // Record the time of this dispatch for rate limiting
      lastDispatchTime = Date.now();

      return {
        success: true,
        workflow_name: workflowName,
        inputs: inputs,
        run_id: runId,
      };
    } catch (error) {
      const errorMessage = getErrorMessage(error);
      core.error(`Failed to dispatch workflow "${workflowName}": ${errorMessage}`);

      return {
        success: false,
        error: `Failed to dispatch workflow "${workflowName}": ${errorMessage}`,
      };
    }
  };
}

/**
 * @param {string[]|string|undefined} allowedRefsValue
 * @returns {string[]}
 */
function parseAllowedRefPatterns(allowedRefsValue) {
  /** @type {string[]} */
  const refs = [];
  if (Array.isArray(allowedRefsValue)) {
    for (const pattern of allowedRefsValue) {
      if (typeof pattern === "string") {
        const trimmed = pattern.trim();
        if (trimmed) {
          refs.push(normalizeRefPattern(trimmed));
        }
      }
    }
    return refs;
  }
  if (typeof allowedRefsValue === "string") {
    return allowedRefsValue
      .split(",")
      .map(pattern => pattern.trim())
      .filter(Boolean)
      .map(normalizeRefPattern);
  }
  return refs;
}

/**
 * @param {string} refOrBranch
 * @returns {string}
 */
function normalizeRef(refOrBranch) {
  if (refOrBranch.startsWith("refs/")) return refOrBranch;
  if (refOrBranch.startsWith("tags/")) return `refs/${refOrBranch}`;
  return `refs/heads/${refOrBranch}`;
}

/**
 * @param {string} pattern
 * @returns {string}
 */
function normalizeRefPattern(pattern) {
  if (pattern.startsWith("refs/")) return pattern;
  if (pattern.startsWith("tags/")) return `refs/${pattern}`;
  return `refs/heads/${pattern}`;
}

module.exports = { main, buildDispatchAwContext };
