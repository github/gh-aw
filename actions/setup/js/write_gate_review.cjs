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
const { OUTPUT_ID_PATTERN } = require("./write_gate_inventory.cjs");

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
 * @param {Record<string, unknown>} [pendingTarget]
 * @returns {import("./report_ast.cjs").ReportBlock[]}
 */
function buildGateReviewBlocks(message, patchContent, secretValues, pendingTarget) {
  const text = typeof message.body === "string" ? message.body : typeof message.reason === "string" ? message.reason : "";
  const redactedText = redactManifestValue(text, secretValues);
  const redactedPatch = redactManifestValue(patchContent, secretValues);
  if (typeof redactedText !== "string" || typeof redactedPatch !== "string") throw new Error(`${ERR_VALIDATION}: Invalid write gate review content`);
  const operationKeys = new Set(["labels", "remove_labels", "add_labels", "workflow", "workflow_id", "workflow_name", "ref", "inputs", "event_type", "client_payload", "assignees", "reviewers", "milestone", "state", "draft"]);
  const operation = Object.fromEntries(Object.entries(message).filter(([key]) => operationKeys.has(key)));
  /** @type {import("./report_ast.cjs").ReportBlock[]} */
  const proposal = pendingTarget
    ? [
        { type: "heading", level: 2, children: [{ type: "text", value: "Verified pending target", agentTainted: false }], agentTainted: false },
        { type: "code_block", language: "json", value: JSON.stringify(redactManifestValue(pendingTarget, secretValues), null, 2), agentTainted: false },
      ]
    : [];
  if (Object.keys(operation).length)
    proposal.push(
      { type: "heading", level: 2, children: [{ type: "text", value: "Proposed operation", agentTainted: false }], agentTainted: false },
      { type: "code_block", language: "json", value: sanitizeContent(JSON.stringify(redactManifestValue(operation, secretValues), null, 2)), agentTainted: true }
    );
  return [
    ...proposal,
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
 * @param {{message: Record<string, unknown>, reviewRepo: unknown, defaultRepo: string, allowedRepos: Set<string>, client: typeof github, assertUnchanged: () => void, patchContent?: string, pendingItem?: Record<string, any>}} options
 */
async function createGateReviewIssue({ message, reviewRepo, defaultRepo, allowedRepos, client, assertUnchanged, patchContent = "", pendingItem }) {
  function stage(reason) {
    core.warning(`Write gate review staged: ${reason}; no repository was written`);
    return { success: false, skipped: true, staged: true, reason, gate_decision: "stage" };
  }
  if (isStagedMode()) return stage("gate_staged");
  if (process.env.GH_AW_WRITE_GATE_REVIEW_USE_APP === "true" && !process.env.GH_AW_WRITE_GATE_REVIEW_TOKEN) return stage("gate_review_auth_unavailable");
  if (process.env.GH_AW_WRITE_GATE_REVIEW_TOKEN) {
    if (typeof global.getOctokit !== "function") throw new Error(`${ERR_VALIDATION}: Gate review GitHub App client is unavailable`);
    client = global.getOctokit(process.env.GH_AW_WRITE_GATE_REVIEW_TOKEN);
  }
  const target = resolveGateReviewRepo(reviewRepo, defaultRepo, allowedRepos);
  if (!target) return stage("gate_review_repo_unusable");
  if (typeof message.type !== "string" || typeof message.output_id !== "string" || !OUTPUT_ID_PATTERN.test(message.output_id)) {
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
  const report = buildIssueBodyReport(title, buildGateReviewBlocks(message, patchContent, secrets, pendingItem?.target), { repo: target, runUrl: buildWorkflowRunUrl(context, context.repo) });
  try {
    const { data: issue } = await withRetry(
      () => {
        assertUnchanged();
        const { pendingWritesEnabled, withPendingReview } = require("./pending_writes.cjs");
        if (!pendingWritesEnabled()) return createReportIssue(client, { ...target, title }, report);
        if (!pendingItem) throw new Error(`${ERR_VALIDATION}: Missing pending item for gate review scope`);
        return withPendingReview(pendingItem, `${target.owner}/${target.repo}`, () => createReportIssue(client, { ...target, title }, report));
      },
      RATE_LIMIT_RETRY_CONFIG,
      `create write gate review issue in ${target.owner}/${target.repo}`
    );
    return { success: true, reviewed: true, gate_decision: "review", fallback_used: true, issue_number: issue.number, issue_url: issue.html_url, repo: `${target.owner}/${target.repo}`, issue };
  } catch (error) {
    const original = error && typeof error === "object" && "originalError" in error ? error.originalError : error;
    const status = getErrorStatus(original);
    if (status === 404 || status === 410) return stage("gate_review_repo_unusable");
    throw error;
  }
}

module.exports = { resolveGateReviewRepo, buildGateReviewBlocks, createGateReviewIssue };
