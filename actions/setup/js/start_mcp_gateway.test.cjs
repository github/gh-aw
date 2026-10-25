import fs from "fs";
import os from "os";
import path from "path";
import http from "http";
import { createRequire } from "node:module";
import vm from "node:vm";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  applyAgenticWorkflowsRunnerIdentity,
  applyOTLPIgnoreIfMissing,
  clearGatewayStartupMarker,
  createGatewayStderrPath,
  getGatewayStartupMarkerPath,
  redactGatewayDiagnostics,
  writeGatewayStartupMarker,
  customGatewayEnvNamesVar,
  customGatewayReservedEnvPrefix,
  customGatewayEnvTransportPrefix,
  detectEngineType,
  extractOptionalServerNames,
  getJSONParseErrorContext,
  getOTLPIfMissingMode,
  hasNonEmptyOTLPHeaders,
  injectCustomGatewayEnvArgs,
  injectProxyEnvArgs,
  computeHealthRetryBudgetMs,
  MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS,
  MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS,
  MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT,
  MCP_GATEWAY_HEALTH_RETRY_DEFAULTS,
  MCP_GATEWAY_HEALTH_RETRY_ENV,
  resolveGatewayHealthRetryConfig,
  normalizeSinkVisibilityEncoding,
  resolveCopilotConfigPaths,
  validateGatewayAgentIdentifiers,
  waitForSafeOutputsReadiness,
  retryGatewayStartup,
  httpGet,
} from "./start_mcp_gateway.cjs";
import { writeMcpConfiguration } from "./mcp_configuration.cjs";
import { applyGatewayRuntimeTimeouts } from "./tool_timeouts.cjs";

describe("MCP gateway runner proxy environment", () => {
  it.each([
    ["HTTP_PROXY", ["HTTP_PROXY", "NO_PROXY"]],
    ["HTTPS_PROXY", ["HTTPS_PROXY", "NO_PROXY"]],
    ["NO_PROXY", ["NO_PROXY"]],
    ["http_proxy", ["NO_PROXY", "http_proxy"]],
    ["https_proxy", ["NO_PROXY", "https_proxy"]],
    ["no_proxy", ["no_proxy"]],
  ])("forwards %s by name without putting values in Docker arguments", (name, names) => {
    const args = ["run", "--rm", "gateway:latest"];
    const value = "synthetic-proxy-value with spaces";
    expect(injectProxyEnvArgs(args, { [name]: value })).toEqual(["run", ...names.flatMap(name => ["-e", name]), "--rm", "gateway:latest"]);
    expect(args).toEqual(["run", "--rm", "gateway:latest"]);
  });

  it("leaves unset variables to Docker client defaults", () => {
    expect(injectProxyEnvArgs(["run", "--rm", "gateway:latest"], {})).toEqual(["run", "--rm", "gateway:latest"]);
  });

  it("forwards empty values and preserves explicit container overrides", () => {
    const args = ["run", "-e", "HTTPS_PROXY=explicit", "gateway:latest"];
    expect(injectProxyEnvArgs(args, { HTTPS_PROXY: "" })).toEqual(["run", "-e", "HTTPS_PROXY", "-e", "HTTPS_PROXY=explicit", "gateway:latest"]);
  });

  it("composes inherited proxy forwarding before explicit gateway environment values", () => {
    const marker = "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__";
    const env = {
      [customGatewayEnvNamesVar]: '["HTTP_PROXY","NO_PROXY"]',
      [`${customGatewayEnvTransportPrefix}0`]: "explicit-proxy",
      [`${customGatewayEnvTransportPrefix}1`]: "explicit-no-proxy",
      HTTP_PROXY: "inherited-proxy",
      NO_PROXY: "localhost,127.0.0.1",
    };
    const args = injectCustomGatewayEnvArgs(["run", marker, "gateway:latest"], env);
    const result = injectProxyEnvArgs(args, env);

    expect(result).toEqual(["run", "-e", "HTTP_PROXY", "-e", "NO_PROXY", "-e", "HTTP_PROXY=explicit-proxy", "-e", "NO_PROXY=explicit-no-proxy", "gateway:latest"]);
    expect(env.NO_PROXY).toBe("localhost,127.0.0.1,host.docker.internal,awf-enclave-mcp");
  });

  it("adds local MCP backend hosts to existing proxy exclusions and preserves Docker defaults when unset", () => {
    const env = { HTTP_PROXY: "http://proxy.example", NO_PROXY: "localhost,127.0.0.1" };
    const args = injectProxyEnvArgs(["run", "--rm", "gateway:latest"], env);

    expect(args).toEqual(["run", "-e", "HTTP_PROXY", "-e", "NO_PROXY", "--rm", "gateway:latest"]);
    expect(env.NO_PROXY).toBe("localhost,127.0.0.1,host.docker.internal,awf-enclave-mcp");
    expect(injectProxyEnvArgs(["run", "--rm", "gateway:latest"], {})).toEqual(["run", "--rm", "gateway:latest"]);
  });

  it.each(["HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"])("adds local MCP backend exclusions with only %s set", name => {
    const env = { [name]: "http://proxy.example:8080" };
    const args = ["run", "--rm", "gateway:latest"];
    const result = injectProxyEnvArgs(args, env);

    expect(env.NO_PROXY).toBe("host.docker.internal,awf-enclave-mcp");
    expect(env.no_proxy).toBeUndefined();
    expect(result).toContain(name);
    expect(result).toContain("NO_PROXY");
    expect(result).not.toContain(env[name]);
    expect(injectProxyEnvArgs(args, env)).toEqual(result);
  });

  it("preserves lowercase runner exclusions without synthesizing uppercase exclusions", () => {
    const env = { HTTP_PROXY: "http://proxy.example:8080", no_proxy: "localhost,.example.com" };
    expect(injectProxyEnvArgs(["run", "gateway:latest"], env)).toEqual(["run", "-e", "HTTP_PROXY", "-e", "no_proxy", "gateway:latest"]);
    expect(env.NO_PROXY).toBeUndefined();
    expect(env.no_proxy).toBe("localhost,.example.com,host.docker.internal,awf-enclave-mcp");
  });

  it("keeps explicit gateway exclusions after synthesized proxy-only exclusions", () => {
    const env = {
      HTTP_PROXY: "http://proxy.example:8080",
      [customGatewayEnvNamesVar]: '["NO_PROXY"]',
      [`${customGatewayEnvTransportPrefix}0`]: "explicit-no-proxy",
    };
    const args = injectCustomGatewayEnvArgs(["run", "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__", "gateway:latest"], env);
    expect(injectProxyEnvArgs(args, env)).toEqual(["run", "-e", "HTTP_PROXY", "-e", "NO_PROXY", "-e", "NO_PROXY=explicit-no-proxy", "gateway:latest"]);
  });

  it("adds runner proxy flags before launching the gateway", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source.indexOf("args = injectProxyEnvArgs(args, gatewayEnv)")).toBeLessThan(source.indexOf("const child = spawn(cmd, args"));
  });

  it.each(["HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"])("redacts %s values in gateway diagnostics", name => {
    const value = "synthetic-proxy-value";
    expect(redactGatewayDiagnostics(`proxy connection failed: ${value}`, { [name]: value })).not.toContain(value);
  });
});

