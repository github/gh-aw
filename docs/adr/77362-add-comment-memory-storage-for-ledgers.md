# ADR-77362: Add comment-memory storage for ledgers

**Date**: 2026-11-13
**Status**: Proposed (maintainer review pending)

---

### Context

Standalone ledgers (`tools.ledger`) store typed, append-only records using the built-in `log`, `set`, `map`, `table`, `counter`, and `notes` reducers. Before this change, every ledger persists only to a Git branch named `ledgers/<name>`, which requires `contents: write` and creates repository branches even for workflows whose state already lives on an issue or pull request through `tools.comment-memory`. Those workflows need durable, reviewable ledger history on the same thread without granting repository write access. The requirements are to reuse as much existing code as possible, keep Git as the default, and persist readable JSONL in comments while exposing the same read-only SQLite query projections as Git-backed ledgers.

### Decision

Add `storage: comment-memory` to standalone ledger configuration, with `git` remaining the default. A comment-backed ledger persists accepted records as new, immutable managed comments on the issue or PR selected by `tools.comment-memory`, containing plain JSONL shards under a separate `gh-aw-comment-ledger:<name>` fence. Trusted preparation reconstructs those records into `/tmp/gh-aw/ledgers/<name>/ledger.db`, a disposable read-only SQLite projection for agent queries. JSONL comments, not the database, are the durable state.

Reuse shared transaction validation, canonical record IDs and DAG reconstruction, reducers, and projection generation rather than defining separate ledger semantics. Persistence runs only in the trusted `push_ledger_changes` job and never updates or deletes earlier ledger comments. Publication must retain every accepted record, including batches spanning multiple shards, and ingestion must authenticate source comment authors rather than trusting a managed marker alone. These are required invariants, not claims that a marker or content hash authenticates history.

Reuse comment-memory token selection, target resolution, and cross-repository allowlists. Targets must be `triggering`, `*`, or a literal positive issue/PR number; `target-repo` must be literal. The [ledger specification](../src/content/docs/specs/repo-memory-ledger-specification.md#comment-backed-storage) defines the backend contract.

### Alternatives Considered

#### Alternative 1: Keep Git-only ledger storage

The Git backend provides compaction, branch-based history, and reconstruction without the comment-memory size ceiling. Keeping it as the only backend does not meet the requirement for issue- or PR-scoped persistence without repository write access. Git remains the default and is the appropriate backend for larger histories; this decision adds a backend rather than replacing one.

#### Alternative 2: Rewrite a single editable comment-memory comment

Keeping one managed comment and rewriting its JSONL state would reuse ordinary comment-memory persistence, but makes history mutable and allows later writers to overwrite earlier state. It does not satisfy the append-only requirement. Immutable comments and a separate ledger marker preserve the distinction between editable memory files and ledger history. This comparison follows from the requirements; it does not assert an undocumented design discussion.

### Consequences

#### Positive
- Comment-only ledgers run with `contents: read` plus `issues: write` and `pull-requests: write`, so workflows no longer need repository write access to persist ledger state.
- Ledger semantics are unchanged: transaction validation, deterministic IDs, reducers, the canonical record DAG, and read-only SQLite projections apply identically to both backends. Persisted comment history remains directly readable as JSONL.
- Immutable JSONL comments keep history inspectable on the issue or PR. Retries reuse deterministic record identities rather than duplicating logical records.

#### Negative
- Each ledger is capped at 48 KiB total, and each JSONL batch at 16 KiB, with no compaction. Larger or long-lived histories should use Git storage.
- Reads scan at most 50 comment pages and fail closed on incomplete or malformed history, so long-lived ledgers can hit a hard ceiling.
- Persistence jobs use `queue: max` by default to retain pending jobs up to GitHub Actions queue limits. Disabling `features.group-concurrency-queue` restores pending-job replacement; workloads that need every run persisted must account for queue limits and workflow cancellation. Writers in different repositories sharing a target must coordinate externally.
- Shards are plain JSONL, not encrypted, so ledger contents are readable by anyone who can read the target issue or PR.

#### Neutral
- Git remains the default, so existing workflows are unaffected unless they opt in. A single `tools.ledger` block can mix backends, and compaction applies only to Git-backed ledgers.
- In staged mode, comment-backed ledgers publish nothing and report `staged: true`.
- Cross-repository targets require allowlisted access and a token with permissions on the target repository.
- Authenticating source comments does not establish the truth of ledger payloads. Records remain untrusted data, and existing comment-memory limits and repository permissions still apply.
- The verified publishing identity is the trust anchor, as with Git writer credentials. Holders of that credential are authorized writers; ordinary agent publication cannot emit reserved ledger blocks, and no separate signing key is required.
