const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");

// Execute the actual trusted collection script, with no network or credentials.
const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/auditor.md"), "utf8");
const script = workflow
  .split("          script: |\n")[1]
  .split("      - name: Upload compact collection")[0]
  .split("\n")
  .map(line => line.replace(/^ {12}/, ""))
  .join("\n");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const collect = new AsyncFunction("require", "process", "context", "github", "core", "Date", script);
const now = new Date().toISOString();
const run = (id, overrides = {}) => ({
  id,
  run_attempt: 1,
  path: `.github/workflows/example-${id}.lock.yml`,
  status: "completed",
  conclusion: "failure",
  created_at: now,
  updated_at: now,
  html_url: `https://github.com/example/repo/actions/runs/${id}`,
  ...overrides,
});

async function fixture(t, options = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "auditor-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const cache = path.join(root, "cache");
  const data = path.join(root, "data");
  fs.mkdirSync(cache);
  if (options.state) {
    fs.writeFileSync(path.join(cache, "checkpoint.json"), JSON.stringify(options.state));
  }
  const runs = options.runs || [];
  const calls = [];
  const outputs = {};
  const clock = { time: options.time || now };
  class FixtureDate extends Date {
    constructor(...args) {
      super(...(args.length ? args : [clock.time]));
    }
    static now() {
      return Date.parse(clock.time);
    }
  }
  let used = 20;
  const github = {
    rest: {
      actions: {
        listWorkflowRunsForRepo: async () => assert.fail("Discovery must use health, not an all-run census"),
        getWorkflowRun: async params => {
          calls.push(["tracked", params.run_id]);
          const selected = runs.find(r => r.id === params.run_id);
          if (!selected) throw new Error("Tracked run unavailable");
          return { data: selected };
        },
      },
      pulls: {
        list: async params => {
          calls.push(["prs", params.page]);
          if (options.prError) throw new Error("PR access denied");
          const prs = options.prs || [];
          return {
            data: prs.slice((params.page - 1) * 100, params.page * 100),
            headers: {
              link: options.prOverflow || params.page * 100 < prs.length ? '<next>; rel="next"' : "",
            },
          };
        },
      },
      rateLimit: {
        get: async () => ({
          data: { resources: { core: { remaining: 1000, used, reset: 9999 } } },
        }),
      },
    },
  };
  const mockRequire = name => {
    if (name !== "node:child_process") return require(name);
    return {
      execFileSync: (command, args) => {
        assert.equal(command, "gh");
        calls.push(args);
        if (args[1] === "version") return "gh aw test-version";
        assert.ok(!args.includes("all"));
        if (args[1] === "health") {
          assert.equal(args[args.indexOf("--repo") + 1], "example/repo");
          const start = args[args.indexOf("--start-date") + 1];
          const end = args[args.indexOf("--end-date") + 1];
          assert.equal(Date.parse(start), Date.parse(clock.time) - 86400000);
          assert.equal(end, clock.time);
          const recent = runs.filter(r => Date.parse(r.created_at) >= Date.parse(start) && Date.parse(r.created_at) <= Date.parse(end) && r.path.endsWith(".lock.yml"));
          return JSON.stringify({
            collection: {
              partial: false,
              failure_listing_complete: true,
              failure_pass_complete: true,
              potential_issues: recent
                .filter(r => r.conclusion === "failure")
                .map(r => ({
                  workflow_path: r.path,
                  representative_run_id: r.id,
                  representative_status: options.representativeStatus || "revision_checked",
                })),
              ...options.healthCollection,
            },
            workflows: recent.map(r => ({
              workflow_path: r.path,
              intentional_failure: options.intentional,
              revisions: [{ current: { run_ids: options.representativeStatus === "superseded" ? [] : [r.id] } }],
            })),
          });
        }
        if (args[1] === "logs") {
          used += options.logCost || 0;
          const id = Number(args[args.indexOf("--after-run-id") + 1]) + 1;
          const selected = runs.find(item => item.id === id);
          const report = {
            runs: [
              {
                run_id: id,
                run_attempt: String(selected.run_attempt),
                updated_at: selected.updated_at,
                status: selected.status,
                conclusion: selected.conclusion,
                intentional_failure: options.intentional,
              },
            ],
            failure_clusters: [
              {
                id: "local-cluster",
                signal: { kind: "error", value: "E001", job: "agent", step: "execute" },
                signal_role: "failure_boundary",
                failure_kind: "driver_exit",
                evidence: "observed",
              },
            ],
            ...(options.partial ? { continuation: { before_run_id: id } } : {}),
            ...(options.changedAttempt ? { runs: [{ run_id: id, run_attempt: "2" }] } : {}),
          };
          if (args.includes("--audit")) {
            const output = args[args.indexOf("--output") + 1];
            fs.mkdirSync(output, { recursive: true });
            const auditFile = path.join(output, "audit.json");
            fs.writeFileSync(
              auditFile,
              JSON.stringify({
                overview: { run_id: id, conclusion: "failure" },
              })
            );
            report.runs[0].audit_path = auditFile;
          }
          return JSON.stringify(report);
        }
        assert.fail("Only health and budgeted logs queries may contact GitHub");
      },
    };
  };
  const core = {
    setOutput: (key, value) => {
      outputs[key] = value;
    },
    summary: { addRaw: () => ({ write: async () => {} }) },
  };
  const execute = () => collect(mockRequire, { env: { CACHE_DIR: cache, DATA_DIR: data } }, { repo: { owner: "example", repo: "repo" } }, github, core, FixtureDate);
  return { execute, calls, outputs, cache, data, runs, clock };
}

