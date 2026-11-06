import { beforeEach, describe, expect, it, vi } from "vitest";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

const { main, createIssue } = require("./create_issue.cjs");
const { normalizeIssueArtifacts } = require("./issue_artifacts.cjs");
const tools = require("./safe_outputs_tools.json");

useExecutionProvenanceFixture();

describe("create_issue artifacts", () => {
  let client;
  const issue = { id: 1234, number: 123, node_id: "I_123", html_url: "https://github.com/test-owner/test-repo/issues/123" };
  const message = { title: "Parser caching", body: "Cache parsed workflows to reduce compilation time.", temporary_id: "aw_cache" };

  beforeEach(() => {
    vi.stubEnv("GH_AW_WORKFLOW_NAME", "Test Workflow");
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com");
    client = {
      rest: { issues: { create: vi.fn().mockResolvedValue({ data: issue }), createComment: vi.fn() } },
      request: vi.fn().mockResolvedValue({ data: { number: 1, html_url: `${issue.html_url}/artifacts/1` } }),
    };
    global.github = client;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 12345, payload: {} };
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn() };
  });

  it("exposes the artifact schema in runtime tools", () => {
    const runtime = tools.find(tool => tool.name === "create_issue").inputSchema.properties.artifacts;
    expect(runtime.items.required).toEqual(["name"]);
    expect(runtime.items.oneOf).toEqual([{ required: ["body"] }, { required: ["path"] }]);
    expect(runtime.items.properties.type.enum).toEqual(["generic", "link", "plan", "embedded_html"]);
    expect(runtime.items.additionalProperties).toBe(false);
  });

  it("rejects unresolved file paths at the privileged processing boundary", () => {
    expect(() => normalizeIssueArtifacts([{ name: "Plan", path: "/tmp/gh-aw/agent/plan.md" }])).toThrow("must be read by the safeoutputs MCP backend before processing");
  });

  it("creates artifacts after the issue without forwarding them to the issue API", async () => {
    const artifacts = [
      { name: "Notes", body: "Research notes" },
      { type: "plan", name: "Plan", body: "Implement caching for #aw_cache" },
      { type: "link", name: "Reference", body: "https://github.com/github/gh-aw" },
      { type: "embedded_html", name: "Preview", body: "<h1>Cache</h1>\n<p>Parser caching</p>" },
    ];
    const handler = await main({});
    const result = await handler({ ...message, artifacts }, {});
    expect(result.success).toBe(true);
    expect(client.rest.issues.create.mock.calls[0][0]).not.toHaveProperty("artifacts");
    expect(client.request).toHaveBeenCalledTimes(4);
    expect(client.rest.issues.create.mock.invocationCallOrder[0]).toBeLessThan(client.request.mock.invocationCallOrder[0]);
    for (const [index, artifact] of artifacts.entries()) {
      expect(client.request.mock.calls[index]).toEqual([
        "POST /repos/{owner}/{repo}/issues/{issue_number}/artifacts",
        {
          owner: "test-owner",
          repo: "test-repo",
          issue_number: 123,
          ...artifact,
          type: artifact.type || "generic",
          body: artifact.body.replace("#aw_cache", "#123"),
          headers: { "X-GitHub-Api-Version": "2026-03-10" },
        },
      ]);
    }
    expect(result.artifacts).toHaveLength(4);
    expect(result.artifacts[0]).toEqual({ number: 1, url: `${issue.html_url}/artifacts/1` });
  });

  it.each(
    [
      null,
      {},
      [null],
      [{ name: "", body: "Notes" }],
      [{ name: "Notes" }],
      [{ name: "Notes", body: 1 }],
      [{ name: "Notes", body: "   " }],
      [{ type: "unknown", name: "Notes", body: "Notes" }],
      [{ type: null, name: "Notes", body: "Notes" }],
      [{ name: "Notes", body: "Notes", repo: "outside/repo" }],
      [{ name: "Notes", body: "x".repeat(1048577) }],
    ].map((artifacts, index) => ({ artifacts, index }))
  )("rejects invalid artifact input before creating an issue (case $index)", async ({ artifacts }) => {
    const handler = await main({});
    const result = await handler({ ...message, artifacts }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("artifacts");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("accepts the exact artifact body limit", () => {
    expect(normalizeIssueArtifacts([{ name: "Notes", body: "x".repeat(1048576) }])[0].body).toHaveLength(1048576);
  });

  it.each([null, [], {}, { number: "1" }, { number: 0 }, { number: 1.5 }, { number: Number.MAX_SAFE_INTEGER + 1 }, { number: 1, html_url: 42 }])("rejects malformed API artifact metadata: %j", async data => {
    client.request.mockResolvedValue({ data });
    const handler = await main({});
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Details" }] }, {});
    expect(result.success).toBe(false);
    expect(result.number).toBe(issue.number);
    expect(result.url).toBe(issue.html_url);
    expect(result.error).toContain("Invalid issue artifact API response");
    expect(result.error).toContain("0/1 attached");
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
  });

  it("accepts API artifact metadata without an optional URL", async () => {
    client.request.mockResolvedValue({ data: { number: 1 } });
    const handler = await main({});
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Details" }] }, {});
    expect(result.success).toBe(true);
    expect(result.artifacts).toEqual([{ number: 1, url: undefined }]);
  });

  it("publishes a 1 MB artifact without the previous half-megabyte truncation", async () => {
    const body = "x".repeat(1048576);
    const handler = await main({});
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body }] }, {});
    expect(result.success).toBe(true);
    expect(client.request.mock.calls[0][1].body).toBe(body);
  });

  it("allows a configured size above the default and exposes no stale schema character cap", async () => {
    const body = "x".repeat(1048577);
    const handler = await main({ max_issue_artifact_size: 1048577 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body }] }, {});
    expect(result.success).toBe(true);
    expect(client.request.mock.calls[0][1].body).toBe(body);
    expect(tools.find(tool => tool.name === "create_issue").inputSchema.properties.artifacts.items.properties.body).not.toHaveProperty("maxLength");
  });

  it("preserves large HTML source and does not apply Markdown line truncation to artifacts", async () => {
    const handler = await main({});
    const html = `<p>${"x".repeat(600000)}</p>`;
    const markdown = "line\n".repeat(70000).trimEnd();
    const result = await handler(
      {
        ...message,
        artifacts: [
          { type: "embedded_html", name: "HTML", body: html },
          { name: "Lines", body: markdown },
        ],
      },
      {}
    );
    expect(result.success).toBe(true);
    expect(client.request.mock.calls[0][1].body).toBe(html);
    expect(client.request.mock.calls[1][1].body).toBe(markdown);
  });

  it.each([false, true])("rejects bodies above the configured byte limit before writes (staged: %s)", async staged => {
    const handler = await main({ staged, max_issue_artifact_size: 4 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "12345" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("5 UTF-8 bytes");
    expect(result.error).toContain("limit of 4 bytes");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("counts UTF-8 bytes rather than JavaScript characters", async () => {
    const handler = await main({ max_issue_artifact_size: 4 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "\u00e9\u00e9\u00e9" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("6 UTF-8 bytes");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(normalizeIssueArtifacts([{ name: "Notes", body: "\u00e9\u00e9" }], 10, 4)[0].body).toBe("\u00e9\u00e9");
  });

  it("measures each body independently rather than combining artifact sizes", async () => {
    const handler = await main({ max_issue_artifact_size: 4 });
    const result = await handler(
      {
        ...message,
        artifacts: [
          { name: "First", body: "1234" },
          { name: "Second", body: "5678" },
        ],
      },
      {}
    );
    expect(result.success).toBe(true);
    expect(client.request).toHaveBeenCalledTimes(2);
  });

  it("rejects bodies that expand beyond the limit during sanitization", async () => {
    const handler = await main({ max_issue_artifact_size: 8 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "@outside" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("10 UTF-8 bytes");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("rejects bodies that expand beyond the limit during reference resolution", async () => {
    const handler = await main({ max_issue_artifact_size: 10 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "#aw_other" }] }, { aw_other: { repo: "other/long-repository", number: 99 } });
    expect(result.success).toBe(false);
    expect(result.error).toContain("max-issue-artifact-size");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("rechecks the final body after resolving a new issue's own temporary ID", async () => {
    client.rest.issues.create.mockResolvedValue({ data: { ...issue, number: 1234567890123456 } });
    const handler = await main({ max_issue_artifact_size: 9 });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "#aw_cache" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("max-issue-artifact-size");
    expect(result.error).toContain("0/1 attached");
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each([0, -1, 1.5, "1024"])("rejects invalid configured sizes explicitly: %s", async max_issue_artifact_size => {
    const handler = await main({ max_issue_artifact_size });
    const result = await handler(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("max-issue-artifact-size must be a positive integer");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("enforces the default cap of 10 before creating an issue", async () => {
    const artifacts = Array.from({ length: 11 }, () => ({ name: "Notes", body: "Research notes" }));
    const handler = await main({});
    const result = await handler({ ...message, artifacts }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("limit of 10 per issue");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("allows the exact configured cap separately for each issue", async () => {
    const handler = await main({ max: 2, max_issue_artifacts: 2 });
    const artifacts = Array.from({ length: 2 }, () => ({ name: "Notes", body: "Research notes" }));
    expect((await handler({ ...message, artifacts }, {})).success).toBe(true);
    expect((await handler({ ...message, title: "Another issue", artifacts }, {})).success).toBe(true);
    expect(client.rest.issues.create).toHaveBeenCalledTimes(2);
    expect(client.request).toHaveBeenCalledTimes(4);
  });

  it.each([false, true])("rejects above the configured cap before writes (staged: %s)", async staged => {
    const handler = await main({ staged, max_issue_artifacts: 1 });
    const artifacts = [
      { name: "First", body: "First notes" },
      { name: "Second", body: "Second notes" },
    ];
    const result = await handler({ ...message, artifacts }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("limit of 1 per issue");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each([0, -1, -20])("disables attachments for max_issue_artifacts=%s but allows ordinary issues", async max_issue_artifacts => {
    const handler = await main({ max_issue_artifacts });
    const rejected = await handler({ ...message, artifacts: [{ name: "Notes", body: "Research notes" }] }, {});
    expect(rejected.success).toBe(false);
    expect(rejected.error).toContain("Issue artifacts are disabled");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
    expect((await handler({ ...message, artifacts: [] }, {})).success).toBe(true);
  });

  it("rejects a malformed configured cap explicitly", async () => {
    const handler = await main({ max_issue_artifacts: 1.5 });
    const result = await handler(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("must be an integer");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it.each([0, -1])("rejects disabled attachments in staged mode for cap %s", async max_issue_artifacts => {
    const handler = await main({ staged: true, max_issue_artifacts });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Research notes" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("Issue artifacts are disabled");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("resolves references to previously created issues in artifact content", async () => {
    const handler = await main({});
    await handler({ ...message, artifacts: [{ name: "Notes", body: "See #aw_other for context" }] }, { aw_other: { repo: "test-owner/test-repo", number: 99 } });
    expect(client.request.mock.calls[0][1].body).toBe("See #99 for context");
  });

  it("does not issue artifact requests when no artifacts are supplied", async () => {
    const handler = await main({});
    const result = await handler(message, {});
    expect(result.success).toBe(true);
    expect(result).not.toHaveProperty("artifacts");
    expect(client.request).not.toHaveBeenCalled();
  });

  it("previews artifacts without any writes, including when group-by-day is enabled", async () => {
    const handler = await main({ staged: true, group_by_day: true });
    const result = await handler({ ...message, artifacts: [{ type: "plan", name: "Plan", body: "Implement caching" }] }, {});
    expect(result.staged).toBe(true);
    expect(result.previewInfo.artifacts).toEqual([{ type: "plan", name: "Plan", body: "Implement caching" }]);
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.rest.issues.createComment).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("does not discard artifacts through same-day comment grouping", async () => {
    const handler = await main({ group_by_day: true });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Research notes" }] }, {});
    expect(result.success).toBe(true);
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.rest.issues.createComment).not.toHaveBeenCalled();
    expect(client.request).toHaveBeenCalledOnce();
  });

  it("uses the per-handler client and configured target repository", async () => {
    global.getOctokit = vi.fn().mockReturnValue(client);
    global.github = { request: vi.fn() };
    const handler = await main({ "github-token": "test-token", "target-repo": "other/project" });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Research notes" }] }, {});
    expect(result.success).toBe(true);
    expect(client.request.mock.calls[0][1]).toMatchObject({ owner: "other", repo: "project", issue_number: 123 });
    expect(global.github.request).not.toHaveBeenCalled();
  });

  it("reports partial attachment failure with the existing issue and does not recreate it", async () => {
    client.request.mockResolvedValueOnce({ data: { number: 1 } }).mockRejectedValueOnce(Object.assign(new Error("Forbidden"), { status: 403 }));
    const handler = await main({});
    const result = await handler(
      {
        ...message,
        artifacts: [
          { name: "First", body: "First notes" },
          { name: "Second", body: "Second notes" },
        ],
      },
      {}
    );
    expect(result).toMatchObject({ success: false, number: 123, url: issue.html_url, temporaryId: "aw_cache" });
    expect(result.error).toContain('artifact "Second"');
    expect(result.error).toContain("1/2 attached");
    expect(result.error).toContain("Forbidden");
    expect(global.core.error).toHaveBeenCalledWith(result.error);
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.request).toHaveBeenCalledTimes(2);
  });

  it("assigns copilot and links the parent after partial attachment failure", async () => {
    vi.stubEnv("GH_AW_ASSIGN_COPILOT", "true");
    client.rest.issues.get = vi.fn().mockResolvedValue({ data: { ...issue, title: message.title, body: "", assignees: [] } });
    client.request
      .mockResolvedValue({ status: 204 })
      .mockResolvedValueOnce({ data: { number: 1 } })
      .mockRejectedValueOnce(Object.assign(new Error("Forbidden"), { status: 403 }));
    client.graphql = vi
      .fn()
      .mockResolvedValueOnce({ repository: { issue: { id: "I_parent_456" } } })
      .mockResolvedValueOnce({ repository: { issue: { id: issue.node_id } } })
      .mockResolvedValueOnce({ addSubIssue: { issue: { id: "I_parent_456" }, subIssue: { id: issue.node_id } } });
    const handler = await main({ assignees: ["copilot"] });
    const result = await handler(
      {
        ...message,
        parent: 456,
        artifacts: [
          { name: "First", body: "First notes" },
          { name: "Second", body: "Second notes" },
          { name: "Third", body: "Third notes" },
        ],
      },
      {}
    );

    expect(result).toMatchObject({ success: false, repo: "test-owner/test-repo", number: issue.number, url: issue.html_url, temporaryId: message.temporary_id });
    expect(result.error).toContain('artifact "Second"');
    expect(result.error).toContain("1/3 attached");
    expect(result.error).toContain("Forbidden");
    expect(result).not.toHaveProperty("artifacts");
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.request.mock.calls.filter(([route]) => route.endsWith("/artifacts"))).toHaveLength(2);
    expect(client.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/assignees", expect.objectContaining({ owner: "test-owner", repo: "test-repo", issue_number: issue.number }));
    expect(client.rest.issues.get.mock.invocationCallOrder[0]).toBeGreaterThan(client.request.mock.invocationCallOrder[1]);
    expect(client.graphql).toHaveBeenNthCalledWith(3, expect.stringContaining("mutation AddSubIssue"), { parentId: "I_parent_456", subIssueId: issue.node_id });
    expect(client.rest.issues.createComment).not.toHaveBeenCalled();
  });

  it("keeps the artifact failure when parent linking needs its comment fallback", async () => {
    client.request.mockRejectedValueOnce(Object.assign(new Error("Forbidden"), { status: 403 }));
    client.graphql = vi.fn().mockRejectedValueOnce(new Error("Sub-issue linking unavailable"));
    client.rest.issues.createComment.mockResolvedValue({ data: {} });
    const handler = await main({});
    const result = await handler({ ...message, parent: 456, artifacts: [{ name: "Notes", body: "Details" }] }, {});

    expect(result).toMatchObject({ success: false, repo: "test-owner/test-repo", number: issue.number, url: issue.html_url, temporaryId: message.temporary_id });
    expect(result.error).toContain("0/1 attached");
    expect(result.error).toContain("Forbidden");
    expect(result.error).not.toContain("Sub-issue linking unavailable");
    expect(client.rest.issues.createComment).toHaveBeenCalledOnce();
    expect(client.rest.issues.createComment).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 456,
      body: expect.stringContaining("Created related issue: #123"),
    });
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.request).toHaveBeenCalledOnce();
  });

  it("retains the issue identity and attachment error if a later post-create step throws", async () => {
    vi.stubEnv("GH_AW_ASSIGN_COPILOT", "true");
    vi.stubEnv("GH_AW_ASSIGN_TO_AGENT_TOKEN", "agent-token");
    global.getOctokit = vi.fn().mockImplementation(() => {
      throw new Error("Assignment client unavailable");
    });
    client.request.mockRejectedValueOnce(Object.assign(new Error("Forbidden"), { status: 403 }));
    const handler = await main({ assignees: ["copilot"] });
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Details" }] }, {});

    expect(result).toMatchObject({ success: false, repo: "test-owner/test-repo", number: issue.number, url: issue.html_url, temporaryId: message.temporary_id });
    expect(result.error).toContain("0/1 attached");
    expect(result.error).toContain("Forbidden");
    expect(result.error).not.toContain("Assignment client unavailable");
    expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("Assignment client unavailable"));
    expect(client.rest.issues.create).toHaveBeenCalledOnce();
    expect(client.request).toHaveBeenCalledOnce();
  });

  it("does not write artifacts when issue creation fails", async () => {
    client.rest.issues.create.mockRejectedValue(Object.assign(new Error("Forbidden"), { status: 403 }));
    const handler = await main({});
    const result = await handler({ ...message, artifacts: [{ name: "Notes", body: "Research notes" }] }, {});
    expect(result.success).toBe(false);
    expect(client.request).not.toHaveBeenCalled();
  });

  it("sanitizes artifact names, mentions and URLs without corrupting HTML source", async () => {
    const handler = await main({});
    await handler(
      {
        ...message,
        artifacts: [
          { name: "@outside", body: "@outside https://outside.invalid" },
          { type: "embedded_html", name: "HTML", body: '<h1>Preview</h1>\n<p>```</p>\n<a href="https://outside.invalid">Link</a>' },
        ],
      },
      {}
    );
    const notes = client.request.mock.calls[0][1];
    expect(notes.name).not.toBe("@outside");
    expect(notes.body).toContain("`@outside`");
    expect(notes.body).not.toContain("https://outside.invalid");
    const html = client.request.mock.calls[1][1].body;
    expect(html).toContain("<h1>Preview</h1>");
    expect(html).toContain("<p>```</p>");
    expect(html).not.toContain("https://outside.invalid");
    expect(html).not.toContain("````html");
  });

  describe("rendered HTML artifact safety", () => {
    it.each(["allowed-only", "allowed-or-code-region"])("filters HTML URLs strictly under workflow policy %s", async policy => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", policy);
      const handler = await main({});
      const result = await handler(
        {
          ...message,
          artifacts: [
            {
              type: "embedded_html",
              name: "Preview",
              body: '<h1>Preview</h1>\n```\n<img src="https://outside.invalid/beacon" onerror="alert(1)">\n```\n<p>`https://outside.invalid/inline`</p>\n<img src="https://github.com/image.png">',
            },
          ],
        },
        {}
      );
      expect(result.success).toBe(true);
      const html = client.request.mock.calls[0][1].body;
      expect(html).toContain("<h1>Preview</h1>");
      expect(html).toContain('src="https://github.com/image.png"');
      expect(html).not.toContain("https://outside.invalid");
      expect(html).not.toContain("onerror");
    });

    it("neutralizes executable HTML tags, attributes and protocols including backtick regions", async () => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-or-code-region");
      const handler = await main({});
      const result = await handler(
        {
          ...message,
          artifacts: [
            {
              type: "embedded_html",
              name: "Preview",
              body: '<script>alert(1)</script>\n<iframe src="https://outside.invalid"></iframe>\n<form action="https://outside.invalid"><input></form>\n```\n<p onclick="alert(1)" style="background:url(https://outside.invalid)">Content</p>\n<a href="javascript:alert(1)">Link</a>\n<img src="javascript:alert(1)" onload="alert(1)">\n<!-- hidden payload -->\n```',
            },
          ],
        },
        {}
      );
      expect(result.success).toBe(true);
      const html = client.request.mock.calls[0][1].body;
      expect(html).toContain("<p>Content</p>");
      expect(html).not.toMatch(/<(?:script|iframe|form|input|a)\b/i);
      expect(html).not.toMatch(/onclick|onload|style=|javascript:|https:\/\/outside\.invalid|<!--/i);
    });

    it("keeps the workflow's code-region URL policy for non-HTML artifacts", async () => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-or-code-region");
      const handler = await main({});
      const body = "Example:\n```\nhttps://outside.invalid/example\n```";
      const result = await handler({ ...message, artifacts: [{ name: "Source", body }] }, {});
      expect(result.success).toBe(true);
      expect(client.request.mock.calls[0][1].body).toBe(body);
      expect(process.env.GH_AW_SAFE_OUTPUTS_URLS).toBe("allowed-or-code-region");
    });

    it.each(['<img src="//outside.invalid/beacon">', '<img src="https&#58;//outside.invalid/beacon">', '<img src="javascript&#58;alert(1)">'])("filters protocol-relative and encoded HTML URLs: %s", async body => {
      vi.stubEnv("GH_AW_SAFE_OUTPUTS_URLS", "allowed-or-code-region");
      const handler = await main({});
      const result = await handler({ ...message, artifacts: [{ type: "embedded_html", name: "Preview", body }] }, {});
      expect(result.success).toBe(true);
      expect(client.request.mock.calls[0][1].body).not.toMatch(/\/\/outside\.invalid|javascript(?:&#58;|:)/);
    });

    it("preserves safe disclosure structure while removing event handlers", async () => {
      const handler = await main({});
      const result = await handler({ ...message, artifacts: [{ type: "embedded_html", name: "Preview", body: '<details ontoggle="alert(1)"><summary>Safe</summary><p>Body</p></details>' }] }, {});
      expect(result.success).toBe(true);
      expect(client.request.mock.calls[0][1].body).toBe("<details><summary>Safe</summary><p>Body</p></details>");
    });

    it("rejects HTML bodies that become empty during sanitization before creating an issue", async () => {
      const handler = await main({});
      const result = await handler({ ...message, artifacts: [{ type: "embedded_html", name: "Preview", body: "<!-- hidden payload -->" }] }, {});
      expect(result.success).toBe(false);
      expect(result.error).toContain("must remain non-empty after sanitization");
      expect(client.rest.issues.create).not.toHaveBeenCalled();
      expect(client.request).not.toHaveBeenCalled();
    });
  });

  it("preserves custom-job artifacts and keeps them out of the issue request", async () => {
    const result = await createIssue({ title: message.title, body: message.body, artifacts: [{ name: "Notes", body: "Trusted notes" }] });
    expect(result.staged).toBe(false);
    expect(result.artifacts).toEqual([{ number: 1, url: `${issue.html_url}/artifacts/1` }]);
    expect(client.rest.issues.create.mock.calls[0][0]).not.toHaveProperty("artifacts");
    expect(client.request.mock.calls[0][1]).toMatchObject({ type: "generic", name: "Notes", body: "Trusted notes" });
  });

  it("previews custom-job artifacts in staged mode", async () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUTS_STAGED", "true");
    const result = await createIssue({ title: message.title, body: message.body, artifacts: [{ name: "Notes", body: "Trusted notes" }] });
    expect(result.staged).toBe(true);
    expect(result.preview.artifacts).toEqual([{ type: "generic", name: "Notes", body: "Trusted notes" }]);
    expect(client.rest.issues.create).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });
});
