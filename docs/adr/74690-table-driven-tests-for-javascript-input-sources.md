# ADR-74690: Table-Driven Tests for javascriptInputHasSources

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: [TODO: verify] gh-aw maintainers and PureLock automation

---

### Context

The JavaScript taint analyzer uses `javascriptInputHasSources` to decide whether an inline JavaScript input has source-bearing data that requires analysis when no JavaScript rule is configured. The helper considers stdin, environment-source lists, and literal-source lists, while metadata, arguments, and files do not affect that decision. Before this change, the helper had no direct test suite, leaving nil and empty collections and less typical keys insufficiently protected. The pull request is test-only and aims to improve regression safety without changing taint-analysis behavior.

### Decision

We will protect `javascriptInputHasSources` with a table-driven, package-level Go test using named parallel subtests. The cases will cover zero values, nil and empty source collections, stdin/environment/literal sources, zero-valued source entries, Unicode and empty map keys, and fields that must not count as sources. We chose focused pure-function tests because they exercise the helper's complete input contract with less setup and less coupling than full analyzer integration tests.

### Alternatives Considered

#### Alternative 1: Add separate tests for each source collection

We could add independent test functions for stdin, environment sources, literal sources, and ignored fields. This would be straightforward and would keep each scenario locally simple. We did not choose it because it would duplicate setup and assertion structure, make the complete decision table harder to review, and provide less consistent coverage for nil, empty, and mixed collections.

#### Alternative 2: Test only through `javascriptInline` and the full analyzer

We could exercise the helper indirectly by constructing analyzer inputs and asserting whether JavaScript execution occurs. This would validate an end-to-end path, but it would also couple the regression tests to rendering, analyzer setup, and JavaScript execution details. We did not choose it as the sole strategy because it would make failures less diagnostic and would not isolate the helper's collection semantics.

#### Alternative 3: Use fuzzing or property-based tests

We could generate arbitrary `JavaScriptInput` values and assert a property such as whether any source-bearing collection is non-empty. We did not choose this because the helper has a small, explicit structural contract whose boundary cases are clearer as named examples; the pull request's coverage analysis also found fuzzing unnecessary.

### Consequences

#### Positive
- The helper's source-bearing branches and empty-collection boundaries are covered directly, including stdin, environment, and literal inputs.
- Named parallel subtests make Unicode keys, empty keys, zero-valued sources, and ignored metadata easy to identify when a regression occurs.
- The tests document that `Args`, `Files`, and other metadata do not make an input source-bearing.

#### Negative
- The test suite pins the current source-detection contract, so adding a new source-bearing field requires an intentional test and ADR review update.
- The 138-line table adds maintenance cost and does not by itself prove that the surrounding JavaScript analyzer integrates every source path correctly.
- Because the tests run in the package's internal namespace, they depend on the unexported helper's name and signature.

#### Neutral
- The test uses the repository's existing `testify/assert` dependency and changes no production code or runtime behavior.
- Parallel subtests run as part of the ordinary `pkg/workflow/taint` Go test suite; no new integration service, fixture, or fuzzing corpus is introduced.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
