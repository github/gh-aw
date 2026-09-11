# ADR-60310: Add Slice Make Zero Length Linter

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new custom Go analyzer under `pkg/linters/` and registers it in the shared linter registry, which makes the change part of the repository's standard static-analysis policy rather than an isolated utility. The implementation targets calls of the form `make([]T, 0)` without a capacity argument and treats them as a performance-oriented code smell, with tests showing both expected findings and allowed cases such as explicit capacity, non-zero length, and `nolint` suppression. Because the linter becomes part of the central analyzer suite, the architectural decision is whether this repository should enforce this allocation pattern through automated linting. The available PR evidence emphasizes performance and repeated review feedback as the primary drivers for codifying the rule.

### Decision

We will add a custom `slicemakezerolength` analyzer to the repository's shared linter registry. It flags `make([]T, 0)` only when the next statement is a range loop over a value with a known length and the loop appends exactly one element to that slice per iteration. This makes `len(rangeValue)` a useful capacity hint while avoiding diagnostics when the slice does not grow or its growth cannot be derived. The analyzer supports existing repository conventions such as generated-file skipping, coverage gating, and `nolint` suppression.

### Alternatives Considered

#### Alternative 1: Keep this as a code review guideline only

The team could continue treating `make([]T, 0)` without capacity as an informal review suggestion instead of building a dedicated analyzer. This was considered because the PR body explicitly describes the pattern as a common review comment, and manual review avoids growing the custom linter suite. It was not chosen because the diff shows the pattern occurs in multiple locations across the codebase and the change aims to make the guidance consistent and automatically enforceable.

#### Alternative 2: Rely on existing third-party linters

Another option would be to depend on an upstream linter or broader performance lint package instead of adding a repository-specific analyzer. This was considered because it could reduce local maintenance and reuse community tooling. It was not chosen because the PR implements the rule directly in `pkg/linters/`, integrates it with the local registry and helper utilities, and therefore indicates the repository wants targeted behavior aligned with its existing custom-linter framework.

#### Alternative 3: Flag every zero-length slice allocation without capacity

The analyzer could report every `make([]T, 0)` without analyzing subsequent use. This simpler syntactic rule was not chosen because slices that never grow need no backing allocation, and slices with indeterminate growth have no defensible capacity hint. Restricting the rule to a direct, known-size append loop provides a precise optimization with predictable enforcement.

### Consequences

#### Positive
- The repository will enforce this slice-allocation convention consistently across reviews and automation.
- Developers get fast feedback for a repeated performance-oriented pattern without waiting for reviewer intervention.
- The analyzer fits the existing custom linter architecture, including registry-based activation, testdata-driven verification, coverage gating, and `nolint` support.

#### Negative
- The deliberately narrow pattern leaves more complex growth patterns to manual performance analysis.
- Maintaining another custom analyzer increases long-term cost for compatibility, testing, and linter-suite complexity.
- Codifying this recommendation as a lint rule may push style and micro-optimization policy into CI, which can increase friction for contributors.

#### Neutral
- The implementation only adds diagnostics; it does not include an automatic fix or rewrite.
- Existing suppression mechanisms remain available through `//nolint:slicemakezerolength`.
- The decision extends the current custom-linter framework rather than introducing a new enforcement mechanism.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
