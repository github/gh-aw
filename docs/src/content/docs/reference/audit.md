---
title: Auditing Workflows
description: Reference for the gh aw audit commands — single-run analysis, behavioral diff, and cross-run security reports.
sidebar:
  order: 297
---

The `gh aw audit` commands download workflow run artifacts and logs, analyze MCP tool usage and network behavior, and produce structured reports suited for security reviews, debugging, and feeding to AI agents.

## `gh aw audit <run-id-or-url> [<run-id-or-url>...]`

Audit one or more workflow runs. When a single run is provided, a detailed Markdown report is generated. When two or more runs are provided, the first is used as the base (reference) run and the remaining runs are compared against it, producing a diff report. Add `--group` to aggregate findings instead of diffing runs.

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<run-id-or-url>` | A numeric run ID, run URL, job URL, or job URL with step anchor |
| `[<run-id-or-url>...]` | Additional runs to compare against the first (diff mode) |

Each argument accepts a numeric run ID, a standard or short run URL, a job URL, or a job URL with a step anchor. GitHub Enterprise URLs follow the same patterns.

In single-run mode, a job URL without a step anchor extracts the first failing step's output; a step-anchored URL extracts that specific step. In diff mode, any job or step-specific URL is normalized to its parent run ID, so comparisons always happen at run scope. Self-comparisons and duplicate run IDs are rejected.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-o, --output <dir>` | `./logs` | Directory to write downloaded artifacts and report files |
| `--json` | off | Output report as JSON to stdout |
| `--parse` | off | Run JavaScript parsers on agent and firewall logs, writing `log.md` and `firewall.md` (single-run only) |
| `--repo <owner/repo>` | auto | Specify repository when the run ID is not from a URL |
| `--stdin` | off | Read run IDs or URLs from stdin (one per line) instead of positional arguments |
| `--verbose` | off | Print detailed progress information |
| `--format <fmt>` | `pretty` | Diff output format: `pretty` or `markdown` (multi-run only) |
| `--group` | off | Group audit findings so each `[run, code]` pair appears once with an occurrence count and representative entry |

Top-level fields in `--json` output are stable; nested sub-fields may be extended but are not removed without deprecation. Add `--parse` to populate `behavior_fingerprint` and `agentic_assessments`.

**Single-run examples:**

```bash
gh aw audit 1234567890
gh aw audit https://github.com/owner/repo/actions/runs/1234567890
gh aw audit 1234567890 --parse
gh aw audit 1234567890 --json
gh aw audit 1234567890 -o ./audit-reports
gh aw audit 1234567890 --repo owner/repo
```

**Stdin mode:**

Use `--stdin` to pass run IDs or URLs from a file or pipeline. This is mutually exclusive with positional arguments. Blank lines and lines starting with `#` are ignored. When passing bare numeric IDs (without embedded repo context), `--repo owner/repo` is required.

```bash
echo "1234567890" | gh aw audit --stdin
echo -e "1234567890\n9876543210" | gh aw audit --stdin   # diff mode: first is base
cat run-ids.txt | gh aw audit --stdin
cat run-ids.txt | gh aw audit --stdin --repo owner/repo  # required for bare numeric IDs
```

**Multi-run diff examples:**

```bash
gh aw audit 12345 12346                        # Compare two runs
gh aw audit 12345 12346 12347 12348            # Compare base against 3 runs
gh aw audit 12345 12346 --format markdown      # Markdown output for PR comments
gh aw audit 12345 12346 --json                 # JSON for CI integration
gh aw audit 12345 12346 --repo owner/repo      # Specify repository
gh aw audit 12345 12346 --group                # Group findings by run and code
gh aw audit 12345 12346 --group --json         # JSON grouped findings
```

**Single-run report sections** (rendered in Markdown or JSON): Overview, Comparison, Task/Domain, Behavior Fingerprint, Agentic Assessments, Metrics, Key Findings, Recommendations, Observability Insights, Performance Metrics, Engine Config, Prompt Analysis, Session Analysis, Safe Output Summary, MCP Server Health, Jobs, Downloaded Files, Missing Tools, Missing Data, Noops, MCP Failures, Gateway Steering Events, Firewall Analysis, Policy Analysis, Redacted Domains, Errors, Warnings, Tool Usage, MCP Tool Usage, Created Items, Graders.

The Gateway Steering Events section reports `token_steering` and `timeout_steering` warnings emitted when a run approaches its AI Credits or time limit. JSON output includes each event's type, message, and timestamp when available.

The Observability Insights section includes `skill_activations` when skill-invocation evidence is found. Each entry reports the skill name, `status` (`invoked`), the detection `source` (`agent_output` or `log_parse`), and provenance fields in JSON output. This makes it possible to distinguish skills that were merely restored or installed from skills that were actually invoked during the run.

