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
  const state = { bad: false, report: null, blob: null, tree: null, commit: null, ref: null };
  const github = {
    rest: {
      repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }), getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
      git: {
        getRef: async () => {
          if (!state.ref) throw Object.assign(new Error("missing"), { status: 404 });
          return { data: { object: { sha: state.bad ? "changed" : state.ref } } };
        },
        createBlob: async input => {
          state.blob = input.content;
          return { data: { sha: "b".repeat(40) } };
        },
        createTree: async input => {
          state.tree = input.tree;
          return { data: { sha: "c".repeat(40) } };
        },
        createCommit: async input => {
          state.commit = { sha: "d".repeat(40), tree: { sha: input.tree }, parents: input.parents };
          return { data: { sha: state.commit.sha } };
        },
        createRef: async input => {
          state.ref = input.sha;
          return { data: { object: { sha: input.sha } } };
        },
        getCommit: async () => ({ data: state.commit }),
        getTree: async input => ({ data: { sha: input.tree_sha, truncated: false, tree: state.tree } }),
        getBlob: async input => ({ data: { sha: input.file_sha, encoding: "base64", content: state.blob } }),
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
