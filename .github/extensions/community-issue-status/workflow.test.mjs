import assert from "node:assert/strict";
import { test } from "node:test";
import { analyzeCommunityIssues, communityIssueStatus, validateSnapshot } from "./workflow.mjs";

function context(path, agent) {
  return {
    args: { path },
    phase: () => {},
    step: async (_, produce) => produce(),
    agent,
  };
}

test("groups every eligible issue into evidenced status buckets", async () => {
    const issues = Array.from({ length: 9 }, (_, i) => ({
      number: i + 1, title: `Issue ${i + 1}`, url: `https://github.com/github/gh-aw/issues/${i + 1}`,
      body: "Reported problem", createdAt: "2025-01-01T00:00:00Z", comments: [],
    }));
    const sizes = [];
    const ctx = context("/tmp/gh-aw/agent/issues.json", async prompt => {
      const chunk = JSON.parse(prompt.split("Issues: ")[1]);
      sizes.push(chunk.length);
      return { issues: chunk.map(issue => ({
        number: issue.number, category: "needs-investigation", status: "No maintainer response recorded.",
      })) };
    });
    const result = await analyzeCommunityIssues(issues, ctx);
    assert.deepEqual(sizes, [8, 1]);
    assert.deepEqual(result.buckets, {
      "awaiting-maintainer": 0, "awaiting-contributor": 0, "in-progress": 0, "needs-investigation": 9,
    });
    assert.deepEqual(result.issues.map(issue => issue.number), [1, 2, 3, 4, 5, 6, 7, 8, 9]);
});

test("rejects missing or unsafe snapshot paths", async () => {
  for (const path of [undefined, "../issues.json", "/tmp/other/issues.json",
    "/tmp/gh-aw/agent/../../etc/passwd"]) {
    await assert.rejects(communityIssueStatus.run(context(path, () => {})), /pre-fetched issue file/);
  }
});

test("rejects oversized and duplicated snapshots", () => {
  const issue = { number: 4, title: "Issue", url: "https://github.com/github/gh-aw/issues/4",
    body: "Body", createdAt: "2025-01-01T00:00:00Z", comments: [] };
  assert.throws(() => validateSnapshot([issue, issue]), /Duplicate issue numbers/);
  assert.throws(() => validateSnapshot(Array.from({ length: 61 }, (_, i) =>
    ({ ...issue, number: i + 1 }))), /oversized/);
  assert.throws(() => validateSnapshot([{ ...issue, comments: null }]), /Invalid/);
});

test("empty snapshot produces no report or subagent", async () => {
  const result = await analyzeCommunityIssues([], context("", () => {
    throw new Error("Unexpected subagent");
  }));
  assert.deepEqual(result, { status: "empty", buckets: {}, issues: [] });
});

test("rejects partial or duplicated classifications", async () => {
  const issue = { number: 4, title: "Issue", url: "https://github.com/github/gh-aw/issues/4" };
  for (const response of [null, { issues: [] }, { issues: [{ number: 5, category: "in-progress", status: "Unknown" }] }]) {
    await assert.rejects(analyzeCommunityIssues([issue], context("", async () => response)),
      /Incomplete or invalid classification/);
  }
});
