---
title: Artifacts
description: Complete reference for artifact names, directory structures, and download patterns used by GitHub Agentic Workflows.
sidebar:
  order: 298
---

GitHub Agentic Workflows upload several artifacts during workflow execution. This reference documents every artifact name, its contents, and how to access the data — especially for downstream workflows that use `gh run download` directly instead of `gh aw logs`.

## Quick Reference

| Artifact Name | Constant | Type | Description |
|---------------|----------|------|-------------|
| `agent` | `constants.AgentArtifactName`<br/>Source: `pkg/constants/job_constants.go` | Multi-file | Unified agent job outputs (logs, safe outputs, token usage summary) |
| `agent-output-fallback` | `constants.AgentOutputFallbackArtifactName` | Multi-file | Small dedicated copy of the processed agent output (`agent_output.json`) and raw safe-output NDJSON (`safeoutputs.jsonl`), used when the larger `agent` upload fails or times out |
| `activation` | `constants.ActivationArtifactName` | Multi-file | Activation job output (`aw_info.json`, `prompt.txt`, rate limits) |
| `firewall-audit-logs` | `constants.FirewallAuditArtifactName`<br/>Source: `pkg/constants/constants.go` | Multi-file | AWF firewall audit/observability logs (token usage, network policy, audit trail) |
| `detection` | `constants.DetectionArtifactName` | Conditional | Legacy inline engine (`features.gh-aw-detection: false`): single-file `detection.log`. The default external `gh-aw-detection` engine: multi-file `detection_result.json` + `step-summary.md`; `detection.log` is intentionally **not** uploaded (see below) |
| `safe-output` | `constants.SafeOutputArtifactName` | Legacy/back-compat | Historical standalone safe output artifact (`safe_output.jsonl`); in current compiled workflows this content is included in the unified `agent` artifact instead |
| `agent-output` | `constants.AgentOutputArtifactName` | Legacy/back-compat | Historical standalone agent output artifact (`agent_output.json`); in current compiled workflows this content is included in the unified `agent` artifact instead |
| `aw-info` | — | Single-file | Engine configuration (`aw_info.json`) |
| `prompt` | — | Single-file | Generated prompt (`prompt.txt`) |
| `experiment` | `constants.ExperimentArtifactName` | Multi-file | A/B experiment state (`state.json`) uploaded by the activation job when experiments are declared in the frontmatter |
| `usage` | `constants.UsageArtifactName` | Multi-file | Compact conclusion-job artifact with workflow-run metadata and token-usage files used by lightweight reporting and forecasting paths |
| `evals` | `constants.EvalsArtifactName` | Single-file | BinEval evaluation results (`evals.jsonl`) uploaded by the evals job when `evals` are declared in the workflow frontmatter |
| `safe-outputs-items` | `constants.SafeOutputItemsArtifactName` | Multi-file | Safe output items manifest (`safe-output-items.jsonl`), temporary ID map (`temporary-id-map.json`), and failure diagnostics (`safe-output-errors.json`, written only when the `Process Safe Outputs` step fails) |
| `code-scanning-sarif` | `constants.SarifArtifactName` | Single-file | SARIF file for code scanning results |

> [!IMPORTANT]
> Sync note: This table mirrors artifact-name constants in `pkg/constants/job_constants.go` and `pkg/constants/constants.go`. When those constant values change, update this page and the downstream artifact references in `docs/src/content/docs/reference/audit.md` and `docs/src/content/docs/reference/cost-management.md`.

## Legacy artifact names

:::caution[Deprecated]
`safe-output` and `agent-output` became legacy in the gh-aw `v0.34.x` unified-agent-artifact rollout. New downstream consumers should migrate to the unified `agent` artifact now; `gh aw logs` and `gh aw audit` keep reading the legacy names only for back-compat with older runs. Removal is planned no earlier than gh-aw `v1.0`.
:::

## Artifact Sets

The `gh aw logs` and `gh aw audit` commands support `--artifacts` to download only specific artifact groups:

| Set Name | Artifacts Downloaded | Use Case |
|----------|---------------------|----------|
| `all` | Everything | Full analysis (default) |
| `agent` | `agent` | Agent logs and outputs |
| `activation` | `activation` | Activation data (`aw_info.json`, `prompt.txt`) |
| `firewall` | `firewall-audit-logs` | Network policy and firewall audit data |
| `mcp` | `firewall-audit-logs` | MCP gateway traffic logs |
| `detection` | `detection` | Threat detection output |
| `experiment` | `experiment` | A/B experiment state (only present when experiments are declared) |
| `usage` | `usage` | Compact conclusion-job artifact for lightweight reporting and forecasting |
| `evals` | `evals` | BinEval evaluation results (only present when `evals` are declared) |
| `graders` | `usage`, `agent`, `agent-output-fallback` | Deterministic grader results (only present when `graders` are declared) |
| `github-api` | `activation`, `agent` | GitHub API rate limit logs |

