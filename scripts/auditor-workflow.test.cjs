const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/auditor.md"), "utf8");
const source = fs.readFileSync(path.join(__dirname, "auditor-collection.cjs"), "utf8");
const now = new Date("2026-11-08T08:00:00Z");
const target = id => ({
  run_id: id,
  repository: "example/repo",
  workflow_path: ".github/workflows/example.lock.yml",
  attempt: "1",
  updated_at: now.toISOString(),
  cluster_ids: [`cluster-${id}`],
});

function fixture(t, options = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "auditor-health-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const cache = path.join(root, "cache");
  const data = path.join(root, "data");
  fs.mkdirSync(cache);
  const stateFile = path.join(cache, "health-checkpoint.json");
  if (options.state) fs.writeFileSync(stateFile, JSON.stringify(options.state));
  const targets = options.targets || [];
  const health = {
    collection: { partial: false, failure_listing_complete: true, failure_pass_complete: true },
    failure_investigation: { selected_runs: targets },
    failure_clusters: targets.map(item => ({ id: item.cluster_ids[0], count: 1 })),
    ...options.health,
  };
  const calls = [];
  const outputs = {};
  let attemptChecks = 0;
  let rateChecks = 0;
  const module = { exports: {} };
  const mockRequire = name =>
    name === "node:child_process"
      ? {
          execFileSync(command, args, opts) {
            assert.equal(command, "gh");
            calls.push({ args, input: opts.input });
            if (args[1] === "health") return JSON.stringify(health);
            assert.equal(args[1], "audit");
            assert.ok(args.includes("--stdin"));
            assert.ok(args.includes("--no-baseline"));
            const id = Number(opts.input.trim());
            return JSON.stringify({
              overview: { run_id: id, conclusion: "failure", updated_at: now.toISOString() },
              ...options.audit,
            });
          },
        }
      : require(name);
  vm.runInNewContext(source, { require: mockRequire, module, process: { env: {} } });
  const github = {
    rest: {
      actions: {
        getWorkflowRun: async params => ({
          data: {
            id: params.run_id,
            run_attempt: Number(targets.find(item => item.run_id === params.run_id).attempt),
            status: "completed",
            conclusion: "failure",
            updated_at: now.toISOString(),
            ...options.refreshed,
            ...(options.changedDuringAudit && ++attemptChecks > 1 ? { run_attempt: 2 } : {}),
          },
        }),
      },
      rateLimit: {
        get: async () => ({
          data: {
            resources: {
              core: {
                remaining: options.remaining ?? 1000,
                reset: options.rateReset && ++rateChecks > 1 ? 10000 : 9999,
              },
            },
          },
        }),
      },
      pulls: {
        list: async params => {
          if (options.prError) throw new Error("PR denied");
          const prs = options.prs || [];
          return { data: prs.slice((params.page - 1) * 100, params.page * 100), headers: { link: options.prOverflow || params.page * 100 < prs.length ? '<next>; rel="next"' : "" } };
        },
      },
    },
  };
  const execute = () =>
    module.exports({
      github,
      context: { repo: { owner: "example", repo: "repo" } },
      core: {
        setOutput: (key, value) => {
          outputs[key] = value;
        },
        summary: { addRaw: () => ({ write: async () => {} }) },
      },
      env: { CACHE_DIR: cache, DATA_DIR: data },
      now,
    });
  return { execute, calls, outputs, stateFile, data };
}

test("complete empty health checkpoints without audits or activation", async t => {
  const f = fixture(t);
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.length, 1);
  assert.ok(fs.existsSync(f.stateFile));
  assert.ok(f.calls[0].args.includes("--start-date"));
  assert.ok(f.calls[0].args.includes("--logs-dir"));
  assert.ok(!f.calls[0].args.includes("--repo"), "local checkout supplies repository identity and intentional-failure tags");
});

