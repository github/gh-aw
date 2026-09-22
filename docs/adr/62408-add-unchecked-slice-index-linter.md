# ADR-62408: Add unchecked-slice-index linter

**Date**: 2026-09-21
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request adds a new Go static-analysis linter under `pkg/linters/unchecked-slice-index/` to detect direct slice and string indexing that can panic at runtime. The implementation walks `ast.IndexExpr` nodes, identifies slice, string, and array indexing patterns, and reports cases that are not protected by local bounds checks, safe range-loop usage, or constant array indices. The PR evidence also adds targeted test fixtures covering flagged and allowed cases, which shows the decision is not a routine bug fix but a new safety rule in the repository's linting surface. The architectural question is whether gh-aw should enforce this class of panic prevention through a dedicated analyzer integrated into the existing custom linter framework.

### Decision

We will add a dedicated `uncheckedsliceindex` analyzer to the repository's custom linter suite to detect direct slice or string indexing without evident bounds checks. The analyzer will reuse the existing internal linter helper packages for traversal, parent-map construction, generated-file filtering, and `nolint` suppression, while treating guarded accesses, range-loop indices, and constant array indices as safe. We chose a custom analyzer because the PR evidence presents a repository-specific linting framework already used for bespoke checks, and this approach makes panic-prone indexing enforceable during static analysis instead of relying on code review alone.

### Alternatives Considered

#### Alternative 1: Rely on code review and existing generic linters

The repository could avoid adding a new analyzer and instead depend on reviewers and the current lint set to catch unsafe indexing patterns manually. This was considered because it avoids maintaining new AST and control-flow logic. It was not chosen because the PR explicitly identifies a gap in the existing linter coverage and provides concrete bad patterns that can still panic at runtime if left to manual review.

#### Alternative 2: Implement a broader panic-safety or data-flow linter instead of a targeted indexing rule

Another option would be to create a more general analyzer for panic-prone operations, possibly with deeper flow analysis beyond direct index checks. This was considered because it could cover more classes of runtime panics with one abstraction. It was not chosen because the diff is narrowly scoped to unchecked slice and string indexing, and a focused analyzer fits the current helper-based linter architecture with lower implementation complexity and clearer diagnostics.

### Consequences

#### Positive
- The repository gains automated detection of a concrete runtime-panic pattern that was previously unchecked by the custom linter suite.
- The analyzer aligns with the existing linter architecture by reusing internal helper packages and established suppression behavior.
- The added testdata demonstrates expected safe and unsafe patterns, which should make the rule easier to maintain and extend.

#### Negative
- The new analyzer adds maintenance cost for custom AST and control-flow logic, including edge cases around bounds checks and scope.
- A targeted static rule may produce false positives or false negatives when code uses more complex guard patterns than the analyzer currently recognizes.
- Expanding the custom linter surface increases the long-term burden of keeping analyzer behavior consistent with repository expectations.

#### Neutral
- The change introduces a new package under `pkg/linters/unchecked-slice-index/` without altering the repository's overall linter framework structure.
- `// nolint:uncheckedsliceindex` remains available as an escape hatch for intentionally accepted cases.
- The PR evidence shows tests and implementation for the analyzer itself, but not broader policy changes elsewhere in the codebase.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
