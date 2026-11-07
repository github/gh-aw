"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const os = require("node:os");
const { execFileSync } = require("node:child_process");

function workflow(name) {
  return fs.readFileSync(path.join(__dirname, "..", ".github", "workflows", `${name}.md`), "utf8");
}

const analyzer = workflow("daily-cache-strategy-analyzer");

test("unused documentation caching is disabled", () => {
  assert.doesNotMatch(workflow("daily-doc-updater"), /cache-memory/);
});

test("flagged stateful workflows read and verify stable cache paths", () => {
  const states = {
    "actions-aw-linter-factory": "/tmp/gh-aw/cache-memory/actions-aw-linter-factory/index.json",
    "feature-grower": "/tmp/gh-aw/cache-memory/feature-grower/index.json",
    "spec-enforcer": "/tmp/gh-aw/cache-memory/spec-enforcer/rotation.json",
    "daily-caveman-optimizer": "/tmp/gh-aw/cache-memory/caveman-optimizer/state.json",
    "daily-mcp-concurrency-analysis": "/tmp/gh-aw/cache-memory/concurrency-state.json",
    "repository-quality-improver": "/tmp/gh-aw/cache-memory-focus-areas/history.json",
    "constraint-solving-potd": "/tmp/gh-aw/cache-memory/covered-topics.json",
    "ci-coach": "/tmp/gh-aw/cache-memory/ci-coach/last-analysis.json",
    "daily-performance-summary": "/tmp/gh-aw/cache-memory/daily-performance-summary/index.json",
  };
  for (const [name, state] of Object.entries(states)) {
    const source = workflow(name);
    assert.ok(source.includes(state), `${name} needs a stable state path`);
    assert.match(source, /Cache hit/, `${name} needs a read signal`);
    assert.match(source, /Cache miss/, `${name} needs a missing-state signal`);
    assert.match(source, /read (?:it|both files|the file) back|Read it back/, `${name} needs write verification`);
  }
});

test("audit initializes missing files without overwriting restored state", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "cache-strategy-test-"));
  try {
    const script = analyzer.match(/```bash\n([\s\S]*?)\n```/)[1].replaceAll("/tmp/gh-aw/cache-memory/cache-strategy", directory);
    const index = path.join(directory, "index.json");
    const restored = '{"workflow":{"last_analyzed":"2026-11-01"}}\n';
    fs.writeFileSync(index, restored);
    execFileSync("bash", ["-c", script]);
    assert.equal(fs.readFileSync(index, "utf8"), restored);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(directory, "runs.json"), "utf8")), []);
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(directory, "known-issues.json"), "utf8")), {});
    execFileSync("bash", ["-c", script]);
    assert.equal(fs.readFileSync(index, "utf8"), restored);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

test("audit deduplicates runs and findings by source identity, not titles", () => {
  assert.match(analyzer, /repository-relative workflow source path as `workflow_id`/);
  assert.match(analyzer, /\(workflow_id, run_id, run_attempt\)/);
  assert.match(analyzer, /one finding per\s+workflow/);
  assert.match(analyzer, /one\s+row per workflow across all severity groups/);
  assert.match(analyzer, /query GitHub for open `\[cache-strategy\]` issues/);
  assert.match(analyzer, /If that query fails, skip new issue creation/);
  assert.match(analyzer, /cache-strategy-workflow: <workflow_id>/);
  assert.doesNotMatch(analyzer, /title → issue_number|title as key/);
});

test("audit distinguishes missing evidence and named caches from confirmed misses", () => {
  assert.match(analyzer, /metadata and usage only/);
  assert.match(analyzer, /usage\/aw_session\.jsonl/);
  assert.match(analyzer, /cache-memory-focus-areas/);
  assert.match(analyzer, /`mkdir -p` alone is idempotent setup, not evidence of a miss/);
  assert.match(analyzer, /Context only, not an independent issue threshold/);
  assert.match(analyzer, /rates only from observed hit\/miss records/);
  assert.match(analyzer, /unknown evidence breaks confirmation/);
  assert.match(analyzer, /pre-fix misses form a post-fix streak/);
  assert.match(analyzer, /exclude workflows that have since disabled\s+cache-memory/);
});
