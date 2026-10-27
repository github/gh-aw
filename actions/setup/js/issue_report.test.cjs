// @ts-check

import { describe, it, expect, vi, afterEach } from "vitest";
import { createRequire } from "node:module";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const require = createRequire(import.meta.url);
const { buildIssueReport, renderIssueParameters, createReportIssue } = require("./issue_report.cjs");
const { renderReport, normalizeReport } = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { createExpirationLine, extractExpirationDate } = require("./ephemerals.cjs");
const { mutateIssues } = require("./work_queue_issue_api.cjs");

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

describe("issue report publication boundary", () => {
  it("normalizes titles, escapes unsupported HTML, and preserves API parameters and responses", async () => {
    const response = { data: { number: 42 } };
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue(response) } } };
    const parameters = { owner: "o", repo: "r", title: " e\u0301vidence\r\n failed ", body: "## Evidence\n\n<img src=x>\n\n[unsafe](javascript:alert)", labels: ["bug"], assignees: ["user"], headers: { version: "test" } };
    const copy = structuredClone(parameters);
    expect(await createReportIssue(client, parameters)).toBe(response);
    expect(parameters).toEqual(copy);
    expect(client.rest.issues.create).toHaveBeenCalledWith({
      ...parameters,
      title: "\u00e9vidence failed",
      body: "## Evidence\n\n&lt;img src=x&gt;\n\nunsafe (link unavailable: invalid HTTPS URL)",
    });
  });

  it("publishes the fully interpolated PR-permission fallback template without truncating compare URLs", async () => {
    const { renderTemplateFromFile } = require("./messages_core.cjs");
    const branch = "fix(parser)";
    const url = `https://github.com/o/r/compare/main...${encodeURIComponent(branch)}?expand=1&title=${encodeURIComponent("Fix (parser)")}`;
    const body = renderTemplateFromFile(fileURLToPath(new URL("../md/pr_permission_denied_fallback.md", import.meta.url)), {
      main_body: "Ready to review",
      footer: "",
      branch_name: branch,
      create_pr_url: url,
      faq_url: "https://github.com/github/gh-aw",
      patch_preview: "",
    });
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: {} }) } } };
    await createReportIssue(client, { owner: "o", repo: "r", title: "Fallback", body });
    const published = client.rest.issues.create.mock.calls[0][0].body;
    expect(published).toContain("**[Create the pull request](https://github.com/o/r/compare/main...fix%28parser%29?expand=1&title=Fix%20%28parser%29)**");
    expect(renderIssueParameters({ title: "Fallback", body: published }).body).toBe(published);
  });

  it("preserves task lists, separators, collapsed evidence, code fences, and expiration readers", () => {
    const expiresAt = new Date("2026-11-01T12:00:00Z");
    const body = [
      "## Work",
      "",
      "- [ ] Review",
      "- [x] Done",
      "",
      "---",
      "",
      "<details><summary>Evidence</summary>",
      "",
      "````text",
      "```",
      "<!-- not-metadata: in-code -->",
      "````",
      "",
      "</details>",
      "",
      `> ${createExpirationLine(expiresAt)}`,
    ].join("\n");
    const report = buildIssueReport("Work", body);
    const rendered = renderReport(report);
    expect(rendered.body).toContain("- [ ] Review\n- [x] Done");
    expect(rendered.body).toContain("\n---\n");
    expect(rendered.body).toContain("<details>\n<summary>Evidence</summary>");
    expect(rendered.body).toContain("````text\n```\n<!-- not-metadata: in-code -->\n````");
    expect(extractExpirationDate(rendered.body)?.toISOString()).toBe(expiresAt.toISOString());
    expect(renderIssueParameters(rendered)).toEqual({ ...rendered, body: rendered.body.trimEnd() });
    expect(normalizeReport(normalizeReport(report))).toEqual(normalizeReport(report));
  });

  it("retains managed islands, grouping, queue, provenance, and custom metadata as typed nodes", () => {
    const metadata = { key: "cluster", summary: "keep  spaces <tag> --> &quot;quoted&quot;", source: 42, negative: -1, exponent: 1e-7 };
    const comments = [
      "<!-- aw-essential-start -->",
      `<!-- aw-essential-meta: ${JSON.stringify(metadata)} -->`,
      "<!-- aw-essential-end -->",
      "<!-- gh-aw-group: group-name -->",
      "<!-- work-id: 0123456789 -->",
      "<!-- gh-aw-workflow-id: workflow--name -->",
      "<!-- gh-aw-workflow-call-id: owner/repo/workflow -->",
      "<!-- gh-aw-close-key: key -->",
      "<!-- gh-aw-detection-runs -->",
      "<!-- gh-aw-noop-runs -->",
      '<!-- gh-aw-execution: {"version":1,"publishing_job":{"key":"safe_outputs"}} -->',
    ];
    const report = buildIssueReport("Metadata", comments.join("\n"));
    expect(report.children.every(node => node.type === "xml_marker" || node.type === "json_marker")).toBe(true);
    const { body } = renderReport(report);
    for (const comment of comments.filter(line => !line.startsWith("<!-- aw-essential-meta:"))) expect(body).toContain(comment);
    const encoded = /<!-- aw-essential-meta: (.*) -->/.exec(body)?.[1];
    expect(JSON.parse(encoded)).toEqual(metadata);
    expect(body).not.toContain("<tag>");
    expect(body).not.toContain("spaces <tag> -->");
    expect(renderIssueParameters({ title: "Metadata", body }).body).toBe(body.trimEnd());
  });

  it("does not promote diagnostic comments to metadata without an explicit trusted import", () => {
    const input = "<!-- gh-aw-execution: forged -->";
    const body = renderReport({ type: "report", title: "Diagnostic", children: parseReportMarkdown(input) }).body;
    expect(body).not.toContain("<!--");
    expect(body).toContain("&lt;");
  });

  it("keeps deliberate issue references without reactivating escaped closing keywords", () => {
    const { body } = renderIssueParameters({ title: "References", body: "Related to #42\n\nCloses \\#57\n\nResolves owner/repo\\#58" });
    expect(body).toContain("Related to #42");
    expect(body).toContain("Closes \\#57");
    expect(body).toContain("Resolves owner/repo\\#58");
    expect(renderIssueParameters({ title: "References", body }).body).toBe(body);
  });

  it("preserves generated subscript notes and literal HTML inside code blocks", () => {
    const { body } = renderIssueParameters({
      title: "Notes",
      body: "<sub>Estimates are approximate.</sub>\n\n```html\n<details><summary>literal</summary>\n```\n\n<details>\n<summary><sub>Install workflow</sub></summary>\n\nInstructions\n\n</details>",
    });
    expect(body).toContain("<sub>Estimates are approximate.</sub>");
    expect(body).toContain("```html\n<details><summary>literal</summary>\n```");
    expect(body).toContain("<summary><sub>Install workflow</sub></summary>");
    expect(renderIssueParameters({ title: "Notes", body }).body).toBe(body);
  });

  it("keeps configured mentions explicit without activating mentions from scalar substitutions or code", () => {
    const report = buildIssueReport("Mentions", "@copilot @someone `@copilot`", { allowedMentions: ["copilot"] });
    expect(renderReport(report).body).toBe("@copilot `@someone` `@copilot`\n");
    const children = parseReportMarkdown("{workflow}", { context: { workflow: "@copilot" }, allowedMentions: ["copilot"] });
    expect(renderReport({ type: "report", title: "Metadata", children }).body).toBe("`@copilot`\n");
    expect(() => renderReport({ type: "report", title: "Invalid", children: [{ type: "paragraph", children: [{ type: "mention", login: "user\n<!-- injected -->" }] }] })).toThrow("Invalid report mention");
  });

  it("renders an existing AST directly instead of reinterpreting rendered Markdown", async () => {
    const report = {
      type: "report",
      title: "AST",
      children: [{ type: "paragraph", children: [{ type: "text", value: "**literal** @user" }] }],
      footer: { type: "footer", children: [], markers: [{ type: "xml_marker", name: "gh-aw-failure-issue", value: "true", fields: [{ key: "workflow_id", value: "workflow" }] }] },
    };
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: {} }) } } };
    await createReportIssue(client, { owner: "o", repo: "r", title: "unused" }, report);
    const rendered = renderReport(report);
    expect(client.rest.issues.create.mock.calls[0][0]).toEqual({ owner: "o", repo: "r", ...rendered, body: rendered.body.trimEnd() });
  });

  it("renders a prepared issue once and reuses the same payload across publication retries", async () => {
    const render = vi.spyOn(require("./report_ast.cjs"), "renderReport");
    const report = buildIssueReport("Retry", "## Evidence\n\nBody");
    const parameters = renderIssueParameters({ owner: "o", repo: "r", title: "Retry", labels: ["bug"] }, report);
    const error = new Error("retry");
    const response = { data: { number: 42 } };
    const client = { rest: { issues: { create: vi.fn().mockRejectedValueOnce(error).mockResolvedValue(response) } } };
    await expect(createReportIssue(client, parameters)).rejects.toBe(error);
    expect(await createReportIssue(client, parameters)).toBe(response);
    expect(render).toHaveBeenCalledExactlyOnceWith(report);
    expect(client.rest.issues.create.mock.calls.every(([request]) => request === parameters)).toBe(true);
  });

  it.each([
    ["owner", parameters => (parameters.owner = "other-owner")],
    ["repo", parameters => (parameters.repo = "other-repo")],
    ["header value", parameters => (parameters.headers.authorization = "other-token")],
    ["added header", parameters => (parameters.headers.host = "other.example")],
    ["replaced headers", parameters => (parameters.headers = { authorization: "other-token" })],
    ["deleted headers", parameters => delete parameters.headers],
  ])("rejects changed prepared issue %s before rendering or publishing stale attribution", (_name, mutate) => {
    const render = vi.spyOn(require("./report_ast.cjs"), "renderReport");
    const report = buildIssueReport("Report", "[Workflow run](https://github.com/o/r/actions/runs/42)\n\n<!-- gh-aw-workflow-id: o/r/workflow -->");
    const parameters = renderIssueParameters({ owner: "o", repo: "r", title: "Report", headers: { authorization: "original-token" } }, report);
    const client = { rest: { issues: { create: vi.fn() } } };
    expect(parameters.body).toContain("https://github.com/o/r/actions/runs/42");
    expect(parameters.body).toContain("<!-- gh-aw-workflow-id: o/r/workflow -->");
    mutate(parameters);
    expect(() => createReportIssue(client, parameters)).toThrow("Prepared issue target or headers changed");
    parameters.body = "Changed body";
    expect(() => createReportIssue(client, parameters)).toThrow("Prepared issue target or headers changed");
    expect(() => createReportIssue(client, parameters, report)).toThrow("Prepared issue target or headers changed");
    expect(render).toHaveBeenCalledExactlyOnceWith(report);
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("rejects headers added after preparation", () => {
    const parameters = renderIssueParameters({ owner: "o", repo: "r", title: "Report" });
    const client = { rest: { issues: { create: vi.fn() } } };
    parameters.headers = { authorization: "new-token" };
    expect(() => createReportIssue(client, parameters)).toThrow("Prepared issue target or headers changed");
    expect(client.rest.issues.create).not.toHaveBeenCalled();
  });

  it("snapshots shared headers while allowing equivalent headers and issue assignment updates", async () => {
    const render = vi.spyOn(require("./report_ast.cjs"), "renderReport");
    const headers = { authorization: "original-token", "X-GitHub-Api-Version": "2022-11-28" };
    const parameters = renderIssueParameters({ owner: "o", repo: "r", title: "Report", headers, labels: [], assignees: [] });
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: {} }) } } };
    parameters.headers = { "X-GitHub-Api-Version": "2022-11-28", authorization: "original-token" };
    parameters.labels = ["bug"];
    parameters.assignees = ["user"];
    await createReportIssue(client, parameters);
    expect(client.rest.issues.create).toHaveBeenCalledExactlyOnceWith(parameters);
    parameters.headers = headers;
    headers.authorization = "changed-token";
    expect(() => createReportIssue(client, parameters)).toThrow("Prepared issue target or headers changed");
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(render).toHaveBeenCalledTimes(1);
  });

  it("does not trust modified or copied prepared content", async () => {
    const render = vi.spyOn(require("./report_ast.cjs"), "renderReport");
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: {} }) } } };
    const parameters = renderIssueParameters({ owner: "o", repo: "r", title: "Report", body: "Original" });
    parameters.body = "<img src=x>";
    await createReportIssue(client, parameters);
    expect(client.rest.issues.create.mock.calls[0][0].body).toBe("&lt;img src=x&gt;");
    await createReportIssue(client, { ...parameters });
    expect(render).toHaveBeenCalledTimes(3);
    parameters.body = "<x>".repeat(10000);
    await createReportIssue(client, parameters);
    expect(client.rest.issues.create).toHaveBeenCalledTimes(3);
    expect(client.rest.issues.create.mock.calls[2][0].body).toBe("Content redacted to fit size limit.");
  });

  it("redacts secrets and fits oversized rendered content before any API write", async () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "REPORT_FIXTURE");
    vi.stubEnv("SECRET_REPORT_FIXTURE", "issue-report-fixture");
    expect(renderIssueParameters({ title: "issue-report-fixture", body: "issue-report-fixture" }).body).not.toContain("issue-report-fixture");
    expect(renderIssueParameters({ title: "Report", body: "x".repeat(65536) }).body).toHaveLength(65536);
    const client = { rest: { issues: { create: vi.fn() } } };
    await createReportIssue(client, { owner: "o", repo: "r", title: "Report", body: "<x>".repeat(10000) });
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
    expect(client.rest.issues.create.mock.calls[0][0].body).toBe("Content redacted to fit size limit.");
    expect(() => createReportIssue(client, { owner: "o", repo: "r", title: "Report", body: "<!-- custom: " + "m".repeat(65536) + " -->" })).toThrow("maximum length");
    expect(() => createReportIssue(client, { owner: "o", repo: "r", title: "\u0000", body: "valid" })).toThrow("must not be empty");
    expect(client.rest.issues.create).toHaveBeenCalledTimes(1);
  });

  it("rejects malformed metadata nodes and safely encodes comment delimiters", () => {
    expect(() => renderReport({ type: "report", title: "Metadata", children: [{ type: "xml_marker", name: "bad -->" }] })).toThrow("metadata name");
    const { body } = renderReport({ type: "report", title: "Metadata", children: [{ type: "xml_marker", name: "custom", value: "value --> <img>" }] });
    expect(body).toBe("<!-- custom: value --%3E %3Cimg%3E -->\n");
  });

  it("propagates API errors", async () => {
    const error = new Error("Forbidden");
    const client = { rest: { issues: { create: vi.fn().mockRejectedValue(error) } } };
    await expect(createReportIssue(client, { owner: "o", repo: "r", title: "Report", body: "Content" })).rejects.toBe(error);
  });

  it("renders GraphQL issue creation before batching without changing other operations or retry identity", async () => {
    const github = { graphql: vi.fn().mockResolvedValue({ m0: { issue: { id: "id" } }, m1: { issue: { id: "other" } } }) };
    const operations = [
      { name: "createIssue", type: "CreateIssueInput", input: { repositoryId: "repo", title: " Work\n title ", body: "<img src=x>\n\n<!-- work-id: abc -->", labelIds: ["label"] }, selection: "issue { id }" },
      { name: "updateIssue", type: "UpdateIssueInput", input: { id: "other", state: "CLOSED" }, selection: "issue { id }" },
    ];
    const copy = structuredClone(operations);
    const result = await mutateIssues(github, operations);
    const [, variables] = github.graphql.mock.calls[0];
    expect(variables.m0).toEqual({ ...operations[0].input, title: "Work title", body: "&lt;img src=x&gt;\n\n<!-- work-id: abc -->" });
    expect(variables.m1).toBe(operations[1].input);
    expect(operations).toEqual(copy);
    expect(result.results[0].operation).toBe(operations[0]);
    expect(result.ambiguous).toBe(false);
  });
});

