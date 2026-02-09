// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { generateFooterWithExpiration } = require("./ephemerals.cjs");
const { renderTemplate } = require("./messages_core.cjs");

/**
 * Search for or create the parent issue for all agentic workflow no-op runs
 * @returns {Promise<{number: number, node_id: string}>} Parent issue number and node ID
 */
async function ensureAgentRunsIssue() {
  const { owner, repo } = context.repo;
  const parentTitle = "[agentic-workflows] Agent runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for agent runs issue: "${parentTitle}"`);

  // Search for existing agent runs issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info(`Found existing agent runs issue #${existingIssue.number}: ${existingIssue.html_url}`);

      return {
        number: existingIssue.number,
        node_id: existingIssue.node_id,
      };
    }
  } catch (error) {
    core.warning(`Error searching for agent runs issue: ${getErrorMessage(error)}`);
  }

  // Create agent runs issue if it doesn't exist
  core.info(`No agent runs issue found, creating one`);

  let parentBodyContent = `This issue tracks all no-op runs from agentic workflows in this repository. Each workflow run that completes with a no-op message (indicating no action was needed) posts a comment here.

### Purpose

This issue helps you:
- Track workflows that ran but determined no action was needed
- Distinguish between failures and intentional no-ops
- Monitor workflow health by seeing when workflows decide not to act

### What is a No-Op?

A no-op (no operation) occurs when an agentic workflow runs successfully but determines that no action is required. For example:
- A security scanner that finds no issues
- An update checker that finds nothing to update
- A monitoring workflow that finds everything is healthy

These are successful outcomes, not failures, and help provide transparency into workflow behavior.

### Resources

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)
- [Safe Outputs Reference](https://github.com/github/gh-aw/blob/main/docs/reference/safe-outputs.md)

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.`;

  // Add expiration marker (30 days from now) inside the quoted section using helper
  const footer = generateFooterWithExpiration({
    footerText: parentBodyContent,
    expiresHours: 24 * 30, // 30 days
  });
  const parentBody = footer;

  try {
    const newIssue = await github.rest.issues.create({
      owner,
      repo,
      title: parentTitle,
      body: parentBody,
      labels: [parentLabel],
    });

    core.info(`✓ Created agent runs issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    return {
      number: newIssue.data.number,
      node_id: newIssue.data.node_id,
    };
  } catch (error) {
    core.error(`Failed to create agent runs issue: ${getErrorMessage(error)}`);
    throw error;
  }
}

/**
 * Handle posting a no-op message to the agent runs issue
 * This script is called from the conclusion job when the agent produced only a noop safe-output
 * It only posts the message when:
 * 1. The agent succeeded (no failures)
 * 2. There are no safe-outputs other than noop
 */
async function main() {
  try {
    // Get workflow context
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const noopMessage = process.env.GH_AW_NOOP_MESSAGE || "";
    const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "";

    core.info(`Workflow name: ${workflowName}`);
    core.info(`Run URL: ${runUrl}`);
    core.info(`No-op message: ${noopMessage}`);
    core.info(`Agent conclusion: ${agentConclusion}`);

    if (!noopMessage) {
      core.info("No no-op message found, skipping");
      return;
    }

    // Only post to "agent runs" issue if the agent succeeded (no failures)
    if (agentConclusion !== "success") {
      core.info(`Agent did not succeed (conclusion: ${agentConclusion}), skipping no-op message posting`);
      return;
    }

    // Check that there are no safe-outputs other than noop
    const { loadAgentOutput } = require("./load_agent_output.cjs");
    const agentOutputResult = loadAgentOutput();

    if (!agentOutputResult.success || !agentOutputResult.items) {
      core.info("No agent output found, skipping");
      return;
    }

    // Check if there are any non-noop outputs
    const nonNoopItems = agentOutputResult.items.filter(item => item.type !== "noop");
    if (nonNoopItems.length > 0) {
      core.info(`Found ${nonNoopItems.length} non-noop output(s), skipping no-op message posting`);
      return;
    }

    core.info("Agent succeeded with only noop outputs - posting to agent runs issue");

    const { owner, repo } = context.repo;

    // Ensure agent runs issue exists
    let agentRunsIssue;
    try {
      agentRunsIssue = await ensureAgentRunsIssue();
    } catch (error) {
      core.warning(`Could not create agent runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow if we can't create the issue
      return;
    }

    // Extract run ID from URL (e.g., https://github.com/owner/repo/actions/runs/123 -> "123")
    let runId = "";
    const runIdMatch = runUrl.match(/\/actions\/runs\/(\d+)/);
    if (runIdMatch) {
      runId = runIdMatch[1];
    }

    // Build the comment body
    const timestamp = new Date().toISOString();
    let commentBody = `### No-Op Run: ${sanitizeContent(workflowName)}

**Run ID:** [${runId}](${runUrl})  
**Timestamp:** ${timestamp}  

**Message:**

${sanitizeContent(noopMessage)}

---

*This workflow completed successfully with no action required.*`;

    // Sanitize the full comment body
    const fullCommentBody = sanitizeContent(commentBody, { maxLength: 65000 });

    try {
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: agentRunsIssue.number,
        body: fullCommentBody,
      });

      core.info(`✓ Posted no-op message to agent runs issue #${agentRunsIssue.number}`);
    } catch (error) {
      core.warning(`Failed to post comment to agent runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow
    }
  } catch (error) {
    core.warning(`Error in handle_noop_message: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = { main, ensureAgentRunsIssue };
