"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { canonicalJSON, sha256, buildSegment } = require("../actions/setup/js/ledger_store.cjs");
const {
  ideaId,
  equivalent,
  duplicate,
  validateBatch,
  validatePatch,
  pages,
  loadHistory,
  loadLegacyPriorArt,
  loadLedgerRecords,
  checkUnstructuredHistory,
  gateOutput,
  incompleteOutput,
  indexedHistory,
  mechanismQueries,
  checkPriorRuns,
  stampCheckpoint,
  validateRecord,
  metadata,
  metadataBlock,
} = require("./harness-research-gate.cjs");
const { validateItem, resetValidationConfigCache } = require("../actions/setup/js/safe_output_type_validator.cjs");
const { sanitizeContent } = require("../actions/setup/js/sanitize_content.cjs");

const proposal = { area: "graders", component: "session graders", mechanism: "cache deterministic evaluator results", objective: "reduce repeated evaluation latency" };
const candidate = {
  version: 2,
  id: ideaId(proposal),
  proposal,
  aliases: [],
  status: "proposal_attempt",
  reason: "Distinct mechanism absent from repository and reviewed historical proposals.",
  run_id: "100",
  run_attempt: "1",
  analyzed_at: "2026-11-08T09:00:00Z",
  sources: [
    {
      url: "https://arxiv.org/abs/2611.01234",
      published: "2026-11-07",
      publication_note: "Verified first-publication date from arXiv metadata",
      accessed_at: "2026-11-08T09:00:00Z",
      finding: "The evaluated mechanism caches deterministic evaluator results by content digest.",
      relevance: "Session graders can reuse isolated deterministic results for identical inputs.",
    },
    {
      url: "https://github.com/example/research",
      published: null,
      publication_note: "The repository has no single publication date; the inspected revision documents the mechanism.",
      accessed_at: "2026-11-08T09:00:00Z",
      finding: "An inspected implementation verifies all grader input hashes before cache reuse.",
      relevance: "This maps to existing grader execution with bounded cache invalidation rules.",
    },
  ],
  scores: { novelty: 4, evidence: 4, impact: 4, actionability: 4, feasibility: 4, cost: 4 },
  score_reasons: {
    novelty: "No equivalent mechanism was present in source or historical technical proposals.",
    evidence: "The paper and inspected implementation independently support the claimed mechanism.",
    impact: "The measured baseline supports a bounded latency improvement for repeated sessions.",
    actionability: "The plan changes one bounded existing interface with measurable acceptance criteria.",
    feasibility: "Existing digest and cache primitives can implement the proposal without blockers.",
    cost: "Sixteen estimated implementation hours place this work in the cost score four band.",
  },
  implementation_hours: 16,
  security: "The cache retains existing trust boundaries and read-only permissions; credentials are excluded from keys and values, and isolated execution remains mandatory.",
  tradeoffs: "Content hashing and invalidation increase implementation complexity and storage cost; incorrect reuse must be prevented even when that reduces the achievable hit rate.",
  validation: { metric: "grader latency milliseconds", baseline: 100, target: 80, direction: "lower", samples: 30 },
  repository_evidence: [
    { path: "pkg/workflow/compiler.go", line: 1, quote: "trusted quoted source" },
    { path: "actions/setup/js/ledger_append.cjs", line: 1, quote: "another trusted quoted source" },
  ],
};

function copy(value) {
  return JSON.parse(JSON.stringify(value));
}

function adrPatch(record = candidate) {
  const filename = `docs/adr/harness-${record.id.slice(7)}.md`;
  const lines = [
    "# Content-keyed session graders",
    `Idea-ID: ${record.id}`,
    `Proposal-area: ${record.proposal.area}`,
    `Proposal-component: ${record.proposal.component}`,
    `Scores: ${Object.entries(record.scores)
      .map(([name, score]) => `${name}=${score}`)
      .join(" ")}`,
    "## Status",
    "Proposed",
    "## Context",
    ...record.repository_evidence.map(evidence => `${evidence.path}:${evidence.line} ${evidence.quote}`),
    "## Decision",
    record.proposal.mechanism,
    record.proposal.objective,
    "## Alternatives",
    "Recompute every time; invalidation is simpler but more expensive.",
    "## Consequences",
    "Bounded performance benefit with invalidation risks.",
    "## Security",
    record.security,
    "## Tradeoffs",
    record.tradeoffs,
    "## Implementation plan",
    "Implement later, following human ADR review.",
    `Estimated implementation hours: ${record.implementation_hours}`,
    "## Validation",
    "Measure cache hits and compare outputs before rollout.",
    `Validation: metric=${record.validation.metric}; baseline=${record.validation.baseline}; target=${record.validation.target}; direction=${record.validation.direction}; samples=${record.validation.samples}`,
    "## Sources",
    ...record.sources.flatMap(source => [source.url, `Published: ${source.published ?? "unknown"}`, `Publication note: ${source.publication_note}`, source.finding]),
  ];
  return `From ${"a".repeat(40)} Mon Sep 17 00:00:00 2001\nSubject: [PATCH] ADR\n\ndiff --git a/${filename} b/${filename}\nnew file mode 100644\nindex 0000000..aaaaaaa\n--- /dev/null\n+++ b/${filename}\n@@ -0,0 +1,${lines.length} @@\n${lines.map(line => `+${line}`).join("\n")}\n-- \n2.43.0\n`;
}

