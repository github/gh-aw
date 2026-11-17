// @ts-check

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("fs");
const os = require("os");
const path = require("path");
const { patchAWFChrootConfig } = require("./patch_awf_chroot_config.cjs");

describe("patchAWFChrootConfig", () => {
  let tempDir;
  let binariesDir;
  let runtimeDir;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "patch-awf-chroot-"));
    binariesDir = fs.mkdtempSync(path.join(os.tmpdir(), "patch-awf-binaries-"));
    runtimeDir = fs.mkdtempSync(path.join(os.tmpdir(), "patch-awf-runtime-"));
    vi.stubEnv("GH_AW_RUNTIME_DIR", runtimeDir);
    fs.mkdirSync(path.join(tempDir, "gh-aw"), { recursive: true });
    fs.writeFileSync(path.join(tempDir, "gh-aw", "awf-config.json"), JSON.stringify({ apiProxy: { enabled: true } }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(tempDir, { recursive: true, force: true });
    fs.rmSync(binariesDir, { recursive: true, force: true });
    fs.rmSync(runtimeDir, { recursive: true, force: true });
  });

  it("patches both AWF config locations with chroot settings", () => {
    vi.spyOn(os, "userInfo").mockReturnValue({
      username: "runner",
      uid: 1001,
      gid: 1002,
      homedir: "/home/runner",
      shell: "/bin/bash",
    });

    const output = patchAWFChrootConfig({
      runnerTemp: tempDir,
      binariesSourcePath: binariesDir,
      identityHome: "/tmp/gh-aw/home",
    });

    const expected = {
      apiProxy: { enabled: true },
      chroot: {
        binariesSourcePath: binariesDir,
        identity: {
          user: "runner",
          uid: 1001,
          gid: 1002,
          home: "/tmp/gh-aw/home",
        },
      },
    };

    expect(output).toBe(`${JSON.stringify(expected)}\n`);
    expect(fs.readFileSync(path.join(tempDir, "gh-aw", "awf-config.json"), "utf8")).toBe(output);
    expect(fs.readFileSync(path.join(binariesDir, "awf-config.json"), "utf8")).toBe(output);
  });

  it("fails when RUNNER_TEMP is unavailable", () => {
    vi.stubEnv("RUNNER_TEMP", "");
    expect(() => patchAWFChrootConfig()).toThrow("RUNNER_TEMP is required");
  });
});
