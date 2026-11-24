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
const { createClaimAdapterHandler, preparedAdapterPath } = require("./work_queue_claim_adapters.cjs");
const { assertGitPushAuthorized } = require("./work_queue_git_effects.cjs");
const bundle = require("./generate_git_bundle.cjs");
const patch = require("./git_patch_utils.cjs");
const scanning = require("./work_queue_code_scanning.cjs");
const coverage = require("./work_queue_code_coverage.cjs");
const assets = require("./work_queue_upload_assets.cjs");
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
  "RUNNER_TEMP",
  "GH_AW_CLAIM_INPUT",
  "GH_AW_CLAIM_OUTPUT",
  "GH_AW_CLAIM_SCRIPT_FILENAME",
];
const globalKeys = ["core", "github", "exec", "context", "getOctokit"];
const roots = [];
let environment;
let globals;
let restoreAssetDescriptors;

function simulateLinuxAssetDescriptors() {
  if (process.platform !== "darwin") return;
  // Claim asset parity uses real file I/O with bounded Linux descriptor-path
  // simulation on macOS; production access remains Linux-only.
  const originals = { openSync: fs.openSync, lstatSync: fs.lstatSync };
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
  restoreAssetDescriptors = () => Object.assign(fs, originals);
  vi.stubGlobal("process", new Proxy(process, { get: (target, key) => (key === "platform" ? "linux" : Reflect.get(target, key)) }));
}

function directory() {
  const root = path.resolve(".queue-git-audit-scratch", crypto.randomUUID());
  fs.mkdirSync(root, { recursive: true });
  roots.push(root);
  return root;
}

function assignment(dispatch = "original-dispatch", ordinal = 1) {
  return {
    dispatch_id: dispatch,
    claims: [{ handle: "h1", claim_id: `claim-${ordinal}`, work_id: `work-${ordinal}`, work: { checkout: `checkout-${ordinal}`, input: `input-${ordinal}` }, result_refs: [] }],
  };
}

/** Two valid singleton dispatches that both reuse handle h1 for different Claims. */
const dispatches = () => [assignment("original-dispatch", 1), assignment("second-dispatch", 2)];

function twoMemberAssignment() {
  const [first] = assignment().claims;
  const [second] = assignment("original-dispatch", 2).claims;
  return { dispatch_id: "original-dispatch", claims: [first, { ...second, handle: "h2" }] };
}

const adapter = { mode: "prepared", "effect-type": "update_issue", "target-repo": "owner/repo", "field-map": { item_number: "number", body: "content" } };
const allow = async request => ({ claim_handle: request.claim_handle, authorized: true });
const digest = value => crypto.createHash("sha256").update(value).digest("hex");

