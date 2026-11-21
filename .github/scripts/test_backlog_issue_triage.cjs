const assert = require("node:assert/strict");
const { Readable } = require("node:stream");
const { test } = require("node:test");
const { collectCandidates } = require("./backlog_issue_triage.cjs");

const issue = {
  number: 1,
  title: "[aw] Smoke Test: reported incomplete result",
  body: "<!-- gh-aw-failure-issue: smoke-test -->",
  user: { login: "github-actions[bot]" },
  labels: [{ name: "agentic-workflows" }],
};
const collect = issues => collectCandidates(Readable.from(issues.map(item => JSON.stringify(item) + "\n")));

test("recognizes runtime reports without treating human or unrelated workflow issues as run alerts", async () => {
  const candidates = await collect([
    issue,
    { ...issue, title: "[aw] Detection Runs", body: "" },
    { ...issue, title: "[aw] Failed jobs: Smoke Test", body: "<!-- gh-aw-agentic-workflow: smoke-test -->" },
    { ...issue, user: { login: "contributor" } },
    { ...issue, labels: [] },
    { ...issue, title: "[aw] Failed jobs: Smoke Test", body: "" },
    { ...issue, title: "[aw] Claude Sonnet fails", body: "" },
    { ...issue, title: "[aw] agentic status report", body: "<!-- gh-aw-agentic-workflow: maintenance -->" },
  ]);
  assert.deepEqual(
    candidates.map(item => item.primary_backlog_label),
    ["backlog:run-alerts", "backlog:run-alerts", "backlog:run-alerts", undefined, undefined, undefined, undefined, undefined]
  );
});

test("skips pull requests, existing backlog labels and issues left for manual triage; caps the batch", async () => {
  const candidates = await collect([
    { ...issue, pull_request: {} },
    { ...issue, labels: [{ name: "backlog:security" }] },
    { ...issue, labels: [{ name: "needs-triage" }] },
    ...Array.from({ length: 21 }, (_, index) => ({ ...issue, number: index + 10 })),
  ]);
  assert.deepEqual(
    candidates.map(item => item.number),
    Array.from({ length: 20 }, (_, index) => index + 10)
  );
});
