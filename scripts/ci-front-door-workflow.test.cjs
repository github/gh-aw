"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const os = require("node:os");
const { execFileSync, spawnSync } = require("node:child_process");
const { test } = require("node:test");
const { frontDoor, SUITE_IDS, JOB_DEPENDENCIES } = require("./ci-front-door.cjs");
const catalog = require("../.github/ci/suite-catalog.json");

const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/cgo.yml"), "utf8");
const jobs = Object.fromEntries([...workflow.split("\njobs:\n")[1].matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm)].map(match => [match[1], match[2]]));
const infrastructure = ["checkout-cache", "front-door-decision", "summarize-timing"];
const decision = jobs["front-door-decision"];
const decisionSteps = decision.split(/^    - name: /m).slice(1);
const validator = decisionSteps.find(step => step.startsWith("Validate suite outputs\n"));
const validatorScript = validator.split("node <<'NODE'\n")[1].split("        NODE")[0];

function dependencies(job) {
  const inline = job.match(/^    needs: \[([^\]]+)\]/m);
  if (inline) return inline[1].split(",").map(id => id.trim());
  return [...(job.match(/^    needs:\n((?:    - .+\n)+)/m)?.[1] || "").matchAll(/^    - (.+)$/gm)].map(match => match[1]);
}

function condition(job) {
  return job.match(/^    if: (.+)$/m)?.[1];
}

function evaluate(expression, { event = "pull_request", ref = "refs/pull/1/merge", result = "success", suites = "[]", results = {}, cancelled = false } = {}) {
  const needs = Object.fromEntries(Object.keys(jobs).map(id => [id, { result: results[id] || "success" }]));
  needs["front-door-decision"] = { result, outputs: { suite_ids: suites } };
  return vm.runInNewContext(expression.replace(/\bneeds\.([\w-]+)/g, 'needs["$1"]'), {
    github: { event_name: event, ref },
    needs,
    always: () => true,
    cancelled: () => cancelled,
    fromJSON: JSON.parse,
    contains: (values, value) => values.includes(value),
  });
}

function validatedSuites(raw, outcome = "success") {
  let output;
  const diagnostics = [];
  vm.runInNewContext(validatorScript, {
    process: {
      env: { RAW_SUITE_IDS: raw, SELECTOR_OUTCOME: outcome, GITHUB_OUTPUT: "captured-output" },
      stderr: { write: message => diagnostics.push(message) },
    },
    require: name => {
      assert.equal(name, "node:fs");
      return {
        appendFileSync(file, value) {
          assert.equal(file, "captured-output");
          output = value;
        },
      };
    },
  });
  assert.match(output, /^suite_ids=.*\n$/);
  if (output === "suite_ids=\n") assert.match(diagnostics.join(""), /outputs unavailable or invalid; running all suites/);
  return output.slice("suite_ids=".length, -1);
}

test("every CGO check job is selectable; only checkout, selection and cleanup are infrastructure", () => {
  assert.deepEqual(
    Object.keys(jobs)
      .filter(id => !infrastructure.includes(id))
      .sort(),
    [...SUITE_IDS].sort()
  );
  assert.deepEqual(catalog.suites.map(suite => suite.id).sort(), [...SUITE_IDS].sort());
  for (const id of SUITE_IDS) {
    assert.ok(dependencies(jobs[id]).includes("front-door-decision"), id);
    assert.match(condition(jobs[id]), /^always\(\) && !cancelled\(\)/, id);
    assert.equal(evaluate(condition(jobs[id])), false, id);
  }
  assert.match(jobs["cross-platform-build"], /os: \[macos-latest, windows-latest\]/);
  assert.doesNotMatch(jobs["cross-platform-build"], /matrix:[\s\S]*outputs\.suite_ids/);
  assert.equal(condition(decision), "github.event_name == 'pull_request'");
  assert.equal(condition(jobs["summarize-timing"]), "always()");
});

