// @ts-check

import { describe, it, expect, afterEach, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { buildFailureReportFooter } = require("./report_footer.cjs");
const { normalizeReport, renderReport } = require("./report_ast.cjs");
const { parseReportMarkdown } = require("./report_markdown.cjs");
const { getFooterAgentFailureIssueTemplate, getFooterAgentFailureIssueMessage } = require("./messages_footer.cjs");
const { extractExpirationDate } = require("./ephemerals.cjs");
const { parseHTMLCommentMetadata, isReusableFailureIssue } = require("./handle_agent_failure.cjs");

const ctx = {
  workflowName: "Smoke Copilot",
  runUrl: "https://github.com/github/gh-aw/actions/runs/123",
  historyUrl: "https://github.com/search?q=smoke",
  aiCredits: "1.25",
};
const fields = [
  { key: "workflow_id", value: "smoke-copilot" },
  { key: "branch", value: "main" },
  { key: "failure_categories", value: "missing_tool|report_incomplete" },
];
const now = new Date("2099-01-01T12:00:00.000Z");

afterEach(() => {
  vi.unstubAllEnvs();
});

function document(footer) {
  return { type: /** @type {const} */ "report", title: "[aw] Smoke Copilot: failed", children: parseReportMarkdown("## What happened\n\nFailure"), footer };
}

describe("structured report footer", () => {
  it("renders attribution, model/cost context, expiration, and machine markers in one document", () => {
    vi.stubEnv("GH_AW_WORKFLOW_ID", "smoke-copilot");
    vi.stubEnv("GITHUB_RUN_ID", "123");
    vi.stubEnv("GH_AW_ENGINE_ID", "copilot");
    vi.stubEnv("GH_AW_ENGINE_MODEL", "claude-sonnet-4.6");
    vi.stubEnv("GH_AW_AMBIENT_CONTEXT", "900");
    const ast = document(buildFailureReportFooter(ctx, { expiresHours: 12, failureFields: fields, now }));
    const { body } = renderReport(ast);
    expect(normalizeReport(normalizeReport(ast))).toEqual(normalizeReport(ast));
    expect(renderReport(ast).body).toBe(body);
    expect(body).toContain("> Generated from [Smoke Copilot](https://github.com/github/gh-aw/actions/runs/123) · copilot · sonnet46 · \\~2 AIC · ⊞ 900");
    expect(body).toContain("> - [x] expires <!-- gh-aw-expires: 2099-01-02T00:00:00.000Z -->");
    expect(extractExpirationDate(body)?.toISOString()).toBe("2099-01-02T00:00:00.000Z");
    expect(parseHTMLCommentMetadata(body, "gh-aw-agentic-workflow")).toMatchObject({ "gh-aw-agentic-workflow": "Smoke Copilot", workflow_id: "smoke-copilot", id: "123", engine: "copilot" });
    expect(parseHTMLCommentMetadata(body, "gh-aw-failure-issue")).toMatchObject({ workflow_id: "smoke-copilot", branch: "main", failure_categories: "missing_tool|report_incomplete" });
    expect(isReusableFailureIssue(body, { workflowId: "smoke-copilot", failureCategories: ["missing_tool", "report_incomplete"] })).toBe(true);
    expect(body.indexOf("Generated from")).toBeGreaterThan(body.indexOf("What happened"));
    expect(body.indexOf("gh-aw-failure-issue")).toBeGreaterThan(body.indexOf("gh-aw-expires"));
  });

  it("preserves custom footer templates and both history-link field spellings", () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_MESSAGES", JSON.stringify({ agentFailureIssue: "> Report by **{workflow_name}**{history_link}{historyLink} · {ai_credits_formatted} {ai_credits_unit}" }));
    const { body } = renderReport(document(buildFailureReportFooter(ctx, { expiresHours: 0, failureFields: fields })));
    expect(body).toContain("> Report by **Smoke Copilot** · [◷](https://github.com/search?q=smoke) · [◷](https://github.com/search?q=smoke) · \\~2 AIC");
    expect(body).not.toContain("gh-aw-expires");
    expect(extractExpirationDate(body)).toBeNull();
  });

  it("preserves unknown custom footer fields as literal text alongside known fields and metadata", () => {
    vi.stubEnv("GH_AW_WORKFLOW_ID", "smoke-copilot");
    vi.stubEnv("GH_AW_SAFE_OUTPUT_MESSAGES", JSON.stringify({ agentFailureIssue: "> {workflow_name}: {triggering_number} {unknownField} {__proto__} {constructor}{history_link}\n\n{unknown_block}" }));
    const footer = buildFailureReportFooter(ctx, { expiresHours: 0, failureFields: fields });
    const { body } = renderReport(document(footer));
    expect(body).toContain("> Smoke Copilot: {triggering\\_number} {unknownField} {\\_\\_proto\\_\\_} {constructor} · [◷](https://github.com/search?q=smoke)");
    expect(body).toContain("{unknown\\_block}");
    expect(footer.children).toEqual([
      {
        type: "quote",
        agentTainted: false,
        children: [
          {
            type: "paragraph",
            agentTainted: false,
            children: [
              { type: "text", value: "Smoke Copilot: {triggering_number} {unknownField} {__proto__} {constructor}", agentTainted: false },
              { type: "text", value: " · ", agentTainted: false },
              { type: "link", url: ctx.historyUrl, children: [{ type: "text", value: "◷", agentTainted: false }], agentTainted: false },
            ],
          },
        ],
      },
      { type: "paragraph", children: [{ type: "text", value: "{unknown_block}", agentTainted: false }], agentTainted: false },
    ]);
    expect(parseHTMLCommentMetadata(body, "gh-aw-agentic-workflow")?.workflow_id).toBe("smoke-copilot");
    expect(parseHTMLCommentMetadata(body, "gh-aw-failure-issue")?.failure_categories).toBe("missing_tool|report_incomplete");
    expect(getFooterAgentFailureIssueMessage(ctx)).toContain("{triggering\\_number} {unknownField} {\\_\\_proto\\_\\_} {constructor}");
  });

  it("does not reparse placeholder-like attribution values into fields or Markdown", () => {
    vi.stubEnv("GH_AW_SAFE_OUTPUT_MESSAGES", JSON.stringify({ agentFailureIssue: "> Name: {workflow_name} {triggering_number}" }));
    const workflowName = "{history_link} **injected** [bad](https://evil.example) <!-- injected -->";
    const footer = buildFailureReportFooter({ ...ctx, workflowName }, { expiresHours: 0, failureFields: fields });
    expect(footer.children).toEqual([{ type: "quote", children: [{ type: "paragraph", children: [{ type: "text", value: `Name: ${workflowName} {triggering_number}`, agentTainted: false }], agentTainted: false }], agentTainted: false }]);
    const { body } = renderReport(document(footer));
    expect(body).not.toContain("<!-- injected -->");
    expect(body.split("\n\n<!-- gh-aw-agentic-workflow:")[0]).not.toContain("[bad](https://evil.example)");
    expect(body).not.toContain("[◷]");
  });

  it("keeps internal templates strict about missing fields even when a custom footer is configured", () => {
    expect(() => parseReportMarkdown("> {triggering_number}", { context: getFooterAgentFailureIssueTemplate(ctx).context })).toThrow("Missing report template field");
    vi.stubEnv("GH_AW_SAFE_OUTPUT_MESSAGES", JSON.stringify({ agentFailureIssue: "> {triggering_number}" }));
    expect(() => buildFailureReportFooter(ctx, { expiresHours: 0, failureFields: fields })).not.toThrow();
    expect(() => parseReportMarkdown("> {triggering_number}", { context: {} })).toThrow("Missing report template field");
  });

  it("escapes hostile attribution and marker values without accepting spoofed metadata fields", () => {
    vi.stubEnv("GH_AW_WORKFLOW_ID", "smoke-copilot");
    const hostile = "Smoke](https://evil.example)\n<!-- injected -->, workflow_id: forged";
    const ast = document(
      buildFailureReportFooter(
        { ...ctx, workflowName: hostile },
        {
          expiresHours: 0,
          failureFields: fields.map(field => (field.key === "branch" ? { ...field, value: "feature/--> <!-- injected -->, failure_cause: forged" } : field)),
        }
      )
    );
    const { body } = renderReport(ast);
    expect(body).not.toContain("<!-- injected -->");
    const attribution = body.split("\n\n<!-- gh-aw-agentic-workflow:")[0];
    expect(attribution).not.toMatch(/(?<!\\)\]\(https:\/\/evil\.example\)/);
    expect(parseHTMLCommentMetadata(body, "gh-aw-agentic-workflow")?.workflow_id).toBe("smoke-copilot");
    expect(parseHTMLCommentMetadata(body, "gh-aw-failure-issue")?.failure_cause).toBeUndefined();
    expect(body).toContain("--%3E");
    expect(body).toContain("%2C failure_cause: forged");
    expect(body.match(/<!-- gh-aw-agentic-workflow:/g)).toHaveLength(1);
    expect(body.match(/<!-- gh-aw-failure-issue:/g)).toHaveLength(1);
  });

  it("renders detection markers as typed preamble nodes, never trusts diagnostic HTML comments", () => {
    const report = document(buildFailureReportFooter(ctx, { expiresHours: 0, failureFields: fields }));
    const caution = "> [!CAUTION]\n> Review required\n> <!-- gh-aw-threat-detected -->";
    report.preamble = parseReportMarkdown(caution, { allowFlagMarkers: true });
    const { body } = renderReport(report);
    expect(body.startsWith("> [!CAUTION]\n")).toBe(true);
    expect(body).toContain("> <!-- gh-aw-threat-detected -->");
    expect(body.indexOf("gh-aw-threat-detected")).toBeLessThan(body.indexOf("## What happened"));
    expect(renderReport({ ...report, preamble: parseReportMarkdown(caution) }).body).not.toContain("<!-- gh-aw-threat-detected -->");
  });

  it("rejects invalid expiration, duplicate fields, and unknown markers", () => {
    expect(() => buildFailureReportFooter(ctx, { expiresHours: -1, failureFields: fields })).toThrow("nonnegative");
    const footer = buildFailureReportFooter(ctx, { expiresHours: 0, failureFields: fields });
    expect(() => renderReport(document({ ...footer, expiresAt: "not a date" }))).toThrow("expiration date");
    expect(() => renderReport(document({ ...footer, markers: [...footer.markers, ...footer.markers] }))).toThrow("Duplicate report metadata marker");
    expect(() => renderReport(document({ ...footer, markers: [{ name: "unrecognized", value: "", fields: [] }] }))).toThrow("Unsupported report metadata marker");
    expect(() => renderReport(document({ ...footer, markers: [{ type: "xml_marker", name: "gh-aw-failure-issue", value: "true", fields: [fields[0], fields[0]] }] }))).toThrow("duplicate report metadata field");
  });
});
