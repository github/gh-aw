import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import zlib from "node:zlib";
import { execFileSync } from "node:child_process";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
const { prepareAdapterContext } = require("./work_queue_prepare_claim_adapter.cjs");
const { createClaimAdapterHandler } = require("./work_queue_claim_adapters.cjs");
const { assertGitPushAuthorized } = require("./work_queue_git_effects.cjs");
const bundle = require("./generate_git_bundle.cjs");
const patch = require("./git_patch_utils.cjs");
const scanning = require("./work_queue_code_scanning.cjs");
const coverage = require("./work_queue_code_coverage.cjs");
const assets = require("./work_queue_upload_assets.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");
const { main: prepareScript } = require("./work_queue_prepare_claim_script.cjs");
const { pushSignedCommits } = require("./push_signed_commits.cjs");

const environmentKeys = [
  "GH_AW_WORK_QUEUE_ENABLED",
  "GH_AW_WORK_QUEUE_ROLE",
  "GH_AW_WORK_QUEUE_SNAPSHOT",
  "GH_AW_SAFE_OUTPUTS_STAGED",
  "GITHUB_SHA",
  "GITHUB_REPOSITORY",
  "GITHUB_SERVER_URL",
  "GITHUB_API_URL",
  "GITHUB_WORKSPACE",
  "GH_AW_ASSETS_BRANCH",
  "RUNNER_TEMP",
  "GH_AW_CLAIM_INPUT",
  "GH_AW_CLAIM_OUTPUT",
  "GH_AW_CLAIM_SCRIPT_FILENAME",
];
const globalKeys = ["core", "github", "exec", "context", "getOctokit"];
const roots = [];
let environment;
let gitConfigEnvironment;
let globals;
let restoreAssetDescriptors;

function simulateLinuxAssetDescriptors() {
  if (process.platform !== "darwin") return;
  // Claim asset parity uses real file I/O with bounded Linux descriptor-path
  // simulation on macOS; production access remains Linux-only.
  const originals = { openSync: fs.openSync, lstatSync: fs.lstatSync, mkdirSync: fs.mkdirSync };
  const pinned = new Map();
  const resolve = filename => {
    if (typeof filename !== "string") return filename;
    const match = /^\/proc\/self\/fd\/(\d+)(?:\/(.*))?$/.exec(filename);
    return match ? path.join(pinned.get(Number(match[1])), match[2] || "") : filename;
  };
  fs.openSync = (filename, ...args) => {
    const resolved = resolve(filename);
    const fd = originals.openSync(resolved, ...args);
    pinned.set(fd, resolved);
    return fd;
  };
  fs.lstatSync = (filename, ...args) => originals.lstatSync(resolve(filename), ...args);
  fs.mkdirSync = (filename, ...args) => originals.mkdirSync(resolve(filename), ...args);
  restoreAssetDescriptors = () => Object.assign(fs, originals);
  vi.stubGlobal("process", new Proxy(process, { get: (target, key) => (key === "platform" ? "linux" : Reflect.get(target, key)) }));
}

function directory() {
  const root = path.resolve(".queue-git-audit-scratch", crypto.randomUUID());
  fs.mkdirSync(root, { recursive: true });
  roots.push(root);
  return root;
}

function assignment(count = 2) {
  return {
    version: 3,
    dispatch_id: "original-dispatch",
    request_id: "original-request",
    commit_id: "original-commit",
    policy_epoch: "original-policy",
    pool: "original-pool",
    worker_profile: "original-profile",
    claims: Array.from({ length: count }, (_, index) => ({
      handle: `h${index + 1}`,
      claim_id: `claim:${index + 1}`,
      work_id: `work:${index + 1}`,
      work: { checkout: `checkout-${index + 1}`, input: `input-${index + 1}` },
      result_refs: [],
    })),
  };
}

const adapter = { mode: "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { item_number: "number", body: "content" } };
const allow = async request => ({ claim_handle: request.claim_handle, authorized: true });
const digest = value => crypto.createHash("sha256").update(value).digest("hex");

beforeEach(() => {
  environment = Object.fromEntries(environmentKeys.map(key => [key, process.env[key]]));
  // createHandlers injects safe.directory into process.env, not only child env.
  gitConfigEnvironment = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.startsWith("GIT_CONFIG_")));
  globals = Object.fromEntries(globalKeys.map(key => [key, Object.getOwnPropertyDescriptor(global, key)]));
  environmentKeys.forEach(key => delete process.env[key]);
  process.env.GITHUB_SHA = "a".repeat(40);
  process.env.GITHUB_REPOSITORY = "owner/repo";
  global.core = { info() {}, warning() {}, error() {}, debug() {}, setOutput() {}, exportVariable() {} };
});

