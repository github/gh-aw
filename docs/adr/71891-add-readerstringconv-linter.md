# ADR-71891: Add `readerstringconv` diagnostic-only analysis-pass linter

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: pelikhan, Unknown

---

### Context

The repository keeps its custom `golang.org/x/tools/go/analysis` linters under `pkg/linters/`, one package per analyzer, registered through `linters.All()` in `pkg/linters/registry.go`. Issue #71684 (raised by the Linter Miner workflow) found code such as `strings.NewReader(string(data))` in `pkg/cli/experiments_fetch.go` and `pkg/cli/secret_set_command.go`. These calls copy a `[]byte` into a `string` (or a `string` into a `[]byte`) only to pick a reader type, even though `bytes.NewReader` and `strings.NewReader` can read the original value directly. A mechanical rewrite is not always safe: the two readers have different concrete types (`*strings.Reader` vs `*bytes.Reader`), and the conversion can act as a defensive copy when the caller later changes the original byte slice. The extent of existing findings across the codebase has not been audited.

### Decision

We will add a standalone analyzer, `pkg/linters/readerstringconv`, built on the shared `analyzerutil`, `astutil`, `filecheck`, `nolint`, and `coverage` helpers. It uses type information to report `strings.NewReader(string(b))` where `b` is `[]byte`, and `bytes.NewReader([]byte(s))` where `s` is `string`. It resolves package identity through types, so import aliases and dot imports work, and it unwraps parentheses. The analyzer will report diagnostics only, with **no suggested fix**, because switching readers changes the concrete type and can remove a defensive copy. It respects `//nolint` directives, generated-file and test-file exclusions, and coverage-based hot-path gating. It is registered in `registry.go` and documented in the README, `doc.go`, and the spec/doc-sync tests. CI enforcement is deferred (`notYetEnforced`) until an audit of existing findings is done.

### Alternatives Considered

#### Alternative 1: Provide an automatic suggested fix

Many sibling linters (e.g. `formatintitoa`, `stringbytesroundtrip`) ship `SuggestedFix` edits, which makes cleanup cheap. We rejected this here because the rewrite is not semantics-preserving in general. The resulting reader has a different concrete type, which can break type assertions, struct fields, or function signatures. It can also remove a copy that protects against later mutation of the `[]byte`. A human needs to judge each site.

#### Alternative 2: Rely on an off-the-shelf linter via golangci-lint (e.g. `mirror`, `gocritic`)

Third-party linters cover some string/byte conversion patterns. They were not chosen because the repository standardizes on in-tree analyzers that share its nolint, generated-file, and coverage-gating infrastructure, and because they would need extra configuration and toolchain dependencies to match these exclusions. [TODO: verify whether an existing third-party check was evaluated]

#### Alternative 3: Fold the check into the existing `stringbytesroundtrip` / `appendbytestring` analyzers

The pattern is a close relative of other redundant-conversion checks. A separate package was preferred to match the established one-analyzer-per-package convention. This keeps the analyzer's name, nolint key, and enforcement status independent.

### Consequences

#### Positive
- Unnecessary allocations and copies in reader construction are flagged automatically in new and existing code.
- Matching uses type information, which avoids false positives from look-alike identifiers and handles aliased and dot imports.
- Diagnostic-only output avoids silently changing reader concrete types or removing defensive copies.

#### Negative
- Without an autofix, each finding needs manual remediation, which slows cleanup.
- Enforcement is deferred, so the linter does not yet stop regressions in CI. It also adds to the `notYetEnforced` backlog.
- One more analyzer package adds maintenance and some per-run analysis cost.

#### Neutral
- Coverage gating means findings may only appear on hot paths when coverage data is supplied.
- Docs (README, `doc.go` analyzer count) and the spec/doc-sync tests must stay in sync as analyzers are added.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
