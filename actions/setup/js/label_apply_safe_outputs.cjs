// @ts-check
/// <reference types="@actions/github-script" />

// @safe-outputs-exempt SEC-004 — the issue body is only parsed for structured XML comment markers
const { renderSizedReport } = require("./report_size.cjs");
// (<!-- gh-aw-agentic-workflow: ..., run: ... --> / <!-- gh-aw-run-url: ... -->); all
// createComment bodies are hardcoded template strings and never reflect raw user-controlled content.

const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_NOT_FOUND } = require("./error_codes.cjs");
const { ensureLabelExists, validateLabeledIssueEvent, removeLabelSafely } = require("./label_trigger_helpers.cjs");

const APPLY_SAFE_OUTPUTS_LABEL = "agentic-workflows:apply-safe-outputs";
const APPLY_SAFE_OUTPUTS_LABEL_COLOR = "8250df"; // GitHub purple
const APPLY_SAFE_OUTPUTS_LABEL_DESCRIPTION = "Re-apply the safe outputs from the agentic workflow run referenced in this issue";

/**
 * Extract a workflow run URL or numeric run ID from an issue body.
 *
 * Looks for (in priority order):
 * 1. Combined marker run field: <!-- gh-aw-agentic-workflow: ..., run: https://..., ... -->
 * 2. Combined marker id field:  <!-- gh-aw-agentic-workflow: ..., id: 12345, ... -->
 * 3. Standalone run-url marker: <!-- gh-aw-run-url: https://... -->
 *
 * @param {string|null|undefined} body - Issue body
 * @returns {string|null} Run URL or numeric run ID, or null if not found
 */
function extractRunUrl(body) {
  if (!body) return null;

  // 1. Combined marker — extract the run: field (full URL)
  const runUrlMatch = body.match(/<!--\s*gh-aw-agentic-workflow:[^>]*?\brun:\s*(https?:\/\/[^\s,>]+)/s);
  if (runUrlMatch) {
    return runUrlMatch[1].trim();
  }

  // 2. Combined marker — extract the id: field (numeric run ID)
  const idMatch = body.match(/<!--\s*gh-aw-agentic-workflow:[^>]*?\bid:\s*(\d+)/s);
  if (idMatch) {
    return idMatch[1].trim();
  }

  // 3. Standalone marker: <!-- gh-aw-run-url: https://... -->
  const standaloneMatch = body.match(/<!--\s*gh-aw-run-url:\s*(https?:\/\/[^\s>]+|[0-9]+)\s*-->/);
  if (standaloneMatch) {
    return standaloneMatch[1].trim();
  }

  return null;
}

/**
 * Re-apply safe outputs from a previous workflow run when the
 * "agentic-workflows:apply-safe-outputs" label is applied to an issue.
 *
 * Reads the labeled issue body to extract a workflow run URL or run ID from XML comment
 * markers, re-applies the safe outputs from that run, posts a success comment, and
 * removes the label.
 *
 * @returns {Promise<void>}
 */
async function main() {
  const ctx = validateLabeledIssueEvent(APPLY_SAFE_OUTPUTS_LABEL);
  if (!ctx) return;

  const { owner, repo, issueNumber, body } = ctx;

  // Ensure the label exists so it is available for future use
  await ensureLabelExists(owner, repo, APPLY_SAFE_OUTPUTS_LABEL, APPLY_SAFE_OUTPUTS_LABEL_COLOR, APPLY_SAFE_OUTPUTS_LABEL_DESCRIPTION);

  core.info(`Processing issue #${issueNumber} labeled with '${APPLY_SAFE_OUTPUTS_LABEL}'`);

  // Extract run URL from body XML comment markers
  const runUrl = extractRunUrl(body);

  if (!runUrl) {
    core.warning(`Could not find run URL in issue #${issueNumber} body.`);
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: issueNumber,
      body: renderSizedReport({
        type: "report",
        title: "Safe outputs warning",
        children: [
          {
            type: "alert",
            kind: "WARNING",
            children: [
              { type: "paragraph", children: [{ type: "strong", children: [{ type: "text", value: "Could not apply safe outputs", agentTainted: false }] }] },
              {
                type: "paragraph",
                children: [
                  { type: "text", value: "No workflow run reference was found in this issue's body. The ", agentTainted: false },
                  { type: "code", value: APPLY_SAFE_OUTPUTS_LABEL, agentTainted: false },
                  { type: "text", value: " label can only be used on issues that were created by an agentic workflow (they contain a ", agentTainted: false },
                  { type: "code", value: "<!-- gh-aw-agentic-workflow: ..., run: https://... -->", agentTainted: false },
                  { type: "text", value: " marker with a run URL).", agentTainted: false },
                ],
              },
              {
                type: "paragraph",
                children: [
                  { type: "text", value: "To apply safe outputs manually, use the maintenance workflow with the ", agentTainted: false },
                  { type: "code", value: "safe_outputs", agentTainted: false },
                  { type: "text", value: " operation and supply the run URL.", agentTainted: false },
                ],
              },
            ],
          },
        ],
      }).body.trimEnd(),
    });
    core.setFailed(`${ERR_NOT_FOUND}: No run URL marker found in issue #${issueNumber}`);
    return;
  }

  core.info(`Found run reference: ${runUrl}`);

  // Set GH_AW_RUN_URL so apply_safe_outputs_replay.cjs can consume it
  process.env.GH_AW_RUN_URL = runUrl;

  // Delegate to the existing replay driver
  const { main: replayMain } = require("./apply_safe_outputs_replay.cjs");
  try {
    await replayMain();
  } catch (err) {
    const msg = getErrorMessage(err);
    core.error(`Failed to apply safe outputs: ${msg}`);
    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: issueNumber,
      body: renderSizedReport({
        type: "report",
        title: "Safe outputs failed",
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
                      { type: "text", value: "Failed to apply safe outputs from run ", agentTainted: false },
                      { type: "code", value: runUrl, agentTainted: false },
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
    core.setFailed(`Failed to apply safe outputs: ${msg}`);
    return;
  }

  core.info(`Successfully applied safe outputs from run ${runUrl}`);
  const runLink = /^\d+$/.test(runUrl) ? `${process.env.GITHUB_SERVER_URL || "https://github.com"}/${owner}/${repo}/actions/runs/${runUrl}` : runUrl;

  // Post a success comment on the issue
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body: renderSizedReport({
      type: "report",
      title: "Safe outputs applied",
      children: [
        {
          type: "paragraph",
          children: [
            { type: "text", value: "✅ Safe outputs from ", agentTainted: false },
            {
              type: "link",
              url: runLink,
              children: [
                { type: "text", value: "run ", agentTainted: false },
                { type: "code", value: runUrl, agentTainted: false },
              ],
            },
            { type: "text", value: " have been applied.", agentTainted: false },
          ],
        },
        { type: "xml_marker", name: "gh-aw-comment-type", value: "safe-outputs-applied", agentTainted: false },
      ],
    }).body.trimEnd(),
  });

  core.info(`Posted success comment on issue #${issueNumber}`);

  // Remove the label now that the action is complete
  await removeLabelSafely(owner, repo, issueNumber, APPLY_SAFE_OUTPUTS_LABEL);
}

module.exports = { main, extractRunUrl };
