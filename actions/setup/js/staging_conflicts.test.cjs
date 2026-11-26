import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "node:module";

const { copyDirectoryExclusive } = createRequire(import.meta.url)("./staging_conflicts.cjs");

describe("staging_conflicts.cjs", () => {
  let testRoot;

  afterEach(() => {
    vi.restoreAllMocks();
    if (testRoot) fs.rmSync(testRoot, { recursive: true, force: true });
  });

  it("does not replace a destination created during exclusive publication", () => {
    testRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-staging-conflicts-"));
    const sourceDir = path.join(testRoot, "source");
    const destDir = path.join(testRoot, "staged");
    fs.mkdirSync(sourceDir);
    fs.writeFileSync(path.join(sourceDir, "source.txt"), "source");

    const mkdirSync = fs.mkdirSync;
    let raced = false;
    let concurrentInode;
    vi.spyOn(fs, "mkdirSync").mockImplementation((directory, options) => {
      if (directory === destDir && !raced) {
        raced = true;
        mkdirSync(destDir, options);
        concurrentInode = fs.statSync(destDir).ino;
      }
      return mkdirSync(directory, options);
    });

    const result = copyDirectoryExclusive(sourceDir, destDir, tempDir => {
      fs.writeFileSync(path.join(tempDir, "source.txt"), "source");
    });

    expect(raced).toBe(true);
    expect(result).toBe("conflict");
    expect(fs.statSync(destDir).ino).toBe(concurrentInode);
    expect(fs.readdirSync(destDir)).toEqual([]);
  });

  it("copies staged contents after exclusively creating the destination", () => {
    testRoot = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-staging-conflicts-"));
    const sourceDir = path.join(testRoot, "source");
    const destDir = path.join(testRoot, "staged");
    fs.mkdirSync(sourceDir);

    const result = copyDirectoryExclusive(sourceDir, destDir, tempDir => {
      fs.mkdirSync(path.join(tempDir, "nested"));
      fs.writeFileSync(path.join(tempDir, "nested", "staged.txt"), "staged");
    });

    expect(result).toBe("copied");
    expect(fs.readFileSync(path.join(destDir, "nested", "staged.txt"), "utf8")).toBe("staged");
  });
});
