"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../.github/workflows/daily-security-observability.md"), "utf8");
const selector = source.match(/jq -s --argjson current "\$CURRENT_RUN_ID" '([\s\S]*?)'\s+\/tmp\/gh-aw\/agent\/firewall/);
assert.ok(selector, "Missing bounded candidate selector");

function run(run_id, overrides = {}) {
  return { run_id, status: "completed", created_at: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"), ...overrides };
}

function select(firewall, integrity, current = 999) {
  const result = spawnSync("jq", ["-s", "--argjson", "current", String(current), selector[1]], {
    input: [firewall, integrity].map(runs => JSON.stringify({ runs })).join("\n"),
    encoding: "utf8",
    timeout: 5000,
  });
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(result.stdout);
}

test("security audits cap the completed union, not each source, and count omissions", () => {
  const result = select(
    Array.from({ length: 25 }, (_, i) => run(i + 1)),
    Array.from({ length: 25 }, (_, i) => run(i + 11))
  );
  assert.deepEqual(
    result.runs.map(run => run.run_id),
    Array.from({ length: 20 }, (_, i) => 35 - i)
  );
  assert.equal(result.eligible_runs, 35);
  assert.equal(result.selected_runs, 20);
  assert.equal(result.omitted_runs, 15);
  assert.equal(result.max_audits, 20);
  assert.equal(result.partial, true);
});

test("security audits exclude current, unfinished, stale, and invalid candidates", () => {
  const result = select(
    [
      run(999),
      run(8, { status: "in_progress" }),
      run(7, { status: "queued" }),
      run(6, { status: undefined }),
      run(5, { created_at: "2020-01-01T00:00:00Z" }),
      run(4, { created_at: "invalid" }),
      run(3, { created_at: undefined }),
      run("2"),
      run(0),
      run(1),
    ],
    [run(1)]
  );
  assert.deepEqual(
    result.runs.map(run => run.run_id),
    [1]
  );
  assert.equal(result.eligible_runs, 1);
  assert.equal(result.omitted_runs, 0);
  assert.equal(result.partial, false);
});

test("empty security datasets remain explicit empty coverage", () => {
  assert.deepEqual(select([], []), {
    runs: [],
    eligible_runs: 0,
    selected_runs: 0,
    omitted_runs: 0,
    partial: false,
    max_audits: 20,
  });
});

test("exactly twenty unique candidates do not report truncated coverage", () => {
  const runs = Array.from({ length: 20 }, (_, i) => run(i + 1));
  const result = select(runs, runs);
  assert.equal(result.selected_runs, 20);
  assert.equal(result.eligible_runs, 20);
  assert.equal(result.omitted_runs, 0);
  assert.equal(result.partial, false);
});

test("audit instructions narrow artifacts and require shared bounded results", () => {
  const examples = [...source.matchAll(/"run_id": 12345678,\s*"artifacts": (\[[^\]]+\])/g)];
  assert.equal(examples.length, 2);
  for (const example of examples) assert.deepEqual(JSON.parse(example[1]), ["firewall", "mcp"]);
  assert.match(source, /Audit each selected run at most once/);
  assert.match(source, /2 concurrent audits/);
  assert.match(source, /retain stderr diagnostics/);
  assert.match(source, /partial rather than a full seven-day census/);
});
