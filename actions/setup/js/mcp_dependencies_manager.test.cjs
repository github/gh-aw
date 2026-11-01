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

  it("never forwards server or tool credentials to dependency installers", async () => {
    vi.stubEnv("GH_AW_MCP_SCRIPTS_API_KEY", "server-secret");
    vi.stubEnv("GH_AW_MCP_SCRIPTS_ENV_616c706861_4150495f4b4559", "tool-secret");
    vi.stubEnv("JOB_SECRET", "job-secret");
    const execRunner = vi.fn().mockReturnValue(Buffer.from(""));
    const { createDependencyInstallGate, resetDependencyInstallStateForTests, setExecFileSyncRunnerForTests } = await import("./mcp_dependencies_manager.cjs");
    resetDependencyInstallStateForTests();
    setExecFileSyncRunnerForTests(execRunner);
    for (const extension of ["cjs", "py", "go", "sh"]) {
      await createDependencyInstallGate({ debug: vi.fn() }, extension, `/tmp/probe.${extension}`, ["test-dependency"], "/tmp")();
    }
    expect(execRunner.mock.calls.length).toBeGreaterThanOrEqual(5);
    for (const [, , options] of execRunner.mock.calls) {
      expect(options.env.PATH).toBe(process.env.PATH);
      expect(options.env.GH_AW_MCP_SCRIPTS_API_KEY).toBeUndefined();
      expect(options.env.GH_AW_MCP_SCRIPTS_ENV_616c706861_4150495f4b4559).toBeUndefined();
      expect(options.env.JOB_SECRET).toBeUndefined();
    }
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
