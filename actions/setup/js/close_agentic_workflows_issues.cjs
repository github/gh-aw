// @ts-check
/// <reference types="@actions/github-script" />

const { resolveExecutionOwnerRepo } = require("./repo_helpers.cjs");
const { sanitizeContent } = require("./sanitize_content.cjs");
const { renderSizedReport } = require("./report_size.cjs");

const TARGET_LABEL = "agentic-workflows";
// Keep a conservative, explicit upper bound for comment sanitization.
const MAX_COMMENT_BODY_LENGTH = 65536;
const NO_REPRO_MESSAGE = `Closing as no repro.

If this is still reproducible, please open a new issue with clear reproduction steps.`;
const CLOSE_ISSUE_MUTATION = `
mutation CloseIssue($issueId: ID!, $stateReason: IssueClosedStateReason!) {
  closeIssue(input: { issueId: $issueId, stateReason: $stateReason }) {
    issue {
      number
      state
      stateReason
    }
  }
}
`;

/**
 * Close an issue via GraphQL with explicit close reason.
 * @param {string} issueId - GraphQL node ID for the issue
 * @returns {Promise<void>}
 */
async function closeIssueAsNotPlanned(issueId) {
  await github.graphql(CLOSE_ISSUE_MUTATION, {
    issueId,
    stateReason: "NOT_PLANNED",
  });
}

/**
 * Build the close-comment body with SEC-004 sanitization guarantees.
 *
 * @param {(content: string, options?: { maxLength?: number }) => string} [sanitize=sanitizeContent]
 * @returns {string}
 */
function getNoReproCommentBody(sanitize = sanitizeContent) {
  const generatedBody = renderSizedReport(
    {
      type: "report",
      title: "Issue closed",
      children: [
        { type: "paragraph", children: [{ type: "text", value: "Closing as no repro.", agentTainted: false }] },
        { type: "paragraph", children: [{ type: "text", value: "If this is still reproducible, please open a new issue with clear reproduction steps.", agentTainted: false }] },
      ],
    },
    MAX_COMMENT_BODY_LENGTH
  ).body;
  const body = sanitize(generatedBody, { maxLength: MAX_COMMENT_BODY_LENGTH });
  if (typeof body !== "string") {
    throw new Error("Close comment body sanitization must return a string");
  }
  if (body.trim().length === 0) {
    throw new Error("Close comment body is empty after sanitization");
  }
  return body;
}

/**
 * Close all open issues with the "agentic-workflows" label.
 * @returns {Promise<void>}
 */
async function main() {
  const { owner, repo } = resolveExecutionOwnerRepo();
  core.info(`Operating on repository: ${owner}/${repo}`);
  core.info(`Searching for open issues labeled "${TARGET_LABEL}"`);

  /** @type {Array<any>} */
  const issues = await github.paginate(github.rest.issues.listForRepo, {
    owner,
    repo,
    labels: TARGET_LABEL,
    state: "open",
    per_page: 100,
  });

  const targetIssues = issues.filter(issue => !issue.pull_request);
  core.info(`Found ${targetIssues.length} issue(s) to close`);

  if (targetIssues.length === 0) {
    return;
  }

  for (const issue of targetIssues) {
    core.info(`Closing issue #${issue.number}: ${issue.title}`);

    await github.rest.issues.createComment({
      owner,
      repo,
      issue_number: issue.number,
      body: getNoReproCommentBody(),
    });

    await closeIssueAsNotPlanned(issue.node_id);
  }
}

module.exports = { main, closeIssueAsNotPlanned, CLOSE_ISSUE_MUTATION, NO_REPRO_MESSAGE, MAX_COMMENT_BODY_LENGTH, getNoReproCommentBody };
