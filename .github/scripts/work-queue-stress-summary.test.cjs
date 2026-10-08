"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");
const { writeSummary } = require("./work-queue-stress-summary.cjs");

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(process.cwd(), ".work-queue-stress-summary-test-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const common = {
    ok: true,
    seed: 7,
    workers: 2,
    branch_count: 1,
    elapsed_ms: 2000,
    max_rss_bytes: 128 * 1024 * 1024,
    ledger_bytes_total: 64 * 1024 * 1024,
    git_api_metrics: { cas_conflicts: 3, git_commands: 250, lost_success_responses: 1 },
  };
  const reports = {
    history: {
      ...common,
      mode: "history",
      requested: 100000,
      retained_history_entities: 100000,
      seeded_cancelled_work: 50000,
      seeded_claims: 50000,
      cold_git_read_replay_ms: 1200,
      ten_batch_plans_ms: 25,
      canonicalization_ms: 300,
      history_worker_verifications: [{ worker: 0, cold_replay_ms: 1400, ten_batch_plans_ms: 30, canonicalization_ms: 400, elapsed_ms: 1900 }],
      lifecycle_smoke: { completed: 16, elapsed_ms: 1000, git_api_metrics: { cas_conflicts: 2, lost_success_responses: 1 } },
    },
    lifecycle: {
      ...common,
      mode: "lifecycle",
      elapsed_ms: 4000,
      requested: 1024,
      completed: 1024,
      verified_results: 1024,
      publisher_metrics: { recovered_publications: 5 },
      queues: [{ ledger_bytes: 1024, cold_replay_ms: 10, selection_ms: 2 }],
    },
    saturation: { ...common, mode: "saturation", requested: 100000, submitted: 256, completed: 0, saturation: { code: "ledger_limit", message: "Recovery headroom exhausted", attempted: 512, refused_batch: 256, not_attempted: 99488 } },
  };
  const save = (name, report) => fs.writeFileSync(path.join(directory, `${name}.json`), JSON.stringify(report));
  for (const [name, report] of Object.entries(reports)) save(name, report);
  return { directory, reports, save };
}

