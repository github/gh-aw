---
title: Graders
description: Pure deterministic metrics from the unified agent session
---

Built-in and JavaScript graders compute metrics without model calls, file reads, subprocesses, or
network access. Built-ins run in the **conclusion job**, after observations from
agent execution, safe outputs, experiments, and evals have been collected and
redacted in memory. Custom JavaScript runs only in a separate **graders job**
after conclusion publishes that session. Repository-owned operational-value Bash
evaluators also run only in that separate job, using a session-file interface.

:::caution[Experimental]
Graders and their result format are experimental.
:::

## Default graders

No configuration is required. Omitting `graders` or setting `graders: {}` enables
these ten fast, linear-time built-ins:

| ID | Measurement |
|---|---|
| `tool-success-rate` | Fraction of tool calls that succeeded |
| `tool-failure-count` | Failed or unfinished tool calls |
| `retries` | Retry observations in main-agent MCP events |
| `loops` | Consecutive identical tool names and arguments |
| `trajectory-efficiency` | Unique tool names / total calls |
| `execution-step-count` | Observed model request count |
| `execution-duration` | Observed duration in milliseconds |
| `working-set-rebuild-factor` | Cumulative input tokens / peak invocation input tokens |
| `context-growth` | Total tokens / first-request tokens |
| `artifact-production` | Number of safe-output results |

Detection and eval usage do not count toward main-agent execution statistics.
Native tool calls take precedence over duplicate gateway records, and proxy
token telemetry takes precedence over aggregate usage. Missing required
evidence yields `unavailable` rather than an invented score.

## Configuration

Override thresholds, disable defaults, or enable additional registered built-ins:

```yaml
graders:
  loops:
    enabled: false
  tool-success-rate:
    threshold: 0.95
  skill-constraint-coverage:
    config:
      constraints:
        - id: uses-docs-skill
          pattern: "skill .*documentation"
```

Supported fields are `enabled`, `name`, `description`, `unit`, `direction`,
`threshold`, `min`, `max`, and `config`. Disabling every default is valid.
Custom IDs additionally require `script`. Built-in implementations cannot be
overridden. Only the reserved `operational-value` ID accepts a Bash `run` path
instead of JavaScript `script`.

## Custom JavaScript

Use a distinct ID and a synchronous JavaScript body:

```yaml
graders:
  documentation-reads:
    unit: calls
    direction: higher_is_better
    threshold: 1
    script: |
      const { toolCalls } = helpers.createSessionLookups(session);
      return toolCalls.filter(call => call.name === "view").length;
```

Scripts receive `session`, `config`, and `helpers.createSessionLookups(session)`.
The helper exposes `stats`, `eventsOfType(type)`, `toolCalls`, scoped
`toolCallsById`, and a session-derived trajectory projection (`trace`).
Inputs are immutable snapshots. Return a finite number, `null` for unavailable
evidence, or an object with `value` and optional `message`, `details`, and
`diagnostics`. Async results are rejected. Scripts are limited to 256 KiB.

The compiler generates a fresh hosted job with read-only repository access,
no inherited deployment environment, and no inference, App, or OIDC credentials.
Each script runs in its own restricted Node process with an empty environment,
bounded memory/output, a five-second VM timeout, and a seven-second process
timeout. File, network, process, clock, random, module, and timer APIs are not
exposed. A VM alone is not a security boundary; job and process isolation also
limit authority. Work-queue participant workflows do not publish this input and
cannot register custom graders.

Conclusion receives only custom IDs, never executable grader source.
User-provided conclusion steps and customization of `jobs.graders` are rejected.

## Operational-value Bash

Reuse a repository-owned evaluator after replacing its old stdin request with
the unified-session JSONL file:

```yaml
graders:
  operational-value:
    run: .github/graders/score.sh
```

```bash title=".github/graders/score.sh"
#!/usr/bin/env bash
set -euo pipefail
session_file=$1 # Also available as GH_AW_SESSION_FILE.
jq -s '[{id: "recorded-tool-failures", value:
  ([.[] | select(.type == "tool.execution_complete" and .data.success == false)] | length)
}]' "$session_file"
```

This diagnostic example measures recorded failures, not semantic task success.
Configuration is JSON in `GH_AW_GRADER_CONFIG`. Stdin is empty. Keep existing
nonempty `[{id,value}, ...]` output: the first metric becomes the primary
`operational-value` value; subsequent metrics remain diagnostics. Use `null`
when required evidence is absent.

