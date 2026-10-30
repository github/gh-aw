// @ts-check
/// <reference types="@actions/github-script" />

const { createReportIssue } = require("./issue_report.cjs");

const { getErrorMessage } = require("./error_helpers.cjs");
const { getAgenticWorkflowMetadata } = require("./messages_footer.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");

const MAX_VALIDATION_OUTPUT_LENGTH = 50000;

/**
 * Truncate and sanitize validation output for safe inclusion in an issue or comment body.
 * @param {string} output - Raw combined stdout/stderr output
 * @param {number} [maxLength] - Maximum length before truncation
 * @returns {string} Sanitized, possibly truncated output
 */
function prepareValidationOutput(output, maxLength = MAX_VALIDATION_OUTPUT_LENGTH) {
  const truncated = output.substring(0, maxLength) + (output.length > maxLength ? "\n\n... (output truncated)" : "");
  return sanitizeContent(truncated);
}

/**
 * Run full workflow validation using gh-aw compile --validate and all known
 * checkers (zizmor, actionlint, poutine). If any errors or warnings are found,
 * file a GitHub issue with the findings.
 *
 * Required environment variables:
 *   GH_AW_CMD_PREFIX - Command prefix: './gh-aw' (dev) or 'gh aw' (release)
 *
 * @returns {Promise<void>}
 */
async function main() {
  const owner = context.repo.owner;
  const repo = context.repo.repo;

  const cmdPrefixStr = process.env.GH_AW_CMD_PREFIX || "gh aw";
  const [bin, ...prefixArgs] = cmdPrefixStr.split(" ").filter(Boolean);

  core.info("Running full workflow validation with all known checkers");

  // Run: gh aw compile --validate --no-emit --zizmor --actionlint --poutine --verbose
  const validateArgs = [...prefixArgs, "compile", "--validate", "--no-emit", "--zizmor", "--actionlint", "--poutine", "--verbose"];
  const fullCmd = [bin, ...validateArgs].join(" ");
  core.info(`Running: ${fullCmd}`);

  let stdout = "";
  let stderr = "";
  let exitCode = 0;

  try {
    exitCode = await exec.exec(bin, validateArgs, {
      ignoreReturnCode: true,
      listeners: {
        stdout: data => {
          stdout += data.toString();
        },
        stderr: data => {
          stderr += data.toString();
        },
      },
    });
  } catch (error) {
    core.error(`Validation command failed: ${getErrorMessage(error)}`);
    throw error;
  }

  const combinedOutput = (stderr + "\n" + stdout).trim();

  // Check if there were any errors or warnings
  const hasErrors = exitCode !== 0;
  const hasWarnings = /\bwarn(ing)?\b/i.test(combinedOutput);

  if (!hasErrors && !hasWarnings) {
    core.info("✓ All workflow validations passed with no errors or warnings");
    return;
  }

  if (hasErrors) {
    core.warning(`Validation exited with code ${exitCode}`);
  }
  if (hasWarnings) {
    core.warning("Validation produced warnings");
  }

  // Search for existing open issue about workflow validation
  const issueTitle = "[aw] workflow validation findings";
  const searchQuery = `repo:${owner}/${repo} is:issue is:open in:title "${issueTitle}"`;

  core.info(`Searching for existing issue with title: "${issueTitle}"`);

  const runUrl = buildWorkflowRunUrl(context, context.repo);
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Agentic Maintenance";
  const repository = `${owner}/${repo}`;

  try {
    const searchResult = await github.rest.search.issuesAndPullRequests({
      q: searchQuery,
      per_page: 1,
    });

    if (searchResult.data.total_count > 0) {
      const existingIssue = searchResult.data.items[0];
      core.info(`Found existing issue #${existingIssue.number}: ${existingIssue.html_url}`);

      const sanitizedOutput = prepareValidationOutput(combinedOutput);

      const commentBody = renderSizedReport(buildValidationReport({ title: issueTitle, output: sanitizedOutput, exitCode, runUrl, workflowName, fullCmd, cmdPrefixStr, repository, comment: true })).body;

      await github.rest.issues.createComment({
        owner,
        repo,
        issue_number: existingIssue.number,
        body: commentBody,
      });

      core.info(`✓ Added comment to existing issue #${existingIssue.number}`);

      if (hasErrors) {
        core.setFailed(`Workflow validation failed. See issue #${existingIssue.number}`);
      }
      return;
    }
  } catch (error) {
    core.error(`Failed to search for existing issues: ${getErrorMessage(error)}`);
    throw error;
  }

  // No existing issue found, create a new one
  core.info("No existing issue found, creating a new issue with validation findings");

  const sanitizedOutput = prepareValidationOutput(combinedOutput);

  const report = buildValidationReport({ title: issueTitle, output: sanitizedOutput, exitCode, runUrl, workflowName, fullCmd, cmdPrefixStr, repository, comment: false });

  try {
    const newIssue = await createReportIssue(
      github,
      {
        owner,
        repo,
        title: issueTitle,
        labels: ["agentic-workflows", "maintenance"],
      },
      report
    );

    core.info(`✓ Created issue #${newIssue.data.number}: ${newIssue.data.html_url}`);

    await core.summary.addHeading("Workflow Validation Findings", 2).addRaw(`Created issue [#${newIssue.data.number}](${newIssue.data.html_url}) to track validation findings.`).write();
  } catch (error) {
    core.error(`Failed to create issue: ${getErrorMessage(error)}`);
    throw error;
  }

  if (hasErrors) {
    core.setFailed("Workflow validation failed. See created issue for details.");
  }
}

