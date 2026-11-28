// @ts-check
"use strict";

const { buildIssueReport, createReportIssue } = require("./issue_report.cjs");
const { renderSizedReport } = require("./report_size.cjs");
const { MAX_COMMENT_LENGTH } = require("./comment_limit_helpers.cjs");
const { expectedGitHubHost } = require("./work_queue_host.cjs");
const { appendReportExecutionProvenance } = require("./execution_provenance.cjs");

// Comment-marker idempotence only inspects the most recent comments of an Issue.
const RECENT_COMMENTS = 30;
// Open labelled Issues inspected per projection (orphan sweep and lost-link recovery).
const LABELLED_PAGES = 3;
const PAGE_SIZE = 100;

/** @param {any} githubClient @param {{owner: string, repo: string}} repo */
async function repositoryId(githubClient, repo) {
  const { data } = await githubClient.rest.repos.get(repo);
  return String(data.id);
}

/**
 * Numeric account id of the token's principal, which authors every projector write
 * (GraphQL `viewer`: REST `GET /user` rejects installation tokens).
 * @param {any} githubClient
 * @returns {Promise<string>}
 */
async function writerId(githubClient) {
  const id = (await githubClient.graphql("query { viewer { databaseId } }"))?.viewer?.databaseId;
  if (!Number.isSafeInteger(id) || id <= 0) throw new Error("projector identity is unverified");
  return String(id);
}

/**
 * Queue resource for a REST Issue.
 * @param {{id: number|string, number: number|string}} issue
 * @param {string} repository owner/repo
 * @param {string} repoId native repository id
 */
function issueResource(issue, repository, repoId) {
  return { kind: "issue", host: expectedGitHubHost(), repository, repository_id: String(repoId), resource_id: String(issue.id), number: String(issue.number) };
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {{title: string, body: import("./report_ast.cjs").ReportBlock[], label: string, workflowName: string}} issue */
async function createIssue(githubClient, repo, { title, body, label, workflowName }) {
  const report = appendReportExecutionProvenance(buildIssueReport(title, body, { agentTainted: false }), workflowName, "");
  const { data } = await createReportIssue(githubClient, { ...repo, title, labels: [label] }, report);
  return data;
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number @param {import("./report_ast.cjs").ReportBlock[]} children @param {string} workflowName */
async function addComment(githubClient, repo, number, children, workflowName) {
  const report = appendReportExecutionProvenance({ type: "report", title: "Work queue status", children }, workflowName, "");
  const { body } = renderSizedReport(report, MAX_COMMENT_LENGTH);
  const { data } = await githubClient.rest.issues.createComment({ ...repo, issue_number: number, body });
  return data;
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number */
async function closeIssue(githubClient, repo, number) {
  await githubClient.rest.issues.update({ ...repo, issue_number: number, state: "closed" });
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number */
async function getIssue(githubClient, repo, number) {
  const { data } = await githubClient.rest.issues.get({ ...repo, issue_number: number });
  return data;
}

/**
 * The last RECENT_COMMENTS comments `{id, body, author}` of an Issue with `total` comments;
 * `author` is the commenter's numeric account id ("" when absent).
 * @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number @param {number} total
 * @returns {Promise<Array<{id: string, body: string, author: string}>>}
 */
async function recentComments(githubClient, repo, number, total) {
  if (!total) return [];
  const last = Math.ceil(total / RECENT_COMMENTS);
  const pages = total % RECENT_COMMENTS && last > 1 ? [last - 1, last] : [last];
  const comments = [];
  for (const page of pages) {
    const { data } = await githubClient.rest.issues.listComments({ ...repo, issue_number: number, per_page: RECENT_COMMENTS, page });
    for (const comment of data) comments.push({ id: String(comment.id), body: typeof comment.body === "string" ? comment.body : "", author: comment.user?.id == null ? "" : String(comment.user.id) });
  }
  return comments.slice(-RECENT_COMMENTS);
}

/** Open, non-PR Issues carrying `label` (bounded). @param {any} githubClient @param {{owner: string, repo: string}} repo @param {string} label */
async function listLabelledOpenIssues(githubClient, repo, label) {
  const issues = [];
  for (let page = 1; page <= LABELLED_PAGES; page++) {
    const { data } = await githubClient.rest.issues.listForRepo({ ...repo, labels: label, state: "open", per_page: PAGE_SIZE, page });
    issues.push(...data.filter(issue => !issue.pull_request));
    if (data.length < PAGE_SIZE) break;
  }
  return issues;
}

module.exports = { RECENT_COMMENTS, addComment, closeIssue, createIssue, getIssue, issueResource, listLabelledOpenIssues, recentComments, repositoryId, writerId };
