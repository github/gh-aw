// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { renderSizedReport } = require("./report_size.cjs");

async function main() {
  const issueNumber = Number.parseInt(process.env.GH_AW_STEERING_ISSUE_NUMBER || "", 10);
  if (!Number.isFinite(issueNumber) || issueNumber <= 0) {
    core.info("No steering issue to complete");
    return;
  }

  const failureIssueNumber = Number.parseInt(process.env.GH_AW_FAILURE_ISSUE_NUMBER || "", 10);
  if (Number.isFinite(failureIssueNumber) && failureIssueNumber === issueNumber) {
    core.info(`Keeping steering issue #${issueNumber} open as the agent failure issue`);
    return;
  }

  let needs;
  try {
    needs = JSON.parse(process.env.GH_AW_NEEDS || "{}");
  } catch (error) {
    core.warning(`Unable to parse downstream job results: ${getErrorMessage(error)}`);
    needs = {};
  }
  const results = Object.values(needs)
    .map(value => value?.result)
    .filter(Boolean);
  if (results.includes("failure") || results.includes("cancelled")) {
    core.info(`Keeping steering issue #${issueNumber} open because the workflow did not complete successfully`);
    return;
  }

  const pullRequestNumber = Number.parseInt(process.env.GH_AW_CREATED_PR_NUMBER || "", 10);
  const pullRequestUrl = process.env.GH_AW_CREATED_PR_URL || "";
  /** @type {import("./report_ast.cjs").ReportInline[]} */
  const children = [{ type: "text", value: "The workflow completed successfully", agentTainted: false }];
  if (Number.isFinite(pullRequestNumber) && pullRequestNumber > 0 && pullRequestUrl) {
    children.push({ type: "text", value: " and created ", agentTainted: false }, { type: "link", url: pullRequestUrl, children: [{ type: "text", value: `pull request #${pullRequestNumber}`, agentTainted: false }] });
  }
  children.push({ type: "text", value: ".", agentTainted: false });
  const body = renderSizedReport({ type: "report", title: "Workflow completed", children: [{ type: "paragraph", children }] }).body.trimEnd();
  try {
    await github.rest.issues.createComment({
      owner: context.repo.owner,
      repo: context.repo.repo,
      issue_number: issueNumber,
      body,
    });
  } catch (error) {
    core.warning(`Failed to comment on steering issue #${issueNumber}: ${getErrorMessage(error)}`);
  }
  try {
    await withRetry(
      () =>
        github.rest.issues.update({
          owner: context.repo.owner,
          repo: context.repo.repo,
          issue_number: issueNumber,
          state: "closed",
          state_reason: "completed",
        }),
      { ...RATE_LIMIT_RETRY_CONFIG, maxRetries: 3 },
      `close steering issue #${issueNumber}`
    );
    core.info(`Closed steering issue #${issueNumber}`);
  } catch (error) {
    core.setFailed(`Failed to close steering issue #${issueNumber}: ${getErrorMessage(error)}`);
  }
}

module.exports = { main };
