// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const os = require("os");

const {
  main,
  getReadableTokenUsagePaths,
  extractRequestId,
  extractTokenUsageDedupeKey,
  readDedupedTokenUsage,
  getSummaryTitle,
  buildStepSummarySection,
  buildWorkingSetDetailsSection,
  renderTokenTableAsPlainText,
  TOKEN_USAGE_AUDIT_PATH,
  TOKEN_USAGE_PATH,
  TOKEN_USAGE_AWF_AUDIT_PATH,
  TOKEN_USAGE_PATHS,
  AGENT_USAGE_PATH,
  DEFAULT_SUMMARY_TITLE,
} = require("./parse_token_usage.cjs");

describe("parse_token_usage", () => {
  const singleEntry = JSON.stringify({
    model: "claude-sonnet-4-6",
    provider: "anthropic",
    input_tokens: 100,
    output_tokens: 200,
    cache_read_tokens: 5000,
    cache_write_tokens: 3000,
    duration_ms: 2500,
  });

  const multiEntry = [
    JSON.stringify({ model: "claude-sonnet-4-6", provider: "anthropic", input_tokens: 100, output_tokens: 200, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 1000 }),
    JSON.stringify({ model: "gpt-4o", provider: "openai", input_tokens: 50, output_tokens: 80, cache_read_tokens: 0, cache_write_tokens: 0, duration_ms: 500 }),
  ].join("\n");

  describe("constant paths", () => {
    test("TOKEN_USAGE_AUDIT_PATH points to firewall audit log file", () => {
      expect(TOKEN_USAGE_AUDIT_PATH).toBe("/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_PATH points to firewall proxy log file", () => {
      expect(TOKEN_USAGE_PATH).toBe("/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_AWF_AUDIT_PATH points to firewall AWF audit log file", () => {
      expect(TOKEN_USAGE_AWF_AUDIT_PATH).toBe("/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl");
    });

    test("TOKEN_USAGE_PATHS includes legacy, AWF audit, and proxy log paths", () => {
      expect(TOKEN_USAGE_PATHS).toEqual([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_AWF_AUDIT_PATH, TOKEN_USAGE_PATH]);
    });

    test("AGENT_USAGE_PATH points to agent_usage.json", () => {
      expect(AGENT_USAGE_PATH).toBe("/tmp/gh-aw/agent_usage.json");
    });

    test("DEFAULT_SUMMARY_TITLE points to Token Usage", () => {
      expect(DEFAULT_SUMMARY_TITLE).toBe("Token Usage");
    });
  });

  describe("main function", () => {
    let tmpDir;
    let mockCore;
    let originalAppendFileSync;
    let originalExistsSync;
    let originalStatSync;
    let originalReadFileSync;
    let originalWriteFileSync;

    beforeEach(() => {
      tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "parse-token-usage-test-"));
      delete process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE;
      process.env.GITHUB_STEP_SUMMARY = "";

      mockCore = {
        info: vi.fn(),
        debug: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
        setFailed: vi.fn(),
        exportVariable: vi.fn(),
        setOutput: vi.fn(),
        summary: {
          addDetails: vi.fn().mockReturnThis(),
          addRaw: vi.fn().mockReturnThis(),
          write: vi.fn().mockResolvedValue(undefined),
        },
      };

      global.core = mockCore;

      originalAppendFileSync = fs.appendFileSync;
      originalExistsSync = fs.existsSync;
      originalStatSync = fs.statSync;
      originalReadFileSync = fs.readFileSync;
      originalWriteFileSync = fs.writeFileSync;

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return "";
        return originalReadFileSync(p, enc);
      });
    });

    afterEach(() => {
      fs.appendFileSync = originalAppendFileSync;
      fs.existsSync = originalExistsSync;
      fs.statSync = originalStatSync;
      fs.readFileSync = originalReadFileSync;
      fs.writeFileSync = originalWriteFileSync;
      delete global.core;
      fs.rmSync(tmpDir, { recursive: true, force: true });
    });

    /**
     * @param {string} summaryText
     * @param {Array<[string, string, string]>} rows [alias, input, output]
     */
    function expectTokenUsageTableRows(summaryText, rows) {
      const escapeRegex = value => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      expect(summaryText).toContain("| # | Alias | Input | Output |");
      for (const [alias, input, output] of rows) {
        const aliasPattern = escapeRegex(alias);
        const inputPattern = escapeRegex(input);
        const outputPattern = escapeRegex(output);
        expect(summaryText).toMatch(new RegExp(`\\|\\s*\\d+\\s*\\|\\s*${aliasPattern}\\s*\\|\\s*${inputPattern}\\s*\\|\\s*${outputPattern}\\s*\\|`));
      }
    }

    test("skips summary when token usage file does not exist", async () => {
      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No token usage data found"));
      expect(mockCore.summary.addDetails).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    test("skips summary when token usage file is empty", async () => {
      const emptyFile = path.join(tmpDir, "token-usage.jsonl");
      fs.writeFileSync(emptyFile, "");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: 0 };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });

      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No token usage data found"));
      expect(mockCore.summary.addDetails).not.toHaveBeenCalled();
    });

    test("writes token usage details section to summary", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<summary>Token Usage</summary>"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("| Alias |"), true);
      expect(mockCore.summary.write).toHaveBeenCalled();
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Token usage summary appended"));
      // Token table should also be rendered to core.info
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Token Usage"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Alias"));
    });

    test("keeps threat-detection title and rows with AWF-reported credits", async () => {
      process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE = "Threat Detection Token Usage";
      const reportedEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 19288,
        output_tokens: 35,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 2242,
        ai_credits_this_response: 0.29142,
        ai_credits_total: 0.29142,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: reportedEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return reportedEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("<summary>Threat Detection Token Usage</summary>"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("gpt40mini"), true);
      expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("0.29142"), true);
    });

    test("appends token usage section to GITHUB_STEP_SUMMARY when configured", async () => {
      const stepSummaryPath = path.join(tmpDir, "step-summary.md");
      process.env.GITHUB_STEP_SUMMARY = stepSummaryPath;
      fs.appendFileSync = vi.fn((...args) => originalAppendFileSync(...args));

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      const stepSummary = originalReadFileSync(stepSummaryPath, "utf8");
      expect(stepSummary).toContain("<summary>Token Usage</summary>");
      expect(stepSummary).toContain("Per-request AI credits and token totals");
      expect(stepSummary).toContain("Working-Set Rebuild Factor (WSRF): 1.00× (measured)");
      expect(stepSummary).toContain("- Cumulative input tokens: 100");
      expect(stepSummary).toContain("| ΔAI Credits | AI Credits |");
      expect(fs.appendFileSync).toHaveBeenCalledWith(stepSummaryPath, expect.any(String), "utf8");
      expect(mockCore.summary.addRaw).not.toHaveBeenCalled();
      expect(mockCore.summary.write).not.toHaveBeenCalled();
    });

    test("writes agent_usage.json with aggregated token totals and primary_model", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      expect(fs.existsSync(agentUsageFile)).toBe(true);
      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(100);
      expect(agentUsage.output_tokens).toBe(200);
      expect(agentUsage.cache_read_tokens).toBe(5000);
      expect(agentUsage.cache_write_tokens).toBe(3000);
      expect(agentUsage.ambient_context).toBe(900);
      expect(typeof agentUsage.ai_credits).toBe("number");
      // primary_model is the actual model from token-usage data (not a user alias)
      expect(agentUsage.primary_model).toBe("claude-sonnet-4-6");
      // GH_AW_PRIMARY_MODEL is exported so footer attribution can use the real model name
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_PRIMARY_MODEL", "claude-sonnet-4-6");
      expect(mockCore.setOutput).toHaveBeenCalledWith("primary_model", "claude-sonnet-4-6");
    });

    test("writes the exact AWF-reported AIC total without repricing", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const fixtureContent = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: fixtureContent.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return fixtureContent;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.ai_credits).toBe(1.03602);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "1.03602");
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "1.03602");
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("1.03602"));
    });

    test("exports AWF-reported zero AIC instead of treating it as missing", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const zeroEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 1000,
        output_tokens: 100,
        ai_credits_this_response: 0,
        ai_credits_total: 0,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: zeroEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return zeroEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.ai_credits).toBe(0);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "0");
      expect(mockCore.setOutput).toHaveBeenCalledWith("aic", "0");
    });

    test("surfaces fallback accounting warnings", async () => {
      const malformedEntry = JSON.stringify({
        model: "gpt-4o-mini-2024-07-18",
        provider: "copilot",
        input_tokens: 19288,
        output_tokens: 35,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        ai_credits_this_response: null,
        ai_credits_total: null,
      });

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: malformedEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return malformedEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });

      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("[ai-credits]"));
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("fallback accounting"));
    });

    test("handles multiple model entries", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: multiEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return multiEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expect(summaryCall[0]).toContain("<summary>Token Usage</summary>");
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["gpt40", "50", "80"],
      ]);
      expect(summaryCall[0]).toContain("**Total**");

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(150);
      expect(agentUsage.output_tokens).toBe(280);
    });

    test("reads token usage from firewall-audit-logs path", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return true;
        if (p === TOKEN_USAGE_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: multiEntry.length };
        if (p === TOKEN_USAGE_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return multiEntry;
        if (p === TOKEN_USAGE_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["gpt40", "50", "80"],
      ]);

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(150);
      expect(agentUsage.output_tokens).toBe(280);
    });

    test("deduplicates overlapping entries across audit and legacy token usage files", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const sharedEntry = JSON.stringify({
        request_id: "req-shared",
        model: "claude-sonnet-4-6",
        provider: "anthropic",
        input_tokens: 100,
        output_tokens: 200,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 1000,
      });
      const auditOnlyEntry = JSON.stringify({
        request_id: "req-audit",
        model: "claude-haiku-4-5",
        provider: "anthropic",
        input_tokens: 50,
        output_tokens: 75,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 500,
      });
      const legacyOnlyEntry = JSON.stringify({
        request_id: "req-legacy",
        model: "gpt-4o",
        provider: "openai",
        input_tokens: 20,
        output_tokens: 30,
        cache_read_tokens: 0,
        cache_write_tokens: 0,
        duration_ms: 400,
      });

      const auditContent = [sharedEntry, auditOnlyEntry].join("\n");
      const legacyContent = [sharedEntry, legacyOnlyEntry].join("\n");

      fs.existsSync = vi.fn(p => (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH ? true : originalExistsSync(p)));
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: auditContent.length };
        if (p === TOKEN_USAGE_PATH) return { size: legacyContent.length };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return auditContent;
        if (p === TOKEN_USAGE_PATH) return legacyContent;
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const summaryCall = mockCore.summary.addRaw.mock.calls[0];
      expectTokenUsageTableRows(summaryCall[0], [
        ["sonnet46", "100", "200"],
        ["haiku45", "50", "75"],
        ["gpt40", "20", "30"],
      ]);

      const agentUsage = JSON.parse(fs.readFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(170);
      expect(agentUsage.output_tokens).toBe(305);
    });

    test("deduplicates mirrored AWF token usage files before writing agent_usage", async () => {
      const agentUsageFile = path.join(tmpDir, "agent_usage.json");
      const fixtureContent = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");

      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return { size: fixtureContent.length };
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return fixtureContent;
        if (p === TOKEN_USAGE_AWF_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn((p, data) => {
        if (p === AGENT_USAGE_PATH) {
          originalWriteFileSync(agentUsageFile, data);
        } else {
          originalWriteFileSync(p, data);
        }
      });

      await main();

      const agentUsage = JSON.parse(originalReadFileSync(agentUsageFile, "utf8"));
      expect(agentUsage.input_tokens).toBe(39376);
      expect(agentUsage.output_tokens).toBe(175);
      expect(agentUsage.ai_credits).toBe(1.03602);
      expect(mockCore.exportVariable).toHaveBeenCalledWith("GH_AW_AIC", "1.03602");
    });

    test("calls setFailed when an error is thrown", async () => {
      fs.existsSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return true;
        if (p === TOKEN_USAGE_AUDIT_PATH) return false;
        return originalExistsSync(p);
      });
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_PATH) return { size: singleEntry.length };
        if (p === TOKEN_USAGE_AUDIT_PATH) return { size: 0 };
        return originalStatSync(p);
      });
      fs.readFileSync = vi.fn((p, enc) => {
        if (p === TOKEN_USAGE_PATH) return singleEntry;
        if (p === TOKEN_USAGE_AUDIT_PATH) return "";
        return originalReadFileSync(p, enc);
      });
      fs.writeFileSync = vi.fn(p => {
        if (p === AGENT_USAGE_PATH) throw new Error("write error");
      });

      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("write error"));
    });
  });

  describe("helpers", () => {
    let originalExistsSync;
    let originalStatSync;
    let originalReadFileSync;

    beforeEach(() => {
      originalExistsSync = fs.existsSync;
      originalStatSync = fs.statSync;
      originalReadFileSync = fs.readFileSync;
      global.core = { warning: vi.fn() };
    });

    afterEach(() => {
      fs.existsSync = originalExistsSync;
      fs.statSync = originalStatSync;
      fs.readFileSync = originalReadFileSync;
      delete global.core;
    });

    test("extractRequestId reads request_id without parsing JSON", () => {
      expect(extractRequestId('{"request_id":"req-123","model":"m"}')).toBe("req-123");
      expect(extractRequestId('{"model":"m"}')).toBe("");
    });

    test("extractTokenUsageDedupeKey includes event and request_id", () => {
      expect(extractTokenUsageDedupeKey('{"event":"token_usage","request_id":"req-123","model":"m"}')).toBe("token_usage:req-123");
      expect(extractTokenUsageDedupeKey('{"event":"other","request_id":"req-123","model":"m"}')).toBe("other:req-123");
      expect(extractTokenUsageDedupeKey('{"request_id":"req-123","model":"m"}')).toBe("token_usage:req-123");
      expect(extractTokenUsageDedupeKey('{"model":"m"}')).toBe("");
    });

    test("getReadableTokenUsagePaths skips failing stat path and keeps valid path", () => {
      fs.existsSync = vi.fn(p => p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH);
      fs.statSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) throw new Error("stat fail");
        if (p === TOKEN_USAGE_PATH) return { size: 42 };
        return originalStatSync(p);
      });

      const paths = getReadableTokenUsagePaths(TOKEN_USAGE_PATHS);
      expect(paths).toEqual([TOKEN_USAGE_PATH]);
    });

    test("readDedupedTokenUsage deduplicates by request_id across files", () => {
      const fileA = '{"request_id":"req-1","model":"m1","input_tokens":1}\n{"request_id":"req-2","model":"m2","input_tokens":2}';
      const fileB = '{"request_id":"req-1","model":"m1","input_tokens":1}\n{"request_id":"req-3","model":"m3","input_tokens":3}';

      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return fileA;
        if (p === TOKEN_USAGE_PATH) return fileB;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      expect(deduped).toContain('"request_id":"req-1"');
      expect(deduped).toContain('"request_id":"req-2"');
      expect(deduped).toContain('"request_id":"req-3"');
      expect(deduped.match(/"request_id":"req-1"/g)).toHaveLength(1);
    });

    test("readDedupedTokenUsage keeps different events with the same request_id", () => {
      const fileA = '{"event":"token_usage","request_id":"req-1","model":"m1","input_tokens":1}';
      const fileB = '{"event":"token_steering","request_id":"req-1","model":"m1","input_tokens":2}';

      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH) return fileA;
        if (p === TOKEN_USAGE_PATH) return fileB;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      expect(deduped).toContain('"event":"token_usage"');
      expect(deduped).toContain('"event":"token_steering"');
      expect(deduped.match(/"request_id":"req-1"/g)).toHaveLength(2);
    });

    test("deduplicates mirrored AWF records before aggregating reported credits", () => {
      const fixture = originalReadFileSync(path.join(__dirname, "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"), "utf8");
      fs.readFileSync = vi.fn(p => {
        if (p === TOKEN_USAGE_AUDIT_PATH || p === TOKEN_USAGE_PATH) return fixture;
        return originalReadFileSync(p, "utf8");
      });

      const deduped = readDedupedTokenUsage([TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_PATH]);
      const { parseTokenUsageJsonl } = require("./parse_mcp_gateway_log.cjs");
      const summary = parseTokenUsageJsonl(deduped);

      expect(summary.totalRequests).toBe(5);
      expect(summary.totalAIC).toBe(1.03602);
    });

    test("getSummaryTitle returns trimmed env title", () => {
      process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE = "  Threat Detection Token Usage  ";
      expect(getSummaryTitle()).toBe("Threat Detection Token Usage");
    });

    test("getSummaryTitle falls back to default title", () => {
      delete process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE;
      expect(getSummaryTitle()).toBe("Token Usage");
    });

    test("buildStepSummarySection wraps markdown in a heading and details block", () => {
      const section = buildStepSummarySection("Token Usage", "| Alias |\n| --- |");
      expect(section).toContain("<details>");
      expect(section).toContain("<summary>Token Usage</summary>");
      expect(section).toContain("Per-request AI credits and token totals");
    });

    test("buildStepSummarySection emits the WSRF block after the token usage details block", () => {
      const section = buildStepSummarySection("Token Usage", "| Alias |\n| --- |\n", {
        measurement_state: "measured",
        rebuild_factor: 1,
        cumulative_input_tokens: 100,
        peak_input_tokens: 100,
        rebuild_excess_tokens: 0,
        invocations: 1,
      });

      const tableIndex = section.indexOf("| Alias |");
      const closingIndex = section.indexOf("</details>");
      const wsrfIndex = section.indexOf("Working-Set Rebuild Factor");
      expect(tableIndex).toBeGreaterThan(-1);
      expect(tableIndex).toBeLessThan(closingIndex);
      expect(wsrfIndex).toBeGreaterThan(closingIndex);
    });

    test("buildWorkingSetDetailsSection renders measured WSRF details", () => {
      const section = buildWorkingSetDetailsSection({
        measurement_state: "measured",
        rebuild_factor: 3.9017857142857144,
        cumulative_input_tokens: 874000,
        peak_input_tokens: 224000,
        rebuild_excess_tokens: 650000,
        invocations: 5,
      });

      expect(section).toContain("Working-Set Rebuild Factor (WSRF): 3.90× (measured)");
      expect(section).toContain("- State: `measured`");
      expect(section).toContain("- Cumulative input tokens: 874,000");
      expect(section).toContain("- Peak invocation input tokens: 224,000");
      expect(section).toContain("- Rebuild excess tokens: 650,000");
      expect(section).toContain("- Invocations: 5");
    });

    test("buildWorkingSetDetailsSection renders unavailable state when no factor exists", () => {
      const section = buildWorkingSetDetailsSection({
        measurement_state: "unavailable",
        cumulative_input_tokens: 0,
        peak_input_tokens: 0,
        rebuild_excess_tokens: 0,
        invocations: 0,
      });

      expect(section).toContain("Working-Set Rebuild Factor (WSRF): unavailable (unavailable)");
      expect(section).toContain("- State: `unavailable`");
    });

    test("renderTokenTableAsPlainText strips table separator lines and pipes", () => {
      const markdown = ["| # | Alias | Input | Output |", "|--:|-------|------:|-------:|", "| 1 | sonnet46 | 100 | 200 |", "| **Total** | | **100** | **200** |", "", "Legend: `Alias` is the model shorthand.", ""].join("\n");

      const result = renderTokenTableAsPlainText("Token Usage", markdown);

      expect(result).toContain("Token Usage");
      // separator line is removed (no dash sequences that leak from separator rows)
      expect(result).not.toMatch(/---/);
      // leading/trailing pipes are stripped
      expect(result).not.toMatch(/^\|/m);
      expect(result).not.toMatch(/\|$/m);
      // bold markers are removed
      expect(result).not.toContain("**");
      // data is preserved
      expect(result).toContain("sonnet46");
      expect(result).toContain("100");
      expect(result).toContain("200");
      expect(result).toContain("Legend:");
    });

    test("renderTokenTableAsPlainText prefixes output with title", () => {
      const result = renderTokenTableAsPlainText("My Token Usage", "| A |\n|---|\n| 1 |");
      expect(result.startsWith("My Token Usage")).toBe(true);
    });
  });
});