afterEach(() => {
  vi.restoreAllMocks();
  restoreAssetDescriptors?.();
  restoreAssetDescriptors = undefined;
  vi.unstubAllGlobals();
  Object.keys(process.env)
    .filter(key => key.startsWith("GIT_CONFIG_"))
    .forEach(key => delete process.env[key]);
  Object.assign(process.env, gitConfigEnvironment);
  for (const [key, value] of Object.entries(environment)) value === undefined ? delete process.env[key] : (process.env[key] = value);
  for (const [key, descriptor] of Object.entries(globals)) descriptor === undefined ? delete global[key] : Object.defineProperty(global, key, descriptor);
  roots.splice(0).forEach(root => fs.rmSync(root, { recursive: true, force: true }));
});

describe("disabled queue git parity at legacy boundaries", () => {
  it.each([undefined, "false"])("retains baseline filenames and does not inspect git, credentials or clients when enabled=%s", async enabled => {
    if (enabled !== undefined) process.env.GH_AW_WORK_QUEUE_ENABLED = enabled;
    process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(directory(), "missing-snapshot");
    expect(bundle.getBundlePathForBranch("feature/test")).toBe("/tmp/gh-aw/aw-feature-test.bundle");
    expect(bundle.getBundlePathForBranchInRepo("feature/test", "other/repository")).toBe("/tmp/gh-aw/aw-other-repository-feature-test.bundle");
    expect(patch.getPatchPathForBranch("feature/test")).toBe("/tmp/gh-aw/aw-feature-test.patch");
    expect(patch.getPatchPathForBranchInRepo("feature/test", "other/repository")).toBe("/tmp/gh-aw/aw-other-repository-feature-test.patch");
    const forbidden = new Proxy(
      {},
      {
        get: () => {
          throw new Error("disabled queue must not inspect any push options");
        },
      }
    );
    await expect(assertGitPushAuthorized(forbidden)).resolves.toBeUndefined();
  });

  it("fails closed on an enabled missing snapshot rather than falling back to ordinary git", async () => {
    process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
    process.env.GH_AW_WORK_QUEUE_SNAPSHOT = path.join(directory(), "missing-snapshot");
    const execute = vi.fn();
    await expect(assertGitPushAuthorized({ branch: "work", execGitSync: execute })).rejects.toThrow(/snapshot is missing/);
    expect(execute).not.toHaveBeenCalled();
  });
});

describe.each([1, 2])("original %i-Claim artifact and preparation routing", count => {
  it("binds each work, checkout and artifact namespace without shrinking the assignment", async () => {
    const original = assignment(count);
    const root = directory();
    const inputs = original.claims.map((member, index) => ({ type: "custom", ...(count > 1 ? { claim_handle: member.handle } : {}), number: index + 41, content: member.work.input }));
    const observed = [];
    const filenames = [];
    for (const [index, member] of original.claims.entries()) {
      const authorize = async request => {
        expect(request.assignment).toEqual(original);
        expect(request.assignment.claims[index].work.checkout).toBe(member.work.checkout);
        expect(request.claim_handle).toBe(member.handle);
        return allow(request);
      };
      const prepared = await prepareAdapterContext({ assignment: original, adapter, index, type: "custom", messages: inputs, authorize, artifactRoot: root });
      expect(prepared.messages).toEqual([{ ...inputs[index], claim_handle: member.handle }]);
      expect(JSON.parse(fs.readFileSync(prepared.assignmentFile, "utf8"))).toEqual(original);
      fs.writeFileSync(
        prepared.outputFile,
        JSON.stringify({ version: 3, claim_handle: member.handle, type: "custom", messages: [{ input: prepared.messages[0], payload: { number: inputs[index].number, content: inputs[index].content } }] })
      );
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize }, async () => {
        const loadEffectHandler = vi.fn(async () => async message => {
          observed.push(message);
          return { success: true };
        });
        const handler = await createClaimAdapterHandler({ adapter, filename: prepared.outputFile, loadEffectHandler });
        expect(loadEffectHandler).not.toHaveBeenCalled();
        expect(await handler(inputs[index])).toEqual({ success: true });
        expect(loadEffectHandler).toHaveBeenCalledTimes(1);
        filenames.push(bundle.getBundlePathForBranch("same-branch"), patch.getPatchPathForBranch("same-branch"));
      });
    }
    expect(observed).toEqual(inputs.map((input, index) => ({ type: "update_issue", claim_handle: original.claims[index].handle, repo: "owner/repo", item_number: input.number, body: input.content })));
    expect(new Set(filenames).size).toBe(count * 2);
  });
});

