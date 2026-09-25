# ADR-63475: Add loop index address linter

**Date**: 2026-09-25
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go analyzer under `pkg/linters/loopindexaddresstaken/` and registers it in `pkg/linters/registry.go`. The PR description explains that it targets a subtle concurrency bug where a `for range` loop index variable is reused across iterations and its address is captured inside a goroutine or deferred function. The implementation and tests show a deliberate choice to detect only address-of usage on the range index variable, while ignoring value capture via function parameters, plain variable reads, generated files, nested function literals, and `nolint` suppressions. The architectural question is how `gh-aw` should encode this bug pattern in its static-analysis suite without broadening the rule into a noisier generic loop-capture checker.

### Decision

We will add a dedicated linter, `loopindexaddresstaken`, that reports taking the address of a `for range` loop index variable when that address is used inside goroutines or deferred function literals, and we will wire it into the shared linter registry. We chose a narrowly scoped analyzer instead of a broader capture rule because the PR evidence emphasizes high signal-to-noise and shows targeted exclusions for by-value capture and non-address uses. This keeps the rule focused on a concrete, repeatable concurrency hazard that is difficult to catch in review.

### Alternatives Considered

#### Alternative 1: Rely on existing linters and code review

This was a realistic option because the bug pattern is understandable to experienced Go reviewers and the project already ships many linters. It was not chosen because the PR description explicitly states that this pattern recurs in real code and is not covered well enough by the current lint stack, making manual review alone too unreliable for a subtle concurrency defect.

#### Alternative 2: Add a broader loop-variable capture analyzer

This was considered because a more general analyzer could flag any goroutine or deferred closure that references range variables, not just address-of expressions. It was not chosen because the implementation and fixtures in this PR intentionally restrict detection to `&indexVar` patterns and ignore plain reads or by-value capture in order to preserve precision and avoid a noisier rule with more false positives.

### Consequences

#### Positive
- The linter suite gains an automated check for a specific concurrency bug that is easy to miss in code review.
- The rule is narrowly scoped and backed by positive and negative fixtures, which should keep signal high.
- Registering the analyzer in the central registry makes the protection available consistently across the existing lint workflow.

#### Negative
- The project takes on ongoing maintenance for another custom analyzer and its test corpus.
- The narrow rule does not catch other loop-variable capture mistakes outside the specific address-of pattern.
- Future contributors must understand the analyzer's intentional exclusions so they do not assume it provides comprehensive closure-capture coverage.

#### Neutral
- The new analyzer follows the repository's existing internal analyzer utilities, generated-file skipping, and `nolint` conventions.
- The ADR documents a focused static-analysis decision rather than a runtime architecture change, but it still affects repository-wide quality enforcement.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
