# ADR-78958: Add mapcopyloop Linter to Flag Range-Copy Loops Replaceable with maps.Copy

**Date**: 2026-11-18
**Status**: Draft
**Deciders**: Unknown (automated PR by gh-aw-bot; not yet reviewed by maintainers)

---

### Context

Go 1.21 added `maps.Copy(dst, src)`, which copies every entry of `src` into `dst` in one call. Before that, the common idiom was a range loop that assigns each entry: `for k, v := range src { dst[k] = v }`. The linked issue (#78801) reports about 15 such loops in `pkg/` and `cmd/`, of which the new analyzer flags 10 in `./pkg/...`. The repository keeps a suite of custom `go/analysis` linters in `pkg/linters/` that enforce idiomatic style, and this pattern is unambiguous enough to flag with low false-positive risk. The existing findings need remediation before CI can enforce the rule, so the analyzer ships in advisory mode.

### Decision

We will add a `mapcopyloop` analysis pass (`pkg/linters/mapcopyloop/`) that reports range-over-map loops whose only body statement is `dst[k] = v` using the range key and value, where `dst` and the ranged map have identical types, and suggests `maps.Copy(dst, src)`. The primary driver is readability and consistency with Go 1.21+ idioms, not performance. The suggested fix is offered only when the `maps` package is visible and unshadowed and the loop contains no comments. The analyzer is registered but stays out of CI enforcement (listed under `notYetEnforced` in `doc_sync_test.go`) until existing findings are remediated.

### Alternatives Considered

#### Alternative 1: Extend the existing `mapclearloop` analyzer

`mapclearloop` already flags range-over-map loops that can be replaced by a single built-in call (`clear(m)`). Adding the copy idiom there would keep both map-loop rewrites in one package.

This was not chosen. The two checks match different loop bodies (`delete(m, k)` versus `dst[k] = v`), and the copy fix depends on checks that do not apply to clearing: `maps` import visibility and type identity between two maps. The project convention is one package per linter, as recorded in the sibling `mapclearloop` ADR (46060).

#### Alternative 2: Rely on code review or an upstream linter

Reviewers could catch copy loops by hand, or the team could enable an upstream linter that covers this pattern.

This was not chosen. Manual review does not scale across the codebase, and the PR does not evaluate upstream linters for this pattern [TODO: verify whether any linter already enabled in this repository covers it]. An in-tree analyzer also reuses the project's `nolint` directive handling, generated-file skipping (`filecheck`), and shared `astutil` helpers, and it can ship a suggested fix. This was a close call on maintenance cost; the PR gives no measured comparison [TODO: verify].

### Consequences

#### Positive
- Copy loops can be rewritten as `maps.Copy`, which states the intent in one call.
- The match is narrow: only single-statement `dst[k] = v` loops with non-blank range variables are reported. The testdata covers negative cases (compound assignment, extra statements, self-copy, blank variables, `nolint:mapcopyloop`).
- The suggested fix enables automatic rewriting when it is safe to apply, which reduces manual cleanup.
- The analyzer reuses shared linter infrastructure (`analyzerutil`, `nolint`, `filecheck`, `astutil`), so its behavior matches the other analyzers.

#### Negative
- About 10 existing findings in `pkg/` (per #78801) must be fixed before CI can enforce the rule. Until then the linter produces advisory output only.
- Another analyzer package must be maintained, and the counts in `registry.go`, `doc.go`, `README.md`, and `spec_test.go` must stay in sync (98 to 99 here).
- The match is deliberately narrow. Loops with extra statements, filters, or transformations are not reported, so the linter will not catch every copy-like loop.

#### Neutral
- The suggested fix is withheld when `maps` is not imported or is shadowed, and when the loop contains comments. Those findings are report-only.
- The ADR is numbered by PR number `78958` (five digits, not zero-padded), following the convention of existing ADRs such as `78857-merge-imported-cache-configuration.md`.
- `doc_sync_test.go` and `spec_test.go` check that the documentation and registration stay in sync with the analyzer set.

---

*ADR drafted by the Design Decision Gate using the adr-writer template. Review and finalize before changing status from Draft to Accepted.*
