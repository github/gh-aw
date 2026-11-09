# ADR-75984: Record durable session continuation as metadata evidence

**Date**: 2026-11-08
**Status**: Proposed
**Deciders**: Maintainers reviewing [PR #75984](https://github.com/github/gh-aw/pull/75984)

## Context

Durable engines can restore a checkpoint without successfully opening the
previous native session. A successful Actions run or artifact download therefore
does not prove continuation. Audit and health need to distinguish restoration
from observed native startup, including historical runs without startup evidence.
Exporting historical transcripts to obtain that distinction would unnecessarily
expand the privacy boundary.

Copilot, Claude, Codex, and Pi have different native session formats. Their
continuation evidence needs a common representation without replacing the native
checkpoint formats or changing OpenTelemetry trace parentage.

## Decision

Record restoration and native startup separately in a metadata-only
`agent/durable-session.jsonl` artifact. `durable.restore` records restored, fresh,
or failed restoration. `durable.session` records resumed or fresh startup only
after the selected native session opens or emits activity. Resumption requires
verification of the exact selected native session ID; restored files alone are
insufficient.

Retain allowlisted engine, artifact storage, run ID and attempt, timestamp,
status, native session ID, and available parent-run and checkpoint artifact
identifiers. Do not include historical conversation content, tool results,
credentials, or arbitrary payload fields. Collect and validate these records
against the current run and attempt, and preserve their metadata through unified
and compact sessions within existing transport limits.

Audit reports the available restoration and startup evidence. Health aggregates
cached evidence for the workflow's current revision, retaining unavailable
samples rather than presenting missing or budget-skipped evidence as fresh
sessions or confirmed resumptions. Historical runs without startup records remain
unavailable; this change does not rewrite earlier session artifacts.

Use the string attribute `gh-aw.durable.parent_run_id` on agent spans and session
logs, including Pi native sessions, for validated checkpoint lineage. This attribute is
correlation metadata, not an OpenTelemetry parent span or a replacement trace ID.

Pi follows the native CLI, SDK, and RPC session chaining adopted in
[PR #75995](https://github.com/github/gh-aw/pull/75995). Audit retains support for
historical task-journal identities, but new startup records use native session UUIDs.

## Alternatives considered

**Infer continuation from artifact restoration or Actions success.** Neither
establishes that the native engine opened the selected session. This would
overstate the evidence and hide startup failures.

**Infer continuation from historical transcripts.** Native formats vary, and
transcripts are not reliable proof of the current startup. Exporting them also
introduces unnecessary historical content into reporting and telemetry.

**Report each engine's native checkpoint format directly.** This couples audit
and health to engine-specific storage details and leaves compact sessions without
a shared evidence contract.

**Make the previous workflow run the OpenTelemetry trace parent.** Checkpoint
lineage is not necessarily execution parentage. An explicit string attribute
preserves correlation without changing trace semantics.

## Consequences

Restoration-only, resumed, fresh, failed, and unavailable outcomes are
distinguishable without exposing historical transcripts. Engines retain their
native checkpoint formats while sharing one reporting representation.

Older runs cannot establish startup retrospectively. Bounded health sampling and
missing artifacts reduce evidence coverage, which must remain visible. Metadata
validation, native startup observation, artifact collection, and schema changes
must stay synchronized across the JavaScript producers and Go consumers.

This metadata provides operational evidence, not cryptographic attestation of
agent behavior. Identifier allowlists and file-safety checks do not turn
sandbox-writable artifacts into an authenticated execution record.
