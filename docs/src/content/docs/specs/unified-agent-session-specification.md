---
title: Unified Agent Session Specification
description: Draft contract for canonical traces, full unified artifacts, and bounded compact unified sessions.
sidebar:
  order: 1365
version: "2.5.0"
status: Draft
publication_date: "2026-10-22"
editors:
  - name: GitHub Agentic Workflows Team
    organization: GitHub
---

# Unified Agent Session Specification

**Version**: 2.5.0<br>
**Status**: Draft<br>
**Publication Date**: 2026-10-22<br>
**Editors**: GitHub Agentic Workflows Team (GitHub)<br>
**This Version**: [unified-agent-session-specification](/gh-aw/specs/unified-agent-session-specification/)<br>
**Latest Version**: This document

---

## Abstract

This specification defines the session traces used by GitHub Agentic Workflows: ordered arrays of Copilot-compatible event objects containing `type`, `data`, and optional native metadata. It establishes a loss-preserving parser contract for Claude, Copilot, Codex, Gemini, Pi, and custom engines, including OpenCode and Goose sample integrations, and a conclusion-job projection of essential agent, MCP gateway (MCPG), agent workflow firewall (AWF), safe-output, experiment, grader, and eval payloads. The serialized `usage/aw_session.jsonl` begins with a numeric file-format version header, followed by timestamp-ordered evidence with source provenance; untimed observations remain available without invented timestamps. Native evidence, canonical parser traces, and existing accounting artifacts remain available separately. TypeScript declarations generate machine-readable schemas for both representations.

## Status of This Document

This is a **GitHub Agentic Workflows project specification**, written using W3C-inspired document conventions. It is **not an official W3C standard**, W3C publication, or W3C-endorsed recommendation.

Version 2.5.0 is a draft governed by the project's normal review process. It may be updated, replaced, or superseded. The accompanying implementation and regression suites exercise this contract, including the sampled CI sessions identified in Section 9.4. This is not a blanket declaration of conformance for every engine version or source format. Section 10 records historical pre-implementation gaps, not current defects. Approval and ongoing compliance testing remain project responsibilities.

The specification version belongs to this document. The unified file's leading
`session.format` record carries an independent numeric serialization-format
version; native trace records do not receive per-event specification versions.

## Table of Contents

