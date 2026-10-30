// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import path from "path";
import { fileURLToPath } from "url";
import { renderReport } from "./report_ast.cjs";

let buildAICreditsRateLimitErrorContext;
const __dirname = path.dirname(fileURLToPath(import.meta.url));

describe("handle_agent_failure Max AI Credits exceeded context", () => {
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

  it("shows budget exhaustion message with inline usage, limit, and overage — no table, no run URL", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "105000", "100000", "https://github.com/octo/repo/actions/runs/456", true);

    expect(rendered).toContain("AI Credits Budget Exceeded");
    expect(rendered).toContain("hit the configured `max-ai-credits` guardrail");
    // inline metrics — no table
    expect(rendered).toContain("Used `105K` of `100K` max (over by `5K`)");
    expect(rendered).not.toContain("| AI credits used |");
    expect(rendered).not.toContain("| Guardrail limit");
    expect(rendered).not.toContain("| Over the limit by |");
    // run URL not repeated
    expect(rendered).not.toContain("https://github.com/octo/repo/actions/runs/456");
    // suggested limit snippet
    expect(rendered).toContain("max-ai-credits: 200000");
    // progressive disclosure
    expect(rendered).toContain("<details>");
    expect(rendered).toContain("<summary>Increase the limit</summary>");
    expect(rendered).toContain("<summary>Tips for reducing AI credit usage</summary>");
    expect(rendered).toContain("https://github.github.com/gh-aw/reference/cost-management/");
  });

  it("shows message without metrics when no credit data is available, still shows snippet with default limit", () => {
    const rendered = buildAICreditsRateLimitErrorContext(true, "", "", "", true);

    expect(rendered).toContain("AI Credits Budget Exceeded");
    expect(rendered).not.toContain("Used `");
    expect(rendered).not.toContain("| AI credits used |");
    expect(rendered).not.toContain("| Guardrail limit");
    expect(rendered).not.toContain("| Run |");
    expect(rendered).toContain("max-ai-credits: 2000");
  });

  it("does not show overage when usage does not exceed limit", () => {
    // isBudgetExceeded=true because the maxAICreditsExceeded signal was set (the credit amounts
    // in the log may be sampled before the final over-budget request, so usage < max is possible
    // even when the budget was truly exceeded).
    const rendered = buildAICreditsRateLimitErrorContext(true, "50000", "100000", "", true);

    expect(rendered).toContain("AI Credits Budget Exceeded");
    expect(rendered).toContain("Used `50K` of `100K` max");
    expect(rendered).not.toContain("over by");
    expect(rendered).not.toContain("| AI credits used |");
    expect(rendered).not.toContain("| Over the limit by |");
    // suggested limit is 2x max
    expect(rendered).toContain("max-ai-credits: 200000");
  });

  it("returns empty string when max_ai_credits_exceeded is false", () => {
    expect(buildAICreditsRateLimitErrorContext(false, "105000", "100000", "https://github.com/octo/repo/actions/runs/456", true)).toBe("");
  });
});
