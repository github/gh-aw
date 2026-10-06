"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const zlib = require("node:zlib");
const { withClaimExecution, claimArtifactPath, readClaimScopeContext, assertClaimArtifactFile } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { verifyClaimDelivery } = require("./work_queue_delivery.cjs");
const scanning = require("./work_queue_code_scanning.cjs");
const coverage = require("./work_queue_code_coverage.cjs");

function assigned(type) {
  return {
    version: 3,
    dispatch_id: "dispatch",
    request_id: "request",
    commit_id: "commit",
    policy_epoch: "epoch",
    pool: "default",
    worker_profile: "default",
    claims: ["h1", "h2"].map(handle => {
      /** @type {Record<string, unknown>} */
      const effect_contract = { version: 1, outputs: [{ type, min: 1, max: 1 }] };
      return { handle, claim_id: `claim:${handle}`, work_id: `work:${handle}`, work: { effect_contract }, result_refs: [] };
    }),
  };
}

async function nativeEnvironment(callback) {
  const keys = ["GITHUB_SHA", "GITHUB_REPOSITORY"];
  const original = keys.map(key => process.env[key]);
  process.env.GITHUB_SHA = "a".repeat(40);
  process.env.GITHUB_REPOSITORY = "owner/repo";
  try {
    await callback();
  } finally {
    keys.forEach((key, index) => (original[index] === undefined ? delete process.env[key] : (process.env[key] = original[index])));
  }
}

