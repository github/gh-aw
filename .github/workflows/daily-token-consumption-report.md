---
private: true
emoji: "📊"
description: Daily report of AI Credits (AIC) consumption across all agentic workflows using OTel telemetry from Sentry and Grafana
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  copilot-requests: write

sandbox:
  agent:
    id: awf
tracker-id: daily-token-consumption-report
engine:
  id: goose
model: copilot/claude-sonnet-5
strict: true
network:
  allowed:
    - defaults
    - github
    - node
tools:
  work-queue:
    worker: true
    require-assignment: true
  bash: true
safe-outputs:
  mentions: false
  allowed-github-references: []
timeout-minutes: 30
imports:
  - shared/daily-report-worker.md
  - shared/goose.md
  - shared/mcp/sentry.md
  - shared/mcp/grafana.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[token-consumption] "
      expires: 1d
  - shared/reporting.md

  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: token_consumption_analyzed
    question: Did the agent analyze AI Credit consumption across agentic workflows using available telemetry?
  - id: consumption_report_created
    question: Did the agent create a report with token consumption trends and actionable observations?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily AIC Consumption Report (Sentry + Grafana OTel)

You are an observability analyst. Generate a daily AI Credits (AIC) consumption report across all agentic workflows in this repository using OpenTelemetry telemetry in both Sentry and Grafana.

## Context

- Repository: `${{ github.repository }}`
- Run ID: `${{ github.run_id }}`
- Time Window: last 24 hours

## Mission

1. Query Sentry and Grafana telemetry for the last 24 hours.
2. Aggregate AIC usage by workflow when available.
3. Identify top AIC consumers and anomalous usage.
4. Call out backend-specific AIC reporting gaps and likely causes.
5. Publish a concise daily GitHub discussion report for the assigned completed UTC report date.

## Data Collection

### Step 1: Discover Sentry Context

1. Call `find_organizations` and select the org for this repository.
2. Call `find_projects` and select the project that corresponds to `${{ github.repository }}`.

### Step 2: Fetch Telemetry Events

Call `search_traces` for spans from the last 24 hours. Without an embedded LLM provider, use direct Sentry query syntax and explicit fields and sort:
- `organizationSlug`: the org discovered in Step 1
- `projectSlug`: the project discovered in Step 1
- Start with schema-valid span identity fields and inspect a representative raw gh-aw trace to discover custom fields. Do not restrict discovery to `span.op:ai*` or `span.op:gen_ai*`: gh-aw job spans carry cost, not individual model/tool spans.
- `fields`: explicitly include `timestamp` (required for `sort: -timestamp`), `id`, and `trace` for Step 4 mirror deduplication; also request the confirmed numeric `gh-aw.aic` field and canonical workflow/run fields (`gh-aw.workflow.name`, `gh-aw.run.id`, `github.run_id`, `github.workflow_ref`) using the backend's discovered field syntax. An `Unknown attribute` error proves only that the requested field is not queryable in this schema; it does not prove the producer omitted it. Record the exact rejected field and inspect raw spans before recommending instrumentation changes.
- `sort`: `-timestamp`
- Split the query into smaller time windows if results hit the tool's limit.

If Sentry returns no usable span records, record the gap and continue with Grafana/Tempo.

Treat "no usable records" as either:
- zero events returned after pagination, or
- events returned but none contain a finite non-negative numeric `gh-aw.aic` value (including explicit zero), or a legacy alias whose meaning and numeric type are confirmed in raw telemetry per Step 4.

### Step 3: Fetch Grafana/Tempo Telemetry

Use the Grafana MCP server in this workflow.

1. Call `list_datasources` and identify the tracing datasource used for Tempo. Note the datasource UID — pass it explicitly to all subsequent Tempo tool calls.
2. Call `tempo_get-attribute-names` (with the datasource UID from step 1) to confirm available attribute keys.
3. Query recent traces in the last 24 hours with `tempo_traceql-search` (datasource UID from step 1, `start: now-24h`, `end: now`), scoped to gh-aw telemetry using `{.service.name =~ "gh-aw.*"}`. If attribute values returned by `tempo_get-attribute-values` reveal the actual `service.name`, use that value instead.
4. If needed, call `tempo_get-trace` (datasource UID from step 1) on representative traces to verify whether AIC fields are present and numeric on spans.
5. Record any query constraints or backend limitations (missing attributes, string-only values, fields not indexed, or inability to aggregate numerically).

Treat Grafana data as "no usable AIC records" when traces exist but none expose a finite non-negative numeric `gh-aw.aic` value (including explicit zero), or a legacy alias whose meaning and numeric type are confirmed in raw telemetry per Step 4, in queryable numeric form.

If `list_datasources` returns no Tempo datasource, or if `tempo_get-attribute-names` or `tempo_traceql-search` returns an error, treat this as "Grafana data unavailable" and record it as an observability gap in the Grafana AIC Findings section. Do not abort the workflow — continue to Step 4 with Sentry data only. Similarly, if the Grafana MCP server itself is unreachable (bad credentials, network error, or unconfigured secrets), record the failure reason and continue with Sentry-only data.

### Step 4: Extract Workflow + AIC Fields

For each Sentry event/span and Grafana span, derive:

