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
 * Check whether a branch is safe to push to.
 *
 * Performs two security checks:
 * 1. Ensures the branch is not the repository's default branch.
 * 2. When `checkBranchProtection` is true, queries the branch API to
 *    verify the branch has no protection rules other than Copilot code review.
 *
 * Returns null when the push is safe to proceed, or a string error message that
 * should be surfaced as a hard failure when the push must be blocked.
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
      // The protected flag includes advisory rulesets. Confirm classic protection separately.
      if (isBranchProtected && typeof defaultBranch === "string" && defaultBranch.length > 0 && branchName !== defaultBranch && branchData.protection?.enabled === false) {
        try {
          if (await hasOnlyCopilotReviewRules(githubClient, owner, repo, branchName)) {
            try {
              await githubClient.rest.repos.getBranchProtection({ owner, repo, branch: branchName });
            } catch (classicError) {
              const classicStatus = classicError && typeof classicError === "object" && "status" in classicError ? classicError.status : undefined;
              const classicMessage = classicError && typeof classicError === "object" ? classicError.response?.data?.message : undefined;
              if (classicStatus === 403) {
                throw new Error(
                  `${getErrorMessage(classicError)}. Reading classic branch protection requires a GitHub App installation token or fine-grained PAT with Administration: read repository permission; GITHUB_TOKEN cannot provide this permission`,
                  { cause: classicError }
                );
              }
              if (classicStatus !== 404 || classicMessage !== "Branch not protected") throw classicError;
              core.info(`Branch "${branchName}" only has Copilot code review rules and no classic protection; proceeding to push`);
              isBranchProtected = false;
            }
          }
        } catch (rulesError) {
          return `Cannot verify branch protection rules for "${branchName}": ${getErrorMessage(rulesError)}. Push blocked to prevent accidental writes to protected branches.`;
        }
      }
    } catch (protectionError) {
      const protectionStatus = protectionError && typeof protectionError === "object" && "status" in protectionError ? protectionError.status : undefined;
      if (protectionStatus === 404) {
        // 404 means the branch does not exist – safe to proceed
        core.info(`Branch "${branchName}" not found; proceeding to push`);
      } else if (protectionStatus === 403) {
        // 403 means the token lacks permission to read branch protection rules.
        // The GitHub platform will still enforce branch protection at push time,
        // so warn and allow the push to proceed.
        core.warning(`Could not check branch protection rules for "${branchName}" (insufficient permissions): ${getErrorMessage(protectionError)}`);
      } else {
        // Unexpected errors (5xx, network failures, etc.) – fail closed to
        // avoid bypassing branch protection due to transient API issues.
        return `Cannot verify branch protection rules for "${branchName}": ${getErrorMessage(protectionError)}. Push blocked to prevent accidental writes to protected branches.`;
      }
    }

    if (isBranchProtected) {
      return `Cannot push to branch "${branchName}": this branch has protection rules. Agents must not push directly to protected branches.`;
    }
  } else {
    core.info(`Branch protection check skipped (check-branch-protection: false)`);
  }

  return null;
}

module.exports = { detectForkPR, getPullRequestNumber, resolvePullRequestRepo, buildBranchInstruction, checkBranchPushable };
