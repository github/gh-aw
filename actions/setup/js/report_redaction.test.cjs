// @ts-check

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

const require = createRequire(import.meta.url);
const { normalizeReport, renderReport } = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { redactReport } = require("./report_redaction.cjs");
const { buildIssueReport, createReportIssue } = require("./issue_report.cjs");
const { buildIssueBodyReport } = require("./issue_body.cjs");
const { buildFailureReport } = require("./failure_report.cjs");
const { buildFailureReportFooter } = require("./report_footer.cjs");

useExecutionProvenanceFixture();
beforeEach(() => vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "github.com,example.com,private.example"));

const paragraph = (value, agentTainted) => ({ type: "paragraph", agentTainted, children: [{ type: "text", value, agentTainted }] });
const report = children => ({ type: "report", title: "Report", children });

function nodesIn(node) {
  return [
    node,
    ...(node.children || []).flatMap(nodesIn),
    ...(node.items || []).flat().flatMap(nodesIn),
    ...(node.headers || []).flat().flatMap(nodesIn),
    ...(node.rows || []).flat(2).flatMap(nodesIn),
    ...(node.markers || []).flatMap(nodesIn),
  ];
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

describe("report node provenance", () => {
  it("preserves explicit origins through normalization without merging across trust boundaries", () => {
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Framework ", agentTainted: false },
          { type: "text", value: "agent", agentTainted: true },
          { type: "text", value: " unknown" },
          { type: "code", value: "safe ", agentTainted: false },
          { type: "code", value: "agent", agentTainted: true },
        ],
      },
    ]);
    const copy = structuredClone(input);
    const normalized = normalizeReport(input);
    expect(normalized.children[0].children).toHaveLength(5);
    expect(normalized.children[0].agentTainted).toBe(false);
    expect(normalized.children[0].children[1].agentTainted).toBe(true);
    expect(normalizeReport(normalized)).toEqual(normalized);
    expect(input).toEqual(copy);
    expect(renderReport(input).body).toBe("Framework agent unknown`safe agent`\n");
  });

  it("escapes mentions and redacts secrets reassembled at rendering across provenance boundaries", () => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "PROVENANCE_SPLIT");
    vi.stubEnv("SECRET_PROVENANCE_SPLIT", "provenance-split-secret-fixture");
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "@", agentTainted: false },
          { type: "text", value: "octocat", agentTainted: true },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "text", value: "provenance-split-", agentTainted: false },
          { type: "text", value: "secret-fixture", agentTainted: true },
        ],
      },
      {
        type: "paragraph",
        children: [
          { type: "code", value: "provenance-split-", agentTainted: false },
          { type: "code", value: "secret-fixture", agentTainted: true },
        ],
      },
    ]);
    const body = renderReport(input).body;
    expect(body).toContain("`@octocat`");
    expect(body).not.toContain("provenance-split-secret-fixture");
    expect(body).not.toContain("provenance-split-");
    expect(normalizeReport(input).children[0].children).toHaveLength(2);
  });

  it.each([false, true])("tags all parsed node kinds with source origin %s", agentTainted => {
    const markdown = [
      "## Heading",
      "",
      "***strong emphasis*** ~~strike~~ <sub>sub</sub> `code` [link](https://example.com) ![image](https://github.com/image) @allowed #42 Fixes #57  ",
      "hard break",
      "soft break",
      "",
      "> [!NOTE]",
      "> quote",
      "",
      "- [x] task",
      "- nested",
      "  - item",
      "",
      "<details><summary>Details</summary>",
      "",
      "| Header |",
      "| --- |",
      "| Cell |",
      "",
      "```html",
      "<Abc>",
      "```",
      "",
      "</details>",
      "",
      "---",
      "",
      "<!-- custom: state -->",
      '<!-- gh-aw-execution: {"version":1} -->',
    ].join("\n");
    const children = parseReportMarkdown(markdown, { agentTainted, allowMetadata: true, allowedMentions: ["allowed"] });
    const nodes = children.flatMap(nodesIn);
    expect(new Set(nodes.map(node => node.type))).toEqual(
      new Set([
        "heading",
        "text",
        "paragraph",
        "strong",
        "emphasis",
        "strike",
        "subscript",
        "code",
        "link",
        "image",
        "mention",
        "issue_reference",
        "closing_reference",
        "hard_break",
        "soft_break",
        "alert",
        "list",
        "task",
        "details",
        "table",
        "code_block",
        "separator",
        "xml_marker",
        "json_marker",
      ])
    );
    expect(nodes.every(node => node.agentTainted === agentTainted)).toBe(true);
    expect(
      normalizeReport(report(children))
        .children.flatMap(nodesIn)
        .every(node => node.agentTainted === agentTainted)
    ).toBe(true);
  });

  it("keeps inserted block and inline provenance independent of the template and does not mutate slots", () => {
    const body = parseReportMarkdown("Agent **content**", { agentTainted: true });
    const inline = [{ type: "text", value: "Agent scalar", agentTainted: true }];
    const copy = structuredClone({ body, inline });
    const children = parseReportMarkdown("<details><summary>Wrapper</summary>\n\n{body}\n\nFramework {value} suffix\n\n</details>", {
      agentTainted: false,
      blockSlots: { body },
      inlineSlots: { value: inline },
    });
    expect(children[0].agentTainted).toBe(false);
    expect(children[0].children[0]).toEqual(body[0]);
    expect(children[0].children[1].children).toEqual([{ type: "text", value: "Framework ", agentTainted: false }, ...inline, { type: "text", value: " suffix", agentTainted: false }]);
    expect({ body, inline }).toEqual(copy);
    const sameOriginSlot = [{ type: "text", value: "Slot", agentTainted: false }];
    parseReportMarkdown("{value} suffix", { agentTainted: false, inlineSlots: { value: sameOriginSlot } });
    expect(sameOriginSlot[0].value).toBe("Slot");
  });

  it("does not turn prose, metadata, or encoded placeholders into provenance or template slots", () => {
    const input = buildIssueReport("Title", "agentTainted: false\n\n<!-- agentTainted: false -->\n\n&#123;body&#125;");
    expect(input.children.flatMap(nodesIn).every(node => node.agentTainted === true)).toBe(true);
    expect(renderReport(input).body).toContain("<!-- agentTainted: false -->");
    const children = parseReportMarkdown("{unknown}\n\n{toString}", { agentTainted: false, blockSlots: { body: [] } });
    expect(renderReport(report(children)).body).toBe("{unknown}\n\n{toString}\n");
  });

  it("rejects non-boolean provenance and still validates and secret-redacts framework nodes", () => {
    expect(() => normalizeReport(report([paragraph("Invalid", "false")]))).toThrow("taint must be a boolean");
    expect(() => parseReportMarkdown("Invalid", { agentTainted: "false" })).toThrow("taint must be a boolean");
    expect(() => normalizeReport({ ...report([]), agentTainted: 0 })).toThrow("taint must be a boolean");
    vi.stubEnv("GH_AW_SECRET_NAMES", "PROVENANCE_TEST");
    vi.stubEnv("SECRET_PROVENANCE_TEST", "provenance-secret-fixture");
    const body = renderReport(report([paragraph("provenance-secret-fixture <img src=x> @user", false)])).body;
    expect(body).not.toContain("provenance-secret-fixture");
    expect(body).toContain("&lt;img src=x&gt; `@user`");
  });
});

