// @ts-check
"use strict";

import { test } from "vitest";
import assert from "node:assert/strict";
import { DispatchWorkCoordinator, coordinatorBranchName } from "./dispatch_work_coordinator_branch.cjs";
import { deriveWorkId, parseTransactionLog, serializeTransactions } from "./dispatch_work_coordinator.cjs";

function apiError(status, message) {
  return Object.assign(new Error(message), { status });
}

function mockGitHub() {
  const branches = new Map();
  const commits = new Map();
  const trees = new Map();
  const blobs = new Map();
  let objectId = 0;
  let refReads = 0;
  let updateFailures = 0;
  const id = prefix => `${prefix}-${++objectId}`;
  const branchFromRef = ref => ref.replace(/^heads\//, "");
  const githubClient = {
    rest: {
      git: {
        async getRef({ ref }) {
          refReads++;
          const sha = branches.get(branchFromRef(ref));
          if (!sha) throw apiError(404, "Not Found");
          return { data: { object: { sha } } };
        },
        async getCommit({ commit_sha }) {
          const commit = commits.get(commit_sha);
          if (!commit) throw apiError(404, "Not Found");
          return { data: { tree: { sha: commit.tree } } };
        },
        async getTree({ tree_sha }) {
          return { data: { tree: trees.get(tree_sha) || [], truncated: false } };
        },
        async getBlob({ file_sha }) {
          const content = blobs.get(file_sha);
          if (content === undefined) throw apiError(404, "Not Found");
          return { data: { type: "file", encoding: "base64", content: Buffer.from(content).toString("base64") } };
        },
        async createBlob({ content }) {
          const sha = id("blob");
          blobs.set(sha, content);
          return { data: { sha } };
        },
        async createTree({ tree }) {
          const sha = id("tree");
          trees.set(
            sha,
            tree.map(entry => ({ ...entry }))
          );
          return { data: { sha } };
        },
        async createCommit({ tree, parents }) {
          const sha = id("commit");
          commits.set(sha, { tree, parents });
          return { data: { sha } };
        },
        async createRef({ ref, sha }) {
          const branch = ref.replace(/^refs\/heads\//, "");
          if (branches.has(branch)) throw apiError(422, "Reference already exists");
          branches.set(branch, sha);
        },
        async updateRef({ ref, sha }) {
          if (updateFailures > 0) {
            updateFailures--;
            throw apiError(409, "Reference changed");
          }
          const branch = branchFromRef(ref);
          const commit = commits.get(sha);
          if (!commit || branches.get(branch) !== commit.parents[0]) throw apiError(422, "Reference update failed");
          branches.set(branch, sha);
        },
      },
      repos: {},
    },
  };

  return {
    githubClient,
    getRefReads: () => refReads,
    setUpdateFailures: count => (updateFailures = count),
    async seed(branchName, transactions) {
      const blob = id("blob");
      blobs.set(blob, serializeTransactions(transactions));
      const tree = id("tree");
      trees.set(tree, [{ path: "dispatch-work-coordinator.jsonl", mode: "100644", type: "blob", sha: blob }]);
      const commit = id("commit");
      commits.set(commit, { tree, parents: [] });
      branches.set(branchName, commit);
    },
    async getTransactions(branchName) {
      const head = branches.get(branchName);
      if (!head) return [];
      const tree = trees.get(commits.get(head).tree);
      const content = blobs.get(tree[0].sha);
      return parseTransactionLog(content);
    },
    branchExists: branchName => branches.has(branchName),
  };
}

function coordinator(client, options = {}) {
  return new DispatchWorkCoordinator({
    githubClient: client.githubClient,
    owner: "octo",
    repo: "repo",
    identity: "octo/repo/.github/workflows/dispatch.yml",
    runId: "1234",
    workflowId: "octo/repo/.github/workflows/dispatch.yml@refs/heads/main",
    workSchema: { type: "object" },
    sleep: async () => {},
    random: () => 0,
    ...options,
  });
}

test("coordinator branch identity is stable and does not expose workflow identifiers", () => {
  const branch = coordinatorBranchName("octo/repo/.github/workflows/dispatch.yml");
  assert.equal(branch, coordinatorBranchName("octo/repo/.github/workflows/dispatch.yml"));
  assert.match(branch, /^gh-aw\/dispatch-work\/[a-f0-9]{32}$/);
  assert.throws(() => coordinatorBranchName(""), /identity/);
});

test("submit initializes an isolated branch with one canonical transaction log and is idempotent", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  const work = { title: "Review PR", priority: 1 };
  const first = await instance.submit(work);
  const second = await instance.submit({ priority: 1, title: "Review PR" });
  assert.equal(first.work_id, deriveWorkId(work));
  assert.equal(first.state, "available");
  assert.equal(second.work_id, first.work_id);
  assert.equal((await client.getTransactions(instance.branchName)).length, 1);
  assert.equal(client.branchExists(instance.branchName), true);
});

test("submit and replay enforce the configured Work schema", async () => {
  const client = mockGitHub();
  const schema = {
    type: "object",
    properties: { title: { type: "string" } },
    required: ["title"],
    additionalProperties: false,
  };
  const instance = coordinator(client, { workSchema: schema });
  await assert.rejects(instance.submit({ title: 42 }), /configured schema/);
  await assert.rejects(instance.submit({}), /configured schema/);
  await instance.submit({ title: "Valid" });

  const invalidWork = { title: 42 };
  await client.seed(instance.branchName, [{ type: "Work", work_id: deriveWorkId(invalidWork), work: invalidWork }]);
  await assert.rejects(instance.status(), /violates its configured schema/);
});

test("each call reads the latest branch and replayed projection", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  await instance.submit({ title: "First" });
  const externalWork = { title: "Added by another dispatcher" };
  const externalId = deriveWorkId(externalWork);
  await client.seed(instance.branchName, [{ type: "Work", work_id: externalId, work: externalWork }, ...(await client.getTransactions(instance.branchName))]);
  const readsBefore = client.getRefReads();
  assert.equal((await instance.get(externalId)).state, "available");
  assert.equal(client.getRefReads(), readsBefore + 1);
  assert.equal((await instance.status()).counts.available, 2);
});

