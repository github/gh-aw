---
title: Graders Specification
description: Pure deterministic metrics over the in-memory unified agent session
sidebar:
  order: 1360
---

# Graders Specification

**Version**: 0.8.0
**Status**: Draft Specification
**Feature Status**: Experimental

## 1. Introduction

Graders compute deterministic execution metrics without additional model calls.
This specification defines configuration, the in-memory interface, execution
ordering, artifact output, grader-backed experiment metrics, and read-only
reporting of recorded operational-value results.

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
A Git commit, checkout, or later GitHub state is not an additional grader input.
External measurement facts MUST already be present in the supplied session.
Historical Git analysis and later lifecycle collection are separate consumers,
not mechanisms for adding facts to an immutable session or regrading it.

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

The [GraderDelivery TLA+ model](https://github.com/github/gh-aw/blob/main/specs/graders/GraderDelivery.tla)
and [TLC configuration](https://github.com/github/gh-aw/blob/main/specs/graders/GraderDelivery.cfg)
check the one-run handoff from conclusion to the separate graders job and audit.
They check that custom grading waits for published usage, that the combined artifact
preserves built-in results, and that audit can refresh a built-in-only view without
rewriting the conclusion session. The model abstracts grader computation, runner
isolation, and artifact formats.

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
metrics MUST become diagnostics. Schema-v2 results and full result-event
projections MUST preserve the complete ordered `metrics` array. Compact profiles
MAY omit per-grader results and MUST NOT be treated as a complete result transport.
Downstream custom results MUST NOT be appended to the already published
conclusion session. File, module, environment,
process, network, clock, randomness, and timer capabilities MUST NOT be exposed.
Separate restricted Node processes MUST bound memory, output, and execution time.
Existing adapter projections, including gh-cao JSONL mapping, MUST continue to
consume the top-level operational-value source and primary value.

### Removed Bash syntax

`graders.operational-value.run` is invalid. Compilers MUST fail with clear
migration guidance to `graders.operational-value.script`, including for disabled
or imported declarations. They MUST NOT ignore or translate Bash automatically.

### Operational-value authority

gh-aw owns producer identity, run-scoped grading, artifacts/events, experiments
consumption, and explicit replay. GitHub is authoritative for later object
disposition; gh-aw projects native delivery, adoption, reversal, and survival
observations separately from run grades. `safe_output.execution` records
handler application, not human adoption.

Workflows in any repository MAY report recorded grades and native lifecycle
evidence through `gh aw value` without a campaign or CAO installation.
githubnext/gh-cao and other consumers MAY schedule collection, retain records,
and derive repository/campaign views; they MUST NOT replace native lifecycle
classification or make campaign membership a prerequisite for reporting.
Workflow-specific measurement contracts remain distinct from collection and
presentation. A run grade, execution success, or merge alone MUST NOT be treated
as longitudinal attributed value. Historical Git and typed-evidence reporting
remain separate report-side analyses, not custom session-grader inputs.

The separate `graders` artifact MUST contain combined built-in and custom
results and data-only metadata, without overwriting built-in outcomes or
replacing the conclusion session. Custom implementation identity MUST include a
source digest. Audit MUST conditionally retrieve this artifact when the usage
manifest registers custom IDs, support workflow-call prefixes, and refresh
cached reports when results appear. Missing optional results MUST preserve
built-in results; actual discovery/download errors MUST be surfaced.

### Read-only operational-value reporting

`gh aw value WORKFLOW` MUST read already exported primary operational-value
grades from `summary.json` under `--logs-dir`, using the existing logs `RunData`
and `GraderResult` fields. Reporting MUST NOT require another evaluator
definition for those recorded results or change the schema-v2 grader artifact.
The [Value Reference](/gh-aw/reference/value/#recorded-grader-results) documents
collection and the additive `run_grades` report section. The
[Operational Value Report Specification](/gh-aw/specs/operational-value-specification/)
defines the outcome-blind measurement contract, follow-up, and attribution boundary.

The report MUST select completed runs by host, repository, exact workflow path,
and the existing creation-time period. Positive run/attempt identity and the
canonical run URL MUST agree. Creation-time selection is a run cohort, not a
reconstruction of grader state at the period cutoff.

Per-run output MUST preserve the primary value, unit, status, and recorded
message/error. An observed zero MUST remain zero. Missing operational-value
results MUST be unavailable; other built-in grades MUST NOT be substituted.
Missing summaries or no matching runs MUST report unavailable coverage.
Matching rows MUST describe a retained subset, never complete run history.
Duplicate selected runs/results, inconsistent statuses/values, malformed
summaries, and conflicting selected-run identities MUST fail explicitly.
Summary inputs MUST be bounded; the current limit is 50 MiB.

Reporting MUST NOT execute or regrade code, fetch artifacts or Git history,
aggregate grades, assign longitudinal attribution, or rewrite sessions,
artifacts, or lifecycle records. Configured Git/typed-evidence measurements
retain their separate input requirements and behavior. Ingestion or reporting
in a Go server MUST NOT introduce execution of workflow-provided JavaScript.
This bridge neither adds a sandbox nor certifies the current restricted
worker as an OS/container security boundary. External lake ingestion and
diagnostic/version-aware aggregation are not implemented by this bridge.

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
- Read-only primary-grade reporting without Git, GitHub CLI, or Node execution.
- Retained-run scoping, observed zero, unavailable/error states, and explicit
  rejection of duplicate or conflicting recorded grade data.

## 9. Change log

### Version 0.8.0

- Defines read-only primary-grade reporting through the existing logs export.
- Separates session-only grading, report-side measurements, and later native
  lifecycle observations; CAO and campaigns are optional consumers.
- Clarifies that downstream custom results do not rewrite the conclusion session.
- Leaves grader artifact schema version 2 and session serialization unchanged;
  adds no execution runtime, sandbox, datastore, or external lake integration.

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
