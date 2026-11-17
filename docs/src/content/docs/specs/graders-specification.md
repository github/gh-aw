---
title: Graders Specification
description: Pure deterministic metrics over the in-memory unified agent session
sidebar:
  order: 1360
---

# Graders Specification

**Version**: 0.7.0
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
The reserved `operational-value` ID MUST declare restricted JavaScript `script`.
`run` is unsupported for every ID. Unsupported fields and built-in overrides
MUST fail validation, even when disabled.

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
Ordinary custom IDs MUST be synchronous and return a finite number, null, or a
result object with a finite/null `value`. Reserved `operational-value` MUST use
the same restricted worker and return a nonempty ordered array of unique
`{id,value}` objects with nonempty string IDs and finite numeric or null values.
The first metric MUST supply top-level `operational-value.value`; remaining
metrics MUST become diagnostics; the complete ordered `metrics` array MUST remain
in schema-v2 results and unified-session events. File, module, environment,
process, network, clock, randomness, and timer capabilities MUST NOT be exposed.
Separate restricted Node processes MUST bound memory, output, and execution time.
Existing adapter projections, including gh-cao JSONL mapping, MUST continue to
consume the top-level operational-value source and primary value.

### Removed Bash syntax

`graders.operational-value.run` is invalid. Compilers MUST fail with clear
migration guidance to `graders.operational-value.script`, including for disabled
or imported declarations. They MUST NOT ignore or translate Bash automatically.

### Operational-value authority

gh-aw owns execution-time producer facts, episode identity, direct outcome
snapshots, restricted run-scoped grading, artifacts/events, experiments
consumption, and replay because these facts are knowable at conclusion. GitHub
is authoritative for later object disposition. githubnext/gh-cao owns provider
refresh; adoption, rejection, reversal, and survival lifecycle; longitudinal
repository/window/campaign Value; canonical storage/querying; and presentation.
`safe_output.execution` is handler application status, not human adoption.

Every standalone Agentic Workflow MUST publish longitudinal Value through
githubnext/gh-cao `add-operational-value OWNER/REPO WORKFLOW-NAME` without a
campaign. `--campaign` MUST only scope ownership and roll up directly included
worker Value; it MUST NOT define separate campaign metric semantics. Delivery,
adoption, reversal, and survival lifecycle applies independently of campaign
membership and independently of whether Value is configured. The restricted
JavaScript operational-value grader is optional run-scoped evidence and MUST NOT
be treated as a prerequisite or substitute for CAO longitudinal Value.

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

For built-ins and JavaScript, including `operational-value.script`, the CLI
SHOULD run Node with the permission model, an empty environment, a bounded
execution time, and read access restricted to staged runtime modules. Offline
collection regrading MUST accept restricted operational-value JavaScript and
MUST preserve the same ordered metrics and primary mapping.

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
- Rejection of built-in overrides, invalid metric arrays, removed `run`, and unsupported fields.
- Custom source exclusion from conclusion and isolated downstream execution.
- Optional custom-artifact discovery, missing-artifact fallback, and cache refresh.
- Determinism, immutable inputs, and absence of file/process I/O in pure graders.
- Ordered operational-value metrics, primary value, diagnostics, implementation digest, experiment lookup, events/artifacts, replay, and external adapter compatibility.
- Shared lookups, tool correlation, duplicate accounting, and phase isolation.
- Conclusion-only execution after unified-session collection and redaction.
- Matching persisted results and unified-session grader events.
- The same behavior in workflow execution and CLI replay.
- A measurable large-session performance bound for the default set.
- Grader-backed experiment reference validation.

## 9. Change log

### Version 0.7.0

- Removes `graders.operational-value.run` Bash and its compile, runtime, replay,
  resource, and lint surfaces.
- Requires explicit migration to restricted `graders.operational-value.script`.
- Preserves ordered metrics, primary mapping, artifacts, events, experiments,
  replay, and implementation digests through JavaScript.

### Version 0.6.0

- Adds reserved `graders.operational-value.script` through the restricted
  JavaScript custom-grader lane.
- Preserves ordered metrics, primary value, source identity, digest, artifacts,
  events, experiments consumption, and CLI replay/regrading.
- Introduced restricted operational-value JavaScript before Bash removal.

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
