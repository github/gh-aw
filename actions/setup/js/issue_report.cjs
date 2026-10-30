// @ts-check
/// <reference types="@actions/github-script" />

const { isDeepStrictEqual } = require("node:util");
const { renderSizedReport } = require("./report_size.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { redactReport } = require("./report_redaction.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

// Reuse only this boundary's unchanged output, never caller-supplied lookalikes.
/** @type {WeakMap<object, {rendered: {title: string, body: string}, owner?: string, repo?: string, headers: Record<string, string | number | undefined>}>} */
const renderedIssues = new WeakMap();

/**
 * Import existing issue Markdown at the publication boundary. Metadata is opt-in
 * here, not in diagnostic fragments; it remains typed and cannot close comments.
 * @param {string} title
 * @param {string | import("./report_ast.cjs").ReportBlock[]} body
 * @param {{allowedMentions?: string[], agentTainted?: boolean, redact?: import("./report_redaction.cjs").ReportRedactionPolicy}} [options]
 * @returns {import("./report_ast.cjs").Report}
 */
function buildIssueReport(title, body, { allowedMentions = [], agentTainted = true, redact } = {}) {
  /** @type {import("./report_ast.cjs").Report} */
  const report = { type: "report", title, agentTainted, children: typeof body === "string" ? parseReportMarkdown(body, { minimumHeadingLevel: 2, allowMetadata: true, allowedMentions, agentTainted }) : body };
  return redact ? redactReport(report, redact) : report;
}

/**
 * @template {{title: string, body?: string, owner?: string, repo?: string, headers?: Record<string, string | number | undefined>}} T
 * @param {T} parameters
 * @param {import("./report_ast.cjs").Report} [report]
 * @returns {T & {title: string, body: string}}
 */
function renderIssueParameters(parameters, report = buildIssueReport(parameters.title, parameters.body ?? "")) {
  const rendered = renderSizedReport(report);
  if (rendered.body.length > 65536) throw new Error(`${ERR_VALIDATION}: Issue body exceeds GitHub's maximum length of 65536 characters including attribution`);
  const result = { ...parameters, ...rendered };
  renderedIssues.set(result, { rendered, owner: result.owner, repo: result.repo, headers: { ...result.headers } });
  return result;
}

/**
 * Keep authentication, retries, and API responses at the caller's existing boundary.
 * @param {typeof github} client
 * @param {NonNullable<Parameters<typeof github.rest.issues.create>[0]> & {title: string, body?: string}} parameters
 * @param {import("./report_ast.cjs").Report} [report]
 */
function createReportIssue(client, parameters, report) {
  const cached = renderedIssues.get(parameters);
  // Re-rendering existing Markdown cannot repair attribution bound to another target.
  if (cached && (cached.owner !== parameters.owner || cached.repo !== parameters.repo || !isDeepStrictEqual(cached.headers, parameters.headers ?? {}))) {
    throw new Error(`${ERR_VALIDATION}: Prepared issue target or headers changed; build and render a fresh report for the intended target before publishing`);
  }
  const rendered = cached?.rendered;
  if (!report && rendered && rendered.title === parameters.title && rendered.body === parameters.body) {
    return client.rest.issues.create(parameters);
  }
  return client.rest.issues.create(renderIssueParameters(parameters, report));
}

module.exports = { buildIssueReport, renderIssueParameters, createReportIssue };
