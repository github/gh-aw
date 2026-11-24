"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { performance } = require("node:perf_hooks");
const { setImmediate: yieldTurn } = require("node:timers/promises");
const { AT, LocalGitHub, OWNER, PROTOCOL_LIMITS, REPO, dispatchParameters, queueActor, stressPolicy } = require("./work-queue-stress-git.cjs");
const { canonical, canonicalBytes, utf8Compare } = require("../../actions/setup/js/work_queue_codec.cjs");
const { LEDGER_BYTES } = require("../../actions/setup/js/work_queue_limits.cjs");
const { normalizeSubmitParameters } = require("../../actions/setup/js/work_queue_intents.cjs");
const { releaseOperations } = require("../../actions/setup/js/work_queue_binding.cjs");
const { publishWorkQueueCheckpoint, publishWorkQueueRequest, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { planDispatch } = require("../../actions/setup/js/work_queue_scheduler.cjs");
const { applyCommit, buildCommit, checkpointCommit, newRequest, replayTransactionLog, replayTransactions, serializeProjection, serializeTransactionLog } = require("../../actions/setup/js/work_queue_replay.cjs");

const PROBES = 8;
const BATCH = 128;
const ENTITY_FIELDS = ["works", "claims", "dispatches", "observations", "work_creators"];

/**
 * Digest of the queue's entities, record by record (one giant JSON string multiplies live
 * memory for a near-64-MiB fixture). Log position (tip, commit ordinal, request set) is
 * excluded, so a checkpoint restore of the same projection has the same digest.
 */
function projectionDigest(state) {
  const hash = createHash("sha256");
  const projection = serializeProjection(state);
  hash.update(`repository\n${canonical(projection.repository)}\n`);
  for (const field of ENTITY_FIELDS) {
    hash.update(`${field}\n`);
    const value = projection[field];
    for (const key of Object.keys(value).sort(utf8Compare)) hash.update(`${canonical(key)}:${canonical(value[key])}\n`);
  }
  return hash.digest("hex");
}

// Both v4 codecs bound every JSON array; a Checkpoint's `requests` array holds every committed request ID.
const JSON_ARRAY_MEMBERS = 16384;

/**
 * The one root line a Checkpoint of `state` would write. A queue whose lifetime request
 * count exceeds the codec's array cap cannot be checkpointed at all; name that cause.
 */
function serializedCheckpoint(state, actor) {
  const commit = checkpointCommit(state, actor, AT);
  try {
    return `${canonical(commit)}\n`;
  } catch (error) {
    // The line lists the requests committed so far; its own request joins the set on restore.
    const requests = state.requests.size;
    if (error?.code !== "resource_limit" || requests <= JSON_ARRAY_MEMBERS) throw error;
    throw Object.assign(
      new Error(
        `checkpoint_request_retention: a v4 Checkpoint retains all ${requests} committed request IDs in one array, ` +
          `above the ${JSON_ARRAY_MEMBERS}-member JSON array cap of both codecs (${error.message}); this queue can no longer be checkpointed`
      ),
      { code: "checkpoint_request_retention", retained_requests: requests }
    );
  }
}

/** Checkpoint bytes, restored through the strict reader, must reproduce the projection digest. */
function verifiedCheckpoint(state, actor, digest) {
  const line = serializedCheckpoint(state, actor);
  assert.equal(projectionDigest(replayTransactionLog(line)), digest, "checkpoint restore changed the projection");
  return Buffer.byteLength(line);
}

function retainedPolicy() {
  // One attempt per historical Work: its prelaunch launch_failed Release exhausts it.
  return stressPolicy({ default: { concurrency: 256, per_account_limit: 0, max_attempts: 1 }, probe: { concurrency: 16, per_account_limit: 0, max_attempts: 3 } });
}

/**
 * Append retained history to the genesis projection in place. Every commit is built by
 * production planners (submit normalization, planDispatch, releaseOperations) and applied
 * once through production replay; per 128 Work: submit, dispatch_next (one Claim per
 * Dispatch) and one dispatcher launch commit per Dispatch recording its prelaunch failure.
 * The first batch that would exceed the ledger bound is rolled back and reported.
 */
async function buildRetainedHistory(genesis, requested, policy, checkDeadline = () => {}) {
  let state = genesis.state;
  let ledgerBytes = Buffer.byteLength(genesis.contents);
  const prefix = genesis.branch;
  const producer = queueActor("producer");
  let accepted = 0;
  let batch = 0;
  let saturation = null;
  while (accepted < requested) {
    checkDeadline();
    const count = Math.min(BATCH, requested - accepted);
    const committed = state.transactions.length;
    const before = ledgerBytes;
    const append = (actor, request, operations) => {
      const commit = buildCommit(state, actor, request, operations, AT);
      applyCommit(state, commit);
      ledgerBytes += canonicalBytes(commit) + 1;
    };
    const works = normalizeSubmitParameters({ nodes: Array.from({ length: count }, (_, index) => ({ graph_id: `${prefix}-g${batch}`, node_key: `n${index}`, payload: {} })) }, policy);
    append(producer, newRequest("submit", producer, { work_ids: works.map(work => work.work_id) }), works);
    // Each grant is its own dispatcher run; identical parameters from one run are one request.
    const dispatcher = queueActor("dispatcher", String(1000 + batch));
    const parameters = dispatchParameters(policy, "default", count);
    const grant = newRequest("dispatch_next", dispatcher, parameters);
    const decision = planDispatch(state, parameters, grant.id);
    assert.equal(decision.operations.length, count, "historical batch must be granted in full");
    append(dispatcher, grant, decision.operations);
    for (const dispatchId of decision.dispatches) append(dispatcher, newRequest("launch", dispatcher, { dispatch_id: dispatchId }), releaseOperations(state, dispatchId, "launch_failed", policy));
    if (ledgerBytes > LEDGER_BYTES) {
      saturation = {
        code: "ledger_limit",
        message: `the next ${count} historical Work would exceed the ${LEDGER_BYTES}-byte ledger bound`,
        accepted_history_work: accepted,
        refused_batch: count,
        requested_history_work: requested,
        unconstructed_history_work: requested - accepted,
      };
      state = replayTransactions(state.transactions.slice(0, committed));
      ledgerBytes = before;
      break;
    }
    accepted += count;
    batch++;
    if (batch % 8 === 0) await yieldTurn();
  }
  return { transactions: state.transactions, ledgerBytes, accepted, saturation };
}

async function persistFixture(host, branch, current, content, message) {
  const common = { owner: OWNER, repo: REPO };
  const git = host.client.rest.git;
  const blob = await git.createBlob({ ...common, content, encoding: "utf-8" });
  const tree = await git.createTree({ ...common, base_tree: current.treeSha, tree: [{ path: "work-queue.jsonl", type: "blob", mode: "100644", sha: blob.data.sha }] });
  const commit = await git.createCommit({ ...common, message, tree: tree.data.sha, parents: [current.sha] });
  await git.updateRef({ ...common, ref: `heads/${branch}`, sha: commit.data.sha, force: false });
}

async function runSingleRetainedSimulator(options) {
  const started = performance.now();
  const directory = await fs.mkdtemp(path.join(process.cwd(), ".work-queue-stress-retained-"));
  const host = new LocalGitHub(path.join(directory, "queue.git"), { seed: options.seed });
  let maxRss = process.memoryUsage().rss;
  const sample = () => (maxRss = Math.max(maxRss, process.memoryUsage().rss));
  const timer = setInterval(sample, 100);
  const report = {
    ok: false,
    mode: "retained",
    requested: options.items,
    requested_history_entities: options.items * 2,
    requested_unit: "historical Work, each paired with one retained Claim; live probes and lifecycle smoke reported separately",
    submitted: 0,
    claimed: 0,
    completed: 0,
    cancelled: 0,
    retained_entities: 0,
    retained_history_entities: 0,
    probe_work: PROBES,
    branch_count: options.skipSmoke ? 1 : 2,
    history_branch_count: 1,
    planned_history_branch_count: 1,
    history_queue_item_bound: options.historyQueueItems,
    limits: PROTOCOL_LIMITS,
    scope: "cold-validated bounded retained fixture scale, NOT 100000 completed Work or 100000 live/pending Work",
    fixture_batch: BATCH,
    saturation: null,
  };
  const checkDeadline = () => {
    if (performance.now() - started > options.timeoutSeconds * 1000) throw new Error(`retained simulator deadline exceeded (${options.timeoutSeconds}s)`);
  };
  try {
    // The real worker-thread publisher is measured separately from efficient
    // fixture generation; neither metric masquerades as the other.
    report.lifecycle_smoke = options.skipSmoke
      ? { ok: true, completed: 0, queues: [], branch_count: 0 }
      : await require("./work-queue-stress.cjs").runSimulator({
          ...options,
          historyItems: undefined,
          mode: "lifecycle",
          items: 16,
          queueItems: 32,
          timeoutSeconds: Math.min(90, options.timeoutSeconds),
        });
    checkDeadline();
    await host.initialize();
    const policy = retainedPolicy();
    const branch = `retained-${options.seed}-${options.historyShard || 0}`;
    const args = { githubClient: host.client, owner: OWNER, repo: REPO, branch };
    const probes = normalizeSubmitParameters({ nodes: Array.from({ length: PROBES }, (_, index) => ({ graph_id: `${branch}-probes`, node_key: `p${index}`, pool: "probe", payload: { probe: index } })) }, policy);
    await publishWorkQueueRequest({
      ...args,
      actor: queueActor("producer"),
      request: { kind: "submit", parameters: { work_ids: probes.map(work => work.work_id) } },
      operationsFor: state => probes.filter(work => !state.works.has(work.work_id)),
      now: () => AT,
    });
    const genesis = await readWorkQueueLog(args);
    const generatedAt = performance.now();
    const fixture = await buildRetainedHistory(genesis, options.items, policy, checkDeadline);
    report.fixture_generation_ms = performance.now() - generatedAt;
    report.submitted = fixture.accepted;
    report.claimed = fixture.accepted;
    report.cancelled = fixture.accepted;
    report.saturation = fixture.saturation;
    checkDeadline();
    const replayAt = performance.now();
    let content = serializeTransactionLog(fixture.transactions);
    report.fixture_validation_ms = performance.now() - replayAt;
    assert.equal(Buffer.byteLength(content), fixture.ledgerBytes);
    await persistFixture(host, branch, genesis, content, "Persist checked large retained stress fixture");
    fixture.transactions = [];
    content = null;
    checkDeadline();
    const coldAt = performance.now();
    let cold = await readWorkQueueLog(args);
    report.cold_git_read_replay_ms = performance.now() - coldAt;
    const digest = projectionDigest(cold.state);
    assert.equal(projectionDigest(replayTransactions(cold.transactions)), digest, "replaying the same commits twice changed the projection");
    report.retained_entities = cold.state.works.size + cold.state.claims.size;
    report.retained_history_entities = 2 * fixture.accepted;
    assert.equal(cold.state.works.size, fixture.accepted + PROBES);
    assert.equal(cold.state.claims.size, fixture.accepted);
    assert.equal(cold.state.stats.cancelled, fixture.accepted);
    assert.equal(cold.state.stats.available, PROBES);
    assert.equal(cold.state.stats.completed, 0);
    for (const claim of cold.state.claims.values()) assert.equal(claim.state, "cancelled");
    for (const dispatch of cold.state.dispatches.values()) {
      assert.equal(dispatch.state, "closed");
      assert.equal(dispatch.launch, null, "a prelaunch release records no launch");
      assert.equal(dispatch.claims.length, 1);
    }
    for (const work of cold.state.works.values()) if (work.pool === "default") assert.equal(work.cancellation_reason, "attempts_exhausted");
    const probeParameters = dispatchParameters(policy, "probe", PROBES);
    const probeRequest = newRequest("dispatch_next", queueActor("dispatcher", "1"), probeParameters).id;
    const planAt = performance.now();
    let selected;
    for (let index = 0; index < 10; index++) {
      const plan = planDispatch(cold.state, probeParameters, probeRequest);
      assert.equal(plan.operations.length, PROBES);
      const current = canonical(plan.operations);
      if (selected) assert.equal(current, selected, "large retained-history planning changed its deterministic prefix");
      selected = current;
    }
    report.ten_batch_plans_ms = performance.now() - planAt;
    report.selected_probe_claims = PROBES;
    report.ledger_bytes = Buffer.byteLength(cold.contents);
    report.pending_work = cold.state.stats.available;
    report.transactions = cold.transactions.length;
    report.checkpoint_retained_requests = cold.state.requests.size;
    report.historical_reservations = cold.state.dispatches.size;
    report.projection_sha256 = digest;
    if (options.verifyHistoryWorkers) {
      const { Publishers } = require("./work-queue-stress.cjs");
      const count = Math.min(4, options.workers);
      const readers = new Publishers(host, count, rss => (maxRss = Math.max(maxRss, rss)));
      let readerDeadline;
      try {
        report.history_validation_workers = count;
        report.history_worker_verifications = await Promise.race([
          readers.runAll(
            Array.from({ length: count }, (_, index) => ({
              kind: "history_check",
              id: `${branch}:reader:${index}`,
              branch,
              expected_work: fixture.accepted + PROBES,
              expected_claims: fixture.accepted,
              probe_parameters: probeParameters,
              probe_request_id: probeRequest,
            }))
          ),
          new Promise((_, reject) => {
            const remaining = options.timeoutSeconds * 1000 - (performance.now() - started);
            readerDeadline = setTimeout(() => reject(new Error("history worker validation deadline exceeded")), Math.max(1, remaining));
          }),
        ]);
        for (const verification of report.history_worker_verifications) {
          assert.equal(verification.projection_sha256, digest);
          assert.equal(verification.ledger_bytes, report.ledger_bytes);
          assert.equal(verification.completed, 0);
        }
      } finally {
        clearTimeout(readerDeadline);
        await readers.close();
      }
    }
    // A checkpoint replaces the log with one root line; Git history keeps the prior content.
    const checkpointAt = performance.now();
    const administrator = queueActor("administrator");
    report.checkpoint_bytes = verifiedCheckpoint(cold.state, administrator, digest);
    for (const verification of report.history_worker_verifications || []) assert.equal(verification.checkpoint_bytes, report.checkpoint_bytes, "history workers built a different checkpoint");
    cold = null;
    try {
      const published = await publishWorkQueueCheckpoint({ ...args, actor: administrator, now: () => AT });
      assert.equal(published.checkpointed, true);
      const restored = await readWorkQueueLog(args);
      assert.equal(restored.transactions.length, 1);
      assert.equal(Buffer.byteLength(restored.contents), report.checkpoint_bytes);
      assert.equal(projectionDigest(restored.state), digest, "persisted checkpoint changed the projection");
      report.checkpoint_persisted = true;
    } catch (error) {
      if (error.code !== "ledger_limit") throw error;
      // The checkpoint is refused like any other oversized write; the log stays as it was.
      assert.equal(projectionDigest((await readWorkQueueLog(args)).state), digest);
      report.checkpoint_persisted = false;
      report.checkpoint_refusal = error.code;
    }
    report.checkpoint_ms = performance.now() - checkpointAt;
    report.checkpoint = "one root Checkpoint line restored to the same entity projection; no history rewritten in Git";
    report.fixture_native_posts = host.metrics.native_posts;
    report.queues = [
      ...report.lifecycle_smoke.queues.map(queue => ({ ...queue, kind: "concurrent-lifecycle-smoke" })),
      {
        branch,
        kind: "retained-fixture",
        requested: options.items,
        submitted: fixture.accepted,
        claimed: fixture.accepted,
        completed: 0,
        cancelled: fixture.accepted,
        probe_work: PROBES,
        retained_entities: report.retained_entities,
        retained_history_entities: report.retained_history_entities,
        ledger_bytes: report.ledger_bytes,
        checkpoint_bytes: report.checkpoint_bytes,
        cold_replay_ms: report.cold_git_read_replay_ms,
        fixture_generation_ms: report.fixture_generation_ms,
        fixture_validation_ms: report.fixture_validation_ms,
        ten_batch_plans_ms: report.ten_batch_plans_ms,
        checkpoint_ms: report.checkpoint_ms,
        projection_sha256: report.projection_sha256,
      },
    ];
    report.ledger_bytes_total = report.queues.reduce((sum, queue) => sum + queue.ledger_bytes, 0);
    await host.git(["fsck", "--strict", "--no-dangling"]);
    checkDeadline();
    report.ok = true;
    return report;
  } catch (error) {
    if (error.code === "checkpoint_request_retention") report.checkpoint_refusal = error.code;
    error.report = report;
    throw error;
  } finally {
    clearInterval(timer);
    await host.close();
    report.git_api_metrics = { ...host.metrics };
    sample();
    report.elapsed_ms = performance.now() - started;
    report.max_rss_bytes = Math.max(maxRss, process.resourceUsage().maxRSS * 1024);
    report.max_rss_scope = "Node process lifetime including worker threads, excluding Git subprocesses";
    await fs.rm(directory, { recursive: true, force: true });
  }
}

async function runRetainedSimulator(options) {
  const historyBranches = Math.ceil(options.items / options.historyQueueItems);
  if (historyBranches === 1) return runSingleRetainedSimulator(options);
  const started = performance.now();
  const report = {
    ok: false,
    mode: "retained",
    requested: options.items,
    requested_history_entities: options.items * 2,
    requested_unit: "historical Work, each paired with one retained Claim; probes and completed lifecycle smoke counted separately",
    submitted: 0,
    claimed: 0,
    completed: 0,
    cancelled: 0,
    retained_entities: 0,
    retained_history_entities: 0,
    probe_work: 0,
    branch_count: 0,
    history_branch_count: 0,
    planned_history_branch_count: historyBranches,
    history_queue_item_bound: options.historyQueueItems,
    limits: PROTOCOL_LIMITS,
    scope: "independent bounded retained-history branches; NOT single-queue 100000 Work support or newly completed worker throughput",
    retention: "one isolated local Git fixture per history branch; cold-verified/checkpointed/fsck-checked then removed; measurements retained",
    fixture_batch: BATCH,
    fixture_native_posts: 0,
    fixture_generation_ms: 0,
    fixture_validation_ms: 0,
    cold_git_read_replay_ms: 0,
    ten_batch_plans_ms: 0,
    batch_plan_count: 0,
    checkpoint_ms: 0,
    historical_reservations: 0,
    ledger_bytes_total: 0,
    max_queue_ledger_bytes: 0,
    pending_work: 0,
    saturation: null,
    git_api_metrics: {},
    queues: [],
  };
  const projectionHash = createHash("sha256");
  try {
    for (let index = 0; index < historyBranches; index++) {
      const remaining = options.timeoutSeconds - (performance.now() - started) / 1000;
      if (remaining <= 0) throw new Error(`retained simulator deadline exceeded (${options.timeoutSeconds}s)`);
      const requested = Math.min(options.historyQueueItems, options.items - report.submitted);
      const current = await runSingleRetainedSimulator({ ...options, items: requested, skipSmoke: index !== 0, historyShard: index, timeoutSeconds: Math.max(1, Math.floor(remaining)) });
      if (index === 0) report.lifecycle_smoke = current.lifecycle_smoke;
      if (current.history_worker_verifications) {
        report.history_validation_workers = current.history_validation_workers;
        report.history_worker_verifications ||= [];
        report.history_worker_verifications.push(...current.history_worker_verifications.map(verification => ({ ...verification, branch: current.queues.at(-1).branch })));
      }
      report.branch_count += current.branch_count;
      report.history_branch_count++;
      report.batch_plan_count += 10;
      report.queues.push(...current.queues);
      for (const key of [
        "submitted",
        "claimed",
        "cancelled",
        "retained_entities",
        "retained_history_entities",
        "probe_work",
        "fixture_generation_ms",
        "fixture_validation_ms",
        "cold_git_read_replay_ms",
        "ten_batch_plans_ms",
        "checkpoint_ms",
        "historical_reservations",
        "pending_work",
      ])
        report[key] += current[key];
      for (const [key, value] of Object.entries(current.git_api_metrics)) report.git_api_metrics[key] = (report.git_api_metrics[key] || 0) + value;
      report.ledger_bytes_total += current.ledger_bytes_total;
      report.max_queue_ledger_bytes = Math.max(report.max_queue_ledger_bytes, current.ledger_bytes);
      projectionHash.update(`${index}:${current.projection_sha256}\n`);
      if (current.saturation) {
        report.saturation = {
          ...current.saturation,
          accepted_branch_history_work: current.submitted,
          accepted_history_work: report.submitted,
          requested_history_work: options.items,
          unconstructed_history_work: options.items - report.submitted,
        };
        break;
      }
    }
    if (!report.saturation) assert.equal(report.submitted, options.items);
    assert.equal(report.claimed, report.submitted);
    assert.equal(report.cancelled, report.submitted);
    assert.equal(report.retained_entities, 2 * report.submitted + report.probe_work);
    assert.equal(report.retained_history_entities, 2 * report.submitted);
    assert.equal(report.completed, 0);
    report.projection_sha256 = projectionHash.digest("hex");
    report.ok = true;
    return report;
  } catch (error) {
    if (error.report) report.failed_branch = error.report;
    error.report = report;
    throw error;
  } finally {
    report.elapsed_ms = performance.now() - started;
    report.validated_history_pairs_per_second = report.submitted / (report.elapsed_ms / 1000);
    report.max_rss_bytes = Math.max(process.memoryUsage().rss, process.resourceUsage().maxRSS * 1024);
    report.max_rss_scope = "Node process lifetime including worker threads, excluding Git subprocesses";
  }
}

module.exports = { BATCH, JSON_ARRAY_MEMBERS, PROBES, buildRetainedHistory, projectionDigest, runRetainedSimulator, runSingleRetainedSimulator, serializedCheckpoint, verifiedCheckpoint };
