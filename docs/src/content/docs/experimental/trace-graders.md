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

Configure the reserved `operational-value` grader with a repository-relative Bash evaluator:

```aw wrap
graders:
  operational-value:
    run: .github/graders/daily-file-diet-operational-value.sh
```

The compiler freezes the evaluator bytes and records their SHA-256 digest. The evaluator returns absolute operational attainment in `[0,1]` for the run's assigned case. A frozen baseline is optional metadata; when present, gh-aw derives `deltaFromBaseline` without changing the primary value.

Each result records the complete run subject, operational case, evidence time, maturity, and provenance. Operational-value evaluators may query the repositories declared by their frozen evidence contract. They receive the workflow token through `GH_TOKEN` with the agent job's explicitly declared permissions, but do not receive workflow secrets. Enabling the grader does not add evidence permissions to the agent job.

Use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from an agentic workflow and design and verify an operational-value evaluator.

### Regrade a historical run

```bash
gh aw graders operational-value 123456789 \
  --evidence-at 2026-08-30T12:00:00.000Z \
  --json
```

The command downloads the original grader artifact and reuses its case, run subject, and frozen evaluator. The archived evaluator must match the digest recorded by both the original manifest and result and the evaluator at the recorded commit in the current repository checkout. Regrading emits a new observation identified by `(runId, evaluatorDigest, evidenceAt)` and never modifies the original artifact. Use `--repo [HOST/]OWNER/REPO` to select the host for the checked-out repository.

### Build a historical report

```bash
gh aw graders operational-value report daily-file-diet
```

The report command discovers every completed workflow run from the evaluator's declared adoption time through the current time. It applies the current evaluator digest to every run, including runs created before grader artifacts existed, and writes a structured JSON report, an SVG timeline, and a Markdown report under `reports/operational-value`. The versioned JSON is the machine-readable integration contract. Each report observation has the stable identity `(repository, workflowId, runId, runAttempt, evaluatorDigest)`.

Pre-grader runs do not have an archived case or event payload. Their evaluator request contains the run ID, attempt, repository, workflow, ref, commit SHA, event name, creation time, and `case: null`. The evaluator must reconstruct the case from that run subject. A result remains explicitly unavailable when the accepted evidence cannot reconstruct it; missing evidence is never scored as zero.

Mature numeric observations are cached in Monday-based UTC weekly files under the user cache directory. Cache paths are partitioned by repository, workflow ID, evaluator digest, and week. Independent weeks are evaluated concurrently; use `--concurrency` to control the number of evaluator executions (the default is 8). Use `--refresh` to replay every run, `--until` to set an evidence endpoint, `--cache-dir` to relocate the cache, and `--output` to relocate the report artifacts.

Every run remains present in the JSON report and timeline. Weekly primary means retain only the latest observation for each repeated `opportunityKey` within that week. Evaluators may also declare multiple normalized diagnostic metrics with `latest` or `mean` weekly aggregation; the report plots and tabulates each diagnostic independently without combining it into the primary value. The report includes coverage, errors, frozen contract details, baseline and delta when available, and a warning that the observations do not establish causation.

The SVG and Markdown files are standalone local exports, not historical storage or source fixtures. Do not commit live report output merely to retain observations. Consumers such as Central Agentic Ops should ingest the JSON contract and own their presentation. The weekly user cache accelerates replay but is not authoritative; deleting it causes gh-aw to rebuild observations from target-repository run metadata and accepted evidence.

## Output files

| File | Description |
|---|---|
| `grader_manifest.json` | Which graders were configured and their enabled state |
| `grader_results.json` | Normalized values, status, implementation identity, and value observations |
| `operational_value_evaluator.sh` | Exact frozen operational-value evaluator used for initial grading and historical replay |

All files are included in the unified `agent` artifact.

## Execution

The graders step runs as an `if: always()` post-agent step in the existing agent job, after log parsing and before the unified artifact upload. It uses a single preprocessing pass over trace files shared by all graders.
