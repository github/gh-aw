"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { performance } = require("node:perf_hooks");
const { setImmediate: yieldTurn } = require("node:timers/promises");
const { AT, LocalGitHub, OWNER, PROTOCOL_LIMITS, REPO, dispatchParameters, queueActor, stressPolicy } = require("./work-queue-stress-git.cjs");
const { canonical, canonicalBytes, utf8Compare } = require("../../actions/setup/js/work_queue_codec.cjs");
const { CHECKPOINT_AFTER_LINES, LEDGER_BYTES, RETAINED_REQUESTS } = require("../../actions/setup/js/work_queue_limits.cjs");
const { gateKey } = require("../../actions/setup/js/work_queue_graph.cjs");
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

function terminal(work) {
  return work.state === "completed" || work.state === "cancelled";
}

function sameRecord(actual, expected, message) {
  assert.ok(actual, message);
  assert.equal(canonical(actual), canonical(expected), message);
}

/**
 * Independent oracle for one Checkpoint commit of `before` (the projection it folds): it keeps
 * every live record and what a live record still reads, nothing else, and only the request
 * window of the folded log. Its size is therefore bounded by the live set plus that window.
 * @returns {{bytes: number, requests: number, works: number, claims: number}}
 */
function assertBoundedCheckpoint(before, checkpoint) {
  const [operation] = checkpoint.operations;
  assert.equal(operation.kind, "Checkpoint");
  assert.equal(operation.prior_tip, before.tip);
  const restored = replayTransactions([checkpoint]);
  // Nothing a future commit reads is lost: non-terminal Work and their Work predecessors.
  const reads = new Set();
  for (const work of before.works.values()) {
    if (terminal(work)) continue;
    reads.add(work.work_id);
    for (const edge of work.depends_on) if (edge.kind === "work") reads.add(edge.work_id);
  }
  for (const id of reads) sameRecord(restored.works.get(id), before.works.get(id), `checkpoint lost live Work ${id}`);
  // Nothing else stays, except terminal Work whose backing Issue still awaits its terminal comment.
  const gates = new Set();
  for (const [id, work] of restored.works) {
    sameRecord(work, before.works.get(id), `checkpoint invented or changed Work ${id}`);
    if (!reads.has(id)) assert.ok(work.backing_issue && work.issue_comment === undefined, `checkpoint retained unread terminal Work ${id}`);
    sameRecord(restored.workCreators.get(id), before.workCreators.get(id), `checkpoint lost the creator of Work ${id}`);
    for (const edge of work.depends_on) if (edge.kind !== "work") gates.add(gateKey(edge.resource, edge.condition));
  }
  assert.equal(restored.workCreators.size, restored.works.size, "checkpoint kept creators of folded Work");
  // Claims and their sole Dispatches follow their Work.
  let claims = 0;
  for (const [id, claim] of before.claims) {
    if (!restored.works.has(claim.work_id)) {
      assert.equal(restored.claims.has(id), false, `checkpoint kept Claim ${id} of folded Work`);
      continue;
    }
    claims++;
    sameRecord(restored.claims.get(id), claim, `checkpoint lost Claim ${id}`);
    sameRecord(restored.dispatches.get(claim.dispatch_id), before.dispatches.get(claim.dispatch_id), `checkpoint lost Dispatch ${claim.dispatch_id}`);
  }
  assert.equal(restored.claims.size, claims);
  assert.equal(restored.dispatches.size, claims, "one Dispatch per retained Claim");
  for (const key of gates) if (before.observations.has(key)) sameRecord(restored.observations.get(key), before.observations.get(key), "checkpoint lost a read gate Observation");
  for (const key of restored.observations.keys()) assert.ok(gates.has(key), "checkpoint kept an Observation no retained Work reads");
  // Request window: the most recent RETAINED_REQUESTS of the folded log, never inherited ones.
  const { requests } = operation.state;
  assert.ok(requests.length <= RETAINED_REQUESTS, `checkpoint retains ${requests.length} > ${RETAINED_REQUESTS} request IDs`);
  const window = before.transactions.slice(-RETAINED_REQUESTS).map(commit => commit.request.id);
  assert.deepEqual([...requests].sort(utf8Compare), window.sort(utf8Compare), "checkpoint request window differs from the folded log's most recent requests");
  assert.equal(restored.requests.size, requests.length + 1, "the checkpoint's own request joins its window");
  return { bytes: canonicalBytes(checkpoint) + 1, requests: requests.length, works: restored.works.size, claims };
}

