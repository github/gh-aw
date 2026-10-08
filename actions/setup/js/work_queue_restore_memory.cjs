// @ts-check
"use strict";

const { parseStrictJSON } = require("./work_queue_codec.cjs");
const { createHash } = require("node:crypto");

const BASE = "46b68a61c366a01d86dc319b8e689d09a48bad04";
const LEGACY_REF = "heads/memory/eslint-refiner";
const SNAPSHOT_PREFIX = "heads/memory/eslint-refiner-runs/claims/";
const REVISION = /^[a-f0-9]{40}(?:[a-f0-9]{24})?$/;
const FILE_LIMIT = 1024 * 1024;
const HISTORY_LIMIT = 16 * FILE_LIMIT;

/**
 * @typedef {{ref: string, object: {sha: string, type?: string}}} MemoryRef
 * @typedef {{sha: string, tree: {sha: string}}} MemoryCommit
 * @typedef {{sha: string, truncated: boolean, tree: Array<{path: string, type: string, mode: string, sha: string, size?: number}>}} MemoryTree
 * @typedef {{sha: string, encoding: string, content: string, size: number}} MemoryBlob
 * @typedef {{rest: {repos: {get: (parameters: {owner: string, repo: string}) => Promise<{data: {id: number, full_name: string}}>}, git: {
 * getRef: (parameters: {owner: string, repo: string, ref: string}) => Promise<{data: MemoryRef}>,
 * getCommit: (parameters: {owner: string, repo: string, commit_sha: string}) => Promise<{data: MemoryCommit}>,
 * getTree: (parameters: {owner: string, repo: string, tree_sha: string, recursive: string}) => Promise<{data: MemoryTree}>,
 * getBlob: (parameters: {owner: string, repo: string, file_sha: string}) => Promise<{data: MemoryBlob}>
 * }}, paginate: (route: string, parameters: {owner: string, repo: string, ref: string, per_page: number}) => Promise<MemoryRef[]>}} MemoryReadClient
 */

function revision(value) {
  if (typeof value !== "string" || !REVISION.test(value)) throw new Error("Memory history requires an exact native commit, tree or blob identity");
  return value;
}

