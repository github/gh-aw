import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const childProcess = require("child_process");
const { generateGitPatch, getPatchPathForBranch } = require("./generate_git_patch.cjs");
const { generateGitBundle, getBundlePathForBranch } = require("./generate_git_bundle.cjs");
const { execLocalGitSync, hasLocalOriginRemoteTrackingRef, hasMergeCommitsInRange } = require("./git_helpers.cjs");

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
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-offline-artifact-"));
    repo = path.join(root, "repo");
    fs.mkdirSync(repo);
    marker = path.join(root, "helper-ran");
    git(["init", "--initial-branch=main"]);
    git(["config", "user.name", "Test User"]);
    git(["config", "user.email", "test@example.com"]);
    fs.writeFileSync(path.join(repo, "content.txt"), "before\n");
    git(["add", "."]);
    git(["commit", "-m", "baseline"]);
    base = git(["rev-parse", "HEAD"]);
    git(["checkout", "-b", "offline-feature"]);
    fs.writeFileSync(path.join(repo, "content.txt"), "after\n");
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
      checkOfflineCalls();
    });

    for (const mode of ["full", "incremental"]) {
      it(`${kind} ${mode} never invokes a remote helper when tracking refs are missing`, async () => {
        const result = await generate("offline-feature", "main", { cwd: repo, mode, token: "synthetic-explicit-token" });
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
