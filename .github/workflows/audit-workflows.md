---
emoji: "🔍"
description: Daily audit of all agentic workflow runs from the last 24 hours to identify issues, missing tools, errors, and improvement opportunities
on:
  schedule: daily
  workflow_dispatch:
max-ai-credits: 1500
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: audit-workflows-daily
experiments:
  audit_decomposition:
    variants: [single_agent, phased_sub_agents]
    description: "Tests whether decomposing audit-workflows into explicit analysis phases improves reliability and reduces long failing runs."
    hypothesis: "H0: no change in run_success_rate. H1: phased_sub_agents improves run_success_rate by at least 15% relative while keeping runtime within +10%."
    metric: run_success_rate
    secondary_metrics: [run_duration_minutes, empty_findings_rate]
    guardrail_metrics:
      - name: timeout_rate
        direction: min
        threshold: 0.05
    min_samples: 264
    weight: [50, 50]
    start_date: "2026-07-03"
    issue: 43177
engine:
  id: codex
  model-provider: openai
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  agentic-workflows:
  ledger:
    audit-history:
      schema:
        type: object
        required: [record_type, run_id, audited_at, audit_window, runs_reviewed, aggregate_metrics, finding_ids, recommendation_ids, anomaly_ids, recurring_finding_ids, outcome]
        properties:
          record_type:
            enum: [workflow_run_audit]
          run_id:
            type: string
          audited_at:
            type: string
          audit_window:
            type: object
            properties:
              start:
                type: string
              end:
                type: string
            additionalProperties: false
          runs_reviewed:
            type: integer
            minimum: 0
          aggregate_metrics:
            type: object
          finding_ids:
            type: array
            items:
              type: string
          recommendation_ids:
            type: array
            items:
              type: string
          anomaly_ids:
            type: array
            items:
              type: string
          recurring_finding_ids:
            type: array
            items:
              type: string
          outcome:
            enum: [findings_reported, noop]
        additionalProperties: false
      replay:
        script: |
          return {
            tables: {
              audits: {
                columns: {
                  ordinal: "integer",
                  run_id: "text",
                  audited_at: "text",
                  audit_window: "json",
                  runs_reviewed: "integer",
                  aggregate_metrics: "json",
                  finding_ids: "json",
                  recommendation_ids: "json",
                  anomaly_ids: "json",
                  recurring_finding_ids: "json",
                  outcome: "text"
                },
                primaryKey: ["run_id"],
                rows: records
                  .filter(record => record.payload.record_type === "workflow_run_audit")
                  .map(({ payload }, index) => ({
                    ordinal: index + 1,
                    run_id: payload.run_id,
                    audited_at: payload.audited_at,
                    audit_window: payload.audit_window,
                    runs_reviewed: payload.runs_reviewed,
                    aggregate_metrics: payload.aggregate_metrics,
                    finding_ids: payload.finding_ids,
                    recommendation_ids: payload.recommendation_ids,
                    anomaly_ids: payload.anomaly_ids,
                    recurring_finding_ids: payload.recurring_finding_ids,
                    outcome: payload.outcome
                  }))
              }
            }
          };
      max-record-kb: 16
      max-patch-kb: 10
  timeout: 300
safe-outputs:
  upload-asset:
    max: 3
    allowed-exts: [.png, .jpg, .jpeg, .svg]
timeout-minutes: 30
imports:
  - uses: shared/daily-audit-charts.md
    with:
      title-prefix: "[audit-workflows] "
      expires: 1d
  - ../skills/jqschema/SKILL.md
  - shared/reporting.md


  - shared/otlp.md
  - shared/default-ai-credits-pricing.md
  - shared/graders.md
features:
  gh-aw-detection: true
evals:
  - id: workflow_runs_audited
    question: Did the agent audit agentic workflow runs from the last 24 hours?
  - id: issues_identified_or_noop
    question: Were issues, missing tools, errors, and improvement opportunities identified, or was noop used when no problems were found?

model: openai/gpt-5.3-codex
---

# Agentic Workflow Audit Agent

You are the Agentic Workflow Audit Agent - an expert system that monitors, analyzes, and improves agentic workflows running in this repository.

## Mission

Daily audit all agentic workflow runs from the last 24 hours to identify issues, missing tools, errors, and opportunities for improvement.

## Current Context

- **Repository**: ${{ github.repository }}

## Report Formatting

- Begin the final discussion with a concise `### Summary` of the key takeaway and recommendations.
- Use `###` headings for report sections and `####` headings for subsections. Do not use `#` or `##` headings in the report body.
- Keep critical findings and key metrics visible, and wrap long audit findings, evidence, or logs in `<details><summary><b>View full findings</b></summary>` blocks.

## 📊 Trend Charts

Generate 2 charts from past 30 days workflow data:

1. **Workflow Health**: Success/failure counts and success rate (green/red lines, secondary y-axis for %)
2. **Token Usage**: Daily tokens (bar/area) + 7-day moving average

Save to: `/tmp/gh-aw/python/charts/{workflow_health,token}_trends.png`
Upload charts and embed them in the discussion with 2-3 sentence analysis each. Call the `upload_asset` safe-output tool for each chart using the absolute chart path. Record the returned asset URLs and include them in the discussion body.

---

## Audit Process

Use gh-aw MCP server (not CLI directly). Run `status` tool to verify.

**Collect Logs**: Use MCP `logs` tool to download workflow logs:
```
Use the agentic-workflows MCP tool `logs` with parameters:
- start_date: "-1d" (last 24 hours)
Output is saved to: /tmp/gh-aw/aw-mcp/logs
```

**Engine Classification**: Use `summary.engine_counts` from the `logs` tool output to report engine usage. Each run also has an `agent` field (e.g., `"copilot"`, `"claude"`, `"codex"`). Both are derived from the `engine_id` field in `aw_info.json`, which is the authoritative source for engine type.

**IMPORTANT**: Do NOT infer engine type by scanning `.lock.yml` files. Lock files contain the word `copilot` in allowed-domains lists and workflow source paths regardless of which engine the workflow uses, causing false positives.

**Success Rate Rollups — Exclude Intentional-Failure Workflows**: When computing the fleet-wide or prod-main success rate, **exclude** runs where `intentional_failure` is `true`. These workflows (e.g. `Daily Credit Limit Test`, `Daily Max AI Credits Test`) are credit-guardrail stress tests that are *designed* to fail; including them would depress the real-regression baseline. The `logs` tool marks them in `runs[].intentional_failure` and counts them in `summary.intentional_failure_runs`. Always report the adjusted rate alongside the raw rate, e.g. `"92.7% raw (94.2% excl. intentional failures)"`.

**Intentional-failure workflows that MUST be excluded from all success-rate and health rollups**:
- `Daily Credit Limit Test` (`daily-credit-limit-test`) — trips the `max-daily-ai-credits` guardrail by design
- `Daily Max AI Credits Test` (`daily-max-ai-credits-test`) — trips the `max-ai-credits` per-run firewall by design

{{#if experiments.audit_decomposition == 'phased_sub_agents'}}
**Analyze** in explicit phases:
1. **Collection phase**: summarize missing tools, hard failures, and token/runtime outliers.
2. **Clustering phase**: group recurring failure signatures and map them to known issues vs. novel anomalies.
3. **Recommendation phase**: derive the smallest actionable set of fixes, each linked to evidence.
4. **Synthesis phase**: combine phase outputs into one final audit report and ledger record.
{{else}}
**Analyze**: Review logs for:
- Missing tools (patterns, frequency, legitimacy)
- Errors (tool execution, MCP failures, auth, timeouts, resources)
- Performance (token usage, timeouts, efficiency)
- Patterns (recurring issues, frequent failures)
{{/if}}

{{#if experiments.audit_decomposition == 'phased_sub_agents'}}
Before writing the final report, verify that each recommendation cites at least one concrete log or trend signal and that recurring issues are deduplicated across phases.
{{else}}
Before writing the final report, verify recommendations are concrete and evidence-based.
{{/if}}

**Audit history**: Use the replay projection at `/tmp/gh-aw/ledgers/audit-history/ledger.db` as the durable record of each audit. Query recent audit summaries before analysis to compare stable finding, recommendation, and anomaly IDs, and query by run ID before appending to avoid duplicate records:

```sql
SELECT run_id, audited_at, finding_ids, recommendation_ids, anomaly_ids, recurring_finding_ids, outcome
FROM audits ORDER BY ordinal DESC LIMIT 100;
```

If replay is reported unavailable, use the generic `records` history instead of querying replay tables.
Use run logs as the source for the 30-day charts and rollups; do not maintain a second copy of those metrics in mutable memory files.

After completing the report, query `audits` for the current run ID, then submit one `ledger_append` record with `record_type: workflow_run_audit` if absent. Include the run ID, UTC audit timestamp, audit window, number of runs reviewed, compact aggregate metrics, stable IDs for findings/recommendations/anomalies, recurring finding IDs, and the outcome (`findings_reported` or `noop`). Keep records structured and bounded; never store logs, prompts, raw tool output, or other sensitive content. Query the projection's `diagnostics` table to inspect malformed or incomplete records. Ledger branches are append-only: do not edit them directly or invoke compaction.

## Guidelines

**Security**: Never execute untrusted code, validate data, sanitize paths
**Quality**: Be thorough, specific, actionable, accurate  
**Efficiency**: Use repo memory, batch operations, respect timeouts

History is stored as bounded structured `workflow_run_audit` records in the configured Git-backed ledger.

Always create discussion with findings and update repo memory.
