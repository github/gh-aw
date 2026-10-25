// @ts-check
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn, spawnSync } from "node:child_process";
import http from "node:http";
import { collect, collectTree, guestEnvironment, readELFInterpreter, stage as stageAssets } from "./nvx_assets.cjs";
import { resolveCopilotSDKMCPConfigPath } from "./copilot_sdk_mcp_config_path.cjs";
import { COPILOT_SDK_MCP_CONFIG_SHA256, digestCopilotSDKMCPConfig } from "./copilot_sdk_mcp_config.cjs";

describe.skipIf(process.platform === "darwin" || (process.platform === "linux" && !fs.existsSync("/usr/bin/patchelf")))("NVX compiler asset handoff (Linux requires patchelf)", () => {
  /** @type {string} */
  let temp;
  /** @type {string} */
  let workspace;
  /** @type {string} */
  let hostRoot;
  /** @type {string} */
  let state;
  /** @type {NodeJS.ProcessEnv} */
  let environment;

  /** @param {NodeJS.ProcessEnv} env @param {string} stateRoot */
  function stage(env, stateRoot) {
    stageAssets(env, stateRoot, path.join(workspace, ".gh-aw-nvx"));
  }

  beforeEach(() => {
    temp = fs.mkdtempSync(path.join(os.tmpdir(), "nvx-assets-"));
    workspace = path.join(temp, "workspace with spaces");
    hostRoot = path.join(temp, "runner-temp", "gh-aw");
    state = path.join(temp, "state");
    for (const dir of [workspace, path.join(hostRoot, "actions"), path.join(state, "aw-prompts")]) fs.mkdirSync(dir, { recursive: true });
    fs.copyFileSync(path.join(import.meta.dirname, "nvx_assets.cjs"), path.join(hostRoot, "actions", "nvx_assets.cjs"));
    fs.copyFileSync(path.join(import.meta.dirname, "copilot_sdk_mcp_config_path.cjs"), path.join(hostRoot, "actions", "copilot_sdk_mcp_config_path.cjs"));
    fs.writeFileSync(path.join(state, "aw-prompts", "prompt.txt"), "fixture prompt");
    fs.writeFileSync(path.join(hostRoot, "provider-token"), "must not be exported");
    fs.writeFileSync(path.join(state, "gateway-secret"), "must not be exported");
    environment = { GITHUB_WORKSPACE: workspace, RUNNER_TEMP: path.dirname(hostRoot) };
  });

  afterEach(() => fs.rmSync(temp, { recursive: true, force: true }));

  it("exports only runtime assets and selected inputs", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    expect(fs.readFileSync(path.join(root, "state", "aw-prompts", "prompt.txt"), "utf8")).toBe("fixture prompt");
    expect(fs.existsSync(path.join(root, "gh-aw", "bin", "node"))).toBe(true);
    expect(fs.existsSync(path.join(root, "gh-aw", "provider-token"))).toBe(false);
    expect(fs.existsSync(path.join(root, "state", "gateway-secret"))).toBe(false);
    expect(fs.existsSync(path.join(root, "state", "github-event.json"))).toBe(false);
  });

  it("exports the unchanged inline webhook event into guest state without widening file permissions", () => {
    const event = JSON.stringify({
      action: "gh-aw-webhook-example",
      client_payload: { schema_version: 1, event: "projects_v2_item", delivery_id: "delivery-example", payload: { action: "edited" } },
    });
    fs.writeFileSync(path.join(state, "github-event.json"), event, { mode: 0o600 });

    stage(environment, state);

    const staged = path.join(workspace, ".gh-aw-nvx", "state", "github-event.json");
    expect(fs.readFileSync(staged, "utf8")).toBe(event);
    expect(fs.statSync(staged).mode & 0o777).toBe(0o600);
  });

  it("rejects a redirected webhook event input", () => {
    fs.symlinkSync(path.join(state, "gateway-secret"), path.join(state, "github-event.json"));
    expect(() => stage(environment, state)).toThrow(/must not be a symlink/);
  });

  it("selects the separately staged guest Copilot variant", () => {
    fs.mkdirSync(path.join(hostRoot, "bin"));
    fs.writeFileSync(path.join(hostRoot, "bin", "copilot"), "host-glibc-copilot", { mode: 0o755 });
    fs.mkdirSync(path.join(hostRoot, "nvx-engine"));
    fs.writeFileSync(path.join(hostRoot, "nvx-engine", "copilot"), "guest-musl-copilot", { mode: 0o755 });

    stage(environment, state);

    const stagedCopilot = path.join(workspace, ".gh-aw-nvx", "gh-aw", "bin", "copilot");
    expect(fs.readFileSync(stagedCopilot, "utf8")).toBe("guest-musl-copilot");
    expect(fs.statSync(stagedCopilot).mode & 0o111).not.toBe(0);
    expect(fs.existsSync(path.join(hostRoot, "nvx-engine"))).toBe(false);
  });

  it("exports only the selected native MCP config at its remapped guest path", () => {
    const dir = path.join(hostRoot, "mcp-config");
    fs.mkdirSync(dir);
    const config = path.join(dir, "config.toml");
    fs.writeFileSync(config, '[mcp_servers.fixture]\nurl = "http://gateway:80"\n');
    fs.writeFileSync(path.join(dir, "provider-token"), "must not be exported");
    environment.GH_AW_MCP_CONFIG = config;
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const guestEnv = guestEnvironment({ ...environment, GH_AW_NVX_HOST_ROOT: hostRoot }, root);
    expect(guestEnv.GH_AW_MCP_CONFIG).toBe(path.join(root, "gh-aw", "mcp-config", "config.toml"));
    expect(fs.readFileSync(guestEnv.GH_AW_MCP_CONFIG || "", "utf8")).toContain("[mcp_servers.fixture]");
    expect(fs.existsSync(path.join(root, "gh-aw", "mcp-config", "provider-token"))).toBe(false);
  });

  it("stages the converter-owned SDK catalog at its derived guest path without copying ambient CLI or gateway files", () => {
    const sdkPath = resolveCopilotSDKMCPConfigPath(environment.RUNNER_TEMP);
    fs.mkdirSync(path.dirname(sdkPath), { recursive: true });
    fs.writeFileSync(sdkPath, '{"mcpServers":{"safeoutputs":{"type":"http","url":"http://gateway:80/mcp/safeoutputs"}}}');
    Object.assign(environment, { GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "1", GITHUB_JOB: "agent" });
    environment[COPILOT_SDK_MCP_CONFIG_SHA256] = digestCopilotSDKMCPConfig(fs.readFileSync(sdkPath), environment);
    fs.writeFileSync(path.join(path.dirname(sdkPath), "gateway-output.json"), "must not be exported");
    const cliPath = path.join(temp, "ambient-cli.json");
    fs.writeFileSync(cliPath, "must not be exported");
    environment.GH_AW_MCP_CONFIG = cliPath;

    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const guestEnv = guestEnvironment({ ...environment, GH_AW_NVX_HOST_ROOT: hostRoot }, root);
    const guestPath = resolveCopilotSDKMCPConfigPath(guestEnv.RUNNER_TEMP);
    expect(guestPath).toBe(path.join(root, "gh-aw", "mcp-config", "copilot-sdk.json"));
    expect(fs.readFileSync(guestPath, "utf8")).toBe(fs.readFileSync(sdkPath, "utf8"));
    expect(digestCopilotSDKMCPConfig(fs.readFileSync(guestPath), guestEnv)).toBe(guestEnv[COPILOT_SDK_MCP_CONFIG_SHA256]);
    expect(fs.existsSync(path.join(root, "gh-aw", "mcp-config", "gateway-output.json"))).toBe(false);
    expect(fs.existsSync(path.join(root, "gh-aw", "mcp-config", "ambient-cli.json"))).toBe(false);
  });

  it("rejects a redirected native MCP config directory", () => {
    fs.writeFileSync(path.join(state, "config.toml"), "must not be exported");
    fs.symlinkSync(state, path.join(hostRoot, "mcp-config"));
    environment.GH_AW_MCP_CONFIG = path.join(hostRoot, "mcp-config", "config.toml");
    expect(() => stage(environment, state)).toThrow(/MCP config directory must not be a symlink/);
    collect(environment, state);
    expect(fs.readFileSync(path.join(state, "config.toml"), "utf8")).toBe("must not be exported");
  });

  it("distinguishes ELF executables from scripts and shared libraries", () => {
    const file = path.join(temp, "elf-fixture");
    fs.writeFileSync(file, "#!/bin/sh\nexit 0\n");
    expect(readELFInterpreter(file)).toBeUndefined();
    const elf = Buffer.alloc(256);
    Buffer.from([0x7f, 0x45, 0x4c, 0x46, 2, 1]).copy(elf);
    elf.writeBigUInt64LE(64n, 32);
    elf.writeUInt16LE(56, 54);
    elf.writeUInt16LE(1, 56);
    elf.writeUInt32LE(3, 64);
    elf.writeBigUInt64LE(128n, 72);
    const interpreter = "/lib64/ld-linux-x86-64.so.2\0";
    elf.writeBigUInt64LE(BigInt(Buffer.byteLength(interpreter)), 96);
    elf.write(interpreter, 128);
    fs.writeFileSync(file, elf);
    expect(readELFInterpreter(file)).toBe("/lib64/ld-linux-x86-64.so.2");
    elf.writeUInt32LE(1, 64);
    fs.writeFileSync(file, elf);
    expect(readELFInterpreter(file)).toBeUndefined();
  });

  it.skipIf(process.platform !== "linux")("exports native CLI loaders and keeps Node subprocesses on the portable entrypoint", () => {
    fs.mkdirSync(path.join(hostRoot, "bin"));
    fs.copyFileSync("/bin/echo", path.join(hostRoot, "bin", "copilot"));
    stage(environment, state);
    const runtime = path.join(workspace, ".gh-aw-nvx", "gh-aw");
    const copilot = path.join(runtime, "bin", "copilot");
    const interpreter = readELFInterpreter(copilot);
    expect(interpreter?.startsWith(`${runtime}/lib/`)).toBe(true);
    expect(fs.existsSync(interpreter || "")).toBe(true);
    const cliResult = spawnSync(copilot, ["literal $HOME; $(exit 1)"], { encoding: "utf8" });
    expect(cliResult.status, cliResult.stderr).toBe(0);
    expect(cliResult.stdout.trim()).toBe("literal $HOME; $(exit 1)");
    const node = path.join(runtime, "bin", "node");
    const result = spawnSync(node, ["-e", 'const cp = require("node:child_process"); const child = cp.spawnSync(process.execPath, ["-e", "process.exit(17)"]); if (child.error) throw child.error; process.exit(child.status);'], {
      encoding: "utf8",
    });
    expect(result.signal, result.stderr).toBeNull();
    expect(result.status, result.stderr).toBe(17);
  });

  it("rejects pre-existing checkout-controlled staging directories and symlinks", () => {
    const root = path.join(workspace, ".gh-aw-nvx");
    fs.symlinkSync(state, root);
    expect(() => stage(environment, state)).toThrow(/EEXIST/);
    expect(fs.readFileSync(path.join(state, "aw-prompts", "prompt.txt"), "utf8")).toBe("fixture prompt");
  });

  it("rejects asset symlinks escaping the exported package", () => {
    fs.symlinkSync("../provider-token", path.join(hostRoot, "actions", "escape"));
    expect(() => stage(environment, state)).toThrow(/symlink escapes/);
    collect(environment, state);
    expect(fs.existsSync(path.join(workspace, ".gh-aw-nvx"))).toBe(false);
  });

  it("preserves confined relative npm bin symlinks", () => {
    const cli = path.join(hostRoot, "engine-cli");
    fs.mkdirSync(path.join(cli, "bin"), { recursive: true });
    fs.mkdirSync(path.join(cli, "node_modules"), { recursive: true });
    fs.writeFileSync(path.join(cli, "node_modules", "cli.js"), "fixture");
    fs.symlinkSync("../node_modules/cli.js", path.join(cli, "bin", "fixture"));
    stage(environment, state);
    expect(fs.readFileSync(path.join(workspace, ".gh-aw-nvx", "gh-aw", "engine-cli", "bin", "fixture"), "utf8")).toBe("fixture");
  });

  it.each([undefined, "/opt/toolcache"])("runs the staged runtime without host runner assets (tool cache %s)", toolCache => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const stateLink = path.join(temp, "guest-state");
    const bootstrap = path.join(root, "gh-aw", "actions", "nvx_assets.cjs");
    const node = path.join(root, "gh-aw", "bin", "node");
    fs.writeFileSync(
      path.join(root, "gh-aw", "actions", "fixture_harness.cjs"),
      `
const fs = require("node:fs");
if (!fs.readFileSync(process.env.GH_AW_PROMPT, "utf8").includes("fixture prompt")) process.exit(2);
fs.mkdirSync(require("node:path").dirname(process.env.GH_AW_OUTPUT), { recursive: true });
fs.writeFileSync(process.env.GH_AW_OUTPUT, "guest output");
process.exit(17);
`
    );
    const harness = path.join(root, "gh-aw", "actions", "fixture_harness.cjs");
    const command = `"$GH_AW_NODE_BIN" ${JSON.stringify(harness)}`;
    const result = spawnSync(node, ["-e", `require(${JSON.stringify(bootstrap)}).guest(${JSON.stringify(command)}, ${JSON.stringify(root)}, ${JSON.stringify(stateLink)})`], {
      env: {
        PATH: "/usr/bin:/bin",
        HOME: path.join(temp, "guest-home"),
        GH_AW_PROMPT: path.join(stateLink, "aw-prompts", "prompt.txt"),
        GH_AW_OUTPUT: path.join(stateLink, "agent", "result.txt"),
        ...(toolCache ? { RUNNER_TOOL_CACHE: toolCache } : {}),
      },
      encoding: "utf8",
    });
    expect(result.error).toBeUndefined();
    expect(result.signal, result.stderr).toBeNull();
    expect(result.status, result.stderr).toBe(17);
    collect(environment, state);
    expect(fs.readFileSync(path.join(state, "agent", "result.txt"), "utf8")).toBe("guest output");
    expect(fs.existsSync(root)).toBe(false);
    expect(fs.existsSync(path.join(hostRoot, "nvx-assets.json"))).toBe(false);
  });

  it("rewrites path-valued environment without rewriting credentials or endpoint values", () => {
    const env = guestEnvironment({
      HOME: "/home/awf",
      PATH: "/usr/local/go/bin:/usr/bin",
      GH_AW_NVX_HOST_ROOT: "/runner/temp/gh-aw",
      GH_AW_NVX_HOST_WORKSPACE: "/runner/repo",
      GH_AW_NVX_HOST_HOME: "/home/runner",
      GH_AW_MCP_CONFIG: "/home/runner/.copilot/mcp-config.json",
      DRIVER_PATH: "/runner/repo/driver.py",
      GH_AW_NODE_BIN: "/opt/hostedtoolcache/node/bin/node",
      GH_AW_SAFE_OUTPUTS: "/runner/temp/gh-aw/safeoutputs/result.json",
      ENDPOINT: "https://example.com/runner/repo",
    });
    expect(env.RUNNER_TEMP).toBe("/workspace/.gh-aw-nvx");
    expect(env.GH_AW_MCP_CONFIG).toBe("/home/awf/.copilot/mcp-config.json");
    expect(env.DRIVER_PATH).toBe("/workspace/driver.py");
    expect(env.GH_AW_SAFE_OUTPUTS).toBe("/workspace/.gh-aw-nvx/gh-aw/safeoutputs/result.json");
    expect(env.GH_AW_NODE_BIN).toBe("/workspace/.gh-aw-nvx/gh-aw/bin/node");
    expect(env.PATH).toContain("/usr/local/go/bin");
    expect(env.ENDPOINT).toBe("https://example.com/runner/repo");
    expect(env.RUNNER_TOOL_CACHE).toBeUndefined();
    expect(env.GH_AW_NVX_HOST_ROOT).toBeUndefined();
  });

  it("refuses guest symlinks during output collection and still removes runtime assets", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    fs.mkdirSync(path.join(root, "state", "agent"));
    fs.symlinkSync(path.join(hostRoot, "provider-token"), path.join(root, "state", "agent", "escape"));
    expect(() => collect(environment, state)).toThrow(/regular file or directory/);
    expect(fs.existsSync(root)).toBe(false);
    expect(fs.readFileSync(path.join(hostRoot, "provider-token"), "utf8")).toBe("must not be exported");
  });

  it("collects only expected artifacts without overwriting host scripts or other state", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    for (const name of ["agent", "threat-detection", "sandbox/agent/logs", "actions-source/actions/setup", "cache-memory"]) {
      fs.mkdirSync(path.join(root, "state", name), { recursive: true });
      fs.writeFileSync(path.join(root, "state", name, "fixture.txt"), "guest artifact");
    }
    fs.mkdirSync(path.join(state, "actions-source", "actions", "setup"), { recursive: true });
    const cleanup = path.join(state, "actions-source", "actions", "setup", "clean.sh");
    fs.writeFileSync(cleanup, "trusted cleanup");
    fs.writeFileSync(path.join(root, "state", "actions-source", "actions", "setup", "clean.sh"), "untrusted cleanup");
    fs.writeFileSync(path.join(root, "state", "gateway-secret"), "untrusted secret");
    fs.writeFileSync(path.join(root, "state", "agent", "harness_retry.json"), '{"attempts":1}');
    fs.writeFileSync(path.join(root, "state", "pi-streaming.jsonl"), "pi events");
    fs.writeFileSync(path.join(root, "state", "sandbox/agent/logs/pi-streaming.jsonl"), "writable pi events");
    fs.writeFileSync(path.join(root, "state", "claude-debug.log"), "claude events");
    fs.writeFileSync(path.join(root, "state", "agent-step-summary.md"), "agent summary");
    fs.writeFileSync(path.join(root, "state", "step-summary.md"), "detector summary");
    const safeoutputs = path.join(root, "gh-aw", "safeoutputs");
    fs.writeFileSync(path.join(safeoutputs, "outputs.jsonl"), '{"type":"noop"}\n');
    fs.writeFileSync(path.join(safeoutputs, "unexpected.json"), "untrusted file");
    fs.mkdirSync(path.join(safeoutputs, "upload-artifacts"));
    fs.writeFileSync(path.join(safeoutputs, "upload-artifacts", "result.txt"), "upload artifact");
    fs.mkdirSync(path.join(hostRoot, "safeoutputs"));
    fs.writeFileSync(path.join(hostRoot, "safeoutputs", "config.json"), '{"max":1}');
    collect(environment, state);
    for (const name of ["agent", "threat-detection", "sandbox/agent/logs"]) {
      expect(fs.readFileSync(path.join(state, name, "fixture.txt"), "utf8")).toBe("guest artifact");
    }
    expect(fs.readFileSync(path.join(state, "agent", "harness_retry.json"), "utf8")).toBe('{"attempts":1}');
    expect(fs.readFileSync(path.join(state, "pi-streaming.jsonl"), "utf8")).toBe("pi events");
    expect(fs.readFileSync(path.join(state, "sandbox/agent/logs/pi-streaming.jsonl"), "utf8")).toBe("writable pi events");
    expect(fs.readFileSync(path.join(state, "claude-debug.log"), "utf8")).toBe("claude events");
    expect(fs.readFileSync(path.join(state, "agent-step-summary.md"), "utf8")).toBe("agent summary");
    expect(fs.readFileSync(path.join(state, "step-summary.md"), "utf8")).toBe("detector summary");
    expect(fs.readFileSync(cleanup, "utf8")).toBe("trusted cleanup");
    expect(fs.existsSync(path.join(state, "actions-source", "actions", "setup", "fixture.txt"))).toBe(false);
    expect(fs.existsSync(path.join(state, "cache-memory"))).toBe(false);
    expect(fs.readFileSync(path.join(state, "gateway-secret"), "utf8")).toBe("must not be exported");
    expect(fs.readFileSync(path.join(hostRoot, "safeoutputs", "config.json"), "utf8")).toBe('{"max":1}');
    expect(fs.readFileSync(path.join(hostRoot, "safeoutputs", "outputs.jsonl"), "utf8")).toBe('{"type":"noop"}\n');
    expect(fs.readFileSync(path.join(hostRoot, "safeoutputs", "upload-artifacts", "result.txt"), "utf8")).toBe("upload artifact");
    expect(fs.existsSync(path.join(hostRoot, "safeoutputs", "unexpected.json"))).toBe(false);
    expect(fs.existsSync(root)).toBe(false);
  });

  it.each(["config.json", "tools.json", "validation.json"])("rejects guest policy files without changing host policy (%s)", name => {
    fs.mkdirSync(path.join(hostRoot, "safeoutputs"));
    const policy = path.join(hostRoot, "safeoutputs", name);
    fs.writeFileSync(policy, "trusted policy");
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    fs.writeFileSync(path.join(root, "gh-aw", "safeoutputs", name), "untrusted policy");
    expect(() => collect(environment, state)).toThrow(/must not export safe-output policy/);
    expect(fs.readFileSync(policy, "utf8")).toBe("trusted policy");
    expect(fs.existsSync(root)).toBe(false);
  });

  it.each(["agent", "sandbox", "sandbox/agent", "sandbox/agent/logs", "gh-aw/safeoutputs"])("rejects redirected output directories (%s)", name => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const source = name.startsWith("gh-aw/") ? path.join(root, name) : path.join(root, "state", name);
    fs.mkdirSync(path.dirname(source), { recursive: true });
    if (fs.existsSync(source)) fs.rmdirSync(source);
    fs.symlinkSync(hostRoot, source);
    expect(() => collect(environment, state)).toThrow(/directory|symlink/);
    expect(fs.readFileSync(path.join(hostRoot, "provider-token"), "utf8")).toBe("must not be exported");
    expect(fs.existsSync(root)).toBe(false);
  });

  it("rejects redirected host parents for nested log collection", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    fs.mkdirSync(path.join(root, "state", "sandbox", "agent", "logs"), { recursive: true });
    fs.writeFileSync(path.join(root, "state", "sandbox", "agent", "logs", "fixture.log"), "untrusted output");
    fs.symlinkSync(hostRoot, path.join(state, "sandbox"));
    expect(() => collect(environment, state)).toThrow(/parent must be a directory/);
    expect(fs.existsSync(path.join(hostRoot, "agent"))).toBe(false);
    expect(fs.existsSync(root)).toBe(false);
  });

  it.each(["pi-streaming.jsonl", "gh-aw/safeoutputs/outputs.jsonl"])("rejects directories in file output slots (%s)", name => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const source = name.startsWith("gh-aw/") ? path.join(root, name) : path.join(root, "state", name);
    fs.mkdirSync(source);
    expect(() => collect(environment, state)).toThrow(/must be a regular file/);
    expect(fs.existsSync(root)).toBe(false);
  });

  it("refuses a symlink destination during collection", () => {
    const source = path.join(temp, "output");
    const destination = path.join(temp, "destination");
    fs.writeFileSync(source, "output");
    fs.symlinkSync(path.join(hostRoot, "provider-token"), destination);
    expect(() => collectTree(source, destination)).toThrow(/Unsafe NVX output destination/);
  });

  it("refuses dangling destination symlinks", () => {
    const source = path.join(temp, "output");
    const target = path.join(temp, "missing");
    const destination = path.join(temp, "destination");
    fs.writeFileSync(source, "output");
    fs.symlinkSync(target, destination);
    expect(() => collectTree(source, destination)).toThrow(/Unsafe NVX output destination/);
    expect(fs.existsSync(target)).toBe(false);
  });

  it.each(["state", "gh-aw"])("refuses replaced asset subdirectories (%s)", directory => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const original = path.join(root, directory);
    fs.renameSync(original, `${original}-original`);
    fs.symlinkSync(hostRoot, original);
    expect(() => collect(environment, state)).toThrow(/must not be a symlink/);
    expect(fs.existsSync(root)).toBe(false);
    expect(fs.readFileSync(path.join(hostRoot, "provider-token"), "utf8")).toBe("must not be exported");
  });

  it("removes read-only MCP CLI directories after collection", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    const bin = path.join(root, "gh-aw", "mcp-cli", "bin");
    fs.mkdirSync(bin, { recursive: true });
    fs.writeFileSync(path.join(bin, "fixture"), "fixture");
    fs.chmodSync(bin, 0o555);
    collect(environment, state);
    expect(fs.existsSync(root)).toBe(false);
  });

  it("does not remove a replaced staging directory", () => {
    stage(environment, state);
    const root = path.join(workspace, ".gh-aw-nvx");
    fs.renameSync(root, `${root}-original`);
    fs.mkdirSync(root);
    fs.writeFileSync(path.join(root, "keep"), "checkout content");
    expect(() => collect(environment, state)).toThrow(/directory was replaced/);
    expect(fs.readFileSync(path.join(root, "keep"), "utf8")).toBe("checkout content");
  });
});
