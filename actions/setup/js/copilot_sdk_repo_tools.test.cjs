import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFile, execFileSync } from "node:child_process";
import { promisify } from "node:util";

const require = createRequire(import.meta.url);
const { createCopilotSDKRepositoryRuntime, parseRepositoryAction, REPOSITORY_ACTIONS } = require("./copilot_sdk_repo_tools.cjs");
const { parseGoRepositoryProfile } = require("./copilot_sdk_repo_policy.cjs");
const { inspectRepositoryFile, parseRepositoryChanges } = require("./copilot_sdk_repo_workspace.cjs");
const { verifyRepositoryProjection } = require("./copilot_sdk_repo_projection.cjs");
const { createRepositoryFailure, MAX_REPOSITORY_FAILURE_BYTES } = require("./copilot_sdk_repo_diagnostics.cjs");
const exec = promisify(execFile);
const defineTool = (name, definition) => ({ name, ...definition });
const fixtures = [];
let goEnvironment;

beforeAll(() => {
  const GOROOT = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 }).trim();
  goEnvironment = { GOROOT };
});

afterEach(async () => {
  vi.restoreAllMocks();
  for (const fixture of fixtures.splice(0)) {
    await fixture.runtime?.close();
    fs.rmSync(fixture.scratch, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
  }
});

async function directProcess(options) {
  try {
    const result = await exec(options.command, options.args, { cwd: options.cwd, env: options.env, encoding: "utf8", timeout: options.timeoutMs, maxBuffer: options.maxOutputBytes, signal: options.signal, windowsHide: true });
    return { exitCode: 0, stdout: result.stdout, stderr: result.stderr };
  } catch (error) {
    if (typeof error.code === "number") return { exitCode: error.code, stdout: error.stdout, stderr: error.stderr };
    throw error;
  }
}

function failureDiagnostic(result) {
  expect(result).toEqual({ resultType: "failure", textResultForLlm: expect.any(String), error: expect.any(String) });
  expect(result.error).toBe(result.textResultForLlm);
  expect(Buffer.byteLength(JSON.stringify(result), "utf8")).toBeLessThanOrEqual(MAX_REPOSITORY_FAILURE_BYTES);
  return result.error;
}

function fixture({ files = {}, policy = {}, realGo = false, intercept, create = defineTool, diagnosticSecrets = [] } = {}) {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-repo-tools-test-"));
  const root = path.join(scratch, "checkout");
  const home = path.join(scratch, "home");
  fs.mkdirSync(root);
  fs.mkdirSync(home);
  const gitConfig = path.join(home, "gitconfig");
  fs.writeFileSync(gitConfig, "");
  const env = {
    ...Object.fromEntries(["PATH", "Path", "SystemRoot", "WINDIR", "PATHEXT", "TEMP", "TMP"].filter(key => process.env[key]).map(key => [key, process.env[key]])),
    ...goEnvironment,
    GOCACHE: path.join(scratch, "go-build"),
    GOMODCACHE: path.join(scratch, "go-mod"),
    HOME: home,
    USERPROFILE: home,
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_GLOBAL: gitConfig,
    GITHUB_WORKSPACE: root,
    GITHUB_REPOSITORY: "fixture/repository",
    COPILOT_CONNECTION_TOKEN: "fixture-private-sdk",
    GITHUB_TOKEN: "fixture-private-token",
    GH_TOKEN: "fixture-private-token",
    GITHUB_ENV: path.join(scratch, "github-env"),
    GITHUB_OUTPUT: path.join(scratch, "github-output"),
    NODE_OPTIONS: "--fixture-must-not-reach-children",
    CI: "true",
  };
  fs.mkdirSync(env.GOCACHE);
  fs.mkdirSync(env.GOMODCACHE);
  function git(args) {
    return execFileSync("git", ["-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], { cwd: root, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 30_000 }).trim();
  }
  function write(filename, content) {
    fs.mkdirSync(path.dirname(path.join(root, filename)), { recursive: true });
    fs.writeFileSync(path.join(root, filename), content);
  }
  git(["init", "-q", "-b", "main"]);
  const initial = { "go.mod": "module fixture\n\ngo 1.20\n", "fixture.go": "package fixture\n\nfunc Value() int { return 1 }\n", "README.md": "Fixture baseline.\n", "ignored.go": "package fixture\n", ...files };
  for (const [filename, content] of Object.entries(initial)) write(filename, content);
  git(["add", "--all", "--", "."]);
  git(["commit", "--quiet", "-m", "Fixture baseline\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"]);
  env.GITHUB_SHA = git(["rev-parse", "HEAD"]);
  const profile = parseGoRepositoryProfile({
    id: "go-repository",
    repositoryDefaultBranch: "main",
    policy: {
      "target-repo": "fixture/repository",
      base_branch: "main",
      allowed_branches: ["automation/*"],
      allowed_files: ["**"],
      excluded_files: ["ignored.go", "excluded.txt", "CHANGELOG.md"],
      protected_files: ["CHANGELOG.md", "go.mod", "go.sum"],
      protected_files_policy: "blocked",
      ...policy,
    },
  });
  const calls = [];
  const runProcess = async options => {
    calls.push(options);
    const intercepted = intercept ? await intercept(options) : undefined;
    if (intercepted !== undefined) return intercepted;
    // Lifecycle/policy fixtures use real Git and gofmt, but synthetic compiler
    // success. The explicitly named real-Go case below executes the compiler.
    if (!realGo && /^go(?:\.exe)?$/.test(path.basename(options.command))) return { exitCode: 0, stdout: "synthetic compiler result\n", stderr: "" };
    return directProcess(options);
  };
  const result = { scratch, root, home, env, git, write, calls, profile, runtime: undefined };
  fixtures.push(result);
  result.runtime = createCopilotSDKRepositoryRuntime(create, profile, { env, runProcess, diagnosticSecrets });
  result.invoke = (action, extra = {}, signal) => {
    const input = { action, ...extra };
    return result.runtime.tool.handler(input, { sessionId: "fixture", toolCallId: "fixture-call", toolName: "go_repository", arguments: input, signal });
  };
  result.call = async (action, extra = {}) => {
    const response = await result.invoke(action, extra);
    // Keep the success-oriented lifecycle assertions, but require a real SDK
    // failure envelope before adapting an owned failure to a test rejection.
    if (typeof response !== "string") throw new Error(failureDiagnostic(response));
    return JSON.parse(response);
  };
  return result;
}

describe("fixed go_repository contract", () => {
  it("offers only closed operations and one branch parameter", () => {
    const f = fixture();
    expect(f.runtime.tool).toMatchObject({ name: "go_repository", defer: "never", parameters: { additionalProperties: false, required: ["action"] } });
    expect(f.runtime.tool.parameters.properties.action.enum).toEqual(REPOSITORY_ACTIONS);
    expect(Object.keys(f.runtime.tool.parameters.properties)).toEqual(["action", "branch"]);
    expect(f.env.GOCACHE).toBe(path.join(f.scratch, "go-build"));
    expect(f.env.GOMODCACHE).toBe(path.join(f.scratch, "go-mod"));
  });

  it.each([{}, null, [], { action: "shell" }, { action: "validate", command: "ignored" }, { action: "status", cwd: ".." }, { action: "format", env: {} }, { action: "status", branch: "main" }, Object.create({ action: "status" })])(
    "rejects unconfigured input %#",
    input => {
      expect(() => parseRepositoryAction(input)).toThrow();
    }
  );

  it("does not overwrite existing branches even when publication permits recreate-ref", async () => {
    const f = fixture({ policy: { recreate_ref: true } });
    f.git(["branch", "automation/existing"]);
    await f.runtime.initialize();
    await expect(f.call("prepare_branch", { branch: "automation/existing" })).rejects.toThrow(/already exists/);
    expect(f.git(["rev-parse", "automation/existing"])).toBe(f.env.GITHUB_SHA);
    expect(f.git(["branch", "--show-current"])).toBe("main");
  });

  it.each(["main", "--orphan", "automation/../escape", "automation/a.lock", "automation/a;echo", "automation/a\nb"])("rejects unsafe or base branch %s", async branch => {
    const f = fixture();
    await f.runtime.initialize();
    const calls = f.calls.length;
    await expect(f.call("prepare_branch", { branch })).rejects.toThrow();
    expect(f.git(["branch", "--show-current"])).toBe("main");
    expect(f.calls.slice(calls).some(call => call.args.includes("switch"))).toBe(false);
  });

  it("leaves a pre-existing dirty checkout untouched", async () => {
    const f = fixture();
    f.write("README.md", "User changes.\n");
    await expect(f.runtime.initialize()).rejects.toThrow("clean checkout");
    expect(fs.readFileSync(path.join(f.root, "README.md"), "utf8")).toBe("User changes.\n");
  });

  it("binds the checkout to the workflow's trusted starting SHA", async () => {
    const f = fixture();
    f.git(["commit", "--allow-empty", "-m", "Later fixture commit\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"]);
    await expect(f.runtime.initialize()).rejects.toThrow("GITHUB_SHA");
  });
});

describe("repository file and environment boundaries", () => {
  it("formats only eligible changed Go files and leaves excluded content intact", async () => {
    const f = fixture();
    await f.runtime.initialize();
    f.write("fixture.go", "package fixture\nfunc Value()int{return 2}\n");
    f.write("ignored.go", "excluded deliberately invalid Go\n");
    const result = await f.call("format");
    expect(result.checkedGoFiles).toEqual(["fixture.go"]);
    expect(fs.readFileSync(path.join(f.root, "fixture.go"), "utf8")).toBe("package fixture\n\nfunc Value() int { return 2 }\n");
    expect(fs.readFileSync(path.join(f.root, "ignored.go"), "utf8")).toBe("excluded deliberately invalid Go\n");
  });

  it("preserves root exclusion and nested changelog rejection", async () => {
    const f = fixture();
    await f.runtime.initialize();
    f.write("CHANGELOG.md", "Excluded root.\n");
    await expect(f.call("format")).resolves.toMatchObject({ checkedGoFiles: [] });
    f.write("docs/CHANGELOG.md", "Protected nested.\n");
    await expect(f.call("format")).rejects.toThrow("file policy");
  });

  it.each(["request-review", "fallback-to-issue"])("does not turn %s protection into an unconditional edit denial", async policy => {
    const f = fixture({ policy: { protected_files: ["fixture.go"], protected_files_policy: policy } });
    await f.runtime.initialize();
    f.write("fixture.go", "package fixture\nfunc Value()int{return 2}\n");
    await expect(f.call("format")).resolves.toMatchObject({ checkedGoFiles: ["fixture.go"] });
  });

  it("runs readiness in an owned projection with a credential-free fixed environment", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("readiness");
    const goCall = f.calls.find(call => /^go(?:\.exe)?$/.test(path.basename(call.command)));
    expect(goCall.cwd).not.toBe(f.root);
    expect(goCall.args).toEqual(["test", "-run", "^$", "./..."]);
    expect(goCall.timeoutMs).toBe(5 * 60_000);
    expect(goCall.maxOutputBytes).toBe(256 * 1024);
    expect(goCall.env).toMatchObject({ CI: "true", GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly", GOPROXY: "off", GOSUMDB: "off", GOENV: "off", GOWORK: "off", GIT_ALLOW_PROTOCOL: "" });
    for (const key of ["COPILOT_CONNECTION_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENV", "GITHUB_OUTPUT", "GITHUB_WORKSPACE", "NODE_OPTIONS"]) expect(goCall.env[key]).toBeUndefined();
    expect(fs.existsSync(goCall.cwd)).toBe(false);
  });

  it("rejects hardlinks before formatting or staging through them", async () => {
    const f = fixture();
    await f.runtime.initialize();
    const outside = path.join(f.scratch, "outside.go");
    fs.writeFileSync(outside, "package fixture\n");
    fs.linkSync(outside, path.join(f.root, "linked.go"));
    expect(() => inspectRepositoryFile(f.root, "linked.go")).toThrow("non-hardlinked");
    await expect(f.call("format")).rejects.toThrow("non-hardlinked");
  });

  it("keeps deletion status separate from the current filesystem shape", () => {
    expect(parseRepositoryChanges("D\0node\0A\0node/file.go\0")).toEqual([
      { filename: "node", deleted: true },
      { filename: "node/file.go", deleted: false },
    ]);
    expect(() => parseRepositoryChanges("U\0file.go\0")).toThrow("conflicted");
    expect(() => parseRepositoryChanges("M\0../escape.go\0")).toThrow("unsafe");
  });
});

describe("projected validation and local publication bookkeeping", () => {
  it("commits validated bytes locally and keeps the complete review diff visible", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Validated review changes.\n");
    const validation = await f.call("validate");
    expect(validation.validatedFiles).toEqual(["README.md"]);
    const result = await f.call("commit");
    expect(f.git(["show", `${result.commit}:README.md`])).toBe("Validated review changes.");
    expect(f.git(["rev-parse", "main"])).toBe(f.env.GITHUB_SHA);
    expect(f.git(["status", "--porcelain"])).toBe("");
    expect(f.git(["log", "-1", "--format=%B"])).toContain("Co-authored-by: Copilot");
    expect((await f.call("diff")).stdout).toContain("+Validated review changes.");
    expect(f.calls.some(call => call.args.some(arg => ["fetch", "push", "clone"].includes(arg)))).toBe(false);
  }, 30_000);

  it("does not commit edits made after validation", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Validated.\n");
    await f.call("validate");
    f.write("README.md", "Not validated.\n");
    await expect(f.call("commit")).rejects.toThrow("edited after validation");
    expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
  }, 30_000);

  it.each(["modified", "added", "deleted"])(
    "synchronizes baseline reversions of previously %s files while preserving unrelated staged changes",
    async change => {
      const original = "Original contents.\n";
      const files = { "excluded.txt": original, ...(change === "added" ? {} : { "target.txt": original }) };
      const f = fixture({ files });
      await f.runtime.initialize();
      await f.call("prepare_branch", { branch: "automation/review" });
      if (change === "deleted") fs.unlinkSync(path.join(f.root, "target.txt"));
      else f.write("target.txt", "First committed contents.\n");
      await f.call("validate");
      await f.call("commit");

      if (change === "added") fs.unlinkSync(path.join(f.root, "target.txt"));
      else f.write("target.txt", original);
      f.write("README.md", "Second review change.\n");
      f.write("excluded.txt", "Unrelated staged contents.\n");
      f.git(["add", "--", "excluded.txt"]);
      await f.call("validate");
      const committed = await f.call("commit");
      expect(committed.files).toContain("target.txt");
      expect(f.git(["rev-list", "--count", `${f.env.GITHUB_SHA}..HEAD`])).toBe("2");
      expect(f.git(["diff", "--cached", "--name-only"])).toBe("excluded.txt");
      expect(f.git(["diff", "--name-only"])).toBe("");
      if (change === "added") expect(f.git(["ls-files", "--", "target.txt"])).toBe("");
      else expect(f.git(["show", "HEAD:target.txt"])).toBe(original.trim());
      expect(f.git(["show", "HEAD:excluded.txt"])).toBe(original.trim());
    },
    30_000
  );

  it("retains the repository-wide formatting prerequisite without modifying an unpublished baseline file", async () => {
    const unformatted = "package fixture\nfunc Value()int{return 1}\n";
    const f = fixture({ files: { "fixture.go": unformatted }, policy: { allowed_files: ["README.md"] } });
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Documentation-only review.\n");
    await expect(f.call("format")).resolves.toMatchObject({ checkedGoFiles: [] });
    await expect(f.call("validate")).rejects.toThrow("Projected Go files need formatting");
    await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
    expect(fs.readFileSync(path.join(f.root, "fixture.go"), "utf8")).toBe(unformatted);
    expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
  }, 30_000);

  it("cannot reuse validation through the former NUL-delimiter fingerprint collision", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    const separator = '\0["b.bin","100644"]\0';
    f.write("a.bin", "A");
    f.write("b.bin", `B${separator}C`);
    await f.call("validate");
    f.write("a.bin", `A${separator}B`);
    f.write("b.bin", "C");
    await expect(f.call("commit")).rejects.toThrow("edited after validation");
  }, 30_000);

  it("excludes ignored files and excluded edits from the validation checkout", async () => {
    let projected;
    const f = fixture({
      files: { ".gitignore": "local-only.go\n" },
      intercept: options => {
        if (/^go(?:\.exe)?$/.test(path.basename(options.command))) {
          projected = options.cwd;
          expect(fs.readFileSync(path.join(projected, "ignored.go"), "utf8")).toBe("package fixture\n");
          expect(fs.existsSync(path.join(projected, "local-only.go"))).toBe(false);
        }
      },
    });
    await f.runtime.initialize();
    f.write("README.md", "Review changes.\n");
    f.write("ignored.go", "Unpublished source changes.\n");
    f.write("local-only.go", "Untracked ignored source.\n");
    await f.call("validate");
    expect(projected).toBeTruthy();
    expect(fs.readFileSync(path.join(f.root, "ignored.go"), "utf8")).toBe("Unpublished source changes.\n");
  }, 30_000);

  it("real Go rejects an allowed change that depends on an excluded helper", async () => {
    const f = fixture({ realGo: true });
    await f.runtime.initialize();
    f.write("fixture.go", "package fixture\n\nfunc Value() int { return excludedHelper() }\n");
    f.write("ignored.go", "package fixture\n\nfunc excludedHelper() int { return 2 }\n");
    await expect(f.call("validate")).rejects.toThrow("undefined: excludedHelper");
    expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
  }, 120_000);

  it("enforces file limits over successive commits instead of resetting at HEAD", async () => {
    const f = fixture({ policy: { max_patch_files: 1 } });
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "First change.\n");
    await f.call("validate");
    await f.call("commit");
    f.write("notes.md", "Second change.\n");
    await expect(f.call("validate")).rejects.toThrow("more than 1 files");
    expect(f.git(["rev-list", "--count", `${f.env.GITHUB_SHA}..HEAD`])).toBe("1");
  }, 30_000);

  it("enforces the configured patch-size budget including publication framing", async () => {
    const f = fixture({ policy: { max_patch_size: 1 } });
    await f.runtime.initialize();
    f.write("README.md", "Small review.\n");
    const small = await f.call("validate");
    expect(small.patchBytes).toBeLessThanOrEqual(1024);
    f.write("README.md", `${"large".repeat(200)}\n`);
    await expect(f.call("validate")).rejects.toThrow(/max-patch-size|output limit/);
  }, 30_000);

  it("reports an existing commit and resumes index synchronization without duplicating it", async () => {
    let failSync = true;
    const f = fixture({
      intercept: options => {
        if (failSync && options.args.includes("update-index") && !options.env.GIT_INDEX_FILE) {
          failSync = false;
          return { exitCode: 128, stdout: "", stderr: "synthetic index lock" };
        }
      },
    });
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Validated commit.\n");
    await f.call("validate");
    await expect(f.call("commit")).rejects.toThrow("index synchronization failed");
    const created = f.git(["rev-parse", "HEAD"]);
    expect(created).not.toBe(f.env.GITHUB_SHA);
    const resumed = await f.call("commit");
    expect(resumed.commit).toBe(created);
    expect(f.git(["rev-list", "--count", `${f.env.GITHUB_SHA}..HEAD`])).toBe("1");
    expect(f.git(["status", "--porcelain"])).toBe("");
  }, 30_000);

  it.each(["file-to-directory", "directory-to-file"])(
    "handles %s replacements without clobbering unrelated index entries",
    async transition => {
      const files = transition === "file-to-directory" ? { node: "Old file.\n", "excluded.txt": "Original.\n" } : { "node/old.go": "package node\n", "excluded.txt": "Original.\n" };
      const f = fixture({ files });
      await f.runtime.initialize();
      await f.call("prepare_branch", { branch: "automation/review" });
      fs.rmSync(path.join(f.root, "node"), { recursive: true });
      if (transition === "file-to-directory") f.write("node/new.go", "package node\n");
      else f.write("node", "Replacement file.\n");
      f.write("excluded.txt", "Unrelated staged change.\n");
      f.git(["add", "--", "excluded.txt"]);
      await f.call("validate");
      await f.call("commit");
      expect(f.git(["cat-file", "-t", "HEAD:node"])).toBe(transition === "file-to-directory" ? "tree" : "blob");
      expect(f.git(["diff", "--cached", "--name-only"])).toBe("excluded.txt");
      expect(f.git(["show", "HEAD:excluded.txt"])).toBe("Original.");
    },
    30_000
  );
});

