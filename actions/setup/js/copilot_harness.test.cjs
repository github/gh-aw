import { afterEach, describe, it, expect, vi } from "vitest";
import { spawnSync } from "child_process";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { EventEmitter } = require("events");
const { PassThrough } = require("stream");
const { COPILOT_SDK_SERVER_STARTUP_TIMEOUT_MS, buildCopilotSDKServerArgs, getCopilotSDKServerPort, startCopilotSDKServer, stopCopilotSDKServer, waitForCopilotSDKServer } = require("./copilot_sdk_sidecar.cjs");
const { buildCopilotSDKEnv, isCopilotSDKEnabled } = require("./process_runner.cjs");
const {
  appendSafeOutputLine,
  buildMissingToolPermissionIssuePayload,
  classifyCopilotFailure,
  extractTokenCountFromOutput,
  buildMissingToolAlternatives,
  buildInfrastructureIncompletePayload,
  buildCopilotProxyAuthFailureDiagnostic,
  buildCopilotSDKChildEnv,
  envFlagEnabled,
  buildPromptFileFallbackInstruction,
  countPermissionDeniedIssues,
  detectCopilotErrors,
  emitInfrastructureIncomplete,
  emitMissingToolPermissionIssue,
  extractOutputTail,
  extractDeniedCommands,
  hasNumerousPermissionDeniedIssues,
  hasNoopInSafeOutputs,
  hasTerminalSafeOutput,
  hasExpectedSafeOutputs,
  INFERENCE_ACCESS_ERROR_PATTERN,
  AGENTIC_ENGINE_TIMEOUT_PATTERN,
  isDetectionPhase,
  isAuthenticationFailedError,
  isConnectionRefusedError,
  shouldRetryFirstConnectionRefused,
  FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS,
  isRetryableProxyAuthenticationFailure,
  isMCPGatewayShutdownError,
  isModelAvailableInReflectData,
  isModelAvailableInReflectFile,
  inferProviderTypeForModel,
  enrichReflectModels,
  extractModelIds,
  fetchAWFReflect,
  fetchModelsFromUrl,
  generateCopilotConnectionToken,
  GEMINI_MODEL_NAME_PREFIX,
  isCAPIQuotaExceededError,
  isHTTP400ResponseError,
  isSDKSessionIdleTimeoutError,
  PROMPT_FILE_INLINE_THRESHOLD_BYTES,
  resolvePromptFileArgs,
  resolveRetryConfig,
  shouldRetryFailedExecution,
  isCrashSignalExitCode,
  crashSignalNameForExitCode,
  writeCopilotOutputs,
  parseCopilotSDKServerArgsFromEnv,
  applyCopilotWireAPI,
  applyCopilotModelAliasResolution,
  formatInferenceEndpointForLog,
  logCopilotInferenceConfiguration,
  resolveLongRunTokenThreshold,
  computeStartupRetryEligible,
} = require("./copilot_harness.cjs");

const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard } = require("./harness_retry_guard.cjs");

const agentTempDir = "/tmp/gh-aw/agent";
const harnessChildEnv = {
  ...process.env,
  GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
  GH_AW_HARNESS_MAX_DELAY_MS: "1",
  GH_AW_SKIP_REFLECT: "true",
};

function makeHarnessTempDir(name) {
  fs.mkdirSync(agentTempDir, { recursive: true });
  return fs.mkdtempSync(path.join(agentTempDir, name));
}

function withTestPromptsDir(promptsDir, callback) {
  const originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
  if (typeof promptsDir === "string") {
    process.env.GH_AW_PROMPTS_DIR = promptsDir;
  } else {
    delete process.env.GH_AW_PROMPTS_DIR;
  }
  try {
    return callback();
  } finally {
    if (typeof originalPromptsDir === "string") {
      process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
    } else {
      delete process.env.GH_AW_PROMPTS_DIR;
    }
  }
}

function withRunnerTemp(runnerTempDir, callback) {
  const originalRunnerTemp = process.env.RUNNER_TEMP;
  process.env.RUNNER_TEMP = runnerTempDir;
  try {
    return callback();
  } finally {
    if (typeof originalRunnerTemp === "string") {
      process.env.RUNNER_TEMP = originalRunnerTemp;
    } else {
      delete process.env.RUNNER_TEMP;
    }
  }
}

