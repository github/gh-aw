"use strict";

const assert = require("node:assert/strict");
const { test } = require("node:test");
const http = require("node:http");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { SUITE_IDS, MANDATORY, LIMITS, DECISIONS_BASE_URL, DECISIONS_MODEL, validateCatalog, summarizeDiff, postDecision, frontDoor } = require("./ci-front-door.cjs");
const { evaluate } = require("./ci-front-door-evaluate.cjs");
const catalog = require("../.github/ci/suite-catalog.json");
const diff = { files: ["pkg/console/style.go"], patch: "+change", truncated: false, unavailable: false };
const token = "test-token";

function decisionResponse({ suite_ids = ["build-wasm"], confidence = 0.99 } = {}) {
  return {
    answers: SUITE_IDS.filter(id => !MANDATORY.includes(id)).map(id => ({
      type: "predicate",
      name: id,
      probability: suite_ids.includes(id) ? 1 - confidence : confidence,
    })),
  };
}

function transport({ value = decisionResponse(), status = 200, raw, error, hanging = false } = {}) {
  const calls = [];
  const fetchImpl = async (url, options) => {
    calls.push({ url, options, body: JSON.parse(options.body) });
    if (error) throw new Error(error);
    if (hanging)
      return new Promise((resolve, reject) => {
        options.signal.addEventListener("abort", () => reject(options.signal.reason), { once: true });
      });
    return new Response(raw === undefined ? JSON.stringify(value) : raw, { status });
  };
  return { calls, fetchImpl };
}

async function exercise(options = {}, response = {}) {
  const mock = transport(response);
  const result = await frontDoor({
    catalog,
    diff,
    token,
    prompt: "Select conservatively.",
    ...options,
    decide: (key, input, opts) => postDecision(key, input, { ...opts, fetchImpl: mock.fetchImpl }),
  });
  return { result, calls: mock.calls };
}

test("GitHub selection authenticates one batched Decisions request and adds mandatory suites", async () => {
  const { result, calls } = await exercise();
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, `${DECISIONS_BASE_URL}/decisions`);
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.headers.Authorization, `Bearer ${token}`);
  assert.equal(calls[0].options.headers["Copilot-Integration-Id"], "agentic-workflows");
  assert.equal(calls[0].options.redirect, "error");
  assert.equal(calls[0].body.model, DECISIONS_MODEL);
  assert.equal(calls[0].body.input[0].role, "developer");
  assert.equal(calls[0].body.input[0].content, "Select conservatively.");
  assert.deepEqual(JSON.parse(calls[0].body.input[1].content), { diff_summary: diff, suite_catalog: catalog });
  assert.deepEqual(
    calls[0].body.questions.map(question => question.name),
    SUITE_IDS.filter(id => !MANDATORY.includes(id))
  );
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
    { answers: [] },
    { answers: decisionResponse().answers.slice(1) },
    { answers: decisionResponse().answers.map(answer => ({ ...answer, name: "unknown" })) },
    { answers: decisionResponse().answers.map(answer => ({ ...answer, name: "build-wasm" })) },
    { answers: decisionResponse().answers.map(answer => ({ type: "refusal", name: answer.name })) },
    { answers: decisionResponse().answers.map(answer => ({ ...answer, probability: null })) },
    { answers: decisionResponse().answers.map(answer => ({ ...answer, probability: 1.01 })) },
    decisionResponse({ confidence: 0.89 }),
    decisionResponse({ confidence: 0.5 }),
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
  t.mock.method(AbortSignal, "timeout", milliseconds => {
    assert.equal(milliseconds, LIMITS.timeout);
    const controller = new AbortController();
    queueMicrotask(() => controller.abort(new DOMException("Timed out", "TimeoutError")));
    return controller.signal;
  });
  const { result, calls } = await exercise({}, { hanging: true });
  assert.equal(result.fallback, true);
  assert.equal(calls.length, 1);
});

test("real HTTP streams enforce response bounds and never follow a redirect to GitHub", async t => {
  let requests = 0;
  let status = 200;
  let body = JSON.stringify(decisionResponse({ suite_ids: [], confidence: 1 }));
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
  const fetchImpl = (_url, options) => fetch(`http://127.0.0.1:${server.address().port}/v1/decisions`, options);
  const decide = (key, input, opts) => postDecision(key, input, { ...opts, fetchImpl });
  const options = { catalog, diff, token, prompt: "", decide };
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
    { diff: { ...diff, files: ["actions/setup/js/decisions_client.cjs"] } },
  ]) {
    const { result, calls } = await exercise(options);
    assert.equal(calls.length, 1);
    assert.deepEqual(result.suite_ids, SUITE_IDS);
    assert.equal(result.fallback, true);
  }
});