```bash
# Download only firewall artifacts
gh aw logs <run-id> --artifacts firewall

# Download agent and firewall artifacts
gh aw logs <run-id> --artifacts agent --artifacts firewall

# Download everything (default)
gh aw logs <run-id>
```

## `firewall-audit-logs`

The `firewall-audit-logs` artifact is uploaded by **all firewall-enabled workflows**. It contains AWF (Agent Workflow Firewall) structured audit and observability logs.

> **⚠️ Important:** This artifact is **separate** from the `agent` artifact. Token usage data (`token-usage.jsonl`) lives here, not in the `agent` artifact.

### Directory Structure

```
firewall-audit-logs/
├── api-proxy-logs/
│   ├── token-usage.jsonl        ← Token usage data (input/output/cache tokens per API request)
│   └── token-diag.log           ← Token diagnostics JSONL (only when AWF_DEBUG_TOKENS=1)
├── squid-logs/
│   └── access.log               ← Network policy log (domain allow/deny decisions)
├── audit.jsonl                  ← Firewall audit trail (policy matches, rule evaluations)
└── policy-manifest.json         ← Policy configuration snapshot
```

`token-diag.log` is written by the AWF api-proxy `diag()` path (`containers/api-proxy/token-persistence.js`) to `$AWF_TOKEN_LOG_DIR/token-diag.log` (default `/var/log/api-proxy/token-diag.log`). It is only emitted when `AWF_DEBUG_TOKENS=1`, so set that environment variable on the workflow step that runs with AWF enabled when you need token diagnostics.

### Accessing Token Usage Data

**Recommended: Use `gh aw logs`**

```bash
# Download and analyze firewall data
gh aw logs <run-id> --artifacts firewall

# Output as JSON for scripting
gh aw logs <run-id> --artifacts firewall --json
```

**Direct download with `gh run download`:**

```bash
# Download the firewall-audit-logs artifact
gh run download <run-id> -n firewall-audit-logs

# Token usage data is at:
cat firewall-audit-logs/api-proxy-logs/token-usage.jsonl

# Network access log is at:
cat firewall-audit-logs/squid-logs/access.log

# Audit trail is at:
cat firewall-audit-logs/audit.jsonl

# Policy manifest is at:
cat firewall-audit-logs/policy-manifest.json
```

### Common Mistake

Downstream workflows sometimes download `agent-artifacts` or `agent` expecting to find `token-usage.jsonl`. This will silently return no data — the token usage file is only in the `firewall-audit-logs` artifact.

```bash
# ❌ WRONG — token-usage.jsonl is NOT in the agent artifact
gh run download <run-id> -n agent
cat agent/token-usage.jsonl  # File not found!

# ✅ CORRECT — download from firewall-audit-logs
gh run download <run-id> -n firewall-audit-logs
cat firewall-audit-logs/api-proxy-logs/token-usage.jsonl
```

### JSON Schemas

