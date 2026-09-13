---
title: Graders
description: Deterministic execution and operational value metrics
---

Graders compute deterministic metrics without LLM calls. Built-in and custom inline graders inspect post-agent execution traces. The reserved `operational-value` grader runs a frozen repository evaluator that defines domain-specific outcome metrics. Results are persisted in the agent artifact for downstream tools.

For normative requirements, see the [Graders Specification](/gh-aw/specs/graders-specification/).

:::caution[Experimental]
Graders are an experimental feature.
:::

## Quick start

```yaml
graders: {}
```

An empty map enables all built-in graders with default settings. Omitting the `graders` field entirely disables grading (no step is emitted).

## Built-in graders

| ID | Description | Value |
|---|---|---|
| `tool-success-rate` | Fraction of tool calls that succeeded | 0–1 |
| `tool-failure-count` | Number of failed tool calls | integer |
| `retries` | Count of retry events in MCP gateway logs | integer |
| `loops` | Consecutive identical tool calls (same name + args) | integer |
| `trajectory-efficiency` | Unique tool names / total tool calls | 0–1 |
| `execution-step-count` | Total LLM request count | integer |
| `execution-duration` | Total execution duration (ms) | integer |
| `working-set-rebuild-factor` | Cumulative input tokens / peak invocation input tokens | ≥1 |
| `context-growth` | Total tokens / first-request tokens | ≥1 |
| `artifact-production` | Count of outputs in agent_output.json | integer |

## Selective configuration

Disable a specific built-in:

```yaml
graders:
  loops:
    enabled: false
```

## Custom inline graders

Add a trusted inline JavaScript expression that receives the preprocessed `trace` object:

```yaml
graders:
  bash-calls:
    script: "return trace.toolCalls.filter(t => t.name === 'bash').length"
```

Custom scripts must return a value and stay within 4096 characters (no `require`, `import`, `fetch`, `eval`, or `process.exit`).

## Operational value grader

Configure the reserved `operational-value` grader with a repository-relative Bash evaluator:

```aw wrap
graders:
  operational-value:
    run: .github/graders/daily-file-diet-operational-value.sh
```

The compiler freezes the evaluator bytes and records their SHA-256 digest. At runtime, gh-aw invokes it once with no arguments and passes run, event, and grader configuration JSON on standard input. The evaluator returns a non-empty ordered array of domain-named metrics:

```json
[
  {"id": "large-file-triage-output", "value": 1},
  {"id": "repository-health", "value": 0.8}
]
```

Each ID must be non-empty and unique. Values must be finite numbers in `[0,1]`, or `null` when evidence is unavailable. The first metric is primary; later metrics are optional diagnostics. gh-aw preserves the array as `metrics` and exposes its first value as the grader's top-level `value` for thresholds and experiments.

Evaluators receive `GH_TOKEN` with the agent job's explicitly declared permissions, but no workflow secrets. Prefer the event payload, local checkout, and agent output when they contain the required evidence. Use the GitHub API when the intended outcome cannot be verified locally; enabling the grader does not add permissions automatically.

Use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from an agentic workflow and design and verify an operational-value evaluator.

Use `gh aw graders run WORKFLOW-NAME operational-value` with a JSON payload on standard input to exercise the configured evaluator locally. The operational value designer skill includes a verifier for its input/output contract.

## Output files

| File | Description |
|---|---|
| `grader_manifest.json` | Which graders were configured and their enabled state |
| `grader_results.json` | Normalized values, status, implementation identity, and named operational metrics |
| `operational_value_evaluator.sh` | Exact frozen operational-value evaluator used by the run |

All files are included in the unified `agent` artifact.

## Execution

The graders step runs as an `if: always()` post-agent step in the existing agent job, after log parsing and before the unified artifact upload. It uses a single preprocessing pass over trace files shared by all graders.
