# ADR-77455: Resolve Package Specification Drift

**Date**: 2026-11-13
**Status**: Draft
**Deciders**: Unknown (AI-drafted from PR #77455 and audit issue #77454; human deciders [TODO: verify])

---

### Context

Each package under `pkg/` documents its public contract in `pkg/<name>/README.md`, and a matching `spec_test.go` enforces that contract. A spec-librarian audit (issue #77454) found four documented APIs that were missing from source (`scanfindings.Sort`, `scanfindings.CountAtLeast`, `intent.ResolveRisk`, and `workflow.SanitizeIdentifier`), a field drift in `actionpins` (`Mappings` vs `ContainerMappings`), an invalid-input contract mismatch in `semverutil`, and about 337 undocumented exported symbols. Where specs and code disagreed, `SPEC_MISMATCH` comments left spec tests that could not compile or run as regression guards. This PR adds 256 lines under `pkg/`, which triggers ADR enforcement because the count exceeds the 100-line default.

### Decision

We will reconcile each spec-to-code mismatch individually, using the package README as the contract. Where the spec describes intended, user-visible behavior, we implement it: `scanfindings.Sort` and `CountAtLeast`, `intent.ResolveRisk`, and column-level `omitempty` for slice tables in `pkg/console`. Where the code is the deliberate behavior, we correct the spec: `actionpins` now documents `ContainerMappings`, and `semverutil` documents the `v`-prefix applied to invalid input. The primary driver is contract accuracy: specs and spec tests should match what callers can actually use. [TODO: verify that the code-is-authoritative direction for `actionpins` and `semverutil` is intended.]

### Alternatives Considered

#### Alternative 1: Remove the phantom APIs from the specs

This is the audit's first recommendation. It is the smallest change, because it adds no exported surface and changes no console output. It was not chosen, as far as the PR shows, because the `scanfindings` and `intent` specs describe behavior that callers would reasonably need, and removing them would drop documented contracts. This was a close call, since removal is cheap and reversible. [TODO: verify the rationale with the PR author, because the PR body does not record why implementation won.]

#### Alternative 2: Keep the current table behavior and document it

The current behavior keeps an `omitempty` column in slice tables and renders a dash for zero values. Documenting that behavior would avoid changing the rendered layout of any existing table. It was not chosen because `pkg/console/README.md` already states that `omitempty` omits zero values, and the spec test in this PR asserts that a column is omitted when every row is zero. Changing the spec to fit the existing output would weaken the documented contract. [TODO: verify whether any caller depends on the existing column layout.]

### Consequences

#### Positive
- `scanfindings.Sort`, `scanfindings.CountAtLeast`, and `intent.ResolveRisk` now exist and have behavior tests, so their documented contracts can be relied on.
- The `actionpins` and `semverutil` READMEs describe actual behavior, and their `SPEC_MISMATCH` markers are resolved by tests instead of comments.
- Slice tables hide columns that are zero in every row, which reduces noise for sparse fields.

#### Negative
- `scanfindings` and `intent` gain new exported functions that become a compatibility commitment. The domain-to-risk mapping in `ResolveRisk` (for example, production maps to high) is a policy that will need review as domains change.
- The console change alters rendered output for existing `omitempty` slice tables that have all-zero columns. Golden output or downstream parsers that expect those columns will break. [TODO: verify that CI tests pass, because the audit could not run Go tests due to TLS errors fetching modules.]
- The README export tables added in this PR are hand-maintained and will drift again. Their "verified on 2026-11-13" stamps need periodic re-verification.

#### Neutral
- `Sort` orders findings by file, line, column, descending severity, and then rule ID. This is a chosen ordering, not an inherent one.
- `ResolveRisk` uses the explicit `Risk` field first, then falls back to `unknown` when no domain rule applies.
- The audit marked the `console` marker as stale, but this PR changes the slice-table path. Reviewers should confirm that the spec wording matches the intended behavior.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