The Graders section is present when the run recorded deterministic grader results (`graders` declared in the workflow frontmatter). The `graders` object in JSON output lists each grader (`id`, `name`, `status`, `value`, `unit`, `passed`, and, when declared in the grader manifest, `direction` and `threshold`) plus aggregate counts: `total`, `passed`, `failed`, `error_count`, and `unavailable_count`. Grader results are read from the compact `usage` artifact (mirrored there by the conclusion job), the unified `agent` artifact, or the `agent-output-fallback` artifact, so they are available even when `--artifacts usage` narrows the download. The same `graders` object is included per run in `gh aw logs --json` output.

### Audit finding codes

Every `key_findings` entry includes a stable `code` for downstream automation. Titles and descriptions are human-readable and may change; consumers must use `code` to identify a finding type. Existing codes are not repurposed.

The top-level `schema_version` identifies the audit cache and JSON contract version. Cached reports with an older version are regenerated before use.

| Code | Finding |
|---|---|
| `workflow_failed` | The workflow failed |
| `workflow_timeout` | The workflow timed out |
| `high_token_usage` | Token usage exceeded the audit threshold |
| `many_iterations` | The run exceeded the turn threshold |
| `multiple_errors` | The run produced more than five errors |
| `mcp_server_failures` | One or more MCP servers failed |
| `tools_not_available` | Requested tools were unavailable |
| `blocked_network_requests` | The firewall blocked network requests |
| `workflow_succeeded` | The workflow completed successfully |
| `threat_detection_job_failed` | The `detection` job failed |
| `threat_detected` | The detection artifact identified one or more threats |
| `agentic_resource_heavy_for_domain` | Resource use was high for the inferred task domain |
| `agentic_overkill_for_agentic` | The task may not require an agentic workflow |
| `agentic_poor_agentic_control` | Agentic control signals were weak |
| `agentic_partially_reducible` | Part of the task could use deterministic steps |
| `agentic_model_downgrade_available` | A smaller model may be sufficient |
| `agentic_delegated_context_present` | Delegated workflow context was preserved |

The audit inspects the `detection` job result and the downloaded `detection` artifact. A failed job emits `threat_detection_job_failed`. A structured verdict in `detection_result.json`, or a legacy `THREAT_DETECTION_RESULT` verdict in `detection.log`, emits `threat_detected` when `prompt_injection`, `secret_leak`, or `malicious_patch` is true. Threat reasons are not copied into the finding description because they may contain sensitive agent output.

The Metrics section includes an `ambient_context` object when available. Ambient context captures the first LLM inference footprint for the run. It is absent when token-usage data is unavailable for the run — for example, when neither `token-usage.jsonl` nor the fallback `agent_usage.json` can be found in the downloaded artifacts, which is common for older runs and runs without firewall/usage artifacts:
- `ambient_context.input_tokens` — input tokens for the first invocation
- `ambient_context.cached_tokens` — cache-read tokens reused by the first invocation

The Metrics section and JSON output also include `working_set` from the compact usage activity summary. When measured, human-readable output shows `working-set-rebuild=<factor>×`; JSON preserves the measurement state, factor, cumulative and peak input tokens, rebuild excess, and invocation count. Diff output compares measured factors without assigning a success or failure interpretation.

Working-Set Rebuild Factor measures cumulative context reconstruction relative to peak invocation context. It is an efficiency/trajectory metric, not a measurement of semantic coherence debt and not a predictor of task success. Equal factors can occur on successful and failed runs, and missing required facts cannot be inferred from the value.

**Diff output** includes network changes (new, removed, and allow/deny flips), anomaly flags, MCP tool invocation changes, run-level metric deltas, token and AIC breakdowns, tokens per turn, per-tool call counts with max input/output sizes, and aggregated bash command usage.

With multiple comparisons, `--json` emits a single object for one comparison or an array for many, while `--format pretty` and `--format markdown` separate each diff with dividers. With `--group`, audit emits `runs_analyzed`, `entries`, and (when applicable) `skipped_runs`; each entry contains `run_id`, `code`, `occurrences`, and `representative_entry`. Only actionable findings (severity of low or above) are grouped. Runs excluded by `--experiment`, `--runtime`, or `--evals`, or whose audit data could not be loaded, are listed in `skipped_runs` and excluded from `runs_analyzed`.

When artifacts are present, audit processing also persists extracted skill-activation data into `run_summary.json`, which downstream automation can consume alongside the rendered report.

## JSON output schemas

Use `gh aw json-schema` to generate a JSON Schema for structured audit or logs output. The `audit` schema describes `gh aw audit --json`, the `logs` schema describes `gh aw logs --json`, and `logs-jsonl` describes each item written by `gh aw logs --cached-jsonl`. The schema is written to stdout and can be redirected to a file:

