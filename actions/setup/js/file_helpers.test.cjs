// @ts-check
/// <reference types="@actions/github-script" />

import { describe, it, expect, beforeEach, afterEach } from "vitest";
const fs = require("fs");
const path = require("path");
const os = require("os");
const { listFilesRecursively, checkFileExists, findExistingFiles, findFirstExistingFile } = require("./file_helpers.cjs");

// Mock core object for testing
global.core = {
  info: () => {},
  warning: () => {},
  error: () => {},
  setFailed: () => {},
};

describe("listFilesRecursively", () => {
  let tempDir;

  beforeEach(() => {
    // Create a temporary directory for testing
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "file-helpers-test-"));
  });

  afterEach(() => {
    // Clean up temporary directory
    if (fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("should return empty array for non-existent directory", () => {
    const result = listFilesRecursively("/non/existent/path");
    expect(result).toEqual([]);
  });

  it("should list files in flat directory", () => {
    // Create test files
    fs.writeFileSync(path.join(tempDir, "file1.txt"), "content1");
    fs.writeFileSync(path.join(tempDir, "file2.txt"), "content2");

    const result = listFilesRecursively(tempDir);
    expect(result).toHaveLength(2);
    expect(result.some(f => f.endsWith("file1.txt"))).toBe(true);
    expect(result.some(f => f.endsWith("file2.txt"))).toBe(true);
  });

  it("should list files recursively in nested directories", () => {
    // Create nested structure
    const subDir = path.join(tempDir, "subdir");
    fs.mkdirSync(subDir);
    fs.writeFileSync(path.join(tempDir, "root.txt"), "root");
    fs.writeFileSync(path.join(subDir, "nested.txt"), "nested");

    const result = listFilesRecursively(tempDir);
    expect(result).toHaveLength(2);
    expect(result.some(f => f.endsWith("root.txt"))).toBe(true);
    expect(result.some(f => f.endsWith("nested.txt"))).toBe(true);
  });

  it("should return relative paths when relativeTo is provided", () => {
    fs.writeFileSync(path.join(tempDir, "file.txt"), "content");

    const result = listFilesRecursively(tempDir, tempDir);
    expect(result).toHaveLength(1);
    expect(result[0]).toBe("file.txt");
  });
});

describe("checkFileExists", () => {
  let tempDir;
  let mockCore;

  beforeEach(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "file-helpers-test-"));

    // Create mock core with tracking
    mockCore = {
      infoCalls: [],
      warningCalls: [],
      errorCalls: [],
      setFailedCalls: [],
      info: function (msg) {
        this.infoCalls.push(msg);
      },
      warning: function (msg) {
        this.warningCalls.push(msg);
      },
      error: function (msg) {
        this.errorCalls.push(msg);
      },
      setFailed: function (msg) {
        this.setFailedCalls.push(msg);
      },
    };
    global.core = mockCore;
  });

  afterEach(() => {
    if (fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("should return true for existing file", () => {
    const filePath = path.join(tempDir, "exists.txt");
    fs.writeFileSync(filePath, "content");

    const result = checkFileExists(filePath, tempDir, "Test file", true);
    expect(result).toBe(true);
    expect(mockCore.infoCalls.some(msg => msg.includes("Test file found"))).toBe(true);
  });

  it("should return false and call setFailed for missing required file", () => {
    const filePath = path.join(tempDir, "missing.txt");

    const result = checkFileExists(filePath, tempDir, "Test file", true);
    expect(result).toBe(false);
    expect(mockCore.errorCalls.some(msg => msg.includes("Test file not found"))).toBe(true);
    expect(mockCore.setFailedCalls).toHaveLength(1);
  });

  it("should return false and warn instead of failing when continueOnError is true", () => {
    const filePath = path.join(tempDir, "missing.txt");

    const result = checkFileExists(filePath, tempDir, "Test file", true, true);
    expect(result).toBe(false);
    expect(mockCore.errorCalls).toHaveLength(0);
    expect(mockCore.warningCalls.some(msg => msg.includes("Continuing because GH_AW_DETECTION_CONTINUE_ON_ERROR=true"))).toBe(true);
    expect(mockCore.setFailedCalls).toHaveLength(0);
  });

  it("should return true for missing non-required file", () => {
    const filePath = path.join(tempDir, "missing.txt");

    const result = checkFileExists(filePath, tempDir, "Test file", false);
    expect(result).toBe(true);
    expect(mockCore.infoCalls.some(msg => msg.includes("No test file found"))).toBe(true);
  });

  it("should list directory contents when file not found", () => {
    const filePath = path.join(tempDir, "missing.txt");
    fs.writeFileSync(path.join(tempDir, "other.txt"), "other");

    checkFileExists(filePath, tempDir, "Test file", true);
    expect(mockCore.infoCalls.some(msg => msg.includes("Listing all files"))).toBe(true);
    expect(mockCore.infoCalls.some(msg => msg.includes("Found 1 file(s)"))).toBe(true);
    expect(mockCore.infoCalls.some(msg => msg.includes("other.txt"))).toBe(true);
  });
});

describe("findExistingFiles / findFirstExistingFile", () => {
  /** @type {string} */
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "find-existing-files-"));
  });

  afterEach(() => {
    if (fs.existsSync(tmpDir)) {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
  });

  it("returns existing candidates in preference order", () => {
    const first = path.join(tmpDir, "first.json");
    const second = path.join(tmpDir, "second.json");
    fs.writeFileSync(first, "{}");
    fs.writeFileSync(second, "{}");

    expect(findExistingFiles([path.join(tmpDir, "missing.json"), second, first])).toEqual([second, first]);
    expect(findFirstExistingFile([path.join(tmpDir, "missing.json"), second, first])).toBe(second);
  });

  it("ignores directories and missing paths", () => {
    const dir = path.join(tmpDir, "nested");
    fs.mkdirSync(dir);

    expect(findExistingFiles([dir, path.join(tmpDir, "missing.json")])).toEqual([]);
    expect(findFirstExistingFile([dir])).toBeUndefined();
  });

  it("returns an empty result for no candidates", () => {
    expect(findExistingFiles([])).toEqual([]);
    expect(findFirstExistingFile([])).toBeUndefined();
  });
});
