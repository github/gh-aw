# ADR-60183: Cache Audit Results in Logs JSONL

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The `gh aw logs --cached-jsonl --audit` command currently writes a JSONL cache record for downloaded workflow logs, but the pull request shows that the generated audit output and related metadata are missing from that cached representation. The PR extends the logs pipeline across standard, multi-target, and stdin entry points so that audit mode can persist the complete audit result together with parsed `aw_info.json` metadata and recorded safe-output items. The diff also adds regression tests and a schema update for the new JSONL fields, which indicates downstream consumers rely on the cached JSONL format as a durable interface. The architectural question is whether audit artifacts should remain separate side files or be embedded directly into the logs JSONL cache records.

### Decision

We will persist audit artifacts directly in each logs JSONL run record when audit mode is enabled. We decided to enrich cached JSONL output with the full `audit` payload, parsed `aw_info`, and `safe_outputs`, and to thread the cache writer through every logs execution path so audit-capable invocations produce a self-contained cache entry. This favors completeness and consistency of the cached run record over keeping audit data only in auxiliary files.

### Alternatives Considered

#### Alternative 1: Keep audit results only in separate files under the logs directory

The command could continue generating audit data and metadata as sidecar files such as cached audit outputs and `aw_info.json` without copying them into the JSONL cache. This was considered because it minimizes schema growth and keeps the JSONL writer focused on base run metadata. It was not chosen because the PR evidence shows `--cached-jsonl --audit` is expected to preserve generated audit data in the cache itself, and relying on side files forces downstream consumers to re-open per-run directories to reconstruct a complete audit view.

#### Alternative 2: Store only references to audit artifacts in JSONL

Another option would be to record paths or lightweight status markers in JSONL and require clients to resolve audit, aw-info, and safe-output details from external files on demand. This was considered because it would reduce record size and avoid duplicating structured data. It was not chosen because the tests and schema changes in the PR point toward direct serialization of the complete audit result and supporting metadata, making the cache portable and easier to consume in a single pass.

#### Alternative 3: Add audit caching only to one logs execution path

The implementation could have patched only the main logs command path and left multi-target or stdin flows unchanged. This was considered because it would be a smaller change with less plumbing through orchestrator options. It was not chosen because the PR explicitly propagates the cache writer through standard, multi-target, and stdin paths, indicating the desired behavior is uniform regardless of how logs input is provided.

### Consequences

#### Positive
- Cached JSONL records become self-contained for audit-enabled runs, so downstream tools can read audit findings, engine metadata, and safe-output activity from one serialized record.
- Standard, multi-target, and stdin logs workflows now share the same audit-caching behavior, reducing surprising differences between entry points.
- Regression tests and schema updates make the enriched cache format explicit and safer to evolve.

#### Negative
- JSONL cache records and the schema become larger and more complex because full audit payloads and related metadata are duplicated into the cache.
- The logs rendering pipeline now has additional coupling between audit generation, sidecar-file parsing, and cache serialization.
- Partial failures while appending audit data must be handled carefully, as shown by the new warning path when caching audit data fails.

#### Neutral
- The cache writer now distinguishes between normal run appends and audit-enriched appends through a dedicated `AppendAudit` path.
- Existing sidecar artifacts such as cached audit data and `aw_info.json` still exist; the PR adds serialization into JSONL rather than replacing those files.
- Consumers of `schemas/logs-jsonl.schema.json` need to tolerate the new optional `audit`, `aw_info`, and `safe_outputs` fields.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
