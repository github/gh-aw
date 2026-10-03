import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
const { CLAUDE_RESUME_PROMPT, claudeFailureEvidence, hasClaudeSessionProgress, claudeSessionId, claudePermissionDenials, claudeBareCapabilities, claudeRepositoryEditPolicy, removeClaudePlugin } = require("./claude_runtime.cjs");

const assistant = { type: "assistant", session_id: "exact-session", message: { content: [{ type: "text", text: "Working" }] } };

function runStub(script, { prompt = "task", env = {} } = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "claude-runtime-test-"));
  try {
    const calls = path.join(dir, "calls.jsonl");
    const stub = path.join(dir, "stub.cjs");
    const promptPath = path.join(dir, "prompt.txt");
    fs.writeFileSync(promptPath, prompt);
    fs.writeFileSync(
      stub,
      `
const fs = require("fs");
const args = process.argv.slice(2);
const previous = fs.existsSync(process.env.CALLS) ? fs.readFileSync(process.env.CALLS, "utf8").trim().split("\\n").length : 0;
const stdin = fs.readFileSync(0, "utf8");
fs.appendFileSync(process.env.CALLS, JSON.stringify({args, stdinLength: stdin.length, timeout: process.env.MCP_TOOL_TIMEOUT}) + "\\n");
${script}
`
    );
    const result = spawnSync(process.execPath, [path.join(__dirname, "claude_harness.cjs"), process.execPath, stub, "--print", "--prompt-file", promptPath], {
      encoding: "utf8",
      timeout: 8000,
      env: {
        ...process.env,
        GH_AW_SKIP_REFLECT: "true",
        GH_AW_HARNESS_MAX_RETRIES: "2",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_DELAY_MS: "1",
        GH_AW_CLAUDE_ALLOW_FRESH_RESTART: "false",
        GITHUB_WORKSPACE: dir,
        GH_AW_ENGINE_CWD: dir,
        GH_AW_SAFE_OUTPUTS: path.join(dir, "outputs.jsonl"),
        CALLS: calls,
        ...env,
      },
    });
    expect(result.error).toBeUndefined();
    return {
      result,
      calls: fs
        .readFileSync(calls, "utf8")
        .trim()
        .split("\n")
        .map(line => JSON.parse(line)),
    };
  } finally {
    fs.rmSync(dir, { recursive: true });
  }
}

