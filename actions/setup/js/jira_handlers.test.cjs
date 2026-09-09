// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { main: createIssueMain } = require("./jira_create_issue.cjs");
const { main: updateIssueMain } = require("./jira_update_issue.cjs");
const { main: addCommentMain } = require("./jira_add_comment.cjs");
const { main: addLabelMain } = require("./jira_add_label.cjs");

describe("Jira safe-output handlers", () => {
  let requests;

  beforeEach(() => {
    requests = [];
    global.core = {
      info: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      debug: vi.fn(),
    };
    process.env.JIRA_BASE_URL = "https://example.atlassian.net";
    process.env.JIRA_USER_EMAIL = "jira@example.com";
    process.env.JIRA_API_TOKEN = "secret-token";
    global.fetch = vi.fn(async (url, options) => {
      requests.push({ url, options, body: options.body ? JSON.parse(options.body) : undefined });
      if (url.endsWith("/comment")) {
        return response(201, { id: "20001", self: `${url}/20001` });
      }
      if (options.method === "POST") {
        return response(201, { id: "10042", key: "ENG-123", self: `${url}/10042` });
      }
      return response(204);
    });
  });

  afterEach(() => {
    delete process.env.JIRA_BASE_URL;
    delete process.env.JIRA_USER_EMAIL;
    delete process.env.JIRA_API_TOKEN;
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    vi.unstubAllGlobals();
  });

  it("creates Jira issues with issue type name and ADF description", async () => {
    const handler = await createIssueMain({ max: 1 });
    const result = await handler({
      project_key: "ENG",
      issue_type: "Task",
      summary: "Investigate parser",
      description: "First paragraph\nSecond paragraph",
    });

    expect(result).toMatchObject({ success: true, issue_key: "ENG-123", issue_id: "10042" });
    expect(requests[0]).toMatchObject({
      url: "https://example.atlassian.net/rest/api/3/issue",
      body: {
        fields: {
          project: { key: "ENG" },
          issuetype: { name: "Task" },
          summary: "Investigate parser",
          description: {
            type: "doc",
            version: 1,
          },
        },
      },
    });
    expect(requests[0].body.fields.description.content).toHaveLength(2);
    expect(global.core.debug).toHaveBeenCalledWith("jira_create_issue: processing request");
    expect(global.core.debug).toHaveBeenCalledWith("jira_create_issue: request completed successfully");
  });

  it.each([
    [{ summary: "Updated" }, { summary: "Updated" }],
    [{ description: "Updated description" }, { description: { type: "doc", version: 1 } }],
    [
      { summary: "Updated", description: "Both" },
      { summary: "Updated", description: { type: "doc", version: 1 } },
    ],
  ])("updates only requested Jira fields", async (updates, expectedFields) => {
    const handler = await updateIssueMain({ max: 1 });
    const result = await handler({ issue_key: "ENG-123", ...updates });

    expect(result).toMatchObject({ success: true, issue_key: "ENG-123" });
    expect(requests[0].url).toBe("https://example.atlassian.net/rest/api/3/issue/ENG-123");
    expect(requests[0].body.fields).toMatchObject(expectedFields);
    expect(Object.keys(requests[0].body.fields)).toEqual(Object.keys(updates));
  });

  it("rejects a Jira update with no changed fields", async () => {
    const handler = await updateIssueMain({ max: 1 });
    await expect(handler({ issue_key: "ENG-123" })).resolves.toMatchObject({
      success: false,
      error: "jira_update_issue requires summary or description",
    });
    expect(requests).toHaveLength(0);
  });

  it("adds a Jira comment as ADF", async () => {
    const handler = await addCommentMain({ max: 1 });
    const result = await handler({ issue_key: "ENG-123", body: "Investigation complete." });

    expect(result).toMatchObject({ success: true, issue_key: "ENG-123", comment_id: "20001" });
    expect(requests[0].body).toEqual({
      body: {
        type: "doc",
        version: 1,
        content: [{ type: "paragraph", content: [{ type: "text", text: "Investigation complete." }] }],
      },
    });
  });

  it("preserves valid Jira text without GitHub-specific sanitization", async () => {
    const handler = await createIssueMain({ max: 1 });
    await handler({
      project_key: "ENG",
      issue_type: "Task",
      summary: "Fix @deprecated flag",
      description: "Use {{version}}.",
    });

    expect(requests[0].body.fields.summary).toBe("Fix @deprecated flag");
    expect(requests[0].body.fields.description.content[0].content[0].text).toBe("Use {{version}}.");
  });

  it("adds one Jira label with additive update semantics", async () => {
    const handler = await addLabelMain({ max: 1 });
    const result = await handler({ issue_key: "ENG-123", label: "needs-investigation" });

    expect(result).toMatchObject({ success: true, issue_key: "ENG-123", label: "needs-investigation" });
    expect(requests[0].body).toEqual({ update: { labels: [{ add: "needs-investigation" }] } });
    expect(requests[0].body.fields).toBeUndefined();
  });

  it("rejects labels that Jira cannot accept", async () => {
    const handler = await addLabelMain({ max: 1 });
    await expect(handler({ issue_key: "ENG-123", label: "needs investigation" })).resolves.toMatchObject({
      success: false,
      error: "label must contain only letters, numbers, periods, hyphens, and underscores",
    });
    expect(requests).toHaveLength(0);
  });

  it.each([
    [createIssueMain, { project_key: "ENG", issue_type: "Task", summary: "Preview" }, "Jira create issue"],
    [updateIssueMain, { issue_key: "ENG-123", summary: "Preview" }, "Jira update issue"],
    [addCommentMain, { issue_key: "ENG-123", body: "Preview" }, "Jira add comment"],
    [addLabelMain, { issue_key: "ENG-123", label: "preview" }, "Jira add label"],
  ])("stages every Jira operation without credentials or HTTP requests", async (factory, message, previewText) => {
    delete process.env.JIRA_BASE_URL;
    delete process.env.JIRA_USER_EMAIL;
    delete process.env.JIRA_API_TOKEN;
    const handler = await factory({ max: 1, staged: true });
    const result = await handler(message);

    expect(result).toMatchObject({ success: true, staged: true });
    expect(global.fetch).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining(previewText));
  });

  it("returns a safe Jira API error", async () => {
    global.fetch = vi.fn(async () =>
      response(400, {
        errorMessages: ["Invalid project"],
        errors: { summary: "Invalid summary" },
      })
    );
    const handler = await createIssueMain({ max: 1 });
    const result = await handler({ project_key: "BAD", issue_type: "Task", summary: "Bad issue" });

    expect(result).toMatchObject({ success: false });
    expect(result.error).toContain("Invalid project");
    expect(result.error).not.toContain("secret-token");
    expect(global.core.debug).toHaveBeenCalledWith("jira_create_issue: request failed");
  });
});

function response(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 204 ? "No Content" : status === 201 ? "Created" : "Bad Request",
    text: async () => (body === undefined ? "" : JSON.stringify(body)),
  };
}