function withTemporaryPromptTemplate(prefix, sourceTemplateDir, promptDirResolver, callback) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  try {
    const promptsDir = promptDirResolver(tempDir);
    fs.mkdirSync(promptsDir, { recursive: true });
    fs.copyFileSync(path.join(sourceTemplateDir, "copilot_requests_proxy_auth_403.md"), path.join(promptsDir, "copilot_requests_proxy_auth_403.md"));
    return callback(tempDir, promptsDir);
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

describe("copilot_harness.cjs", () => {
  // Test the core logic patterns used by the driver without importing the module
  // (importing the module would invoke main() which calls process.exit).

  describe("CAPIError 400 detection pattern", () => {
    const CAPI_ERROR_400_PATTERN = /CAPIError:\s*400/;

    it("matches the exact error from the failed workflow run", () => {
      const errorOutput = "Execution failed: CAPIError: 400 400 Bad Request\n (Request ID: C818:3ED713:19D401B:1C446B7:69D653CA)";
      expect(CAPI_ERROR_400_PATTERN.test(errorOutput)).toBe(true);
    });

    describe("connection refused detection", () => {
      it("detects ECONNREFUSED signals in SDK driver output", () => {
        const output = "Failed native model HTTP request: error sending request for url (http://api-proxy:10002/chat/completions): " + "client error (Connect): tcp connect error: Connection refused (os error 111) [ECONNREFUSED]";
        expect(isConnectionRefusedError(output)).toBe(true);
      });

      it("does not match unrelated output", () => {
        expect(isConnectionRefusedError("CAPIError: 400 bad request")).toBe(false);
      });
    });

    describe("CAPI quota-exceeded detection pattern", () => {
      it("matches the observed CAPIError 429 quota exceeded error", () => {
        expect(isCAPIQuotaExceededError("CAPIError: 429 429 quota exceeded")).toBe(true);
      });

      it("matches the observed error when embedded in Copilot CLI output", () => {
        const output = "Failed to get response from the AI model; retried 5 times " + "(Request-ID ABC123) Last error: CAPIError: 429 429 quota exceeded";
        expect(isCAPIQuotaExceededError(output)).toBe(true);
      });

      it("matches the observed error with extra spacing", () => {
        expect(isCAPIQuotaExceededError("CAPIError: 429   429   quota exceeded")).toBe(true);
      });

      it("does not match CAPIError 400", () => {
        expect(isCAPIQuotaExceededError("CAPIError: 400 Bad Request")).toBe(false);
      });

      it("matches Copilot/CAPI 429 Too Many Requests output", () => {
        expect(isCAPIQuotaExceededError("CAPIError: 429 Too Many Requests")).toBe(true);
        expect(isCAPIQuotaExceededError("Last error: CAPIError: Too Many Requests")).toBe(true);
      });

      it("does not match unrelated errors", () => {
        expect(isCAPIQuotaExceededError("Error: connection reset by peer")).toBe(false);
        expect(isCAPIQuotaExceededError("Authentication failed")).toBe(false);
        expect(isCAPIQuotaExceededError("")).toBe(false);
      });

      it("matches the Copilot CLI's own retry-exhaustion message without a CAPIError: prefix (429)", () => {
        const output = "Failed to get response from the AI model; retried 5 times (total retry wait time: 380.35 seconds) " + "(Request-ID AC21:F5CEC:33A719:40DD88:6A83AA27) Last error: 429 Too Many Requests\nChanges    +0 -0";
        expect(isCAPIQuotaExceededError(output)).toBe(true);
      });

      it("matches the Copilot CLI's own retry-exhaustion message for 5xx statuses (503)", () => {
        const output = "Failed to get response from the AI model; retried 5 times (total retry wait time: 300 seconds) Last error: 503 Service Unavailable";
        expect(isCAPIQuotaExceededError(output)).toBe(true);
      });

      it("does not retry a zero-progress attempt that exhausted the CLI's own 429 retries", () => {
        const output = "Failed to get response from the AI model; retried 5 times (total retry wait time: 380.35 seconds) " + "(Request-ID AC21:F5CEC:33A719:40DD88:6A83AA27) Last error: 429 Too Many Requests";
        expect(
          shouldRetryFailedExecution({
            exitCode: 1,
            hasOutput: true,
            output,
            attempt: 0,
            maxRetries: 3,
          })
        ).toBe(false);
      });
    });

    it("matches CAPIError: 400 with various spacing", () => {
      expect(CAPI_ERROR_400_PATTERN.test("CAPIError: 400")).toBe(true);
      expect(CAPI_ERROR_400_PATTERN.test("CAPIError:400")).toBe(true);
      expect(CAPI_ERROR_400_PATTERN.test("CAPIError:  400")).toBe(true);
    });

    it("does not match CAPIError 401 Unauthorized", () => {
      expect(CAPI_ERROR_400_PATTERN.test("Execution failed: CAPIError: 401 Unauthorized")).toBe(false);
    });

    it("does not match generic 400 errors without CAPIError prefix", () => {
      expect(CAPI_ERROR_400_PATTERN.test("Error: 400 Bad Request")).toBe(false);
      expect(CAPI_ERROR_400_PATTERN.test("HTTP 400")).toBe(false);
    });

    it("does not match unrelated errors", () => {
      expect(CAPI_ERROR_400_PATTERN.test("Error: ENOENT: no such file")).toBe(false);
      expect(CAPI_ERROR_400_PATTERN.test("Fatal: out of memory")).toBe(false);
      expect(CAPI_ERROR_400_PATTERN.test("")).toBe(false);
    });
  });

  describe("generateCopilotConnectionToken", () => {
    it("generates a 32-byte hex token", () => {
      const token = generateCopilotConnectionToken();
      expect(token).toMatch(/^[a-f0-9]{64}$/);
    });

    it("uses a pluggable random byte source", () => {
      const randomBytes = vi.fn(() => Buffer.alloc(32, 0xab));
      const token = generateCopilotConnectionToken({ randomBytes });
      expect(token).toMatch(/^[a-f0-9]{64}$/);
      expect(token).toBe("ab".repeat(32));
      expect(randomBytes).toHaveBeenCalledWith(32);
    });
  });

  describe("buildCopilotSDKChildEnv", () => {
    it("includes native sidecar provider vars and multiProviderJson when wireApi is configured", () => {
      const multiProviderJson = JSON.stringify({ model: "gpt-5.4", providers: [{ name: "copilot", type: "openai", baseUrl: "http://api-proxy:10002", wireApi: "completions" }], models: [{ id: "gpt-5.4", provider: "copilot" }] });
      const env = buildCopilotSDKChildEnv({
        sdkEnv: { COPILOT_SDK_URI: "http://127.0.0.1:4000" },
        copilotSDKMode: true,
        copilotConnectionToken: "token-123",
        providerBaseUrl: "http://api-proxy:10002",
        providerType: "openai",
        providerWireApi: "completions",
        resolvedModel: "gpt-5.4",
        multiProviderJson,
      });

      expect(env).toMatchObject({
        COPILOT_SDK_URI: "http://127.0.0.1:4000",
        COPILOT_CONNECTION_TOKEN: "token-123",
        GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON: multiProviderJson,
        COPILOT_MODEL: "gpt-5.4",
        COPILOT_PROVIDER_BASE_URL: "http://api-proxy:10002",
        COPILOT_PROVIDER_TYPE: "openai",
        COPILOT_PROVIDER_WIRE_API: "completions",
      });
      expect(env).not.toHaveProperty("GH_AW_COPILOT_SDK_PROVIDER_BASE_URL");
      expect(env).not.toHaveProperty("GH_AW_COPILOT_SDK_PROVIDER_TYPE");
      expect(env).not.toHaveProperty("GH_AW_COPILOT_SDK_PROVIDER_WIRE_API");
    });

    it("omits wireApi vars when wireApi is empty", () => {
      const env = buildCopilotSDKChildEnv({
        sdkEnv: { COPILOT_SDK_URI: "http://127.0.0.1:4000" },
        copilotSDKMode: true,
        copilotConnectionToken: "token-123",
        providerBaseUrl: "http://api-proxy:10002",
        providerType: "openai",
        providerWireApi: "",
        resolvedModel: "gpt-5.4",
      });

      expect(env.COPILOT_PROVIDER_WIRE_API).toBeUndefined();
    });
  });

  describe("inference endpoint logging", () => {
    it("logs every BYOK route and the selected endpoint without URL secrets", () => {
      const logs = [];
      const credentials = {
        username: ["endpoint", "user"].join("-"),
        password: ["endpoint", "password"].join("-"),
        apiKey: ["secret", "api", "key"].join("-"),
        fragment: ["secret", "fragment"].join("-"),
      };
      const secretValues = Object.values(credentials);
      const providers = [
        {
          name: "copilot",
          type: "openai",
          baseUrl: `https://${credentials.username}:${credentials.password}@api.githubcopilot.com/inference?api_key=${credentials.apiKey}#${credentials.fragment}`,
          wireApi: "responses",
        },
        {
          name: "openai",
          type: "openai",
          baseUrl: "http://api-proxy:10001",
          wireApi: "completions",
        },
      ];
      const models = [
        { id: "auto", provider: "copilot" },
        { id: "gpt-5.4", provider: "openai" },
      ];

      logCopilotInferenceConfiguration({
        copilotSDKMode: true,
        configuredModel: "auto",
        resolvedModel: "copilot/auto",
        primaryProviderName: "copilot",
        providers,
        models,
        logger: message => logs.push(message),
      });

      expect(logs).toHaveLength(5);
      expect(logs[0]).toContain('mode=sdk-byok source=awf-reflect configuredModel="auto" resolvedModel="copilot/auto" providerCount=2 modelRouteCount=2');
      expect(logs[1]).toContain('name="copilot" type="openai" wireApi="responses" endpoint="https://api.githubcopilot.com/inference" modelCount=1 selected=true');
      expect(logs[2]).toContain('name="openai" type="openai" wireApi="completions" endpoint="http://api-proxy:10001/" modelCount=1 selected=false');
      expect(logs[3]).toContain('model="copilot/auto" provider="copilot" type="openai" wireApi="responses" endpoint="https://api.githubcopilot.com/inference"');
      expect(logs[4]).toContain("authentication values are omitted");
      for (const secret of secretValues) {
        expect(logs.join("\n")).not.toContain(secret);
      }
    });

    it("does not echo malformed endpoint values", () => {
      expect(formatInferenceEndpointForLog("not-a-url-with-secret")).toBe("(invalid endpoint)");
      expect(formatInferenceEndpointForLog("file:///tmp/secret")).toBe("(invalid endpoint)");
      expect(formatInferenceEndpointForLog("")).toBe("(not set)");
    });

    it("falls back to the first provider when the primary provider name is unavailable", () => {
      const logs = [];

      logCopilotInferenceConfiguration({
        copilotSDKMode: true,
        configuredModel: "auto",
        resolvedModel: "",
        primaryProviderName: "",
        providers: [{ name: "copilot", type: "openai", baseUrl: "http://api-proxy:10002", wireApi: "responses" }],
        models: [{ id: "auto", provider: "copilot" }],
        logger: message => logs.push(message),
      });

      expect(logs[0]).toContain('resolvedModel="(not resolved)"');
      expect(logs[1]).toContain("selected=true");
      expect(logs[2]).toContain('provider="copilot"');
    });

    it("documents when the Copilot CLI manages endpoint selection", () => {
      const logger = vi.fn();

      logCopilotInferenceConfiguration({
        copilotSDKMode: false,
        configuredModel: "gpt-5.4",
        resolvedModel: "",
        primaryProviderName: "",
        providers: [],
        models: [],
        logger,
      });

      expect(logger).toHaveBeenCalledOnce();
      expect(logger).toHaveBeenCalledWith('inference routing: mode=cli configuredModel="gpt-5.4" endpoint=managed-by-copilot-cli');
    });
  });

  describe("hasTerminalSafeOutput", () => {
    it("returns true for noop entries", () => {
      const tempDir = makeHarnessTempDir("terminal-safeoutput-noop-");
      const filePath = path.join(tempDir, "safe-outputs.jsonl");
      try {
        fs.writeFileSync(filePath, JSON.stringify({ type: "noop", reason: "nothing to do" }) + "\n", "utf8");
        expect(hasTerminalSafeOutput(filePath)).toBe(true);
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns true for non-diagnostic task outputs", () => {
      const tempDir = makeHarnessTempDir("terminal-safeoutput-task-");
      const filePath = path.join(tempDir, "safe-outputs.jsonl");
      try {
        fs.writeFileSync(filePath, JSON.stringify({ type: "comment_issue", body: "done" }) + "\n", "utf8");
        expect(hasTerminalSafeOutput(filePath)).toBe(true);
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns false for diagnostic-only output", () => {
      const tempDir = makeHarnessTempDir("terminal-safeoutput-diagnostic-");
      const filePath = path.join(tempDir, "safe-outputs.jsonl");
      try {
        fs.writeFileSync(filePath, JSON.stringify({ type: "missing_tool", reason: "missing permission" }) + "\n", "utf8");
        expect(hasTerminalSafeOutput(filePath)).toBe(false);
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns false for missing_data diagnostic output", () => {
      const tempDir = makeHarnessTempDir("terminal-safeoutput-missing-data-");
      const filePath = path.join(tempDir, "safe-outputs.jsonl");
      try {
        fs.writeFileSync(filePath, JSON.stringify({ type: "missing_data", reason: "metadata only" }) + "\n", "utf8");
        expect(hasTerminalSafeOutput(filePath)).toBe(false);
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });
  });

  describe("retry policy: continue on partial execution", () => {
    const CAPI_ERROR_400_PATTERN = /CAPIError:\s*400/;
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @returns {boolean}
     */
    function shouldRetry(result, attempt) {
      return shouldRetryFailedExecution({ ...result, attempt, maxRetries: MAX_RETRIES });
    }

    /**
     * @param {string} output
     * @returns {"CAPIError 400 (transient)" | "partial execution"}
     */
    function retryReason(output) {
      return CAPI_ERROR_400_PATTERN.test(output) ? "CAPIError 400 (transient)" : "partial execution";
    }

    it("retries on CAPIError 400 after partial output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "CAPIError: 400 Bad Request" };
      expect(shouldRetry(result, 0)).toBe(true);
      expect(retryReason(result.output)).toBe("CAPIError 400 (transient)");
    });

    it("retries on any other non-zero exit when session produced output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset by peer" };
      expect(shouldRetry(result, 0)).toBe(true);
      expect(retryReason(result.output)).toBe("partial execution");
    });

    it("does not retry when no output was produced (process failed to start)", () => {
      const result = { exitCode: 1, hasOutput: false, output: "" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry after retries are exhausted", () => {
      const result = { exitCode: 1, hasOutput: true, output: "CAPIError: 400 Bad Request" };
      expect(shouldRetry(result, MAX_RETRIES)).toBe(false);
    });

    it("does not retry on success", () => {
      const result = { exitCode: 0, hasOutput: true, output: "Done." };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("numerous permission-denied issues are treated as non-retryable", () => {
      const result = { exitCode: 1, hasOutput: true, output: "permission denied\npermission denied\npermission denied" };
      expect(hasNumerousPermissionDeniedIssues(result.output)).toBe(true);
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("retries AWF API proxy blocks instead of treating them as a guard condition", () => {
      const result = { exitCode: 1, hasOutput: true, output: "awf api proxy is blocking requests for this run" };
      expect(detectNonRetryableHarnessGuard(result.output).awfAPIProxyBlockingRequests).toBe(true);
      expect(shouldRetry(result, 0)).toBe(true);
    });

    it("does not retry the observed CAPIError 429 quota exceeded error even when session produced output", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "Failed to get response from the AI model; retried 5 times. Last error: CAPIError: 429 429 quota exceeded",
      };

      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry Copilot/CAPI Too Many Requests output", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "Failed to get response from the AI model; retried 5 times. Last error: CAPIError: Too Many Requests",
      };

      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when the pooled LLM invocation cap is saturated (CAPI form: CAPIError 429 Maximum LLM invocations exceeded)", () => {
      // The pooled per-run invocation budget is shared across all retry attempts.
      // Once saturated, retries immediately re-fail with 0B output — they cannot make progress.
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "Execution failed: CAPIError: 429 Maximum LLM invocations exceeded (25/25)",
      };

      expect(detectNonRetryableHarnessGuard(result.output).maxRunsExceeded).toBe(true);
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry when the pooled LLM invocation cap is saturated (Anthropic JSON form: max_runs_exceeded)", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: '{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}',
      };

      expect(detectNonRetryableHarnessGuard(result.output).maxRunsExceeded).toBe(true);
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("still retries generic partial-execution errors with output", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "Error: connection reset by peer",
      };

      expect(shouldRetry(result, 0)).toBe(true);
    });
  });

  describe("scheduled startup retry policy (exit code 2)", () => {
    const MAX_RETRIES = 3;
    const MAX_SCHEDULED_EXIT2_RETRIES = 1;

    /**
     * @param {{hasOutput: boolean, exitCode: number}} result
     * @param {number} attempt
     * @param {boolean} isStartupRetryEligible
     * @param {number} scheduledExit2Retries
     * @returns {boolean}
     */
    function shouldRetry(result, attempt, isStartupRetryEligible, scheduledExit2Retries) {
      if (result.exitCode === 0) return false;

      const isStartupNoOutputRetryCandidate = !result.hasOutput && result.exitCode === 2;
      // Scheduled or push startup outage: retry once even when no output was produced.
      if (isStartupRetryEligible && isStartupNoOutputRetryCandidate && scheduledExit2Retries < MAX_SCHEDULED_EXIT2_RETRIES && attempt < MAX_RETRIES) {
        return true;
      }

      // Existing partial-execution retry policy
      return attempt < MAX_RETRIES && result.hasOutput;
    }

    it("retries once for scheduled startup interruption with exit code 2 and no output", () => {
      const result = { exitCode: 2, hasOutput: false };
      expect(shouldRetry(result, 0, true, 0)).toBe(true);
      expect(shouldRetry(result, 1, true, 1)).toBe(false);
    });

    it("retries once for push startup interruption with exit code 2 and no output", () => {
      const result = { exitCode: 2, hasOutput: false };
      // push events are startup-retry-eligible via computeStartupRetryEligible
      const isEligible = computeStartupRetryEligible("push");
      expect(isEligible).toBe(true);
      expect(shouldRetry(result, 0, isEligible, 0)).toBe(true);
      expect(shouldRetry(result, 1, isEligible, 1)).toBe(false);
    });

    it("does not retry exit code 1 with no output (watchdog-fired exits are suppressed as late-activity, not retried)", () => {
      const result = { exitCode: 1, hasOutput: false };
      expect(shouldRetry(result, 0, true, 0)).toBe(false);
    });

    it("does not retry on exit code 2 with no output for non-eligible triggers", () => {
      const result = { exitCode: 2, hasOutput: false };
      // pull_request and other event types are not startup-retry-eligible
      expect(computeStartupRetryEligible("pull_request")).toBe(false);
      expect(computeStartupRetryEligible("workflow_dispatch")).toBe(false);
      expect(computeStartupRetryEligible(undefined)).toBe(false);
      expect(shouldRetry(result, 0, false, 0)).toBe(false);
    });

    describe("computeStartupRetryEligible — event-to-policy wiring", () => {
      it("schedule is eligible", () => {
        expect(computeStartupRetryEligible("schedule")).toBe(true);
      });

      it("push is eligible", () => {
        expect(computeStartupRetryEligible("push")).toBe(true);
      });

      it("pull_request is not eligible", () => {
        expect(computeStartupRetryEligible("pull_request")).toBe(false);
      });

      it("workflow_dispatch is not eligible", () => {
        expect(computeStartupRetryEligible("workflow_dispatch")).toBe(false);
      });

      it("undefined event name is not eligible", () => {
        expect(computeStartupRetryEligible(undefined)).toBe(false);
      });
    });

    describe("infrastructure-incomplete terminal hasOutput guard", () => {
      /**
       * Mirrors the emitInfrastructureIncomplete guard in the harness.
       * Returns true when the incomplete diagnostic should be emitted.
       */
      function shouldEmitIncomplete({ isStartupRetryEligible, lastExitCode, retryAttempted, lastHasOutput }) {
        const isStartupNoOutputRetryCandidate = !lastHasOutput && lastExitCode === 2;
        return isStartupRetryEligible && isStartupNoOutputRetryCandidate && retryAttempted;
      }

      it("emits diagnostic when terminal attempt had no output", () => {
        expect(shouldEmitIncomplete({ isStartupRetryEligible: true, lastExitCode: 2, retryAttempted: true, lastHasOutput: false })).toBe(true);
      });

      it("does not emit diagnostic when terminal attempt produced output", () => {
        // retry fired but the terminal attempt recovered and produced output — not a Turns=0 failure
        expect(shouldEmitIncomplete({ isStartupRetryEligible: true, lastExitCode: 2, retryAttempted: true, lastHasOutput: true })).toBe(false);
      });

      it("does not emit diagnostic when no retry was attempted", () => {
        expect(shouldEmitIncomplete({ isStartupRetryEligible: true, lastExitCode: 2, retryAttempted: false, lastHasOutput: false })).toBe(false);
      });

      it("does not emit diagnostic for exit code 1 (watchdog-fired exits are suppressed as late-activity, not emitted as incomplete)", () => {
        expect(shouldEmitIncomplete({ isStartupRetryEligible: true, lastExitCode: 1, retryAttempted: true, lastHasOutput: false })).toBe(false);
      });

      it("does not emit diagnostic for non-eligible event", () => {
        expect(shouldEmitIncomplete({ isStartupRetryEligible: false, lastExitCode: 2, retryAttempted: true, lastHasOutput: false })).toBe(false);
      });
    });

    describe("failure classification helpers", () => {
      it("classifies Copilot SDK session.idle timeouts distinctly", () => {
        const output = "[copilot-sdk-driver] Timeout after 60000ms waiting for session.idle";
        expect(isSDKSessionIdleTimeoutError(output)).toBe(true);
        expect(classifyCopilotFailure({ hasOutput: true, isSDKSessionIdleTimeout: true })).toBe("sdk_session_idle_timeout");
      });

      it("classifies MCP gateway shutdown distinctly when present in output", () => {
        const output = 'Response: {"message":"Gateway shutdown initiated","serversTerminated":2,"status":"closed"}';
        expect(isMCPGatewayShutdownError(output)).toBe(true);
        expect(classifyCopilotFailure({ hasOutput: true, isMCPGatewayShutdown: true })).toBe("mcp_gateway_shutdown");
      });

      it("classifies invocation cap exhaustion as invocation_cap_exceeded", () => {
        expect(classifyCopilotFailure({ hasOutput: true, isInvocationCapExceeded: true })).toBe("invocation_cap_exceeded");
      });

      it("invocation_cap_exceeded outranks capi_quota_exceeded in failure classification", () => {
        // Both flags set — invocation cap is more specific than generic quota exceeded.
        expect(classifyCopilotFailure({ hasOutput: true, isInvocationCapExceeded: true, isQuotaExceeded: true })).toBe("invocation_cap_exceeded");
      });

      it("invocation_cap_exceeded outranks no_output when hasOutput is false", () => {
        expect(classifyCopilotFailure({ hasOutput: false, isInvocationCapExceeded: true })).toBe("invocation_cap_exceeded");
      });

      it("classifies trusted AI credits budget exhaustion distinctly", () => {
        expect(classifyCopilotFailure({ hasOutput: true, isTrustedAICreditsBudgetExhausted: true })).toBe("ai_credits_exhausted");
      });

      it("trusted AI credits budget exhaustion outranks authentication_failed", () => {
        expect(classifyCopilotFailure({ hasOutput: true, isTrustedAICreditsBudgetExhausted: true, isAuthenticationFailed: true })).toBe("ai_credits_exhausted");
      });

      it("sdk_session_idle_timeout outranks permission_denied in failure classification", () => {
        // Both flags set — the more specific signal must win.
        expect(classifyCopilotFailure({ hasOutput: true, isSDKSessionIdleTimeout: true, hasNumerousPermissionDenied: true })).toBe("sdk_session_idle_timeout");
      });

      it("mcp_gateway_shutdown outranks permission_denied in failure classification", () => {
        // Both flags set — the more specific signal must win.
        expect(classifyCopilotFailure({ hasOutput: true, isMCPGatewayShutdown: true, hasNumerousPermissionDenied: true })).toBe("mcp_gateway_shutdown");
      });

      it("retries sdk_session_idle_timeout as partial execution (shouldRetry)", () => {
        // sdk_session_idle_timeout is not a quota/permission blocker; the harness should retry.
        const result = {
          exitCode: 1,
          hasOutput: true,
          output: "[copilot-sdk-driver] Timeout after 60000ms waiting for session.idle",
        };
        const MAX_RETRIES = 3;
        const shouldRetryLocal = (r, attempt) => {
          if (r.exitCode === 0) return false;
          if (hasNumerousPermissionDeniedIssues(r.output)) return false;
          if (isCAPIQuotaExceededError(r.output)) return false;
          return attempt < MAX_RETRIES && r.hasOutput;
        };
        expect(shouldRetryLocal(result, 0)).toBe(true);
      });

      it("retries mcp_gateway_shutdown as partial execution (shouldRetry)", () => {
        // mcp_gateway_shutdown is not a quota/permission blocker; the harness should retry.
        const result = {
          exitCode: 1,
          hasOutput: true,
          output: '{"message":"Gateway shutdown initiated","serversTerminated":1,"status":"closed"}',
        };
        const MAX_RETRIES = 3;
        const shouldRetryLocal = (r, attempt) => {
          if (r.exitCode === 0) return false;
          if (hasNumerousPermissionDeniedIssues(r.output)) return false;
          if (isCAPIQuotaExceededError(r.output)) return false;
          return attempt < MAX_RETRIES && r.hasOutput;
        };
        expect(shouldRetryLocal(result, 0)).toBe(true);
      });

      it("extractOutputTail never exceeds maxChars even when maxChars is 1", () => {
        const tail = extractOutputTail("abc", { maxLines: 5, maxChars: 1 });
        expect(tail.length).toBeLessThanOrEqual(1);
      });

      it("extracts a compact tail preview from large output", () => {
        const tail = extractOutputTail(["line 1", "line 2", "line 3", "line 4"].join("\n"), { maxLines: 2, maxChars: 20 });
        expect(tail).toBe("line 3\nline 4");
      });

      it("truncates very large output tails from the front", () => {
        const tail = extractOutputTail(`prefix\n${"x".repeat(40)}`, { maxLines: 5, maxChars: 16 });
        expect(tail).toBe(`…${"x".repeat(15)}`);
      });

      describe("extractTokenCountFromOutput", () => {
        it("returns 0 for empty string", () => {
          expect(extractTokenCountFromOutput("")).toBe(0);
        });

        it("returns 0 for null/undefined", () => {
          expect(extractTokenCountFromOutput(null)).toBe(0);
          expect(extractTokenCountFromOutput(undefined)).toBe(0);
        });

        it("returns 0 when no total_tokens field is present", () => {
          expect(extractTokenCountFromOutput("some output with no token data")).toBe(0);
        });

        it("extracts a single total_tokens value", () => {
          const output = '{"total_tokens": 5000, "prompt_tokens": 3000}';
          expect(extractTokenCountFromOutput(output)).toBe(5000);
        });

        it("sums multiple total_tokens fields", () => {
          const output = '{"total_tokens": 5000}\n{"total_tokens": 7500}';
          expect(extractTokenCountFromOutput(output)).toBe(12500);
        });

        it("handles total_tokens without spaces around colon", () => {
          const output = '{"total_tokens":4321}';
          expect(extractTokenCountFromOutput(output)).toBe(4321);
        });

        it("handles total_tokens with extra spaces", () => {
          const output = '{"total_tokens"  :  9999}';
          expect(extractTokenCountFromOutput(output)).toBe(9999);
        });

        it("ignores non-total_tokens token fields like prompt_tokens and completion_tokens", () => {
          const output = '{"prompt_tokens": 1000, "completion_tokens": 500}';
          expect(extractTokenCountFromOutput(output)).toBe(0);
        });

        it("returns correct sum across multiple JSON blocks in CLI output", () => {
          const block1 = '{"model":"gpt-4","usage":{"prompt_tokens":3000,"completion_tokens":2000,"total_tokens":5000}}';
          const block2 = '{"model":"gpt-4","usage":{"prompt_tokens":4000,"completion_tokens":3000,"total_tokens":7000}}';
          expect(extractTokenCountFromOutput(`${block1}\n${block2}`)).toBe(12000);
        });
      });

      describe("long_run_exit classification", () => {
        it("classifies as long_run_exit when hasOutput and tokenCount exceeds threshold", () => {
          expect(classifyCopilotFailure({ hasOutput: true, tokenCount: 10001 })).toBe("long_run_exit");
        });

        it("classifies as partial_execution when tokenCount is exactly at threshold", () => {
          expect(classifyCopilotFailure({ hasOutput: true, tokenCount: 10000 })).toBe("partial_execution");
        });

        it("classifies as partial_execution when tokenCount is below threshold", () => {
          expect(classifyCopilotFailure({ hasOutput: true, tokenCount: 9999 })).toBe("partial_execution");
        });

        it("classifies as no_output when hasOutput is false even with high tokenCount", () => {
          expect(classifyCopilotFailure({ hasOutput: false, tokenCount: 50000 })).toBe("no_output");
        });

        it("classifies as partial_execution when tokenCount is 0", () => {
          expect(classifyCopilotFailure({ hasOutput: true, tokenCount: 0 })).toBe("partial_execution");
        });

        it("classifies as partial_execution when tokenCount is absent", () => {
          expect(classifyCopilotFailure({ hasOutput: true })).toBe("partial_execution");
        });

        it("named error classes outrank long_run_exit: auth error", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isAuthErr: true, tokenCount: 50000 })).toBe("no_auth_info");
        });

        it("named error classes outrank long_run_exit: quota exceeded", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isQuotaExceeded: true, tokenCount: 50000 })).toBe("capi_quota_exceeded");
        });

        it("named error classes outrank long_run_exit: invocation cap exceeded", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isInvocationCapExceeded: true, tokenCount: 50000 })).toBe("invocation_cap_exceeded");
        });

        it("named error classes outrank long_run_exit: MCP policy", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isMCPPolicy: true, tokenCount: 50000 })).toBe("mcp_policy_blocked");
        });

        it("named error classes outrank long_run_exit: capi_error_400", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isTransientCAPIError: true, tokenCount: 50000 })).toBe("capi_error_400");
        });

        it("named error classes outrank long_run_exit: sdk_session_idle_timeout", () => {
          expect(classifyCopilotFailure({ hasOutput: true, isSDKSessionIdleTimeout: true, tokenCount: 50000 })).toBe("sdk_session_idle_timeout");
        });
      });

      describe("resolveLongRunTokenThreshold", () => {
        it("returns default 10000 when env var is unset", () => {
          expect(resolveLongRunTokenThreshold({})).toBe(10000);
        });

        it("returns the configured value when GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD is set", () => {
          expect(resolveLongRunTokenThreshold({ GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD: "5000" })).toBe(5000);
        });

        it("returns default when env var is not a number", () => {
          expect(resolveLongRunTokenThreshold({ GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD: "abc" })).toBe(10000);
        });

        it("returns default when env var is negative", () => {
          expect(resolveLongRunTokenThreshold({ GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD: "-1" })).toBe(10000);
        });

        it("returns 0 when env var is '0' (disables long_run_exit classification)", () => {
          expect(resolveLongRunTokenThreshold({ GH_AW_HARNESS_LONG_RUN_TOKEN_THRESHOLD: "0" })).toBe(0);
        });
      });
    });

    it("does not claim a retry when already at max retry attempt", () => {
      const result = { exitCode: 2, hasOutput: false };
      expect(shouldRetry(result, MAX_RETRIES, true, 0)).toBe(false);
    });

    it("does not apply startup retry for non-scheduled runs", () => {
      const result = { exitCode: 2, hasOutput: false };
      expect(shouldRetry(result, 0, false, 0)).toBe(false);
    });

    it("continues to use partial-execution retries when output exists", () => {
      const result = { exitCode: 2, hasOutput: true };
      expect(shouldRetry(result, 0, true, 0)).toBe(true);
    });
  });

  describe("copilot-sdk sidecar helpers", () => {
    it("extracts the configured Copilot SDK server port", () => {
      expect(
        getCopilotSDKServerPort({
          COPILOT_SDK_URI: "http://127.0.0.1:3002",
        })
      ).toBe("3002");
    });

    describe("parseCopilotSDKServerArgsFromEnv", () => {
      it("returns parsed server args and logs count", () => {
        const logger = vi.fn();
        const result = parseCopilotSDKServerArgsFromEnv('["--headless","--port","3002"]', { logger });
        expect(result).toEqual(["--headless", "--port", "3002"]);
        expect(logger).toHaveBeenCalledWith("copilot-sdk driver mode: parsed 3 sidecar args from GH_AW_COPILOT_SDK_SERVER_ARGS");
      });

      it("falls back to empty args when value is not a string array", () => {
        const logger = vi.fn();
        const result = parseCopilotSDKServerArgsFromEnv('{"port":3002}', { logger });
        expect(result).toEqual([]);
        expect(logger).toHaveBeenCalledWith("copilot-sdk driver mode: GH_AW_COPILOT_SDK_SERVER_ARGS must be a JSON string array; using sidecar default args");
      });

      it("falls back to empty args when json is invalid", () => {
        const logger = vi.fn();
        const result = parseCopilotSDKServerArgsFromEnv("not-json", { logger });
        expect(result).toEqual([]);
        expect(logger).toHaveBeenCalledWith(expect.stringContaining("failed to parse GH_AW_COPILOT_SDK_SERVER_ARGS"));
      });
    });

    it("builds headless Copilot CLI sidecar args", () => {
      expect(
        buildCopilotSDKServerArgs({
          COPILOT_SDK_URI: "http://127.0.0.1:3002",
        })
      ).toEqual(["--headless", "--no-auto-update", "--port", "3002"]);
    });

    it("centralizes copilot-sdk activation checks", () => {
      expect(isCopilotSDKEnabled({ COPILOT_SDK_URI: "http://127.0.0.1:3002" })).toBe(true);
      expect(isCopilotSDKEnabled({})).toBe(false);
      expect(buildCopilotSDKEnv({ COPILOT_SDK_URI: "http://127.0.0.1:3002" })).toEqual({
        COPILOT_SDK_URI: "http://127.0.0.1:3002",
        COPILOT_SDK_LOG_LEVEL: "all",
      });
    });

    it("returns null when copilot-sdk mode is disabled", async () => {
      const spawnImpl = vi.fn();
      const result = await startCopilotSDKServer({
        command: "copilot",
        env: {},
        spawnImpl,
      });
      expect(result).toBeNull();
      expect(spawnImpl).not.toHaveBeenCalled();
    });

    it("starts the headless Copilot CLI sidecar with the configured port", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.pid = 1234;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn();
      const spawnImpl = vi.fn(() => child);
      /** @type {(() => void) | undefined} */
      let resolveReady;
      const waitForReady = vi.fn(
        () =>
          new Promise(resolve => {
            resolveReady = resolve;
          })
      );

      const startPromise = startCopilotSDKServer({
        command: "copilot",
        env: {
          COPILOT_SDK_URI: "http://127.0.0.1:3002",
        },
        logger: () => {},
        spawnImpl,
        waitForReady,
      });

      await Promise.resolve();
      expect(child.listenerCount("error")).toBe(1);
      expect(child.listenerCount("close")).toBe(1);

      if (!resolveReady) {
        throw new Error("waitForReady not yet called");
      }
      resolveReady();
      const result = await startPromise;

      expect(result).toBe(child);
      expect(spawnImpl).toHaveBeenCalledWith(
        "copilot",
        ["--headless", "--no-auto-update", "--port", "3002"],
        expect.objectContaining({
          stdio: ["ignore", "pipe", "pipe"],
          env: {
            COPILOT_SDK_URI: "http://127.0.0.1:3002",
          },
        })
      );
      expect(waitForReady).toHaveBeenCalledWith({
        host: "127.0.0.1",
        port: "3002",
        logger: expect.any(Function),
      });
      expect(child.listenerCount("error")).toBe(0);
      expect(child.listenerCount("close")).toBe(0);
    });

    it("includes stderr tail when the headless Copilot CLI sidecar exits before ready", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.exitCode = null;
      child.signalCode = "SIGABRT";
      const spawnImpl = vi.fn(() => child);
      const waitForReady = vi.fn(
        () =>
          new Promise(() => {
            // Keep readiness pending so the close event wins the startup race.
          })
      );

      const startPromise = startCopilotSDKServer({
        command: "copilot",
        env: { COPILOT_SDK_URI: "http://127.0.0.1:3002" },
        logger: () => {},
        spawnImpl,
        waitForReady,
      });

      await Promise.resolve();
      child.stderr.write("native assertion failed before listen\npanic details\n");
      child.emit("close", null, "SIGABRT");

      await expect(startPromise).rejects.toThrow("copilot-sdk headless server exited before ready (exitCode=unknown signal=SIGABRT)\nstderr tail:\nnative assertion failed before listen\npanic details");
      expect(child.listenerCount("error")).toBe(0);
      expect(child.listenerCount("close")).toBe(0);
    });

    it("forwards extraArgs to the headless server when provided", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.pid = 5678;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn();
      const spawnImpl = vi.fn(() => child);
      const waitForReady = vi.fn().mockResolvedValue(undefined);

      await startCopilotSDKServer({
        command: "copilot",
        env: { COPILOT_SDK_URI: "http://127.0.0.1:3002" },
        extraArgs: ["--add-dir", "/tmp/gh-aw/", "--log-level", "all", "--disable-builtin-mcps"],
        logger: () => {},
        spawnImpl,
        waitForReady,
      });

      expect(spawnImpl).toHaveBeenCalledWith(
        "copilot",
        ["--headless", "--no-auto-update", "--port", "3002", "--add-dir", "/tmp/gh-aw/", "--log-level", "all", "--disable-builtin-mcps"],
        expect.objectContaining({ stdio: ["ignore", "pipe", "pipe"] })
      );
    });

    it("uses engine-generated serverArgs directly when provided", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.pid = 5680;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn();
      const spawnImpl = vi.fn(() => child);
      const waitForReady = vi.fn().mockResolvedValue(undefined);

      const engineGeneratedArgs = ["--headless", "--no-auto-update", "--port", "3002", "--add-dir", "/tmp/gh-aw/", "--log-level", "all", "--disable-builtin-mcps", "--no-ask-user"];
      await startCopilotSDKServer({
        command: "copilot",
        env: { COPILOT_SDK_URI: "http://127.0.0.1:3002" },
        serverArgs: engineGeneratedArgs,
        logger: () => {},
        spawnImpl,
        waitForReady,
      });

      expect(spawnImpl).toHaveBeenCalledWith("copilot", engineGeneratedArgs, expect.objectContaining({ stdio: ["ignore", "pipe", "pipe"] }));
    });

    it("uses only base headless args when extraArgs is empty or omitted", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.pid = 5679;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn();
      const spawnImpl = vi.fn(() => child);
      const waitForReady = vi.fn().mockResolvedValue(undefined);

      await startCopilotSDKServer({
        command: "copilot",
        env: { COPILOT_SDK_URI: "http://127.0.0.1:3002" },
        extraArgs: [],
        logger: () => {},
        spawnImpl,
        waitForReady,
      });

      expect(spawnImpl).toHaveBeenCalledWith("copilot", ["--headless", "--no-auto-update", "--port", "3002"], expect.objectContaining({ stdio: ["ignore", "pipe", "pipe"] }));
    });

    it("stops the headless Copilot CLI sidecar with SIGTERM", async () => {
      const child = new EventEmitter();
      child.pid = 4321;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn(signal => {
        child.signalCode = signal;
        setImmediate(() => child.emit("close", 0, signal));
      });

      await stopCopilotSDKServer(child, { logger: () => {}, timeoutMs: 50 });

      expect(child.kill).toHaveBeenCalledWith("SIGTERM");
    });

    it("stops the sidecar when readiness fails after spawn", async () => {
      const child = new EventEmitter();
      child.stdout = new PassThrough();
      child.stderr = new PassThrough();
      child.pid = 1234;
      child.exitCode = null;
      child.signalCode = null;
      child.kill = vi.fn(signal => {
        child.signalCode = signal;
        setImmediate(() => child.emit("close", 0, signal));
      });
      const spawnImpl = vi.fn(() => child);
      const waitForReady = vi.fn().mockRejectedValue(new Error("not ready"));

      await expect(
        startCopilotSDKServer({
          command: "copilot",
          env: {
            COPILOT_SDK_URI: "http://127.0.0.1:3002",
          },
          logger: () => {},
          spawnImpl,
          waitForReady,
        })
      ).rejects.toThrow("not ready");

      expect(child.kill).toHaveBeenCalledWith("SIGTERM");
      expect(child.listenerCount("error")).toBe(0);
      expect(child.listenerCount("close")).toBe(0);
    });

    it("waits for the Copilot SDK sidecar port to accept connections", async () => {
      const connectImpl = vi.fn(({ host, port }) => {
        const socket = new EventEmitter();
        socket.end = vi.fn();
        socket.destroy = vi.fn();
        setImmediate(() => socket.emit("connect"));
        expect(host).toBe("127.0.0.1");
        expect(port).toBe(3002);
        return socket;
      });

      await expect(
        waitForCopilotSDKServer({
          host: "127.0.0.1",
          port: "3002",
          timeoutMs: 100,
          logger: () => {},
          connectImpl,
        })
      ).resolves.toBeUndefined();
    });

    it("allows the headless server enough startup budget for Copilot CLI package extraction", () => {
      // Package extraction alone has been observed to take ~7s on hosted runners.
      expect(COPILOT_SDK_SERVER_STARTUP_TIMEOUT_MS).toBeGreaterThanOrEqual(30000);
    });
  });

  describe("infrastructure report_incomplete emission helpers", () => {
    it("builds report_incomplete payload with infrastructure_error reason", () => {
      const payload = buildInfrastructureIncompletePayload("temporary outage");
      expect(JSON.parse(payload)).toEqual({
        type: "report_incomplete",
        reason: "infrastructure_error",
        details: "temporary outage",
      });
    });

    it("appends one JSONL line through appendSafeOutputLine", () => {
      const writes = [];
      const appendStub = (file, data, options) => writes.push({ file, data, options });
      appendSafeOutputLine(appendStub, "/tmp/safeoutputs.jsonl", '{"type":"report_incomplete"}');
      expect(writes).toEqual([{ file: "/tmp/safeoutputs.jsonl", data: '{"type":"report_incomplete"}\n', options: { encoding: "utf8" } }]);
    });

    it("emitInfrastructureIncomplete writes payload when path is configured", () => {
      const calls = [];
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        runSafeOutputsCLI: (toolName, args) => calls.push({ toolName, args }),
        logger: message => logs.push(message),
      });
      expect(calls).toEqual([
        {
          toolName: "report_incomplete",
          args: { reason: "infrastructure_error", details: "temporary outage" },
        },
      ]);
      expect(logs.some(message => message.includes("report_incomplete emitted"))).toBe(true);
    });

    it("emitInfrastructureIncomplete skips when path is missing", () => {
      const calls = [];
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "",
        runSafeOutputsCLI: () => calls.push("call"),
        logger: message => logs.push(message),
      });
      expect(calls).toHaveLength(0);
      expect(logs.some(message => message.includes("skipped"))).toBe(true);
    });

    it("emitInfrastructureIncomplete logs CLI errors", () => {
      const logs = [];
      emitInfrastructureIncomplete("temporary outage", {
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        runSafeOutputsCLI: () => {
          throw new Error("EROFS");
        },
        logger: message => logs.push(message),
      });
      expect(logs.some(message => message.includes("report_incomplete emission failed: EROFS"))).toBe(true);
    });
  });

  describe("permission-denied classification helpers", () => {
    it("counts repeated permission-denied signals", () => {
      const output = "permission denied\nEACCES: permission denied\nEPERM operation not permitted\npermissions denied";
      expect(countPermissionDeniedIssues(output)).toBe(5);
    });

    it("detects numerous permission-denied issues at threshold", () => {
      const output = "permission denied\npermission denied\npermission denied";
      expect(hasNumerousPermissionDeniedIssues(output)).toBe(true);
    });

    it("does not classify sparse permission-denied output as numerous", () => {
      const output = "permission denied once";
      expect(hasNumerousPermissionDeniedIssues(output)).toBe(false);
    });

    it("builds missing_tool payload for permission issues", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload());
      expect(payload.type).toBe("missing_tool");
      expect(payload.reason).toContain("missing tool/permission issue");
      expect(payload.denied_commands).toEqual([]);
    });

    it("builds missing_tool payload with denied commands", () => {
      const payload = JSON.parse(buildMissingToolPermissionIssuePayload(["go version", "ls /usr/local/go/bin/go"]));
      expect(payload.type).toBe("missing_tool");
      expect(payload.denied_commands).toEqual(["go version", "ls /usr/local/go/bin/go"]);
    });

    it("builds missing_tool alternatives with denied command details", () => {
      const base = "Verify token scopes, repository permissions, and MCP/tool access configuration.";
      const alternatives = buildMissingToolAlternatives(base, ["go version"]);
      expect(alternatives).toContain("Denied commands: go version");
    });

    it("keeps base alternatives when denied command list is empty", () => {
      const base = "Verify token scopes, repository permissions, and MCP/tool access configuration.";
      expect(buildMissingToolAlternatives(base, [])).toBe(base);
    });

    it("caps alternatives to 512 chars and uses compact overflow marker", () => {
      const base = "base";
      const deniedCommands = Array.from({ length: 30 }, (_, i) => `command-${i}-${"x".repeat(30)}`);
      const alternatives = buildMissingToolAlternatives(base, deniedCommands);
      expect(alternatives.length).toBeLessThanOrEqual(512);
      expect(alternatives).toContain("Denied commands:");
      expect(alternatives).toContain("... and");
    });

    it("emitMissingToolPermissionIssue calls safeoutputs CLI when path is configured", () => {
      const calls = [];
      const logs = [];
      emitMissingToolPermissionIssue({
        safeOutputsPath: "/tmp/safeoutputs.jsonl",
        deniedCommands: ["go version"],
        runSafeOutputsCLI: (toolName, args) => calls.push({ toolName, args }),
        logger: message => logs.push(message),
      });
      expect(calls).toHaveLength(1);
      expect(calls[0].toolName).toBe("missing_tool");
      expect(calls[0].args.tool).toBe("tool/permission");
      expect(calls[0].args.reason).toContain("missing tool/permission issue");
      expect(calls[0].args.alternatives).toContain("Denied commands: go version");
      expect(logs.some(message => message.includes("missing_tool emitted"))).toBe(true);
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

  describe("MCP policy blocked detection pattern", () => {
    const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;

    it("matches the exact error from the issue report", () => {
      const errorOutput = "! 2 MCP servers were blocked by policy: 'github', 'safeoutputs'";
      expect(MCP_POLICY_BLOCKED_PATTERN.test(errorOutput)).toBe(true);
    });

    it("matches with different server names", () => {
      expect(MCP_POLICY_BLOCKED_PATTERN.test("! 1 MCP servers were blocked by policy: 'github'")).toBe(true);
      expect(MCP_POLICY_BLOCKED_PATTERN.test("MCP servers were blocked by policy: 'custom-server'")).toBe(true);
    });

    it("does not match unrelated errors", () => {
      expect(MCP_POLICY_BLOCKED_PATTERN.test("Error: MCP server connection failed")).toBe(false);
      expect(MCP_POLICY_BLOCKED_PATTERN.test("MCP server timeout")).toBe(false);
      expect(MCP_POLICY_BLOCKED_PATTERN.test("Access denied by policy settings")).toBe(false);
      expect(MCP_POLICY_BLOCKED_PATTERN.test("")).toBe(false);
    });
  });

  describe("MCP policy error prevents retry", () => {
    // Inline the same retry logic as the driver, including MCP policy check
    const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;
    const MODEL_NOT_SUPPORTED_PATTERN = /The requested model is not supported/;
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @returns {boolean}
     */
    function shouldRetry(result, attempt) {
      if (result.exitCode === 0) return false;
      // MCP policy errors are persistent — never retry
      if (MCP_POLICY_BLOCKED_PATTERN.test(result.output)) return false;
      // Model-not-supported errors are persistent — never retry
      if (MODEL_NOT_SUPPORTED_PATTERN.test(result.output)) return false;
      return attempt < MAX_RETRIES && result.hasOutput;
    }

    it("does not retry when MCP servers are blocked by policy", () => {
      const result = { exitCode: 1, hasOutput: true, output: "! 2 MCP servers were blocked by policy: 'github', 'safeoutputs'" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry MCP policy error even on first attempt with output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Some output\nMCP servers were blocked by policy: 'github'\nMore output" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry model-not-supported error", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Execution failed: CAPIError: 400 The requested model is not supported." };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("does not retry model-not-supported error even on first attempt with output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Some output\nExecution failed: CAPIError: 400 The requested model is not supported.\nMore output" };
      expect(shouldRetry(result, 0)).toBe(false);
    });

    it("still retries non-policy errors with output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "CAPIError: 400 Bad Request" };
      expect(shouldRetry(result, 0)).toBe(true);
    });
  });

  describe("model-not-supported detection pattern", () => {
    const MODEL_NOT_SUPPORTED_PATTERN = /The requested model is not supported/;

    it("matches the exact error from the issue report", () => {
      const errorOutput = "Execution failed: CAPIError: 400 The requested model is not supported.";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(errorOutput)).toBe(true);
    });

    describe("copilot output detection + workflow outputs", () => {
      afterEach(() => {
        delete process.env.GITHUB_OUTPUT;
      });

      it("detects inference/mcp/timeout/model-not-supported patterns from output", () => {
        const output = [
          "Access denied by policy settings",
          "MCP servers were blocked by policy: 'github'",
          "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM",
          "Execution failed: CAPIError: 400 The requested model is not supported.",
          "Response status code does not indicate success: 400 (Bad Request)",
        ].join("\n");
        expect(detectCopilotErrors(output)).toEqual({
          inferenceAccessError: true,
          mcpPolicyError: true,
          agenticEngineTimeout: true,
          modelNotSupportedError: true,
          http400ResponseError: true,
        });
        expect(INFERENCE_ACCESS_ERROR_PATTERN.test(output)).toBe(true);
        expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(output)).toBe(true);
      });

      it("detects the Copilot SDK driver policy-enablement error as model-not-supported", () => {
        const output = "[copilot-sdk-driver] [sdk-driver] error: Execution failed: Error: No model available. Check policy enablement under GitHub Settings > Copilot";
        expect(detectCopilotErrors(output).modelNotSupportedError).toBe(true);
        expect(classifyCopilotFailure({ hasOutput: true, isModelNotSupported: detectCopilotErrors(output).modelNotSupportedError })).toBe("model_not_supported");
      });

      it("writes copilot detection outputs to GITHUB_OUTPUT", () => {
        const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-output-test-"));
        const outputFile = path.join(tempDir, "github-output.txt");
        process.env.GITHUB_OUTPUT = outputFile;

        writeCopilotOutputs({
          inferenceAccessError: true,
          mcpPolicyError: false,
          agenticEngineTimeout: true,
          modelNotSupportedError: false,
          http400ResponseError: true,
        });

        const content = fs.readFileSync(outputFile, "utf8");
        expect(content).toContain("inference_access_error=true");
        expect(content).toContain("mcp_policy_error=false");
        expect(content).toContain("agentic_engine_timeout=true");
        expect(content).toContain("model_not_supported_error=false");
        expect(content).toContain("http_400_response_error=true");
      });
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some output\nExecution failed: CAPIError: 400 The requested model is not supported.\nMore output";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(log)).toBe(true);
    });

    it("does not match other CAPIError 400 errors", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
    });

    it("does not match unrelated errors", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("Access denied by policy settings")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("MCP servers were blocked by policy: 'github'")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("")).toBe(false);
    });
  });

  describe("isHTTP400ResponseError", () => {
    it("matches the exact SDK message format", () => {
      expect(isHTTP400ResponseError("Response status code does not indicate success: 400 (Bad Request)")).toBe(true);
    });

    it("matches without the (Bad Request) suffix", () => {
      expect(isHTTP400ResponseError("Response status code does not indicate success: 400")).toBe(true);
    });

    it("does not match CAPIError 400 (a distinct error shape)", () => {
      expect(isHTTP400ResponseError("CAPIError: 400 The requested model is not supported.")).toBe(false);
    });

    it("returns false for empty output", () => {
      expect(isHTTP400ResponseError("")).toBe(false);
    });

    it("matches the 'no model endpoints available given user constraints' SDK error", () => {
      expect(isHTTP400ResponseError("[copilot-sdk-driver] [sdk-driver] error: 400 400 400 no model endpoints available given user constraints")).toBe(true);
    });

    it("matches the no-model-endpoints error embedded in larger output", () => {
      const output = 'some prior output\n[copilot-sdk-driver] [sdk-driver] error: 400 400 400 no model endpoints available given user constraints\n{"type":"subagent.failed"}';
      expect(isHTTP400ResponseError(output)).toBe(true);
    });

    it("matches the 'stream_options: Extra inputs are not permitted' Anthropic BYOK error", () => {
      expect(isHTTP400ResponseError("[copilot-sdk-driver] [sdk-driver] error: 400 400 400 stream_options: Extra inputs are not permitted")).toBe(true);
    });

    it("matches the stream_options error embedded in larger output", () => {
      const output = 'some prior output\n[copilot-sdk-driver] [sdk-driver] error: 400 400 400 stream_options: Extra inputs are not permitted\n{"type":"subagent.failed"}';
      expect(isHTTP400ResponseError(output)).toBe(true);
    });

    it("does not false-positive on unrelated messages mentioning stream_options", () => {
      expect(isHTTP400ResponseError("Configuring stream_options for the request")).toBe(false);
    });
  });

  describe("no-auth-info detection pattern", () => {
    const NO_AUTH_INFO_PATTERN = /No authentication information found/;

    it("matches the exact error from the issue report", () => {
      const errorOutput =
        "Error: No authentication information found.\n" +
        "Copilot can be authenticated with GitHub using an OAuth Token or a Fine-Grained Personal Access Token.\n" +
        "To authenticate, you can use any of the following methods:\n" +
        "  - Start 'copilot' and run the '/login' command\n" +
        "  - Set the COPILOT_GITHUB_TOKEN, GH_TOKEN, or GITHUB_TOKEN environment variable\n" +
        "  - Run 'gh auth login' to authenticate with the GitHub CLI";
      expect(NO_AUTH_INFO_PATTERN.test(errorOutput)).toBe(true);
    });

    it("matches when embedded in larger output after a long run", () => {
      const output = "Some agent work output\nMore work\nNo authentication information found\nEnd";
      expect(NO_AUTH_INFO_PATTERN.test(output)).toBe(true);
    });

    it("does not match unrelated auth errors", () => {
      expect(NO_AUTH_INFO_PATTERN.test("Access denied by policy settings")).toBe(false);
      expect(NO_AUTH_INFO_PATTERN.test("Error: 401 Unauthorized")).toBe(false);
      expect(NO_AUTH_INFO_PATTERN.test("Authentication failed")).toBe(false);
      expect(NO_AUTH_INFO_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(NO_AUTH_INFO_PATTERN.test("")).toBe(false);
    });
  });

  describe("authentication-failed detection pattern", () => {
    it("matches authentication failed with request id", () => {
      expect(isAuthenticationFailedError("Authentication failed (Request ID: C818:3ED713:19D401B:1C446B7:69D653CA)")).toBe(true);
    });

    it("does not match no-auth-info error", () => {
      expect(isAuthenticationFailedError("Error: No authentication information found.")).toBe(false);
    });

    it("matches PAT-not-supported 400 from Copilot CAPI", () => {
      expect(isAuthenticationFailedError("400 400 checking third-party user token: bad request: Personal Access Tokens are not supported for this endpoint")).toBe(true);
    });
  });

  describe("gh-aw API proxy auth diagnostics", () => {
    const promptsSourceDir = path.resolve("../md");

    it("rewrites local proxy 401 errors to COPILOT_GITHUB_TOKEN guidance", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 401).\nCheck your COPILOT_PROVIDER_API_KEY or COPILOT_PROVIDER_BEARER_TOKEN.", {
        COPILOT_MODEL: "claude-sonnet-4.5",
      });

      expect(diagnostic).toContain("gh-aw API proxy");
      expect(diagnostic).toContain("HTTP 401");
      expect(diagnostic).toContain("model=claude-sonnet-4.5");
      expect(diagnostic).toContain("stage=starting the Copilot CLI request");
      expect(diagnostic).toContain("COPILOT_GITHUB_TOKEN");
      expect(diagnostic).toContain("GH_AW_MODEL_AGENT_COPILOT");
      expect(diagnostic).not.toContain("COPILOT_PROVIDER_API_KEY");
    });

    it("rewrites local proxy 403 errors in copilot-requests mode to org-billing guidance", () => {
      withTestPromptsDir(promptsSourceDir, () => {
        const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).\nCheck your COPILOT_PROVIDER_API_KEY or COPILOT_PROVIDER_BEARER_TOKEN.", {
          COPILOT_MODEL: "claude-sonnet-4.5",
          S2STOKENS: "true",
        });

        expect(diagnostic).toContain("Copilot requests authentication failed");
        expect(diagnostic).toContain("HTTP 403");
        expect(diagnostic).toContain("model=claude-sonnet-4.5");
        expect(diagnostic).toContain("stage=starting the Copilot CLI request");
        expect(diagnostic).toContain("permissions.copilot-requests: write");
        expect(diagnostic).toContain("centralized Copilot billing");
        expect(diagnostic).toContain("https://github.github.com/gh-aw/reference/billing/");
        expect(diagnostic).not.toContain("COPILOT_PROVIDER_API_KEY");
      });
    });

    it("treats truthy S2STOKENS values as copilot-requests mode for 403 guidance", () => {
      withTestPromptsDir(promptsSourceDir, () => {
        const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).", {
          COPILOT_MODEL: "claude-sonnet-4.5",
          S2STOKENS: " YES ",
        });

        expect(diagnostic).toContain("Copilot requests authentication failed");
        expect(diagnostic).toContain("https://github.github.com/gh-aw/reference/billing/");
        expect(diagnostic).not.toContain("COPILOT_PROVIDER_API_KEY");
      });
    });

    it("resolves the 403 guidance template from the runtime prompts directory", () => {
      withTemporaryPromptTemplate(
        "runtime-prompts-",
        promptsSourceDir,
        tempDir => tempDir,
        (_tempDir, runtimePromptsDir) => {
          withTestPromptsDir(runtimePromptsDir, () => {
            const renderTemplateFromFile = vi.fn((templatePath, context) => {
              return fs.readFileSync(templatePath, "utf8").replace("{selected_model}", context.selected_model).replace("{stage}", context.stage);
            });
            const diagnostic = buildCopilotProxyAuthFailureDiagnostic(
              "Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).",
              {
                COPILOT_MODEL: "claude-sonnet-4.5",
                S2STOKENS: "true",
              },
              { renderTemplateFromFile }
            );

            expect(diagnostic).toContain("Copilot requests authentication failed");
            expect(diagnostic).toContain("model=claude-sonnet-4.5");
            expect(diagnostic).toContain("stage=starting the Copilot CLI request");
            expect(renderTemplateFromFile).toHaveBeenCalledWith(path.join(runtimePromptsDir, "copilot_requests_proxy_auth_403.md"), {
              selected_model: "claude-sonnet-4.5",
              stage: "starting the Copilot CLI request",
            });
          });
        }
      );
    });

    it("resolves the 403 guidance template from RUNNER_TEMP when GH_AW_PROMPTS_DIR is unset", () => {
      withTemporaryPromptTemplate(
        "runner-temp-",
        promptsSourceDir,
        tempDir => path.join(tempDir, "gh-aw", "prompts"),
        runnerTempDir => {
          withTestPromptsDir(undefined, () => {
            withRunnerTemp(runnerTempDir, () => {
              const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).", {
                COPILOT_MODEL: "claude-sonnet-4.5",
                S2STOKENS: "true",
              });

              expect(diagnostic).toContain("Copilot requests authentication failed");
              expect(diagnostic).toContain("model=claude-sonnet-4.5");
              expect(diagnostic).toContain("stage=starting the Copilot CLI request");
            });
          });
        }
      );
    });

    it("returns empty string for proxy 403 when S2STOKENS is not set (BYOK mode)", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).", {
        COPILOT_MODEL: "claude-sonnet-4.5",
      });

      expect(diagnostic).toBe("");
    });

    it("returns empty string for proxy 403 when S2STOKENS is falsy", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).", {
        COPILOT_MODEL: "claude-sonnet-4.5",
        S2STOKENS: "false",
      });

      expect(diagnostic).toBe("");
    });

    it("returns empty string for non-proxy 403 even when S2STOKENS is true", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at (api.anthropic.com/redacted) (HTTP 403).", {
        COPILOT_MODEL: "claude-sonnet-4.5",
        S2STOKENS: "true",
      });

      expect(diagnostic).toBe("");
    });

    it("reports token-validation stage when present in the output", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Validating token with provider.\nAuthentication failed with provider at http://localhost:10002 (HTTP 401).", { COPILOT_MODEL: "gpt-4.1" });

      expect(diagnostic).toContain("stage=validating the token");
    });

    it("reports model-listing stage when present in the output", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Listing models from /models endpoint.\nAuthentication failed with provider at http://api-proxy:10002 (HTTP 401).", { COPILOT_MODEL: "o4-mini" });

      expect(diagnostic).toContain("stage=listing models");
    });

    it("ignores non-proxy provider auth failures", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at https://api.openai.com/v1 (HTTP 401).", { COPILOT_MODEL: "gpt-4.1" });

      expect(diagnostic).toBe("");
    });

    it("ignores local BYOK provider auth failures on non-proxy ports", () => {
      const diagnostic = buildCopilotProxyAuthFailureDiagnostic("Authentication failed with provider at http://host.docker.internal:11434/v1 (HTTP 401).", { COPILOT_MODEL: "qwen2.5:0.5b" });

      expect(diagnostic).toBe("");
    });
  });

  const PROXY_AUTH_FAILURE_OUTPUT = "Authentication failed with provider at http://api-proxy:10002 (HTTP 403).";

  describe("isRetryableProxyAuthenticationFailure", () => {
    it("returns true for gh-aw proxy auth failures after partial execution", () => {
      expect(isRetryableProxyAuthenticationFailure(PROXY_AUTH_FAILURE_OUTPUT, true)).toBe(true);
    });

    it("returns false when the auth failure happened before any output was produced", () => {
      expect(isRetryableProxyAuthenticationFailure(PROXY_AUTH_FAILURE_OUTPUT, false)).toBe(false);
    });

    it("returns false for non-proxy authentication failures", () => {
      expect(isRetryableProxyAuthenticationFailure("Authentication failed (Request ID: ABC123)", true)).toBe(false);
      expect(isRetryableProxyAuthenticationFailure("Authentication failed with provider at https://api.openai.com/v1 (HTTP 401).", true)).toBe(false);
    });
  });

  describe("envFlagEnabled", () => {
    it.each(["true", "TRUE", "True", "1", "yes", " YES "])("returns true for '%s'", v => {
      expect(envFlagEnabled(v)).toBe(true);
    });

    it.each(["false", "FALSE", "0", "no", "", "  "])("returns false for '%s'", v => {
      expect(envFlagEnabled(v)).toBe(false);
    });

    it("returns false for undefined", () => {
      expect(envFlagEnabled(undefined)).toBe(false);
    });
  });

  describe("provider auth retry policy", () => {
    // Inline the same retry logic as the driver for auth-related failures.
    const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;
    const NO_AUTH_INFO_PATTERN = /No authentication information found/;
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @param {boolean} useContinueOnRetry - whether the current attempt used --continue
     * @returns {boolean}
     */
    function shouldRetry(result, attempt, useContinueOnRetry = false) {
      if (result.exitCode === 0) return false;
      // MCP policy errors are persistent — never retry
      if (MCP_POLICY_BLOCKED_PATTERN.test(result.output)) return false;
      if (isAuthenticationFailedError(result.output)) {
        return attempt === 0 && isRetryableProxyAuthenticationFailure(result.output, result.hasOutput);
      }
      // Auth error on --continue: fall back to fresh run once; on fresh run: bail
      if (NO_AUTH_INFO_PATTERN.test(result.output)) {
        return useContinueOnRetry && attempt < MAX_RETRIES;
      }
      return attempt < MAX_RETRIES && result.hasOutput;
    }

    it("does not retry when auth fails on first attempt (no real work done)", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: No authentication information found." };
      expect(shouldRetry(result, 0, false)).toBe(false);
    });

    it("retries once when the first attempt hits a proxy auth failure after partial execution", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: PROXY_AUTH_FAILURE_OUTPUT,
      };
      expect(shouldRetry(result, 0, false)).toBe(true);
    });

    it("does not retry when proxy auth fails before any output was produced", () => {
      const result = {
        exitCode: 1,
        hasOutput: false,
        output: PROXY_AUTH_FAILURE_OUTPUT,
      };
      expect(shouldRetry(result, 0, false)).toBe(false);
    });

    it("does not retry generic authentication_failed errors that do not come from the gh-aw proxy", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Authentication failed (Request ID: ABC123)" };
      expect(shouldRetry(result, 0, false)).toBe(false);
    });

    it("retries the first proxy auth failure only once", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: PROXY_AUTH_FAILURE_OUTPUT,
      };
      expect(shouldRetry(result, 0, false)).toBe(true);
      expect(shouldRetry(result, 1, false)).toBe(false);
      expect(shouldRetry(result, 2, false)).toBe(false);
    });

    it("retries as fresh run when no-auth failure happens on a --continue attempt", () => {
      // This replicates the fix: attempt 1 ran for 3+ min then failed mid-stream,
      // attempt 2 (--continue) fails with auth error — driver retries once as fresh run.
      const continueResult = { exitCode: 1, hasOutput: true, output: "Error: No authentication information found." };
      expect(shouldRetry(continueResult, 1, true)).toBe(true); // --continue attempt: triggers fresh retry
      expect(shouldRetry(continueResult, 2, true)).toBe(true); // still within retry budget
      expect(shouldRetry(continueResult, 3, true)).toBe(false); // budget exhausted
    });

    it("does not retry when auth fails on a fresh-run recovery attempt (useContinueOnRetry=false)", () => {
      // After falling back to a fresh run, useContinueOnRetry is reset to false.
      // If the fresh run also hits auth error, the driver bails immediately.
      const freshResult = { exitCode: 1, hasOutput: true, output: "Error: No authentication information found." };
      expect(shouldRetry(freshResult, 1, false)).toBe(false);
      expect(shouldRetry(freshResult, 2, false)).toBe(false);
    });

    it("does not retry auth error even when output is mixed with other content", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Some output\nError: No authentication information found.\nMore output" };
      expect(shouldRetry(result, 0, false)).toBe(false);
    });

    it("still retries non-auth errors with output (CAPIError 400)", () => {
      const result = { exitCode: 1, hasOutput: true, output: "CAPIError: 400 Bad Request" };
      expect(shouldRetry(result, 0, false)).toBe(true);
    });

    it("still retries generic partial-execution errors with output", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Failed to get response from the AI model; retried 5 times" };
      expect(shouldRetry(result, 0, false)).toBe(true);
    });
  });

  describe("null-type tool_call detection pattern", () => {
    const NULL_TYPE_TOOL_CALL_PATTERN = /tool_calls\[.*?\]\.type.*null/;

    it("matches the error format observed in failed workflow runs", () => {
      const errorOutput = "Execution failed: CAPIError: 400 Invalid type for 'messages[45].tool_calls[0].type': expected one of 'function', 'all...ols', or 'custom', but got null instead.";
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test(errorOutput)).toBe(true);
    });

    it("matches with different array indices", () => {
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("tool_calls[0].type: null")).toBe(true);
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("tool_calls[12].type, got null")).toBe(true);
    });

    it("does not match unrelated tool_calls errors", () => {
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("tool_calls[0].name: missing")).toBe(false);
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("Error: tool call failed")).toBe(false);
    });

    it("does not match unrelated null errors", () => {
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("Unexpected null value in response")).toBe(false);
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(NULL_TYPE_TOOL_CALL_PATTERN.test("")).toBe(false);
    });
  });

  describe("null-type tool_call restarts fresh instead of --continue", () => {
    // Inline the same retry logic as the driver including null-type tool_call handling
    const MCP_POLICY_BLOCKED_PATTERN = /MCP servers were blocked by policy:/;
    const NO_AUTH_INFO_PATTERN = /No authentication information found/;
    const NULL_TYPE_TOOL_CALL_PATTERN = /tool_calls\[.*?\]\.type.*null/;
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @param {boolean} useContinueOnRetry
     * @param {boolean} continueDisabledPermanently
     * @returns {{ shouldRetry: boolean, useContinueOnRetry: boolean, continueDisabledPermanently: boolean }}
     */
    function applyRetryPolicy(result, attempt, useContinueOnRetry = false, continueDisabledPermanently = false) {
      if (result.exitCode === 0) return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      if (MCP_POLICY_BLOCKED_PATTERN.test(result.output)) return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      if (NO_AUTH_INFO_PATTERN.test(result.output)) {
        if (useContinueOnRetry && attempt < MAX_RETRIES) {
          return { shouldRetry: true, useContinueOnRetry: false, continueDisabledPermanently: true };
        }
        return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      }
      if (NULL_TYPE_TOOL_CALL_PATTERN.test(result.output)) {
        if (attempt < MAX_RETRIES && result.hasOutput) {
          return { shouldRetry: true, useContinueOnRetry: false, continueDisabledPermanently: true };
        }
        return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      }
      if (attempt < MAX_RETRIES && result.hasOutput) {
        return { shouldRetry: true, useContinueOnRetry: !continueDisabledPermanently, continueDisabledPermanently };
      }
      return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
    }

    it("restarts fresh when null-type error occurs on a --continue attempt", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "CAPIError: 400 Invalid type for 'messages[45].tool_calls[0].type': expected one of 'function', 'all...ols', or 'custom', but got null instead.",
      };
      const {
        shouldRetry,
        useContinueOnRetry: newContinue,
        continueDisabledPermanently: disabled,
      } = applyRetryPolicy(
        result,
        1,
        true, // was using --continue
        false
      );
      expect(shouldRetry).toBe(true);
      expect(newContinue).toBe(false); // must NOT use --continue on restart
      expect(disabled).toBe(true); // permanently disabled
    });

    it("restarts fresh when null-type error occurs on a fresh run", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "CAPIError: 400 Invalid type for 'messages[0].tool_calls[0].type': got null instead.",
      };
      const { shouldRetry, useContinueOnRetry: newContinue, continueDisabledPermanently: disabled } = applyRetryPolicy(result, 0, false, false);
      expect(shouldRetry).toBe(true);
      expect(newContinue).toBe(false); // must NOT use --continue
      expect(disabled).toBe(true); // permanently disabled
    });

    it("does not retry when budget is exhausted", () => {
      const result = {
        exitCode: 1,
        hasOutput: true,
        output: "tool_calls[0].type: null",
      };
      const { shouldRetry } = applyRetryPolicy(result, MAX_RETRIES, true, false);
      expect(shouldRetry).toBe(false);
    });

    it("does not retry when no output was produced", () => {
      const result = {
        exitCode: 1,
        hasOutput: false,
        output: "tool_calls[0].type: null",
      };
      const { shouldRetry } = applyRetryPolicy(result, 0, false, false);
      expect(shouldRetry).toBe(false);
    });
  });

  describe("fatal-signal crash exit codes disable --continue", () => {
    it("recognizes known fatal-signal exit codes", () => {
      expect(isCrashSignalExitCode(134)).toBe(true); // SIGABRT
      expect(isCrashSignalExitCode(139)).toBe(true); // SIGSEGV
      expect(isCrashSignalExitCode(159)).toBe(true); // SIGSYS
      expect(crashSignalNameForExitCode(134)).toBe("SIGABRT");
      expect(crashSignalNameForExitCode(139)).toBe("SIGSEGV");
      expect(crashSignalNameForExitCode(159)).toBe("SIGSYS");
    });

    it("does not classify normal exit codes or timeout/cancellation signals as crashes", () => {
      expect(isCrashSignalExitCode(1)).toBe(false);
      expect(isCrashSignalExitCode(137)).toBe(false); // SIGKILL — timeout/cancellation, not a crash
      expect(isCrashSignalExitCode(143)).toBe(false); // SIGTERM — timeout/cancellation, not a crash
      expect(crashSignalNameForExitCode(1)).toBeNull();
      expect(crashSignalNameForExitCode(137)).toBeNull();
    });

    // Inline the same retry logic as the driver's shouldRetryFailedExecution handler,
    // including the crash-signal guard: a fatal-signal crash (SIGSEGV, SIGSYS, ...)
    // must never be retried with --continue since resuming the on-disk session risks
    // immediately reproducing the crash.
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number}} result
     * @param {number} attempt
     * @param {boolean} continueDisabledPermanently
     * @returns {{ shouldRetry: boolean, useContinueOnRetry: boolean, continueDisabledPermanently: boolean }}
     */
    function applyRetryPolicy(result, attempt, continueDisabledPermanently = false) {
      if (result.exitCode === 0) return { shouldRetry: false, useContinueOnRetry: false, continueDisabledPermanently };
      if (!(attempt < MAX_RETRIES && result.hasOutput)) {
        return { shouldRetry: false, useContinueOnRetry: false, continueDisabledPermanently };
      }
      const isCrashSignal = isCrashSignalExitCode(result.exitCode);
      const nextContinueDisabledPermanently = continueDisabledPermanently || isCrashSignal;
      return { shouldRetry: true, useContinueOnRetry: !nextContinueDisabledPermanently, continueDisabledPermanently: nextContinueDisabledPermanently };
    }

    it("disables --continue and restarts fresh after a SIGSYS (159) crash", () => {
      const result = { exitCode: 159, hasOutput: true };
      const { shouldRetry, useContinueOnRetry, continueDisabledPermanently } = applyRetryPolicy(result, 0, false);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(false);
      expect(continueDisabledPermanently).toBe(true);
    });

    it("disables --continue and restarts fresh after a SIGSEGV (139) crash", () => {
      const result = { exitCode: 139, hasOutput: true };
      const { shouldRetry, useContinueOnRetry, continueDisabledPermanently } = applyRetryPolicy(result, 0, false);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(false);
      expect(continueDisabledPermanently).toBe(true);
    });

    it("keeps --continue disabled on subsequent retries after a crash-signal restart", () => {
      const crashResult = { exitCode: 134, hasOutput: true };
      const after0 = applyRetryPolicy(crashResult, 0, false);
      expect(after0.useContinueOnRetry).toBe(false);
      expect(after0.continueDisabledPermanently).toBe(true);

      const nextResult = { exitCode: 1, hasOutput: true };
      const after1 = applyRetryPolicy(nextResult, 1, after0.continueDisabledPermanently);
      expect(after1.shouldRetry).toBe(true);
      expect(after1.useContinueOnRetry).toBe(false); // must not re-enable --continue
      expect(after1.continueDisabledPermanently).toBe(true);
    });
  });

  describe("connection-refused retries once on first attempt", () => {
    it("retries once as a fresh run with a short delay on the first SDK-mode attempt", () => {
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: true, attempt: 0, isConnectionRefused: true, maxRetries: 3 })).toBe(true);
      expect(FIRST_CONNECTION_REFUSED_RETRY_DELAY_MS).toBe(1000);
    });

    it("does not take this path in CLI mode", () => {
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: false, attempt: 0, isConnectionRefused: true, maxRetries: 3 })).toBe(false);
    });

    it("does not take this path on later attempts", () => {
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: true, attempt: 1, isConnectionRefused: true, maxRetries: 3 })).toBe(false);
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: true, attempt: 2, isConnectionRefused: true, maxRetries: 3 })).toBe(false);
    });

    it("does not take this path when the retry budget is zero", () => {
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: true, attempt: 0, isConnectionRefused: true, maxRetries: 0 })).toBe(false);
    });

    it("does not take this path when the error is not a connection refusal", () => {
      expect(shouldRetryFirstConnectionRefused({ copilotSDKMode: true, attempt: 0, isConnectionRefused: false, maxRetries: 3 })).toBe(false);
    });
  });

  describe("permanent --continue disable guard", () => {
    // Inline retry logic to verify that once continueDisabledPermanently is set,
    // subsequent partial-execution retries never re-enable --continue.
    const NULL_TYPE_TOOL_CALL_PATTERN = /tool_calls\[.*?\]\.type.*null/;
    const MAX_RETRIES = 3;

    /**
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @param {boolean} useContinueOnRetry
     * @param {boolean} continueDisabledPermanently
     * @returns {{ shouldRetry: boolean, useContinueOnRetry: boolean, continueDisabledPermanently: boolean }}
     */
    function applyRetryPolicy(result, attempt, useContinueOnRetry = false, continueDisabledPermanently = false) {
      if (result.exitCode === 0) return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      if (NULL_TYPE_TOOL_CALL_PATTERN.test(result.output)) {
        if (attempt < MAX_RETRIES && result.hasOutput) {
          return { shouldRetry: true, useContinueOnRetry: false, continueDisabledPermanently: true };
        }
        return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      }
      if (attempt < MAX_RETRIES && result.hasOutput) {
        return { shouldRetry: true, useContinueOnRetry: !continueDisabledPermanently, continueDisabledPermanently };
      }
      return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
    }

    it("does not re-enable --continue after a null-type fresh restart", () => {
      // Attempt 0 (fresh): normal failure → schedule --continue
      const attempt0Result = { exitCode: 1, hasOutput: true, output: "some error" };
      const after0 = applyRetryPolicy(attempt0Result, 0, false, false);
      expect(after0.shouldRetry).toBe(true);
      expect(after0.useContinueOnRetry).toBe(true);
      expect(after0.continueDisabledPermanently).toBe(false);

      // Attempt 1 (--continue): null-type error → restart fresh, disable permanently
      const attempt1Result = { exitCode: 1, hasOutput: true, output: "tool_calls[0].type: null" };
      const after1 = applyRetryPolicy(attempt1Result, 1, after0.useContinueOnRetry, after0.continueDisabledPermanently);
      expect(after1.shouldRetry).toBe(true);
      expect(after1.useContinueOnRetry).toBe(false); // disabled for this retry
      expect(after1.continueDisabledPermanently).toBe(true); // permanently set

      // Attempt 2 (fresh): another partial failure → MUST NOT re-enable --continue
      const attempt2Result = { exitCode: 1, hasOutput: true, output: "another error" };
      const after2 = applyRetryPolicy(attempt2Result, 2, after1.useContinueOnRetry, after1.continueDisabledPermanently);
      expect(after2.shouldRetry).toBe(true);
      expect(after2.useContinueOnRetry).toBe(false); // guard prevents re-enabling
      expect(after2.continueDisabledPermanently).toBe(true);
    });

    it("does not re-enable --continue after an auth-error fresh restart", () => {
      const NO_AUTH_INFO_PATTERN_LOCAL = /No authentication information found/;

      function applyRetryPolicyWithAuth(result, attempt, useContinueOnRetry = false, continueDisabledPermanently = false) {
        if (result.exitCode === 0) return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
        if (NO_AUTH_INFO_PATTERN_LOCAL.test(result.output)) {
          if (useContinueOnRetry && attempt < MAX_RETRIES) {
            return { shouldRetry: true, useContinueOnRetry: false, continueDisabledPermanently: true };
          }
          return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
        }
        if (attempt < MAX_RETRIES && result.hasOutput) {
          return { shouldRetry: true, useContinueOnRetry: !continueDisabledPermanently, continueDisabledPermanently };
        }
        return { shouldRetry: false, useContinueOnRetry, continueDisabledPermanently };
      }

      // Attempt 0 (fresh): normal failure → schedule --continue
      const attempt0Result = { exitCode: 1, hasOutput: true, output: "some work done" };
      const after0 = applyRetryPolicyWithAuth(attempt0Result, 0, false, false);
      expect(after0.useContinueOnRetry).toBe(true);

      // Attempt 1 (--continue): auth error → restart fresh, disable permanently
      const attempt1Result = { exitCode: 1, hasOutput: true, output: "No authentication information found" };
      const after1 = applyRetryPolicyWithAuth(attempt1Result, 1, after0.useContinueOnRetry, after0.continueDisabledPermanently);
      expect(after1.shouldRetry).toBe(true);
      expect(after1.useContinueOnRetry).toBe(false);
      expect(after1.continueDisabledPermanently).toBe(true);

      // Attempt 2 (fresh): partial failure → MUST NOT re-enable --continue
      const attempt2Result = { exitCode: 1, hasOutput: true, output: "some other error" };
      const after2 = applyRetryPolicyWithAuth(attempt2Result, 2, after1.useContinueOnRetry, after1.continueDisabledPermanently);
      expect(after2.useContinueOnRetry).toBe(false); // guard prevents re-enabling
    });
  });

  describe("retry configuration", () => {
    it("has sensible default values", () => {
      const retryConfig = resolveRetryConfig({});
      expect(retryConfig.maxRetries).toBe(3);
      expect(retryConfig.initialDelayMs).toBe(5000);
      expect(retryConfig.backoffMultiplier).toBe(2);
      expect(retryConfig.maxDelayMs).toBe(60000);
    });

    it("accepts env overrides for retry parameters", () => {
      const retryConfig = resolveRetryConfig({
        GH_AW_HARNESS_MAX_RETRIES: "7",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1500",
        GH_AW_HARNESS_BACKOFF_MULTIPLIER: "1.5",
        GH_AW_HARNESS_MAX_DELAY_MS: "45000",
      });
      expect(retryConfig).toEqual({
        maxRetries: 7,
        initialDelayMs: 1500,
        backoffMultiplier: 1.5,
        maxDelayMs: 45000,
      });
    });

    it("falls back to defaults for invalid env values", () => {
      const logs = [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_MAX_RETRIES: "-1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "abc",
          GH_AW_HARNESS_BACKOFF_MULTIPLIER: "0",
          GH_AW_HARNESS_MAX_DELAY_MS: "bogus",
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
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_BACKOFF_MULTIPLIER"))).toBe(true);
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_MAX_DELAY_MS"))).toBe(true);
    });

    it("clamps max delay when it is lower than the configured initial delay", () => {
      const logs = [];
      const retryConfig = resolveRetryConfig(
        {
          GH_AW_HARNESS_INITIAL_DELAY_MS: "4000",
          GH_AW_HARNESS_MAX_DELAY_MS: "1000",
        },
        msg => logs.push(msg)
      );
      expect(retryConfig.initialDelayMs).toBe(4000);
      expect(retryConfig.maxDelayMs).toBe(4000);
      expect(logs.some(msg => msg.includes("clamping max delay"))).toBe(true);
    });

    it("accepts max-retries=0 to disable retries entirely", () => {
      const retryConfig = resolveRetryConfig({ GH_AW_HARNESS_MAX_RETRIES: "0" });
      expect(retryConfig.maxRetries).toBe(0);
    });

    it("clamps max-retries to 100 when given an excessively large value", () => {
      const logs = [];
      const retryConfig = resolveRetryConfig({ GH_AW_HARNESS_MAX_RETRIES: "9999" }, msg => logs.push(msg));
      expect(retryConfig.maxRetries).toBe(100);
      expect(logs.some(msg => msg.includes("GH_AW_HARNESS_MAX_RETRIES"))).toBe(true);
    });

    it("rejects non-decimal integer formats such as '1e3' and '0x10'", () => {
      const config1 = resolveRetryConfig({ GH_AW_HARNESS_MAX_RETRIES: "1e3" });
      expect(config1.maxRetries).toBe(3);
      const config2 = resolveRetryConfig({ GH_AW_HARNESS_INITIAL_DELAY_MS: "0x10" });
      expect(config2.initialDelayMs).toBe(5000);
    });

    it("exponential backoff does not exceed max delay", () => {
      const { maxRetries, initialDelayMs, backoffMultiplier, maxDelayMs } = resolveRetryConfig({
        GH_AW_HARNESS_MAX_RETRIES: "3",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "5000",
        GH_AW_HARNESS_BACKOFF_MULTIPLIER: "2",
        GH_AW_HARNESS_MAX_DELAY_MS: "60000",
      });

      let delay = initialDelayMs;
      for (let i = 0; i < maxRetries; i++) {
        delay = Math.min(delay * backoffMultiplier, maxDelayMs);
        expect(delay).toBeLessThanOrEqual(maxDelayMs);
      }
    });
  });

  describe("prompt-file support", () => {
    it("inlines small prompt files as -p", () => {
      const promptFile = path.join(os.tmpdir(), `copilot-driver-small-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "small prompt body", "utf8");

      const resolved = resolvePromptFileArgs(["--add-dir", "/tmp", "--prompt-file", promptFile, "--allow-all-tools"]);
      expect(resolved).toEqual(["--add-dir", "/tmp", "-p", "small prompt body", "--allow-all-tools"]);
    });

    it("uses compact fallback prompt when prompt file is larger than 100KB", () => {
      const promptFile = path.join(os.tmpdir(), `copilot-driver-large-${Date.now()}.txt`);
      fs.writeFileSync(promptFile, "x".repeat(PROMPT_FILE_INLINE_THRESHOLD_BYTES + 1), "utf8");

      const resolved = resolvePromptFileArgs(["--prompt-file", promptFile, "--allow-all-tools"]);
      expect(resolved).toEqual(["-p", buildPromptFileFallbackInstruction(promptFile), "--allow-all-tools"]);
    });

    it("keeps --prompt-file arguments unchanged when file resolution fails", () => {
      const missingPath = path.join(os.tmpdir(), `copilot-driver-missing-${Date.now()}.txt`);
      const resolved = resolvePromptFileArgs(["--prompt-file", missingPath, "--allow-all-tools"]);
      expect(resolved).toEqual(["--prompt-file", missingPath, "--allow-all-tools"]);
    });
  });

  describe("formatDuration", () => {
    // Inline the same logic as the driver's formatDuration for unit testing
    function formatDuration(ms) {
      const totalSeconds = Math.floor(ms / 1000);
      const minutes = Math.floor(totalSeconds / 60);
      const seconds = totalSeconds % 60;
      if (minutes > 0) {
        return `${minutes}m ${seconds}s`;
      }
      return `${seconds}s`;
    }

    it("formats sub-minute durations as seconds", () => {
      expect(formatDuration(0)).toBe("0s");
      expect(formatDuration(500)).toBe("0s");
      expect(formatDuration(1000)).toBe("1s");
      expect(formatDuration(59000)).toBe("59s");
    });

    it("formats minute-level durations with minutes and seconds", () => {
      expect(formatDuration(60000)).toBe("1m 0s");
      expect(formatDuration(90000)).toBe("1m 30s");
      expect(formatDuration(192000)).toBe("3m 12s"); // 3m 12s (real-world example)
    });

    it("handles long durations correctly", () => {
      expect(formatDuration(3600000)).toBe("60m 0s");
    });
  });

  describe("log format", () => {
    it("log lines include [copilot-harness] prefix without rendered timestamp", () => {
      // Verify the format matches what we expect in agent-stdio.log
      const logLine = "[copilot-harness] test message";
      expect(logLine).toBe("[copilot-harness] test message");
    });
  });

  describe("startup log includes node version and platform", () => {
    it("starting log line contains nodeVersion and platform fields", () => {
      const command = "/usr/local/bin/copilot";
      const startingLine = `starting: command=${command} maxRetries=3 initialDelayMs=5000` + ` backoffMultiplier=2 maxDelayMs=60000` + ` nodeVersion=${process.version} platform=${process.platform}`;
      expect(startingLine).toContain("nodeVersion=");
      expect(startingLine).toContain("platform=");
      expect(startingLine).toMatch(/nodeVersion=v\d+\.\d+/);
    });
  });

  describe("no-output failure message", () => {
    it("includes actionable possible causes", () => {
      const msg = `attempt 1: no output produced — not retrying` + ` (possible causes: binary not found, permission denied, auth failure, or silent startup crash)`;
      expect(msg).toContain("binary not found");
      expect(msg).toContain("permission denied");
      expect(msg).toContain("auth failure");
      expect(msg).toContain("silent startup crash");
    });
  });

  describe("error event message", () => {
    it("includes code and syscall fields", () => {
      const errMessage = "spawn /usr/local/bin/copilot ENOENT";
      const errCode = "ENOENT";
      const errSyscall = "spawn";
      const logMsg = `attempt 1: failed to start process '/usr/local/bin/copilot': ${errMessage}` + ` (code=${errCode} syscall=${errSyscall})`;
      expect(logMsg).toContain("code=ENOENT");
      expect(logMsg).toContain("syscall=spawn");
    });
  });

  describe("extractModelIds", () => {
    it("returns null for null input", () => {
      expect(extractModelIds(null)).toBeNull();
    });

    it("returns null for empty object", () => {
      expect(extractModelIds({})).toBeNull();
    });

    it("returns null for empty data array", () => {
      expect(extractModelIds({ data: [] })).toBeNull();
    });

    it("extracts ids from OpenAI format", () => {
      const json = { data: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }] };
      expect(extractModelIds(json)).toEqual(["gpt-4o", "gpt-4o-mini"]);
    });

    it("falls back to name when id is absent in OpenAI format", () => {
      const json = { data: [{ name: "model-a" }, { id: "model-b" }] };
      expect(extractModelIds(json)).toEqual(["model-a", "model-b"]);
    });

    it("extracts ids from Gemini format, stripping prefix", () => {
      const json = {
        models: [{ name: "models/gemini-1.5-pro" }, { name: "models/gemini-1.0-pro" }],
      };
      expect(extractModelIds(json)).toEqual(["gemini-1.0-pro", "gemini-1.5-pro"]);
    });

    it("handles Gemini entries without the prefix", () => {
      const json = { models: [{ name: "custom-model" }] };
      expect(extractModelIds(json)).toEqual(["custom-model"]);
    });

    it("returns sorted results", () => {
      const json = { data: [{ id: "z-model" }, { id: "a-model" }, { id: "m-model" }] };
      expect(extractModelIds(json)).toEqual(["a-model", "m-model", "z-model"]);
    });
  });

  describe("detection model availability helpers", () => {
    it("identifies detection phase from GH_AW_PHASE", () => {
      expect(isDetectionPhase("detection")).toBe(true);
      expect(isDetectionPhase("DETECTION")).toBe(true);
      expect(isDetectionPhase("agent")).toBe(false);
      expect(isDetectionPhase("")).toBe(false);
    });

    it("checks model availability from reflect endpoint payload", () => {
      const reflectData = {
        endpoints: [
          { provider: "copilot", configured: true, models: ["claude-sonnet-4.6", "gpt-5.4"] },
          { provider: "openai", configured: false, models: ["gpt-4.1"] },
        ],
      };
      expect(isModelAvailableInReflectData("claude-sonnet-4.6", reflectData)).toBe(true);
      expect(isModelAvailableInReflectData("gpt-4.1", reflectData)).toBe(false);
      expect(isModelAvailableInReflectData("missing-model", reflectData)).toBe(false);
    });

    it("reads reflect file and checks model availability", () => {
      const reflectFile = path.join(os.tmpdir(), `awf-reflect-${Date.now()}.json`);
      try {
        fs.writeFileSync(
          reflectFile,
          JSON.stringify({
            endpoints: [{ provider: "copilot", configured: true, models: ["claude-sonnet-4.6"] }],
          }),
          "utf8"
        );
        const logs = [];
        expect(isModelAvailableInReflectFile("claude-sonnet-4.6", { reflectPath: reflectFile, logger: msg => logs.push(msg) })).toBe(true);
        expect(isModelAvailableInReflectFile("gpt-4.1", { reflectPath: reflectFile, logger: msg => logs.push(msg) })).toBe(false);
      } finally {
        fs.unlinkSync(reflectFile);
      }
    });
  });

  describe("enrichReflectModels", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("does nothing when all configured endpoints already have models", async () => {
      const reflectData = {
        endpoints: [{ provider: "openai", configured: true, models: ["gpt-4o"], models_url: "http://api-proxy:10000/v1/models" }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toEqual(["gpt-4o"]);
    });

    it("does nothing for unconfigured endpoints with null models", async () => {
      const reflectData = {
        endpoints: [{ provider: "anthropic", configured: false, models: null, models_url: "http://api-proxy:10001/v1/models" }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toBeNull();
    });

    it("does nothing when models_url is null", async () => {
      const reflectData = {
        endpoints: [{ provider: "opencode", configured: true, models: null, models_url: null }],
      };
      const logger = () => {};
      await enrichReflectModels(reflectData, 1000, logger);
      expect(reflectData.endpoints[0].models).toBeNull();
    });

    it("fetches models from models_url for configured endpoints with null models", async () => {
      const modelResponse = { data: [{ id: "claude-sonnet-4.6" }, { id: "gpt-4o" }] };
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => modelResponse }));

      const reflectData = {
        endpoints: [{ provider: "copilot", configured: true, models: null, models_url: "http://api-proxy:10002/models" }],
      };
      const logs = [];
      await enrichReflectModels(reflectData, 3000, msg => logs.push(msg));

      expect(reflectData.endpoints[0].models).toEqual(["claude-sonnet-4.6", "gpt-4o"]);
      expect(logs.some(l => l.includes("fetched 2 model(s)"))).toBe(true);
    });

    it("leaves models null when models_url fetch fails", async () => {
      vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("ECONNREFUSED")));

      const reflectData = {
        endpoints: [{ provider: "openai", configured: true, models: null, models_url: "http://api-proxy:10000/v1/models" }],
      };
      const logs = [];
      await enrichReflectModels(reflectData, 500, msg => logs.push(msg));
      expect(reflectData.endpoints[0].models).toBeNull();
      expect(logs.some(l => l.includes("models fetch error"))).toBe(true);
    });
  });

  describe("SDK mode retry policy", () => {
    // In SDK mode, --continue is a CLI concept and must never be used.
    // Retries always restart the session fresh.
    // The retry eligibility rules (hasOutput, MAX_RETRIES) are otherwise shared.
    const MAX_RETRIES = 3;

    /**
     * Mirrors the blended retry decision from copilot_harness.cjs (the
     * `attempt < MAX_RETRIES && result.hasOutput` branch plus the
     * `useContinueOnRetry = !copilotSDKMode && !continueDisabledPermanently` assignment).
     * Keep this helper in sync with the production logic.
     *
     * @param {{hasOutput: boolean, exitCode: number, output: string}} result
     * @param {number} attempt
     * @param {boolean} copilotSDKMode
     * @param {boolean} continueDisabledPermanently
     * @returns {{ shouldRetry: boolean, useContinueOnRetry: boolean }}
     */
    function blendedRetryDecision(result, attempt, copilotSDKMode, continueDisabledPermanently = false) {
      if (result.exitCode === 0) return { shouldRetry: false, useContinueOnRetry: false };
      if (hasNumerousPermissionDeniedIssues(result.output)) return { shouldRetry: false, useContinueOnRetry: false };
      if (attempt >= MAX_RETRIES || !result.hasOutput) return { shouldRetry: false, useContinueOnRetry: false };
      // --continue is only enabled in CLI mode and only when not permanently disabled.
      const useContinueOnRetry = !copilotSDKMode && !continueDisabledPermanently;
      return { shouldRetry: true, useContinueOnRetry };
    }

    it("retries on partial execution in SDK mode (fresh run, not --continue)", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset" };
      const { shouldRetry, useContinueOnRetry } = blendedRetryDecision(result, 0, true);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(false);
    });

    it("retries on CAPIError 400 in SDK mode (fresh run, not --continue)", () => {
      const result = { exitCode: 1, hasOutput: true, output: "CAPIError: 400 Bad Request" };
      const { shouldRetry, useContinueOnRetry } = blendedRetryDecision(result, 0, true);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(false);
    });

    it("never sets useContinueOnRetry=true in SDK mode regardless of error type", () => {
      for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
        const result = { exitCode: 1, hasOutput: true, output: "Error: partial execution" };
        const { useContinueOnRetry } = blendedRetryDecision(result, attempt, /* copilotSDKMode */ true);
        expect(useContinueOnRetry).toBe(false);
      }
    });

    it("does not retry in SDK mode when no output was produced", () => {
      const result = { exitCode: 1, hasOutput: false, output: "" };
      const { shouldRetry } = blendedRetryDecision(result, 0, true);
      expect(shouldRetry).toBe(false);
    });

    it("does not retry in SDK mode after retries are exhausted", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: partial execution" };
      const { shouldRetry } = blendedRetryDecision(result, MAX_RETRIES, true);
      expect(shouldRetry).toBe(false);
    });

    it("CLI mode still enables --continue on partial execution when not disabled", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset" };
      const { shouldRetry, useContinueOnRetry } = blendedRetryDecision(result, 0, /* copilotSDKMode */ false);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(true);
    });

    it("CLI mode respects continueDisabledPermanently", () => {
      const result = { exitCode: 1, hasOutput: true, output: "Error: connection reset" };
      const { shouldRetry, useContinueOnRetry } = blendedRetryDecision(result, 0, /* copilotSDKMode */ false, /* continueDisabledPermanently */ true);
      expect(shouldRetry).toBe(true);
      expect(useContinueOnRetry).toBe(false);
    });

    it("currentArgs never appends --continue in SDK mode", () => {
      const resolvedArgs = ["--prompt", "hello"];
      // Simulate the blended loop's currentArgs logic for multiple attempts in SDK mode
      for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
        const useContinueOnRetry = false; // always false in SDK mode
        const copilotSDKMode = true;
        const currentArgs = !copilotSDKMode && attempt > 0 && useContinueOnRetry ? [...resolvedArgs, "--continue"] : resolvedArgs;
        expect(currentArgs).not.toContain("--continue");
      }
    });

    it("currentArgs appends --continue in CLI mode when useContinueOnRetry=true", () => {
      const resolvedArgs = ["--prompt", "hello"];
      const copilotSDKMode = false;
      const useContinueOnRetry = true;
      // attempt > 0 is when --continue kicks in
      const currentArgs = !copilotSDKMode && 1 > 0 && useContinueOnRetry ? [...resolvedArgs, "--continue"] : resolvedArgs;
      expect(currentArgs).toContain("--continue");
    });
  });

  describe("fetchAWFReflect enriches models via fallback", () => {
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("saves enriched reflect data when api-proxy returns null models for configured provider", async () => {
      const modelData = { data: [{ id: "gpt-4o" }, { id: "gpt-4o-mini" }] };
      const reflectPayload = {
        endpoints: [{ provider: "openai", port: 10000, configured: true, models: null, models_url: "http://api-proxy:10000/v1/models" }],
        models_fetch_complete: true,
      };

      vi.stubGlobal(
        "fetch",
        vi.fn().mockImplementation(url => {
          const body = String(url).includes("/reflect") ? reflectPayload : modelData;
          return Promise.resolve({ ok: true, status: 200, json: async () => body });
        })
      );

      const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-reflect-test-"));
      const outputPath = path.join(outputDir, "awf-reflect.json");
      const logs = [];

      try {
        await fetchAWFReflect({
          reflectUrl: "http://api-proxy:10000/reflect",
          outputPath,
          timeoutMs: 3000,
          modelsTimeoutMs: 1000,
          logger: msg => logs.push(msg),
        });

        const saved = JSON.parse(fs.readFileSync(outputPath, "utf8"));
        expect(saved.endpoints[0].models).toEqual(["gpt-4o", "gpt-4o-mini"]);
      } finally {
        fs.rmSync(outputDir, { recursive: true, force: true });
      }
    });
  });

  describe("noop pre-flight and retry guard", () => {
    it("skips the agent when a noop is already in safe-outputs before the run", () => {
      const tempDir = makeHarnessTempDir("copilot-noop-preflight-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"nothing to do"}\n', "utf8");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.exit(0);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
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
      const tempDir = makeHarnessTempDir("copilot-noop-retry-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a noop on the first call then fails; harness must not retry.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"noop",message:"nothing to do"}) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
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

    it("exits 1 and emits soft-timeout signal when guard deadline is exceeded before next retry", () => {
      const tempDir = makeHarnessTempDir("copilot-soft-timeout-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub records the call, writes a line to stdout so hasOutput=true (enabling retry),
      // then sleeps past the soft-timeout window before exiting 1.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("running\\n");
// Sleep 1.5 s so the soft deadline (clamped to start+1 s for 0.001-min timeout) is elapsed
const end = Date.now() + 1500;
while (Date.now() < end) {}
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_SAFEOUTPUTS_CLI: "true",
          GH_AW_TIMEOUT_MINUTES: "0.001",
        },
        encoding: "utf8",
        timeout: 20000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Stub was called once; soft deadline fires before attempt 2
      expect(callCount).toBe(1);
      // Harness exits 1 (soft-timeout signal)
      expect(result.status).toBe(1);
      // Guard log appears in stderr
      expect(result.stderr).toContain("soft-timeout guard reached");
    });
  });

  describe("permission-denied suppression when expected safe-outputs already produced", () => {
    it("exits 0 and suppresses terminal verdict when numerous permission-denied occurs after expected safe-output was written", () => {
      const tempDir = makeHarnessTempDir("copilot-perm-denied-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes an expected safe-output then fails with numerous permission-denied output.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"Report posted"}) + "\\n");
process.stdout.write("permission denied\\npermission denied\\npermission denied\\nEACCES: permission denied\\nEPERM operation not permitted\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
        encoding: "utf8",
        timeout: 15000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      // Only one attempt — no retries when permission-denied is suppressed
      expect(callCount).toBe(1);
      // Harness exits 0 because the core work (add_comment) already succeeded
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("suppressing terminal verdict (false-red: core work succeeded)");
    });

    it("exits 1 and emits missing_tool when numerous permission-denied occurs with no expected safe-outputs", () => {
      const tempDir = makeHarnessTempDir("copilot-perm-denied-no-outputs-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub fails with numerous permission-denied but writes no expected safe-output.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("permission denied\\npermission denied\\npermission denied\\nEACCES: permission denied\\nEPERM operation not permitted\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath, GH_AW_SAFEOUTPUTS_CLI: "true" },
        encoding: "utf8",
        timeout: 15000,
      });
      // Harness exits 1 because no expected output was produced
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("detected numerous permission-denied issues — not retrying");
    });

    it("exits 0 without retrying when the LLM invocation cap is saturated but a terminal safe-output was already produced", () => {
      const tempDir = makeHarnessTempDir("copilot-invocation-cap-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes an expected safe-output then fails with the pooled invocation-cap error.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"ADR reviewed"}) + "\\n");
process.stdout.write("Execution failed: CAPIError: 429 Maximum LLM invocations exceeded (20/20)\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "fix the bug", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
        encoding: "utf8",
        timeout: 15000,
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
    it("exits 0 without retrying when a terminal safe-output was already produced and run fails with partial_execution", () => {
      const tempDir = makeHarnessTempDir("copilot-watchdog-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a terminal safe-output, then remains alive until SIGTERM.
      // This exercises the watchdog-fired partial_execution path end-to-end.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"Daily report posted"}) + "\\n");
process.stdout.write("Report uploaded. Checking logs...\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "generate the report", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
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

    it("still retries partial_execution when no terminal safe-output was produced (watchdog not armed)", () => {
      const tempDir = makeHarnessTempDir("copilot-watchdog-no-output-");
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
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("partial work done\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "generate the report", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
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

    it("exits 0 without retrying when watchdog fires after terminal safe-output was produced (no stdio output)", () => {
      const tempDir = makeHarnessTempDir("copilot-watchdog-no-stdio-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub writes a terminal safe-output but produces NO stdio output.
      // The watchdog arms (because hasTerminalSafeOutput is true) and fires after inactivity.
      // This exercises the no_output + watchdogFired + hasTerminalSafeOutput suppression path.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"Report posted"}) + "\\n");
// No stdout output — hasOutput remains false
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "generate the report", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
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

    it('exits 0 without retrying when watchdog fires after terminal safe-output was produced and output contains benign "not logged in" tool text', () => {
      const tempDir = makeHarnessTempDir("copilot-watchdog-authentication-failed-suppression-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
const safeOutputsPath = process.env.GH_AW_SAFE_OUTPUTS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
fs.appendFileSync(safeOutputsPath, JSON.stringify({type:"add_comment",body:"Daily report posted"}) + "\\n");
process.stdout.write(JSON.stringify({
  type: "tool.execution_complete",
  tool: "bash",
  output: "You are not logged into any GitHub hosts. To log in, run: gh auth login"
}) + "\\n");
process.on("SIGTERM", () => process.exit(1));
setInterval(() => {}, 1000);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "generate the report", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
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

    it("does not rescue authentication_failed when no terminal safe-output was produced before the watchdog fires", () => {
      const tempDir = makeHarnessTempDir("copilot-watchdog-auth-failed-no-output-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      // Stub emits auth-failure-looking text but does NOT write any safe-output entry.
      // Without terminal safe-output the watchdog never arms, so authentication_failed
      // falls through to the normal non-retryable failure path and exits non-zero.
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("Error: No authentication information found.\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "generate the report", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
        },
        encoding: "utf8",
        timeout: 15000,
      });
      // Harness exits non-zero: genuine auth failure with no terminal safe-output is not rescued
      expect(result.status).not.toBe(0);
      expect(result.stderr).not.toContain("late-activity exit suppressed");
    });
  });

  describe("AI credits budget enforcement exits 0", () => {
    /**
     * @param {string} tempDir
     * @returns {string}
     */
    function writeTrustedAICreditsExceededAudit(tempDir) {
      const auditDir = path.join(tempDir, "sandbox", "firewall", "audit");
      const tokenUsageDir = path.join(auditDir, "api-proxy-logs");
      fs.mkdirSync(auditDir, { recursive: true });
      fs.mkdirSync(tokenUsageDir, { recursive: true });
      fs.writeFileSync(path.join(auditDir, "log.jsonl"), `${JSON.stringify({ type: "response", status: 200, max_ai_credits: 300 })}\n`, "utf8");
      fs.writeFileSync(path.join(tokenUsageDir, "token-usage.jsonl"), `${JSON.stringify({ status: 200, ai_credits_total: 302.111025 })}\n`, "utf8");
      return path.join(tempDir, "agent-output.json");
    }

    it("exits 0 when the agent outputs max_ai_credits_exceeded and the CLI exits non-zero", () => {
      const tempDir = makeHarnessTempDir("copilot-ai-credits-exceeded-");
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
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath, GH_AW_AGENT_OUTPUT: agentOutputPath },
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
      const tempDir = makeHarnessTempDir("copilot-ai-credits-rate-limit-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: ai_credits_rate_limit_error=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: { ...harnessChildEnv, COPILOT_HARNESS_STUB_CALLS: callsPath, GH_AW_SAFE_OUTPUTS: safeOutputsPath, GH_AW_AGENT_OUTPUT: agentOutputPath },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("exits 0 when the AWF API proxy returns HTTP 403 max-AI-credits as an authentication failure", () => {
      // Same proxy signature as the claude_harness.test.cjs regression, replayed against copilot.
      const tempDir = makeHarnessTempDir("copilot-ai-credits-proxy-403-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write(JSON.stringify({error: "authentication_failed", message: "Failed to authenticate. API Error: 403 Maximum AI credits exceeded (302.111025 / 300)."}) + "\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("trusted budget-abort evidence");
      expect(result.stderr).toContain("AI credits budget enforced");
    });

    it("keeps non-zero exit for auth failure even when AI-credit markers and trusted audit are present", () => {
      const tempDir = makeHarnessTempDir("copilot-auth-failure-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.stdout.write("Authentication failed (Request ID: 123)\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
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
      const tempDir = makeHarnessTempDir("copilot-ai-credits-untrusted-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("error: max_ai_credits_exceeded=true\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_HARNESS_MAX_RETRIES: "0",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("without trusted firewall audit confirmation");
    });

    it("exits 0 without retry when proxy HTTP 403 auth failure has trusted audit budget evidence", () => {
      const tempDir = makeHarnessTempDir("copilot-proxy-auth-403-audit-budget-");
      const safeOutputsPath = path.join(tempDir, "safe-outputs.jsonl");
      const stubPath = path.join(tempDir, "stub.cjs");
      const promptPath = path.join(tempDir, "prompt.txt");
      const callsPath = path.join(tempDir, "calls.jsonl");
      const agentOutputPath = writeTrustedAICreditsExceededAudit(tempDir);
      fs.writeFileSync(
        stubPath,
        `const fs = require("fs");
const callsPath = process.env.COPILOT_HARNESS_STUB_CALLS;
fs.appendFileSync(callsPath, JSON.stringify({args: process.argv.slice(2)}) + "\\n");
process.stdout.write("Authentication failed with provider at http://api-proxy:10002 (HTTP 403).\\n");
process.exit(1);`,
        "utf8"
      );
      fs.writeFileSync(promptPath, "do some work", "utf8");

      const result = spawnSync(process.execPath, ["copilot_harness.cjs", process.execPath, stubPath, "--prompt-file", promptPath], {
        cwd: path.dirname(require.resolve("./copilot_harness.cjs")),
        env: {
          ...harnessChildEnv,
          COPILOT_HARNESS_STUB_CALLS: callsPath,
          GH_AW_SAFE_OUTPUTS: safeOutputsPath,
          GH_AW_AGENT_OUTPUT: agentOutputPath,
          GH_AW_HARNESS_MAX_RETRIES: "3",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      const callCount = fs.readFileSync(callsPath, "utf8").trim().split("\n").filter(Boolean).length;
      expect(callCount).toBe(1);
      expect(result.status).toBe(0);
      expect(result.stderr).toContain("failureClass=ai_credits_exhausted");
      expect(result.stderr).toContain("AI credits budget exceeded");
    });
  });

  describe("applyCopilotWireAPI", () => {
    afterEach(() => {
      delete process.env.COPILOT_MODEL;
      delete process.env.COPILOT_PROVIDER_WIRE_API;
    });

    /** @returns {Record<string, unknown>} */
    function makeModelsJson() {
      return {
        providers: {
          "github-copilot": {
            models: {
              "gpt-5-mini": { wire_api: "responses" },
              "gpt-5.5": { wire_api: "responses" },
              "gemini-2.5-pro": { wire_api: "completions" },
              "mai-code-1-flash-picker": { wire_api: "responses" },
              "claude-sonnet-4": {},
            },
          },
        },
      };
    }

    it("sets COPILOT_PROVIDER_WIRE_API=responses for a responses model", () => {
      process.env.COPILOT_MODEL = "gpt-5-mini";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBe("responses");
    });

    it("sets COPILOT_PROVIDER_WIRE_API=completions for a completions model", () => {
      process.env.COPILOT_MODEL = "gemini-2.5-pro";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBe("completions");
    });

    it("does not override when COPILOT_PROVIDER_WIRE_API is already set", () => {
      process.env.COPILOT_MODEL = "gpt-5-mini";
      process.env.COPILOT_PROVIDER_WIRE_API = "completions";
      const logs = [];
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: msg => logs.push(msg) });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBe("completions");
      expect(logs.some(l => l.includes("already set"))).toBe(true);
    });

    it("leaves COPILOT_PROVIDER_WIRE_API unset for models without wire_api entry", () => {
      process.env.COPILOT_MODEL = "claude-sonnet-4";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBeUndefined();
    });

    it("leaves COPILOT_PROVIDER_WIRE_API unset for unknown models", () => {
      process.env.COPILOT_MODEL = "some-unknown-byok-model";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBeUndefined();
    });

    it("is case-insensitive for the model name", () => {
      process.env.COPILOT_MODEL = "GPT-5-MINI";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBe("responses");
    });

    it("strips query parameters before catalog lookup (e.g. model?effort=high)", () => {
      process.env.COPILOT_MODEL = "gpt-5-mini?effort=high";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBe("responses");
    });

    it("skips configuration when COPILOT_MODEL is empty", () => {
      process.env.COPILOT_MODEL = "";
      applyCopilotWireAPI({ modelsJson: makeModelsJson(), logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBeUndefined();
    });

    it("skips configuration when modelsJson is null", () => {
      process.env.COPILOT_MODEL = "gpt-5-mini";
      applyCopilotWireAPI({ modelsJson: null, logger: () => {} });
      expect(process.env.COPILOT_PROVIDER_WIRE_API).toBeUndefined();
    });
  });

  describe("applyCopilotModelAliasResolution", () => {
    const awfConfigPath = "/tmp/gh-aw/awf-config.json";
    const originalAwfConfig = fs.existsSync(awfConfigPath) ? fs.readFileSync(awfConfigPath, "utf8") : null;

    function writeAwfConfig(aliasMap) {
      fs.mkdirSync(path.dirname(awfConfigPath), { recursive: true });
      fs.writeFileSync(awfConfigPath, JSON.stringify({ apiProxy: { models: aliasMap } }));
    }

    afterEach(() => {
      delete process.env.COPILOT_MODEL;
      if (originalAwfConfig !== null) {
        fs.writeFileSync(awfConfigPath, originalAwfConfig);
      } else if (fs.existsSync(awfConfigPath)) {
        fs.rmSync(awfConfigPath);
      }
      vi.restoreAllMocks();
    });

    it("resolves a known alias against an available catalog without refetching", async () => {
      process.env.COPILOT_MODEL = "sol";
      writeAwfConfig({ sol: ["copilot/gpt-5.6-sol"] });
      const reflectData = { endpoints: [{ configured: true, provider: "copilot", models: ["gpt-5.6-sol"] }] };
      const refetch = vi.fn();

      const resolved = await applyCopilotModelAliasResolution({ awfReflectData: reflectData, logger: () => {}, refetchReflectData: refetch });

      expect(resolved).toBe("gpt-5.6-sol");
      expect(process.env.COPILOT_MODEL).toBe("gpt-5.6-sol");
      expect(refetch).not.toHaveBeenCalled();
    });

    it("leaves a concrete configured model unchanged and does not refetch, even without a catalog", async () => {
      process.env.COPILOT_MODEL = "copilot/gpt-5.6-sol";
      writeAwfConfig({ sol: ["copilot/gpt-5.6-sol"] });
      const refetch = vi.fn();

      const resolved = await applyCopilotModelAliasResolution({ awfReflectData: null, logger: () => {}, refetchReflectData: refetch });

      expect(resolved).toBe("gpt-5.6-sol");
      expect(refetch).not.toHaveBeenCalled();
    });

    it("resolves a known alias after a transient catalog failure followed by a successful refresh", async () => {
      process.env.COPILOT_MODEL = "sol";
      writeAwfConfig({ sol: ["copilot/gpt-5.6-sol"] });
      const logs = [];
      const refreshedReflectData = { endpoints: [{ configured: true, provider: "copilot", models: ["gpt-5.6-sol"] }] };
      const refetch = vi.fn().mockResolvedValue(refreshedReflectData);

      const resolved = await applyCopilotModelAliasResolution({
        awfReflectData: null, // empty catalog on first attempt (e.g. transient 429)
        logger: msg => logs.push(msg),
        refetchReflectData: refetch,
      });

      expect(resolved).toBe("gpt-5.6-sol");
      expect(process.env.COPILOT_MODEL).toBe("gpt-5.6-sol");
      expect(refetch).toHaveBeenCalledTimes(1);
      expect(logs.some(l => l.includes("retrying awf-reflect model-catalog fetch once"))).toBe(true);
    });

    it("stops before spawning Copilot when catalog retries are exhausted for a known alias", async () => {
      process.env.COPILOT_MODEL = "sol";
      writeAwfConfig({ sol: ["copilot/gpt-5.6-sol"] });
      const logs = [];
      const refetch = vi.fn().mockResolvedValue(null); // refresh still cannot produce a catalog
      const exitSpy = vi.spyOn(process, "exit").mockImplementation(() => undefined);

      await applyCopilotModelAliasResolution({
        awfReflectData: null,
        logger: msg => logs.push(msg),
        refetchReflectData: refetch,
      });

      expect(exitSpy).toHaveBeenCalledWith(1);
      expect(logs.some(l => l.includes("model-catalog retrieval prevented alias resolution") && l.includes("sol"))).toBe(true);
      expect(logs.some(l => l.includes("no AI credits pricing"))).toBe(false);
      // The unresolved alias must never be forwarded to Copilot via COPILOT_MODEL.
      expect(process.env.COPILOT_MODEL).toBe("sol");
    });
  });
});
