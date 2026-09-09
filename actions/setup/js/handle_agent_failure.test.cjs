// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import { syncRuntimePromptTemplates } from "./test_prompt_templates.js";

const require = createRequire(import.meta.url);
const { runtimePromptsDir } = syncRuntimePromptTemplates(import.meta.url);

describe("handle_agent_failure", () => {
  let main;
  let buildCodePushFailureContext;
  let buildPushRepoMemoryFailureContext;
  let buildReportIncompleteContext;
  let buildFailureIssueTitle;
  let buildModelPricingFrontmatterSnippet;
  let fetchModelPricingFromModelsDev;
  let buildMissingModelPricingContext;
  let buildSecretVerificationContext;
  let buildDockerSbxSecretsContext;
  let buildAssignmentErrorsContext;
  let buildAssignCopilotFailureContext;
  let setFailureIssueOutputs;
  let getActionFailureIssueExpiresHours;
  const ENGINE_RATE_LIMIT_TEMPLATE = "> [!WARNING]\n> **Engine Rate Limited (HTTP 429)**\n> OTLP telemetry\n> {engine_label}\n";
  const ENGINE_MAX_RUNS_EXCEEDED_TEMPLATE = "> [!WARNING]\n> **Engine Max Runs Exceeded**\n> max-runs guardrail\n> {engine_label}\n";
  const ENGINE_MAX_CACHE_MISSES_EXCEEDED_TEMPLATE = "> [!WARNING]\n> **Engine Cache Miss Limit Exceeded**\n> cache misses guardrail\n> {engine_label}\n";

  beforeEach(() => {
    // Provide minimal GitHub Actions globals expected by require-time code
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };
    global.github = {};
    global.context = { repo: { owner: "owner", repo: "repo" } };

    // Reset module registry so each test gets a fresh require
    vi.resetModules();
    ({
      main,
      buildCodePushFailureContext,
      buildPushRepoMemoryFailureContext,
      buildReportIncompleteContext,
      buildFailureIssueTitle,
      buildModelPricingFrontmatterSnippet,
      fetchModelPricingFromModelsDev,
      buildMissingModelPricingContext,
      buildSecretVerificationContext,
      buildDockerSbxSecretsContext,
      buildAssignmentErrorsContext,
      buildAssignCopilotFailureContext,
      setFailureIssueOutputs,
      getActionFailureIssueExpiresHours,
    } = require("./handle_agent_failure.cjs"));
  });

  afterEach(() => {
    delete global.core;
    delete global.github;
    delete global.context;
    delete process.env.GITHUB_SHA;
    delete process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS;
    delete process.env.GH_AW_GROUP_REPORTS;
  });

  describe("getActionFailureIssueExpiresHours", () => {
    it("returns default when env var is missing", () => {
      expect(getActionFailureIssueExpiresHours()).toBe(168);
    });

    it("returns configured value when env var is a positive integer", () => {
      process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS = "48";
      expect(getActionFailureIssueExpiresHours()).toBe(48);
    });

    it("returns 0 (disabled) when the compiler explicitly opts out of expiration", () => {
      process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS = "0";
      expect(getActionFailureIssueExpiresHours()).toBe(0);
    });

    it("returns default for invalid values", () => {
      process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS = "invalid";
      expect(getActionFailureIssueExpiresHours()).toBe(168);
    });

    it("returns default for malformed values with numeric prefixes", () => {
      process.env.GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS = "0invalid";
      expect(getActionFailureIssueExpiresHours()).toBe(168);
    });
  });

  describe("setFailureIssueOutputs", () => {
    it("publishes the created failure issue as step outputs", () => {
      setFailureIssueOutputs({ number: 99, html_url: "https://github.com/owner/repo/issues/99" });

      expect(global.core.setOutput).toHaveBeenCalledWith("failure_issue_number", "99");
      expect(global.core.setOutput).toHaveBeenCalledWith("failure_issue_url", "https://github.com/owner/repo/issues/99");
    });
  });

  describe("buildFailureIssueTitle", () => {
    const baseOptions = {
      workflowName: "Test Workflow",
      isTimedOut: false,
      hasMissingSafeOutputs: false,
      hasReportIncomplete: false,
      hasMissingTool: false,
      hasMissingData: false,
      hasCacheMissMisconfiguration: false,
      hasToolDenialsExceeded: false,
      hasAppTokenMintingFailed: false,
      hasLockdownCheckFailed: false,
      hasStaleLockFileFailed: false,
      hasDailyAICExceeded: false,
      aiCreditsRateLimitError: false,
      hasEngineRateLimit429: false,
      maxAICreditsExceeded: false,
      hasAssignmentErrors: false,
      http400ResponseError: false,
      unknownModelAICredits: false,
      missingModelPricingError: false,
      missingModelPricingModelName: "",
      shellExpansionGuardRejected: false,
    };

    const cases = [
      { flag: "hasDailyAICExceeded", expected: "[aw] Test Workflow exceeded daily AI credits budget" },
      { flag: "maxAICreditsExceeded", expected: "[aw] Test Workflow exceeded max AI credits" },
      { flag: "aiCreditsRateLimitError", expected: "[aw] Test Workflow hit AI credits rate limit" },
      { flag: "hasEngineRateLimit429", expected: "[aw] Test Workflow hit engine rate limit (HTTP 429)" },
      { flag: "http400ResponseError", expected: "[aw] Test Workflow hit HTTP 400 bad request" },
      { flag: "unknownModelAICredits", expected: "[aw] Test Workflow has unknown model pricing" },
      { flag: "hasAppTokenMintingFailed", expected: "[aw] Test Workflow failed to mint GitHub App token" },
      { flag: "hasLockdownCheckFailed", expected: "[aw] Test Workflow failed lockdown check" },
      { flag: "hasStaleLockFileFailed", expected: "[aw] Test Workflow has stale lock file" },
      { flag: "shellExpansionGuardRejected", expected: "[aw] Test Workflow hit shell expansion guard rejection" },
      { flag: "isTimedOut", expected: "[aw] Test Workflow timed out" },
      { flag: "hasToolDenialsExceeded", expected: "[aw] Test Workflow exceeded tool denial limit" },
      { flag: "hasCacheMissMisconfiguration", expected: "[aw] Test Workflow has cache-memory miss misconfiguration" },
      { flag: "hasReportIncomplete", expected: "[aw] Test Workflow reported incomplete result" },
      { flag: "hasMissingSafeOutputs", expected: "[aw] Test Workflow produced no safe outputs" },
      { flag: "hasMissingTool", expected: "[aw] Test Workflow is missing required tool" },
      { flag: "hasMissingData", expected: "[aw] Test Workflow is missing required data" },
      { flag: "hasAssignmentErrors", expected: "[aw] Test Workflow failed to assign agent" },
    ];

    it.each(cases)("returns expected title for isolated $flag", ({ flag, expected }) => {
      expect(buildFailureIssueTitle({ ...baseOptions, [flag]: true })).toBe(expected);
    });

    it("falls back to generic failed title when no specific condition matches", () => {
      expect(buildFailureIssueTitle(baseOptions)).toBe("[aw] Test Workflow failed");
    });

    it("prefers unknownModelAICredits over isTimedOut when both are true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, unknownModelAICredits: true, isTimedOut: true })).toBe("[aw] Test Workflow has unknown model pricing");
    });

    it("prefers shellExpansionGuardRejected over isTimedOut when both are true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, shellExpansionGuardRejected: true, isTimedOut: true })).toBe("[aw] Test Workflow hit shell expansion guard rejection");
    });

    it("returns missing model pricing title with model name when missingModelPricingError is true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, missingModelPricingError: true, missingModelPricingModelName: "claude-opus-5" })).toBe("[aw] Test Workflow has no AI credits pricing for model (claude-opus-5)");
    });

    it("returns missing model pricing title without model name when model name is empty", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, missingModelPricingError: true, missingModelPricingModelName: "" })).toBe("[aw] Test Workflow has no AI credits pricing for model");
    });

    it("prefers missingModelPricingError over unknownModelAICredits when both are true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, missingModelPricingError: true, missingModelPricingModelName: "claude-opus-5", unknownModelAICredits: true })).toBe(
        "[aw] Test Workflow has no AI credits pricing for model (claude-opus-5)"
      );
    });

    it("prefers missingModelPricingError over http400ResponseError when both are true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, missingModelPricingError: true, missingModelPricingModelName: "claude-opus-5", http400ResponseError: true })).toBe(
        "[aw] Test Workflow has no AI credits pricing for model (claude-opus-5)"
      );
    });

    it("aiCreditsRateLimitError takes precedence over hasEngineRateLimit429 when both are true", () => {
      expect(buildFailureIssueTitle({ ...baseOptions, aiCreditsRateLimitError: true, hasEngineRateLimit429: true })).toBe("[aw] Test Workflow hit AI credits rate limit");
    });
  });

  describe("detection caution placement in main()", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-handle-agent-failure-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });

      // Minimal templates used by main()
      fs.writeFileSync(path.join(promptsDir, "agent_failure_comment.md"), "COMMENT TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "ISSUE TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_issue.md"), "Daily cap rollup issue body cap={cap} window={window_hours}");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_comment.md"), "Failure suppressed workflow={workflow_name} run={run_url} categories={summary} cap={cap} window={window_hours}h");
      fs.writeFileSync(path.join(promptsDir, "optimize_token_consumption_context.md"), "OPTIMIZE CONTEXT guardrail={guardrail_name} run={run_url}");
      fs.writeFileSync(
        path.join(promptsDir, "threat_detection_caution.md"),
        "> [!CAUTION]\n> agentic threat detected\n> Threat detection flagged this output in warn mode. Manual review is REQUIRED before any follow-up automation.\n> {threat_detected_marker}\n>\n> <details>\n> <summary>Details</summary>\n>\n> {reason_text}\n>\n> Review the [workflow run logs]({run_url}) for details.\n> </details>"
      );
      fs.writeFileSync(
        path.join(promptsDir, "threat_detection_engine_error.md"),
        "> [!WARNING]\n> **Threat Detection Engine Failure** — The analysis engine could not complete. This is a tooling failure, not a security finding.\n> {threat_detected_marker}\n>\n> <details>\n> <summary>What happened</summary>\n>\n> {reason_text}\n>\n> Review the [workflow run logs]({run_url}) for details.\n> </details>"
      );

      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123456";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      process.env.GH_AW_DETECTION_CONCLUSION = "warning";
      process.env.GH_AW_DETECTION_REASON = "threat_detected";
      process.env.GITHUB_HEAD_REF = "feature/detection-caution";
      process.env.GITHUB_WORKSPACE = tmpDir;
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_RUN_URL;
      delete process.env.GH_AW_AGENT_CONCLUSION;
      delete process.env.GH_AW_DETECTION_CONCLUSION;
      delete process.env.GH_AW_DETECTION_REASON;
      delete process.env.GITHUB_HEAD_REF;
      delete process.env.GITHUB_WORKSPACE;

      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("prepends caution callout to existing-issue comment body and includes it only once", async () => {
      /** @type {string} */
      let capturedCommentBody = "";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      body:
                        "> footer\n> - [x] expires <!-- gh-aw-expires: 2099-01-01T00:00:00.000Z --> on Jan 1, 2099, 12:00 AM UTC\n\n" +
                        "<!-- gh-aw-agentic-workflow: Test Workflow, workflow_id: test-workflow, run: https://github.com/owner/repo/actions/runs/123456 -->\n" +
                        "<!-- gh-aw-failure-issue: true, workflow_id: test-workflow, branch: feature/detection-caution, failure_categories: agent_failure -->",
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            createComment: vi.fn(async ({ body }) => {
              capturedCommentBody = body;
              return { data: { id: 1001 } };
            }),
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(capturedCommentBody).toBeTruthy();
      expect(capturedCommentBody.startsWith("> [!CAUTION]")).toBe(true);
      expect(capturedCommentBody.indexOf("> [!CAUTION]")).toBeLessThan(capturedCommentBody.indexOf("COMMENT TEMPLATE CONTENT"));
      expect((capturedCommentBody.match(/> \[!CAUTION\]/g) || []).length).toBe(1);
      expect(capturedCommentBody).toContain("> Generated from [Test Workflow]");
    });

    it("prepends caution callout to new issue body and includes it only once", async () => {
      /** @type {string} */
      let capturedIssueBody = "";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => {
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: vi.fn(async ({ body }) => {
              capturedIssueBody = body;
              return {
                data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
              };
            }),
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(capturedIssueBody).toBeTruthy();
      expect(capturedIssueBody.startsWith("> [!CAUTION]")).toBe(true);
      expect(capturedIssueBody.indexOf("> [!CAUTION]")).toBeLessThan(capturedIssueBody.indexOf("ISSUE TEMPLATE CONTENT"));
      expect((capturedIssueBody.match(/> \[!CAUTION\]/g) || []).length).toBe(1);
      expect(capturedIssueBody).toContain("> Generated from [Test Workflow]");
    });

    it("includes AIC and ambient context metrics in the generated failure issue footer", async () => {
      process.env.GH_AW_AIC = "1.25";
      process.env.GH_AW_AMBIENT_CONTEXT = "900";
      process.env.GH_AW_ENGINE_MODEL = "claude-sonnet-4.6";
      /** @type {string} */
      let capturedIssueBody = "";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: vi.fn(async ({ body }) => {
              capturedIssueBody = body;
              return {
                data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
              };
            }),
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      try {
        await main();

        expect(capturedIssueBody).toContain("> Generated from [Test Workflow](https://github.com/owner/repo/actions/runs/123456) · sonnet46 · 1.25 AIC · ⊞ 900");
      } finally {
        delete process.env.GH_AW_AIC;
        delete process.env.GH_AW_AMBIENT_CONTEXT;
        delete process.env.GH_AW_ENGINE_MODEL;
      }
    });

    it("includes AIC in failure issue footer when resolved from audit log and GH_AW_AIC is unset", async () => {
      const auditPath = path.join(tmpDir, "sandbox", "firewall", "audit");
      fs.mkdirSync(auditPath, { recursive: true });
      fs.writeFileSync(path.join(auditPath, "log.jsonl"), `${JSON.stringify({ ai_credits: "2.5" })}\n`);
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent-output.json");
      expect(process.env.GH_AW_AIC).toBeUndefined();
      /** @type {string} */
      let capturedIssueBody = "";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: vi.fn(async ({ body }) => {
              capturedIssueBody = body;
              return {
                data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
              };
            }),
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
        expect(capturedIssueBody).toContain("> Generated from [Test Workflow](https://github.com/owner/repo/actions/runs/123456) · 2.5 AIC");
      } finally {
        delete process.env.GH_AW_AGENT_OUTPUT;
      }
    });

    it("includes AIC in failure comment footer when resolved from audit log and GH_AW_AIC is unset", async () => {
      const auditPath = path.join(tmpDir, "sandbox", "firewall", "audit");
      fs.mkdirSync(auditPath, { recursive: true });
      fs.writeFileSync(path.join(auditPath, "log.jsonl"), `${JSON.stringify({ ai_credits: "2.5" })}\n`);
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent-output.json");
      expect(process.env.GH_AW_AIC).toBeUndefined();
      /** @type {string} */
      let capturedCommentBody = "";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      body:
                        "> footer\n> - [x] expires <!-- gh-aw-expires: 2099-01-01T00:00:00.000Z --> on Jan 1, 2099, 12:00 AM UTC\n\n" +
                        "<!-- gh-aw-agentic-workflow: Test Workflow, workflow_id: test-workflow, run: https://github.com/owner/repo/actions/runs/123456 -->\n" +
                        "<!-- gh-aw-failure-issue: true, workflow_id: test-workflow, branch: feature/detection-caution, failure_categories: agent_failure -->",
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            createComment: vi.fn(async ({ body }) => {
              capturedCommentBody = body;
              return { data: { id: 1001 } };
            }),
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
        expect(capturedCommentBody).toContain("> Generated from [Test Workflow](https://github.com/owner/repo/actions/runs/123456) · 2.5 AIC");
      } finally {
        delete process.env.GH_AW_AGENT_OUTPUT;
      }
    });
  });

  describe("main() precise failure issue matching", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;

    function buildExistingIssueBody({ branch, categories, expires = "2099-01-01T00:00:00.000Z", pullRequestNumber, workflowName = "Test Workflow", workflowId = "test-workflow" } = {}) {
      const prPart = pullRequestNumber ? `, pull_request: ${pullRequestNumber}` : "";
      return (
        `> Generated from [${workflowName}](https://github.com/owner/repo/actions/runs/123456)\n` +
        `> - [x] expires <!-- gh-aw-expires: ${expires} --> on Jan 1, 2099, 12:00 AM UTC\n\n` +
        `<!-- gh-aw-agentic-workflow: ${workflowName}, workflow_id: ${workflowId}, run: https://github.com/owner/repo/actions/runs/123456 -->\n` +
        `<!-- gh-aw-failure-issue: true, workflow_id: ${workflowId}, branch: ${branch || ""}, failure_categories: ${categories.join("|")}${prPart} -->`
      );
    }

    function buildWorkflowMarkerOnlyIssueBody({ expires = "2099-01-01T00:00:00.000Z", workflowName = "Test Workflow", workflowId = "test-workflow" } = {}) {
      return (
        `> Generated from [${workflowName}](https://github.com/owner/repo/actions/runs/123456)\n` +
        `> - [x] expires <!-- gh-aw-expires: ${expires} --> on Jan 1, 2099, 12:00 AM UTC\n\n` +
        `<!-- gh-aw-agentic-workflow: ${workflowName}, workflow_id: ${workflowId}, run: https://github.com/owner/repo/actions/runs/123456 -->`
      );
    }

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-handle-agent-failure-match-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "agent_failure_comment.md"), "COMMENT TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "ISSUE TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_issue.md"), "Daily cap rollup issue body cap={cap} window={window_hours}");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_comment.md"), "Failure suppressed workflow={workflow_name} run={run_url} categories={summary} cap={cap} window={window_hours}h");
      fs.writeFileSync(path.join(promptsDir, "optimize_token_consumption_context.md"), "OPTIMIZE CONTEXT guardrail={guardrail_name} run={run_url}");

      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123456";
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      process.env.GITHUB_HEAD_REF = "feature/current";
      process.env.GITHUB_WORKSPACE = tmpDir;
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_RUN_URL;
      delete process.env.GH_AW_AGENT_CONCLUSION;
      delete process.env.GH_AW_FAILURE_REPORT_AS_ISSUE;
      delete process.env.GH_AW_FAILURE_ISSUE_NUMBER;
      delete process.env.GH_AW_STEERING_ISSUE_URL;
      delete process.env.GH_AW_AGENTIC_ENGINE_TIMEOUT;
      delete process.env.GH_AW_SHELL_EXPANSION_GUARD_REJECTED;
      delete process.env.GITHUB_HEAD_REF;
      delete process.env.GITHUB_WORKSPACE;
      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("adds a comment only when the existing issue metadata matches exactly", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      body: buildExistingIssueBody({ branch: "feature/current", categories: ["missing_safe_outputs"] }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
    });

    it("reuses the steering issue instead of searching for or creating a failure issue", async () => {
      process.env.GH_AW_FAILURE_ISSUE_NUMBER = "42";
      const updateIssueMock = vi.fn(async options => ({
        data: {
          number: options.issue_number,
          html_url: "https://github.com/owner/repo/issues/42",
          node_id: "issue-node",
        },
      }));
      const createIssueMock = vi.fn();
      const searchMock = vi.fn(async () => ({ data: { total_count: 0, items: [] } }));

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            get: vi.fn(async () => ({
              data: {
                number: 42,
                html_url: "https://github.com/owner/repo/issues/42",
                node_id: "issue-node",
                state: "open",
                labels: [],
              },
            })),
            create: createIssueMock,
            update: updateIssueMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(updateIssueMock).toHaveBeenCalledWith(
        expect.objectContaining({
          issue_number: 42,
          title: "[aw] Test Workflow produced no safe outputs",
          labels: ["agentic-workflows"],
          state: "open",
        })
      );
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(searchMock.mock.calls.some(([options]) => options.q.includes('"gh-aw-agentic-workflow:"'))).toBe(false);
      expect(global.core.setOutput).toHaveBeenCalledWith("failure_issue_number", "42");
      expect(global.core.setOutput).toHaveBeenCalledWith("failure_issue_url", "https://github.com/owner/repo/issues/42");
    });

    it("skips failure issue creation when the runtime report flag resolves to false", async () => {
      const searchMock = vi.fn();
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn();
      process.env.GH_AW_FAILURE_REPORT_AS_ISSUE = " False ";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(searchMock).not.toHaveBeenCalled();
      expect(createCommentMock).not.toHaveBeenCalled();
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(global.core.info).toHaveBeenCalledWith("Failure issue reporting is disabled (report-failure-as-issue: false), skipping failure issue creation");
    });

    it("adds a comment when existing issue metadata contains commas in free-form values", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn();

      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow, Retry";
      process.env.GITHUB_HEAD_REF = "feature/current,with-comma";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      body: buildExistingIssueBody({
                        workflowName: "Test Workflow, Retry",
                        branch: "feature/current,with-comma",
                        categories: ["missing_safe_outputs"],
                      }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
    });

    it("creates a new issue when an open issue only has workflow XML metadata without failure metadata", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn();
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }
        return {
          data: {
            total_count: 1,
            items: [
              {
                number: 42,
                title: "[aw] Test Workflow failed",
                html_url: "https://github.com/owner/repo/issues/42",
                body: buildWorkflowMarkerOnlyIssueBody(),
              },
            ],
          },
        };
      });

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).not.toHaveBeenCalled();
      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.headers).toEqual({ "X-GitHub-Api-Version": "2022-11-28" });
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining('"gh-aw-agentic-workflow:"') }));
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining('"workflow_id: test-workflow" in:body') }));
    });

    it("creates a parent issue with the API version header when group reports are enabled", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn(async ({ title }) => ({
        data: {
          number: title === "[aw] Failed runs" ? 200 : 201,
          html_url: `https://github.com/owner/repo/issues/${title === "[aw] Failed runs" ? 200 : 201}`,
          node_id: title === "[aw] Failed runs" ? "I_parent" : "I_child",
        },
      }));
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }
        return { data: { total_count: 0, items: [] } };
      });

      process.env.GH_AW_GROUP_REPORTS = "true";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      const parentCreateCall = createIssueMock.mock.calls.map(([call]) => call).find(call => call.title === "[aw] Failed runs");
      expect(parentCreateCall).toBeDefined();
      expect(parentCreateCall.headers).toEqual({ "X-GitHub-Api-Version": "2022-11-28" });
      expect(createCommentMock).not.toHaveBeenCalled();
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining('"[aw] Failed runs"') }));
    });

    it("creates a new parent issue when the existing parent issue has expired", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn(async ({ title }) => ({
        data: {
          number: title === "[aw] Failed runs" ? 300 : 301,
          html_url: `https://github.com/owner/repo/issues/${title === "[aw] Failed runs" ? 300 : 301}`,
          node_id: title === "[aw] Failed runs" ? "I_parent_new" : "I_child",
        },
      }));
      const expiredParentBody = "This issue tracks failures.\n\n> - [x] expires <!-- gh-aw-expires: 2000-01-01T00:00:00.000Z --> on Jan 1, 2000, 12:00 AM UTC";
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }
        if (q.includes('"[aw] Failed runs"')) {
          return {
            data: {
              total_count: 1,
              items: [{ number: 199, html_url: "https://github.com/owner/repo/issues/199", node_id: "I_parent_old", body: expiredParentBody }],
            },
          };
        }
        return { data: { total_count: 0, items: [] } };
      });

      process.env.GH_AW_GROUP_REPORTS = "true";

      const graphqlMock = vi.fn(async () => ({ repository: { issue: { subIssues: { totalCount: 0 } } } }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: graphqlMock,
      };

      await main();

      // github.rest.issues.create is always invoked with a single options object
      // (see github.rest.issues.create({...}) call sites in handle_agent_failure.cjs),
      // so destructuring the first call argument yields the options object itself.
      const parentCreateCall = createIssueMock.mock.calls.map(([call]) => call).find(call => call.title === "[aw] Failed runs");
      expect(parentCreateCall).toBeDefined();
      expect(parentCreateCall.body).toContain("previous parent issue #199");
      // Expired parent must not be reused: getSubIssueCount must not be queried
      // for the expired parent #199, since the expiration check short-circuits first.
      expect(graphqlMock).not.toHaveBeenCalledWith(expect.stringContaining("subIssues"), expect.objectContaining({ issueNumber: 199 }));
    });

    it("does not abort grouped handling when fetching parent issue body fails", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn(async ({ title }) => ({
        data: {
          number: title === "[aw] Failed runs" ? 300 : 301,
          html_url: `https://github.com/owner/repo/issues/${title === "[aw] Failed runs" ? 300 : 301}`,
          node_id: title === "[aw] Failed runs" ? "I_parent_new" : "I_child",
        },
      }));
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }
        if (q.includes('"[aw] Failed runs"')) {
          return {
            data: {
              total_count: 1,
              items: [{ number: 199, html_url: "https://github.com/owner/repo/issues/199", node_id: "I_parent_old", body: null }],
            },
          };
        }
        return { data: { total_count: 0, items: [] } };
      });
      const getIssueMock = vi.fn(async () => {
        throw new Error("transient API failure");
      });
      const graphqlMock = vi.fn(async () => ({ repository: { issue: { subIssues: { totalCount: 1 } } } }));

      process.env.GH_AW_GROUP_REPORTS = "true";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            get: getIssueMock,
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: graphqlMock,
      };

      await main();

      expect(getIssueMock).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 199 }));
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Could not fetch parent issue #199 body"));
      const parentCreateCall = createIssueMock.mock.calls.map(([call]) => call).find(call => call.title === "[aw] Failed runs");
      expect(parentCreateCall).toBeUndefined();
      expect(createIssueMock).toHaveBeenCalledOnce();
      expect(createCommentMock).not.toHaveBeenCalled();
    });

    it("escapes workflow IDs before searching for legacy XML marker matches", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn();
      const workflowId = 'test"workflow\\path\nnext-line';
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }
        return {
          data: {
            total_count: 1,
            items: [
              {
                number: 42,
                title: "[aw] Test Workflow failed",
                html_url: "https://github.com/owner/repo/issues/42",
                body: buildExistingIssueBody({
                  workflowId,
                  branch: "feature/current",
                  categories: ["missing_safe_outputs"],
                }),
              },
            ],
          },
        };
      });

      process.env.GH_AW_WORKFLOW_ID = workflowId;

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining('"workflow_id: test\\"workflow\\\\path next-line" in:body') }));
    });

    it("adds a comment when branch differs but workflow/category markers match", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 2,
                  items: [
                    {
                      number: 42,
                      title: "[aw] Test Workflow failed",
                      html_url: "https://github.com/owner/repo/issues/42",
                      body: buildExistingIssueBody({ branch: "feature/other", categories: ["missing_safe_outputs"] }),
                    },
                    {
                      number: 43,
                      title: "[aw] Test Workflow failed",
                      html_url: "https://github.com/owner/repo/issues/43",
                      body: buildExistingIssueBody({ branch: "feature/current", categories: ["agent_failure"] }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
    });

    it("creates a new issue instead of commenting on an expired issue", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      body: buildExistingIssueBody({
                        branch: "feature/current",
                        categories: ["missing_safe_outputs"],
                        expires: "2000-01-01T00:00:00.000Z",
                      }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).not.toHaveBeenCalled();
      expect(createIssueMock).toHaveBeenCalledOnce();
    });

    it("creates a new issue when the only matching issue is older than 24 hours", async () => {
      const createCommentMock = vi.fn();
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));
      const oldTimestamp = new Date(Date.now() - 25 * 60 * 60 * 1000).toISOString();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      html_url: "https://github.com/owner/repo/issues/42",
                      created_at: oldTimestamp,
                      body: buildExistingIssueBody({
                        branch: "feature/current",
                        categories: ["missing_safe_outputs"],
                      }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).not.toHaveBeenCalled();
      expect(createIssueMock).toHaveBeenCalledOnce();
    });

    it("uses a precise timeout title when the agent times out", async () => {
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));
      fs.writeFileSync(path.join(promptsDir, "agent_timeout.md"), "TIMEOUT TEMPLATE");
      process.env.GH_AW_AGENT_CONCLUSION = "timed_out";
      process.env.GH_AW_FAILURE_REPORT_AS_ISSUE = "true";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
      } finally {
        delete process.env.GH_AW_FAILURE_REPORT_AS_ISSUE;
      }

      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe("[aw] Test Workflow timed out");
    });

    it("uses shell expansion guard title instead of timeout when both flags are present", async () => {
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "{shell_expansion_guard_rejected_context}{timeout_context}{engine_failure_context}");
      fs.writeFileSync(path.join(promptsDir, "agent_timeout.md"), "TIMEOUT TEMPLATE");
      fs.writeFileSync(path.join(promptsDir, "shell_expansion_guard_rejected.md"), "SHELL GUARD TEMPLATE");
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      process.env.GH_AW_AGENTIC_ENGINE_TIMEOUT = "true";
      process.env.GH_AW_SHELL_EXPANSION_GUARD_REJECTED = "true";
      process.env.GH_AW_FAILURE_REPORT_AS_ISSUE = "true";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
      } finally {
        delete process.env.GH_AW_FAILURE_REPORT_AS_ISSUE;
      }

      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe("[aw] Test Workflow hit shell expansion guard rejection");
      expect(createCall.body).toContain("SHELL GUARD TEMPLATE");
      expect(createCall.body).not.toContain("TIMEOUT TEMPLATE");
    });

    it("uses a precise missing safe outputs title", async () => {
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));
      process.env.GH_AW_FAILURE_REPORT_AS_ISSUE = "true";

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
      } finally {
        delete process.env.GH_AW_FAILURE_REPORT_AS_ISSUE;
      }

      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe("[aw] Test Workflow produced no safe outputs");
    });

    it("uses a precise report incomplete title", async () => {
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));
      const agentOutputPath = path.join(tmpDir, "agent_output.json");
      fs.writeFileSync(agentOutputPath, JSON.stringify({ items: [{ type: "report_incomplete", reason: "tool failed" }] }));
      process.env.GH_AW_AGENT_OUTPUT = agentOutputPath;

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      try {
        await main();
      } finally {
        delete process.env.GH_AW_AGENT_OUTPUT;
      }

      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe("[aw] Test Workflow reported incomplete result");
    });

    it("continues searching later pages until it finds an exact metadata match", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn();
      const searchMock = vi.fn(async ({ q, page }) => {
        if (q.includes("is:pr")) {
          return { data: { total_count: 0, items: [] } };
        }

        if (page === 1) {
          return {
            data: {
              total_count: 101,
              items: Array.from({ length: 100 }, (_, index) => ({
                number: index + 1,
                html_url: `https://github.com/owner/repo/issues/${index + 1}`,
                body: buildExistingIssueBody({ branch: `feature/other-${index + 1}`, categories: ["agent_failure"] }),
              })),
            },
          };
        }

        return {
          data: {
            total_count: 101,
            items: [
              {
                number: 101,
                html_url: "https://github.com/owner/repo/issues/101",
                body: buildExistingIssueBody({ branch: "feature/current", categories: ["missing_safe_outputs"] }),
              },
            ],
          },
        };
      });

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: searchMock,
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ page: 1 }));
      expect(searchMock).toHaveBeenCalledWith(expect.objectContaining({ page: 2 }));
    });

    it("adds a comment when pull request metadata differs but workflow/category markers match", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
      }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return {
                  data: {
                    total_count: 1,
                    items: [{ number: 123, html_url: "https://github.com/owner/repo/pull/123" }],
                  },
                };
              }
              return {
                data: {
                  total_count: 1,
                  items: [
                    {
                      number: 42,
                      title: "[aw] Test Workflow failed",
                      html_url: "https://github.com/owner/repo/issues/42",
                      body: buildExistingIssueBody({
                        branch: "feature/current",
                        categories: ["missing_safe_outputs"],
                        pullRequestNumber: 999,
                      }),
                    },
                  ],
                },
              };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: {
            get: vi.fn(async () => ({
              data: {
                head: { sha: "abc123" },
                mergeable: true,
                mergeable_state: "clean",
                updated_at: "2026-05-18T00:00:00Z",
              },
            })),
          },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createIssueMock).not.toHaveBeenCalled();
    });

    it("creates a centralized rollup issue and adds a comment when per-category daily cap is reached", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 2001 } }));
      const createIssueMock = vi.fn(async () => ({
        data: { number: 999, html_url: "https://github.com/owner/repo/issues/999", node_id: "I_999" },
      }));
      const now = new Date().toISOString();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              if (q.includes('"workflow_id: test-workflow" in:body')) {
                return { data: { total_count: 0, items: [] } };
              }
              if (q.includes('"gh-aw-failure-issue:"') && q.includes('"failure_categories:"')) {
                return {
                  data: {
                    total_count: 50,
                    items: Array.from({ length: 50 }, (_, index) => ({
                      number: index + 1,
                      html_url: `https://github.com/owner/repo/issues/${index + 1}`,
                      created_at: now,
                      body: buildExistingIssueBody({
                        branch: `feature/any-${index + 1}`,
                        categories: ["missing_safe_outputs"],
                      }),
                    })),
                  },
                };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Daily per-category issue cap reached"));
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Summarize-and-stop"));
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe("[aw] Daily failure issue cap exceeded");
      expect(createCall.headers).toEqual({ "X-GitHub-Api-Version": "2022-11-28" });
      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createCommentMock).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 999 }));
      expect(global.github.rest.search.issuesAndPullRequests).toHaveBeenCalledWith(expect.objectContaining({ q: expect.stringContaining("is:open") }));
    });

    it("reuses an existing rollup issue when per-category daily cap is reached", async () => {
      const createCommentMock = vi.fn(async () => ({ data: { id: 2002 } }));
      const createIssueMock = vi.fn();
      const now = new Date().toISOString();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              if (q.includes('"workflow_id: test-workflow" in:body')) {
                return { data: { total_count: 0, items: [] } };
              }
              if (q.includes('"gh-aw-failure-issue:"') && q.includes('"failure_categories:"')) {
                return {
                  data: {
                    total_count: 50,
                    items: Array.from({ length: 50 }, (_, index) => ({
                      number: index + 1,
                      html_url: `https://github.com/owner/repo/issues/${index + 1}`,
                      created_at: now,
                      body: buildExistingIssueBody({
                        branch: `feature/any-${index + 1}`,
                        categories: ["missing_safe_outputs"],
                      }),
                    })),
                  },
                };
              }
              if (q.includes("[aw] Daily failure issue cap exceeded")) {
                return {
                  data: {
                    total_count: 1,
                    items: [{ number: 888, html_url: "https://github.com/owner/repo/issues/888" }],
                  },
                };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: {
            get: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      await main();

      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("Daily per-category issue cap reached"));
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(createCommentMock).toHaveBeenCalledOnce();
      expect(createCommentMock).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 888 }));
    });
  });

  describe("main() writes failure_categories.json", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;
    let { FAILURE_CATEGORIES_PATH } = require("./handle_agent_failure.cjs");

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-failure-categories-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "agent_failure_comment.md"), "COMMENT TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "ISSUE TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_issue.md"), "Daily cap rollup issue body cap={cap} window={window_hours}");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_comment.md"), "Failure suppressed workflow={workflow_name} run={run_url} categories={summary} cap={cap} window={window_hours}h");
      fs.writeFileSync(path.join(promptsDir, "optimize_token_consumption_context.md"), "OPTIMIZE CONTEXT guardrail={guardrail_name} run={run_url}");

      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123456";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      process.env.GITHUB_HEAD_REF = "feature/test";
      process.env.GITHUB_WORKSPACE = tmpDir;
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_RUN_URL;
      delete process.env.GH_AW_AGENT_CONCLUSION;
      delete process.env.GITHUB_HEAD_REF;
      delete process.env.GITHUB_WORKSPACE;
      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("writes failure_categories.json with the computed failure categories", async () => {
      const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: vi.fn(async () => ({
              data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_123" },
            })),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      let writeCalls;
      try {
        await main();
        writeCalls = writeSpy.mock.calls.slice();
      } finally {
        writeSpy.mockRestore();
      }

      const categoriesCall = writeCalls.find(([filePath]) => filePath === FAILURE_CATEGORIES_PATH);
      expect(categoriesCall).toBeDefined();
      const written = JSON.parse(categoriesCall[1]);
      expect(Array.isArray(written)).toBe(true);
      expect(written).toContain("agent_failure");
    });

    it("includes timed_out category when GH_AW_AGENTIC_ENGINE_TIMEOUT is set", async () => {
      process.env.GH_AW_AGENTIC_ENGINE_TIMEOUT = "true";
      const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation(() => {});

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: vi.fn(async () => ({
              data: { number: 102, html_url: "https://github.com/owner/repo/issues/102", node_id: "I_456" },
            })),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      let writeCalls;
      try {
        await main();
        writeCalls = writeSpy.mock.calls.slice();
      } finally {
        writeSpy.mockRestore();
      }

      const categoriesCall = writeCalls.find(([filePath]) => filePath === FAILURE_CATEGORIES_PATH);
      expect(categoriesCall).toBeDefined();
      const written = JSON.parse(categoriesCall[1]);
      expect(written).toContain("timed_out");
    });
  });

  describe("agent failure templates", () => {
    const fs = require("fs");
    const path = require("path");
    const { renderTemplate } = require("./messages_core.cjs");
    const reportIncompleteMarker = "MARKER: cannot continue";

    it("renders report_incomplete context in both comment and issue templates", () => {
      const reportIncompleteContext = buildReportIncompleteContext([{ type: "report_incomplete", reason: reportIncompleteMarker }]);
      const templateContext = {
        run_id: "123456",
        run_url: "https://github.com/owner/repo/actions/runs/123456",
        workflow_name: "Test Workflow",
        workflow_source_url: "https://github.com/owner/repo/blob/main/.github/workflows/test.md",
        branch: "main",
        pull_request_info: "",
        secret_verification_context: "",
        docker_sbx_secrets_context: "",
        credential_auth_error_context: "",
        inference_access_error_context: "",
        mcp_policy_error_context: "",
        model_not_supported_error_context: "",
        http_400_response_error_context: "",
        ai_credits_rate_limit_error_context: "",
        app_token_minting_failed_context: "",
        lockdown_check_failed_context: "",
        stale_lock_file_failed_context: "",
        assignment_errors_context: "",
        assign_copilot_failure_context: "",
        create_discussion_errors_context: "",
        code_push_failure_context: "",
        repo_memory_validation_context: "",
        push_repo_memory_failure_context: "",
        missing_data_context: "",
        missing_tool_context: "",
        permission_denied_context: "",
        tool_denials_exceeded_context: "",
        report_incomplete_context: reportIncompleteContext,
        missing_safe_outputs_context: "",
        engine_failure_context: "",
        timeout_context: "",
        fork_context: "",
      };

      const commentTemplate = fs.readFileSync(path.join(__dirname, "../md/agent_failure_comment.md"), "utf8");
      const issueTemplate = fs.readFileSync(path.join(__dirname, "../md/agent_failure_issue.md"), "utf8");

      expect(renderTemplate(commentTemplate, templateContext)).toContain(reportIncompleteMarker);
      expect(renderTemplate(issueTemplate, templateContext)).toContain(reportIncompleteMarker);
    });
  });

  describe("buildSecretVerificationContext", () => {
    it("returns empty string when verification did not fail", () => {
      const copilotMessage = "**Alternative**: If your organization has a Copilot subscription, you can avoid the need for a personal access token by adding a top-level `permissions` block to your workflow file.";
      expect(buildSecretVerificationContext("", copilotMessage)).toBe("");
      expect(buildSecretVerificationContext("success", copilotMessage)).toBe("");
      expect(buildSecretVerificationContext("", "")).toBe("");
    });

    describe("buildDockerSbxSecretsContext", () => {
      it("returns empty string when docker-sbx secret verification did not fail", () => {
        expect(buildDockerSbxSecretsContext("")).toBe("");
        expect(buildDockerSbxSecretsContext("success")).toBe("");
      });

      it("renders docker-sbx setup guidance from the dedicated markdown template", () => {
        const originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
        try {
          process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir;
          const result = buildDockerSbxSecretsContext("failed");

          expect(result).toContain("Docker sbx is not configured");
          expect(result).toContain("DOCKER_USERNAME");
          expect(result).toContain("DOCKER_PAT");
          expect(result).toContain("sandbox.agent.runtime: docker-sbx");
        } finally {
          if (originalPromptsDir === undefined) {
            delete process.env.GH_AW_PROMPTS_DIR;
          } else {
            process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
          }
        }
      });
    });

    describe("buildAssignmentErrorsContext", () => {
      it("returns empty string when there are no assignment errors", () => {
        expect(buildAssignmentErrorsContext("")).toBe("");
      });

      it("returns empty string for whitespace-only input", () => {
        expect(buildAssignmentErrorsContext("   ")).toBe("");
        expect(buildAssignmentErrorsContext("\n")).toBe("");
        expect(buildAssignmentErrorsContext("\n\n")).toBe("");
      });

      it("renders assignment failures with token guidance docs", () => {
        const originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
        try {
          process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir;
          const result = buildAssignmentErrorsContext("issue:42:copilot:Bad credentials\npr:7:copilot:copilot coding agent is not available for this repository");

          expect(result).toContain("Agent Assignment Failed");
          expect(result).toContain("Issue #42 (agent: copilot): Bad credentials");
          expect(result).toContain("PR #7 (agent: copilot): copilot coding agent is not available for this repository");
          expect(result).toContain("GH_AW_AGENT_TOKEN");
          expect(result).toContain("metadata: read");
          expect(result).toContain("GitHub App installation token");
          expect(result).toContain("https://github.github.com/gh-aw/reference/copilot-cloud-agent/#authentication");
          expect(result).toContain("https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api");
          expect(result).not.toContain("copilot-requests: write");
        } finally {
          if (originalPromptsDir === undefined) {
            delete process.env.GH_AW_PROMPTS_DIR;
          } else {
            process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
          }
        }
      });
    });

    describe("buildAssignCopilotFailureContext", () => {
      it("returns empty string when there are no copilot assignment failures", () => {
        expect(buildAssignCopilotFailureContext(false, "")).toBe("");
      });

      it("renders standardized copilot assignment remediation guidance", () => {
        const originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
        try {
          process.env.GH_AW_PROMPTS_DIR = runtimePromptsDir;
          const result = buildAssignCopilotFailureContext(true, "issue:42:copilot:Bad credentials");

          expect(result).toContain("Copilot Assignment Failed");
          expect(result).toContain("Issue #42: Bad credentials");
          expect(result).toContain("GH_AW_AGENT_TOKEN");
          expect(result).toContain("metadata");
          expect(result).toContain("GitHub App installation token");
          expect(result).toContain("YOUR_AGENT_PAT");
          expect(result).toContain("https://github.github.com/gh-aw/reference/copilot-cloud-agent/#authentication");
          expect(result).toContain("https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api");
        } finally {
          if (originalPromptsDir === undefined) {
            delete process.env.GH_AW_PROMPTS_DIR;
          } else {
            process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
          }
        }
      });
    });

    it("returns generic warning for engines with no failure message when verification failed", () => {
      const result = buildSecretVerificationContext("failed", "");
      expect(result).toContain("Secret Verification Failed");
      expect(result).toContain("required secrets are configured");
      expect(result).toContain("https://github.github.com/gh-aw/reference/engines/");
      expect(result).not.toContain("copilot-requests");
    });

    it("returns copilot-specific message with copilot-requests: write permissions suggestion when verification failed", () => {
      const copilotMessage =
        "**Alternative**: If your organization has a Copilot subscription, you can avoid the need for a personal access token by adding a top-level `permissions` block to your workflow file. " +
        "This enables Copilot inference through the org using the built-in GitHub Actions token.\n" +
        "\n```yaml\npermissions:\n  copilot-requests: write\n```\n" +
        "\nSee: https://github.github.com/gh-aw/reference/engines/#github-copilot-default";
      const result = buildSecretVerificationContext("failed", copilotMessage);
      expect(result).toContain("Secret Verification Failed");
      expect(result).toContain("required secrets are configured");
      expect(result).toContain("```yaml\npermissions:\n  copilot-requests: write\n```");
      expect(result).toContain("https://github.github.com/gh-aw/reference/engines/#github-copilot-default");
    });
  });

  describe("buildCodePushFailureContext", () => {
    it("returns empty string when no errors", () => {
      expect(buildCodePushFailureContext("")).toBe("");
      expect(buildCodePushFailureContext(null)).toBe("");
      expect(buildCodePushFailureContext(undefined)).toBe("");
    });

    it("shows protected file protection section for protected file errors", () => {
      const errors = "create_pull_request:Cannot create pull request: patch modifies protected files (package.json). Set manifest-files: fallback-to-issue to create a review issue instead.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).toContain("package.json");
      expect(result).toContain("protected-files: fallback-to-issue");
      // Should NOT contain generic "Code Push Failed" for pure manifest errors
      expect(result).not.toContain("Code Push Failed");
    });

    it("shows protected file protection section for legacy 'package manifest files' error messages", () => {
      // Old error message format – must still be detected
      const errors = "create_pull_request:Cannot create pull request: patch modifies package manifest files (package.json). Set allow-manifest-files: true in your workflow to allow this.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).not.toContain("Code Push Failed");
    });

    it("shows protected file protection section for push_to_pull_request_branch errors", () => {
      const errors = "push_to_pull_request_branch:Cannot push to pull request branch: patch modifies protected files (go.mod, go.sum). Set manifest-files: fallback-to-issue to create a review issue.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).toContain("go.mod");
      expect(result).toContain("`push_to_pull_request_branch`");
      expect(result).not.toContain("Code Push Failed");
    });

    it("shows protected file protection for .github/ protected path errors", () => {
      const errors = "create_pull_request:Cannot create pull request: patch modifies protected files (.github/workflows/ci.yml). Set manifest-files: fallback-to-issue to create a review issue.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).toContain(".github/workflows/ci.yml");
    });

    it("includes PR link in protected file protection section when PR is provided", () => {
      const errors = "create_pull_request:Cannot create pull request: patch modifies package manifest files (package.json). Set allow-manifest-files: true in your workflow to allow this.";
      const pullRequest = { number: 42, html_url: "https://github.com/owner/repo/pull/42" };
      const result = buildCodePushFailureContext(errors, pullRequest);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).toContain("#42");
      expect(result).toContain("https://github.com/owner/repo/pull/42");
      // PR state diagnostics should NOT appear for protected-file-only failures
      expect(result).not.toContain("PR State at Push Time");
    });

    it("shows generic code push failure section for non-manifest errors", () => {
      const errors = "push_to_pull_request_branch:Branch not found";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("Code Push Failed");
      expect(result).toContain("Branch not found");
      expect(result).not.toContain("Protected Files");
    });

    it("renders allowed-files errors with progressive disclosure", () => {
      const errors =
        "create_pull_request:Cannot create pull request: patch modifies files outside the allowed-files list " +
        "(pkg/workflow/codex_engine.go, pkg/workflow/codex_mcp.go, pkg/workflow/compiler_yaml.go). " +
        "Add the files to the allowed-files configuration field or remove them from the patch.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("Code Push Failed");
      expect(result).toContain("outside the allowed-files list. Add the files to the allowed-files configuration field or remove them from the patch.");
      expect(result).toContain("<details>");
      expect(result).toContain("<summary>Show 3 blocked files</summary>");
      expect(result).toContain("`pkg/workflow/codex_engine.go`");
      expect(result).toContain("`pkg/workflow/codex_mcp.go`");
      expect(result).toContain("`pkg/workflow/compiler_yaml.go`");
      expect(result).not.toContain("outside the allowed-files list (pkg/workflow/codex_engine.go, pkg/workflow/codex_mcp.go, pkg/workflow/compiler_yaml.go)");
    });

    it("falls back to raw error line for non-matching allowed-files text", () => {
      const errors = "create_pull_request:Cannot create pull request: some other error message.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("Cannot create pull request: some other error message.");
      expect(result).not.toContain("<details>");
    });

    it("uses singular wording for one blocked file", () => {
      const errors = "create_pull_request:Cannot create pull request: patch modifies files outside the allowed-files list " + "(pkg/foo.go). Add the files to the allowed-files configuration field or remove them from the patch.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("<summary>Show 1 blocked file</summary>");
      expect(result).toContain("`pkg/foo.go`");
    });

    it("renders bundle remediation variant", () => {
      const errors =
        "create_pull_request:Cannot create pull request: patch modifies files outside the allowed-files list " +
        "(pkg/workflow/compiler(bundle).go, pkg/workflow/compiler_yaml.go). " +
        "Add the files to the allowed-files configuration field or remove them from the bundle.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("remove them from the bundle.");
      expect(result).toContain("`pkg/workflow/compiler(bundle).go`");
      expect(result).toContain("`pkg/workflow/compiler_yaml.go`");
      expect(result).toContain("<summary>Show 2 blocked files</summary>");
    });

    it("shows both sections when protected file and non-protected-file errors are mixed", () => {
      const errors = [
        "create_pull_request:Cannot create pull request: patch modifies package manifest files (package.json). Set allow-manifest-files: true in your workflow to allow this.",
        "push_to_pull_request_branch:Branch not found",
      ].join("\n");
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🛡️ Protected Files");
      expect(result).toContain("Code Push Failed");
      expect(result).toContain("package.json");
      expect(result).toContain("Branch not found");
    });

    it("includes yaml remediation snippet in protected file protection section", () => {
      const errors = "create_pull_request:Cannot create pull request: patch modifies package manifest files (requirements.txt). Set allow-manifest-files: true in your workflow to allow this.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("```yaml");
      expect(result).toContain("create-pull-request:");
      expect(result).toContain("protected-files: fallback-to-issue");
    });

    it("uses push-to-pull-request-branch key in yaml snippet for push type", () => {
      const errors = "push_to_pull_request_branch:Cannot push to pull request branch: patch modifies package manifest files (go.mod). Set manifest-files: fallback-to-issue in your workflow to allow this.";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("push-to-pull-request-branch:");
      expect(result).toContain("protected-files: fallback-to-issue");
      expect(result).not.toContain("create-pull-request:");
    });

    it("includes both yaml keys when both types have protected file errors", () => {
      const errors = [
        "create_pull_request:Cannot create pull request: patch modifies package manifest files (package.json). Set manifest-files: fallback-to-issue in your workflow to allow this.",
        "push_to_pull_request_branch:Cannot push to pull request branch: patch modifies package manifest files (go.mod). Set manifest-files: fallback-to-issue in your workflow to allow this.",
      ].join("\n");
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("create-pull-request:");
      expect(result).toContain("push-to-pull-request-branch:");
    });

    // ──────────────────────────────────────────────────────
    // Patch Size Exceeded
    // ──────────────────────────────────────────────────────

    it("shows patch size exceeded section for create_pull_request patch size error", () => {
      const errors = "create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (1024 KB)";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("📦 Patch Size Exceeded");
      expect(result).toContain("create-pull-request:");
      expect(result).toContain("max-patch-size:");
      expect(result).not.toContain("Code Push Failed");
      expect(result).not.toContain("Protected Files");
    });

    it("shows patch size exceeded section for push_to_pull_request_branch patch size error", () => {
      const errors = "push_to_pull_request_branch:Patch size (3072 KB) exceeds maximum allowed size (1024 KB)";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("📦 Patch Size Exceeded");
      expect(result).toContain("push-to-pull-request-branch:");
      expect(result).toContain("max-patch-size:");
      expect(result).not.toContain("Code Push Failed");
    });

    it("shows patch size exceeded yaml snippet with both types when both have patch size errors", () => {
      const errors = ["create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (1024 KB)", "push_to_pull_request_branch:Patch size (3072 KB) exceeds maximum allowed size (1024 KB)"].join("\n");
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("📦 Patch Size Exceeded");
      expect(result).toContain("create-pull-request:");
      expect(result).toContain("push-to-pull-request-branch:");
      expect(result).toContain("max-patch-size:");
    });

    it("includes PR link in patch size exceeded section when PR is provided", () => {
      const errors = "create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (1024 KB)";
      const pullRequest = { number: 99, html_url: "https://github.com/owner/repo/pull/99" };
      const result = buildCodePushFailureContext(errors, pullRequest);
      expect(result).toContain("📦 Patch Size Exceeded");
      expect(result).toContain("#99");
      expect(result).toContain("https://github.com/owner/repo/pull/99");
    });

    it("includes download instructions with run ID when runUrl is provided for patch size exceeded", () => {
      const errors = "create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (4096 KB)";
      const runUrl = "https://github.com/owner/repo/actions/runs/12345678";
      const result = buildCodePushFailureContext(errors, null, runUrl);
      expect(result).toContain("📥 Download the oversized patch");
      expect(result).toContain("gh run download 12345678");
      expect(result).toContain("View run and download artifacts");
      expect(result).toContain(runUrl);
      expect(result).toContain("<details>");
      expect(result).toContain("Download the oversized patch to inspect or apply manually");
    });

    it("includes generic download instructions when no runUrl is provided for patch size exceeded", () => {
      const errors = "create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (4096 KB)";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("📥 Download the oversized patch");
      expect(result).toContain("git am --3way");
      expect(result).not.toContain("gh run download");
      expect(result).toContain("<details>");
      expect(result).toContain("Download the oversized patch to inspect or apply manually");
    });

    it("does not show patch size section for generic errors", () => {
      const errors = "push_to_pull_request_branch:Branch not found";
      const result = buildCodePushFailureContext(errors);
      expect(result).not.toContain("📦 Patch Size Exceeded");
    });

    it("shows both patch size and generic sections when mixed", () => {
      const errors = ["create_pull_request:Patch size (2048 KB) exceeds maximum allowed size (1024 KB)", "push_to_pull_request_branch:Branch not found"].join("\n");
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("📦 Patch Size Exceeded");
      expect(result).toContain("Code Push Failed");
      expect(result).toContain("Branch not found");
    });

    // ──────────────────────────────────────────────────────
    // Patch Apply Failed (merge conflict)
    // ──────────────────────────────────────────────────────

    it("shows patch apply failed section for create_pull_request patch apply error", () => {
      const errors = "create_pull_request:Failed to apply patch";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("merge conflict");
      expect(result).toContain("`create_pull_request`");
      expect(result).toContain("Failed to apply patch");
      // Should NOT show generic "Code Push Failed" for pure patch apply errors
      expect(result).not.toContain("Code Push Failed");
    });

    it("shows patch apply failed section for push_to_pull_request_branch patch apply error", () => {
      const errors = "push_to_pull_request_branch:Failed to apply patch";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("`push_to_pull_request_branch`");
      expect(result).not.toContain("Code Push Failed");
    });

    it("includes PR link in patch apply failed section when PR is provided", () => {
      const errors = "create_pull_request:Failed to apply patch";
      const pullRequest = { number: 77, html_url: "https://github.com/owner/repo/pull/77" };
      const result = buildCodePushFailureContext(errors, pullRequest);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("#77");
      expect(result).toContain("https://github.com/owner/repo/pull/77");
    });

    it("includes patch download instructions with run ID when runUrl is provided", () => {
      const errors = "create_pull_request:Failed to apply patch";
      const runUrl = "https://github.com/owner/repo/actions/runs/12345678";
      const result = buildCodePushFailureContext(errors, null, runUrl);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("gh run download 12345678");
      expect(result).toContain("-n agent");
      expect(result).toContain("/tmp/agent-");
      expect(result).toContain("git am --3way");
      expect(result).toContain(runUrl);
      // Should use progressive disclosure for the apply commands
      expect(result).toContain("<details>");
      expect(result).toContain("Apply the patch manually");
    });

    it("shows generic download instructions when runUrl is not provided", () => {
      const errors = "create_pull_request:Failed to apply patch";
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("git am --3way");
      // No specific run ID in instructions
      expect(result).not.toContain("gh run download");
      // Should still use progressive disclosure
      expect(result).toContain("<details>");
      expect(result).toContain("Apply the patch manually");
    });

    it("shows both patch apply failed and generic sections when mixed", () => {
      const errors = ["create_pull_request:Failed to apply patch", "push_to_pull_request_branch:Branch not found"].join("\n");
      const result = buildCodePushFailureContext(errors);
      expect(result).toContain("🔀 Patch Apply Failed");
      expect(result).toContain("Code Push Failed");
      expect(result).toContain("Branch not found");
    });

    it("does not show patch apply section for generic errors", () => {
      const errors = "push_to_pull_request_branch:Branch not found";
      const result = buildCodePushFailureContext(errors);
      expect(result).not.toContain("🔀 Patch Apply Failed");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildPushRepoMemoryFailureContext
  // ──────────────────────────────────────────────────────

  describe("buildPushRepoMemoryFailureContext", () => {
    it("returns empty string when no failure", () => {
      expect(buildPushRepoMemoryFailureContext(false, [], "https://example.com/run")).toBe("");
    });

    it("shows generic failure message when failure but no patch size exceeded", () => {
      const result = buildPushRepoMemoryFailureContext(true, [], "https://example.com/run");
      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("Repo-Memory Push Failed");
      expect(result).toContain("https://example.com/run");
      expect(result).not.toContain("📦 Repo-Memory Patch Size Exceeded");
    });

    it("shows patch size exceeded message with front matter example when patch size exceeded", () => {
      const result = buildPushRepoMemoryFailureContext(true, ["default"], "https://example.com/run");
      expect(result).toContain("📦 Repo-Memory Patch Size Exceeded");
      expect(result).toContain("`default`");
      expect(result).toContain("max-patch-size:");
      expect(result).toContain("repo-memory:");
      expect(result).not.toContain("⚠️ Repo-Memory Push Failed");
    });

    it("includes all affected memory IDs in patch size exceeded message", () => {
      const result = buildPushRepoMemoryFailureContext(true, ["default", "secondary"], "https://example.com/run");
      expect(result).toContain("`default`");
      expect(result).toContain("`secondary`");
      expect(result).toContain("id: default");
      expect(result).toContain("id: secondary");
    });

    it("shows yaml front matter snippet for each affected memory ID", () => {
      const result = buildPushRepoMemoryFailureContext(true, ["my-memory"], "https://example.com/run");
      expect(result).toContain("```yaml");
      expect(result).toContain("repo-memory:");
      expect(result).toContain("id: my-memory");
      expect(result).toContain("max-patch-size: 51200");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildAppTokenMintingFailedContext
  // ──────────────────────────────────────────────────────

  describe("buildAppTokenMintingFailedContext", () => {
    let buildAppTokenMintingFailedContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/app_token_minting_failed.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      // Stub readFileSync so the runtime path resolves to the source-tree template
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("app_token_minting_failed.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildAppTokenMintingFailedContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
    });

    it("returns empty string when no failure", () => {
      expect(buildAppTokenMintingFailedContext(false)).toBe("");
    });

    it("returns formatted error message when app token minting failed", () => {
      const result = buildAppTokenMintingFailedContext(true);
      expect(result).toContain("GitHub App Authentication Failed");
      expect(result).toContain("App ID");
      expect(result).toContain("private key");
      expect(result).toContain("installed");
    });

    it("includes actionable remediation steps", () => {
      const result = buildAppTokenMintingFailedContext(true);
      expect(result).toContain("required permissions");
      expect(result).toContain("https://github.github.com/gh-aw/reference/safe-outputs/");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildLockdownCheckFailedContext
  // ──────────────────────────────────────────────────────

  describe("buildLockdownCheckFailedContext", () => {
    let buildLockdownCheckFailedContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/lockdown_check_failed.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      process.env.RUNNER_TEMP = "/nonexistent";
      // Stub readFileSync so the runtime path resolves to the source-tree template
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("lockdown_check_failed.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildLockdownCheckFailedContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
      delete process.env.RUNNER_TEMP;
    });

    it("returns empty string when no failure", () => {
      expect(buildLockdownCheckFailedContext(false)).toBe("");
    });

    it("returns formatted error message when lockdown check failed", () => {
      const result = buildLockdownCheckFailedContext(true);
      expect(result).toContain("Lockdown Check Failed");
    });

    it("includes token configuration guidance", () => {
      const result = buildLockdownCheckFailedContext(true);
      expect(result).toContain("GH_AW_GITHUB_TOKEN");
      expect(result).toContain("gh aw secrets set");
    });

    it("includes strict mode guidance", () => {
      const result = buildLockdownCheckFailedContext(true);
      expect(result).toContain("gh aw compile --strict");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildStaleLockFileFailedContext
  // ──────────────────────────────────────────────────────

  describe("buildStaleLockFileFailedContext", () => {
    let buildStaleLockFileFailedContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/stale_lock_file_failed.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      process.env.RUNNER_TEMP = "/nonexistent";
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("stale_lock_file_failed.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildStaleLockFileFailedContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
      delete process.env.RUNNER_TEMP;
    });

    it("returns empty string when check did not fail", () => {
      expect(buildStaleLockFileFailedContext(false)).toBe("");
    });

    it("returns formatted context when stale lock file check failed", () => {
      const result = buildStaleLockFileFailedContext(true);
      expect(result).toBeTruthy();
      expect(result.length).toBeGreaterThan(0);
    });

    it("includes recompile guidance", () => {
      const result = buildStaleLockFileFailedContext(true);
      expect(result).toContain("gh aw compile");
    });

    it("includes guidance on how to disable the check", () => {
      const result = buildStaleLockFileFailedContext(true);
      expect(result).toContain("stale-check: false");
    });

    it("includes debug logging guidance", () => {
      const result = buildStaleLockFileFailedContext(true);
      expect(result).toContain("[hash-debug]");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildOAuthTokenCheckFailedContext
  // ──────────────────────────────────────────────────────

  describe("buildOAuthTokenCheckFailedContext", () => {
    let buildOAuthTokenCheckFailedContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/oauth_token_check_failed.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      process.env.RUNNER_TEMP = "/nonexistent";
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("oauth_token_check_failed.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildOAuthTokenCheckFailedContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
      delete process.env.RUNNER_TEMP;
    });

    it("returns empty string when check did not fail", () => {
      expect(buildOAuthTokenCheckFailedContext(false, "https://example.com/runs/123")).toBe("");
    });

    it("returns formatted context when OAuth token check failed", () => {
      const result = buildOAuthTokenCheckFailedContext(true, "https://example.com/runs/123");
      expect(result).toBeTruthy();
      expect(result.length).toBeGreaterThan(0);
    });

    it("includes OAuth token detection message", () => {
      const result = buildOAuthTokenCheckFailedContext(true, "https://example.com/runs/123");
      expect(result).toContain("OAuth Token Detected");
    });

    it("includes fine-grained PAT replacement guidance", () => {
      const result = buildOAuthTokenCheckFailedContext(true, "https://example.com/runs/123");
      expect(result).toContain("fine-grained Personal Access Token");
      expect(result).toContain("github_pat_");
    });

    it("includes run URL link", () => {
      const runUrl = "https://github.com/org/repo/actions/runs/99999";
      const result = buildOAuthTokenCheckFailedContext(true, runUrl);
      expect(result).toContain(runUrl);
    });
  });

  // ──────────────────────────────────────────────────────
  // buildTimeoutContext
  // ──────────────────────────────────────────────────────

  describe("buildTimeoutContext", () => {
    let buildTimeoutContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/agent_timeout.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      process.env.RUNNER_TEMP = "/nonexistent";
      // Stub readFileSync so the runtime path resolves to the source-tree template
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("agent_timeout.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildTimeoutContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
      delete process.env.RUNNER_TEMP;
    });

    it("returns empty string when not timed out", () => {
      expect(buildTimeoutContext(false, "20")).toBe("");
      expect(buildTimeoutContext(false, "")).toBe("");
    });

    it("returns formatted error message when timed out", () => {
      const result = buildTimeoutContext(true, "20");
      expect(result).toContain("Agent Timed Out");
      expect(result).toContain("20");
      expect(result).toContain("30");
      expect(result).toContain("timeout-minutes");
    });

    it("uses default of 20 minutes when timeoutMinutes is empty", () => {
      const result = buildTimeoutContext(true, "");
      expect(result).toContain("20");
      expect(result).toContain("30");
    });

    it("suggests current + 10 minutes", () => {
      const result = buildTimeoutContext(true, "45");
      expect(result).toContain("45");
      expect(result).toContain("55");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildOptimizeTokenConsumptionContext
  // ──────────────────────────────────────────────────────

  describe("buildOptimizeTokenConsumptionContext", () => {
    let buildOptimizeTokenConsumptionContext;
    const fs = require("fs");
    const path = require("path");
    const templateContent = fs.readFileSync(path.join(__dirname, "../md/optimize_token_consumption_context.md"), "utf8");
    const originalReadFileSync = fs.readFileSync.bind(fs);

    beforeEach(() => {
      vi.resetModules();
      process.env.RUNNER_TEMP = "/nonexistent";
      // Stub readFileSync so the runtime path resolves to the source-tree template
      fs.readFileSync = (filePath, encoding) => {
        if (typeof filePath === "string" && filePath.includes("optimize_token_consumption_context.md")) {
          return templateContent;
        }
        return originalReadFileSync(filePath, encoding);
      };
      ({ buildOptimizeTokenConsumptionContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.readFileSync = originalReadFileSync;
      delete process.env.RUNNER_TEMP;
    });

    it("returns empty string when no guardrail was triggered", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: false,
        hasDailyAICExceeded: false,
        hasToolDenialsExceeded: false,
        isTimedOut: false,
        runUrl: "https://github.com/owner/repo/actions/runs/123",
      });
      expect(result).toBe("");
    });

    it("returns optimize context when maxAICreditsExceeded is true", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: true,
        hasDailyAICExceeded: false,
        hasToolDenialsExceeded: false,
        isTimedOut: false,
        runUrl: "https://github.com/owner/repo/actions/runs/123",
      });
      expect(result).toContain("Optimize token consumption");
      expect(result).toContain("max-ai-credits");
      expect(result).toContain("https://github.com/owner/repo/actions/runs/123");
      expect(result).toContain("optimize.md");
    });

    it("returns optimize context when hasDailyAICExceeded is true", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: false,
        hasDailyAICExceeded: true,
        hasToolDenialsExceeded: false,
        isTimedOut: false,
        runUrl: "https://github.com/owner/repo/actions/runs/456",
      });
      expect(result).toContain("max-daily-ai-credits");
    });

    it("returns optimize context when hasToolDenialsExceeded is true", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: false,
        hasDailyAICExceeded: false,
        hasToolDenialsExceeded: true,
        isTimedOut: false,
        runUrl: "https://github.com/owner/repo/actions/runs/456",
      });
      expect(result).toContain("max-tool-denials");
    });

    it("returns optimize context when isTimedOut is true", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: false,
        hasDailyAICExceeded: false,
        hasToolDenialsExceeded: false,
        isTimedOut: true,
        runUrl: "https://github.com/owner/repo/actions/runs/789",
      });
      expect(result).toContain("max-turns / timeout");
    });

    it("prefers maxAICreditsExceeded label when multiple guardrails are true", () => {
      const result = buildOptimizeTokenConsumptionContext({
        maxAICreditsExceeded: true,
        hasDailyAICExceeded: true,
        hasToolDenialsExceeded: false,
        isTimedOut: false,
        runUrl: "https://github.com/owner/repo/actions/runs/789",
      });
      expect(result).toContain("max-ai-credits");
    });
  });

  // ──────────────────────────────────────────────────────
  // timeout classification (isTimedOut logic in main)
  // ──────────────────────────────────────────────────────

  describe("timeout classification", () => {
    // Mirrors the classification logic in main():
    //   const isTimedOut = agentConclusion === "timed_out" || agenticEngineTimeout;
    // This ensures step-level timeouts (detected via signal in the engine log)
    // are treated as timeouts even when agentConclusion is "failure".
    function classifyTimeout(agentConclusion, agenticEngineTimeout) {
      return agentConclusion === "timed_out" || agenticEngineTimeout;
    }

    it("detects job-level timeout (agentConclusion === 'timed_out')", () => {
      expect(classifyTimeout("timed_out", false)).toBe(true);
    });

    it("detects step-level timeout (agentConclusion === 'failure' with agenticEngineTimeout)", () => {
      expect(classifyTimeout("failure", true)).toBe(true);
    });

    it("detects timeout when both indicators are present", () => {
      expect(classifyTimeout("timed_out", true)).toBe(true);
    });

    it("does not flag timeout for plain failure without engine timeout signal", () => {
      expect(classifyTimeout("failure", false)).toBe(false);
    });

    it("does not flag timeout for successful completion", () => {
      expect(classifyTimeout("success", false)).toBe(false);
    });

    it("does not flag timeout for cancelled job", () => {
      expect(classifyTimeout("cancelled", false)).toBe(false);
    });
  });

  describe("shouldBuildEngineFailureContext", () => {
    const { shouldBuildEngineFailureContext } = require("./handle_agent_failure.cjs");

    it("returns true for plain failure without timeout or tool-denials-exceeded", () => {
      expect(shouldBuildEngineFailureContext("failure", false, false)).toBe(true);
    });

    it("returns false when timeout is detected", () => {
      expect(shouldBuildEngineFailureContext("failure", false, true)).toBe(false);
    });

    it("returns false when tool-denials-exceeded is present", () => {
      expect(shouldBuildEngineFailureContext("failure", true, false)).toBe(false);
    });

    it("returns false when missing model pricing error is present", () => {
      expect(shouldBuildEngineFailureContext("failure", false, false, true)).toBe(false);
    });

    it("returns false for non-failure conclusions", () => {
      expect(shouldBuildEngineFailureContext("timed_out", false, true)).toBe(false);
      expect(shouldBuildEngineFailureContext("success", false, false)).toBe(false);
    });
  });

  describe("isIssueWritePermissionError", () => {
    const { isIssueWritePermissionError } = require("./handle_agent_failure.cjs");

    it("returns true for 403 Resource not accessible by integration", () => {
      expect(isIssueWritePermissionError({ status: 403, message: "Resource not accessible by integration" })).toBe(true);
    });

    it("returns true for 403 insufficient permissions", () => {
      expect(isIssueWritePermissionError({ status: 403, message: "Insufficient permissions to create issue" })).toBe(true);
    });

    it("returns true for 403 resource not accessible by personal access token", () => {
      expect(isIssueWritePermissionError({ status: 403, message: "Resource not accessible by personal access token" })).toBe(true);
    });

    it("returns false for non-403 errors", () => {
      expect(isIssueWritePermissionError({ status: 500, message: "Internal server error" })).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────
  // buildEngineFailureContext
  // ──────────────────────────────────────────────────────

  describe("buildEngineFailureContext", () => {
    let buildEngineFailureContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let stdioLogPath;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-"));
      stdioLogPath = path.join(tmpDir, "agent-stdio.log");
      process.env.GH_AW_OTEL_JSONL_PATH = path.join(tmpDir, "otel.jsonl");
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "engine_rate_limit_429.md"), ENGINE_RATE_LIMIT_TEMPLATE);
      fs.writeFileSync(path.join(promptsDir, "engine_max_runs_exceeded.md"), ENGINE_MAX_RUNS_EXCEEDED_TEMPLATE);
      fs.writeFileSync(path.join(promptsDir, "max_cache_misses_exceeded.md"), ENGINE_MAX_CACHE_MISSES_EXCEEDED_TEMPLATE);
      fs.writeFileSync(path.join(promptsDir, "shell_expansion_guard_rejected.md"), "SHELL GUARD TEMPLATE");
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_MAX_CACHE_MISSES_EXCEEDED;
      delete process.env.GH_AW_SHELL_EXPANSION_GUARD_REJECTED;
      delete process.env.GH_AW_OTEL_JSONL_PATH;
      delete process.env.RUNNER_TEMP;
      // Clean up temp dir
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when log file does not exist", () => {
      // stdioLogPath not written — file does not exist
      expect(buildEngineFailureContext()).toBe("");
    });

    it("returns empty string when log file is empty", () => {
      fs.writeFileSync(stdioLogPath, "");
      expect(buildEngineFailureContext()).toBe("");
    });

    it("returns empty string when log file contains only whitespace", () => {
      fs.writeFileSync(stdioLogPath, "   \n\n   ");
      expect(buildEngineFailureContext()).toBe("");
    });

    it("detects ERROR: prefix pattern (Codex/generic CLI)", () => {
      fs.writeFileSync(stdioLogPath, "ERROR: quota exceeded\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("quota exceeded");
      expect(result).toContain("Error details:");
      expect(result).toContain("> [!WARNING]");
    });

    it("returns dedicated context for engine 429/rate-limit failures in stdio logs", () => {
      fs.writeFileSync(stdioLogPath, "Failed to get response from the AI model; retried 5 times. Last error: CAPIError: 429 429 Sorry, you've exceeded your rate limit for utility models.\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Rate Limited (HTTP 429)");
      expect(result).toContain("OTLP telemetry");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context for max-runs guardrail failures in stdio logs", () => {
      fs.writeFileSync(stdioLogPath, '[ERROR] API error (attempt 1/11): {"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (50 / 50)."}}\n');
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Max Runs Exceeded");
      expect(result).toContain("max-runs guardrail");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context when only max-runs message text is present", () => {
      fs.writeFileSync(stdioLogPath, "Maximum LLM invocations exceeded (50 / 50).\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Max Runs Exceeded");
      expect(result).toContain("max-runs guardrail");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context for max-cache-misses failures in stdio logs", () => {
      fs.writeFileSync(
        stdioLogPath,
        '2026-07-30T06:14:50.000Z [ERROR] Error in API request: 403 {"error":{"type":"max_cache_misses_exceeded","message":"Maximum consecutive cache misses exceeded (6 / 5).","consecutive_cache_misses":6,"max_cache_misses":5}}\n'
      );
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Cache Miss Limit Exceeded");
      expect(result).toContain("cache misses guardrail");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context when max-cache-misses is only present in structured logs", () => {
      const logDir = path.join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs");
      fs.mkdirSync(logDir, { recursive: true });
      fs.writeFileSync(path.join(logDir, "event-logs.jsonl"), `${JSON.stringify({ type: "max_cache_misses_exceeded", consecutive_cache_misses: 6, max_cache_misses: 5 })}\n`);
      fs.writeFileSync(stdioLogPath, "Agent terminated unexpectedly without clear error details\n");
      process.env.GH_AW_MAX_CACHE_MISSES_EXCEEDED = "true";
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Cache Miss Limit Exceeded");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context for shell expansion guard rejections in stdio logs", () => {
      fs.writeFileSync(stdioLogPath, "Command rejected: shell command contains dangerous patterns that could enable arbitrary code execution. Please rewrite the command without these expansion patterns.\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("SHELL GUARD TEMPLATE");
      expect(result).not.toContain("Last agent output");
    });

    it("returns dedicated context when shell expansion guard rejection is only present in detection output", () => {
      fs.writeFileSync(stdioLogPath, "Agent terminated unexpectedly without clear error details\n");
      const result = buildEngineFailureContext({ shellExpansionGuardRejected: true });
      expect(result).toContain("SHELL GUARD TEMPLATE");
      expect(result).not.toContain("Last agent output");
    });

    it("suppresses engine 429 context when max-ai-credits-exceeded takes precedence", () => {
      fs.writeFileSync(stdioLogPath, "Failed to get response from the AI model; retried 5 times. Last error: CAPIError: 429 429 Sorry, you've exceeded your rate limit for utility models.\n");
      const result = buildEngineFailureContext({ suppressEngineRateLimit429: true });
      expect(result).not.toContain("Engine Rate Limited (HTTP 429)");
      expect(result).toContain("Engine Failure");
    });

    it("returns dedicated context when 429/rate-limit is only present in OTLP mirror", () => {
      fs.writeFileSync(stdioLogPath, "Agent terminated unexpectedly without clear error details\n");
      fs.writeFileSync(
        process.env.GH_AW_OTEL_JSONL_PATH,
        JSON.stringify({
          resourceSpans: [
            {
              scopeSpans: [
                {
                  spans: [
                    {
                      name: "gh-aw.agent.conclusion",
                      status: { code: 2, message: "agent failure: CAPIError: 429 Too Many Requests" },
                    },
                  ],
                },
              ],
            },
          ],
        }) + "\n"
      );
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Rate Limited (HTTP 429)");
      expect(result).toContain("OTLP telemetry");
      expect(result).not.toContain("Last agent output");
    });

    it("strips ::add-mask:: command lines and redacts masked values from the last agent output", () => {
      const logLines = ["   The Docker agent cgroup cannot be passed through.", "token is 79aab19823c27dbc1f3fcd49f16666ca8ab130b4b234b54f286cfcae347a844d", "::add-mask::79aab19823c27dbc1f3fcd49f16666ca8ab130b4b234b54f286cfcae347a844d"];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Last agent output");
      expect(result).not.toContain("::add-mask::");
      expect(result).not.toContain("79aab19823c27dbc1f3fcd49f16666ca8ab130b4b234b54f286cfcae347a844d");
      expect(result).toContain("token is ***");
    });

    it("surfaces the harness terminal error instead of infrastructure noise", () => {
      const logLines = [
        "[WARN] ⚠️  --pids-limit/container.pidsLimit is not supported by this microVM runtime and will be ignored.",
        "   The Docker agent cgroup cannot be passed through, so pids.max/pids.current are unavailable.",
        "[copilot-harness] copilot-sdk: starting headless Copilot CLI server",
        "[copilot-harness] unexpected error: copilot-sdk headless server did not become ready on 127.0.0.1:3002 within 5000ms (connect ETIMEDOUT 127.0.0.1:3002)",
        "[INFO] [cloud-hypervisor] Agent command exited with code 1",
        "[WARN] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Error details:");
      expect(result).toContain("copilot-sdk headless server did not become ready on 127.0.0.1:3002");
      expect(result).not.toContain("Last agent output");
      expect(result).not.toContain("pids.max/pids.current");
    });

    it("filters indented AWF infrastructure continuation lines from the fallback tail", () => {
      const logLines = [
        "[WARN] ⚠️  --pids-limit/container.pidsLimit is not supported by this microVM runtime and will be ignored.",
        "   The Docker agent cgroup cannot be passed through, so pids.max/pids.current are unavailable.",
        "agent produced this final line",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Last agent output");
      expect(result).toContain("agent produced this final line");
      expect(result).not.toContain("pids.max/pids.current");
    });

    it("treats a log of infrastructure lines and their continuations as producing no output", () => {
      const logLines = [
        "[WARN] ⚠️  --pids-limit/container.pidsLimit is not supported by this microVM runtime and will be ignored.",
        "   The Docker agent cgroup cannot be passed through, so pids.max/pids.current are unavailable.",
        "[WARN] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("terminated before producing output");
      expect(result).not.toContain("Last agent output");
    });

    it("redacts masked values from engine error details", () => {
      const logLines = ["::add-mask::sup3rs3cr3t", "Error: authentication failed with token sup3rs3cr3t"];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Error details:");
      expect(result).toContain("authentication failed with token ***");
      expect(result).not.toContain("sup3rs3cr3t");
    });

    it("detects Error: prefix pattern (Node.js style)", () => {
      fs.writeFileSync(stdioLogPath, "Error: connect ECONNREFUSED 127.0.0.1:8080\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("connect ECONNREFUSED 127.0.0.1:8080");
    });

    it("suppresses recovered no-deferred-marker retry errors and surfaces the terminal failure tail", () => {
      const logLines = [
        "[claude-harness] attempt 1: partial execution — will retry with --continue",
        "Error: No deferred tool marker found in the resumed session. Either the session was not deferred, the marker is stale (tool already ran), or it exceeds the tail-scan window. Provide a prompt to continue the conversation.",
        "[claude-harness] attempt 2: no deferred tool marker on --continue — retrying as fresh run (failure_reason=harness_retry_path_invalid, --continue disabled permanently, attempt 3/4)",
        "[claude-harness] attempt 3: spawning: claude --print",
        "2026-08-11T09:12:53.245Z [ERROR] API error (attempt 11/11): undefined Connection error.",
        "[claude-harness] all 3 retries exhausted — giving up (exitCode=1)",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Engine Failure");
      expect(result).toContain("Last agent output");
      expect(result).toContain("Connection error");
      expect(result).not.toContain("No deferred tool marker");
      expect(result).not.toContain("Error details:");
    });

    it("still surfaces unrecovered no-deferred-marker errors", () => {
      fs.writeFileSync(stdioLogPath, "Error: No deferred tool marker found in the resumed session.\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("No deferred tool marker found");
      expect(result).toContain("Error details:");
    });

    it("surfaces a terminal no-deferred-marker error that follows a recovered one", () => {
      const logLines = [
        "Error: No deferred tool marker found in the resumed session.",
        "[claude-harness] attempt 2: no deferred tool marker on --continue — retrying as fresh run (failure_reason=harness_retry_path_invalid, --continue disabled permanently, attempt 3/4)",
        "Error: No deferred tool marker found in the resumed session.",
        "[claude-harness] attempt 3: no deferred tool marker — not retriable via --continue (failure_reason=harness_retry_path_invalid)",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Error details:");
      expect(result).toContain("No deferred tool marker found");
    });

    it("does not treat agent output quoting the recovery phrase as a harness recovery", () => {
      const logLines = ["Error: No deferred tool marker found in the resumed session.", "The log said: no deferred tool marker on --continue — retrying as fresh run (--continue disabled permanently)"];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");

      const result = buildEngineFailureContext();

      expect(result).toContain("Error details:");
      expect(result).toContain("No deferred tool marker found");
    });

    it("extracts AWF startup errors from dependency lines and container startup failures", () => {
      const lines = [
        " Container awf-squid  Error",
        "dependency failed to start: container awf-squid is unhealthy",
        "[ERROR] Failed to start containers: Error: Command failed with exit code 1: docker compose up -d --pull never",
        "  stdout: undefined,",
        "  stderr: undefined,",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("dependency failed to start: container awf-squid is unhealthy");
      expect(result).toContain("Failed to start containers: Error: Command failed with exit code 1: docker compose up -d --pull never");
      expect(result).not.toContain("Last agent output");
      expect(result).not.toContain("stdout: undefined");
      expect(result).not.toContain("stderr: undefined");
    });

    it("detects Fatal: prefix pattern", () => {
      fs.writeFileSync(stdioLogPath, "Fatal: out of memory\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("out of memory");
    });

    it("detects FATAL: prefix pattern", () => {
      fs.writeFileSync(stdioLogPath, "FATAL: unexpected shutdown\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("unexpected shutdown");
    });

    it("detects panic: prefix pattern (Go runtime)", () => {
      fs.writeFileSync(stdioLogPath, "panic: runtime error: index out of range\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("runtime error: index out of range");
    });

    it("detects Reconnecting pattern", () => {
      fs.writeFileSync(stdioLogPath, "Reconnecting... 1/3 (connection lost)\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("connection lost");
    });

    it("deduplicates repeated error messages", () => {
      fs.writeFileSync(stdioLogPath, "ERROR: quota exceeded\nERROR: quota exceeded\nERROR: quota exceeded\n");
      const result = buildEngineFailureContext();
      const count = (result.match(/quota exceeded/g) || []).length;
      expect(count).toBe(1);
    });

    it("collects multiple distinct error messages", () => {
      fs.writeFileSync(stdioLogPath, "ERROR: quota exceeded\nERROR: auth failed\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("quota exceeded");
      expect(result).toContain("auth failed");
    });

    it("falls back to last lines when no known error patterns match", () => {
      const logLines = ["Starting agent...", "Running tool: list_branches", '{"branches": ["main"]}', "Running tool: get_file_contents", "Agent interrupted"];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("Last agent output");
      expect(result).toContain("Agent interrupted");
    });

    it("fallback includes at most 10 non-empty lines", () => {
      const lines = Array.from({ length: 20 }, (_, i) => `line ${i + 1}`);
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("line 20");
      expect(result).toContain("line 11");
      // Lines 1-10 should not appear in the tail
      expect(result).not.toContain("line 10\n");
      expect(result).not.toContain("line 1\n");
    });

    it("fallback ignores empty lines when counting tail", () => {
      const lines = ["line 1", "", "line 2", "", "line 3", "", "", "line 4"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Last agent output");
      expect(result).toContain("line 4");
      expect(result).toContain("line 1");
    });

    it("shows startup-failure message when log contains only AWF infrastructure lines", () => {
      // This is the exact pattern from the Apr 8 systemic failure incident:
      // containers stop cleanly, engine exits with code 1, no substantive output produced.
      const infraLines = [
        " Container awf-squid  Removing",
        " Container awf-squid  Removed",
        "[SUCCESS] Containers stopped successfully",
        "[INFO] Agent session state preserved at: /tmp/awf-agent-session-state-abc123",
        "[INFO] API proxy logs available at: /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs",
        "[ERROR] Command completed with exit code: 1",
        "[WARN] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, infraLines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("terminated before producing output");
      expect(result).toContain("transient infrastructure issue");
      // Infrastructure lines should NOT appear as "Last agent output"
      expect(result).not.toContain("Last agent output");
      expect(result).not.toContain("awf-squid");
      expect(result).not.toContain("Command completed with exit code");
      expect(result).not.toContain("[ERROR]");
      expect(result).not.toContain("Process exiting with code");
    });

    it("filters infrastructure lines from fallback tail when mixed with real agent output", () => {
      // Real agent output followed by AWF infrastructure shutdown lines.
      // Only the real agent output should appear in the fallback.
      const logLines = [
        "Starting agent...",
        "● list_files",
        "  └ Found 12 files",
        " Container awf-squid  Removing",
        " Container awf-squid  Removed",
        "[SUCCESS] Containers stopped successfully",
        "[WARN] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, logLines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Last agent output");
      expect(result).toContain("Starting agent");
      expect(result).toContain("Found 12 files");
      // Infrastructure lines must be excluded from the displayed output
      expect(result).not.toContain("awf-squid");
      expect(result).not.toContain("Command completed with exit code");
      expect(result).not.toContain("Process exiting with code");
    });

    it("includes [entrypoint] and [health-check] infra lines in the infra filter", () => {
      // AWF container scripts emit lowercase [entrypoint] and [health-check] prefixes.
      // The INFRA_LINE_RE pattern is intentionally case-sensitive and matches exactly
      // the casing produced by each AWF component (consistent with parse_copilot_log.cjs).
      const lines = ["[entrypoint] Starting firewall...", "[health-check] Proxy ready", "[INFO] API proxy logs available at: /tmp/gh-aw/logs", "Process exiting with code: 1"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("terminated before producing output");
      // None of the infra lines should appear
      expect(result).not.toContain("entrypoint");
      expect(result).not.toContain("health-check");
      expect(result).not.toContain("API proxy");
    });

    it("includes engine ID in startup-failure message", () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      vi.resetModules();
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
      const infraLines = ["[WARN] Command completed with exit code: 1", "Process exiting with code: 1"];
      fs.writeFileSync(stdioLogPath, infraLines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("`copilot` engine");
      expect(result).toContain("terminated before producing output");
      // Copilot-specific status page guidance
      expect(result).toContain("GitHub Copilot status page");
    });

    it("shows provider-agnostic status page guidance for non-copilot engines", () => {
      process.env.GH_AW_ENGINE_ID = "claude";
      vi.resetModules();
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
      const infraLines = ["[WARN] Command completed with exit code: 1", "Process exiting with code: 1"];
      fs.writeFileSync(stdioLogPath, infraLines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("`claude` engine");
      expect(result).toContain("terminated before producing output");
      // Generic guidance for non-copilot engines
      expect(result).toContain("provider status page");
      expect(result).not.toContain("GitHub Copilot status page");
    });

    it("includes engine ID in failure message when GH_AW_ENGINE_ID is set", () => {
      process.env.GH_AW_ENGINE_ID = "copilot";
      vi.resetModules();
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
      fs.writeFileSync(stdioLogPath, "ERROR: quota exceeded\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("`copilot` engine");
    });

    it("includes engine ID in fallback message when GH_AW_ENGINE_ID is set", () => {
      process.env.GH_AW_ENGINE_ID = "claude";
      vi.resetModules();
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
      fs.writeFileSync(stdioLogPath, "Agent did something unexpected\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("`claude` engine");
    });

    it("uses generic 'AI engine' label when GH_AW_ENGINE_ID is not set", () => {
      fs.writeFileSync(stdioLogPath, "ERROR: connection reset\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("The AI engine");
    });

    it("returns dedicated cyber_policy_violation message when template exists", () => {
      const templateContent = "**OpenAI Cyber Policy Violation**: The Codex engine was blocked by OpenAI's safety policy.";
      fs.writeFileSync(path.join(promptsDir, "cyber_policy_violation.md"), templateContent);
      fs.writeFileSync(stdioLogPath, "ERROR: cyber_policy_violation\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Cyber Policy Violation");
      expect(result).not.toContain("Engine Failure");
      expect(result).not.toContain("cyber_policy_violation");
    });

    it("falls back to generic message when cyber_policy_violation template is missing", () => {
      // No template file written — promptsDir exists but template is absent
      fs.writeFileSync(stdioLogPath, "ERROR: cyber_policy_violation\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("cyber_policy_violation");
    });

    it("returns dedicated message when cyber_policy_violation appears among multiple errors", () => {
      const templateContent = "**OpenAI Cyber Policy Violation**: The Codex engine was blocked by OpenAI's safety policy.";
      fs.writeFileSync(path.join(promptsDir, "cyber_policy_violation.md"), templateContent);
      fs.writeFileSync(stdioLogPath, "ERROR: connection reset\nERROR: cyber_policy_violation\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Cyber Policy Violation");
      expect(result).not.toContain("Engine Failure");
    });

    it("detects AWF firewall startup failure with DNS failure (EAI_AGAIN)", () => {
      // Representative log from run 28172712935: cli-proxy cannot resolve awmg-cli-proxy DNS
      const lines = [
        "[INFO] CLI proxy sidecar enabled - connecting to external DIFC proxy at awmg-cli-proxy:18443",
        " Container awf-cli-proxy  Creating",
        " Container awf-cli-proxy  Error",
        "dependency failed to start: container awf-cli-proxy is unhealthy",
        "[ERROR] awf-cli-proxy container logs (last 50 lines):",
        "[cli-proxy] DIFC proxy probe failed (attempt 1/10, diagnosis=not-yet-ready (ECONNREFUSED)), retrying in 1s...",
        "[cli-proxy] DIFC proxy probe failed (attempt 2/10, diagnosis=unknown), retrying in 2s...",
        "[tcp-tunnel] Upstream error (::1:48576): getaddrinfo EAI_AGAIN awmg-cli-proxy",
        "[ERROR] Fatal error: Error: AWF firewall failed to start: awf-cli-proxy could not connect to the external DIFC proxy",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("AWF Firewall Startup Failure");
      expect(result).toContain("EAI_AGAIN");
      expect(result).toContain("awmg-cli-proxy");
      expect(result).toContain("dependency failed to start: container awf-cli-proxy is unhealthy");
      expect(result).toContain("https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md");
      expect(result).not.toContain("Last agent output");
      expect(result).not.toContain("Engine Failure");
    });

    it("detects AWF firewall startup failure without DNS failure", () => {
      const lines = [" Container awf-cli-proxy  Error", "dependency failed to start: container awf-cli-proxy is unhealthy", "[ERROR] Fatal error: Error: AWF firewall failed to start: awf-cli-proxy could not connect"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("AWF Firewall Startup Failure");
      expect(result).toContain("dependency failed to start: container awf-cli-proxy is unhealthy");
      expect(result).toContain("https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md");
      expect(result).not.toContain("EAI_AGAIN");
      expect(result).not.toContain("Diagnosis:");
    });

    it("detects AWF firewall startup failure with diagnosis=unknown but no EAI_AGAIN", () => {
      // When only diagnosis=unknown triggers the DNS signal (no EAI_AGAIN in log),
      // the diagnosis message should NOT claim EAI_AGAIN as the cause.
      const lines = [
        "[INFO] CLI proxy sidecar enabled - connecting to external DIFC proxy at awmg-cli-proxy:18443",
        " Container awf-cli-proxy  Error",
        "dependency failed to start: container awf-cli-proxy is unhealthy",
        "[cli-proxy] DIFC proxy probe failed (attempt 1/10, diagnosis=unknown), retrying in 1s...",
        "[ERROR] Fatal error: Error: AWF firewall failed to start: awf-cli-proxy could not connect to the external DIFC proxy",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("AWF Firewall Startup Failure");
      expect(result).toContain("Diagnosis:");
      expect(result).toContain("diagnosis=unknown");
      expect(result).not.toContain("EAI_AGAIN");
      expect(result).toContain("https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md");
    });

    it("detects AWF firewall startup failure from infra-only log with specific failure signal", () => {
      // When the log contains only infra lines but includes a specific AWF firewall failure signal.
      // Note: container lifecycle lines like " Container awf-cli-proxy  Started" are NOT enough —
      // the detection requires a specific failure pattern ("AWF firewall failed to start" or
      // "dependency failed to start: container awf-cli-proxy").
      const lines = [
        "[INFO] CLI proxy sidecar enabled - connecting to external DIFC proxy at awmg-cli-proxy:18443",
        "[ERROR] Fatal error: Error: AWF firewall failed to start: awf-cli-proxy could not connect",
        "[ERROR] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("AWF Firewall Startup Failure");
      expect(result).toContain("https://github.com/github/gh-aw-firewall/blob/main/docs/diagnosing-awf-failures.md");
      expect(result).not.toContain("transient infrastructure issue");
      expect(result).not.toContain("Engine Failure");
    });

    it("does not trigger AWF firewall detection for infra-only log with container lifecycle lines only", () => {
      // Container lifecycle lines mentioning awf-cli-proxy appear on successful runs too
      // and must NOT trigger AWF firewall startup failure detection.
      const lines = [
        " Container awf-cli-proxy  Starting",
        " Container awf-cli-proxy  Started",
        "[INFO] CLI proxy sidecar enabled - connecting to external DIFC proxy at awmg-cli-proxy:18443",
        "[ERROR] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      // Should fall through to the generic "transient infrastructure" message
      expect(result).toContain("Engine Failure");
      expect(result).toContain("transient infrastructure issue");
      expect(result).not.toContain("AWF Firewall Startup Failure");
      expect(result).not.toContain("diagnosing-awf-failures");
    });

    it("does not trigger AWF firewall detection for unrelated awf-squid dependency failures", () => {
      // awf-squid is a different AWF component — should NOT trigger cli-proxy detection
      const lines = [" Container awf-squid  Error", "dependency failed to start: container awf-squid is unhealthy", "[ERROR] Failed to start containers: Error: Command failed with exit code 1: docker compose up -d --pull never"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("dependency failed to start: container awf-squid is unhealthy");
      // Should NOT show AWF firewall startup failure message (awf-squid is not awf-cli-proxy)
      expect(result).not.toContain("AWF Firewall Startup Failure");
      expect(result).not.toContain("diagnosing-awf-failures");
    });
  });

  // ──────────────────────────────────────────────────────
  // detectAWFFirewallStartupFailureFromLog
  // ──────────────────────────────────────────────────────

  describe("detectAWFFirewallStartupFailureFromLog", () => {
    let detectAWFFirewallStartupFailureFromLog;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let stdioLogPath;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-"));
      stdioLogPath = path.join(tmpDir, "agent-stdio.log");
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      process.env.RUNNER_TEMP = tmpDir;
      ({ detectAWFFirewallStartupFailureFromLog } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns false when log file does not exist", () => {
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(false);
    });

    it("returns false for unrelated agent failures", () => {
      fs.writeFileSync(stdioLogPath, "ERROR: quota exceeded\n");
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(false);
    });

    it("returns true when log contains AWF firewall failed to start message", () => {
      fs.writeFileSync(stdioLogPath, "[ERROR] Fatal error: Error: AWF firewall failed to start: awf-cli-proxy could not connect\n");
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(true);
    });

    it("returns true when log contains awf-cli-proxy reference", () => {
      fs.writeFileSync(stdioLogPath, "dependency failed to start: container awf-cli-proxy is unhealthy\n");
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(true);
    });

    it("returns false for container lifecycle lines mentioning awf-cli-proxy (false-positive guard)", () => {
      // Container lifecycle lines appear on successful runs and must not trigger detection
      const lines = [" Container awf-cli-proxy  Starting", " Container awf-cli-proxy  Started", "[INFO] CLI proxy sidecar enabled - connecting to external DIFC proxy at awmg-cli-proxy:18443"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(false);
    });

    it("returns false when log references other awf containers (e.g., awf-squid)", () => {
      fs.writeFileSync(stdioLogPath, "dependency failed to start: container awf-squid is unhealthy\n");
      expect(detectAWFFirewallStartupFailureFromLog()).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────
  // hasEngineMaxRunsExceededSignal
  // ──────────────────────────────────────────────────────

  describe("hasEngineMaxRunsExceededSignal", () => {
    let hasEngineMaxRunsExceededSignal;

    beforeEach(() => {
      vi.resetModules();
      ({ hasEngineMaxRunsExceededSignal } = require("./handle_agent_failure.cjs"));
    });

    it("returns false for empty-like content", () => {
      expect(hasEngineMaxRunsExceededSignal("")).toBe(false);
      expect(hasEngineMaxRunsExceededSignal(null)).toBe(false);
      expect(hasEngineMaxRunsExceededSignal(undefined)).toBe(false);
    });

    it("returns true when max_runs_exceeded marker is present", () => {
      expect(hasEngineMaxRunsExceededSignal('{"error":{"type":"max_runs_exceeded"}}')).toBe(true);
    });

    it("returns true when Maximum LLM invocations exceeded text is present", () => {
      expect(hasEngineMaxRunsExceededSignal("Maximum LLM invocations exceeded (50 / 50).")).toBe(true);
    });

    it("returns false for unrelated content", () => {
      expect(hasEngineMaxRunsExceededSignal("request failed for unrelated reason")).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────
  // buildEngineMaxRunsExceededContext
  // ──────────────────────────────────────────────────────

  describe("buildEngineMaxRunsExceededContext", () => {
    let buildEngineMaxRunsExceededContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-engine-max-runs-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildEngineMaxRunsExceededContext } = require("./handle_agent_failure.cjs"));
      fs.writeFileSync(path.join(promptsDir, "engine_max_runs_exceeded.md"), ENGINE_MAX_RUNS_EXCEEDED_TEMPLATE);
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("renders template content for a provided engine label", () => {
      const result = buildEngineMaxRunsExceededContext("Claude");
      expect(result).toContain("Engine Max Runs Exceeded");
      expect(result).toContain("max-runs guardrail");
      expect(result).toContain("Claude");
    });

    it("falls back to AI when engine label is empty or whitespace", () => {
      expect(buildEngineMaxRunsExceededContext("")).toContain("AI");
      expect(buildEngineMaxRunsExceededContext("   ")).toContain("AI");
    });

    it("trims leading/trailing whitespace from engine label", () => {
      const result = buildEngineMaxRunsExceededContext("  copilot  ");
      expect(result).toContain("copilot");
      expect(result).not.toContain("  copilot  ");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildEngineRateLimit429Context
  // ──────────────────────────────────────────────────────

  describe("buildEngineRateLimit429Context", () => {
    let buildEngineRateLimit429Context;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-engine-rate-limit-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildEngineRateLimit429Context } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("renders template content when engine rate-limit template exists", () => {
      fs.writeFileSync(path.join(promptsDir, "engine_rate_limit_429.md"), ENGINE_RATE_LIMIT_TEMPLATE);
      const result = buildEngineRateLimit429Context("Copilot");
      expect(result).toContain("Engine Rate Limited (HTTP 429)");
      expect(result).toContain("Copilot");
      expect(result).toContain("OTLP telemetry");
    });

    it("throws when engine rate-limit template is missing", () => {
      expect(() => buildEngineRateLimit429Context("Copilot")).toThrow(/ENOENT|no such file/i);
    });
  });

  // ──────────────────────────────────────────────────────
  // detectEngineRateLimit429Failure
  // ──────────────────────────────────────────────────────

  describe("detectEngineRateLimit429Failure", () => {
    let detectEngineRateLimit429Failure;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let stdioLogPath;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-detect-429-"));
      stdioLogPath = path.join(tmpDir, "agent-stdio.log");
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      ({ detectEngineRateLimit429Failure } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.GH_AW_OTEL_JSONL_PATH;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns true when stdio log contains a 429 rate-limit signal", () => {
      fs.writeFileSync(stdioLogPath, "Failed to get response from the AI model; retried 5 times. Last error: CAPIError: 429 429 Sorry, you've exceeded your rate limit.\n");
      expect(detectEngineRateLimit429Failure()).toBe(true);
    });

    it("returns false when stdio log contains terminal_reason: completed even with a 429 signal", () => {
      fs.writeFileSync(stdioLogPath, 'Failed to get response; CAPIError: 429 rate limit\n{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":10}\n');
      expect(detectEngineRateLimit429Failure()).toBe(false);
    });

    it("returns false when stdio log contains terminal_reason: completed and no 429 signal", () => {
      fs.writeFileSync(stdioLogPath, '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":5}\n');
      expect(detectEngineRateLimit429Failure()).toBe(false);
    });

    it("returns false when stdio log has no 429 signal and OTLP mirror is absent", () => {
      fs.writeFileSync(stdioLogPath, "Agent exited normally.\n");
      expect(detectEngineRateLimit429Failure()).toBe(false);
    });

    it("returns false when log file does not exist and OTLP mirror is absent", () => {
      expect(detectEngineRateLimit429Failure()).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────
  // hasEngineMaxCacheMissesExceededSignal
  // ──────────────────────────────────────────────────────

  describe("hasEngineMaxCacheMissesExceededSignal", () => {
    let hasEngineMaxCacheMissesExceededSignal;

    beforeEach(() => {
      vi.resetModules();
      ({ hasEngineMaxCacheMissesExceededSignal } = require("./handle_agent_failure.cjs"));
    });

    it("returns false for empty-like content", () => {
      expect(hasEngineMaxCacheMissesExceededSignal("")).toBe(false);
      expect(hasEngineMaxCacheMissesExceededSignal(null)).toBe(false);
      expect(hasEngineMaxCacheMissesExceededSignal(undefined)).toBe(false);
    });

    it("returns true when max_cache_misses_exceeded marker is present", () => {
      expect(hasEngineMaxCacheMissesExceededSignal('{"error":{"type":"max_cache_misses_exceeded"}}')).toBe(true);
    });

    it("returns true when Maximum consecutive cache misses exceeded text is present", () => {
      expect(hasEngineMaxCacheMissesExceededSignal("Maximum consecutive cache misses exceeded (6 / 5).")).toBe(true);
    });

    it("returns true for the exact error seen in production logs", () => {
      const logLine =
        '2026-07-30T06:14:50.000Z [ERROR] Error in API request: 403 {"error":{"type":"max_cache_misses_exceeded","message":"Maximum consecutive cache misses exceeded (6 / 5).","consecutive_cache_misses":6,"max_cache_misses":5}}';
      expect(hasEngineMaxCacheMissesExceededSignal(logLine)).toBe(true);
    });

    it("returns false for unrelated content", () => {
      expect(hasEngineMaxCacheMissesExceededSignal("request failed for unrelated reason")).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────
  // buildEngineMaxCacheMissesExceededContext
  // ──────────────────────────────────────────────────────

  describe("buildEngineMaxCacheMissesExceededContext", () => {
    let buildEngineMaxCacheMissesExceededContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-cache-misses-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildEngineMaxCacheMissesExceededContext } = require("./handle_agent_failure.cjs"));
      fs.writeFileSync(path.join(promptsDir, "max_cache_misses_exceeded.md"), ENGINE_MAX_CACHE_MISSES_EXCEEDED_TEMPLATE);
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("renders template content for a provided engine label", () => {
      const result = buildEngineMaxCacheMissesExceededContext("claude");
      expect(result).toContain("Engine Cache Miss Limit Exceeded");
      expect(result).toContain("cache misses guardrail");
      expect(result).toContain("claude");
    });

    it("falls back to AI when engine label is empty or whitespace", () => {
      expect(buildEngineMaxCacheMissesExceededContext("")).toContain("AI");
      expect(buildEngineMaxCacheMissesExceededContext("   ")).toContain("AI");
    });

    it("trims leading/trailing whitespace from engine label", () => {
      const result = buildEngineMaxCacheMissesExceededContext("  claude  ");
      expect(result).toContain("claude");
      expect(result).not.toContain("  claude  ");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildMCPPolicyErrorContext
  // ──────────────────────────────────────────────────────

  describe("buildMCPPolicyErrorContext", () => {
    let buildMCPPolicyErrorContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-mcp-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildMCPPolicyErrorContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when no MCP policy error", () => {
      expect(buildMCPPolicyErrorContext(false)).toBe("");
    });

    it("returns template content when MCP policy error and template exists", () => {
      const templateContent = "\n**🔒 MCP Servers Blocked by Policy**: Test message.\n";
      fs.writeFileSync(path.join(promptsDir, "mcp_policy_error.md"), templateContent);
      const result = buildMCPPolicyErrorContext(true);
      expect(result).toContain("MCP Servers Blocked by Policy");
    });

    it("includes link to official documentation when template exists", () => {
      const templateContent = "**🔒 MCP Servers Blocked by Policy**: See [docs](https://docs.github.com/en/copilot/how-tos/administer-copilot/manage-mcp-usage/configure-mcp-server-access).\n";
      fs.writeFileSync(path.join(promptsDir, "mcp_policy_error.md"), templateContent);
      const result = buildMCPPolicyErrorContext(true);
      expect(result).toContain("docs.github.com/en/copilot/how-tos/administer-copilot/manage-mcp-usage/configure-mcp-server-access");
    });

    it("throws when template is missing", () => {
      expect(() => buildMCPPolicyErrorContext(true)).toThrow(/ENOENT|no such file/i);
    });
  });

  // buildModelNotSupportedErrorContext
  // ──────────────────────────────────────────────────────

  describe("buildModelNotSupportedErrorContext", () => {
    let buildModelNotSupportedErrorContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-model-not-supported-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildModelNotSupportedErrorContext } = require("./handle_agent_failure.cjs"));
    });

    describe("buildHTTP400ResponseErrorContext", () => {
      let buildHTTP400ResponseErrorContext;
      const fs = require("fs");
      const path = require("path");
      const os = require("os");

      /** @type {string} */
      let tmpDir;

      /** @type {string} */
      let promptsDir;

      beforeEach(() => {
        vi.resetModules();
        tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-http-400-"));
        promptsDir = path.join(tmpDir, "gh-aw", "prompts");
        fs.mkdirSync(promptsDir, { recursive: true });
        process.env.RUNNER_TEMP = tmpDir;
        ({ buildHTTP400ResponseErrorContext } = require("./handle_agent_failure.cjs"));
      });

      afterEach(() => {
        delete process.env.RUNNER_TEMP;
        if (fs.existsSync(tmpDir)) {
          fs.rmSync(tmpDir, { recursive: true, force: true });
        }
      });

      it("returns empty string when no HTTP 400 response error", () => {
        expect(buildHTTP400ResponseErrorContext(false)).toBe("");
      });

      it("returns template content when HTTP 400 response error and template exists", () => {
        const templateContent = "\n**HTTP 400 Bad Request from Copilot**: Test message.\n";
        fs.writeFileSync(path.join(promptsDir, "http_400_response_error.md"), templateContent);
        const result = buildHTTP400ResponseErrorContext(true);
        expect(result).toContain("HTTP 400 Bad Request from Copilot");
      });

      it("throws when template is missing", () => {
        expect(() => buildHTTP400ResponseErrorContext(true)).toThrow(/ENOENT|no such file/i);
      });
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when no model-not-supported error", () => {
      expect(buildModelNotSupportedErrorContext(false)).toBe("");
    });

    it("returns template content when model-not-supported error and template exists", () => {
      const templateContent = "\n**🚫 Model Not Supported**: Test message.\n";
      fs.writeFileSync(path.join(promptsDir, "model_not_supported_error.md"), templateContent);
      const result = buildModelNotSupportedErrorContext(true);
      expect(result).toContain("Model Not Supported");
    });

    it("throws when template is missing", () => {
      expect(() => buildModelNotSupportedErrorContext(true)).toThrow(/ENOENT|no such file/i);
    });
  });

  // buildUnknownModelAICreditsContext
  // ──────────────────────────────────────────────────────

  describe("buildUnknownModelAICreditsContext", () => {
    let buildUnknownModelAICreditsContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-unknown-model-aic-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildUnknownModelAICreditsContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when no unknown_model_ai_credits error", () => {
      expect(buildUnknownModelAICreditsContext(false)).toBe("");
    });

    it("returns template content when error is true and template exists", () => {
      const templateContent = "\n> [!WARNING]\n> **Unknown Model for AI Credits Pricing**: Test message.\n";
      fs.writeFileSync(path.join(promptsDir, "unknown_model_ai_credits.md"), templateContent);
      const result = buildUnknownModelAICreditsContext(true);
      expect(result).toContain("Unknown Model for AI Credits Pricing");
    });

    it("throws when template is missing", () => {
      expect(() => buildUnknownModelAICreditsContext(true)).toThrow(/ENOENT|no such file/i);
    });
  });
  // ──────────────────────────────────────────────────────

  describe("models.dev pricing helpers", () => {
    it("prefers the inferred provider match before cross-provider fallback", async () => {
      const https = require("https");
      const { EventEmitter } = require("events");
      vi.spyOn(https, "get").mockImplementation((url, callback) => {
        expect(url).toBe("https://models.dev/catalog.json");
        const req = new EventEmitter();
        req.destroy = vi.fn();
        process.nextTick(() => {
          const res = new EventEmitter();
          res.statusCode = 200;
          callback(res);
          res.emit(
            "data",
            Buffer.from(
              JSON.stringify({
                providers: {
                  openai: { models: { "shared-model": { cost: { input: 1, output: 2 } } } },
                  anthropic: { models: { "shared-model": { cost: { input: 3, output: 4 } } } },
                },
              })
            )
          );
          res.emit("end");
          req.emit("close");
        });
        return req;
      });

      await expect(fetchModelPricingFromModelsDev("shared-model", "anthropic")).resolves.toEqual({ input: 3, output: 4 });
    });

    it("quotes model names when building frontmatter pricing snippets", () => {
      const snippet = buildModelPricingFrontmatterSnippet("model: alias", "claude", { input: 15, output: 75 });
      expect(snippet).toContain("anthropic:");
      expect(snippet).toContain("'model: alias':");
    });
  });

  describe("buildMissingModelPricingContext", () => {
    let buildMissingModelPricingContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-missing-pricing-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildMissingModelPricingContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when hasMissingModelPricingError is false", async () => {
      const result = await buildMissingModelPricingContext(false, "claude-opus-5", "claude");
      expect(result).toBe("");
    });

    it("returns template content with model name substituted when template exists", async () => {
      const templateContent = "> [!WARNING]\n> **Missing AI Credits Pricing for model `{model_name}`**: Test message.\n{pricing_snippet}";
      fs.writeFileSync(path.join(promptsDir, "missing_model_pricing.md"), templateContent);
      const result = await buildMissingModelPricingContext(true, "claude-opus-5", "claude");
      expect(result).toContain("claude-opus-5");
    });

    it("renders the manual pricing fallback when no pricing snippet is available", async () => {
      const https = require("https");
      const { EventEmitter } = require("events");
      vi.spyOn(https, "get").mockImplementation((url, callback) => {
        const req = new EventEmitter();
        req.destroy = vi.fn();
        process.nextTick(() => {
          const res = new EventEmitter();
          res.statusCode = 200;
          callback(res);
          res.emit("data", Buffer.from(JSON.stringify({ providers: {} })));
          res.emit("end");
          req.emit("close");
        });
        return req;
      });
      fs.writeFileSync(path.join(promptsDir, "missing_model_pricing.md"), "manual snippet:\n{pricing_snippet}\noption2 key {model_name_yaml_key}");
      const result = await buildMissingModelPricingContext(true, "model: alias", "claude");
      expect(result).toContain("manual snippet:");
      expect(result).toContain("anthropic:");
      expect(result).toContain("'model: alias':");
      expect(result).toContain('input: "0e0"');
      expect(result).toContain('cache_read: "0e0"');
      expect(result).toContain('cache_write: "0e0"');
      expect(result).toContain("Placeholder values");
    });

    it("throws when template is missing and error is true", async () => {
      await expect(buildMissingModelPricingContext(true, "claude-opus-5", "claude")).rejects.toThrow(/ENOENT|no such file/i);
    });
  });
  // ──────────────────────────────────────────────────────

  describe("resolveCacheMemoryRestored", () => {
    let resolveCacheMemoryRestored;

    beforeEach(() => {
      vi.resetModules();
      ({ resolveCacheMemoryRestored } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      for (const key of Object.keys(process.env)) {
        if (key.startsWith("GH_AW_CACHE_MEMORY_RESTORE_")) {
          delete process.env[key];
        }
      }
    });

    it("returns false when restore signals are absent", () => {
      expect(resolveCacheMemoryRestored()).toBe(false);
    });

    it("returns true when matched key exists", () => {
      process.env.GH_AW_CACHE_MEMORY_RESTORE_0_MATCHED_KEY = "memory-none-default";
      expect(resolveCacheMemoryRestored()).toBe(true);
    });

    it("returns true when cache-hit is true", () => {
      process.env.GH_AW_CACHE_MEMORY_RESTORE_1_CACHE_HIT = "true";
      expect(resolveCacheMemoryRestored()).toBe(true);
    });
  });

  describe("buildMissingDataContext", () => {
    let buildMissingDataContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-missing-data-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_AGENT_OUTPUT;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when agent output file does not exist", () => {
      expect(buildMissingDataContext(false, false)).toBe("");
      expect(buildMissingDataContext(true, false)).toBe("");
    });

    it("returns empty string when agent output has no missing_data items", () => {
      fs.writeFileSync(path.join(tmpDir, "agent_output.json"), JSON.stringify({ items: [{ type: "noop", reason: "done" }] }));
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      expect(buildMissingDataContext(false, false)).toBe("");
      expect(buildMissingDataContext(true, false)).toBe("");
    });

    it("returns missing data context without cache warning when cacheMemoryEnabled is false", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_data", data_type: "cache_memory", reason: "cache_memory_miss" }],
        })
      );
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(false, false);
      expect(result).toContain("Missing Data Reported");
      expect(result).toContain("cache\\_memory"); // data_type after markdown escaping
      expect(result).not.toContain("Cache Configuration Problem");
    });

    it("appends cache configuration warning when cache restore matched and cache_memory_miss item present", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_data", data_type: "cache_memory", reason: "cache_memory_miss" }],
        })
      );
      const templateContent =
        "> [!WARNING]\n" +
        "> <details>\n" +
        "> <summary>Cache Configuration Problem: cache miss detected after cache restore succeeded.</summary>\n>\n" +
        "> Review the [cache-memory configuration](https://github.github.com/gh-aw/reference/cache-memory/) and ensure the agent prompt correctly references files inside the cache directory.\n>\n" +
        "> **File naming convention:** Cache files are stored at `/tmp/gh-aw/cache-memory/`.\n>\n" +
        "> </details>";
      fs.writeFileSync(path.join(promptsDir, "cache_memory_miss.md"), templateContent);
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(true, true);
      expect(result).not.toContain("Missing Data Reported");
      expect(result).toContain("Cache Configuration Problem");
      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("<summary>");
      expect(result).toContain("<details>");
      expect(result).toContain("/gh-aw/reference/cache-memory/");
      expect(result).toContain("File naming convention");
    });

    it("captures reason-only missing_data items (no data_type) and detects cache miss", () => {
      // Agents may emit missing_data with only reason (no data_type) — ensure it is still captured
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_data", reason: "cache_memory_miss" }],
        })
      );
      const templateContent = "> [!WARNING]\n" + "> <details>\n" + "> <summary>Cache Configuration Problem: cache miss detected after cache restore succeeded.</summary>\n>\n" + "> Details here.\n>\n" + "> </details>";
      fs.writeFileSync(path.join(promptsDir, "cache_memory_miss.md"), templateContent);
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(true, true);
      expect(result).not.toContain("Missing Data Reported");
      expect(result).toContain("Cache Configuration Problem");
      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("<summary>");
      expect(result).toContain("<details>");
    });

    it("does not append cache configuration warning when cache restore did not match", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_data", data_type: "cache_memory", reason: "cache_memory_miss" }],
        })
      );
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(true, false);
      expect(result).toContain("Missing Data Reported");
      expect(result).toContain("cache\\_memory");
      expect(result).not.toContain("Cache Configuration Problem");
    });

    it("shows generic missing-data context for non-cache items while still appending cache warning", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "missing_data", data_type: "cache_memory", reason: "cache_memory_miss" },
            { type: "missing_data", data_type: "user_data", reason: "not_provided" },
          ],
        })
      );
      const templateContent = "> [!WARNING]\n> <details>\n> <summary>Cache Configuration Problem: cache miss detected after cache restore succeeded.</summary>\n>\n> Details here.\n>\n> </details>";
      fs.writeFileSync(path.join(promptsDir, "cache_memory_miss.md"), templateContent);
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(true, true);
      expect(result).toContain("Missing Data Reported");
      expect(result).toContain("user\\_data");
      expect(result).toContain("Cache Configuration Problem");
      expect(result).not.toContain("cache\\_memory");
      expect(result).not.toContain("cache\\_memory\\_miss");
    });

    it("keeps cache warning and report_incomplete context without duplicate missing-data section", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "missing_data", data_type: "cache_memory", reason: "cache_memory_miss" },
            { type: "report_incomplete", reason: "mcp_crash" },
          ],
        })
      );
      const templateContent = "> [!WARNING]\n> <details>\n> <summary>Cache Configuration Problem: cache miss detected after cache restore succeeded.</summary>\n>\n> Details here.\n>\n> </details>";
      fs.writeFileSync(path.join(promptsDir, "cache_memory_miss.md"), templateContent);
      vi.resetModules();
      const { buildMissingDataContext: buildMissingDataContextFn, buildReportIncompleteContext } = require("./handle_agent_failure.cjs");
      const missingDataResult = buildMissingDataContextFn(true, true);
      const reportIncompleteResult = buildReportIncompleteContext();
      expect(missingDataResult).not.toContain("Missing Data Reported");
      expect(missingDataResult).toContain("Cache Configuration Problem");
      expect(reportIncompleteResult).toContain("Task Could Not Be Completed");
      expect(reportIncompleteResult).toContain("mcp_crash");
    });

    it("does not append cache warning for unrelated missing_data reasons when cacheMemoryEnabled is true", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_data", data_type: "user_data", reason: "not_provided" }],
        })
      );
      vi.resetModules();
      ({ buildMissingDataContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingDataContext(true, true);
      expect(result).toContain("Missing Data Reported");
      expect(result).not.toContain("Cache Configuration Problem");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildMissingToolContext
  // ──────────────────────────────────────────────────────

  describe("buildMissingToolContext", () => {
    let buildMissingToolContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-missing-tool-"));
      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_AGENT_OUTPUT;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when agent output file does not exist", () => {
      expect(buildMissingToolContext()).toBe("");
    });

    it("returns empty string when agent output has no missing_tool items", () => {
      fs.writeFileSync(path.join(tmpDir, "agent_output.json"), JSON.stringify({ items: [{ type: "noop", reason: "done" }] }));
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      expect(buildMissingToolContext()).toBe("");
    });

    it("returns missing tool context with tool name and reason", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "bash", reason: "bash is not available" }],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
      expect(result).toContain("bash");
      expect(result).toContain("bash is not available");
      expect(result).toContain("> [!WARNING]");
      expect(result).not.toContain("**⚠️ Missing Tools Reported**");
    });

    it("returns missing tool context for tool with alternatives", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "docker", reason: "docker is not installed", alternatives: "podman" }],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
      expect(result).toContain("docker");
      expect(result).toContain("podman");
    });

    it("skips missing_tool items without a reason field", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "bash" }],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      expect(buildMissingToolContext()).toBe("");
    });

    it("handles multiple missing_tool items", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "missing_tool", tool: "tool1", reason: "not available" },
            { type: "missing_tool", tool: "tool2", reason: "not installed" },
          ],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
      expect(result).toContain("tool1");
      expect(result).toContain("tool2");
    });

    it("suppresses generic context for repeated permission denied missing_tool entries", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1"] }],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      expect(buildMissingToolContext()).toBe("");
    });

    it("does not suppress tool/permission entry when denied_commands is empty", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "tool/permission", reason: "permission queried", denied_commands: [] }],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
      expect(result).toContain("tool/permission");
    });

    it("keeps non-permission missing tools when permission-denied entries are present", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1"] },
            { type: "missing_tool", tool: "bash", reason: "bash is not available" },
          ],
        })
      );
      vi.resetModules();
      ({ buildMissingToolContext } = require("./handle_agent_failure.cjs"));
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
      expect(result).toContain("bash");
      expect(result).not.toContain("tool/permission");
    });
  });

  // ──────────────────────────────────────────────────────
  // buildPermissionDeniedContext
  // ──────────────────────────────────────────────────────

  describe("buildPermissionDeniedContext", () => {
    let buildPermissionDeniedContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;

    function writePermissionDeniedTemplate() {
      fs.writeFileSync(path.join(promptsDir, "permission_denied_context.md"), "> [!WARNING]\n> **Repeated Permission Denied**\n\n**Denied Commands:**\n{denied_commands_list}\n\nworkflow: {workflow_id}\n");
    }

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-permission-denied-"));
      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      ({ buildPermissionDeniedContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_AGENT_OUTPUT;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when agent output file does not exist", () => {
      expect(buildPermissionDeniedContext()).toBe("");
    });

    it("returns empty string when there are no tool/permission items", () => {
      fs.writeFileSync(path.join(tmpDir, "agent_output.json"), JSON.stringify({ items: [{ type: "noop", reason: "done" }] }));
      expect(buildPermissionDeniedContext()).toBe("");
    });

    it("returns empty string when tool/permission item has no denied_commands", () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: [] }],
        })
      );
      expect(buildPermissionDeniedContext()).toBe("");
    });

    it("renders template with denied command", () => {
      writePermissionDeniedTemplate();
      const items = [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1"] }];
      const result = buildPermissionDeniedContext(items);
      expect(result).toContain("go version 2>&1");
      expect(result).toContain("Repeated Permission Denied");
    });

    it("renders template with denied commands listed", () => {
      writePermissionDeniedTemplate();
      const items = [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1", "ls /usr/local/go/bin/go"] }];
      const result = buildPermissionDeniedContext(items);
      expect(result).toContain("`go version 2>&1`");
      expect(result).toContain("`ls /usr/local/go/bin/go`");
    });

    it("parses denied commands from alternatives when denied_commands is omitted", () => {
      writePermissionDeniedTemplate();
      const items = [
        {
          type: "missing_tool",
          tool: "tool/permission",
          reason: "permission denied",
          alternatives: "Verify token scopes, repository permissions, and MCP/tool access configuration. Denied commands: shell(find specs -type f -name '*.md' | sort) | read(/home/runner/work/gh-aw/gh-aw/specs)",
        },
      ];
      const result = buildPermissionDeniedContext(items);
      expect(result).toContain("`shell(find specs -type f -name '*.md' | sort)`");
      expect(result).toContain("`read(...)`");
    });

    it("parses denied commands from Daily SPDD Spec Planner alternatives", () => {
      writePermissionDeniedTemplate();
      const items = [
        {
          type: "missing_tool",
          tool: "tool/permission",
          reason: "numerous permission denied errors detected",
          alternatives:
            'Verify token scopes, repository permissions, and MCP/tool access configuration. Denied commands: shell(mkdir -p /tmp/gh-aw/cache-memory/spdd-daily/ && touch /tmp/gh-aw/cache-memory/spdd-daily/.preflight && echo "preflight_ok") | read(/home/runner/work/gh-aw/gh-aw) | read(/tmp/gh-aw/cache-memory/spdd-daily/rotation.json)',
        },
      ];
      const result = buildPermissionDeniedContext(items);
      expect(result).toContain('`shell(mkdir -p /tmp/gh-aw/cache-memory/spdd-daily/ && touch /tmp/gh-aw/cache-memory/spdd-daily/.preflight && echo "preflight_ok")`');
      expect(result).toContain("`read(...)`");
      expect(result).not.toContain("`read(/home/runner/work/gh-aw/gh-aw)`");
      expect(result).not.toContain("`read(/tmp/gh-aw/cache-memory/spdd-daily/rotation.json)`");
    });

    it("deduplicates denied commands across multiple tool/permission items", () => {
      writePermissionDeniedTemplate();
      const items = [
        { type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1", "ls /usr/local/go/bin/go"] },
        { type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1", "which go"] },
      ];
      const result = buildPermissionDeniedContext(items);
      const deniedCommandsSection = (result.match(/\*\*Denied Commands:\*\*\n([\s\S]*?)\n\n/) || [])[1] || "";
      // "go version 2>&1" should appear exactly once (deduplicated) in the denied commands list section
      const listOccurrences = (deniedCommandsSection.match(/`go version 2>&1`/g) || []).length;
      expect(listOccurrences).toBe(1);
      expect(result).toContain("`ls /usr/local/go/bin/go`");
      expect(result).toContain("`which go`");
    });

    it("collapses read(path) denials to a single read(...) entry", () => {
      writePermissionDeniedTemplate();
      const items = [
        {
          type: "missing_tool",
          tool: "tool/permission",
          reason: "permission denied",
          denied_commands: ["read(/home/runner/work/gh-aw/gh-aw)", "read(/tmp/gh-aw/cache-memory/spdd-daily/rotation.json)"],
        },
      ];
      const result = buildPermissionDeniedContext(items);
      const readOccurrences = (result.match(/`read\(\.\.\.\)`/g) || []).length;
      expect(readOccurrences).toBe(1);
      expect(result).not.toContain("`read(/home/runner/work/gh-aw/gh-aw)`");
      expect(result).not.toContain("`read(/tmp/gh-aw/cache-memory/spdd-daily/rotation.json)`");
    });

    it("throws when permission_denied_context template is missing", () => {
      const items = [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1"] }];
      expect(() => buildPermissionDeniedContext(items)).toThrow(/ENOENT|no such file/i);
    });

    it("renders template when permission_denied_context.md is available", () => {
      writePermissionDeniedTemplate();
      const items = [{ type: "missing_tool", tool: "tool/permission", reason: "permission denied", denied_commands: ["go version 2>&1"] }];
      const result = buildPermissionDeniedContext(items, "my-workflow");
      expect(result).toContain("go version 2>&1");
      expect(result).toContain("my-workflow");
      expect(result).toContain("Repeated Permission Denied");
    });
  });

  describe("tool_denials_exceeded context", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");
    let loadToolDenialsExceededEvents;
    let buildToolDenialsExceededContext;
    /** @type {string} */
    let tmpDir;

    beforeEach(() => {
      vi.resetModules();
      try {
        fs.rmSync(path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state"), { recursive: true, force: true });
      } catch {}
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-tool-denials-exceeded-"));
      process.env.RUNNER_TEMP = tmpDir;
      ({ loadToolDenialsExceededEvents, buildToolDenialsExceededContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
      try {
        fs.rmSync(path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state"), { recursive: true, force: true });
      } catch {}
    });

    it("loads guard.tool_denials_exceeded events from copilot session events.jsonl", () => {
      const sessionDir = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state", "session-1");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        [
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:00Z", data: { toolName: "read", mcpServerName: "mcp__safeoutputs" } }),
          JSON.stringify({ type: "assistant.message", timestamp: "2026-06-06T00:00:00Z", data: { content: "" } }),
          JSON.stringify({
            type: "guard.tool_denials_exceeded",
            timestamp: "2026-06-06T00:00:01Z",
            data: { denialCount: 5, threshold: 5, reason: "permission denied: read" },
          }),
        ].join("\n") + "\n"
      );

      const events = loadToolDenialsExceededEvents();
      expect(events).toEqual([{ denialCount: 5, threshold: 5, reason: "permission denied: read", recentToolCalls: ["mcp__safeoutputs.read"], timestamp: "2026-06-06T00:00:01Z" }]);
    });

    it("renders dedicated context for tool denial threshold events", () => {
      const promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.copyFileSync(path.join(__dirname, "../md/tool_denials_exceeded_context.md"), path.join(promptsDir, "tool_denials_exceeded_context.md"));

      const result = buildToolDenialsExceededContext([{ denialCount: 5, threshold: 5, reason: "permission denied: read", recentToolCalls: ["read(...)", "bash(git status)"] }], "daily-spdd-spec-planner");
      expect(result).toContain("Excessive Tool Denials");
      expect(result).toContain("5/5");
      expect(result).toContain("guard.tool_denials_exceeded");
      expect(result).toContain("daily-spdd-spec-planner");
      expect(result).toContain("> [!WARNING]");
      expect(result).toContain("<details>");
      expect(result).toContain("<summary><strong>Last denied request</strong></summary>");
      expect(result).toContain("<summary><strong>Last 5 tool calls</strong></summary>");
      expect(result).toContain("```text");
      expect(result).toContain("\nread\n");
      expect(result).toContain("- `read(...)`");
      expect(result).toContain("- `bash(git status)`");
    });

    it("normalizes Python 3 heredoc reason to a single-line summary", () => {
      const promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.copyFileSync(path.join(__dirname, "../md/tool_denials_exceeded_context.md"), path.join(promptsDir, "tool_denials_exceeded_context.md"));

      const python3Reason = 'permission denied: shell(python3 << \'EOF\'\nimport re\n\nfiles = [("foo.go", "/path/foo.go")]\nfor f, p in files:\n    print(f)\nEOF)';
      const result = buildToolDenialsExceededContext([{ denialCount: 5, threshold: 5, reason: python3Reason }], "daily-compiler-quality");
      expect(result).toContain("shell(python3 ...)");
      expect(result).not.toContain("`shell(python3 ...)`");
      // The full multi-line program body should not appear in the output
      expect(result).not.toContain("import re");
      expect(result).not.toContain("for f, p in files");
    });

    it("captures only the last 5 tool calls before guard event", () => {
      const sessionDir = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state", "session-1");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        [
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:00Z", data: { toolName: "list" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:01Z", data: { toolName: "read" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:02Z", data: { toolName: "edit" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:03Z", data: { toolName: "bash" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:04Z", data: { toolName: "grep" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:05Z", data: { toolName: "glob" } }),
          JSON.stringify({ type: "tool.execution_start", timestamp: "2026-06-06T00:00:06Z", data: { toolName: "write" } }),
          JSON.stringify({
            type: "guard.tool_denials_exceeded",
            timestamp: "2026-06-06T00:00:07Z",
            data: { denialCount: 5, threshold: 5, reason: "permission denied: read" },
          }),
        ].join("\n") + "\n"
      );

      const events = loadToolDenialsExceededEvents();
      expect(events).toEqual([
        {
          denialCount: 5,
          threshold: 5,
          reason: "permission denied: read",
          recentToolCalls: ["edit", "bash", "grep", "glob", "write"],
          timestamp: "2026-06-06T00:00:07Z",
        },
      ]);
    });

    it("captures shell command details for recent bash tool calls", () => {
      const sessionDir = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state", "session-1");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        [
          JSON.stringify({
            type: "tool.execution_start",
            timestamp: "2026-06-06T00:00:00Z",
            data: { toolName: "bash", mcpServerName: "terminal", command: "cd /home/runner/work/gh-aw/gh-aw && git diff --name-only" },
          }),
          JSON.stringify({
            type: "guard.tool_denials_exceeded",
            timestamp: "2026-06-06T00:00:01Z",
            data: { denialCount: 5, threshold: 5, reason: "permission denied: bash" },
          }),
        ].join("\n") + "\n"
      );

      const events = loadToolDenialsExceededEvents();
      expect(events).toEqual([
        {
          denialCount: 5,
          threshold: 5,
          reason: "permission denied: bash",
          recentToolCalls: ["terminal.bash(cd /home/runner/work/gh-aw/gh-aw && git diff --name-only)"],
          timestamp: "2026-06-06T00:00:01Z",
        },
      ]);
    });

    it("sanitizes backticks in shell command previews", () => {
      const sessionDir = path.join(os.tmpdir(), "gh-aw", "sandbox", "agent", "logs", "copilot-session-state", "session-1");
      fs.mkdirSync(sessionDir, { recursive: true });
      fs.writeFileSync(
        path.join(sessionDir, "events.jsonl"),
        [
          JSON.stringify({
            type: "tool.execution_start",
            timestamp: "2026-06-06T00:00:00Z",
            data: { toolName: "bash", mcpServerName: "terminal", command: "echo `hostname` && echo ok" },
          }),
          JSON.stringify({
            type: "guard.tool_denials_exceeded",
            timestamp: "2026-06-06T00:00:01Z",
            data: { denialCount: 5, threshold: 5, reason: "permission denied: bash" },
          }),
        ].join("\n") + "\n"
      );

      const events = loadToolDenialsExceededEvents();
      expect(events).toEqual([
        {
          denialCount: 5,
          threshold: 5,
          reason: "permission denied: bash",
          recentToolCalls: ["terminal.bash(echo 'hostname' && echo ok)"],
          timestamp: "2026-06-06T00:00:01Z",
        },
      ]);
    });
  });

  // ──────────────────────────────────────────────────────
  // normalizeDeniedPermissionCommand
  // ──────────────────────────────────────────────────────

  describe("normalizeDeniedPermissionCommand", () => {
    let normalizeDeniedPermissionCommand;

    beforeEach(() => {
      vi.resetModules();
      ({ normalizeDeniedPermissionCommand } = require("./handle_agent_failure.cjs"));
    });

    it("returns empty string for empty/non-string input", () => {
      expect(normalizeDeniedPermissionCommand("")).toBe("");
      expect(normalizeDeniedPermissionCommand(null)).toBe("");
      expect(normalizeDeniedPermissionCommand(undefined)).toBe("");
    });

    it("collapses read(path) to read(...)", () => {
      expect(normalizeDeniedPermissionCommand("read(/some/path/file.go)")).toBe("read(...)");
      expect(normalizeDeniedPermissionCommand("read(/home/runner/work/repo/go.mod)")).toBe("read(...)");
    });

    it("returns non-read single-line commands unchanged", () => {
      expect(normalizeDeniedPermissionCommand("bash(echo hello)")).toBe("bash(echo hello)");
      expect(normalizeDeniedPermissionCommand("permission denied: shell(python3 --version)")).toBe("shell(python3 --version)");
    });

    it("collapses shell(python3 << 'EOF' ...) heredoc to shell(python3 ...)", () => {
      const cmd = "shell(python3 << 'EOF'\nimport re\nprint('hello')\nEOF)";
      expect(normalizeDeniedPermissionCommand(cmd)).toBe("shell(python3 ...)");
    });

    it('collapses shell(python3 << "EOF" ...) heredoc with double-quoted marker', () => {
      const cmd = 'shell(python3 << "EOF"\nimport sys\nsys.exit(0)\nEOF)';
      expect(normalizeDeniedPermissionCommand(cmd)).toBe("shell(python3 ...)");
    });

    it("collapses shell(python << 'EOF' ...) heredoc for unversioned python", () => {
      const cmd = "shell(python << 'EOF'\nprint(1)\nEOF)";
      expect(normalizeDeniedPermissionCommand(cmd)).toBe("shell(python ...)");
    });

    it("collapses any shell heredoc program, not just python3", () => {
      const cmd = "shell(node << 'EOF'\nconsole.log('hi');\nEOF)";
      expect(normalizeDeniedPermissionCommand(cmd)).toBe("shell(node ...)");
    });
  });

  describe("missing_tool and missing_data report-as-failure flags", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-report-as-failure-"));
      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      // Write agent output with both missing_tool and missing_data items
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "missing_tool", tool: "bash", reason: "not available" },
            { type: "missing_data", data_type: "config", reason: "file not found" },
          ],
        })
      );
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.GH_AW_MISSING_TOOL_REPORT_AS_FAILURE;
      delete process.env.GH_AW_MISSING_DATA_REPORT_AS_FAILURE;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("buildMissingToolContext returns context when GH_AW_MISSING_TOOL_REPORT_AS_FAILURE is not set (default true)", () => {
      const { buildMissingToolContext } = require("./handle_agent_failure.cjs");
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
    });

    it("buildMissingToolContext returns context when GH_AW_MISSING_TOOL_REPORT_AS_FAILURE is true", () => {
      process.env.GH_AW_MISSING_TOOL_REPORT_AS_FAILURE = "true";
      vi.resetModules();
      const { buildMissingToolContext } = require("./handle_agent_failure.cjs");
      const result = buildMissingToolContext();
      expect(result).toContain("Missing Tools Reported");
    });

    it("buildMissingToolContext still returns context when GH_AW_MISSING_TOOL_REPORT_AS_FAILURE is false (context building is independent of flag)", () => {
      // buildMissingToolContext always builds context; the flag controls hasMissingTool in main()
      process.env.GH_AW_MISSING_TOOL_REPORT_AS_FAILURE = "false";
      vi.resetModules();
      const { buildMissingToolContext } = require("./handle_agent_failure.cjs");
      const result = buildMissingToolContext();
      // buildMissingToolContext reads agent output directly, not the env flag
      expect(result).toContain("Missing Tools Reported");
    });
  });

  // ──────────────────────────────────────────────────────
  // hasAgentTerminalReasonCompleted
  // ──────────────────────────────────────────────────────

  describe("hasAgentTerminalReasonCompleted", () => {
    let hasAgentTerminalReasonCompleted;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let stdioLogPath;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-terminal-reason-"));
      stdioLogPath = path.join(tmpDir, "agent-stdio.log");
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      ({ hasAgentTerminalReasonCompleted } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns false when log file does not exist", () => {
      expect(hasAgentTerminalReasonCompleted()).toBe(false);
    });

    it("returns false when log file is empty", () => {
      fs.writeFileSync(stdioLogPath, "");
      expect(hasAgentTerminalReasonCompleted()).toBe(false);
    });

    it("returns true for plain JSON result line with terminal_reason: completed", () => {
      fs.writeFileSync(stdioLogPath, '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":53,"total_cost_usd":1.91}\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });

    it("returns true for timestamp-prefixed result line", () => {
      fs.writeFileSync(stdioLogPath, '2026-04-27T21:45:00.080Z  {"type":"result","subtype":"success","terminal_reason":"completed","num_turns":53}\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });

    it("returns false when terminal_reason is not completed", () => {
      fs.writeFileSync(stdioLogPath, '{"type":"result","subtype":"error_max_turns","terminal_reason":"max_turns","num_turns":50}\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(false);
    });

    it("returns false when log contains only non-JSON lines", () => {
      fs.writeFileSync(stdioLogPath, "Starting agent...\nRunning tool: list_files\nAgent interrupted\n");
      expect(hasAgentTerminalReasonCompleted()).toBe(false);
    });

    it("returns true when terminal_reason: completed appears among other log lines", () => {
      const lines = ["Starting agent...", '{"type":"system","subtype":"init","model":"claude-sonnet-4"}', '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":10}', "Process exiting with code: 0"];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });

    it("returns true when JSON is truncated but substring is present in content", () => {
      // A truncated line that can't be fully parsed as JSON but contains the literal substring
      fs.writeFileSync(stdioLogPath, '"terminal_reason":"completed","num_turns":53 (truncated\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });

    it("returns true with no spaces around colon (compact JSON)", () => {
      fs.writeFileSync(stdioLogPath, '{"terminal_reason":"completed"}\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });

    it("returns true with one space on each side of colon", () => {
      fs.writeFileSync(stdioLogPath, '{"terminal_reason" : "completed"}\n');
      expect(hasAgentTerminalReasonCompleted()).toBe(true);
    });
  });

  // ──────────────────────────────────────────────────────
  // buildEngineFailureContext — terminal_reason guard
  // ──────────────────────────────────────────────────────

  describe("buildEngineFailureContext with terminal_reason guard", () => {
    let buildEngineFailureContext;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let stdioLogPath;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-engine-fail-guard-"));
      stdioLogPath = path.join(tmpDir, "agent-stdio.log");
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      process.env.GH_AW_OTEL_JSONL_PATH = path.join(tmpDir, "otel.jsonl");
      process.env.RUNNER_TEMP = tmpDir;
      ({ buildEngineFailureContext } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.GH_AW_ENGINE_ID;
      delete process.env.GH_AW_OTEL_JSONL_PATH;
      delete process.env.RUNNER_TEMP;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when log contains terminal_reason: completed (plain JSON)", () => {
      fs.writeFileSync(stdioLogPath, '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":53,"total_cost_usd":1.91}\n');
      expect(buildEngineFailureContext()).toBe("");
    });

    it("returns empty string when log contains terminal_reason: completed with timestamp prefix", () => {
      const lines = [
        "Starting claude workflow...",
        "2026-04-27T21:44:49.870Z  safeoutputs.create_discussion: completed successfully in 76ms",
        '2026-04-27T21:45:00.080Z  {"type":"result","subtype":"success","terminal_reason":"completed","num_turns":53,"total_cost_usd":1.91}',
        "[WARN] Command completed with exit code: 1",
        "Process exiting with code: 1",
      ];
      fs.writeFileSync(stdioLogPath, lines.join("\n") + "\n");
      expect(buildEngineFailureContext()).toBe("");
    });

    it("still surfaces errors when terminal_reason is not completed", () => {
      fs.writeFileSync(stdioLogPath, 'ERROR: quota exceeded\n{"type":"result","terminal_reason":"max_turns"}\n');
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("quota exceeded");
    });

    it("still uses fallback tail when no terminal_reason and no error patterns", () => {
      fs.writeFileSync(stdioLogPath, "Starting agent...\nAgent interrupted unexpectedly\n");
      const result = buildEngineFailureContext();
      expect(result).toContain("Engine Failure");
      expect(result).toContain("Agent interrupted unexpectedly");
    });
  });

  // ──────────────────────────────────────────────────────
  // main() — hasCompletedDespiteJobFailure early-return
  // ──────────────────────────────────────────────────────

  describe("main() hasCompletedDespiteJobFailure early-return", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;

    beforeEach(() => {
      vi.resetModules();
      for (const key of Object.keys(process.env)) {
        if (key.startsWith("GH_AW_")) {
          delete process.env[key];
        }
      }
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-test-completed-despite-failure-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });

      // Minimal templates required by main()
      fs.writeFileSync(path.join(promptsDir, "agent_failure_comment.md"), "COMMENT TEMPLATE");
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "ISSUE TEMPLATE");

      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123456";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      process.env.GH_AW_CHECKOUT_PR_SUCCESS = "true";
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_RUN_URL;
      delete process.env.GH_AW_AGENT_CONCLUSION;
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.GH_AW_CHECKOUT_PR_SUCCESS;
      if (fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("skips failure issue creation when terminal_reason: completed and non-noop safe outputs present", async () => {
      // Agent produced a valid non-noop safe output
      fs.writeFileSync(path.join(tmpDir, "agent_output.json"), JSON.stringify({ items: [{ type: "create_discussion", title: "Done", body: "All set." }] }));
      // stdio log contains terminal_reason: completed
      fs.writeFileSync(path.join(tmpDir, "agent-stdio.log"), '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":10}\n');

      const createIssueMock = vi.fn();
      const createCommentMock = vi.fn();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      vi.resetModules();
      const { main: mainFn } = require("./handle_agent_failure.cjs");
      await mainFn();

      expect(createIssueMock).not.toHaveBeenCalled();
      expect(createCommentMock).not.toHaveBeenCalled();
    });

    it("keeps the step failed when failure issue creation fails after a successful agent reports incomplete", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      // Agent produced both a non-noop item and a report_incomplete signal
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "create_discussion", title: "Done", body: "All set." },
            { type: "report_incomplete", reason: "mcp_crash" },
          ],
        })
      );
      fs.writeFileSync(path.join(tmpDir, "agent-stdio.log"), '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":10}\n');

      const createIssueMock = vi.fn(async () => {
        throw new Error("issue creation failed");
      });
      const createCommentMock = vi.fn(async () => ({ data: { id: 1001 } }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) return { data: { total_count: 0, items: [] } };
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      vi.resetModules();
      const { main: mainFn } = require("./handle_agent_failure.cjs");
      await mainFn();

      // report_incomplete overrides the hasCompletedDespiteJobFailure exemption
      expect(createIssueMock).toHaveBeenCalled();
      expect(global.core.setFailed).toHaveBeenCalledWith("Agent reported that it could not complete the task");
      expect(global.core.warning).toHaveBeenCalledWith("Failed to create or update failure tracking issue: issue creation failed");
    });

    it("marks the step failed when failure issue reporting is disabled", async () => {
      process.env.GH_AW_AGENT_CONCLUSION = "success";
      process.env.GH_AW_FAILURE_REPORT_AS_ISSUE = "false";
      fs.writeFileSync(path.join(tmpDir, "agent_output.json"), JSON.stringify({ items: [{ type: "report_incomplete", reason: "mcp_crash" }] }));

      const createIssueMock = vi.fn();
      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      vi.resetModules();
      const { main: mainFn } = require("./handle_agent_failure.cjs");
      await mainFn();

      expect(global.core.setFailed).toHaveBeenCalledWith("Agent reported that it could not complete the task");
      expect(createIssueMock).not.toHaveBeenCalled();
    });

    it("ignores report_incomplete when it only describes task_complete registration trouble after successful outputs", async () => {
      fs.writeFileSync(
        path.join(tmpDir, "agent_output.json"),
        JSON.stringify({
          items: [
            { type: "add_comment", body: "done" },
            { type: "submit_pull_request_review", event: "APPROVE", body: "done" },
            {
              type: "report_incomplete",
              reason: "Test Quality Sentinel analysis completed successfully. However, task_complete tool calls are not registering with the system despite repeated attempts and no remaining work identified.",
            },
          ],
        })
      );
      fs.writeFileSync(path.join(tmpDir, "agent-stdio.log"), '{"type":"result","subtype":"success","terminal_reason":"completed","num_turns":10}\n');

      const createIssueMock = vi.fn();
      const createCommentMock = vi.fn();

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async () => ({ data: { total_count: 0, items: [] } })),
          },
          issues: {
            create: createIssueMock,
            createComment: createCommentMock,
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      vi.resetModules();
      const { main: mainFn } = require("./handle_agent_failure.cjs");
      await mainFn();

      expect(createIssueMock).not.toHaveBeenCalled();
      expect(createCommentMock).not.toHaveBeenCalled();
    });
  });

  describe("parseFirewallAuthErrors", () => {
    const fs = require("fs");
    const os = require("os");
    const path = require("path");

    let tmpDir;
    let parseFirewallAuthErrors;

    beforeEach(() => {
      global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), debug: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
      global.github = {};
      global.context = { repo: { owner: "owner", repo: "repo" } };
      vi.resetModules();
      ({ parseFirewallAuthErrors } = require("./handle_agent_failure.cjs"));
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-firewall-auth-"));
    });

    afterEach(() => {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_ENGINE_API_HOSTS;
      delete process.env.GH_AW_ENGINE_ID;
      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty array when file does not exist", () => {
      const result = parseFirewallAuthErrors(path.join(tmpDir, "nonexistent.jsonl"));
      expect(result).toEqual([]);
    });

    it("returns empty array when file is empty", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, "");
      expect(parseFirewallAuthErrors(jsonlPath)).toEqual([]);
    });

    it("returns empty array when no 401/403 entries", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, [JSON.stringify({ ts: 1000, host: "api.enterprise.githubcopilot.com:443", status: 200 }), JSON.stringify({ ts: 1001, host: "api.openai.com:443", status: 200 })].join("\n"));
      expect(parseFirewallAuthErrors(jsonlPath)).toEqual([]);
    });

    it("detects Copilot 401 auth rejection via hardcoded fallback", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.enterprise.githubcopilot.com:443", status: 401 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("GitHub Copilot");
      expect(result[0].credential).toContain("COPILOT_GITHUB_TOKEN");
    });

    it("detects OpenAI 401 auth rejection via hardcoded fallback", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.openai.com:443", status: 401 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("OpenAI Codex");
      expect(result[0].credential).toContain("OPENAI_API_KEY");
    });

    it("detects Anthropic 403 auth rejection via hardcoded fallback", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.anthropic.com:443", status: 403 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("Anthropic Claude");
      expect(result[0].credential).toContain("ANTHROPIC_API_KEY");
    });

    it("detects Gemini 403 auth rejection via hardcoded fallback", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "generativelanguage.googleapis.com:443", status: 403 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("Google Gemini");
      expect(result[0].credential).toContain("GEMINI_API_KEY");
    });

    it("deduplicates multiple auth errors for the same provider", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, [JSON.stringify({ ts: 1000, host: "api.enterprise.githubcopilot.com:443", status: 401 }), JSON.stringify({ ts: 1001, host: "api.githubcopilot.com:443", status: 401 })].join("\n"));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("GitHub Copilot");
    });

    it("reports multiple different providers", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, [JSON.stringify({ ts: 1000, host: "api.openai.com:443", status: 401 }), JSON.stringify({ ts: 1001, host: "api.anthropic.com:443", status: 403 })].join("\n"));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(2);
      const providers = result.map(r => r.provider);
      expect(providers).toContain("OpenAI Codex");
      expect(providers).toContain("Anthropic Claude");
    });

    it("skips non-JSON lines without throwing", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, ["# comment line", "not json", JSON.stringify({ ts: 1000, host: "api.openai.com:443", status: 401 }), ""].join("\n"));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("OpenAI Codex");
    });

    it("uses GH_AW_ENGINE_API_HOSTS env var when set", () => {
      process.env.GH_AW_ENGINE_API_HOSTS = "api.enterprise.githubcopilot.com,api.githubcopilot.com";
      process.env.GH_AW_ENGINE_ID = "copilot";
      vi.resetModules();
      ({ parseFirewallAuthErrors } = require("./handle_agent_failure.cjs"));

      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.enterprise.githubcopilot.com:443", status: 401 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("GitHub Copilot");
      expect(result[0].credential).toContain("COPILOT_GITHUB_TOKEN");
    });

    it("uses engine label from ENGINE_ID_TO_LABEL when env var host matches", () => {
      process.env.GH_AW_ENGINE_API_HOSTS = "api.anthropic.com";
      process.env.GH_AW_ENGINE_ID = "claude";
      vi.resetModules();
      ({ parseFirewallAuthErrors } = require("./handle_agent_failure.cjs"));

      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.anthropic.com:443", status: 401 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("Anthropic Claude");
      expect(result[0].credential).toContain("ANTHROPIC_API_KEY");
    });

    it("uses engine ID as provider label when not in lookup table", () => {
      process.env.GH_AW_ENGINE_API_HOSTS = "custom-llm.internal.example.com";
      process.env.GH_AW_ENGINE_ID = "custom";
      vi.resetModules();
      ({ parseFirewallAuthErrors } = require("./handle_agent_failure.cjs"));

      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "custom-llm.internal.example.com:443", status: 401 }));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toHaveLength(1);
      expect(result[0].provider).toBe("custom");
    });

    it("selective pre-scan: skips full parse when no 4xx entries in large file", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      // Write many 200 entries — should bail on pre-scan without parsing each line
      const lines = [];
      for (let i = 0; i < 100; i++) {
        lines.push(JSON.stringify({ ts: 1000 + i, host: "api.github.com:443", status: 200 }));
      }
      fs.writeFileSync(jsonlPath, lines.join("\n"));
      const result = parseFirewallAuthErrors(jsonlPath);
      expect(result).toEqual([]);
    });
  });

  describe("readTokenUsageMarkdown", () => {
    let readTokenUsageMarkdown;
    const fs = require("fs");
    const os = require("os");
    const path = require("path");
    let tmpDir;

    beforeEach(() => {
      vi.resetModules();
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "rt-usage-test-"));
      ({ readTokenUsageMarkdown } = require("./handle_agent_failure.cjs"));
    });

    afterEach(() => {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    });

    it("returns null when no token-usage.jsonl files exist", () => {
      const result = readTokenUsageMarkdown();
      expect(result).toBeNull();
    });

    it("returns markdown and model aliases when a valid token-usage.jsonl file is present", () => {
      const { TOKEN_USAGE_PATH } = require("./parse_token_usage.cjs");
      fs.mkdirSync(path.dirname(TOKEN_USAGE_PATH), { recursive: true });
      const entry = JSON.stringify({ model: "claude-sonnet-4.5", input_tokens: 1000, output_tokens: 500, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 });
      const origContent = fs.existsSync(TOKEN_USAGE_PATH) ? fs.readFileSync(TOKEN_USAGE_PATH, "utf8") : null;
      fs.writeFileSync(TOKEN_USAGE_PATH, entry + "\n");
      try {
        vi.resetModules();
        ({ readTokenUsageMarkdown } = require("./handle_agent_failure.cjs"));
        const result = readTokenUsageMarkdown();
        expect(result).not.toBeNull();
        expect(result.markdown).toContain("sonnet45");
        expect(result.markdown).toContain("1,000");
        expect(result.markdown).toContain("Alias");
        expect(result.modelNames).toEqual(["claude-sonnet-4.5"]);
      } finally {
        if (origContent !== null) {
          fs.writeFileSync(TOKEN_USAGE_PATH, origContent);
        } else if (fs.existsSync(TOKEN_USAGE_PATH)) {
          fs.unlinkSync(TOKEN_USAGE_PATH);
        }
      }
    });
  });

  describe("buildCredentialAuthErrorContext", () => {
    const fs = require("fs");
    const os = require("os");
    const path = require("path");

    let tmpDir;
    let buildCredentialAuthErrorContext;

    beforeEach(() => {
      global.core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), debug: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
      global.github = {};
      global.context = { repo: { owner: "owner", repo: "repo" } };
      vi.resetModules();
      ({ buildCredentialAuthErrorContext } = require("./handle_agent_failure.cjs"));
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-cred-auth-"));
      // Create prompt template so getPromptPath resolves
      const promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "credential_auth_error.md"), "**🔑 Credential Authentication Failed**: Missing/invalid credentials for:\n{providers}\n");
      process.env.RUNNER_TEMP = tmpDir;
    });

    afterEach(() => {
      delete global.core;
      delete global.github;
      delete global.context;
      delete process.env.GH_AW_AGENT_OUTPUT;
      delete process.env.RUNNER_TEMP;
      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it("returns empty string when audit.jsonl does not exist", () => {
      const result = buildCredentialAuthErrorContext(path.join(tmpDir, "nonexistent.jsonl"));
      expect(result).toBe("");
    });

    it("returns empty string when no auth errors in audit.jsonl", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.github.com:443", status: 200 }));
      const result = buildCredentialAuthErrorContext(jsonlPath);
      expect(result).toBe("");
    });

    it("returns credential alert when auth rejection found", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.openai.com:443", status: 401 }));
      const result = buildCredentialAuthErrorContext(jsonlPath);
      expect(result).toBeTruthy();
      expect(result).toContain("OpenAI");
      expect(result).toContain("OPENAI_API_KEY");
    });

    it("includes all affected providers in the output", () => {
      const jsonlPath = path.join(tmpDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, [JSON.stringify({ ts: 1000, host: "api.enterprise.githubcopilot.com:443", status: 401 }), JSON.stringify({ ts: 1001, host: "api.anthropic.com:443", status: 403 })].join("\n"));
      const result = buildCredentialAuthErrorContext(jsonlPath);
      expect(result).toContain("GitHub Copilot");
      expect(result).toContain("Anthropic Claude");
    });

    it("derives audit.jsonl path from GH_AW_AGENT_OUTPUT when no override provided", () => {
      const auditDir = path.join(tmpDir, "sandbox", "firewall", "audit");
      fs.mkdirSync(auditDir, { recursive: true });
      const jsonlPath = path.join(auditDir, "audit.jsonl");
      fs.writeFileSync(jsonlPath, JSON.stringify({ ts: 1000, host: "api.openai.com:443", status: 401 }));

      process.env.GH_AW_AGENT_OUTPUT = path.join(tmpDir, "agent_output.json");
      vi.resetModules();
      ({ buildCredentialAuthErrorContext } = require("./handle_agent_failure.cjs"));

      const result = buildCredentialAuthErrorContext();
      expect(result).toContain("OpenAI");
    });
  });

  describe("detectAndHandleFailureCascade", () => {
    let detectAndHandleFailureCascade;
    let findRecentFailureIssues;
    let CASCADE_THRESHOLD;
    let CASCADE_LABEL;
    let CASCADE_ROLLUP_LABEL;
    let CASCADE_ROLLUP_TITLE;
    let CASCADE_WINDOW_MINUTES;

    /**
     * Build a list of N fake `[aw] * failed` items starting at issue number `startNum`.
     * @param {number} count
     * @param {number} [startNum=100]
     */
    function makeFailureItems(count, startNum = 100) {
      return Array.from({ length: count }, (_, i) => ({
        number: startNum + i,
        title: `[aw] Workflow ${i} failed`,
        html_url: `https://github.com/owner/repo/issues/${startNum + i}`,
        created_at: new Date().toISOString(),
      }));
    }

    beforeEach(() => {
      vi.resetModules();
      ({ detectAndHandleFailureCascade, findRecentFailureIssues, CASCADE_THRESHOLD, CASCADE_LABEL, CASCADE_ROLLUP_LABEL, CASCADE_ROLLUP_TITLE, CASCADE_WINDOW_MINUTES } = require("./handle_agent_failure.cjs"));
    });

    it("exports cascade constants with expected values", () => {
      expect(CASCADE_THRESHOLD).toBe(10);
      expect(CASCADE_WINDOW_MINUTES).toBe(60);
      expect(CASCADE_LABEL).toBe("cascade-suspected");
      expect(CASCADE_ROLLUP_LABEL).toBe("cascade-rollup");
      expect(CASCADE_ROLLUP_TITLE).toBe("[aw] Failure cascade detected");
    });

    it("does nothing when fewer than CASCADE_THRESHOLD issues are in the window", async () => {
      const createIssueMock = vi.fn();
      const addLabelsMock = vi.fn();
      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) return { data: { total_count: 0, items: [] } };
        // Return 5 issues — below threshold
        const items = makeFailureItems(5);
        return { data: { total_count: items.length, items } };
      });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: vi.fn().mockResolvedValue({ data: {} }),
            create: createIssueMock,
            update: vi.fn(),
            addLabels: addLabelsMock,
          },
        },
        graphql: vi.fn(),
      };

      await detectAndHandleFailureCascade("owner", "repo", 999);

      expect(createIssueMock).not.toHaveBeenCalled();
      expect(addLabelsMock).not.toHaveBeenCalled();
    });

    it("creates a rollup issue and labels individual issues when CASCADE_THRESHOLD is met", async () => {
      const createIssueMock = vi.fn().mockResolvedValue({
        data: { number: 200, html_url: "https://github.com/owner/repo/issues/200", node_id: "I_200" },
      });
      const addLabelsMock = vi.fn().mockResolvedValue({});
      const getLabelMock = vi.fn().mockResolvedValue({ data: {} });

      const recentItems = makeFailureItems(10);

      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) return { data: { total_count: 0, items: [] } };
        return { data: { total_count: recentItems.length, items: recentItems } };
      });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: getLabelMock,
            create: createIssueMock,
            update: vi.fn(),
            addLabels: addLabelsMock,
          },
        },
        graphql: vi.fn(),
      };

      // triggeringIssueNumber is already in recentItems (100)
      await detectAndHandleFailureCascade("owner", "repo", 100);

      // Rollup issue created
      expect(createIssueMock).toHaveBeenCalledOnce();
      const createCall = createIssueMock.mock.calls[0][0];
      expect(createCall.title).toBe(CASCADE_ROLLUP_TITLE);
      expect(createCall.labels).toContain(CASCADE_ROLLUP_LABEL);
      expect(createCall.labels).toContain("agentic-workflows");
      expect(createCall.headers).toEqual({ "X-GitHub-Api-Version": "2022-11-28" });

      // All 10 issues labeled
      expect(addLabelsMock).toHaveBeenCalledTimes(10);
      for (const call of addLabelsMock.mock.calls) {
        expect(call[0].labels).toContain(CASCADE_LABEL);
      }
    });

    it("triggers cascade when triggering issue is not in search results (search indexing lag)", async () => {
      const createIssueMock = vi.fn().mockResolvedValue({
        data: { number: 201, html_url: "https://github.com/owner/repo/issues/201", node_id: "I_201" },
      });
      const addLabelsMock = vi.fn().mockResolvedValue({});

      // Search returns only 9 issues (not the triggering one — simulating indexing lag)
      const recentItems = makeFailureItems(9);

      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) return { data: { total_count: 0, items: [] } };
        return { data: { total_count: recentItems.length, items: recentItems } };
      });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: vi.fn().mockResolvedValue({ data: {} }),
            create: createIssueMock,
            update: vi.fn(),
            addLabels: addLabelsMock,
          },
        },
        graphql: vi.fn(),
      };

      // Triggering issue (109) is NOT in recentItems — total becomes 10 once merged
      await detectAndHandleFailureCascade("owner", "repo", 109);

      expect(createIssueMock).toHaveBeenCalledOnce();
      // 9 from search + 1 triggering issue = 10 addLabels calls
      expect(addLabelsMock).toHaveBeenCalledTimes(10);
    });

    it("updates existing rollup issue instead of creating a new one", async () => {
      const createIssueMock = vi.fn();
      const updateIssueMock = vi.fn().mockResolvedValue({});
      const addLabelsMock = vi.fn().mockResolvedValue({});

      const recentItems = makeFailureItems(10);

      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) {
          return {
            data: {
              total_count: 1,
              items: [{ number: 50, html_url: "https://github.com/owner/repo/issues/50" }],
            },
          };
        }
        return { data: { total_count: recentItems.length, items: recentItems } };
      });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: vi.fn().mockResolvedValue({ data: {} }),
            create: createIssueMock,
            update: updateIssueMock,
            addLabels: addLabelsMock,
          },
        },
        graphql: vi.fn(),
      };

      await detectAndHandleFailureCascade("owner", "repo", 100);

      // Should update, not create
      expect(createIssueMock).not.toHaveBeenCalled();
      expect(updateIssueMock).toHaveBeenCalledOnce();
      expect(updateIssueMock.mock.calls[0][0].issue_number).toBe(50);
    });

    it("creates cascade-suspected label when it does not exist (404)", async () => {
      const createLabelMock = vi.fn().mockResolvedValue({});
      const createIssueMock = vi.fn().mockResolvedValue({
        data: { number: 202, html_url: "https://github.com/owner/repo/issues/202", node_id: "I_202" },
      });
      const addLabelsMock = vi.fn().mockResolvedValue({});

      const recentItems = makeFailureItems(10);

      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) return { data: { total_count: 0, items: [] } };
        return { data: { total_count: recentItems.length, items: recentItems } };
      });

      const getLabelMock = vi.fn().mockRejectedValue(Object.assign(new Error("Not Found"), { status: 404 }));

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: getLabelMock,
            createLabel: createLabelMock,
            create: createIssueMock,
            update: vi.fn(),
            addLabels: addLabelsMock,
          },
        },
        graphql: vi.fn(),
      };

      await detectAndHandleFailureCascade("owner", "repo", 100);

      // Both cascade-suspected and cascade-rollup labels should be created
      expect(createLabelMock).toHaveBeenCalledTimes(2);
      const createdNames = createLabelMock.mock.calls.map(c => c[0].name);
      expect(createdNames).toContain(CASCADE_LABEL);
      expect(createdNames).toContain(CASCADE_ROLLUP_LABEL);
    });

    it("rollup body includes affected workflow list", async () => {
      let capturedBody = "";
      const createIssueMock = vi.fn(async ({ body }) => {
        capturedBody = body;
        return { data: { number: 203, html_url: "https://github.com/owner/repo/issues/203", node_id: "I_203" } };
      });

      const recentItems = makeFailureItems(10);

      const searchMock = vi.fn(async ({ q }) => {
        if (q.includes("cascade-rollup")) return { data: { total_count: 0, items: [] } };
        return { data: { total_count: recentItems.length, items: recentItems } };
      });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: {
            getLabel: vi.fn().mockResolvedValue({ data: {} }),
            create: createIssueMock,
            update: vi.fn(),
            addLabels: vi.fn().mockResolvedValue({}),
          },
        },
        graphql: vi.fn(),
      };

      await detectAndHandleFailureCascade("owner", "repo", 100);

      expect(capturedBody).toContain("⚠️ Failure Cascade Detected");
      expect(capturedBody).toContain("cascade-suspected");
      expect(capturedBody).toContain("#100");
      expect(capturedBody).toContain("[aw] Workflow 0 failed");
    });

    it("filters out non-[aw] issues from findRecentFailureIssues", async () => {
      const mixedItems = [
        {
          number: 1,
          title: "[aw] My Workflow failed",
          html_url: "https://github.com/owner/repo/issues/1",
          created_at: new Date().toISOString(),
        },
        {
          number: 2,
          title: "Some other issue title",
          html_url: "https://github.com/owner/repo/issues/2",
          created_at: new Date().toISOString(),
        },
        {
          number: 3,
          title: "[aw] Another Workflow timed out",
          html_url: "https://github.com/owner/repo/issues/3",
          created_at: new Date().toISOString(),
        },
        {
          number: 4,
          title: "Workflow timed out",
          html_url: "https://github.com/owner/repo/issues/4",
          created_at: new Date().toISOString(),
        },
        {
          number: 5,
          title: "[aw] Budget Workflow exceeded daily AI credits budget",
          html_url: "https://github.com/owner/repo/issues/5",
          created_at: new Date().toISOString(),
        },
        {
          number: 6,
          title: "[aw] Rate Limit Workflow hit AI credits rate limit",
          html_url: "https://github.com/owner/repo/issues/6",
          created_at: new Date().toISOString(),
        },
      ];

      const searchMock = vi.fn().mockResolvedValue({ data: { total_count: mixedItems.length, items: mixedItems } });

      global.github = {
        rest: {
          search: { issuesAndPullRequests: searchMock },
          issues: { getLabel: vi.fn(), addLabels: vi.fn() },
        },
        graphql: vi.fn(),
      };

      const result = await findRecentFailureIssues("owner", "repo");
      expect(result).toHaveLength(4);
      expect(result.map(i => i.number)).toEqual([1, 3, 5, 6]);
      expect(result.map(i => i.number)).not.toContain(2);
      expect(result.map(i => i.number)).not.toContain(4);
      const query = searchMock.mock.calls[0][0].q;
      expect(query).toContain('"[aw]" in:title');
      expect(query).not.toContain('"failed" in:title');
    });

    it("is non-fatal: handles search API errors gracefully", async () => {
      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn().mockRejectedValue(new Error("API rate limit exceeded")),
          },
          issues: {
            getLabel: vi.fn(),
            create: vi.fn(),
            addLabels: vi.fn(),
          },
        },
        graphql: vi.fn(),
      };

      // Should not throw
      await expect(detectAndHandleFailureCascade("owner", "repo", 999)).resolves.toBeUndefined();
    });
  });

  describe("failure categories generation", () => {
    let buildFailureMatchCategories;

    beforeEach(() => {
      vi.resetModules();
      ({ buildFailureMatchCategories } = require("./handle_agent_failure.cjs"));
    });

    it("returns expected categories for agent failure", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        isTimedOut: false,
      });
      expect(categories).toContain("agent_failure");
      expect(categories.length).toBeGreaterThan(0);
    });

    it("returns timed_out category", () => {
      const categories = buildFailureMatchCategories({
        isTimedOut: true,
      });
      expect(categories).toContain("timed_out");
    });

    it("returns missing_safe_outputs category", () => {
      const categories = buildFailureMatchCategories({
        hasMissingSafeOutputs: true,
      });
      expect(categories).toContain("missing_safe_outputs");
    });

    it("returns report_incomplete category", () => {
      const categories = buildFailureMatchCategories({
        hasReportIncomplete: true,
      });
      expect(categories).toContain("report_incomplete");
    });

    it("does not add agent_failure when a specific category already exists", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        isTimedOut: false,
        hasReportIncomplete: true,
      });
      expect(categories).toContain("report_incomplete");
      expect(categories).not.toContain("agent_failure");
    });

    it("returns sorted categories", () => {
      const categories = buildFailureMatchCategories({
        hasMissingSafeOutputs: true,
        isTimedOut: true,
        hasReportIncomplete: true,
      });
      // Should be sorted alphabetically
      for (let i = 1; i < categories.length; i++) {
        expect(categories[i] >= categories[i - 1]).toBe(true);
      }
    });

    it("returns http_400_response_error category", () => {
      const categories = buildFailureMatchCategories({
        http400ResponseError: true,
      });
      expect(categories).toContain("http_400_response_error");
    });

    it("returns awf_firewall_startup_failed category when isAWFFirewallStartupFailed is true", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        isTimedOut: false,
        isAWFFirewallStartupFailed: true,
      });
      expect(categories).toContain("awf_firewall_startup_failed");
      expect(categories).not.toContain("agent_failure");
    });

    it("does not return awf_firewall_startup_failed category when isAWFFirewallStartupFailed is false", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        isTimedOut: false,
        isAWFFirewallStartupFailed: false,
      });
      expect(categories).not.toContain("awf_firewall_startup_failed");
    });

    it("returns missing_model_pricing category when missingModelPricingError is true", () => {
      const categories = buildFailureMatchCategories({
        missingModelPricingError: true,
      });
      expect(categories).toContain("missing_model_pricing");
    });

    it("returns shell_expansion_guard_rejected category when shellExpansionGuardRejected is true", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        shellExpansionGuardRejected: true,
      });
      expect(categories).toContain("shell_expansion_guard_rejected");
      expect(categories).not.toContain("agent_failure");
    });

    it("does not return missing_model_pricing category when missingModelPricingError is false", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        missingModelPricingError: false,
      });
      expect(categories).not.toContain("missing_model_pricing");
    });

    it("returns engine_rate_limit_429 category when hasEngineRateLimit429 is true", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        hasEngineRateLimit429: true,
      });
      expect(categories).toContain("engine_rate_limit_429");
      expect(categories).not.toContain("agent_failure");
    });

    it("does not return engine_rate_limit_429 category when hasEngineRateLimit429 is false", () => {
      const categories = buildFailureMatchCategories({
        agentConclusion: "failure",
        hasEngineRateLimit429: false,
      });
      expect(categories).not.toContain("engine_rate_limit_429");
    });
  });

  describe("failure categories filter behavior", () => {
    const fs = require("fs");
    const path = require("path");
    const os = require("os");

    /** @type {string} */
    let tmpDir;
    /** @type {string} */
    let promptsDir;

    const setupGithubMock = () => {
      const createIssueMock = vi.fn(async () => ({
        data: { number: 101, html_url: "https://github.com/owner/repo/issues/101", node_id: "I_101" },
      }));

      global.github = {
        rest: {
          search: {
            issuesAndPullRequests: vi.fn(async ({ q }) => {
              if (q.includes("is:pr")) {
                return { data: { total_count: 0, items: [] } };
              }
              return { data: { total_count: 0, items: [] } };
            }),
          },
          issues: {
            create: createIssueMock,
            createComment: vi.fn(),
            getLabel: vi.fn(),
            addLabels: vi.fn(),
          },
          pulls: { get: vi.fn() },
        },
        graphql: vi.fn(),
      };

      return { createIssueMock };
    };

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "aw-failure-filter-"));
      promptsDir = path.join(tmpDir, "gh-aw", "prompts");
      fs.mkdirSync(promptsDir, { recursive: true });
      fs.writeFileSync(path.join(promptsDir, "agent_failure_comment.md"), "COMMENT TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "agent_failure_issue.md"), "ISSUE TEMPLATE CONTENT");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_issue.md"), "Daily cap rollup issue body cap={cap} window={window_hours}");
      fs.writeFileSync(path.join(promptsDir, "daily_cap_rollup_comment.md"), "Failure suppressed workflow={workflow_name} run={run_url} categories={summary} cap={cap} window={window_hours}h");
      fs.writeFileSync(path.join(promptsDir, "optimize_token_consumption_context.md"), "OPTIMIZE CONTEXT guardrail={guardrail_name} run={run_url}");

      process.env.RUNNER_TEMP = tmpDir;
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/123456";
      process.env.GH_AW_AGENT_CONCLUSION = "failure";
      process.env.GITHUB_HEAD_REF = "feature/test";
      process.env.GITHUB_WORKSPACE = tmpDir;
    });

    afterEach(() => {
      delete process.env.RUNNER_TEMP;
      delete process.env.GH_AW_WORKFLOW_NAME;
      delete process.env.GH_AW_WORKFLOW_ID;
      delete process.env.GH_AW_RUN_URL;
      delete process.env.GH_AW_AGENT_CONCLUSION;
      delete process.env.GITHUB_HEAD_REF;
      delete process.env.GITHUB_WORKSPACE;
      delete process.env.GH_AW_FAILURE_CATEGORIES_FILTER;
      delete process.env.GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER;
      delete process.env.GH_AW_INFERENCE_ACCESS_ERROR;

      if (tmpDir && fs.existsSync(tmpDir)) {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      }
    });

    it.each([
      {
        name: "include-only filter creates issue when category matches",
        includeFilter: ["agent_failure"],
        excludeFilter: null,
        extraEnv: {},
        shouldCreateIssue: true,
      },
      {
        name: "include-only filter skips issue when category does not match",
        includeFilter: ["missing_safe_outputs"],
        excludeFilter: null,
        extraEnv: {},
        shouldCreateIssue: false,
      },
      {
        name: "include-only filter skips infra-classified failures when only agent_failure is included",
        includeFilter: ["agent_failure"],
        excludeFilter: null,
        extraEnv: { GH_AW_INFERENCE_ACCESS_ERROR: "true" },
        shouldCreateIssue: false,
      },
      {
        name: "exclude-only filter skips issue when category is excluded",
        includeFilter: null,
        excludeFilter: ["agent_failure"],
        extraEnv: {},
        shouldCreateIssue: false,
      },
      {
        name: "mixed include+exclude filter skips issue when excluded category is present",
        includeFilter: ["agent_failure"],
        excludeFilter: ["agent_failure"],
        extraEnv: {},
        shouldCreateIssue: false,
      },
    ])("$name", async ({ includeFilter, excludeFilter, extraEnv, shouldCreateIssue }) => {
      const { createIssueMock } = setupGithubMock();
      if (includeFilter) {
        process.env.GH_AW_FAILURE_CATEGORIES_FILTER = JSON.stringify(includeFilter);
      }
      if (excludeFilter) {
        process.env.GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER = JSON.stringify(excludeFilter);
      }
      for (const [key, value] of Object.entries(extraEnv || {})) {
        process.env[key] = value;
      }

      await main();

      if (shouldCreateIssue) {
        expect(createIssueMock).toHaveBeenCalledOnce();
      } else {
        expect(createIssueMock).not.toHaveBeenCalled();
      }
    });
  });
});