test("health selections feed singleton stdin audits and repeat observations are not new", async t => {
  const f = fixture(t, { targets: [target(42), target(43)] });
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "true");
  assert.deepEqual(
    f.calls.slice(1).map(call => call.input),
    ["42\n", "43\n"]
  );
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.discovery, "bounded_current_revision_sample");
  assert.equal(manifest.clusters[0].audit_file, "audit-42.json");
  await f.execute();
  assert.equal(f.outputs.has_new_failures, "false");
  assert.equal(f.calls.filter(call => call.args[1] === "audit").length, 2);
});

test("three-audit budget retains unprocessed selections for the next hourly scan", async t => {
  const f = fixture(t, { targets: [1, 2, 3, 4, 5].map(target) });
  await f.execute();
  let manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.new_failures, 3);
  assert.equal(manifest.remaining_targets, 2);
  await f.execute();
  manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.new_failures, 2);
  assert.equal(manifest.remaining_targets, 0);
  assert.equal(f.calls.filter(call => call.args[1] === "audit").length, 5);
});

test("new attempt is audited independently and auditor never selects itself", async t => {
  const old = target(42);
  const f = fixture(t, {
    targets: [
      { ...old, attempt: "2" },
      { ...target(43), workflow_path: ".github/workflows/auditor.lock.yml" },
    ],
    state: { version: 2, repository: "example/repo", seen: [{ key: `42-1-${old.updated_at}`, updated_at: old.updated_at }] },
  });
  await f.execute();
  assert.equal(f.calls.filter(call => call.args[1] === "audit").length, 1);
});

test("PR pagination retains all-author drafts", async t => {
  const prs = Array.from({ length: 101 }, (_, n) => ({
    number: n + 1,
    title: "Human fix",
    body: "",
    html_url: "https://github.com/pr",
    head: { sha: "123" },
    base: { ref: "main" },
    draft: true,
  }));
  const f = fixture(t, { prs });
  await f.execute();
  const manifest = JSON.parse(fs.readFileSync(path.join(f.data, "manifest.json")));
  assert.equal(manifest.open_prs.length, 101);
  assert.equal(manifest.open_prs.at(-1).draft, true);
});

for (const [name, options] of [
  ["partial health", { health: { collection: { partial: true } } }],
  ["missing health completeness", { health: { collection: {} } }],
  ["unfinished failure inventory", { health: { collection: { partial: false, failure_listing_complete: false } } }],
  ["partial audit", { targets: [target(42)], audit: { partial: true, complete: false } }],
  ["changed audit", { targets: [target(42)], audit: { overview: { run_id: 43 } } }],
  ["changed run attempt", { targets: [target(42)], refreshed: { run_attempt: 2 } }],
  ["attempt changed during audit", { targets: [target(42)], changedDuringAudit: true }],
  ["missing cluster", { targets: [target(42)], health: { failure_clusters: [] } }],
  ["missing investigation", { health: { failure_clusters: [{ id: "cluster" }], failure_investigation: null } }],
  ["rate window reset", { targets: [target(42)], rateReset: true }],
  ["PR denial", { prError: true }],
  ["PR overflow", { prOverflow: true }],
  ["API reserve", { remaining: 349 }],
  ["duplicate targets", { targets: [target(42), target(42)] }],
]) {
  test(`${name} preserves checkpoint and suppresses activation`, async t => {
    const state = { version: 2, repository: "example/repo", seen: [] };
    const f = fixture(t, { state, ...options });
    const before = fs.readFileSync(f.stateFile, "utf8");
    await assert.rejects(f.execute());
    assert.equal(fs.readFileSync(f.stateFile, "utf8"), before);
    assert.equal(f.outputs.has_new_failures, "false");
  });
}

test("workflow wires trusted module, native exclusion and scheduled-only activation", () => {
  assert.match(workflow, /require\('\.\/scripts\/auditor-collection\.cjs'\)/);
  assert.match(workflow, /excluded-files:\n\s+- "\*\*\/\*\.lock\.yml"/);
  assert.match(workflow, /- "scripts\/auditor-collection\.cjs"/);
  assert.doesNotMatch(workflow, /workflow_dispatch:/);
  assert.match(workflow, /github-token-for-extra-empty-commit: none/);
  assert.match(workflow, /auditor-health-v2-/);
});