describe("MCP gateway CA launcher arguments", () => {
  it.each([false, true])("keeps CA mounts before existing Docker flags and the image (inherited proxy: %s)", async inheritedProxy => {
    const directory = fs.mkdtempSync(path.join(process.cwd(), ".mcp-ca-launcher-"));
    const caPath = path.join(directory, "private ca.pem");
    const require = createRequire(import.meta.url);
    const spawn = vi.fn(() => ({
      on: vi.fn(),
      stdin: { write: vi.fn(), end: vi.fn() },
      unref: vi.fn(),
      // Stop before health polling; this test covers the launch boundary.
      pid: undefined,
    }));
    const core = { info: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn() };
    const env = {
      RUNNER_TEMP: directory,
      MCP_GATEWAY_DOCKER_COMMAND: "docker run --rm -i --network host __GH_AW_MCP_GATEWAY_CUSTOM_ENV__ gateway:latest --config-stdin",
      ...(inheritedProxy ? { HTTP_PROXY: "http://inherited-proxy.example:8080" } : {}),
      [customGatewayEnvNamesVar]: '["HTTP_PROXY"]',
      [`${customGatewayEnvTransportPrefix}0`]: "http://custom-proxy.example:8080",
    };
    const config = {
      gateway: { port: 8080, domain: "localhost", agentId: "test-agent" },
      mcpServers: {
        private: { type: "http", url: "https://private.example/mcp", caCertFile: caPath },
        public: { type: "http", url: "https://public.example/mcp" },
      },
    };
    const mocks = {
      fs: {
        readFileSync: vi.fn(() => Buffer.from(JSON.stringify(config))),
        mkdirSync: vi.fn(),
        chmodSync: vi.fn(),
        rmSync: vi.fn(),
        openSync: vi.fn(() => 42),
        mkdtempSync: vi.fn(() => directory),
      },
      os: { tmpdir: () => directory },
      child_process: { spawn, execSync: vi.fn() },
      "./shim.cjs": {},
      "./symlink_guard.cjs": { lstatGuard: vi.fn(() => ({})) },
      "./mcp_configuration.cjs": { writeMcpConfiguration: vi.fn() },
      "./mcp_server_ca.cjs": {
        prepareMCPServerCAs: config => require("./mcp_server_ca.cjs").prepareMCPServerCAs(config, env),
      },
    };

    try {
      fs.writeFileSync(caPath, "certificate validated by gateway", { mode: 0o600 });
      const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
      await vm.runInNewContext(`${source}\nmain();`, {
        require: id => mocks[id] || require(id),
        module: { exports: {} },
        process: { env, umask: vi.fn(), on: vi.fn() },
        core,
      });

      expect(spawn).toHaveBeenCalledOnce();
      const [command, args, options] = spawn.mock.calls[0];
      const launchedConfig = JSON.parse(spawn.mock.results[0].value.stdin.write.mock.calls[0][0]);
      const target = launchedConfig.mcpServers.private.caCertFile;
      expect(target).toMatch(/^\/etc\/gh-aw\/mcp-ca\/[a-f0-9]{64}\.pem$/);
      expect(command).toBe("docker");
      expect(args).toEqual([
        "run",
        ...(inheritedProxy ? ["-e", "HTTP_PROXY", "-e", "NO_PROXY"] : []),
        "--mount",
        `type=bind,source=${fs.realpathSync(caPath)},target=${target},readonly`,
        "--rm",
        "-i",
        "--network",
        "host",
        "-e",
        "HTTP_PROXY=http://custom-proxy.example:8080",
        "gateway:latest",
        "--config-stdin",
      ]);
      expect(options.env.NO_PROXY).toBe(inheritedProxy ? "host.docker.internal,awf-enclave-mcp" : undefined);
      expect(launchedConfig.mcpServers.public).not.toHaveProperty("caCertFile");
      expect(core.setFailed).toHaveBeenCalledExactlyOnceWith("ERR_SYSTEM: Failed to start gateway container");
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});

describe("agenticworkflows backend ownership", () => {
  it.each(["localhost/gh-aw:dev", "alpine:3.24"])("uses the runner identity without widening private artifact modes (%s)", container => {
    const env = { GITHUB_TOKEN: "${GITHUB_TOKEN}", GITHUB_ACTOR: "${GITHUB_ACTOR}" };
    const backend = { container, args: ["--network", "host", "-w", "/workspace"], env };
    const other = { container: "example/server", args: [] };
    const config = { mcpServers: { agenticworkflows: backend, other } };
    applyAgenticWorkflowsRunnerIdentity(config, 1001, 123);
    expect(backend.args).toEqual(["--network", "host", "-w", "/workspace", "--user", "1001:123"]);
    expect(backend.env).toEqual({ HOME: "/tmp", ...env });
    expect(other).toEqual({ container: "example/server", args: [] });
    applyAgenticWorkflowsRunnerIdentity(config, 1001, 123);
    expect(backend.args.filter(arg => arg === "--user")).toHaveLength(1);
  });

  it.each([["--user", "42:43"], ["--user=42:43"], ["-u", "42:43"], ["-u42:43"], ["--user", "app:staff"], ["--user=app:staff"], ["-u", "app:staff"], ["-uroot"], ["-uapp:staff"], ["-u=app:staff"]])(
    "preserves an explicitly configured container user: %j",
    (...args) => {
      const backend = { container: "example/server", args, env: { HOME: "/custom" } };
      const original = structuredClone(backend);
      applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, 1001, 123);
      applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, 1001, 123);
      expect(backend).toEqual(original);
    }
  );

  it.each(["-update", "-unshare"])("preserves %s as Docker's attached -u username, not an unrelated flag", arg => {
    const backend = { container: "example/server", args: [arg], env: { HOME: "/custom" } };
    const original = structuredClone(backend);
    applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, 1001, 123);
    expect(backend).toEqual(original);
  });

  it("does not confuse --userns with an explicit container user", () => {
    const backend = { container: "example/server", args: ["--userns=host"], env: { HOME: "/custom" } };
    applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, 1001, 123);
    expect(backend).toEqual({ container: "example/server", args: ["--userns=host", "--user", "1001:123"], env: { HOME: "/custom" } });
  });

  it("preserves a configured HOME and supports default container arguments", () => {
    const backend = { container: "alpine:3.24", env: { HOME: "/custom" } };
    applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, 1001, 123);
    expect(backend).toEqual({ container: "alpine:3.24", args: ["--user", "1001:123"], env: { HOME: "/custom" } });
  });

  it.each([{}, { mcpServers: {} }, { mcpServers: { agenticworkflows: { url: "https://example.com/mcp" } } }])("leaves absent and HTTP backends unchanged", config => {
    const original = structuredClone(config);
    applyAgenticWorkflowsRunnerIdentity(config, 1001, 123);
    expect(config).toEqual(original);
  });

  it.each([
    [-1, 123],
    [1001, -1],
    [NaN, 123],
    [1001, Infinity],
    [1.5, 123],
  ])("rejects invalid runner identities (%s, %s)", (uid, gid) => {
    const backend = { container: "alpine:3.24" };
    expect(() => applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: backend } }, uid, gid)).toThrow("Runner identity is required");
    expect(backend).toEqual({ container: "alpine:3.24" });
  });

  it.each([{ args: "invalid" }, { args: [42] }, { env: [] }, { env: null }])("rejects malformed backend configuration without publishing it", fields => {
    expect(() => applyAgenticWorkflowsRunnerIdentity({ mcpServers: { agenticworkflows: { container: "alpine:3.24", ...fields } } }, 1001, 123)).toThrow(/Invalid agenticworkflows container/);
  });

  it("applies identity before serializing the configuration sent to the gateway", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source.indexOf("applyAgenticWorkflowsRunnerIdentity(configObj)")).toBeLessThan(source.indexOf("mcpConfig = JSON.stringify(configObj)"));
  });
});

