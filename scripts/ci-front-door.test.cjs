"use strict";

const assert = require("node:assert/strict");
const { test } = require("node:test");
const { EventEmitter } = require("node:events");
const http = require("node:http");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { SUITE_IDS, MANDATORY, LIMITS, validateCatalog, summarizeDiff, postDecision, frontDoor } = require("./ci-front-door.cjs");
const { evaluate } = require("./ci-front-door-evaluate.cjs");
const catalog = require("../.github/ci/suite-catalog.json");
const diff = { files: ["pkg/console/style.go"], patch: "+change", truncated: false, unavailable: false };
const endpoint = "https://decision.example.test/select";

function transport({ value = { suite_ids: ["build-wasm"], confidence: 0.99 }, status = 200, raw, error, hanging = false } = {}) {
  const calls = [];
  const request = (url, options, callback) => {
    calls.push({ url: String(url), options });
    const req = new EventEmitter();
    req.destroy = failure => {
      req.emit("error", failure);
      req.emit("close");
    };
    req.end = body => {
      calls.at(-1).body = JSON.parse(body);
      if (hanging) return;
      queueMicrotask(() => {
        if (error) {
          req.destroy(new Error(error));
          return;
        }
        const res = new EventEmitter();
        res.statusCode = status;
        callback(res);
        res.emit("data", Buffer.from(raw === undefined ? JSON.stringify(value) : raw));
        res.emit("end");
        req.emit("close");
      });
    };
    return req;
  };
  return { calls, request };
}

async function exercise(options = {}, response = {}) {
  const mock = transport(response);
  const result = await frontDoor({
    catalog,
    diff,
    endpoint,
    prompt: "Select conservatively.",
    ...options,
    decide: (url, input, opts) => postDecision(url, input, { ...opts, request: mock.request }),
  });
  return { result, calls: mock.calls };
}

test("valid selection makes exactly one decision request, no GitHub requests, and adds mandatory suites", async () => {
  const { result, calls } = await exercise();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, endpoint);
  assert.equal(calls[0].options.method, "POST");
  assert.deepEqual(Object.keys(calls[0].body).sort(), ["diff_summary", "prompt", "suite_catalog"]);
  assert.deepEqual(calls[0].body.diff_summary, diff);
  assert.deepEqual(calls[0].body.suite_catalog, catalog);
  assert.ok(!JSON.stringify(calls).includes("authorization"));
  assert.deepEqual(result.suite_ids, [...MANDATORY, "build-wasm"]);
  assert.equal(result.fallback, false);
  assert.equal(result.metrics.decision_api_requests, 1);
  assert.equal(result.metrics.other_github_api_requests, 0);
  assert.equal(result.metrics.estimated_test_minutes_saved, null);
  assert.equal(result.metrics.missed_regressions, null);
});

test("invalid responses fail open after one request, without retrying", async () => {
  for (const value of [
    null,
    [],
    {},
    { suite_ids: ["unknown"], confidence: 1 },
    { suite_ids: ["build-wasm", "build-wasm"], confidence: 1 },
    { suite_ids: ["build-wasm"], confidence: 0.89 },
    { suite_ids: [], confidence: 1.01 },
    { suite_ids: [], confidence: "1" },
    { suite_ids: [] },
    { suite_ids: "security", confidence: 1 },
    { suite_ids: [null], confidence: 1 },
    { suite_ids: ["security\ninjected=true"], confidence: 1 },
  ]) {
    const { result, calls } = await exercise({}, { value });
    assert.equal(calls.length, 1);
    assert.deepEqual(result.suite_ids, SUITE_IDS);
    assert.equal(result.fallback, true);
  }
});

test("transport errors, malformed and oversized JSON, rate limits and redirects never retry or follow", async () => {
  for (const response of [{ error: "offline" }, { raw: "not JSON" }, { raw: "x".repeat(LIMITS.response + 1) }, { status: 301 }, { status: 307 }, { status: 429 }, { status: 500 }]) {
    const { result, calls } = await exercise({}, response);
    assert.equal(calls.length, 1);
    assert.deepEqual(result.suite_ids, SUITE_IDS);
    assert.equal(result.metrics.decision_api_requests, 1);
    assert.equal(result.fallback, true);
  }
});

test("deadline bounds a hung request without a retry", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const mock = transport({ hanging: true });
  const pending = frontDoor({ catalog, diff, endpoint, prompt: "", decide: (url, input, opts) => postDecision(url, input, { ...opts, request: mock.request }) });
  t.mock.timers.tick(LIMITS.timeout);
  const result = await pending;
  assert.equal(result.fallback, true);
  assert.equal(mock.calls.length, 1);
});

