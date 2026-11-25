// @ts-check
import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

const require = createRequire(import.meta.url);
const { domainURLPatterns, addCopilotURLPermissions, readAWFNetworkConfig, createURLPermissionCheck } = require("./copilot_url_permissions.cjs");
const { buildCopilotSDKPermissionHandler } = require("./copilot_sdk_permissions.cjs");
const { main: updateNetworkAllowed } = require("./update_network_allowed.cjs");

describe("Copilot URL permissions", () => {
  let tempDir;
  afterEach(() => {
    vi.unstubAllEnvs();
    if (tempDir) fs.rmSync(tempDir, { recursive: true, force: true });
    tempDir = undefined;
  });
  const writeConfig = config => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-url-permissions-"));
    const configPath = path.join(tempDir, "awf-config.json");
    fs.writeFileSync(configPath, JSON.stringify(config));
    return configPath;
  };

  it.each(["github.com", "*.github.com"])("approves apex and subdomains over both protocols: %s", domain => {
    expect(domainURLPatterns([domain])).toEqual(["http://github.com", "http://*.github.com", "https://github.com", "https://*.github.com"]);
  });

  it.each(["localhost", "foo"])("preserves valid single-label AWF hosts: %s", domain => {
    expect(domainURLPatterns([domain])).toEqual([`http://${domain}`, `http://*.${domain}`, `https://${domain}`, `https://*.${domain}`]);
  });

  it.each([
    ["https://example.com", ["https://example.com", "https://*.example.com"]],
    ["http://example.com:8080", ["http://example.com:8080", "http://*.example.com:8080"]],
    ["https://*.example.com:8443/", ["https://example.com:8443", "https://*.example.com:8443"]],
    ["127.0.0.1", ["http://127.0.0.1", "https://127.0.0.1"]],
    ["http://127.0.0.1:4173", ["http://127.0.0.1:4173"]],
    ["::1", ["http://[::1]", "https://[::1]"]],
    ["https://[::1]:8443", ["https://[::1]:8443"]],
  ])("preserves protocol, port and IP restrictions: %s", (domain, expected) => {
    expect(domainURLPatterns([domain])).toEqual(expected);
  });

  it("deduplicates scoped approvals", () => {
    expect(domainURLPatterns(["https://example.com", "https://*.example.com"])).toEqual(["https://example.com", "https://*.example.com"]);
    expect(domainURLPatterns([])).toEqual([]);
    expect(domainURLPatterns(undefined)).toEqual([]);
  });

  it.each(["*", "http://*", "https://*"])("rejects global allows instead of silently dropping them: %s", domain => {
    expect(() => domainURLPatterns([domain])).toThrow("Global AWF domain allows");
  });

  it.each(["*", "http://*", "https://*"])("fails explicitly rather than dropping a global block: %s", domain => {
    const configPath = writeConfig({ network: { allowDomains: ["example.com"], blockDomains: [domain] } });
    expect(() => addCopilotURLPermissions([], { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toThrow("Global AWF domain blocks");
  });

  it.each([
    null,
    "github.com",
    [null],
    [""],
    ["*.*.example.com"],
    ["https://example.com/path"],
    ["https://user@example.com"],
    ["https://example.com?secret=value"],
    ["--allow-all-urls"],
    ["example.com:0"],
    ["example.com:65536"],
    ["[not-ipv6]"],
  ])("rejects malformed rules without including their values in errors: %j", domains => {
    expect(() => domainURLPatterns(domains)).toThrow(/AWF/);
    try {
      domainURLPatterns(domains);
    } catch (error) {
      expect(String(error)).not.toContain("secret=value");
    }
  });

  it("preserves tool and explicit URL denials, and mirrors firewall blocks", () => {
    const configPath = writeConfig({ network: { allowDomains: ["https://example.com"], blockDomains: ["private.example.com"] } });
    const args = ["--allow-tool", "shell(curl:*)", "--deny-tool", "shell(rm)", "--deny-url", "https://sensitive.example.com"];
    expect(addCopilotURLPermissions(args, { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toEqual([
      ...args,
      "--allow-url",
      "https://example.com",
      "--allow-url",
      "https://*.example.com",
      "--deny-url",
      "http://private.example.com",
      "--deny-url",
      "http://*.private.example.com",
      "--deny-url",
      "https://private.example.com",
      "--deny-url",
      "https://*.private.example.com",
    ]);
    expect(args).toHaveLength(6);
  });

  it("inserts approvals before the CLI argument delimiter", () => {
    const configPath = writeConfig({ network: { allowDomains: ["https://example.com"] } });
    expect(addCopilotURLPermissions(["--deny-tool", "workflow", "--", "prompt"], { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toEqual([
      "--deny-tool",
      "workflow",
      "--allow-url",
      "https://example.com",
      "--allow-url",
      "https://*.example.com",
      "--",
      "prompt",
    ]);
  });

  it("leaves SDK and unsandboxed runs untouched without reading a config", () => {
    const args = ["--allow-tool", "shell(ls)"];
    expect(addCopilotURLPermissions(args, { env: {}, configPath: "/missing" })).toBe(args);
    expect(addCopilotURLPermissions(args, { env: { AWF_REFLECT_ENABLED: "1" }, sdkMode: true, configPath: "/missing" })).toBe(args);
  });

  it("adds no grants for an empty network", () => {
    const configPath = writeConfig({ network: {} });
    expect(addCopilotURLPermissions([], { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toEqual([]);
  });

  it("fails explicitly for unreadable or malformed sandbox config", () => {
    expect(() => addCopilotURLPermissions([], { env: { AWF_REFLECT_ENABLED: "1" }, configPath: "/missing" })).toThrow(/Cannot read AWF config.*ENOENT/);
    const configPath = writeConfig({ network: null });
    expect(() => addCopilotURLPermissions([], { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toThrow("Invalid AWF network config");
    fs.writeFileSync(configPath, '{"secret":"value",');
    expect(() => addCopilotURLPermissions([], { env: { AWF_REFLECT_ENABLED: "1" }, configPath })).toThrow(/Cannot parse AWF config.*JSON/);
  });

  it("falls back only when the implicit config file is missing", () => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-url-fallback-"));
    const configPath = path.join(tempDir, "default-awf-config.json");
    const fallbackConfigPath = path.join(tempDir, "runner-temp", "gh-aw", "awf-config.json");
    fs.mkdirSync(path.dirname(fallbackConfigPath), { recursive: true });
    fs.writeFileSync(fallbackConfigPath, JSON.stringify({ network: { allowDomains: ["shared.example.test"] } }));
    const options = { env: { AWF_REFLECT_ENABLED: "1" }, configPath, fallbackConfigPath };

    expect(addCopilotURLPermissions([], options)).toContain("https://shared.example.test");
    fs.writeFileSync(configPath, "{");
    expect(() => addCopilotURLPermissions([], options)).toThrow(/Cannot parse AWF config.*JSON/);
  });

  it("uses caller-added domains and ecosystems from the finalized runtime config", async () => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-url-runtime-"));
    const dir = path.join(tempDir, "gh-aw");
    fs.mkdirSync(dir);
    const configPath = path.join(dir, "awf-config.json");
    fs.writeFileSync(configPath, JSON.stringify({ network: { allowDomains: ["https://github.com"] } }));
    vi.stubEnv("RUNNER_TEMP", tempDir);
    vi.stubEnv("GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED", "custom.example.com,node");
    vi.stubEnv("GH_AW_ECOSYSTEM_MAP_JSON", JSON.stringify({ node: ["registry.npmjs.org"] }));
    await updateNetworkAllowed();
    const args = addCopilotURLPermissions(["--allow-tool", "shell(curl:*)"], { env: { AWF_REFLECT_ENABLED: "1" }, configPath });
    expect(args).toContain("https://custom.example.com");
    expect(args).toContain("http://*.custom.example.com");
    expect(args).toContain("https://registry.npmjs.org");
    expect(args).not.toContain("http://github.com");
    expect(args).not.toContain("--allow-all-urls");
    expect(args).not.toContain("--allow-all-tools");
  });

  it("passes scoped URL flags to the real harness subprocess without changing tool permissions", () => {
    const configPath = writeConfig({ network: { allowDomains: ["https://github.com"], blockDomains: ["https://private.github.com"] } });
    const stubPath = path.join(tempDir, "stub.cjs");
    const argsPath = path.join(tempDir, "args.json");
    fs.writeFileSync(stubPath, `require("fs").writeFileSync(${JSON.stringify(argsPath)}, JSON.stringify(process.argv.slice(2)));`);
    const result = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stubPath, "--allow-tool", "shell(curl:*)", "--deny-tool", "workflow"], {
      cwd: tempDir,
      env: {
        PATH: process.env.PATH,
        HOME: tempDir,
        COPILOT_HOME: path.join(tempDir, ".copilot"),
        GH_AW_TMP_DIR: tempDir,
        GH_AW_SESSION_STATE_BASE_DIR: tempDir,
        GH_AW_AWF_CONFIG_PATH: configPath,
        GH_AW_SKIP_REFLECT: "true",
        AWF_REFLECT_ENABLED: "1",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(result.status, result.stderr).toBe(0);
    const args = JSON.parse(fs.readFileSync(argsPath, "utf8"));
    expect(args).toEqual(expect.arrayContaining(["--allow-tool", "shell(curl:*)", "--deny-tool", "workflow", "--allow-url", "https://github.com", "https://*.github.com", "--deny-url", "https://private.github.com"]));
    expect(args).not.toContain("http://github.com");
    expect(args).not.toContain("--allow-all-urls");
  });

  it("starts with the shared RUNNER_TEMP config when the default config is absent and no override is set", () => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-url-shared-config-"));
    const runnerTemp = path.join(tempDir, "runner-temp");
    const sharedConfigDir = path.join(runnerTemp, "gh-aw");
    fs.mkdirSync(sharedConfigDir, { recursive: true });
    fs.writeFileSync(path.join(sharedConfigDir, "awf-config.json"), JSON.stringify({ network: { allowDomains: ["shared.example.test"] } }));
    const stubPath = path.join(tempDir, "shared-config-stub.cjs");
    const argsPath = path.join(tempDir, "shared-config-args.json");
    fs.writeFileSync(stubPath, `require("fs").writeFileSync(${JSON.stringify(argsPath)}, JSON.stringify(process.argv.slice(2)));`);
    const result = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stubPath], {
      cwd: tempDir,
      env: {
        PATH: process.env.PATH,
        HOME: tempDir,
        COPILOT_HOME: path.join(tempDir, ".copilot"),
        GH_AW_TMP_DIR: tempDir,
        GH_AW_SESSION_STATE_BASE_DIR: tempDir,
        RUNNER_TEMP: runnerTemp,
        GH_AW_SKIP_REFLECT: "true",
        AWF_REFLECT_ENABLED: "1",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(result.status, result.stderr).toBe(0);
    const args = JSON.parse(fs.readFileSync(argsPath, "utf8"));
    expect(args).toContain("https://shared.example.test");
  });

  describe("SDK URL approvals", () => {
    const check = createURLPermissionCheck({ allowDomains: ["localhost", "github.com", "https://secure.example.com", "http://127.0.0.1:4173"], blockDomains: ["blocked.github.com"] });

    it.each(["http://localhost:4321/gh-aw/", "https://api.github.com/repos", "https://github.com", "https://secure.example.com/x", "http://127.0.0.1:4173/"])("approves URLs inside the AWF allowlist: %s", url => {
      expect(check(url)).toBe(true);
    });

    it.each(["https://blocked.github.com/", "https://evilgithub.com/", "http://secure.example.com/", "http://127.0.0.1:8080/", "ftp://github.com/", "file:///etc/passwd", "not a url", undefined])(
      "denies URLs outside the allowlist, blocked, or unparseable: %s",
      url => {
        expect(check(url)).toBe(false);
      }
    );

    it("denies every URL without an AWF network config", () => {
      expect(createURLPermissionCheck(undefined)("https://github.com")).toBe(false);
      expect(readAWFNetworkConfig({ env: {}, configPath: "/nonexistent" })).toBeUndefined();
    });

    it("approves a shell-triggered URL request only inside the network scope", () => {
      const approveAll = () => ({ kind: "approve-once" });
      const handler = buildCopilotSDKPermissionHandler({ allowedTools: ["shell(curl http://localhost:4321/gh-aw/:*)"] }, approveAll, { isURLAllowed: check });
      expect(handler({ kind: "url", url: "http://localhost:4321/gh-aw/" })).toEqual({ kind: "approve-once" });
      expect(handler({ kind: "url", url: "https://example.org/" })).toMatchObject({ kind: "reject" });
    });
  });
});
