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

## 3. Lifecycle

### 3.1 Restore and reconstruct

Persistence first restores the repository-memory directory and reconstructs the
record DAG from all valid shards. The projection MAY then be rebuilt in memory
for bounded structured queries. The projection is not durable ledger state and
MUST NOT be required for recovery.

### 3.2 Agent append

The agent MAY request `ledger_append`, `ledger_get`, `ledger_query`, and
`ledger_status`. Only `ledger_append` mutates ledger state. The MCP server MUST
validate arguments and MUST NOT expose filesystem paths or runtime exception
details.

After a durable append, the server MUST serialize one `ledger_mutation` event
to the safe-output transaction log. The event MUST identify the operation,
record ID, record type, timestamp, parent hashes, record SHA, and a hash of the
payload. Payload contents MUST NOT be copied into this audit event. A failed
audit write MUST NOT be represented as a successful mutation.

### 3.3 Compaction

Compaction runs outside the agent, after restore and before the next agent
execution. Only closed stable shards MAY be selected; the current writer shard
MUST be excluded. The runtime MUST deduplicate records by SHA, sort them
deterministically, validate them, create an immutable replacement shard, and
write a coverage declaration.

Declarative compaction options MAY select a deterministic batch. A custom
compactor, when enabled, MUST be treated as policy code only. It MUST NOT
receive Git credentials, arbitrary filesystem access, network access, shell
execution, or Git mutation capabilities.

### 3.4 Retirement

The runtime, not a custom compactor, owns retirement. Before deleting a source
shard, it MUST validate the declaration, require that replacement and sources
are stable, read the replacement, and verify that every source record SHA is
present in the replacement. Invalid, incomplete, current-run, or forged
declarations MUST be ignored. Source deletion and coverage-declaration removal
MUST be followed by directory synchronization.

### 3.5 Save

The persistence job saves ordinary memory files and trusted ledger shards to
the repository-memory branch. Agent-supplied ledger shards and coverage files
MUST be excluded from the trusted copy set. Ledger files MUST be included in
the safe-output transaction and threat-detection artifacts so mutations can be
reviewed alongside other workflow outputs.

## 4. Concurrency and guarantees

Concurrent writers MAY produce multiple DAG heads. Reconstruction MUST retain
all valid heads, and the next append MUST reference every current head up to
the configured parent bound. Identical deterministic replacement shards MUST
converge by identity; different valid replacements MAY coexist. No compaction
operation MAY remove a record unless the replacement coverage check succeeds.

The ledger provides integrity, boundedness, deterministic reconstruction, and
best-effort convergence. It does not provide distributed locking, serializable
transactions across branches, or confidentiality of repository-memory contents.

## 5. Configuration

Implementations MUST expose bounded configuration for maximum shard count,
segment size, record size, and patch size. Size fields are expressed in KiB in
workflow configuration and MUST be converted to bytes exactly once. Invalid or
overflowing values MUST be rejected or ignored without silently increasing a
bound. When a ledger limit exceeds the corresponding repo-memory persistence
limit, compilation SHOULD emit a diagnostic.

## 6. Security considerations

The ledger directory is untrusted input during persistence. Path traversal,
symlink replacement, forged coverage, malformed JSON, hash mismatch, duplicate
IDs, and oversized files MUST be handled as invalid input. Threat detection
SHOULD consume the transaction log and ledger artifacts together; an audit
event is evidence of an attempted or completed mutation, not authorization for
that mutation.

## 7. Conformance tests

A conforming implementation SHOULD test canonical serialization, hash
verification, malformed-line isolation, concurrent-head convergence, all
configured limits, deterministic compaction, forged and current-run coverage,
safe retirement, transaction-log redaction, and projection rebuilds.
