// @ts-check
/// <reference types="@actions/github-script" />

const { getErrorMessage } = require("./error_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { getFooterAgentFailureIssueMessage, getFooterAgentFailureCommentMessage, generateXMLMarker } = require("./messages.cjs");
const { renderTemplate } = require("./messages_core.cjs");
const { getCurrentBranch } = require("./get_current_branch.cjs");
const { createExpirationLine, generateFooterWithExpiration } = require("./ephemerals.cjs");
const { MAX_SUB_ISSUES, getSubIssueCount } = require("./sub_issue_helpers.cjs");
const { formatMissingData } = require("./missing_info_formatter.cjs");
const fs = require("fs");

/**
 * Attempt to find a pull request for the current branch
 * @returns {Promise<{number: number, html_url: string} | null>} PR info or null if not found
 */
async function findPullRequestForCurrentBranch() {
  try {
    const { owner, repo } = context.repo;
    const currentBranch = getCurrentBranch();

    core.info(`Searching for pull request from branch: ${currentBranch}`);

    // Search for open PRs with the current branch as head
    const searchQuery = `repo:${owner}/${repo} is:pr is:open head:${currentBranch}`;

    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const pr = searchResult.data.items[0];
      core.info(`Found pull request #${pr.number}: ${pr.html_url}`);
      return {
        number: pr.number,
        html_url: pr.html_url,
      };
    }

    core.info(`No pull request found for branch: ${currentBranch}`);
    return null;
  } catch (error) {
    core.warning(`Failed to find pull request for current branch: ${getErrorMessage(error)}`);
    return null;
  }
}

/**
 * Search for or create the parent issue for all agentic workflow failures
 * @param {number|null} previousParentNumber - Previous parent issue number if creating due to limit
 * @returns {Promise<{number: number, node_id: string}>} Parent issue number and node ID
 */
async function ensureParentIssue(previousParentNumber = null) {
  const { owner, repo } = context.repo;
  const parentTitle = "[agentics] Failed runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for parent issue: "${parentTitle}"`);

  // Search for existing parent issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info(`Found existing parent issue #${existingIssue.number}: ${existingIssue.html_url}`);

      // Check the sub-issue count
      const subIssueCount = await getSubIssueCount(owner, repo, existingIssue.number);

      if (subIssueCount !== null && subIssueCount >= MAX_SUB_ISSUES) {
        core.warning(`Parent issue #${existingIssue.number} has ${subIssueCount} sub-issues (max: ${MAX_SUB_ISSUES})`);
        core.info(`Creating a new parent issue (previous parent #${existingIssue.number} is full)`);

        // Fall through to create a new parent issue, passing the previous parent number
        previousParentNumber = existingIssue.number;
      } else {
        // Parent issue is within limits, return it
        if (subIssueCount !== null) {
          core.info(`Parent issue has ${subIssueCount} sub-issues (within limit of ${MAX_SUB_ISSUES})`);
        }
        return {
          number: existingIssue.number,
          node_id: existingIssue.node_id,
        };
      }
    }
  } catch (error) {
    core.warning(`Error searching for parent issue: ${getErrorMessage(error)}`);
  }

  // Create parent issue if it doesn't exist or if previous one is full
  const creationReason = previousParentNumber ? `creating new parent (previous #${previousParentNumber} reached limit)` : "creating first parent";
  core.info(`No suitable parent issue found, ${creationReason}`);

  let parentBodyContent = `This issue tracks all failures from agentic workflows in this repository. Each failed workflow run creates a sub-issue linked here for organization and easy filtering.`;

  // Add reference to previous parent if this is a continuation
  if (previousParentNumber) {
    parentBodyContent += `

> **Note:** This is a continuation parent issue. The previous parent issue #${previousParentNumber} reached the maximum of ${MAX_SUB_ISSUES} sub-issues.`;
  }

  parentBodyContent += `

### Purpose

This parent issue helps you:
- View all workflow failures in one place by checking the sub-issues below
- Filter out failure issues from your main issue list using \`no:parent-issue\`
- Track the health of your agentic workflows over time

### Sub-Issues

All individual workflow failure issues are linked as sub-issues below. Click on any sub-issue to see details about a specific failure.

### Troubleshooting Failed Workflows

#### Using agentic-workflows Agent (Recommended)

**Agent:** \`agentic-workflows\`  
**Purpose:** Debug and fix workflow failures

**Instructions:**

1. Invoke the agent: Type \`/agent\` in GitHub Copilot Chat and select **agentic-workflows**
2. Provide context: Tell the agent to **debug** the workflow failure
3. Supply the workflow run URL for analysis
4. The agent will:
   - Analyze failure logs
   - Identify root causes
   - Propose specific fixes
   - Validate solutions

#### Using gh aw CLI

You can also debug failures using the \`gh aw\` CLI:

\`\`\`bash
# Download and analyze workflow logs
gh aw logs <workflow-run-url>

# Audit a specific workflow run
gh aw audit <run-id>
\`\`\`

#### Manual Investigation

1. Click on a sub-issue to see the failed workflow details
2. Follow the workflow run link in the issue
3. Review the agent job logs for error messages
4. Check the workflow configuration in your repository

### Resources

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)
- [Troubleshooting Guide](https://github.github.io/gh-aw/troubleshooting/common-issues/)

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.`;

  // Add expiration marker (7 days from now) inside the quoted section using helper
  const footer = generateFooterWithExpiration({
    footerText: parentBodyContent,
    expiresHours: 24 * 7, // 7 days
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

    core.info(`✓ Created parent issue #${newIssue.data.number}: ${newIssue.data.html_url}`);
    return {
      number: newIssue.data.number,
      node_id: newIssue.data.node_id,
    };
  } catch (error) {
    core.error(`Failed to create parent issue: ${getErrorMessage(error)}`);
    throw error;
  }
}

