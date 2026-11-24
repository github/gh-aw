"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { LocalGitHub, parseArgs, policyFor, runSimulator } = require("./work-queue-stress.cjs");
const { PROBES, appendHistoryBatch, assertBoundedCheckpoint, retainedPolicy, verifiedCheckpoint } = require("./work-queue-stress-retained.cjs");
const { AT, LAUNCH_REF, OWNER, PROTOCOL_LIMITS, REPO, WORKFLOW, dispatchParameters, queueActor } = require("./work-queue-stress-git.cjs");
const { canonical } = require("../../actions/setup/js/work_queue_codec.cjs");
const { normalizeSubmitParameters } = require("../../actions/setup/js/work_queue_intents.cjs");
const { validatePolicyEnv } = require("../../actions/setup/js/work_queue_policy.cjs");
const { buildCommit, checkpointCommit, newRequest, newState, replayTransactions, serializeTransactionLog } = require("../../actions/setup/js/work_queue_replay.cjs");
const { launchedRunEvidence, postQueueDispatch } = require("../../actions/setup/js/work_queue_native.cjs");
const { planDispatch } = require("../../actions/setup/js/work_queue_scheduler.cjs");
const { publishWorkQueueCheckpoint, publishWorkQueueRequest, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");

function assertProtocolLimits(limits) {
  assert.deepEqual(limits, PROTOCOL_LIMITS);
  assert.equal(limits.ledger_bytes, 64 * 1024 * 1024);
  assert.equal(limits.operations, 256);
  assert.equal(limits.pending_work, 4096);
  assert.equal(limits.graph_nodes, 4096);
  assert.equal(limits.claims_per_dispatch, 1);
  assert.equal(limits.checkpoint_after_lines, 512);
  assert.equal(limits.retained_requests, 4096);
}

test("a checkpoint never retains more than RETAINED_REQUESTS request IDs and keeps every live record", { timeout: 60000 }, () => {
  const policy = retainedPolicy();
  const producer = queueActor("producer");
  const administrator = queueActor("administrator");
  const probes = normalizeSubmitParameters({ nodes: Array.from({ length: PROBES }, (_, index) => ({ graph_id: "window-probes", node_key: `p${index}`, pool: "probe", payload: { probe: index } })) }, policy);
  const state = replayTransactions([buildCommit(newState(), producer, newRequest("submit", producer, { work_ids: probes.map(work => work.work_id) }), probes, AT)]);
  // One unfolded log longer than the request window: 1 + 33 * 130 commits.
  for (let batch = 0; batch < 33; batch++) appendHistoryBatch(state, policy, "window", batch, 128);
  assert.equal(state.transactions.length, 4291);
  assert.ok(state.requests.size > 4096);
  const bounded = verifiedCheckpoint(state, administrator);
  assert.deepEqual({ requests: bounded.requests, works: bounded.works, claims: bounded.claims }, { requests: 4096, works: PROBES, claims: 0 }, "only the live probes and the newest 4096 requests survive");
  const checkpoint = checkpointCommit(state, administrator, AT);
  const tampered = mutate => {
    const commit = structuredClone(checkpoint);
    mutate(commit.operations[0].state);
    return commit;
  };
  // Production restore refuses a window past the bound.
  assert.throws(
    () => replayTransactions([tampered(snapshot => snapshot.requests.push("request:one-too-many"))]),
    error => error.code === "checkpoint_invalid"
  );
  // The harness oracle refuses a stale window and a lost live Work, which restore cannot see.
  const oldest = state.transactions[0].request.id;
  assert.throws(
    () =>
      assertBoundedCheckpoint(
        state,
        tampered(snapshot => snapshot.requests.splice(0, 1, oldest))
      ),
    /request window/
  );
  const [probe] = probes;
  assert.throws(
    () =>
      assertBoundedCheckpoint(
        state,
        tampered(snapshot => {
          delete snapshot.works[probe.work_id];
          delete snapshot.work_creators[probe.work_id];
        })
      ),
    /lost live Work/
  );
});

test("checkpoint loses real-Git publication race, refreshes, and preserves cold replay", { timeout: 60000 }, async () => {
  const directory = await fs.mkdtemp(path.join(process.cwd(), ".work-queue-stress-checkpoint-"));
  const host = await new LocalGitHub(path.join(directory, "queue.git")).initialize();
  const branch = "checkpoint-race";
  const args = { githubClient: host.client, owner: OWNER, repo: REPO, branch };
  try {
    const policy = policyFor(2);
    const administrator = queueActor("administrator");
    const producer = queueActor("producer");
    const dispatcher = queueActor("dispatcher");
    const checkpoint = () => publishWorkQueueCheckpoint({ ...args, actor: administrator, now: () => AT });
    const submit = async name => {
      const works = normalizeSubmitParameters({ nodes: [{ graph_id: "checkpoint-graph", node_key: name, payload: { item: name } }] }, policy);
      return publishWorkQueueRequest({
        ...args,
        actor: producer,
        request: { kind: "submit", parameters: { work_ids: works.map(work => work.work_id) } },
        operationsFor: state => works.filter(work => !state.works.has(work.work_id)),
        now: () => AT,
      });
    };
    await submit("first");
    const stale = await readWorkQueueLog(args);
    assert.equal(stale.transactions.length, 1, "first Work is one native publication");
    assert.deepEqual(
      stale.transactions[0].operations.map(operation => operation.kind),
      ["Work"],
      "v4 genesis carries no Policy: the compiled policy stays outside the ledger"
    );
    assert.equal(stale.transactions[0].request.kind, "submit");
    assert.equal(stale.transactions[0].actor.role, "producer");
    assert.deepEqual([...stale.state.works.values()][0].position, { commit: 0, operation: 0 });
    assert.equal((await submit("first")).recovered, true, "genesis retry must recover the original submission");
    const staleCheckpoint = checkpointCommit(stale.state, administrator, AT);
    await submit("racing");
    const parameters = dispatchParameters(policy, "default", 1);
    const grant = await publishWorkQueueRequest({
      ...args,
      actor: dispatcher,
      request: { kind: "dispatch_next", parameters },
      operationsFor: (state, request) => planDispatch(state, parameters, request.id).operations,
      now: () => AT,
    });
    assert.equal(grant.commit.operations.length, 1);
    const publish = async (base, transactions) => {
      const content = serializeTransactionLog(transactions);
      const blob = await host.client.rest.git.createBlob({ owner: OWNER, repo: REPO, content, encoding: "utf-8" });
      const tree = await host.client.rest.git.createTree({
        owner: OWNER,
        repo: REPO,
        base_tree: base.treeSha,
        tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }],
      });
      const candidate = await host.client.rest.git.createCommit({
        owner: OWNER,
        repo: REPO,
        tree: tree.data.sha,
        parents: [base.sha],
        message: "checkpoint candidate",
      });
      return host.client.rest.git.updateRef({ owner: OWNER, repo: REPO, ref: `heads/${branch}`, sha: candidate.data.sha, force: false });
    };
    await assert.rejects(publish(stale, [staleCheckpoint]), error => error.status === 422 && /not a fast-forward/.test(error.message));
    const fresh = await readWorkQueueLog(args);
    assert.equal(fresh.state.works.size, 2, "failed checkpoint must not erase competing submission");
    assert.equal(fresh.state.claims.size, 1, "checkpoint must preserve an open Claim");
    assert.equal((await checkpoint()).checkpointed, true);
    const restored = await readWorkQueueLog(args);
    assert.equal(restored.transactions.length, 1);
    assert.equal(restored.transactions[0].previous, null, "a checkpoint is a root commit");
    const snapshot = restored.transactions[0].operations[0];
    assert.equal(snapshot.kind, "Checkpoint");
    assert.equal(Object.hasOwn(snapshot, "history"), false, "checkpoint must store minimal state, not a compressed historical log");
    assert.equal(Object.hasOwn(snapshot.state, "transactions"), false, "checkpoint snapshot must not embed the transaction log");
    assert.equal(restored.state.works.size, 2);
    assert.equal(restored.state.claims.size, 1);
    assert.equal(restored.state.dispatches.size, 1);
    for (const field of ["works", "claims", "dispatches"]) for (const [id, record] of fresh.state[field]) assert.equal(canonical(restored.state[field].get(id)), canonical(record));
    const [claim] = restored.state.claims.values();
    assert.equal(claim.state, "open", "the granted Claim stays open across the checkpoint");
    assert.equal(restored.state.dispatches.get(claim.dispatch_id).state, "reserved");
    assert.equal(restored.state.requests.size, fresh.state.requests.size + 1);
    assert.equal(host.metrics.cas_conflicts, 1);
    // Malformed one-Claim-per-Dispatch ownership is checkpoint_invalid, never an empty or truncated queue.
    const tampered = mutate => {
      const commit = structuredClone(restored.transactions[0]);
      mutate(commit.operations[0].state);
      return () => replayTransactions([commit]);
    };
    const invalid = error => error.code === "checkpoint_invalid";
    assert.throws(
      tampered(state => (state.dispatches[claim.dispatch_id].state = "closed")),
      invalid
    );
    assert.throws(
      tampered(state => state.dispatches[claim.dispatch_id].claims.push(claim.claim_id)),
      invalid
    );
    const [available] = [...fresh.state.works.values()].filter(work => work.state === "available");
    assert.throws(
      tampered(state => {
        state.claims.hidden = { ...state.claims[claim.claim_id], claim_id: "hidden", work_id: available.work_id };
        Object.assign(state.works[available.work_id], { state: "claimed", claim_id: "hidden" });
      }),
      invalid
    );
    await submit("after-checkpoint");
    const continued = await readWorkQueueLog(args);
    assert.equal(continued.transactions.length, 2);
    assert.equal(continued.state.works.size, 3, "post-checkpoint append must preserve snapshot Work");
    assert.equal(continued.transactions[1].previous, restored.transactions[0].id);
    host.armFaults(branch);
    const outcomes = await Promise.allSettled([0, 1].map(() => checkpoint()));
    assert.equal(outcomes.filter(result => result.status === "fulfilled").length, 2, "both checkpoint publishers must recover after one wins a ref race");
    assert.ok(
      outcomes.every(result => result.status === "fulfilled" && result.value.checkpointed === false),
      "neither ambiguous response is a fresh publication"
    );
    assert.equal((await readWorkQueueLog(args)).state.works.size, 3, "competing checkpoint must not lose snapshot Work");
    assert.equal(host.metrics.forced_races, 1);
    assert.equal(host.metrics.lost_success_responses, 1);
    assert.ok(host.metrics.cas_conflicts >= 2);
    await submit("intervening-baseline");
    const updateRef = host.client.rest.git.updateRef;
    let injected = false;
    host.client.rest.git.updateRef = async request => {
      if (!injected && request.ref === `heads/${branch}`) {
        injected = true;
        await submit("intervening");
        await checkpoint();
      }
      return updateRef(request);
    };
    try {
      const recovered = await checkpoint();
      assert.equal(injected, true, "the checkpoint writer must encounter a competing mutation and checkpoint");
      assert.equal(recovered.checkpointed, false);
      const current = await readWorkQueueLog(args);
      assert.equal(current.state.works.size, 5, "checkpoint retry must preserve Work published between source read and competing checkpoint");
      assert.equal(current.transactions.length, 1);
      assert.equal(current.transactions[0].operations[0].kind, "Checkpoint");
    } finally {
      host.client.rest.git.updateRef = updateRef;
    }
    await host.git(["fsck", "--strict", "--no-dangling"]);
  } finally {
    await host.close();
    await fs.rm(directory, { recursive: true, force: true });
  }
});