/**
 * @param {{title: string, output: string, exitCode: number, runUrl: string, workflowName: string, fullCmd: string, cmdPrefixStr: string, repository: string, comment: boolean}} options
 * @returns {import("./report_ast.cjs").Report}
 */
function buildValidationReport({ title, output, exitCode, runUrl, workflowName, fullCmd, cmdPrefixStr, repository, comment }) {
  /** @param {string} value @returns {import("./report_ast.cjs").ReportInline} */
  const text = value => ({ type: "text", value, agentTainted: false });
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const paragraph = value => ({ type: "paragraph", children: [text(value)], agentTainted: false });
  /** @param {string} value @returns {import("./report_ast.cjs").ReportBlock} */
  const heading = value => ({ type: "heading", level: 2, children: [text(value)], agentTainted: false });
  const [workflow, ...fields] = getAgenticWorkflowMetadata(workflowName, runUrl);
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const outputNodes = [{ type: "details", summary: comment ? "Validation output" : "Full output", children: [{ type: "code_block", language: "", value: output, agentTainted: true }], agentTainted: false }];
  /** @type {import("./report_ast.cjs").ReportBlock} */
  const runLink = { type: "paragraph", children: [{ type: "link", url: runUrl, children: [text(comment ? "View workflow run" : "View run")], agentTainted: false }], agentTainted: false };
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const children = comment
    ? [paragraph(`Validation still has findings (exit code: ${exitCode}).`), ...outputNodes, runLink]
    : [
        heading("Problem"),
        paragraph("Workflow validation found errors or warnings that need to be addressed."),
        {
          type: "paragraph",
          children: [{ type: "strong", children: [text("Validation command:")] }, text(" "), { type: "code", value: fullCmd }, { type: "soft_break" }, { type: "strong", children: [text("Exit code:")] }, text(` ${exitCode}`)],
        },
        heading("Validation Output"),
        ...outputNodes,
        heading("Instructions"),
        paragraph("Fix the reported issues and re-run validation:"),
        { type: "code_block", language: "bash", value: `${cmdPrefixStr} compile --validate --no-emit --zizmor --actionlint --poutine --verbose` },
        paragraph("Or use the validate shorthand:"),
        { type: "code_block", language: "bash", value: `${cmdPrefixStr} validate` },
        heading("References"),
        { type: "list", ordered: false, items: [[paragraph(`Repository: ${repository}`)], [{ ...runLink, children: [text("Workflow run: "), ...runLink.children] }]] },
      ];
  return {
    type: "report",
    title,
    agentTainted: false,
    children: [...children, { type: "separator", agentTainted: false }, { type: "xml_marker", name: "gh-aw-agentic-workflow", value: workflow.value, fields, agentTainted: false }],
  };
}

module.exports = { main, prepareValidationOutput };