describe("Claude runtime contracts", () => {
  it("creates bare-mode plugins in sandbox-writable scratch instead of RUNNER_TEMP", () => {
    const managedDir = fs.mkdtempSync(path.join(os.tmpdir(), "claude-managed-test-"));
    const priorRunnerTemp = process.env.RUNNER_TEMP;
    let pluginDir;
    try {
      fs.mkdirSync(path.join(managedDir, "skills"));
      process.env.RUNNER_TEMP = path.join(managedDir, "unmounted-runner-temp");
      const loaded = claudeBareCapabilities(["--bare"], managedDir);
      pluginDir = loaded.pluginDir;
      expect(path.dirname(pluginDir)).toBe("/tmp/gh-aw/agent");
      expect(fs.existsSync(process.env.RUNNER_TEMP)).toBe(false);
      expect(fs.realpathSync(path.join(pluginDir, "skills"))).toBe(fs.realpathSync(path.join(managedDir, "skills")));
    } finally {
      if (priorRunnerTemp === undefined) delete process.env.RUNNER_TEMP;
      else process.env.RUNNER_TEMP = priorRunnerTemp;
      removeClaudePlugin(pluginDir);
      fs.rmSync(managedDir, { recursive: true });
    }
  });
  it.each(["dontAsk", "acceptEdits", "bypassPermissions"])("denies every native editor in %s without disabling scoped memory edits", mode => {
    const original = ["--permission-mode", mode, "--allowed-tools", "Edit(//tmp/memory/**)", "--disallowed-tools", "Bash,Write"];
    const args = claudeRepositoryEditPolicy(original, { GH_AW_CLAUDE_DISABLE_REPO_EDITS: "true", GITHUB_WORKSPACE: "/workspace/repo/" });
    expect(args).toEqual(["--permission-mode", mode, "--allowed-tools", "Edit(//tmp/memory/**)", "--disallowed-tools", "Bash,Write,Edit(//workspace/repo/**),MultiEdit,NotebookEdit"]);
    expect(original.at(-1)).toBe("Bash,Write");
  });
  it("adds an editor deny list when the caller has no deny flag", () => {
    const args = claudeRepositoryEditPolicy([], { GH_AW_CLAUDE_DISABLE_REPO_EDITS: "true", GITHUB_WORKSPACE: "/workspace/repo" });
    expect(args).toEqual(["--disallowed-tools", "Edit(//workspace/repo/**),Write,MultiEdit,NotebookEdit"]);
  });
  it("leaves editor-enabled runs unchanged and rejects invalid workspace paths", () => {
    const args = ["--allowed-tools", "Edit,Write"];
    expect(claudeRepositoryEditPolicy(args, {})).toBe(args);
    expect(() => claudeRepositoryEditPolicy(args, { GH_AW_CLAUDE_DISABLE_REPO_EDITS: "true", GITHUB_WORKSPACE: "relative" })).toThrow("absolute GITHUB_WORKSPACE");
  });
  it("excludes model answers and tool results from failure classifiers", () => {
    const records = [assistant, { type: "user", message: { content: "unknown model fake not found" } }];
    expect(claudeFailureEvidence(records.map(JSON.stringify).join("\n"))).toBe("");
    expect(hasClaudeSessionProgress(JSON.stringify({ ...assistant, error: "server_error", is_api_error_message: true }))).toBe(false);
    expect(claudeSessionId(JSON.stringify(assistant))).toBe("exact-session");
    expect(claudeFailureEvidence(JSON.stringify({ type: "result", is_error: false, result: "unknown model fake not found" }))).not.toContain("unknown model");
    expect(claudeFailureEvidence(JSON.stringify({ type: "result", is_error: true, result: "API Error: 529 overloaded_error" }))).toContain("overloaded_error");
  });
  it("counts structured denials once and extracts the actual commands", () => {
    const record = { type: "result", permission_denials: [{ tool_use_id: "one", tool_input: { command: "blocked" } }] };
    expect(claudePermissionDenials(`${JSON.stringify(record)}\n${JSON.stringify(record)}`)).toEqual({ count: 1, commands: ["blocked"] });
  });
  it("delivers a large prompt through stdin and applies resolved timeouts", () => {
    const { result, calls } = runStub("process.exit(0);", { prompt: "x".repeat(512 * 1024), env: { GH_AW_TOOL_TIMEOUT: "90" } });
    expect(result.status).toBe(0);
    expect(calls[0]).toMatchObject({ stdinLength: 512 * 1024, timeout: "90000" });
    expect(calls[0].args.join(" ").length).toBeLessThan(100);
  });
  it("resumes the captured session ID with continuation input rather than replaying the original prompt", () => {
    const { result, calls } = runStub(`
if (previous === 0) {
  console.log(${JSON.stringify(JSON.stringify(assistant))});
  console.error("API Error: 529 overloaded_error");
  process.exit(1);
}
if (!stdin) {
  console.error("No deferred tool marker found; provide a prompt to continue");
  process.exit(1);
}
process.exit(0);
`);
    expect(result.status).toBe(0);
    expect(calls).toHaveLength(2);
    expect(calls[1].args.slice(-2)).toEqual(["--resume", "exact-session"]);
    expect(calls[1].stdinLength).toBe(CLAUDE_RESUME_PROMPT.length);
    expect(result.stderr).toContain("--resume exact-session");
  });
  it.each(["max_cache_misses_exceeded", "effective_tokens_limit_exceeded", "permission_denied_limit_exceeded", "model_policy_violation"])("does not retry terminal proxy guard %s", guard => {
    const { calls } = runStub(`
console.log(${JSON.stringify(JSON.stringify(assistant))});
console.log(JSON.stringify({type:"assistant",error:"server_error",is_api_error_message:true,message:{content:[{type:"text",text:${JSON.stringify(guard)}}]}}));
process.exit(1);`);
    expect(calls).toHaveLength(1);
  });
  it("does not replay partially completed work after a corrupted resume path", () => {
    const { result, calls } = runStub(`
console.log(${JSON.stringify(JSON.stringify(assistant))});
console.error("The request body is not valid JSON");
process.exit(1);`);
    expect(result.status).toBe(1);
    expect(calls).toHaveLength(1);
    expect(result.stderr).toContain("refusing to replay");
  });
  it("terminates a quiet child after a terminal safe-output", () => {
    const { result, calls } = runStub(
      `
fs.appendFileSync(process.env.GH_AW_SAFE_OUTPUTS, JSON.stringify({type:"add_comment",body:"done"}) + "\\n");
setInterval(() => {}, 10000);
`,
      { env: { GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS: "100" } }
    );
    expect(result.status).toBe(0);
    expect(calls).toHaveLength(1);
    expect(result.stderr).toContain("post-result watchdog");
  });
  it("preempts a running attempt at its soft deadline instead of waiting for another retry", () => {
    const { result, calls } = runStub("setInterval(() => {}, 10000);", { env: { GH_AW_TIMEOUT_MINUTES: "0.03" } });
    expect(result.status).not.toBe(0);
    expect(calls).toHaveLength(1);
    expect(result.stderr).toContain("during execution");
  });
  it("loads only workflow-managed capabilities explicitly in bare mode under the mounted temp directory", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "claude-bare-test-"));
    let pluginDir;
    try {
      const managed = path.join(dir, ".claude");
      fs.mkdirSync(path.join(managed, "skills/example"), { recursive: true });
      fs.mkdirSync(path.join(managed, "agents"));
      const loaded = claudeBareCapabilities(["--bare"], managed, dir);
      pluginDir = loaded.pluginDir;
      expect(loaded.args.slice(-2)).toEqual(["--plugin-dir", pluginDir]);
      expect(path.dirname(pluginDir)).toBe(dir);
      expect(fs.realpathSync(path.join(pluginDir, "skills"))).toBe(fs.realpathSync(path.join(managed, "skills")));
      expect(JSON.parse(fs.readFileSync(path.join(pluginDir, ".claude-plugin/plugin.json"), "utf8")).name).toBe("gh-aw-workflow");
      expect(claudeBareCapabilities([], managed, dir)).toEqual({ args: [], pluginDir: undefined });
    } finally {
      fs.rmSync(dir, { recursive: true });
    }
  });
});