beforeEach(() => {
  environment = Object.fromEntries(environmentKeys.map(key => [key, process.env[key]]));
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

describe("singleton Claim slot-zero artifact and preparation routing", () => {
  it("prepares in slot zero and consumes the downloaded artifact at the manager's slot-zero path", async () => {
    const original = assignment();
    const [member] = original.claims;
    const root = directory();
    const input = { type: "custom", number: 41, content: member.work.input };
    const authorize = async request => {
      expect(request.assignment).toEqual(original);
      expect(request.claim_handle).toBe(member.handle);
      return allow(request);
    };
    const prepared = await prepareAdapterContext({ assignment: original, adapter, index: 0, type: "custom", messages: [input], authorize, artifactRoot: root });
    expect(prepared.messages).toEqual([{ ...input, claim_handle: member.handle }]);
    expect(JSON.parse(fs.readFileSync(prepared.assignmentFile, "utf8"))).toEqual(original);
    fs.writeFileSync(prepared.outputFile, JSON.stringify({ version: 3, claim_handle: member.handle, type: "custom", messages: [{ input: prepared.messages[0], payload: { number: 41, content: member.work.input } }] }));
    // The preparer uploads <root>/claims/; the trusted job downloads it into claim-adapters/<type>/0/claims/.
    const trusted = directory();
    fs.cpSync(path.join(root, "claims"), path.join(trusted, "claim-adapters", "custom", "0", "claims"), { recursive: true });
    const observed = [];
    await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize }, async () => {
      const loadEffectHandler = vi.fn(async () => async message => {
        observed.push(message);
        return { success: true };
      });
      const filename = preparedAdapterPath(path.join(trusted, "claim-adapters", "custom", "0"), scope.currentClaimHandle(), "custom");
      const handler = await createClaimAdapterHandler({ adapter, filename, loadEffectHandler });
      expect(loadEffectHandler).not.toHaveBeenCalled();
      expect(await handler(input)).toEqual({ success: true });
      expect(loadEffectHandler).toHaveBeenCalledTimes(1);
    });
    expect(observed).toEqual([{ type: "update_issue", claim_handle: member.handle, repo: "owner/repo", item_number: 41, body: member.work.input }]);
  });

  it("keeps older compiled slots 1-15 inactive for a valid singleton and rejects out-of-range slots", async () => {
    const original = assignment();
    const root = directory();
    const authorize = vi.fn(allow);
    const messages = [{ type: "custom", number: 41, content: "input-1" }];
    for (const index of [1, 15]) expect(await prepareAdapterContext({ assignment: original, adapter, index, type: "custom", messages, authorize, artifactRoot: root })).toEqual({ active: false });
    for (const index of [-1, 16, "x", undefined]) await expect(prepareAdapterContext({ assignment: original, adapter, index, type: "custom", messages, authorize, artifactRoot: root })).rejects.toThrow(/slot/);
    expect(authorize).not.toHaveBeenCalled();
    expect(fs.readdirSync(root)).toEqual([]);
  });
});

describe("prepared adapter rejection ordering", () => {
  it("does not authorize or prepare empty, foreign-only, contradictory, mistyped or two-member input", async () => {
    const original = assignment();
    const root = directory();
    const authorize = vi.fn(async () => {
      throw new Error("empty preparation cannot access credentials");
    });
    for (const messages of [[], [{ type: "custom", claim_handle: "foreign" }], [{ type: "custom", claim_handle: "h2", number: 42 }], [{ type: "custom", number: 42, work_id: "work:2" }], [{ type: "other", claim_handle: "h1" }]]) {
      expect(await prepareAdapterContext({ assignment: original, adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root })).toEqual({ active: false, claim_handle: "h1" });
    }
    const messages = [
      { type: "custom", claim_handle: "h1", number: 42 },
      { type: "custom", claim_handle: "h2", number: 43 },
    ];
    for (const index of [0, 1]) {
      await expect(prepareAdapterContext({ assignment: twoMemberAssignment(), adapter, index, type: "custom", messages, authorize, artifactRoot: root })).rejects.toThrow(/exactly one open Claim/);
    }
    expect(authorize).not.toHaveBeenCalled();
    expect(fs.readdirSync(root)).toEqual([]);
  });

  it("rejects foreign selectors, contradictory identities, cross-dispatch factories, missing artifacts and terminal Claims before loading effects", async () => {
    const [original, second] = dispatches();
    const loadEffectHandler = vi.fn(async () => {
      throw new Error("invalid input cannot acquire effect credentials");
    });
    const authorize = vi.fn(allow);
    let factory;
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize }, async () => {
      const handler = await createClaimAdapterHandler({ adapter, filename: path.join(directory(), "missing.json"), loadEffectHandler });
      factory = handler;
      for (const message of [
        { type: "custom", claim_handle: "foreign" },
        { type: "custom", claim_handle: "h2" },
        { type: "custom", claim_handle: "h1", work_id: "work:2" },
        { type: "custom", claim_id: "claim-2" },
      ])
        await expect(handler(message)).rejects.toThrow(/foreign|conflicts/);
      expect(authorize).not.toHaveBeenCalled();
      await expect(handler({ type: "custom" })).rejects.toThrow();
      await expect(handler({ type: "custom", claim_handle: "h1" })).rejects.toThrow();
    });
    authorize.mockClear();
    await scope.withClaimExecution({ assignment: second, claim_handle: "h1", authorize }, async () => {
      await expect(factory({ type: "custom", claim_handle: "h1" })).rejects.toThrow(/original immutable Claim identity/);
    });
    expect(authorize).not.toHaveBeenCalled();
    await scope.withClaimExecution({ assignment: original, claim_handle: "h1", authorize: async request => ({ claim_handle: request.claim_handle, authorized: false, state: "completed", suppressed: true }) }, async () => {
      const handler = await createClaimAdapterHandler({ adapter, filename: "unreadable", loadEffectHandler });
      await expect(handler({ type: "custom", claim_handle: "h1" })).rejects.toThrow(/already completed/);
    });
    expect(loadEffectHandler).not.toHaveBeenCalled();
  });

  it("keeps a cancelled singleton inactive and writes no preparation", async () => {
    const original = assignment();
    const root = directory();
    const messages = [{ type: "custom", claim_handle: "h1", number: 42, content: "input-1" }];
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: false, state: "cancelled", suppressed: true });
    await expect(prepareAdapterContext({ assignment: original, adapter, index: 0, type: "custom", messages, authorize, artifactRoot: root })).rejects.toThrow(/cancelled/);
    expect(fs.existsSync(path.join(root, "claims")) ? fs.readdirSync(path.join(root, "claims")) : []).toEqual([]);
  });
});

