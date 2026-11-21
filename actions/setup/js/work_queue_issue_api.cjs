// @ts-check
"use strict";

const { createReportIssue } = require("./issue_report.cjs");
const { expectedGitHubHost } = require("./work_queue_host.cjs");

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
 * Queue resource for a REST Issue.
 * @param {{id: number|string, number: number|string}} issue
 * @param {string} repository owner/repo
 * @param {string} repoId native repository id
 */
function issueResource(issue, repository, repoId) {
  return { kind: "issue", host: expectedGitHubHost(), repository, repository_id: String(repoId), resource_id: String(issue.id), number: String(issue.number) };
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {{title: string, body: string, label: string}} issue */
async function createIssue(githubClient, repo, { title, body, label }) {
  const { data } = await createReportIssue(githubClient, { ...repo, title, body, labels: [label] });
  return data;
}

/** @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number @param {string} body */
async function addComment(githubClient, repo, number, body) {
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
 * The last RECENT_COMMENTS comments `{id, body}` of an Issue with `total` comments.
 * @param {any} githubClient @param {{owner: string, repo: string}} repo @param {number} number @param {number} total
 * @returns {Promise<Array<{id: string, body: string}>>}
 */
async function recentComments(githubClient, repo, number, total) {
  if (!total) return [];
  const last = Math.ceil(total / RECENT_COMMENTS);
  const pages = total % RECENT_COMMENTS && last > 1 ? [last - 1, last] : [last];
  const comments = [];
  for (const page of pages) {
    const { data } = await githubClient.rest.issues.listComments({ ...repo, issue_number: number, per_page: RECENT_COMMENTS, page });
    for (const comment of data) comments.push({ id: String(comment.id), body: typeof comment.body === "string" ? comment.body : "" });
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

module.exports = { RECENT_COMMENTS, addComment, closeIssue, createIssue, getIssue, issueResource, listLabelledOpenIssues, recentComments, repositoryId };
