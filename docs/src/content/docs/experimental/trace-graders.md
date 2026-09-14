---
title: Graders
description: Deterministic execution and operational value metrics
---

Graders compute deterministic metrics without LLM calls. Built-in and custom inline graders inspect post-agent execution traces. The reserved `operational-value` grader evaluates the current run's operational outcome under a frozen evaluator. Results are persisted in the agent artifact for downstream tools.

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
    name: Goal Attainment
    description: Whether the current run attained the workflow's intended outcome
    unit: ratio
    direction: higher_is_better
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
    name: File Diet Decision Conformance
    description: Whether the run requested the correct refactoring issue or noop
    unit: ratio
    direction: higher_is_better
    run: .github/graders/daily-file-diet-operational-value.sh
```

Specify exactly one of `script` or `run`. Give the primary metric a concise `name`, `description`, `unit`, and `direction`; its description is retained in grader artifacts. Document every emitted metric in evaluator comments so its meaning is frozen with the evaluator bytes. The compiler records the evaluator's SHA-256 digest and packages its exact bytes with the run.

The evaluator runs once with no arguments. It reads a request containing `schemaVersion`, `run`, `event`, `outputs`, and `config` from standard input. `outputs` contains the current run's validated safe-output requests; those requests do not prove that their GitHub mutations were applied. The evaluator writes one non-empty ordered array of `{id,value}` metrics. The first metric is primary; later metrics are diagnostics. Values are finite numbers in `[0,1]` or `null`.

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