/**
 * Link an issue as a sub-issue to a parent issue
 * @param {string} parentNodeId - GraphQL node ID of the parent issue
 * @param {string} subIssueNodeId - GraphQL node ID of the sub-issue
 * @param {number} parentNumber - Parent issue number (for logging)
 * @param {number} subIssueNumber - Sub-issue number (for logging)
 */
async function linkSubIssue(parentNodeId, subIssueNodeId, parentNumber, subIssueNumber) {
  core.info(`Linking issue #${subIssueNumber} as sub-issue of #${parentNumber}`);

  try {
    // Use GraphQL to link the sub-issue
    await github.graphql(
      `mutation($parentId: ID!, $subIssueId: ID!) {
        addSubIssue(input: {issueId: $parentId, subIssueId: $subIssueId}) {
          issue {
            id
            number
          }
          subIssue {
            id
            number
          }
        }
      }`,
      {
        parentId: parentNodeId,
        subIssueId: subIssueNodeId,
      }
    );

    core.info(`✓ Successfully linked #${subIssueNumber} as sub-issue of #${parentNumber}`);
  } catch (error) {
    const errorMessage = getErrorMessage(error);
    if (errorMessage.includes("Field 'addSubIssue' doesn't exist") || errorMessage.includes("not yet available")) {
      core.warning(`Sub-issue API not available. Issue #${subIssueNumber} created but not linked to parent.`);
    } else {
      core.warning(`Failed to link sub-issue: ${errorMessage}`);
    }
  }
}

/**
 * Build create_discussion errors context string from error environment variable
 * @param {string} createDiscussionErrors - Newline-separated error strings
 * @returns {string} Formatted error context for display
 */