test("real-Git adapter refuses stale non-fast-forward candidates and foreign repositories", { timeout: 30000 }, async () => {
  const directory = await fs.mkdtemp(path.join(process.cwd(), ".work-queue-stress-adapter-"));
  const host = await new LocalGitHub(path.join(directory, "queue.git")).initialize();
  try {
    const common = { owner: OWNER, repo: REPO };
    const git = host.client.rest.git;
    const base = await git.getCommit({ ...common, commit_sha: host.ref });
    const a = await git.createCommit({ ...common, tree: base.data.tree.sha, parents: [host.ref], message: "candidate A" });
    const b = await git.createCommit({ ...common, tree: base.data.tree.sha, parents: [host.ref], message: "candidate B" });
    await git.createRef({ ...common, ref: "refs/heads/adapter", sha: host.ref });
    await git.updateRef({ ...common, ref: "heads/adapter", sha: a.data.sha, force: false });
    await assert.rejects(git.updateRef({ ...common, ref: "heads/adapter", sha: b.data.sha, force: false }), error => error.status === 422 && /not a fast-forward/.test(error.message));
    assert.equal((await git.getRef({ ...common, ref: "heads/adapter" })).data.object.sha, a.data.sha);
    assert.equal(host.metrics.cas_conflicts, 1);
    await assert.rejects(host.client.rest.repos.get({ owner: "foreign", repo: "queue" }), /foreign repositories/);
    await assert.rejects(git.updateRef({ ...common, ref: "heads/adapter", sha: b.data.sha, force: true }), /must not force/);
    // A launch at the branch ref yields a readable run at the resolved revision.
    const destination = { ...common, workflow: WORKFLOW };
    const response = await postQueueDispatch(host.client, { ...destination, ref: LAUNCH_REF }, { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: "d_adapter" }) });
    assert.deepEqual(await launchedRunEvidence(host.client, destination, response), { run_id: "1000", revision: host.ref });
    assert.deepEqual(host.launches.get("d_adapter").run_ids, ["1000"]);
    await assert.rejects(postQueueDispatch(host.client, { ...destination, ref: "refs/heads/adapter" }, { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: "d_adapter" }) }));
    assert.equal(host.metrics.native_posts, 1);
    await host.git(["fsck", "--strict", "--no-dangling"]);
  } finally {
    await host.close();
    await fs.rm(directory, { recursive: true, force: true });
  }
});

