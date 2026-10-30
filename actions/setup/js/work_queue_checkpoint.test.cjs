import { describe, it } from "vitest";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { deflateRawSync, inflateRawSync } from "node:zlib";

const require = createRequire(import.meta.url);
const fixture = require("../../../specs/work-queue/fixtures/checkpoint.json");
const { administrator, bind, context, dispatcher, finish, genesis, grant, producer, submission } = require("./work_queue_test_helpers.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const { diagnostics } = require("./work_queue_scheduler.cjs");
const { appendCommit, compactTransactions, generateRequestOperations, newRequest, prepareCheckpoint, proposedCommitId, replayTransactionLog, replayTransactions, serializeTransactionLog } = require("./work_queue_replay.cjs");
const { readWorkQueueLog } = require("./work_queue_store.cjs");
const { readCheckedQueue } = require("./work_queue_checked_transport.cjs");
const { acceptedSubmissionParameters } = require("./work_queue_dispatch.cjs");
const { fakeGitHub, options } = require("./work_queue_store_checks.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { queueFixture } = require("./work_queue_lifecycle.test_helpers.cjs");
const { futurePolicy, serializeDeployments } = require("./work_queue_deployment.cjs");
const { normalizeSubmitParameters } = require("./work_queue_intents.cjs");
const { registerStaticQueueAuthentication } = require("./work_queue_auth_cache.cjs");
const { registerQueueLedgerClient } = require("./work_queue_git_transport.cjs");

async function aliasedCheckpointFixture() {
  const contract = "a".repeat(64);
  const revision = "b".repeat(40);
  const queue = queueFixture({
    count: 2,
    batch: 1,
    granted: false,
    configurePolicy(policy) {
      policy.authorization = "aw";
      policy.producers = {};
      const profile = policy.pools.default.profiles.default;
      delete profile.principal;
      profile.logical_contract = contract;
    },
  });
  const fake = fakeGitHub(queue.transactions);
  const source = fake.refs.get("work-queue");
  const prior = "c".repeat(40);
  const git = fake.githubClient.rest.git;
  const getCommit = git.getCommit;
  git.getCommit = args => getCommit({ ...args, commit_sha: args.commit_sha === prior ? source : args.commit_sha });
  fake.refs.set("work-queue", prior);
  async function store(transactions) {
    const content = transactions.map(canonical).join("\n") + "\n";
    const blob = await git.createBlob({ content, encoding: "utf-8" });
    const tree = await git.createTree({ tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
    const commit = await git.createCommit({ tree: tree.data.sha, parents: [fake.refs.get("work-queue")] });
    fake.refs.set("work-queue", commit.data.sha);
    return commit.data.sha;
  }
  const checkpoint = prepareCheckpoint(queue.transactions, prior, queue.administrator, queue.at);
  const parent = await store(checkpoint);
  const restored = replayTransactions(checkpoint);
  const worker = restored.deployments.get("default").get("default");
  const operation = {
    kind: "Deployment",
    pool: "default",
    worker_profile: "default",
    expected_ref: worker.current_ref,
    expected_contract: worker.current_contract,
    profile: { ...worker.revisions[worker.current_ref].profile, ref: revision },
    available: true,
    reason: "compiler_deployment",
  };
  const request = newRequest("checkpoint-worker-upgrade", "deployment", queue.dispatcher, { operations: [operation] });
  const at = queue.at + 1;
  const decision = generateRequestOperations(restored, request, queue.dispatcher, at, request.id);
  const deployment = { version: 3, id: request.id, previous: restored.tip, request, actor: queue.dispatcher, policy_epoch: restored.policy_epoch, at, operations: decision.operations };
  const valid = [...checkpoint, deployment];
  const damaged = structuredClone(valid);
  damaged[0].operations[0].state.deployments = serializeDeployments(replayTransactions(valid));
  const head = await store(damaged);
  fake.state.defaultRevision = revision;
  fake.state.workerContent += `env:\n  GH_AW_WORK_QUEUE_CONTRACT: "${contract}"\n`;
  fake.githubClient.graphql = async () => {
    const oid = fake.refs.get("work-queue");
    const commit = await git.getCommit({ commit_sha: oid });
    const tree = await git.getTree({ tree_sha: commit.data.tree.sha });
    const blobOid = tree.data.tree[0].sha;
    const blob = await git.getBlob({ file_sha: blobOid });
    const text = Buffer.from(blob.data.content, "base64").toString("utf8");
    return {
      repository: {
        id: "repository",
        nameWithOwner: "owner/repo",
        isEmpty: false,
        defaultBranchRef: { target: { oid: revision } },
        legacy0: null,
        legacy1: null,
        ref: { target: { oid, tree: { oid: commit.data.tree.sha, entries: [{ name: "work-queue.jsonl", type: "blob", mode: 33188, oid: blobOid }] } } },
        log: { oid: blobOid, text, byteSize: Buffer.byteLength(text), isTruncated: false },
      },
    };
  };
  return { fake, queue, checkpoint, valid, damaged, parent, head, prior, store };
}

describe("shared version-3 checkpoint conformance", () => {
  it.each([readWorkQueueLog, readCheckedQueue])("retains the verified head when checkpoint ancestry exceeds the proof cache (%#)", async read => {
    const f = await aliasedCheckpointFixture();
    const token = "static-checkpoint-token";
    Object.assign(f.fake.githubClient, { auth: async () => ({ type: "token", token }) });
    registerStaticQueueAuthentication(f.fake.githubClient, token);
    f.fake.refs.set("work-queue", f.parent);
    for (let index = 0; index < 160; index++) await f.store(f.checkpoint);
    const git = f.fake.githubClient.rest.git;
    const getCommit = git.getCommit;
    const commits = [];
    git.getCommit = parameters => {
      commits.push(parameters.commit_sha);
      return getCommit(parameters);
    };
    const input = { githubClient: f.fake.githubClient, owner: "owner", repo: "repo" };
    const first = await read(input);
    assert.ok(commits.includes(f.prior));
    assert.ok(new Set(commits).size > 128);
    commits.length = 0;
    const repeated = await read(input);
    assert.equal(repeated.sha, first.sha);
    assert.equal(canonical(repeated.transactions), canonical(f.checkpoint));
    assert.ok(
      commits.every(sha => sha === first.sha),
      "an unchanged verified head must not replay ancestry"
    );
    assert.ok(commits.length <= 1, `expected at most one head refresh, got ${commits.length}`);

    const next = await f.store(f.valid);
    commits.length = 0;
    const advanced = await read(input);
    assert.equal(advanced.sha, next);
    assert.equal(canonical(advanced.transactions), canonical(f.valid));
    assert.ok(commits.length <= 4, `expected bounded ancestry reads, got ${commits.length}`);
    assert.ok(!commits.includes(f.prior));
    const forged = structuredClone(f.valid);
    forged[0].at++;
    await f.store(forged);
    await assert.rejects(() => read(input), /checkpoint_invalid/);
  });

  it.each([readWorkQueueLog, readCheckedQueue])("reuses verified immutable ancestry while refreshing refs and replaying detached projections (%#)", async read => {
    const f = await aliasedCheckpointFixture();
    const token = "static-checkpoint-token";
    Object.assign(f.fake.githubClient, { auth: async () => ({ type: "token", token }) });
    registerStaticQueueAuthentication(f.fake.githubClient, token);
    f.fake.refs.set("work-queue", f.parent);
    const git = f.fake.githubClient.rest.git;
    const getCommit = git.getCommit;
    const commits = [];
    git.getCommit = parameters => {
      commits.push(parameters.commit_sha);
      return getCommit(parameters);
    };
    const input = { githubClient: f.fake.githubClient, owner: "owner", repo: "repo" };
    const first = await read(input);
    assert.ok(commits.includes(f.prior));
    first.state.policy.limits.operations--;
    first.transactions[0].operations[0].state.stats.nodes++;
    commits.length = 0;
    const repeated = await read(input);
    assert.equal(repeated.state.policy.limits.operations, f.queue.policy.limits.operations);
    assert.equal(canonical(repeated.transactions), canonical(f.checkpoint));
    assert.ok(!commits.includes(f.prior));

    const next = await f.store(f.valid);
    commits.length = 0;
    const advanced = await read(input);
    assert.equal(advanced.sha, next);
    assert.equal(canonical(advanced.transactions), canonical(f.valid));
    assert.ok(!commits.includes(f.prior));
    assert.ok(commits.length <= 4, `expected bounded ancestry reads, got ${commits.length}`);
    const forged = structuredClone(f.valid);
    forged[0].at++;
    await f.store(forged);
    await assert.rejects(() => read(input), /checkpoint_invalid/);
    assert.equal(f.fake.state.updates, 0);
  });

  it("does not reuse ancestry proofs for a different client or cache failed verification", async () => {
    const f = await aliasedCheckpointFixture();
    const token = "static-checkpoint-token";
    Object.assign(f.fake.githubClient, { auth: async () => ({ type: "token", token }) });
    registerStaticQueueAuthentication(f.fake.githubClient, token);
    f.fake.refs.set("work-queue", f.parent);
    const input = { githubClient: f.fake.githubClient, owner: "owner", repo: "repo" };
    await readWorkQueueLog(input);
    const git = f.fake.githubClient.rest.git;
    const getCommit = git.getCommit;
    let denied = 0;
    git.getCommit = async parameters => {
      if (parameters.commit_sha === f.prior) {
        denied++;
        throw Object.assign(new Error("History unavailable"), { status: 403 });
      }
      return getCommit(parameters);
    };
    const other = { ...f.fake.githubClient };
    registerQueueLedgerClient(other, other);
    for (let attempt = 0; attempt < 2; attempt++) await assert.rejects(() => readWorkQueueLog({ ...input, githubClient: other }), /History unavailable/);
    assert.equal(denied, 2);
    git.getCommit = getCommit;
    assert.equal(canonical((await readWorkQueueLog({ ...input, githubClient: other })).transactions), canonical(f.checkpoint));
  });

  it.each([readWorkQueueLog, readCheckedQueue])("does not replay ancestry authorization after same-client credential changes or with unknown auth (%#)", async read => {
    for (const authentication of ["rotated-token", "replaced-auth", "reregistered-auth", "dynamic-auth", "unknown-auth"]) {
      const f = await aliasedCheckpointFixture();
      f.fake.refs.set("work-queue", f.parent);
      let token = "initial-checkpoint-token";
      if (authentication !== "unknown-auth") Object.assign(f.fake.githubClient, { auth: async () => ({ type: "token", token }) });
      if (authentication === "rotated-token" || authentication === "replaced-auth" || authentication === "reregistered-auth") registerStaticQueueAuthentication(f.fake.githubClient, token);
      const input = { githubClient: f.fake.githubClient, owner: "owner", repo: "repo" };
      await read(input);
      if (authentication === "rotated-token" || authentication === "reregistered-auth") token = "downgraded-checkpoint-token";
      if (authentication === "replaced-auth") Object.assign(f.fake.githubClient, { auth: async () => ({ type: "token", token: "replacement-checkpoint-token" }) });
      if (authentication === "reregistered-auth") registerStaticQueueAuthentication(f.fake.githubClient, token);
      const git = f.fake.githubClient.rest.git;
      const getCommit = git.getCommit;
      let denied = 0;
      git.getCommit = async parameters => {
        if (parameters.commit_sha === f.prior) {
          denied++;
          throw Object.assign(new Error("Ancestor access denied"), { status: 403 });
        }
        return getCommit(parameters);
      };
      for (let attempt = 0; attempt < 2; attempt++) await assert.rejects(() => read(input), /Ancestor access denied/, authentication);
      assert.equal(denied, 2, authentication);
      git.getCommit = getCommit;
      assert.equal(canonical((await read(input)).transactions), canonical(f.checkpoint), authentication);
      assert.equal(f.fake.state.updates, 0);
    }
  });

  it.each([readWorkQueueLog, readCheckedQueue])("recovers only deployment aliasing from the verified Git parent without writes (%#)", async read => {
    const f = await aliasedCheckpointFixture();
    const warnings = [];
    const input = { githubClient: f.fake.githubClient, owner: "owner", repo: "repo", core: { warning: message => warnings.push(message), info() {} } };
    const before = canonical(f.checkpoint);
    assert.throws(() => replayTransactions(f.damaged), /checkpoint_invalid/);
    for (let iteration = 0; iteration < 2; iteration++) {
      const recovered = await read(input);
      assert.equal(recovered.checkpointRecovered, true);
      assert.equal(recovered.sha, f.head);
      assert.equal(canonical(recovered.transactions), canonical(f.valid));
      assert.deepEqual(structuredClone([...recovered.state.requests]), structuredClone([...replayTransactions(f.valid).requests]));
    }
    assert.equal(canonical(f.checkpoint), before);
    assert.equal(warnings.length, 2);
    assert.equal(f.fake.state.updates, 0);
    assert.equal(f.fake.refs.get("work-queue"), f.head);
  });

  it("persists recovered checkpoints through submit and dispatch, including lost acknowledgements and stable retries", async () => {
    const f = await aliasedCheckpointFixture();
    const read = () => readWorkQueueLog({ githubClient: f.fake.githubClient, owner: "owner", repo: "repo" });
    const recovered = await read();
    const actor = { role: "producer", principal: f.queue.dispatcher.principal, repository: "owner/repo" };
    const parameters = normalizeSubmitParameters({ nodes: [{ graph_id: "after-recovery", node_key: "task", payload: { task: "new work" } }] }, futurePolicy(recovered.state), f.queue.at + 2, recovered.state);
    const submit = newRequest("submit-after-recovery", "submit", actor, parameters);
    f.fake.state.ambiguousOnce = true;
    const submitted = await publishWorkQueueRequest(options(f.fake, submit, actor, { now: () => f.queue.at + 3 }));
    assert.equal(submitted.state.works.size, 3);
    assert.equal(canonical(submitted.transactions[0]), canonical(f.checkpoint[0]));
    const healed = await read();
    assert.equal(healed.checkpointRecovered, undefined);
    assert.equal(healed.state.requests.has("checkpoint-worker-upgrade"), true);
    const dispatch = newRequest("dispatch-after-recovery", "dispatch_next", f.queue.dispatcher, { pool: "default", max_claims: 3, max_dispatches: 3, max_bytes: healed.state.policy.limits.assignment_bytes });
    const dispatched = await publishWorkQueueRequest(options(f.fake, dispatch, f.queue.dispatcher, { now: () => f.queue.at + 4 }));
    assert.equal(dispatched.state.claims.size, 3);
    assert.equal(dispatched.state.dispatches.size, 3);
    assert.equal(canonical(dispatched.transactions[0]), canonical(f.checkpoint[0]));
    const snapshot = canonical(dispatched.transactions);
    const updates = f.fake.state.updates;
    for (const [request, publisher] of [
      [submit, actor],
      [dispatch, f.queue.dispatcher],
    ]) {
      const retry = await publishWorkQueueRequest(options(f.fake, request, publisher, { now: () => f.queue.at + 5 }));
      assert.equal(retry.recovered, true);
      assert.equal(canonical(retry.transactions), snapshot);
    }
    assert.equal(f.fake.state.updates, updates);
    assert.equal((await read()).state.claims.size, 3);
  });

  it.each(["work", "deployment", "metadata", "digest", "tail", "foreign-parent", "merge-parent", "missing-parent", "unverified-history", "lost-parent-request"])("refuses unrelated corruption or missing authority: %s", async defect => {
    const f = await aliasedCheckpointFixture();
    const damaged = structuredClone(f.damaged);
    const operation = damaged[0].operations[0];
    if (defect === "work") Object.values(operation.state.works)[0].priority = 2;
    if (defect === "deployment") operation.state.deployments.default.default.current_contract = "e".repeat(64);
    if (defect === "metadata") damaged[0].at++;
    if (defect === "digest") operation.state_sha256 = "0".repeat(64);
    if (defect === "tail") damaged[1].operations[0].available = false;
    if (defect === "unverified-history") {
      const source = await f.fake.githubClient.rest.git.getCommit({ commit_sha: f.prior });
      const tree = await f.fake.githubClient.rest.git.getTree({ tree_sha: source.data.tree.sha });
      f.fake.blobs.set(tree.data.tree[0].sha, serializeTransactionLog(f.queue.transactions.slice(0, 1)));
    }
    let extraParent;
    if (defect === "lost-parent-request") {
      const state = replayTransactions(f.valid);
      const operation = { kind: "Control", control: "admission_paused", value: true, reason: "pause" };
      const request = newRequest("must-retain-parent-request", "control", f.queue.administrator, { operations: [operation] });
      const commit = { version: 3, id: request.id, previous: state.tip, request, actor: f.queue.administrator, policy_epoch: state.policy_epoch, at: f.queue.at + 2, operations: [operation] };
      f.fake.refs.set("work-queue", f.parent);
      extraParent = await f.store([...f.valid, commit]);
    }
    const head = await f.store(damaged);
    const git = f.fake.githubClient.rest.git;
    const getCommit = git.getCommit;
    git.getCommit = async args => {
      const response = await getCommit(args);
      if (args.commit_sha === head) {
        const parent = defect === "foreign-parent" ? f.prior : defect === "missing-parent" ? "missing" : (extraParent ?? f.parent);
        response.data.parents = defect === "merge-parent" ? [{ sha: parent }, { sha: f.prior }] : [{ sha: parent }];
      }
      return response;
    };
    for (const read of [readWorkQueueLog, readCheckedQueue]) await assert.rejects(() => read({ githubClient: f.fake.githubClient, owner: "owner", repo: "repo" }));
    assert.equal(f.fake.state.updates, 0);
  });

  it("keeps all checkpoint state isolated while granting and reprioritizing restored Work", () => {
    const checkpoint = structuredClone(fixture.checkpoint);
    const before = canonical(checkpoint);
    const restored = replayTransactions(checkpoint);
    const work = [...restored.works.values()].find(item => item.state === "available" && !item.claim_id);
    assert.ok(work);
    work.effective_priority = 1;
    restored.policy.limits.operations--;
    const request = newRequest("post-checkpoint-grant", "dispatch_next", dispatcher, { pool: "default", max_claims: 1, max_dispatches: 1, max_bytes: 49152 });
    const pristine = replayTransactions(checkpoint);
    const decision = generateRequestOperations(pristine, request, dispatcher, checkpoint[0].at + 1, request.id);
    replayTransactions([...checkpoint, { version: 3, id: request.id, previous: pristine.tip, request, actor: dispatcher, policy_epoch: pristine.policy_epoch, at: checkpoint[0].at + 1, operations: decision.operations }]);
    assert.equal(canonical(checkpoint), before);
    replayTransactions(checkpoint);
  });

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
    let head = "f".repeat(40);
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
    registerQueueLedgerClient(githubClient, githubClient);
    const read = () => readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" });
    try {
      assert.equal((await read()).state.tip, fixture.checkpoint[0].id);
      head = "9".repeat(40);
      parent = foreign;
      await assert.rejects(read(), /checkpoint_invalid/);
      parent = prior;
      head = "8".repeat(40);
      blobs.history = serializeTransactionLog(fixture.history.slice(0, 1));
      await assert.rejects(read(), /checkpoint_invalid/);
    } finally {
      if (checkedTransport === undefined) delete process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT;
      else process.env.GH_AW_WORK_QUEUE_CHECKED_TRANSPORT = checkedTransport;
    }
  });

  it("authenticates checkpoints read through the checked GraphQL transport", async () => {
    let head = "f".repeat(40);
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
    registerQueueLedgerClient(githubClient, githubClient);
    const read = () => readCheckedQueue({ githubClient, owner: "owner", repo: "repo" });
    assert.equal((await read()).state.tip, fixture.checkpoint[0].id);
    head = "9".repeat(40);
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
    registerQueueLedgerClient(githubClient, githubClient);
    const current = await readWorkQueueLog({ githubClient, owner: "owner", repo: "repo" });
    assert.equal(current.sha, parent);
    assert.equal(current.transactions.length, 1);
  });
});
