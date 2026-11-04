"use strict";

const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs");
const path = require("node:path");
const { queryPack, counter } = require("./reference.cjs");
const { tables, root, child, start, end, span, id, trace, grader, log } = require("./fixtures.cjs");
const { expected, identity, usageRow, compactRow } = require("./expected.cjs");
const options = { start, end };

for (const panel of Object.keys(expected)) {
  test(`reference specification: exact ${panel} table, all attempts`, () => {
    assert.deepEqual(queryPack(tables, options)[panel], expected[panel]);
  });
}

test("latest-observed chooses the run attempt once, preserving jobs and reusable workflow refs", () => {
  const actual = queryPack(tables, { ...options, mode: "latest-observed" });
  assert.deepEqual(actual.coverage, [
    {
      ...expected.coverage[0],
      ObservedSpans: 18,
      PhysicalCopies: 20,
      MissingCorrelation: 0,
      ObservedExecutions: 5,
      AttemptMode: "latest-observed",
    },
  ]);
  assert.deepEqual(actual.failures, [expected.failures[1]]);
  for (const panel of ["phases", "models-routing", "graders", "compact-quality"]) {
    assert.deepEqual(
      actual[panel],
      expected[panel].filter(row => (row.Attempt !== null && !(row.ExecutionRunId === "100" && row.Attempt === 1)) || row.ExecutionRunId === "")
    );
  }
  assert.deepEqual(actual.usage, [expected.usage[0], usageRow(root, "agent", 4, 2, [1, 0], [1, 0], [1, 0], [1, 0], [1, 0], [1, 0]), expected.usage[2], expected.usage[3]]);
});

test("exact attempt and run filters do not conflate child hops, detection, or engine owners", () => {
  const actual = queryPack(tables, { ...options, mode: "exact", attempt: 2, run: "100", repository: "example/repo" });
  assert.deepEqual(actual.coverage, [
    {
      ...expected.coverage[0],
      ObservedSpans: 13,
      PhysicalCopies: 14,
      MissingCorrelation: 0,
      ConflictingSpans: 0,
      SampledSpans: 0,
      ObservedExecutions: 2,
      AttemptMode: "exact",
      RequestedAttempt: 2,
    },
  ]);
  assert.deepEqual(actual.failures, [expected.failures[1]]);
  for (const panel of ["phases", "models-routing", "graders", "compact-quality"]) {
    const selected = expected[panel].filter(row => row.ExecutionRunId === "100" && row.Attempt === 2);
    // Unknown logs cannot be scoped; unknown events without repository attributes are excluded.
    if (panel === "compact-quality") selected.unshift(expected[panel][0]);
    assert.deepEqual(actual[panel], selected);
  }
  assert.deepEqual(actual.usage, [expected.usage[0], usageRow(root, "agent", 1, 0, [1, 0], [1, 0], [1, 0], [1, 0], [1, 0], [1, 0]), expected.usage[2], expected.usage[3]]);
});

test("physical mirror duplicates and input order never change logical results", () => {
  const reversed = Object.fromEntries(Object.entries(tables).map(([name, rows]) => [name, rows.toReversed()]));
  assert.deepEqual(queryPack(reversed, options), expected);
  const moreCopies = { ...tables, OTelSpans: [...tables.OTelSpans, structuredClone(tables.OTelSpans[6])] };
  const result = queryPack(moreCopies, options);
  assert.deepEqual(result, {
    ...expected,
    coverage: [{ ...expected.coverage[0], PhysicalCopies: 28, WindowPhysicalSpans: 29 }],
  });
});

test("missing dedicated usage never falls back to a copied conclusion counter", () => {
  const input = {
    OTelSpans: [span(81, "agent", "conclusion", { attributes: { "gh-aw.aic": 999, "gen_ai.usage.input_tokens": 999 } }), span(82, "agent", "agent", { ParentSpanId: id(81) })],
    OTelEvents: [],
    OTelLogs: [],
  };
  assert.deepEqual(queryPack(input, options).usage, [usageRow(root, "agent", 1, 0, [0, null], [0, null], [0, null], [0, null])]);
});