`run` is a repository-root-relative `.sh` file path, not an inline shell command.
`./` paths are relative to the declaring workflow. Source must be UTF-8, have a
Bash shebang, and fit within 64 KiB. The compiler freezes it into the separate
job, so runtime checkout is unnecessary. `gh aw add` and packages install the
referenced evaluator resource.

The runtime starts Bash without profiles or inherited credentials, in a temporary
working directory, with a 120-second deadline and 1 MiB output limit. Compilation
and the default `grader-shell-file-write` linter reject detected file writes and
unchecked dynamic execution. Read the session, compute in memory, and print metrics;
remove temporary-file writes, checkout/API fetches, and mutation commands.

> [!CAUTION]
> Shell evaluators are trusted repository code, not a filesystem/network sandbox.
> Static write detection cannot prove arbitrary shell safety. MicroVM isolation
> is future work. Never evaluate session text as shell code or claim missing
> evidence was observed.

## Opt-in trajectory graders

These built-ins are not enabled by default:

| Family | IDs |
|---|---|
| Policy and constraints | `policy-near-miss`, `skill-constraint-coverage` |
| Exploration | `exploration-error`, `exploitation-error` |
| Recurrence | `state-revisit-probability-rep`, `recurrence-rate`, `recurrence-determinism`, `recurrence-laminarity`, `recurrence-trapping-time` |
| Sequence complexity | `event-entropy-rate`, `lempel-ziv-trajectory-complexity` |
| Provenance | `tool-output-consumption-rate`, `end-to-end-lineage-completeness`, `action-provenance-coverage` |
| Objectives | `premature-termination-gap`, `evidence-saturation-stopping-lag`, `dependency-order-violation-rate`, `objective-coverage`, `grounding-accuracy` |
| Reference comparison | `tool-wise-score`, `trajectory-ndtw`, `code-search-recall`, `code-read-precision`, `code-edit-precision` |
| Process model | `process-alignment-fitness` |

All use the same in-memory session interface. Internal projections cannot add
evidence absent from that session: analyses needing objectives, state,
references, or lineage report `unavailable` when those observations are missing.
Trajectory collections are bounded to 5,000 items for expensive analyses.
Importable fragments under `.github/workflows/shared/graders/` configure these
built-ins; they do not contain executable custom code.

## Output and replay

The `usage` artifact contains:

| File | Description |
|---|---|
| `graders/grader_manifest.json` | Effective built-ins, metadata, thresholds, and enabled states |
| `graders/grader_results.json` | Numeric or unavailable results, status, and implementation identity |
| `aw_session.jsonl` | Unified session including conclusion-provenance grader manifest and results |

The collector passes the redacted session array directly to `computeGraders`.
`createSessionLookups` provides shared event-type lookups, correlated tool calls,
and concrete accounting statistics. Pure graders never reread the persisted session
or source logs.

When custom graders are registered, the separate `graders` artifact contains a
combined manifest and results, preserving built-in outcomes. Executable source
is omitted from this artifact. Audit automatically downloads it when the usage
manifest registers custom IDs, including for `--artifacts usage` and cached
reports. If it is not available, audit retains built-in results. Download
failures are reported rather than hidden.

Replay one enabled built-in or custom grader using the same implementation:

```bash
gh aw graders run weekly-research loops 123456789
cat aw_session.jsonl | gh aw graders run weekly-research loops
```

Every invocation emits a version 1 envelope with `definition` and `result`.
No flag is required. The definition contains the resolved
ID, name, description, source, unit, direction, threshold, and data-only
configuration from the same parsed workflow used for execution. Executable
JavaScript and Bash source is excluded; result implementation identities and
digests remain available. Consumers of the former flat output must read
`.result.value`, `.result.status`, and other result fields under `.result`.
To retain the former object shape in a pipeline, use `jq '.result'`.

A finite measurement without a threshold has `status: "pass"` and
`passed: null`: computation succeeded, but task acceptance was not evaluated.
A threshold miss has `status: "fail"` and does not by itself cause a nonzero
exit. An unavailable measurement has a null value, not zero. Runtime errors
return nonzero and can still emit a structured result on stdout; keep stderr
separate when consuming output.

The [Harbor verifier example](https://github.com/github/gh-aw/tree/main/scripts/harbor)
maps explicitly selected measurements into numeric rewards in a separate
verifier environment. Required unavailable evidence and grader errors produce
diagnostics without a reward file; they are not valid zero rewards. The example
uses the canonical unified session, independently of ATIF trajectory export,
and does not execute a fresh workflow.

For normative requirements, see the [Graders Specification](/gh-aw/specs/graders-specification/).