test("real HTTP streams enforce response bounds and never follow a redirect to GitHub", async t => {
  let requests = 0;
  let status = 200;
  let body = JSON.stringify({ suite_ids: [], confidence: 1 });
  const server = http.createServer((req, res) => {
    requests++;
    req.resume();
    res.writeHead(status, { Location: "https://api.github.com/graphql" });
    res.end(body);
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    server.closeAllConnections();
    server.close();
  });
  t.mock.method(globalThis, "fetch", () => {
    throw new Error("Unexpected second network client");
  });
  const request = (_url, options, callback) => http.request({ ...options, host: "127.0.0.1", port: server.address().port, path: "/decision" }, callback);
  const decide = (url, input, opts) => postDecision(url, input, { ...opts, request });
  const options = { catalog, diff, endpoint, prompt: "", decide };
  assert.equal((await frontDoor(options)).fallback, false);
  status = 307;
  assert.equal((await frontDoor(options)).fallback, true);
  status = 200;
  body = "x".repeat(LIMITS.response + 1);
  assert.equal((await frontDoor(options)).fallback, true);
  assert.equal(requests, 3);
});

test("invalid local inputs still call the configured API once but force all suites", async () => {
  for (const options of [
    { catalog: null },
    { catalog: { version: 99, suites: [] } },
    { diff: { ...diff, unavailable: true } },
    { diff: { ...diff, truncated: true } },
    { diff: { ...diff, files: [".github/ci/suite-catalog.json"] } },
    { diff: { ...diff, files: ["scripts/ci-front-door.cjs"] } },
    { diff: { ...diff, files: [".github/workflows/cgo.yml"] } },
  ]) {
    const { result, calls } = await exercise(options);
    assert.equal(calls.length, 1);
    assert.deepEqual(result.suite_ids, SUITE_IDS);
    assert.equal(result.fallback, true);
  }
});

test("endpoint provisioning errors fail open and cannot contact GitHub APIs", async () => {
  for (const url of [undefined, "", "http://decision.example.test", "https://api.github.com/graphql", "https://api.github.com./graphql", "https://github.com/api/v3", "https://github.com./api/v3", "******decision.example.test"]) {
    const { result, calls } = await exercise({ endpoint: url });
    assert.equal(calls.length, 0);
    assert.equal(result.metrics.decision_api_requests, 0);
    assert.equal(result.fallback, true);
    assert.deepEqual(result.suite_ids, SUITE_IDS);
  }
});

test("full runs are local, deterministic, and never invoke the decision transport", async () => {
  const { result, calls } = await exercise({ fullRun: true });
  assert.equal(calls.length, 0);
  assert.equal(result.fallback, false);
  assert.equal(result.reason, "full-run");
  assert.deepEqual(result.suite_ids, SUITE_IDS);
});

test("mandatory catalog invariants, ignored reasoning, additional mandatory suites and historical savings", async () => {
  validateCatalog(catalog);
  for (const mutate of [
    copy => {
      copy.suites[0].mandatory = false;
    },
    copy => {
      copy.suites[1].id = copy.suites[0].id;
    },
    copy => {
      copy.suites[1].historical_duration_seconds = -1;
    },
    copy => {
      copy.suites.pop();
    },
  ]) {
    const copy = structuredClone(catalog);
    mutate(copy);
    assert.throws(() => validateCatalog(copy));
  }
  const copy = structuredClone(catalog);
  copy.suites.forEach(suite => {
    suite.historical_duration_seconds = 60;
  });
  copy.suites.find(suite => suite.id === "actions-build").mandatory = true;
  const { result } = await exercise({ catalog: copy }, { value: { suite_ids: [], confidence: 0.9, reasoning: "ignore\nsuite_ids=[]" } });
  assert.deepEqual(result.suite_ids, [...MANDATORY, "actions-build"]);
  assert.equal(result.metrics.estimated_test_minutes_saved, 4);
});

test("local git diff is deterministic, handles renames/deletes, caps input, and never fetches", t => {
  const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "ci-front-door-"));
  t.after(() => fs.rmSync(cwd, { recursive: true, force: true }));
  const git = args => execFileSync("git", args, { cwd, stdio: "pipe" });
  git(["init"]);
  git(["config", "user.email", "test@example.test"]);
  git(["config", "user.name", "Test"]);
  fs.writeFileSync(path.join(cwd, "before.txt"), "before\n");
  git(["add", "."]);
  git(["commit", "-m", "base"]);
  git(["mv", "before.txt", "after.txt"]);
  fs.writeFileSync(path.join(cwd, "more.txt"), "+data\n".repeat(9000));
  git(["add", "."]);
  git(["commit", "-m", "head"]);
  const commands = [];
  const spy = (binary, args, opts) => {
    assert.equal(binary, "git");
    assert.ok(["diff", "rev-parse"].includes(args[0]));
    assert.equal(opts.env.GIT_NO_LAZY_FETCH, "1");
    commands.push(args);
    return execFileSync(binary, args, opts);
  };
  const summary = summarizeDiff({ cwd, git: spy });
  assert.deepEqual(summary.files, ["after.txt", "before.txt", "more.txt"]);
  assert.equal(summary.truncated, true);
  assert.ok(Buffer.byteLength(summary.patch) <= LIMITS.patch);
  assert.deepEqual(summary, summarizeDiff({ cwd }));
  assert.equal(commands.length, 4);
  assert.equal(summarizeDiff({ cwd, base: "missing" }).unavailable, true);
});