describe("prepared adapter rejection ordering", () => {
  it("does not authorize or prepare empty, ambiguous or foreign-only input", async () => {
    const original = assignment();
    const root = directory();
    const authorize = vi.fn(async () => {
      throw new Error("empty preparation cannot access credentials");
    });
    for (const messages of [[], [{ type: "custom", number: 42 }], [{ type: "custom", claim_handle: "foreign" }], [{ type: "other", claim_handle: "h1" }]]) {
      expect(await prepareAdapterContext({ assignment: original, adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root })).toEqual({ active: false, claim_handle: "h1" });
    }
    expect(authorize).not.toHaveBeenCalled();
    expect(fs.readdirSync(root)).toEqual([]);
  });

  it("rejects missing/foreign selectors, contradictory identities, missing artifacts and terminal Claims before loading effects", async () => {
    const original = assignment();
    const loadEffectHandler = vi.fn(async () => {
      throw new Error("invalid input cannot acquire effect credentials");
    });
    const authorize = vi.fn(allow);
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize }, async () => {
      const handler = await createClaimAdapterHandler({ adapter, filename: path.join(directory(), "missing.json"), loadEffectHandler });
      for (const message of [{ type: "custom" }, { type: "custom", claim_handle: "foreign" }, { type: "custom", claim_handle: "h2" }, { type: "custom", claim_handle: "h1", work_id: "work:2" }])
        await expect(handler(message)).rejects.toThrow(/claim_handle|cannot escape|work_id/);
      expect(authorize).not.toHaveBeenCalled();
      await expect(handler({ type: "custom", claim_handle: "h1" })).rejects.toThrow();
    });
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize: async request => ({ claim_handle: request.claim_handle, authorized: false, state: "result", suppressed: true }) }, async () => {
      const handler = await createClaimAdapterHandler({ adapter, filename: "unreadable", loadEffectHandler });
      await expect(handler({ type: "custom", claim_handle: "h1" })).rejects.toThrow(/already settled/);
    });
    expect(loadEffectHandler).not.toHaveBeenCalled();
  });

  it("keeps a cancelled sibling inactive while the valid sibling consumes only its own prepared payload", async () => {
    const original = assignment();
    const root = directory();
    const messages = original.claims.map(member => ({ type: "custom", claim_handle: member.handle, number: 42, content: member.work.input }));
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.claim_handle === "h2", ...(request.claim_handle === "h1" ? { state: "cancelled", suppressed: true } : {}) });
    await expect(prepareAdapterContext({ assignment: original, adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root })).rejects.toThrow(/cancelled/);
    const prepared = await prepareAdapterContext({ assignment: original, adapter, index: 1, type: "custom", messages, authorize, artifactRoot: root });
    expect(prepared.messages).toEqual([messages[1]]);
    expect(fs.readdirSync(path.join(root, "claims"))).toEqual([path.basename(prepared.directory)]);
  });
});

