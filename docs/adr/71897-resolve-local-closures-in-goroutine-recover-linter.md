# ADR-71897: Resolve Immutable Local Closures in the goroutinemissingrecover Linter

**Date**: 2026-10-25
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent [TODO: verify]

---

### Context

The `goroutinemissingrecover` analyzer (`pkg/linters/goroutinemissingrecover`) flags goroutines that are not protected by a top-level `defer`/`recover` guard. Previously it only inspected goroutines launched with an inline function literal (`go func() { ... }()`), so extracting that literal into a local variable (`fn := func() { ... }; go fn()`) silently bypassed the panic-safety check (issue #71811). A static analyzer cannot generally know which function value a variable holds at the `go` statement, so any extension must avoid inspecting a stale or wrong initializer, while preserving existing behaviour for named functions and `nolint`-style suppression directives.

### Decision

We will extend the analyzer to resolve `go ident()` calls to a function literal when `ident` is a **local** variable whose `:=` or `var` initializer is a function literal, keyed by `*types.Var` object identity (not by name). Variables that are ever reassigned, used as a `range` key/value, or have their address taken (`&v`) are treated as mutable and excluded, as are package-level variables. The existing top-level defer/recover check is then applied unchanged to the resolved literal. The primary driver is closing the bypass with low false-positive risk using a simple, per-package pre-pass.

### Alternatives Considered

#### Alternative 1: Keep checking only inline function literals

Leave the analyzer scoped to `go func() { ... }()` and document that closures assigned to variables are not checked. This is the simplest option with zero false-positive risk, but it leaves a trivial refactoring bypass for a check whose purpose is preventing process-wide crashes, which is why issue #71811 was raised.

#### Alternative 2: Flow-sensitive / SSA-based resolution of function values

Use `golang.org/x/tools/go/ssa` or a dataflow analysis to determine the possible function values at each `go` statement, which would also cover reassigned variables, package variables, and values passed through parameters. This is more complete, but substantially more complex, slower, and harder to maintain than the rest of the repository's AST-based linters; for this gap, conservatively excluding mutable variables was judged sufficient. [TODO: verify whether this was considered]

#### Alternative 3: Name-based resolution of initializers

Match the identifier in `go fn()` to the nearest assignment with the same name. This is simpler than type-object identity but is wrong under shadowing and across scopes; the PR explicitly adds shadowing regression cases, indicating object identity was chosen to avoid this.

### Consequences

#### Positive
- Closes the bypass where extracting an inline literal into a local variable skipped the defer/recover check.
- Object-identity keying makes resolution correct under shadowing and parentheses, avoiding name-collision false positives.
- Excluding mutable (reassigned, range-bound, address-taken) and package variables keeps false positives low by never inspecting a possibly stale initializer.

#### Negative
- Goroutines launched via reassigned, address-taken, package-level, or parameter-passed function values remain unchecked, so the coverage is still incomplete.
- Adds an extra whole-package AST walk (`indexLocalFuncLiterals`) per analysis pass, slightly increasing linter cost and code complexity.
- Existing code that launches unprotected local closures will start producing new diagnostics, requiring fixes or suppressions.

#### Neutral
- Named-function goroutines (`go f()`) remain out of scope, consistent with the previous design.
- Mutability detection is syntactic (any assignment, range binding, or `&` anywhere in the package marks a variable mutable), which is conservative rather than precise.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
