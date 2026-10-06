// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const zlib = require("zlib");
const { currentClaimHandle, assertClaimAuthorized, claimArtifactPath, assertClaimArtifactFile, claimIdentity, assertClaimIdentity, receiptMatchesClaim } = require("./work_queue_claim_scope.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const privateReceipts = new WeakMap();

/** @param {Record<string, any>} [config] @param {any} [suppliedClient] */
async function main(config = {}, suppliedClient) {
  const handle = currentClaimHandle();
  if (!handle) throw new Error("Queue coverage requires a Claim factory");
  const factoryIdentity = claimIdentity(handle);
  const github = suppliedClient || global.github;
  const repository = process.env.GITHUB_REPOSITORY;
  if (typeof repository !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("Queue coverage requires its trusted native repository");
  const [owner, repo] = repository.split("/");
  const ref = config["target-ref"];
  const revision = process.env.GITHUB_SHA;
  const root = claimArtifactPath(config["coverage-dir"] || path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-code-coverage"), handle);
  const limit = Number(config.max || 1);
  let count = 0;
  const stagedMode = isStagedMode(config);
  if (!/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/.test(revision || "") || !/^refs\/(?:heads|tags)\/[A-Za-z0-9._/-]+$/.test(ref || "")) throw new Error("Queue coverage requires a trusted ref and immutable native revision");
  return async message => {
    if (currentClaimHandle() !== handle) throw new Error("Queue coverage cannot escape its original Claim");
    assertClaimIdentity(factoryIdentity);
    if (Object.hasOwn(message, "repo") && message.repo !== repository) throw new Error("Queue coverage explicit repository conflicts with its trusted adapter");
    message = await assertClaimAuthorized({ ...message, repo: repository }, { requireCompletion: !stagedMode, resource: { repository, ref, path: message.file } });
    if (stagedMode) return { success: true, staged: true, claim_handle: handle };
    if (!Number.isSafeInteger(limit) || limit < 1 || ++count > limit) throw new Error("Queue coverage exceeds its trusted per-Claim maximum");
    if (
      typeof message.file !== "string" ||
      path.basename(message.file) !== message.file ||
      !message.file ||
      message.file === "." ||
      message.file === ".." ||
      typeof message.language !== "string" ||
      !message.language ||
      message.language.length > 128 ||
      typeof message.label !== "string" ||
      !message.label ||
      message.label.length > 128
    )
      throw new Error("Queue coverage requires declared isolated report metadata");
    const resolved = await github.rest.repos.getCommit({ owner, repo, ref });
    if (resolved.data.sha !== revision) throw new Error("Trusted coverage target differs from the immutable worker revision");
    const filename = path.join(root, message.file);
    assertClaimArtifactFile(filename, root);
    const fd = fs.openSync(filename, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
    let bytes;
    try {
      const stat = fs.fstatSync(fd);
      if (!stat.isFile() || stat.nlink !== 1 || stat.size > 10 * 1024 * 1024) throw new Error("Queue coverage report is not a bounded isolated regular file");
      bytes = fs.readFileSync(fd);
    } finally {
      fs.closeSync(fd);
    }
    const sha256 = crypto.createHash("sha256").update(bytes).digest("hex");
    const label = `${message.label}/claim/${path.basename(claimArtifactPath("", handle))}`;
    const { data } = await github.request("PUT /repos/{owner}/{repo}/code-coverage/report", {
      owner,
      repo,
      commit_oid: revision,
      ref,
      language_name: message.language,
      label,
      coverage_report: zlib.gzipSync(bytes).toString("base64"),
    });
    if (typeof data.id !== "string" || !/^[A-Za-z0-9_-]{1,256}$/.test(data.id)) throw new Error("Queue coverage has no exact service upload receipt");
    const result = { success: true, report_id: data.id, repo: repository, ref, commit_sha: revision, sha256 };
    privateReceipts.set(result, { ...factoryIdentity, repository, ref, revision, language: message.language, label, sha256, report_id: data.id, timeout: config["wait-for-processing-timeout"] ?? 160 });
    return result;
  };
}

async function verifyCodeCoverageDelivery({ claim, result, github }) {
  const receipt = result && privateReceipts.get(result);
  if (!receiptMatchesClaim(receipt, claim) || !Number.isSafeInteger(receipt.timeout) || receipt.timeout < 0 || receipt.timeout > 600) return { verified: false };
  const [owner, repo] = receipt.repository.split("/");
  const deadline = Date.now() + receipt.timeout * 1000;
  let status;
  do {
    status = (await github.request("GET /repos/{owner}/{repo}/code-coverage/reports/{report_id}", { owner, repo, report_id: receipt.report_id })).data;
    if (!["pending", "processing"].includes(status.processing_status)) break;
    if (Date.now() >= deadline) return { verified: false };
    await new Promise(resolve => setTimeout(resolve, Math.min(2000, deadline - Date.now())));
  } while (Date.now() <= deadline);
  if (status.processing_status !== "succeeded" || status.errors?.length) return { verified: false };
  return {
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "code_coverage", repository: receipt.repository, id: receipt.report_id, ref: receipt.ref, commit_sha: receipt.revision, label: receipt.label, sha256: receipt.sha256 },
    evidence: { source: "github_code_coverage_api", report_id: receipt.report_id, processing_status: "succeeded", request_sha256: receipt.sha256, language: receipt.language },
  };
}

module.exports = { main, verifyCodeCoverageDelivery };
