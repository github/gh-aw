import { beforeEach, describe, expect, it, vi } from "vitest";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

const { main } = require("./update_issue.cjs");
const { upsertIssueArtifacts } = require("./issue_artifacts.cjs");
const tools = require("./safe_outputs_tools.json");

useExecutionProvenanceFixture();

describe("update_issue artifacts", () => {
  let client;
  const issue = { number: 123, title: "Planning", body: "Original body", html_url: "https://github.com/test-owner/test-repo/issues/123" };
  const artifact = { name: "Plan", type: "plan", body: "Implement caching" };
  const route = "GET /repos/{owner}/{repo}/issues/{issue_number}/artifacts";
  const post = "POST /repos/{owner}/{repo}/issues/{issue_number}/artifacts";
  const patch = "PATCH /repos/{owner}/{repo}/issues/{issue_number}/artifacts/{artifact_number}";

  beforeEach(() => {
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com");
    client = {
      rest: { issues: { get: vi.fn().mockResolvedValue({ data: issue }), update: vi.fn().mockResolvedValue({ data: issue }) } },
      request: vi.fn().mockImplementation(async method => ({
        data: method === route ? [] : { number: 7, html_url: `${issue.html_url}/artifacts/7` },
      })),
    };
    global.github = client;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 12345, eventName: "issues", payload: { issue: { number: 123 } } };
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setOutput: vi.fn() };
  });

  it("exposes the same artifact inputs as create_issue", () => {
    const schema = tools.find(tool => tool.name === "update_issue").inputSchema.properties.artifacts;
    expect(schema.items).toEqual(tools.find(tool => tool.name === "create_issue").inputSchema.properties.artifacts.items);
  });

  it("attaches an artifact-only request to the triggering issue without editing its body", async () => {
    const handler = await main({ target: "triggering" });
    const result = await handler({ issue_number: 999, artifacts: [artifact] }, {});
    expect(result.success).toBe(true);
    expect(result.number).toBe(123);
    expect(result.artifacts).toEqual([{ number: 7, url: `${issue.html_url}/artifacts/7` }]);
    expect(client.rest.issues.update).not.toHaveBeenCalled();
    expect(client.request).toHaveBeenLastCalledWith(post, {
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      ...artifact,
      headers: { "X-GitHub-Api-Version": "2026-03-10" },
    });
  });

  it("adds a version by PATCH on the second run instead of creating a duplicate", async () => {
    const first = await main({});
    expect((await first({ artifacts: [artifact] }, {})).success).toBe(true);
    client.request.mockClear();
    client.request.mockImplementation(async method => ({ data: method === route ? [{ number: 7, name: "Plan", type: "plan" }] : { number: 7 } }));
    const second = await main({});
    expect((await second({ artifacts: [{ ...artifact, body: "Revised plan" }] }, {})).success).toBe(true);
    expect(client.request).toHaveBeenLastCalledWith(patch, {
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      artifact_number: 7,
      name: "Plan",
      body: "Revised plan",
      headers: { "X-GitHub-Api-Version": "2026-03-10" },
    });
    expect(client.request.mock.calls.some(([method]) => method === post)).toBe(false);
  });

  it("finds matching names after a full page even without Link headers", async () => {
    client.request
      .mockResolvedValueOnce({ data: Array.from({ length: 100 }, (_, index) => ({ number: index + 1, name: `Note ${index}`, type: "generic" })) })
      .mockResolvedValueOnce({ data: [{ number: 101, name: "Plan", type: "plan" }] })
      .mockResolvedValueOnce({ data: { number: 101 } });
    const handler = await main({});
    expect((await handler({ artifacts: [artifact] }, {})).success).toBe(true);
    expect(client.request.mock.calls[1]).toEqual([route, expect.objectContaining({ page: 2, per_page: 100 })]);
    expect(client.request).toHaveBeenLastCalledWith(patch, expect.objectContaining({ artifact_number: 101 }));
  });

  it("uses the same sanitized name for matching and publishing", async () => {
    const name = "Plan".repeat(32);
    client.request.mockResolvedValueOnce({ data: [{ number: 7, name, type: "plan" }] }).mockResolvedValueOnce({ data: { number: 7 } });
    const handler = await main({});
    expect((await handler({ artifacts: [{ ...artifact, name: `${name}!` }] }, {})).success).toBe(true);
    expect(client.request).toHaveBeenLastCalledWith(patch, expect.objectContaining({ name }));
  });

  it("upserts repeated names within a call rather than creating two artifacts", async () => {
    const handler = await main({});
    const result = await handler({ artifacts: [artifact, { ...artifact, body: "Revised plan" }] }, {});
    expect(result.success).toBe(true);
    expect(client.request.mock.calls.map(([method]) => method)).toEqual([route, post, patch]);
  });

  it("does not forward artifacts or internal limits to the ordinary issue API", async () => {
    const handler = await main({});
    const result = await handler({ title: "Updated planning", artifacts: [artifact] }, {});
    expect(result.success).toBe(true);
    expect(client.rest.issues.update).toHaveBeenCalledWith({ owner: "test-owner", repo: "test-repo", issue_number: 123, title: "Updated planning" });
  });

  it.each([true, false])("validates count and byte limits before any writes (staged=%s)", async staged => {
    const handler = await main({ staged, max_issue_artifacts: 1, max_issue_artifact_size: 2 });
    const count = await handler({ title: "Update", artifacts: [artifact, artifact] }, {});
    expect(count.success).toBe(false);
    expect(count.error).toContain("max-issue-artifacts");
    const size = await handler({ title: "Update", artifacts: [{ ...artifact, body: "éa" }] }, {});
    expect(size.success).toBe(false);
    expect(size.error).toContain("3 UTF-8 bytes");
    expect(client.rest.issues.update).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each([0, -1])("disables artifacts with cap %s but keeps ordinary issue updates working", async max_issue_artifacts => {
    const handler = await main({ max_issue_artifacts });
    const result = await handler({ title: "Update", artifacts: [artifact] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("disabled");
    expect(client.rest.issues.update).not.toHaveBeenCalled();
    expect((await handler({ title: "Update" }, {})).success).toBe(true);
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each(
    [
      null,
      {},
      [null],
      [{ name: "Plan" }],
      [{ name: "Plan", body: "Plan", path: "/tmp/gh-aw/agent/plan.md" }],
      [{ name: "Plan", path: "/tmp/gh-aw/agent/plan.md" }],
      [{ name: "Plan", type: "unknown", body: "Plan" }],
      [{ name: "Plan", body: "x".repeat(1048577) }],
      Array.from({ length: 11 }, () => ({ name: "Plan", body: "Plan" })),
    ].map((artifacts, index) => ({ artifacts, index }))
  )("rejects malformed or over-default-limit artifacts before issue writes (case $index)", async ({ artifacts }) => {
    const handler = await main({});
    const result = await handler({ title: "Update", artifacts }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("artifacts");
    expect(client.rest.issues.update).not.toHaveBeenCalled();
    expect(client.request).not.toHaveBeenCalled();
  });

  it("previews artifact-only requests without any artifact API calls", async () => {
    const handler = await main({ staged: true });
    const result = await handler({ artifacts: [artifact] }, {});
    expect(result).toMatchObject({ success: true, staged: true, previewInfo: { updateFields: ["artifacts"] } });
    expect(client.request).not.toHaveBeenCalled();
    expect(client.rest.issues.update).not.toHaveBeenCalled();
  });

  it("sanitizes HTML, mentions, and URLs before publishing without truncating large content", async () => {
    const handler = await main({});
    const body = `<p>${"x".repeat(600000)}</p><script>alert(1)</script> @someone https://outside.example/private`;
    expect((await handler({ artifacts: [{ name: "Preview", type: "embedded_html", body }] }, {})).success).toBe(true);
    const published = client.request.mock.calls[1][1].body;
    expect(published).toContain("x".repeat(600000));
    expect(published).not.toContain("<script>");
    expect(published.includes("`@someone`")).toBe(true);
    expect(published).not.toContain("https://outside.example/private");
  });

  it("checks size after sanitization as well as before it", async () => {
    const handler = await main({ max_issue_artifact_size: 4 });
    const result = await handler({ artifacts: [{ name: "Notes", body: "@abc" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("max-issue-artifact-size");
    expect(client.request).not.toHaveBeenCalled();
  });

  it.each([[null], null, {}, [{ number: "7", name: "Plan", type: "plan" }], [{ number: 7, name: "Plan", type: "unknown" }]])("fails closed on malformed list responses: %j", async data => {
    client.request.mockResolvedValueOnce({ data });
    const handler = await main({});
    const result = await handler({ artifacts: [artifact] }, {});
    expect(result.success).toBe(false);
    expect(result.number).toBe(123);
    expect(result.error).toContain("Invalid issue artifact");
    expect(client.request).toHaveBeenCalledTimes(1);
  });

  it("rejects type changes before any artifact writes, including Markdown into HTML", async () => {
    client.request.mockResolvedValueOnce({ data: [{ number: 7, name: "Preview", type: "embedded_html" }] });
    const handler = await main({});
    const result = await handler({ artifacts: [artifact, { name: "Preview", body: "`<script>alert(1)</script>`" }] }, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("types cannot be changed");
    expect(client.request).toHaveBeenCalledTimes(1);
  });

  it("reports partial failures with the target issue and does not retry the entire update", async () => {
    client.request
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: { number: 7 } })
      .mockRejectedValueOnce(new Error("Forbidden"));
    const handler = await main({});
    const result = await handler({ title: "Update", artifacts: [artifact, { name: "Notes", body: "Research" }] }, {});
    expect(result).toMatchObject({ success: false, number: 123, url: issue.html_url, repo: "test-owner/test-repo" });
    expect(result.error).toContain("1/2 attached");
    expect(client.rest.issues.update).toHaveBeenCalledTimes(1);
    expect(client.request).toHaveBeenCalledTimes(3);
  });

  it("uses cross-repository targets for listing and writing artifacts", async () => {
    const handler = await main({ target: "*", "target-repo": "other/repo" });
    const result = await handler({ issue_number: 456, artifacts: [artifact] }, {});
    expect(result.success).toBe(true);
    for (const [, parameters] of client.request.mock.calls) {
      expect(parameters).toMatchObject({ owner: "other", repo: "repo", issue_number: 456 });
    }
  });

  it("skips the list API when no artifacts are supplied", async () => {
    expect(await upsertIssueArtifacts(client, "owner", "repo", 123, [])).toEqual([]);
    expect(client.request).not.toHaveBeenCalled();
  });
});
