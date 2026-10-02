---
title: Git-backed Ledger Specification
description: Specification for standalone Git-backed ledgers, built-in projections, trusted persistence, and lossless compaction
sidebar:
  order: 1370
---

# Git-backed Ledger Specification

**Version**: 1.1<br>
**Status**: Working Draft<br>
**Editor**: GitHub Agentic Workflows Team

## Abstract

This specification defines bounded, deterministic, append-only JSONL ledgers with
disposable query projections. It specifies standalone `tools.ledger`
configuration, record identity, per-job responsibilities, built-in state models,
concurrent reconstruction, compaction, retirement, configuration limits, the
validated transactions consumed by trusted persistence, and the adversarial
review of the feature.

## 1. Conformance and terminology

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, and **MAY** in this document are to be interpreted as described in
RFC 2119 and RFC 8174.

* **Agent** is the untrusted workflow process that reads a disposable projection
  and requests mutations through configured ledger safe-output tools.
* **Record** is one immutable version-1 JSON object.
* **Shard** is a JSONL file containing immutable records.
* **Projection** is disposable SQLite state reconstructed from shards.
* **Coverage declaration** says that a replacement shard contains all records
  from one or more source shards.
* **Stable shard** is a closed shard not created by the current persistence run.
* **Validated transactions** are the versioned artifact of accepted ledger
  mutations produced by safe-output validation.
* **Agent job** is the workflow job that prepares read-only projections and runs the agent.
* **Persistence job** is the trusted `push_ledger_changes` job that revalidates
  transactions, reconciles the latest ledger branch, and pushes durable records.
* **Safe-outputs job** is the trusted job that ingests and handles safe outputs.

## 2. Data model and integrity

An implementation MUST serialize each record as canonical JSON followed by one
newline. A record MUST contain exactly `version`, `id`, `type`, `timestamp`,
`parents`, `payload`, and `sha`. `sha` MUST be the SHA-256 digest of the
canonical record with `sha` omitted.

Record IDs and shard IDs MUST use UUID version 4 syntax. Timestamps MUST be
canonical UTC ISO 8601 strings. Parent hashes MUST be unique, sorted, and
bounded. Readers MUST ignore malformed lines and report diagnostics without
discarding valid records.

A ledger MUST enforce configured record, shard, patch, and shard-count limits
before acknowledging a mutation. File writes MUST use regular, non-symlink
files, atomic publication where a new shard is created, and `fsync` for the
file and containing directory.

## 3. Job responsibilities

Ledger work is split across jobs with different privileges. An
implementation MUST NOT move a responsibility to a less trusted job.

| Job | Trust | Ledger responsibilities | Credentials |
| --- | --- | --- | --- |
| Agent job preparation | Trusted | Fetches canonical ledger history, validates records, and creates disposable read-only SQLite projections | Repository read access |
| Agent execution | Untrusted | Queries projections and queues mutations through the configured ledger safe-output tools; MUST NOT write canonical files | No ledger-branch write token |
| Safe-outputs job | Trusted | Validates requested mutations and emits the versioned transaction artifact; does not persist ledger records | Safe-output tokens |
| Persistence job | Trusted | Revalidates transactions, reconciles the latest ledger branch, commits and pushes accepted records. It MUST NOT compact. | Ledger-branch `contents: write` token |
| Maintenance plan job | Untrusted | Per ledger: reads the ledger branch, uses built-in segment selection, and emits a compaction plan artifact | `contents: read` only |
| Maintenance apply job | Trusted | Per ledger: validates the hostile plan against the latest ledger state and atomically applies it | `contents: write`; never runs user JavaScript |

The agent job MUST NOT perform compaction, retirement, or persistence. The
safe-outputs job MUST NOT mutate ledger storage. An immediate safe-output response
MUST NOT imply durability: an accepted request becomes durable only after the
persistence job succeeds. The persistence job MUST treat transaction artifacts
and restored ledger files as untrusted input and revalidate both.

## 4. Built-in behavior and script restrictions

Custom ledger replay and compaction scripts are not supported. Domain-specific
constraints MUST be expressed as value schemas on supported built-in types.

### 4.1 Memory validation script

