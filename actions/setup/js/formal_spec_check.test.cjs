import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

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
    processIdentity: pid => (pid === process.pid ? "fixture-owner" : null),
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
  it.each(["sany", "tlc"])("runs %s with fixed tools, snapshotted inputs, bounds and hashed evidence", action => {
    const result = runFormalCheck([action, "specs/nested/selected.md", "Model.tla", ...(action === "tlc" ? ["Model.cfg"] : [])], options);
    expect(result.status).toBe(0);
    expect(result.results).toMatch(new RegExp(`/results/nested-selected/${action}-`));
    expect(calls).toHaveLength(2);
    for (const call of calls) {
      expect(call.command).toBe("timeout");
      expect(call.args.slice(0, 3)).toEqual(["--kill-after=30s", call === calls[0] ? "30s" : "120s", options.java]);
      expect(call.settings.cwd).toBe(join(result.results, "inputs"));
      expect(call.settings.timeout).toBeLessThanOrEqual(155000);
    }
    expect(calls[1].args).toContain("-Xmx2g");
    expect(calls[1].args).toContain(options.jar);
    expect(calls[1].args).toContain(action === "tlc" ? "tlc2.TLC" : "tla2sany.SANY");
    if (action === "tlc") {
      expect(calls[1].args).toEqual(expect.arrayContaining(["-noGenerateSpecTE", "-workers", "2", "-config", "Model.cfg", "-metadir", join(result.results, "states")]));
    }
    const evidence = JSON.parse(readFileSync(join(result.results, "evidence.json"), "utf8"));
    expect(evidence.sources.map(source => source.path)).toEqual(["specs/nested/selected.md", "specs/formal/nested-selected/Model.tla", ...(action === "tlc" ? ["specs/formal/nested-selected/Model.cfg"] : [])]);
    expect(evidence.sources.every(source => /^[a-f0-9]{64}$/.test(source.sha256))).toBe(true);
    expect(evidence.records.map(record => record.status)).toEqual([0, 0]);
    expect(evidence.records.map(record => record.validation_status)).toEqual([0, 0]);
    expect(evidence.records.every(record => record.stdout === "" && record.stderr === "")).toBe(true);
    expect(readdirSync(result.results).some(file => file.endsWith(".log"))).toBe(false);
  });

  it.each([
    ["sany", "EXTENDS Escaped\n"],
    ["tlc", "EXTENDS Naturals, (* nested (* comment *) *) Escaped\n"],
    ["sany", "EXTENDS Naturals \\* a continued declaration\n, Escaped\n"],
    ["sany", "Dependency == INSTANCE Escaped\n"],
    ["sany", "EXTENDS (* comment *) Escaped\n"],
    ["sany", '(* " *)\nEXTENDS Escaped\n(* " *)'],
    ["koka", "import escaped\n"],
    ["koka", 'extern include "escaped.h"\n'],
  ])("rejects unsupported %s dependency before starting any child: %s", (action, dependency) => {
    symlinkSync(join(workspace, "specs/nested/selected.md"), join(models, action === "koka" ? "escaped.kk" : "Escaped.tla"));
    const model = action === "koka" ? "main.kk" : "Model.tla";
    writeFileSync(join(models, model), dependency);
    expect(() => runFormalCheck([action, "specs/nested/selected.md", model, ...(action === "tlc" ? ["Model.cfg"] : [])], options)).toThrow(/Unsupported.*dependency/);
    expect(calls).toHaveLength(0);
  });

  it("uses the hashed snapshot even if the checkout changes, and excludes neighboring standard-module shadows", () => {
    const source = '---- MODULE Model ----\nEXTENDS (* comment *) Naturals, Sequences\nValue == "INSTANCE is not a dependency"\n====\n';
    writeFileSync(join(models, "Model.tla"), source);
    symlinkSync(join(workspace, "specs/nested/selected.md"), join(models, "Naturals.tla"));
    options.spawnSync.mockImplementation((command, args, settings) => {
      writeFileSync(join(models, "Model.tla"), "changed after validation");
      expect(readFileSync(join(settings.cwd, "Model.tla"), "utf8")).toBe(source);
      expect(readdirSync(settings.cwd)).toEqual(["Model.tla"]);
      return { status: 0, signal: null };
    });
    const result = runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options);
    const evidence = JSON.parse(readFileSync(join(result.results, "evidence.json"), "utf8"));
    expect(evidence.sources[1].sha256).toBe(createHash("sha256").update(source).digest("hex"));
  });

  it.each([
    "Semantic errors:\n\n*** Errors: 1\n\nUnknown operator: Missing",
    "Level errors:\n\n*** Errors: 1\n\nLevel error in applying operator",
    "*** Abort messages: 1\nModule could not be analyzed",
    "***Parse Error***\nEncountered invalid token",
    `${"x".repeat(9000)}\n*** Errors: 1\nSemantic failure beyond the persisted preview`,
  ])("fails zero-exit SANY diagnostics while preserving raw process status: %s", diagnostic => {
    options.spawnSync.mockReturnValueOnce({ status: 0, signal: null }).mockReturnValueOnce({ status: 0, signal: null, stdout: Buffer.from(diagnostic) });
    const result = runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options);
    expect(result.status).toBe(1);
    const record = JSON.parse(readFileSync(join(result.results, "evidence.json"), "utf8")).records[1];
    expect(record.status).toBe(0);
    expect(record.validation_status).toBe(1);
    expect(record.stdout).toBe(diagnostic.slice(0, 8192));
  });

  it("does not mistake zero-exit SANY warnings for errors", () => {
    options.spawnSync.mockReturnValueOnce({ status: 0, signal: null }).mockReturnValueOnce({ status: 0, signal: null, stdout: Buffer.from("*** Warnings: 1\nUnused operator") });
    expect(runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options).status).toBe(0);
  });

  it("suppresses TLC trace-explorer generation for negative controls without changing source files", () => {
    const before = readdirSync(models);
    options.spawnSync.mockReturnValueOnce({ status: 0, signal: null }).mockImplementationOnce((_command, args, settings) => {
      expect(args).toContain("-noGenerateSpecTE");
      expect(settings.cwd).not.toBe(models);
      return { status: 12, signal: null, stdout: Buffer.from("Invariant NegativeControl is violated.") };
    });
    const result = runFormalCheck(["tlc", "specs/nested/selected.md", "Model.tla", "Model.cfg"], options);
    expect(result.status).toBe(12);
    expect(readdirSync(models)).toEqual(before);
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
    expect(readFileSync(join(options.resultsRoot, "budget.lock.done"), "utf8")).toBe("");
    expect(JSON.parse(readFileSync(join(options.resultsRoot, "budget.json"), "utf8")).elapsed_ms).toBe(480000);
  });

  it.each([480000, -1, "invalid"])("rejects exhausted or corrupt shared budgets: %j", elapsed => {
    mkdirSync(options.resultsRoot);
    writeFileSync(join(options.resultsRoot, "budget.json"), JSON.stringify({ elapsed_ms: elapsed }));
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/budget/);
    expect(calls).toHaveLength(0);
  });

  it("waits for live owners without deleting their lock and caps the remaining shared budget", () => {
    mkdirSync(options.resultsRoot);
    const owner = JSON.stringify({ pid: process.pid, identity: "fixture-owner" });
    writeFileSync(join(options.resultsRoot, "budget.lock"), owner);
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/still running; wait/);
    expect(readFileSync(join(options.resultsRoot, "budget.lock"), "utf8")).toBe(owner);
    expect(calls).toHaveLength(0);
    rmSync(join(options.resultsRoot, "budget.lock"));
    writeFileSync(join(options.resultsRoot, "budget.json"), JSON.stringify({ elapsed_ms: 470000 }));
    runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options);
    expect(calls.every(call => call.args[1] === "10s" || call.args[1] === "9s")).toBe(true);
  });

  it.each([null, "reused-pid-start"])("recovers a dead/reused owner (%j) without refunding interrupted children", identity => {
    mkdirSync(options.resultsRoot);
    const owner = JSON.stringify({ pid: 12345, identity: "old-process-start" });
    writeFileSync(join(options.resultsRoot, "budget.lock"), owner);
    writeFileSync(join(options.resultsRoot, "budget.json"), JSON.stringify({ elapsed_ms: 480000 }));
    options.processIdentity = pid => (pid === process.pid ? "fixture-owner" : identity);
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/budget is exhausted/);
    expect(readFileSync(join(options.resultsRoot, "budget.lock"), "utf8")).toBe(owner);
    expect(readFileSync(join(options.resultsRoot, "budget.lock.1.done"), "utf8")).toBe("");
    expect(calls).toHaveLength(0);
  });

  it("can recover an owner that died before reserving, and never lets a competing owner spend the same budget", () => {
    mkdirSync(options.resultsRoot);
    writeFileSync(join(options.resultsRoot, "budget.lock"), JSON.stringify({ pid: 12345, identity: "old-process-start" }));
    options.spawnSync.mockImplementation(() => {
      expect(JSON.parse(readFileSync(join(options.resultsRoot, "budget.json"), "utf8")).elapsed_ms).toBe(480000);
      expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/still running; wait/);
      return { status: 0, signal: null };
    });
    const result = runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options);
    expect(result.status).toBe(0);
    expect(options.spawnSync).toHaveBeenCalledTimes(2);
    expect(readFileSync(join(options.resultsRoot, "budget.lock.1.done"), "utf8")).toBe("");
  });

  it("forfeits the reservation on uncertain child termination and cannot execute another check", () => {
    options.spawnSync.mockImplementation(() => {
      expect(JSON.parse(readFileSync(join(options.resultsRoot, "budget.json"), "utf8")).elapsed_ms).toBe(480000);
      return { status: null, signal: "SIGTERM" };
    });
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/terminated by SIGTERM/);
    expect(() => runFormalCheck(["sany", "specs/nested/selected.md", "Model.tla"], options)).toThrow(/budget is exhausted/);
    expect(options.spawnSync).toHaveBeenCalledTimes(1);
  });

  it("stages a checksum-verified helper and trusted workspace configuration outside the editable checkout", () => {
    const source = readFileSync(new URL("../../../.github/workflows/daily-formal-spec-verifier.md", import.meta.url), "utf8");
    const metadata = readDocumentMetadata(source.match(/^---\n([\s\S]*?)\n---/)[1], { version: "1.2", uniqueKeys: true }).value;
    const setup = metadata.steps.find(step => step.name === "Stage verified Formal checker");
    expect(setup.if).toBe("github.ref == format('refs/heads/{0}', github.event.repository.default_branch)");
    expect(metadata.sandbox.agent.mounts).toContain("/opt/gh-aw/formal-check:/opt/gh-aw/formal-check:ro");
    expect(metadata.tools.bash).toContain("node /opt/gh-aw/formal-check/formal_spec_check.cjs:*");
    expect(metadata.tools.bash).not.toContain("node .github/scripts/formal_spec_check.cjs:*");
    expect(source).toContain("Koka's native C compiler/header availability through the helper's compile");
    const staged = join(directory, "protected-checker");
    mkdirSync(join(workspace, ".github/scripts"), { recursive: true });
    const original = readFileSync(new URL("../../../.github/scripts/formal_spec_check.cjs", import.meta.url));
    const checkout = join(workspace, ".github/scripts/formal_spec_check.cjs");
    writeFileSync(checkout, original);
    const checksumAdapter = process.platform === "darwin" ? 'sha256sum() { shasum -a 256 "$@"; }\n' : "";
    const script = checksumAdapter + setup.run.replaceAll("/opt/gh-aw/formal-check", staged).replaceAll("sudo ", "");
    const result = spawnSync("bash", ["-c", script], { env: { ...process.env, GITHUB_WORKSPACE: workspace }, encoding: "utf8" });
    expect(result.status, result.stderr).toBe(0);
    expect(readFileSync(join(staged, "formal_spec_check.cjs"))).toEqual(original);
    expect(JSON.parse(readFileSync(join(staged, "workspace.json"), "utf8"))).toEqual({ workspace });
    const missing = spawnSync(process.execPath, [join(staged, "formal_spec_check.cjs"), "sany", "specs/missing.md", "Model.tla"], { encoding: "utf8" });
    expect(missing.status).toBe(1);
    expect(missing.stderr).toContain(join(workspace, "specs/missing.md"));
    writeFileSync(checkout, "throw new Error('editable checkout replacement');\n");
    expect(readFileSync(join(staged, "formal_spec_check.cjs"))).toEqual(original);
    const rejected = spawnSync("bash", ["-c", script], { env: { ...process.env, GITHUB_WORKSPACE: workspace }, encoding: "utf8" });
    expect(rejected.status).not.toBe(0);
    expect(rejected.stdout).toContain("FAILED");
  });
});