```bash
gh aw json-schema audit > audit.schema.json
gh aw json-schema logs > logs.schema.json
gh aw json-schema logs-jsonl > logs-jsonl.schema.json
```

`make recompile` regenerates the checked-in schemas, including `schemas/logs-jsonl.schema.json`. These schemas derive directly from the corresponding Go output types.

## `gh aw logs --format <fmt>`

Generate a cross-run security and performance audit report across multiple recent workflow runs.
This feature is built into the `gh aw logs` command via the `--format` flag.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `[workflow]` | all workflows | Filter by workflow name or filename (positional argument) |
| `-c, --count <n>` | 10 | Number of recent runs to analyze |
| `--last <n>` | — | Alias for `--count` |
| `--format <fmt>` | — | Output format: `markdown` or `pretty` (generates cross-run audit report) |
| `--json` | off | Output cross-run report as JSON (when combined with `--format`) |
| `--repo <owner/repo>` | auto | Specify repository |
| `-o, --output <dir>` | `./logs` | Directory for downloaded artifacts |
| `--stdin` | off | Read run IDs or URLs from stdin (one per line) instead of run-discovery; content filters still apply |
| `--verbose` | off | Print detailed progress |

Top-level fields in `--json` output are stable; nested sub-fields may be extended but are not removed without deprecation.

The report output includes an executive summary, domain inventory, metrics trends, MCP server health, and per-run breakdown. It detects cross-run anomalies such as domain access spikes, elevated MCP error rates, and connection rate changes.

When cross-run behavior data is available, JSON output also includes `cluster_analysis` with:

- `clusters`: groups of runs sharing the same value for a behavioral dimension
- `patterns`: automatically detected divergence signals across those groups

### Interpreting `cluster_analysis` values

**Cluster dimensions (`clusters[].dimension`)**

| Dimension | Meaning | Typical values |
|---|---|---|
| `conclusion` | Workflow completion outcome | `success`, `failure`, `timed_out`, `cancelled` |
| `task_domain` | Dominant task type inferred from run behavior | e.g. `code_editing`, `testing`, `unknown` |
| `execution_style` | How the run progresses | e.g. `sequential`, `iterative`, `unknown` |
| `resource_profile` | Relative resource usage posture | e.g. `light`, `heavy`, `unknown` |

`clusters[].metrics` summarizes each group:
- `avg_tokens`, `median_tokens`, `stddev_tokens`: token usage center/spread
- `avg_turns`: average turns per run
- `avg_duration_ns`: average runtime (nanoseconds)
- `avg_errors_per_run`: average error count in the cluster
- `success_rate`: fraction of runs in the cluster with `conclusion == "success"`

Clusters from dimensions with only one observed value are omitted to avoid noise.

**Pattern kinds (`patterns[].kind`)**

| Kind | What it means | Severity rule |
|---|---|---|
| `resource_divergence` | Failed runs use more tokens than successful runs | `low` ≥1.5x, `medium` ≥2.0x, `high` ≥3.0x |
| `failure_correlation` | A non-conclusion dimension value has only failed runs | always `high` |
| `style_skew` | One non-conclusion value dominates the dataset | `low` when a value is >80% of runs (minimum 5 runs total) |

Use these as triage signals: high-severity patterns are good candidates for immediate investigation; low-severity patterns are often workload-shape hints.

For each run in detailed logs JSON output, an `ambient_context` object is included when token usage data is available. It reflects only the first LLM invocation in the run (`input_tokens` and `cached_tokens`). It is absent when the downloaded artifacts do not contain usable `token-usage.jsonl` or fallback `agent_usage.json` data for that run.

Detailed logs JSON output includes the same `working_set` object when the usage activity summary is available. The default `gh aw logs` runs table (both the compact agent-optimized format and the verbose `-v` format) also surfaces a single `WSRF` column with the rebuild factor rounded to two decimal places, showing `-` when the metric was not measured for that run.

**`--stdin` mode:** Pass `--stdin` to supply an explicit list of run IDs or URLs instead of letting the command discover runs from the GitHub API. Date, count, and workflow-name filters are ignored; `--engine`, `--firewall`, `--safe-output`, and other content filters still apply. Blank lines and `#`-prefixed lines are ignored. Bare numeric IDs require `--repo owner/repo`.

```bash
cat run-ids.txt | gh aw logs --stdin
echo "1234567890" | gh aw logs --stdin --engine claude
cat run-ids.txt | gh aw logs --stdin --repo owner/repo   # required for bare numeric IDs
```

**Examples:**

```bash
gh aw logs --format markdown
gh aw logs daily-repo-status --format markdown --count 10
gh aw logs agent-task --format markdown --last 5 --json
gh aw logs --format pretty
gh aw logs --format markdown --repo owner/repo --count 10
```

## Learn More