describe("git authorization before credentialed repository readback", () => {
  it.each(["open", "cancelled", "result"])("rejects a %s Claim without accessing its repository client", async state => {
    const repositoryRead = vi.fn(async () => {
      throw new Error("unauthorized native read");
    });
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: false, state, ...(state === "open" ? {} : { suppressed: true }) });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      await expect(assertGitPushAuthorized({ remote: "origin", branch: "work", execGitSync: () => "https://github.com/owner/repo.git\n", github: { rest: { repos: { get: repositoryRead } } } })).rejects.toThrow(
        /Completion|cancelled|settled/
      );
    });
    expect(repositoryRead).not.toHaveBeenCalled();
  });

  it("uses each checkout's cwd/auth and independently resolved native identity", async () => {
    const original = assignment();
    const seen = [];
    for (const member of original.claims) {
      const authorize = async request => {
        expect(request.requireCompletion).toBe(true);
        if (request.resource) expect(request.resource.repository_id).toBe("7");
        return allow(request);
      };
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize }, () =>
        assertGitPushAuthorized({
          remote: "origin",
          branch: `work-${member.handle}`,
          cwd: member.work.checkout,
          gitAuthEnv: { CHECKOUT_CREDENTIAL: member.handle },
          execGitSync: (_args, options) => {
            seen.push([options.cwd, options.env.CHECKOUT_CREDENTIAL]);
            return "https://github.com/owner/repo.git\n";
          },
          github: { rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } } },
        })
      );
    }
    expect(seen).toEqual([
      ["checkout-1", "h1"],
      ["checkout-2", "h2"],
    ]);
  });

  it("does not change checkout authentication before rejecting a direct signed-push fallback", async () => {
    const root = directory();
    execFileSync("git", ["init", "--quiet", root]);
    execFileSync("git", ["-C", root, "remote", "add", "origin", "https://github.com/owner/repo.git"]);
    global.exec = { exec: vi.fn(), getExecOutput: vi.fn() };
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: async request => ({ claim_handle: request.claim_handle, authorized: false }) }, async () => {
      await expect(
        pushSignedCommits({ owner: "owner", repo: "repo", branch: "work", baseRef: "main", cwd: root, signedCommits: false, pushRemoteUrl: "https://github.com/owner/repo.git", pushToken: "checkout-specific-token" })
      ).rejects.toThrow(/Completion/);
    });
    expect(global.exec.exec).not.toHaveBeenCalled();
    expect(global.exec.getExecOutput).not.toHaveBeenCalled();
  });

  it("resolves direct URLs and pushInsteadOf locally without persisting an ephemeral remote or contacting GitHub", async () => {
    const root = directory();
    execFileSync("git", ["init", "--quiet", root]);
    execFileSync("git", ["-C", root, "remote", "add", "unrelated", "https://github.com/foreign/unrelated.git"]);
    const read = vi.fn(async () => ({ data: { id: 7, full_name: "owner/repo" } }));
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === "owner/repo" });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      await expect(assertGitPushAuthorized({ remote: "https://github.com/owner/repo.git", branch: "work", cwd: root, github: { rest: { repos: { get: read } } } })).resolves.toBeUndefined();
      expect(read).toHaveBeenCalledTimes(1);
      execFileSync("git", ["-C", root, "config", "--local", "url.https://github.com/foreign/.pushInsteadOf", "https://github.com/owner/"]);
      await expect(assertGitPushAuthorized({ remote: "https://github.com/owner/repo.git", branch: "work", cwd: root, github: { rest: { repos: { get: read } } } })).rejects.toThrow(/Completion/);
      expect(read).toHaveBeenCalledTimes(1);
    });
    expect(execFileSync("git", ["-C", root, "remote"], { encoding: "utf8" }).trim()).toBe("unrelated");
  });
});

