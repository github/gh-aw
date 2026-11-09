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
const lowerPriorityAlert = {
  ...alert,
  number: 43,
  rule: { id: "lower-priority-rule", security_severity_level: "medium" },
};
const fingerprint = JSON.stringify(["test-rule", "example.go", 10, "Test alert"]);

function select(records, alerts = [alert]) {
  return JSON.parse(execFileSync("jq", ["-n", "--argjson", "now", JSON.stringify(now), "--argjson", "cached", JSON.stringify([records]), "--argjson", "alerts", JSON.stringify([alerts]), selector[1]], { encoding: "utf8" }));
}

function checkpoint(startedAt = "2026-11-06T21:00:00Z", lastSuccessfulStep = "alert_selected") {
  return {
    alert_number: 42,
    fingerprint,
    outcome: "in_progress",
    started_at: startedAt,
    last_successful_step: lastSuccessfulStep,
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

test("A stalled attempt reports its cause and last successful step, then releases the alert for retry", () => {
  const stalled = select([checkpoint("2026-11-06T21:00:00Z", "vulnerability_analyzed")]);
  assert.equal(stalled.stalled_record.last_successful_step, "vulnerability_analyzed");
  assert.match(stalled.stalled_record.stall_reason, /No completion outcome was recorded within 30 minutes/);
  assert.deepEqual(stalled.candidates, []);

  const legacyStall = select([{ ...checkpoint(), last_successful_step: undefined }]);
  assert.equal(legacyStall.stalled_record.last_successful_step, "alert_selected");
  assert.equal(legacyStall.stalled_record.last_successful_step_invalid, false);

  const diagnostic = workflow.split("### 1. Select an Unfixed Alert")[1].split("### 2.")[0];
  assert.match(diagnostic, /`stall_reason`/);
  assert.match(diagnostic, /`last_successful_step`/);
  assert.match(diagnostic, /releases the fingerprint for one retry/);

  const released = select([checkpoint(), { ...checkpoint(), outcome: "stalled_reported", recorded_at: "2026-11-07T03:00:00Z" }]);
  assert.equal(released.stalled_alert_number, null);
  assert.deepEqual(
    released.candidates.map(({ number }) => number),
    [42]
  );
});

test("Unknown and non-string milestones are reported as unknown", () => {
  for (const lastSuccessfulStep of [{ x: 1 }, "unexpected-step", 7, null]) {
    const selection = select([checkpoint("2026-11-06T21:00:00Z", lastSuccessfulStep)]);
    assert.equal(selection.stalled_record.last_successful_step, "unknown");
    assert.equal(selection.stalled_record.last_successful_step_invalid, true);
  }
});

test("Missing or malformed checkpoint timestamps have an invalid-timestamp reason", () => {
  const missingTimestamp = checkpoint();
  delete missingTimestamp.started_at;

  for (const record of [missingTimestamp, checkpoint("not-a-timestamp")]) {
    const selection = select([record]);
    assert.equal(selection.stalled_record.timestamp_invalid, true);
    assert.match(selection.stalled_record.stall_reason, /missing or invalid started_at timestamp/);
    assert.doesNotMatch(selection.stalled_record.stall_reason, /within 30 minutes/);
  }
});

test("Repeated stalls are bounded by fingerprint and do not starve lower-priority alerts", () => {
  const firstStall = { ...checkpoint(), outcome: "stalled_reported" };
  const secondStall = { ...firstStall, recorded_at: "2026-11-07T03:00:00Z" };

  const oneStall = select([firstStall], [alert, lowerPriorityAlert]);
  assert.deepEqual(
    oneStall.candidates.map(({ number }) => number),
    [42]
  );

  const repeatedStall = select([firstStall, secondStall], [alert, lowerPriorityAlert]);
  assert.deepEqual(
    repeatedStall.candidates.map(({ number }) => number),
    [43]
  );

  const changedFingerprint = {
    ...alert,
    most_recent_instance: {
      ...alert.most_recent_instance,
      message: { text: "Changed alert" },
    },
  };
  const changedAlert = select([firstStall, secondStall], [changedFingerprint, lowerPriorityAlert]);
  assert.deepEqual(
    changedAlert.candidates.map(({ number }) => number),
    [42]
  );

  const selectionInstructions = workflow.split("### 1. Select an Unfixed Alert")[1].split("### 2.")[0];
  assert.match(selectionInstructions, /Do not claim all alerts are addressed/);
});

test("Every milestone checkpoint preserves alert identity and original start time", () => {
  for (const [heading, milestone] of [
    ["3. Get Alert Details", "alert_details_retrieved"],
    ["4. Analyze the Vulnerability", "vulnerability_analyzed"],
    ["5. Generate the Fix", "fix_generated_and_reviewed"],
    ["6. Create Pull Request", "patch_preflight_passed"],
  ]) {
    const section = workflow.split(`### ${heading}`)[1].split("\n### ")[0];
    const checkpointPattern = new RegExp(`\\{"alert_number": \\[alert-number\\], "fingerprint": \\[candidate fingerprint\\], "outcome": "in_progress", "started_at": "\\[original started_at\\]", "last_successful_step": "${milestone}"\\}`);
    assert.match(section, checkpointPattern);
  }
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
