# ADR-65566: Table-Driven Shared Helper for strings Comparison Analyzers

**Date**: 2026-10-04
**Status**: Draft
**Deciders**: Unknown

---

### Context

The `pkg/linters` suite contains three analyzers that each detect a `strings` method call compared against an integer constant and suggest a clearer replacement: `stringsindexcontains` (`strings.Index(s, sub) >= 0` → `strings.Contains`), `stringscountcontains` (`strings.Count(s, sub) > 0` → `strings.Contains`), and `stringsindexhasprefix` (`strings.Index(s, prefix) == 0` → `strings.HasPrefix`). Each package independently re-implemented the same pipeline: normalize the comparison operands, flip the operator when the call is on the right-hand side, resolve the constant integer value, render the argument and package-qualifier text, build the diagnostic message, and emit a suggested fix guarded against overlapping comments. The only genuine difference between the three was the set of `(operator, constant, negated)` tuples each one accepts and the replacement method name. This duplication is the same drift hazard previously recorded in [ADR-43649](43649-consolidate-linter-astutil-helpers.md): a fix to the comment-overlap guard or the operand-rendering logic in one analyzer would silently leave the other two behind.

### Decision

We will extract the shared matching, diagnostic, and suggested-fix construction into a single exported helper, `astutil.ReportStringsMethodComparison(pass, expr, method, replacement, comparisons)`, in `pkg/linters/internal/astutil/string_comparison.go`, and express each analyzer's accepted operator/sentinel combinations as a declarative table of `astutil.StringMethodComparison{Op, Value, Negated}` values. The primary driver is maintainability: behavior that is identical across the three analyzers lives in exactly one place, while behavior that genuinely differs stays explicit and readable at each call site as a small package-level table (`indexContainsComparisons`, `countContainsComparisons`, `indexHasPrefixComparisons`). Each analyzer's inspection body collapses to a single call into the helper, removing roughly 95 lines per package.

### Alternatives Considered

#### Alternative 1: Keep three independent implementations (status quo)

Leave each analyzer self-contained with its own copy of the operand normalization, message construction, and fix-building code. This requires zero refactoring, keeps each analyzer independently readable end-to-end, and avoids any coupling between packages. It was rejected because the three copies were already near-identical and any correctness improvement — for example the `HasOverlappingComment` guard that suppresses unsafe autofixes — must currently be applied three times with no mechanism to detect when one copy is missed.

#### Alternative 2: Merge the three analyzers into one `stringscomparison` analyzer

Since all three detect variants of the same anti-pattern, a single analyzer could handle `Index`/`Count` with all replacement targets and emit differentiated messages. This would remove the duplication entirely, not just the shared internals. It was rejected because the analyzers are registered, enabled, suppressed, and reported individually; collapsing them into one would change the public diagnostic names, break existing `//nolint`-style suppressions and per-linter enablement configuration, and make it harder to roll out or disable a single rule. Sharing an internal helper achieves most of the deduplication benefit at no cost to the external contract.

#### Alternative 3: Generic/interface-based helper instead of a data table

Model each analyzer's accepted combinations as a predicate function (`func(op token.Token, v int64) (negated, ok bool)`) passed into the shared helper. This is more flexible for rules that are not expressible as a finite tuple set. It was rejected as premature: all three current rules are exactly enumerable, and a plain slice of structs is easier to read, diff, and test than closures.

### Consequences

#### Positive
- Single source of truth for comparison matching, message formatting, and comment-safe suggested-fix construction across all three analyzers; a fix applies everywhere at once.
- Net reduction of roughly 250 lines across the three linter packages while the supported operator/sentinel matrix becomes more visible as an explicit table rather than buried in nested `switch`/`if` chains.
- Adding a fourth analyzer of the same shape (e.g., `strings.Index(...) < 0` variants or a `HasSuffix` rule) now requires a table plus one call, lowering the cost of new rules.

#### Negative
- The three analyzers now share a mutable dependency: a signature or behavior change to `ReportStringsMethodComparison` requires updating and re-validating all consumers in the same commit, and a regression there affects three rules simultaneously.
- Reading a single analyzer no longer shows the full diagnostic text or fix logic inline; a maintainer must jump to `astutil` to understand exactly what is reported, which slightly raises the onboarding cost per rule.
- The helper currently assumes a two-argument `strings` method call and an integer-constant right-hand side; rules that do not fit that shape cannot use it without widening the abstraction.

#### Neutral
- No change to diagnostic identifiers, analyzer registration, or user-facing rule names; the refactor is intended to be behavior-preserving. [TODO: verify existing analyzer test suites pass unchanged, including suggested-fix golden output.]
- This ADR extends the direction already set by ADR-43649 (consolidating linter helpers into `pkg/linters/internal/astutil`), keeping `internal` scoping so the helper stays private to `pkg/linters/...`.
- The PR also carries unrelated incidental changes (`go.mod` promotion of `go.yaml.in/yaml/v3` to a direct dependency, and recompiled `.github/workflows/agentic_commands.yml` / `pkg/workflow/schemas/github-workflow.json` artifacts) that are not covered by this decision.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
