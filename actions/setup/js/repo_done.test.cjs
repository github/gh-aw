import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";

const { main } = await import("./repo_done.cjs");
const sha = "a".repeat(40);
let summary;
let github;

function invocation(eventName, payload) {
  vi.stubGlobal("context", { eventName, payload, repo: { owner: "owner", repo: "repo" }, runId: 123, sha });
}

beforeEach(() => {
  summary = { addHeading: vi.fn().mockReturnThis(), addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
  github = {
    rest: { issues: { createComment: vi.fn().mockResolvedValue({}) }, repos: { createCommitComment: vi.fn().mockResolvedValue({}) } },
    graphql: vi
      .fn()
      .mockResolvedValueOnce({ repository: { discussion: { id: "D_42" } } })
      .mockResolvedValue({}),
  };
  vi.stubGlobal("core", { info: vi.fn(), summary });
  vi.stubGlobal("github", github);
  vi.stubEnv("GH_AW_REPO_DONE_ACTIVATION_RESULT", "success");
  vi.stubEnv("GH_AW_REPO_DONE_AGENT_RESULT", "success");
  vi.stubEnv("GH_AW_REPO_DONE_BODY", "");
  vi.stubEnv("GH_AW_REPO_DONE_STAGED", "false");
  vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "false");
  vi.stubEnv("GH_AW_REPO_DONE_DRY_RUN", "false");
  vi.stubEnv("GITHUB_SERVER_URL", "https://github.example.com");
  // Neither presence nor validity of the agent's safe-output file matters.
  vi.stubEnv("GH_AW_AGENT_OUTPUT", "/nonexistent/invalid-agent-output.json");
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("repo-done trusted completion", () => {
  it.each([
    ["issues", { issue: { number: 42 } }],
    ["issue_comment", { issue: { number: 42 }, comment: { body: "agent comment", id: 9 } }],
    ["pull_request", { pull_request: { number: 42 } }],
    ["pull_request_review", { pull_request: { number: 42 } }],
    ["pull_request_review_comment", { pull_request: { number: 42 } }],
  ])("posts to the %s conversation without reading agent output", async (eventName, payload) => {
    invocation(eventName, payload);
    await main();
    expect(github.rest.issues.createComment).toHaveBeenCalledExactlyOnceWith({
      owner: "owner",
      repo: "repo",
      issue_number: 42,
      body: expect.stringContaining("Agent job result: success."),
    });
    expect(github.rest.issues.createComment.mock.calls[0][0].body).toContain("https://github.example.com/owner/repo/actions/runs/123");
    expect(summary.write).not.toHaveBeenCalled();
  });

  it.each(["discussion", "discussion_comment"])("posts a top-level discussion comment for %s", async eventName => {
    invocation(eventName, { discussion: { number: 42, node_id: "untrusted-forwarded-id" }, comment: { node_id: "reply" } });
    await main();
    expect(github.graphql).toHaveBeenCalledTimes(2);
    expect(github.graphql.mock.calls[0][1]).toEqual({ owner: "owner", repo: "repo", number: 42 });
    expect(github.graphql.mock.calls[1][1]).toEqual({ discussionId: "D_42", body: expect.stringContaining("Workflow completed.") });
  });

  it.each([
    ["push", { after: sha }],
    ["commit_comment", { comment: { commit_id: sha } }],
  ])("posts a commit comment for %s", async (eventName, payload) => {
    invocation(eventName, payload);
    await main();
    expect(github.rest.repos.createCommitComment).toHaveBeenCalledExactlyOnceWith({
      owner: "owner",
      repo: "repo",
      commit_sha: sha,
      body: expect.stringContaining("Workflow completed."),
    });
  });

  it.each([
    ["schedule", {}],
    ["schedule", { after: sha, comment: { commit_id: sha } }],
    ["workflow_dispatch", { inputs: {} }],
    ["workflow_dispatch", { inputs: { event_payload: JSON.stringify({ after: sha }) } }],
    ["check_run", { check_run: { head_sha: sha } }],
    ["check_suite", { check_suite: { head_sha: sha } }],
    ["workflow_run", { workflow_run: { head_sha: sha } }],
    ["push", { after: "0".repeat(40) }],
    ["push", { after: "not-a-sha" }],
    ["push", { after: "a".repeat(41) }],
    ["push", { comment: { commit_id: sha } }],
    ["commit_comment", { after: sha }],
  ])("uses a summary when %s has no commentable target", async (eventName, payload) => {
    invocation(eventName, payload);
    await main();
    expect(summary.write).toHaveBeenCalledOnce();
    expect(github.rest.issues.createComment).not.toHaveBeenCalled();
    expect(github.rest.repos.createCommitComment).not.toHaveBeenCalled();
    expect(github.graphql).not.toHaveBeenCalled();
  });

  it("uses forwarded workflow_dispatch aw_context", async () => {
    invocation("workflow_dispatch", { inputs: { aw_context: JSON.stringify({ item_type: "pull_request", item_number: 73, repo: "owner/repo", event_type: "issue_comment" }) } });
    await main();
    expect(github.rest.issues.createComment).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", issue_number: 73 }));
  });

  it("uses forwarded repository_dispatch payload", async () => {
    invocation("repository_dispatch", { action: "commit_comment", client_payload: { repository: { owner: { login: "owner" }, name: "repo" }, comment: { commit_id: sha } } });
    await main();
    expect(github.rest.repos.createCommitComment).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", commit_sha: sha }));
  });

  it.each([
    ["workflow_dispatch", { inputs: { event_name: "push", event_payload: JSON.stringify({ after: sha }) } }],
    ["repository_dispatch", { action: "push", client_payload: { after: sha } }],
  ])("recognizes an effective push forwarded through %s", async (eventName, payload) => {
    invocation(eventName, payload);
    await main();
    expect(github.rest.repos.createCommitComment).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", commit_sha: sha }));
  });

  it.each([
    ["workflow_dispatch", { inputs: { event_repo: "other/target", event_payload: JSON.stringify({ issue: { number: 42 } }) } }],
    ["workflow_dispatch", { inputs: { event_payload: JSON.stringify({ repository: { owner: { login: "other" }, name: "target" }, discussion: { number: 42 } }) } }],
    ["workflow_dispatch", { inputs: { aw_context: JSON.stringify({ item_type: "pull_request", item_number: 73, repo: "other/target", event_type: "issue_comment" }) } }],
    ["workflow_dispatch", { inputs: { target_repo: "other/target", event_payload: JSON.stringify({ issue: { number: 42 } }) } }],
    ["repository_dispatch", { action: "commit_comment", client_payload: { repository: { owner: { login: "other" }, name: "target" }, comment: { commit_id: sha } } }],
    ["issues", { repository: { owner: { login: "other" }, name: "target" }, issue: { number: 42 } }],
  ])("rejects foreign %s destinations before any API call, even if allowlisted", async (eventName, payload) => {
    invocation(eventName, payload);
    vi.stubEnv("GH_AW_ALLOWED_REPOS", "other/target");
    await expect(main()).rejects.toThrow("restricted to the workflow repository");
    expect(github.rest.issues.createComment).not.toHaveBeenCalled();
    expect(github.rest.repos.createCommitComment).not.toHaveBeenCalled();
    expect(github.graphql).not.toHaveBeenCalled();
    expect(summary.write).not.toHaveBeenCalled();
  });

  it("compares workflow repository identity case-insensitively", async () => {
    invocation("workflow_dispatch", { inputs: { event_repo: "OWNER/REPO", event_payload: JSON.stringify({ issue: { number: 42 } }) } });
    await main();
    expect(github.rest.issues.createComment).toHaveBeenCalledOnce();
  });

  it.each(["failure", "cancelled"])("reports completion honestly for %s", async result => {
    invocation("issues", { issue: { number: 42 } });
    vi.stubEnv("GH_AW_REPO_DONE_AGENT_RESULT", result);
    await main();
    expect(github.rest.issues.createComment.mock.calls[0][0].body).toContain(`Agent job result: ${result}.`);
  });

  it.each([
    ["success", "skipped"],
    ["success", ""],
    ["skipped", "success"],
    ["failure", "success"],
    ["cancelled", "success"],
  ])("does not emit for activation=%s, agent=%s", async (activation, agent) => {
    invocation("issues", { issue: { number: 42 } });
    vi.stubEnv("GH_AW_REPO_DONE_ACTIVATION_RESULT", activation);
    vi.stubEnv("GH_AW_REPO_DONE_AGENT_RESULT", agent);
    await main();
    expect(summary.write).not.toHaveBeenCalled();
    expect(github.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it.each(["GH_AW_SAFE_OUTPUTS_STAGED", "GH_AW_REPO_DONE_STAGED", "GH_AW_REPO_DONE_DRY_RUN"])("previews without any API calls when %s is true", async flag => {
    invocation("discussion", { discussion: { number: 42 } });
    vi.stubEnv(flag, "true");
    await main();
    expect(summary.addHeading).toHaveBeenCalledWith(expect.stringContaining("Staged Mode"), 2);
    expect(summary.write).toHaveBeenCalledOnce();
    expect(github.graphql).not.toHaveBeenCalled();
    expect(github.rest.issues.createComment).not.toHaveBeenCalled();
    expect(github.rest.repos.createCommitComment).not.toHaveBeenCalled();
  });

  it("preserves trusted result and run link after hostile Markdown in configured body", async () => {
    invocation("issues", { issue: { number: 42 } });
    vi.stubEnv("GH_AW_REPO_DONE_BODY", "Custom checks 🧪\n\n```md\nunclosed fence");
    await main();
    const body = github.rest.issues.createComment.mock.calls[0][0].body;
    expect(body).toContain("Custom checks 🧪");
    expect(body).toContain("unclosed fence\n```\n\nAgent job result:");
    expect(body).toContain("View workflow run");
  });

  it("surfaces API failures rather than silently dropping the notification", async () => {
    invocation("issues", { issue: { number: 42 } });
    github.rest.issues.createComment.mockRejectedValue(new Error("permission denied"));
    await expect(main()).rejects.toThrow("permission denied");
    expect(summary.write).not.toHaveBeenCalled();
  });
});