function registerTests({ describe, it }) {
  describe("trusted standalone native Claim adapters", () => {
    it("rejects symlinked Claim roots and parent directories rather than consuming a sibling artifact", async () => {
      const root = path.resolve(".queue-validation-cache", `queue-isolation-${randomUUID()}`);
      const assignment = assigned("noop");
      const first = claimArtifactPath(root, "h1", assignment);
      const sibling = claimArtifactPath(root, "h2", assignment);
      try {
        fs.mkdirSync(first, { recursive: true });
        fs.mkdirSync(sibling, { recursive: true });
        fs.writeFileSync(path.join(first, "own.json"), "{}");
        fs.writeFileSync(path.join(sibling, "sibling.json"), "{}");
        await withClaimExecution({ assignment, claim_handle: "h1" }, async () => {
          assertClaimArtifactFile(path.join(first, "own.json"), first);
          fs.symlinkSync(sibling, path.join(first, "redirect"));
          assert.throws(() => assertClaimArtifactFile(path.join(first, "redirect", "sibling.json"), first), /escapes|redirects/);
          fs.rmSync(first, { recursive: true });
          fs.symlinkSync(sibling, first);
          assert.throws(() => assertClaimArtifactFile(path.join(first, "sibling.json"), first), /redirects/);
        });
      } finally {
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("uses only explicit closed current snapshots and leaves non-queue runs unaffected", () => {
      const root = path.resolve(".queue-validation-cache", `queue-snapshot-${randomUUID()}`);
      const keys = ["GH_AW_WORK_QUEUE_ENABLED", "GH_AW_WORK_QUEUE_SNAPSHOT"];
      const previous = keys.map(key => process.env[key]);
      try {
        fs.mkdirSync(root, { recursive: true });
        const filename = path.join(root, "snapshot.json");
        process.env.GH_AW_WORK_QUEUE_SNAPSHOT = filename;
        const snapshot = { version: 3, sha: "a".repeat(40), transactionLog: [], captured_at: 0, origin: {}, worker: assigned("noop") };
        fs.writeFileSync(filename, JSON.stringify(snapshot));
        process.env.GH_AW_WORK_QUEUE_ENABLED = "true";
        const scope = readClaimScopeContext();
        assert.ok(scope);
        assert.equal(scope.assignment.claims.length, 2);
        fs.writeFileSync(filename, JSON.stringify({ ...snapshot, version: 2 }));
        assert.throws(readClaimScopeContext, /current closed version-3/);
        fs.writeFileSync(filename, JSON.stringify({ ...snapshot, worker: { assignment: snapshot.worker } }));
        assert.throws(readClaimScopeContext, /immutable array/);
        delete process.env.GH_AW_WORK_QUEUE_ENABLED;
        assert.equal(readClaimScopeContext(), null);
      } finally {
        keys.forEach((key, index) => (previous[index] === undefined ? delete process.env[key] : (process.env[key] = previous[index])));
        fs.rmSync(root, { recursive: true, force: true });
      }
    });

    it("rejects actual writes under an immutable no-write Work contract before the API call", async () => {
      const assignment = assigned("noop");
      assignment.claims[0].work.effect_contract = { kind: "none" };
      let writes = 0;
      const source = {
        rest: {
          issues: {
            create: async () => {
              writes++;
            },
          },
        },
      };
      const authorize = async request => ({ claim_handle: request.claim_handle, authorized: true });
      await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
        const client = wrapClaimEffectClient(source, { claim_handle: "h1", authorize });
        await assert.rejects(client.rest.issues.create({ owner: "owner", repo: "repo", title: "forbidden" }), /effect_contract prohibits/);
        assert.equal(writes, 0);
      });
    });

    it("verifies exact SARIF analysis, immutable revision, category and finding instead of job success", () =>
      nativeEnvironment(async () => {
        const assignment = assigned("create_code_scanning_alert");
        const effects = [];
        let report;
        let incorrect = false;
        let writes = 0;
        let originalResult;
        let originalHandler;
        let originalCategory;
        const source = {
          rest: {
            repos: { getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) },
            codeScanning: {
              uploadSarif: async input => {
                writes++;
                report = JSON.parse(zlib.gunzipSync(Buffer.from(input.sarif, "base64")).toString("utf8"));
                return { data: { id: "upload-1" } };
              },
              getSarif: async () => ({ data: { processing_status: "complete", analyses_url: "https://api.github.com/repos/owner/repo/code-scanning/analyses?sarif_id=upload-1", errors: [] } }),
              getAnalysis: async () => ({ data: incorrect ? { ...report, runs: [] } : report }),
            },
          },
          request: async () => ({
            data: [
              {
                sarif_id: "upload-1",
                id: 42,
                commit_sha: process.env.GITHUB_SHA,
                ref: "refs/heads/approved",
                category: report.runs[0].automationDetails.id,
                tool: { name: report.runs[0].tool.driver.name },
                results_count: 1,
                error: "",
              },
            ],
          }),
        };
        const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === undefined || request.message.repo === "owner/repo" });
        await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
          const handler = await scanning.main({ "target-ref": "refs/heads/approved" }, wrapClaimEffectClient(source, { claim_handle: "h1", authorize }));
          const message = { type: "create_code_scanning_alert", claim_handle: "h1", file: "src/one.js", line: 4, message: "Scoped finding", severity: "warning" };
          const result = await handler(message);
          originalResult = result;
          originalHandler = handler;
          originalCategory = report.runs[0].automationDetails.id;
          assert.equal(writes, 1);
          const options = { assignment, claim_handle: "h1", messages: [message], results: [{ messageIndex: 0, success: true, result }], effects, authorize, github: source, verifyOutput: scanning.verifyCodeScanningDelivery };
          assert.equal((await verifyClaimDelivery(options)).verification, "verified");
          assert.equal((await scanning.verifyCodeScanningDelivery({ claim: assignment.claims[0], result: structuredClone(result), github: source })).verified, false);
          incorrect = true;
          assert.equal((await verifyClaimDelivery(options)).verification, "unknown");
          await assert.rejects(handler({ ...message, repo: "foreign/repo" }), /conflicts/);
          await assert.rejects(handler({ ...message, claim_handle: "h2" }), /cannot escape/);
        });
        const next = structuredClone(assignment);
        next.dispatch_id = "next-dispatch";
        next.claims[0].claim_id = "next-claim";
        incorrect = false;
        await withClaimExecution({ assignment: next, claim_handle: "h1", authorize, effects: [] }, async () => {
          assert.equal((await scanning.verifyCodeScanningDelivery({ claim: next.claims[0], result: originalResult, github: source })).verified, false);
          const message = { type: "create_code_scanning_alert", claim_handle: "h1", file: "src/one.js", line: 4, message: "Scoped finding", severity: "warning" };
          await assert.rejects(originalHandler(message), /original immutable Claim identity/);
          const handler = await scanning.main({ "target-ref": "refs/heads/approved" }, wrapClaimEffectClient(source, { claim_handle: "h1", authorize }));
          await handler(message);
          assert.notEqual(report.runs[0].automationDetails.id, originalCategory);
        });
      }));

    it("requires exact private coverage upload bytes plus independently successful service processing", () =>
      nativeEnvironment(async () => {
        const root = path.resolve(".queue-validation-cache", `queue-coverage-${randomUUID()}`);
        const assignment = assigned("upload_code_coverage");
        const effects = [];
        const bytes = Buffer.from("TN:\nSF:src/file.c\nDA:1,1\nend_of_record\n");
        let incorrect = false;
        let writes = 0;
        const labels = [];
        let originalResult;
        let originalHandler;
        const source = {
          rest: { repos: { getCommit: async () => ({ data: { sha: process.env.GITHUB_SHA } }) } },
          request: async (route, input) => {
            if (route.startsWith("PUT ")) {
              writes++;
              assert.deepEqual(zlib.gunzipSync(Buffer.from(input.coverage_report, "base64")), bytes);
              assert.equal(input.commit_oid, process.env.GITHUB_SHA);
              assert.match(input.label, /\/claim\/[a-f0-9]{64}$/);
              labels.push(input.label);
              return { data: { id: "report-1" } };
            }
            assert.equal(input.report_id, "report-1");
            return { data: { processing_status: incorrect ? "failed" : "succeeded", errors: [] } };
          },
        };
        const authorize = async request => ({ claim_handle: request.claim_handle, authorized: request.message.repo === undefined || request.message.repo === "owner/repo" });
        try {
          const directory = claimArtifactPath(root, "h1", assignment);
          fs.mkdirSync(directory, { recursive: true });
          fs.writeFileSync(path.join(directory, "coverage.info"), bytes);
          await withClaimExecution({ assignment, claim_handle: "h1", authorize, effects }, async () => {
            const handler = await coverage.main({ "target-ref": "refs/heads/approved", "coverage-dir": root, "wait-for-processing-timeout": 0 }, wrapClaimEffectClient(source, { claim_handle: "h1", authorize }));
            const message = { type: "upload_code_coverage", claim_handle: "h1", file: "coverage.info", language: "c", label: "native" };
            const result = await handler(message);
            originalResult = result;
            originalHandler = handler;
            assert.equal(writes, 1);
            const options = { assignment, claim_handle: "h1", messages: [message], results: [{ messageIndex: 0, success: true, result }], effects, authorize, github: source, verifyOutput: coverage.verifyCodeCoverageDelivery };
            assert.equal((await verifyClaimDelivery(options)).verification, "verified");
            assert.equal((await coverage.verifyCodeCoverageDelivery({ claim: assignment.claims[0], result: structuredClone(result), github: source })).verified, false);
            incorrect = true;
            assert.equal((await verifyClaimDelivery(options)).verification, "unknown");
            await assert.rejects(handler({ ...message, repo: "foreign/repo" }), /conflicts/);
          });
          const next = structuredClone(assignment);
          next.dispatch_id = "next-dispatch";
          next.claims[0].claim_id = "next-claim";
          const nextDirectory = claimArtifactPath(root, "h1", next);
          fs.mkdirSync(nextDirectory, { recursive: true });
          fs.writeFileSync(path.join(nextDirectory, "coverage.info"), bytes);
          incorrect = false;
          await withClaimExecution({ assignment: next, claim_handle: "h1", authorize, effects: [] }, async () => {
            assert.equal((await coverage.verifyCodeCoverageDelivery({ claim: next.claims[0], result: originalResult, github: source })).verified, false);
            const message = { type: "upload_code_coverage", claim_handle: "h1", file: "coverage.info", language: "c", label: "native" };
            await assert.rejects(originalHandler(message), /original immutable Claim identity/);
            const handler = await coverage.main({ "target-ref": "refs/heads/approved", "coverage-dir": root, "wait-for-processing-timeout": 0 }, wrapClaimEffectClient(source, { claim_handle: "h1", authorize }));
            await handler(message);
            assert.equal(labels.length, 2);
            assert.notEqual(labels[0], labels[1]);
          });
        } finally {
          fs.rmSync(root, { recursive: true, force: true });
        }
      }));

    it("previews remain bound and scoped without requiring Completion or invoking a standalone upload", () =>
      nativeEnvironment(async () => {
        for (const { type, module } of [
          { type: "create_code_scanning_alert", module: scanning },
          { type: "upload_code_coverage", module: coverage },
        ]) {
          const assignment = assigned(type);
          let checked = 0;
          const authorize = async request => {
            checked++;
            assert.equal(request.requireCompletion, false);
            return { claim_handle: request.claim_handle, authorized: true };
          };
          await withClaimExecution({ assignment, claim_handle: "h1", authorize }, async () => {
            const handler = await module.main({ staged: true, "target-ref": "refs/heads/approved" }, {});
            const result = await handler({ type, claim_handle: "h1", file: "report" });
            assert.equal(result.staged, true);
            assert.equal(checked, 1);
          });
        }
      }));
  });
}

module.exports = { registerTests };
if (require.main === module) registerTests(require("node:test"));
