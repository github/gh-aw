// @ts-check
import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { parseCopilotRetryAfterMs } = require("./copilot_retry_after.cjs");
const { normalizeHarnessRetryRecord } = require("./harness_retry_record.cjs");
const harnessPath = require.resolve("./copilot_harness.cjs");
const now = Date.parse("Wed, 11 Nov 2026 08:43:00 GMT");

describe("Copilot Retry-After parsing", () => {
  it.each([
    ["HTTP 429\nretry-after: 245", 245000],
    ['CAPIError: 429 Too Many Requests\nheaders: { "Retry-After": "245" }', 245000],
    ["HTTP 429\n{ 'retry-after': '245', 'x-ratelimit-exceeded': 'global-chat:model:userID' }", 245000],
    ['{"status":429,"headers":{"retry-after":245}}', 245000],
    [JSON.stringify({ type: "session.error", data: { message: 'HTTP 429\nheaders: {"retry-after":"245"}' } }), 245000],
    ["HTTP 429\nRetry-After: Wed, 11 Nov 2026 08:47:05 GMT", 245000],
    ['HTTP 429\n{"Retry-After":"Wed, 11 Nov 2026 08:47:05 GMT"}', 245000],
    [JSON.stringify({ type: "session.error", data: { message: "HTTP 429\nRetry-After: Wed, 11 Nov 2026 08:47:05 GMT" } }), 245000],
    ["HTTP 429\nRetry-After: 10\nRetry-After: 245", 245000],
    ["HTTP 429\nRetry-After: 999999999999999999999999", Number.MAX_SAFE_INTEGER],
  ])("parses %s", (text, expected) => {
    expect(parseCopilotRetryAfterMs(text, now)).toBe(expected);
  });

  it.each([
    "HTTP 503\nretry-after: 245",
    "HTTP 429",
    "HTTP 429\nx-retry-after: 245",
    "HTTP 429\nretry-after: invalid",
    "HTTP 429\nretry-after: -1",
    "HTTP 429\nretry-after: 0",
    "HTTP 429\nretry-after: 1.5",
    "HTTP 429\nretry-after: 1e3",
    "HTTP 429\nretry-after: 245 seconds",
    "HTTP 429\nretry-after: Wed, 11 Nov 2026 08:42:00 GMT",
  ])("preserves existing policy for %s", text => {
    expect(parseCopilotRetryAfterMs(text, now)).toBeNull();
  });
});

describe("Copilot Retry-After harness integration", () => {
  it.each([
    ["stderr", "1", "2000", "", 0, 2, "rate_limit_error"],
    ["session.error", "1", "2000", "", 0, 2, "rate_limit_error"],
    ["stderr:session.error", "1", "2000", "", 0, 2, "rate_limit_error"],
    ["error", "1", "1000", "", 0, 2, "rate_limit_error"],
    ["turn.failed", "245", "60000", "", 1, 1, "rate_limit_wait_exceeded"],
    ["stderr", "245", "60000", "", 1, 1, "rate_limit_wait_exceeded"],
    ["stderr", "245", "300000", "2", 1, 1, "rate_limit_wait_exceeded"],
    ["stderr", "2147484", "3000000000", "", 1, 1, "rate_limit_wait_exceeded"],
    ["stderr", "999999999999999999999999", "60000", "", 1, 1, "rate_limit_wait_exceeded"],
    ["stderr", "invalid", "2000", "", 1, 1, "capi_quota_exceeded"],
    ["stderr", "", "2000", "", 1, 1, "capi_quota_exceeded"],
    ["assistant.message", "245", "1", "", 0, 2, "partial_execution"],
    ["tool.execution_complete", "245", "1", "", 0, 2, "partial_execution"],
    ["stderr:assistant.message", "245", "1", "", 0, 2, "partial_execution"],
    ["stderr:tool.execution_complete", "245", "1", "", 0, 2, "partial_execution"],
    ["invocation-cap", "245", "300000", "", 1, 1, "invocation_cap_exceeded"],
  ])("%s Retry-After=%s with cap=%s and timeout=%s", (source, retryAfter, maxDelay, timeout, exitCode, attempts, failureClass) => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-retry-after-"));
    const callsPath = path.join(directory, "calls.jsonl");
    const stubPath = path.join(directory, "stub.cjs");
    const eventType = source.replace(/^stderr:/, "");
    const error = source === "invocation-cap" ? "CAPIError: 429 Maximum LLM invocations exceeded (20/20)" : ["assistant.message", "tool.execution_complete"].includes(eventType) ? "HTTP 429" : "CAPIError: 429 Too Many Requests";
    const message = `${error}${retryAfter ? `\nretry-after: ${retryAfter}` : ""}`;
    fs.writeFileSync(
      stubPath,
      `const fs = require("fs");
const callsPath = ${JSON.stringify(callsPath)};
fs.appendFileSync(callsPath, JSON.stringify({ time: Date.now(), args: process.argv.slice(2) }) + "\\n");
if (fs.readFileSync(callsPath, "utf8").trim().split("\\n").length === 1) {
  console.${source.startsWith("stderr") || source === "invocation-cap" ? "error" : "log"}(${["stderr", "invocation-cap"].includes(source) ? JSON.stringify(message) : `JSON.stringify({ type: ${JSON.stringify(eventType)}, data: { message: ${JSON.stringify(message)} } })`});
  process.exitCode = 1;
}`
    );
    try {
      const result = spawnSync(process.execPath, [harnessPath, process.execPath, stubPath, "--prompt", "test"], {
        cwd: directory,
        env: {
          ...process.env,
          GITHUB_WORKSPACE: directory,
          GH_AW_TMP_DIR: directory,
          GH_AW_HARNESS_TELEMETRY_DIR: directory,
          GH_AW_HARNESS_MAX_RETRIES: "1",
          GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
          GH_AW_HARNESS_MAX_DELAY_MS: maxDelay,
          GH_AW_TIMEOUT_MINUTES: timeout,
          GH_AW_SAFE_OUTPUTS: "",
          GH_AW_SKIP_REFLECT: "true",
          AWF_REFLECT_ENABLED: "",
          COPILOT_MODEL: "",
          GH_AW_COPILOT_SDK: "",
          GH_AW_SKIP_IF_DECISION: "",
        },
        encoding: "utf8",
        timeout: 10000,
      });
      expect(result.status, result.stderr).toBe(exitCode);
      const calls = fs
        .readFileSync(callsPath, "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(calls).toHaveLength(attempts);
      const telemetry = JSON.parse(fs.readFileSync(path.join(directory, "harness_retry.json"), "utf8"));
      expect(telemetry.attempt_results[0].failure_classes).toEqual([failureClass]);
      expect(normalizeHarnessRetryRecord(telemetry)?.attemptResults[0].failureClasses).toEqual([failureClass]);
      if (failureClass === "rate_limit_error") {
        expect(calls[1].time - calls[0].time).toBeGreaterThanOrEqual(1000);
        expect(telemetry.attempt_results[1].backoff_ms).toBe(1000);
        expect(calls[1].args).toContain("--continue");
      }
      if (failureClass === "rate_limit_wait_exceeded") {
        expect(result.stderr).toContain("not retrying");
        expect(result.stderr).not.toContain("sleeping");
      }
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
