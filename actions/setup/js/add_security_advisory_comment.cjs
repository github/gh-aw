// @ts-check
/// <reference types="@actions/github-script" />

const { createAuthenticatedGitHubClient } = require("./handler_auth.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { buildIssueBodyReport } = require("./issue_body.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { logStagedPreviewInfo } = require("./staged_preview.cjs");
const { ERR_API, ERR_CONFIG, ERR_VALIDATION } = require("./error_codes.cjs");

const GHSA_ID_PATTERN = /^GHSA(-[23456789cfghjmpqrvwx]{4}){3}$/;

/** @type {import('./types/handler-factory').HandlerFactoryFunction} */
async function main(config = {}) {
  const staged = isStagedMode(config);
  if (!staged && (typeof config["github-token"] !== "string" || !config["github-token"].trim())) {
    throw new Error(`${ERR_CONFIG}: add_security_advisory_comment requires an external github-token or GitHub App token with Repository security advisories write permission; GITHUB_TOKEN is not supported`);
  }
  const githubClient = staged ? null : await createAuthenticatedGitHubClient(config);
  const max = config.max === -1 ? Infinity : (config.max ?? 1);
  let processedCount = 0;

  return async function handleAddSecurityAdvisoryComment(message) {
    if (typeof message.ghsa_id !== "string" || !GHSA_ID_PATTERN.test(message.ghsa_id)) {
      throw new Error(`${ERR_VALIDATION}: add_security_advisory_comment requires a valid GHSA identifier, for example GHSA-2345-6789-cfgh`);
    }
    if (typeof message.body !== "string" || !message.body.trim() || message.body.length > 65000) {
      throw new Error(`${ERR_VALIDATION}: add_security_advisory_comment body must contain between 1 and 65000 characters`);
    }
    const sanitizedBody = sanitizeContent(message.body);
    if (!sanitizedBody.trim()) {
      throw new Error(`${ERR_VALIDATION}: add_security_advisory_comment body is empty after sanitization`);
    }
    const report = buildIssueBodyReport("Security advisory comment", sanitizedBody, {
      repo: context.repo,
      footer: parseBoolTemplatable(config.footer, true),
      bodyFooter: config.body_footer,
      itemType: "comment",
      workflowIdMarker: false,
    });
    const { body } = renderSizedReport(report, 65000);
    if (processedCount >= max) {
      return { success: false, skipped: true, reasonCode: "MAX_COUNT_REACHED", error: `Max count of ${max} reached` };
    }
    if (staged) {
      processedCount++;
      logStagedPreviewInfo("Would add a security advisory comment in the workflow repository (content omitted)");
      return { success: true, staged: true };
    }
    try {
      const { data } = await githubClient.request("POST /repos/{owner}/{repo}/security-advisories/{ghsa_id}/comments", {
        ...context.repo,
        ghsa_id: message.ghsa_id,
        body,
        headers: { "X-GitHub-Api-Version": "2026-03-10" },
      });
      processedCount++;
      if (!Number.isSafeInteger(data.id) || data.id <= 0 || typeof data.html_url !== "string") {
        throw new Error(`${ERR_API}: GitHub did not return a security advisory comment`);
      }
      core.info("Added security advisory comment");
      return { success: true, comment_id: data.id, url: data.html_url };
    } catch (error) {
      // API errors can include the confidential request body; expose only status.
      const status = typeof error === "object" && error !== null && "status" in error ? error.status : "unknown";
      const errorMessage = `${ERR_API}: Failed to add security advisory comment (HTTP ${status}). Verify API availability and external token or GitHub App advisory-write access.`;
      core.error(errorMessage);
      return { success: false, error: errorMessage };
    }
  };
}

module.exports = { main };
