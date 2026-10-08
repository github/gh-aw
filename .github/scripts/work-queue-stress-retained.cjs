"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { createHash } = require("node:crypto");
const { performance } = require("node:perf_hooks");
const { setImmediate: yieldTurn } = require("node:timers/promises");
const { LocalGitHub, PRINCIPAL, REPOSITORY, PUBLISHER_WORKFLOW } = require("./work-queue-stress-git.cjs");
const { canonical, canonicalBytes, utf8Compare } = require("../../actions/setup/js/work_queue_codec.cjs");
const { actorFromContext, defaultPolicy } = require("../../actions/setup/js/work_queue_policy.cjs");
const { newWork } = require("../../actions/setup/js/work_queue_graph.cjs");
const { checkLedgerBudget, DEFAULT_LIMITS } = require("../../actions/setup/js/work_queue_limits.cjs");
const { authenticatePublisher } = require("../../actions/setup/js/work_queue_native.cjs");
const { initializeWorkQueue, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { applyScheduledClaim, planDispatch, planNext } = require("../../actions/setup/js/work_queue_scheduler.cjs");
const { compactTransactions, generateRequestOperations, newRequest, newState, replayTransactions, serializeProjection, serializeTransactionLog, validateCommit } = require("../../actions/setup/js/work_queue_replay.cjs");

const AT = 1800000000000;
const PROBES = 8;
const BATCH = 128;

// Hash projection records individually: a single giant projection JSON string
// unnecessarily multiplies the live memory for a near-64-MiB fixture.
function projectionDigest(state) {
  const hash = createHash("sha256");
  const projection = serializeProjection(state);
  for (const field of Object.keys(projection).sort(utf8Compare)) {
    hash.update(`${field}\n`);
    const value = projection[field];
    if (["works", "claims", "dispatches", "observations", "requests", "clocks", "observation_writes"].includes(field)) {
      for (const key of Object.keys(value).sort(utf8Compare)) hash.update(`${canonical(key)}:${canonical(value[key])}\n`);
    } else hash.update(`${canonical(value)}\n`);
  }
  return hash.digest("hex");
}

function checkedFixtureCommit(state, id, kind, actor, parameters, operations, admission) {
  const request = newRequest(id, kind, actor, parameters);
  const commit = { version: 3, id: `history-${id}`, previous: state.tip, request, actor, policy_epoch: state.policy_epoch, at: AT, operations };
  validateCommit(commit);
  const bytes = canonicalBytes(commit) + 1;
  checkLedgerBudget(state, bytes, admission);
  state.ledgerBytes += bytes;
  state.tip = commit.id;
  return commit;
}

async function buildRetainedHistory(genesis, requested, checkDeadline = () => {}) {
  const policy = genesis.state.policy;
  const actors = {
    administrator: { role: "administrator", repository: REPOSITORY, principal: PRINCIPAL },
    producer: { role: "producer", repository: REPOSITORY, principal: PRINCIPAL },
    reconciler: { role: "reconciler", repository: REPOSITORY, principal: PRINCIPAL },
  };
  const transactions = [...genesis.transactions];
  const prefix = genesis.branch;
  const probeNodes = Array.from({ length: PROBES }, (_, index) => newWork({ probe: index }, `${prefix}-probes`, `p${index}`, "probe", policy, AT));
  let tip = genesis.state.tip;
  let ledgerBytes = genesis.state.ledgerBytes;
  let clocks = new Map();
  let accepted = 0;
  let saturation = null;

  function boundedState() {
    const state = newState();
    Object.assign(state, { repository: REPOSITORY, policy, policy_epoch: genesis.state.policy_epoch, tip, ledgerBytes, clocks: new Map(clocks) });
    for (const [index, node] of probeNodes.entries()) state.works.set(node.work_id, { ...node, state: "available", position: { commit: 1, operation: index }, attempts: 0, retry_not_before: 0, barrier: "none" });
    return state;
  }

  // Fixture construction is intentionally not a publisher benchmark. Every
  // historical request/Claim is subsequently checked by a complete cold replay.
  const initial = boundedState();
  const probeRequest = newRequest(`${prefix}:probes`, "submit", actors.producer, { nodes: probeNodes });
  const probeCommit = checkedFixtureCommit(initial, probeRequest.id, "submit", actors.producer, probeRequest.parameters, probeNodes, true);
  transactions.push(probeCommit);
  tip = initial.tip;
  ledgerBytes = initial.ledgerBytes;

  while (accepted < requested) {
    checkDeadline();
    const count = Math.min(BATCH, requested - accepted);
    const state = boundedState();
    const ordinal = transactions.length;
    const nodes = Array.from({ length: count }, (_, index) => newWork({}, `${prefix}-g${Math.floor(accepted / BATCH)}`, `n${index}`, "default", policy, AT));
    try {
      const submitId = `${prefix}:s${accepted}`;
      const submitRequest = newRequest(submitId, "submit", actors.producer, { nodes });
      const submit = generateRequestOperations(state, submitRequest, actors.producer, AT, `history-${submitId}`);
      for (const [index, node] of nodes.entries()) state.works.set(node.work_id, { ...node, state: "available", position: { commit: ordinal, operation: index }, attempts: 0, retry_not_before: 0, barrier: "none" });
      const submission = checkedFixtureCommit(state, submitId, "submit", actors.producer, submitRequest.parameters, submit.operations, true);

      const grantId = `${prefix}:c${accepted}`;
      const parameters = { pool: "default", max_claims: count, max_dispatches: 16, max_bytes: DEFAULT_LIMITS.assignment_bytes };
      const grantRequest = newRequest(grantId, "dispatch_next", actors.administrator, parameters);
      const granted = generateRequestOperations(state, grantRequest, actors.administrator, AT, `history-${grantId}`);
      assert.equal(granted.operations.length, count);
      const grantCommit = { id: `history-${grantId}`, request: grantRequest };
      for (const operation of granted.operations) applyScheduledClaim(state, operation, planNext(state, "default", AT), grantCommit);
      const grant = checkedFixtureCommit(state, grantId, "dispatch_next", actors.administrator, parameters, granted.operations, true);

      const cancellations = nodes.map(node => ({ kind: "WorkCancellation", work_id: node.work_id, reason: "fixture_cancelled" }));
      const releases = [...state.dispatches.values()].map(dispatch => ({
        kind: "Release",
        dispatch_id: dispatch.dispatch_id,
        evidence: {
          kind: "prelaunch",
          source: "trusted_publisher",
          repository: REPOSITORY,
          workflow: dispatch.profile.workflow,
          ref: dispatch.profile.ref,
          principal: dispatch.profile.principal,
          checked_at: AT,
        },
      }));
      // Only a bounded budget projection is updated here. No unchecked public
      // queue state is exported; replay validates each terminal operation later.
      for (const node of nodes) state.works.get(node.work_id).state = "cancelled";
      for (const claim of state.claims.values()) claim.state = "cancelled";
      const cancellationRequest = newRequest(`${prefix}:x${accepted}`, "cancel_work", actors.administrator, { operations: cancellations });
      const cancelled = checkedFixtureCommit(state, cancellationRequest.id, "cancel_work", actors.administrator, cancellationRequest.parameters, cancellations, false);
      for (const dispatch of state.dispatches.values()) dispatch.released = true;
      const operations = releases;
      const releaseRequest = newRequest(`${prefix}:r${accepted}`, "release", actors.reconciler, { operations });
      const closure = checkedFixtureCommit(state, releaseRequest.id, "release", actors.reconciler, releaseRequest.parameters, operations, false);
      transactions.push(submission, grant, cancelled, closure);
      tip = state.tip;
      ledgerBytes = state.ledgerBytes;
      clocks = state.clocks;
      accepted += count;
    } catch (error) {
      if (error.code !== "ledger_limit") throw error;
      saturation = { code: error.code, message: error.message, accepted_history_work: accepted, refused_batch: count, requested_history_work: requested, unconstructed_history_work: requested - accepted };
      break;
    }
    if (accepted % (BATCH * 8) === 0) await yieldTurn();
  }
  return { transactions, ledgerBytes, accepted, saturation, probeNodes };
}

async function persistFixture(host, branch, current, content, message) {
  const common = { owner: "local", repo: "queue" };
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
    limits: { ...DEFAULT_LIMITS, profile_max_claims: 16 },
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
    const policy = defaultPolicy({ repository: REPOSITORY, principal: PRINCIPAL, ref: host.ref });
    policy.pools.default.profiles.default.max_claims = 16;
    policy.pools.default.logical_limit = 256;
    policy.pools.default.native_limit = 16;
    policy.pools.probe = structuredClone(policy.pools.default);
    policy.pools.probe.logical_limit = 16;
    policy.pools.probe.native_limit = 1;
    policy.producers[PRINCIPAL].pools.push("probe");
    const admin = await authenticatePublisher({ githubClient: host.client, context: host.nativeContext(), role: "administrator", workflowRef: `${REPOSITORY}/${PUBLISHER_WORKFLOW}@${host.ref}` });
    assert.equal(actorFromContext(admin).principal, PRINCIPAL);
    const branch = `retained-${options.seed}-${options.historyShard || 0}`;
    const args = { githubClient: host.client, owner: "local", repo: "queue", branch };
    await initializeWorkQueue({ ...args, context: admin, policyProposal: policy, now: () => AT });
    const genesis = await readWorkQueueLog(args);
    const generatedAt = performance.now();
    const fixture = await buildRetainedHistory(genesis, options.items, checkDeadline);
    report.fixture_generation_ms = performance.now() - generatedAt;
    report.submitted = fixture.accepted;
    report.claimed = fixture.accepted;
    report.cancelled = fixture.accepted;
    report.saturation = fixture.saturation;
    checkDeadline();
    const replayAt = performance.now();
    let state = replayTransactions(fixture.transactions);
    report.fixture_validation_ms = performance.now() - replayAt;
    assert.equal(state.ledgerBytes, fixture.ledgerBytes);
    let content = serializeTransactionLog(fixture.transactions);
    assert.equal(Buffer.byteLength(content), fixture.ledgerBytes);
    await persistFixture(host, branch, genesis, content, "Persist checked large retained stress fixture");
    fixture.transactions = [];
    content = null;
    state = null;
    checkDeadline();
    const coldAt = performance.now();
    let cold = await readWorkQueueLog(args);
    report.cold_git_read_replay_ms = performance.now() - coldAt;
    const digest = projectionDigest(cold.state);
    report.retained_entities = cold.state.works.size + cold.state.claims.size;
    report.retained_history_entities = 2 * fixture.accepted;
    assert.equal(cold.state.works.size, fixture.accepted + PROBES);
    assert.equal(cold.state.claims.size, fixture.accepted);
    assert.equal(cold.state.stats.cancelled, fixture.accepted);
    assert.equal(cold.state.stats.available, PROBES);
    assert.equal(cold.state.stats.completed, 0);
    for (const claim of cold.state.claims.values()) assert.equal(claim.state, "cancelled");
    for (const dispatch of cold.state.dispatches.values()) {
      assert.equal(dispatch.released, true);
      assert.ok(dispatch.claims.length <= 16);
    }
    const planAt = performance.now();
    let selected;
    for (let index = 0; index < 10; index++) {
      const plan = planDispatch(cold.state, { pool: "probe", max_claims: PROBES, max_dispatches: 1, max_bytes: DEFAULT_LIMITS.assignment_bytes }, { requestId: "retained-probe", commitId: "retained-probe-commit", at: AT });
      assert.equal(plan.operations.length, PROBES);
      const current = canonical(plan.operations);
      if (selected) assert.equal(current, selected, "large retained-history planning changed immutable prefix");
      selected = current;
    }
    report.ten_batch_plans_ms = performance.now() - planAt;
    report.selected_probe_claims = PROBES;
    report.ledger_bytes = cold.state.ledgerBytes;
    report.recovery_bytes_used = Math.max(0, report.ledger_bytes - DEFAULT_LIMITS.ledger_bytes);
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
              expected_work: fixture.accepted + PROBES,
              expected_claims: fixture.accepted,
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
    const compactAt = performance.now();
    const compacted = compactTransactions([...cold.transactions, cold.transactions[0], cold.transactions.at(-1)]);
    assert.equal(compacted.length, cold.transactions.length);
    assert.equal(projectionDigest(replayTransactions(compacted)), digest);
    content = serializeTransactionLog(compacted);
    assert.equal(Buffer.byteLength(content), report.ledger_bytes);
    await persistFixture(host, branch, cold, content, "Persist retained fixture safe canonicalization");
    cold = null;
    content = null;
    const reread = await readWorkQueueLog(args);
    assert.equal(projectionDigest(reread.state), digest);
    report.canonicalization_ms = performance.now() - compactAt;
    report.canonicalization = "exact duplicate elimination only; no historical Work/Claim/request pruning";
    report.fixture_generation_replays = 0;
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
        recovery_bytes_used: report.recovery_bytes_used,
        cold_replay_ms: report.cold_git_read_replay_ms,
        fixture_generation_ms: report.fixture_generation_ms,
        fixture_validation_ms: report.fixture_validation_ms,
        ten_batch_plans_ms: report.ten_batch_plans_ms,
        canonicalization_ms: report.canonicalization_ms,
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
    limits: { ...DEFAULT_LIMITS, profile_max_claims: 16 },
    scope: "independent bounded retained-history branches; NOT single-queue 100000 Work support or newly completed worker throughput",
    retention: "one isolated local Git fixture per history branch; cold-verified/canonicalized/fsck-checked then removed; measurements retained",
    fixture_batch: BATCH,
    fixture_generation_replays: 0,
    fixture_native_posts: 0,
    fixture_generation_ms: 0,
    fixture_validation_ms: 0,
    cold_git_read_replay_ms: 0,
    ten_batch_plans_ms: 0,
    batch_plan_count: 0,
    canonicalization_ms: 0,
    historical_reservations: 0,
    ledger_bytes_total: 0,
    max_queue_ledger_bytes: 0,
    recovery_bytes_used: 0,
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
        "canonicalization_ms",
        "historical_reservations",
        "recovery_bytes_used",
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

module.exports = { BATCH, PROBES, buildRetainedHistory, projectionDigest, runRetainedSimulator, runSingleRetainedSimulator };