/** Build a Checkpoint of `state`, strictly re-read its bytes as persisted, and check it is bounded. */
function verifiedCheckpoint(state, actor) {
  const line = `${canonical(checkpointCommit(state, actor, AT))}\n`;
  const [commit] = replayTransactionLog(line).transactions;
  const bounded = assertBoundedCheckpoint(state, commit);
  assert.equal(bounded.bytes, Buffer.byteLength(line));
  return bounded;
}

function retainedPolicy() {
  // One attempt per historical Work: its prelaunch launch_failed Release exhausts it.
  return stressPolicy({ default: { concurrency: 256, per_account_limit: 0, max_attempts: 1 }, probe: { concurrency: 16, per_account_limit: 0, max_attempts: 3 } });
}

/**
 * Append one historical batch to `state` in place and return the bytes it adds. Every commit
 * is built by production planners (submit normalization, planDispatch, releaseOperations) and
 * applied once through production replay: submit, dispatch_next (one Claim per Dispatch) and
 * one dispatcher launch commit per Dispatch recording its prelaunch failure.
 */
function appendHistoryBatch(state, policy, prefix, batch, count) {
  let bytes = 0;
  const append = (actor, request, operations) => {
    const commit = buildCommit(state, actor, request, operations, AT);
    applyCommit(state, commit);
    bytes += canonicalBytes(commit) + 1;
  };
  const producer = queueActor("producer");
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
  return bytes;
}

/**
 * Build lifetime history on the real Git queue the way it accrues in production: batches are
 * appended to the current head and, whenever the log passes CHECKPOINT_AFTER_LINES, the epoch
 * is persisted and maintenance's production checkpoint folds it. Each checkpoint is checked to
 * be bounded by the live set plus the request window, and the next epoch builds on the head
 * read back from Git. The first batch that would exceed the ledger bound is rolled back and
 * reported. The final (tail) epoch is persisted unfolded.
 */
