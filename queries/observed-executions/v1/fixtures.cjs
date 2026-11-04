"use strict";

// Synthetic native Azure rows, not a capture of Azure's OTLP ingestion mapping.
const trace = "11111111111111111111111111111111";
const root = "example/repo/.github/workflows/root.yml@refs/heads/main";
const child = "example/repo/.github/workflows/child.yml@refs/heads/main";
const start = "2026-10-01T00:00:00.000Z";
const end = "2026-10-02T00:00:00.000Z";
const id = number => number.toString(16).padStart(16, "0");

function span(number, job, phase, { run = "100", attempt = 2, workflow = root, attributes = {}, ...overrides } = {}) {
  return {
    TraceId: trace,
    SpanId: id(number),
    ParentSpanId: "",
    ScopeName: "gh-aw",
    Name: `gh-aw.${job}.${phase}`,
    TimeGenerated: start,
    EndTime: new Date(Date.parse(start) + (overrides.DurationMs ?? 60000)).toISOString(),
    DurationMs: 60000,
    StatusCode: "Ok",
    StatusMessage: "",
    ItemCount: 1,
    DroppedAttributesCount: 0,
    DroppedEventsCount: 0,
    Attributes: {
      "gh-aw.repository": "example/repo",
      "gh-aw.run.id": run,
      "gh-aw.run.attempt": attempt,
      "gh-aw.hop.id": `${run}-${attempt}`,
      "gh-aw.job.name": job,
      "gh-aw.engine.id": "copilot",
      "gen_ai.request.model": "model-a",
      ...attributes,
    },
    ResourceAttributes: {
      "service.name": "gh-aw",
      "github.repository": "example/repo",
      "github.run_id": run,
      "github.run_attempt": attempt,
      "github.workflow_ref": workflow,
      "github.job": job,
    },
    ...overrides,
  };
}

