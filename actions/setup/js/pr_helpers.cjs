// @ts-check

const { getErrorMessage } = require("./error_helpers.cjs");

const MAX_BRANCH_RULE_PAGES = 50;

/**
 * Detect if a pull request is from a fork repository.
 *
 * A "fork PR" means the head and base are in *different* repositories
 * (cross-repo PR). Detection uses two signals:
 * 1. Handle deleted fork case (head.repo is null)
 * 2. Compare repository full names — different names mean cross-repo
 *
 * NOTE: We intentionally do NOT check head.repo.fork. That flag indicates
 * whether the repository *itself* is a fork of another repo, not whether
 * the PR is cross-repo. A same-repo PR in a forked repository (common in
 * OSS) would have fork=true but is NOT a cross-repo fork PR. Using that
 * flag caused false positives that forced `gh pr checkout` instead of fast
 * `git fetch`, which then failed due to stale GH_HOST values. See #24208.
 *
 * @param {any} pullRequest - The pull request object from GitHub context
 * @returns {{isFork: boolean, reason: string}} Fork detection result with reason
 */
function detectForkPR(pullRequest) {
  if (!pullRequest.head?.repo) {
    // Head repo is null - likely a deleted fork
    return { isFork: true, reason: "head repository deleted (was likely a fork)" };
  }

  if (pullRequest.head.repo.full_name !== pullRequest.base?.repo?.full_name) {
    // Different repository names — this is a cross-repo (fork) PR
    return { isFork: true, reason: "different repository names" };
  }

  return { isFork: false, reason: "same repository" };
}

/**
 * Extract and validate pull request number from a message or GitHub context.
 *
 * Tries to get PR number from:
 * 1. The message's pull_request_number field (if provided)
 * 2. The GitHub context payload (if in a PR context)
 *
 * @param {any|undefined} messageItem - The message object that might contain pull_request_number
 * @param {any} context - The GitHub context object with payload information
 * @returns {{prNumber: number|null, error: string|null}} Result with PR number or error message
 */
function getPullRequestNumber(messageItem, context) {
  // Try to get from message first
  if (messageItem?.pull_request_number !== undefined) {
    const prNumber = parseInt(String(messageItem.pull_request_number), 10);
    if (Number.isNaN(prNumber)) {
      return {
        prNumber: null,
        error: `Invalid pull_request_number: ${messageItem.pull_request_number}`,
      };
    }
    return { prNumber, error: null };
  }

  // Fall back to context
  const contextPR = context.payload?.pull_request?.number;
  if (!contextPR) {
    return {
      prNumber: null,
      error: "No pull_request_number provided and not in pull request context",
    };
  }

  return { prNumber: contextPR, error: null };
}

/**
 * Resolves pull request repository context and effective base branch.
 * Fetches repository metadata from the GitHub REST API.
 * The effective base branch is the explicitly configured branch (if any),
 * falling back to the repository's actual default branch.
 *
 * @param {import("@actions/github-script").AsyncFunctionArguments["github"]} github
 * @param {string} owner
 * @param {string} repo
 * @param {string|null|undefined} configuredBaseBranch - explicitly configured base branch (may be null or undefined)
 * @returns {Promise<{repoSlug: string, effectiveBaseBranch: string|null, resolvedDefaultBranch: string|null}>}
 */
async function resolvePullRequestRepo(github, owner, repo, configuredBaseBranch) {
  const { data } = await github.rest.repos.get({ owner, repo });
  const repoId = data.node_id;
  if (!repoId) {
    throw new Error(`Repository ${owner}/${repo} did not return a valid node_id from the REST API`);
  }
  const resolvedDefaultBranch = data.default_branch ?? null;
  const effectiveBaseBranch = configuredBaseBranch || resolvedDefaultBranch;
  return { repoSlug: `${owner}/${repo}`, effectiveBaseBranch, resolvedDefaultBranch };
}

/**
 * Builds a branch instruction string to prepend to custom instructions.
 * Tells the agent which branch to create its work branch from, with an
 * optional NOT clause when the effective branch differs from the repo default.
 *
 * @param {string} effectiveBaseBranch - the branch the agent should branch from
 * @param {string|null} resolvedDefaultBranch - the repo's actual default branch (used in NOT clause)
 * @returns {string}
 */
