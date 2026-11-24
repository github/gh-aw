// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — runtime repository fallbacks are checked by assertClaimAuthorized against authorizeWorkerClaim's profile.effect_scope allowlist (E004); the guarded effect client reauthorizes SARIF writes.

const path = require("path");
const zlib = require("zlib");
const { currentClaimHandle, assertClaimAuthorized, claimArtifactPath, claimIdentity, assertClaimIdentity, withClaimResourceEffects, markClaimEffects } = require("./work_queue_claim_scope.cjs");
const { resolveRepositoryTarget, canonicalResourceTarget } = require("./work_queue_effect_resource.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

/** @param {{ "target-repo"?: string, "target-ref"?: string, driver?: string, max?: number, workflow_filename?: string, staged?: boolean }} [config] @param {import("./types/work-queue-native-client").ClaimNativeClient} [suppliedClient] */
async function main(config = {}, suppliedClient) {
  const handle = currentClaimHandle();
  if (!handle) throw new Error(`${SAFE_OUTPUT_E001}: Queue code scanning requires a Claim factory`);
  const factoryIdentity = claimIdentity(handle);
  /** @type {import("./types/work-queue-native-client").ClaimNativeClient} */
  const selectedClient = suppliedClient || global.github;
  const github = wrapClaimEffectClient(selectedClient, { claim_handle: handle });
  const repository = config["target-repo"] || process.env.GITHUB_REPOSITORY;
  if (typeof repository !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("Queue code scanning requires its trusted repository destination");
  const [owner, repo] = repository.split("/");
  const ref = config["target-ref"];
  const revision = process.env.GITHUB_SHA;
  const driver = config.driver || "GitHub Agentic Workflows Security Scanner";
  const prefix = `gh-aw/claims/${path.basename(claimArtifactPath("", handle))}`;
  const maximum = Number(config.max || 128);
  let count = 0;
  const stagedMode = isStagedMode(config);
  if (typeof revision !== "string" || typeof ref !== "string" || !/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/.test(revision) || !/^refs\/(?:heads|tags)\/[A-Za-z0-9._/-]+$/.test(ref))
    throw new Error("Queue code scanning requires a trusted ref and immutable native revision");
  return async message => {
    if (currentClaimHandle() !== handle) throw new Error("Queue code scanning cannot escape its original Claim");
    assertClaimIdentity(factoryIdentity);
    if (Object.hasOwn(message, "repo") && message.repo !== repository) throw new Error("Queue code scanning explicit repository conflicts with its trusted adapter");
    message = await assertClaimAuthorized({ ...message, repo: repository }, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: handle };
    const number = ++count;
    if (!Number.isSafeInteger(maximum) || maximum < 1 || number > maximum) throw new Error("Queue code scanning exceeds the trusted per-Claim limit");
    if (
      typeof message.file !== "string" ||
      !message.file ||
      Buffer.byteLength(message.file) > 256 ||
      message.file.startsWith("/") ||
      /[\\\x00-\x1f\x7f]/.test(message.file) ||
      message.file.split("/").some(part => !part || part === "." || part === "..") ||
      !Number.isSafeInteger(message.line) ||
      message.line < 1 ||
      !["error", "warning", "info", "note"].includes(message.severity) ||
      typeof message.message !== "string" ||
      !message.message ||
      message.message.length > 2048 ||
      (message.column !== undefined && (!Number.isSafeInteger(message.column) || message.column < 1))
    )
      throw new Error("Queue code scanning finding is malformed");
    const target = canonicalResourceTarget({ repository, ref, path: message.file });
    const getCommit = github.rest?.repos?.getCommit;
    const uploadSarif = github.rest?.codeScanning?.uploadSarif;
    const getSarif = github.rest?.codeScanning?.getSarif;
    if (typeof getCommit !== "function" || typeof uploadSarif !== "function" || typeof getSarif !== "function") throw new Error("Queue code scanning requires its native read and write endpoints");
    const resolved = await getCommit({ owner, repo, ref });
    if (resolved.data.sha !== revision) throw new Error("Trusted code scanning ref differs from the immutable worker revision");
    const category = `${prefix}/${number}`;
    const finding = {
      ruleId: `${config.workflow_filename || "workflow"}-${message.ruleIdSuffix || "security-finding"}`,
      message: { text: message.message },
      level: message.severity === "info" ? "note" : message.severity,
      locations: [{ physicalLocation: { artifactLocation: { uri: message.file }, region: { startLine: message.line, startColumn: message.column || 1 } } }],
    };
    const report = {
      version: "2.1.0",
      $schema: "https://json.schemastore.org/sarif-2.1.0.json",
      runs: [{ tool: { driver: { name: driver, version: "1.0.0" } }, automationDetails: { id: category }, results: [finding] }],
    };
    const authorityResource = await resolveRepositoryTarget(github, target);
    const upload = await withClaimResourceEffects([authorityResource], ["rest.codeScanning.uploadSarif"], () =>
      uploadSarif({ owner, repo, commit_sha: revision, ref, sarif: zlib.gzipSync(Buffer.from(JSON.stringify(report))).toString("base64"), tool_name: driver, validate: true })
    );
    if (typeof upload.data.id !== "string" || !upload.data.id) throw new Error("Queue code scanning has no exact service receipt");
    // One status read, no polling: still-pending uploads are recorded as unverified.
    const { data: status } = await getSarif({ owner, repo, sarif_id: upload.data.id });
    if (status?.processing_status === "pending") markClaimEffects("sarif", { status: "unverified" });
    else if (status?.processing_status !== "complete" || status.errors?.length) throw new Error(`Queue code scanning SARIF ${upload.data.id} failed processing`);
    return { success: true, sarif_id: upload.data.id, repo: repository, ref, commit_sha: revision };
  };
}

module.exports = { main };
