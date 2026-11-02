// @ts-check
/// <reference types="@actions/github-script" />

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { isDeepStrictEqual } = require("node:util");
const { getErrorMessage } = require("./error_helpers.cjs");
const { resolveTargetRepoConfig, resolveAndValidateRepo } = require("./repo_helpers.cjs");
const { globPatternToRegex } = require("./glob_pattern_helpers.cjs");
const { isStagedMode, resolveTarget } = require("./safe_output_helpers.cjs");
const { selectLatestRelevantChecks, isDeploymentCheck } = require("./check_runs_helpers.cjs");
const { withRetry, isTransientError } = require("./error_recovery.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { resolveNumberFromTemporaryId } = require("./temporary_id.cjs");
const { SAFE_OUTPUT_E001, SAFE_OUTPUT_E007, SAFE_OUTPUT_E099 } = require("./error_codes.cjs");
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
 * @param {string} headSha
 * @returns {Promise<{reviewDecision: string|null, unresolvedThreadCount: number}>}
 */
async function getReviewSummary(githubClient, owner, repo, pullNumber, headSha) {
  core.info(`Collecting review summary for PR #${pullNumber}`);
  let unresolvedThreadCount = 0;
  /** @type {any} */
  let reviewDecision = null;
  /** @type {any} */
  let cursor = null;
  let hasNextPage = true;
  let page = 0;
  const seenCursors = new Set();
  while (hasNextPage) {
    page++;
    const result = await withRetry(
      async () =>
        githubClient.graphql(
          `
            query($owner: String!, $repo: String!, $number: Int!, $after: String) {
              repository(owner: $owner, name: $repo) {
                pullRequest(number: $number) {
                  headRefOid
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
    const connection = pr?.reviewThreads;
    if (
      !pr ||
      pr.headRefOid !== headSha ||
      ![null, "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED"].includes(pr.reviewDecision) ||
      !Array.isArray(connection?.nodes) ||
      connection.nodes.some(t => typeof t?.isResolved !== "boolean") ||
      typeof connection.pageInfo?.hasNextPage !== "boolean" ||
      !(connection.pageInfo.endCursor === null || typeof connection.pageInfo.endCursor === "string")
    ) {
      throw new Error(`${SAFE_OUTPUT_E007}: Incomplete or stale review evidence for PR #${pullNumber} (page ${page})`);
    }
    if (page > 1 && reviewDecision !== pr.reviewDecision) {
      throw new Error(`${SAFE_OUTPUT_E007}: Review decision changed during pagination for PR #${pullNumber}`);
    }
    reviewDecision = pr.reviewDecision;
    const threads = connection.nodes;
    core.info(`Review page ${page}: ${threads.length} thread(s)`);
    unresolvedThreadCount += threads.filter(t => !t.isResolved).length;
    hasNextPage = connection.pageInfo.hasNextPage;
    cursor = connection.pageInfo.endCursor;
    if (hasNextPage && (threads.length === 0 || typeof cursor !== "string" || !cursor || seenCursors.has(cursor))) {
      throw new Error(`${SAFE_OUTPUT_E007}: Invalid review thread pagination for PR #${pullNumber} (page ${page})`);
    }
    seenCursors.add(cursor);
  }

  core.info(`Review summary: decision=${reviewDecision || "null"}, unresolvedThreads=${unresolvedThreadCount}`);
  core.debug(`merge_pull_request: ${JSON.stringify({ event: "review_evidence", pages: page, unresolved_threads: unresolvedThreadCount, decision_present: reviewDecision !== null })}`);
  return { reviewDecision, unresolvedThreadCount };
}

/**
 * @param {any} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {any} pr
 * @param {string[]} requiredApprovers
 */
async function getApprovalEvidence(githubClient, owner, repo, pr, requiredApprovers) {
  const identity = requiredApprovers.length > 0 ? await githubClient.graphql(`query { viewer { login } }`) : null;
  if (requiredApprovers.length > 0 && (!identity?.viewer?.login || !pr.user?.login)) {
    throw new Error(`${SAFE_OUTPUT_E007}: Missing merge identity or PR author for independent approval evaluation`);
  }
  const reviews = await githubClient.paginate(githubClient.rest.pulls.listReviews, {
    owner,
    repo,
    pull_number: pr.number,
    per_page: 100,
  });
  if (!Array.isArray(reviews) || reviews.some(r => !r?.user?.login || !Number.isInteger(r.id) || !["APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED", "PENDING"].includes(r.state))) {
    throw new Error(`${SAFE_OUTPUT_E007}: Incomplete approval evidence for PR #${pr.number}`);
  }
  const decisiveReviews = reviews
    .filter(review => review.state !== "COMMENTED" && review.state !== "PENDING")
    .map(review => {
      const submittedAt = Date.parse(review.submitted_at);
      if (
        typeof review.submitted_at !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(review.submitted_at) ||
        !Number.isFinite(submittedAt) ||
        new Date(submittedAt).toISOString() !== review.submitted_at.replace(/Z$/, ".000Z")
      ) {
        throw new Error(`${SAFE_OUTPUT_E007}: Missing or invalid review submission time for PR #${pr.number}`);
      }
      return { review, submittedAt };
    })
    .sort((a, b) => a.submittedAt - b.submittedAt);
  const latest = new Map();
  for (const { review, submittedAt } of decisiveReviews) {
    const login = review.user.login.toLowerCase();
    if (latest.has(login) && Date.parse(latest.get(login).submitted_at) === submittedAt) {
      throw new Error(`${SAFE_OUTPUT_E007}: Ambiguous review submission order for PR #${pr.number}`);
    }
    // Review IDs reflect creation, so a pending review can be submitted after a higher-ID review.
    latest.set(login, review);
  }
  const blockingReviews = [...latest.values()].filter(review => review.state === "CHANGES_REQUESTED").map(review => ({ login: review.user.login, review_id: review.id, commit_id: review.commit_id }));
  const allowed = requiredApprovers.map(login => login.toLowerCase());
  const approvals = [...latest.values()]
    .filter(
      review =>
        review.state === "APPROVED" &&
        review.commit_id === pr.head.sha &&
        review.user.type === "User" &&
        allowed.includes(review.user.login.toLowerCase()) &&
        review.user.login.toLowerCase() !== pr.user.login.toLowerCase() &&
        review.user.login.toLowerCase() !== identity.viewer.login.toLowerCase()
    )
    .map(review => ({ login: review.user.login, review_id: review.id, commit_id: review.commit_id }));
  core.debug(`merge_pull_request: ${JSON.stringify({ event: "approval_evidence", reviews: reviews.length, eligible_approvals: approvals.length })}`);
  return { approvals, blocking_reviews: blockingReviews };
}

/**
 * Stable merge policy inputs; asynchronous mergeability is checked separately.
 * @param {any} pr
 */
function policySnapshot(pr) {
  return {
    state: pr.state,
    merged: pr.merged,
    draft: pr.draft,
    head: [pr.head?.sha, pr.head?.ref, pr.head?.repo?.full_name],
    base: [pr.base?.sha, pr.base?.ref, pr.base?.repo?.full_name],
    author: pr.user?.login,
    title: pr.title,
    labels: (pr.labels || []).map(label => label.name).sort(),
    reviewers: (pr.requested_reviewers || []).map(user => user.login).sort(),
    teams: (pr.requested_teams || []).map(team => team.slug).sort(),
  };
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
  if (!Number.isInteger(prs[0]?.number) || prs[0].number <= 0) {
    throw new Error(`${SAFE_OUTPUT_E007}: Incomplete upstream pull request evidence for branch "${branch}"`);
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

  if (typeof branch?.protected !== "boolean" || typeof repository?.default_branch !== "string") {
    throw new Error(`${SAFE_OUTPUT_E007}: Incomplete target branch policy evidence`);
  }
  const defaultBranchRaw = repository.default_branch;
  const defaultBranchValidation = sanitizeBranchName(defaultBranchRaw, "default");
  if (!defaultBranchValidation.valid) {
    throw new Error(`${SAFE_OUTPUT_E007}: Invalid repository default branch in policy evidence`);
  }
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
 * @param {Record<string, number>} [requiredCheckApps]
 * @returns {Promise<{missing: string[], failing: Array<{name: string, status: string, conclusion: string|null}>, passing: Array<{name: string, check_run_id: number, head_sha: string, app_id: number|null}>}>}
 */
async function evaluateRequiredChecks(githubClient, owner, repo, headSha, requiredChecks, requiredCheckApps = {}) {
  core.info(`Evaluating required checks on ${headSha}: ${requiredChecks.join(", ") || "(none)"}`);
  if (requiredChecks.length === 0) {
    return { missing: [], failing: [], passing: [] };
  }

  const checkRuns = await githubClient.paginate(githubClient.rest.checks.listForRef, {
    owner,
    repo,
    ref: headSha,
    per_page: 100,
  });

  // Names identify checks, not trusted producers; do not choose between competing apps.
  const producersByName = new Map();
  for (const run of checkRuns) {
    if (!requiredChecks.includes(run.name) || isDeploymentCheck(run)) {
      continue;
    }
    if (!Number.isInteger(run.app?.id) || run.app.id <= 0) {
      throw new Error(`${SAFE_OUTPUT_E007}: Missing check producer evidence for required check "${run.name}"`);
    }
    if (Object.hasOwn(requiredCheckApps, run.name) && run.app.id !== requiredCheckApps[run.name]) {
      throw new Error(`${SAFE_OUTPUT_E007}: Unexpected check producer for required check "${run.name}"`);
    }
    const appId = producersByName.get(run.name);
    if (appId !== undefined && appId !== run.app.id) {
      throw new Error(`${SAFE_OUTPUT_E007}: Ambiguous check producers for required check "${run.name}"`);
    }
    producersByName.set(run.name, run.app.id);
  }

  const { relevant } = selectLatestRelevantChecks(checkRuns, { includeList: requiredChecks });
  core.info(`Fetched ${checkRuns.length} check run(s), ${relevant.length} relevant latest check run(s)`);
  const byName = new Map(relevant.map(run => [run.name, run]));
  // A queued rerun may have no started_at; it must supersede an older success.
  for (const run of checkRuns) {
    const selected = byName.get(run.name);
    if (selected && run.id > selected.id && !isDeploymentCheck(run)) {
      byName.set(run.name, run);
    }
  }
  const missing = [];
  const failing = [];
  const passing = [];

  for (const checkName of requiredChecks) {
    const run = byName.get(checkName);
    if (!run) {
      core.warning(`Required check missing: ${checkName}`);
      missing.push(checkName);
      continue;
    }
    if (run.status !== "completed" || run.conclusion !== "success" || run.head_sha !== headSha) {
      core.warning(`Required check not passing: ${checkName} status=${run.status} conclusion=${run.conclusion || "null"}`);
      failing.push({ name: checkName, status: run.status, conclusion: run.conclusion || null });
    } else {
      passing.push({ name: checkName, check_run_id: run.id, head_sha: run.head_sha, app_id: run.app?.id || null });
    }
  }

  return { missing, failing, passing };
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
  const requiredChecks = Array.isArray(config.required_checks) ? config.required_checks : [];
  const requiredCheckApps = config.required_check_apps || {};
  const requiredApprovers = Array.isArray(config.required_approvers) ? config.required_approvers : [];

  const allowedBranchPatterns = compilePathGlobs(allowedBranches);
  core.info(
    `merge_pull_request handler configured: max=${maxCount}, requiredLabels=${requiredLabels.length}, requiredTitlePrefix=${requiredTitlePrefix ? JSON.stringify(requiredTitlePrefix) : "none"}, allowedBranches=${allowedBranches.length}, staged=${isStaged}`
  );
  core.debug(
    `merge_pull_request: ${JSON.stringify({ event: "configured", staged: isStaged, required_checks: requiredChecks.length, required_approvers: requiredApprovers.length, required_labels: requiredLabels.length, allowed_branches: allowedBranches.length })}`
  );

  let processedCount = 0;

  return async function handleMergePullRequest(message, resolvedTemporaryIds) {
    core.info(`Processing merge_pull_request message: ${JSON.stringify({ pull_request_number: message?.pull_request_number, repo: message?.repo, merge_method: message?.merge_method })}`);
    if (processedCount >= maxCount) {
      core.debug('merge_pull_request: {"event":"limit_reached"}');
      core.warning(`Skipping merge_pull_request: max count of ${maxCount} reached`);
      return { success: false, data_trust: "untrusted", staged: isStaged, error: `Max count of ${maxCount} reached` };
    }
    processedCount++;

    const repoResult = resolveAndValidateRepo(message, defaultTargetRepo, allowedRepos, "merge pull request");
    if (!repoResult.success) {
      core.error(`Repository validation failed: ${repoResult.error}`);
      return { success: false, data_trust: "untrusted", staged: isStaged, error: repoResult.error };
    }
    const { owner, repo } = repoResult.repoParts;
    core.info(`Resolved target repository: ${owner}/${repo}`);

    let targetItem = message;
    if (targetConfig === "*" && message?.pull_request_number != null) {
      const pullNumberResolution = resolvePullRequestNumber(message, resolvedTemporaryIds);
      if (!pullNumberResolution.success) {
        core.error(pullNumberResolution.error);
        return { success: false, data_trust: "untrusted", staged: isStaged, error: pullNumberResolution.error };
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
      return { success: false, data_trust: "untrusted", staged: isStaged, error: targetResult.error };
    }
    const pullNumber = targetResult.number;
    core.info(`Target PR number: ${pullNumber}`);

    /** @type {Array<{code: string, message: string, details?: any}>} */
    const failureReasons = [];
    /** @type {Record<string, any>} */
    const evidence = { data_trust: "untrusted", repository: `${owner}/${repo}`, pull_request_number: pullNumber, evaluated_at: new Date().toISOString() };
    let phase = "pull_request";

    try {
      const pr = await getPullRequestWithMergeability(githubClient, owner, repo, pullNumber);
      if (!pr) {
        throw new Error(`${SAFE_OUTPUT_E007}: Pull request #${pullNumber} not found`);
      }
      evidence.head_sha = pr.head?.sha;
      evidence.base_branch = pr.base?.ref;
      evidence.base_sha = pr.base?.sha;
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
        core.debug('merge_pull_request: {"event":"already_merged"}');
        core.info(`PR #${pullNumber} is already merged, returning idempotent success`);
        return {
          success: true,
          data_trust: "untrusted",
          merged: true,
          alreadyMerged: true,
          pull_request_number: pr.number,
          pull_request_url: pr.html_url,
          checks_evaluated: [],
        };
      }
      if (typeof pr.head?.sha !== "string" || !pr.head.sha) {
        throw new Error(`${SAFE_OUTPUT_E007}: Missing evaluated pull request head SHA`);
      }
      const evaluatedSnapshot = policySnapshot(pr);
      evidence.eligibility = {
        state: pr.state,
        draft: pr.draft,
        mergeable: pr.mergeable,
        mergeable_state: pr.mergeable_state,
        requested_reviewers: (pr.requested_reviewers || []).map(user => user.login),
        requested_teams: (pr.requested_teams || []).map(team => team.slug),
      };

      if (pr.draft) {
        failureReasons.push({ code: "pr_is_draft", message: "Pull request is still in draft state" });
      }
      if (pr.state !== "open") {
        failureReasons.push({ code: "pr_not_open", message: "Pull request is not open" });
      }
      if (pr.mergeable === false || pr.mergeable_state === "dirty") {
        failureReasons.push({ code: "merge_conflicts", message: "Pull request has unresolved merge conflicts" });
      }
      if (pr.mergeable !== true) {
        failureReasons.push({ code: "not_mergeable", message: `Pull request is not mergeable (mergeable=${String(pr.mergeable)}, state=${pr.mergeable_state || "unknown"})` });
      }

      const labels = (pr.labels || []).map(l => l.name).filter(Boolean);
      core.info(`PR labels: ${labels.length}`);
      const missingRequiredLabels = findMissingRequiredLabels(labels, requiredLabels);
      evidence.labels = { required: requiredLabels, present: labels };
      evidence.source_branch = sourceBranch;
      evidence.allowed_branches = allowedBranches;
      evidence.required_title_prefix = requiredTitlePrefix;
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
        core.info(`Allowed branch patterns: ${allowedBranches.length}`);
      }

      /** @type {{isProtected: boolean, isDefault: boolean, defaultBranch: string|null, requiredChecks: string[]}} */
      let branchPolicy = { isProtected: false, isDefault: false, defaultBranch: null, requiredChecks: [] };
      phase = "branch_policy";
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
          evidence.upstream_pull_request = upstreamPR?.number || null;
          if (!upstreamPR) {
            failureReasons.push({
              code: "target_branch_has_no_open_pr",
              message: `Target branch "${baseBranch}" is not the head branch of any open pull request`,
            });
          }
        }
      }

      branchPolicy.requiredChecks = [...new Set([...branchPolicy.requiredChecks, ...requiredChecks])].sort();
      evidence.branch_policy = branchPolicy;
      phase = "required_checks";
      const checkSummary = await evaluateRequiredChecks(githubClient, owner, repo, pr.head.sha, branchPolicy.requiredChecks, requiredCheckApps);
      core.debug(
        `merge_pull_request: ${JSON.stringify({ event: "check_evidence", required: branchPolicy.requiredChecks.length, passing: checkSummary.passing.length, missing: checkSummary.missing.length, failing: checkSummary.failing.length })}`
      );
      evidence.checks = { required: branchPolicy.requiredChecks, required_apps: requiredCheckApps, ...checkSummary };
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

      phase = "review_evidence";
      const reviewSummary = await getReviewSummary(githubClient, owner, repo, pullNumber, pr.head.sha);
      evidence.reviews = reviewSummary;
      phase = "approval_evidence";
      const approvalEvidence = await getApprovalEvidence(githubClient, owner, repo, pr, requiredApprovers);
      evidence.approval_policy = { required_approvers: requiredApprovers, ...approvalEvidence };
      if (approvalEvidence.blocking_reviews.length > 0) {
        failureReasons.push({
          code: "blocking_individual_review",
          message: "An effective changes-requested review remains active",
        });
      }
      if (requiredApprovers.length > 0 && approvalEvidence.approvals.length === 0) {
        failureReasons.push({
          code: "required_approval_missing",
          message: "An independent allowlisted human must approve the evaluated head revision",
        });
      }
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
        core.debug(`merge_pull_request: ${JSON.stringify({ event: "gates_blocked", failures: failureReasons.length })}`);
        core.warning(`merge_pull_request blocked with ${failureReasons.length} gate failure(s): ${failureReasons.map(r => r.code).join(", ")}`);
        return {
          success: false,
          data_trust: "untrusted",
          staged: isStaged,
          error: "merge_pull_request gate checks failed",
          failure_reasons: failureReasons,
          checks_evaluated: branchPolicy.requiredChecks,
          decision_evidence: evidence,
        };
      }

      phase = "revalidation";
      core.debug('merge_pull_request: {"event":"revalidation_started"}');
      if (!baseBranch) {
        throw new Error(`${SAFE_OUTPUT_E007}: Missing target base branch for final policy evaluation`);
      }
      const currentPR = await getPullRequestWithMergeability(githubClient, owner, repo, pullNumber);
      if (!currentPR || !isDeepStrictEqual(policySnapshot(currentPR), evaluatedSnapshot)) {
        throw new Error(`${SAFE_OUTPUT_E007}: Pull request policy state changed after evaluation; fresh evaluation required`);
      }
      if (currentPR.mergeable !== true || currentPR.mergeable_state === "dirty") {
        throw new Error(`${SAFE_OUTPUT_E007}: Pull request mergeability is not confirmed or has unresolved conflicts after revalidation`);
      }
      const currentPolicy = await getBranchPolicy(githubClient, owner, repo, baseBranch);
      currentPolicy.requiredChecks = [...new Set([...currentPolicy.requiredChecks, ...requiredChecks])].sort();
      const upstreamPR = await getOpenPullRequestForBranch(githubClient, owner, repo, baseBranch);
      const currentReviews = await getReviewSummary(githubClient, owner, repo, pullNumber, pr.head.sha);
      const currentApprovals = await getApprovalEvidence(githubClient, owner, repo, currentPR, requiredApprovers);
      const currentChecks = await evaluateRequiredChecks(githubClient, owner, repo, pr.head.sha, branchPolicy.requiredChecks, requiredCheckApps);
      evidence.revalidation = {
        branch_policy: currentPolicy,
        upstream_pull_request: upstreamPR?.number || null,
        reviews: currentReviews,
        approval_policy: currentApprovals,
        checks: currentChecks,
        evaluated_at: new Date().toISOString(),
      };
      if (
        !isDeepStrictEqual(currentPolicy, branchPolicy) ||
        !upstreamPR ||
        !isDeepStrictEqual(currentReviews, reviewSummary) ||
        !isDeepStrictEqual(currentApprovals, approvalEvidence) ||
        currentChecks.missing.length > 0 ||
        currentChecks.failing.length > 0
      ) {
        throw new Error(`${SAFE_OUTPUT_E007}: Merge policy evidence changed after evaluation; fresh evaluation required`);
      }
      evidence.upstream_pull_request = upstreamPR.number;
      evidence.checks = { required: branchPolicy.requiredChecks, required_apps: requiredCheckApps, ...currentChecks };
      evidence.revalidated_at = new Date().toISOString();
      core.debug('merge_pull_request: {"event":"revalidation_passed"}');

      if (isStaged) {
        core.debug('merge_pull_request: {"event":"staged_preview"}');
        core.info(`Staged mode: merge for PR #${pullNumber} not executed`);
        return {
          success: true,
          data_trust: "untrusted",
          staged: true,
          merged: false,
          pull_request_number: pr.number,
          pull_request_url: pr.html_url,
          checks_evaluated: branchPolicy.requiredChecks,
          decision_evidence: evidence,
        };
      }

      phase = "merge";
      core.debug('merge_pull_request: {"event":"merge_started","head_bound":true}');
      const mergeResponse = await githubClient.rest.pulls.merge({
        owner,
        repo,
        pull_number: pullNumber,
        sha: pr.head.sha,
        merge_method: message.merge_method || "merge",
        commit_title: message.commit_title,
        commit_message: message.commit_message,
      });
      core.debug(`merge_pull_request: ${JSON.stringify({ event: "merge_completed", merged: mergeResponse.data?.merged === true })}`);

      if (mergeResponse.data?.merged !== true) {
        core.error(`Merge API returned merged=false for PR #${pullNumber}`);
        return {
          success: false,
          data_trust: "untrusted",
          staged: isStaged,
          error: mergeResponse.data?.message || "Merge API returned merged=false",
          failure_reasons: [{ code: "merge_not_completed", message: mergeResponse.data?.message || "Merge was not completed" }],
          checks_evaluated: branchPolicy.requiredChecks,
          decision_evidence: evidence,
        };
      }

      return {
        success: true,
        data_trust: "untrusted",
        merged: true,
        pull_request_number: pr.number,
        pull_request_url: pr.html_url,
        sha: mergeResponse.data?.sha,
        message: mergeResponse.data?.message,
        checks_evaluated: branchPolicy.requiredChecks,
        decision_evidence: evidence,
      };
    } catch (error) {
      core.debug(`merge_pull_request: ${JSON.stringify({ event: "operation_failed", phase })}`);
      core.error(`merge_pull_request failed for PR #${pullNumber} during ${phase}`);
      evidence.failure_phase = phase;
      return {
        success: false,
        data_trust: "untrusted",
        staged: isStaged,
        error: getErrorMessage(error),
        failure_reasons: [{ code: "merge_operation_error", message: getErrorMessage(error) }],
        decision_evidence: evidence,
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
