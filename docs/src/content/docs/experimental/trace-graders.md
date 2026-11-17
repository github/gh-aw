---
title: Graders
description: Pure deterministic metrics from the unified agent session
---

Built-in and JavaScript graders compute metrics without model calls, file reads,
subprocesses, or network access. Built-ins run in the **conclusion job**, after
observations have been collected and redacted in memory. Custom JavaScript,
including reserved operational-value JavaScript, runs only in a separate
**graders job** after conclusion publishes that session.

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
overridden. Reserved `operational-value` accepts restricted JavaScript `script`
with an ordered metric-array contract. Removed `run` declarations fail with
migration guidance to `script`.

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

## Operational value

Use restricted JavaScript over the recorded session:

```yaml
graders:
  operational-value:
    direction: higher_is_better
    threshold: 1
    script: |
      const outcomes = helpers.createSessionLookups(session)
        .eventsOfType("safe_output.execution");
      if (!outcomes.length) return [{id: "handler-success-recorded", value: null}];
      return [{id: "handler-success-recorded", value:
        outcomes.some(({data}) => data?.status === "success") ? 1 : 0}];
```

Return a nonempty ordered array of unique `{id,value}` objects with finite
numeric or null values. The first metric becomes top-level
`operational-value.value`; subsequent metrics become diagnostics; the full
ordered `metrics` array remains in schema-v2 artifacts and events. Inputs,
capability restrictions, size limits, digesting, and time limits are the same
as other restricted custom JavaScript. The stable top-level
`id/source/status/value` projection remains compatible with gh-cao JSONL and
other existing adapters.

`safe_output.execution` proves handler application status at the run boundary; it
does not prove human adoption or later object survival. gh-aw owns this direct
run snapshot, episode identity, grader artifacts/events, and replay. GitHub is
authoritative for later object disposition. githubnext/gh-cao owns provider
refresh, adoption/rejection/reversal/survival lifecycle, longitudinal
repository/window/campaign Value, canonical storage/querying, and presentation.

Standalone workflows publish longitudinal Value without a campaign. Use
githubnext/gh-cao `add-operational-value OWNER/REPO WORKFLOW-NAME` without
`--campaign`. The optional `--campaign` flag only scopes ownership and rolls up
directly included worker Value; it does not create campaign-specific metric
semantics. Delivery, adoption, reversal, and survival apply independently of
campaign membership and Value configuration. The restricted-JavaScript
operational-value grader is optional run-scoped evidence, not a prerequisite or
substitute for CAO longitudinal Value.

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

Replay one enabled built-in or custom grader using the same implementation,
including reserved operational-value JavaScript:

```bash
gh aw graders run weekly-research loops 123456789
cat aw_session.jsonl | gh aw graders run weekly-research loops
```

For normative requirements, see the [Graders Specification](/gh-aw/specs/graders-specification/).
