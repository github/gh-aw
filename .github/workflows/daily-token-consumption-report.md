---
private: true
emoji: "📊"
description: Daily report of AI Credits (AIC) consumption across all agentic workflows using OTel telemetry from Sentry and Grafana
on:
  schedule: daily on weekdays
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
  bash: true
safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    title-prefix: "[token-consumption] "
    labels: [automation, observability, telemetry]
    close-older-issues: true
    expires: 1d
    max: 1
timeout-minutes: 30
imports:
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
5. Publish a concise daily GitHub issue report.

## Data Collection

### Step 1: Discover Sentry Context

1. Call `find_organizations` and select the org for this repository.
2. Call `find_projects` and select the project that corresponds to `${{ github.repository }}`.

### Step 2: Fetch Telemetry Events

First, attempt to call `search_events` using:
- `dataset: spans`
- query constrained to the selected project
- time range: last 24 hours
- include enough results to cover the day (use pagination as needed)

If `search_events` is **not available** (the tool is absent from the available tool list because no embedded LLM provider is configured), fall back to `list_events` with direct Sentry query syntax:
- `organizationSlug`: the org discovered in Step 1
- `dataset: spans`
- `query`: a filter for AI/LLM spans with AIC/cost data; start with `span.op:ai*` and also try `span.op:gen_ai*` if the first returns no results; if neither matches, try an empty query and filter client-side for records with AIC fields
- `fields`: include AIC/cost fields such as `gh-aw.aic`, `gh_aw.aic`, `aic`, `agent_usage.aic`, plus workflow metadata (`github.workflow`, `github.run_id`, `span.op`, `span.description`, `timestamp`); omit fields that return errors and retry with remaining fields
- `sort`: `-timestamp`
- Use pagination to cover the last 24 hours

If `dataset: spans` returns no usable records with either tool, retry with `dataset: transactions`.

Treat "no usable records" as either:
- zero events returned after pagination, or
- events returned but none contain any recognized AIC fields.

### Step 3: Fetch Grafana/Tempo Telemetry

Use the Grafana MCP server in this workflow.

1. Call `list_datasources` and identify the tracing datasource used for Tempo. Note the datasource UID — pass it explicitly to all subsequent Tempo tool calls.
2. Call `tempo_get-attribute-names` (with the datasource UID from step 1) to confirm available attribute keys.
3. Query recent traces in the last 24 hours with `tempo_traceql-search` (datasource UID from step 1, `start: now-24h`, `end: now`), scoped to gh-aw telemetry using `{.service.name =~ "gh-aw.*"}`. If attribute values returned by `tempo_get-attribute-values` reveal the actual `service.name`, use that value instead.
4. If needed, call `tempo_get-trace` (datasource UID from step 1) on representative traces to verify whether AIC fields are present and numeric on spans.
5. Record any query constraints or backend limitations (missing attributes, string-only values, fields not indexed, or inability to aggregate numerically).

Treat Grafana data as "no usable AIC records" when traces exist but none expose recognized AIC fields in queryable numeric form.

If `list_datasources` returns no Tempo datasource, or if `tempo_get-attribute-names` or `tempo_traceql-search` returns an error, treat this as "Grafana data unavailable" and record it as an observability gap in the Grafana AIC Findings section. Do not abort the workflow — continue to Step 4 with Sentry data only. Similarly, if the Grafana MCP server itself is unreachable (bad credentials, network error, or unconfigured secrets), record the failure reason and continue with Sentry-only data.

### Step 4: Extract Workflow + AIC Fields

For each Sentry event/span and Grafana span, derive:

- **Workflow name** using first non-empty of likely fields:
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
- **AIC value** with precedence:
  - Prefer `gh-aw.aic` → `gh_aw.aic` → `agent_usage.aic` → `aic`.
  - **For Sentry spans**: if none are present, use `0` (zero is distinct from "absent" and makes gaps visible in Sentry EAP).
  - **For Grafana spans**: if none are present, record as **absent** (not 0). Track absent-AIC spans separately for the `events_with_aic_data_grafana` gap metric — only count a span in that metric when a confirmed numeric AIC value is present.
- Recognized AIC fields:
  - `gh-aw.aic`
  - `gh_aw.aic`
  - `agent_usage.aic`
  - `aic`

For Sentry spans, normalize missing values to `0`. For Grafana spans, treat absent AIC fields as unknown and do not zero-fill them.

## Analysis Requirements

Calculate:

- `total_events_analyzed`: total Sentry spans/events analyzed (use Sentry as the canonical source to avoid double-counting, since spans are exported to both backends simultaneously)
- `events_with_aic_data`: union count of spans from either backend that have a confirmed numeric AIC value
- `events_with_aic_data_sentry`: Sentry spans with confirmed numeric AIC data
- `events_with_aic_data_grafana`: Grafana spans with confirmed numeric AIC data

(Note: `events_with_aic_data_sentry + events_with_aic_data_grafana` may exceed `events_with_aic_data` when the same span is reported by both backends, since spans are exported to both simultaneously.)

- `events_missing_workflow`
- `total_aic`: sourced from Sentry (canonical); note if Grafana-only would yield a different total
- `workflow_count` (unique workflows)
- `top_workflows_by_aic` (top 10)
- `avg_aic_per_event`
- `p95_aic_per_event`

For each workflow include:
- workflow name
- event count
- total AIC
- average AIC/event
- highest-AIC event (with run id if available)

## Report Output

Create exactly one issue titled:

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
- If Grafana lacks queryable numeric AIC, include at least one recommendation for fixing the instrumentation gap (for example: ensure spans emit `gh-aw.aic` as a numeric attribute, configure Tempo to index the relevant attribute, or verify OTLP export settings).

### References
- Include up to four relevant links (Sentry query links, Grafana traces/query references, and/or run links when available).

## Guardrails

- Be explicit when telemetry fields are absent or ambiguous.
- Never invent AIC values.
- If Grafana lacks queryable numeric AIC, report that as an observability gap (unknown AIC), not as zero usage.
- Keep the report concise and actionable.

## Completion Requirement

You must call one safe output tool before finishing:
- `create_issue` for normal reporting.
- `noop` only if no valid telemetry could be retrieved.