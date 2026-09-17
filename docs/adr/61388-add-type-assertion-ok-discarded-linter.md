# ADR-61388: Add type-assertion-ok-discarded linter

**Date**: 2026-09-16
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analyzer under `pkg/linters/typeassertionokdiscarded/` and registers it in the shared linter registry. The implementation targets a specific pattern: two-value type assertions where the second `ok` result is explicitly discarded with `_`, as shown in the new analyzer test fixtures and described in the PR body. The repository already contains linter infrastructure, generated-file skipping, nolint support, and a complementary `uncheckedtypeassertion` analyzer, so the architectural question is whether this codebase should treat discarded-`ok` type assertions as a first-class lint violation. The non-negotiable constraint visible in the PR is to implement the check as a standard AST-based analyzer that integrates with the existing linter suite and test harness.

### Decision

We will add a dedicated `typeassertionokdiscarded` analyzer to the gh-aw linter suite to report two-value type assertions whose `ok` result is discarded with the blank identifier. The analyzer will inspect `ast.TypeAssertExpr` nodes, determine whether they participate in a two-value assignment or declaration with `_` in the second position, and emit a diagnostic directing authors to either use the single-value assertion form or actually check the `ok` result. We chose this because the PR evidence shows the repository wants explicit enforcement of this type-assertion anti-pattern through the same reusable analyzer framework used by other custom Go linters.

### Alternatives Considered

#### Alternative 1: Rely on the existing uncheckedtypeassertion linter only

The repository already has an `uncheckedtypeassertion` analyzer, so one alternative was to keep enforcing only single-value assertion misuse and leave discarded-`ok` cases unaddressed. This was considered because it avoids another custom linter and keeps type-assertion guidance consolidated in one existing rule. It was not chosen because the PR body and new fixtures make clear that discarded-`ok` assertions are treated as a distinct anti-pattern with different remediation: either intentionally use the single-value form or check `ok` instead of discarding it.

#### Alternative 2: Depend on code review or a generic external linter rule

Another option was to document this as a style expectation and catch it during review, or to wait for a generic upstream lint rule to cover it. This was considered because it would avoid maintaining a repository-specific analyzer with AST parent tracking and tests. It was not chosen because the diff explicitly invests in an in-repo analyzer integrated with the current registry, generated-file handling, nolint directives, and analysistest fixtures, indicating the decision is to automate enforcement rather than rely on manual review or unavailable generic tooling.

### Consequences

#### Positive
- The repository gains automated enforcement for a specific misleading type-assertion pattern that was previously easy to miss in review.
- The new rule integrates with the existing custom linter registry, test harness, generated-file filtering, and `nolint` support.
- The test fixtures document accepted and rejected type-assertion forms, making the intended coding standard more explicit.

#### Negative
- The project must maintain another custom analyzer, including AST parent mapping logic and ongoing compatibility with analyzer infrastructure.
- Some contributors may need to rewrite existing code patterns or add justified suppressions when this rule is enabled against broader code.
- The linter suite becomes slightly more complex because type-assertion guidance is now split across complementary analyzers.

#### Neutral
- The change affects static analysis behavior and tests, but does not alter runtime behavior of production code directly.
- The analyzer distinguishes only assignments and declarations with a blank second result, so other type-assertion patterns remain governed by existing rules.
- Registration in `pkg/linters/registry.go` makes the new analyzer part of the standard linter bundle used by the repository.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
