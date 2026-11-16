#!/usr/bin/env bash

set -euo pipefail

if (( $# != 2 )); then
    printf 'error: usage: verify-operational-value-evaluator.sh <evaluator.js> <fixtures.json>\n' >&2
    exit 1
fi

skill_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
repo_root=$(CDPATH='' cd -- "$skill_dir/../../.." && pwd)

node - "$repo_root" "$1" "$2" <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const [root, evaluator, fixtures] = process.argv.slice(2);
const { runCustomGraders } = require(path.join(root, "actions/setup/js/custom_graders.cjs"));
const script = fs.readFileSync(evaluator, "utf8");
assert.ok(script.trim(), "evaluator body must be nonempty");
assert.ok(Buffer.byteLength(script) <= 256 * 1024, "evaluator exceeds 256 KiB");
const cases = JSON.parse(fs.readFileSync(fixtures, "utf8"));
assert.ok(Array.isArray(cases) && cases.length >= 4, "fixtures require attained, missed, unavailable, and malformed cases");
const names = cases.map(test => test.name);
assert.equal(new Set(names).size, names.length, "fixture names must be unique");
for (const name of ["attained", "missed", "unavailable", "malformed"]) {
  assert.ok(names.includes(name), `missing ${name} fixture`);
}
let metricIDs;
const primary = new Map();
for (const test of cases) {
  assert.deepEqual(Object.keys(test).sort(), ["config", "expected", "name", "session"], "fixture fields");
  assert.ok(typeof test.name === "string" && test.name.trim(), "fixture name is required");
  assert.ok(Array.isArray(test.session), "fixture session must be an event array");
  assert.ok(test.config && typeof test.config === "object" && !Array.isArray(test.config), "fixture config must be an object");
  for (const event of test.session) {
    assert.ok(event && typeof event.type === "string" && event.type &&
      event.data && typeof event.data === "object" && !Array.isArray(event.data) &&
      event.provenance && typeof event.provenance === "object" && !Array.isArray(event.provenance),
      "fixture event requires type, data, and provenance");
  }
  const run = () => runCustomGraders(test.session, [{
    id: "operational-value",
    source: "operational-value",
    script,
    config: test.config,
  }]).output.results.find(result => result.id === "operational-value");
  const actual = run();
  assert.ok(actual, `${test.name}: operational-value result is missing`);
  assert.notEqual(actual.status, "error", `${test.name}: evaluator error: ${actual.error ?? ""}`);
  assert.deepEqual(actual.metrics, test.expected, `${test.name}: metrics differ`);
  assert.deepEqual(run(), actual, `${test.name}: nondeterministic result`);
  assert.equal(actual.source, "operational-value");
  assert.equal(actual.implementation.id, "gh-aw/operational-value");
  assert.match(actual.implementation.digest, /^[a-f0-9]{64}$/);
  assert.equal(actual.value, actual.metrics[0].value, `${test.name}: primary value`);
  assert.deepEqual(actual.diagnostics ?? {}, Object.fromEntries(actual.metrics.slice(1).map(metric => [metric.id, metric.value])));
  const ids = actual.metrics.map(metric => metric.id);
  if (metricIDs) assert.deepEqual(ids, metricIDs, `${test.name}: metric IDs or order changed`);
  metricIDs = ids;
  primary.set(test.name, actual.value);
}
assert.equal(typeof primary.get("attained"), "number");
assert.equal(typeof primary.get("missed"), "number");
assert.notEqual(primary.get("attained"), primary.get("missed"), "attained and missed primary values must differ");
assert.equal(primary.get("unavailable"), null);
assert.equal(primary.get("malformed"), null);
console.log(`verified ${evaluator}`);
NODE