test("empty complete collection checkpoints but never audits or activates", async t => {
  const f = await fixture(t);
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.ok(fs.existsSync(path.join(f.cache, "checkpoint.json")));
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 0);
});

test("repeated observation is not new", async t => {
  const f = await fixture(t, { runs: [run(42)] });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.clusters[0].count, 1);
  assert.ok(fs.existsSync(path.join(f.data, manifest.clusters[0].audit_file)));
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.filter(call => call[1] === "logs").length, 2);
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 1);
});

test("old-run rerun attempt is independently collected", async t => {
  const old = run(42);
  const f = await fixture(t, {
    runs: [run(42, { run_attempt: 2, created_at: new Date(Date.parse(now) - 7200000).toISOString() })],
    state: {
      version: 1,
      repository: "example/repo",
      checkpoint: now,
      seen: [`42-1-${old.updated_at}`],
      backlog: [],
      pending: [42],
    },
  });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
});

test("query-local symptom clusters merge by unique run identity", async t => {
  const f = await fixture(t, { runs: [run(42), run(43), run(44)] });
  await f.execute();
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.new_failures, 3);
  assert.equal(manifest.clusters.length, 1);
  assert.equal(manifest.clusters[0].count, 3);
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 1);
});

test("intentional failures and auditor never activate the agent", async t => {
  const f = await fixture(t, {
    runs: [run(42), run(43, { path: ".github/workflows/auditor.lock.yml" })],
    intentional: true,
  });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 0);
  assert.equal(f.calls.filter(call => call[1] === "logs").length, 0);
});

test("PR pagination includes human and draft PRs on subsequent pages", async t => {
  const prs = Array.from({ length: 101 }, (_, n) => ({
    number: n + 1,
    title: `Human fix ${n}`,
    body: "",
    html_url: "https://github.com/pr",
    draft: true,
    head: { sha: "123" },
    base: { ref: "main" },
  }));
  const f = await fixture(t, { prs });
  await f.execute();
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.open_prs.length, 101);
  assert.equal(manifest.open_prs.at(-1).draft, true);
});

for (const [name, options] of [
  ["health API error", { healthCollection: { partial: true, reason: "api_error" } }],
  ["health time budget", { healthCollection: { partial: true, reason: "time_budget" } }],
  ["missing health completeness", { healthCollection: { partial: undefined } }],
  ["incomplete PR pagination", { prOverflow: true }],
  ["PR access denied", { prError: true }],
  ["partial logs query", { runs: [run(42)], partial: true }],
  ["changed run attempt", { runs: [run(42)], changedAttempt: true }],
]) {
  test(`${name} fails closed without saving a checkpoint`, async t => {
    const f = await fixture(t, options);
    await assert.rejects(f.execute());
    assert.equal(f.outputs.has_new_failures, "false");
    assert.equal(fs.existsSync(path.join(f.cache, "checkpoint.json")), false);
  });
}

