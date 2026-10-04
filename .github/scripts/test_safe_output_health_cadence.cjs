const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { checkCadence } = require("./safe_output_health_cadence.cjs");

async function check(t, previous, overrides = {}) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "safe-output-health-cadence-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const warnings = [];
  const requests = [];
  const summaries = [];
  const summary = {
    addHeading() {
      return this;
    },
    addRaw(message) {
      summaries.push(message);
      return this;
    },
    async write() {},
  };
  const outputPath = path.join(dir, "agent", "cadence.json");
  const result = await checkCadence({
    context: { repo: { owner: "github", repo: "gh-aw" }, runId: 382, payload: { repository: { default_branch: "main" } } },
    github: {
      async paginate(method, request) {
        return (await method(request)).data.workflow_runs;
      },
      rest: {
        actions: {
          async getWorkflowRun() {
            return { data: { workflow_id: 201005955, created_at: "2026-09-24T04:00:00Z", run_started_at: "2026-10-04T04:00:00Z" } };
          },
          async listWorkflowRuns(request) {
            requests.push(request);
            return { data: { workflow_runs: Array.isArray(previous) ? previous : previous ? [previous] : [] } };
          },
          ...overrides,
        },
      },
    },
    core: { warning: message => warnings.push(message), info() {}, summary },
    outputPath,
  });
  assert.deepEqual(JSON.parse(fs.readFileSync(outputPath, "utf8")), result);
  assert.deepEqual(summaries, [result.message]);
  return { result, warnings, requests };
}

const run = run_started_at => ({ id: 381, run_started_at, html_url: "https://github.com/github/gh-aw/actions/runs/381" });

test("daily successful audits are healthy and use only prior default-branch successes", async t => {
  const { result, warnings, requests } = await check(t, run("2026-10-03T04:00:00Z"));
  assert.equal(result.status, "healthy");
  assert.equal(result.elapsed_hours, 24);
  assert.deepEqual(warnings, []);
  assert.deepEqual(requests, [
    {
      owner: "github",
      repo: "gh-aw",
      workflow_id: 201005955,
      branch: "main",
      status: "success",
      per_page: 100,
    },
  ]);
});

test("exactly twice the daily interval does not alert", async t => {
  const { result, warnings } = await check(t, run("2026-10-02T04:00:00Z"));
  assert.equal(result.status, "healthy");
  assert.equal(result.elapsed_hours, 48);
  assert.deepEqual(warnings, []);
});

test("a gap just above 48 hours alerts", async t => {
  const { result, warnings } = await check(t, run("2026-10-02T03:59:59Z"));
  assert.equal(result.status, "stale");
  assert.equal(warnings.length, 1);
});

test("intervening failed daily attempts do not hide the September monitoring gap", async t => {
  const previous = run("2026-09-23T04:00:00Z");
  const { result, warnings } = await check(t, previous);
  assert.equal(result.status, "stale");
  assert.equal(result.elapsed_hours, 264);
  assert.deepEqual(result.previous_successful_run, { id: previous.id, run_started_at: previous.run_started_at, url: previous.html_url });
  assert.equal(warnings.length, 1);
});

test("first run without successful history is unknown rather than healthy", async t => {
  const { result, warnings } = await check(t);
  assert.equal(result.status, "unknown");
  assert.equal(warnings.length, 1);
  assert.match(result.message, /no previous successful/);
});

test("API errors do not prevent the audit and do not leak error details", async t => {
  const { result, warnings } = await check(t, null, {
    async listWorkflowRuns() {
      throw new Error("sensitive response");
    },
  });
  assert.equal(result.status, "unknown");
  assert.equal(warnings.length, 1);
  assert.doesNotMatch(JSON.stringify(result), /sensitive response/);
});

test("invalid timestamps cannot be reported as healthy", async t => {
  const { result } = await check(t, run("invalid"));
  assert.equal(result.status, "unknown");
});

test("successful reruns use execution time rather than original creation order", async t => {
  const previous = { ...run("2026-10-03T04:00:00Z"), id: 300, created_at: "2026-09-01T04:00:00Z" };
  const { result } = await check(t, [run("2026-10-01T04:00:00Z"), previous]);
  assert.equal(result.status, "healthy");
  assert.equal(result.elapsed_hours, 24);
  assert.equal(result.previous_successful_run.id, 300);
});

test("current run and attempts starting after this attempt are excluded", async t => {
  const previous = run("2026-09-23T04:00:00Z");
  const { result } = await check(t, [{ ...run("2026-10-04T04:00:00Z"), id: 382 }, { ...run("2026-10-05T04:00:00Z"), id: 383 }, previous]);
  assert.equal(result.status, "stale");
  assert.equal(result.previous_successful_run.id, previous.id);
});
