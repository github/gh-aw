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

  it("accepts .json files", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), '{"test": true}');
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .jsonl files", () => {
    fs.writeFileSync(path.join(tempDir, "data.jsonl"), '{"line": 1}\n{"line": 2}');
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .txt files", () => {
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "Some notes");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .md files", () => {
    fs.writeFileSync(path.join(tempDir, "README.md"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts .csv files", () => {
    fs.writeFileSync(path.join(tempDir, "data.csv"), "col1,col2\nval1,val2");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("accepts multiple valid files", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "notes");
    fs.writeFileSync(path.join(tempDir, "README.md"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("rejects .log files", () => {
    fs.writeFileSync(path.join(tempDir, "app.log"), "log entry");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual(["app.log"]);
  });

  it("rejects .yaml files", () => {
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "key: value");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual(["config.yaml"]);
  });

  it("rejects .xml files", () => {
    fs.writeFileSync(path.join(tempDir, "data.xml"), "<root></root>");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual(["data.xml"]);
  });

  it("rejects files without extension", () => {
    fs.writeFileSync(path.join(tempDir, "noext"), "content");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual(["noext"]);
  });

  it("rejects multiple invalid files", () => {
    fs.writeFileSync(path.join(tempDir, "app.log"), "log");
    fs.writeFileSync(path.join(tempDir, "config.yaml"), "yaml");
    fs.writeFileSync(path.join(tempDir, "valid.json"), "{}");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toHaveLength(2);
    expect(result.invalidFiles).toContain("app.log");
    expect(result.invalidFiles).toContain("config.yaml");
  });

  it("validates files in subdirectories", () => {
    const subdir = path.join(tempDir, "subdir");
    fs.mkdirSync(subdir);
    fs.writeFileSync(path.join(subdir, "valid.json"), "{}");
    fs.writeFileSync(path.join(subdir, "invalid.log"), "log");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual([path.join("subdir", "invalid.log")]);
  });

  it("validates files in deeply nested directories", () => {
    const level1 = path.join(tempDir, "level1");
    const level2 = path.join(level1, "level2");
    const level3 = path.join(level2, "level3");
    fs.mkdirSync(level1);
    fs.mkdirSync(level2);
    fs.mkdirSync(level3);
    fs.writeFileSync(path.join(level3, "deep.json"), "{}");
    fs.writeFileSync(path.join(level3, "invalid.bin"), "binary");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toEqual([path.join("level1", "level2", "level3", "invalid.bin")]);
  });

  it("is case-insensitive for extensions", () => {
    fs.writeFileSync(path.join(tempDir, "data.JSON"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.TXT"), "text");
    fs.writeFileSync(path.join(tempDir, "README.MD"), "# Title");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(true);
    expect(result.invalidFiles).toEqual([]);
  });

  it("handles mixed valid and invalid files in subdirectories", () => {
    const subdir1 = path.join(tempDir, "valid-files");
    const subdir2 = path.join(tempDir, "invalid-files");
    fs.mkdirSync(subdir1);
    fs.mkdirSync(subdir2);
    fs.writeFileSync(path.join(subdir1, "data.json"), "{}");
    fs.writeFileSync(path.join(subdir1, "notes.txt"), "text");
    fs.writeFileSync(path.join(subdir2, "app.log"), "log");
    fs.writeFileSync(path.join(subdir2, "config.ini"), "ini");
    const result = validateMemoryFiles(tempDir, "cache");
    expect(result.valid).toBe(false);
    expect(result.invalidFiles).toHaveLength(2);
    expect(result.invalidFiles).toContain(path.join("invalid-files", "app.log"));
    expect(result.invalidFiles).toContain(path.join("invalid-files", "config.ini"));
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

  it("uses default extensions when custom array is empty", () => {
    fs.writeFileSync(path.join(tempDir, "data.json"), "{}");
    fs.writeFileSync(path.join(tempDir, "notes.txt"), "text");
    const result = validateMemoryFiles(tempDir, "cache", []);
    expect(result.valid).toBe(true); // Empty array falls back to defaults
    expect(result.invalidFiles).toEqual([]);
  });
});
