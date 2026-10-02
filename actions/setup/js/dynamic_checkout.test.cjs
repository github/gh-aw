// @ts-check

/**
 * Tests for dynamic_checkout.cjs - Multi-repo checkout utilities
 *
 * Note: These tests are limited because dynamic_checkout.cjs relies heavily on:
 * - GitHub Actions `exec` global for git commands
 * - GitHub Actions `core` global for logging
 *
 * Testing the actual checkout functionality requires mocking these globals,
 * which is done via setup_globals_mock.cjs in more complex integration tests.
 * Here we test the module structure and pure functions where possible.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const { getCurrentCheckoutRepo, checkoutRepo, createCheckoutManager } = require("./dynamic_checkout.cjs");

describe("dynamic_checkout exports", () => {
  it("should export getCurrentCheckoutRepo function", () => {
    expect(typeof getCurrentCheckoutRepo).toBe("function");
  });

  it("should export checkoutRepo function", () => {
    expect(typeof checkoutRepo).toBe("function");
  });

  it("should export createCheckoutManager function", () => {
    expect(typeof createCheckoutManager).toBe("function");
  });
});

describe("createCheckoutManager", () => {
  let mockExec;
  let mockCore;
  let originalExec;
  let originalCore;

  beforeEach(() => {
    // Save original globals
    originalExec = global.exec;
    originalCore = global.core;

    // Create mocks
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "https://github.com/owner/original-repo.git\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    mockCore = {
      info: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      debug: vi.fn(),
    };

    global.exec = mockExec;
    global.core = mockCore;
  });

  afterEach(() => {
    // Restore globals
    global.exec = originalExec;
    global.core = originalCore;
  });

  it("should create a manager with getCurrent and switchTo methods", () => {
    const manager = createCheckoutManager("fake-token");

    expect(typeof manager.getCurrent).toBe("function");
    expect(typeof manager.switchTo).toBe("function");
  });

  it("should respect defaultBaseBranch option", () => {
    const manager = createCheckoutManager("fake-token", { defaultBaseBranch: "develop" });

    // The manager is created, we can't easily test internal state
    // but we verify it doesn't throw
    expect(manager).toBeDefined();
  });
});

describe("checkoutRepo slug validation", () => {
  let mockExec;
  let mockCore;
  let originalExec;
  let originalCore;

  beforeEach(() => {
    originalExec = global.exec;
    originalCore = global.core;

    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "",
        stderr: "",
        exitCode: 0,
      }),
    };

    mockCore = {
      info: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      debug: vi.fn(),
    };

    global.exec = mockExec;
    global.core = mockCore;
  });

  afterEach(() => {
    global.exec = originalExec;
    global.core = originalCore;
  });

  it("should reject invalid repo slug without slash", async () => {
    const result = await checkoutRepo("invalid-slug-no-slash", "fake-token");

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid repository slug");
    expect(result.error).toContain("Expected format: owner/repo");
  });

  it("should reject repo slug with extra slashes", async () => {
    const result = await checkoutRepo("owner/repo/extra", "fake-token");

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid repository slug");
    expect(result.error).toContain("Expected format: owner/repo");
  });

  it("should reject empty owner", async () => {
    const result = await checkoutRepo("/repo", "fake-token");

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid repository slug");
  });

  it("should reject empty repo", async () => {
    const result = await checkoutRepo("owner/", "fake-token");

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid repository slug");
  });

  it("should reject empty string", async () => {
    const result = await checkoutRepo("", "fake-token");

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid repository slug");
  });

  it("should accept valid repo slug format", async () => {
    // This will proceed to try git commands, which will fail in mocks
    // but that's ok - we're testing slug validation passed
    const result = await checkoutRepo("owner/repo", "fake-token");

    // Should have proceeded past validation (may fail at git commands)
    // Either it succeeds or fails for git-related reasons, not slug validation
    if (!result.success) {
      expect(result.error).not.toContain("Invalid repository slug");
    }
  });

  it("should fail with error when specified branch does not exist, not silently fall back", async () => {
    // Simulate git checkout failing for the specified branch
    let callCount = 0;
    mockExec.exec = vi.fn().mockImplementation((_cmd, args) => {
      if (args && args[0] === "checkout") {
        throw new Error("fatal: Remote branch develop not found");
      }
      return Promise.resolve(0);
    });

    const result = await checkoutRepo("owner/repo", "fake-token", { baseBranch: "develop" });

    // Should fail with an error about the branch, NOT silently fall back to master
    expect(result.success).toBe(false);
    expect(result.error).toContain("develop");
    expect(result.error).not.toContain("master");
  });

  it("should fail with error when 'main' branch does not exist, not silently fall back to master", async () => {
    // Simulate git checkout failing for 'main' - previously this would silently try 'master'
    mockExec.exec = vi.fn().mockImplementation((_cmd, args) => {
      if (args && args[0] === "checkout") {
        throw new Error("fatal: Remote branch main not found");
      }
      return Promise.resolve(0);
    });

    const result = await checkoutRepo("owner/repo", "fake-token", { baseBranch: "main" });

    // Should fail rather than silently trying 'master'
    expect(result.success).toBe(false);
    expect(result.error).toContain("main");
  });
});

describe("checkoutRepo extraheader credential handling", () => {
  let mockExec;
  let mockCore;
  let originalExec;
  let originalCore;

  /** Build an exec mock whose `git config --get-all ...extraheader` returns `persisted`. */
  function makeExec(persisted) {
    return {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn().mockImplementation((_cmd, args) => {
        if (args && args[0] === "config" && args.includes("--get-all")) {
          return Promise.resolve({ stdout: persisted, stderr: "", exitCode: persisted ? 0 : 1 });
        }
        return Promise.resolve({ stdout: "", stderr: "", exitCode: 0 });
      }),
    };
  }

  /** Returns true if any exec.exec call configured an http.<...>.extraheader. */
  function injectedExtraheader(exec) {
    return exec.exec.mock.calls.some(([, args]) => Array.isArray(args) && args[0] === "config" && args.some(a => typeof a === "string" && a.endsWith(".extraheader")));
  }

  beforeEach(() => {
    originalExec = global.exec;
    originalCore = global.core;
    mockCore = { info: vi.fn(), error: vi.fn(), warning: vi.fn(), debug: vi.fn() };
    global.core = mockCore;
  });

  afterEach(() => {
    global.exec = originalExec;
    global.core = originalCore;
  });

  it("does not inject a second extraheader when the checkout already persisted one", async () => {
    mockExec = makeExec("AUTHORIZATION: basic PERSISTED");
    global.exec = mockExec;

    const result = await checkoutRepo("owner/repo", "fake-token", { baseBranch: "main" });

    expect(result.success).toBe(true);
    expect(injectedExtraheader(mockExec)).toBe(false);
    // origin is still repointed at the target repo so the persisted credential applies
    const setUrl = mockExec.exec.mock.calls.find(([, args]) => Array.isArray(args) && args[0] === "remote" && args[1] === "set-url");
    expect(setUrl).toBeDefined();
  });

  it("injects an extraheader when no credential is persisted", async () => {
    mockExec = makeExec("");
    global.exec = mockExec;

    const result = await checkoutRepo("owner/repo", "fake-token", { baseBranch: "main" });

    expect(result.success).toBe(true);
    expect(injectedExtraheader(mockExec)).toBe(true);
  });
});

