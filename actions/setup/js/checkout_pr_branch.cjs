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
 *    - Must use `gh pr checkout` to fetch from the fork
 *    - Has write permissions (be cautious with untrusted code)
 *
 * 3. Other PR events (issue_comment, pull_request_review, etc.):
 *    - Also run in base repository context
 *    - Must use `gh pr checkout` to get PR branch
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplate } = require("./messages_core.cjs");
const { detectForkPR } = require("./pr_helpers.cjs");
const fs = require("fs");

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

  // Determine if this is a fork PR using the helper function
  const { isFork, reason: forkReason } = detectForkPR(pullRequest);
  core.info(`Is fork PR: ${isFork} (${forkReason})`);

  // Log current repository context
  core.info(`Current repository: ${context.repo.owner}/${context.repo.repo}`);
  core.info(`GitHub SHA: ${context.sha}`);

  core.endGroup();

  return { isFork };
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

async function main() {
  const eventName = context.eventName;
  const pullRequest = context.payload.pull_request;

  if (!pullRequest) {
    core.info("No pull request context available, skipping checkout");
    core.setOutput("checkout_pr_success", "true");
    return;
  }

  core.info(`Event: ${eventName}`);
  core.info(`Pull Request #${pullRequest.number}`);

  try {
    // Log detailed context for debugging
    const { isFork } = logPRContext(eventName, pullRequest);

    if (eventName === "pull_request") {
      // For pull_request events, we run in the merge commit context
      // The PR branch is already available, so we can use direct git commands
      const branchName = pullRequest.head.ref;

      logCheckoutStrategy(eventName, "git fetch + checkout", "pull_request event runs in merge commit context with PR branch available");

      core.info(`Fetching branch: ${branchName} from origin`);
      await exec.exec("git", ["fetch", "origin", branchName]);

      core.info(`Checking out branch: ${branchName}`);
      await exec.exec("git", ["checkout", branchName]);

      core.info(`✅ Successfully checked out branch: ${branchName}`);
    } else {
      // For pull_request_target and other PR events, we run in base repository context
      // IMPORTANT: For fork PRs, the head branch doesn't exist in the base repo
      // We must use `gh pr checkout` which handles fetching from forks
      const prNumber = pullRequest.number;

      const strategyReason = eventName === "pull_request_target" ? "pull_request_target runs in base repo context; for fork PRs, head branch doesn't exist in origin" : `${eventName} event runs in base repo context; must fetch PR branch`;

      logCheckoutStrategy(eventName, "gh pr checkout", strategyReason);

      if (isFork) {
        core.warning("⚠️ Fork PR detected - gh pr checkout will fetch from fork repository");
      }

      core.info(`Checking out PR #${prNumber} using gh CLI`);
      await exec.exec("gh", ["pr", "checkout", prNumber.toString()]);

      // Log the resulting branch after checkout
      let currentBranch = "";
      await exec.exec("git", ["branch", "--show-current"], {
        listeners: {
          stdout: data => {
            currentBranch += data.toString();
          },
        },
      });
      currentBranch = currentBranch.trim();

      core.info(`✅ Successfully checked out PR #${prNumber}`);
      core.info(`Current branch: ${currentBranch || "detached HEAD"}`);
    }

    // Set output to indicate successful checkout
    core.setOutput("checkout_pr_success", "true");
  } catch (error) {
    const errorMsg = getErrorMessage(error);

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
    const templatePath = "/opt/gh-aw/prompts/pr_checkout_failure.md";
    const template = fs.readFileSync(templatePath, "utf8");
    const summaryContent = renderTemplate(template, {
      error_message: errorMsg,
    });

    await core.summary.addRaw(summaryContent).write();
    core.setFailed(`Failed to checkout PR branch: ${errorMsg}`);
  }
}

module.exports = { main };
