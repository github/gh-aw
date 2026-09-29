---
title: Repository Memory Ledger Specification
description: W3C-style specification for the append-only repo-memory ledger, its projections, compaction, and audit trail
sidebar:
  order: 1370
---

# Repository Memory Ledger Specification

**Version**: 1.0<br>
**Status**: Working Draft<br>
**Editor**: GitHub Agentic Workflows Team

## Abstract

This specification defines the repository memory ledger: a bounded, deterministic,
append-only JSONL record store with a disposable query projection. It specifies
record identity, concurrent reconstruction, compaction, retirement, configuration
limits, and the transaction log consumed by safe-output threat detection.

## 1. Conformance and terminology

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, and **MAY** in this document are to be interpreted as described in
RFC 2119 and RFC 8174.

* **Agent** is the untrusted workflow process that can request ledger reads and
  appends through the ledger MCP server.
* **Record** is one immutable version-1 JSON object.
* **Shard** is a JSONL file containing records. The current writer shard is not
  closed.
* **Projection** is disposable SQLite state reconstructed from shards.
* **Coverage declaration** says that a replacement shard contains all records
  from one or more source shards.
* **Stable shard** is a closed shard not created by the current persistence run.
* **Transaction log** is the JSONL safe-output artifact containing ledger mutation
  events.
* **Agent job** is the workflow job that runs the agent and the ledger MCP server.
* **Persistence job** is the trusted job that restores, compacts, and pushes
  repository memory.
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

Ledger work is split across three jobs with different privileges. An
implementation MUST NOT move a responsibility to a less trusted job.

| Job | Trust | Ledger responsibilities | Credentials |
| --- | --- | --- | --- |
| Agent job | Untrusted | Runs the ledger MCP server; requests `ledger_append`, `ledger_get`, `ledger_query`, `ledger_status`; writes shards into the run's writer shard and audit entries into the transaction log | No memory-branch write token |
| Safe-outputs job | Trusted | Ingests and validates `ledger_mutation` entries and reports them through the log-only handler | Safe-output tokens; no ledger storage access |
| Persistence job | Trusted | Restores memory, reconstructs the DAG, runs compaction and retirement, normalizes files, selects the trusted copy set, commits and pushes | Memory-branch `contents: write` token |

The agent job MUST NOT perform compaction, retirement, or persistence. The
safe-outputs job MUST NOT mutate ledger storage; its `ledger_mutation` handler is
log-only and performs no side effects, because the append it describes already
happened. The persistence job MUST treat everything under the restored memory
directory, including shards, coverage declarations, and audit entries, as
untrusted input.

## 4. Script extension points

Two user-supplied scripts may exist near the ledger. Both are workflow-author
code, not agent output, and both run in the persistence job.

### 4.1 Memory validation script

`repo-memory.validation.script` runs after normalization and before commit. It
MUST run in a separate Node process with a bounded timeout and a sanitized
environment, and a non-zero exit MUST fail persistence (fail-closed), because it
is a policy gate on what is about to be pushed.

### 4.2 Compactor script

A custom `ledger.compactor.script` is **not supported**. Configuring it MUST be a
compile-time error. An in-process Node `vm` context is not a security boundary:
host functions exposed to the context reach the host realm through their own
constructors, which in the persistence job would expose the push token, the
filesystem, and process execution. Compaction policy is therefore declarative
(`ledger.compaction.min-segments`, `ledger.compaction.max-segments`), and all
correctness-sensitive steps — canonical serialization, hash validation, segment
identity, atomic writes, `fsync`, path safety, writer exclusion, coverage
verification, and retirement — remain runtime-owned.

If a future revision reintroduces scripted compaction policy, the script MUST run
in a separate least-privilege process that exchanges only serialized data, MUST
NOT receive host objects, credentials, network access, or Git access, MUST be
deterministic given the restored ledger state, MUST be able to declare coverage
but never delete a source shard, and MUST fail open so that a failed compaction
never blocks ledger reads, appends, or persistence.

## 5. Lifecycle

