// @ts-check
/// <reference types="@actions/github-script" />

const { ERR_API, ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { GITHUB_API_VERSION } = require("./constants.cjs");
const { withRetry, RATE_LIMIT_RETRY_CONFIG } = require("./error_recovery.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { sanitizeTitle } = require("./sanitize_title.cjs");

/**
 * @typedef {{type: "generic" | "link" | "plan" | "embedded_html", name: string, body: string}} IssueArtifact
 */

/**
 * Validate all artifacts before creating their issue.
 * @param {unknown} value
 * @param {number} [maxArtifacts=10] - Per-issue limit; non-positive values disable artifacts.
 * @returns {IssueArtifact[]}
 */
function normalizeIssueArtifacts(value, maxArtifacts = 10) {
  if (!Number.isSafeInteger(maxArtifacts)) throw new Error(`${ERR_VALIDATION}: max-issue-artifacts must be an integer`);
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new Error(`${ERR_VALIDATION}: create_issue 'artifacts' must be an array`);
  if (value.length > Math.max(0, maxArtifacts)) {
    throw new Error(maxArtifacts <= 0 ? `${ERR_VALIDATION}: Issue artifacts are disabled by max-issue-artifacts; omit artifacts` : `${ERR_VALIDATION}: create_issue artifacts exceeds max-issue-artifacts limit of ${maxArtifacts} per issue`);
  }
  return value.map((artifact, index) => {
    const field = `create_issue artifacts[${index}]`;
    if (!artifact || typeof artifact !== "object" || Array.isArray(artifact)) {
      throw new Error(`${ERR_VALIDATION}: ${field} must be an object`);
    }
    if (Object.keys(artifact).some(key => !["type", "name", "body"].includes(key))) {
      throw new Error(`${ERR_VALIDATION}: ${field} accepts only type, name, and body`);
    }
    const type = artifact.type === undefined ? "generic" : artifact.type;
    if (type !== "generic" && type !== "link" && type !== "plan" && type !== "embedded_html") {
      throw new Error(`${ERR_VALIDATION}: ${field}.type must be generic, link, plan, or embedded_html`);
    }
    if (typeof artifact.name !== "string" || !artifact.name.trim()) {
      throw new Error(`${ERR_VALIDATION}: ${field}.name must be a non-empty string`);
    }
    if (typeof artifact.body !== "string" || !artifact.body.trim() || artifact.body.length > 524288) {
      throw new Error(`${ERR_VALIDATION}: ${field}.body must be a non-empty string of at most 524288 characters`);
    }
    return { type, name: artifact.name.trim(), body: artifact.body };
  });
}

/**
 * Preserve HTML as source rather than converting its tags to Markdown text.
 * A fence longer than any source backtick run prevents source from escaping it.
 * @param {IssueArtifact[]} artifacts
 * @param {import("./sanitize_content.cjs").SanitizeOptions} options
 * @returns {IssueArtifact[]}
 */
function sanitizeIssueArtifacts(artifacts, options) {
  return artifacts.map(artifact => {
    const name = sanitizeTitle(artifact.name);
    let body;
    if (artifact.type === "embedded_html") {
      const longestRun = Array.from(artifact.body.matchAll(/`+/g)).reduce((longest, match) => Math.max(longest, match[0].length), 0);
      const fence = "`".repeat(Math.max(3, longestRun + 1));
      const source = `${fence}html\n${artifact.body}\n${fence}`;
      const sanitized = sanitizeContent(source, { ...options, maxLength: source.length * 2, maxLines: source.length + 1 });
      body = sanitized.slice(`${fence}html\n`.length, -`\n${fence}`.length);
    } else {
      body = sanitizeContent(artifact.body, options);
    }
    if (!name.trim() || !body.trim()) throw new Error(`${ERR_VALIDATION}: Issue artifact name and body must remain non-empty after sanitization`);
    return { ...artifact, name, body };
  });
}

/**
 * @param {typeof github} githubClient
 * @param {string} owner
 * @param {string} repo
 * @param {number} issueNumber
 * @param {IssueArtifact[]} artifacts
 * @returns {Promise<Array<{number: number, url: string | undefined}>>}
 */
async function createIssueArtifacts(githubClient, owner, repo, issueNumber, artifacts) {
  const created = [];
  for (const artifact of artifacts) {
    try {
      const { data } = await withRetry(
        () =>
          githubClient.request("POST /repos/{owner}/{repo}/issues/{issue_number}/artifacts", {
            owner,
            repo,
            issue_number: issueNumber,
            ...artifact,
            headers: { "X-GitHub-Api-Version": GITHUB_API_VERSION },
          }),
        RATE_LIMIT_RETRY_CONFIG,
        `create issue artifact on ${owner}/${repo}#${issueNumber}`
      );
      created.push({ number: data.number, url: data.html_url });
    } catch (error) {
      throw new Error(`${ERR_API}: Issue ${owner}/${repo}#${issueNumber} was created, but artifact "${artifact.name}" could not be attached (${created.length}/${artifacts.length} attached): ${getErrorMessage(error)}`, { cause: error });
    }
  }
  return created;
}

module.exports = { normalizeIssueArtifacts, sanitizeIssueArtifacts, createIssueArtifacts };
