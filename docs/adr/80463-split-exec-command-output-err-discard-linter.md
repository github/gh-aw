# ADR-80463: Split Exec Command Output Linter into Responsibility-Based Files

**Date**: 2026-11-23
**Status**: Draft
**Deciders**: Unknown — PR author `gh-aw-bot`; reviewers [TODO: verify]

---

### Context

`pkg/linters/execcommandoutputerrdiscard/execcommandoutputerrdiscard.go` had grown to 1,534 lines and about 60 functions in a single file. Linked issue #80381 describes this as hard to maintain and proposes a split at an 800-line threshold. The package's only test is one `analysistest` run over `testdata` in a 16-line test file, so behavior is guarded mainly by that testdata run. Many AST-walking helpers take 5–6 parameters (`pass`, `body`, `object`, `after`, `before`), which makes the call graph hard to follow. The PR body reports 83.1% coverage.

Unknown: review discussion beyond the PR body and issue #80381 was not available when this draft was written.

### Decision

We will split the analyzer implementation into five files in the same package, grouped by responsibility, and keep the exported `Analyzer` API and the diagnostics unchanged:

- `execcommandoutputerrdiscard.go`: analyzer entry point, call detection, and result binding (about 180 lines remain).
- `failure_paths.go`: failure-path propagation checks (312 lines added).
- `retention.go`: output retention and slice alias analysis (346 lines).
- `stderr.go`: stderr and `ExitError` detection, plus captured-buffer analysis (460 lines).
- `astutil.go`: shared AST and type helpers (239 lines).

The driver is maintainability: each file covers one analysis concern, so each concern can be reviewed and changed without reading the whole analyzer.

### Alternatives Considered

#### Alternative 1: Keep the single file and rely on section comments

This is the status quo that issue #80381 describes as hard to maintain. The file is 1,534 lines, well above the proposed 800-line threshold. The PR does not record whether this option was evaluated.

#### Alternative 2: Introduce an `analysisContext` struct in the same change

Issue #80381 proposes grouping the repeated `(pass, body, object, after, before)` parameters into a struct. The PR diff contains no `analysisContext` type, so this change is not included. The rationale for excluding it is not recorded. Adding it would change function signatures across all five files and widen the review surface of a refactor intended to preserve behavior.

### Consequences

#### Positive

- Each file maps to one analysis concern. The largest new file is `stderr.go` at 460 lines, down from one 1,534-line file.
- The exported `Analyzer` API and the diagnostics are intended to be unchanged, per the PR description.

#### Negative

- Logic for a single check can now span `failure_paths.go`, `retention.go`, and `stderr.go`, so reviewers must follow helpers across files.
- Behavior preservation depends mainly on the existing testdata-driven `analysistest` run. The test file is 16 lines, and issue #80381 notes that coverage is thin.
- Four index expressions in the moved code gained `//nolint:uncheckedsliceindex` comments. These suppress the linter at those sites and need reviewer confirmation that the guards they cite are sufficient.
