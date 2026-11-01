---
title: Auditing Workflows
description: Reference for the gh aw audit commands — single-run analysis, behavioral diff, and cross-run security reports.
sidebar:
  order: 297
---

The `gh aw audit` commands download workflow run artifacts and logs, analyze MCP tool usage and network behavior, and produce structured reports suited for security reviews, debugging, and feeding to AI agents.

## `gh aw audit <run-id-or-url> [<run-id-or-url>...]`

Audit one or more workflow runs. When a single run is provided, a detailed Markdown report is generated. When two or more runs are provided, the first is used as the base (reference) run and the remaining runs are compared against it, producing a diff report. Add `--group` to aggregate findings instead of diffing runs.

**Arguments:** each `<run-id-or-url>` accepts a numeric run ID, a standard or short run URL, a job URL, or a job URL with a step anchor (GitHub Enterprise URLs included). Additional arguments are compared against the first (diff mode).

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
| `--no-baseline` | off | Skip baseline lookup and comparison for single-run audits; the Comparison section reports no baseline found |
| `--search <query>` | off | Retrieve relevant unified-session evidence with local BM25 ranking (single-run only; incompatible with `--group` and job/step URLs) |
| `--search-limit <n>` | `5` | Maximum evidence groups returned by `--search`, from 1 to 20 |

Top-level fields in `--json` output are stable; nested sub-fields may be extended but are not removed without deprecation. Add `--parse` to populate `behavior_fingerprint` and `agentic_assessments`.

By default a single-run audit renders a Comparison section against an earlier run of the same workflow. Use `--no-baseline` when that baseline is unrepresentative or to avoid the extra downloads. A `--no-baseline` result is never written to the audit cache, so it cannot suppress the Comparison section of a later default audit.

### Retry history

When unified-session retry evidence is available, `--json` includes a `retries`
object. `agent_retries` and `detection_retries` count observed harness and startup
retries separately for the main agent and threat detection. `attempts` retains
harness and startup attempts, including interrupted attempts, with available IDs,
timestamps, strategy, backoff, exit code, failure classes, and outcome.
Checkpoint and completion records for the same attempt are deduplicated.

`engine_retries` and `detection_engine_retries` count native engine retries
separately; `engine_retry_history` preserves their available metadata and source
references. Native retries are associated with harness attempts only when the
recorded evidence supports that association. Missing evidence does not prove
that no retries occurred, and a missing exit code is not a successful exit.

