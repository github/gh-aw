import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import zlib from "node:zlib";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
const { createAssetGitFixture } = require("./work_queue_asset_git.test_helpers.cjs");
const modules = {
  asset: ["work_queue_upload_assets.cjs", "Asset"],
  code_scanning: ["work_queue_code_scanning.cjs", "CodeScanning"],
  code_coverage: ["work_queue_code_coverage.cjs", "CodeCoverage"],
};
const assignment = {
  version: 3,
  dispatch_id: "dispatch",
  request_id: "request",
  commit_id: "commit",
  policy_epoch: "policy",
  pool: "pool",
  worker_profile: "profile",
  claims: [{ handle: "h1", claim_id: "claim", work_id: "work", work: {}, result_refs: [] }],
};
const claim = assignment.claims[0];
const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
const execute = callback => scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects: [] }, callback);
const sha = value => crypto.createHash("sha256").update(value).digest("hex");
let root;
let environment;
beforeEach(() => {
  environment = { ...process.env };
  for (const key of ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT", "GH_AW_SAFE_OUTPUTS_STAGED", "GITHUB_API_URL", "GITHUB_SERVER_URL"]) delete process.env[key];
  Object.assign(process.env, { GITHUB_REPOSITORY: "owner/repo", GITHUB_SHA: "a".repeat(40) });
  fs.mkdirSync(".queue-assets-test-scratch", { recursive: true });
  root = fs.mkdtempSync(path.resolve(".queue-assets-test-scratch", "receipts-"));
});
afterEach(() => {
  process.env = environment;
  fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
});

function client(ledger = "") {
  const { api, state, git, logBlob, durableBlob } = createAssetGitFixture(root, ledger);
  const github = {
    rest: {
      repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }), getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
      git: api,
      codeScanning: {
        uploadSarif: async input => {
          state.report = JSON.parse(zlib.gunzipSync(Buffer.from(input.sarif, "base64")));
          return { data: { id: "sarif-1" } };
        },
        getSarif: async () => ({ data: { processing_status: "complete", analyses_url: "https://api.github.com/repos/owner/repo/code-scanning/analyses?sarif_id=sarif-1" } }),
        getAnalysis: async () => {
          const report = structuredClone(state.report);
          if (state.bad) report.runs[0].results[0].message.text = "changed";
          return { data: report };
        },
      },
    },
    request: async route => {
      if (route.startsWith("PUT ")) return { data: { id: "coverage-1" } };
      if (route.includes("code-coverage")) return { data: { processing_status: state.bad ? "failed" : "succeeded" } };
      return { data: [{ id: 9, sarif_id: "sarif-1", commit_sha: process.env.GITHUB_SHA, ref: "refs/heads/main", category: state.report.runs[0].automationDetails.id, tool: state.report.runs[0].tool.driver, results_count: 1 }] };
    },
  };
  return { github, state, git, logBlob, durableBlob };
}

function stageAsset(source, content = source) {
  const directory = scope.claimArtifactPath(root, "h1", assignment);
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, sha(source) + path.extname(source)), content);
}

function assertCoveredEffects(effects, proofs) {
  const resources = proofs.flatMap(proof => proof.effect_resources);
  expect(effects.every(effect => effect.outcome === "succeeded" && resources.some(resource => resource.kind === effect.kind && resource.id === effect.id && resource.repository === effect.repository))).toBe(true);
}

