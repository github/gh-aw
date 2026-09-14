# ADR-60893: Add Blank-Assign-Comma Linter

**Date**: 2026-09-14
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go static analysis linter in `pkg/linters/blankassigncomma/` and registers it in the shared linter registry. The implementation specifically targets assignment statements where every result is discarded via two or more blank identifiers such as `_, _ = f()` and `_, _, _ = g()`, while intentionally allowing single blank assignments and any assignment that retains at least one non-blank identifier (e.g. `_, _, err := f()`). The PR description and test data frame this pattern as a code smell that can hide unintentionally ignored return values and reduce clarity about whether results should be checked. The architectural question is whether the lint suite should explicitly enforce this code-quality rule as a first-class analyzer.

### Decision

We will add a dedicated `blankassigncomma` analyzer to the repository's Go linter suite and run it through the existing analyzer registry. The analyzer will report assignments where every left-hand side entry is blank and there are two or more of them, while skipping generated files and respecting `nolint` directives to stay consistent with existing linter behavior. Because the codebase still contains such occurrences, the analyzer is registered but tracked as `notYetEnforced` in CI until those are remediated. We chose this because the PR evidence shows a recurring pattern in the codebase that is better handled by a reusable static analysis rule than by ad hoc review comments.

### Alternatives Considered

#### Alternative 1: Rely on manual code review

The team could leave detection of `_, _ = ...` patterns to human reviewers instead of adding a new analyzer. This was considered because it avoids growing the linter surface area and keeps policy flexible. It was not chosen because the PR explicitly documents an existing production occurrence and adds deterministic test coverage, indicating this smell is better enforced automatically and consistently.

#### Alternative 2: Broaden or narrow the rule scope

Another option would be to flag any assignment with two or more leading blank identifiers regardless of trailing non-blank identifiers (e.g. `_, _, err := f()`). This was considered because it would catch more candidate result-ignoring patterns. It was not chosen because such assignments retain and check a value that cannot be dropped, so requiring every left-hand side entry to be blank avoids flagging these legitimate selective-assignment patterns.

### Consequences

#### Positive
- The repository gains automated detection of a documented code smell involving multiple ignored return values.
- Linter behavior is consistent with existing analyzer infrastructure because it is registered centrally and honors generated-file and `nolint` exclusions.
- Regression tests make the accepted and rejected patterns explicit for future maintainers.

#### Negative
- The project adds another custom lint rule that maintainers must keep compatible with Go AST and analyzer utility changes.
- Some existing or future code may need `nolint` annotations or refactoring when the rule flags intentional patterns.
- The rule encodes a style judgment that may occasionally be debated in edge cases where multiple ignored returns were deliberate.

#### Neutral
- The implementation introduces a new package under `pkg/linters/blankassigncomma/` plus testdata fixtures.
- Analyzer registration changes in `pkg/linters/registry.go` expand the default linter set.
- The rule only examines assignment statements where every left-hand side entry is blank, leaving other result-ignoring patterns (such as selective assignments that keep one real identifier) unchanged.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
