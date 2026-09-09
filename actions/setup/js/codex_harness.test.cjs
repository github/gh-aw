import { describe, it, expect } from "vitest";
import { spawnSync } from "child_process";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const {
  resolveCodexPromptFileArgs,
  injectJsonFlag,
  isRateLimitError,
  isTokenPerMinuteRateLimitError,
  isAuthenticationFailedError,
  isMissingApiKeyError,
  isServerError,
  isInvalidModelError,
  isUnsupportedModelToolsError,
  isInvalidRequestError,
  isReconnectExhaustedError,
  countPermissionDeniedIssues,
  hasNumerousPermissionDeniedIssues,
  extractDeniedCommands,
  buildMissingToolPermissionIssuePayload,
  buildCodexChildEnv,
  extractPortFromURL,
  extractOpenAIProxyBaseURLFromToml,
  getConfiguredProviderPortFromReflect,
  validateCodexOpenAIBaseURLFromReflect,
  configureCodexProviderFromReflect,
  hasNoopInSafeOutputs,
  resolveRetryConfig,
  resolveContextRebuildCircuitBreakerConfig,
  evaluateContextRebuildCircuitBreaker,
  evaluateContextRebuildCircuitBreakerForAttempt,
  readWorkingSetFromTokenUsage,
  TOKEN_USAGE_PATHS,
  DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT,
  DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS,
  DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS,
  DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS,
  resolvePostResultWatchdogIdleTimeoutMs,
  DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS,
  MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS,
  MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS,
} = require("./codex_harness.cjs");
const { detectNonRetryableHarnessGuard } = require("./harness_retry_guard.cjs");

const agentTempDir = "/tmp/gh-aw/agent";

function makeHarnessTempDir(name) {
  fs.mkdirSync(agentTempDir, { recursive: true });
  return fs.mkdtempSync(path.join(agentTempDir, name));
}

