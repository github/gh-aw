// @ts-check

import { describe, it, expect, vi } from "vitest";
import fs, { readFileSync } from "node:fs";
import path from "node:path";
import { parseDeepSeekLog, isDeepSeekLog } from "./parse_deepseek_log.cjs";
import { startupFailure, timedOut } from "./fixtures/deepseek_ci_logs.cjs";
import { projectSessionResult } from "./agent_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";
import { collectUnifiedSession, parseEngineSession, writeUnifiedSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { persistAgentExecution, detectErrors } from "./detect_agent_errors.cjs";
import { validateSession } from "./scripts/validate_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";

// Verified against agent artifact 9781648195 from existing successful Actions
// run https://github.com/github/gh-aw/actions/runs/33456355349.
const fixture = readFileSync(new URL("./test_data/deepseek_headless_stdout.log", import.meta.url), "utf8");
const answer = fixture.slice(fixture.indexOf("Perfect!"), fixture.indexOf("[INFO] Stopping containers...")).trim();

describe("parseDeepSeekLog", () => {
  it("preserves the actual 577-character final answer and its build failure explanation", () => {
    const result = parseDeepSeekLog(fixture);

    expect(isDeepSeekLog(fixture)).toBe(true);
    expect(answer).toHaveLength(577);
    expect(result.logEntries).toEqual([
      { type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "claude-sonnet-4.5" } },
      { type: "assistant.message", data: { content: answer } },
    ]);
    expect(result.markdown).toContain("Perfect! All smoke tests completed.");
    expect(result.markdown).toContain("Go toolchain download restriction from the proxy.");
    expect(result.markdown).toContain("## Smoke Test Results: ❌ FAIL");
    expect(result.mcpFailures).toEqual([]);
    expect(result.maxTurnsHit).toBe(false);
  });

  it("omits the user prompt, credentials, and infrastructure outside the answer", () => {
    const raw = fixture.replace("fixture user prompt; do not publish", "User prompt: private-input; credential=fixture-secret-value");
    const result = parseDeepSeekLog(raw);
    const published = JSON.stringify(result);

    expect(published).not.toContain("private-input");
    expect(published).not.toContain("fixture-secret-value");
    expect(published).not.toContain("COPILOT_GITHUB_TOKEN");
    expect(published).not.toContain("models fetch returned 401");
    expect(published).not.toContain("Stopping containers");
    expect(result.logEntries[1].data.content).toBe(answer);
  });

  it("preserves multiline answers while normalizing CRLF", () => {
    expect(parseDeepSeekLog(fixture.replace(/\n/g, "\r\n")).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it("preserves paragraph indentation and trailing spaces except for ANSI framing", () => {
    const text = "  First paragraph with preserved spaces.  \n\n\tSecond paragraph.\n\nFinal paragraph.  ";
    const raw = fixture.replace(answer, `\u001b[32m${text}\u001b[0m`);
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries[1].data.content).toBe(text);
  });

  it("accepts ANSI-colored framing without retaining escape sequences", () => {
    const raw = fixture.replace("[deepseek-harness]", "\u001b[32m[deepseek-harness]\u001b[0m").replace("[INFO] Stopping containers...", "\u001b[32m[INFO] Stopping containers...\u001b[0m");

    expect(parseDeepSeekLog(raw).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it("does not fill missing source fields with generated canonical identifiers or metrics", () => {
    for (const event of parseDeepSeekLog(fixture).logEntries) {
      for (const key of ["id", "sessionId", "toolCallId", "timestamp", "turnId", "numTurns", "turns", "usage", "inputTokens", "outputTokens", "totalTokens", "success", "status"]) {
        expect(event).not.toHaveProperty(key);
        expect(event.data).not.toHaveProperty(key);
      }
    }
  });

  it("does not invent tool calls, errors, success, usage, or reasoning from the smoke summary", () => {
    const result = parseDeepSeekLog(fixture);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "assistant.message"]);
    expect(result.logEntries.some(event => event.type.startsWith("tool.") || event.type === "session.error" || event.type === "session.result" || event.type === "assistant.reasoning")).toBe(false);
  });

  it("does not interpret tool-looking answer text as a native tool transcript", () => {
    const text = 'Tool call: bash {"command":"make build"}\nOutcome: build download forbidden.';
    const result = parseDeepSeekLog(fixture.replace(answer, text));

    expect(result.logEntries[1]).toEqual({ type: "assistant.message", data: { content: text } });
    expect(result.logEntries).toHaveLength(2);
  });

  it.each(["", "Some unrelated stdout", answer, '{"type":"assistant","message":{"content":"unrelated"}}'])("withholds unrecognized input %s", raw => {
    const result = parseDeepSeekLog(raw);

    expect(isDeepSeekLog(raw)).toBe(false);
    expect(result.logEntries).toEqual([]);
    expect(result.markdown).toContain("Raw content is omitted");
    if (raw) expect(result.markdown).not.toContain(raw);
  });

  it.each([
    fixture.slice(0, fixture.indexOf("[INFO] Stopping containers...")),
    fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command failed"),
    fixture.replace("Process exiting with code: 0", "Process exiting with code: 1"),
    fixture.replace(answer, ""),
    fixture.replace(answer, "User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "  User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "System message: private-input"),
    fixture.replace(answer, "Prompt: private-input"),
    fixture.replace(answer, "[entrypoint] Unexpected diagnostic\nPossible answer"),
  ])("withholds incomplete or ambiguous answers without losing configuration", raw => {
    expect(isDeepSeekLog(raw)).toBe(true);
    expect(parseDeepSeekLog(raw).logEntries).toEqual([{ type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "claude-sonnet-4.5" } }]);
    expect(parseDeepSeekLog(raw).markdown).toContain("Unattributed stdout is omitted");
    expect(parseDeepSeekLog(raw).markdown).not.toContain("private-input");
  });

  it.each([fixture.replace("[deepseek-harness] configured provider=github model=claude-sonnet-4.5\n", ""), `${fixture}\n${fixture}`])("withholds unattributed or repeated headless envelopes", raw => {
    expect(isDeepSeekLog(raw)).toBe(false);
    expect(parseDeepSeekLog(raw).logEntries).toEqual([]);
  });

  it("ignores post-shutdown content rather than publishing another conversation", () => {
    const result = parseDeepSeekLog(`${fixture}\nUser prompt: private-input\nOther assistant text`);

    expect(result.logEntries[1].data.content).toBe(answer);
    expect(JSON.stringify(result)).not.toContain("private-input");
    expect(JSON.stringify(result)).not.toContain("Other assistant text");
  });

  it("retains the configuration from the actual timed-out stdout shape without inventing a final response", () => {
    const result = parseDeepSeekLog(timedOut);

    expect(result.logEntries).toEqual([{ type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "auto" } }]);
    expect(result.markdown).toContain("Headless assistant output is unavailable");
    expect(result.markdown).not.toContain("private fixture prompt");
    expect(projectSessionResult(result.logEntries)).toBeUndefined();
    expect(parseCustomLog(timedOut).logEntries).toEqual(result.logEntries);
    expect(parseEngineSession(timedOut, "deepseek-harness")).toEqual(result.logEntries);
  });

  it("captures the observed AWF timeout without inventing an exit code", () => {
    // Existing live run https://github.com/github/gh-aw/actions/runs/42540358186.
    const raw = `${timedOut}[WARN] Agent command timed out after 15 minutes, stopping container...\n`;
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({ message: "Agent command timed out after 15 minutes, stopping container...", code: "agentic_engine_timeout", errorType: "TimeoutError" });
    expect(result.logEntries[2].data).toEqual({ categories: ["agentic_engine_timeout"], errorCodes: ["agentic_engine_timeout"], errorTypes: ["TimeoutError"] });
    expect(projectSessionResult(result.logEntries)).toBeUndefined();
    expect(JSON.stringify(result)).not.toContain("private fixture prompt");
  });

  it.each([
    fixture.replace(answer, "[WARN] Agent command timed out after 15 minutes, stopping container..."),
    `${timedOut}User prompt: private-input\n[WARN] Agent command timed out after 15 minutes, stopping container...\n`,
    `${timedOut}The log says:\n[WARN] Agent command timed out after 15 minutes, stopping container...\n`,
    `${timedOut}[WARN] Agent command timed out after 15 minutes, stopping container...\nOther assistant text\n`,
  ])("does not attribute quoted or ambiguous timeout text to execution", raw => {
    expect(parseDeepSeekLog(raw).logEntries.some(event => event.type === "session.error")).toBe(false);
  });

  it("captures a matching failed wrapper exit independently of unlabeled output", () => {
    const raw = fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command completed with exit code: 7").replace("Process exiting with code: 0", "Process exiting with code: 7");
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({ message: "DeepSeek Harness execution exited with code 7", exitCode: 7 });
    expect(result.logEntries[2].data.exitCode).toBe(7);
    expect(JSON.stringify(result)).not.toContain(answer);
  });

  it.each(["Partial assistant output", "Ordinary diagnostic\nError: unframed child failure"])("retains a verified wrapper failure after output and a harness failure: %s", text => {
    const raw = startupFailure.replace(/file:\/\/\/fixture[\s\S]*?Node\.js v24\.21\.0/, text);
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({ message: "DeepSeek Harness execution exited with code 1", exitCode: 1 });
    expect(result.logEntries[2].data.exitCode).toBe(1);
    expect(JSON.stringify(result)).not.toContain(text);
    expect(projectSessionResult(result.logEntries)).toBeUndefined();
  });

  it("falls back to wrapper evidence when output follows the harness failure", () => {
    const raw = startupFailure.replace("file:///fixture", "Partial output\nfile:///fixture").replace("[entrypoint] Relaxed", "More unattributed output\n[entrypoint] Relaxed");
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({ message: "DeepSeek Harness execution exited with code 1", exitCode: 1 });
    expect(JSON.stringify(result)).not.toContain("unattributed output");
  });

  it.each([
    startupFailure.replace(/file:\/\/\/fixture[\s\S]*?Node\.js v24\.21\.0/, "User prompt: private-input"),
    startupFailure.replace(/file:\/\/\/fixture[\s\S]*?Node\.js v24\.21\.0/, "Partial output").replace("Process exiting with code: 1", "Process exiting with code: 0"),
    startupFailure.replace(/file:\/\/\/fixture[\s\S]*?Node\.js v24\.21\.0/, "Partial output").replace("[WARN] Command completed with exit code: 1", "[SUCCESS] Command completed successfully"),
    fixture.replace(answer, "Partial output\n[deepseek-harness] DeepSeek Harness execution failed with exit code 1"),
    `${timedOut}Partial output\n[deepseek-harness] DeepSeek Harness execution failed with exit code 1\n`,
  ])("retains quote and contradiction guards when startup extraction rejects the body", raw => {
    expect(parseDeepSeekLog(raw).logEntries.some(event => event.type === "session.error")).toBe(false);
  });

  it.each([
    fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command completed with exit code: 7"),
    `${fixture}[WARN] Command completed with exit code: 7\nProcess exiting with code: 7\n`,
    fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command completed with exit code: 256").replace("Process exiting with code: 0", "Process exiting with code: 256"),
  ])("rejects contradictory or invalid wrapper failure framing", raw => {
    expect(parseDeepSeekLog(raw).logEntries.some(event => event.type === "session.error")).toBe(false);
  });

  it.each(["none", "init-only", "full"])("normalizes observed AWF timeout evidence with a zero exit artifact and $0 canonical capture", canonical => {
    const raw = `${timedOut}[WARN] Agent command timed out after 15 minutes, stopping container...\n`;
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-timeout-capture-"));
    try {
      vi.stubEnv("GH_AW_ENGINE_ID", "deepseek-harness");
      vi.stubEnv("GH_AW_AGENTIC_EXECUTION_OUTCOME", "failure");
      const parsed = parseDeepSeekLog(raw).logEntries;
      fs.writeFileSync(path.join(root, "agent-stdio.log"), raw);
      fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0");
      if (canonical !== "none") writeSessionArtifact(path.join(root, "agent-session.jsonl"), canonical === "full" ? parsed : parsed.filter(event => event.type === "session.init"));
      persistAgentExecution(raw, detectErrors(raw), root);
      const detected = fs.readFileSync(path.join(root, "agent-errors.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
      expect(detected).toEqual(parsed.filter(event => event.type === "session.error" || event.type === "agent.execution"));
      const events = writeUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} });
      const errors = events.filter(event => event.type === "session.error");
      expect(errors).toHaveLength(1);
      expect(errors[0].data.code).toBe("agentic_engine_timeout");
      const execution = events.filter(event => event.type === "agent.execution");
      expect(execution).toHaveLength(1);
      expect(execution[0].data.categories).toContain("agentic_engine_timeout");
      expect(execution[0].data).not.toHaveProperty("exitCode");
      expect(events.some(event => event.type === "assistant.message" || event.type === "assistant.reasoning" || event.type === "session.result" || event.type.startsWith("tool."))).toBe(false);
      const jsonl = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
      expect(jsonl.trimEnd().split("\n").map(JSON.parse)).toEqual(events);
      expect(validateSession(jsonl)).toBe(events.length);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each([
    { raw: startupFailure, recordedExit: 0, expectedExit: 1 },
    { raw: startupFailure, recordedExit: 7, expectedExit: 7 },
    { raw: `${timedOut}[WARN] Agent command timed out after 15 minutes, stopping container...\n`, recordedExit: 124, expectedExit: 124 },
    { raw: fixture, recordedExit: 0, expectedExit: 0 },
  ])("reconciles recorded exit $recordedExit with attributed DeepSeek observations", ({ raw, recordedExit, expectedExit }) => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-exit-capture-"));
    try {
      vi.stubEnv("GH_AW_ENGINE_ID", "deepseek-harness");
      vi.stubEnv("GH_AW_AGENTIC_EXECUTION_OUTCOME", "");
      fs.writeFileSync(path.join(root, "agent-stdio.log"), raw);
      fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), String(recordedExit));
      const fallback = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} }).events;
      expect(fallback.find(event => event.type === "agent.execution").data.exitCode).toBe(expectedExit);
      persistAgentExecution(raw, detectErrors(""), root);
      const detected = fs
        .readFileSync(path.join(root, "agent-errors.jsonl"), "utf8")
        .trimEnd()
        .split("\n")
        .map(JSON.parse)
        .find(event => event.type === "agent.execution");
      expect(detected.data.exitCode).toBe(expectedExit);
      const persisted = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} }).events;
      expect(persisted.find(event => event.type === "agent.execution").data.exitCode).toBe(expectedExit);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each(["none", "init-only", "partial", "full"])("preserves parsed startup errors without duplicating $0 canonical capture", canonical => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-error-capture-"));
    try {
      vi.stubEnv("GH_AW_ENGINE_ID", "deepseek-harness");
      vi.stubEnv("GH_AW_AGENTIC_EXECUTION_OUTCOME", "failure");
      const parsed = parseDeepSeekLog(startupFailure).logEntries;
      const parsedErrors = parsed.filter(event => event.type === "session.error");
      fs.writeFileSync(path.join(root, "agent-stdio.log"), startupFailure);
      fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0");
      if (canonical !== "none") {
        const captured = canonical === "full" ? parsed : canonical === "partial" ? parsed.slice(0, 2) : parsed.slice(0, 1);
        writeSessionArtifact(
          path.join(root, "agent-session.jsonl"),
          captured.map((event, index) => ({
            ...event,
            id: `canonical-${index}`,
            timestamp: "2026-11-08T00:00:00Z",
            data: Object.fromEntries(Object.entries(event.data).reverse()),
          }))
        );
      }
      persistAgentExecution(startupFailure, detectErrors(""), root);
      const events = writeUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} });
      const errors = events.filter(event => event.type === "session.error");
      expect(errors).toHaveLength(parsedErrors.length);
      expect(errors.map(event => event.data)).toEqual(parsedErrors.map(event => normalizeUnifiedSessionEvent(event).data));
      if (canonical === "full" || canonical === "partial") {
        expect(errors[0].id).toBe("canonical-1");
        expect(errors[0].provenance.path).toBe("agent-session.jsonl");
      }
      const execution = events.filter(event => event.type === "agent.execution");
      expect(execution).toHaveLength(1);
      expect(execution[0].data.exitCode).toBe(1);
      expect(execution[0].data.errorTypes).toEqual(["Error"]);
      expect(events.some(event => event.type === "assistant.message" || event.type === "assistant.reasoning" || event.type === "session.result" || event.type.startsWith("tool."))).toBe(false);
      expect(validateSession(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8"))).toBe(events.length);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("preserves distinct canonical errors and repeated error occurrences while removing detector copies", () => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-distinct-errors-"));
    try {
      vi.stubEnv("GH_AW_ENGINE_ID", "deepseek-harness");
      vi.stubEnv("GH_AW_AGENTIC_EXECUTION_OUTCOME", "failure");
      const parsed = parseDeepSeekLog(startupFailure).logEntries;
      const error = parsed.find(event => event.type === "session.error");
      const distinctError = { ...error, data: { ...error.data, code: "distinct_failure" } };
      fs.writeFileSync(path.join(root, "agent-stdio.log"), startupFailure);
      fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0");
      writeSessionArtifact(path.join(root, "agent-session.jsonl"), [...parsed, error, distinctError]);
      persistAgentExecution(startupFailure, detectErrors(""), root);
      const events = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} }).events;
      expect(events.filter(event => event.type === "session.error")).toHaveLength(4);
      expect(events.filter(event => event.type === "session.error" && event.data.code === "distinct_failure")).toHaveLength(1);
      expect(events.filter(event => event.type === "agent.execution")).toHaveLength(1);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it.each([
    { outcome: "failure", timeout: false, code: "agentic_engine_execution_failed" },
    { outcome: "failure", timeout: true, code: "agentic_engine_timeout" },
  ])("retains failed step metadata when no native terminal diagnostics exist: $code", ({ outcome, timeout, code }) => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-step-failure-"));
    try {
      vi.stubEnv("GH_AW_ENGINE_ID", "deepseek-harness");
      vi.stubEnv("GH_AW_AGENTIC_EXECUTION_OUTCOME", outcome);
      fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0");
      persistAgentExecution("", { ...detectErrors(""), agenticEngineTimeout: timeout }, root);
      const events = writeUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} });
      expect(events.filter(event => event.type === "session.error")).toHaveLength(1);
      expect(events.find(event => event.type === "session.error").data.code).toBe(code);
      expect(events.find(event => event.type === "agent.execution").data).not.toHaveProperty("exitCode");
      expect(events.find(event => event.type === "agent.execution").data.errorCodes).toContain(code);
    } finally {
      vi.unstubAllEnvs();
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("preserves the actual startup error and observed exit as standard session observations", () => {
    const result = parseDeepSeekLog(startupFailure);
    const message = "dsh: user patch-layer watching requires the Cordis HMR service";

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({
      message,
      errorType: "Error",
      stack: `Error: ${message}\n    at watchUserPatches (file:///fixture/node_modules/@deepseek-ai/dsh-app-boot/lib/index.js:764:28)\n    at runProfile (file:///fixture/node_modules/@deepseek-ai/dsh/lib/profile-boot.js:264:9)\n    at async file:///fixture/node_modules/@deepseek-ai/dsh/lib/bin.js:133:3`,
    });
    expect(result.logEntries[2].data).toEqual({ message: "DeepSeek Harness execution failed with exit code 1", exitCode: 1 });
    expect(result.logEntries[3].data).toEqual({ categories: [], errorCodes: [], errorTypes: ["Error"], exitCode: 1 });
    expect(result.logEntries.some(event => event.type === "assistant.message" || event.type === "session.result")).toBe(false);
    expect(JSON.stringify(result)).not.toContain("private fixture prompt");
    expect(parseCustomLog(startupFailure).logEntries).toEqual(result.logEntries);
    expect(parseEngineSession(startupFailure, "deepseek-harness")).toEqual(result.logEntries);
  });

  it("preserves Node startup errors when the header includes a column", () => {
    const raw = startupFailure.replace("lib/index.js:764\n", "lib/index.js:764:28\n");

    expect(parseDeepSeekLog(raw).logEntries).toEqual(parseDeepSeekLog(startupFailure).logEntries);
  });

  it("retains a harness failure even without a Node stack or AWF shutdown", () => {
    const result = parseDeepSeekLog(`${timedOut}[deepseek-harness] DeepSeek Harness execution failed with exit code 137 (signal=SIGKILL)\n`);

    expect(result.logEntries[1]).toEqual({ type: "session.error", data: { message: "DeepSeek Harness execution failed with exit code 137 (signal=SIGKILL)", exitCode: 137 } });
    expect(result.logEntries.at(-1).data.exitCode).toBe(137);
    expect(result.logEntries.some(event => event.type === "session.result")).toBe(false);
  });

  it("does not attribute an unframed Error line, provider warning, or a quoted stack to the engine", () => {
    const text = "The tool failed:\nError: tool build download forbidden\n    at tool.js:1\nTry another mirror.";
    const quoted = parseDeepSeekLog(fixture.replace(answer, text));

    expect(quoted.logEntries.map(event => event.type)).toEqual(["session.init", "assistant.message"]);
    expect(quoted.logEntries[1].data.content).toBe(text);
    expect(quoted.logEntries.some(event => event.type === "session.error")).toBe(false);
    expect(parseDeepSeekLog(fixture).logEntries.some(event => event.type === "session.error")).toBe(false);
  });

  it.each(["Here is the fix.\n[deepseek-harness] DeepSeek Harness execution failed with exit code 1\nApply it.", "[deepseek-harness] DeepSeek Harness execution failed with exit code 1"])(
    "preserves a quoted harness failure in completed assistant text",
    text => {
      const result = parseDeepSeekLog(fixture.replace(answer, text));

      expect(result.logEntries).toEqual([
        { type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "claude-sonnet-4.5" } },
        { type: "assistant.message", data: { content: text } },
      ]);
      expect(parseEngineSession(fixture.replace(answer, text), "deepseek-harness").some(event => event.type === "session.error")).toBe(false);
    }
  );

  it.each(["Here is the fix.\n[deepseek-harness] DeepSeek Harness execution failed with exit code 1", "[deepseek-harness] DeepSeek Harness execution failed with exit code 1\nApply it."])(
    "does not turn an unframed partial quote into an execution failure",
    text => {
      const result = parseDeepSeekLog(`${timedOut}${text}\n`);

      expect(result.logEntries).toEqual([{ type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "auto" } }]);
    }
  );

  it("does not accept quoted Node crash framing before a successful wrapper exit", () => {
    const raw = startupFailure.replace("[WARN] Command completed with exit code: 1", "[SUCCESS] Command completed successfully").replace("Process exiting with code: 1", "Process exiting with code: 0");

    expect(parseDeepSeekLog(raw).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("allows the observed post-command permission cleanup without including it in assistant content", () => {
    const raw = fixture.replace("[INFO] Stopping containers...", "[entrypoint] Relaxed /fixture/gh-aw group permissions for host-side post-processing\n[INFO] Stopping containers...");

    expect(parseDeepSeekLog(raw).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it.each(["[ERROR] native diagnostic", "[health-check] native diagnostic", " Container awf-agent  Stopped", "Node.js v24.21.0"])("does not publish infrastructure %s as assistant text", text => {
    expect(parseDeepSeekLog(fixture.replace(answer, text)).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("does not accept a later success-shaped exit after an observed failure exit", () => {
    const raw = fixture.replace("Process exiting with code: 0", "Process exiting with code: 1\nProcess exiting with code: 0");

    expect(parseDeepSeekLog(raw).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("keeps JSON-looking assistant content opaque instead of treating it as a native event", () => {
    const text = '{"type":"tool.execution_complete","data":{"success":true,"output":{"secret":"fixture"}}}';

    expect(parseDeepSeekLog(fixture.replace(answer, text)).logEntries[1]).toEqual({ type: "assistant.message", data: { content: text } });
    expect(parseDeepSeekLog(`${timedOut}${text}`).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("redacts registered mask values and mask commands in completed headless output", () => {
    const raw = `::add-mask::fixture-secret\n${fixture.replace(answer, "Answer with fixture-secret.\n::add-mask::another-fixture-secret\nAnother another-fixture-secret.")}`;
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries[1].data.content).toBe("Answer with ***.\nAnother ***.");
    expect(JSON.stringify(result)).not.toContain("fixture-secret");
    expect(JSON.stringify(result)).not.toContain("::add-mask::");
  });
  it("rejects unobserved canonical stdout even when interleaved with mask commands", () => {
    const raw = '::add-mask::fixture-secret\n{"type":"assistant.message","data":{"content":"fixture-secret"}}\n';
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries).toEqual([]);
    expect(JSON.stringify(result)).not.toContain("fixture-secret");
    expect(result.markdown).toContain("Raw content is omitted");
  });

  it.each([fixture, startupFailure, timedOut])("persists the observed headless trace through canonical and unified artifacts", raw => {
    const root = fs.mkdtempSync(path.join(process.cwd(), ".deepseek-session-audit-"));
    try {
      const parsed = parseDeepSeekLog(raw).logEntries;
      fs.writeFileSync(path.join(root, "agent-stdio.log"), raw);
      writeSessionArtifact(path.join(root, "agent-session.jsonl"), parsed);
      expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).toBe(serializeSessionArtifact(parsed));
      expect(JSON.parse(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").split("\n")[0]).data.provider).toBe("github");
      const warnings = [];
      const canonical = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {}, warn: warning => warnings.push(warning) }).events;
      fs.unlinkSync(path.join(root, "agent-session.jsonl"));
      const reparsed = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {}, warn: warning => warnings.push(warning) }).events;
      const agent = session => session.filter(event => event.provenance?.component === "agent").map(({ provenance, ...event }) => event);
      expect(agent(reparsed)).toEqual(agent(canonical));
      expect(warnings).toEqual([]);
      const persisted = writeUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} });
      const jsonl = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
      expect(jsonl.endsWith("\n")).toBe(true);
      expect(jsonl.trimEnd().split("\n").map(JSON.parse)).toEqual(persisted);
      expect(persisted[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
      expect(agent(persisted)).toEqual(agent(reparsed));
      for (const event of persisted.filter(event => event.provenance?.component === "agent")) {
        expect(event.provenance.path).toBe("agent-stdio.log");
        expect(event.provenance.phase).toBe("agent");
      }
      expect(persisted.find(event => event.type === "session.init").data).toEqual({ sourceEngine: "deepseek-harness", model: parsed[0].data.model });
      expect(persisted.some(event => event.type === "session.result" || event.type === "assistant.refusal" || event.type.startsWith("tool.") || event.type === "assistant.reasoning")).toBe(false);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
