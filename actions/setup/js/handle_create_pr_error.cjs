// @ts-check
/// <reference types="@actions/github-script" />

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

  const issueTitle = "[agentics] GitHub Actions needs permission to create pull requests";

  const issueBody =
    "## GitHub Actions Permission Required\n\n" +
    "The workflow **" +
    workflowName +
    "** attempted to create a pull request but failed because GitHub Actions is not permitted to create or approve pull requests in this repository.\n\n" +
    "### How to Fix\n\n" +
    "1. Go to **Settings** → **Actions** → **General**\n" +
    "2. Scroll to the **Workflow permissions** section\n" +
    "3. Check the box: **Allow GitHub Actions to create and approve pull requests**\n" +
    "4. Click **Save**\n\n" +
    "### Documentation\n\n" +
    "For more information, see: [Managing GitHub Actions settings for a repository](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository#preventing-github-actions-from-creating-or-approving-pull-requests)\n\n" +
    "### Workflow Details\n\n" +
    "- **Workflow**: " +
    workflowName +
    "\n" +
    "- **Run**: " +
    runUrl +
    "\n" +
    (workflowSourceURL ? "- **Source**: [" + workflowSource + "](" + workflowSourceURL + ")\n" : "") +
    "\n";

  // Search for existing issue with the same title
  const searchQuery = "repo:" + owner + "/" + repo + ' is:issue is:open in:title "' + issueTitle + '"';
  const searchResult = await github.rest.search.issuesAndPullRequests({
    q: searchQuery,
    per_page: 1,
  });

  if (searchResult.data.total_count > 0) {
    const existingIssue = searchResult.data.items[0];
    core.info("Issue already exists: #" + existingIssue.number);

    // Add a comment with run details
    const commentBody = "This error occurred again in workflow run: " + runUrl;
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: existingIssue.number,
      body: commentBody,
    });
    core.info("Added comment to existing issue #" + existingIssue.number);
  } else {
    // Create new issue
    const { data: issue } = await github.rest.issues.create({
      owner,
      repo,
      title: issueTitle,
      body: issueBody,
      labels: ["agentic-workflows", "configuration"],
    });
    core.info("Created issue #" + issue.number + ": " + issue.html_url);
  }
}

module.exports = { main };
