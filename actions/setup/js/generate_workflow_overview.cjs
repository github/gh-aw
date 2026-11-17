// @ts-check
/// <reference types="@actions/github-script" />

const { jsonObjectToMarkdown } = require("./json_object_to_markdown.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Generate workflow overview step that writes an agentic workflow run overview
 * to the GitHub step summary. This reads from aw_info.json that was created by
 * a previous step and uses HTML details/summary tags for collapsible output.
 *
 * @param {typeof import('@actions/core')} core - GitHub Actions core library
 * @returns {Promise<void>}
 */
async function generateWorkflowOverview(core) {
  const fs = require("fs");
  const awInfoPath = require("./runtime_paths.cjs").runtimePath("aw_info.json");

  // Load aw_info.json
  let awInfo;
  try {
    awInfo = JSON.parse(fs.readFileSync(awInfoPath, "utf8"));
  } catch (err) {
    throw new Error("Failed to parse aw_info.json at " + awInfoPath + ": " + getErrorMessage(err), { cause: err });
  }

  // Build the collapsible summary label with engine_id and version
  const engineLabel = [awInfo.engine_id, awInfo.version].filter(Boolean).join(" ");
  const summaryLabel = engineLabel ? `Run details - ${engineLabel}` : "Run details";

  // Render all aw_info fields as markdown bullet points
  const details = jsonObjectToMarkdown(awInfo);

  // Build summary using string concatenation to avoid YAML parsing issues with template literals
  const summary = "<details>\n" + `<summary>${summaryLabel}</summary>\n\n` + details + "\n" + "</details>";

  await core.summary.addRaw(summary).write();
  core.info("Generated workflow overview in step summary");
}

module.exports = {
  generateWorkflowOverview,
};