describe("content-addressed asset deliveries on the existing work-queue branch", () => {
  it.each(["second.png", "first.png"])("keeps both uploads independently verifiable when the second path is %s", async secondPath => {
    let assets = require("./work_queue_upload_assets.cjs");
    const { github, state } = client();
    const directory = scope.claimArtifactPath(root, "h1", assignment);
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, sha("first.png") + ".png"), "first asset");
    if (secondPath !== "first.png") fs.writeFileSync(path.join(directory, sha(secondPath) + ".png"), "second asset");
    const results = await execute(async () => {
      const handler = await assets.main({ "target-repo": "owner/repo", "assets-dir": root, max: 2 }, github);
      return [await handler({ type: "upload_asset", path: "first.png" }), await handler({ type: "upload_asset", path: secondPath })];
    });
    const receipts = await execute(() => results.map(result => assets.durableAssetReceipt(result, claim)));
    delete require.cache[require.resolve("./work_queue_upload_assets.cjs")];
    assets = require("./work_queue_upload_assets.cjs");
    const verify = index => execute(() => assets.verifyAssetDelivery({ claim, github, authorize, durable: true, result: { entry: { index, type: "upload_asset", verifier: "asset", receipt: JSON.parse(JSON.stringify(receipts[index])) } } }));
    expect((await verify(0)).verified).toBe(true);
    expect((await verify(1)).verified).toBe(true);
    expect(results[0].branch).toBe(results[1].branch);
    expect(results[0].branch).toBe("work-queue");
    expect(state.refs.size).toBe(1);
    state.refs.set(`heads/${results[0].branch}`, results[0].commit);
    expect((await verify(0)).verified).toBe(true);
    expect((await verify(1)).verified).toBe(secondPath === "first.png");
  });

  it("recovers two concurrent uploads from the same parent without losing files or rejected-attempt effect fencing", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github, state, git, logBlob, durableBlob } = client();
    stageAsset("first.png");
    stageAsset("second.png");
    const getRef = github.rest.git.getRef;
    let release;
    const barrier = new Promise(resolve => {
      release = resolve;
    });
    let reads = 0;
    vi.spyOn(github.rest.git, "getRef").mockImplementation(async input => {
      const response = await getRef(input);
      if (++reads <= 2) {
        if (reads === 2) release();
        await barrier;
      }
      return response;
    });
    const update = vi.spyOn(github.rest.git, "updateRef");
    const effects = [];
    const results = await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root, max: 2 }, github);
      return Promise.all([handler({ type: "upload_asset", path: "first.png" }), handler({ type: "upload_asset", path: "second.png" })]);
    });
    expect(update).toHaveBeenCalledTimes(3);
    expect(update.mock.calls.every(([input]) => input.force === false)).toBe(true);
    expect(state.refs.size).toBe(1);
    const head = state.refs.get("heads/work-queue");
    expect(
      git(["rev-parse", `${head}:work-queue.jsonl`])
        .toString("utf8")
        .trim()
    ).toBe(logBlob);
    expect(
      git(["rev-parse", `${head}:projection/state.json`])
        .toString("utf8")
        .trim()
    ).toBe(durableBlob);
    const proofs = await execute(() => Promise.all(results.map(result => assets.verifyAssetDelivery({ claim, result, github, authorize }))));
    expect(proofs.every(proof => proof.verified)).toBe(true);
    expect(effects).toHaveLength(8);
    assertCoveredEffects(effects, proofs);
  });

  it("rebases an upload over a checked ledger publication and preserves assets in subsequent ledger writes and checkpoint proofs", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { canonical } = require("./work_queue_codec.cjs");
    const { newRequest } = require("./work_queue_replay.cjs");
    const { administrator, context, genesis } = require("./work_queue_test_helpers.cjs");
    const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");
    const { publishWorkQueueRequest, readWorkQueueLog, compactWorkQueue } = require("./work_queue_store.cjs");
    const { github, state, git, durableBlob } = client(canonical(genesis()) + "\n");
    registerQueueLedgerClient(github, github);
    const options = { githubClient: github, owner: "owner", repo: "repo", context: context(administrator), now: () => 1000, sleepFn: async () => {} };
    await compactWorkQueue(options);
    const updateRef = github.rest.git.updateRef;
    let ledgerPublication;
    vi.spyOn(github.rest.git, "updateRef").mockImplementationOnce(async input => {
      const operations = [{ kind: "Control", control: "admission_paused", value: true, reason: "asset_interleaving" }];
      ledgerPublication = await publishWorkQueueRequest({ ...options, request: newRequest("interleaved-ledger", "control", administrator, { operations }) });
      return updateRef(input);
    });
    stageAsset("asset.png");
    const effects = [];
    const result = await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      return handler({ type: "upload_asset", path: "asset.png" });
    });
    expect(ledgerPublication.persisted).toBe(true);
    const afterUpload = await readWorkQueueLog(options);
    expect(afterUpload.state.requests.has("interleaved-ledger")).toBe(true);
    expect(afterUpload.transactions[0].request.kind).toBe("checkpoint");
    expect(
      git(["rev-parse", `${result.commit}:projection/state.json`])
        .toString("utf8")
        .trim()
    ).toBe(durableBlob);
    expect(
      git(["rev-parse", `${result.commit}^`])
        .toString("utf8")
        .trim()
    ).toBe(ledgerPublication.sha);
    const operations = [{ kind: "Control", control: "admission_paused", value: false, reason: "after_asset" }];
    await publishWorkQueueRequest({ ...options, request: newRequest("subsequent-ledger", "control", administrator, { operations }) });
    const current = await readWorkQueueLog(options);
    expect(current.state.requests.has("subsequent-ledger")).toBe(true);
    expect(current.sha).toBe(state.refs.get("heads/work-queue"));
    const proof = await execute(() => assets.verifyAssetDelivery({ claim, result, github, authorize }));
    expect(proof.verified).toBe(true);
    expect(effects).toHaveLength(4);
    assertCoveredEffects(effects, [proof]);
  });

  it("fails closed for a missing queue branch before creating native objects", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github, state } = client();
    state.refs.delete("heads/work-queue");
    stageAsset("asset.png");
    const writes = ["createBlob", "createTree", "createCommit", "createRef", "updateRef"].map(method => vi.spyOn(github.rest.git, method));
    await execute(async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      await expect(handler({ type: "upload_asset", path: "asset.png" })).rejects.toThrow("existing work-queue branch");
    });
    expect(writes.every(write => write.mock.calls.length === 0)).toBe(true);
  });

  it("diagnoses ignored worker branch overrides without echoing configuration and still uses work-queue", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github, state } = client();
    stageAsset("asset.png");
    const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const result = await execute(async () => {
      const handler = await assets.main({ "assets-dir": root, branch: "assets/private-config-sentinel" }, github);
      return handler({ type: "upload_asset", path: "asset.png" });
    });
    expect(result.branch).toBe("work-queue");
    expect(state.refs.size).toBe(1);
    const diagnostic = stderr.mock.calls.flat().join("");
    expect(diagnostic).toContain("asset.branch_override_ignored");
    expect(diagnostic).toContain("applies only to ordinary uploads");
    expect(diagnostic).not.toContain("private-config-sentinel");
  });

  it.each([
    { status: 403, message: "not a fast forward" },
    { status: 401, message: "unauthorized" },
    { status: 500, message: "not a fast forward" },
    { status: 408, message: "timeout" },
    { status: 409, message: "unrelated conflict" },
    { status: 422, message: "invalid parameters" },
    { message: "connection lost" },
  ])("does not retry or discard write inventory for a non-definitive ref failure: %j", async failure => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github } = client();
    stageAsset("asset.png");
    const error = Object.assign(new Error(failure.message), failure);
    const update = vi.spyOn(github.rest.git, "updateRef").mockRejectedValue(error);
    const effects = [];
    await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      await expect(handler({ type: "upload_asset", path: "asset.png" })).rejects.toBe(error);
    });
    expect(update).toHaveBeenCalledTimes(1);
    expect(effects).toHaveLength(4);
    expect(effects[3]).toMatchObject({ kind: "git_ref", outcome: "unknown" });
  });

  it("does not replay a ref update whose successful write response was lost", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github, state } = client();
    stageAsset("asset.png");
    const initialHead = state.refs.get("heads/work-queue");
    const updateRef = github.rest.git.updateRef;
    const error = Object.assign(new Error("response lost after commit"), { status: 500 });
    const update = vi.spyOn(github.rest.git, "updateRef").mockImplementation(async input => {
      await updateRef(input);
      throw error;
    });
    const effects = [];
    await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      await expect(handler({ type: "upload_asset", path: "asset.png" })).rejects.toBe(error);
    });
    expect(update).toHaveBeenCalledTimes(1);
    expect(state.refs.get("heads/work-queue")).not.toBe(initialHead);
    expect(effects).toHaveLength(4);
    expect(effects[3]).toMatchObject({ kind: "git_ref", outcome: "unknown" });
  });

  it.each(["createBlob", "createTree", "createCommit"])("does not misclassify a %s failure as a rejected ref conflict", async method => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github } = client();
    stageAsset("asset.png");
    const error = Object.assign(new Error("reference update failed: head changed"), { status: 409 });
    const object = vi.spyOn(github.rest.git, method).mockRejectedValue(error);
    const update = vi.spyOn(github.rest.git, "updateRef");
    const effects = [];
    await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      await expect(handler({ type: "upload_asset", path: "asset.png" })).rejects.toBe(error);
    });
    expect(object).toHaveBeenCalledTimes(1);
    expect(update).not.toHaveBeenCalled();
    expect(effects.at(-1).outcome).toBe("unknown");
  });

  it("bounds definitive conflicts without force updates, replaying bytes, or leaking rejected ref effects", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github, state } = client();
    stageAsset("asset.png");
    const updateRef = github.rest.git.updateRef;
    let interleaving = 0;
    const update = vi.spyOn(github.rest.git, "updateRef").mockImplementation(async input => {
      const head = state.refs.get(input.ref);
      const parent = await github.rest.git.getCommit({ commit_sha: head });
      const winner = await github.rest.git.createCommit({ tree: parent.data.tree.sha, parents: [head], message: `Interleaved publication ${++interleaving}` });
      await updateRef({ ...input, sha: winner.data.sha });
      return updateRef(input);
    });
    const blob = vi.spyOn(github.rest.git, "createBlob");
    const effects = [];
    await scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      await expect(handler({ type: "upload_asset", path: "asset.png" })).rejects.toThrow("publication conflicts exhausted");
    });
    expect(update).toHaveBeenCalledTimes(4);
    expect(blob).toHaveBeenCalledTimes(1);
    expect(update.mock.calls.every(([input]) => input.force === false)).toBe(true);
    expect(effects).toHaveLength(1);
    expect(effects[0]).toMatchObject({ kind: "git_blob", outcome: "succeeded" });
  });

  it("verifies only the exact asset path even when an unrelated recursive listing would be truncated", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github } = client();
    stageAsset("asset.png");
    const result = await execute(async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      return handler({ type: "upload_asset", path: "asset.png" });
    });
    const getTree = github.rest.git.getTree;
    const tree = vi.spyOn(github.rest.git, "getTree").mockImplementation(async input => {
      const response = await getTree(input);
      return input.recursive ? { data: { ...response.data, truncated: true, tree: [] } } : response;
    });
    expect((await execute(() => assets.verifyAssetDelivery({ claim, result, github, authorize }))).verified).toBe(true);
    expect(tree).toHaveBeenCalledTimes(3);
    expect(tree.mock.calls.every(([input]) => input.recursive === undefined)).toBe(true);
    tree.mockImplementation(async input => ({ data: { ...(await getTree(input)).data, truncated: true } }));
    expect((await execute(() => assets.verifyAssetDelivery({ claim, result, github, authorize }))).verified).toBe(false);
  });

  it("does not rebind a durable sibling receipt to another Claim's namespace", async () => {
    const assets = require("./work_queue_upload_assets.cjs");
    const { github } = client();
    const sibling = { ...claim, handle: "h2", claim_id: "sibling-claim", work_id: "sibling-work" };
    const original = { ...assignment, claims: [claim, sibling] };
    const directory = scope.claimArtifactPath(root, sibling.handle, original);
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, sha("asset.png") + ".png"), "sibling asset");
    const receipt = await scope.withClaimExecution({ assignment: original, claim_handle: sibling.handle, authorize, effects: [] }, async () => {
      const handler = await assets.main({ "assets-dir": root }, github);
      return assets.durableAssetReceipt(await handler({ type: "upload_asset", claim_handle: sibling.handle, path: "asset.png" }), sibling);
    });
    const proof = await scope.withClaimExecution({ assignment: original, claim_handle: claim.handle, authorize, effects: [] }, () =>
      assets.verifyAssetDelivery({ claim, github, authorize, durable: true, result: { entry: { verifier: "asset", receipt } } })
    );
    expect(proof).toMatchObject({ verified: false, diagnostics: { event: "asset.receipt_missing" } });
  });
});

