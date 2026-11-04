import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "child_process";
import crypto from "crypto";
import fs from "fs";
import path from "path";
import { execLocalGitSync, stageRawGitFiles } from "./git_helpers.cjs";
import { createHandlers } from "./safe_outputs_handlers.cjs";
import { simulateLinuxDescriptors } from "./test_linux_descriptors.mjs";

describe("raw local Git staging", () => {
  let root;
  let repo;
  let originalEnv;
  let originalCore;
  let restoreDescriptors;

  function git(args, input) {
    return childProcess.execFileSync("git", args, { cwd: repo, input, encoding: "utf8", stdio: "pipe", maxBuffer: 100 * 1024 * 1024 });
  }

  beforeEach(() => {
    originalEnv = { ...process.env };
    originalCore = global.core;
    global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() };
    root = path.resolve(`.git-raw-staging-${crypto.randomUUID()}`);
    repo = path.join(root, "repo");
    fs.mkdirSync(repo, { recursive: true });
    process.env.GIT_CEILING_DIRECTORIES = root;
    process.env.GITHUB_WORKSPACE = repo;
    git(["init"]);
    git(["config", "user.name", "Test"]);
    git(["config", "user.email", "test@example.com"]);
    git(["commit", "--allow-empty", "-m", "base"]);
    restoreDescriptors = simulateLinuxDescriptors(vi);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    restoreDescriptors?.();
    vi.unstubAllGlobals();
    process.env = originalEnv;
    global.core = originalCore;
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("stages exact bytes, executable modes, symlink targets, deletions and literal paths", () => {
    fs.writeFileSync(path.join(repo, "deleted.md"), "old");
    fs.writeFileSync(path.join(repo, "replaced.md"), "old");
    fs.mkdirSync(path.join(repo, "collapsed.md"));
    fs.writeFileSync(path.join(repo, "collapsed.md", "child.md"), "old");
    fs.mkdirSync(path.join(repo, "gone"));
    fs.writeFileSync(path.join(repo, "gone", "deleted.md"), "old");
    git(["add", "."]);
    git(["commit", "-m", "tracked"]);
    fs.unlinkSync(path.join(repo, "deleted.md"));
    fs.unlinkSync(path.join(repo, "replaced.md"));
    fs.mkdirSync(path.join(repo, "replaced.md"));
    fs.writeFileSync(path.join(repo, "replaced.md", "child.md"), "new");
    fs.rmSync(path.join(repo, "collapsed.md"), { recursive: true });
    fs.writeFileSync(path.join(repo, "collapsed.md"), "collapsed");
    fs.rmSync(path.join(repo, "gone"), { recursive: true });
    const literal = ":(glob)*\t\n\\raw.md";
    const raw = Buffer.from([0, 255, 13, 10, 65]);
    fs.writeFileSync(path.join(repo, literal), raw);
    fs.writeFileSync(path.join(repo, "run.md"), "#!/bin/sh\r\n", { mode: 0o755 });
    fs.symlinkSync("../outside.md", path.join(repo, "link.md"));
    fs.writeFileSync(path.join(root, "outside.md"), "not the symlink blob");
    git(["config", "core.autocrlf", "true"]);
    const configBefore = fs.readFileSync(path.join(repo, ".git", "config"));
    const envBefore = { ...process.env };

    stageRawGitFiles([literal, "run.md", "link.md", "deleted.md", "gone/deleted.md", "replaced.md", "replaced.md/child.md", "collapsed.md", "collapsed.md/child.md"], { cwd: repo });

    expect(fs.readFileSync(path.join(repo, ".git", "config"))).toEqual(configBefore);
    expect({ ...process.env }).toEqual(envBefore);
    expect(childProcess.execFileSync("git", ["show", `:${literal}`], { cwd: repo })).toEqual(raw);
    expect(git(["show", ":run.md"])).toBe("#!/bin/sh\r\n");
    expect(git(["show", ":link.md"])).toBe("../outside.md");
    const index = git(["ls-files", "--stage", "-z"]);
    expect(index).toContain(`\t${literal}\0`);
    expect(index).toMatch(/100755 [0-9a-f]+ 0\trun\.md\0/);
    expect(index).toMatch(/120000 [0-9a-f]+ 0\tlink\.md\0/);
    expect(index).not.toContain("deleted.md");
    expect(git(["show", ":replaced.md/child.md"])).toBe("new");
    expect(index).not.toMatch(/\treplaced\.md\0/);
    expect(git(["show", ":collapsed.md"])).toBe("collapsed");
    expect(index).not.toContain("collapsed.md/child.md");
  });

  it("rejects traversal and symlink parents without reading their targets", () => {
    fs.mkdirSync(path.join(root, "outside"));
    fs.writeFileSync(path.join(root, "outside", "secret.md"), "secret");
    fs.symlinkSync("../outside", path.join(repo, "escape"));
    for (const file of ["../outside/secret.md", "/absolute.md", ".git/config", "escape/secret.md"]) {
      expect(() => stageRawGitFiles([file], { cwd: repo })).toThrow();
    }
    expect(git(["ls-files"])).toBe("");
  });

  it.each(["darwin", "win32"])("fails closed on %s without importing file contents", platform => {
    fs.writeFileSync(path.join(repo, "notes.md"), "allowed memory");
    const oid = git(["hash-object", "--no-filters", "--stdin"], "allowed memory").trim();
    vi.stubGlobal("process", new Proxy(process, { get: (target, key) => (key === "platform" ? platform : Reflect.get(target, key)) }));

    expect(() => stageRawGitFiles(["notes.md"], { cwd: repo })).toThrow("requires Linux descriptor-relative paths");

    expect(git(["ls-files"])).toBe("");
    expect(() => git(["cat-file", "-e", oid])).toThrow();
  });

  it("rejects a parent replaced with a symlink after the initial parent check", () => {
    const parent = path.join(repo, "memory");
    const outside = path.join(root, "outside");
    fs.mkdirSync(parent);
    fs.mkdirSync(outside);
    fs.writeFileSync(path.join(parent, "notes.md"), "allowed memory");
    fs.writeFileSync(path.join(outside, "notes.md"), "outside secret");
    const secretOid = git(["hash-object", "--no-filters", "--stdin"], "outside secret").trim();
    const lstat = fs.lstatSync;
    let replaced = false;
    vi.spyOn(fs, "lstatSync").mockImplementation((filename, ...args) => {
      const result = lstat(filename, ...args);
      if (!replaced && filename === parent) {
        replaced = true;
        fs.renameSync(parent, `${parent}-original`);
        fs.symlinkSync(outside, parent);
      }
      return result;
    });

    expect(() => stageRawGitFiles(["memory/notes.md"], { cwd: repo })).toThrow();

    expect(replaced).toBe(true);
    expect(git(["ls-files"])).toBe("");
    expect(() => git(["cat-file", "-e", secretOid])).toThrow();
  });

  it.each([false, true])("pins parents when replaced during file open (restored: %s)", restore => {
    const parent = path.join(repo, "memory");
    const outside = path.join(root, "outside");
    fs.mkdirSync(parent);
    fs.mkdirSync(outside);
    fs.writeFileSync(path.join(parent, "notes.md"), "allowed memory");
    fs.writeFileSync(path.join(outside, "notes.md"), "outside secret");
    const secretOid = git(["hash-object", "--no-filters", "--stdin"], "outside secret").trim();
    const open = fs.openSync;
    let replaced = false;
    vi.spyOn(fs, "openSync").mockImplementation((filename, ...args) => {
      if (!replaced && typeof filename === "string" && /^\/proc\/self\/fd\/\d+\/notes\.md$/.test(filename)) {
        replaced = true;
        fs.renameSync(parent, `${parent}-original`);
        fs.symlinkSync(outside, parent);
        const fd = open(filename, ...args);
        if (restore) {
          fs.unlinkSync(parent);
          fs.renameSync(`${parent}-original`, parent);
        }
        return fd;
      }
      return open(filename, ...args);
    });

    if (restore) {
      stageRawGitFiles(["memory/notes.md"], { cwd: repo });
      expect(git(["show", ":memory/notes.md"])).toBe("allowed memory");
    } else {
      expect(() => stageRawGitFiles(["memory/notes.md"], { cwd: repo })).toThrow();
      expect(git(["ls-files"])).toBe("");
    }
    expect(replaced).toBe(true);
    expect(() => git(["cat-file", "-e", secretOid])).toThrow();
  });

  it("keeps handler staging conversion-free when a new driver appears after every config probe", () => {
    fs.writeFileSync(path.join(repo, "deleted.md"), "old");
    git(["add", "."]);
    git(["commit", "-m", "tracked"]);
    fs.unlinkSync(path.join(repo, "deleted.md"));
    fs.writeFileSync(path.join(repo, "notes.md"), "raw memory\r\n");
    fs.chmodSync(path.join(repo, "notes.md"), 0o755);
    const literal = ":(glob)*\t\n\\raw.md";
    fs.writeFileSync(path.join(repo, literal), Buffer.from([0, 255, 13, 10]));
    fs.symlinkSync("../outside.md", path.join(repo, "link.md"));
    fs.writeFileSync(path.join(root, "outside.md"), "not the symlink blob");
    const marker = path.join(repo, "filter-ran");
    const originalSpawn = childProcess.spawnSync;
    let probes = 0;
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      const result = originalSpawn(command, args, options);
      if (command === "git" && args.includes("--name-only") && args.includes("--get-regexp")) {
        const driver = `race${++probes}`;
        git(["config", `filter.${driver}.clean`, `touch '${marker}'; cat`]);
        git(["config", `filter.${driver}.required`, "true"]);
        fs.writeFileSync(path.join(repo, ".gitattributes"), `*.md filter=${driver}\n`);
      }
      return result;
    });
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, {
      push_repo_memory: { memories: [{ id: "default", dir: repo, allowed_extensions: [".md"], max_file_size: 1024, max_patch_size: 4096, max_file_count: 5 }] },
    });

    const result = handlers.pushRepoMemoryHandler({});

    expect(result.isError).toBeUndefined();
    expect(JSON.parse(result.content[0].text).result).toBe("success");
    expect(probes).toBeGreaterThan(0);
    expect(fs.existsSync(marker)).toBe(false);
    expect(git(["show", ":notes.md"])).toBe("raw memory\r\n");
    expect(git(["show", ":link.md"])).toBe("../outside.md");
    expect(childProcess.execFileSync("git", ["show", `:${literal}`], { cwd: repo })).toEqual(Buffer.from([0, 255, 13, 10]));
    expect(git(["ls-files", "--stage", "-z"])).toMatch(/100755 [0-9a-f]+ 0\tnotes\.md\0/);
    expect(git(["ls-files", "-z"])).not.toContain("deleted.md");
    expect(git(["config", "--get", `filter.race${probes}.clean`])).toContain("touch");
  });

  it("does not forward a large filtered patch to configuration probes", () => {
    const content = "large filtered line\r\n".repeat(100000);
    fs.writeFileSync(path.join(repo, "large.md"), content);
    git(["config", "filter.noop.clean", "cat"]);
    git(["config", "filter.noop.required", "true"]);
    fs.writeFileSync(path.join(repo, ".gitattributes"), "*.md filter=noop\n");
    const diff = childProcess.spawnSync("git", ["diff", "--no-index", "--", "/dev/null", "large.md"], { cwd: repo, encoding: "utf8", maxBuffer: 100 * 1024 * 1024 });
    expect(diff.status).toBe(1);
    const patch = diff.stdout;
    const originalSpawn = childProcess.spawnSync;
    const probeInputs = [];
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (args.includes("--get-regexp")) probeInputs.push(options.input);
      return originalSpawn(command, args, options);
    });

    execLocalGitSync(["apply", "--cached", "--whitespace=nowarn"], { cwd: repo, input: patch });

    expect(Buffer.byteLength(patch)).toBeGreaterThan(1024 * 1024);
    expect(probeInputs).toEqual([undefined]);
    expect(git(["show", ":large.md"])).toBe(content);
  });
});
