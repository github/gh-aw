import { describe, it, expect, beforeEach, afterEach } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { spawnSync } from "child_process";

import { extractDiffGitHeaderEntries } from "./patch_path_helpers.cjs";
import { countUniquePatchFiles } from "./create_pull_request.cjs";
import { checkForManifestFiles, extractPathsFromPatch } from "./manifest_file_helpers.cjs";

function execGit(args, options = {}) {
  const result = spawnSync("git", args, { encoding: "utf8", ...options });
  if (result.error) throw result.error;
  if (result.status !== 0 && !options.allowFailure) {
    throw new Error(`git ${args.join(" ")} failed:\nstdout: ${result.stdout}\nstderr: ${result.stderr}`);
  }
  return result;
}

function createRepo() {
  const repoDir = fs.mkdtempSync(path.join(os.tmpdir(), "patch-path-helpers-it-"));
  execGit(["init", "-q"], { cwd: repoDir });
  execGit(["config", "user.name", "Test"], { cwd: repoDir });
  execGit(["config", "user.email", "test@example.com"], { cwd: repoDir });
  execGit(["config", "commit.gpgsign", "false"], { cwd: repoDir });
  fs.writeFileSync(path.join(repoDir, "README.md"), "init\n");
  execGit(["add", "."], { cwd: repoDir });
  execGit(["commit", "-q", "-m", "init"], { cwd: repoDir });
  return repoDir;
}

function cleanupRepo(repoDir) {
  if (repoDir && fs.existsSync(repoDir)) {
    fs.rmSync(repoDir, { recursive: true, force: true });
  }
}

function lastCommitPatch(repoDir) {
  return execGit(["show", "--pretty=format:", "--patch", "HEAD"], { cwd: repoDir }).stdout;
}

describe("patch_path_helpers integration - real git outputs", () => {
  let repoDir;

  beforeEach(() => {
    repoDir = createRepo();
  });

  afterEach(() => {
    cleanupRepo(repoDir);
  });

  it("parses real git headers for unquoted paths containing spaces", () => {
    fs.mkdirSync(path.join(repoDir, "dir with space"), { recursive: true });
    fs.writeFileSync(path.join(repoDir, "dir with space", "file name.txt"), "space\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add spaced path"], { cwd: repoDir });
    const patch = lastCommitPatch(repoDir);

    const entries = extractDiffGitHeaderEntries(patch);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toEqual(
      expect.objectContaining({
        parseable: true,
        oldPath: "dir with space/file name.txt",
        newPath: "dir with space/file name.txt",
      })
    );
    expect(countUniquePatchFiles(patch)).toBe(1);
    expect(extractPathsFromPatch(patch)).toContain("dir with space/file name.txt");
  });

  it("parses real git headers for quoted escaped filenames", () => {
    fs.writeFileSync(path.join(repoDir, 'foo"bar.txt'), "quoted\n");
    fs.writeFileSync(path.join(repoDir, "foo\\bar.txt"), "slash\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add escaped names"], { cwd: repoDir });
    const patch = lastCommitPatch(repoDir);

    const entries = extractDiffGitHeaderEntries(patch);
    expect(entries).toHaveLength(2);
    expect(entries[0].parseable).toBe(true);
    expect(entries[1].parseable).toBe(true);
    expect(countUniquePatchFiles(patch)).toBe(2);
    expect(extractPathsFromPatch(patch)).toContain('foo"bar.txt');
    expect(extractPathsFromPatch(patch)).toContain("foo\\bar.txt");
  });

  it("decodes C-style octal escapes for non-ASCII paths", () => {
    const filePath = "répo/pyproject.toml";
    fs.mkdirSync(path.join(repoDir, "répo"), { recursive: true });
    fs.writeFileSync(path.join(repoDir, filePath), "config\n");
    execGit(["config", "core.quotePath", "true"], { cwd: repoDir });
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add non-ASCII manifest"], { cwd: repoDir });
    const patch = lastCommitPatch(repoDir);

    expect(patch).toMatch(/diff --git "a\/r\\303\\251po\/pyproject\.toml"/);
    expect(extractDiffGitHeaderEntries(patch)[0].newPath).toBe(filePath);
    expect(extractPathsFromPatch(patch)).toContain(filePath);
    expect(checkForManifestFiles(patch, ["pyproject.toml"], [filePath]).hasManifestFiles).toBe(false);
  });

  it("parses real git rename headers and exposes both old/new paths", () => {
    fs.writeFileSync(path.join(repoDir, "old-name.txt"), "hello\n");
    execGit(["add", "."], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "add old file"], { cwd: repoDir });

    execGit(["mv", "old-name.txt", "new-name.txt"], { cwd: repoDir });
    execGit(["commit", "-q", "-m", "rename file"], { cwd: repoDir });
    const patch = lastCommitPatch(repoDir);

    const entries = extractDiffGitHeaderEntries(patch);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toEqual(
      expect.objectContaining({
        parseable: true,
        oldPath: "old-name.txt",
        newPath: "new-name.txt",
      })
    );
    expect(countUniquePatchFiles(patch)).toBe(1);
    expect(extractPathsFromPatch(patch)).toContain("old-name.txt");
    expect(extractPathsFromPatch(patch)).toContain("new-name.txt");
  });
});
