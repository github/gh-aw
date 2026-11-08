import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { main, rpcArgs } = await import("./pi_rpc_driver.cjs");
let dir;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-rpc-"));
  vi.stubEnv("PI_CODING_AGENT_DIR", path.join(dir, "agent"));
  vi.stubEnv("RUNNER_TEMP", dir);
  vi.stubEnv("GH_AW_PROMPT", path.join(dir, "prompt.txt"));
  vi.stubEnv("GH_AW_PI_CONFIG", "{}");
  vi.stubEnv("GH_AW_PI_ARGS", '["--print","--mode","json","--no-session","--no-approve"]');
  fs.writeFileSync(path.join(dir, "prompt.txt"), "Complete the task");
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Pi RPC driver", () => {
  it("uses the restored native session and rejects a substituted conversation before prompting", async () => {
    const sessionDirectory = path.join(dir, "durable-sessions/pi");
    fs.mkdirSync(sessionDirectory, { recursive: true });
    const sessionFile = path.join(sessionDirectory, "session.jsonl");
    const id = "00000000-0000-4000-8000-000000000001";
    fs.writeFileSync(sessionFile, JSON.stringify({ type: "session", version: 3, id, timestamp: new Date().toISOString(), cwd: dir }) + "\n");
    vi.stubEnv("GH_AW_DURABLE_STORAGE", "artifact");
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "pi");
    vi.stubEnv("GH_AW_DURABLE_SESSION_DIR", sessionDirectory);
    vi.stubEnv("GH_AW_DURABLE_RESUMED", "true");
    const client = { onEvent: vi.fn(() => vi.fn()), start: vi.fn(), getState: vi.fn(async () => ({ sessionId: id })), prompt: vi.fn(async () => "handled"), waitForIdle: vi.fn(), stop: vi.fn(), getStderr: () => "" };
    const RpcClient = vi.fn(function () {
      return client;
    });
    await main({ sdk: { RpcClient }, cliPath: "/pi/dist/bundle/cli.js", emit: vi.fn() });
    const { args } = RpcClient.mock.calls[0][0];
    expect(args).not.toContain("--no-session");
    expect(args[args.indexOf("--session") + 1]).toBe(sessionFile);
    expect(client.prompt).toHaveBeenCalledWith("Complete the task");
    client.getState.mockResolvedValue({ sessionId: "00000000-0000-4000-8000-000000000002" });
    await expect(main({ sdk: { RpcClient }, cliPath: "/pi/dist/bundle/cli.js", emit: vi.fn() })).rejects.toThrow("exact durable session");
    expect(client.prompt).toHaveBeenCalledTimes(1);
    expect(client.stop).toHaveBeenCalledTimes(2);
  });

  it("keeps session and policy options without overriding RPC mode", () => {
    expect(rpcArgs(["--print", "--mode", "json", "--no-session", "--thinking", "high"])).toEqual(["--no-session", "--thinking", "high"]);
  });

  it("handles settlement before prompt acceptance and always stops the client", async () => {
    let handler;
    const client = {
      onEvent: vi.fn(fn => {
        handler = fn;
        return vi.fn();
      }),
      start: vi.fn(),
      getState: vi.fn(async () => ({ sessionId: "rpc-session" })),
      prompt: vi.fn(async () => {
        handler({ type: "agent_settled" });
        return "started";
      }),
      waitForIdle: vi.fn(),
      stop: vi.fn(),
      getStderr: () => "",
    };
    const RpcClient = vi.fn(function () {
      return client;
    });
    await main({ sdk: { RpcClient }, cliPath: "/pi/dist/bundle/cli.js", emit: vi.fn() });
    expect(RpcClient).toHaveBeenCalledWith(expect.objectContaining({ args: expect.arrayContaining(["--no-approve", path.join(dir, "gh-aw/actions/pi_tool_policy.cjs"), path.join(dir, "gh-aw/actions/pi_subagent_extension.cjs")]) }));
    expect(client.waitForIdle).not.toHaveBeenCalled();
    expect(client.stop).toHaveBeenCalledOnce();
  });

  it("passes the staged workflow skills to the bare RPC child", async () => {
    vi.stubEnv("GH_AW_PI_BARE", "true");
    vi.stubEnv("GH_AW_PI_STAGING_DIR", path.join(dir, "staging"));
    vi.stubEnv("GH_AW_PI_ARGS", '["--print","--mode","json","--no-session","--no-approve","--no-skills"]');
    fs.mkdirSync(path.join(dir, "staging/skills/reporting"), { recursive: true });
    fs.writeFileSync(path.join(dir, "staging/skills/reporting/SKILL.md"), "Report.");
    fs.writeFileSync(path.join(dir, "staging/skills/.gh-aw-inline-skills.json"), '["reporting"]');
    const client = { onEvent: vi.fn(() => vi.fn()), start: vi.fn(), getState: vi.fn(async () => ({ sessionId: "rpc-session" })), prompt: vi.fn(async () => "handled"), waitForIdle: vi.fn(), stop: vi.fn(), getStderr: () => "" };
    const RpcClient = vi.fn(function () {
      return client;
    });
    await main({ sdk: { RpcClient }, cliPath: "/pi/dist/bundle/cli.js", emit: vi.fn() });
    const { args } = RpcClient.mock.calls[0][0];
    expect(args.slice(0, 5)).toEqual(["--no-session", "--no-approve", "--no-skills", "--skill", path.join(dir, "agent/skills/reporting/SKILL.md")]);
  });
});