function batch(record = candidate) {
  return {
    items: [
      { type: "ledger_append", ledger: "harness-proposals", operation: "append", value: copy(record) },
      { type: "create_pull_request", branch: `harness-${record.id.slice(7)}`, title: `ADR: session cache ${record.id}`, body: `Research proposal with source-grounded context.\n${metadataBlock(record)}` },
    ],
  };
}

function gate(output = batch(), options = {}) {
  return validateBatch(output, { patch: adrPatch(), runId: "100", ...options });
}

function persisted(record = candidate) {
  const envelope = { version: 1, id: "ldg-00000000-0000-4000-8000-000000000000", type: "append", timestamp: "2026-11-08T09:00:00.000Z", parents: [], payload: { operation: "append", value: record } };
  return { ...envelope, sha: sha256(envelope) };
}

function api({ runs = [], jobs = [], issues = [], records = [], branch = records.length > 0, truncated = false } = {}) {
  const calls = [];
  const request = async (route, params) => {
    calls.push({ route, params });
    if (route.endsWith("/runs")) return { data: { workflow_runs: runs, total_count: runs.length } };
    if (route.endsWith("/jobs")) return { data: { jobs, total_count: jobs.length } };
    if (route === "GET /search/issues") return { data: { items: issues.map((issue, index) => ({ number: index + 1, ...issue })), total_count: issues.length, incomplete_results: false } };
    if (route.endsWith("/branches")) return { data: [] };
    if (route.includes("/git/ref/")) {
      if (!branch) throw Object.assign(new Error("Not found"), { status: 404 });
      return { data: { object: { sha: "commit" } } };
    }
    if (route.endsWith("/git/commits/commit")) return { data: { tree: { sha: "tree" } } };
    if (route.includes("/git/trees/")) return { data: { truncated, tree: records.length ? [{ path: "ledger/shards/00000000-0000-4000-8000-000000000000.jsonl", type: "blob", mode: "100644", size: 4096, sha: "blob" }] : [] } };
    if (route.endsWith("/git/blobs/blob")) return { data: { encoding: "base64", content: Buffer.from(records.map(record => canonicalJSON(record)).join("\n") + "\n").toString("base64") } };
    throw new Error(`Unexpected route ${route}`);
  };
  return { request, calls };
}

test("canonical technical IDs normalize synonyms, punctuation, Unicode, case and word order", () => {
  const other = { area: "graders", component: "GRADERS session", mechanism: "Use results, deterministic, EVALUATORS cached", objective: "latency reduce repeated grading" };
  assert.equal(ideaId(proposal), ideaId(other));
  assert.match(ideaId(proposal), /^sha256:[a-f0-9]{64}$/);
  assert.notEqual(ideaId(proposal), ideaId({ ...proposal, mechanism: "isolate grader execution in microvm" }));
  assert.throws(() => ideaId({ ...proposal, title: "Some new paper" }), /only area/);
});

test("actual gate permits exactly one grounded proposal_attempt ADR request", () => {
  assert.equal(gate().id, candidate.id);
});

test("duplicate canonical IDs and semantic equivalents in one run are rejected", () => {
  const output = batch();
  output.items.unshift(copy(output.items[0]));
  assert.throws(() => gate(output), /Duplicate canonical IDs/);
  const semantic = { ...copy(candidate), proposal: { ...proposal, objective: "reduce grader compute resource use" }, status: "rejected" };
  semantic.id = ideaId(semantic.proposal);
  assert.ok(equivalent(proposal, semantic.proposal));
  output.items[0].value = semantic;
  assert.throws(() => gate(output), /semantic alias/);
});

test("all historical decisions suppress reproposals, including failed reservations and aliases", () => {
  for (const status of ["proposal_attempt", "reserved", "rejected", "implemented", "proposed"]) {
    assert.throws(() => gate(batch(), { history: [{ ...copy(candidate), status }] }), /Known technical idea/);
  }
  const prior = { ...copy(candidate), proposal: { ...proposal, mechanism: "isolate grader execution in microvm" }, aliases: [proposal] };
  prior.id = ideaId(prior.proposal);
  assert.ok(duplicate(candidate, [prior]));
  assert.throws(() => gate(batch(), { history: [prior] }), /Known technical idea/);
});

