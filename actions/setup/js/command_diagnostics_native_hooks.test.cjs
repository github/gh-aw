import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";
import { spawn, spawnSync } from "node:child_process";
import { createInterface } from "node:readline";
import { once } from "node:events";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
const require = createRequire(import.meta.url);
const { diagnosticHookOutput } = require("./command_diagnostics_hook.cjs");
const { withClaudeDiagnostics } = require("./claude_diagnostics.cjs");
const { withCodexDiagnostics } = require("./codex_diagnostics.cjs");
const { diagnoseToolResult } = require("./command_diagnostics_tools.cjs");
const { renderDiagnostics } = require("./command_diagnostics_render.cjs");
const { serializeConfig } = require("./codex_config.cjs");

const fixtures = [
  ["go", "go test ./...", "main.go:4:2: undefined: missing"],
  ["typescript", "npx tsc --noEmit", "src/main.ts(4,2): error TS2322: Wrong type"],
  ["python", "python main.py", 'Traceback (most recent call last):\n  File "/repo/main.py", line 4\nValueError: invalid value'],
];
const env = { GITHUB_WORKSPACE: "/repo" };

describe("Claude and Codex diagnostic hooks", () => {
  it.each(fixtures)("provides identical %s context for both native protocols", (language, command, output) => {
    const input = { hook_event_name: "PostToolUse", tool_name: "Bash", tool_use_id: "tool-1", tool_input: { command }, tool_response: { stdout: output, stderr: "", exit_code: 1 }, cwd: "/repo" };
    const original = structuredClone(input);
    const expected = renderDiagnostics(diagnoseToolResult("Bash", { command }, output, [language], { root: "/repo", cwd: "/repo" }));
    for (const engine of ["claude", "codex"]) {
      expect(diagnosticHookOutput(engine, input, [language], env)).toEqual({ hookSpecificOutput: { hookEventName: "PostToolUse", additionalContext: expected } });
      expect(input).toEqual(original);
    }
    expect(diagnosticHookOutput("claude", { ...input, hook_event_name: "PostToolUseFailure", error: output, tool_response: undefined }, [language], env)).toEqual({
      hookSpecificOutput: { hookEventName: "PostToolUseFailure", additionalContext: expected },
    });
    expect(diagnosticHookOutput("codex", { ...input, tool_response: output }, [language], env).hookSpecificOutput.additionalContext).toBe(expected);
  });

  it("ignores unrelated tools/events, disabled selection, and unrecognized output", () => {
    const input = { hook_event_name: "PostToolUse", tool_name: "Bash", tool_response: "main.go:1: wrong", cwd: "/repo" };
    expect(diagnosticHookOutput("claude", input, undefined, env)).toBeUndefined();
    for (const engine of ["claude", "codex"]) {
      expect(diagnosticHookOutput(engine, { ...input, tool_name: "Read" }, ["go"], env)).toBeUndefined();
      expect(diagnosticHookOutput(engine, { ...input, hook_event_name: "PreToolUse" }, ["go"], env)).toBeUndefined();
      expect(diagnosticHookOutput(engine, { ...input, tool_response: "ordinary text" }, ["go"], env)).toBeUndefined();
    }
    expect(diagnosticHookOutput("codex", { ...input, hook_event_name: "PostToolUseFailure" }, ["go"], env)).toBeUndefined();
  });

  it("supports observed MCP shell results without parsing arbitrary MCP tools", () => {
    const input = { hook_event_name: "PostToolUse", tool_name: "mcp__cli__bash", tool_input: { command: "go test" }, tool_response: { content: [{ type: "text", text: "main.go:1: bad" }] }, cwd: "/repo" };
    expect(diagnosticHookOutput("codex", input, ["go"], env).hookSpecificOutput.additionalContext).toContain("main.go:1");
    expect(diagnosticHookOutput("codex", { ...input, tool_name: "mcp__cli__read_file" }, ["go"], env)).toBeUndefined();
  });

  it("maps explicit shell working directories and does not guess after cd", () => {
    const input = { hook_event_name: "PostToolUse", tool_name: "Bash", tool_input: { command: "go test", workdir: "nested" }, tool_response: "main.go:1: bad", cwd: "/repo/sub" };
    expect(diagnosticHookOutput("codex", input, ["go"], env).hookSpecificOutput.additionalContext).toContain("sub/nested/main.go:1");
    const report = diagnoseToolResult("Bash", { command: " cd /elsewhere && go test" }, "main.go:1: bad", ["go"], { root: "/repo", cwd: "/repo" });
    expect(report.diagnostics[0].location).toBeUndefined();
  });

  it("uses bounded stdin JSON and redacts explicitly known credentials", () => {
    const hook = require.resolve("./command_diagnostics_hook.cjs");
    const input = { hook_event_name: "PostToolUseFailure", tool_name: "Bash", error: "main.go:1: test-auth-value", cwd: "/repo" };
    const result = spawnSync(process.execPath, [hook, "claude", '["go"]'], { input: JSON.stringify(input), encoding: "utf8", env: { ...process.env, ...env, ANTHROPIC_API_KEY: "test-auth-value" } });
    expect(result.status).toBe(0);
    expect(JSON.parse(result.stdout).hookSpecificOutput.additionalContext).toContain("main.go:1");
    expect(result.stdout).not.toContain("test-auth-value");
    for (const text of ["{secret-invalid-json", "x".repeat(512 * 1024 + 1)]) {
      const invalid = spawnSync(process.execPath, [hook, "claude", '["go"]'], { input: text, encoding: "utf8" });
      expect(invalid.status).toBe(1);
      expect(invalid.stdout).toBe("");
      expect(invalid.stderr).toContain("native tool results are unchanged");
      expect(invalid.stderr).not.toContain("secret-invalid-json");
    }
  });
});

