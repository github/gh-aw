// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Checkout PR branch when PR context is available
 * This script handles both pull_request events and comment events on PRs
 */

const { getErrorMessage } = require("./error_helpers.cjs");
const { renderTemplate } = require("./messages_core.cjs");
const fs = require("fs");

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

    // Set output to indicate successful checkout
    core.setOutput("checkout_pr_success", "true");
  } catch (error) {
    const errorMsg = getErrorMessage(error);

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