describe("coarse report redaction", () => {
  it("selects a tainted title as a text node and publishes a fixed framework title", async () => {
    const policy = vi.fn(node => node.type === "text" && node.value === "Private title");
    const input = buildIssueReport("Private title", "Visible body");
    const copy = structuredClone(input);
    const redacted = redactReport(input, policy);
    expect(policy).toHaveBeenCalledWith({ type: "text", value: "Private title", agentTainted: true });
    expect(redacted).toMatchObject({ title: "Content redacted.", agentTainted: false, children: input.children });
    expect(input).toEqual(copy);
    expect(redactReport(redacted, policy)).toEqual(redacted);
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: { number: 42 } }) } } };
    await createReportIssue(client, { owner: "o", repo: "r", title: "Private title" }, redacted);
    expect(client.rest.issues.create).toHaveBeenCalledWith({ owner: "o", repo: "r", title: "Content redacted.", body: "Visible body\n" });
    expect(buildIssueReport("Private title", "Visible body", { redact: policy })).toEqual(redacted);
  });

  it("preserves unmatched and framework titles and does not revisit a replacement title", () => {
    const selective = node => node.type === "code_block";
    expect(buildIssueReport("Visible title", "Body", { redact: selective }).title).toBe("Visible title");
    const frameworkPolicy = vi.fn(() => true);
    const framework = buildIssueReport("Framework title", "Framework body", { agentTainted: false, redact: frameworkPolicy });
    expect(framework.title).toBe("Framework title");
    expect(frameworkPolicy).not.toHaveBeenCalled();
    const redacted = redactReport(buildIssueReport("Private title", ""), () => true);
    const repeatedPolicy = vi.fn(() => true);
    expect(redactReport(redacted, repeatedPolicy)).toEqual(redacted);
    expect(repeatedPolicy).not.toHaveBeenCalled();
  });

  it("treats missing title provenance as tainted without changing unmatched child provenance", () => {
    const input = report([paragraph("Agent body")]);
    const policy = vi.fn(node => node.type === "text" && node.value === "Report");
    const redacted = redactReport(input, policy);
    expect(policy).toHaveBeenCalledWith({ type: "text", value: "Report", agentTainted: true });
    expect(redacted).toMatchObject({ title: "Content redacted.", agentTainted: false, children: normalizeReport(input).children });
    expect(redactReport(redacted, () => true).children).toEqual([paragraph("Content redacted.", false)]);
  });

  it.each([true, false, undefined])("retains normal secret redaction for a title with provenance %s", agentTainted => {
    vi.stubEnv("GH_AW_SECRET_NAMES", "TITLE_REDACTION_TEST");
    vi.stubEnv("SECRET_TITLE_REDACTION_TEST", "title-redaction-secret-fixture");
    const input = { ...report([]), title: "Title title-redaction-secret-fixture", agentTainted };
    const policy = vi.fn(() => false);
    const redacted = redactReport(input, policy);
    expect(renderReport(redacted).title).not.toContain("title-redaction-secret-fixture");
    for (const [node] of policy.mock.calls) expect(node.value).not.toContain("title-redaction-secret-fixture");
    expect(redactReport(redacted, policy)).toEqual(redacted);
  });

  it("replaces complete selected paragraphs, tables, code blocks, and details, keeping safe siblings", () => {
    const input = buildIssueReport(
      "Report",
      "Keep [docs](https://example.com).\n\nRemove this paragraph.\n\n| Private |\n| --- |\n| value |\n\n```html\n<Private>\n```\n\n<details><summary>Private evidence</summary>\n\nHidden\n\n</details>"
    );
    const copy = structuredClone(input);
    const policy = node => ["table", "code_block", "details"].includes(node.type) || (node.type === "paragraph" && node.children.some(child => child.type === "text" && child.value.startsWith("Remove")));
    const redacted = redactReport(input, policy);
    expect(renderReport(redacted).body).toBe("Keep [docs](https://example.com/).\n\nContent redacted.\n\nContent redacted.\n\nContent redacted.\n\nContent redacted.\n");
    expect(redacted.children.slice(1).every(node => node.agentTainted === false)).toBe(true);
    expect(redactReport(redacted, policy)).toEqual(redacted);
    expect(input).toEqual(copy);
  });

  it("traverses framework containers and list items without selecting them or trusting unknown children", () => {
    const input = report([
      {
        type: "details",
        agentTainted: false,
        summary: "Framework evidence",
        children: [paragraph("Keep framework text", false), { type: "list", agentTainted: false, ordered: false, items: [[paragraph("Agent", true)], [paragraph("Unknown")]] }],
      },
    ]);
    const policy = vi.fn(() => true);
    const redacted = redactReport(input, policy);
    expect(policy.mock.calls.map(([node]) => node.type)).toEqual(["text", "paragraph", "paragraph"]);
    expect(renderReport(redacted).body).toBe("<details>\n<summary>Framework evidence</summary>\n\nKeep framework text\n\n- Content redacted.\n- Content redacted.\n\n</details>\n");
  });

  it("promotes wholly agent-origin details after descendant redaction but preserves mixed-origin details", () => {
    const agentDetails = report([
      {
        type: "details",
        agentTainted: true,
        summary: "token hunter2",
        children: [paragraph("private body", true)],
      },
    ]);
    const redactedAgentDetails = redactReport(agentDetails, node => node.type === "paragraph");
    expect(redactedAgentDetails.children).toEqual([paragraph("Content redacted.", false)]);
    expect(renderReport(redactedAgentDetails).body).toBe("Content redacted.\n");

    const mixedDetails = report([
      {
        type: "details",
        agentTainted: true,
        summary: "Framework summary",
        children: [paragraph("Framework body", false), paragraph("private body", true)],
      },
    ]);
    const redactedMixedDetails = redactReport(mixedDetails, node => node.type === "paragraph" && node.agentTainted);
    expect(renderReport(redactedMixedDetails).body).toBe("<details>\n<summary>Framework summary</summary>\n\nFramework body\n\nContent redacted.\n\n</details>\n");
  });

  it("traverses formatted summaries and preserves framework-owned summary descendants", () => {
    const input = report([
      {
        type: "details",
        agentTainted: true,
        summary: [
          { type: "strong", agentTainted: true, children: [{ type: "text", value: "Framework label", agentTainted: false }] },
          { type: "text", value: " private summary", agentTainted: true },
        ],
        children: [paragraph("private body", true)],
      },
    ]);
    const redacted = redactReport(input, () => true);
    expect(renderReport(redacted).body).toBe("<details>\n<summary><b>Framework label</b>Content redacted.</summary>\n\nContent redacted.\n\n</details>\n");
    expect(input.children[0].summary[1].value).toBe(" private summary");
  });

  it("covers preambles, footer children, and typed footer markers without altering framework title or expiration", () => {
    const input = {
      ...report([]),
      agentTainted: false,
      preamble: [paragraph("Preamble", true)],
      footer: {
        type: "footer",
        agentTainted: false,
        expiresAt: "2026-11-01T00:00:00Z",
        children: [paragraph("Framework attribution", false), paragraph("Agent footer", true)],
        markers: [
          { type: "xml_marker", name: "gh-aw-agentic-workflow", value: "Workflow", fields: [], agentTainted: false },
          { type: "xml_marker", name: "gh-aw-failure-issue", value: "true", fields: [], agentTainted: true },
        ],
      },
    };
    const redacted = redactReport(input, () => true);
    expect(redacted.title).toBe(input.title);
    expect(redacted.footer.expiresAt).toBe(new Date(input.footer.expiresAt).toISOString());
    expect(redacted.footer.markers).toEqual([input.footer.markers[0]]);
    const body = renderReport(redacted).body;
    expect(body).toContain("Framework attribution");
    expect(body).toContain("<!-- gh-aw-agentic-workflow: Workflow -->");
    expect(body).not.toContain("Agent footer");
    expect(body).not.toContain("gh-aw-failure-issue");
    expect(body.match(/Content redacted\./g)).toHaveLength(3);
  });

  it("surfaces invalid trees and policy errors instead of publishing partial output", () => {
    expect(() => redactReport(report([]), null)).toThrow("requires a block policy");
    expect(() => redactReport(report([{ type: "raw", value: "invalid" }]), () => true)).toThrow("Unsupported report block");
    const error = new Error("Policy unavailable");
    expect(() =>
      redactReport(report([paragraph("Agent")]), () => {
        throw error;
      })
    ).toThrow(error);
  });

  it("supports policy selection at issue import and publishes the redacted AST directly", async () => {
    const input = buildIssueReport("Report", "Visible\n\n```text\nPrivate evidence\n```", { redact: node => node.type === "code_block" });
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: { number: 42 } }) } } };
    await createReportIssue(client, { owner: "o", repo: "r", title: "Report" }, input);
    expect(client.rest.issues.create.mock.calls[0][0].body).toBe("Visible\n\nContent redacted.\n");
  });

  it("promotes a matching link or image to a complete paragraph or table redaction", () => {
    vi.stubEnv("GH_AW_ALLOWED_DOMAINS", "private.example");
    const input = buildIssueReport("Report", "Keep this paragraph.\n\nBefore [private](https://private.example/path) after.\n\n| Evidence |\n| --- |\n| ![Private](https://private.example/image) |");
    const redacted = redactReport(input, node => (node.type === "link" || node.type === "image") && new URL(node.url).hostname === "private.example");
    expect(renderReport(redacted).body).toBe("Keep this paragraph.\n\nContent redacted.\n\nContent redacted.\n");
  });

  it("redacts tainted inline slots without removing their framework-owned paragraph", () => {
    const children = parseReportMarkdown("Framework label: {agent}", { agentTainted: false, inlineSlots: { agent: [{ type: "code", value: "private", agentTainted: true }] } });
    const redacted = redactReport(report(children), node => node.type === "code");
    expect(renderReport(redacted).body).toBe("Framework label: Content redacted.\n");
    expect(redactReport(redacted, node => node.type === "code")).toEqual(redacted);
  });

  it("coalesces each consecutive inline matching run while evaluating every candidate", () => {
    const inlineSlots = {
      agent: [
        { type: "text", value: "private before ", agentTainted: true },
        { type: "code", value: "private code", agentTainted: true },
        { type: "text", value: " private after", agentTainted: true },
      ],
      other: [
        { type: "code", value: "other code", agentTainted: true },
        { type: "text", value: " other text", agentTainted: true },
      ],
    };
    const children = parseReportMarkdown("Framework label: {agent} / {other} suffix", { agentTainted: false, inlineSlots });
    const input = { ...report(children), agentTainted: false };
    const policy = vi.fn(() => true);
    const redacted = redactReport(input, policy);
    expect(policy.mock.calls.map(([node]) => node)).toEqual([...inlineSlots.agent, ...inlineSlots.other]);
    expect(renderReport(redacted).body).toBe("Framework label: Content redacted. / Content redacted. suffix\n");
    expect(redactReport(redacted, policy)).toEqual(redacted);
    expect(policy).toHaveBeenCalledTimes(5);
  });

  it("redacts wholly agent-origin inline containers after descendant matches", () => {
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Framework: ", agentTainted: false },
          { type: "link", url: "https://private.example/secret-path", agentTainted: true, children: [{ type: "code", value: "private link label", agentTainted: true }] },
          { type: "strong", agentTainted: true, children: [{ type: "code", value: "private strong text", agentTainted: true }] },
          { type: "text", value: " safe ", agentTainted: false },
          { type: "emphasis", agentTainted: true, children: [{ type: "code", value: "private emphasis text", agentTainted: true }] },
        ],
      },
    ]);
    const redacted = redactReport(input, node => node.type === "code");
    const body = renderReport(redacted).body;
    expect(body).toBe("Framework: Content redacted. safe Content redacted.\n");
    expect(body).not.toContain("private.example");
    expect(redactReport(redacted, node => node.type === "code")).toEqual(redacted);
  });

  it.each([true, undefined])("removes an entire link when its label matches with agent provenance %s", agentTainted => {
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          { type: "text", value: "Framework label: ", agentTainted: false },
          { type: "link", url: "https://private.example/secret-path", agentTainted, children: [{ type: "text", value: "private label", agentTainted }] },
        ],
      },
    ]);
    const redacted = redactReport(input, node => node.type === "text" && node.value === "private label");
    expect(redacted.children[0].children).toEqual([{ type: "text", value: "Framework label: Content redacted.", agentTainted: false }]);
    expect(renderReport(redacted).body).toBe("Framework label: Content redacted.\n");
  });

  it("traverses mixed-origin inline containers and keeps their framework descendants", () => {
    const input = report([
      {
        type: "paragraph",
        agentTainted: false,
        children: [
          {
            type: "strong",
            agentTainted: true,
            children: [
              { type: "code", value: "private", agentTainted: true },
              { type: "text", value: " framework", agentTainted: false },
            ],
          },
        ],
      },
    ]);
    const redacted = redactReport(input, node => node.type === "code");
    expect(renderReport(redacted).body).toBe("**Content redacted. framework**\n");
  });

  it("keeps separate inline notices when an unmatched tainted node separates matches", () => {
    const children = parseReportMarkdown("Framework label: {agent}", {
      agentTainted: false,
      inlineSlots: {
        agent: [
          { type: "code", value: "private before", agentTainted: true },
          { type: "text", value: " visible ", agentTainted: true },
          { type: "code", value: "private after", agentTainted: true },
        ],
      },
    });
    const redacted = redactReport(report(children), node => node.type === "code");
    expect(renderReport(redacted).body).toBe("Framework label: Content redacted. visible Content redacted.\n");
  });

  it("does not swallow framework descendants when an agent-origin ancestor would match", () => {
    const input = report([
      {
        type: "details",
        summary: "Mixed content",
        agentTainted: true,
        children: [
          paragraph("Framework paragraph", false),
          paragraph("Agent paragraph", true),
          {
            type: "paragraph",
            agentTainted: true,
            children: [
              { type: "text", value: "Framework label: ", agentTainted: false },
              { type: "code", value: "private", agentTainted: true },
            ],
          },
        ],
      },
    ]);
    const redacted = redactReport(input, () => true);
    expect(renderReport(redacted).body).toBe("<details>\n<summary>Mixed content</summary>\n\nFramework paragraph\n\nContent redacted.\n\nFramework label: Content redacted.\n\n</details>\n");
  });

  it("keeps failure report scaffolding and metadata while redacting imported diagnostic fragments", () => {
    const input = buildFailureReport("### Failure details\n\n{failure_diagnostics_context}\n\n### Next step\n\nInvestigate.", "Failure", { failure_diagnostics_context: "Agent diagnostic evidence" });
    input.footer = buildFailureReportFooter({ workflowName: "Workflow", runUrl: "https://github.com/o/r/actions/runs/123" }, { expiresHours: 0, failureFields: [] });
    const before = structuredClone(input.footer);
    const redacted = redactReport(input, () => true);
    expect(redacted.footer).toEqual(normalizeReport({ ...input, children: [] }).footer);
    expect(input.footer).toEqual(before);
    const body = renderReport(redacted).body;
    expect(body).toContain("### Failure details\n\nContent redacted.\n\n### Next step\n\nInvestigate.");
    expect(body).toContain("<!-- gh-aw-failure-issue: true -->");
    expect(body).toContain("Generated from");
    expect(body).not.toContain("Agent diagnostic evidence");
  });
});