function buildBranchInstruction(effectiveBaseBranch, resolvedDefaultBranch) {
  const notClause = resolvedDefaultBranch && resolvedDefaultBranch !== effectiveBaseBranch ? `, NOT from '${resolvedDefaultBranch}'` : "";
  return `IMPORTANT: Create your branch from the '${effectiveBaseBranch}' branch${notClause}.`;
}

/**
 * @param {import("@actions/github-script").AsyncFunctionArguments["github"]} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {string} branch
 * @returns {Promise<boolean>}
 */
async function hasOnlyCopilotReviewRules(githubClient, owner, repo, branch) {
  // Read pages directly: Octokit's paginator can swallow a later-page HTTP 409.
  for (let page = 1; page <= MAX_BRANCH_RULE_PAGES; page++) {
    const { data: rules } = await githubClient.rest.repos.getBranchRules({ owner, repo, branch, per_page: 100, page });
    if (!Array.isArray(rules) || (page === 1 && rules.length === 0) || !rules.every(rule => rule?.type === "copilot_code_review")) {
      return false;
    }
    if (rules.length < 100) return true;
  }
  return false;
}

/**
 * Check the default-branch guard and optionally diagnose branch protection.
 * Call with `checkBranchProtection: false` before pushing, and inspect protection
 * only after a push failure. Inspection failures are informational.
 *
 * Returns a string describing the default-branch guard or diagnosed protection,
 * or null when neither applies or inspection is unavailable.
 *
 * @param {any} githubClient - Octokit REST client
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {string} branchName - Target branch to validate
 * @param {boolean} checkBranchProtection - Whether to call the branch API to check protection
 * @returns {Promise<string|null>} Error message if push is blocked, null if safe
 */
async function checkBranchPushable(githubClient, owner, repo, branchName, checkBranchProtection) {
  // Check whether the branch is the repository default branch
  /** @type {any} */
  let defaultBranch = null;
  try {
    const { data: repoData } = await githubClient.rest.repos.get({ owner, repo });
    defaultBranch = repoData.default_branch;
  } catch (repoError) {
    core.warning(`Could not check repository default branch: ${getErrorMessage(repoError)}`);
  }

  if (defaultBranch && branchName === defaultBranch) {
    return `Cannot push to branch "${branchName}": this is the repository's default branch. Agents must not push directly to the default branch.`;
  }

  // Check whether the branch has protection rules
  if (checkBranchProtection) {
    let isBranchProtected = false;
    try {
      const { data: branchData } = await githubClient.rest.repos.getBranch({ owner, repo, branch: branchName });
      isBranchProtected = branchData.protected;
      // The protected flag includes advisory rulesets; use read-compatible metadata.
      if (isBranchProtected && typeof defaultBranch === "string" && defaultBranch.length > 0 && branchName !== defaultBranch && branchData.protection?.enabled === false) {
        try {
          if (await hasOnlyCopilotReviewRules(githubClient, owner, repo, branchName)) {
            core.info(`Branch "${branchName}" reports only Copilot code review rules in its active rulesets`);
            isBranchProtected = false;
          }
        } catch (rulesError) {
          core.info(`Could not check branch protection rules for "${branchName}": ${getErrorMessage(rulesError)}. GitHub enforces branch protection and permissions when pushing.`);
          return null;
        }
      }
    } catch (protectionError) {
      const protectionStatus = protectionError && typeof protectionError === "object" && "status" in protectionError ? protectionError.status : undefined;
      if (protectionStatus === 404) {
        // 404 means the branch does not exist – safe to proceed
        core.info(`Branch "${branchName}" not found during branch-protection inspection`);
      } else {
        core.info(`Could not check branch protection rules for "${branchName}": ${getErrorMessage(protectionError)}. GitHub enforces branch protection and permissions when pushing.`);
      }
    }

    if (isBranchProtected) {
      return `Branch "${branchName}" has protection rules that may explain the rejected push.`;
    }
  }

  return null;
}

module.exports = { detectForkPR, getPullRequestNumber, resolvePullRequestRepo, buildBranchInstruction, checkBranchPushable };
