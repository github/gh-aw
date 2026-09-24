// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Checkout PR branch when PR context is available
 *
 * This script handles checkout for different GitHub event types:
 *
 * 1. pull_request: Runs in merge commit context (PR head + base merged)
 *    - Can use direct git commands since we're already in PR context
 *    - Branch exists in current checkout
 *
 * 2. pull_request_target: Runs in BASE repository context (not PR head)
 *    - CRITICAL: For fork PRs, the head branch doesn't exist in base repo
 *    - Uses refs/pull/N/head to fetch from origin (works for forks too)
 *    - Has write permissions (be cautious with untrusted code)
 *
 * 3. Other PR events (issue_comment, pull_request_review, etc.):
 *    - Also run in base repository context
 *    - Uses refs/pull/N/head to fetch PR branch
 *
 * 4. workflow_dispatch with aw_context:
 *    - When aw_context input contains item_type=="pull_request" and item_number,
 *      the PR number is extracted and the head is fetched via refs/pull/N/head
 *    - Mirrors the guard in the compiled workflow's if: condition
 *
 * NOTE: This handler operates within the PR context from the workflow event
 * and does not support cross-repository operations or target-repo parameters.
 * No allowlist validation (checkAllowedRepo/validateTargetRepo) is needed as
 * it only works with the PR from the triggering event.
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplateFromFile, getPromptPath } = require("./messages_core.cjs");
const { detectForkPR } = require("./pr_helpers.cjs");
const { ERR_API, ERR_PERMISSION } = require("./error_codes.cjs");
const TRUSTED_CHECKOUT_PERMISSIONS = ["write", "maintain", "admin"];
const PR_HEAD_BASE_REF = "refs/remotes/origin/pr-head";

/**
 * Resolve the commit SHA actually checked out at HEAD.
 *
 * Payload/API `head.sha` values can go stale if the PR advances between the
 * event/API read and the `git fetch` above, or be absent from a shallow
 * fetch. Resolving `HEAD^{commit}` after checkout reflects exactly what was
 * fetched and checked out, so patch generation never ranges over commits the
 * workflow never saw.
 *
 * @returns {Promise<string | null>}
 */
async function resolveCheckedOutHeadSha() {
  try {
    const result = await exec.getExecOutput("git", ["rev-parse", "HEAD^{commit}"], {
      silent: true,
      ignoreReturnCode: true,
    });
    if (result.exitCode !== 0) {
      return null;
    }
    const sha = result.stdout.trim();
    return sha || null;
  } catch (e) {
    core.warning(`Could not resolve checked-out HEAD commit: ${getErrorMessage(e)}`);
    return null;
  }
}

async function exportPRHeadBaseline({ branchName, baseRepo, headRepo, prNumber }) {
  if (!branchName) {
    return;
  }
  const baseSha = await resolveCheckedOutHeadSha();
  if (!baseSha) {
    core.warning("Could not resolve checked-out HEAD commit; skipping PR head baseline export for incremental patches.");
    return;
  }
  core.exportVariable("GH_AW_PR_HEAD_BASE_BRANCH", branchName);
  core.exportVariable("GH_AW_PR_HEAD_BASE_SHA", baseSha);
  if (baseRepo) {
    core.exportVariable("GH_AW_PR_HEAD_BASE_REPO", baseRepo);
  }
  if (headRepo) {
    core.exportVariable("GH_AW_PR_HEAD_REPO", headRepo);
  }
  if (prNumber != null) {
    core.exportVariable("GH_AW_PR_HEAD_BASE_PR_NUMBER", String(prNumber));
  }
  core.exportVariable("GH_AW_PR_HEAD_BASE_REF", PR_HEAD_BASE_REF);
  core.info(`Recorded PR head baseline for incremental patches: ${branchName}@${baseSha}`);
}

/**
 * Determine whether the current repository is a shallow clone.
 *
 * A `--depth` fetch against an already-complete clone writes `.git/shallow` and
 * grafts history, silently undoing an explicit `checkout: fetch-depth: 0`. That
 * breaks later `git merge-base` calls (e.g. patch generation for
 * create_pull_request). We therefore only pass `--depth` when the repository is
 * already shallow; a complete clone already has the objects we need.
 *
 * @returns {Promise<boolean>} true when the repository is shallow
 */
async function isShallowRepository() {
  try {
    const result = await exec.getExecOutput("git", ["rev-parse", "--is-shallow-repository"], {
      silent: true,
      ignoreReturnCode: true,
    });
    if (result.exitCode !== 0) {
      return false;
    }
    return result.stdout.trim() === "true";
  } catch (e) {
    core.warning(`Could not determine repository shallowness, assuming complete clone: ${getErrorMessage(e)}`);
    return false;
  }
}