describe("projected mutation diagnostics", () => {
  async function check(tracked, untracked) {
    const run = vi.fn().mockResolvedValueOnce({ exitCode: 0, stdout: tracked, stderr: "" }).mockResolvedValueOnce({ exitCode: 0, stdout: untracked, stderr: "" });
    const signal = new AbortController().signal;
    const directory = path.join(os.tmpdir(), "unused-projection-diagnostic");
    const tree = "a".repeat(40);
    const error = await verifyRepositoryProjection({ run }, { tree }, directory, signal).then(
      () => null,
      error => error
    );
    expect(run.mock.calls).toEqual([
      ["git", ["diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", tree, "--"], signal, { directory }],
      ["git", ["ls-files", "--others", "-z"], signal, { directory }],
    ]);
    if (error) {
      expect(error).toBeInstanceOf(Error);
      const text = failureDiagnostic(createRepositoryFailure(error));
      expect(text).not.toContain(directory);
      return text;
    }
    return null;
  }

  it("accepts only an unchanged projected tree", async () => {
    expect(await check("", "")).toBeNull();
  });

  it("rejects and distinguishes tracked changes and ignored untracked additions with sorted, deduplicated relative paths", async () => {
    const text = await check("z.go\0a.go\0z.go\0", "validation-receipt.json\0");
    expect(text).toContain("Tracked changes: 2; untracked additions: 1.");
    expect(text.split("\n").filter(line => line.startsWith("tracked change: "))).toEqual(["tracked change: a.go", "tracked change: z.go"]);
    expect(text).toContain("untracked addition: validation-receipt.json");
  });

  it.each(["../private-canary\0", "/private-canary\0", ".git/private-canary\0", "dir\\.git\\private-canary\0", "private-canary\nforged\0", "private-canary\rforged\0", "private-canary\0\0", "private-canary"])(
    "withholds unsafe or incomplete path lists instead of making them acceptable (%j)",
    async output => {
      for (const streams of [
        [output, ""],
        ["", output],
      ]) {
        const text = await check(...streams);
        expect(text).toContain("Go validation changed or added files");
        expect(text).toContain("path details withheld");
        expect(text).not.toContain("private-canary");
      }
    }
  );

  it("withholds excessive lists, including the combined tracked/untracked limit", async () => {
    const names = Array.from({ length: 501 }, (_, index) => `file-${index}`);
    expect(await check(names.join("\0") + "\0", "")).toContain("path details withheld");
    expect(await check(names.slice(0, 260).join("\0") + "\0", names.slice(260).join("\0") + "\0")).toContain("path details withheld");
  });

  it.each(["--option-like.txt", ":(top,glob)**", "name\u202e.json", "\u754c".repeat(300), "long".repeat(300)])("still rejects mutation %j and exposes only a bounded literal summary", async filename => {
    const text = await check("", filename + "\0");
    expect(text).toContain("Go validation changed or added files");
    const rendered = text
      .split("\n")
      .find(line => line.startsWith("untracked addition: "))
      .slice("untracked addition: ".length);
    expect(Buffer.byteLength(rendered, "utf8")).toBeLessThanOrEqual(256);
    expect(rendered).not.toMatch(/[\x00-\x1f\u202e\ufffd]/);
    if (filename.includes("\u202e")) expect(rendered).toContain("\\u202e");
  });
});

