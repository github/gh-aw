// @ts-check
/// <reference types="@actions/github-script" />

const { getAgentName, getIssueDetails, findAgent, assignAgentToIssue } = require("./assign_agent_helpers.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Assign an issue to a user or bot (including copilot)
 * This script handles assigning issues after they are created
 */

async function main() {
  // Validate required environment variables
  const ghToken = process.env.GH_TOKEN;
  const assignee = process.env.ASSIGNEE;
  const issueNumber = process.env.ISSUE_NUMBER;

  // Check if GH_TOKEN is present
  if (!ghToken?.trim()) {
    const docsUrl = "https://github.github.com/gh-aw/reference/safe-outputs/#assigning-issues-to-copilot";
    core.setFailed(`GH_TOKEN environment variable is required but not set. This token is needed to assign issues. For more information on configuring Copilot tokens, see: ${docsUrl}`);
    return;
  }

  // Validate assignee
  if (!assignee?.trim()) {
    core.setFailed("ASSIGNEE environment variable is required but not set");
    return;
  }

  // Validate issue number
  if (!issueNumber?.trim()) {
    core.setFailed("ISSUE_NUMBER environment variable is required but not set");
    return;
  }

  const trimmedAssignee = assignee.trim();
  const issueNum = parseInt(issueNumber.trim(), 10);

  core.info(`Assigning issue #${issueNum} to ${trimmedAssignee}`);

  // Check if the assignee is a known coding agent (e.g., copilot, @copilot)
  const agentName = getAgentName(trimmedAssignee);

  try {
    if (agentName) {
      // Use GraphQL API for agent assignment
      // The token is set at the step level via github-token parameter
      core.info(`Detected coding agent: ${agentName}. Using GraphQL API for assignment.`);

      // Get repository owner and repo from context
      const { owner, repo } = context.repo;

      // Find the agent in the repository
      const agentId = await findAgent(owner, repo, agentName);
      if (!agentId) {
        throw new Error(`${agentName} coding agent is not available for this repository`);
      }
      core.info(`Found ${agentName} coding agent (ID: ${agentId})`);

      // Get issue details
      const issueDetails = await getIssueDetails(owner, repo, issueNum);
      if (!issueDetails) {
        throw new Error("Failed to get issue details");
      }

      // Check if agent is already assigned
      if (issueDetails.currentAssignees.some(a => a.id === agentId)) {
        core.info(`${agentName} is already assigned to issue #${issueNum}`);
      } else {
        // Assign agent using GraphQL mutation - uses built-in github object authenticated via github-token (no allowed list filtering)
        const success = await assignAgentToIssue(issueDetails.issueId, agentId, issueDetails.currentAssignees, agentName, null);

        if (!success) {
          throw new Error(`Failed to assign ${agentName} via GraphQL`);
        }
      }
    } else {
      // Use gh CLI for regular user assignment
      await exec.exec("gh", ["issue", "edit", String(issueNum), "--add-assignee", trimmedAssignee], {
        env: { ...process.env, GH_TOKEN: ghToken },
      });
    }

    core.info(`✅ Successfully assigned issue #${issueNum} to ${trimmedAssignee}`);

    // Write summary
    await core.summary.addRaw(`## Issue Assignment\n\nSuccessfully assigned issue #${issueNum} to \`${trimmedAssignee}\`.\n`).write();
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    core.error(`Failed to assign issue: ${errorMessage}`);
    core.setFailed(`Failed to assign issue #${issueNum} to ${trimmedAssignee}: ${errorMessage}`);
  }
}

module.exports = { main };
