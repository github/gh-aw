import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { runFormalCheck } = require("../../../.github/scripts/formal_spec_check.cjs");
let directory;
let workspace;
let models;
let options;
let calls;

beforeEach(() => {
  directory = mkdtempSync(resolve(".gh-aw-formal-check-"));
  workspace = join(directory, "workspace");
  models = join(workspace, "specs/formal/nested-selected");
  mkdirSync(models, { recursive: true });
  mkdirSync(join(workspace, "specs/nested"));
  writeFileSync(join(workspace, "specs/nested/selected.md"), "# Specification\n");
  writeFileSync(join(models, "Model.tla"), "---- MODULE Model ----\n====\n");
  writeFileSync(join(models, "Model.cfg"), "SPECIFICATION Spec\n");
  writeFileSync(join(models, "main.kk"), 'fun main() { println("checked") }\n');
  calls = [];
  options = {
    workspace,
    resultsRoot: join(directory, "results"),
    java: "/pinned/java",
    jar: "/pinned/tla2tools.jar",
    koka: "/pinned/koka",
    spawnSync: vi.fn((command, args, settings) => {
      calls.push({ command, args, settings });
      if (args.includes("-o")) writeFileSync(args[args.indexOf("-o") + 1], "fixture executable");
      return { status: 0, signal: null };
    }),
  };
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  rmSync(directory, { recursive: true, force: true });
});