`repo-memory.validation.script` runs after normalization and before commit. It
MUST run in a separate Node process with a bounded timeout and a sanitized
environment, and a non-zero exit MUST fail persistence (fail-closed), because it
is a policy gate on what is about to be pushed.

This separate file-storage validator is not a ledger replay or compaction
extension point. Removing custom ledger scripts MUST NOT disable it.

### 4.2 Built-in compaction selection

Compaction is owned by Agentic Maintenance (section 5.3), never by the agent or
persistence job. The maintenance plan job MUST use the built-in deterministic
segment-selection policy and MUST reject `tools.ledger.<name>.compaction.script`.
Its output is a proposal: the trusted apply job independently revalidates the
resulting plan. Eligible segments MUST be considered in segment-ID order, with
at most `max-segments` selected and their union bounded by `max-segment-kb`.
Segments that do not fit MUST be skipped. Fewer than two selected segments
MUST result in no compaction.

### 4.3 Built-in replay

The only supported declared types are `log`, `set`, `map`, `table`,
`counter`, and `claims`. Unknown types and any `replay` configuration MUST be rejected.

| Type | Supported operations | Derived `state` columns |
| --- | --- | --- |
| `log` | `append(value)` | `position`, `value` |
| `set` | `add(value)`, `remove(value)` | `identity`, `value` |
| `map` | `put(key, value)`, `delete(key)` | `key`, `value` |
| `table` | `insert(value)`, `update(key, patch)`, `upsert(value)`, `delete(key)` | `key`, `value` |
| `counter` | `increment(name, amount)`, `decrement(name, amount)` | `name`, `value` |
| `claims` | `claim(subject, claim, reason, citations)`, `vote(claim_id, vote, reason?)` | `claims`, `claim_citations`, `claim_votes`, `claim_state` |

Replay MUST apply canonical topological record order, breaking ties between
simultaneously ready records by SHA-256 lexical order. The `state` table MUST
be disposable and read-only to the agent; canonical JSONL records remain
authoritative. Invalid built-in operations MUST fail projection preparation,
not silently fall back to generic records.

Value schemas MUST validate values rather than operation envelopes. A `table`
MUST declare a string primary-key field through `key`; other types MUST NOT
declare `key`. Table inserts MUST reject duplicate keys, updates MUST require
an existing key and preserve the primary key, and upserts MUST replace the
entire row. Set membership MUST use canonical JSON equality. Counter amounts
MUST be nonnegative safe integers, and resulting arithmetic MUST remain within
the safe-integer range. Counters and claims MUST NOT accept value schemas.

Claims MUST include at least one valid repository citation. Claims and votes
MUST remain immutable records; the derived `claim_state` view reports vote
counts and timestamps, not truth or authority. Consumers MUST verify cited
evidence against current repository state before relying on a claim.

The `map` type exposes `ledger_map_put` and `ledger_map_delete`; `claims`
exposes `ledger_claim_add` and `ledger_claim_vote`. Other types use typed
`ledger_append` operations. Ledgers without a declared type
retain the generic records projection and raw-record append interface. They
MUST NOT execute custom replay code.

## 5. Lifecycle

### 5.1 Restore and reconstruct

Trusted preparation fetches each `ledgers/<name>` branch and reconstructs the
record DAG from canonical shards. A missing branch represents an empty ledger.
Malformed canonical history MUST fail standalone projection preparation.
The read-only database is created at `/tmp/gh-aw/ledgers/<name>/ledger.db`.
It is not durable ledger state and MUST NOT be required for recovery.

### 5.2 Deferred mutations

The agent MAY query the SQLite projection and queue mutations through the
configured ledger safe-output tools. It MUST NOT edit SQLite or ledger files
to persist changes. Mutations queued during a run are not immediately visible
in that run's projection.

Safe-output validation MUST check the configured ledger, operation fields,
schemas, and record and batch limits. It MUST normalize temporary IDs and
same-batch references into deterministic record IDs and emit a versioned
transaction artifact. The persistence job MUST independently revalidate the
artifact against trusted configuration before writing.

The persistence result MUST report ledger type, operation, deterministic
record ID, transaction ID, and validation status without echoing values.
An accepted request MUST NOT be reported as durable before its branch push
succeeds.

