// @ts-check
/// <reference types="@actions/github-script" />

const { createReportIssue } = require("./issue_report.cjs");

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_API, ERR_SYSTEM } = require("./error_codes.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { getPromptPath } = require("./messages_core.cjs");

/**
 * Classify the warning/failure reasons emitted by parse_threat_detection_results.
 * Other detection metadata (such as detection_skipped) is not a problem reason.
 * @param {string} reason
 * @returns {{category: string, reason: string, description: string}}
 */
function classifyDetectionOutcome(reason) {
  switch (reason) {
    case "threat_detected":
      return {
        category: "threat_finding",
        reason,
        description: "The detector reported a potential security threat. This is a finding, not a detector execution failure.",
      };
    case "agent_failure":
      return {
        category: "detector_execution_failure",
        reason,
        description: "The detector failed to complete. Security assessment is incomplete; this is not evidence of a threat or a clean result.",
      };
    case "parse_error":
      return {
        category: "detector_result_invalid",
        reason,
        description: "The detector did not produce a valid result. Security assessment is incomplete; this is not evidence of a threat or a clean result.",
      };
    default:
      return {
        category: "detection_outcome_unknown",
        reason: "unknown",
        description: "Detection produced a warning or failure without a recognized reason. Security assessment remains unconfirmed.",
      };
  }
}
/**
 * Search for or create the parent issue for all agentic workflow detection runs.
 * @returns {Promise<{number: number, node_id: string}>} Parent issue number and node ID
 */
async function ensureDetectionRunsIssue() {
  const { owner, repo } = context.repo;
  const parentTitle = "[aw] Detection Runs";
  const parentLabel = "agentic-workflows";

  core.info(`Searching for detection runs issue: "${parentTitle}"`);

  // Search for existing detection runs issue
  const searchQuery = `repo:${owner}/${repo} is:issue is:open label:${parentLabel} in:title "${parentTitle}"`;

  try {
    const { data } = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (data.total_count > 0) {
      const existingIssue = data.items[0];
      core.info(`Found existing detection runs issue #${existingIssue.number}: ${existingIssue.html_url}`);

      return {
        number: existingIssue.number,
        node_id: existingIssue.node_id,
      };
    }
  } catch (error) {
    throw new Error(`${ERR_API}: Failed to search for existing detection runs issue: ${getErrorMessage(error)}`, { cause: error });
  }

  // Create detection runs issue if it doesn't exist
  core.info(`No detection runs issue found, creating one`);

  // Load template from file
  const templatePath = getPromptPath("detection_runs_issue.md");
  let parentBodyContent;
  try {
    parentBodyContent = fs.readFileSync(templatePath, "utf8");
  } catch (err) {
    throw new Error(`${ERR_SYSTEM}: Failed to read file ${templatePath}: ${getErrorMessage(err)}`, { cause: err });
  }

  const { data: newIssue } = await createReportIssue(
    github,
    {
      owner,
      repo,
      title: parentTitle,
      labels: [parentLabel],
    },
    {
      type: "report",
      title: parentTitle,
      agentTainted: false,
      children: [...parseReportMarkdown(parentBodyContent, { agentTainted: false }), { type: "expiration", expiresAt: new Date(Date.now() + 24 * 30 * 60 * 60 * 1000).toISOString(), agentTainted: false }],
    }
  );

  core.info(`✓ Created detection runs issue #${newIssue.number}: ${newIssue.html_url}`);
  return {
    number: newIssue.number,
    node_id: newIssue.node_id,
  };
}

/**
 * Log detection problems (warnings and failures) as comments on a tracking issue.
 * Similar to the noop handler, this step posts a comment to the "[aw] Detection Runs"
 * issue when threat detection produces a non-success conclusion.
 */
async function main() {
  try {
    const rawConclusion = process.env.GH_AW_DETECTION_CONCLUSION || "";
    const detectionConclusion = ["success", "skipped", "cancelled", "warning", "failure"].includes(rawConclusion) ? rawConclusion : "unknown";
    const detectionReason = process.env.GH_AW_DETECTION_REASON || "";
    const workflowName = process.env.GH_AW_WORKFLOW_NAME || "unknown";
    const runUrl = process.env.GH_AW_RUN_URL || "";
    const outcome = classifyDetectionOutcome(detectionReason);

    core.info(`Detection conclusion: ${detectionConclusion}`);
    core.info(`Detection category: ${outcome.category}`);
    core.info(`Workflow name: ${workflowName}`);
    core.info(`Run URL: ${runUrl}`);

    // Only log when detection produced a warning or failure
    if (detectionConclusion !== "warning" && detectionConclusion !== "failure") {
      core.info("Detection did not produce a warning or failure, no issue comment needed");
      return;
    }

    core.info(`Detection problem detected (${detectionConclusion}), posting to tracking issue`);

    const { owner, repo } = context.repo;

    // Ensure detection runs issue exists
    let detectionRunsIssue;
    try {
      detectionRunsIssue = await ensureDetectionRunsIssue();
    } catch (error) {
      core.warning(`Could not create detection runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow if we can't create the issue
      return;
    }

    // Load and render comment template from file
    const commentTemplatePath = getPromptPath("detection_runs_comment.md");
    const children = parseReportMarkdown(fs.readFileSync(commentTemplatePath, "utf8"), {
      context: {
        workflow_name: workflowName,
        conclusion: detectionConclusion,
        reason: outcome.reason,
        category: outcome.category,
        description: outcome.description,
        run_url: runUrl,
      },
      minimumHeadingLevel: 2,
      agentTainted: false,
    });
    const fullCommentBody = renderSizedReport({ type: "report", title: "Detection run", children, agentTainted: false }, 65000).body;

    try {
      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: detectionRunsIssue.number,
        body: fullCommentBody,
      });

      core.info(`✓ Posted detection run comment to issue #${detectionRunsIssue.number}`);
    } catch (error) {
      core.warning(`Failed to post comment to detection runs issue: ${getErrorMessage(error)}`);
      // Don't fail the workflow
    }
  } catch (error) {
    core.warning(`Error in handle_detection_runs: ${getErrorMessage(error)}`);
    // Don't fail the workflow
  }
}

module.exports = { main, ensureDetectionRunsIssue, classifyDetectionOutcome };