test("metadata mismatch, unsourced claims, low scores, overrides and fake success are rejected", () => {
  for (const mutate of [
    output => {
      output.items[0].value.id = `sha256:${"b".repeat(64)}`;
    },
    output => {
      output.items[0].value.scores.evidence = 3;
    },
    output => {
      delete output.items[0].value.scores.feasibility;
    },
    output => {
      output.items[0].value.implementation_hours = 80;
    },
    output => {
      output.items[0].value.validation.target = 120;
    },
    output => {
      output.items[0].value.validation.samples = 1;
    },
    output => {
      output.items[0].value.security = "No security risk";
    },
    output => {
      output.items[0].value.sources[0].published = null;
    },
    output => {
      output.items[0].value.sources[0].published = "2026-02-31";
    },
    output => {
      output.items[0].value.sources[1].publication_note = "";
    },
    output => {
      output.items[0].value.sources.pop();
    },
    output => {
      output.items[0].value.status = "proposed";
    },
    output => {
      output.items[1].base = "other";
    },
    output => {
      output.items[1].body = "No structured proposal";
    },
    output => {
      output.items[1].branch = "harness-other";
    },
    output => {
      output.items.push(copy(output.items[1]));
    },
    output => {
      output.items.push({ type: "create_issue", title: "Fallback" });
    },
  ]) {
    const output = batch();
    mutate(output);
    assert.throws(() => gate(output));
  }
});

test("noop and incomplete are separate outcomes; incomplete cannot persist decisions", () => {
  const scan = { version: 1, kind: "scan", status: "complete", run_id: "100", run_attempt: "1", analyzed_at: "2026-11-08T09:00:00Z" };
  assert.equal(
    gate({
      items: [
        { type: "ledger_append", ledger: "harness-proposals", operation: "append", value: scan },
        { type: "noop", message: "Complete scan with no novel idea." },
      ],
    }),
    null
  );
  assert.throws(() => gate({ items: [{ type: "noop", message: "Unchecked empty scan." }] }), /checkpoint/);
  assert.equal(gate({ items: [{ type: "report_incomplete", reason: "arXiv unavailable." }] }), null);
  const output = batch({ ...copy(candidate), status: "rejected" });
  output.items[1] = { type: "report_incomplete", reason: "arXiv unavailable." };
  assert.throws(() => gate(output), /cannot persist/);
});

test("NEW one-file regular ADR patch is required with stable ID and cited data", () => {
  for (const patch of [
    adrPatch().replace("new file mode 100644", "new file mode 120000"),
    adrPatch().replace("--- /dev/null", `--- a/docs/adr/harness-${candidate.id.slice(7)}.md`),
    adrPatch() + "\ndiff --git a/production.go b/production.go\n",
    adrPatch().replace(`Idea-ID: ${candidate.id}`, "Idea-ID: fake"),
    adrPatch().replace("## Validation", "## Test sometime"),
    adrPatch().replace(candidate.sources[0].finding, "Unsupported claim"),
    adrPatch().replace(`Proposal-component: ${proposal.component}`, "Proposal-component: different component"),
    adrPatch().replace("evidence=4", "evidence=3"),
    adrPatch().replace("## Security", "## Safety someday"),
    adrPatch().replace("baseline=100; target=80", "baseline=100; target=90"),
    adrPatch().replace("Published: 2026-11-07", "Published: 2026-11-06"),
  ])
    assert.throws(() => validatePatch(patch, candidate));
});

test("trusted repository quotations are actually checked", () => {
  const record = copy(candidate);
  record.repository_evidence = [
    { path: "actions/setup/js/ledger_append.cjs", line: 4, quote: 'const fs = require("node:fs");' },
    { path: "scripts/harness-research-gate.cjs", line: 4, quote: 'const path = require("node:path");' },
  ];
  const root = path.join(__dirname, "..");
  assert.equal(validateBatch(batch(record), { patch: adrPatch(record), runId: "100", root }).id, record.id);
  record.repository_evidence[0].quote = "fabricated source quotation";
  assert.throws(() => validateBatch(batch(record), { patch: adrPatch(record), runId: "100", root }), /does not match/);
});

test("production history helper preserves immutable record statuses and aliases", async () => {
  const record = { ...copy(candidate), status: "rejected" };
  const client = api({ records: [persisted(record)] });
  const { history } = await loadHistory(client.request, "o/r", "100");
  assert.equal(history.length, 1);
  assert.equal(history[0].status, "rejected");
  assert.throws(() => gate(batch(), { history }), /Known technical idea/);
});

test("failed native PR creation or ledger persistence fail closed on actual history helper", async () => {
  for (const jobs of [
    [
      { name: "safe_outputs", conclusion: "success" },
      { name: "push_ledger_changes", conclusion: "failure" },
    ],
    [{ name: "safe_outputs", conclusion: "cancelled" }],
    [],
  ]) {
    const client = api({ runs: [{ id: 99, status: "completed", conclusion: "failure" }], jobs });
    await assert.rejects(loadHistory(client.request, "o/r", "100"), /uncertain/);
  }
});

