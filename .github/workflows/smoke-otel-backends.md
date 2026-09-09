---
private: true
emoji: "🧪"
description: Smoke test that validates OTEL span export and query access for Sentry, Grafana, and Datadog
on:
  schedule: every 2 days
  slash_command:
    name: smoke-otel-backends
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  label_command:
    name: smoke
    events: [pull_request]
  reaction: "eyes"
  status-comment: true
permissions:
  contents: read
  issues: read
  pull-requests: read
name: Smoke OTEL
engine:
  id: codex
model: copilot/gpt-5.3-codex
strict: true
tools:
  bash: true
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, issues]
safe-outputs:
  create-issue:
    expires: 2h
    close-older-issues: true
    close-older-key: "smoke-otel-backends"
    labels: [automation, testing, observability]
    max: 1
timeout-minutes: 20
imports:
  - shared/mcp/datadog.md
  - shared/mcp/grafana.md
  - shared/mcp/sentry.md
  - shared/otel-queries.md
  - shared/sentry.md
  - shared/grafana.md
  - shared/datadog.md
  - shared/reporting.md
features:
  gh-aw-detection: false
sandbox:
  agent:
    id: awf
---

# Smoke OTEL

Validate the full OTEL loop for this repository:

1. gh-aw emits spans through the shared OTEL configuration.
2. The local OTEL mirror shows spans for this run.
3. Sentry can be queried for recent gh-aw telemetry.
4. Grafana can be queried for recent gh-aw telemetry.
5. Datadog can be queried for recent gh-aw telemetry.

The goal is to verify the current run end to end, not just prove that the backends contain some older telemetry.

Step 1 is local-only: it checks env injection, the local JSONL mirror, and span emission for the current run before any remote backend queries begin. Treat remote OTLP export errors as backend evidence for Sentry and Grafana, not as a failure of the local mirror.

## Required Secrets

This workflow expects these secrets to be present:

- `GH_AW_OTEL_SENTRY_ENDPOINT`
- `GH_AW_OTEL_SENTRY_AUTHORIZATION`
- `GH_AW_OTEL_GRAFANA_ENDPOINT`
- `GH_AW_OTEL_GRAFANA_AUTHORIZATION`
- `GH_AW_OTEL_DATADOG_ENDPOINT`
- `GH_AW_OTEL_DATADOG_API_KEY`
- `DD_API_KEY`
- `DD_APPLICATION_KEY`
- `DD_APP_KEY` (optional fallback)
- `SENTRY_ACCESS_TOKEN`
- `GRAFANA_URL`
- `GRAFANA_SERVICE_ACCOUNT_TOKEN`

## Rules

- Keep the investigation narrow and execution-oriented.
- Use the OTEL query playbook from `shared/otel-queries.md`.
- Prefer proving the current run is visible in each backend.
- Distinguish `pass`, `fail`, and `inconclusive` explicitly.
- Do not browse unrelated dashboards, issues, or traces.
- Always complete the workflow in this order: Step 1 local OTEL checks, Step 2 Sentry, Step 3 Grafana, then Step 4 Datadog.
- Do not skip Grafana or Datadog because an earlier backend failed or consumed time. Report the full matrix in the same run.

## Status model

- `pass`: the current run is visible and the read or write path worked.
- `inconclusive`: the backend can be queried and recent `gh-aw` spans exist, but this run is not yet visible.
- `fail`: emit-side or read-side behavior is broken.

## Steps

### Step 1: Verify local OTEL emission

Use bash to verify the send side for this run.

Run these checks:

