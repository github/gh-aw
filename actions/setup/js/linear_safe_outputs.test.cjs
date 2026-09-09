import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { LINEAR_GRAPHQL_ENDPOINT, linearGraphQL } = require("./linear_graphql.cjs");
const { LINEAR_CREATE_ISSUE, LINEAR_RESOLVE_PROJECT, main: createIssue } = require("./linear_create_issue.cjs");
const { LINEAR_COMMENT_CREATE, main: addComment } = require("./linear_add_comment.cjs");
const { LINEAR_UPDATE_ISSUE, main: updateIssue } = require("./linear_update_issue.cjs");

global.core = { info: vi.fn(), warning: vi.fn(), debug: vi.fn() };

function response(payload, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: vi.fn().mockResolvedValue(payload),
  };
}

describe("Linear safe outputs", () => {
  beforeEach(() => {
    process.env.GH_AW_LINEAR_TOKEN = "linear-secret";
    global.fetch = vi.fn();
    vi.clearAllMocks();
  });

  afterEach(() => {
    delete process.env.GH_AW_LINEAR_TOKEN;
    delete process.env.LINEAR_PROJECT_ID;
    delete process.env.LINEAR_TEAM_ID;
    delete global.fetch;
  });

  it("posts fixed GraphQL documents with variables and raw API-key authorization", async () => {
    fetch
      .mockResolvedValueOnce(response({ data: { projects: { nodes: [{ id: "a3f91a0b-6d71-4c58-a4bb-72b925bbebc8" }] } } }))
      .mockResolvedValueOnce(response({ data: { issueCreate: { success: true, issue: { id: "id", identifier: "ENG-1", title: "Safe title" } } } }));
    const handler = await createIssue({ team_id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d", project_id: "810f57a7e383" });
    await handler({ title: "Safe title", body: "Detailed hello to @user" });

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      LINEAR_GRAPHQL_ENDPOINT,
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: "linear-secret" },
      })
    );
    const projectRequest = JSON.parse(fetch.mock.calls[0][1].body);
    expect(projectRequest).toEqual({ query: LINEAR_RESOLVE_PROJECT, variables: { slugId: "810f57a7e383" } });
    const request = JSON.parse(fetch.mock.calls[1][1].body);
    expect(request.query).toBe(LINEAR_CREATE_ISSUE);
    expect(request.query).not.toContain("Safe title");
    expect(request.variables.input).toEqual({
      teamId: "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
      projectId: "a3f91a0b-6d71-4c58-a4bb-72b925bbebc8",
      title: "Safe title",
      description: "Detailed hello to `@user`",
    });
  });

  it("creates a comment against only the configured target", async () => {
    fetch.mockResolvedValue(response({ data: { commentCreate: { success: true, comment: { id: "comment-id", body: "body" } } } }));
    const handler = await addComment({ target: "ENG-123" });
    await handler({ body: "Comment @team" });

    const request = JSON.parse(fetch.mock.calls[0][1].body);
    expect(request.query).toBe(LINEAR_COMMENT_CREATE);
    expect(request.variables).toEqual({ input: { issueId: "ENG-123", body: "Comment `@team`" } });
  });

  it("updates only enabled fields against the configured target", async () => {
    fetch.mockResolvedValue(response({ data: { issueUpdate: { success: true, issue: { id: "id", identifier: "ENG-123", title: "New" } } } }));
    const handler = await updateIssue({ target: "ENG-123", allow_title: true });
    await expect(handler({ body: "not enabled" })).rejects.toThrow("body updates are not enabled");
    await handler({ title: "New @owner" });

    const request = JSON.parse(fetch.mock.calls[0][1].body);
    expect(request.query).toBe(LINEAR_UPDATE_ISSUE);
    expect(request.variables).toEqual({ id: "ENG-123", input: { title: "New `@owner`" } });
  });

  it("does not perform network requests in staged mode", async () => {
    const handler = await addComment({ target: "ENG-123", staged: true });
    await expect(handler({ body: "Preview" })).resolves.toMatchObject({ success: true, staged: true });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("rejects malformed configured project IDs", async () => {
    await expect(createIssue({ team_id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d", project_id: "not-a-project" })).rejects.toThrow("safe-outputs.linear-create-issue.project-id or LINEAR_PROJECT_ID");
  });

  it("identifies the required team ID configuration", async () => {
    await expect(createIssue({})).rejects.toThrow("safe-outputs.linear-create-issue.team-id or LINEAR_TEAM_ID");
  });

  it("uses global team and project IDs as fallbacks", async () => {
    process.env.LINEAR_TEAM_ID = "9cfb482a-81e3-4154-b5b9-2c805e70a02d";
    process.env.LINEAR_PROJECT_ID = "810f57a7e383";
    fetch
      .mockResolvedValueOnce(response({ data: { projects: { nodes: [{ id: "a3f91a0b-6d71-4c58-a4bb-72b925bbebc8" }] } } }))
      .mockResolvedValueOnce(response({ data: { issueCreate: { success: true, issue: { id: "id", identifier: "ENG-1", title: "Title" } } } }));

    const handler = await createIssue({});
    await handler({ title: "Title", body: "Body with enough detail" });

    expect(JSON.parse(fetch.mock.calls[0][1].body).variables.slugId).toBe("810f57a7e383");
    expect(JSON.parse(fetch.mock.calls[1][1].body).variables.input.teamId).toBe("9cfb482a-81e3-4154-b5b9-2c805e70a02d");
  });

  it("prefers explicit team and project IDs over global fallbacks", async () => {
    process.env.LINEAR_TEAM_ID = "11111111-1111-1111-1111-111111111111";
    process.env.LINEAR_PROJECT_ID = "111111111111";
    fetch
      .mockResolvedValueOnce(response({ data: { projects: { nodes: [{ id: "a3f91a0b-6d71-4c58-a4bb-72b925bbebc8" }] } } }))
      .mockResolvedValueOnce(response({ data: { issueCreate: { success: true, issue: { id: "id", identifier: "ENG-1", title: "Title" } } } }));

    const handler = await createIssue({ team_id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d", project_id: "810f57a7e383" });
    await handler({ title: "Title", body: "Body with enough detail" });

    expect(JSON.parse(fetch.mock.calls[0][1].body).variables.slugId).toBe("810f57a7e383");
    expect(JSON.parse(fetch.mock.calls[1][1].body).variables.input.teamId).toBe("9cfb482a-81e3-4154-b5b9-2c805e70a02d");
  });

  it("rejects invalid explicit IDs instead of using global fallbacks", async () => {
    process.env.LINEAR_TEAM_ID = "11111111-1111-1111-1111-111111111111";
    process.env.LINEAR_PROJECT_ID = "111111111111";

    await expect(createIssue({ team_id: "" })).rejects.toThrow("safe-outputs.linear-create-issue.team-id");
    await expect(createIssue({ team_id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d", project_id: "" })).rejects.toThrow("safe-outputs.linear-create-issue.project-id");
  });

  it("rejects HTTP, malformed JSON, GraphQL, and unsuccessful mutation responses", async () => {
    fetch.mockResolvedValueOnce(response({}, 429));
    await expect(linearGraphQL("query Fixed { viewer { id } }", {})).rejects.toThrow("rate limit exceeded");

    fetch.mockResolvedValueOnce({ ok: true, status: 200, json: vi.fn().mockRejectedValue(new Error("bad")) });
    await expect(linearGraphQL("query Fixed { viewer { id } }", {})).rejects.toThrow("malformed JSON");

    fetch.mockResolvedValueOnce(response({ errors: [{ message: "denied linear-secret" }] }));
    await expect(linearGraphQL("query Fixed { viewer { id } }", {})).rejects.not.toThrow("linear-secret");

    fetch.mockResolvedValueOnce(response({ errors: [{ message: "Entity not found: Team" }] }));
    await expect(linearGraphQL(LINEAR_CREATE_ISSUE, {})).rejects.toThrow("Verify safe-outputs.linear-create-issue.team-id or LINEAR_TEAM_ID references a team in the workspace authorized by LINEAR_API_KEY");

    fetch.mockResolvedValueOnce(response({ data: { issueCreate: { success: false, issue: null } } }));
    const handler = await createIssue({ team_id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d" });
    await expect(handler({ title: "Title", body: "Body with enough detail" })).rejects.toThrow("did not return a successful issue");
  });

  it("rejects oversized content instead of truncating it", async () => {
    const handler = await addComment({ target: "ENG-123" });
    await expect(handler({ body: "x".repeat(65001) })).rejects.toThrow("exceeds 65000 characters");
    expect(fetch).not.toHaveBeenCalled();
  });
});
