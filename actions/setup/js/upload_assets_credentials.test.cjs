import { describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

describe("asset publication credential", () => {
  it("fails closed before running git when the selected token is empty", async () => {
    const previous = { ...process.env };
    const core = { setFailed: vi.fn(), info: vi.fn() };
    const exec = { exec: vi.fn(), getExecOutput: vi.fn() };
    try {
      process.env.GH_AW_ASSET_GIT_AUTH_REQUIRED = "true";
      delete process.env.GH_AW_ASSET_GIT_TOKEN;
      const require = createRequire(import.meta.url);
      const script = fs.readFileSync(path.join(__dirname, "upload_assets.cjs"), "utf8");
      await eval(`(async () => { ${script}; await main(); })()`);
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("Asset publication credential is required"));
      expect(exec.exec).not.toHaveBeenCalled();
      expect(exec.getExecOutput).not.toHaveBeenCalled();
    } finally {
      process.env = previous;
    }
  });

  it("allows staged publication without a credential", async () => {
    const previous = { ...process.env };
    const core = { setFailed: vi.fn(), info: vi.fn() };
    try {
      process.env.GH_AW_ASSET_GIT_AUTH_REQUIRED = "true";
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      delete process.env.GH_AW_ASSET_GIT_TOKEN;
      delete process.env.GH_AW_ASSETS_BRANCH;
      const require = createRequire(import.meta.url);
      const script = fs.readFileSync(path.join(__dirname, "upload_assets.cjs"), "utf8");
      await eval(`(async () => { ${script}; await main(); })()`);
      expect(core.setFailed).toHaveBeenCalledWith(expect.stringContaining("GH_AW_ASSETS_BRANCH"));
    } finally {
      process.env = previous;
    }
  });
});
