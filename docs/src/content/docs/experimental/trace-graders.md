---
title: Graders
description: Deterministic execution and operational value metrics
---

Graders compute deterministic metrics without LLM calls. Built-in and custom inline graders inspect post-agent execution traces. The reserved `operational-value` grader evaluates operational repository outcomes under a frozen evaluator and explicit evidence cutoff. Results are persisted in the agent artifact for downstream tools.

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

Configure the reserved `operational-value` grader with either inline Bash:

```aw wrap
graders:
  operational-value:
    script: |
      #!/usr/bin/env bash
      set -euo pipefail
      request=$(cat)
      printf '%s\n' '[{"id":"goal-attained","value":1}]'
```

or a repository-relative Bash evaluator:

```aw wrap
graders:
  operational-value:
    run: .github/graders/daily-file-diet-operational-value.sh
```

Specify exactly one of `script` or `run`. The compiler freezes the evaluator bytes, records their SHA-256 digest, and packages them with the run. Inline Bash is verified against the workflow Markdown at the run commit; file-backed Bash is verified against its file at that commit.

The evaluator runs once with no arguments. It reads a request containing `schemaVersion`, `run`, `event`, and `config` from standard input and writes one non-empty ordered array of `{id,value}` metrics. The first metric is primary; later metrics are diagnostics. Values are finite numbers in `[0,1]` or `null`.

Evaluators receive `GH_TOKEN` with the agent job's explicitly declared permissions, but no workflow secrets, and enabling the grader does not add evidence permissions to the agent job.

Use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from an agentic workflow and design and verify an operational-value evaluator.

## Output files

| File | Description |
|---|---|
| `grader_manifest.json` | Which graders were configured and their enabled state |
| `grader_results.json` | Normalized values, status, implementation identity, and ordered operational-value metrics |
| `operational_value_evaluator.sh` | Exact frozen operational-value evaluator used for this run |

All files are included in the unified `agent` artifact.

## Execution

The graders step runs as an `if: always()` post-agent step in the existing agent job, after log parsing and before the unified artifact upload. It uses a single preprocessing pass over trace files shared by all graders.