describe("getCurrentCheckoutRepo URL parsing", () => {
  let mockCore;
  let originalExec;
  let originalCore;

  beforeEach(() => {
    originalExec = global.exec;
    originalCore = global.core;

    mockCore = {
      info: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      debug: vi.fn(),
    };

    global.core = mockCore;
  });

  afterEach(() => {
    global.exec = originalExec;
    global.core = originalCore;
  });

  it("should parse HTTPS URL format", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "https://github.com/owner/repo.git\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBe("owner/repo");
  });

  it("should parse HTTPS URL without .git suffix", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "https://github.com/my-org/my-project\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBe("my-org/my-project");
  });

  it("should parse SSH URL format", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "git@github.com:owner/repo.git\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBe("owner/repo");
  });

  it("should handle GitHub Enterprise URLs", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "https://github.mycompany.com/team/project.git\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBe("team/project");
  });

  it("should return null on git command error", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockRejectedValue(new Error("git not found")),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBeNull();
  });

  it("should normalize to lowercase", async () => {
    global.exec = {
      getExecOutput: vi.fn().mockResolvedValue({
        stdout: "https://github.com/Owner/Repo.git\n",
        stderr: "",
        exitCode: 0,
      }),
    };

    const result = await getCurrentCheckoutRepo();
    expect(result).toBe("owner/repo");
  });
});

