import { afterEach, describe, expect, it, vi } from "vitest";

const { default: policyExtension, isPiBashAllowed } = await import("./pi_tool_policy.cjs");

afterEach(() => vi.unstubAllEnvs());

describe("Pi tool policy", () => {
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