test("summary reports correctly labelled performance metrics with collapsed diagnostics", t => {
  const { directory } = fixture(t);
  const { summary, failed } = writeSummary({ directory, env: { INVARIANTS_OUTCOME: "success" } });
  assert.equal(failed, false);
  assert.match(summary, /3 passed, 0 failed or cancelled, 0 unavailable/);
  assert.match(summary, /Simulator invariants: \*\*Passed\*\*/);
  assert.match(summary, /100,000 retained entities \(50,000 Work \+ 50,000 Claims\).*2\.00 s.*50,000\.00 entities\/s.*128\.00 MiB/);
  assert.match(summary, /1,024 completed \/ 1,024 requested Work.*4\.00 s.*256\.00 Work\/s/);
  assert.match(summary, /Admission saturation \| Passed \(admission limit reached\).*256 admitted \/ 100,000 requested Work.*Not a completion benchmark/);
  assert.match(summary, /not worker completion throughput/);
  assert.match(summary, /Attempted: 512 Work; refused batch: 256 Work; not attempted: 99,488 Work/);
  assert.match(summary, /excludes Git subprocesses/);
  assert.ok(summary.indexOf("<details>") > summary.indexOf("| Admission saturation"));
  assert.ok(summary.indexOf("| CAS conflicts | 3 | 3 | 3 |") > summary.indexOf("<details>"));
  assert.match(summary, /\| Batch planning \| 25\.0 ms \/ 10 plans/);
  assert.match(summary, /\| 0 \| 1\.40 s \| 30\.0 ms \| 400\.0 ms \| 1\.90 s \|/);
  assert.match(summary, /Separate live smoke: 16 completed Work/);
  assert.match(summary, /excluded from the history fixture's Git counters/);
  assert.ok(
    summary
      .split("\n")
      .filter(line => line.startsWith("#"))
      .every(line => line.startsWith("### "))
  );
});

test("summary appends to GITHUB_STEP_SUMMARY and saves the same artifact", t => {
  const { directory } = fixture(t);
  const summaryPath = path.join(directory, "github-summary.md");
  fs.writeFileSync(summaryPath, "Earlier step summary\n");
  const { summary } = writeSummary({ directory, env: { GITHUB_STEP_SUMMARY: summaryPath } });
  assert.equal(fs.readFileSync(summaryPath, "utf8"), `Earlier step summary\n${summary}`);
  assert.equal(fs.readFileSync(path.join(directory, "summary.md"), "utf8"), summary);
});

test("failed partial results retain counts but do not claim validated throughput", t => {
  const { directory, reports, save } = fixture(t);
  save("lifecycle", { ...reports.lifecycle, ok: false, completed: 12, error: "Worker deadline exceeded", counts_scope: "previously verified progress only" });
  const { summary, failed } = writeSummary({ directory, env: { HISTORY_OUTCOME: "skipped", LIFECYCLE_OUTCOME: "failure", SATURATION_OUTCOME: "skipped" } });
  assert.equal(failed, true);
  assert.match(summary, /0 passed, 1 failed or cancelled, 2 unavailable/);
  assert.match(summary, /Worker lifecycles \| Failed \| 12 completed \/ 1,024 requested Work \| 4\.00 s \| Not measured/);
  assert.match(summary, /Retained history \| Not run \| Not measured/);
  assert.match(summary, /Diagnostic:\*\* Worker deadline exceeded/);
  assert.match(summary, /Partial counts:\*\* previously verified progress only/);
});

test("missing, malformed and cancelled reports remain visible without hiding failure", t => {
  const { directory } = fixture(t);
  fs.writeFileSync(path.join(directory, "history.json"), "");
  fs.unlinkSync(path.join(directory, "lifecycle.json"));
  const { summary, failed } = writeSummary({ directory, env: { HISTORY_OUTCOME: "failure", LIFECYCLE_OUTCOME: "cancelled", SATURATION_OUTCOME: "skipped" } });
  assert.equal(failed, true);
  assert.match(summary, /Retained history \| Failed/);
  assert.match(summary, /Worker lifecycles \| Cancelled/);
  assert.match(summary, /Cannot read history\.json/);
  assert.match(summary, /Cannot read lifecycle\.json/);
  assert.doesNotMatch(summary, /NaN|Infinity|undefined/);
  assert.ok(fs.existsSync(path.join(directory, "summary.md")));
});

for (const invalid of [null, {}, { ok: "yes" }, { ok: true, mode: "lifecycle" }]) {
  test(`invalid history report is an explicit reporting failure: ${JSON.stringify(invalid)}`, t => {
    const { directory, save } = fixture(t);
    save("history", invalid);
    const result = writeSummary({ directory, env: { HISTORY_OUTCOME: "success" } });
    assert.equal(result.failed, true);
    assert.match(result.summary, /Retained history \| Failed/);
    assert.match(result.summary, /Cannot read history\.json/);
  });
}

test("a successful step with no metrics fails reporting rather than appearing skipped", t => {
  const { directory } = fixture(t);
  fs.unlinkSync(path.join(directory, "history.json"));
  const { summary, failed } = writeSummary({ directory, env: { HISTORY_OUTCOME: "success" } });
  assert.equal(failed, true);
  assert.match(summary, /Retained history \| Failed/);
});

test("local reports distinguish missing metrics from explicitly skipped profiles", t => {
  const { directory } = fixture(t);
  fs.unlinkSync(path.join(directory, "history.json"));
  const missing = writeSummary({ directory, env: {} });
  assert.equal(missing.failed, true);
  assert.match(missing.summary, /Retained history \| No report/);
  const skipped = writeSummary({ directory, env: { HISTORY_OUTCOME: "skipped" } });
  assert.equal(skipped.failed, false);
  assert.match(skipped.summary, /Retained history \| Not run/);
});

test("actual step failure overrides an otherwise successful JSON report", t => {
  const { directory } = fixture(t);
  const result = writeSummary({ directory, env: { HISTORY_OUTCOME: "failure", INVARIANTS_OUTCOME: "failure" } });
  assert.equal(result.failed, true);
  assert.match(result.summary, /Retained history \| Failed.*2\.00 s \| Not measured/);
  assert.match(result.summary, /Simulator invariants: \*\*Failed\*\*/);
});

test("zero elapsed time and missing metrics never produce invalid or invented numbers", t => {
  const { directory, reports, save } = fixture(t);
  save("history", { ...reports.history, elapsed_ms: 0, max_rss_bytes: 0 });
  save("lifecycle", { ok: false, mode: "lifecycle", error: "Setup failed before measurements" });
  const result = writeSummary({ directory, env: {} });
  assert.match(result.summary, /Retained history.*0\.0 ms \| Not measured \| 0 B/);
  assert.doesNotMatch(result.summary, /NaN|Infinity|undefined/);
});

test("diagnostic text cannot break summary HTML or markdown tables", t => {
  const { directory, save } = fixture(t);
  save("history", { ok: false, error: "</details><script>alert(1)</script>\n| `failure`" });
  const { summary } = writeSummary({ directory, env: {} });
  assert.match(summary, /&lt;\/details&gt;&lt;script&gt;/);
  assert.match(summary, /<br>&#124; \\`failure\\`/);
  assert.equal(summary.match(/<\/details>/g).length, 1);
});

test("step-summary filesystem errors propagate", t => {
  const { directory } = fixture(t);
  assert.throws(() => writeSummary({ directory, env: { GITHUB_STEP_SUMMARY: path.join(directory, "missing", "summary.md") } }), { code: "ENOENT" });
});

test("CLI publishes failure diagnostics before returning a failing exit status", t => {
  const { directory, save } = fixture(t);
  save("history", { ok: false, error: "Replay failed" });
  const summaryPath = path.join(directory, "github-summary.md");
  const result = spawnSync(process.execPath, [path.join(__dirname, "work-queue-stress-summary.cjs"), directory], { encoding: "utf8", env: { ...process.env, GITHUB_STEP_SUMMARY: summaryPath } });
  assert.equal(result.status, 1, result.stderr);
  assert.match(fs.readFileSync(summaryPath, "utf8"), /Diagnostic:\*\* Replay failed/);
  assert.equal(result.stdout, fs.readFileSync(path.join(directory, "summary.md"), "utf8"));
});

test("real simulator JSON supplies history, worker throughput and admission metrics", { timeout: 120000 }, async t => {
  const { directory, save } = fixture(t);
  const { runSimulator } = require("./work-queue-stress.cjs");
  const history = await runSimulator({ mode: "history", items: 64, workers: 2, seed: 7, timeoutSeconds: 90 });
  save("history", history);
  save("lifecycle", history.lifecycle_smoke);
  save("saturation", await runSimulator({ mode: "saturation", items: 100000, workers: 2, timeoutSeconds: 20 }));
  const { summary, failed } = writeSummary({ directory, env: {} });
  assert.equal(failed, false);
  assert.match(summary, /64 retained entities \(32 Work \+ 32 Claims\)/);
  assert.match(summary, /16 completed \/ 16 requested Work.*[\d,.]+ Work\/s/);
  assert.match(summary, /\| Batch planning \| [\d,.]+ (ms|s) \/ 10 plans/);
  assert.match(summary, /256 admitted \/ 100,000 requested Work/);
  assert.doesNotMatch(summary, /NaN|Infinity|undefined/);
});
