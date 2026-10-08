import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const { default: policyExtension, isPiBashAllowed } = await import("./pi_tool_policy.cjs");

afterEach(() => vi.unstubAllEnvs());

describe("Pi tool policy", () => {
  it("blocks and aborts if the shared budget cannot be enforced", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-tool-budget-failure-"));
    const root = path.join(dir, "not-a-directory");
    fs.writeFileSync(root, "Fixture");
    vi.stubEnv("GH_AW_PI_TOOL_BUDGET_DIR", root);
    vi.stubEnv("GH_AW_MAX_TOOL_CALLS", "1");
    const handlers = {};
    policyExtension({ on: (name, handler) => (handlers[name] = handler) });
    const ctx = { abort: vi.fn() };
    const previousExitCode = process.exitCode;
    try {
      expect(handlers.tool_call({ toolName: "read" }, ctx)).toMatchObject({ block: true, reason: expect.stringContaining("Cannot enforce") });
      expect(ctx.abort).toHaveBeenCalledOnce();
      expect(process.exitCode).toBe(1);
    } finally {
      process.exitCode = previousExitCode;
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
  it("reserves finite budget slots atomically across concurrent processes", async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-tool-budget-processes-"));
    const script = `const { reservePiBudget } = require(${JSON.stringify(path.join(process.cwd(), "pi_tool_policy.cjs"))}); console.log(reservePiBudget("calls", 0, 3));`;
    try {
      const results = await Promise.all(Array.from({ length: 6 }, () => promisify(execFile)(process.execPath, ["-e", script], { env: { ...process.env, GH_AW_PI_TOOL_BUDGET_DIR: dir } })));
      const reserved = results
        .map(result => Number(result.stdout))
        .filter(slot => slot <= 3)
        .sort();
      expect(reserved).toEqual([1, 2, 3]);
      expect(results.filter(result => Number(result.stdout) === 4)).toHaveLength(3);
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
  it("shares tool-call budget reservations between parent and child policies", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-tool-budget-"));
    vi.stubEnv("GH_AW_PI_TOOL_BUDGET_DIR", dir);
    vi.stubEnv("GH_AW_MAX_TOOL_CALLS", "2");
    const parent = {};
    const child = {};
    policyExtension({ on: (name, handler) => (parent[name] = handler) });
    policyExtension({ on: (name, handler) => (child[name] = handler) });
    try {
      expect(parent.tool_call({ toolName: "subagent" }, {})).toBeUndefined();
      expect(child.tool_call({ toolName: "read" }, {})).toBeUndefined();
      expect(parent.tool_call({ toolName: "read" }, {})).toMatchObject({ block: true });
      expect(child.tool_call({ toolName: "read" }, {})).toMatchObject({ block: true });
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
  it.each([
    "echo ok && curl example.test",
    "echo $(curl example.test)",
    "echo `curl example.test`",
    "echo ok & curl example.test",
    "echo ok >file",
    "(echo ok)",
    "if curl example.test; then echo ok; fi",
    "echo allowed # '\nprintf blocked\n# '",
  ])("refuses ambiguous or out-of-scope commands: %s", command => {
    expect(isPiBashAllowed(command, ["echo"])).toBe(false);
  });

  it("checks every pipeline segment and exact command prefixes", () => {
    expect(isPiBashAllowed("git status && echo 'a; b'", ["git status", "echo"])).toBe(true);
    expect(isPiBashAllowed("git status-other", ["git status"])).toBe(false);
    expect(isPiBashAllowed("git status | cat", ["git:*", "cat"])).toBe(true);
    expect(isPiBashAllowed("echo ok", false)).toBe(false);
    expect(isPiBashAllowed("git diff --output=example", ["git diff"])).toBe(false);
    expect(isPiBashAllowed("git diff --stat", ["git diff:*"])).toBe(true);
    expect(isPiBashAllowed("git diff --stat", ["git diff *"])).toBe(true);
    expect(isPiBashAllowed("echo hello", ["echo *"])).toBe(true);
  });

  it("reserves budgets synchronously for direct and nested dispatches", () => {
    vi.stubEnv("GH_AW_MAX_TOOL_CALLS", "2");
    vi.stubEnv("GH_AW_PI_TOOL_POLICY", "{}");
    const handlers = {};
    policyExtension({ on: (event, handler) => (handlers[event] = handler) });
    expect(handlers.tool_call({ toolName: "codemode" }, {})).toBeUndefined();
    expect(handlers.tool_call({ toolName: "bash", parentToolCallId: "outer", input: { command: "echo ok" } }, {})).toBeUndefined();
    expect(handlers.tool_call({ toolName: "read", input: {} }, {})).toMatchObject({ block: true });
  });

  it("blocks both file-editing tools", () => {
    vi.stubEnv("GH_AW_PI_TOOL_POLICY", '{"edit":false}');
    const handlers = {};
    policyExtension({ on: (event, handler) => (handlers[event] = handler) });
    for (const toolName of ["edit", "write"]) expect(handlers.tool_call({ toolName }, {})).toMatchObject({ block: true });
  });

  it("aborts inference at the configured repeated-denial threshold", () => {
    const originalExitCode = process.exitCode;
    vi.stubEnv("GH_AW_PI_TOOL_POLICY", '{"bash":false}');
    vi.stubEnv("GH_AW_MAX_TOOL_DENIALS", "2");
    const handlers = {};
    const abort = vi.fn();
    policyExtension({ on: (event, handler) => (handlers[event] = handler) });
    try {
      handlers.tool_call({ toolName: "bash", input: { command: "echo blocked" } }, { abort });
      expect(abort).not.toHaveBeenCalled();
      handlers.tool_call({ toolName: "bash", input: { command: "echo blocked" } }, { abort });
      expect(abort).toHaveBeenCalledOnce();
      expect(process.exitCode).toBe(1);
    } finally {
      process.exitCode = originalExitCode;
    }
  });
});