The JSONL files in this artifact are described by versioned JSON Schemas published by [github/gh-aw-firewall](https://github.com/github/gh-aw-firewall). Each record includes a `_schema` field (for example `"audit/v0.26.0"`) so consumers can identify the record type and AWF version.

| File | Schema asset | Pinned URL |
|------|--------------|------------|
| `audit.jsonl` | `audit.schema.json` | `https://github.com/github/gh-aw-firewall/releases/download/<tag>/audit.schema.json` |
| `api-proxy-logs/token-usage.jsonl` | `token-usage.schema.json` | `https://github.com/github/gh-aw-firewall/releases/download/<tag>/token-usage.schema.json` |

Use `releases/latest/download/` in place of a specific tag to track the most recent published release. Schemas are versioned by AWF release tag; consumers should match `_schema` by prefix (for example `_schema.startsWith("audit/")`) so additive changes remain non-breaking.

## `agent`

The unified `agent` artifact contains agent job outputs:

- Agent execution logs
- Safe output data (`agent_output.json`)
- GitHub API rate limit logs (`github_rate_limits.jsonl`)
- Token usage summary (`agent_usage.json`) — aggregated totals only; per-request data is in `firewall-audit-logs`. When AWF records include valid `ai_credits_this_response` and `ai_credits_total` values, the summary preserves those reported values instead of repricing the tokens.
- `otel.jsonl` — OTLP span mirror written by gh-aw's JavaScript span exporters when `observability.otlp` is configured

For OTLP configuration, runtime environment variables, and span semantics, see the [OpenTelemetry guide](/gh-aw/reference/open-telemetry/).

## `activation`

The `activation` artifact contains activation job outputs:

- `aw_info.json` — Engine configuration and workflow metadata
- `prompt.txt` — The generated prompt sent to the AI agent
- `github_rate_limits.jsonl` — Rate limit data from the activation job

## `detection`

The `detection` artifact is conditional:

- Inline engine (default): `detection.log`, the threat-detection analysis output. Legacy name: `threat-detection.log`.
- External `gh-aw-detection` engine (the default, or `features.gh-aw-detection: true`): `detection_result.json` and `step-summary.md`.

> [!IMPORTANT]
> When the external `gh-aw-detection` engine is used (the default, or `features.gh-aw-detection: true`), `detection.log` is not uploaded: it can contain content derived from the untrusted agent transcript that was passed to the detection engine (including secrets the agent may have echoed), so uploading it as a downloadable artifact would be a secret-exfiltration path. On that path the `detection` artifact contains `detection_result.json` (the structured verdict) and `step-summary.md`.

## `experiment`

The `experiment` artifact is uploaded by the activation job only when the workflow frontmatter declares one or more `experiments` entries. It contains:

- `state.json` — Cumulative per-variant invocation counters used to balance A/B assignments across runs

### Accessing experiment data

```bash
# Download the experiment artifact for a specific run
gh aw audit <run-id> --artifacts experiment

# Display the A/B experiment section in the audit report
gh aw audit <run-id>
```

The `🧪 A/B Experiments` section of the audit report shows the variant chosen for the run and the cumulative counts:

```
🧪 A/B Experiments
  • style = concise (cumulative: concise:5, detailed:4)
```

See [A/B Experiments](/gh-aw/experimental/experiments/) for how to declare experiments in workflow frontmatter.

## `usage`

The `usage` artifact is a compact conclusion-job artifact with workflow-run metadata and token-usage files for lightweight reporting and forecasting, so downstream tools can read aggregated usage data without downloading the full `agent` artifact.

Its `activity/summary.json` file uses the `usage-activity-summary/v1` schema. The optional activity sections are additive; the `working_set` section is always written when the calculation step executes:

```json
{
  "schema": "usage-activity-summary/v1",
  "firewall": {
    "total_requests": 12,
    "allowed_requests": 10,
    "blocked_requests": 2
  },
  "gateway": {
    "total_calls": 5,
    "failed_calls": 1,
    "total_input_size": 1000,
    "total_output_size": 5000,
    "max_input_size": 400,
    "max_output_size": 3000,
    "tool_calls": [
      {
        "tool_call_id": "call-1",
        "timestamp": "2026-09-09T00:00:00Z",
        "server_name": "github",
        "tool_name": "issue_read",
        "request_size": 200,
        "response_size": 800,
        "duration_ms": 100,
        "outcome": "success"
      }
    ],
    "servers": [
      {
        "server_name": "github",
        "request_count": 5,
        "tool_call_count": 5,
        "failed_calls": 1
      }
    ],
    "tools": [
      {
        "server_name": "github",
        "tool_name": "issue_read",
        "call_count": 5,
        "failed_calls": 1,
        "total_input_size": 1000,
        "total_output_size": 5000,
        "max_input_size": 400,
        "max_output_size": 3000,
        "avg_duration_ms": 120,
        "max_duration_ms": 250
      }
    ]
  },
  "integrity": {
    "total_filtered": 2,
    "filtered_server_counts": { "github": 2 },
    "filtered_tool_counts": { "issue_read": 2 },
    "filtered_reason_counts": { "integrity": 2 }
  },
  "working_set": {
    "measurement_state": "measured",
    "rebuild_factor": 3.9017857142857144,
    "cumulative_input_tokens": 874000,
    "peak_input_tokens": 224000,
    "rebuild_excess_tokens": 650000,
    "invocations": 5
  }
}
```

MCP `tool_calls` contain quantitative metadata only. Tool-call IDs are replaced with
run-local opaque identifiers, and request and response content is never copied into
the usage artifact. `outcome` is `success`, `failure`, or `incomplete`.

The conclusion job derives `gateway` and `integrity` from MCP gateway logs, falling back to `rpc-messages.jsonl` when `gateway.jsonl` is unavailable. These compact aggregates let `gh aw logs --artifacts usage` report MCP call, payload-size, duration, failure, and integrity-filter metrics without downloading raw logs. Cross-run reports include `runs_with_filtered_events`; the existing logs report summary remains the source for the total number of runs.

`rebuild_factor` is `cumulative_input_tokens / peak_input_tokens`, where each invocation contributes the canonical `input_tokens` value from the agent `token_usage.jsonl` record. Cache-read and cache-write fields are not added because provider normalization has already produced that logical input count. The factor is omitted when `measurement_state` is `unavailable`; `partial` means usable records were measured but malformed or unsupported records were ignored.

Working-Set Rebuild Factor measures cumulative context reconstruction relative to peak invocation context. It is an efficiency/trajectory metric, not a measurement of semantic coherence debt and not a predictor of task success. It cannot identify missing task facts or classify outcome quality. The metric is conceptually inspired by [“The Working Set of a Coding Agent: Coherence Debt in Repository-Scale Tasks”](https://arxiv.org/abs/2608.16630), while deliberately limiting the implementation to observable token traffic.

### Accessing usage data

Token-usage files are diagnostic data produced in the agent runtime. Their mirrored AIC fields support usage reporting and analysis, but are not sufficient evidence to classify a provider failure as a trusted budget-enforcement event.

```bash
# Download only the usage artifact
gh aw logs <run-id> --artifacts usage

# Or with gh run download
gh run download <run-id> -n usage
```

## `evals`

The `evals` artifact is uploaded by the evals job only when the workflow frontmatter declares one or more `evals` entries. It is not present on runs without evals and contains:

- `evals.jsonl` — Per-question BinEval evaluation results (YES/NO records) produced by running the declared evaluation questions against the agent output

### Accessing evals data

```bash
# Download only the evals artifact
gh aw logs <run-id> --artifacts evals

# Or with gh run download
gh run download <run-id> -n evals
```

The `gh aw audit` command exposes an `--evals` flag that skips runs without evals results and automatically downloads the evals artifact when `--artifacts` is narrowed:

```bash
# Audit only runs that contain evals results
gh aw audit <run-id> --evals
```

## Grader files

When the workflow frontmatter declares one or more `graders`, the grader files are stored inside existing artifacts rather than uploaded as a standalone `graders` artifact. They live under `agent/graders/` in the unified `agent` artifact, under `graders/` in the `agent-output-fallback` artifact when the fallback transport is used, and under `usage/graders/` after the conclusion job mirrors them for lightweight downloads. The files are:

- `grader_manifest.json` — The configured graders with their unit, direction, and threshold
- `grader_results.json` — The normalized grader results (status, value, pass/fail) computed from the run trace

### Accessing graders data

```bash
# Download the artifacts that carry grader results
gh aw logs <run-id> --artifacts graders

# Or with gh run download against the actual artifacts
gh run download <run-id> -n usage
gh run download <run-id> -n agent
gh run download <run-id> -n agent-output-fallback
```

`gh aw audit` reports grader outcomes in its console output and includes them in the JSON report under the `graders` key (results, plus `total`, `passed`, `failed`, `error_count`, and `unavailable_count`). Use `gh aw graders operational-value <run-id>` to replay the archived operational-value evaluator at an explicit evidence cutoff.

## Naming Compatibility

Artifact names changed between upload-artifact v4 and v5. The `gh aw logs` and `gh aw audit` commands handle both naming schemes transparently:

| Old Name (pre-v5) | New Name (v5+) | File Inside |
|--------------------|----------------|-------------|
| `aw_info.json` | `aw-info` | `aw_info.json` |
| `safe_output.jsonl` | `safe-output` | `safe_output.jsonl` |
| `agent_output.json` | `agent-output` | `agent_output.json` |
| `prompt.txt` | `prompt` | `prompt.txt` |
| `threat-detection.log` | `detection` | `detection.log` (inline engine only) |

Single-file artifacts are automatically flattened to root level regardless of their artifact directory name. Multi-file artifacts (`firewall-audit-logs`, `agent`, `activation`, `experiment`, and `detection` when the external `gh-aw-detection` engine is enabled) retain their directory structure.

## Workflow Call Prefixes

When workflows are invoked via `workflow_call`, GitHub Actions prepends a short hash to artifact names (e.g., `abc123-firewall-audit-logs`). The CLI handles this automatically by matching artifact names that end with `-{base-name}`.

```bash
# Both of these are recognized as the firewall artifact:
# - firewall-audit-logs           (direct invocation)
# - abc123-firewall-audit-logs    (workflow_call invocation)
```

## Learn More

See [Audit Commands](/gh-aw/reference/audit/) for downloading and analyzing workflow run artifacts, [Cost Management](/gh-aw/reference/cost-management/) for token-usage and spend reporting, [Network](/gh-aw/reference/network/) for firewall configuration, and [Compilation Process](/gh-aw/reference/compilation-process/) for how workflows upload artifacts.