test("large historical failure inventory is outside recent discovery", async t => {
  const history = Array.from({ length: 40000 }, (_, n) => run(n + 1, { created_at: "2020-01-01T00:00:00Z" }));
  const f = await fixture(t, { runs: [...history, run(40001)] });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
  assert.equal(f.calls.filter(call => call[1] === "health").length, 1);
  assert.equal(f.calls.filter(call => call[0] === "tracked").length, 1);
});

test("busy recent non-agentic runs do not overflow health discovery", async t => {
  const history = Array.from({ length: 1500 }, (_, n) => run(n + 1, { path: ".github/workflows/ci.yml" }));
  const f = await fixture(t, { runs: [...history, run(1501)] });
  await f.execute();
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(Date.parse(manifest.window_start), Date.parse(now) - 86400000);
  assert.equal(Date.parse(manifest.retention_start), Date.parse(now) - 86400000);
  assert.equal(manifest.new_failures, 1);
  assert.deepEqual(
    f.calls.filter(call => call[0] === "tracked"),
    [["tracked", 1501]]
  );
});

test("health cohorts retain processed backlog identities until expiry", async t => {
  const checkpoint = new Date(Date.parse(now) - 3600000).toISOString();
  const f = await fixture(t, {
    runs: [run(42)],
    state: { version: 1, repository: "example/repo", checkpoint, seen: [], backlog: [], pending: [] },
  });
  await f.execute();
  for (let hour = 1; hour <= 24; hour++) {
    f.clock.time = new Date(Date.parse(now) + hour * 3600000).toISOString();
    await f.execute();
    const state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
    assert.deepEqual(state.seen, [`42-1-${now}`]);
    assert.equal(state.backlog.length, 1);
    assert.equal(f.outputs.has_new_failures, "false");
  }
  assert.equal(f.calls.filter(call => call[1] === "logs").length, 2);
  assert.equal(f.calls.filter(call => call[0] === "tracked").length, 25);
  f.clock.time = new Date(Date.parse(now) + 25 * 3600000).toISOString();
  await f.execute();
  const state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.deepEqual(state.seen, []);
  assert.deepEqual(state.backlog, []);
  assert.equal(f.calls.filter(call => call[0] === "tracked").length, 25);
});

for (const [name, age, healthCollection] of [
  ["gap beyond 24 hours", 86400001, undefined],
  ["failed health discovery", 3600000, { partial: true, reason: "api_error" }],
]) {
  test(`${name} preserves an existing checkpoint and does not activate`, async t => {
    const state = {
      version: 1,
      repository: "example/repo",
      checkpoint: new Date(Date.parse(now) - age).toISOString(),
      seen: [],
      backlog: [],
      pending: [42],
    };
    const f = await fixture(t, { state, healthCollection });
    await assert.rejects(f.execute(), /checkpoint|scope/);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json"))), state);
    assert.equal(f.outputs.has_new_failures, "false");
    assert.equal(f.calls.filter(call => call[0] === "tracked").length, 0);
    if (age > 86400000) assert.equal(f.calls.length, 0);
  });
}

test("a checkpoint gap of exactly 24 hours remains covered, not silently clamped", async t => {
  const checkpoint = new Date(Date.parse(now) - 86400000).toISOString();
  const f = await fixture(t, {
    state: { version: 1, repository: "example/repo", checkpoint, seen: [], backlog: [], pending: [] },
  });
  await f.execute();
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(Date.parse(manifest.window_start), Date.parse(checkpoint));
});

test("unfinished run is discovered by health after failing", async t => {
  const f = await fixture(t, { runs: [run(42, { status: "in_progress", conclusion: null })] });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json"))).pending, []);
  Object.assign(f.runs[0], { status: "completed", conclusion: "failure" });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
  assert.ok(f.calls.some(call => call[0] === "tracked" && call[1] === 42));
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json"))).pending, []);
});

test("26 failures checkpoint the excess and proceed without newly created runs", async t => {
  const f = await fixture(t, { runs: Array.from({ length: 26 }, (_, n) => run(n + 1)) });
  await f.execute();
  let manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.new_failures, 25);
  assert.equal(manifest.evidence_complete, false);
  assert.equal(manifest.deferred_failures, 1);
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 0);
  let state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.equal(state.seen.length, 25);
  assert.deepEqual(state.pending, [26]);
  f.clock.time = new Date(Date.parse(now) + 3600000).toISOString();
  await f.execute();
  manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.new_failures, 1);
  assert.equal(manifest.evidence_complete, true);
  assert.equal(f.outputs.has_new_failures, "true");
  assert.equal(manifest.clusters[0].count, 26);
  assert.equal(f.calls.filter(call => call.includes("--audit")).length, 1);
  state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.equal(state.seen.length, 26);
  assert.deepEqual(state.pending, []);
});

