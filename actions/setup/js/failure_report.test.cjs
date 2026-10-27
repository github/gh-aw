// @ts-check

import { describe, it, expect } from "vitest";
import { createRequire } from "module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { buildFailureReport } = require("./failure_report.cjs");
const { normalizeReport, renderReport } = require("./report_ast.cjs");
const template = readFileSync(new URL("../md/agent_failure_issue.md", import.meta.url), "utf8");

function values(overrides = {}) {
  return {
    ...Object.fromEntries(Array.from(template.matchAll(/\{(\w+)\}/g), match => [match[1], ""])),
    workflow_name: "Smoke Codex",
    workflow_id: "smoke-codex",
    failure_summary: "is missing required tools",
    run_id: "39046125927",
    run_url: "https://github.com/github/gh-aw/actions/runs/39046125927",
    workflow_source_url: "https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-codex.md",
    history_url: "https://github.com/search?q=repo%3Agithub%2Fgh-aw&type=issues",
    branch: "copilot/bump-mcp-gateway-v0435",
    pull_request_info: "**Pull Request:** [#69581](https://github.com/github/gh-aw/pull/69581)",
    ...overrides,
  };
}

describe("failure report structure", () => {
  it("preserves typed diagnostic and pull-request slots without reparsing literal text", () => {
    const diagnostic = { type: "paragraph", children: [{ type: "text", value: "## literal <!-- not metadata -->", agentTainted: true }], agentTainted: true };
    const pullRequest = { type: "paragraph", children: [{ type: "code", value: "PR #123", agentTainted: false }], agentTainted: false };
    const ast = buildFailureReport("{missing_tool_context}\n\n{pull_request_context}", "Failure", values({ missing_tool_context: [diagnostic], pull_request_info: [pullRequest] }));
    expect(ast.children).toEqual([diagnostic, pullRequest]);
    const { body } = renderReport(ast);
    expect(body).toContain("\\#\\# literal &lt;");
    expect(body).not.toContain("<!-- not metadata -->");
    expect(body).toContain("`PR #123`");
  });

  it("has a fixed overview, next step, and details order with one debugging prompt", () => {
    const ast = buildFailureReport(
      template,
      "[aw] Smoke Codex: is missing required tools",
      values({
        missing_tool_context: "> [!WARNING]\n> **Missing tools:** The GitHub MCP interface is unavailable.\n\n- **report\\_progress**: not exposed",
      })
    );
    const { title, body } = renderReport(ast);
    expect(title).toBe("[aw] Smoke Codex: is missing required tools");
    expect(
      normalizeReport(ast)
        .children.filter(node => node.type === "heading")
        .map(node => node.children)
    ).toEqual([[{ type: "text", value: "What happened", agentTainted: false }], [{ type: "text", value: "Next step", agentTainted: false }], [{ type: "text", value: "Failure details", agentTainted: false }]]);
    expect(
      normalizeReport(ast)
        .children.filter(node => node.type === "heading")
        .map(node => node.level)
    ).toEqual([3, 3, 3]);
    expect(body).not.toMatch(/^#{1,2} /m);
    expect(body).toContain("**Smoke Codex** is missing required tools.");
    expect(body).toContain("View run \\#39046125927");
    expect(body).toContain("[Workflow source]");
    expect(body).toContain("[Previous failures]");
    expect(body).toContain("**Branch:** `copilot/bump-mcp-gateway-v0435`");
    expect(body).toContain("[#69581](https://github.com/github/gh-aw/pull/69581)");
    expect(body).toContain("**report\\_progress**");
    expect(body.match(/<summary>Copy a debugging prompt<\/summary>/g)).toHaveLength(1);
    expect(body).not.toContain("Manually invoke the agent");
    expect(body).not.toContain("<summary>Runtime evidence");
    expect(body).not.toMatch(/\{\w+\}/);
  });

  it("keeps a classified cause visible while hiding secondary runtime evidence", () => {
    const { body } = renderReport(
      buildFailureReport(
        template,
        "[aw] AOAI: has no AI credits pricing",
        values({
          workflow_name: "Smoke Copilot - AOAI (Entra)",
          failure_summary: "has no AI credits pricing for model (o4-mini-aw)",
          missing_model_pricing_context: "> [!WARNING]\n> **Model has no AI credits pricing:** `o4-mini-aw` was rejected.\n\nThis is a configuration issue; retrying will not help.",
          report_incomplete_context: "> [!WARNING]\n> Task completion could not be confirmed.",
          failure_diagnostics_context: "### Failure Diagnostics\n\n**Failing step:** `Execute GitHub Copilot CLI`\n\n```text\nDriver exit code: 1\n```",
        })
      )
    );
    const evidencePosition = body.indexOf("<summary>Runtime evidence</summary>");
    expect(body.indexOf("Model has no AI credits pricing")).toBeLessThan(evidencePosition);
    expect(body.indexOf("retrying will not help")).toBeLessThan(evidencePosition);
    expect(body.indexOf("Task completion could not be confirmed")).toBeGreaterThan(evidencePosition);
    expect(body.indexOf("Driver exit code: 1")).toBeGreaterThan(evidencePosition);
    expect(body).toContain("#### Failure Diagnostics");
    expect(body).not.toContain("did not record a specific cause");
  });

  it("retains uncertainty and never promotes diagnostic headings into report sections", () => {
    const ast = buildFailureReport(
      template,
      "[aw] Smoke Gemini: failed (cause unknown)",
      values({
        workflow_name: "Smoke Gemini",
        failure_summary: "failed (cause unknown)",
        failure_diagnostics_context: "## Unexpected heading\n\nThe runtime did not record a specific cause.",
      })
    );
    const { body } = renderReport(ast);
    expect(body).toContain("**Smoke Gemini** failed (cause unknown).");
    expect(body).toContain("#### Unexpected heading");
    expect(body.match(/^### /gm)).toHaveLength(3);
  });

  it("treats hostile workflow, branch, and prompt fields as data", () => {
    const ast = buildFailureReport(
      template,
      "[aw] Smoke: failed",
      values({
        workflow_name: "Smoke**\n\n## Injected\n<img src=x>",
        workflow_id: "smoke\n```\n</details>\n# Injected",
        branch: "feature/`\n</details><img src=x>",
      })
    );
    const { body } = renderReport(ast);
    expect(body.match(/^### /gm)).toHaveLength(3);
    expect(body).toContain("&lt;img src=x&gt;");
    expect(body).toContain("``feature/` </details><img src=x>``");
    expect(body).toContain("````text\nDebug");
    expect(body).not.toMatch(/^\{|\{\w+\}/m);
    expect(body).toContain('````text\nDebug the agentic workflow "smoke\n```\n</details>\n# Injected" failure.');
    expect(body.match(/^<details>$/gm)).toHaveLength(2);
  });

  it("omits absent optional links and reports missing run metadata explicitly", () => {
    const { body } = renderReport(buildFailureReport(template, "[aw] Smoke: failed", values({ workflow_source_url: "#", history_url: "", pull_request_info: "", run_url: "" })));
    expect(body).not.toContain("[Workflow source]");
    expect(body).not.toContain("[Previous failures]");
    expect(body).not.toContain("Pull Request");
    expect(body).toContain("Run link unavailable: run URL was not recorded.");
  });
});
