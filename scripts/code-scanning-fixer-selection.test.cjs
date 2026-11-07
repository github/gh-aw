"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const workflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "code-scanning-fixer.md"), "utf8");
const selector = workflow.match(/jq -n --argjson now "\$NOW" --slurpfile cached "\$CACHE_RECORDS" --slurpfile alerts "\$ALERTS_FILE" '([\s\S]*?)' > "\$SELECTION_FILE"/);
assert.ok(selector, "Missing workflow alert-selection program");

const now = Date.parse("2026-11-07T03:00:00Z") / 1000;
const alert = {
  number: 42,
  rule: { id: "test-rule", security_severity_level: "high" },
  most_recent_instance: {
    location: { path: "example.go", start_line: 10 },
    message: { text: "Test alert" },
  },
};
const fingerprint = JSON.stringify(["test-rule", "example.go", 10, "Test alert"]);

function select(records, alerts = [alert]) {
  return JSON.parse(execFileSync("jq", ["-n", "--argjson", "now", JSON.stringify(now), "--argjson", "cached", JSON.stringify([records]), "--argjson", "alerts", JSON.stringify([alerts]), selector[1]], { encoding: "utf8" }));
}

function checkpoint(startedAt = "2026-11-06T21:00:00Z") {
  return {
    alert_number: 42,
    fingerprint,
    outcome: "in_progress",
    started_at: startedAt,
  };
}

const unavailable = {
  alert_number: 42,
  fingerprint,
  outcome: "details_unavailable",
  recorded_at: "2026-11-06T21:01:00Z",
};

test("Unavailable details records a non-fixed outcome before noop without bypassing policy", () => {
  const details = workflow.split("### 3. Get Alert Details")[1].split("### 4.")[0];
  assert.match(details, /Before `safeoutputs noop`, use `edit` to append/);
  assert.match(details, /"outcome": "details_unavailable"/);
  assert.match(details, /without `fixed_at`/);
  assert.match(details, /Do not retry through another transport or change the policy/);
});

test("Unavailable details supersedes old, recent, and malformed checkpoints and permits retry", () => {
  for (const startedAt of ["2026-11-06T21:00:00Z", "2026-11-07T02:59:00Z", "invalid"]) {
    const selection = select([checkpoint(startedAt), unavailable]);
    assert.equal(selection.stalled_alert_number, null);
    assert.equal(selection.stalled_record, null);
    assert.deepEqual(
      selection.candidates.map(({ number }) => number),
      [42]
    );
    assert.equal(selection.candidates[0].fingerprint, fingerprint);
  }
});

test("A later retry checkpoint still distinguishes a real hang from an active attempt", () => {
  const records = [checkpoint(), unavailable];
  const active = select([...records, checkpoint("2026-11-07T02:59:00Z")]);
  assert.equal(active.stalled_alert_number, null);
  assert.deepEqual(active.candidates, []);

  const stalled = select([...records, checkpoint("2026-11-06T22:00:00Z")]);
  assert.equal(stalled.stalled_alert_number, "42");
  assert.equal(stalled.stalled_record.fingerprint, fingerprint);
  assert.deepEqual(stalled.candidates, []);
});

test("Existing fixed and oversized-patch exclusions remain intact", () => {
  for (const record of [
    { ...unavailable, outcome: "fixed", fixed_at: "2026-11-06T22:00:00Z" },
    { ...unavailable, outcome: "patch_too_large" },
  ]) {
    const selection = select([checkpoint(), unavailable, record]);
    assert.equal(selection.stalled_alert_number, null);
    assert.deepEqual(selection.candidates, []);
  }
  const changed = select([{ ...unavailable, outcome: "patch_too_large", fingerprint: "older-fingerprint" }]);
  assert.deepEqual(
    changed.candidates.map(({ number }) => number),
    [42]
  );
});