describe("native hook configuration", () => {
  it("merges Claude explicit settings and hook arrays, including bare mode", () => {
    const existing = { matcher: "Write", hooks: [{ type: "command", command: "existing-hook" }] };
    const second = { matcher: "Edit", hooks: [{ type: "command", command: "second-hook" }] };
    const args = ["--bare", "--settings", JSON.stringify({ permissions: { deny: ["Bash(rm *)"] }, hooks: { PostToolUse: [existing] } }), "--settings=" + JSON.stringify({ hooks: { PostToolUse: [second] } }), "--", "prompt"];
    const result = withClaudeDiagnostics(args, ["go"]);
    const settings = JSON.parse(result[result.indexOf("--settings") + 1]);
    expect(settings.permissions.deny).toEqual(["Bash(rm *)"]);
    expect(settings.hooks.PostToolUse.slice(0, 2)).toEqual([existing, second]);
    expect(settings.hooks.PostToolUse.at(-1).hooks[0].command).toContain("command_diagnostics_hook.cjs");
    expect(settings.hooks.PostToolUseFailure).toHaveLength(1);
    expect(result).toContain("--bare");
    expect(result.slice(-2)).toEqual(["--", "prompt"]);
    expect(args).toHaveLength(6);
    expect(withClaudeDiagnostics(args, undefined)).toBe(args);
    expect(() => withClaudeDiagnostics(["--settings", '{"disableAllHooks":true}'], ["go"])).toThrow("requires Claude hooks");
  });

  it("loads file settings and preserves the launched process exit status", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-native-diagnostics-"));
    try {
      const settingsPath = path.join(directory, "settings.json");
      fs.writeFileSync(settingsPath, '{"permissions":{"deny":["Write"]}}');
      const updated = withClaudeDiagnostics(["--settings", settingsPath], ["python"]);
      expect(JSON.parse(updated[1]).permissions.deny).toEqual(["Write"]);
      const client = path.join(directory, "client.cjs");
      fs.writeFileSync(client, "process.stdout.write(JSON.stringify(process.argv.slice(2))); process.exitCode = 23;");
      const result = spawnSync(process.execPath, [require.resolve("./claude_diagnostics.cjs"), process.execPath, client, "--bare"], { encoding: "utf8", env: { ...process.env, GH_AW_DIAGNOSTICS: '["go"]' } });
      expect(result.status).toBe(23);
      expect(JSON.parse(result.stdout)).toContain("--settings");
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("appends and trusts only the bundled Codex handler with the pinned normalization", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-codex-diagnostics-"));
    try {
      const original = { features: { shell_tool: false }, hooks: { PostToolUse: [{ hooks: [{ type: "command", command: "untrusted-hook" }] }], state: { unrelated: { enabled: false } } } };
      const before = structuredClone(original);
      const config = withCodexDiagnostics(original, ["go", "typescript"], directory);
      expect(original).toEqual(before);
      expect(config.features).toEqual({ shell_tool: false, hooks: true });
      expect(config.hooks.PostToolUse).toHaveLength(2);
      const handler = config.hooks.PostToolUse[1].hooks[0];
      const normalized = `{"event_name":"post_tool_use","hooks":[{"async":false,"command":${JSON.stringify(handler.command)},"timeout":10,"type":"command"}],"matcher":${JSON.stringify(config.hooks.PostToolUse[1].matcher)}}`;
      expect(config.hooks.state).toEqual({
        unrelated: { enabled: false },
        [`${fs.realpathSync(directory)}/config.toml:post_tool_use:1:0`]: { enabled: true, trusted_hash: "sha256:" + createHash("sha256").update(normalized).digest("hex") },
      });
      expect(serializeConfig(config)).not.toContain("bypass_hook_trust");
      expect(withCodexDiagnostics(original, undefined)).toBe(original);
      expect(() => withCodexDiagnostics({ features: { hooks: false } }, ["go"], "/tmp/codex")).toThrow("requires Codex hooks");
      expect(() => withCodexDiagnostics({}, ["go"], "relative")).toThrow("absolute CODEX_HOME");
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it.skipIf(process.env.GH_AW_CODEX_DIAGNOSTICS_SMOKE !== "1")(
    "is accepted and selectively trusted by the pinned Codex CLI",
    async () => {
      const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-codex-hook-smoke-"));
      const home = path.join(directory, "codex");
      fs.mkdirSync(home);
      const config = withCodexDiagnostics({ features: { plugins: false }, hooks: { PostToolUse: [{ hooks: [{ type: "command", command: "echo untrusted" }] }] } }, ["go"], home);
      fs.writeFileSync(path.join(home, "config.toml"), serializeConfig(config));
      const child = spawn("codex", ["app-server", "--listen", "stdio://"], { cwd: directory, env: { PATH: process.env.PATH, HOME: directory, CODEX_HOME: home }, stdio: ["pipe", "pipe", "pipe"] });
      const started = once(child, "spawn");
      const lines = createInterface({ input: child.stdout });
      const pending = new Map();
      const timers = [];
      let stderr = "";
      child.stderr.on("data", chunk => {
        stderr += chunk;
      });
      lines.on("line", line => {
        const response = JSON.parse(line);
        pending.get(response.id)?.(response);
      });
      const request = (id, method, params) =>
        new Promise((resolve, reject) => {
          pending.set(id, response => (response.error ? reject(new Error(JSON.stringify(response.error))) : resolve(response.result)));
          timers.push(setTimeout(() => reject(new Error(`Codex ${method} timed out: ${stderr}`)), 10000));
          child.stdin.write(JSON.stringify({ id, method, params }) + "\n");
        });
      try {
        await started;
        await request(1, "initialize", { clientInfo: { name: "gh-aw-diagnostics-test", version: "1.0" }, capabilities: { experimentalApi: true } });
        child.stdin.write('{"method":"initialized"}\n');
        const result = await request(2, "hooks/list", { cwds: [directory] });
        const hooks = [];
        const collect = value => {
          if (Array.isArray(value)) value.forEach(collect);
          else if (value && typeof value === "object") {
            if ("currentHash" in value) hooks.push(value);
            else Object.values(value).forEach(collect);
          }
        };
        collect(result);
        const generated = hooks.find(hook => JSON.stringify(hook).includes("command_diagnostics_hook.cjs"));
        const untrusted = hooks.find(hook => JSON.stringify(hook).includes("echo untrusted"));
        expect(generated?.enabled).toBe(true);
        expect(generated?.key).toBe(Object.keys(config.hooks.state)[0]);
        expect(generated?.currentHash).toBe(Object.values(config.hooks.state)[0].trusted_hash);
        expect(generated?.trustStatus?.toLowerCase()).toBe("trusted");
        expect(untrusted?.trustStatus?.toLowerCase()).toBe("untrusted");
      } finally {
        timers.forEach(clearTimeout);
        lines.close();
        if (child.pid && child.exitCode === null && child.signalCode === null) {
          const exited = once(child, "exit");
          child.kill("SIGTERM");
          await exited;
        }
        fs.rmSync(directory, { recursive: true, force: true });
      }
    },
    20000
  );
});
