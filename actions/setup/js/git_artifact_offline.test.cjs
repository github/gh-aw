import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const childProcess = require("child_process");
const { generateGitPatch, getPatchPathForBranch, getGitPatchFailureMessage } = require("./generate_git_patch.cjs");
const { generateGitBundle, getBundlePathForBranch } = require("./generate_git_bundle.cjs");
const { execLocalGitSync, hasLocalOriginRemoteTrackingRef, hasMergeCommitsInRange } = require("./git_helpers.cjs");
const { WORKFLOW_COMPILATION_NOTICE } = require("./workflow_file_exclusions.cjs");

global.core = { debug: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() };

describe("local-only safe-output artifacts", () => {
  let root;
  let repo;
  let marker;
  let base;
  let config;
  let spawn;
  const artifacts = new Set();

  function git(args, cwd = repo) {
    const result = childProcess.spawnSync("git", args, { cwd, encoding: "utf8" });
    if (result.status !== 0) throw new Error(result.stderr);
    return result.stdout.trim();
  }

  beforeEach(() => {
    vi.clearAllMocks();
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-offline-artifact-"));
    repo = path.join(root, "repo");
    fs.mkdirSync(repo);
    marker = path.join(root, "helper-ran");
    git(["init", "--initial-branch=main"]);
    git(["config", "user.name", "Test User"]);
    git(["config", "user.email", "test@example.com"]);
    fs.writeFileSync(path.join(repo, ".gitattributes"), "*.txt diff=hostile filter=hostile\n");
    fs.writeFileSync(path.join(repo, "content.txt"), "before\n");
    fs.writeFileSync(path.join(repo, "excluded.txt"), "excluded before\n");
    fs.writeFileSync(path.join(repo, "binary.dat"), Buffer.from([0, 1, 2]));
    git(["add", "."]);
    git(["commit", "-m", "baseline"]);
    base = git(["rev-parse", "HEAD"]);
    git(["checkout", "-b", "offline-feature"]);
    fs.writeFileSync(path.join(repo, "content.txt"), "after\n");
    fs.writeFileSync(path.join(repo, "excluded.txt"), "excluded after\n");
    fs.writeFileSync(path.join(repo, "binary.dat"), Buffer.from([0, 3, 255]));
    git(["add", "."]);
    git(["commit", "-m", "agent change"]);
    const helper = path.join(root, "recorder.sh");
    fs.writeFileSync(helper, "#!/bin/sh\nprintf 'ran\\n' > \"$1\"\nexit 1\n", { mode: 0o700 });
    git(["remote", "add", "origin", `ext::${helper} ${marker}`]);
    git(["config", "protocol.ext.allow", "always"]);
    config = fs.readFileSync(path.join(repo, ".git", "config"), "utf8");
    vi.stubEnv("GITHUB_WORKSPACE", repo);
    vi.stubEnv("GITHUB_SHA", base);
    vi.stubEnv("GITHUB_TOKEN", "synthetic-ambient-token");
    vi.stubEnv("GIT_ALLOW_PROTOCOL", "ext:file:https");
    vi.stubEnv("GIT_NO_LAZY_FETCH", "0");
    artifacts.add(getPatchPathForBranch("offline-feature"));
    artifacts.add(getBundlePathForBranch("offline-feature"));
    spawn = vi.spyOn(childProcess, "spawnSync");
  });

  afterEach(() => {
    spawn.mockRestore();
    vi.unstubAllEnvs();
    for (const artifact of artifacts) fs.rmSync(artifact, { force: true });
    artifacts.clear();
    fs.rmSync(root, { recursive: true, force: true });
  });

  function checkOfflineCalls() {
    expect(fs.existsSync(marker)).toBe(false);
    for (const [command, args, options] of spawn.mock.calls) {
      expect(command).toBe("git");
      expect(args).not.toContain("fetch");
      expect(options.env.GIT_ALLOW_PROTOCOL).toBe("");
      expect(options.env.GIT_NO_LAZY_FETCH).toBe("1");
      expect(Object.values(options.env)).not.toContain(`Authorization: basic ${Buffer.from("x-access-token:synthetic-ambient-token").toString("base64")}`);
      expect(Object.values(options.env)).not.toContain(`Authorization: basic ${Buffer.from("x-access-token:synthetic-explicit-token").toString("base64")}`);
    }
    expect(process.env.GIT_ALLOW_PROTOCOL).toBe("ext:file:https");
    expect(process.env.GIT_NO_LAZY_FETCH).toBe("0");
    expect(fs.readFileSync(path.join(repo, ".git", "config"), "utf8")).toBe(config);
  }

  function configurePrograms() {
    spawn.mockRestore();
    const program = path.join(root, "program.sh");
    fs.writeFileSync(program, `#!/bin/sh\nprintf 'ran\\n' >> '${marker}'\nexit 1\n`, { mode: 0o700 });
    const hooks = path.join(root, "hooks");
    fs.mkdirSync(hooks);
    for (const hook of ["post-checkout", "pre-applypatch", "reference-transaction"]) {
      fs.copyFileSync(program, path.join(hooks, hook));
      fs.chmodSync(path.join(hooks, hook), 0o700);
    }
    for (const [key, value] of [
      ["diff.external", program],
      ["diff.hostile.command", program],
      ["diff.hostile.textconv", program],
      ["filter.hostile.clean", program],
      ["filter.hostile.smudge", program],
      ["filter.hostile.process", program],
      ["filter.hostile.required", "true"],
      ["core.hooksPath", hooks],
      ["core.fsmonitor", program],
      ["commit.gpgSign", "true"],
      ["tag.gpgSign", "true"],
      ["log.showSignature", "true"],
      ["gpg.program", program],
    ]) {
      git(["config", key, value]);
    }
    config = fs.readFileSync(path.join(repo, ".git", "config"), "utf8");
    spawn = vi.spyOn(childProcess, "spawnSync");
    return { program, hooks };
  }

  it.each([
    ["external diff", ["-c", "diff.hostile.command=", "diff", "--binary", "main..HEAD"]],
    ["textconv", ["diff", "--no-ext-diff", "--textconv", "main..HEAD"]],
    ["clean filter", ["hash-object", "-w", "--path=content.txt", "content.txt"]],
    ["smudge filter", ["checkout-index", "--force", "content.txt"]],
    ["process filter", ["hash-object", "-w", "--path=content.txt", "content.txt"]],
    ["signing", ["commit-tree", "-S", "HEAD^{tree}", "-p", "HEAD", "-m", "signing control"]],
    ["fsmonitor", ["status", "--porcelain"]],
    ["hooks", ["update-ref", "refs/heads/control", "BASE"]],
  ])("has a working harmless %s program control without the guard", (_kind, args) => {
    const command = args.map(arg => (arg === "BASE" ? base : arg));
    configurePrograms();
    if (_kind === "clean filter" || _kind === "smudge filter") git(["config", "--unset", "filter.hostile.process"]);
    childProcess.spawnSync("git", command, { cwd: repo, encoding: "utf8" });
    expect(fs.readFileSync(marker, "utf8")).toContain("ran\n");
  });

  it("caller config and environment cannot re-enable local inspection programs", () => {
    const { program, hooks } = configurePrograms();
    const callerEnv = {
      GIT_EXTERNAL_DIFF: program,
      GIT_CONFIG_COUNT: "5",
      GIT_CONFIG_KEY_0: "core.fsmonitor",
      GIT_CONFIG_VALUE_0: program,
      GIT_CONFIG_KEY_1: "core.hooksPath",
      GIT_CONFIG_VALUE_1: hooks,
      GIT_CONFIG_KEY_2: "commit.gpgSign",
      GIT_CONFIG_VALUE_2: "true",
      GIT_CONFIG_KEY_3: "filter.caller-only.process",
      GIT_CONFIG_VALUE_3: program,
      GIT_CONFIG_KEY_4: "filter.caller-only.required",
      GIT_CONFIG_VALUE_4: "true",
    };
    const originalEnv = { ...callerEnv };
    expect(execLocalGitSync(["-c", `diff.external=${program}`, "diff", "--ext-diff", "--textconv", "--binary", "main..HEAD"], { cwd: repo, env: callerEnv })).toContain("+after");
    execLocalGitSync(["-c", `core.fsmonitor=${program}`, "status", "--porcelain"], { cwd: repo, env: callerEnv });
    fs.appendFileSync(path.join(repo, ".gitattributes"), "content.txt filter=caller-only\n");
    fs.writeFileSync(path.join(repo, "content.txt"), "raw staged content\n");
    execLocalGitSync(["-c", `filter.hostile.process=${program}`, "add", "content.txt"], { cwd: repo, env: callerEnv });
    expect(execLocalGitSync(["show", ":content.txt"], { cwd: repo, env: callerEnv })).toBe("raw staged content\n");
    execLocalGitSync(["checkout-index", "--force", "content.txt"], { cwd: repo, env: callerEnv });
    expect(fs.readFileSync(path.join(repo, "content.txt"), "utf8")).toBe("raw staged content\n");
    execLocalGitSync(["-c", `core.hooksPath=${hooks}`, "update-ref", "refs/heads/guarded", base], { cwd: repo, env: callerEnv });
    execLocalGitSync(["-c", "commit.gpgSign=true", "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "guarded"], { cwd: repo, env: callerEnv });
    expect(callerEnv).toEqual(originalEnv);
    checkOfflineCalls();
  });

  it("fails closed when a conversion filter name cannot be overridden through -c", () => {
    const { program } = configurePrograms();
    const env = { GIT_CONFIG_COUNT: "1", GIT_CONFIG_KEY_0: "filter.a=b.process", GIT_CONFIG_VALUE_0: program };
    expect(() => execLocalGitSync(["add", "content.txt"], { cwd: repo, env })).toThrow("cannot safely disable");
    checkOfflineCalls();
  });

  it("uses local tracking refs and never restores missing ones", () => {
    expect(hasLocalOriginRemoteTrackingRef("main", { cwd: repo })).toBe(false);
    checkOfflineCalls();
    spawn.mockRestore();
    git(["update-ref", "refs/remotes/origin/main", base]);
    spawn = vi.spyOn(childProcess, "spawnSync");
    expect(hasLocalOriginRemoteTrackingRef("main", { cwd: repo })).toBe(true);
    checkOfflineCalls();
  });

  it("cannot relax the transport guard through caller options", () => {
    expect(() => execLocalGitSync(["fetch", "origin"], { cwd: repo, env: { GIT_ALLOW_PROTOCOL: "ext", GIT_NO_LAZY_FETCH: "0" }, suppressLogs: true })).toThrow(/not allowed/);
    expect(fs.existsSync(marker)).toBe(false);
    expect(spawn.mock.calls[0][2].env.GIT_ALLOW_PROTOCOL).toBe("");
  });

  it("has a working harmless remote-helper fixture without the offline guard", () => {
    const result = childProcess.spawnSync("git", ["fetch", "origin"], { cwd: repo, encoding: "utf8" });
    expect(result.status).not.toBe(0);
    expect(fs.readFileSync(marker, "utf8")).toBe("ran\n");
  });

  it("keeps pre-generation merge inspection offline", () => {
    expect(hasMergeCommitsInRange(base, "offline-feature", { cwd: repo })).toBe(false);
    checkOfflineCalls();
  });

  it("sanitizes filtered bundle Git failures even when stderr includes compiler guidance", async () => {
    spawn.mockRestore();
    const originalSpawn = childProcess.spawnSync;
    spawn = vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (args.includes("--name-only")) {
        return { status: 1, stdout: "", stderr: `No changes remain after workflow YAML exclusions.\n\n${WORKFLOW_COMPILATION_NOTICE}\nsynthetic-secret`, error: undefined, signal: null };
      }
      return originalSpawn(command, args, options);
    });
    const result = await generateGitBundle("offline-feature", "main", { cwd: repo, excludedFiles: ["excluded.txt"] });
    expect(result.success).toBe(false);
    expect(result.error).toBe("Failed to generate filtered patch for bundle synthesis: Git operation failed. Ensure the checkout contains the required refs and objects before agent execution.");
    expect(result.error).not.toContain(WORKFLOW_COMPILATION_NOTICE);
    expect(result.error).not.toContain("synthetic-secret");
    checkOfflineCalls();
  });

  it("does not recognize compiler-looking text as a structured diagnostic", () => {
    expect(getGitPatchFailureMessage({ error: `No changes remain after workflow YAML exclusions.\n\n${WORKFLOW_COMPILATION_NOTICE}` }, repo)).not.toContain(WORKFLOW_COMPILATION_NOTICE);
    expect(getGitPatchFailureMessage({ errorCode: "workflow_exclusions", error: "synthetic-secret" }, repo)).toContain(WORKFLOW_COMPILATION_NOTICE);
    expect(getGitPatchFailureMessage({ errorCode: "workflow_exclusions", error: "synthetic-secret" }, repo)).not.toContain("synthetic-secret");
  });

  it.each(["offline-feature", null])("sanitizes raw filtered patch failures with tip %s at the source", async branch => {
    spawn.mockRestore();
    const originalSpawn = childProcess.spawnSync;
    spawn = vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (args.includes("--name-only")) {
        return { status: 1, stdout: "", stderr: `${WORKFLOW_COMPILATION_NOTICE}\nhttps://synthetic-secret@example.com/private\nAuthorization: synthetic-secret`, error: undefined, signal: null };
      }
      return originalSpawn(command, args, options);
    });
    const result = await generateGitPatch(branch, "main", { cwd: repo, excludedFiles: ["excluded.txt"] });
    artifacts.add(result.patchPath);
    expect(result.success).toBe(false);
    expect(result.error).toContain("Git operation failed");
    expect(result.error).not.toContain(WORKFLOW_COMPILATION_NOTICE);
    expect(result.error).not.toContain("synthetic-secret");
    expect(result.error).not.toContain(repo);
    checkOfflineCalls();
  });

  it("does not expose caller-provided baseline refs in filtered failure messages", async () => {
    const result = await generateGitBundle("offline-feature", "main", {
      cwd: repo,
      mode: "incremental",
      incrementalBaseRef: "refs/remotes/origin/synthetic-secret",
      excludedFiles: ["excluded.txt"],
    });
    expect(result.success).toBe(false);
    expect(result.error).toContain("PR-head baseline");
    expect(result.error).toContain("checkout.fetch-depth");
    expect(result.error).not.toContain("synthetic-secret");
    expect(result.error).not.toContain(repo);
    checkOfflineCalls();
  });

  it.each([false, true])("preserves missing baseline guidance in %s filtered generation with a pinned tip", async filtered => {
    vi.stubEnv("GITHUB_SHA", "");
    const tip = execLocalGitSync(["rev-parse", "HEAD"], { cwd: repo }).trim();
    const options = { cwd: repo, pinnedSha: tip, excludedFiles: filtered ? ["excluded.txt"] : undefined };
    const patch = await generateGitPatch("offline-feature", "missing-base", options);
    expect(patch).toMatchObject({ success: false, errorCode: "missing_base" });
    expect(patch.error).toContain("checkout.fetch");
    expect(patch.error).toContain("checkout.fetch-depth");
    if (filtered) {
      const bundle = await generateGitBundle("offline-feature", "missing-base", options);
      expect(bundle.error).toContain(patch.error);
    }
    checkOfflineCalls();
  });

  it.each([false, true])("uses a local default branch for %s filtered cross-repository bundles", async filtered => {
    vi.stubEnv("GITHUB_SHA", "f".repeat(40));
    const result = await generateGitBundle("offline-feature", "main", { cwd: repo, excludedFiles: filtered ? ["excluded.txt"] : undefined });
    expect(result).toMatchObject({ success: true, baseCommit: base });
    checkOfflineCalls();
    execLocalGitSync(["bundle", "verify", result.bundlePath], { cwd: repo });
  });

  it.each([false, true])("preserves a depth-one non-root baseline in %s linked-worktree filtered bundles", async linked => {
    spawn.mockRestore();
    const shallowRepo = path.join(root, "shallow");
    git(["clone", "--depth=1", "--branch=offline-feature", `file://${repo}`, shallowRepo]);
    let checkout = shallowRepo;
    if (linked) {
      checkout = path.join(root, "linked");
      git(["worktree", "add", "-b", "linked-feature", checkout], shallowRepo);
    }
    git(["config", "user.name", "Test User"], checkout);
    git(["config", "user.email", "test@example.com"], checkout);
    const shallowBase = git(["rev-parse", "HEAD"], checkout);
    fs.writeFileSync(path.join(checkout, "content.txt"), "shallow after\n");
    fs.writeFileSync(path.join(checkout, "excluded.txt"), "shallow excluded\n");
    git(["add", "."], checkout);
    git(["commit", "-m", "shallow agent change"], checkout);
    const branch = git(["branch", "--show-current"], checkout);
    vi.stubEnv("GITHUB_SHA", shallowBase);
    spawn = vi.spyOn(childProcess, "spawnSync");
    const result = await generateGitBundle(branch, "main", { cwd: checkout, mode: "incremental", incrementalBaseSha: shallowBase, excludedFiles: ["excluded.txt"] });
    artifacts.add(result.bundlePath);
    artifacts.add(getPatchPathForBranch(branch));
    expect(result).toMatchObject({ success: true, baseCommit: shallowBase });
    checkOfflineCalls();
    const heads = execLocalGitSync(["bundle", "list-heads", result.bundlePath], { cwd: checkout });
    const tip = heads.trim().split(" ")[0];
    execLocalGitSync(["bundle", "unbundle", result.bundlePath], { cwd: checkout });
    expect(execLocalGitSync(["show", `${tip}:content.txt`], { cwd: checkout })).toBe("shallow after\n");
    expect(execLocalGitSync(["show", `${tip}:excluded.txt`], { cwd: checkout })).toBe("excluded after\n");
  });

  for (const [kind, generate] of [
    ["patch", generateGitPatch],
    ["bundle", generateGitBundle],
  ]) {
    it(`${kind} reports missing local history with checkout guidance`, async () => {
      vi.stubEnv("GITHUB_SHA", "");
      const result = await generate("offline-feature", "missing-base", { cwd: repo });
      expect(result.success).toBe(false);
      expect(result.error).toContain("No local base ref");
      expect(result.error).toContain("checkout.fetch-depth");
      expect(core.warning).not.toHaveBeenCalled();
      checkOfflineCalls();
    });

    for (const mode of ["full", "incremental"]) {
      for (const filtered of [false, true]) {
        it(`${kind} ${mode} ${filtered ? "filtered" : "unfiltered"} disables repository and environment programs`, async () => {
          const { program } = configurePrograms();
          vi.stubEnv("GIT_EXTERNAL_DIFF", program);
          vi.stubEnv("GIT_CONFIG_COUNT", "2");
          vi.stubEnv("GIT_CONFIG_KEY_0", "core.fsmonitor");
          vi.stubEnv("GIT_CONFIG_VALUE_0", program);
          vi.stubEnv("GIT_CONFIG_KEY_1", "commit.gpgSign");
          vi.stubEnv("GIT_CONFIG_VALUE_1", "true");
          const result = await generate("offline-feature", "main", { cwd: repo, mode, incrementalBaseSha: base, excludedFiles: filtered ? ["excluded.txt"] : undefined });
          artifacts.add(result.patchPath || result.bundlePath);
          expect(result).toMatchObject({ success: true });
          checkOfflineCalls();
          expect(process.env.GIT_EXTERNAL_DIFF).toBe(program);
          expect(process.env.GIT_CONFIG_VALUE_0).toBe(program);
          if (kind === "patch") {
            const patch = fs.readFileSync(result.patchPath, "utf8");
            expect(patch).toContain("+after");
            expect(patch).toContain("GIT binary patch");
            expect(patch.includes("+excluded after")).toBe(!filtered);
            if (mode === "incremental") expect(result.diffSize).toBeGreaterThan(0);
          } else {
            // Inspect/import objects without checkout so hostile filters remain
            // configured while verifying the actual bundle tree and metadata.
            const heads = execLocalGitSync(["bundle", "list-heads", result.bundlePath], { cwd: repo }).trim();
            const tip = heads.split(" ")[0];
            expect(heads).toContain(filtered ? "HEAD" : "offline-feature");
            expect(execLocalGitSync(["show", `${tip}:content.txt`], { cwd: repo })).toBe("after\n");
            expect(execLocalGitSync(["show", `${tip}:excluded.txt`], { cwd: repo })).toBe(filtered ? "excluded before\n" : "excluded after\n");
            expect(execLocalGitSync(["rev-parse", `${tip}:binary.dat`], { cwd: repo })).toBe(execLocalGitSync(["rev-parse", "HEAD:binary.dat"], { cwd: repo }));
            expect(execLocalGitSync(["show", "-s", "--format=%an <%ae>%n%s", tip], { cwd: repo })).toContain("Test User <test@example.com>");
            expect(fs.existsSync(marker)).toBe(false);
          }
        });
      }

      it(`${kind} ${mode} never invokes a remote helper when tracking refs are missing`, async () => {
        const result = await generate("offline-feature", "main", { cwd: repo, mode, token: "synthetic-explicit-token" });
        expect(core.warning).toHaveBeenCalledTimes(1);
        expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("options.token is deprecated and ignored"));
        expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("never configures authentication or fetches"));
        expect(JSON.stringify(core.warning.mock.calls)).not.toContain("synthetic-explicit-token");
        artifacts.add(result.patchPath || result.bundlePath);
        if (mode === "incremental") {
          expect(result.success).toBe(false);
          expect(result.error).toContain("Artifact generation never fetches");
          expect(result.error).toContain("checkout.fetch");
        } else {
          expect(result.success).toBe(true);
          expect(result.baseCommit).toBe(base);
        }
        checkOfflineCalls();
        if (result.success && kind === "patch") {
          expect(fs.readFileSync(result.patchPath, "utf8")).toContain("+after");
        }
        if (result.success && kind === "bundle") {
          spawn.mockRestore();
          expect(git(["bundle", "verify", result.bundlePath])).toContain("offline-feature");
          spawn = vi.spyOn(childProcess, "spawnSync");
        }
      });

      it(`${kind} ${mode} succeeds with a local immutable baseline and filtering`, async () => {
        const result = await generate("offline-feature", "main", { cwd: repo, mode, incrementalBaseSha: base, excludedFiles: ["excluded.txt"] });
        artifacts.add(result.patchPath || result.bundlePath);
        expect(result.success).toBe(true);
        expect(result.baseCommit).toBe(base);
        expect(core.warning).not.toHaveBeenCalled();
        checkOfflineCalls();
      });

      it(`${kind} ${mode} warns only once when filtering with a legacy token`, async () => {
        const options = { cwd: repo, mode, incrementalBaseSha: base, excludedFiles: ["excluded.txt"], token: "synthetic-explicit-token" };
        const result = await generate("offline-feature", "main", options);
        artifacts.add(result.patchPath || result.bundlePath);
        expect(result).toMatchObject({ success: true });
        expect(core.warning).toHaveBeenCalledTimes(1);
        expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("options.token is deprecated and ignored"));
        expect(JSON.stringify(core.warning.mock.calls)).not.toContain(options.token);
        expect(options.token).toBe("synthetic-explicit-token");
        checkOfflineCalls();
      });

      it(`${kind} ${mode} refuses implicit fetches for missing partial-clone blobs`, async () => {
        spawn.mockRestore();
        // A bundle does not require blobs belonging only to its excluded base.
        const blob = git(["rev-parse", `${kind === "bundle" ? "HEAD" : base}:content.txt`]);
        git(["config", "remote.origin.promisor", "true"]);
        git(["config", "remote.origin.partialclonefilter", "blob:none"]);
        git(["update-ref", "refs/remotes/origin/main", base]);
        fs.unlinkSync(path.join(repo, ".git", "objects", blob.slice(0, 2), blob.slice(2)));
        config = fs.readFileSync(path.join(repo, ".git", "config"), "utf8");
        spawn = vi.spyOn(childProcess, "spawnSync");
        const result = await generate("offline-feature", "main", { cwd: repo, mode, incrementalBaseSha: base });
        artifacts.add(result.patchPath || result.bundlePath);
        expect(result.success).toBe(false);
        expect(result.error).toContain("partial clone");
        expect(result.error).toContain("required objects");
        checkOfflineCalls();
      });
    }
  }
});
