import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

// Minimal mock for @actions/core used by github-script CJS modules (renderLogFromFile).
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  setSecret: vi.fn(),
};
global.core = mockCore;

const {
  detectErrors,
  isCAPIQuotaExceededError,
  isInvocationCapExceededError,
  isMaxCacheMissesExceededError,
  isAgenticEngineTimeout,
  isUnsupportedModelToolsError,
  isStepTimeout,
  INFERENCE_ACCESS_ERROR_PATTERN,
  MCP_POLICY_BLOCKED_PATTERN,
  AGENTIC_ENGINE_TIMEOUT_PATTERN,
  WATCHDOG_SIGTERM_PATTERN,
  STEP_TIMEOUT_SIGTERM_PATTERN,
  MODEL_NOT_SUPPORTED_PATTERN,
  HTTP_400_RESPONSE_ERROR_PATTERN,
  CAPI_QUOTA_EXCEEDED_PATTERN,
  INVOCATION_CAP_EXCEEDED_PATTERN,
  MAX_CACHE_MISSES_EXCEEDED_PATTERN,
  MISSING_MODEL_PRICING_PATTERN,
  SHELL_EXPANSION_GUARD_REJECTED_PATTERN,
  isShellExpansionGuardRejectedError,
  extractMissingModelPricingModelName,
  buildOutputLines,
  findMostRecentLogFile,
  renderInternalEngineLogOnFailure,
} = require("./detect_agent_errors.cjs");

