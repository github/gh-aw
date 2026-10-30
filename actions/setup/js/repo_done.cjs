// @ts-check
/// <reference types="@actions/github-script" />

const { resolveInvocationContext } = require("./invocation_context_helpers.cjs");
const { parseBoolTemplatable } = require("./templatable.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { renderSizedReport } = require("./report_size.cjs");

/**
 * Trusted conclusion notification. Never reads agent output or accepts tool calls.
 * Completion describes the agent job result, not a claim that its task succeeded.
 */
async function main() {
  const result = process.env.GH_AW_REPO_DONE_AGENT_RESULT;
  if (process.env.GH_AW_REPO_DONE_ACTIVATION_RESULT !== "success" || !["success", "failure", "cancelled"].includes(result || "")) {
    core.info("Skipping report-done: the agent was not activated.");
    return;
  }
  const staged = parseBoolTemplatable(process.env.GH_AW_REPO_DONE_DRY_RUN, false) || parseBoolTemplatable(process.env.GH_AW_SAFE_OUTPUTS_STAGED, false) || parseBoolTemplatable(process.env.GH_AW_REPO_DONE_STAGED, false);
  const runUrl = `${process.env.GITHUB_SERVER_URL || "https://github.com"}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;
  /** @type {import("./report_ast.cjs").Report} */
  const report = {
    type: "report",
    title: "Repository workflow completion",
    children: [
      ...parseReportMarkdown(process.env.GH_AW_REPO_DONE_BODY || "Workflow completed.", { agentTainted: false }),
      {
        type: "paragraph",
        agentTainted: false,
        sizeProtected: true,
        children: [
          { type: "text", value: `Agent job result: ${result}. `, agentTainted: false },
          { type: "link", url: runUrl, children: [{ type: "text", value: "View workflow run", agentTainted: false }], agentTainted: false },
        ],
      },
    ],
  };
  const { body } = renderSizedReport(report);
  // Preview before even resolving the target: no API reads or writes are needed.
  if (staged) {
    await core.summary.addHeading("🎭 Staged Mode: Repository completion Preview", 2).addRaw(body).write();
    return;
  }

  const { eventName, eventPayload: payload, eventRepo: repo } = resolveInvocationContext(context);
  // Invocation resolution normalizes forwarded metadata; it does not authorize
  // every possible repository override. This notification is workflow-repo only.
  if (repo.owner.toLowerCase() !== context.repo.owner.toLowerCase() || repo.repo.toLowerCase() !== context.repo.repo.toLowerCase()) {
    throw new Error("report-done: completion notifications are restricted to the workflow repository");
  }
  const number = payload.issue?.number || payload.pull_request?.number;
  if (Number.isSafeInteger(number) && number > 0) {
    await github.rest.issues.createComment({ ...repo, issue_number: number, body });
    return;
  }
  const discussionNumber = payload.discussion?.number;
  if (Number.isSafeInteger(discussionNumber) && discussionNumber > 0) {
    // Resolve by repository + number, rather than trusting a forwarded node ID.
    const data = await github.graphql(
      `query($owner: String!, $repo: String!, $number: Int!) {
        repository(owner: $owner, name: $repo) { discussion(number: $number) { id } }
      }`,
      { ...repo, number: discussionNumber }
    );
    if (!data.repository?.discussion?.id) throw new Error("report-done: discussion not found");
    await github.graphql(
      `mutation($discussionId: ID!, $body: String!) {
        addDiscussionComment(input: {discussionId: $discussionId, body: $body}) { comment { id } }
      }`,
      { discussionId: data.repository.discussion.id, body }
    );
    return;
  }
  const sha = eventName === "push" ? payload.after : eventName === "commit_comment" ? payload.comment?.commit_id : undefined;
  if (typeof sha === "string" && /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/i.test(sha) && !/^0+$/.test(sha)) {
    await github.rest.repos.createCommitComment({ ...repo, commit_sha: sha, body });
    return;
  }
  await core.summary.addHeading("Repository workflow completion", 2).addRaw(body).write();
}

module.exports = { main };