describe("start_mcp_gateway logging", () => {
  it("records only a sanitized configuration snapshot before launching the gateway", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain("writeMcpConfiguration(logDir, configObj)");
    expect(source.indexOf("writeMcpConfiguration(logDir, configObj)")).toBeLessThan(source.indexOf('core.info("Starting gateway container...")'));
    expect(source.indexOf("writeMcpConfiguration(logDir, configObj)")).toBeLessThan(source.indexOf("const optionalServerNames = extractOptionalServerNames(configObj)"));
    expect(source).not.toContain("core.info(mcpConfig)");
    expect(source).not.toContain("core.error(parseMessage)");
    expect(source).not.toContain("parseContext.lineText");
    expect(source).not.toContain('core.error("Configuration content:")');
  });

  it("reads piped configuration as a Buffer before decoding UTF-8", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain('fs.readFileSync(0).toString("utf8")');
    expect(source).not.toContain('fs.readFileSync(0, "utf8")');
  });

  it("does not create the legacy MCP gateway stderr log", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).not.toContain("/tmp/gh-aw/mcp-logs/stderr.log");
  });

  it("does not create the MCP gateway startup log", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).not.toContain("/tmp/gh-aw/mcp-logs/start-gateway.log");
  });

  it("stops health polling when the gateway process exits", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    const healthCheckStart = source.indexOf('core.info("=== Health Check Progress ===")');
    const healthCheckEnd = source.indexOf("succeeded = true;", healthCheckStart);
    expect(source.slice(healthCheckStart, healthCheckEnd)).toContain("() => isProcessAlive(gatewayPid)");
  });

  it("captures the gateway child process stderr outside the artifact directory", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain(`stdio: ["pipe", outputFd, stderrFd]`);
    expect(source).toContain(`path.join(os.tmpdir(), "gh-aw-mcp-gateway-")`);
  });

  it("exports the gateway agent ID as the AWF API key handoff", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain("`gateway-api-key=${agentId}`");
  });

  it.each([false, true])("times health and safe-output readiness separately when readiness fails: %s", async fails => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    const start = source.indexOf("  if (succeeded) {");
    const end = source.indexOf("\n  } else {", start);
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const runSuccessBlock = new AsyncFunction(
      "{ nowMs, printTiming, waitForSafeOutputsReadiness, core, writeGatewayStartupMarker }",
      `const succeeded = true, healthCheckStart = 10, gatewayStartTime = 0;
       const configObj = {}, gatewayPort = 8080, agentId = "test", healthRetryConfig = {}, gatewayStartupMarker = "marker";
       ${source.slice(start, end)}
       }`
    );
    let clock = 30;
    const timings = [];
    const failure = new Error("readiness failed");
    const readiness = vi.fn(async () => {
      clock += 50;
      if (fails) throw failure;
    });
    const marker = vi.fn();
    const result = runSuccessBlock({
      nowMs: () => clock,
      printTiming: (startMs, label) => timings.push([label, clock - startMs]),
      waitForSafeOutputsReadiness: readiness,
      core: { info: vi.fn() },
      writeGatewayStartupMarker: marker,
    });
    if (fails) {
      await expect(result).rejects.toBe(failure);
      expect(marker).not.toHaveBeenCalled();
    } else {
      await result;
      expect(marker).toHaveBeenCalledWith("marker");
    }
    expect(timings).toEqual([
      ["Health check wait", 20],
      ["Safe outputs readiness wait", 50],
    ]);
    expect(readiness).toHaveBeenCalledWith({}, 8080, "test", {}, 0);
  });
});

