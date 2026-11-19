"use strict";

const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const root = path.join(__dirname, "..");
const workflow = fs.readFileSync(path.join(root, ".github/workflows/benchmarks.yml"), "utf8");
const shards = [...workflow.matchAll(/- shard: (.+)\n +packages: (.+)\n +pattern: (.+)/g)].map(([, id, packages, pattern]) => ({
  id,
  packages,
  pattern: pattern.replace(/^'|'$/g, ""),
}));

test("benchmarks cannot run on PRs or non-main manual runs", () => {
  const cgo = fs.readFileSync(path.join(root, ".github/workflows/cgo.yml"), "utf8");
  assert.doesNotMatch(cgo, /^  bench:|^    - bench$/m);
  assert.doesNotMatch(workflow, /^  pull_request(?:_target)?:/m);
  assert.match(workflow, /push:\n +branches: \[main\]/);
  assert.match(workflow, /schedule:\n +-[^\n]*cron:/);
  assert.match(workflow, /^  workflow_dispatch:/m);
  const condition = workflow.match(/^    if: (.+)$/m)[1];
  for (const event of ["push", "schedule", "workflow_dispatch", "pull_request"]) {
    for (const ref of ["refs/heads/main", "refs/heads/feature", "refs/pull/1/merge"]) {
      assert.equal(vm.runInNewContext(condition, { github: { event_name: event, ref } }), event !== "pull_request" && ref === "refs/heads/main", `${event}: ${ref}`);
    }
  }
});

test("parallel shards partition CLI, other packages and every queue replay size without reducing sampling", () => {
  assert.equal(new Set(shards.map(shard => shard.id)).size, shards.length);
  assert.match(workflow, /fail-fast: false/);
  assert.doesNotMatch(workflow, /max-parallel:/);
  assert.deepEqual(
    shards.filter(shard => shard.pattern === "."),
    [
      { id: "packages", packages: "./pkg/...", pattern: "." },
      { id: "cli", packages: "./pkg/cli", pattern: "." },
    ]
  );
  assert.match(workflow, /go list \.\/pkg\/\.\.\. \| grep -vE '\/\(cli\|workqueue\)\$'/);
  const replay = fs.readFileSync(path.join(root, "pkg/workqueue/replay_benchmark_test.go"), "utf8");
  const sizes = replay
    .match(/range \[\]int\{([^}]+)\}/)[1]
    .split(",")
    .map(size => Number(size.trim()));
  const queueShards = shards.filter(shard => shard.packages === "./pkg/workqueue");
  assert.equal(queueShards.length, sizes.length);
  for (const size of sizes) {
    const matches = queueShards.filter(shard => {
      const [parent, child] = shard.pattern.split("/").map(pattern => new RegExp(pattern));
      return parent.test("BenchmarkQueueColdReplay") && child.test(`${size}MiB`);
    });
    assert.equal(matches.length, 1, `${size}MiB must run exactly once`);
  }
  assert.match(workflow, /BENCH_PACKAGES: \$\{\{ matrix\.packages \}\}/);
  assert.match(workflow, /BENCH_PATTERN: \$\{\{ matrix\.pattern \}\}/);
  assert.match(workflow, /run: make bench/);
});

test("baselines come from successful main pushes of the benchmark workflow and artifacts are shard-specific", () => {
  assert.match(workflow, /actions\/workflows\/benchmarks\.yml\/runs\?branch=main&event=push&status=success&per_page=1/);
  assert.match(workflow, /\.expired == false/);
  assert.match(workflow, /--name "benchmark-results-\$SHARD"/);
  assert.match(workflow, /name: benchmark-results-\$\{\{ matrix\.shard \}\}/);
  assert.match(workflow, /if: always\(\) && steps\.benchmarks\.outcome == 'success'/);
  assert.match(workflow, /if-no-files-found: error/);
});

test("make bench preserves selectors and three iterations, and propagates failures through tee", t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "benchmark-target-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.copyFileSync(path.join(root, "Makefile"), path.join(directory, "Makefile"));
  fs.writeFileSync(path.join(directory, "go"), '#!/bin/sh\nprintf "%s\\n" "$@"\nexit "$BENCH_TEST_EXIT"\n', { mode: 0o755 });
  const args = ["bench", "BENCH_PACKAGES=./pkg/workqueue", "BENCH_PATTERN=^BenchmarkQueueColdReplay$/^16MiB$"];
  const env = { ...process.env, PATH: `${directory}:${process.env.PATH}`, BENCH_TEST_EXIT: "0" };
  const success = spawnSync("make", args, { cwd: directory, env, encoding: "utf8" });
  assert.equal(success.status, 0, success.stderr);
  const output = fs.readFileSync(path.join(directory, "bench_results.txt"), "utf8").trim().split("\n");
  assert.deepEqual(output, ["test", "-bench=^BenchmarkQueueColdReplay$/^16MiB$", "-benchmem", "-benchtime=3x", "-run=^$", "./pkg/workqueue"]);
  const inherited = spawnSync("make", ["bench"], {
    cwd: directory,
    env: { ...env, BENCH_PACKAGES: "./pkg/workqueue", BENCH_PATTERN: "^BenchmarkQueueColdReplay$/^16MiB$" },
    encoding: "utf8",
  });
  assert.equal(inherited.status, 0, inherited.stderr);
  assert.deepEqual(fs.readFileSync(path.join(directory, "bench_results.txt"), "utf8").trim().split("\n"), output);
  const failure = spawnSync("make", args, { cwd: directory, env: { ...env, BENCH_TEST_EXIT: "1" }, encoding: "utf8" });
  assert.notEqual(failure.status, 0, "tee must not mask a failed benchmark");
});
