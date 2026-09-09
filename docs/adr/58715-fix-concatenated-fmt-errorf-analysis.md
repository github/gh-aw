# ADR-58715: Fix concatenated fmt.Errorf analysis in linters

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: gh-aw maintainers

---

## Context

The `cacheRecoveryError` helper in the audit pipeline built its `fmt.Errorf` format string from concatenated string fragments and formatted the causal error with `%v`, which prevented callers from using `errors.Is` and `errors.As` on the returned error. The existing `errorfwrapv` and `fmterrorfnoverbs` linters only inspected plain string literals, so they missed this bug pattern when the format string was assembled with `+` concatenation. This pull request fixes the production bug and extends the shared linter infrastructure so the same class of mistake is detected in concatenated format strings. The implementation must avoid inventing false format verbs when opaque non-literal operands appear between literal fragments.

## Decision

We will refactor `cacheRecoveryError` to use a literal format string with `%s` for its message argument and `%w` for its error argument (`fmt.Errorf("%s\n\n...", message, runID, runOutputDir, err)`), avoiding format string concatenation in production while wrapping the causal error with `%w`.

We will add a shared AST utility, `astutil.ResolveFormatString`, that resolves `fmt.Errorf`-style format-string expressions built from string literals and `+`-concatenated literal trees. When an expression contains non-literal (opaque) operands, `ResolveFormatString` returns `ok = false` because format verbs and positional argument indices cannot be proven at compile time.

We will update `errorfwrapv` and `fmterrorfnoverbs` to analyze format strings resolved by `astutil.ResolveFormatString`. This allows detecting mistakes in multi-line or concatenated string literal format strings without risking false positives or incorrect argument indexing when opaque operands are present.

## Alternatives Considered

### Keep linter analysis limited to plain string literals

This was the previous behavior and would have minimized implementation change in the linter stack. It was rejected because the production bug in `cacheRecoveryError` demonstrates that real code in this repository already constructs `fmt.Errorf` format strings through concatenation, so literal-only analysis leaves an important blind spot.

### Special-case only `cacheRecoveryError`

The PR could have changed `%v` to `%w` in the helper and added a regression test without touching shared linter code. It was rejected because the PR evidence shows the underlying issue is broader than one helper: both `errorfwrapv` and `fmterrorfnoverbs` missed concatenated format strings, so a one-off fix would not prevent recurrence elsewhere.

### Fully evaluate arbitrary non-literal string expressions

A more aggressive option would be to resolve identifiers, function calls, or constant propagation across all string-producing expressions. It was rejected because the current PR only justifies support for concatenated expressions with literal segments, and broader evaluation would add complexity and risk without evidence it is needed for this bug class.

## Consequences

### Positive

- `cacheRecoveryError` now preserves the wrapped error chain, so callers can use `errors.Is` and `errors.As` on permission and cache-recovery failures.
- `errorfwrapv` and `fmterrorfnoverbs` now detect concatenated `fmt.Errorf` patterns that previously escaped linting, reducing the chance of similar regressions.

### Negative

- The shared AST utility adds concatenation tree traversal, which requires test coverage for nested `+` literal expressions.

### Neutral

- The new analysis intentionally skips format strings with non-literal (opaque) operands, choosing safety over unprovable expression evaluation.
- Additional unit and testdata coverage is required to lock in behavior around concatenation, verb detection, and false-positive prevention.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
