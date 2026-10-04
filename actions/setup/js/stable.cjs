// @ts-check
/// <reference types="@actions/github-script" />

const { setupGlobals } = require("./setup_globals.cjs");
const { formatIssueBody } = require("./issue_body.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");

/**
 * @typedef {NonNullable<Parameters<typeof github.rest.issues.create>[0]>} IssueParameters
 * @typedef {Awaited<ReturnType<typeof github.rest.issues.create>>["data"]} Issue
 */

/**
 * Create an attributed issue from a custom safe-output job.
 * Call setupGlobals before using this API. Title and body must be trusted or
 * sanitized by the custom job; this helper does not enforce agent tool policies.
 * @param {Omit<IssueParameters, "owner" | "repo" | "title" | "body"> & {owner?: string, repo?: string, title: string, body: string}} parameters
 * @returns {Promise<{staged: false, issue: Issue} | {staged: true, preview: IssueParameters}>}
 */
async function createIssue(parameters) {
  if (typeof github === "undefined" || typeof context === "undefined" || typeof core === "undefined") {
    throw new Error(`${ERR_VALIDATION}: Call stable.setupGlobals(core, github, context, exec, io, getOctokit) before creating an issue`);
  }
  if (!process.env.GH_AW_WORKFLOW_ID || !process.env.GH_AW_WORKFLOW_NAME) {
    throw new Error(`${ERR_VALIDATION}: GH_AW_WORKFLOW_ID and GH_AW_WORKFLOW_NAME are required for custom issue attribution; recompile the workflow`);
  }
  if (!parameters || typeof parameters.title !== "string" || !parameters.title.trim() || typeof parameters.body !== "string") {
    throw new Error(`${ERR_VALIDATION}: createIssue requires a non-empty title and a string body`);
  }
  const owner = parameters.owner ?? context.repo.owner;
  const repo = parameters.repo ?? context.repo.repo;
  if (typeof owner !== "string" || !owner.trim() || typeof repo !== "string" || !repo.trim()) {
    throw new Error(`${ERR_VALIDATION}: createIssue requires a repository owner and name`);
  }
  const body = formatIssueBody(parameters.body, { repo: { owner, repo } });
  if (body.length > 65536) {
    throw new Error(`${ERR_VALIDATION}: Issue body exceeds GitHub's maximum length of 65536 characters including attribution`);
  }
  const request = { ...parameters, owner, repo, body };
  if (isStagedMode()) {
    core.info(`Staged: would create issue in ${owner}/${repo} with title: ${parameters.title}`);
    return { staged: true, preview: request };
  }
  const { data: issue } = await withRetry(() => github.rest.issues.create(request), RATE_LIMIT_RETRY_CONFIG, `create_issue in ${owner}/${repo}`);
  core.info(`Created issue ${owner}/${repo}#${issue.number}: ${issue.html_url}`);
  return { staged: false, issue };
}

module.exports = { setupGlobals, createIssue };