describe("bounded formal-spec check helper", () => {
  it.each(["sany", "tlc"])("runs %s with fixed tools, model working directory, bounds and hashed evidence", action => {
    const result = runFormalCheck([action, "specs/nested/selected.md", "Model.tla", ...(action === "tlc" ? ["Model.cfg"] : [])], options);
    expect(result.status).toBe(0);
    expect(result.results).toMatch(new RegExp(`/results/nested-selected/${action}-`));
    expect(calls).toHaveLength(2);
    for (const call of calls) {
      expect(call.command).toBe("timeout");
      expect(call.args.slice(0, 3)).toEqual(["--kill-after=30s", call === calls[0] ? "30s" : "120s", options.java]);
      expect(call.settings.cwd).toBe(models);
      expect(call.settings.timeout).toBeLessThanOrEqual(155000);
    }
    expect(calls[1].args).toContain("-Xmx2g");
    expect(calls[1].args).toContain(options.jar);
    expect(calls[1].args).toContain(action === "tlc" ? "tlc2.TLC" : "tla2sany.SANY");
    if (action === "tlc") {
      expect(calls[1].args).toEqual(expect.arrayContaining(["-workers", "2", "-config", "Model.cfg", "-metadir", join(result.results, "states")]));
    }
    const evidence = JSON.parse(readFileSync(join(result.results, "evidence.json"), "utf8"));
    expect(evidence.sources.map(source => source.path)).toEqual(["specs/nested/selected.md", "specs/formal/nested-selected/Model.tla", ...(action === "tlc" ? ["specs/formal/nested-selected/Model.cfg"] : [])]);
    expect(evidence.sources.every(source => /^[a-f0-9]{64}$/.test(source.sha256))).toBe(true);
    expect(evidence.records.map(record => record.status)).toEqual([0, 0]);
    expect(evidence.records.every(record => record.stdout === "" && record.stderr === "")).toBe(true);
    expect(readdirSync(result.results).some(file => file.endsWith(".log"))).toBe(false);
  });

  it("rejects colliding spec slugs before using a shared model directory", () => {
    writeFileSync(join(workspace, "specs/nested-selected.md"), "# Another specification\n");
    for (const spec of ["specs/nested/selected.md", "specs/nested-selected.md"]) {
      expect(() => runFormalCheck(["sany", spec, "Model.tla"], options)).toThrow(/slug.*collision/i);
    }
    expect(calls).toHaveLength(0);
  });

  it("isolates every child environment, including compiled models, from agent credentials and runtime hooks", () => {
    vi.stubEnv("GH_TOKEN", "fake-agent-token");
    vi.stubEnv("NODE_OPTIONS", "--require=/agent/hook.cjs");
    vi.stubEnv("JAVA_TOOL_OPTIONS", "-javaagent:/agent/hook.jar");
    vi.stubEnv("LD_PRELOAD", "/agent/hook.so");
    vi.stubEnv("PATH", "/agent/bin");
    vi.stubEnv("HOME", "/agent/home");
    vi.stubEnv("TMPDIR", "/agent/tmp");
    const result = runFormalCheck(["koka", "specs/nested/selected.md", "main.kk"], options);
    expect(calls).toHaveLength(3);
    for (const { settings } of calls) {
      expect(settings.env).toEqual({
        PATH: "/usr/local/bin:/usr/bin:/bin",
        HOME: join(result.results, "home"),
        TMPDIR: join(result.results, "tmp"),
        LANG: "C.UTF-8",
        LC_ALL: "C.UTF-8",
        TZ: "UTC",
      });
      expect(readdirSync(settings.env.HOME)).toEqual([]);
      expect(readdirSync(settings.env.TMPDIR)).toEqual([]);
      expect(settings.stdio).toEqual(["ignore", "pipe", "pipe"]);
      expect(settings.maxBuffer).toBe(65536);
    }
  });

  it("persists only bounded redacted output previews and sanitizes process errors", () => {
    const secret = "fake-sensitive-agent-token";
    vi.stubEnv("GH_TOKEN", secret);
    const message = `runtime unavailable: ${secret}`;
    options.spawnSync.mockReturnValue({
      status: null,
      signal: null,
      stdout: Buffer.from(`${secret}${"x".repeat(100000)}`),
      stderr: Buffer.from(`diagnostic ${secret}`),
      error: new Error(message),
    });
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow("runtime unavailable: [REDACTED]");
    const results = join(options.resultsRoot, "nested-selected", readdirSync(join(options.resultsRoot, "nested-selected"))[0]);
    const evidenceText = readFileSync(join(results, "evidence.json"), "utf8");
    const record = JSON.parse(evidenceText).records[0];
    expect(evidenceText).not.toContain(secret);
    expect(record.stdout).toContain("[REDACTED]");
    expect(Buffer.byteLength(record.stdout)).toBeLessThanOrEqual(8192);
    expect(record.stdout_truncated).toBe(true);
    expect(record.stderr).toBe("diagnostic [REDACTED]");
    expect(record.stderr_truncated).toBe(false);
    expect(record.error).toBe("runtime unavailable: [REDACTED]");
    expect(readdirSync(results).some(file => file.endsWith(".log"))).toBe(false);
  });

  it("compiles and runs Koka only in a fresh confined output directory", () => {
    const result = runFormalCheck(["koka", "specs/nested/selected.md", "main.kk"], options);
    expect(result.status).toBe(0);
    expect(calls).toHaveLength(3);
    expect(calls[1].args).toEqual(["--kill-after=30s", "120s", options.koka, `--builddir=${join(result.results, "build")}`, "-o", join(result.results, "model"), "main.kk"]);
    expect(calls[2].args).toEqual(["--kill-after=30s", "30s", join(result.results, "model")]);
    const next = runFormalCheck(["koka", "specs/nested/selected.md", "main.kk"], options);
    expect(next.results).not.toBe(result.results);
  });

  it.each([
    ["shell", "specs/nested/selected.md", "Model.tla"],
    ["sany", "specs/nested/selected.md", "Model.tla", "--unsafe"],
    ["sany", "specs/../outside.md", "Model.tla"],
    ["sany", "specs/nested/selected.md", "../Model.tla"],
    ["sany", "specs/nested/selected.md", "-Model.tla"],
    ["tlc", "specs/nested/selected.md", "Model.tla", "../Model.cfg"],
  ])("rejects unsupported arguments before execution: %j", (...args) => {
    expect(() => runFormalCheck(args, options)).toThrow();
    expect(calls).toHaveLength(0);
  });

  it.each(["model", "model-directory", "results-directory", "results-parent", "spec"])("rejects symlinked %s paths", target => {
    if (target === "model") {
      rmSync(join(models, "Model.tla"));
      symlinkSync(join(models, "Model.cfg"), join(models, "Model.tla"));
    } else if (target === "model-directory") {
      rmSync(models, { recursive: true });
      symlinkSync(join(workspace, "specs/nested"), models);
    } else if (target === "spec") {
      rmSync(join(workspace, "specs/nested/selected.md"));
      symlinkSync(join(models, "Model.cfg"), join(workspace, "specs/nested/selected.md"));
    } else {
      symlinkSync(models, options.resultsRoot);
      if (target === "results-parent") options.resultsRoot = join(options.resultsRoot, "nested");
    }
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/Symlinks|real directory/);
    expect(calls).toHaveLength(0);
  });

  it.each([124, 42])("preserves failing tool status %i and does not execute later stages", status => {
    options.spawnSync.mockImplementationOnce(() => ({ status: 0, signal: null })).mockImplementationOnce(() => ({ status, signal: null }));
    const result = runFormalCheck(["koka", "specs/nested/selected.md", "main.kk"], options);
    expect(result.status).toBe(status);
    expect(options.spawnSync).toHaveBeenCalledTimes(2);
    expect(JSON.parse(readFileSync(join(result.results, "evidence.json"), "utf8")).records[1].status).toBe(status);
  });

  it("fails explicitly on a missing runtime and records process errors", () => {
    options.spawnSync.mockReturnValue({ status: null, signal: null, error: new Error("runtime unavailable") });
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow("runtime unavailable");
    expect(options.spawnSync).toHaveBeenCalledTimes(1);
    expect(() => readFileSync(join(options.resultsRoot, "budget.lock"))).toThrow();
  });

  it.each([480000, -1, "invalid"])("rejects exhausted or corrupt shared budgets: %j", elapsed => {
    mkdirSync(options.resultsRoot);
    writeFileSync(join(options.resultsRoot, "budget.json"), JSON.stringify({ elapsed_ms: elapsed }));
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/budget/);
    expect(calls).toHaveLength(0);
  });

  it("serializes checks and caps the remaining shared budget", () => {
    mkdirSync(options.resultsRoot);
    writeFileSync(join(options.resultsRoot, "budget.lock"), "another check");
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(
      `Formal-check budget lock already exists: ${join(options.resultsRoot, "budget.lock")}. Another check may still be running; remove this lock only after confirming no formal check is active.`
    );
    expect(readFileSync(join(options.resultsRoot, "budget.lock"), "utf8")).toBe("another check");
    expect(calls).toHaveLength(0);
    rmSync(join(options.resultsRoot, "budget.lock"));
    writeFileSync(join(options.resultsRoot, "budget.json"), JSON.stringify({ elapsed_ms: 470000 }));
    runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options);
    expect(calls.every(call => call.args[1] === "10s" || call.args[1] === "9s")).toBe(true);
  });
});