test("two publishers complete real-Git lifecycle with CAS races, lost response, duplicate launch, and immutable cold replay", { timeout: 120000 }, async () => {
  const report = await runSimulator({ items: 32, workers: 2, seed: 7, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.requested, 32);
  assert.equal(report.submitted, 32);
  assert.equal(report.claimed, 32);
  assert.equal(report.completed, 32);
  assert.equal(report.verified_results, 32);
  assert.equal(report.closed_dispatches, 32, "one Dispatch per Claim");
  assert.equal(report.branch_count, 1);
  assert.ok(report.git_api_metrics.cas_conflicts >= 1);
  assert.equal(report.git_api_metrics.forced_races, 1);
  assert.equal(report.git_api_metrics.lost_success_responses, 1);
  assert.ok(report.duplicate_runs <= 1);
  assert.equal(report.publisher_metrics.duplicate_launch_refusals, report.duplicate_runs);
  assert.equal(report.git_api_metrics.native_posts, report.closed_dispatches + report.duplicate_runs);
  assert.ok(report.publisher_metrics.recovered_publications >= 3);
  assert.ok(report.publisher_metrics.binding_checks >= 32 * 11);
  assert.ok(report.account_limit_skips > 0, "the per-account limit must bind at least once");
  assert.deepEqual(validatePolicyEnv(structuredClone(report.policy)), report.policy);
  assert.ok(report.max_rss_bytes > 0);
  assert.ok(report.queues[0].cold_replay_ms > 0);
  assert.ok(report.queues[0].ledger_bytes > 0);
  assert.ok(report.queues[0].checkpoint_bytes > 0);
  assert.equal(report.queues[0].checkpoint_works, 0, "the checkpoint folds every completed Work");
  assert.equal(report.queues[0].checkpoint_requests, report.queues[0].transactions, "a short log keeps its whole request window");
  assertProtocolLimits(report.limits);
});

test("single-queue saturation reports actual admission and never pretends 100000 completed", { timeout: 60000 }, async () => {
  const report = await runSimulator({ mode: "saturation", items: 100000, workers: 2, timeoutSeconds: 45 });
  assert.equal(report.ok, true);
  assert.equal(report.branch_count, 1);
  assert.equal(report.submitted, 4096);
  assert.equal(report.claimed, 0);
  assert.equal(report.completed, 0);
  assert.equal(report.saturation.code, "work_queue_pending_limit");
  assert.equal(report.saturation.attempted, 4352);
  assert.equal(report.saturation.refused_batch, 256);
  assert.equal(report.saturation.not_attempted, 95648);
  assert.equal(report.saturation.replay_ceiling.code, "resource_limit", "replay enforces the protocol ceiling below the policy check");
  assert.equal(report.git_api_metrics.native_posts, 0);
  assert.equal(report.git_api_metrics.forced_races, 0);
  assert.equal(report.queues[0].selection_reason, "claim_budget_reached");
});

test("bounded branches are explicit and cleanly finish with more than two workers", { timeout: 120000 }, async () => {
  const report = await runSimulator({ items: 16, workers: 4, queueItems: 8, seed: 11, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.planned_branch_count, 2);
  assert.equal(report.branch_count, 2);
  assert.deepEqual(
    report.queues.map(queue => queue.submitted),
    [8, 8]
  );
  assert.equal(report.completed, 16);
  assert.equal(report.closed_dispatches, 16);
  assert.equal(report.git_api_metrics.native_posts, report.closed_dispatches + report.duplicate_runs);
  assert.match(report.scope, /NOT single-queue/);
});

test("CLI accepts 100000 total without claiming single-queue support or raising ceilings", () => {
  const options = parseArgs(["--items", "100000", "--workers", "4", "--queue-items", "128"]);
  assert.equal(options.items, 100000);
  assert.equal(options.queueItems, 128);
  assert.equal(Math.ceil(options.items / options.queueItems), 782);
  assert.equal(parseArgs(["--mode", "retained", "--items", "100000"]).mode, "retained");
  assert.equal(parseArgs([]).queueItems, 32);
  assert.equal(parseArgs(["--mode", "retained", "--items", "100000"]).historyQueueItems, 10000);
  const entityTarget = parseArgs(["--history-items", "100000"]);
  assert.equal(entityTarget.mode, "retained");
  assert.equal(entityTarget.items, 50000);
  assert.equal(entityTarget.historyQueueItems, 50000);
  assert.throws(() => parseArgs(["--history-items", "99999"]), /even integer/);
  assert.throws(() => parseArgs(["--history-items", "100000", "--mode", "lifecycle"]), /retained mode/);
  const history = parseArgs(["--mode", "history", "--items", "100000"]);
  assert.equal(history.mode, "history");
  assert.equal(history.items, 100000);
  assert.equal(history.historyQueueItems, 50000);
  assert.throws(() => parseArgs(["--mode", "history", "--items", "99999"]), /even/);
  assert.throws(() => parseArgs(["--workers", "1"]), /workers/);
  assert.throws(() => parseArgs(["--queue-items", "4096"]), /queueItems/);
  assert.throws(() => parseArgs(["--timeout-seconds", "0"]), /timeoutSeconds/);
  assert.throws(() => parseArgs(["--items", "Infinity"]), /items/);
  assert.throws(() => parseArgs(["--unknown"]), /argument/);
});

test("retained mode validates real Work/Claim history and separates fixture scale from publisher throughput", { timeout: 120000 }, async () => {
  const report = await runSimulator({ historyItems: 256, workers: 2, seed: 17, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.requested, 128);
  assert.equal(report.submitted, 128);
  assert.equal(report.claimed, 128);
  assert.equal(report.completed, 0);
  assert.equal(report.cancelled, 128);
  assert.equal(report.probe_work, 8);
  assert.equal(report.retained_entities, 264);
  assert.equal(report.history_entities, 256);
  assert.equal(report.checkpoint_epochs, 0, "131 lines never pass CHECKPOINT_AFTER_LINES");
  assert.equal(report.requested_history_entities, 256);
  assert.equal(report.fixture_batch, 128);
  assert.equal(report.transactions, 131, "probe genesis, then submit, dispatch_next and one launch release per Dispatch for 128 historical Work");
  assert.equal(report.pending_work, 8);
  assert.equal(report.queues.length, 2);
  assert.equal(report.queues[1].retained_entities, 264);
  assert.equal(report.selected_probe_claims, 8);
  assert.equal(report.historical_reservations, 128, "one Dispatch per historical Claim");
  assert.equal(report.checkpoint_requests, report.transactions, "the final checkpoint keeps the whole short request window");
  assert.equal(report.max_checkpoint_works, 8, "the final checkpoint keeps only the live probes");
  assert.equal(report.fixture_native_posts, 0);
  assert.equal(report.checkpoint_persisted, true);
  assert.ok(report.checkpoint_bytes > 0);
  assert.equal(report.lifecycle_smoke.completed, 16);
  assert.equal(report.lifecycle_smoke.git_api_metrics.lost_success_responses, 1);
  assert.ok(report.lifecycle_smoke.git_api_metrics.cas_conflicts >= 1);
  assert.ok(report.cold_git_read_replay_ms > 0);
  assert.ok(report.ten_batch_plans_ms > 0);
  assert.ok(report.checkpoint_ms > 0);
  assert.match(report.projection_sha256, /^[a-f0-9]{64}$/);
  assert.equal(report.saturation, null);
  assertProtocolLimits(report.limits);
});

test("retained shards count validated histories separately and run worker smoke only once", { timeout: 120000 }, async () => {
  const report = await runSimulator({ mode: "retained", items: 128, historyQueueItems: 64, workers: 2, seed: 23, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.submitted, 128);
  assert.equal(report.claimed, 128);
  assert.equal(report.completed, 0);
  assert.equal(report.cancelled, 128);
  assert.equal(report.retained_entities, 272);
  assert.equal(report.probe_work, 16);
  assert.equal(report.branch_count, 3);
  assert.equal(report.history_branch_count, 2);
  assert.equal(report.planned_history_branch_count, 2);
  assert.equal(report.history_queue_item_bound, 64);
  assert.equal(report.batch_plan_count, 20);
  assert.equal(report.lifecycle_smoke.completed, 16);
  assert.equal(report.lifecycle_smoke.git_api_metrics.lost_success_responses, 1);
  const history = report.queues.filter(queue => queue.kind === "retained-fixture");
  assert.equal(history.length, 2);
  assert.equal(new Set(history.map(queue => queue.branch)).size, 2);
  assert.equal(
    report.ledger_bytes_total,
    report.queues.reduce((sum, queue) => sum + queue.ledger_bytes, 0)
  );
  assert.ok(history.every(queue => queue.ledger_bytes < report.limits.ledger_bytes));
  assert.equal(report.saturation, null);
});

test("history mode builds lifetime history across checkpoint epochs and cold-checks it in multiple worker threads", { timeout: 120000 }, async () => {
  const report = await runSimulator({ mode: "history", items: 1280, workers: 2, seed: 29, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.mode, "history");
  assert.equal(report.requested, 1280);
  assert.equal(report.requested_history_work, 640);
  assert.equal(report.history_entities, 1280);
  assert.equal(report.seeded_cancelled_work, 640);
  assert.equal(report.seeded_claims, 640);
  assert.equal(report.completed, 0);
  assert.equal(report.new_worker_completions, 16);
  // Genesis plus four 130-line batches passes 512 lines once; the fifth batch is the unfolded tail.
  assert.equal(report.checkpoint_epochs, 1);
  assert.equal(report.tail_history_work, 128);
  assert.equal(report.transactions, 131, "checkpoint root plus one tail batch");
  assert.equal(report.retained_entities, 264, "live probes plus the tail epoch");
  assert.equal(report.max_checkpoint_works, 8, "every checkpoint keeps only the live probes");
  assert.equal(report.max_checkpoint_requests, 521, "the epoch checkpoint keeps its whole 521-request log");
  assert.ok(report.max_checkpoint_requests <= report.limits.retained_requests);
  assert.ok(report.max_ledger_bytes > report.ledger_bytes, "the folded epoch was larger than the tail");
  assert.equal(report.history_validation_workers, 2);
  assert.equal(report.history_worker_verifications.length, 2);
  for (const verification of report.history_worker_verifications) {
    assert.equal(verification.work, 136);
    assert.equal(verification.claims, 128);
    assert.equal(verification.completed, 0);
    assert.equal(verification.projection_sha256, report.projection_sha256);
    assert.equal(verification.ledger_bytes, report.ledger_bytes);
    assert.ok(verification.checkpoint_ms > 0);
    assert.equal(verification.checkpoint_bytes, report.checkpoint_bytes);
    assert.equal(verification.checkpoint_requests, report.checkpoint_requests);
  }
});

test("harness deadline terminates workers, reports partial Git counts, and cleans its local fixture", { timeout: 15000 }, async () => {
  const fixtures = async () => (await fs.readdir(process.cwd())).filter(name => name.startsWith(".work-queue-stress-")).sort();
  const before = await fixtures();
  await assert.rejects(runSimulator({ items: 64, workers: 2, seed: 13, timeoutSeconds: 1 }), error => {
    assert.match(error.message, /deadline/);
    assert.equal(error.report.ok, false);
    assert.ok(error.report.elapsed_ms < 10000);
    assert.ok(error.report.submitted >= error.report.completed);
    assert.ok(error.report.queues.length === 1);
    assert.equal(error.report.queues[0].partial, true);
    return true;
  });
  assert.deepEqual(await fixtures(), before, "deadline leaked its real Git fixture");
});
