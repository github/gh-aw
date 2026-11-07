# ADR-75294: Unwrap Parenthesized Expressions in Linter Matchers

**Date**: 2026-11-06
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Several custom Go linters identify calls by asserting directly on AST nodes such as `CallExpr.Fun`. Go permits redundant parentheses around callees and arguments, so equivalent expressions can have a `ParenExpr` at the node being inspected. Direct assertions then miss valid matches. Suggested fixes also need to account for parentheses in the source span: deleting only an inner call can leave a syntactically invalid replacement.

### Decision

At the affected matcher boundaries, unwrap parenthesized AST expressions with `astutil.UnwrapParenExpr` before type assertions. Keep matching and fixes specific to each linter. For `fprintlnsprintf`, compute deletion ranges from the printed argument as written so the replacement removes parentheses around the nested call as well as the call itself.

This change does not introduce a shared matcher abstraction. Whether to centralize this behavior remains a future maintenance decision; the current changes address only the identified matchers.

### Alternatives Considered

#### Alternative 1: Keep direct AST assertions

This avoids code changes, but continues to miss calls that differ only by redundant parentheses and leaves the associated suggested fixes unavailable.

#### Alternative 2: Introduce a shared matcher abstraction

A common helper could make parenthesis handling consistent across more linters. It was not included because this change targets four specific gaps, and broadening the helper API would expand the scope beyond the regression fixes.

#### Alternative 3: Rewrite source text before matching

Normalizing source text could remove parentheses before analysis, but would alter the representation used for diagnostics and fixes. AST-level unwrapping preserves source positions and keeps the change local to matching.

### Consequences

#### Positive

- The affected linters recognize equivalent parenthesized calls and arguments.
- Suggested fixes use source spans that include surrounding parentheses where necessary.
- Regression fixtures document the supported forms.

#### Negative

- Other linters may still need the same targeted treatment if they inspect parenthesized expressions with direct type assertions.
- The repository does not yet have a shared matcher abstraction for this behavior.

#### Neutral

- Parenthesis unwrapping changes which existing source forms are recognized; it does not change the intended transformations for unparenthesized calls.

---

*Review and finalize this draft before changing its status from Draft to Accepted.*