describe.each([1, 2])("native standalone adapters with %i original Claims", count => {
  it("routes upload bytes, coverage labels and SARIF categories through the original member", async () => {
    const original = assignment(count);
    const root = directory();
    const uploadedLabels = [];
    const categories = [];
    const authorize = async request => {
      expect(request.assignment).toEqual(original);
      expect(request.requireCompletion).toBe(true);
      return allow(request);
    };
    const github = {
      rest: {
        repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }), getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
        codeScanning: {
          uploadSarif: vi.fn(async input => {
            categories.push(JSON.parse(zlib.gunzipSync(Buffer.from(input.sarif, "base64"))).runs[0].automationDetails.id);
            return { data: { id: `sarif-${categories.length}` } };
          }),
        },
      },
      request: vi.fn(async (route, input) => {
        expect(route).toBe("PUT /repos/{owner}/{repo}/code-coverage/report");
        uploadedLabels.push(input.label);
        expect(zlib.gunzipSync(Buffer.from(input.coverage_report, "base64")).toString()).toBe(original.claims[uploadedLabels.length - 1].work.input);
        return { data: { id: `coverage-${uploadedLabels.length}` } };
      }),
    };
    for (const member of original.claims) {
      const reportRoot = scope.claimArtifactPath(root, member.handle, original);
      fs.mkdirSync(reportRoot, { recursive: true });
      fs.writeFileSync(path.join(reportRoot, "coverage.info"), member.work.input);
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize, effects: [] }, async () => {
        const selector = count === 1 ? {} : { claim_handle: member.handle };
        const scan = await scanning.main({ "target-ref": "refs/heads/main" }, github);
        const upload = await coverage.main({ "target-ref": "refs/heads/main", "coverage-dir": root }, github);
        if (count > 1) {
          await expect(scan({ type: "create_code_scanning_alert", file: "src/file.js", line: 1, severity: "warning", message: "finding" })).rejects.toThrow(/claim_handle is required/);
          await expect(upload({ type: "upload_code_coverage", file: "coverage.info", label: "coverage", language: "c" })).rejects.toThrow(/claim_handle is required/);
        }
        expect((await scan({ type: "create_code_scanning_alert", ...selector, file: "src/file.js", line: 1, severity: "warning", message: member.work.input })).success).toBe(true);
        expect((await upload({ type: "upload_code_coverage", ...selector, file: "coverage.info", label: "coverage", language: "c" })).success).toBe(true);
      });
    }
    expect(categories).toHaveLength(count);
    expect(new Set(categories).size).toBe(count);
    expect(uploadedLabels).toHaveLength(count);
    expect(new Set(uploadedLabels).size).toBe(count);
  });

  it.skipIf(process.platform === "win32")(
    "publishes Claim-isolated assets beside a legacy ref with matching URLs and private receipts",
    async () => {
      simulateLinuxAssetDescriptors();
      const original = assignment(count);
      const root = directory();
      const git = args =>
        execFileSync("git", ["-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", ...args], {
          cwd: root,
          encoding: "utf8",
          stdio: ["ignore", "pipe", "pipe"],
          env: { PATH: process.env.PATH, HOME: root, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null", GIT_TERMINAL_PROMPT: "0" },
        }).trim();
      git(["init", "--quiet", "--template="]);
      git(["-c", "user.name=Asset Test", "-c", "user.email=asset-test@example.com", "commit", "--quiet", "--allow-empty", "-m", "Legacy asset branch"]);
      const legacyHead = git(["rev-parse", "HEAD"]);
      git(["update-ref", "refs/heads/assets/audit", legacyHead]);
      expect(() => git(["update-ref", "refs/heads/assets/audit/claims/old", legacyHead])).toThrow(/cannot lock ref/);
      const refs = new Map([["heads/assets/audit", legacyHead]]);
      process.env.GITHUB_WORKSPACE = root;
      process.env.RUNNER_TEMP = root;
      process.env.GH_AW_ASSETS_BRANCH = "assets/audit";
      const assetsRoot = path.join(root, "gh-aw", "safeoutputs", "assets");
      const trees = new Map();
      const commits = new Map();
      const blobs = new Map();
      const createRefCalls = [];
      const updateRefCalls = [];
      const updateRefHadExistingRef = [];
      const results = [];
      const sha = value => crypto.createHash("sha1").update(JSON.stringify(value)).digest("hex");
      const github = {
        rest: {
          repos: {
            get: async () => ({ data: { id: 7, full_name: "owner/repo" } }),
            compareCommits: vi.fn(async ({ base, head }) => {
              const pending = [head];
              const seen = new Set();
              while (pending.length) {
                const commit = pending.pop();
                if (commit === base) return { data: { base_commit: { sha: base }, merge_base_commit: { sha: base }, status: "ahead", ahead_by: 1, behind_by: 0 } };
                if (seen.has(commit)) continue;
                seen.add(commit);
                pending.push(...(commits.get(commit)?.parents || []).map(parent => parent.sha));
              }
              return { data: { base_commit: { sha: base }, status: "diverged", ahead_by: 1, behind_by: 1 } };
            }),
          },
          git: {
            getRef: async input => {
              if (!refs.has(input.ref)) throw Object.assign(new Error("missing ref"), { status: 404 });
              return { data: { ref: `refs/${input.ref}`, object: { type: "commit", sha: refs.get(input.ref) } } };
            },
            createBlob: async input => {
              const id = sha(input);
              blobs.set(id, input.content);
              return { data: { sha: id } };
            },
            createTree: async input => {
              const id = sha(input);
              const entries = new Map((trees.get(input.base_tree) || []).map(entry => [entry.path, entry]));
              input.tree.forEach(entry => entries.set(entry.path, entry));
              trees.set(id, [...entries.values()]);
              return { data: { sha: id } };
            },
            createCommit: async input => {
              const id = sha(input);
              commits.set(id, { sha: id, tree: { sha: input.tree }, parents: input.parents.map(parent => ({ sha: parent })) });
              return { data: { sha: id } };
            },
            createRef: async input => {
              createRefCalls.push(input);
              // Real Git enforces the legacy ref's file/directory conflict.
              git(["update-ref", input.ref, legacyHead]);
              refs.set(input.ref.slice(5), input.sha);
              return { data: { object: { sha: input.sha } } };
            },
            updateRef: async input => {
              updateRefCalls.push(input);
              updateRefHadExistingRef.push(refs.has(input.ref));
              refs.set(input.ref, input.sha);
              return { data: { object: { sha: input.sha } } };
            },
            getCommit: async input => ({ data: commits.get(input.commit_sha) }),
            getTree: async input => ({ data: { sha: input.tree_sha, truncated: false, tree: trees.get(input.tree_sha) } }),
            getBlob: async input => ({ data: { sha: input.file_sha, encoding: "base64", content: blobs.get(input.file_sha) } }),
          },
        },
      };
      for (const member of original.claims) {
        await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize: allow, effects: [] }, async () => {
          const entries = [];
          const producer = createHandlers({}, entry => entries.push(entry));
          const handler = await assets.main({ "assets-dir": assetsRoot, "target-repo": "owner/repo", branch: "assets/audit" }, github);
          for (const filename of ["same.png", "second.png"]) {
            const source = path.join(root, filename);
            fs.writeFileSync(source, member.work.input + filename);
            producer.uploadAssetHandler({ path: source, ...(count > 1 ? { claim_handle: member.handle } : {}) });
            const entry = entries.at(-1);
            const message = { ...entry, ...(count > 1 ? { claim_handle: member.handle } : {}) };
            const result = await handler(message);
            results.push({ claimHandle: member.handle, result });
            expect(result.sha).toBe(digest(member.work.input + filename));
            expect(result.url).toBe(entry.url);
            expect(result.path).toBe(entry.targetFileName);
            expect(result.branch).toMatch(/^assets\/audit-claim-[a-f0-9]{64}$/);
            expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified).toBe(true);
            expect((await assets.verifyAssetDelivery({ claim: member, result: structuredClone(result), github })).verified).toBe(false);
          }
        });
      }
      for (const member of original.claims) {
        await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize: allow, effects: [] }, async () => {
          for (const { result } of results.filter(receipt => receipt.claimHandle === member.handle)) {
            const proof = await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow });
            expect(proof.verified).toBe(true);
            expect(proof.effect_resources).toEqual([
              { kind: "git_blob", repository: result.repo, id: trees.get(commits.get(result.commit).tree.sha).find(entry => entry.path === result.path).sha },
              { kind: "git_tree", repository: result.repo, id: commits.get(result.commit).tree.sha },
              { kind: "git_commit", repository: result.repo, id: result.commit },
              { kind: "git_ref", repository: result.repo, id: result.commit },
            ]);
            expect((await assets.verifyAssetDelivery({ claim: member, result: structuredClone(result), github })).verified).toBe(false);
            const differentClaim = original.claims.find(claim => claim.handle !== member.handle) || assignment(2).claims[1];
            expect((await assets.verifyAssetDelivery({ claim: differentClaim, result, github })).verified).toBe(false);
          }
        });
      }
      const member = original.claims[0];
      const result = results[0].result;
      const ref = `heads/${result.branch}`;
      const head = refs.get(ref);
      const headTree = commits.get(head).tree.sha;
      const headEntries = trees.get(headTree);
      const asset = headEntries.find(entry => entry.path === result.path);
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize: allow, effects: [] }, async () => {
        for (const mutation of ["unrelated", "removed", "replaced", "moved", "mode"]) {
          const tree = sha({ mutation, headTree });
          const commit = sha({ mutation, head });
          const entries = headEntries.filter(entry => entry.path !== result.path);
          if (mutation === "unrelated") entries.push(asset);
          if (mutation === "replaced") entries.push({ ...asset, sha: "b".repeat(40) });
          if (mutation === "moved") entries.push({ ...asset, path: `moved/${asset.path}` });
          if (mutation === "mode") entries.push({ ...asset, mode: "100755" });
          trees.set(tree, entries);
          commits.set(commit, { sha: commit, tree: { sha: tree }, parents: mutation === "unrelated" ? [] : [{ sha: head }] });
          refs.set(ref, commit);
          try {
            expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified, mutation).toBe(false);
          } finally {
            refs.set(ref, head);
            commits.delete(commit);
            trees.delete(tree);
          }
        }
        const originalCompare = github.rest.repos.compareCommits;
        for (const mismatch of [{ base_commit: { sha: "b".repeat(40) } }, { merge_base_commit: { sha: "b".repeat(40) } }, { status: "diverged" }, { ahead_by: 0 }, { ahead_by: 1.5 }, { behind_by: 1 }]) {
          github.rest.repos.compareCommits = vi.fn(async () => ({ data: { base_commit: { sha: result.commit }, merge_base_commit: { sha: result.commit }, status: "ahead", ahead_by: 1, behind_by: 0, ...mismatch } }));
          expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified).toBe(false);
        }
        github.rest.repos.compareCommits = vi.fn(async () => {
          throw new Error("comparison unavailable");
        });
        await expect(assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).rejects.toThrow(/comparison unavailable/);
        github.rest.repos.compareCommits = originalCompare;
        const originalGetTree = github.rest.git.getTree;
        for (const mismatch of [{ truncated: true }, { sha: "b".repeat(40) }]) {
          github.rest.git.getTree = async input => {
            const response = await originalGetTree(input);
            return input.tree_sha === headTree ? { data: { ...response.data, ...mismatch } } : response;
          };
          expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified).toBe(false);
        }
        github.rest.git.getTree = originalGetTree;
        const originalGetRef = github.rest.git.getRef;
        for (const receipt of [result, results[1].result]) {
          for (const mismatch of [{ ref: "refs/heads/unrelated" }, { object: { type: "tree", sha: head } }, { object: { type: "commit", sha: "" } }, { object: null }]) {
            github.rest.git.getRef = async input => ({ data: { ...(await originalGetRef(input)).data, ...mismatch } });
            expect((await assets.verifyAssetDelivery({ claim: member, result: receipt, github, authorize: allow })).verified).toBe(false);
          }
        }
        let refReads = 0;
        github.rest.git.getRef = async input => {
          const response = await originalGetRef(input);
          return ++refReads === 2 ? { data: { ...response.data, object: { type: "commit", sha: legacyHead } } } : response;
        };
        expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified).toBe(false);
        github.rest.git.getRef = originalGetRef;
        await expect(assets.verifyAssetDelivery({ claim: member, result, github, authorize: async request => ({ claim_handle: request.claim_handle, authorized: false }) })).rejects.toThrow(/Completion/);
        expect((await assets.verifyAssetDelivery({ claim: member, result, github, authorize: allow })).verified).toBe(true);
      });
      expect(refs.get("heads/assets/audit")).toBe(legacyHead);
      expect(git(["rev-parse", "refs/heads/assets/audit"])).toBe(legacyHead);
      const legacyRefCount = 1;
      expect(refs.size).toBe(count + legacyRefCount);
      expect(createRefCalls).toHaveLength(count);
      expect(updateRefCalls).toHaveLength(count);
      expect(updateRefCalls.every((input, index) => input.force === false && updateRefHadExistingRef[index])).toBe(true);
      expect(new Set(results.map(({ result }) => result.branch)).size).toBe(count);
      expect(new Set(results.map(({ result }) => result.path)).size).toBe(count * 2);
    },
    60000
  );
});

