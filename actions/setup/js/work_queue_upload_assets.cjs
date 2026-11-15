// @ts-check
"use strict";
const { SAFE_OUTPUT_E001 } = require("./error_codes.cjs");
// @safe-outputs-exempt SEC-005 — runtime repository fallbacks are checked by assertClaimAuthorized against authorizeWorkerClaim's profile.effect_scope allowlist (E004); the guarded effect client reauthorizes native writes.

const path = require("path");
const crypto = require("crypto");
const {
  assertClaimAuthorized,
  currentClaimHandle,
  claimArtifactPath,
  assertClaimArtifactFile,
  claimIdentity,
  assertClaimIdentity,
  receiptMatchesClaim,
  withClaimResourceEffects,
  createClaimResourceVerification,
  withClaimResourceVerification,
  claimDeliveryReceipt,
  durableReceipt,
  createClaimEffectBatch,
  withClaimEffectBatch,
  discardClaimEffectBatch,
} = require("./work_queue_claim_scope.cjs");
const { resolveRepositoryTarget } = require("./work_queue_effect_resource.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { readGuardedFile } = require("./symlink_guard.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("assets");

const receipts = new WeakMap();
const MAX_PUBLICATION_ATTEMPTS = 4;

function rejectedRefConflict(error) {
  return (error?.status === 422 && /not a fast[ -]forward/i.test(error.message || "")) || (error?.status === 409 && /expected.*head|head.*changed|reference update failed/i.test(error.message || ""));
}

/** @param {{ "target-repo"?: string, "assets-dir"?: string, "allowed-exts"?: string[], "max-size"?: number, max?: number, branch?: string, staged?: boolean }} [config] @param {import("./types/work-queue-native-client").ClaimNativeClient} [suppliedClient] */
async function main(config = {}, suppliedClient) {
  const factoryClaim = currentClaimHandle();
  if (!factoryClaim) throw new Error(`${SAFE_OUTPUT_E001}: Queue asset adapter requires an immutable Claim factory`);
  const factoryIdentity = claimIdentity(factoryClaim);
  /** @type {import("./types/work-queue-native-client").ClaimNativeClient} */
  const selectedClient = suppliedClient || global.github;
  const client = wrapClaimEffectClient(selectedClient, { claim_handle: factoryClaim });
  const repository = config["target-repo"] || process.env.GITHUB_REPOSITORY || `${global.context.repo.owner}/${global.context.repo.repo}`;
  if (typeof repository !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("Queue asset requires its trusted repository destination");
  const [owner, repo] = repository.split("/");
  const namespace = path.basename(claimArtifactPath("", factoryClaim));
  const branch = "work-queue";
  if (config.branch !== undefined && config.branch !== branch) {
    log.diagnostic("asset.branch_override_ignored", {}, "Work-queue workers always publish assets on the existing work-queue branch; upload-asset.branch applies only to ordinary uploads.");
  }
  const allowed = config["allowed-exts"] || [".png", ".jpg", ".jpeg"];
  const maximum = Number(config["max-size"] || 10240) * 1024;
  const limit = Number(config.max || 10);
  let count = 0;
  const root = claimArtifactPath(config["assets-dir"] || "/tmp/gh-aw/safeoutputs/assets", factoryClaim);
  const stagedMode = isStagedMode(config);
  return async message => {
    if (currentClaimHandle() !== factoryClaim) throw new Error("Queue asset state cannot escape its original Claim");
    assertClaimIdentity(factoryIdentity);
    if (Object.hasOwn(message, "repo") && message.repo !== repository) throw new Error("Queue asset explicit repository conflicts with its trusted adapter");
    message = await assertClaimAuthorized({ ...message, repo: repository }, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: factoryClaim };
    if (!Number.isSafeInteger(limit) || limit < 1 || ++count > limit) throw new Error("Queue asset output exceeds its trusted per-Claim maximum");
    const source = message.path;
    if (typeof source !== "string" || !source) throw new Error("Queue asset requires its original staged source path");
    const extension = path.extname(source).toLowerCase();
    if (!allowed.includes(extension)) throw new Error("Queue asset extension is outside the trusted adapter policy");
    const filename = crypto.createHash("sha256").update(source).digest("hex") + extension;
    const staged = path.join(root, filename);
    assertClaimArtifactFile(staged, root);
    const content = readGuardedFile(staged, maximum);
    const sha256 = crypto.createHash("sha256").update(content).digest("hex");
    if (message.sha !== undefined && message.sha !== sha256) throw new Error("Queue asset staged content differs from its declared digest");
    const assetPath = `claims/${namespace}/${sha256}${extension}`;
    const authorityResource = await resolveRepositoryTarget(client, { repository, ref: `refs/heads/${branch}`, path: assetPath });
    const git = client.rest?.git;
    const getRef = git?.getRef;
    const createBlob = git?.createBlob;
    const createTree = git?.createTree;
    const createCommit = git?.createCommit;
    const updateRef = git?.updateRef;
    const getCommit = git?.getCommit;
    if (typeof getRef !== "function" || typeof createBlob !== "function" || typeof createTree !== "function" || typeof createCommit !== "function" || typeof updateRef !== "function")
      throw new Error("Queue asset requires its native read and write endpoints");
    if (typeof getCommit !== "function") throw new Error("Queue asset requires its native parent commit endpoint");
    const { blob, tree, commit, head } = await withClaimResourceEffects([authorityResource], ["rest.git.createBlob", "rest.git.createTree", "rest.git.createCommit", "rest.git.updateRef"], async () => {
      let blob;
      for (let attempt = 0; attempt < MAX_PUBLICATION_ATTEMPTS; attempt++) {
        let head;
        try {
          const { data } = await getRef({ owner, repo, ref: `heads/${branch}` });
          head = data.object.sha;
        } catch (error) {
          if (error.status !== 404) throw error;
          throw new Error("Queue asset requires the existing work-queue branch");
        }
        const parent = await getCommit({ owner, repo, commit_sha: head });
        if (typeof head !== "string" || !head || typeof parent.data?.tree?.sha !== "string" || !parent.data.tree.sha) throw new Error("Queue asset requires its immutable parent tree");
        blob ||= await createBlob({ owner, repo, content: content.toString("base64"), encoding: "base64" });
        const batch = createClaimEffectBatch();
        const { tree, commit } = await withClaimEffectBatch(batch, async () => {
          const tree = await createTree({ owner, repo, base_tree: parent.data.tree.sha, tree: [{ path: assetPath, mode: "100644", type: "blob", sha: blob.data.sha }] });
          const commit = await createCommit({ owner, repo, message: `Publish Claim ${factoryClaim} asset ${sha256}`, tree: tree.data.sha, parents: [head] });
          return { tree, commit };
        });
        try {
          await withClaimEffectBatch(batch, () => updateRef({ owner, repo, ref: `heads/${branch}`, sha: commit.data.sha, force: false }));
          return { blob, tree, commit, head };
        } catch (error) {
          if (!rejectedRefConflict(error)) throw error;
          // Only a definitive rejected ref write makes this attempt's detached
          // objects safe to omit from delivery; uncertain writes stay fenced.
          discardClaimEffectBatch(batch);
          log.debug("asset.publication_conflict", { attempt: attempt + 1 });
          if (attempt + 1 === MAX_PUBLICATION_ATTEMPTS) throw new Error("Queue asset publication conflicts exhausted; preserve the queue and reconcile the original upload", { cause: error });
        }
      }
      throw new Error("Queue asset publication did not establish a receipt");
    });
    const server = process.env.GITHUB_SERVER_URL || "https://github.com";
    const result = { success: true, repo: repository, branch, path: assetPath, sha: sha256, commit: commit.data.sha, url: `${server}/${repository}/blob/${branch}/${assetPath}?raw=true` };
    receipts.set(result, { ...factoryIdentity, repository, branch, path: assetPath, sha256, blob: blob.data.sha, tree: tree.data.sha, commit: commit.data.sha, parent: head, authorityResource, url: result.url });
    return result;
  };
}

async function verifyAssetDelivery(options) {
  const receipt = claimDeliveryReceipt(options, "asset", receipts);
  return verifyAssetReceipt(options, receipt);
}

function validAssetReceipt(receipt, claim) {
  return (
    receiptMatchesClaim(receipt, claim) &&
    ["repository", "branch", "path", "blob", "tree", "commit", "url"].every(key => typeof receipt[key] === "string" && receipt[key].length > 0) &&
    /^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(receipt.repository) &&
    receipt.branch === "work-queue" &&
    /^[a-f0-9]{64}$/.test(receipt.sha256) &&
    receipt.path === `claims/${path.basename(claimArtifactPath("", claim.handle))}/${receipt.sha256}${path.extname(receipt.path)}` &&
    (receipt.parent === null || (typeof receipt.parent === "string" && receipt.parent.length > 0)) &&
    receipt.authorityResource?.repository === receipt.repository &&
    receipt.authorityResource.ref === `refs/heads/${receipt.branch}` &&
    receipt.authorityResource.path === receipt.path
  );
}

function durableAssetReceipt(result, claim) {
  const receipt = result && receipts.get(result);
  if (!validAssetReceipt(receipt, claim)) return null;
  return durableReceipt(receipt);
}

async function verifyAssetReceipt(options, receipt) {
  const { claim, github } = options;
  if (!validAssetReceipt(receipt, claim)) return { verified: false, diagnostics: { event: "asset.receipt_missing" } };
  const [owner, repo] = receipt.repository.split("/");
  const ref = await github.rest.git.getRef({ owner, repo, ref: `heads/${receipt.branch}` });
  const branchHead = ref.data.object.sha;
  if (typeof branchHead !== "string" || !branchHead) return { verified: false, diagnostics: { event: "asset.ref_mismatch" } };
  const commit = await github.rest.git.getCommit({ owner, repo, commit_sha: receipt.commit });
  if (commit.data.sha !== receipt.commit || commit.data.tree.sha !== receipt.tree) return { verified: false, diagnostics: { event: "asset.commit_mismatch" } };
  if (receipt.parent ? commit.data.parents?.length !== 1 || commit.data.parents[0].sha !== receipt.parent : commit.data.parents?.length !== 0) return { verified: false, diagnostics: { event: "asset.parent_mismatch" } };
  if (!(await treeContainsAsset(github, owner, repo, receipt.tree, receipt.path, receipt.blob))) return { verified: false, diagnostics: { event: "asset.tree_mismatch" } };
  if (branchHead !== receipt.commit) {
    const headCommit = await github.rest.git.getCommit({ owner, repo, commit_sha: branchHead });
    if (headCommit.data?.sha !== branchHead || typeof headCommit.data.tree?.sha !== "string") return { verified: false, diagnostics: { event: "asset.ref_mismatch" } };
    if (!(await treeContainsAsset(github, owner, repo, headCommit.data.tree.sha, receipt.path, receipt.blob))) return { verified: false, diagnostics: { event: "asset.path_missing_from_ref" } };
  }
  const blob = await github.rest.git.getBlob({ owner, repo, file_sha: receipt.blob });
  if (blob.data.sha !== receipt.blob || blob.data.encoding !== "base64" || crypto.createHash("sha256").update(Buffer.from(blob.data.content, "base64")).digest("hex") !== receipt.sha256)
    return { verified: false, diagnostics: { event: "asset.blob_mismatch" } };
  const proof = createClaimResourceVerification({
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "asset", repository: receipt.repository, id: receipt.commit, ref: `heads/${receipt.branch}`, path: receipt.path, sha256: receipt.sha256, url: receipt.url },
    authority_resource: await resolveRepositoryTarget(github, receipt.authorityResource),
    effect_resources: [
      { kind: "git_blob", repository: receipt.repository, id: receipt.blob },
      { kind: "git_tree", repository: receipt.repository, id: receipt.tree },
      { kind: "git_commit", repository: receipt.repository, id: receipt.commit },
      { kind: "git_ref", repository: receipt.repository, id: receipt.commit },
      ...(branchHead === receipt.commit ? [] : [{ kind: "git_ref", repository: receipt.repository, id: branchHead }]),
    ],
    evidence: { source: "github_git_api", commit: receipt.commit, tree: receipt.tree, blob: receipt.blob, branch_head: branchHead, sha256: receipt.sha256 },
  });
  return withClaimResourceVerification(proof, () => proof, { authorize: options.authorize, context: options.context, github });
}

async function treeContainsAsset(github, owner, repo, treeSha, assetPath, blobSha) {
  const segments = assetPath.split("/");
  for (const [index, segment] of segments.entries()) {
    const { data } = await github.rest.git.getTree({ owner, repo, tree_sha: treeSha });
    if (!data || data.sha !== treeSha || data.truncated !== false || !Array.isArray(data.tree)) return false;
    const entries = data.tree.filter(entry => entry.path === segment);
    if (entries.length !== 1) return false;
    const entry = entries[0];
    if (index === segments.length - 1) return entry.type === "blob" && entry.mode === "100644" && entry.sha === blobSha;
    if (entry.type !== "tree" || entry.mode !== "040000" || typeof entry.sha !== "string" || !entry.sha) return false;
    treeSha = entry.sha;
  }
  return false;
}

module.exports = { main, verifyAssetDelivery, durableAssetReceipt };
