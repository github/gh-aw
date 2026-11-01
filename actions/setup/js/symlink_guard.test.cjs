// @ts-check
/// <reference types="@actions/github-script" />

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fs = require("fs");
const path = require("path");
const { lstatGuard, readGuardedFile, writeGuardedFile } = require("./symlink_guard.cjs");

function setPlatform(platform) {
  vi.stubGlobal("process", new Proxy(process, { get: (target, key) => (key === "platform" ? platform : Reflect.get(target, key)) }));
}

// Model Linux descriptor-relative lookup on macOS, retaining real file I/O.
// Renames update pinned descriptor paths, not subsequent absolute-path lookups.
function simulateLinuxDescriptors() {
  if (process.platform === "linux") return;
  const originals = { openSync: fs.openSync, lstatSync: fs.lstatSync, mkdirSync: fs.mkdirSync, renameSync: fs.renameSync };
  const pinned = new Map();
  const resolve = filename => {
    if (typeof filename !== "string") return filename;
    const match = /^\/proc\/self\/fd\/(\d+)(?:\/(.*))?$/.exec(filename);
    return match ? path.join(pinned.get(Number(match[1])), match[2] || "") : filename;
  };
  const open = fs.openSync;
  fs.openSync = (filename, ...args) => {
    const resolved = resolve(filename);
    const fd = open(resolved, ...args);
    pinned.set(fd, resolved);
    return fd;
  };
  for (const method of ["lstatSync", "mkdirSync"]) {
    const original = fs[method];
    fs[method] = (filename, ...args) => original(resolve(filename), ...args);
  }
  const rename = fs.renameSync;
  fs.renameSync = (source, target) => {
    rename(source, target);
    for (const [fd, filename] of pinned) {
      if (filename === source || filename.startsWith(`${source}${path.sep}`)) pinned.set(fd, target + filename.slice(source.length));
    }
  };
  setPlatform("linux");
  return () => Object.assign(fs, originals);
}

describe("lstatGuard", () => {
  let tmpDir;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.resolve(".symlink-guard-test-"));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
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

  it.each(["darwin", "win32"])("fails closed on %s before accessing files, including junctions", platform => {
    setPlatform(platform);
    const inspected = vi.spyOn(fs, "lstatSync");
    const opened = vi.spyOn(fs, "openSync");
    const created = vi.spyOn(fs, "mkdirSync");
    for (const operation of [() => readGuardedFile(path.join(tmpDir, "source.png")), () => writeGuardedFile(path.join(tmpDir, "staging", "target.png"), Buffer.from("safe"))]) {
      expect(operation).toThrow(expect.objectContaining({ code: "ERR_VALIDATION", message: expect.stringMatching(/^ERR_VALIDATION:.*requires Linux/) }));
    }
    expect(inspected).not.toHaveBeenCalled();
    expect(opened).not.toHaveBeenCalled();
    expect(created).not.toHaveBeenCalled();
  });

  describe.skipIf(process.platform === "win32")("Linux guarded access", () => {
    let restoreDescriptors;
    beforeEach(() => {
      restoreDescriptors = simulateLinuxDescriptors();
    });
    afterEach(() => {
      vi.restoreAllMocks();
      restoreDescriptors?.();
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
      expect(() => readGuardedFile(source)).toThrow(expect.objectContaining({ code: "ERR_VALIDATION" }));
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
      expect(() => readGuardedFile(path.join(parent, "chart.png"))).toThrow(expect.objectContaining({ code: "ERR_VALIDATION" }));
    });

    it.each(["read", "write"])("pins the parent during %s even if swapped and restored after verification", operation => {
      const parent = path.join(tmpDir, "parent");
      const external = path.join(tmpDir, "external");
      fs.mkdirSync(parent);
      fs.mkdirSync(external);
      fs.writeFileSync(path.join(parent, "chart.png"), "safe");
      if (operation === "read") fs.writeFileSync(path.join(external, "chart.png"), "external sentinel");
      const open = fs.openSync;
      const basename = operation === "read" ? "chart.png" : "staged.png";
      let swapped = false;
      vi.spyOn(fs, "openSync").mockImplementation((filename, ...args) => {
        if (path.basename(String(filename)) !== basename) return open(filename, ...args);
        expect(String(filename)).toMatch(/^\/proc\/self\/fd\/\d+\//);
        swapped = true;
        fs.renameSync(parent, `${parent}-original`);
        fs.symlinkSync(external, parent, "dir");
        try {
          return open(filename, ...args);
        } finally {
          fs.unlinkSync(parent);
          fs.renameSync(`${parent}-original`, parent);
        }
      });
      if (operation === "read") {
        expect(readGuardedFile(path.join(parent, "chart.png")).toString()).toBe("safe");
        expect(fs.readFileSync(path.join(external, "chart.png"), "utf8")).toBe("external sentinel");
      } else {
        writeGuardedFile(path.join(parent, "staged.png"), Buffer.from("safe"));
      }
      expect(swapped).toBe(true);
      if (operation === "write") {
        expect(fs.existsSync(path.join(external, "staged.png"))).toBe(false);
        expect(fs.readFileSync(path.join(parent, "staged.png"), "utf8")).toBe("safe");
      }
    });

    it.each([-1, NaN, Infinity, 1.5])("codes invalid maximum size %s as validation", maximum => {
      expect(() => readGuardedFile(path.join(tmpDir, "missing.png"), maximum)).toThrow(expect.objectContaining({ code: "ERR_VALIDATION", message: expect.stringMatching(/^ERR_VALIDATION:/) }));
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
});