describe("git authorization before credentialed repository readback", () => {
  it.each(["unbound", "cancelled", "completed"])("rejects a %s Claim without accessing its repository client", async state => {
    const repositoryRead = vi.fn(async () => {
      throw new Error("unauthorized native read");
    });
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: false, state, ...(state === "unbound" ? {} : { suppressed: true }) });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      await expect(assertGitPushAuthorized({ remote: "origin", branch: "work", execGitSync: () => "https://github.com/owner/repo.git\n", github: { rest: { repos: { get: repositoryRead } } } })).rejects.toThrow(
        /not open|cancelled|completed/
      );
    });
    expect(repositoryRead).not.toHaveBeenCalled();
  });

  it("uses each singleton dispatch's checkout cwd/auth and independently resolved native identity", async () => {
    const seen = [];
    const execGitSync = (_args, options) => {
      seen.push([options.cwd, options.env.CHECKOUT_CREDENTIAL]);
      return "https://github.com/owner/repo.git\n";
    };
    const github = { rest: { repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) } } };
    const authorize = vi.fn(async request => {
      expect(request.requireCompletion).toBe(true);
      expect(request.assignment.claims).toHaveLength(1);
      if (request.resource) expect(request.resource.repository_id).toBe("7");
      return allow(request);
    });
    for (const original of dispatches()) {
      const [member] = original.claims;
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize }, () =>
        assertGitPushAuthorized({ remote: "origin", branch: `work-${member.claim_id}`, cwd: member.work.checkout, gitAuthEnv: { CHECKOUT_CREDENTIAL: member.claim_id }, execGitSync, github })
      );
    }
    expect(seen).toEqual([
      ["checkout-1", "claim-1"],
      ["checkout-2", "claim-2"],
    ]);
    const calls = authorize.mock.calls.length;
    expect(() =>
      scope.withClaimExecution({ assignment: twoMemberAssignment(), claim_handle: "h1", authorize }, () =>
        assertGitPushAuthorized({ remote: "origin", branch: "work-two-member", cwd: "checkout-1", gitAuthEnv: { CHECKOUT_CREDENTIAL: "two-member" }, execGitSync, github })
      )
    ).toThrow(/exactly one open Claim/);
    expect(seen).toHaveLength(2);
    expect(authorize).toHaveBeenCalledTimes(calls);
  });

  it("does not change checkout authentication before rejecting a direct signed-push fallback", async () => {
    const root = directory();
    execFileSync("git", ["init", "--quiet", root]);
    execFileSync("git", ["-C", root, "remote", "add", "origin", "https://github.com/owner/repo.git"]);
    global.exec = { exec: vi.fn(), getExecOutput: vi.fn() };
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize: async request => ({ claim_handle: request.claim_handle, authorized: false }) }, async () => {
      await expect(
        pushSignedCommits({ owner: "owner", repo: "repo", branch: "work", baseRef: "main", cwd: root, signedCommits: false, pushRemoteUrl: "https://github.com/owner/repo.git", pushToken: "checkout-specific-token" })
      ).rejects.toThrow(/not open/);
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
      await expect(assertGitPushAuthorized({ remote: "https://github.com/owner/repo.git", branch: "work", cwd: root, github: { rest: { repos: { get: read } } } })).rejects.toThrow(/not open/);
      expect(read).toHaveBeenCalledTimes(1);
    });
    expect(execFileSync("git", ["-C", root, "remote"], { encoding: "utf8" }).trim()).toBe("unrelated");
  });
});