describe("start_mcp_gateway health timeout", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("allows cleanup and registration time after the backend startup timeout", () => {
    const config = resolveGatewayHealthRetryConfig({});

    expect(config).toMatchObject(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS);
    expect(config.retryBudgetMs).toBe(computeHealthRetryBudgetMs(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS));
    expect(config.retryBudgetMs).toBeGreaterThanOrEqual(MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_DEFAULT_MS + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS);
  });

  it("applies environment overrides for every retry parameter", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "10",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "100",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "400",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "1.5",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "1000",
    });

    expect(config).toMatchObject({ maxTotalAttempts: 10, initialDelayMs: 100, maxDelayMs: 400, backoffMultiplier: 1.5, backendStartupTimeoutMs: 1000 });
    expect(config.retryBudgetMs).toBe(computeHealthRetryBudgetMs(config));
  });

  it("sizes default health retries for tools.startup-timeout", () => {
    const env = { GH_AW_STARTUP_TIMEOUT: "300" };
    const gatewayConfig = { gateway: {} };
    applyGatewayRuntimeTimeouts(gatewayConfig, env);
    const warning = vi.spyOn(global.core, "warning").mockImplementation(() => {});

    const config = resolveGatewayHealthRetryConfig(env);

    expect(gatewayConfig.gateway.startupTimeout).toBe(300);
    expect(config.backendStartupTimeoutMs).toBe(300_000);
    expect(config.maxTotalAttempts).toBe(327);
    expect(config.retryBudgetMs).toBeGreaterThanOrEqual(325_000);
    expect(computeHealthRetryBudgetMs({ ...config, maxTotalAttempts: config.maxTotalAttempts - 1 })).toBeLessThan(325_000);
    expect(warning).not.toHaveBeenCalled();
  });

  it.each([{}, { initialDelayMs: "10", maxDelayMs: "100", backoffMultiplier: "1.5" }, { initialDelayMs: "250", maxDelayMs: "1000", backoffMultiplier: "1" }, { initialDelayMs: "2000", maxDelayMs: "500", backoffMultiplier: "0.5" }])(
    "covers the startup window with normalized custom retry delays: %j",
    overrides => {
      const env = { [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "300000" };
      for (const [key, value] of Object.entries(overrides)) {
        env[MCP_GATEWAY_HEALTH_RETRY_ENV[key]] = value;
      }

      const config = resolveGatewayHealthRetryConfig(env);

      expect(config.retryBudgetMs).toBeGreaterThanOrEqual(325_000);
      expect(config.maxTotalAttempts).toBeGreaterThanOrEqual(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS.maxTotalAttempts);
    }
  );

  it.each(["", " ", "not-a-number", "0", "-1", "1.5"])("automatically sizes attempts when the override is unusable: %j", override => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: override,
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "300000",
    });

    expect(config.retryBudgetMs).toBeGreaterThanOrEqual(325_000);
  });

  it.each(["10", "150", "330"])("preserves an explicit attempt limit: %s", attempts => {
    const warning = vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: attempts,
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "300000",
    });

    expect(config.maxTotalAttempts).toBe(Number(attempts));
    if (Number(attempts) < 327) {
      expect(warning).toHaveBeenCalledWith(expect.stringContaining("health check may give up"));
    } else {
      expect(warning).not.toHaveBeenCalled();
    }
  });

  it("caps automatic attempts when short delays cannot cover the startup window", () => {
    const warning = vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "300000",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "1",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "1",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "1",
    });

    expect(config.maxTotalAttempts).toBe(MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT);
    expect(config.retryBudgetMs).toBe(9_999);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("health retry budget is"));
  });

  it.each(["1", "1.0000000000000002", "2"])("bounds automatically sized attempts for large startup windows (%s)", backoffMultiplier => {
    const warning = vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "2147483000",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "1",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: backoffMultiplier,
    });

    expect(config.maxTotalAttempts).toBe(MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT);
    expect(config.retryBudgetMs).toBeLessThan(2147483000 + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("health retry budget is"));
  });

  it("stops retrying when the gateway process is no longer alive", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    const probe = vi.fn(async () => {
      throw new Error("connection refused");
    });
    const isGatewayAlive = vi.fn(() => false);
    const config = { maxTotalAttempts: 10, initialDelayMs: 1, maxDelayMs: 1, backoffMultiplier: 1 };

    await expect(retryGatewayStartup(probe, config, 1000, "health", isGatewayAlive)).rejects.toThrow("connection refused");

    expect(probe).toHaveBeenCalledTimes(1);
    expect(isGatewayAlive).toHaveBeenCalledTimes(1);
    expect(Date.now()).toBe(0);
  });

  it("limits retry warnings to exponentially spaced progress reports", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    const warning = vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const probe = vi.fn(async () => {
      throw new Error("backend unavailable");
    });
    const config = { maxTotalAttempts: 20, initialDelayMs: 1, maxDelayMs: 1, backoffMultiplier: 1 };
    const wait = retryGatewayStartup(probe, config, 1000, "health");
    const rejection = expect(wait).rejects.toThrow("backend unavailable");

    await vi.runAllTimersAsync();
    await rejection;

    expect(probe).toHaveBeenCalledTimes(20);
    expect(warning).toHaveBeenCalledTimes(5);
  });

  it.each([
    { backendStartupTimeoutMs: 300_000, readyMs: 300_000, backoffMultiplier: "2" },
    { backendStartupTimeoutMs: 1000, readyMs: 25_000, initialDelayMs: "3", backoffMultiplier: "1.00001" },
  ])("keeps polling through backend startup and registration: %j", async ({ backendStartupTimeoutMs, readyMs, initialDelayMs, backoffMultiplier }) => {
    vi.useFakeTimers({ loopLimit: 50_000 });
    vi.setSystemTime(0);
    vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: String(backendStartupTimeoutMs),
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: initialDelayMs,
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: backoffMultiplier,
    });
    const deadline = backendStartupTimeoutMs + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS;
    const probe = vi.fn(async () => {
      if (Date.now() < readyMs) throw new Error("backend still starting");
    });
    const wait = retryGatewayStartup(probe, config, deadline, "health");

    await vi.runAllTimersAsync();
    await wait;

    expect(probe.mock.calls.length).toBeGreaterThan(150);
    expect(Date.now()).toBeGreaterThanOrEqual(readyMs);
    expect(Date.now()).toBeLessThan(deadline);
  });

  it("stops an unavailable gateway at the shared startup deadline", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    vi.spyOn(global.core, "warning").mockImplementation(() => {});
    const config = resolveGatewayHealthRetryConfig({ [MCP_GATEWAY_HEALTH_RETRY_ENV.backendStartupTimeoutMs]: "300000" });
    const probe = vi.fn(async () => {
      throw new Error("backend unavailable");
    });
    const wait = retryGatewayStartup(probe, config, 325_000, "health");
    const rejection = expect(wait).rejects.toThrow("startup deadline exceeded");

    await vi.runAllTimersAsync();
    await rejection;

    expect(probe.mock.calls.length).toBeGreaterThan(150);
    expect(Date.now()).toBe(325_000);
  });

  it("falls back to defaults for invalid overrides", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "not-a-number",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "-5",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "0",
    });

    expect(config).toMatchObject(MCP_GATEWAY_HEALTH_RETRY_DEFAULTS);
  });

  it("raises the delay cap when it is below the initial delay", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.initialDelayMs]: "2000",
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxDelayMs]: "500",
    });

    expect(config.maxDelayMs).toBe(2000);
  });

  it("clamps a shrinking backoff multiplier to a constant delay", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.backoffMultiplier]: "0.5",
    });

    expect(config.backoffMultiplier).toBe(1);
  });

  it("caps an excessive attempt count", () => {
    const config = resolveGatewayHealthRetryConfig({
      [MCP_GATEWAY_HEALTH_RETRY_ENV.maxTotalAttempts]: "1000000000",
    });

    expect(config.maxTotalAttempts).toBe(MCP_GATEWAY_HEALTH_MAX_ATTEMPTS_LIMIT);
  });

  it("computes the cumulative startup retry delay", () => {
    // The retry loop multiplies before its first sleep: 500ms, then 1000ms capped.
    expect(computeHealthRetryBudgetMs({ maxTotalAttempts: 3, initialDelayMs: 250, maxDelayMs: 1000, backoffMultiplier: 2 })).toBe(1500);
  });

  it.each([
    { maxTotalAttempts: 1, initialDelayMs: 250, maxDelayMs: 1000, backoffMultiplier: 2 },
    { maxTotalAttempts: 500, initialDelayMs: 100, maxDelayMs: 400, backoffMultiplier: 1.5 },
    { maxTotalAttempts: 500, initialDelayMs: 250, maxDelayMs: 1000, backoffMultiplier: 1 },
    { maxTotalAttempts: 500, initialDelayMs: 1, maxDelayMs: 1000, backoffMultiplier: 1.0000000000000002 },
    { maxTotalAttempts: 500, initialDelayMs: 250, maxDelayMs: 1000, backoffMultiplier: Number.MAX_VALUE },
  ])("matches the unrounded backoff schedule: %j", config => {
    let delayMs = config.initialDelayMs;
    let budgetMs = 0;
    for (let attempt = 1; attempt < config.maxTotalAttempts; attempt += 1) {
      delayMs = Math.min(delayMs * config.backoffMultiplier, config.maxDelayMs);
      budgetMs += delayMs;
    }

    expect(computeHealthRetryBudgetMs(config)).toBeCloseTo(budgetMs, 5);
  });
});

