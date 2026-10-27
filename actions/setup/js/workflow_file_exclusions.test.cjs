import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";

const { getExcludedFiles, getExcludedWorkflowFiles, getWorkflowCompilationNotice, isWorkflowYamlFile, WORKFLOW_COMPILATION_NOTICE } = require("./workflow_file_exclusions.cjs");
const { generateGitPatch } = require("./generate_git_patch.cjs");
const { generateGitBundle } = require("./generate_git_bundle.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");

describe("workflow file exclusions", () => {
  it("is opt-in and preserves explicit exclusions without mutating config", () => {
    expect(getExcludedFiles({})).toEqual([]);
    expect(getExcludedFiles({ exclude_lock_yml: false })).toEqual([]);
    const config = { exclude_lock_yml: true, excluded_files: ["dist/**"] };
    expect(getExcludedFiles(config)).toEqual(["dist/**", ".github/workflows/**.lock.yml"]);
    expect(config.excluded_files).toEqual(["dist/**"]);
  });

  it("reports omitted workflow YAML by comparing Git file lists", () => {
    const files = [".github/workflows/test.lock.yml", ".github/workflows/nested/test.lock.yml", ".github/workflows/test.lock.yaml", ".github/workflows/ci.yml", ".github/workflows/test.md", "docs/test.lock.yml"];
    expect(getExcludedWorkflowFiles(files, files.slice(2))).toEqual(files.slice(0, 2));
  });

  it("detects both YAML extensions omitted by explicit exclusions", () => {
    expect(getExcludedWorkflowFiles([".github/workflows/a.yml", ".github/workflows/b.yaml", ".github/workflows/c.md"], [])).toEqual([".github/workflows/a.yml", ".github/workflows/b.yaml"]);
  });

  it("only renders a push callout when generation recorded dropped YAML", () => {
    expect(getWorkflowCompilationNotice({ message: WORKFLOW_COMPILATION_NOTICE }, { exclude_lock_yml: true })).toContain("`gh aw compile`");
    expect(getWorkflowCompilationNotice({ message: "No dropped files" }, { exclude_lock_yml: true })).toBe("");
    expect(getWorkflowCompilationNotice({ message: WORKFLOW_COMPILATION_NOTICE }, {})).toBe("");
  });

  it("does not require workflow-write scope for retained Markdown sources", () => {
    expect(isWorkflowYamlFile(".github/workflows/source.md")).toBe(false);
    expect(isWorkflowYamlFile(".github/workflows/ci.yml")).toBe(true);
    expect(isWorkflowYamlFile(".github/workflows/ci.yaml")).toBe(true);
  });
});

