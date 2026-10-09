// @ts-check
"use strict";

const { canonical, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { replayTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { validateBranch, verifyCheckpointGitHistory } = require("./work_queue_store.cjs");

const MAX_BYTES = 80 * 1024 * 1024;
const JOURNAL_PATH = /^\.gh-aw\/issue-projection\/[a-f0-9]{64}\.json$/;

async function checkedBlobText(githubClient, owner, repo, blob, oid, maxBytes) {
  if (!blob || blob.oid !== oid || !Number.isSafeInteger(blob.byteSize) || blob.byteSize > maxBytes || blob.byteSize < 0) throw queueError("ledger_invalid", "blob does not belong to the immutable checked head or is oversized");
  if (blob.isTruncated === false && typeof blob.text === "string" && blob.byteSize === Buffer.byteLength(blob.text, "utf8")) return blob.text;
  if (blob.isTruncated !== true) throw queueError("ledger_invalid", "checked blob is unreadable");
  const response = await githubClient.rest.git.getBlob({ owner, repo, file_sha: oid, request: { retries: 0 } });
  const value = response?.data;
  if (value?.sha !== oid || value.encoding !== "base64" || typeof value.content !== "string" || value.size !== blob.byteSize) throw queueError("ledger_invalid", "immutable blob fallback identity or encoding is invalid");
  const encoded = value.content.replace(/\s/g, "");
  if (encoded.length > Math.ceil(maxBytes / 3) * 4 || encoded.length % 4 !== 0 || /[^A-Za-z0-9+/=]/.test(encoded)) throw queueError("ledger_invalid", "immutable blob fallback is malformed or oversized");
  const bytes = Buffer.from(encoded, "base64");
  if (bytes.length !== blob.byteSize || bytes.toString("base64") !== encoded) throw queueError("ledger_invalid", "immutable blob fallback is truncated or malformed");
  try {
    return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw queueError("ledger_invalid", "immutable blob fallback contains malformed UTF-8");
  }
}

/** @param {import("./work_queue_store.cjs").QueueReadOptions & {paths?: string[], issueRead?: ReturnType<typeof import("./work_queue_issue_api.cjs").issueReadQuery>}} options */
async function readCheckedQueue({ githubClient, owner, repo, branch = "work-queue", paths = [], issueRead = undefined }) {
  validateBranch(branch);
  if (paths.length > 25 || paths.some(path => !JOURNAL_PATH.test(path))) throw queueError("projection_invalid", "invalid bounded projection journal paths");
  const journals = paths.map((path, index) => `j${index}: object(expression:$j${index}) { oid ... on Blob { text byteSize isTruncated } }`).join("\n");
  const variables = { owner, repo, ref: `refs/heads/${branch}`, log: `refs/heads/${branch}:work-queue.jsonl` };
  paths.forEach((path, index) => {
    variables[`j${index}`] = `refs/heads/${branch}:${path}`;
  });
  const parameters = paths.map((_, index) => `$j${index}:String!`).join(",");
  const nativeParameters = issueRead?.declarations?.join(",") || "";
  Object.assign(variables, issueRead?.variables || {});
  const response = await githubClient.graphql(
    `query CheckedWorkQueue($owner:String!,$repo:String!,$ref:String!,$log:String!${parameters ? "," + parameters : ""}${nativeParameters ? "," + nativeParameters : ""}) {
      repository(owner:$owner,name:$repo) {
        id nameWithOwner isEmpty defaultBranchRef { target { oid } }
        legacy0: ref(qualifiedName:"refs/heads/dispatch-coordinator") { target { oid } }
        legacy1: ref(qualifiedName:"refs/heads/gh-aw-work-queue") { target { oid } }
        ref(qualifiedName:$ref) { target { ... on Commit { oid tree { oid entries { name type mode oid object {
          ... on Tree { entries { name type mode oid object { ... on Tree { entries { name type mode oid } } } } }
        } } } } } }
        log: object(expression:$log) { oid ... on Blob { text byteSize isTruncated } }
        ${journals}
      }
      ${issueRead?.selections?.join("\n") || ""}
    }`,
    variables
  );
  const repository = response?.repository;
  if (!repository || repository.nameWithOwner.toLowerCase() !== `${owner}/${repo}`.toLowerCase()) throw queueError("repository_unavailable", "checked queue repository identity is unavailable");
  if (repository.ref === null) {
    if (repository.isEmpty !== true && !repository.defaultBranchRef?.target?.oid) throw queueError("repository_unavailable", "cannot establish contents access before treating a queue as absent");
    if (branch === "work-queue" && (repository.legacy0 || repository.legacy1)) throw queueError("unsupported_protocol", "a legacy queue cannot be implicitly adopted");
    return { sha: null, treeSha: null, transactions: [], state: replayTransactions([]), branch, logPath: "work-queue.jsonl", repositoryId: repository.id, journal: new Map() };
  }
  const commit = repository.ref?.target;
  if (!commit?.oid || !commit.tree?.oid || !Array.isArray(commit.tree.entries)) throw queueError("ledger_invalid", "checked transport requires an initialized queue");
  const entries = commit.tree.entries;
  if (entries.length > 16384) throw queueError("resource_limit", "queue tree entry limit exceeded");
  if (entries.some(entry => entry.name === "dispatch-work-coordinator.jsonl")) throw queueError("unsupported_protocol", "legacy queue storage is unsupported");
  const logs = entries.filter(entry => entry.name === "work-queue.jsonl");
  const entry = logs[0];
  if (logs.length !== 1 || entry.type !== "blob" || entry.mode !== 33188) throw queueError("ledger_invalid", "queue requires one regular canonical log");
  const text = await checkedBlobText(githubClient, owner, repo, repository.log, entry.oid, MAX_BYTES);
  const state = replayTransactionLog(text);
  if (state.transactions.some(transaction => transaction.actor.repository.toLowerCase() !== repository.nameWithOwner.toLowerCase())) throw queueError("actor_unauthorized", "ledger contains a foreign repository");
  if (state.transactions[0]?.operations[0]?.kind === "Checkpoint") {
    await verifyCheckpointGitHistory({ githubClient, owner, repo, branch, repository: { full_name: repository.nameWithOwner } }, commit.oid, state.transactions[0]);
  }
  const journal = new Map();
  const directory = entries.find(entry => entry.name === ".gh-aw")?.object?.entries?.find(entry => entry.name === "issue-projection")?.object?.entries || [];
  for (const [index, path] of paths.entries()) {
    const value = repository[`j${index}`];
    const expected = directory.find(entry => entry.name === path.split("/").at(-1));
    if (!expected && value === null) continue;
    if (!expected || expected.type !== "blob" || expected.mode !== 33188 || !value || value.oid !== expected.oid) throw queueError("projection_journal_conflict", "journal does not belong to the immutable checked ledger head");
    journal.set(path, parseStrictJSON(await checkedBlobText(githubClient, owner, repo, value, expected.oid, 1024 * 1024), { maxBytes: 1024 * 1024 }));
  }
  return { sha: commit.oid, treeSha: commit.tree.oid, transactions: state.transactions, state, branch, logPath: "work-queue.jsonl", repositoryId: repository.id, journal, nativeResponse: issueRead ? response : undefined };
}

async function writeCheckedFiles({ githubClient, owner, repo, branch, expectedHeadOid, files }) {
  validateBranch(branch);
  if (
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(expectedHeadOid) ||
    files.length < 1 ||
    files.length > 26 ||
    new Set(files.map(file => file.path)).size !== files.length ||
    files.some(
      file =>
        typeof file.path !== "string" ||
        (file.path !== "work-queue.jsonl" && !JOURNAL_PATH.test(file.path)) ||
        typeof file.content !== "string" ||
        Buffer.byteLength(file.content, "utf8") > (file.path === "work-queue.jsonl" ? MAX_BYTES : 1024 * 1024)
    )
  )
    throw queueError("publication_invalid", "checked publication requires explicit head and files");
  const response = await githubClient.graphql("mutation CheckedWorkQueuePublication($input:CreateCommitOnBranchInput!) { createCommitOnBranch(input:$input) { commit { oid } } }", {
    input: {
      branch: { repositoryNameWithOwner: `${owner}/${repo}`, branchName: branch },
      expectedHeadOid,
      message: { headline: "Publish checked work queue projection" },
      fileChanges: { additions: files.map(file => ({ path: file.path, contents: Buffer.from(file.content, "utf8").toString("base64") })) },
    },
    request: { retries: 0, timeout: 30000 },
  });
  const oid = response?.createCommitOnBranch?.commit?.oid;
  if (typeof oid !== "string" || !/^[a-f0-9]{40}$|^[a-f0-9]{64}$/.test(oid)) throw queueError("publication_unresolved", "checked publication returned no commit identity");
  return oid;
}

function writeCheckedCandidate({ githubClient, owner, repo, current, transactions }) {
  return writeCheckedFiles({ githubClient, owner, repo, branch: current.branch, expectedHeadOid: current.sha, files: [{ path: current.logPath, content: transactions.map(commit => canonical(commit)).join("\n") + "\n" }] });
}

module.exports = { readCheckedQueue, writeCheckedFiles, writeCheckedCandidate };