Retry history does not change token accounting or classify ordinary
continuations as retries. See the
[Unified Agent Session Specification](/gh-aw/specs/unified-agent-session-specification/)
for `harness.attempt` records and
[Harness Retry Policy](/gh-aw/reference/engines/#harness-retry-and-post-result-watchdog-policy)
for execution behavior.

### Searching session evidence

```bash
gh aw audit 1234567890 --search "api.example.com ERR_AUTH_401" --json
gh aw audit 1234567890 --search "github.issue_read timeout" --search-limit 3
```

`--search` adds an `evidence_search` section without replacing the full audit.
It indexes actual events from the downloaded, redacted `usage/aw_session.jsonl`
(or root `aw_session.jsonl`), not raw engine logs or synthesized run summaries.
Narrowed `--artifacts` selections automatically include `usage`. Missing,
malformed, unsupported, oversized, or symlinked unified-session inputs produce
an error rather than a partial search presented as complete. Search accepts
queries up to 1,024 bytes and sessions up to 64 MiB, 20,000 events, and 256
distinct stages. Decoded data is bounded to 64 nesting levels.

Ranking uses an in-memory BM25 index with no model calls, embeddings, external
search service, or persistent index. Paths, tool names, domains, and error codes
are indexed both as full identifiers and as components. Exact repeated events
share one ranking document so retries cannot dominate relevance statistics.
BM25 scores only complete decoded event fields and event types, never Drain3
templates or anomaly scores. Identifiers remain searchable even when a
displayed template masks them.

Repetition grouping is automatic; there is no Drain3 option to configure.
Exact duplicates share occurrence counts without mining. Drain3 runs only when
more matches exist than the requested result limit and the first `limit + 1`
ranked candidates include repeated component/phase/type stages with identical
structured outcomes. It mines up to the first 2,048 bytes of each actual event
in session order. A template group is used only when it combines at least two
of those candidates and its displayed template costs fewer bytes than the
suppressed evidence excerpts. Grouping preserves BM25 scores and the highest
ranked representative; it never reranks results.

Explicit outcome fields (including success, error, exit code, status, job result,
and firewall decision) partition groups, so success and failure counts cannot
be combined. `occurrences` counts all events in that outcome-specific group;
`matching_occurrences` counts its query-matching events. Ungrouped events and
exact-duplicate groups omit `cluster_id`, `template`, and `anomaly_score`.
Drain3 IDs are scoped to the run-local stage and are not stable across queries
or indexes.

Each result includes up to three matching source references, a representative
event, and its preceding and following session events. References retain JSONL
line numbers, event IDs, timestamps, and original artifact paths/indexes when
present.

`bm25_score` measures query relevance; `anomaly_score` separately describes
Drain3's online assessment when the representative event was ingested, when
grouping is used (zero scores may be omitted). Neither
score determines finding severity, confidence, audit completeness, or threat
absence. Zero matches do not mean a run is safe. Matches are relevance-ordered;
their references and neighboring context preserve session chronology.

Returned excerpts are capped at 600 bytes each and template/excerpt text at
16 KiB overall. `text_bytes`, report-level `truncated`, and per-excerpt
`truncated` indicate these bounds. Search results and query text are never
written into the shared audit cache; each query reads the unified session anew.

**Single-run examples:**

```bash
gh aw audit 1234567890
gh aw audit https://github.com/owner/repo/actions/runs/1234567890
gh aw audit 1234567890 --parse --json
gh aw audit 1234567890 --no-baseline
gh aw audit 1234567890 -o ./audit-reports --repo owner/repo
```

**Stdin mode:**

Use `--stdin` to pass run IDs or URLs from a file or pipeline (mutually exclusive with positional arguments). Blank lines and `#` lines are ignored. Bare numeric IDs require `--repo owner/repo`.

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
gh aw audit 12345 12346 --group --json         # Grouped findings as JSON
```

**Single-run report sections**: Overview, Comparison, Task/Domain, Behavior Fingerprint, Agentic Assessments, Metrics, Model Routing, Friction, Key Findings, Recommendations, Observability Insights, Performance Metrics, Engine Config, Prompt Analysis, Session Analysis, Safe Output Summary, MCP Server Health, Jobs, Downloaded Files, Missing Tools, Missing Data, Noops, MCP Failures, Gateway Steering Events, Firewall Analysis, Policy Analysis, Redacted Domains, Errors, Warnings, Tool Usage, MCP Tool Usage, Created Items, Graders, Evals.

When routing data is available, Model Routing summarizes the classifier and selected route, ranked alternatives, router version and latency, request outcomes and deviations, and classifier/selected/deviated token and AIC costs. Older AWF endpoint-only deviations are attributed to the selected route. The Comparison section identifies changes in selected model, effort, mode, or router version even when overall cost is stable.

The Friction section attributes execution-friction costs by driver and source. In JSON, `friction.drivers[]` provides each driver's `source`, counted and deduplicated occurrences, attribution state, and cost dimensions; `friction.events[]` provides the corresponding event-level source and label. Console output includes the source and available AIC, token, turn, tool-call, and latency costs on each driver and event, so a high-cost source such as `firewall`, `mcp_gateway`, or `agent_session` can be investigated directly. Unavailable dimensions are omitted rather than presented as measured zeroes.

The Gateway Steering Events section reports `token_steering` and `timeout_steering` warnings emitted when a run approaches its AI Credits or time limit. JSON output includes each event's type, message, and timestamp when available.

The Work Queue section reports the activation-time queue snapshot, the assigned worker's Work and Claim IDs, the agent's finish intent, and timestamped queue operations from workflow job logs. The section is omitted when no queue evidence is available. A finish intent is a request, not evidence of durable Completion or verified delivery.

For version-3 snapshots, `work_queue.snapshot.current.diagnostics` contains the same metadata-only counters as [`work-queue stats`](/gh-aw/reference/work-queue/#structured-diagnostics): Claim outcomes and cancellation reasons, pending/verified/failed delivery and failure reasons, retained reservations versus historical dispatches, per-pool eligibility and compact predictions, trusted admission ages, and 24-hour ledger activity. Its `status` is `captured_snapshot` and `evaluated_at` equals `captured_at`; ages, activity and predictions use that captured time, never the time of the audit. `branch` is empty because activation artifacts do not record its name. No live queue or Actions state is fetched to produce these diagnostics.

`diagnostics.assigned_claims` adds structured `work` and `claim` metadata for the worker's original assignment, including admission/grant/terminal timestamps, attempts, retry boundaries, and delivery-failure reason and disposition. `work` describes its state at capture; `claim` describes the original Claim, which can be historical. Payloads, Result descriptors, request parameters, receipts, and actor principals are excluded. Dispatcher and observer snapshots have aggregate diagnostics but no assigned Claims; an uninitialized observer has zero counts and no installed pools. Console output bounds pool and Claim detail rows; JSON retains the full metadata.

An activation snapshot cannot prove the audited run's eventual outcome. Later finish intents and `work_queue.operations` do not advance its counters or delivery barriers. Completion is not verified delivery, released dispatch state is historical rather than live worker status, and unknown effects must not be replayed. Historical version-2 snapshots remain readable in `work_queue.snapshot.transactions`; their summaries are not inferred using version-3 rules.

Operations are accepted only from the compiler-owned snapshot, finish-intent-copy, and claim-reconciliation step logs, not from agent stdout or whole-job log copies. Incomplete workflow-log downloads are retried before their operations are reported. Text reports escape control characters in stored identifiers.

Runs using the previous queue artifact filenames and step/message names remain readable. New filenames take precedence; invalid new artifacts are reported as errors rather than falling back to an older copy.

`audit` downloads this evidence by default. For `logs`, which defaults to compact usage downloads, select `work-queue` (activation and agent artifacts plus workflow logs) or `all`. Both commands include `work_queue` in their JSON reports, per run for `logs`, and preserve it in cached summaries. Usage-only logs omit the section even when a previous request cached work-queue evidence:

```bash
gh aw logs 1234567890 --artifacts work-queue --json
gh aw audit 1234567890 --artifacts work-queue --json
```

The Observability Insights section includes `skill_activations` when skill-invocation evidence is found. Each entry reports the skill name, `status` (`invoked`), the detection `source` (`agent_output` or `log_parse`), and provenance fields in JSON output. This distinguishes skills that were merely installed from those actually invoked.

The Graders section is present when the run recorded deterministic grader results.
Ten fast built-ins run by default in conclusion, using the redacted in-memory
unified session. The `graders` object in JSON output lists each grader (`id`,
`name`, `status`, `value`, `unit`, `passed`, and, when declared in the manifest,
`direction` and `threshold`) plus aggregate counts: `total`, `passed`, `failed`,
`error_count`, and `unavailable_count`. Results are read from the compact `usage`
artifact, including with `--artifacts usage`; historical agent and fallback
artifact layouts remain readable. The same object appears per run in
`gh aw logs --json`.

When the usage manifest registers custom grader IDs, audit also downloads the
optional standalone `graders` artifact and uses its combined built-in/custom
results, even with `--artifacts usage`. Built-in-only runs make no extra request.
Cached reports are refreshed when custom results become available. Missing
optional artifacts retain built-in results; discovery/download failures are
reported. Audit reads results as data and never executes custom graders.

### Evals summary

When evals results are available, the Evals section summarizes the recorded
answers. The top-level `evals` object in JSON includes `questions`, `yes`, `no`,
and `unknown` counts, plus `execution_state` when execution evidence is available.
Counts combine the run's evals invocations without counting duplicate artifact
copies. An unavailable or conflicting execution state is omitted.

For example, a judge that never started can produce:

```json
{
  "evals": {
    "questions": 2,
    "yes": 0,
    "no": 0,
    "unknown": 2,
    "execution_state": "not_started"
  }
}
```

`UNKNOWN` answers do not establish that judging completed. Review execution
evidence and findings alongside the answer counts; a successful Actions
conclusion alone does not establish judge or threat-detection success.

### Audit finding codes

Every `key_findings` entry includes a stable `code` for downstream automation. Titles and descriptions are human-readable and may change; consumers must use `code` to identify a finding type. Existing codes are not repurposed.

The top-level `schema_version` identifies the audit cache and JSON contract version. Cached reports with an older version are regenerated before use.

Parent workflow success does not imply successful delegation. `metrics.error_count` retains its workflow-error meaning; `metrics.subagent_failure_count` separately counts observed failed sub-agent invocations and is omitted when zero. A successful run with failed delegation retains its Actions conclusion and `workflow_succeeded` finding, but the finding qualifies parent success, includes the failed invocation count, and recommends reviewing recovery. Failed invocations remain counted even when a later retry succeeds; parent success alone does not establish recovery.

| Code | Finding |
|---|---|
| `workflow_failed` | The workflow failed |
| `workflow_timeout` | The workflow timed out |
| `high_token_usage` | Token usage exceeded the audit threshold |
| `many_iterations` | The run exceeded the turn threshold |
| `multiple_errors` | The run produced more than five errors |
| `mcp_server_failures` | One or more MCP servers failed |
| `tools_not_available` | Requested tools were unavailable |
| `model_unavailable` | Model resolution stopped startup and the same run's complete provider inventory confirms the model was not advertised |
| `model_availability_unknown` | Model resolution stopped startup, but saved evidence cannot confirm model absence or records another discovery failure |
| `model_resolution_failed` | Low-severity advisory: failed model resolution was recorded, but evidence does not establish it as the startup failure cause |
| `model_endpoint_unsupported` | Startup recorded `unsupported_endpoint`: the model is live, but the engine cannot call its endpoint; choose a compatible model |
| `blocked_network_requests` | The firewall blocked network requests |
| `workflow_succeeded` | The parent workflow completed successfully; the description qualifies any observed delegated failures |
| `subagent_failed` | One or more invocations of a sub-agent failed, regardless of the parent conclusion |
| `threat_detection_job_failed` | The `detection` job failed |
| `engine_step_failed` | An evals judge or threat-detection engine step failed, including failures hidden by `continue-on-error` |
| `threat_detected` | The detection artifact identified one or more threats |
| `agentic_resource_heavy_for_domain` | Resource use was high for the inferred task domain |
| `agentic_overkill_for_agentic` | The task may not require an agentic workflow |
| `agentic_poor_agentic_control` | Agentic control signals were weak |
| `agentic_partially_reducible` | Part of the task could use deterministic steps |
| `agentic_model_downgrade_available` | A smaller model may be sufficient |
| `agentic_delegated_context_present` | Delegated workflow context was preserved |

The audit inspects the `detection` job result and the downloaded `detection` artifact. A failed job emits `threat_detection_job_failed`. A structured verdict in `detection_result.json`, or a legacy `THREAT_DETECTION_RESULT` verdict in `detection.log`, emits `threat_detected` when `prompt_injection`, `secret_leak`, or `malicious_patch` is true. Threat reasons are not copied into the finding description because they may contain sensitive agent output.

`engine_step_failed` is a high-severity finding based on a nonzero process exit
in the evals or detection engine step's downloaded log, or `not_started` execution
evidence for an evals job that ran. It includes the job, available step details,
and a bounded, sanitized diagnostic. The finding can coexist with
`workflow_succeeded`: it does not change the Actions conclusion, and a detection
execution failure is distinct from a detected threat. Missing step evidence
does not establish successful execution.

Audit reads failed `model.resolution` events from `usage/aw_session.jsonl` across
engines. It attributes a role's resolution failure to startup only when the run
failed, `agent.execution` exited nonzero, no native agent session activity is
present, and `agent-stdio.log` records a harness refusal or model-specific
resolution error for that role. Otherwise, it emits `model_resolution_failed`;
optional repository agents can record unresolved models without stopping the run.
Pi's legacy `not advertised` log check applies only when the unified session has
no failed resolution records.

For recorded `model_unavailable` failures, audit checks the exact model ID against
the matching configured provider in the same run's `awf-reflect.json`. It confirms
absence only when discovery completed and an array inventory is available.
Missing, malformed, incomplete, ambiguous, or contradictory evidence produces
`model_availability_unknown`, not a claim that the model was removed.
Findings list up to five advertised same-family candidates and their known
supported endpoints. These are inspection candidates, not guaranteed replacements.
Recommendations use `gh aw models --refresh-observed=false --logs-dir <audit-output-dir>`
to inspect cached model evidence without downloading more runs. Audit does not
change model pins or infer global retirement from one run's inventory.
Each finding includes a `playbook_url` in JSON and a playbook link in console
output. Model and sub-agent findings link to specialized guidance; other findings
link to the diagnostic code playbook. Actions runtime error detection and
safe-output diagnostics also print the relevant playbook URL.

The Metrics section includes an `ambient_context` object capturing the first LLM inference footprint. It is absent when neither `token-usage.jsonl` nor the fallback `agent_usage.json` is in the downloaded artifacts (common for older runs and runs without firewall/usage artifacts):
- `ambient_context.input_tokens` — input tokens for the first invocation
- `ambient_context.cached_tokens` — cache-read tokens reused by the first invocation

The Metrics section and JSON output also include `working_set` from the compact usage activity summary. When measured, human-readable output shows `working-set-rebuild=<factor>×`; JSON preserves the measurement state, factor, cumulative and peak input tokens, rebuild excess, and invocation count. Diff output compares measured factors without assigning a success or failure interpretation.

Working-Set Rebuild Factor measures cumulative context reconstruction relative to peak invocation context. It is an efficiency/trajectory metric, not a measurement of semantic coherence debt and not a predictor of task success. Equal factors can occur on successful and failed runs, and missing required facts cannot be inferred from the value.

**Diff output** includes network changes (new, removed, and allow/deny flips), anomaly flags, MCP tool invocation changes, run-level metric deltas, token and AIC breakdowns, tokens per turn, per-tool call counts with max input/output sizes, aggregated bash command usage, and model-routing changes including effective and selected endpoints.

With multiple comparisons, `--json` emits a single object for one comparison or an array for many, while `--format pretty` and `--format markdown` separate each diff with dividers. With `--group`, audit emits `runs_analyzed`, `entries`, and (when applicable) `skipped_runs`; each entry contains `run_id`, `code`, `occurrences`, and `representative_entry`. Only actionable findings (severity of low or above) are grouped. Runs excluded by `--experiment`, `--runtime`, or `--evals`, or whose audit data could not be loaded, are listed in `skipped_runs` and excluded from `runs_analyzed`.

Audit also persists extracted skill-activation data into `run_summary.json` for downstream automation.

## JSON output schemas

`gh aw json-schema` writes a JSON Schema to stdout: `audit` for `gh aw audit --json`, `logs` for `gh aw logs --json`, and `logs-jsonl` for each item of `gh aw logs --cached-jsonl`.

```bash
gh aw json-schema audit > audit.schema.json
gh aw json-schema logs > logs.schema.json
gh aw json-schema logs-jsonl > logs-jsonl.schema.json
```

`make recompile` regenerates the checked-in schemas (including `schemas/logs-jsonl.schema.json`) from the Go output types.

When routing records are available, single-run `audit --json` includes `model_routing` with status, objective, labels, classifier and selected model details, ranked choices, router metadata, effective and selected endpoints, request outcome/deviation counts, failure details, and classifier/selected/deviated cost buckets. Logs JSON includes `model_routing.routes` entries keyed by labels, mode, model, effort, router version, and endpoint attribution, with `run_count`, `total_aic`, and `average_aic`, plus classifier AIC and deviation totals.

## `gh aw logs --format <fmt>`

Generates a cross-run security and performance audit report across recent runs, built into `gh aw logs`.
When routed runs are present, the report's Model Routing section groups labels and mode by selected model, effort, router version, and endpoint attribution, with run counts and total/average AIC. It also reports classifier AIC and the share of requests marked deviated. The same aggregate is available as `model_routing` in `--json` output.

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

Detailed logs JSON includes a per-run `ambient_context` (first LLM invocation only) under the same availability conditions.

Detailed logs JSON output includes the same `working_set` object when the usage activity summary is available. The default `gh aw logs` runs table (both the compact agent-optimized format and the verbose `-v` format) also surfaces a single `WSRF` column with the rebuild factor rounded to two decimal places, showing `-` when the metric was not measured for that run.

**`--stdin` mode:** Pass `--stdin` to supply an explicit list of run IDs or URLs instead of letting the command discover runs from the GitHub API. Date, count, and workflow-name filters are ignored; `--engine`, `--firewall`, `--safe-output`, and other content filters still apply. Blank lines and `#`-prefixed lines are ignored. Bare numeric IDs require `--repo owner/repo`.

```bash
cat run-ids.txt | gh aw logs --stdin --repo owner/repo
```

**Examples:**

```bash
gh aw logs --format markdown
gh aw logs daily-repo-status --format markdown --count 10
gh aw logs agent-task --format markdown --last 5 --json
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
