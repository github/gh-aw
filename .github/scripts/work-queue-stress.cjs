#!/usr/bin/env node
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { performance } = require("node:perf_hooks");
const { Worker } = require("node:worker_threads");
const { AT, LocalGitHub, OWNER, PROTOCOL_LIMITS, REPO, dispatchParameters, queueActor, stressPolicy } = require("./work-queue-stress-git.cjs");
const { assertBoundedCheckpoint, projectionDigest } = require("./work-queue-stress-retained.cjs");
const { nodeId } = require("../../actions/setup/js/work_queue_graph.cjs");
const { normalizeSubmitParameters } = require("../../actions/setup/js/work_queue_intents.cjs");
const { appendCommit, applyCommit, buildCommit, newRequest, newState, replayTransactions, serializeTransactionLog } = require("../../actions/setup/js/work_queue_replay.cjs");
const { publishWorkQueueCheckpoint, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { planDispatch } = require("../../actions/setup/js/work_queue_scheduler.cjs");

// Claims one dispatcher run grants; each Claim is its own Dispatch and worker launch.
const GRANT_CLAIMS = 8;
const SUBMIT_BATCH = 256;
const ADMISSION_REFUSALS = ["work_queue_pending_limit", "resource_limit", "ledger_limit"];

class Publishers {
  constructor(host, count, rssSample) {
    this.host = host;
    this.rssSample = rssSample;
    this.closed = false;
    this.inFlight = new Set();
    this.slots = Array.from({ length: count }, (_, index) => {
      const worker = new Worker(path.join(__dirname, "work-queue-stress-worker.cjs"), { workerData: { index } });
      const slot = { worker, index, counters: {}, active: null, failure: null };
      worker.on("message", message => this.message(slot, message));
      worker.on("error", error => {
        slot.failure = error;
        slot.active?.reject(error);
      });
      worker.on("exit", code => {
        if (!this.closed) {
          slot.failure ||= new Error(`publisher ${index} exited unexpectedly (${code})`);
          slot.active?.reject(slot.failure);
        }
      });
      return slot;
    });
  }

  async message(slot, message) {
    if (message.type === "api") {
      const operation = this.host.call(message.method, message.args, message.meta);
      this.inFlight.add(operation);
      try {
        const result = await operation;
        if (!this.closed) slot.worker.postMessage({ type: "api-result", id: message.id, result });
      } catch (error) {
        if (!this.closed) slot.worker.postMessage({ type: "api-result", id: message.id, error: { message: error.message, status: error.status, code: error.code } });
      } finally {
        this.inFlight.delete(operation);
      }
    } else if (message.type === "result") {
      slot.counters = message.counters;
      this.rssSample(message.rss);
      const active = slot.active;
      slot.active = null;
      if (!active) return;
      if (message.error) active.reject(Object.assign(new Error(message.error.message), message.error));
      else active.resolve(message.result);
    }
  }

  async runAll(jobs) {
    let next = 0;
    const results = new Array(jobs.length);
    await Promise.all(
      this.slots.map(async slot => {
        while (next < jobs.length && !this.closed) {
          if (slot.failure) throw slot.failure;
          const index = next++;
          assert.equal(slot.active, null);
          results[index] = await new Promise((resolve, reject) => {
            slot.active = { resolve, reject };
            slot.worker.postMessage({ type: "job", job: jobs[index] });
          });
        }
      })
    );
    return results;
  }

  counters() {
    const counters = {};
    for (const slot of this.slots) for (const [name, value] of Object.entries(slot.counters)) counters[name] = (counters[name] || 0) + value;
    return counters;
  }

  async close() {
    this.closed = true;
    for (const slot of this.slots) slot.active?.reject(new Error("publisher pool closing"));
    await Promise.all(this.slots.map(slot => slot.worker.terminate()));
  }
}

function optionsFor(input = {}) {
  const options = { items: 128, workers: 4, queueItems: 32, historyQueueItems: 10000, seed: 1, mode: "lifecycle", timeoutSeconds: 3600, faults: true, ...input };
  if (options.historyItems !== undefined) {
    if (!Number.isSafeInteger(options.historyItems) || options.historyItems < 2 || options.historyItems > 20000000 || options.historyItems % 2 !== 0)
      throw new Error("historyItems must be an even integer in 2..20000000 (Work plus Claim entities)");
    if (input.mode !== undefined && input.mode !== "retained") throw new Error("history-items requires retained mode");
    if (input.items !== undefined && input.items * 2 !== options.historyItems) throw new Error("items counts Work; history-items counts Work plus Claims and must equal twice items");
    options.mode = "retained";
    options.items = options.historyItems / 2;
    if (input.historyQueueItems === undefined) options.historyQueueItems = options.items;
  }
  for (const [key, min, max] of [
    ["items", 1, 10000000],
    ["workers", 2, 32],
    ["queueItems", 2, 256],
    ["historyQueueItems", 1, 10000000],
    ["seed", 0, 0xffffffff],
    ["timeoutSeconds", 1, 86400],
  ])
    if (!Number.isSafeInteger(options[key]) || options[key] < min || options[key] > max) throw new Error(`${key} must be an integer in ${min}..${max}`);
  if (!["lifecycle", "saturation", "retained", "history"].includes(options.mode)) throw new Error("mode must be lifecycle, saturation, retained or history");
  if (options.mode === "history") {
    if (options.items % 2 !== 0) throw new Error("history mode items must be even: historical Work plus retained Claim entities");
    if (input.historyQueueItems === undefined) options.historyQueueItems = options.items / 2;
  }
  if (typeof options.faults !== "boolean") throw new Error("faults must be boolean");
  return options;
}

/** One pool whose concurrency and per-account limit both bind while concurrent dispatchers grant. */
function policyFor(workers) {
  const concurrency = Math.min(64, 4 * workers);
  return stressPolicy({ default: { concurrency, per_account_limit: concurrency / 2, max_attempts: 3 } });
}

/**
 * Independent oracle for every dispatch_next commit: against the projection just before it,
 * Claims follow priority then causal FIFO among available Work, skip accounts at their
 * per-account limit, stop at pool concurrency or the request budget, and each Claim is its
 * own Dispatch. Returns how many grants the per-account limit skipped.
 */
function assertDispatchOrder(transactions) {
  const state = newState();
  let skips = 0;
  for (const commit of transactions) {
    if (commit.request.kind === "dispatch_next") {
      const parameters = commit.request.parameters;
      const held = new Map();
      let occupied = 0;
      for (const claim of state.claims.values()) {
        const work = state.works.get(claim.work_id);
        if (claim.state !== "open" || work.pool !== parameters.pool) continue;
        occupied++;
        held.set(work.fairness_key, (held.get(work.fairness_key) ?? 0) + 1);
      }
      const candidates = [...state.works.values()]
        .filter(work => work.pool === parameters.pool && work.state === "available" && Object.hasOwn(parameters.profiles, work.worker_profile))
        .sort((a, b) => a.priority - b.priority || a.position.commit - b.position.commit || a.position.operation - b.position.operation);
      const expected = [];
      for (const work of candidates) {
        if (occupied >= parameters.concurrency || expected.length >= Math.min(parameters.max_claims, parameters.max_dispatches)) break;
        const count = held.get(work.fairness_key) ?? 0;
        if (parameters.per_account_limit > 0 && count >= parameters.per_account_limit) {
          skips++;
          continue;
        }
        expected.push(work.work_id);
        occupied++;
        held.set(work.fairness_key, count + 1);
      }
      const claims = commit.operations.filter(operation => operation.kind === "Claim");
      assert.deepEqual(
        claims.map(operation => operation.work_id),
        expected,
        "Claims departed from priority/FIFO order under the pool and per-account limits"
      );
      assert.equal(new Set(claims.map(operation => operation.dispatch_id)).size, claims.length, "a Dispatch carries exactly one Claim");
    }
    applyCommit(state, commit);
  }
  return skips;
}

function assertLifecycle(state, host, { graph, start, count }) {
  assert.equal(state.works.size, count, "lost or duplicated Work");
  assert.equal(state.claims.size, count, "lost or duplicated Claim");
  assert.equal(state.dispatches.size, count, "one Dispatch per Claim");
  assert.equal(state.stats.completed, count);
  assert.equal(state.stats.available, 0);
  assert.equal(state.stats.claimed, 0);
  assert.equal(state.stats.dispatches, 0, "no Dispatch may stay open");
  const expected = new Set(Array.from({ length: count }, (_, index) => nodeId(graph, `item-${start + index}`)));
  for (const work of state.works.values()) {
    assert.ok(expected.delete(work.work_id), "unexpected or duplicated Work identity");
    assert.equal(work.state, "completed");
    assert.equal(work.attempts, 0, "a completed first attempt consumes no attempt");
    assert.equal(work.effects.length, 1);
    const receipt = host.receipts.get(work.effects[0].receipt);
    assert.equal(receipt?.work_id, work.work_id);
    assert.equal(receipt.claim_id, work.claim_id);
  }
  assert.equal(expected.size, 0);
  const accountLimitSkips = assertDispatchOrder(state.transactions);
  let duplicateRuns = 0;
  for (const dispatch of state.dispatches.values()) {
    const launch = host.launches.get(dispatch.dispatch_id);
    assert.equal(dispatch.state, "closed");
    assert.ok(launch?.run_ids.includes(dispatch.launch.run_id), "recorded launch is not a POSTed run");
    assert.equal(dispatch.launch.revision, host.ref);
    assert.deepEqual(dispatch.run, { run_id: dispatch.launch.run_id, run_attempt: 1 }, "only the recorded launch binds");
    const run = host.runs.get(dispatch.launch.run_id);
    assert.equal(run.status, "completed");
    assert.equal(run.conclusion, "success");
    const claim = state.claims.get(dispatch.claims[0]);
    const work = state.works.get(claim.work_id);
    assert.equal(claim.state, "completed");
    assert.equal(claim.terminal_commit_id, work.completion_id);
    const receipt = host.receipts.get(work.effects[0].receipt);
    assert.equal(receipt.run_id, dispatch.launch.run_id);
    assert.equal(receipt.dispatch_id, dispatch.dispatch_id);
    assert.equal(receipt.handle, claim.handle);
    for (const runId of launch.run_ids) {
      if (runId === dispatch.launch.run_id) continue;
      // A second POST created a real run; it was refused, cancelled and delivered nothing.
      duplicateRuns++;
      assert.equal(host.runs.get(runId).conclusion, "cancelled");
      assert.ok(![...host.receipts.values()].some(record => record.run_id === runId));
    }
  }
  return { accountLimitSkips, duplicateRuns };
}

/**
 * Cold-read the branch from Git, check its bytes are the canonical causal log and that a
 * second replay of the same commits agrees, then publish a production checkpoint and verify
 * the restored projection.
 */
async function coldAndCheckpoint(host, branch, policy) {
  const args = { githubClient: host.client, owner: OWNER, repo: REPO, branch };
  const coldStart = performance.now();
  const cold = await readWorkQueueLog(args);
  const coldReplayMs = performance.now() - coldStart;
  const digest = projectionDigest(cold.state);
  const stored = (await host.git(["show", `${cold.sha}:work-queue.jsonl`])).toString("utf8");
  assert.equal(serializeTransactionLog(cold.transactions), stored, "the ledger must hold canonical commits in causal order");
  assert.equal(projectionDigest(replayTransactions(cold.transactions)), digest, "replaying the same commits twice changed the projection");
  const checkpointStart = performance.now();
  const published = await publishWorkQueueCheckpoint({ ...args, actor: queueActor("administrator"), now: () => AT });
  assert.equal(published.checkpointed, true);
  assert.equal(projectionDigest(published.state), digest, "the checkpoint folded a different head");
  const restored = await readWorkQueueLog(args);
  const checkpointMs = performance.now() - checkpointStart;
  assert.equal(restored.transactions.length, 1);
  // The root line keeps the live set, what it reads and the request window; settled Work folds away.
  const bounded = assertBoundedCheckpoint(published.state, restored.transactions[0]);
  assert.equal(bounded.bytes, Buffer.byteLength(restored.contents));
  const parameters = dispatchParameters(policy, "default", 1);
  const selectionStart = performance.now();
  const selection = planDispatch(restored.state, parameters, newRequest("dispatch_next", queueActor("dispatcher", "1"), parameters).id);
  return {
    history: cold.state,
    state: restored.state,
    ledger_bytes: Buffer.byteLength(stored),
    checkpoint_bytes: bounded.bytes,
    checkpoint_requests: bounded.requests,
    checkpoint_works: bounded.works,
    cold_replay_ms: coldReplayMs,
    checkpoint_ms: checkpointMs,
    selection_ms: performance.now() - selectionStart,
    selection_reason: selection.reason,
    checkpoint: "production Checkpoint root line keeping the live set, what it reads and the request window; folded history stays in Git",
  };
}

/** The replay engine enforces the protocol's pending ceiling even if a publisher skipped the policy check. */
async function replayCeiling(host, branch, policy) {
  const { state } = await readWorkQueueLog({ githubClient: host.client, owner: OWNER, repo: REPO, branch });
  const producer = queueActor("producer");
  const works = normalizeSubmitParameters({ nodes: [{ graph_id: `${branch}-ceiling`, node_key: "over", payload: {} }] }, policy);
  try {
    appendCommit(state, buildCommit(state, producer, newRequest("submit", producer, { work_ids: works.map(work => work.work_id) }), works, AT));
  } catch (error) {
    return { code: error.code, message: error.message };
  }
  throw new Error("replay admitted Work beyond the protocol pending ceiling");
}

async function runSimulator(input = {}) {
  const options = optionsFor(input);
  if (options.mode === "history") {
    const decorate = report => {
      report.mode = "history";
      report.workers = options.workers;
      report.requested = options.items;
      report.requested_history_work = options.items / 2;
      report.requested_unit = "combined historical Work plus Claim entities; probes and newly completed worker Work counted separately";
      report.seeded_cancelled_work = report.cancelled;
      report.seeded_claims = report.claimed;
      report.new_worker_completions = report.lifecycle_smoke?.completed || 0;
      return report;
    };
    try {
      return decorate(await require("./work-queue-stress-retained.cjs").runRetainedSimulator({ ...options, mode: "retained", items: options.items / 2, historyItems: undefined, verifyHistoryWorkers: true }));
    } catch (error) {
      if (error.report) decorate(error.report);
      throw error;
    }
  }
  if (options.mode === "retained") return require("./work-queue-stress-retained.cjs").runRetainedSimulator(options);
  const started = performance.now();
  const directory = await fs.mkdtemp(path.join(process.cwd(), ".work-queue-stress-"));
  let host;
  let pool;
  let deadline;
  let maxRss = process.memoryUsage().rss;
  const sample = rss => (maxRss = Math.max(maxRss, rss || process.memoryUsage().rss));
  const sampler = setInterval(() => sample(), 100);
  const report = {
    ok: false,
    mode: options.mode,
    seed: options.seed,
    workers: options.workers,
    requested: options.items,
    submitted: 0,
    claimed: 0,
    completed: 0,
    verified_results: 0,
    closed_dispatches: 0,
    duplicate_runs: 0,
    account_limit_skips: 0,
    dispatch_reasons: {},
    planned_branch_count: options.mode === "lifecycle" ? Math.ceil(options.items / options.queueItems) : 1,
    branch_count: 0,
    queue_item_bound: options.mode === "lifecycle" ? options.queueItems : null,
    scope: options.mode === "lifecycle" ? "explicit-bounded-rotating-branch-shards; NOT single-queue 100000 support" : "single-queue admission saturation; NOT full-lifecycle throughput",
    retention: "completed branch fixtures are cold-replayed, checkpointed, fsck-verified, then retired/pruned; JSON measurements retained",
    limits: PROTOCOL_LIMITS,
    queues: [],
    saturation: null,
    fault_schedule:
      options.mode === "saturation"
        ? "not applicable to admission-only saturation"
        : options.faults && options.items > 1
          ? "seeded two-candidate real-Git CAS barrier and one lost successful ref response on first branch; one concurrent duplicate launch of the first Dispatch"
          : "disabled",
  };
  try {
    host = new LocalGitHub(path.join(directory, "queue.git"), { seed: options.seed, gitTimeoutMs: Math.min(30000, options.timeoutSeconds * 1000) });
    await host.initialize();
    pool = new Publishers(host, options.workers, sample);
    const policy = policyFor(options.workers);
    report.policy = policy;
    let dispatcherRun = 2000;
    const scenario = async () => {
      let start = 0;
      let verifiedReceipts = 0;
      for (let index = 0; index < report.planned_branch_count; index++) {
        const branch = `stress-${options.seed}-${index}`;
        const graph = `${branch}-graph`;
        const count = options.mode === "lifecycle" ? Math.min(options.queueItems, options.items - start) : options.items;
        report.branch_count++;
        if (options.mode === "saturation") {
          let accepted = 0;
          let attempted = 0;
          // Use actual production admission, not an oversized synthetic transaction or a raised limit.
          while (accepted < options.items) {
            const size = Math.min(SUBMIT_BATCH, options.items - accepted);
            attempted += size;
            try {
              await pool.runAll([{ kind: "submit", id: `${branch}:submit:${accepted}`, branch, graph: `${graph}-${accepted}`, start: accepted, count: size, policy }]);
              accepted += size;
            } catch (error) {
              if (!ADMISSION_REFUSALS.includes(error.code)) throw error;
              report.saturation = { code: error.code, message: error.message, accepted, refused_batch: size, attempted, not_attempted: options.items - attempted };
              break;
            }
          }
          if (report.saturation?.code === "work_queue_pending_limit") report.saturation.replay_ceiling = await replayCeiling(host, branch, policy);
          const cold = await coldAndCheckpoint(host, branch, policy);
          report.submitted = cold.state.works.size;
          assert.equal(report.submitted, accepted);
          report.queues.push({
            branch,
            requested: options.items,
            submitted: accepted,
            claimed: 0,
            completed: 0,
            ledger_bytes: cold.ledger_bytes,
            checkpoint_bytes: cold.checkpoint_bytes,
            checkpoint_requests: cold.checkpoint_requests,
            checkpoint_works: cold.checkpoint_works,
            cold_replay_ms: cold.cold_replay_ms,
            checkpoint_ms: cold.checkpoint_ms,
            selection_ms: cold.selection_ms,
            selection_reason: cold.selection_reason,
            transactions: cold.history.transactions.length,
            checkpoint: cold.checkpoint,
          });
          break;
        }
        const batch = Math.max(1, Math.min(64, Math.ceil(count / options.workers)));
        const submissions = [];
        for (let offset = 0; offset < count; offset += batch) submissions.push({ kind: "submit", id: `${branch}:submit:${offset}`, branch, graph, start: start + offset, count: Math.min(batch, count - offset), policy });
        const forced = options.faults && index === 0 && submissions.length >= 2;
        if (forced) {
          host.armFaults(branch);
          await pool.runAll(submissions.slice(0, 2));
          await pool.runAll(submissions.slice(2));
        } else await pool.runAll(submissions);
        const duplicates = await pool.runAll([submissions[0], submissions.at(-1)]);
        assert.ok(
          duplicates.every(result => result.recovered),
          "stable submission replay did not recover original request"
        );
        report.submitted += count;
        let completed = 0;
        let round = 0;
        let firstGrant;
        while (completed < count) {
          const grants = Array.from({ length: Math.min(options.workers, 4, Math.ceil((count - completed) / GRANT_CLAIMS)) }, (_, worker) => ({
            kind: "dispatch",
            id: `${branch}:grant:${round}:${worker}`,
            branch,
            run_id: String(dispatcherRun++),
            parameters: dispatchParameters(policy, "default", GRANT_CLAIMS),
          }));
          const decisions = await pool.runAll(grants);
          // A grant that found the pool full commits nothing; replay a committed one.
          firstGrant ||= grants.find((_, position) => decisions[position].assignments.length);
          for (const decision of decisions) if (decision.reason) report.dispatch_reasons[decision.reason] = (report.dispatch_reasons[decision.reason] || 0) + 1;
          const assignments = decisions.flatMap(decision => decision.assignments);
          assert.ok(assignments.length, "queue stalled with unfinished Work");
          assert.equal(new Set(assignments.map(assignment => assignment.dispatch_id)).size, assignments.length, "duplicate Dispatch reservation");
          assert.ok(
            assignments.every(assignment => assignment.claims.length === 1),
            "a granted Dispatch carries exactly one Claim"
          );
          const members = assignments.map(assignment => assignment.claims[0]);
          assert.equal(new Set(members.map(member => member.work_id)).size, members.length, "duplicate reservation owner");
          assert.equal(new Set(members.map(member => member.claim_id)).size, members.length);
          report.claimed += members.length;
          const jobs = assignments.map(assignment => ({ kind: "lifecycle", id: `${branch}:lifecycle:${assignment.dispatch_id}`, branch, assignment, policy }));
          // A concurrent second launcher of the same Dispatch: at most one launch is recorded.
          if (options.faults && index === 0 && round === 0) jobs.splice(1, 0, { ...jobs[0], id: `${jobs[0].id}:concurrent-replay` });
          const results = await pool.runAll(jobs);
          assert.equal(results.filter(result => result.completed).length, assignments.length, "every granted Claim completes exactly once");
          // Replay after the Dispatch closed must not launch or reopen it.
          const replayed = await pool.runAll([{ ...jobs[0], id: `${jobs[0].id}:terminal-replay` }]);
          assert.equal(replayed[0].skipped, true);
          completed += members.length;
          report.completed += members.length;
          round++;
        }
        const cold = await coldAndCheckpoint(host, branch, policy);
        const checked = assertLifecycle(cold.history, host, { graph, start, count });
        // The grant's request survives the checkpoint: its replay is still recovered, not re-planned.
        const grantReplay = await pool.runAll([firstGrant]);
        assert.equal(grantReplay[0].recovered, true, "stable grant request was not recovered");
        assert.equal(host.receipts.size, count);
        verifiedReceipts += host.receipts.size;
        report.verified_results += count;
        report.closed_dispatches += cold.history.dispatches.size;
        report.duplicate_runs += checked.duplicateRuns;
        report.account_limit_skips += checked.accountLimitSkips;
        assert.equal(cold.selection_reason, "no_work");
        report.queues.push({
          branch,
          requested: count,
          submitted: cold.history.works.size,
          claimed: cold.history.claims.size,
          completed: cold.history.stats.completed,
          ledger_bytes: cold.ledger_bytes,
          checkpoint_bytes: cold.checkpoint_bytes,
          checkpoint_requests: cold.checkpoint_requests,
          checkpoint_works: cold.checkpoint_works,
          cold_replay_ms: cold.cold_replay_ms,
          checkpoint_ms: cold.checkpoint_ms,
          selection_ms: cold.selection_ms,
          transactions: cold.history.transactions.length,
          dispatches: cold.history.dispatches.size,
          duplicate_runs: checked.duplicateRuns,
          account_limit_skips: checked.accountLimitSkips,
          checkpoint: cold.checkpoint,
        });
        start += count;
        // Retire only fully verified fixtures. Rewriting a growing ledger on
        // every publication otherwise retains gigabytes of loose Git blobs.
        await host.git(["fsck", "--strict", "--no-dangling"]);
        await host.git(["update-ref", "-d", `refs/heads/${branch}`]);
        await host.git(["prune", "--expire=now"]);
        host.receipts.clear();
        host.launches.clear();
        for (const runId of host.runs.keys()) if (runId !== "100") host.runs.delete(runId);
      }
      if (options.mode === "lifecycle") {
        assert.equal(report.submitted, options.items);
        assert.equal(report.claimed, options.items);
        assert.equal(report.completed, options.items);
        assert.equal(verifiedReceipts, options.items);
        assert.equal(report.closed_dispatches, options.items);
        assert.equal(host.metrics.native_posts, report.closed_dispatches + report.duplicate_runs);
        if (options.faults && options.items > 1) {
          assert.ok(host.metrics.forced_races > 0);
          assert.ok(host.metrics.cas_conflicts > 0, "no genuine non-fast-forward CAS was observed");
          assert.equal(host.metrics.lost_success_responses, 1);
          assert.ok(pool.counters().recovered_publications > 0);
        }
      }
      await host.git(["fsck", "--strict", "--no-dangling"]);
      report.ok = true;
    };
    await Promise.race([
      scenario(),
      new Promise((_, reject) => {
        deadline = setTimeout(() => reject(new Error(`simulator deadline exceeded (${options.timeoutSeconds}s)`)), options.timeoutSeconds * 1000);
      }),
    ]);
    return report;
  } catch (error) {
    // Preserve honest persisted counts even when one contender exhausts its
    // retry budget. Terminate publishers before inspecting the last Git tip.
    if (pool && host) {
      await pool.close();
      for (const fault of host.faults.values()) for (const arrival of fault.arrivals) arrival.reject(new Error("stress scenario stopped"));
      await Promise.allSettled([...pool.inFlight]);
      const index = report.branch_count - 1;
      if (index >= 0 && report.queues.length < report.branch_count) {
        const branch = `stress-${options.seed}-${index}`;
        try {
          const cold = await readWorkQueueLog({ githubClient: host.client, owner: OWNER, repo: REPO, branch });
          const state = cold.state;
          const works = [...state.works.values()];
          report.queues.push({
            branch,
            requested: options.mode === "lifecycle" ? Math.min(options.queueItems, options.items - index * options.queueItems) : options.items,
            submitted: state.works.size,
            claimed: state.claims.size,
            completed: state.stats.completed,
            verified_results: works.filter(work => work.state === "completed" && host.receipts.has(work.effects[0]?.receipt)).length,
            closed_dispatches: [...state.dispatches.values()].filter(dispatch => dispatch.state === "closed").length,
            ledger_bytes: Buffer.byteLength(cold.contents),
            partial: true,
          });
          for (const name of ["submitted", "claimed", "completed"]) report[name] = report.queues.reduce((sum, queue) => sum + queue[name], 0);
          report.verified_results = report.queues.reduce((sum, queue) => sum + (queue.verified_results ?? queue.completed), 0);
          report.closed_dispatches = report.queues.reduce((sum, queue) => sum + (queue.closed_dispatches ?? queue.dispatches ?? 0), 0);
        } catch (snapshotError) {
          report.partial_snapshot_error = snapshotError.message;
          report.counts_scope = "previously verified progress; final in-flight Git state could not be read";
        }
      }
    }
    error.report = report;
    throw error;
  } finally {
    clearTimeout(deadline);
    clearInterval(sampler);
    if (pool) {
      report.publisher_metrics = pool.counters();
      await pool.close();
    }
    if (host) {
      report.git_api_metrics = { ...host.metrics };
      await host.close();
    }
    sample();
    report.elapsed_ms = performance.now() - started;
    // resourceUsage is process-wide, so worker memory must not be added again.
    report.max_rss_bytes = Math.max(maxRss, process.resourceUsage().maxRSS * 1024);
    report.max_rss_scope = "entire Node process including worker threads; excludes Git subprocesses; process-lifetime high-water mark";
    report.ledger_bytes_total = report.queues.reduce((sum, queue) => sum + queue.ledger_bytes, 0);
    report.items_per_second = report.completed / (report.elapsed_ms / 1000);
    await fs.rm(directory, { recursive: true, force: true });
  }
}

function parseArgs(args) {
  const options = {};
  const names = {
    "--items": "items",
    "--history-items": "historyItems",
    "--workers": "workers",
    "--queue-items": "queueItems",
    "--history-queue-items": "historyQueueItems",
    "--seed": "seed",
    "--timeout-seconds": "timeoutSeconds",
    "--mode": "mode",
  };
  for (let index = 0; index < args.length; index++) {
    const argument = args[index];
    if (argument === "--no-faults") options.faults = false;
    else if (argument === "--help") return { help: true };
    else {
      if (!Object.hasOwn(names, argument) || index + 1 === args.length) throw new Error(`unknown or incomplete argument: ${argument}`);
      const value = args[++index];
      options[names[argument]] = argument === "--mode" ? value : /^\d+$/.test(value) ? Number(value) : NaN;
    }
  }
  return optionsFor(options);
}

const HELP = `Local real-Git work-queue (protocol v4) stress simulator (no GitHub credentials/network).

node .github/scripts/work-queue-stress.cjs --items 32 --workers 2 --seed 7
node .github/scripts/work-queue-stress.cjs --items 100000 --workers 4 --queue-items 128 --timeout-seconds 86400
node .github/scripts/work-queue-stress.cjs --mode saturation --items 100000 --workers 2
node .github/scripts/work-queue-stress.cjs --mode retained --items 100000 --history-queue-items 10000 --workers 4 --timeout-seconds 600
node .github/scripts/work-queue-stress.cjs --history-items 100000 --workers 4 --timeout-seconds 600
node .github/scripts/work-queue-stress.cjs --mode history --items 100000 --workers 2 --seed 7 --timeout-seconds 600

Lifecycle: independent rotating bounded branches (default 32 Work/branch, configurable
2..256). Total 100000 is NOT support for 100000 Work on one branch. Branches run
sequentially; workers publish concurrently within each branch through the production
store. Concurrent dispatcher runs grant Claims under the compiled pool concurrency and
per-account limit; each Claim is its own Dispatch. Every Dispatch gets a real POST, a
recorded launch (only that run may bind), a provenance-checked Bound, a simulated no-op
effect receipt and a production finish Completion. A concurrent duplicate launch on the
first branch creates a second run that is refused. Completed fixtures are cold-replayed,
checkpointed and fsck-verified before their refs are retired and unreachable objects pruned.
Saturation: one queue; reports the first real admission refusal (the compiled
pending_limit) and the unattempted remainder, then proves replay enforces the same
ceiling. No claims/completions are fabricated or protocol limits raised.
Retained: --items is historical Work requested; each accepted Work has one cancelled
Claim. Bounded fixture batches use production submit/scheduler/release planners and are
applied once through replay onto the real Git head. Whenever the log passes
CHECKPOINT_AFTER_LINES the epoch is persisted and checkpointed by production maintenance;
each checkpoint must keep exactly the live set, what it reads and the newest
RETAINED_REQUESTS request IDs. The tail epoch is cold-validated from real Git and
checkpointed. Independent retained branches default to 10000 historical Work each
(--history-queue-items). Eight live probe Work per history branch and a 16-item concurrent
full-store smoke are reported separately. Completed history fixtures are retired/pruned.
An actual ledger-bound refusal stops construction; no ceilings change.
--history-items counts historical Work PLUS Claims, selects retained mode, and
defaults to one history queue (100000 means 50000 Work plus 50000 Claims).
It does not include live probes or worker smoke. Explicit --history-queue-items
can still bound/shard that entity target. Per 128 historical Work the fixture commits
submit, one dispatch_next, and one prelaunch launch_failed release per Dispatch.
History: --mode history --items 100000 is the CI-facing combined-entity form
(50000 historical Work + 50000 Claims on one queue across checkpoint epochs by default).
It additionally checks the final Git ledger concurrently in 2..4 worker threads using
real cold reads, deterministic selection and checkpoint round trips. Use --workers 2 for
bounded CI memory. Newly completed worker Work is reported separately.

Options: --items 1..10000000, --workers 2..32, --queue-items 2..256,
--history-queue-items 1..10000000 (default 10000), --seed uint32,
--history-items even 2..20000000 (optional historical entity target),
--timeout-seconds 1..86400 (harness wall-clock deadline, default 3600),
--mode lifecycle|saturation|retained|history, --no-faults.
Faults force two real stale Git candidates, drop one successful update response and
launch the first Dispatch twice.
Git fixtures use mkdtemp under cwd and are always removed; no /tmp or remotes.
stdout: JSON; stderr: explicit scope/branch plan. Nonzero exit on invariant failure.
`;

if (require.main === module) {
  Promise.resolve()
    .then(async () => {
      const options = parseArgs(process.argv.slice(2));
      if (options.help) return process.stdout.write(HELP);
      const branches =
        options.mode === "lifecycle"
          ? Math.ceil(options.items / options.queueItems)
          : options.mode === "retained" || options.mode === "history"
            ? Math.ceil((options.mode === "history" ? options.items / 2 : options.items) / options.historyQueueItems) + 1
            : 1;
      process.stderr.write(
        `Local ${options.mode}: ${options.items} requested, ${options.workers} workers, ${branches} branch(es)${
          options.mode === "lifecycle"
            ? `, <=${options.queueItems} Work/branch, rotating verified fixtures (not single-queue scale)`
            : options.mode === "retained" || options.mode === "history"
              ? `, <=${options.historyQueueItems} historical Work/branch plus concurrent lifecycle smoke; actual budget refusal reported`
              : ", native single-queue saturation"
        }.\n`
      );
      process.stdout.write(`${JSON.stringify(await runSimulator(options), null, 2)}\n`);
    })
    .catch(error => {
      process.stdout.write(`${JSON.stringify({ ...(error.report || {}), ok: false, error: error.message }, null, 2)}\n`);
      process.exitCode = 1;
    });
}

module.exports = { LocalGitHub, Publishers, assertDispatchOrder, assertLifecycle, coldAndCheckpoint, optionsFor, parseArgs, policyFor, runSimulator };
