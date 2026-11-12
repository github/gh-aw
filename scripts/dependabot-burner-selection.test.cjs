"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const workflow = fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", "dependabot-burner.md"), "utf8");
const script = workflow.match(/      script: \|\n([\s\S]*?)\nevals:/);
assert.ok(script, "Missing burner prefetch script");
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const prefetch = new AsyncFunction("github", "context", "require", "process", "console", script[1].replace(/^        /gm, ""));

function pull(number, files, author = "dependabot[bot]") {
  return {
    number,
    title: `Bump dependency-${number} from 1.0.0 to 2.0.0`,
    user: { login: author },
    state: "open",
    created_at: `2026-11-${String(number).padStart(2, "0")}T00:00:00Z`,
    html_url: `https://github.com/example/repo/pull/${number}`,
    files,
  };
}

async function select(pulls, trigger = null, fallback = null) {
  let payload;
  const list = Symbol("pulls.list");
  const listFiles = Symbol("pulls.listFiles");
  const github = {
    rest: {
      pulls: {
        list,
        listFiles,
        get: async ({ pull_number }) => {
          assert.equal(pull_number, fallback?.number);
          return { data: fallback };
        },
      },
    },
    paginate: async (method, options) => {
      assert.equal(options.per_page, 100);
      if (method === list) return options.state === "closed" ? [] : pulls;
      assert.equal(method, listFiles);
      const pr = pulls.find(item => item.number === options.pull_number) || fallback;
      assert.ok(pr, "Unexpected PR file request");
      return pr.files.map(filename => ({ filename }));
    },
  };
  await prefetch(
    github,
    { repo: { owner: "example", repo: "repo" }, eventName: trigger ? "issue_comment" : "schedule", payload: trigger ? { issue: { number: trigger, pull_request: {} } } : {} },
    name => {
      if (name === "path") return path;
      assert.equal(name, "fs");
      return {
        mkdirSync() {},
        writeFileSync(filename, data) {
          assert.equal(filename, "/tmp/gh-aw/agent/dependabot-burner/context.json");
          payload = JSON.parse(data);
        },
      };
    },
    { env: {} },
    { log() {} }
  );
  return payload;
}

test("standalone AgentRx locks are reported rather than mistaken for no Dependabot PRs", async () => {
  const result = await select([pull(1, [".github/workflows/requirements-agentrx.txt"]), pull(2, [".github/workflows/requirements-agentrx.txt"])]);
  assert.equal(result.open_dependabot_pr_count, 2);
  assert.equal(result.open_pr_count, 0);
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.deepEqual(
    result.excluded_prs.map(({ number, changed_files, reason }) => ({ number, changed_files, reason })),
    [1, 2].map(number => ({ number, changed_files: [".github/workflows/requirements-agentrx.txt"], reason: "outside-generated-manifest-scope" }))
  );
});

test("generated manifests retain oldest-first grouping and ignore non-Dependabot authors", async () => {
  const result = await select([
    pull(3, [".github/workflows/package.json", ".github/workflows/package-lock.json"], "app/dependabot"),
    pull(1, [".github/workflows/requirements.txt"]),
    pull(2, ["go.mod"]),
    pull(4, [".github/workflows/go.mod"], "contributor"),
  ]);
  assert.equal(result.open_dependabot_pr_count, 3);
  assert.equal(result.open_pr_count, 2);
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 3]);
  assert.deepEqual(
    result.selected_batch_dependencies.map(item => item.manifest_families),
    [["pip"], ["npm"]]
  );
  assert.deepEqual(
    result.excluded_prs.map(item => item.number),
    [2]
  );
});

test("mixed source and generated manifest PRs are excluded with all paths preserved", async () => {
  const files = [".github/workflows/package.json", "docs/package.json"];
  const result = await select([pull(1, files)]);
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.equal(result.excluded_prs[0].reason, "mixed-generated-and-source-files");
  assert.deepEqual(result.excluded_prs[0].changed_files, files);
});

test("slash-command grouping still uses only shared generated manifests", async () => {
  const result = await select([pull(1, [".github/workflows/package.json"]), pull(2, [".github/workflows/package.json", ".github/workflows/package-lock.json"]), pull(3, [".github/workflows/requirements.txt"])], 1);
  assert.equal(result.selection_reason, "slash-command-similar-prs");
  assert.deepEqual(result.selected_batch_pr_numbers, [1, 2]);
  assert.deepEqual(
    result.related_prs.map(item => item.number),
    [2]
  );
});

test("out-of-scope slash-command triggers are explained without selecting unrelated PRs", async () => {
  const result = await select([pull(1, [".github/workflows/requirements-agentrx.txt"]), pull(2, [".github/workflows/requirements.txt"])], 1);
  assert.equal(result.selection_reason, "slash-command-trigger-not-in-scope");
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.equal(result.excluded_prs[0].number, 1);
});

test("slash-command fallback cannot admit closed or non-Dependabot PRs", async () => {
  for (const fallback of [pull(1, [".github/workflows/requirements.txt"], "contributor"), { ...pull(1, [".github/workflows/requirements.txt"]), state: "closed" }]) {
    const result = await select([], 1, fallback);
    assert.equal(result.trigger_pr, null);
    assert.deepEqual(result.selected_batch_pr_numbers, []);
  }
});

test("truly empty selection stays distinct and exclusion explanations cannot be skipped", async () => {
  const result = await select([]);
  assert.equal(result.open_dependabot_pr_count, 0);
  assert.deepEqual(result.excluded_prs, []);
  assert.deepEqual(result.selected_batch_pr_numbers, []);
  assert.match(workflow, /selected remediation batch and excluded_prs are both empty/);
  assert.match(workflow, /Do not claim that all dependency updates are handled/);
});
