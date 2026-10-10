import { describe, it } from "vitest";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { deflateRawSync, inflateRawSync } from "node:zlib";

const require = createRequire(import.meta.url);
const fixture = require("../../../specs/work-queue/fixtures/checkpoint.json");
const { administrator, bind, context, finish, genesis, grant, producer, submission } = require("./work_queue_test_helpers.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const { diagnostics } = require("./work_queue_scheduler.cjs");
const { appendCommit, compactTransactions, generateRequestOperations, newRequest, prepareCheckpoint, proposedCommitId, replayTransactions, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { readCheckedQueue } = require("./work_queue_checked_transport.cjs");
const { acceptedSubmissionParameters } = require("./work_queue_dispatch.cjs");
const { fakeGitHub, options } = require("./work_queue_store_checks.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");

describe("shared version-3 checkpoint conformance", () => {
  it("preserves first-submit genesis Work and request identity across repeated checkpoints", async () => {
    const fake = fakeGitHub();
    const policy = genesis().operations[0].policy;
    policy.pools.default.profiles.default.ref = fake.state.defaultRevision;
    const nodes = submission([genesis(policy)], ["bootstrap-root"]).operations;
    const request = newRequest("checkpoint-bootstrap", "submit", producer, { nodes });
    const result = await publishWorkQueueRequest(options(fake, request, producer, { policyProposal: policy, maxRetries: 0 }));
    assert.deepEqual(
      result.commit.operations.map(operation => operation.kind),
      ["Policy", "Work"]
    );
    let history = result.transactions;
    for (let iteration = 0; iteration < 2; iteration++) {
      history = prepareCheckpoint(history, "c".repeat(40), administrator, 10000 + iteration);
      const restored = replayTransactions(history);
      assert.equal(restored.works.size, 1);
      assert.equal(restored.works.get(nodes[0].work_id).position.operation, 1);
      assert.equal(canonical(restored.requests.get(request.id).request.parameters), canonical(request.parameters));
      assert.equal(restored.requests.get(request.id).request.fingerprint, request.fingerprint);
      assert.equal(generateRequestOperations(restored, request, producer, 10001 + iteration, "unused").commit.id, result.commit.id);
    }
  });

  it("preserves pending delivery deadline and bound native capacity", () => {
    const history = [genesis()];
    history.push(submission(history, ["delivery"]));
    const decision = grant(history);
    history.push(decision.commit);
    history.push(...bind(history, decision.assignments[0].dispatch_id).slice(history.length));
    history.push(finish(history, decision.assignments[0].dispatch_id, decision.assignments[0].claims[0].handle, "completed"));
    const before = replayTransactions(history);
    const checkpoint = prepareCheckpoint(history, "c".repeat(40), administrator);
    const after = replayTransactions(checkpoint);
    const workId = decision.assignments[0].claims[0].work_id;
    assert.equal(after.works.get(workId).completion_at, before.works.get(workId).completion_at);
    assert.equal(after.works.get(workId).barrier, "pending");
    assert.equal(canonical([...after.dispatches]), canonical([...before.dispatches]));
  });

  it("restores the historical projection and all stable request identities", () => {
    const before = replayTransactions(fixture.history);
    const after = replayTransactions(fixture.checkpoint);
    for (const field of ["works", "claims", "dispatches", "observations", "clocks"]) {
      const entries = map => [...map].map(([key, value]) => [key, field === "clocks" ? diagnostics(value) : value]);
      assert.equal(canonical(entries(after[field])), canonical(entries(before[field])), field);
    }
    assert.equal(after.requests.size, before.requests.size + 1);
    for (const original of fixture.history) {
      const restored = after.requests.get(original.request.id);
      assert.equal(restored.request.fingerprint, original.request.fingerprint);
      assert.equal(canonical(restored.request.parameters), canonical(["submit", "dispatch_next"].includes(original.request.kind) ? original.request.parameters : null));
    }
    const originalSubmission = fixture.history[1];
    const intent = {
      nodes: originalSubmission.request.parameters.nodes.map(({ kind, batch_trust_domain, enqueued, ...node }) => node),
    };
    assert.equal(
      canonical(acceptedSubmissionParameters(after, context(originalSubmission.actor, { created_at: originalSubmission.at }), intent, after.requests.get(originalSubmission.request.id))),
      canonical(originalSubmission.request.parameters)
    );
    assert.equal(after.transactions.length, 1);
    assert.equal(serializeTransactionLog(after.transactions), serializeTransactionLog(fixture.checkpoint));
    const decodedCheckpoint = history => {
      replayTransactions(history);
      const decoded = structuredClone(history);
      // Deflate bytes vary across zlib versions; compare the entire decoded envelope.
      for (const commit of decoded) {
        const state = commit.operations[0].state;
        state.requests_compressed = inflateRawSync(Buffer.from(state.requests_compressed, "base64")).toString("utf8");
      }
      return decoded;
    };
    assert.deepEqual(decodedCheckpoint(compactTransactions(fixture.history, fixture.prior_git_sha, administrator, 100)), decodedCheckpoint(fixture.checkpoint));
    assert.deepEqual(decodedCheckpoint(prepareCheckpoint(fixture.history.slice(0, 1), fixture.prior_git_sha, administrator, 0)), decodedCheckpoint(fixture.genesis_checkpoint));
    assert.equal(canonical(prepareCheckpoint(before, fixture.prior_git_sha)), canonical(prepareCheckpoint(fixture.history, fixture.prior_git_sha)));
    assert.equal(prepareCheckpoint(before, fixture.prior_git_sha)[0].operations[0].prior_git_sha, fixture.prior_git_sha);
    const originalRequest = fixture.history[1].request;
    assert.equal(generateRequestOperations(after, originalRequest, fixture.history[1].actor, 101, "unused").commit.id, fixture.history[1].id);
    const operation = { kind: "Control", control: "grants_paused", value: true, reason: "pause" };
    const request = newRequest("post-checkpoint-pause", "control", administrator, { operations: [operation] });
    const next = {
      version: 3,
      id: proposedCommitId(after, request),
      previous: after.tip,
      request,
      actor: administrator,
      policy_epoch: after.policy_epoch,
      at: 101,
      operations: [operation],
    };
    const extended = appendCommit(fixture.checkpoint, next).state;
    assert.equal(extended.grants_paused, true);
    assert.equal(extended.stats.transactions, fixture.history.length + 2);
    assert.equal(canonical([...extended.clocks].map(([pool, debt]) => [pool, diagnostics(debt)])), canonical([...after.clocks].map(([pool, debt]) => [pool, diagnostics(debt)])));
    const nested = replayTransactions(compactTransactions([fixture.checkpoint[0], next], "b".repeat(40), administrator, 102));
    assert.equal(nested.stats.transactions, fixture.history.length + 3);
    assert.equal(nested.requests.size, extended.requests.size + 1);
    assert.equal(nested.grants_paused, true);
  });

  it("rejects malformed state, digest, predecessor and stable request binding", () => {
    const base = fixture.checkpoint[0];
    const rejects = [
      { ...base, previous: "bogus" },
      { ...base, operations: [{ ...base.operations[0], state: { ...base.operations[0].state, works: {} } }] },
      { ...base, operations: [{ ...base.operations[0], prior_tip: "false-tip" }] },
      { ...base, operations: [{ ...base.operations[0], state_sha256: "0".repeat(64) }], request: newRequest("tampered", "checkpoint", administrator, { ...base.request.parameters, state_sha256: "0".repeat(64) }) },
    ];
    for (const bad of rejects) assert.throws(() => replayTransactions([bad]), /checkpoint_invalid|ledger_invalid/);
    const forged = structuredClone(base);
    const [claimId] = Object.keys(forged.operations[0].state.claims);
    forged.operations[0].state.claims[claimId].work_id = "missing-work";
    forged.operations[0].state_sha256 = digest(forged.operations[0].state);
    forged.request = newRequest(base.request.id, "checkpoint", administrator, { ...base.request.parameters, state_sha256: forged.operations[0].state_sha256 });
    assert.throws(() => replayTransactions([forged]), /checkpoint_invalid/);
    const receiptForgery = structuredClone(base);
    const state = receiptForgery.operations[0].state;
    const requests = JSON.parse(inflateRawSync(Buffer.from(state.requests_compressed, "base64")).toString("utf8"));
    const receipt = requests.find(request => request.kind === "dispatch_next");
    assert.ok(receipt);
    receipt.parameters_digest = "0".repeat(64);
    state.requests_compressed = deflateRawSync(Buffer.from(canonical(requests), "utf8")).toString("base64");
    receiptForgery.operations[0].state_sha256 = digest(receiptForgery.operations[0].state);
    receiptForgery.request = newRequest(base.request.id, "checkpoint", administrator, { ...base.request.parameters, state_sha256: receiptForgery.operations[0].state_sha256 });
    assert.throws(() => replayTransactions([receiptForgery]), /checkpoint_invalid/);
    const rogue = { ...base, id: "rogue", previous: base.id, request: newRequest("rogue", "checkpoint", administrator, base.request.parameters) };
    assert.throws(() => replayTransactions([base, rogue]), /checkpoint_invalid/);
  });

  it("authenticates the checkpoint against its actual Git parent ledger", async () => {
    const checkedTransport = process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT;
    process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT = "graphql";
    const head = "f".repeat(40);
    const foreign = "e".repeat(40);
    const prior = fixture.prior_git_sha;
    const blobs = { root: serializeTransactionLog(fixture.checkpoint), history: serializeTransactionLog(fixture.history) };
    let parent = prior;
    const githubClient = {
      rest: {
        repos: { get: async () => ({ data: { full_name: "owner/repo", size: 1, default_branch: "main" } }) },
        git: {
          getRef: async () => ({ data: { object: { sha: head } } }),
          getCommit: async ({ commit_sha }) => ({
            data: { tree: { sha: commit_sha === head ? "root" : "history" }, parents: commit_sha === head ? [{ sha: parent }] : [] },
          }),
          getTree: async ({ tree_sha }) => ({ data: { tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: tree_sha }] } }),
          getBlob: async ({ file_sha }) => ({ data: { encoding: "base64", content: Buffer.from(blobs[file_sha]).toString("base64") } }),
        },
      },
    };
    const read = () => readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" });
    try {
      assert.equal((await read()).state.tip, fixture.checkpoint[0].id);
      parent = foreign;
      await assert.rejects(read(), /checkpoint_invalid/);
      parent = prior;
      blobs.history = serializeTransactionLog(fixture.history.slice(0, 1));
      await assert.rejects(read(), /checkpoint_invalid/);
    } finally {
      if (checkedTransport === undefined) delete process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT;
      else process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT = checkedTransport;
    }
  });

  it("authenticates checkpoints read through the checked GraphQL transport", async () => {
    const head = "f".repeat(40);
    const prior = fixture.prior_git_sha;
    let parent = prior;
    const logs = {
      root: serializeTransactionLog(fixture.checkpoint),
      history: serializeTransactionLog(fixture.history),
    };
    const githubClient = {
      graphql: async () => ({
        repository: {
          id: "repository",
          nameWithOwner: "owner/repo",
          isEmpty: false,
          defaultBranchRef: { target: { oid: "d".repeat(40) } },
          legacy0: null,
          legacy1: null,
          ref: { target: { oid: head, tree: { oid: "root-tree", entries: [{ name: "work-queue.jsonl", type: "blob", mode: 33188, oid: "root" }] } } },
          log: { oid: "root", text: logs.root, byteSize: Buffer.byteLength(logs.root), isTruncated: false },
        },
      }),
      rest: {
        git: {
          getCommit: async ({ commit_sha }) => ({
            data: {
              tree: { sha: commit_sha === head ? "root-tree" : "history-tree" },
              parents: commit_sha === head ? [{ sha: parent }] : [],
            },
          }),
          getTree: async ({ tree_sha }) => ({
            data: { tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: tree_sha === "root-tree" ? "root" : "history" }] },
          }),
          getBlob: async ({ file_sha }) => ({
            data: { encoding: "base64", content: Buffer.from(logs[file_sha]).toString("base64"), size: Buffer.byteLength(logs[file_sha]) },
          }),
        },
      },
    };
    const read = () => readCheckedQueue({ githubClient, owner: "owner", repo: "repo" });
    assert.equal((await read()).state.tip, fixture.checkpoint[0].id);
    parent = "e".repeat(40);
    await assert.rejects(read(), /checkpoint_invalid/);
  });

  it("verifies checkpoint ancestry without a finite compaction lifetime", async () => {
    const commits = new Map();
    const trees = new Map();
    let parent = fixture.prior_git_sha;
    let ledger = fixture.history;
    let at = fixture.checkpoint[0].at;
    const baseTree = "checkpoint-chain-base";
    commits.set(parent, { tree: baseTree, parents: [] });
    trees.set(baseTree, serializeTransactionLog(ledger));
    for (let index = 0; index < 66; index++) {
      const checkpoint = compactTransactions(ledger, parent, administrator, at++);
      const tree = `checkpoint-chain-${index}`;
      const head = index.toString(16).padStart(40, "0");
      commits.set(head, { tree, parents: [parent] });
      trees.set(tree, serializeTransactionLog(checkpoint));
      ledger = checkpoint;
      parent = head;
    }
    const githubClient = {
      rest: {
        repos: { get: async () => ({ data: { full_name: "owner/repo", size: 1, default_branch: "main" } }) },
        git: {
          getRef: async () => ({ data: { object: { sha: parent } } }),
          getCommit: async ({ commit_sha }) => {
            const commit = commits.get(commit_sha);
            assert.ok(commit, `missing mock commit ${commit_sha}`);
            return { data: { tree: { sha: commit.tree }, parents: commit.parents.map(sha => ({ sha })) } };
          },
          getTree: async ({ tree_sha }) => ({ data: { tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: tree_sha }] } }),
          getBlob: async ({ file_sha }) => ({ data: { encoding: "base64", content: Buffer.from(trees.get(file_sha)).toString("base64") } }),
        },
      },
    };
    const current = await readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" });
    assert.equal(current.sha, parent);
    assert.equal(current.transactions.length, 1);
  });
});
