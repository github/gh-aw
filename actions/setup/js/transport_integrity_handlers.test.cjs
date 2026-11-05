import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import path from "path";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";
import { getPatchPathForBranch, getPatchPathForBranchInRepo } from "./git_patch_utils.cjs";
import { getBundlePathForBranch, getBundlePathForBranchInRepo } from "./generate_git_bundle.cjs";

useExecutionProvenanceFixture();
const require = createRequire(import.meta.url);
const patchContent = `From ${"a".repeat(40)} Mon Sep 17 00:00:00 2001
From: Test <test@example.com>
Date: Thu, 1 Jan 2026 00:00:00 +0000
Subject: [PATCH] Update

diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1 +1 @@
-old
+new
`;

describe.each(["create_pull_request", "push_to_pull_request_branch"])("%s transport integrity", handlerName => {
  let directory;
  let patch;
  let bundle;
  let originalResolver;
  let originalEnv;
  let snapshots;

  beforeEach(() => {
    originalEnv = { ...process.env };
    directory = fs.mkdtempSync(path.join(process.cwd(), ".transport-handler-test-"));
    patch = path.join(directory, "aw-feature.patch");
    bundle = path.join(directory, "aw-feature.bundle");
    fs.writeFileSync(patch, patchContent);
    snapshots = new Set();
    process.env.GH_AW_PROMPTS_DIR = path.resolve("..", "md");
    process.env.GITHUB_REPOSITORY = "test-owner/test-repo";
    process.env.GITHUB_BASE_REF = "main";
    process.env.GH_AW_WORKFLOW_ID = "integrity-test";
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      debug: vi.fn(),
      error: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
    const pr = {
      number: 123,
      state: "open",
      title: "Test",
      labels: [],
      head: { ref: "feature", sha: "b".repeat(40), repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
      base: { ref: "main", sha: "c".repeat(40), repo: { full_name: "test-owner/test-repo", owner: { login: "test-owner" } } },
    };
    global.context = {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: { pull_request: pr, repository: { html_url: "https://github.com/test-owner/test-repo" } },
    };
    global.github = {
      rest: {
        pulls: { get: vi.fn().mockResolvedValue({ data: pr }), create: vi.fn(), list: vi.fn().mockResolvedValue({ data: [] }) },
        repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }), getBranch: vi.fn().mockResolvedValue({ data: { protected: false } }) },
        issues: { create: vi.fn(), addLabels: vi.fn(), addAssignees: vi.fn() },
        git: { getRef: vi.fn().mockRejectedValue({ status: 404 }), deleteRef: vi.fn(), createRef: vi.fn() },
      },
      graphql: vi.fn(),
    };
    global.exec = {
      exec: vi.fn().mockResolvedValue(0),
      getExecOutput: vi.fn(async (_command, args) => {
        if (args[0] === "rev-parse" && args[1] === "HEAD") return { exitCode: 0, stdout: "b".repeat(40), stderr: "" };
        if (args[0] === "diff" && args.includes("--name-only")) return { exitCode: 0, stdout: "file.txt\0", stderr: "" };
        if (args[0] === "bundle" && args[1] === "unbundle") return { exitCode: 0, stdout: `${"b".repeat(40)} refs/heads/feature\n`, stderr: "" };
        return { exitCode: 0, stdout: "", stderr: "" };
      }),
    };
    // Keep fixtures local; path-derivation security is covered independently by
    // resolve_transport_paths.test.cjs. The handler still ignores message paths.
    const resolver = require("./resolve_transport_paths.cjs");
    originalResolver = resolver.resolveTransportPaths;
    resolver.resolveTransportPaths = () => ({ patchPath: patch, bundlePath: fs.existsSync(bundle) ? bundle : undefined });
    delete require.cache[require.resolve(`./${handlerName}.cjs`)];
  });

  afterEach(() => {
    require("./resolve_transport_paths.cjs").resolveTransportPaths = originalResolver;
    delete require.cache[require.resolve(`./${handlerName}.cjs`)];
    for (const key of Object.keys(process.env)) if (!(key in originalEnv)) delete process.env[key];
    Object.assign(process.env, originalEnv);
    for (const snapshot of snapshots) expect(fs.existsSync(path.dirname(snapshot))).toBe(false);
    fs.rmSync(directory, { recursive: true, force: true });
    for (const key of ["core", "github", "context", "exec"]) delete global[key];
  });

  async function run(config = {}, ids = {}) {
    const { main } = require(`./${handlerName}.cjs`);
    const handler = await main({ base_branch: "main", signed_commits: false, allow_empty: true, ...config });
    return handler({ branch: "feature", title: "Test", body: "Body", patch: "/untrusted.patch", bundle: "/untrusted.bundle" }, ids);
  }

  function assertNoRemoteSideEffects() {
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
    expect(global.github.rest.issues.create).not.toHaveBeenCalled();
    expect(global.github.graphql).not.toHaveBeenCalled();
    expect(global.github.rest.git.deleteRef).not.toHaveBeenCalled();
    expect(global.github.rest.git.createRef).not.toHaveBeenCalled();
    expect(global.exec.exec.mock.calls.some(([, args]) => args[0] === "push")).toBe(false);
  }

  it.each(["duplicate patch", "duplicate bundle", "split namespace"])("rejects ambiguous %s before Git, API calls, or fallbacks", async kind => {
    const branch = `ambiguous-${path.basename(directory)}`;
    const files = [
      kind === "duplicate bundle" ? getBundlePathForBranchInRepo(branch, "test-owner/test-repo") : getPatchPathForBranchInRepo(branch, "test-owner/test-repo"),
      kind === "duplicate bundle" ? getBundlePathForBranch(branch) : getPatchPathForBranch(branch),
    ];
    if (kind === "split namespace") files[1] = getBundlePathForBranch(branch);
    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    try {
      for (const file of files) fs.writeFileSync(file, patchContent);
      require("./resolve_transport_paths.cjs").resolveTransportPaths = originalResolver;
      delete require.cache[require.resolve(`./${handlerName}.cjs`)];
      const { main } = require(`./${handlerName}.cjs`);
      const handler = await main({ base_branch: "main", allow_empty: true, fallback_as_issue: true });
      const result = await handler({ branch, title: "Test", body: "Body", repo: "test-repo" }, {});
      expect(result).toEqual(expect.objectContaining({ success: false, error_type: "ERR_TRANSPORT_INTEGRITY", error: expect.stringContaining("ambiguous transport candidates") }));
      expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("resolve: ambiguous transport candidates"));
      expect(global.exec.exec).not.toHaveBeenCalled();
      expect(global.exec.getExecOutput).not.toHaveBeenCalled();
      assertNoRemoteSideEffects();
    } finally {
      for (const file of files) fs.rmSync(file, { force: true });
    }
  });

  it.each([undefined, "test-repo", " test-owner/test-repo "])("resolves repo %s with the qualified authorized slug before applying", async repo => {
    const branch = `qualified-${path.basename(directory)}`;
    const file = getPatchPathForBranchInRepo(branch, "test-owner/test-repo");
    fs.mkdirSync("/tmp/gh-aw", { recursive: true });
    try {
      fs.writeFileSync(file, patchContent);
      const resolve = vi.fn(originalResolver);
      require("./resolve_transport_paths.cjs").resolveTransportPaths = resolve;
      delete require.cache[require.resolve(`./${handlerName}.cjs`)];
      const { main } = require(`./${handlerName}.cjs`);
      const handler = await main({ base_branch: "main", signed_commits: false });
      global.exec.exec.mockImplementation(async () => {
        fs.unlinkSync(file);
        return 0;
      });
      const result = await handler({ branch, repo, title: "Test", body: "Body" }, {});
      expect(resolve).toHaveBeenCalledWith(expect.objectContaining({ repo }), "test-owner/test-repo");
      expect(result.error_type).toBe("ERR_TRANSPORT_INTEGRITY");
      expect(global.exec.exec).toHaveBeenCalled();
      assertNoRemoteSideEffects();
    } finally {
      fs.rmSync(file, { force: true });
    }
  });

  it.each(["same-size mutation", "replacement", "deletion"])("fails closed on patch %s after validation", async attack => {
    let attacked = false;
    global.exec.exec.mockImplementation(async (_command, args) => {
      if (!attacked && args[0] === "fetch") {
        attacked = true;
        if (attack === "same-size mutation") fs.writeFileSync(patch, patchContent.replace("+new", "+bad"));
        if (attack === "replacement") {
          fs.renameSync(patch, `${patch}.old`);
          fs.writeFileSync(patch, patchContent);
        }
        if (attack === "deletion") fs.unlinkSync(patch);
      }
      return 0;
    });
    const result = await run();
    expect(attacked).toBe(true);
    expect(result.success).toBe(false);
    expect(result.error).toContain("SECURITY:");
    expect(global.exec.exec.mock.calls.some(([, args]) => args[0] === "am")).toBe(false);
    assertNoRemoteSideEffects();
  });

  it("uses a stable patch during git am and does not recover tampering as a conflict", async () => {
    let applied;
    const applySession = path.join(directory, "active-rebase-apply");
    fs.mkdirSync(applySession);
    const originalGetExecOutput = global.exec.getExecOutput.getMockImplementation();
    global.exec.getExecOutput.mockImplementation(async (command, args, options) => {
      if (command === "git" && args[0] === "rev-parse" && args[1] === "--git-path" && args[2] === "rebase-apply") {
        return { exitCode: 0, stdout: `${applySession}\n`, stderr: "" };
      }
      return originalGetExecOutput(command, args, options);
    });
    global.exec.exec.mockImplementation(async (_command, args) => {
      if (args[0] === "am" && args[1] === "--3way") {
        const snapshot = args[2];
        snapshots.add(snapshot);
        fs.writeFileSync(patch, patchContent.replace("+new", "+bad"));
        applied = fs.readFileSync(snapshot, "utf8");
        throw new Error("git conflict");
      }
      if (args[0] === "am" && args[1] === "--abort") fs.rmSync(applySession, { recursive: true, force: true });
      return 0;
    });
    const result = await run();
    expect(applied).toBe(patchContent);
    expect(result.error).toContain("SECURITY:");
    expect(global.exec.exec.mock.calls.filter(([, args]) => args[0] === "am" && args[1] === "--3way")).toHaveLength(1);
    expect(global.exec.exec).toHaveBeenCalledWith("git", ["am", "--abort"], expect.objectContaining({ cwd: expect.any(String) }));
    assertNoRemoteSideEffects();
  });

  it.each(["patch", "bundle"])("rejects %s mutations after successful application and before publication", async mode => {
    if (mode === "bundle") fs.writeFileSync(bundle, "bundle original");
    let attacked = false;
    global.core.info.mockImplementation(message => {
      const applied = mode === "patch" ? message === "Patch applied successfully" : handlerName === "create_pull_request" ? /^Checked out branch .* from bundle$/.test(message) : message.startsWith("Updated branch to bundle tip from ");
      if (applied && !attacked) {
        attacked = true;
        fs.writeFileSync(mode === "bundle" ? bundle : patch, "tampered after import");
      }
    });
    const result = await run();
    expect(attacked).toBe(true);
    expect(result.success).toBe(false);
    expect(result.error).toContain("SECURITY:");
    assertNoRemoteSideEffects();
  });

  it.runIf(handlerName === "create_pull_request")("rejects mutation during collision ls-remote before deleting an existing ref", async () => {
    const originalOutput = global.exec.getExecOutput.getMockImplementation();
    let attacked = false;
    global.exec.getExecOutput.mockImplementation(async (command, args) => {
      if (args[0] === "ls-remote" && args[1] === "--heads") {
        attacked = true;
        fs.writeFileSync(patch, "tampered during collision check");
        return { exitCode: 0, stdout: `${"b".repeat(40)} refs/heads/feature\n`, stderr: "" };
      }
      return originalOutput(command, args);
    });
    const result = await run({ preserve_branch_name: true, recreate_ref: true });
    expect(attacked).toBe(true);
    expect(result.error).toContain("SECURITY:");
    assertNoRemoteSideEffects();
  });

  it("checks integrity inside signed push after asynchronous commit validation and before remote mutation", async () => {
    const originalOutput = global.exec.getExecOutput.getMockImplementation();
    const sha = "a".repeat(40);
    const parent = "b".repeat(40);
    let attacked = false;
    global.exec.getExecOutput.mockImplementation(async (command, args) => {
      if (args[0] === "rev-parse" && args[1]?.endsWith("^{commit}")) return { exitCode: 0, stdout: parent, stderr: "" };
      if (args[0] === "rev-list" && args.includes("--parents")) return { exitCode: 0, stdout: `${sha} ${parent}\n`, stderr: "" };
      if (args[0] === "ls-remote") return { exitCode: 0, stdout: `${parent} refs/heads/feature\n`, stderr: "" };
      if (args[0] === "log" && args.includes("--format=%B") && args.includes(sha)) {
        attacked = true;
        fs.writeFileSync(patch, "tampered during signed-push validation");
        return { exitCode: 0, stdout: "Update\n", stderr: "" };
      }
      return originalOutput(command, args);
    });
    const result = await run({ signed_commits: true });
    expect(attacked).toBe(true);
    expect(result.error).toContain("SECURITY:");
    assertNoRemoteSideEffects();
  });

  it.each(["prerequisite verification", "bundle import", "prerequisite recovery"])("freezes bundle input and refuses tampering during %s", async stage => {
    fs.writeFileSync(bundle, "bundle original");
    const originalOutput = global.exec.getExecOutput.getMockImplementation();
    const prerequisite = "d".repeat(40);
    let imports = 0;
    let attacked = false;
    let recovered = false;
    const tamper = () => {
      attacked = true;
      fs.writeFileSync(bundle, "bundle tampered");
    };
    global.exec.exec.mockImplementation(async (_command, args) => {
      return 0;
    });
    global.exec.getExecOutput.mockImplementation(async (command, args) => {
      if (args[0] === "bundle" && args[1] === "verify") {
        if (stage === "prerequisite verification") tamper();
        return {
          exitCode: 0,
          stdout: stage === "prerequisite recovery" ? `The bundle requires this ref:\n  ${prerequisite}\n` : "",
          stderr: "",
        };
      }
      if (args[0] === "cat-file" && args[1] === "-e" && args[2] === `${prerequisite}^{commit}`) {
        return { exitCode: recovered ? 0 : 1, stdout: "", stderr: "" };
      }
      if (args[0] === "fetch" && args.includes(prerequisite)) {
        tamper();
        recovered = true;
        return { exitCode: 0, stdout: "", stderr: "" };
      }
      if (args[0] === "bundle" && args[1] === "unbundle") {
        imports++;
        const snapshot = args.find(arg => arg.endsWith(".bundle"));
        snapshots.add(snapshot);
        expect(fs.readFileSync(snapshot, "utf8")).toBe("bundle original");
        if (stage === "bundle import") tamper();
        return { exitCode: 0, stdout: `${"b".repeat(40)} refs/heads/feature\n`, stderr: "" };
      }
      return originalOutput(command, args);
    });
    const result = await run(stage === "prerequisite recovery" ? { allow_full_history_fetch: true } : {});
    expect(attacked).toBe(true);
    expect(imports).toBe(stage === "bundle import" ? 1 : 0);
    expect(result.success).toBe(false);
    expect(result.error).toContain("SECURITY:");
    assertNoRemoteSideEffects();
  });

  it("applies trusted patch transformations from captured bytes without rewriting canonical artifacts", async () => {
    const content = patchContent.replace("+new", "+#aw_example");
    fs.writeFileSync(patch, content);
    let applied;
    global.exec.exec.mockImplementation(async (_command, args) => {
      if (args[0] === "am" && args[1] === "--3way") {
        snapshots.add(args[2]);
        applied = fs.readFileSync(args[2], "utf8");
        // Stop before push; no conflict recovery should bless or reread disk.
        fs.unlinkSync(patch);
      }
      return 0;
    });
    const result = await run({ commit_title_suffix: " [suffix]" }, { aw_example: { repo: "test-owner/test-repo", number: 123 } });
    expect(result.error).toContain("SECURITY:");
    if (handlerName === "create_pull_request") expect(applied).toContain("+#123");
    else expect(applied).toContain("Subject: [PATCH] Update [suffix]");
    assertNoRemoteSideEffects();
  });
});
