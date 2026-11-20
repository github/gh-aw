"use strict";

const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { sanitizeContent } = require("../actions/setup/js/sanitize_content.cjs");

const workflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "code-scanning-fixer.md"), "utf8");
const selectorPath = path.join(__dirname, "..", ".github", "scripts", "code-scanning-fixer-select.jq");
const contextPath = path.join(__dirname, "..", ".github", "scripts", "code-scanning-fixer-context.sh");
assert.match(workflow, /bash \.github\/scripts\/code-scanning-fixer-context\.sh/);
assert.match(fs.readFileSync(contextPath, "utf8"), /-f \.github\/scripts\/code-scanning-fixer-select\.jq/);

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

function select(records, alerts = [alert], pulls = []) {
  return JSON.parse(
    execFileSync(
      "jq",
      [
        "-n",
        "--arg",
        "repo",
        "github/gh-aw",
        "--argjson",
        "now",
        JSON.stringify(now),
        "--argjson",
        "cached",
        JSON.stringify([records]),
        "--argjson",
        "alerts",
        JSON.stringify([alerts]),
        "--argjson",
        "pulls",
        JSON.stringify([pulls]),
        "-f",
        selectorPath,
      ],
      {
        encoding: "utf8",
      }
    )
  );
}

function fetchContext(t, fixtures) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-code-scanning-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const fixturePath = path.join(directory, "fixtures.json");
  fs.writeFileSync(fixturePath, JSON.stringify(fixtures));
  fs.writeFileSync(
    path.join(directory, "gh"),
    `#!/usr/bin/env node
const fs = require("node:fs");
const fixtures = JSON.parse(fs.readFileSync(process.env.SCANNING_FIXER_FIXTURES, "utf8"));
if (!process.argv.includes("--paginate")) throw new Error("Every list must be paginated");
const endpoint = process.argv.at(-1);
const response = fixtures[endpoint];
if (!response) throw new Error("Unexpected API request: " + endpoint);
if (response.error) {
  console.error(response.error);
  process.exit(1);
}
for (const page of response.pages) console.log(JSON.stringify(page));
`,
    { mode: 0o755 }
  );
  const agentDir = path.join(directory, "agent");
  execFileSync("bash", [contextPath, agentDir, path.join(directory, "missing-cache.jsonl")], {
    cwd: path.join(__dirname, ".."),
    env: { ...process.env, PATH: `${directory}${path.delimiter}${process.env.PATH}`, GITHUB_REPOSITORY: "github/gh-aw", SCANNING_FIXER_FIXTURES: fixturePath },
    stdio: "pipe",
  });
  return {
    selection: JSON.parse(fs.readFileSync(path.join(agentDir, "alert-selection.json"), "utf8")),
    pulls: fs.existsSync(path.join(agentDir, "open-fix-pulls.json")) ? JSON.parse(fs.readFileSync(path.join(agentDir, "open-fix-pulls.json"), "utf8")) : [],
  };
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

test("AI Scan findings outrank other scanners, then severity and alert number break ties", () => {
  const critical = { ...alert, tool: { name: "CodeQL" }, rule: { id: "critical", security_severity_level: "critical" } };
  const ai = { ...lowerPriorityAlert, tool: { name: "AI Scan" } };
  const aiHigh = { ...alert, number: 44, tool: { name: "ai scan" } };
  const aiEarlier = { ...aiHigh, number: 41 };
  assert.deepEqual(
    select([], [critical, ai]).candidates.map(({ number }) => number),
    [43]
  );
  assert.deepEqual(
    select([], [ai, aiHigh, aiEarlier]).candidates.map(({ number }) => number),
    [41, 44]
  );
  const ruleOnly = { ...ai, tool: undefined, rule: { id: "ai-scan/cwe-22", severity: "note" } };
  assert.equal(select([], [critical, ruleOnly]).candidates[0].number, 43);
  assert.equal(select([], [critical]).candidates[0].number, 42);
});

test("Clusters contain at most five alerts with the same scanner, rule and parent directory", () => {
  const related = Array.from({ length: 7 }, (_, index) => ({
    ...alert,
    number: 50 + index,
    tool: { name: "AI Scan" },
    most_recent_instance: { ...alert.most_recent_instance, location: { path: `pkg/example/file-${index}.go`, start_line: 10 } },
  }));
  const otherDirectory = { ...related[0], number: 60, most_recent_instance: { ...alert.most_recent_instance, location: { path: "pkg/other/file.go", start_line: 10 } } };
  const otherRule = { ...related[0], number: 61, rule: { ...alert.rule, id: "other-rule" } };
  const otherScanner = { ...related[0], number: 62, tool: { name: "CodeQL" } };
  const selection = select([], [otherDirectory, otherRule, otherScanner, ...related.reverse()]);
  assert.deepEqual(
    selection.candidates.map(({ number }) => number),
    [50, 51, 52, 53, 54]
  );
  assert.ok(selection.candidates.every(candidate => candidate.cluster_key === selection.cluster_key));
});

test("An open cluster PR blocks only that cluster, including its unbatched alerts", () => {
  const key = select([]).cluster_key;
  const marker = `Code scanning cluster: ${key}`;
  const body = sanitizeContent(`# Security Fix\n\n${marker}`);
  assert.ok(body.includes(marker), "Cluster identity must survive safe-output publication sanitization");
  const pulls = [{ number: 100, title: "[code-scanning-fix] shared correction", body, files: [] }];
  const sameCluster = { ...alert, number: 44 };
  assert.deepEqual(
    select([], [alert, sameCluster, lowerPriorityAlert], pulls).candidates.map(({ number }) => number),
    [43]
  );
  assert.deepEqual(select([], [alert], pulls).candidates, []);
  assert.deepEqual(
    select([], [alert], [{ ...pulls[0], title: "Unrelated PR" }]).candidates.map(({ number }) => number),
    [42]
  );
  assert.deepEqual(
    select([], [alert]).candidates.map(({ number }) => number),
    [42]
  );
});