async function buildRetainedHistory({ host, args, genesis, requested, policy, checkDeadline = () => {} }) {
  let current = genesis;
  let state = genesis.state;
  let ledgerBytes = Buffer.byteLength(genesis.contents);
  let maxLedgerBytes = ledgerBytes;
  let validationMs = 0;
  let checkpointMs = 0;
  let accepted = 0;
  let tail = 0;
  let batch = 0;
  let saturation = null;
  const epochs = [];
  const persist = async message => {
    const at = performance.now();
    const content = serializeTransactionLog(state.transactions);
    validationMs += performance.now() - at;
    assert.equal(Buffer.byteLength(content), ledgerBytes);
    await persistFixture(host, args.branch, current, content, message);
  };
  while (accepted < requested) {
    checkDeadline();
    const count = Math.min(BATCH, requested - accepted);
    const committed = state.transactions.length;
    const added = appendHistoryBatch(state, policy, args.branch, batch, count);
    if (ledgerBytes + added > LEDGER_BYTES) {
      saturation = {
        code: "ledger_limit",
        message: `the next ${count} historical Work would exceed the ${LEDGER_BYTES}-byte ledger bound`,
        accepted_history_work: accepted,
        refused_batch: count,
        requested_history_work: requested,
        unconstructed_history_work: requested - accepted,
      };
      state = replayTransactions(state.transactions.slice(0, committed));
      break;
    }
    ledgerBytes += added;
    maxLedgerBytes = Math.max(maxLedgerBytes, ledgerBytes);
    accepted += count;
    tail += count;
    batch++;
    if (state.transactions.length > CHECKPOINT_AFTER_LINES) {
      await persist(`Persist retained history epoch ${epochs.length}`);
      const at = performance.now();
      // Production maintenance: the reconciler checkpoints once the log passes CHECKPOINT_AFTER_LINES.
      const reconciler = queueActor("reconciler", String(5000 + epochs.length));
      const published = await publishWorkQueueCheckpoint({ ...args, actor: reconciler, minimumLines: CHECKPOINT_AFTER_LINES, now: () => AT });
      assert.equal(published.checkpointed, true, "maintenance must checkpoint a log past CHECKPOINT_AFTER_LINES");
      assert.equal(projectionDigest(published.state), projectionDigest(state), "the persisted epoch differs from the built history");
      current = await readWorkQueueLog(args);
      assert.equal(current.transactions.length, 1);
      const bounded = assertBoundedCheckpoint(published.state, current.transactions[0]);
      assert.equal(bounded.bytes, Buffer.byteLength(current.contents));
      checkpointMs += performance.now() - at;
      epochs.push({ lines: published.state.transactions.length, ledger_bytes: ledgerBytes, folded_history_work: tail, checkpoint_bytes: bounded.bytes, checkpoint_requests: bounded.requests, checkpoint_works: bounded.works });
      state = current.state;
      ledgerBytes = bounded.bytes;
      tail = 0;
    }
    if (batch % 8 === 0) await yieldTurn();
  }
  await persist("Persist retained history tail epoch");
  return { accepted, tail, saturation, epochs, ledgerBytes, maxLedgerBytes, validationMs, checkpointMs };
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
    requested_unit: "lifetime historical Work, each with one Claim, built across checkpoint epochs; live probes and lifecycle smoke reported separately",
    submitted: 0,
    claimed: 0,
    completed: 0,
    cancelled: 0,
    retained_entities: 0,
    history_entities: 0,
    checkpoint_epochs: 0,
    max_checkpoint_bytes: 0,
    max_checkpoint_requests: 0,
    max_checkpoint_works: 0,
    max_ledger_bytes: 0,
    probe_work: PROBES,
    branch_count: options.skipSmoke ? 1 : 2,
    history_branch_count: 1,
    planned_history_branch_count: 1,
    history_queue_item_bound: options.historyQueueItems,
    limits: PROTOCOL_LIMITS,
    scope: "lifetime history on one queue across production checkpoint epochs, each checkpoint bounded by the live set plus the request window; NOT 100000 completed Work or 100000 live/pending Work",
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
    const fixture = await buildRetainedHistory({ host, args, genesis, requested: options.items, policy, checkDeadline });
    report.fixture_validation_ms = fixture.validationMs;
    report.epoch_checkpoint_ms = fixture.checkpointMs;
    report.fixture_generation_ms = performance.now() - generatedAt - fixture.validationMs - fixture.checkpointMs;
    report.submitted = fixture.accepted;
    report.claimed = fixture.accepted;
    report.cancelled = fixture.accepted;
    report.saturation = fixture.saturation;
    report.checkpoint_epochs = fixture.epochs.length;
    for (const epoch of fixture.epochs) {
      report.max_checkpoint_bytes = Math.max(report.max_checkpoint_bytes, epoch.checkpoint_bytes);
      report.max_checkpoint_requests = Math.max(report.max_checkpoint_requests, epoch.checkpoint_requests);
      report.max_checkpoint_works = Math.max(report.max_checkpoint_works, epoch.checkpoint_works);
    }
    report.max_ledger_bytes = fixture.maxLedgerBytes;
    report.tail_history_work = fixture.tail;
    checkDeadline();
    const coldAt = performance.now();
    let cold = await readWorkQueueLog(args);
    report.cold_git_read_replay_ms = performance.now() - coldAt;
    const digest = projectionDigest(cold.state);
    assert.equal(projectionDigest(replayTransactions(cold.transactions)), digest, "replaying the same commits twice changed the projection");
    // Earlier epochs are folded: the head holds the live probes plus the unfolded tail epoch.
    report.retained_entities = cold.state.works.size + cold.state.claims.size;
    report.history_entities = 2 * fixture.accepted;
    assert.equal(cold.state.works.size, fixture.tail + PROBES);
    assert.equal(cold.state.claims.size, fixture.tail);
    assert.equal(cold.state.stats.cancelled, fixture.tail);
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
              expected_work: fixture.tail + PROBES,
              expected_claims: fixture.tail,
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
    // The final checkpoint folds the tail epoch too; Git history keeps the prior content.
    const checkpointAt = performance.now();
    const administrator = queueActor("administrator");
    const bounded = verifiedCheckpoint(cold.state, administrator);
    report.checkpoint_bytes = bounded.bytes;
    report.checkpoint_requests = bounded.requests;
    for (const verification of report.history_worker_verifications || []) assert.equal(verification.checkpoint_bytes, report.checkpoint_bytes, "history workers built a different checkpoint");
    cold = null;
    const published = await publishWorkQueueCheckpoint({ ...args, actor: administrator, now: () => AT });
    assert.equal(published.checkpointed, true);
    assert.equal(projectionDigest(published.state), digest, "the checkpoint folded a different head");
    const restored = await readWorkQueueLog(args);
    assert.equal(restored.transactions.length, 1);
    assert.equal(Buffer.byteLength(restored.contents), report.checkpoint_bytes);
    assert.deepEqual(assertBoundedCheckpoint(published.state, restored.transactions[0]), bounded);
    assert.equal(restored.state.works.size, PROBES, "only live probes survive the final fold");
    report.checkpoint_persisted = true;
    report.checkpoint_ms = performance.now() - checkpointAt;
    report.max_checkpoint_bytes = Math.max(report.max_checkpoint_bytes, bounded.bytes);
    report.max_checkpoint_requests = Math.max(report.max_checkpoint_requests, bounded.requests);
    report.max_checkpoint_works = Math.max(report.max_checkpoint_works, bounded.works);
    report.checkpoint = "maintenance checkpoints after CHECKPOINT_AFTER_LINES; each root line keeps only the live set, what it reads, and the request window; folded history stays in Git";
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
        history_entities: report.history_entities,
        checkpoint_epochs: report.checkpoint_epochs,
        ledger_bytes: report.ledger_bytes,
        max_ledger_bytes: report.max_ledger_bytes,
        checkpoint_bytes: report.checkpoint_bytes,
        max_checkpoint_bytes: report.max_checkpoint_bytes,
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
    requested_unit: "lifetime historical Work, each with one Claim, built across checkpoint epochs; probes and completed lifecycle smoke counted separately",
    submitted: 0,
    claimed: 0,
    completed: 0,
    cancelled: 0,
    retained_entities: 0,
    history_entities: 0,
    checkpoint_epochs: 0,
    max_checkpoint_bytes: 0,
    max_checkpoint_requests: 0,
    max_checkpoint_works: 0,
    max_ledger_bytes: 0,
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
        "history_entities",
        "checkpoint_epochs",
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
      for (const key of ["max_checkpoint_bytes", "max_checkpoint_requests", "max_checkpoint_works", "max_ledger_bytes"]) report[key] = Math.max(report[key], current[key]);
      for (const [key, value] of Object.entries(current.git_api_metrics)) report.git_api_metrics[key] = (report.git_api_metrics[key] || 0) + value;
      report.ledger_bytes_total += current.ledger_bytes_total;
      report.max_queue_ledger_bytes = Math.max(report.max_queue_ledger_bytes, current.max_ledger_bytes);
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
    assert.equal(report.history_entities, 2 * report.submitted);
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

module.exports = { BATCH, PROBES, appendHistoryBatch, assertBoundedCheckpoint, buildRetainedHistory, projectionDigest, retainedPolicy, runRetainedSimulator, runSingleRetainedSimulator, verifiedCheckpoint };
