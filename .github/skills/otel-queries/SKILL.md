---
name: otel-queries
description: Analyze gh-aw OpenTelemetry traces from JSONL mirrors or OTLP backends.
---

# OTel Queries

Use this skill to inspect gh-aw OpenTelemetry/OTLP data and answer telemetry questions without re-deriving trace fields, backend filters, and diagnostics.

## When To Use

Use this skill for requests such as:

- analyze OTEL or OTLP data
- inspect traces in Grafana, Tempo, Sentry, Honeycomb, or Datadog
- explain why a workflow or agent run is slow or failing
- compare run phases, error clusters, or span attributes
- identify the best observability or performance improvement
- close the loop from telemetry into code or workflow changes

Do not use this skill for instrumentation-only tasks that do not require reading telemetry. For pure emit-side work, start with the existing OTLP code and docs.

## Primary Goal

Reduce a broad telemetry task to one tight loop:

1. Find the cheapest trustworthy telemetry source.
2. Run a small fixed set of common queries.
3. Confirm one concrete bottleneck, missing attribute, or broken correlation path.
4. Answer the user's telemetry question directly.
5. Recommend or implement a follow-on optimization only when the evidence supports it.

## Telemetry Sources In Priority Order

Prefer sources in this order unless the user says otherwise:

1. Local artifacts or mirrors already in the workspace.
2. `/tmp/gh-aw/otel.jsonl` for gh-aw spans.
3. Live OTLP backend data through an MCP server or supported tool — Copilot CLI spans are exported directly to the configured OTLP backend (no local file mirror) and must be queried there, filtered by the `github.run_id` resource attribute.
4. Static code inspection only, when no telemetry is available.

Use the cheapest source that can disconfirm the current hypothesis.

## Standard Analysis Loop

Always answer these questions in order before expanding scope.

### 1. Do spans exist for the run or workflow at all?

Look for:

- `traceId`
- span `name`
- `service.name`
- `github.repository`
- `github.run_id`

If these are missing, the problem is likely export, filtering, or trace propagation rather than optimization.

### 2. Is trace continuity intact?

Check whether spans that should belong together share the same:

- trace ID
- parent span lineage
- run ID
- workflow reference

If setup, agent, and conclusion spans are not connected, fix correlation before interpreting latency.

### 3. Which phase is actually slow or failing?

Bucket spans into phases:

- setup
- agent execution
- tool or safe-output calls
- conclusion

Prefer wall-clock duration and count by span name prefix before reading code.

### 4. Do the spans contain enough attributes to explain the slowdown or failure?

Minimum diagnostic attributes to verify:

- `service.version`
- `deployment.environment`
- `github.repository`
- `github.run_id`
- `github.event_name`
- `github.workflow_ref`
- `gh-aw.workflow`
- `gh-aw.engine`
- conclusion or failure attributes

If the slow or failing span lacks the attribute needed to group, filter, or explain it, the right next step may be an instrumentation change rather than a runtime change.

### 5. Is the problem systemic or isolated?

Check whether the pattern repeats across:

- multiple runs of the same workflow
- multiple jobs in the same trace
- one engine only
- one event type only
- one environment only

Do not propose broad architectural changes for a single outlier trace.

## Common Queries

Use these backend-agnostic query shapes first. Translate them into the native query language or MCP tool calls for the active backend.

### Query 1: Recent gh-aw spans

Filter for the last 24 hours and `service.name = gh-aw`.

Return:

- timestamp
- trace ID
- span name
- duration
- status
- `github.run_id`
- `github.workflow_ref`

### Query 2: Slowest spans by name

Group by span name and sort by:

- p95 duration
- max duration
- count

Use this to find whether the bottleneck is setup, agent, tool, or conclusion work.

### Query 3: Errors by span name

Filter for error status and group by:

- span name
- status message
- workflow ref
- engine

Use this to separate exporter failures from workflow logic failures.

### Query 4: Missing core attributes

Sample recent spans and explicitly record whether each span includes:

- `service.version`
- `github.repository`
- `github.run_id`
- `github.event_name`
- `deployment.environment`

If a backend supports `has` or `exists` filters, use them. Otherwise inspect a small sample manually.

### Query 5: Trace integrity for one failing run

