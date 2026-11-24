// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const zlib = require("zlib");
const { currentClaimHandle, assertClaimAuthorized, claimArtifactPath, assertClaimArtifactFile, claimIdentity, assertClaimIdentity, withClaimResourceEffects, markClaimEffects } = require("./work_queue_claim_scope.cjs");
const { resolveRepositoryTarget, canonicalResourceTarget } = require("./work_queue_effect_resource.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { authorizeHandlerInvocation } = require("./safe_output_authorization_gate.cjs");

/** @param {{ "target-ref"?: string, "coverage-dir"?: string, max?: number, staged?: boolean }} [config] @param {import("./types/work-queue-native-client").ClaimNativeClient} [suppliedClient] */
async function main(config = {}, suppliedClient) {
  const handle = currentClaimHandle();
  if (!handle) throw new Error(`${SAFE_OUTPUT_E001}: Queue coverage requires a Claim factory`);
  const factoryIdentity = claimIdentity(handle);
  /** @type {import("./types/work-queue-native-client").ClaimNativeClient} */
  const selectedClient = suppliedClient || global.github;
  const github = wrapClaimEffectClient(selectedClient, { claim_handle: handle });
  const repository = process.env.GITHUB_REPOSITORY;
  if (typeof repository !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("Queue coverage requires its trusted native repository");
  const [owner, repo] = repository.split("/");
  const ref = config["target-ref"];
  const revision = process.env.GITHUB_SHA;
  const root = claimArtifactPath(config["coverage-dir"] || path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "upload-code-coverage"), handle);
  const limit = Number(config.max || 1);
  let count = 0;
  const stagedMode = isStagedMode(config);
  if (typeof revision !== "string" || typeof ref !== "string" || !/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/.test(revision) || !/^refs\/(?:heads|tags)\/[A-Za-z0-9._/-]+$/.test(ref))
    throw new Error("Queue coverage requires a trusted ref and immutable native revision");
  return async message => {
    if (currentClaimHandle() !== handle) throw new Error("Queue coverage cannot escape its original Claim");
    assertClaimIdentity(factoryIdentity);
    if (Object.hasOwn(message, "repo") && message.repo !== repository) throw new Error("Queue coverage explicit repository conflicts with its trusted adapter");
    message = await assertClaimAuthorized({ ...message, repo: repository }, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: handle };
    if (!Number.isSafeInteger(limit) || limit < 1 || ++count > limit) throw new Error("Queue coverage exceeds its trusted per-Claim maximum");
    if (
      typeof message.file !== "string" ||
      path.basename(message.file) !== message.file ||
      !message.file ||
      message.file === "." ||
      message.file === ".." ||
      Buffer.byteLength(message.file) > 256 ||
      /[\\\x00-\x1f\x7f]/.test(message.file) ||
      typeof message.language !== "string" ||
      !message.language ||
      message.language.length > 128 ||
      typeof message.label !== "string" ||
      !message.label ||
      message.label.length > 128
    )
      throw new Error("Queue coverage requires declared isolated report metadata");
    const target = canonicalResourceTarget({ repository, ref, path: message.file });
    const getCommit = github.rest?.repos?.getCommit;
    const request = github.request;
    if (typeof getCommit !== "function" || typeof request !== "function") throw new Error("Queue coverage requires its native read and write endpoints");
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
    const resolved = await getCommit({ owner, repo, ref });
    if (resolved.data.sha !== revision) throw new Error("Trusted coverage target differs from the immutable worker revision");
    const sha256 = crypto.createHash("sha256").update(bytes).digest("hex");
    const label = `${message.label}/claim/${path.basename(claimArtifactPath("", handle))}`;
    const authorityResource = await resolveRepositoryTarget(github, target);
    const authorization = await authorizeHandlerInvocation({
      config,
      inputs: { repository, ref, revision, file: message.file, language: message.language, label, sha256 },
      scope: { binding: "invocation", refs: [`sha256:${sha256}`] },
    });
    if (authorization.state === "allowed" && (await getCommit({ owner, repo, ref })).data.sha !== revision) {
      throw new Error("Trusted coverage target changed after safe-output authorization");
    }
    const { data } = await withClaimResourceEffects([authorityResource], ["request:PUT /repos/{owner}/{repo}/code-coverage/report"], () =>
      request("PUT /repos/{owner}/{repo}/code-coverage/report", {
        owner,
        repo,
        commit_oid: revision,
        ref,
        language_name: message.language,
        label,
        coverage_report: zlib.gzipSync(bytes).toString("base64"),
      })
    );
    if (!data || typeof data !== "object" || !("id" in data) || typeof data.id !== "string" || !/^[A-Za-z0-9_-]{1,256}$/.test(data.id)) throw new Error("Queue coverage has no exact service upload receipt");
    // One status read, no polling: still-processing uploads are recorded as unverified.
    const { data: statusData } = await request("GET /repos/{owner}/{repo}/code-coverage/reports/{report_id}", { owner, repo, report_id: data.id });
    // prettier-ignore
    const status = /** @type {{processing_status?: string, errors?: unknown[] | null} | null | undefined} */ (statusData);
    if (status?.processing_status === "pending" || status?.processing_status === "processing") markClaimEffects("code_coverage", { status: "unverified" });
    else if (status?.processing_status !== "succeeded" || status.errors?.length) throw new Error(`Queue coverage report ${data.id} failed processing`);
    return { success: true, report_id: data.id, repo: repository, ref, commit_sha: revision, sha256 };
  };
}

module.exports = { main };
