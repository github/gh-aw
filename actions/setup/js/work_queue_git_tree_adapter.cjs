// @ts-check
"use strict";

const crypto = require("crypto");
const path = require("path");
const { canonical, closed, digest, utf8Compare } = require("./work_queue_codec.cjs");
const { assertClaimAuthorized, currentClaimHandle, claimIdentity, assertClaimIdentity, receiptMatchesClaim, claimArtifactPath } = require("./work_queue_claim_scope.cjs");
const { wrapClaimEffectClient } = require("./work_queue_effect_client.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

const privateReceipts = new WeakMap();
const REVISION = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/;

function validateGitTreeAdapter(adapter) {
  closed(adapter["git-tree"], ["base-revision", "branch-prefix"], ["pull-request", "base-branch"], "trusted code adapter");
  const config = adapter["git-tree"];
  if (!REVISION.test(config["base-revision"]) || typeof config["branch-prefix"] !== "string" || !/^[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*$/.test(config["branch-prefix"]) || config["branch-prefix"].length > 128)
    throw new Error("Trusted code adapter requires an immutable base revision and fixed branch namespace");
  if (config["pull-request"] !== undefined && typeof config["pull-request"] !== "boolean") throw new Error("Trusted code adapter pull-request must be boolean");
  if (config["pull-request"] === true && (typeof config["base-branch"] !== "string" || !/^[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+)*$/.test(config["base-branch"]) || config["base-branch"].length > 128))
    throw new Error("Trusted code adapter pull request requires a fixed base branch");
  const fields = new Set([...Object.keys(adapter["field-map"] || {}), ...Object.keys(adapter.expected || {})]);
  if (!fields.has("files") || [...fields].some(field => !["files", ...(config["pull-request"] ? ["title", "body"] : [])].includes(field)) || (config["pull-request"] && !fields.has("title")))
    throw new Error("Trusted code adapter requires complete declared file and pull request fields");
  return adapter;
}

function treeLeaves(data, sha) {
  if (!data || data.sha !== sha || data.truncated !== false || !Array.isArray(data.tree) || data.tree.length > 4096) throw new Error("Code delivery requires a complete immutable native tree");
  const leaves = new Map();
  const directories = new Set();
  for (const entry of data.tree) {
    if (!entry || typeof entry.path !== "string" || !validPath(entry.path) || !REVISION.test(entry.sha)) throw new Error("Code delivery has a malformed native tree entry");
    if (entry.type === "tree" && entry.mode === "040000") {
      if (directories.has(entry.path)) throw new Error("Code delivery has duplicate native directories");
      directories.add(entry.path);
    } else if ((entry.type === "blob" && ["100644", "100755", "120000"].includes(entry.mode)) || (entry.type === "commit" && entry.mode === "160000")) {
      if (leaves.has(entry.path)) throw new Error("Code delivery has duplicate native file paths");
      leaves.set(entry.path, { path: entry.path, mode: entry.mode, type: entry.type, sha: entry.sha });
    } else throw new Error("Code delivery has unsupported native tree entries");
  }
  const parents = new Set();
  for (const name of leaves.keys()) {
    const segments = name.split("/");
    for (let length = 1; length < segments.length; length++) parents.add(segments.slice(0, length).join("/"));
  }
  if (canonical([...parents].sort(utf8Compare)) !== canonical([...directories].sort(utf8Compare))) throw new Error("Code delivery tree contains incomplete or undeclared directories");
  return leaves;
}

function validPath(value) {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    Buffer.byteLength(value) <= 1024 &&
    !/[\\\u0000-\u001f\u007f]/.test(value) &&
    value.split("/").every(segment => segment && segment !== "." && segment !== ".." && segment.toLowerCase() !== ".git")
  );
}

/** @returns {Array<Record<string, any>>} */
function normalizeFiles(value, baseLeaves) {
  if (!Array.isArray(value) || value.length === 0 || value.length > 128) throw new Error("Trusted code adapter requires a bounded nonempty file declaration");
  let bytes = 0;
  const seen = new Set();
  const files = value.map(entry => {
    closed(entry, ["path"], ["content", "encoding", "mode", "delete"], "prepared code file");
    if (!validPath(entry.path) || seen.has(entry.path)) throw new Error("Prepared code has an invalid or duplicate file path");
    seen.add(entry.path);
    if (entry.delete === true) {
      if (entry.content !== undefined || entry.encoding !== undefined || entry.mode !== undefined || !baseLeaves.has(entry.path)) throw new Error("Prepared code deletion requires one existing exact base file");
      return { path: entry.path, mode: baseLeaves.get(entry.path).mode, type: baseLeaves.get(entry.path).type, sha: null };
    }
    if (entry.delete !== undefined || typeof entry.content !== "string" || !["100644", "100755"].includes(entry.mode || "100644") || ![undefined, "utf-8", "base64"].includes(entry.encoding))
      throw new Error("Prepared code requires declared regular file content, encoding and mode");
    const content = Buffer.from(entry.content, entry.encoding === "base64" ? "base64" : "utf8");
    if (entry.encoding === "base64" && content.toString("base64") !== entry.content) throw new Error("Prepared binary code requires exact canonical base64");
    bytes += content.length;
    if (bytes > 1024 * 1024) throw new Error("Prepared code content exceeds the Claim byte ceiling");
    return { path: entry.path, mode: entry.mode || "100644", type: "blob", content };
  });
  const expectedPaths = new Set(baseLeaves.keys());
  for (const file of files) file.sha === null ? expectedPaths.delete(file.path) : expectedPaths.add(file.path);
  const directories = new Set();
  for (const name of expectedPaths) {
    const segments = name.split("/");
    for (let length = 1; length < segments.length; length++) {
      const parent = segments.slice(0, length).join("/");
      if (expectedPaths.has(parent)) throw new Error("Prepared code conflicts with an existing file or directory");
      directories.add(parent);
    }
  }
  if (expectedPaths.size + directories.size > 4096) throw new Error("Prepared code cannot exceed the complete native tree readback bound");
  return files;
}

function blobIdentity(content, revision) {
  const bytes = Buffer.from(content);
  return crypto
    .createHash(revision.length === 64 ? "sha256" : "sha1")
    .update(`blob ${bytes.length}\0`)
    .update(bytes)
    .digest("hex");
}

function nativeId(value) {
  const id = typeof value === "number" && Number.isSafeInteger(value) ? String(value) : value;
  if (typeof id !== "string" || !/^[1-9][0-9]{0,255}$/.test(id)) throw new Error("Code delivery has an invalid native resource identity");
  return id;
}

function createGitTreeEffectHandler(adapter, suppliedClient) {
  adapter = validateGitTreeAdapter(structuredClone(adapter));
  const identity = claimIdentity(currentClaimHandle());
  const github = wrapClaimEffectClient(suppliedClient, { claim_handle: identity.claim_handle });
  const config = adapter["git-tree"];
  const repository = adapter["target-repo"];
  const [owner, repo] = repository.split("/");
  const branch = `${config["branch-prefix"]}/claims/${path.basename(claimArtifactPath("", identity.claim_handle))}`;
  const stagedMode = isStagedMode();
  let attempted = false;
  return async message => {
    assertClaimIdentity(identity);
    await assertClaimAuthorized(message, { requireCompletion: !stagedMode });
    if (stagedMode) return { success: true, staged: true, claim_handle: identity.claim_handle };
    if (attempted) throw new Error("Code adapter allows only one immutable delivery per Claim");
    const { data: base } = await github.rest.git.getCommit({ owner, repo, commit_sha: config["base-revision"] });
    if (base?.sha !== config["base-revision"] || !REVISION.test(base.tree?.sha)) throw new Error("Code adapter base commit does not match its trusted immutable revision");
    const { data: original } = await github.rest.git.getTree({ owner, repo, tree_sha: base.tree.sha, recursive: "1" });
    const baseLeaves = treeLeaves(original, base.tree.sha);
    const files = normalizeFiles(message.files, baseLeaves);
    if (config["pull-request"] && (typeof message.title !== "string" || !message.title || message.title.length > 256 || (message.body !== undefined && (typeof message.body !== "string" || Buffer.byteLength(message.body) > 65536))))
      throw new Error("Prepared code pull request requires bounded declared title and body");
    for (const file of files) await assertClaimAuthorized({ ...message, repo: repository }, { resource: { repository, path: file.path, ref: `heads/${branch}` } });
    try {
      await github.rest.git.getRef({ owner, repo, ref: `heads/${branch}` });
      throw new Error("Code adapter cannot overwrite an existing Claim branch");
    } catch (error) {
      if (error.status !== 404) throw error;
    }
    attempted = true;
    const expected = new Map(baseLeaves);
    const blobs = [];
    const tree = [];
    for (const file of files) {
      if (file.sha === null) {
        expected.delete(file.path);
        tree.push(file);
        continue;
      }
      const response = await github.rest.git.createBlob({ owner, repo, encoding: "base64", content: Buffer.from(file.content).toString("base64") });
      const sha = blobIdentity(file.content, config["base-revision"]);
      if (response.data?.sha !== sha) throw new Error("Native code blob differs from its exact prepared content");
      const entry = { path: file.path, mode: file.mode, type: "blob", sha };
      expected.set(file.path, entry);
      tree.push(entry);
      blobs.push({ sha, content: file.content.toString("base64") });
    }
    const createdTree = await github.rest.git.createTree({ owner, repo, base_tree: base.tree.sha, tree });
    if (!REVISION.test(createdTree.data?.sha)) throw new Error("Native code tree has no immutable identity");
    const commitMessage = `Apply work ${identity.work_id} for Claim ${identity.claim_id}`;
    const commit = await github.rest.git.createCommit({ owner, repo, tree: createdTree.data.sha, parents: [base.sha], message: commitMessage });
    if (!REVISION.test(commit.data?.sha)) throw new Error("Native code commit has no immutable identity");
    await github.rest.git.createRef({ owner, repo, ref: `refs/heads/${branch}`, sha: commit.data.sha });
    let pull;
    if (config["pull-request"]) {
      const { data } = await github.rest.pulls.create({ owner, repo, head: branch, base: config["base-branch"], title: message.title, body: message.body || "" });
      if (!data || !Number.isSafeInteger(data.number) || data.number < 1) throw new Error("Native code pull request has no exact resource number");
      pull = { id: nativeId(data.id), number: data.number, title: message.title, body: message.body || "" };
    }
    const result = { success: true, repo: repository, branch, commit: commit.data.sha, ...(pull ? { number: pull.number, id: pull.id } : {}) };
    privateReceipts.set(result, {
      ...identity,
      repository,
      branch,
      parent: base.sha,
      tree: createdTree.data.sha,
      commit: commit.data.sha,
      commitMessage,
      expected: [...expected.values()].sort((a, b) => utf8Compare(a.path, b.path)),
      blobs,
      pull,
      baseBranch: config["base-branch"],
      adapter: canonical(adapter),
    });
    return result;
  };
}

async function verifyGitTreeDelivery({ adapter, result, claim, github }) {
  validateGitTreeAdapter(adapter);
  const receipt = result && privateReceipts.get(result);
  if (!receiptMatchesClaim(receipt, claim) || receipt.adapter !== canonical(adapter)) return { verified: false };
  const [owner, repo] = receipt.repository.split("/");
  const { data: ref } = await github.rest.git.getRef({ owner, repo, ref: `heads/${receipt.branch}` });
  if (ref?.object?.sha !== receipt.commit) return { verified: false };
  const { data: commit } = await github.rest.git.getCommit({ owner, repo, commit_sha: receipt.commit });
  if (commit?.sha !== receipt.commit || commit.tree?.sha !== receipt.tree || commit.message !== receipt.commitMessage || commit.parents?.length !== 1 || commit.parents[0].sha !== receipt.parent) return { verified: false };
  const { data: tree } = await github.rest.git.getTree({ owner, repo, tree_sha: receipt.tree, recursive: "1" });
  const leaves = [...treeLeaves(tree, receipt.tree).values()].sort((a, b) => utf8Compare(a.path, b.path));
  if (canonical(leaves) !== canonical(receipt.expected)) return { verified: false };
  for (const expected of receipt.blobs) {
    const { data: blob } = await github.rest.git.getBlob({ owner, repo, file_sha: expected.sha });
    if (blob?.sha !== expected.sha || blob.encoding !== "base64" || !Buffer.from(blob.content, "base64").equals(Buffer.from(expected.content, "base64"))) return { verified: false };
  }
  const effects = [
    ...receipt.blobs.map(blob => ({ kind: "git_blob", repository: receipt.repository, id: blob.sha })),
    { kind: "git_tree", repository: receipt.repository, id: receipt.tree },
    { kind: "git_commit", repository: receipt.repository, id: receipt.commit },
    { kind: "git_ref", repository: receipt.repository, id: receipt.commit },
  ];
  /** @type {Record<string, any>} */
  let resource = { kind: "git_commit", repository: receipt.repository, id: receipt.commit, ref: `heads/${receipt.branch}` };
  if (receipt.pull) {
    const { data } = await github.rest.pulls.get({ owner, repo, pull_number: receipt.pull.number });
    if (
      nativeId(data?.id) !== receipt.pull.id ||
      data.number !== receipt.pull.number ||
      data.title !== receipt.pull.title ||
      data.body !== receipt.pull.body ||
      data.head?.sha !== receipt.commit ||
      data.head.ref !== receipt.branch ||
      data.head.repo?.full_name !== receipt.repository ||
      data.base?.ref !== receipt.baseBranch ||
      data.base.repo?.full_name !== receipt.repository
    )
      return { verified: false };
    resource = { kind: "pull_request", repository: receipt.repository, id: receipt.pull.id, number: receipt.pull.number };
  }
  return {
    verified: true,
    claim_handle: claim.handle,
    resource,
    effect_resources: effects,
    evidence: { source: "independent_git_tree_readback", commit: receipt.commit, tree: receipt.tree, parent: receipt.parent, files_digest: digest(receipt.expected), content_digest: digest(receipt.blobs) },
  };
}

module.exports = { validateGitTreeAdapter, createGitTreeEffectHandler, verifyGitTreeDelivery };