describe("start_mcp_gateway startup diagnostics", () => {
  it("creates the stderr capture file in an owner-only directory outside /tmp/gh-aw", () => {
    const stderrPath = createGatewayStderrPath();
    const dir = path.dirname(stderrPath);
    expect(dir.startsWith("/tmp/gh-aw/")).toBe(false);
    expect(fs.statSync(dir).mode & 0o777).toBe(0o700);
    fs.rmSync(dir, { recursive: true, force: true });
  });

  describe("start_mcp_gateway safeoutputs readiness", () => {
    let server;
    let originalCore;
    let requests;
    const config = { mcpServers: { safeoutputs: {} } };
    const retryConfig = {
      maxTotalAttempts: 4,
      initialDelayMs: 1,
      maxDelayMs: 1,
      backoffMultiplier: 1,
      backendStartupTimeoutMs: 1000,
      retryBudgetMs: 3,
    };

    beforeEach(() => {
      originalCore = global.core;
      global.core = { ...originalCore, info: vi.fn(), warning: vi.fn() };
      requests = [];
    });

    afterEach(async () => {
      vi.restoreAllMocks();
      vi.useRealTimers();
      if (server) {
        await new Promise(resolve => server.close(resolve));
        server = undefined;
      }
      global.core = originalCore;
    });

    async function startBackend(handleToolsList, { resetFirstInitialize = false, resetEveryInitialize = false, initStatus = 200, serverName = "safeoutputs", listResponse } = {}) {
      let initializes = 0;
      server = http.createServer((req, res) => {
        let data = "";
        req.on("data", chunk => (data += chunk));
        req.on("end", () => {
          const rpc = JSON.parse(data);
          requests.push(rpc.method);
          expect(req.url).toBe(`/mcp/${serverName}`);
          expect(req.headers.authorization).toBe("readiness-test-key");
          if (rpc.method === "initialize") {
            initializes += 1;
            if (resetEveryInitialize || (resetFirstInitialize && initializes === 1)) {
              req.socket.destroy();
              return;
            }
            res.writeHead(initStatus === 503 && initializes > 1 ? 200 : initStatus, { "Content-Type": "application/json", "Mcp-Session-Id": "readiness-test-session" });
            res.end(JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result: {} }));
          } else {
            expect(req.headers["mcp-session-id"]).toBe("readiness-test-session");
            if (rpc.method === "tools/list") {
              res.writeHead(200, { "Content-Type": "application/json" });
              res.end(listResponse ?? JSON.stringify({ jsonrpc: "2.0", id: rpc.id, result: { tools: handleToolsList() } }));
            } else {
              res.writeHead(204);
              res.end();
            }
          }
        });
      });
      await new Promise(resolve => server.listen(0, "localhost", resolve));
      return String(server.address().port);
    }

    it("waits for delayed backend registration after process health succeeds", async () => {
      let lists = 0;
      const port = await startBackend(() => (++lists < 3 ? [] : [{ name: "noop" }]));

      await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig);

      expect(lists).toBe(3);
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("ready with 1 tool(s) after 3 attempt(s)"));
    });

    it.each([
      [undefined, "readiness-test-key", "a numeric MCP_GATEWAY_PORT"],
      ["1234", undefined, "MCP_GATEWAY_AGENT_ID"],
    ])("requires safe-output gateway configuration with a stable error code", async (port, agentId, message) => {
      await expect(waitForSafeOutputsReadiness(config, port, agentId, retryConfig)).rejects.toThrow(`ERR_SAFE_OUTPUTS_GATEWAY_CONFIG: MCP gateway safeoutputs readiness requires ${message}`);
    });

    it.each(["safeoutputs", "safe-outputs"])("probes the exact configured %s backend endpoint", async serverName => {
      const port = await startBackend(() => [{ name: "noop" }], { serverName });

      await waitForSafeOutputsReadiness({ mcpServers: { [serverName]: {} } }, port, "readiness-test-key", retryConfig);

      expect(requests.filter(method => method === "tools/list")).toHaveLength(1);
    });

    it("retries transient HTTP server failures before registration succeeds", async () => {
      const port = await startBackend(() => [{ name: "noop" }], { initStatus: 503 });

      await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig);

      expect(requests.filter(method => method === "initialize")).toHaveLength(2);
    });

    it("retries connection resets and then empty tools before registration succeeds", async () => {
      let lists = 0;
      const port = await startBackend(() => (++lists < 2 ? [] : [{ name: "noop" }]), { resetFirstInitialize: true });

      await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig);

      expect(requests.filter(method => method === "initialize")).toHaveLength(3);
      expect(lists).toBe(2);
      expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("initialize failed"));
    });

    it("fails setup explicitly after the bounded attempts when tools stay empty", async () => {
      const port = await startBackend(() => []);

      await expect(waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig)).rejects.toThrow(
        /MCP gateway safeoutputs readiness failed after 4 attempt\(s\): no tools registered or backend unavailable\. The agent will not start/
      );

      expect(requests.filter(method => method === "tools/list")).toHaveLength(4);
      expect(global.core.info).not.toHaveBeenCalledWith(expect.stringContaining("is ready"));
    });

    it("fails immediately without accepting tools after an authentication failure", async () => {
      const port = await startBackend(() => [{ name: "noop" }], { initStatus: 401 });

      const error = await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig).catch(err => err);
      expect(error.message).toContain("permanent authentication or MCP protocol failure");
      expect(error.message).toContain("Last probe: initialize failed (HTTP 401).");
      expect(error.cause).toBeInstanceOf(Error);
      expect(error.stack).not.toContain("readiness-test-key");

      expect(requests).toEqual(["initialize"]);
      expect(JSON.stringify(global.core.warning.mock.calls)).not.toContain("readiness-test-key");
    });

    it.each(['{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"readiness-test-key"}}', "malformed readiness-test-key"])("fails immediately on permanent tools/list protocol failures", async listResponse => {
      const port = await startBackend(() => [], { listResponse });

      const error = await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig).catch(err => err);
      expect(error.message).toContain("permanent authentication or MCP protocol failure");
      expect(error.message).toContain("Last probe: tools/list failed (HTTP 200).");
      expect(error.cause).toBeInstanceOf(Error);
      expect(error.stack).not.toContain("readiness-test-key");

      expect(requests.filter(method => method === "initialize")).toHaveLength(1);
      expect(JSON.stringify(global.core.warning.mock.calls)).not.toContain("readiness-test-key");
    });

    it("includes the failing phase and whitelisted connection code in the final error", async () => {
      const port = await startBackend(() => [], { resetEveryInitialize: true });
      const error = await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig).catch(err => err);

      expect(error.message).toContain("Last probe: initialize failed (ECONNRESET).");
      expect(error.cause).toBeInstanceOf(Error);
      expect(error.stack).not.toContain("readiness-test-key");
      expect(error.stack).not.toContain(`localhost:${port}`);
      expect(requests).toEqual(["initialize", "initialize", "initialize", "initialize"]);
    });

    it.each([1, 300, 749])("skips a near-deadline probe with only %ims remaining", async remainingMs => {
      const port = await startBackend(() => [{ name: "noop" }]);
      const now = Date.now();
      vi.spyOn(Date, "now").mockReturnValue(now);
      const startupStart = now - retryConfig.backendStartupTimeoutMs - MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS + remainingMs;
      const error = await waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig, startupStart).catch(err => err);

      expect(error.message).toBe(
        "ERR_SYSTEM: MCP gateway safeoutputs readiness failed after 0 attempt(s): no tools registered or backend unavailable. The agent will not start. Check MCP gateway startup logs for connection resets (ECONNRESET) or delayed backend registration. Last probe: insufficient startup budget for a readiness probe (requires at least 750ms)."
      );
      expect(error.cause.message).toBe("ERR_SYSTEM: insufficient startup budget for a readiness probe (requires at least 750ms)");
      expect(requests).toEqual([]);
      expect(global.core.warning).not.toHaveBeenCalled();
      expect(Date.now()).toBe(now);
    });

    it("does not begin another readiness window after late health success exhausts startup", async () => {
      const port = await startBackend(() => [{ name: "noop" }]);
      const now = Date.now();
      vi.spyOn(Date, "now").mockReturnValue(now);
      const startupStart = now - retryConfig.backendStartupTimeoutMs - MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS;

      await expect(waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig, startupStart)).rejects.toThrow(/readiness failed after 0 attempt/);

      expect(requests).toEqual([]);
    });

    it("uses only the remaining original startup budget after late health success", async () => {
      const now = Date.now();
      let clock = now;
      vi.spyOn(Date, "now").mockImplementation(() => clock);
      const startupStart = now - retryConfig.backendStartupTimeoutMs - MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS + 750;
      const port = await startBackend(() => {
        clock += 750;
        return [];
      });

      await expect(waitForSafeOutputsReadiness(config, port, "readiness-test-key", retryConfig, startupStart)).rejects.toThrow(/readiness failed after 1 attempt/);

      expect(requests.filter(method => method === "initialize")).toHaveLength(1);
    });

    it("caps health probes and readiness backoff to one shared deadline", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(1000);
      const deadline = 1100;
      const health = vi.fn(async remainingMs => {
        expect(remainingMs).toBe(100);
        await new Promise(resolve => setTimeout(resolve, 90));
      });
      const healthWait = retryGatewayStartup(health, retryConfig, deadline, "health");
      await vi.advanceTimersByTimeAsync(90);
      await healthWait;
      const readiness = vi.fn(async remainingMs => {
        expect(remainingMs).toBe(10);
        throw new Error("empty tools");
      });
      const readinessWait = retryGatewayStartup(readiness, { ...retryConfig, initialDelayMs: 1000, maxDelayMs: 1000 }, deadline, "readiness");
      const rejection = expect(readinessWait).rejects.toThrow(/startup deadline exceeded/);
      await vi.advanceTimersByTimeAsync(10);
      await rejection;

      expect(Date.now()).toBe(deadline);
      expect(readiness).toHaveBeenCalledTimes(1);
    });

    it("rejects a probe that finishes at the startup deadline", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(1000);
      const wait = retryGatewayStartup(
        async remainingMs => {
          await new Promise(resolve => setTimeout(resolve, remainingMs));
        },
        retryConfig,
        1010,
        "readiness"
      );
      const rejection = expect(wait).rejects.toThrow(/startup deadline exceeded/);
      await vi.advanceTimersByTimeAsync(10);
      await rejection;
    });

    it("bounds health request duration even when the response body keeps streaming", async () => {
      let interval;
      server = http.createServer((_req, res) => {
        res.writeHead(200, { "Content-Type": "text/plain" });
        res.write("starting");
        interval = setInterval(() => res.write("."), 5);
        res.on("close", () => clearInterval(interval));
      });
      await new Promise(resolve => server.listen(0, "localhost", resolve));
      const started = Date.now();
      try {
        await expect(httpGet(`http://localhost:${server.address().port}/health`, 40)).rejects.toThrow("timeout");
        expect(Date.now() - started).toBeLessThan(500);
      } finally {
        clearInterval(interval);
      }
    });

    it.each([{}, { mcpServers: {} }, { mcpServers: { github: {} } }])("skips workflows without configured safe outputs: %j", async noSafeOutputs => {
      await waitForSafeOutputsReadiness(noSafeOutputs, "invalid-port", "readiness-test-key", retryConfig);
      expect(global.core.info).not.toHaveBeenCalled();
    });

    it("gates startup completion and outputs on safeoutputs readiness", () => {
      const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
      const gate = source.indexOf("await waitForSafeOutputsReadiness(configObj, gatewayPort, agentId, healthRetryConfig, gatewayStartTime)");
      expect(gate).toBeGreaterThan(0);
      expect(gate).toBeLessThan(source.indexOf("writeGatewayStartupMarker(gatewayStartupMarker)"));
      expect(gate).toBeLessThan(source.indexOf("fs.appendFileSync(githubOutput, outputs"));
      expect(source).toContain("stopGatewayProcess(activeGatewayPid)");
      expect(source).toContain("function setFailed(message)");
      expect(source).toContain("core.setFailed(/^ERR_[A-Z]+:/.test(text) ? text : `${ERR_SYSTEM}: ${text}`)");
      expect(source).toContain("setFailed(getErrorMessage(err));");
      expect(source).toContain("setFailed(message);");
      expect(source).toContain("const startupDeadline = gatewayStartTime + healthRetryConfig.backendStartupTimeoutMs + MCP_GATEWAY_HEALTH_REGISTRATION_MARGIN_MS");
      expect(source).toContain("httpGet(healthUrl, Math.min(2000, remainingMs))");
    });
  });

  it("does not include the configured Docker command in syntax errors", () => {
    const source = fs.readFileSync(new URL("./start_mcp_gateway.cjs", import.meta.url), "utf8");
    expect(source).toContain("setFailed(`${ERR_CONFIG}: MCP_GATEWAY_DOCKER_COMMAND has incorrect syntax; expected docker run command with image and arguments`);");
    expect(source).not.toContain("has incorrect syntax — got: ${dockerCommand}");
  });

  it("redacts bearer headers and structured credential keys", () => {
    const sample = ["Authorization: ******", '{"agentId":"redact-me"}', "token=redact-me"].join("\n");
    const redacted = redactGatewayDiagnostics(sample);
    expect(redacted).not.toContain("redact-me");
    expect(redacted.match(/\[REDACTED\]/g)).toHaveLength(3);
  });

  it("coalesces registered proxy URLs with overlapping credential patterns", () => {
    const proxy = `http://token:${"alpha,beta"}@proxy.example:8080`;
    const redacted = redactGatewayDiagnostics(`gateway proxy ${proxy}`, { HTTPS_PROXY: proxy });

    expect(redacted).toBe("gateway proxy ***");
    expect(redacted).not.toContain("alpha,beta");
    expect(redacted).not.toContain("beta@proxy.example");
  });

  it("preserves credential keys when an environment value overlaps the key", () => {
    const redacted = redactGatewayDiagnostics("token=credential-value", { HTTP_PROXY: "token" });

    expect(redacted).toBe("token=[REDACTED]");
    expect(redacted).not.toContain("credential-value");
  });

  it("redacts earlier gateway diagnostics using later mask registrations", () => {
    const value = 'synthetic-runtime"\\proxy-credential';
    const redacted = redactGatewayDiagnostics(`proxy failed: ${value}\nescaped: ${JSON.stringify(value)}\n::add-mask::${value}`, {});

    expect(redacted).toContain("proxy failed:");
    expect(redacted).not.toContain(value);
    expect(redacted).not.toContain(JSON.stringify(value).slice(1, -1));
    expect(redacted).not.toContain("::add-mask::");
  });
});

