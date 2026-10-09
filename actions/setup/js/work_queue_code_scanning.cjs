// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — runtime repository fallbacks are checked by assertClaimAuthorized against authorizeWorkerClaim's profile.effect_scope allowlist (E004); the guarded effect client reauthorizes SARIF writes.

const crypto = require("crypto");
const path = require("path");
const zlib = require("zlib");
const { canonical } = require("./work_queue_codec.cjs");
const {
  currentClaimHandle,
  assertClaimAuthorized,
  claimArtifactPath,
  claimIdentity,
  assertClaimIdentity,
  receiptMatchesClaim,
  withClaimResourceEffects,
  createClaimResourceVerification,
  withClaimResourceVerification,
} = require("./work_queue_claim_scope.cjs");
const { resolveRepositoryTarget, canonicalResourceTarget } = require("./work_queue_effect_resource.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const privateReceipts = new WeakMap();

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
    if (typeof getCommit !== "function" || typeof uploadSarif !== "function") throw new Error("Queue code scanning requires its native read and write endpoints");
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
    const result = { success: true, sarif_id: upload.data.id, repo: repository, ref, commit_sha: revision };
    privateReceipts.set(result, { ...factoryIdentity, repository, revision, ref, driver, category, finding, sarif_id: upload.data.id, authorityResource });
    return result;
  };
}

async function verifyCodeScanningDelivery(options) {
  const { claim, result, github } = options;
  const receipt = result && privateReceipts.get(result);
  if (!receiptMatchesClaim(receipt, claim)) return { verified: false };
  const [owner, repo] = receipt.repository.split("/");
  let status;
  for (let attempt = 0; attempt < 8; attempt++) {
    status = (await github.rest.codeScanning.getSarif({ owner, repo, sarif_id: receipt.sarif_id })).data;
    if (status.processing_status !== "pending") break;
    if (attempt < 7) await new Promise(resolve => setTimeout(resolve, 500));
  }
  if (status.processing_status !== "complete" || status.errors?.length) return { verified: false };
  const url = new URL(status.analyses_url);
  const base = new URL(process.env.GITHUB_API_URL || "https://api.github.com");
  if (url.origin !== base.origin || url.pathname !== `${base.pathname.replace(/\/$/, "")}/repos/${receipt.repository}/code-scanning/analyses` || url.searchParams.get("sarif_id") !== receipt.sarif_id) return { verified: false };
  const { data: analyses } = await github.request(`GET ${url.href}`);
  if (!Array.isArray(analyses) || analyses.length !== 1) return { verified: false };
  const analysis = analyses[0];
  if (
    analysis.sarif_id !== receipt.sarif_id ||
    analysis.commit_sha !== receipt.revision ||
    analysis.ref !== receipt.ref ||
    analysis.category !== receipt.category ||
    analysis.tool?.name !== receipt.driver ||
    analysis.error ||
    analysis.results_count !== 1 ||
    !Number.isSafeInteger(analysis.id)
  )
    return { verified: false };
  const { data: report } = await github.rest.codeScanning.getAnalysis({ owner, repo, analysis_id: analysis.id, headers: { accept: "application/sarif+json" } });
  if (report.version !== "2.1.0" || report.runs?.length !== 1 || report.runs[0].tool?.driver?.name !== receipt.driver || report.runs[0].results?.length !== 1) return { verified: false };
  const finding = report.runs[0].results[0];
  if (finding.locations?.length !== 1) return { verified: false };
  const location = finding.locations[0].physicalLocation;
  const projected = {
    ruleId: finding.ruleId,
    message: { text: finding.message?.text },
    level: finding.level,
    locations: [{ physicalLocation: { artifactLocation: { uri: location?.artifactLocation?.uri }, region: { startLine: location?.region?.startLine, startColumn: location?.region?.startColumn } } }],
  };
  if (canonical(projected) !== canonical(receipt.finding)) return { verified: false };
  const proof = createClaimResourceVerification({
    verified: true,
    claim_handle: claim.handle,
    resource: {
      kind: "sarif",
      repository: receipt.repository,
      id: receipt.sarif_id,
      analysis_id: analysis.id,
      ref: receipt.ref,
      commit_sha: receipt.revision,
      category: receipt.category,
      path: receipt.finding.locations[0].physicalLocation.artifactLocation.uri,
    },
    authority_resource: await resolveRepositoryTarget(github, receipt.authorityResource),
    evidence: { source: "github_code_scanning_api", sarif_id: receipt.sarif_id, analysis_id: analysis.id, result_sha256: crypto.createHash("sha256").update(canonical(receipt.finding)).digest("hex") },
  });
  return withClaimResourceVerification(proof, () => proof, { authorize: options.authorize, context: options.context, github });
}

module.exports = { main, verifyCodeScanningDelivery };
