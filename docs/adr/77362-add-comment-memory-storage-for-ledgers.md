# ADR-77362: Add comment-memory storage for ledgers

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: Peli de Halleux (PR author); gh-aw maintainers (review pending)

---

### Context

Standalone ledgers (`tools.ledger`) store typed, append-only records using the built-in `log`, `set`, `map`, `table`, `counter`, and `notes` reducers. Today every ledger persists only to a Git branch named `ledgers/<name>`, which requires `contents: write` and creates repository branches even for workflows whose state already lives on an issue or pull request through `tools.comment-memory`. Those workflows need durable, reviewable ledger history on the same thread without granting repository write access. The solution must keep Git as the default, preserve transaction validation, deterministic IDs, reducers, and read-only SQLite projections, and stay within the existing comment-memory size and permission limits.

### Decision

We will add `storage: comment-memory` to standalone ledger configuration, with `git` remaining the default. A comment-backed ledger persists each accepted batch as a new, immutable managed comment on the issue or PR selected by `tools.comment-memory`, containing a base64-encoded canonical JSONL shard under a separate `gh-aw-comment-ledger:<name>` fence. Persistence runs only in the trusted `push_ledger_changes` job and never updates or deletes earlier ledger comments. The primary driver is removing the need for repository write access for comment-only ledgers while reusing the existing ledger pipeline and comment-memory targeting.

### Alternatives Considered

#### Alternative 1: Keep Git-only ledger storage

This is the realistic baseline. The Git backend already provides compaction, branch-based history, and reconstruction, and it has no comment size ceiling. It was not chosen for issue- or PR-scoped ledgers because it requires `contents: write` and one `ledgers/<name>` branch per ledger, which is heavy for small, thread-scoped state. This was not a close call on the available evidence. The PR keeps Git as the default, so this decision adds a backend rather than replacing one.

#### Alternative 2: Rewrite a single editable comment-memory comment

This reuses the existing comment-memory model directly: keep one managed comment and rewrite its encoded ledger state on each run. It was not chosen because in-place rewrites make history mutable and let a later writer overwrite earlier state. The spec instead requires immutable batches, forbids updating or deleting prior ledger comments, and uses a separate marker so ordinary editable comment-memory files cannot overwrite ledger history. [TODO: verify] The PR does not record a discussion of this option; the rejection reasoning is inferred from the spec's immutability requirements.

### Consequences

#### Positive
- Comment-only ledgers run with `contents: read` plus `issues: write` and `pull-requests: write`, so workflows no longer need repository write access to persist ledger state.
- Ledger semantics are unchanged: transaction validation, deterministic IDs, reducers, the canonical record DAG, and SQLite projections apply identically to both backends.
- Immutable per-batch comments give an auditable history on the issue or PR, and retried transactions do not duplicate records.

#### Negative
- Each ledger is capped at 48 KiB total, and each batch at 16 KiB, with no compaction. Larger or long-lived histories must move to Git storage.
- Reads scan at most 50 comment pages and fail closed on incomplete or malformed history, so long-lived ledgers can hit a hard ceiling.
- GitHub Actions concurrency can replace pending persistence jobs, and writers in different repositories sharing a target must coordinate externally. Workloads that need every run persisted must serialize the workflow upstream.
- Shards are base64-encoded, not encrypted, so ledger contents are readable by anyone who can read the target issue or PR.

#### Neutral
- Git remains the default, so existing workflows are unaffected unless they opt in. A single `tools.ledger` block can mix backends, and compaction applies only to Git-backed ledgers.
- In staged mode, comment-backed ledgers publish nothing and report `staged: true`.
- Targets are limited to `triggering`, `*`, or a literal issue or PR number, and `target-repo` must be literal. Compile-time validation enforces both rules.
- Ledger records remain untrusted data, and existing comment-memory limits and repository permissions still apply.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
