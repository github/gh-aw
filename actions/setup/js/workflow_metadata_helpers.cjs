// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Get workflow metadata from environment variables and context
 * This is used by expired entity cleanup scripts to build workflow run URLs
 * for closing comment footers.
 *
 * @param {string} owner - Repository owner
 * @param {string} repo - Repository name
 * @param {{ runId?: number, serverUrl?: string }} [ctx] - GitHub Actions context (defaults to the global `context`)
 * @returns {{workflowName: string, workflowId: string, runId: number, runUrl: string}} Workflow metadata
 */
function getWorkflowMetadata(owner, repo, ctx = context) {
  const workflowName = process.env.GH_AW_WORKFLOW_NAME || "Workflow";
  const workflowId = process.env.GH_AW_WORKFLOW_ID || "";
  const runId = ctx.runId ?? 0;
  const runUrl = buildWorkflowRunUrl({ runId, serverUrl: ctx.serverUrl }, { owner, repo });

  return { workflowName, workflowId, runId, runUrl };
}

/**
 * Build a workflow run URL from the provided context and workflow repository.
 *
 * This is the canonical helper for constructing attribution run URLs in safe-output
 * handlers. It must always receive the **original** workflow repo (not an overridden
 * cross-repo effectiveContext) so that footer links point back to the actual workflow
 * run regardless of which repository the output action targets.
 *
 * @param {{ serverUrl?: string, runId: number | string }} ctx - GitHub Actions context (provides serverUrl and runId)
 * @param {{ owner: string, repo: string }} workflowRepo - The repository that owns the workflow run
 * @returns {string} The full workflow run URL
 */
function buildWorkflowRunUrl(ctx, workflowRepo) {
  const server = ctx.serverUrl || process.env.GITHUB_SERVER_URL || "https://github.com";
  let { owner = "", repo = "" } = workflowRepo ?? {};
  if (!owner || !repo) {
    // When context is spread (e.g. `{...context}`), prototype getters like context.repo
    // are not included. Fall back to GITHUB_REPOSITORY for the workflow repo.
    const [envOwner = "", envRepo = ""] = (process.env.GITHUB_REPOSITORY || "").split("/");
    if (envOwner && envRepo) {
      owner = owner || envOwner;
      repo = repo || envRepo;
    }
  }
  return `${server}/${owner}/${repo}/actions/runs/${ctx.runId}`;
}

/**
 * Human-readable descriptions for GitHub event types, used in comment messages.
 * Shared by handlers that describe the triggering event (e.g. "issue", "pull request").
 * @type {Record<string, string>}
 */
const EVENT_TYPE_DESCRIPTIONS = {
  issues: "issue",
  pull_request: "pull request",
  pull_request_comment: "pull request comment",
  pull_request_review: "pull request review",
  issue_comment: "issue comment",
  pull_request_review_comment: "pull request review comment",
  discussion: "discussion",
  discussion_comment: "discussion comment",
};

module.exports = {
  getWorkflowMetadata,
  buildWorkflowRunUrl,
  EVENT_TYPE_DESCRIPTIONS,
};