test("production publication sequence records only an attempt and suppresses every uncertain retry", async () => {
  const context = { ref: "refs/heads/main", payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  const first = await gateOutput(batch(), { request: api().request, context, patch: adrPatch() });
  assert.equal(first.status, "proposal_attempt");
  const next = { ...copy(candidate), run_id: "101" };
  const nextContext = { ...context, runId: 101 };
  // Native ledger persistence is always() after safe_outputs, so a failed PR
  // may still leave a durable attempt. Neither that write nor queuing is success.
  const failedPR = api({
    runs: [{ id: 100, status: "completed", conclusion: "failure" }],
    jobs: [
      { name: "safe_outputs", conclusion: "failure" },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
    records: [persisted(first)],
  });
  await assert.rejects(gateOutput(batch(next), { request: failedPR.request, context: nextContext, patch: adrPatch(next) }), /Known technical idea/);
  const different = copy(next);
  different.proposal.mechanism = "isolate grader execution inside microvm boundaries";
  different.id = ideaId(different.proposal);
  const newDay = await gateOutput(batch(different), { request: failedPR.request, context: nextContext, patch: adrPatch(different) });
  assert.equal(newDay.status, "proposal_attempt", "Durable failed PR attempt blocks its idea, not every future proposal");
  const completed = api({
    runs: [{ id: 100, status: "completed", conclusion: "success" }],
    jobs: [
      { name: "safe_outputs", conclusion: "success" },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
    records: [persisted(first)],
    issues: [{ title: "Prior ADR", body: batch().items[1].body }],
  });
  await assert.rejects(gateOutput(batch(next), { request: completed.request, context: nextContext, patch: adrPatch(next) }), /Known technical idea/);
  const failedLedger = api({
    runs: [{ id: 100, status: "completed", conclusion: "failure" }],
    jobs: [
      { name: "safe_outputs", conclusion: "success" },
      { name: "push_ledger_changes", conclusion: "failure" },
    ],
    issues: [{ title: "Prior ADR", body: batch().items[1].body }],
  });
  await assert.rejects(gateOutput(batch(next), { request: failedLedger.request, context: nextContext, patch: adrPatch(next) }), /uncertain/);
});

test("same historical snapshots are not a lock; whole-workflow serialization enables durable replay suppression", async () => {
  const context = { ref: "refs/heads/main", payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  const next = { ...copy(candidate), run_id: "101" };
  const results = await Promise.all([gateOutput(batch(), { request: api().request, context, patch: adrPatch() }), gateOutput(batch(next), { request: api().request, context: { ...context, runId: 101 }, patch: adrPatch(next) })]);
  assert.ok(
    results.every(record => record.status === "proposal_attempt"),
    "Snapshot checks alone deliberately do not claim an atomic distributed lock"
  );
  const persistedFirst = api({ records: [persisted(results[0])] });
  await assert.rejects(gateOutput(batch(next), { request: persistedFirst.request, context: { ...context, runId: 101 }, patch: adrPatch(next) }), /Known technical idea/);
  const lock = fs.readFileSync(path.join(__dirname, "..", ".github/workflows/daily-harness-researcher.lock.yml"), "utf8");
  assert.match(lock, /\nconcurrency:\n  cancel-in-progress: false\n  group: daily-harness-researcher/);
});

test("trusted legacy prior-art loading preserves rejection provenance and detects semantic repeats", async () => {
  const rejected = [{ decision: "REJECT", workflow_id: "session-graders", mechanism: proposal.mechanism, primary_dimension: "output processing" }];
  const request = async route => {
    if (route.endsWith("/branches")) return { data: [{ name: "memory/harness-experiment-proposer", commit: { sha: "legacy" } }] };
    if (route.endsWith("/git/commits/legacy")) return { data: { tree: { sha: "legacy-tree" } } };
    if (route.endsWith("/git/trees/legacy-tree")) return { data: { truncated: false, tree: [{ path: "harness-proposals.json", mode: "100644", type: "blob", size: 1000, sha: "legacy-blob" }] } };
    if (route.endsWith("/git/blobs/legacy-blob")) return { data: { encoding: "base64", content: Buffer.from(JSON.stringify(rejected)).toString("base64") } };
    throw new Error("Unexpected legacy route");
  };
  const prior = await loadLegacyPriorArt(request, "o/r");
  assert.match(prior[0].body, /REJECT/);
  assert.throws(() => checkUnstructuredHistory(candidate, prior), /semantic duplicate/);
  await assert.rejects(
    loadLegacyPriorArt(async () => {
      throw new Error("Prior art unavailable");
    }, "o/r"),
    /unavailable/
  );
});

test("known skipped safe-output runs did not attempt a PR; unknown/running invocations block", async () => {
  const skipped = api({ runs: [{ id: 99, status: "completed", conclusion: "failure" }], jobs: [{ name: "safe_outputs", conclusion: "skipped" }] });
  await loadHistory(skipped.request, "o/r", "100");
  const running = api({ runs: [{ id: 99, status: "in_progress" }] });
  await assert.rejects(loadHistory(running.request, "o/r", "100"), /unresolved/);
});

test("simultaneous invocations and repeated executions are suppressed by actual gate policy", async () => {
  const client = api({ runs: [{ id: 99, status: "in_progress" }] });
  const results = await Promise.allSettled([loadHistory(client.request, "o/r", "100"), loadHistory(client.request, "o/r", "101")]);
  assert.ok(results.every(result => result.status === "rejected"));
  assert.throws(() => gate(batch(), { runAttempt: "2" }), /provenance mismatch|Repeated workflow/);
  const repeated = batch({ ...copy(candidate), run_attempt: "2" });
  assert.throws(() => gate(repeated, { runAttempt: "2" }), /Repeated workflow/);
});

test("production gate orchestration rejects simultaneous/repeated attempts and previous reservations", async () => {
  const context = { ref: "refs/heads/main", payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  const pending = api({ runs: [{ id: 99, status: "in_progress" }] });
  const results = await Promise.allSettled([
    gateOutput(batch(), { request: pending.request, context, patch: adrPatch() }),
    gateOutput(batch({ ...copy(candidate), run_id: "101" }), { request: pending.request, context: { ...context, runId: 101 }, patch: adrPatch() }),
  ]);
  assert.ok(results.every(result => result.status === "rejected"));
  const previous = api({ records: [persisted(candidate)] });
  await assert.rejects(gateOutput(batch(), { request: previous.request, context, patch: adrPatch() }), /Known technical idea/);
  await assert.rejects(gateOutput(batch({ ...copy(candidate), run_attempt: "2" }), { request: api().request, context, runAttempt: "2", patch: adrPatch() }), /Repeated workflow/);
  await assert.rejects(gateOutput(batch(), { request: api().request, context: { ...context, ref: "refs/heads/untrusted" }, patch: adrPatch() }), /trusted default/);
});

test("run/SHA-bound trusted attestation replaces inaccessible Actions API calls, not the final gate", async () => {
  const context = { ref: "refs/heads/main", sha: "a".repeat(40), payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  const attestation = { version: 1, clear: true, run_id: "100", sha: context.sha, ledger_expected: false, attempt_run_ids: [], verified_through_run_id: "0" };
  const client = api();
  assert.equal((await gateOutput(batch(), { request: client.request, context, patch: adrPatch(), attestation })).id, candidate.id);
  assert.ok(client.calls.every(call => !call.route.includes("/actions/")));
  for (const invalid of [
    { ...attestation, clear: false },
    { ...attestation, run_id: "99" },
    { ...attestation, sha: "b".repeat(40) },
  ]) {
    await assert.rejects(gateOutput(batch(), { request: client.request, context, patch: adrPatch(), attestation: invalid }), /attestation/);
  }
  assert.equal(await gateOutput({ items: [{ type: "report_incomplete", reason: "Required research unavailable." }] }, { request: client.request, context, attestation: { ...attestation, clear: false } }), null);
});

test("trusted gate failure replacement is native report_incomplete with no mutations or clean noop", () => {
  const output = incompleteOutput();
  assert.equal(output.items.length, 1);
  assert.equal(output.items[0].type, "report_incomplete");
  assert.equal(gate(output), null);
  assert.match(output.items[0].reason, /No ADR publication was attempted/);
});

test("missing/truncated/corrupt immutable state and API failures are not clean noops", async () => {
  const successful = api({
    runs: [{ id: 99, status: "completed", conclusion: "success" }],
    jobs: [
      { name: "safe_outputs", conclusion: "success" },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
    issues: [{ title: "[Harness ADR] prior proposal", body: batch().items[1].body }],
  });
  await assert.rejects(loadHistory(successful.request, "o/r", "100"), /ledger missing/);
  const truncated = api({ branch: true, truncated: true });
  await assert.rejects(loadHistory(truncated.request, "o/r", "100"), /Incomplete ledger/);
  const corrupt = persisted();
  corrupt.payload.value.reason = "Modified without recomputing immutable hash";
  await assert.rejects(loadHistory(api({ records: [corrupt] }).request, "o/r", "100"), /invalid record/);
  await assert.rejects(
    loadHistory(
      async () => {
        throw new Error("API unavailable");
      },
      "o/r",
      "100"
    ),
    /API unavailable/
  );
});

test("bounded complete pagination fails closed rather than accepting partial history", async () => {
  await assert.rejects(
    pages(async () => ({ data: { rows: [], total_count: 1 } }), "route", "rows"),
    /Incomplete/
  );
  await assert.rejects(
    pages(async () => ({ data: Array(100).fill({}) }), "route", null, {}, 2),
    /budget exhausted/
  );
});

test("indexed candidate-specific all-state history is complete without enumerating the global issue corpus", async () => {
  const calls = [];
  const request = async (route, params) => {
    assert.equal(route, "GET /search/issues", "No global repository issue enumeration");
    calls.push(params.q);
    return { data: { items: [{ number: 42, title: "Closed historical idea", state: "closed", body: "Inspect the specific technical mechanism." }], total_count: 1, incomplete_results: false } };
  };
  const result = await indexedHistory(request, "o/r", candidate);
  assert.equal(result.length, 1);
  assert.ok(calls.some(query => query.includes('"harness-proposal"')));
  assert.ok(calls.some(query => query.includes('"[Harness ADR]"')));
  assert.ok(calls.every(query => !/\bis:(?:open|closed)\b/.test(query)));
  assert.ok(mechanismQueries(candidate, "o/r").length <= 12);
  await assert.rejects(
    indexedHistory(async () => ({ data: { items: [], total_count: 0, incomplete_results: true } }), "o/r", candidate),
    /search is incomplete/
  );
});

test("long indexed history records are safely token-scanned instead of rejected by technical-field limits", () => {
  assert.doesNotThrow(() => checkUnstructuredHistory(candidate, [{ title: "Unrelated history", body: "long unrelated background ".repeat(3000) }]));
  assert.throws(() => checkUnstructuredHistory(candidate, [{ title: "Session graders", body: `${"long unrelated background ".repeat(3000)} ${proposal.mechanism}` }]), /semantic duplicate/);
});

test("failed native PR with missing durable attempt still fails closed", async () => {
  const client = api({
    runs: [{ id: 99, status: "completed", conclusion: "failure" }],
    jobs: [
      { name: "safe_outputs", conclusion: "failure" },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
    branch: true,
  });
  await assert.rejects(loadHistory(client.request, "o/r", "100"), /no provably durable proposal attempt/);
});

test("older immutable proposal versions remain exclusion history without rewriting provenance", async () => {
  const legacy = copy(candidate);
  legacy.version = 1;
  legacy.status = "reserved";
  for (const field of ["implementation_hours", "score_reasons", "security", "tradeoffs", "validation"]) delete legacy[field];
  delete legacy.scores.cost;
  delete legacy.scores.feasibility;
  for (const source of legacy.sources) {
    delete source.published;
    delete source.publication_note;
  }
  const { history } = await loadHistory(api({ records: [persisted(legacy)] }).request, "o/r", "100");
  assert.equal(history[0].version, 1);
  assert.equal(history[0].status, "reserved");
  assert.throws(() => gate(batch(), { history }), /Known technical idea/);
});

test("Actions history uses the executable lock workflow exact route, never the Markdown source", async () => {
  const client = api();
  await checkPriorRuns(client.request, "o/r", "100");
  const calls = client.calls.filter(call => call.route.includes("/actions/workflows/"));
  assert.equal(calls.length, 1);
  assert.equal(calls[0].route, "GET /repos/o/r/actions/workflows/daily-harness-researcher.lock.yml/runs");
  assert.ok(!calls[0].route.includes(".md/"));
});

test("native collector validation, trusted gate, checkpoint stamp and publication sanitizer preserve visible metadata", () => {
  const previous = { config: process.env.GH_AW_VALIDATION_CONFIG, domains: process.env.GH_AW_ALLOWED_DOMAINS, core: global.core, context: global.context };
  try {
    global.core = { info() {}, warning() {}, error() {}, debug() {} };
    global.context = { repo: { owner: "o", repo: "r" } };
    process.env.GH_AW_ALLOWED_DOMAINS = "github.com,arxiv.org";
    process.env.GH_AW_VALIDATION_CONFIG = JSON.stringify({
      create_pull_request: { fields: { title: { type: "string", required: true, sanitize: true, maxLength: 256 }, body: { type: "string", required: true, sanitize: true, maxLength: 65000 }, branch: { type: "string", required: true } } },
    });
    resetValidationConfigCache();
    const record = copy(candidate);
    record.reason += " Backtick data `x` and ``` must survive without closing a fence; <!-- harness-proposal:untrusted-string --> remains inert JSON data.";
    const output = batch(record);
    assert.ok(output.items[1].body.includes("\\u0060"));
    const collected = validateItem(output.items[1], "create_pull_request", 1, { deferMentions: true, allowedAliases: [], maxMentions: 50 });
    assert.equal(collected.isValid, true, collected.error);
    output.items[1] = collected.normalizedItem;
    assert.deepEqual(metadata(output.items[1].body), record);
    assert.equal(validateBatch(output, { patch: adrPatch(record), runId: "100" }).id, record.id);
    stampCheckpoint(output, "99");
    const published = sanitizeContent(output.items[1].body, { allowedAliases: [], maxMentions: 50 });
    assert.deepEqual(metadata(published), output.items[0].value);
    assert.equal(metadata(published).reason, record.reason);
    const legacyBody = `Research proposal with source-grounded context.\n<!-- harness-proposal:${JSON.stringify(candidate)} -->`;
    assert.deepEqual(metadata(legacyBody), candidate, "Legacy history remains readable");
    const old = validateItem({ ...batch().items[1], body: legacyBody }, "create_pull_request", 1, { deferMentions: true });
    assert.equal(old.isValid, true);
    assert.equal(metadata(old.normalizedItem.body), null, "Regression verifies why HTML comments cannot carry current metadata");
  } finally {
    for (const [key, value] of [
      ["GH_AW_VALIDATION_CONFIG", previous.config],
      ["GH_AW_ALLOWED_DOMAINS", previous.domains],
    ]) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
    global.core = previous.core;
    global.context = previous.context;
    resetValidationConfigCache();
  }
});

test("missing ledger after successful native processing cannot erase rejected decisions", async () => {
  const client = api({
    runs: [{ id: 99, status: "completed", conclusion: "success" }],
    jobs: [
      { name: "safe_outputs", conclusion: "success", steps: [{ name: "Process Safe Outputs", conclusion: "success" }] },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
  });
  await assert.rejects(checkPriorRuns(client.request, "o/r", "100"), /ledger missing/);
});

test("zero-write incomplete recovery skips native processing and permits initialization", async () => {
  const client = api({
    runs: [{ id: 99, status: "completed", conclusion: "failure" }],
    jobs: [
      { name: "safe_outputs", conclusion: "failure", steps: [{ name: "Process Safe Outputs", conclusion: "skipped" }] },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
  });
  const runs = await checkPriorRuns(client.request, "o/r", "100");
  assert.equal(runs.ledger_expected, false, "Job/workflow success does not prove nonempty persistence");
  const context = { ref: "refs/heads/main", payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  assert.equal((await gateOutput(batch(), { request: client.request, context, patch: adrPatch() })).id, candidate.id);
});

test("empty or short uncheckpointed Actions responses must reconcile total_count", async () => {
  const client = api();
  for (const rows of [[], [{ id: 99, status: "completed" }]]) {
    const request = (route, params) => (route.endsWith("/runs") ? Promise.resolve({ data: { workflow_runs: rows, total_count: rows.length + 1 } }) : client.request(route, params));
    await assert.rejects(checkPriorRuns(request, "o/r", "100"), /Incomplete uncheckpointed/);
  }
});

test("durable trusted checkpoint bounds run-job scans despite a large complete workflow history", async () => {
  const known = { ...copy(candidate), run_id: "2000", verified_through_run_id: "1999" };
  const client = api({
    records: [persisted(known)],
    runs: Array.from({ length: 100 }, (_, index) => ({ id: 2002 - index, status: "completed", conclusion: "success" })),
    jobs: [
      { name: "safe_outputs", conclusion: "success" },
      { name: "push_ledger_changes", conclusion: "success" },
    ],
  });
  const runs = await checkPriorRuns(client.request, "o/r", "2002");
  assert.equal(runs.length, 2, "Only runs beyond the durable verified watermark require job evidence");
  assert.equal(runs.verified_through_run_id, "2001");
  assert.equal(client.calls.filter(call => call.route.endsWith("/jobs")).length, 2);
  assert.equal(client.calls.filter(call => call.route.endsWith("/runs")).length, 1);
});

test("trusted checkpoint stamping preserves ledger/PR agreement and cannot be supplied by the agent", () => {
  const output = stampCheckpoint(batch(), "99");
  const record = output.items[0].value;
  assert.equal(record.verified_through_run_id, "99");
  assert.deepEqual(metadata(output.items[1].body), record);
  assert.equal(validateRecord(record, { historical: true }).id, candidate.id);
  assert.throws(() => gate(output), /Only the trusted gate/);
  assert.throws(() => validateRecord({ ...record, verified_through_run_id: "100" }, { historical: true }), /verified prior-run/);
});

test("a failed gate before native Process Safe Outputs does not permanently block a different day", async () => {
  const client = api({
    runs: [{ id: 99, status: "completed", conclusion: "failure" }],
    jobs: [
      { name: "safe_outputs", conclusion: "failure", steps: [{ name: "Process Safe Outputs", conclusion: "skipped" }] },
      { name: "push_ledger_changes", conclusion: "failure" },
    ],
  });
  const runs = await checkPriorRuns(client.request, "o/r", "100");
  assert.equal(runs[0].ledger_attempt_required, undefined);
  assert.equal(runs.verified_through_run_id, "99");
  const context = { ref: "refs/heads/main", payload: { repository: { default_branch: "main" } }, repo: { owner: "o", repo: "r" }, runId: 100 };
  assert.equal((await gateOutput(batch(), { request: client.request, context, patch: adrPatch() })).status, "proposal_attempt");
});

test("native compaction preserves hundreds of daily checkpoints and proposal provenance in one bounded shard", async () => {
  const envelopes = Array.from({ length: 500 }, (_, index) => {
    const envelope = {
      version: 1,
      id: `ldg-00000000-0000-4000-8000-${(index + 1).toString(16).padStart(12, "0")}`,
      type: "append",
      timestamp: "2026-11-08T09:00:00.000Z",
      parents: [],
      payload: { operation: "append", value: { version: 1, kind: "scan", status: "complete", run_id: String(200 + index), run_attempt: "1", analyzed_at: "2026-11-08T09:00:00Z", verified_through_run_id: String(199 + index) } },
    };
    return { ...envelope, sha: sha256(envelope) };
  });
  envelopes.push(persisted({ ...copy(candidate), status: "rejected" }));
  const compacted = buildSegment(envelopes, { maxSegmentBytes: 1024 * 1024, metadata: { compaction: "gh-aw/ledger-compaction-plan/v1" } });
  assert.ok(compacted.content.length > 102400, "Compacted data must exercise the configured larger native segment bound");
  const client = api({ records: compacted.records });
  const request = async (route, params) => {
    if (route.includes("/git/trees/"))
      return {
        data: {
          truncated: false,
          tree: [
            { path: `ledger/shards/${compacted.id}.jsonl`, mode: "100644", type: "blob", size: compacted.content.length, sha: "blob" },
            { path: "ledger/compaction/state.json", mode: "100644", type: "blob", size: 100, sha: "maintenance-state" },
            { path: `ledger/coverage/${"a".repeat(64)}.jsonl`, mode: "100644", type: "blob", size: 100, sha: "coverage" },
          ],
        },
      };
    return client.request(route, params);
  };
  const values = await loadLedgerRecords(request, "o/r");
  assert.equal(values.length, 501);
  assert.deepEqual(values.find(value => value.status === "rejected").sources, candidate.sources);
  assert.equal(values.filter(value => value.kind === "scan").length, 500);
  const runs = await checkPriorRuns(request, "o/r", "1000");
  assert.equal(runs.verified_through_run_id, "698");
  assert.ok(client.calls.filter(call => call.route.includes("/git/blobs/")).length <= 2, "Daily run count does not become per-run-shard API calls after native compaction");
  assert.ok(client.calls.every(call => !/maintenance-state|coverage$/.test(call.route)));
});

test("unstructured historical semantic descriptions conservatively block equivalent ideas", () => {
  assert.throws(() => checkUnstructuredHistory(candidate, [{ title: "Graders", body: "Use cached deterministic evaluator results for session scoring." }]), /Potential semantic duplicate/);
});

test("compiled workflow runs the trusted gate before native writes with full workflow serialization", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", ".github/workflows/daily-harness-researcher.md"), "utf8");
  assert.match(source, /concurrency:\n  group: daily-harness-researcher\n  cancel-in-progress: false/);
  assert.match(source, /GH_AW_AGENT_OUTPUT: '\$\{\{ steps\.setup-agent-output-env\.outputs\.GH_AW_AGENT_OUTPUT \}\}'/);
  assert.match(source, /ref: \$\{\{ github\.sha \}\}/);
  assert.match(source, /await main\(\{ github, context, root \}\)/);
  assert.match(source, /GH_AW_HISTORY_ATTESTATION: '\$\{\{ needs\.harness_history\.outputs\.proof \}\}'/);
  assert.match(source, /fallback-as-issue: false/);
  assert.match(source, /preserve-branch-name: false/);
  assert.match(source, /recreate-ref: false/);
  assert.match(source, /max-patch-files: 1/);
  assert.match(source, /compaction:\n        schedule: daily\n        min-segments: 16\n        max-segments: 32/);
  assert.match(source, /max-segment-kb: 1024/);
  const lockPath = path.join(__dirname, "..", ".github/workflows/daily-harness-researcher.lock.yml");
  assert.ok(fs.existsSync(lockPath), "Workflow must be compiled");
  const lock = fs.readFileSync(lockPath, "utf8");
  const job = lock.split("\n  safe_outputs:")[1].split(/\n  \w+:/)[0];
  assert.ok(job.indexOf("id: setup-agent-output-env") < job.indexOf("Validate deterministic harness proposal"));
  assert.ok(job.indexOf("Validate deterministic harness proposal") < job.indexOf("Process Safe Outputs"));
  assert.match(lock, /\nconcurrency:\n  cancel-in-progress: false\n  group: "?daily-harness-researcher"?/);
  assert.match(lock, /push_ledger_changes:[\s\S]*?needs:[\s\S]*?- safe_outputs/);
  const historyJob = lock.split("\n  harness_history:")[1].split(/\n  \w+:/)[0];
  assert.match(historyJob, /actions: read/);
  assert.match(historyJob, /proof: "?\$\{\{ steps\.history\.outputs\.proof \}\}/);
  assert.match(job, /- harness_history/);
});
