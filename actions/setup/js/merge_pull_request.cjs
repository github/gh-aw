// @ts-check
/// <reference types="@actions/github-script" />

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { isStagedMode, resolveTarget } = require("./safe_output_helpers.cjs");
const { selectLatestRelevantChecks } = require("./check_runs_helpers.cjs");
const { withRetry, isTransientError } = require("./error_recovery.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { resolveNumberFromTemporaryId } = require("./temporary_id.cjs");
const { SAFE_OUTPUT_E001, SAFE_OUTPUT_E099 } = require("./error_codes.cjs");
const MERGEABILITY_PENDING_ERROR = "pull request mergeability is still being computed";
const MERGEABILITY_PENDING_ERROR_E099 = `${SAFE_OUTPUT_E099}: ${MERGEABILITY_PENDING_ERROR}`;

/**
 * @typedef {import('./types/handler-factory').HandlerFactoryFunction} HandlerFactoryFunction
 */

/**
 * @param {string[]} patterns
 * @returns {RegExp[]}
 */
function compilePathGlobs(patterns) {
  return patterns.map(p => globPatternToRegex(p, { pathMode: true, caseSensitive: true }));
}

/**
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @returns {Promise<any>}
 */
async function getPullRequestWithMergeability(githubClient, owner, repo, pullNumber) {
  core.info(`Fetching PR #${pullNumber} in ${owner}/${repo} with mergeability retry`);
  return withRetry(
    async () => {
      const { data } = await githubClient.rest.pulls.get({
        owner,
        repo,
        pull_number: pullNumber,
      });
      if (data && data.mergeable === null) {
        throw new Error(MERGEABILITY_PENDING_ERROR_E099);
      }
      return data;
    },
    {
      maxRetries: 3,
      initialDelayMs: 1000,
      shouldRetry: error => {
        const msg = getErrorMessage(error).toLowerCase();
        return isTransientError(error) || msg === MERGEABILITY_PENDING_ERROR_E099.toLowerCase();
      },
    },
    `fetch pull request #${pullNumber}`
  ).catch(async error => {
    try {
      const fallback = await githubClient.rest.pulls.get({
        owner,
        repo,
        pull_number: pullNumber,
      });
      if (fallback?.data) {
        core.warning(`Mergeability remained unknown after retries for PR #${pullNumber}, continuing with latest state`);
        return fallback.data;
      }
    } catch (fallbackError) {
      throw new Error(`${SAFE_OUTPUT_E099}: Failed to fetch pull request #${pullNumber} after retry and fallback attempts. Retry error: ${getErrorMessage(error)}. Fallback error: ${getErrorMessage(fallbackError)}`, {
        cause: fallbackError,
      });
    }
    throw error;
  });
}

/**
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} pullNumber
 * @returns {Promise<{reviewDecision: string|null, unresolvedThreadCount: number}>}
 */
async function getReviewSummary(githubClient, owner, repo, pullNumber) {
  core.info(`Collecting review summary for PR #${pullNumber}`);
  let unresolvedThreadCount = 0;
  /** @type {any} */
  let reviewDecision = null;
  /** @type {any} */
  let cursor = null;
  let hasNextPage = true;
  let page = 0;
  while (hasNextPage) {
    page++;
    const result = await withRetry(
      async () =>
        githubClient.graphql(
          `
            query($owner: String!, $repo: String!, $number: Int!, $after: String) {
              repository(owner: $owner, name: $repo) {
                pullRequest(number: $number) {
                  reviewDecision
                  reviewThreads(first: 100, after: $after) {
                    pageInfo { hasNextPage endCursor }
                    nodes { isResolved }
                  }
                }
              }
            }
          `,
          { owner, repo, number: pullNumber, after: cursor }
        ),
      {
        maxRetries: 3,
        initialDelayMs: 1000,
        shouldRetry: error => isTransientError(error),
      },
      `fetch review summary GraphQL page ${page} for PR #${pullNumber}`
    );

    const pr = result?.repository?.pullRequest;
    if (!pr) {
      core.warning(`No pull request data returned while reading review summary for PR #${pullNumber}`);
      break;
    }
    reviewDecision = pr.reviewDecision || null;
    const threads = pr.reviewThreads?.nodes || [];
    core.info(`Review page ${page}: ${threads.length} thread(s)`);
    unresolvedThreadCount += threads.filter(t => !t.isResolved).length;
    hasNextPage = pr.reviewThreads?.pageInfo?.hasNextPage === true;
    cursor = pr.reviewThreads?.pageInfo?.endCursor || null;
  }

  core.info(`Review summary: decision=${reviewDecision || "null"}, unresolvedThreads=${unresolvedThreadCount}`);
  return { reviewDecision, unresolvedThreadCount };
}

/**
 * Returns the first open pull request where the given branch is the head (source) branch,
 * or null if no such PR exists.
 *
 * Note: the `head` filter uses `owner:branch` format, which matches only PRs whose head branch
 * lives in the base repository's owner account. Fork-sourced upstream PRs (e.g. an external
 * contributor's fork tracking `release/1.0 → main`) are intentionally excluded; this gate
 * enforces an intra-repository PR-chain model only.
 *
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {string} branch
 * @returns {Promise<{number: number, html_url: string}|null>}
 */
async function getOpenPullRequestForBranch(githubClient, owner, repo, branch) {
  core.info(`Looking up open pull request for head branch "${branch}" in ${owner}/${repo}`);
  const { data: prs } = await withRetry(() =>
    githubClient.rest.pulls.list({
      owner,
      repo,
      state: "open",
      head: `${owner}:${branch}`,
      per_page: 1,
    })
  );
  if (!Array.isArray(prs) || prs.length === 0) {
    core.info(`No open pull request found for head branch "${branch}"`);
    return null;
  }
  core.info(`Found open pull request #${prs[0].number} for head branch "${branch}"`);
  return { number: prs[0].number, html_url: prs[0].html_url };
}

/**
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {string} baseBranch
 * @returns {Promise<{isProtected: boolean, isDefault: boolean, defaultBranch: string|null, requiredChecks: string[]}>}
 */
async function getBranchPolicy(githubClient, owner, repo, baseBranch) {
  const baseBranchValidation = sanitizeBranchName(baseBranch, "target base");
  if (!baseBranchValidation.valid || !baseBranchValidation.value) {
    throw new Error(`${SAFE_OUTPUT_E001}: Invalid target base branch for policy evaluation: ${baseBranchValidation.error} (original: ${JSON.stringify(baseBranch)}, normalized: ${JSON.stringify(baseBranchValidation.normalized || "")})`);
  }
  const sanitizedBaseBranch = baseBranchValidation.value;

  core.info(`Checking target branch policy for ${owner}/${repo}@${sanitizedBaseBranch}`);
  const [{ data: branch }, { data: repository }] = await Promise.all([
    githubClient.rest.repos.getBranch({
      owner,
      repo,
      branch: sanitizedBaseBranch,
    }),
    githubClient.rest.repos.get({
      owner,
      repo,
    }),
  ]);

  const defaultBranchRaw = repository.default_branch;
  const defaultBranchValidation = sanitizeBranchName(defaultBranchRaw, "default");
  const defaultBranch = defaultBranchValidation.valid ? defaultBranchValidation.value : defaultBranchRaw;
  const isDefault = defaultBranch !== null && sanitizedBaseBranch === defaultBranch;
  if (isDefault) {
    core.info(`Target branch ${sanitizedBaseBranch} is the repository default branch`);
  }

  const isProtected = branch?.protected === true;
  if (isProtected) {
    core.info(`Target branch ${sanitizedBaseBranch} is protected`);
    return { isProtected: true, isDefault, defaultBranch, requiredChecks: [] };
  }

  try {
    const { data } = await githubClient.rest.repos.getBranchProtection({
      owner,
      repo,
      branch: sanitizedBaseBranch,
    });
    const contexts = Array.isArray(data?.required_status_checks?.contexts) ? data.required_status_checks.contexts : [];
    const checks = Array.isArray(data?.required_status_checks?.checks) ? data.required_status_checks.checks.map(c => c?.context).filter(Boolean) : [];
    core.info(`Branch protection checks for ${sanitizedBaseBranch}: ${[...new Set([...contexts, ...checks])].join(", ") || "(none)"}`);
    return { isProtected: false, isDefault, defaultBranch, requiredChecks: [...new Set([...contexts, ...checks])] };
  } catch (error) {
    if (error && typeof error === "object" && "status" in error && error.status === 404) {
      core.info(`No branch protection rules found for ${sanitizedBaseBranch}`);
      return { isProtected: false, isDefault, defaultBranch, requiredChecks: [] };
    }
    core.error(`Failed to read branch protection for ${sanitizedBaseBranch}: ${getErrorMessage(error)}`);
    throw error;
  }
}

/**
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {string} headSha
 * @param {string[]} requiredChecks
 * @returns {Promise<{missing: string[], failing: Array<{name: string, status: string, conclusion: string|null}>}>}
 */
async function evaluateRequiredChecks(githubClient, owner, repo, headSha, requiredChecks) {
  core.info(`Evaluating required checks on ${headSha}: ${requiredChecks.join(", ") || "(none)"}`);
  if (requiredChecks.length === 0) {
    return { missing: [], failing: [] };
  }

  const checkRuns = await githubClient.paginate(githubClient.rest.checks.listForRef, {
    owner,
    repo,
    ref: headSha,
    per_page: 100,
  });

  const { relevant } = selectLatestRelevantChecks(checkRuns, { includeList: requiredChecks });
  core.info(`Fetched ${checkRuns.length} check run(s), ${relevant.length} relevant latest check run(s)`);
  const byName = new Map(relevant.map(run => [run.name, run]));
  const missing = [];
  const failing = [];

  for (const checkName of requiredChecks) {
    const run = byName.get(checkName);
    if (!run) {
      core.warning(`Required check missing: ${checkName}`);
      missing.push(checkName);
      continue;
    }
    if (run.status !== "completed" || run.conclusion !== "success") {
      core.warning(`Required check not passing: ${checkName} status=${run.status} conclusion=${run.conclusion || "null"}`);
      failing.push({ name: checkName, status: run.status, conclusion: run.conclusion || null });
    }
  }

  return { missing, failing };
}

/**
 * @returns {number|undefined}
 */
function resolveContextPullNumber() {
  if (context.payload?.pull_request?.number) {
    return context.payload.pull_request.number;
  }
  if (context.payload?.issue?.pull_request && context.payload?.issue?.number) {
    return context.payload.issue.number;
  }
  return undefined;
}

/**
 * @param {string|undefined|null} branchName
 * @param {string} branchRole
 * @returns {{valid: boolean, value?: string, error?: string, normalized?: string}}
 */
function sanitizeBranchName(branchName, branchRole) {
  if (typeof branchName !== "string" || branchName.trim() === "") {
    return { valid: false, error: `${branchRole} branch is missing` };
  }

  const normalized = normalizeBranchName(branchName);
  if (typeof normalized !== "string" || normalized.trim() === "") {
    return {
      valid: false,
      error: `${branchRole} branch is invalid after sanitization`,
      normalized: String(normalized || ""),
    };
  }

  if (normalized !== branchName) {
    return {
      valid: false,
      error: `${branchRole} branch contains invalid characters`,
      normalized,
    };
  }

  return { valid: true, value: normalized };
}

/**
 * @param {string[]} labels
 * @returns {string[]}
 */
function findMissingRequiredLabels(labels, requiredLabels) {
  return requiredLabels.filter(label => !labels.includes(label));
}

/**
 * @param {any} message
 *   Message object containing pull_request_number (optional)
 * @param {any} [resolvedTemporaryIds]
 *   Optional map of resolved temporary IDs from prior safe-output operations
 * @returns {{success: true, pullNumber: number, fromTemporaryId: boolean} | {success: false, error: string}}
 */
function resolvePullRequestNumber(message, resolvedTemporaryIds) {
  const pullNumberRaw = message?.pull_request_number;
  if (pullNumberRaw !== undefined && pullNumberRaw !== null) {
    const resolution = resolveNumberFromTemporaryId(pullNumberRaw, resolvedTemporaryIds);
    if (resolution.errorMessage) {
      return { success: false, error: resolution.errorMessage };
    }
    if (resolution.resolved === null) {
      return { success: false, error: "Failed to resolve pull_request_number" };
    }
    return { success: true, pullNumber: resolution.resolved, fromTemporaryId: resolution.wasTemporaryId };
  }

  const contextPullNumber = resolveContextPullNumber();
  if (!contextPullNumber) {
    return { success: false, error: "pull_request_number is required for merge_pull_request" };
  }
  return { success: true, pullNumber: contextPullNumber, fromTemporaryId: false };
}

/**
 * Handler factory for merge_pull_request.
 * @type {HandlerFactoryFunction}
 */
async function main(config = {}) {
  const githubClient = await createAuthenticatedGitHubClient(config);
  const isStaged = isStagedMode(config);
  const { defaultTargetRepo, allowedRepos } = resolveTargetRepoConfig(config);
  const targetConfig = config.target || "triggering";
  const maxCount = Number(config.max || 1);
  const requiredLabels = Array.isArray(config.required_labels) ? config.required_labels : [];
  const requiredTitlePrefix = config.required_title_prefix || "";
  const allowedBranches = Array.isArray(config.allowed_branches) ? config.allowed_branches : [];

  const allowedBranchPatterns = compilePathGlobs(allowedBranches);
  core.info(
    `merge_pull_request handler configured: max=${maxCount}, requiredLabels=${requiredLabels.length}, requiredTitlePrefix=${requiredTitlePrefix ? JSON.stringify(requiredTitlePrefix) : "none"}, allowedBranches=${allowedBranches.length}, staged=${isStaged}`
  );

  let processedCount = 0;

  return async function handleMergePullRequest(message, resolvedTemporaryIds) {
    core.info(`Processing merge_pull_request message: ${JSON.stringify({ pull_request_number: message?.pull_request_number, repo: message?.repo, merge_method: message?.merge_method })}`);
    if (processedCount >= maxCount) {
      core.warning(`Skipping merge_pull_request: max count of ${maxCount} reached`);
      return { success: false, error: `Max count of ${maxCount} reached` };
    }
    processedCount++;

    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "merge pull request");
    if (!repoResult.success) {
      core.error(`Repository validation failed: ${repoResult.error}`);
      return { success: false, error: repoResult.error };
    }
    const { owner, repo } = repoResult.repoParts;
    core.info(`Resolved target repository: ${owner}/${repo}`);

    let targetItem = message;
    if (targetConfig === "*" && message?.pull_request_number != null) {
      const pullNumberResolution = resolvePullRequestNumber(message, resolvedTemporaryIds);
      if (!pullNumberResolution.success) {
        core.error(pullNumberResolution.error);
        return { success: false, error: pullNumberResolution.error };
      }
      targetItem = { ...message, pull_request_number: pullNumberResolution.pullNumber };
      if (pullNumberResolution.fromTemporaryId) {
        core.info(`Resolved temporary ID '${String(message.pull_request_number)}' to pull request #${pullNumberResolution.pullNumber}`);
      }
    }

    const targetResult = resolveTarget({
      targetConfig,
      item: targetItem,
      context,
      itemType: "merge_pull_request",
    });
    if (!targetResult.success) {
      core.error(targetResult.error);
      return { success: false, error: targetResult.error };
    }
    const pullNumber = targetResult.number;
    core.info(`Target PR number: ${pullNumber}`);

    /** @type {Array<{code: string, message: string, details?: any}>} */
    const failureReasons = [];

    try {
      const pr = await getPullRequestWithMergeability(githubClient, owner, repo, pullNumber);
      if (!pr) {
        core.error(`Pull request #${pullNumber} not found`);
        return { success: false, error: `Pull request #${pullNumber} not found` };
      }
      const sourceBranchValidation = sanitizeBranchName(pr.head?.ref, "source");
      if (!sourceBranchValidation.valid) {
        failureReasons.push({
          code: "source_branch_invalid",
          message: sourceBranchValidation.error || "source branch is invalid",
          details: { source_branch: pr.head?.ref, normalized: sourceBranchValidation.normalized || null },
        });
      }
      const sourceBranch = sourceBranchValidation.valid ? sourceBranchValidation.value : null;

      const baseBranchValidation = sanitizeBranchName(pr.base?.ref, "target base");
      if (!baseBranchValidation.valid) {
        failureReasons.push({
          code: "target_base_branch_invalid",
          message: baseBranchValidation.error || "target base branch is invalid",
          details: { base_branch: pr.base?.ref, normalized: baseBranchValidation.normalized || null },
        });
      }
      const baseBranch = baseBranchValidation.valid ? baseBranchValidation.value : null;

      core.info(
        `PR state: merged=${pr.merged}, draft=${pr.draft}, mergeable=${pr.mergeable}, mergeable_state=${pr.mergeable_state || "unknown"}, head=${JSON.stringify(sourceBranch || pr.head?.ref || null)}, base=${JSON.stringify(baseBranch || pr.base?.ref || null)}`
      );
      if (pr.merged) {
        core.info(`PR #${pullNumber} is already merged, returning idempotent success`);
        return {
          success: true,
          merged: true,
          alreadyMerged: true,
          pull_request_number: pr.number,
          pull_request_url: pr.html_url,
          checks_evaluated: [],
        };
      }

      if (pr.draft) {
        failureReasons.push({ code: "pr_is_draft", message: "Pull request is still in draft state" });
      }
      if (pr.mergeable === false || pr.mergeable_state === "dirty") {
        failureReasons.push({ code: "merge_conflicts", message: "Pull request has unresolved merge conflicts" });
      }
      if (pr.mergeable !== true) {
        failureReasons.push({ code: "not_mergeable", message: `Pull request is not mergeable (mergeable=${String(pr.mergeable)}, state=${pr.mergeable_state || "unknown"})` });
      }

      const labels = (pr.labels || []).map(l => l.name).filter(Boolean);
      core.info(`PR labels (${labels.length}): ${labels.join(", ") || "(none)"}`);
      const missingRequiredLabels = findMissingRequiredLabels(labels, requiredLabels);
      if (missingRequiredLabels.length > 0) {
        failureReasons.push({
          code: "missing_required_labels",
          message: "Required labels are missing",
          details: { missing: missingRequiredLabels, present: labels },
        });
      }
      if (requiredTitlePrefix && !pr.title?.startsWith(requiredTitlePrefix)) {
        failureReasons.push({
          code: "title_prefix_mismatch",
          message: `PR title does not start with required prefix "${requiredTitlePrefix}"`,
          details: { required_prefix: requiredTitlePrefix, actual_title: pr.title },
        });
      }

      if (allowedBranchPatterns.length > 0 && sourceBranch && !allowedBranchPatterns.some(re => re.test(sourceBranch))) {
        failureReasons.push({
          code: "branch_not_allowed",
          message: `Source branch "${sourceBranch}" does not match allowed-branches`,
          details: { source_branch: sourceBranch, patterns: allowedBranches },
        });
      }
      if (allowedBranchPatterns.length > 0) {
        core.info(`Allowed branch patterns: ${allowedBranches.join(", ")}`);
      }

      /** @type {{isProtected: boolean, isDefault: boolean, defaultBranch: string|null, requiredChecks: string[]}} */
      let branchPolicy = { isProtected: false, isDefault: false, defaultBranch: null, requiredChecks: [] };
      if (baseBranch) {
        branchPolicy = await getBranchPolicy(githubClient, owner, repo, baseBranch);
        if (branchPolicy.isProtected) {
          failureReasons.push({
            code: "target_branch_protected",
            message: `Target branch "${baseBranch}" is protected`,
          });
        }
        if (branchPolicy.isDefault) {
          failureReasons.push({
            code: "target_branch_default",
            message: `Target branch "${baseBranch}" is the repository default branch`,
            details: { default_branch: branchPolicy.defaultBranch },
          });
        }
        if (!branchPolicy.isProtected && !branchPolicy.isDefault) {
          const upstreamPR = await getOpenPullRequestForBranch(githubClient, owner, repo, baseBranch);
          if (!upstreamPR) {
            failureReasons.push({
              code: "target_branch_has_no_open_pr",
              message: `Target branch "${baseBranch}" is not the head branch of any open pull request`,
            });
          }
        }
      }

      const checkSummary = await evaluateRequiredChecks(githubClient, owner, repo, pr.head.sha, branchPolicy.requiredChecks);
      core.info(`Required check summary: missing=${checkSummary.missing.length}, failing=${checkSummary.failing.length}`);
      if (checkSummary.missing.length > 0) {
        failureReasons.push({
          code: "required_checks_missing",
          message: "Required status checks are not completed",
          details: { missing: checkSummary.missing },
        });
      }
      if (checkSummary.failing.length > 0) {
        failureReasons.push({
          code: "required_checks_failing",
          message: "Required status checks are not passing",
          details: { failing: checkSummary.failing },
        });
      }

      if ((pr.requested_reviewers || []).length > 0 || (pr.requested_teams || []).length > 0) {
        failureReasons.push({
          code: "pending_reviewers",
          message: "All assigned reviewers have not approved yet",
          details: {
            requested_reviewers: (pr.requested_reviewers || []).map(r => r.login),
            requested_teams: (pr.requested_teams || []).map(t => t.slug),
          },
        });
      }

      const reviewSummary = await getReviewSummary(githubClient, owner, repo, pullNumber);
      if (reviewSummary.reviewDecision === "CHANGES_REQUESTED" || reviewSummary.reviewDecision === "REVIEW_REQUIRED") {
        failureReasons.push({
          code: "blocking_review_state",
          message: `Blocking review state remains active (${reviewSummary.reviewDecision})`,
        });
      }
      if (reviewSummary.unresolvedThreadCount > 0) {
        failureReasons.push({
          code: "unresolved_review_threads",
          message: "Pull request has unresolved review threads",
          details: { unresolved_count: reviewSummary.unresolvedThreadCount },
        });
      }

      if (failureReasons.length > 0) {
        core.warning(`merge_pull_request blocked with ${failureReasons.length} gate failure(s): ${failureReasons.map(r => r.code).join(", ")}`);
        return {
          success: false,
          error: "merge_pull_request gate checks failed",
          failure_reasons: failureReasons,
          checks_evaluated: branchPolicy.requiredChecks,
        };
      }

      if (isStaged) {
        core.info(`Staged mode: merge for PR #${pullNumber} not executed`);
        return {
          success: true,
          staged: true,
          merged: false,
          pull_request_number: pr.number,
          pull_request_url: pr.html_url,
          checks_evaluated: branchPolicy.requiredChecks,
        };
      }

      const mergeResponse = await githubClient.rest.pulls.merge({
        owner,
        repo,
        pull_number: pullNumber,
        merge_method: message.merge_method || "merge",
        commit_title: message.commit_title,
        commit_message: message.commit_message,
      });

      if (mergeResponse.data?.merged !== true) {
        core.error(`Merge API returned merged=false for PR #${pullNumber}: ${mergeResponse.data?.message || "no message"}`);
        return {
          success: false,
          error: mergeResponse.data?.message || "Merge API returned merged=false",
          failure_reasons: [{ code: "merge_not_completed", message: mergeResponse.data?.message || "Merge was not completed" }],
          checks_evaluated: branchPolicy.requiredChecks,
        };
      }

      return {
        success: true,
        merged: true,
        pull_request_number: pr.number,
        pull_request_url: pr.html_url,
        sha: mergeResponse.data?.sha,
        message: mergeResponse.data?.message,
        checks_evaluated: branchPolicy.requiredChecks,
      };
    } catch (error) {
      core.error(`merge_pull_request failed for PR #${pullNumber}: ${getErrorMessage(error)}`);
      return {
        success: false,
        error: getErrorMessage(error),
        failure_reasons: [{ code: "merge_operation_error", message: getErrorMessage(error) }],
      };
    }
  };
}

module.exports = {
  main,
  __testables: {
    compilePathGlobs,
    resolveContextPullNumber,
    sanitizeBranchName,
    getBranchPolicy,
    findMissingRequiredLabels,
    resolvePullRequestNumber,
    getOpenPullRequestForBranch,
  },
};