describe("excluded workflow lock transport", () => {
  let repoDir;
  let originalSha;
  let originalCore;
  let originalContext;
  let originalEnv;
  let base;
  const git = (...args) => execFileSync("git", args, { cwd: repoDir, encoding: "utf8", stdio: "pipe" }).trim();
  const write = (file, content) => {
    const destination = path.join(repoDir, file);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, content);
  };

  beforeEach(() => {
    originalSha = process.env.GITHUB_SHA;
    originalCore = global.core;
    originalContext = global.context;
    originalEnv = Object.fromEntries(["GITHUB_WORKSPACE", "GITHUB_REPOSITORY", "GITHUB_BASE_REF"].map(key => [key, process.env[key]]));
    global.core = { debug() {}, info() {}, warning() {}, error() {} };
    repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-lock-exclusion-"));
    git("init", "-b", "main");
    git("config", "user.email", "test@example.com");
    git("config", "user.name", "Test");
    write(".github/workflows/existing.lock.yml", "name: original\n");
    write(".github/workflows/deleted.lock.yml", "name: original\n");
    write("README.md", "original\n");
    git("add", ".");
    git("commit", "-m", "base");
    base = git("rev-parse", "HEAD");
    process.env.GITHUB_SHA = base;
    git("update-ref", "refs/remotes/origin/main", base);
    git("checkout", "-b", "changes");
    git("update-ref", "refs/remotes/origin/changes", base);
  });

  afterEach(() => {
    if (originalSha === undefined) delete process.env.GITHUB_SHA;
    else process.env.GITHUB_SHA = originalSha;
    global.core = originalCore;
    global.context = originalContext;
    for (const [key, value] of Object.entries(originalEnv)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    fs.rmSync(repoDir, { recursive: true, force: true });
  });

  it.each(["full", "incremental", "head-fallback"])("filters additions, modifications and deletions from patch and bundle (%s)", async mode => {
    write(".github/workflows/new.lock.yml", "name: excluded\n");
    write(".github/workflows/nested/new.lock.yml", "name: excluded\n");
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    fs.unlinkSync(path.join(repoDir, ".github/workflows/deleted.lock.yml"));
    write(".github/workflows/source.md", "# source\n");
    write(".github/workflows/ci.yaml", "name: retained\n");
    write("docs/example.lock.yml", "retained\n");
    write("README.md", "changed\n");
    git("add", ".");
    git("commit", "-m", "changes");
    const branch = mode === "head-fallback" ? null : "changes";
    const options = { cwd: repoDir, mode: mode === "head-fallback" ? "full" : mode, excludedFiles: getExcludedFiles({ exclude_lock_yml: true }) };
    const patch = await generateGitPatch(branch, "main", options);
    expect(patch.success, patch.error).toBe(true);
    expect(patch.excludedWorkflowFiles).toEqual([".github/workflows/deleted.lock.yml", ".github/workflows/existing.lock.yml", ".github/workflows/nested/new.lock.yml", ".github/workflows/new.lock.yml"]);
    expect(fs.readFileSync(patch.patchPath, "utf8")).not.toContain("name: excluded");
    const bundle = await generateGitBundle(branch, "main", options);
    expect(bundle.success, bundle.error).toBe(true);
    git("fetch", bundle.bundlePath, "HEAD:refs/heads/filtered");
    expect(git("diff", "--name-only", base, "filtered")).toBe(".github/workflows/ci.yaml\n.github/workflows/source.md\nREADME.md\ndocs/example.lock.yml");
    expect(git("show", "filtered:.github/workflows/existing.lock.yml")).toBe("name: original");
    expect(git("show", "filtered:.github/workflows/deleted.lock.yml")).toBe("name: original");
  });

  it.each([
    ["full", false],
    ["pinned-full", false],
    ["incremental", false],
    ["full", true],
    ["incremental", true],
  ])("stops at an all-excluded requested branch even when HEAD has unrelated changes (%s, basename glob: %s)", async (mode, basenameGlob) => {
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    git("add", ".");
    git("commit", "-m", "only lock changes");
    const tip = git("rev-parse", "HEAD");
    git("checkout", "-b", "unrelated", base);
    write("README.md", "unrelated source changes\n");
    git("add", ".");
    git("commit", "-m", "unrelated changes");
    const options = { cwd: repoDir, mode: mode === "incremental" ? mode : "full", ...(mode === "pinned-full" ? { pinnedSha: tip } : {}), excludedFiles: basenameGlob ? ["*.yml"] : getExcludedFiles({ exclude_lock_yml: true }) };
    const patch = await generateGitPatch("changes", "main", options);
    const bundle = await generateGitBundle("changes", "main", options);
    expect(patch.success).toBe(false);
    expect(bundle.success).toBe(false);
    expect(patch.error).toContain("gh aw compile");
    expect(bundle.error).toContain("gh aw compile");
    expect(patch.excludedWorkflowFiles).toEqual([".github/workflows/existing.lock.yml"]);
  });

  it.each([
    ["full", false],
    ["full", true],
    ["incremental", false],
    ["incremental", true],
  ])("preserves resolved merge content in filtered patches and bundles (%s, lock changes: %s)", async (mode, changeLock) => {
    write("README.md", "branch changes\n");
    git("add", ".");
    git("commit", "-m", "branch changes");
    git("checkout", "-b", "side", base);
    write("README.md", "side changes\n");
    write("side-only.txt", "side content\n");
    git("add", ".");
    git("commit", "-m", "side changes");
    git("checkout", "changes");
    expect(() => git("merge", "--no-ff", "side", "-m", "merge side")).toThrow();
    write("README.md", "merged resolution\n");
    write("resolution-only.txt", "created during resolution\n");
    if (changeLock) write(".github/workflows/existing.lock.yml", "name: excluded\n");
    git("add", ".");
    git("commit", "--no-edit");
    const tip = git("rev-parse", "HEAD");
    const indexTree = git("write-tree");
    const options = { cwd: repoDir, mode, pinnedSha: tip, excludedFiles: getExcludedFiles({ exclude_lock_yml: true }) };
    const patch = await generateGitPatch("changes", "main", options);
    expect(patch.success, patch.error).toBe(true);
    expect(fs.readFileSync(patch.patchPath, "utf8")).toContain("+merged resolution");
    git("worktree", "add", "--detach", path.join(repoDir, "patch-check"), base);
    execFileSync("git", ["am", patch.patchPath], { cwd: path.join(repoDir, "patch-check"), stdio: "pipe" });
    const bundle = await generateGitBundle("changes", "main", options);
    expect(bundle.success, bundle.error).toBe(true);
    git("fetch", bundle.bundlePath, "HEAD:refs/heads/filtered");
    expect(git("show", "filtered:README.md")).toBe("merged resolution");
    expect(git("show", "filtered:resolution-only.txt")).toBe("created during resolution");
    expect(git("show", "filtered:side-only.txt")).toBe("side content");
    expect(git("show", "filtered:.github/workflows/existing.lock.yml")).toBe("name: original");
    const patchTree = execFileSync("git", ["rev-parse", "HEAD^{tree}"], { cwd: path.join(repoDir, "patch-check"), encoding: "utf8" }).trim();
    expect(git("rev-parse", "filtered^{tree}")).toBe(patchTree);
    expect(git("write-tree")).toBe(indexTree);
    expect(git("rev-parse", "HEAD")).toBe(tip);
  });

  it("preserves binary data, executable modes and symlinks in the filtered tree", async () => {
    const binary = Buffer.from([0, 1, 2, 255, 0, 128]);
    write("binary.dat", binary);
    write("run.sh", "#!/bin/sh\nexit 0\n");
    fs.chmodSync(path.join(repoDir, "run.sh"), 0o755);
    fs.symlinkSync("binary.dat", path.join(repoDir, "binary-link"));
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    git("add", ".");
    git("commit", "-m", "retained file types and excluded workflow");
    const result = await generateGitBundle("changes", "main", { cwd: repoDir, excludedFiles: getExcludedFiles({ exclude_lock_yml: true }) });
    expect(result.success, result.error).toBe(true);
    git("fetch", result.bundlePath, "HEAD:refs/heads/filtered");
    expect(execFileSync("git", ["show", "filtered:binary.dat"], { cwd: repoDir })).toEqual(binary);
    expect(git("ls-tree", "filtered", "run.sh")).toMatch(/^100755 /);
    expect(git("ls-tree", "filtered", "binary-link")).toMatch(/^120000 /);
    expect(git("show", "filtered:binary-link")).toBe("binary.dat");
    expect(git("show", "filtered:.github/workflows/existing.lock.yml")).toBe("name: original");
  });

  it.each(["*.yml", "*.yaml", ".github/workflows/**", ".github/workflows/**.lock.yml"])("uses Git pathspec semantics to warn about omitted YAML (%s)", async pattern => {
    const file = pattern === "*.yaml" ? ".github/workflows/nested/new.yaml" : ".github/workflows/nested/new.lock.yml";
    write(file, "name: excluded\n");
    write("README.md", "retained\n");
    git("add", ".");
    git("commit", "-m", "yaml and source");
    const result = await generateGitPatch("changes", "main", { cwd: repoDir, excludedFiles: [pattern] });
    expect(result.success, result.error).toBe(true);
    expect(result.excludedWorkflowFiles).toEqual([file]);
    expect(fs.readFileSync(result.patchPath, "utf8")).not.toContain("name: excluded");
  });

  it.each([
    ["am", false],
    ["bundle", false],
    ["am", true],
    ["bundle", true],
  ])("records compiler callouts in declared downstream fields (%s, basename glob: %s)", async (format, basenameGlob) => {
    git("remote", "add", "origin", "https://github.com/test-owner/test-repo.git");
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    if (basenameGlob) {
      write(".github/workflows/nested/new.yml", "name: excluded\n");
      write(".github/workflows/nested/new.yaml", "name: excluded\n");
    }
    write("README.md", "changed\n");
    git("add", ".");
    git("commit", "-m", "source and lock");
    process.env.GITHUB_WORKSPACE = repoDir;
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, payload: {}, eventName: "push" };
    const entries = [];
    const config = { ...(basenameGlob ? { excluded_files: ["*.yml", "*.yaml"] } : { exclude_lock_yml: true }), allowed_files: ["README.md"], patch_format: format };
    const handlers = createHandlers({ debug() {} }, entry => entries.push(entry), { create_pull_request: config, push_to_pull_request_branch: config });
    const createResult = await handlers.createPullRequestHandler({ title: "Test", body: "Implementation", branch: "changes" });
    expect(createResult.isError, JSON.stringify(createResult)).not.toBe(true);
    expect(entries[0].body).toContain(WORKFLOW_COMPILATION_NOTICE);
    const pushResult = await handlers.pushToPullRequestBranchHandler({ branch: "changes", message: "Implementation" });
    expect(pushResult.isError, JSON.stringify(pushResult)).not.toBe(true);
    expect(entries[1].message).toContain(WORKFLOW_COMPILATION_NOTICE);
  });
});
