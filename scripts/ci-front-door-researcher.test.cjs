"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");

const root = path.join(__dirname, "..");
const source = fs.readFileSync(path.join(root, ".github/workflows/ci-front-door-researcher.md"), "utf8");
const [, frontmatter, prompt] = source.split(/^---\s*$/m);
const lock = fs.readFileSync(path.join(root, ".github/workflows/ci-front-door-researcher.lock.yml"), "utf8");

test("CGO runs researcher conformance independently alongside selector conformance", () => {
  const cgo = fs.readFileSync(path.join(root, ".github/workflows/cgo.yml"), "utf8");
  const job = cgo.split("\n  front-door-conformance:\n")[1]?.split(/\n  [a-z][a-z0-9-]*:/)[0];
  assert.ok(job, "missing independent front-door-conformance job");
  assert.doesNotMatch(job, /^\s+needs:/m);
  assert.match(job, /name: Test CI Front Door conformance\s+run: \|\s+node --test scripts\/ci-front-door\.test\.cjs\s+node --test scripts\/ci-front-door-researcher\.test\.cjs/);
});

test("researcher is manually invoked and has no event-driven or downloading imports", () => {
  assert.doesNotMatch(frontmatter, /^\s+schedule:/m);
  assert.match(frontmatter, /workflow_dispatch:/);
  assert.match(frontmatter, /^\s+github: false$/m);
  assert.doesNotMatch(frontmatter, /^\s*(imports|jobs|steps|post-steps|agentic-workflows|web-fetch|web-search):/m);
  assert.doesNotMatch(frontmatter, /^\s*(pull_request|push|workflow_run):/m);
  assert.match(frontmatter, /allowed: \[\]/);
});

