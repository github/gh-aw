# ADR-64809: Add pointer type assertion nil-safety linter

**Date**: 2026-10-01
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analyzer at `pkg/linters/typeassertionnil` and registers it in the linter registry. The analyzer specifically reports single-value type assertions to pointer types such as `v.(*MyType)` when they are used without the two-value `ok` form, and the included test fixtures distinguish those cases from safe two-value assertions, type switches, and non-pointer assertions. The PR description frames this as panic prevention for a recurring safety pattern in the codebase, and the visible constraint is that the new check must stay narrowly targeted so it improves safety without flagging unrelated or already-safe assertion patterns.

### Decision

We will add a dedicated `typeassertionnil` linter that flags unchecked type assertions to pointer types and recommends the two-value `v, ok := ...` form. We will implement it as a focused analyzer in the existing linter framework and register it alongside the other repository-specific analyzers. We chose this narrower rule because the PR evidence shows a specific architectural decision to treat pointer assertions as a distinct nil-panic risk instead of broadening an existing general type-assertion lint.

### Alternatives Considered

#### Alternative 1: Rely on the existing general unchecked type assertion linter

This was a realistic option because the repository already contains related analyzers such as `uncheckedtypeassertion` and `typeassertionokdiscarded`. It was not chosen because this PR introduces a new analyzer with pointer-specific logic, indicating the maintainers want a rule focused on nil-prone pointer assertions with tailored diagnostics rather than folding the behavior into a broader existing check.

#### Alternative 2: Do not add a linter and rely on code review or runtime testing

This was plausible because unsafe type assertions can sometimes be caught during review or through failing tests. It was not chosen because the PR explicitly adds static analysis and fixtures for multiple unsafe patterns, which shows a preference for catching this panic class automatically before code reaches runtime.

### Consequences

#### Positive
- The codebase gains an automated check for a specific nil-pointer panic pattern in Go code.
- Developers get actionable diagnostics that point them toward the safer two-value assertion form.
- The rule remains narrowly scoped, which should reduce noise compared with a broader assertion policy.

#### Negative
- The repository now has another custom linter to maintain, test, and keep registered in the analyzer set.
- Pointer-specific detection may overlap conceptually with existing assertion-related linters, increasing rule surface area for contributors.
- Some future edge cases may require additional refinement if safe patterns are expressed in forms not covered by the initial implementation.

#### Neutral
- The implementation follows the existing internal analyzer utilities, file skipping, and `nolint` handling conventions.
- Test coverage is provided through `analysistest` fixtures that encode both flagged and allowed examples.
- Adoption affects repository lint behavior rather than application runtime behavior directly.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