describe("codex_harness.cjs", () => {
  describe("resolveCodexPromptFileArgs", () => {
    it("replaces --prompt-file with the file's content as the last positional arg", () => {
      const promptFile = path.join(os.tmpdir(), `codex-harness-prompt-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "fix the bug", "utf8");
      try {
        const result = resolveCodexPromptFileArgs(["exec", "--dangerously-bypass-approvals-and-sandbox", "--prompt-file", promptFile]);
        expect(result).toEqual(["exec", "--dangerously-bypass-approvals-and-sandbox", "fix the bug"]);
      } finally {
        fs.rmSync(promptFile);
      }
    });

    it("appends prompt content as the last arg when only --prompt-file is provided", () => {
      const promptFile = path.join(os.tmpdir(), `codex-harness-prompt-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "my task", "utf8");
      try {
        const result = resolveCodexPromptFileArgs(["--prompt-file", promptFile]);
        expect(result).toEqual(["my task"]);
      } finally {
        fs.rmSync(promptFile);
      }
    });

    it("passes through args that have no --prompt-file", () => {
      const result = resolveCodexPromptFileArgs(["exec", "--dangerously-bypass-approvals-and-sandbox"]);
      expect(result).toEqual(["exec", "--dangerously-bypass-approvals-and-sandbox"]);
    });

    it("preserves args when --prompt-file is provided without a path", () => {
      const result = resolveCodexPromptFileArgs(["exec", "--prompt-file"]);
      // When no path follows --prompt-file, it is preserved as-is
      expect(result).toEqual(["exec", "--prompt-file"]);
    });

    it("throws when the prompt file does not exist", () => {
      const missingFile = path.join(os.tmpdir(), `codex-harness-missing-${Date.now()}.txt`);
      expect(() => resolveCodexPromptFileArgs(["--prompt-file", missingFile])).toThrow(`--prompt-file '${missingFile}' is not readable`);
    });

    it("throws when the prompt file cannot be read (directory)", () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-harness-dir-"));
      try {
        expect(() => resolveCodexPromptFileArgs(["--prompt-file", dir])).toThrow(`--prompt-file '${dir}' is not readable`);
      } finally {
        fs.rmdirSync(dir);
      }
    });
  });

  describe("isRateLimitError", () => {
    it("returns true for rate_limit_exceeded error", () => {
      expect(isRateLimitError("Error: rate_limit_exceeded")).toBe(true);
    });

    describe("isTokenPerMinuteRateLimitError", () => {
      it("returns true for OpenAI TPM-limit wording", () => {
        expect(isTokenPerMinuteRateLimitError("Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 166655, Requested 35398. Please try again in 615ms.")).toBe(true);
      });

      it("returns false for generic rate-limit wording", () => {
        expect(isTokenPerMinuteRateLimitError("rate_limit_exceeded")).toBe(false);
      });

      it("returns false for unrelated mention of tokens per min", () => {
        expect(isTokenPerMinuteRateLimitError("rate_limit_exceeded while printing docs about 'on tokens per min'")).toBe(false);
      });
    });

    it("returns true for 429 Too Many Requests", () => {
      expect(isRateLimitError("429 Too Many Requests")).toBe(true);
    });

    it("returns true for RateLimitError", () => {
      expect(isRateLimitError("RateLimitError: You exceeded your current quota")).toBe(true);
    });

    it("returns true for 'Rate limit reached for' human-readable message", () => {
      expect(isRateLimitError("Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): " + "Limit 200000, Used 166655, Requested 35398. Please try again in 615ms.")).toBe(true);
    });

    it("returns false for unrelated errors", () => {
      expect(isRateLimitError("Error: ENOENT: no such file")).toBe(false);
      expect(isRateLimitError("Fatal: out of memory")).toBe(false);
      expect(isRateLimitError("")).toBe(false);
    });

    it("returns false for a 500 server error", () => {
      expect(isRateLimitError("500 Internal Server Error")).toBe(false);
    });
  });

  describe("isAuthenticationFailedError", () => {
    it("returns true for authentication failed with request id", () => {
      expect(isAuthenticationFailedError("Authentication failed (Request ID: C818:3ED713:19D401B:1C446B7:69D653CA)")).toBe(true);
    });

    it("returns false for non-authentication-failed output", () => {
      expect(isAuthenticationFailedError("No authentication information found")).toBe(false);
      expect(isAuthenticationFailedError("rate_limit_exceeded")).toBe(false);
    });
  });

  describe("isMissingApiKeyError", () => {
    it("returns true for missing OPENAI_API_KEY with backtick delimiters", () => {
      expect(isMissingApiKeyError("ERROR: Missing environment variable: `OPENAI_API_KEY`")).toBe(true);
    });

    it("returns true for missing CODEX_API_KEY with backtick delimiters", () => {
      expect(isMissingApiKeyError("ERROR: Missing environment variable: `CODEX_API_KEY`")).toBe(true);
    });

    it("returns true for missing OPENAI_API_KEY without backtick delimiters", () => {
      expect(isMissingApiKeyError("Missing environment variable: OPENAI_API_KEY")).toBe(true);
    });

    it("returns true when the error appears within a larger output block", () => {
      const output = "Starting codex...\nERROR: Missing environment variable: `OPENAI_API_KEY`\nExiting.";
      expect(isMissingApiKeyError(output)).toBe(true);
    });

    it("returns false for unrelated errors", () => {
      expect(isMissingApiKeyError("Authentication failed")).toBe(false);
      expect(isMissingApiKeyError("rate_limit_exceeded")).toBe(false);
      expect(isMissingApiKeyError("Missing environment variable: HOME")).toBe(false);
      expect(isMissingApiKeyError("")).toBe(false);
    });
  });

  describe("injectJsonFlag", () => {
    it("injects --json after exec when not already present", () => {
      expect(injectJsonFlag(["exec", "--dangerously-bypass-approvals-and-sandbox", "do the thing"])).toEqual(["exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "do the thing"]);
    });

    it("does not inject --json when already present", () => {
      expect(injectJsonFlag(["exec", "--json", "--skip-git-repo-check", "do the thing"])).toEqual(["exec", "--json", "--skip-git-repo-check", "do the thing"]);
    });

    it("does not inject --json for non-exec subcommands", () => {
      expect(injectJsonFlag(["resume", "--last", "fix it"])).toEqual(["resume", "--last", "fix it"]);
    });

    it("returns empty array unchanged", () => {
      expect(injectJsonFlag([])).toEqual([]);
    });
  });

  describe("buildCodexChildEnv", () => {
    it("preserves captured keys even when base environment is missing them", () => {
      const result = buildCodexChildEnv({ PATH: "/usr/bin" }, "codex-key", "openai-key");
      expect(result.CODEX_API_KEY).toBe("codex-key");
      expect(result.OPENAI_API_KEY).toBe("openai-key");
      expect(result.PATH).toBe("/usr/bin");
    });

    it("does not add unset keys", () => {
      const result = buildCodexChildEnv({ PATH: "/usr/bin" }, undefined, undefined);
      expect(result.CODEX_API_KEY).toBeUndefined();
      expect(result.OPENAI_API_KEY).toBeUndefined();
    });
  });

  describe("context rebuild circuit breaker", () => {
    it("uses defaults when env overrides are absent or invalid", () => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_MAX_REBUILD_FACTOR: "nope",
        GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "-1",
        GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "0",
        GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "0",
      });
      expect(cfg.enabled).toBe(true);
      expect(cfg.maxRebuildFactor).toBe(DEFAULT_CONTEXT_REBUILD_FACTOR_LIMIT);
      expect(cfg.minCumulativeInputTokens).toBe(DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS);
      expect(cfg.pollIntervalMs).toBe(DEFAULT_CONTEXT_REBUILD_POLL_INTERVAL_MS);
      expect(cfg.termGraceMs).toBe(DEFAULT_CONTEXT_REBUILD_TERM_GRACE_MS);
    });

    it("supports explicit disable via env", () => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: "false",
      });
      expect(cfg.enabled).toBe(false);
    });

    it("trips only when both rebuild factor and cumulative input exceed thresholds", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(
        evaluateContextRebuildCircuitBreaker(
          {
            measurement_state: "measured",
            rebuild_factor: 4.5,
            cumulative_input_tokens: 1400,
            peak_input_tokens: 311,
            rebuild_excess_tokens: 1089,
            invocations: 5,
          },
          config
        ).terminate
      ).toBe(true);
      expect(
        evaluateContextRebuildCircuitBreaker(
          {
            measurement_state: "measured",
            rebuild_factor: 4.5,
            cumulative_input_tokens: 999,
            peak_input_tokens: 222,
            rebuild_excess_tokens: 777,
            invocations: 4,
          },
          config
        ).terminate
      ).toBe(false);
    });

    it("falls back to the default cumulative floor for fractional overrides below one token", () => {
      const cfg = resolveContextRebuildCircuitBreakerConfig({
        GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "0.5",
      });
      expect(cfg.minCumulativeInputTokens).toBe(DEFAULT_CONTEXT_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS);
    });

    it("does not trip when rebuild_factor is just below the threshold", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 3.99, cumulative_input_tokens: 2000 }, config).terminate).toBe(false);
    });

    it("trips when rebuild_factor is exactly at the threshold", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, config).terminate).toBe(true);
    });

    it("does not trip after a terminal safe-output was produced in the current attempt", () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-rebuild-safe-output-"));
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"done"}\n', "utf8");
      const logs = [];
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt(
          { rebuild_factor: 4, cumulative_input_tokens: 2000 },
          { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 },
          { safeOutputsPath, safeOutputsByteOffset: 0, logger: msg => logs.push(msg) }
        );
        expect(decision.terminate).toBe(false);
        expect(logs.some(line => line.includes("allowing Codex to exit normally"))).toBe(true);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("does not trip after a report_incomplete safe-output was produced in the current attempt", () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-rebuild-report-incomplete-"));
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"report_incomplete","reason":"blocked"}\n', "utf8");
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 }, { safeOutputsPath, safeOutputsByteOffset: 0 });
        expect(decision.terminate).toBe(false);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("still trips when terminal safe-output predates the current attempt", () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-rebuild-stale-safe-output-"));
      const safeOutputsPath = path.join(dir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"old"}\n', "utf8");
      const byteOffset = fs.statSync(safeOutputsPath).size;
      try {
        const decision = evaluateContextRebuildCircuitBreakerForAttempt({ rebuild_factor: 4, cumulative_input_tokens: 2000 }, { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 }, { safeOutputsPath, safeOutputsByteOffset: byteOffset });
        expect(decision.terminate).toBe(true);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("does not trip for null, empty, or non-finite working sets", () => {
      const config = { maxRebuildFactor: 4, minCumulativeInputTokens: 1000 };
      expect(evaluateContextRebuildCircuitBreaker(null, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({}, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: Number.NaN, cumulative_input_tokens: 5000 }, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: Number.POSITIVE_INFINITY, cumulative_input_tokens: 5000 }, config).terminate).toBe(false);
      expect(evaluateContextRebuildCircuitBreaker({ rebuild_factor: 9, cumulative_input_tokens: Number.NaN }, config).terminate).toBe(false);
    });

    it("accepts a rebuild factor threshold of exactly 1", () => {
      expect(resolveContextRebuildCircuitBreakerConfig({ GH_AW_CODEX_MAX_REBUILD_FACTOR: "1" }).maxRebuildFactor).toBe(1);
    });

    it("skips token-usage candidates whose measurements are unavailable", async () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-token-usage-"));
      const malformed = path.join(dir, "malformed.jsonl");
      const valid = path.join(dir, "valid.jsonl");
      fs.writeFileSync(malformed, "not json\n{oops\n");
      fs.writeFileSync(valid, `${JSON.stringify({ input_tokens: 100 })}\n${JSON.stringify({ input_tokens: 900 })}\n`);
      try {
        const workingSet = await readWorkingSetFromTokenUsage([malformed, valid]);
        expect(workingSet).not.toBeNull();
        expect(workingSet.measurement_state).toBe("measured");
        expect(workingSet.cumulative_input_tokens).toBe(1000);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("prefers the most recently written token-usage candidate", async () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-token-usage-"));
      const stale = path.join(dir, "stale.jsonl");
      const fresh = path.join(dir, "fresh.jsonl");
      fs.writeFileSync(stale, `${JSON.stringify({ input_tokens: 7 })}\n`);
      fs.writeFileSync(fresh, `${JSON.stringify({ input_tokens: 500 })}\n${JSON.stringify({ input_tokens: 500 })}\n`);
      const now = Date.now() / 1000;
      fs.utimesSync(stale, now - 600, now - 600);
      fs.utimesSync(fresh, now, now);
      try {
        // `stale` is listed first, but `fresh` has the newer mtime and must win.
        const workingSet = await readWorkingSetFromTokenUsage([stale, fresh]);
        expect(workingSet.cumulative_input_tokens).toBe(1000);
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });

    it("returns null when no candidate yields usable measurements", async () => {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "codex-token-usage-"));
      const malformed = path.join(dir, "malformed.jsonl");
      fs.writeFileSync(malformed, "not json\n");
      try {
        expect(await readWorkingSetFromTokenUsage([malformed, path.join(dir, "missing.jsonl")])).toBeNull();
      } finally {
        fs.rmSync(dir, { recursive: true, force: true });
      }
    });
  });

  describe("OpenAI base URL validation", () => {
    it("extracts port from URL", () => {
      expect(extractPortFromURL("http://172.30.0.30:10000")).toBe(10000);
      expect(extractPortFromURL("https://example.com")).toBeNull();
      expect(extractPortFromURL("not-a-url")).toBeNull();
    });

    describe("configureCodexProviderFromReflect", () => {
      it("configures OPENAI_BASE_URL from reflected github/copilot endpoint", () => {
        const tmpDir = makeHarnessTempDir("codex-provider-");
        const configPath = path.join(tmpDir, "config.toml");
        const reflectPath = path.join(tmpDir, "awf-reflect.json");
        fs.writeFileSync(configPath, `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`, "utf8");
        fs.writeFileSync(reflectPath, JSON.stringify({ endpoints: [{ provider: "copilot", configured: true, port: 10002 }] }), "utf8");
        try {
          const result = configureCodexProviderFromReflect({
            codexConfigPath: configPath,
            reflectPath,
            provider: "github",
          });
          expect(result.configured).toBe(true);
          expect(result.env.OPENAI_BASE_URL).toBe("http://api-proxy:10002");
          expect(fs.readFileSync(configPath, "utf8")).toContain(`base_url = "http://api-proxy:10002"`);
        } finally {
          fs.rmSync(tmpDir, { recursive: true, force: true });
        }
      });
    });

    it("extracts openai-proxy base_url from TOML", () => {
      const toml = `
[history]
persistence = "none"
[model_providers.openai-proxy]
name = "OpenAI AWF proxy"
base_url = "http://172.30.0.30:10000"
env_key = "OPENAI_API_KEY"
`;
      expect(extractOpenAIProxyBaseURLFromToml(toml)).toBe("http://172.30.0.30:10000");
    });

    it("extracts configured provider port from reflect payload", () => {
      const reflect = {
        endpoints: [
          { provider: "anthropic", port: 10001, configured: true },
          { provider: "openai", port: 10000, configured: true },
          { provider: "copilot", port: 10002, configured: true },
        ],
      };
      expect(getConfiguredProviderPortFromReflect(reflect)).toBe(10000);
      expect(getConfiguredProviderPortFromReflect(reflect, "github")).toBe(10002);
    });

    it("returns null for malformed reflect endpoint ports", () => {
      const reflect = {
        endpoints: [{ provider: "openai", port: "not-a-number", configured: true }],
      };
      expect(getConfiguredProviderPortFromReflect(reflect)).toBeNull();
    });

    it("returns null when the selected provider is not configured", () => {
      const reflect = {
        endpoints: [{ provider: "anthropic", port: 10001, configured: true }],
      };
      expect(getConfiguredProviderPortFromReflect(reflect, "github")).toBeNull();
    });

    it("fails validation when config and reflect OpenAI ports mismatch", () => {
      const toml = `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10001"\n`;
      const reflect = JSON.stringify({
        endpoints: [
          { provider: "openai", port: 10000, configured: true },
          { provider: "anthropic", port: 10001, configured: true },
        ],
      });
      const files = {
        "/tmp/codex-config.toml": toml,
        "/tmp/awf-reflect.json": reflect,
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toContain("mismatch");
    });

    it("passes validation when ports match", () => {
      const toml = `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`;
      const reflect = JSON.stringify({
        endpoints: [{ provider: "openai", port: 10000, configured: true }],
      });
      const files = {
        "/tmp/codex-config.toml": toml,
        "/tmp/awf-reflect.json": reflect,
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(true);
    });

    it("validates against the selected GitHub provider", () => {
      const files = {
        "/tmp/codex-config.toml": `[model_providers.openai-proxy]\nbase_url = "http://api-proxy:10002"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "copilot", port: 10002, configured: true }] }),
      };
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        provider: "github",
        readFileSync: filePath => files[filePath],
      });
      expect(result.ok).toBe(true);
    });

    it("fails strictly when /reflect has configured endpoints but none for the selected provider", () => {
      const files = {
        "/tmp/codex-config.toml": `[model_providers.openai-proxy]\nbase_url = "http://172.30.0.30:10000"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "anthropic", port: 10001, configured: true }] }),
      };
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        provider: "github",
        readFileSync: filePath => files[filePath],
      });
      expect(result.ok).toBe(false);
      expect(result.reason).toContain("no configured endpoint for provider");
    });

    it("passes through when TOML lacks openai-proxy section", () => {
      const files = {
        "/tmp/codex-config.toml": `[history]\npersistence = "none"\n`,
        "/tmp/awf-reflect.json": JSON.stringify({ endpoints: [{ provider: "openai", port: 10000, configured: true }] }),
      };
      const readFileSync = filePath => files[filePath];
      const result = validateCodexOpenAIBaseURLFromReflect({
        codexConfigPath: "/tmp/codex-config.toml",
        reflectPath: "/tmp/awf-reflect.json",
        readFileSync,
      });
      expect(result.ok).toBe(true);
    });
  });

  describe("isServerError", () => {
    it("returns true for InternalServerError", () => {
      expect(isServerError("InternalServerError: The server had an error processing your request")).toBe(true);
    });

    describe("isInvalidModelError", () => {
      it("returns true for model-not-supported errors", () => {
        expect(isInvalidModelError("Execution failed: CAPIError: 400 The requested model is not supported.")).toBe(true);
      });

      it("returns true for invalid model name errors", () => {
        expect(isInvalidModelError("invalid model name 'claude-sonnet-999'")).toBe(true);
        expect(isInvalidModelError("model 'gpt-foo' not found")).toBe(true);
        expect(isInvalidModelError("model gpt-unknown is not available")).toBe(true);
        expect(isInvalidModelError("model 'claude-3-5-sonnet@20241022' not found")).toBe(true);
      });

      it("returns true for AIC api-proxy 404 standalone Model not found shape", () => {
        expect(isInvalidModelError("404 Not Found: Model not found")).toBe(true);
        expect(isInvalidModelError("ResponseError: 404 Not Found: Model not found")).toBe(true);
        expect(isInvalidModelError("Error: 404 Model not found")).toBe(true);
      });

      it("returns false for unrelated errors", () => {
        expect(isInvalidModelError("rate_limit_exceeded")).toBe(false);
        expect(isInvalidModelError("unknown model behavior detected")).toBe(false);
        expect(isInvalidModelError("ServiceUnavailableError")).toBe(false);
        expect(isInvalidModelError("")).toBe(false);
      });
    });

    it("returns true for ServiceUnavailableError", () => {
      expect(isServerError("ServiceUnavailableError: The server is temporarily unable to service your request")).toBe(true);
    });

    it("returns true for 500 Internal Server Error", () => {
      expect(isServerError("500 Internal Server Error")).toBe(true);
    });

    it("returns true for 503 Service Unavailable", () => {
      expect(isServerError("503 Service Unavailable")).toBe(true);
    });

    it("returns false for rate limit errors", () => {
      expect(isServerError("rate_limit_exceeded")).toBe(false);
      expect(isServerError("429 Too Many Requests")).toBe(false);
    });

    it("returns false for unrelated errors", () => {
      expect(isServerError("Error: ENOENT: no such file")).toBe(false);
      expect(isServerError("")).toBe(false);
    });
  });

  describe("isInvalidRequestError", () => {
    it("returns true for a provider invalid_request_error payload", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"messages[4].content\\\",\\n    \\\"code\\\": \\\"empty_array\\\"}\"}}}"}}`;
      expect(isInvalidRequestError(output)).toBe(true);
    });

    it("returns true for an unescaped provider error field in a turn.failed event", () => {
      expect(isInvalidRequestError('{"type":"turn.failed","error":{"type":"invalid_request_error"}}')).toBe(true);
    });

    it("returns false for unrelated errors and tool transcript content", () => {
      expect(isInvalidRequestError("rate_limit_exceeded")).toBe(false);
      expect(isInvalidRequestError("500 Internal Server Error")).toBe(false);
      expect(isInvalidRequestError("GitHub API responded with 400 Bad Request")).toBe(false);
      expect(isInvalidRequestError('{"type":"item.completed","item":{"type":"tool_call_output","output":"invalid_request_error"}}')).toBe(false);
      expect(isInvalidRequestError("")).toBe(false);
    });
  });

  describe("isUnsupportedModelToolsError", () => {
    it("returns true for the observed 'tools' unknown_parameter turn.failed event", () => {
      const output =
        '{"type":"thread.started","thread_id":"01a0545e-6060-7472-9d50-d4a643611434"}\n' +
        '{"type":"turn.started"}\n' +
        String.raw`{"type":"error","message":"{\n  \"error\": {\n    \"message\": \"Invalid value: 'custom'\",\n    \"type\": \"invalid_request_error\",\n    \"param\": \"tools\",\n    \"code\": \"unknown_parameter\"\n  }\n}"}` +
        "\n" +
        String.raw`{"type":"turn.failed","error":{"message":"{\n  \"error\": {\n    \"message\": \"Invalid value: 'custom'\",\n    \"type\": \"invalid_request_error\",\n    \"param\": \"tools\",\n    \"code\": \"unknown_parameter\"\n  }\n}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns true regardless of the order of param/code fields", () => {
      const output = '{"type":"turn.failed","error":{"code":"unknown_parameter","param":"tools"}}';
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns true for a provider metadata.raw envelope", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"tools\\\",\\n    \\\"code\\\": \\\"unknown_parameter\\\"}\"}}}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(true);
    });

    it("returns false for an unrelated invalid_request_error (e.g. empty message array)", () => {
      const output = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Provider returned error\",\"code\":400,\"metadata\":{\"raw\":\"{\\\"type\\\": \\\"invalid_request_error\\\",\\n    \\\"param\\\": \\\"messages[4].content\\\",\\n    \\\"code\\\": \\\"empty_array\\\"}\"}}}"}}`;
      expect(isUnsupportedModelToolsError(output)).toBe(false);
    });

    it("returns false for unrelated errors and empty output", () => {
      expect(isUnsupportedModelToolsError("rate_limit_exceeded")).toBe(false);
      expect(isUnsupportedModelToolsError('{"type":"item.completed","item":{"type":"tool_call_output","output":"unknown_parameter tools"}}')).toBe(false);
      expect(isUnsupportedModelToolsError("")).toBe(false);
    });
  });

  describe("permission-denied classification helpers", () => {
    it("counts repeated permission-denied signals", () => {
      const output = "permission denied\npermissions denied\nEACCES: permission denied";
      expect(countPermissionDeniedIssues(output)).toBe(4);
    });

    it("detects numerous permission-denied issues at threshold", () => {
      const output = "permission denied\npermission denied\npermission denied";
      expect(hasNumerousPermissionDeniedIssues(output)).toBe(true);
    });

    it("does not classify sparse permission-denied output as numerous", () => {
      expect(hasNumerousPermissionDeniedIssues("permission denied")).toBe(false);
    });

    it("builds missing_tool payload for permission issues", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload());
      expect(payload.type).toBe("missing_tool");
      expect(payload.reason).toContain("missing tool/permission issue");
      expect(payload.denied_commands).toEqual([]);
    });

    it("builds missing_tool payload with denied commands", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload(["go version", "ls /usr/local/go"]));
      expect(payload.type).toBe("missing_tool");
      expect(payload.denied_commands).toEqual(["go version", "ls /usr/local/go"]);
    });
  });

  describe("extractDeniedCommands", () => {
    it("returns empty array for empty output", () => {
      expect(extractDeniedCommands("")).toEqual([]);
      expect(extractDeniedCommands(null)).toEqual([]);
    });

    it("extracts command from line with box-drawing pipe marker (│) before permission denied", () => {
      const output = ["\u2713 Some successful step", "\u2717 Check if go command works (shell)", "  \u2502 go version 2>&1", "  \u2514 Permission denied and could not request permission from user"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["go version 2>&1"]);
    });

    it("extracts command with plain pipe (|) before permission denied", () => {
      const output = ["| ls -la", "Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["ls -la"]);
    });

    it("deduplicates repeated denied commands", () => {
      const output = ["  \u2502 go version", "  Permission denied", "  \u2502 go version", "  Permission denied", "  \u2502 go version", "  Permission denied"].join("\n");
      const result = extractDeniedCommands(output);
      expect(result).toEqual(["go version"]);
    });

    it("extracts multiple distinct denied commands", () => {
      const output = ["  \u2502 go version 2>&1", "  Permission denied", "  \u2502 ls /usr/local/go/bin/go", "  Permission denied", "  \u2502 which go", "  Permission denied"].join("\n");
      const result = extractDeniedCommands(output);
      expect(result).toContain("go version 2>&1");
      expect(result).toContain("ls /usr/local/go/bin/go");
      expect(result).toContain("which go");
    });

    it("returns empty array when no pipe markers are present before permission denied", () => {
      const output = "Some output\nPermission denied\nMore output";
      expect(extractDeniedCommands(output)).toEqual([]);
    });

    it("looks back up to 3 lines for command context", () => {
      const output = ["  \u2502 make test", "Running...", "Still running...", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual(["make test"]);
    });

    it("does not look back more than 3 lines", () => {
      const output = ["  \u2502 make test", "line2", "line3", "line4", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual([]);
    });

    it("does not capture suffix of a command containing an internal pipe", () => {
      // "find . -name '*.go' | sort" should not match by splitting on the internal |
      const output = ["  find . -name '*.go' | sort", "  Permission denied"].join("\n");
      expect(extractDeniedCommands(output)).toEqual([]);
    });
  });

  describe("retry policy: fresh run on partial execution", () => {
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @returns {boolean}
     */
    function shouldRetry(result, attempt) {
      if (result.exitCode === 0) return false;
      if (attempt === 0 && isAuthenticationFailedError(result.output)) return false;
      if (isMissingApiKeyError(result.output)) return false;
      if (hasNumerousPermissionDeniedIssues(result.output)) return false;
      const nonRetryableGuard = detectNonRetryableHarnessGuard(result.output);
      if (nonRetryableGuard.aiCreditsExceeded || nonRetryableGuard.awfAPIProxyBlockingRequests || nonRetryableGuard.goalAlreadyActive || nonRetryableGuard.maxRunsExceeded) return false;
      const isRateLimit = isRateLimitError(result.output);
      const isTokenPerMinuteRateLimit = isTokenPerMinuteRateLimitError(result.output);
      if (isTokenPerMinuteRateLimit) return false;
      if (isRateLimit && isReconnectExhaustedError(result.output)) return false;
      const isTransient = isRateLimit || isServerError(result.output);
      return attempt < MAX_RETRIES && (result.hasOutput || isTransient);
    }

    it("does not retry a provider invalid_request_error failure", () => {
      const tempDir = makeHarnessTempDir("codex-invalid-request-error-");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stderr.write(JSON.stringify({ type: "turn.failed", error: { message: JSON.stringify({ error: { type: "invalid_request_error" } }) } }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("isInvalidRequestError=true");
      expect(result.stderr).toContain("invalid_request_error (HTTP 400) — not retrying");
    });

    it("does not retry the observed unsupported-model 'tools' unknown_parameter failure", () => {
      const tempDir = makeHarnessTempDir("codex-unsupported-model-tools-");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stderr.write(JSON.stringify({ type: "thread.started", thread_id: "01a0545e-6060-7472-9d50-d4a643611434" }) + "\\n");
process.stderr.write(JSON.stringify({ type: "turn.started" }) + "\\n");
const errorMessage = JSON.stringify({ error: { message: "Invalid value: 'custom'", type: "invalid_request_error", param: "tools", code: "unknown_parameter" } });
process.stderr.write(JSON.stringify({ type: "error", message: errorMessage }) + "\\n");
process.stderr.write(JSON.stringify({ type: "turn.failed", error: { message: errorMessage } }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("isUnsupportedModelToolsError=true");
      expect(result.stderr).toContain("configured model does not support Codex's required tool-calling schema");
      expect(result.stderr).toContain("not retrying");
    });

    it("exits 0 when the AWF API proxy returns HTTP 403 max-AI-credits as an authentication failure", () => {
      // Same proxy signature as the claude_harness.test.cjs regression, replayed against codex.
      // CODEX_API_KEY is set so the `!isMissingApiKey` guard cannot suppress the budget path.
      const tempDir = makeHarnessTempDir("codex-ai-credits-proxy-403-");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
fs.appendFileSync(process.env.CODEX_HARNESS_STUB_CALLS, "called\\n");
process.stdout.write(JSON.stringify({ type: "error", error: "authentication_failed", message: "Failed to authenticate. API Error: 403 Maximum AI credits exceeded (302.111025 / 300)." }) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });

      expect(fs.readFileSync(callsPath, "utf8").trim().split("\n")).toHaveLength(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("trusted budget-abort evidence");
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("retries on rate limit error even without output", () => {
      const result = { exitCode: 1, hasOutput: false, output: "rate_limit_exceeded" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("retries on server error even without output", () => {
      const result = { exitCode: 1, hasOutput: false, output: "InternalServerError" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("retries on any other non-zero exit when session produced output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset" };
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("does not retry when first attempt fails authentication", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Authentication failed (Request ID: ABC123)" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when missing API key is detected (any attempt)", () => {
      const result = { exitCode: 1, hasOutput: false, output: "ERROR: Missing environment variable: `OPENAI_API_KEY`" };
      expect(shouldRetry(result, 0)).toBe(false);
      expect(shouldRetry(result, 1)).toBe(false);
    });

    it("does not retry when no output was produced and no transient error", () => {
      const result = { exitCode: 1, hasOutput: false, output: "" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry after retries are exhausted", () => {
      const result = { exitCode: 1, hasOutput: true, output: "rate_limit_exceeded" };
      expect(shouldRetry(result, MAX_RETRIES)).toBe(false);
    });

    it("does not retry on success", () => {
      const result = { exitCode: 0, hasOutput: true, output: "Task complete" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when numerous permission-denied issues are present", () => {
      const result = { exitCode: 1, hasOutput: true, output: "permission denied\npermission denied\npermission denied" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when codex reports an existing active goal", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "cannot create a new goal because this thread already has a goal; use update_goal only when the existing goal is complete",
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when maximum LLM invocations are exceeded", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: '{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry on token-per-minute rate limit wording", () => {
      const result = {
        exitCode: 1,
        hasOutput: false,
        output: '{"type":"error","message":"Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 50000, Requested 35000. Please try again in 615ms."}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry on token-per-minute rate limit wording even with partial output", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: '{"type":"error","message":"Rate limit reached for gpt-4o-mini in organization org-xxx on tokens per min (TPM): Limit 200000, Used 50000, Requested 35000. Please try again in 615ms."}',
      };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when rate-limit reconnects are exhausted (non-TPM rate limit)", () => {
      // Simulates the real log format: multiple Reconnecting... lines appear in
      // the output as codex retries the stream. The final "5/5" line is what
      // triggers the exhausted-reconnect detection; intermediate lines (1/5, 2/5)
      // confirm that the function ignores non-final attempts.
      const output =
        '{"type":"error","message":"Reconnecting... 1/5 (stream disconnected before completion: RateLimitError)"}\n' +
        '{"type":"error","message":"Reconnecting... 2/5 (stream disconnected before completion: RateLimitError)"}\n' +
        '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: RateLimitError)"}';
      const result = { exitCode: 1, hasOutput: true, output };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("retries when reconnects are exhausted but no rate-limit error is present", () => {
      const output =
        '{"type":"error","message":"Reconnecting... 1/5 (stream disconnected before completion: Connection timed out)"}\n' + '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: Connection timed out)"}';
      const result = { exitCode: 1, hasOutput: true, output };
      expect(shouldRetry(result, 0)).toBe(true);
    });
  });

  describe("isReconnectExhaustedError", () => {
    it("returns true when output contains Reconnecting N/N pattern (same numbers)", () => {
      expect(isReconnectExhaustedError("Reconnecting... 5/5 (some error)")).toBe(true);
    });

    it("returns true for last reconnect embedded in JSON output", () => {
      const output = '{"type":"error","message":"Reconnecting... 5/5 (stream disconnected before completion: Rate limit reached for gpt-4o-mini...)"}';
      expect(isReconnectExhaustedError(output)).toBe(true);
    });

    it("returns false when reconnect attempt is not the last (different numbers)", () => {
      expect(isReconnectExhaustedError("Reconnecting... 1/5 (some error)")).toBe(false);
      expect(isReconnectExhaustedError("Reconnecting... 3/5 (some error)")).toBe(false);
    });

    it("returns false when output has no reconnect messages", () => {
      expect(isReconnectExhaustedError("rate_limit_exceeded")).toBe(false);
      expect(isReconnectExhaustedError("")).toBe(false);
    });

    it("returns true for multi-digit N/N", () => {
      expect(isReconnectExhaustedError("Reconnecting... 10/10 (error)")).toBe(true);
    });

    it("returns false for N/M where N !== M", () => {
      expect(isReconnectExhaustedError("Reconnecting... 10/15 (error)")).toBe(false);
    });
  });

  describe("noop pre-flight and retry guard", () => {
    it("skips the agent when a noop is already in safe-outputs before the run", () => {
      const tempDir = makeHarnessTempDir("codex-noop-preflight-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"nothing to do"}\n', "utf8");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.exit(0);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: { ...process.env, CODEX_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
        encoding: "utf8",
        timeout: 10000,
      });
      // Agent stub should never have been invoked
      const stubCallCount = fs.existsSync(callsPath) ? fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length : 0;
      expect(stubCallCount).toBe(0);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("pre-flight: noop message found in safe-outputs");
    });

    it("does not retry after a failed run when a noop was written to safe-outputs", () => {
      const tempDir = makeHarnessTempDir("codex-noop-retry-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a noop on the first call then fails; harness must not retry.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"nothing to do"}) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — no retries after noop detected
      expect(callCount).toBe(1);
      // Harness exits 0 because noop means the work is done
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("noop message found in safe-outputs — not retrying");
    });

    it("exits 0 without retrying when the LLM invocation cap is saturated but the expected safe-output was already produced", () => {
      const tempDir = makeHarnessTempDir("codex-invocation-cap-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes an expected safe-output then fails with the pooled invocation-cap error.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"ADR reviewed"}) + "\\n");
process.stderr.write('{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20)."}}\\n');
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: { ...process.env, CODEX_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath, CODEX_API_KEY: "fake-key-for-test" },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — invocation cap exhaustion is never retried
      expect(callCount).toBe(1);
      // Harness exits 0 because the core work (add_comment) already succeeded
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("invocation cap saturated but safe-outputs already contain expected output");
    });
  });

  describe("post-result watchdog suppression when terminal safe-output already produced", () => {
    it("exits 0 without retrying when a terminal safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a terminal safe-output, then remains alive until SIGTERM.
      // This exercises the watchdog-fired path end-to-end.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add-labels",labels:["spam"]}) + "\\n");
process.stdout.write("Label applied. Continuing cleanup...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — no retries when watchdog suppression applies
      expect(callCount).toBe(1);
      // Harness exits 0 because the terminal safe-output was already produced
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("exits 0 without retrying when a noop safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-noop-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a noop safe-output, then remains alive until SIGTERM.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"nothing to do"}) + "\\n");
process.stdout.write("Noop recorded. Waiting...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("exits 0 without retrying when a report_incomplete safe-output was produced and the process hangs on exit", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-report-incomplete-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"report_incomplete",reason:"infrastructure_error"}) + "\\n");
process.stdout.write("report_incomplete recorded. Waiting...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
      expect(result.stderr).toContain("late-activity exit suppressed");
    });

    it("still retries partial_execution when no terminal safe-output was produced (watchdog not armed)", () => {
      const tempDir = makeHarnessTempDir("codex-watchdog-no-output-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub produces output but exits 1 without writing any safe-output.
      // The watchdog cannot fire (no terminal safe-output to arm it), so this
      // should fall through to the normal partial-execution retry path.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("partial work done\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          // Override retry config to keep the test fast.
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Should retry (2 attempts total with max_retries=1)
      expect(callCount).toBeGreaterThan(1);
      // Harness exits 1 because retries are exhausted with no output
      expect(result.status).toBe(1);
      expect(result.stderr).not.toContain("late-activity exit suppressed");
    });

    it("does not arm watchdog on retry from terminal output left by a previous attempt", () => {
      // Attempt 0 writes a terminal safe-output and exits non-zero quickly (no hang).
      // Attempt 1 should NOT have its watchdog armed by attempt 0's output, and
      // should NOT be suppressed as a success.
      const tempDir = makeHarnessTempDir("codex-watchdog-baseline-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Attempt 0: writes a terminal safe-output, produces output, then exits 1 (no hang).
      // Attempt 1+: produces output and exits 1 without writing anything new to safe-outputs.
      // The watchdog on attempt 1 must NOT arm from attempt 0's output.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
const calls = fs.existsSync(callsPath) ? fs.readFileSync(callsPath, "utf8").trim().split("\\n").filter(Boolean).length : 0;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
if (calls === 0) {
  // First attempt: write terminal output and exit immediately
  fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add-labels",labels:["spam"]}) + "\\n");
}
// All attempts: produce output (so harness classifies as partial execution and retries)
process.stdout.write("partial work done\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "moderate the issue", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
          GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
        },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Should retry: attempt 0's output does not suppress attempt 1
      expect(callCount).toBeGreaterThan(1);
      // Harness exits 1: attempt 1 produced no new terminal safe-output
      expect(result.status).toBe(1);
      // The watchdog-suppression path must not have fired
      expect(result.stderr).not.toContain("late-activity exit suppressed");
    });
  });

  describe("context rebuild circuit breaker termination", () => {
    it("stops and fails the run when the guard fires even if the process exits 0 on SIGTERM", () => {
      const tempDir = makeHarnessTempDir("codex-rebuild-breaker-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const tokenUsagePath = TOKEN_USAGE_PATHS[0];
      // Preserve any pre-existing token-usage log so the test never destroys real data,
      // while still always executing its assertions.
      const previousTokenUsage = fs.existsSync(tokenUsagePath) ? fs.readFileSync(tokenUsagePath) : null;
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(tokenUsagePath, [100, 100, 100].map(t => JSON.stringify({ input_tokens: t })).join("\n") + "\n", "utf8");
      // Stub stays alive until SIGTERM, then exits cleanly (exit code 0). Without the
      // guard-fired normalization this would be misreported as a successful run.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("rebuilding context...\\n");
process.on("SIGTERM", () => process.exit(0));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do the work", "utf8");

      try {
        const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
          cwd: path.dirname(require.resolve("./codex_harness.cjs")),
          env: {
            ...process.env,
            CODEX_HARNESS_STUB_CALLS: callsPath,
            GH_AW_SAFE_OUTPUTS: safeOutputsPath,
            CODEX_API_KEY: "fake-key-for-test",
            GH_AW_HARNESS_MAX_RETRIES: "1",
            GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
            GH_AW_CODEX_MAX_REBUILD_FACTOR: "2",
            GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "10",
            GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "1000",
            GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "250",
          },
          encoding: "utf8",
          timeout: 20000,
        });
        const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
        // The circuit breaker stops the retry loop after the first attempt.
        expect(callCount).toBe(1);
        expect(result.status).toBe(1);
        expect(result.stderr).toContain("runtime guard requested termination");
        expect(result.stderr).toContain("normalizing exit code to 1");
        expect(result.stderr).toContain("not retrying (circuit breaker)");
      } finally {
        if (previousTokenUsage === null) {
          fs.rmSync(tokenUsagePath, { force: true });
        } else {
          fs.writeFileSync(tokenUsagePath, previousTokenUsage);
        }
      }
    });

    it("allows the post-result watchdog to report success when the breaker threshold is crossed after terminal output", () => {
      const tempDir = makeHarnessTempDir("codex-rebuild-breaker-safe-output-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const tokenUsagePath = TOKEN_USAGE_PATHS[0];
      const previousTokenUsage = fs.existsSync(tokenUsagePath) ? fs.readFileSync(tokenUsagePath) : null;
      fs.mkdirSync(path.dirname(tokenUsagePath), { recursive: true });
      fs.writeFileSync(tokenUsagePath, [100, 100, 100].map(t => JSON.stringify({ input_tokens: t })).join("\n") + "\n", "utf8");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"Metrics collection complete"}) + "\\n");
process.stdout.write("terminal safe-output written; waiting for slow shutdown\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "collect metrics", "utf8");

      try {
        const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
          cwd: path.dirname(require.resolve("./codex_harness.cjs")),
          env: {
            ...process.env,
            CODEX_HARNESS_STUB_CALLS: callsPath,
            GH_AW_SAFE_OUTPUTS: safeOutputsPath,
            CODEX_API_KEY: "fake-key-for-test",
            GH_AW_HARNESS_MAX_RETRIES: "1",
            GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
            GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100",
            GH_AW_CODEX_MAX_REBUILD_FACTOR: "2",
            GH_AW_CODEX_REBUILD_MIN_CUMULATIVE_INPUT_TOKENS: "10",
            GH_AW_CODEX_REBUILD_GUARD_POLL_MS: "1000",
            GH_AW_CODEX_REBUILD_GUARD_TERM_GRACE_MS: "250",
          },
          encoding: "utf8",
          timeout: 20000,
        });
        const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
        expect(callCount).toBe(1);
        expect(result.status).toBe(0);
        expect(result.stderr).toContain("context-rebuild circuit breaker threshold exceeded after terminal safe-output was emitted");
        expect(result.stderr).toContain("post-result watchdog fired after terminal safe-output was emitted");
        expect(result.stderr).not.toContain("not retrying (circuit breaker)");
        expect(result.stderr).not.toContain("report_incomplete emitted via safeoutputs CLI");
      } finally {
        if (previousTokenUsage === null) {
          fs.rmSync(tokenUsagePath, { force: true });
        } else {
          fs.writeFileSync(tokenUsagePath, previousTokenUsage);
        }
      }
    });
  });

  describe("resolvePostResultWatchdogIdleTimeoutMs", () => {
    it("uses a 2-minute shared default", () => {
      expect(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS).toBe(120000);
    });

    it("returns the default when no env var is set", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({})).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the configured value when GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS is a positive number", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "5000" })).toBe(5000);
    });

    it("clamps to MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS when configured value is too small", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "1" })).toBe(MIN_POST_RESULT_WATCHDOG_TIMEOUT_MS);
    });

    it("clamps to MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS when configured value is too large", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: String(MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS + 1) })).toBe(MAX_POST_RESULT_WATCHDOG_TIMEOUT_MS);
    });

    it("returns the default for non-numeric input", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "not-a-number" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the default for zero", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "0" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });

    it("returns the default for negative values", () => {
      expect(resolvePostResultWatchdogIdleTimeoutMs({ GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "-100" })).toBe(DEFAULT_POST_RESULT_WATCHDOG_IDLE_TIMEOUT_MS);
    });
  });

  describe("retry configuration", () => {
    it("uses the default retry settings when env vars are unset", () => {
      expect(resolveRetryConfig({})).toEqual({
        maxRetries: 3,
        initialDelayMs: 5000,
        backoffMultiplier: 2,
        maxDelayMs: 60000,
      });
    });

    it("accepts env overrides for retry settings", () => {
      expect(
        resolveRetryConfig({
          GH_AW_HARNESS_MAX_RETRIES: "6",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "10000",
          GH_AW_HARNESS_BACKOFF_MULTIPLIER: "3",
          GH_AW_HARNESS_MAX_DELAY_MS: "180000",
        })
      ).toEqual({
        maxRetries: 6,
        initialDelayMs: 10000,
        backoffMultiplier: 3,
        maxDelayMs: 180000,
      });
    });

    it("falls back to defaults for invalid env values and logs a warning", () => {
      const logs = /** @type {string[]} */ [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_MAX_RETRIES: "-1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "not-a-number",
        },
        msg => logs.push(msg)
      );
      expect(retryConfig).toEqual({
        maxRetries: 3,
        initialDelayMs: 5000,
        backoffMultiplier: 2,
        maxDelayMs: 60000,
      });
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_MAX_RETRIES"))).toBe(true);
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_INITIAL_DELAY_MS"))).toBe(true);
    });

    it("clamps max delay to at least initial delay", () => {
      const logs = /** @type {string[]} */ [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_INITIAL_DELAY_MS: "30000",
          GH_AW_HARNESS_MAX_DELAY_MS: "1000",
        },
        msg => logs.push(msg)
      );
      expect(retryConfig.maxDelayMs).toBe(30000);
      expect(logs.some(msg => msg.includes("clamping max delay"))).toBe(true);
    });
  });

  describe("AI credits budget enforcement exits 0", () => {
    /**
     * @param {string} tempDir
     * @returns {string}
     */
    function writeTrustedAICreditsExceededAudit(tempDir) {
      const auditDir = path.join(tempDir, "sandbox", "firewall", "audit");
      fs.mkdirSync(auditDir, { recursive: true });
      fs.writeFileSync(path.join(auditDir, "log.jsonl"), `${JSON.stringify({ max_ai_credits_exceeded: true })}\n`, "utf8");
      return path.join(tempDir, "agent-output.json");
    }

    it("exits 0 when the agent outputs max_ai_credits_exceeded and the CLI exits non-zero", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-exceeded-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      // Stub emits the AI-credits-exceeded marker on stdout (as the AWF firewall would)
      // then exits non-zero.  The harness must detect this, set lastExitCode=0, and exit 0.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — credit limit is non-retryable
      expect(callCount).toBe(1);
      // Harness exits 0: budget enforcement is intentional, not a job failure
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("AI credits budget exceeded");
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("exits 0 when the agent outputs ai_credits_rate_limit_error and the CLI exits non-zero", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-rate-limit-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: ai_credits_rate_limit_error=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("keeps non-zero exit for auth failure even when AI-credit markers and trusted audit are present", () => {
      const tempDir = makeHarnessTempDir("codex-auth-failure-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.stdout.write("Authentication failed (Request ID: 123)\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      // Harness exits 1: normal non-credit failures still fail the job
      expect(result.status).toBe(1);
      expect(result.stderr).not.toContain("AI credits budget enforced");
    });

    it("keeps non-zero exit when AI-credit marker appears without trusted firewall audit evidence", () => {
      const tempDir = makeHarnessTempDir("codex-ai-credits-untrusted-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.CODEX_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["codex_harness.cjs", process.execPath, stubPath, "exec", "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./codex_harness.cjs")),
        env: {
          ...process.env,
          CODEX_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          CODEX_API_KEY: "fake-key-for-test",
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("without trusted firewall audit confirmation");
    });
  });
});