test("Pending output acceptance suppresses immediate duplicates but not failed publication forever", () => {
  const pending = { ...unavailable, cluster_key: select([]).cluster_key, outcome: "pr_pending", recorded_at: "2026-11-07T02:59:00Z", pr_temporary_id: "aw_batch" };
  assert.deepEqual(select([pending]).candidates, []);
  const sameCluster = { ...alert, number: 44 };
  assert.deepEqual(
    select([pending], [alert, sameCluster, lowerPriorityAlert]).candidates.map(({ number }) => number),
    [43]
  );
  for (const recorded_at of ["2026-11-07T02:00:00Z", "invalid"]) {
    assert.deepEqual(
      select([{ ...pending, recorded_at }]).candidates.map(({ number }) => number),
      [42]
    );
  }
});

test("A stalled alert does not prevent selecting an unrelated fixable cluster", () => {
  const selection = select([checkpoint()], [alert, lowerPriorityAlert]);
  assert.equal(selection.stalled_alert_number, "42");
  assert.deepEqual(
    selection.candidates.map(({ number }) => number),
    [43]
  );
  assert.match(workflow, /diagnostic must not block unrelated remediation/);
});

test("Any PR linking the exact alert excludes it without requiring fixer metadata", () => {
  const pull = { number: 100, title: "Human-authored fix", draft: true, body: "Fixes https://github.com/github/gh-aw/security/code-scanning/42.", files: [] };
  const selection = select([], [alert, lowerPriorityAlert], [pull]);
  assert.deepEqual(
    selection.candidates.map(({ number }) => number),
    [43]
  );
  assert.deepEqual(selection.covered_alerts, [{ number: 42, pull_numbers: [100] }]);
  for (const body of ["https://github.com/github/gh-aw/security/code-scanning/420", "https://github.com/other/repo/security/code-scanning/42"]) {
    assert.deepEqual(
      select([], [alert], [{ ...pull, body }]).candidates.map(({ number }) => number),
      [42]
    );
  }
  for (const body of ["**Alert Number**: #42", "**Alerts**: #43, #42", "Fix AI Scan #42"]) {
    assert.deepEqual(select([], [alert], [{ ...pull, body }]).candidates, []);
  }
  assert.deepEqual(select([], [alert], [{ ...pull, title: "Fix code scanning alert #42", body: "" }]).candidates, []);
  assert.deepEqual(
    select([], [alert], [{ ...pull, body: "Fix issue #42; unrelated work" }]).candidates.map(({ number }) => number),
    [42]
  );
});

test("Any open PR changing an alert path blocks remediation even without an alert reference", () => {
  const unrelated = { ...lowerPriorityAlert, most_recent_instance: { ...alert.most_recent_instance, location: { path: "unrelated.go", start_line: 10 } } };
  const pull = { number: 101, title: "Refactor example", body: null, files: ["example.go"] };
  const selection = select([], [alert, unrelated], [pull]);
  assert.deepEqual(
    selection.candidates.map(({ number }) => number),
    [43]
  );
  assert.deepEqual(selection.covered_alerts, [{ number: 42, pull_numbers: [101] }]);
  assert.deepEqual(select([], [alert], [pull]).candidates, []);
});

test("Context collection inspects every PR and file page, including renamed paths", t => {
  const result = fetchContext(t, {
    "repos/github/gh-aw/code-scanning/alerts?state=open&per_page=100": { pages: [[alert]] },
    "repos/github/gh-aw/pulls?state=open&per_page=100": { pages: [[{ number: 100, title: "Unrelated PR" }], [{ number: 101, title: "Rename", draft: true }]] },
    "repos/github/gh-aw/pulls/100/files?per_page=100": { pages: [[{ filename: "unrelated.go" }]] },
    "repos/github/gh-aw/pulls/101/files?per_page=100": { pages: [[{ filename: "another.go" }], [{ filename: "renamed.go", previous_filename: "example.go" }]] },
  });
  assert.deepEqual(result.pulls[1].files, ["another.go", "example.go", "renamed.go"]);
  assert.deepEqual(result.selection.candidates, []);
  assert.deepEqual(result.selection.covered_alerts, [{ number: 42, pull_numbers: [101] }]);
});

test("No open PRs leaves the AI-first selection eligible", t => {
  const result = fetchContext(t, {
    "repos/github/gh-aw/code-scanning/alerts?state=open&per_page=100": { pages: [[alert]] },
    "repos/github/gh-aw/pulls?state=open&per_page=100": { pages: [[]] },
  });
  assert.deepEqual(result.pulls, []);
  assert.deepEqual(
    result.selection.candidates.map(({ number }) => number),
    [42]
  );
});

test("Incomplete or failed PR coverage fails closed instead of selecting a duplicate fix", t => {
  const fixtures = {
    "repos/github/gh-aw/code-scanning/alerts?state=open&per_page=100": { pages: [[alert]] },
    "repos/github/gh-aw/pulls?state=open&per_page=100": { pages: [[{ number: 101, title: "Existing fix" }]] },
  };
  for (const response of [{ error: "API unavailable" }, { pages: [[{ status: "modified" }]] }, { pages: [Array.from({ length: 3000 }, (_, index) => ({ filename: `file-${index}.go` }))] }]) {
    const result = fetchContext(t, { ...fixtures, "repos/github/gh-aw/pulls/101/files?per_page=100": response });
    assert.equal(result.selection.status, "fetch_error");
    assert.match(result.selection.reason, /pull request #101/);
    assert.equal(result.selection.candidates, undefined);
  }
});
