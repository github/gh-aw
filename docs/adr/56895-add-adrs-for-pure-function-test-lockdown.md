# ADR-56895: Add ADRs for Pure Function Test Lockdown

**Date**: 2026-08-29
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request adds targeted tests for three existing pure functions in `pkg/cli` and `pkg/parser` to increase branch coverage and lock down current behavior around malformed remote URLs, historical grader selection, and OTLP attribute extraction. The PR body frames the work as "PureLock" coverage hardening with deterministic, side-effect-free functions and explicitly calls out before/after coverage improvements, validation steps, and residual risk. Although the changes are test-only, they codify architectural expectations about which helper functions are treated as pure and therefore safe to validate with exhaustive table-driven tests rather than broader integration flows. Because the design gate was triggered by >100 added lines in business-logic directories, the decision needs to be recorded explicitly.

### Decision

We will treat narrowly scoped, side-effect-free helper functions as behavior that should be locked down with targeted unit tests covering success paths, malformed input handling, and defensive branches. For this PR, that means adding focused tests for `selectHistoricalOperationalValueGrader`, `extractHostFromRemoteURL`, and `extractOTLPAttributesFromObsMap` instead of changing their production implementations or relying on broader package-level integration tests. We chose this approach because the PR evidence shows these functions are deterministic, coverage gaps are localized, and the desired outcome is to preserve existing semantics while raising confidence.

### Alternatives Considered

#### Alternative 1: Rely on Existing Integration and Package Tests

Keep the current test suite structure and accept the uncovered branches in these helper functions.

This was considered because it avoids adding over 200 lines of new test code and keeps the test surface smaller. It was not chosen because the PR evidence shows specific uncovered error and fallback paths that are important to preserve, and the existing broader tests do not exercise them directly.

#### Alternative 2: Refactor the Production Functions Before Adding Tests

Change the helper implementations or surrounding abstractions first, then add tests against the refactored shape.

This was considered because refactoring can sometimes simplify testability or reduce edge cases. It was not chosen because the PR's stated goal is to lock down current pure behavior, not to alter the design, and changing production code would introduce a broader decision than the evidence in this PR supports.

### Consequences

#### Positive
- The repository gains explicit regression protection for edge cases in three pure helper functions without changing production behavior.
- Coverage improves in targeted areas of `pkg/cli` and `pkg/parser`, making future regressions in fallback and validation logic easier to detect.
- The tests document which behaviors are considered stable for malformed inputs, duplicate records, missing observations, and OTLP attribute filtering.

#### Negative
- The codebase takes on additional test maintenance cost, especially if these helpers evolve and many table cases need updating.
- Locking down current behavior may make future intentional semantic changes noisier because multiple targeted assertions will need coordinated updates.
- Treating large test-only additions as ADR-worthy may create process overhead when the architectural decision is narrow.

#### Neutral
- The decision affects test strategy and behavioral documentation more than runtime architecture or user-facing execution semantics.
- The ADR records purity-based testing as the rationale for this PR, but it does not require every future coverage increase to use the same pattern.
- No new runtime dependencies or external integrations are introduced; the implementation impact remains within test files under existing packages.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