describe("start_mcp_gateway startup marker", () => {
  it("defaults to the path read by print_firewall_logs.sh", () => {
    expect(getGatewayStartupMarkerPath({})).toBe("/tmp/gh-aw/mcp-gateway-started");
  });

  it("honors the MCP_GATEWAY_STARTUP_MARKER override", () => {
    expect(getGatewayStartupMarkerPath({ MCP_GATEWAY_STARTUP_MARKER: "/tmp/custom-marker" })).toBe("/tmp/custom-marker");
  });

  it("clears a stale marker and recreates it with owner-only permissions", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-marker-"));
    const markerPath = path.join(dir, "nested", "mcp-gateway-started");
    fs.mkdirSync(path.dirname(markerPath), { recursive: true });
    fs.writeFileSync(markerPath, "stale");

    clearGatewayStartupMarker(markerPath);
    expect(fs.existsSync(markerPath)).toBe(false);

    writeGatewayStartupMarker(markerPath);
    expect(fs.existsSync(markerPath)).toBe(true);
    expect(fs.statSync(markerPath).mode & 0o777).toBe(0o600);

    fs.rmSync(dir, { recursive: true, force: true });
  });
});

describe("start_mcp_gateway custom environment arguments", () => {
  const marker = "__GH_AW_MCP_GATEWAY_CUSTOM_ENV__";

  it("passes hostile values as one atomic Docker argument", () => {
    const hostileValue = `x" --privileged -v /workspace/evil:/evil --entrypoint /evil -e X="x`;
    const args = injectCustomGatewayEnvArgs(["run", "--rm", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["BASH_ENV"]',
      [`${customGatewayEnvTransportPrefix}0`]: hostileValue,
    });

    expect(args).toEqual(["run", "--rm", "-e", `BASH_ENV=${hostileValue}`, "gateway-image"]);
  });

  it("preserves sorted multi-value index mapping and empty values", () => {
    const args = injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["ALPHA","EMPTY","OMEGA"]',
      [`${customGatewayEnvTransportPrefix}0`]: "first",
      [`${customGatewayEnvTransportPrefix}1`]: "",
      [`${customGatewayEnvTransportPrefix}2`]: "last\nline",
    });

    expect(args).toEqual(["run", "-e", "ALPHA=first", "-e", "EMPTY=", "-e", "OMEGA=last\nline", "gateway-image"]);
  });

  it("uses an empty value when transport metadata is missing", () => {
    const args = injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
      [customGatewayEnvNamesVar]: '["PRESENT","MISSING"]',
      [`${customGatewayEnvTransportPrefix}0`]: "value",
    });

    expect(args).toEqual(["run", "-e", "PRESENT=value", "-e", "MISSING=", "gateway-image"]);
  });

  it("leaves commands without the marker unchanged", () => {
    const args = ["run", "--rm", "gateway-image"];
    expect(injectCustomGatewayEnvArgs(args, { [customGatewayEnvNamesVar]: "not-json" })).toBe(args);
  });

  it("rejects malformed JSON metadata", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: "not-json",
      })
    ).toThrow(/must be valid JSON/);
  });

  it("rejects malformed or unsafe environment variable names", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: '["BAD-NAME"]',
      })
    ).toThrow(/valid environment variable names/);
  });

  it.each([[`${customGatewayEnvTransportPrefix}0`], [customGatewayEnvNamesVar], [`${customGatewayReservedEnvPrefix}FOO`]])("rejects the reserved name %s", reservedName => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: JSON.stringify([reservedName]),
      })
    ).toThrow(/reserved/);
  });

  it("rejects duplicate environment variable names", () => {
    expect(() =>
      injectCustomGatewayEnvArgs(["run", marker, "gateway-image"], {
        [customGatewayEnvNamesVar]: '["API_TOKEN","API_TOKEN"]',
      })
    ).toThrow(/duplicate/);
  });
});