test("missing credentials and oversized requests fail open without contacting GitHub", async () => {
  for (const options of [{ token: undefined }, { token: "" }, { token: " " }, { prompt: "x".repeat(LIMITS.request) }]) {
    const { result, calls } = await exercise(options);
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
  const { result } = await exercise({ catalog: copy }, { value: decisionResponse({ suite_ids: [], confidence: 0.9 }) });
  assert.deepEqual(result.suite_ids, [...MANDATORY, "actions-build"]);
  assert.equal(result.metrics.estimated_test_minutes_saved, 4);
});

test("local git diff is deterministic, handles renames/deletes, bounds patches, and never fetches", t => {
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
  assert.equal(summary.truncated, false);
  assert.equal(summary.groups.other.patch_truncated, true);
  assert.ok(Buffer.byteLength(summary.groups.other.patch) <= LIMITS.groupPatch);
  assert.deepEqual(summary, summarizeDiff({ cwd }));
  assert.ok(commands.every(args => !args.includes("fetch")));
  assert.equal(summarizeDiff({ cwd, base: "missing" }).unavailable, true);
});

test("large diffs retain separate JavaScript, Go, and other inventories while ignoring lock files", async t => {
  const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "front-door-groups-"));
  t.after(() => fs.rmSync(cwd, { recursive: true, force: true }));
  const git = args => execFileSync("git", args, { cwd, stdio: "pipe" });
  git(["init"]);
  git(["config", "user.email", "test@example.test"]);
  git(["config", "user.name", "Test"]);
  git(["commit", "--allow-empty", "-m", "base"]);
  fs.mkdirSync(path.join(cwd, ".github/workflows"), { recursive: true });
  fs.writeFileSync(path.join(cwd, "generated.lock.yml"), "generated\n".repeat(LIMITS.git));
  fs.writeFileSync(path.join(cwd, ".github/workflows/test.lock.yml"), "ignored\n".repeat(9000));
  fs.writeFileSync(path.join(cwd, "app.js"), "const value = 'data';\n".repeat(2000));
  fs.writeFileSync(path.join(cwd, "app.cjs"), "module.exports = {};\n");
  fs.writeFileSync(path.join(cwd, "README.md"), "documentation change\n");
  fs.writeFileSync(path.join(cwd, "large.go"), "// large Go change\n".repeat(150000));
  for (let index = 0; index < 201; index++) fs.writeFileSync(path.join(cwd, `file-${index}.go`), "package example\n");
  git(["add", "."]);
  git(["commit", "-m", "large mixed diff"]);
  const summary = summarizeDiff({ cwd });
  assert.equal(summary.unavailable, false);
  assert.equal(summary.truncated, false);
  assert.equal(summary.files.length, 205);
  assert.ok(summary.files.every(file => !file.endsWith(".lock.yml")));
  assert.deepEqual(summary.groups.javascript.files, ["app.cjs", "app.js"]);
  assert.equal(summary.groups.go.files.length, 202);
  assert.deepEqual(summary.groups.other.files, ["README.md"]);
  assert.match(summary.groups.other.patch, /documentation change/);
  assert.equal(summary.groups.other.patch_truncated, false);
  assert.equal(summary.groups.javascript.patch_truncated, true);
  assert.equal(summary.groups.go.patch_truncated, true);
  assert.ok(Object.values(summary.groups).every(group => Buffer.byteLength(group.patch) <= LIMITS.groupPatch));
  assert.ok(Buffer.byteLength(JSON.stringify(summary)) <= LIMITS.summary);
  const { result } = await exercise({ diff: summary });
  assert.equal(result.fallback, false);
  assert.deepEqual(result.suite_ids, [...MANDATORY, "build-wasm"]);
});

test("incomplete file inventories and genuine Git failures remain fail-open", async () => {
  const git = (binary, args) => {
    if (args[0] === "rev-parse") return "a".repeat(40) + "\n";
    if (args.includes("--name-only")) return Array.from({ length: LIMITS.files + 1 }, (_, index) => `file-${index}.go\0`).join("");
    return "";
  };
  const summary = summarizeDiff({ cwd: ".", git });
  assert.equal(summary.truncated, true);
  assert.equal((await exercise({ diff: summary })).result.fallback, true);
  const failed = summarizeDiff({
    cwd: ".",
    git: () => {
      throw new Error("Git failed");
    },
  });
  assert.equal(failed.unavailable, true);
  assert.equal((await exercise({ diff: failed })).result.fallback, true);
});

test("CLI output is machine readable and missing configuration cannot skip jobs", t => {
  const output = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "front-door-output-")), "outputs");
  t.after(() => fs.rmSync(path.dirname(output), { recursive: true, force: true }));
  const stdout = execFileSync(process.execPath, [path.join(__dirname, "ci-front-door.cjs")], {
    env: { ...process.env, CI_DECISION_TOKEN: "", GITHUB_EVENT_NAME: "pull_request", GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: "" },
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
  assert.match(jobs["checkout-cache"], /copilot-requests: write/);
  assert.match(jobs["checkout-cache"], /CI_DECISION_TOKEN: \$\{\{ github.token \}\}/);
  assert.doesNotMatch(workflow, /CI_DECISION_URL|secrets\.COPILOT_GITHUB_TOKEN/);
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
