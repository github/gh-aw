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
    expect(RpcClient).toHaveBeenCalledWith(expect.objectContaining({ args: expect.arrayContaining(["--no-approve", path.join(dir, "gh-aw/actions/pi_tool_policy.cjs")]) }));
    expect(client.waitForIdle).not.toHaveBeenCalled();
    expect(client.stop).toHaveBeenCalledOnce();
  });
});
