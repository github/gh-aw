---
title: Graders Specification
description: Pure deterministic metrics over the in-memory unified agent session
sidebar:
  order: 1360
---

# Graders Specification

**Version**: 0.6.0
**Status**: Draft Specification
**Feature Status**: Experimental

## 1. Introduction

Graders compute deterministic execution metrics without additional model calls.
This specification defines configuration, the in-memory interface, execution
ordering, artifact output, and grader-backed experiment metrics.

The key words MUST, MUST NOT, SHOULD, and MAY have the meanings defined by
[RFC 2119](https://www.ietf.org/rfc/rfc2119).

## 2. Grader interface

Built-in and JavaScript graders MUST receive an in-memory
[unified session](/gh-aw/specs/unified-agent-session-specification/), represented
as an array of canonical events with `type`, `data`, and `provenance`.
The caller MAY supply non-executable configuration and shared indexes.

These graders MUST be pure computation. They MUST NOT read files, spawn processes,
access the network, inspect environment variables, or mutate the input.
Identical session and configuration inputs MUST produce identical results.
Legacy preprocessed trace payloads are not accepted by the public interface.

`createSessionLookups(session)` materializes event-type indexes, correlated
tool calls, and concrete execution statistics once for all graders. Internal
trajectory algorithms MAY consume projections derived solely from that session.
They MUST NOT reconstruct missing evidence by reading external artifacts.

Main-agent execution statistics MUST exclude detection and eval usage.
Native tool starts and completions MUST be correlated within their session;
unfinished calls count as failures. Native calls take precedence over duplicate
gateway evidence. Proxy token telemetry takes precedence over aggregate agent
usage to avoid double-counting.

Missing required evidence MUST produce `unavailable`, not fabricated measurements.

## 3. Configuration model

Omitting `graders`, setting it to null, or using `graders: {}` MUST enable the
ten fast built-ins listed below. Other registered built-ins require explicit
configuration.

```yaml
graders:
  loops:
    enabled: false
  tool-success-rate:
    threshold: 0.95
  recurrence-determinism: {}
```

Supported per-grader fields are `enabled`, `name`, `description`, `unit`,
`direction`, `threshold`, `min`, `max`, and `config`. These fields carry data
only. Custom IDs MUST additionally declare an inline JavaScript `script`, limited
to 256 KiB. Built-in IDs MUST NOT accept scripts or implementation overrides.
Operational-value metrics MUST use the same JavaScript contract. `run` and other
shell-backed grader fields MUST fail validation, even when disabled. Unsupported
fields and built-in overrides MUST also fail validation.

Disabling a default with `enabled: false` MUST persist through manifest
normalization and replay. Disabling all defaults is valid.

## 4. Built-in graders

The default set MUST contain only fast, linear-time metrics:

| ID | Measurement |
|---|---|
| `tool-success-rate` | Successful tool calls / total tool calls |
| `tool-failure-count` | Failed or unfinished tool calls |
| `retries` | Retry observations in main-agent MCP events |
| `loops` | Consecutive calls with identical tool names and arguments |
| `trajectory-efficiency` | Distinct tool names / total calls |
| `execution-step-count` | Observed model request count |
| `execution-duration` | Observed duration in milliseconds |
| `working-set-rebuild-factor` | Cumulative input tokens / peak invocation input tokens |
| `context-growth` | Total tokens / first-request tokens |
| `artifact-production` | Safe-output result count |

Trajectory analyses MUST remain opt-in. Reference-dependent results MUST be
unavailable when the unified session does not contain the required evidence.
Potentially quadratic trajectory analyses MUST retain bounded input sizes.
The full registry is documented in the [Graders Reference](/gh-aw/experimental/trace-graders/).

## 5. Execution and artifacts

Built-in grading MUST execute in the **conclusion job**, after agent, detection,
safe-output, experiment, and eval observations have been collected into the
unified session. The caller MUST redact the in-memory session before grading.
Graders MUST NOT run in the agent job or reread raw logs.

Collection, grading, and persistence are separate responsibilities. The
persistence boundary MUST:

1. Compute enabled graders from the collected, redacted event array.
2. Write `usage/graders/grader_manifest.json` and `usage/graders/grader_results.json`.
3. Append `grader.manifest` and `grader.result` events with conclusion provenance.
4. Persist `usage/aw_session.jsonl` and upload the `usage` artifact.

Manifest and result schema version is 2. Results MUST preserve stable grader IDs,
implementation identity, numeric value or null, unit, status, and threshold
outcome. Individual computation failures MUST be recorded as errors and surfaced
in conclusion, not converted into successful scores.

Conclusion MUST NOT receive or execute workflow-provided JavaScript.
Its manifest MAY register custom grader IDs, but MUST NOT contain custom
executable definitions. User-provided conclusion steps MUST be rejected.

Custom JavaScript MUST run only in a separate **graders job**, downstream of a
successful conclusion and its published `usage` artifact. This job MUST use a
fresh hosted runner, read-only repository access, no inherited deployment
environment, and no App, inference, or OIDC credential minting. Work-queue
participants without a published conclusion session MUST reject custom graders.
Authors MUST NOT customize this compiler-owned job.

Each custom JavaScript script MUST receive immutable in-memory session/configuration
snapshots and session-derived lookup helpers, with no host-object bindings.
Scripts MUST be synchronous and return a finite number, null, or a result object
with a finite/null `value`. File, module, environment, process, network, clock,
randomness, and timer capabilities MUST NOT be exposed. Separate restricted Node
processes MUST bound memory, output, and execution time; VM isolation alone is
insufficient.

Operational-value JavaScript MUST obey the same immutable-input and denied-host-
capability boundary. A result MUST be a finite number, null, or an object with a
finite/null `value`. Metric arrays and shell compatibility MUST be rejected.
Each independently useful metric MUST use a distinct grader ID; explanatory
values MAY be returned as `diagnostics`.

The separate `graders` artifact MUST contain combined built-in and custom
results and data-only metadata, without overwriting built-in outcomes or
replacing the conclusion session. Custom implementation identity MUST include a
source digest. Audit MUST conditionally retrieve this artifact when the usage
manifest registers custom IDs, support workflow-call prefixes, and refresh
cached reports when results appear. Missing optional results MUST preserve
built-in results; actual discovery/download errors MUST be surfaced.

## 6. Local replay

`gh aw graders run` MUST use the same pure built-in implementation as conclusion
or the same isolated custom runtime as the graders job.
The CLI owns input parsing and optional download of the run's `usage` artifact.
It accepts unified-session JSONL or a JSON event array, not legacy trace payloads
or executable evaluator requests.

The CLI SHOULD run Node with the permission model, an empty environment, a
bounded execution time, and read access restricted to the staged runtime modules.
The grader itself still receives only the session and data configuration.

## 7. Experiment metric references

Experiment metrics MAY reference `grader:<id>` or `graders.<id>.value`.
The ID MUST resolve to an enabled built-in or custom grader, including automatically enabled
defaults. Unknown, disabled, or empty references MUST fail validation.

`gh aw experiments analyze` reads persisted assignments and grader results.
Only valid numeric results count toward sampling requirements; missing,
unavailable, and failed computations MUST NOT be treated as zero.
Direction determines whether higher or lower values are favorable.
Execution quality metrics are not evidence of semantic task correctness.

See the [Experiments Specification](/gh-aw/specs/experiments-specification/).

## 8. Compliance testing

Conforming implementations MUST cover:

- Default activation, explicit opt-ins, and disabling all defaults.
- Rejection of built-in overrides, shell-backed grader fields, and unsupported fields.
- Custom source exclusion from conclusion and isolated downstream execution.
- Optional custom-artifact discovery, missing-artifact fallback, and cache refresh.
- Determinism, immutable inputs, and absence of file/process I/O in pure graders.
- Restricted operational-value JavaScript with immutable inputs and no host capabilities.
- Shared lookups, tool correlation, duplicate accounting, and phase isolation.
- Conclusion-only execution after unified-session collection and redaction.
- Matching persisted results and unified-session grader events.
- The same behavior in workflow execution and CLI replay.
- A measurable large-session performance bound for the default set.
- Grader-backed experiment reference validation.

## 9. Change log

### Version 0.6.0

- Removes `operational-value.run`, external Bash evaluator resources, and
  metric-array output.
- Runs operational-value metrics through the restricted custom JavaScript
  contract.
- Removes the dedicated shell grader runtime and shell file-write linter.

### Version 0.5.0

- Restores repository-owned `operational-value.run` Bash only in the separate job.
- Replaces legacy evaluator request input with the published unified-session file.
- Adds shared Bash AST file-write checking during compilation and lint.

### Version 0.4.0

- Restores pure custom JavaScript in an isolated downstream job.
- Keeps all user-provided JavaScript out of conclusion.
- Adds conditional audit downloads of combined custom-results artifacts.

### Version 0.3.0

- Replaces file-based trace inputs with the in-memory unified session.
- Moves grading to conclusion and enables fast built-ins by default.
- Removes custom inline and operational-value executable graders.
- Retains opt-in bounded trajectory analyses and shared pure lookup helpers.