describe.each(Object.entries(modules))("durable %s native verification", (verifier, [filename, name]) => {
  it("round-trips the private receipt, independently reads native state, and fails closed", async () => {
    let runtime = require(`./${filename}`);
    const { github, state } = client();
    const directory = scope.claimArtifactPath(root, "h1", assignment);
    fs.mkdirSync(directory, { recursive: true });
    fs.writeFileSync(path.join(directory, "coverage.info"), "coverage bytes");
    fs.writeFileSync(path.join(directory, sha("asset.png") + ".png"), "asset bytes");
    const result = await execute(async () => {
      const handler = await runtime.main({ "target-repo": "owner/repo", "target-ref": "refs/heads/main", "coverage-dir": root, "assets-dir": root, "wait-for-processing-timeout": 0 }, github);
      if (verifier === "asset") return handler({ type: "upload_asset", path: "asset.png" });
      if (verifier === "code_coverage") return handler({ type: "upload_code_coverage", file: "coverage.info", language: "c", label: "coverage" });
      return handler({ type: "create_code_scanning_alert", file: "src/file.js", line: 1, severity: "warning", message: "finding" });
    });
    const body = await execute(() => runtime[`durable${name}Receipt`](result, claim));
    expect(body).not.toBeNull();
    for (const key of ["dispatch_id", "claim_id", "work_id", "claim_handle"]) expect(body).not.toHaveProperty(key);
    if (verifier === "asset") expect(body.url).toBe(result.url);
    if (verifier === "code_scanning") {
      expect(body.finding_digest).toMatch(/^[a-f0-9]{64}$/);
      expect(body).not.toHaveProperty("finding");
    }
    const receipt = JSON.parse(JSON.stringify(body));
    const entry = { index: 0, type: "output", verifier, receipt };
    const verify = (overrides = {}) => execute(() => runtime[`verify${name}Delivery`]({ claim, github, authorize, durable: true, result: { entry }, ...overrides }));
    expect((await verify()).verified).toBe(true);
    expect((await verify({ durable: false })).verified).toBe(false);
    expect(await execute(() => runtime[`durable${name}Receipt`]({ ...result }, claim))).toBeNull();
    expect(await execute(() => runtime[`durable${name}Receipt`](result, { ...claim, claim_id: "foreign" }))).toBeNull();
    if (verifier === "code_coverage") {
      await execute(async () => {
        const handler = await runtime.main({ "target-ref": "refs/heads/main", "coverage-dir": root, "wait-for-processing-timeout": 601 }, github);
        const invalid = await handler({ type: "upload_code_coverage", file: "coverage.info", language: "c", label: "coverage" });
        expect(runtime.durableCodeCoverageReceipt(invalid, claim)).toBeNull();
        expect((await runtime.verifyCodeCoverageDelivery({ claim, result: invalid, github })).verified).toBe(false);
      });
    }
    delete require.cache[require.resolve(`./${filename}`)];
    runtime = require(`./${filename}`);
    expect((await verify()).verified).toBe(true);
    expect((await verify({ durable: false, result })).verified).toBe(false);
    expect((await verify({ result: { entry: { ...entry, verifier: "wrong" } } })).verified).toBe(false);
    for (const key of Object.keys(receipt).filter(key => key !== "url")) {
      const incomplete = { ...receipt };
      delete incomplete[key];
      expect((await verify({ result: { entry: { ...entry, receipt: incomplete } } })).verified).toBe(false);
    }
    state.bad = true;
    expect((await verify()).verified).toBe(false);
  });
});
