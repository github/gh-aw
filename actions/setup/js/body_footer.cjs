// @ts-check
/// <reference types="@actions/github-script" />

const { getBodyFooterMessage } = require("./messages_footer.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

/**
 * Append a configured deterministic body footer using the current workflow context.
 * When `maxLength` is provided and appending the footer would exceed that limit,
 * the original body is returned unchanged so the request still satisfies the
 * destination's body contract.
 * @param {string} body
 * @param {string|undefined} template
 * @param {{workflowRepo?: any, maxLength?: number}} [options]
 * @returns {string}
 */
function appendConfiguredBodyFooter(body, template, options = {}) {
  if (!template) return body;
  const { workflowRepo, maxLength } = options || {};
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const runUrl = buildWorkflowRunUrl(context, workflowRepo || context.repo);
  const bodyFooter = getBodyFooterMessage(template, { workflowName, runUrl });
  if (!bodyFooter) return body;
  const trimmedBody = body.trimEnd();
  const composed = trimmedBody ? `${trimmedBody}\n\n${bodyFooter.trimEnd()}` : bodyFooter.trimEnd();
  if (typeof maxLength === "number" && maxLength > 0 && composed.length > maxLength) {
    if (typeof core !== "undefined" && core && typeof core.warning === "function") {
      core.warning(`Configured body footer omitted: appending it would exceed the ${maxLength} character body limit`);
    }
    return body;
  }
  return composed;
}

module.exports = { appendConfiguredBodyFooter };