test("inference credentials and executable policy stay isolated from PR code", () => {
  assert.equal((workflow.match(/copilot-requests: write/g) || []).length, 1);
  assert.equal((workflow.match(/CI_DECISION_TOKEN:/g) || []).length, 1);
  for (const [id, job] of Object.entries(jobs)) {
    if (id !== "front-door-decision") assert.doesNotMatch(job, /copilot-requests: write|CI_DECISION_TOKEN/, id);
  }
  assert.equal(decisionSteps.length, 3);
  const [trusted, selector] = decisionSteps;
  assert.match(trusted, /uses: actions\/checkout@[a-f0-9]{40}\b/);
  assert.match(trusted, /ref: \$\{\{ github\.event\.pull_request\.base\.sha \}\}/);
  assert.match(trusted, /path: trusted\n/);
  assert.match(trusted, /fetch-depth: 1\n/);
  assert.match(trusted, /persist-credentials: false/);
  assert.match(trusted, /sparse-checkout-cone-mode: false\n/);
  assert.deepEqual(trusted.split("sparse-checkout: |\n")[1].trim().split(/\s+/), [
    "/scripts/ci-front-door.cjs",
    "/scripts/ci-front-door-diff.cjs",
    "/actions/setup/js/decisions_client.cjs",
    "/.github/ci/suite-catalog.json",
    "/.github/ci/decision-prompt.txt",
  ]);
  assert.equal((decision.match(/uses: actions\/checkout@/g) || []).length, 1);
  assert.match(selector, /working-directory: trusted\n/);
  assert.match(selector, /CI_DIFF_TOKEN: \$\{\{ github\.token \}\}/);
  assert.match(selector, /CI_DIFF_BASE_SHA: \$\{\{ github\.event\.pull_request\.base\.sha \}\}/);
  assert.match(selector, /CI_DIFF_HEAD_SHA: \$\{\{ github\.sha \}\}/);
  assert.match(selector, /CI_DECISION_TOKEN: \$\{\{ github\.token \}\}/);
  assert.match(selector, /run: node scripts\/ci-front-door\.cjs\n/);
  assert.match(selector, /continue-on-error: true/);
  assert.match(selector, /if: hashFiles\('trusted\/scripts\/ci-front-door\.cjs'\) != ''/);
  assert.doesNotMatch(decision, /actions\/cache|npm |npx |--test|uses: \.\/|CI_DIFF_WORKSPACE|path: evidence/);
  assert.doesNotMatch(validator, /working-directory:|require\((?!'node:fs')/);
  assert.doesNotMatch(decision, /^    (?:env|defaults):/m);
  assert.deepEqual(dependencies(decision), []);
});

test("PR model decisions control every check job through the actual output validator and job expressions", async () => {
  for (const wanted of [[], ...SUITE_IDS.map(id => [id]), [...SUITE_IDS]]) {
    const selection = await frontDoor({
      catalog,
      diff: { files: ["pkg/console/style.go"], patch: "+change", truncated: false, unavailable: false },
      prompt: "",
      token: "test-token",
      decide: async () => ({ suite_ids: wanted, confidence: 1 }),
    });
    assert.equal(selection.fallback, false);
    const suites = validatedSuites(JSON.stringify(selection.suite_ids));
    assert.notEqual(suites, "");
    for (const id of SUITE_IDS) assert.equal(evaluate(condition(jobs[id]), { suites }), selection.suite_ids.includes(id), `${wanted}: ${id}`);
  }
});

test("invalid outputs and missing, failed or skipped decisions run every check job", () => {
  for (const raw of ["", "not-json", "null", "{}", '["unknown"]', JSON.stringify([...SUITE_IDS, "unknown"]), '["build","build"]', '["go-tests"]', ...Object.keys(JOB_DEPENDENCIES).map(id => JSON.stringify([id]))]) {
    assert.equal(validatedSuites(raw), "", raw);
  }
  assert.equal(validatedSuites("[]"), "[]");
  assert.equal(validatedSuites(JSON.stringify(SUITE_IDS)), JSON.stringify(SUITE_IDS));
  for (const id of SUITE_IDS) {
    for (const result of ["failure", "skipped", "cancelled", ""]) assert.equal(evaluate(condition(jobs[id]), { result }), true, `${id}: ${result}`);
    assert.equal(evaluate(condition(jobs[id]), { suites: "" }), true, id);
    assert.equal(evaluate(condition(jobs[id]), { suites: JSON.stringify(SUITE_IDS), cancelled: true }), false, id);
  }
  for (const outcome of ["failure", "skipped", "cancelled", ""]) assert.equal(validatedSuites("[]", outcome), "", outcome);
  assert.match(decision, /suite_ids: \$\{\{ steps\.validated\.outputs\.suite_ids \}\}/);
});

test("selected prerequisite jobs match the CGO graph and dependency failures still block consumers", () => {
  for (const id of SUITE_IDS) {
    const checkDependencies = dependencies(jobs[id]).filter(dependency => SUITE_IDS.includes(dependency));
    assert.deepEqual(checkDependencies, JOB_DEPENDENCIES[id] || [], id);
    for (const dependency of checkDependencies) {
      for (const result of ["failure", "skipped", "cancelled"]) {
        assert.equal(evaluate(condition(jobs[id]), { suites: JSON.stringify(SUITE_IDS), results: { [dependency]: result } }), false, `${id}: ${dependency}:${result}`);
      }
    }
    if (dependencies(jobs[id]).includes("checkout-cache")) {
      assert.equal(evaluate(condition(jobs[id]), { suites: JSON.stringify(SUITE_IDS), results: { "checkout-cache": "failure" } }), false, id);
    }
  }
});

test("non-PR main controls retain all checks and never require a successful decision job", () => {
  for (const event of ["push", "schedule", "workflow_dispatch"]) {
    for (const id of SUITE_IDS) {
      assert.equal(evaluate(condition(jobs[id]), { event, ref: "refs/heads/main", result: "skipped" }), true, `${event}: ${id}`);
    }
  }
  for (const id of ["fuzz", "security-scan"]) {
    assert.equal(evaluate(condition(jobs[id]), { event: "workflow_dispatch", ref: "refs/heads/feature", result: "skipped" }), false, id);
  }
  assert.match(workflow, /schedule:\n[\s\S]*cron:/);
});

test("benchmarks are isolated from PR checks, including selector fallback", () => {
  assert.ok(!jobs.bench);
  assert.ok(!SUITE_IDS.includes("bench"));
  assert.equal(validatedSuites('["bench"]'), "");
  const benchmarks = fs.readFileSync(path.join(__dirname, "../.github/workflows/benchmarks.yml"), "utf8");
  const triggers = benchmarks.split("\njobs:\n")[0];
  assert.doesNotMatch(triggers, /\bpull_request(?:_target)?:/);
  const expression = condition(benchmarks.split("\n  bench:\n")[1]);
  for (const event of ["pull_request", "pull_request_target", "push", "schedule", "workflow_dispatch"]) {
    for (const ref of ["refs/heads/main", "refs/heads/feature", "refs/pull/1/merge"]) {
      assert.equal(evaluate(expression, { event, ref }), ref === "refs/heads/main" && ["push", "schedule", "workflow_dispatch"].includes(event), `${event}: ${ref}`);
    }
  }
});

test("benchmark shards cover every benchmark package once and bound expensive replay iterations", t => {
  const benchmarks = fs.readFileSync(path.join(__dirname, "../.github/workflows/benchmarks.yml"), "utf8");
  const script = benchmarks
    .split("    - name: Run benchmarks\n")[1]
    .split("      run: |\n")[1]
    .split("\n    - name:")[0]
    .replace(/^        /gm, "");
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "benchmark-shards-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const root = path.join(__dirname, "..");
  fs.writeFileSync(path.join(directory, "make"), '#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\nprintf "BenchmarkSmoke-1 1 1 ns/op\\n" > bench_results.txt\n', { mode: 0o755 });
  fs.writeFileSync(path.join(directory, "git"), '#!/bin/bash\nexec "$REAL_GIT" -C "$BENCH_ROOT" "$@"\n', { mode: 0o755 });
  const realGit = execFileSync("which", ["git"], { encoding: "utf8" }).trim();
  const files = execFileSync(realGit, ["grep", "-l", "^func Benchmark", "--", "pkg/*_test.go"], { cwd: root, encoding: "utf8" }).trim().split("\n");
  const expected = [...new Set(files.map(file => `./${path.dirname(file)}`))].sort();
  const selected = [];
  for (const shard of ["cli", "workflow", "workqueue", "other"]) {
    const capture = path.join(directory, `${shard}.args`);
    execFileSync("bash", ["-c", script], {
      cwd: directory,
      env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, REAL_GIT: realGit, BENCH_ROOT: root, SHARD: shard, CAPTURE_ARGS: capture, GITHUB_STEP_SUMMARY: path.join(directory, "summary") },
    });
    const args = fs.readFileSync(capture, "utf8").trim().split("\n");
    selected.push(
      ...args
        .find(arg => arg.startsWith("BENCH_PACKAGES="))
        .slice("BENCH_PACKAGES=".length)
        .split(" ")
    );
    assert.ok(args.includes(`BENCH_TIME=${shard === "workqueue" ? "1x" : "3x"}`));
  }
  assert.deepEqual(selected.sort(), expected);
});

