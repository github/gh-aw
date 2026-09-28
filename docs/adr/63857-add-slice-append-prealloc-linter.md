# ADR-63857: Add slice-append-prealloc linter

**Date**: 2026-09-28
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go static analysis linter under `pkg/linters/sliceappendpreallocmissing` and registers it in the shared linter registry. The analyzer detects repeated `append` calls inside loops when the loop bound is statically knowable and the destination slice was not pre-allocated with capacity. The PR also adds positive and negative analyzer fixtures that define which loop forms, declarations, and suppressions are in scope. The architectural question is whether gh-aw should enforce this performance-oriented pattern as a first-class built-in linter in its custom linting suite.

### Decision

We will add a built-in `sliceappendpreallocmissing` analyzer to the gh-aw linter registry to report slice growth via repeated `append` in loops with statically determinable iteration counts when the slice was not pre-allocated. The analyzer will target simple `for` and `range` loop cases, honor generated-file and `nolint` exclusions, and ship with analyzer tests and fixtures. We chose this because the PR evidence frames the pattern as a high-signal, low-ambiguity performance issue with a clear remediation of pre-allocating slice capacity.

### Alternatives Considered

#### Alternative 1: Leave this pattern unenforced and rely on manual review

This was a realistic option because slice pre-allocation is an optimization rather than a correctness fix, and reviewers could catch obvious cases during code review. It was not chosen because the PR introduces a dedicated analyzer with test fixtures showing repeatable static detection rules, which is more scalable and consistent than reviewer memory.

#### Alternative 2: Use a broader existing performance linter instead of adding a dedicated analyzer

This was considered because gh-aw already has an expanding set of custom linters and a broader analyzer might reduce maintenance overhead. It was not chosen because the implementation adds repository-specific AST logic for loop-bound inference, declaration checks, `nolint` handling, and generated-file skipping that indicates a dedicated analyzer is the intended mechanism for this exact pattern.

### Consequences

#### Positive
- gh-aw gains automated detection of a recurring slice-capacity performance issue instead of relying on ad hoc review.
- The rule is integrated into the existing analyzer registry and test harness, making it runnable anywhere the custom linter suite runs.
- The implementation explicitly defines supported cases and exclusions, which should keep findings predictable for contributors.

#### Negative
- The repository takes on long-term maintenance for another custom analyzer and its fixtures.
- The analyzer intentionally supports only statically inferable loop sizes, so some real pre-allocation opportunities will remain unreported.
- Contributors may need to update code or add targeted suppressions for findings that are technically valid but low priority in context.

#### Neutral
- The new rule extends existing linting architecture rather than changing the runtime behavior of production code.
- Future enhancements to support more loop forms or data-flow inference can build on this analyzer instead of starting from scratch.
- Test coverage for this decision lives primarily in analyzer fixtures under `pkg/linters/sliceappendpreallocmissing/testdata`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