test("compiled researcher cannot use GitHub MCP tools or publish failure issues", () => {
  const manifestLine = lock.match(/^# gh-aw-manifest: (.+)$/m);
  assert.ok(manifestLine);
  const manifest = JSON.parse(manifestLine[1]);
  assert.ok(!(manifest.mcp_servers || []).some(server => server.name === "github"));
  assert.doesNotMatch(lock, /github-mcp-server/);
  assert.ok(manifest.mcp_servers?.find(server => server.name === "safeoutputs")?.tools.includes("report_missing_data"));
  assert.match(lock, /GH_AW_FAILURE_REPORT_AS_ISSUE: "false"/);
  assert.match(lock, /GH_AW_MISSING_DATA_REPORT_AS_FAILURE: "false"/);
  assert.match(lock, /GH_AW_REPORT_INCOMPLETE_CREATE_ISSUE: "false"/);
  assert.match(frontmatter, /report-failure-as-issue: false/);
  assert.match(frontmatter, /report-failed-jobs: false/);
  assert.match(frontmatter, /report-missing-data:\s*\n\s+report-as-failure: false/);
  assert.match(frontmatter, /report-incomplete:\s*\n\s+create-issue: false/);
  assert.match(frontmatter, /noop:\s*\n\s+report-as-issue: false/);
  assert.match(prompt, /report_missing_data/);
});

test("shared PR evidence is restored without modifying the producer cache", () => {
  assert.match(frontmatter, /cache-memory:\s*\n\s+key: copilot-pr-data\s*\n\s+restore-only: true/);
  assert.match(lock, /uses: actions\/cache\/restore@[a-f0-9]{40}/);
  assert.match(lock, /key: memory-[^\n]*copilot-pr-data/);
  assert.doesNotMatch(lock, /uses: actions\/cache\/save@/);
  assert.doesNotMatch(lock, /update_cache_memory:/);
  assert.match(prompt, /Missing, stale, malformed, truncated or inaccessible evidence/);
  assert.match(prompt, /report_missing_data/);
  assert.match(prompt, /continue with available local records/);
});

test("publication is draft-only and confined to catalog, prompt, and diff summarizer module", () => {
  assert.match(frontmatter, /create-issue: false/);
  assert.match(frontmatter, /draft: true/);
  assert.match(frontmatter, /max: 1/);
  assert.match(frontmatter, /max-patch-files: 3/);
  const allowed = frontmatter.match(/allowed-files:\n((?:\s+- "[^"]+"\n)+)/);
  assert.ok(allowed);
  assert.deepEqual(
    [...allowed[1].matchAll(/"([^"]+)"/g)].map(match => match[1]),
    [".github/ci/suite-catalog.json", ".github/ci/decision-prompt.txt", "scripts/ci-front-door-diff.cjs"]
  );
  assert.match(prompt, /\*\*the diff summarizer module only\*\*/);
  assert.match(prompt, /scripts\/ci-front-door-diff\.cjs/);
  assert.match(prompt, /Do not modify the selector module/);
  assert.doesNotMatch(frontmatter, /^\s+(contents|actions|issues|pull-requests): write/m);
});

test("catalog and runtime safety contracts are explicit", () => {
  for (const id of ["go-tests", "security", "native-javascript-tests", "build-wasm", "mcp-server-compile-test", "cross-platform-build", "alpine-container-test", "actions-build"]) {
    assert.ok(prompt.includes(`\`${id}\``), `missing suite ${id}`);
  }
  assert.match(prompt, /Preserve schema version 1/);
  assert.match(prompt, /finite nonnegative seconds/);
  assert.match(prompt, /Keep `go-tests` and `security` mandatory/);
  assert.match(prompt, /`native-javascript-tests` mandatory/);
  assert.match(prompt, /at\s+least \*\*0\.9\*\*/);
  assert.match(prompt, /all-suite fallback/);
  assert.match(prompt, /Reasoning is ignored/);
  assert.match(prompt, /diff_summary:\{\.\.\.\},suite_catalog:catalog,prompt:string/);
});

test("endpoint provisioning never authorizes research calls or weaker fallback", () => {
  assert.match(prompt, /repository variable `CI_DECISION_URL`/);
  assert.match(prompt, /no authentication token/);
  assert.match(prompt, /invalid endpoint configuration fails open with zero requests/);
  assert.match(prompt, /exactly one request, with no retries or redirects/);
  assert.match(prompt, /never provision, probe, or call that service/);
  assert.match(prompt, /make no changes: report the gap/);
});

test("candidate replay cannot reuse decisions for a different prompt or input", () => {
  assert.match(prompt, /exact matching summarized input/);
  assert.match(prompt, /catalog and prompt hashes/);
  assert.match(prompt, /is not a candidate replay/);
  assert.match(prompt, /Never invoke a remote model from a replay script/);
  assert.match(prompt, /nonempty disjoint holdout with complete outcomes/);
  assert.match(prompt, /do not propose an efficacy\s+improvement/);
  assert.match(prompt, /node --test scripts\/ci-front-door\.test\.cjs/);
  assert.match(prompt, /node scripts\/ci-front-door-evaluate\.cjs/);
  assert.match(prompt, /Never manufacture records/);
});

test("historical full-run evidence is matched without dispatch or fabricated safety", () => {
  assert.match(prompt, /one\s+already cached complete all-suite run per seven-day window/);
  assert.match(prompt, /same head SHA and compatible workflow/);
  assert.match(prompt, /Never dispatch or rerun CI/);
  assert.match(prompt, /unknown\s+regression coverage, not zero missed regressions/);
  assert.match(prompt, /Block a candidate PR on\s+any new missed regression/);
  assert.match(prompt, /raw and confirmed\s+misses separately/);
  assert.match(prompt, /weekly scheduled CGO full runs and all main-push controls/);
});

test("metrics distinguish runner time, unknowns, and platform API costs", () => {
  assert.match(prompt, /runner-minutes, not wall-clock savings/);
  assert.match(prompt, /Suite count/);
  assert.match(prompt, /Missed regressions/);
  assert.match(prompt, /API counts/);
  assert.match(prompt, /zero by design/);
  assert.match(prompt, /not zero total API calls/);
  assert.match(prompt, /net savings are unavailable/);
  assert.match(prompt, /never zero/);
  assert.match(prompt, /baseline\/candidate\s+metrics table with denominators/);
});
