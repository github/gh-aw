---
title: Unified Agent Session Specification
description: Draft contract for canonical engine traces and essential unified session payloads across gh-aw runtime components.
sidebar:
  order: 1365
version: "1.4.0"
status: Draft
publication_date: "2026-10-03"
editors:
  - name: GitHub Agentic Workflows Team
    organization: GitHub
---

# Unified Agent Session Specification

**Version**: 1.4.0<br>
**Status**: Draft<br>
**Publication Date**: 2026-10-03<br>
**Editors**: GitHub Agentic Workflows Team (GitHub)<br>
**This Version**: [unified-agent-session-specification](/gh-aw/specs/unified-agent-session-specification/)<br>
**Latest Version**: This document

---

## Abstract

This specification defines the session traces used by GitHub Agentic Workflows: ordered arrays of Copilot-compatible event objects containing `type`, `data`, and optional native metadata. It establishes a loss-preserving parser contract for Claude, Copilot, Codex, Gemini, Pi, and custom engines, and a conclusion-job projection of essential agent, MCP gateway (MCPG), agent workflow firewall (AWF), safe-output, experiment, grader, and eval payloads. The serialized `usage/aw_session.jsonl` begins with a numeric file-format version header, followed by timestamp-ordered evidence with source provenance; untimed observations remain available without invented timestamps. Native evidence, canonical parser traces, and existing accounting artifacts remain available separately.

## Status of This Document

This is a **GitHub Agentic Workflows project specification**, written using W3C-inspired document conventions. It is **not an official W3C standard**, W3C publication, or W3C-endorsed recommendation.

Version 1.4.0 is a draft governed by the project's normal review process. It may be updated, replaced, or superseded. The accompanying implementation and regression suites exercise this contract, including the sampled CI sessions identified in Section 9.4. This is not a blanket declaration of conformance for every engine version or source format. Section 10 records pre-implementation gaps. Approval and ongoing compliance testing remain project responsibilities.

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

This specification covers parsing supported input records, canonical conversion, usage aggregation, tool correlation, compatibility projections, bootstrap telemetry, and conclusion-job artifact merging. It does not specify engine execution, workflow frontmatter, model behavior, transport protocols, or a cross-language storage API. JSON and JSON Lines are serialization forms, not different session models.

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
`usage/aw_session.jsonl`. Graders arrive in the agent artifact. Collection and upload
run after conclusion handlers, before setup cleanup. The existing usage JSON,
JSONL accounting, activity summary, and result files remain available.

### 1.3 Terminology

| Term | Meaning |
| --- | --- |
| Canonical event | An object with a dot-namespaced `type`, a `data` object, and any supplied native metadata. |
| Core event | One of the seven event types defined in Section 4. |
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
| F — Full pipeline | Satisfy P for all six engines, R for all shared summary renderers, B, and M. | All normative requirements |

A parser supporting fewer engines can conform for its declared engines. Omitting an OPTIONAL field because the source did not expose it is full conformance, not partial conformance. An implementation failing a mandatory requirement is nonconforming for the affected class; “partial implementation” is a progress description, not a weaker compliance level.

### 2.3 Source-dependent availability

“Required core event” means required vocabulary support and required emission when corresponding supported source evidence exists. It does not mean every trace contains every event or every field. Availability rules in Sections 3 and 4 are part of the contract, particularly for partial logs and older native streams.

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

### 3.3 Parser integration

**T-UAS-008 — Existing API boundary.** Parser integrations MUST keep the trace in the existing `logEntries` return member. Presentation text and flags such as `mcpFailures` and `maxTurnsHit` MUST remain separate from event data unless they reflect actual source observations. Canonical `logEntries` MUST NOT contain bare legacy `result` entries for telemetry convenience.

### 3.4 TypeScript message signatures (Informative)