test("claim and claim_next persist trusted provenance and expose the replay result", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  const first = await instance.submit({ title: "A" });
  const second = await instance.submit({ title: "B" });
  const assignment = await instance.claim(first.work_id);
  assert.equal(assignment.claim_state, "effective");
  assert.equal(assignment.work.title, "A");
  const next = await instance.claimNext();
  assert.equal(next.work_id, second.work_id);
  assert.equal(next.claim_state, "effective");
  const claimRecord = (await client.getTransactions(instance.branchName)).find(tx => tx.type === "Claim");
  assert.equal(claimRecord.run_id, "1234");
  assert.equal(claimRecord.workflow_id, "octo/repo/.github/workflows/dispatch.yml@refs/heads/main");
});

test("claim cannot displace an active Claim but can reclaim Work after cancellation", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  const work = await instance.submit({ title: "Stable claim" });
  const first = await instance.claim(work.work_id);

  await assert.rejects(instance.claim(work.work_id), /already claimed/);
  assert.equal((await client.getTransactions(instance.branchName)).filter(tx => tx.type === "Claim").length, 1);

  await instance.cancelClaim(first.claim_id);
  const replacement = await instance.claim(work.work_id);
  assert.equal(replacement.claim_state, "effective");
});

test("work cancellation is idempotent and rejects completed work", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  const cancelled = await instance.submit({ title: "Cancel me" });
  assert.equal((await instance.cancel(cancelled.work_id)).state, "cancelled");
  assert.equal((await instance.cancel(cancelled.work_id)).state, "cancelled");
  const completed = await instance.submit({ title: "Already done" });
  await instance.claim(completed.work_id);
  const transactions = await client.getTransactions(instance.branchName);
  const completedClaim = transactions.find(tx => tx.type === "Claim" && tx.work_id === completed.work_id);
  await client.seed(instance.branchName, [...transactions, { type: "Completion", claim_id: completedClaim.claim_id }]);
  await assert.rejects(instance.cancel(completed.work_id), /Completed Work/);
});

test("optimistic conflicts trigger a fresh replay and bounded retries", async () => {
  const client = mockGitHub();
  const instance = coordinator(client);
  await instance.submit({ title: "Existing" });
  client.setUpdateFailures(1);
  const beforeReads = client.getRefReads();
  const result = await instance.submit({ title: "Retry" });
  assert.equal(result.state, "available");
  assert.ok(client.getRefReads() >= beforeReads + 3);
  client.setUpdateFailures(10);
  await assert.rejects(instance.submit({ title: "Exhausted" }), /bounded concurrency retries/);
});
