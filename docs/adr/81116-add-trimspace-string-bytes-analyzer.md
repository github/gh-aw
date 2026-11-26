# ADR-81116: Add trimspacestringbytes analyzer for TrimSpace byte conversions

**Date**: 2026-11-25
**Status**: Draft
**PR**: #81116 (closes #81115)

## Context

`strings.TrimSpace(string(b))` converts a byte slice to a string, which copies the full slice, and only then trims whitespace. `bytes.TrimSpace(b)` trims without the copy. The pattern is an allocation micro-optimization candidate in `pkg/linters/`, which already hosts a family of allocation and performance analyzers (for example `stringbytesroundtrip`, `bytescomparestring`, and `appendbytestring`).

Existing repository conventions shape the change:

- Analyzers are registered in `pkg/linters/registry.go` and documented in `pkg/linters/doc.go`, `pkg/linters/README.md`, and the `documentedAnalyzers()` list in `pkg/linters/spec_test.go`. Doc-sync tests keep these in step.
- The README states that micro-optimizations "only matter on hot paths" and gates them on a Go coverage profile (`GH_AW_LINT_COVERAGE_PROFILE`), so that code tests never execute is not flagged.
- Analyzers that are not yet ready for CI are listed in `notYetEnforced` in `pkg/linters/doc_sync_test.go`.

Forces in this PR:

- The rewrite is not a drop-in replacement. `bytes.TrimSpace` returns a slice that aliases the input, so a caller that mutates the result or retains it can behave differently than with the string copy.
- The number of existing matches in the repository is not verified by this gate. The linked issue #81115 reports roughly 144 matches in non-test Go under `pkg/` and `cmd/`. That figure is not re-verified here.

## Decision

Add a `go/analysis` analyzer, `trimspacestringbytes`, with these properties:

1. It reports `strings.TrimSpace(string(b))` when the argument to the conversion is a byte slice. It resolves parenthesized expressions, named byte-slice types, and aliases, and it ignores calls where `strings` has been shadowed. The diagnostic recommends `bytes.TrimSpace(b)` and notes that the result should be converted to `string` if needed.
2. It is hot-path gated through `coverage.ShouldApply` and the `RegisterHotThresholdFlag` flag, so findings are limited to code that tests execute.
3. It honors generated-file exclusions and `//nolint:trimspacestringbytes` directives.
4. It is registered in `allAnalyzers`, documented in the three documentation locations, and listed in `notYetEnforced`. CI enforcement waits for a case-by-case review of existing matches.

## Alternatives Considered

1. **Report every occurrence without a coverage gate.** Rejected. The copy cost matters only where the code runs often, so ungated findings would add churn in cold code. This conflicts with the README policy for allocation linters.
2. **Enable enforcement in CI immediately, by removing the `notYetEnforced` entry.** Rejected for this PR. Because the suggested rewrite aliases its input, existing matches need individual review for mutation and ownership before a blocking rule is safe.
3. **Use a textual or regex check instead of a typed AST analyzer.** Rejected. Text matching cannot reliably handle parenthesized expressions, named byte-slice types, aliases, shadowed identifiers, or per-line `nolint` scoping. The typed analyzer in this PR handles these cases, and its testdata covers them.

## Consequences

Positive:

- The copy-before-trim pattern is flagged on hot paths, consistent with the existing allocation linters.
- The coverage gate and `nolint` directive keep findings focused and give authors a way to suppress known-safe cases.
- The analyzer follows the existing registration and documentation pattern, so the doc-sync and spec tests cover it.

Negative:

- The rule is advisory until the `notYetEnforced` entry is removed. Existing matches are not blocked in CI in the meantime.
- Findings depend on the coverage profile supplied through `GH_AW_LINT_COVERAGE_PROFILE`, so results vary with how that profile is produced.
- The diagnostic suggests a rewrite whose result aliases the input. Authors must check mutation and ownership before applying it, and the message says "consider" for this reason.
