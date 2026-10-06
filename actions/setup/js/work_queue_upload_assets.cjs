// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { assertClaimAuthorized, currentClaimHandle, claimArtifactPath, assertClaimArtifactFile, claimIdentity, assertClaimIdentity, receiptMatchesClaim } = require("./work_queue_claim_scope.cjs");
const { normalizeBranchName } = require("./normalize_branch_name.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

const receipts = new WeakMap();

/** @param {Record<string, any>} [config] @param {any} [suppliedClient] */
async function main(config = {}, suppliedClient) {
  const factoryClaim = currentClaimHandle();
  if (!factoryClaim) throw new Error("Queue asset adapter requires an immutable Claim factory");
  const factoryIdentity = claimIdentity(factoryClaim);
  const client = suppliedClient || global.github;
  const repository = config["target-repo"] || process.env.GITHUB_REPOSITORY || `${global.context.repo.owner}/${global.context.repo.repo}`;
  if (typeof repository !== "string" || !/^[A-Za-z0-9_-]+\/[A-Za-z0-9._-]+$/.test(repository)) throw new Error("Queue asset requires its trusted repository destination");
  const [owner, repo] = repository.split("/");
  const namespace = path.basename(claimArtifactPath("", factoryClaim));
  const branch = normalizeBranchName(config.branch || `assets/${process.env.GH_AW_WORKFLOW_ID || "work-queue"}`) + `/claims/${namespace}`;
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
    const fd = fs.openSync(staged, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
    let content;
    try {
      const stat = fs.fstatSync(fd);
      if (!stat.isFile() || stat.nlink !== 1 || !Number.isSafeInteger(maximum) || stat.size > maximum) throw new Error("Queue asset is not a bounded isolated regular file");
      content = fs.readFileSync(fd);
    } finally {
      fs.closeSync(fd);
    }
    const sha256 = crypto.createHash("sha256").update(content).digest("hex");
    if (message.sha !== undefined && message.sha !== sha256) throw new Error("Queue asset staged content differs from its declared digest");
    const assetPath = `claims/${namespace}/${sha256}${extension}`;
    await assertClaimAuthorized({ ...message, repo: repository, ref: `heads/${branch}`, path: assetPath });
    let head = null;
    let baseTree;
    try {
      const { data } = await client.rest.git.getRef({ owner, repo, ref: `heads/${branch}` });
      head = data.object.sha;
    } catch (error) {
      if (error.status !== 404) throw error;
      if (!branch.startsWith("assets/")) throw new Error("Only the approved assets/ namespace can create a new asset branch");
    }
    if (head) {
      const commit = await client.rest.git.getCommit({ owner, repo, commit_sha: head });
      baseTree = commit.data.tree.sha;
    }
    const blob = await client.rest.git.createBlob({ owner, repo, content: content.toString("base64"), encoding: "base64" });
    const tree = await client.rest.git.createTree({ owner, repo, ...(baseTree ? { base_tree: baseTree } : {}), tree: [{ path: assetPath, mode: "100644", type: "blob", sha: blob.data.sha }] });
    const commit = await client.rest.git.createCommit({ owner, repo, message: `Publish Claim ${factoryClaim} asset ${sha256}`, tree: tree.data.sha, parents: head ? [head] : [] });
    if (head) await client.rest.git.updateRef({ owner, repo, ref: `heads/${branch}`, sha: commit.data.sha, force: false });
    else await client.rest.git.createRef({ owner, repo, ref: `refs/heads/${branch}`, sha: commit.data.sha });
    const server = process.env.GITHUB_SERVER_URL || "https://github.com";
    const result = { success: true, repo: repository, branch, path: assetPath, sha: sha256, commit: commit.data.sha, url: `${server}/${repository}/blob/${branch}/${assetPath}?raw=true` };
    receipts.set(result, { ...factoryIdentity, repository, branch, path: assetPath, sha256, blob: blob.data.sha, tree: tree.data.sha, commit: commit.data.sha, parent: head });
    return result;
  };
}

async function verifyAssetDelivery({ claim, result, github }) {
  const receipt = result && receipts.get(result);
  if (!receiptMatchesClaim(receipt, claim)) return { verified: false };
  const [owner, repo] = receipt.repository.split("/");
  const ref = await github.rest.git.getRef({ owner, repo, ref: `heads/${receipt.branch}` });
  if (ref.data.object.sha !== receipt.commit) return { verified: false };
  const commit = await github.rest.git.getCommit({ owner, repo, commit_sha: receipt.commit });
  if (commit.data.sha !== receipt.commit || commit.data.tree.sha !== receipt.tree) return { verified: false };
  if (receipt.parent ? commit.data.parents?.length !== 1 || commit.data.parents[0].sha !== receipt.parent : commit.data.parents?.length !== 0) return { verified: false };
  const tree = await github.rest.git.getTree({ owner, repo, tree_sha: receipt.tree, recursive: "true" });
  if (tree.data.truncated || tree.data.sha !== receipt.tree || !tree.data.tree?.some(entry => entry.path === receipt.path && entry.type === "blob" && entry.mode === "100644" && entry.sha === receipt.blob)) return { verified: false };
  const blob = await github.rest.git.getBlob({ owner, repo, file_sha: receipt.blob });
  if (blob.data.sha !== receipt.blob || blob.data.encoding !== "base64" || crypto.createHash("sha256").update(Buffer.from(blob.data.content, "base64")).digest("hex") !== receipt.sha256) return { verified: false };
  return {
    verified: true,
    claim_handle: claim.handle,
    resource: { kind: "asset", repository: receipt.repository, id: receipt.commit, ref: `heads/${receipt.branch}`, path: receipt.path, sha256: receipt.sha256, url: result.url },
    effect_resources: [
      { kind: "git_blob", repository: receipt.repository, id: receipt.blob },
      { kind: "git_tree", repository: receipt.repository, id: receipt.tree },
      { kind: "git_commit", repository: receipt.repository, id: receipt.commit },
      { kind: "git_ref", repository: receipt.repository, id: receipt.commit },
    ],
    evidence: { source: "github_git_api", commit: receipt.commit, tree: receipt.tree, blob: receipt.blob, sha256: receipt.sha256 },
  };
}

module.exports = { main, verifyAssetDelivery };
