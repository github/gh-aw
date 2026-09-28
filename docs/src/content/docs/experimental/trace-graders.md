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

### Trajectory graders

These built-ins are deterministic projections over the canonical [Trajectory IR](https://github.com/github/gh-aw/blob/main/.github/workflows/shared/graders/trajectory-ir.md) (`trace.trajectoryIR`, `trace.ir`, or `agentOutput.trajectory`). They run in-process with no API calls. When the IR data a grader needs is missing, the grader reports `unavailable` instead of a score. Any IR collection with more than 5,000 items is also reported as `unavailable`, which bounds the cost of the quadratic graders.

| ID | Description | Value |
|---|---|---|
| `policy-near-miss` | Fraction of successful traces that left guard or policy objectives unsatisfied | 0–1 |
| `skill-constraint-coverage` | Fraction of configured skill constraints that were exercised and passed | 0–1 |
| `exploration-error` | Unmet objectives attributable to insufficient search | 0–1 |
| `exploitation-error` | Unmet objectives attributable to gathered evidence that was never used | 0–1 |
| `state-revisit-probability-rep` | Fraction of canonical state visits that revisit an already visited state | 0–1 |
| `recurrence-determinism` | RQA DET: fraction of recurrent points forming diagonal (repeated-subsequence) lines | 0–1 |
| `recurrence-laminarity` | RQA LAM: fraction of recurrent points forming vertical (stagnation) lines | 0–1 |
| `recurrence-trapping-time` | RQA TT: average length of vertical recurrence lines | ≥0 |
| `recurrence-rate` | RQA RR: density of recurrent state pairs across the run | 0–1 |
| `event-entropy-rate` | Normalized conditional Shannon entropy rate of the ordered event sequence | 0–1 |
| `lempel-ziv-trajectory-complexity` | Normalized LZ76 complexity of the canonical event sequence | 0–1 |
| `tool-output-consumption-rate` | Fraction of tool outputs referenced by a later action | 0–1 |
| `end-to-end-lineage-completeness` | Fraction of final outputs traceable to tool or observation evidence roots | 0–1 |
| `action-provenance-coverage` | Fraction of consequential actions with a provenance path to tool or observation evidence | 0–1 |
| `premature-termination-gap` | Declared completion conditions still unsatisfied at termination | integer |
| `evidence-saturation-stopping-lag` | Events elapsed between all objectives being satisfied and the run stopping | integer |
| `dependency-order-violation-rate` | Fraction of dependent objectives satisfied before their prerequisites | 0–1 |
| `objective-coverage` | Fraction of declared objectives that were completed | 0–1 |
| `grounding-accuracy` | Fraction of actions that were valid in the state in which they were issued | 0–1 |
| `tool-wise-score` | Longest correct execution prefix against a reference trajectory, with parameter credit | 0–1 |
| `trajectory-ndtw` | Normalized dynamic time warping similarity to a reference state trajectory | 0–1 |
| `code-search-recall` | Fraction of reference patch files located during the run | 0–1 |

`skill-constraint-coverage` reads its constraints from `config.constraints`:

```yaml
graders:
  skill-constraint-coverage:
    config:
      constraints:
        - id: uses-docs-skill
          pattern: "skill .*documentation"
```

Every built-in grader runs on every trace whenever the graders step runs, even if the workflow lists only custom graders. To opt out of a built-in, set `enabled: false` for it.

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

`trace.toolCalls` merges native agent tool calls parsed from the Copilot `events.jsonl` session log with the MCP gateway records. Native entries always expose `name` and `source: "agent"`, plus `arguments`, `toolCallId`, `mcpServerName`, and `mcpToolName` when the underlying event provides them, so built-in tools such as `skill` are visible to graders. A matching `tool.execution_complete` event sets `success` and `completed: true`; calls that never complete keep `completed: false` and no `success` value, and are scored as failures by the built-in `tool-success-rate` and `tool-failure-count` graders. Gateway records that duplicate a native call (same tool name and arguments) are dropped, and `trace.nativeToolCalls` holds the native records on their own.

## Operational value grader

Configure the reserved `operational-value` grader with either inline Bash:

```aw wrap
graders:
  operational-value:
    name: Maintainer Time Saved
    description: Maintainer effort avoided by the current run's accepted outcome
    unit: hours
    direction: higher_is_better
    script: |
      #!/usr/bin/env bash
      set -euo pipefail
      request=$(cat)
      printf '%s\n' '[{"id":"maintainer-hours-saved","value":2.5}]'
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

The evaluator runs once with no arguments. It reads a request containing `schemaVersion`, `run`, `event`, `outputs`, and `config` from standard input. `outputs` contains the current run's validated safe-output requests; those requests do not prove that their GitHub mutations were applied. The evaluator writes one non-empty ordered array of `{id,value}` metrics. The first metric is primary; later metrics are diagnostics. Values are finite numbers or `null`. Numeric values retain their native scale; gh-aw does not normalize, clamp, or convert them to pass/fail. Use `unit` and `direction` to describe their meaning.

Evaluators receive `GH_TOKEN` with the agent job's explicitly declared permissions, but no workflow secrets, and enabling the grader does not add evidence permissions to the agent job.

Use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from an agentic workflow and design and verify an operational-value evaluator.

## Output files

| File | Description |
|---|---|
| `grader_manifest.json` | Which graders were configured and their enabled state |
| `grader_results.json` | Validated raw values, status, implementation identity, and ordered operational-value metrics |
| `operational_value_evaluator.sh` | Exact frozen operational-value evaluator used for this run |

All files are included in the unified `agent` artifact.

## Execution

The graders step runs as an `if: always()` post-agent step in the existing agent job, after log parsing and before the unified artifact upload. It uses a single preprocessing pass over trace files shared by all graders.