function buildCreateDiscussionErrorsContext(createDiscussionErrors) {
  if (!createDiscussionErrors) {
    return "";
  }

  let context = "\n**⚠️ Create Discussion Failed**: Failed to create one or more discussions.\n\n**Discussion Errors:**\n";
  const errorLines = createDiscussionErrors.split("\n").filter(line => line.trim());
  for (const errorLine of errorLines) {
    const parts = errorLine.split(":");
    if (parts.length >= 4) {
      // parts[0] is "discussion", parts[1] is index - both unused
      const repo = parts[2];
      const title = parts[3];
      const error = parts.slice(4).join(":"); // Rest is the error message
      context += `- Discussion "${title}" in ${repo}: ${error}\n`;
    }
  }
  context += "\n";
  return context;
}

/**
 * Load missing_data messages from agent output
 * @returns {Array<{data_type: string, reason: string, context?: string, alternatives?: string}>} Array of missing data messages
 */
function loadMissingDataMessages() {
  try {
    const { loadAgentOutput } = require("./load_agent_output.cjs");
    const agentOutputResult = loadAgentOutput();

    if (!agentOutputResult.success || !agentOutputResult.items) {
      return [];
    }

    // Extract missing_data messages from agent output
    const missingDataMessages = [];
    for (const item of agentOutputResult.items) {
      if (item.type === "missing_data") {
        // Extract the fields we need
        if (item.data_type && item.reason) {
          missingDataMessages.push({
            data_type: item.data_type,
            reason: item.reason,
            context: item.context || null,
            alternatives: item.alternatives || null,
          });
        }
      }
    }

    return missingDataMessages;
  } catch (error) {
    core.warning(`Failed to load missing_data messages: ${getErrorMessage(error)}`);
    return [];
  }
}

/**
 * Build missing_data context string for display in failure issues/comments
 * @returns {string} Formatted missing data context
 */
function buildMissingDataContext() {
  const missingDataMessages = loadMissingDataMessages();

  if (missingDataMessages.length === 0) {
    return "";
  }

  core.info(`Found ${missingDataMessages.length} missing_data message(s)`);

  // Format the missing data using the existing formatter
  const formattedList = formatMissingData(missingDataMessages);

  let context = "\n**⚠️ Missing Data Reported**: The agent reported missing data during execution.\n\n**Missing Data:**\n";
  context += formattedList;
  context += "\n\n";

  return context;
}

/**
 * Handle agent job failure by creating or updating a failure tracking issue
 * This script is called from the conclusion job when the agent job has failed
 * or when the agent succeeded but produced no safe outputs
 */