[`types/agent_session.d.ts`](https://github.com/github/gh-aw/blob/main/actions/setup/js/types/agent_session.d.ts)
defines the TypeScript interfaces for every core message signature. `CoreSessionEvent`
is their discriminated union; `SessionEventDataMap` associates each core event type
with its payload interface. `AgentSession` also accepts native dot-namespaced extension
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
Numeric range checks remain runtime responsibilities.

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
| Agent tool lifecycle | Correlation IDs, tool/server names, one `input` or `output` field, command, outcome/error signals, duration, and exit code. |
| Agent accounting | Turns, duration, cost, observed terminal `status` and `sourceType`, normalized `usage` including reported reasoning tokens, errors, and permission denials. |
| Agent execution diagnostics | Unique harness/compiler `categories`, native `errorCodes` and `errorTypes`, and the observed final process `exitCode`; no diagnostic messages or duplicated engine envelopes. |
| MCP | Server, direction, RPC/call/request IDs, method, tool name, duration, sizes, status, reason, and error code/message; no RPC arguments, response bodies, or error context. |
| Firewall | Host, method, status, decision, byte count, duration; steering/tracker event, level, message, reason, and request ID. |
| Safe outputs | Operation type, repository/number, provider/identifier/URL, status, and errors; no requested title/body or arbitrary operation payload. |
| Experiments | Run ID, assignments, and counts. |
| Graders and evals | Grader IDs/names, values, units, statuses, decisions, thresholds, and errors; eval ID, answer, model, and error. No grader scripts or eval questions. |
| Runtime accounting | Provider, model, request ID, status, AIC, cumulative/checkpoint AIC, premium requests, duration, and normalized `usage`; overlapping reports stay separate. Detection and eval usage reports resolve per-observation AIC from known model pricing only when explicit AIC is unavailable; unknown pricing leaves AIC absent, while explicit zero and checkpoints remain authoritative. |
| Execution, detection, workflow | Observed outcomes, exit code, duration/start/end, detection job result/conclusion/categorical reason and verdict flags, engine ID, requested model, trigger type, workflow/repository/run ID, and available gh-aw, AWF, MCPG, and agent versions. No detector transcript or free-form reasons. |

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
`detection`, `workflow`, and `collector`. Phases distinguish agent and detection
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

**T-UAS-056 — Timestamp ordering.** After the file-format header, events with a
supported source timestamp MUST sort ascending by `provenance.timestampMs`. The merger MUST preserve native
timestamp values and MUST derive its ordering key using the source schema's
units, not an epoch-magnitude heuristic. ISO timestamps MUST carry a timezone.
Numeric Squid audit timestamps use Unix seconds; native agent timestamps and
token-tracker audit `ts` values use milliseconds. Pi's observed
`message.timestamp` is also a supported source timestamp. Timestamp precision is
limited to the JavaScript ordering key; original source precision remains
preserved.

**T-UAS-057 — Untimed and tied observations.** Missing or invalid timestamps
MUST NOT cause observations to be discarded. Except for the pinned file-format
header, untimed observations MUST follow all timed events. Equal-time events and
untimed events MUST preserve the deterministic source enumeration and source-local
array order. The merger MUST
NOT substitute file modification time, collection time, a neighboring event's
time, or an inferred tool duration for absent evidence. Wall-clock ordering
does not establish causality or correct cross-process clock skew.

**T-UAS-058 — Authoritative source selection.** A persisted canonical agent
stream MUST take precedence over its raw engine logs. Without it, the merger
MUST use available native Copilot sessions or the existing engine parser for
supported raw logs. It MUST NOT combine these equivalent agent representations
and count them as separate observations. Replicated firewall paths MUST select
`sandbox/firewall/logs` before `sandbox/firewall/audit`, then supported legacy
layouts; an existing empty authoritative file MUST suppress its older copy.
Distinct gateway streams and events MUST remain separate observations.

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

## 4. Core Event Vocabulary (Normative)

The following tables describe the common fields in `data`. Optional native and previously supported fields are not prohibited. A field marked **source-dependent** is preserved or mapped when supplied and meaningful; its absence is not an error. This avoids making unsupported metrics or IDs prerequisites for canonical conversion.

### 4.1 `session.init`

**T-UAS-009 — Initialization.** A mapped initialization event MUST use `session.init` and MUST set `sourceEngine` to the recognized source engine. It MUST preserve the initialization fields below when exposed. Existing native initialization events lacking `sourceEngine` MUST remain readable and MUST NOT be rejected solely for that absence.

| Field | Value and availability |
| --- | --- |
| `sourceEngine` | String: `claude`, `copilot`, `codex`, `gemini`, `pi`, `opencode`, or `custom` for the originating integration. A custom delegation can retain the detected parser's engine label as described in 7.6. |
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

### 4.3 `assistant.message` and `assistant.reasoning`

**T-UAS-011 — Assistant channels.** Supported assistant text MUST map to `assistant.message`, and supported reasoning or thinking text MUST map to `assistant.reasoning`, with exact text in `data.content`. Normalizers MUST NOT merge these channels or reinterpret session/provider errors as ordinary assistant answers.

| Event | Field | Value and availability |
| --- | --- | --- |
| `assistant.message` | `content` | Source text string; native structured content remains supported. |
| `assistant.reasoning` | `content` | Source reasoning/thinking string when available. |

Older readers' bare `reasoning` alias can remain a compatibility input. New canonical reasoning events use `assistant.reasoning`. Reasoning is not required from an engine that does not expose it.

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
| `mcp.rpc.request`, `mcp.rpc.response` | MCPG `REQUEST`/`RESPONSE` or `rpc_request`/`rpc_response`, with flat RPC metadata and error code/message. |
| `mcp.difc.filtered`, `mcp.guard.blocked` | DIFC and guard-policy diagnostics; no inferred successful tool outcome. |
| `mcp.tool_call`, `mcp.event` | Structured gateway calls or other gateway log messages. |
| `firewall.http_access` | AWF network audit observations, including operational entries without a host. |
| `firewall.token_usage`, `firewall.steering`, `firewall.event` | API proxy accounting, token/time steering, tracker and other firewall messages. |
| `safe_output.request`, `safe_output.result`, `safe_output.error` | Requested operation identities, executed item identities, and available errors. Requests are not execution results. |
| `experiment.state`, `experiment.assignment` | Downloaded state and assignment observations, including historical state retained in the supplied snapshot. |
| `grader.manifest`, `grader.result` | Essential deterministic grader definitions/results, without scripts; grading does not invent event time. |
| `eval.result` | Evals JSONL observations, preserving answers, IDs, and observed timestamps. |
| `usage.report`, `execution.result`, `detection.result`, `workflow.info` | Existing accounting, execution evidence, detection verdicts, and run metadata. `workflow.info` retains available `cliVersion` (gh-aw), `awfVersion`, `mcpgVersion`, `engineId`, `agentVersion`, `requestedModel`, and `triggerType` from `aw_info.json` (`cli_version`, `awf_version`, `awmg_version`, `engine_id`, `agent_version`, `model`, and `event_name`, respectively). Unavailable values are not inferred. |
| `session.collection_warning`, `session.collection` | Explicit collection diagnostics and coverage. |
| `session.format` | Leading collector-owned file-format metadata, distinct from source-native events with the same type. |

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
Tool failures and errors quoted in user/assistant messages or tool outputs MUST
NOT become agent execution errors. Original native and canonical error events
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
| Compiler error-detection outputs | `inference_access_error`, `mcp_policy_error`, `agentic_engine_timeout`, `model_not_supported_error`, `http_400_response_error`, `capi_quota_exceeded_error`, `invocation_cap_exceeded`, `max_cache_misses_exceeded`, `missing_model_pricing_error`, `shell_expansion_guard_rejected` |
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
| `type: "assistant", message.content[]` with `text` blocks | `assistant.message`, `content` from each `text`. |
| Same envelope with `thinking` blocks | `assistant.reasoning`, `content` from `thinking`. |
| Same envelope with `tool_use` blocks | `tool.execution_start`; `id` → `toolCallId`, `name` → `toolName`, `input` retained. |
| `type: "user", message.content[]` with `text` blocks | `user.message`, not discarded because the user envelope also carries tool results. |
| Same user envelope with `tool_result` blocks | `tool.execution_complete`; `tool_use_id`, `content`, `is_error`, `duration_ms` mapped. |
| `type: "result"` with recognized accounting/diagnostic fields | `session.result`; preserve source result metadata. |
| Recognized non-init system status with textual message/content | Retain status metadata; use the established assistant-text compatibility view only for actual status text, not user prompts or provider errors. |

### 7.2 Copilot

**T-UAS-037 — Copilot adapter.** The Copilot adapter MUST support native `events.jsonl` records, recognized legacy JSON entries, structured debug response blocks, and recognized pretty-print tool/output records. It MUST preserve native extension events, IDs, `command`, `mcpServerName`, `result`, and metadata. A debug response containing a requested tool call but no observed execution result MUST produce a start without fabricated success.

| Source signature | Canonical mapping or interpretation |
| --- | --- |
| Dot-namespaced `type` with object `data` and optional native envelope | Core events retained; other native types retained as extensions. |
| Recognized Claude-compatible legacy entries | Per-record legacy mappings, with `sourceEngine: "copilot"` on mapped initialization. |
| Framed `[DEBUG] data:` JSON response with `choices[].message` | `content` → assistant message, `reasoning_text` → reasoning, `tool_calls[].id/function` → starts; preserve argument text when it is not valid structured JSON. |
| Distinct debug response `usage.prompt_tokens` / `completion_tokens` | Map to input/output tokens and aggregate distinct response contributions. |
| Pretty-print tool markers `✗`, `●`, or `✓` in recognized CLI layout | Map observed tool records and their actual continuation output; outcome interpretation follows the recognized format's semantics. |
| Recognized usage/model footer and explicit `Turns:` | Preserve reported usage/model and explicit turn count; no tool-count fallback for turns. |

CLI/debug framing is removed, but retained source payload text is not trimmed. A native event stream does not need a synthesized result merely because its renderer can estimate conversation turns.

### 7.3 Codex

**T-UAS-038 — Codex adapter.** The Codex adapter MUST support recognized `thread.*`, `turn.*`, and `item.*` JSONL records and the existing recognized legacy text layouts. It MUST preserve native item/tool IDs, independent starts/completions, text/reasoning, explicit tool failures, and session errors. Distinct `turn.completed.usage` reports MUST accumulate as specified in Section 6.

| Source signature | Canonical mapping |
| --- | --- |
| `thread.started` with `thread_id` | Initialization/session identity; reported model/workdir metadata retained when available. |
| Supported `item.started` / `item.updated` / `item.completed` with `item.type: "agent_message"` or `"reasoning"` | Message/reasoning observations; distinguish partial text from full snapshots to avoid duplication. |
| Same item envelope with `item.type: "mcp_tool_call"` | Start/completion according to observed lifecycle; preserve item/call ID, `server`, `tool`, `arguments`, `result`, `error`, `status`. |
| Same item envelope with `item.type: "command_execution"` | Preserve ID, command, `aggregated_output`, status, and `exit_code`; nonzero exit is failure. |
| `turn.completed` with `usage` | Accumulate `input_tokens`, `output_tokens`, `cached_input_tokens` → `cache_read_input_tokens`, and `cache_write_input_tokens` → `cache_creation_input_tokens`; count distinct completed turns. |
| Supported `turn.failed`, top-level `error`, or completed error item | Session/provider errors in `session.result.errors`, retaining supplied error structure and metadata. |
| Legacy `thinking`, recognized tool/exec call and outcome lines | Exact payload text and independently observed tool lifecycle; no source ID if none is exposed. |
| Legacy `ERROR:` / recognized reconnect diagnostics | Session errors, separately from successful tool activity. |

An item carrying a complete invocation and result can expand to a start and completion at that item position. An unfinished item or a legacy call lacking an outcome does not supply a successful completion. Recognized model headers and harness spawn metadata can supply a model, but arbitrary text does not establish a Codex session.

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
| `type: "error"` with a message/error payload | Retain `gemini.error`; severity `error` or an unspecified severity also exposes a separate `session.result.errors` observation. A warning remains a warning, not an inferred session failure. |

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
`messageId`, or legacy `message.id` can identify a full-message snapshot:
`gemini.message_snapshot` retains its native payload. An unchanged snapshot adds
no core content; an append-only snapshot adds only its unreported suffix. A
rewritten or shortened snapshot retires the matching message/channel/block's
earlier core fragments to opaque `gemini.message_observation` events, retaining
exact original source envelopes in `data.observations`, and emits the full
authoritative core content at the snapshot position. Tool observations and
unrelated identities remain in place, without duplicate or stale answers.
For a full legacy content-array snapshot, a rewrite, removal, reordering,
insertion before existing content, or extension before an unchanged sibling
replaces all visible text/reasoning fragments of that message; supplied blocks
are emitted in their authoritative array order.
An empty final array retains the snapshot but emits no fabricated text.
Gemini's flat stream has no native message identity; a record's event `id` or
repeated anonymous text does not establish snapshot coverage.

### 7.5 Pi

**T-UAS-040 — Pi adapter.** The Pi adapter MUST support both recognized flat JSONL and v3 streaming records. It MUST preserve observed streaming content, execution events, IDs, source metadata, and provider errors even without a finalized message. It MUST accumulate distinct finalized-turn usage and MUST emit canonical `session.result`, not append bare legacy `result` for telemetry.

| Source signature | Canonical mapping |
| --- | --- |
| Flat `init`, `assistant`, `tool_use`, `tool_result` | Initialization, assistant content/deltas, starts, and completions, preserving native fields. |
| Flat `result.stats` | Map supplied input/output usage, `turns`, and `duration_ms`; retain other supported statistics. |
| v3 `session` with numeric source `version` and native `id` | Initialization with `sessionId` from the source ID, retaining timestamp/cwd/version as source metadata. |
| Recognized `message_start`, `message_update`, `message_end` / `turn_end.message.content` | Text, thinking, and `toolCall` observations; finalized content is a snapshot, not another copy of streaming text. |
| `tool_execution_start` / `tool_execution_end` | Independent start/completion using `toolCallId`, tool name/arguments, `result`, and `isError`; keep orphan ends and dangling starts. |
| Distinct `turn_end.message.usage` | Sum supplied `input`/`output`; map supplied `cacheRead`/`cacheWrite` to cache-read/cache-creation fields. Preserve other usage/cost data when exposed. |
| `turn_end.message.errorMessage` | Session/provider error, even when content is empty. |
| Recognized `agent_end` or other terminal accounting | Reconcile any supplied session snapshot; do not add it again to per-turn totals. |

The adapter can discover a reported model from a finalized turn without inventing an earlier model observation. Presentation aliases such as `bash` → `Bash` do not change canonical tool names. Sources lacking duration or cost do not supply zero duration or zero cost.

### 7.6 Custom engines

**T-UAS-041 — Custom format detection.** The custom adapter MUST select a delegate from actual supported input signatures. Parseable JSON alone, any dot-containing type alone, a nonempty markdown result, or a delegate's fabricated initialization MUST NOT establish detection. Existing Claude-compatible and Codex fallback support MUST remain available, and native canonical events MUST remain supported.

Recognized candidates include the native event envelope, Claude legacy message/init/result shapes, Codex known JSONL lifecycle/item shapes, and Codex legacy tool/exec/thinking layouts with their actual framing. Mere namespace matches with no supported payload are insufficient. Signature detection uses valid supported records even when unknown or malformed records are adjacent.

Delegation is deterministic. Native event recognition and per-record mixed conversion take precedence over broad fallbacks; remaining delegate ties use the existing Claude-before-Codex preference. A custom adapter MAY support additional documented engine signatures, but those signatures follow the same preservation contract. A delegated initialization MAY retain the detected engine's `sourceEngine`; custom-origin metadata, when supplied, is preserved separately. A custom engine emitting its own mapped initialization uses `sourceEngine: "custom"`.

When no supported signature is present, the existing custom “unrecognized format” result and empty trace are returned. A raw preview is not a normalized event and is subject to the privacy boundary in Section 8.

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

| Source event | Display |
| --- | --- |
| `session.init` | Merge supplied initialization observations for the display header: engine, observed model, session ID, cwd, tools, MCP status, slash commands, and model information. Missing inventories remain unavailable, not empty. |
| `user.message` | Retained in the canonical trace; omitted from default publication. |
| `assistant.message` | Assistant answer, including supported structured native content. |
| `assistant.reasoning` | Distinct reasoning channel, not converted into an answer or tool output. |
| `tool.execution_start` | All standard tools, including built-ins and bookkeeping tools; JSON arguments can be objects, arrays, scalars, null, or empty strings. A missing result is visibly pending. |
| `tool.execution_complete` | Source-proven pairing, separate output and error previews, known success/failure versus unknown outcome, duration, and an explicit missing-start label for orphan results. Empty observed output has an `[empty output]` marker. |
| `session.result` | Selected accounting, zero-valued metrics, cache counts, structured provider errors, and permission-denial records. Tool statistics separate failed, pending, and unknown outcomes. |

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
Formatted tool output uses a fence longer than its payload's backtick runs, and
untrusted inline HTML is escaped rather than treated as summary structure.

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
sources. Gateway, firewall, accounting, grader, and eval observations MUST NOT
be added to agent snapshots as additional session usage. Private correlation
keys MAY remain internal until projection, but MUST NOT bypass final publication
redaction.

The conclusion collector publishes the already-redacted `aw_session.jsonl` to
both sinks after writing it. `generatePlainTextSummary`,
`generateCopilotCliStyleSummary`, and `generateConversationMarkdown` recognize
unified events. Their unified view shows file version, per-component record
counts, per-source agent statistics, and a chronological trace. Existing
agent-only inputs retain their earlier rendering behavior.

| Observation | Unified display projection |
| --- | --- |
| Agent messages and tool lifecycle | Assistant/reasoning text, tool name, correlation ID, observed outcome, and scoped accounting. User prompt records remain omitted. |
| MCPG | Method, server, RPC ID, tool name, observed responses/errors, and filter/block diagnostics. Raw RPC arguments and response bodies are omitted. |
| AWF | Network host/method/status/decision, observed token usage, steering messages, and tracker event names. |
| Safe outputs | Requested versus execution-recorded operations, target metadata, and structured error counts. Requests are not displayed as successes. |
| Experiments, graders, evals | Assignments/state, grader value/unit/outcome, and eval ID/answer/model. Grader scripts and raw evaluation questions are omitted. |
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

**T-UAS-051 — Requirement coverage.** A conformance test suite MUST exercise every applicable requirement ID, every declared engine signature, and the edge cases in the matrices below. Full-pipeline conformance MUST cover all six engines, shared helpers, all shared renderers, and bootstrap telemetry. A report MUST identify failures and unavailable coverage; current implementation tests alone are not a conformance declaration.

**T-UAS-052 — Preservation assertions.** Tests MUST compare ordered canonical arrays and nested values, not only markdown substrings. They MUST check normalization twice for idempotence, repeated equal inputs for determinism, JSON round trips for supported fields, and input deep equality before/after conversion and reading. Frozen input fixtures SHOULD be used to expose accidental mutation.

**T-UAS-053 — Isolated consumer tests.** Renderer and bootstrap tests MUST exercise canonical-only traces, mixed input normalization, missing results, multiple snapshots, reported zero values, and failures. Bootstrap tests MUST mock or isolate filesystem and summary operations, assert the legacy projection and unchanged canonical input, and avoid depending on real engine execution or production telemetry files.

Recommended execution is fixture parsing, canonical structural assertions, accounting assertions, renderer checks, and bootstrap checks. Record the class, engine formats, requirement IDs, and outcomes in the test report.

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
| T-UAS-054–T-UAS-064 | Six engine adapters; interleaved MCPG/AWF sources; downstream snapshots/results; leading numeric format version; timestamp units and ties; malformed/missing logs; read/write failure; escaped secrets and symlinks | Complete compact `aw_session.jsonl`, pinned format header, provenance and payload preservation, deterministic chronology and untimed tail, explicit coverage, safe atomic persistence, existing accounting unchanged. |
| T-UAS-065–T-UAS-066 | Unified file through both publication sinks and the conversation renderer; colliding source IDs; overlapping accounting; hostile/secret text; exhausted summary budget | Known runtime types remain visible, scopes and accounting remain independent, private prompts/payloads stay omitted, output is bounded and safely redacted, source artifact remains intact. |
| T-UAS-067 | Repeated native/canonical/detector error observations, live timeout evidence, final-zero and missing exits, malformed or duplicate aggregate records, quoted errors and tool failures | One deterministic `agent.execution` record with distinct native codes/types, stable categories, observed exit precedence, unchanged error evidence, and matching JS/Go reader validation. |

### 9.3 Engine and integration fixture matrix

| Target | Required fixtures and edge cases | Principal assertions |
| --- | --- | --- |
| Claude | Legacy JSON array and JSONL; native events; mixed forms; user+result blocks; thinking; metadata; result followed by debug/extension; error/denial arrays | Complete field mapping, prompt retention without summary disclosure, result selection, native ID/metadata preservation. |
| Copilot | Native `events.jsonl`; legacy input; structured debug responses; known pretty-print layouts; command-only input; MCP server; JSON result blocks; incomplete debug tool call | Native extensions survive; no synthetic debug success/random IDs; display names do not rewrite storage; explicit turns only. |
| Codex | Known JSONL lifecycle/item shapes and legacy text; partial item start; preserved item IDs; failed command/MCP result; top-level/turn/item errors; two completed turns plus snapshot | Starts/ends retained in observation order; session errors separate; cumulative usage/cache counts; no last-turn-only accounting. |
| Gemini | Flat JSONL init, user/assistant messages, whitespace-only deltas, tool use/result, final stats, unknown/debug/partial lines; false/zero/object outputs | Exact streaming text; typed outputs; canonical result present; `tool_calls` retained but never used as turns. |
| Pi | Flat and v3 streams; session metadata; tool end before finalized turn; streaming-only partial turn; orphan end; reasoning; empty-content provider error; two finalized usage reports; terminal snapshot | No reorder for pairing, no lost partial tools/text, cumulative input/output/cache fields once, absent duration/cost, canonical result only. |
| Custom | Actual native/Claude/Codex signatures; deterministic delegate ties; mixed legacy/events; unrelated JSON, unknown-only dotted record, plain prose, delegate fallback markdown | Correct supported detection and delegation; no false format recognition from parseability or nonempty markdown. |
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
| Codex | [Smoke Codex](https://github.com/github/gh-aw/actions/runs/36909965579) and [Daily Documentation Updater](https://github.com/github/gh-aw/actions/runs/36850958249) | `agent-stdio.log` | `test_data/codex_ci_smoke.jsonl`, `test_data/codex_ci_mcp.jsonl`, `codex_session.test.cjs` |
| Copilot | [Smoke Copilot success](https://github.com/github/gh-aw/actions/runs/36798242962) and [failure](https://github.com/github/gh-aw/actions/runs/36946387975) | `events.jsonl` in the failed run's `copilot-session-state/`; process and stdio logs in the successful run | `copilot_session.test.cjs`, `parse_copilot_log.test.cjs` |
| Pi | [Chronicle success](https://github.com/github/gh-aw/actions/runs/36884805242) and [Tree Map failure](https://github.com/github/gh-aw/actions/runs/36447274044) | `pi-streaming.jsonl` | `fixtures/pi_ci_stream.cjs`, `pi_session.test.cjs` |
| Gemini | [Smoke Gemini success](https://github.com/github/gh-aw/actions/runs/36078916290), [spending-cap failure September 29](https://github.com/github/gh-aw/actions/runs/36504829912), and [September 27](https://github.com/github/gh-aw/actions/runs/36283760088) | `agent-stdio.log` | `fixtures/gemini_ci_sessions.cjs`, `gemini_session.test.cjs`, `fixtures/gemini_ci_lifecycle.cjs`, `gemini_ci_lifecycle.test.cjs` |

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

## 10. Implementation Gap Matrix (Informative)

These observations record the code **before** the implementation accompanying this
draft. They are a historical gap inventory, not claims about the updated code or an
exhaustive defect inventory. The normative sections and conformance suites define
the behavior implemented and verified by this change.

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
reconstructed, clocks are not corrected, binary threat-detection runs do not
become fabricated agent conversations, and Go CLI readers are not automatically
changed to consume the richer artifact. Actions step-summary and action-output
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
- **[Formatters]** [`actions/setup/js/log_parser_format.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/log_parser_format.cjs).
- **[Bootstrap]** [`actions/setup/js/log_parser_bootstrap.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/log_parser_bootstrap.cjs).
- **[Merger]** [`actions/setup/js/unified_session.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/unified_session.cjs), [`session_artifact.cjs`](https://github.com/github/gh-aw/blob/main/actions/setup/js/session_artifact.cjs), and [`collect_usage_artifact_files.sh`](https://github.com/github/gh-aw/blob/main/actions/setup/sh/collect_usage_artifact_files.sh).
- **[Driver]** [Copilot SDK Driver Specification](/gh-aw/specs/copilot-sdk-driver-specification/), related runtime logging context.
- **[SemVer]** [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html), document-version change classification.
- **[Tests]** Colocated `log_parser_shared.test.cjs`, `log_parser_bootstrap.test.cjs`, and the six `parse_*_log.test.cjs` files provide existing fixture/test integration points.

## 12. Appendices (Informative)

### 12.1 Appendix A: Complete canonical example

This example includes every core type, native metadata, an extension, structured/false/zero outputs, explicit failure, and reported zero cost. All identifiers and metrics represent supplied example source evidence. The user prompt is retained internally but omitted from default summaries.

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
