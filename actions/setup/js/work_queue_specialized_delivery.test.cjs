import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import zlib from "node:zlib";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
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
  root = fs.mkdtempSync(path.join(os.tmpdir(), "specialized-receipts-"));
});
afterEach(() => {
  process.env = environment;
  fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
});

function client() {
  const state = { bad: false, report: null, blobs: new Map(), trees: new Map(), commits: new Map(), refs: new Map() };
  const objectId = value => crypto.createHash("sha1").update(JSON.stringify(value)).digest("hex");
  const github = {
    rest: {
      repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }), getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
      git: {
        getRef: async input => {
          if (!state.refs.has(input.ref)) throw Object.assign(new Error("missing"), { status: 404 });
          return { data: { object: { sha: state.bad ? "changed" : state.refs.get(input.ref) } } };
        },
        createBlob: async input => {
          const id = objectId(input);
          state.blobs.set(id, input.content);
          return { data: { sha: id } };
        },
        createTree: async input => {
          const entries = [...(state.trees.get(input.base_tree) || []), ...input.tree];
          const id = objectId(entries);
          state.trees.set(id, entries);
          return { data: { sha: id } };
        },
        createCommit: async input => {
          const id = objectId(input);
          state.commits.set(id, { sha: id, tree: { sha: input.tree }, parents: input.parents.map(parent => ({ sha: parent })) });
          return { data: { sha: id } };
        },
        createRef: async input => {
          state.refs.set(input.ref.slice(5), input.sha);
          return { data: { object: { sha: input.sha } } };
        },
        updateRef: async input => {
          state.refs.set(input.ref, input.sha);
          return { data: { object: { sha: input.sha } } };
        },
        getCommit: async input => ({ data: state.commits.get(input.commit_sha) }),
        getTree: async input => ({ data: { sha: input.tree_sha, truncated: false, tree: state.trees.get(input.tree_sha) } }),
        getBlob: async input => ({ data: { sha: input.file_sha, encoding: "base64", content: state.blobs.get(input.file_sha) } }),
      },
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
  return { github, state };
}

describe("multiple asset deliveries in one Claim", () => {
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
    expect(state.refs.size).toBe(1);
    state.refs.set(`heads/${results[0].branch}`, results[0].commit);
    expect((await verify(0)).verified).toBe(true);
    expect((await verify(1)).verified).toBe(secondPath === "first.png");
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
