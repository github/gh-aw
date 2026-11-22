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

test("preserves existing primary labels, including on recognized run alerts", async () => {
  const candidates = await collect([
    { ...issue, pull_request: {} },
    { ...issue, labels: [...issue.labels, { name: "backlog:security" }, { name: "needs-triage" }] },
  ]);
  assert.deepEqual(candidates, []);
});

test("revisits needs-triage issues without a primary label, but prioritizes first attempts and known alerts", async () => {
  const needsTriage = { name: "needs-triage" };
  const candidates = await collect([
    ...Array.from({ length: 21 }, (_, index) => ({ ...issue, number: index + 10, labels: [needsTriage] })),
    { ...issue, number: 31, labels: [] },
    { ...issue, number: 32, labels: [...issue.labels, needsTriage] },
  ]);
  assert.deepEqual(
    candidates.map(item => item.number),
    [31, 32, ...Array.from({ length: 18 }, (_, index) => index + 10)]
  );
  assert.equal(candidates[1].primary_backlog_label, "backlog:run-alerts");
  assert.deepEqual(candidates[2].labels, ["needs-triage"]);
  assert.equal(candidates[2].primary_backlog_label, undefined);
});
