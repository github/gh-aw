"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../.github/workflows/copilot-workflow-approval.yml"), "utf8");
const body = source.split("          script: |\n")[1].replace(/^            /gm, "");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const report = new AsyncFunction("github", "context", "core", body);
const repository = { full_name: "github/gh-aw" };
const pull = {
  number: 42,
  user: { login: "Copilot", type: "Bot" },
  head: { ref: "copilot/fix", sha: "abc123", repo: repository },
};
const run = {
  id: 100,
  workflow_id: 10,
  name: "CJS",
  event: "pull_request",
  conclusion: "action_required",
  head_sha: pull.head.sha,
  head_repository: repository,
  head_branch: pull.head.ref,
};
const statusContext = "Copilot workflow approval / 10/pull_request";

async function check({ pulls = [pull], runs = [run], statuses = [], trigger } = {}) {
  const writes = [];
  const requests = [];
  const listPulls = () => {};
  const listRuns = () => {};
  await report(
    {
      paginate: async (method, parameters) => {
        requests.push(parameters);
        return method === listPulls ? pulls : runs;
      },
      rest: {
        pulls: { list: listPulls },
        actions: { listWorkflowRunsForRepo: listRuns },
        repos: {
          getCombinedStatusForRef: async () => ({ data: { statuses } }),
          createCommitStatus: async parameters => writes.push(parameters),
        },
      },
    },
    { repo: { owner: "github", repo: "gh-aw" }, serverUrl: "https://github.com", payload: { workflow_run: trigger } },
    { info() {} }
  );
  return { writes, requests };
}

test("monitor has no checkout, dispatch, approval API or write-capable Actions token", () => {
  assert.match(source, /types: \[completed\]/);
  assert.match(source, /schedule:\n +\- cron:/);
  assert.match(source, /permissions: \{\}/);
  assert.match(source, /actions: read\n +pull-requests: read\n +statuses: write/);
  assert.match(source, /actions\/github-script@[a-f0-9]{40}/);
  assert.doesNotMatch(source, /uses: actions\/checkout|actions: write|secrets\.|approveWorkflowRun|reRunWorkflow|createWorkflowDispatch/);
});

test("approval-gated runs produce a pending PR status linked to the blocked run", async () => {
  const { writes, requests } = await check();
  assert.deepEqual(writes, [
    {
      owner: "github",
      repo: "gh-aw",
      sha: pull.head.sha,
      context: statusContext,
      state: "pending",
      description: "CJS: approval required; review the PR, then Approve and run workflows.",
      target_url: "https://github.com/github/gh-aw/actions/runs/100",
    },
  ]);
  assert.equal(requests[1].head_sha, pull.head.sha);
  assert.equal(requests[1].branch, pull.head.ref);
});

test("completed events scope discovery to the triggering branch", async () => {
  const { requests } = await check({ trigger: run });
  assert.equal(requests[0].head, "github:copilot/fix");
});

for (const conclusion of ["success", "failure", "cancelled", null]) {
  test(`recovery clears only an existing pending approval notice (${conclusion})`, async () => {
    const runs = [{ ...run, conclusion }];
    assert.deepEqual((await check({ runs })).writes, []);
    const { writes } = await check({ runs, statuses: [{ context: statusContext, state: "pending" }] });
    assert.equal(writes.length, 1);
    assert.equal(writes[0].state, "success");
    assert.match(writes[0].description, /workflow result is reported separately/);
  });
}

test("duplicate events and scheduled reconciliation do not repeat unchanged statuses", async () => {
  const { writes } = await check();
  assert.deepEqual((await check({ statuses: writes })).writes, []);
});

test("latest run wins even if older gated runs arrive out of order", async () => {
  const runs = [{ ...run, id: 101, conclusion: "success" }, run];
  assert.deepEqual((await check({ runs })).writes, []);
});

test("different workflows and trigger types retain separate approval notices", async () => {
  const runs = [run, { ...run, id: 101, event: "pull_request_review_comment" }, { ...run, id: 102, name: "CGO", workflow_id: 11 }];
  const { writes } = await check({ runs });
  assert.equal(new Set(writes.map(status => status.context)).size, 3);
});

for (const [label, overrides] of [
  ["human branch author", { user: { login: "Copilot", type: "User" } }],
  ["untrusted bot", { user: { login: "other[bot]", type: "Bot" } }],
  ["fork PR", { head: { ...pull.head, repo: { full_name: "attacker/gh-aw" } } }],
  ["deleted fork", { head: { ...pull.head, repo: null } }],
  ["non-Copilot branch", { head: { ...pull.head, ref: "feature/fix" } }],
]) {
  test(`${label} cannot cause status writes`, async () => {
    const { writes, requests } = await check({ pulls: [{ ...pull, ...overrides }] });
    assert.deepEqual(writes, []);
    assert.equal(requests.length, 1);
  });
}

for (const [label, overrides] of [
  ["stale commit", { head_sha: "old" }],
  ["fork run", { head_repository: { full_name: "attacker/gh-aw" } }],
  ["unrelated workflow", { name: "Deploy production" }],
]) {
  test(`${label} is ignored`, async () => {
    assert.deepEqual((await check({ runs: [{ ...run, ...overrides }] })).writes, []);
  });
}

test("untrusted workflow_run origins are rejected before API calls", async () => {
  for (const trigger of [
    { ...run, head_repository: null },
    { ...run, head_branch: "main" },
  ]) {
    assert.deepEqual(await check({ trigger }), { writes: [], requests: [] });
  }
});

test("legacy Copilot bot identity is accepted", async () => {
  const { writes } = await check({ pulls: [{ ...pull, user: { login: "copilot[bot]", type: "Bot" } }] });
  assert.equal(writes.length, 1);
});