describe("materializeRepo (real git)", () => {
  const fs = require("fs");
  const os = require("os");
  const path = require("path");
  const { spawnSync } = require("child_process");
  const { materializeRepo } = require("./dynamic_checkout.cjs");

  let tmp;
  let originalExec;
  let originalCore;
  let originalServerUrl;

  const git = (args, cwd) => {
    const r = spawnSync("git", args, { cwd, encoding: "utf8" });
    if (r.status !== 0) throw new Error(`git ${args.join(" ")} failed: ${r.stderr}`);
    return r.stdout.trim();
  };

  const realExec = {
    exec: vi.fn(async (cmd, args, opts = {}) => {
      const r = spawnSync(cmd, args, { cwd: opts.cwd, env: opts.env, encoding: "utf8" });
      if (r.status !== 0 && !opts.ignoreReturnCode) throw new Error(`${cmd} ${args.join(" ")} failed: ${r.stderr}`);
      return r.status;
    }),
    getExecOutput: vi.fn(async (cmd, args, opts = {}) => {
      const r = spawnSync(cmd, args, { cwd: opts.cwd, env: opts.env, encoding: "utf8" });
      if (r.status !== 0 && !opts.ignoreReturnCode) throw new Error(`${cmd} ${args.join(" ")} failed: ${r.stderr}`);
      return { stdout: r.stdout, stderr: r.stderr, exitCode: r.status };
    }),
  };

  beforeEach(() => {
    tmp = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-materialize-"));
    // Remote layout: <tmp>/server/<owner>/<repo>.git so GITHUB_SERVER_URL=file://<tmp>/server resolves it.
    const work = path.join(tmp, "work");
    fs.mkdirSync(work);
    git(["init", "-q", "-b", "main"], work);
    for (const n of ["1", "2"]) {
      fs.writeFileSync(path.join(work, `f${n}.txt`), n);
      git(["add", "."], work);
      git(["-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", `c${n}`], work);
    }
    const bare = path.join(tmp, "server", "octo", "demo.git");
    fs.mkdirSync(path.dirname(bare), { recursive: true });
    git(["clone", "-q", "--bare", work, bare]);
    git(["--git-dir", bare, "config", "uploadpack.allowFilter", "true"]);

    originalExec = global.exec;
    originalCore = global.core;
    originalServerUrl = process.env.GITHUB_SERVER_URL;
    global.exec = realExec;
    global.core = { info: vi.fn(), error: vi.fn(), warning: vi.fn(), debug: vi.fn(), setSecret: vi.fn() };
    process.env.GITHUB_SERVER_URL = `file://${path.join(tmp, "server")}`;
  });

  afterEach(() => {
    global.exec = originalExec;
    global.core = originalCore;
    if (originalServerUrl === undefined) delete process.env.GITHUB_SERVER_URL;
    else process.env.GITHUB_SERVER_URL = originalServerUrl;
    fs.rmSync(tmp, { recursive: true, force: true });
  });

  it("initializes a shallow checkout of the base branch with local identity and credential", async () => {
    const rootDir = path.join(tmp, "dyn");
    const result = await materializeRepo("octo/demo", "tok-123", { baseBranch: "main", rootDir });
    expect(result.success).toBe(true);
    expect(result.path).toBe(path.join(rootDir, "octo", "demo"));
    expect(fs.readFileSync(path.join(result.path, "f2.txt"), "utf8")).toBe("2");
    expect(git(["rev-parse", "--abbrev-ref", "HEAD"], result.path)).toBe("main");
    expect(git(["rev-parse", "--is-shallow-repository"], result.path)).toBe("true");
    expect(git(["config", "--local", "user.name"], result.path)).toBe("github-actions[bot]");
    const header = git(["config", "--local", "--get", `http.${process.env.GITHUB_SERVER_URL}/.extraheader`], result.path);
    expect(header).toBe(`Authorization: basic ${Buffer.from("x-access-token:tok-123").toString("base64")}`);
    expect(global.core.setSecret).toHaveBeenCalledWith("tok-123");
  });

  it("reuses and cleans an existing materialized repository", async () => {
    const rootDir = path.join(tmp, "dyn");
    const first = await materializeRepo("octo/demo", "tok", { baseBranch: "main", rootDir });
    fs.writeFileSync(path.join(first.path, "junk.txt"), "x");
    const second = await materializeRepo("OCTO/demo", "tok", { baseBranch: "main", rootDir });
    expect(second.success).toBe(true);
    expect(second.reused).toBe(true);
    expect(fs.existsSync(path.join(second.path, "junk.txt"))).toBe(false);
  });

  it("reinitializes a cached shallow checkout for a partial clone with full ancestry", async () => {
    const rootDir = path.join(tmp, "dyn-upgrade");
    const first = await materializeRepo("octo/demo", "tok", { baseBranch: "main", rootDir });
    expect(first.success).toBe(true);
    expect(git(["rev-parse", "--is-shallow-repository"], first.path)).toBe("true");

    const second = await materializeRepo("octo/demo", "tok", { partialClone: true, rootDir });
    expect(second.success).toBe(true);
    expect(second.reused).toBe(false);
    expect(git(["config", "--local", "remote.origin.partialclonefilter"], second.path)).toBe("blob:none");
    git(["fetch", "origin", "+refs/heads/main:refs/remotes/origin/main"], second.path);
    expect(git(["rev-parse", "--is-shallow-repository"], second.path)).toBe("false");
    const oldBase = git(["rev-parse", "origin/main^"], second.path);
    expect(git(["merge-base", oldBase, "origin/main"], second.path)).toBe(oldBase);
  });

  it("configures a blobless promisor remote when partialClone is set", async () => {
    const rootDir = path.join(tmp, "dyn-partial");
    const result = await materializeRepo("octo/other", "tok", { partialClone: true, rootDir });
    expect(result.success).toBe(true);
    expect(git(["config", "--local", "remote.origin.partialclonefilter"], result.path)).toBe("blob:none");
    expect(git(["config", "--local", "remote.origin.promisor"], result.path)).toBe("true");
  });

  it("fails when the base branch does not exist", async () => {
    const result = await materializeRepo("octo/demo", "tok", { baseBranch: "missing", rootDir: path.join(tmp, "dyn-missing") });
    expect(result.success).toBe(false);
    expect(result.error).toContain("missing");
  });

  it("rejects invalid slugs and missing tokens", async () => {
    expect((await materializeRepo("../evil", "tok")).success).toBe(false);
    expect((await materializeRepo("a/..", "tok")).success).toBe(false);
    expect((await materializeRepo("octo/demo", "")).success).toBe(false);
  });
});