test("make bench preserves Go failures through tee", t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "benchmark-failure-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.writeFileSync(path.join(directory, "go"), '#!/bin/bash\nprintf "FAIL benchmark\\n"\nexit 17\n', { mode: 0o755 });
  const result = spawnSync("make", ["-f", path.join(__dirname, "../Makefile"), "bench", "VERSION=test"], {
    cwd: directory,
    env: { ...process.env, PATH: `${directory}:${process.env.PATH}` },
    encoding: "utf8",
  });
  assert.notEqual(result.status, 0);
  assert.match(fs.readFileSync(path.join(directory, "bench_results.txt"), "utf8"), /FAIL benchmark/);
});

test("the entire needs graph is acyclic and timing/cache cleanup waits for all checks", () => {
  const visited = new Set();
  function visit(id, active = new Set()) {
    assert.ok(jobs[id], `unknown dependency: ${id}`);
    assert.ok(!active.has(id), `dependency cycle at ${id}`);
    if (visited.has(id)) return;
    for (const dependency of dependencies(jobs[id])) visit(dependency, new Set([...active, id]));
    visited.add(id);
  }
  for (const id of Object.keys(jobs)) visit(id);
  assert.deepEqual(dependencies(jobs["checkout-cache"]), []);
  assert.deepEqual(
    dependencies(jobs["summarize-timing"]).sort(),
    Object.keys(jobs)
      .filter(id => id !== "summarize-timing")
      .sort()
  );
  assert.match(jobs["front-door-conformance"], /node --test scripts\/ci-front-door-workflow\.test\.cjs/);
});
