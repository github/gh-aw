// @ts-check
/// <reference types="@actions/github-script" />

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("fs");
const os = require("os");
const path = require("path");
const { lstatGuard, readGuardedFile, writeGuardedFile } = require("./symlink_guard.cjs");

describe("lstatGuard", () => {
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "symlink-guard-test-"));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it("returns Stats for a regular file", () => {
    const filePath = path.join(tmpDir, "file.txt");
    fs.writeFileSync(filePath, "hello");

    const stat = lstatGuard(filePath);

    expect(stat).not.toBeNull();
    expect(stat?.isFile()).toBe(true);
  });

  it("returns Stats for a directory", () => {
    const dirPath = path.join(tmpDir, "subdir");
    fs.mkdirSync(dirPath);

    const stat = lstatGuard(dirPath);

    expect(stat).not.toBeNull();
    expect(stat?.isDirectory()).toBe(true);
  });

  it("returns null for a symbolic link", () => {
    const target = path.join(tmpDir, "target.txt");
    fs.writeFileSync(target, "content");
    const link = path.join(tmpDir, "link.txt");
    fs.symlinkSync(target, link);

    const stat = lstatGuard(link);

    expect(stat).toBeNull();
  });

  it("returns null for a dangling symbolic link", () => {
    const link = path.join(tmpDir, "dangling.txt");
    fs.symlinkSync("/nonexistent/path", link);

    const stat = lstatGuard(link);

    expect(stat).toBeNull();
  });

  it("throws when the path does not exist", () => {
    const missing = path.join(tmpDir, "missing.txt");

    expect(() => lstatGuard(missing)).toThrow();
  });

  it("reads bounded regular files and exclusively stages their bytes", () => {
    const source = path.join(tmpDir, "source.png");
    const target = path.join(tmpDir, "staging", "chart.png");
    fs.writeFileSync(source, "chart");
    const content = readGuardedFile(source, 5);
    expect(content.toString()).toBe("chart");
    expect(() => readGuardedFile(source, 4)).toThrow("exceeds maximum allowed size");
    writeGuardedFile(target, content);
    expect(fs.readFileSync(target)).toEqual(content);
    expect(() => writeGuardedFile(target, Buffer.from("overwrite"))).toThrow("EEXIST");
    expect(fs.readFileSync(target)).toEqual(content);
  });

  it.each(["symlink", "regular"])("rejects a file swapped to a different %s between inspection and open", kind => {
    const source = path.join(tmpDir, "source.png");
    const sentinel = path.join(tmpDir, "sentinel.png");
    fs.writeFileSync(source, "safe");
    fs.writeFileSync(sentinel, "harmless sentinel");
    const originalOpen = fs.openSync;
    vi.spyOn(fs, "openSync").mockImplementation((filename, ...args) => {
      if (path.basename(String(filename)) === "source.png") {
        fs.unlinkSync(source);
        if (kind === "symlink") fs.symlinkSync(sentinel, source);
        else fs.renameSync(sentinel, source);
      }
      return originalOpen(filename, ...args);
    });
    expect(() => readGuardedFile(source)).toThrow(/ELOOP|changed while opening/);
  });

  it("rejects parent redirection between directory inspection and opening", () => {
    const parent = path.join(tmpDir, "parent");
    const sentinelDir = path.join(tmpDir, "external");
    fs.mkdirSync(parent);
    fs.mkdirSync(sentinelDir);
    fs.writeFileSync(path.join(parent, "chart.png"), "safe");
    fs.writeFileSync(path.join(sentinelDir, "chart.png"), "harmless sentinel");
    const originalOpen = fs.openSync;
    vi.spyOn(fs, "openSync").mockImplementation((filename, ...args) => {
      if (path.basename(String(filename)) === "parent") {
        fs.renameSync(parent, `${parent}-original`);
        fs.symlinkSync(sentinelDir, parent, "dir");
      }
      return originalOpen(filename, ...args);
    });
    expect(() => readGuardedFile(path.join(parent, "chart.png"))).toThrow(/ELOOP|ENOTDIR/);
  });

  it("rejects changes during reading and closes every descriptor", () => {
    const source = path.join(tmpDir, "source.png");
    fs.writeFileSync(source, "safe");
    const originalRead = fs.readSync;
    const opened = vi.spyOn(fs, "openSync");
    const closed = vi.spyOn(fs, "closeSync");
    vi.spyOn(fs, "readSync").mockImplementation((...args) => {
      fs.appendFileSync(source, " changed");
      return originalRead(...args);
    });
    expect(() => readGuardedFile(source)).toThrow("changed while reading");
    for (const result of opened.mock.results) {
      if (result.type === "return") expect(closed).toHaveBeenCalledWith(result.value);
    }
  });
});