test("two same-named job instances remain separate owners; a missing workflow ref is unknown accounting", () => {
  const first = span(81, "agent", "conclusion", { attributes: { "gh-aw.aic": 2 } });
  const second = span(82, "agent", "conclusion", { attributes: { "gh-aw.aic": 3 } });
  const missing = span(83, "agent", "conclusion", { attributes: { "gh-aw.aic": 90 }, ResourceAttributes: {} });
  assert.deepEqual(queryPack({ OTelSpans: [first, second, missing], OTelEvents: [], OTelLogs: [] }, options).usage, [
    usageRow("", "agent", 1, 1, [0, null], [0, null], [0, null], [0, null]),
    usageRow(root, "agent", 2, 0, [2, 5], [0, null], [0, null], [0, null]),
  ]);
});

test("absent, negative, nonfinite, null, string and boolean counters are not measured zero", () => {
  for (const value of [undefined, null, "", "0", true, false, -1, NaN, Infinity]) assert.equal(counter(value), null);
  assert.equal(counter(0), 0);
  assert.equal(counter(1.5), 1.5);
  const zero = queryPack(tables, { ...options, mode: "exact", attempt: 2, run: "100" }).usage.find(row => row.WorkflowRef === root && row.Job === "agent");
  assert.equal(zero.ObservedAic, 0);
  assert.equal(zero.ObservedTotalTokens, 0);
  const absent = queryPack(tables, { ...options, run: "200" }).usage[0];
  assert.deepEqual(absent, usageRow(root, "agent", 1, 0, [0, null], [0, null], [0, null], [0, null]));
});

test("event clocks are not fabricated from span start, and grader conflicts are partial", () => {
  const first = grader(19, "quality", "builtin", "fail", 0, false);
  const conflicting = grader(19, "quality", "builtin", "fail", 1, false);
  const result = queryPack({ ...tables, OTelEvents: [first, conflicting] }, options).graders;
  assert.deepEqual(result, [
    {
      ...identity(19, "conclusion"),
      GraderId: "quality",
      Source: "builtin",
      Status: "fail",
      Value: 1,
      Unit: "ratio",
      Passed: false,
      Ownership: "owned",
      EventTime: null,
      ClockState: "unavailable-native-event-clock",
      ParentEndTime: "2026-10-01T00:01:00.000Z",
      EventCopies: 2,
      PartialEvidence: true,
    },
  ]);
});

test("a bounded compact projection is not complete evidence; optional future untimed metadata stays optional", () => {
  const resource = {
    "gh-aw.session.included_events": 1,
    "gh-aw.session.omitted_events": 0,
    "gh-aw.session.oversized_lines": 0,
    "gh-aw.session.scan_truncated": false,
    "gh-aw.session.truncated": false,
  };
  const first = log(18, { type: "usage.report", timestamp: "2026-10-01T00:00:01.000Z", data: {} }, resource);
  const result = queryPack({ OTelSpans: [span(18, "conclusion", "setup")], OTelEvents: [], OTelLogs: [first] }, options);
  assert.deepEqual(result["compact-quality"], [
    compactRow(identity(18, "conclusion"), {
      PhysicalLogRecords: 1,
      PhysicalUntimedRecords: 0,
      UnparseableBodies: 0,
      UnknownQualityRecords: 0,
      Included: 1,
      Omitted: 0,
      Oversized: 0,
      ScanTruncated: 0,
      Truncated: 0,
      Evidence: "compact-projection",
    }),
  ]);
  assert.deepEqual(result.usage, []);
  assert.deepEqual(result.graders, []);
  first.ResourceAttributes["gh-aw.session.untimed_events"] = 0;
  assert.equal(queryPack({ OTelSpans: [span(18, "conclusion", "setup")], OTelEvents: [], OTelLogs: [first] }, options)["compact-quality"][0].ExporterUntimed, 0);
});

test("logs opt-in is not required for span panels; absent logs are not zero activity", () => {
  const result = queryPack({ ...tables, OTelLogs: [] }, options);
  for (const panel of ["coverage", "failures", "phases", "usage", "models-routing", "graders"]) assert.deepEqual(result[panel], expected[panel]);
  assert.deepEqual(result["compact-quality"], [compactRow(identity(23, "conclusion", "100", 2, child), {}), compactRow(identity(18, "conclusion"), {}), compactRow(identity(32, "conclusion", "200", 1), {})]);
});

