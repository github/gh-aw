import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "path";
import { fileURLToPath } from "url";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

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
      "Daily workflow AI Credits are unknown: Missing accounting for executed detection component in run 34616576735 (attempt 1, job 103321731068, conclusion success): detection/token_usage.jsonl is missing; detection_usage.jsonl is missing"
    );

    expect(rendered).toContain("Daily Workflow AI Credits Could Not Be Verified");
    expect(rendered).toContain("`transient_error`");
    expect(rendered).toContain("run 34616576735");
    expect(rendered).toContain("detection/token_usage.jsonl is missing");
    expect(rendered).toContain("fails closed");
    expect(rendered).toContain("missing, empty, or unreadable");
    expect(rendered).toContain("rolling 24-hour window");
  });

  it("directs structural failures to an access/configuration fix, not the rolling-window advice", () => {
    const rendered = buildDailyAICGuardrailErrorContext(true, "structural_error", "Daily workflow AI Credits are unknown: Request failed with status code 401");

    expect(rendered).toContain("`structural_error`");
    expect(rendered).toContain("access or configuration problem");
    expect(rendered).not.toContain("rolling 24-hour window");
  });

  it("directs generic transient API failures to retry guidance, not the rolling-window advice", () => {
    const rendered = buildDailyAICGuardrailErrorContext(true, "transient_error", "Daily workflow AI Credits are unknown: Network error while listing workflow runs");

    expect(rendered).toContain("`transient_error`");
    expect(rendered).toContain("retry automatically");
    expect(rendered).not.toContain("rolling 24-hour window");
  });

  it("returns empty string when accounting was verified", () => {
    expect(buildDailyAICGuardrailErrorContext(false, "under_budget", "")).toBe("");
  });

  it("propagates a reachable missing-accounting error end-to-end from coverage detection through the template", async () => {
    const { getRunAIC } = require("./check_daily_aic_workflow_guardrail.cjs");
    const { createAPIBudget } = require("./daily_aic_api_budget.cjs");
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-e2e-"));
    global.core = { info: vi.fn(), warning: vi.fn() };
    try {
      const jobs = [
        { id: 1, name: "agent", status: "completed", conclusion: "success", run_attempt: 1, started_at: "2025-01-01T12:00:00Z", completed_at: "2025-01-01T12:00:00Z" },
        { id: 103321731068, name: "detection", status: "completed", conclusion: "success", run_attempt: 1, started_at: "2025-01-01T12:00:00Z", completed_at: "2025-01-01T12:00:00Z" },
      ];
      const files = { "agent/token_usage.jsonl": '{"aic":2}' };
      const list = vi.fn(async () => ({ status: 200, headers: {}, data: { jobs } }));
      const client = {
        listArtifacts: vi.fn(async () => ({
          artifacts: [{ id: 10, name: "usage", createdAt: new Date("2025-01-01T12:30:00Z") }],
        })),
        downloadArtifact: vi.fn(async (_id, options) => {
          for (const [name, value] of Object.entries(files)) {
            const file = path.join(options.path, ...name.split("/"));
            fs.mkdirSync(path.dirname(file), { recursive: true });
            fs.writeFileSync(file, value);
          }
          return { downloadPath: options.path };
        }),
      };

      let thrown;
      try {
        await getRunAIC(
          client,
          1,
          "synthetic",
          "example",
          "project",
          { id: 34616576735, run_attempt: 1, run_started_at: "2025-01-01T12:00:00Z" },
          { github: { rest: { actions: { listJobsForWorkflowRun: list } } }, budget: createAPIBudget() }
        );
      } catch (error) {
        thrown = error;
      }
      expect(thrown).toBeDefined();
      expect(thrown.message).toContain("Missing accounting for executed detection component in run 34616576735 (attempt 1, job 103321731068, conclusion success)");
      expect(thrown.message).toContain("detection/token_usage.jsonl is missing");
      expect(thrown.message).toContain("detection_usage.jsonl is missing");

      const message = `Daily workflow AI Credits are unknown: ${thrown.message}`;
      const rendered = buildDailyAICGuardrailErrorContext(true, "transient_error", message);
      expect(rendered).toContain("Daily Workflow AI Credits Could Not Be Verified");
      expect(rendered).toContain("run 34616576735");
      expect(rendered).toContain("rolling 24-hour window");
      expect(rendered).toContain("detection/token_usage.jsonl is missing");
    } finally {
      delete global.core;
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