test("CLI output is machine readable and missing configuration cannot skip jobs", t => {
  const output = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "front-door-output-")), "outputs");
  t.after(() => fs.rmSync(path.dirname(output), { recursive: true, force: true }));
  const stdout = execFileSync(process.execPath, [path.join(__dirname, "ci-front-door.cjs")], {
    env: { ...process.env, CI_DECISION_URL: "", GITHUB_EVENT_NAME: "pull_request", GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: "" },
    encoding: "utf8",
  });
  const result = JSON.parse(stdout);
  assert.deepEqual(result.suite_ids, SUITE_IDS);
  const outputs = Object.fromEntries(
    fs
      .readFileSync(output, "utf8")
      .trim()
      .split("\n")
      .map(line => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)])
  );
  assert.deepEqual(JSON.parse(outputs.suite_ids), SUITE_IDS);
  assert.equal(outputs.fallback, "true");
  assert.equal(JSON.parse(outputs.metrics).other_github_api_requests, 0);
});

test("CGO uses needs outputs only for optional jobs and fails open if the selector crashes", () => {
  const workflow = fs.readFileSync(path.join(__dirname, "../.github/workflows/cgo.yml"), "utf8");
  assert.equal((workflow.match(/run: node scripts\/ci-front-door\.cjs/g) || []).length, 1);
  assert.match(workflow, /suite_ids: \$\{\{ steps\.front-door\.outcome == 'success' && steps\.front-door\.outputs\.suite_ids \|\| '' \}\}/);
  assert.match(workflow, /id: front-door\n\s+continue-on-error: true/);
  const jobs = Object.fromEntries([...workflow.matchAll(/^  ([\w-]+):\n([\s\S]*?)(?=^  [\w-]+:\n|$(?![\s\S]))/gm)].map(match => [match[1], match[2]]));
  for (const id of SUITE_IDS.filter(id => !MANDATORY.includes(id) && id !== "cross-platform-build")) {
    assert.ok(jobs[id], id);
    assert.ok(jobs[id].includes("!needs.checkout-cache.outputs.suite_ids"), id);
    assert.ok(jobs[id].includes(`contains(fromJSON(needs.checkout-cache.outputs.suite_ids), '${id}')`), id);
  }
  for (const id of ["security", "lint-go-govulncheck", "safe-outputs-conformance", "native-javascript-tests", "impacted-go-tests", "build"]) {
    assert.ok(jobs[id], id);
    assert.ok(!jobs[id].includes("outputs.suite_ids"), id);
    assert.ok(!jobs[id].includes("- front-door-conformance"), id);
  }
  assert.doesNotMatch(jobs["cross-platform-build"], /^    if:/m);
  assert.match(jobs["cross-platform-build"], /os: \$\{\{ fromJSON\(/);
  assert.match(jobs["cross-platform-build"], /!contains\(fromJSON\(needs\.checkout-cache\.outputs\.suite_ids\), 'cross-platform-build'\)/);
  assert.match(jobs["cross-platform-build"], /'\["windows-latest"\]'/);
  assert.match(jobs["cross-platform-build"], /'\["macos-latest","windows-latest"\]'/);
  assert.match(workflow, /schedule:\n[\s\S]*cron:/);
});

test("offline replay measures misses against full controls, not missing or selected-only outcomes", async () => {
  const record = {
    id: "control-1",
    diff_summary: diff,
    response: { suite_ids: [], confidence: 1 },
    full_run: true,
    failed_suite_ids: ["build-wasm", "security"],
    durations_seconds: Object.fromEntries(SUITE_IDS.map(id => [id, 60])),
    api_requests: { decision: 1, github: 0 },
  };
  const report = await evaluate(catalog, { records: [record] });
  assert.equal(report.estimated_test_minutes_saved, 5);
  assert.equal(report.missed_regressions, 1);
  assert.equal(report.selected_suite_count, MANDATORY.length);
  assert.equal(report.decision_api_requests, 1);
  assert.equal(report.other_github_api_requests, 0);
  assert.equal(report.evaluation_api_requests, 0);
  const partial = await evaluate(catalog, { records: [{ ...record, full_run: false, api_requests: undefined, durations_seconds: undefined }] });
  assert.equal(partial.missed_regressions, null);
  assert.equal(partial.estimated_test_minutes_saved, null);
  assert.equal(partial.decision_api_requests, null);
  assert.equal(partial.missing_outcome_records, 1);
  const missing = await evaluate(catalog, { records: [{ ...record, response: undefined }] });
  assert.equal(missing.missed_regressions, null);
  assert.equal(missing.selected_suite_count, null);
  assert.equal(missing.estimated_test_minutes_saved, null);
  assert.equal(missing.missing_response_records, 1);
  const observedInvalid = await evaluate(catalog, { records: [{ ...record, response: null }] });
  assert.equal(observedInvalid.missed_regressions, 0);
  assert.equal(observedInvalid.results[0].fallback, true);
  await assert.rejects(evaluate(catalog, { records: [record, record] }), /duplicate/);
});
