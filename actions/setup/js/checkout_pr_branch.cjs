// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Checkout PR branch when PR context is available
 * This script handles both pull_request events and comment events on PRs
 */

const { getErrorMessage } = require("./error_helpers.cjs");

async function main() {
  const eventName = context.eventName;
  const pullRequest = context.payload.pull_request;

  if (!pullRequest) {
    core.info("No pull request context available, skipping checkout");
    return;
  }

  core.info(`Event: ${eventName}`);
  core.info(`Pull Request #${pullRequest.number}`);

  try {
    if (eventName === "pull_request") {
      // For pull_request events, use the head ref directly
      const branchName = pullRequest.head.ref;
      core.info(`Checking out PR branch: ${branchName}`);

      await exec.exec("git", ["fetch", "origin", branchName]);
      await exec.exec("git", ["checkout", branchName]);

      core.info(`✅ Successfully checked out branch: ${branchName}`);
    } else {
      // For comment events on PRs, use gh pr checkout with PR number
      const prNumber = pullRequest.number;
      core.info(`Checking out PR #${prNumber} using gh pr checkout`);

      await exec.exec("gh", ["pr", "checkout", prNumber.toString()]);

      core.info(`✅ Successfully checked out PR #${prNumber}`);
    }
  } catch (error) {
    const errorMsg = getErrorMessage(error);

    // Write to step summary to provide context about the failure
    const summaryContent = `## ❌ Failed to Checkout PR Branch

**Error:** ${errorMsg}

### Possible Reasons

This failure typically occurs when:
- The pull request has been closed or merged
- The branch has been deleted
- There are insufficient permissions to access the PR

### What to Do

If the pull request is closed, you may need to:
1. Reopen the pull request, or
2. Create a new pull request with the changes

If the pull request is still open, verify that:
- The branch still exists in the repository
- You have the necessary permissions to access it
`;

    await core.summary.addRaw(summaryContent).write();
    core.setFailed(`Failed to checkout PR branch: ${errorMsg}`);
  }
}

module.exports = { main };