/**
 * Build the optional `--depth=N` argument for a fetch, omitting it when the
 * repository is a complete (non-shallow) clone.
 *
 * @param {number} fetchDepth
 * @returns {Promise<string[]>}
 */
async function depthArgs(fetchDepth) {
  if (await isShallowRepository()) {
    return [`--depth=${fetchDepth}`];
  }
  core.info("Repository is not shallow (e.g. fetch-depth: 0), fetching without --depth to preserve full history");
  return [];
}

/**
 * Log detailed PR context information for debugging
 */
function logPRContext(eventName, pullRequest) {
  core.startGroup("📋 PR Context Details");

  core.info(`Event type: ${eventName}`);
  core.info(`PR number: ${pullRequest.number}`);
  core.info(`PR state: ${pullRequest.state || "unknown"}`);

  // Log head information
  if (pullRequest.head) {
    core.info(`Head ref: ${pullRequest.head.ref || "unknown"}`);
    core.info(`Head SHA: ${pullRequest.head.sha || "unknown"}`);

    if (pullRequest.head.repo) {
      core.info(`Head repo: ${pullRequest.head.repo.full_name || "unknown"}`);
      core.info(`Head repo owner: ${pullRequest.head.repo.owner?.login || "unknown"}`);
    } else {
      core.warning("⚠️ Head repo information not available (repo may be deleted)");
    }
  }

  // Log base information
  if (pullRequest.base) {
    core.info(`Base ref: ${pullRequest.base.ref || "unknown"}`);
    core.info(`Base SHA: ${pullRequest.base.sha || "unknown"}`);

    if (pullRequest.base.repo) {
      core.info(`Base repo: ${pullRequest.base.repo.full_name || "unknown"}`);
      core.info(`Base repo owner: ${pullRequest.base.repo.owner?.login || "unknown"}`);
    }
  }

  // Determine if this is a fork PR using the helper function.
  // Only call detectForkPR when head/base data is present (pull_request and
  // pull_request_target payloads). For minimal PR objects (e.g. issue_comment)
  // fork status is unknown until we fetch full PR details from the API.
  /** @type {any} */
  let isFork = null;
  if (pullRequest.head?.repo && pullRequest.base?.repo) {
    const { isFork: detected, reason: forkReason } = detectForkPR(pullRequest);
    isFork = detected;
    core.info(`Is fork PR: ${isFork} (${forkReason})`);
  } else {
    core.info("Is fork PR: unknown (head/base repo details not available in event payload)");
  }

  // Log current repository context
  core.info(`Current repository: ${context.repo.owner}/${context.repo.repo}`);
  core.info(`GitHub SHA: ${context.sha}`);

  core.endGroup();

  return { isFork };
}

/**
 * Fetch PR details from the GitHub API.
 * Returns head ref and commit count needed for checkout.
 */
async function fetchPRDetails(prNumber) {
  const { data } = await github.rest.pulls.get({
    owner: context.repo.owner,
    repo: context.repo.repo,
    pull_number: prNumber,
  });
  return { commitCount: data.commits, headRef: data.head.ref, pullRequest: data };
}

/**
 * Log the checkout strategy being used
 */
function logCheckoutStrategy(eventName, strategy, reason) {
  core.startGroup("🔄 Checkout Strategy");
  core.info(`Event type: ${eventName}`);
  core.info(`Strategy: ${strategy}`);
  core.info(`Reason: ${reason}`);
  core.endGroup();
}

/**
 * Ensure checkout step only runs in trusted runtime contexts.
 * - workflow_dispatch PR replay must not run in a forked repository
 * - triggering actor must have write-or-higher repository permission
 */