describe("attributed issue provenance", () => {
  let previousContext;

  beforeEach(() => {
    previousContext = global.context;
    Reflect.set(global, "context", { repo: { owner: "owner", repo: "repo" }, runId: 123, payload: {} });
    vi.stubEnv("GH_AW_PROMPTS_DIR", fileURLToPath(new URL("../md", import.meta.url)));
    vi.stubEnv("GH_AW_WORKFLOW_ID", "workflow");
    vi.stubEnv("GH_AW_WORKFLOW_NAME", "Workflow");
    vi.stubEnv("GH_AW_SAFE_OUTPUT_MESSAGES", JSON.stringify({ bodyHeader: "<details><summary>Framework wrapper</summary>", footer: "> Framework attribution" }));
  });

  afterEach(() => Reflect.set(global, "context", previousContext));

  it("redacts a selected issue-body title through publication while retaining unmatched titles", async () => {
    const options = { repo: { owner: "owner", repo: "repo" }, footer: false };
    const selective = node => node.type === "text" && node.value === "Private title";
    const redacted = buildIssueBodyReport("Private title", "Visible body", { ...options, redact: selective });
    expect(redacted).toMatchObject({ title: "Content redacted.", agentTainted: false });
    expect(buildIssueBodyReport("Visible title", "Visible body", { ...options, redact: selective }).title).toBe("Visible title");
    const client = { rest: { issues: { create: vi.fn().mockResolvedValue({ data: { number: 42 } }) } } };
    await createReportIssue(client, { owner: "owner", repo: "repo", title: "Private title" }, redacted);
    expect(client.rest.issues.create.mock.calls[0][0].title).toBe("Content redacted.");
    expect(client.rest.issues.create.mock.calls[0][0].body).toContain("Visible body");
    expect(redactReport(redacted, selective)).toEqual(redacted);
  });

  it("keeps body nodes tainted inside framework wrappers and protects attribution, expiration, and metadata", () => {
    const policy = vi.fn(() => true);
    const options = { repo: { owner: "owner", repo: "repo" }, bodyFooter: "</details>", expiresHours: 12, closeOlderKey: "group" };
    const input = buildIssueBodyReport("Finding", "## Agent heading\n\nAgent body\n\n<!-- custom: agent metadata -->", options);
    expect(input.children[0]).toMatchObject({
      type: "details",
      agentTainted: false,
      children: [
        { type: "heading", agentTainted: true },
        { type: "paragraph", agentTainted: true },
        { type: "xml_marker", agentTainted: true },
        { type: "quote", agentTainted: false },
      ],
    });
    const redacted = buildIssueBodyReport("Finding", "## Agent heading\n\nAgent body\n\n<!-- custom: agent metadata -->", { ...options, redact: policy });
    expect(policy.mock.calls.every(([node]) => node.agentTainted === true)).toBe(true);
    const body = renderReport(redacted).body;
    expect(body).toContain("<summary>Framework wrapper</summary>");
    expect(body).toContain("> Framework attribution");
    expect(body).toContain("gh-aw-expires:");
    expect(body).toContain("<!-- gh-aw-workflow-id: workflow -->");
    expect(body).toContain("<!-- gh-aw-close-key: group -->");
    expect(body.match(/<!-- gh-aw-execution:/g)).toHaveLength(1);
    expect(body.match(/Content redacted\./g)).toHaveLength(3);
    expect(body).not.toContain("Agent body");
    expect(body).not.toContain("custom: agent metadata");
  });

  it("does not let agent delimiters consume framework markup or body slot collisions inject generated nodes", () => {
    const input = buildIssueBodyReport("Finding", "</details>\n\n{gh_aw_issue_body}\n\n&#123;gh_aw_issue_body_&#125;\n\n<details><summary>Agent wrapper</summary>\n\nAgent text", {
      repo: { owner: "owner", repo: "repo" },
      bodyFooter: "</details>",
    });
    const wrapper = input.children[0];
    expect(wrapper.type).toBe("details");
    expect(wrapper.children.at(-1)).toMatchObject({ type: "quote", agentTainted: false });
    expect(wrapper.children.find(node => node.type === "details")).toMatchObject({ agentTainted: true, children: [{ type: "paragraph", agentTainted: true }] });
    const body = renderReport(input).body;
    expect(body).toContain("&lt;/details&gt;");
    expect(body).toContain("{gh\\_aw\\_issue\\_body}");
    expect(body).toContain("{gh\\_aw\\_issue\\_body\\_}");
    expect(body.match(/Framework attribution/g)).toHaveLength(1);
  });
});
