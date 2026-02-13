// @ts-check

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

const { validateMemoryFiles } = require("./validate_memory_files.cjs");

// Mock core globally
global.core = {
  info: () => {},
  error: () => {},
  warning: () => {},
  debug: () => {},
};

describe("validateMemoryFiles", () => {
  let tempDir = "";

  beforeEach(() => {
    // Create a temporary directory for testing
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "validate-memory-test-"));
  });

  afterEach(() => {
    // Clean up temporary directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("returns valid for empty directory", () => {
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("returns valid for non-existent directory", () => {
    const nonExistentDir = path.join(tempDir, "does-not-exist");
    const result = validateMemoryFiles(nonExistentDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .json files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), '{"test": true}');
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .jsonl files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.jsonl"), '{"line": 1}\n{"line": 2}');
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .txt files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "Some notes");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .md files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "README.md"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .csv files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.csv"), "col1,col2\nval1,val2");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts multiple valid files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "notes");
    fs.writeFileSync(path.join(tempDir, "README.md"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .log files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "app.log"), "log entry");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // Now accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .yaml files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "key: value");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // Now accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .xml files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.xml"), "<root></root>");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // Now accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts files without extension by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "noext"), "content");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // Now accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts all files by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "app.log"), "log");
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "yaml");
    fs.writeFileSync(path.join(tempDir, "valid.json"), "{}");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // All files accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("validates files in subdirectories by default (allow all)", () => {
    const subdir = path.join(tempDir, "subdir");
    fs.mkdirSync(subdir);
    fs.writeFileSync(path.join(subdir, "valid.json"), "{}");
    fs.writeFileSync(path.join(subdir, "invalid.log"), "log");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // All files accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("validates files in deeply nested directories by default (allow all)", () => {
    const level1 = path.join(tempDir, "level1");
    const level2 = path.join(level1, "level2");
    const level3 = path.join(level2, "level3");
    fs.mkdirSync(level1);
    fs.mkdirSync(level2);
    fs.mkdirSync(level3);
    fs.writeFileSync(path.join(level3, "deep.json"), "{}");
    fs.writeFileSync(path.join(level3, "invalid.bin"), "binary");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // All files accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("is case-insensitive for extensions by default (allow all)", () => {
    fs.writeFileSync(path.join(tempDir, "data.JSON"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.TXT"), "text");
    fs.writeFileSync(path.join(tempDir, "README.MD"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("handles all files in subdirectories by default (allow all)", () => {
    const subdir1 = path.join(tempDir, "valid-files");
    const subdir2 = path.join(tempDir, "invalid-files");
    fs.mkdirSync(subdir1);
    fs.mkdirSync(subdir2);
    fs.writeFileSync(path.join(subdir1, "data.json"), "{}");
    fs.writeFileSync(path.join(subdir1, "notes.txt"), "text");
    fs.writeFileSync(path.join(subdir2, "app.log"), "log");
    fs.writeFileSync(path.join(subdir2, "config.ini"), "ini");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // All files accepted when no restrictions
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts custom allowed extensions", () => {
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "key: value");
    fs.writeFileSync(path.join(tempDir, "data.xml"), "<root></root>");
    const customExts = [".yaml", ".xml"];
    const result = validateMemoryFiles(tempDir, "cache", customExts);
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("rejects files not in custom allowed extensions", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    const customExts = [".yaml", ".xml"];
    const result = validateMemoryFiles(tempDir, "cache", customExts);
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual(["data.json"]);
  });

  it("allows all files when custom array is empty", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "text");
    fs.writeFileSync(path.join(tempDir, "app.log"), "log");
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "key: value");
    const result = validateMemoryFiles(tempDir, "cache", []);
    expect(result.valid).toBe(true); // Empty array means allow all
    expect(result.invalidFiles).toEqual([]);
  });

  it("allows all files when allowedExtensions is undefined", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "app.log"), "log");
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "key: value");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true); // undefined means allow all
    expect(result.invalidFiles).toEqual([]);
  });
});