- [Cost Management](/gh-aw/reference/cost-management/) — Track AIC-first spend and token usage
- [Artifacts](/gh-aw/reference/artifacts/) — Artifact names, directory structures, and token usage file locations (`token-usage.jsonl` in `firewall-audit-logs`)
- [AI Credits Specification](/gh-aw/specs/ai-credits-specification/) — Primary AIC computation details
- [Network](/gh-aw/reference/network/) — Firewall and domain allow/deny configuration
- [MCP Gateway](/gh-aw/reference/mcp-gateway/) — MCP server health and debugging
- [CLI Commands](/gh-aw/setup/cli/) — Full CLI reference

## Consuming Audit Reports in Workflows

All three audit commands support `--json` for structured stdout, which you can pipe through `jq` to extract only the fields a model needs.

| Command | Use case |
| --------- | ---------- |
| `gh aw audit <run-id> --json` | Single run — `key_findings`, `recommendations`, `metrics` |
| `gh aw logs [workflow] --last 10 --json` | Trend analysis — `per_run_breakdown`, `domain_inventory` |
| `gh aw audit <id1> <id2> --json` | Before/after — `run_metrics_diff`, `firewall_diff` |

Inside GitHub Actions workflows, agents should use the `agentic-workflows` MCP tool instead of invoking the CLI directly.

### Posting findings as a PR comment

```aw wrap
---
description: Post audit findings as a PR comment after each agent run

on:
  workflow_run:
    workflows: ['my-workflow']
    types: [completed]

engine: copilot

tools:
  github:
    toolsets: [pull_requests]
  agentic-workflows:

permissions:
  contents: read
  actions: read
  pull-requests: write
---

# Summarize Audit Findings

Use the `agentic-workflows` MCP tool `audit` with run ID ${{ github.event.workflow_run.id }}, identify the pull request that triggered it, and post a comment summarizing key findings and blocked domains. Highlight issues with severity `high` or `critical`. If there are no findings, post a brief "no issues found" comment.
```

### Detecting regressions with diff

```aw wrap
---
description: Detect regressions between two workflow runs

on:
  workflow_dispatch:
    inputs:
      base_run_id:
        description: 'Baseline run ID'
        required: true
      current_run_id:
        description: 'Current run ID to compare'
        required: true

engine: copilot

tools:
  github:
    toolsets: [issues]
  agentic-workflows:

permissions:
  contents: read
  actions: read
  issues: write
---

# Regression Detection

Use the `agentic-workflows` MCP tool `audit` with run IDs ${{ inputs.base_run_id }} and ${{ inputs.current_run_id }} to compare the two runs. Check for new blocked domains, increased MCP error rates, cost increase > 20%, or token usage increase > 50%. If regressions are found, open a GitHub issue with a table from `run_metrics_diff`, affected domains from `firewall_diff`, and affected MCP tools from `mcp_tools_diff`.
```

### Filing issues from audit findings

```aw wrap
---
description: File GitHub issues for high-severity audit findings

on:
  workflow_run:
    workflows: ['my-workflow']
    types: [completed]

engine: copilot

tools:
  github:
    toolsets: [issues]
  agentic-workflows:

permissions:
  contents: read
  actions: read
  issues: write
---

# Auto-File Issues for Critical Findings

Use the `agentic-workflows` MCP tool `audit` with run ID ${{ github.event.workflow_run.id }}. Filter `key_findings` for severity `high` or `critical`. For each finding without a matching open issue, create one with the finding title, description, impact, and recommendations, labelled `audit-finding`. If no critical findings, call the `noop` safe output tool.
```

### Weekly audit monitoring agent

```aw wrap
---
description: Weekly audit digest with trend analysis

on:
  schedule: weekly

engine: copilot

tools:
  github:
    toolsets: [discussions]
  agentic-workflows:
  cache-memory:
    key: audit-monitoring-trends

permissions:
  contents: read
  actions: read
  discussions: write
---

# Weekly Audit Monitoring Digest

1. Use the `agentic-workflows` MCP tool `logs` with parameters `workflow: my-workflow, last: 10` and read `/tmp/gh-aw/cache-memory/audit-trends.json` as the previous baseline.
2. Detect: cost spikes (`cost_spike: true` in `per_run_breakdown`), new denied domains in `domain_inventory`, MCP servers with `error_rate > 0.10` or `unreliable: true`, and week-over-week changes in `error_trend.runs_with_errors`.
3. Create a GitHub discussion "Audit Digest — [YYYY-MM-DD]" with an executive summary, anomalies table, and MCP health table.
4. Update `/tmp/gh-aw/cache-memory/audit-trends.json` with rolling averages (cost, tokens, error count, deny rate), keeping only the last 30 days.
```

Cross-run JSON can be large — extract only the slices your model needs.
