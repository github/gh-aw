import { describe, it, expect, afterEach, beforeEach } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { clearValidationMarker, formatJSONFiles, getValidationMarkerPath, runCustomMemoryValidation, writeValidationMarker } from "./memory_custom_validation.cjs";

describe("memory_custom_validation", () => {
  let tempDir;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-memory-validation-test-"));
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
    clearValidationMarker("repo", "default");
  });

  it("runs a successful validator with memory globals", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), JSON.stringify({ ok: true }));
    const result = runCustomMemoryValidation({
      script: `
        const state = JSON.parse(fs.readFileSync(path.join(memoryRoot, "state.json"), "utf8"));
        if (!state.ok || memoryKind !== "repo" || memoryId !== "default") throw new Error("bad context");
        console.log("domain ok");
      `,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(true);
    expect(result.stdout).toContain("domain ok");
  });

  it("reports a nonzero validator separately from stdout", () => {
    const result = runCustomMemoryValidation({
      script: `
        console.log("generic-looking stdout");
        console.error("domain schema failed");
        return false;
      `,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "cache",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(false);
    expect(result.stdout).toContain("generic-looking stdout");
    expect(result.stderr).toContain("domain schema failed");
  });

  it("rejects validators that modify memory files", () => {
    const statePath = path.join(tempDir, "state.json");
    fs.writeFileSync(statePath, JSON.stringify({ ok: true }));

    const result = runCustomMemoryValidation({
      script: `fs.writeFileSync(path.join(memoryRoot, "state.json"), JSON.stringify({ ok: false }));`,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "cache",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("must not modify memory files");
  });

  it("keeps the working directory intact when a validator removes its memory root", () => {
    const statePath = path.join(tempDir, "state.json");
    fs.writeFileSync(statePath, JSON.stringify({ ok: true }));

    const result = runCustomMemoryValidation({
      script: `fs.rmSync(memoryRoot, { recursive: true, force: true });`,
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 5,
    });

    expect(result.ok).toBe(false);
    expect(result.stderr).toContain("Unable to snapshot memory after custom validation");
    expect(fs.existsSync(tempDir)).toBe(true);
    expect(fs.readFileSync(statePath, "utf8")).toBe(JSON.stringify({ ok: true }));
  });

  it("times out long-running validators", () => {
    const result = runCustomMemoryValidation({
      script: "while (true) {}",
      memoryDir: tempDir,
      memoryId: "default",
      kind: "repo",
      timeoutSeconds: 1,
    });

    expect(result.ok).toBe(false);
    expect(result.timedOut).toBe(true);
  });

  it("formats JSON before validation can inspect it", () => {
    const file = path.join(tempDir, "state.json");
    fs.writeFileSync(file, '{"b":2,"a":1}');

    const formatted = formatJSONFiles(tempDir, 1024);

    expect(formatted).toEqual(["state.json"]);
    expect(fs.readFileSync(file, "utf8")).toBe('{\n  "b": 2,\n  "a": 1\n}\n');
  });

  it("writes and clears validation markers", () => {
    const marker = writeValidationMarker("repo", "default");
    expect(marker).toBe(getValidationMarkerPath("repo", "default"));
    expect(fs.existsSync(marker)).toBe(true);

    clearValidationMarker("repo", "default");

    expect(fs.existsSync(marker)).toBe(false);
  });
});