### 5.3 Compaction

Compaction runs in the generated Agentic Maintenance workflow, as one
shared plan/apply job pair with separate steps and plan files for each ledger.
The jobs MUST NOT share a workflow concurrency group. Compaction runs
on the ledger's `compaction.schedule` (`daily` by default, `weekly`, or `manual`)
and on demand through the `compact_ledger` maintenance operation, which the
`ledger_request_compaction` safe output dispatches. Both triggers use the same
jobs.

The plan job MUST produce a plan of version `gh-aw/ledger-compaction-plan/v1`
containing exactly `version`, `ledger`, `branch`, `trigger`, `created_at`,
`base_commit`, `sources`, `replacement`, and `plan_id`. Each source and the
replacement MUST be identified by segment ID, the SHA-256 of the segment bytes,
the byte size, and the sorted record SHAs. `plan_id` MUST be the SHA-256 of the
canonical ledger, branch, version, source identities, and replacement identity,
so the same transition always has the same ID. Plans MUST NOT contain commands,
paths, or Git arguments.

The apply job MUST treat the plan as hostile input: it MUST enforce size and
count limits, reject unknown keys, recompute `plan_id`, and require that the
plan targets the configured ledger and branch. It MUST then reload the latest
ledger branch, verify that every source segment still has the expected bytes and
records, rebuild the replacement from those verified records, and prove that no
record is lost. Segments appended after planning MUST be preserved. The
transition (add the replacement and state file, delete the sources) MUST be
published as a single commit guarded by the expected branch head; a moved head
MUST cause a refetch and revalidation, never an overwrite.

A plan whose sources are all gone and whose replacement or recorded plan ID is
present is already applied and MUST succeed as a no-op. A plan with only some
sources present is stale, and a plan whose segments changed is a conflict; both
MUST leave the ledger unchanged so the next maintenance run can plan again.
Compaction MUST fail open: a failed plan or apply MUST leave the ledger readable
and MUST NOT block appends or persistence.

### 5.4 Retirement

Maintenance apply owns retirement. Before deleting a source shard, it MUST
validate the plan against the latest branch state and verify that every source
record SHA is present in the rebuilt replacement. Invalid plans MUST fail
without changing the branch. Source deletion, replacement creation, and the
compaction state update MUST be published together under the expected-head guard.

### 5.5 Save

The persistence job saves accepted records to the corresponding
`ledgers/<name>` branch. It MUST reconcile concurrent or retried requests
against the latest canonical history, preserve existing records, and
revalidate built-in operations against reconstructed state before pushing.
Agent-supplied ledger files MUST NOT be used as authoritative storage.

## 6. Concurrency and guarantees

Concurrent writers MAY produce multiple DAG heads. Reconstruction MUST retain
all valid heads, and the next append MUST reference every current head up to
the configured parent bound. Identical deterministic replacement shards MUST
converge by identity; different valid replacements MAY coexist. No compaction
operation MAY remove a source segment unless the replacement record-union check succeeds.

The ledger provides integrity, boundedness, deterministic reconstruction, and
best-effort convergence. It does not provide distributed locking, serializable
transactions across branches, or confidentiality of repository-memory contents.

## 7. Configuration

Implementations MUST bound canonical file counts and expose configuration for
segment size, record size, and patch size. Size fields are expressed in KiB in
workflow configuration and MUST be converted to bytes exactly once. Invalid or
overflowing values MUST be rejected or ignored without silently increasing a
bound. Standalone projection preparation MUST inspect at most 1024 canonical
files and 100 MiB of history. These limits are independent of repo-memory
file-storage limits.

Compaction MUST default to `schedule: daily`, `min-segments: 32`, and
`max-segments: 128`; `compaction: false` disables it. Schedules MUST be
`daily`, `weekly`, or `manual`, and segment counts MUST be between 2 and 256
with the maximum no smaller than the minimum. When only the minimum is raised
above 128, the default maximum MUST rise to match.

The compiler MUST reject `replay`, including `replay.script` and
`replay.config`, and `compaction.script` with an explicit diagnostic.
The legacy `tools.repo-memory.ledger` declaration is not supported; standalone
ledgers MUST use `tools.ledger`.

