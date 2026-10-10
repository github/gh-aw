import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { getRepoMemoryBaselinePath, getValidationMarkerPath } from "./memory_custom_validation.cjs";
import { checkRepoMemoryBaseline, validateMemoryStep, validateRepoMemoryBaseline } from "./validate_memory_step.cjs";

describe("validateMemoryStep", () => {
  let tempDir;
  let originalEnv;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-validate-memory-step-"));
    originalEnv = { ...process.env };
    process.env.MEMORY_DIR = tempDir;
    process.env.GH_AW_PROMPT_DIR = `${tempDir}-prompts`;
    fs.mkdirSync(process.env.GH_AW_PROMPT_DIR);
    fs.writeFileSync(path.join(process.env.GH_AW_PROMPT_DIR, "user.txt"), "user prompt\n");
    fs.writeFileSync(path.join(process.env.GH_AW_PROMPT_DIR, "prompt.txt"), "combined prompt\n");
    process.env.MEMORY_ID = "default";
    process.env.ALLOWED_EXTENSIONS = '[".json"]';
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('console.log("valid");').toString("base64");
  });

  afterEach(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
    fs.rmSync(process.env.GH_AW_PROMPT_DIR, { recursive: true, force: true });
    fs.rmSync(getValidationMarkerPath("cache", "default"), { force: true });
    fs.rmSync(getRepoMemoryBaselinePath(process.env.MEMORY_ID), { force: true });
    fs.rmSync(getRepoMemoryBaselinePath(process.env.MEMORY_ID).replace(/\.json$/, ".log"), { force: true });
    process.env = originalEnv;
  });

  it.each([
    ["valid baseline", '"valid"', false, '"invalid"', true],
    ["invalid baseline repaired", '"invalid"', true, '"valid"', false],
    ["invalid baseline unchanged", '"invalid"', true, '"invalid"', true],
  ])("%s: validates the baseline and only skips unchanged invalid memory", (_name, initial, invalidBaseline, candidate, invalidCandidate) => {
    process.env.MEMORY_ID = path.basename(tempDir);
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('if (fs.readFileSync(path.join(memoryDir, "state.json"), "utf8") !== \'"valid"\') throw new Error("invalid state");').toString("base64");
    const statePath = path.join(tempDir, "state.json");
    fs.writeFileSync(statePath, initial);
    fs.mkdirSync(path.join(tempDir, ".git"));
    fs.writeFileSync(path.join(tempDir, ".git", "HEAD"), "original");
    const warnings = [];
    const outputs = [];
    const core = {
      info: () => {},
      warning: message => warnings.push(message),
      error: () => {},
      setFailed: message => warnings.push(message),
      setOutput: (key, value) => outputs.push([key, value]),
    };

    validateRepoMemoryBaseline(core);
    const baseline = JSON.parse(fs.readFileSync(getRepoMemoryBaselinePath(process.env.MEMORY_ID), "utf8"));
    expect(baseline.ok).toBe(!invalidBaseline);
    expect(baseline.stderr.includes("invalid state")).toBe(invalidBaseline);
    for (const name of ["user.txt", "prompt.txt"]) {
      const rendered = fs.readFileSync(path.join(process.env.GH_AW_PROMPT_DIR, name), "utf8");
      expect(rendered.includes("invalid state")).toBe(invalidBaseline);
      expect(rendered.includes("call push_repo_memory")).toBe(invalidBaseline);
    }
    fs.writeFileSync(path.join(tempDir, ".git", "HEAD"), "updated");
    fs.writeFileSync(statePath, candidate);
    checkRepoMemoryBaseline(core);
    expect(outputs).toEqual(invalidBaseline && initial === candidate ? [["skip", "true"]] : []);
    if (outputs.length === 0) {
      expect(validateMemoryStep(core, { kind: "repo", requireValidationScript: true })).toBe(!invalidCandidate);
    }
    expect(warnings.some(message => message.includes("remains identical"))).toBe(outputs.length > 0);
  });

  it("does not allow baseline validation to migrate the actual checkout", () => {
    process.env.MEMORY_ID = path.basename(tempDir);
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('fs.writeFileSync(path.join(memoryDir, "state.json"), "migrated");').toString("base64");
    fs.writeFileSync(path.join(tempDir, "state.json"), "original");
    const warnings = [];
    validateRepoMemoryBaseline({ info: () => {}, warning: message => warnings.push(message) });
    expect(fs.readFileSync(path.join(tempDir, "state.json"), "utf8")).toBe("original");
    expect(warnings.join("\n")).toContain("must not modify memory files");
  });

  it("keeps large diagnostics in private temporary files with bounded prompt excerpts", () => {
    process.env.MEMORY_ID = path.basename(tempDir);
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('console.error("x".repeat(6000)); process.exitCode = 1;').toString("base64");
    const baselinePath = getRepoMemoryBaselinePath(process.env.MEMORY_ID);
    const logPath = baselinePath.replace(/\.json$/, ".log");
    const promptPaths = ["user.txt", "prompt.txt"].map(name => path.join(process.env.GH_AW_PROMPT_DIR, name));
    fs.mkdirSync(path.dirname(baselinePath), { recursive: true });
    for (const file of [baselinePath, logPath, ...promptPaths]) {
      fs.writeFileSync(file, "original\n");
      fs.chmodSync(file, 0o666);
    }
    const warnings = [];

    validateRepoMemoryBaseline({ info: () => {}, warning: message => warnings.push(message) });

    expect(fs.readFileSync(logPath, "utf8")).toContain("x".repeat(6000));
    for (const promptPath of promptPaths) {
      const rendered = fs.readFileSync(promptPath, "utf8");
      expect(rendered).toMatch(/^original\n/);
      expect(rendered).toContain(logPath);
      expect(rendered).toContain("(truncated)");
      expect(rendered.length).toBeLessThan(2000);
      expect(rendered).not.toContain("x".repeat(1001));
    }
    expect(warnings[0].length).toBeLessThan(1500);
    if (process.platform !== "win32") {
      for (const file of [baselinePath, logPath, ...promptPaths]) {
        expect(fs.statSync(file).mode & 0o777).toBe(0o600);
      }
    }
  });

  it.each(["baseline", "log", "user.txt", "prompt.txt"])("rejects a symlinked %s diagnostic destination without overwriting its target", destination => {
    if (process.platform === "win32") return;
    process.env.MEMORY_ID = path.basename(tempDir);
    process.env.VALIDATION_SCRIPT_B64 = Buffer.from('throw new Error("invalid state");').toString("base64");
    const baselinePath = getRepoMemoryBaselinePath(process.env.MEMORY_ID);
    const target = path.join(tempDir, "protected");
    fs.writeFileSync(target, "unchanged");
    fs.mkdirSync(path.dirname(baselinePath), { recursive: true });
    const destinationPath = destination === "baseline" ? baselinePath : destination === "log" ? baselinePath.replace(/\.json$/, ".log") : path.join(process.env.GH_AW_PROMPT_DIR, destination);
    fs.rmSync(destinationPath, { force: true });
    fs.symlinkSync(target, destinationPath);

    expect(() => validateRepoMemoryBaseline({ info: () => {}, warning: () => {} })).toThrow(/Unable to (record|write|add)/);
    expect(fs.readFileSync(target, "utf8")).toBe("unchanged");
  });

  it("validates cache content and writes its marker after success", () => {
    fs.writeFileSync(path.join(tempDir, "state.json"), "{}");
    const messages = [];
    const core = {
      info: message => messages.push(message),
      warning: message => messages.push(`warning: ${message}`),
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
      warning: () => {},
      error: () => {},
      setFailed: () => {},
    };

    expect(validateMemoryStep(core, { kind: "drive" })).toBe(true);
  });

  it("runs required schema-only validation and fails when transported schemas are missing", () => {
    delete process.env.VALIDATION_SCRIPT_B64;
    fs.writeFileSync(path.join(tempDir, "state.json"), '{"version":1}');
    const messages = [];
    const core = {
      info: message => messages.push(message),
      warning: message => messages.push(`warning: ${message}`),
      error: message => messages.push(`error: ${message}`),
      setFailed: message => messages.push(`failed: ${message}`),
    };
    const schemas = [{ file: "state.json", format: "json", schema: { type: "object", required: ["version"], properties: { version: { enum: [1] } } } }];
    process.env.MEMORY_JSON_SCHEMAS_B64 = Buffer.from(JSON.stringify(schemas)).toString("base64");
    process.env.MEMORY_JSON_SCHEMAS_REQUIRED = "true";

    expect(validateMemoryStep(core, { kind: "cache", writeMarker: true })).toBe(true);
    expect(messages.some(message => message.includes("Declarative cache-memory schemas passed"))).toBe(true);
    expect(fs.existsSync(getValidationMarkerPath("cache", "default"))).toBe(true);

    delete process.env.MEMORY_JSON_SCHEMAS_B64;
    fs.rmSync(getValidationMarkerPath("cache", "default"), { force: true });
    expect(validateMemoryStep(core, { kind: "cache", writeMarker: true })).toBe(false);
    expect(messages.some(message => message.includes("json-schemas configuration is missing"))).toBe(true);
    expect(fs.existsSync(getValidationMarkerPath("cache", "default"))).toBe(false);
  });

  it("filters (never hard-fails) disallowed-extension files uniformly across memory kinds", () => {
    delete process.env.VALIDATION_SCRIPT_B64;
    fs.writeFileSync(path.join(tempDir, "notes.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.json.new"), "ignored");
    const messages = [];
    const core = {
      info: message => messages.push(message),
      warning: message => messages.push(`warning: ${message}`),
      error: message => messages.push(`error: ${message}`),
      setFailed: message => messages.push(`failed: ${message}`),
    };

    for (const kind of ["repo", "cache", "drive"]) {
      expect(validateMemoryStep(core, { kind })).toBe(true);
    }

    expect(messages.some(m => m.startsWith("failed:"))).toBe(false);
    expect(messages).toContain('warning: Ignored 1 ineligible file(s) before validation/upload:\n  - notes.json.new (disallowed extension ".new")');
    expect(fs.existsSync(path.join(tempDir, "notes.json"))).toBe(true);
    expect(fs.existsSync(path.join(tempDir, "notes.json.new"))).toBe(false);
  });
});
