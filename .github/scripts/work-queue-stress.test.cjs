"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { LocalGitHub, parseArgs, runSimulator } = require("./work-queue-stress.cjs");

test("real-Git adapter refuses stale non-fast-forward candidates and foreign repositories", { timeout: 30000 }, async () => {
  const directory = await fs.mkdtemp(path.join(process.cwd(), ".work-queue-stress-adapter-"));
  const host = await new LocalGitHub(path.join(directory, "queue.git")).initialize();
  try {
    const common = { owner: "local", repo: "queue" };
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
    host.runs.get("100").run_attempt = 2;
    host.runs.get("100").actor.id = "9999";
    const current = await host.client.rest.actions.getWorkflowRun({ ...common, run_id: "100" });
    const original = await host.client.rest.actions.getWorkflowRunAttempt({ ...common, run_id: "100", attempt_number: 1 });
    assert.equal(current.data.run_attempt, 2);
    assert.equal(original.data.run_attempt, 1);
    assert.equal(original.data.actor.id, "1001", "original authenticated evidence must not inherit rerun principal");
    await host.git(["fsck", "--strict", "--no-dangling"]);
  } finally {
    await host.close();
    await fs.rm(directory, { recursive: true, force: true });
  }
});

test("two publishers complete real-Git lifecycle with CAS races, lost response, and immutable cold replay", { timeout: 120000 }, async () => {
  const report = await runSimulator({ items: 32, workers: 2, seed: 7, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.requested, 32);
  assert.equal(report.submitted, 32);
  assert.equal(report.claimed, 32);
  assert.equal(report.completed, 32);
  assert.equal(report.verified_results, 32);
  assert.equal(report.branch_count, 1);
  assert.ok(report.git_api_metrics.cas_conflicts >= 1);
  assert.equal(report.git_api_metrics.forced_races, 1);
  assert.equal(report.git_api_metrics.lost_success_responses, 1);
  assert.equal(report.git_api_metrics.native_posts, report.released_assignments);
  assert.ok(report.publisher_metrics.recovered_publications >= 3);
  assert.ok(report.publisher_metrics.retry_sleeps >= 1);
  assert.ok(report.publisher_metrics.binding_checks >= 80);
  assert.ok(report.max_rss_bytes > 0);
  assert.ok(report.queues[0].cold_replay_ms > 0);
  assert.ok(report.queues[0].ledger_bytes > 0);
  assert.equal(report.limits.ledger_bytes, 64 * 1024 * 1024);
  assert.equal(report.limits.recovery_bytes, 16 * 1024 * 1024);
  assert.equal(report.limits.pending_nodes, 4096);
  assert.equal(report.limits.graph_nodes, 4096);
  assert.equal(report.limits.operations, 256);
  assert.equal(report.limits.profile_max_claims, 16);
});

test("single-queue saturation reports actual admission and never pretends 100000 completed", { timeout: 30000 }, async () => {
  const report = await runSimulator({ mode: "saturation", items: 100000, workers: 2, timeoutSeconds: 20 });
  assert.equal(report.ok, true);
  assert.equal(report.branch_count, 1);
  assert.equal(report.submitted, 256);
  assert.equal(report.claimed, 0);
  assert.equal(report.completed, 0);
  assert.equal(report.saturation.code, "ledger_limit");
  assert.equal(report.saturation.attempted, 512);
  assert.equal(report.saturation.not_attempted, 99488);
  assert.equal(report.git_api_metrics.native_posts, 0);
  assert.equal(report.git_api_metrics.forced_races, 0);
  assert.equal(report.queues[0].selection_reason, "selected");
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
  assert.equal(report.git_api_metrics.native_posts, 2);
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
  assert.equal(report.retained_history_entities, 256);
  assert.equal(report.requested_history_entities, 256);
  assert.equal(report.fixture_batch, 128);
  assert.equal(report.transactions, 6, "genesis, probe submission, and four checked commits per 128 historical Work");
  assert.equal(report.pending_work, 8);
  assert.equal(report.recovery_bytes_used, 0);
  assert.equal(report.queues.length, 2);
  assert.equal(report.queues[1].retained_entities, 264);
  assert.equal(report.selected_probe_claims, 8);
  assert.equal(report.historical_reservations, 8);
  assert.equal(report.fixture_generation_replays, 0);
  assert.equal(report.fixture_native_posts, 0);
  assert.equal(report.lifecycle_smoke.completed, 16);
  assert.equal(report.lifecycle_smoke.git_api_metrics.lost_success_responses, 1);
  assert.ok(report.lifecycle_smoke.git_api_metrics.cas_conflicts >= 1);
  assert.ok(report.cold_git_read_replay_ms > 0);
  assert.ok(report.ten_batch_plans_ms > 0);
  assert.ok(report.canonicalization_ms > 0);
  assert.match(report.projection_sha256, /^[a-f0-9]{64}$/);
  assert.equal(report.saturation, null);
  assert.equal(report.limits.ledger_bytes, 64 * 1024 * 1024);
  assert.equal(report.limits.recovery_bytes, 16 * 1024 * 1024);
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

test("history mode counts combined entities and cold-checks the large fixture in multiple worker threads", { timeout: 120000 }, async () => {
  const report = await runSimulator({ mode: "history", items: 256, workers: 2, seed: 29, timeoutSeconds: 90 });
  assert.equal(report.ok, true);
  assert.equal(report.mode, "history");
  assert.equal(report.requested, 256);
  assert.equal(report.requested_history_work, 128);
  assert.equal(report.retained_history_entities, 256);
  assert.equal(report.seeded_cancelled_work, 128);
  assert.equal(report.seeded_claims, 128);
  assert.equal(report.completed, 0);
  assert.equal(report.new_worker_completions, 16);
  assert.equal(report.history_validation_workers, 2);
  assert.equal(report.history_worker_verifications.length, 2);
  for (const verification of report.history_worker_verifications) {
    assert.equal(verification.work, 136);
    assert.equal(verification.claims, 128);
    assert.equal(verification.completed, 0);
    assert.equal(verification.projection_sha256, report.projection_sha256);
    assert.equal(verification.ledger_bytes, report.ledger_bytes);
  }
});

test("deadline terminates workers, reports partial Git counts, and cleans its local fixture", { timeout: 15000 }, async () => {
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
