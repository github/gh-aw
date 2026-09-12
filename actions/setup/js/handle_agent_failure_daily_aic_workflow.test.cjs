import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import path from "path";
import { fileURLToPath } from "url";

let buildDailyAICExceededContext;
let buildDailyAICGuardrailErrorContext;
const __dirname = path.dirname(fileURLToPath(import.meta.url));

describe("handle_agent_failure daily workflow AI Credits context", () => {
  beforeEach(async () => {
    vi.resetModules();
    process.env.GH_AW_PROMPTS_DIR = path.join(__dirname, "../md");
    const mod = await import("./handle_agent_failure.cjs");
    const exports = mod.default || mod;
    buildDailyAICExceededContext = exports.buildDailyAICExceededContext;
    buildDailyAICGuardrailErrorContext = exports.buildDailyAICGuardrailErrorContext;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    delete process.env.GH_AW_PROMPTS_DIR;
  });

  it("renders the daily workflow AI Credits guardrail context when exceeded", () => {
    const rendered = buildDailyAICExceededContext(true, "17.329230000000003", "10");
    expect(rendered).toContain("Daily Workflow AIC Guardrail Exceeded");
    expect(rendered).toContain("**24h AIC usage:** `18` AI Credits");
    expect(rendered).toContain("**Configured threshold:** `10` AI Credits");
    expect(rendered).not.toContain("Activation Issue:");
    // Progressive disclosure sections
    expect(rendered).toContain("How to raise the daily limit");
    expect(rendered).toContain("max-daily-ai-credits: 20K");
    expect(rendered).toContain("max-daily-ai-credits");
    expect(rendered).toContain("What is the daily AI Credits guardrail");
    expect(rendered).toContain("How to disable this guardrail");
    expect(rendered).toContain("Consult the billing dashboards for accurate usage and charges.");
  });

  it("returns empty string when the guardrail did not trigger", () => {
    expect(buildDailyAICExceededContext(false, "2500", "2000", "")).toBe("");
  });

  it("renders an actionable accounting failure with the propagated reason", () => {
    const rendered = buildDailyAICGuardrailErrorContext(
      true,
      "transient_error",
      "Daily workflow AI Credits are unknown: Missing accounting for executed detection component in run 34616576735 (attempt 1, job 103321731068, conclusion success): detection/token_usage.jsonl is empty; detection_usage.jsonl is missing"
    );

    expect(rendered).toContain("Daily Workflow AI Credits Could Not Be Verified");
    expect(rendered).toContain("`transient_error`");
    expect(rendered).toContain("run 34616576735");
    expect(rendered).toContain("detection/token_usage.jsonl is empty");
    expect(rendered).toContain("fails closed");
    expect(rendered).toContain("missing, empty, or unreadable");
  });

  it("returns empty string when accounting was verified", () => {
    expect(buildDailyAICGuardrailErrorContext(false, "under_budget", "")).toBe("");
  });
});
