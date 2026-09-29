# ADR-64310: Add close-error-unchecked linter

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analysis linter under `pkg/linters/closeerrorunchecked/` and registers it in `pkg/linters/registry.go`. The implementation scans AST assignment and expression statements to report zero-argument `Close()` calls whose error return is explicitly discarded, including methods that return an error as their sole result or as their second result. Existing linters in the repository cover related resource-handling concerns, but this PR introduces a distinct policy: explicit suppression of `Close()` errors should be diagnosed as a code quality problem. The architectural decision is whether gh-aw should expand its built-in linting system with a dedicated analyzer for unchecked `Close()` errors.

### Decision

We will add a dedicated `closeerrorunchecked` analyzer to the built-in linter registry to detect zero-argument `Close()` calls whose error return value is explicitly ignored. The analyzer will target bare `Close()` expression statements and blank-identifier assignments, while avoiding methods with parameters or without an error result and respecting generated-file and `nolint` suppression paths already used by the linter framework. Multi-result methods are checked when the error is their second result. Existing production findings must be remediated before the analyzer is added to the CI-enforced linter set. We chose this because the PR evidence shows a recurring error-handling pattern that is not covered by the existing linter set and can hide resource cleanup failures.

### Alternatives Considered

#### Alternative 1: Rely on existing resource-management linters only

This was realistic because the repository already has linters for related cleanup practices such as deferred closing and HTTP response body handling. It was not chosen because the PR explicitly adds new analyzer logic and tests for ignored `Close()` errors, indicating that the existing linters do not catch this specific failure mode.

#### Alternative 2: Leave unchecked `Close()` errors to manual code review

This was considered because it avoids adding another analyzer to the linter registry and keeps the linting surface smaller. It was not chosen because the PR description cites repeated occurrences in the codebase and the implementation demonstrates that the pattern can be detected mechanically with focused AST and type analysis.

### Consequences

#### Positive
- The built-in linter suite will automatically detect explicit discarding of `Close()` errors across standard-library and custom closer types.
- The rule integrates with the existing analyzer framework, generated-file skipping, and `nolint` directives, so it fits current linting workflows.
- Test coverage in `analysistest` documents the intended signal and the non-goals of the rule.

#### Negative
- The linter registry grows, increasing maintenance and review cost for analyzer behavior over time.
- The analyzer uses AST and type inspection heuristics that may need refinement as new edge cases appear.
- Authors may need to add explicit error handling or suppressions in places where they previously ignored cleanup failures.

#### Neutral
- The change is limited to the Go linting subsystem and does not alter runtime workflow execution behavior.
- The repository gains a new package and testdata fixture set under `pkg/linters/closeerrorunchecked/`.
- Future linter-mining efforts can use this analyzer as precedent for adding narrowly scoped error-handling checks.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
