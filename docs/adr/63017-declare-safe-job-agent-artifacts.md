# ADR-63017: Declare safe-job agent artifacts

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Custom `safe-outputs.jobs.*` handlers can read files produced during the agent job, but before this change the unified agent artifact upload only persisted a fixed compiler-managed set of paths. The PR description and diff show that when a custom safe job depended on another path written under `/tmp/gh-aw/agent/`, that path was silently omitted from upload and downstream jobs failed at runtime with no compile-time warning. The change touches safe-job parsing, compiled artifact path collection, schema validation, documentation, and tests. The architectural question is how custom safe jobs should declare filesystem dependencies that must survive from the agent job into downstream safe-output jobs.

### Decision

We will add an explicit `artifacts:` property to `safe-outputs.jobs.*` so custom safe jobs can declare additional agent-job filesystem paths they depend on, and the compiler will merge those paths into the unified agent artifact upload. We will validate those entries at compile time and require them to be rooted under `/tmp/gh-aw/`, the subtree already covered by secret redaction before upload. We chose this because the PR evidence shows the current implicit behavior silently drops required files, while an explicit declaration preserves determinism, keeps security boundaries aligned with existing redaction coverage, and gives authors actionable validation errors.

### Alternatives Considered

#### Alternative 1: Keep the current fixed artifact upload set

This was the existing behavior and requires no new schema or compiler logic. It was not chosen because the PR evidence shows it causes deterministic downstream failures whenever a custom safe job depends on agent-written files outside the compiler's built-in known paths, with no compile-time indication of the real problem.

#### Alternative 2: Upload arbitrary agent-written paths automatically

This was a realistic option because it would avoid asking workflow authors to declare dependencies explicitly. It was not chosen because the diff makes clear that artifact persistence needs to stay constrained to the `/tmp/gh-aw/` tree already scanned for secrets, and broad implicit discovery would weaken predictability, increase upload surface area, and make it harder to reason about what data crosses from the agent job into safe-output jobs.

### Consequences

#### Positive
- Custom safe-output jobs can reliably consume agent-generated directories or files by declaring them explicitly with `artifacts:`.
- Invalid artifact paths fail at compile time with clear errors instead of surfacing later as opaque runtime or step-ordering failures.
- The feature reuses the existing `/tmp/gh-aw/` secret-redaction boundary, keeping artifact persistence aligned with the current security model.

#### Negative
- Workflow authors must learn and maintain a new `artifacts:` field when passing filesystem-based payloads between the agent job and safe-output jobs.
- The compiler and schema gain additional parsing, validation, and deterministic ordering logic that must be tested and maintained.
- Paths outside `/tmp/gh-aw/` remain unsupported, which may require refactoring some workflows to stage files under that subtree.

#### Neutral
- Small values are still expected to flow through normal safe-output JSON inputs; `artifacts:` is an additional mechanism for directories, multiple files, or other filesystem payloads.
- Compiled agent artifact uploads become partly user-declared, but only through a constrained, validated field on custom safe jobs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