describe("start_mcp_gateway OTLP if-missing helpers", () => {
  let originalWarning;

  beforeEach(() => {
    originalWarning = global.core.warning;
    global.core.warning = vi.fn();
  });

  afterEach(() => {
    delete process.env.GH_AW_OTLP_IF_MISSING;
    global.core.warning = originalWarning;
  });

  it("normalizes if-missing mode", () => {
    expect(getOTLPIfMissingMode(undefined)).toBe("error");
    expect(getOTLPIfMissingMode(" warn ")).toBe("warn");
    expect(getOTLPIfMissingMode("ignore")).toBe("ignore");
    expect(getOTLPIfMissingMode("invalid")).toBe("error");
  });

  it("detects non-empty OTLP headers for string/map/array forms", () => {
    expect(hasNonEmptyOTLPHeaders("")).toBe(false);
    expect(hasNonEmptyOTLPHeaders("Authorization=Bearer token")).toBe(true);
    expect(hasNonEmptyOTLPHeaders({ Authorization: "" })).toBe(false);
    expect(hasNonEmptyOTLPHeaders({ Authorization: "Bearer token" })).toBe(true);
    expect(hasNonEmptyOTLPHeaders(["", "  "])).toBe(false);
    expect(hasNonEmptyOTLPHeaders(["", "token"])).toBe(true);
  });

  it("is a no-op when if-missing mode is unset/error", () => {
    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "   ",
          headers: "",
        },
      },
    };
    applyOTLPIgnoreIfMissing(config);
    expect(config.gateway.opentelemetry).toEqual({
      endpoint: "   ",
      headers: "",
    });
  });

  it("removes opentelemetry when endpoint is empty for warn mode and emits a warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "warn";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "   ",
          headers: { Authorization: "" },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry).toBeUndefined();
    expect(warningSpy).toHaveBeenCalledOnce();
    expect(warningSpy).toHaveBeenCalledWith(expect.stringContaining("OTLP endpoint is missing/empty"));
  });

  it("removes empty headers object for warn mode and emits a warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "warn";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "https://collector.example/v1/traces",
          headers: { Authorization: "", "X-Tenant": "   " },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry.headers).toBeUndefined();
    expect(warningSpy).toHaveBeenCalledOnce();
    expect(warningSpy).toHaveBeenCalledWith(expect.stringContaining("OTLP headers are missing/empty"));
  });

  it("removes empty headers object for ignore mode without warning", () => {
    const warningSpy = vi.fn();
    global.core.warning = warningSpy;
    process.env.GH_AW_OTLP_IF_MISSING = "ignore";

    const config = {
      gateway: {
        opentelemetry: {
          endpoint: "https://collector.example/v1/traces",
          headers: { Authorization: "" },
        },
      },
    };

    applyOTLPIgnoreIfMissing(config);

    expect(config.gateway.opentelemetry.headers).toBeUndefined();
    expect(warningSpy).not.toHaveBeenCalled();
  });
});

