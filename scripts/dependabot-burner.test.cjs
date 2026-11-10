"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const source = fs.readFileSync(path.join(__dirname, "../.github/workflows/dependabot-burner.md"), "utf8");
const script = source.match(/^      script: \|\n((?:        .*\n|\n)+)/m);
assert.ok(script, "Missing Dependabot burner context script");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const runScript = new AsyncFunction("require", "github", "context", "process", "console", "Date", script[1].replace(/^        /gm, ""));
const now = Date.parse("2026-11-10T12:00:00Z");
const cutoff = "2026-11-07T12:00:00.000Z";

function pull(number, updatedAt, files = [".github/workflows/package.json"]) {
  return {
    number,
    title: "Bump example from 1.0.0 to 1.1.0",
    user: { login: "dependabot[bot]" },
    created_at: "2026-11-01T12:00:00Z",
    updated_at: updatedAt,
    files,
  };
}

async function select(pulls, trigger, fallback) {
  let payload;
  const methods = { list() {}, listFiles() {}, get: async () => ({ data: fallback }) };
  await runScript(
    name => (name === "fs" ? { mkdirSync() {}, writeFileSync: (_filename, content) => (payload = JSON.parse(content)) } : require(name)),
    {
      rest: { pulls: methods },
      paginate: async (method, options) => {
        if (method === methods.list) return options.state === "open" ? pulls : [];
        assert.equal(method, methods.listFiles);
        return (pulls.find(candidate => candidate.number === options.pull_number) || fallback).files.map(filename => ({ filename }));
      },
    },
    {
      repo: { owner: "example", repo: "repository" },
      eventName: trigger ? "issue_comment" : "schedule",
      payload: trigger ? { issue: { number: trigger, pull_request: {} } } : {},
    },
    { env: {} },
    { log() {} },
    class extends Date {
      static now() {
        return now;
      }
    }
  );
  return payload;
}

test("burner includes the exact 72-hour boundary and defers recent, rebased, invalid, or future updates", async () => {
  const result = await select([pull(1, "2026-11-06T12:00:00Z"), pull(2, cutoff), pull(3, "2026-11-07T12:00:00.001Z"), pull(4, "2026-11-10T11:00:00Z"), pull(5, "invalid"), pull(6, "2026-11-11T12:00:00Z")]);
  assert.equal(result.cooldown_cutoff, cutoff);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.selected_batch_dependencies.map(dependency => dependency.pr_number),
    [1, 2]
  );
  assert.deepEqual(result.deferred_pr_numbers, [3, 4, 5, 6]);
  assert.equal(result.open_pr_count, 6);
});

test("burner leaves an empty batch when every candidate is cooling down", async () => {
  const result = await select([pull(1, "2026-11-10T11:00:00Z")]);
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.deepEqual(result.deferred_pr_numbers, [1]);
});

test("burner preserves author and generated-manifest scope filtering", async () => {
  const unrelatedAuthor = pull(2, cutoff);
  unrelatedAuthor.user.login = "maintainer";
  const result = await select([pull(1, cutoff), unrelatedAuthor, pull(3, cutoff, ["package.json"])]);
  assert.deepEqual(result.selected_batch_pr_numbers, [1]);
  assert.equal(result.open_pr_count, 1);
});

test("slash commands group only cooled-down PRs sharing the triggering manifest", async () => {
  const result = await select([pull(1, cutoff), pull(2, cutoff), pull(3, "2026-11-10T11:00:00Z"), pull(4, cutoff, [".github/workflows/go.mod"])], 1);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.related_prs.map(candidate => candidate.number),
    [2]
  );
  assert.deepEqual(result.deferred_pr_numbers, [3]);
});

test("a cooling-down slash-command trigger prevents remediation of related PRs", async () => {
  const result = await select([pull(1, "2026-11-10T11:00:00Z"), pull(2, cutoff)], 1);
  assert.equal(result.selection_reason, "slash-command-trigger-in-cooldown");
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.deepEqual(result.related_prs, []);
  assert.equal(result.trigger_pr.number, 1);
});

test("slash-command fallback cannot bypass the triggering PR cooldown", async () => {
  const result = await select([pull(2, cutoff)], 1, pull(1, "2026-11-10T11:00:00Z"));
  assert.equal(result.selection_reason, "slash-command-trigger-in-cooldown");
  assert.deepEqual(result.selected_batch_pr_numbers, []);
});
