import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

describe("mcp_dependencies_manager", () => {
  afterEach(() => vi.unstubAllEnvs());
  beforeEach(() => {
    vi.resetModules();
  });

  it("infers manager from handler extension", async () => {
    const { inferDependencyManager } = await import("./mcp_dependencies_manager.cjs");
    expect(inferDependencyManager("/tmp/tool.py")).toBe("pip");
    expect(inferDependencyManager("/tmp/tool.go")).toBe("go");
    expect(inferDependencyManager("/tmp/tool.sh")).toBe("shell");
    expect(inferDependencyManager("/tmp/tool.cjs")).toBe("npm");
  });

  it("preserves credential-free installer configuration but never forwards server or tool credentials", async () => {
    vi.stubEnv("GH_AW_MCP_SCRIPTS_API_KEY", "server-secret");
    vi.stubEnv("GH_AW_MCP_SCRIPTS_ENV_616c706861_4150495f4b4559", "tool-secret");
    vi.stubEnv("JOB_SECRET", "job-secret");
    vi.stubEnv("GITHUB_TOKEN", "github-secret");
    vi.stubEnv("NPM_TOKEN", "npm-secret");
    vi.stubEnv("PIP_CONFIG_FILE", "/credential-file");
    vi.stubEnv("NPM_CONFIG_USERCONFIG", "/credential-file");
    vi.stubEnv("NODE_OPTIONS", "--require=/server-hook.cjs");
    const config = {
      HTTP_PROXY: "http://proxy.example.com:8080",
      HTTPS_PROXY: "https://proxy.example.com:8443",
      ALL_PROXY: "socks5h://proxy.example.com:1080",
      http_proxy: "http://proxy.example.com:8080",
      https_proxy: "http://proxy.example.com:8080",
      all_proxy: "socks5://proxy.example.com:1080",
      NO_PROXY: "localhost,127.0.0.1,.example.com,10.0.0.0/8,[::1]",
      no_proxy: "*.example.com",
      PIP_INDEX_URL: "https://packages.example.com/simple",
      PIP_EXTRA_INDEX_URL: "https://mirror.example.com/simple https://backup.example.com/simple",
      NPM_CONFIG_REGISTRY: "https://packages.example.com/npm/",
      npm_config_registry: "https://mirror.example.com/npm/",
      GOPROXY: "https://packages.example.com/go|https://mirror.example.com/go,direct",
      NODE_EXTRA_CA_CERTS: "/etc/company-ca.pem",
      SSL_CERT_FILE: "/etc/company-ca.pem",
      SSL_CERT_DIR: "/etc/company-certs",
      REQUESTS_CA_BUNDLE: "/etc/company-ca.pem",
      CURL_CA_BUNDLE: "/etc/company-ca.pem",
      PIP_CERT: "/etc/company-ca.pem",
      NPM_CONFIG_CAFILE: "/etc/company-ca.pem",
      npm_config_cafile: "/etc/company-ca.pem",
    };
    for (const [name, value] of Object.entries(config)) vi.stubEnv(name, value);
    const execRunner = vi.fn().mockReturnValue(Buffer.from(""));
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);
    for (const extension of ["cjs", "py", "go", "sh"]) {
      await createDependencyInstallGate({ debug: vi.fn() }, extension, `probe.${extension}`, ["test-dependency"], ".mcp-deps-test-base")();
    }
    expect(execRunner.mock.calls.length).toBeGreaterThanOrEqual(5);
    for (const [, , options] of execRunner.mock.calls) {
      expect(options.env.PATH).toBe(process.env.PATH);
      expect(options.env.GH_AW_MCP_SCRIPTS_API_KEY).toBeUndefined();
      expect(options.env.GH_AW_MCP_SCRIPTS_ENV_616c706861_4150495f4b4559).toBeUndefined();
      expect(options.env.JOB_SECRET).toBeUndefined();
      for (const name of ["GITHUB_TOKEN", "NPM_TOKEN", "PIP_CONFIG_FILE", "NPM_CONFIG_USERCONFIG", "NODE_OPTIONS"]) expect(options.env).not.toHaveProperty(name);
      expect(options.env).toMatchObject(config);
    }
    expect(execRunner.mock.calls.some(([command, args]) => command === "go" && args[0] === "mod")).toBe(true);
    expect(execRunner.mock.calls.some(([command]) => command === "which")).toBe(true);
  });

  it.each([
    ["HTTPS_PROXY", "http://user:installer-secret@proxy.example.com"],
    ["http_proxy", "http://installer-secret@proxy.example.com"],
    ["PIP_INDEX_URL", "https://user:installer-secret@packages.example.com/simple"],
    ["NPM_CONFIG_REGISTRY", "https://packages.example.com/npm?token=installer-secret"],
    ["npm_config_registry", "https://packages.example.com/npm#installer-secret"],
    ["PIP_EXTRA_INDEX_URL", "https://public.example.com/simple https://user:installer-secret@private.example.com/simple"],
    ["GOPROXY", "https://public.example.com,https://user:installer-secret@private.example.com|direct"],
    ["ALL_PROXY", "file:///installer-secret"],
    ["HTTPS_PROXY", "installer-secret"],
    ["NO_PROXY", "user:installer-secret@proxy.example.com"],
    ["SSL_CERT_FILE", "/cert.pem\ninstaller-secret"],
  ])("rejects unsafe %s before launching installers without echoing values", async (name, value) => {
    vi.stubEnv(name, value);
    const execRunner = vi.fn();
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);
    for (const extension of ["cjs", "py", "go", "sh"]) {
      const gate = createDependencyInstallGate({ debug: vi.fn() }, extension, `probe.${extension}`, ["requests"], ".mcp-deps-test-base");
      await expect(gate()).rejects.toThrow(`Unsafe MCP dependency installer configuration in '${name}'`);
      await expect(gate()).rejects.not.toThrow("installer-secret");
    }
    expect(execRunner).not.toHaveBeenCalled();
  });

  it.each(["direct", "off"])("preserves GOPROXY=%s without treating it as a URL", async value => {
    vi.stubEnv("GOPROXY", value);
    const { createInstallerEnvironment } = await import("./mcp_dependencies_manager.cjs");
    expect(createInstallerEnvironment().GOPROXY).toBe(value);
  });

  it("installs python dependencies before first invocation only", async () => {
    const execRunner = vi.fn().mockReturnValue(Buffer.from(""));
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);

    const logger = { debug: vi.fn(), debugError: vi.fn() };
    const gate = createDependencyInstallGate(logger, "fetch-url", "/tmp/fetch.py", ["requests"], "/tmp");
    await gate();
    await gate();

    const installCalls = execRunner.mock.calls.filter(call => call[0] === "python3" && call[1][0] === "-m");
    expect(installCalls).toHaveLength(1);
    expect(installCalls[0][1]).toEqual(["-m", "pip", "install", "--disable-pip-version-check", "requests"]);
  });

  it("retries transient install failures", async () => {
    const execRunner = vi
      .fn()
      .mockImplementationOnce(() => {
        const error = new Error("timeout");
        error.stderr = Buffer.from("network timeout");
        throw error;
      })
      .mockReturnValueOnce(Buffer.from(""));
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);

    const logger = { debug: vi.fn(), debugError: vi.fn() };
    const gate = createDependencyInstallGate(logger, "fetch-url", "/tmp/fetch.py", ["requests"], "/tmp");
    await gate();

    const installCalls = execRunner.mock.calls.filter(call => call[0] === "python3" && call[1][0] === "-m");
    expect(installCalls).toHaveLength(2);
  });

  it("fails fast on deterministic install failures", async () => {
    const execRunner = vi.fn().mockImplementation(() => {
      const error = new Error("bad package");
      error.stderr = Buffer.from("No matching distribution found for bad package");
      throw error;
    });
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);

    const logger = { debug: vi.fn(), debugError: vi.fn() };
    const gate = createDependencyInstallGate(logger, "fetch-url", "/tmp/fetch.py", ["bad package"], "/tmp");

    await expect(gate()).rejects.toThrow("Dependency installation failed");
    const installCalls = execRunner.mock.calls.filter(call => call[0] === "python3" && call[1][0] === "-m");
    expect(installCalls).toHaveLength(1);
  });
});