describe.each([1, 2])("native standalone adapters across %i singleton dispatches reusing h1", count => {
  it("routes upload bytes, coverage labels and SARIF categories through each dispatch's sole member", async () => {
    const originals = dispatches().slice(0, count);
    const root = directory();
    const uploadedLabels = [];
    const categories = [];
    const authorize = async request => {
      expect(originals).toContainEqual(request.assignment);
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
          getSarif: async () => ({ data: { processing_status: "complete" } }),
        },
      },
      request: vi.fn(async (route, input) => {
        if (route.startsWith("GET ")) return { data: { processing_status: "succeeded" } };
        expect(route).toBe("PUT /repos/{owner}/{repo}/code-coverage/report");
        uploadedLabels.push(input.label);
        expect(zlib.gunzipSync(Buffer.from(input.coverage_report, "base64")).toString()).toBe(originals[uploadedLabels.length - 1].claims[0].work.input);
        return { data: { id: `coverage-${uploadedLabels.length}` } };
      }),
    };
    const factories = [];
    for (const original of originals) {
      const [member] = original.claims;
      const reportRoot = scope.claimArtifactPath(root, member.handle, original);
      fs.mkdirSync(reportRoot, { recursive: true });
      fs.writeFileSync(path.join(reportRoot, "coverage.info"), member.work.input);
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize, effects: [] }, async () => {
        const scan = await scanning.main({ "target-ref": "refs/heads/main" }, github);
        const upload = await coverage.main({ "target-ref": "refs/heads/main", "coverage-dir": root }, github);
        factories.push({ scan, upload });
        await expect(scan({ type: "create_code_scanning_alert", claim_handle: "h2", file: "src/file.js", line: 1, severity: "warning", message: "finding" })).rejects.toThrow(/foreign/);
        await expect(upload({ type: "upload_code_coverage", claim_handle: "h2", file: "coverage.info", label: "coverage", language: "c" })).rejects.toThrow(/foreign/);
        expect((await scan({ type: "create_code_scanning_alert", file: "src/file.js", line: 1, severity: "warning", message: member.work.input })).success).toBe(true);
        expect((await upload({ type: "upload_code_coverage", file: "coverage.info", label: "coverage", language: "c" })).success).toBe(true);
      });
    }
    if (count > 1) {
      await scope.withClaimExecution({ assignment: originals[1], claim_handle: "h1", authorize, effects: [] }, async () => {
        await expect(factories[0].scan({ type: "create_code_scanning_alert", file: "src/file.js", line: 1, severity: "warning", message: "escape" })).rejects.toThrow(/original immutable Claim identity/);
        await expect(factories[0].upload({ type: "upload_code_coverage", file: "coverage.info", label: "coverage", language: "c" })).rejects.toThrow(/original immutable Claim identity/);
      });
    }
    expect(categories).toHaveLength(count);
    expect(new Set(categories).size).toBe(count);
    expect(uploadedLabels).toHaveLength(count);
    expect(new Set(uploadedLabels).size).toBe(count);
  });

  it.skipIf(process.platform === "win32")("publishes independently namespaced assets per singleton dispatch", async () => {
    simulateLinuxAssetDescriptors();
    const originals = dispatches().slice(0, count);
    const root = directory();
    const refs = new Map();
    const trees = new Map();
    const commits = new Map();
    const blobs = new Map();
    const results = [];
    const sha = value => crypto.createHash("sha1").update(JSON.stringify(value)).digest("hex");
    const github = {
      rest: {
        repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }) },
        git: {
          getRef: async input => {
            if (!refs.has(input.ref)) throw Object.assign(new Error("missing ref"), { status: 404 });
            return { data: { object: { sha: refs.get(input.ref) } } };
          },
          createBlob: async input => {
            const id = sha(input);
            blobs.set(id, input.content);
            return { data: { sha: id } };
          },
          createTree: async input => {
            const id = sha(input);
            trees.set(id, input.tree);
            return { data: { sha: id } };
          },
          createCommit: async input => {
            const id = sha(input);
            commits.set(id, { sha: id, tree: { sha: input.tree }, parents: input.parents.map(parent => ({ sha: parent })) });
            return { data: { sha: id } };
          },
          createRef: async input => {
            refs.set(input.ref.slice(5), input.sha);
            return { data: { object: { sha: input.sha } } };
          },
          getCommit: async input => ({ data: commits.get(input.commit_sha) }),
          getTree: async input => ({ data: { sha: input.tree_sha, truncated: false, tree: trees.get(input.tree_sha) } }),
          getBlob: async input => ({ data: { sha: input.file_sha, encoding: "base64", content: blobs.get(input.file_sha) } }),
        },
      },
    };
    for (const original of originals) {
      const [member] = original.claims;
      const reportRoot = scope.claimArtifactPath(root, member.handle, original);
      fs.mkdirSync(reportRoot, { recursive: true });
      fs.writeFileSync(path.join(reportRoot, digest("same.png") + ".png"), member.work.input);
      await scope.withClaimExecution({ assignment: original, claim_handle: member.handle, authorize: allow, effects: [] }, async () => {
        const handler = await assets.main({ "assets-dir": root, "target-repo": "owner/repo", branch: "assets/audit" }, github);
        const result = await handler({ type: "upload_asset", path: "same.png" });
        results.push(result);
        expect(result.sha).toBe(digest(member.work.input));
        expect(result.path).toContain(scope.claimArtifactPath("", member.handle, original).split("/").pop());
      });
    }
    expect(refs.size).toBe(count);
    expect(new Set(results.map(result => result.branch)).size).toBe(count);
    expect(new Set(results.map(result => result.path)).size).toBe(count);
  });
});

describe("standalone adapter rejection before file or native effect access", () => {
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

  it.each(["cancelled", "completed"])("rejects terminal %s selectors before accessing any standalone upload endpoint", async state => {
    const access = vi.fn(() => {
      throw new Error("terminal Claim cannot access native credentials or endpoints");
    });
    const github = new Proxy({}, { get: access });
    const authorize = async request => ({ claim_handle: request.claim_handle, authorized: false, state, suppressed: true });
    await scope.withClaimExecution({ assignment: assignment(), claim_handle: "h1", authorize }, async () => {
      for (const module of [scanning, coverage, assets]) {
        const handler = await module.main({ "target-ref": "refs/heads/main", "target-repo": "owner/repo" }, github);
        await expect(handler({ type: "upload", claim_handle: "h1" })).rejects.toThrow(/cancelled|already completed/);
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
      await expect(handler({ type: "upload_asset", claim_handle: "h2", path: "missing.png" })).rejects.toThrow(/foreign/);
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
