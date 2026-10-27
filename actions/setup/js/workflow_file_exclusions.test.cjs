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

  it("only excludes workflow locks, including nested paths", () => {
    const files = [".github/workflows/test.lock.yml", ".github/workflows/nested/test.lock.yml", ".github/workflows/test.lock.yaml", ".github/workflows/ci.yml", ".github/workflows/test.md", "docs/test.lock.yml"];
    expect(getExcludedWorkflowFiles(files, getExcludedFiles({ exclude_lock_yml: true }))).toEqual(files.slice(0, 2));
  });

  it("detects both YAML extensions omitted by explicit exclusions", () => {
    expect(getExcludedWorkflowFiles([".github/workflows/a.yml", ".github/workflows/b.yaml", ".github/workflows/c.md"], [".github/workflows/**"])).toEqual([".github/workflows/a.yml", ".github/workflows/b.yaml"]);
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

  it("reports all-excluded changes without generating an unfiltered bundle", async () => {
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    git("add", ".");
    git("commit", "-m", "only lock changes");
    const options = { cwd: repoDir, excludedFiles: getExcludedFiles({ exclude_lock_yml: true }) };
    const patch = await generateGitPatch("changes", "main", options);
    const bundle = await generateGitBundle("changes", "main", options);
    expect(patch.success).toBe(false);
    expect(bundle.success).toBe(false);
    expect(patch.error).toContain("gh aw compile");
    expect(bundle.error).toContain("gh aw compile");
  });

  it.each(["am", "bundle"])("records compiler callouts in declared downstream fields (%s)", async format => {
    git("remote", "add", "origin", "https://github.com/test-owner/test-repo.git");
    write(".github/workflows/existing.lock.yml", "name: excluded\n");
    write("README.md", "changed\n");
    git("add", ".");
    git("commit", "-m", "source and lock");
    process.env.GITHUB_WORKSPACE = repoDir;
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    global.context = { repo: { owner: "test-owner", repo: "test-repo" }, payload: {}, eventName: "push" };
    const entries = [];
    const config = { exclude_lock_yml: true, patch_format: format, allowed_files: ["README.md"] };
    const handlers = createHandlers({ debug() {} }, entry => entries.push(entry), { create_pull_request: config, push_to_pull_request_branch: config });
    const createResult = await handlers.createPullRequestHandler({ title: "Test", body: "Implementation", branch: "changes" });
    expect(createResult.isError, JSON.stringify(createResult)).not.toBe(true);
    expect(entries[0].body).toContain(WORKFLOW_COMPILATION_NOTICE);
    const pushResult = await handlers.pushToPullRequestBranchHandler({ branch: "changes", message: "Implementation" });
    expect(pushResult.isError, JSON.stringify(pushResult)).not.toBe(true);
    expect(entries[1].message).toContain(WORKFLOW_COMPILATION_NOTICE);
  });
});