// -----------------------------------------------------------------------------
// resolveCopilotConfigPaths — guards against the regression where /home/runner
// was hard-coded and broke self-hosted runners with HOME != /home/runner.
// -----------------------------------------------------------------------------
describe("start_mcp_gateway resolveCopilotConfigPaths", () => {
  let originalHome;

  beforeEach(() => {
    originalHome = process.env.HOME;
  });

  afterEach(() => {
    if (originalHome === undefined) {
      delete process.env.HOME;
    } else {
      process.env.HOME = originalHome;
    }
  });

  it("resolves the Copilot config dir under the runtime $HOME", () => {
    process.env.HOME = "/home/runner";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/home/runner/.copilot",
      file: "/home/runner/.copilot/mcp-config.json",
    });
  });

  it("respects a self-hosted runner HOME (not /home/runner)", () => {
    process.env.HOME = "/home/actions";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/home/actions/.copilot",
      file: "/home/actions/.copilot/mcp-config.json",
    });
  });

  it("respects a containerized HOME (/root)", () => {
    process.env.HOME = "/root";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/root/.copilot",
      file: "/root/.copilot/mcp-config.json",
    });
  });

  it("handles HOME with spaces and special characters via path.join", () => {
    process.env.HOME = "/var/lib/actions runner";
    expect(resolveCopilotConfigPaths()).toEqual({
      dir: "/var/lib/actions runner/.copilot",
      file: "/var/lib/actions runner/.copilot/mcp-config.json",
    });
  });

  it("throws (not exits) when HOME is unset so tests can exercise the branch", () => {
    delete process.env.HOME;
    expect(() => resolveCopilotConfigPaths()).toThrow(/HOME environment variable is not set/);
  });

  it("throws when HOME is empty string", () => {
    process.env.HOME = "";
    expect(() => resolveCopilotConfigPaths()).toThrow(/HOME environment variable is not set/);
  });

  it("never returns a path containing the literal /home/runner when HOME is different", () => {
    process.env.HOME = "/opt/actions/home";
    const { dir, file } = resolveCopilotConfigPaths();
    expect(dir).not.toContain("/home/runner");
    expect(file).not.toContain("/home/runner");
  });
});

describe("start_mcp_gateway detectEngineType", () => {
  const configDir = "/tmp/gh-aw/mcp-config";

  it("does not require HOME for an explicit non-copilot engine", () => {
    expect(detectEngineType(configDir, { GH_AW_ENGINE: "codex" }, () => false)).toBe("codex");
  });

  it("does not require HOME when auto-detecting codex", () => {
    const existsSync = vi.fn(p => p === `${configDir}/config.toml`);
    expect(detectEngineType(configDir, {}, existsSync)).toBe("codex");
    expect(existsSync).not.toHaveBeenCalledWith("/.copilot");
  });

  it("auto-detects copilot from the HOME-scoped config directory", () => {
    const env = { HOME: "/var/lib/actions runner" };
    const existsSync = vi.fn(p => p === "/var/lib/actions runner/.copilot");
    expect(detectEngineType(configDir, env, existsSync)).toBe("copilot");
  });
});

describe("start_mcp_gateway getJSONParseErrorContext", () => {
  it("extracts line/column and key for invalid escape values", () => {
    const invalidConfig = `{
  "mcpServers": {
    "github": {
      "env": {
        "GITHUB_HOST": "\\https://github.com"
      }
    }
  }
}`;
    let parseErrorMessage = "";
    try {
      JSON.parse(invalidConfig);
    } catch (err) {
      parseErrorMessage = /** @type {Error} */ err.message;
    }
    const context = getJSONParseErrorContext(invalidConfig, parseErrorMessage);
    expect(context).toBeTruthy();
    expect(context?.key).toBe("GITHUB_HOST");
    expect(context?.lineText).toContain(`"GITHUB_HOST"`);
  });
});

describe("start_mcp_gateway normalizeSinkVisibilityEncoding", () => {
  it("normalizes double-encoded sink visibility values", () => {
    const invalidConfig = `{
  "guard-policies": {
    "write-sink": {
      "sink-visibility": ""public""
    }
  }
}`;
    expect(normalizeSinkVisibilityEncoding(invalidConfig)).toContain(`"sink-visibility": "public"`);
  });

  it("leaves correctly encoded sink visibility values unchanged", () => {
    const validConfig = `{
  "guard-policies": {
    "write-sink": {
      "sink-visibility": "public"
    }
  }
}`;
    expect(normalizeSinkVisibilityEncoding(validConfig)).toBe(validConfig);
  });
});

describe("start_mcp_gateway extractOptionalServerNames", () => {
  it("writes explicit required settings before extracting them for gateway startup", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-mcp-required-"));
    try {
      const configObj = { mcpServers: { optional: { type: "http", required: false } } };
      writeMcpConfiguration(directory, configObj);
      expect(extractOptionalServerNames(configObj)).toEqual(["optional"]);
      const snapshot = JSON.parse(fs.readFileSync(path.join(directory, "configuration.jsonl"), "utf8"));
      expect(snapshot.data.servers).toEqual([{ serverName: "optional", transport: "http", required: false }]);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("collects servers declared with required: false and strips the flag from every server", () => {
    const configObj = {
      mcpServers: {
        datadog: { type: "http", url: "https://example.com/mcp", required: false },
        grafana: { type: "http", url: "https://example.com/grafana" },
        sentry: { type: "http", url: "https://example.com/sentry", required: true },
      },
    };

    expect(extractOptionalServerNames(configObj)).toEqual(["datadog"]);
    // The gateway configuration specification has no `required` field, so it is
    // removed for every server regardless of its value.
    expect(configObj.mcpServers.datadog).not.toHaveProperty("required");
    expect(configObj.mcpServers.sentry).not.toHaveProperty("required");
    expect(configObj.mcpServers.grafana).not.toHaveProperty("required");
  });

  it("returns an empty list when no servers are configured", () => {
    expect(extractOptionalServerNames({})).toEqual([]);
    expect(extractOptionalServerNames({ mcpServers: null })).toEqual([]);
  });
});

describe("start_mcp_gateway gateway agent identifier validation", () => {
  it.each([
    ["accepts a singular agent ID", { agentId: "agent-1" }, null],
    ["accepts one plural agent ID", { agentIds: ["agent-1"] }, null],
    ["accepts multiple plural agent IDs", { agentIds: ["agent-1", "agent-2"] }, null],
    ["rejects an empty plural agent ID list", { agentIds: [] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects a non-array plural agent ID", { agentIds: "agent-1" }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects an empty ID in the plural list", { agentIds: ["agent-1", ""] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects a non-string ID in the plural list", { agentIds: ["agent-1", 2] }, "ERROR: Gateway 'agentIds' must be a non-empty array of non-empty strings"],
    ["rejects both identifier forms", { agentId: "agent-1", agentIds: ["agent-1"] }, "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"],
    ["rejects neither identifier form", {}, "ERROR: Gateway configuration must specify exactly one of 'agentId' or 'agentIds'"],
  ])("%s", (_name, gateway, expectedError) => {
    expect(validateGatewayAgentIdentifiers(gateway)).toBe(expectedError);
  });

  it("rejects an empty or malformed singular agent ID", () => {
    expect(validateGatewayAgentIdentifiers({ agentId: "" })).toBe("ERROR: Gateway 'agentId' must be a non-empty string");
    expect(validateGatewayAgentIdentifiers({ agentId: ["agent-1"] })).toBe("ERROR: Gateway 'agentId' must be a non-empty string");
  });
});