```bash
echo "=== OTEL environment ==="
echo "OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT:+set}"
echo "OTEL_EXPORTER_OTLP_HEADERS=${OTEL_EXPORTER_OTLP_HEADERS:+set}"
echo "GH_AW_OTLP_ENDPOINTS=${GH_AW_OTLP_ENDPOINTS:+set}"
echo "OTEL_SERVICE_NAME=${OTEL_SERVICE_NAME:-}"

echo "=== OTEL configured backend hosts ==="
if [ -n "${GH_AW_OTLP_ENDPOINTS:-}" ]; then
  printf '%s' "$GH_AW_OTLP_ENDPOINTS" | jq -r '.[].url' | sed -E 's#https?://([^/]+)/?.*#\1#'
else
  echo "GH_AW_OTLP_ENDPOINTS missing"
fi

echo "=== OTEL local mirror ==="
if [ -f /tmp/gh-aw/agent/otel.jsonl ]; then
  wc -l /tmp/gh-aw/agent/otel.jsonl
  jq -c '.resourceSpans[]?.scopeSpans[]?.spans[]? | {name, traceId}' /tmp/gh-aw/agent/otel.jsonl | head -10
  echo "=== Current run markers in local mirror ==="
  jq -c '.resourceSpans[]? as $rs | ([($rs.resource.attributes[]? | select(.key == "github.run_id") | .value.stringValue)] | first // "") as $run_id | $rs.scopeSpans[]?.spans[]? | {name, run_id: $run_id}' /tmp/gh-aw/agent/otel.jsonl | grep '"run_id":"${{ github.run_id }}"' | head -5 || true
else
  echo "otel.jsonl missing"
fi

echo "=== OTEL export errors ==="
if [ -f /tmp/gh-aw/agent/otlp-export-errors.count ]; then
  cat /tmp/gh-aw/agent/otlp-export-errors.count
else
  echo 0
fi
if [ -f /tmp/gh-aw/agent/otlp-export-errors.jsonl ]; then
  echo "=== OTEL export error details (host/status/reason) ==="
  cat /tmp/gh-aw/agent/otlp-export-errors.jsonl
fi
```

Decide:

- `send_status = pass` only if OTEL env vars are present and `/tmp/gh-aw/agent/otel.jsonl` exists with at least one span for `${{ github.run_id }}`.
- `send_status = inconclusive` if spans exist locally but none for `${{ github.run_id }}` can be confirmed.
- Otherwise set `send_status = fail` and record the exact missing artifact or error.

Do not downgrade `send_status` because `/tmp/gh-aw/agent/otlp-export-errors.count` is non-zero. Record OTLP export errors as evidence for the remote backend rows and in `## Failure Analysis`, using `/tmp/gh-aw/agent/otlp-export-errors.jsonl` details (`host`, `status`, `reason`) for attribution.

### Step 2: Query Sentry

Use the Sentry MCP tools configured in this workflow.

1. Discover the organization and project for `${{ github.repository }}`.
2. Query recent telemetry for the last 24 hours.
3. First try to find spans for the current run using `${{ github.run_id }}` plus `service.name=gh-aw` when the MCP tool supports those filters.
4. If the current run is not visible, run a fallback query for `gh-aw` spans from the last 24 hours to distinguish ingestion delay from a broken Sentry query path.

Record all of the following:

- whether the MCP connection worked
- whether a project was found for this repository
- whether current-run spans were found
- whether recent `gh-aw` spans were found even if current-run spans were not
- one representative trace, event, or span link when available

Set:

- `sentry_status = pass` when query access works, current-run spans are visible, and the Sentry OTLP export path shows no errors attributable to Sentry
- `sentry_status = fail` when the Sentry OTLP export path shows errors attributable to Sentry
- `sentry_status = inconclusive` when query access works, no errors attributable to Sentry were seen, and recent `gh-aw` spans are visible but this run is not yet visible
- `sentry_status = fail` otherwise

### Step 3: Query Grafana

Use the Grafana MCP server configured in this workflow.

1. Inspect the available Grafana tracing tools first.
2. Discover the tracing datasource or Tempo surface that contains `gh-aw` spans.
3. Query the last 24 hours of traces.
4. First try to locate spans for `${{ github.run_id }}`.
5. If the current run is not visible, fall back to recent `service.name=gh-aw` spans to distinguish ingestion delay from a broken Grafana query path.

Record all of the following:

- whether the MCP connection worked
- which tracing datasource or tool was used
- whether current-run spans were found
- whether recent `gh-aw` spans were found even if current-run spans were not
- one representative trace, query, or panel reference when available

Set:

- `grafana_status = pass` when query access works, current-run spans are visible, and the Grafana OTLP export path shows no errors attributable to Grafana
- `grafana_status = inconclusive` when query access works and recent `gh-aw` spans are visible but this run is not yet visible
- `grafana_status = fail` otherwise

### Step 4: Query Datadog

Use the Datadog MCP server configured in this workflow, plus the bash evidence already gathered in Step 1.

1. Confirm the MCP connection works.
2. First try to find spans for the current run using Datadog span or trace tools with `service:gh-aw` and `${{ github.run_id }}`.
3. If current-run spans are not visible, run a fallback query for recent `gh-aw` spans from the last 24 hours to distinguish ingestion delay from a broken Datadog query path.
4. Inspect `/tmp/gh-aw/agent/otlp-export-errors.jsonl` for entries attributable to Datadog.

Record all of the following:

- whether the MCP connection worked
- whether current-run spans were found
- whether recent `gh-aw` spans were found even if current-run spans were not
- whether any OTLP export errors were attributable to Datadog
- one representative Datadog trace, span, host, or error record when available

Set:

- `datadog_status = pass` when query access works, current-run spans are visible, and the Datadog OTLP export path shows no errors attributable to Datadog
- `datadog_status = inconclusive` when query access works, no Datadog-attributed export errors were seen, and recent `gh-aw` spans are visible but this run is not yet visible
- `datadog_status = fail` otherwise

### Step 5: Final verdict

Compute the overall result:

- `PASS` only when `send_status`, `sentry_status`, `grafana_status`, and `datadog_status` all pass
- `INCONCLUSIVE` when no status is `fail` but at least one status is `inconclusive`
- otherwise `FAIL`

## Output

Create exactly one GitHub issue with:

- Draft the body locally first if needed, but emit only one final `create_issue` safe output after the full report is complete.
- Never create a placeholder, empty, `-`, or partial issue body.
- Do not retry `create_issue`: this workflow allows only one issue, so a premature call leaves the final report empty.
- Title: `Smoke OTEL - ${{ github.run_id }}`
- A short executive summary with overall `PASS`, `INCONCLUSIVE`, or `FAIL`
- A markdown table with one row for `Local OTLP`, one row for `Sentry`, one row for `Grafana`, and one row for `Datadog`, using these exact columns: `Backend`, `Write Config Present`, `Write Export Succeeded`, `Read Config Present`, `Read Query Succeeded`, `Overall`
- Use `✅` for pass, `❌` for fail, `🔶` for inconclusive, and `—` where a cell does not apply
- For the `Local OTLP` row, map `Write Config Present` to OTEL env vars being injected and map `Write Export Succeeded` to the local JSONL mirror containing current-run spans.
- For the `Datadog` row, map `Write Config Present` to Datadog appearing in the configured backend hosts, `Write Export Succeeded` to the absence of Datadog-attributed OTLP export errors, `Read Config Present` to Datadog MCP configuration being available, and `Read Query Succeeded` to Datadog MCP returning current-run or fallback recent `gh-aw` trace evidence.
- Use this table form:

  ```markdown
  | Backend | Write Config Present | Write Export Succeeded | Read Config Present | Read Query Succeeded | Overall |
  | --- | --- | --- | --- | --- | --- |
  | Local OTLP | ✅ | ✅ | — | — | ✅ |
  | Sentry | ✅ | ✅ | ✅ | 🔶 | 🔶 |
  | Grafana | ✅ | ❌ | ✅ | ✅ | ❌ |
  | Datadog | ✅ | ✅ | ✅ | ✅ | ✅ |
  ```

- The exact evidence used for each backend
- A `## Failure Analysis` section after the table
- The run URL: `${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`

For every unchecked cell in the table, add a dedicated subsection under `## Failure Analysis` that explains:

- the exact failing step
- the evidence you observed
- the most likely root cause
- whether the problem is on the write path, read path, auth, configuration, propagation, or the backend itself
- the next concrete debug step or fix

Do not stop after the first failure. Report the full Sentry, Grafana, and Datadog matrix even if one backend is completely broken.