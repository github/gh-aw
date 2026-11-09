# ADR-76295: Add sortbasicslices Analyzer for Basic Sort Helpers

**Date**: 2026-11-09
**Status**: Draft
**Deciders**: Unknown [TODO: verify. PR requested by pelikhan; PR authored by copilot-swe-agent from linter-miner issue #76288.]

---

### Context

Go code under `pkg/` and `cmd/` calls `sort.Strings`, `sort.Ints`, and `sort.Float64s`, which predate the generic `slices` package and are not type-safe. The existing `sortslice` analyzer covers only `sort.Slice` and `sort.SliceStable`, so nothing flags these three helpers. The linked issue #76288 reports about 172 non-test call sites, with 169 existing findings under `./pkg/...`. Enforcement must not block CI until those findings are remediated, and the rule must respect the repository's `//nolint` conventions and aliased imports.

### Decision

We will add `sortbasicslices`, a diagnostic-only `go/analysis` analyzer that reports `sort.Strings`, `sort.Ints`, and `sort.Float64s` calls and recommends `slices.Sort`. It resolves the callee through `types.Info` so aliased imports are covered, and it honors `//nolint:sortbasicslices`. The analyzer is registered and documented but is added to the `notYetEnforced` map, so CI enforcement is deferred until existing findings are remediated.

### Alternatives Considered

#### Alternative 1: Extend the existing `sortslice` analyzer

`sortslice` already targets the sort family and suggests `slices` replacements, so the three helpers could be added to its scope. This was not chosen because `sortslice` is scoped to `sort.Slice` and `sort.SliceStable` with `slices.SortFunc` guidance, and the replacement and fix shape differ. A separate analyzer keeps each rule's scope, documentation, and suppression directive independent. This is a judgment inferred from the diff, not stated in the PR.

#### Alternative 2: Enforce in CI immediately with a `SuggestedFix`

Enforcing now would catch regressions sooner and could offer an automatic fix. This was not chosen. A fix would also need to add the `slices` import, which the analyzer does not do, and roughly 169 existing findings would fail CI today. The PR therefore ships diagnostics only and defers enforcement.

### Consequences

#### Positive
- New code is steered toward the type-safe `slices.Sort` at analysis time.
- Aliased imports (for example `import s "sort"`) are detected, and identifiers that shadow the `sort` package are not flagged, per the testdata.
- The analyzer follows the existing registration pattern and supports the standard `//nolint` directive.

#### Negative
- Existing violations remain, so the rule is advisory until remediation is complete and CI enforcement is enabled.
- Registration is spread across `registry.go`, `README.md`, `doc.go`, `spec_test.go`, and the `doc_sync_test.go` count map. Drift is caught only by tests.
- There is no automatic fix, so remediation is manual and each site needs its own `slices` import.

#### Neutral
- The rule is excluded from hot-path coverage gating because it is a readability rule, not a performance rule.
- Behavior is covered by `analysistest` cases for direct calls, aliased imports, parenthesized calls, shadowed identifiers, and `nolint` suppression.
- The published analyzer count moves from 93 to 94 in `doc.go` and the README.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
