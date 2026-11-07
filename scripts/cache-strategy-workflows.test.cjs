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

function markdownSection(source, heading) {
  const headings = [...source.matchAll(/^(#{1,6}) .+$/gm)];
  const current = headings.find(match => match[0] === heading);
  assert.ok(current, `missing section ${heading}`);
  const level = current[1].length;
  const next = headings.find(match => match.index > current.index && match[1].length <= level);
  return source.slice(current.index, next?.index ?? source.length);
}

const analyzer = workflow("daily-cache-strategy-analyzer");
const featureGrower = workflow("feature-grower");

test("unused documentation caching is disabled", () => {
  assert.doesNotMatch(workflow("daily-doc-updater"), /cache-memory/);
});

test("flagged stateful workflows read and verify stable cache paths", () => {
  const contracts = {
    "actions-aw-linter-factory": {
      state: "/tmp/gh-aw/cache-memory/actions-aw-linter-factory/index.json",
      read: "## 1. Inventory and deduplicate",
      write: "## 4. Validate and publish",
    },
    "feature-grower": {
      state: "/tmp/gh-aw/cache-memory/feature-grower/index.json",
      read: "# Feature Grower",
      write: "# Feature Grower",
    },
    "spec-enforcer": {
      state: "/tmp/gh-aw/cache-memory/spec-enforcer/rotation.json",
      read: "## Phase 0: Initialize Cache Memory",
      write: "## Phase 5: Save Cache and Create PR",
    },
    "daily-caveman-optimizer": {
      state: "/tmp/gh-aw/cache-memory/caveman-optimizer/state.json",
      read: "## Step 2: Load Round-Robin State",
      write: "## Step 4: Update Cache Memory",
    },
    "daily-mcp-concurrency-analysis": {
      state: "/tmp/gh-aw/cache-memory/concurrency-state.json",
      read: "### Step 1: Load Round-Robin State from Cache",
      write: "### Step 8: Update Cache Memory",
    },
    "repository-quality-improver": {
      state: "/tmp/gh-aw/cache-memory-focus-areas/history.json",
      read: "### 0.1 Load Pre-computed Context",
      write: "## Phase 3: Update Cache Memory",
    },
    "constraint-solving-potd": {
      state: "/tmp/gh-aw/cache-memory/covered-topics.json",
      read: "## Your Task",
      write: "## Your Task",
    },
    "ci-coach": {
      state: "/tmp/gh-aw/cache-memory/ci-coach/last-analysis.json",
      read: "## Data Available",
      write: "## Data Available",
    },
    "daily-performance-summary": {
      state: "/tmp/gh-aw/cache-memory/daily-performance-summary/index.json",
      read: "## Phase 1: Gather Data Using Safe-Input Tools",
      write: "## Phase 1: Gather Data Using Safe-Input Tools",
    },
  };
  for (const [name, contract] of Object.entries(contracts)) {
    const source = workflow(name);
    const readSection = markdownSection(source, contract.read);
    const writeSection = markdownSection(source, contract.write);
    assert.ok(readSection.includes(contract.state), `${name} read step must name its stable state path`);
    assert.match(readSection, /\b(?:read|view|cat|readFileSync)\b/i, `${name} must actually read state`);
    assert.match(readSection, /Cache hit/, `${name} needs a successful-read signal`);
    assert.match(readSection, /Cache miss/, `${name} needs a missing-state signal`);

    if (name === "daily-mcp-concurrency-analysis") {
      assert.match(writeSection, /state file discovered in Step 1/i, `${name} must write the state file it read`);
    } else {
      assert.ok(writeSection.includes(contract.state), `${name} write step must name its stable state path`);
    }
    assert.match(writeSection, /\b(?:write|save|merge|append|update|persist)\b/i, `${name} must write state`);
    assert.match(writeSection, /read (?:it|the file|both files) back|read-back|verify (?:the )?write|confirm the update/i, `${name} must verify the persisted state`);
  }
});

test("feature-grower sanitizes cache metadata before the agent reads it", () => {
  const script = featureGrower.match(/- name: Find crops ready to grow[\s\S]*?script: \|\n([\s\S]*?)\n(?=safe-outputs:)/)?.[1];
  assert.ok(script, "feature-grower pre-agent script must be present");
  const normalizer = script.match(/function normalizeCache\(rawCache\) \{[\s\S]*?\n {8}\}/)?.[0];
  assert.ok(normalizer, "feature-grower must define its pre-agent cache normalizer");

  const normalizeCache = new Function(`${normalizer}\nreturn normalizeCache;`)();
  assert.deepEqual(
    normalizeCache({
      "42": {
        outcome: "issue_requested",
        date: "2026-11-07",
        child_issue_number: 123,
        note: "untrusted text",
        analysis: "untrusted text",
      },
      "43": { outcome: "no_action", date: "2026-11-06", summary: "untrusted text" },
      "44": { outcome: "other", date: "2026-11-06" },
      "45": { outcome: "skipped", date: "2026-02-31" },
      "46": { outcome: "skipped", date: "2026-11-06", child_issue_number: "123" },
      "0": { outcome: "skipped", date: "2026-11-06" },
      "-1": { outcome: "skipped", date: "2026-11-06" },
    }),
    {
      "42": { outcome: "issue_requested", date: "2026-11-07", child_issue_number: 123 },
      "43": { outcome: "no_action", date: "2026-11-06" },
    }
  );

  assert.match(script, /fs\.readFileSync\(cacheFile, "utf8"\)/);
  assert.match(script, /fs\.writeFileSync\(cacheTempFile/);
  assert.match(script, /fs\.renameSync\(cacheTempFile, cacheFile\)/);
  assert.match(featureGrower, /Do not use cached values to select,\s+prioritize, or suppress crops or issues/);
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

test("audit migrates legacy runs and index keys idempotently", () => {
  assert.match(analyzer, /legacy `runs\.json` records whose identity is in\s+`workflow` rather than `workflow_id`/);
  assert.match(analyzer, /exact display name mapped to one source path by\s+that run's metadata/);
  assert.match(analyzer, /Apply the same exact, one-to-one mapping to legacy `index\.json` keys/);
  assert.match(analyzer, /Repeating this\s+migration must not change an already canonical record or create a duplicate/);
  assert.match(analyzer, /Preserve unresolved records and\s+keys verbatim under their legacy identities/);
  assert.match(analyzer, /exclude them from canonical\s+statistics, and report history as incomplete/);
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