Pick one trace ID and inspect the full trace. Record:

- root span name
- child spans present
- missing expected spans
- parent-child continuity gaps

### Query 6: Repeated cost or latency hotspot

For agent-heavy traces, group by:

- engine
- workflow
- job
- tool span name

Then compare count, total duration, and p95 duration.

## Local JSONL Recipes

When telemetry is available as JSONL, prefer shell plus `jq` over broad file reading.

### Recent spans

```bash
jq -c '.resourceSpans[]?.scopeSpans[]?.spans[]? | {traceId, name, startTimeUnixNano, endTimeUnixNano, status, attributes}' /tmp/gh-aw/otel.jsonl
```

### Filter by span name prefix

```bash
jq -c '.resourceSpans[]?.scopeSpans[]?.spans[]? | select(.name | startswith("gh-aw."))' /tmp/gh-aw/otel.jsonl
```

### Extract one attribute by key

```bash
jq -r '.resourceSpans[]?.scopeSpans[]?.spans[]? as $span | $span.attributes[]? | select(.key == "github.run_id") | .value.stringValue' /tmp/gh-aw/otel.jsonl
```

### Find spans missing an attribute

```bash
jq -c '.resourceSpans[]?.scopeSpans[]?.spans[]? | select(any(.attributes[]?; .key == "github.run_id") | not) | {traceId, name}' /tmp/gh-aw/otel.jsonl
```

### Inspect one trace

```bash
jq -c '.resourceSpans[]?.scopeSpans[]?.spans[]? | select(.traceId == $traceId)' --arg traceId "TRACE_ID_HERE" /tmp/gh-aw/otel.jsonl
```

## Backend Translation Notes

Adapt the same six common queries to the active backend instead of inventing new analysis questions.

### Grafana or Tempo

- Start with datasource or trace search discovery.
- Prefer trace search scoped to `service.name="gh-aw"` and a short time window.
- Use trace detail views to validate parent-child continuity.
- Use derived metrics or span aggregations only after a sample trace confirms the field names.

### Sentry

- Search the spans dataset first.
- Fall back to transactions only if spans are unavailable.
- Use one full trace to validate attribute presence; do not infer from issue titles alone.

### Honeycomb or Datadog

- Start with dataset or service filters on `service.name`.
- Group by span name and error status.
- Sample raw spans to confirm exact attribute keys before building aggregate conclusions.

## Follow-On Decisions

After answering the telemetry question, choose the next step based on the evidence.

Prioritize in this order:

1. Broken trace continuity or missing spans.
2. Missing attributes that block filtering, correlation, or incident response.
3. High-frequency latency hotspot with a narrow owner.
4. High-severity error cluster with a narrow owner.
5. Dashboard or query ergonomics improvements.

Prefer the smallest change that unlocks the most operational clarity.

## Output Contract

When using this skill, produce findings in this shape:

1. Telemetry source used.
2. The question answered.
3. One confirmed bottleneck, observability gap, or healthy result.
4. The exact evidence: span name, trace ID or run ID, attribute presence or absence, and duration or error pattern.
5. The smallest code, workflow, or instrumentation change to make, if one is needed.
6. The validation step that would prove the result or follow-on change.

## gh-aw Specific Pointers

Start with these files when telemetry indicates an instrumentation or correlation problem:

- `actions/setup/js/send_otlp_span.cjs`
- `actions/setup/js/action_setup_otlp.cjs`
- `actions/setup/js/action_conclusion_otlp.cjs`
- `actions/setup/js/otlp.cjs`
- `actions/setup/js/generate_observability_summary.cjs`
- `actions/setup/js/aw_context.cjs`
- `pkg/workflow/observability_otlp.go`
- `docs/src/content/docs/reference/open-telemetry-attributes.mdx`

## Anti-Patterns

Avoid these common mistakes:

- starting with full-code inspection before checking whether telemetry already proves the issue
- treating a single anomalous trace as a systemic problem
- proposing instrumentation changes without naming the missing attribute or broken correlation edge
- spending prompt budget on backend-specific browsing before confirming the standard six queries
- mixing exporter failures with business-logic failures

## Expected Result

After using this skill, the agent should be able to move from raw OTel data to a grounded answer without re-deriving the telemetry playbook.
