#!/usr/bin/env node
"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { performance } = require("node:perf_hooks");
const { Worker } = require("node:worker_threads");
const { LocalGitHub, REPOSITORY, PRINCIPAL } = require("./work-queue-stress-git.cjs");
const { canonical, utf8Compare } = require("../../actions/setup/js/work_queue_codec.cjs");
const { defaultPolicy } = require("../../actions/setup/js/work_queue_policy.cjs");
const { nodeId } = require("../../actions/setup/js/work_queue_graph.cjs");
const { DEFAULT_LIMITS } = require("../../actions/setup/js/work_queue_limits.cjs");
const { compactTransactions, replayTransactions, serializeProjection, serializeTransactionLog } = require("../../actions/setup/js/work_queue_replay.cjs");
const { readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { assignmentForDispatch, fifoCompare, planNext } = require("../../actions/setup/js/work_queue_scheduler.cjs");

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

function policyFor(host, workers) {
  const policy = defaultPolicy({ repository: REPOSITORY, principal: PRINCIPAL, ref: host.ref });
  policy.accounting_weights = { "": 1, alpha: 1, beta: 1 };
  policy.producers[PRINCIPAL].fairness_keys = ["alpha", "beta"];
  policy.pools.default.profiles.default.max_claims = 16;
  policy.pools.default.profiles.default.share_keys = true;
  policy.pools.default.logical_limit = Math.min(64, 16 * workers);
  policy.pools.default.native_limit = Math.min(4, workers);
  // Ordinary/recovery, graph, pending and operation ceilings remain untouched.
  assert.deepEqual(policy.limits, DEFAULT_LIMITS);
  return policy;
}

function shuffled(values, seed) {
  const output = [...values];
  let state = seed >>> 0;
  for (let index = output.length - 1; index > 0; index--) {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    const other = state % (index + 1);
    [output[index], output[other]] = [output[other], output[index]];
  }
  return output;
}

function assertLifecycle(state, host, { graph, start, count }) {
  assert.equal(state.works.size, count, "lost or duplicated Work");
  assert.equal(state.claims.size, count, "lost or duplicated Claim");
  assert.equal(state.stats.completed, count);
  assert.equal(state.stats.available, 0);
  assert.equal(state.stats.claimed, 0);
  const expected = new Set(Array.from({ length: count }, (_, index) => nodeId(graph, `item-${start + index}`)));
  const queues = new Map();
  for (const work of [...state.works.values()].sort(fifoCompare)) {
    assert.ok(expected.delete(work.work_id), "unexpected or duplicated Work identity");
    assert.equal(work.state, "completed");
    assert.equal(work.barrier, "verified");
    assert.equal(work.attempts, 1);
    assert.equal(work.result.local_noop, work.work_id);
    const queue = queues.get(work.fairness_key) || [];
    queue.push(work.work_id);
    queues.set(work.fairness_key, queue);
  }
  assert.equal(expected.size, 0);
  const picked = new Map([...queues.keys()].map(key => [key, 0]));
  const owners = new Set();
  for (const commit of state.transactions) {
    for (const operation of commit.operations) {
      if (operation.kind !== "Claim") continue;
      assert.ok(!owners.has(operation.work_id), "Work has two reservation owners");
      owners.add(operation.work_id);
      const work = state.works.get(operation.work_id);
      const eligible = [...queues].filter(([key, queue]) => picked.get(key) < queue.length).map(([key]) => key);
      // An independent equal-weight stride oracle, not planNext itself. All
      // these Work are ready before grants; FIFO is ledger position, not item ID.
      eligible.sort((a, b) => picked.get(a) - picked.get(b) || utf8Compare(a, b));
      assert.equal(work.fairness_key, eligible[0], "Claim departed from fair prefix");
      assert.equal(operation.work_id, queues.get(work.fairness_key)[picked.get(work.fairness_key)], "Claim departed from per-account FIFO");
      picked.set(work.fairness_key, picked.get(work.fairness_key) + 1);
    }
  }
  for (const dispatch of state.dispatches.values()) {
    const launch = host.launches.get(dispatch.dispatch_id);
    assert.equal(launch?.count, 1, "assignment did not launch exactly once");
    assert.equal(dispatch.released, true);
    assert.equal(dispatch.run.run_attempt, 1);
    assert.equal(dispatch.run.run_id, launch.run_id);
    assert.equal(dispatch.run.ref, host.ref);
    assert.equal(canonical(assignmentForDispatch(state, dispatch.dispatch_id)), canonical(launch.assignment), "persisted immutable membership differs from launched inputs");
    const run = host.runs.get(dispatch.run.run_id);
    assert.equal(run.status, "completed");
    for (const member of dispatch.claims) {
      assert.equal(state.claims.get(member.claim_id).state, "completed");
      const result = state.terminalBarriers.get(member.work_id);
      const receipt = host.receipts.get(result.evidence.receipt);
      assert.equal(receipt?.completion_id, state.works.get(member.work_id).completion_id);
      assert.equal(receipt?.run_id, run.id);
      assert.equal(receipt?.work_id, member.work_id);
      assert.equal(receipt?.handle, member.handle);
    }
  }
}

async function coldAndCompact(host, branch, seed) {
  const args = { githubClient: host.client, owner: "local", repo: "queue", branch };
  const coldStart = performance.now();
  const cold = await readWorkQueueLog(args);
  const coldReplayMs = performance.now() - coldStart;
  const before = canonical(serializeProjection(cold.state));
  const source = shuffled([...cold.transactions, cold.transactions[0], cold.transactions.at(-1)], seed);
  assert.throws(() => compactTransactions([...cold.transactions, { ...cold.transactions.at(-1), at: cold.transactions.at(-1).at + 1 }]), /conflicting duplicate commit ID/);
  const compacted = compactTransactions(source);
  assert.equal(compacted.length, cold.transactions.length, "canonicalization lost committed facts");
  const canonicalized = replayTransactions(compacted);
  assert.equal(canonical(serializeProjection(canonicalized)), before, "compaction changed queue semantics");
  const content = serializeTransactionLog(compacted);
  const actual = await host.git(["show", `${cold.sha}:work-queue.jsonl`]);
  assert.equal(actual.length, cold.state.ledgerBytes);
  assert.equal(Buffer.byteLength(content), actual.length, "canonicalization must not pretend to prune retained history");
  const blob = await host.client.rest.git.createBlob({ owner: "local", repo: "queue", content, encoding: "utf-8" });
  const tree = await host.client.rest.git.createTree({ owner: "local", repo: "queue", base_tree: cold.treeSha, tree: [{ path: "work-queue.jsonl", mode: "100644", type: "blob", sha: blob.data.sha }] });
  const commit = await host.client.rest.git.createCommit({ owner: "local", repo: "queue", message: "Local stress canonicalization", tree: tree.data.sha, parents: [cold.sha] });
  await host.client.rest.git.updateRef({ owner: "local", repo: "queue", ref: `heads/${branch}`, sha: commit.data.sha, force: false });
  const reread = await readWorkQueueLog(args);
  assert.equal(canonical(serializeProjection(reread.state)), before, "persisted cold compaction replay changed semantics");
  const selectionStart = performance.now();
  const selection = planNext(reread.state, "default", 1800000000000);
  return {
    state: reread.state,
    ledger_bytes: actual.length,
    cold_replay_ms: coldReplayMs,
    selection_ms: performance.now() - selectionStart,
    selection_reason: selection.reason,
    canonicalization: "causal-sort-and-exact-dedup-only; no history pruning",
  };
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
    released_assignments: 0,
    planned_branch_count: options.mode === "lifecycle" ? Math.ceil(options.items / options.queueItems) : 1,
    branch_count: 0,
    queue_item_bound: options.mode === "lifecycle" ? options.queueItems : null,
    scope: options.mode === "lifecycle" ? "explicit-bounded-rotating-branch-shards; NOT single-queue 100000 support" : "single-queue admission saturation; NOT full-lifecycle throughput",
    retention: "completed branch fixtures are cold-replayed, fsck-verified, then retired/pruned; JSON measurements retained",
    limits: { ...DEFAULT_LIMITS, profile_max_claims: 16 },
    queues: [],
    saturation: null,
    fault_schedule:
      options.mode === "saturation" ? "not applicable to admission-only saturation" : options.faults && options.items > 1 ? "seeded two-candidate real-Git CAS barrier and one lost successful ref response on first branch" : "disabled",
  };
  try {
    host = new LocalGitHub(path.join(directory, "queue.git"), { seed: options.seed, gitTimeoutMs: Math.min(30000, options.timeoutSeconds * 1000) });
    await host.initialize();
    pool = new Publishers(host, options.workers, sample);
    const policy = policyFor(host, options.workers);
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
          // Use actual production publication, not an oversized synthetic
          // transaction or a raised pending/recovery limit.
          while (accepted < options.items) {
            // Leave room for Policy in genesis while preserving the same
            // 256-Work admission boundary before testing full-sized batches.
            const size = Math.min(accepted < 256 ? 128 : 256, options.items - accepted);
            attempted += size;
            try {
              await pool.runAll([{ kind: "submit", id: `${branch}:submit:${accepted}`, branch, graph, start: accepted, count: size, policy }]);
              accepted += size;
            } catch (error) {
              if (!["ledger_limit", "resource_limit"].includes(error.code)) throw error;
              report.saturation = { code: error.code, message: error.message, accepted, refused_batch: size, attempted, not_attempted: options.items - attempted };
              break;
            }
          }
          const cold = await coldAndCompact(host, branch, options.seed);
          report.submitted = cold.state.works.size;
          assert.equal(report.submitted, accepted);
          report.queues.push({
            branch,
            requested: options.items,
            submitted: accepted,
            claimed: 0,
            completed: 0,
            ledger_bytes: cold.ledger_bytes,
            cold_replay_ms: cold.cold_replay_ms,
            selection_ms: cold.selection_ms,
            selection_reason: cold.selection_reason,
            transactions: cold.state.transactions.length,
            canonicalization: cold.canonicalization,
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
          const grants = Array.from({ length: Math.min(options.workers, 4, Math.ceil((count - completed) / 16)) }, (_, worker) => ({ kind: "dispatch", id: `${branch}:grant:${round}:${worker}`, branch }));
          firstGrant ||= grants[0];
          const decisions = await pool.runAll(grants);
          const assignments = decisions.flatMap(decision => decision.assignments);
          assert.ok(assignments.length, "queue stalled with unfinished Work");
          assert.equal(new Set(assignments.map(assignment => assignment.dispatch_id)).size, assignments.length, "duplicate assignment reservation");
          const members = assignments.flatMap(assignment => assignment.claims);
          assert.equal(new Set(members.map(member => member.work_id)).size, members.length, "duplicate reservation owner");
          assert.equal(new Set(members.map(member => member.claim_id)).size, members.length);
          report.claimed += members.length;
          const jobs = assignments.map(assignment => ({ kind: "lifecycle", id: `${branch}:lifecycle:${assignment.dispatch_id}`, branch, assignment }));
          if (index === 0 && round === 0) jobs.splice(1, 0, { ...jobs[0], id: `${jobs[0].id}:concurrent-replay` });
          await pool.runAll(jobs);
          // Replay after complete native release must not launch or reopen it.
          const replayed = await pool.runAll([{ ...jobs[0], id: `${jobs[0].id}:terminal-replay` }]);
          assert.equal(replayed[0].skipped, true);
          completed += members.length;
          report.completed += members.length;
          report.verified_results += members.length;
          report.released_assignments += assignments.length;
          round++;
        }
        const grantReplay = await pool.runAll([firstGrant]);
        assert.equal(grantReplay[0].recovered, true, "stable grant request was not recovered");
        const cold = await coldAndCompact(host, branch, options.seed + index);
        assertLifecycle(cold.state, host, { graph, start, count });
        assert.equal(host.receipts.size, count);
        verifiedReceipts += host.receipts.size;
        assert.equal(cold.selection_reason, "no_work");
        report.queues.push({
          branch,
          requested: count,
          submitted: cold.state.works.size,
          claimed: cold.state.claims.size,
          completed: cold.state.stats.completed,
          ledger_bytes: cold.ledger_bytes,
          cold_replay_ms: cold.cold_replay_ms,
          selection_ms: cold.selection_ms,
          transactions: cold.state.transactions.length,
          assignments: cold.state.dispatches.size,
          canonicalization: cold.canonicalization,
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
        for (const runId of host.originalRuns.keys()) if (runId !== "100") host.originalRuns.delete(runId);
      }
      if (options.mode === "lifecycle") {
        assert.equal(report.submitted, options.items);
        assert.equal(report.claimed, options.items);
        assert.equal(report.completed, options.items);
        assert.equal(verifiedReceipts, options.items);
        assert.equal(host.metrics.native_posts, report.released_assignments);
        if (options.faults && options.items > 1) {
          assert.ok(host.metrics.forced_races > 0);
          assert.ok(host.metrics.cas_conflicts > 0, "no genuine non-fast-forward CAS was observed");
          assert.equal(host.metrics.lost_success_responses, 1);
          assert.ok(pool.counters().recovered_publications > 0);
          assert.ok(pool.counters().retry_sleeps > 0);
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
          const cold = await readWorkQueueLog({ githubClient: host.client, owner: "local", repo: "queue", branch });
          const state = cold.state;
          report.queues.push({
            branch,
            requested: options.mode === "lifecycle" ? Math.min(options.queueItems, options.items - index * options.queueItems) : options.items,
            submitted: state.works.size,
            claimed: state.claims.size,
            completed: state.stats.completed,
            verified_results: [...state.works.values()].filter(work => work.barrier === "verified").length,
            released_assignments: [...state.dispatches.values()].filter(dispatch => dispatch.released).length,
            ledger_bytes: state.ledgerBytes,
            partial: true,
          });
          for (const name of ["submitted", "claimed", "completed"]) report[name] = report.queues.reduce((sum, queue) => sum + queue[name], 0);
          report.verified_results = report.queues.reduce((sum, queue) => sum + (queue.verified_results ?? queue.completed), 0);
          report.released_assignments = report.queues.reduce((sum, queue) => sum + (queue.released_assignments ?? queue.assignments ?? 0), 0);
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

const HELP = `Local real-Git work-queue stress simulator (no GitHub credentials/network).

node .github/scripts/work-queue-stress.cjs --items 32 --workers 2 --seed 7
node .github/scripts/work-queue-stress.cjs --items 100000 --workers 4 --queue-items 128 --timeout-seconds 86400
node .github/scripts/work-queue-stress.cjs --mode saturation --items 100000 --workers 2
node .github/scripts/work-queue-stress.cjs --mode retained --items 100000 --history-queue-items 10000 --workers 4 --timeout-seconds 600
node .github/scripts/work-queue-stress.cjs --mode retained --items 100000 --history-queue-items 100000 --workers 4 --timeout-seconds 600
node .github/scripts/work-queue-stress.cjs --history-items 100000 --workers 4 --timeout-seconds 600
node .github/scripts/work-queue-stress.cjs --mode history --items 100000 --workers 2 --seed 7 --timeout-seconds 300

Lifecycle: independent rotating bounded branches (default 32 Work/branch, configurable
2..256). Total 100000 is NOT support for 100000 Work on one branch. Branches run
sequentially; workers publish concurrently within each branch. Every Work has a
real Claim, simulated native POST/run, Completion, no-op receipt Result, Release.
Completed fixtures are cold-replayed/fsck-verified before their refs are retired
and unreachable objects pruned. JSON retains each branch's measured workload.
Saturation: one queue; reports the first real admission refusal and unattempted
remainder. No claims/completions are fabricated or protocol limits raised.
Retained: --items is historical Work requested; each accepted Work retains one
cancelled Claim. Bounded fixture batches use production request/graph/scheduler
logic, then the complete ledger is cold-validated from real Git. Independent
retained branches default to 10000 historical Work each (--history-queue-items).
Eight live probe Work per history branch and a 16-item concurrent full-store
smoke are reported separately. Completed history fixtures are retired/pruned.
Actual ordinary/recovery budget refusal stops construction; no ceilings change.
--history-items counts historical Work PLUS Claims, selects retained mode, and
defaults to one history ledger (100000 means 50000 Work plus 50000 Claims).
It does not include live probes or worker smoke. Explicit --history-queue-items
can still bound/shard that entity target. Fixtures use four commits/128 nodes:
submit, fair Claim prefix, WorkCancellation, and positive prelaunch Release.
History: --mode history --items 100000 is the CI-facing combined-entity form
(50000 historical Work + 50000 Claims in one ledger by default). It additionally
checks that large Git ledger concurrently in 2..4 worker threads using real cold
reads, deterministic selection and exact canonicalization. Use --workers 2 for
bounded CI memory. Newly completed worker Work is reported separately.

Options: --items 1..10000000, --workers 2..32, --queue-items 2..256,
--history-queue-items 1..10000000 (default 10000), --seed uint32,
--history-items even 2..20000000 (optional historical entity target),
--timeout-seconds 1..86400 (default 3600), --mode lifecycle|saturation|retained|history, --no-faults.
Faults force two real stale Git candidates and drop one successful update response.
Git fixtures use mkdtemp under cwd and are always removed; no /tmp or remotes.
stdout: JSON; stderr: explicit scope/branch plan. Nonzero exit on invariant failure.
Compaction means safe causal canonicalization/exact dedup, not pruning history.
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

module.exports = { LocalGitHub, Publishers, assertLifecycle, coldAndCompact, optionsFor, parseArgs, runSimulator };
