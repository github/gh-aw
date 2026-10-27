// @ts-check
/// <reference types="@actions/github-script" />

const { createReportIssue } = require("./issue_report.cjs");

const { renderSizedReport } = require("./report_size.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { FAQ_CREATE_PR_PERMISSIONS_URL } = require("./constants.cjs");

/**
 * Handle create_pull_request permission errors
 * This script is called from the conclusion job when create_pull_request fails
 * due to GitHub Actions not being permitted to create or approve pull requests.
 */

async function main() {
  const errorMessage = process.env.CREATE_PR_ERROR_MESSAGE;
  if (!errorMessage) {
    core.info("No create_pull_request error message - skipping");
    return;
  }

  // Check if this is the permission error
  if (!errorMessage.includes("GitHub Actions is not permitted to create or approve pull requests")) {
    core.info("Not a permission error - skipping");
    return;
  }

  core.info("Creating issue for create_pull_request permission error");

  const { owner, repo } = context.repo;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
  const runUrl = process.env.GH_AW_RUN_URL || "";
  const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
  const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";

  const issueTitle = "[aw] GitHub Actions needs permission to create pull requests";

  /** @param {string} value @returns {import("./report_ast.cjs").ReportInline} */
  const text = value => ({ type: "text", value, agentTainted: false });
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const paragraph = value => ({ type: "paragraph", children: [text(value)], agentTainted: false });
  /** @param {string} value @param {number} level @returns {import("./report_ast.cjs").ReportBlock} */
  const heading = (value, level) => ({ type: "heading", level, children: [text(value)], agentTainted: false });
  /** @param {string} label @param {string} url @returns {import("./report_ast.cjs").ReportInline} */
  const link = (label, url) => ({ type: "link", url, children: [text(label)], agentTainted: false });
  /** @type {import("./report_ast.cjs").ReportBlock[][]} */
  const sourceItem = workflowSourceURL ? [[{ type: "paragraph", children: [text("Source: "), link(workflowSource, workflowSourceURL)] }]] : [];
  /** @type {import("./report_ast.cjs").Report} */
  const report = {
    type: "report",
    title: issueTitle,
    agentTainted: false,
    children: [
      heading("GitHub Actions Permission Required", 2),
      {
        type: "paragraph",
        children: [
          text("The workflow "),
          { type: "strong", children: [text(workflowName)] },
          text(" attempted to create a pull request but failed because GitHub Actions is not permitted to create or approve pull requests in this repository."),
        ],
      },
      heading("How to Fix", 3),
      {
        type: "list",
        ordered: true,
        items: ["Go to Settings → Actions → General", "Scroll to the Workflow permissions section", "Check the box: Allow GitHub Actions to create and approve pull requests", "Click Save"].map(value => [paragraph(value)]),
      },
      heading("Documentation", 3),
      paragraph("For more information, see:"),
      {
        type: "list",
        ordered: false,
        items: [
          [{ type: "paragraph", children: [link("gh-aw FAQ: Why is my create-pull-request workflow failing?", FAQ_CREATE_PR_PERMISSIONS_URL)] }],
          [
            {
              type: "paragraph",
              children: [
                link(
                  "Managing GitHub Actions settings for a repository",
                  "https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#preventing-github-actions-from-creating-or-approving-pull-requests"
                ),
              ],
            },
          ],
        ],
      },
      heading("Workflow Details", 3),
      {
        type: "list",
        ordered: false,
        items: [[paragraph(`Workflow: ${workflowName}`)], [{ type: "paragraph", children: [text("Run: "), ...(runUrl ? [link(runUrl, runUrl)] : [])] }], ...sourceItem],
      },
    ],
  };

  // Search for existing issue with the same title
  const searchQuery = "repo:" + owner + "/" + repo + ' is:issue is:open in:title "' + issueTitle + '"';

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info("Issue already exists: #" + existingIssue.number);

      // Add a comment with run details
      const commentBody = renderSizedReport({ type: "report", title: issueTitle, children: [{ type: "paragraph", children: [text("This error occurred again in workflow run: "), ...(runUrl ? [link(runUrl, runUrl)] : [])] }] }).body;
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: existingIssue.number,
        body: commentBody,
      });
      core.info("Added comment to existing issue #" + existingIssue.number);
    } else {
      // Create new issue
      const { data: issue } = await createReportIssue(
        github,
        {
          owner,
          repo,
          title: issueTitle,
          labels: ["agentic-workflows", "configuration"],
        },
        report
      );
      core.info("Created issue #" + issue.number + ": " + issue.html_url);
    }
  } catch (error) {
    core.warning("Failed to create or update permission error issue: " + getErrorMessage(error));
    // Don't fail the conclusion job if we can't report the PR creation error
  }
}

module.exports = { main };
