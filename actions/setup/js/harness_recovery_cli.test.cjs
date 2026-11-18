import { afterEach, describe, expect, it } from "vitest";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
let directory;
afterEach(() => {
  if (directory) fs.rmSync(directory, { recursive: true, force: true });
  directory = undefined;
});

describe("Recovery CLI prompt delivery", () => {
  it("reports unsupported shell controls without adding a retry or altering the result", () => {
    const result = spawnSync(process.execPath, [require.resolve("./shell_harness.cjs"), "custom", "printf 'completed\\n'; exit 7"], {
      encoding: "utf8",
      timeout: 5000,
      env: { ...process.env, GH_AW_HARNESS_RECOVERY: "1", GH_AW_SKIP_IF_DECISION: "" },
    });
    expect(result.status).toBe(7);
    expect(result.stderr).toContain("no native steering or retry guidance capability");
    expect(result.stdout.match(/completed/g)).toHaveLength(1);
  });

  it.each(["copilot", "codex", "claude"])("preserves %s prompt and session controls while adding guidance to an authorized retry", engine => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "harness-recovery-cli-"));
    const promptPath = path.join(directory, "prompt.txt");
    const callsPath = path.join(directory, "calls.jsonl");
    const decisionsPath = path.join(directory, "decisions.jsonl");
    const executable = path.join(directory, "agent.cjs");
    const preload = path.join(directory, "preload.cjs");
    fs.writeFileSync(promptPath, "PRIVATE original workflow instructions");
    fs.writeFileSync(
      preload,
      `
      const fs = require("node:fs");
      require(${JSON.stringify(require.resolve("./harness_recovery.cjs"))}).evaluateRecoveryDecision = async evidence => {
        fs.appendFileSync(process.env.RECOVERY_TEST_DECISIONS, JSON.stringify(evidence) + "\\n");
        return "change_strategy";
      };
      require(${JSON.stringify(require.resolve("./ai_credits_context.cjs"))}).parseMaxAICreditsExceededFromAuditLog = () => null;
    `
    );
    fs.writeFileSync(
      executable,
      `#!${process.execPath}
      const fs = require("node:fs");
      let stdin = "";
      process.stdin.setEncoding("utf8");
      process.stdin.on("data", chunk => stdin += chunk);
      process.stdin.on("end", () => {
        fs.appendFileSync(process.env.RECOVERY_TEST_CALLS, JSON.stringify({ args: process.argv.slice(2), stdin }) + "\\n");
        const attempt = fs.readFileSync(process.env.RECOVERY_TEST_CALLS, "utf8").trim().split("\\n").length;
        const id = "11111111-1111-4111-8111-111111111111";
        const engine = process.env.RECOVERY_TEST_ENGINE;
        if (engine === "codex") console.log(JSON.stringify({ type: "thread.started", thread_id: id }));
        if (engine === "claude") {
          console.log(JSON.stringify({ type: "system", subtype: "init", session_id: id }));
          console.log(JSON.stringify({ type: "assistant", message: { content: [{ type: "text", text: "Working on unfinished work." }] } }));
        }
        if (attempt < 4) {
          if (engine === "codex") console.log(JSON.stringify({ type: "turn.failed", error: { message: "503 Service Unavailable" } }));
          else if (engine === "claude") console.log(JSON.stringify({ type: "result", is_error: true, result: "API Error: 503 Service Unavailable" }));
          else console.log("Error: CAPI 503 Service Unavailable");
          process.exitCode = 1;
        }
      });
    `
    );
    fs.chmodSync(executable, 0o700);
    const args = engine === "codex" ? ["exec", "--sandbox", "read-only"] : engine === "claude" ? ["--print", "--output-format", "stream-json"] : ["--allow-tool", "view"];
    const result = spawnSync(process.execPath, [require.resolve(`./${engine}_harness.cjs`), executable, ...args, "--prompt-file", promptPath], {
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...process.env,
        NODE_OPTIONS: `--require ${JSON.stringify(preload)}`,
        RUNNER_TEMP: directory,
        HOME: path.join(directory, "home"),
        CODEX_HOME: "",
        CODEX_API_KEY: "test-placeholder",
        COPILOT_MODEL: "",
        COPILOT_SDK_MODE: "",
        GH_AW_COPILOT_SDK: "",
        ANTHROPIC_MODEL: "",
        GH_AW_SESSION_CONTINUATION: "",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "",
        GH_AW_MODEL_FALLBACK: "",
        GH_AW_HARNESS_RECOVERY: "1",
        GH_AW_HARNESS_MAX_RETRIES: "3",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_DELAY_MS: "1",
        GH_AW_TIMEOUT_MINUTES: "10",
        GH_AW_SKIP_IF_DECISION: "",
        GH_AW_CLAUDE_ALLOW_FRESH_RESTART: "true",
        GH_AW_CODEX_CONTEXT_REBUILD_CIRCUIT_BREAKER: "false",
        GH_AW_SAFE_OUTPUTS: "",
        GH_AW_AGENT_OUTPUT: path.join(directory, "agent-output.json"),
        GH_AW_TMP_DIR: directory,
        GH_AW_HARNESS_TELEMETRY_DIR: directory,
        GH_AW_AWF_CONFIG_PATH: path.join(directory, "awf-config.json"),
        GH_AW_LLM_PROVIDER: engine === "claude" ? "anthropic" : "openai",
        GH_AW_LLM_PROVIDER_EXPLICIT: "0",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_SKIP_REFLECT: "true",
        RECOVERY_TEST_CALLS: callsPath,
        RECOVERY_TEST_DECISIONS: decisionsPath,
        RECOVERY_TEST_ENGINE: engine,
      },
    });
    expect(result.status, result.stderr).toBe(0);
    const calls = fs.readFileSync(callsPath, "utf8").trim().split("\n").map(JSON.parse);
    expect(calls).toHaveLength(4);
    expect(calls[0].stdin).toBe("PRIVATE original workflow instructions");
    expect(calls.slice(0, 3).every(call => !call.stdin.includes("Repeated failures"))).toBe(true);
    expect(calls[3].stdin).toContain("never bypass a denial");
    expect(calls.every(call => !JSON.stringify(call.args).includes("PRIVATE"))).toBe(true);
    expect(result.stderr).not.toContain("PRIVATE");
    const decisions = fs.readFileSync(decisionsPath, "utf8").trim().split("\n").map(JSON.parse);
    expect(decisions).toEqual([{ consecutiveFailures: 3, decision: 1, remainingMs: expect.any(Number) }]);
    if (engine === "codex") {
      expect(calls[3].args).toEqual(["exec", "--json", "--sandbox", "read-only", "resume", "11111111-1111-4111-8111-111111111111", "-"]);
      expect(calls[3].stdin).toContain("Complete only unfinished work.");
    } else if (engine === "claude") {
      expect(calls[3].args).toContain("--resume");
      expect(calls[3].args).toContain("11111111-1111-4111-8111-111111111111");
      expect(calls[3].stdin).not.toContain("PRIVATE");
    } else {
      expect(calls[3].args).toContain("--allow-tool");
      expect(calls[3].args).toContain("view");
      expect(calls[3].stdin).toContain("PRIVATE original workflow instructions");
    }
  });
});