test("over 100 deferred failures drain in bounded batches", async t => {
  const f = await fixture(t, { runs: Array.from({ length: 126 }, (_, n) => run(n + 1)) });
  for (let n = 0; n < 6; n++) {
    f.clock.time = new Date(Date.parse(now) + n * 3600000).toISOString();
    await f.execute();
    assert.equal(f.outputs.has_new_failures, n === 5 ? "true" : "false");
  }
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.clusters[0].count, 126);
  assert.equal(manifest.deferred_failures, 0);
  assert.equal(f.calls.filter(call => call[1] === "logs" && !call.includes("--audit")).length, 126);
});

test("shared API budget interruption checkpoints a resumable evidence batch", async t => {
  const f = await fixture(t, {
    runs: Array.from({ length: 6 }, (_, n) => run(n + 1)),
    logCost: 30,
  });
  await f.execute();
  let state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.equal(state.seen.length, 5);
  assert.deepEqual(state.pending, [6]);
  assert.equal(JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json"))).evidence_complete, false);
  assert.equal(f.outputs.has_new_failures, "false");
  await f.execute();
  state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.equal(state.seen.length, 6);
  assert.deepEqual(state.pending, []);
  assert.equal(JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json"))).clusters[0].count, 6);
});

test("tracked successful completion and superseded failure drop obsolete opportunities", async t => {
  const f = await fixture(t, { runs: [run(42), run(43, { status: "queued", conclusion: null })] });
  await f.execute();
  f.runs.forEach(r =>
    Object.assign(r, {
      created_at: "2020-01-01T00:00:00Z",
      run_attempt: 2,
      status: "completed",
      conclusion: "success",
    })
  );
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  const state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.deepEqual(state.pending, []);
  assert.deepEqual(state.backlog, []);
});

test("unobserved rerun outside the health horizon is explicitly not covered", async t => {
  const f = await fixture(t, {
    runs: [run(42, { created_at: "2020-01-01T00:00:00Z", run_attempt: 2 })],
  });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.filter(call => call[1] === "logs").length, 0);
});

for (const status of ["superseded", "jobless", "unverified", "revision_unknown"]) {
  test(`health ${status} representatives never activate or download logs`, async t => {
    const f = await fixture(t, { runs: [run(42)], representativeStatus: status });
    await f.execute();
    assert.equal(f.outputs.has_new_failures, "false");
    assert.equal(f.calls.filter(call => call[1] === "logs").length, 0);
  });
}

test("bounded health inventory keeps verified failures actionable and reports partial coverage", async t => {
  const f = await fixture(t, {
    runs: [run(42)],
    healthCollection: { partial: true, reason: "failure_listing_limit", failure_listing_complete: false, failure_pass_complete: false },
  });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.discovery.partial, true);
  assert.equal(manifest.discovery.reason, "failure_listing_limit");
});

test("health removes previously collected superseded failures from the backlog", async t => {
  const options = { runs: [run(42)] };
  const f = await fixture(t, options);
  await f.execute();
  options.representativeStatus = "superseded";
  await f.execute();
  const state = JSON.parse(fs.readFileSync(path.join(f.cache, "checkpoint.json")));
  assert.deepEqual(state.backlog, []);
  assert.equal(f.outputs.has_new_failures, "false");
});

test("native exclusion and no-dispatch policy are explicit", () => {
  assert.match(workflow, /excluded-files:\n\s+- "\*\*\/\*\.lock\.yml"/);
  assert.doesNotMatch(workflow, /ignore-files:|workflow_dispatch:/);
  assert.match(workflow, /if: github\.event_name == 'schedule' && needs\.collect\.outputs\.has_new_failures == 'true'/);
  assert.match(workflow, /github-token-for-extra-empty-commit: none/);
  assert.match(workflow, /name: Collect failure evidence\n\s+if: github\.event_name == 'schedule'/);
  assert.match(workflow, /tools:\n\s+bash: \["\*"\]\n\s+edit:/);
});