Removing custom replay MUST NOT rewrite existing history. Existing raw-record
ledgers MAY omit `type` and query the generic `records.payload` column with
SQLite JSON functions. Declaring a built-in type on raw-record history is not
an automatic migration: each payload MUST satisfy the selected type's
operation format.

## 8. Security considerations

The ledger directory is untrusted input during persistence. Path traversal,
symlink replacement, forged coverage, malformed JSON, hash mismatch, duplicate
IDs, and oversized files MUST be handled as invalid input. A queued safe output
is a request, not authorization or proof of a durable mutation. Persistence
MUST revalidate the versioned transaction artifact and the canonical history.

## 9. Adversarial review

This section records the agentic threats considered for this feature and the
required countermeasures. The adversary is a prompt-injected agent that can call
ledger safe-output tools, write arbitrary files into agent-accessible directories,
and append arbitrary lines to the safe-output file, but that holds no
ledger-branch write credential.

| Threat | Required countermeasure |
| --- | --- |
| Forged replacement shard used to delete history | Maintenance apply MUST rebuild the replacement from verified branch records and verify that it contains every source record SHA |
| Forged ledger files supplied by the agent | Persistence MUST consume validated requests, not agent-authored shards or coverage declarations |
| Overwriting an existing shard with a truncated copy | Agent-supplied files whose path matches an already-trusted shard ID MUST be rejected during persistence |
| Deleting records appended after planning | Maintenance apply MUST preserve every segment not selected by the validated plan |
| Record or segment identity forgery | Record SHA, canonical serialization, and duplicate identity MUST be revalidated on read; mismatches MUST be diagnostics, never silent acceptance |
| Unbounded growth or denial of service | Record, segment, patch, and shard-count limits MUST be enforced before acknowledging a mutation, and file scanning MUST stop at the configured shard bound |
| Path traversal, symlinks, or non-regular files | Ledger paths MUST be derived from validated UUIDs, opened without following symlinks, and rejected when not regular files |
| Executing custom ledger replay or compaction code | Custom scripts MUST be rejected; replay and segment selection MUST use built-in implementations (sections 4.2 and 4.3) |
| Malicious, stale, or replayed compaction plans | The apply job MUST strictly validate the plan, revalidate it against the latest ledger state, commit with an expected-head guard, and treat a reapplied plan as an idempotent no-op |
| Forged validated transactions | Persistence MUST reject invalid artifact versions, ledger names, deterministic IDs, schemas, operation fields, and size limits |
| Durable prompt injection through stored payloads | Ledger payloads are untrusted content; consumers MUST treat projection query results as data, never as instructions |
| Pre-creating a deterministic replacement shard ID to block compaction | A shard identity collision MUST fail open with a diagnostic and MUST NOT delete, overwrite, or retire any source shard |
| Leaking payloads or credentials through audit or logs | Audit entries MUST carry hashes and identifiers only, ledger errors MUST NOT expose filesystem paths or exception details, and audit logging MUST truncate unexpected values |
| Compaction failure blocking persistence | Compaction MUST fail open and report a diagnostic |
| Silent limit misconfiguration | Count limits and size limits MUST be parsed with their own units; a misparsed limit that disables compaction through fail-open handling is a defect |

Residual risks are accepted and documented rather than mitigated: records are
integrity-checked but not authenticated, so a well-formed record proves only that
its content hashes correctly, never who authored it; the agent can append
semantically false but well-formed records, can consume its own configured
budget, and can request semantically false but schema-valid mutations.
Ledger storage is not confidential; it MUST NOT be used
for secrets.

## 10. Conformance tests

A conforming implementation SHOULD test canonical serialization, hash
verification, malformed-line isolation, concurrent-head convergence, all
configured limits, deterministic compaction, hostile and stale plans,
safe retirement, redacted persistence results,
ledger limit unit parsing, deferred transaction validation, and projection rebuilds.
Tests MUST cover rejection of custom replay and compaction scripts, all six
built-in reducers, invalid-operation projection failures, and preservation of
built-in state and generic record history after lossless compaction.
