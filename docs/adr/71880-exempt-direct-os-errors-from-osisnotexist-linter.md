# ADR-71880: Exempt Fresh, Direct `os` Errors from the `osisnotexist` Linter

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: [TODO: verify] PR author and maintainers of `pkg/linters` / `eslint-factory`

---

### Context

The repository runs custom `go/analysis` analyzers (`pkg/linters/`) and ESLint rules (`eslint-factory/`) to enforce coding conventions. The `osisnotexist` analyzer flagged every call to `os.IsNotExist`, `os.IsExist`, `os.IsPermission`, and `os.IsTimeout`. These legacy predicates do not unwrap wrapped errors. However, they work correctly on an error returned directly by an `os` filesystem function, so the linter reported false positives on code that was actually safe. Other detectors had the opposite problem and missed real cases. `no-ad-hoc-jsonl-parser` did not follow the branches of a ternary expression (`cond ? [] : text.split("\n")`). `wgdonenotdeferred` did not detect parenthesized `WaitGroup.Done()` calls or callees. Issue #71384 tracks these detection gaps.

### Decision

We will add a narrow, syntactic exemption to `osisnotexist`. The linter will skip a predicate call only when its single identifier argument meets all of these conditions:
- It is freshly bound with `:=` in the immediately preceding statement, or in the enclosing `if` initializer.
- It comes from a direct call to an allow-listed `os` filesystem function, such as `Stat`, `Open`, `ReadFile`, or `Remove`.
- Nothing between the assignment and the check could change it. The analyzer treats any call, function literal, address-of, or reassignment as a possible change.

Any other case still produces a diagnostic: wrapped, reassigned, aliased, function-value-sourced, or unknown-origin errors, and checks that cross a closure or loop boundary. We are also closing the detection gaps by following both ternary branches in the JSONL rule and by unwrapping parentheses in the WaitGroup rule. The main goals are to reduce noise without hiding real wrapped-error bugs, and to keep the analysis local and cheap.

### Alternatives Considered

#### Alternative 1: Keep flagging every legacy `os` predicate

This is the simplest option and needs no extra analysis. With it, every call site moves to `errors.Is(err, fs.ErrNotExist)`. We rejected it because it reports provably safe code. That pushes authors toward `//nolint` suppressions, which weaken the signal for genuine problems.

#### Alternative 2: Full data-flow or SSA-based error-origin tracking

An analysis built on `go/ssa` could prove where an error came from across statements, branches, and helper functions, and exempt more safe cases. We rejected it because of its cost and complexity, and because mistakes in it could silently suppress real diagnostics. The adjacent-statement heuristic covers the common idiom (`if _, err := os.Stat(p); os.IsNotExist(err)`) and stays easy to audit.

#### Alternative 3: Remove the linter and rely on code review

This would avoid false positives entirely. We rejected it because wrapped errors passed to `os.IsNotExist` are a real source of bugs. Automated detection is more reliable than reviewer vigilance.

### Consequences

#### Positive
- Fewer false positives on idiomatic code that checks an `os` error right after receiving it, so fewer `//nolint` suppressions are needed.
- The JSONL and WaitGroup rules now catch cases they used to miss: ternary-sourced lines and parenthesized `Done()` calls.
- Regression fixtures cover both the new exemptions and the cases that must still report.

#### Negative
- `osisnotexist` now uses an inspector cursor that walks parent nodes and siblings, plus a list of exempt `os` functions. This adds logic that must be kept up to date. For example, if new `os` functions are added, the exempt list must change by hand.
- The exemption is deliberately conservative. Some safe patterns, such as a check that is not adjacent to the assignment or an error passed through a local variable, are still flagged. That can look inconsistent to authors.
- It is possible to rely on legacy predicates and skip `errors.Is` in the exempt cases. The codebase may then mix both styles.

#### Neutral
- The linter README now describes the exemption.
- The diagnostic messages and the suggested replacements are unchanged.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
