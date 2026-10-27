import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import path from "path";
import { fileURLToPath } from "url";
import { renderReport } from "./report_ast.cjs";

let buildAICreditsRateLimitErrorContext;
const __dirname = path.dirname(fileURLToPath(import.meta.url));

describe("handle_agent_failure AI Credits rate-limit context", () => {
  beforeEach(async () => {
    vi.resetModules();
    process.env.GH_AW_PROMPTS_DIR = path.join(__dirname, "../md");
    const mod = await import("./handle_agent_failure.cjs");
    const exports = mod.default || mod;
    buildAICreditsRateLimitErrorContext = (...args) => {
      const children = exports.buildAICreditsRateLimitErrorContext(...args);
      return children.length ? renderReport({ type: "report", title: "Workflow failure", children }).body : "";
    };
  });

  afterEach(() => {
    vi.restoreAllMocks();
    delete process.env.GH_AW_PROMPTS_DIR;
  });

  it("shows inline usage and overage without table, no run URL, with suggested limit snippet", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "17.329230000000003", "10.1", "https://github.com/octo/repo/actions/runs/123", true);

    expect(rendered).toContain("AI Credits Budget Exceeded");
    // inline metrics
    expect(rendered).toContain("Used `17.3` of `10.1` max (over by `7.23`)");
    // no table rows
    expect(rendered).not.toContain("| AI credits used |");
    expect(rendered).not.toContain("| Guardrail limit");
    expect(rendered).not.toContain("| Over the limit by |");
    // run URL not repeated
    expect(rendered).not.toContain("https://github.com/octo/repo/actions/runs/123");
    // suggested limit snippet (2x max = 20.2, ceiled to 21)
    expect(rendered).toContain("max-ai-credits: 21");
    // progressive disclosure
    expect(rendered).toContain("<details>");
    expect(rendered).toContain("<summary>Increase the limit</summary>");
    expect(rendered).toContain("<summary>Tips for reducing AI credit usage</summary>");
    expect(rendered).toContain("https://github.github.com/gh-aw/reference/cost-management/");
    expect(rendered).not.toContain("Consult the billing dashboards for accurate usage and charges.");
  });

  it("shows throughput rate-limit message when under budget (isBudgetExceeded=false)", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "236.079", "1000", "https://github.com/octo/repo/actions/runs/123", false);

    expect(rendered).toContain("AI Credits Rate Limit");
    expect(rendered).not.toContain("AI Credits Budget Exceeded");
    // inline metrics show usage without an overage
    expect(rendered).toContain("Used `236.1` of `1K` max");
    expect(rendered).not.toContain("over by");
    // no "Increase the limit" section
    expect(rendered).not.toContain("<summary>Increase the limit</summary>");
    // tips section is still present
    expect(rendered).toContain("<summary>Tips for reducing rate limit issues</summary>");
    expect(rendered).toContain("https://github.github.com/gh-aw/reference/cost-management/");
  });

  it("defaults to throughput template when isBudgetExceeded is omitted", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "236.079", "1000", "");

    expect(rendered).toContain("AI Credits Rate Limit");
    expect(rendered).not.toContain("AI Credits Budget Exceeded");
  });

  it("shows throughput message without metrics when no credit data is available", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "", "", "", false);

    expect(rendered).toContain("AI Credits Rate Limit");
    expect(rendered).not.toContain("Used");
  });

  it("returns empty string when the AI Credits rate-limit did not trigger", () => {
    expect(buildAICreditsRateLimitErrorContext(false, "17.3", "10", "")).toBe("");
  });
});