test("each compact bound independently establishes partial evidence, including uninspected source records", () => {
  const profile = {
    "gh-aw.session.included_events": 1,
    "gh-aw.session.omitted_events": 0,
    "gh-aw.session.oversized_lines": 0,
    "gh-aw.session.scan_truncated": false,
    "gh-aw.session.truncated": false,
  };
  const base = {
    PhysicalLogRecords: 1,
    PhysicalUntimedRecords: 0,
    UnparseableBodies: 0,
    UnknownQualityRecords: 0,
    Included: 1,
    Omitted: 0,
    Oversized: 0,
    ScanTruncated: 0,
    Truncated: 0,
    Evidence: "partial",
  };
  for (const [field, column, value] of [
    ["omitted_events", "Omitted", 1],
    ["oversized_lines", "Oversized", 1],
    ["scan_truncated", "ScanTruncated", true],
    ["truncated", "Truncated", true],
    ["untimed_events", "ExporterUntimed", 1],
  ]) {
    const record = log(18, { type: "execution.result", timestamp: "2026-10-01T00:00:01.000Z" }, { ...profile, [`gh-aw.session.${field}`]: value });
    const result = queryPack({ OTelSpans: [span(18, "conclusion", "setup")], OTelEvents: [], OTelLogs: [record] }, options);
    assert.deepEqual(result["compact-quality"], [compactRow(identity(18, "conclusion"), { ...base, [column]: Number(value) })], field);
  }
});

test("latest attempt selection is independent across repositories sharing a numeric run ID", () => {
  const other = span(81, "agent", "conclusion", {
    attempt: 9,
    attributes: { "gh-aw.repository": "another/repo", "gh-aw.aic": 2 },
  });
  const result = queryPack({ ...tables, OTelSpans: [...tables.OTelSpans, other] }, { ...options, mode: "latest-observed", repository: "example/repo", run: "100" });
  assert.deepEqual(result.usage, [expected.usage[0], usageRow(root, "agent", 1, 0, [1, 0], [1, 0], [1, 0], [1, 0], [1, 0], [1, 0]), expected.usage[2], expected.usage[3]]);
  assert.equal(result.coverage[0].ObservedExecutions, 2);
});

test("routing failures and classifier credits are attribution, not billable usage or guessed resolved models", () => {
  const row = span(81, "agent", "conclusion", {
    attributes: {
      "gen_ai.request.model": "",
      "gh-aw.model.requested": "auto",
      "gh-aw.model_routing.status": "failed",
      "gh-aw.model_routing.failure_code": "no_candidate",
      "gh-aw.model_routing.classifier_aic": 0,
    },
  });
  const result = queryPack({ OTelSpans: [row], OTelEvents: [], OTelLogs: [] }, options);
  assert.deepEqual(result.usage, [usageRow(root, "agent", 1, 0, [0, null], [0, null], [0, null], [0, null])]);
  assert.deepEqual(result["models-routing"], [
    {
      ...expected["models-routing"][0],
      ...identity(81, "agent"),
      RequestedModel: "auto",
      EffectiveRequestModel: "",
      RoutingStatus: "failed",
      FailureCode: "no_candidate",
      ClassifierAicDiagnosticOnly: 0,
    },
  ]);
});

test("dropped attributes make owner accounting unavailable and sampled grader events stay partial", () => {
  const parent = span(19, "conclusion", "conclusion");
  const owner = span(81, "agent", "conclusion", { DroppedAttributesCount: 1, attributes: { "gh-aw.aic": 2 } });
  const event = { ...grader(19, "coverage", "builtin", "pass", 1, true), ItemCount: 2, DroppedAttributesCount: 1 };
  const result = queryPack({ OTelSpans: [parent, owner], OTelEvents: [event], OTelLogs: [] }, options);
  assert.deepEqual(result.usage, [usageRow(root, "agent", 1, 1, [0, null], [0, null], [0, null], [0, null])]);
  assert.equal(result.coverage[0].DroppedAttributes, 1);
  assert.deepEqual(result.graders, [
    {
      ...expected.graders[3],
      Status: "pass",
      Value: 1,
      Passed: true,
      PartialEvidence: true,
    },
  ]);
});

