// @ts-check
/// <reference types="@actions/github-script" />

const { isExactRepoSlug, parseRepoSlug, validateRepo } = require("./repo_helpers.cjs");
const { buildPatchPreviewBlocks, neutralizeClosingKeywordsForIssueBody } = require("./create_pull_request_helpers.cjs");
const { buildIssueBodyReport } = require("./issue_body.cjs");
const { createReportIssue } = require("./issue_report.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { stripExecutionProvenance } = require("./execution_provenance.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorStatus } = require("./error_helpers.cjs");

/**
 * No implicit target: an absent or unusable review repository always stages.
 * @param {unknown} reviewRepo
 * @param {string} defaultRepo
 * @param {Set<string>} allowedRepos
 * @returns {import("./repo_helpers.cjs").RepoParts|null}
 */
function resolveGateReviewRepo(reviewRepo, defaultRepo, allowedRepos) {
  if (typeof reviewRepo !== "string" || !isExactRepoSlug(parseRepoSlug(reviewRepo))) return null;
  const validation = validateRepo(reviewRepo, defaultRepo, allowedRepos);
  return validation.valid ? parseRepoSlug(validation.qualifiedRepo) : null;
}

/**
 * @param {Record<string, unknown>} message
 * @param {string} patchContent Already verified against the gate manifest
 * @param {string[]} secretValues
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildGateReviewBlocks(message, patchContent, secretValues) {
  const text = typeof message.body === "string" ? message.body : typeof message.reason === "string" ? message.reason : "";
  const redactedText = redactManifestValue(text, secretValues);
  const redactedPatch = redactManifestValue(patchContent, secretValues);
  if (typeof redactedText !== "string" || typeof redactedPatch !== "string") throw new Error(`${ERR_VALIDATION}: Invalid write gate review content`);
  return [
    {
      type: "alert",
      kind: "NOTE",
      agentTainted: false,
      children: [{ type: "paragraph", agentTainted: false, children: [{ type: "text", value: "The write gate requested review. The proposed operation was not applied.", agentTainted: false }] }],
    },
    {
      type: "paragraph",
      agentTainted: false,
      children: [
        { type: "text", value: "Proposed output: ", agentTainted: false },
        { type: "code", value: String(message.type), agentTainted: true },
        { type: "text", value: " (", agentTainted: false },
        { type: "code", value: String(message.output_id), agentTainted: false },
        { type: "text", value: ")", agentTainted: false },
      ],
    },
    ...parseReportMarkdown(sanitizeContent(neutralizeClosingKeywordsForIssueBody(stripExecutionProvenance(redactedText))), { minimumHeadingLevel: 2, allowMetadata: false, agentTainted: true }),
    ...buildPatchPreviewBlocks(redactedPatch),
  ];
}

/**
 * Publish only a review issue in the exact configured, allowlisted repository.
 * It deliberately does not reuse fallback helpers that retry in other repos or
 * assign agents. Integrity is rechecked immediately before every API write.
 *
 * @param {{message: Record<string, unknown>, reviewRepo: unknown, defaultRepo: string, allowedRepos: Set<string>, client: typeof github, assertUnchanged: () => void, patchContent?: string}} options
 */
async function createGateReviewIssue({ message, reviewRepo, defaultRepo, allowedRepos, client, assertUnchanged, patchContent = "" }) {
  function stage(reason) {
    core.warning(`Write gate review staged: ${reason}; no repository was written`);
    return { success: false, skipped: true, staged: true, reason, gate_decision: "stage" };
  }
  if (isStagedMode()) return stage("gate_staged");
  const target = resolveGateReviewRepo(reviewRepo, defaultRepo, allowedRepos);
  if (!target) return stage("gate_review_repo_unusable");
  if (typeof message.type !== "string" || typeof message.output_id !== "string" || !/^so_[A-Za-z0-9_-]{12}$/.test(message.output_id)) {
    throw new Error(`${ERR_VALIDATION}: Write gate review requires a valid output_id and type`);
  }
  assertUnchanged();
  try {
    const { data: repository } = await client.rest.repos.get(target);
    if (repository.has_issues !== true) return stage("gate_review_repo_unusable");
  } catch (error) {
    const status = getErrorStatus(error);
    if (status === 404 || status === 410) return stage("gate_review_repo_unusable");
    throw error;
  }
  const secrets = collectArtifactSecretValues();
  const proposedTitle = typeof message.title === "string" ? message.title : message.type;
  const redactedTitle = redactManifestValue(proposedTitle, secrets);
  if (typeof redactedTitle !== "string") throw new Error(`${ERR_VALIDATION}: Invalid write gate review title`);
  const title = sanitizeTitle(`[gh-aw] Review: ${redactedTitle}`);
  const report = buildIssueBodyReport(title, buildGateReviewBlocks(message, patchContent, secrets), { repo: target, runUrl: buildWorkflowRunUrl(context, context.repo) });
  try {
    const { data: issue } = await withRetry(
      () => {
        assertUnchanged();
        return createReportIssue(client, { ...target, title }, report);
      },
      RATE_LIMIT_RETRY_CONFIG,
      `create write gate review issue in ${target.owner}/${target.repo}`
    );
    return { success: true, reviewed: true, gate_decision: "review", fallback_used: true, issue_number: issue.number, issue_url: issue.html_url, repo: `${target.owner}/${target.repo}` };
  } catch (error) {
    const original = error && typeof error === "object" && "originalError" in error ? error.originalError : error;
    const status = getErrorStatus(original);
    if (status === 404 || status === 410) return stage("gate_review_repo_unusable");
    throw error;
  }
}

module.exports = { resolveGateReviewRepo, buildGateReviewBlocks, createGateReviewIssue };
