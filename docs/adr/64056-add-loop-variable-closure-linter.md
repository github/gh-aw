# ADR-64056: Add loop variable closure linter

**Date**: 2026-09-28
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analyzer under `pkg/linters/loopvarmutationinclosure` and registers it in the shared linter registry. The implementation targets closures inside `for` and `range` loops where loop variables are captured directly instead of being shadowed or passed as parameters, a bug pattern that can lead to unintended mutations and data races. The PR body states that the repository already contains multiple real examples of this pattern and that existing lint coverage does not catch it. The decision in this PR is how gh-aw should enforce this Go safety rule within its existing static-analysis architecture.

### Decision

We will add a dedicated custom analyzer named `loopvarmutationinclosure` to the gh-aw linter suite and enable it through the central analyzer registry. The analyzer will inspect `for` and `range` loop bodies, detect function literals that capture loop variables, and allow the two explicit safe patterns shown in the PR evidence: shadowing the variable locally or passing it as a function parameter. We chose this because it fits the repository's existing custom-linter architecture and provides repository-specific enforcement for a recurring, high-signal Go correctness issue.

### Alternatives Considered

#### Alternative 1: Rely on existing upstream lint rules only

This was a realistic option because gh-aw already uses standard Go analysis infrastructure and could avoid maintaining another custom analyzer. It was not chosen because the PR evidence explicitly states that the loop-variable-in-closure pattern is not covered by the existing lint configuration while still appearing repeatedly in the codebase.

#### Alternative 2: Fix known call sites manually without adding a reusable analyzer

This was considered because the PR body lists concrete examples already found in the repository, and targeted code fixes would remove the currently known instances. It was not chosen because manual cleanup would not prevent regressions, whereas a registered analyzer enforces the rule across future code changes and packages.

### Consequences

#### Positive
- gh-aw gains automated detection for a known Go closure-capture bug pattern across the codebase.
- The decision reuses the existing analyzer framework, registry, generated-file skipping, and `nolint` directive support already established in the repository.
- Future regressions can be caught during linting instead of after runtime failures or race investigations.

#### Negative
- The repository now owns another custom analyzer that must be maintained as Go syntax, tooling, and local lint conventions evolve.
- The analyzer logic must balance sensitivity and false positives, especially around shadowing, parameter passing, and nested closures.
- Additional tests and fixture maintenance are required to keep the rule behavior stable.

#### Neutral
- The change expands the linter catalog and testdata footprint under `pkg/linters/`.
- Contributors now have a project-specific rule to satisfy when writing closures inside loops.
- The analyzer supplements, rather than replaces, language knowledge and code review for concurrency-related bugs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