const spans = [
  span(1, "agent", "conclusion", { attempt: 1 }),
  span(2, "agent", "agent", {
    attempt: 1,
    ParentSpanId: id(1),
    DurationMs: 40000,
    StatusCode: "Error",
    StatusMessage: "agent timeout",
    attributes: {
      "gh-aw.aic": 4,
      "gen_ai.usage.input_tokens": 100,
      "gen_ai.usage.output_tokens": 20,
      "gh-aw.run.status": "timeout",
      "gh-aw.failure.categories": ["timeout"],
    },
  }),
  span(3, "agent", "setup", { attempt: 1, DurationMs: 1000 }),
  span(5, "conclusion", "conclusion", { attempt: 1 }),
  span(11, "agent", "setup", { DurationMs: 2000 }),
  span(12, "agent", "conclusion", { attributes: { "gh-aw.aic": 999, "gen_ai.usage.input_tokens": 999 } }),
  span(13, "agent", "agent", {
    ParentSpanId: id(12),
    DurationMs: 30000,
    attributes: {
      "gh-aw.aic": 0,
      "gen_ai.usage.input_tokens": 0,
      "gen_ai.usage.output_tokens": 0,
      "gen_ai.usage.cache_read.input_tokens": 0,
      "gen_ai.usage.cache_creation.input_tokens": 0,
      "gh-aw.model.requested": "auto",
      "gen_ai.response.model": "model-b",
      "gh-aw.model.effort": "high",
      "gh-aw.model_routing.status": "selected",
      "gh-aw.model_routing.mode": "auto",
      "gh-aw.model_routing.router_version": "fixture-v1",
      "gh-aw.model_routing.objective": "cost",
      "gh-aw.model_routing.task_type": "code",
      "gh-aw.model_routing.scope": "file",
      "gh-aw.model_routing.complexity": "simple",
      "gh-aw.model_routing.degraded": false,
      "gh-aw.model_routing.classifier_aic": 0.25,
      "gh-aw.model_routing.deviated_requests": 1,
    },
  }),
  span(14, "detection", "conclusion", {
    attributes: { "gh-aw.aic": 1.5, "gen_ai.usage.input_tokens": 30, "gen_ai.usage.total_tokens": 900 },
  }),
  span(15, "copilot", "conclusion", {
    attributes: { "gh-aw.aic": 2, "gen_ai.usage.input_tokens": 5, "gen_ai.usage.output_tokens": 7 },
  }),
  span(16, "safe_outputs", "conclusion", {
    attributes: {
      "gh-aw.aic": 999,
      "gen_ai.usage.input_tokens": 999,
      "gh-aw.otlp.export_errors": 1,
      "gh-aw.otlp.export_error_details": "fixture rejected_spans=2",
    },
  }),
  span(17, "graders", "conclusion"),
  span(18, "conclusion", "setup"),
  span(19, "conclusion", "conclusion"),
  span(21, "agent", "conclusion", {
    workflow: child,
    attributes: { "gh-aw.aic": 3, "gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2 },
  }),
  span(22, "agent", "setup", { workflow: child, DurationMs: 3000 }),
  span(23, "conclusion", "setup", { workflow: child }),
  span(24, "conclusion", "conclusion", { workflow: child }),
  span(31, "agent", "conclusion", { run: "200", attempt: 1 }),
  span(32, "conclusion", "setup", { run: "200", attempt: 1 }),
  span(33, "conclusion", "conclusion", { run: "200", attempt: 1 }),
  span(41, "agent", "conclusion", {
    run: "300",
    attempt: null,
    EndTime: null,
    attributes: { "gh-aw.aic": 9, "gh-aw.run.status": "failure" },
  }),
  span(51, "agent", "conclusion", { run: "400", attempt: 1, ItemCount: 2, attributes: { "gh-aw.aic": 50 } }),
  span(61, "agent", "conclusion", { run: "500", attempt: 1, attributes: { "gh-aw.aic": 6 } }),
  span(70, "agent", "conclusion", { TraceId: "" }),
];
spans.push(structuredClone(spans[1]), structuredClone(spans[2]), structuredClone(spans[6]));
spans.push(span(61, "agent", "conclusion", { run: "500", attempt: 1, attributes: { "gh-aw.aic": 7 } }));

function grader(parent, graderId, source, status, value, passed) {
  const owner = spans.find(row => row.SpanId === id(parent));
  return {
    TraceId: trace,
    SpanId: id(parent),
    TimeGenerated: start,
    Name: "grader.result",
    ResourceAttributes: owner?.ResourceAttributes || {},
    Attributes: {
      "gh-aw.grader.id": graderId,
      "gh-aw.grader.source": source,
      "gh-aw.grader.status": status,
      ...(value === null ? {} : { "gh-aw.grader.value": value }),
      ...(passed === null ? {} : { "gh-aw.grader.passed": passed }),
      "gh-aw.grader.unit": "ratio",
    },
  };
}
const events = [
  grader(5, "quality", "builtin", "fail", 0, false),
  grader(17, "quality", "inline", "fail", 0, false),
  grader(17, "outcome", "operational-value", "pass", 1, true),
  grader(19, "coverage", "builtin", "unavailable", null, null),
  grader(24, "quality", "builtin", "pass", 1, true),
  grader(12, "quality", "builtin", "pass", 999, true), // Wrong job owner.
  grader(17, "coverage", "builtin", "pass", 999, true), // Downloaded built-in copy.
  grader(99, "orphan", "builtin", "fail", null, false),
];
events.push(structuredClone(events[1]));

const profile = {
  "service.name": "gh-aw",
  "gh-aw.session.compact.version": 1,
  "gh-aw.session.included_events": 2,
  "gh-aw.session.omitted_events": 4,
  "gh-aw.session.oversized_lines": 1,
  "gh-aw.session.scan_truncated": true,
  "gh-aw.session.truncated": true,
};
function log(parent, body, resource = profile) {
  return {
    TraceId: trace,
    SpanId: id(parent),
    ScopeName: "gh-aw.session",
    TimeGenerated: "2026-10-01T00:02:00.000Z", // Observation/export time on this baseline.
    ResourceAttributes: resource,
    Body: typeof body === "string" ? body : JSON.stringify(body),
  };
}
const logs = [
  log(18, { type: "execution.result", timestamp: "2026-10-01T00:00:45.000Z", data: { outcome: "success" } }),
  log(18, { type: "grader.result", data: { run: { passed: 1, failed: 1 } } }),
  log(32, { type: "session.result", timestamp: 1790812845000, data: {} }, {}),
  log(99, "not JSON", {}),
];
logs.push(structuredClone(logs[1]));

module.exports = { tables: { OTelSpans: spans, OTelEvents: events, OTelLogs: logs }, trace, root, child, start, end, id, span, grader, log };
