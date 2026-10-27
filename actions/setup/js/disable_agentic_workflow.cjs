// @ts-check
/// <reference types="@actions/github-script" />

// @safe-outputs-exempt SEC-004 — the issue body is only parsed for structured XML comment markers
// (<!-- gh-aw-workflow-id: ... -->); all createComment bodies are hardcoded template strings and
// never reflect raw user-controlled content.

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { ensureLabelExists, validateLabeledIssueEvent, removeLabelSafely } = require("./label_trigger_helpers.cjs");
const { extractWorkflowId, isValidWorkflowId } = require("./generate_footer.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const DISABLE_LABEL = "agentic-workflows:disable";
const DISABLE_LABEL_COLOR = "8250df"; // GitHub purple
const DISABLE_LABEL_DESCRIPTION = "Disable the agentic workflow that created this issue";

/**
 * Disable an agentic workflow when the "agentic-workflows:disable" label is applied to an issue.
 *
 * Reads the labeled issue body to extract the workflow_id from XML comment markers,
 * disables the corresponding agentic workflow via the GitHub REST API, and posts a comment
 * confirming the action.
 *
 * @returns {Promise<void>}
 */
async function main() {
  const ctx = validateLabeledIssueEvent(DISABLE_LABEL);
  if (!ctx) return;

  const { owner, repo, issueNumber, body } = ctx;

  // Ensure the disable label exists so it is available for future use
  await ensureLabelExists(owner, repo, DISABLE_LABEL, DISABLE_LABEL_COLOR, DISABLE_LABEL_DESCRIPTION);

  core.info(`Processing issue #${issueNumber} labeled with '${DISABLE_LABEL}'`);

  // Extract workflow ID from body XML comment markers
  const workflowId = extractWorkflowId(body);

  if (!workflowId) {
    core.warning(`Could not find workflow ID in issue #${issueNumber} body. Expected a <!-- gh-aw-workflow-id: ... --> marker.`);
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: issueNumber,
      body: renderSizedReport({
        type: "report",
        title: "Workflow disable warning",
        children: [
          {
            type: "alert",
            kind: "WARNING",
            children: [
              { type: "paragraph", children: [{ type: "strong", children: [{ type: "text", value: "Could not disable agentic workflow", agentTainted: false }] }] },
              {
                type: "paragraph",
                children: [
                  { type: "text", value: "No workflow ID marker was found in this issue's body. The ", agentTainted: false },
                  { type: "code", value: DISABLE_LABEL, agentTainted: false },
                  { type: "text", value: " label can only be used on issues that were created by an agentic workflow (they contain a ", agentTainted: false },
                  { type: "code", value: "<!-- gh-aw-workflow-id: ... -->", agentTainted: false },
                  { type: "text", value: " marker).", agentTainted: false },
                ],
              },
              {
                type: "paragraph",
                children: [
                  { type: "text", value: "To disable a workflow manually, trigger the maintenance workflow with the ", agentTainted: false },
                  { type: "code", value: "disable", agentTainted: false },
                  { type: "text", value: " operation.", agentTainted: false },
                ],
              },
            ],
          },
        ],
      }).body.trimEnd(),
    });
    core.setFailed(`${ERR_NOT_FOUND}: No workflow ID marker found in issue #${issueNumber}`);
    return;
  }

  core.info(`Found workflow ID: ${workflowId}`);
  core.info(`Disabling agentic workflow '${workflowId}'...`);

  // Disable the workflow via the GitHub REST API using its compiled lock file name
  const lockFileName = `${workflowId}.lock.yml`;
  try {
    await github.rest.actions.disableWorkflow({ owner, repo, workflow_id: lockFileName });
  } catch (err) {
    const msg = getErrorMessage(err);
    core.error(`Failed to disable workflow '${workflowId}': ${msg}`);
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: issueNumber,
      body: renderSizedReport({
        type: "report",
        title: "Workflow disable failed",
        children: [
          {
            type: "alert",
            kind: "WARNING",
            children: [
              {
                type: "paragraph",
                children: [
                  {
                    type: "strong",
                    children: [
                      { type: "text", value: "Failed to disable agentic workflow ", agentTainted: false },
                      { type: "code", value: workflowId, agentTainted: false },
                    ],
                  },
                ],
              },
              { type: "paragraph", children: [{ type: "text", value: msg, agentTainted: false }] },
              {
                type: "paragraph",
                children: [
                  { type: "text", value: "Please check the ", agentTainted: false },
                  {
                    type: "link",
                    url: `${process.env.GITHUB_SERVER_URL || "https://github.com"}/${owner}/${repo}/actions/runs/${process.env.GITHUB_RUN_ID || ""}`,
                    children: [{ type: "text", value: "workflow run logs", agentTainted: false }],
                  },
                  { type: "text", value: " for details.", agentTainted: false },
                ],
              },
            ],
          },
        ],
      }).body.trimEnd(),
    });
    core.setFailed(`Failed to disable workflow '${workflowId}': ${msg}`);
    return;
  }

  core.info(`Successfully disabled workflow '${workflowId}'`);

  // Post a success comment on the issue
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body: renderSizedReport({
      type: "report",
      title: "Workflow disabled",
      children: [
        {
          type: "paragraph",
          children: [
            { type: "text", value: "The agentic workflow ", agentTainted: false },
            { type: "code", value: workflowId, agentTainted: false },
            { type: "text", value: " has been disabled.", agentTainted: false },
          ],
        },
        {
          type: "paragraph",
          children: [
            { type: "text", value: "To re-enable it, trigger the maintenance workflow with the ", agentTainted: false },
            { type: "code", value: "enable", agentTainted: false },
            { type: "text", value: " operation.", agentTainted: false },
          ],
        },
        { type: "xml_marker", name: "gh-aw-comment-type", value: "workflow-disabled", agentTainted: false },
      ],
    }).body.trimEnd(),
  });

  core.info(`Posted disable confirmation comment on issue #${issueNumber}`);

  // Remove the disable label now that the action is complete
  await removeLabelSafely(owner, repo, issueNumber, DISABLE_LABEL);
}

module.exports = { main };