describe("owned repository failures and validation state", () => {
  it("returns a first-class failure for nonzero Go output even when it says PASS, timeout or denied", async () => {
    const secret = "already-held-runtime-secret";
    const f = fixture({
      diagnosticSecrets: [secret],
      intercept: options => {
        if (/^go(?:\.exe)?$/.test(path.basename(options.command))) return { exitCode: 17, stdout: `PASS stdout-marker ${secret}`, stderr: "timeout denied stderr-marker" };
      },
    });
    await f.runtime.initialize();
    const text = failureDiagnostic(await f.invoke("validate"));
    expect(text).toContain("go test failed with exit code 17");
    expect(text).toContain("stdout: PASS stdout-marker");
    expect(text).toContain("stderr: timeout denied stderr-marker");
    expect(text).not.toContain(secret);
  });

  it("keeps inactive and invalid-input preflight guards as rejections", async () => {
    const f = fixture();
    await expect(f.invoke("status")).rejects.toThrow("not active");
    await f.runtime.initialize();
    await expect(f.invoke("status", { branch: "main" })).rejects.toThrow("Only prepare_branch");
    await expect(f.invoke("status", { command: "anything" })).rejects.toThrow("no command");
  });

  it.each(["test", "vet", "build"])(
    "invalidates an earlier candidate when a passing Go %s stage writes an ignored file into the projection",
    async stage => {
      let mutate = false;
      const f = fixture({
        files: { ".gitignore": "validation-receipt.json\n" },
        intercept: options => {
          if (mutate && /^go(?:\.exe)?$/.test(path.basename(options.command)) && options.args[0] === stage) {
            mutate = false;
            fs.writeFileSync(path.join(options.cwd, "validation-receipt.json"), "ignored receipt");
            return { exitCode: 0, stdout: "PASS", stderr: "" };
          }
        },
      });
      await f.runtime.initialize();
      await f.call("prepare_branch", { branch: "automation/review" });
      f.write("README.md", "Candidate bytes.\n");
      await f.call("validate");
      mutate = true;
      expect(failureDiagnostic(await f.invoke("validate"))).toContain("untracked addition: validation-receipt.json");
      await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
      expect(fs.existsSync(path.join(f.root, "validation-receipt.json"))).toBe(false);
      expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
    },
    30_000
  );

  it.each(["validate", "readiness"])(
    "invalidates an earlier full validation after a failed %s, then permits a fresh repair",
    async action => {
      let fail = false;
      const f = fixture({
        intercept: options => {
          if (fail && /^go(?:\.exe)?$/.test(path.basename(options.command))) return { exitCode: 1, stdout: "PASS but failed", stderr: "validation failure marker" };
        },
      });
      await f.runtime.initialize();
      await f.call("prepare_branch", { branch: "automation/review" });
      f.write("README.md", "Unchanged candidate bytes.\n");
      await f.call("validate");
      fail = true;
      expect(failureDiagnostic(await f.invoke(action))).toContain("validation failure marker");
      fail = false;
      await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
      expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
      await f.call("validate");
      expect((await f.call("commit")).commit).not.toBe(f.env.GITHUB_SHA);
    },
    30_000
  );

  it.each(["validate", "readiness"])(
    "invalidates an earlier full validation after a truly cancelled %s",
    async action => {
      const f = fixture();
      await f.runtime.initialize();
      await f.call("prepare_branch", { branch: "automation/review" });
      f.write("README.md", "Candidate bytes.\n");
      await f.call("validate");
      const cancellation = new AbortController();
      cancellation.abort(new Error("Fixture cancelled"));
      const text = failureDiagnostic(await f.invoke(action, {}, cancellation.signal));
      expect(cancellation.signal.aborted).toBe(true);
      expect(text).toMatch(/abort|cancel/i);
      await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
      expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
    },
    30_000
  );

  it("invalidates before the full-validation checkout preflight can fail", async () => {
    let fail = false;
    const f = fixture({
      intercept: options => {
        if (fail && options.args.includes("HEAD^{commit}")) {
          fail = false;
          return { exitCode: 73, stdout: "", stderr: "checkout preflight marker" };
        }
      },
    });
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Candidate bytes.\n");
    await f.call("validate");
    fail = true;
    expect(failureDiagnostic(await f.invoke("validate"))).toContain("checkout preflight marker");
    await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
  }, 30_000);

  it("does not publish the candidate if temporary cleanup fails after test, vet and build succeed", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Candidate bytes.\n");
    await f.call("validate");
    const before = f.calls.length;
    const remove = fs.promises.rm;
    let fail = true;
    vi.spyOn(fs.promises, "rm").mockImplementation(async (directory, options) => {
      if (fail && path.basename(String(directory)).startsWith("validation-")) {
        fail = false;
        throw new Error("fixture validation cleanup marker");
      }
      return remove(directory, options);
    });
    const text = failureDiagnostic(await f.invoke("validate"));
    expect(text).toContain("repository temporary cleanup failed");
    expect(text).toContain("fixture validation cleanup marker");
    expect(
      f.calls
        .slice(before)
        .filter(call => /^go(?:\.exe)?$/.test(path.basename(call.command)))
        .map(call => call.args[0])
    ).toEqual(["test", "vet", "build"]);
    await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
    expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
  }, 30_000);

  it("checks cancellation after successful projected checks and temporary cleanup, before publishing validation", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Candidate bytes.\n");
    await f.call("validate");
    const cancellation = new AbortController();
    const remove = fs.promises.rm;
    vi.spyOn(fs.promises, "rm").mockImplementation(async (directory, options) => {
      await remove(directory, options);
      if (path.basename(String(directory)).startsWith("validation-")) cancellation.abort(new Error("Fixture cancelled after cleanup"));
    });
    expect(failureDiagnostic(await f.invoke("validate", {}, cancellation.signal))).toContain("cancelled after cleanup");
    await expect(f.call("commit")).rejects.toThrow("Run validate successfully");
    expect(f.git(["rev-parse", "HEAD"])).toBe(f.env.GITHUB_SHA);
  }, 30_000);

  it("does not invalidate successful validation when the SDK subsequently aborts the completed request signal", async () => {
    const f = fixture();
    await f.runtime.initialize();
    await f.call("prepare_branch", { branch: "automation/review" });
    f.write("README.md", "Candidate bytes.\n");
    const request = new AbortController();
    const result = await f.invoke("validate", {}, request.signal);
    expect(typeof result).toBe("string");
    expect(JSON.parse(result).validatedFiles).toEqual(["README.md"]);
    expect(Object.keys(JSON.parse(result)).sort()).toEqual(["action", "build", "patchBytes", "patchFiles", "test", "validatedFiles", "vet"]);
    request.abort(new Error("SDK request completed normally"));
    expect((await f.call("commit")).commit).not.toBe(f.env.GITHUB_SHA);
  }, 30_000);
});

