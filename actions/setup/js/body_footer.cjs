// @ts-check
/// <reference types="@actions/github-script" />

const { getBodyFooterMessage } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/**
 * Append a configured deterministic body footer using the current workflow context.
 * @param {string} body
 * @param {string|undefined} template
 * @param {any} [workflowRepo]
 * @returns {string}
 */
function appendConfiguredBodyFooter(body, template, workflowRepo) {
  if (!template) return body;
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const runUrl = buildWorkflowRunUrl(context, workflowRepo || context.repo);
  const bodyFooter = getBodyFooterMessage(template, { workflowName, runUrl });
  if (!bodyFooter) return body;
  const trimmedBody = body.trimEnd();
  return trimmedBody ? `${trimmedBody}\n\n${bodyFooter.trimEnd()}` : bodyFooter.trimEnd();
}

module.exports = { appendConfiguredBodyFooter };