1. [Introduction (Informative)](#1-introduction-informative)
2. [Conformance (Normative)](#2-conformance-normative)
3. [Trace Model (Normative)](#3-trace-model-normative)
4. [Core Event Vocabulary (Normative)](#4-core-event-vocabulary-normative)
5. [Parsing and Normalization (Normative)](#5-parsing-and-normalization-normative)
6. [Usage and Session Accounting (Normative)](#6-usage-and-session-accounting-normative)
7. [Engine Mappings (Normative)](#7-engine-mappings-normative)
8. [Renderers and Bootstrap Telemetry (Normative)](#8-renderers-and-bootstrap-telemetry-normative)
9. [Compliance Testing (Normative)](#9-compliance-testing-normative)
10. [Implementation Gap Matrix (Informative)](#10-implementation-gap-matrix-informative)
11. [References](#11-references)
12. [Appendices (Informative)](#12-appendices-informative)
13. [Change Log (Informative)](#13-change-log-informative)

---

## 1. Introduction (Informative)

### 1.1 Purpose and scope

The parsers in `actions/setup/js/` translate different engine logs into a shared conversation model. `log_parser_shared.cjs` exposes `convertLegacyLogEntriesToCopilotEvents` and `convertCopilotEventsToLegacyLogEntries`; the first establishes the canonical event representation, while the second supports existing legacy renderers.

This specification covers parsing supported input records, canonical conversion, usage aggregation, tool correlation, compatibility projections, bootstrap telemetry, and conclusion-job artifact merging. It does not generally specify engine execution, workflow frontmatter, model behavior, transport protocols, or a cross-language storage API; Section 7.6 additionally constrains ingestion and publication for opt-in protected declarative sessions. JSON and JSON Lines are serialization forms, not different session models. Section 3.8 additionally defines a bounded, lossy compact unified-session profile for metadata delivery; it never replaces the full session artifact.

### 1.2 Data flow

```text
engine log / native event stream
             |
       tolerant parser
             |
     ordered canonical events
        /              \
summary renderer    session.result selection
     |                     |
legacy display       legacy telemetry projection
projection           for existing metric readers
```

The parser's existing return object can contain `markdown`, `logEntries`, `mcpFailures`, and `maxTurnsHit`. The session trace is the `logEntries` array, not that return object.

The agent artifact already contains native session evidence: Copilot session
`events.jsonl`, Pi `pi-streaming.jsonl`, and other engine logs such as
`agent-stdio.log`. The bootstrap additionally persists its redacted, normalized
`logEntries` as `agent-session.jsonl` so conclusion can reuse canonical events
without parsing the same native transcript again. After downloading agent, detection, safe-output, experiment,
and eval evidence, the conclusion job merges these observations into
`usage/aw_session.jsonl`. The agent artifact also carries the split activation
`aw-prompts/system.txt` and `aw-prompts/user.txt` files; the merger records each
available file as an untimed `prompt.system` or `prompt.user` event with its exact
content and source path. These prompt events are omitted from default summaries.
Built-in graders compute from the redacted in-memory unified session in
conclusion, after all sources are collected. Their manifest and result events
are appended before the session is persisted; grader files are written into
the `usage` artifact. Graders never reread the session or source files. Collection and upload
run after conclusion handlers, before setup cleanup. The existing usage JSON,
JSONL accounting, activity summary, and result files remain available.

### 1.3 Terminology

| Term | Meaning |
| --- | --- |
| Canonical event | An object with a dot-namespaced `type`, a `data` object, and any supplied native metadata. |
| Core event | One of the eight common agent event types in Sections 4.1–4.6. Runtime observations and file metadata have additional declared signatures. |
| Native extension event | A native event with another dot-namespaced type, such as `session.shutdown` or `assistant.message_delta`, retaining its original payload. |
| Legacy entry | A supported pre-event record, such as `system`/`init`, `assistant`, `user`, or `result`. |
| Raw engine record | An engine-specific record that still needs mapping, such as Gemini `tool_result` or Codex `item.completed`. |
| Partial trace | A trace missing some source evidence, including a start, completion, final message, usage report, or terminal record. |
| Dangling call | A tool start with no matching observed completion. |
| Orphan completion | A tool completion with no matching observed start. |
| Per-turn report | Accounting for one distinct model turn or response, not a session-total snapshot. |
| Snapshot | An accounting observation covering work already reported, rather than additional work. |
| Projection | A derived compatibility or presentation view, not a replacement for the canonical trace. |

## 2. Conformance (Normative)

### 2.1 Requirement levels

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

**T-UAS-001 — Conformance claims.** An implementation claiming conformance MUST identify its conformance class and supported engines. It MUST satisfy every applicable MUST and MUST NOT requirement and report unsupported engine mappings rather than claiming full pipeline conformance.

**T-UAS-002 — Normative scope.** Conformance assessment MUST use the explicitly normative sections and their identified requirements. Informative examples, implementation observations, and references to source code MUST NOT override the normative contract.

### 2.2 Conformance classes

| Class | Responsibilities | Applicable sections |
| --- | --- | --- |
| P — Parser/normalizer | Parse source records and produce canonical traces; shared conversion helpers are included. | 3–7 and applicable tests in 9 |
| R — Reader/renderer | Read canonical events and produce truthful, privacy-preserving summaries or compatibility views. | 3–6, 8.1, 8.3, and applicable tests in 9 |
| B — Bootstrap telemetry adapter | Consume canonical results and derive legacy telemetry without altering the trace. | 3, 6, 8.2–8.3, and applicable tests in 9 |
| M — Artifact merger | Combine canonical agent and runtime observations into the conclusion usage artifact. | 3.5, 4.7–4.8, 8.3, and applicable tests in 9 |
| Q — Compact projection | Derive a bounded, lossy metadata view without changing the full unified artifact. | 3.8 and applicable tests in 9 |
| F — Full pipeline | Satisfy P for Claude, Copilot, Codex, Gemini, Pi, custom, and any additional declared integrations; R for all shared summary renderers; B; and M. | All applicable normative requirements |

A parser supporting fewer engines can conform for its declared engines. Omitting an OPTIONAL field because the source did not expose it is full conformance, not partial conformance. An implementation failing a mandatory requirement is nonconforming for the affected class; “partial implementation” is a progress description, not a weaker compliance level.

### 2.3 Source-dependent availability

“Required core event” means required vocabulary support and required emission when corresponding supported source evidence exists. It does not mean every trace contains every event or every field. Availability rules in Sections 3 and 4 are part of the contract, particularly for partial logs and older native streams.

T-UAS-084 through T-UAS-086 apply only when a declarative definition enables `engine.behaviors.execution.protected-session-capture`. Other engines are not required to implement that optional profile.

## 3. Trace Model (Normative)

### 3.1 Representation

**T-UAS-003 — Event array.** The canonical trace MUST be an ordered JavaScript array of event objects, serializable as a JSON array or in the same order as JSON Lines. Each emitted canonical event MUST contain a string `type` and an object `data`. A normalizer MUST NOT introduce a top-level session wrapper or add a specification `version` property to event records.

```json
[
  {
    "type": "assistant.message",
    "data": { "content": "Done.\n" },
    "id": "native-event-7",
    "parentId": "native-event-6",
    "timestamp": "2026-10-02T00:00:01Z"
  }
]
```

**T-UAS-004 — Core support and partiality.** Parsers and readers MUST support all core types in Section 4. A parser MUST emit the applicable core event for each supported source observation, but MUST accept traces without initialization, messages, completions, or a session result. An empty array is a valid representation of no supported observations.

**T-UAS-005 — Absence is not zero.** A normalizer MUST preserve supplied valid values, including `false`, `0`, `""`, `null`, empty objects, and empty arrays. It MUST NOT fabricate source identifiers, timestamps, model names, tool outcomes, duration, cost, token counts, or turn counts to fill missing fields. Unavailable mapped fields MUST be omitted; supplied native optional fields remain supported. An in-memory `undefined` mapped property that is omitted by JSON serialization is equivalent to an absent property.

### 3.2 Native extensions and metadata

**T-UAS-006 — Open event vocabulary.** A parser or loss-preserving reader MUST retain unknown native dot-namespaced event types and their `data`, top-level fields, and order. The vocabulary is not limited to `user.`, `assistant.`, `tool.`, and `session.`. A display-only renderer MAY omit an extension it cannot display, but MUST NOT remove it from the canonical trace.

Dot-namespaced means a type containing a namespace separator, for example `session.info` or `vendor.progress`; it does not require a literal leading dot. A parseable object whose type happens to contain a dot is not automatically a recognized raw engine record: the canonical native-event signature includes an object `data`, or the applicable engine mapping supplies a supported raw signature.

**T-UAS-007 — Metadata preservation.** Normalization MUST retain supplied native `id`, `parentId`, `timestamp`, and other source metadata without rewriting their values. Known payload values MUST map to the core fields; other supplied metadata MUST survive as compatible extension fields. When one source record expands into several events, its applicable metadata MUST be retained on those events. A native record ID MUST NOT be replaced with a tool correlation ID or assumed globally unique across such expansion.

Existing native payload fields, including fields not understood by the implementation, remain supported. Native metadata can include source-specific versions; preserving an existing native field is different from adding a specification version field.

Copilot dynamic workflow observations (`workflow.run_started`,
`workflow.run_updated`, and `workflow.run_settled`) retain run identity, attempt
or revision, status, failure class, and supplied resource counters in the unified
projection. Subagent lifecycle observations retain their agent and tool-call
identity; `subagent.started.workflowRunId` links them to the dynamic run, with
`factoryRunId` accepted as a legacy alias. Publication views display these fields
without dumping workflow arguments, results, or agent descriptions. Workflow
resource counters are observations, not additional session token or cost totals.
The SDK driver captures ephemeral workflow lifecycle signals explicitly. Native
CLI disk logs need not contain these signals; collectors must not invent events
that were never supplied by the source.

### 3.3 Parser integration

**T-UAS-008 — Existing API boundary.** Parser integrations MUST keep the trace in the existing `logEntries` return member. Presentation text and flags such as `mcpFailures` and `maxTurnsHit` MUST remain separate from event data unless they reflect actual source observations. Canonical `logEntries` MUST NOT contain bare legacy `result` entries for telemetry convenience.

### 3.4 TypeScript message signatures (Informative)

[`types/agent_session.d.ts`](https://github.com/github/gh-aw/blob/main/actions/setup/js/types/agent_session.d.ts)
defines the TypeScript interfaces for common agent signatures and the declared
`agent.execution`, `detection.result`, and `session.format` observations.
`CoreSessionEvent` is their discriminated union; `SessionEventDataMap` associates
each declared event type with its payload interface. `AgentSession` also accepts native dot-namespaced extension
events. CommonJS implementations import these types through JSDoc, without introducing
a TypeScript runtime dependency.

```typescript
import type { ToolExecutionCompleteEvent } from "./types/agent_session";

const completion: ToolExecutionCompleteEvent = {
  type: "tool.execution_complete",
  data: { toolCallId: "call-1", success: false, output: 0 }
};
```

The generic `createSessionEvent` factory checks known event payload signatures.
`types/agent_session.type-test.ts` checks valid signatures and rejects incorrect
outcome types, metric types, and legacy event names during `npm run typecheck`.
`SessionCount` and `SessionMetric` annotate numeric ranges for schema generation;
JavaScript runtime types remain numbers. Range enforcement belongs to validators,
not TypeScript's static type system.

### 3.5 Conclusion session artifact

**T-UAS-054 — Single-file serialization.** The conclusion job MUST publish the
unified trace as `aw_session.jsonl` in the existing `usage` artifact. Each nonblank
line MUST be one canonical event, without a session wrapper, display truncation,
or a bare legacy accounting result. Serialization MUST use compact JSONL:
exactly one unindented JSON object per physical line, no pretty-print whitespace
outside string values, no blank separator lines, and a trailing newline after
the final record. Embedded line breaks in retained payload strings MUST be
JSON-escaped, not trimmed or emitted as additional physical lines. Existing
usage files MUST remain compatible.
Collection MUST follow available downstream result downloads and conclusion
handlers; setup cleanup MUST follow publication.

The loss-preserving parser requirements apply to `logEntries` and
`agent-session.jsonl`. The unified artifact is an essential-payload projection:
it MUST retain the event type, supplied `id` and `parentId`, observed timestamp,
source provenance, and the essential fields below. It MUST omit duplicated
engine envelopes and nonessential fields from known payloads. Retained strings
MUST NOT be trimmed or truncated. Unknown native extension `data` MUST remain
opaque because its essential fields are not defined by this specification.

| Payload family | Essential fields |
| --- | --- |
| Agent initialization | Engine, model, session ID, working directory; no tool inventories or duplicated provider metadata. |
| Agent messages and reasoning | Exact `content`, without duplicate text blocks or the original message envelope. |
| Agent session errors | Observed diagnostic fields, including `errorType`, `code`, and `statusCode`; absent status codes remain absent, and unrelated native error context is omitted. |
| Agent policy refusals | Structured `reason`, exact available `content`, `policyCategory`, `explanation`, and streaming `partial` flag; no duplicated provider envelope. |
| Agent tool lifecycle | Correlation IDs, tool/server names, one `input` or `output` field, command, outcome/error signals, duration, and exit code. Background tool updates retain observed `taskId` and `taskType`; progress and task notifications do not imply a tool completion. |
| Agent accounting | Turns, duration, cost, observed terminal `status` and `sourceType`, normalized `usage` including reported reasoning tokens, errors, and permission denials. |
| Agent execution diagnostics | Unique harness/compiler `categories`, native `errorCodes` and `errorTypes`, and the observed final process `exitCode`; no diagnostic messages or duplicated engine envelopes. |
| MCP | Server, direction, RPC/call/request IDs, method, tool name, duration, sizes, status, reason, and error code/message. Failed calls reporting HTTP 429 also retain `httpStatus: 429` and the sanitized `retryHint`, including retry durations or `Retry-After` seconds/HTTP-date. No RPC arguments, response bodies, echoed queries/URLs in rate-limit diagnostics, or error context. |
| Firewall | Host, method, status, decision, byte count, duration; steering/tracker event, level, message, reason, and request ID. |
| Safe outputs | Operation type, repository/number, provider/identifier/URL, status, and errors; no requested title/body or arbitrary operation payload. |
| Experiments | Run ID, assignments, and counts. `experiment.assignment.data.assignments` retains the complete current-run name-to-variant map, including an experiment named `assignments`; historical ledger entries remain separate `experiment.state` observations. |
| Graders and evals | Grader IDs/names, sources, enabled flags, bounds, values, units, statuses, decisions, thresholds, errors, severity, and diagnostic messages. Manifest/result versions, run ID/attempt and counts, implementation ID/version/digest, and metric ID/value pairs are retained when supplied. Eval ID, answer, model, error, run ID, and experiment assignments are retained. No grader scripts, opaque diagnostics, or eval questions. |
| Runtime accounting | Provider, model, request ID, status, AIC, cumulative/checkpoint AIC, premium requests, duration, and normalized `usage`; overlapping reports stay separate. Detection and eval usage reports resolve per-observation AIC from known model pricing only when explicit AIC is unavailable; unknown pricing leaves AIC absent, while explicit zero and checkpoints remain authoritative. |
| Execution, detection, workflow | Observed outcomes, exit code, duration/start/end, detection job result/conclusion/categorical reason and verdict flags, engine ID, requested model, trigger type, workflow/repository/run ID, and available gh-aw, AWF, MCPG, and agent versions. No detector transcript or free-form reasons. |
| Model routing | `firewall.model_routing` retains its stage, purpose, provider, labels, selection, endpoint, router, request, outcome, and deviation fields, plus AWF's ranked choices, pool counts, request endpoints, telemetry availability, HTTP status, and failure phase when supplied. `workflow.info` includes the observed `model`, `requestedModel`, and a compact `modelRouting` object. `model_routing.outcome` records the harness's status, wire model, effective and selected endpoints, effort, applied effort, and failure code. |
| Caller context and episode lineage | `workflow.info.awContext` retains the supplied `aw_context` object from runner-owned `aw_info.json.context`, including original keys and nested work-queue data. For webhook batches, it retains unique affected items and delivery IDs but omits each delivery's raw `payload`; those bodies remain in runner-owned context files. Text and Markdown summaries display the log-safe context as compact JSON alongside the workflow metadata, subject to existing output-size limits. Registered secrets are redacted during artifact serialization and summary rendering. `workflow.info.episode` additionally projects supplied canonical lineage and parent repository, run, attempt, workflow, call identity, and event type. |
| Skills | Activation inventory records installed skill name, relative source, scope, SHA-256 content digest, and declared purpose, without skill content. Exact-name and cross-scope collisions retain names, kinds, and source identifiers: current sources are workspace-relative paths, while overwritten frontmatter installations use ordinal identifiers such as `frontmatter-1`. Observed engine invocations retain the selected skill name, path, source, trigger, model, and turn when supplied; a declared or installed skill is not proof of invocation. |

OpenTelemetry observations use `otel.span`, `otel.span_event`, and `otel.log`.
They retain trace/span/parent IDs, names, status, attributes, links, log bodies
and severity, resource/scope context, and original Unix-nanosecond clocks. Span
events are separate observations rather than duplicated inside the span payload.

Known payload aliases MUST use one canonical key, preferring an explicitly
present canonical value even when it is `false`, `0`, `null`, or empty.
`parameters` maps to `input`; `result` maps to `output` when `output` is absent.
Safe-output `failures` maps to `errors`, retaining operation types, error codes,
and messages. Copilot checkpoint `ai_credits` maps to `totalAic` and
`premium_requests` maps to `premiumRequests`.
Explicit failure signals MUST survive alias compaction even when another
source field claims success. Known unified payload fields use camelCase, such as
`serverName`, `rpcId`, `durationMs`, `secretLeak`, and nested `usage.inputTokens`.
The parser traces and original accounting artifacts continue to use the
snake_case token keys from Section 6; unknown native extension payloads remain
opaque. Workflow metadata has one `engineId` rather than a duplicate `engine`
and distinguishes `requestedModel` from the observed agent model. `triggerType`
comes from the GitHub Actions event name; `cliVersion`, `awfVersion`,
`mcpgVersion`, and `agentVersion` come from available workflow metadata and
are not inferred. The numeric file-format version remains `1`: the
`type`/`data`/`provenance` envelope and JSONL framing are unchanged.

The optional `workflow.info.episode` object projects recorded lineage and parent
identity from `aw_info.json.context` without inferring absent values or changing
identifiers. `episodeId`, `hopId`, `parentHopId`, `originEvent`, `rootRepo`,
`rootWorkflowId`, and `rootRunId` map the corresponding snake_case lineage fields.
The caller's `repo`, `run_id`, `run_attempt`, `workflow_id`, `workflow_call_id`,
and `event_type` become `parentRepo`, `parentRunId`, `parentRunAttempt`,
`parentWorkflowId`, `parentWorkflowCallId`, and `parentEventType`, respectively.
The current run's metadata remains separate: `workflow.info.runAttempt` and
`workflowId` retain the recorded attempt and executing workflow's full reference
from compiler-wired `job.workflow_ref`, including for reusable invocations.
`workflowCallId` retains the top-level `aw_info.json.workflow_call_id`, computed
with the existing workflow-call ID helper as `<run_id>-<run_attempt>:<workflow_id>`.
The attempt defaults to `1`; when the executing workflow reference is unavailable,
the ID contains only `<run_id>-<run_attempt>`. This identifies the current
workflow, while `episode.parentWorkflowCallId` retains the caller's recorded ID.
`callerWorkflowRef` separately retains `GITHUB_WORKFLOW_REF`, which identifies
the top-level caller for reusable workflows (and the current workflow otherwise),
while `ref`, `sha`, `actor`, and `targetRepo` retain the existing metadata used
for legacy lineage matching. Both references are captured when the runner writes
`aw_info.json`, never from the collector's environment. An unavailable executing
reference remains empty; the caller reference is not substituted for it.
`workflow.info.workflowRef` (runner-written `aw_info.json.workflow_ref`) repeats
the executing reference held by `workflowId`; it is never `GITHUB_WORKFLOW_REF`.
`serverUrl` (`server_url`) is the origin of the runner's GitHub server URL, such
as `https://github.com`. `engineHarness` (`engine_harness`) is `copilot-cli` or
`copilot-sdk`, compiled from the workflow's Copilot harness configuration
(`engine.copilot-sdk`) and wired as `GH_AW_INFO_ENGINE_HARNESS`; it is never
inferred from `engineId`, the workflow name, or model output, and is absent for
other engines. All three are absent in sessions written before they existed;
readers MUST treat their absence as unknown, not as a particular harness or
server.

That context describes the recorded caller: `episode.hopId` is not relabeled as
the current run's hop, and `episode.parentHopId` remains that caller's recorded
parent, not the current run's immediate parent. Parent and current run IDs may
match for reusable workflows; workflow and call identities must remain distinct.
Legacy context can provide parent identity without a canonical `episodeId` or
hop ID. Existing files without lineage remain valid. Only runner-owned workflow
metadata supplies this projection, using the collector's existing source
precedence. Explicit normalized `episode` values take precedence over context,
including explicit `null`; canonical `awContext` takes precedence over legacy
`context`.

**T-UAS-055 — Source provenance.** Every merged event MUST have a `provenance`
object with `component`, `phase`, `path`, and `index`. `path` MUST be relative to
the downloaded runtime root. `index` MUST identify the position in that source's
normalized event array, not claim to be a raw line number. Native engine event
IDs and tool correlation IDs MUST NOT become global IDs or be rewritten to
disambiguate sources. A preexisting native `provenance` field MUST survive under
`provenance.native`. Merge operations MUST NOT mutate source observations.

The TypeScript `SessionProvenance`, `UnifiedSessionEvent`, and `UnifiedSession`
types describe this additive envelope. Components include `agent`, `mcp`,
`firewall`, `safe_output`, `experiment`, `grader`, `eval`, `usage`, `execution`,
`detection`, `workflow`, `github_api`, `otel`, and `collector`. Phases distinguish agent and detection
traffic, activation snapshots, evals, safe-output execution, and conclusion
collection. A component is not an engine name or a success claim.

**T-UAS-064 — File-format version header.** The first record of
`aw_session.jsonl` MUST be a collector-owned `session.format` entry with
`data.version` set to the positive integer file-format version, currently `1`.
The header MUST be present even when no source events are available. Its
provenance MUST identify component `collector`, phase `conclusion`, the logical
artifact path `usage/aw_session.jsonl`, and index `0`. It MUST NOT carry an
invented timestamp. Source-native events named `session.format` MUST remain
ordinary source observations and MUST NOT replace the file header.

```json
{"type":"session.format","data":{"version":1},"provenance":{"component":"collector","phase":"conclusion","path":"usage/aw_session.jsonl","index":0}}
```

The numeric file-format version is independent of this document's semantic
version and native engine versions. Readers claiming support for this file
format MUST inspect the first record before consuming the remaining stream and
MUST report a missing, invalid, or unsupported version instead of silently
assuming compatibility. Parser-only canonical arrays and `agent-session.jsonl`
are source traces, not versioned unified files, and do not require this header.

**T-UAS-056 — Timestamp ordering.** After the file-format header, the merger MUST
interleave sources by supported timestamps while preserving each sequential
source's normalized event order. A regressing clock MUST NOT reverse source-local
observations. For merging only, each sequential source uses a nondecreasing
ordering anchor: its first valid timestamp anchors preceding untimed records,
and subsequent records use the latest timestamp observed so far. These anchors
MUST NOT be written as observed timestamps. The merger MUST preserve native
timestamp values and MUST derive its ordering key using the source schema's
units, not an epoch-magnitude heuristic. ISO timestamps MUST carry a timezone.
Numeric Squid audit timestamps use Unix seconds; native agent timestamps and
token-tracker audit `ts` values use milliseconds. Pi's observed
`message.timestamp` is also a supported source timestamp. ISO fractional seconds
and OTLP decimal-string Unix nanoseconds MUST be compared at nanosecond precision.
`provenance.timestampMs` is an observed millisecond projection, not the exact
ordering key; native timestamps retain their original precision.

OTLP export envelopes are unordered batches, not sequential execution streams.
Their expanded spans, span events, and logs MUST sort by their own observed
timestamps, regardless of export order. A span uses `startTimeUnixNano`; a span
event uses `timeUnixNano`; a log uses `timeUnixNano`, falling back to supplied
`observedTimeUnixNano` only when the former is absent. The merger MUST NOT
substitute a containing span's clock for an untimed span event.

**T-UAS-057 — Untimed and tied observations.** Missing or invalid timestamps
MUST NOT cause observations to be discarded. Untimed sequential observations MUST
stay in source-local order using the merge-only anchors in T-UAS-056. Completely
untimed sources and untimed OTLP observations MUST follow anchored observations.
Equal ordering keys and unanchored observations MUST preserve deterministic source
enumeration and source-local array order. The merger MUST NOT fabricate an
observed timestamp from file modification time, collection time, a neighboring
event's time, or an inferred tool duration. Wall-clock ordering
does not establish causality or correct cross-process clock skew.

**T-UAS-058 — Authoritative source selection.** A persisted canonical agent
stream MUST take precedence over its raw engine logs. Without it, the merger
MUST use available native Copilot sessions or the existing engine parser for
supported raw logs. It MUST NOT combine these equivalent agent representations
and count them as separate observations. Replicated firewall paths MUST select
`sandbox/firewall/logs` before `sandbox/firewall/audit`, then supported legacy
layouts; an existing empty authoritative file MUST suppress its older copy.
Distinct gateway streams and events MUST remain separate observations.

For agent-phase OTel evidence, `otel.jsonl` takes precedence over
`usage/otel.jsonl`, including an empty primary mirror. Detection evidence uses
`threat-detection/otel.jsonl`, then `usage/threat-detection/otel.jsonl`.
The collector MUST expand OTLP `resourceSpans[].scopeSpans[].spans[]` and
`resourceLogs[].scopeLogs[].logRecords[]`, retaining resource and scope context.
Malformed nested entries MUST emit collection warnings without discarding
adjacent valid spans or messages. Only available local mirrors are imported;
collection MUST NOT fetch an OTLP backend or infer missing engine spans.
Publication summaries show span identity, severity, and status rather than
dumping log bodies, attributes, links, or resource payloads. Artifact secret
redaction and symlink protections apply to these sources as to other traces.

Native Copilot session files MUST pass through the Copilot adapter before
essential-payload projection so model selection, streamed assistant text, tool
correlation, finalized turns, and shutdown usage remain available. The agent
step-summary parser prefers usable native events, then `agent-stdio.log`, then
debug logs. Missing or unparseable native files MUST NOT suppress a usable
stdio conversation; fallback diagnostics MUST NOT claim successful parsing
when no structured events were recovered.

For `detection.result`, the sanitized conclusion result at
`usage/detection/detection_result.json` MUST take precedence over the raw
`threat-detection/detection_result.json` verdict. The conclusion result combines
trusted job outputs with verdict flags extracted from structured results or
inline detector logs. These files MUST NOT produce duplicate detection results.
The raw structured verdict MAY be used when the conclusion result is absent.
Provenance MUST identify the selected file; detection-log timestamps MUST NOT be
invented for an extracted verdict.

The essential detection payload is `jobResult`, `conclusion`, `reason`,
`promptInjection`, `secretLeak`, and `maliciousPatch`, when observed. `reason` is
the categorical conclusion reason, such as `threat_detected`, `agent_failure`,
`parse_error`, or `detection_skipped`; it is not the detector's free-form `reasons`
array. Missing verdict flags MUST remain absent, including skipped, cancelled,
or failed detection without a verdict. Explicit `false` flags MUST be retained.
Readers MUST NOT equate a successful job with a clean verdict: warn-mode
detection can have `jobResult: "success"` and `conclusion: "warning"`.

The Go audit reader consumes version-1 `usage/aw_session.jsonl` detection
observations with detection component and phase provenance. It uses their
verdict flags and recorded outcomes without exposing detector prose, and retains
legacy detection-artifact compatibility. For older unified files containing only
verdict flags, separately recorded conclusion outcomes supply missing status
fields. Explicit canonical fields, including an empty `reason`, take precedence.

**T-UAS-059 — Partial collection.** Missing optional sources MUST be reported
by component availability, not fabricated empty results. Malformed JSONL
records MUST produce explicit collection warnings while preserving valid
adjacent records. Filesystem read/write failures MUST be surfaced; a failed
collection MUST NOT leave a stale or partially written unified output eligible
for upload. Artifact paths MUST NOT follow symbolic links. Any source traversal
limit MUST produce a coverage warning.

Grader and eval result collection MUST import only records attributed to the
current run, not restored historical results. Runner-owned `aw_info.json`
supplies the run ID and available attempt; the supplied collector environment
(defaulting to the process environment) supplies them only when recorded run
identity is absent. An explicit environment snapshot MUST NOT borrow run identity
from the ambient process. Records from other runs or
explicitly different attempts are excluded without changing source artifacts.
Unattributed results or an unavailable current run ID produce collection
warnings and are omitted. Retained records preserve their original source
indices; evals without attempt metadata are selected by run ID. Warning line
numbers refer to original JSONL lines, including skipped blank or malformed
lines; JSON result arrays do not supply file line numbers.

**T-UAS-060 — Publication boundary.** Both persisted canonical agent events and
the unified session MUST apply the artifact secret/add-mask policy before JSON
serialization. Redaction MUST process decoded string values, including native
IDs and nested structured payloads, without mutating internal traces. Private
correlation-key exemptions used by display renderers MUST NOT exempt persisted
artifact fields. Registered masks from agent stdio MUST apply even when a native
session file is preferred.

Runtime masks MUST be collected before the pre-upload secret redaction pass
changes stdio or bootstrap removes its `::add-mask::` commands. The pass MUST
apply these in-memory masks to all artifact sources, including decoded JSON
strings in native sessions, MCP, firewall, and safe-output evidence, before any
summary rendering or upload. Raw mask values MUST NOT be persisted in an
uploaded handoff file. Conclusion consumes sanitized sources and retains its
stdio mask scan for compatibility with unsanitized legacy inputs. A failed
runtime-mask redaction MUST remove the affected source rather than leave it
eligible for an `always()` artifact upload.

The artifact can contain redacted user prompts, reasoning, paths, and full tool
outputs. It is execution evidence with the workflow artifact's access and
retention policy, not a default summary or a public log preview. Its size can
exceed the former telemetry-only usage artifact; summary display budgets do not
truncate this file.

### 3.6 Generated schemas and local tools (Informative)

The declaration files are the machine-readable signature source.
[`types/unified_session.d.ts`](https://github.com/github/gh-aw/blob/main/actions/setup/js/types/unified_session.d.ts)
defines essential runtime payloads and `UnifiedSessionUsage`, whose camelCase
keys differ from parser-trace accounting. `UnifiedSessionEventDataMap` covers the
known projection vocabulary, including collector records. Unknown native
extension payloads remain open. Runtime scalar observations use JSON-compatible
values where the merger preserves source-specific values rather than coercing them.

| Artifact or command | Purpose |
| --- | --- |
| [Canonical agent schema](/gh-aw/schemas/agent-session.schema.json) | JSON-array schema for parser traces and the records in `agent-session.jsonl`; an empty trace is valid and no header is required. |
| [Unified session schema](/gh-aw/schemas/unified-session.schema.json) | JSON-array schema for essential payloads and provenance, requiring the leading version-1 collector header. |
| `make session-schemas` | Regenerate both checked-in JSON Schema Draft-07 files using `ts-json-schema-generator` and deterministic formatting. |
| `make check-session-schemas` | Regenerate in memory and fail on missing or byte-different schemas; never rewrite files. |
| `node actions/setup/js/scripts/validate_session.cjs agent agent-session.jsonl` | Validate a canonical source trace. |
| `node actions/setup/js/scripts/validate_session.cjs unified aw_session.jsonl` | Validate the unified artifact, including compact JSONL framing, provenance paths, ordering, and execution singleton semantics. |

The validator also accepts JSON arrays: a `.json` filename selects array input,
or a final `json`/`jsonl` argument selects the format explicitly. JSONL schemas
describe the logical array of decoded records, not the line framing itself.
Each schema is self-contained and can be used by standard Draft-07 validators.
The local validator reports record counts on stdout and structural diagnostics
on stderr, exits nonzero on failure, and does not print source payloads.
Validation reads the complete input and materializes its events in memory, so
very large session files may require substantial memory.
The existing `session_cli.cjs markdown` mode supplies a privacy-preserving
rendered view; schema validation does not replace rendering or redaction.

Generation dispatches known types conditionally: malformed known payloads cannot
pass as opaque native extensions. Canonical schemas allow native additions;
known essential projection interfaces omit discarded inventories, envelopes,
and accounting aliases. These schemas check declared writer signatures, not
every historical diagnostic value that a tolerant parser can retain. Validation
does not mutate, repair, redact, or drop records. A schema pass alone cannot
establish loss preservation, correct engine accounting, safe publication, or full
conformance; those requirements remain covered by behavioral tests.

### 3.7 Session downloads (Informative)

`gh aw sessions download <run-id-or-url> --format jsonl` prefers a published
`usage/aw_session.jsonl` and returns its validated bytes unchanged. When that file
is absent, it reconstructs the current format from available agent artifacts,
including supported legacy names and nested layouts, using the same embedded
JavaScript parsers as Actions. Reconstruction requires recognizable agent or
execution evidence and cannot recover missing downstream artifacts.
`--format markdown` uses the shared privacy-preserving renderer.
Ambiguous called-workflow artifact matches and invalid published files fail
explicitly; invalid published data does not silently fall back to reconstruction.

### 3.8 Compact unified session (Normative)

A **compact unified session**, compact profile version `1`, is a deterministic,
bounded, intentionally lossy JSONL projection of an already redacted unified
session. It supports lightweight completion notifications and metadata consumers;
it is not the full session, a new source of accounting totals, or an alternative
publication of `aw_session.jsonl`. The full artifact and its serialization-format
version remain unchanged. A compact stream can omit required full-session
metadata and MUST NOT be presented as conforming to the full unified-session
schema or as evidence that omitted activity did not occur.

**T-UAS-076 — Compact vocabulary and projection.** Compact producers MUST
preserve encounter order and retain only these event kinds and `data` keys:

| Event kind | Scalar `data` fields |
| --- | --- |
| `session.format` | `version` |
| `workflow.info` | `engineId`, `agentVersion`, `cliVersion`, `awfVersion`, `mcpgVersion`, `model`, `requestedModel`, `triggerType`, `dryRun`, `workflowId`, `workflowCallId`, `repository`, `runId`, `runAttempt`, `ref`, `sha` |
| `session.result` | `sourceEngine`, `numTurns`, `durationMs`, `totalCostUsd`, `status`, `sourceType`, `sessionId`, `agentId` |
| `usage.report`, `firewall.token_usage` | `stepIndex`, `provider`, `model`, `purpose`, `status`, `aic`, `totalAic`, `premiumRequests`, `durationMs`, `sessionId`, `agentId` |
| `guardrail.admission` | `status`, `maxAiCredits`, `threatDetectionMaxAiCredits`, `evalsMaxAiCredits` |
| `guardrail.daily_aic` | `status`, `exceeded`, `total`, `estimated`, `threshold` |
| `workflow.job_result` | `jobId`, `result`, `durationMs` |
| `execution.result` | `outcome`, `conclusion`, `exitCode`, `durationMs`, `startedAt`, `finishedAt` |
| `grader.result` | `id`, `status`, `value`, `score`, `unit`, `passed`, `direction`, `threshold`, `enabled`, `min`, `max`, `version` |

`session.result`, `usage.report` and `firewall.token_usage` MAY additionally retain `data.usage`, but only
its scalar `totalTokens`, `inputTokens`, `outputTokens`, `reasoningOutputTokens`,
`cacheReadInputTokens`, `cacheCreationInputTokens`, and `inputTokensIncludeCache`
fields. `grader.result` MAY retain `data.run`, restricted to scalar `id`,
`attempt`, `graderCount`, `passed`, `failed`, and `errors`.
These are source observations, not additive contributions: overlapping session,
usage-report and firewall accounting MUST NOT be summed as independent spend.

Only finite numbers, booleans and Unicode-scalar strings of at most 256 UTF-8
bytes survive scalar projection. Nulls, arrays, other objects, nonfinite numbers,
oversized strings and unpaired surrogates MUST be omitted, not coerced or
truncated. At the event level, only `type`, projected `data`, an eligible scalar
`timestamp`, and projected `provenance` MAY survive. Provenance is restricted to
eligible scalar `component`, `phase`, `path`, and `index`; native metadata and
unknown fields MUST NOT survive. Missing values MUST NOT become defaults.

All other event kinds MUST be dropped, including user and assistant messages,
reasoning, refusals, prompts, tool calls, arguments, output and native extensions.
Unbounded `errors`, `permissionDenials`, `agentMetrics`, grader result arrays,
inventories, caller context and arbitrary nested payloads MUST NOT be forwarded
through a superficially eligible event.

**T-UAS-077 — Hard output limits.** The serialized compact JSONL MUST contain at
most **128 records** and **16,384 UTF-8 bytes**, including JSON escaping and one LF
after each retained record. Each candidate is projected and serialized before
budget admission. A record that would exceed either limit MUST be omitted whole;
the producer MAY retain a later, smaller record that fits, without reordering.
Input events and full artifact bytes MUST NOT be mutated, rewritten or replaced.
Repeated equal inputs MUST produce identical compact output and statistics.
An empty compact stream is valid and does not mean that the full session is empty.

**T-UAS-078 — Bounded file reading and explicit loss.** A file-based producer
MUST snapshot the nonempty regular file's size, reject a symlink at the file
path, and inspect no more than the first **67,108,864 bytes** of that snapshot.
It MUST frame records incrementally without retaining more than **16,384 source
bytes per line**. An oversized line MUST be discarded through its next LF
without decoding or parsing its unbounded content; subsequent complete records
remain eligible. A final unterminated record MAY be decoded only when it reaches
the snapshotted file end, not when a scan limit cuts it off. Source growth MUST
NOT extend the scan. Retained-size records with invalid UTF-8 or JSON, I/O errors,
missing files, empty files and non-regular files MUST fail explicitly rather than
produce a fabricated successful session.

The reusable producer result has these fields:

| Field | Meaning |
| --- | --- |
| `version` | Compact profile version, currently `1`; independent of the full file-format header. |
| `session` | Compact JSONL, bounded by T-UAS-077. |
| `includedEvents` | Number of retained records. |
| `omittedEvents` | Number of inspected records omitted by the vocabulary, projection or output budgets, including oversized completed lines. Counts describe the inspected prefix only. |
| `oversizedLines` | Number of completed oversized source lines discarded by file framing; `0` for an in-memory producer. |
| `scanTruncated` | The file snapshot exceeded the source-scan budget; `false` for an in-memory producer. |
| `truncated` | An output budget, completed oversized source line or source-scan limit discarded evidence. This does not describe routine vocabulary/field filtering and MUST NOT certify completeness when false. |

**T-UAS-079 — Full-session resolution.** A transport using a compact unified
session MUST identify it as compact profile version `1`, include the above
loss/coverage metadata, and provide available usage artifact IDs when the full
session was uploaded. IDs MUST be preserved as canonical positive-decimal
strings, not floating-point numbers. Retrieval MUST bind each artifact to the
authenticated repository and current run/attempt and executing workflow
invocation before consuming its `aw_session.jsonl`. An absent or expired artifact
MUST remain explicitly unavailable; a compact stream MUST NOT be relabeled as
the full session or silently treated as complete accounting evidence.

The shared runtime helper is
[`compact_unified_session.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/compact_unified_session.cjs).
`compactUnifiedSession(events)` accepts an iterable of decoded events;
`readCompactUnifiedSession(filePath)` uses the same projection with bounded
file framing. Both return the above result without modifying their input.
The [conclusion request contract](/gh-aw/specs/admission-service-specification/#conclusion-notification)
defines its 64 KiB transport envelope and upload-ID wiring separately.

## 4. Core Event Vocabulary (Normative)

The following tables describe the common fields in `data`. Optional native and previously supported fields are not prohibited. A field marked **source-dependent** is preserved or mapped when supplied and meaningful; its absence is not an error. This avoids making unsupported metrics or IDs prerequisites for canonical conversion.

### 4.1 `session.init`

**T-UAS-009 — Initialization.** A mapped initialization event MUST use `session.init` and MUST set `sourceEngine` to the recognized source engine. It MUST preserve the initialization fields below when exposed. Existing native initialization events lacking `sourceEngine` MUST remain readable and MUST NOT be rejected solely for that absence.

| Field | Value and availability |
| --- | --- |
| `sourceEngine` | String naming the originating integration, such as `claude`, `copilot`, `codex`, `gemini`, `pi`, `opencode`, `goose`, a named plaintext sample, or `custom`. A custom delegation can retain the detected parser's engine label as described in 7.6. |
| `model` | Source-dependent model string. |
| `sessionId` | Source-dependent native session or thread identifier. |
| `cwd` | Source-dependent working-directory string, unchanged. |
| `tools` | Source-dependent array of tool names or native tool descriptions; item structure is preserved. |
| `mcpServers` | Source-dependent array of native server descriptors, including status and diagnostic fields. |
| `slashCommands` | Source-dependent array of native slash-command values. |
| `modelInfo` | Source-dependent native model metadata object, including billing information. |

Legacy mappings include `session_id` → `sessionId`, `mcp_servers` → `mcpServers`, `slash_commands` → `slashCommands`, and `model_info` → `modelInfo`. Empty collections supplied by the source are retained; missing collections do not require fabricated inventories.

### 4.2 `user.message`

**T-UAS-010 — User content.** A supported user prompt MUST map to `user.message` with its exact text in `data.content`. User text and tool results in the same legacy user entry MUST be converted separately in content-block order. A tool result MUST NOT be misclassified as a user prompt. Existing native events with no exposed content MUST remain acceptable.

| Field | Value and availability |
| --- | --- |
| `content` | String when source text is available; native structured content remains supported without destructive string coercion. |

Retention in the trace does not authorize publication in a summary; Section 8.1 defines the separate presentation boundary.

### 4.3 `assistant.message`, `assistant.reasoning`, and `assistant.refusal`

**T-UAS-011 — Assistant channels.** Supported assistant text MUST map to `assistant.message`, and supported reasoning or thinking text MUST map to `assistant.reasoning`, with exact text in `data.content`. Normalizers MUST NOT merge these channels or reinterpret session/provider errors as ordinary assistant answers.

| Event | Field | Value and availability |
| --- | --- | --- |
| `assistant.message` | `content` | Source text string; native structured content remains supported. |
| `assistant.reasoning` | `content` | Source reasoning/thinking string when available. |

Older readers' bare `reasoning` alias can remain a compatibility input. New canonical reasoning events use `assistant.reasoning`. Reasoning is not required from an engine that does not expose it.

**T-UAS-069 — Structured policy refusals.** A supported provider refusal or content-filter signal MUST map to `assistant.refusal`, not an empty or successful `assistant.message`. Parsers MUST NOT infer a refusal from natural-language disclaimers, arbitrary nested user/tool payloads, permission denials, authentication errors, or token truncation. A refusal is a provider response observation, not evidence that the workflow failed or the request actually violated a policy.

| Field | Value and availability |
| --- | --- |
| `reason` | Required: `refusal` for an explicit model/classifier refusal; `content_filter` for provider-filtered output. |
| `content` | Exact refusal text or available filtered partial text. Absent when unavailable; an explicitly empty string remains empty. Supplied native structured content or `null` remains supported without destructive coercion. |
| `policyCategory` | Anthropic `stop_details.category`, when supplied; string or `null`. Categories remain open to future provider values. |
| `explanation` | Exact Anthropic `stop_details.explanation`, when supplied; string or `null`. Its wording is not a classification signal. |
| `partial` | `true` for an observed OpenAI streaming fragment; absence does not imply that a truncated log contains the complete refusal. |

Supported signals follow the [OpenAI Chat API reference](https://developers.openai.com/api/reference/resources/chat), [OpenAI Responses streaming reference](https://developers.openai.com/api/reference/resources/responses/streaming-events), and [Anthropic refusal specification](https://platform.claude.com/docs/en/build-with-claude/refusals-and-fallback):

| Transport | Refusal evidence |
| --- | --- |
| OpenAI Chat Completions | A string `choices[].message.refusal`, a `refusal` content part, or `choices[].finish_reason: "content_filter"` even with null or absent message text. Streaming `delta.refusal` is partial evidence. |
| OpenAI Responses | Output-message content parts with `type: "refusal"` and string `refusal`; `response.refusal.delta`/`done`, refusal-bearing content-part/output-item events, and terminal response snapshots; `incomplete_details.reason: "content_filter"` distinguishes filtering from token limits. |
| Anthropic Messages / Claude SDK | `stop_reason: "refusal"` in an assistant response, including a Claude SDK assistant envelope or streaming `message_delta.delta`. Available policy details and usage remain retained. |

Refusals can be successful HTTP responses with no content. The transport does not identify the model provider: the CI sample in `fixtures/provider_refusals.cjs` is a Claude response delivered through OpenAI-compatible Chat Completions. Existing native model, request, timestamp, and source metadata MUST remain preserved. A streamed Claude refusal marker can follow already observed partial text; the later assistant snapshot's fields MUST be merged onto the existing refusal event, and implementations MUST NOT emit a duplicate refusal for the same logical response. Readers MUST label refusal observations distinctly and MUST NOT treat partial text as a completed answer.

The sanitized CI fixture comes from [githubnext/gh-aw-rai run 37397159182](https://github.com/githubnext/gh-aw-rai/actions/runs/37397159182), agent artifact `sandbox/agent/logs/process-1791248741707-407.2.log`. It captures `finish_reason: "content_filter"` with both `message.content` and `message.refusal` null. `provider_refusal.test.cjs` covers that shape, provider-documented variants, false positives, metadata, accounting, canonical round trips, and publication.

### 4.4 `tool.execution_start`

**T-UAS-012 — Tool start.** An observed invocation MUST map to `tool.execution_start`. The parser MUST preserve the supplied `toolCallId`, `toolName`, and `input`, and the optional `command` and `mcpServerName` fields when present. It MUST NOT replace valid non-object inputs with `{}` or change canonical tool names merely to satisfy a renderer.

| Field | Value and availability |
| --- | --- |
| `toolCallId` | Source-dependent native correlation identifier; typically a string. |
| `toolName` | Source-dependent tool name, unchanged. |
| `input` | Source-dependent JSON-compatible argument value, usually an object; arrays and primitive arguments are supported. |
| `command` | Optional source command string, including Copilot's top-level command form. |
| `mcpServerName` | Optional native MCP server name; an explicitly empty string is preserved. |

Native `parameters` remains a reader alias for `input`. If both are present, readers prefer `input` by property presence, not truthiness, and preserve both.

### 4.5 `tool.execution_complete`

**T-UAS-013 — Completion payload.** An observed completion MUST map to `tool.execution_complete` and preserve the fields below when supplied or unambiguously mapped. Structured outputs and native `result`/`error` values MUST remain structured. Values such as `false`, `0`, `null`, and `""` MUST NOT become `"success"`, an empty replacement, or an inferred error message.

| Field | Value and availability |
| --- | --- |
| `toolCallId` | Source-dependent original correlation identifier, including on orphan completions. |
| `toolName` | Source-dependent native name, or a name recovered from an exact matching start. |
| `success` | Boolean when the source completion establishes an outcome; omitted when unknown. |
| `output` | Source-dependent JSON-compatible output, not restricted to text. |
| `durationMs` | Source-dependent finite nonnegative duration in milliseconds, including zero. |
| `error` | Optional native error string, object, or other supported error value. |
| `result` | Optional native result value, including Copilot `result.content` arrays or JSON content blocks. |

`output` and `result` can coexist. A reader uses an explicitly present `output` for the primary output view, otherwise the native `result`; neither is destroyed. Source-specific result envelopes remain available to readers.

**T-UAS-014 — Explicit failures.** A mapping MUST represent an explicit failed status, error outcome, `is_error: true`, `isError: true`, or nonzero command exit code as a failure. Absence of an error is not, by itself, evidence of completion or success. A source protocol's documented successful completion or default non-error tool-result semantics MAY establish `success: true`; a start alone MUST NOT. If preserved native signals conflict, readers MUST treat an explicit failure as controlling and retain the contradictory source evidence.

### 4.6 `session.result`

**T-UAS-015 — Session accounting and diagnostics.** Exposed session accounting and session/provider errors MUST be represented in `session.result.data` using the fields below. Errors and permission denials MUST preserve their supplied entries and structures. Tool failures MUST remain on tool completions and MUST NOT automatically become session errors; a source that explicitly reports the same failure at both levels retains both observations.

| Field | Value and availability |
| --- | --- |
| `numTurns` | Source-dependent nonnegative integer count of actual turns under the source's turn definition. |
| `durationMs` | Source-dependent finite nonnegative session duration in milliseconds. |
| `totalCostUsd` | Source-dependent finite nonnegative USD cost. Zero is a reported cost, not absence. |
| `status` | Observed source outcome, such as Codex `completed` or `failed`; CLI turn completion does not imply task completion. |
| `sourceType` | Native terminal or diagnostic event type when mapped, such as `turn.completed` or `turn.failed`. |
| `usage` | Source-dependent object with token fields defined in Section 6 and preserved native additions. |
| `errors` | Source-dependent array of session/provider error strings or objects, including an explicitly empty array. |
| `permissionDenials` | Source-dependent array of native permission-denial records, including an explicitly empty array. |

Legacy mappings include `num_turns` → `numTurns`, `duration_ms` → `durationMs`, `total_cost_usd` → `totalCostUsd`, and `permission_denials` → `permissionDenials`. Other native result metadata remains supported. A result reports evidence; it does not assert that the task or session succeeded.

### 4.7 Runtime observation vocabulary

**T-UAS-061 — Essential runtime payloads.** Non-agent source records MUST use
namespaced event types and the essential `data` fields defined in Section 3.5.
RPC IDs, filter/block decisions, network statuses, provider accounting, and
steering messages MUST survive projection, including observed false/zero/null
values. RPC transport wrappers, opaque arguments/response bodies, and duplicated
timestamp fields MUST NOT be copied into known payloads. Projection MUST NOT
truncate retained values into display previews. Unknown runtime kinds MUST
remain `<component>.event` observations with their event kind, level, status,
message, reason, and request ID when supplied; native runtime logs remain the
source for opaque fields.

| Event type | Source observation |
| --- | --- |
| `agent.execution` | One aggregate execution/error observation for the main agent, as defined in Section 4.8. |
| `subagent.started`, `subagent.configured`, `subagent.completed`, `subagent.failed` | Cross-engine sub-agent lifecycle observations. `agentId` identifies the invocation; `data.invocationId` preserves an available engine invocation ID. Failures may include `errorCode`, including `SUBAGENT_MODEL_UNAVAILABLE`. |
| `subagent.request` | A sub-agent inference observation with `agentId`, `invocationId`, model, and the available input, output, cache-read, and cache-write token counts. It provides the per-request evidence used to correlate proxy accounting. |
| `prompt.system`, `prompt.user` | Split activation prompt file contents with source provenance, when available; omitted from default summaries. |
| `skill.inventory`, `skill.conflict` | Activation inventory of available skills and deterministic exact-name/scope conflicts from `skill_inventory.json`. Similar names are not automatically conflicts; conflict reports are advisory and do not assert which skill an engine selected. Absent inventory means unavailable evidence, not an empty installation. |
| `skill.invoked`, `skill.context_delivered_ref` | Native agent observations of selected skill and delivered context reference, respectively. Only explicit engine observations establish usage; skill contents and unrecognized payload fields are excluded from the unified projection. Engines without such events have unknown usage, not zero usage. |
| `mcp.rpc.request`, `mcp.rpc.response` | MCPG `REQUEST`/`RESPONSE` or `rpc_request`/`rpc_response`, with flat RPC metadata and error code/message. |
| `mcp.difc.filtered`, `mcp.guard.blocked` | DIFC and guard-policy diagnostics; no inferred successful tool outcome. |
| `mcp.tool_call`, `mcp.event` | Structured gateway calls or other gateway log messages. |
| `mcp.configuration` | Sanitized pre-startup inventory from `mcp-logs/configuration.jsonl`, including detection-phase gateways when present. `servers` retains only `serverName`, recognized `transport`, explicit boolean `required`, and string `tools` filters. An empty inventory or tool filter is preserved; absent settings are not inferred. URLs, container images, commands, arguments, environment, headers, credentials, gateway settings, policies, and unknown fields are omitted before persistence. Registered secrets are additionally redacted by the session artifact writer. This records configuration, not successful connections or available tools. |
| `firewall.http_access` | AWF network audit observations, including operational entries without a host. |
| `firewall.token_usage`, `firewall.model_routing`, `firewall.upstream_error`, `firewall.steering`, `firewall.event` | API proxy accounting, essential AWF model-routing selection/request records, identifier-only upstream error responses (T-UAS-082), token/time steering, tracker and other firewall messages. |
| `model.config`, `model.baseline_estimate` | Runner-written configured routing/model policy, budget, and baseline model (T-UAS-080), and the collector's counterfactual baseline repricing for routed runs (T-UAS-081). |
| `safe_output.request`, `safe_output.result`, `safe_output.error` | Requested operation identities, executed item identities, and available errors. Requests are not execution results. |
| `experiment.state`, `experiment.assignment` | Downloaded state and assignment observations, including historical state retained in the supplied snapshot. |
| `grader.manifest`, `grader.result` | Essential deterministic grader definitions/results, without scripts; grading does not invent event time. |
| `eval.result` | Evals JSONL observations, preserving answers, IDs, and observed timestamps. |
| `usage.report`, `execution.result`, `detection.result`, `workflow.info` | Existing accounting, execution evidence, detection verdicts, and run metadata. `workflow.info` retains available `cliVersion` (gh-aw), `awfVersion`, `mcpgVersion`, `engineId`, `agentVersion`, observed `model`, `requestedModel`, compact `modelRouting`, and `triggerType` from `aw_info.json`. `requestedModel` prefers `requested_model` and falls back to `model` for earlier metadata. Unavailable values are not inferred. |
| `workflow.job_result` | Conclusion's GitHub Actions dependency snapshot from `needs`, passed through `GH_AW_JOB_RESULTS`. Each record contains the stable `jobId` and its observed `result`: `success`, `failure`, `cancelled`, or `skipped`. Optional `durationMs` is the completed Actions job's `completed_at - started_at` in milliseconds, excluding queue time. Outputs and other dependency fields are excluded. Records have workflow component, conclusion phase, and `usage/aw_session.jsonl` provenance, with deterministic job-ID order and no invented timestamp. |
| `model_routing.outcome` | The harness's observed routing status, wire model, effective endpoint, selected endpoint, effort, applied effort, and failure code from `agent/awf-routing-outcome.json`; free-form detail is excluded. |
| `model.inventory` | AWF's runtime model list from `sandbox/firewall/models.json` and persisted `/reflect` inventory, selecting the first valid copy from `agent/awf-reflect.json`, then `sandbox/firewall/awf-reflect.json`. Agent and threat-detection inventories retain their source path and phase separately. Payloads contain provider names, AWF's endpoint `configured` state, full model ID lists, routing model IDs, supported endpoints, supported reasoning efforts, and context windows, and the observed `modelsFetchComplete` flag when supplied. Null or absent model and routing lists are unknown and omitted, never empty (T-UAS-075). Endpoint routing metadata takes precedence over the top-level fallback. Credentials, headers, URLs, and unrecognized fields are excluded. Missing inventory does not imply unavailable models; malformed sources emit collection warnings rather than empty inventory events. JSON parse failures produce one warning per source. |
| `model_endpoint.mismatch` | A single harness JSON record from `agent/model-endpoint-mismatch.json`, with the model/API mismatch cause and corrective action. |
| `model.resolution` | Runner-written records from `agent/model-resolution.json`, one per resolved role, with the engine, role, configured and resolved model, provider, status, and failure cause from the shared "resolve configured model" step (T-UAS-074). |
| `guardrail.daily_aic` | Activation's daily AI Credits decision (`status`, `exceeded`, and available numeric `total`, `estimated`, `threshold`). Omitted when the guardrail has no decision; an existing event is not duplicated. |
| `github_api.rate_limit` | Available GitHub API rate-limit JSONL observations retain the recorded source (`response_headers`, `rate_limit_api`, or `retry`), operation, resource, limit, remaining, used, reset, and retry state. `credentialSource` identifies a known GitHub Actions token, PAT, or app token where classified by the client; absent or unknown credential types are not inferred from quota values. Raw credentials and unrecognized fields are excluded. The collector prefers `github_rate_limits.jsonl` over its `usage/` copy to avoid duplicate observations. |
| `plugin.installation` | Successful plugin installations retain the pinned `plugin` reference, `repository`, `ref`, local checkout `path`, and `status: "installed"`, with the installation timestamp. CLI and directory-based engines record success after installation or staging; Claude records the checked-out directory made available through `--plugin-dir`, and Codex records success after marketplace registration and plugin installation. The collector prefers `plugin-installations.jsonl` over its `usage/` copy. Credentials and unrecognized fields are excluded; absent evidence does not imply an installation. |
| `session.collection_warning`, `session.collection` | Explicit collection diagnostics and coverage. |
| `session.format` | Leading collector-owned file-format metadata, distinct from source-native events with the same type. |

Job-result records cover only the conclusion job's direct dependencies, including
custom jobs when present in `needs`. They do not imply a final workflow result
or a result for the still-running conclusion job. Missing dependency metadata,
including historical reconstruction without this context, produces no job-result
records. Malformed metadata emits collection warnings without copying its
contents; valid adjacent job results remain available. These native Actions
results are distinct from agent execution exit codes and detection verdicts.
Duration collection reuses the conclusion failed-job reporter's existing outcome
listing through its `job_durations` step output. When no listing was produced,
the collector reads the current run attempt's paginated Actions jobs, using
`actions: read`. The reporter and summary inherit the same job-level
`GH_AW_JOB_DISPLAY_NAMES` inventory and explicit `GH_AW_JOB_TIMING_ELIGIBLE`
set, finalized after generated-job annotations from effective emitted names:
unnamed jobs use emitted job IDs, and static author overrides are retained.
Only compiler-confirmed static-name, non-matrix, non-reusable single jobs with
a unique exact API-name match supply timing. Dynamic-name and grouped jobs omit
timing even when only one cell appears in the current attempt. Prefix-colliding
names remain ambiguous on partial reruns. Skipped jobs and missing or invalid
timestamps also omit duration rather than reporting zero; a measured zero remains
valid. API failures emit a warning without discarding dependency outcomes.

Job-duration collection is always enabled; there is no frontmatter opt-out.
Consequently, every generated conclusion job requests at least `actions: read`,
even when both `safe-outputs.report-failed-jobs` and
`safe-outputs.report-failure-as-issue` are disabled. This expands the previous
permission model, where disabling both reporters could omit Actions access.
Existing `actions: write` grants required by configured safe outputs are preserved;
duration collection itself requires only read access.

**T-UAS-072 — Skill evidence and conflicts.** When an activation skill inventory
is present, the merger MUST include its deterministic installed-skill names,
relative sources, installation scopes, content digests, and declared purposes
with activation provenance. It MUST report exact-name collisions and conflicting
scopes without treating similar names as failures. It MUST NOT include skill
contents in these observations. Engine usage MUST be attributed only to explicit
native skill invocation or context-delivery observations; installation alone
MUST NOT be interpreted as a skill read. Engines without usage observations
remain unobserved rather than unused. Conflict reports are advisory and do not
imply that an engine selected the wrong skill.

`firewall.token_usage` and its `usage.report` alias expose the following
source-dependent fields: `provider`, `model`, `purpose` (for example `agent`,
`subagent`, or `routing_classification`), `agentName`, `callerSource`, `path`,
`xInitiator`, `requestId`,
`status`, `durationMs`, `aic`, `totalAic`, `premiumRequests`, `steering`, and
`usage`. `steering` is the AWF notice delivered on that request, retaining only
its `type` (`ai_credit`, `timeout`, or `token`) and `threshold` (80, 90, 95, or
99); unrecognized values are dropped. `firewall.steering` retains the event-log
`threshold` and maps `ai_credit_steering`, `token_steering`, and
`timeout_steering` events.
Consumers identify router and classifier traffic by `purpose`; a missing
`purpose` means unknown, not agent.

**T-UAS-070 — Firewall usage purpose.** A producer MUST preserve supplied
`purpose`, `path`, and `xInitiator` values on `firewall.token_usage` and
`usage.report` events. A consumer MUST treat a missing `purpose` as unknown and
MUST NOT classify the event as agent traffic by default.

**T-UAS-083 — Proxy caller attribution.** AWF v0.28.67 and later record the
sandbox's `x-awf-caller` declaration (`main` or `subagent/<name>`, at most 128
characters, matching `^(?:main|subagent/[A-Za-z0-9_.:/-]+)$`) as `purpose`
(`agent` or `subagent`), `agent_name`, and `caller_source: caller_header`; a
trusted AWF purpose such as `routing_classification` wins and is recorded with
`caller_source: awf`. A producer MUST preserve supplied `purpose`, MUST map
`agent_name` to `agentName` and `caller_source` to `callerSource`, MUST accept
the canonical camelCase names, and MUST NOT synthesize absent caller fields. The
caller declaration is unauthenticated attribution metadata only; it MUST NOT
change routing, policy, steering, budgets, token counts, or costs. When
attributing proxy requests to the main agent or a named sub-agent, a consumer
MUST prefer explicit caller fields over token, model, or time correlation; MUST
NOT assign an explicitly named record to a different agent or infer an explicit
main record as a child; and MUST keep its correlation fallback for records
without caller fields, where a missing `purpose` remains unknown. AWF-owned
purposes remain non-agent traffic. A harness MUST send `x-awf-caller` only where
the request's caller is known at a real process or request boundary and MUST NOT
send a provider- or process-wide `main` that sub-agent requests would inherit.

| Engine (pinned runtime) | `x-awf-caller` support |
| --- | --- |
| Pi 1.3.0 | Each Pi process sets the header in its provider extension's `before_provider_headers` hook: `main` in the parent (SDK/agent-core, RPC, and CLI paths) and `subagent/<name>` in each sub-agent process, whose declared name is passed in that process's environment. The hook is registered only when the AWF api-proxy is present (`AWF_REFLECT_ENABLED=1`). API selection and `models.json` are unchanged. |
| Copilot SDK 1.0.16 | None. `providers[].headers` apply to every request to a provider and `MessageOptions.requestHeaders` to a whole turn; built-in and inherited-model sub-agents share the session provider, and per-agent provider clones would change qualified model IDs. Requests remain unattributed. |
| Copilot CLI | None. `COPILOT_PROVIDER_HEADERS` and provider-registry headers are provider- and process-wide. |
| Claude Code | None. `ANTHROPIC_CUSTOM_HEADERS` is process-wide and shared by in-process sub-agents. |
| Codex 0.163.0 | None. Provider `http_headers`/`env_http_headers` are provider-wide; role overrides do not copy provider headers. |

**T-UAS-071 — Firewall steering notices.** A producer MUST preserve a supplied
`steering` notice type and threshold on `firewall.token_usage` and
`usage.report` events. A consumer MUST treat a missing `steering` field as "no
notice recorded", not "no notice delivered", because earlier AWF versions never
write it.

**T-UAS-062 — Observation semantics.** A merger MUST NOT sum overlapping agent,
firewall, or accounting observations to produce another session total. Readers
MUST scope tool correlation, result selection, and accounting by provenance
before applying agent-only accounting rules. A state snapshot or historical
experiment record is evidence of that snapshot, not a new action executed by
this run. A missing timestamp, grader result, eval result, or safe-output result
MUST remain unavailable rather than become success or zero cost.

**T-UAS-063 — Collection coverage.** The merger MUST append a
`session.collection` observation recording selected source paths, components,
phases, event counts, warning count, untimed source-event count, and absent
required-vocabulary components. Source absence and a present empty source MUST
remain distinguishable. Warnings MUST identify the source and cause without
printing malformed payloads or unredacted source text.
Source-event counts and the untimed source-event count exclude the generated
file-format header and collection diagnostics.

**T-UAS-070 — Model routing attribution.** When available, the merger MUST select
the agent job's final `agent/aw_info.json` ahead of activation and usage copies
and emit its workflow metadata as `workflow.info` with agent-phase provenance.
The event MUST retain observed `model`, prefer `requested_model` for
`requestedModel`, and project only status, source, provider, wire model, model,
effort, applied effort, effective endpoint, selected endpoint, mode, selected
ID, router version, and failure code into `modelRouting`. The collector MUST
also emit `agent/awf-routing-outcome.json` as `model_routing.outcome` in the
agent phase, retaining status, wire model, effective endpoint, selected
endpoint, effort, applied effort, and failure code. Free-form detail MUST NOT
be included in either compact routing payload. Absence of workflow metadata
MUST be reported in `session.collection.absentComponents` or by a collection
warning.

`firewall.model_routing` retains the essential selected routing record fields:
schema, stage, purpose, attempt, classifier model and effort, objective,
provider, labels, mode, classifier attempts, degraded-classification state,
selected ID/provider/model/effort, wire model, endpoint, router, latency, code,
request ID, routed outcome, deviations, requested model, and requested effort.
It also retains the following AWF metadata when the source record supplies it,
accepting each snake_case source spelling and its camelCase equivalent:

| Field | AWF source | Meaning |
| --- | --- | --- |
| `rankedChoices` | `ranked_choices` | Route choices in AWF order. Each entry has a one-based `rank` equal to its array position, `model`, and `effort` as supplied, including an explicit `null`. `id` appears only when supplied. |
| `eligibleChoices` | `eligible_choices` | Count of choices remaining after inventory and policy filtering. |
| `catalogueOverlap` | `catalogue_overlap` | Count of routing-table choices present in the live catalogue. |
| `selectedEndpoint` | `selected_endpoint` | Selected endpoint, including on request records. The existing `endpoint` field is unchanged. |
| `requestedEndpoint` | `requested_endpoint` | Endpoint requested by the harness. |
| `upstreamEndpoint` | `upstream_endpoint` | Endpoint sent upstream; never substituted from the requested endpoint. |
| `unavailable` | `unavailable` | Request-telemetry dimensions AWF did not observe, such as `model` and `effort`. It is not a list of unavailable models. |
| `status` | `status` | Observed HTTP status, independent of `outcome`. |
| `phase` | `phase` | AWF failure phase, such as `primary` or `bootstrap`; distinct from the provenance phase. |

The collector MUST NOT sort, deduplicate, truncate, or otherwise reorder ranked
choices, and MUST NOT infer counts from their length. Absent fields MUST remain
absent: no inferred count, manufactured endpoint, zero, empty array, or `null`
placeholder is added. Supplied zeros, `null` efforts, and empty arrays are
retained. Candidate entries MUST contain only rank, optional ID, model, and
effort; per-candidate predictions or explanations MUST NOT be recorded.
`conversation_sha256`, `detail`, `method`, `pathname`, their camelCase aliases,
and other free-form or payload-bearing fields MUST be excluded.

#### Model/endpoint mismatch

The collector MUST map the single JSON object in
`agent/model-endpoint-mismatch.json` to `model_endpoint.mismatch`, with provenance
component `model_endpoint`, source path `agent/model-endpoint-mismatch.json`, and
phase equal to the record's `startup` or `runtime` phase. Arrays, malformed JSON,
and records that violate the following contract MUST produce a collection
warning rather than a mismatch event.

The payload has required fields `category: "model_endpoint_mismatch"`,
`phase: "startup" | "runtime"`, and strings `configured_model`, `resolved_model`,
`wire_api`, `wire_api_source`, `detail`, and `fix`. `supported_endpoints` is a
required array of strings. `model` and `endpoint` are optional strings describing
runtime observations. Unknown source fields are excluded; unavailable optional
fields MUST NOT be inferred.

Default summaries MUST render the cause (`detail`) and corrective action (`fix`),
alongside the configured and resolved model, wire API and its source, and
supported endpoints. Failure reports MUST derive this context only from the
unified session, without rescanning raw agent or firewall logs or reading the
source JSON separately. Reports MUST use the `model_endpoint_mismatch` grouping
category and the `model_endpoint_mismatch_context` template placeholder, and
redact and bound diagnostic text before publication.

#### Model resolution

**T-UAS-074 — Configured model resolution.** The collector MUST map each record
in the JSON array `agent/model-resolution.json` to one `model.resolution` event,
in array order, with provenance component `model_resolution`, phase `agent`,
source path `agent/model-resolution.json`, and the record's array index. The
harness writes this file before it launches the engine CLI, so the events are
runner-written evidence, not engine output. Empty arrays, non-array JSON,
malformed JSON, and arrays with any record that violates the following contract
MUST produce a collection warning rather than model-resolution events.

Each payload has required non-empty strings `engine` and `role` (`main` or the
sub-agent name) and `configured_model`; `resolved_model` and `provider` are
strings or `null`; `status` is `resolved` (an alias resolved against the live
catalogue), `exact` (an exact id passed through unchanged), or `failed`; and
`cause` is `null` unless `status` is `failed`, when it is `model_unavailable`,
`catalog_unavailable`, or `unsupported_endpoint` and `resolved_model` is `null`.
`unsupported_endpoint` means the model is live, but its `/reflect`
`supported_endpoints` on the configured provider do not include the endpoint the
engine calls (for example `/responses` for Codex sub-agents), or the alias
resolves only to such models. Unknown source fields are
excluded; an absent record does not imply that a model was or was not resolved.

#### Routing analytics

**T-UAS-080 — Configured model policy.** The runner (`generate_aw_info`) writes
the compiler-wired `GH_AW_INFO_MODEL_CONFIG` and evaluated `GH_AW_INFO_MAX_AI_CREDITS`
to `aw_info.json.model_config` before the engine starts, so the record survives
routing selection, sub-agent resolution, and startup failures. After the agent
exits, the agent-phase token-usage step adds `baseline_model` from runner-written
evidence. The collector projects `runtime/aw_info.json.model_config` (or the
selected `aw_info.json` copy) as exactly one `model.config` event per run, with
`workflow` provenance and the aw_info source's phase and path. Sessions without
`model_config` contain no `model.config` event; an invalid record produces an
`invalid_model_config` collection warning rather than a partial event.

| Field | Writer and provenance | Meaning |
| --- | --- | --- |
| `routing.enabled` | Compiler → runner | Whether `engine.model-routing` is configured. `false` is an explicit unrouted run. |
| `routing.goal`, `routing.mode` | Compiler → runner | Configured routing objective and mode. Never the router's per-run classification (`workflow.info.modelRouting.mode`). |
| `routing.allowedModels` | Compiler → runner | Configured routing candidates as authored (exact ids or gh-aw aliases) without the `github-copilot/` prefix; absent when unset or expression-valued. |
| `routing.candidateModels` | Compiler → runner | Effective router pool sent to AWF (`routing.candidateModels`, or the allow list on AWF without it): `github-copilot/` ids and patterns with aliases expanded and narrowed by `models.allowed` and `models.blocked`, or the Claude/Codex family default. Absent when the router has no shortlist or a candidate is expression-valued. |
| `models.allowed`, `models.blocked` | Compiler → runner | Configured model allow/block policy sent to AWF, kept separate from the runtime inventory and routing candidate pool. Empty lists mean no restriction; the object is absent without the firewall or when a rule is expression-valued. |
| `maxAiCredits` | Runner | Effective per-run budget: the evaluated compiled literal or repository-default expression (`vars.GH_AW_DEFAULT_MAX_AI_CREDITS`, falling back to `1000`), or the admitted allowance. |
| `maxAiCreditsSource` | Compiler → runner; collector | `frontmatter` (explicit `max-ai-credits`), `default` (the repository-default expression), or `admission` when an admitted `guardrail.admission` allowance replaced the configured budget. Recorded from configuration provenance, never inferred from the value. |
| `baselineModel` | Runner (agent phase) | Concrete main model the run would use without routing, after alias resolution: the recorded `main` model resolution; for unrouted runs, `model_selection.resolved_model`; for routed Copilot runs, the configured model resolved against the run's alias map and AWF reflection. Never `auto` and never the routed selection. |

All values are identifier-shaped metadata; prompts, transcripts, secrets, and
free-form diagnostics MUST NOT be recorded. Every field other than
`routing.enabled` is optional: an absent field is unknown, never `false`, an empty
policy, or zero, while an explicitly recorded `false`, empty list, or number is a
configured value. Older sessions without `model.config` have unknown policy; their
absence MUST NOT be read as routing being disabled.

**T-UAS-081 — Baseline estimate.** For runs whose `model.config` records
`routing.enabled: true`, the collector emits one `model.baseline_estimate` event
with `collector` provenance, phase `conclusion`. Explicitly unrouted runs and runs
without `model.config` have no estimate; readers MUST treat a missing estimate as
unknown, not zero. The estimate reprices each agent-phase main-agent proxy request
(`firewall.token_usage`) at `baselineModel` with `computeInferenceAIC`, using the
request's recorded input, output, cache-read, cache-write, and reasoning tokens and
its `input_tokens_include_cache` semantics (provider default when absent). Each
`request_id` is counted once. Native session accounting copies and detection- or
evals-phase requests are never included.

Attribution uses only proxy-written and runner-written evidence: requests with
`purpose: routing_classification` are the classifier and are excluded; requests
AWF recorded as `routed: deviated` whose requested model matches a runner-declared
`aw_info.json.sub_agent_models` entry are sub-agent requests and are excluded;
requests AWF recorded as `routed: as_selected` that do not match a declared
sub-agent are main requests. Any other request — no request record, conflicting
records, a missing request ID, or an `as_selected` request matching a declared
sub-agent — is counted in `unattributedRequests`. `model_routing.outcome`,
agent-authored reports, differing models or endpoints, and `xInitiator` alone are
never attribution evidence. An undeclared sub-agent that inherits the selected
model is indistinguishable from the main agent in this evidence.

| Field | Meaning |
| --- | --- |
| `estimated` | Always `true`: a counterfactual estimate, not observed spend. |
| `baselineModel` | The `model.config.baselineModel` used for pricing; absent when unknown. |
| `aic` | Sum over priced main requests; absent when no request could be priced. |
| `mainRequests` | Main-agent requests attributed by the rules above. |
| `pricedRequests`, `unpricedRequests` | Main requests with and without baseline pricing and complete token counts. Unknown pricing is unpriced, never zero cost. |
| `unattributedRequests` | Agent-phase requests that could not be attributed. |
| `complete` | `true` only when `baselineModel` is known, at least one main request was observed, and no request is unpriced or unattributed. A priced subtotal with unpriced or unattributed requests is incomplete. |

#### Upstream errors

**T-UAS-082 — Identifier-only proxy errors.** The collector maps AWF
`upstream-errors.jsonl` records (`event: UPSTREAM_ERROR_RESPONSE`, written by the
proxy) to `firewall.upstream_error`, retaining only `requestId`, `provider`,
`model`, `httpStatus` (from `status`), and the identifier-only `errorCode`,
`errorType`, and `errorParam` that AWF extracted from the provider's error
envelope. `firewall.model_routing` request records likewise retain `errorCode`,
`errorType`, and `errorSource` (`upstream` or `awf`) from `error_code`,
`error_type`, and `error_source`. Identifiers MUST match
`^[A-Za-z0-9_.:-]{1,64}$`; other values are dropped, never truncated or recovered
by parsing error text. `response_body`, response headers, endpoints, messages,
and other payload fields MUST NOT be copied. Absent identifiers remain absent:
a record without `error_code` has an unknown code, not an empty one.

#### Model inventory

**T-UAS-075 — Model inventory endpoint state.** A `model.inventory` payload has
an ordered `endpoints` array and an optional boolean `modelsFetchComplete`. Each
endpoint has the following essential fields, in source order:

- `provider`: required non-blank string.
- `configured`: AWF's boolean endpoint state, `true` or `false`, when supplied.
  The collector MUST NOT infer it from model counts or fetch completion, and MUST
  leave it absent when the source omits it.
- `models`: optional array of non-blank model ID strings in source order.
- `routingModels`: optional array of entries in source order, each with a
  required non-blank `modelId`, optional non-blank `supportedEndpoints` strings,
  optional `supportedReasoningEfforts`, and optional `contextWindowTokens`.
- `supportedReasoningEfforts`: identifier tokens of 1–64 characters that start
  with a letter or digit and otherwise contain only letters, digits, `.`, `_`,
  or `-` (for example `low` or `xhigh`).
- `contextWindowTokens`: positive safe integer.

AWF writes `null` model and routing lists for unconfigured endpoints and for
configured endpoints whose model fetch is pending. The collector MUST omit such a
property rather than write `null` or an empty array, and MUST still publish the
other endpoints. Readers MUST treat an absent `models`, `routingModels`, or
`supportedReasoningEfforts` as unknown, never as an empty list or as evidence that
a model is unavailable. A supplied empty array MUST remain empty. An absent or
`null` context window MUST be omitted, never written as zero or inferred from
another model. A null model list is accepted only on an endpoint with a boolean
`configured`. An explicit endpoint-local `null` routing list MUST NOT inherit the
top-level routing list; only an absent endpoint-local list does. Snake_case
source spellings (`routing_models`, `model_id`, `supported_endpoints`,
`supported_reasoning_efforts`, `context_window_tokens`, and
`models_fetch_complete`) normalize to these names.

A source with a wrong collection type, a non-string or blank model ID, a
non-boolean `configured`, an invalid reasoning effort, or a context window that
is not a positive safe integer MUST produce one `invalid_model_inventory`
collection warning and no inventory event for that source; the collector MUST NOT
publish a partial or empty inventory in its place. Normalization MUST accept its
own output unchanged, survive a JSON round trip, and leave the source unchanged.
Each value comes from the recorded AWF source; credentials, headers, URLs,
pricing, candidate-completeness flags, source labels, and other unrecognized
endpoint or model fields MUST be excluded.

### 4.8 `agent.execution`

**T-UAS-067 — Unique execution diagnostics.** When an agent artifact contains an
observed process exit code, an engine/provider error, or a harness/compiler error
classification, the merger MUST emit exactly one `agent.execution` event for the
main agent. Readers MUST accept older files without this event and MUST reject
multiple aggregate execution entries. The serialization-format version remains
`1`; this is an additive vocabulary change.

| Field | Value and availability |
| --- | --- |
| `categories` | Required array of unique, nonempty strings containing observed harness/compiler classifications. Empty when no classification was observed. |
| `errorCodes` | Required array of unique native error/status codes: nonempty strings or safe integers. Numeric `0` and string `"0"` remain distinct. |
| `errorTypes` | Required array of unique, nonempty native provider error identifiers, terminal reasons, or fatal signal names. |
| `exitCode` | Source-dependent integer from 0 through 255 for the final execution. Zero MUST be retained. An unavailable code MUST be omitted, not inferred from a failed attempt or job outcome. |

Collections are sorted deterministically. Error codes sort lexicographically by
their string form, with numbers before strings when the forms are equal; native
code values and types remain unchanged. Safe integral JSON numbers such as
`1e3` and `1.0` MUST be accepted consistently by JavaScript and Go readers.
Equivalent numeric values MUST count as duplicates regardless of their JSON
spelling; negative zero and zero are the same numeric value.
Native error type identifiers retain their original spelling.
Multiple retries, repeated source records,
canonical events, and detector observations MUST NOT produce duplicate entries.
Tool failures and errors quoted in user/assistant messages or tool completion
`output`, `result`, or `error` payloads, including nested structured values, MUST
NOT become agent execution errors. Diagnostics emitted in child-agent context
(an observed caller `agentId` or nonempty parent-tool identity, including
`parentToolCallId` on a diagnostic) MUST NOT supply
main-agent classifications; subagent lifecycle target IDs alone do not establish
the emitting caller's context. Original native and canonical error events
remain available; the aggregate is not a replacement for their evidence.
Errors from earlier attempts MAY coexist with a final `exitCode: 0`; neither
the entry nor a nonempty error array asserts that the final execution failed.

The detector persists `agent-errors.jsonl` before artifact upload, retaining
classifications obtained from the live environment and structured firewall logs,
including step timeouts without a stdio signature. The collector merges that
evidence with canonical/native engine errors and supported stdio diagnostics.
Raw-text mining MUST use recognized engine diagnostic prefixes or anchored
startup error signatures, not arbitrary substring matches across conversation
text. Labeled plaintext conversation/tool-output blocks, fenced quotations, and
harness echoes of child output or commands MUST NOT supply classifications.
The live detector MUST use the same attribution boundary before persisting
stdio-derived classifications; environment and structured firewall evidence
retain their separate sources.
Exit-code precedence is the recorded `agent_execution_exit_code.txt`, an explicit
agent execution snapshot, a persisted diagnostic observation, then the last
anchored harness `done: exitCode=...` line. Intermediate attempt and tool exit
codes MUST NOT be substituted for the final code. Missing historical evidence
MUST remain missing; reconstruction MUST NOT use the current clock to invent a
past timeout.

`provenance.component` is `execution` and `provenance.phase` is `agent`. Its path
identifies the primary contributing source; other error evidence remains in the
trace or original artifact. A merger MUST NOT fabricate a timestamp for an
aggregate covering several attempts.

The compiler's current engine classifications and additional harness labels
are listed below. Readers MUST accept additional categories and native
codes/types without requiring a closed enumeration.

| Origin | Observed categories |
| --- | --- |
| Compiler error-detection outputs | `inference_access_error`, `mcp_policy_error`, `agentic_engine_timeout`, `model_not_supported_error`, `model_endpoint_mismatch_error`, `http_400_response_error`, `capi_quota_exceeded_error`, `invocation_cap_exceeded`, `max_cache_misses_exceeded`, `missing_model_pricing_error`, `shell_expansion_guard_rejected` |
| Harness retry and proxy guards | `capi_server_error`, `authentication_failed`, `max_ai_credits_exceeded`, `effective_tokens_limit_exceeded`, `permission_denied_limit_exceeded`, `model_policy_violation`, `awf_api_proxy_blocking_requests`, `goal_already_active` |
| Harness failure reasons and fatal signals | Observed `failure_reason` identifiers such as `harness_retry_path_invalid`, `cancelled_or_timed_out`, and `sandbox_runtime_crash`; crash exit codes preserve the corresponding native signal name. |

Invocation-cap exhaustion takes precedence over generic CAPI quota exhaustion,
as in compiler outputs. A post-result watchdog termination alone MUST NOT be
classified as an agent timeout. The existing MCP-derived
`ai_credits_rate_limit_error` and `unknown_model_ai_credits` signals retain their
separate runtime provenance; they are not fabricated from an absent detector
output.

## 5. Parsing and Normalization (Normative)

### 5.1 Tolerant record parsing

**T-UAS-016 — Adjacent valid records.** Input parsers MUST tolerate supported logs containing debug lines, non-JSON text, malformed JSON, null/non-object entries, and truncated records without discarding independently parseable, supported adjacent records. Recovery MUST continue after a bad record. Trimming a framing line to locate JSON MUST NOT trim string values inside the parsed record.

JSON array inputs and JSONL inputs remain supported where already recognized by the engine adapter. A malformed multi-line record need not be reconstructed, but complete following JSONL records remain recoverable. Recognized structured debug blocks and legacy text sections use their engine-specific framing rules.

**T-UAS-017 — Recognition versus parsing.** A parser MUST distinguish syntactically parseable JSON from supported input signatures. It MAY skip unknown raw engine records. If no supported records remain, it MUST return empty `logEntries` and the existing parser's empty-input or “unrecognized format” reporting, as applicable. A nonempty markdown fallback or a synthetic initialization record MUST NOT count as evidence of a supported format.

### 5.2 Mixed inputs and ordering

**T-UAS-018 — Per-record conversion.** Normalization MUST classify and convert each record independently. A mixed array or stream containing canonical native events and supported legacy records MUST retain both. A whole-array legacy/event detector MUST NOT cause either side to be dropped or to bypass necessary conversion.

**T-UAS-019 — Observation order.** A normalizer MUST preserve source-record order and content-block order when emitting events. It MUST NOT sort by timestamp, move all tools before messages, or reorder a completion to precede/follow a reconstructed start solely for rendering convenience. It MAY expand a single completed-item record into an observed invocation and completion at that record's position. A derived aggregate result can be appended after the observations it accounts for.

Native timestamps can disagree with arrival order. Array position, not inferred clock order, defines trace order. A source snapshot can supply previously unavailable message content; it does not authorize moving already observed execution events.

### 5.3 Text and streaming

**T-UAS-020 — Exact source text.** Normalizers MUST preserve source text exactly, including leading/trailing spaces, whitespace-only chunks, empty strings, newlines, carriage returns inside strings, Unicode, and literal escape sequences after JSON decoding. They MUST NOT trim, unfence, truncate, reindent, insert separators, or otherwise rewrite retained message, reasoning, command, argument-text, or textual output values.

**T-UAS-021 — Streaming observations.** Streaming chunks MUST remain in order. A parser MAY concatenate consecutive deltas for the same message and channel only by direct string concatenation, retaining all supplied metadata and chunk boundaries when needed for loss-preserving recovery. It MUST NOT concatenate across a tool/event boundary or mix separate messages. A final full-message snapshot MUST NOT duplicate text already emitted from its deltas; when deltas are incomplete, their observed text MUST remain available rather than being discarded for lack of a final snapshot.

An implementation can preserve native delta extension events and emit one corresponding core message, or map deltas to ordered core content fragments. A reader of the chosen representation distinguishes retained transport observations from additional message content; it does not count a retained snapshot as another answer.

### 5.4 Correlation and incomplete execution

**T-UAS-022 — Tool identity.** Supplied native tool IDs MUST be retained exactly on both mapped starts and completions. Pairing MUST use an available exact ID before any compatibility fallback. Conflicting supplied IDs MUST NOT be paired by tool name. A fallback for missing IDs MAY associate an unambiguous source-proven pair, but MUST NOT invent a canonical source ID or pair ambiguous concurrent calls.

A source-proven numeric step key can map to `stepIndex`, scoped by `sessionId`,
without fabricating a string `toolCallId`. Display-only compatibility views may
allocate IDs for these pairs; stored traces retain the native step identity.
An observed `inputTruncated` flag remains on a tool start when the source exposes
only a command preview; it is not a claim that the full command was captured.
An explicitly supplied `exitCode: null` remains null, indicating that the source
did not report an exit code, rather than success or failure.

**T-UAS-023 — Dangling calls.** A start without an observed completion MUST remain a dangling start. Parsers and readers MUST NOT synthesize a successful completion, successful output, or failed completion merely because input ended. Session-level errors MUST NOT be converted into completions of otherwise unresolved tools.

**T-UAS-024 — Orphan completions.** An orphan completion MUST remain available with its original ID, outcome, output, and metadata. A compatibility renderer MAY create a display-only placeholder for the missing invocation, but MUST NOT replace a supplied completion ID, invent executed arguments or commands, or append that placeholder to the canonical trace.

### 5.5 Determinism and preservation

**T-UAS-025 — Deterministic normalization.** Equal supported inputs and equal explicit parser options MUST produce semantically equal canonical arrays, including order and field values. Normalizing a canonical trace again MUST be idempotent. Normalizers MUST NOT use the current time, random values, or environment-dependent fabricated IDs to fill gaps.

**T-UAS-026 — No input mutation.** Parsers operating on supplied objects, converters, renderers, and telemetry adapters MUST NOT mutate input arrays, records, nested payloads, or metadata. Appending a result, merging text, adding a command to input, or computing usage MUST operate on implementation-owned output values. Reusing unchanged objects is permissible only if downstream operations cannot mutate the supplied input through that reuse.

**T-UAS-027 — Backward-compatible fields.** A normalizer MUST preserve supported optional fields and native additions instead of enforcing a closed whitelist. It MUST distinguish property absence from falsy values and MUST NOT flatten structured values for storage. A presentation projection MAY serialize them for display without modifying the originals.

**T-UAS-028 — Canonical results only.** Supported legacy `result` records MUST normalize to `session.result`. A canonical trace MUST NOT mix a bare `result` with event records. A derived partial accounting result MAY be emitted for observed usage, turns, or session errors without a terminal source record, but MUST NOT fabricate a terminal-success assertion or missing totals.

**T-UAS-029 — Supported-field round trips.** JSON serialization and reparsing of canonical events MUST preserve every supported field's value and structure, including native extensions and metadata. A conversion offered as a reversible legacy interchange MUST preserve the supported fields in Sections 3 and 4 across both conversion directions. A deliberately lossy renderer projection MAY omit nondisplayed data, but MUST retain the original canonical trace separately and MUST NOT claim that its display projection is a lossless round trip.

## 6. Usage and Session Accounting (Normative)

### 6.1 Canonical token fields

**T-UAS-030 — Names and aliases.** Newly mapped usage MUST use the canonical snake_case names below. Readers MUST also accept the camelCase aliases, preferring a present canonical property over its alias, including when its value is zero. Normalization MUST preserve supplied native usage additions and aliases; an existing native event using aliases remains supported.

| Canonical writer field | CamelCase reader alias | Meaning |
| --- | --- | --- |
| `input_tokens` | `inputTokens` | Source-reported input tokens. |
| `output_tokens` | `outputTokens` | Source-reported output tokens. |
| `reasoning_output_tokens` | None | Source-reported reasoning subset of output tokens; not an additional contribution to the total. |
| `cache_creation_input_tokens` | `cacheCreationInputTokens` | Source-reported cache-creation/write input tokens. |
| `cache_read_input_tokens` | `cacheReadInputTokens` | Source-reported cache-read input tokens. |

Aliases identify the same metric; they are not additional contributions. Engine-specific fields such as `cached_input_tokens`, `cache_write_input_tokens`,
`cached`, `cacheRead`, and `cacheWrite` are mapped only by the applicable source schema
in Section 7.

**T-UAS-031 — Numeric and cache semantics.** Mapped token counts MUST be finite nonnegative integers. Invalid numeric observations MUST NOT become invented zero counts; native invalid values MAY remain as diagnostic metadata. An adapter MUST preserve the engine's token-accounting meaning and MUST NOT assume that cache tokens are disjoint from input tokens. If a reader displays a computed total, it MUST identify the calculation and avoid adding cache subtotals already included in input.

This contract does not require a canonical `total_tokens` field or authorize estimating source token counts from character lengths. A supplied native total remains supported without reallocating it into unknown input/output categories.

The implementation's optional `usage.input_tokens_include_cache` boolean records
known source accounting: `false` permits adding separate cache contributions; `true`
means cache counts are already included in input. Without that evidence, the displayed
computed total is input plus output, with cache counts shown separately. A valid
source `total_tokens` takes precedence over a computed display total.

The implementation rejects aggregate token counts above `Number.MAX_SAFE_INTEGER`.
Its optional `usage.overflowed_tokens` array records which canonical fields became
unavailable, preventing subsequent contributions or result selection from restoring
an incomplete earlier subtotal. A later valid authoritative snapshot can replace
the unavailable field. This metadata survives JSON serialization; it is not a token
count.

### 6.2 Per-turn accumulation and snapshots

**T-UAS-032 — Cumulative per-turn accounting.** Distinct Codex per-turn usage reports and distinct Pi finalized-turn usage reports MUST accumulate into session usage, field by field, rather than retaining only the last turn. A field MUST remain absent if no report exposes it; missing values are not assertions of zero. Turn or response identities, when supplied, MUST prevent counting duplicate observations of the same report.

**T-UAS-033 — Snapshot reconciliation.** An adapter MUST distinguish per-turn contributions from cumulative or terminal snapshots using the supported source schema, not a heuristic such as “largest number wins.” A snapshot covering already accumulated reports MUST NOT be added to those reports. A later authoritative session snapshot MAY replace corresponding derived totals; other independently known fields MUST remain available. Retained native snapshots and multiple `session.result` observations MUST NOT be summed by readers or bootstrap.

For supported snapshots, the reader selects the latest authoritative supplied value for each metric in source order; a later record that omits a field does not erase an earlier known value. Errors and denials retain their separate observations. This selection works even if native extension events follow the result.

### 6.3 Turns, duration, cost, and errors

**T-UAS-034 — No invented totals.** An adapter MUST emit `numTurns` only from a source turn count or a count of source-defined finalized turns. It MUST NOT equate tool calls, user messages, assistant fragments, or inferred renderer turns with `numTurns`. Duration and cost MUST be included only when exposed by the source or exactly derivable from source values with documented units. Wall-clock parser runtime and default zero values MUST NOT replace unavailable session metrics.

In particular, Gemini `stats.tool_calls` is not a turn count. Codex completed turns and Pi finalized `turn_end` records can support an observed turn count, including for a partial session; that count is not proof of task completion. A bare total-token figure does not supply an input/output split or USD cost.

**T-UAS-035 — Diagnostic separation.** Source session/provider errors, including empty-content error turns and supported terminal error records, MUST remain separately available in `session.result.data.errors` rather than being hidden, downgraded to assistant answers, or merged into tool output. Distinct error observations MUST remain distinguishable. Deduplication MAY remove a repeated observation with the same supplied source identity, but MUST NOT silently remove separate retries merely because their error text matches.

## 7. Engine Mappings (Normative)

All mappings inherit Sections 3–6. The tables identify supported signatures and field relationships, not requirements that every engine expose every field. Existing native canonical events and supported mixed legacy/event records are accepted per record before engine-specific fallback.

### 7.1 Claude

**T-UAS-036 — Claude adapter.** The Claude adapter MUST support the recognized JSON array/JSONL legacy shapes below and native canonical events. It MUST retain user prompts, reasoning, metadata, initialization details, result accounting, and explicit tool errors without depending on the final array element being a result.

| Source signature | Canonical mapping |
| --- | --- |
| `type: "system", subtype: "init"` | `session.init`; map snake_case initialization names as in 4.1. |
| `type: "assistant", message.content[]` with `text` blocks | `assistant.message`, exact `content` from each `text`; preserve the message ID, model, and content-block position. |
| Same envelope with `thinking` blocks | `assistant.reasoning`, `content` from `thinking`. |
| Same envelope with `tool_use` blocks | `tool.execution_start`; `id` → `toolCallId`, `input` retained. MCP names `mcp__server__tool` map to `mcpServerName: "server"` and `toolName: "tool"`; the original name remains in the parser trace. |
| `type: "user", message.content[]` with `text` blocks | `user.message`, not discarded because the user envelope also carries tool results. |
| Same user envelope with `tool_result` blocks | `tool.execution_complete`; `tool_use_id`, `content`, `is_error`, `duration_ms` mapped. |
| Direct `type: "message", role: "assistant"` or `"user"` envelopes | The same content-block mappings as SDK assistant/user envelopes; only assistant usage contributes to session accounting. |
| `type: "result"` with recognized accounting/diagnostic fields | `session.result`; preserve source result metadata, expose `sourceType: "result"` and the terminal subtype as `status`. Explicit `is_error` or an error subtype takes precedence and maps to `status: "error"`. |
| `system/permission_denied` | `session.result.permissionDenials`; no inferred tool completion. Later terminal denial snapshots reconcile by observed tool and caller identity. |
| `assistant.error`, `system/api_retry`, or Messages API error | `session.result.errors`; native diagnostic/retry metadata remains on the canonical event, without a duplicate Claude-specific envelope. |
| `system/task_started` with `task_type: "local_agent"` | `subagent.started`; task ID identifies the target agent, with launcher tool, agent type, execution mode, and observed spawn depth. |
| Agent launch tool result with `agentId` and `resolvedModel` | Retain the tool completion and emit `subagent.configured` for the observed target model. |
| `system/task_notification` for an observed local agent | `subagent.completed` or `subagent.failed`; retain observed token/tool/duration counters, and mark stopped tasks as cancelled. Other local-agent statuses remain native observations rather than dynamic-workflow events. |
| Recognized non-init system status with textual message/content | Retain status metadata; use the established assistant-text compatibility view only for actual status text, not user prompts, permission-denial notices, or provider errors. |
| `system/thinking_tokens`, VCS state, compaction boundary, or status notices | `session.info`; reasoning estimates remain `estimatedTokens`/`estimatedTokensDelta`, never measured usage. VCS kind, branch, and working directory remain available. |
| `tool_progress` | Partial `tool.execution_update` with supplied tool identity and elapsed duration; not an inferred completion. |
| Tool summaries, authentication status, and rate-limit notices | `session.info` with source subtype and observed content/status payloads. Authentication errors retain the error mapping above. |

Claude `usage.output_tokens_details.thinking_tokens` maps to
`usage.reasoning_output_tokens` in parser traces and `usage.reasoningOutputTokens`
in the unified projection. Reasoning tokens are a subset of output tokens, not
an additional contribution to total tokens. Explicit canonical reasoning counts,
including zero, take precedence over nested provider details.

SDK assistant snapshots can contain a single finalized block rather than a full
response. Streaming reconciliation matches that block to its observed type and
tool identity when unambiguous, retaining its stream position, exact text, and
thinking signature. A divergent final text snapshot remains a canonical
non-delta observation; display views replace the preceding streamed text rather
than concatenating it. User, assistant, and reasoning messages retain available
attribution in the unified projection; user prompts remain omitted from default
summaries. Structured tool-result content stays structured, and an absent tool
outcome remains unknown.

Mapped stream envelopes and finalized snapshots remain metadata on the core
observations they describe; they do not introduce duplicated engine-specific
message events. Unknown controls and native extensions remain opaque.
Recognized stream-only boundaries use `assistant.message_update`, preserving
message identity and source subtype without introducing another answer.
Argument and signature fragments remain snapshots on their corresponding tool
or reasoning event. Supported historical Claude status wrappers migrate through
the same mappings when reconstructing or normalizing saved sessions.

Nested SDK sessions can share the root `session_id`. Available `sessionId`,
`parentToolUseId`, and caller `agentId` remain on messages, tools, initialization,
refusals, and results in both representations. A subagent lifecycle envelope's
`agentId` identifies its target; its payload context identifies the caller.
Lifecycle observations fall back to stored start or launch context for omitted
scope and launcher fields; incomplete observations do not erase known values.
Explicit notification context wins, including a null root parent. A changed
parent invalidates a stored caller unless the notification explicitly supplies it.
Caller identity is inferred from launcher observations only when unambiguous.
Message reconciliation and tool pairing use the complete caller scope, never
display names or unscoped IDs. Native IDs remain unchanged; legacy display copies
may allocate distinct IDs for collisions.

Assistant response usage is retained per nested scope, with a partial result only
when no terminal result was observed for that scope. Root accounting excludes
child results and lifecycle counters; isolated child conversations display their
own accounting. Nested initialization cannot replace the root model or metrics.
Summaries label child conversations separately and omit agent-launch prompts from
display copies without removing tool inputs from stored traces.

### 7.2 Copilot

**T-UAS-037 — Copilot adapter.** The Copilot adapter MUST support native `events.jsonl` records, recognized legacy JSON entries, structured debug response blocks, and recognized pretty-print tool/output records. It MUST preserve native extension events, IDs, `command`, `mcpServerName`, `result`, and metadata. A debug response containing a requested tool call but no observed execution result MUST produce a start without fabricated success.

| Source signature | Canonical mapping or interpretation |
| --- | --- |
| Dot-namespaced `type` with object `data` and optional native envelope | Core events retained; other native types retained as extensions. |
| `session.task_complete` with textual `data.summary` | Retain the native completion event and expose its source-proven final summary as assistant content without duplicating an already observed answer. |
| Recognized Claude-compatible legacy entries | Per-record legacy mappings, with `sourceEngine: "copilot"` on mapped initialization. |
| Framed `[DEBUG] data:` JSON response with `choices[].message` | `content` → assistant message, `reasoning_text` → reasoning, `tool_calls[].id/function` → starts; preserve argument text when it is not valid structured JSON. |
| Distinct debug response `usage.prompt_tokens` / `completion_tokens` | Map to input/output tokens and aggregate distinct response contributions. |
| Pretty-print tool markers `✗`, `●`, or `✓` in recognized CLI layout | Map observed tool records and their actual continuation output; outcome interpretation follows the recognized format's semantics. |
| Recognized usage/model footer and explicit `Turns:` | Preserve reported usage/model and explicit turn count; no tool-count fallback for turns. |

CLI/debug framing is removed, but retained source payload text is not trimmed. A native event stream does not need a synthesized result merely because its renderer can estimate conversation turns.

Retry sessions retain independent source provenance and tool-correlation scopes.
The bootstrap persists each observed session rather than only the last attempt;
final-attempt telemetry remains separate from the complete conversation evidence.

Copilot subagent lifecycle events retain the instance `agentId`, definition and
display names, parent agent identity, model, reasoning effort, execution mode,
model-selection source, and first dispatched model when supplied. The unified
projection also retains assistant request-correlation metadata (`apiCallId`,
`interactionId`, `turnId`, and `parentToolCallId`) and the main agent's reasoning
effort. `session.shutdown.data.agentMetrics` is exposed unchanged in
`session.result.data.agentMetrics`, including per-agent model request counts,
token counts, and nano-AIU credits. This is an authoritative snapshot, not an
additional contribution to session usage.

The unified projection compacts `session.shutdown` to observed lifecycle and
request counters; accounting details remain in `session.result`.
`session.task_complete` retains its supplied success flag without duplicating a
textual summary already mapped to `assistant.message`. Nontext summaries remain
available as supplied data. Refusals retain the same request and caller identity
fields as assistant messages.

Pi dispatches, resolved models, per-request token usage, terminal outcomes, and
invocation IDs are projected into the shared `subagent.*` lifecycle vocabulary.
`subagent.request` retains the observed model and token quartet; failure events
retain sanitized error text and any supplied failure code.

Audit and logs read sub-agent lifecycle and model evidence from
`usage/aw_session.jsonl`. Per-agent proxy credits are correlated from that
file's `firewall.token_usage` observations with sub-agent requests using model,
token counts, and observation order. Classifier credits are identified by
`purpose` and excluded from per-agent reconciliation. Structured per-model
actuals include only sub-agent requests, not main-agent or router traffic.
`agent-session.jsonl` is used only for older runs without a unified-session
artifact. `agent-stdio.log` is not a source of sub-agent rows.

**T-UAS-073 — Sub-agent rows by definition.** Audit `agent_usage` and
`subagent_costs` rows MUST be keyed by the agent definition recorded in
`agentName`, with `agent_type` remaining the role (`main` or `subagent`). An
audit row MUST combine all instances of one definition in a run, summing
instance and outcome counts, requests, tokens, and credits; effort is the shared
value or `mixed`. Models and `subagent_model_requests` rows MUST be resolved
from each instance's own evidence before rows are combined by definition.
`gh aw logs` MUST combine rows across runs by definition and
effort. `display_names` MUST list the sorted, distinct `agentDisplayName` values
from each instance's `subagent.started`, `subagent.completed`, and
`subagent.failed` events and per-agent metrics, matched by instance ID, and MUST
NOT use `subagent.selected`. When an instance has no `agentName`, its row MUST be
named by its display name without `display_names`. Served models MUST come only
from runner- or proxy-written evidence (the session's sub-agent records and
per-agent metrics, and AWF `token-usage.jsonl`), never from the agent-writable
`awf-routing-outcome.json`. They MUST omit provider prefixes, MUST NOT list alias
names such as `small`, and MUST NOT drop a concrete model ID because a declared
sub-agent pins it, including as the selected variant of an experiment. A dated
model ID that served requests MUST be kept as served evidence when it resolves
to its undated identity.

Subagent attribution is scoped to the final retry session; earlier source
records cannot contribute even when merged timestamps interleave them.
Malformed preferred artifacts produce diagnostics while the reader continues
to the next structured source before attempting legacy inference.

Step summaries display subagent instance IDs, definition and display names,
parent-agent identity, model configuration, per-model requests and token counts,
and per-agent credits. Credit percentages use only the complete per-agent
snapshot, not proxy or router totals. Snapshots replace earlier observations;
native and projected copies are not added together. Missing accounting remains
unavailable, distinct from observed zero usage. Publication applies the existing
redaction, escaping, and byte limits.

### 7.3 Codex

**T-UAS-038 — Codex adapter.** The Codex adapter MUST support recognized `thread.*`, `turn.*`, and `item.*` JSONL records and the existing recognized legacy text layouts. It MUST preserve native item/tool IDs, independent starts/completions, text/reasoning, explicit tool failures, and session errors. Distinct `turn.completed.usage` reports MUST accumulate as specified in Section 6.

| Source signature | Canonical mapping |
| --- | --- |
| `thread.started` with `thread_id` | Initialization/session identity; reported model/workdir metadata retained when available. |
| Supported `item.started` / `item.updated` / `item.completed` with `item.type: "agent_message"`, `"user_message"`, or `"reasoning"` | Message/reasoning observations; retain the source speaker even before content arrives, and distinguish partial text from full snapshots to avoid duplication. |
| Same item envelope with `item.type: "mcp_tool_call"` | Start/completion according to observed lifecycle; preserve item/call ID, `server`, `tool`, `arguments`, `result`, `error`, `status`. |
| Same item envelope with `item.type: "command_execution"` | Preserve ID, command, `aggregated_output`, status, and `exit_code`; nonzero exit is failure. |
| Recognized patch, web-search, or collaboration items | Standard tool observations with supplied inputs, outputs, identities, and lifecycle. Summary-only file changes supply an orphan `apply_patch` completion, not invented patch arguments or a start. |
| Supported tool `item.updated` and repeated completion snapshots | `tool.execution_update` retains partial output and invocation context without another execution or inferred completion. |
| `todo_list` items | `session.info` with the supplied todo items as structured content. |
| `turn.completed` with `usage` | Accumulate `input_tokens`, `output_tokens`, `cached_input_tokens` → `cache_read_input_tokens`, and `cache_write_input_tokens` → `cache_creation_input_tokens`; count distinct completed turns. |
| Supported `turn.failed`, top-level `error`, or completed error item | Session/provider errors in `session.result.errors`, retaining supplied error structure and metadata. |
| Legacy `thinking`, recognized tool/exec call and outcome lines | Exact payload text and independently observed tool lifecycle; no source ID if none is exposed. |
| Legacy `ERROR:` / recognized reconnect diagnostics | Session errors, separately from successful tool activity. |

An item carrying a complete invocation and result can expand to a start and completion at that item position, except `item.updated`, which remains a partial update even without a prior start. An unfinished item or a legacy call lacking an outcome does not supply a successful completion. Declined commands are failures and retain a reported unavailable exit code as `null`. Repeated or revised accounting snapshots reconcile before subsequent turn reports; overflow remains unavailable until a valid authoritative replacement. Recognized model headers and harness spawn metadata can supply a model, but arbitrary text does not establish a Codex session.

Supported message-item snapshots retain their native history on the selected
core message instead of adding `codex.item_snapshot` wrappers. Unknown items
remain extensions. Observed thread identity scopes messages, diagnostics, and
tools in the unified trace.

### 7.4 Gemini

**T-UAS-039 — Gemini adapter.** The Gemini adapter MUST support the recognized flat JSONL records below, preserving user messages, streaming whitespace, tool IDs, structured results, initialization metadata, and exposed result statistics. It MUST NOT map `stats.tool_calls` to `numTurns`.

| Source signature | Canonical mapping |
| --- | --- |
| `type: "init"` with model/session fields | `session.init`, including supplied `session_id`, model, and other source metadata. |
| `type: "message", role: "user"` | `user.message` with exact `content`. |
| `type: "message", role: "assistant"` | `assistant.message`; `delta: true` follows 5.3. |
| Compatible flat `type: "reasoning"` or legacy `thinking` content block | `assistant.reasoning`, retaining exact content. |
| `type: "tool_use"` with `tool_name`, `tool_id`, `parameters` | Start with native name, ID, and arguments. |
| `type: "tool_result"` with `tool_id`, `status`, `output` | Completion with native output type and outcome; preserve explicit error fields. |
| `type: "result"` with recognized `stats`/error metadata | `session.result`; map supplied `input_tokens`, `output_tokens`, `cached` → cache-read tokens, and `duration_ms`. |
| `type: "error"` with a message/error payload | Retain the diagnostic in `session.error`; severity `error` or an unspecified severity also exposes a separate `session.result.errors` observation. A warning remains a warning, not an inferred session failure. |
| Recognized non-init system status | `session.info`, preserving status text and source metadata without inventing an assistant answer. |

Native tool-call counts and other statistics remain compatible extension data. Turn count, USD cost, and cache-creation usage remain absent unless a supported source field actually supplies them.

`gemini_session.cjs` normalizes these observations behind the existing
`parseGeminiLog`/`transformGeminiEntries` entry points. Gemini stream statistics
are cumulative snapshots, including per-model diagnostic totals; they are not
per-turn contributions. `cached` is included in `input_tokens`, recorded by
`usage.input_tokens_include_cache: true`; per-model totals are retained without
adding them again. Failed terminal statuses without a separate error payload
retain their status as an explicit diagnostic. Permission-denial arrays remain
separate from tool errors.

Adjacent deltas coalesce only when their metadata envelopes are identical.
Differing native IDs, timestamps, channels, or additions retain separate core
fragments. Supplemental compatible inputs with an explicit `message_id`,
`messageId`, or legacy `message.id` can identify a full-message snapshot.
An unchanged snapshot adds no core content; an append-only snapshot adds only
its unreported suffix. A rewritten or shortened snapshot retires the matching
message/channel/block's earlier core fragments, retaining exact original source
envelopes in the authoritative core event's `data.observations`, and emits the
full authoritative core content at the snapshot position. These recognized
snapshots do not introduce Gemini-specific message wrappers. Tool observations and
unrelated identities remain in place, without duplicate or stale answers.
For a full legacy content-array snapshot, a rewrite, removal, reordering,
insertion before existing content, or extension before an unchanged sibling
replaces all visible text/reasoning fragments of that message; supplied blocks
are emitted in their authoritative array order.
An empty final array remains structured core `content: []`, not fabricated text.
Gemini's flat stream has no native message identity; a record's event `id` or
repeated anonymous text does not establish snapshot coverage.

### 7.5 Pi

**T-UAS-040 — Pi adapter.** The Pi adapter MUST support both recognized flat JSONL and v3 streaming records. It MUST preserve observed streaming content, execution events, IDs, source metadata, and provider errors even without a finalized message. It MUST accumulate distinct finalized-turn usage and MUST emit canonical `session.result`, not append bare legacy `result` for telemetry.

| Source signature | Canonical mapping |
| --- | --- |
| Flat `init`, `assistant`, `tool_use`, `tool_result` | Initialization, assistant content/deltas, starts, and completions, preserving native fields. |
| Flat `result.stats` | Map supplied input/output usage, `turns`, and `duration_ms`; retain other supported statistics. |
| v3 `session` with numeric source `version` and native `id` | Initialization with `sessionId` from the source ID, retaining timestamp/cwd/version as source metadata. |
| Recognized `message_start`, `message_update`, `message_end` / `turn_end.message.content` | Text, thinking, and `toolCall` observations; structured non-text blocks remain message content. Finalized content is a snapshot, not another copy of streaming text or atomic content. |
| `tool_execution_start` / `tool_execution_end` | Independent start/completion using `toolCallId`, tool name/arguments, `result`, and `isError`; keep orphan ends and dangling starts. |
| `toolcall_start` / `toolcall_delta` / `toolcall_end` message updates | One `tool.execution_start`, reconciled with partial argument text and finalized arguments. An orphan argument delta remains a partial `tool.execution_update`; boundary-only updates and repeated snapshots do not introduce message wrappers. |
| `tool_execution_update` | `tool.execution_update` with canonical `input`, partial `output`, correlation IDs, and `partial: true`, not a Pi-specific message. |
| Distinct `turn_end.message.usage` | Sum supplied `input`/`output`; map supplied `cacheRead`/`cacheWrite` to cache-read/cache-creation fields and `reasoning` to reasoning-output tokens. Reasoning is a subset of output, not additional total tokens. Include reported auxiliary inference usage once, preserving reported zero counts. |
| Finalized message `errorMessage`, top-level `error`, or RPC `extension_error` | `session.error` and `session.result.errors`, even when content is empty or no session header was emitted; repeated message snapshots retain one diagnostic observation. |
| Recognized `agent_end` or other terminal accounting | Reconcile any supplied session snapshot; do not add it again to per-turn totals. |
| `agent_start`, `agent_settled`, retry, compaction, queue, entry, and reasoning-effort observations | Engine-independent `session.*` lifecycle extensions retaining the supplied payload. |

Known historical `pi.*` observations in saved Pi sessions are normalized without
replaying them as new conversation or inference usage. The engine dispatcher
invokes the Pi adapter's saved-observation normalization hook; shared collection
and CLI code do not import Pi-specific migration logic.

| Historical observation | Normalized mapping |
| --- | --- |
| `pi.message_snapshot` | `session.info` with `sourceType: "message_snapshot"`, role/model/correlation metadata, and `usageSnapshot`. |
| Argument `pi.message_update` | `tool.execution_update` retaining the native update subtype, argument fragments or finalized input, correlation, and optional `usageSnapshot`. |
| Other `pi.message_update` | `assistant.message_update`, retaining supported boundary/update fields through the shared essential projection without inventing text; the canonical trace retains native additions. |
| `pi.tool_execution_update` | `tool.execution_update` with partial output and the observed input and identities. |
| `pi.error` | `session.error`. |
| Recognized Pi lifecycle or subagent wrappers | The corresponding engine-independent `session.*` or `subagent.*` events. |

`usageSnapshot` is repeated observation metadata, not additive accounting.
Existing final messages, tool outcomes, and `session.result` accounting remain
authoritative. Missing content in a compact historical snapshot is not recovered
from model/usage metadata. Unknown `pi.*` extensions remain opaque. The session
download command applies this normalization to published JSONL only when trusted
run metadata identifies Pi; it does not rewrite stored Actions artifacts.

The adapter can discover a reported model from a finalized turn without inventing an earlier model observation. When Pi reports `responseModel`, canonical message and error observations use that served model; the canonical trace retains the requested `model` and native thinking levels as message metadata. Child request observations also preserve reported reasoning-output tokens without adding them to parent usage. Presentation aliases such as `bash` → `Bash` do not change canonical tool names. Sources lacking duration or cost do not supply zero duration or zero cost.

### 7.6 Custom engines

**T-UAS-041 — Custom format detection.** The custom adapter MUST select a delegate from actual supported input signatures. Parseable JSON alone, any dot-containing type alone, a nonempty markdown result, or a delegate's fabricated initialization MUST NOT establish detection. Existing Claude-compatible and Codex fallback support MUST remain available for unprotected ingestion, and native canonical events MUST remain supported.

Recognized candidates include the native event envelope, Claude legacy message/init/result shapes, Codex known JSONL lifecycle/item shapes, Codex legacy tool/exec/thinking layouts with their actual framing, and the OpenCode and Goose signatures below. Mere namespace matches with no supported payload are insufficient. Signature detection uses valid supported records even when unknown or malformed records are adjacent.

Delegation is deterministic. Specific Goose signatures precede OpenCode signatures; native event recognition and Claude-compatible per-record mixed conversion precede broad Codex fallbacks. A Goose `error` record alone does not establish custom format detection. A custom adapter MAY support additional documented engine signatures, but those signatures follow the same preservation contract. A delegated initialization MAY retain the detected engine's `sourceEngine`; custom-origin metadata, when supplied, is preserved separately. A custom engine emitting its own mapped initialization uses `sourceEngine: "custom"`.

Named sample adapters also support captured plaintext from Aider, Crush, Cursor,
DeepSeek Harness, Kiro, and Pydantic AI. These adapters use engine-specific
framing, not an arbitrary raw-log fallback. They retain observed assistant text
and tool evidence, omit prompts and recognized wrapper diagnostics, and leave
unavailable native tool arguments, results, and accounting absent.
Catalogued canonical-producing adapters own their complete mixed stdout before
generic record extraction. Cursor and Crush retain a compatibility probe for
older definitions that return legacy entries; a canonical-producing definition,
including one returning an empty trace, remains authoritative. Generic message
preflight must not bypass its native tool-event mappings or restore rejected
records. Exact quiet output does not supply initialization,
tool executions, terminal success, or accounting merely because text exists.
Attributable startup errors remain `session.error`, not assistant answers.

When no supported signature is present, the existing custom “unrecognized format” result and empty trace are returned. A raw preview is not a normalized event and is subject to the privacy boundary in Section 8.

#### Protected declarative session ingestion

The optional boolean `engine.behaviors.execution.protected-session-capture`
defaults to `false`. Its configuration and runtime prerequisites are documented
in the [custom engine reference](/gh-aw/reference/third-party-agent/#protected-session-capture).
It requires a definition-owned `behaviors.log-parser`; the generic format
detection and tolerant recovery rules do not authorize fallback after protected
recorder verification fails.

**T-UAS-084 — Native source and collection completeness.** Protected ingestion
MUST use AWF-native host-only command output and validate its metadata, private
regular stream files, completion/drain state, aggregate byte counts, and command
outcome before parsing. Only the final started attempt's stderr MUST be passed to
the engine's `parseLog(logContent)`; stdout, presentation logs, sandbox-written
files, and earlier attempts MUST NOT supply substitute or concatenated session
evidence. Missing, inconsistent, incomplete, undrained, or truncated collection
MUST fail publication. The native aggregate capture bound is 64 MiB across both
streams and all attempts. A complete capture with a nonzero command exit remains
valid collection; its native exit status MUST remain authoritative over
sandbox-writable exit metadata.

**T-UAS-085 — Recorder authentication.** A protected engine MUST isolate recorder
keys and capture-bearing descriptors from same-UID tools, including descriptors
retained by dumpable ancestor processes. The engine-owned parser MUST verify its
authenticated framing, sequence continuity, and terminal completeness before
returning nonempty canonical `logEntries`. Native storage protection, parseable
JSON, a key prefix, or an unanchored first public-key header MUST NOT alone
establish producer authenticity. Invalid verification MUST NOT fall back to a
legacy parser or publish a partial verified-looking trace. The compiler's
explicit final `exec` does not replace the harness's responsibility to remove
remaining launchers and protect its recorder channel.

**T-UAS-086 — Structure-preserving protected publication.** Raw native capture
streams MUST remain outside published session artifacts. Publication copies MUST
apply configured workflow secrets and mask requests from either native stream,
including to summary copies; mask requests MUST be used solely for privacy rather
than session observations. Redaction MAY change schema-defined text and tool
payload keys and values, but MUST NOT silently rewrite event types, identities,
tool names, status, accounting structure, or other structural fields. The
sandbox-writable `agent-errors.jsonl` source and its temporary sibling MUST NOT
be published as session evidence. The verified native exit status MUST replace
any shared exit-code sidecar used by downstream collection. A structural
redaction collision or verification/publication failure MUST reject
publication, gate session-artifact uploads, and discard stale publishable
artifacts without following artifact symlinks. Cleanup failure MUST be reported
explicitly and MUST NOT make artifacts eligible for upload.

Protected session capture MUST NOT be enabled when workflow `evals` are
configured, because the secondary engine invocation consumes `evals.log` rather
than the native capture.

### 7.7 OpenCode sample

The unsupported OpenCode integration uses `run --format json` and the same
parser for Actions, unified artifacts, and local session reconstruction. Its
signature requires a string `sessionID` and a recognized event with an object
`part` or a structured `error`; unrelated JSON and diagnostic prose are not
assistant messages. The custom adapter also recognizes this signature.

| Source observation | Canonical mapping |
| --- | --- |
| First recognized record for a `sessionID` | `session.init` with `sourceEngine: "opencode"` and the observed session ID. |
| `text` / `reasoning` with `part.text` | `assistant.message` / `assistant.reasoning`, preserving whitespace. |
| `tool_use` with `part.callID`, `part.tool`, and `part.state.input` | `tool.execution_start` with native call ID, name, and input. |
| Tool state `completed` / `error` | Correlated `tool.execution_complete` with explicit outcome, output/error, and duration only when valid native start/end times exist. |
| Distinct `step_finish` parts | Accumulate observed turns, cost, native `tokens.total`, `tokens.input`, `tokens.output`, `tokens.reasoning`, and `tokens.cache.read`/`write` into `session.result`. |
| Structured `error` | Preserve `session.error` and terminal errors without inventing a turn count or successful outcome. |
| Harness `opencode.max_turns` | Retain the explicit budget event and report `maxTurnsHit`. |
| Native logfmt `message="server unavailable"` with a failed MCP server, or harness `opencode.mcp_failure` | Report the observed server name in `mcpFailures`; preserve harness events without interpreting arbitrary prose as failure evidence. |

OpenCode envelope timestamps use milliseconds and remain unchanged, including
when a completed tool part expands into invocation and result events. Duplicate
parts are identified within their session, not globally. Cache-read and
cache-write tokens are separate from the reported input count; reasoning tokens
are an output subset, not an additional contribution to total tokens.
Synthetic coverage lives in `parse_opencode_log.test.cjs` and
`opencode_workflow.test.cjs`; it is not evidence of a live smoke run.

### 7.8 Goose sample

**T-UAS-068 — Goose adapter.** An implementation declaring Goose support MUST
recognize `run --output-format stream-json` message, completion, and error records
and native canonical events. It MUST retain source message identities, observed
text and thinking, independent tool lifecycles, explicit failures, and available
cumulative accounting. The sample integration uses the same parser for Actions,
unified collection, custom delegation, and local reconstruction.

| Source observation | Canonical mapping |
| --- | --- |
| `message` with string `message.id`, assistant/user role, and content array | First recognized message supplies `session.init` with `sourceEngine: "goose"`; observed inference metadata can supply its requested model. |
| Content `text` / `thinking` | User/assistant message or assistant reasoning; same-message/channel deltas concatenate without trimming. |
| `toolRequest` with string `id` and structured `toolCall` | Start with native call ID, successful call's name and arguments; malformed requests produce coverage warnings rather than fabricated executions. |
| `toolResponse` with string `id` and valid success/error `toolResult` | Completion with native call ID, matched name, exact content or structured-only result, and explicit outcome; absent starts do not create invented starts. |
| `complete` with observed token fields | One cumulative `session.result` snapshot; map input/output/total/cache-read counts and cache-write → cache-creation, preserving valid zero counts and exposed `cost_usd`. |
| `error` with string error | Preserve `session.error` and terminal `session.result.errors`; recognized maximum-turn diagnostics retain their code on `session.error` and set `maxTurnsHit`. |
| Recognized extension-start warning or canonical `goose.mcp_failure` | Map the observed startup failure to `mcp.event` with `event: "extension_start_failure"`, `serverName`, and `status: "error"`; report `mcpFailures`, not arbitrary quoted error prose. Existing native extension records remain readable. |

`Message.created` uses Unix seconds. The parser retains the original envelope
and derives an ISO timestamp from valid source seconds for timeline ordering;
the original source seconds remain available in the canonical trace.
Distinct assistant message IDs supply the source-defined turn count only when
a terminal observation exists; an incomplete stream does not invent a result.
Tool IDs suppress repeated lifecycle observations, while completion usage is a
snapshot, never another per-turn contribution. Cache counts are included in the
reported input count (`input_tokens_include_cache: true`).
Synthetic protocol and collection coverage lives in `parse_goose_log.test.cjs`;
it does not alone establish conformance for every Goose version.

### 7.9 Cursor sample

The Cursor sample uses `--output-format stream-json` and a dedicated parser shared
by Actions and local reconstruction, following the
[Cursor output-format reference](https://cursor.com/docs/cli/reference/output-format).
Recognized initialization, user/assistant
text, reasoning, structured refusals, tool lifecycles, errors, and terminal
results use the standard `session.*`, message, tool, and usage vocabulary.
Supplied identities, exact text, structured payloads, partial markers, and
zero/false/null values survive; repeated partial/final snapshots reconcile
without duplicate answers or tool executions. Native canonical extensions remain
opaque.

Historical `text` output supplies only the captured final answer, not a full
conversation. Its blank lines and final newlines remain exact. Supplied message
`sourceEngine` attribution survives unified-session compaction; absent attribution
is not inferred, and plaintext alone does not fabricate a `session.init`. CLI version
verification and infrastructure output are not assistant messages; prose does
not establish tools, turn counts, outcomes, or token accounting. The sampled
historical runs verify this text path, whereas stream-json mappings are
documentation-derived synthetic coverage, not evidence that the new invocation
succeeded in a deployed run.

## 8. Renderers and Bootstrap Telemetry (Normative)

### 8.1 Reader and presentation compatibility

**T-UAS-042 — Shared renderer inputs.** `generateConversationMarkdown`, `generatePlainTextSummary`, `generateCopilotCliStyleSummary`, and their information/statistics paths MUST accept canonical core events and native extensions without requiring a legacy result appended to the array. They MUST use supported initialization/result observations even when those are not the last records.

**T-UAS-043 — Downconversion boundary.** Renderers MAY downconvert to legacy entries for compatibility. A projection MUST preserve the available supported display values, including structured or falsy outputs, error status, and reported zero metrics. It MAY normalize display tool names, represent MCP names in the legacy `mcp__server__tool` form, and merge an exposed `command` into a display copy of tool input when that input lacks a command. It MUST NOT overwrite an explicitly supplied input command or alter the canonical data.

**T-UAS-044 — Truthful partial summaries.** Renderers MUST distinguish known success, known failure, and unknown/pending execution. A dangling call or ambiguous completion MUST NOT receive a success checkmark or count toward successful-tool statistics. Orphan completions MUST remain displayable with their observed outcomes. Unknown accounting MUST be omitted or marked unavailable, not presented as measured zero.

**T-UAS-045 — User-prompt privacy.** Retaining `user.message` MUST NOT newly expose user prompts in ordinary conversation summaries, plain-text logs, Copilot-style summaries, or unrecognized-format previews. Existing summary behavior that omits user prompts MUST remain the default. Publishing prompts requires a separate explicit authorized diagnostic view, with appropriate redaction; it MUST NOT happen as a side effect of normalization, generic extension rendering, or fallback raw-log inclusion.

A summary can shorten or format displayed assistant text and output, provided it does not rewrite the canonical values. Tool output and reasoning can also contain sensitive data; omission of user messages alone is not sufficient sanitization.

### 8.2 Bootstrap legacy telemetry projection

**T-UAS-046 — Canonical telemetry source.** `log_parser_bootstrap.cjs` MUST derive its legacy telemetry result from canonical `session.result.data`, applying the alias and snapshot-selection rules in Section 6. It MUST NOT require or prefer a bare legacy `result` in canonical `logEntries`, infer turns from renderer output, or sum multiple result snapshots.

The existing metric-reader projection is separate from the canonical trace:

| Canonical selected value | Legacy telemetry field |
| --- | --- |
| `data.numTurns` | `num_turns` |
| `data.usage.input_tokens` or accepted alias | `usage.input_tokens` |
| `data.usage.output_tokens` or accepted alias | `usage.output_tokens` |

The projection uses `type: "result"` only at the legacy telemetry boundary. Additional accounting fields can be projected when the receiving reader supports them; this does not authorize insertion of a legacy entry into the canonical array.

**T-UAS-047 — Safe telemetry enrichment.** The bootstrap projection MUST preserve known finite nonnegative values, including zero, and omit unavailable or invalid fields rather than replace them with zero. If no projectable metrics exist, it MUST NOT fabricate a metric result. Enrichment MUST be idempotent when a suitable existing legacy telemetry result is already present, write a standalone JSON record with valid newline separation when appending, and remain best-effort: an enrichment I/O failure MUST NOT fail agent execution or mutate canonical input.

**T-UAS-048 — Accounting is not success.** Bootstrap and other readers MUST NOT interpret `numTurns > 0`, a `session.result`, or token usage alone as evidence that a task succeeded or every tool completed. Existing operational policies can use observed turns as evidence of activity, but MUST keep that distinction from task/session success.

### 8.3 Safe publication and limits

**T-UAS-049 — Untrusted presentation.** Published summaries and telemetry artifacts MUST apply the applicable secret/add-mask redaction policy, treat source text as untrusted data, and prevent log payloads from escaping generated HTML/code containers or being interpreted as executable commands. Redaction, escaping, and display formatting MUST operate on publication copies, not mutate normalization inputs. Exact internal preservation does not require publication of secrets.

**T-UAS-050 — Explicit limits.** Implementations imposing parsing or display limits MUST report truncation or partial coverage rather than fabricate complete execution or accounting. A display limit MUST NOT truncate the canonical trace. Recovery from a malformed record MUST remain independent of display limits and MUST preserve valid adjacent records within the declared parsing limits.

### 8.4 Implemented summary views (Informative)

The Actions and console renderers share the same compatibility projection and
accounting selection. The Actions view starts with `### Agent session`, shows
statistics first, and places the detailed fenced trace in a collapsed
`<details>` section. The console view renders the same observations as plain text.
Neither view publishes user prompts or dumps unknown extension payloads.

The unified-session view shows source-scoped accounting followed by one full
timeline, interleaving assistant text, reasoning, tool starts and completions,
subagent activity, and runtime observations in merged record order. Tool output
appears at the completion, with source-local identities used for pairing even
when source timestamps disagree. Pairing prefers a string `toolCallId`, then a
non-negative safe-integer `stepIndex`; without either, only a unique pending
normalized tool name in the same source and conversation can match.
Each timed entry ends with an elapsed `+...s` suffix relative to the preceding
displayed clock; the first uses `+0s`. The display clock never moves backward:
regressing timestamps use `+0s` until the source clock exceeds the previous maximum.
Untimed observations are labeled rather than assigned a clock. Message bodies
retain multiline terminal-style formatting, and the Actions view fences the
overview and timeline separately. Stream fragments reconcile within each private
source and conversation in source-local order; the resulting message appears at
its first fragment's timeline position without changing the persisted records.
Literal command operators and HTML stay readable inside payload-safe fences,
rather than displaying HTML entities; source text cannot close those fences.

| Source event | Display |
| --- | --- |
| `session.init` | Merge supplied initialization observations for the display header: engine, observed model, session ID, cwd, tools, MCP status, slash commands, and model information. Missing inventories remain unavailable, not empty. |
| `user.message` | Retained in the canonical trace; omitted from default publication. |
| `session.info`, `session.error` | Whitelisted scalar diagnostic metadata and error code/type/message; generic `content` and structured metadata payloads are omitted. |
| `assistant.message` | Assistant answer, including supported structured native content. |
| `assistant.reasoning` | Distinct reasoning channel, not converted into an answer or tool output. |
| `assistant.message_update` | Stream boundary, message/block identity, partial markers, and observed usage; opaque update payloads are omitted. |
| `model.call_final_result` | Observed model, BYOK flag, stop reason, status, and usage, without the model response envelope. |
| `guardrail.daily_aic` | Observed budget status, exceeded flag, total, estimate, and threshold, including zero and false. |
| Permission, turn, mode, and skill lifecycle observations | Explicit lifecycle fields; permission prompts, system instructions, and skill contents are omitted. |
| `tool.execution_start` | All standard tools, including built-ins and bookkeeping tools; JSON arguments can be objects, arrays, scalars, null, or empty strings. A missing result is visibly pending. |
| `tool.execution_complete` | Source-proven pairing, separate output and error previews, known success/failure versus unknown outcome, duration, and an explicit missing-start label for orphan results. MCP text-block arrays display their text rather than JSON envelopes; structured errors prioritize their message and code. Empty observed output has an `[empty output]` marker. |
| `session.result` | Selected accounting, zero-valued metrics, cache counts, structured provider errors, and permission-denial records. Tool statistics separate failed, pending, and unknown outcomes. |

Conversation entries combine observed streaming text fragments within the same
source, message, and channel before formatting. Whitespace-only fragments remain
part of the combined text. Separate turns, reasoning channels, and tool boundaries
stay separate; native records and timestamps remain unchanged in the source trace.
Adjacent streaming observations in the displayed timeline also use one message
entry with the first observed timestamp; record counts still describe the source.
Compaction retains observed delta/partial flags, message/block identities, and
source/channel scope needed to group fragments; it does not infer absent IDs.

These are display projections, not reversible interchange conversions. Generated
display-only IDs avoid collisions with supplied native IDs, including empty IDs.
MCP names retain their namespace; the `Bash` presentation alias applies only to the
builtin shell tool. Legacy-only inputs keep their earlier compact tool filtering.

`generateConversationMarkdown` includes its Information section by default for
canonical input. Engine adapters use `includeInformation: false` when they append
that section themselves, avoiding duplicate accounting. This option does not
change the canonical result or the Actions/console accounting paths.

Publication creates redacted copies before shortening previews, so truncation
cannot expose the prefix of a recognized credential or registered add-mask value.
Private tool pairing keys remain available internally for correct correlation;
the final publication text is redacted as well. Source events are unchanged.
Formatted tool output uses a fence longer than its payload's backtick runs.
Untrusted inline HTML outside fenced code is escaped rather than treated as
summary structure.

Both text publication views have a 1000 KiB byte budget, below GitHub Actions'
1024 KiB hard limit. UTF-8-safe clipping and explicit notices distinguish partial
display from complete source evidence; the Actions budget reserves its generated
code fences and disclosure markup. Existing per-message and conversation-line
limits remain in effect. `agent_session_render.test.cjs` exercises field coverage,
tool states, namespace and ID handling, payload fences, and the measured byte
limit; bootstrap tests verify pre-truncation redaction in both publication sinks.

### 8.5 Unified-file publication views

**T-UAS-065 — Unified trace rendering.** Actions step summaries and action output
logs MUST support the runtime observation types in Section 4.7 in addition to
agent events. A full-file reader MUST validate the leading format header before
rendering. Format metadata MUST appear separately from timed observations.
Rendering MUST preserve file order, label each observation's component and phase,
and mark unavailable timestamps as untimed rather than fabricate a clock value.

**T-UAS-066 — Scoped agent statistics.** Readers of the unified file MUST scope
agent initialization, tool pairing, and accounting selection by provenance phase
and source path. They MUST restore source-local event order before deriving those
statistics. Coincident tool IDs in different source files MUST NOT pair across
sources. Nested caller scopes within one source remain separate, including when
session or tool IDs are reused. Gateway, firewall, accounting, grader, and eval observations MUST NOT
be added to agent snapshots as additional session usage. Private correlation
keys MAY remain internal until projection, but MUST NOT bypass final publication
redaction.

The conclusion collector publishes the already-redacted `aw_session.jsonl` to
both sinks after writing it. `generatePlainTextSummary`,
`generateCopilotCliStyleSummary`, and `generateConversationMarkdown` recognize
unified events. Their unified view shows file version, per-component record
counts, per-source agent statistics and conversations, and a chronological trace. Existing
agent-only inputs retain their earlier rendering behavior except that nested
conversations are labeled separately.

Caller `agentId` alone identifies a nested conversation when the source does not
provide a parent tool ID, as in Pi subagent streams. Its messages, tool pairing,
model, and accounting remain separate from the parent. A subagent lifecycle
envelope's target `agentId` does not identify the emitting caller. Readers must
not invent parent tool IDs to distinguish these scopes; a view containing only
one nested scope can select that scope's initialization and accounting explicitly.

Actions publishes this view once in a top-level `Unified session` disclosure,
using a fenced CLI-style transcript like the Claude conversation summary rather
than a Markdown table. Opening the disclosure shows source-local conversations
with paired tool outcomes; per-source statistics and the full chronological
timeline remain in separate, collapsed disclosures. The log parser does not wrap
this view in an additional `Agentic Conversation` disclosure. Gateway diagnostics
do not append a second unified event timeline, and the disclosure does not repeat
its title as a standalone heading.

| Observation | Unified display projection |
| --- | --- |
| Agent messages and tool lifecycle | Source-ordered conversations preserve multiline assistant/reasoning text and display tool arguments, output/error previews, pending/unknown/orphan outcomes, and scoped accounting before the runtime trace. The chronological trace includes correlation IDs and observed outcomes. User prompt records remain omitted. |
| MCPG | Method, server, RPC ID, tool name, observed responses/errors, and filter/block diagnostics. Raw RPC arguments and response bodies are omitted. |
| AWF | Network host/method/status/decision, observed token usage, steering messages, and tracker event names. |
| Safe outputs | Requested versus execution-recorded operations, target metadata, and structured error counts. Requests are not displayed as successes. |
| Experiments, graders, evals | Assignments/state, grader value/unit/outcome, implementation identity, diagnostic messages and metrics, and eval ID/answer/model with run and experiment attribution. Grader scripts, opaque diagnostics, and raw evaluation questions are omitted. |
| Execution, detection, collection | Recorded outcomes/verdict flags, detection job result/conclusion/categorical reason, source coverage, warnings, untimed counts, and absent components. No detector transcript or free-form reasons. |
| Unknown extensions | Type and provenance only; opaque payloads are not dumped. |

Redaction operates on decoded publication copies before preview clipping.
Untrusted text cannot close the Actions view's generated code fence or disclosure
container, and cannot inject workflow commands into output log lines. Both views
retain the measured byte budget and explicit truncation notices. Appending the
conclusion view also respects the remaining budget of an existing step-summary
file; if no space remains, logs still receive the view and publication emits a
warning. The complete compact JSONL artifact is not truncated or rewritten by
rendering. `unified_session_render.test.cjs` covers these projections, scopes,
privacy boundaries, version failures, and end-to-end publication.

## 9. Compliance Testing (Normative)

### 9.1 Test procedure and coverage

**T-UAS-051 — Requirement coverage.** A conformance test suite MUST exercise every applicable requirement ID, every declared engine signature, and the edge cases in the matrices below. Full-pipeline conformance MUST cover the engines declared in Section 2.2, shared helpers, all shared renderers, and bootstrap telemetry. A report MUST identify failures and unavailable coverage; current implementation tests alone are not a conformance declaration.

**T-UAS-052 — Preservation assertions.** Tests MUST compare ordered canonical arrays and nested values, not only markdown substrings. They MUST check normalization twice for idempotence, repeated equal inputs for determinism, JSON round trips for supported fields, and input deep equality before/after conversion and reading. Frozen input fixtures SHOULD be used to expose accidental mutation.

**T-UAS-053 — Isolated consumer tests.** Renderer and bootstrap tests MUST exercise canonical-only traces, mixed input normalization, missing results, multiple snapshots, reported zero values, and failures. Bootstrap tests MUST mock or isolate filesystem and summary operations, assert the legacy projection and unchanged canonical input, and avoid depending on real engine execution or production telemetry files.

Recommended execution is fixture parsing, canonical structural assertions, accounting assertions, renderer checks, and bootstrap checks. Record the class, engine formats, requirement IDs, and outcomes in the test report.

For the protected profile, parser/publication fixtures do not establish live
process isolation. Report Linux/AWF same-UID descriptor-attack coverage and the
engine's authenticated-protocol coverage separately, including unavailable
evidence; compilation alone is not a conformance claim.

### 9.2 Requirement compliance matrix

“Expected outcome” is the assertion, not a statement that the implementation currently passes.

| Requirement IDs | Test stimulus | Expected outcome |
| --- | --- | --- |
| T-UAS-001, T-UAS-002 | Class/engine claim and coverage report | Claims list supported scope; informative gaps do not weaken requirements. |
| T-UAS-003, T-UAS-004, T-UAS-008 | Full, partial, empty, and canonical-only parser outputs | Array of `type`/`data` events in `logEntries`; no session wrapper, added event version, or bare legacy result. |
| T-UAS-005, T-UAS-027 | Missing values versus `false`, `0`, `""`, `null`, `[]`, `{}` | Supplied values and native optional fields survive; absent fields are not fabricated. |
| T-UAS-006, T-UAS-007 | `vendor.progress` plus native IDs/parents/timestamps and extra payload metadata | Extension and metadata survive exactly, including between core events. |
| T-UAS-009 | Rich and sparse initialization records | All exposed init fields preserved; no invented model/session ID/inventory. |
| T-UAS-010, T-UAS-011 | User text and tool result in one entry; assistant text and thinking blocks | Ordered distinct user/tool/assistant/reasoning events with exact text. |
| T-UAS-012, T-UAS-013 | Primitive/array arguments; command and MCP name; object, array, false/zero/null outputs; native result blocks | Types and values retained; no truthiness fallback or storage stringification. |
| T-UAS-014, T-UAS-015, T-UAS-035 | Explicit tool error/nonzero exit, provider error, permission denial, conflicting native flags | Failure controls display; tool errors remain separate from session errors and denials. |
| T-UAS-016, T-UAS-017 | Debug lines, bad/truncated JSON, null entries, valid records before/after, unknown-only JSON | Supported adjacent records survive; unsupported-only input reports unrecognized format. |
| T-UAS-018, T-UAS-019 | Alternating native event, legacy message, extension, legacy result | Every supported record retained in order; result converted per record. |
| T-UAS-020, T-UAS-021 | Deltas `"hello"`, `" "`, `"world"`, `"\n"`; interrupted stream; final full snapshot | Exact `"hello world\n"` once; no whitespace loss, cross-event merge, or missing partial text. |
| T-UAS-022, T-UAS-023, T-UAS-024 | Concurrent same-name calls with IDs; unknown ID completion; missing start/end | Exact-ID pairing; mismatched/orphan/dangling events retained; no fabricated outcome/ID. |
| T-UAS-025, T-UAS-026 | Repeated normalization/rendering of frozen fixtures | Equal deterministic output; idempotence; no nested input changes. |
| T-UAS-028, T-UAS-029 | Legacy result; canonical JSON round trip; claimed reversible legacy conversion | Canonical result only; supported content, values, metadata, errors, and extensions survive promised round trips. |
| T-UAS-030, T-UAS-031 | All token names/aliases, canonical zero versus nonzero alias, overlapping cache/input, invalid numbers | Canonical names emitted; zero wins by presence; no invalid/default counts or cache double counting. |
| T-UAS-032, T-UAS-033, T-UAS-034 | Two per-turn reports, duplicate report, terminal snapshot, missing duration/cost/turns | Cumulative distinct contributions once; snapshots not added; missing totals omitted. |
| T-UAS-036–T-UAS-041 | Engine fixtures in 9.3 | Every declared engine signature maps according to Section 7. |
| T-UAS-042, T-UAS-043 | Canonical-only trace through all renderer variants, trailing extension after result | Initialization/statistics found; command/MCP display correct; originals unchanged. |
| T-UAS-044, T-UAS-045 | Dangling/orphan calls and a uniquely identifiable sensitive user prompt | Unknown calls not successful; prompt absent from all default summaries and previews. |
| T-UAS-046, T-UAS-047, T-UAS-048 | Canonical snapshots, alias usage, zero/missing metrics, existing telemetry, no trailing newline, failed append | Correct single projection; no double counting/defaults; safe line boundary; best-effort failure; activity not success. |
| T-UAS-049, T-UAS-050 | Hostile HTML/fences, mask values, oversized display, partial parse boundary | Safe/redacted publication, explicit truncation, canonical source remains unchanged. |
| T-UAS-051, T-UAS-052, T-UAS-053 | Conformance report and isolated test harness | Applicable IDs covered; structural/round-trip/purity assertions; no real production I/O. |
| T-UAS-054–T-UAS-064 | Six engine adapters; interleaved agent/MCPG/AWF/GitHub API/OTel sources; downstream snapshots/results; leading numeric format version; timestamp units, ties, regressions, and nanoseconds; malformed/missing logs; read/write failure; escaped secrets and symlinks | Complete compact `aw_session.jsonl`, pinned format header, provenance and payload preservation, deterministic source-preserving interleaving, unanchored tail, explicit coverage, safe atomic persistence, existing accounting unchanged. |
| T-UAS-065–T-UAS-066 | Unified file through both publication sinks and the conversation renderer; colliding source IDs; overlapping accounting; hostile/secret text; exhausted summary budget | Known runtime types remain visible, scopes and accounting remain independent, private prompts/payloads stay omitted, output is bounded and safely redacted, source artifact remains intact. |
| T-UAS-067 | Repeated native/canonical/detector error observations, live timeout evidence, final-zero and missing exits, malformed or duplicate aggregate records, quoted errors and tool failures | One deterministic `agent.execution` record with distinct native codes/types, stable categories, observed exit precedence, unchanged error evidence, and matching JS/Go reader validation. |
| T-UAS-068 | Goose stream messages/deltas, tool requests/results, cumulative completion usage, source-second timestamps, malformed records, error-only and partial sessions | Exact supported content and IDs, observed outcomes only, no duplicated snapshots or invented partial results, explicit diagnostics. |
| T-UAS-070 | Firewall token usage with supplied or absent purpose/path/initiator; `usage.report` alias; recorded AWF selection, request, and failure records in snake_case and camelCase, with and without ranked choices, pool counts, endpoints, `unavailable`, status, and phase, and with private fields | Supplied routing metadata survives projection; consumers treat absent purpose as unknown, not agent; ranked choices keep source order with one-based ranks; absent fields stay absent; private fields and nested candidate payloads are excluded; projection is idempotent. |
| T-UAS-071 | Firewall token usage with supplied, absent, or malformed `steering`; AI-credit, token, and time steering event-log entries | Type and threshold survive projection, malformed notices are dropped, steering events keep threshold and request ID, and absent notices remain unrecorded rather than undelivered. |
| T-UAS-073 | Copilot built-in and declared sub-agents with distinct display names, provider-qualified SDK model IDs, declared concrete and alias models, and sessions without `agentName` | Rows keyed by `agentName` with sorted `display_names`, one audit row per definition and one logs row per definition and effort, bare served model IDs without aliases, unchanged counts and credits, and display-name rows when `agentName` is absent. |
| T-UAS-074 | `agent/model-resolution.json` with resolved, exact, and failed records for main and sub-agent roles; empty, non-array, malformed, and contract-violating records | One `model.resolution` event per record in array order with `model_resolution` provenance and only the contract fields; invalid sources emit collection warnings and no events. |
| T-UAS-075 | Recorded AWF reflection from run 38995410124 with four unconfigured null endpoints; pending-fetch, legacy, and malformed-ID or capability-metadata variants | One inventory event per valid source with endpoint order, `configured`, model IDs, routing endpoints, reasoning efforts, and known context windows; null lists absent rather than empty; malformed sources warn and publish no inventory; projection is idempotent. |
| T-UAS-076–T-UAS-079 | Conversation/tool events, nested agent payloads, finite and nonfinite scalars, exact 128-record/16 KiB boundaries, Unicode and JSON escaping, giant lines with later accounting, scan-limit fragments, append races, malformed files, and initial/retry artifact IDs | Only bounded allowlisted metadata survives; explicit loss statistics; unchanged full artifact; deterministic output; exact UTF-8 size caps; bounded framing without whole-file reads; invalid sources fail explicitly; full-session retrieval uses verified artifact identity. |
| T-UAS-080 | Real compiled inputs and agent artifacts of routed SDK, unrouted CLI, and failed routed SDK runs; older sessions without the new metadata | One `model.config` per run with configured (not selected) routing, policy, budget/source, and resolved baseline; `workflow.info` harness, executing reference, and server; older sessions keep the fields absent. |
| T-UAS-081 | Routed run with main, sub-agent, classifier, and detection requests; replicated proxy copies; unknown pricing, malformed tokens, missing request records, unrouted runs | Only main requests repriced once with `computeInferenceAIC`; unpriced and unattributed coverage explicit; never a complete zero; unrouted runs omit the estimate. |
| T-UAS-083 | AWF v0.28.70 token records for `main`, `subagent/<name>`, absent, invalid, duplicate, empty-name, illegal-character, over-length, and exact-boundary headers, and AWF-owned classifier purposes; labelled and unlabelled main/sub-agent records sharing model, tokens, and time | Valid caller fields survive projection, rejected or absent fields stay absent, AWF purposes keep `callerSource: awf`; explicit callers determine attribution, unlabelled records keep the correlation fallback, and totals are unchanged. |
| T-UAS-082 | AWF upstream-error and failed request records from gh-aw-firewall#10211 writers with and without code/type/param/source | Supplied identifiers, status, provider, model, and request ID retained; bodies, headers, and messages excluded; invalid identifiers dropped. |
| T-UAS-084 | Forged stdout/shared logs, multiple native attempts, inconsistent metadata/files, undrained or truncated collection, and complete nonzero exit | Only final-attempt stderr reaches the parser; invalid collection fails closed; native exit status is retained. |
| T-UAS-085 | Valid, forged, replayed, interrupted, or missing-terminal recorder streams; same-UID tools probing recorder and ancestor `/proc/<pid>/fd` descriptors under Linux/AWF | Authenticated contiguous complete events only; keys and recorder channels inaccessible to tools; no legacy fallback or unsupported conformance claim. |
| T-UAS-086 | Configured secrets and masks from either stream, summary copies, nested tool-payload keys/values, masks colliding with `failure`, `session.result`, IDs, or accounting keys, forged `agent-errors.jsonl`, conflicting shared/native exit codes, stale files/symlinks, and failed publication/cleanup | Payload and summary secrets redacted; structure unchanged or publication rejected; native exit code survives downstream collection; forged sources excluded; raw capture not uploaded; session uploads gated on successful publication and cleanup; stale artifacts discarded without following links. |

### 9.3 Engine and integration fixture matrix

| Target | Required fixtures and edge cases | Principal assertions |
| --- | --- | --- |
| Claude | Legacy JSON array and JSONL; native events; mixed forms; user+result blocks; thinking; metadata; result followed by debug/extension; error/denial arrays | Complete field mapping, prompt retention without summary disclosure, result selection, native ID/metadata preservation. |
| Copilot | Native `events.jsonl`; legacy input; structured debug responses; known pretty-print layouts; command-only input; MCP server; JSON result blocks; incomplete debug tool call | Native extensions survive; no synthetic debug success/random IDs; display names do not rewrite storage; explicit turns only. |
| Codex | Known JSONL lifecycle/item shapes and legacy text; partial item start; preserved item IDs; failed command/MCP result; top-level/turn/item errors; two completed turns plus snapshot | Starts/ends retained in observation order; session errors separate; cumulative usage/cache counts; no last-turn-only accounting. |
| Gemini | Flat JSONL init, user/assistant messages, whitespace-only deltas, tool use/result, final stats, unknown/debug/partial lines; false/zero/object outputs | Exact streaming text; typed outputs; canonical result present; `tool_calls` retained but never used as turns. |
| Pi | Flat and v3 streams; session metadata; tool end before finalized turn; streaming-only partial turn; orphan end; reasoning; empty-content provider error; two finalized usage reports; terminal snapshot | No reorder for pairing, no lost partial tools/text, cumulative input/output/cache fields once, absent duration/cost, canonical result only. |
| Custom | Actual native/Claude/Codex signatures; deterministic delegate ties; mixed legacy/events; unrelated JSON, unknown-only dotted record, plain prose, delegate fallback markdown | Correct supported detection and delegation; no false format recognition from parseability or nonempty markdown. |
| OpenCode | Recognized session/part envelopes; tool lifecycle; distinct step finishes; structured errors; budget/server diagnostics | Source IDs and millisecond timestamps, accounting once, independent failures and absent metrics. |
| Goose | Message/channel deltas; source-second timestamps; repeated tool observations; structured-only results; zero/invalid cumulative metrics; startup and maximum-turn errors | Exact content, retained identities, snapshot accounting, observed turn counts, explicit malformed-record warnings and no fabricated outcomes. |
| Shared helpers | Per-record mixed conversion; JSON round trip; reversible supported-field interchange; missing IDs; structured/falsy values; source extras; frozen nested fixtures | Open vocabulary, exact values/metadata/order, deterministic/idempotent normalization, no mutation or fabricated results. |
| Renderers | All three summary functions and information/statistics paths; canonical-only and normalized mixed fixtures; command/MCP forms; structured outputs; trailing extension; partial execution; private prompt | Compatible display, known-zero versus unknown accounting, truthful success counts, no new prompt exposure, unchanged canonical trace. |
| Bootstrap | Canonical `session.result` only; token aliases; multiple snapshots; zero/absent/invalid values; preexisting legacy telemetry; line separation; redaction; I/O failure | Legacy metric projection derives from canonical data, remains idempotent/best-effort, and never appends legacy result to `logEntries`. |

### 9.4 CI-backed fixture provenance (Informative)

The initial implementation samples existing workflow runs; it does not trigger CI.
Downloaded `agent` artifacts remain local investigation data. Committed fixtures
retain protocol structure, observed identities, lifecycle ordering, and accounting
semantics while replacing prompts, repository content, commands, and other sensitive
payloads with harmless examples.

| Engine | Workflow and existing run | Extracted session | Regression corpus |
| --- | --- | --- | --- |
| Claude | [Smoke Claude success](https://github.com/github/gh-aw/actions/runs/36812703027) and [failure](https://github.com/github/gh-aw/actions/runs/36762044297) | `agent-stdio.log` | `fixtures/claude_ci_sessions.cjs`, `claude_session.test.cjs` |
| Claude | [Documentation review](https://github.com/github/gh-aw/actions/runs/37623157868), [dynamic workflow](https://github.com/github/gh-aw/actions/runs/37732499835), and [Smoke Claude](https://github.com/github/gh-aw/actions/runs/37089888775) | `agent-stdio.log` | `fixtures/claude_ci_normalization.cjs`, `claude_session_normalization.test.cjs`; direct API and streaming cases are synthetic |
| Claude | [Documentation review](https://github.com/github/gh-aw/actions/runs/37623157868) | Two background-agent conversations sharing the root `session_id` in `agent-stdio.log` | `fixtures/claude_ci_subagents.cjs`, `claude_subagent_session.test.cjs`; reused IDs and grandchild depth are synthetic |
| Codex | [Smoke Codex](https://github.com/github/gh-aw/actions/runs/36909965579) and [Daily Documentation Updater](https://github.com/github/gh-aw/actions/runs/36850958249) | `agent-stdio.log` | `test_data/codex_ci_smoke.jsonl`, `test_data/codex_ci_mcp.jsonl`, `codex_session.test.cjs` |
| Copilot | [Smoke Copilot success](https://github.com/github/gh-aw/actions/runs/36798242962) and [failure](https://github.com/github/gh-aw/actions/runs/36946387975) | `events.jsonl` in the failed run's `copilot-session-state/`; process and stdio logs in the successful run | `copilot_session.test.cjs`, `parse_copilot_log.test.cjs` |
| Pi | [Chronicle success](https://github.com/github/gh-aw/actions/runs/36884805242) and [Tree Map failure](https://github.com/github/gh-aw/actions/runs/36447274044) | `pi-streaming.jsonl` | `fixtures/pi_ci_stream.cjs`, `pi_session.test.cjs` |
| Pi | [Smoke success](https://github.com/github/gh-aw/actions/runs/37789661712) and [failed workflow](https://github.com/github/gh-aw/actions/runs/37865831889) | `pi-streaming.jsonl`, `agent-session.jsonl`, and `usage/aw_session.jsonl` | Complete native and saved traces replayed locally. Historical migration replaces 475/9,036 custom observations in each saved representation without changing conversation, tool outcomes, accounting, or provenance; the CLI normalizes downloaded copies, not stored artifacts. |
| Gemini | [Smoke Gemini success](https://github.com/github/gh-aw/actions/runs/36078916290), [spending-cap failure September 29](https://github.com/github/gh-aw/actions/runs/36504829912), and [September 27](https://github.com/github/gh-aw/actions/runs/36283760088) | `agent-stdio.log` | `fixtures/gemini_ci_sessions.cjs`, `gemini_session.test.cjs`, `fixtures/gemini_ci_lifecycle.cjs`, `gemini_ci_lifecycle.test.cjs` |
| AGY | [Smoke](https://github.com/github/gh-aw/actions/runs/37848575608) and [authentication check](https://github.com/github/gh-aw/actions/runs/37732637929) | Smoke `agent-stdio.log`, `agent-session.jsonl`, and `usage/aw_session.jsonl`; authentication receipt only, not a native transcript | `fixtures/agy_ci_sessions.cjs`, `parse_agy_log_conformance.test.cjs` |
| Aider | [Smoke success](https://github.com/github/gh-aw/actions/runs/37863370348), [downstream failure](https://github.com/github/gh-aw/actions/runs/37976723705), and [rate-limit failure](https://github.com/github/gh-aw/actions/runs/37080828156) | Each modern native/canonical agent trace has six records; the historical run has startup-framed rate-limit errors but no conversation capture | Repository-relative `pkg/workflow/testdata/aider_session/`; native-hook reasoning/refusal/partial/accounting cases are synthetic. |
| Cursor | [Smoke success](https://github.com/github/gh-aw/actions/runs/37866957846) and [failed workflow](https://github.com/github/gh-aw/actions/runs/37142216516) | `agent-stdio.log`, historical `agent-session.jsonl`, and `usage/aw_session.jsonl`; both Cursor processes exit zero | `parse_cursor_log.test.cjs`; historical answers preserve 297/268 UTF-8 bytes. Stream-json protocol cases are synthetic. |
| Kiro | [Smoke success](https://github.com/github/gh-aw/actions/runs/37862725262) and [failed workflow](https://github.com/github/gh-aw/actions/runs/37549851664) | CLI 2.27.1 compact `[tool]` framing and buffered final answers in `agent-stdio.log`; historical unified files did not recognize the engine | `parse_kiro_log.test.cjs`; native tool starts/completions are 11/11 and 9/11, including two anonymous orphan failures. |
| Crush | [Executed timeout](https://github.com/github/gh-aw/actions/runs/37866184524) and [Agent-skipped success](https://github.com/github/gh-aw/actions/runs/37553337234) | Executed native output contains infrastructure only; an appended zero-turn compatibility trailer is not native conversation evidence | `crush_session.test.cjs`; core-message/tool/refusal/accounting cases are synthetic, not live-run coverage. |

The AGY smoke workflow failed its checker after successful inference. Its v1.3.1
native stream exposes `conversation_id` and `step_index`, not string tool-call
IDs. These map to scoped `sessionId` and `stepIndex`; `user_input` and
`agent_response` map to messages, outputless `DONE` tools still map to completions,
and per-step/checkpoint usage remains separate `usage.report` evidence.
Usage observations retain their available `sessionId` and `stepIndex` without
fabricated request IDs.
Its `input_tokens` excludes cache-read tokens. An authentication receipt is not
evidence of successfully parsed inference content.

The two modern Aider traces reconcile exactly through native output, the declared
parser, canonical persistence, and unified projection. The older rate-limit run
supplies nine startup diagnostic blocks, not reconstructed messages or exact
accounting from rounded token/cost prose. Pinned v0.86.2 response hooks cover the
nonstreaming profile; a normal `main()` return of `None` does not supply terminal
success. Native reasoning, refusals, and tool-exception hook cases were absent
from the inspected traces.

Cursor's failed workflow failed its token-usage assertion, and Kiro's failed
workflow failed secret redaction; neither establishes a failed inference
process. Sampled Kiro stdout exposes no native prompts, reasoning, refusals,
tool outputs, token usage, IDs, or timestamps. Crush's job timeout conflicts with
its persisted exit code zero and execution state `started`; parsers retain those
observations without inventing a terminal outcome. No successful executed Crush
conversation was inspected. Pydantic AI and the generic-custom audit sessions
were archived before a completed handoff; this audit does not establish their
conformance.

Paths in the corpus column are relative to `actions/setup/js/`. Supplemental cases
cover features absent from the samples: Claude streaming wrappers, Codex terminal
failures that reached the engine, Pi terminal-metric snapshots, and malformed or
interrupted transport records. These are labeled synthetic rather than attributed
to a CI run. In particular, observed Pi streams expose their model on message
snapshots and do not expose a session duration. Readers display that observed model
without rewriting initialization or inventing zero duration.

The successful Copilot artifacts lack native session files; their legacy process
logs provide the success-path evidence. Additional usage, delta, and error cases use
documented SDK shapes. The failed Smoke Copilot workflow failed in downstream safe
outputs, not in its recorded agent session. A failed workflow does not by itself
establish a failed agent session (T-UAS-048).

The two Gemini spending-cap samples contain only initialization, a user prompt,
and a terminal provider error. Their reported zero token counts and duration are
retained; neither exposes a turn count, USD cost, assistant answer, reasoning,
or tool activity. The successful run `36078916290` supplies a sanitized
nine-observation lifecycle excerpt with original IDs and timestamps, assistant
fragments, native tool outcome shapes (including an empty successful output and
one failed tool), and exact reported full-session accounting. Its original trace
contains 208 observations: 33 assistant fragments, 86 tool starts, 86 tool
completions (85 successful and one failed), initialization, a user prompt, and a
terminal result. Full-session accounting is retained from that result, not
inferred from the excerpt's activity.

Five additional sampled Smoke Gemini runs
(`37083329446`, `36812703528`, `36798614887`, `36762043435`, `36745457680`)
contained no supported agent observations. Explicit-message-identity snapshots,
reasoning, permission-denial, mixed-input, and other source-unobserved regressions
remain explicitly synthetic. No CI run or paid engine was launched to generate
evidence.

The shared `agent_session.test.cjs` suite exercises all six adapters, including Gemini
and custom engines. `agent_session_telemetry.test.cjs` uses a mocked filesystem for
canonical-result telemetry. `npm run typecheck` checks both runtime JSDoc imports
and positive/negative TypeScript signature tests.

### 9.5 Merger verification (Informative)

The artifact-merger implementation loads the same existing Claude, Codex,
Copilot-failure, and Pi-success runs identified in Section 9.4. It executes
`writeUnifiedSession` locally against their downloaded agent artifacts and
available usage/safe-output evidence. It does not launch engines or trigger
workflows. Chronology, canonical event shapes, retained agent counts, and source
coverage are checked independently of rendered markdown.

`unified_session.test.cjs` exercises all six adapters with sanitized existing
fixtures and synthetic supplemental inputs. Integration cases combine all
requested components in one persisted file, compare full nested payloads and
source-native IDs, test timestamp units and Pi message timestamps, retain unknown
extensions, verify the first record's numeric file-format version (including
empty runs and native namespace collisions), and verify byte-identical repeated collection. Missing sources,
malformed adjacent records, empty authoritative files, read/write failure,
symlinks, and decoded-secret/add-mask redaction have separate cases.
`agent_session_telemetry.test.cjs` verifies canonical bootstrap persistence.
Compiler tests cover engine artifact paths, conclusion ordering, and the usage
upload path.

Live artifact checks for [Daily News run 38875907949](https://github.com/github/gh-aw/actions/runs/38875907949)
and [Daily Cache Strategy Analyzer run 38948382604](https://github.com/github/gh-aw/actions/runs/38948382604)
confirmed that each published `usage/aw_session.jsonl` contains exactly one
`experiment.assignment` matching its original experiment artifact, on both a
successful and a failed workflow. Collector regressions cover original and
usage-only assignment files, historical state separation, the `assignments`
name collision, schema validity, and byte-stable repeated collection. These
checks use completed runs; no workflow was dispatched.

`agent_execution.test.cjs` verifies the existing sanitized Claude API-error/retry
fixture, every current compiler detector classification, native nested provider
errors, guardrail precedence, watchdog exclusion, and final-zero exits after
failed attempts. JS collector and CLI tests verify singleton merging and
error-only startup sessions. Go session parsing validates the same arrays,
native code types, exit-code range, and singleton contract, and reconstructs
the same evidence through the embedded JS parsers.

Local reconstruction additionally checked downloaded artifacts from
[Claude API-error/retry](https://github.com/github/gh-aw/actions/runs/36762044297),
[Copilot](https://github.com/github/gh-aw/actions/runs/36946387975),
[Pi HTTP-400 failure](https://github.com/github/gh-aw/actions/runs/37151459460),
[Codex model failure](https://github.com/github/gh-aw/actions/runs/37100404405),
and [Codex success](https://github.com/github/gh-aw/actions/runs/37096302208).
Each produced one byte-stable aggregate on repeated collection. The Copilot
workflow failed downstream despite its observed agent exit code being zero;
the entry preserves that distinction. Claude and Pi lacked a final exit-code
observation, so their entries omit it.

#### Eval result recording: 2026-10-16 sample

Read-only inspection of completed live runs compared the `evals` artifact's
`evals.jsonl` with `eval.result` events in the `usage` artifact's
`aw_session.jsonl`. Local replay through the current collector preserved the
same results. No new workflows were dispatched.

| Workflow and existing run | YES | NO | UNKNOWN | Recorded answers |
| --- | ---: | ---: | ---: | ---: |
| [Smoke Copilot](https://github.com/github/gh-aw/actions/runs/38826296309) | 0 | 2 | 1 | 3 |
| [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/38980042038) | 0 | 0 | 3 | 3 |
| Total | 0 (0%) | 2 (33.3%) | 4 (66.7%) | 6 |

This is a six-answer sample from two runs, not a repository-wide distribution
or a workflow success rate. `UNKNOWN` remains an observed answer, not a pass.
Every result appeared exactly once with its ID, answer, model, run attribution,
available experiment assignments, timestamp, and eval provenance intact; raw
question text was excluded. A separate
[Smoke Copilot run with skipped evals](https://github.com/github/gh-aw/actions/runs/38901218081)
contained no fabricated eval results and contributed nothing to the denominator.
All three saved unified sessions passed schema validation. Both executed runs
had an unrelated `invalid_model_inventory` collection warning; eval result
collection was complete.

### 9.6 Mechanical contract checks (Informative)

`scripts/session_schemas.test.cjs`, relative to `actions/setup/js/`, checks
byte-identical regeneration, independent schema compilation, native passthrough
labels, raw engine mappings, and collected runtime payload families. A separate
fixture matrix exercises all 13 non-Pi engine routes through canonical artifact
precedence, unified schema validation, and CLI reconstruction. Existing sanitized
CI fixtures cover observed formats; Cursor and Crush cases use synthetic
canonical input, not native-format or latest-release verification. Their older
plaintext definitions emit a null model in initialization, which the canonical
schema rejects; adapter corrections are required rather than relaxing the schema.
Negative cases include invalid known payloads that would otherwise match the
open native union, unsafe/fractional token counts, legacy event names, absent
provenance, wrong file headers, invalid paths, ordering errors, duplicate execution
records, malformed lines, and noncompact JSONL. CLI tests check exit status and
diagnostics without printing payloads.

```bash
make check-session-schemas
npm --prefix actions/setup/js run typecheck
npm --prefix actions/setup/js run test:js -- scripts/session_schemas.test.cjs
```

`npm test` checks schema freshness before its behavioral suites.
The change-scoped `make agent-report-progress` gate also checks freshness when
session declarations, tooling, or generated schemas change. Generated schemas
are committed with declaration changes; changing prose alone does not regenerate
or change the serialization format. Native invalid diagnostic values may require
inspection rather than a successful writer-signature validation.

## 10. Implementation Gap Matrix (Informative)

These observations record the code **before** the implementation accompanying this
draft. They are a historical gap inventory, not claims about the updated code or an
exhaustive defect inventory. The normative sections and conformance suites define
the contract; current implementation coverage must be assessed separately.

| Area and source function | Observed behavior | Contract to implement |
| --- | --- | --- |
| Shared `isCopilotEventLogEntries` and forward conversion | Whole-array detection rejects legacy message types, returns native arrays unchanged, and otherwise ignores native events; legacy user text and source metadata are not mapped. | Per-record mixed conversion, user retention, open extensions, metadata preservation (T-UAS-006–T-UAS-010, T-UAS-018). |
| Shared forward/reverse converters | Whitespace-only text is skipped; IDs are synthesized; reverse conversion can replace orphan IDs, infer success, lose structured/falsy output, and synthesize a turn result. | Exact text and typed values, source IDs, partial outcomes, no fabricated accounting (T-UAS-005, T-UAS-013, T-UAS-020–T-UAS-029). |
| Claude `parseClaudeLog` | Canonical conversion is used, but information/max-turn handling reads the last raw entry and inherits shared user/metadata losses. | Canonical result selection independent of last record and loss-preserving mapping (T-UAS-036, T-UAS-042). |
| Copilot `parseCopilotLog`, debug and pretty-print paths | Native arrays can be appended to directly; missing results inherit renderer-derived turns. Debug calls receive synthetic tool results and time/random IDs; pretty-print text is trimmed and tools can stand in for turns. | Nonmutating deterministic conversion, observed outcomes only, exact text, source turn counts (T-UAS-023, T-UAS-025, T-UAS-026, T-UAS-034, T-UAS-037). |
| Codex JSONL and legacy conversion | JSONL keeps only the latest turn usage, rebuilds tool IDs, and handles completed items rather than partial starts. Top-level/turn failures are not mapped; legacy conversion makes unknown outcomes non-error results and trims/filters reasoning. | Cumulative accounting, independent lifecycle/IDs, explicit session errors, truthful partial traces (T-UAS-020, T-UAS-022, T-UAS-023, T-UAS-032, T-UAS-035, T-UAS-038). |
| Gemini transformations | User messages and results are omitted from canonical events; whitespace chunks are skipped; non-string output is serialized using a truthiness fallback. Information uses `tool_calls` as turns. | User/result mapping, exact deltas, typed outputs, source-specific metrics (T-UAS-010, T-UAS-013, T-UAS-020, T-UAS-034, T-UAS-039). |
| Pi transformations and stats | v3 sums input/output per finalized turn and retains provider-error text, but reconstruction depends on `turn_end`, reorders indexed results, omits orphan/unfinished observations and cache accounting, defaults duration to zero, and appends a bare legacy result. | Retain existing accumulation/error behavior while preserving streaming order/partiality, exposed cache fields, missing metrics, and canonical results (T-UAS-019, T-UAS-028, T-UAS-032–T-UAS-035, T-UAS-040). |
| Custom fallback selection | Claude success is based on nonempty normalized entries; Codex fallback success is based on nonempty markdown, even when a recognized input signature is absent. | Signature-based deterministic recognition and empty/unrecognized handling (T-UAS-017, T-UAS-041). |
| Shared formatters and bootstrap | Summary tool rows/statistics count missing results as success; numeric display paths use truthiness. Bootstrap enrichment finds bare `result`, defaults absent metrics to zero, and does not project canonical `session.result`; several paths assume the last entry has statistics. | Truthful summaries, known-zero handling, canonical result selection, canonical-derived telemetry projection (T-UAS-042–T-UAS-048). |

### 10.1 Conclusion artifact review

The following observations describe the state reviewed for version 1.1.0,
separately from the historical parser gap inventory above.

| Area | Reviewed gap | Implemented design |
| --- | --- | --- |
| Canonical engine trace | Raw native session files were already bundled in the agent artifact, but normalized `logEntries` existed only in memory. | Reuse existing native evidence; also persist redacted compact `agent-session.jsonl` for every engine to avoid repeated parsing. |
| Unified timeline | Display-only rows omitted payloads and untimed events; agent collector was Copilot-only. | Separate loss-preserving merger, using the established event model across six engine adapters. Existing display views remain unchanged. |
| MCPG and AWF | Raw logs remained in separate agent/detection artifacts with different timestamp units and layouts. | Preserve all selected JSONL records, normalize namespaces, attach provenance, select authoritative replicas, and sort observed timestamps. |
| Safe outputs and evaluations | Results were separate files or counts; usage upload preceded conclusion handlers. | Include requested/executed/error observations, experiment state, deterministic graders, evals, and accounting; publish usage after handlers. |
| Missing evidence | No unified coverage record distinguished missing, empty, untimed, or malformed sources. | Append explicit collection coverage and warnings; preserve untimed observations and fail visibly on I/O errors. |

Remaining boundaries are intentional: unobserved execution times are not
reconstructed, clocks are not corrected, and binary threat-detection runs do not
become fabricated agent conversations. Go audit readers consume selected
detection and execution evidence; `gh aw sessions download` exports published
or reconstructed unified files, rather than independently reimplementing every
JavaScript engine mapping. Actions step-summary and action-output
renderers consume the unified file as defined in Section 8.5. Evals contribute recorded
results/accounting, not an invented conversation transcript. Current collection
loads selected files and the merged array into memory; streaming external sort
for exceptionally large sessions remains future work.

## 11. References

### 11.1 Normative references

- **[RFC 2119]** S. Bradner, *Key words for use in RFCs to Indicate Requirement Levels*, March 1997. [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).
- **[RFC 8259]** T. Bray, *The JavaScript Object Notation (JSON) Data Interchange Format*, December 2017. [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259).

### 11.2 Informative implementation references

These links identify inspected implementation surfaces. They are not external engine protocol specifications and do not supersede the normative requirements.

- **[Shared]** [`actions/setup/js/log_parser_shared.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/log_parser_shared.cjs), particularly the event detector and conversion functions near lines 627–1075 at inspection time.
- **[Claude]** [`actions/setup/js/parse_claude_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_claude_log.cjs).
- **[Copilot]** [`actions/setup/js/parse_copilot_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_copilot_log.cjs).
- **[Codex]** [`actions/setup/js/parse_codex_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_codex_log.cjs).
- **[Gemini]** [`actions/setup/js/parse_gemini_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_gemini_log.cjs).
- **[Pi]** [`actions/setup/js/parse_pi_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_pi_log.cjs).
- **[Custom]** [`actions/setup/js/parse_custom_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_custom_log.cjs).
- **[OpenCode]** [`actions/setup/js/parse_opencode_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_opencode_log.cjs).
- **[Goose]** [`actions/setup/js/parse_goose_log.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/parse_goose_log.cjs).
- **[Schema tooling]** [`generate_session_schemas.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/scripts/generate_session_schemas.cjs), [`validate_session.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/scripts/validate_session.cjs), and the declaration maps in `actions/setup/js/types/`.
- **[Session downloads]** [`pkg/cli/sessions_download.go`](https://github.com/github/gh-aw/blob/main/pkg/cli/sessions_download.go) and [`actions/setup/js/session_cli.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/session_cli.cjs).
- **[Formatters]** [`actions/setup/js/log_parser_format.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/log_parser_format.cjs).
- **[Bootstrap]** [`actions/setup/js/log_parser_bootstrap.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/log_parser_bootstrap.cjs).
- **[Merger]** [`actions/setup/js/unified_session.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/unified_session.cjs), [`session_artifact.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/session_artifact.cjs), and [`collect_usage_artifact_files.sh`](https://github.com/github/gh-aw/blob/main/actions/setup/sh/collect_usage_artifact_files.sh).
- **[Driver]** [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/), related runtime logging context.
- **[SemVer]** [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html), document-version change classification.
- **[Tests]** Colocated `log_parser_shared.test.cjs`, `log_parser_bootstrap.test.cjs`, and the six `parse_*_log.test.cjs` files provide existing fixture/test integration points.

## 12. Appendices (Informative)

### 12.1 Appendix A: Complete canonical example

This example includes the seven non-refusal common agent types, native metadata, an extension, structured/false/zero outputs, explicit failure, and reported zero cost. It is a parser trace, not a unified file, so it has no collector header or provenance. All identifiers and metrics represent supplied example source evidence. The user prompt is retained internally but omitted from default summaries.

```json
[
  {
    "type": "session.init",
    "id": "event-1",
    "parentId": null,
    "timestamp": "2026-10-02T00:00:00Z",
    "data": {
      "sourceEngine": "copilot",
      "model": "example-model",
      "sessionId": "source-session-42",
      "cwd": "/workspace/example",
      "tools": ["bash", "lookup", "check"],
      "mcpServers": [{ "name": "catalog", "status": "connected" }],
      "slashCommands": ["/help"],
      "modelInfo": {
        "name": "example-model",
        "vendor": "example",
        "billing": { "is_premium": false }
      }
    }
  },
  {
    "type": "user.message",
    "id": "event-2",
    "parentId": "event-1",
    "data": { "content": "Check the example.\n" }
  },
  {
    "type": "assistant.reasoning",
    "id": "event-3",
    "parentId": "event-2",
    "data": { "content": "  Inspect the supplied evidence first.\n" }
  },
  {
    "type": "tool.execution_start",
    "id": "event-4",
    "data": {
      "toolCallId": "native-call-a",
      "toolName": "lookup",
      "mcpServerName": "catalog",
      "input": { "name": "example", "includeArchived": false }
    }
  },
  {
    "type": "tool.execution_complete",
    "id": "event-5",
    "parentId": "event-4",
    "data": {
      "toolCallId": "native-call-a",
      "toolName": "lookup",
      "success": true,
      "output": { "items": [], "count": 0 },
      "result": { "content": [{ "type": "json", "json": { "count": 0 } }] },
      "durationMs": 0
    }
  },
  {
    "type": "tool.execution_start",
    "id": "event-6",
    "data": {
      "toolCallId": "native-call-b",
      "toolName": "check",
      "input": { "name": "example" }
    }
  },
  {
    "type": "tool.execution_complete",
    "id": "event-7",
    "data": {
      "toolCallId": "native-call-b",
      "toolName": "check",
      "success": true,
      "output": false
    }
  },
  {
    "type": "tool.execution_start",
    "id": "event-8",
    "data": {
      "toolCallId": "native-call-c",
      "toolName": "bash",
      "input": { "cwd": "/workspace/example" },
      "command": "example-check --count\n"
    }
  },
  {
    "type": "tool.execution_complete",
    "id": "event-9",
    "parentId": "event-8",
    "data": {
      "toolCallId": "native-call-c",
      "toolName": "bash",
      "success": false,
      "output": 0,
      "error": { "code": "CHECK_FAILED", "message": "Example check failed." },
      "durationMs": 12
    }
  },
  {
    "type": "vendor.progress",
    "id": "event-10",
    "timestamp": "2026-10-02T00:00:01Z",
    "nativeSequence": 10,
    "data": { "phase": "report", "retryable": false }
  },
  {
    "type": "assistant.message",
    "id": "event-11",
    "data": { "content": "No catalog entries were found. The local check failed.\n" }
  },
  {
    "type": "session.result",
    "id": "event-12",
    "timestamp": "2026-10-02T00:00:02Z",
    "data": {
      "numTurns": 2,
      "durationMs": 2000,
      "totalCostUsd": 0,
      "usage": {
        "input_tokens": 30,
        "output_tokens": 8,
        "cache_creation_input_tokens": 0,
        "cache_read_input_tokens": 6
      },
      "errors": [],
      "permissionDenials": []
    }
  }
]
```

The tool failure remains on `native-call-c`; the source's empty session-error array is not populated automatically from it. The result contains no assertion of overall task success.

### 12.2 Appendix B: Partial and mixed traces

A valid partial canonical trace can contain a dangling start and an unrelated orphan completion:

```json
[
  {
    "type": "tool.execution_start",
    "data": { "toolCallId": "native-pending", "toolName": "bash", "command": "inspect" }
  },
  {
    "type": "tool.execution_complete",
    "timestamp": "2026-10-02T00:00:03Z",
    "data": {
      "toolCallId": "native-orphan",
      "toolName": "bash",
      "success": false,
      "output": "",
      "error": { "message": "Permission denied." }
    }
  }
]
```

The completion's different ID prevents it from completing `native-pending`. A summary describes the first call as pending/unknown and the second as an observed failed completion with an unavailable start. No result, duration, cost, or synthetic IDs are needed.

The following mixed **input** is not itself canonical:

```json
[
  { "type": "assistant.message", "id": "native-a", "data": { "content": "A " } },
  { "type": "assistant", "message": { "content": [{ "type": "text", "text": " B\n" }] } },
  { "type": "vendor.notice", "data": { "count": 0 } },
  { "type": "result", "num_turns": 1, "usage": { "input_tokens": 0, "output_tokens": 2 } }
]
```

Per-record conversion yields this canonical output:

```json
[
  { "type": "assistant.message", "id": "native-a", "data": { "content": "A " } },
  { "type": "assistant.message", "data": { "content": " B\n" } },
  { "type": "vendor.notice", "data": { "count": 0 } },
  { "type": "session.result", "data": { "numTurns": 1, "usage": { "input_tokens": 0, "output_tokens": 2 } } }
]
```

Neither message is dropped, whitespace is unchanged, the extension survives, and the legacy result is converted.

### 12.3 Appendix C: Accounting examples

| Source observations | Canonical accounting |
| --- | --- |
| Two distinct Codex turns: `{input_tokens: 10, output_tokens: 3, cached_input_tokens: 2}` and `{input_tokens: 20, output_tokens: 5, cached_input_tokens: 4}` | `numTurns: 2`, usage `{input_tokens: 30, output_tokens: 8, cache_read_input_tokens: 6}`. |
| Two distinct Pi finalized turns: `{input: 10, output: 3, cacheRead: 2, cacheWrite: 0}` and `{input: 20, output: 5, cacheRead: 4, cacheWrite: 1}` | `numTurns: 2`, usage `{input_tokens: 30, output_tokens: 8, cache_read_input_tokens: 6, cache_creation_input_tokens: 1}`. |
| A terminal session snapshot reporting the same totals after either sequence | Totals remain unchanged; the snapshot is not another contribution. |
| Gemini result with `{input_tokens: 30, output_tokens: 8, cached: 6, tool_calls: 4}` and no turn count | Usage is mapped; tool-call metadata is retained; `numTurns` remains absent. |
| Usage `{input_tokens: 0, inputTokens: 99, outputTokens: 2}` | Input reads as reported zero; output reads as 2; aliases are not summed. |

If a source's cached tokens are included in its input figure, `30 + 8` is the input/output total, not `30 + 8 + 6`. These examples do not imply that missing duration or cost is zero.

For a selected canonical result with `numTurns: 2` and usage `{input_tokens: 30, output_tokens: 8}`, bootstrap's legacy telemetry projection is:

```json
{"type":"result","num_turns":2,"usage":{"input_tokens":30,"output_tokens":8}}
```

This record is written only to the compatibility telemetry destination. It is not appended to the canonical event array.

### 12.4 Appendix D: Diagnostics and recovery

No new canonical error-code enumeration is introduced. Existing parser reporting remains available, while source error codes/messages remain source data.

| Condition | Recovery or diagnostic interpretation |
| --- | --- |
| Empty input | Existing no-content report; empty trace. |
| No supported records | Existing unrecognized-format report; empty trace. |
| Unknown raw engine record | Skip it; continue with supported adjacent records. |
| Unknown native extension | Retain it unchanged; a display may omit its contents. |
| Malformed/truncated JSON record | Skip unrecoverable framing; recover independently valid adjacent records. |
| Source provider/session error | Preserve its structure in session errors; do not relabel it as assistant success. |
| Tool execution failure | Preserve failure on the completion, independently of session-error reporting. |
| Missing start or completion | Preserve the orphan/dangling observation with unknown missing evidence. |
| Telemetry enrichment failure | Existing warning/best-effort reporting; no trace mutation or invented metrics. |

### 12.5 Appendix E: Security and privacy considerations

Source logs can contain secrets, prompts, reasoning, repository paths, untrusted tool output, hostile markup, and text resembling commands. Lossless normalization and safe publication are different boundaries: internal source evidence can remain intact while a redacted presentation omits sensitive values.

Default summaries historically omit user prompts. Retaining prompts for trace fidelity does not justify exposing them through a newly generic event renderer, a raw preview, an unknown-extension dump, or bootstrap telemetry. Error objects and native metadata can also carry sensitive content, so publication filtering applies beyond `user.message`.

A formatter treats embedded HTML and Markdown fences as hostile payload rather than trusted page structure. Dynamic payloads do not become shell commands, HTML instructions, or active links without the appropriate escaping and policy. Add-mask values and known secrets are redacted in publication copies before artifact upload.

Malformed logs and very large records can exhaust memory or produce misleading summaries. Explicit limits and truncation notices preserve the distinction between observed evidence and complete execution. A missing tool result is not success, and observed usage or turns is not an authorization decision or a task-completion guarantee.

Native IDs can collide, timestamps can be out of order, and a trace can contain ambiguous concurrent calls. Exact-ID pairing avoids attributing a failure or output to the wrong tool. Cross-session concatenation requires an external boundary policy; this specification does not invent a wrapper or fabricated session IDs to resolve such ambiguity.

## 13. Change Log (Informative)

### Version 2.5.0 - Draft (2026-10-22)

- Defined opt-in protected declarative session ingestion: final native stderr and collection completeness, engine-owned recorder authentication, and structure-preserving publication (T-UAS-084 through T-UAS-086).
- Distinguished protected verification failure from unprotected tolerant parsing and required separate Linux/AWF isolation and authenticated-protocol acceptance evidence.
- Retained serialization-format version 1 and unchanged default ingestion behavior.

### Version 2.4.0 - Draft (2026-10-22)

- Added `agentName` and `callerSource` to `firewall.token_usage` and `usage.report`, projected from AWF's `agent_name` and `caller_source`, and required consumers to prefer explicit proxy caller attribution over correlation (T-UAS-083).
- Documented per-engine `x-awf-caller` support: Pi sends `main` from the parent process and `subagent/<name>` from each sub-agent process; Copilot CLI, Copilot SDK, Claude Code, and Codex send none because their header mechanisms are provider- or process-wide.
- Retained serialization-format version 1.

### Version 2.3.0 - Draft (2026-10-22)

- Added `model.config` `routing.candidateModels`, the effective router pool sent to AWF after gh-aw alias expansion and model-policy narrowing; `routing.allowedModels` keeps the authored entries, including aliases (T-UAS-080).
- Retained serialization-format version 1.

### Version 2.2.0 — Draft (2026-10-21)

- Added `model.resolution` cause `unsupported_endpoint` for a live model whose `/reflect` `supported_endpoints` do not include the endpoint the engine calls (T-UAS-074).
- Retained serialization-format version 1.

### Version 2.1.0 - Draft (2026-10-20)

- Defined compact unified session profile version 1: bounded metadata-only JSONL, explicit loss statistics, and full-session resolution through usage artifact IDs (T-UAS-076 through T-UAS-079).
- Added a shared in-memory projection and bounded file-reader helper without changing or truncating the full session artifact.
- Retained full serialization-format version 1.

### Version 2.0.0 — Draft (2026-10-18)

- Incompatible: `model.inventory` endpoint `models` is now optional. AWF `null` model and routing lists are omitted as unknown instead of written as empty arrays, so readers can no longer assume every endpoint has a model list (T-UAS-075).
- Accepted endpoint-local `null` routing lists without rejecting the rest of the inventory, and retained endpoint `configured`, routing `supportedReasoningEfforts`, and `contextWindowTokens` when supplied.
- Retained serialization-format version 1.

### Version 1.12.0 — Draft (2026-10-18)

- Retained AWF ranked choices, eligible-choice and catalogue-overlap counts, selected, requested, and upstream endpoints, request-telemetry `unavailable` dimensions, HTTP status, and failure phase in `firewall.model_routing` when supplied (T-UAS-070).
- Retained serialization-format version 1.

### Version 1.11.0 — Draft (2026-10-16)

- Added the runner-written `model.resolution` observation from `agent/model-resolution.json`, recording each engine's configured and resolved model, provider, status, and failure cause (T-UAS-074).
- Retained serialization-format version 1.

### Version 1.10.0 — Draft (2026-10-16)

- Added `workflow.job_result` dependency outcomes and observed job durations to the unified artifact and publication views without job outputs or inferred final workflow status.
- Retained serialization-format version 1.

### Version 1.9.0 — Draft (2026-10-15)

- Keyed audit and logs sub-agent rows by agent definition with `display_names` (T-UAS-073).
- Restricted sub-agent served models to runner- and proxy-written evidence, without provider prefixes or aliases, and kept concrete model IDs pinned by declared sub-agents.
- Retained serialization-format version 1.

### Version 1.8.1 — Draft (2026-10-12)

- Added activation skill inventory and advisory exact-name/scope conflict evidence (T-UAS-072).
- Preserved observed engine skill invocation separately from installed availability, without skill contents.
- Retained serialization-format version 1.

### Version 1.8.0 — Draft (2026-10-11)

- Added the AWF `steering` notice to `firewall.token_usage` and `usage.report` (T-UAS-071).
- Mapped `ai_credit_steering` to `firewall.steering` and retained steering thresholds.
- Added the `model_endpoint.mismatch` harness observation from `agent/model-endpoint-mismatch.json` and the `model_endpoint_mismatch_error` category.
- Preserved sequential source order across missing timestamps and clock regressions while interleaving agent, AWF, MCPG, and GitHub API evidence.
- Imported local OTLP spans, span events, and log messages with resource/scope context and nanosecond ordering; kept publication summaries metadata-only.
- Updated the ordering validator and regression coverage without changing the numeric serialization-format version.
- Retained serialization-format version 1.

### Version 1.7.0 — Draft (2026-10-09)

- Added final agent workflow metadata and compact model-routing attribution to `workflow.info`.
- Added the `model_routing.outcome` harness observation and documented essential `firewall.model_routing` fields.
- Recorded absent workflow metadata in collection coverage and retained serialization-format version 1.

### Version 1.6.0 — Draft (2026-10-05)

- Added `assistant.refusal` and T-UAS-069 for structured OpenAI/Anthropic refusal and content-filter signals.
- Preserved exact available text, optional policy details, and partial-stream evidence in canonical and essential unified payloads.
- Added provider-documentation fixtures and a sanitized content-filter regression from the RAI validation run.
- Retained numeric serialization-format version 1.

### Version 1.5.0 — Draft (2026-10-05)

- Added Goose's stream-JSON mapping (T-UAS-068), custom delegate precedence, and current engine labels.
- Separated canonical and essential unified payload declarations, including their accounting-key conventions.
- Added mechanically generated Draft-07 schemas, deterministic drift checks, and JSON/JSONL validation tools.
- Documented published-session downloads and historical reconstruction; clarified historical gaps and schema-validation limits.
- Retained numeric serialization-format version 1.

### Version 1.4.0 — Draft (2026-10-03)

- Added T-UAS-067: one `agent.execution` entry retaining harness/compiler categories,
  native error codes/types, and the observed final exit code.
- Defined detector persistence, historical reconstruction, precedence, retry
  semantics, and JS/Go reader validation without changing file-format version 1.

### Version 1.3.0 — Draft (2026-10-02)

- Preserved detection job results, conclusions, categorical reasons, and verdict flags in unified session artifacts and publication views.
- Preferred sanitized conclusion results over raw verdicts, covering structured and inline detectors without duplicate observations.
- Defined absent verdict flags for skipped, cancelled, and failed detection, and excluded detector transcripts and free-form reasons.

### Version 1.1.0 — Draft (2026-10-02)

- Added the artifact-merger conformance class and requirements T-UAS-054 through T-UAS-066.
- Defined complete MCPG/AWF and downstream observation payloads, source provenance, timestamp units, deterministic ties, and untimed retention.
- Specified canonical bootstrap persistence, conclusion-job usage publication, coverage diagnostics, replica precedence, and artifact redaction.
- Required compact single-record-per-line JSONL while preserving escaped payload whitespace and native metadata.
- Named the unified artifact file `aw_session.jsonl` and required a leading numeric `session.format` version entry, independent of document and engine versions.
- Added unified-file views to Actions step summaries and output logs, with source-scoped agent accounting and privacy-preserving runtime projections.
- Recorded the conclusion pipeline review, empirical multi-engine merger execution, and remaining boundaries.

### Version 1.0.0 — Draft (2026-10-02)

- Defined the existing ordered Copilot-compatible event array without a new session wrapper or event version field.
- Specified core events, optional/native-field preservation, mixed-record normalization, streaming fidelity, partial execution, accounting, and error separation.
- Defined mappings for all six engine adapters and compatibility requirements for shared renderers and bootstrap telemetry.
- Added requirement IDs T-UAS-001 through T-UAS-053, compliance matrices, complete examples, security considerations, and an informative implementation gap matrix.

Future document revisions use semantic versioning: incompatible contract changes increment the major version, backward-compatible additions increment the minor version, and clarifications or editorial corrections increment the patch version. No earlier published version is asserted.
