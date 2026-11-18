// @ts-check
import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";
import crypto from "node:crypto";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const source = fs.readFileSync(new URL("./upload_assets.cjs", import.meta.url), "utf8");

describe("standalone asset authorization boundary", () => {
  async function execute(authorize) {
    const bytes = Buffer.from("prepared image");
    const sha = crypto.createHash("sha256").update(bytes).digest("hex");
    const batch = [{ type: "noop" }, { type: "upload_asset", fileName: "image.png", sha, targetFileName: `${sha}.png` }];
    const core = {
      info: vi.fn(),
      warning: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() },
    };
    const exec = { exec: vi.fn().mockResolvedValue(0), getExecOutput: vi.fn().mockResolvedValue({ exitCode: 0 }) };
    const module = { exports: {} };
    vm.runInNewContext(source, {
      module,
      Buffer,
      URL,
      core,
      exec,
      context: {},
      process: {
        env: {
          GH_AW_ASSETS_BRANCH: "assets/test",
          GH_AW_ASSETS_DIR: "/virtual/staging",
          GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE: '{"type":"upload_asset"}',
          GITHUB_REPOSITORY: "owner/repo",
        },
      },
      require(name) {
        if (name === "fs") return { existsSync: () => false };
        if (name === "./load_agent_output.cjs") return { loadAgentOutput: () => ({ success: true, items: batch }) };
        if (name === "./symlink_guard.cjs") return { readGuardedFile: () => bytes, writeGuardedFile: vi.fn() };
        if (name === "./safe_output_authorization_gate.cjs") return { main: authorize, recordExecutorOutcome: vi.fn() };
        return require(name);
      },
    });
    await module.exports.main();
    return { core, exec, sha };
  }

  it("binds prepared content and destination, then blocks publication when denied", async () => {
    const authorize = vi.fn().mockRejectedValue(new Error("authorization denied"));
    const { core, exec, sha } = await execute(authorize);
    expect(authorize).toHaveBeenCalledOnce();
    expect(authorize.mock.calls[0][0].records).toMatchObject([
      {
        index: 1,
        inputs: { repository: "owner/repo", branch: "assets/test", sha256: sha, targetFileName: `${sha}.png` },
        artifacts: [{ digest: sha }],
      },
    ]);
    expect(exec.getExecOutput).not.toHaveBeenCalled();
    expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("authorization denied"));
  });

  it("requires an executable decision instead of accepting an empty or staged release", async () => {
    const { core, exec } = await execute(vi.fn().mockResolvedValue({ state: "not_requested" }));
    expect(exec.getExecOutput).not.toHaveBeenCalled();
    expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("did not release"));
  });

  it("enters publication only after the prepared batch has been authorized", async () => {
    const { core, exec } = await execute(vi.fn().mockResolvedValue({ state: "allowed" }));
    expect(core.setFailed).not.toHaveBeenCalled();
    expect(exec.getExecOutput).toHaveBeenCalledWith("git", ["push", "--porcelain", "origin", "assets/test"], { ignoreReturnCode: true });
  });
});