- **Workflow name** using first non-empty of confirmed fields:
  - `gh-aw.workflow.name` (canonical span/resource attribute)
  - `gen_ai.workflow.name`
  - `github.workflow`
  - `github.workflow_ref`
  - `workflow.name`
  - `gh_aw.workflow`
  - fallback: `"unknown-workflow"`
- **Run ID** using:
  - `github.run_id`
  - `gh-aw.run.id`
  - `gh_aw.run.id`
  - `gh_aw.run_id`
- **AIC value**: use a finite non-negative numeric `gh-aw.aic`, including an explicit zero. Only use legacy aliases if raw telemetry confirms their meaning and numeric type; never substitute USD cost or token counts.
- On **both backends**, absent, invalid, or unqueryable AIC is **unknown**, never `0`. Count only confirmed numeric values as events with AIC data.
- Cost-owning jobs are `agent`, `detection`, and engine-named jobs. Cost is emitted on the dedicated agent span (`gh-aw.agent.agent`) when present, otherwise on the owning job's conclusion span. Do not require AIC on individual `gen_ai.chat`/tool spans or downstream non-owning jobs.
- Correlate cost and workflow identity on the same span using `gh-aw.workflow.name`, `gh-aw.run.id`, `gh-aw.repository`, and resource `github.workflow_ref`. Deduplicate mirrored spans by trace ID and span ID; do not sum both backends or collapse distinct cost-owning jobs into one run record.

## Analysis Requirements

Calculate:

- `total_events_analyzed`: total Sentry spans/events analyzed (use Sentry as the canonical source to avoid double-counting, since spans are exported to both backends simultaneously)
- `events_with_aic_data`: union count of spans from either backend that have a confirmed numeric AIC value
- `events_with_aic_data_sentry`: Sentry spans with confirmed numeric AIC data
- `events_with_aic_data_grafana`: Grafana spans with confirmed numeric AIC data

(Note: `events_with_aic_data_sentry + events_with_aic_data_grafana` may exceed `events_with_aic_data` when the same span is reported by both backends, since spans are exported to both simultaneously.)

- `events_missing_workflow`
- `total_aic`: sum confirmed numeric Sentry cost records (canonical); label a partial sample as a subtotal and report unknown, not zero, when no numeric records exist. Note if Grafana-only would yield a different total.
- `workflow_count` (unique workflows)
- `top_workflows_by_aic` (top 10)
- `avg_aic_per_event`: over confirmed numeric cost records only
- `p95_aic_per_event`: over confirmed numeric cost records only

For each workflow include:
- workflow name
- event count
- total AIC
- average AIC/event
- highest-AIC event (with run id if available)

## Report Output

Create exactly one discussion titled:

`[token-consumption] Daily AIC Consumption Report - YYYY-MM-DD`

Use this body structure:

### Executive Summary
- Total AIC (when available), workflow count, high-level trend notes, and whether Sentry/Grafana AIC is queryable.

### Key Metrics
| Metric | Value |
|---|---|
| Events analyzed | ... |
| Events with AIC data | ... |
| Events with AIC data (Sentry) | ... |
| Events with AIC data (Grafana) | ... |
| Total AIC | ... |
| Unique workflows | ... |
| Avg AIC/event | ... |
| P95 AIC/event | ... |

### Top 10 Workflows by AIC Consumption
| Workflow | Events | Total AIC | Avg AIC/Event |
|---|---:|---:|---:|
| ... |

### Grafana AIC Findings
(Show this as a top-level section only when AIC is not fully queryable in Grafana. Otherwise, include a one-line note "Grafana AIC: fully queryable" and collapse details using a `<details>` block.)

- State whether AIC was queryable in Grafana.
- If not queryable, list the exact issue (for example: attributes missing on spans, attribute present but typed as string, no numeric aggregation support in queried datasource, or insufficient workflow attribution fields).
- Include one concrete query or trace evidence line.
- Note: Grafana trace explore links may be limited in the report if the Grafana base URL is redacted by the safe-outputs step; use datasource UIDs or relative references where possible.

<details>
<summary>Data Quality and Gaps</summary>

- Events missing workflow identifiers
- Events missing AIC attributes
- Any assumptions or fallback fields used
- Sentry-specific AIC caveats
- Grafana-specific AIC caveats

</details>

### Recommendations
- 2-4 concrete actions to reduce AIC usage for the highest consumers.
- For either backend's AIC gap, recommend the smallest evidence-supported fix: producer emission only if raw cost-owning spans lack the field despite available cost data; otherwise field indexing/query syntax or missing trace-query tool access. Do not infer an emission bug from schema validation errors alone.

### References
- Include up to four relevant links (Sentry query links, Grafana traces/query references, and/or run links when available).

## Guardrails

- Be explicit when telemetry fields are absent or ambiguous.
- Never invent AIC values.
- If either backend lacks queryable numeric AIC, report that as an observability gap (unknown AIC), not as zero usage.
- Keep the report concise and actionable.

## Completion Requirement

You must call one safe output tool before finishing:
- `create_discussion` for normal reporting, then finish the original Claim.
- `noop` only if no valid telemetry could be retrieved, then finish the original Claim as cancelled.