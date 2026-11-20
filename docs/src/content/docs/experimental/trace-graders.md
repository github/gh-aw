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

### Decision-lineage integrity prototype

The existing `end-to-end-lineage-completeness` grader supports an experimental
`config.mode: decision-integrity` projection. Its default safe-output reachability
measurement is unchanged; no additional grader is registered or enabled.

```yaml
graders:
  end-to-end-lineage-completeness:
    threshold: 1
    config:
      mode: decision-integrity
```

#### Existing evidence coverage

This audit uses the [UAS specification](/gh-aw/specs/unified-agent-session-specification/)
(sections 3–7), the essential-payload projection, and the existing engine
normalizers. **Explicit** means supplied structured evidence, **inferable**
means a limited interpretation rather than a causal guarantee, and
**unavailable** means the adapters do not establish that relationship.

| Evidence or relationship | Copilot | Codex | Claude |
|---|---|---|---|
| Tool invocation | Explicit `tool.execution_start`; native `toolCallId`, `input`/`arguments` | `item.started` or invocation-bearing `item.completed` maps to start; `tool_call_id` or item `id` | Assistant `tool_use` maps to start; block `id`, `input` |
| Tool result correlation | `tool.execution_complete.data.toolCallId` | Completed tool item maps to completion with the same call ID | User `tool_result.tool_use_id` maps to completion call ID |
| Observed outcome | Supplied `success`, errors, `shellExecution.exitCode` promoted to `exitCode` | Supplied item status, errors, exit code | Explicit error/success flags; absence of error is not validation proof |
| Workspace mutation | Supplied `fileEdits` or explicit IR action; no universal mutation record | File-change item maps to generic tool output containing `changes`; no workspace revision | Tool result or explicit IR action; an edit tool name is not revision evidence |
| Validation operation | Inferable from command text only; purpose, required checks, and validated revision unavailable | Same limitation | Same limitation |
| Terminal observations | `session.task_complete`, shutdown and accounting projections | `turn.completed`/`turn.failed` project to `session.result`; a completed turn is not task acceptance | Native result projects to `session.result`; not validation proof |
| Acceptance decision → evidence → workspace revision | Unavailable by default | Unavailable by default | Unavailable by default |
| Optional provenance graph | Existing `trajectory.ir` extension, when explicitly supplied | Same extension contract | Same extension contract |

Tool IDs require session, agent, and attempt scope. Native event IDs can repeat
when a single source record expands into several canonical events. `parentId`
preserves native ancestry, not an asserted validation dependency. UAS timestamp
ordering, adjacent records, tool names, and a final “done” message do not establish
causality or acceptance.

The implementation reuses the shared trajectory projection. Existing
`action-provenance-coverage` checks consequential-action reachability and
`premature-termination-gap` checks declared objectives; neither establishes
validation freshness. The prototype measures consistency of explicitly recorded
decision evidence, not code correctness, human adoption, or telemetry
authenticity. Ordinary engine traces therefore cannot establish a full
decision-lineage score without additional producer instrumentation.

Coverage is evidence availability, not engine quality. A tool invocation with no
result, an unscoped reused identifier, or an incomplete evidence chain cannot be
silently treated as success or failure. A successful safe-output handler proves
application of that operation, not acceptance of the agent's reasoning.

