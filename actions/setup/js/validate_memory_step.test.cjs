import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { getValidationMarkerPath } from "./memory_custom_validation.cjs";
import { validateMemoryStep } from "./validate_memory_step.cjs";

describe("validateMemoryStep", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-validate-memory-step-"));
    originalEnv = { ...process.env };
    process.env.MEMORY_DIR = tempDir;
    process.env.MEMORY_ID = "default";
    process.env.ALLOWED_EXTENSIONS = '[".json"]';
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('console.log("valid");').toString("base64");
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
    fs.rmSync(getValidationMarkerPath("cache", "default"), { force: true });
    process.env = originalEnv;
  });

  it("validates cache content and writes its marker after success", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), "{}");
    const messages = [];
    const core = {
      info: message => messages.push(message),
      error: message => messages.push(`error: ${message}`),
      setFailed: message => messages.push(`failed: ${message}`),
    };

    expect(validateMemoryStep(core, { kind: "cache", writeMarker: true })).toBe(true);
    expect(messages).toContain("Custom cache-memory validation stdout:\nvalid\n");
    expect(fs.existsSync(getValidationMarkerPath("cache", "default"))).toBe(true);
  });

  it("validates drive content", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), "{}");
    const core = {
      info: () => {},
      error: () => {},
      setFailed: () => {},
    };

    expect(validateMemoryStep(core, { kind: "drive" })).toBe(true);
  });

  it("filters (never hard-fails) disallowed-extension files uniformly across memory kinds", () => {
    delete process.env.VALIDATION_SCRIPT_B64;
    fs.writeFileSync(path.join(tempDir, "notes.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.json.new"), "ignored");
    const messages = [];
    const core = {
      info: message => messages.push(message),
      error: message => messages.push(`error: ${message}`),
      setFailed: message => messages.push(`failed: ${message}`),
    };

    for (const kind of ["repo", "cache", "drive"]) {
      expect(validateMemoryStep(core, { kind })).toBe(true);
    }

    expect(messages.some(m => m.startsWith("failed:"))).toBe(false);
    expect(fs.existsSync(path.join(tempDir, "notes.json"))).toBe(true);
    expect(fs.existsSync(path.join(tempDir, "notes.json.new"))).toBe(false);
  });
});