describe("detect_agent_errors.cjs", () => {
  describe("INFERENCE_ACCESS_ERROR_PATTERN", () => {
    it("matches 'Access denied by policy settings'", () => {
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test("Access denied by policy settings")).toBe(true);
    });

    it("matches 'invalid access to inference'", () => {
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test("invalid access to inference")).toBe(true);
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some output\nError: Access denied by policy settings\nMore output";
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test(log)).toBe(true);
    });

    it("does not match unrelated errors", () => {
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test("MCP server connection failed")).toBe(false);
      expect(INFERENCE_ACCESS_ERROR_PATTERN.test("")).toBe(false);
    });
  });

  describe("MCP_POLICY_BLOCKED_PATTERN", () => {
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

  describe("AGENTIC_ENGINE_TIMEOUT_PATTERN", () => {
    it("matches copilot-harness process closed with SIGTERM", () => {
      const log = "[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s stdout=1234B stderr=567B hasOutput=true";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches copilot-harness process exit with SIGTERM", () => {
      const log = "[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process exit event exitCode=null signal=SIGTERM";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches SIGKILL from any engine", () => {
      const log = "process closed exitCode=1 signal=SIGKILL duration=20m 0s";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches SIGINT from any engine", () => {
      const log = "process closed exitCode=1 signal=SIGINT duration=20m 0s";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some agent output\n✓ All tests pass\n[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s\nMore output";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches signal from non-copilot engine logs", () => {
      const log = "Claude CLI terminated with signal=SIGTERM after timeout";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("matches SDK session.idle timeout signature", () => {
      const log = "[copilot-sdk-driver] [sdk-driver] error: Timeout after 870000ms waiting for session.idle";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(true);
    });

    it("does not match timeouts waiting for states other than session.idle", () => {
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("Timeout after 5000ms waiting for session.connected")).toBe(false);
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("Timeout after 5000ms waiting for session_idle")).toBe(false);
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("Timeout after 5000ms waiting for network response")).toBe(false);
    });

    it("does not match regular exit without signal", () => {
      const log = "[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process closed exitCode=1 duration=5m 3s stdout=1234B stderr=567B hasOutput=true";
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test(log)).toBe(false);
    });

    it("does not match unrelated errors", () => {
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("MCP server timeout")).toBe(false);
      expect(AGENTIC_ENGINE_TIMEOUT_PATTERN.test("")).toBe(false);
    });
  });

  describe("MODEL_NOT_SUPPORTED_PATTERN", () => {
    it("matches the exact error from the issue report", () => {
      const errorOutput = "Execution failed: CAPIError: 400 The requested model is not supported.";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(errorOutput)).toBe(true);
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some output\nExecution failed: CAPIError: 400 The requested model is not supported.\nMore output";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(log)).toBe(true);
    });

    it("matches invalid/unknown model name variants", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("invalid model name 'claude-sonnet-999'")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("unknown model gpt-unknown")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model 'gpt-foo' not found")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model 'claude-ultra' does not exist")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model claude-fake is not supported")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model gpt-unknown is not available")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model gemini-v99 is unavailable")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("model 'claude-3-5-sonnet@20241022' not found")).toBe(true);
    });

    it("matches the Copilot SDK driver policy-enablement error", () => {
      const errorOutput = "[copilot-sdk-driver] [sdk-driver] error: Execution failed: Error: No model available. Check policy enablement under GitHub Settings > Copilot";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(errorOutput)).toBe(true);
    });

    it("classifies the raw Codex custom-tools rejection from unsupported models", () => {
      const errorOutput = String.raw`{"type":"turn.failed","error":{"message":"{\n  \"error\": {\n    \"message\": \"Invalid value: 'custom'\",\n    \"type\": \"invalid_request_error\",\n    \"param\": \"tools\",\n    \"code\": \"unknown_parameter\"\n  }\n}"}}`;
      expect(isUnsupportedModelToolsError(errorOutput)).toBe(true);
      expect(detectErrors(errorOutput).modelNotSupportedError).toBe(true);
    });

    it("classifies the raw Codex rejection when the error fields are reordered", () => {
      const errorOutput = String.raw`{"type":"turn.failed","error":{"message":"{\"error\":{\"param\":\"tools\",\"message\":\"Invalid value: 'custom'\"}}"}}`;
      expect(isUnsupportedModelToolsError(errorOutput)).toBe(true);
      expect(detectErrors(errorOutput).modelNotSupportedError).toBe(true);
    });

    it("does not combine custom-value and tools fields from separate error objects", () => {
      const errorOutput = String.raw`{"type":"turn.failed","error":{"message":"{\"first\":{\"message\":\"Invalid value: 'custom'\"},\"second\":{\"param\":\"tools\"}}"}}`;
      expect(isUnsupportedModelToolsError(errorOutput)).toBe(false);
      expect(detectErrors(errorOutput).modelNotSupportedError).toBe(false);
    });

    it("does not match 'No model available' without the policy-enablement hint", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("No model available. Retrying shortly.")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("No model available\nCheck policy enablement under GitHub Settings > Copilot")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("warning: no model available yet, waiting")).toBe(false);
    });

    it("matches AIC api-proxy 404 standalone 'Model not found' shape", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("404 Not Found: Model not found")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("ResponseError: 404 Not Found: Model not found")).toBe(true);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("Error: 404 Model not found")).toBe(true);
    });

    it("matches 404 model-not-found when embedded in larger log output", () => {
      const log = "Some output\n[codex-harness] attempt 2: exitCode=1 isInvalidModelError=false\nError 404 Not Found: Model not found\nall 3 retries exhausted — giving up";
      expect(MODEL_NOT_SUPPORTED_PATTERN.test(log)).toBe(true);
    });

    it("does not match unrelated invalid/unknown model wording", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("Error: invalid model response format")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("Error: invalid model schema definition")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("unknown model behavior detected")).toBe(false);
    });

    it("does not match other CAPIError 400 errors", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("CAPIError: 400 400 Bad Request")).toBe(false);
    });

    it("does not match unrelated errors", () => {
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("Access denied by policy settings")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("MCP servers were blocked by policy: 'github'")).toBe(false);
      expect(MODEL_NOT_SUPPORTED_PATTERN.test("")).toBe(false);
    });
  });

  describe("CAPI_QUOTA_EXCEEDED_PATTERN / isCAPIQuotaExceededError", () => {
    it("matches the exact observed error message", () => {
      expect(CAPI_QUOTA_EXCEEDED_PATTERN.test("CAPIError: 429 429 quota exceeded")).toBe(true);
      expect(isCAPIQuotaExceededError("CAPIError: 429 429 quota exceeded")).toBe(true);
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some agent output\nExecution failed: CAPIError: 429 429 quota exceeded\nMore output";
      expect(isCAPIQuotaExceededError(log)).toBe(true);
    });

    it("matches with varying whitespace around status codes", () => {
      expect(isCAPIQuotaExceededError("CAPIError:429 429 quota exceeded")).toBe(true);
      expect(isCAPIQuotaExceededError("CAPIError:  429  429  quota exceeded")).toBe(true);
    });

    it("matches case-insensitively", () => {
      expect(isCAPIQuotaExceededError("CAPIError: 429 429 QUOTA EXCEEDED")).toBe(true);
    });

    it("matches Copilot/CAPI Too Many Requests output", () => {
      expect(isCAPIQuotaExceededError("CAPIError: 429 Too Many Requests")).toBe(true);
      expect(isCAPIQuotaExceededError("Last error: CAPIError: Too Many Requests")).toBe(true);
    });

    it("does not match CAPIError 400", () => {
      expect(isCAPIQuotaExceededError("CAPIError: 400 Bad Request")).toBe(false);
    });

    it("does not match unrelated errors", () => {
      expect(isCAPIQuotaExceededError("Access denied by policy settings")).toBe(false);
      expect(isCAPIQuotaExceededError("MCP servers were blocked by policy: 'github'")).toBe(false);
      expect(isCAPIQuotaExceededError("")).toBe(false);
    });

    it("CAPI_QUOTA_EXCEEDED_PATTERN does not match the invocation cap error (different 429 subtype)", () => {
      // "Maximum LLM invocations exceeded" is a distinct error from quota/rate-limit —
      // it should NOT match the CAPI quota pattern.
      expect(isCAPIQuotaExceededError("CAPIError: 429 Maximum LLM invocations exceeded (25/25)")).toBe(false);
    });

    it("matches the Copilot CLI's own retry-exhaustion message with no CAPIError: prefix (429)", () => {
      const message = "Failed to get response from the AI model; retried 5 times (total retry wait time: 380.35 seconds) " + "(Request-ID AC21:F5CEC:33A719:40DD88:6A83AA27) Last error: 429 Too Many Requests";
      expect(isCAPIQuotaExceededError(message)).toBe(true);
    });

    it("matches the Copilot CLI's own retry-exhaustion message for 5xx statuses (503)", () => {
      const message = "Failed to get response from the AI model; retried 5 times (total retry wait time: 300 seconds) Last error: 503 Service Unavailable";
      expect(isCAPIQuotaExceededError(message)).toBe(true);
    });

    it("does not match a 'Failed to get response' message without retry-exhaustion context", () => {
      expect(isCAPIQuotaExceededError("Failed to get response from the AI model due to a network error")).toBe(false);
    });
  });

  describe("INVOCATION_CAP_EXCEEDED_PATTERN / isInvocationCapExceededError", () => {
    it("matches the CAPI form: CAPIError 429 Maximum LLM invocations exceeded", () => {
      expect(INVOCATION_CAP_EXCEEDED_PATTERN.test("CAPIError: 429 Maximum LLM invocations exceeded (25/25)")).toBe(true);
      expect(isInvocationCapExceededError("CAPIError: 429 Maximum LLM invocations exceeded (25/25)")).toBe(true);
    });

    it("matches when embedded in larger log output", () => {
      const log = "Some agent output\nExecution failed: CAPIError: 429 Maximum LLM invocations exceeded (20/20)\nMore output";
      expect(isInvocationCapExceededError(log)).toBe(true);
    });

    it("matches the Anthropic JSON error type field", () => {
      const output = '{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}';
      expect(INVOCATION_CAP_EXCEEDED_PATTERN.test(output)).toBe(true);
      expect(isInvocationCapExceededError(output)).toBe(true);
    });

    it("matches the human-readable Anthropic message form", () => {
      expect(isInvocationCapExceededError("Failed to authenticate. API Error: 403 Maximum LLM invocations exceeded (20 / 20).")).toBe(true);
    });

    it("matches case-insensitively for the human-readable form", () => {
      expect(isInvocationCapExceededError("maximum llm invocations exceeded (25/25)")).toBe(true);
    });

    it("does not match unrelated errors", () => {
      expect(isInvocationCapExceededError("CAPIError: 429 429 quota exceeded")).toBe(false);
      expect(isInvocationCapExceededError("CAPIError: Too Many Requests")).toBe(false);
      expect(isInvocationCapExceededError("Access denied by policy settings")).toBe(false);
      expect(isInvocationCapExceededError("")).toBe(false);
    });
  });

  describe("HTTP_400_RESPONSE_ERROR_PATTERN", () => {
    it("matches the generic HTTP 400 Bad Request response shape", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("Response status code does not indicate success: 400 (Bad Request)")).toBe(true);
    });

    it("matches the HTTP 400 response shape without the (Bad Request) suffix", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("Response status code does not indicate success: 400")).toBe(true);
    });

    it("does not match unrelated 400 text", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("CAPIError: 400 Bad Request")).toBe(false);
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("Error: 400 Bad Request")).toBe(false);
    });

    it("matches the Copilot SDK 'no model endpoints available given user constraints' error", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("[copilot-sdk-driver] [sdk-driver] error: 400 400 400 no model endpoints available given user constraints")).toBe(true);
    });

    it("matches the no-model-endpoints error embedded in larger output", () => {
      const output = 'some prior output\n[copilot-sdk-driver] [sdk-driver] error: 400 400 400 no model endpoints available given user constraints\n{"type":"subagent.failed"}';
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test(output)).toBe(true);
    });

    it("does not match 'no model endpoints available' without a leading 400", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("no model endpoints available given user constraints")).toBe(false);
    });

    it("matches the stream_options: Extra inputs are not permitted Anthropic BYOK error", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("[copilot-sdk-driver] [sdk-driver] error: 400 400 400 stream_options: Extra inputs are not permitted")).toBe(true);
    });

    it("does not false-positive on unrelated messages mentioning stream_options", () => {
      expect(HTTP_400_RESPONSE_ERROR_PATTERN.test("Configuring stream_options for the request")).toBe(false);
    });
  });

  describe("MISSING_MODEL_PRICING_PATTERN / extractMissingModelPricingModelName", () => {
    it("matches the exact error from issue #48344 with claude-opus-5", () => {
      const msg = 'Model "claude-opus-5" has no AI credits pricing and no default pricing is configured.';
      expect(MISSING_MODEL_PRICING_PATTERN.test(msg)).toBe(true);
    });

    it("matches case-insensitively", () => {
      expect(MISSING_MODEL_PRICING_PATTERN.test('model "some-model" HAS NO AI CREDITS PRICING')).toBe(true);
    });

    it("matches when embedded in a larger log line", () => {
      const log = 'some prior output\nError: 400 Model "gpt-4.1" has no AI credits pricing and no default pricing is configured.\nmore output';
      expect(MISSING_MODEL_PRICING_PATTERN.test(log)).toBe(true);
    });

    it("does not match unrelated pricing messages", () => {
      expect(MISSING_MODEL_PRICING_PATTERN.test("AI credits pricing updated for model")).toBe(false);
      expect(MISSING_MODEL_PRICING_PATTERN.test("no default pricing is configured")).toBe(false);
    });

    it("extractMissingModelPricingModelName returns model name from matching log", () => {
      const log = 'Error: 400 Model "claude-opus-5" has no AI credits pricing and no default pricing is configured.';
      expect(extractMissingModelPricingModelName(log)).toBe("claude-opus-5");
    });

    it("extractMissingModelPricingModelName returns empty string for non-matching log", () => {
      expect(extractMissingModelPricingModelName("Some unrelated error")).toBe("");
    });

    it("extractMissingModelPricingModelName handles model names with dots", () => {
      const log = 'Model "gpt-4.1-mini" has no AI credits pricing';
      expect(extractMissingModelPricingModelName(log)).toBe("gpt-4.1-mini");
    });
  });

  describe("detectErrors", () => {
    it("returns all false for empty log", () => {
      const result = detectErrors("");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
      expect(result.maxCacheMissesExceeded).toBe(false);
      expect(result.missingModelPricingError).toBe(false);
      expect(result.missingModelPricingModelName).toBe("");
      expect(result.shellExpansionGuardRejected).toBe(false);
    });

    it("detects inference access error only", () => {
      const result = detectErrors("Error: Access denied by policy settings");
      expect(result.inferenceAccessError).toBe(true);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects MCP policy error only", () => {
      const result = detectErrors("! 2 MCP servers were blocked by policy: 'github', 'safeoutputs'");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(true);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects engine timeout only", () => {
      const result = detectErrors("[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(true);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects model not supported error only", () => {
      const result = detectErrors("Execution failed: CAPIError: 400 The requested model is not supported.");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(true);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects invalid model name errors", () => {
      const result = detectErrors("Error: invalid model name 'claude-sonnet-999'");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(true);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects CAPI quota exceeded error only", () => {
      const result = detectErrors("Execution failed: CAPIError: 429 429 quota exceeded");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(true);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects invocation cap exceeded error only (CAPI form)", () => {
      const result = detectErrors("Execution failed: CAPIError: 429 Maximum LLM invocations exceeded (25/25)");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(true);
    });

    it("detects invocation cap exceeded error only (Anthropic JSON form)", () => {
      const result = detectErrors('{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}');
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(true);
    });

    it("detects shell expansion guard rejection only (issue github/gh-aw#52254 payload shape)", () => {
      const log = [
        "[copilot-harness] attempt 1: shell(safeoutputs create_discussion --title 'MCP toolset unavailable' --body \"...\\n...\")",
        "Command rejected: shell command contains dangerous patterns (command substitution, indirect expansion, or nested command substitution) that could enable arbitrary code execution. Please rewrite the command without these expansion patterns.",
        "[copilot-harness] attempt 2: retrying identical command",
        "Command rejected: shell command contains dangerous patterns (command substitution, indirect expansion, or nested command substitution) that could enable arbitrary code execution. Please rewrite the command without these expansion patterns.",
        "##[error]The action 'Execute GitHub Copilot CLI' has timed out after 5 minutes.",
      ].join("\n");
      const result = detectErrors(log);
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.shellExpansionGuardRejected).toBe(true);
    });

    it("detects both capi quota and invocation-cap flags when both signatures are present", () => {
      const result = detectErrors("CAPIError: Too Many Requests\nCAPIError: 429 Maximum LLM invocations exceeded (25/25)");
      expect(result.capiQuotaExceededError).toBe(true);
      expect(result.invocationCapExceeded).toBe(true);
    });

    it("detects HTTP 400 response error only", () => {
      const result = detectErrors("Response status code does not indicate success: 400 (Bad Request)");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(true);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
      expect(result.missingModelPricingError).toBe(false);
    });

    it("detects both errors in the same log", () => {
      const log = "Access denied by policy settings\nMCP servers were blocked by policy: 'github'";
      const result = detectErrors(log);
      expect(result.inferenceAccessError).toBe(true);
      expect(result.mcpPolicyError).toBe(true);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects timeout alongside other errors", () => {
      const log = "Access denied by policy settings\n[copilot-harness] 2026-04-12T04:56:28.000Z attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s";
      const result = detectErrors(log);
      expect(result.inferenceAccessError).toBe(true);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(true);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects SDK session.idle timeout", () => {
      const result = detectErrors("[copilot-sdk-driver] [sdk-driver] error: Timeout after 870000ms waiting for session.idle");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(true);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects SDK session.idle timeout alongside other errors", () => {
      const log = "Access denied by policy settings\n[sdk-driver] info: retrying request\n[copilot-sdk-driver] error: Timeout after 870000ms waiting for session.idle";
      const result = detectErrors(log);
      expect(result.inferenceAccessError).toBe(true);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(true);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("returns false for unrelated log content", () => {
      const result = detectErrors("CAPIError: 400 Bad Request\nSome normal output");
      expect(result.inferenceAccessError).toBe(false);
      expect(result.mcpPolicyError).toBe(false);
      expect(result.agenticEngineTimeout).toBe(false);
      expect(result.modelNotSupportedError).toBe(false);
      expect(result.http400ResponseError).toBe(false);
      expect(result.capiQuotaExceededError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
      expect(result.maxCacheMissesExceeded).toBe(false);
      expect(result.missingModelPricingError).toBe(false);
    });

    it("detects missing model pricing error from agent-stdio.log message", () => {
      const log = 'Error: 400 Model "claude-opus-5" has no AI credits pricing and no default pricing is configured. Set apiProxy.defaultAiCreditsPricing in the AWF config.';
      const result = detectErrors(log);
      expect(result.missingModelPricingError).toBe(true);
      expect(result.missingModelPricingModelName).toBe("claude-opus-5");
      expect(result.http400ResponseError).toBe(false);
    });

    it("detects missing model pricing with model name containing dots", () => {
      const log = 'Model "gpt-4.1-mini" has no AI credits pricing';
      const result = detectErrors(log);
      expect(result.missingModelPricingError).toBe(true);
      expect(result.missingModelPricingModelName).toBe("gpt-4.1-mini");
    });

    it("normalizes multiline model names to a single line", () => {
      const log = `Model "claude-opus-5
commentary" has no AI credits pricing`;
      const result = detectErrors(log);
      expect(result.missingModelPricingError).toBe(true);
      expect(result.missingModelPricingModelName).toBe("claude-opus-5 commentary");
    });

    it("does not false-positive on unrelated AI credits error messages", () => {
      const result = detectErrors("Maximum AI credits exceeded for this run");
      expect(result.missingModelPricingError).toBe(false);
      expect(result.missingModelPricingModelName).toBe("");
    });

    it("does not report engine timeout when post-result watchdog fired SIGTERM (watchdogFired=true)", () => {
      // Mirrors the actual failing run: watchdog terminated idle process, not a step timeout
      const log = [
        "[copilot-harness] attempt 1: post-result watchdog terminating idle process after 20736ms (SIGTERM)",
        "[copilot-harness] attempt 1: process exit event exitCode=1 signal=SIGTERM",
        "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=12m 38s stdout=0B stderr=431678B hasOutput=true watchdogFired=true",
        "[copilot-harness] attempt 1 failed: exitCode=1 failureClass=authentication_failed",
      ].join("\n");
      const result = detectErrors(log);
      expect(result.agenticEngineTimeout).toBe(false);
    });

    it("reports engine timeout when SIGTERM is from step timeout (watchdogFired=false)", () => {
      const log = [
        "[copilot-harness] attempt 1: process exit event exitCode=1 signal=SIGTERM",
        "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s stdout=1234B stderr=567B hasOutput=true watchdogFired=false",
      ].join("\n");
      const result = detectErrors(log);
      expect(result.agenticEngineTimeout).toBe(true);
    });

    it("reports engine timeout when SIGTERM is from step timeout (no watchdogFired field)", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s stdout=1234B stderr=567B hasOutput=true";
      const result = detectErrors(log);
      expect(result.agenticEngineTimeout).toBe(true);
    });

    it("reports engine timeout when both watchdog and step-timeout SIGTERMs are present", () => {
      const log = [
        "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=10m 0s hasOutput=true watchdogFired=true",
        "[copilot-harness] attempt 2: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true watchdogFired=false",
      ].join("\n");
      const result = detectErrors(log);
      expect(result.agenticEngineTimeout).toBe(true);
    });

    it("detects max cache misses exceeded (JSON error type form)", () => {
      const result = detectErrors('{"error":{"type":"max_cache_misses_exceeded","message":"Maximum consecutive cache misses exceeded (6 / 5).","consecutive_cache_misses":6,"max_cache_misses":5}}');
      expect(result.maxCacheMissesExceeded).toBe(true);
      expect(result.inferenceAccessError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects max cache misses exceeded (human-readable message form)", () => {
      const result = detectErrors("Maximum consecutive cache misses exceeded");
      expect(result.maxCacheMissesExceeded).toBe(true);
      expect(result.inferenceAccessError).toBe(false);
      expect(result.invocationCapExceeded).toBe(false);
    });

    it("detects max cache misses exceeded in a production log line", () => {
      const log = '2026-07-30T06:14:50.000Z [ERROR] Error in API request: 403 {"error":{"type":"max_cache_misses_exceeded","message":"Maximum consecutive cache misses exceeded (6 / 5).","consecutive_cache_misses":6,"max_cache_misses":5}}';
      const result = detectErrors(log);
      expect(result.maxCacheMissesExceeded).toBe(true);
    });

    it("does not false-positive on unrelated cache miss content", () => {
      const result = detectErrors("Cache miss for key: model-output-xyz");
      expect(result.maxCacheMissesExceeded).toBe(false);
    });
  });

  describe("MAX_CACHE_MISSES_EXCEEDED_PATTERN", () => {
    it("matches max_cache_misses_exceeded error type", () => {
      expect(MAX_CACHE_MISSES_EXCEEDED_PATTERN.test('{"type":"max_cache_misses_exceeded"}')).toBe(true);
    });

    it("matches Maximum consecutive cache misses exceeded message", () => {
      expect(MAX_CACHE_MISSES_EXCEEDED_PATTERN.test("Maximum consecutive cache misses exceeded")).toBe(true);
    });

    it("is case-insensitive", () => {
      expect(MAX_CACHE_MISSES_EXCEEDED_PATTERN.test("MAXIMUM CONSECUTIVE CACHE MISSES EXCEEDED")).toBe(true);
    });

    it("does not match unrelated cache miss content", () => {
      expect(MAX_CACHE_MISSES_EXCEEDED_PATTERN.test("Cache miss for key: output")).toBe(false);
    });
  });

  describe("isMaxCacheMissesExceededError", () => {
    it("returns false for empty input", () => {
      expect(isMaxCacheMissesExceededError("")).toBe(false);
    });

    it("detects max_cache_misses_exceeded JSON error type", () => {
      expect(isMaxCacheMissesExceededError('{"error":{"type":"max_cache_misses_exceeded"}}')).toBe(true);
    });

    it("detects human-readable message form", () => {
      expect(isMaxCacheMissesExceededError("Maximum consecutive cache misses exceeded")).toBe(true);
    });

    it("returns false for unrelated content", () => {
      expect(isMaxCacheMissesExceededError("Some unrelated error message")).toBe(false);
    });
  });

  describe("SHELL_EXPANSION_GUARD_REJECTED_PATTERN / isShellExpansionGuardRejectedError", () => {
    // Exact payload shape from the issue report (github/gh-aw#52254): the sandbox's shell
    // command-injection guard rejected a benign multi-line `printf` call to `safeoutputs
    // create_discussion` for containing bash expansion patterns.
    const ISSUE_REJECTION_MESSAGE =
      "Command rejected: shell command contains dangerous patterns (command substitution, " +
      "indirect expansion, or nested command substitution) that could enable arbitrary code " +
      "execution. Please rewrite the command without these expansion patterns.";

    it("matches the exact rejection message from the issue report", () => {
      expect(SHELL_EXPANSION_GUARD_REJECTED_PATTERN.test(ISSUE_REJECTION_MESSAGE)).toBe(true);
      expect(isShellExpansionGuardRejectedError(ISSUE_REJECTION_MESSAGE)).toBe(true);
    });

    it("matches when embedded in larger multi-line log output", () => {
      const log = [
        "[copilot-harness] attempt 1: invoking shell(safeoutputs create_discussion --title ... --body ...)",
        ISSUE_REJECTION_MESSAGE,
        "[copilot-harness] attempt 2: retrying identical command",
        ISSUE_REJECTION_MESSAGE,
        "##[error]The action 'Execute GitHub Copilot CLI' has timed out after 5 minutes.",
      ].join("\n");
      expect(isShellExpansionGuardRejectedError(log)).toBe(true);
    });

    it("is case-insensitive", () => {
      expect(SHELL_EXPANSION_GUARD_REJECTED_PATTERN.test(ISSUE_REJECTION_MESSAGE.toUpperCase())).toBe(true);
    });

    it("matches when the two anchor phrases are split across a line break", () => {
      const wrapped = "Command rejected: ...that could enable arbitrary code execution.\nPlease rewrite the command without these expansion patterns.";
      expect(isShellExpansionGuardRejectedError(wrapped)).toBe(true);
    });

    it("does not match unrelated shell errors", () => {
      expect(isShellExpansionGuardRejectedError("bash: safeoutputs: command not found")).toBe(false);
      expect(isShellExpansionGuardRejectedError("permission denied by workflow tool permissions")).toBe(false);
      expect(isShellExpansionGuardRejectedError("")).toBe(false);
    });

    it("does not match arbitrary code execution mentions without the rewrite guidance", () => {
      expect(isShellExpansionGuardRejectedError("This could enable arbitrary code execution if left unchecked.")).toBe(false);
    });
  });

  describe("WATCHDOG_SIGTERM_PATTERN", () => {
    it("matches a process closed line with SIGTERM and watchdogFired=true", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=12m 38s hasOutput=true watchdogFired=true";
      expect(WATCHDOG_SIGTERM_PATTERN.test(log)).toBe(true);
    });

    it("does not match a process closed line with SIGTERM and watchdogFired=false", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true watchdogFired=false";
      expect(WATCHDOG_SIGTERM_PATTERN.test(log)).toBe(false);
    });

    it("does not match a process closed line with SIGTERM and no watchdogFired field", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true";
      expect(WATCHDOG_SIGTERM_PATTERN.test(log)).toBe(false);
    });

    it("does not match a process exit event line (not process closed)", () => {
      const log = "[copilot-harness] attempt 1: process exit event exitCode=1 signal=SIGTERM";
      expect(WATCHDOG_SIGTERM_PATTERN.test(log)).toBe(false);
    });
  });

  describe("STEP_TIMEOUT_SIGTERM_PATTERN", () => {
    it("matches a process closed line with SIGTERM and no watchdogFired field", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s hasOutput=true";
      expect(STEP_TIMEOUT_SIGTERM_PATTERN.test(log)).toBe(true);
    });

    it("matches a process closed line with SIGTERM and watchdogFired=false", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true watchdogFired=false";
      expect(STEP_TIMEOUT_SIGTERM_PATTERN.test(log)).toBe(true);
    });

    it("does not match a process closed line with SIGTERM and watchdogFired=true", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=12m 38s hasOutput=true watchdogFired=true";
      expect(STEP_TIMEOUT_SIGTERM_PATTERN.test(log)).toBe(false);
    });
  });

  describe("isAgenticEngineTimeout", () => {
    it("returns true for SDK session.idle timeout", () => {
      expect(isAgenticEngineTimeout("[sdk-driver] error: Timeout after 870000ms waiting for session.idle")).toBe(true);
    });

    it("returns true for step-timeout SIGTERM (no watchdogFired field)", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 12s hasOutput=true";
      expect(isAgenticEngineTimeout(log)).toBe(true);
    });

    it("returns true for step-timeout SIGTERM (watchdogFired=false)", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true watchdogFired=false";
      expect(isAgenticEngineTimeout(log)).toBe(true);
    });

    it("returns false for SIGTERM in a non-process-closed context", () => {
      expect(isAgenticEngineTimeout("Claude CLI terminated with signal=SIGTERM after timeout")).toBe(false);
    });

    it("returns true for SIGKILL", () => {
      const log = "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGKILL duration=20m 0s hasOutput=true";
      expect(isAgenticEngineTimeout(log)).toBe(true);
    });

    it("returns false for post-result watchdog SIGTERM (watchdogFired=true)", () => {
      // Mirrors the actual failing run: watchdog terminated idle process after authentication failure
      const log = [
        "[copilot-harness] attempt 1: post-result watchdog terminating idle process after 20736ms (SIGTERM)",
        "[copilot-harness] attempt 1: process exit event exitCode=1 signal=SIGTERM",
        "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=12m 38s stdout=0B stderr=431678B hasOutput=true watchdogFired=true",
        "[copilot-harness] attempt 1 failed: exitCode=1 failureClass=authentication_failed",
      ].join("\n");
      expect(isAgenticEngineTimeout(log)).toBe(false);
    });

    it("returns true when both a watchdog SIGTERM and a step-timeout SIGTERM are present", () => {
      // If there's a watchdog AND a non-watchdog "process closed" SIGTERM, it's still a timeout
      const log = [
        "[copilot-harness] attempt 1: process closed exitCode=1 signal=SIGTERM duration=10m 0s hasOutput=true watchdogFired=true",
        "[copilot-harness] attempt 2: process closed exitCode=1 signal=SIGTERM duration=20m 0s hasOutput=true watchdogFired=false",
      ].join("\n");
      expect(isAgenticEngineTimeout(log)).toBe(true);
    });

    it("returns false for empty log", () => {
      expect(isAgenticEngineTimeout("")).toBe(false);
    });

    it("returns false for unrelated content", () => {
      expect(isAgenticEngineTimeout("CAPIError: 400 Bad Request")).toBe(false);
      expect(isAgenticEngineTimeout("MCP server timeout")).toBe(false);
    });
  });

  describe("isStepTimeout", () => {
    const timeoutMinutes = "15";
    const startMs = 1_000_000;

    it("detects a step killed after reaching its timeout-minutes budget", () => {
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes, startMs, nowMs: startMs + 15 * 60_000 })).toBe(true);
    });

    it("tolerates the small delay between step start and the start timestamp write", () => {
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes, startMs, nowMs: startMs + 15 * 60_000 - 10_000 })).toBe(true);
    });

    it("returns false when the engine failed well before the timeout budget", () => {
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes, startMs, nowMs: startMs + 5 * 60_000 })).toBe(false);
    });

    it("returns false when the engine step succeeded", () => {
      expect(isStepTimeout({ outcome: "success", timeoutMinutes, startMs, nowMs: startMs + 20 * 60_000 })).toBe(false);
    });

    it("returns false when the outcome, timeout or start timestamp is unavailable", () => {
      expect(isStepTimeout({ outcome: "", timeoutMinutes, startMs, nowMs: startMs + 20 * 60_000 })).toBe(false);
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes: "", startMs, nowMs: startMs + 20 * 60_000 })).toBe(false);
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes: "${{ inputs.timeout }}", startMs, nowMs: startMs + 20 * 60_000 })).toBe(false);
      expect(isStepTimeout({ outcome: "failure", timeoutMinutes, startMs: NaN, nowMs: startMs + 20 * 60_000 })).toBe(false);
    });
  });

  describe("buildOutputLines", () => {
    it("suppresses generic capi_quota_exceeded_error when invocation cap is present", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: true,
        invocationCapExceeded: true,
      });

      expect(lines).toContain("capi_quota_exceeded_error=false");
      expect(lines).toContain("invocation_cap_exceeded=true");
    });

    it("emits missing_model_pricing_error and missing_model_pricing_model_name outputs", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: false,
        invocationCapExceeded: false,
        maxCacheMissesExceeded: false,
        missingModelPricingError: true,
        missingModelPricingModelName: "claude-opus-5",
      });

      expect(lines).toContain("missing_model_pricing_error=true");
      expect(lines).toContain("missing_model_pricing_model_name=claude-opus-5");
    });

    it("emits missing_model_pricing_error=false and empty model name when not detected", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: false,
        invocationCapExceeded: false,
        maxCacheMissesExceeded: false,
        missingModelPricingError: false,
        missingModelPricingModelName: "",
      });

      expect(lines).toContain("missing_model_pricing_error=false");
      expect(lines).toContain("missing_model_pricing_model_name=");
    });

    it("emits max_cache_misses_exceeded=true when detected", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: false,
        invocationCapExceeded: false,
        maxCacheMissesExceeded: true,
        missingModelPricingError: false,
        missingModelPricingModelName: "",
      });

      expect(lines).toContain("max_cache_misses_exceeded=true");
    });

    it("emits max_cache_misses_exceeded=false when not detected", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: false,
        invocationCapExceeded: false,
        maxCacheMissesExceeded: false,
        missingModelPricingError: false,
        missingModelPricingModelName: "",
      });

      expect(lines).toContain("max_cache_misses_exceeded=false");
    });

    it("emits shell_expansion_guard_rejected=true when detected", () => {
      const lines = buildOutputLines({
        inferenceAccessError: false,
        mcpPolicyError: false,
        agenticEngineTimeout: false,
        modelNotSupportedError: false,
        http400ResponseError: false,
        capiQuotaExceededError: false,
        invocationCapExceeded: false,
        maxCacheMissesExceeded: false,
        missingModelPricingError: false,
        missingModelPricingModelName: "",
        shellExpansionGuardRejected: true,
      });

      expect(lines).toContain("shell_expansion_guard_rejected=true");
    });
  });
});

