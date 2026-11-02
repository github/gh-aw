import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "child_process";
import crypto from "crypto";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();
const require = createRequire(import.meta.url);
const { createObjectPack, recoverObjectPack, canRecoverPatch, isObjectPack } = require("./object_pack_transport.cjs");
const { createTransportIntegrity } = require("./transport_integrity.cjs");
const { generateGitPatch } = require("./generate_git_patch.cjs");
const { generateGitBundle } = require("./generate_git_bundle.cjs");
const { getPatchPathForBranchInRepo } = require("./git_patch_utils.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");

describe("object-pack packaging and application with real Git", { timeout: 60000 }, () => {
  let root, author, agent, remote, destination, repoSlug, branch, publishBranch, baseline, tip, expectedTree;
  const artifacts = new Set();
  const transports = [];
  const recoveryDirectories = new Set();
  const regeneratedArtifacts = new Set();

  function runGit(cwd, args, { online = false, ...options } = {}) {
    const result = childProcess.spawnSync("git", args, {
      cwd,
      encoding: "utf8",
      timeout: 60000,
      ...options,
      env: { ...process.env, GIT_ALLOW_PROTOCOL: online ? "file" : "", GIT_NO_LAZY_FETCH: online ? "0" : "1", ...options.env },
    });
    if (result.error) throw result.error;
    return { exitCode: result.status, stdout: result.stdout, stderr: result.stderr };
  }

  function git(cwd, args, options) {
    const result = runGit(cwd, args, options);
    if (result.exitCode !== 0) throw new Error(`git ${args.join(" ")}: ${result.stderr}`);
    return result.stdout.trim();
  }

  function identity(cwd) {
    git(cwd, ["config", "user.name", "Recovery Integration"]);
    git(cwd, ["config", "user.email", "recovery@example.com"]);
    git(cwd, ["config", "commit.gpgsign", "false"]);
    git(cwd, ["config", "core.filemode", "true"]);
  }

  function write(cwd, name, bytes) {
    const filename = path.join(cwd, name);
    fs.mkdirSync(path.dirname(filename), { recursive: true });
    fs.writeFileSync(filename, bytes);
  }

  function commit(cwd, message) {
    git(cwd, ["add", "-A"]);
    git(cwd, ["commit", "-qm", message]);
    return git(cwd, ["rev-parse", "HEAD"]);
  }

  function clone(source, target, args = []) {
    git(root, ["clone", "-q", "--no-local", ...args, `file://${source}`, target], { online: true });
    identity(target);
  }

  function fixture(shape = "missing-ref", { objectFormat = "sha1", dispatch = false } = {}) {
    fs.mkdirSync(author);
    git(author, ["init", "-q", "-b", "main", `--object-format=${objectFormat}`]);
    identity(author);
    write(author, "text.txt", "earlier trusted history\n");
    commit(author, "trusted ancestor");
    write(author, "text.txt", "baseline\n");
    write(author, "delete.txt", "remove me\n");
    write(author, "removed-binary.bin", Buffer.from([0, 255, 0]));
    write(author, "removed-empty.txt", "");
    write(author, "rename source.txt", "retain renamed contents\n");
    write(author, "binary.bin", Buffer.from([0, 1, 255, 0, 2]));
    write(author, "executable.sh", "#!/bin/sh\nexit 0\n");
    fs.chmodSync(path.join(author, "executable.sh"), 0o644);
    write(author, "file-to-link", "previous regular file\n");
    fs.symlinkSync("text.txt", path.join(author, "link-to-file"));
    fs.symlinkSync("text.txt", path.join(author, "removed-link"));
    fs.symlinkSync("text.txt", path.join(author, "updated-link"));
    write(author, "ignored.tmp", "original excluded contents\n");
    baseline = commit(author, "trusted baseline");
    git(author, ["checkout", "-qb", branch]);
    let checkoutSha;
    if (dispatch) {
      write(author, "preexisting.txt", "already on the dispatched feature branch\n");
      checkoutSha = commit(author, "preexisting feature work");
    }
    git(root, ["clone", "-q", "--bare", "--no-local", author, remote], { online: true });
    git(remote, ["config", "uploadpack.allowFilter", "true"]);
    git(remote, ["config", "uploadpack.allowAnySHA1InWant", "true"]);
    write(author, "text.txt", "first agent edit\n");
    write(author, "binary.bin", Buffer.from([255, 0, 17, 0, 128]));
    write(author, "new-binary.bin", Buffer.from([0, 128, 255, 0]));
    write(author, "new-empty.txt", "");
    fs.unlinkSync(path.join(author, "delete.txt"));
    fs.unlinkSync(path.join(author, "removed-binary.bin"));
    fs.unlinkSync(path.join(author, "removed-empty.txt"));
    fs.renameSync(path.join(author, "rename source.txt"), path.join(author, "renamed target.txt"));
    fs.chmodSync(path.join(author, "executable.sh"), 0o755);
    fs.unlinkSync(path.join(author, "file-to-link"));
    fs.symlinkSync("text.txt", path.join(author, "file-to-link"));
    fs.unlinkSync(path.join(author, "link-to-file"));
    write(author, "link-to-file", "now a regular file\n");
    fs.unlinkSync(path.join(author, "removed-link"));
    fs.unlinkSync(path.join(author, "updated-link"));
    fs.symlinkSync("renamed target.txt", path.join(author, "updated-link"));
    for (const name of ["space name.txt", "tab\tname.txt", "line\nbreak.txt", 'quote"file.txt', "-leading-dash.txt", "nested/caf\u00e9.txt"]) write(author, name, `contents of ${name}\n`);
    commit(author, "first agent commit");
    write(author, "text.txt", "final agent edit\n");
    tip = commit(author, "second agent commit");
    expectedTree = git(author, ["rev-parse", "HEAD^{tree}"]);
    git(author, ["config", "uploadpack.allowFilter", "true"]);
    if (shape === "complete") {
      agent = author;
    } else {
      const args = ["--single-branch", "--branch", branch];
      if (shape === "shallow") args.push("--depth=2");
      if (shape === "partial") args.push("--filter=blob:none", "--no-checkout");
      clone(author, agent, args);
      if (shape === "partial") {
        git(agent, ["checkout", "-q", branch], { online: true });
        git(agent, ["fetch", "-q", "--filter=blob:none", "origin", "main:refs/remotes/origin/main"], { online: true });
        // Agent-authored bytes must remain local; only trusted baseline blobs may be missing.
        const missing = git(agent, ["rev-list", "--objects", "--no-object-names", "--missing=print", `${baseline}..${tip}`])
          .split("\n")
          .filter(oid => oid.startsWith("?"));
        for (const oid of missing) {
          const bytes = runGit(author, ["cat-file", "blob", oid.slice(1)], { encoding: null }).stdout;
          git(agent, ["hash-object", "-w", "--stdin"], { input: bytes });
        }
        const oldBlob = git(author, ["rev-parse", `${baseline}:text.txt`]);
        expect(runGit(agent, ["cat-file", "-e", oldBlob]).exitCode).not.toBe(0);
      }
      if (shape === "shallow") {
        git(agent, ["fetch", "-q", "--depth=1", `file://${remote}`, "main:refs/remotes/origin/main"], { online: true });
        expect(git(agent, ["rev-parse", "--is-shallow-repository"])).toBe("true");
        expect(runGit(agent, ["merge-base", "origin/main", "HEAD"]).exitCode).not.toBe(0);
      }
      git(agent, ["update-ref", "-d", `refs/remotes/origin/${branch}`]);
    }
    vi.stubEnv("GITHUB_WORKSPACE", agent);
    clone(remote, destination, ["--single-branch", "--branch", "main", ...(shape === "shallow" ? ["--depth=1"] : shape === "partial" ? ["--filter=blob:none"] : [])]);
    if (shape === "shallow") expect(git(destination, ["rev-parse", "--is-shallow-repository"])).toBe("true");
    if (shape === "partial") expect(git(destination, ["config", "--get", "remote.origin.promisor"])).toBe("true");
    expect(runGit(remote, ["cat-file", "-e", tip]).exitCode).not.toBe(0);
    expect(runGit(destination, ["cat-file", "-e", tip]).exitCode).not.toBe(0);
    return { checkoutSha };
  }

  function state(cwd) {
    return {
      head: git(cwd, ["rev-parse", "HEAD"]),
      refs: git(cwd, ["for-each-ref", "--format=%(refname) %(objectname)"]),
      objects: git(cwd, ["cat-file", "--batch-all-objects", "--batch-check=%(objectname)"]).split("\n").sort(),
      shallow: fs.existsSync(path.join(cwd, ".git/shallow")) ? fs.readFileSync(path.join(cwd, ".git/shallow"), "utf8") : null,
    };
  }

  function capture(artifact) {
    const transport = createTransportIntegrity();
    transports.push(transport);
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    return transport;
  }

  function packageObjects({ pinned = tip, beforeCapture } = {}) {
    const paths = createObjectPack(branch, repoSlug, pinned, { cwd: agent });
    artifacts.add(paths.patchPath);
    artifacts.add(paths.bundlePath);
    if (beforeCapture) beforeCapture(paths);
    return { paths, transport: capture(paths) };
  }

  function redirectRecoveryFetch() {
    const spawn = childProcess.spawnSync;
    const fetches = [];
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (command === process.execPath && args[0] === "-e") {
        const request = JSON.parse(options.input);
        const patch = getPatchPathForBranchInRepo(request.branch, request.options.repoSlug);
        regeneratedArtifacts.add(patch);
        regeneratedArtifacts.add(patch.replace(/\.patch$/, ".bundle"));
        expect(options.env.GIT_ALLOW_PROTOCOL).toBe("");
        expect(JSON.stringify(options.env)).not.toContain("integration-only-token");
      }
      if (command === "git" && args.includes("fetch") && options.cwd !== destination) {
        const url = `https://github.com/${repoSlug}.git`;
        expect(args).toContain(url);
        expect(options.cwd).not.toBe(agent);
        expect(options.env.GIT_ALLOW_PROTOCOL).toBe("https");
        expect(options.env.GIT_CONFIG_GLOBAL).toBe(os.devNull);
        expect(git(options.cwd, ["cat-file", "--batch-all-objects", "--batch-check=%(objectname)"])).toBe("");
        recoveryDirectories.add(path.dirname(options.cwd));
        fetches.push({ args, options });
        // Only redirect the network boundary; fetching, importing and regeneration run real Git.
        const result = spawn(
          command,
          args.map(arg => (arg === url ? `file://${remote}` : arg)),
          { ...options, env: { ...options.env, GIT_ALLOW_PROTOCOL: "file" } }
        );
        expect(runGit(options.cwd, ["cat-file", "-e", tip]).exitCode).not.toBe(0);
        return result;
      }
      return spawn(command, args, options);
    });
    return fetches;
  }

  function options(mode = "full", extra = {}) {
    return { repo: repoSlug, branch: publishBranch, baseBranch: "main", mode, baseline, config: {}, message: {}, token: "integration-only-token", ...extra };
  }

  function execApi(cwd, transport) {
    const run = async (command, args, opts = {}) => {
      if (command !== "git") throw new Error(`Unexpected command: ${command}`);
      const result = runGit(opts.cwd || cwd, args, { online: process.env.GIT_ALLOW_PROTOCOL === "file", ...opts });
      if (result.exitCode !== 0 && !opts.ignoreReturnCode) throw new Error(result.stderr);
      return result;
    };
    const api = { getExecOutput: run, exec: async (command, args, opts) => (await run(command, args, opts)).exitCode };
    if (!transport) return api;
    const guarded = transport.guardExec(api);
    return {
      getExecOutput: (command, args, opts) => guarded.getExecOutput(command, args, { ...opts, cwd }),
      exec: (command, args, opts) => guarded.exec(command, args, { ...opts, cwd }),
    };
  }

  async function apply(transport, paths, format, { base = baseline, tree = expectedTree } = {}) {
    const { applyBundleToBranch } = require("./create_pull_request.cjs");
    git(destination, ["checkout", "-qb", publishBranch, base]);
    const api = execApi(destination, transport);
    if (format === "bundle") await applyBundleToBranch(paths.bundlePath, publishBranch, publishBranch, api);
    else await api.exec("git", ["am", "--3way", paths.patchPath]);
    expect(git(destination, ["rev-parse", "HEAD^{tree}"])).toBe(tree);
    expect(git(destination, ["status", "--porcelain"])).toBe("");
    expect(git(destination, ["for-each-ref", "--format=%(refname)", "refs/bundles/"])).toBe("");
    git(destination, ["fsck", "--strict"]);
    transport.assertUnchanged();
  }

  beforeEach(() => {
    for (const key of Object.keys(process.env)) if (key.startsWith("GIT_")) vi.stubEnv(key, undefined);
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-recovery-integration-"));
    author = path.join(root, "author");
    agent = path.join(root, "agent");
    remote = path.join(root, "trusted.git");
    destination = path.join(root, "publisher");
    const id = crypto.randomUUID();
    repoSlug = `test-owner/recovery-${id}`;
    branch = `agent/${id}`;
    publishBranch = `published/${id}`;
    vi.stubEnv("HOME", root);
    vi.stubEnv("XDG_CONFIG_HOME", root);
    vi.stubEnv("GIT_CONFIG_GLOBAL", os.devNull);
    vi.stubEnv("GIT_CONFIG_NOSYSTEM", "1");
    vi.stubEnv("GIT_CEILING_DIRECTORIES", root);
    vi.stubEnv("GITHUB_REPOSITORY", repoSlug);
    vi.stubEnv("GITHUB_SHA", undefined);
    vi.stubEnv("GH_AW_PROMPTS_DIR", path.resolve("..", "md"));
    vi.stubGlobal("core", { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() });
  });

  afterEach(async () => {
    vi.restoreAllMocks();
    for (const transport of transports.splice(0)) {
      await transport.cleanup();
      transport.dispose();
    }
    for (const filename of artifacts) fs.rmSync(filename, { force: true });
    artifacts.clear();
    for (const directory of recoveryDirectories) expect(fs.existsSync(directory)).toBe(false);
    recoveryDirectories.clear();
    for (const filename of regeneratedArtifacts) expect(fs.existsSync(filename)).toBe(false);
    regeneratedArtifacts.clear();
    fs.rmSync(root, { recursive: true, force: true });
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  const matrix = ["missing-ref", "shallow", "partial"].flatMap(shape => ["full", "incremental"].flatMap(mode => ["bundle", "am"].map(format => ({ shape, mode, format }))));
  it.each(matrix)("recovers $shape $mode output and applies $format to an independent checkout", async ({ shape, mode, format }) => {
    fixture(shape);
    const before = state(agent);
    const failed = await generateGitPatch(branch, "main", { cwd: agent, repoSlug, mode, pinnedSha: tip, ...(shape === "partial" && mode === "incremental" ? { incrementalBaseSha: baseline } : {}) });
    artifacts.add(failed.patchPath);
    expect(failed.success).toBe(false);
    expect(canRecoverPatch(failed)).toBe(true);
    const fetches = redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    const uploaded = { patch: fs.readFileSync(paths.patchPath), bundle: fs.readFileSync(paths.bundlePath) };
    const request = options(mode);
    expect(await recoverObjectPack(transport, paths, request)).toBe(true);
    expect(fetches).toHaveLength(1);
    expect(request.message.base_commit).toBe(baseline);
    expect(request.message.diff_size).toBeGreaterThan(0);
    expect(isObjectPack(transport.read(paths.bundlePath))).toBe(false);
    await apply(transport, paths, format);
    if (format === "bundle") expect(git(destination, ["rev-parse", "HEAD"])).toBe(tip);
    expect(git(destination, ["rev-list", "--count", `${baseline}..HEAD`])).toBe("2");
    expect(fs.readFileSync(path.join(destination, "binary.bin"))).toEqual(Buffer.from([255, 0, 17, 0, 128]));
    expect(git(destination, ["ls-tree", "HEAD", "executable.sh"])).toMatch(/^100755 /);
    expect(fs.readlinkSync(path.join(destination, "file-to-link"))).toBe("text.txt");
    expect(fs.lstatSync(path.join(destination, "link-to-file")).isSymbolicLink()).toBe(false);
    expect(fs.readFileSync(paths.patchPath)).toEqual(uploaded.patch);
    expect(fs.readFileSync(paths.bundlePath)).toEqual(uploaded.bundle);
    expect(state(agent)).toEqual(before);
  });

  it.each(["full", "incremental"])("queues the real %s producer before recovering and applying its artifacts", async mode => {
    fixture();
    vi.stubGlobal("context", {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: repoSlug.split("/")[1] },
      payload: { pull_request: { number: 7, head: { sha: baseline, repo: { full_name: repoSlug } }, base: { ref: "main", repo: { full_name: repoSlug } } } },
    });
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, { create_pull_request: { base_branch: "main" }, push_to_pull_request_branch: { target: "triggering" } });
    const patchPath = getPatchPathForBranchInRepo(branch, repoSlug);
    const paths = { patchPath, bundlePath: patchPath.replace(/\.patch$/, ".bundle") };
    artifacts.add(paths.patchPath);
    artifacts.add(paths.bundlePath);
    const fetches = redirectRecoveryFetch();
    const response = mode === "full" ? await handlers.createPullRequestHandler({ branch, title: "Recover incomplete checkout", body: "Preserve validated agent changes." }) : await handlers.pushToPullRequestBranchHandler({ branch });
    expect(response.isError, response.content[0].text).toBeUndefined();
    expect(JSON.parse(response.content[0].text)).toMatchObject({ result: "queued", recovery: "object-pack" });
    expect(append).toHaveBeenCalledTimes(1);
    expect(fetches).toHaveLength(0);
    const transport = capture(paths);
    await recoverObjectPack(transport, paths, options(mode));
    await apply(transport, paths, "bundle");
  });

  const publishers = ["create_pull_request", "push_to_pull_request_branch"].flatMap(handler =>
    ["bundle", "am"].flatMap(format => ["allow", "deny", ...(handler === "push_to_pull_request_branch" ? ["advanced"] : [])].map(policy => ({ handler, format, policy })))
  );
  it.each(publishers)("enforces $policy while publishing recovered $format through $handler", async ({ handler, format, policy }) => {
    fixture();
    let remoteHead = baseline;
    if (policy === "advanced") {
      git(author, ["checkout", "-q", "--detach", baseline]);
      write(author, "concurrent.txt", "a newer remote PR head\n");
      remoteHead = commit(author, "concurrent writer");
      git(author, ["push", "-q", `file://${remote}`, `${remoteHead}:refs/heads/${branch}`], { online: true });
    }
    const before = state(remote);
    packageObjects();
    const fetches = redirectRecoveryFetch();
    vi.spyOn(process, "cwd").mockReturnValue(destination);
    vi.stubEnv("GITHUB_WORKSPACE", destination);
    vi.stubEnv("GIT_ALLOW_PROTOCOL", "file");
    const pr = {
      number: 7,
      state: "open",
      title: "Recover incomplete checkout",
      labels: [],
      html_url: `https://github.com/${repoSlug}/pull/7`,
      head: { ref: branch, sha: remoteHead, repo: { full_name: repoSlug, owner: { login: "test-owner" } } },
      base: { ref: "main", sha: baseline, repo: { full_name: repoSlug, owner: { login: "test-owner" } } },
    };
    vi.stubGlobal("context", {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: repoSlug.split("/")[1] },
      payload: { pull_request: { ...pr, head: { ...pr.head, sha: baseline } }, repository: { html_url: `https://github.com/${repoSlug}` } },
    });
    const create = vi.fn(async request => ({ data: { ...pr, number: 8, head: { ref: request.head }, base: { ref: request.base } } }));
    vi.stubGlobal("github", {
      auth: vi.fn().mockResolvedValue({ token: "integration-only-token" }),
      rest: {
        pulls: { get: vi.fn().mockResolvedValue({ data: pr }), list: vi.fn().mockResolvedValue({ data: [] }), create },
        repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) },
      },
    });
    vi.stubGlobal("exec", execApi(destination));
    global.core.summary = { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) };
    const { main } = require(`./${handler}.cjs`);
    const publish = await main({
      base_branch: "main",
      target: "triggering",
      signed_commits: false,
      check_branch_protection: false,
      patch_format: format,
      fallback_as_pull_request: false,
      ...(policy === "deny" ? { allowed_files: ["not-permitted/**"] } : {}),
    });
    const result = await publish({ branch, title: pr.title, body: "Preserve validated agent changes." }, {});
    expect(fetches).toHaveLength(1);
    if (policy !== "allow") {
      expect(result, JSON.stringify(result)).toMatchObject({ success: false });
      expect(result.error).toMatch(policy === "deny" ? /allowed-files|file protection/i : /non-fast-forward|changed while/i);
      expect(state(remote)).toEqual(before);
      expect(create).not.toHaveBeenCalled();
      return;
    }
    expect(result, JSON.stringify(result)).toMatchObject({ success: true });
    const publishedBranch = result.branch_name;
    expect(git(remote, ["rev-parse", `refs/heads/${publishedBranch}^{tree}`])).toBe(expectedTree);
    expect(git(remote, ["rev-parse", "refs/heads/main"])).toBe(baseline);
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining(`Apply transport mode: ${format === "am" ? "patch" : "bundle"}`));
    if (handler === "create_pull_request") expect(create).toHaveBeenCalledTimes(1);
    else expect(create).not.toHaveBeenCalled();
  });

  it.each(["sha1", "sha256"])("exports deterministic full %s objects from packed, tagged and unreachable history", objectFormat => {
    fixture("complete", { objectFormat });
    git(author, ["tag", "-a", "annotated", "-m", "annotated object preview", baseline]);
    const hiddenBlob = git(author, ["hash-object", "-w", "--stdin"], { input: "unreachable bytes must be scanned\n" });
    git(author, ["repack", "-ad"]);
    const first = packageObjects();
    const preview = first.transport.read(first.paths.patchPath);
    const envelope = first.transport.read(first.paths.bundlePath);
    expect(preview.toString()).toContain(`Object ${hiddenBlob} blob`);
    expect(preview.toString()).toContain("annotated object preview");
    const second = createObjectPack(branch, repoSlug, tip, { cwd: agent });
    expect(fs.readFileSync(second.patchPath)).toEqual(preview);
    expect(fs.readFileSync(second.bundlePath)).toEqual(envelope);
    const boundary = envelope.indexOf(10, envelope.indexOf(10) + 1);
    const imported = path.join(root, "independent-import.git");
    git(root, ["init", "-q", "--bare", `--object-format=${objectFormat}`, imported]);
    const packId = git(imported, ["index-pack", "--strict", "--stdin"], { input: envelope.subarray(boundary + 1) })
      .split(/\s+/)
      .pop();
    git(imported, ["fsck", "--strict", tip]);
    expect(git(imported, ["rev-parse", `${tip}^{tree}`])).toBe(expectedTree);
    expect(git(imported, ["cat-file", "blob", hiddenBlob])).toBe("unreachable bytes must be scanned");
    const entries = git(imported, ["verify-pack", "-v", path.join(imported, "objects", "pack", `pack-${packId}.idx`)])
      .split("\n")
      .filter(line => /^[0-9a-f]{40,64} /.test(line));
    expect(entries.length).toBeGreaterThan(0);
    expect(entries.every(line => line.split(/\s+/).length === 5)).toBe(true);
  });

  it.each(["bundle", "am"])("matches ordinary offline generation when applying recovered %s output", async format => {
    fixture("complete");
    const patch = await generateGitPatch(branch, "main", { cwd: author, repoSlug, pinnedSha: tip });
    artifacts.add(patch.patchPath);
    expect(patch.success).toBe(true);
    const ordinaryPatch = fs.readFileSync(patch.patchPath);
    const bundle = await generateGitBundle(branch, "main", { cwd: author, repoSlug, pinnedSha: tip });
    artifacts.add(bundle.bundlePath);
    expect(bundle.success).toBe(true);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    await recoverObjectPack(transport, paths, options());
    expect(transport.read(paths.patchPath)).toEqual(ordinaryPatch);
    await apply(transport, paths, format);
  });

  it.each(["bundle", "am"])("preserves resolved merges using %s (filtered tree for am)", async format => {
    fixture("complete");
    git(author, ["checkout", "-qb", "side", baseline]);
    write(author, "text.txt", "side branch edit\n");
    commit(author, "side branch commit");
    git(author, ["checkout", "-q", branch]);
    expect(runGit(author, ["merge", "--no-ff", "side", "-m", "resolved merge"]).exitCode).toBe(1);
    write(author, "text.txt", "explicit agent conflict resolution\n");
    tip = commit(author, "resolved merge");
    expectedTree = git(author, ["rev-parse", "HEAD^{tree}"]);
    const parents = git(author, ["show", "-s", "--format=%P", tip]);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    await recoverObjectPack(transport, paths, options("full", { config: format === "am" ? { excluded_files: ["excluded/**"] } : {} }));
    await apply(transport, paths, format);
    expect(fs.readFileSync(path.join(destination, "text.txt"), "utf8")).toBe("explicit agent conflict resolution\n");
    if (format === "bundle") expect(git(destination, ["show", "-s", "--format=%P", "HEAD"])).toBe(parents);
  });

  it("aborts an unsupported unfiltered am merge replay without publishing partial commits", async () => {
    fixture("complete");
    git(author, ["checkout", "-qb", "side", baseline]);
    write(author, "text.txt", "conflicting side branch edit\n");
    commit(author, "side branch commit");
    git(author, ["checkout", "-q", branch]);
    expect(runGit(author, ["merge", "--no-ff", "side", "-m", "resolved merge"]).exitCode).toBe(1);
    write(author, "text.txt", "resolved merge contents\n");
    tip = commit(author, "resolved merge");
    const before = state(remote);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    await recoverObjectPack(transport, paths, options());
    await expect(apply(transport, paths, "am")).rejects.toThrow("Failed to merge");
    await transport.cleanup();
    expect(git(destination, ["rev-parse", "HEAD"])).toBe(baseline);
    expect(git(destination, ["status", "--porcelain"])).toBe("");
    expect(fs.existsSync(path.join(destination, ".git/rebase-apply"))).toBe(false);
    expect(state(remote)).toEqual(before);
  });

  it.each(["bundle", "am"])("applies exclusions to the recovered %s tree, not just its preview", async format => {
    fixture("complete");
    write(author, "ignored.tmp", "must not be published\n");
    write(author, ".github/workflows/generated.lock.yml", "jobs: {}\n");
    tip = commit(author, "excluded files");
    const index = path.join(root, "expected-index");
    const env = { GIT_INDEX_FILE: index };
    git(author, ["read-tree", tip], { env });
    git(author, ["update-index", "--force-remove", "--", ".github/workflows/generated.lock.yml"], { env });
    git(author, ["update-index", "--cacheinfo", `100644,${git(author, ["rev-parse", `${baseline}:ignored.tmp`])},ignored.tmp`], { env });
    expectedTree = git(author, ["write-tree"], { env });
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    const request = options("full", { config: { excluded_files: ["*.tmp"], exclude_lock_yml: true } });
    await recoverObjectPack(transport, paths, request);
    expect(request.message.body).toContain("gh aw compile");
    expect(transport.read(paths.patchPath).toString()).not.toContain("must not be published");
    await apply(transport, paths, format);
    expect(fs.readFileSync(path.join(destination, "ignored.tmp"), "utf8")).toBe("original excluded contents\n");
    expect(fs.existsSync(path.join(destination, ".github/workflows/generated.lock.yml"))).toBe(false);
  });

  it.each(["bundle", "am"])("retains gitlinks without fetching submodules through %s", async format => {
    fixture("complete");
    const submodule = "1".repeat(40);
    git(author, ["update-index", "--add", "--cacheinfo", `160000,${submodule},deps/submodule`]);
    git(author, ["commit", "-qm", "submodule pointer"]);
    tip = git(author, ["rev-parse", "HEAD"]);
    expectedTree = git(author, ["rev-parse", "HEAD^{tree}"]);
    const fetches = redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    await recoverObjectPack(transport, paths, options());
    await apply(transport, paths, format);
    expect(git(destination, ["ls-tree", "HEAD", "deps/submodule"])).toBe(`160000 commit ${submodule}\tdeps/submodule`);
    expect(fetches).toHaveLength(1);
    expect(fs.existsSync(path.join(destination, "deps/submodule/.git"))).toBe(false);
  });

  it.each(["full", "incremental"])("returns empty %s output without applying unreachable agent work", async mode => {
    fixture();
    const before = state(destination);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects({ pinned: baseline });
    expect(await recoverObjectPack(transport, paths, options(mode))).toBe(true);
    expect(transport.read(paths.patchPath)).toHaveLength(0);
    expect(transport.read(paths.bundlePath)).toHaveLength(0);
    expect(state(destination)).toEqual(before);
  });

  it("uses the immutable dispatch baseline rather than including preexisting feature work", async () => {
    const { checkoutSha } = fixture("complete", { dispatch: true });
    git(destination, ["fetch", "-q", "origin", branch], { online: true });
    const fetches = redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    const request = options("full", { checkoutSha });
    await recoverObjectPack(transport, paths, request);
    expect(request.message.base_commit).toBe(checkoutSha);
    expect(fetches[0].args).toContain(`${checkoutSha}:refs/heads/recovery-checkout`);
    expect(transport.read(paths.patchPath).toString()).not.toContain("preexisting feature work");
    await apply(transport, paths, "bundle", { base: checkoutSha });
  });

  it("keeps the event baseline when the trusted PR branch advances after packaging", async () => {
    fixture("complete");
    const { paths, transport } = packageObjects();
    git(author, ["checkout", "-q", "--detach", baseline]);
    write(author, "concurrent.txt", "another writer\n");
    const concurrent = commit(author, "concurrent remote update");
    git(author, ["push", "-q", `file://${remote}`, `${concurrent}:refs/heads/${branch}`], { online: true });
    const fetches = redirectRecoveryFetch();
    const request = options("incremental", { message: { base_commit: concurrent } });
    await recoverObjectPack(transport, paths, request);
    expect(fetches[0].args).toContain(`${baseline}:refs/heads/recovery-base`);
    expect(request.message.base_commit).toBe(baseline);
    await apply(transport, paths, "bundle");
    expect(fs.existsSync(path.join(destination, "concurrent.txt"))).toBe(false);
    expect(git(remote, ["rev-parse", `refs/heads/${branch}`])).toBe(concurrent);
  });

  it("exports the pinned tip even after the agent moves its named branch", async () => {
    fixture();
    git(agent, ["update-ref", `refs/heads/${branch}`, baseline]);
    const before = state(agent);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    await recoverObjectPack(transport, paths, options());
    await apply(transport, paths, "bundle");
    expect(git(destination, ["rev-parse", "HEAD"])).toBe(tip);
    expect(state(agent)).toEqual(before);
  });

  it("never copies or executes agent remotes, hooks, diff programs or conversion filters", async () => {
    fixture("complete");
    write(author, ".gitattributes", "text.txt filter=marker diff=marker\n");
    tip = commit(author, "attribute configuration");
    expectedTree = git(author, ["rev-parse", "HEAD^{tree}"]);
    const marker = path.join(root, "agent-program-ran");
    const hooks = path.join(root, "hooks");
    write(root, "hooks/post-checkout", `#!/bin/sh\ntouch "${marker}"\n`);
    fs.chmodSync(path.join(hooks, "post-checkout"), 0o755);
    for (const [key, value] of [
      ["core.hooksPath", hooks],
      ["core.fsmonitor", `touch "${marker}"`],
      ["filter.marker.clean", `touch "${marker}"`],
      ["filter.marker.smudge", `touch "${marker}"`],
      ["diff.marker.command", `touch "${marker}"`],
      ["diff.marker.textconv", `touch "${marker}"`],
      ["remote.origin.url", `ext::touch ${marker}`],
      ["protocol.ext.allow", "always"],
      ["http.https://github.com/.extraheader", "AUTHORIZATION: bearer integration-agent-token"],
    ])
      git(author, ["config", key, value]);
    redirectRecoveryFetch();
    const { paths, transport } = packageObjects();
    expect(transport.read(paths.patchPath).toString()).not.toContain("integration-agent-token");
    await recoverObjectPack(transport, paths, options());
    await apply(transport, paths, "bundle");
    expect(fs.existsSync(marker)).toBe(false);
    expect(git(destination, ["config", "--get", "remote.origin.url"])).toBe(`file://${remote}`);
    expect(runGit(destination, ["config", "--get", "filter.marker.clean"]).exitCode).toBe(1);
  });

  it("rejects a real oversized local object inventory before packing or fetching", () => {
    fixture("complete");
    git(author, ["hash-object", "-w", "--stdin"], { input: Buffer.alloc(64 * 1024 * 1024, 65) });
    const before = state(remote);
    const fetches = redirectRecoveryFetch();
    expect(() => createObjectPack(branch, repoSlug, tip, { cwd: agent })).toThrow("64 MiB");
    expect(fetches).toHaveLength(0);
    const patch = getPatchPathForBranchInRepo(branch, repoSlug);
    expect(fs.existsSync(patch)).toBe(false);
    expect(fs.existsSync(patch.replace(/\.patch$/, ".bundle"))).toBe(false);
    expect(state(remote)).toEqual(before);
  });

  it.each(["patch", "bundle"])("cleans staging when the final %s rename fails", failedRename => {
    fixture("complete");
    const patchPath = getPatchPathForBranchInRepo(branch, repoSlug);
    const bundlePath = patchPath.replace(/\.patch$/, ".bundle");
    artifacts.add(patchPath);
    artifacts.add(bundlePath);
    const directories = [];
    const mkdir = fs.mkdtempSync;
    vi.spyOn(fs, "mkdtempSync").mockImplementation((...args) => {
      const directory = mkdir(...args);
      directories.push(directory);
      return directory;
    });
    const rename = fs.renameSync;
    vi.spyOn(fs, "renameSync").mockImplementation((source, target) => {
      if (target === (failedRename === "patch" ? patchPath : bundlePath)) throw new Error("simulated rename failure");
      return rename(source, target);
    });
    const fetches = redirectRecoveryFetch();
    expect(() => createObjectPack(branch, repoSlug, tip, { cwd: agent })).toThrow("Unable to package");
    expect(fetches).toHaveLength(0);
    expect(fs.existsSync(bundlePath)).toBe(false);
    expect(fs.existsSync(patchPath)).toBe(failedRename === "bundle");
    expect(directories.length).toBeGreaterThan(0);
    for (const directory of directories) expect(fs.existsSync(directory)).toBe(false);
  });

  it("rejects application to an unrelated consumer missing bundle prerequisites", async () => {
    fixture("complete");
    destination = path.join(root, "unrelated-consumer");
    fs.mkdirSync(destination);
    git(destination, ["init", "-q", "-b", "main"]);
    identity(destination);
    write(destination, "unrelated.txt", "independent repository\n");
    commit(destination, "unrelated consumer");
    const before = state(destination);
    const { paths, transport } = packageObjects();
    const fetches = redirectRecoveryFetch();
    await recoverObjectPack(transport, paths, options());
    const { applyBundleToBranch } = require("./create_pull_request.cjs");
    await expect(applyBundleToBranch(paths.bundlePath, publishBranch, publishBranch, execApi(destination, transport))).rejects.toThrow(/prerequisites|history/i);
    expect(state(destination)).toEqual(before);
    expect(git(destination, ["for-each-ref", "--format=%(refname)", "refs/bundles/"])).toBe("");
    expect(fetches).toHaveLength(1);
  });

  it.each(["preview", "pack", "forged-preview", "captured-preview", "missing-agent-blob", "missing-baseline", "unrelated-tip"])("rejects %s without applying anything or changing the uploaded artifacts", async failure => {
    fixture("complete");
    if (failure === "missing-agent-blob") {
      const blob = git(author, ["rev-parse", `${tip}:binary.bin`]);
      fs.unlinkSync(path.join(author, ".git/objects", blob.slice(0, 2), blob.slice(2)));
    }
    if (failure === "unrelated-tip") {
      const tree = git(author, ["hash-object", "-t", "tree", "-w", "--stdin"], { input: "" });
      tip = git(author, ["commit-tree", tree, "-m", "unrelated root"]);
    }
    const { paths, transport } = packageObjects({
      beforeCapture(paths) {
        if (failure === "preview") fs.appendFileSync(paths.patchPath, "\nredacted\n");
        if (failure === "pack") {
          const bytes = fs.readFileSync(paths.bundlePath);
          bytes[bytes.length - 1] ^= 1;
          fs.writeFileSync(paths.bundlePath, bytes);
        }
        if (failure === "forged-preview") {
          const bytes = fs.readFileSync(paths.bundlePath);
          const boundary = bytes.indexOf(10, bytes.indexOf(10) + 1);
          const metadata = JSON.parse(bytes.subarray(bytes.indexOf(10) + 1, boundary));
          const preview = Buffer.from("concealed object contents\n");
          metadata.previewHash = crypto.createHash("sha256").update(preview).digest("hex");
          fs.writeFileSync(paths.patchPath, preview);
          fs.writeFileSync(paths.bundlePath, Buffer.concat([bytes.subarray(0, bytes.indexOf(10) + 1), Buffer.from(`${JSON.stringify(metadata)}\n`), bytes.subarray(boundary + 1)]));
        }
      },
    });
    if (failure === "captured-preview") fs.appendFileSync(paths.patchPath, "\nchanged after capture\n");
    const before = state(destination);
    const uploaded = fs.readFileSync(paths.bundlePath);
    const fetches = redirectRecoveryFetch();
    const applyRecovered = vi.fn(() => apply(transport, paths, "bundle"));
    await expect(async () => {
      await recoverObjectPack(transport, paths, options(failure === "missing-baseline" ? "incremental" : "full", failure === "missing-baseline" ? { baseline: "a".repeat(40) } : {}));
      await applyRecovered();
    }).rejects.toThrow();
    expect(applyRecovered).not.toHaveBeenCalled();
    expect(fetches).toHaveLength(["preview", "pack", "forged-preview", "captured-preview"].includes(failure) ? 0 : 1);
    expect(state(destination)).toEqual(before);
    expect(fs.readFileSync(paths.bundlePath)).toEqual(uploaded);
  });
});