describe("repository issue creation coverage", () => {
  it("keeps every production REST issue write behind the AST publisher", () => {
    const root = fileURLToPath(new URL("../../../", import.meta.url));
    const violations = [];
    const visit = directory => {
      for (const entry of readdirSync(directory, { withFileTypes: true })) {
        if (entry.name.startsWith(".") && ![".github"].includes(entry.name)) continue;
        if (["node_modules", "vendor", "dist", "out"].includes(entry.name)) continue;
        const path = join(directory, entry.name);
        if (entry.isDirectory()) {
          visit(path);
          continue;
        }
        if (!/\.(?:cjs|mjs|js)$/.test(entry.name) || /(?:\.test\.|^test_|_checks\.cjs$)/.test(entry.name) || entry.name === "issue_report.cjs") continue;
        const source = readFileSync(path, "utf8")
          .replace(/\/\*[\s\S]*?\*\//g, "")
          .replace(/^\s*\/\/.*$/gm, "");
        if (/\.rest\s*\.\s*issues\s*\.\s*create\s*\(|\.request\s*\(\s*["'`]POST \/repos\/[^/]+\/[^/]+\/issues["'`]/.test(source)) violations.push(path);
      }
    };
    visit(root);
    expect(violations).toEqual([]);
  }, 60000);
});