describe("repository runtime ownership", () => {
  it("keeps the original rejecting operation and cleanup metadata while returning an SDK failure to its caller", async () => {
    const entered = Promise.withResolvers();
    const release = Promise.withResolvers();
    const f = fixture({
      intercept: options => {
        if (/^go(?:\.exe)?$/.test(path.basename(options.command))) {
          entered.resolve(options);
          return release.promise;
        }
      },
    });
    await f.runtime.initialize();
    const operation = f.invoke("validate");
    const options = await entered.promise;
    await expect(f.invoke("status")).rejects.toThrow("sequentially");
    const closing = f.runtime.close();
    expect(f.runtime.close()).toBe(closing);
    const closed = closing.then(
      () => null,
      error => error
    );
    expect(fs.existsSync(path.dirname(options.env.HOME))).toBe(true);
    const failure = Object.assign(new Error("cancelled fixture; owned cleanup marker"), { code: "ABORT_ERR", cleanupErrors: [new Error("owned cleanup marker")] });
    release.reject(failure);
    expect(failureDiagnostic(await operation)).toContain("owned cleanup marker");
    const error = await closed;
    expect(error).toBeInstanceOf(AggregateError);
    expect(error.message).toBe("Repository runtime cleanup failed");
    expect(error.errors).toContain(failure);
    expect(fs.existsSync(path.dirname(options.env.HOME))).toBe(false);
    f.runtime = undefined;
  }, 30_000);

  it("rejects initialization reentry and shares shutdown until initialization cleanup finishes", async () => {
    let entered;
    const firstCall = new Promise(resolve => {
      entered = resolve;
    });
    let release;
    let privateRoot;
    const f = fixture({
      intercept: options =>
        new Promise((resolve, reject) => {
          privateRoot = path.dirname(options.env.HOME);
          release = () => reject(Object.assign(new Error("cancelled fixture"), { code: "ABORT_ERR", cleanupErrors: [] }));
          entered();
        }),
    });
    const initialization = f.runtime.initialize().catch(error => error);
    await firstCall;
    await expect(f.runtime.initialize()).rejects.toThrow("initialized again");
    const closing = f.runtime.close();
    expect(f.runtime.close()).toBe(closing);
    expect(fs.existsSync(privateRoot)).toBe(true);
    release();
    await closing;
    await initialization;
    expect(fs.existsSync(privateRoot)).toBe(false);
  });

  it("cleans its private workspace if SDK tool registration throws", () => {
    const original = fs.mkdtempSync;
    const created = [];
    vi.spyOn(fs, "mkdtempSync").mockImplementation(prefix => {
      const result = original(prefix);
      if (String(prefix).includes("gh-aw-go-repository-")) created.push(result);
      return result;
    });
    expect(() =>
      fixture({
        create: () => {
          throw new Error("synthetic registration failure");
        },
      })
    ).toThrow("registration failure");
    expect(created.length).toBe(1);
    expect(created.every(directory => !fs.existsSync(directory))).toBe(true);
  });
});
