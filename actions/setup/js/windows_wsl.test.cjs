import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const require = createRequire(import.meta.url);
const childProcess = require("node:child_process");
const fs = require("node:fs");
const originalSpawnSync = childProcess.spawnSync;
const spawnSync = vi.fn();
childProcess.spawnSync = spawnSync;
delete require.cache[require.resolve("./windows_wsl.cjs")];
const { windowsToWSL, wslToWindows, wslEnvironment, runWSLScript, setupWindowsRuntime, cleanupWindowsRuntime } = require("./windows_wsl.cjs");
childProcess.spawnSync = originalSpawnSync;

describe("Windows WSL runtime", () => {
  let directory;
  let script;
  let env;

  beforeEach(() => {
    spawnSync.mockReset();
    directory = mkdtempSync(join(tmpdir(), "windows-wsl-"));
    mkdirSync(join(directory, "gh-aw"));
    script = join(directory, "script with 'quotes'.sh");
    writeFileSync(script, "\uFEFFprintf '%s\\n' \"$VALUE\"\r\n");
    env = { RUNNER_TEMP: directory };
    for (const name of ["GITHUB_ENV", "GITHUB_OUTPUT", "GITHUB_PATH"]) {
      env[name] = join(directory, name);
      writeFileSync(env[name], "");
    }
    spawnSync.mockReturnValue({ status: 0 });
  });

  afterEach(() => {
    rmSync(directory, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  it("translates paths with spaces and quotes without shell interpolation", () => {
    expect(windowsToWSL("D:\\a folder\\it's a script.sh")).toBe("/mnt/d/a folder/it's a script.sh");
    expect(windowsToWSL("C:/a folder/script.sh")).toBe("/mnt/c/a folder/script.sh");
    expect(wslToWindows("ROOT=/mnt/d/a folder\n")).toBe("ROOT=D:/a folder\n");
    expect(wslToWindows("ROOT=/tmp/gh-aw\n")).toBe("ROOT=/tmp/gh-aw\n");
  });

  it("forwards values as environment data and translates Windows path variables", () => {
    const environment = wslEnvironment({
      PATH: "C:\\Windows",
      HOME: "C:\\Users\\runner",
      WSLENV: "PATH/p",
      GITHUB_OUTPUT: "D:\\a folder\\output",
      RUNNER_TEMP: "D:/a/_temp",
      VALUE: "$(touch /tmp/not-executed); 'quoted'",
      CONFIG: '{"path":"/tmp/gh-aw"}',
    });
    expect(environment.WSLENV).toBe("GITHUB_OUTPUT/up:RUNNER_TEMP/up:VALUE/u:CONFIG/u");
    expect(environment.VALUE).toBe("$(touch /tmp/not-executed); 'quoted'");
  });

  it("runs the script in Linux Bash with strict failure handling", () => {
    spawnSync.mockImplementation((_command, args, options) => {
      expect(args.slice(0, 6)).toEqual(["--distribution", "Ubuntu-24.04", "--user", "gh-aw", "--exec", "bash"]);
      expect(args).toContain("pipefail");
      expect(args.at(-1)).toBe(`${script}.wsl.sh`);
      expect(readFileSync(`${script}.wsl.sh`, "utf8")).toBe("printf '%s\\n' \"$VALUE\"\n");
      expect(options.env.VALUE).toBe("untrusted 'value'");
      expect(args.join(" ")).not.toContain("untrusted");
      return { status: 17 };
    });
    expect(runWSLScript(script, { ...env, VALUE: "untrusted 'value'" })).toBe(17);
    expect(existsSync(`${script}.wsl.sh`)).toBe(false);
  });

  it("returns outputs to native actions and keeps Linux PATH additions inside WSL", () => {
    spawnSync.mockImplementation((_command, _args, options) => {
      writeFileSync(env.GITHUB_ENV, "ROOT=/mnt/d/a folder\n");
      writeFileSync(env.GITHUB_OUTPUT, "config=/mnt/d/a/_temp/config.json\n");
      writeFileSync(options.env.GITHUB_PATH, "/home/gh-aw/.local/bin\n/mnt/d/tools/bin\n");
      return { status: 0 };
    });
    expect(runWSLScript(script, env)).toBe(0);
    expect(readFileSync(env.GITHUB_ENV, "utf8")).toBe("ROOT=D:/a folder\n");
    expect(readFileSync(env.GITHUB_OUTPUT, "utf8")).toBe("config=D:/a/_temp/config.json\n");
    expect(readFileSync(join(directory, "gh-aw", "wsl-path"), "utf8")).toBe("/home/gh-aw/.local/bin\n/mnt/d/tools/bin\n");
    expect(readFileSync(env.GITHUB_PATH, "utf8")).toBe("D:/tools/bin\n");
  });

  it("surfaces startup failures and removes temporary scripts", () => {
    spawnSync.mockReturnValue({ error: new Error("wsl.exe unavailable") });
    expect(() => runWSLScript(script, env)).toThrow("wsl.exe unavailable");
    expect(existsSync(`${script}.wsl.sh`)).toBe(false);
    expect(existsSync(`${script}.wsl.sh.path`)).toBe(false);
  });

  it("stops setup if the WSL distribution cannot be installed", () => {
    spawnSync.mockReturnValueOnce({ status: 0, stdout: "" }).mockReturnValueOnce({ status: 1 });
    expect(() => setupWindowsRuntime()).toThrow("WSL command failed");
    expect(spawnSync.mock.calls[1][1]).toEqual(["--install", "--distribution", "Ubuntu-24.04", "--no-launch"]);
    expect(spawnSync).toHaveBeenCalledTimes(2);
  });

  it("provisions Linux prerequisites before runtime scripts or image pulls", () => {
    spawnSync.mockReturnValueOnce({ status: 0, stdout: "Ubuntu-24.04\r\n" }).mockReturnValue({ status: 0 });
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "readlinkSync").mockReturnValue("\\\\wsl.localhost\\Ubuntu-24.04\\tmp\\gh-aw");
    setupWindowsRuntime();
    expect(spawnSync).toHaveBeenCalledTimes(3);
    const [_command, args, options] = spawnSync.mock.calls[2];
    expect(args.slice(0, 5)).toEqual(["--distribution", "Ubuntu-24.04", "--user", "root", "--exec"]);
    expect(options.input).toContain("docker.io docker-compose-v2");
    expect(options.input).toContain("service docker start");
    expect(options.input).toContain("docker info --format '{{.OSType}}'");
    expect(options.input).toContain("sha256sum --check checksum");
    expect(options.input).toContain("runuser -u gh-aw -- /usr/local/bin/npm config set prefix /home/gh-aw/.local");
  });

  it("refuses to replace an unrelated host runtime tree", () => {
    spawnSync.mockReturnValue({ status: 0, stdout: "Ubuntu-24.04\r\n" });
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "readlinkSync").mockReturnValue("unrelated target");
    const symlink = vi.spyOn(fs, "symlinkSync");
    expect(() => setupWindowsRuntime()).toThrow("Refusing to replace existing runtime path");
    expect(symlink).not.toHaveBeenCalled();
  });

  it("cleans the Linux runtime tree without invoking a host shell", () => {
    cleanupWindowsRuntime();
    expect(spawnSync).toHaveBeenCalledWith("wsl.exe", ["--distribution", "Ubuntu-24.04", "--user", "root", "--exec", "rm", "-rf", "--", "/tmp/gh-aw"], { stdio: "inherit", timeout: 900_000 });
  });
});
