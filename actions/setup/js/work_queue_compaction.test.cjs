"use strict";

const assert = require("node:assert/strict");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { canonical } = require("./work_queue_codec.cjs");
const { planFor } = require("./work_queue_compaction_plan.cjs");
const { main: apply, readPlan } = require("./work_queue_compaction_apply.cjs");
const { fakeGitHub } = require("./work_queue_store_checks.cjs");

function planFile(plan) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "work-queue-compaction-test-"));
  const file = path.join(directory, "plan.json");
  fs.writeFileSync(file, canonical(plan));
  return { file, directory };
}

function publisherOptions(githubClient, actorId = "11") {
  const sha = "b".repeat(40);
  githubClient.rest.repos.get = async () => ({ status: 200, data: { full_name: "owner/repo", id: 1, default_branch: "main", size: 1 } });
  githubClient.rest.actions.getWorkflowRun = async () => ({
    status: 200,
    data: {
      id: "123",
      run_attempt: 1,
      repository: { full_name: "owner/repo", id: 1 },
      path: ".github/workflows/agentics-maintenance.yml",
      event: "schedule",
      head_sha: sha,
      actor: { id: 11 },
      created_at: "2026-10-08T00:00:00Z",
    },
  });
  return {
    githubClient,
    owner: "owner",
    repo: "repo",
    context: {
      repo: { owner: "owner", repo: "repo" },
      runId: "123",
      runAttempt: 1,
      actorId,
      sha,
      eventName: "schedule",
      payload: { repository: { id: 1 } },
    },
    workflowRef: `owner/repo/.github/workflows/agentics-maintenance.yml@${sha}`,
  };
}

test("planning skips absent queues and binds the current head and tip", () => {
  assert.equal(planFor({ sha: null, transactions: [] }), null);
  const current = { sha: "a".repeat(40), branch: "work-queue", state: { tip: "tip" }, transactions: [{}, {}] };
  const plan = planFor(current);
  assert.equal(plan.base_sha, current.sha);
  assert.equal(plan.tip, "tip");
  assert.equal(
    plan.plan_id,
    createHash("sha256")
      .update(canonical({ version: 1, branch: "work-queue", base_sha: current.sha, tip: "tip" }))
      .digest("hex")
  );
});

test("apply rejects malformed plans and defers stale plans without writing", async () => {
  const original = global.core;
  global.core = { setOutput() {}, info() {}, warning() {} };
  const valid = planFor({ sha: "a".repeat(40), branch: "work-queue", state: { tip: "tip" }, transactions: [{}, {}] });
  const { directory, file } = planFile(valid);
  try {
    assert.equal(canonical(readPlan(file)), canonical(valid));
    fs.writeFileSync(file, canonical({ ...valid, tip: "tampered" }));
    assert.throws(() => readPlan(file), /identity mismatch/);
    fs.writeFileSync(file, canonical(valid));
    const fake = fakeGitHub();
    const result = await apply({ ...publisherOptions(fake.githubClient), planFile: file });
    assert.equal(result.status, "deferred");
    assert.equal(fake.state.updates, 0);
  } finally {
    global.core = original;
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test("apply authenticates the administrator actor against the native run", async () => {
  const original = global.core;
  global.core = { setOutput() {}, info() {}, warning() {} };
  const plan = planFor({ sha: "a".repeat(40), branch: "work-queue", state: { tip: "tip" }, transactions: [{}, {}] });
  const { directory, file } = planFile(plan);
  try {
    const unauthenticated = fakeGitHub();
    await assert.rejects(apply({ ...publisherOptions(unauthenticated.githubClient, "12"), planFile: file }), /publisher_principal_mismatch/);
    assert.equal(unauthenticated.state.updates, 0);

    const mismatchedActor = fakeGitHub();
    await assert.rejects(
      apply({
        ...publisherOptions(mismatchedActor.githubClient),
        actor: { role: "administrator", repository: "owner/repo", principal: "99" },
        planFile: file,
      }),
      /request origin differs from authenticated caller/
    );
    assert.equal(mismatchedActor.state.updates, 0);
  } finally {
    global.core = original;
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
