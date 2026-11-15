// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { evaluate, initialize, main } = require("./write_gate_verdict.cjs");
let directory;
let filename;
let previousCore;

beforeEach(() => {
  directory = fs.mkdtempSync(path.join(os.tmpdir(), "write-gate-verdict-"));
  filename = path.join(directory, "decisions.json");
  previousCore = globalThis.core;
  globalThis.core = { exportVariable: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn(), info: vi.fn() };
});

afterEach(() => {
  globalThis.core = previousCore;
  vi.unstubAllEnvs();
  fs.rmSync(directory, { recursive: true, force: true });
});

function write(value) {
  fs.writeFileSync(filename, JSON.stringify(value));
}

describe("write gate verdict", () => {
  it.each(["allow", "deny", "partial"])("accepts a completed %s gate", decision => {
    write({ decision, reason: "policy.allowed-1", ...(decision === "partial" ? { items: [] } : {}) });
    expect(evaluate({ filename, outcomes: ["success"] })).toMatchObject({ decision, reason: "policy.allowed-1", error: false, sha256: expect.stringMatching(/^[a-f0-9]{64}$/) });
  });

  it.each(["failure", "cancelled", "timed_out", "skipped", "", null, true])("rejects gate outcome %j despite an allow file", outcome => {
    write({ decision: "allow", reason: "approved" });
    expect(evaluate({ filename, outcomes: ["success", outcome] })).toMatchObject({ decision: "deny", reason: "gate_error", error: true });
  });

  it.each(["", "A", "bad\nreason", "x".repeat(65)])("rejects malformed reason %j", reason => {
    write({ decision: "allow", reason });
    expect(evaluate({ filename, outcomes: ["success"] }).decision).toBe("deny");
  });

  it.each([null, [], { decision: "unknown", reason: "ok" }, { decision: "allow" }, { decision: "partial", reason: "ok" }])("rejects malformed document %j", value => {
    write(value);
    expect(evaluate({ filename, outcomes: ["success"] })).toMatchObject({ decision: "deny", reason: "gate_error" });
  });

  it("rejects absent files, malformed JSON, duplicate fields, symlinks, and invalid UTF-8", () => {
    const check = () => expect(evaluate({ filename, outcomes: ["success"] }).decision).toBe("deny");
    check();
    fs.writeFileSync(filename, "{");
    check();
    fs.writeFileSync(filename, '{"decision":"deny","decision":"allow","reason":"ok"}');
    check();
    fs.writeFileSync(filename, Buffer.from([0xc3, 0x28]));
    check();
    fs.unlinkSync(filename);
    const target = path.join(directory, "target.json");
    fs.writeFileSync(target, '{"decision":"allow","reason":"ok"}');
    fs.symlinkSync(target, filename);
    check();
  });

  it("requires a real completed gate and discards stale pre-gate files", () => {
    write({ decision: "allow", reason: "old_run" });
    expect(evaluate({ filename, outcomes: [] }).decision).toBe("deny");
    initialize(filename);
    expect(fs.existsSync(filename)).toBe(false);
    expect(globalThis.core.exportVariable).toHaveBeenCalledWith("GH_AW_WRITE_DECISIONS", filename);
  });

  it("uses a compiler-owned dry-run denial without executing an authored decision", () => {
    expect(evaluate({ filename, outcomes: null, dryRun: true })).toEqual({ decision: "deny", reason: "dry_run", error: false });
  });

  it("surfaces gate errors while publishing deny outputs", () => {
    vi.stubEnv("GH_AW_WRITE_DECISIONS", filename);
    vi.stubEnv("GH_AW_WRITE_GATE_STEP_OUTCOMES", '["success"]');
    main();
    expect(globalThis.core.setOutput).toHaveBeenCalledWith("write_gate", "deny");
    expect(globalThis.core.setOutput).toHaveBeenCalledWith("write_gate_reason", "gate_error");
    expect(globalThis.core.setFailed).toHaveBeenCalledOnce();
  });
});
