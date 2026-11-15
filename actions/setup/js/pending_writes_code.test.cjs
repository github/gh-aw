import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { buildPendingWrites } = require("./pending_writes_plan.cjs");
const { getPatchPathForBranchInRepo, buildExcludePathspecs } = require("./git_patch_utils.cjs");
const { checkFileProtectionPostApply } = require("./manifest_file_helpers.cjs");

describe("pending code transport goldens", () => {
  let directory;
  let patch;
  let bundle;
  beforeEach(() => {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "pending-code-golden-"));
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn() });
    vi.stubGlobal("context", { repo: { owner: "owner", repo: "repo" }, eventName: "workflow_dispatch", payload: {} });
    vi.stubEnv("GITHUB_REPOSITORY", "owner/repo");
  });
  afterEach(() => {
    if (patch && fs.existsSync(patch)) fs.unlinkSync(patch);
    if (bundle && fs.existsSync(bundle)) fs.unlinkSync(bundle);
    fs.rmSync(directory, { recursive: true, force: true });
    patch = undefined;
    bundle = undefined;
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it.each(["patch", "bundle"])("uses the publisher's %s preparation for renames/deletions/binary/modes/submodules/exclusions", async transport => {
    const git = args => execFileSync("git", ["-c", "commit.gpgsign=false", ...args], { cwd: directory, encoding: "utf8" });
    git(["init", "--quiet"]);
    git(["config", "user.name", "test"]);
    git(["config", "user.email", "test@example.com"]);
    git(["commit", "--quiet", "--allow-empty", "-m", "seed"]);
    const seed = git(["rev-parse", "HEAD"]).trim();
    fs.writeFileSync(path.join(directory, "old.txt"), "rename\n");
    fs.writeFileSync(path.join(directory, "deleted.txt"), "delete\n");
    fs.writeFileSync(path.join(directory, "script.sh"), "#!/bin/sh\n");
    fs.writeFileSync(path.join(directory, "image.bin"), Buffer.from([0, 1, 2]));
    fs.writeFileSync(path.join(directory, "excluded.txt"), "base\n");
    git(["add", "."]);
    git(["update-index", "--add", "--cacheinfo", `160000,${seed},submodule`]);
    git(["commit", "--quiet", "-m", "base"]);
    const base = git(["rev-parse", "HEAD"]).trim();
    const branch = `pending-golden-${path.basename(directory)}`;
    git(["checkout", "--quiet", "-b", branch]);
    git(["mv", "old.txt", "new.txt"]);
    git(["rm", "--quiet", "deleted.txt"]);
    fs.chmodSync(path.join(directory, "script.sh"), 0o755);
    fs.writeFileSync(path.join(directory, "image.bin"), Buffer.from([0, 3, 4]));
    fs.writeFileSync(path.join(directory, "excluded.txt"), "must not persist\n");
    git(["add", "--", "script.sh", "image.bin", "excluded.txt"]);
    git(["update-index", "--cacheinfo", `160000,${base},submodule`]);
    git(["commit", "--quiet", "-m", "changes"]);
    patch = getPatchPathForBranchInRepo(branch, "owner/repo");
    bundle = patch.replace(/\.patch$/, ".bundle");
    fs.mkdirSync(path.dirname(patch), { recursive: true });
    fs.writeFileSync(patch, git(["format-patch", "--stdout", `${base}..HEAD`, ...(transport === "patch" ? buildExcludePathspecs(["excluded.txt"]) : [])]));
    if (transport === "bundle") git(["bundle", "create", bundle, `refs/heads/${branch}`, `^${base}`]);
    const agentOutput = path.join(directory, "agent_output.json");
    fs.writeFileSync(agentOutput, JSON.stringify({ items: [{ type: "create_pull_request", branch, title: "Golden title", body: "Golden body" }] }));
    vi.stubGlobal("github", { rest: { repos: { getBranch: async () => ({ data: { commit: { sha: base } } }) } } });
    const options = {
      agentOutputPath: agentOutput,
      handlerConfig: { create_pull_request: { base_branch: "main", excluded_files: ["excluded.txt"], preserve_branch_name: true, protected_files_policy: "allowed" } },
      context: global.context,
      localRepository: directory,
      run: { repository: "owner/repo", workflow_ref: "owner/repo/.github/workflows/golden.md@main", workflow_sha: "a".repeat(40), run_id: "1", run_attempt: "1", event: "workflow_dispatch" },
      bindings: [],
      publications: [],
    };
    const first = await buildPendingWrites(options);
    const second = await buildPendingWrites(options);
    expect(first.manifest.complete).toBe(true);
    expect(first.bytes.equals(second.bytes)).toBe(true);
    expect(first.sha256).toBe(second.sha256);
    const code = first.manifest.items[0].code;
    expect(code.transport).toBe(transport);
    expect(code.files).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "new.txt", previous_path: "old.txt", status: "renamed" }),
        expect.objectContaining({ path: "deleted.txt", status: "deleted" }),
        expect.objectContaining({ path: "image.bin", binary: true }),
        expect.objectContaining({ path: "script.sh", mode_changed: true }),
        expect.objectContaining({ path: "submodule", submodule: true }),
      ])
    );
    expect(code.evaluated_paths).not.toContain("excluded.txt");
    const evaluated = checkFileProtectionPostApply(code.evaluated_paths, { allowed_files: ["not-present"] });
    expect(evaluated.files).toEqual(code.evaluated_paths);
    expect(first.bytes.toString()).not.toContain("Golden title");
    expect(first.bytes.toString()).not.toContain("Golden body");
  });
});
