# ADR-65567: Share Discarded-Error Matching Across Go Analyzers

**Date**: 2026-10-04
**Status**: Draft
**Deciders**: Unknown (PR author: app/copilot-swe-agent)

---

### Context

The repository ships several custom Go static analyzers under `pkg/linters/` that flag discarded `error` returns: `strconvparseignorederror`, `globwalkignorederror`, and `jsonmarshalignoredeerror`. Each analyzer independently re-implemented the same two mechanics: (1) recognizing the assignment shape `value, _ := pkg.Func(...)` and (2) resolving a selector expression back to its *imported package path* via `pass.TypesInfo.Uses` and `*types.PkgName`. That duplication meant edge-case fixes (such as handling aliased imports like `import conv "strconv"`, or rejecting method calls on local values that merely share a function name) had to be applied in three places and were applied inconsistently. PR #65567 (fixes #65436) targets this duplication in `pkg/linters/`, a business-logic directory, with ~130 added lines.

### Decision

We will centralize discarded-error detection in a single shared helper, `astutil.MatchDiscardedErrorCall(pass, assign)`, inside `pkg/linters/internal/astutil`, which returns the call expression, the resolved imported package path, and the function name. All three analyzers will call this helper instead of hand-rolling the AST and type-resolution logic, while each analyzer retains its own function allow-list and diagnostic message. The primary driver is correctness-through-single-source-of-truth: package resolution must be identical across analyzers so aliased imports are matched and non-package (method) calls are rejected uniformly.

### Alternatives Considered

#### Alternative 1: Leave each analyzer self-contained

Keep the per-analyzer inline matching code as-is and fix bugs individually. This is the status quo and preserves maximum independence between analyzer packages, which is attractive because analyzers are otherwise decoupled plugins. Rejected because it is exactly what produced the inconsistency reported in #65436: three copies of resolution logic drifting apart, with aliased-import support present in some analyzers and not others.

#### Alternative 2: Collapse the analyzers into one generic "discarded error" analyzer driven by configuration

Instead of sharing a helper, a single analyzer could take a table of `package path -> function names` and emit diagnostics for all of them. This would eliminate the duplication even more aggressively. Rejected (at least for this change) because each analyzer carries a distinct, domain-specific diagnostic message, its own `nolint` linter name used in existing `//nolint` directives in the codebase, and `jsonmarshalignoredeerror` additionally handles cases the shared shape does not cover (`_ = json.Unmarshal(...)` and expression statements). Collapsing them would break existing suppression comments and flatten diagnostic quality.

### Consequences

#### Positive
- Single source of truth for assignment-shape and imported-package resolution; a fix such as aliased-import support (`import conv "strconv"`) benefits all three analyzers at once.
- Analyzer files shrink substantially and drop their direct `go/types` dependency, making each analyzer's remaining code purely about *which* functions it cares about and *what* it reports.
- New unit tests (`TestMatchDiscardedErrorCall`) exercise the shared logic directly, including the negative cases of a named error variable and a method call on a local type.

#### Negative
- Introduces coupling: `pkg/linters/internal/astutil` becomes a shared chokepoint, so a regression there now affects three analyzers simultaneously rather than one.
- The shared helper fixes a single matching shape (`x, _ = f(...)`, exactly 2 LHS / 1 RHS). Analyzers needing variants — as `jsonmarshalignoredeerror` already does for `_ = json.Unmarshal(...)` — must still keep bespoke code, so duplication is reduced but not eliminated.
- Diagnostic text now derives the package display name from `path.Base(pkgPath)` rather than the resolved `types.PkgName`, which can differ from the package's declared name for packages whose name does not match the last path segment. [TODO: verify no checked package in `checkedFuncs` is affected.]

#### Neutral
- `go.mod` promotes `go.yaml.in/yaml/v3` from an indirect to a direct dependency, and `.github/workflows/agentic_commands.yml` plus `pkg/workflow/schemas/github-workflow.json` change in the same PR; these appear to be regenerated/unrelated artifacts rather than part of this decision. [TODO: verify whether these belong in this PR.]
- The helper lives under `internal/`, so it remains private to `pkg/linters` and does not become part of any public API surface.
- Several call sites were rewritten to iterate slices (`for index, lhs := range ...`) instead of indexing directly, presumably to satisfy another repository lint rule; this is a stylistic side effect of the refactor.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
