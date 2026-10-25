import { describe, it, expect, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { buildCopilotSDKDriverEnv } = require("./copilot_sdk_driver_env.cjs");

describe("buildCopilotSDKDriverEnv", () => {
  it("leaves legacy installations unchanged", () => {
    const env = { GITHUB_WORKSPACE: "/nonexistent-workspace", PATH: "/usr/bin" };
    expect(buildCopilotSDKDriverEnv(env)).toBe(env);
  });

  it("does not load a workspace resolver without explicit trust", () => {
    const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-sdk-untrusted-"));
    try {
      fs.mkdirSync(path.join(workspace, ".gh-aw", "copilot-sdk"), { recursive: true });
      fs.writeFileSync(path.join(workspace, ".gh-aw", "copilot-sdk", "resolve.cjs"), "");
      const env = { GITHUB_WORKSPACE: workspace, PATH: "/usr/bin", NODE_OPTIONS: "--trace-warnings" };
      const logger = vi.fn();

      expect(buildCopilotSDKDriverEnv(env, logger)).toBe(env);
      expect(logger).toHaveBeenCalledWith("Skipping workspace Copilot SDK resolver preload because GH_AW_TRUSTED_SDK_RESOLVER is not enabled");
    } finally {
      fs.rmSync(workspace, { recursive: true, force: true });
    }
  });

  it("adds SDK hooks and executable paths only to a copy of the driver environment", () => {
    const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-sdk workspace-"));
    try {
      const sdkDir = path.join(workspace, ".gh-aw", "copilot-sdk");
      fs.mkdirSync(sdkDir, { recursive: true });
      const resolver = path.join(sdkDir, "resolve.cjs");
      fs.writeFileSync(resolver, "");
      const baseEnv = {
        GITHUB_WORKSPACE: workspace,
        PATH: "/usr/bin",
        NODE_PATH: "/existing/modules",
        NODE_OPTIONS: "--trace-warnings",
        GH_AW_TRUSTED_SDK_RESOLVER: "1",
      };

      const driverEnv = buildCopilotSDKDriverEnv(baseEnv);

      expect(driverEnv).toMatchObject({
        NODE_OPTIONS: `--trace-warnings --import=${JSON.stringify(resolver)}`,
        NODE_PATH: `${path.join(sdkDir, "node_modules")}${path.delimiter}/existing/modules`,
        PATH: `${path.join(sdkDir, "node_modules", ".bin")}${path.delimiter}/usr/bin`,
      });
      expect(baseEnv).toEqual({
        GITHUB_WORKSPACE: workspace,
        PATH: "/usr/bin",
        NODE_PATH: "/existing/modules",
        NODE_OPTIONS: "--trace-warnings",
        GH_AW_TRUSTED_SDK_RESOLVER: "1",
      });
      expect(buildCopilotSDKDriverEnv({ GITHUB_WORKSPACE: workspace, GH_AW_TRUSTED_SDK_RESOLVER: "1" })).toMatchObject({
        NODE_OPTIONS: `--import=${JSON.stringify(resolver)}`,
        NODE_PATH: path.join(sdkDir, "node_modules"),
        PATH: `${path.join(sdkDir, "node_modules", ".bin")}${path.delimiter}`,
      });
    } finally {
      fs.rmSync(workspace, { recursive: true, force: true });
    }
  });
});