async function assertTrustedCheckoutRuntime() {
  if (context.eventName === "workflow_dispatch") {
    const repository = context.payload.repository;
    // Fork status can only be verified when the payload carries repository
    // data. Fail closed rather than silently trusting an unverifiable
    // runtime: RS-05a requires proving "not a fork", not merely failing to
    // prove "is a fork".
    if (!repository) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to determine repository fork status for workflow_dispatch");
    }
    if (repository.fork === true) {
      throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout in forked repository runtime context");
    }
  }

  // context.actor is preferred when available; sender.login and GITHUB_ACTOR
  // are retained as event/runtime-compatible fallbacks.
  const actor = context.actor || context.payload.sender?.login || process.env.GITHUB_ACTOR;
  if (!actor) {
    throw new Error(`${ERR_PERMISSION}: ` + "Refusing PR checkout: unable to determine triggering actor");
  }

  // Bot and app actors (e.g. Copilot, dependabot[bot]) are not regular GitHub
  // users and cannot be resolved via the collaborators API (returns 404).
  // Trust them based on the event payload's own identity signal, independently
  // of the workflow_dispatch-only fork check above: GitHub sets `sender.type`,
  // so a "Bot" sender identifies an app installation acting on this repository.
  const senderType = context.payload.sender?.type;
  if (senderType === "Bot") {
    core.info(`Runtime safety check passed for bot/app actor '${actor}' (sender type: ${senderType})`);
    return;
  }

  try {
    const { data: permissionData } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner: context.repo.owner,
      repo: context.repo.repo,
      username: actor,
    });

    const permission = permissionData?.permission || "none";
    const hasWriteOrHigher = TRUSTED_CHECKOUT_PERMISSIONS.includes(permission);
    if (!hasWriteOrHigher) {
      throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: actor '${actor}' has '${permission}' permission (requires write or higher)`);
    }

    core.info(`Runtime safety check passed for actor '${actor}' with '${permission}' permission`);
  } catch (err) {
    // A 404 here is ambiguous: it can indicate either a non-user app/bot actor
    // or a real user that is not a collaborator. Disambiguate via users API.
    // Real users resolve via users.getByUsername; app/bot actors return 404.
    const errAny = /** @type {any} */ err;
    if (errAny.status === 404) {
      try {
        await github.rest.users.getByUsername({ username: actor });
        throw new Error(`${ERR_PERMISSION}: Refusing PR checkout: actor '${actor}' is not a collaborator (requires write or higher)`);
      } catch (userErr) {
        const userErrAny = /** @type {any} */ userErr;
        if (userErrAny.status === 404) {
          core.info(`Runtime safety check passed for app actor '${actor}' (not a regular user)`);
          return;
        }
        throw userErr;
      }
    }
    throw err;
  }
}

async function main() {
  const eventName = context.eventName;
  // For pull_request events, the PR context is in context.payload.pull_request.
  // For issue_comment events on PRs, context.payload.pull_request is not set;
  // instead context.payload.issue.pull_request indicates the issue is a PR.
  let pullRequest = context.payload.pull_request;

  // Handle issue_comment (and similar) events triggered on a PR
  if (!pullRequest && context.payload.issue?.pull_request) {
    pullRequest = {
      number: context.payload.issue.number,
      state: context.payload.issue.state || "open",
    };
    core.info(`Detected ${eventName} event on PR #${pullRequest.number}, will fetch PR ref`);
  }

  // Handle workflow_dispatch events with aw_context pointing to a PR
  if (!pullRequest && eventName === "workflow_dispatch") {
    const awContextStr = context.payload.inputs?.aw_context;
    if (awContextStr) {
      try {
        const awContext = JSON.parse(awContextStr);
        const prNumber = Number(awContext.item_number);
        if (awContext.item_type === "pull_request" && Number.isInteger(prNumber) && prNumber > 0) {
          if (awContext.repo) {
            const currentRepo = `${context.repo.owner}/${context.repo.repo}`;
            if (awContext.repo !== currentRepo) {
              core.warning(`Cross-repository workflow_dispatch is not supported: aw_context.repo (${awContext.repo}) does not match current repository (${currentRepo}), skipping checkout`);
            } else {
              pullRequest = {
                number: prNumber,
                state: "open",
              };
              core.info(`Detected workflow_dispatch event for PR #${pullRequest.number} via aw_context, will fetch PR ref`);
            }
          } else {
            pullRequest = {
              number: prNumber,
              state: "open",
            };
            core.info(`Detected workflow_dispatch event for PR #${pullRequest.number} via aw_context, will fetch PR ref`);
          }
        }
      } catch (e) {
        core.warning(`Failed to parse aw_context: ${getErrorMessage(e)}`);
      }
    }
  }

  if (!pullRequest) {
    core.info("No pull request context available, skipping checkout");
    core.setOutput("checkout_pr_success", "true");
    return;
  }

  core.info(`Event: ${eventName}`);
  core.info(`Pull Request #${pullRequest.number}`);

  // Check if PR is closed
  const isClosed = pullRequest.state === "closed";
  if (isClosed) {
    core.info("⚠️ Pull request is closed");
  }

  try {
    await assertTrustedCheckoutRuntime();

    // Log detailed context for debugging
    const { isFork } = logPRContext(eventName, pullRequest);

    if (eventName === "pull_request" && isFork === false) {
      // For non-fork pull_request events, we run in the merge commit context.
      // The PR branch is in the same repo as origin, so we can use direct git commands.
      // Fork PRs cannot use git fetch because their head branch only exists in the fork
      // (not in origin/base repo), so they must use gh pr checkout in the else branch below.
      const branchName = pullRequest.head.ref;
      // commits is in the payload for pull_request events; +1 to include the merge base
      const commitCount = pullRequest.commits || 1;
      const fetchDepth = commitCount + 1;

      logCheckoutStrategy(eventName, "git fetch + checkout", "pull_request event runs in merge commit context with PR branch available");

      core.info(`Fetching branch: ${branchName} from origin (depth: ${fetchDepth} for ${commitCount} PR commit(s))`);
      const fetchArgs = await depthArgs(fetchDepth);
      core.info(fetchArgs.length > 0 ? `Fetching with ${fetchArgs.join(" ")}` : "Fetching without --depth (full history preserved)");
      await exec.exec("git", ["fetch", "origin", branchName, ...fetchArgs]);

      core.info(`Checking out branch: ${branchName}`);
      await exec.exec("git", ["checkout", branchName]);

      await exportPRHeadBaseline({
        branchName,
        baseRepo: pullRequest.base?.repo?.full_name || `${context.repo.owner}/${context.repo.repo}`,
        headRepo: pullRequest.head?.repo?.full_name,
        prNumber: pullRequest.number,
      });
      core.info(`✅ Successfully checked out branch: ${branchName}`);
    } else {
      // For pull_request_target, fork pull_request events, and other PR events,
      // we run in base repository context.
      // Use refs/pull/N/head which GitHub makes available for all PRs (including forks)
      // so we don't need `gh pr checkout` and avoid GH_HOST / DIFC proxy issues.
      const prNumber = pullRequest.number;

      // Get PR details from API to determine head ref name and commit count.
      // This also gives us the full PR object for accurate fork detection
      // when the event payload only had a minimal PR (e.g. issue_comment).
      const { commitCount, headRef, pullRequest: fullPR } = await fetchPRDetails(prNumber);

      // Re-evaluate fork status with full PR data when it was unknown
      const fullPRForkDetection = detectForkPR(fullPR);
      const actualIsFork = isFork ?? fullPRForkDetection.isFork;
      if (isFork === null) {
        core.info(`Is fork PR (from API): ${actualIsFork} (${fullPRForkDetection.reason})`);
      }

      const strategyReason =
        eventName === "pull_request_target"
          ? "pull_request_target runs in base repo context; fetching via refs/pull/N/head"
          : eventName === "pull_request" && actualIsFork
            ? "pull_request event from fork repository; fetching via refs/pull/N/head"
            : `${eventName} event runs in base repo context; fetching via refs/pull/N/head`;

      logCheckoutStrategy(eventName, "git fetch refs/pull + checkout", strategyReason);

      if (actualIsFork) {
        core.warning("⚠️ Fork PR detected - fetching via refs/pull/N/head from origin");
      }
      const fetchDepth = (commitCount || 1) + 1; // +1 to include the merge base

      core.info(`Fetching PR #${prNumber} head via refs/pull/${prNumber}/head (depth: ${fetchDepth} for ${commitCount} PR commit(s))`);
      const prFetchArgs = await depthArgs(fetchDepth);
      core.info(prFetchArgs.length > 0 ? `Fetching with ${prFetchArgs.join(" ")}` : "Fetching without --depth (full history preserved)");
      await exec.exec("git", ["fetch", "origin", `+refs/pull/${prNumber}/head:${PR_HEAD_BASE_REF}`, ...prFetchArgs]);

      const branchName = headRef || `pr-${prNumber}`;
      core.info(`Checking out branch: ${branchName}`);
      await exec.exec("git", ["checkout", "-B", branchName, "origin/pr-head"]);

      await exportPRHeadBaseline({
        branchName,
        baseRepo: fullPR.base?.repo?.full_name || `${context.repo.owner}/${context.repo.repo}`,
        headRepo: fullPR.head?.repo?.full_name,
        prNumber,
      });
      core.info(`✅ Successfully checked out PR #${prNumber}`);
      core.info(`Current branch: ${branchName}`);
    }

    // Set output to indicate successful checkout
    core.setOutput("checkout_pr_success", "true");
  } catch (error) {
    const errorMsg = getErrorMessage(error);

    // Check if PR is closed - if so, treat checkout failure as a warning
    if (isClosed) {
      core.startGroup("⚠️ Closed PR Checkout Warning");
      core.warning(`Event type: ${eventName}`);
      core.warning(`PR number: ${pullRequest.number}`);
      core.warning(`PR state: closed`);
      core.warning(`Checkout failed (expected for closed PR): ${errorMsg}`);

      if (pullRequest.head?.ref) {
        core.warning(`Branch likely deleted: ${pullRequest.head.ref}`);
      }

      core.warning("This is expected behavior when a PR is closed - the branch may have been deleted.");
      core.endGroup();

      // Set output to indicate successful handling of closed PR
      core.setOutput("checkout_pr_success", "true");

      // Add a brief summary noting this is expected
      const warningMessage = `## ⚠️ Closed Pull Request

Pull request #${pullRequest.number} is closed. The checkout failed because the branch has likely been deleted, which is expected behavior.

**This is not an error** - workflows targeting closed PRs will continue normally.`;

      await core.summary.addRaw(warningMessage).write();

      // Do NOT call setFailed - this should not fail the step
      return;
    }

    // Re-check current PR state via API to handle race conditions where
    // the PR was merged/closed after the webhook payload was captured but
    // before the agent job ran (e.g. PR merged within seconds of triggering).
    let isNowClosed = false;
    try {
      const { data: currentPR } = await github.rest.pulls.get({
        owner: context.repo.owner,
        repo: context.repo.repo,
        pull_number: pullRequest.number,
      });
      isNowClosed = currentPR.state === "closed";
      if (isNowClosed) {
        core.info(`ℹ️ PR #${pullRequest.number} is now closed (was '${pullRequest.state}' in webhook payload) — treating checkout failure as expected`);
      }
    } catch (apiError) {
      const apiErrorMsg = getErrorMessage(apiError);
      const statusCode = /** @type {any} */ apiError?.status;
      const statusSuffix = statusCode ? ` (HTTP ${statusCode})` : "";
      core.warning(`Could not fetch current PR state${statusSuffix}: ${apiErrorMsg}`);
    }

    if (isNowClosed) {
      core.startGroup("⚠️ Closed PR Checkout Warning");
      core.warning(`Event type: ${eventName}`);
      core.warning(`PR number: ${pullRequest.number}`);
      core.warning(`PR state: closed (merged after workflow was triggered)`);
      core.warning(`Checkout failed (expected for closed PR): ${errorMsg}`);

      if (pullRequest.head?.ref) {
        core.warning(`Branch likely deleted: ${pullRequest.head.ref}`);
      }

      core.warning("This is expected behavior when a PR is closed - the branch may have been deleted.");
      core.endGroup();

      // Set output to indicate successful handling of closed PR
      core.setOutput("checkout_pr_success", "true");

      const warningMessage = `## ⚠️ Closed Pull Request

Pull request #${pullRequest.number} was merged after this workflow was triggered. The checkout failed because the branch has been deleted, which is expected behavior.

**This is not an error** - workflows targeting closed PRs will continue normally.`;

      await core.summary.addRaw(warningMessage).write();
      return;
    }

    // For open PRs, treat checkout failure as an error
    // Log detailed error context
    core.startGroup("❌ Checkout Error Details");
    core.error(`Event type: ${eventName}`);
    core.error(`PR number: ${pullRequest.number}`);
    core.error(`Error message: ${errorMsg}`);

    if (pullRequest.head?.ref) {
      core.error(`Attempted to check out: ${pullRequest.head.ref}`);
    }

    // Log current git state for debugging
    try {
      core.info("Current git status:");
      await exec.exec("git", ["status"]);

      core.info("Available remotes:");
      await exec.exec("git", ["remote", "-v"]);

      core.info("Current branch:");
      await exec.exec("git", ["branch", "--show-current"]);
    } catch (gitError) {
      core.warning(`Could not retrieve git state: ${getErrorMessage(gitError)}`);
    }

    core.endGroup();

    // Set output to indicate checkout failure
    core.setOutput("checkout_pr_success", "false");

    // Load and render step summary template
    const templatePath = getPromptPath("pr_checkout_failure.md");
    const summaryContent = renderTemplateFromFile(templatePath, {
      error_message: errorMsg,
    });

    await core.summary.addRaw(summaryContent).write();
    core.setFailed(`${ERR_API}: Failed to checkout PR branch: ${errorMsg}`);
  }
}

module.exports = { main };
