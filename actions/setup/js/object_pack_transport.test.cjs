import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import childProcess from "child_process";
import crypto from "crypto";
import fs from "fs";
import path from "path";
import { createRequire } from "module";
import zlib from "zlib";
import { createObjectPack, recoverObjectPack, canRecoverPatch, isObjectPack, recoveryToken } from "./object_pack_transport.cjs";
import { createTransportIntegrity } from "./transport_integrity.cjs";
import { generateGitPatch } from "./generate_git_patch.cjs";
import { createHandlers } from "./safe_outputs_handlers.cjs";
import { useExecutionProvenanceFixture } from "./test_execution_provenance.js";

useExecutionProvenanceFixture();
const require = createRequire(import.meta.url);
const { withClaimExecution } = require("./work_queue_claim_scope.cjs");

describe("offline object-pack recovery", () => {
  let root, repo, base, tip, originalEnv, originalCore, originalGlobals;
  const artifacts = [];
  const transports = [];
  const artifactDirs = [];
  const repoSlug = "test-owner/recovery";
  function git(args, cwd = repo) {
    return childProcess.execFileSync("git", args, { cwd, encoding: "utf8", stdio: "pipe" }).trim();
  }
  function packageObjects() {
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: repo });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    const transport = createTransportIntegrity();
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    transports.push(transport);
    return { artifact, transport, paths: { patchPath: artifact.patchPath, bundlePath: artifact.bundlePath } };
  }
  function redirectTrustedFetch() {
    const original = childProcess.spawnSync;
    const fetches = [];
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (command === "git" && args.includes("fetch")) {
        fetches.push({ args, options });
        const redirected = args.map(arg => (arg === `https://github.com/${repoSlug}.git` ? repo : arg));
        return original(command, redirected, { ...options, env: { ...options.env, GIT_ALLOW_PROTOCOL: "file" } });
      }
      return original(command, args, options);
    });
    return fetches;
  }
  beforeEach(() => {
    originalEnv = { ...process.env };
    originalCore = global.core;
    originalGlobals = { context: global.context, github: global.github, exec: global.exec };
    global.core = { debug: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() };
    root = path.resolve(`.object-pack-${crypto.randomUUID()}`);
    repo = path.join(root, "source");
    fs.mkdirSync(repo, { recursive: true });
    process.env.GIT_CEILING_DIRECTORIES = root;
    process.env.GITHUB_WORKSPACE = repo;
    process.env.GITHUB_REPOSITORY = repoSlug;
    delete process.env.GITHUB_SHA;
    git(["init", "--initial-branch=main", "--object-format=sha1"]);
    git(["config", "user.name", "Test User"]);
    git(["config", "user.email", "test@example.com"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "base\n");
    git(["add", "."]);
    git(["commit", "-m", "base"]);
    base = git(["rev-parse", "HEAD"]);
    git(["checkout", "-b", "agent-change"]);
    fs.writeFileSync(path.join(repo, "file.txt"), "agent content\n");
    fs.writeFileSync(path.join(repo, "binary.dat"), Buffer.from([0, 255, 17]));
    fs.writeFileSync(path.join(repo, "executable.txt"), "preserve executable mode\n", { mode: 0o755 });
    fs.symlinkSync("file.txt", path.join(repo, "file-link"));
    git(["add", "."]);
    git(["commit", "-m", "agent change"]);
    tip = git(["rev-parse", "HEAD"]);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    for (const transport of transports.splice(0)) transport.dispose();
    for (const artifact of artifacts.splice(0)) if (fs.existsSync(artifact)) fs.unlinkSync(artifact);
    for (const directory of artifactDirs.splice(0)) fs.rmdirSync(directory);
    fs.rmSync(root, { recursive: true, force: true });
    process.env = originalEnv;
    global.core = originalCore;
    Object.assign(global, originalGlobals);
  });

  it("exports full local objects without invoking transports or repository programs", () => {
    const marker = path.join(root, "helper-ran");
    git(["remote", "add", "origin", `ext::touch ${marker}`]);
    git(["config", "protocol.ext.allow", "always"]);
    git(["config", "core.fsmonitor", `touch ${marker}`]);
    const original = childProcess.spawnSync;
    const calls = vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      expect(args).not.toContain("fetch");
      expect(options.env.GIT_ALLOW_PROTOCOL).toBe("");
      return original(command, args, options);
    });
    const { artifact } = packageObjects();
    expect(isObjectPack(fs.readFileSync(artifact.bundlePath))).toBe(true);
    expect(fs.readFileSync(artifact.patchPath, "utf8")).toContain("agent content");
    expect(fs.existsSync(marker)).toBe(false);
    expect(calls).toHaveBeenCalled();
  });

  it.each(["full", "incremental"])("reconstructs %s output and retains binary files", async mode => {
    const { transport, paths } = packageObjects();
    const fetches = redirectTrustedFetch();
    const message = {};
    expect(await recoverObjectPack(transport, paths, { repo: repoSlug, branch: "agent-change", baseBranch: "main", mode, baseline: base, token: "synthetic-token", config: {}, message })).toBe(true);
    expect(fetches).toHaveLength(1);
    expect(fetches[0].args).toContain(`https://github.com/${repoSlug}.git`);
    expect(fetches[0].args).not.toContain("origin");
    expect(fetches[0].options.env.GIT_ALLOW_PROTOCOL).toBe("https");
    expect(fetches[0].options.env.GIT_CONFIG_GLOBAL).toBe("/dev/null");
    expect(message.base_commit).toBe(base);
    expect(transport.read(paths.patchPath).toString()).toContain("agent content");
    expect(isObjectPack(transport.read(paths.bundlePath))).toBe(false);
    transport.assertUnchanged();
    const recovered = path.join(root, "recovered.bundle");
    fs.writeFileSync(recovered, transport.read(paths.bundlePath));
    git(["bundle", "verify", recovered]);
    expect(git(["bundle", "list-heads", recovered])).toBe(`${tip} refs/heads/agent-change`);
    git(["fetch", recovered, "refs/heads/agent-change:refs/heads/recovered-output"]);
    expect(git(["show", "recovered-output:binary.dat"])).toBe(Buffer.from([0, 255, 17]).toString("utf8").trim());
    expect(git(["ls-tree", "recovered-output", "executable.txt"])).toMatch(/^100755 /);
    expect(git(["ls-tree", "recovered-output", "file-link"])).toMatch(/^120000 /);
    expect(git(["show", "recovered-output:file-link"])).toBe("file.txt");
  });

  it("recovers a blob missing from a partial agent checkout", async () => {
    const remote = path.join(root, "trusted");
    git(["clone", "--bare", repo, remote]);
    const blob = git(["rev-parse", `${base}:file.txt`]);
    fs.unlinkSync(path.join(repo, ".git", "objects", blob.slice(0, 2), blob.slice(2)));
    git(["config", "remote.origin.promisor", "true"]);
    git(["config", "remote.origin.url", "ext::false"]);
    const failed = await generateGitPatch("agent-change", "main", { cwd: repo, pinnedSha: tip });
    expect(failed.success).toBe(false);
    const { transport, paths } = packageObjects();
    const original = childProcess.spawnSync;
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (command === "git" && args.includes("fetch")) {
        return original(
          command,
          args.map(arg => (arg === `https://github.com/${repoSlug}.git` ? remote : arg)),
          { ...options, env: { ...options.env, GIT_ALLOW_PROTOCOL: "file" } }
        );
      }
      return original(command, args, options);
    });

    expect(await recoverObjectPack(transport, paths, { repo: repoSlug, baseBranch: "main", mode: "full", config: {}, message: {} })).toBe(true);
    expect(transport.read(paths.patchPath).toString()).toContain("agent content");
  });

  it("preserves Claim-scoped artifact paths during isolated regeneration", async () => {
    const assignment = {
      version: 3,
      dispatch_id: crypto.randomUUID(),
      request_id: "r",
      commit_id: "q",
      policy_epoch: "p",
      pool: "default",
      worker_profile: "default",
      claims: [{ handle: "h1", claim_id: "c1", work_id: "w1", work: {}, result_refs: [] }],
    };
    await withClaimExecution({ assignment, claim_handle: "h1" }, async () => {
      const { transport, paths } = packageObjects();
      expect(paths.patchPath).toContain("/claims/");
      artifactDirs.push(path.dirname(paths.patchPath));
      redirectTrustedFetch();
      await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", config: {}, message: {} });
      expect(paths.patchPath).toContain("/claims/");
      expect(transport.read(paths.patchPath).toString()).toContain("agent content");
      expect(fs.readdirSync(path.dirname(paths.patchPath)).sort()).toEqual([path.basename(paths.bundlePath), path.basename(paths.patchPath)].sort());
      transport.assertUnchanged();
    });
  });

  it("preserves the trusted non-default dispatch baseline instead of widening to main", async () => {
    const checkoutSha = tip;
    fs.writeFileSync(path.join(repo, "file.txt"), "only the new agent change\n");
    git(["add", "."]);
    git(["commit", "-m", "post-dispatch change"]);
    tip = git(["rev-parse", "HEAD"]);
    const { transport, paths } = packageObjects();
    const fetches = redirectTrustedFetch();
    const message = {};
    await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", checkoutSha, config: {}, message });
    expect(fetches[0].args).toContain(`${checkoutSha}:refs/heads/recovery-checkout`);
    expect(message.base_commit).toBe(checkoutSha);
    const patch = transport.read(paths.patchPath).toString();
    expect(patch).toContain("-agent content");
    expect(patch).not.toContain("diff --git a/binary.dat");
  });

  it("retains the exact incremental baseline when the remote head has advanced", async () => {
    const { transport, paths } = packageObjects();
    fs.writeFileSync(path.join(repo, "later.txt"), "another writer\n");
    git(["add", "."]);
    git(["commit", "-m", "concurrent remote change"]);
    const fetches = redirectTrustedFetch();
    const message = { base_commit: git(["rev-parse", "HEAD"]) };
    await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "incremental", baseline: base, config: {}, message });
    expect(fetches[0].args).toContain(`${base}:refs/heads/recovery-base`);
    expect(message.base_commit).toBe(base);
    expect(transport.read(paths.patchPath).toString()).not.toContain("later.txt");
  });
  it("rejects redacted previews before network access", async () => {
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: repo });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    fs.writeFileSync(artifact.patchPath, fs.readFileSync(artifact.patchPath, "utf8").replace("agent content", "***"));
    const transport = createTransportIntegrity();
    transports.push(transport);
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, artifact, { repo: repoSlug, mode: "full", baseBranch: "main" })).rejects.toThrow("preview changed");
    expect(fetches).toHaveLength(0);
  });

  it("requires a trusted immutable baseline, not a manifest-provided replacement", async () => {
    const { transport, paths } = packageObjects();
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, paths, { repo: repoSlug, mode: "incremental", config: {}, message: { base_commit: base } })).rejects.toThrow("trusted pre-agent");
    expect(fetches).toHaveLength(0);
  });

  it("does not recover ordinary bundle artifacts", async () => {
    expect(await recoverObjectPack({ read: () => Buffer.from("ordinary bundle") }, { bundlePath: "existing" }, {})).toBe(false);
  });

  it("applies exclusions and recomputes policy inputs after reconstruction", async () => {
    fs.mkdirSync(path.join(repo, ".github", "workflows"), { recursive: true });
    fs.writeFileSync(path.join(repo, ".github", "workflows", "generated.lock.yml"), "jobs: {}\n");
    git(["add", "."]);
    git(["commit", "-m", "generated workflow"]);
    tip = git(["rev-parse", "HEAD"]);
    const { transport, paths } = packageObjects();
    redirectTrustedFetch();
    const message = { base_commit: "a".repeat(40), diff_size: 0 };
    await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", config: { exclude_lock_yml: true }, message });
    const patch = transport.read(paths.patchPath).toString();
    expect(patch).toContain("diff --git a/file.txt b/file.txt");
    expect(patch).not.toContain("diff --git a/.github/workflows/generated.lock.yml");
    expect(message.body).toContain("gh aw compile");
    expect(message.base_commit).toBe(base);
    expect(message.diff_size).toBeGreaterThan(0);
  });

  it("does not silently replace a missing agent-created blob with remote content", async () => {
    const blob = git(["rev-parse", `${tip}:binary.dat`]);
    fs.unlinkSync(path.join(repo, ".git", "objects", blob.slice(0, 2), blob.slice(2)));
    const { transport, paths } = packageObjects();
    redirectTrustedFetch();
    await expect(recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", config: {}, message: {} })).rejects.toThrow("no changes were published");
  });

  it("rejects corrupt pack content before fetching", async () => {
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: repo });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    const envelope = fs.readFileSync(artifact.bundlePath);
    envelope[envelope.length - 1] ^= 1;
    fs.writeFileSync(artifact.bundlePath, envelope);
    const transport = createTransportIntegrity();
    transports.push(transport);
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, artifact, { repo: repoSlug, mode: "full", baseBranch: "main" })).rejects.toThrow("bytes");
    expect(fetches).toHaveLength(0);
  });

  it("enforces the exact 64 MiB uncompressed inventory limit before packing", () => {
    const original = childProcess.spawnSync;
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (args.includes("--batch-all-objects")) return { status: 0, stdout: `${tip} 67108865\n`, stderr: "" };
      return original(command, args, options);
    });
    expect(() => createObjectPack("agent-change", repoSlug, tip, { cwd: repo })).toThrow("64 MiB");
  });

  it("does not trust a forged preview hash to hide packed objects", async () => {
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: repo });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    const envelope = fs.readFileSync(artifact.bundlePath);
    const firstLine = envelope.indexOf(10);
    const secondLine = envelope.indexOf(10, firstLine + 1);
    const metadata = JSON.parse(envelope.subarray(firstLine + 1, secondLine));
    const preview = Buffer.from("concealed objects");
    metadata.previewHash = crypto.createHash("sha256").update(preview).digest("hex");
    fs.writeFileSync(artifact.bundlePath, Buffer.concat([envelope.subarray(0, firstLine + 1), Buffer.from(`${JSON.stringify(metadata)}\n`), envelope.subarray(secondLine + 1)]));
    fs.writeFileSync(artifact.patchPath, preview);
    const transport = createTransportIntegrity();
    transports.push(transport);
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, artifact, { repo: repoSlug, mode: "full", baseBranch: "main" })).rejects.toThrow("does not match");
    expect(fetches).toHaveLength(0);
  });

  it.each(["delta", "object-count", "uncompressed-limit", "inflate-overrun", "trailing-data"])("rejects a checksummed %s pack before fetching", async malformed => {
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: repo });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    const envelope = fs.readFileSync(artifact.bundlePath);
    const firstLine = envelope.indexOf(10);
    const boundary = envelope.indexOf(10, firstLine + 1);
    const metadata = JSON.parse(envelope.subarray(firstLine + 1, boundary));
    let raw = Buffer.from(envelope.subarray(boundary + 1, envelope.length - 20));
    if (malformed === "delta") raw[12] = (raw[12] & 0x8f) | (6 << 4);
    if (malformed === "object-count") raw.writeUInt32BE(100001, 8);
    if (malformed === "uncompressed-limit") raw.set([0xbf, 0xff, 0xff, 0xff, 0x01], 12);
    if (malformed === "inflate-overrun") {
      raw.writeUInt32BE(1, 8);
      raw = Buffer.concat([raw.subarray(0, 12), Buffer.from([0x31]), zlib.deflateSync(Buffer.alloc(1024, 65))]);
    }
    if (malformed === "trailing-data") raw = Buffer.concat([raw, Buffer.from([1])]);
    const pack = Buffer.concat([raw, crypto.createHash("sha1").update(raw).digest()]);
    metadata.packHash = crypto.createHash("sha256").update(pack).digest("hex");
    fs.writeFileSync(artifact.bundlePath, Buffer.concat([envelope.subarray(0, firstLine + 1), Buffer.from(`${JSON.stringify(metadata)}\n`), pack]));
    const transport = createTransportIntegrity();
    transports.push(transport);
    transport.captureOptional(artifact.patchPath);
    transport.captureOptional(artifact.bundlePath);
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, artifact, { repo: repoSlug, mode: "full", baseBranch: "main" })).rejects.toThrow();
    expect(fetches).toHaveLength(0);
  });

  it("uses the publisher client's resolved credential, never an agent token", async () => {
    process.env.GITHUB_TOKEN = "different-default";
    const client = { auth: vi.fn().mockResolvedValue({ token: "scoped-publisher" }) };
    expect(await recoveryToken(client, "per-handler")).toBe("per-handler");
    expect(client.auth).not.toHaveBeenCalled();
    expect(await recoveryToken(client)).toBe("scoped-publisher");
  });

  it("rejects staged recovery before any fetch", async () => {
    const { transport, paths } = packageObjects();
    const fetches = redirectTrustedFetch();
    await expect(recoverObjectPack(transport, paths, { staged: true })).rejects.toThrow("non-staged");
    expect(fetches).toHaveLength(0);
  });

  it("queues a create-PR request when the default base ref is absent", async () => {
    git(["branch", "-D", "main"]);
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, { create_pull_request: { base_branch: "main" } });
    const result = await handlers.createPullRequestHandler({ branch: "agent-change", title: "Preserve checkout recovery", body: "Restore validated output generation for incomplete checkouts." });
    expect(result.isError, result.content[0].text).toBeUndefined();
    expect(JSON.parse(result.content[0].text).result).toBe("queued");
    expect(append).toHaveBeenCalledTimes(1);
    const artifact = packageObjects().artifact;
    expect(isObjectPack(fs.readFileSync(artifact.bundlePath))).toBe(true);
  });

  it("queues a triggering-PR update without fetching when its local tracking ref is missing", async () => {
    global.context = {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: "recovery" },
      payload: { pull_request: { number: 7, head: { sha: base, repo: { full_name: repoSlug } }, base: { ref: "main", repo: { full_name: repoSlug } } } },
    };
    const append = vi.fn();
    const handlers = createHandlers({ debug: vi.fn() }, append, { push_to_pull_request_branch: { target: "triggering" } });
    const result = await handlers.pushToPullRequestBranchHandler({ branch: "agent-change" });
    expect(result.isError, result.content[0].text).toBeUndefined();
    expect(JSON.parse(result.content[0].text)).toEqual(expect.objectContaining({ result: "queued", recovery: "object-pack" }));
    expect(append).toHaveBeenCalledTimes(1);
    packageObjects();
  });

  it.each([
    ["create_pull_request", "bundle"],
    ["create_pull_request", "am"],
    ["push_to_pull_request_branch", "bundle"],
    ["push_to_pull_request_branch", "am"],
  ])("%s (%s) enforces file policy on reconstructed content before publication", async (handlerName, patchFormat) => {
    packageObjects();
    redirectTrustedFetch();
    process.env.GH_AW_PROMPTS_DIR = path.resolve("..", "md");
    const pr = {
      number: 7,
      state: "open",
      title: "Preserve checkout recovery",
      labels: [],
      head: { ref: "agent-change", sha: base, repo: { full_name: repoSlug, owner: { login: "test-owner" } } },
      base: { ref: "main", sha: base, repo: { full_name: repoSlug, owner: { login: "test-owner" } } },
    };
    global.context = { eventName: "pull_request", repo: { owner: "test-owner", repo: "recovery" }, payload: { pull_request: pr, repository: { html_url: `https://github.com/${repoSlug}` } } };
    global.github = {
      auth: vi.fn().mockResolvedValue({ token: "scoped-token" }),
      rest: {
        pulls: { get: vi.fn().mockResolvedValue({ data: pr }), list: vi.fn().mockResolvedValue({ data: [] }), create: vi.fn() },
        repos: { get: vi.fn().mockResolvedValue({ data: { default_branch: "main" } }) },
        git: { createRef: vi.fn(), deleteRef: vi.fn() },
        issues: { create: vi.fn() },
      },
      graphql: vi.fn(),
    };
    global.exec = { exec: vi.fn(), getExecOutput: vi.fn() };
    const { main } = require(`./${handlerName}.cjs`);
    const handler = await main({ base_branch: "main", target: "triggering", allowed_files: ["allowed.txt"], signed_commits: true, patch_format: patchFormat });
    const result = await handler({ branch: "agent-change", title: "Preserve checkout recovery", body: "Restore validated output generation." }, {});
    expect(result.success).toBe(false);
    expect(result.error).toMatch(/outside.*allowed-files|file protection/i);
    expect(global.github.rest.pulls.create).not.toHaveBeenCalled();
    expect(global.github.rest.git.createRef).not.toHaveBeenCalled();
    expect(global.exec.exec).not.toHaveBeenCalled();
    expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining(`Apply transport mode: ${patchFormat === "am" ? "patch" : "bundle"}`));
  });

  it("preserves no-change handling when the recovered exact baseline equals the tip", async () => {
    const { transport, paths } = packageObjects();
    redirectTrustedFetch();
    await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "incremental", baseline: tip, config: {}, message: {} });
    expect(transport.read(paths.patchPath)).toHaveLength(0);
    expect(transport.read(paths.bundlePath)).toHaveLength(0);
    expect(global.core.debug).toHaveBeenCalledWith("[object_pack_transport] recovery-empty");
    transport.assertUnchanged();
  });

  it("resolves workspacePath before cwd without widening outside the workspace", () => {
    process.env.GITHUB_WORKSPACE = root;
    const artifact = createObjectPack("agent-change", repoSlug, tip, { cwd: root, workspacePath: "source" });
    artifacts.push(artifact.patchPath, artifact.bundlePath);
    expect(fs.readFileSync(artifact.patchPath, "utf8")).toContain("agent content");
    expect(() => createObjectPack("agent-change", repoSlug, tip, { workspacePath: "../outside" })).toThrow("stay under");
  });

  it("traces successful resolutions without identities, content, or credentials", async () => {
    const { transport, paths } = packageObjects();
    redirectTrustedFetch();
    await recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", token: "private-publisher-credential", config: {}, message: {} });
    const logs = global.core.debug.mock.calls.map(([message]) => message).join("\n");
    for (const stage of ["inventory-ready", "preview-verified", "fetch-ready", "import-ready", "baseline-resolved", "regeneration-ready", "recovery-ready"]) {
      expect(logs).toContain(`[object_pack_transport] ${stage}`);
    }
    for (const sensitive of [repoSlug, repo, root, base, tip, paths.patchPath, "agent content", "agent-change", "private-publisher-credential"]) expect(logs).not.toContain(sensitive);
    expect(logs).toContain("incremental=false dispatch=false");
    expect(global.core.error).not.toHaveBeenCalled();
  });

  it("traces failed resolution stages without raw subprocess diagnostics", async () => {
    const { transport, paths } = packageObjects();
    const original = childProcess.spawnSync;
    vi.spyOn(childProcess, "spawnSync").mockImplementation((command, args, options) => {
      if (command === "git" && args.includes("fetch")) return { status: 128, stdout: "", stderr: `private-marker ${repoSlug} ${repo} ${tip}` };
      return original(command, args, options);
    });
    await expect(recoverObjectPack(transport, paths, { repo: repoSlug, mode: "full", baseBranch: "main", token: "private-marker", config: {}, message: {} })).rejects.toThrow("no changes were published");
    const logs = [...global.core.debug.mock.calls, ...global.core.error.mock.calls].map(([message]) => message).join("\n");
    expect(logs).toContain("fetch-start");
    expect(logs).toContain("recovery-rejected");
    expect(logs).not.toContain("fetch-ready");
    for (const sensitive of ["private-marker", repoSlug, repo, tip, base]) expect(logs).not.toContain(sensitive);
  });

  it.each(["missing-baseline", "closed", "wrong-head", "wrong-event-head", "wrong-target"])("rejects %s PR recovery before any fetch or publication", async failure => {
    packageObjects();
    const fetches = redirectTrustedFetch();
    const eventPR = { number: 7, head: { sha: base, repo: { full_name: repoSlug } }, base: { repo: { full_name: repoSlug } } };
    const livePR = { state: "open", head: { ref: "agent-change", repo: { full_name: repoSlug } } };
    if (failure === "missing-baseline") delete eventPR.head.sha;
    if (failure === "closed") livePR.state = "closed";
    if (failure === "wrong-head") livePR.head.repo.full_name = "unapproved/head";
    if (failure === "wrong-event-head") eventPR.head.repo.full_name = "unapproved/head";
    global.context = { eventName: "pull_request", repo: { owner: "test-owner", repo: "recovery" }, payload: { pull_request: eventPR } };
    global.github = { rest: { pulls: { get: vi.fn().mockResolvedValue({ data: livePR }) } } };
    global.exec = { exec: vi.fn(), getExecOutput: vi.fn() };
    const { main } = require("./push_to_pull_request_branch.cjs");
    const handler = await main({ target: failure === "wrong-target" ? 8 : "triggering" });
    await expect(handler({ branch: "agent-change", base_commit: base }, {})).rejects.toThrow(/trusted.*baseline|not authorized/);
    expect(fetches).toHaveLength(0);
    expect(global.exec.exec).not.toHaveBeenCalled();
  });

  it("never treats no-changes or workflow-only exclusions as recovery requests", () => {
    for (const errorCode of ["no_changes", "no_new_commits", "workflow_exclusions"]) expect(canRecoverPatch({ success: false, errorCode })).toBe(false);
    expect(canRecoverPatch({ success: false, errorCode: "missing_base" })).toBe(true);
  });
});