/** @param {MemoryReadClient} github */
async function restoreESLintMemory(github) {
  const repository = { owner: "github", repo: "gh-aw" };
  const { data: nativeRepository } = await github.rest.repos.get(repository);
  if (nativeRepository.full_name !== "github/gh-aw" || nativeRepository.id !== 1036865607) throw new Error("Memory history repository identity differs from its protected configuration");
  let bytesRead = 0;
  /** @param {string} commit */
  const readTree = async commit => {
    const { data } = await github.rest.git.getCommit({ ...repository, commit_sha: revision(commit) });
    if (data.sha !== commit) throw new Error("Memory history returned a different native commit");
    const treeSha = revision(data.tree.sha);
    const { data: tree } = await github.rest.git.getTree({ ...repository, tree_sha: treeSha, recursive: "1" });
    if (tree.sha !== treeSha || tree.truncated !== false || !Array.isArray(tree.tree) || tree.tree.length > 4096) throw new Error("Memory history requires a complete bounded immutable tree");
    const seen = new Set();
    for (const entry of tree.tree) {
      if (typeof entry.path !== "string" || entry.path.startsWith("/") || entry.path.includes("\\") || entry.path.split("/").some(part => !part || [".", "..", ".git"].includes(part)) || seen.has(entry.path))
        throw new Error("Memory history has a malformed or duplicate native path");
      seen.add(entry.path);
      revision(entry.sha);
    }
    return tree.tree;
  };
  /** @param {{path: string, sha: string, size?: number}} entry */
  const readBlob = async entry => {
    if (entry.size !== undefined && (!Number.isSafeInteger(entry.size) || entry.size < 0 || entry.size > FILE_LIMIT)) throw new Error("Memory history file exceeds its protected byte limit");
    const { data } = await github.rest.git.getBlob({ ...repository, file_sha: entry.sha });
    if (data.sha !== entry.sha || data.encoding !== "base64" || !Number.isSafeInteger(data.size) || data.size < 0 || data.size > FILE_LIMIT) throw new Error("Memory history has an invalid native blob readback");
    const encoded = data.content.replace(/\s/g, "");
    const bytes = Buffer.from(encoded, "base64");
    if (bytes.length !== data.size || bytes.toString("base64") !== encoded || (bytesRead += bytes.length) > HISTORY_LIMIT) throw new Error("Memory history exceeds its complete readback byte bound");
    const hash = createHash(entry.sha.length === 64 ? "sha256" : "sha1")
      .update(`blob ${bytes.length}\0`)
      .update(bytes)
      .digest("hex");
    if (hash !== entry.sha) throw new Error("Memory history bytes differ from their immutable native blob identity");
    const content = bytes.toString("utf8");
    if (!Buffer.from(content).equals(bytes)) throw new Error("Memory history must contain exact UTF-8 JSON");
    for (const line of entry.path.endsWith(".jsonl") ? content.split("\n").filter(line => line.trim()) : [content]) parseStrictJSON(line);
    return { path: entry.path, blob: entry.sha, content };
  };

  let legacyCommit = BASE;
  try {
    const { data } = await github.rest.git.getRef({ ...repository, ref: LEGACY_REF });
    if (data.ref !== `refs/${LEGACY_REF}` || data.object.type !== "commit") throw new Error("Legacy memory history has a foreign native ref");
    legacyCommit = revision(data.object.sha);
  } catch (error) {
    if (!(error instanceof Error) || !("status" in error) || error.status !== 404) throw error;
  }
  const legacyFiles = [];
  for (const entry of await readTree(legacyCommit)) {
    if (entry.type === "blob" && entry.mode === "100644" && /\.(json|jsonl)$/.test(entry.path)) legacyFiles.push(await readBlob(entry));
  }
  const refs = await github.paginate("GET /repos/{owner}/{repo}/git/matching-refs/{ref}", { ...repository, ref: SNAPSHOT_PREFIX, per_page: 100 });
  if (!Array.isArray(refs) || refs.length > 128) throw new Error("Memory history requires at most 128 complete Claim snapshots; archive older history before continuing");
  const seenRefs = new Set();
  const snapshots = [];
  for (const ref of refs.sort((left, right) => left.ref.localeCompare(right.ref))) {
    if (!ref.ref.startsWith(`refs/${SNAPSHOT_PREFIX}`) || !/^[a-f0-9]{64}$/.test(ref.ref.slice(`refs/${SNAPSHOT_PREFIX}`.length)) || ref.object.type !== "commit" || seenRefs.has(ref.ref))
      throw new Error("Memory history has a foreign, duplicated or malformed Claim snapshot ref");
    seenRefs.add(ref.ref);
    const commit = revision(ref.object.sha);
    const entries = await readTree(commit);
    const entry = entries.find(item => item.path === "eslint-refiner.json" && item.type === "blob" && item.mode === "100644");
    if (!entry) throw new Error("Claim memory snapshot has no independently readable memory file");
    const file = await readBlob(entry);
    const memory = parseStrictJSON(file.content);
    if (!memory || typeof memory !== "object" || Array.isArray(memory)) throw new Error("Claim memory snapshot must contain a JSON object");
    snapshots.push({ ref: ref.ref, commit, blob: file.blob, memory });
  }
  // Restored content informs the agent. It never supplies Claim authority or
  // privately verified delivery receipts, even when a ref name looks trusted.
  return { version: 1, repository: "github/gh-aw", legacy: { ref: `refs/${LEGACY_REF}`, commit: legacyCommit, files: legacyFiles }, snapshots };
}

module.exports = { restoreESLintMemory };
