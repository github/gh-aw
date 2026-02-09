// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
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
  const parentTitle = "[agentics] No-Op Runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for no-op runs issue: "${parentTitle}"`);

  // Search for existing no-op runs issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info(`Found existing no-op runs issue #${existingIssue.number}: ${existingIssue.html_url}`);

      return {
        number: existingIssue.number,
        node_id: existingIssue.node_id,
      };
    }
  } catch (error) {
    core.warning(`Error searching for no-op runs issue: ${getErrorMessage(error)}`);
  }

  // Create no-op runs issue if it doesn't exist
  core.info(`No no-op runs issue found, creating one`);

  // Load template from file
  const templatePath = "/opt/gh-aw/prompts/noop_runs_issue.md";
  const parentBodyContent = fs.readFileSync(templatePath, "utf8");

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

    core.info(`✓ Created no-op runs issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    return {
      number: newIssue.data.number,
      node_id: newIssue.data.node_id,
    };
  } catch (error) {
    core.error(`Failed to create no-op runs issue: ${getErrorMessage(error)}`);
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
    const reportAsIssue = process.env.GH_AW_NOOP_REPORT_AS_ISSUE !== "false"; // Default to true

    core.info(`Workflow name: ${workflowName}`);
    core.info(`Run URL: ${runUrl}`);
    core.info(`No-op message: ${noopMessage}`);
    core.info(`Agent conclusion: ${agentConclusion}`);
    core.info(`Report as issue: ${reportAsIssue}`);

    if (!noopMessage) {
      core.info("No no-op message found, skipping");
      return;
    }

    // Check if report-as-issue is disabled
    if (!reportAsIssue) {
      core.info("report-as-issue is disabled (set to false), skipping no-op message posting to issue");
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

    core.info("Agent succeeded with only noop outputs - posting to no-op runs issue");

    const { owner, repo } = context.repo;

    // Ensure no-op runs issue exists
    let noopRunsIssue;
    try {
      noopRunsIssue = await ensureAgentRunsIssue();
    } catch (error) {
      core.warning(`Could not create no-op runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow if we can't create the issue
      return;
    }

    // Load comment template from file
    const commentTemplatePath = "/opt/gh-aw/prompts/noop_comment.md";
    const commentTemplate = fs.readFileSync(commentTemplatePath, "utf8");

    // Build the comment body by replacing template variables
    const commentBody = renderTemplate(commentTemplate, {
      workflow_name: workflowName,
      message: noopMessage,
      run_url: runUrl,
    });

    // Sanitize the full comment body
    const fullCommentBody = sanitizeContent(commentBody, { maxLength: 65000 });

    try {
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: noopRunsIssue.number,
        body: fullCommentBody,
      });

      core.info(`✓ Posted no-op message to no-op runs issue #${noopRunsIssue.number}`);
    } catch (error) {
      core.warning(`Failed to post comment to no-op runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow
    }
  } catch (error) {
    core.warning(`Error in handle_noop_message: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = { main, ensureAgentRunsIssue };
