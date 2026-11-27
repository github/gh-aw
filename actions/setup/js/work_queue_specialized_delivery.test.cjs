import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const scope = require("./work_queue_claim_scope.cjs");
const coverage = require("./work_queue_code_coverage.cjs");
const scanning = require("./work_queue_code_scanning.cjs");

const assignment = { dispatch_id: "dispatch", claims: [{ handle: "h1", claim_id: "claim", work_id: "work", work: {}, result_refs: [] }] };
const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true, state: "open" });
let root;
let environment;

beforeEach(() => {
  environment = { ...process.env };
  for (const key of ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_WORK_QUEUE_SNAPSHOT", "GH_AW_SAFE_OUTPUTS_STAGED"]) delete process.env[key];
  Object.assign(process.env, { GITHUB_REPOSITORY: "owner/repo", GITHUB_SHA: "a".repeat(40) });
  root = fs.mkdtempSync(path.join(os.tmpdir(), "specialized-uploads-"));
  const directory = scope.claimArtifactPath(root, "h1", assignment);
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(path.join(directory, "coverage.info"), "coverage bytes");
});

afterEach(() => {
  process.env = environment;
  fs.rmSync(root, { recursive: true, force: true });
  vi.restoreAllMocks();
});

function client(status) {
  const reads = [];
  const github = {
    rest: {
      repos: { get: async () => ({ data: { id: 7, full_name: "owner/repo" } }), getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
      codeScanning: {
        uploadSarif: async () => ({ data: { id: "sarif-1" } }),
        getSarif: async input => {
          reads.push(input.sarif_id);
          return { data: { processing_status: status } };
        },
      },
    },
    request: async (route, input) => {
      if (route.startsWith("PUT ")) return { data: { id: "coverage-1" } };
      reads.push(input.report_id);
      return { data: { processing_status: status } };
    },
  };
  return { github, reads };
}

async function upload(kind, status) {
  const { github, reads } = client(status);
  const effects = [];
  const run = () =>
    scope.withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
      if (kind === "code_coverage") {
        const handler = await coverage.main({ "target-ref": "refs/heads/main", "coverage-dir": root }, github);
        return handler({ type: "upload_code_coverage", file: "coverage.info", language: "c", label: "coverage" });
      }
      const handler = await scanning.main({ "target-repo": "owner/repo", "target-ref": "refs/heads/main" }, github);
      return handler({ type: "create_code_scanning_alert", file: "src/file.js", line: 1, severity: "warning", message: "finding" });
    });
  return { run, reads, effects };
}

describe.each([
  ["code_coverage", "code_coverage", "processing", "succeeded", "failed"],
  ["sarif", "code_scanning", "pending", "complete", "failed"],
])("%s upload status", (effectKind, kind, pending, done, failed) => {
  it("reads status once and marks a still-processing upload unverified without timers", async () => {
    const timeout = vi.spyOn(globalThis, "setTimeout");
    const { run, reads, effects } = await upload(kind, pending);
    expect((await run()).success).toBe(true);
    expect(reads).toHaveLength(1);
    expect(timeout).not.toHaveBeenCalled();
    const recorded = effects.filter(effect => effect.kind === effectKind);
    expect(recorded.length).toBeGreaterThan(0);
    expect(recorded.every(effect => effect.status === "unverified")).toBe(true);
  });

  it("succeeds without marking when processing finished", async () => {
    const { run, reads, effects } = await upload(kind, done);
    expect((await run()).success).toBe(true);
    expect(reads).toHaveLength(1);
    expect(effects.some(effect => effect.status === "unverified")).toBe(false);
  });

  it("fails on a definitive failure status", async () => {
    const { run, reads } = await upload(kind, failed);
    await expect(run()).rejects.toThrow(/failed processing/);
    expect(reads).toHaveLength(1);
  });
});