Source mappings:
[Copilot normalizer](https://github.com/github/gh-aw/blob/main/actions/setup/js/copilot_session.cjs),
[Codex normalizer](https://github.com/github/gh-aw/blob/main/actions/setup/js/codex_session.cjs),
[Claude normalizer](https://github.com/github/gh-aw/blob/main/actions/setup/js/claude_session.cjs),
[unified payload projection](https://github.com/github/gh-aw/blob/main/actions/setup/js/unified_session_payload.cjs),
and [shared trajectory projection](https://github.com/github/gh-aw/blob/main/actions/setup/js/grader_session.cjs).

#### Experimental evidence contract

The prototype consumes producer-supplied `decisionLineage` metadata inside the
existing agent-phase `trajectory.ir` extension. This is **not a new UAS event
type**, a core UAS guarantee, or metadata currently emitted by the three adapters.
The selected IR is the latest agent-phase IR; separate snapshots are not merged.
All references are local to that snapshot.

The annotation has `version: 1`, optional boolean `complete`, and arrays
`decisions` and `validations`. `decisions` declares acceptance decisions only;
it must not include ordinary tool actions or rejected proposals.
Each decision records `id`, `actionId`, `eventIndex`, `revisionId`,
`requiredValidations` (requirement IDs), and `validationIds` (the checks cited to
support acceptance, not a history of superseded attempts). Each validation
records `id`, `requirement`, `revisionId`, `outcome` (`passed` or `failed`),
`toolCallId`, and `resultObservationId`. Decisions reference existing IR actions;
validation evidence references existing IR tool calls and observations.
`provenanceEdges` must explicitly connect evidence to the decision, and call
`outputRef` and observation `sourceToolCallId` must agree.
Validation occurrences must have IR kinds `tool_call` and `observation`.
The existing `informed`, `required-for`, and `derived-from` relations all establish
reachability; the prototype does not rank their strength or verify the semantic
relevance of a check to its declared requirement.

`revisionId` must identify the exact workspace state, including uncommitted
changes when applicable; a branch name or unchanged HEAD commit does not suffice.
The grader compares opaque identities, not timestamps. A producer must record
which revision a validation checked and which revision the decision accepted.
`complete: true` attests that the snapshot contains all relevant validation
records and provenance relationships for its declared decisions. It must not be
set merely because the agent stopped.
Omitting `complete` makes no completeness assertion.

Each decision is `valid`, `invalid`, or `unknown`. A supported validation on an
explicitly different revision is stale. An explicitly failed required validation
cannot support acceptance. Missing required validations or evidence paths are
violations only under the producer's completeness attestation; otherwise they
are unknown. Ambiguous IDs, dangling references, cycles, conflicting evidence,
and missing metadata remain unknown rather than being repaired heuristically.
Structural uncertainty takes precedence over simultaneous suspected violations;
violation counts include only decisions classified invalid.

The integrity score is `valid / (valid + invalid)`; it is `null` when no decision
is evaluable. Diagnostic coverage is `(valid + invalid) / declared decisions`,
not coverage of every decision the agent may actually have made. Counts retain
unknown decisions separately, with bounded per-decision event references and
violation categories. Consumers must read coverage and unknown counts alongside
the score; a numeric result is not proof of complete instrumentation.

The full diagnostics are available in the grader-results artifact and offline
replay output. The essential UAS `grader.result` projection intentionally omits
opaque diagnostics; the prototype does not weaken that publication boundary.
It performs only pure in-memory computation over already-redacted evidence.
It adds no collector, model call, network request, retry policy, or replay engine.
Future instrumentation needs independently recorded acceptance intent, exact
workspace identities, required-check declarations, and causal evidence links.
Agent-supplied annotations alone cannot prove those assertions are truthful.
This self-consistency prototype is not an authorization or merge gate. A producer
can omit decisions or mislabel an irrelevant successful tool call as validation.
Even coverage `1` means only that all *declared* decisions were evaluable, not
that every actual decision was recorded or independently verified.

For example, replaying one fully linked decision that accepts revision `r1`
using a passing check of revision `r0` produces the following diagnostic fields:

```json
{
  "value": 0,
  "diagnostics": {
    "mode": "decision-integrity",
    "version": 1,
    "evidenceBasis": "producer-asserted",
    "scope": {
      "population": "annotated-decisions",
      "eventIndices": "ir-local",
      "source": { "sessionEventIndex": 0, "phase": "agent" }
    },
    "counts": { "total": 1, "valid": 0, "invalid": 1, "unknown": 0, "evaluable": 1 },
    "coverage": 1,
    "violations": {
      "missing-required-validation": 0,
      "stale-validation": 1,
      "unsupported-acceptance": 0
    },
    "decisions": [{
      "decisionIndex": 0,
      "eventIndex": 33,
      "status": "invalid",
      "reasons": ["stale-validation"],
      "support": [
        { "collection": "actions", "index": 0 },
        { "collection": "events", "index": 2 },
        { "collection": "validations", "index": 0 },
        { "collection": "toolCalls", "index": 0 },
        { "collection": "observations", "index": 0 },
        { "collection": "events", "index": 0 },
        { "collection": "events", "index": 1 }
      ]
    }],
    "truncated": false
  }
}
```

IR event index `33` is not UAS record 33: `scope.source.sessionEventIndex`
locates the containing UAS record, and `support` contains collection positions
within that record's IR (or its `decisionLineage.validations` array).
The outer grader status follows the existing numeric threshold contract;
per-decision tri-state outcomes and coverage are carried in diagnostics.

After configuring the mode in a workflow, use the existing offline replay path:

```bash
cat aw_session.jsonl | gh aw graders run WORKFLOW end-to-end-lineage-completeness
```

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