test("window end is exclusive and no physical identity, invalid selection or no rows means no invented activity", () => {
  const empty = { OTelSpans: [], OTelEvents: [], OTelLogs: [] };
  const cases = [
    { tables: empty, options },
    { tables, options: { start: end, end: "2026-10-03T00:00:00.000Z" } },
    { tables, options: { ...options, mode: "typo" }, invalid: true, raw: 28 },
    { tables, options: { ...options, mode: "exact", attempt: 0 }, invalid: true, raw: 28 },
    { tables: { ...empty, OTelSpans: [span(1, "agent", "conclusion", { TimeGenerated: end })] }, options },
  ];
  for (const entry of cases) {
    const result = queryPack(entry.tables, entry.options);
    for (const panel of ["failures", "phases", "usage", "models-routing", "graders", "compact-quality"]) assert.deepEqual(result[panel], []);
    assert.equal(result.coverage[0].ObservedExecutions, 0);
    assert.equal(result.coverage[0].ObservedSpans, 0);
    assert.equal(result.coverage[0].SelectionValid, !entry.invalid);
    assert.equal(result.coverage[0].WindowPhysicalSpans, entry.raw || 0);
  }
  const malformed = span(1, "agent", "conclusion", { TraceId: "", SpanId: "" });
  const result = queryPack({ ...empty, OTelSpans: [malformed, malformed] }, options);
  assert.equal(result.coverage[0].WindowInvalidIdentity, 2);
  assert.deepEqual(result.usage, []);
});

test("static query/dashboard contracts (not execution or provider conformance)", () => {
  const dashboard = JSON.parse(fs.readFileSync(path.join(__dirname, "dashboard.json"), "utf8"));
  assert.equal(dashboard.title, "observed executions");
  assert.equal(dashboard.version, 1);
  assert.deepEqual(dashboard.panels.map(panel => panel.id).sort(), Object.keys(expected).sort());
  const common = fs.readFileSync(path.join(__dirname, "azure/common.kql"), "utf8");
  assert.match(common, /by TraceId, SpanId/);
  assert.match(common, /join kind=leftanti DedicatedParents on TraceId, SpanId/);
  assert.match(common, /by RepositoryName, ExecutionRunId/);
  assert.match(common, /Counter\(Attributes\["gh-aw.aic"\]\)/);
  for (const panel of dashboard.panels) {
    const query = fs.readFileSync(path.join(__dirname, panel.query), "utf8");
    assert.ok(query.trim().length > 0);
    if (panel.id !== "compact-quality") assert.doesNotMatch(common + query, /\bOTelLogs\b/);
    assert.doesNotMatch(query, /\bApp(?:Traces|Requests|Dependencies)\b|\bsavings\b.*=/i);
  }
  const eventQuery = fs.readFileSync(path.join(__dirname, "azure/graders.kql"), "utf8");
  assert.match(eventQuery, /EventTime = datetime\(null\)/);
  assert.match(eventQuery, /gh-aw\.conclusion\.conclusion/);
  assert.match(eventQuery, /Source in \("inline", "operational-value"\)/);
  const emitter = fs.readFileSync(path.join(__dirname, "../../../actions/setup/js/send_otlp_span.cjs"), "utf8");
  for (const attribute of ["gh-aw.grader.source", "gh-aw.grader.value", "gh-aw.aic", "gh-aw.run.attempt", "gen_ai.usage.input_tokens", "gh-aw.model_routing.status"]) assert.ok(emitter.includes(attribute), attribute);
  assert.match(emitter, /jobName === "conclusion"\) return readJSON/);
  assert.match(emitter, /result\?\.source === "inline" \|\| result\?\.source === "operational-value"/);
  const exporter = fs.readFileSync(path.join(__dirname, "../../../actions/setup/js/send_otlp_session_logs.cjs"), "utf8");
  for (const field of ["included_events", "omitted_events", "oversized_lines", "scan_truncated", "truncated"]) assert.ok(exporter.includes(`gh-aw.session.${field}`));
  assert.equal(trace.length, 32);
});
