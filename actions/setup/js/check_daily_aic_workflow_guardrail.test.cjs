import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let exports;
let scanCacheDirectory;
const runMain = () => exports.main({ cachePath: path.join(scanCacheDirectory, "scan.jsonl") });

function httpError(status, message) {
  const error = new Error(message);
  error.status = status;
  error.response = { status };
  return error;
}

describe("check_daily_aic_workflow_guardrail", () => {
  beforeEach(async () => {
    scanCacheDirectory = fs.mkdtempSync(path.join(os.tmpdir(), "aic-main-test-"));
    vi.resetModules();
    process.env.GITHUB_EVENT_NAME = "";
    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = "";
    process.env.GH_AW_HAS_SLASH_COMMAND = "false";
    process.env.GH_AW_HAS_LABEL_COMMAND = "false";
    const mod = await import("./check_daily_aic_workflow_guardrail.cjs");
    exports = mod.default || mod;
  });

  afterEach(() => {
    fs.rmSync(scanCacheDirectory, { recursive: true, force: true });
    delete process.env.GITHUB_EVENT_NAME;
    delete process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT;
    delete process.env.GH_AW_HAS_SLASH_COMMAND;
    delete process.env.GH_AW_HAS_LABEL_COMMAND;
  });

  it("skips workflow_call, repository_dispatch, and workflow_dispatch (manual and aw_context)", () => {
    process.env.GITHUB_EVENT_NAME = "workflow_call";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GITHUB_EVENT_NAME = "repository_dispatch";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = "";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"schedule"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"workflow_dispatch"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("skips for label command triggers in aw_context", () => {
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"pull_request","trigger_label":"smoke"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"issues","trigger_label":"ci-doctor"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("skips for slash command triggers in aw_context", () => {
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"issue_comment","trigger_label":""}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"pull_request_review_comment"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = '{"event_type":"discussion_comment"}';
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("skips for workflow_dispatch with malformed aw_context", () => {
    process.env.GITHUB_EVENT_NAME = "workflow_dispatch";
    process.env.GH_AW_WORKFLOW_DISPATCH_AW_CONTEXT = "not-json";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("skips for non-centralized slash command events only when slash command is enabled", () => {
    process.env.GITHUB_EVENT_NAME = "issue_comment";
    process.env.GH_AW_HAS_SLASH_COMMAND = "false";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(false);

    process.env.GH_AW_HAS_SLASH_COMMAND = "true";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GITHUB_EVENT_NAME = "pull_request_review_comment";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("skips for non-centralized label command events only when label command is enabled", () => {
    process.env.GITHUB_EVENT_NAME = "issues";
    process.env.GH_AW_HAS_LABEL_COMMAND = "false";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(false);

    process.env.GH_AW_HAS_LABEL_COMMAND = "true";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);

    process.env.GITHUB_EVENT_NAME = "pull_request";
    expect(exports.shouldSkipDailyAICGuardrail()).toBe(true);
  });

  it("matches usage artifacts only", () => {
    expect(exports.matchesGuardrailArtifactName("usage")).toBe(true);
    expect(exports.matchesGuardrailArtifactName("prefix-usage")).toBe(true);
    expect(exports.matchesGuardrailArtifactName("agent")).toBe(false);
    expect(exports.matchesGuardrailArtifactName("detection")).toBe(false);
    expect(exports.matchesGuardrailArtifactName("activation")).toBe(false);
  });

  it("sums AI Credits across multiple JSONL files and usage attributes", () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "daily-guardrail-token-usage-"));
    const nestedDir = path.join(tmpDir, "nested");
    fs.mkdirSync(nestedDir);
    const filePathA = path.join(tmpDir, "token-usage-a.jsonl");
    const filePathB = path.join(nestedDir, "token-usage-b.jsonl");
    fs.writeFileSync(filePathA, [JSON.stringify({ model: "gpt-5.5", aic: 1.25 }), JSON.stringify({ model: "gpt-5.5", aic: 0.75 })].join("\n"), "utf8");
    fs.writeFileSync(
      filePathB,
      [
        JSON.stringify({ usage: { aic: 1.5 } }),
        JSON.stringify({ usage: { aic: 0.5 } }),
        JSON.stringify({ aic: 9, usage: { aic: 0.25 } }),
        JSON.stringify({ ai_credits: 8, usage: { ai_credits: 0.1 } }),
        JSON.stringify({ aiCredits: 7, usage: { aiCredits: 0.15 } }),
        JSON.stringify({ aiCredits: 0.2, usage: { aiCredits: "" } }),
        JSON.stringify({ aic: 0.3, usage: { aic: "" } }),
      ].join("\n"),
      "utf8"
    );

    expect(exports.sumAICFromUsageJSONLFiles(exports.findJSONLFiles(tmpDir))).toBe(5);
  });

  it("computes aggregate AIC statistics for prior runs", () => {
    expect(exports.calculateDailyAICStats([{ aic: 100 }, { aic: 200 }, { aic: 300 }])).toEqual({
      count: 3,
      total: 600,
      average: 200,
      min: 100,
      max: 300,
      stddev: 100,
    });
  });

  it("caps inspection when GitHub API rate limit headroom is low", () => {
    expect(exports.computeMaxInspectableRuns(110)).toBe(0);
    expect(exports.computeMaxInspectableRuns(120)).toBeGreaterThan(0);
  });

  it("formats structured daily AI Credits log messages", () => {
    const message = exports.formatDailyGuardrailLogMessage("Resolved current workflow AI Credits guardrail context", {
      currentRunId: 123,
      workflowId: 456,
      currentAIC: 789,
    });
    const prefix = "[daily-workflow-aic] Resolved current workflow AI Credits guardrail context: ";
    expect(message).toContain(prefix);
    expect(JSON.parse(message.slice(prefix.length))).toEqual({
      currentRunId: 123,
      workflowId: 456,
      currentAIC: 789,
    });
    expect(exports.formatDailyGuardrailLogMessage("Completed AI Credits inspection window")).toBe("[daily-workflow-aic] Completed AI Credits inspection window");
  });

  it("renders a daily AI Credits summary with zero counts when no prior runs are in the 24h window", () => {
    const markdown = exports.renderDailyAICSummary(
      "Impact Efficiency Report",
      "mnkiefer",
      5000,
      [],
      {
        remaining: 13194,
        limit: 15000,
        used: 1806,
        reset: "2026-06-10T07:07:04.000Z",
      },
      {
        candidateRunsCount: 0,
        inspectedRunsCount: 0,
        truncatedByRateLimit: false,
      }
    );

    expect(markdown).toContain("| 24h total AIC | 0 |");
    expect(markdown).toContain("| Runs counted | 0 |");
    expect(markdown).toContain("| Avg AIC / run | — |");
    expect(markdown).toContain("| Std dev AIC | — |");
    expect(markdown).toContain("| Min / Max AIC | — / — |");
    expect(markdown).toContain("| _none_ | — | — | 0 |");
    expect(markdown).not.toContain("| 24h total AIC |  |");
    expect(markdown).not.toContain("| Avg AIC / run |  |");
    expect(markdown).not.toContain("| Min / Max AIC |  /  |");
  });

  it("renders a daily AI Credits details summary with stats and prior runs", () => {
    const markdown = exports.renderDailyAICSummary(
      "Nightly triage",
      "copilot-swe-agent[bot]",
      1_500_000,
      [
        {
          id: 11,
          html_url: "https://example.test/runs/11",
          created_at: "2026-05-31T10:00:00Z",
          conclusion: "success",
          aic: 1_200_000,
        },
        {
          id: 10,
          html_url: "https://example.test/runs/10",
          created_at: "2026-05-31T09:00:00Z",
          conclusion: "failure",
          aic: 300_000,
        },
      ],
      {
        remaining: 4321,
        limit: 5000,
        used: 679,
        reset: "2026-05-31T12:00:00.000Z",
      },
      {
        candidateRunsCount: 5,
        inspectedRunsCount: 2,
        truncatedByRateLimit: true,
      }
    );

    expect(markdown).toContain("| 24h total AIC | 1.5M |");
    expect(markdown).toContain("| Threshold | 1.5M |");
    expect(markdown).toContain("| Avg AIC / run | 750K |");
    expect(markdown).toContain("| Std dev AIC | 636.4K |");
    expect(markdown).toContain("| [#11](https://example.test/runs/11) | 2026-05-31T10:00:00Z | success | 1.2M |");
    expect(markdown).toContain("Stopped early to preserve GitHub API rate limit headroom");
    expect(markdown).not.toContain("Guardrail issue:");
  });

  it("main() fails the step when GitHub API calls leave accounting unknown", async () => {
    // Simulate a scenario where the GitHub API throws during workflow run lookup.
    // core.setFailed blocks activation without misreporting a budget exceedance.
    const coreOutputs = {};
    const coreWarnings = [];
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: () => {},
      warning: msg => coreWarnings.push(msg),
      setFailed: msg => coreWarnings.push(msg),
    };

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => {
            throw new Error("API rate limit exceeded");
          },
        },
        actions: {
          getWorkflowRun: async () => {
            throw new Error("Network error");
          },
          listWorkflowRuns: async () => {
            throw new Error("Unexpected error");
          },
        },
      },
    };

    const mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      runId: 42,
    };

    // Inject globals so the module can use them
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "1000000";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";

    try {
      await expect(runMain()).resolves.toBeUndefined();
      // The default "false" output must be set
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("transient_error");
      // Unknown accounting is an activation failure, not a successful skip.
      expect(coreWarnings.some(w => /AI Credits are unknown/i.test(w))).toBe(true);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
    }
  });

  it("main() counts business requests instead of unrelated rate-limit snapshots", async () => {
    // The unrelated quota endpoint must not supply the business-request budget.
    const coreInfos = [];
    const coreOutputs = {};
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: msg => coreInfos.push(msg),
      warning: () => {},
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    let rateLimitCallCount = 0;
    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => {
            rateLimitCallCount += 1;
            const remaining = rateLimitCallCount === 1 ? 4995 : 5000;
            return {
              data: {
                resources: {
                  core: { limit: 5000, remaining, used: 5000 - remaining, reset: Math.floor(Date.now() / 1000) + 3600 },
                },
              },
              headers: {},
            };
          },
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 777,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => ({
            data: { workflow_runs: [] },
            headers: {},
          }),
        },
      },
    };

    const mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      runId: 99,
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "50000";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";

    try {
      await expect(runMain()).resolves.toBeUndefined();

      const consumptionLog = coreInfos.find(msg => msg.includes("Daily AIC business API requests"));
      expect(consumptionLog).toBeDefined();
      const detailsPrefix = "[daily-workflow-aic] Daily AIC business API requests: ";
      const details = JSON.parse(consumptionLog.slice(detailsPrefix.length));
      expect(details).toMatchObject({
        requests: 2,
        cacheHits: 0,
      });
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("under_budget");

      // Only the authoritative run and history requests are needed.
      expect(rateLimitCallCount).toBe(0);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
    }
  });

  it("main() stops paginating as soon as a run older than the 24h cutoff is found", async () => {
    const nowIso = new Date().toISOString();
    const staleIso = new Date(Date.now() - 25 * 60 * 60 * 1000).toISOString();

    let listCallCount = 0;
    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: { workflow_id: 999, actor: { login: "bot" }, triggering_actor: { login: "bot" } },
            headers: {},
          }),
          listWorkflowRuns: async ({ page }) => {
            listCallCount += 1;
            // Page 1: one recent run followed by a stale run.
            // If early-exit works, page 2 should never be requested.
            if (page === 1) {
              return {
                data: {
                  workflow_runs: [
                    { id: 10, html_url: "https://example.test/runs/10", created_at: nowIso, updated_at: nowIso, run_attempt: 1, status: "completed", conclusion: "success" },
                    // A stale run: encountering this should immediately stop pagination.
                    { id: 9, html_url: "https://example.test/runs/9", created_at: staleIso, conclusion: "success" },
                  ],
                },
                headers: {},
              };
            }
            // Should never reach page 2.
            return { data: { workflow_runs: [] }, headers: {} };
          },
        },
      },
    };

    const getRunAICSpy = vi.spyOn(exports, "getRunAIC").mockResolvedValue(50);

    const coreOutputs = {};
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: () => {},
      warning: () => {},
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    const mockContext = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "1000";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();

      // Only page 1 should have been fetched; the stale run should have
      // terminated pagination before page 2 was requested.
      expect(listCallCount).toBe(1);

      // Only the recent run (id: 10) should have been inspected.
      expect(getRunAICSpy).toHaveBeenCalledTimes(1);
      expect(getRunAICSpy.mock.calls[0][1]).toBe(10);

      // Guardrail not exceeded (50 < 1000).
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("under_budget");
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GITHUB_EVENT_NAME;
      getRunAICSpy.mockRestore();
    }
  });

  it("main() marks the guardrail exceeded when daily AI Credits equal the threshold", async () => {
    const getRunAICSpy = vi.spyOn(exports, "getRunAIC").mockResolvedValue(100);

    const coreOutputs = {};
    const setFailed = vi.fn();
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      setFailed,
      info: () => {},
      warning: () => {},
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    const nowIso = new Date().toISOString();
    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 777,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => ({
            data: {
              workflow_runs: [
                {
                  id: 41,
                  run_attempt: 1,
                  updated_at: nowIso,
                  status: "completed",
                  html_url: "https://example.test/runs/41",
                  created_at: nowIso,
                  conclusion: "success",
                },
              ],
            },
            headers: {},
          }),
        },
      },
    };

    const mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      runId: 42,
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "100";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GH_AW_WORKFLOW_NAME = "Daily Guardrail Test";
    process.env.GH_AW_WORKFLOW_ID = "daily-guardrail-test";
    process.env.GITHUB_TRIGGERING_ACTOR = "octocat";
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("true");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("exceeded");
      expect(coreOutputs["daily_ai_credits_total_effective_tokens"]).toBe("100");
      expect(coreOutputs["daily_ai_credits_threshold"]).toBe("100");
      expect(setFailed).not.toHaveBeenCalled();
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GITHUB_TRIGGERING_ACTOR;
      delete process.env.GITHUB_EVENT_NAME;
      getRunAICSpy.mockRestore();
    }
  });

  it("main() stops on business-response quota exhaustion without trusting rate_limit", async () => {
    // A business-request rejection must stop before inspecting the second run.
    const quotaError = httpError(403, "API quota exhausted");
    quotaError.response.headers = { "x-ratelimit-remaining": "0" };
    const getRunAICSpy = vi.spyOn(exports, "getRunAIC").mockRejectedValue(quotaError);

    let rateLimitGetCallCount = 0;
    const nowIso = new Date().toISOString();

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => {
            rateLimitGetCallCount++;
            // This endpoint must never be consulted by the scan.
            const remaining = rateLimitGetCallCount === 1 ? 5000 : 50;
            return {
              data: {
                resources: {
                  core: { limit: 5000, remaining, used: 5000 - remaining, reset: Math.floor(Date.now() / 1000) + 3600 },
                },
              },
              headers: {},
            };
          },
        },
        actions: {
          getWorkflowRun: async () => ({
            data: { workflow_id: 444, actor: { login: "bot" }, triggering_actor: { login: "bot" } },
            headers: {},
          }),
          listWorkflowRuns: async () => ({
            data: {
              workflow_runs: Array.from({ length: 15 }, (_, i) => ({
                id: i + 100,
                run_attempt: 1,
                updated_at: nowIso,
                status: "completed",
                html_url: `https://example.test/runs/${i + 100}`,
                created_at: nowIso,
                conclusion: "success",
              })),
            },
            headers: {},
          }),
        },
      },
    };

    const coreInfos = [];
    const coreOutputs = {};
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: msg => coreInfos.push(msg),
      warning: () => {},
      setFailed: msg => coreInfos.push(msg),
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    const mockContext = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "1000000";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();

      expect(getRunAICSpy).toHaveBeenCalledTimes(1);

      // The incomplete window is reported as a failure.
      const stopLog = coreInfos.find(msg => msg.includes("AI Credits are unknown"));
      expect(stopLog).toBeDefined();

      // Unknown is neither an exceedance nor a verified under-budget result.
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("transient_error");
      expect(rateLimitGetCallCount).toBe(0);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GITHUB_EVENT_NAME;
      getRunAICSpy.mockRestore();
    }
  });

  it("falls back to repository run history when workflow-specific run lookup 404s under required workflows", async () => {
    const getRunAICSpy = vi.spyOn(exports, "getRunAIC").mockResolvedValue(25);
    const workflowName = "PR Quality Review";
    const nowIso = new Date().toISOString();
    let listWorkflowRunsCalls = 0;
    let listWorkflowRunsForRepoCalls = 0;

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 324645976,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => {
            listWorkflowRunsCalls += 1;
            throw httpError(404, "Not Found");
          },
          listWorkflowRunsForRepo: async ({ page }) => {
            listWorkflowRunsForRepoCalls += 1;
            if (page === 1) {
              return {
                data: {
                  workflow_runs: Array.from({ length: 100 }, (_, index) => ({
                    id: 1000 + index,
                    name: "Unrelated Workflow",
                    html_url: `https://example.test/runs/${1000 + index}`,
                    created_at: nowIso,
                    conclusion: "success",
                  })),
                },
                headers: {},
              };
            }
            return {
              data: {
                workflow_runs: [
                  {
                    id: 41,
                    run_attempt: 1,
                    updated_at: nowIso,
                    status: "completed",
                    name: workflowName,
                    html_url: "https://example.test/runs/41",
                    created_at: nowIso,
                    conclusion: "success",
                  },
                ],
              },
              headers: {},
            };
          },
        },
      },
    };

    const coreInfos = [];
    const coreOutputs = {};
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: msg => coreInfos.push(msg),
      warning: () => {},
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "100";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GH_AW_WORKFLOW_NAME = workflowName;
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();

      expect(listWorkflowRunsCalls).toBe(1);
      expect(listWorkflowRunsForRepoCalls).toBe(2);
      expect(getRunAICSpy).toHaveBeenCalledTimes(1);
      expect(getRunAICSpy.mock.calls[0][1]).toBe(41);
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("under_budget");
      expect(coreInfos.some(msg => msg.includes("falling back to repository run listing by workflow name"))).toBe(true);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GITHUB_EVENT_NAME;
      getRunAICSpy.mockRestore();
    }
  });

  it("marks permanent 404 guardrail failures as structural errors distinct from transient failures", async () => {
    const coreOutputs = {};
    const coreWarnings = [];
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: () => {},
      warning: msg => coreWarnings.push(msg),
      setFailed: msg => coreWarnings.push(msg),
    };

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 324645976,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => {
            throw httpError(404, "Not Found");
          },
          listWorkflowRunsForRepo: async () => {
            throw httpError(404, "Not Found");
          },
        },
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "10";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GH_AW_WORKFLOW_NAME = "PR Quality Review";
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("structural_error");
      expect(coreWarnings.some(w => /AI Credits are unknown/i.test(w))).toBe(true);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GITHUB_EVENT_NAME;
    }
  });

  it("fallback stops immediately when a page returns zero unfiltered repository runs (sourceRunCount === 0 fast-exit)", async () => {
    const workflowName = "My Workflow";
    let listWorkflowRunsForRepoCalls = 0;
    const nowIso = new Date().toISOString();

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 324645976,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => {
            throw httpError(404, "Not Found");
          },
          listWorkflowRunsForRepo: async ({ page }) => {
            listWorkflowRunsForRepoCalls += 1;
            if (page === 1) {
              // 100 unrelated runs — causes the loop to continue to page 2
              return {
                data: {
                  workflow_runs: Array.from({ length: 100 }, (_, i) => ({
                    id: 100 + i,
                    name: "Other Workflow",
                    html_url: `https://example.test/runs/${100 + i}`,
                    created_at: nowIso,
                    conclusion: "success",
                  })),
                },
                headers: {},
              };
            }
            // page 2: empty — signals exhaustion of repository run history
            return { data: { workflow_runs: [] }, headers: {} };
          },
        },
      },
    };

    const coreOutputs = {};
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: () => {},
      warning: () => {},
      summary: {
        addDetails: function () {
          return this;
        },
        write: async () => {},
      },
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "100";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    process.env.GH_AW_WORKFLOW_NAME = workflowName;
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();
      // page 1 has 100 unrelated runs → loop must continue; page 2 is empty →
      // sourceRunCount === 0 fast-exit fires, page 3 must never be queried.
      expect(listWorkflowRunsForRepoCalls).toBe(2);
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("under_budget");
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GITHUB_EVENT_NAME;
    }
  });

  it("treats 404 as structural error without calling listWorkflowRunsForRepo when GH_AW_WORKFLOW_NAME is absent", async () => {
    let listWorkflowRunsForRepoCalls = 0;

    const mockGithub = {
      rest: {
        rateLimit: {
          get: async () => ({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4990, used: 10, reset: Math.floor(Date.now() / 1000) + 3600 },
              },
            },
            headers: {},
          }),
        },
        actions: {
          getWorkflowRun: async () => ({
            data: {
              workflow_id: 324645976,
              actor: { login: "octocat" },
              triggering_actor: { login: "octocat" },
            },
            headers: {},
          }),
          listWorkflowRuns: async () => {
            throw httpError(404, "Not Found");
          },
          listWorkflowRunsForRepo: async () => {
            listWorkflowRunsForRepoCalls += 1;
            return { data: { workflow_runs: [] }, headers: {} };
          },
        },
      },
    };

    const coreOutputs = {};
    const coreWarnings = [];
    const mockCore = {
      setOutput: (key, value) => {
        coreOutputs[key] = value;
      },
      info: () => {},
      warning: msg => coreWarnings.push(msg),
      setFailed: msg => coreWarnings.push(msg),
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, runId: 42 };

    process.env.GH_AW_MAX_DAILY_AI_CREDITS = "10";
    process.env.GH_AW_GITHUB_TOKEN = "fake-token";
    // GH_AW_WORKFLOW_NAME intentionally not set — workflowFilterName will be "" so
    // the !workflowName branch re-throws the 404 without attempting the fallback.
    process.env.GITHUB_EVENT_NAME = "pull_request";

    try {
      await expect(runMain()).resolves.toBeUndefined();
      expect(coreOutputs["daily_ai_credits_exceeded"]).toBe("false");
      expect(coreOutputs["daily_ai_credits_guardrail_status"]).toBe("structural_error");
      expect(listWorkflowRunsForRepoCalls).toBe(0);
    } finally {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_MAX_DAILY_AI_CREDITS;
      delete process.env.GH_AW_GITHUB_TOKEN;
      delete process.env.GITHUB_EVENT_NAME;
    }
  });

  describe("loadAICUsageCache", () => {
    let tmpDir;
    let cacheFile;

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aic-cache-test-"));
      cacheFile = path.join(tmpDir, "agentic-workflow-usage-cache.jsonl");
      global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
    });

    afterEach(() => {
      fs.rmSync(tmpDir, { recursive: true, force: true });
      delete global.core;
    });

    it("returns an empty map when the cache file does not exist", () => {
      const cache = exports.loadAICUsageCache(path.join(tmpDir, "nonexistent.jsonl"));
      expect(cache).toBeInstanceOf(Map);
      expect(cache.size).toBe(0);
    });

    it("returns an empty map when the cache file is empty", () => {
      fs.writeFileSync(cacheFile, "", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache).toBeInstanceOf(Map);
      expect(cache.size).toBe(0);
    });

    it("parses valid JSONL entries into a run_id → aic map", () => {
      fs.writeFileSync(cacheFile, [JSON.stringify({ run_id: 101, aic: 5.5 }), JSON.stringify({ run_id: 202, aic: 12.0 })].join("\n") + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.size).toBe(2);
      expect(cache.get(101)).toBe(5.5);
      expect(cache.get(202)).toBe(12.0);
    });

    it("skips malformed lines without throwing", () => {
      fs.writeFileSync(cacheFile, ["not-json-at-all", JSON.stringify({ run_id: 303, aic: 7.0 }), "{bad json", "", JSON.stringify({ run_id: 404, aic: 3.0 })].join("\n") + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.size).toBe(2);
      expect(cache.get(303)).toBe(7.0);
      expect(cache.get(404)).toBe(3.0);
    });

    it("uses the last entry when run_id appears more than once (last-writer-wins)", () => {
      fs.writeFileSync(cacheFile, [JSON.stringify({ run_id: 505, aic: 1.0 }), JSON.stringify({ run_id: 505, aic: 9.9 })].join("\n") + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.size).toBe(1);
      expect(cache.get(505)).toBe(9.9);
    });

    it("loads entries with aic = 0 and skips entries with negative or non-finite aic values", () => {
      fs.writeFileSync(cacheFile, [JSON.stringify({ run_id: 601, aic: 0 }), JSON.stringify({ run_id: 602, aic: -1.5 }), JSON.stringify({ run_id: 603, aic: null }), JSON.stringify({ run_id: 604, aic: 4.2 })].join("\n") + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      // aic = 0 is loaded (agent was blocked, legitimately used 0 credits)
      expect(cache.has(601)).toBe(true);
      expect(cache.get(601)).toBe(0);
      // negative and non-finite aic values are skipped
      expect(cache.has(602)).toBe(false);
      expect(cache.has(603)).toBe(false);
      expect(cache.get(604)).toBe(4.2);
    });

    it("loads entries that have a recent timestamp (within 48 h)", () => {
      const recentTimestamp = new Date(Date.now() - 60 * 60 * 1000).toISOString(); // 1 hour ago
      fs.writeFileSync(cacheFile, JSON.stringify({ run_id: 701, aic: 8.0, timestamp: recentTimestamp }) + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.has(701)).toBe(true);
      expect(cache.get(701)).toBe(8.0);
    });

    it("skips entries whose timestamp is older than 48 h", () => {
      const staleTimestamp = new Date(Date.now() - 49 * 60 * 60 * 1000).toISOString(); // 49 hours ago
      const recentTimestamp = new Date(Date.now() - 30 * 60 * 1000).toISOString(); // 30 minutes ago
      fs.writeFileSync(cacheFile, [JSON.stringify({ run_id: 801, aic: 3.0, timestamp: staleTimestamp }), JSON.stringify({ run_id: 802, aic: 5.0, timestamp: recentTimestamp })].join("\n") + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.has(801)).toBe(false);
      expect(cache.has(802)).toBe(true);
      expect(cache.get(802)).toBe(5.0);
    });

    it("keeps entries without a timestamp (backward compatibility)", () => {
      fs.writeFileSync(cacheFile, JSON.stringify({ run_id: 901, aic: 2.5 }) + "\n", "utf8");
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.has(901)).toBe(true);
      expect(cache.get(901)).toBe(2.5);
    });
  });

  describe("appendZeroAICEntriesToCache", () => {
    let tmpDir;
    let cacheFile;

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aic-zero-cache-test-"));
      cacheFile = path.join(tmpDir, "agentic-workflow-usage-cache.jsonl");
      global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
    });

    afterEach(() => {
      fs.rmSync(tmpDir, { recursive: true, force: true });
      delete global.core;
    });

    it("does nothing when runIds is an empty array", () => {
      exports.appendZeroAICEntriesToCache([], cacheFile);
      expect(fs.existsSync(cacheFile)).toBe(false);
    });

    it("does nothing when runIds is null or not an array", () => {
      exports.appendZeroAICEntriesToCache(null, cacheFile);
      exports.appendZeroAICEntriesToCache(undefined, cacheFile);
      exports.appendZeroAICEntriesToCache(42, cacheFile);
      expect(fs.existsSync(cacheFile)).toBe(false);
    });

    it("creates the cache file when it does not exist", () => {
      exports.appendZeroAICEntriesToCache([1001, 1002], cacheFile);
      expect(fs.existsSync(cacheFile)).toBe(true);
      const lines = fs.readFileSync(cacheFile, "utf8").trim().split("\n").filter(Boolean);
      expect(lines).toHaveLength(2);
      expect(JSON.parse(lines[0])).toMatchObject({ run_id: 1001, aic: 0 });
      expect(JSON.parse(lines[1])).toMatchObject({ run_id: 1002, aic: 0 });
    });

    it("appends to an existing cache file without overwriting", () => {
      fs.writeFileSync(cacheFile, JSON.stringify({ run_id: 900, aic: 5.0 }) + "\n", "utf8");
      exports.appendZeroAICEntriesToCache([2001], cacheFile);
      const lines = fs.readFileSync(cacheFile, "utf8").trim().split("\n").filter(Boolean);
      expect(lines).toHaveLength(2);
      expect(JSON.parse(lines[0])).toMatchObject({ run_id: 900, aic: 5.0 });
      expect(JSON.parse(lines[1])).toMatchObject({ run_id: 2001, aic: 0 });
    });

    it("creates parent directories if they do not exist", () => {
      const deepPath = path.join(tmpDir, "nested", "deep", "cache.jsonl");
      exports.appendZeroAICEntriesToCache([3001], deepPath);
      expect(fs.existsSync(deepPath)).toBe(true);
      const lines = fs.readFileSync(deepPath, "utf8").trim().split("\n").filter(Boolean);
      expect(lines).toHaveLength(1);
      expect(JSON.parse(lines[0])).toMatchObject({ run_id: 3001, aic: 0 });
    });

    it("written entries round-trip through loadAICUsageCache with aic=0", () => {
      exports.appendZeroAICEntriesToCache([4001, 4002], cacheFile);
      const cache = exports.loadAICUsageCache(cacheFile);
      expect(cache.has(4001)).toBe(true);
      expect(cache.get(4001)).toBe(0);
      expect(cache.has(4002)).toBe(true);
      expect(cache.get(4002)).toBe(0);
    });

    it("each written entry includes a valid ISO timestamp", () => {
      const before = Date.now() - 1000; // 1 s tolerance for slow systems
      exports.appendZeroAICEntriesToCache([5001], cacheFile);
      const after = Date.now() + 1000;
      const lines = fs.readFileSync(cacheFile, "utf8").trim().split("\n").filter(Boolean);
      const entry = JSON.parse(lines[0]);
      expect(typeof entry.timestamp).toBe("string");
      const ts = Date.parse(entry.timestamp);
      expect(ts).toBeGreaterThanOrEqual(before);
      expect(ts).toBeLessThanOrEqual(after);
    });

    it("handles write errors gracefully without throwing", () => {
      // Pass a path inside a file (not a directory) to force a write error.
      fs.writeFileSync(cacheFile, "", "utf8");
      // Use cacheFile as if it were a directory — this will cause mkdirSync/appendFileSync to fail.
      const badPath = path.join(cacheFile, "subfile.jsonl");
      expect(() => exports.appendZeroAICEntriesToCache([6001], badPath)).not.toThrow();
    });
  });
});