describe("renderInternalEngineLogOnFailure() / findMostRecentLogFile()", () => {
  let tempDir;
  let logsDir;
  let originalOutcome;
  let originalInternalLogsDir;

  // Patch process.stdout.write so we can capture workflow-command output emitted by
  // renderLogFromFile/renderLogToStdout.
  const originalWrite = process.stdout.write.bind(process.stdout);
  let stdoutChunks = [];
  const stubbedWrite = chunk => {
    stdoutChunks.push(typeof chunk === "string" ? chunk : chunk.toString("utf8"));
    return true;
  };

  function capturedStdout() {
    return stdoutChunks.join("");
  }

  beforeEach(() => {
    vi.clearAllMocks();
    stdoutChunks = [];
    process.stdout.write = stubbedWrite;

    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "detect-agent-errors-internal-log-test-"));
    logsDir = path.join(tempDir, "logs");
    fs.mkdirSync(logsDir, { recursive: true });

    originalOutcome = process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME;
    originalInternalLogsDir = process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR;
  });

  afterEach(() => {
    process.stdout.write = originalWrite;
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
    if (originalOutcome === undefined) {
      delete process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME;
    } else {
      process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME = originalOutcome;
    }
    if (originalInternalLogsDir === undefined) {
      delete process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR;
    } else {
      process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR = originalInternalLogsDir;
    }
  });

  describe("findMostRecentLogFile()", () => {
    it("returns undefined when the directory does not exist", () => {
      expect(findMostRecentLogFile(path.join(tempDir, "missing"))).toBeUndefined();
    });

    it("returns undefined when no *.log files are present", () => {
      fs.writeFileSync(path.join(logsDir, "notes.txt"), "hi", "utf8");
      expect(findMostRecentLogFile(logsDir)).toBeUndefined();
    });

    it("finds a *.log file nested in subdirectories", () => {
      const nested = path.join(logsDir, "session-1");
      fs.mkdirSync(nested, { recursive: true });
      const nestedLog = path.join(nested, "codex.log");
      fs.writeFileSync(nestedLog, "nested log content\n", "utf8");

      expect(findMostRecentLogFile(logsDir)).toBe(nestedLog);
    });

    it("picks the most recently modified *.log file", async () => {
      const older = path.join(logsDir, "older.log");
      const newer = path.join(logsDir, "newer.log");
      fs.writeFileSync(older, "older\n", "utf8");
      await new Promise(resolve => setTimeout(resolve, 10));
      fs.writeFileSync(newer, "newer\n", "utf8");
      fs.utimesSync(older, new Date(Date.now() - 60_000), new Date(Date.now() - 60_000));

      expect(findMostRecentLogFile(logsDir)).toBe(newer);
    });
  });

  describe("renderInternalEngineLogOnFailure()", () => {
    it("is a no-op when GH_AW_ENGINE_INTERNAL_LOGS_DIR is not set", async () => {
      delete process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR;
      process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME = "failure";
      fs.writeFileSync(path.join(logsDir, "codex.log"), "should not be rendered\n", "utf8");

      await renderInternalEngineLogOnFailure();
      expect(capturedStdout()).toBe("");
    });

    it("is a no-op when the execution outcome was not 'failure'", async () => {
      process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR = logsDir;
      process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME = "success";
      fs.writeFileSync(path.join(logsDir, "codex.log"), "should not be rendered\n", "utf8");

      await renderInternalEngineLogOnFailure();
      expect(capturedStdout()).toBe("");
    });

    it("is a no-op when no log files exist under the internal logs directory", async () => {
      process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR = logsDir;
      process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME = "failure";

      await renderInternalEngineLogOnFailure();
      expect(capturedStdout()).toBe("");
    });

    it("renders the last 200 lines of the most recent log wrapped in group + stop-commands macros on failure", async () => {
      process.env.GH_AW_ENGINE_INTERNAL_LOGS_DIR = logsDir;
      process.env.GH_AW_AGENTIC_EXECUTION_OUTCOME = "failure";
      const lines = Array.from({ length: 250 }, (_, index) => `codex line ${String(index + 1).padStart(3, "0")}`);
      fs.writeFileSync(path.join(logsDir, "codex.log"), lines.join("\n") + "\n", "utf8");

      await renderInternalEngineLogOnFailure();

      const out = capturedStdout();
      expect(out).toMatch(/^::group::Engine internal logs \(/);
      expect(out).not.toContain("codex line 050");
      expect(out).toContain("codex line 051");
      expect(out).toContain("codex line 250");
      const stopMatch = out.match(/::stop-commands::(render-[a-f0-9]+)\n/);
      expect(stopMatch).not.toBeNull();
      expect(out).toContain("::" + stopMatch[1] + "::\n");
      expect(out).toContain("::endgroup::\n");
    });

    it("runs directly under Node and redacts credentials", () => {
      const fakeToken = "ghp_" + "A".repeat(36);
      fs.writeFileSync(path.join(logsDir, "codex.log"), `token=${fakeToken}\n`, "utf8");

      const result = spawnSync(process.execPath, [path.join(import.meta.dirname, "detect_agent_errors.cjs")], {
        encoding: "utf8",
        env: {
          ...process.env,
          GH_AW_AGENTIC_EXECUTION_OUTCOME: "failure",
          GH_AW_ENGINE_INTERNAL_LOGS_DIR: logsDir,
        },
      });

      expect(result.status).toBe(0);
      expect(result.stdout).toContain("***REDACTED***");
      expect(result.stdout).not.toContain(fakeToken);
      expect(result.stderr).not.toContain("ReferenceError");
    });
  });
});