async function main() {
  try {
    // Get workflow context
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const workflowID = process.env.GH_AW_WORKFLOW_ID || "unknown";
    const agentConclusion = process.env.GH_AW_AGENT_CONCLUSION || "";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const workflowSource = process.env.GH_AW_WORKFLOW_SOURCE || "";
    const workflowSourceURL = process.env.GH_AW_WORKFLOW_SOURCE_URL || "";
    const secretVerificationResult = process.env.GH_AW_SECRET_VERIFICATION_RESULT || "";
    const assignmentErrors = process.env.GH_AW_ASSIGNMENT_ERRORS || "";
    const assignmentErrorCount = process.env.GH_AW_ASSIGNMENT_ERROR_COUNT || "0";
    const createDiscussionErrors = process.env.GH_AW_CREATE_DISCUSSION_ERRORS || "";
    const createDiscussionErrorCount = process.env.GH_AW_CREATE_DISCUSSION_ERROR_COUNT || "0";
    const checkoutPRSuccess = process.env.GH_AW_CHECKOUT_PR_SUCCESS || "";

    core.info(`Agent conclusion: ${agentConclusion}`);
    core.info(`Workflow name: ${workflowName}`);
    core.info(`Workflow ID: ${workflowID}`);
    core.info(`Secret verification result: ${secretVerificationResult}`);
    core.info(`Assignment error count: ${assignmentErrorCount}`);
    core.info(`Create discussion error count: ${createDiscussionErrorCount}`);
    core.info(`Checkout PR success: ${checkoutPRSuccess}`);

    // Check if there are assignment errors (regardless of agent job status)
    const hasAssignmentErrors = parseInt(assignmentErrorCount, 10) > 0;

    // Check if there are create_discussion errors (regardless of agent job status)
    const hasCreateDiscussionErrors = parseInt(createDiscussionErrorCount, 10) > 0;

    // Check if agent succeeded but produced no safe outputs
    let hasMissingSafeOutputs = false;
    let hasOnlyNoopOutputs = false;
    if (agentConclusion === "success") {
      const { loadAgentOutput } = require("./load_agent_output.cjs");
      const agentOutputResult = loadAgentOutput();

      if (!agentOutputResult.success || !agentOutputResult.items || agentOutputResult.items.length === 0) {
        hasMissingSafeOutputs = true;
        core.info("Agent succeeded but produced no safe outputs");
      } else {
        // Check if all outputs are noop types
        const nonNoopItems = agentOutputResult.items.filter(item => item.type !== "noop");
        if (nonNoopItems.length === 0) {
          hasOnlyNoopOutputs = true;
          core.info("Agent succeeded with only noop outputs - this is not a failure");
        }
      }
    }

    // Only proceed if the agent job actually failed OR there are assignment errors OR create_discussion errors OR missing safe outputs
    // BUT skip if we only have noop outputs (that's a successful no-action scenario)
    if (agentConclusion !== "failure" && !hasAssignmentErrors && !hasCreateDiscussionErrors && !hasMissingSafeOutputs) {
      core.info(`Agent job did not fail and no assignment/discussion errors and has safe outputs (conclusion: ${agentConclusion}), skipping failure handling`);
      return;
    }

    // If we only have noop outputs, skip failure handling - this is a successful no-action scenario
    if (hasOnlyNoopOutputs) {
      core.info("Agent completed with only noop outputs - skipping failure handling");
      return;
    }

    // Check if the failure was due to PR checkout (e.g., PR was merged and branch deleted)
    // If checkout_pr_success is "false", skip creating an issue as this is expected behavior
    if (agentConclusion === "failure" && checkoutPRSuccess === "false") {
      core.info("Skipping failure handling - failure was due to PR checkout (likely PR merged)");
      return;
    }

    const { owner, repo } = context.repo;

    // Try to find a pull request for the current branch
    const pullRequest = await findPullRequestForCurrentBranch();

    // Ensure parent issue exists first
    let parentIssue;
    try {
      parentIssue = await ensureParentIssue();
    } catch (error) {
      core.warning(`Could not create parent issue, proceeding without parent: ${getErrorMessage(error)}`);
      // Continue without parent issue
    }

    // Sanitize workflow name for title
    const sanitizedWorkflowName = sanitizeContent(workflowName, { maxLength: 100 });
    const issueTitle = `[agentics] ${sanitizedWorkflowName} failed`;

    core.info(`Checking for existing issue with title: "${issueTitle}"`);

    // Search for existing open issue with this title and label
    const searchQuery = `repo:${owner}/${repo} is:issue is:open label:agentic-workflows in:title "${issueTitle}"`;

    try {
      const searchResult = await github.rest.search.issuesAndPullRequests({
        q: searchQuery,
        per_page: 1,
      });

      if (searchResult.data.total_count > 0) {
        // Issue exists, add a comment
        const existingIssue = searchResult.data.items[0];
        core.info(`Found existing issue #${existingIssue.number}: ${existingIssue.html_url}`);

        // Read comment template
        const commentTemplatePath = "/opt/gh-aw/prompts/agent_failure_comment.md";
        const commentTemplate = fs.readFileSync(commentTemplatePath, "utf8");

        // Extract run ID from URL (e.g., https://github.com/owner/repo/actions/runs/123 -> "123")
        let runId = "";
        const runIdMatch = runUrl.match(/\/actions\/runs\/(\d+)/);
        if (runIdMatch) {
          runId = runIdMatch[1];
        }

        // Build assignment errors context
        let assignmentErrorsContext = "";
        if (hasAssignmentErrors && assignmentErrors) {
          assignmentErrorsContext = "\n**⚠️ Agent Assignment Failed**: Failed to assign agent to issues due to insufficient permissions or missing token.\n\n**Assignment Errors:**\n";
          const errorLines = assignmentErrors.split("\n").filter(line => line.trim());
          for (const errorLine of errorLines) {
            const parts = errorLine.split(":");
            if (parts.length >= 4) {
              const type = parts[0]; // "issue" or "pr"
              const number = parts[1];
              const agent = parts[2];
              const error = parts.slice(3).join(":"); // Rest is the error message
              assignmentErrorsContext += `- ${type === "issue" ? "Issue" : "PR"} #${number} (agent: ${agent}): ${error}\n`;
            }
          }
          assignmentErrorsContext += "\n";
        }

        // Build create_discussion errors context
        const createDiscussionErrorsContext = hasCreateDiscussionErrors ? buildCreateDiscussionErrorsContext(createDiscussionErrors) : "";

        // Build missing_data context
        const missingDataContext = buildMissingDataContext();

        // Build missing safe outputs context
        let missingSafeOutputsContext = "";
        if (hasMissingSafeOutputs) {
          missingSafeOutputsContext = "\n**⚠️ No Safe Outputs Generated**: The agent job succeeded but did not produce any safe outputs. This typically indicates:\n";
          missingSafeOutputsContext += "- The safe output server failed to run\n";
          missingSafeOutputsContext += "- The prompt failed to generate any meaningful result\n";
          missingSafeOutputsContext += "- The agent should have called `noop` to explicitly indicate no action was taken\n\n";
        }

        // Create template context
        const templateContext = {
          run_url: runUrl,
          run_id: runId,
          workflow_name: workflowName,
          workflow_source: workflowSource,
          workflow_source_url: workflowSourceURL,
          secret_verification_failed: String(secretVerificationResult === "failed"),
          secret_verification_context:
            secretVerificationResult === "failed"
              ? "\n**⚠️ Secret Verification Failed**: The workflow's secret validation step failed. Please check that the required secrets are configured in your repository settings.\n\nFor more information on configuring tokens, see: https://github.github.io/gh-aw/reference/engines/\n"
              : "",
          assignment_errors_context: assignmentErrorsContext,
          create_discussion_errors_context: createDiscussionErrorsContext,
          missing_data_context: missingDataContext,
          missing_safe_outputs_context: missingSafeOutputsContext,
        };

        // Render the comment template
        const commentBody = renderTemplate(commentTemplate, templateContext);

        // Generate footer for the comment using templated message
        const ctx = {
          workflowName,
          runUrl,
          workflowSource,
          workflowSourceUrl: workflowSourceURL,
        };
        const footer = getFooterAgentFailureCommentMessage(ctx);

        // Combine comment body with footer
        const fullCommentBody = sanitizeContent(commentBody + "\n\n" + footer, { maxLength: 65000 });

        await github.rest.issues.createComment({
          owner,
          repo,
          issue_number: existingIssue.number,
          body: fullCommentBody,
        });

        core.info(`✓ Added comment to existing issue #${existingIssue.number}`);
      } else {
        // No existing issue, create a new one
        core.info("No existing issue found, creating a new one");

        // Read issue template
        const issueTemplatePath = "/opt/gh-aw/prompts/agent_failure_issue.md";
        const issueTemplate = fs.readFileSync(issueTemplatePath, "utf8");

        // Get current branch information
        const currentBranch = getCurrentBranch();

        // Build assignment errors context
        let assignmentErrorsContext = "";
        if (hasAssignmentErrors && assignmentErrors) {
          assignmentErrorsContext = "\n**⚠️ Agent Assignment Failed**: Failed to assign agent to issues due to insufficient permissions or missing token.\n\n**Assignment Errors:**\n";
          const errorLines = assignmentErrors.split("\n").filter(line => line.trim());
          for (const errorLine of errorLines) {
            const parts = errorLine.split(":");
            if (parts.length >= 4) {
              const type = parts[0]; // "issue" or "pr"
              const number = parts[1];
              const agent = parts[2];
              const error = parts.slice(3).join(":"); // Rest is the error message
              assignmentErrorsContext += `- ${type === "issue" ? "Issue" : "PR"} #${number} (agent: ${agent}): ${error}\n`;
            }
          }
          assignmentErrorsContext += "\n";
        }

        // Build create_discussion errors context
        const createDiscussionErrorsContext = hasCreateDiscussionErrors ? buildCreateDiscussionErrorsContext(createDiscussionErrors) : "";

        // Build missing_data context
        const missingDataContext = buildMissingDataContext();

        // Build missing safe outputs context
        let missingSafeOutputsContext = "";
        if (hasMissingSafeOutputs) {
          missingSafeOutputsContext = "\n**⚠️ No Safe Outputs Generated**: The agent job succeeded but did not produce any safe outputs. This typically indicates:\n";
          missingSafeOutputsContext += "- The safe output server failed to run\n";
          missingSafeOutputsContext += "- The prompt failed to generate any meaningful result\n";
          missingSafeOutputsContext += "- The agent should have called `noop` to explicitly indicate no action was taken\n\n";
        }

        // Create template context with sanitized workflow name
        const templateContext = {
          workflow_name: sanitizedWorkflowName,
          workflow_id: workflowID,
          run_url: runUrl,
          workflow_source_url: workflowSourceURL || "#",
          branch: currentBranch,
          pull_request_info: pullRequest ? `  \n**Pull Request:** [#${pullRequest.number}](${pullRequest.html_url})` : "",
          secret_verification_failed: String(secretVerificationResult === "failed"),
          secret_verification_context:
            secretVerificationResult === "failed"
              ? "\n**⚠️ Secret Verification Failed**: The workflow's secret validation step failed. Please check that the required secrets are configured in your repository settings.\n\nFor more information on configuring tokens, see: https://github.github.io/gh-aw/reference/engines/\n"
              : "",
          assignment_errors_context: assignmentErrorsContext,
          create_discussion_errors_context: createDiscussionErrorsContext,
          missing_data_context: missingDataContext,
          missing_safe_outputs_context: missingSafeOutputsContext,
        };

        // Render the issue template
        const issueBodyContent = renderTemplate(issueTemplate, templateContext);

        // Generate footer for the issue using templated message
        const ctx = {
          workflowName,
          runUrl,
          workflowSource,
          workflowSourceUrl: workflowSourceURL,
        };
        const footer = getFooterAgentFailureIssueMessage(ctx);

        // Add expiration marker (7 days from now) inside the quoted footer section using helper
        const footerWithExpires = generateFooterWithExpiration({
          footerText: footer,
          expiresHours: 24 * 7, // 7 days
          suffix: `\n\n${generateXMLMarker(workflowName, runUrl)}`,
        });

        // Combine issue body with footer
        const bodyLines = [issueBodyContent, "", footerWithExpires];
        const issueBody = bodyLines.join("\n");

        const newIssue = await github.rest.issues.create({
          owner,
          repo,
          title: issueTitle,
          body: issueBody,
          labels: ["agentic-workflows"],
        });

        core.info(`✓ Created new issue #${newIssue.data.number}: ${newIssue.data.html_url}`);

        // Link as sub-issue to parent if parent issue was created
        if (parentIssue) {
          try {
            await linkSubIssue(parentIssue.node_id, newIssue.data.node_id, parentIssue.number, newIssue.data.number);
          } catch (error) {
            core.warning(`Could not link issue as sub-issue: ${getErrorMessage(error)}`);
            // Continue even if linking fails
          }
        }
      }
    } catch (error) {
      core.warning(`Failed to create or update failure tracking issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow if we can't create the issue
    }
  } catch (error) {
    core.warning(`Error in handle_agent_failure: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = { main };