### 5.1 Restore and reconstruct

Persistence first restores the repository-memory directory and reconstructs the
record DAG from all valid shards. The projection MAY then be rebuilt in memory
for bounded structured queries. The projection is not durable ledger state and
MUST NOT be required for recovery.

### 5.2 Agent append

The agent MAY request `ledger_append`, `ledger_get`, `ledger_query`, and
`ledger_status`. Only `ledger_append` mutates ledger state. The MCP server MUST
validate arguments and MUST NOT expose filesystem paths or runtime exception
details.

After a durable append, the server MUST serialize one `ledger_mutation` event
to the safe-output transaction log. The event MUST identify the operation,
record ID, record type, timestamp, parent hashes, record SHA, and a hash of the
payload. Payload contents MUST NOT be copied into this audit event. A failed
audit write MUST NOT be represented as a successful mutation.

When the workflow enables a ledger, the compiler MUST declare `ledger_mutation`
as an ingested safe-output type so audit entries are validated and bounded rather
than rejected as unexpected output. The corresponding handler MUST be log-only:
it reports the redacted metadata and performs no repository, ledger, or network
side effect, because the mutation it describes is already durable and its
provenance is not verifiable from the transaction log alone.

### 5.3 Compaction

Compaction runs outside the agent, after restore and before the next agent
execution. Only closed stable shards MAY be selected; the current writer shard
MUST be excluded. The runtime MUST deduplicate records by SHA, sort them
deterministically, validate them, create an immutable replacement shard, and
write a coverage declaration.

Compaction policy is declarative and bounded by `min-segments` and
`max-segments`; see section 4.2 for why scripted policy is not supported.
Compaction MUST fail open: a failed compaction MUST leave the ledger readable and
MUST NOT block persistence.

### 5.4 Retirement

The runtime owns retirement. Before deleting a source
shard, it MUST validate the declaration, require that replacement and sources
are stable, read the replacement, and verify that every source record SHA is
present in the replacement. Invalid, incomplete, current-run, or forged
declarations MUST be ignored. Source deletion and coverage-declaration removal
MUST be followed by directory synchronization.

### 5.5 Save

The persistence job saves ordinary memory files and trusted ledger shards to
the repository-memory branch. Agent-supplied ledger shards and coverage files
MUST be excluded from the trusted copy set. Ledger files MUST be included in
the safe-output transaction and threat-detection artifacts so mutations can be
reviewed alongside other workflow outputs.

## 6. Concurrency and guarantees

Concurrent writers MAY produce multiple DAG heads. Reconstruction MUST retain
all valid heads, and the next append MUST reference every current head up to
the configured parent bound. Identical deterministic replacement shards MUST
converge by identity; different valid replacements MAY coexist. No compaction
operation MAY remove a record unless the replacement coverage check succeeds.

The ledger provides integrity, boundedness, deterministic reconstruction, and
best-effort convergence. It does not provide distributed locking, serializable
transactions across branches, or confidentiality of repository-memory contents.

## 7. Configuration

Implementations MUST expose bounded configuration for maximum shard count,
segment size, record size, and patch size. Size fields are expressed in KiB in
workflow configuration and MUST be converted to bytes exactly once. Invalid or
overflowing values MUST be rejected or ignored without silently increasing a
bound. When a ledger limit exceeds the corresponding repo-memory persistence
limit, compilation SHOULD emit a diagnostic.

## 8. Security considerations

The ledger directory is untrusted input during persistence. Path traversal,
symlink replacement, forged coverage, malformed JSON, hash mismatch, duplicate
IDs, and oversized files MUST be handled as invalid input. Threat detection
SHOULD consume the transaction log and ledger artifacts together; an audit
event is evidence of an attempted or completed mutation, not authorization for
that mutation.

## 10. Conformance tests

A conforming implementation SHOULD test canonical serialization, hash
verification, malformed-line isolation, concurrent-head convergence, all
configured limits, deterministic compaction, forged and current-run coverage,
safe retirement, transaction-log redaction, and projection rebuilds.