describe("standalone adapter rejection before file or native effect access", () => {
  it.each(["!!!", "", "   "])("rejects an empty normalized publisher branch %j before files or native endpoints", async branch => {
    const access = vi.fn(() => {
      throw new Error("invalid branch cannot access native endpoints");
    });
    const github = new Proxy({}, { get: access });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: allow }, async () => {
      await expect(assets.main({ branch, "assets-dir": "missing-assets", "target-repo": "owner/repo" }, github)).rejects.toThrow(/E001:.*non-empty normalized name/);
    });
    expect(access).not.toHaveBeenCalled();
  });

  it.each(["symlink", "dangling", "hardlink"])("rejects %s Claim assets before native publication", async kind => {
    simulateLinuxAssetDescriptors();
    const root = directory();
    const original = assignment();
    const reportRoot = scope.claimArtifactPath(root, "h1", original);
    fs.mkdirSync(reportRoot, { recursive: true });
    const sentinel = path.join(root, "external-sentinel.png");
    fs.writeFileSync(sentinel, "harmless external sentinel");
    const staged = path.join(reportRoot, digest("chart.png") + ".png");
    if (kind === "hardlink") fs.linkSync(sentinel, staged);
    else fs.symlinkSync(kind === "dangling" ? path.join(root, "missing-sentinel.png") : sentinel, staged);
    const github = { rest: { repos: { get: vi.fn() }, git: { createBlob: vi.fn() } } };
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize: allow }, async () => {
      const handler = await assets.main({ "assets-dir": root, "target-repo": "owner/repo" }, github);
      await expect(handler({ type: "upload_asset", claim_handle: "h1", path: "chart.png" })).rejects.toThrow();
    });
    expect(github.rest.repos.get).not.toHaveBeenCalled();
    expect(github.rest.git.createBlob).not.toHaveBeenCalled();
    expect(fs.readFileSync(sentinel, "utf8")).toBe("harmless external sentinel");
  });

  it.each(["cancelled", "result"])("rejects terminal %s selectors before accessing any standalone upload endpoint", async state => {
    const access = vi.fn(() => {
      throw new Error("terminal Claim cannot access native credentials or endpoints");
    });
    const github = new Proxy({}, { get: access });
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: false, state, suppressed: true });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      for (const module of [scanning, coverage, assets]) {
        const handler = await module.main({ "target-ref": "refs/heads/main", "target-repo": "owner/repo" }, github);
        await expect(handler({ type: "upload", claim_handle: "h1" })).rejects.toThrow(/cancelled|already settled/);
      }
    });
    expect(access).not.toHaveBeenCalled();
  });

  it("rejects invalid SARIF paths and missing/invalid coverage reports before native reads", async () => {
    const root = directory();
    const github = { rest: { repos: { getCommit: vi.fn(), get: vi.fn() }, codeScanning: { uploadSarif: vi.fn() } }, request: vi.fn() };
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: allow }, async () => {
      const scan = await scanning.main({ "target-ref": "refs/heads/main", max: 16 }, github);
      for (const file of ["../outside", "src//file.js", "./file.js", "src/\u0000file.js", "a".repeat(257)]) {
        await expect(scan({ type: "create_code_scanning_alert", claim_handle: "h1", file, line: 1, severity: "warning", message: "finding" })).rejects.toThrow(/malformed/);
      }
      const upload = await coverage.main({ "target-ref": "refs/heads/main", "coverage-dir": root, max: 16 }, github);
      for (const file of ["../outside", "report\\file", ".", "report\u0000file", "missing.info"]) {
        await expect(upload({ type: "upload_code_coverage", claim_handle: "h1", file, language: "c", label: "coverage" })).rejects.toThrow();
      }
    });
    expect(github.rest.repos.getCommit).not.toHaveBeenCalled();
    expect(github.rest.repos.get).not.toHaveBeenCalled();
    expect(github.rest.codeScanning.uploadSarif).not.toHaveBeenCalled();
    expect(github.request).not.toHaveBeenCalled();
  });

  it("keeps assets staged without reading files and rejects missing assets and explicit repository overrides before native reads", async () => {
    const github = { rest: { repos: { get: vi.fn() }, git: {} } };
    const root = directory();
    const original = assignment();
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize: allow }, async () => {
      const handler = await assets.main({ "assets-dir": root, "target-repo": "owner/repo", max: 8 }, github);
      await expect(handler({ type: "upload_asset", claim_handle: "h1", path: "missing.png" })).rejects.toThrow();
      await expect(handler({ type: "upload_asset", claim_handle: "h1", path: "missing.exe" })).rejects.toThrow(/extension/);
      await expect(handler({ type: "upload_asset", claim_handle: "h1", repo: "foreign/repo", path: "missing.png" })).rejects.toThrow(/conflicts/);
      await expect(handler({ type: "upload_asset", path: "missing.png" })).rejects.toThrow(/claim_handle is required/);
      const preview = await assets.main({ staged: true, "assets-dir": root, "target-repo": "owner/repo" }, github);
      expect(await preview({ type: "upload_asset", claim_handle: "h1", path: "missing.png" })).toEqual({ success: true, staged: true, claim_handle: "h1" });
    });
    expect(github.rest.repos.get).not.toHaveBeenCalled();
    expect(fs.readdirSync(root)).toEqual([]);
  });
});

describe("compiler-produced script preparation input", () => {
  it("rejects foreign attribution and malformed bounded input before loading the script", async () => {
    const root = directory();
    process.env.RUNNER_TEMP = root;
    process.env.GH_AW_CLAIM_INPUT = path.join(root, "input.json");
    process.env.GH_AW_CLAIM_OUTPUT = path.join(root, "output.json");
    process.env.GH_AW_CLAIM_SCRIPT_FILENAME = "safe_output_script_custom.cjs";
    const input = { version: 3, claim_handle: "h1", type: "custom", messages: [{ type: "custom", claim_handle: "h1" }] };
    for (const invalid of [
      { ...input, version: 2 },
      { ...input, messages: {} },
      { ...input, messages: [{ type: "custom", claim_handle: "h2" }] },
      { ...input, messages: [{ type: "other", claim_handle: "h1" }] },
      { ...input, messages: Array(129).fill(input.messages[0]) },
    ]) {
      fs.writeFileSync(process.env.GH_AW_CLAIM_INPUT, JSON.stringify(invalid));
      await expect(prepareScript()).rejects.toThrow(/bounded version-3/);
    }
    expect(fs.existsSync(process.env.GH_AW_CLAIM_OUTPUT)).toBe(false);
  });
});
